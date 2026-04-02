// Package crossrepo — Dependency Graph Service
//
// DepGraphService sits between the HTTP handler and the gRPC engine client.
// It enforces authorization, resolves linked repos, and delegates to the
// engine's CrossRepoService for manifest retrieval and graph building.
package crossrepo

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/model"
	"github.com/AuralithAI/rtvortex-server/internal/store"
)

// ── DepGraphService ─────────────────────────────────────────────────────────

// DepGraphService orchestrates cross-repo dependency graph operations.
type DepGraphService struct {
	authorizer   *Authorizer
	engineClient *engine.Client
	repoLinkRepo *store.RepoLinkRepo
	repoRepo     *store.RepositoryRepo
}

// NewDepGraphService creates a new dependency graph orchestrator.
func NewDepGraphService(
	authorizer *Authorizer,
	engineClient *engine.Client,
	repoLinkRepo *store.RepoLinkRepo,
	repoRepo *store.RepositoryRepo,
) *DepGraphService {
	return &DepGraphService{
		authorizer:   authorizer,
		engineClient: engineClient,
		repoLinkRepo: repoLinkRepo,
		repoRepo:     repoRepo,
	}
}

// ── GetManifest ─────────────────────────────────────────────────────────────

// GetManifest retrieves the structural manifest for a single repo.
// The user must have at least ActionCrossRepoMetadata access.
func (s *DepGraphService) GetManifest(
	ctx context.Context,
	userID, repoID uuid.UUID,
) (*engine.RepoManifestResult, error) {
	// Manifest is single-repo; no cross-repo authz needed — just verify the
	// user has access to this repo. This is already handled by route-level
	// middleware. We call the engine directly.
	result, err := s.engineClient.GetRepoManifest(ctx, repoID.String())
	if err != nil {
		return nil, fmt.Errorf("engine get manifest: %w", err)
	}
	return result, nil
}

// ── GetDependencies ─────────────────────────────────────────────────────────

// GetDependenciesRequest is the input for resolving cross-repo dependencies.
type GetDependenciesRequest struct {
	UserID        uuid.UUID
	SourceRepoID  uuid.UUID
	TargetRepoIDs []uuid.UUID // empty = all linked repos
	MaxDepth      uint32
}

// GetDependenciesResponse wraps the engine result with authz metadata.
type GetDependenciesResponse struct {
	Dependencies    []engine.CrossRepoDependency
	TotalEdges      uint32
	ReposAuthorized int
	ReposDenied     int
	Duration        time.Duration
}

// GetDependencies resolves inter-repo dependency edges with authorization.
func (s *DepGraphService) GetDependencies(
	ctx context.Context,
	req GetDependenciesRequest,
) (*GetDependenciesResponse, error) {
	start := time.Now()

	// Resolve target repos.
	var targetIDs []uuid.UUID
	if len(req.TargetRepoIDs) > 0 {
		targetIDs = req.TargetRepoIDs
	} else {
		linked, err := s.repoLinkRepo.ListLinkedRepoIDs(ctx, req.SourceRepoID)
		if err != nil {
			return nil, fmt.Errorf("list linked repos: %w", err)
		}
		targetIDs = linked
	}

	// Authorize each target.
	authorized := make([]string, 0, len(targetIDs))
	denied := 0

	for _, tid := range targetIDs {
		decision := s.authorizer.Authorize(
			ctx, req.UserID, req.SourceRepoID, tid,
			model.ActionCrossRepoGraphView,
		)
		if decision.Allowed {
			authorized = append(authorized, tid.String())
		} else {
			denied++
			slog.Debug("dep graph: target repo denied",
				"user_id", req.UserID,
				"target_repo", tid,
				"reason", decision.Reason,
			)
		}
	}

	if len(authorized) == 0 {
		return &GetDependenciesResponse{
			ReposDenied: denied,
			Duration:    time.Since(start),
		}, nil
	}

	result, err := s.engineClient.GetCrossRepoDependencies(
		ctx, req.SourceRepoID.String(), authorized, req.MaxDepth,
	)
	if err != nil {
		return nil, fmt.Errorf("engine get cross-repo deps: %w", err)
	}

	return &GetDependenciesResponse{
		Dependencies:    result.Dependencies,
		TotalEdges:      result.TotalEdges,
		ReposAuthorized: len(authorized),
		ReposDenied:     denied,
		Duration:        time.Since(start),
	}, nil
}

// ── BuildGraph ──────────────────────────────────────────────────────────────

// BuildGraphRequest is the input for building the org-level dependency graph.
type BuildGraphRequest struct {
	UserID      uuid.UUID
	OrgID       uuid.UUID
	RepoIDs     []uuid.UUID // empty = all linked repos in org
	ForceRescan bool
}

// BuildGraphResponse wraps the engine result.
type BuildGraphResponse struct {
	Success      bool
	Message      string
	ReposScanned uint32
	TotalNodes   uint32
	TotalEdges   uint32
	Nodes        []engine.DepGraphNode
	Edges        []engine.DepGraphEdge
	Duration     time.Duration
}

// BuildGraph constructs the org-level dependency graph across authorized repos.
func (s *DepGraphService) BuildGraph(
	ctx context.Context,
	req BuildGraphRequest,
) (*BuildGraphResponse, error) {
	start := time.Now()

	// Resolve repo IDs.
	var repoIDs []uuid.UUID
	if len(req.RepoIDs) > 0 {
		repoIDs = req.RepoIDs
	} else {
		// Get all repos in the org that have links.
		links, _, err := s.repoLinkRepo.ListByOrg(ctx, req.OrgID, 500, 0)
		if err != nil {
			return nil, fmt.Errorf("list org links: %w", err)
		}
		seen := make(map[uuid.UUID]bool)
		for _, l := range links {
			if !seen[l.SourceRepoID] {
				repoIDs = append(repoIDs, l.SourceRepoID)
				seen[l.SourceRepoID] = true
			}
			if !seen[l.TargetRepoID] {
				repoIDs = append(repoIDs, l.TargetRepoID)
				seen[l.TargetRepoID] = true
			}
		}
	}

	// Authorize each repo for graph_view.
	authorized := make([]string, 0, len(repoIDs))
	for _, rid := range repoIDs {
		// For graph building, we check that cross-repo is enabled at org level.
		// Individual link-level authz is checked per edge pair by the engine.
		// Here we verify the user can view graphs for each repo.
		for _, other := range repoIDs {
			if rid == other {
				continue
			}
			decision := s.authorizer.Authorize(
				ctx, req.UserID, rid, other,
				model.ActionCrossRepoGraphView,
			)
			if decision.Allowed {
				if !containsStr(authorized, rid.String()) {
					authorized = append(authorized, rid.String())
				}
				break
			}
		}
	}

	if len(authorized) == 0 {
		return &BuildGraphResponse{
			Success:  false,
			Message:  "no authorized repos for graph building",
			Duration: time.Since(start),
		}, nil
	}

	result, err := s.engineClient.BuildDependencyGraph(
		ctx, req.OrgID.String(), authorized, req.ForceRescan,
	)
	if err != nil {
		return nil, fmt.Errorf("engine build dep graph: %w", err)
	}

	return &BuildGraphResponse{
		Success:      result.Success,
		Message:      result.Message,
		ReposScanned: result.ReposScanned,
		TotalNodes:   result.TotalNodes,
		TotalEdges:   result.TotalEdges,
		Nodes:        result.Nodes,
		Edges:        result.Edges,
		Duration:     time.Since(start),
	}, nil
}

// containsStr checks if a string slice contains a value.
func containsStr(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
