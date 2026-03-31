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

type GitHubMCPProvider struct {
	client  *http.Client
	baseURL string
}

func NewGitHubMCPProvider() *GitHubMCPProvider {
	return &GitHubMCPProvider{
		client:  &http.Client{Timeout: 20 * time.Second},
		baseURL: "https://api.github.com",
	}
}

func (p *GitHubMCPProvider) Name() string        { return "github" }
func (p *GitHubMCPProvider) Category() string     { return "devops" }
func (p *GitHubMCPProvider) Description() string  { return "Issues, pull requests, repos, actions, code search, and notifications." }

func (p *GitHubMCPProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{
			Name:           "list_repos",
			Description:    "List repositories for the authenticated user.",
			OptionalParams: []string{"sort", "per_page", "page", "visibility"},
		},
		{
			Name:           "get_repo",
			Description:    "Get details of a specific repository.",
			RequiredParams: []string{"owner", "repo"},
		},
		{
			Name:           "list_issues",
			Description:    "List issues for a repository.",
			RequiredParams: []string{"owner", "repo"},
			OptionalParams: []string{"state", "labels", "per_page", "page"},
		},
		{
			Name:            "create_issue",
			Description:     "Create a new issue in a repository.",
			RequiredParams:  []string{"owner", "repo", "title"},
			OptionalParams:  []string{"body", "labels", "assignees"},
			ConsentRequired: true,
		},
		{
			Name:            "add_issue_comment",
			Description:     "Add a comment to an issue or pull request.",
			RequiredParams:  []string{"owner", "repo", "issue_number", "body"},
			ConsentRequired: true,
		},
		{
			Name:           "list_pull_requests",
			Description:    "List pull requests for a repository.",
			RequiredParams: []string{"owner", "repo"},
			OptionalParams: []string{"state", "per_page", "page"},
		},
		{
			Name:           "get_pull_request",
			Description:    "Get details of a specific pull request.",
			RequiredParams: []string{"owner", "repo", "pull_number"},
		},
		{
			Name:           "search_code",
			Description:    "Search code across GitHub repositories.",
			RequiredParams: []string{"query"},
			OptionalParams: []string{"per_page", "page"},
		},
		{
			Name:           "list_workflow_runs",
			Description:    "List recent GitHub Actions workflow runs.",
			RequiredParams: []string{"owner", "repo"},
			OptionalParams: []string{"per_page", "status"},
		},
		{
			Name:           "list_notifications",
			Description:    "List unread notifications for the authenticated user.",
			OptionalParams: []string{"per_page", "all"},
		},
		{
			Name:           "get_user",
			Description:    "Get the authenticated user's profile.",
		},
	}
}

func (p *GitHubMCPProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "list_repos":
		return p.listRepos(ctx, params, token)
	case "get_repo":
		owner, _ := params["owner"].(string)
		repo, _ := params["repo"].(string)
		return p.apiGet(ctx, fmt.Sprintf("/repos/%s/%s", owner, repo), nil, token)
	case "list_issues":
		return p.listIssues(ctx, params, token)
	case "create_issue":
		return p.createIssue(ctx, params, token)
	case "add_issue_comment":
		return p.addIssueComment(ctx, params, token)
	case "list_pull_requests":
		return p.listPullRequests(ctx, params, token)
	case "get_pull_request":
		owner, _ := params["owner"].(string)
		repo, _ := params["repo"].(string)
		prNum, _ := params["pull_number"].(string)
		return p.apiGet(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%s", owner, repo, prNum), nil, token)
	case "search_code":
		q, _ := params["query"].(string)
		qs := url.Values{"q": {q}}
		if pp, ok := params["per_page"].(string); ok {
			qs.Set("per_page", pp)
		}
		return p.apiGet(ctx, "/search/code", qs, token)
	case "list_workflow_runs":
		owner, _ := params["owner"].(string)
		repo, _ := params["repo"].(string)
		return p.apiGet(ctx, fmt.Sprintf("/repos/%s/%s/actions/runs", owner, repo), nil, token)
	case "list_notifications":
		return p.apiGet(ctx, "/notifications", nil, token)
	case "get_user":
		return p.apiGet(ctx, "/user", nil, token)
	default:
		return nil, fmt.Errorf("unknown github action %q", action)
	}
}

func (p *GitHubMCPProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	// GitHub personal access tokens / app tokens do not refresh via OAuth.
	return "", "", 0, fmt.Errorf("github tokens do not support refresh")
}

func (p *GitHubMCPProvider) listRepos(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	q := url.Values{"per_page": {stringParam(params, "per_page", "20")}}
	if s, ok := params["sort"].(string); ok {
		q.Set("sort", s)
	}
	if v, ok := params["visibility"].(string); ok {
		q.Set("visibility", v)
	}
	return p.apiGet(ctx, "/user/repos", q, token)
}

func (p *GitHubMCPProvider) listIssues(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	owner, _ := params["owner"].(string)
	repo, _ := params["repo"].(string)
	q := url.Values{"per_page": {stringParam(params, "per_page", "20")}}
	if s, ok := params["state"].(string); ok {
		q.Set("state", s)
	}
	return p.apiGet(ctx, fmt.Sprintf("/repos/%s/%s/issues", owner, repo), q, token)
}

func (p *GitHubMCPProvider) createIssue(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	owner, _ := params["owner"].(string)
	repo, _ := params["repo"].(string)
	body := map[string]interface{}{"title": params["title"]}
	if b, ok := params["body"].(string); ok {
		body["body"] = b
	}
	return p.apiPost(ctx, fmt.Sprintf("/repos/%s/%s/issues", owner, repo), body, token)
}

func (p *GitHubMCPProvider) addIssueComment(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	owner, _ := params["owner"].(string)
	repo, _ := params["repo"].(string)
	num, _ := params["issue_number"].(string)
	body := map[string]interface{}{"body": params["body"]}
	return p.apiPost(ctx, fmt.Sprintf("/repos/%s/%s/issues/%s/comments", owner, repo, num), body, token)
}

func (p *GitHubMCPProvider) listPullRequests(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	owner, _ := params["owner"].(string)
	repo, _ := params["repo"].(string)
	q := url.Values{"per_page": {stringParam(params, "per_page", "20")}}
	if s, ok := params["state"].(string); ok {
		q.Set("state", s)
	}
	return p.apiGet(ctx, fmt.Sprintf("/repos/%s/%s/pulls", owner, repo), q, token)
}

func (p *GitHubMCPProvider) apiGet(ctx context.Context, path string, q url.Values, token string) (*mcp.Result, error) {
	endpoint := p.baseURL + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return doAPICall(p.client, req)
}

func (p *GitHubMCPProvider) apiPost(ctx context.Context, path string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return doAPICall(p.client, req)
}
