package swarm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
	wsHub       *WSHub
}

// NewPRCreator creates a new PR creator.
func NewPRCreator(db *pgxpool.Pool, vcsResolver *vcs.Resolver, taskMgr *TaskManager, wsHub *WSHub) *PRCreator {
	return &PRCreator{
		db:          db,
		vcsResolver: vcsResolver,
		taskMgr:     taskMgr,
		wsHub:       wsHub,
	}
}

// CreatePR creates a pull request for a completed task.
// 1. Get diffs for this task
// 2. Look up repo platform (github/gitlab/etc) from repositories table
// 3. Create branch, commit diffs, open PR via VCS client
// 4. Store PR URL + number in swarm_tasks
func (c *PRCreator) CreatePR(ctx context.Context, taskID uuid.UUID) error {
	task, err := c.taskMgr.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("pr_creator: get task: %w", err)
	}

	slog.Info("swarm pr_creator: creating PR",
		"task_id", taskID,
		"repo_id", task.RepoID,
	)

	// Emit progress event.
	c.broadcastPREvent(taskID, "pr_creating", nil)

	// 1. Get all approved diffs for this task.
	diffs, err := c.taskMgr.ListDiffs(ctx, taskID)
	if err != nil {
		return c.failPR(ctx, taskID, fmt.Errorf("listing diffs: %w", err))
	}
	if len(diffs) == 0 {
		return c.failPR(ctx, taskID, fmt.Errorf("no diffs found for task %s", taskID))
	}

	// 2. Resolve VCS client for this repo.
	repoID, err := uuid.Parse(task.RepoID)
	if err != nil {
		return c.failPR(ctx, taskID, fmt.Errorf("invalid repo_id %q: %w", task.RepoID, err))
	}

	platform, err := c.vcsResolver.ForRepo(ctx, repoID)
	if err != nil {
		return c.failPR(ctx, taskID, fmt.Errorf("resolving VCS client: %w", err))
	}

	// Look up repo owner/name from DB.
	owner, repoName, err := c.getRepoInfo(ctx, repoID)
	if err != nil {
		return c.failPR(ctx, taskID, fmt.Errorf("get repo info: %w", err))
	}

	// 3. Get default branch and its HEAD SHA.
	defaultBranch, err := platform.GetDefaultBranch(ctx, owner, repoName)
	if err != nil {
		return c.failPR(ctx, taskID, fmt.Errorf("get default branch: %w", err))
	}

	baseSHA, err := platform.GetBranchSHA(ctx, owner, repoName, defaultBranch)
	if err != nil {
		return c.failPR(ctx, taskID, fmt.Errorf("get branch SHA: %w", err))
	}

	// 4. Create a branch for this task.
	branchName := fmt.Sprintf("swarm/task-%s", taskID.String()[:8])
	if err := platform.CreateBranch(ctx, owner, repoName, &vcs.CreateBranchRequest{
		BranchName: branchName,
		FromSHA:    baseSHA,
	}); err != nil {
		return c.failPR(ctx, taskID, fmt.Errorf("create branch: %w", err))
	}

	c.broadcastPREvent(taskID, "branch_created", map[string]interface{}{
		"branch": branchName,
	})

	// 5. Commit each diff to the branch.
	var lastCommitSHA string
	for i, diff := range diffs {
		commitMsg := fmt.Sprintf("swarm: %s %s\n\nTask: %s\nAgent: %s",
			diff.ChangeType, diff.FilePath, taskID.String()[:8],
			formatAgentID(diff.AgentID))

		sha, err := platform.CreateOrUpdateFile(ctx, owner, repoName, branchName, &vcs.FileCommit{
			Path:    diff.FilePath,
			Content: diff.Proposed,
			Message: commitMsg,
		})
		if err != nil {
			return c.failPR(ctx, taskID, fmt.Errorf("commit file %s: %w", diff.FilePath, err))
		}
		lastCommitSHA = sha

		c.broadcastPREvent(taskID, "file_committed", map[string]interface{}{
			"file_path": diff.FilePath,
			"progress":  fmt.Sprintf("%d/%d", i+1, len(diffs)),
		})
	}

	// 6. Create the pull request.
	prTitle := c.buildPRTitle(task)
	prBody := c.buildPRBody(task, diffs, branchName, lastCommitSHA)

	pr, err := platform.CreatePullRequest(ctx, owner, repoName, &vcs.CreatePullRequestRequest{
		Title:        prTitle,
		Body:         prBody,
		SourceBranch: branchName,
		TargetBranch: defaultBranch,
		Draft:        false,
	})
	if err != nil {
		return c.failPR(ctx, taskID, fmt.Errorf("create PR: %w", err))
	}

	// 7. Store PR URL + number in swarm_tasks.
	_, dbErr := c.db.Exec(ctx, `
		UPDATE swarm_tasks SET pr_url = $1, pr_number = $2, status = $3 WHERE id = $4`,
		pr.URL, pr.Number, StatusCompleted, taskID,
	)
	if dbErr != nil {
		slog.Error("swarm pr_creator: failed to update task with PR info",
			"task_id", taskID, "error", dbErr, "pr_url", pr.URL)
	}

	c.broadcastPREvent(taskID, "pr_created", map[string]interface{}{
		"pr_url":    pr.URL,
		"pr_number": pr.Number,
		"branch":    branchName,
	})

	SwarmPRsCreated.Inc()

	slog.Info("swarm pr_creator: PR created successfully",
		"task_id", taskID,
		"pr_url", pr.URL,
		"pr_number", pr.Number,
		"files", len(diffs),
	)

	return nil
}

// getRepoInfo returns the owner and repo name from the repositories table.
func (c *PRCreator) getRepoInfo(ctx context.Context, repoID uuid.UUID) (owner, repoName string, err error) {
	var fullName string
	err = c.db.QueryRow(ctx,
		`SELECT full_name FROM repositories WHERE id = $1`, repoID,
	).Scan(&fullName)
	if err != nil {
		return "", "", fmt.Errorf("querying repo %s: %w", repoID, err)
	}

	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo full_name %q (expected owner/repo)", fullName)
	}
	return parts[0], parts[1], nil
}

// buildPRTitle generates a PR title from the task description.
func (c *PRCreator) buildPRTitle(task *Task) string {
	desc := task.Description
	if len(desc) > 72 {
		desc = desc[:69] + "..."
	}
	return fmt.Sprintf("[Swarm] %s", desc)
}

// buildPRBody generates a structured PR description.
func (c *PRCreator) buildPRBody(task *Task, diffs []TaskDiff, branch, commitSHA string) string {
	var b strings.Builder

	b.WriteString("## 🤖 Swarm Agent Pull Request\n\n")
	b.WriteString(fmt.Sprintf("**Task ID:** `%s`\n", task.ID))
	b.WriteString(fmt.Sprintf("**Description:** %s\n", task.Description))
	b.WriteString(fmt.Sprintf("**Branch:** `%s`\n", branch))
	b.WriteString(fmt.Sprintf("**Created:** %s\n\n", time.Now().UTC().Format(time.RFC3339)))

	b.WriteString("### Files Changed\n\n")
	b.WriteString("| File | Change | Agent |\n")
	b.WriteString("|------|--------|-------|\n")
	for _, d := range diffs {
		agentStr := "—"
		if d.AgentID != nil {
			agentStr = fmt.Sprintf("`%s`", d.AgentID.String()[:8])
		}
		b.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", d.FilePath, d.ChangeType, agentStr))
	}

	b.WriteString("\n---\n")
	b.WriteString("*This PR was created automatically by the RTVortex Agent Swarm.*\n")

	return b.String()
}

// failPR logs the error, sets the task to failed, and returns it.
func (c *PRCreator) failPR(ctx context.Context, taskID uuid.UUID, err error) error {
	slog.Error("swarm pr_creator: PR creation failed", "task_id", taskID, "error", err)
	_ = c.taskMgr.UpdateStatus(ctx, taskID, StatusFailed)

	SwarmPRsFailed.Inc()

	c.broadcastPREvent(taskID, "pr_failed", map[string]interface{}{
		"error": err.Error(),
	})

	return fmt.Errorf("pr_creator: %w", err)
}

// broadcastPREvent emits a WebSocket event for PR creation progress.
func (c *PRCreator) broadcastPREvent(taskID uuid.UUID, event string, data map[string]interface{}) {
	if c.wsHub != nil {
		c.wsHub.BroadcastTaskEvent(event, taskID.String(), data)
	}
}

// formatAgentID returns a short agent ID string or "unknown".
func formatAgentID(id *uuid.UUID) string {
	if id == nil {
		return "unknown"
	}
	return id.String()[:8]
}
