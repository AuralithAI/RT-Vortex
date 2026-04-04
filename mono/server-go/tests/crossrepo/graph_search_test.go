package crossrepo_test

import (
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/crossrepo"
	"github.com/AuralithAI/rtvortex-server/internal/engine"
)

// ── FederatedSearchService ──────────────────────────────────────────────────

func TestNewFederatedSearchService_NilDeps(t *testing.T) {
	// Should not panic with nil dependencies.
	svc := crossrepo.NewFederatedSearchService(nil, nil, nil, nil)
	if svc == nil {
		t.Fatal("expected non-nil FederatedSearchService")
	}
}

// ── DepGraphService ─────────────────────────────────────────────────────────

func TestNewDepGraphService_NilDeps(t *testing.T) {
	svc := crossrepo.NewDepGraphService(nil, nil, nil, nil)
	if svc == nil {
		t.Fatal("expected non-nil DepGraphService")
	}
}

// ── GraphHandler ────────────────────────────────────────────────────────────

func TestNewGraphHandler_NilDeps(t *testing.T) {
	h := crossrepo.NewGraphHandler(nil, nil, nil)
	if h == nil {
		t.Fatal("expected non-nil GraphHandler")
	}
}

// ── FederatedSearchConfig defaults ──────────────────────────────────────────

func TestFederatedSearchConfig_Defaults(t *testing.T) {
	cfg := engine.FederatedSearchConfig{}
	if cfg.MaxTotalResults != 0 {
		t.Errorf("expected default 0, got %d", cfg.MaxTotalResults)
	}
	if cfg.MaxConcurrent != 0 {
		t.Errorf("expected default 0, got %d", cfg.MaxConcurrent)
	}
	if cfg.ScoreNormalization != "" {
		t.Errorf("expected empty string, got %q", cfg.ScoreNormalization)
	}
}

// ── FederatedSearchRequest fields ───────────────────────────────────────────

func TestFederatedSearchRequest_Zero(t *testing.T) {
	req := crossrepo.FederatedSearchRequest{}
	if req.Query != "" {
		t.Errorf("expected empty query, got %q", req.Query)
	}
	if req.TouchedSymbols != nil {
		t.Errorf("expected nil TouchedSymbols")
	}
}

// ── FederatedSearchResponse fields ──────────────────────────────────────────

func TestFederatedSearchResponse_Empty(t *testing.T) {
	resp := crossrepo.FederatedSearchResponse{}
	if resp.ReposAuthorized != 0 {
		t.Errorf("expected 0, got %d", resp.ReposAuthorized)
	}
	if resp.ReposDenied != 0 {
		t.Errorf("expected 0, got %d", resp.ReposDenied)
	}
	if resp.Chunks != nil {
		t.Errorf("expected nil Chunks")
	}
}

// ── GetDependenciesRequest fields ───────────────────────────────────────────

func TestGetDependenciesRequest_Zero(t *testing.T) {
	req := crossrepo.GetDependenciesRequest{}
	if req.MaxDepth != 0 {
		t.Errorf("expected 0 max depth, got %d", req.MaxDepth)
	}
}

// ── BuildGraphRequest fields ────────────────────────────────────────────────

func TestBuildGraphRequest_Defaults(t *testing.T) {
	req := crossrepo.BuildGraphRequest{}
	if req.ForceRescan {
		t.Error("expected ForceRescan=false by default")
	}
}

// ── Integration tests (require engine + DB) ─────────────────────────────────

func TestFederatedSearch_Integration(t *testing.T) {
	t.Skip("integration test: requires running engine and PostgreSQL")
	// Would test:
	// 1. Create org, two repos, link them
	// 2. Index both repos
	// 3. Execute FederatedSearchService.Search
	// 4. Verify results come from both repos
	// 5. Verify denied repos are excluded
}

func TestBuildDepGraph_Integration(t *testing.T) {
	t.Skip("integration test: requires running engine and PostgreSQL")
	// Would test:
	// 1. Create org with 3 repos that share a common dependency
	// 2. Link them with share_profile="full"
	// 3. Execute DepGraphService.BuildGraph
	// 4. Verify nodes include all 3 repos + their modules
	// 5. Verify edges include the dependency relationships
}
