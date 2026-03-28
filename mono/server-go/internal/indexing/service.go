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
	JobStateQueued    JobState = "queued"
	JobStateRunning   JobState = "running"
	JobStateCompleted JobState = "completed"
	JobStateFailed    JobState = "failed"
	JobStateCancelled JobState = "cancelled"
)

// JobStatus tracks the progress of an indexing job.
type JobStatus struct {
	JobID          string             `json:"job_id"`
	RepoID         string             `json:"repo_id"`
	State          JobState           `json:"state"`
	Progress       int                `json:"progress"`        // 0-100
	Phase          string             `json:"phase,omitempty"` // "cloning", "scanning", etc.
	Message        string             `json:"message"`
	FilesProcessed uint64             `json:"files_processed"`
	FilesTotal     uint64             `json:"files_total"`
	CurrentFile    string             `json:"current_file,omitempty"`
	ETASeconds     int64              `json:"eta_seconds"` // -1 = unknown
	StartedAt      time.Time          `json:"started_at"`
	CompletedAt    *time.Time         `json:"completed_at,omitempty"`
	Error          string             `json:"error,omitempty"`
	Stats          *engine.IndexStats `json:"stats,omitempty"`
}

// ── Progress callback ───────────────────────────────────────────────────────

// IndexProgressFunc is called when an indexing job's status changes.
// It receives the job ID and the current status snapshot.
type IndexProgressFunc func(jobID string, status JobStatus)

// ── Service ─────────────────────────────────────────────────────────────────

// Service manages indexing jobs.
type Service struct {
	engineClient *engine.Client
	repoStore    *store.RepositoryRepo
	jobs         map[string]*JobStatus
	mu           sync.RWMutex
	onProgress   IndexProgressFunc

	// Concurrency control — limits how many repos index in parallel
	maxConcurrent int
	sem           chan struct{}
}

// NewService creates an indexing service.
func NewService(engineClient *engine.Client, repoStore *store.RepositoryRepo) *Service {
	maxConcurrent := 3
	return &Service{
		engineClient:  engineClient,
		repoStore:     repoStore,
		jobs:          make(map[string]*JobStatus),
		maxConcurrent: maxConcurrent,
		sem:           make(chan struct{}, maxConcurrent),
	}
}

// SetProgressFunc registers a callback invoked on every progress update.
func (s *Service) SetProgressFunc(fn IndexProgressFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onProgress = fn
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
		JobID:      jobID,
		RepoID:     req.RepoID,
		State:      JobStatePending,
		Progress:   0,
		Phase:      "pending",
		Message:    "Job queued",
		ETASeconds: -1,
		StartedAt:  time.Now().UTC(),
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
	s.updateJobProgress(jobID, JobStatePending, 0, "queued", "Waiting for available slot...", "", 0, 0, -1)

	// Acquire concurrency slot (blocks if all slots are taken)
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	s.updateJobProgress(jobID, JobStateRunning, 5, "cloning", "Initializing indexing pipeline", "", 0, 0, -1)

	// Large repos (100K+ files, millions of chunks) can take many hours to
	// embed.  Use a 24-hour deadline so the streaming RPC isn't killed early.
	// The real guard is job cancellation, not this timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()

	// Use streaming RPC for real-time progress from the C++ engine
	result, err := s.engineClient.IndexRepositoryStream(ctx, req.RepoID, req.RepoPath, req.Config,
		func(update engine.IndexProgressUpdate) {
			s.updateJobProgress(
				jobID,
				JobStateRunning,
				update.Progress,
				update.Phase,
				fmt.Sprintf("Processing: %s", update.Phase),
				update.CurrentFile,
				update.FilesProcessed,
				update.FilesTotal,
				update.ETASeconds,
			)
		},
	)

	if err != nil {
		s.failJob(jobID, fmt.Sprintf("indexing failed: %v", err))
		return
	}

	if !result.Success {
		s.failJob(jobID, fmt.Sprintf("engine reported failure: %s", result.Message))
		return
	}

	// Update indexed_at in the database
	s.updateJobProgress(jobID, JobStateRunning, 95, "finalizing", "Recording index statistics", "", 0, 0, 0)
	if s.repoStore != nil {
		repoUUID, parseErr := uuid.Parse(req.RepoID)
		if parseErr == nil {
			if markErr := s.repoStore.MarkIndexed(context.Background(), repoUUID); markErr != nil {
				slog.Error("failed to mark repo as indexed", "repo_id", req.RepoID, "error", markErr)
			}
		}
	}

	// Mark completed
	now := time.Now().UTC()
	s.mu.Lock()
	if job, ok := s.jobs[jobID]; ok {
		job.State = JobStateCompleted
		job.Progress = 100
		job.Phase = "completed"
		job.Message = "Indexing completed successfully"
		job.CompletedAt = &now
		job.Stats = result.Stats
		job.ETASeconds = 0
		job.CurrentFile = ""
	}
	s.mu.Unlock()

	s.emitProgress(jobID)

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
		JobID:      jobID,
		RepoID:     req.RepoID,
		State:      JobStatePending,
		Progress:   0,
		Phase:      "pending",
		Message:    "Incremental index queued",
		ETASeconds: -1,
		StartedAt:  time.Now().UTC(),
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
	// Acquire concurrency slot
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	s.updateJobProgress(jobID, JobStateRunning, 10, "scanning", "Starting incremental index", "", 0, 0, -1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
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
		job.Phase = "completed"
		job.Message = "Incremental index completed"
		job.CompletedAt = &now
		job.Stats = result.Stats
		job.ETASeconds = 0
	}
	s.mu.Unlock()

	s.emitProgress(jobID)

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

func (s *Service) updateJobProgress(jobID string, state JobState, progress int, phase, msg, currentFile string, filesProcessed, filesTotal uint64, etaSeconds int64) {
	s.mu.Lock()
	if job, ok := s.jobs[jobID]; ok {
		job.State = state
		job.Progress = progress
		job.Phase = phase
		job.Message = msg
		job.CurrentFile = currentFile
		job.FilesProcessed = filesProcessed
		job.FilesTotal = filesTotal
		job.ETASeconds = etaSeconds
	}
	s.mu.Unlock()

	s.emitProgress(jobID)
}

func (s *Service) failJob(jobID, errMsg string) {
	now := time.Now().UTC()
	s.mu.Lock()
	if job, ok := s.jobs[jobID]; ok {
		job.State = JobStateFailed
		job.Phase = "failed"
		job.Message = "Indexing failed"
		job.Error = errMsg
		job.CompletedAt = &now
		job.ETASeconds = 0
	}
	s.mu.Unlock()

	s.emitProgress(jobID)

	slog.Error("indexing job failed", "job_id", jobID, "error", errMsg)
}

// emitProgress calls the registered progress callback with a snapshot.
func (s *Service) emitProgress(jobID string) {
	s.mu.RLock()
	fn := s.onProgress
	job, ok := s.jobs[jobID]
	var snapshot JobStatus
	if ok {
		snapshot = *job
	}
	s.mu.RUnlock()

	if fn != nil && ok {
		fn(jobID, snapshot)
	}
}

func (s *Service) getJob(jobID string) *JobStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.jobs[jobID]
}

// ActiveJobCount returns the number of currently running/pending jobs.
func (s *Service) ActiveJobCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, job := range s.jobs {
		if job.State == JobStatePending || job.State == JobStateQueued || job.State == JobStateRunning {
			count++
		}
	}
	return count
}

// GetActiveJobForRepo returns the active job for a repo, if any.
func (s *Service) GetActiveJobForRepo(repoID string) (*JobStatus, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, job := range s.jobs {
		if job.RepoID == repoID && (job.State == JobStatePending || job.State == JobStateQueued || job.State == JobStateRunning) {
			cp := *job
			return &cp, true
		}
	}
	return nil, false
}
