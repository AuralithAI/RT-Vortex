// Package crossrepo — Pipeline Enricher
//
// PipelineEnricher is an optional component injected into the review pipeline.
// When present, it runs a federated search across linked repos using the
// touched symbols from the engine's ContextPack, then merges the cross-repo
// context chunks into the review prompt.
//
// This allows the LLM to see relevant code from *other* repositories in the
// org when reviewing a PR — catching cross-repo breaking changes, API
// contract violations, and dependency impacts.
package crossrepo

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/metrics"
	"github.com/AuralithAI/rtvortex-server/internal/store"
)

// ── PipelineEnricher ────────────────────────────────────────────────────────

// EnricherConfig controls how cross-repo context enrichment works.
type EnricherConfig struct {
	// MaxCrossRepoChunks caps the number of cross-repo chunks appended
	// to the review context. Default: 10.
	MaxCrossRepoChunks int

	// MinRelevanceScore discards cross-repo chunks below this threshold.
	// Range: 0.0–1.0. Default: 0.3.
	MinRelevanceScore float32

	// Timeout for the cross-repo federated search. Default: 30s.
	Timeout time.Duration

	// MaxConcurrentRepos limits the engine's per-request concurrency.
	// Default: 4.
	MaxConcurrentRepos uint32

	// ScoreNormalization strategy: "min_max", "z_score", or "none".
	// Default: "min_max".
	ScoreNormalization string

	// Enabled gates the entire enrichment flow. When false, Enrich() is
	// a no-op. This allows runtime toggling without removing the wiring.
	Enabled bool
}

// DefaultEnricherConfig returns sensible production defaults.
func DefaultEnricherConfig() EnricherConfig {
	return EnricherConfig{
		MaxCrossRepoChunks: 10,
		MinRelevanceScore:  0.3,
		Timeout:            30 * time.Second,
		MaxConcurrentRepos: 4,
		ScoreNormalization: "min_max",
		Enabled:            true,
	}
}

// CrossRepoContext is the enrichment result appended to the review pipeline.
type CrossRepoContext struct {
	// Chunks from linked repos, filtered and capped.
	Chunks []engine.FederatedChunk

	// ReposSearched is the number of linked repos that were queried.
	ReposSearched int

	// ReposAuthorized / ReposDenied for observability.
	ReposAuthorized int
	ReposDenied     int

	// Duration of the enrichment step.
	Duration time.Duration

	// Empty is true if no cross-repo context was produced (no links, no
	// results, or the enricher is disabled).
	Empty bool
}

// PipelineEnricher enriches review context with cross-repo federated search.
type PipelineEnricher struct {
	federatedSearch *FederatedSearchService
	repoLinkRepo    *store.RepoLinkRepo
	config          EnricherConfig
}

// NewPipelineEnricher creates a new cross-repo pipeline enricher.
// Returns nil if federatedSearch is nil (graceful degradation).
func NewPipelineEnricher(
	federatedSearch *FederatedSearchService,
	repoLinkRepo *store.RepoLinkRepo,
	config EnricherConfig,
) *PipelineEnricher {
	if federatedSearch == nil {
		return nil
	}
	return &PipelineEnricher{
		federatedSearch: federatedSearch,
		repoLinkRepo:   repoLinkRepo,
		config:          config,
	}
}

// Enrich runs federated search using touched symbols from the current repo's
// ContextPack and returns cross-repo context to merge into the LLM prompt.
//
// Parameters:
//   - ctx: request context (respects cancellation)
//   - userID: the user who triggered the review (for authorization)
//   - repoID: the source repo being reviewed
//   - touchedSymbols: symbols affected by the PR diff (from engine ContextPack)
//   - query: the PR title + description as a natural language search query
//
// Returns a CrossRepoContext that the pipeline can embed in the prompt.
// On any error, returns an empty CrossRepoContext (never fails the pipeline).
func (e *PipelineEnricher) Enrich(
	ctx context.Context,
	userID, repoID uuid.UUID,
	touchedSymbols []string,
	query string,
) *CrossRepoContext {
	start := time.Now()

	// Guard: disabled or nil receiver.
	if e == nil || !e.config.Enabled {
		return &CrossRepoContext{Empty: true, Duration: time.Since(start)}
	}

	// Fast path: check if the repo has any active links.
	linked, err := e.repoLinkRepo.ListLinkedRepoIDs(ctx, repoID)
	if err != nil {
		slog.Warn("cross-repo enricher: failed to list linked repos",
			"repo_id", repoID, "error", err)
		metrics.RecordCrossRepoEnrichment("error", 0, time.Since(start))
		return &CrossRepoContext{Empty: true, Duration: time.Since(start)}
	}
	if len(linked) == 0 {
		return &CrossRepoContext{Empty: true, Duration: time.Since(start)}
	}

	// Build the federated search query.
	// Use touched symbols as the primary signal, with the PR title as context.
	searchQuery := query
	if searchQuery == "" && len(touchedSymbols) > 0 {
		searchQuery = touchedSymbols[0] // at minimum use the first symbol
	}
	if searchQuery == "" {
		return &CrossRepoContext{Empty: true, Duration: time.Since(start)}
	}

	// Apply timeout.
	searchCtx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	result, err := e.federatedSearch.Search(searchCtx, FederatedSearchRequest{
		UserID:         userID,
		SourceRepoID:   repoID,
		Query:          searchQuery,
		TouchedSymbols: touchedSymbols,
		Config: engine.FederatedSearchConfig{
			SearchConfig: engine.SearchConfig{
				TopK: uint32(e.config.MaxCrossRepoChunks * 2), // fetch 2x and filter
			},
			MaxTotalResults:    uint32(e.config.MaxCrossRepoChunks * 3),
			MaxConcurrent:      e.config.MaxConcurrentRepos,
			ScoreNormalization: e.config.ScoreNormalization,
		},
	})
	if err != nil {
		slog.Warn("cross-repo enricher: federated search failed",
			"repo_id", repoID, "error", err)
		metrics.RecordCrossRepoEnrichment("error", 0, time.Since(start))
		return &CrossRepoContext{Empty: true, Duration: time.Since(start)}
	}

	// Filter by minimum score and cap.
	filtered := filterAndCapChunks(result.Chunks, e.config.MinRelevanceScore, e.config.MaxCrossRepoChunks)

	duration := time.Since(start)

	// Record Prometheus metrics.
	status := "ok"
	if len(filtered) == 0 {
		status = "empty"
	}
	metrics.RecordCrossRepoEnrichment(status, result.ReposAuthorized, duration)

	slog.Info("cross-repo enricher: completed",
		"repo_id", repoID,
		"repos_searched", result.ReposAuthorized,
		"repos_denied", result.ReposDenied,
		"raw_chunks", len(result.Chunks),
		"filtered_chunks", len(filtered),
		"duration", duration,
	)

	return &CrossRepoContext{
		Chunks:          filtered,
		ReposSearched:   result.ReposAuthorized,
		ReposAuthorized: result.ReposAuthorized,
		ReposDenied:     result.ReposDenied,
		Duration:        duration,
		Empty:           len(filtered) == 0,
	}
}

// filterAndCapChunks removes chunks below the score threshold and caps the total.
func filterAndCapChunks(chunks []engine.FederatedChunk, minScore float32, maxChunks int) []engine.FederatedChunk {
	if len(chunks) == 0 {
		return nil
	}

	filtered := make([]engine.FederatedChunk, 0, len(chunks))
	for _, c := range chunks {
		if c.NormalizedScore >= minScore {
			filtered = append(filtered, c)
		}
	}

	if maxChunks > 0 && len(filtered) > maxChunks {
		filtered = filtered[:maxChunks]
	}

	return filtered
}

// FormatForPrompt renders the cross-repo context as a Markdown section
// suitable for embedding in the LLM review prompt.
func (crc *CrossRepoContext) FormatForPrompt() string {
	if crc == nil || crc.Empty || len(crc.Chunks) == 0 {
		return ""
	}

	result := "\n\n## Cross-Repo Context (from linked repositories)\n"
	result += fmt.Sprintf("Searched %d linked repositories. The following code from **other repos** in this organization may be affected by these changes:\n\n",
		crc.ReposSearched)

	for i, chunk := range crc.Chunks {
		repoLabel := chunk.RepoID
		if chunk.RepoName != "" {
			repoLabel = chunk.RepoName
		}

		result += fmt.Sprintf("### [%s] %s (L%d–L%d, score: %.2f)\n```%s\n%s\n```\n\n",
			repoLabel,
			chunk.Chunk.FilePath,
			chunk.Chunk.StartLine,
			chunk.Chunk.EndLine,
			chunk.NormalizedScore,
			chunk.Chunk.Language,
			chunk.Chunk.Content,
		)

		if i >= 9 { // hard cap at 10 even if config allows more
			break
		}
	}

	return result
}
