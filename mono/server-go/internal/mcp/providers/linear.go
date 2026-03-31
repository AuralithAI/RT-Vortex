package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/mcp"
)

type LinearProvider struct {
	client *http.Client
}

func NewLinearProvider() *LinearProvider {
	return &LinearProvider{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (p *LinearProvider) Name() string     { return "linear" }
func (p *LinearProvider) Category() string { return "project_management" }
func (p *LinearProvider) Description() string {
	return "Issues, projects, cycles, teams, and workflow management."
}

func (p *LinearProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{Name: "list_issues", Description: "List issues with optional filters.", OptionalParams: []string{"team_id", "state", "assignee_id", "first", "after"}},
		{Name: "get_issue", Description: "Get issue details.", RequiredParams: []string{"issue_id"}},
		{Name: "create_issue", Description: "Create a new issue.", RequiredParams: []string{"team_id", "title"}, OptionalParams: []string{"description", "priority", "assignee_id", "state_id", "label_ids"}},
		{Name: "update_issue", Description: "Update an issue.", RequiredParams: []string{"issue_id"}, OptionalParams: []string{"title", "description", "priority", "state_id", "assignee_id"}},
		{Name: "list_teams", Description: "List all teams.", OptionalParams: []string{"first"}},
		{Name: "list_projects", Description: "List projects.", OptionalParams: []string{"first", "after"}},
		{Name: "add_comment", Description: "Add a comment to an issue.", RequiredParams: []string{"issue_id", "body"}},
		{Name: "list_cycles", Description: "List cycles for a team.", RequiredParams: []string{"team_id"}, OptionalParams: []string{"first"}},
	}
}

func (p *LinearProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "list_issues":
		query := `query($filter: IssueFilter, $first: Int, $after: String) {
			issues(filter: $filter, first: $first, after: $after) {
				nodes { id identifier title state { name } priority assignee { name } createdAt updatedAt }
				pageInfo { hasNextPage endCursor }
			}
		}`
		vars := map[string]interface{}{}
		filter := map[string]interface{}{}
		if t := stringParam(params, "team_id", ""); t != "" {
			filter["team"] = map[string]interface{}{"id": map[string]interface{}{"eq": t}}
		}
		if s := stringParam(params, "state", ""); s != "" {
			filter["state"] = map[string]interface{}{"name": map[string]interface{}{"eq": s}}
		}
		if len(filter) > 0 {
			vars["filter"] = filter
		}
		if f := intParam(params, "first", 0); f > 0 {
			vars["first"] = f
		} else {
			vars["first"] = 50
		}
		if a := stringParam(params, "after", ""); a != "" {
			vars["after"] = a
		}
		return p.doGraphQL(ctx, query, vars, token)

	case "get_issue":
		query := `query($id: String!) {
			issue(id: $id) {
				id identifier title description state { name } priority
				assignee { name email } labels { nodes { name } }
				createdAt updatedAt
			}
		}`
		return p.doGraphQL(ctx, query, map[string]interface{}{"id": stringParam(params, "issue_id", "")}, token)

	case "create_issue":
		query := `mutation($input: IssueCreateInput!) {
			issueCreate(input: $input) {
				success issue { id identifier title url }
			}
		}`
		input := map[string]interface{}{
			"teamId": stringParam(params, "team_id", ""),
			"title":  stringParam(params, "title", ""),
		}
		if d := stringParam(params, "description", ""); d != "" {
			input["description"] = d
		}
		if pr := intParam(params, "priority", 0); pr > 0 {
			input["priority"] = pr
		}
		if a := stringParam(params, "assignee_id", ""); a != "" {
			input["assigneeId"] = a
		}
		if s := stringParam(params, "state_id", ""); s != "" {
			input["stateId"] = s
		}
		return p.doGraphQL(ctx, query, map[string]interface{}{"input": input}, token)

	case "update_issue":
		query := `mutation($id: String!, $input: IssueUpdateInput!) {
			issueUpdate(id: $id, input: $input) {
				success issue { id identifier title state { name } }
			}
		}`
		input := map[string]interface{}{}
		if t := stringParam(params, "title", ""); t != "" {
			input["title"] = t
		}
		if d := stringParam(params, "description", ""); d != "" {
			input["description"] = d
		}
		if s := stringParam(params, "state_id", ""); s != "" {
			input["stateId"] = s
		}
		if a := stringParam(params, "assignee_id", ""); a != "" {
			input["assigneeId"] = a
		}
		return p.doGraphQL(ctx, query, map[string]interface{}{"id": stringParam(params, "issue_id", ""), "input": input}, token)

	case "list_teams":
		query := `query($first: Int) { teams(first: $first) { nodes { id name key } } }`
		vars := map[string]interface{}{"first": 50}
		if f := intParam(params, "first", 0); f > 0 {
			vars["first"] = f
		}
		return p.doGraphQL(ctx, query, vars, token)

	case "list_projects":
		query := `query($first: Int, $after: String) {
			projects(first: $first, after: $after) {
				nodes { id name state startDate targetDate }
				pageInfo { hasNextPage endCursor }
			}
		}`
		vars := map[string]interface{}{"first": 50}
		if f := intParam(params, "first", 0); f > 0 {
			vars["first"] = f
		}
		if a := stringParam(params, "after", ""); a != "" {
			vars["after"] = a
		}
		return p.doGraphQL(ctx, query, vars, token)

	case "add_comment":
		query := `mutation($input: CommentCreateInput!) {
			commentCreate(input: $input) { success comment { id body } }
		}`
		input := map[string]interface{}{
			"issueId": stringParam(params, "issue_id", ""),
			"body":    stringParam(params, "body", ""),
		}
		return p.doGraphQL(ctx, query, map[string]interface{}{"input": input}, token)

	case "list_cycles":
		query := `query($filter: CycleFilter, $first: Int) {
			cycles(filter: $filter, first: $first) {
				nodes { id name number startsAt endsAt }
			}
		}`
		vars := map[string]interface{}{
			"filter": map[string]interface{}{
				"team": map[string]interface{}{"id": map[string]interface{}{"eq": stringParam(params, "team_id", "")}},
			},
			"first": 20,
		}
		return p.doGraphQL(ctx, query, vars, token)

	default:
		return nil, fmt.Errorf("unknown Linear action %q", action)
	}
}

func (p *LinearProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("Linear uses API keys; refresh not supported")
}

func (p *LinearProvider) doGraphQL(ctx context.Context, query string, variables map[string]interface{}, token string) (*mcp.Result, error) {
	payload := map[string]interface{}{"query": query, "variables": variables}
	data, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.linear.app/graphql", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}
