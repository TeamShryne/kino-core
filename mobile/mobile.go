// Package mobile exports a small JSON-and-callback surface for gomobile.
// Android owns persisted state; each call receives all provider credentials,
// device metadata, and conversation handles it needs.
package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	kinocore "github.com/TeamShryne/kino-core/core"
)

// EventListener is implemented by Android. Events are compact JSON objects
// using the stable provider-neutral kinocore.Event shape.
type EventListener interface {
	OnEvent(eventJSON string)
	IsCancelled() bool
}

type MobileCore struct{ registry *kinocore.Registry }

func NewMobileCore() *MobileCore {
	client := kinocore.NewClient()
	return &MobileCore{registry: kinocore.NewRegistry(kinocore.NewDeepSeekAdapter(client))}
}

// Capabilities loads model-specific and general settings. deviceID should be
// a stable random ID generated and stored by Android, without user identity.
func (m *MobileCore) Capabilities(credentialsJSON, deviceID string) (string, error) {
	adapter, credentials, err := m.resolve(credentialsJSON)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	value, err := adapter.LoadCapabilities(ctx, credentials, deviceID)
	if err != nil {
		return "", err
	}
	return encode(value)
}

func (m *MobileCore) CreateSession(credentialsJSON string) (string, error) {
	adapter, credentials, err := m.resolve(credentialsJSON)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	value, err := adapter.CreateSession(ctx, credentials)
	if err != nil {
		return "", err
	}
	return encode(value)
}

func (m *MobileCore) History(credentialsJSON, sessionID string) (string, error) {
	adapter, credentials, err := m.resolve(credentialsJSON)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	value, err := adapter.History(ctx, credentials, sessionID)
	if err != nil {
		return "", err
	}
	return encode(value)
}

func (m *MobileCore) Sessions(credentialsJSON, requestJSON string) (string, error) {
	adapter, credentials, err := m.resolve(credentialsJSON)
	if err != nil {
		return "", err
	}
	var request kinocore.SessionPageRequest
	if err := json.Unmarshal([]byte(requestJSON), &request); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	value, err := adapter.ListSessions(ctx, credentials, request)
	if err != nil {
		return "", err
	}
	return encode(value)
}

func (m *MobileCore) UpdateSessionTitle(credentialsJSON, sessionID, title string) error {
	adapter, credentials, err := m.resolve(credentialsJSON)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return adapter.UpdateSessionTitle(ctx, credentials, sessionID, title)
}

func (m *MobileCore) DeleteSessions(credentialsJSON, sessionIDsJSON string) error {
	adapter, credentials, err := m.resolve(credentialsJSON)
	if err != nil {
		return err
	}
	var ids []string
	if err := json.Unmarshal([]byte(sessionIDsJSON), &ids); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return adapter.DeleteSessions(ctx, credentials, ids)
}

func (m *MobileCore) SetSessionsPinned(credentialsJSON, sessionIDsJSON string, pinned bool) error {
	adapter, credentials, err := m.resolve(credentialsJSON)
	if err != nil {
		return err
	}
	var ids []string
	if err := json.Unmarshal([]byte(sessionIDsJSON), &ids); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return adapter.SetSessionsPinned(ctx, credentials, ids, pinned)
}

func (m *MobileCore) SubmitFeedback(credentialsJSON, feedbackJSON string) error {
	adapter, credentials, err := m.resolve(credentialsJSON)
	if err != nil {
		return err
	}
	var feedback kinocore.FeedbackRequest
	if err := json.Unmarshal([]byte(feedbackJSON), &feedback); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return adapter.SubmitFeedback(ctx, credentials, feedback)
}

func (m *MobileCore) UploadFile(credentialsJSON, localPath string) (string, error) {
	adapter, credentials, err := m.resolve(credentialsJSON)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	value, err := adapter.UploadFile(ctx, credentials, localPath)
	if err != nil {
		return "", err
	}
	return encode(value)
}

func (m *MobileCore) FetchFiles(credentialsJSON, fileIDsJSON string) (string, error) {
	adapter, credentials, err := m.resolve(credentialsJSON)
	if err != nil {
		return "", err
	}
	var ids []string
	if err := json.Unmarshal([]byte(fileIDsJSON), &ids); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	value, err := adapter.FetchFiles(ctx, credentials, ids)
	if err != nil {
		return "", err
	}
	return encode(value)
}

// Complete blocks its calling worker thread while emitting JSON events to
// listener. Android must call this off the main thread and persist returned
// session/message IDs. Cancellation is polled without a Go request registry.
func (m *MobileCore) Complete(credentialsJSON, requestJSON string, listener EventListener) (string, error) {
	adapter, credentials, err := m.resolve(credentialsJSON)
	if err != nil {
		return "", err
	}
	var request kinocore.CompletionRequest
	if err := json.Unmarshal([]byte(requestJSON), &request); err != nil {
		return "", err
	}
	timeout := time.Duration(request.TimeoutMillis) * time.Millisecond
	if timeout <= 0 || timeout > 10*time.Minute {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	watchDone := make(chan struct{})
	active := &activeRequest{}
	if listener != nil {
		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-watchDone:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					if listenerCancelled(listener) {
						cancel()
						return
					}
				}
			}
		}()
	}
	defer close(watchDone)
	result, err := adapter.Complete(ctx, credentials, request, func(event kinocore.Event) error {
		active.update(event)
		if listener == nil {
			return nil
		}
		if listenerCancelled(listener) {
			cancel()
			return context.Canceled
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if callErr := safeOnEvent(listener, string(encoded)); callErr != nil {
			cancel()
			return callErr
		}
		return ctx.Err()
	})
	if err != nil {
		if ctx.Err() != nil {
			sessionID, messageID := active.snapshot()
			if sessionID != "" && messageID > 0 {
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = adapter.Stop(stopCtx, credentials, sessionID, messageID)
				stopCancel()
			}
		}
		return "", err
	}
	return encode(result)
}

type activeRequest struct {
	mu        sync.Mutex
	sessionID string
	messageID int64
}

func (r *activeRequest) update(event kinocore.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.SessionID != "" {
		r.sessionID = event.SessionID
	}
	if event.ResponseMessageID > 0 {
		r.messageID = event.ResponseMessageID
	}
}

func (r *activeRequest) snapshot() (string, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionID, r.messageID
}

func (m *MobileCore) resolve(credentialsJSON string) (kinocore.ProviderAdapter, json.RawMessage, error) {
	var identity struct {
		Provider kinocore.Provider `json:"provider"`
	}
	if err := json.Unmarshal([]byte(credentialsJSON), &identity); err != nil {
		return nil, nil, err
	}
	if identity.Provider == "" {
		identity.Provider = kinocore.ProviderDeepSeek
	}
	adapter, ok := m.registry.Get(identity.Provider)
	if !ok {
		return nil, nil, errors.New("unsupported provider: " + string(identity.Provider))
	}
	return adapter, json.RawMessage(credentialsJSON), nil
}

func (m *MobileCore) Close() error {
	var first error
	for _, name := range m.registry.Names() {
		if adapter, ok := m.registry.Get(name); ok {
			if err := adapter.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func encode(value any) (string, error) { data, err := json.Marshal(value); return string(data), err }

func listenerCancelled(listener EventListener) (cancelled bool) {
	defer func() {
		if recover() != nil {
			cancelled = true
		}
	}()
	return listener.IsCancelled()
}

func safeOnEvent(listener EventListener, value string) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("Android event listener failed")
		}
	}()
	listener.OnEvent(value)
	return nil
}
