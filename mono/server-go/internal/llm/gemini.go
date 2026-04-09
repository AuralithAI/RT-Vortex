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

// ── Gemini Provider ─────────────────────────────────────────────────────────

const geminiDefaultBase = "https://generativelanguage.googleapis.com/v1beta"

// GeminiConfig configures the Google Gemini provider.
type GeminiConfig struct {
	APIKey       string
	BaseURL      string
	DefaultModel string
	Timeout      time.Duration
}

// GeminiProvider implements Provider for Google Gemini APIs.
type GeminiProvider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	client       *http.Client
}

// NewGeminiProvider creates a Gemini provider.
func NewGeminiProvider(cfg GeminiConfig) *GeminiProvider {
	base := cfg.BaseURL
	if base == "" {
		base = geminiDefaultBase
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	model := cfg.DefaultModel
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return &GeminiProvider{
		apiKey:       cfg.APIKey,
		baseURL:      base,
		defaultModel: model,
		client:       &http.Client{Timeout: timeout},
	}
}

func (p *GeminiProvider) Name() string { return "gemini" }

// ── Gemini Generate Content API ─────────────────────────────────────────────

type geminiRequest struct {
	Contents         []geminiContent          `json:"contents"`
	SystemInstruct   *geminiContent           `json:"systemInstruction,omitempty"`
	GenerationConfig *geminiGenerationCfg     `json:"generationConfig,omitempty"`
	Tools            []geminiToolDeclarations `json:"tools,omitempty"`
}

type geminiToolDeclarations struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations"`
}

type geminiFunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string          `json:"text,omitempty"`
	FunctionCall     *geminiFuncCall `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResp `json:"functionResponse,omitempty"`
}

type geminiFuncCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type geminiFuncResp struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

type geminiGenerationCfg struct {
	MaxOutputTokens int                `json:"maxOutputTokens,omitempty"`
	Temperature     float64            `json:"temperature,omitempty"`
	TopP            float64            `json:"topP,omitempty"`
	StopSequences   []string           `json:"stopSequences,omitempty"`
	ThinkingConfig  *geminiThinkingCfg `json:"thinkingConfig,omitempty"`
}

type geminiThinkingCfg struct {
	ThinkingBudget int `json:"thinkingBudget"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"` // "STOP", "MAX_TOKENS", "SAFETY", etc.
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

type geminiErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

func (p *GeminiProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	// Build tool_call_id → function name lookup for tool result messages.
	tcNameByID := make(map[string]string)
	for _, m := range req.Messages {
		for _, tc := range m.ToolCalls {
			if tc.ID != "" && tc.Function.Name != "" {
				tcNameByID[tc.ID] = tc.Function.Name
			}
		}
	}

	var systemInstruct *geminiContent
	var contents []geminiContent
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			systemInstruct = &geminiContent{
				Parts: []geminiPart{{Text: m.Content}},
			}
			continue
		}
		// Handle tool result messages.
		if m.Role == RoleTool {
			var respObj map[string]interface{}
			if err := json.Unmarshal([]byte(m.Content), &respObj); err != nil {
				respObj = map[string]interface{}{"result": m.Content}
			}
			funcName := m.Name
			if funcName == "" {
				funcName = tcNameByID[m.ToolCallID]
			}
			if funcName == "" {
				funcName = "tool_result"
			}
			contents = append(contents, geminiContent{
				Role: "function",
				Parts: []geminiPart{{
					FunctionResponse: &geminiFuncResp{
						Name:     funcName,
						Response: respObj,
					},
				}},
			})
			continue
		}
		// Handle assistant messages with tool calls.
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			var parts []geminiPart
			if m.Content != "" {
				parts = append(parts, geminiPart{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args map[string]interface{}
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				parts = append(parts, geminiPart{
					FunctionCall: &geminiFuncCall{
						Name: tc.Function.Name,
						Args: args,
					},
				})
			}
			contents = append(contents, geminiContent{Role: "model", Parts: parts})
			continue
		}
		role := "user"
		if m.Role == RoleAssistant {
			role = "model"
		}
		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: m.Content}},
		})
	}

	// Convert tool definitions to Gemini format.
	var geminiTools []geminiToolDeclarations
	if len(req.Tools) > 0 {
		var decls []geminiFunctionDecl
		for _, t := range req.Tools {
			decls = append(decls, geminiFunctionDecl{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			})
		}
		geminiTools = []geminiToolDeclarations{{FunctionDeclarations: decls}}
	}

	// Cap thinking budget for 2.5 models — without a cap the model can
	// spend all output tokens on thinking and produce zero content.
	var thinkingCfg *geminiThinkingCfg
	if strings.Contains(model, "2.5") {
		budget := req.MaxTokens / 4
		if budget < 1024 {
			budget = 1024
		}
		if budget > 8192 {
			budget = 8192
		}
		thinkingCfg = &geminiThinkingCfg{ThinkingBudget: budget}
		slog.Debug("gemini: capping thinking budget",
			"model", model,
			"max_output_tokens", req.MaxTokens,
			"thinking_budget", budget,
		)
	}

	body := geminiRequest{
		Contents:       contents,
		SystemInstruct: systemInstruct,
		Tools:          geminiTools,
		GenerationConfig: &geminiGenerationCfg{
			MaxOutputTokens: req.MaxTokens,
			Temperature:     req.Temperature,
			TopP:            req.TopP,
			StopSequences:   req.Stop,
			ThinkingConfig:  thinkingCfg,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.baseURL, model, p.apiKey)

	// Retry loop — 503/429 with exponential backoff: 1s → 2s → 4s.
	const maxRetries = 3
	var respBody []byte
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			slog.Warn("gemini: retrying after transient error",
				"model", model,
				"attempt", attempt+1,
				"backoff", backoff,
				"error", lastErr,
			)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("gemini request cancelled during retry: %w", ctx.Err())
			case <-time.After(backoff):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := p.client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("gemini request: %w", err)
			continue
		}

		respBody, err = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response: %w", err)
			continue
		}

		// Retryable: 429, 503.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			var errResp geminiErrorResponse
			_ = json.Unmarshal(respBody, &errResp)
			lastErr = fmt.Errorf("%w: %d — %s", ErrProviderError, resp.StatusCode, errResp.Error.Message)
			continue // retry
		}

		if resp.StatusCode != http.StatusOK {
			var errResp geminiErrorResponse
			_ = json.Unmarshal(respBody, &errResp)
			return nil, fmt.Errorf("%w: %d — %s", ErrProviderError, resp.StatusCode, errResp.Error.Message)
		}

		lastErr = nil
		break // success
	}

	if lastErr != nil {
		return nil, lastErr
	}

	var gr geminiResponse
	if err := json.Unmarshal(respBody, &gr); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(gr.Candidates) == 0 {
		return nil, fmt.Errorf("gemini returned no candidates")
	}

	// Parse response parts.
	var textContent string
	var toolCalls []ToolCall
	for i, part := range gr.Candidates[0].Content.Parts {
		if part.Text != "" {
			textContent += part.Text
		}
		if part.FunctionCall != nil {
			argsJSON, _ := json.Marshal(part.FunctionCall.Args)
			toolCalls = append(toolCalls, ToolCall{
				ID:   fmt.Sprintf("gemini-call-%d", i),
				Type: "function",
				Function: ToolCallFunc{
					Name:      part.FunctionCall.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}

	finishReason := strings.ToLower(gr.Candidates[0].FinishReason)
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	if finishReason == "max_tokens" {
		finishReason = "length"
	}

	if gr.UsageMetadata.ThoughtsTokenCount > 0 {
		slog.Info("gemini: thinking tokens consumed",
			"model", model,
			"thoughts_tokens", gr.UsageMetadata.ThoughtsTokenCount,
			"candidates_tokens", gr.UsageMetadata.CandidatesTokenCount,
			"total_tokens", gr.UsageMetadata.TotalTokenCount,
			"finish_reason", finishReason,
		)
	}

	// Report completion tokens = candidates + thoughts.
	completionTokens := gr.UsageMetadata.CandidatesTokenCount
	if completionTokens == 0 && gr.UsageMetadata.ThoughtsTokenCount > 0 {
		completionTokens = gr.UsageMetadata.ThoughtsTokenCount
	}

	return &CompletionResponse{
		Content:      textContent,
		Model:        model,
		FinishReason: finishReason,
		ToolCalls:    toolCalls,
		Usage: Usage{
			PromptTokens:     gr.UsageMetadata.PromptTokenCount,
			CompletionTokens: completionTokens,
			TotalTokens:      gr.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

func (p *GeminiProvider) ListModels(ctx context.Context) ([]string, error) {
	if p.apiKey != "" {
		url := p.baseURL + "/models?key=" + p.apiKey
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err == nil {
			resp, err := p.client.Do(httpReq)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var result struct {
						Models []struct {
							Name string `json:"name"`
						} `json:"models"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && len(result.Models) > 0 {
						models := make([]string, 0, len(result.Models))
						for _, m := range result.Models {
							name := strings.TrimPrefix(m.Name, "models/")
							if strings.HasPrefix(name, "gemini-") {
								models = append(models, name)
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

	// Fallback.
	return []string{
		"gemini-2.5-flash",
		"gemini-2.5-pro",
		"gemini-2.0-flash",
		"gemini-1.5-pro",
		"gemini-1.5-flash",
	}, nil
}

func (p *GeminiProvider) Healthy(ctx context.Context) bool {
	if p.apiKey == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := p.Complete(ctx, &CompletionRequest{
		Model:     p.defaultModel,
		Messages:  []Message{{Role: RoleUser, Content: "ping"}},
		MaxTokens: 5,
	})
	if err != nil {
		slog.Debug("gemini health check failed", "error", err)
		return false
	}
	return true
}

// ── Gemini Streaming ────────────────────────────────────────────────────────

// StreamComplete sends a streaming request to Gemini via SSE.
func (p *GeminiProvider) StreamComplete(ctx context.Context, req *CompletionRequest) (<-chan StreamChunk, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	var systemInstruct *geminiContent
	var contents []geminiContent
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			systemInstruct = &geminiContent{
				Parts: []geminiPart{{Text: m.Content}},
			}
			continue
		}
		if m.Role == RoleTool {
			var respObj map[string]interface{}
			if err := json.Unmarshal([]byte(m.Content), &respObj); err != nil {
				respObj = map[string]interface{}{"result": m.Content}
			}
			contents = append(contents, geminiContent{
				Role: "function",
				Parts: []geminiPart{{
					FunctionResponse: &geminiFuncResp{Name: m.Name, Response: respObj},
				}},
			})
			continue
		}
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			var parts []geminiPart
			if m.Content != "" {
				parts = append(parts, geminiPart{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args map[string]interface{}
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				parts = append(parts, geminiPart{
					FunctionCall: &geminiFuncCall{Name: tc.Function.Name, Args: args},
				})
			}
			contents = append(contents, geminiContent{Role: "model", Parts: parts})
			continue
		}
		role := "user"
		if m.Role == RoleAssistant {
			role = "model"
		}
		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: m.Content}},
		})
	}

	// Convert tool definitions.
	var geminiTools []geminiToolDeclarations
	if len(req.Tools) > 0 {
		var decls []geminiFunctionDecl
		for _, t := range req.Tools {
			decls = append(decls, geminiFunctionDecl{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			})
		}
		geminiTools = []geminiToolDeclarations{{FunctionDeclarations: decls}}
	}

	// Cap thinking budget for 2.5 models (same as Complete).
	var thinkingCfg *geminiThinkingCfg
	if strings.Contains(model, "2.5") {
		budget := req.MaxTokens / 4
		if budget < 1024 {
			budget = 1024
		}
		if budget > 8192 {
			budget = 8192
		}
		thinkingCfg = &geminiThinkingCfg{ThinkingBudget: budget}
	}

	body := geminiRequest{
		Contents:       contents,
		SystemInstruct: systemInstruct,
		Tools:          geminiTools,
		GenerationConfig: &geminiGenerationCfg{
			MaxOutputTokens: req.MaxTokens,
			Temperature:     req.Temperature,
			TopP:            req.TopP,
			StopSequences:   req.Stop,
			ThinkingConfig:  thinkingCfg,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", p.baseURL, model, p.apiKey)

	// Retry loop — same transient-error handling as Complete.
	const streamMaxRetries = 3
	var resp *http.Response
	var lastStreamErr error

	for attempt := 0; attempt < streamMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			slog.Warn("gemini stream: retrying after transient error",
				"model", model,
				"attempt", attempt+1,
				"backoff", backoff,
				"error", lastStreamErr,
			)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("gemini stream cancelled during retry: %w", ctx.Err())
			case <-time.After(backoff):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err = p.client.Do(httpReq)
		if err != nil {
			lastStreamErr = fmt.Errorf("gemini stream request: %w", err)
			continue
		}

		// Retryable: 429, 503.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			var errResp geminiErrorResponse
			_ = json.Unmarshal(respBody, &errResp)
			lastStreamErr = fmt.Errorf("%w: %d — %s", ErrProviderError, resp.StatusCode, errResp.Error.Message)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			var errResp geminiErrorResponse
			_ = json.Unmarshal(respBody, &errResp)
			return nil, fmt.Errorf("%w: %d — %s", ErrProviderError, resp.StatusCode, errResp.Error.Message)
		}

		lastStreamErr = nil
		break // success
	}

	if lastStreamErr != nil {
		return nil, lastStreamErr
	}

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		var toolCalls []ToolCall
		toolCallIdx := 0

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var gr geminiResponse
			if err := json.Unmarshal([]byte(data), &gr); err != nil {
				slog.Debug("gemini stream: failed to parse chunk", "error", err)
				continue
			}

			sc := StreamChunk{Model: model}
			if len(gr.Candidates) > 0 {
				for _, part := range gr.Candidates[0].Content.Parts {
					if part.Text != "" {
						sc.Content += part.Text
					}
					if part.FunctionCall != nil {
						argsJSON, _ := json.Marshal(part.FunctionCall.Args)
						toolCalls = append(toolCalls, ToolCall{
							ID:   fmt.Sprintf("gemini-call-%d", toolCallIdx),
							Type: "function",
							Function: ToolCallFunc{
								Name:      part.FunctionCall.Name,
								Arguments: string(argsJSON),
							},
						})
						toolCallIdx++
					}
				}
				if gr.Candidates[0].FinishReason != "" && gr.Candidates[0].FinishReason != "STOP" {
					sc.FinishReason = strings.ToLower(gr.Candidates[0].FinishReason)
				}
				if gr.Candidates[0].FinishReason == "STOP" {
					sc.FinishReason = "stop"
					sc.Done = true
				}
			}

			// Attach accumulated tool calls on the final chunk.
			if len(toolCalls) > 0 && (sc.Done || sc.FinishReason != "") {
				sc.ToolCalls = toolCalls
				sc.FinishReason = "tool_calls"
				sc.Done = true
			}

			if gr.UsageMetadata.TotalTokenCount > 0 {
				sc.Usage = &Usage{
					PromptTokens:     gr.UsageMetadata.PromptTokenCount,
					CompletionTokens: gr.UsageMetadata.CandidatesTokenCount,
					TotalTokens:      gr.UsageMetadata.TotalTokenCount,
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
var _ StreamingProvider = (*GeminiProvider)(nil)
