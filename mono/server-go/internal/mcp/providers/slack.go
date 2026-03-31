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

type SlackProvider struct {
	client  *http.Client
	baseURL string
}

func NewSlackProvider(baseURL string) *SlackProvider {
	return &SlackProvider{
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

func (p *SlackProvider) Name() string        { return "slack" }
func (p *SlackProvider) Category() string     { return "communication" }
func (p *SlackProvider) Description() string  { return "Send messages, read channels, search conversations, and share snippets." }

func (p *SlackProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{
			Name:           "list_channels",
			Description:    "List public and joined Slack channels.",
			OptionalParams: []string{"limit", "cursor"},
		},
		{
			Name:            "send_message",
			Description:     "Send a message to a Slack channel.",
			RequiredParams:  []string{"channel", "text"},
			ConsentRequired: true,
		},
		{
			Name:           "get_channel_history",
			Description:    "Retrieve recent messages from a channel.",
			RequiredParams: []string{"channel"},
			OptionalParams: []string{"limit"},
		},
		{
			Name:            "upload_snippet",
			Description:     "Upload a code snippet to a channel.",
			RequiredParams:  []string{"channel", "content"},
			OptionalParams:  []string{"filename", "title"},
			ConsentRequired: true,
		},
		{
			Name:           "search_messages",
			Description:    "Search messages across the workspace.",
			RequiredParams: []string{"query"},
			OptionalParams: []string{"count"},
		},
	}
}

func (p *SlackProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "list_channels":
		return p.listChannels(ctx, params, token)
	case "send_message":
		return p.sendMessage(ctx, params, token)
	case "get_channel_history":
		return p.getChannelHistory(ctx, params, token)
	case "upload_snippet":
		return p.uploadSnippet(ctx, params, token)
	case "search_messages":
		return p.searchMessages(ctx, params, token)
	default:
		return nil, fmt.Errorf("unknown slack action %q", action)
	}
}

func (p *SlackProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("slack uses bot tokens that do not expire; re-install the app to rotate")
}

func (p *SlackProvider) listChannels(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	q := url.Values{"types": {"public_channel,private_channel"}}
	if lim, ok := params["limit"]; ok {
		q.Set("limit", fmt.Sprintf("%v", lim))
	}
	if cur, ok := params["cursor"]; ok {
		q.Set("cursor", fmt.Sprintf("%v", cur))
	}
	return p.apiGet(ctx, p.baseURL+"/conversations.list", q, token)
}

func (p *SlackProvider) sendMessage(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	body := map[string]interface{}{
		"channel": params["channel"],
		"text":    params["text"],
	}
	return p.apiPost(ctx, p.baseURL+"/chat.postMessage", body, token)
}

func (p *SlackProvider) getChannelHistory(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	q := url.Values{"channel": {fmt.Sprintf("%v", params["channel"])}}
	if lim, ok := params["limit"]; ok {
		q.Set("limit", fmt.Sprintf("%v", lim))
	} else {
		q.Set("limit", "20")
	}
	return p.apiGet(ctx, p.baseURL+"/conversations.history", q, token)
}

func (p *SlackProvider) uploadSnippet(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	body := map[string]interface{}{
		"channels":        params["channel"],
		"content":         params["content"],
		"filename":        stringParam(params, "filename", "snippet.txt"),
		"title":           stringParam(params, "title", "Agent Snippet"),
		"filetype":        "text",
		"initial_comment": "Uploaded by RTVortex agent.",
	}
	return p.apiPost(ctx, p.baseURL+"/files.upload", body, token)
}

func (p *SlackProvider) searchMessages(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	q := url.Values{"query": {fmt.Sprintf("%v", params["query"])}}
	if cnt, ok := params["count"]; ok {
		q.Set("count", fmt.Sprintf("%v", cnt))
	} else {
		q.Set("count", "10")
	}
	return p.apiGet(ctx, p.baseURL+"/search.messages", q, token)
}

func (p *SlackProvider) apiGet(ctx context.Context, endpoint string, q url.Values, token string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return p.doRequest(req)
}

func (p *SlackProvider) apiPost(ctx context.Context, endpoint string, body map[string]interface{}, token string) (*mcp.Result, error) {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	return p.doRequest(req)
}

func (p *SlackProvider) doRequest(req *http.Request) (*mcp.Result, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("slack API request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read slack response: %w", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON from slack: %s", string(raw[:min(len(raw), 200)]))
	}

	ok, _ := data["ok"].(bool)
	if !ok {
		errStr, _ := data["error"].(string)
		return &mcp.Result{Success: false, Data: data, Error: errStr}, nil
	}

	return &mcp.Result{Success: true, Data: data}, nil
}

func stringParam(params map[string]interface{}, key, fallback string) string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return fallback
}

func intParam(params map[string]interface{}, key string, fallback int) int {
	if v, ok := params[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return fallback
}

func buildQueryString(params map[string]interface{}, keys ...string) string {
	q := url.Values{}
	for _, k := range keys {
		if v, ok := params[k]; ok {
			q.Set(k, fmt.Sprintf("%v", v))
		}
	}
	return q.Encode()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
