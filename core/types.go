package kinocore

import (
	"encoding/json"
	"time"
)

// Provider names a chat backend. Android stores this stable identifier beside
// each conversation so provider-specific metadata can remain isolated.
type Provider string

const ProviderDeepSeek Provider = "deepseek"

type Credentials struct {
	Provider       Provider `json:"provider"`
	Token          string   `json:"token"`
	WAFToken       string   `json:"waf_token"`
	HIFDliq        string   `json:"hif_dliq"`
	HIFLeim        string   `json:"hif_leim"`
	SessionID      string   `json:"ds_session_id,omitempty"`
	UserAgent      string   `json:"user_agent,omitempty"`
	AppVersion     string   `json:"app_version,omitempty"`
	Locale         string   `json:"locale,omitempty"`
	TimezoneOffset string   `json:"timezone_offset,omitempty"`
}

type ModelConfig struct {
	ModelType           string          `json:"model_type"`
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	WelcomeMessage      string          `json:"welcome_msg"`
	IsDefault           bool            `json:"is_default"`
	Enabled             bool            `json:"enabled"`
	Switchable          bool            `json:"switchable"`
	InputCharacterLimit int64           `json:"input_character_limit"`
	ThinkFeature        json.RawMessage `json:"think_feature"`
	SearchFeature       json.RawMessage `json:"search_feature"`
	FileFeature         *FileFeature    `json:"file_feature"`
	RegenerateOptions   json.RawMessage `json:"regenerate_options"`
	Raw                 json.RawMessage `json:"-"`
}

type FileFeature struct {
	TokenLimit             int64    `json:"token_limit"`
	TokenLimitWithThinking int64    `json:"token_limit_with_thinking"`
	MaxInputFileCount      int      `json:"max_input_file_count"`
	MaxUploadFileSize      int64    `json:"max_upload_file_size"`
	SupportedExtensions    []string `json:"support_file_exts"`
	ConflictWithSearch     bool     `json:"conflict_with_search"`
	Vision                 bool     `json:"vision"`
	EnableThumbnail        bool     `json:"enable_thumbnail"`
}

type FileReference struct {
	ID          string `json:"id,omitempty"`
	FileName    string `json:"file_name,omitempty"`
	FileSize    int64  `json:"file_size,omitempty"`
	TokenUsage  int64  `json:"token_usage,omitempty"`
	Status      string `json:"status,omitempty"`
	AuditResult string `json:"audit_result,omitempty"`
	IsImage     bool   `json:"is_image,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

func (m ModelConfig) SupportsThinking() bool { return featurePresent(m.ThinkFeature) }
func (m ModelConfig) SupportsSearch() bool   { return featurePresent(m.SearchFeature) }
func (m ModelConfig) SupportsFiles() bool    { return m.FileFeature != nil }
func (m ModelConfig) SupportsVision() bool   { return m.FileFeature != nil && m.FileFeature.Vision }
func (m ModelConfig) Available() bool        { return m.Enabled && m.Switchable }

func featurePresent(raw json.RawMessage) bool {
	return len(raw) != 0 && string(raw) != "null" && string(raw) != "false"
}

type SettingsSnapshot struct {
	Scope        string                     `json:"scope"`
	Version      int64                      `json:"version"`
	RefreshAfter time.Duration              `json:"refresh_after"`
	Values       map[string]json.RawMessage `json:"values"`
	ModelConfigs []ModelConfig              `json:"model_configs,omitempty"`
}

type Capabilities struct {
	Provider      Provider         `json:"provider"`
	DeviceID      string           `json:"device_id"`
	Version       int64            `json:"version"`
	Models        []ModelConfig    `json:"models"`
	ModelSettings SettingsSnapshot `json:"model_settings"`
	Settings      SettingsSnapshot `json:"settings"`
	TokenLimits   TokenLimits      `json:"token_limits"`
}

type TokenLimits struct {
	NormalHistoryAndFiles   int64 `json:"normal_history_and_files,omitempty"`
	ThinkingHistoryAndFiles int64 `json:"thinking_history_and_files,omitempty"`
}

type ChatSession struct {
	ID         string `json:"id"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty"`
}

type SearchResult struct {
	URL      string          `json:"url,omitempty"`
	Title    string          `json:"title,omitempty"`
	SiteName string          `json:"site_name,omitempty"`
	Snippet  string          `json:"snippet,omitempty"`
	Raw      json.RawMessage `json:"raw,omitempty"`
}

type Fragment struct {
	ID      int64           `json:"id,omitempty"`
	Type    string          `json:"type"`
	Status  string          `json:"status,omitempty"`
	Content string          `json:"content,omitempty"`
	Queries []SearchQuery   `json:"queries,omitempty"`
	Results []SearchResult  `json:"results,omitempty"`
	Files   []FileReference `json:"files,omitempty"`
	Raw     json.RawMessage `json:"raw,omitempty"`
}

type SearchQuery struct {
	Query string `json:"query"`
}

// Event is the provider-neutral stream contract sent to Android. Unknown
// provider data remains available in Raw for forward-compatible rendering.
type Event struct {
	Kind                  string          `json:"kind"`
	Provider              Provider        `json:"provider"`
	SessionID             string          `json:"session_id,omitempty"`
	RequestMessageID      int64           `json:"request_message_id,omitempty"`
	ResponseMessageID     int64           `json:"response_message_id,omitempty"`
	ParentMessageID       int64           `json:"parent_message_id,omitempty"`
	Fragment              *Fragment       `json:"fragment,omitempty"`
	FragmentType          string          `json:"fragment_type,omitempty"`
	Content               string          `json:"content,omitempty"`
	Path                  string          `json:"path,omitempty"`
	Operation             string          `json:"operation,omitempty"`
	AccumulatedTokenUsage *int64          `json:"accumulated_token_usage,omitempty"`
	ThinkingEnabled       *bool           `json:"thinking_enabled,omitempty"`
	SearchEnabled         *bool           `json:"search_enabled,omitempty"`
	SearchTriggered       *bool           `json:"search_triggered,omitempty"`
	Status                string          `json:"status,omitempty"`
	Raw                   json.RawMessage `json:"raw,omitempty"`
}

type CompletionRequest struct {
	DeviceID        string   `json:"device_id"`
	TimeoutMillis   int64    `json:"timeout_ms,omitempty"`
	SessionID       string   `json:"session_id,omitempty"`
	ParentMessageID int64    `json:"parent_message_id,omitempty"`
	ModelType       string   `json:"model_type"`
	Prompt          string   `json:"prompt"`
	RefFileIDs      []string `json:"ref_file_ids,omitempty"`
	ThinkingEnabled bool     `json:"thinking_enabled"`
	SearchEnabled   bool     `json:"search_enabled"`
	Source          string   `json:"source,omitempty"`
	Preempt         bool     `json:"preempt,omitempty"`
}

type CompletionResult struct {
	SessionID             string `json:"session_id"`
	RequestMessageID      int64  `json:"request_message_id"`
	ResponseMessageID     int64  `json:"response_message_id"`
	AccumulatedTokenUsage int64  `json:"accumulated_token_usage"`
	Status                string `json:"status,omitempty"`
	SearchTriggered       bool   `json:"search_triggered"`
}

type Message struct {
	ID                    int64           `json:"message_id"`
	ParentID              int64           `json:"parent_id"`
	Role                  string          `json:"role"`
	Status                string          `json:"status"`
	ThinkingEnabled       bool            `json:"thinking_enabled"`
	SearchEnabled         bool            `json:"search_enabled"`
	SearchTriggered       bool            `json:"search_triggered"`
	AccumulatedTokenUsage int64           `json:"accumulated_token_usage"`
	InsertedAt            float64         `json:"inserted_at"`
	Fragments             []Fragment      `json:"fragments"`
	Raw                   json.RawMessage `json:"-"`
}

type History struct {
	Messages []Message       `json:"messages"`
	Raw      json.RawMessage `json:"-"`
}

type SessionPageRequest struct {
	Limit     int     `json:"limit,omitempty"`
	Mode      string  `json:"mode,omitempty"` // "lte" or "gte"
	Pinned    bool    `json:"pinned,omitempty"`
	UpdatedAt float64 `json:"updated_at,omitempty"`
}

type SessionSummary struct {
	ID        string          `json:"id"`
	Title     string          `json:"title,omitempty"`
	Pinned    bool            `json:"pinned,omitempty"`
	UpdatedAt float64         `json:"updated_at,omitempty"`
	ModelType string          `json:"model_type,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}

type SessionPage struct {
	Sessions []SessionSummary `json:"sessions"`
	Next     string           `json:"next,omitempty"`
	HasMore  bool             `json:"has_more"`
	Raw      json.RawMessage  `json:"raw,omitempty"`
}

type FeedbackRequest struct {
	SessionID   string `json:"session_id"`
	MessageID   int64  `json:"message_id"`
	Type        string `json:"type"` // "LIKE" or "DISLIKE"
	Tag         string `json:"tag,omitempty"`
	Description string `json:"description,omitempty"`
}
