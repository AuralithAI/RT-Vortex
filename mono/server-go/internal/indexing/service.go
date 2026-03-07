// Package indexing orchestrates repository indexing via the C++ engine.
//
// It manages async indexing jobs with progress tracking.
package indexing

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/store"
)

// ── Job Status ──────────────────────────────────────────────────────────────

// JobState represents the current state of an indexing job.
type JobState string

const (
	JobStatePending   JobState = "pending"
	JobStateRunning   JobState = "running"
	JobStateCompleted JobState = "completed"
	JobStateFailed    JobState = "failed"
	JobStateCancelled JobState = "cancelled"
)

// JobStatus tracks the progress of an indexing job.
type JobStatus struct {
	JobID       string             `json:"job_id"`
	RepoID      string             `json:"repo_id"`
	State       JobState           `json:"state"`
	Progress    int                `json:"progress"` // 0-100
	Message     string             `json:"message"`
	StartedAt   time.Time          `json:"started_at"`
	CompletedAt *time.Time         `json:"completed_at,omitempty"`
	Error       string             `json:"error,omitempty"`
	Stats       *engine.IndexStats `json:"stats,omitempty"`
}

// ── Service ─────────────────────────────────────────────────────────────────

// Service manages indexing jobs.
type Service struct {
	engineClient *engine.Client
	repoStore    *store.RepositoryRepo
	jobs         map[string]*JobStatus
	mu           sync.RWMutex
}

// NewService creates an indexing service.
func NewService(engineClient *engine.Client, repoStore *store.RepositoryRepo) *Service {
	return &Service{
		engineClient: engineClient,
		repoStore:    repoStore,
		jobs:         make(map[string]*JobStatus),
	}
}

// ── Full Index ──────────────────────────────────────────────────────────────

// FullIndexRequest holds the parameters for a full repository index.
type FullIndexRequest struct {
	RepoID   string
	RepoPath string
	Config   engine.IndexConfig
}

// StartFullIndex launches an async full indexing job and returns immediately.
func (s *Service) StartFullIndex(ctx context.Context, req FullIndexRequest) (string, error) {
	jobID := uuid.New().String()
	job := &JobStatus{
		JobID:     jobID,
		RepoID:    req.RepoID,
		State:     JobStatePending,
		Progress:  0,
		Message:   "Job queued",
		StartedAt: time.Now().UTC(),
	}

	s.mu.Lock()
	s.jobs[jobID] = job
	s.mu.Unlock()

	slog.Info("indexing job created", "job_id", jobID, "repo_id", req.RepoID)

	// Run indexing pipeline asynchronously.
	go s.runFullIndex(jobID, req)

	return jobID, nil
}

func (s *Service) runFullIndex(jobID string, req FullIndexRequest) {
	s.updateJob(jobID, JobStateRunning, 5, "Initializing indexing pipeline")

	// Step 1: Push storage config if needed (5%)
	s.updateJob(jobID, JobStateRunning, 10, "Preparing repository for indexing")

	// Step 2: Call engine to index repository (10% → 85%)
	s.updateJob(jobID, JobStateRunning, 15, "Scanning repository files")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := s.engineClient.IndexRepository(ctx, req.RepoID, req.RepoPath, req.Config)
	if err != nil {
		s.failJob(jobID, fmt.Sprintf("indexing failed: %v", err))
		return
	}

	if !result.Success {
		s.failJob(jobID, fmt.Sprintf("engine reported failure: %s", result.Message))
		return
	}

	s.updateJob(jobID, JobStateRunning, 90, "Finalizing index")

	// Step 3: Update indexed_at in the database
	s.updateJob(jobID, JobStateRunning, 95, "Recording index statistics")
	if s.repoStore != nil {
		repoUUID, parseErr := uuid.Parse(req.RepoID)
		if parseErr == nil {
			if markErr := s.repoStore.MarkIndexed(context.Background(), repoUUID); markErr != nil {
				slog.Error("failed to mark repo as indexed", "repo_id", req.RepoID, "error", markErr)
			}
		}
	}

	// Step 4: Mark completed
	now := time.Now().UTC()
	s.mu.Lock()
	if job, ok := s.jobs[jobID]; ok {
		job.State = JobStateCompleted
		job.Progress = 100
		job.Message = "Indexing completed successfully"
		job.CompletedAt = &now
		job.Stats = result.Stats
	}
	s.mu.Unlock()

	slog.Info("indexing job completed",
		"job_id", jobID,
		"repo_id", req.RepoID,
		"duration", time.Since(s.getJob(jobID).StartedAt),
	)
}

// ── Incremental Index ───────────────────────────────────────────────────────

// IncrementalIndexRequest holds parameters for an incremental index.
type IncrementalIndexRequest struct {
	RepoID       string
	ChangedFiles []string
	BaseCommit   string
	HeadCommit   string
}

// StartIncrementalIndex launches an async incremental indexing job.
func (s *Service) StartIncrementalIndex(ctx context.Context, req IncrementalIndexRequest) (string, error) {
	jobID := uuid.New().String()
	job := &JobStatus{
		JobID:     jobID,
		RepoID:    req.RepoID,
		State:     JobStatePending,
		Progress:  0,
		Message:   "Incremental index queued",
		StartedAt: time.Now().UTC(),
	}

	s.mu.Lock()
	s.jobs[jobID] = job
	s.mu.Unlock()

	slog.Info("incremental index job created",
		"job_id", jobID,
		"repo_id", req.RepoID,
		"changed_files", len(req.ChangedFiles),
	)

	go s.runIncrementalIndex(jobID, req)
	return jobID, nil
}

func (s *Service) runIncrementalIndex(jobID string, req IncrementalIndexRequest) {
	s.updateJob(jobID, JobStateRunning, 10, "Starting incremental index")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	result, err := s.engineClient.IncrementalIndex(ctx, req.RepoID, req.ChangedFiles, req.BaseCommit, req.HeadCommit)
	if err != nil {
		s.failJob(jobID, fmt.Sprintf("incremental index failed: %v", err))
		return
	}

	if !result.Success {
		s.failJob(jobID, fmt.Sprintf("engine reported failure: %s", result.Message))
		return
	}

	now := time.Now().UTC()
	s.mu.Lock()
	if job, ok := s.jobs[jobID]; ok {
		job.State = JobStateCompleted
		job.Progress = 100
		job.Message = "Incremental index completed"
		job.CompletedAt = &now
		job.Stats = result.Stats
	}
	s.mu.Unlock()

	slog.Info("incremental index completed", "job_id", jobID, "repo_id", req.RepoID)
}

// ── Query / Cancel ──────────────────────────────────────────────────────────

// GetJobStatus returns the status of an indexing job.
func (s *Service) GetJobStatus(jobID string) (*JobStatus, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return nil, false
	}
	// Return a copy
	cp := *job
	return &cp, true
}

// GetIndexInfo returns the current index stats for a repository from the engine.
func (s *Service) GetIndexInfo(ctx context.Context, repoID string) (*engine.IndexStats, error) {
	return s.engineClient.GetIndexStats(ctx, repoID)
}

// DeleteIndex removes a repository's index from the engine.
func (s *Service) DeleteIndex(ctx context.Context, repoID string) error {
	return s.engineClient.DeleteIndex(ctx, repoID)
}

// ── Cleanup ─────────────────────────────────────────────────────────────────

// CleanupOldJobs removes completed/failed jobs older than maxAge.
func (s *Service) CleanupOldJobs(maxAge time.Duration) int {
	cutoff := time.Now().UTC().Add(-maxAge)
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for id, job := range s.jobs {
		if job.State == JobStateCompleted || job.State == JobStateFailed || job.State == JobStateCancelled {
			if job.CompletedAt != nil && job.CompletedAt.Before(cutoff) {
				delete(s.jobs, id)
				removed++
			}
		}
	}
	return removed
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func (s *Service) updateJob(jobID string, state JobState, progress int, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[jobID]; ok {
		job.State = state
		job.Progress = progress
		job.Message = msg
	}
}

func (s *Service) failJob(jobID, errMsg string) {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[jobID]; ok {
		job.State = JobStateFailed
		job.Message = "Indexing failed"
		job.Error = errMsg
		job.CompletedAt = &now
	}
	slog.Error("indexing job failed", "job_id", jobID, "error", errMsg)
}

func (s *Service) getJob(jobID string) *JobStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.jobs[jobID]
}
