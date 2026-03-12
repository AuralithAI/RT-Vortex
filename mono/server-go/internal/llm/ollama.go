package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// ── Ollama Provider (Local / Self-Hosted) ───────────────────────────────────

const ollamaDefaultBase = "http://localhost:11434"

// OllamaConfig configures the Ollama provider.
type OllamaConfig struct {
	BaseURL      string
	DefaultModel string
	Timeout      time.Duration
}

// OllamaProvider implements Provider for the Ollama local inference engine.
type OllamaProvider struct {
	baseURL      string
	defaultModel string
	client       *http.Client
}

// NewOllamaProvider creates an Ollama provider.
func NewOllamaProvider(cfg OllamaConfig) *OllamaProvider {
	base := cfg.BaseURL
	if base == "" {
		base = ollamaDefaultBase
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	model := cfg.DefaultModel
	if model == "" {
		model = "llama3.1:8b"
	}
	return &OllamaProvider{
		baseURL:      base,
		defaultModel: model,
		client:       &http.Client{Timeout: timeout},
	}
}

func (p *OllamaProvider) Name() string { return "ollama" }

// ── Ollama Chat API ─────────────────────────────────────────────────────────

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  *ollamaOptions  `json:"options,omitempty"`
	Tools    []ToolDef       `json:"tools,omitempty"`
}

type ollamaMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ollamaOptions struct {
	Temperature float64  `json:"temperature,omitempty"`
	TopP        float64  `json:"top_p,omitempty"`
	NumPredict  int      `json:"num_predict,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

type ollamaChatResponse struct {
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
	Model   string        `json:"model"`
	// Token counts (available when done=true).
	PromptEvalCount int `json:"prompt_eval_count"`
	EvalCount       int `json:"eval_count"`
}

func (p *OllamaProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	var msgs []ollamaMessage
	for _, m := range req.Messages {
		om := ollamaMessage{Role: string(m.Role), Content: m.Content}
		if m.Role == RoleTool {
			om.Role = "tool"
			om.ToolCallID = m.ToolCallID
		}
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			om.ToolCalls = m.ToolCalls
		}
		msgs = append(msgs, om)
	}

	body := ollamaChatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   false,
		Tools:    req.Tools,
		Options: &ollamaOptions{
			Temperature: req.Temperature,
			TopP:        req.TopP,
			NumPredict:  req.MaxTokens,
			Stop:        req.Stop,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2 MB
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: ollama returned %d: %s", ErrProviderError, resp.StatusCode, string(respBody))
	}

	var cr ollamaChatResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	out := &CompletionResponse{
		Content:      cr.Message.Content,
		Model:        cr.Model,
		FinishReason: "stop",
		Usage: Usage{
			PromptTokens:     cr.PromptEvalCount,
			CompletionTokens: cr.EvalCount,
			TotalTokens:      cr.PromptEvalCount + cr.EvalCount,
		},
	}

	if len(cr.Message.ToolCalls) > 0 {
		out.ToolCalls = cr.Message.ToolCalls
		out.FinishReason = "tool_calls"
	}

	return out, nil
}

func (p *OllamaProvider) ListModels(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]string, len(result.Models))
	for i, m := range result.Models {
		models[i] = m.Name
	}
	return models, nil
}

func (p *OllamaProvider) Healthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	resp, err := p.client.Do(req)
	if err != nil {
		slog.Debug("ollama health check failed", "error", err)
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ── Ollama Streaming ────────────────────────────────────────────────────────

// StreamComplete sends a streaming chat request to Ollama.
// Ollama streams newline-delimited JSON objects when stream=true.
func (p *OllamaProvider) StreamComplete(ctx context.Context, req *CompletionRequest) (<-chan StreamChunk, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	var msgs []ollamaMessage
	for _, m := range req.Messages {
		om := ollamaMessage{Role: string(m.Role), Content: m.Content}
		if m.Role == RoleTool {
			om.Role = "tool"
			om.ToolCallID = m.ToolCallID
		}
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			om.ToolCalls = m.ToolCalls
		}
		msgs = append(msgs, om)
	}

	body := ollamaChatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   true,
		Tools:    req.Tools,
		Options: &ollamaOptions{
			Temperature: req.Temperature,
			TopP:        req.TopP,
			NumPredict:  req.MaxTokens,
			Stop:        req.Stop,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		return nil, fmt.Errorf("%w: ollama returned %d: %s", ErrProviderError, resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var cr ollamaChatResponse
			if err := json.Unmarshal(line, &cr); err != nil {
				slog.Debug("ollama stream: failed to parse chunk", "error", err)
				continue
			}

			sc := StreamChunk{
				Content: cr.Message.Content,
				Model:   cr.Model,
			}

			if cr.Done {
				sc.Done = true
				sc.FinishReason = "stop"
				sc.Usage = &Usage{
					PromptTokens:     cr.PromptEvalCount,
					CompletionTokens: cr.EvalCount,
					TotalTokens:      cr.PromptEvalCount + cr.EvalCount,
				}
			}

			select {
			case ch <- sc:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// Compile-time interface check.
var _ StreamingProvider = (*OllamaProvider)(nil)
