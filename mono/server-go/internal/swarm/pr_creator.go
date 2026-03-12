package swarm

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AuralithAI/rtvortex-server/internal/vcs"
)

// PRCreator creates pull requests from approved diffs using the existing VCS
// platform clients (GitHub, GitLab, Bitbucket, Azure DevOps).
type PRCreator struct {
	db          *pgxpool.Pool
	vcsResolver *vcs.Resolver
	taskMgr     *TaskManager
}

// NewPRCreator creates a new PR creator.
func NewPRCreator(db *pgxpool.Pool, vcsResolver *vcs.Resolver, taskMgr *TaskManager) *PRCreator {
	return &PRCreator{
		db:          db,
		vcsResolver: vcsResolver,
		taskMgr:     taskMgr,
	}
}

// CreatePR creates a pull request for a completed task. This is a Phase 2
// implementation — for Phase 0 it logs the intent and marks the task.
func (c *PRCreator) CreatePR(ctx context.Context, taskID uuid.UUID) error {
	task, err := c.taskMgr.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	slog.Info("swarm pr_creator: would create PR",
		"task_id", taskID,
		"repo_id", task.RepoID,
		"status", task.Status,
	)

	// Phase 2: Wire into vcsResolver to actually create the PR:
	// 1. Get diffs for this task
	// 2. Look up repo platform (github/gitlab/etc) from repositories table
	// 3. Create branch, commit diffs, open PR via VCS client
	// 4. Store PR URL + number in swarm_tasks

	return nil
}
