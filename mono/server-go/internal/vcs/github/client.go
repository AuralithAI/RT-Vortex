package github

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
