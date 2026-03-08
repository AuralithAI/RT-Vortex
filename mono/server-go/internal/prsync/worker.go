// Package prsync provides background pull request discovery and synchronisation.
//
// It periodically polls all registered repositories across all connected VCS
// platforms, discovers open PRs, upserts them into the tracked_pull_requests
// table, marks stale PRs, and optionally pre-embeds diffs via the C++ engine.
package prsync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/model"
	"github.com/AuralithAI/rtvortex-server/internal/store"
	"github.com/AuralithAI/rtvortex-server/internal/vcs"
	"github.com/AuralithAI/rtvortex-server/internal/ws"
)

// Config holds configuration for the PR sync worker.
type Config struct {
	// SyncInterval is how often the worker polls VCS platforms for new PRs.
	SyncInterval time.Duration

	// EmbedInterval is how often the worker checks for PRs needing embedding.
	EmbedInterval time.Duration

	// MaxPRsPerRepo limits how many open PRs to fetch per repository.
	MaxPRsPerRepo int

	// MaxConcurrentSyncs limits how many repos are synced in parallel.
	MaxConcurrentSyncs int

	// MaxEmbedBatchSize limits how many PRs are embedded per cycle.
	MaxEmbedBatchSize int

	// StaleAfter marks open PRs as stale if not seen in this duration.
	StaleAfter time.Duration

	// EnableEmbedding controls whether PR diffs are pre-embedded by the engine.
	EnableEmbedding bool
}

// DefaultConfig returns a production-ready default configuration.
func DefaultConfig() Config {
	return Config{
		SyncInterval:       5 * time.Minute,
		EmbedInterval:      2 * time.Minute,
		MaxPRsPerRepo:      200,
		MaxConcurrentSyncs: 4,
		MaxEmbedBatchSize:  10,
		StaleAfter:         24 * time.Hour,
		EnableEmbedding:    true,
	}
}

// Worker performs background PR discovery and sync.
type Worker struct {
	ctx    context.Context
	cancel context.CancelFunc

	prRepo       *store.PullRequestRepo
	repoRepo     *store.RepositoryRepo
	vcsRegistry  *vcs.PlatformRegistry
	engineClient *engine.Client
	wsHub        *ws.Hub

	cfg Config

	// embedUnsupported is set to true when the C++ engine returns Unimplemented
	// for BuildReviewContext. This prevents repeatedly trying every cycle.
	embedMu          sync.Mutex
	embedUnsupported bool
}

// NewWorker creates a new PR sync worker.
func NewWorker(
	ctx context.Context,
	prRepo *store.PullRequestRepo,
	repoRepo *store.RepositoryRepo,
	vcsRegistry *vcs.PlatformRegistry,
	engineClient *engine.Client,
	wsHub *ws.Hub,
	cfg Config,
) *Worker {
	workerCtx, cancel := context.WithCancel(ctx)
	return &Worker{
		ctx:          workerCtx,
		cancel:       cancel,
		prRepo:       prRepo,
		repoRepo:     repoRepo,
		vcsRegistry:  vcsRegistry,
		engineClient: engineClient,
		wsHub:        wsHub,
		cfg:          cfg,
	}
}

// Start launches the background sync and embed loops.
func (w *Worker) Start() {
	slog.Info("PR sync worker starting",
		"sync_interval", w.cfg.SyncInterval,
		"embed_interval", w.cfg.EmbedInterval,
		"max_prs_per_repo", w.cfg.MaxPRsPerRepo,
		"embedding_enabled", w.cfg.EnableEmbedding,
	)

	go w.runPeriodic("pr-sync", w.cfg.SyncInterval, w.syncAllRepos)

	if w.cfg.EnableEmbedding {
		go w.runPeriodic("pr-embed", w.cfg.EmbedInterval, w.embedPendingPRs)
	}
}

// Stop cancels all background work.
func (w *Worker) Stop() {
	w.cancel()
	slog.Info("PR sync worker stopped")
}

// runPeriodic runs a task at regular intervals with panic recovery.
func (w *Worker) runPeriodic(name string, interval time.Duration, fn func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on first tick.
	func() {
		defer w.recoverPanic(name)
		fn()
	}()

	for {
		select {
		case <-w.ctx.Done():
			slog.Debug("PR sync task stopping", "task", name)
			return
		case <-ticker.C:
			func() {
				defer w.recoverPanic(name)
				fn()
			}()
		}
	}
}

func (w *Worker) recoverPanic(name string) {
	if r := recover(); r != nil {
		slog.Error("PR sync task panicked", "task", name, "panic", r)
	}
}

// ── PR Discovery & Sync ────────────────────────────────────────────────────

// syncAllRepos iterates through all registered repositories and syncs their
// open PRs from the respective VCS platform.
func (w *Worker) syncAllRepos() {
	// Fetch all repos (no pagination — we want them all).
	// Use a large limit; repositories table is bounded by org plans.
	repos, err := w.listAllRepos(w.ctx)
	if err != nil {
		slog.Error("PR sync: failed to list repositories", "error", err)
		return
	}

	if len(repos) == 0 {
		return
	}

	slog.Debug("PR sync: starting sync cycle", "repos", len(repos))

	sem := make(chan struct{}, w.cfg.MaxConcurrentSyncs)
	var wg sync.WaitGroup

	for _, repo := range repos {
		repo := repo // capture loop var
		wg.Add(1)
		sem <- struct{}{} // acquire semaphore
		go func() {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore
			defer func() {
				if r := recover(); r != nil {
					slog.Error("PR sync: panic syncing repo",
						"repo", repo.Owner+"/"+repo.Name,
						"panic", r,
					)
				}
			}()

			w.syncRepo(w.ctx, repo)
		}()
	}

	wg.Wait()
	slog.Debug("PR sync: cycle complete", "repos", len(repos))
}

// syncRepo syncs open PRs for a single repository.
func (w *Worker) syncRepo(ctx context.Context, repo *model.Repository) {
	platform, ok := w.vcsRegistry.Get(vcs.PlatformType(repo.Platform))
	if !ok {
		slog.Debug("PR sync: no VCS client for platform", "platform", repo.Platform, "repo", repo.Owner+"/"+repo.Name)
		return
	}

	syncStart := time.Now()
	log := slog.With("repo", repo.Owner+"/"+repo.Name, "platform", repo.Platform, "repo_id", repo.ID)

	// Fetch open PRs from VCS
	openPRs, err := platform.ListOpenPullRequests(ctx, repo.Owner, repo.Name, w.cfg.MaxPRsPerRepo)
	if err != nil {
		log.Error("PR sync: failed to list PRs", "error", err)
		return
	}

	log.Debug("PR sync: fetched open PRs", "count", len(openPRs))

	// Upsert each PR
	for _, pr := range openPRs {
		syncStatus := model.PRSyncStatusOpen
		if pr.Draft {
			syncStatus = model.PRSyncStatusDraft
		}

		tracked := &model.TrackedPullRequest{
			RepoID:       repo.ID,
			Platform:     repo.Platform,
			PRNumber:     pr.Number,
			ExternalID:   pr.ID,
			Title:        pr.Title,
			Description:  truncateString(pr.Description, 10000),
			Author:       pr.Author,
			SourceBranch: pr.SourceBranch,
			TargetBranch: pr.TargetBranch,
			HeadSHA:      pr.HeadSHA,
			BaseSHA:      pr.BaseSHA,
			PRURL:        pr.URL,
			SyncStatus:   syncStatus,
			ReviewStatus: model.PRReviewNone,
		}

		if err := w.prRepo.Upsert(ctx, tracked); err != nil {
			log.Error("PR sync: failed to upsert PR",
				"pr_number", pr.Number,
				"error", err,
			)
			continue
		}
	}

	// Mark PRs as stale that were not refreshed in this cycle (they've been closed/merged upstream).
	if w.cfg.StaleAfter > 0 {
		cutoff := syncStart.Add(-w.cfg.StaleAfter)
		staleCount, err := w.prRepo.MarkStaleBefore(ctx, repo.ID, cutoff)
		if err != nil {
			log.Error("PR sync: failed to mark stale PRs", "error", err)
		} else if staleCount > 0 {
			log.Info("PR sync: marked stale PRs", "count", staleCount)
		}
	}

	log.Debug("PR sync: repo sync complete",
		"open_prs", len(openPRs),
		"duration", time.Since(syncStart).Round(time.Millisecond),
	)
}

// ── PR Diff Pre-Embedding ──────────────────────────────────────────────────

// embedPendingPRs finds PRs that need their diffs embedded and sends them
// to the C++ engine for pre-indexing.
func (w *Worker) embedPendingPRs() {
	// Skip if we've already discovered the engine doesn't support embedding
	w.embedMu.Lock()
	unsupported := w.embedUnsupported
	w.embedMu.Unlock()
	if unsupported {
		return
	}

	prs, err := w.prRepo.ListNeedingEmbedding(w.ctx, w.cfg.MaxEmbedBatchSize)
	if err != nil {
		slog.Error("PR embed: failed to list PRs needing embedding", "error", err)
		return
	}

	if len(prs) == 0 {
		return
	}

	slog.Debug("PR embed: processing batch", "count", len(prs))

	for _, pr := range prs {
		if w.ctx.Err() != nil {
			return // context cancelled
		}
		w.embedPR(w.ctx, pr)
	}
}

// embedPR pre-embeds the diff for a single tracked PR using the C++ engine.
func (w *Worker) embedPR(ctx context.Context, pr *model.TrackedPullRequest) {
	log := slog.With("pr_id", pr.ID, "pr_number", pr.PRNumber, "repo_id", pr.RepoID)

	// Look up the repo for VCS client
	repo, err := w.repoRepo.GetByID(ctx, pr.RepoID)
	if err != nil {
		log.Error("PR embed: failed to look up repo", "error", err)
		return
	}

	platform, ok := w.vcsRegistry.Get(vcs.PlatformType(repo.Platform))
	if !ok {
		log.Debug("PR embed: no VCS client for platform", "platform", repo.Platform)
		return
	}

	repoIDStr := pr.RepoID.String()
	prIDStr := pr.ID.String()

	// Broadcast initial "embedding" state
	w.broadcastEmbedProgress(repoIDStr, pr.PRNumber, prIDStr, "embedding", 0, "fetching_diff", "", 0, 0, -1, "")

	// Mark as embedding in progress
	if err := w.prRepo.UpdateSyncStatus(ctx, pr.ID, model.PRSyncStatusEmbedding); err != nil {
		log.Error("PR embed: failed to mark embedding", "error", err)
		return
	}

	// Fetch the diff from VCS
	w.broadcastEmbedProgress(repoIDStr, pr.PRNumber, prIDStr, "embedding", 10, "fetching_diff", "Fetching diff from VCS", 0, 0, -1, "")

	diffFiles, err := platform.GetPullRequestDiff(ctx, repo.Owner, repo.Name, pr.PRNumber)
	if err != nil {
		errMsg := fmt.Sprintf("failed to fetch diff: %v", err)
		log.Error("PR embed: " + errMsg)
		_ = w.prRepo.MarkEmbedError(ctx, pr.ID, errMsg)
		w.broadcastEmbedProgress(repoIDStr, pr.PRNumber, prIDStr, "failed", 0, "fetching_diff", "", 0, 0, -1, errMsg)
		return
	}

	// Build a combined diff string for the engine
	var diffBuilder strings.Builder
	var totalAdditions, totalDeletions int
	filesTotal := uint32(len(diffFiles))
	for i, f := range diffFiles {
		diffBuilder.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", f.Filename, f.Filename))
		diffBuilder.WriteString(f.Patch)
		diffBuilder.WriteString("\n")
		totalAdditions += f.Additions
		totalDeletions += f.Deletions

		// Broadcast diff parsing progress (10-30% range)
		progress := 10 + int(float64(i+1)/float64(filesTotal)*20)
		w.broadcastEmbedProgress(repoIDStr, pr.PRNumber, prIDStr, "embedding", progress, "parsing_diff", f.Filename, uint32(i+1), filesTotal, -1, "")
	}

	// Update file stats
	pr.FilesChanged = len(diffFiles)
	pr.Additions = totalAdditions
	pr.Deletions = totalDeletions

	w.broadcastEmbedProgress(repoIDStr, pr.PRNumber, prIDStr, "embedding", 30, "embedding_chunks", "Sending to engine for embedding", 0, filesTotal, -1, "")

	// Call engine's BuildReviewContext to pre-embed the diff
	if w.engineClient != nil {
		// Try streaming version first (provides real-time progress), fall back to unary
		_, err = w.engineClient.BuildReviewContextStream(ctx,
			repo.ID.String(),
			diffBuilder.String(),
			pr.Title,
			pr.Description,
			50, // max context chunks
			func(update engine.PREmbedProgressUpdate) {
				// Map engine progress (0-100) into our 30-90% range
				mappedProgress := 30 + int(float64(update.Progress)*0.6)
				state := "embedding"
				if update.Done && update.Success {
					state = "completed"
					mappedProgress = 95
				} else if update.Done && !update.Success {
					state = "failed"
				}
				w.broadcastEmbedProgress(repoIDStr, pr.PRNumber, prIDStr, state,
					mappedProgress, update.Phase, update.CurrentFile,
					update.FilesProcessed, update.FilesTotal,
					update.ETASeconds, update.Error)
			},
		)
		if err != nil {
			// If the engine doesn't support BuildReviewContext at all (Unimplemented),
			// revert PR to "open" status and skip embedding gracefully — the C++ engine
			// hasn't implemented this RPC yet.
			if s, ok := grpcstatus.FromError(err); ok && s.Code() == codes.Unimplemented {
				log.Info("PR embed: engine does not support BuildReviewContext yet, disabling embedding until restart")
				w.embedMu.Lock()
				w.embedUnsupported = true
				w.embedMu.Unlock()
				_ = w.prRepo.UpdateSyncStatus(ctx, pr.ID, model.PRSyncStatusOpen)
				w.broadcastEmbedProgress(repoIDStr, pr.PRNumber, prIDStr, "completed", 0, "skipped", "Engine embedding not available yet", 0, filesTotal, -1, "")
				return
			}

			errMsg := fmt.Sprintf("engine BuildReviewContext failed: %v", err)
			log.Warn("PR embed: " + errMsg)
			_ = w.prRepo.MarkEmbedError(ctx, pr.ID, errMsg)
			w.broadcastEmbedProgress(repoIDStr, pr.PRNumber, prIDStr, "failed", 0, "embedding_chunks", "", 0, filesTotal, -1, errMsg)
			return
		}
	}

	// Mark as successfully embedded
	if err := w.prRepo.MarkEmbedded(ctx, pr.ID); err != nil {
		log.Error("PR embed: failed to mark as embedded", "error", err)
	} else {
		log.Info("PR embed: successfully embedded PR diff",
			"files", len(diffFiles),
			"additions", totalAdditions,
			"deletions", totalDeletions,
		)
	}

	// Broadcast completion
	w.broadcastEmbedProgress(repoIDStr, pr.PRNumber, prIDStr, "completed", 100, "finalizing",
		fmt.Sprintf("Embedded %d files (+%d/-%d)", len(diffFiles), totalAdditions, totalDeletions),
		filesTotal, filesTotal, 0, "")
}

// broadcastEmbedProgress sends a PR embed progress event to WebSocket clients.
func (w *Worker) broadcastEmbedProgress(repoID string, prNumber int, prID string, state string,
	progress int, phase string, currentFile string,
	filesProcessed, filesTotal uint32, etaSeconds int64, errMsg string) {
	if w.wsHub == nil {
		return
	}
	w.wsHub.BroadcastPREmbed(repoID, ws.PREmbedProgressEvent{
		PRNumber:       prNumber,
		PRID:           prID,
		State:          state,
		Progress:       progress,
		Phase:          phase,
		CurrentFile:    currentFile,
		FilesProcessed: filesProcessed,
		FilesTotal:     filesTotal,
		ETASeconds:     etaSeconds,
		Error:          errMsg,
	})
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// listAllRepos fetches all repositories from the database in batches.
func (w *Worker) listAllRepos(ctx context.Context) ([]*model.Repository, error) {
	var all []*model.Repository
	offset := 0
	batchSize := 100

	for {
		// We call ListByOrg with a nil org, but the repo store doesn't support that.
		// Instead, do a direct pool query for all repos.
		repos, err := w.listReposBatch(ctx, batchSize, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, repos...)
		if len(repos) < batchSize {
			break
		}
		offset += batchSize
	}
	return all, nil
}

// listReposBatch is a direct query for all repos with pagination.
func (w *Worker) listReposBatch(ctx context.Context, limit, offset int) ([]*model.Repository, error) {
	// We access the pool through repoRepo by listing repos across all orgs.
	// This is a pragmatic approach for the background worker.
	// In a real deployment, we'd iterate org-by-org.
	rows, err := w.repoRepo.Pool().Query(ctx,
		`SELECT id, org_id, platform, external_id, owner, name, default_branch, clone_url, webhook_secret, config, indexed_at, created_at, updated_at
		 FROM repositories ORDER BY name LIMIT $1 OFFSET $2`, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list all repos: %w", err)
	}
	defer rows.Close()

	var repos []*model.Repository
	for rows.Next() {
		repo := &model.Repository{}
		if err := rows.Scan(&repo.ID, &repo.OrgID, &repo.Platform, &repo.ExternalID, &repo.Owner, &repo.Name,
			&repo.DefaultBranch, &repo.CloneURL, &repo.WebhookSecret, &repo.Config, &repo.IndexedAt, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan repo: %w", err)
		}
		repos = append(repos, repo)
	}
	return repos, nil
}

// SyncSingleRepo is the public API for triggering a sync of a single repository.
// Used by API handlers to trigger an immediate sync.
func (w *Worker) SyncSingleRepo(ctx context.Context, repoID uuid.UUID) error {
	repo, err := w.repoRepo.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	w.syncRepo(ctx, repo)
	return nil
}

// truncateString truncates a string to maxLen characters.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
