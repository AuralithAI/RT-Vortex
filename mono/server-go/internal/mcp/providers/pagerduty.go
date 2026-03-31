package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/mcp"
)

const pagerdutyBaseURL = "https://api.pagerduty.com"

type PagerDutyProvider struct {
	client *http.Client
}

func NewPagerDutyProvider() *PagerDutyProvider {
	return &PagerDutyProvider{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (p *PagerDutyProvider) Name() string     { return "pagerduty" }
func (p *PagerDutyProvider) Category() string { return "monitoring" }
func (p *PagerDutyProvider) Description() string {
	return "Incidents, services, on-call schedules, escalation policies, and alerts."
}

func (p *PagerDutyProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{Name: "list_incidents", Description: "List incidents.", OptionalParams: []string{"statuses", "service_ids", "since", "until", "limit"}},
		{Name: "get_incident", Description: "Get incident details.", RequiredParams: []string{"incident_id"}},
		{Name: "create_incident", Description: "Create a new incident.", RequiredParams: []string{"service_id", "title"}, OptionalParams: []string{"urgency", "body"}, ConsentRequired: true},
		{Name: "update_incident", Description: "Update an incident (acknowledge/resolve).", RequiredParams: []string{"incident_id", "status"}, OptionalParams: []string{"resolution"}},
		{Name: "list_services", Description: "List services.", OptionalParams: []string{"limit"}},
		{Name: "list_oncalls", Description: "List current on-call users.", OptionalParams: []string{"schedule_ids", "escalation_policy_ids"}},
		{Name: "list_schedules", Description: "List on-call schedules.", OptionalParams: []string{"limit"}},
		{Name: "add_incident_note", Description: "Add a note to an incident.", RequiredParams: []string{"incident_id", "content"}},
	}
}

func (p *PagerDutyProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "list_incidents":
		q := url.Values{}
		if s := stringParam(params, "statuses", ""); s != "" {
			q.Set("statuses[]", s)
		}
		if s := stringParam(params, "service_ids", ""); s != "" {
			q.Set("service_ids[]", s)
		}
		if s := stringParam(params, "since", ""); s != "" {
			q.Set("since", s)
		}
		if s := stringParam(params, "until", ""); s != "" {
			q.Set("until", s)
		}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		return p.doGet(ctx, "/incidents?"+q.Encode(), token)

	case "get_incident":
		iid := stringParam(params, "incident_id", "")
		return p.doGet(ctx, fmt.Sprintf("/incidents/%s", iid), token)

	case "create_incident":
		incident := map[string]interface{}{
			"type":    "incident",
			"title":   stringParam(params, "title", ""),
			"service": map[string]interface{}{"id": stringParam(params, "service_id", ""), "type": "service_reference"},
		}
		if u := stringParam(params, "urgency", ""); u != "" {
			incident["urgency"] = u
		}
		if b := stringParam(params, "body", ""); b != "" {
			incident["body"] = map[string]interface{}{"type": "incident_body", "details": b}
		}
		return p.doPost(ctx, "/incidents", map[string]interface{}{"incident": incident}, token)

	case "update_incident":
		iid := stringParam(params, "incident_id", "")
		incident := map[string]interface{}{
			"id":     iid,
			"type":   "incident_reference",
			"status": stringParam(params, "status", ""),
		}
		if r := stringParam(params, "resolution", ""); r != "" {
			incident["resolution"] = r
		}
		return p.doPut(ctx, fmt.Sprintf("/incidents/%s", iid), map[string]interface{}{"incident": incident}, token)

	case "list_services":
		q := url.Values{}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		return p.doGet(ctx, "/services?"+q.Encode(), token)

	case "list_oncalls":
		q := url.Values{}
		if s := stringParam(params, "schedule_ids", ""); s != "" {
			q.Set("schedule_ids[]", s)
		}
		if e := stringParam(params, "escalation_policy_ids", ""); e != "" {
			q.Set("escalation_policy_ids[]", e)
		}
		return p.doGet(ctx, "/oncalls?"+q.Encode(), token)

	case "list_schedules":
		q := url.Values{}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		return p.doGet(ctx, "/schedules?"+q.Encode(), token)

	case "add_incident_note":
		iid := stringParam(params, "incident_id", "")
		note := map[string]interface{}{
			"content": stringParam(params, "content", ""),
		}
		return p.doPost(ctx, fmt.Sprintf("/incidents/%s/notes", iid), map[string]interface{}{"note": note}, token)

	default:
		return nil, fmt.Errorf("unknown PagerDuty action %q", action)
	}
}

func (p *PagerDutyProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("PagerDuty uses API keys; refresh not supported")
}

func (p *PagerDutyProvider) doGet(ctx context.Context, path, token string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pagerdutyBaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token token="+token)
	req.Header.Set("Accept", "application/json")
	return doAPICall(p.client, req)
}

func (p *PagerDutyProvider) doPost(ctx context.Context, path string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pagerdutyBaseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token token="+token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}

func (p *PagerDutyProvider) doPut(ctx context.Context, path string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, pagerdutyBaseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token token="+token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}
