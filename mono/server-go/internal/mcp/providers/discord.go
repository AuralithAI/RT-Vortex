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

type DiscordProvider struct {
	client  *http.Client
	baseURL string
}

func NewDiscordProvider(baseURL string) *DiscordProvider {
	return &DiscordProvider{
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

func (p *DiscordProvider) Name() string        { return "discord" }
func (p *DiscordProvider) Category() string     { return "communication" }
func (p *DiscordProvider) Description() string  { return "Send messages, manage channels, create threads, and add reactions." }

func (p *DiscordProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{
			Name:        "get_current_user",
			Description: "Get the authenticated Discord user profile.",
		},
		{
			Name:        "list_guilds",
			Description: "List guilds (servers) the bot is in.",
		},
		{
			Name:           "list_channels",
			Description:    "List channels in a guild.",
			RequiredParams: []string{"guild_id"},
		},
		{
			Name:            "send_message",
			Description:     "Send a message to a Discord channel.",
			RequiredParams:  []string{"channel_id", "content"},
			ConsentRequired: true,
		},
		{
			Name:           "get_channel_messages",
			Description:    "Get recent messages from a channel.",
			RequiredParams: []string{"channel_id"},
			OptionalParams: []string{"limit"},
		},
		{
			Name:            "create_thread",
			Description:     "Create a thread in a channel.",
			RequiredParams:  []string{"channel_id", "name"},
			OptionalParams:  []string{"message"},
			ConsentRequired: true,
		},
		{
			Name:            "add_reaction",
			Description:     "Add a reaction emoji to a message.",
			RequiredParams:  []string{"channel_id", "message_id", "emoji"},
			ConsentRequired: true,
		},
	}
}

func (p *DiscordProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "get_current_user":
		return p.apiGet(ctx, "/users/@me", nil, token)
	case "list_guilds":
		return p.apiGet(ctx, "/users/@me/guilds", nil, token)
	case "list_channels":
		return p.apiGet(ctx, fmt.Sprintf("/guilds/%v/channels", params["guild_id"]), nil, token)
	case "send_message":
		return p.sendMessage(ctx, params, token)
	case "get_channel_messages":
		return p.getChannelMessages(ctx, params, token)
	case "create_thread":
		return p.createThread(ctx, params, token)
	case "add_reaction":
		return p.addReaction(ctx, params, token)
	default:
		return nil, fmt.Errorf("unknown discord action %q", action)
	}
}

func (p *DiscordProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("discord bot tokens do not expire; regenerate in the developer portal")
}

func (p *DiscordProvider) sendMessage(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	path := fmt.Sprintf("/channels/%v/messages", params["channel_id"])
	body := map[string]interface{}{
		"content": params["content"],
	}
	return p.apiPost(ctx, path, body, token)
}

func (p *DiscordProvider) getChannelMessages(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	q := url.Values{
		"limit": {fmt.Sprintf("%d", intParam(params, "limit", 20))},
	}
	path := fmt.Sprintf("/channels/%v/messages", params["channel_id"])
	return p.apiGet(ctx, path, q, token)
}

func (p *DiscordProvider) createThread(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	path := fmt.Sprintf("/channels/%v/threads", params["channel_id"])
	body := map[string]interface{}{
		"name":                  params["name"],
		"auto_archive_duration": 1440,
		"type":                  11,
	}
	if msg, ok := params["message"]; ok {
		body["message"] = map[string]interface{}{"content": msg}
	}
	return p.apiPost(ctx, path, body, token)
}

func (p *DiscordProvider) addReaction(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	emoji := url.PathEscape(fmt.Sprintf("%v", params["emoji"]))
	path := fmt.Sprintf("/channels/%v/messages/%v/reactions/%s/@me", params["channel_id"], params["message_id"], emoji)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+token)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Discord reaction request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return &mcp.Result{Success: true, Data: map[string]interface{}{"status": "reaction_added"}}, nil
	}

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	var data map[string]interface{}
	_ = json.Unmarshal(raw, &data)
	return &mcp.Result{Success: false, Data: data, Error: fmt.Sprintf("status %d", resp.StatusCode)}, nil
}

func (p *DiscordProvider) apiGet(ctx context.Context, path string, q url.Values, token string) (*mcp.Result, error) {
	u := p.baseURL + path
	if q != nil && len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+token)
	return p.doRequest(req)
}

func (p *DiscordProvider) apiPost(ctx context.Context, path string, body interface{}, token string) (*mcp.Result, error) {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("Content-Type", "application/json")
	return p.doRequest(req)
}

func (p *DiscordProvider) doRequest(req *http.Request) (*mcp.Result, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Discord API request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read Discord response: %w", err)
	}

	if resp.StatusCode == http.StatusNoContent {
		return &mcp.Result{Success: true, Data: map[string]interface{}{"status": "ok"}}, nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		if resp.StatusCode < 300 {
			var arr []interface{}
			if err2 := json.Unmarshal(raw, &arr); err2 == nil {
				return &mcp.Result{Success: true, Data: map[string]interface{}{"items": arr}}, nil
			}
		}
		return nil, fmt.Errorf("invalid JSON from Discord (status %d): %s", resp.StatusCode, string(raw[:min(len(raw), 200)]))
	}

	if resp.StatusCode >= 400 {
		errMsg, _ := data["message"].(string)
		return &mcp.Result{Success: false, Data: data, Error: errMsg}, nil
	}

	return &mcp.Result{Success: true, Data: data}, nil
}
