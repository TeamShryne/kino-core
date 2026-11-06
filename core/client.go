package kinocore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/TeamShryne/kino-core/core/pow"
	"github.com/TeamShryne/kino-core/core/sse"
)

const (
	defaultBaseURL    = "https://chat.deepseek.com"
	pathCreateSession = "/api/v0/chat_session/create"
	pathPowChallenge  = "/api/v0/chat/create_pow_challenge"
	pathCompletion    = "/api/v0/chat/completion"
	pathSettings      = "/api/v0/client/settings"
	pathHistory       = "/api/v0/chat/history_messages"
	pathSessionPage   = "/api/v0/chat_session/fetch_page"
	pathSessionTitle  = "/api/v0/chat_session/update_title"
	pathSessionDelete = "/api/v0/chat_session/delete"
	pathSessionPin    = "/api/v0/chat_session/batch_update_pinned"
	pathFeedback      = "/api/v0/chat/message_feedback"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient() *Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return &Client{baseURL: defaultBaseURL, http: &http.Client{Transport: transport}}
}

// NewClientWithHTTPClient supports tests and host-app networking policies.
func NewClientWithHTTPClient(baseURL string, client *http.Client) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if client == nil {
		client = NewClient().http
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: client}
}

func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }

func validateCredentials(creds Credentials) error {
	if creds.Provider != "" && creds.Provider != ProviderDeepSeek {
		return fmt.Errorf("unsupported provider %q", creds.Provider)
	}
	missing := make([]string, 0, 4)
	if strings.TrimSpace(creds.Token) == "" {
		missing = append(missing, "token")
	}
	if strings.TrimSpace(creds.WAFToken) == "" {
		missing = append(missing, "waf_token")
	}
	if strings.TrimSpace(creds.HIFDliq) == "" {
		missing = append(missing, "hif_dliq")
	}
	if strings.TrimSpace(creds.HIFLeim) == "" {
		missing = append(missing, "hif_leim")
	}
	if len(missing) != 0 {
		return fmt.Errorf("missing DeepSeek credentials: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (c *Client) headers(creds Credentials, accept string) (http.Header, error) {
	if err := validateCredentials(creds); err != nil {
		return nil, err
	}
	ua := creds.UserAgent
	if ua == "" {
		ua = "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Mobile Safari/537.36"
	}
	version := creds.AppVersion
	if version == "" {
		version = "2.3.0"
	}
	locale := creds.Locale
	if locale == "" {
		locale = "en_US"
	}
	tz := creds.TimezoneOffset
	if tz == "" {
		tz = "19800"
	}
	h := make(http.Header)
	h.Set("Accept", accept)
	h.Set("Content-Type", "application/json")
	h.Set("Origin", c.baseURL)
	h.Set("Referer", c.baseURL+"/")
	h.Set("X-App-Version", version)
	h.Set("X-Client-Version", version)
	h.Set("X-Client-Platform", "web")
	h.Set("X-Client-Locale", locale)
	h.Set("X-Client-Bundle-Id", "com.deepseek.chat")
	h.Set("X-Client-Timezone-Offset", tz)
	h.Set("Authorization", "Bearer "+creds.Token)
	h.Set("User-Agent", ua)
	h.Set("X-Hif-Dliq", creds.HIFDliq)
	h.Set("X-Hif-Leim", creds.HIFLeim)
	cookie := "aws-waf-token=" + creds.WAFToken
	if creds.SessionID != "" {
		cookie += "; ds_session_id=" + creds.SessionID
	}
	h.Set("Cookie", cookie)
	return h, nil
}

type apiEnvelope struct {
	Code int      `json:"code"`
	Msg  string   `json:"msg"`
	Data *apiData `json:"data"`
}
type apiData struct {
	BizCode int             `json:"biz_code"`
	BizMsg  string          `json:"biz_msg"`
	BizData json.RawMessage `json:"biz_data"`
}

type APIError struct {
	HTTPStatus int    `json:"http_status,omitempty"`
	Code       int    `json:"code,omitempty"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string { return e.Message }

func (c *Client) doJSON(ctx context.Context, creds Credentials, method, path string, query url.Values, body any, out any) (http.Header, error) {
	var reader io.Reader
	if body != nil {
		var b bytes.Buffer
		if err := json.NewEncoder(&b).Encode(body); err != nil {
			return nil, err
		}
		reader = &b
	}
	u := c.baseURL + path
	if len(query) != 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return nil, err
	}
	h, err := c.headers(creds, "application/json, text/plain, */*")
	if err != nil {
		return nil, err
	}
	req.Header = h
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DeepSeek %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("DeepSeek %s: read response: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{HTTPStatus: resp.StatusCode, Message: fmt.Sprintf("DeepSeek %s returned HTTP %d: %s", path, resp.StatusCode, safeBody(raw))}
	}
	var envelope apiEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("DeepSeek %s: invalid JSON response: %w", path, err)
	}
	if envelope.Code != 0 {
		return nil, &APIError{Code: envelope.Code, Message: fmt.Sprintf("DeepSeek %s: %s", path, envelope.Msg)}
	}
	if envelope.Data == nil {
		return nil, fmt.Errorf("DeepSeek %s: missing response data", path)
	}
	if envelope.Data.BizCode != 0 {
		return nil, &APIError{Code: envelope.Data.BizCode, Message: fmt.Sprintf("DeepSeek %s: %s", path, envelope.Data.BizMsg)}
	}
	if out != nil {
		if err := json.Unmarshal(envelope.Data.BizData, out); err != nil {
			return nil, fmt.Errorf("DeepSeek %s: invalid business data: %w", path, err)
		}
	}
	return resp.Header, nil
}

func safeBody(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 320 {
		s = s[:320] + "…"
	}
	return s
}

func (c *Client) CreateSession(ctx context.Context, creds Credentials) (ChatSession, error) {
	var response struct {
		ChatSession struct {
			ID string `json:"id"`
		} `json:"chat_session"`
		TTLSeconds int64 `json:"ttl_seconds"`
	}
	_, err := c.doJSON(ctx, creds, http.MethodPost, pathCreateSession, nil, map[string]any{}, &response)
	if err != nil {
		return ChatSession{}, err
	}
	if response.ChatSession.ID == "" {
		return ChatSession{}, errors.New("DeepSeek create session response did not contain an id")
	}
	return ChatSession{ID: response.ChatSession.ID, TTLSeconds: response.TTLSeconds}, nil
}

type powChallenge struct {
	Algorithm  string `json:"algorithm"`
	Challenge  string `json:"challenge"`
	Salt       string `json:"salt"`
	Signature  string `json:"signature"`
	Difficulty uint64 `json:"difficulty"`
	ExpireAt   int64  `json:"expire_at"`
	TargetPath string `json:"target_path"`
}

func (c *Client) proofHeader(ctx context.Context, creds Credentials, targetPath string) (string, error) {
	var response struct {
		Challenge powChallenge `json:"challenge"`
	}
	_, err := c.doJSON(ctx, creds, http.MethodPost, pathPowChallenge, nil, map[string]string{"target_path": targetPath}, &response)
	if err != nil {
		return "", err
	}
	ch := response.Challenge
	if ch.Algorithm != "DeepSeekHashV1" {
		return "", fmt.Errorf("unsupported DeepSeek PoW algorithm %q", ch.Algorithm)
	}
	answer, err := pow.Solve(ctx, ch.Salt, ch.ExpireAt, ch.Challenge, ch.Difficulty)
	if err != nil {
		return "", fmt.Errorf("solve DeepSeek PoW: %w", err)
	}
	data, err := json.Marshal(struct {
		Algorithm  string `json:"algorithm"`
		Challenge  string `json:"challenge"`
		Salt       string `json:"salt"`
		Answer     uint64 `json:"answer"`
		Signature  string `json:"signature"`
		TargetPath string `json:"target_path"`
	}{ch.Algorithm, ch.Challenge, ch.Salt, answer, ch.Signature, ch.TargetPath})
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func (c *Client) GetSettings(ctx context.Context, creds Credentials, deviceID, scope string) (SettingsSnapshot, error) {
	if strings.TrimSpace(deviceID) == "" {
		return SettingsSnapshot{}, errors.New("device id is required to load DeepSeek settings")
	}
	query := url.Values{"did": []string{deviceID}}
	if scope != "" {
		query.Set("scope", scope)
	}
	var response struct {
		Settings map[string]json.RawMessage `json:"settings"`
		Version  int64                      `json:"version"`
	}
	headers, err := c.doJSON(ctx, creds, http.MethodGet, pathSettings, query, nil, &response)
	if err != nil {
		return SettingsSnapshot{}, err
	}
	refresh := time.Duration(0)
	if sec, parseErr := strconv.Atoi(headers.Get("X-Fetch-After-Sec")); parseErr == nil && sec > 0 {
		refresh = time.Duration(sec) * time.Second
	}
	snapshot := SettingsSnapshot{Scope: scope, Version: response.Version, RefreshAfter: refresh, Values: make(map[string]json.RawMessage, len(response.Settings))}
	for key, rawEntry := range response.Settings {
		var entry struct {
			Value json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(rawEntry, &entry); err == nil {
			snapshot.Values[key] = entry.Value
		}
	}
	if raw, ok := snapshot.Values["model_configs"]; ok {
		if err := json.Unmarshal(raw, &snapshot.ModelConfigs); err != nil {
			return SettingsSnapshot{}, fmt.Errorf("decode model config setting: %w", err)
		}
		var modelRaw []json.RawMessage
		if err := json.Unmarshal(raw, &modelRaw); err == nil {
			for i := range snapshot.ModelConfigs {
				if i < len(modelRaw) {
					snapshot.ModelConfigs[i].Raw = append(json.RawMessage(nil), modelRaw[i]...)
				}
			}
		}
	}
	return snapshot, nil
}

func (c *Client) LoadCapabilities(ctx context.Context, creds Credentials, deviceID string) (Capabilities, error) {
	type result struct {
		snapshot SettingsSnapshot
		err      error
	}
	results := make(chan result, 2)
	for _, scope := range []string{"model", ""} {
		scope := scope
		go func() { s, err := c.GetSettings(ctx, creds, deviceID, scope); results <- result{s, err} }()
	}
	var models, general SettingsSnapshot
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil {
			return Capabilities{}, r.err
		}
		if r.snapshot.Scope == "model" {
			models = r.snapshot
		} else {
			general = r.snapshot
		}
	}
	version := models.Version
	if general.Version > version {
		version = general.Version
	}
	refresh := models.RefreshAfter
	if refresh == 0 || general.RefreshAfter != 0 && general.RefreshAfter < refresh {
		refresh = general.RefreshAfter
	}
	general.RefreshAfter = refresh
	limits := TokenLimits{}
	if raw := general.Values["normal_history_and_file_token_limit"]; len(raw) != 0 {
		_ = json.Unmarshal(raw, &limits.NormalHistoryAndFiles)
	}
	if raw := general.Values["r1_history_and_file_token_limit"]; len(raw) != 0 {
		_ = json.Unmarshal(raw, &limits.ThinkingHistoryAndFiles)
	}
	return Capabilities{Provider: ProviderDeepSeek, DeviceID: deviceID, Version: version, Models: models.ModelConfigs, ModelSettings: models, Settings: general, TokenLimits: limits}, nil
}

func (c *Client) History(ctx context.Context, creds Credentials, sessionID string) (History, error) {
	if sessionID == "" {
		return History{}, errors.New("session id is required")
	}
	query := url.Values{"chat_session_id": []string{sessionID}}
	var response struct {
		ChatMessages []json.RawMessage `json:"chat_messages"`
	}
	_, err := c.doJSON(ctx, creds, http.MethodGet, pathHistory, query, nil, &response)
	if err != nil {
		return History{}, err
	}
	messages := make([]Message, 0, len(response.ChatMessages))
	for _, raw := range response.ChatMessages {
		var message Message
		if err := json.Unmarshal(raw, &message); err != nil {
			return History{}, fmt.Errorf("decode history message: %w", err)
		}
		message.Raw = append(json.RawMessage(nil), raw...)
		messages = append(messages, message)
	}
	return History{Messages: messages}, nil
}

func (c *Client) ListSessions(ctx context.Context, creds Credentials, request SessionPageRequest) (SessionPage, error) {
	if request.Limit <= 0 {
		request.Limit = 30
	}
	if request.Limit > 100 {
		request.Limit = 100
	}
	mode := request.Mode
	if mode == "" {
		mode = "lte"
	}
	if mode != "lte" && mode != "gte" {
		return SessionPage{}, fmt.Errorf("session page mode must be lte or gte")
	}
	query := url.Values{"count": []string{strconv.Itoa(request.Limit)}}
	query.Set(mode+"_cursor.pinned", strconv.FormatBool(request.Pinned))
	query.Set(mode+"_cursor.updated_at", strconv.FormatFloat(request.UpdatedAt, 'f', -1, 64))
	var response struct {
		HasMore      bool             `json:"has_more"`
		ChatSessions []SessionSummary `json:"chat_sessions"`
	}
	_, err := c.doJSON(ctx, creds, http.MethodGet, pathSessionPage, query, nil, &response)
	if err != nil {
		return SessionPage{}, err
	}
	page := SessionPage{Sessions: response.ChatSessions, HasMore: response.HasMore}
	if len(page.Sessions) > 0 {
		last := page.Sessions[len(page.Sessions)-1]
		page.Next = fmt.Sprintf("%t:%s", last.Pinned, strconv.FormatFloat(last.UpdatedAt, 'f', -1, 64))
	}
	return page, nil
}

func (c *Client) UpdateSessionTitle(ctx context.Context, creds Credentials, sessionID, title string) error {
	if sessionID == "" || strings.TrimSpace(title) == "" {
		return fmt.Errorf("session id and title are required")
	}
	_, err := c.doJSON(ctx, creds, http.MethodPost, pathSessionTitle, nil, map[string]string{"chat_session_id": sessionID, "title": title}, nil)
	return err
}

func (c *Client) DeleteSessions(ctx context.Context, creds Credentials, sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	_, err := c.doJSON(ctx, creds, http.MethodPost, pathSessionDelete, nil, map[string][]string{"chat_session_ids": sessionIDs}, nil)
	return err
}

func (c *Client) SetSessionsPinned(ctx context.Context, creds Credentials, sessionIDs []string, pinned bool) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	_, err := c.doJSON(ctx, creds, http.MethodPost, pathSessionPin, nil, map[string]any{"chat_session_ids": sessionIDs, "pinned": pinned}, nil)
	return err
}

func (c *Client) SubmitFeedback(ctx context.Context, creds Credentials, feedback FeedbackRequest) error {
	if feedback.SessionID == "" || feedback.MessageID <= 0 {
		return fmt.Errorf("session id and positive message id are required")
	}
	if feedback.Type != "LIKE" && feedback.Type != "DISLIKE" {
		return fmt.Errorf("feedback type must be LIKE or DISLIKE")
	}
	body := map[string]any{"chat_session_id": feedback.SessionID, "message_id": feedback.MessageID, "feedback_type": feedback.Type}
	if feedback.Tag != "" {
		body["feedback_tag"] = feedback.Tag
	}
	if feedback.Description != "" {
		body["description"] = feedback.Description
	}
	_, err := c.doJSON(ctx, creds, http.MethodPost, pathFeedback, nil, body, nil)
	return err
}

func (c *Client) Complete(ctx context.Context, creds Credentials, req CompletionRequest, emit func(Event) error) (CompletionResult, error) {
	if strings.TrimSpace(req.Prompt) == "" && len(req.RefFileIDs) == 0 {
		return CompletionResult{}, errors.New("prompt or file reference is required")
	}
	if req.SessionID == "" && req.ModelType == "" {
		return CompletionResult{}, errors.New("model type is required to start a DeepSeek session")
	}
	sessionID := req.SessionID
	if sessionID == "" {
		session, err := c.CreateSession(ctx, creds)
		if err != nil {
			return CompletionResult{}, err
		}
		sessionID = session.ID
		if err := sendEvent(ctx, emit, Event{Kind: "session", Provider: ProviderDeepSeek, SessionID: sessionID}); err != nil {
			return CompletionResult{}, err
		}
	}
	proof, err := c.proofHeader(ctx, creds, pathCompletion)
	if err != nil {
		return CompletionResult{}, err
	}
	var parent any
	if req.ParentMessageID > 0 {
		parent = req.ParentMessageID
	}
	refFileIDs := req.RefFileIDs
	if refFileIDs == nil {
		refFileIDs = []string{}
	}
	body := map[string]any{
		"chat_session_id":   sessionID,
		"parent_message_id": parent,
		"prompt":            req.Prompt,
		"ref_file_ids":      refFileIDs,
		"thinking_enabled":  req.ThinkingEnabled,
		"search_enabled":    req.SearchEnabled,
		"action":            nil,
		"preempt":           req.Preempt,
	}
	if req.ParentMessageID == 0 {
		body["model_type"] = req.ModelType
	}
	source := req.Source
	if source == "" || source == "web" { // "web" was used by earlier Kino Android builds.
		source = "default"
	}
	if source != "default" && source != "landing" {
		return CompletionResult{}, fmt.Errorf("completion source must be default or landing")
	}
	body["source"] = source
	encoded, err := json.Marshal(body)
	if err != nil {
		return CompletionResult{}, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+pathCompletion, bytes.NewReader(encoded))
	if err != nil {
		return CompletionResult{}, err
	}
	h, err := c.headers(creds, "text/event-stream")
	if err != nil {
		return CompletionResult{}, err
	}
	h.Set("X-Ds-Pow-Response", proof)
	hreq.Header = h
	resp, err := c.http.Do(hreq)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("DeepSeek completion: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return CompletionResult{}, &APIError{HTTPStatus: resp.StatusCode, Message: fmt.Sprintf("DeepSeek completion returned HTTP %d: %s", resp.StatusCode, safeBody(raw))}
	}
	state := streamState{sessionID: sessionID, result: CompletionResult{SessionID: sessionID}}
	err = sse.Parse(resp.Body, func(frame sse.Frame) error {
		if frame.Data == "[DONE]" {
			return nil
		}
		if frame.Event == "ready" {
			var ready struct {
				RequestMessageID  int64 `json:"request_message_id"`
				ResponseMessageID int64 `json:"response_message_id"`
			}
			if json.Unmarshal([]byte(frame.Data), &ready) == nil {
				state.result.RequestMessageID = ready.RequestMessageID
				state.result.ResponseMessageID = ready.ResponseMessageID
				return sendEvent(ctx, emit, Event{Kind: "message_started", Provider: ProviderDeepSeek, SessionID: sessionID, RequestMessageID: ready.RequestMessageID, ResponseMessageID: ready.ResponseMessageID, ParentMessageID: req.ParentMessageID, ThinkingEnabled: boolPtr(req.ThinkingEnabled), SearchEnabled: boolPtr(req.SearchEnabled), Raw: json.RawMessage(frame.Data)})
			}
		}
		if frame.Data == "" {
			return nil
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(frame.Data), &payload); err != nil {
			return nil
		}
		return state.consume(ctx, emit, frame.Event, payload)
	})
	if err != nil {
		return state.result, err
	}
	if !state.closed {
		return state.result, io.ErrUnexpectedEOF
	}
	if state.result.ResponseMessageID == 0 {
		return state.result, errors.New("DeepSeek stream ended without a response message id")
	}
	if err := sendEvent(ctx, emit, Event{Kind: "complete", Provider: ProviderDeepSeek, SessionID: sessionID, RequestMessageID: state.result.RequestMessageID, ResponseMessageID: state.result.ResponseMessageID, AccumulatedTokenUsage: int64Ptr(state.result.AccumulatedTokenUsage), Status: state.result.Status, SearchTriggered: boolPtr(state.result.SearchTriggered)}); err != nil {
		return state.result, err
	}
	return state.result, nil
}

func sendEvent(ctx context.Context, emit func(Event) error, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if emit == nil {
		return nil
	}
	return emit(event)
}

func boolPtr(v bool) *bool    { return &v }
func int64Ptr(v int64) *int64 { return &v }
