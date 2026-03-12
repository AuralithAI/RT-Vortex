package review

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/llm"
	"github.com/AuralithAI/rtvortex-server/internal/metrics"
	"github.com/AuralithAI/rtvortex-server/internal/model"
	"github.com/AuralithAI/rtvortex-server/internal/store"
	"github.com/AuralithAI/rtvortex-server/internal/vcs"
)

// ── Pipeline ────────────────────────────────────────────────────────────────

// PipelineConfig holds configuration for the review pipeline.
type PipelineConfig struct {
	MaxFilesPerReview int
	MaxDiffSizeBytes  int
	ConcurrentFiles   int
	SkipPatterns      []string // file glob patterns to skip
}

// ProgressFunc is called at each pipeline step to report progress.
// It receives the review ID, step name, step index (1-based), total steps, and
// status ("started" or "completed" or "failed").
type ProgressFunc func(reviewID uuid.UUID, step string, stepIndex, totalSteps int, status, message string, meta map[string]interface{})

// Pipeline orchestrates the end-to-end PR review workflow.
type Pipeline struct {
	reviewRepo   *store.ReviewRepository
	repoRepo     *store.RepositoryRepo
	llmRegistry  *llm.Registry
	vcsResolver  *vcs.Resolver
	engineClient *engine.Client
	config       PipelineConfig
	onProgress   ProgressFunc
}

// NewPipeline creates a review pipeline.
func NewPipeline(
	reviewRepo *store.ReviewRepository,
	repoRepo *store.RepositoryRepo,
	llmRegistry *llm.Registry,
	vcsResolver *vcs.Resolver,
	engineClient *engine.Client,
	cfg PipelineConfig,
) *Pipeline {
	if cfg.MaxFilesPerReview == 0 {
		cfg.MaxFilesPerReview = 50
	}
	if cfg.MaxDiffSizeBytes == 0 {
		cfg.MaxDiffSizeBytes = 512 * 1024 // 512 KB
	}
	if cfg.ConcurrentFiles == 0 {
		cfg.ConcurrentFiles = 5
	}
	return &Pipeline{
		reviewRepo:   reviewRepo,
		repoRepo:     repoRepo,
		llmRegistry:  llmRegistry,
		vcsResolver:  vcsResolver,
		engineClient: engineClient,
		config:       cfg,
	}
}

// SetProgressFunc registers a callback for pipeline progress reporting.
func (p *Pipeline) SetProgressFunc(fn ProgressFunc) {
	p.onProgress = fn
}

const totalPipelineSteps = 14

// emitProgress calls the registered progress callback, if any.
func (p *Pipeline) emitProgress(reviewID uuid.UUID, step string, stepIndex int, status, message string, meta map[string]interface{}) {
	if p.onProgress != nil {
		p.onProgress(reviewID, step, stepIndex, totalPipelineSteps, status, message, meta)
	}
}

// ── Review Request ──────────────────────────────────────────────────────────

// Request captures everything needed to start a review.
type Request struct {
	RepoID      uuid.UUID
	PRNumber    int
	TriggeredBy uuid.UUID // user who triggered (or system for webhooks)
}

// Result is the outcome of a review run.
type Result struct {
	ReviewID      uuid.UUID
	Status        model.ReviewStatus
	CommentsCount int
	Duration      time.Duration
	Error         error
}

// ── Execute ─────────────────────────────────────────────────────────────────

// Execute runs the full review pipeline for a pull request.
func (p *Pipeline) Execute(ctx context.Context, req Request) (*Result, error) {
	start := time.Now()

	// 1. Look up the repository.
	p.emitProgress(uuid.Nil, "lookup_repo", 1, "started", "Looking up repository", nil)
	repo, err := p.repoRepo.GetByID(ctx, req.RepoID)
	if err != nil {
		return nil, fmt.Errorf("lookup repo: %w", err)
	}
	p.emitProgress(uuid.Nil, "lookup_repo", 1, "completed", "Repository found: "+repo.Owner+"/"+repo.Name, nil)

	// 2. Get the VCS platform client.
	p.emitProgress(uuid.Nil, "get_vcs_client", 2, "started", "Resolving VCS platform: "+repo.Platform, nil)
	platform, err := p.vcsResolver.ForRepoDirect(ctx, repo.OrgID, vcs.PlatformType(repo.Platform), repo.WebhookSecret)
	if err != nil {
		return nil, fmt.Errorf("resolve VCS client: %w", err)
	}
	p.emitProgress(uuid.Nil, "get_vcs_client", 2, "completed", "VCS platform ready", nil)

	// 3. Create the review record in pending state.
	p.emitProgress(uuid.Nil, "create_review", 3, "started", "Creating review record", nil)
	rev := &model.Review{
		RepoID:      req.RepoID,
		TriggeredBy: req.TriggeredBy,
		Platform:    repo.Platform,
		PRNumber:    req.PRNumber,
		Status:      model.ReviewStatusPending,
	}
	if err := p.reviewRepo.Create(ctx, rev); err != nil {
		return nil, fmt.Errorf("create review record: %w", err)
	}
	p.emitProgress(rev.ID, "create_review", 3, "completed", "Review record created", nil)

	slog.Info("review pipeline started",
		"review_id", rev.ID,
		"repo", fmt.Sprintf("%s/%s", repo.Owner, repo.Name),
		"pr", req.PRNumber,
	)

	// 4. Transition to in_progress.
	p.emitProgress(rev.ID, "status_update", 4, "started", "Transitioning to in_progress", nil)
	if err := p.reviewRepo.UpdateStatus(ctx, rev.ID, model.ReviewStatusInProgress, nil); err != nil {
		slog.Error("failed to update review status", "error", err)
	}
	p.emitProgress(rev.ID, "status_update", 4, "completed", "Review is in progress", nil)

	// 5. Fetch PR metadata.
	p.emitProgress(rev.ID, "fetch_pr", 5, "started", fmt.Sprintf("Fetching PR #%d metadata", req.PRNumber), nil)
	pr, err := platform.GetPullRequest(ctx, repo.Owner, repo.Name, req.PRNumber)
	if err != nil {
		p.emitProgress(rev.ID, "fetch_pr", 5, "failed", err.Error(), nil)
		p.failReview(ctx, rev.ID, err)
		return &Result{ReviewID: rev.ID, Status: model.ReviewStatusFailed, Error: err}, nil
	}
	rev.PRTitle = pr.Title
	rev.PRAuthor = pr.Author
	rev.BaseBranch = pr.TargetBranch
	rev.HeadBranch = pr.SourceBranch
	p.emitProgress(rev.ID, "fetch_pr", 5, "completed", fmt.Sprintf("PR #%d: %s", pr.Number, pr.Title), nil)

	// 6. Fetch the diff.
	p.emitProgress(rev.ID, "fetch_diff", 6, "started", "Fetching PR diff", nil)
	diffFiles, err := platform.GetPullRequestDiff(ctx, repo.Owner, repo.Name, req.PRNumber)
	if err != nil {
		p.emitProgress(rev.ID, "fetch_diff", 6, "failed", err.Error(), nil)
		p.failReview(ctx, rev.ID, err)
		return &Result{ReviewID: rev.ID, Status: model.ReviewStatusFailed, Error: err}, nil
	}
	p.emitProgress(rev.ID, "fetch_diff", 6, "completed", fmt.Sprintf("Fetched %d files", len(diffFiles)), map[string]interface{}{"files_count": len(diffFiles)})

	// 7. Filter files.
	p.emitProgress(rev.ID, "filter_files", 7, "started", "Filtering files", nil)
	filtered := p.filterFiles(diffFiles)
	if len(filtered) == 0 {
		slog.Info("no reviewable files in PR", "pr", req.PRNumber)
		p.emitProgress(rev.ID, "filter_files", 7, "completed", "No reviewable files — skipping", nil)
		meta := map[string]interface{}{"reason": "no reviewable files"}
		_ = p.reviewRepo.UpdateStatus(ctx, rev.ID, model.ReviewStatusCompleted, meta)
		p.emitProgress(rev.ID, "completed", 12, "completed", "Review completed (no files to review)", nil)
		return &Result{ReviewID: rev.ID, Status: model.ReviewStatusCompleted, Duration: time.Since(start)}, nil
	}
	p.emitProgress(rev.ID, "filter_files", 7, "completed", fmt.Sprintf("%d files to review", len(filtered)), map[string]interface{}{"filtered_count": len(filtered)})

	// 8. Build engine context (C++ engine pre-analysis).
	var contextPack *engine.ContextPack
	var heuristicComments []*model.ReviewComment
	combinedDiff := buildCombinedDiff(filtered)

	if p.engineClient != nil {
		p.emitProgress(rev.ID, "engine_context", 8, "started", "Building review context via engine", nil)
		ctxPack, engineErr := p.engineClient.BuildReviewContext(ctx, repo.ID.String(), combinedDiff, pr.Title, pr.Description, 50)
		if engineErr != nil {
			slog.Warn("engine context building failed, falling back to LLM-only", "error", engineErr)
			p.emitProgress(rev.ID, "engine_context", 8, "completed", "Engine unavailable — falling back to LLM-only", nil)
		} else {
			contextPack = ctxPack
			p.emitProgress(rev.ID, "engine_context", 8, "completed",
				fmt.Sprintf("Engine context: %d chunks, %d symbols, est. %d tokens",
					len(ctxPack.ContextChunks), len(ctxPack.TouchedSymbols), ctxPack.TotalTokensEstimate),
				map[string]interface{}{
					"chunks":          len(ctxPack.ContextChunks),
					"touched_symbols": len(ctxPack.TouchedSymbols),
					"tokens_estimate": ctxPack.TotalTokensEstimate,
				})
		}
	} else {
		p.emitProgress(rev.ID, "engine_context", 8, "completed", "Engine not connected — skipping", nil)
	}

	// 9. Run engine heuristics (pattern-based checks — zero LLM cost).
	if p.engineClient != nil {
		p.emitProgress(rev.ID, "engine_heuristics", 9, "started", "Running heuristic checks", nil)
		findings, heurErr := p.engineClient.RunHeuristics(ctx, combinedDiff, nil)
		if heurErr != nil {
			slog.Warn("heuristic checks failed", "error", heurErr)
			p.emitProgress(rev.ID, "engine_heuristics", 9, "completed", "Heuristics unavailable", nil)
		} else {
			heuristicComments = convertHeuristicFindings(findings)
			p.emitProgress(rev.ID, "engine_heuristics", 9, "completed",
				fmt.Sprintf("Found %d heuristic issues (no LLM cost)", len(heuristicComments)),
				map[string]interface{}{"heuristic_count": len(heuristicComments)})
		}
	} else {
		p.emitProgress(rev.ID, "engine_heuristics", 9, "completed", "Engine not connected — skipping", nil)
	}

	// 10. Build LLM prompt and get review (with engine context for reduced token usage).
	p.emitProgress(rev.ID, "analyze_files", 10, "started", fmt.Sprintf("Analyzing %d files with LLM", len(filtered)), nil)
	comments, err := p.analyzeFilesWithContext(ctx, pr, filtered, contextPack)
	if err != nil {
		p.emitProgress(rev.ID, "analyze_files", 10, "failed", err.Error(), nil)
		p.failReview(ctx, rev.ID, err)
		return &Result{ReviewID: rev.ID, Status: model.ReviewStatusFailed, Error: err}, nil
	}
	// Merge heuristic comments (engine-generated, zero LLM cost)
	comments = append(comments, heuristicComments...)
	p.emitProgress(rev.ID, "analyze_files", 10, "completed", fmt.Sprintf("Generated %d comments (%d from engine, %d from LLM)", len(comments), len(heuristicComments), len(comments)-len(heuristicComments)), map[string]interface{}{"comments_count": len(comments), "heuristic_count": len(heuristicComments)})

	// 11. Persist comments.
	p.emitProgress(rev.ID, "persist_comments", 11, "started", "Saving comments to database", nil)
	for _, c := range comments {
		c.ReviewID = rev.ID
		if err := p.reviewRepo.CreateComment(ctx, c); err != nil {
			slog.Error("failed to persist comment", "error", err, "file", c.FilePath)
		}
	}
	p.emitProgress(rev.ID, "persist_comments", 11, "completed", fmt.Sprintf("Saved %d comments", len(comments)), nil)

	// 12. Post comments to VCS.
	p.emitProgress(rev.ID, "post_comments", 12, "started", "Posting comments to VCS", nil)
	for _, c := range comments {
		vcComment := &vcs.ReviewCommentRequest{
			Body:     fmt.Sprintf("**[%s] %s** — %s\n\n%s", c.Severity, c.Category, c.Title, c.Body),
			Path:     c.FilePath,
			Line:     c.LineNumber,
			Side:     "RIGHT",
			CommitID: pr.HeadSHA,
		}
		if c.Suggestion != "" {
			vcComment.Body += fmt.Sprintf("\n\n```suggestion\n%s\n```", c.Suggestion)
		}
		if err := platform.PostReviewComment(ctx, repo.Owner, repo.Name, req.PRNumber, vcComment); err != nil {
			slog.Error("failed to post comment to VCS", "error", err, "file", c.FilePath, "line", c.LineNumber)
		}
	}
	p.emitProgress(rev.ID, "post_comments", 10, "completed", "Comments posted to VCS", nil)

	// 13. Post summary.
	p.emitProgress(rev.ID, "post_summary", 13, "started", "Posting review summary", nil)
	summary := p.buildSummary(pr, comments, time.Since(start))
	if err := platform.PostReviewSummary(ctx, repo.Owner, repo.Name, req.PRNumber, summary); err != nil {
		slog.Error("failed to post review summary", "error", err)
	}
	p.emitProgress(rev.ID, "post_summary", 13, "completed", "Summary posted", nil)

	// 14. Mark completed.
	p.emitProgress(rev.ID, "mark_completed", 14, "started", "Finalizing review", nil)
	duration := time.Since(start)
	meta := map[string]interface{}{
		"duration_ms":     duration.Milliseconds(),
		"files_reviewed":  len(filtered),
		"comments_count":  len(comments),
		"heuristic_count": len(heuristicComments),
		"engine_context":  contextPack != nil,
	}
	_ = p.reviewRepo.UpdateStatus(ctx, rev.ID, model.ReviewStatusCompleted, meta)

	p.emitProgress(rev.ID, "mark_completed", 14, "completed", fmt.Sprintf("Review completed — %d comments in %s", len(comments), duration.Round(time.Millisecond)), map[string]interface{}{
		"duration_ms": duration.Milliseconds(), "comments_count": len(comments), "files_reviewed": len(filtered),
	})

	slog.Info("review pipeline completed",
		"review_id", rev.ID,
		"duration", duration,
		"comments", len(comments),
	)

	// Record Prometheus metrics for the pipeline run.
	metrics.RecordPipelineComplete("completed", duration, len(comments), len(filtered))

	return &Result{
		ReviewID:      rev.ID,
		Status:        model.ReviewStatusCompleted,
		CommentsCount: len(comments),
		Duration:      duration,
	}, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func (p *Pipeline) failReview(ctx context.Context, id uuid.UUID, err error) {
	metrics.ReviewPipelineTotal.WithLabelValues("failed").Inc()
	meta := map[string]interface{}{"error": err.Error()}
	if updateErr := p.reviewRepo.UpdateStatus(ctx, id, model.ReviewStatusFailed, meta); updateErr != nil {
		slog.Error("failed to mark review as failed", "error", updateErr)
	}
}

func (p *Pipeline) filterFiles(files []vcs.DiffFile) []vcs.DiffFile {
	var result []vcs.DiffFile
	for _, f := range files {
		if f.Status == "deleted" {
			continue
		}
		if f.Patch == "" {
			continue
		}
		if p.matchesSkipPattern(f.Filename) {
			slog.Debug("skipping file by glob pattern", "file", f.Filename)
			continue
		}
		result = append(result, f)
	}
	if len(result) > p.config.MaxFilesPerReview {
		result = result[:p.config.MaxFilesPerReview]
	}
	return result
}

// matchesSkipPattern returns true if the filename matches any configured glob
// pattern. Supports filepath.Match syntax (e.g. "*.lock", "vendor/**",
// "**/*.min.js"). For patterns containing "**", we also check every path suffix
// so "**/*.json" matches "a/b/c.json".
func (p *Pipeline) matchesSkipPattern(filename string) bool {
	for _, pattern := range p.config.SkipPatterns {
		// Direct filepath.Match (handles single-level globs like "*.lock").
		if matched, _ := filepath.Match(pattern, filename); matched {
			return true
		}
		// Also match just the base name for patterns like "*.lock".
		if matched, _ := filepath.Match(pattern, filepath.Base(filename)); matched {
			return true
		}
		// Support "**/" prefix patterns (e.g. "**/node_modules/**").
		if strings.Contains(pattern, "**") {
			// Replace "**" with a match-all and test every suffix.
			simple := strings.ReplaceAll(pattern, "**/", "")
			if matched, _ := filepath.Match(simple, filepath.Base(filename)); matched {
				return true
			}
			// Walk suffixes: "a/b/c.js" → try "a/b/c.js", "b/c.js", "c.js"
			parts := strings.Split(filename, "/")
			for i := range parts {
				suffix := strings.Join(parts[i:], "/")
				if matched, _ := filepath.Match(simple, suffix); matched {
					return true
				}
			}
		}
	}
	return false
}

func (p *Pipeline) analyzeFiles(ctx context.Context, pr *vcs.PullRequest, files []vcs.DiffFile) ([]*model.ReviewComment, error) {
	return p.analyzeFilesWithContext(ctx, pr, files, nil)
}

// analyzeFilesWithContext sends each file's diff to the LLM for review.
// When contextPack is non-nil, it enriches the prompt with engine-retrieved
// code context (related functions, callers, etc.) — this produces more
// targeted reviews with significantly fewer tokens.
func (p *Pipeline) analyzeFilesWithContext(ctx context.Context, pr *vcs.PullRequest, files []vcs.DiffFile, contextPack *engine.ContextPack) ([]*model.ReviewComment, error) {
	provider, ok := p.llmRegistry.Primary()
	if !ok {
		return nil, fmt.Errorf("no LLM provider configured")
	}

	var allComments []*model.ReviewComment

	// Build engine context lookup for per-file enrichment.
	fileContextMap := buildFileContextMap(contextPack)

	for _, f := range files {
		prompt := buildEnrichedFileReviewPrompt(pr, f, fileContextMap[f.Filename], contextPack)

		llmStart := time.Now()
		resp, err := provider.Complete(ctx, &llm.CompletionRequest{
			Messages: []llm.Message{
				{Role: llm.RoleSystem, Content: engineAwareSystemPrompt},
				{Role: llm.RoleUser, Content: prompt},
			},
			MaxTokens:   2048,
			Temperature: 0.2,
		})
		llmDuration := time.Since(llmStart)
		if err != nil {
			slog.Error("LLM analysis failed for file", "file", f.Filename, "error", err)
			metrics.RecordLLMRequest(provider.Name(), "", "error", llmDuration, 0, 0)
			continue
		}
		metrics.RecordLLMRequest(provider.Name(), resp.Model, "ok", llmDuration, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)

		comments := parseReviewResponse(resp.Content, f.Filename)
		allComments = append(allComments, comments...)
	}

	return allComments, nil
}

// buildCombinedDiff merges all file diffs into a single unified diff string.
func buildCombinedDiff(files []vcs.DiffFile) string {
	var sb strings.Builder
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", f.Filename, f.Filename))
		sb.WriteString(f.Patch)
		sb.WriteString("\n")
	}
	return sb.String()
}

// buildFileContextMap groups engine context chunks by file path for O(1) lookup.
func buildFileContextMap(pack *engine.ContextPack) map[string][]engine.ContextChunk {
	if pack == nil {
		return nil
	}
	m := make(map[string][]engine.ContextChunk)
	for _, chunk := range pack.ContextChunks {
		m[chunk.FilePath] = append(m[chunk.FilePath], chunk)
	}
	return m
}

// convertHeuristicFindings converts engine heuristic results into ReviewComment
// objects, allowing them to be persisted and posted alongside LLM comments.
func convertHeuristicFindings(findings []engine.HeuristicFinding) []*model.ReviewComment {
	comments := make([]*model.ReviewComment, 0, len(findings))
	for _, f := range findings {
		sev := model.Severity(f.Severity)
		switch sev {
		case model.SeverityCritical, model.SeverityHigh, model.SeverityMedium, model.SeverityLow, model.SeverityInfo:
			// valid
		default:
			sev = model.SeverityInfo
		}
		var endLine *int
		if f.EndLine > 0 {
			el := int(f.EndLine)
			endLine = &el
		}
		comments = append(comments, &model.ReviewComment{
			FilePath:   f.FilePath,
			LineNumber: int(f.Line),
			EndLine:    endLine,
			Severity:   sev,
			Category:   f.Category,
			Title:      fmt.Sprintf("[Heuristic: %s] %s", f.RuleName, f.Message),
			Body:       f.Evidence,
			Suggestion: f.Suggestion,
		})
	}
	return comments
}

// buildEnrichedFileReviewPrompt creates a prompt that includes engine-retrieved
// context (related code, symbol info) alongside the diff for a more focused review.
func buildEnrichedFileReviewPrompt(pr *vcs.PullRequest, file vcs.DiffFile, chunks []engine.ContextChunk, pack *engine.ContextPack) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf(`## Pull Request Context
- **Title:** %s
- **Author:** %s
- **Branch:** %s → %s

## File: %s (%s)

### Diff:
%s`,
		pr.Title, pr.Author, pr.SourceBranch, pr.TargetBranch,
		file.Filename, file.Status,
		file.Patch,
	))

	// Add engine-retrieved context chunks (related code that uses/is-used-by the changed code).
	if len(chunks) > 0 {
		sb.WriteString("\n\n## Related Code Context (from repository index)\n")
		sb.WriteString("The following code snippets are related to the changes above. Use them to understand the broader impact:\n\n")
		for i, chunk := range chunks {
			if i >= 5 { // limit context to top 5 chunks to control token usage
				break
			}
			sb.WriteString(fmt.Sprintf("### %s (L%d–L%d, relevance: %.2f)\n```%s\n%s\n```\n\n",
				chunk.FilePath, chunk.StartLine, chunk.EndLine,
				chunk.RelevanceScore, chunk.Language, chunk.Content,
			))
		}
	}

	// Add touched symbol information if available.
	if pack != nil && len(pack.TouchedSymbols) > 0 {
		sb.WriteString("\n## Touched Symbols\n")
		sb.WriteString("These symbols are affected by this change:\n\n")
		for _, sym := range pack.TouchedSymbols {
			if sym.FilePath != file.Filename {
				continue
			}
			sb.WriteString(fmt.Sprintf("- `%s` (%s, %s)", sym.QualifiedName, sym.Kind, sym.ChangeType))
			if len(sym.Callers) > 0 {
				sb.WriteString(fmt.Sprintf(" — called by: %s", strings.Join(sym.Callers, ", ")))
			}
			sb.WriteString("\n")
		}
	}

	// Add heuristic warnings if the engine flagged any.
	if pack != nil && len(pack.HeuristicWarnings) > 0 {
		sb.WriteString("\n## Engine Warnings\n")
		for _, w := range pack.HeuristicWarnings {
			sb.WriteString(fmt.Sprintf("- ⚠️ %s\n", w))
		}
	}

	return sb.String()
}

// ── Engine-Aware Prompts ────────────────────────────────────────────────────

const engineAwareSystemPrompt = `You are RTVortex, an expert AI code reviewer enhanced with deep repository context.

You have access to:
1. The code diff being reviewed
2. Related code context from the repository's semantic index (functions, classes that call or are called by the changed code)
3. Touched symbol analysis showing the impact radius of the change
4. Heuristic warnings from static analysis

Use ALL of this context to provide more accurate, context-aware reviews. Focus on:
- Breaking changes: does this change break callers/dependents shown in the context?
- Security vulnerabilities (injection, XSS, auth bypass, secrets)
- API contract violations: do related functions expect different behavior?
- Bugs and logic errors, especially considering how callers use this code
- Performance regressions in hot paths
- Missing error handling

For each issue found, respond with a JSON array of objects:
[
  {
    "line": <int>,
    "severity": "critical|high|medium|low|info",
    "category": "security|performance|bug|style|maintainability|testing|breaking_change",
    "title": "Brief issue title",
    "body": "Detailed explanation referencing the context",
    "suggestion": "Optional suggested fix code"
  }
]

If the code looks good, return an empty array [].`

func (p *Pipeline) buildSummary(pr *vcs.PullRequest, comments []*model.ReviewComment, duration time.Duration) string {
	critCount, highCount, medCount, lowCount := 0, 0, 0, 0
	for _, c := range comments {
		switch c.Severity {
		case model.SeverityCritical:
			critCount++
		case model.SeverityHigh:
			highCount++
		case model.SeverityMedium:
			medCount++
		default:
			lowCount++
		}
	}

	return fmt.Sprintf(`## 🔍 RTVortex Code Review

**PR:** %s (#%d)
**Duration:** %s
**Files reviewed:** analyzed
**Comments:** %d total

| Severity | Count |
|----------|-------|
| 🔴 Critical | %d |
| 🟠 High | %d |
| 🟡 Medium | %d |
| 🔵 Low/Info | %d |

---
*Powered by [RTVortex](https://github.com/AuralithAI/rtvortex) — AI Code Review Engine*`,
		pr.Title, pr.Number, duration.Round(time.Millisecond),
		len(comments), critCount, highCount, medCount, lowCount,
	)
}

// ── Prompts ─────────────────────────────────────────────────────────────────

const systemPrompt = `You are RTVortex, an expert AI code reviewer. Analyze the given code diff and provide actionable review comments.

For each issue found, respond with a JSON array of objects:
[
  {
    "line": <int>,
    "severity": "critical|high|medium|low|info",
    "category": "security|performance|bug|style|maintainability|testing",
    "title": "Brief issue title",
    "body": "Detailed explanation",
    "suggestion": "Optional suggested fix code"
  }
]

Focus on:
- Security vulnerabilities (injection, XSS, auth bypass, secrets)
- Bugs and logic errors
- Performance issues
- Best practices and code quality
- Missing error handling

If the code looks good, return an empty array [].`

func buildFileReviewPrompt(pr *vcs.PullRequest, file vcs.DiffFile) string {
	return fmt.Sprintf(`## Pull Request Context
- **Title:** %s
- **Author:** %s
- **Branch:** %s → %s

## File: %s (%s)

### Diff:
%s`,
		pr.Title, pr.Author, pr.SourceBranch, pr.TargetBranch,
		file.Filename, file.Status,
		file.Patch,
	)
}

// parseReviewResponse parses the LLM's JSON response into ReviewComment structs.
// It handles both raw JSON arrays and Markdown-fenced code blocks (```json ... ```).
func parseReviewResponse(content string, filePath string) []*model.ReviewComment {
	jsonStr := extractJSON(content)
	if jsonStr == "" {
		return nil
	}

	var items []struct {
		Line       int    `json:"line"`
		Severity   string `json:"severity"`
		Category   string `json:"category"`
		Title      string `json:"title"`
		Body       string `json:"body"`
		Suggestion string `json:"suggestion,omitempty"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &items); err != nil {
		slog.Warn("failed to parse LLM review response", "error", err, "file", filePath)
		return nil
	}

	comments := make([]*model.ReviewComment, 0, len(items))
	for _, item := range items {
		sev := model.Severity(item.Severity)
		switch sev {
		case model.SeverityCritical, model.SeverityHigh, model.SeverityMedium, model.SeverityLow, model.SeverityInfo:
			// valid
		default:
			sev = model.SeverityInfo
		}
		comments = append(comments, &model.ReviewComment{
			FilePath:   filePath,
			LineNumber: item.Line,
			Severity:   sev,
			Category:   item.Category,
			Title:      item.Title,
			Body:       item.Body,
			Suggestion: item.Suggestion,
		})
	}
	return comments
}

// extractJSON finds the first JSON array in the content, handling Markdown fences.
func extractJSON(content string) string {
	// Try to find ```json ... ``` block first.
	if idx := strings.Index(content, "```json"); idx != -1 {
		start := idx + len("```json")
		end := strings.Index(content[start:], "```")
		if end != -1 {
			return strings.TrimSpace(content[start : start+end])
		}
	}
	// Try ``` ... ``` (no language tag).
	if idx := strings.Index(content, "```"); idx != -1 {
		start := idx + len("```")
		// Skip optional language name on the same line.
		if nl := strings.IndexByte(content[start:], '\n'); nl != -1 {
			start += nl + 1
		}
		end := strings.Index(content[start:], "```")
		if end != -1 {
			return strings.TrimSpace(content[start : start+end])
		}
	}
	// Try raw JSON — find first '[' to last ']'.
	start := strings.IndexByte(content, '[')
	end := strings.LastIndexByte(content, ']')
	if start != -1 && end > start {
		return content[start : end+1]
	}
	return ""
}
