package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ── Executor interface ──────────────────────────────────────────────────────

// ExecutionResult captures the raw output from running a benchmark task
// through one of the agent pipelines.
type ExecutionResult struct {
	Comments   []GenComment
	DiffOutput string
	LLMCalls   int
	TokensUsed int
	Trace      []string
	Error      error
}

// Executor abstracts the agent execution backends so the benchmark runner
// stays decoupled from the review pipeline and swarm infrastructure.
type Executor interface {
	// ExecuteReview runs a single-agent review pipeline for the given task
	// and returns the generated comments and metrics.
	ExecuteReview(ctx context.Context, task *Task) (*ExecutionResult, error)

	// ExecuteSwarm submits the task to the swarm pipeline, waits for
	// completion (up to the context deadline), and returns the result.
	ExecuteSwarm(ctx context.Context, task *Task) (*ExecutionResult, error)
}

// ── Runner ──────────────────────────────────────────────────────────────────

// Runner executes benchmark suites.  It is the core orchestrator that drives
// tasks through the swarm or single-agent pipelines and collects metrics.
type Runner struct {
	mu        sync.Mutex
	tasks     map[string]*Task
	runs      map[uuid.UUID]*Run
	ratings   map[Mode]*ELORating
	taskOrder []string

	executor Executor
	logger   *slog.Logger
}

// NewRunner creates a benchmark runner.  If executor is nil, task execution
// will fail at runtime with a clear error — it must be wired before use.
func NewRunner(executor Executor, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		tasks: make(map[string]*Task),
		runs:  make(map[uuid.UUID]*Run),
		ratings: map[Mode]*ELORating{
			ModeSwarm:       {Mode: ModeSwarm, Rating: 1500},
			ModeSingleAgent: {Mode: ModeSingleAgent, Rating: 1500},
		},
		executor: executor,
		logger:   logger,
	}
}

// LoadTasks loads benchmark tasks from JSON bytes.
func (r *Runner) LoadTasks(data []byte) error {
	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range tasks {
		t := &tasks[i]
		if t.ID == "" {
			t.ID = uuid.New().String()[:8]
		}
		r.tasks[t.ID] = t
		r.taskOrder = append(r.taskOrder, t.ID)
	}
	return nil
}

// ListTasks returns all loaded benchmark tasks.
func (r *Runner) ListTasks() []Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Task, 0, len(r.tasks))
	for _, id := range r.taskOrder {
		if t, ok := r.tasks[id]; ok {
			out = append(out, *t)
		}
	}
	return out
}

// ListRuns returns all benchmark runs, newest first.
func (r *Runner) ListRuns() []Run {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Run, 0, len(r.runs))
	for _, run := range r.runs {
		out = append(out, *run)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

// GetRun returns a specific benchmark run by ID.
func (r *Runner) GetRun(id uuid.UUID) (*Run, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	return run, ok
}

// GetRatings returns current ELO ratings.
func (r *Runner) GetRatings() map[Mode]*ELORating {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[Mode]*ELORating, len(r.ratings))
	for k, v := range r.ratings {
		cp := *v
		out[k] = &cp
	}
	return out
}

// StartRun creates and starts a benchmark run.  It filters tasks by the
// request criteria and immediately returns the run ID.  Execution is
// synchronous for now (the handler wraps this in a goroutine).
func (r *Runner) StartRun(req RunRequest) (*Run, error) {
	r.mu.Lock()

	run := &Run{
		ID:        uuid.New(),
		Name:      req.Name,
		Mode:      req.Mode,
		Status:    "running",
		StartedAt: time.Now(),
	}
	r.runs[run.ID] = run

	// Collect matching tasks.
	var selectedTasks []*Task
	if len(req.TaskIDs) > 0 {
		for _, tid := range req.TaskIDs {
			if t, ok := r.tasks[tid]; ok {
				selectedTasks = append(selectedTasks, t)
			}
		}
	} else {
		for _, id := range r.taskOrder {
			t := r.tasks[id]
			if req.Category != "" && t.Category != req.Category {
				continue
			}
			if len(req.Tags) > 0 && !hasAnyTag(t.Tags, req.Tags) {
				continue
			}
			selectedTasks = append(selectedTasks, t)
		}
	}

	r.mu.Unlock()

	// Execute each task.
	for _, task := range selectedTasks {
		modes := []Mode{req.Mode}
		if req.Mode == ModeBoth {
			modes = []Mode{ModeSwarm, ModeSingleAgent}
		}
		for _, mode := range modes {
			result := r.executeTask(task, mode)
			r.mu.Lock()
			run.Results = append(run.Results, result)
			r.mu.Unlock()
		}
	}

	// Compute summary.
	r.mu.Lock()
	run.Summary = computeSummary(run.Results)
	now := time.Now()
	run.EndedAt = &now
	run.Status = "completed"

	// Update ELO if A/B.
	if req.Mode == ModeBoth {
		r.updateELO(run)
	}
	r.mu.Unlock()

	return run, nil
}

// ── Internal helpers ────────────────────────────────────────────────────────

// taskTimeout is the maximum allowed duration for a single benchmark task.
const taskTimeout = 10 * time.Minute

func (r *Runner) executeTask(task *Task, mode Mode) RunResult {
	start := time.Now()
	result := RunResult{
		TaskID:    task.ID,
		TaskName:  task.Name,
		Mode:      mode,
		StartedAt: start,
	}

	if r.executor == nil {
		result.Status = "error"
		result.Error = "benchmark executor not configured"
		result.EndedAt = time.Now()
		result.LatencyMS = time.Since(start).Milliseconds()
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), taskTimeout)
	defer cancel()

	var execResult *ExecutionResult
	var execErr error

	switch mode {
	case ModeSingleAgent:
		execResult, execErr = r.executor.ExecuteReview(ctx, task)
	case ModeSwarm:
		execResult, execErr = r.executor.ExecuteSwarm(ctx, task)
	default:
		result.Status = "error"
		result.Error = fmt.Sprintf("unsupported benchmark mode: %s", mode)
		result.EndedAt = time.Now()
		result.LatencyMS = time.Since(start).Milliseconds()
		return result
	}

	result.EndedAt = time.Now()
	result.LatencyMS = time.Since(start).Milliseconds()

	if execErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.Status = "timeout"
		} else {
			result.Status = "error"
		}
		result.Error = execErr.Error()
		r.logger.Error("benchmark task execution failed",
			"task_id", task.ID, "mode", mode, "error", execErr,
			"latency_ms", result.LatencyMS)
	} else {
		result.Status = "passed"
		result.Comments = execResult.Comments
		result.DiffOutput = execResult.DiffOutput
		result.LLMCalls = execResult.LLMCalls
		result.TokensUsed = execResult.TokensUsed
		result.Trace = execResult.Trace
	}

	// Record Prometheus metrics for this task execution.
	BenchTasksTotal.WithLabelValues(string(mode), result.Status).Inc()
	BenchTaskDuration.WithLabelValues(string(mode), task.Category).Observe(float64(result.LatencyMS) / 1000.0)
	BenchLLMCallsTotal.WithLabelValues(string(mode)).Add(float64(result.LLMCalls))

	// Score against expected results if ground truth is available.
	if task.Expected != nil {
		result.Score = scoreResult(&result, task.Expected)
		BenchScoreHistogram.WithLabelValues(string(mode)).Observe(result.Score.Composite)
	}

	r.logger.Info("benchmark task completed",
		"task_id", task.ID, "mode", mode, "status", result.Status,
		"latency_ms", result.LatencyMS, "llm_calls", result.LLMCalls,
		"comments", len(result.Comments))

	return result
}

func scoreResult(result *RunResult, expected *ExpectedResult) *QualityScore {
	score := &QualityScore{}

	// Precision / Recall against expected comments
	if len(expected.Comments) > 0 {
		matched := 0
		for _, exp := range expected.Comments {
			for _, gen := range result.Comments {
				if gen.File == exp.File && (exp.Line == 0 || gen.Line == exp.Line) {
					matched++
					break
				}
			}
		}
		if len(result.Comments) > 0 {
			score.Precision = float64(matched) / float64(len(result.Comments))
		}
		score.Recall = float64(matched) / float64(len(expected.Comments))
		score.Correctness = (score.Precision + score.Recall) / 2.0
	}

	// Speed score: sub-10s = 1.0, scales linearly to 0 at 300s
	latencySec := float64(result.LatencyMS) / 1000.0
	score.Speed = math.Max(0, 1.0-latencySec/300.0)

	// Efficiency: fewer LLM calls = better (10 calls = 0, 0 calls = 1.0)
	score.Efficiency = math.Max(0, 1.0-float64(result.LLMCalls)/10.0)

	// Composite: weighted average
	score.Composite = 0.40*score.Correctness +
		0.20*score.Precision +
		0.15*score.Speed +
		0.15*score.Efficiency +
		0.10*score.HumanQuality

	return score
}

func computeSummary(results []RunResult) *RunSummary {
	if len(results) == 0 {
		return &RunSummary{}
	}

	s := &RunSummary{
		TotalTasks: len(results),
		ByMode:     make(map[Mode]*ModeStats),
	}

	var totalLatency, totalLLM, totalTokens, totalScore float64
	var latencies []float64

	for _, r := range results {
		if r.Status == "passed" {
			s.Passed++
		} else {
			s.Failed++
		}
		totalLatency += float64(r.LatencyMS)
		totalLLM += float64(r.LLMCalls)
		totalTokens += float64(r.TokensUsed)
		latencies = append(latencies, float64(r.LatencyMS))

		if r.Score != nil {
			totalScore += r.Score.Composite
		}

		// Per-mode stats
		ms, ok := s.ByMode[r.Mode]
		if !ok {
			ms = &ModeStats{Mode: r.Mode}
			s.ByMode[r.Mode] = ms
		}
		ms.TotalTasks++
		if r.Status == "passed" {
			ms.Passed++
		} else {
			ms.Failed++
		}
		ms.AvgLatencyMS += float64(r.LatencyMS)
		ms.AvgLLMCalls += float64(r.LLMCalls)
		if r.Score != nil {
			ms.AvgScore += r.Score.Composite
		}
	}

	n := float64(len(results))
	s.AvgLatencyMS = totalLatency / n
	s.AvgLLMCalls = totalLLM / n
	s.AvgTokensUsed = totalTokens / n
	s.AvgScore = totalScore / n

	// Percentile latencies
	sort.Float64s(latencies)
	s.P50LatencyMS = percentile(latencies, 0.50)
	s.P90LatencyMS = percentile(latencies, 0.90)
	s.P99LatencyMS = percentile(latencies, 0.99)

	// Finalize per-mode averages
	for _, ms := range s.ByMode {
		mn := float64(ms.TotalTasks)
		if mn > 0 {
			ms.AvgLatencyMS /= mn
			ms.AvgLLMCalls /= mn
			ms.AvgScore /= mn
		}
	}

	return s
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := p * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi || hi >= len(sorted) {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

func (r *Runner) updateELO(run *Run) {
	// Collect scores per mode
	swarmScores := make(map[string]float64)
	singleScores := make(map[string]float64)

	for _, res := range run.Results {
		if res.Score == nil {
			continue
		}
		switch res.Mode {
		case ModeSwarm:
			swarmScores[res.TaskID] = res.Score.Composite
		case ModeSingleAgent:
			singleScores[res.TaskID] = res.Score.Composite
		}
	}

	// Head-to-head comparison
	K := 32.0
	for taskID, sw := range swarmScores {
		sa, ok := singleScores[taskID]
		if !ok {
			continue
		}

		rSwarm := r.ratings[ModeSwarm]
		rSingle := r.ratings[ModeSingleAgent]

		eSwarm := 1.0 / (1.0 + math.Pow(10, (rSingle.Rating-rSwarm.Rating)/400.0))
		eSingle := 1.0 - eSwarm

		var sSwarm, sSingle float64
		switch {
		case sw > sa+0.05: // swarm wins
			sSwarm, sSingle = 1.0, 0.0
			rSwarm.Wins++
			rSingle.Losses++
		case sa > sw+0.05: // single wins
			sSwarm, sSingle = 0.0, 1.0
			rSwarm.Losses++
			rSingle.Wins++
		default: // draw
			sSwarm, sSingle = 0.5, 0.5
			rSwarm.Draws++
			rSingle.Draws++
		}

		rSwarm.Rating += K * (sSwarm - eSwarm)
		rSingle.Rating += K * (sSingle - eSingle)
		rSwarm.LastUpdate = time.Now()
		rSingle.LastUpdate = time.Now()
	}
}

func hasAnyTag(tags, want []string) bool {
	m := make(map[string]bool, len(tags))
	for _, t := range tags {
		m[t] = true
	}
	for _, w := range want {
		if m[w] {
			return true
		}
	}
	return false
}
