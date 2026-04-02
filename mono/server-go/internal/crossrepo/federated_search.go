// Package crossrepo — Federated Search Orchestrator
//
// FederatedSearchService sits between the HTTP handler and the gRPC engine
// client. It enforces authorization via crossrepo.Authorizer, resolves
// the set of allowed target repos, then delegates to engine.Client.FederatedSearch.
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

// ── FederatedSearchService ──────────────────────────────────────────────────

// FederatedSearchService orchestrates cross-repo searches with authorization.
type FederatedSearchService struct {
	authorizer   *Authorizer
	engineClient *engine.Client
	repoLinkRepo *store.RepoLinkRepo
	repoRepo     *store.RepositoryRepo
}

// NewFederatedSearchService creates a new federated search orchestrator.
func NewFederatedSearchService(
	authorizer *Authorizer,
	engineClient *engine.Client,
	repoLinkRepo *store.RepoLinkRepo,
	repoRepo *store.RepositoryRepo,
) *FederatedSearchService {
	return &FederatedSearchService{
		authorizer:   authorizer,
		engineClient: engineClient,
		repoLinkRepo: repoLinkRepo,
		repoRepo:     repoRepo,
	}
}

// FederatedSearchRequest is the input for a cross-repo federated search.
type FederatedSearchRequest struct {
	// The user making the request (extracted from JWT).
	UserID uuid.UUID
	// The source repo where the user is working (context origin).
	SourceRepoID uuid.UUID
	// Query text.
	Query string
	// Optional touched symbols from the diff context.
	TouchedSymbols []string
	// Search config (top-k, weights, filters).
	Config engine.FederatedSearchConfig
}

// FederatedSearchResponse is the output of a cross-repo federated search.
type FederatedSearchResponse struct {
	// Chunks from all authorized repos, merged and score-normalized.
	Chunks []engine.FederatedChunk
	// Metrics about the search execution.
	Metrics *engine.FederatedSearchMetrics
	// ReposAuthorized is the count of repos that passed authorization.
	ReposAuthorized int
	// ReposDenied is the count of linked repos the user couldn't access.
	ReposDenied int
	// DeniedReasons maps denied repo IDs to their denial reason.
	DeniedReasons map[string]string
	// TotalDuration is the end-to-end wall-clock time including authz.
	TotalDuration time.Duration
}

// Search executes a federated search across all repos linked to sourceRepoID
// that the user is authorized to access.
//
// Flow:
//  1. Fetch all repo IDs linked to sourceRepoID.
//  2. For each linked repo, call Authorizer.Authorize() with ActionCrossRepoSearch.
//  3. Collect the authorized repo ID list.
//  4. Call engine.Client.FederatedSearch with the authorized list.
//  5. Enrich results with repo names from the DB.
func (s *FederatedSearchService) Search(
	ctx context.Context,
	req FederatedSearchRequest,
) (*FederatedSearchResponse, error) {
	start := time.Now()

	// Step 1: Get all linked repo IDs.
	linkedIDs, err := s.repoLinkRepo.ListLinkedRepoIDs(ctx, req.SourceRepoID)
	if err != nil {
		return nil, fmt.Errorf("list linked repo IDs: %w", err)
	}

	if len(linkedIDs) == 0 {
		return &FederatedSearchResponse{
			TotalDuration: time.Since(start),
		}, nil
	}

	// Step 2: Authorize each linked repo.
	authorized := make([]string, 0, len(linkedIDs))
	denied := make(map[string]string)

	for _, targetID := range linkedIDs {
		decision := s.authorizer.Authorize(
			ctx,
			req.UserID,
			req.SourceRepoID,
			targetID,
			model.ActionCrossRepoSearch,
		)

		if decision.Allowed {
			authorized = append(authorized, targetID.String())
		} else {
			denied[targetID.String()] = decision.Reason
			slog.Debug("federated search: repo denied",
				"user_id", req.UserID,
				"source_repo", req.SourceRepoID,
				"target_repo", targetID,
				"reason", decision.Reason,
			)
		}
	}

	if len(authorized) == 0 {
		return &FederatedSearchResponse{
			ReposDenied:   len(denied),
			DeniedReasons: denied,
			TotalDuration: time.Since(start),
		}, nil
	}

	// Step 3: Include the source repo itself.
	sourceRepoStr := req.SourceRepoID.String()
	allRepoIDs := append([]string{sourceRepoStr}, authorized...)

	// Step 4: Call the engine.
	searchResult, err := s.engineClient.FederatedSearch(
		ctx,
		req.Query,
		allRepoIDs,
		req.TouchedSymbols,
		req.Config,
	)
	if err != nil {
		return nil, fmt.Errorf("engine federated search: %w", err)
	}

	// Step 5: Enrich with repo names.
	repoNames := s.resolveRepoNames(ctx, allRepoIDs)
	for i := range searchResult.Chunks {
		if name, ok := repoNames[searchResult.Chunks[i].RepoID]; ok {
			searchResult.Chunks[i].RepoName = name
		}
	}

	return &FederatedSearchResponse{
		Chunks:          searchResult.Chunks,
		Metrics:         searchResult.Metrics,
		ReposAuthorized: len(authorized),
		ReposDenied:     len(denied),
		DeniedReasons:   denied,
		TotalDuration:   time.Since(start),
	}, nil
}

// resolveRepoNames fetches repo names for a list of repo ID strings.
func (s *FederatedSearchService) resolveRepoNames(
	ctx context.Context,
	repoIDs []string,
) map[string]string {
	names := make(map[string]string, len(repoIDs))
	for _, idStr := range repoIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		repo, err := s.repoRepo.GetByID(ctx, id)
		if err != nil {
			continue
		}
		names[idStr] = repo.Name
	}
	return names
}
