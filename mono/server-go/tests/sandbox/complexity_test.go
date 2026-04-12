package sandbox_test

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/sandbox"
	"github.com/google/uuid"
)

// ── ExtractBuildSignals ─────────────────────────────────────────────────────

func TestExtractBuildSignals_MinimalPlan(t *testing.T) {
	plan := &sandbox.BuildPlan{
		ID:          uuid.New(),
		BuildSystem: "go",
	}
	s := sandbox.ExtractBuildSignals(plan, nil)

	if s.BuildSystem != "go" {
		t.Errorf("BuildSystem = %q, want %q", s.BuildSystem, "go")
	}
	if s.FileCount != 0 {
		t.Errorf("FileCount = %d, want 0", s.FileCount)
	}
	if s.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 (nil result)", s.ExitCode)
	}
}

func TestExtractBuildSignals_FullPlan(t *testing.T) {
	plan := &sandbox.BuildPlan{
		ID:          uuid.New(),
		BuildSystem: "gradle",
		SecretRefs:  []string{"DB_URL", "API_KEY"},
		PreCommands: []string{"chmod +x gradlew"},
		WorkspaceFS: map[string]string{
			"src/main/App.java":     "class App {}",
			"src/test/AppTest.java": "class AppTest {}",
			"build.gradle":          "apply plugin: 'java'",
		},
		Cache: &sandbox.CacheConfig{VolumeName: "test"},
	}
	result := &sandbox.BuildResult{
		ExitCode: 1,
		Duration: 45 * time.Second,
	}

	s := sandbox.ExtractBuildSignals(plan, result)

	if s.SecretCount != 2 {
		t.Errorf("SecretCount = %d, want 2", s.SecretCount)
	}
	if !s.HasPreCommands {
		t.Error("HasPreCommands should be true")
	}
	if s.FileCount != 3 {
		t.Errorf("FileCount = %d, want 3", s.FileCount)
	}
	if s.LanguageCount != 1 {
		t.Errorf("LanguageCount = %d, want 1 (java)", s.LanguageCount)
	}
	if !s.CacheHit {
		t.Error("CacheHit should be true when Cache is non-nil")
	}
	if s.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", s.ExitCode)
	}
	if s.DurationSec != 45 {
		t.Errorf("DurationSec = %v, want 45", s.DurationSec)
	}
}

// ── ScoreBuildComplexity ────────────────────────────────────────────────────

func TestScoreBuildComplexity_ZeroSignals(t *testing.T) {
	score := sandbox.ScoreBuildComplexity(sandbox.BuildSignals{})

	if score < 0 || score > 1 {
		t.Errorf("score = %v, out of [0,1] range", score)
	}
}

func TestScoreBuildComplexity_HighComplexity(t *testing.T) {
	s := sandbox.BuildSignals{
		BuildSystem:    "gradle",
		FileCount:      500,
		WorkspaceBytes: 50 * 1024 * 1024,
		SecretCount:    5,
		HasPreCommands: true,
		LanguageCount:  4,
		DurationSec:    300,
		ExitCode:       1,
		RetryCount:     2,
		CacheHit:       false,
	}
	score := sandbox.ScoreBuildComplexity(s)

	if score < 0.6 {
		t.Errorf("score = %v, expected >= 0.6 for high-complexity signals", score)
	}
}

func TestScoreBuildComplexity_LowComplexity(t *testing.T) {
	s := sandbox.BuildSignals{
		BuildSystem:    "go",
		FileCount:      3,
		WorkspaceBytes: 1024,
		LanguageCount:  1,
		DurationSec:    2,
		CacheHit:       true,
	}
	score := sandbox.ScoreBuildComplexity(s)

	if score > 0.3 {
		t.Errorf("score = %v, expected <= 0.3 for low-complexity signals", score)
	}
}

func TestScoreBuildComplexity_Bounded(t *testing.T) {
	extremes := []sandbox.BuildSignals{
		{FileCount: 100000, WorkspaceBytes: 1 << 30, SecretCount: 100, LanguageCount: 20, DurationSec: 10000, RetryCount: 10, ExitCode: 127, HasPreCommands: true},
		{},
	}
	for _, s := range extremes {
		score := sandbox.ScoreBuildComplexity(s)
		if score < 0 || score > 1 {
			t.Errorf("score = %v, out of [0,1] range for signals %+v", score, s)
		}
	}
}

// ── ClassifyBuildComplexity ─────────────────────────────────────────────────

func TestClassifyBuildComplexity_AllLabels(t *testing.T) {
	cases := []struct {
		score float64
		want  sandbox.BuildComplexityLabel
	}{
		{0.05, sandbox.BuildComplexityTrivial},
		{0.09, sandbox.BuildComplexityTrivial},
		{0.10, sandbox.BuildComplexitySimple},
		{0.20, sandbox.BuildComplexitySimple},
		{0.30, sandbox.BuildComplexityModerate},
		{0.45, sandbox.BuildComplexityModerate},
		{0.55, sandbox.BuildComplexityHigh},
		{0.75, sandbox.BuildComplexityHigh},
		{0.80, sandbox.BuildComplexityCritical},
		{0.95, sandbox.BuildComplexityCritical},
	}
	for _, tc := range cases {
		got := sandbox.ClassifyBuildComplexity(tc.score)
		if got != tc.want {
			t.Errorf("ClassifyBuildComplexity(%v) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

// ── ComputeBuildComplexity ──────────────────────────────────────────────────

func TestComputeBuildComplexity_NilResult(t *testing.T) {
	plan := &sandbox.BuildPlan{
		ID:          uuid.New(),
		BuildSystem: "python",
	}
	c := sandbox.ComputeBuildComplexity(plan, nil)

	if c == nil {
		t.Fatal("expected non-nil complexity")
	}
	if c.Score < 0 || c.Score > 1 {
		t.Errorf("score = %v out of range", c.Score)
	}
	if c.Label == "" {
		t.Error("label should not be empty")
	}
}

func TestComputeBuildComplexity_ScoreRoundedTo3Decimals(t *testing.T) {
	plan := &sandbox.BuildPlan{
		ID:          uuid.New(),
		BuildSystem: "go",
		WorkspaceFS: map[string]string{"main.go": "package main"},
	}
	result := &sandbox.BuildResult{ExitCode: 0, Duration: 5 * time.Second}

	c := sandbox.ComputeBuildComplexity(plan, result)

	rounded := math.Round(c.Score*1000) / 1000
	if c.Score != rounded {
		t.Errorf("score %v not rounded to 3 decimals", c.Score)
	}
}

func TestComputeBuildComplexity_IncludesAllFields(t *testing.T) {
	plan := &sandbox.BuildPlan{
		ID:          uuid.New(),
		BuildSystem: "gradle",
		PreCommands: []string{"chmod +x gradlew"},
		SecretRefs:  []string{"TOKEN"},
		WorkspaceFS: map[string]string{
			"App.java": "class App {}",
		},
	}
	result := &sandbox.BuildResult{ExitCode: 0, Duration: 30 * time.Second}

	c := sandbox.ComputeBuildComplexity(plan, result)

	if c.DurationBucket == "" {
		t.Error("DurationBucket should not be empty")
	}
	if c.ResourceHints.TimeoutSec == 0 {
		t.Error("ResourceHints.TimeoutSec should not be 0")
	}
	if c.ResourceHints.MemoryLimit == "" {
		t.Error("ResourceHints.MemoryLimit should not be empty")
	}
	if c.ResourceHints.CPULimit == "" {
		t.Error("ResourceHints.CPULimit should not be empty")
	}
}

// ── RecommendResources ──────────────────────────────────────────────────────

func TestRecommendResources_Defaults(t *testing.T) {
	s := sandbox.BuildSignals{BuildSystem: "go", DurationSec: 10}
	h := sandbox.RecommendResources(s)

	if h.TimeoutSec != int(sandbox.DefaultTimeout.Seconds()) {
		t.Errorf("TimeoutSec = %d, want %d", h.TimeoutSec, int(sandbox.DefaultTimeout.Seconds()))
	}
	if h.MemoryLimit != sandbox.DefaultMemoryLimit {
		t.Errorf("MemoryLimit = %q, want %q", h.MemoryLimit, sandbox.DefaultMemoryLimit)
	}
	if h.CPULimit != sandbox.DefaultCPULimit {
		t.Errorf("CPULimit = %q, want %q", h.CPULimit, sandbox.DefaultCPULimit)
	}
}

func TestRecommendResources_LongDuration(t *testing.T) {
	s := sandbox.BuildSignals{DurationSec: 350}
	h := sandbox.RecommendResources(s)

	if h.TimeoutSec != 900 {
		t.Errorf("TimeoutSec = %d, want 900 for duration > 300s", h.TimeoutSec)
	}
}

func TestRecommendResources_MediumDuration(t *testing.T) {
	s := sandbox.BuildSignals{DurationSec: 150}
	h := sandbox.RecommendResources(s)

	if h.TimeoutSec != 600 {
		t.Errorf("TimeoutSec = %d, want 600 for duration 120-300s", h.TimeoutSec)
	}
}

func TestRecommendResources_LargeWorkspace(t *testing.T) {
	s := sandbox.BuildSignals{
		FileCount:      200,
		WorkspaceBytes: 100 * 1024 * 1024,
	}
	h := sandbox.RecommendResources(s)

	if h.MemoryLimit != "4g" {
		t.Errorf("MemoryLimit = %q, want 4g for large workspace", h.MemoryLimit)
	}
	if h.CPULimit != "4" {
		t.Errorf("CPULimit = %q, want 4 for large workspace", h.CPULimit)
	}
}

func TestRecommendResources_GradleBoost(t *testing.T) {
	s := sandbox.BuildSignals{BuildSystem: "gradle"}
	h := sandbox.RecommendResources(s)

	if h.MemoryLimit != "3g" {
		t.Errorf("MemoryLimit = %q, want 3g for gradle", h.MemoryLimit)
	}
}

func TestRecommendResources_CMakeBoost(t *testing.T) {
	s := sandbox.BuildSignals{BuildSystem: "cmake"}
	h := sandbox.RecommendResources(s)

	if h.CPULimit != "3" {
		t.Errorf("CPULimit = %q, want 3 for cmake", h.CPULimit)
	}
}

func TestRecommendResources_RetryWithFailureIncreasesTimeout(t *testing.T) {
	s := sandbox.BuildSignals{
		BuildSystem: "go",
		DurationSec: 10,
		RetryCount:  1,
		ExitCode:    1,
	}
	h := sandbox.RecommendResources(s)

	expected := int(float64(int(sandbox.DefaultTimeout.Seconds())) * 1.5)
	if h.TimeoutSec != expected {
		t.Errorf("TimeoutSec = %d, want %d for retry with failure", h.TimeoutSec, expected)
	}
}

// ── BuildComplexitySummary ──────────────────────────────────────────────────

func TestBuildComplexitySummary_Nil(t *testing.T) {
	s := sandbox.BuildComplexitySummary(nil)
	if s != "complexity=unknown" {
		t.Errorf("got %q, want %q", s, "complexity=unknown")
	}
}

func TestBuildComplexitySummary_Format(t *testing.T) {
	c := &sandbox.BuildComplexity{
		Score:          0.450,
		Label:          sandbox.BuildComplexityModerate,
		DurationBucket: "normal",
		ResourceHints: sandbox.ResourceHints{
			MemoryLimit: "3g",
			CPULimit:    "2",
		},
	}
	s := sandbox.BuildComplexitySummary(c)

	if s == "" {
		t.Error("expected non-empty summary")
	}
	for _, want := range []string{"moderate", "0.450", "normal", "3g", "2"} {
		if !containsStr(s, want) {
			t.Errorf("summary %q missing %q", s, want)
		}
	}
}

// ── ComputeHistoricalStats ──────────────────────────────────────────────────

func TestComputeHistoricalStats_Empty(t *testing.T) {
	stats := sandbox.ComputeHistoricalStats(nil)

	if stats.TotalBuilds != 0 {
		t.Errorf("TotalBuilds = %d, want 0", stats.TotalBuilds)
	}
	if stats.SuccessRate != 0 {
		t.Errorf("SuccessRate = %v, want 0", stats.SuccessRate)
	}
}

func TestComputeHistoricalStats_AllSuccess(t *testing.T) {
	d1, d2 := 5000, 10000
	records := []*sandbox.BuildRecord{
		{Status: "success", DurationMS: &d1},
		{Status: "success", DurationMS: &d2},
	}
	stats := sandbox.ComputeHistoricalStats(records)

	if stats.TotalBuilds != 2 {
		t.Errorf("TotalBuilds = %d, want 2", stats.TotalBuilds)
	}
	if stats.SuccessRate != 1.0 {
		t.Errorf("SuccessRate = %v, want 1.0", stats.SuccessRate)
	}
	if stats.AvgDurationSec != 7.5 {
		t.Errorf("AvgDurationSec = %v, want 7.5", stats.AvgDurationSec)
	}
}

func TestComputeHistoricalStats_MixedResults(t *testing.T) {
	d1, d2, d3, d4 := 2000, 4000, 8000, 20000
	records := []*sandbox.BuildRecord{
		{Status: "success", DurationMS: &d1},
		{Status: "failed", DurationMS: &d2, RetryCount: 1},
		{Status: "success", DurationMS: &d3},
		{Status: "failed", DurationMS: &d4, RetryCount: 2},
	}
	stats := sandbox.ComputeHistoricalStats(records)

	if stats.TotalBuilds != 4 {
		t.Errorf("TotalBuilds = %d, want 4", stats.TotalBuilds)
	}
	if stats.SuccessRate != 0.5 {
		t.Errorf("SuccessRate = %v, want 0.5", stats.SuccessRate)
	}
	if stats.AvgRetries != 0.75 {
		t.Errorf("AvgRetries = %v, want 0.75", stats.AvgRetries)
	}
	if stats.P95DurationSec < 15 {
		t.Errorf("P95DurationSec = %v, expected >= 15", stats.P95DurationSec)
	}
}

func TestComputeHistoricalStats_NoDurations(t *testing.T) {
	records := []*sandbox.BuildRecord{
		{Status: "success"},
		{Status: "failed"},
	}
	stats := sandbox.ComputeHistoricalStats(records)

	if stats.AvgDurationSec != 0 {
		t.Errorf("AvgDurationSec = %v, want 0 when no durations", stats.AvgDurationSec)
	}
	if stats.P95DurationSec != 0 {
		t.Errorf("P95DurationSec = %v, want 0 when no durations", stats.P95DurationSec)
	}
}

// ── RefineResourceHints ─────────────────────────────────────────────────────

func TestRefineResourceHints_NilHistory(t *testing.T) {
	base := sandbox.ResourceHints{TimeoutSec: 600, MemoryLimit: "2g", CPULimit: "2"}
	refined := sandbox.RefineResourceHints(base, nil)

	if refined != base {
		t.Errorf("expected unchanged hints with nil history, got %+v", refined)
	}
}

func TestRefineResourceHints_EmptyHistory(t *testing.T) {
	base := sandbox.ResourceHints{TimeoutSec: 600, MemoryLimit: "2g", CPULimit: "2"}
	refined := sandbox.RefineResourceHints(base, &sandbox.HistoricalBuildStats{})

	if refined != base {
		t.Errorf("expected unchanged hints with empty history, got %+v", refined)
	}
}

func TestRefineResourceHints_IncreasesTimeout(t *testing.T) {
	base := sandbox.ResourceHints{TimeoutSec: 600, MemoryLimit: "2g", CPULimit: "2"}
	history := &sandbox.HistoricalBuildStats{
		TotalBuilds:    10,
		P95DurationSec: 700,
	}
	refined := sandbox.RefineResourceHints(base, history)

	expected := int(700 * 1.3)
	if refined.TimeoutSec != expected {
		t.Errorf("TimeoutSec = %d, want %d", refined.TimeoutSec, expected)
	}
}

func TestRefineResourceHints_CapsTimeoutAt1200(t *testing.T) {
	base := sandbox.ResourceHints{TimeoutSec: 600, MemoryLimit: "2g", CPULimit: "2"}
	history := &sandbox.HistoricalBuildStats{
		TotalBuilds:    5,
		P95DurationSec: 2000,
	}
	refined := sandbox.RefineResourceHints(base, history)

	if refined.TimeoutSec != 1200 {
		t.Errorf("TimeoutSec = %d, want 1200 (cap)", refined.TimeoutSec)
	}
}

func TestRefineResourceHints_BumpsMemoryOnLowSuccessRate(t *testing.T) {
	base := sandbox.ResourceHints{TimeoutSec: 600, MemoryLimit: "2g", CPULimit: "2"}
	history := &sandbox.HistoricalBuildStats{
		TotalBuilds: 5,
		SuccessRate: 0.3,
	}
	refined := sandbox.RefineResourceHints(base, history)

	if refined.MemoryLimit != "3g" {
		t.Errorf("MemoryLimit = %q, want 3g with low success rate", refined.MemoryLimit)
	}
}

func TestRefineResourceHints_BumpsMemoryFrom3gTo4g(t *testing.T) {
	base := sandbox.ResourceHints{TimeoutSec: 600, MemoryLimit: "3g", CPULimit: "2"}
	history := &sandbox.HistoricalBuildStats{
		TotalBuilds: 10,
		SuccessRate: 0.2,
	}
	refined := sandbox.RefineResourceHints(base, history)

	if refined.MemoryLimit != "4g" {
		t.Errorf("MemoryLimit = %q, want 4g", refined.MemoryLimit)
	}
}

func TestRefineResourceHints_NoMemoryBumpWithHighSuccessRate(t *testing.T) {
	base := sandbox.ResourceHints{TimeoutSec: 600, MemoryLimit: "2g", CPULimit: "2"}
	history := &sandbox.HistoricalBuildStats{
		TotalBuilds: 10,
		SuccessRate: 0.8,
	}
	refined := sandbox.RefineResourceHints(base, history)

	if refined.MemoryLimit != "2g" {
		t.Errorf("MemoryLimit = %q, want 2g (unchanged)", refined.MemoryLimit)
	}
}

// ── DurationBucket (via ComputeBuildComplexity) ─────────────────────────────

func TestDurationBuckets(t *testing.T) {
	cases := []struct {
		dur    time.Duration
		bucket string
	}{
		{0, "unknown"},
		{5 * time.Second, "instant"},
		{20 * time.Second, "fast"},
		{60 * time.Second, "normal"},
		{200 * time.Second, "slow"},
		{600 * time.Second, "very_slow"},
	}
	for _, tc := range cases {
		plan := &sandbox.BuildPlan{ID: uuid.New(), BuildSystem: "go"}
		result := &sandbox.BuildResult{Duration: tc.dur}
		c := sandbox.ComputeBuildComplexity(plan, result)
		if c.DurationBucket != tc.bucket {
			t.Errorf("duration %v: bucket = %q, want %q", tc.dur, c.DurationBucket, tc.bucket)
		}
	}
}

// ── End-to-end pipeline ─────────────────────────────────────────────────────

func TestComputeBuildComplexity_GradleHighComplexity(t *testing.T) {
	plan := &sandbox.BuildPlan{
		ID:          uuid.New(),
		BuildSystem: "gradle",
		SecretRefs:  []string{"DB_URL", "API_KEY", "TOKEN"},
		PreCommands: []string{"chmod +x gradlew", "apt-get install -y zip"},
		WorkspaceFS: map[string]string{
			"src/main/java/App.java":     "",
			"src/main/java/Service.java": "",
			"src/test/java/AppTest.java": "",
			"build.gradle":               "",
			"settings.gradle":            "",
			"frontend/index.ts":          "",
			"frontend/app.tsx":           "",
			"scripts/deploy.py":          "",
		},
	}
	result := &sandbox.BuildResult{ExitCode: 1, Duration: 180 * time.Second}

	c := sandbox.ComputeBuildComplexity(plan, result)

	if c.Label != sandbox.BuildComplexityModerate && c.Label != sandbox.BuildComplexityHigh && c.Label != sandbox.BuildComplexityCritical {
		t.Errorf("label = %q, expected moderate/high/critical for gradle+secrets+multi-lang+failure", c.Label)
	}
	if c.ResourceHints.MemoryLimit == sandbox.DefaultMemoryLimit {
		t.Errorf("expected memory boost for gradle, got default %q", c.ResourceHints.MemoryLimit)
	}
}

func TestComputeBuildComplexity_GoTrivial(t *testing.T) {
	plan := &sandbox.BuildPlan{
		ID:          uuid.New(),
		BuildSystem: "go",
		WorkspaceFS: map[string]string{"main.go": "package main"},
		Cache:       &sandbox.CacheConfig{VolumeName: "cache-1"},
	}
	result := &sandbox.BuildResult{ExitCode: 0, Duration: 3 * time.Second}

	c := sandbox.ComputeBuildComplexity(plan, result)

	if c.Label != sandbox.BuildComplexityTrivial && c.Label != sandbox.BuildComplexitySimple {
		t.Errorf("label = %q, expected trivial or simple for minimal go build", c.Label)
	}
}

// ── HandleBuildComplexity endpoint ──────────────────────────────────────────

func TestHandleBuildComplexity_NoStore(t *testing.T) {
	h := sandbox.NewHandler(sandbox.NewMockRuntime(), nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/sandbox/complexity/test-repo", nil)
	rec := httptest.NewRecorder()

	h.HandleBuildComplexity(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// ── Complexity in resolve-execute response ──────────────────────────────────

func TestHandleResolveAndExecute_ComplexityInResponse(t *testing.T) {
	mock := sandbox.NewMockRuntime()
	mock.WaitResult = &sandbox.BuildResult{
		ExitCode: 0,
		Logs:     "BUILD OK",
		Duration: 25 * time.Second,
	}

	h := sandbox.NewHandler(mock, nil, nil, nil)

	body, _ := json.Marshal(map[string]any{
		"task_id":      uuid.New().String(),
		"repo_id":      uuid.New().String(),
		"user_id":      uuid.New().String(),
		"build_system": "gradle",
		"command":      "gradle build",
		"base_image":   "rtvortex/builder-jvm:17",
	})

	req := httptest.NewRequest(http.MethodPost, "/sandbox/resolve-execute", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleResolveAndExecute(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	cplx, ok := resp["complexity"].(map[string]any)
	if !ok {
		t.Fatalf("expected complexity object in response, got %T", resp["complexity"])
	}

	if _, ok := cplx["score"].(float64); !ok {
		t.Error("complexity.score missing or not a number")
	}
	if _, ok := cplx["label"].(string); !ok {
		t.Error("complexity.label missing or not a string")
	}
	if _, ok := cplx["duration_bucket"].(string); !ok {
		t.Error("complexity.duration_bucket missing")
	}

	hints, ok := cplx["resource_hints"].(map[string]any)
	if !ok {
		t.Fatal("complexity.resource_hints missing")
	}
	if _, ok := hints["timeout_sec"].(float64); !ok {
		t.Error("resource_hints.timeout_sec missing")
	}
}

func TestHandleResolveAndExecute_ComplexityOnFailedBuild(t *testing.T) {
	mock := sandbox.NewMockRuntime()
	mock.WaitResult = &sandbox.BuildResult{
		ExitCode: 1,
		Logs:     "COMPILE ERROR",
		Duration: 8 * time.Second,
	}

	h := sandbox.NewHandler(mock, nil, nil, nil)

	body, _ := json.Marshal(map[string]any{
		"task_id":      uuid.New().String(),
		"repo_id":      uuid.New().String(),
		"user_id":      uuid.New().String(),
		"build_system": "go",
		"command":      "go build ./...",
		"base_image":   "golang:1.22",
	})

	req := httptest.NewRequest(http.MethodPost, "/sandbox/resolve-execute", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleResolveAndExecute(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)

	cplx, ok := resp["complexity"].(map[string]any)
	if !ok {
		t.Fatal("expected complexity even on failed builds")
	}

	fp, ok := cplx["failure_probability"].(float64)
	if !ok {
		t.Error("failure_probability missing")
	}
	if fp <= 0 {
		t.Errorf("failure_probability = %v, expected > 0 for failed build signals", fp)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && searchStr(haystack, needle)
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
