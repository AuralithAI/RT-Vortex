package review_test

import (
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/review"
	"github.com/AuralithAI/rtvortex-server/internal/vcs"
)

// ── ExtractJSON ─────────────────────────────────────────────────────────────

func TestExtractJSON_MarkdownFence(t *testing.T) {
	input := "Here are the issues:\n```json\n[{\"line\":1}]\n```\nDone."
	result := review.ExtractJSON(input)
	if result != `[{"line":1}]` {
		t.Errorf("expected '[{\"line\":1}]', got %q", result)
	}
}

func TestExtractJSON_RawArray(t *testing.T) {
	input := `Some text [{"line":5}] more text`
	result := review.ExtractJSON(input)
	if result != `[{"line":5}]` {
		t.Errorf("expected '[{\"line\":5}]', got %q", result)
	}
}

func TestExtractJSON_NoJSON(t *testing.T) {
	result := review.ExtractJSON("no json here")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestExtractJSON_EmptyArray(t *testing.T) {
	input := "```json\n[]\n```"
	result := review.ExtractJSON(input)
	if result != "[]" {
		t.Errorf("expected '[]', got %q", result)
	}
}

func TestExtractJSON_FenceNoLang(t *testing.T) {
	input := "```\n[{\"line\":10}]\n```"
	result := review.ExtractJSON(input)
	if result != `[{"line":10}]` {
		t.Errorf("expected '[{\"line\":10}]', got %q", result)
	}
}

// ── ParseReviewResponse ─────────────────────────────────────────────────────

func TestParseReviewResponse_ValidJSON(t *testing.T) {
	content := `[
		{
			"line": 42,
			"severity": "high",
			"category": "security",
			"title": "SQL Injection",
			"body": "User input not sanitized",
			"suggestion": "Use parameterized queries"
		}
	]`

	comments := review.ParseReviewResponse(content, "main.go")
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	c := comments[0]
	if c.FilePath != "main.go" {
		t.Errorf("expected file path main.go, got %s", c.FilePath)
	}
	if c.LineNumber != 42 {
		t.Errorf("expected line 42, got %d", c.LineNumber)
	}
	if string(c.Severity) != "high" {
		t.Errorf("expected severity high, got %s", c.Severity)
	}
	if c.Category != "security" {
		t.Errorf("expected category security, got %s", c.Category)
	}
	if c.Title != "SQL Injection" {
		t.Errorf("expected title 'SQL Injection', got %s", c.Title)
	}
}

func TestParseReviewResponse_EmptyArray(t *testing.T) {
	comments := review.ParseReviewResponse("[]", "clean.go")
	if len(comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(comments))
	}
}

func TestParseReviewResponse_InvalidJSON(t *testing.T) {
	comments := review.ParseReviewResponse("not json", "bad.go")
	if comments != nil {
		t.Errorf("expected nil for invalid JSON, got %v", comments)
	}
}

func TestParseReviewResponse_UnknownSeverityFallsToInfo(t *testing.T) {
	content := `[{"line":1, "severity":"unknown", "category":"style", "title":"T", "body":"B"}]`
	comments := review.ParseReviewResponse(content, "f.go")
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if string(comments[0].Severity) != "info" {
		t.Errorf("expected severity 'info' for unknown, got %s", comments[0].Severity)
	}
}

func TestParseReviewResponse_MultipleComments(t *testing.T) {
	content := `[
		{"line":1, "severity":"critical", "category":"security", "title":"A", "body":"B"},
		{"line":20, "severity":"low", "category":"style", "title":"C", "body":"D"},
		{"line":50, "severity":"medium", "category":"bug", "title":"E", "body":"F"}
	]`
	comments := review.ParseReviewResponse(content, "multi.go")
	if len(comments) != 3 {
		t.Fatalf("expected 3 comments, got %d", len(comments))
	}
}

func TestParseReviewResponse_MarkdownWrapped(t *testing.T) {
	content := "Here are the issues:\n```json\n[{\"line\":5, \"severity\":\"high\", \"category\":\"bug\", \"title\":\"Null\", \"body\":\"Nil deref\"}]\n```"
	comments := review.ParseReviewResponse(content, "wrapped.go")
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
}

// ── FilterFiles ─────────────────────────────────────────────────────────────

func TestFilterFiles_SkipsDeleted(t *testing.T) {
	files := []vcs.DiffFile{
		{Filename: "a.go", Status: "modified", Patch: "diff"},
		{Filename: "b.go", Status: "deleted", Patch: "diff"},
	}
	cfg := review.PipelineConfig{MaxFilesPerReview: 50, SkipPatterns: nil}
	result := review.FilterFilesExported(cfg, files)
	if len(result) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result))
	}
	if result[0].Filename != "a.go" {
		t.Errorf("expected a.go, got %s", result[0].Filename)
	}
}

func TestFilterFiles_SkipsEmptyPatch(t *testing.T) {
	files := []vcs.DiffFile{
		{Filename: "a.go", Status: "modified", Patch: "diff"},
		{Filename: "b.go", Status: "modified", Patch: ""},
	}
	cfg := review.PipelineConfig{MaxFilesPerReview: 50}
	result := review.FilterFilesExported(cfg, files)
	if len(result) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result))
	}
}

func TestFilterFiles_SkipPatterns(t *testing.T) {
	files := []vcs.DiffFile{
		{Filename: "main.go", Status: "modified", Patch: "diff"},
		{Filename: "go.sum", Status: "modified", Patch: "diff"},
		{Filename: "package-lock.json", Status: "modified", Patch: "diff"},
		{Filename: "vendor/lib.go", Status: "added", Patch: "diff"},
	}
	cfg := review.PipelineConfig{
		MaxFilesPerReview: 50,
		SkipPatterns:      []string{"*.sum", "*.json", "vendor/*"},
	}
	result := review.FilterFilesExported(cfg, files)
	if len(result) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result))
	}
	if result[0].Filename != "main.go" {
		t.Errorf("expected main.go, got %s", result[0].Filename)
	}
}

func TestFilterFiles_MaxFilesLimit(t *testing.T) {
	files := make([]vcs.DiffFile, 100)
	for i := range files {
		files[i] = vcs.DiffFile{Filename: "file.go", Status: "modified", Patch: "diff"}
	}
	cfg := review.PipelineConfig{MaxFilesPerReview: 10}
	result := review.FilterFilesExported(cfg, files)
	if len(result) != 10 {
		t.Errorf("expected 10 files, got %d", len(result))
	}
}

// ── MatchesSkipPattern ──────────────────────────────────────────────────────

func TestMatchesSkipPattern_SingleGlob(t *testing.T) {
	cfg := review.PipelineConfig{SkipPatterns: []string{"*.lock"}}
	if !review.MatchesSkipPatternExported(cfg, "package.lock") {
		t.Error("expected *.lock to match package.lock")
	}
	if review.MatchesSkipPatternExported(cfg, "main.go") {
		t.Error("expected *.lock to NOT match main.go")
	}
}

func TestMatchesSkipPattern_DoubleStarGlob(t *testing.T) {
	cfg := review.PipelineConfig{SkipPatterns: []string{"**/*.min.js"}}
	if !review.MatchesSkipPatternExported(cfg, "assets/js/app.min.js") {
		t.Error("expected **/*.min.js to match assets/js/app.min.js")
	}
	if review.MatchesSkipPatternExported(cfg, "app.js") {
		t.Error("expected **/*.min.js to NOT match app.js")
	}
}

func TestMatchesSkipPattern_NoPatterns(t *testing.T) {
	cfg := review.PipelineConfig{SkipPatterns: nil}
	if review.MatchesSkipPatternExported(cfg, "anything.go") {
		t.Error("expected no patterns to match nothing")
	}
}

// ── PipelineConfig Defaults ─────────────────────────────────────────────────

func TestNewPipeline_ConfigDefaults(t *testing.T) {
	cfg := review.PipelineConfig{}
	p := review.NewPipeline(nil, nil, nil, nil, nil, cfg)
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}
}
