package benchmark

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AuralithAI/rtvortex-server/internal/model"
	"github.com/AuralithAI/rtvortex-server/internal/review"
	"github.com/AuralithAI/rtvortex-server/internal/store"
	"github.com/AuralithAI/rtvortex-server/internal/swarm"
)

// swarmPollInterval is how often the executor checks swarm task status.
const swarmPollInterval = 2 * time.Second

// PipelineExecutor implements the Executor interface using the real review
// pipeline for single-agent mode and the swarm TaskManager for swarm mode.
type PipelineExecutor struct {
	pipeline   *review.Pipeline
	taskMgr    *swarm.TaskManager
	reviewRepo *store.ReviewRepository
	repoRepo   *store.RepositoryRepo
	db         *pgxpool.Pool
	logger     *slog.Logger
}

// NewPipelineExecutor creates an executor wired to the review pipeline and swarm.
func NewPipelineExecutor(
	pipeline *review.Pipeline,
	taskMgr *swarm.TaskManager,
	reviewRepo *store.ReviewRepository,
	repoRepo *store.RepositoryRepo,
	db *pgxpool.Pool,
	logger *slog.Logger,
) *PipelineExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &PipelineExecutor{
		pipeline:   pipeline,
		taskMgr:    taskMgr,
		reviewRepo: reviewRepo,
		repoRepo:   repoRepo,
		db:         db,
		logger:     logger,
	}
}

// ExecuteReview runs the task through the single-agent review pipeline.
// For real_pr tasks with a repo_id and PR number, it calls the review pipeline
// directly.  For synthetic tasks (no PR), it returns an error because the
// review pipeline requires a real PR to analyze.
func (e *PipelineExecutor) ExecuteReview(ctx context.Context, task *Task) (*ExecutionResult, error) {
	if e.pipeline == nil {
		return nil, fmt.Errorf("review pipeline not configured")
	}

	repoID, prNumber, err := e.resolveRepoAndPR(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("resolving task target: %w", err)
	}

	start := time.Now()

	result, err := e.pipeline.Execute(ctx, review.Request{
		RepoID:      repoID,
		PRNumber:    prNumber,
		TriggeredBy: uuid.Nil,
	})
	if err != nil {
		return nil, fmt.Errorf("review pipeline failed: %w", err)
	}

	execResult := &ExecutionResult{
		Trace: []string{
			fmt.Sprintf("review_id=%s", result.ReviewID),
			fmt.Sprintf("status=%s", result.Status),
			fmt.Sprintf("duration=%s", result.Duration),
		},
	}

	// Collect generated comments from the review.
	if e.reviewRepo != nil {
		comments, listErr := e.reviewRepo.ListCommentsByReview(ctx, result.ReviewID)
		if listErr != nil {
			e.logger.Warn("failed to list review comments", "review_id", result.ReviewID, "error", listErr)
		} else {
			execResult.Comments = convertModelComments(comments)
			execResult.LLMCalls = estimateLLMCalls(comments)
		}
	}

	execResult.Trace = append(execResult.Trace,
		fmt.Sprintf("comments=%d", len(execResult.Comments)),
		fmt.Sprintf("latency=%s", time.Since(start)))

	if result.Status == model.ReviewStatusFailed {
		return execResult, fmt.Errorf("review completed with status: %s", result.Status)
	}

	return execResult, nil
}

// ExecuteSwarm submits the task to the swarm pipeline and polls for completion.
func (e *PipelineExecutor) ExecuteSwarm(ctx context.Context, task *Task) (*ExecutionResult, error) {
	if e.taskMgr == nil {
		return nil, fmt.Errorf("swarm task manager not configured")
	}

	repoID := task.RepoID
	if repoID == "" {
		repoID = task.RepoURL
	}
	if repoID == "" {
		return nil, fmt.Errorf("task has no repo_id or repo_url for swarm execution")
	}

	description := task.Description
	if len(task.Files) > 0 {
		var fileList []string
		for _, f := range task.Files {
			fileList = append(fileList, f.Path)
		}
		description += "\n\nFiles: " + strings.Join(fileList, ", ")
	}

	swarmTask, err := e.taskMgr.CreateTask(ctx, repoID, task.Name, description, uuid.Nil)
	if err != nil {
		return nil, fmt.Errorf("creating swarm task: %w", err)
	}

	execResult := &ExecutionResult{
		Trace: []string{
			fmt.Sprintf("swarm_task_id=%s", swarmTask.ID),
		},
	}

	// Poll for completion.
	completedTask, err := e.pollSwarmTask(ctx, swarmTask.ID)
	if err != nil {
		execResult.Error = err
		return execResult, err
	}

	execResult.Trace = append(execResult.Trace,
		fmt.Sprintf("final_status=%s", completedTask.Status))

	// Collect diffs as the output.
	diffs, err := e.taskMgr.ListDiffs(ctx, swarmTask.ID)
	if err != nil {
		e.logger.Warn("failed to list swarm task diffs", "task_id", swarmTask.ID, "error", err)
	} else {
		var diffParts []string
		for _, d := range diffs {
			if d.UnifiedDiff != "" {
				diffParts = append(diffParts, d.UnifiedDiff)
			}
			execResult.Comments = append(execResult.Comments, GenComment{
				File:     d.FilePath,
				Body:     d.ChangeType + ": " + d.FilePath,
				Severity: "info",
			})
		}
		execResult.DiffOutput = strings.Join(diffParts, "\n")
	}

	// Aggregate LLM calls and tokens from agent task logs.
	llmCalls, tokensUsed := e.aggregateSwarmMetrics(ctx, swarmTask.ID)
	execResult.LLMCalls = llmCalls
	execResult.TokensUsed = tokensUsed

	if completedTask.Status == swarm.StatusFailed || completedTask.Status == swarm.StatusTimedOut {
		return execResult, fmt.Errorf("swarm task ended with status: %s reason: %s",
			completedTask.Status, completedTask.FailureReason)
	}

	return execResult, nil
}

// pollSwarmTask polls the swarm task until it reaches a terminal state.
func (e *PipelineExecutor) pollSwarmTask(ctx context.Context, taskID uuid.UUID) (*swarm.Task, error) {
	ticker := time.NewTicker(swarmPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled while waiting for swarm task: %w", ctx.Err())
		case <-ticker.C:
			task, err := e.taskMgr.GetTask(ctx, taskID)
			if err != nil {
				e.logger.Warn("failed to poll swarm task", "task_id", taskID, "error", err)
				continue
			}
			switch task.Status {
			case swarm.StatusCompleted, swarm.StatusFailed, swarm.StatusTimedOut, swarm.StatusCancelled:
				return task, nil
			}
		}
	}
}

// aggregateSwarmMetrics sums LLM calls and tokens from the agent_task_log
// table for a given swarm task.
func (e *PipelineExecutor) aggregateSwarmMetrics(ctx context.Context, taskID uuid.UUID) (llmCalls, tokensUsed int) {
	if e.db == nil {
		return 0, 0
	}
	row := e.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(llm_calls), 0), COALESCE(SUM(tokens_used), 0)
		 FROM agent_task_log WHERE task_id = $1`, taskID)
	if err := row.Scan(&llmCalls, &tokensUsed); err != nil {
		e.logger.Warn("failed to aggregate swarm task metrics", "task_id", taskID, "error", err)
	}
	return
}

// resolveRepoAndPR extracts the repository UUID and PR number from a
// benchmark task.  Tasks with repo_id set use it directly; others attempt
// to parse the repo_url as "owner/name".
func (e *PipelineExecutor) resolveRepoAndPR(ctx context.Context, task *Task) (uuid.UUID, int, error) {
	var repoUUID uuid.UUID

	if task.RepoID != "" {
		parsed, err := uuid.Parse(task.RepoID)
		if err != nil {
			return uuid.Nil, 0, fmt.Errorf("invalid repo_id %q: %w", task.RepoID, err)
		}
		repoUUID = parsed
	} else if task.RepoURL != "" {
		return uuid.Nil, 0, fmt.Errorf("repo_url resolution not yet available; provide repo_id")
	} else {
		return uuid.Nil, 0, fmt.Errorf("task has neither repo_id nor repo_url")
	}

	// Extract PR number from task metadata.
	prNumber := 0
	if task.Metadata != nil {
		if prStr, ok := task.Metadata["pr_number"]; ok {
			fmt.Sscanf(prStr, "%d", &prNumber)
		}
	}
	if prNumber == 0 {
		return uuid.Nil, 0, fmt.Errorf("task missing pr_number in metadata")
	}

	return repoUUID, prNumber, nil
}

// convertModelComments maps review comments from the model layer to benchmark
// GenComment types.
func convertModelComments(comments []*model.ReviewComment) []GenComment {
	out := make([]GenComment, 0, len(comments))
	for _, c := range comments {
		out = append(out, GenComment{
			File:     c.FilePath,
			Line:     c.LineNumber,
			Body:     c.Body,
			Severity: string(c.Severity),
		})
	}
	return out
}

// estimateLLMCalls returns a rough estimate of LLM invocations based on the
// generated comments.  Heuristic-only comments don't cost LLM calls; LLM
// generated comments count as one call per file analyzed.
func estimateLLMCalls(comments []*model.ReviewComment) int {
	seen := make(map[string]bool)
	for _, c := range comments {
		if c.Category != "heuristic" {
			seen[c.FilePath] = true
		}
	}
	return len(seen)
}
