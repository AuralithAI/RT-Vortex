package model

import (
	"time"

	"github.com/google/uuid"
)

// ── Pull Request Sync Status ────────────────────────────────────────────────

// PRSyncStatus represents the synchronisation state of a discovered PR.
type PRSyncStatus string

const (
	PRSyncStatusOpen       PRSyncStatus = "open"
	PRSyncStatusClosed     PRSyncStatus = "closed"
	PRSyncStatusMerged     PRSyncStatus = "merged"
	PRSyncStatusDraft      PRSyncStatus = "draft"
	PRSyncStatusStale      PRSyncStatus = "stale"     // no longer found on the VCS
	PRSyncStatusEmbedded   PRSyncStatus = "embedded"  // diff has been pre-embedded by engine
	PRSyncStatusEmbedding  PRSyncStatus = "embedding" // diff embedding is in progress
	PRSyncStatusEmbedError PRSyncStatus = "embed_error"
)

// PRReviewStatus tracks whether we have reviewed a PR.
type PRReviewStatus string

const (
	PRReviewNone      PRReviewStatus = "none"      // not yet reviewed
	PRReviewPending   PRReviewStatus = "pending"   // review queued / in-progress
	PRReviewCompleted PRReviewStatus = "completed" // review finished
	PRReviewSkipped   PRReviewStatus = "skipped"   // user skipped or filter excluded
)

// ── Pull Request Model ──────────────────────────────────────────────────────

// TrackedPullRequest is a PR discovered from a connected VCS repository and
// persisted locally for background syncing, pre-embedding, and one-click review.
type TrackedPullRequest struct {
	ID           uuid.UUID `json:"id"`
	RepoID       uuid.UUID `json:"repo_id"`
	Platform     string    `json:"platform"`
	PRNumber     int       `json:"pr_number"`
	ExternalID   string    `json:"external_id"` // platform-specific PR id
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Author       string    `json:"author"`
	SourceBranch string    `json:"source_branch"`
	TargetBranch string    `json:"target_branch"`
	HeadSHA      string    `json:"head_sha"`
	BaseSHA      string    `json:"base_sha"`
	PRURL        string    `json:"pr_url"`

	// Sync metadata
	SyncStatus   PRSyncStatus   `json:"sync_status"`   // VCS state
	ReviewStatus PRReviewStatus `json:"review_status"` // our review state
	LastReviewID *uuid.UUID     `json:"last_review_id,omitempty"`

	// File change stats (populated during sync)
	FilesChanged int `json:"files_changed"`
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`

	// Engine embedding metadata
	EmbeddedAt *time.Time `json:"embedded_at,omitempty"`
	EmbedError *string    `json:"embed_error,omitempty"`

	// Timestamps
	SyncedAt  time.Time `json:"synced_at"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PRListFilter provides filtering options for listing tracked PRs.
type PRListFilter struct {
	SyncStatus   *PRSyncStatus
	ReviewStatus *PRReviewStatus
	Author       string
	TargetBranch string
}
