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

type GitLabProvider struct {
	baseURL string
	client  *http.Client
}

func NewGitLabProvider(baseURL string) *GitLabProvider {
	if baseURL == "" {
		baseURL = "https://gitlab.com/api/v4"
	}
	return &GitLabProvider{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 20 * time.Second},
	}
}

func (p *GitLabProvider) Name() string     { return "gitlab" }
func (p *GitLabProvider) Category() string { return "devops" }
func (p *GitLabProvider) Description() string {
	return "Projects, merge requests, issues, pipelines, and CI/CD management."
}

func (p *GitLabProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{Name: "list_projects", Description: "List accessible projects.", OptionalParams: []string{"search", "per_page", "page", "owned", "membership"}},
		{Name: "get_project", Description: "Get project details.", RequiredParams: []string{"project_id"}},
		{Name: "list_merge_requests", Description: "List merge requests.", RequiredParams: []string{"project_id"}, OptionalParams: []string{"state", "per_page", "page"}},
		{Name: "get_merge_request", Description: "Get merge request details.", RequiredParams: []string{"project_id", "mr_iid"}},
		{Name: "create_merge_request", Description: "Create a merge request.", RequiredParams: []string{"project_id", "source_branch", "target_branch", "title"}, OptionalParams: []string{"description", "assignee_id"}},
		{Name: "list_issues", Description: "List project issues.", RequiredParams: []string{"project_id"}, OptionalParams: []string{"state", "labels", "per_page", "page"}},
		{Name: "create_issue", Description: "Create an issue.", RequiredParams: []string{"project_id", "title"}, OptionalParams: []string{"description", "labels", "assignee_ids"}},
		{Name: "list_pipelines", Description: "List project pipelines.", RequiredParams: []string{"project_id"}, OptionalParams: []string{"status", "per_page", "page"}},
		{Name: "get_pipeline", Description: "Get pipeline details.", RequiredParams: []string{"project_id", "pipeline_id"}},
		{Name: "add_mr_comment", Description: "Add a comment to a merge request.", RequiredParams: []string{"project_id", "mr_iid", "body"}},
	}
}

func (p *GitLabProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "list_projects":
		q := url.Values{}
		if s := stringParam(params, "search", ""); s != "" {
			q.Set("search", s)
		}
		if s := stringParam(params, "per_page", ""); s != "" {
			q.Set("per_page", s)
		}
		if s := stringParam(params, "page", ""); s != "" {
			q.Set("page", s)
		}
		if stringParam(params, "owned", "") == "true" {
			q.Set("owned", "true")
		}
		if stringParam(params, "membership", "") == "true" {
			q.Set("membership", "true")
		}
		return p.doGet(ctx, "/projects?"+q.Encode(), token)

	case "get_project":
		pid := stringParam(params, "project_id", "")
		return p.doGet(ctx, fmt.Sprintf("/projects/%s", url.PathEscape(pid)), token)

	case "list_merge_requests":
		pid := stringParam(params, "project_id", "")
		q := url.Values{}
		if s := stringParam(params, "state", ""); s != "" {
			q.Set("state", s)
		}
		if s := stringParam(params, "per_page", ""); s != "" {
			q.Set("per_page", s)
		}
		return p.doGet(ctx, fmt.Sprintf("/projects/%s/merge_requests?%s", url.PathEscape(pid), q.Encode()), token)

	case "get_merge_request":
		pid := stringParam(params, "project_id", "")
		iid := stringParam(params, "mr_iid", "")
		return p.doGet(ctx, fmt.Sprintf("/projects/%s/merge_requests/%s", url.PathEscape(pid), iid), token)

	case "create_merge_request":
		pid := stringParam(params, "project_id", "")
		body := map[string]interface{}{
			"source_branch": stringParam(params, "source_branch", ""),
			"target_branch": stringParam(params, "target_branch", ""),
			"title":         stringParam(params, "title", ""),
		}
		if d := stringParam(params, "description", ""); d != "" {
			body["description"] = d
		}
		if a := stringParam(params, "assignee_id", ""); a != "" {
			body["assignee_id"] = a
		}
		return p.doPost(ctx, fmt.Sprintf("/projects/%s/merge_requests", url.PathEscape(pid)), body, token)

	case "list_issues":
		pid := stringParam(params, "project_id", "")
		q := url.Values{}
		if s := stringParam(params, "state", ""); s != "" {
			q.Set("state", s)
		}
		if s := stringParam(params, "labels", ""); s != "" {
			q.Set("labels", s)
		}
		if s := stringParam(params, "per_page", ""); s != "" {
			q.Set("per_page", s)
		}
		return p.doGet(ctx, fmt.Sprintf("/projects/%s/issues?%s", url.PathEscape(pid), q.Encode()), token)

	case "create_issue":
		pid := stringParam(params, "project_id", "")
		body := map[string]interface{}{"title": stringParam(params, "title", "")}
		if d := stringParam(params, "description", ""); d != "" {
			body["description"] = d
		}
		if l := stringParam(params, "labels", ""); l != "" {
			body["labels"] = l
		}
		return p.doPost(ctx, fmt.Sprintf("/projects/%s/issues", url.PathEscape(pid)), body, token)

	case "list_pipelines":
		pid := stringParam(params, "project_id", "")
		q := url.Values{}
		if s := stringParam(params, "status", ""); s != "" {
			q.Set("status", s)
		}
		if s := stringParam(params, "per_page", ""); s != "" {
			q.Set("per_page", s)
		}
		return p.doGet(ctx, fmt.Sprintf("/projects/%s/pipelines?%s", url.PathEscape(pid), q.Encode()), token)

	case "get_pipeline":
		pid := stringParam(params, "project_id", "")
		plid := stringParam(params, "pipeline_id", "")
		return p.doGet(ctx, fmt.Sprintf("/projects/%s/pipelines/%s", url.PathEscape(pid), plid), token)

	case "add_mr_comment":
		pid := stringParam(params, "project_id", "")
		iid := stringParam(params, "mr_iid", "")
		body := map[string]interface{}{"body": stringParam(params, "body", "")}
		return p.doPost(ctx, fmt.Sprintf("/projects/%s/merge_requests/%s/notes", url.PathEscape(pid), iid), body, token)

	default:
		return nil, fmt.Errorf("unknown GitLab action %q", action)
	}
}

func (p *GitLabProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("GitLab uses personal access tokens; refresh not supported")
}

func (p *GitLabProvider) doGet(ctx context.Context, path, token string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	return doAPICall(p.client, req)
}

func (p *GitLabProvider) doPost(ctx context.Context, path string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}
