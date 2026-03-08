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
