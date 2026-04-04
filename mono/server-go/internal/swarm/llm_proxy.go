package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/llm"
)

// LLMProxy normalises all provider responses to the OpenAI chat-completion
// wire format (the de facto industry standard). Every major provider — Anthropic,
// Gemini, Grok/xAI, Ollama — either natively supports this format or the Go
// provider adapter in internal/llm/ translates to it.
//
// This means the Python swarm receives one consistent JSON shape regardless of
// which LLM provider the user has configured in rtserverprops.xml or the
// dashboard. The Go llm.Registry handles all provider-specific API details.
type LLMProxy struct {
	registry *llm.Registry
}

// NewLLMProxy creates an LLM proxy backed by the existing registry.
func NewLLMProxy(reg *llm.Registry) *LLMProxy {
	return &LLMProxy{registry: reg}
}

// ── Request / Response (OpenAI-compatible) ──────────────────────────────────

// LLMCompleteRequest is the JSON body for POST /internal/swarm/llm/complete.
type LLMCompleteRequest struct {
	Messages   []llm.Message `json:"messages"`
	Tools      []llm.ToolDef `json:"tools,omitempty"`
	ToolChoice string        `json:"tool_choice,omitempty"`
	MaxTokens  int           `json:"max_tokens,omitempty"`
	Model      string        `json:"model,omitempty"`
	AgentRole  string        `json:"agent_role,omitempty"` // role hint for smart model routing
}

// LLMCompleteResponse is the OpenAI-compatible response shape.
type LLMCompleteResponse struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Model   string      `json:"model"`
	Choices []LLMChoice `json:"choices"`
	Usage   LLMUsage    `json:"usage"`
}

// LLMChoice is a single completion choice.
type LLMChoice struct {
	Index        int        `json:"index"`
	Message      LLMMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

// LLMMessage is the assistant's message in the response.
type LLMMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []llm.ToolCall `json:"tool_calls,omitempty"`
}

// LLMUsage tracks token consumption.
type LLMUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ── Complete ────────────────────────────────────────────────────────────────

// Complete sends a completion request through the existing llm.Registry and
// normalises the response to OpenAI-compatible format.
func (p *LLMProxy) Complete(ctx context.Context, req *LLMCompleteRequest) (*LLMCompleteResponse, error) {
	// Build the internal CompletionRequest.
	cr := &llm.CompletionRequest{
		Messages:   req.Messages,
		Tools:      req.Tools,
		ToolChoice: req.ToolChoice,
		MaxTokens:  req.MaxTokens,
		Model:      req.Model,
		AgentRole:  req.AgentRole,
	}

	// Use registry to complete (handles fallback, provider selection).
	resp, err := p.registry.Complete(ctx, cr)
	if err != nil {
		return nil, fmt.Errorf("llm proxy: %w", err)
	}

	// Normalise to OpenAI-compatible response.
	msg := LLMMessage{
		Role:    "assistant",
		Content: resp.Content,
	}
	if len(resp.ToolCalls) > 0 {
		msg.ToolCalls = resp.ToolCalls
	}

	out := &LLMCompleteResponse{
		ID:     "swarm-" + resp.Model,
		Object: "chat.completion",
		Model:  resp.Model,
		Choices: []LLMChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: resp.FinishReason,
			},
		},
		Usage: LLMUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}

	return out, nil
}

// ── Multi-LLM Probe ────────────────────────────────────────────────────────

// ProbeRequest is the JSON body for POST /internal/swarm/llm/probe.
// The agent sends the same messages to multiple LLMs in parallel.
type ProbeRequest struct {
	Messages   []llm.Message `json:"messages"`
	Tools      []llm.ToolDef `json:"tools,omitempty"`
	ToolChoice string        `json:"tool_choice,omitempty"`
	MaxTokens  int           `json:"max_tokens,omitempty"`
	AgentRole  string        `json:"agent_role,omitempty"`  // role for priority matrix lookup
	ActionType string        `json:"action_type,omitempty"` // optional action type filter
	NumModels  int           `json:"num_models,omitempty"`  // max providers to probe (0 = all)
}

// ProbeResult is the response from a single LLM provider in a multi-probe.
type ProbeResult struct {
	Provider     string         `json:"provider"`
	Model        string         `json:"model"`
	Content      string         `json:"content"`
	FinishReason string         `json:"finish_reason"`
	ToolCalls    []llm.ToolCall `json:"tool_calls,omitempty"`
	Usage        LLMUsage       `json:"usage"`
	LatencyMs    int64          `json:"latency_ms"`
	Error        string         `json:"error,omitempty"` // non-empty if this provider failed
}

// ProbeResponse wraps the results from all probed providers.
type ProbeResponse struct {
	Results   []ProbeResult `json:"results"`
	TotalMs   int64         `json:"total_ms"`   // wall-clock time for the entire probe
	Providers int           `json:"providers"`   // number of providers probed
	Successes int           `json:"successes"`   // number that returned successfully
	AgentRole string        `json:"agent_role"`  // echo back for caller convenience
}

// ProbeMultiple fans out completion requests to multiple LLM providers in
// parallel, collects all responses, and returns them in priority order.
//
// This is the core of the Perplexity-style multi-LLM approach: every provider
// in the role's priority matrix is queried simultaneously, and the agent (or
// consensus engine in Phase 5) chooses the best answer.
func (p *LLMProxy) ProbeMultiple(ctx context.Context, req *ProbeRequest) (*ProbeResponse, error) {
	probeStart := time.Now()

	// Get the ordered provider list from the priority matrix.
	order := p.registry.PriorityOrder(req.AgentRole, req.ActionType, req.NumModels)
	if len(order) == 0 {
		return nil, fmt.Errorf("llm probe: no providers configured for role %q", req.AgentRole)
	}

	slog.Info("llm probe: fanning out",
		"role", req.AgentRole,
		"action_type", req.ActionType,
		"providers", len(order),
	)

	// Fan out to all providers in parallel.
	var mu sync.Mutex
	var wg sync.WaitGroup
	results := make([]ProbeResult, len(order))

	for i, entry := range order {
		wg.Add(1)
		go func(idx int, e llm.RouteEntry) {
			defer wg.Done()

			provStart := time.Now()

			// Build per-provider request with the provider's model.
			cr := &llm.CompletionRequest{
				Messages:   req.Messages,
				Tools:      req.Tools,
				ToolChoice: req.ToolChoice,
				MaxTokens:  req.MaxTokens,
				Model:      e.Model,
				AgentRole:  req.AgentRole,
			}

			result := ProbeResult{
				Provider: e.Provider,
				Model:    e.Model,
			}

			// Get the specific provider from the registry and call it directly.
			provider, ok := p.registry.Get(e.Provider)
			if !ok {
				result.Error = fmt.Sprintf("provider %q not found", e.Provider)
				result.LatencyMs = time.Since(provStart).Milliseconds()
				mu.Lock()
				results[idx] = result
				mu.Unlock()
				return
			}

			resp, err := provider.Complete(ctx, cr)
			result.LatencyMs = time.Since(provStart).Milliseconds()

			if err != nil {
				result.Error = err.Error()
				slog.Warn("llm probe: provider failed",
					"provider", e.Provider,
					"model", e.Model,
					"error", err,
					"latency_ms", result.LatencyMs,
				)
				// Record per-provider failure metric.
				SwarmProbeProviderLatency.WithLabelValues(e.Provider, "error").Observe(float64(result.LatencyMs) / 1000.0)
			} else {
				result.Content = resp.Content
				result.Model = resp.Model // use actual model name from response
				result.FinishReason = resp.FinishReason
				result.ToolCalls = resp.ToolCalls
				result.Usage = LLMUsage{
					PromptTokens:     resp.Usage.PromptTokens,
					CompletionTokens: resp.Usage.CompletionTokens,
					TotalTokens:      resp.Usage.TotalTokens,
				}
				slog.Info("llm probe: provider responded",
					"provider", e.Provider,
					"model", resp.Model,
					"latency_ms", result.LatencyMs,
					"tokens", resp.Usage.TotalTokens,
				)
				// Record per-provider success metric.
				SwarmProbeProviderLatency.WithLabelValues(e.Provider, "ok").Observe(float64(result.LatencyMs) / 1000.0)
			}

			mu.Lock()
			results[idx] = result
			mu.Unlock()
		}(i, entry)
	}

	wg.Wait()

	totalMs := time.Since(probeStart).Milliseconds()
	successes := 0
	for _, r := range results {
		if r.Error == "" {
			successes++
		}
	}

	return &ProbeResponse{
		Results:   results,
		TotalMs:   totalMs,
		Providers: len(order),
		Successes: successes,
		AgentRole: req.AgentRole,
	}, nil
}

// ── HTTP Handlers ───────────────────────────────────────────────────────────

// HandleProbe is the HTTP handler for POST /internal/swarm/llm/probe.
// It fans out completion requests to multiple providers and returns all results.
func (p *LLMProxy) HandleProbe(w http.ResponseWriter, r *http.Request) {
	// Probes query multiple LLMs — allow up to 5 minutes total.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(5 * time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var req ProbeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		http.Error(w, `{"error":"messages array is required"}`, http.StatusBadRequest)
		return
	}

	slog.Info("swarm llm probe request",
		"agent_role", req.AgentRole,
		"action_type", req.ActionType,
		"num_models", req.NumModels,
		"messages", len(req.Messages),
	)

	start := time.Now()
	resp, err := p.ProbeMultiple(ctx, &req)
	duration := time.Since(start)

	if err != nil {
		slog.Error("swarm llm probe error", "error", err)
		SwarmProbeCallsTotal.WithLabelValues("error").Inc()
		SwarmProbeWallTime.Observe(duration.Seconds())
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	// Record aggregate metrics.
	if resp.Successes > 0 {
		SwarmProbeCallsTotal.WithLabelValues("ok").Inc()
	} else {
		SwarmProbeCallsTotal.WithLabelValues("all_failed").Inc()
	}
	SwarmProbeWallTime.Observe(duration.Seconds())
	SwarmProbeProviderCount.Observe(float64(resp.Providers))

	// Aggregate token usage across all providers.
	for _, r := range resp.Results {
		if r.Error == "" {
			if r.Usage.PromptTokens > 0 {
				SwarmLLMTokensTotal.WithLabelValues("prompt").Add(float64(r.Usage.PromptTokens))
			}
			if r.Usage.CompletionTokens > 0 {
				SwarmLLMTokensTotal.WithLabelValues("completion").Add(float64(r.Usage.CompletionTokens))
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ── HTTP Handler ────────────────────────────────────────────────────────────

// HandleComplete is the HTTP handler for POST /internal/swarm/llm/complete.
func (p *LLMProxy) HandleComplete(w http.ResponseWriter, r *http.Request) {
	// LLM calls routinely exceed 60s — large context packs, provider
	// fallback cascades (Anthropic rate-limited → OpenAI → Gemini), etc.
	// We bypass two server-level timeouts:
	//
	//  1. chi middleware.Timeout(60s) — wraps r.Context() with a 60s
	//     deadline. We detach from it with context.WithTimeout on
	//     context.Background() so outbound HTTP calls aren't cancelled.
	//
	//  2. http.Server.WriteTimeout(60s) — kills the TCP connection
	//     after 60s of silence. We extend it via ResponseController.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(5 * time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var req LLMCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Log the context the agent is sending.
	logAgentContext(&req)

	start := time.Now()
	resp, err := p.Complete(ctx, &req)
	duration := time.Since(start)

	if err != nil {
		slog.Error("swarm llm proxy error", "error", err)
		SwarmLLMCallsTotal.WithLabelValues("error").Inc()
		SwarmLLMCallDuration.Observe(duration.Seconds())
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	// Record metrics.
	SwarmLLMCallsTotal.WithLabelValues("ok").Inc()
	SwarmLLMCallDuration.Observe(duration.Seconds())
	if resp.Usage.PromptTokens > 0 {
		SwarmLLMTokensTotal.WithLabelValues("prompt").Add(float64(resp.Usage.PromptTokens))
	}
	if resp.Usage.CompletionTokens > 0 {
		SwarmLLMTokensTotal.WithLabelValues("completion").Add(float64(resp.Usage.CompletionTokens))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// logAgentContext logs a summary of every message the agent sends to the LLM,
// including role, content length, tool call names, and tool result IDs.
func logAgentContext(req *LLMCompleteRequest) {
	roleCounts := map[string]int{}
	totalChars := 0
	for _, m := range req.Messages {
		roleCounts[string(m.Role)]++
		totalChars += len(m.Content)
	}

	slog.Info("swarm llm request",
		"agent_role", req.AgentRole,
		"model", req.Model,
		"messages", len(req.Messages),
		"tools", len(req.Tools),
		"roles", roleCounts,
		"total_chars", totalChars,
	)

	for i, m := range req.Messages {
		attrs := []any{
			"idx", i,
			"role", m.Role,
			"len", len(m.Content),
		}
		if m.Name != "" {
			attrs = append(attrs, "name", m.Name)
		}
		if m.ToolCallID != "" {
			attrs = append(attrs, "tool_call_id", m.ToolCallID)
		}
		if len(m.ToolCalls) > 0 {
			names := make([]string, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				names[j] = tc.Function.Name
			}
			attrs = append(attrs, "tool_calls", names)
		}

		// Truncate content for log readability.
		preview := m.Content
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		attrs = append(attrs, "content", preview)

		slog.Debug("swarm llm msg", attrs...)
	}
}
