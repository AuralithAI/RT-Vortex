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
	Contents         []geminiContent      `json:"contents"`
	SystemInstruct   *geminiContent       `json:"systemInstruction,omitempty"`
	GenerationConfig *geminiGenerationCfg `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
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
		FinishReason string `json:"finishReason"`
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
		role := "user"
		if m.Role == RoleAssistant {
			role = "model"
		}
		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: m.Content}},
		})
	}

	body := geminiRequest{
		Contents:       contents,
		SystemInstruct: systemInstruct,
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

	content := ""
	for _, part := range gr.Candidates[0].Content.Parts {
		content += part.Text
	}

	return &CompletionResponse{
		Content:      content,
		Model:        model,
		FinishReason: strings.ToLower(gr.Candidates[0].FinishReason),
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
		role := "user"
		if m.Role == RoleAssistant {
			role = "model"
		}
		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: m.Content}},
		})
	}

	body := geminiRequest{
		Contents:       contents,
		SystemInstruct: systemInstruct,
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
