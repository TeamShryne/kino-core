package kinocore

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type streamState struct {
	sessionID           string
	lastFragmentType    string
	fragmentTypeByPath  map[string]string
	fragmentTypeByIndex map[string]string
	nextFragmentIndex   int
	result              CompletionResult
}

func (s *streamState) consume(ctx context.Context, emit func(Event) error, eventName string, payload map[string]any) error {
	if eventName != "" && eventName != "message" {
		if err := sendEvent(ctx, emit, Event{Kind: "server_event", Provider: ProviderDeepSeek, SessionID: s.sessionID, Status: eventName}); err != nil {
			return err
		}
	}
	if raw, ok := payload["response_message_id"].(float64); ok {
		s.result.ResponseMessageID = int64(raw)
	}
	if raw, ok := payload["request_message_id"].(float64); ok {
		s.result.RequestMessageID = int64(raw)
	}
	if p, ok := payload["p"].(string); ok {
		return s.consumeOperation(ctx, emit, p, stringValue(payload["o"]), payload["v"], payload)
	}
	// DeepSeek also sends plain v-string delta frames without a path or
	// operation. Kino Server forwards these as content; ignoring them here
	// leaves only the initial fragment payload visible in Android.
	if content, ok := payload["v"].(string); ok {
		fragmentType := s.lastFragmentType
		if fragmentType == "" {
			fragmentType = "RESPONSE"
		}
		kind := "text_delta"
		if fragmentType == "THINK" {
			kind = "thinking_delta"
		} else if fragmentType != "RESPONSE" {
			kind = "fragment_delta"
		}
		return sendEvent(ctx, emit, Event{Kind: kind, Provider: ProviderDeepSeek, SessionID: s.sessionID, ResponseMessageID: s.result.ResponseMessageID, FragmentType: fragmentType, Content: content})
	}
	if response := responseObject(payload); response != nil {
		if raw, ok := numberValue(response["message_id"]); ok {
			s.result.ResponseMessageID = raw
		}
		if raw, ok := numberValue(response["accumulated_token_usage"]); ok {
			s.result.AccumulatedTokenUsage = raw
		}
		if status := stringValue(response["status"]); status != "" {
			s.result.Status = status
		}
		if triggered, ok := response["search_triggered"].(bool); ok {
			s.result.SearchTriggered = triggered
		}
		if enabled, ok := response["thinking_enabled"].(bool); ok {
			if err := sendEvent(ctx, emit, Event{Kind: "message_state", Provider: ProviderDeepSeek, SessionID: s.sessionID, ResponseMessageID: s.result.ResponseMessageID, ThinkingEnabled: boolPtr(enabled)}); err != nil {
				return err
			}
		}
		if enabled, ok := response["search_enabled"].(bool); ok {
			if err := sendEvent(ctx, emit, Event{Kind: "message_state", Provider: ProviderDeepSeek, SessionID: s.sessionID, ResponseMessageID: s.result.ResponseMessageID, SearchEnabled: boolPtr(enabled), SearchTriggered: boolPtr(s.result.SearchTriggered)}); err != nil {
				return err
			}
		}
		if raw, ok := response["accumulated_token_usage"]; ok {
			if value, valid := numberValue(raw); valid {
				s.result.AccumulatedTokenUsage = value
				if err := sendEvent(ctx, emit, Event{Kind: "usage", Provider: ProviderDeepSeek, SessionID: s.sessionID, ResponseMessageID: s.result.ResponseMessageID, AccumulatedTokenUsage: int64Ptr(value)}); err != nil {
					return err
				}
			}
		}
		if raw, ok := response["fragments"].([]any); ok {
			for index, value := range raw {
				if fragment, ok := decodeFragment(value); ok {
					if err := s.sendFragmentAt(ctx, emit, fragment, index); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func responseObject(payload map[string]any) map[string]any {
	if value, ok := payload["response"].(map[string]any); ok {
		return value
	}
	if outer, ok := payload["v"].(map[string]any); ok {
		if value, ok := outer["response"].(map[string]any); ok {
			return value
		}
	}
	return nil
}

func (s *streamState) consumeOperation(ctx context.Context, emit func(Event) error, path, operation string, value any, payload map[string]any) error {
	if operation == "BATCH" {
		if operations, ok := value.([]any); ok {
			for _, item := range operations {
				operationMap, ok := item.(map[string]any)
				if !ok {
					continue
				}
				nestedPath := stringValue(operationMap["p"])
				if nestedPath == "" {
					nestedPath = path
				}
				if err := s.consumeOperation(ctx, emit, nestedPath, stringValue(operationMap["o"]), operationMap["v"], operationMap); err != nil {
					return err
				}
			}
			return nil
		}
	}
	raw, _ := json.Marshal(value)
	switch {
	case path == "response/fragments":
		if fragments, ok := value.([]any); ok {
			for _, item := range fragments {
				if fragment, valid := decodeFragment(item); valid {
					if err := s.sendFragmentAt(ctx, emit, fragment, s.nextFragmentIndex); err != nil {
						return err
					}
				}
			}
		}
	case strings.HasSuffix(path, "/content"):
		content, _ := value.(string)
		fragmentType := s.fragmentTypeForPath(path)
		kind := "fragment_delta"
		if fragmentType == "THINK" {
			kind = "thinking_delta"
		}
		if fragmentType == "RESPONSE" {
			kind = "text_delta"
		}
		return sendEvent(ctx, emit, Event{Kind: kind, Provider: ProviderDeepSeek, SessionID: s.sessionID, ResponseMessageID: s.result.ResponseMessageID, FragmentType: fragmentType, Content: content, Path: path, Operation: operation, Raw: raw})
	case strings.HasSuffix(path, "/results") || strings.HasSuffix(path, "/queries"):
		return sendEvent(ctx, emit, Event{Kind: "search_update", Provider: ProviderDeepSeek, SessionID: s.sessionID, ResponseMessageID: s.result.ResponseMessageID, FragmentType: "SEARCH", Path: path, Operation: operation, Raw: raw})
	case strings.HasSuffix(path, "/accumulated_token_usage"):
		if n, ok := numberValue(value); ok {
			s.result.AccumulatedTokenUsage = n
			return sendEvent(ctx, emit, Event{Kind: "usage", Provider: ProviderDeepSeek, SessionID: s.sessionID, ResponseMessageID: s.result.ResponseMessageID, AccumulatedTokenUsage: int64Ptr(n), Path: path, Operation: operation})
		}
	case strings.HasSuffix(path, "/search_triggered"):
		if triggered, ok := value.(bool); ok {
			s.result.SearchTriggered = triggered
		}
	case strings.HasSuffix(path, "/status"):
		if status, ok := value.(string); ok {
			s.result.Status = status
			return sendEvent(ctx, emit, Event{Kind: "status", Provider: ProviderDeepSeek, SessionID: s.sessionID, ResponseMessageID: s.result.ResponseMessageID, Status: status, Path: path, Operation: operation})
		}
	}
	return sendEvent(ctx, emit, Event{Kind: "provider_update", Provider: ProviderDeepSeek, SessionID: s.sessionID, ResponseMessageID: s.result.ResponseMessageID, Path: path, Operation: operation, Raw: raw})
}

func (s *streamState) fragmentTypeForPath(path string) string {
	if s.fragmentTypeByPath == nil {
		s.fragmentTypeByPath = make(map[string]string)
	}
	parts := strings.Split(path, "/")
	if len(parts) >= 3 {
		if parts[2] == "-1" {
			return s.lastFragmentType
		}
		if value := s.fragmentTypeByIndex[parts[2]]; value != "" {
			return value
		}
		if value := s.fragmentTypeByPath[parts[2]]; value != "" {
			return value
		}
	}
	return s.lastFragmentType
}

func (s *streamState) sendFragmentAt(ctx context.Context, emit func(Event) error, fragment Fragment, index int) error {
	if index >= s.nextFragmentIndex {
		s.nextFragmentIndex = index + 1
	}
	if fragment.Type != "" {
		s.lastFragmentType = fragment.Type
		if s.fragmentTypeByPath == nil {
			s.fragmentTypeByPath = make(map[string]string)
		}
		if s.fragmentTypeByIndex == nil {
			s.fragmentTypeByIndex = make(map[string]string)
		}
		s.fragmentTypeByPath[strconv.FormatInt(fragment.ID, 10)] = fragment.Type
		s.fragmentTypeByIndex[strconv.Itoa(index)] = fragment.Type
	}
	kind := "fragment"
	if fragment.Type == "SEARCH" {
		kind = "search_fragment"
	}
	if fragment.Type == "THINK" {
		kind = "thinking_fragment"
	}
	if err := sendEvent(ctx, emit, Event{Kind: kind, Provider: ProviderDeepSeek, SessionID: s.sessionID, ResponseMessageID: s.result.ResponseMessageID, FragmentType: fragment.Type, Fragment: &fragment}); err != nil {
		return err
	}
	if fragment.Content != "" {
		deltaKind := "fragment_delta"
		if fragment.Type == "RESPONSE" {
			deltaKind = "text_delta"
		}
		if fragment.Type == "THINK" {
			deltaKind = "thinking_delta"
		}
		return sendEvent(ctx, emit, Event{Kind: deltaKind, Provider: ProviderDeepSeek, SessionID: s.sessionID, ResponseMessageID: s.result.ResponseMessageID, FragmentType: fragment.Type, Content: fragment.Content})
	}
	return nil
}

func decodeFragment(value any) (Fragment, bool) {
	var f Fragment
	raw, err := json.Marshal(value)
	if err != nil {
		return f, false
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return f, false
	}
	f.Raw = raw
	return f, true
}

func stringValue(v any) string { s, _ := v.(string); return s }

func numberValue(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

func (c *Client) Stop(ctx context.Context, creds Credentials, sessionID string, messageID int64) error {
	if sessionID == "" || messageID <= 0 {
		return fmt.Errorf("session id and positive message id are required")
	}
	_, err := c.doJSON(ctx, creds, "POST", "/api/v0/chat/stop_stream", nil, map[string]any{"chat_session_id": sessionID, "message_id": messageID}, nil)
	return err
}
