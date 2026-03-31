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

type HubSpotProvider struct {
	client *http.Client
}

func NewHubSpotProvider() *HubSpotProvider {
	return &HubSpotProvider{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

const hubspotBaseURL = "https://api.hubapi.com"

func (p *HubSpotProvider) Name() string     { return "hubspot" }
func (p *HubSpotProvider) Category() string { return "crm" }
func (p *HubSpotProvider) Description() string {
	return "Contacts, companies, deals, tickets, and CRM pipeline management."
}

func (p *HubSpotProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{Name: "list_contacts", Description: "List contacts.", OptionalParams: []string{"limit", "after"}},
		{Name: "get_contact", Description: "Get contact details.", RequiredParams: []string{"contact_id"}},
		{Name: "create_contact", Description: "Create a new contact.", RequiredParams: []string{"email"}, OptionalParams: []string{"firstname", "lastname", "phone", "company"}, ConsentRequired: true},
		{Name: "list_companies", Description: "List companies.", OptionalParams: []string{"limit", "after"}},
		{Name: "get_company", Description: "Get company details.", RequiredParams: []string{"company_id"}},
		{Name: "list_deals", Description: "List deals.", OptionalParams: []string{"limit", "after"}},
		{Name: "get_deal", Description: "Get deal details.", RequiredParams: []string{"deal_id"}},
		{Name: "search_crm", Description: "Search CRM objects.", RequiredParams: []string{"object_type", "query"}, OptionalParams: []string{"limit"}},
	}
}

func (p *HubSpotProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "list_contacts":
		q := url.Values{}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		if a := stringParam(params, "after", ""); a != "" {
			q.Set("after", a)
		}
		return p.doGet(ctx, "/crm/v3/objects/contacts?"+q.Encode(), token)

	case "get_contact":
		cid := stringParam(params, "contact_id", "")
		return p.doGet(ctx, fmt.Sprintf("/crm/v3/objects/contacts/%s", cid), token)

	case "create_contact":
		props := map[string]interface{}{"email": stringParam(params, "email", "")}
		if f := stringParam(params, "firstname", ""); f != "" {
			props["firstname"] = f
		}
		if l := stringParam(params, "lastname", ""); l != "" {
			props["lastname"] = l
		}
		if ph := stringParam(params, "phone", ""); ph != "" {
			props["phone"] = ph
		}
		if c := stringParam(params, "company", ""); c != "" {
			props["company"] = c
		}
		return p.doPost(ctx, "/crm/v3/objects/contacts", map[string]interface{}{"properties": props}, token)

	case "list_companies":
		q := url.Values{}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		if a := stringParam(params, "after", ""); a != "" {
			q.Set("after", a)
		}
		return p.doGet(ctx, "/crm/v3/objects/companies?"+q.Encode(), token)

	case "get_company":
		cid := stringParam(params, "company_id", "")
		return p.doGet(ctx, fmt.Sprintf("/crm/v3/objects/companies/%s", cid), token)

	case "list_deals":
		q := url.Values{}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		if a := stringParam(params, "after", ""); a != "" {
			q.Set("after", a)
		}
		return p.doGet(ctx, "/crm/v3/objects/deals?"+q.Encode(), token)

	case "get_deal":
		did := stringParam(params, "deal_id", "")
		return p.doGet(ctx, fmt.Sprintf("/crm/v3/objects/deals/%s", did), token)

	case "search_crm":
		objType := stringParam(params, "object_type", "contacts")
		body := map[string]interface{}{
			"query": stringParam(params, "query", ""),
		}
		if l := stringParam(params, "limit", ""); l != "" {
			body["limit"] = l
		}
		return p.doPost(ctx, fmt.Sprintf("/crm/v3/objects/%s/search", objType), body, token)

	default:
		return nil, fmt.Errorf("unknown HubSpot action %q", action)
	}
}

func (p *HubSpotProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("HubSpot uses API keys; refresh not supported")
}

func (p *HubSpotProvider) doGet(ctx context.Context, path, token string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hubspotBaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return doAPICall(p.client, req)
}

func (p *HubSpotProvider) doPost(ctx context.Context, path string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubspotBaseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}
