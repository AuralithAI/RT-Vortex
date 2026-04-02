package review

import "github.com/AuralithAI/rtvortex-server/internal/vcs"
import "github.com/AuralithAI/rtvortex-server/internal/model"
import "github.com/AuralithAI/rtvortex-server/internal/crossrepo"

// ── Exported test helpers ───────────────────────────────────────────────────
// These wrappers expose unexported functions for unit testing.
// They are compiled only when building with the test tag, but since they are
// in the production package we keep them unconditional and rely on the
// compiler to strip unused code in non-test binaries (which Go does).

// ExtractJSON wraps extractJSON for testing.
func ExtractJSON(content string) string { return extractJSON(content) }

// ParseReviewResponse wraps parseReviewResponse for testing.
func ParseReviewResponse(content, filePath string) []*model.ReviewComment {
	return parseReviewResponse(content, filePath)
}

// FilterFilesExported wraps the pipeline's filterFiles method for testing.
func FilterFilesExported(cfg PipelineConfig, files []vcs.DiffFile) []vcs.DiffFile {
	p := &Pipeline{config: cfg}
	return p.filterFiles(files)
}

// MatchesSkipPatternExported wraps the pipeline's matchesSkipPattern for testing.
func MatchesSkipPatternExported(cfg PipelineConfig, filename string) bool {
	p := &Pipeline{config: cfg}
	return p.matchesSkipPattern(filename)
}

// SetCrossRepoEnricherExported wraps SetCrossRepoEnricher for testing.
func SetCrossRepoEnricherExported(p *Pipeline, enricher *crossrepo.PipelineEnricher) {
	p.SetCrossRepoEnricher(enricher)
}
