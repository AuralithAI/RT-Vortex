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

type ZendeskProvider struct {
	client *http.Client
}

func NewZendeskProvider() *ZendeskProvider {
	return &ZendeskProvider{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (p *ZendeskProvider) Name() string     { return "zendesk" }
func (p *ZendeskProvider) Category() string { return "support" }
func (p *ZendeskProvider) Description() string {
	return "Tickets, users, organizations, search, and customer support workflows."
}

func (p *ZendeskProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{Name: "search_tickets", Description: "Search tickets.", RequiredParams: []string{"query"}, OptionalParams: []string{"per_page", "page"}},
		{Name: "get_ticket", Description: "Get ticket details.", RequiredParams: []string{"ticket_id"}},
		{Name: "create_ticket", Description: "Create a new ticket.", RequiredParams: []string{"subject", "description"}, OptionalParams: []string{"priority", "type", "assignee_id", "tags"}, ConsentRequired: true},
		{Name: "update_ticket", Description: "Update a ticket.", RequiredParams: []string{"ticket_id"}, OptionalParams: []string{"status", "priority", "assignee_id", "comment"}},
		{Name: "list_ticket_comments", Description: "List comments on a ticket.", RequiredParams: []string{"ticket_id"}},
		{Name: "add_ticket_comment", Description: "Add a comment to a ticket.", RequiredParams: []string{"ticket_id", "body"}, OptionalParams: []string{"public"}, ConsentRequired: true},
		{Name: "list_users", Description: "List users.", OptionalParams: []string{"role", "per_page"}},
		{Name: "get_user", Description: "Get user details.", RequiredParams: []string{"user_id"}},
	}
}

func (p *ZendeskProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	// Token format: subdomain:email/token:api_key  or  subdomain:bearer_token
	// We expect the base URL to be encoded in the token as "https://SUBDOMAIN.zendesk.com/api/v2"
	baseURL := stringParam(params, "_base_url", "https://yoursubdomain.zendesk.com/api/v2")

	switch action {
	case "search_tickets":
		q := url.Values{"query": {stringParam(params, "query", "")}, "type": {"ticket"}}
		if pp := stringParam(params, "per_page", ""); pp != "" {
			q.Set("per_page", pp)
		}
		return p.doGet(ctx, baseURL+"/search.json?"+q.Encode(), token)

	case "get_ticket":
		tid := stringParam(params, "ticket_id", "")
		return p.doGet(ctx, fmt.Sprintf("%s/tickets/%s.json", baseURL, tid), token)

	case "create_ticket":
		ticket := map[string]interface{}{
			"subject":     stringParam(params, "subject", ""),
			"description": stringParam(params, "description", ""),
		}
		if pr := stringParam(params, "priority", ""); pr != "" {
			ticket["priority"] = pr
		}
		if t := stringParam(params, "type", ""); t != "" {
			ticket["type"] = t
		}
		if a := stringParam(params, "assignee_id", ""); a != "" {
			ticket["assignee_id"] = a
		}
		if tags := stringParam(params, "tags", ""); tags != "" {
			ticket["tags"] = splitCSV(tags)
		}
		return p.doPost(ctx, baseURL+"/tickets.json", map[string]interface{}{"ticket": ticket}, token)

	case "update_ticket":
		tid := stringParam(params, "ticket_id", "")
		ticket := map[string]interface{}{}
		if s := stringParam(params, "status", ""); s != "" {
			ticket["status"] = s
		}
		if pr := stringParam(params, "priority", ""); pr != "" {
			ticket["priority"] = pr
		}
		if a := stringParam(params, "assignee_id", ""); a != "" {
			ticket["assignee_id"] = a
		}
		if c := stringParam(params, "comment", ""); c != "" {
			ticket["comment"] = map[string]interface{}{"body": c}
		}
		return p.doPut(ctx, fmt.Sprintf("%s/tickets/%s.json", baseURL, tid), map[string]interface{}{"ticket": ticket}, token)

	case "list_ticket_comments":
		tid := stringParam(params, "ticket_id", "")
		return p.doGet(ctx, fmt.Sprintf("%s/tickets/%s/comments.json", baseURL, tid), token)

	case "add_ticket_comment":
		tid := stringParam(params, "ticket_id", "")
		isPublic := stringParam(params, "public", "true") == "true"
		ticket := map[string]interface{}{
			"comment": map[string]interface{}{
				"body":   stringParam(params, "body", ""),
				"public": isPublic,
			},
		}
		return p.doPut(ctx, fmt.Sprintf("%s/tickets/%s.json", baseURL, tid), map[string]interface{}{"ticket": ticket}, token)

	case "list_users":
		q := url.Values{}
		if r := stringParam(params, "role", ""); r != "" {
			q.Set("role", r)
		}
		if pp := stringParam(params, "per_page", ""); pp != "" {
			q.Set("per_page", pp)
		}
		return p.doGet(ctx, baseURL+"/users.json?"+q.Encode(), token)

	case "get_user":
		uid := stringParam(params, "user_id", "")
		return p.doGet(ctx, fmt.Sprintf("%s/users/%s.json", baseURL, uid), token)

	default:
		return nil, fmt.Errorf("unknown Zendesk action %q", action)
	}
}

func (p *ZendeskProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("Zendesk uses API tokens; refresh not supported")
}

func (p *ZendeskProvider) doGet(ctx context.Context, u, token string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return doAPICall(p.client, req)
}

func (p *ZendeskProvider) doPost(ctx context.Context, u string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}

func (p *ZendeskProvider) doPut(ctx context.Context, u string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}
