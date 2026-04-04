package crossrepo_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/crossrepo"
	"github.com/AuralithAI/rtvortex-server/internal/engine"
)

// ── PipelineEnricher ────────────────────────────────────────────────────────

func TestNewPipelineEnricher_NilSearch(t *testing.T) {
	// Should return nil when federatedSearch is nil (graceful degradation).
	enricher := crossrepo.NewPipelineEnricher(nil, nil, crossrepo.DefaultEnricherConfig())
	if enricher != nil {
		t.Error("expected nil enricher when federatedSearch is nil")
	}
}

func TestNewPipelineEnricher_NilEnricherSafe(t *testing.T) {
	// Enrich on a nil enricher should not panic.
	var enricher *crossrepo.PipelineEnricher
	result := enricher.Enrich(context.TODO(), uuid.Nil, uuid.Nil, nil, "test")
	// The nil receiver check in Enrich should produce Empty=true.
	// Since receiver is nil, this tests the nil guard.
	_ = result
}

func TestDefaultEnricherConfig(t *testing.T) {
	cfg := crossrepo.DefaultEnricherConfig()

	if cfg.MaxCrossRepoChunks != 10 {
		t.Errorf("expected MaxCrossRepoChunks=10, got %d", cfg.MaxCrossRepoChunks)
	}
	if cfg.MinRelevanceScore != 0.3 {
		t.Errorf("expected MinRelevanceScore=0.3, got %f", cfg.MinRelevanceScore)
	}
	if cfg.Timeout.Seconds() != 30 {
		t.Errorf("expected Timeout=30s, got %s", cfg.Timeout)
	}
	if cfg.MaxConcurrentRepos != 4 {
		t.Errorf("expected MaxConcurrentRepos=4, got %d", cfg.MaxConcurrentRepos)
	}
	if cfg.ScoreNormalization != "min_max" {
		t.Errorf("expected ScoreNormalization=min_max, got %q", cfg.ScoreNormalization)
	}
	if !cfg.Enabled {
		t.Error("expected Enabled=true by default")
	}
}

// ── CrossRepoContext ────────────────────────────────────────────────────────

func TestCrossRepoContext_FormatForPrompt_Empty(t *testing.T) {
	ctx := &crossrepo.CrossRepoContext{Empty: true}
	if ctx.FormatForPrompt() != "" {
		t.Error("expected empty string for empty context")
	}
}

func TestCrossRepoContext_FormatForPrompt_NilSafe(t *testing.T) {
	var ctx *crossrepo.CrossRepoContext
	if ctx.FormatForPrompt() != "" {
		t.Error("expected empty string for nil context")
	}
}

func TestCrossRepoContext_FormatForPrompt_WithChunks(t *testing.T) {
	ctx := &crossrepo.CrossRepoContext{
		Chunks: []engine.FederatedChunk{
			{
				RepoID:   "repo-1",
				RepoName: "org/backend",
				Chunk: engine.ContextChunk{
					FilePath:       "src/api/handler.go",
					Content:        "func HandleRequest(w http.ResponseWriter, r *http.Request) {}",
					Language:       "go",
					StartLine:      10,
					EndLine:        15,
					RelevanceScore: 0.85,
				},
				NormalizedScore: 0.85,
				RawScore:        0.92,
			},
			{
				RepoID:   "repo-2",
				RepoName: "org/shared-lib",
				Chunk: engine.ContextChunk{
					FilePath:       "pkg/models/user.go",
					Content:        "type User struct { Name string }",
					Language:       "go",
					StartLine:      1,
					EndLine:        3,
					RelevanceScore: 0.72,
				},
				NormalizedScore: 0.72,
				RawScore:        0.78,
			},
		},
		ReposSearched: 3,
		Empty:         false,
	}

	output := ctx.FormatForPrompt()

	if output == "" {
		t.Fatal("expected non-empty prompt output")
	}

	// Check that repo names appear.
	if !containsSubstring(output, "org/backend") {
		t.Error("expected repo name 'org/backend' in prompt")
	}
	if !containsSubstring(output, "org/shared-lib") {
		t.Error("expected repo name 'org/shared-lib' in prompt")
	}

	// Check that file paths appear.
	if !containsSubstring(output, "src/api/handler.go") {
		t.Error("expected file path in prompt")
	}

	// Check that code content appears.
	if !containsSubstring(output, "HandleRequest") {
		t.Error("expected code content in prompt")
	}

	// Check that header mentions linked repos.
	if !containsSubstring(output, "Cross-Repo Context") {
		t.Error("expected 'Cross-Repo Context' header in prompt")
	}
	if !containsSubstring(output, "3 linked repositories") {
		t.Error("expected repos count in header")
	}
}

func TestCrossRepoContext_FormatForPrompt_RepoIDFallback(t *testing.T) {
	// When RepoName is empty, should use RepoID.
	ctx := &crossrepo.CrossRepoContext{
		Chunks: []engine.FederatedChunk{
			{
				RepoID: "abc-123",
				Chunk: engine.ContextChunk{
					FilePath:  "main.go",
					Content:   "package main",
					Language:  "go",
					StartLine: 1,
					EndLine:   1,
				},
				NormalizedScore: 0.5,
			},
		},
		ReposSearched: 1,
		Empty:         false,
	}

	output := ctx.FormatForPrompt()
	if !containsSubstring(output, "abc-123") {
		t.Error("expected repo ID fallback in prompt")
	}
}

// ── EnricherConfig edge cases ───────────────────────────────────────────────

func TestEnricherConfig_ZeroValues(t *testing.T) {
	cfg := crossrepo.EnricherConfig{}
	if cfg.Enabled {
		t.Error("zero-value config should have Enabled=false")
	}
	if cfg.MaxCrossRepoChunks != 0 {
		t.Error("zero-value config should have MaxCrossRepoChunks=0")
	}
}

// ── FilterAndCapChunks (indirectly via FormatForPrompt) ─────────────────────

func TestCrossRepoContext_FormatForPrompt_CappedAt10(t *testing.T) {
	chunks := make([]engine.FederatedChunk, 15)
	for i := range chunks {
		chunks[i] = engine.FederatedChunk{
			RepoID: "repo-x",
			Chunk: engine.ContextChunk{
				FilePath:  "file.go",
				Content:   "code",
				Language:  "go",
				StartLine: 1,
				EndLine:   1,
			},
			NormalizedScore: 0.9,
		}
	}

	ctx := &crossrepo.CrossRepoContext{
		Chunks:        chunks,
		ReposSearched: 1,
		Empty:         false,
	}

	output := ctx.FormatForPrompt()
	// FormatForPrompt has a hard cap of 10 chunks (break at i >= 9).
	// Count occurrences of "### [" which marks each chunk.
	count := 0
	for i := 0; i < len(output); i++ {
		if i+4 < len(output) && output[i:i+4] == "### " {
			count++
		}
	}
	if count > 10 {
		t.Errorf("expected at most 10 chunks in output, got %d", count)
	}
}

// ── Integration placeholders ────────────────────────────────────────────────

func TestPipelineEnricher_Integration(t *testing.T) {
	t.Skip("integration test: requires running engine, PostgreSQL, and linked repos")
	// Would test:
	// 1. Create org with 2 repos, link them with share_profile="full"
	// 2. Index both repos
	// 3. Make a change to repo A that touches a symbol used in repo B
	// 4. Run PipelineEnricher.Enrich with the touched symbols
	// 5. Verify chunks from repo B appear in the result
	// 6. Verify min relevance score filtering works
	// 7. Verify max chunk cap works
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && searchSubstring(s, sub)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
