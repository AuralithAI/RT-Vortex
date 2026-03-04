package model

import (
	"time"

	"github.com/google/uuid"
)

// ─── Review ──────────────────────────────────────────────────────────────────

// ReviewStatus represents the state of a review.
type ReviewStatus string

const (
	ReviewStatusPending    ReviewStatus = "pending"
	ReviewStatusInProgress ReviewStatus = "in_progress"
	ReviewStatusCompleted  ReviewStatus = "completed"
	ReviewStatusFailed     ReviewStatus = "failed"
	ReviewStatusCancelled  ReviewStatus = "cancelled"
)

// Review represents a code review of a pull request.
type Review struct {
	ID           uuid.UUID              `json:"id" db:"id"`
	RepoID       uuid.UUID              `json:"repo_id" db:"repo_id"`
	TriggeredBy  uuid.UUID              `json:"triggered_by" db:"triggered_by"`
	Platform     string                 `json:"platform" db:"platform"`
	PRNumber     int                    `json:"pr_number" db:"pr_number"`
	PRTitle      string                 `json:"pr_title" db:"pr_title"`
	PRAuthor     string                 `json:"pr_author" db:"pr_author"`
	BaseBranch   string                 `json:"base_branch" db:"base_branch"`
	HeadBranch   string                 `json:"head_branch" db:"head_branch"`
	Status       ReviewStatus           `json:"status" db:"status"`
	FilesChanged int                    `json:"files_changed" db:"files_changed"`
	Additions    int                    `json:"additions" db:"additions"`
	Deletions    int                    `json:"deletions" db:"deletions"`
	Metadata     map[string]interface{} `json:"metadata,omitempty" db:"metadata"` // JSONB: timing, LLM model, tokens used
	CreatedAt    time.Time              `json:"created_at" db:"created_at"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty" db:"completed_at"`
}

// ─── Review Comment ──────────────────────────────────────────────────────────

// Severity represents the severity of a review comment.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// ReviewComment represents a single review comment on a file.
type ReviewComment struct {
	ID         uuid.UUID `json:"id" db:"id"`
	ReviewID   uuid.UUID `json:"review_id" db:"review_id"`
	FilePath   string    `json:"file_path" db:"file_path"`
	LineNumber int       `json:"line_number" db:"line_number"`
	EndLine    *int      `json:"end_line,omitempty" db:"end_line"`
	Severity   Severity  `json:"severity" db:"severity"`
	Category   string    `json:"category" db:"category"` // security, performance, style, bug, etc.
	Title      string    `json:"title" db:"title"`
	Body       string    `json:"body" db:"body"`
	Suggestion string    `json:"suggestion,omitempty" db:"suggestion"` // suggested fix
	Posted     bool      `json:"posted" db:"posted"`                   // was it posted to VCS?
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// ─── Usage / Billing ─────────────────────────────────────────────────────────

// UsageDaily tracks daily usage per organization for billing.
type UsageDaily struct {
	OrgID        uuid.UUID `json:"org_id" db:"org_id"`
	Date         time.Time `json:"date" db:"date"`
	ReviewsCount int       `json:"reviews_count" db:"reviews_count"`
	TokensUsed   int64     `json:"tokens_used" db:"tokens_used"`
}

// ─── Webhook ─────────────────────────────────────────────────────────────────

// WebhookEvent records an incoming webhook for audit/debugging.
type WebhookEvent struct {
	ID        uuid.UUID  `json:"id" db:"id"`
	Platform  string     `json:"platform" db:"platform"`
	EventType string     `json:"event_type" db:"event_type"`
	RepoID    *uuid.UUID `json:"repo_id,omitempty" db:"repo_id"`
	Payload   []byte     `json:"-" db:"payload"` // raw JSON, not exposed
	Processed bool       `json:"processed" db:"processed"`
	Error     string     `json:"error,omitempty" db:"error"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}
