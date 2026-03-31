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

type AsanaProvider struct {
	client  *http.Client
	baseURL string
}

func NewAsanaProvider() *AsanaProvider {
	return &AsanaProvider{
		client:  &http.Client{Timeout: 20 * time.Second},
		baseURL: "https://app.asana.com/api/1.0",
	}
}

func (p *AsanaProvider) Name() string     { return "asana" }
func (p *AsanaProvider) Category() string { return "project_management" }
func (p *AsanaProvider) Description() string {
	return "Tasks, projects, workspaces, sections, and team management."
}

func (p *AsanaProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{Name: "list_tasks", Description: "List tasks in a project.", RequiredParams: []string{"project"}, OptionalParams: []string{"opt_fields", "limit"}},
		{Name: "get_task", Description: "Get task details.", RequiredParams: []string{"task_gid"}},
		{Name: "create_task", Description: "Create a new task.", RequiredParams: []string{"workspace", "name"}, OptionalParams: []string{"projects", "assignee", "due_on", "notes"}, ConsentRequired: true},
		{Name: "update_task", Description: "Update a task.", RequiredParams: []string{"task_gid"}, OptionalParams: []string{"name", "completed", "assignee", "due_on", "notes"}, ConsentRequired: true},
		{Name: "list_projects", Description: "List projects in a workspace.", RequiredParams: []string{"workspace"}, OptionalParams: []string{"limit"}},
		{Name: "list_workspaces", Description: "List workspaces."},
		{Name: "search_tasks", Description: "Search tasks.", RequiredParams: []string{"workspace", "text"}, OptionalParams: []string{"limit"}},
		{Name: "add_comment", Description: "Add a comment to a task.", RequiredParams: []string{"task_gid", "text"}, ConsentRequired: true},
	}
}

func (p *AsanaProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "list_tasks":
		q := url.Values{"project": {stringParam(params, "project", "")}}
		if f := stringParam(params, "opt_fields", ""); f != "" {
			q.Set("opt_fields", f)
		}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		return p.doGet(ctx, p.baseURL+"/tasks?"+q.Encode(), token)

	case "get_task":
		gid := stringParam(params, "task_gid", "")
		return p.doGet(ctx, fmt.Sprintf("%s/tasks/%s", p.baseURL, gid), token)

	case "create_task":
		data := map[string]interface{}{
			"workspace": stringParam(params, "workspace", ""),
			"name":      stringParam(params, "name", ""),
		}
		if pr := stringParam(params, "projects", ""); pr != "" {
			data["projects"] = splitCSV(pr)
		}
		if a := stringParam(params, "assignee", ""); a != "" {
			data["assignee"] = a
		}
		if d := stringParam(params, "due_on", ""); d != "" {
			data["due_on"] = d
		}
		if n := stringParam(params, "notes", ""); n != "" {
			data["notes"] = n
		}
		return p.doPost(ctx, p.baseURL+"/tasks", map[string]interface{}{"data": data}, token)

	case "update_task":
		gid := stringParam(params, "task_gid", "")
		data := map[string]interface{}{}
		if n := stringParam(params, "name", ""); n != "" {
			data["name"] = n
		}
		if c := stringParam(params, "completed", ""); c != "" {
			data["completed"] = c == "true"
		}
		if a := stringParam(params, "assignee", ""); a != "" {
			data["assignee"] = a
		}
		if d := stringParam(params, "due_on", ""); d != "" {
			data["due_on"] = d
		}
		if nt := stringParam(params, "notes", ""); nt != "" {
			data["notes"] = nt
		}
		return p.doPut(ctx, fmt.Sprintf("%s/tasks/%s", p.baseURL, gid), map[string]interface{}{"data": data}, token)

	case "list_projects":
		q := url.Values{"workspace": {stringParam(params, "workspace", "")}}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		return p.doGet(ctx, p.baseURL+"/projects?"+q.Encode(), token)

	case "list_workspaces":
		return p.doGet(ctx, p.baseURL+"/workspaces", token)

	case "search_tasks":
		ws := stringParam(params, "workspace", "")
		q := url.Values{"text": {stringParam(params, "text", "")}}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		return p.doGet(ctx, fmt.Sprintf("%s/workspaces/%s/tasks/search?%s", p.baseURL, ws, q.Encode()), token)

	case "add_comment":
		gid := stringParam(params, "task_gid", "")
		body := map[string]interface{}{
			"data": map[string]interface{}{"text": stringParam(params, "text", "")},
		}
		return p.doPost(ctx, fmt.Sprintf("%s/tasks/%s/stories", p.baseURL, gid), body, token)

	default:
		return nil, fmt.Errorf("unknown Asana action %q", action)
	}
}

func (p *AsanaProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("Asana token refresh not supported; use OAuth flow")
}

func (p *AsanaProvider) doGet(ctx context.Context, u, token string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return doAPICall(p.client, req)
}

func (p *AsanaProvider) doPost(ctx context.Context, u string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}

func (p *AsanaProvider) doPut(ctx context.Context, u string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}
