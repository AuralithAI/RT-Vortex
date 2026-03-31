package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/mcp"
)

type GmailProvider struct {
	client   *http.Client
	baseURL  string
	tokenURL string
}

func NewGmailProvider(baseURL, tokenURL string) *GmailProvider {
	return &GmailProvider{
		client:   &http.Client{Timeout: 15 * time.Second},
		baseURL:  strings.TrimRight(baseURL, "/"),
		tokenURL: tokenURL,
	}
}

func (p *GmailProvider) Name() string        { return "gmail" }
func (p *GmailProvider) Category() string     { return "google" }
func (p *GmailProvider) Description() string  { return "Read emails, send messages, manage labels, and search mail." }

func (p *GmailProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{
			Name:        "get_profile",
			Description: "Get the authenticated user's Gmail profile.",
		},
		{
			Name:           "list_messages",
			Description:    "List recent email messages.",
			OptionalParams: []string{"query", "max_results", "label_ids"},
		},
		{
			Name:           "get_message",
			Description:    "Get full details of a specific email.",
			RequiredParams: []string{"message_id"},
		},
		{
			Name:            "send_email",
			Description:     "Send an email via Gmail.",
			RequiredParams:  []string{"to", "subject", "body"},
			ConsentRequired: true,
		},
		{
			Name:           "list_labels",
			Description:    "List all labels in the user's mailbox.",
		},
		{
			Name:           "search_messages",
			Description:    "Search messages with Gmail query syntax.",
			RequiredParams: []string{"query"},
			OptionalParams: []string{"max_results"},
		},
	}
}

func (p *GmailProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "get_profile":
		return p.apiGet(ctx, "/users/me/profile", nil, token)
	case "list_messages":
		return p.listMessages(ctx, params, token)
	case "get_message":
		return p.getMessage(ctx, params, token)
	case "send_email":
		return p.sendEmail(ctx, params, token)
	case "list_labels":
		return p.apiGet(ctx, "/users/me/labels", nil, token)
	case "search_messages":
		return p.searchMessages(ctx, params, token)
	default:
		return nil, fmt.Errorf("unknown gmail action %q", action)
	}
}

func (p *GmailProvider) RefreshToken(ctx context.Context, refreshToken string) (string, string, time.Duration, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("gmail token refresh failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", 0, err
	}
	if result.Error != "" {
		return "", "", 0, fmt.Errorf("gmail refresh error: %s — %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", "", 0, fmt.Errorf("empty access token in gmail refresh response")
	}
	return result.AccessToken, result.RefreshToken, time.Duration(result.ExpiresIn) * time.Second, nil
}

func (p *GmailProvider) listMessages(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	q := url.Values{
		"maxResults": {fmt.Sprintf("%d", intParam(params, "max_results", 10))},
	}
	if query, ok := params["query"]; ok {
		q.Set("q", fmt.Sprintf("%v", query))
	}
	if labels, ok := params["label_ids"]; ok {
		for _, l := range splitCSV(fmt.Sprintf("%v", labels)) {
			q.Add("labelIds", l)
		}
	}
	return p.apiGet(ctx, "/users/me/messages", q, token)
}

func (p *GmailProvider) getMessage(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	msgID := fmt.Sprintf("%v", params["message_id"])
	q := url.Values{"format": {"full"}}
	return p.apiGet(ctx, fmt.Sprintf("/users/me/messages/%s", msgID), q, token)
}

func (p *GmailProvider) sendEmail(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	to := fmt.Sprintf("%v", params["to"])
	subject := fmt.Sprintf("%v", params["subject"])
	body := fmt.Sprintf("%v", params["body"])

	raw := fmt.Sprintf("To: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s", to, subject, body)

	import_b64 := encodeBase64URL([]byte(raw))
	payload := map[string]string{"raw": import_b64}
	return p.apiPost(ctx, "/users/me/messages/send", payload, token)
}

func (p *GmailProvider) searchMessages(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	q := url.Values{
		"q":          {fmt.Sprintf("%v", params["query"])},
		"maxResults": {fmt.Sprintf("%d", intParam(params, "max_results", 10))},
	}
	return p.apiGet(ctx, "/users/me/messages", q, token)
}

func (p *GmailProvider) apiGet(ctx context.Context, path string, q url.Values, token string) (*mcp.Result, error) {
	u := p.baseURL + path
	if q != nil && len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return p.doRequest(req)
}

func (p *GmailProvider) apiPost(ctx context.Context, path string, body interface{}, token string) (*mcp.Result, error) {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return p.doRequest(req)
}

func (p *GmailProvider) doRequest(req *http.Request) (*mcp.Result, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Gmail API request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read Gmail response: %w", err)
	}

	if resp.StatusCode == http.StatusNoContent {
		return &mcp.Result{Success: true, Data: map[string]interface{}{"status": "ok"}}, nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON from Gmail (status %d): %s", resp.StatusCode, string(raw[:min(len(raw), 200)]))
	}

	if resp.StatusCode >= 400 {
		errMsg := ""
		if e, ok := data["error"].(map[string]interface{}); ok {
			errMsg, _ = e["message"].(string)
		}
		return &mcp.Result{Success: false, Data: data, Error: errMsg}, nil
	}

	return &mcp.Result{Success: true, Data: data}, nil
}

func encodeBase64URL(data []byte) string {
	encoded := strings.NewReplacer("+", "-", "/", "_").Replace(
		encodeBase64StdRaw(data),
	)
	return strings.TrimRight(encoded, "=")
}

func encodeBase64StdRaw(data []byte) string {
	const base64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var result strings.Builder
	for i := 0; i < len(data); i += 3 {
		var b0, b1, b2 byte
		b0 = data[i]
		if i+1 < len(data) {
			b1 = data[i+1]
		}
		if i+2 < len(data) {
			b2 = data[i+2]
		}
		result.WriteByte(base64Chars[b0>>2])
		result.WriteByte(base64Chars[((b0&0x03)<<4)|(b1>>4)])
		if i+1 < len(data) {
			result.WriteByte(base64Chars[((b1&0x0f)<<2)|(b2>>6)])
		} else {
			result.WriteByte('=')
		}
		if i+2 < len(data) {
			result.WriteByte(base64Chars[b2&0x3f])
		} else {
			result.WriteByte('=')
		}
	}
	return result.String()
}
