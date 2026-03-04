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
	"strings"
	"time"
)

// ── Anthropic Provider ──────────────────────────────────────────────────────

const anthropicDefaultBase = "https://api.anthropic.com/v1"

// AnthropicConfig configures the Anthropic provider.
type AnthropicConfig struct {
	APIKey       string
	BaseURL      string
	DefaultModel string
	Timeout      time.Duration
	APIVersion   string
}

// AnthropicProvider implements Provider for Anthropic Claude APIs.
type AnthropicProvider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	apiVersion   string
	client       *http.Client
}

// NewAnthropicProvider creates an Anthropic provider.
func NewAnthropicProvider(cfg AnthropicConfig) *AnthropicProvider {
	base := cfg.BaseURL
	if base == "" {
		base = anthropicDefaultBase
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	model := cfg.DefaultModel
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	apiVer := cfg.APIVersion
	if apiVer == "" {
		apiVer = "2023-06-01"
	}
	return &AnthropicProvider{
		apiKey:       cfg.APIKey,
		baseURL:      base,
		defaultModel: model,
		apiVersion:   apiVer,
		client:       &http.Client{Timeout: timeout},
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

// ── Anthropic Messages API ──────────────────────────────────────────────────

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Model      string `json:"model"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicErrorResp struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (p *AnthropicProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	// Anthropic separates the system message.
	var systemMsg string
	var msgs []anthropicMessage
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			systemMsg = m.Content
			continue
		}
		msgs = append(msgs, anthropicMessage{Role: string(m.Role), Content: m.Content})
	}

	body := anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    systemMsg,
		Messages:  msgs,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", p.apiVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: %s", ErrRateLimited, string(respBody))
	}
	if resp.StatusCode != http.StatusOK {
		var errResp anthropicErrorResp
		_ = json.Unmarshal(respBody, &errResp)
		return nil, fmt.Errorf("%w: %d — %s: %s", ErrProviderError, resp.StatusCode, errResp.Error.Type, errResp.Error.Message)
	}

	var ar anthropicResponse
	if err := json.Unmarshal(respBody, &ar); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	content := ""
	for _, c := range ar.Content {
		if c.Type == "text" {
			content += c.Text
		}
	}

	return &CompletionResponse{
		Content:      content,
		Model:        ar.Model,
		FinishReason: ar.StopReason,
		Usage: Usage{
			PromptTokens:     ar.Usage.InputTokens,
			CompletionTokens: ar.Usage.OutputTokens,
			TotalTokens:      ar.Usage.InputTokens + ar.Usage.OutputTokens,
		},
	}, nil
}

func (p *AnthropicProvider) ListModels(_ context.Context) ([]string, error) {
	return []string{
		"claude-sonnet-4-20250514",
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
		"claude-3-opus-20240229",
	}, nil
}

func (p *AnthropicProvider) Healthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Quick test with minimal request.
	_, err := p.Complete(ctx, &CompletionRequest{
		Model:     p.defaultModel,
		Messages:  []Message{{Role: RoleUser, Content: "ping"}},
		MaxTokens: 5,
	})
	if err != nil {
		slog.Debug("anthropic health check failed", "error", err)
		return false
	}
	return true
}

// ── Anthropic Streaming ─────────────────────────────────────────────────────

type anthropicStreamRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Stream    bool               `json:"stream"`
}

// StreamComplete sends a streaming completion request using Anthropic's SSE API.
func (p *AnthropicProvider) StreamComplete(ctx context.Context, req *CompletionRequest) (<-chan StreamChunk, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	var systemMsg string
	var msgs []anthropicMessage
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			systemMsg = m.Content
			continue
		}
		msgs = append(msgs, anthropicMessage{Role: string(m.Role), Content: m.Content})
	}

	body := anthropicStreamRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    systemMsg,
		Messages:  msgs,
		Stream:    true,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", p.apiVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("%w: %s", ErrRateLimited, string(respBody))
		}
		var errResp anthropicErrorResp
		_ = json.Unmarshal(respBody, &errResp)
		return nil, fmt.Errorf("%w: %d — %s: %s", ErrProviderError, resp.StatusCode, errResp.Error.Type, errResp.Error.Message)
	}

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		var currentModel string

		for scanner.Scan() {
			line := scanner.Text()

			if !strings.HasPrefix(line, "data: ") {
				// Anthropic also sends "event: " lines — skip them.
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var evt map[string]interface{}
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}

			evtType, _ := evt["type"].(string)

			switch evtType {
			case "message_start":
				// Extract model from message_start.message.model
				if msg, ok := evt["message"].(map[string]interface{}); ok {
					if m, ok := msg["model"].(string); ok {
						currentModel = m
					}
				}

			case "content_block_delta":
				if delta, ok := evt["delta"].(map[string]interface{}); ok {
					text, _ := delta["text"].(string)
					select {
					case ch <- StreamChunk{Content: text, Model: currentModel}:
					case <-ctx.Done():
						return
					}
				}

			case "message_delta":
				// Final delta with stop_reason and usage.
				sc := StreamChunk{Model: currentModel, Done: true}
				if delta, ok := evt["delta"].(map[string]interface{}); ok {
					if sr, ok := delta["stop_reason"].(string); ok {
						sc.FinishReason = sr
					}
				}
				if usage, ok := evt["usage"].(map[string]interface{}); ok {
					outTokens := int(usage["output_tokens"].(float64))
					sc.Usage = &Usage{CompletionTokens: outTokens}
				}
				select {
				case ch <- sc:
				case <-ctx.Done():
					return
				}

			case "message_stop":
				return
			}
		}
	}()

	return ch, nil
}

// Compile-time interface check.
var _ StreamingProvider = (*AnthropicProvider)(nil)
