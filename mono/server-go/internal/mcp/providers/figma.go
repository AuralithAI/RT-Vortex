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

type FigmaProvider struct {
	client  *http.Client
	baseURL string
}

func NewFigmaProvider() *FigmaProvider {
	return &FigmaProvider{
		client:  &http.Client{Timeout: 20 * time.Second},
		baseURL: "https://api.figma.com/v1",
	}
}

func (p *FigmaProvider) Name() string     { return "figma" }
func (p *FigmaProvider) Category() string { return "design" }
func (p *FigmaProvider) Description() string {
	return "Design files, components, styles, comments, and image exports."
}

func (p *FigmaProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{Name: "get_file", Description: "Get a Figma file.", RequiredParams: []string{"file_key"}},
		{Name: "list_projects", Description: "List projects in a team.", RequiredParams: []string{"team_id"}},
		{Name: "get_comments", Description: "Get comments on a file.", RequiredParams: []string{"file_key"}},
		{Name: "post_comment", Description: "Post a comment on a file.", RequiredParams: []string{"file_key", "message"}, OptionalParams: []string{"x", "y", "node_id"}, ConsentRequired: true},
		{Name: "list_components", Description: "List published components.", RequiredParams: []string{"file_key"}},
		{Name: "get_images", Description: "Export images from a file.", RequiredParams: []string{"file_key", "ids"}, OptionalParams: []string{"format", "scale"}},
		{Name: "get_styles", Description: "List published styles.", RequiredParams: []string{"file_key"}},
		{Name: "get_team_projects", Description: "List all projects for a team.", RequiredParams: []string{"team_id"}},
	}
}

func (p *FigmaProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "get_file":
		fk := stringParam(params, "file_key", "")
		return p.doGet(ctx, fmt.Sprintf("%s/files/%s", p.baseURL, fk), token)

	case "list_projects":
		tid := stringParam(params, "team_id", "")
		return p.doGet(ctx, fmt.Sprintf("%s/teams/%s/projects", p.baseURL, tid), token)

	case "get_comments":
		fk := stringParam(params, "file_key", "")
		return p.doGet(ctx, fmt.Sprintf("%s/files/%s/comments", p.baseURL, fk), token)

	case "post_comment":
		fk := stringParam(params, "file_key", "")
		body := map[string]interface{}{
			"message": stringParam(params, "message", ""),
		}
		if nid := stringParam(params, "node_id", ""); nid != "" {
			body["client_meta"] = map[string]interface{}{"node_id": nid}
		} else if x := stringParam(params, "x", ""); x != "" {
			body["client_meta"] = map[string]interface{}{
				"x": x,
				"y": stringParam(params, "y", "0"),
			}
		}
		return p.doPost(ctx, fmt.Sprintf("%s/files/%s/comments", p.baseURL, fk), body, token)

	case "list_components":
		fk := stringParam(params, "file_key", "")
		return p.doGet(ctx, fmt.Sprintf("%s/files/%s/components", p.baseURL, fk), token)

	case "get_images":
		fk := stringParam(params, "file_key", "")
		ids := stringParam(params, "ids", "")
		format := stringParam(params, "format", "png")
		scale := stringParam(params, "scale", "1")
		return p.doGet(ctx, fmt.Sprintf("%s/images/%s?ids=%s&format=%s&scale=%s", p.baseURL, fk, ids, format, scale), token)

	case "get_styles":
		fk := stringParam(params, "file_key", "")
		return p.doGet(ctx, fmt.Sprintf("%s/files/%s/styles", p.baseURL, fk), token)

	case "get_team_projects":
		tid := stringParam(params, "team_id", "")
		return p.doGet(ctx, fmt.Sprintf("%s/teams/%s/projects", p.baseURL, tid), token)

	default:
		return nil, fmt.Errorf("unknown Figma action %q", action)
	}
}

func (p *FigmaProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("Figma uses personal access tokens; refresh not supported")
}

func (p *FigmaProvider) doGet(ctx context.Context, u, token string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Figma-Token", token)
	req.Header.Set("Accept", "application/json")
	return doAPICall(p.client, req)
}

func (p *FigmaProvider) doPost(ctx context.Context, u string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Figma-Token", token)
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}
