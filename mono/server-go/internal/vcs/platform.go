package vcs

import (
	"context"
	"errors"
	"time"
)

// ── Errors ──────────────────────────────────────────────────────────────────

var (
	ErrPlatformNotFound = errors.New("VCS platform not found")
	ErrUnauthorized     = errors.New("VCS authorization failed")
	ErrRepoNotFound     = errors.New("repository not found")
	ErrPRNotFound       = errors.New("pull request not found")
)

// ── Types ───────────────────────────────────────────────────────────────────

// PlatformType identifies a VCS platform.
type PlatformType string

const (
	PlatformGitHub      PlatformType = "github"
	PlatformGitLab      PlatformType = "gitlab"
	PlatformBitbucket   PlatformType = "bitbucket"
	PlatformAzureDevOps PlatformType = "azure_devops"
)

// PullRequest represents a normalized pull request across platforms.
type PullRequest struct {
	ID           string    `json:"id"`
	Number       int       `json:"number"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Author       string    `json:"author"`
	SourceBranch string    `json:"source_branch"`
	TargetBranch string    `json:"target_branch"`
	State        string    `json:"state"` // open, closed, merged
	URL          string    `json:"url"`
	HeadSHA      string    `json:"head_sha"`
	BaseSHA      string    `json:"base_sha"`
	Draft        bool      `json:"draft"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

// DiffFile represents a single changed file in a PR.
type DiffFile struct {
	Filename     string `json:"filename"`
	Status       string `json:"status"` // added, modified, deleted, renamed
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	Patch        string `json:"patch"`
	PreviousName string `json:"previous_name,omitempty"`
}

// ReviewCommentRequest is sent to the VCS platform to post a review comment.
type ReviewCommentRequest struct {
	Body     string `json:"body"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Side     string `json:"side"` // LEFT or RIGHT
	CommitID string `json:"commit_id"`
}

// WebhookPayload is the raw data received from a VCS webhook.
type WebhookPayload struct {
	Platform  PlatformType `json:"platform"`
	Event     string       `json:"event"`  // e.g. pull_request, merge_request
	Action    string       `json:"action"` // e.g. opened, synchronize
	RepoOwner string       `json:"repo_owner"`
	RepoName  string       `json:"repo_name"`
	PRNumber  int          `json:"pr_number"`
	RawBody   []byte       `json:"-"`
}

// ── Platform Interface ──────────────────────────────────────────────────────

// Platform defines the contract for interacting with a VCS provider.
type Platform interface {
	// Type returns the platform identifier.
	Type() PlatformType

	// GetPullRequest fetches a PR by number.
	GetPullRequest(ctx context.Context, owner, repo string, number int) (*PullRequest, error)

	// ListOpenPullRequests returns all open/active PRs for a repository.
	// The implementation handles pagination internally and returns up to maxResults PRs.
	ListOpenPullRequests(ctx context.Context, owner, repo string, maxResults int) ([]PullRequest, error)

	// GetPullRequestDiff returns the file-level diff for a PR.
	GetPullRequestDiff(ctx context.Context, owner, repo string, number int) ([]DiffFile, error)

	// PostReviewComment posts an inline review comment on a PR.
	PostReviewComment(ctx context.Context, owner, repo string, number int, comment *ReviewCommentRequest) error

	// PostReviewSummary posts a top-level review summary on a PR.
	PostReviewSummary(ctx context.Context, owner, repo string, number int, body string) error

	// GetFileContent returns the content of a file at a specific commit SHA.
	GetFileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, error)

	// ValidateWebhookSignature verifies the webhook payload is authentic.
	ValidateWebhookSignature(payload []byte, signature string) bool
}

// ── Platform Registry ───────────────────────────────────────────────────────

// PlatformRegistry manages VCS platform integrations.
type PlatformRegistry struct {
	platforms map[PlatformType]Platform
}

// NewPlatformRegistry creates an empty registry.
func NewPlatformRegistry() *PlatformRegistry {
	return &PlatformRegistry{
		platforms: make(map[PlatformType]Platform),
	}
}

// Register adds a platform to the registry.
func (r *PlatformRegistry) Register(p Platform) {
	r.platforms[p.Type()] = p
}

// Get returns a platform by type.
func (r *PlatformRegistry) Get(t PlatformType) (Platform, bool) {
	p, ok := r.platforms[t]
	return p, ok
}

// List returns all registered platform types.
func (r *PlatformRegistry) List() []PlatformType {
	types := make([]PlatformType, 0, len(r.platforms))
	for t := range r.platforms {
		types = append(types, t)
	}
	return types
}
