package swarm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/llm"
	"github.com/AuralithAI/rtvortex-server/internal/swarm"
)

// ── Mock Providers ──────────────────────────────────────────────────────────

type mockProvider struct {
	name    string
	healthy bool
	err     error
	delay   time.Duration
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Complete(_ context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.err != nil {
		return nil, m.err
	}
	model := req.Model
	if model == "" {
		model = m.name + "-default-model"
	}
	return &llm.CompletionResponse{
		Content:      "response from " + m.name,
		Model:        model,
		FinishReason: "stop",
		Usage:        llm.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}, nil
}
func (m *mockProvider) ListModels(_ context.Context) ([]string, error) {
	return []string{"mock-model"}, nil
}
func (m *mockProvider) Healthy(_ context.Context) bool { return m.healthy }

// newTestRegistry creates a registry with the given providers all configured.
func newTestRegistry(names ...string) *llm.Registry {
	r := llm.NewRegistry()
	for _, name := range names {
		r.RegisterWithMeta(
			&mockProvider{name: name, healthy: true},
			llm.ProviderMeta{
				DisplayName: name,
				Configured:  true,
				RequiresKey: name != "ollama",
				APIKey:      "test-key",
			},
		)
	}
	return r
}

// newTestRegistryWithProviders creates a registry with custom mock providers.
func newTestRegistryWithProviders(providers ...*mockProvider) *llm.Registry {
	r := llm.NewRegistry()
	for _, p := range providers {
		r.RegisterWithMeta(
			p,
			llm.ProviderMeta{
				DisplayName: p.name,
				Configured:  true,
				RequiresKey: true,
				APIKey:      "test-key",
			},
		)
	}
	return r
}

// ── ProbeMultiple Unit Tests ────────────────────────────────────────────────

func TestProbeMultiple_Basic(t *testing.T) {
	reg := newTestRegistry("grok", "anthropic", "openai")
	reg.SetPriorityMatrix(map[string][]llm.ProviderPriority{
		"orchestrator": {
			{Provider: "grok", Model: "grok-3"},
			{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
			{Provider: "openai", Model: "gpt-4o"},
		},
	})
	proxy := swarm.NewLLMProxy(reg)

	req := &swarm.ProbeRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		AgentRole: "orchestrator",
	}

	resp, err := proxy.ProbeMultiple(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Providers != 3 {
		t.Errorf("expected 3 providers, got %d", resp.Providers)
	}
	if resp.Successes != 3 {
		t.Errorf("expected 3 successes, got %d", resp.Successes)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(resp.Results))
	}
	if resp.AgentRole != "orchestrator" {
		t.Errorf("expected role orchestrator, got %s", resp.AgentRole)
	}

	// Verify order matches priority matrix.
	expectedProviders := []string{"grok", "anthropic", "openai"}
	for i, name := range expectedProviders {
		if resp.Results[i].Provider != name {
			t.Errorf("result[%d]: expected provider %s, got %s", i, name, resp.Results[i].Provider)
		}
		if resp.Results[i].Error != "" {
			t.Errorf("result[%d]: unexpected error: %s", i, resp.Results[i].Error)
		}
		if resp.Results[i].Content == "" {
			t.Errorf("result[%d]: expected non-empty content", i)
		}
		if resp.Results[i].LatencyMs < 0 {
			t.Errorf("result[%d]: expected non-negative latency", i)
		}
	}
}

func TestProbeMultiple_NoProviders(t *testing.T) {
	reg := llm.NewRegistry()
	proxy := swarm.NewLLMProxy(reg)

	req := &swarm.ProbeRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		AgentRole: "nonexistent_role",
	}

	_, err := proxy.ProbeMultiple(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for no providers")
	}
}

func TestProbeMultiple_PartialFailure(t *testing.T) {
	reg := newTestRegistryWithProviders(
		&mockProvider{name: "grok", healthy: true},
		&mockProvider{name: "anthropic", healthy: true, err: errors.New("rate limited")},
		&mockProvider{name: "openai", healthy: true},
	)
	reg.SetPriorityMatrix(map[string][]llm.ProviderPriority{
		"senior_dev": {
			{Provider: "grok"},
			{Provider: "anthropic"},
			{Provider: "openai"},
		},
	})
	proxy := swarm.NewLLMProxy(reg)

	req := &swarm.ProbeRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		AgentRole: "senior_dev",
	}

	resp, err := proxy.ProbeMultiple(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Providers != 3 {
		t.Errorf("expected 3 providers, got %d", resp.Providers)
	}
	if resp.Successes != 2 {
		t.Errorf("expected 2 successes, got %d", resp.Successes)
	}

	// Anthropic (index 1) should have an error.
	if resp.Results[1].Error == "" {
		t.Error("expected error from anthropic")
	}
	if resp.Results[1].Provider != "anthropic" {
		t.Errorf("expected anthropic at index 1, got %s", resp.Results[1].Provider)
	}

	// grok and openai should succeed.
	if resp.Results[0].Error != "" {
		t.Errorf("grok should not have error: %s", resp.Results[0].Error)
	}
	if resp.Results[2].Error != "" {
		t.Errorf("openai should not have error: %s", resp.Results[2].Error)
	}
}

func TestProbeMultiple_AllFail(t *testing.T) {
	reg := newTestRegistryWithProviders(
		&mockProvider{name: "grok", healthy: true, err: errors.New("fail1")},
		&mockProvider{name: "openai", healthy: true, err: errors.New("fail2")},
	)
	reg.SetPriorityMatrix(map[string][]llm.ProviderPriority{
		"qa": {
			{Provider: "grok"},
			{Provider: "openai"},
		},
	})
	proxy := swarm.NewLLMProxy(reg)

	req := &swarm.ProbeRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		AgentRole: "qa",
	}

	resp, err := proxy.ProbeMultiple(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Successes != 0 {
		t.Errorf("expected 0 successes, got %d", resp.Successes)
	}
	for _, r := range resp.Results {
		if r.Error == "" {
			t.Errorf("expected all results to have errors")
		}
	}
}

func TestProbeMultiple_NumModelsLimit(t *testing.T) {
	reg := newTestRegistry("grok", "anthropic", "gemini", "openai")
	reg.SetPriorityMatrix(map[string][]llm.ProviderPriority{
		"architect": {
			{Provider: "grok"},
			{Provider: "anthropic"},
			{Provider: "gemini"},
			{Provider: "openai"},
		},
	})
	proxy := swarm.NewLLMProxy(reg)

	req := &swarm.ProbeRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		AgentRole: "architect",
		NumModels: 2,
	}

	resp, err := proxy.ProbeMultiple(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Providers != 2 {
		t.Errorf("expected 2 providers (limited), got %d", resp.Providers)
	}
	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(resp.Results))
	}
}

func TestProbeMultiple_ActionTypeFilter(t *testing.T) {
	reg := newTestRegistry("grok", "anthropic", "openai")
	reg.SetPriorityMatrix(map[string][]llm.ProviderPriority{
		"architect": {
			{Provider: "grok", ActionTypes: []string{"reasoning", "architecture"}},
			{Provider: "anthropic", ActionTypes: []string{"code_gen"}},
			{Provider: "openai"},
		},
	})
	proxy := swarm.NewLLMProxy(reg)

	req := &swarm.ProbeRequest{
		Messages:   []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		AgentRole:  "architect",
		ActionType: "reasoning",
	}

	resp, err := proxy.ProbeMultiple(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should get grok + openai (anthropic filtered by action type).
	if resp.Providers != 2 {
		t.Errorf("expected 2 providers (anthropic filtered), got %d", resp.Providers)
	}
	if resp.Results[0].Provider != "grok" {
		t.Errorf("expected grok first, got %s", resp.Results[0].Provider)
	}
	if resp.Results[1].Provider != "openai" {
		t.Errorf("expected openai second, got %s", resp.Results[1].Provider)
	}
}

func TestProbeMultiple_Parallel(t *testing.T) {
	// Verify parallelism: 3 providers each with 100ms delay.
	// If run sequentially, would take ~300ms. Parallel should be ~100ms.
	reg := newTestRegistryWithProviders(
		&mockProvider{name: "grok", healthy: true, delay: 100 * time.Millisecond},
		&mockProvider{name: "anthropic", healthy: true, delay: 100 * time.Millisecond},
		&mockProvider{name: "openai", healthy: true, delay: 100 * time.Millisecond},
	)
	reg.SetPriorityMatrix(map[string][]llm.ProviderPriority{
		"test": {
			{Provider: "grok"},
			{Provider: "anthropic"},
			{Provider: "openai"},
		},
	})
	proxy := swarm.NewLLMProxy(reg)

	req := &swarm.ProbeRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		AgentRole: "test",
	}

	start := time.Now()
	resp, err := proxy.ProbeMultiple(context.Background(), req)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Successes != 3 {
		t.Errorf("expected 3 successes, got %d", resp.Successes)
	}

	// Should complete in ~100-200ms if parallel, not ~300ms+.
	if elapsed > 250*time.Millisecond {
		t.Errorf("expected parallel execution (~100ms), but took %v (sequential?)", elapsed)
	}
}

func TestProbeMultiple_PreservesOrder(t *testing.T) {
	// Even though goroutines run concurrently, results should be in priority order.
	reg := newTestRegistryWithProviders(
		&mockProvider{name: "grok", healthy: true, delay: 50 * time.Millisecond},
		&mockProvider{name: "anthropic", healthy: true, delay: 10 * time.Millisecond}, // faster
		&mockProvider{name: "openai", healthy: true, delay: 30 * time.Millisecond},
	)
	reg.SetPriorityMatrix(map[string][]llm.ProviderPriority{
		"test": {
			{Provider: "grok"},
			{Provider: "anthropic"},
			{Provider: "openai"},
		},
	})
	proxy := swarm.NewLLMProxy(reg)

	req := &swarm.ProbeRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		AgentRole: "test",
	}

	resp, err := proxy.ProbeMultiple(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Despite different latencies, order must match priority matrix.
	expected := []string{"grok", "anthropic", "openai"}
	for i, name := range expected {
		if resp.Results[i].Provider != name {
			t.Errorf("result[%d]: expected %s, got %s", i, name, resp.Results[i].Provider)
		}
	}
}

func TestProbeMultiple_UsageTracking(t *testing.T) {
	reg := newTestRegistry("grok", "openai")
	reg.SetPriorityMatrix(map[string][]llm.ProviderPriority{
		"docs": {{Provider: "grok"}, {Provider: "openai"}},
	})
	proxy := swarm.NewLLMProxy(reg)

	req := &swarm.ProbeRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		AgentRole: "docs",
	}

	resp, err := proxy.ProbeMultiple(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range resp.Results {
		if r.Error == "" {
			if r.Usage.TotalTokens != 30 {
				t.Errorf("expected 30 total tokens from mock, got %d", r.Usage.TotalTokens)
			}
			if r.Usage.PromptTokens != 10 {
				t.Errorf("expected 10 prompt tokens, got %d", r.Usage.PromptTokens)
			}
		}
	}
}

func TestProbeMultiple_TotalMs(t *testing.T) {
	reg := newTestRegistry("grok")
	reg.SetPriorityMatrix(map[string][]llm.ProviderPriority{
		"test": {{Provider: "grok"}},
	})
	proxy := swarm.NewLLMProxy(reg)

	req := &swarm.ProbeRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		AgentRole: "test",
	}

	resp, err := proxy.ProbeMultiple(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.TotalMs < 0 {
		t.Errorf("expected non-negative total_ms, got %d", resp.TotalMs)
	}
}

// ── HandleProbe HTTP Tests ──────────────────────────────────────────────────

func TestHandleProbe_Success(t *testing.T) {
	reg := newTestRegistry("grok", "openai")
	reg.SetPriorityMatrix(map[string][]llm.ProviderPriority{
		"orchestrator": {
			{Provider: "grok", Model: "grok-3"},
			{Provider: "openai", Model: "gpt-4o"},
		},
	})
	proxy := swarm.NewLLMProxy(reg)

	body := swarm.ProbeRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		AgentRole: "orchestrator",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/internal/swarm/llm/probe", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	proxy.HandleProbe(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp swarm.ProbeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Providers != 2 {
		t.Errorf("expected 2 providers, got %d", resp.Providers)
	}
	if resp.Successes != 2 {
		t.Errorf("expected 2 successes, got %d", resp.Successes)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
}

func TestHandleProbe_InvalidJSON(t *testing.T) {
	reg := newTestRegistry("grok")
	proxy := swarm.NewLLMProxy(reg)

	req := httptest.NewRequest(http.MethodPost, "/internal/swarm/llm/probe",
		bytes.NewReader([]byte(`{invalid json`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	proxy.HandleProbe(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestHandleProbe_EmptyMessages(t *testing.T) {
	reg := newTestRegistry("grok")
	proxy := swarm.NewLLMProxy(reg)

	body := swarm.ProbeRequest{
		Messages:  []llm.Message{},
		AgentRole: "orchestrator",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/internal/swarm/llm/probe",
		bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	proxy.HandleProbe(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty messages, got %d", w.Code)
	}
}

func TestHandleProbe_NoConfiguredProviders(t *testing.T) {
	reg := llm.NewRegistry()
	proxy := swarm.NewLLMProxy(reg)

	body := swarm.ProbeRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		AgentRole: "unknown_role",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/internal/swarm/llm/probe",
		bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	proxy.HandleProbe(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for no providers, got %d", w.Code)
	}
}

func TestHandleProbe_ResponseShape(t *testing.T) {
	reg := newTestRegistry("grok", "anthropic", "openai")
	reg.SetPriorityMatrix(map[string][]llm.ProviderPriority{
		"senior_dev": {
			{Provider: "grok"},
			{Provider: "anthropic"},
			{Provider: "openai"},
		},
	})
	proxy := swarm.NewLLMProxy(reg)

	body := swarm.ProbeRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "review this code"}},
		AgentRole: "senior_dev",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/internal/swarm/llm/probe",
		bytes.NewReader(jsonBody))
	w := httptest.NewRecorder()

	proxy.HandleProbe(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp swarm.ProbeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	// Verify all expected fields are present.
	if resp.TotalMs < 0 {
		t.Error("expected non-negative total_ms")
	}
	if resp.AgentRole != "senior_dev" {
		t.Errorf("expected agent_role senior_dev, got %s", resp.AgentRole)
	}
	for i, r := range resp.Results {
		if r.Provider == "" {
			t.Errorf("result[%d]: empty provider", i)
		}
		if r.FinishReason != "stop" {
			t.Errorf("result[%d]: expected finish_reason stop, got %s", i, r.FinishReason)
		}
		if r.LatencyMs < 0 {
			t.Errorf("result[%d]: negative latency", i)
		}
	}
}

// ── ProbeResult / ProbeResponse Type Tests ──────────────────────────────────

func TestProbeResult_JSONSerialization(t *testing.T) {
	result := swarm.ProbeResult{
		Provider:     "grok",
		Model:        "grok-3",
		Content:      "test response",
		FinishReason: "stop",
		Usage:        swarm.LLMUsage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
		LatencyMs:    150,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded swarm.ProbeResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Provider != "grok" {
		t.Errorf("expected provider grok, got %s", decoded.Provider)
	}
	if decoded.LatencyMs != 150 {
		t.Errorf("expected latency 150, got %d", decoded.LatencyMs)
	}
	if decoded.Error != "" {
		t.Errorf("expected empty error, got %s", decoded.Error)
	}
}

func TestProbeResult_WithError(t *testing.T) {
	result := swarm.ProbeResult{
		Provider:  "anthropic",
		Error:     "rate limited",
		LatencyMs: 50,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded swarm.ProbeResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Error != "rate limited" {
		t.Errorf("expected error 'rate limited', got %s", decoded.Error)
	}
	if decoded.Content != "" {
		t.Errorf("expected empty content on error result, got %s", decoded.Content)
	}
}

func TestProbeResponse_JSONSerialization(t *testing.T) {
	resp := swarm.ProbeResponse{
		Results: []swarm.ProbeResult{
			{Provider: "grok", Content: "test", FinishReason: "stop"},
			{Provider: "openai", Content: "test2", FinishReason: "stop"},
		},
		TotalMs:   200,
		Providers: 2,
		Successes: 2,
		AgentRole: "orchestrator",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded swarm.ProbeResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Providers != 2 {
		t.Errorf("expected 2 providers, got %d", decoded.Providers)
	}
	if decoded.Successes != 2 {
		t.Errorf("expected 2 successes, got %d", decoded.Successes)
	}
	if decoded.AgentRole != "orchestrator" {
		t.Errorf("expected orchestrator, got %s", decoded.AgentRole)
	}
	if len(decoded.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(decoded.Results))
	}
}
