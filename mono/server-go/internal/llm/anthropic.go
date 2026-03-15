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
	System    json.RawMessage    `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicToolDef `json:"tools,omitempty"`
}

// anthropicMessage supports text, tool_use, and tool_result content blocks.
// Anthropic requires Content to be either a string (simple) or an array of
// content blocks (multi-part / tool conversations). We use json.RawMessage
// and marshal conditionally.
type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// anthropicContentBlock represents a single content block in an Anthropic message.
type anthropicContentBlock struct {
	Type      string          `json:"type"`                  // "text", "tool_use", "tool_result"
	Text      string          `json:"text,omitempty"`        // for type="text"
	ID        string          `json:"id,omitempty"`          // for type="tool_use" (tool call ID)
	Name      string          `json:"name,omitempty"`        // for type="tool_use"
	Input     json.RawMessage `json:"input,omitempty"`       // for type="tool_use" (arguments object)
	ToolUseID string          `json:"tool_use_id,omitempty"` // for type="tool_result"
	Content   string          `json:"content,omitempty"`     // for type="tool_result"
}

// anthropicToolDef is Anthropic's tool definition format.
type anthropicToolDef struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	InputSchema  json.RawMessage        `json:"input_schema"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

// anthropicCacheControl enables Anthropic prompt caching on a content block.
type anthropicCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// anthropicSystemBlock is a content block in the system message array.
type anthropicSystemBlock struct {
	Type         string                 `json:"type"` // "text"
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"` // "end_turn", "tool_use", etc.
	Model      string                  `json:"model"`
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

// newAnthropicTextMessage creates an anthropicMessage with a simple text content.
func newAnthropicTextMessage(role, text string) anthropicMessage {
	b, _ := json.Marshal(text)
	return anthropicMessage{Role: role, Content: b}
}

// newAnthropicBlockMessage creates an anthropicMessage with content block array.
func newAnthropicBlockMessage(role string, blocks []anthropicContentBlock) anthropicMessage {
	b, _ := json.Marshal(blocks)
	return anthropicMessage{Role: role, Content: b}
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
		// Handle tool-result messages: Anthropic expects a content block array
		// with type="tool_result" under role="user".
		if m.Role == RoleTool {
			blocks := []anthropicContentBlock{{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			}}
			msgs = append(msgs, newAnthropicBlockMessage("user", blocks))
			continue
		}
		// Handle assistant messages with tool_calls: Anthropic expects
		// tool_use content blocks.
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			var blocks []anthropicContentBlock
			if m.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: json.RawMessage(tc.Function.Arguments),
				})
			}
			msgs = append(msgs, newAnthropicBlockMessage("assistant", blocks))
			continue
		}
		msgs = append(msgs, newAnthropicTextMessage(string(m.Role), m.Content))
	}

	// Convert tool definitions: OpenAI format → Anthropic format.
	// Mark the last tool with cache_control so Anthropic caches the tool list.
	var tools []anthropicToolDef
	for _, t := range req.Tools {
		tools = append(tools, anthropicToolDef{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}
	if len(tools) > 0 {
		tools[len(tools)-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}

	// Build system content block array with cache_control on the last block.
	var systemJSON json.RawMessage
	if systemMsg != "" {
		sysBlocks := []anthropicSystemBlock{{
			Type:         "text",
			Text:         systemMsg,
			CacheControl: &anthropicCacheControl{Type: "ephemeral"},
		}}
		var err error
		systemJSON, err = json.Marshal(sysBlocks)
		if err != nil {
			return nil, fmt.Errorf("marshal system blocks: %w", err)
		}
	}

	body := anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    systemJSON,
		Messages:  msgs,
		Tools:     tools,
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
	httpReq.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

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

	// Parse content blocks — collect text and tool_use blocks.
	var textContent string
	var toolCalls []ToolCall
	for _, c := range ar.Content {
		switch c.Type {
		case "text":
			textContent += c.Text
		case "tool_use":
			// Anthropic sends arguments as a JSON object in Input.
			// OpenAI format expects a JSON string in Arguments.
			argsStr := string(c.Input)
			toolCalls = append(toolCalls, ToolCall{
				ID:   c.ID,
				Type: "function",
				Function: ToolCallFunc{
					Name:      c.Name,
					Arguments: argsStr,
				},
			})
		}
	}

	finishReason := ar.StopReason
	if finishReason == "end_turn" {
		finishReason = "stop"
	}
	if finishReason == "tool_use" {
		finishReason = "tool_calls"
	}

	return &CompletionResponse{
		Content:      textContent,
		Model:        ar.Model,
		FinishReason: finishReason,
		ToolCalls:    toolCalls,
		Usage: Usage{
			PromptTokens:     ar.Usage.InputTokens,
			CompletionTokens: ar.Usage.OutputTokens,
			TotalTokens:      ar.Usage.InputTokens + ar.Usage.OutputTokens,
		},
	}, nil
}

func (p *AnthropicProvider) ListModels(ctx context.Context) ([]string, error) {
	// Try the Anthropic models API (beta).
	if p.apiKey != "" {
		url := p.baseURL + "/models?limit=50"
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err == nil {
			httpReq.Header.Set("x-api-key", p.apiKey)
			httpReq.Header.Set("anthropic-version", p.apiVersion)
			resp, err := p.client.Do(httpReq)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var result struct {
						Data []struct {
							ID string `json:"id"`
						} `json:"data"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && len(result.Data) > 0 {
						models := make([]string, 0, len(result.Data))
						for _, m := range result.Data {
							if strings.HasPrefix(m.ID, "claude-") {
								models = append(models, m.ID)
							}
						}
						if len(models) > 0 {
							return models, nil
						}
					}
				}
			}
		}
	}

	// Fallback: well-known Claude models.
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
	System    json.RawMessage    `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Stream    bool               `json:"stream"`
	Tools     []anthropicToolDef `json:"tools,omitempty"`
}

// StreamComplete sends a streaming completion request using Anthropic's SSE API.
// Supports both text content and tool calling during streaming. When the model
// invokes tools, the final chunk's ToolCalls field is populated with the
// accumulated tool_use blocks.
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
		if m.Role == RoleTool {
			blocks := []anthropicContentBlock{{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			}}
			msgs = append(msgs, newAnthropicBlockMessage("user", blocks))
			continue
		}
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			var blocks []anthropicContentBlock
			if m.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: json.RawMessage(tc.Function.Arguments),
				})
			}
			msgs = append(msgs, newAnthropicBlockMessage("assistant", blocks))
			continue
		}
		msgs = append(msgs, newAnthropicTextMessage(string(m.Role), m.Content))
	}

	// Convert tool definitions.
	var tools []anthropicToolDef
	for _, t := range req.Tools {
		tools = append(tools, anthropicToolDef{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}
	if len(tools) > 0 {
		tools[len(tools)-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}

	var systemJSON json.RawMessage
	if systemMsg != "" {
		sysBlocks := []anthropicSystemBlock{{
			Type:         "text",
			Text:         systemMsg,
			CacheControl: &anthropicCacheControl{Type: "ephemeral"},
		}}
		sysBytes, marshalErr := json.Marshal(sysBlocks)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal system blocks: %w", marshalErr)
		}
		systemJSON = sysBytes
	}

	body := anthropicStreamRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    systemJSON,
		Messages:  msgs,
		Stream:    true,
		Tools:     tools,
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
	httpReq.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

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

		// Tool call accumulation: Anthropic streams tool_use blocks as
		// content_block_start → input_json_delta(s) → content_block_stop.
		type pendingToolCall struct {
			ID        string
			Name      string
			InputJSON strings.Builder
		}
		var toolCalls []ToolCall
		var activeTool *pendingToolCall

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

			case "content_block_start":
				// A new content block is starting. Could be text or tool_use.
				if cb, ok := evt["content_block"].(map[string]interface{}); ok {
					blockType, _ := cb["type"].(string)
					if blockType == "tool_use" {
						id, _ := cb["id"].(string)
						name, _ := cb["name"].(string)
						activeTool = &pendingToolCall{ID: id, Name: name}
					}
				}

			case "content_block_delta":
				if delta, ok := evt["delta"].(map[string]interface{}); ok {
					deltaType, _ := delta["type"].(string)
					switch deltaType {
					case "text_delta":
						text, _ := delta["text"].(string)
						select {
						case ch <- StreamChunk{Content: text, Model: currentModel}:
						case <-ctx.Done():
							return
						}
					case "input_json_delta":
						// Accumulate partial JSON for the active tool call.
						if activeTool != nil {
							partialJSON, _ := delta["partial_json"].(string)
							activeTool.InputJSON.WriteString(partialJSON)
						}
					}
				}

			case "content_block_stop":
				// Finalise the active tool call if one was in progress.
				if activeTool != nil {
					toolCalls = append(toolCalls, ToolCall{
						ID:   activeTool.ID,
						Type: "function",
						Function: ToolCallFunc{
							Name:      activeTool.Name,
							Arguments: activeTool.InputJSON.String(),
						},
					})
					activeTool = nil
				}

			case "message_delta":
				// Final delta with stop_reason and usage.
				sc := StreamChunk{Model: currentModel, Done: true}
				if delta, ok := evt["delta"].(map[string]interface{}); ok {
					if sr, ok := delta["stop_reason"].(string); ok {
						sc.FinishReason = sr
						if sr == "end_turn" {
							sc.FinishReason = "stop"
						}
						if sr == "tool_use" {
							sc.FinishReason = "tool_calls"
						}
					}
				}
				if usage, ok := evt["usage"].(map[string]interface{}); ok {
					outTokens := int(usage["output_tokens"].(float64))
					sc.Usage = &Usage{CompletionTokens: outTokens}
				}
				// Attach accumulated tool calls to the final chunk.
				if len(toolCalls) > 0 {
					sc.ToolCalls = toolCalls
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
