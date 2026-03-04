package llm

import (
	"context"
	"errors"
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
)

// Message is a single message in a conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// CompletionRequest is sent to an LLM provider.
type CompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	Stop        []string  `json:"stop,omitempty"`
}

// CompletionResponse is the result from an LLM provider.
type CompletionResponse struct {
	Content      string `json:"content"`
	Model        string `json:"model"`
	FinishReason string `json:"finish_reason"`
	Usage        Usage  `json:"usage"`
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk is a single piece of a streamed completion response.
type StreamChunk struct {
	Content      string `json:"content"`                // incremental text delta
	Model        string `json:"model,omitempty"`        // only set on first/last chunk
	FinishReason string `json:"finish_reason,omitempty"` // set when streaming is done (e.g. "stop")
	Done         bool   `json:"done"`                   // true on the final chunk
	Usage        *Usage `json:"usage,omitempty"`         // set on the final chunk
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

// ── Provider Registry ───────────────────────────────────────────────────────

// Registry manages configured LLM providers with fallback ordering.
type Registry struct {
	providers map[string]Provider
	primary   string
	fallbacks []string
}

// NewRegistry creates an empty LLM provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
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

// SetPrimary changes the primary provider.
func (r *Registry) SetPrimary(name string) {
	r.primary = name
}

// Get returns a specific provider.
func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// Primary returns the primary provider.
func (r *Registry) Primary() (Provider, bool) {
	return r.Get(r.primary)
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
