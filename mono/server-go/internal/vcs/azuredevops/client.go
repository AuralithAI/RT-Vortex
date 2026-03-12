package azuredevops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/vcs"
)

const azureDevOpsAPIBase = "https://dev.azure.com"

// apiVersion is appended to all Azure DevOps REST API calls.
const apiVersion = "7.1"

func init() {
	vcs.RegisterFactory(vcs.PlatformAzureDevOps, func(creds *vcs.ResolvedCreds) vcs.Platform {
		return New(Config{
			PAT:           creds.Token,
			Organization:  creds.Organization,
			WebhookSecret: creds.WebhookSecret,
			BaseURL:       creds.BaseURL,
		})
	})
}

// Config holds Azure DevOps specific configuration.
type Config struct {
	// PAT (Personal Access Token) — used with Basic auth (username is empty).
	PAT string

	// Organization is the Azure DevOps org name (e.g. "contoso").
	Organization string

	// WebhookSecret is a shared secret for validating service hook payloads.
	// Azure DevOps supports Basic auth on the webhook URL or a resource-version
	// header check; this field is used for a simple header-token comparison.
	WebhookSecret string

	// BaseURL overrides the API base for Azure DevOps Server (on-prem).
	BaseURL string

	Timeout time.Duration
}

// Client implements vcs.Platform for Azure DevOps.
type Client struct {
	pat           string
	org           string
	webhookSecret string
	baseURL       string
	client        *http.Client
}

// New creates an Azure DevOps VCS client.
func New(cfg Config) *Client {
	base := cfg.BaseURL
	if base == "" {
		base = azureDevOpsAPIBase
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		pat:           cfg.PAT,
		org:           cfg.Organization,
		webhookSecret: cfg.WebhookSecret,
		baseURL:       strings.TrimSuffix(base, "/"),
		client:        &http.Client{Timeout: timeout},
	}
}

func (c *Client) Type() vcs.PlatformType { return vcs.PlatformAzureDevOps }

// apiURL builds a full API URL.
// Azure DevOps endpoints are:
//
//	https://dev.azure.com/{org}/{project}/_apis/git/repositories/{repo}/...
//
// The "owner" parameter maps to the project name.
// The "repo" parameter maps to the repository name.
func (c *Client) apiURL(project, repo, path string) string {
	return fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/%s?api-version=%s",
		c.baseURL, c.org, project, repo, path, apiVersion)
}

// basicAuth returns the Base64-encoded credentials for PAT authentication.
// Azure DevOps PATs use Basic auth with an empty username: ":<PAT>".
func (c *Client) basicAuth() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+c.pat))
}

// ── Pull Request ────────────────────────────────────────────────────────────

type adoPullRequest struct {
	PullRequestID int       `json:"pullRequestId"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Status        string    `json:"status"` // active, completed, abandoned
	IsDraft       bool      `json:"isDraft"`
	URL           string    `json:"url"`
	CreationDate  time.Time `json:"creationDate"`
	CreatedBy     struct {
		DisplayName string `json:"displayName"`
		UniqueName  string `json:"uniqueName"`
	} `json:"createdBy"`
	SourceRefName         string `json:"sourceRefName"` // refs/heads/feature
	TargetRefName         string `json:"targetRefName"` // refs/heads/main
	LastMergeSourceCommit struct {
		CommitID string `json:"commitId"`
	} `json:"lastMergeSourceCommit"`
	LastMergeTargetCommit struct {
		CommitID string `json:"commitId"`
	} `json:"lastMergeTargetCommit"`
}

func (c *Client) GetPullRequest(ctx context.Context, project, repo string, number int) (*vcs.PullRequest, error) {
	url := c.apiURL(project, repo, fmt.Sprintf("pullrequests/%d", number))

	var adoPR adoPullRequest
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &adoPR); err != nil {
		return nil, err
	}

	state := strings.ToLower(adoPR.Status)
	switch state {
	case "active":
		state = "open"
	case "completed":
		state = "merged"
	case "abandoned":
		state = "closed"
	}

	return &vcs.PullRequest{
		ID:           fmt.Sprintf("%d", adoPR.PullRequestID),
		Number:       adoPR.PullRequestID,
		Title:        adoPR.Title,
		Description:  adoPR.Description,
		Author:       adoPR.CreatedBy.UniqueName,
		SourceBranch: trimRefPrefix(adoPR.SourceRefName),
		TargetBranch: trimRefPrefix(adoPR.TargetRefName),
		State:        state,
		URL:          adoPR.URL,
		HeadSHA:      adoPR.LastMergeSourceCommit.CommitID,
		BaseSHA:      adoPR.LastMergeTargetCommit.CommitID,
		Draft:        adoPR.IsDraft,
		CreatedAt:    adoPR.CreationDate,
	}, nil
}

// ── List Open PRs ───────────────────────────────────────────────────────────

// adoPullRequestList wraps the Azure DevOps PR list response.
type adoPullRequestList struct {
	Value []adoPullRequest `json:"value"`
	Count int              `json:"count"`
}

// ListOpenPullRequests returns all active PRs for an Azure DevOps repository.
// Note: Azure DevOps uses "project" as "owner" and "repo" as the repository name.
func (c *Client) ListOpenPullRequests(ctx context.Context, project, repo string, maxResults int) ([]vcs.PullRequest, error) {
	if maxResults <= 0 {
		maxResults = 100
	}

	top := maxResults
	if top > 1000 {
		top = 1000
	}

	url := c.apiURL(project, repo, fmt.Sprintf("pullrequests?searchCriteria.status=active&$top=%d", top))

	var adoList adoPullRequestList
	if err := c.doJSON(ctx, http.MethodGet, url, nil, &adoList); err != nil {
		return nil, fmt.Errorf("list open PRs: %w", err)
	}

	prs := make([]vcs.PullRequest, 0, len(adoList.Value))
	for _, adoPR := range adoList.Value {
		if len(prs) >= maxResults {
			break
		}
		state := strings.ToLower(adoPR.Status)
		switch state {
		case "active":
			state = "open"
		case "completed":
			state = "merged"
		case "abandoned":
			state = "closed"
		}
		prs = append(prs, vcs.PullRequest{
			ID:           fmt.Sprintf("%d", adoPR.PullRequestID),
			Number:       adoPR.PullRequestID,
			Title:        adoPR.Title,
			Description:  adoPR.Description,
			Author:       adoPR.CreatedBy.UniqueName,
			SourceBranch: trimRefPrefix(adoPR.SourceRefName),
			TargetBranch: trimRefPrefix(adoPR.TargetRefName),
			State:        state,
			URL:          adoPR.URL,
			HeadSHA:      adoPR.LastMergeSourceCommit.CommitID,
			BaseSHA:      adoPR.LastMergeTargetCommit.CommitID,
			Draft:        adoPR.IsDraft,
			CreatedAt:    adoPR.CreationDate,
		})
	}

	return prs, nil
}

// trimRefPrefix strips "refs/heads/" from Azure DevOps branch ref names.
func trimRefPrefix(ref string) string {
	return strings.TrimPrefix(ref, "refs/heads/")
}

// ── PR Diff ─────────────────────────────────────────────────────────────────

// Azure DevOps uses "iterations" to track push updates to a PR.
// We fetch the latest iteration, then get the file changes for it.

type adoIterationList struct {
	Value []struct {
		ID int `json:"id"`
	} `json:"value"`
}

type adoChangeList struct {
	ChangeEntries []adoChangeEntry `json:"changeEntries"`
}

type adoChangeEntry struct {
	ChangeType string `json:"changeType"` // add, edit, delete, rename
	Item       struct {
		Path string `json:"path"`
	} `json:"item"`
	OriginalPath string `json:"originalPath,omitempty"`
}

func (c *Client) GetPullRequestDiff(ctx context.Context, project, repo string, number int) ([]vcs.DiffFile, error) {
	// 1. Get iterations to find the latest one.
	iterURL := c.apiURL(project, repo, fmt.Sprintf("pullrequests/%d/iterations", number))
	var iterations adoIterationList
	if err := c.doJSON(ctx, http.MethodGet, iterURL, nil, &iterations); err != nil {
		return nil, fmt.Errorf("list iterations: %w", err)
	}
	if len(iterations.Value) == 0 {
		return nil, fmt.Errorf("no iterations found for PR %d", number)
	}

	latestIter := iterations.Value[len(iterations.Value)-1].ID

	// 2. Get changes for the latest iteration.
	changesURL := c.apiURL(project, repo,
		fmt.Sprintf("pullrequests/%d/iterations/%d/changes", number, latestIter))
	var changes adoChangeList
	if err := c.doJSON(ctx, http.MethodGet, changesURL, nil, &changes); err != nil {
		return nil, fmt.Errorf("list changes: %w", err)
	}

	files := make([]vcs.DiffFile, 0, len(changes.ChangeEntries))
	for _, entry := range changes.ChangeEntries {
		// Skip directory-level entries (paths ending in /).
		if strings.HasSuffix(entry.Item.Path, "/") {
			continue
		}

		status := mapChangeType(entry.ChangeType)

		files = append(files, vcs.DiffFile{
			Filename:     strings.TrimPrefix(entry.Item.Path, "/"),
			Status:       status,
			PreviousName: strings.TrimPrefix(entry.OriginalPath, "/"),
		})
	}

	return files, nil
}

func mapChangeType(ct string) string {
	switch strings.ToLower(ct) {
	case "add":
		return "added"
	case "edit", "edit, rename":
		return "modified"
	case "delete":
		return "deleted"
	case "rename":
		return "renamed"
	default:
		return "modified"
	}
}

// ── Review Comments ─────────────────────────────────────────────────────────

// Azure DevOps uses "threads" for PR comments. Each thread has a context
// (file path + line) and one or more comments.

type adoThread struct {
	Comments      []adoComment      `json:"comments"`
	ThreadContext *adoThreadContext `json:"threadContext,omitempty"`
	Status        string            `json:"status"` // active, fixed, closed, etc.
}

type adoComment struct {
	Content     string `json:"content"`
	CommentType string `json:"commentType"` // text, system
}

type adoThreadContext struct {
	FilePath       string             `json:"filePath"`
	RightFileStart *adoThreadPosition `json:"rightFileStart,omitempty"`
	RightFileEnd   *adoThreadPosition `json:"rightFileEnd,omitempty"`
}

type adoThreadPosition struct {
	Line   int `json:"line"`
	Offset int `json:"offset"`
}

func (c *Client) PostReviewComment(ctx context.Context, project, repo string, number int, comment *vcs.ReviewCommentRequest) error {
	url := c.apiURL(project, repo, fmt.Sprintf("pullrequests/%d/threads", number))

	thread := adoThread{
		Comments: []adoComment{
			{
				Content:     comment.Body,
				CommentType: "text",
			},
		},
		ThreadContext: &adoThreadContext{
			FilePath: "/" + comment.Path,
			RightFileStart: &adoThreadPosition{
				Line:   comment.Line,
				Offset: 1,
			},
			RightFileEnd: &adoThreadPosition{
				Line:   comment.Line,
				Offset: 1,
			},
		},
		Status: "active",
	}

	return c.doJSON(ctx, http.MethodPost, url, thread, nil)
}

func (c *Client) PostReviewSummary(ctx context.Context, project, repo string, number int, body string) error {
	url := c.apiURL(project, repo, fmt.Sprintf("pullrequests/%d/threads", number))

	// A top-level comment is a thread without a file context.
	thread := adoThread{
		Comments: []adoComment{
			{
				Content:     body,
				CommentType: "text",
			},
		},
		Status: "closed", // Summary comments are informational, not actionable.
	}

	return c.doJSON(ctx, http.MethodPost, url, thread, nil)
}

// ── File Content ────────────────────────────────────────────────────────────

func (c *Client) GetFileContent(ctx context.Context, project, repo, path, ref string) ([]byte, error) {
	// The items endpoint returns raw file content when download=true.
	itemURL := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/items?path=%s&versionDescriptor.version=%s&versionDescriptor.versionType=commit&api-version=%s",
		c.baseURL, c.org, project, repo, path, ref, apiVersion)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, itemURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.basicAuth())
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, vcs.ErrRepoNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("azure devops returned %d", resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB limit
}

// ── Webhook Validation ──────────────────────────────────────────────────────

// ValidateWebhookSignature validates Azure DevOps service hook payloads.
//
// Azure DevOps doesn't natively sign payloads with HMAC. The common pattern is
// to use Basic auth on the webhook URL itself, or to include a shared secret
// as a custom HTTP header. We support the shared-secret-header approach: the
// webhook must send the secret in the "X-Vss-Token" header.
func (c *Client) ValidateWebhookSignature(_ []byte, signature string) bool {
	if c.webhookSecret == "" {
		slog.Warn("azuredevops: webhook secret not configured, skipping validation")
		return true
	}
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
	req.Header.Set("Authorization", c.basicAuth())
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("azure devops api: %w", err)
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
		return fmt.Errorf("azure devops api %d: %s", resp.StatusCode, string(respBody))
	}

	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
