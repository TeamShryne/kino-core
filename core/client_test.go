package kinocore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TeamShryne/kino-core/core/pow"
)

func testCredentials() Credentials {
	return Credentials{Provider: ProviderDeepSeek, Token: "token", WAFToken: "waf", HIFDliq: "dliq", HIFLeim: "leim"}
}

func TestLoadCapabilitiesDecodesCurrentFeatureShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathSettings || r.URL.Query().Get("did") != "device" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		w.Header().Set("X-Fetch-After-Sec", "300")
		settings := map[string]any{"normal_history_and_file_token_limit": map[string]any{"id": 7, "value": 890880}}
		if r.URL.Query().Get("scope") == "model" {
			settings["model_configs"] = map[string]any{"id": 1, "value": []any{map[string]any{
				"model_type": "default", "name": "Instant", "enabled": true, "switchable": true,
				"think_feature": map[string]any{}, "search_feature": map[string]any{},
				"file_feature": map[string]any{"token_limit": 890880, "vision": true},
			}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"biz_code": 0, "biz_data": map[string]any{"settings": settings, "version": 100}}})
	}))
	defer server.Close()
	c := NewClientWithHTTPClient(server.URL, server.Client())
	got, err := c.LoadCapabilities(context.Background(), testCredentials(), "device")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 1 || !got.Models[0].Available() || !got.Models[0].SupportsThinking() || !got.Models[0].SupportsSearch() || !got.Models[0].SupportsVision() {
		t.Fatalf("unexpected capabilities: %+v", got.Models)
	}
	if got.Settings.RefreshAfter.Seconds() != 300 {
		t.Fatalf("refresh after %s", got.Settings.RefreshAfter)
	}
	if got.TokenLimits.NormalHistoryAndFiles != 890880 {
		t.Fatalf("token limits: %+v", got.TokenLimits)
	}
}

func TestCompleteCreatesSessionSolvesPowAndEmitsDeltas(t *testing.T) {
	const salt = "test-salt"
	const expiry = int64(1785644803019)
	const answer = uint64(17)
	challenge := HashForTest(fmt.Sprintf("%s_%d_%d", salt, expiry, answer))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathCreateSession:
			_, _ = w.Write([]byte(`{"code":0,"data":{"biz_code":0,"biz_data":{"chat_session":{"id":"session-1"},"ttl_seconds":3600}}}`))
		case pathPowChallenge:
			_, _ = w.Write([]byte(fmt.Sprintf(`{"code":0,"data":{"biz_code":0,"biz_data":{"challenge":{"algorithm":"DeepSeekHashV1","challenge":"%s","salt":"%s","signature":"sig","difficulty":18,"expire_at":%d,"target_path":"%s"}}}}`, challenge, salt, expiry, pathCompletion)))
		case pathCompletion:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["chat_session_id"] != "session-1" || body["model_type"] != "default" {
				t.Errorf("bad completion body: %#v", body)
			}
			powHeader, err := base64.StdEncoding.DecodeString(r.Header.Get("X-Ds-Pow-Response"))
			if err != nil || !strings.Contains(string(powHeader), `"answer":17`) {
				t.Errorf("bad PoW header: %s", powHeader)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: ready\ndata: {\"request_message_id\":11,\"response_message_id\":12}\n\n" +
				"data: {\"p\":\"response/fragments\",\"o\":\"APPEND\",\"v\":[{\"id\":12,\"type\":\"RESPONSE\",\"content\":\"\"}]}\n\n" +
				"data: {\"p\":\"response/fragments/-1/content\",\"o\":\"APPEND\",\"v\":\"hello\"}\n\n" +
				"data: {\"p\":\"response/accumulated_token_usage\",\"o\":\"SET\",\"v\":23}\n\n" +
				"event: close\ndata: {}\n\n"))
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := NewClientWithHTTPClient(server.URL, server.Client())
	var events []Event
	got, err := c.Complete(context.Background(), testCredentials(), CompletionRequest{ModelType: "default", Prompt: "hi"}, func(e Event) error { events = append(events, e); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "session-1" || got.RequestMessageID != 11 || got.ResponseMessageID != 12 || got.AccumulatedTokenUsage != 23 {
		t.Fatalf("result: %+v", got)
	}
	foundText, foundUsage := false, false
	for _, event := range events {
		if event.Kind == "text_delta" && event.Content == "hello" {
			foundText = true
		}
		if event.Kind == "usage" && event.AccumulatedTokenUsage != nil && *event.AccumulatedTokenUsage == 23 {
			foundUsage = true
		}
	}
	if !foundText || !foundUsage {
		t.Fatalf("missing normalized events: %+v", events)
	}
}

func HashForTest(input string) string {
	h := pow.HashV1([]byte(input))
	return fmt.Sprintf("%x", h)
}
