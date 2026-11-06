package kinocore

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveDeepSeek uses a caller-provided Kino Server account store. It is
// skipped by default and must never run in CI with committed credentials.
func TestLiveDeepSeek(t *testing.T) {
	path := os.Getenv("KINO_CORE_CREDENTIALS_FILE")
	if path == "" {
		t.Skip("set KINO_CORE_CREDENTIALS_FILE to opt into live DeepSeek integration")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var store struct {
		Accounts map[string]struct {
			Provider    string      `json:"provider"`
			Credentials Credentials `json:"credentials"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(data, &store); err != nil {
		t.Fatal(err)
	}
	var creds Credentials
	for _, account := range store.Accounts {
		if account.Provider == "deepseek" {
			creds = account.Credentials
			break
		}
	}
	if creds.Token == "" {
		t.Fatal("DeepSeek account not found in credential store")
	}
	creds.Provider = ProviderDeepSeek

	c := NewClient()
	defer c.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	deviceID := "kino-core-live-test"
	caps, err := c.LoadCapabilities(ctx, creds, deviceID)
	if err != nil {
		t.Fatalf("load capabilities: %v", err)
	}
	if len(caps.Models) == 0 {
		t.Fatal("DeepSeek returned no model configurations")
	}
	t.Logf("loaded %d model configs at settings version %d", len(caps.Models), caps.Version)

	var seen []Event
	var answer strings.Builder
	first, err := c.Complete(ctx, creds, CompletionRequest{
		DeviceID: deviceID, ModelType: "default", Prompt: "Reply with exactly: KINO CORE LIVE OK",
	}, func(event Event) error {
		seen = append(seen, event)
		if event.Kind == "text_delta" {
			answer.WriteString(event.Content)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("direct completion: %v", err)
	}
	if first.SessionID == "" || first.ResponseMessageID <= 0 {
		t.Fatalf("missing conversation handles: %+v", first)
	}
	if len(seen) == 0 {
		t.Fatal("stream returned no events")
	}
	if strings.TrimSpace(answer.String()) == "" {
		t.Fatal("direct completion emitted no assistant text")
	}

	history, err := c.History(ctx, creds, first.SessionID)
	if err != nil {
		t.Fatalf("fetch history: %v", err)
	}
	if len(history.Messages) < 2 {
		t.Fatalf("history has %d messages", len(history.Messages))
	}
	if history.Messages[len(history.Messages)-1].AccumulatedTokenUsage <= 0 {
		t.Fatalf("assistant history did not return token context usage: %+v", history.Messages[len(history.Messages)-1])
	}
	t.Logf("history returned %d messages; final context usage %d", len(history.Messages), history.Messages[len(history.Messages)-1].AccumulatedTokenUsage)

	var thought bool
	second, err := c.Complete(ctx, creds, CompletionRequest{
		DeviceID: deviceID, SessionID: first.SessionID, ParentMessageID: first.ResponseMessageID,
		Prompt: "What exact phrase did I ask you to reply with? Answer briefly.", ThinkingEnabled: true,
	}, func(event Event) error {
		if event.Kind == "thinking_delta" || event.Kind == "thinking_fragment" {
			thought = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("thinking follow-up: %v", err)
	}
	if second.ResponseMessageID <= 0 {
		t.Fatalf("thinking follow-up returned no response id: %+v", second)
	}
	t.Logf("thinking follow-up returned response id; streamed thinking=%t", thought)

	var searchObserved bool
	_, err = c.Complete(ctx, creds, CompletionRequest{
		DeviceID: deviceID, SessionID: first.SessionID, ParentMessageID: second.ResponseMessageID,
		Prompt: "Search the web for the official DeepSeek API documentation hostname.", SearchEnabled: true,
	}, func(event Event) error {
		if event.Kind == "search_fragment" || event.Kind == "search_update" {
			searchObserved = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("search follow-up: %v", err)
	}
	t.Logf("search follow-up completed; search fragment observed=%t", searchObserved)
}
