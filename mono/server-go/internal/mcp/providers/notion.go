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

type NotionProvider struct {
	client  *http.Client
	baseURL string
}

func NewNotionProvider() *NotionProvider {
	return &NotionProvider{
		client:  &http.Client{Timeout: 20 * time.Second},
		baseURL: "https://api.notion.com/v1",
	}
}

func (p *NotionProvider) Name() string        { return "notion" }
func (p *NotionProvider) Category() string     { return "productivity" }
func (p *NotionProvider) Description() string  { return "Pages, databases, blocks, comments, and full-text search." }

func (p *NotionProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{
			Name:           "search",
			Description:    "Search pages and databases across the workspace.",
			OptionalParams: []string{"query", "page_size", "filter_type"},
		},
		{
			Name:           "get_page",
			Description:    "Get a page and its properties.",
			RequiredParams: []string{"page_id"},
		},
		{
			Name:            "create_page",
			Description:     "Create a new page in a database or as a child of another page.",
			RequiredParams:  []string{"parent_id"},
			OptionalParams:  []string{"title", "properties", "content"},
			ConsentRequired: true,
		},
		{
			Name:           "get_database",
			Description:    "Get a database schema and properties.",
			RequiredParams: []string{"database_id"},
		},
		{
			Name:           "query_database",
			Description:    "Query a database with filters and sorts.",
			RequiredParams: []string{"database_id"},
			OptionalParams: []string{"filter", "sorts", "page_size"},
		},
		{
			Name:           "get_block_children",
			Description:    "Get the child blocks (content) of a page or block.",
			RequiredParams: []string{"block_id"},
			OptionalParams: []string{"page_size"},
		},
		{
			Name:            "append_block",
			Description:     "Append content blocks to a page.",
			RequiredParams:  []string{"block_id", "children"},
			ConsentRequired: true,
		},
		{
			Name:           "list_comments",
			Description:    "List comments on a page or block.",
			RequiredParams: []string{"block_id"},
		},
	}
}

func (p *NotionProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "search":
		return p.search(ctx, params, token)
	case "get_page":
		pid, _ := params["page_id"].(string)
		return p.apiGet(ctx, "/pages/"+pid, nil, token)
	case "create_page":
		return p.createPage(ctx, params, token)
	case "get_database":
		did, _ := params["database_id"].(string)
		return p.apiGet(ctx, "/databases/"+did, nil, token)
	case "query_database":
		return p.queryDatabase(ctx, params, token)
	case "get_block_children":
		bid, _ := params["block_id"].(string)
		q := url.Values{}
		if ps, ok := params["page_size"].(string); ok {
			q.Set("page_size", ps)
		}
		return p.apiGet(ctx, "/blocks/"+bid+"/children", q, token)
	case "append_block":
		return p.appendBlock(ctx, params, token)
	case "list_comments":
		bid, _ := params["block_id"].(string)
		return p.apiGet(ctx, "/comments?block_id="+bid, nil, token)
	default:
		return nil, fmt.Errorf("unknown notion action %q", action)
	}
}

func (p *NotionProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("notion uses integration tokens which do not expire")
}

func (p *NotionProvider) search(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	body := make(map[string]interface{})
	if q, ok := params["query"].(string); ok {
		body["query"] = q
	}
	if ps, ok := params["page_size"].(string); ok {
		body["page_size"] = ps
	}
	if ft, ok := params["filter_type"].(string); ok {
		body["filter"] = map[string]string{"value": ft, "property": "object"}
	}
	return p.apiPost(ctx, "/search", body, token)
}

func (p *NotionProvider) createPage(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	body := map[string]interface{}{
		"parent": map[string]string{"page_id": params["parent_id"].(string)},
	}
	if t, ok := params["title"].(string); ok {
		body["properties"] = map[string]interface{}{
			"title": []interface{}{
				map[string]interface{}{"text": map[string]string{"content": t}},
			},
		}
	}
	return p.apiPost(ctx, "/pages", body, token)
}

func (p *NotionProvider) queryDatabase(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	did, _ := params["database_id"].(string)
	body := make(map[string]interface{})
	if f, ok := params["filter"]; ok {
		body["filter"] = f
	}
	if s, ok := params["sorts"]; ok {
		body["sorts"] = s
	}
	if ps, ok := params["page_size"].(string); ok {
		body["page_size"] = ps
	}
	return p.apiPost(ctx, "/databases/"+did+"/query", body, token)
}

func (p *NotionProvider) appendBlock(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	bid, _ := params["block_id"].(string)
	body := map[string]interface{}{
		"children": params["children"],
	}
	return p.apiPatch(ctx, "/blocks/"+bid+"/children", body, token)
}

func (p *NotionProvider) apiGet(ctx context.Context, path string, q url.Values, token string) (*mcp.Result, error) {
	endpoint := p.baseURL + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", "2022-06-28")
	return doAPICall(p.client, req)
}

func (p *NotionProvider) apiPost(ctx context.Context, path string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", "2022-06-28")
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}

func (p *NotionProvider) apiPatch(ctx context.Context, path string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPatch, p.baseURL+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", "2022-06-28")
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}
