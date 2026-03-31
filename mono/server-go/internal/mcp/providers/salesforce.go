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

type SalesforceProvider struct {
	client *http.Client
}

func NewSalesforceProvider() *SalesforceProvider {
	return &SalesforceProvider{
		client: &http.Client{Timeout: 25 * time.Second},
	}
}

func (p *SalesforceProvider) Name() string     { return "salesforce" }
func (p *SalesforceProvider) Category() string { return "crm" }
func (p *SalesforceProvider) Description() string {
	return "Accounts, contacts, leads, opportunities, and SOQL queries."
}

func (p *SalesforceProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{Name: "query", Description: "Execute a SOQL query.", RequiredParams: []string{"soql"}},
		{Name: "get_record", Description: "Get a record by type and ID.", RequiredParams: []string{"object_type", "record_id"}},
		{Name: "create_record", Description: "Create a record.", RequiredParams: []string{"object_type", "fields"}, ConsentRequired: true},
		{Name: "update_record", Description: "Update a record.", RequiredParams: []string{"object_type", "record_id", "fields"}},
		{Name: "list_objects", Description: "List available SObject types."},
		{Name: "describe_object", Description: "Describe an SObject's fields.", RequiredParams: []string{"object_type"}},
		{Name: "search", Description: "SOSL search.", RequiredParams: []string{"sosl"}},
		{Name: "get_limits", Description: "Get org API usage limits."},
	}
}

func (p *SalesforceProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	// Token format: "instance_url|access_token"
	instanceURL := stringParam(params, "_instance_url", "https://yourinstance.salesforce.com")
	apiBase := instanceURL + "/services/data/v59.0"

	switch action {
	case "query":
		soql := stringParam(params, "soql", "")
		q := url.Values{"q": {soql}}
		return p.doGet(ctx, apiBase+"/query?"+q.Encode(), token)

	case "get_record":
		objType := stringParam(params, "object_type", "")
		recID := stringParam(params, "record_id", "")
		return p.doGet(ctx, fmt.Sprintf("%s/sobjects/%s/%s", apiBase, objType, recID), token)

	case "create_record":
		objType := stringParam(params, "object_type", "")
		fieldsRaw := stringParam(params, "fields", "{}")
		var fields map[string]interface{}
		if err := json.Unmarshal([]byte(fieldsRaw), &fields); err != nil {
			return nil, fmt.Errorf("invalid fields JSON: %w", err)
		}
		return p.doPost(ctx, fmt.Sprintf("%s/sobjects/%s", apiBase, objType), fields, token)

	case "update_record":
		objType := stringParam(params, "object_type", "")
		recID := stringParam(params, "record_id", "")
		fieldsRaw := stringParam(params, "fields", "{}")
		var fields map[string]interface{}
		if err := json.Unmarshal([]byte(fieldsRaw), &fields); err != nil {
			return nil, fmt.Errorf("invalid fields JSON: %w", err)
		}
		return p.doPatch(ctx, fmt.Sprintf("%s/sobjects/%s/%s", apiBase, objType, recID), fields, token)

	case "list_objects":
		return p.doGet(ctx, apiBase+"/sobjects", token)

	case "describe_object":
		objType := stringParam(params, "object_type", "")
		return p.doGet(ctx, fmt.Sprintf("%s/sobjects/%s/describe", apiBase, objType), token)

	case "search":
		sosl := stringParam(params, "sosl", "")
		q := url.Values{"q": {sosl}}
		return p.doGet(ctx, apiBase+"/search?"+q.Encode(), token)

	case "get_limits":
		return p.doGet(ctx, apiBase+"/limits", token)

	default:
		return nil, fmt.Errorf("unknown Salesforce action %q", action)
	}
}

func (p *SalesforceProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("Salesforce token refresh not implemented; reconnect via OAuth")
}

func (p *SalesforceProvider) doGet(ctx context.Context, u, token string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return doAPICall(p.client, req)
}

func (p *SalesforceProvider) doPost(ctx context.Context, u string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}

func (p *SalesforceProvider) doPatch(ctx context.Context, u string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, u, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}
