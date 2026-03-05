package indexing_test

import (
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/indexing"
)

// ── JobState Constants ──────────────────────────────────────────────────────

func TestJobStateConstants(t *testing.T) {
	tests := []struct {
		name     string
		value    indexing.JobState
		expected string
	}{
		{"Pending", indexing.JobStatePending, "pending"},
		{"Running", indexing.JobStateRunning, "running"},
		{"Completed", indexing.JobStateCompleted, "completed"},
		{"Failed", indexing.JobStateFailed, "failed"},
		{"Cancelled", indexing.JobStateCancelled, "cancelled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.value)
			}
		})
	}
}

// ── Service ─────────────────────────────────────────────────────────────────

func TestNewService_NilClient(t *testing.T) {
	// Creating a service with nil engine client should not panic.
	s := indexing.NewService(nil)
	if s == nil {
		t.Fatal("expected non-nil service")
	}
}

// ── JobStatus struct ────────────────────────────────────────────────────────

func TestJobStatus_Fields(t *testing.T) {
	js := indexing.JobStatus{
		JobID:    "test-job-1",
		RepoID:   "repo-abc",
		State:    indexing.JobStatePending,
		Progress: 0,
		Message:  "Job queued",
	}

	if js.JobID != "test-job-1" {
		t.Errorf("expected test-job-1, got %s", js.JobID)
	}
	if js.State != indexing.JobStatePending {
		t.Errorf("expected pending, got %s", js.State)
	}
	if js.Progress != 0 {
		t.Errorf("expected 0 progress, got %d", js.Progress)
	}
}

func TestFullIndexRequest_Fields(t *testing.T) {
	req := indexing.FullIndexRequest{
		RepoID:   "repo-123",
		RepoPath: "/repos/my-repo",
	}

	if req.RepoID != "repo-123" {
		t.Errorf("expected repo-123, got %s", req.RepoID)
	}
	if req.RepoPath != "/repos/my-repo" {
		t.Errorf("expected /repos/my-repo, got %s", req.RepoPath)
	}
}
