package llm

import (
	"context"
	"log/slog"
	"time"
)

// ── Grok Provider (xAI) ─────────────────────────────────────────────────────
// Grok uses an OpenAI-compatible API via xAI.

const grokDefaultBase = "https://api.x.ai/v1"

// GrokConfig configures the xAI Grok provider.
type GrokConfig struct {
	APIKey       string
	BaseURL      string
	DefaultModel string
	Timeout      time.Duration
}

// GrokProvider wraps OpenAIProvider for xAI's Grok API.
type GrokProvider struct {
	inner *OpenAIProvider
}

// NewGrokProvider creates a Grok provider.
func NewGrokProvider(cfg GrokConfig) *GrokProvider {
	base := cfg.BaseURL
	if base == "" {
		base = grokDefaultBase
	}
	model := cfg.DefaultModel
	if model == "" {
		model = "grok-3"
	}
	return &GrokProvider{
		inner: NewOpenAIProvider(OpenAIConfig{
			APIKey:       cfg.APIKey,
			BaseURL:      base,
			DefaultModel: model,
			Timeout:      cfg.Timeout,
		}),
	}
}

func (p *GrokProvider) Name() string { return "grok" }

func (p *GrokProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	return p.inner.Complete(ctx, req)
}

func (p *GrokProvider) ListModels(ctx context.Context) ([]string, error) {
	if p.inner.apiKey != "" {
		models, err := p.inner.ListModels(ctx)
		if err == nil && len(models) > 0 {
			return models, nil
		}
		slog.Debug("grok: dynamic model list failed, using fallback", "error", err)
	}
	// Fallback.
	return []string{
		"grok-3",
		"grok-3-mini",
		"grok-3-fast",
		"grok-2",
	}, nil
}

func (p *GrokProvider) Healthy(ctx context.Context) bool {
	if p.inner.apiKey == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := p.inner.ListModels(ctx)
	if err != nil {
		slog.Debug("grok health check failed", "error", err)
		return false
	}
	return true
}

func (p *GrokProvider) StreamComplete(ctx context.Context, req *CompletionRequest) (<-chan StreamChunk, error) {
	return p.inner.StreamComplete(ctx, req)
}

// Compile-time interface check.
var _ StreamingProvider = (*GrokProvider)(nil)
