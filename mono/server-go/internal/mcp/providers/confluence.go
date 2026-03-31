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

type ConfluenceProvider struct {
	client *http.Client
}

func NewConfluenceProvider() *ConfluenceProvider {
	return &ConfluenceProvider{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (p *ConfluenceProvider) Name() string     { return "confluence" }
func (p *ConfluenceProvider) Category() string { return "atlassian" }
func (p *ConfluenceProvider) Description() string {
	return "Confluence wiki pages, spaces, search, and content management."
}

func (p *ConfluenceProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{Name: "list_spaces", Description: "List all spaces.", OptionalParams: []string{"limit", "cursor"}},
		{Name: "get_space", Description: "Get space details.", RequiredParams: []string{"space_id"}},
		{Name: "get_page", Description: "Get a page by ID.", RequiredParams: []string{"page_id"}},
		{Name: "search_content", Description: "Search Confluence content.", RequiredParams: []string{"query"}, OptionalParams: []string{"limit", "cursor"}},
		{Name: "create_page", Description: "Create a new page.", RequiredParams: []string{"space_id", "title", "body"}, OptionalParams: []string{"parent_id"}, ConsentRequired: true},
		{Name: "update_page", Description: "Update a page.", RequiredParams: []string{"page_id", "title", "body", "version_number"}, ConsentRequired: true},
		{Name: "list_page_comments", Description: "List comments on a page.", RequiredParams: []string{"page_id"}},
		{Name: "delete_page", Description: "Delete a page.", RequiredParams: []string{"page_id"}, ConsentRequired: true},
	}
}

func (p *ConfluenceProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	baseURL := stringParam(params, "_base_url", "https://api.atlassian.com/wiki/api/v2")

	switch action {
	case "list_spaces":
		q := url.Values{}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		if c := stringParam(params, "cursor", ""); c != "" {
			q.Set("cursor", c)
		}
		return p.doReq(ctx, http.MethodGet, baseURL+"/spaces?"+q.Encode(), nil, token)

	case "get_space":
		sid := stringParam(params, "space_id", "")
		return p.doReq(ctx, http.MethodGet, fmt.Sprintf("%s/spaces/%s", baseURL, sid), nil, token)

	case "get_page":
		pid := stringParam(params, "page_id", "")
		return p.doReq(ctx, http.MethodGet, fmt.Sprintf("%s/pages/%s?body-format=storage", baseURL, pid), nil, token)

	case "search_content":
		q := url.Values{"cql": {stringParam(params, "query", "")}}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		return p.doReq(ctx, http.MethodGet, baseURL+"/search?"+q.Encode(), nil, token)

	case "create_page":
		body := map[string]interface{}{
			"spaceId": stringParam(params, "space_id", ""),
			"status":  "current",
			"title":   stringParam(params, "title", ""),
			"body": map[string]interface{}{
				"representation": "storage",
				"value":          stringParam(params, "body", ""),
			},
		}
		if pid := stringParam(params, "parent_id", ""); pid != "" {
			body["parentId"] = pid
		}
		return p.doReq(ctx, http.MethodPost, baseURL+"/pages", body, token)

	case "update_page":
		pid := stringParam(params, "page_id", "")
		body := map[string]interface{}{
			"id":     pid,
			"status": "current",
			"title":  stringParam(params, "title", ""),
			"body": map[string]interface{}{
				"representation": "storage",
				"value":          stringParam(params, "body", ""),
			},
			"version": map[string]interface{}{
				"number": intParam(params, "version_number", 1),
			},
		}
		return p.doReq(ctx, http.MethodPut, fmt.Sprintf("%s/pages/%s", baseURL, pid), body, token)

	case "list_page_comments":
		pid := stringParam(params, "page_id", "")
		return p.doReq(ctx, http.MethodGet, fmt.Sprintf("%s/pages/%s/footer-comments", baseURL, pid), nil, token)

	case "delete_page":
		pid := stringParam(params, "page_id", "")
		return p.doReq(ctx, http.MethodDelete, fmt.Sprintf("%s/pages/%s", baseURL, pid), nil, token)

	default:
		return nil, fmt.Errorf("unknown Confluence action %q", action)
	}
}

func (p *ConfluenceProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("Confluence token refresh not supported; use Atlassian OAuth")
}

func (p *ConfluenceProvider) doReq(ctx context.Context, method, u string, body interface{}, token string) (*mcp.Result, error) {
	var reqBody *bytes.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reqBody = bytes.NewReader(data)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	return doAPICall(p.client, req)
}
