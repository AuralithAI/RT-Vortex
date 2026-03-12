package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/vcs"
)

const gitlabAPIBase = "https://gitlab.com/api/v4"

func init() {
	vcs.RegisterFactory(vcs.PlatformGitLab, func(creds *vcs.ResolvedCreds) vcs.Platform {
		return New(Config{
			Token:         creds.Token,
			WebhookSecret: creds.WebhookSecret,
			BaseURL:       creds.BaseURL,
		})
	})
}

// Config holds GitLab-specific configuration.
type Config struct {
	Token         string
	WebhookSecret string
	BaseURL       string // override for self-hosted GitLab
	Timeout       time.Duration
}

// Client implements vcs.Platform for GitLab.
type Client struct {
	token         string
	webhookSecret string
	baseURL       string
	client        *http.Client
}

// New creates a GitLab VCS client.
func New(cfg Config) *Client {
	base := cfg.BaseURL
	if base == "" {
		base = gitlabAPIBase
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		token:         cfg.Token,
		webhookSecret: cfg.WebhookSecret,
		baseURL:       strings.TrimSuffix(base, "/"),
		client:        &http.Client{Timeout: timeout},
	}
}

func (c *Client) Type() vcs.PlatformType { return vcs.PlatformGitLab }

// ── Merge Request ───────────────────────────────────────────────────────────

type glMergeRequest struct {
	IID          int       `json:"iid"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	State        string    `json:"state"` // opened, closed, merged
	WebURL       string    `json:"web_url"`
	SourceBranch string    `json:"source_branch"`
	TargetBranch string    `json:"target_branch"`
	Draft        bool      `json:"draft"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Author       struct {
		Username string `json:"username"`
	} `json:"author"`
	SHA      string `json:"sha"`
	DiffRefs struct {
		BaseSHA string `json:"base_sha"`
		HeadSHA string `json:"head_sha"`
	} `json:"diff_refs"`
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (*vcs.PullRequest, error) {
	projectPath := fmt.Sprintf("%s%%2F%s", owner, repo)
	url := fmt.Sprintf("%s/projects/%s/merge_requests/%d", c.baseURL, projectPath, number)

	var glMR glMergeRequest
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &glMR); err != nil {
		return nil, err
	}

	return &vcs.PullRequest{
		ID:           fmt.Sprintf("%d", glMR.IID),
		Number:       glMR.IID,
		Title:        glMR.Title,
		Description:  glMR.Description,
		Author:       glMR.Author.Username,
		SourceBranch: glMR.SourceBranch,
		TargetBranch: glMR.TargetBranch,
		State:        glMR.State,
		URL:          glMR.WebURL,
		HeadSHA:      glMR.DiffRefs.HeadSHA,
		BaseSHA:      glMR.DiffRefs.BaseSHA,
		Draft:        glMR.Draft,
		CreatedAt:    glMR.CreatedAt,
		UpdatedAt:    glMR.UpdatedAt,
	}, nil
}

// ListOpenPullRequests returns all open merge requests for a GitLab project.
func (c *Client) ListOpenPullRequests(ctx context.Context, owner, repo string, maxResults int) ([]vcs.PullRequest, error) {
	if maxResults <= 0 {
		maxResults = 100
	}

	projectPath := fmt.Sprintf("%s%%2F%s", owner, repo)
	var allPRs []vcs.PullRequest
	page := 1
	perPage := 100
	if maxResults < perPage {
		perPage = maxResults
	}

	for len(allPRs) < maxResults {
		url := fmt.Sprintf("%s/projects/%s/merge_requests?state=opened&per_page=%d&page=%d&order_by=updated_at&sort=desc",
			c.baseURL, projectPath, perPage, page)

		var glMRs []glMergeRequest
		if err := c.doJSON(ctx, http.MethodGet, url, nil, &glMRs); err != nil {
			return nil, fmt.Errorf("list open MRs (page %d): %w", page, err)
		}

		if len(glMRs) == 0 {
			break
		}

		for _, mr := range glMRs {
			if len(allPRs) >= maxResults {
				break
			}
			state := mr.State
			if state == "opened" {
				state = "open"
			}
			allPRs = append(allPRs, vcs.PullRequest{
				ID:           fmt.Sprintf("%d", mr.IID),
				Number:       mr.IID,
				Title:        mr.Title,
				Description:  mr.Description,
				Author:       mr.Author.Username,
				SourceBranch: mr.SourceBranch,
				TargetBranch: mr.TargetBranch,
				State:        state,
				URL:          mr.WebURL,
				HeadSHA:      mr.DiffRefs.HeadSHA,
				BaseSHA:      mr.DiffRefs.BaseSHA,
				Draft:        mr.Draft,
				CreatedAt:    mr.CreatedAt,
				UpdatedAt:    mr.UpdatedAt,
			})
		}

		if len(glMRs) < perPage {
			break
		}
		page++
	}

	return allPRs, nil
}

// ── MR Diff ─────────────────────────────────────────────────────────────────

type glDiff struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	Diff        string `json:"diff"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
}

func (c *Client) GetPullRequestDiff(ctx context.Context, owner, repo string, number int) ([]vcs.DiffFile, error) {
	projectPath := fmt.Sprintf("%s%%2F%s", owner, repo)
	url := fmt.Sprintf("%s/projects/%s/merge_requests/%d/diffs?per_page=100", c.baseURL, projectPath, number)

	var glDiffs []glDiff
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &glDiffs); err != nil {
		return nil, err
	}

	files := make([]vcs.DiffFile, len(glDiffs))
	for i, d := range glDiffs {
		status := "modified"
		if d.NewFile {
			status = "added"
		} else if d.DeletedFile {
			status = "deleted"
		} else if d.RenamedFile {
			status = "renamed"
		}
		files[i] = vcs.DiffFile{
			Filename:     d.NewPath,
			Status:       status,
			Patch:        d.Diff,
			PreviousName: d.OldPath,
		}
	}
	return files, nil
}

// ── Review Comments ─────────────────────────────────────────────────────────

func (c *Client) PostReviewComment(ctx context.Context, owner, repo string, number int, comment *vcs.ReviewCommentRequest) error {
	projectPath := fmt.Sprintf("%s%%2F%s", owner, repo)
	url := fmt.Sprintf("%s/projects/%s/merge_requests/%d/discussions", c.baseURL, projectPath, number)

	body := map[string]interface{}{
		"body": comment.Body,
		"position": map[string]interface{}{
			"position_type": "text",
			"new_path":      comment.Path,
			"new_line":      comment.Line,
			"head_sha":      comment.CommitID,
		},
	}

	return c.doJSON(ctx, http.MethodPost, url, body, nil)
}

func (c *Client) PostReviewSummary(ctx context.Context, owner, repo string, number int, body string) error {
	projectPath := fmt.Sprintf("%s%%2F%s", owner, repo)
	url := fmt.Sprintf("%s/projects/%s/merge_requests/%d/notes", c.baseURL, projectPath, number)

	noteBody := map[string]interface{}{
		"body": body,
	}

	return c.doJSON(ctx, http.MethodPost, url, noteBody, nil)
}

// ── File Content ────────────────────────────────────────────────────────────

func (c *Client) GetFileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	projectPath := fmt.Sprintf("%s%%2F%s", owner, repo)
	encodedPath := strings.ReplaceAll(path, "/", "%2F")
	url := fmt.Sprintf("%s/projects/%s/repository/files/%s/raw?ref=%s", c.baseURL, projectPath, encodedPath, ref)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, vcs.ErrRepoNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab returned %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 10<<20))
}

// ── Webhook Validation ──────────────────────────────────────────────────────

func (c *Client) ValidateWebhookSignature(_ []byte, signature string) bool {
	if c.webhookSecret == "" {
		slog.Warn("gitlab: webhook secret not configured, skipping validation")
		return true
	}
	// GitLab sends the secret in X-Gitlab-Token header (plaintext comparison).
	return signature == c.webhookSecret
}

// ── HTTP Helper ─────────────────────────────────────────────────────────────

func (c *Client) doJSON(ctx context.Context, method, url string, body interface{}, dest interface{}) error {
	var reqBody io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = strings.NewReader(string(jsonBytes))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("gitlab api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return vcs.ErrUnauthorized
	}
	if resp.StatusCode == http.StatusNotFound {
		return vcs.ErrRepoNotFound
	}
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("gitlab api %d: %s", resp.StatusCode, string(respBody))
	}

	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// ── Phase 2 stubs — branch / commit / PR creation ──────────────────────────

// CreateBranch creates a new branch from the given commit SHA.
// TODO: implement GitLab branch creation via POST /projects/:id/repository/branches.
func (c *Client) CreateBranch(ctx context.Context, owner, repo string, req *vcs.CreateBranchRequest) error {
	return fmt.Errorf("gitlab: CreateBranch not implemented")
}

// CreateOrUpdateFile creates or updates a file on a branch and commits it.
// TODO: implement via PUT /projects/:id/repository/files/:path.
func (c *Client) CreateOrUpdateFile(ctx context.Context, owner, repo, branch string, file *vcs.FileCommit) (string, error) {
	return "", fmt.Errorf("gitlab: CreateOrUpdateFile not implemented")
}

// CreatePullRequest opens a new merge request (PR) on GitLab.
// TODO: implement via POST /projects/:id/merge_requests.
func (c *Client) CreatePullRequest(ctx context.Context, owner, repo string, req *vcs.CreatePullRequestRequest) (*vcs.PullRequest, error) {
	return nil, fmt.Errorf("gitlab: CreatePullRequest not implemented")
}

// GetDefaultBranch returns the repo's default branch name.
// TODO: implement via GET /projects/:id → default_branch field.
func (c *Client) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	return "", fmt.Errorf("gitlab: GetDefaultBranch not implemented")
}

// GetBranchSHA returns the HEAD commit SHA for a branch.
// TODO: implement via GET /projects/:id/repository/branches/:branch.
func (c *Client) GetBranchSHA(ctx context.Context, owner, repo, branch string) (string, error) {
	return "", fmt.Errorf("gitlab: GetBranchSHA not implemented")
}