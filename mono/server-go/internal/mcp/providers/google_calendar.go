package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/mcp"
)

type GoogleCalendarProvider struct {
	client   *http.Client
	baseURL  string
	tokenURL string
}

func NewGoogleCalendarProvider(tokenURL string) *GoogleCalendarProvider {
	return &GoogleCalendarProvider{
		client:   &http.Client{Timeout: 15 * time.Second},
		baseURL:  "https://www.googleapis.com/calendar/v3",
		tokenURL: tokenURL,
	}
}

func (p *GoogleCalendarProvider) Name() string        { return "google_calendar" }
func (p *GoogleCalendarProvider) Category() string     { return "google" }
func (p *GoogleCalendarProvider) Description() string  { return "Create and manage calendar events, check availability, manage attendees." }

func (p *GoogleCalendarProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{
			Name:           "list_events",
			Description:    "List upcoming calendar events.",
			OptionalParams: []string{"time_min", "time_max", "max_results", "calendar_id"},
		},
		{
			Name:           "get_event",
			Description:    "Get details of a specific calendar event.",
			RequiredParams: []string{"event_id"},
			OptionalParams: []string{"calendar_id"},
		},
		{
			Name:            "create_event",
			Description:     "Create a new calendar event.",
			RequiredParams:  []string{"summary", "start", "end"},
			OptionalParams:  []string{"description", "attendees", "location", "calendar_id"},
			ConsentRequired: true,
		},
		{
			Name:            "update_event",
			Description:     "Update an existing calendar event.",
			RequiredParams:  []string{"event_id"},
			OptionalParams:  []string{"summary", "start", "end", "description", "attendees", "location", "calendar_id"},
			ConsentRequired: true,
		},
		{
			Name:            "delete_event",
			Description:     "Delete a calendar event.",
			RequiredParams:  []string{"event_id"},
			OptionalParams:  []string{"calendar_id"},
			ConsentRequired: true,
		},
		{
			Name:           "list_calendars",
			Description:    "List all calendars the user has access to.",
		},
		{
			Name:           "freebusy_query",
			Description:    "Check free/busy status for given time range.",
			RequiredParams: []string{"time_min", "time_max"},
			OptionalParams: []string{"calendar_ids"},
		},
	}
}

func (p *GoogleCalendarProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "list_events":
		return p.listEvents(ctx, params, token)
	case "get_event":
		return p.getEvent(ctx, params, token)
	case "create_event":
		return p.createEvent(ctx, params, token)
	case "update_event":
		return p.updateEvent(ctx, params, token)
	case "delete_event":
		return p.deleteEvent(ctx, params, token)
	case "list_calendars":
		return p.apiGet(ctx, "/users/me/calendarList", nil, token)
	case "freebusy_query":
		return p.freebusyQuery(ctx, params, token)
	default:
		return nil, fmt.Errorf("unknown google_calendar action %q", action)
	}
}

func (p *GoogleCalendarProvider) RefreshToken(ctx context.Context, refreshToken string) (string, string, time.Duration, error) {
	return googleRefreshToken(ctx, p.tokenURL, refreshToken)
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func (p *GoogleCalendarProvider) listEvents(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	q := url.Values{}
	q.Set("orderBy", "startTime")
	q.Set("singleEvents", "true")
	if v, ok := params["time_min"].(string); ok && v != "" {
		q.Set("timeMin", v)
	} else {
		q.Set("timeMin", time.Now().Format(time.RFC3339))
	}
	if v, ok := params["time_max"].(string); ok && v != "" {
		q.Set("timeMax", v)
	}
	if v, ok := params["max_results"].(string); ok && v != "" {
		q.Set("maxResults", v)
	} else {
		q.Set("maxResults", "20")
	}
	calID := "primary"
	if v, ok := params["calendar_id"].(string); ok && v != "" {
		calID = v
	}
	return p.apiGet(ctx, fmt.Sprintf("/calendars/%s/events", url.PathEscape(calID)), q, token)
}

func (p *GoogleCalendarProvider) getEvent(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	eventID, _ := params["event_id"].(string)
	calID := "primary"
	if v, ok := params["calendar_id"].(string); ok && v != "" {
		calID = v
	}
	return p.apiGet(ctx, fmt.Sprintf("/calendars/%s/events/%s", url.PathEscape(calID), url.PathEscape(eventID)), nil, token)
}

func (p *GoogleCalendarProvider) createEvent(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	calID := "primary"
	if v, ok := params["calendar_id"].(string); ok && v != "" {
		calID = v
	}
	body := map[string]interface{}{
		"summary": params["summary"],
		"start":   map[string]string{"dateTime": params["start"].(string)},
		"end":     map[string]string{"dateTime": params["end"].(string)},
	}
	if d, ok := params["description"].(string); ok {
		body["description"] = d
	}
	if l, ok := params["location"].(string); ok {
		body["location"] = l
	}
	return p.apiPost(ctx, fmt.Sprintf("/calendars/%s/events", url.PathEscape(calID)), body, token)
}

func (p *GoogleCalendarProvider) updateEvent(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	eventID, _ := params["event_id"].(string)
	calID := "primary"
	if v, ok := params["calendar_id"].(string); ok && v != "" {
		calID = v
	}
	body := make(map[string]interface{})
	if s, ok := params["summary"].(string); ok {
		body["summary"] = s
	}
	if s, ok := params["start"].(string); ok {
		body["start"] = map[string]string{"dateTime": s}
	}
	if s, ok := params["end"].(string); ok {
		body["end"] = map[string]string{"dateTime": s}
	}
	return p.apiPatch(ctx, fmt.Sprintf("/calendars/%s/events/%s", url.PathEscape(calID), url.PathEscape(eventID)), body, token)
}

func (p *GoogleCalendarProvider) deleteEvent(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	eventID, _ := params["event_id"].(string)
	calID := "primary"
	if v, ok := params["calendar_id"].(string); ok && v != "" {
		calID = v
	}
	endpoint := fmt.Sprintf("%s/calendars/%s/events/%s", p.baseURL, url.PathEscape(calID), url.PathEscape(eventID))
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.client.Do(req)
	if err != nil {
		return &mcp.Result{Success: false, Error: err.Error()}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &mcp.Result{Success: false, Error: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(b))}, nil
	}
	return &mcp.Result{Success: true, Data: map[string]interface{}{"deleted": eventID}}, nil
}

func (p *GoogleCalendarProvider) freebusyQuery(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	body := map[string]interface{}{
		"timeMin": params["time_min"],
		"timeMax": params["time_max"],
		"items":   []map[string]string{{"id": "primary"}},
	}
	return p.apiPost(ctx, "/freeBusy", body, token)
}

func (p *GoogleCalendarProvider) apiGet(ctx context.Context, path string, q url.Values, token string) (*mcp.Result, error) {
	endpoint := p.baseURL + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return doAPICall(p.client, req)
}

func (p *GoogleCalendarProvider) apiPost(ctx context.Context, path string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}

func (p *GoogleCalendarProvider) apiPatch(ctx context.Context, path string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPatch, p.baseURL+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}
