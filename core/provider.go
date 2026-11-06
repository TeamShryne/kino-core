package kinocore

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// ProviderAdapter is the shared boundary for all providers. An adapter receives
// credentials and conversation state from its caller and returns data/events;
// it never owns chat history or persists account state.
type ProviderAdapter interface {
	Name() Provider
	Close() error
	LoadCapabilities(context.Context, json.RawMessage, string) (Capabilities, error)
	CreateSession(context.Context, json.RawMessage) (ChatSession, error)
	Complete(context.Context, json.RawMessage, CompletionRequest, func(Event) error) (CompletionResult, error)
	History(context.Context, json.RawMessage, string) (History, error)
	ListSessions(context.Context, json.RawMessage, SessionPageRequest) (SessionPage, error)
	UpdateSessionTitle(context.Context, json.RawMessage, string, string) error
	DeleteSessions(context.Context, json.RawMessage, []string) error
	SetSessionsPinned(context.Context, json.RawMessage, []string, bool) error
	SubmitFeedback(context.Context, json.RawMessage, FeedbackRequest) error
	Stop(context.Context, json.RawMessage, string, int64) error
	UploadFile(context.Context, json.RawMessage, string) (FileReference, error)
	FetchFiles(context.Context, json.RawMessage, []string) ([]FileReference, error)
}

// Registry is immutable after startup by convention. It contains provider
// implementations only, not per-user sessions or chat data.
type Registry struct {
	mu        sync.RWMutex
	providers map[Provider]ProviderAdapter
}

func NewRegistry(adapters ...ProviderAdapter) *Registry {
	r := &Registry{providers: make(map[Provider]ProviderAdapter, len(adapters))}
	for _, adapter := range adapters {
		if adapter != nil {
			r.providers[adapter.Name()] = adapter
		}
	}
	return r
}

func (r *Registry) Register(adapter ProviderAdapter) error {
	if adapter == nil || adapter.Name() == "" {
		return fmt.Errorf("provider adapter and name are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[adapter.Name()]; exists {
		return fmt.Errorf("provider %q is already registered", adapter.Name())
	}
	r.providers[adapter.Name()] = adapter
	return nil
}

func (r *Registry) Get(name Provider) (ProviderAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

func (r *Registry) Names() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]Provider, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sortProviders(names)
	return names
}

func sortProviders(providers []Provider) {
	for i := 1; i < len(providers); i++ {
		for j := i; j > 0 && providers[j] < providers[j-1]; j-- {
			providers[j], providers[j-1] = providers[j-1], providers[j]
		}
	}
}

type DeepSeekAdapter struct{ client *Client }

func NewDeepSeekAdapter(client *Client) *DeepSeekAdapter {
	if client == nil {
		client = NewClient()
	}
	return &DeepSeekAdapter{client: client}
}

func (a *DeepSeekAdapter) Name() Provider { return ProviderDeepSeek }
func (a *DeepSeekAdapter) Close() error   { a.client.CloseIdleConnections(); return nil }

func decodeCredentials(raw json.RawMessage) (Credentials, error) {
	var creds Credentials
	if err := json.Unmarshal(raw, &creds); err != nil {
		return Credentials{}, fmt.Errorf("decode %s credentials: %w", ProviderDeepSeek, err)
	}
	if creds.Provider == "" {
		creds.Provider = ProviderDeepSeek
	}
	if creds.Provider != ProviderDeepSeek {
		return Credentials{}, fmt.Errorf("credentials provider is %q, want %q", creds.Provider, ProviderDeepSeek)
	}
	return creds, nil
}

func (a *DeepSeekAdapter) LoadCapabilities(ctx context.Context, raw json.RawMessage, deviceID string) (Capabilities, error) {
	c, err := decodeCredentials(raw)
	if err != nil {
		return Capabilities{}, err
	}
	return a.client.LoadCapabilities(ctx, c, deviceID)
}

func (a *DeepSeekAdapter) CreateSession(ctx context.Context, raw json.RawMessage) (ChatSession, error) {
	c, err := decodeCredentials(raw)
	if err != nil {
		return ChatSession{}, err
	}
	return a.client.CreateSession(ctx, c)
}

func (a *DeepSeekAdapter) Complete(ctx context.Context, raw json.RawMessage, req CompletionRequest, emit func(Event) error) (CompletionResult, error) {
	c, err := decodeCredentials(raw)
	if err != nil {
		return CompletionResult{}, err
	}
	return a.client.Complete(ctx, c, req, emit)
}

func (a *DeepSeekAdapter) History(ctx context.Context, raw json.RawMessage, sessionID string) (History, error) {
	c, err := decodeCredentials(raw)
	if err != nil {
		return History{}, err
	}
	return a.client.History(ctx, c, sessionID)
}

func (a *DeepSeekAdapter) ListSessions(ctx context.Context, raw json.RawMessage, request SessionPageRequest) (SessionPage, error) {
	c, err := decodeCredentials(raw)
	if err != nil {
		return SessionPage{}, err
	}
	return a.client.ListSessions(ctx, c, request)
}

func (a *DeepSeekAdapter) UpdateSessionTitle(ctx context.Context, raw json.RawMessage, sessionID, title string) error {
	c, err := decodeCredentials(raw)
	if err != nil {
		return err
	}
	return a.client.UpdateSessionTitle(ctx, c, sessionID, title)
}

func (a *DeepSeekAdapter) DeleteSessions(ctx context.Context, raw json.RawMessage, ids []string) error {
	c, err := decodeCredentials(raw)
	if err != nil {
		return err
	}
	return a.client.DeleteSessions(ctx, c, ids)
}

func (a *DeepSeekAdapter) SetSessionsPinned(ctx context.Context, raw json.RawMessage, ids []string, pinned bool) error {
	c, err := decodeCredentials(raw)
	if err != nil {
		return err
	}
	return a.client.SetSessionsPinned(ctx, c, ids, pinned)
}

func (a *DeepSeekAdapter) SubmitFeedback(ctx context.Context, raw json.RawMessage, feedback FeedbackRequest) error {
	c, err := decodeCredentials(raw)
	if err != nil {
		return err
	}
	return a.client.SubmitFeedback(ctx, c, feedback)
}

func (a *DeepSeekAdapter) Stop(ctx context.Context, raw json.RawMessage, sessionID string, messageID int64) error {
	c, err := decodeCredentials(raw)
	if err != nil {
		return err
	}
	return a.client.Stop(ctx, c, sessionID, messageID)
}

func (a *DeepSeekAdapter) UploadFile(ctx context.Context, raw json.RawMessage, path string) (FileReference, error) {
	c, err := decodeCredentials(raw)
	if err != nil {
		return FileReference{}, err
	}
	return a.client.UploadFile(ctx, c, path)
}

func (a *DeepSeekAdapter) FetchFiles(ctx context.Context, raw json.RawMessage, ids []string) ([]FileReference, error) {
	c, err := decodeCredentials(raw)
	if err != nil {
		return nil, err
	}
	return a.client.FetchFiles(ctx, c, ids)
}
