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
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	Temperature     float64  `json:"temperature,omitempty"`
	TopP            float64  `json:"topP,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
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

	var systemInstruct *geminiContent
	var contents []geminiContent
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			systemInstruct = &geminiContent{
				Parts: []geminiPart{{Text: m.Content}},
			}
			continue
		}
		// Handle tool result messages: Gemini expects functionResponse parts.
		if m.Role == RoleTool {
			// Parse the content as JSON for the response object.
			var respObj map[string]interface{}
			if err := json.Unmarshal([]byte(m.Content), &respObj); err != nil {
				// If not valid JSON, wrap in a result object.
				respObj = map[string]interface{}{"result": m.Content}
			}
			contents = append(contents, geminiContent{
				Role: "function",
				Parts: []geminiPart{{
					FunctionResponse: &geminiFuncResp{
						Name:     m.Name,
						Response: respObj,
					},
				}},
			})
			continue
		}
		// Handle assistant messages with tool calls: Gemini expects functionCall parts.
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

	body := geminiRequest{
		Contents:       contents,
		SystemInstruct: systemInstruct,
		Tools:          geminiTools,
		GenerationConfig: &geminiGenerationCfg{
			MaxOutputTokens: req.MaxTokens,
			Temperature:     req.Temperature,
			TopP:            req.TopP,
			StopSequences:   req.Stop,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.baseURL, model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: %s", ErrRateLimited, string(respBody))
	}
	if resp.StatusCode != http.StatusOK {
		var errResp geminiErrorResponse
		_ = json.Unmarshal(respBody, &errResp)
		return nil, fmt.Errorf("%w: %d — %s", ErrProviderError, resp.StatusCode, errResp.Error.Message)
	}

	var gr geminiResponse
	if err := json.Unmarshal(respBody, &gr); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(gr.Candidates) == 0 {
		return nil, fmt.Errorf("gemini returned no candidates")
	}

	// Parse response parts — collect text and function calls.
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

	return &CompletionResponse{
		Content:      textContent,
		Model:        model,
		FinishReason: finishReason,
		ToolCalls:    toolCalls,
		Usage: Usage{
			PromptTokens:     gr.UsageMetadata.PromptTokenCount,
			CompletionTokens: gr.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      gr.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

func (p *GeminiProvider) ListModels(_ context.Context) ([]string, error) {
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

// StreamComplete sends a streaming request to Gemini and returns chunks via SSE.
// NOTE: Tool calling during streaming is not supported — the swarm uses
// non-streaming completions for tool calling workflows.
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

	body := geminiRequest{
		Contents:       contents,
		SystemInstruct: systemInstruct,
		Tools:          geminiTools,
		GenerationConfig: &geminiGenerationCfg{
			MaxOutputTokens: req.MaxTokens,
			Temperature:     req.Temperature,
			TopP:            req.TopP,
			StopSequences:   req.Stop,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", p.baseURL, model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("%w: %s", ErrRateLimited, string(respBody))
		}
		var errResp geminiErrorResponse
		_ = json.Unmarshal(respBody, &errResp)
		return nil, fmt.Errorf("%w: %d — %s", ErrProviderError, resp.StatusCode, errResp.Error.Message)
	}

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
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
					sc.Content += part.Text
				}
				if gr.Candidates[0].FinishReason != "" && gr.Candidates[0].FinishReason != "STOP" {
					sc.FinishReason = strings.ToLower(gr.Candidates[0].FinishReason)
				}
				if gr.Candidates[0].FinishReason == "STOP" {
					sc.FinishReason = "stop"
					sc.Done = true
				}
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
