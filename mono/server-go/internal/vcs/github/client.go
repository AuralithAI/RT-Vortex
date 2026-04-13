package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/vcs"
)

const (
	githubAPIBase = "https://api.github.com"
)

func init() {
	vcs.RegisterFactory(vcs.PlatformGitHub, func(creds *vcs.ResolvedCreds) vcs.Platform {
		baseURL := creds.APIURL
		if baseURL == "" {
			baseURL = creds.BaseURL
		}
		return New(Config{
			Token:         creds.Token,
			WebhookSecret: creds.WebhookSecret,
			BaseURL:       baseURL,
		})
	})
}

// Config holds GitHub-specific configuration.
type Config struct {
	Token         string
	WebhookSecret string
	BaseURL       string // override for GitHub Enterprise
	Timeout       time.Duration
}

// Client implements vcs.Platform for GitHub.
type Client struct {
	token         string
	webhookSecret string
	baseURL       string
	client        *http.Client
}

// New creates a GitHub VCS client.
func New(cfg Config) *Client {
	base := cfg.BaseURL
	if base == "" {
		base = githubAPIBase
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

func (c *Client) Type() vcs.PlatformType { return vcs.PlatformGitHub }

// ── Pull Request ────────────────────────────────────────────────────────────

type ghPullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	URL       string    `json:"html_url"`
	Draft     bool      `json:"draft"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"base"`
	Merged bool `json:"merged"`
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (*vcs.PullRequest, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.baseURL, owner, repo, number)
	var ghPR ghPullRequest
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &ghPR); err != nil {
		return nil, err
	}

	state := ghPR.State
	if ghPR.Merged {
		state = "merged"
	}

	return &vcs.PullRequest{
		ID:           fmt.Sprintf("%d", ghPR.Number),
		Number:       ghPR.Number,
		Title:        ghPR.Title,
		Description:  ghPR.Body,
		Author:       ghPR.User.Login,
		SourceBranch: ghPR.Head.Ref,
		TargetBranch: ghPR.Base.Ref,
		State:        state,
		URL:          ghPR.URL,
		HeadSHA:      ghPR.Head.SHA,
		BaseSHA:      ghPR.Base.SHA,
		Draft:        ghPR.Draft,
		CreatedAt:    ghPR.CreatedAt,
		UpdatedAt:    ghPR.UpdatedAt,
	}, nil
}

// ListOpenPullRequests returns all open PRs for a GitHub repository.
// It handles pagination internally, fetching up to maxResults PRs.
func (c *Client) ListOpenPullRequests(ctx context.Context, owner, repo string, maxResults int) ([]vcs.PullRequest, error) {
	if maxResults <= 0 {
		maxResults = 100
	}

	var allPRs []vcs.PullRequest
	page := 1
	perPage := 100
	if maxResults < perPage {
		perPage = maxResults
	}

	for len(allPRs) < maxResults {
		url := fmt.Sprintf("%s/repos/%s/%s/pulls?state=open&per_page=%d&page=%d&sort=updated&direction=desc",
			c.baseURL, owner, repo, perPage, page)

		var ghPRs []ghPullRequest
		if err := c.doJSON(ctx, http.MethodGet, url, nil, &ghPRs); err != nil {
			return nil, fmt.Errorf("list open PRs (page %d): %w", page, err)
		}

		if len(ghPRs) == 0 {
			break
		}

		for _, ghPR := range ghPRs {
			if len(allPRs) >= maxResults {
				break
			}
			state := ghPR.State
			if ghPR.Merged {
				state = "merged"
			}
			allPRs = append(allPRs, vcs.PullRequest{
				ID:           fmt.Sprintf("%d", ghPR.Number),
				Number:       ghPR.Number,
				Title:        ghPR.Title,
				Description:  ghPR.Body,
				Author:       ghPR.User.Login,
				SourceBranch: ghPR.Head.Ref,
				TargetBranch: ghPR.Base.Ref,
				State:        state,
				URL:          ghPR.URL,
				HeadSHA:      ghPR.Head.SHA,
				BaseSHA:      ghPR.Base.SHA,
				Draft:        ghPR.Draft,
				CreatedAt:    ghPR.CreatedAt,
				UpdatedAt:    ghPR.UpdatedAt,
			})
		}

		if len(ghPRs) < perPage {
			break // last page
		}
		page++
	}

	return allPRs, nil
}

// ── PR Diff ─────────────────────────────────────────────────────────────────

type ghFile struct {
	Filename     string `json:"filename"`
	Status       string `json:"status"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	Patch        string `json:"patch"`
	PreviousName string `json:"previous_filename"`
}

func (c *Client) GetPullRequestDiff(ctx context.Context, owner, repo string, number int) ([]vcs.DiffFile, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files?per_page=100", c.baseURL, owner, repo, number)
	var ghFiles []ghFile
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &ghFiles); err != nil {
		return nil, err
	}

	files := make([]vcs.DiffFile, len(ghFiles))
	for i, f := range ghFiles {
		files[i] = vcs.DiffFile{
			Filename:     f.Filename,
			Status:       f.Status,
			Additions:    f.Additions,
			Deletions:    f.Deletions,
			Patch:        f.Patch,
			PreviousName: f.PreviousName,
		}
	}
	return files, nil
}

// ── Review Comments ─────────────────────────────────────────────────────────

func (c *Client) PostReviewComment(ctx context.Context, owner, repo string, number int, comment *vcs.ReviewCommentRequest) error {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/comments", c.baseURL, owner, repo, number)

	body := map[string]interface{}{
		"body":      comment.Body,
		"path":      comment.Path,
		"line":      comment.Line,
		"side":      comment.Side,
		"commit_id": comment.CommitID,
	}

	return c.doJSON(ctx, http.MethodPost, url, body, nil)
}

func (c *Client) PostReviewSummary(ctx context.Context, owner, repo string, number int, body string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews", c.baseURL, owner, repo, number)

	reviewBody := map[string]interface{}{
		"body":  body,
		"event": "COMMENT",
	}

	return c.doJSON(ctx, http.MethodPost, url, reviewBody, nil)
}

// ── File Content ────────────────────────────────────────────────────────────

func (c *Client) GetFileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", c.baseURL, owner, repo, path, ref)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3.raw")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, vcs.ErrRepoNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github returned %d", resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB limit
}

// ListDirectory returns the entries in a directory via the GitHub Contents API.
// When the path points to a directory, the API returns a JSON array of entries.
func (c *Client) ListDirectory(ctx context.Context, owner, repo, path, ref string) ([]vcs.DirEntry, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.baseURL, owner, repo, path)
	if ref != "" {
		u += "?ref=" + ref
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, vcs.ErrRepoNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github list-dir returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2 MB limit
	if err != nil {
		return nil, err
	}

	// GitHub Contents API returns a JSON array for directories.
	var items []struct {
		Name string `json:"name"`
		Type string `json:"type"` // "file", "dir", "symlink", "submodule"
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		// Might be a single file object instead of an array — return it as one entry.
		var single struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Size int64  `json:"size"`
		}
		if err2 := json.Unmarshal(body, &single); err2 == nil && single.Name != "" {
			entryType := single.Type
			if entryType != "file" && entryType != "dir" {
				entryType = "file"
			}
			return []vcs.DirEntry{{Name: single.Name, Type: entryType, Size: single.Size}}, nil
		}
		return nil, fmt.Errorf("github list-dir: failed to parse response: %w", err)
	}

	entries := make([]vcs.DirEntry, 0, len(items))
	for _, item := range items {
		entryType := item.Type
		if entryType != "file" && entryType != "dir" {
			entryType = "file" // symlinks, submodules → treat as file
		}
		entries = append(entries, vcs.DirEntry{
			Name: item.Name,
			Type: entryType,
			Size: item.Size,
		})
	}
	return entries, nil
}

// ── Webhook Validation ──────────────────────────────────────────────────────

func (c *Client) ValidateWebhookSignature(payload []byte, signature string) bool {
	if c.webhookSecret == "" {
		slog.Warn("github: webhook secret not configured, skipping validation")
		return true
	}
	sig := strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(c.webhookSecret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// ── Branch / Commit / PR Creation ────────────────────────────────────────────

// GetDefaultBranch returns the default branch for a GitHub repo (e.g. "main").
func (c *Client) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, owner, repo)
	var repoInfo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &repoInfo); err != nil {
		return "", fmt.Errorf("get default branch: %w", err)
	}
	return repoInfo.DefaultBranch, nil
}

// GetBranchSHA returns the HEAD commit SHA for a branch.
func (c *Client) GetBranchSHA(ctx context.Context, owner, repo, branch string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/git/ref/heads/%s", c.baseURL, owner, repo, branch)
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &ref); err != nil {
		return "", fmt.Errorf("get branch SHA: %w", err)
	}
	return ref.Object.SHA, nil
}

// CreateBranch creates a new branch from the given commit SHA.
func (c *Client) CreateBranch(ctx context.Context, owner, repo string, req *vcs.CreateBranchRequest) error {
	url := fmt.Sprintf("%s/repos/%s/%s/git/refs", c.baseURL, owner, repo)
	body := map[string]interface{}{
		"ref": "refs/heads/" + req.BranchName,
		"sha": req.FromSHA,
	}
	if err := c.doJSON(ctx, http.MethodPost, url, body, nil); err != nil {
		return fmt.Errorf("create branch %s: %w", req.BranchName, err)
	}
	slog.Info("github: created branch", "owner", owner, "repo", repo, "branch", req.BranchName)
	return nil
}

// CreateOrUpdateFile creates or updates a single file on a branch and commits it.
// It uses the GitHub Contents API (PUT /repos/{owner}/{repo}/contents/{path}).
// Returns the new commit SHA.
func (c *Client) CreateOrUpdateFile(ctx context.Context, owner, repo, branch string, file *vcs.FileCommit) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.baseURL, owner, repo, file.Path)

	body := map[string]interface{}{
		"message": file.Message,
		"content": base64Encode([]byte(file.Content)),
		"branch":  branch,
	}

	// Check if the file already exists to get its SHA (required for updates).
	existingContent, err := c.GetFileContent(ctx, owner, repo, file.Path, branch)
	if err == nil && len(existingContent) > 0 {
		// File exists — we need the blob SHA for the update.
		blobSHA, shaErr := c.getFileBlobSHA(ctx, owner, repo, file.Path, branch)
		if shaErr == nil && blobSHA != "" {
			body["sha"] = blobSHA
		}
	}

	var result struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := c.doJSON(ctx, http.MethodPut, url, body, &result); err != nil {
		return "", fmt.Errorf("create/update file %s: %w", file.Path, err)
	}
	slog.Info("github: committed file", "owner", owner, "repo", repo, "path", file.Path, "sha", result.Commit.SHA)
	return result.Commit.SHA, nil
}

// getFileBlobSHA returns the blob SHA for a file on a branch (needed for updates).
func (c *Client) getFileBlobSHA(ctx context.Context, owner, repo, path, ref string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", c.baseURL, owner, repo, path, ref)
	var info struct {
		SHA string `json:"sha"`
	}
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &info); err != nil {
		return "", err
	}
	return info.SHA, nil
}

// CreatePullRequest opens a new pull request and returns it.
func (c *Client) CreatePullRequest(ctx context.Context, owner, repo string, req *vcs.CreatePullRequestRequest) (*vcs.PullRequest, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls", c.baseURL, owner, repo)
	body := map[string]interface{}{
		"title": req.Title,
		"body":  req.Body,
		"head":  req.SourceBranch,
		"base":  req.TargetBranch,
		"draft": req.Draft,
	}

	var ghPR ghPullRequest
	if err := c.doJSON(ctx, http.MethodPost, url, body, &ghPR); err != nil {
		return nil, fmt.Errorf("create PR: %w", err)
	}

	state := ghPR.State
	if ghPR.Merged {
		state = "merged"
	}

	pr := &vcs.PullRequest{
		ID:           fmt.Sprintf("%d", ghPR.Number),
		Number:       ghPR.Number,
		Title:        ghPR.Title,
		Description:  ghPR.Body,
		Author:       ghPR.User.Login,
		SourceBranch: ghPR.Head.Ref,
		TargetBranch: ghPR.Base.Ref,
		State:        state,
		URL:          ghPR.URL,
		HeadSHA:      ghPR.Head.SHA,
		BaseSHA:      ghPR.Base.SHA,
		Draft:        ghPR.Draft,
		CreatedAt:    ghPR.CreatedAt,
		UpdatedAt:    ghPR.UpdatedAt,
	}

	slog.Info("github: created pull request", "owner", owner, "repo", repo, "number", ghPR.Number, "url", ghPR.URL)
	return pr, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// base64Encode encodes bytes to a standard base64 string.
func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// ── CI Commit Status ────────────────────────────────────────────────────────

// ghCombinedStatus models GitHub's GET /repos/{owner}/{repo}/commits/{ref}/status.
type ghCombinedStatus struct {
	State    string         `json:"state"` // pending, success, failure
	Statuses []ghStatusItem `json:"statuses"`
}

type ghStatusItem struct {
	Context     string    `json:"context"`
	State       string    `json:"state"` // pending, success, failure, error
	Description string    `json:"description"`
	TargetURL   string    `json:"target_url"`
	CreatedAt   time.Time `json:"created_at"`
}

// ghCheckRuns models GitHub's GET /repos/{owner}/{repo}/commits/{ref}/check-runs.
type ghCheckRunsResp struct {
	TotalCount int          `json:"total_count"`
	CheckRuns  []ghCheckRun `json:"check_runs"`
}

type ghCheckRun struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`     // queued, in_progress, completed
	Conclusion string    `json:"conclusion"` // success, failure, neutral, cancelled, skipped, timed_out, action_required
	HTMLURL    string    `json:"html_url"`
	StartedAt  time.Time `json:"started_at"`
}

func (c *Client) GetCombinedStatus(ctx context.Context, owner, repo, ref string) (*vcs.CombinedStatus, error) {
	// 1. Get combined commit statuses (Status API).
	statusURL := fmt.Sprintf("%s/repos/%s/%s/commits/%s/status", c.baseURL, owner, repo, ref)
	var ghStatus ghCombinedStatus
	if err := c.doJSON(ctx, http.MethodGet, statusURL, nil, &ghStatus); err != nil {
		return nil, fmt.Errorf("github combined status: %w", err)
	}

	result := &vcs.CombinedStatus{}
	for _, s := range ghStatus.Statuses {
		cs := vcs.CommitStatus{
			Context:     s.Context,
			State:       mapGitHubState(s.State),
			Description: s.Description,
			TargetURL:   s.TargetURL,
			CreatedAt:   s.CreatedAt,
		}
		result.Statuses = append(result.Statuses, cs)
	}

	// 2. Get check runs (Checks API — GitHub Apps, GitHub Actions).
	checksURL := fmt.Sprintf("%s/repos/%s/%s/commits/%s/check-runs", c.baseURL, owner, repo, ref)
	var ghChecks ghCheckRunsResp
	if err := c.doJSON(ctx, http.MethodGet, checksURL, nil, &ghChecks); err != nil {
		// Check runs API may 404 if not enabled — not fatal.
		slog.Debug("github check-runs unavailable", "ref", ref, "error", err)
	} else {
		for _, cr := range ghChecks.CheckRuns {
			state := mapCheckRunToState(cr.Status, cr.Conclusion)
			cs := vcs.CommitStatus{
				Context:     cr.Name,
				State:       state,
				Description: cr.Conclusion,
				TargetURL:   cr.HTMLURL,
				CreatedAt:   cr.StartedAt,
			}
			result.Statuses = append(result.Statuses, cs)
		}
	}

	// 3. Aggregate.
	for _, s := range result.Statuses {
		result.Total++
		switch s.State {
		case vcs.CommitStatusSuccess:
			result.Passed++
		case vcs.CommitStatusFailure, vcs.CommitStatusError:
			result.Failed++
		default:
			result.Pending++
		}
	}

	if result.Total == 0 {
		result.State = vcs.CommitStatusSuccess // no checks = pass
	} else if result.Failed > 0 {
		result.State = vcs.CommitStatusFailure
	} else if result.Pending > 0 {
		result.State = vcs.CommitStatusPending
	} else {
		result.State = vcs.CommitStatusSuccess
	}

	return result, nil
}

func mapGitHubState(s string) vcs.CommitStatusState {
	switch s {
	case "success":
		return vcs.CommitStatusSuccess
	case "failure":
		return vcs.CommitStatusFailure
	case "error":
		return vcs.CommitStatusError
	default:
		return vcs.CommitStatusPending
	}
}

func mapCheckRunToState(status, conclusion string) vcs.CommitStatusState {
	if status != "completed" {
		return vcs.CommitStatusPending
	}
	switch conclusion {
	case "success", "neutral", "skipped":
		return vcs.CommitStatusSuccess
	case "failure", "timed_out", "action_required":
		return vcs.CommitStatusFailure
	case "cancelled":
		return vcs.CommitStatusError
	default:
		return vcs.CommitStatusPending
	}
}

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
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("github api: %w", err)
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
		return fmt.Errorf("github api %d: %s", resp.StatusCode, string(respBody))
	}

	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
