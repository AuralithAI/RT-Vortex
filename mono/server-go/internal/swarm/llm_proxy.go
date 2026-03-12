package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

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
	Messages  []llm.Message `json:"messages"`
	Tools     []ToolSchema  `json:"tools,omitempty"`
	MaxTokens int           `json:"max_tokens,omitempty"`
	Model     string        `json:"model,omitempty"`
}

// ToolSchema describes a tool in OpenAI function-calling format.
type ToolSchema struct {
	Type     string       `json:"type"` // "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction is the function definition within a tool schema.
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
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
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents a tool invocation requested by the LLM.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function ToolCallFunc `json:"function"`
}

// ToolCallFunc is the function name + arguments string.
type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
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
		Messages:  req.Messages,
		MaxTokens: req.MaxTokens,
		Model:     req.Model,
	}

	// Use registry to complete (handles fallback, provider selection).
	resp, err := p.registry.Complete(ctx, cr)
	if err != nil {
		return nil, fmt.Errorf("llm proxy: %w", err)
	}

	// Normalise to OpenAI-compatible response.
	out := &LLMCompleteResponse{
		ID:     "swarm-" + resp.Model,
		Object: "chat.completion",
		Model:  resp.Model,
		Choices: []LLMChoice{
			{
				Index: 0,
				Message: LLMMessage{
					Role:    "assistant",
					Content: resp.Content,
				},
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

// ── HTTP Handler ────────────────────────────────────────────────────────────

// HandleComplete is the HTTP handler for POST /internal/swarm/llm/complete.
func (p *LLMProxy) HandleComplete(w http.ResponseWriter, r *http.Request) {
	var req LLMCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	resp, err := p.Complete(r.Context(), &req)
	if err != nil {
		slog.Error("swarm llm proxy error", "error", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
