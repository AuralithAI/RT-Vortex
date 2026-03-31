package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/mcp"
)

type GoogleDriveProvider struct {
	client   *http.Client
	baseURL  string
	tokenURL string
}

func NewGoogleDriveProvider(tokenURL string) *GoogleDriveProvider {
	return &GoogleDriveProvider{
		client:   &http.Client{Timeout: 30 * time.Second},
		baseURL:  "https://www.googleapis.com/drive/v3",
		tokenURL: tokenURL,
	}
}

func (p *GoogleDriveProvider) Name() string        { return "google_drive" }
func (p *GoogleDriveProvider) Category() string     { return "google" }
func (p *GoogleDriveProvider) Description() string  { return "List, search, read, and share files in Google Drive." }

func (p *GoogleDriveProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{
			Name:           "list_files",
			Description:    "List files in Google Drive.",
			OptionalParams: []string{"query", "page_size", "order_by", "page_token"},
		},
		{
			Name:           "get_file",
			Description:    "Get metadata of a specific file.",
			RequiredParams: []string{"file_id"},
		},
		{
			Name:           "read_file_content",
			Description:    "Read the text content of a Google Doc, Sheet, or exported file.",
			RequiredParams: []string{"file_id"},
			OptionalParams: []string{"mime_type"},
		},
		{
			Name:           "search_files",
			Description:    "Search files with Drive query syntax.",
			RequiredParams: []string{"query"},
			OptionalParams: []string{"page_size"},
		},
		{
			Name:            "create_file",
			Description:     "Create a new file (metadata only).",
			RequiredParams:  []string{"name"},
			OptionalParams:  []string{"mime_type", "parent_id", "description"},
			ConsentRequired: true,
		},
		{
			Name:            "share_file",
			Description:     "Share a file with a user or group.",
			RequiredParams:  []string{"file_id", "email", "role"},
			ConsentRequired: true,
		},
		{
			Name:            "delete_file",
			Description:     "Move a file to trash.",
			RequiredParams:  []string{"file_id"},
			ConsentRequired: true,
		},
	}
}

func (p *GoogleDriveProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "list_files":
		return p.listFiles(ctx, params, token)
	case "get_file":
		fid, _ := params["file_id"].(string)
		return p.apiGet(ctx, "/files/"+url.PathEscape(fid)+"?fields=*", nil, token)
	case "read_file_content":
		return p.readFileContent(ctx, params, token)
	case "search_files":
		return p.listFiles(ctx, params, token)
	case "create_file":
		return p.createFile(ctx, params, token)
	case "share_file":
		return p.shareFile(ctx, params, token)
	case "delete_file":
		fid, _ := params["file_id"].(string)
		return p.apiPost(ctx, "/files/"+url.PathEscape(fid)+"/trash", nil, token)
	default:
		return nil, fmt.Errorf("unknown google_drive action %q", action)
	}
}

func (p *GoogleDriveProvider) RefreshToken(ctx context.Context, refreshToken string) (string, string, time.Duration, error) {
	return googleRefreshToken(ctx, p.tokenURL, refreshToken)
}

func (p *GoogleDriveProvider) listFiles(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	q := url.Values{}
	q.Set("fields", "files(id,name,mimeType,modifiedTime,size,owners),nextPageToken")
	if v, ok := params["query"].(string); ok && v != "" {
		q.Set("q", v)
	}
	if v, ok := params["page_size"].(string); ok && v != "" {
		q.Set("pageSize", v)
	} else {
		q.Set("pageSize", "20")
	}
	if v, ok := params["order_by"].(string); ok && v != "" {
		q.Set("orderBy", v)
	}
	if v, ok := params["page_token"].(string); ok && v != "" {
		q.Set("pageToken", v)
	}
	return p.apiGet(ctx, "/files", q, token)
}

func (p *GoogleDriveProvider) readFileContent(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	fid, _ := params["file_id"].(string)
	mime := "text/plain"
	if v, ok := params["mime_type"].(string); ok && v != "" {
		mime = v
	}
	endpoint := fmt.Sprintf("%s/files/%s/export?mimeType=%s", p.baseURL, url.PathEscape(fid), url.QueryEscape(mime))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.client.Do(req)
	if err != nil {
		return &mcp.Result{Success: false, Error: err.Error()}, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode >= 400 {
		return &mcp.Result{Success: false, Error: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))}, nil
	}
	return &mcp.Result{Success: true, Data: map[string]interface{}{"content": string(body), "mime_type": mime}}, nil
}

func (p *GoogleDriveProvider) createFile(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	body := map[string]interface{}{
		"name": params["name"],
	}
	if v, ok := params["mime_type"].(string); ok {
		body["mimeType"] = v
	}
	if v, ok := params["parent_id"].(string); ok {
		body["parents"] = []string{v}
	}
	if v, ok := params["description"].(string); ok {
		body["description"] = v
	}
	return p.apiPost(ctx, "/files", body, token)
}

func (p *GoogleDriveProvider) shareFile(ctx context.Context, params map[string]interface{}, token string) (*mcp.Result, error) {
	fid, _ := params["file_id"].(string)
	body := map[string]interface{}{
		"type":         "user",
		"role":         params["role"],
		"emailAddress": params["email"],
	}
	return p.apiPost(ctx, fmt.Sprintf("/files/%s/permissions", url.PathEscape(fid)), body, token)
}

func (p *GoogleDriveProvider) apiGet(ctx context.Context, path string, q url.Values, token string) (*mcp.Result, error) {
	endpoint := p.baseURL + path
	if len(q) > 0 {
		if strings.Contains(endpoint, "?") {
			endpoint += "&" + q.Encode()
		} else {
			endpoint += "?" + q.Encode()
		}
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return doAPICall(p.client, req)
}

func (p *GoogleDriveProvider) apiPost(ctx context.Context, path string, body interface{}, token string) (*mcp.Result, error) {
	var reqBody io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reqBody = bytes.NewReader(data)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, reqBody)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return doAPICall(p.client, req)
}
