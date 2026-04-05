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

// ── Branch / Commit / PR Creation ───────────────────────────────────────────

// CreateBranch creates a new branch from the given commit SHA.
func (c *Client) CreateBranch(ctx context.Context, owner, repo string, req *vcs.CreateBranchRequest) error {
	projectPath := fmt.Sprintf("%s%%2F%s", owner, repo)
	url := fmt.Sprintf("%s/projects/%s/repository/branches?branch=%s&ref=%s",
		c.baseURL, projectPath, req.BranchName, req.FromSHA)

	if err := c.doJSON(ctx, http.MethodPost, url, nil, nil); err != nil {
		return fmt.Errorf("create branch %s: %w", req.BranchName, err)
	}
	slog.Info("gitlab: created branch", "project", owner+"/"+repo, "branch", req.BranchName)
	return nil
}

// CreateOrUpdateFile creates or updates a file on a branch and commits it.
// Returns the new commit SHA.
func (c *Client) CreateOrUpdateFile(ctx context.Context, owner, repo, branch string, file *vcs.FileCommit) (string, error) {
	projectPath := fmt.Sprintf("%s%%2F%s", owner, repo)
	encodedPath := strings.ReplaceAll(file.Path, "/", "%2F")

	// Check if the file already exists to decide create vs update.
	method := http.MethodPost // create
	checkURL := fmt.Sprintf("%s/projects/%s/repository/files/%s?ref=%s",
		c.baseURL, projectPath, encodedPath, branch)

	checkReq, _ := http.NewRequestWithContext(ctx, http.MethodHead, checkURL, nil)
	checkReq.Header.Set("PRIVATE-TOKEN", c.token)
	if resp, err := c.client.Do(checkReq); err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			method = http.MethodPut // update
		}
	}

	url := fmt.Sprintf("%s/projects/%s/repository/files/%s",
		c.baseURL, projectPath, encodedPath)
	body := map[string]interface{}{
		"branch":         branch,
		"content":        file.Content,
		"commit_message": file.Message,
	}

	var result struct {
		FilePath string `json:"file_path"`
	}
	if err := c.doJSON(ctx, method, url, body, &result); err != nil {
		return "", fmt.Errorf("create/update file %s: %w", file.Path, err)
	}

	// Fetch the latest commit SHA on the branch.
	sha, err := c.GetBranchSHA(ctx, owner, repo, branch)
	if err != nil {
		slog.Warn("gitlab: committed file but failed to get SHA", "path", file.Path, "err", err)
		return "", nil
	}

	slog.Info("gitlab: committed file", "project", owner+"/"+repo, "path", file.Path, "sha", sha)
	return sha, nil
}

// CreatePullRequest opens a new merge request on GitLab.
func (c *Client) CreatePullRequest(ctx context.Context, owner, repo string, req *vcs.CreatePullRequestRequest) (*vcs.PullRequest, error) {
	projectPath := fmt.Sprintf("%s%%2F%s", owner, repo)
	url := fmt.Sprintf("%s/projects/%s/merge_requests", c.baseURL, projectPath)

	body := map[string]interface{}{
		"source_branch": req.SourceBranch,
		"target_branch": req.TargetBranch,
		"title":         req.Title,
		"description":   req.Body,
	}

	var glMR glMergeRequest
	if err := c.doJSON(ctx, http.MethodPost, url, body, &glMR); err != nil {
		return nil, fmt.Errorf("create MR: %w", err)
	}

	pr := &vcs.PullRequest{
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
	}

	slog.Info("gitlab: created merge request", "project", owner+"/"+repo, "iid", glMR.IID, "url", glMR.WebURL)
	return pr, nil
}

// GetDefaultBranch returns the repo's default branch name.
func (c *Client) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	projectPath := fmt.Sprintf("%s%%2F%s", owner, repo)
	url := fmt.Sprintf("%s/projects/%s", c.baseURL, projectPath)

	var project struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &project); err != nil {
		return "", fmt.Errorf("get default branch: %w", err)
	}
	return project.DefaultBranch, nil
}

// GetBranchSHA returns the HEAD commit SHA for a branch.
func (c *Client) GetBranchSHA(ctx context.Context, owner, repo, branch string) (string, error) {
	projectPath := fmt.Sprintf("%s%%2F%s", owner, repo)
	url := fmt.Sprintf("%s/projects/%s/repository/branches/%s", c.baseURL, projectPath, branch)

	var branchInfo struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &branchInfo); err != nil {
		return "", fmt.Errorf("get branch SHA: %w", err)
	}
	return branchInfo.Commit.ID, nil
}

// GetCombinedStatus returns the aggregated CI pipeline status for a commit.
func (c *Client) GetCombinedStatus(ctx context.Context, owner, repo, ref string) (*vcs.CombinedStatus, error) {
	projectPath := fmt.Sprintf("%s%%2F%s", owner, repo)
	url := fmt.Sprintf("%s/projects/%s/repository/commits/%s/statuses", c.baseURL, projectPath, ref)

	var statuses []struct {
		Name        string    `json:"name"`
		Status      string    `json:"status"` // pending, running, success, failed, canceled
		Description string    `json:"description"`
		TargetURL   string    `json:"target_url"`
		CreatedAt   time.Time `json:"created_at"`
	}
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &statuses); err != nil {
		slog.Debug("gitlab commit statuses unavailable", "ref", ref, "error", err)
		return nil, nil
	}

	result := &vcs.CombinedStatus{}
	for _, s := range statuses {
		state := mapGitLabState(s.Status)
		result.Statuses = append(result.Statuses, vcs.CommitStatus{
			Context:     s.Name,
			State:       state,
			Description: s.Description,
			TargetURL:   s.TargetURL,
			CreatedAt:   s.CreatedAt,
		})
		result.Total++
		switch state {
		case vcs.CommitStatusSuccess:
			result.Passed++
		case vcs.CommitStatusFailure, vcs.CommitStatusError:
			result.Failed++
		default:
			result.Pending++
		}
	}

	if result.Total == 0 {
		result.State = vcs.CommitStatusSuccess
	} else if result.Failed > 0 {
		result.State = vcs.CommitStatusFailure
	} else if result.Pending > 0 {
		result.State = vcs.CommitStatusPending
	} else {
		result.State = vcs.CommitStatusSuccess
	}

	return result, nil
}

func mapGitLabState(s string) vcs.CommitStatusState {
	switch s {
	case "success":
		return vcs.CommitStatusSuccess
	case "failed":
		return vcs.CommitStatusFailure
	case "canceled":
		return vcs.CommitStatusError
	default:
		return vcs.CommitStatusPending
	}
}
