package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
)

// ── Errors ──────────────────────────────────────────────────────────────────

var (
	ErrProviderNotFound   = errors.New("LLM provider not found")
	ErrRateLimited        = errors.New("LLM provider rate limited")
	ErrContextTooLarge    = errors.New("context exceeds provider limit")
	ErrProviderError      = errors.New("LLM provider returned an error")
	ErrStreamNotSupported = errors.New("provider does not support streaming")
)

// ── Request / Response ──────────────────────────────────────────────────────

// Role identifies the role in a chat message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a single message in a conversation.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`         // function name when Role == RoleTool
	ToolCallID string     `json:"tool_call_id,omitempty"` // correlates tool result to ToolCall.ID
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // set by assistant when requesting tool invocations
}

// ── Tool Calling Types ──────────────────────────────────────────────────────

// ToolCall represents a single tool invocation requested by the LLM.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // always "function"
	Function ToolCallFunc `json:"function"`
}

// ToolCallFunc is the function name and JSON-encoded arguments string.
type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDef describes a tool the LLM may invoke (OpenAI function-calling format).
type ToolDef struct {
	Type     string       `json:"type"` // "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction is the function schema within a tool definition.
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// CompletionRequest is sent to an LLM provider.
type CompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	Stop        []string  `json:"stop,omitempty"`
	Tools       []ToolDef `json:"tools,omitempty"`       // tool definitions for function calling
	ToolChoice  string    `json:"tool_choice,omitempty"` // "auto", "none", "required"
}

// CompletionResponse is the result from an LLM provider.
type CompletionResponse struct {
	Content      string     `json:"content"`
	Model        string     `json:"model"`
	FinishReason string     `json:"finish_reason"` // "stop" or "tool_calls"
	Usage        Usage      `json:"usage"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"` // tool invocations requested by the LLM
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk is a single piece of a streamed completion response.
type StreamChunk struct {
	Content      string     `json:"content"`                 // incremental text delta
	Model        string     `json:"model,omitempty"`         // only set on first/last chunk
	FinishReason string     `json:"finish_reason,omitempty"` // set when streaming is done (e.g. "stop")
	Done         bool       `json:"done"`                    // true on the final chunk
	Usage        *Usage     `json:"usage,omitempty"`         // set on the final chunk
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`    // tool invocations accumulated during stream
}

// ── Provider Interface ──────────────────────────────────────────────────────

// Provider is the interface every LLM backend must implement.
type Provider interface {
	// Name returns the provider identifier (e.g. "openai", "anthropic").
	Name() string

	// Complete sends a completion request and returns the response.
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)

	// ListModels returns the models available from this provider.
	ListModels(ctx context.Context) ([]string, error)

	// Healthy returns true if the provider is reachable.
	Healthy(ctx context.Context) bool
}

// StreamingProvider extends Provider with streaming support.
// Providers that support streaming should implement this interface.
type StreamingProvider interface {
	Provider

	// StreamComplete sends a completion request and returns a channel that
	// receives incremental StreamChunk values. The channel is closed when
	// the response is fully received or the context is cancelled.
	StreamComplete(ctx context.Context, req *CompletionRequest) (<-chan StreamChunk, error)
}

// ── Provider Metadata ───────────────────────────────────────────────────────

// ProviderMeta stores display metadata and configuration status for a provider.
type ProviderMeta struct {
	DisplayName  string `json:"display_name"`
	BaseURL      string `json:"base_url"`
	DefaultModel string `json:"default_model"`
	Configured   bool   `json:"configured"`   // true when an API key has been set
	RequiresKey  bool   `json:"requires_key"` // false for local-only providers like Ollama
	APIKey       string `json:"-"`            // never serialised — runtime only
}

// ── Provider Registry ───────────────────────────────────────────────────────

// SecretStore is the interface for persisting API keys. Implementations include
// file-based vaults (local dev), HashiCorp Vault, AWS Secrets Manager, etc.
type SecretStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
	GetAll() (map[string]string, error)
}

// Registry manages configured LLM providers with fallback ordering.
type Registry struct {
	providers map[string]Provider
	meta      map[string]ProviderMeta
	primary   string
	fallbacks []string
	vault     SecretStore // optional — if set, API keys are persisted
}

// NewRegistry creates an empty LLM provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		meta:      make(map[string]ProviderMeta),
	}
}

// SetVault attaches a secret store for persisting API keys across restarts.
func (r *Registry) SetVault(v SecretStore) {
	r.vault = v
}

// Register adds a provider. The first registered becomes the primary.
func (r *Registry) Register(p Provider) {
	r.providers[p.Name()] = p
	if r.primary == "" {
		r.primary = p.Name()
	} else {
		r.fallbacks = append(r.fallbacks, p.Name())
	}
}

// RegisterWithMeta adds a provider along with its display metadata.
func (r *Registry) RegisterWithMeta(p Provider, m ProviderMeta) {
	r.Register(p)
	r.meta[p.Name()] = m
}

// SetPrimary changes the primary provider.
func (r *Registry) SetPrimary(name string) {
	r.primary = name
}

// Get returns a specific provider.
func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// GetMeta returns metadata for a provider.
func (r *Registry) GetMeta(name string) (ProviderMeta, bool) {
	m, ok := r.meta[name]
	return m, ok
}

// UpdateAPIKey replaces the API key for a provider at runtime and marks it configured.
// If a vault is attached, the key is persisted for survival across restarts.
func (r *Registry) UpdateAPIKey(name, apiKey string) bool {
	m, ok := r.meta[name]
	if !ok {
		return false
	}
	m.Configured = apiKey != ""
	m.APIKey = apiKey
	r.meta[name] = m

	// Re-create the provider with the new key, preserving the rest.
	switch name {
	case "openai":
		r.providers[name] = NewOpenAIProvider(OpenAIConfig{
			APIKey: apiKey, BaseURL: m.BaseURL, DefaultModel: m.DefaultModel,
		})
	case "anthropic":
		r.providers[name] = NewAnthropicProvider(AnthropicConfig{
			APIKey: apiKey, BaseURL: m.BaseURL, DefaultModel: m.DefaultModel,
		})
	case "gemini":
		r.providers[name] = NewGeminiProvider(GeminiConfig{
			APIKey: apiKey, BaseURL: m.BaseURL, DefaultModel: m.DefaultModel,
		})
	case "grok":
		r.providers[name] = NewGrokProvider(GrokConfig{
			APIKey: apiKey, BaseURL: m.BaseURL, DefaultModel: m.DefaultModel,
		})
	case "ollama":
		r.providers[name] = NewOllamaProvider(OllamaConfig{
			BaseURL: m.BaseURL, DefaultModel: m.DefaultModel,
		})
	default:
		return false
	}

	// Persist to vault if available.
	if r.vault != nil {
		vaultKey := fmt.Sprintf("llm.%s.api_key", name)
		if apiKey != "" {
			if err := r.vault.Set(vaultKey, apiKey); err != nil {
				slog.Error("vault: failed to persist API key", "provider", name, "error", err)
			} else {
				slog.Info("vault: API key persisted", "provider", name)
			}
		} else {
			if err := r.vault.Delete(vaultKey); err != nil {
				slog.Error("vault: failed to delete API key", "provider", name, "error", err)
			}
		}
	}

	return true
}

// LoadFromVault loads any previously-persisted API keys from the vault and
// applies them to the registered providers. Called once at startup after all
// providers are registered. Environment variables take precedence — vault
// values are only used when the env var is empty.
func (r *Registry) LoadFromVault() int {
	if r.vault == nil {
		return 0
	}

	secrets, err := r.vault.GetAll()
	if err != nil {
		slog.Error("vault: failed to load secrets", "error", err)
		return 0
	}

	loaded := 0
	for vaultKey, apiKey := range secrets {
		// Keys are stored as "llm.<provider>.api_key".
		if len(vaultKey) < 5 || vaultKey[:4] != "llm." {
			continue
		}
		parts := splitVaultKey(vaultKey)
		if len(parts) != 3 || parts[2] != "api_key" {
			continue
		}
		providerName := parts[1]

		// Skip if the provider already has a key (from env var).
		if m, ok := r.meta[providerName]; ok && m.Configured && m.APIKey != "" {
			slog.Debug("vault: skipping — env var already set", "provider", providerName)
			continue
		}

		if apiKey != "" {
			// Apply without re-persisting (avoid write loop).
			r.updateAPIKeyInternal(providerName, apiKey)
			slog.Info("vault: loaded API key for provider", "provider", providerName)
			loaded++
		}
	}

	return loaded
}

// updateAPIKeyInternal applies an API key without persisting back to vault.
// Used during vault loading to avoid write loops.
func (r *Registry) updateAPIKeyInternal(name, apiKey string) bool {
	m, ok := r.meta[name]
	if !ok {
		return false
	}
	m.Configured = apiKey != ""
	m.APIKey = apiKey
	r.meta[name] = m

	switch name {
	case "openai":
		r.providers[name] = NewOpenAIProvider(OpenAIConfig{
			APIKey: apiKey, BaseURL: m.BaseURL, DefaultModel: m.DefaultModel,
		})
	case "anthropic":
		r.providers[name] = NewAnthropicProvider(AnthropicConfig{
			APIKey: apiKey, BaseURL: m.BaseURL, DefaultModel: m.DefaultModel,
		})
	case "gemini":
		r.providers[name] = NewGeminiProvider(GeminiConfig{
			APIKey: apiKey, BaseURL: m.BaseURL, DefaultModel: m.DefaultModel,
		})
	case "grok":
		r.providers[name] = NewGrokProvider(GrokConfig{
			APIKey: apiKey, BaseURL: m.BaseURL, DefaultModel: m.DefaultModel,
		})
	case "ollama":
		r.providers[name] = NewOllamaProvider(OllamaConfig{
			BaseURL: m.BaseURL, DefaultModel: m.DefaultModel,
		})
	default:
		return false
	}
	return true
}

// splitVaultKey splits a dotted key like "llm.openai.api_key" into parts.
func splitVaultKey(key string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(key); i++ {
		if key[i] == '.' {
			parts = append(parts, key[start:i])
			start = i + 1
		}
	}
	parts = append(parts, key[start:])
	return parts
}

// UpdateModel updates the default model for a provider at runtime.
func (r *Registry) UpdateModel(name, model string) bool {
	m, ok := r.meta[name]
	if !ok {
		return false
	}
	m.DefaultModel = model
	r.meta[name] = m
	return true
}

// UpdateBaseURL updates the base URL for a provider at runtime and re-creates
// the provider instance. Used for local providers like Ollama where the user
// configures the URL instead of an API key.
func (r *Registry) UpdateBaseURL(name, baseURL string) bool {
	m, ok := r.meta[name]
	if !ok {
		return false
	}
	m.BaseURL = baseURL
	m.Configured = baseURL != ""
	r.meta[name] = m

	switch name {
	case "ollama":
		r.providers[name] = NewOllamaProvider(OllamaConfig{
			BaseURL: baseURL, DefaultModel: m.DefaultModel,
		})
	default:
		return false
	}
	return true
}

// Primary returns the primary provider.
func (r *Registry) Primary() (Provider, bool) {
	return r.Get(r.primary)
}

// PrimaryName returns the name of the primary provider.
func (r *Registry) PrimaryName() string {
	return r.primary
}

// Complete tries the primary provider, then falls back on error.
func (r *Registry) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	// Try primary first.
	if p, ok := r.providers[r.primary]; ok {
		resp, err := p.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		// Log and try fallbacks.
	}

	for _, name := range r.fallbacks {
		p := r.providers[name]
		resp, err := p.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
	}

	return nil, ErrProviderNotFound
}

// ListProviders returns the names of all registered providers.
func (r *Registry) ListProviders() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// AllMeta returns metadata for every registered provider.
func (r *Registry) AllMeta() map[string]ProviderMeta {
	out := make(map[string]ProviderMeta, len(r.meta))
	for k, v := range r.meta {
		out[k] = v
	}
	return out
}

// StreamComplete tries streaming from the primary provider, falls back to
// non-streaming providers by converting the full response into a single chunk.
func (r *Registry) StreamComplete(ctx context.Context, req *CompletionRequest) (<-chan StreamChunk, error) {
	// Try primary first.
	if p, ok := r.providers[r.primary]; ok {
		if sp, ok := p.(StreamingProvider); ok {
			ch, err := sp.StreamComplete(ctx, req)
			if err == nil {
				return ch, nil
			}
		}
	}

	// Try fallbacks.
	for _, name := range r.fallbacks {
		p := r.providers[name]
		if sp, ok := p.(StreamingProvider); ok {
			ch, err := sp.StreamComplete(ctx, req)
			if err == nil {
				return ch, nil
			}
		}
	}

	// No streaming provider — fall back to regular Complete and emit as single chunk.
	resp, err := r.Complete(ctx, req)
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk, 1)
	ch <- StreamChunk{
		Content:      resp.Content,
		Model:        resp.Model,
		FinishReason: resp.FinishReason,
		Done:         true,
		Usage:        &resp.Usage,
	}
	close(ch)
	return ch, nil
}
