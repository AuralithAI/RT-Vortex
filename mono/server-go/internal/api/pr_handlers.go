package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/audit"
	"github.com/AuralithAI/rtvortex-server/internal/auth"
	"github.com/AuralithAI/rtvortex-server/internal/model"
	"github.com/AuralithAI/rtvortex-server/internal/review"
	"github.com/AuralithAI/rtvortex-server/internal/store"
)

// ── Pull Request Endpoints ──────────────────────────────────────────────────

// ListPullRequests returns tracked PRs for a repository with optional filtering.
// GET /api/v1/repos/{repoID}/pull-requests
func (h *Handler) ListPullRequests(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	limit, offset := parsePagination(r)

	// Parse optional filters
	filter := model.PRListFilter{}
	if s := r.URL.Query().Get("sync_status"); s != "" {
		status := model.PRSyncStatus(s)
		filter.SyncStatus = &status
	}
	if s := r.URL.Query().Get("review_status"); s != "" {
		status := model.PRReviewStatus(s)
		filter.ReviewStatus = &status
	}
	if s := r.URL.Query().Get("author"); s != "" {
		filter.Author = s
	}
	if s := r.URL.Query().Get("target_branch"); s != "" {
		filter.TargetBranch = s
	}

	prs, total, err := h.PRRepo.ListByRepo(r.Context(), repoID, filter, limit, offset)
	if err != nil {
		slog.Error("failed to list pull requests", "error", err, "repo_id", repoID)
		writeError(w, http.StatusInternalServerError, "failed to list pull requests")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data":     prs,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+limit < total,
	})
}

// GetPullRequest returns a single tracked PR by ID.
// GET /api/v1/repos/{repoID}/pull-requests/{prID}
func (h *Handler) GetPullRequest(w http.ResponseWriter, r *http.Request) {
	prID, err := uuid.Parse(chi.URLParam(r, "prID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull request ID")
		return
	}

	pr, err := h.PRRepo.GetByID(r.Context(), prID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "pull request not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	writeJSON(w, http.StatusOK, pr)
}

// GetPullRequestByNumber returns a tracked PR by its PR number within a repo.
// GET /api/v1/repos/{repoID}/pull-requests/by-number/{prNumber}
func (h *Handler) GetPullRequestByNumber(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	prNumber, err := strconv.Atoi(chi.URLParam(r, "prNumber"))
	if err != nil || prNumber <= 0 {
		writeError(w, http.StatusBadRequest, "invalid PR number")
		return
	}

	pr, err := h.PRRepo.GetByRepoAndNumber(r.Context(), repoID, prNumber)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "pull request not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	writeJSON(w, http.StatusOK, pr)
}

// SyncPullRequests triggers an immediate sync of PRs for a repository.
// POST /api/v1/repos/{repoID}/pull-requests/sync
func (h *Handler) SyncPullRequests(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	// Verify repo exists
	_, err = h.RepoRepo.GetByID(r.Context(), repoID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "repository not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	// Trigger sync in background
	if h.PRSyncWorker != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := h.PRSyncWorker.SyncSingleRepo(ctx, repoID); err != nil {
				slog.Error("manual PR sync failed", "error", err, "repo_id", repoID)
			}
		}()
	}

	h.AuditLogger.LogRequest(r, audit.ActionPRSync, "repository", repoID.String(), nil)

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  "accepted",
		"message": "PR sync has been triggered",
	})
}

// GetPullRequestStats returns PR counts grouped by sync status for a repository.
// GET /api/v1/repos/{repoID}/pull-requests/stats
func (h *Handler) GetPullRequestStats(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	counts, err := h.PRRepo.CountByRepo(r.Context(), repoID)
	if err != nil {
		slog.Error("failed to get PR stats", "error", err, "repo_id", repoID)
		writeError(w, http.StatusInternalServerError, "failed to get PR stats")
		return
	}

	embedQueue, err := h.PRRepo.CountEmbedQueue(r.Context(), repoID)
	if err != nil {
		slog.Error("failed to get embed queue count", "error", err, "repo_id", repoID)
		// Non-fatal: continue with 0
		embedQueue = 0
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"counts":      counts,
		"embed_queue": embedQueue,
	})
}

// ReviewPullRequest triggers a review of a specific tracked PR using the
// engine's pre-embedded context to minimize LLM usage.
// POST /api/v1/repos/{repoID}/pull-requests/{prID}/review
func (h *Handler) ReviewPullRequest(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	prID, err := uuid.Parse(chi.URLParam(r, "prID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull request ID")
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Look up the tracked PR
	pr, err := h.PRRepo.GetByID(r.Context(), prID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "pull request not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	if pr.RepoID != repoID {
		writeError(w, http.StatusBadRequest, "PR does not belong to this repository")
		return
	}

	// Update review status to pending
	pendingStatus := model.PRReviewPending
	_ = h.PRRepo.UpdateReviewStatus(r.Context(), prID, pendingStatus, nil)

	// Launch the review pipeline in the background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		result, pipelineErr := h.ReviewPipeline.Execute(ctx, review.Request{
			RepoID:      repoID,
			PRNumber:    pr.PRNumber,
			TriggeredBy: userID,
		})
		if pipelineErr != nil {
			slog.Error("PR review pipeline failed",
				"error", pipelineErr,
				"pr_id", prID,
				"pr_number", pr.PRNumber,
			)
			_ = h.PRRepo.UpdateReviewStatus(context.Background(), prID, model.PRReviewNone, nil)
			return
		}

		// Update the tracked PR with review result
		_ = h.PRRepo.UpdateReviewStatus(context.Background(), prID, model.PRReviewCompleted, &result.ReviewID)
		slog.Info("PR review completed",
			"review_id", result.ReviewID,
			"pr_id", prID,
			"comments", result.CommentsCount,
		)
	}()

	h.AuditLogger.LogRequest(r, audit.ActionReviewTrigger, "pull_request", prID.String(), map[string]interface{}{
		"pr_number": pr.PRNumber,
		"source":    "pr_dashboard",
	})

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  "accepted",
		"message": fmt.Sprintf("Review for PR #%d has been queued", pr.PRNumber),
	})
}
