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

type MS365Provider struct {
	client   *http.Client
	graphURL string
	tokenURL string
}

func NewMS365Provider(graphURL, tokenURL string) *MS365Provider {
	return &MS365Provider{
		client:   &http.Client{Timeout: 20 * time.Second},
		graphURL: strings.TrimRight(graphURL, "/"),
		tokenURL: tokenURL,
	}
}

func (p *MS365Provider) Name() string        { return "ms365" }
func (p *MS365Provider) Category() string     { return "microsoft" }
func (p *MS365Provider) Description() string  { return "Outlook mail, calendar, OneDrive files, Teams channels, and SharePoint." }

func (p *MS365Provider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{
			Name:        "get_profile",
			Description: "Get the authenticated user's Microsoft 365 profile.",
		},
		{
			Name:            "send_email",
			Description:     "Send an email via Microsoft Graph.",
			RequiredParams:  []string{"to", "subject", "body"},
			ConsentRequired: true,
		},
		{
			Name:           "list_emails",
			Description:    "List recent emails from the user's inbox.",
			OptionalParams: []string{"top", "filter"},
		},
		{
			Name:           "search_emails",
			Description:    "Search emails by keyword.",
			RequiredParams: []string{"query"},
			OptionalParams: []string{"top"},
		},
		{
			Name:           "list_events",
			Description:    "List upcoming calendar events.",
			OptionalParams: []string{"top"},
		},
		{
			Name:            "create_event",
			Description:     "Create a calendar event.",
			RequiredParams:  []string{"subject", "start", "end"},
			OptionalParams:  []string{"body", "attendees", "location"},
			ConsentRequired: true,
		},
		{
			Name:           "list_teams_channels",
			Description:    "List Teams channels the user has joined.",
			OptionalParams: []string{"team_id"},
		},
		{
			Name:            "send_teams_message",
			Description:     "Send a message to a Teams channel.",
			RequiredParams:  []string{"team_id", "channel_id", "text"},
			ConsentRequired: true,
		},
	}
}

func (p *MS365Provider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "get_profile":
		return p.graphGet(ctx, "/me", nil, token)
	case "send_email":
		return p.sendEmail(ctx, params, token)
	case "list_emails":
		return p.listEmails(ctx, params, token)
	case "search_emails":
		return p.searchEmails(ctx, params, token)
	case "list_events":
		return p.listEvents(ctx, params, token)
	case "create_event":
		return p.createEvent(ctx, params, token)
	case "list_teams_channels":
		return p.listTeamsChannels(ctx, params, token)
	case "send_teams_message":
		return p.sendTeamsMessage(ctx, params, token)
	default:
		return nil, fmt.Errorf("unknown ms365 action %q", action)
	}
}

func (p *MS365Provider) RefreshToken(ctx context.Context, refreshToken string) (string, string, time.Duration, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"scope":         {"https://graph.microsoft.com/.default offline_access"},
	}
	return p.oauthRefresh(ctx, p.tokenURL, data)
}

func (p *MS365Provider) sendEmail(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	to := fmt.Sprintf("%v", params["to"])
	recipients := make([]map[string]interface{}, 0)
	for _, addr := range splitCSV(to) {
		recipients = append(recipients, map[string]interface{}{
			"emailAddress": map[string]string{"address": addr},
		})
	}
	body := map[string]interface{}{
		"message": map[string]interface{}{
			"subject": params["subject"],
			"body": map[string]interface{}{
				"contentType": "Text",
				"content":     params["body"],
			},
			"toRecipients": recipients,
		},
	}
	return p.graphPost(ctx, "/me/sendMail", body, token)
}

func (p *MS365Provider) listEmails(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	q := url.Values{
		"$top":     {fmt.Sprintf("%d", intParam(params, "top", 10))},
		"$orderby": {"receivedDateTime desc"},
		"$select":  {"subject,from,receivedDateTime,bodyPreview,isRead"},
	}
	if f, ok := params["filter"]; ok {
		q.Set("$filter", fmt.Sprintf("%v", f))
	}
	return p.graphGet(ctx, "/me/messages", q, token)
}

func (p *MS365Provider) searchEmails(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	q := url.Values{
		"$search": {fmt.Sprintf(`"%v"`, params["query"])},
		"$top":    {fmt.Sprintf("%d", intParam(params, "top", 10))},
		"$select": {"subject,from,receivedDateTime,bodyPreview"},
	}
	return p.graphGet(ctx, "/me/messages", q, token)
}

func (p *MS365Provider) listEvents(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	q := url.Values{
		"$top":     {fmt.Sprintf("%d", intParam(params, "top", 10))},
		"$orderby": {"start/dateTime"},
		"$select":  {"subject,start,end,location,organizer"},
	}
	return p.graphGet(ctx, "/me/events", q, token)
}

func (p *MS365Provider) createEvent(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	event := map[string]interface{}{
		"subject": params["subject"],
		"start": map[string]string{
			"dateTime": fmt.Sprintf("%v", params["start"]),
			"timeZone": "UTC",
		},
		"end": map[string]string{
			"dateTime": fmt.Sprintf("%v", params["end"]),
			"timeZone": "UTC",
		},
	}
	if body, ok := params["body"]; ok {
		event["body"] = map[string]interface{}{
			"contentType": "Text",
			"content":     body,
		}
	}
	if loc, ok := params["location"]; ok {
		event["location"] = map[string]interface{}{"displayName": loc}
	}
	if att, ok := params["attendees"]; ok {
		attendeeList := make([]map[string]interface{}, 0)
		for _, addr := range splitCSV(fmt.Sprintf("%v", att)) {
			attendeeList = append(attendeeList, map[string]interface{}{
				"emailAddress": map[string]string{"address": addr},
				"type":         "required",
			})
		}
		event["attendees"] = attendeeList
	}
	return p.graphPost(ctx, "/me/events", event, token)
}

func (p *MS365Provider) listTeamsChannels(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	if teamID, ok := params["team_id"]; ok {
		return p.graphGet(ctx, fmt.Sprintf("/teams/%v/channels", teamID), nil, token)
	}
	return p.graphGet(ctx, "/me/joinedTeams", nil, token)
}

func (p *MS365Provider) sendTeamsMessage(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	path := fmt.Sprintf("/teams/%v/channels/%v/messages", params["team_id"], params["channel_id"])
	body := map[string]interface{}{
		"body": map[string]interface{}{
			"content": params["text"],
		},
	}
	return p.graphPost(ctx, path, body, token)
}

func (p *MS365Provider) graphGet(ctx context.Context, path string, q url.Values, token string) (*mcp.Result, error) {
	u := p.graphURL + path
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

func (p *MS365Provider) graphPost(ctx context.Context, path string, body interface{}, token string) (*mcp.Result, error) {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.graphURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return p.doRequest(req)
}

func (p *MS365Provider) doRequest(req *http.Request) (*mcp.Result, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MS Graph request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read MS Graph response: %w", err)
	}

	if resp.StatusCode == http.StatusNoContent {
		return &mcp.Result{Success: true, Data: map[string]interface{}{"status": "sent"}}, nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON from MS Graph (status %d): %s", resp.StatusCode, string(raw[:min(len(raw), 200)]))
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

func (p *MS365Provider) oauthRefresh(ctx context.Context, tokenURL string, data url.Values) (string, string, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("token refresh request failed: %w", err)
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
		return "", "", 0, fmt.Errorf("failed to decode token response: %w", err)
	}
	if result.Error != "" {
		return "", "", 0, fmt.Errorf("token refresh failed: %s — %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", "", 0, fmt.Errorf("empty access token in refresh response")
	}

	return result.AccessToken, result.RefreshToken, time.Duration(result.ExpiresIn) * time.Second, nil
}
