package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// testExecutor is a deterministic executor for unit tests that returns
// predictable results without requiring real infrastructure.
type testExecutor struct{}

func (e *testExecutor) ExecuteReview(_ context.Context, task *Task) (*ExecutionResult, error) {
	result := &ExecutionResult{
		Trace: []string{fmt.Sprintf("test-review: task=%s", task.ID)},
	}
	for _, f := range task.Files {
		result.Comments = append(result.Comments, GenComment{
			File: f.Path, Line: 1, Body: "test finding", Severity: "info",
		})
	}
	if task.Expected != nil {
		for _, exp := range task.Expected.Comments {
			result.Comments = append(result.Comments, GenComment{
				File: exp.File, Line: exp.Line, Body: exp.Pattern, Severity: exp.Severity,
			})
		}
	}
	return result, nil
}

func (e *testExecutor) ExecuteSwarm(_ context.Context, task *Task) (*ExecutionResult, error) {
	result := &ExecutionResult{
		LLMCalls:   2,
		TokensUsed: 500,
		Trace:      []string{fmt.Sprintf("test-swarm: task=%s", task.ID)},
	}
	if task.Expected != nil {
		for _, exp := range task.Expected.Comments {
			result.Comments = append(result.Comments, GenComment{
				File: exp.File, Line: exp.Line, Body: exp.Pattern, Severity: exp.Severity,
			})
		}
	}
	return result, nil
}

func newTestRunner() *Runner {
	return NewRunner(&testExecutor{}, nil)
}

func TestRunnerLoadTasks(t *testing.T) {
	r := newTestRunner()

	tasks := []Task{
		{
			ID:          "test-001",
			Name:        "SQL Injection Test",
			Category:    "real_pr",
			Complexity:  "simple",
			Description: "Test for SQL injection detection",
		},
		{
			ID:          "test-002",
			Name:        "Race Condition Test",
			Category:    "real_pr",
			Complexity:  "medium",
			Description: "Test for race condition detection",
		},
	}

	data, err := json.Marshal(tasks)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if err := r.LoadTasks(data); err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}

	loaded := r.ListTasks()
	if len(loaded) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(loaded))
	}
	if loaded[0].ID != "test-001" {
		t.Errorf("expected first task id test-001, got %s", loaded[0].ID)
	}
	if loaded[1].Name != "Race Condition Test" {
		t.Errorf("expected second task name 'Race Condition Test', got %s", loaded[1].Name)
	}
}

func TestRunnerStartRun(t *testing.T) {
	r := newTestRunner()

	tasks := []Task{
		{
			ID:       "t1",
			Name:     "Task 1",
			Category: "real_pr",
			Expected: &ExpectedResult{
				Comments: []ExpectedComment{
					{File: "main.go", Severity: "critical", Category: "security", Pattern: "injection"},
				},
			},
		},
		{
			ID:       "t2",
			Name:     "Task 2",
			Category: "synthetic",
		},
	}

	data, _ := json.Marshal(tasks)
	_ = r.LoadTasks(data)

	// Run all tasks
	run, err := r.StartRun(RunRequest{
		Name: "test-run",
		Mode: ModeBoth,
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	if run.Status != "completed" {
		t.Errorf("expected completed, got %s", run.Status)
	}

	// Both mode: should have 4 results (2 tasks × 2 modes)
	if len(run.Results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(run.Results))
	}

	if run.Summary == nil {
		t.Fatal("expected summary to be non-nil")
	}

	if run.Summary.TotalTasks != 4 {
		t.Errorf("expected 4 total tasks in summary, got %d", run.Summary.TotalTasks)
	}
}

func TestRunnerCategoryFilter(t *testing.T) {
	r := newTestRunner()

	tasks := []Task{
		{ID: "t1", Name: "Task 1", Category: "real_pr"},
		{ID: "t2", Name: "Task 2", Category: "synthetic"},
		{ID: "t3", Name: "Task 3", Category: "real_pr"},
	}

	data, _ := json.Marshal(tasks)
	_ = r.LoadTasks(data)

	run, err := r.StartRun(RunRequest{
		Name:     "filtered-run",
		Mode:     ModeSwarm,
		Category: "synthetic",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	if len(run.Results) != 1 {
		t.Fatalf("expected 1 result (synthetic only), got %d", len(run.Results))
	}

	if run.Results[0].TaskID != "t2" {
		t.Errorf("expected task t2, got %s", run.Results[0].TaskID)
	}
}

func TestPercentile(t *testing.T) {
	sorted := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}

	p50 := percentile(sorted, 0.50)
	if p50 < 50 || p50 > 60 {
		t.Errorf("expected p50 around 55, got %f", p50)
	}

	p90 := percentile(sorted, 0.90)
	if p90 < 90 || p90 > 100 {
		t.Errorf("expected p90 around 91, got %f", p90)
	}

	p99 := percentile(sorted, 0.99)
	if p99 < 99 {
		t.Errorf("expected p99 near 100, got %f", p99)
	}
}

func TestELOUpdate(t *testing.T) {
	r := newTestRunner()

	// Both start at 1500
	ratings := r.GetRatings()
	if ratings[ModeSwarm].Rating != 1500 {
		t.Errorf("expected swarm rating 1500, got %f", ratings[ModeSwarm].Rating)
	}

	// Create a run where swarm wins
	run := &Run{
		Results: []RunResult{
			{TaskID: "t1", Mode: ModeSwarm, Score: &QualityScore{Composite: 0.9}},
			{TaskID: "t1", Mode: ModeSingleAgent, Score: &QualityScore{Composite: 0.3}},
		},
	}

	r.mu.Lock()
	r.updateELO(run)
	r.mu.Unlock()

	ratings = r.GetRatings()
	if ratings[ModeSwarm].Rating <= 1500 {
		t.Errorf("expected swarm rating > 1500 after win, got %f", ratings[ModeSwarm].Rating)
	}
	if ratings[ModeSingleAgent].Rating >= 1500 {
		t.Errorf("expected single agent rating < 1500 after loss, got %f", ratings[ModeSingleAgent].Rating)
	}
	if ratings[ModeSwarm].Wins != 1 {
		t.Errorf("expected swarm wins=1, got %d", ratings[ModeSwarm].Wins)
	}
}

func TestScoreResult(t *testing.T) {
	result := &RunResult{
		LatencyMS: 5000, // 5 seconds
		LLMCalls:  3,
		Comments: []GenComment{
			{File: "main.go", Line: 10, Body: "SQL injection here"},
			{File: "main.go", Line: 20, Body: "Another issue"},
		},
	}

	expected := &ExpectedResult{
		Comments: []ExpectedComment{
			{File: "main.go", Line: 10, Severity: "critical", Category: "security"},
			{File: "other.go", Line: 5, Severity: "warning", Category: "logic"},
		},
	}

	score := scoreResult(result, expected)

	// 1/2 expected matched (main.go line 10)
	if score.Recall != 0.5 {
		t.Errorf("expected recall 0.5, got %f", score.Recall)
	}

	// 1/2 generated matched expected
	if score.Precision != 0.5 {
		t.Errorf("expected precision 0.5, got %f", score.Precision)
	}

	// Speed: 5s → 1.0 - 5/300 ≈ 0.983
	if score.Speed < 0.98 || score.Speed > 1.0 {
		t.Errorf("expected speed ~0.98, got %f", score.Speed)
	}

	// Efficiency: 3 calls → 1.0 - 3/10 = 0.7
	if score.Efficiency != 0.7 {
		t.Errorf("expected efficiency 0.7, got %f", score.Efficiency)
	}

	// Composite should be > 0
	if score.Composite <= 0 {
		t.Errorf("expected positive composite score, got %f", score.Composite)
	}
}
