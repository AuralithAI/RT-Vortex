package model_test

import (
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/model"
	"github.com/google/uuid"
)

// ── ReviewStatus ────────────────────────────────────────────────────────────

func TestReviewStatusConstants(t *testing.T) {
	tests := []struct {
		name     string
		value    model.ReviewStatus
		expected string
	}{
		{"Pending", model.ReviewStatusPending, "pending"},
		{"InProgress", model.ReviewStatusInProgress, "in_progress"},
		{"Completed", model.ReviewStatusCompleted, "completed"},
		{"Failed", model.ReviewStatusFailed, "failed"},
		{"Cancelled", model.ReviewStatusCancelled, "cancelled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.value)
			}
		})
	}
}

// ── Severity ────────────────────────────────────────────────────────────────

func TestSeverityConstants(t *testing.T) {
	tests := []struct {
		name     string
		value    model.Severity
		expected string
	}{
		{"Critical", model.SeverityCritical, "critical"},
		{"High", model.SeverityHigh, "high"},
		{"Medium", model.SeverityMedium, "medium"},
		{"Low", model.SeverityLow, "low"},
		{"Info", model.SeverityInfo, "info"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.value)
			}
		})
	}
}

// ── Review struct ───────────────────────────────────────────────────────────

func TestReview_DefaultValues(t *testing.T) {
	r := model.Review{
		RepoID:     uuid.New(),
		PRNumber:   42,
		PRTitle:    "Fix bug",
		PRAuthor:   "dev",
		BaseBranch: "main",
		HeadBranch: "fix-42",
		Status:     model.ReviewStatusPending,
		Platform:   "github",
	}

	if r.PRNumber != 42 {
		t.Errorf("expected PR number 42, got %d", r.PRNumber)
	}
	if r.Status != model.ReviewStatusPending {
		t.Errorf("expected pending status, got %s", r.Status)
	}
	if r.ID != uuid.Nil {
		t.Error("expected nil UUID before creation")
	}
}

// ── ReviewComment struct ────────────────────────────────────────────────────

func TestReviewComment_Fields(t *testing.T) {
	c := model.ReviewComment{
		FilePath:   "main.go",
		LineNumber: 42,
		Severity:   model.SeverityHigh,
		Category:   "security",
		Title:      "SQL Injection",
		Body:       "User input not sanitized",
		Suggestion: "Use parameterized queries",
	}

	if c.FilePath != "main.go" {
		t.Errorf("expected main.go, got %s", c.FilePath)
	}
	if c.LineNumber != 42 {
		t.Errorf("expected line 42, got %d", c.LineNumber)
	}
	if c.Severity != model.SeverityHigh {
		t.Errorf("expected high, got %s", c.Severity)
	}
	if c.Posted {
		t.Error("expected Posted to be false by default")
	}
}

// ── WebhookEvent ────────────────────────────────────────────────────────────

func TestWebhookEvent_Fields(t *testing.T) {
	e := model.WebhookEvent{
		Platform:  "github",
		EventType: "pull_request",
		Payload:   []byte(`{"action":"opened"}`),
		Processed: false,
	}

	if e.Platform != "github" {
		t.Error("wrong platform")
	}
	if e.Processed {
		t.Error("expected unprocessed")
	}
}

// ── UsageDaily ──────────────────────────────────────────────────────────────

func TestUsageDaily_Fields(t *testing.T) {
	u := model.UsageDaily{
		OrgID:        uuid.New(),
		ReviewsCount: 10,
		TokensUsed:   5000,
	}

	if u.ReviewsCount != 10 {
		t.Errorf("expected 10, got %d", u.ReviewsCount)
	}
	if u.TokensUsed != 5000 {
		t.Errorf("expected 5000, got %d", u.TokensUsed)
	}
}
