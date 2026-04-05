package bitbucket

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
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

const bitbucketAPIBase = "https://api.bitbucket.org/2.0"

func init() {
	vcs.RegisterFactory(vcs.PlatformBitbucket, func(creds *vcs.ResolvedCreds) vcs.Platform {
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

// Config holds Bitbucket-specific configuration.
type Config struct {
	Token         string        // OAuth2 access token
	WebhookSecret string        // HMAC-SHA256 secret for webhook validation
	BaseURL       string        // override for Bitbucket Data Center / self-hosted
	Timeout       time.Duration // HTTP client timeout
}

// Client implements vcs.Platform for Bitbucket Cloud.
type Client struct {
	token         string
	webhookSecret string
	baseURL       string
	client        *http.Client
}

// New creates a Bitbucket VCS client.
func New(cfg Config) *Client {
	base := cfg.BaseURL
	if base == "" {
		base = bitbucketAPIBase
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

func (c *Client) Type() vcs.PlatformType { return vcs.PlatformBitbucket }

// ── Pull Request ────────────────────────────────────────────────────────────

type bbPullRequest struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	State       string    `json:"state"` // OPEN, MERGED, DECLINED, SUPERSEDED
	CreatedOn   time.Time `json:"created_on"`
	UpdatedOn   time.Time `json:"updated_on"`
	Links       struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
	Author struct {
		DisplayName string `json:"display_name"`
		UUID        string `json:"uuid"`
		Nickname    string `json:"nickname"`
	} `json:"author"`
	Source struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
		Commit struct {
			Hash string `json:"hash"`
		} `json:"commit"`
	} `json:"source"`
	Destination struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
		Commit struct {
			Hash string `json:"hash"`
		} `json:"commit"`
	} `json:"destination"`
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (*vcs.PullRequest, error) {
	url := fmt.Sprintf("%s/repositories/%s/%s/pullrequests/%d", c.baseURL, owner, repo, number)
	var bbPR bbPullRequest
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &bbPR); err != nil {
		return nil, err
	}

	state := strings.ToLower(bbPR.State)
	if state == "declined" || state == "superseded" {
		state = "closed"
	}

	author := bbPR.Author.Nickname
	if author == "" {
		author = bbPR.Author.DisplayName
	}

	return &vcs.PullRequest{
		ID:           fmt.Sprintf("%d", bbPR.ID),
		Number:       bbPR.ID,
		Title:        bbPR.Title,
		Description:  bbPR.Description,
		Author:       author,
		SourceBranch: bbPR.Source.Branch.Name,
		TargetBranch: bbPR.Destination.Branch.Name,
		State:        state,
		URL:          bbPR.Links.HTML.Href,
		HeadSHA:      bbPR.Source.Commit.Hash,
		BaseSHA:      bbPR.Destination.Commit.Hash,
		CreatedAt:    bbPR.CreatedOn,
		UpdatedAt:    bbPR.UpdatedOn,
	}, nil
}

// ── List Open PRs ───────────────────────────────────────────────────────────

// bbPullRequestListResponse wraps the paginated Bitbucket PR list response.
type bbPullRequestListResponse struct {
	Values []bbPullRequest `json:"values"`
	Next   string          `json:"next"` // pagination URL
	Size   int             `json:"size"` // total count
}

// ListOpenPullRequests returns all open PRs for a Bitbucket repository.
func (c *Client) ListOpenPullRequests(ctx context.Context, owner, repo string, maxResults int) ([]vcs.PullRequest, error) {
	if maxResults <= 0 {
		maxResults = 100
	}

	var allPRs []vcs.PullRequest
	url := fmt.Sprintf("%s/repositories/%s/%s/pullrequests?state=OPEN&pagelen=50&sort=-updated_on",
		c.baseURL, owner, repo)

	for url != "" && len(allPRs) < maxResults {
		var page bbPullRequestListResponse
		if err := c.doJSON(ctx, http.MethodGet, url, nil, &page); err != nil {
			return nil, fmt.Errorf("list open PRs: %w", err)
		}

		for _, bbPR := range page.Values {
			if len(allPRs) >= maxResults {
				break
			}
			state := strings.ToLower(bbPR.State)
			if state == "declined" || state == "superseded" {
				state = "closed"
			}
			author := bbPR.Author.Nickname
			if author == "" {
				author = bbPR.Author.DisplayName
			}
			allPRs = append(allPRs, vcs.PullRequest{
				ID:           fmt.Sprintf("%d", bbPR.ID),
				Number:       bbPR.ID,
				Title:        bbPR.Title,
				Description:  bbPR.Description,
				Author:       author,
				SourceBranch: bbPR.Source.Branch.Name,
				TargetBranch: bbPR.Destination.Branch.Name,
				State:        state,
				URL:          bbPR.Links.HTML.Href,
				HeadSHA:      bbPR.Source.Commit.Hash,
				BaseSHA:      bbPR.Destination.Commit.Hash,
				CreatedAt:    bbPR.CreatedOn,
				UpdatedAt:    bbPR.UpdatedOn,
			})
		}

		url = page.Next
	}

	return allPRs, nil
}

// ── PR Diff ─────────────────────────────────────────────────────────────────

// Bitbucket's diffstat endpoint returns per-file change summaries.
type bbDiffStatResponse struct {
	Values []bbDiffStat `json:"values"`
	Next   string       `json:"next"` // pagination
}

type bbDiffStat struct {
	Status string `json:"status"` // added, removed, modified, renamed
	Old    *struct {
		Path string `json:"path"`
	} `json:"old"`
	New *struct {
		Path string `json:"path"`
	} `json:"new"`
	LinesAdded   int `json:"lines_added"`
	LinesRemoved int `json:"lines_removed"`
}

func (c *Client) GetPullRequestDiff(ctx context.Context, owner, repo string, number int) ([]vcs.DiffFile, error) {
	var allDiffs []bbDiffStat
	url := fmt.Sprintf("%s/repositories/%s/%s/pullrequests/%d/diffstat", c.baseURL, owner, repo, number)

	// Paginate through all pages.
	for url != "" {
		var page bbDiffStatResponse
		if err := c.doJSON(ctx, http.MethodGet, url, nil, &page); err != nil {
			return nil, err
		}
		allDiffs = append(allDiffs, page.Values...)
		url = page.Next
	}

	// Also fetch the raw unified diff for patch content.
	patches := c.fetchRawDiffPatches(ctx, owner, repo, number)

	files := make([]vcs.DiffFile, len(allDiffs))
	for i, d := range allDiffs {
		filename := ""
		previousName := ""
		if d.New != nil {
			filename = d.New.Path
		}
		if d.Old != nil {
			previousName = d.Old.Path
		}
		if filename == "" && previousName != "" {
			filename = previousName
		}

		status := strings.ToLower(d.Status)
		if status == "removed" {
			status = "deleted"
		}

		files[i] = vcs.DiffFile{
			Filename:     filename,
			Status:       status,
			Additions:    d.LinesAdded,
			Deletions:    d.LinesRemoved,
			Patch:        patches[filename],
			PreviousName: previousName,
		}
	}
	return files, nil
}

// fetchRawDiffPatches fetches the unified diff and splits it into per-file patches.
func (c *Client) fetchRawDiffPatches(ctx context.Context, owner, repo string, number int) map[string]string {
	url := fmt.Sprintf("%s/repositories/%s/%s/pullrequests/%d/diff", c.baseURL, owner, repo, number)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20)) // 50 MB limit
	if err != nil {
		return nil
	}

	return splitUnifiedDiff(string(body))
}

// splitUnifiedDiff parses a unified diff into per-file patches keyed by filename.
func splitUnifiedDiff(diff string) map[string]string {
	patches := make(map[string]string)
	sections := strings.Split(diff, "diff --git ")
	for _, section := range sections {
		if section == "" {
			continue
		}
		// Extract filename from "+++ b/path/to/file"
		lines := strings.SplitN(section, "\n", -1)
		filename := ""
		for _, line := range lines {
			if strings.HasPrefix(line, "+++ b/") {
				filename = strings.TrimPrefix(line, "+++ b/")
				break
			}
		}
		if filename != "" {
			patches[filename] = section
		}
	}
	return patches
}

// ── Review Comments ─────────────────────────────────────────────────────────

func (c *Client) PostReviewComment(ctx context.Context, owner, repo string, number int, comment *vcs.ReviewCommentRequest) error {
	url := fmt.Sprintf("%s/repositories/%s/%s/pullrequests/%d/comments", c.baseURL, owner, repo, number)

	body := map[string]interface{}{
		"content": map[string]string{
			"raw": comment.Body,
		},
		"inline": map[string]interface{}{
			"path": comment.Path,
			"to":   comment.Line,
		},
	}

	return c.doJSON(ctx, http.MethodPost, url, body, nil)
}

func (c *Client) PostReviewSummary(ctx context.Context, owner, repo string, number int, body string) error {
	url := fmt.Sprintf("%s/repositories/%s/%s/pullrequests/%d/comments", c.baseURL, owner, repo, number)

	commentBody := map[string]interface{}{
		"content": map[string]string{
			"raw": body,
		},
	}

	return c.doJSON(ctx, http.MethodPost, url, commentBody, nil)
}

// ── File Content ────────────────────────────────────────────────────────────

func (c *Client) GetFileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	url := fmt.Sprintf("%s/repositories/%s/%s/src/%s/%s", c.baseURL, owner, repo, ref, path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, vcs.ErrRepoNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bitbucket returned %d", resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB limit
}

// ── Webhook Validation ──────────────────────────────────────────────────────

// ValidateWebhookSignature checks the X-Hub-Signature header (HMAC-SHA256).
// Bitbucket Cloud uses the same HMAC scheme as GitHub.
func (c *Client) ValidateWebhookSignature(payload []byte, signature string) bool {
	if c.webhookSecret == "" {
		slog.Warn("bitbucket: webhook secret not configured, skipping validation")
		return true
	}
	// Signature format: "sha256=<hex>"
	sig := strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(c.webhookSecret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
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
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("bitbucket api: %w", err)
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
		return fmt.Errorf("bitbucket api %d: %s", resp.StatusCode, string(respBody))
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
	url := fmt.Sprintf("%s/repositories/%s/%s/refs/branches", c.baseURL, owner, repo)

	body := map[string]interface{}{
		"name": req.BranchName,
		"target": map[string]interface{}{
			"hash": req.FromSHA,
		},
	}

	if err := c.doJSON(ctx, http.MethodPost, url, body, nil); err != nil {
		return fmt.Errorf("create branch %s: %w", req.BranchName, err)
	}
	slog.Info("bitbucket: created branch", "owner", owner, "repo", repo, "branch", req.BranchName)
	return nil
}

// CreateOrUpdateFile creates or updates a file on a branch and commits it.
// Bitbucket uses a multipart form POST to the /src endpoint.
func (c *Client) CreateOrUpdateFile(ctx context.Context, owner, repo, branch string, file *vcs.FileCommit) (string, error) {
	url := fmt.Sprintf("%s/repositories/%s/%s/src", c.baseURL, owner, repo)

	// Bitbucket's /src endpoint accepts form data for committing files.
	body := map[string]interface{}{
		file.Path: file.Content,
		"message": file.Message,
		"branch":  branch,
	}

	if err := c.doJSON(ctx, http.MethodPost, url, body, nil); err != nil {
		return "", fmt.Errorf("create/update file %s: %w", file.Path, err)
	}

	// Fetch the latest commit SHA on the branch.
	sha, err := c.GetBranchSHA(ctx, owner, repo, branch)
	if err != nil {
		slog.Warn("bitbucket: committed file but failed to get SHA", "path", file.Path, "err", err)
		return "", nil
	}

	slog.Info("bitbucket: committed file", "owner", owner, "repo", repo, "path", file.Path, "sha", sha)
	return sha, nil
}

// CreatePullRequest opens a new pull request on Bitbucket.
func (c *Client) CreatePullRequest(ctx context.Context, owner, repo string, req *vcs.CreatePullRequestRequest) (*vcs.PullRequest, error) {
	url := fmt.Sprintf("%s/repositories/%s/%s/pullrequests", c.baseURL, owner, repo)

	body := map[string]interface{}{
		"title":       req.Title,
		"description": req.Body,
		"source": map[string]interface{}{
			"branch": map[string]string{
				"name": req.SourceBranch,
			},
		},
		"destination": map[string]interface{}{
			"branch": map[string]string{
				"name": req.TargetBranch,
			},
		},
		"close_source_branch": true,
	}

	var bbPR bbPullRequest
	if err := c.doJSON(ctx, http.MethodPost, url, body, &bbPR); err != nil {
		return nil, fmt.Errorf("create PR: %w", err)
	}

	state := strings.ToLower(bbPR.State)
	if state == "declined" || state == "superseded" {
		state = "closed"
	}
	author := bbPR.Author.Nickname
	if author == "" {
		author = bbPR.Author.DisplayName
	}

	pr := &vcs.PullRequest{
		ID:           fmt.Sprintf("%d", bbPR.ID),
		Number:       bbPR.ID,
		Title:        bbPR.Title,
		Description:  bbPR.Description,
		Author:       author,
		SourceBranch: bbPR.Source.Branch.Name,
		TargetBranch: bbPR.Destination.Branch.Name,
		State:        state,
		URL:          bbPR.Links.HTML.Href,
		HeadSHA:      bbPR.Source.Commit.Hash,
		BaseSHA:      bbPR.Destination.Commit.Hash,
		CreatedAt:    bbPR.CreatedOn,
		UpdatedAt:    bbPR.UpdatedOn,
	}

	slog.Info("bitbucket: created pull request", "owner", owner, "repo", repo, "id", bbPR.ID, "url", bbPR.Links.HTML.Href)
	return pr, nil
}

// GetDefaultBranch returns the repo's default branch name.
func (c *Client) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	url := fmt.Sprintf("%s/repositories/%s/%s", c.baseURL, owner, repo)

	var repoInfo struct {
		MainBranch struct {
			Name string `json:"name"`
		} `json:"mainbranch"`
	}
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &repoInfo); err != nil {
		return "", fmt.Errorf("get default branch: %w", err)
	}
	if repoInfo.MainBranch.Name == "" {
		return "main", nil
	}
	return repoInfo.MainBranch.Name, nil
}

// GetBranchSHA returns the HEAD commit SHA for a branch.
func (c *Client) GetBranchSHA(ctx context.Context, owner, repo, branch string) (string, error) {
	url := fmt.Sprintf("%s/repositories/%s/%s/refs/branches/%s", c.baseURL, owner, repo, branch)

	var branchInfo struct {
		Target struct {
			Hash string `json:"hash"`
		} `json:"target"`
	}
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &branchInfo); err != nil {
		return "", fmt.Errorf("get branch SHA: %w", err)
	}
	return branchInfo.Target.Hash, nil
}

// GetCombinedStatus returns the aggregated build status for a commit.
func (c *Client) GetCombinedStatus(ctx context.Context, owner, repo, ref string) (*vcs.CombinedStatus, error) {
	url := fmt.Sprintf("%s/repositories/%s/%s/commit/%s/statuses", c.baseURL, owner, repo, ref)

	var resp struct {
		Values []struct {
			Name        string    `json:"name"`
			State       string    `json:"state"` // SUCCESSFUL, FAILED, INPROGRESS, STOPPED
			Description string    `json:"description"`
			URL         string    `json:"url"`
			CreatedOn   time.Time `json:"created_on"`
		} `json:"values"`
	}
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &resp); err != nil {
		slog.Debug("bitbucket commit statuses unavailable", "ref", ref, "error", err)
		return nil, nil
	}

	result := &vcs.CombinedStatus{}
	for _, s := range resp.Values {
		state := mapBitbucketState(s.State)
		result.Statuses = append(result.Statuses, vcs.CommitStatus{
			Context:     s.Name,
			State:       state,
			Description: s.Description,
			TargetURL:   s.URL,
			CreatedAt:   s.CreatedOn,
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

func mapBitbucketState(s string) vcs.CommitStatusState {
	switch s {
	case "SUCCESSFUL":
		return vcs.CommitStatusSuccess
	case "FAILED":
		return vcs.CommitStatusFailure
	case "STOPPED":
		return vcs.CommitStatusError
	default:
		return vcs.CommitStatusPending
	}
}
