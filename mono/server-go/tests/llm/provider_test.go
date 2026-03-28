package llm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/llm"
)

// ── Mock Providers ──────────────────────────────────────────────────────────

type mockProvider struct {
	name    string
	healthy bool
	err     error
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Complete(_ context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &llm.CompletionResponse{
		Content:      "mock response from " + m.name,
		Model:        "mock-model",
		FinishReason: "stop",
		Usage:        llm.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}, nil
}
func (m *mockProvider) ListModels(_ context.Context) ([]string, error) {
	return []string{"mock-model"}, nil
}
func (m *mockProvider) Healthy(_ context.Context) bool { return m.healthy }

type mockStreamingProvider struct {
	mockProvider
	streamErr error
}

func (m *mockStreamingProvider) StreamComplete(_ context.Context, _ *llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	ch := make(chan llm.StreamChunk, 2)
	ch <- llm.StreamChunk{Content: "chunk1", Done: false}
	ch <- llm.StreamChunk{Content: "chunk2", Done: true, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

// ── Registry Tests ──────────────────────────────────────────────────────────

func TestNewRegistry(t *testing.T) {
	r := llm.NewRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if len(r.ListProviders()) != 0 {
		t.Errorf("expected 0 providers, got %d", len(r.ListProviders()))
	}
}

func TestRegistry_RegisterSetsFirstAsPrimary(t *testing.T) {
	r := llm.NewRegistry()
	r.Register(&mockProvider{name: "alpha", healthy: true})
	r.Register(&mockProvider{name: "beta", healthy: true})

	p, ok := r.Primary()
	if !ok {
		t.Fatal("expected primary provider")
	}
	if p.Name() != "alpha" {
		t.Errorf("expected primary 'alpha', got %q", p.Name())
	}
}

func TestRegistry_SetPrimary(t *testing.T) {
	r := llm.NewRegistry()
	r.Register(&mockProvider{name: "alpha", healthy: true})
	r.Register(&mockProvider{name: "beta", healthy: true})
	r.SetPrimary("beta")

	p, ok := r.Primary()
	if !ok {
		t.Fatal("expected primary provider")
	}
	if p.Name() != "beta" {
		t.Errorf("expected primary 'beta', got %q", p.Name())
	}
}

func TestRegistry_Get(t *testing.T) {
	r := llm.NewRegistry()
	r.Register(&mockProvider{name: "alpha", healthy: true})

	p, ok := r.Get("alpha")
	if !ok || p.Name() != "alpha" {
		t.Error("expected to find alpha")
	}

	_, ok = r.Get("nonexistent")
	if ok {
		t.Error("expected Get to return false for nonexistent provider")
	}
}

func TestRegistry_ListProviders(t *testing.T) {
	r := llm.NewRegistry()
	r.Register(&mockProvider{name: "a", healthy: true})
	r.Register(&mockProvider{name: "b", healthy: true})
	r.Register(&mockProvider{name: "c", healthy: true})

	names := r.ListProviders()
	if len(names) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(names))
	}
}

func TestRegistry_Complete_PrimarySuccess(t *testing.T) {
	r := llm.NewRegistry()
	r.Register(&mockProvider{name: "primary", healthy: true})

	resp, err := r.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "mock response from primary" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}

func TestRegistry_Complete_FallbackOnPrimaryError(t *testing.T) {
	r := llm.NewRegistry()
	r.Register(&mockProvider{name: "primary", healthy: true, err: errors.New("failed")})
	r.Register(&mockProvider{name: "fallback", healthy: true})

	resp, err := r.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "mock response from fallback" {
		t.Errorf("expected fallback response, got %q", resp.Content)
	}
}

func TestRegistry_Complete_AllFail(t *testing.T) {
	r := llm.NewRegistry()
	r.Register(&mockProvider{name: "a", healthy: true, err: errors.New("fail a")})
	r.Register(&mockProvider{name: "b", healthy: true, err: errors.New("fail b")})

	_, err := r.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

func TestRegistry_Complete_EmptyRegistry(t *testing.T) {
	r := llm.NewRegistry()
	_, err := r.Complete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for empty registry")
	}
}

func TestRegistry_StreamComplete_StreamingProvider(t *testing.T) {
	r := llm.NewRegistry()
	r.Register(&mockStreamingProvider{
		mockProvider: mockProvider{name: "streamer", healthy: true},
	})

	ch, err := r.StreamComplete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []llm.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Content != "chunk1" {
		t.Errorf("expected 'chunk1', got %q", chunks[0].Content)
	}
	if !chunks[1].Done {
		t.Error("expected last chunk to have Done=true")
	}
}

func TestRegistry_StreamComplete_FallbackToNonStreaming(t *testing.T) {
	r := llm.NewRegistry()
	// Non-streaming provider — StreamComplete should fall back to Complete
	r.Register(&mockProvider{name: "regular", healthy: true})

	ch, err := r.StreamComplete(context.Background(), &llm.CompletionRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []llm.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk (from non-streaming fallback), got %d", len(chunks))
	}
	if !chunks[0].Done {
		t.Error("expected single chunk to have Done=true")
	}
	if chunks[0].Content != "mock response from regular" {
		t.Errorf("unexpected content: %s", chunks[0].Content)
	}
}

// ── Type Tests ──────────────────────────────────────────────────────────────

func TestRoleConstants(t *testing.T) {
	if llm.RoleSystem != "system" {
		t.Errorf("expected system, got %s", llm.RoleSystem)
	}
	if llm.RoleUser != "user" {
		t.Errorf("expected user, got %s", llm.RoleUser)
	}
	if llm.RoleAssistant != "assistant" {
		t.Errorf("expected assistant, got %s", llm.RoleAssistant)
	}
}

func TestErrorConstants(t *testing.T) {
	if llm.ErrProviderNotFound == nil {
		t.Error("expected non-nil ErrProviderNotFound")
	}
	if llm.ErrRateLimited == nil {
		t.Error("expected non-nil ErrRateLimited")
	}
	if llm.ErrContextTooLarge == nil {
		t.Error("expected non-nil ErrContextTooLarge")
	}
	if llm.ErrStreamNotSupported == nil {
		t.Error("expected non-nil ErrStreamNotSupported")
	}
}
