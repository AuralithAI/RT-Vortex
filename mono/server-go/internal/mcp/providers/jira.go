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

type JiraProvider struct {
	client *http.Client
}

func NewJiraProvider() *JiraProvider {
	return &JiraProvider{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (p *JiraProvider) Name() string        { return "jira" }
func (p *JiraProvider) Category() string     { return "atlassian" }
func (p *JiraProvider) Description() string  { return "Issues, sprints, boards, transitions, comments, and project management." }

func (p *JiraProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{
			Name:           "search_issues",
			Description:    "Search issues with JQL.",
			RequiredParams: []string{"jql"},
			OptionalParams: []string{"max_results", "start_at", "fields"},
		},
		{
			Name:           "get_issue",
			Description:    "Get details of a specific issue.",
			RequiredParams: []string{"issue_key"},
		},
		{
			Name:            "create_issue",
			Description:     "Create a new Jira issue.",
			RequiredParams:  []string{"project_key", "summary", "issue_type"},
			OptionalParams:  []string{"description", "assignee", "priority", "labels"},
			ConsentRequired: true,
		},
		{
			Name:            "add_comment",
			Description:     "Add a comment to an issue.",
			RequiredParams:  []string{"issue_key", "body"},
			ConsentRequired: true,
		},
		{
			Name:            "transition_issue",
			Description:     "Move an issue to a new status.",
			RequiredParams:  []string{"issue_key", "transition_id"},
			ConsentRequired: true,
		},
		{
			Name:           "list_projects",
			Description:    "List all Jira projects the user has access to.",
			OptionalParams: []string{"max_results"},
		},
		{
			Name:           "get_board_sprints",
			Description:    "List sprints for a board.",
			RequiredParams: []string{"board_id"},
			OptionalParams: []string{"state"},
		},
		{
			Name:            "assign_issue",
			Description:     "Assign an issue to a user.",
			RequiredParams:  []string{"issue_key", "account_id"},
			ConsentRequired: true,
		},
	}
}

func (p *JiraProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	// Jira Cloud uses: https://{site}.atlassian.net
	// The base URL is stored in the connection metadata.
	baseURL := stringParam(params, "_base_url", "")
	if baseURL == "" {
		return nil, fmt.Errorf("jira: _base_url is required (set in connection metadata)")
	}

	switch action {
	case "search_issues":
		return p.searchIssues(ctx, baseURL, params, token)
	case "get_issue":
		key, _ := params["issue_key"].(string)
		return p.apiGet(ctx, baseURL, fmt.Sprintf("/rest/api/3/issue/%s", key), nil, token)
	case "create_issue":
		return p.createIssue(ctx, baseURL, params, token)
	case "add_comment":
		return p.addComment(ctx, baseURL, params, token)
	case "transition_issue":
		return p.transitionIssue(ctx, baseURL, params, token)
	case "list_projects":
		q := url.Values{"maxResults": {stringParam(params, "max_results", "50")}}
		return p.apiGet(ctx, baseURL, "/rest/api/3/project/search", q, token)
	case "get_board_sprints":
		boardID, _ := params["board_id"].(string)
		q := url.Values{}
		if s, ok := params["state"].(string); ok {
			q.Set("state", s)
		}
		return p.apiGet(ctx, baseURL, fmt.Sprintf("/rest/agile/1.0/board/%s/sprint", boardID), q, token)
	case "assign_issue":
		key, _ := params["issue_key"].(string)
		body := map[string]interface{}{"accountId": params["account_id"]}
		return p.apiPut(ctx, baseURL, fmt.Sprintf("/rest/api/3/issue/%s/assignee", key), body, token)
	default:
		return nil, fmt.Errorf("unknown jira action %q", action)
	}
}

func (p *JiraProvider) RefreshToken(_ context.Context, refreshToken string) (string, string, time.Duration, error) {
	// Jira Cloud OAuth2 token refresh.
	// In practice, Jira uses Atlassian's OAuth2 flow which requires the client_id and client_secret.
	// For token-based connections (API keys), refresh is not supported.
	return "", "", 0, fmt.Errorf("jira token refresh requires Atlassian OAuth2 client credentials — use the OAuth flow")
}

func (p *JiraProvider) searchIssues(ctx context.Context, baseURL string, params map[string]interface{}, token string) (*mcp.Result, error) {
	q := url.Values{
		"jql":        {params["jql"].(string)},
		"maxResults": {stringParam(params, "max_results", "20")},
	}
	if s, ok := params["start_at"].(string); ok {
		q.Set("startAt", s)
	}
	if f, ok := params["fields"].(string); ok {
		q.Set("fields", f)
	}
	return p.apiGet(ctx, baseURL, "/rest/api/3/search", q, token)
}

func (p *JiraProvider) createIssue(ctx context.Context, baseURL string, params map[string]interface{}, token string) (*mcp.Result, error) {
	fields := map[string]interface{}{
		"project":   map[string]string{"key": params["project_key"].(string)},
		"summary":   params["summary"],
		"issuetype": map[string]string{"name": params["issue_type"].(string)},
	}
	if d, ok := params["description"].(string); ok {
		fields["description"] = map[string]interface{}{
			"type":    "doc",
			"version": 1,
			"content": []interface{}{
				map[string]interface{}{
					"type": "paragraph",
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": d},
					},
				},
			},
		}
	}
	body := map[string]interface{}{"fields": fields}
	return p.apiPost(ctx, baseURL, "/rest/api/3/issue", body, token)
}

func (p *JiraProvider) addComment(ctx context.Context, baseURL string, params map[string]interface{}, token string) (*mcp.Result, error) {
	key, _ := params["issue_key"].(string)
	body := map[string]interface{}{
		"body": map[string]interface{}{
			"type":    "doc",
			"version": 1,
			"content": []interface{}{
				map[string]interface{}{
					"type": "paragraph",
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": params["body"]},
					},
				},
			},
		},
	}
	return p.apiPost(ctx, baseURL, fmt.Sprintf("/rest/api/3/issue/%s/comment", key), body, token)
}

func (p *JiraProvider) transitionIssue(ctx context.Context, baseURL string, params map[string]interface{}, token string) (*mcp.Result, error) {
	key, _ := params["issue_key"].(string)
	body := map[string]interface{}{
		"transition": map[string]interface{}{"id": params["transition_id"]},
	}
	return p.apiPost(ctx, baseURL, fmt.Sprintf("/rest/api/3/issue/%s/transitions", key), body, token)
}

func (p *JiraProvider) apiGet(ctx context.Context, baseURL, path string, q url.Values, token string) (*mcp.Result, error) {
	endpoint := baseURL + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return doAPICall(p.client, req)
}

func (p *JiraProvider) apiPost(ctx context.Context, baseURL, path string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}

func (p *JiraProvider) apiPut(ctx context.Context, baseURL, path string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, baseURL+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}
