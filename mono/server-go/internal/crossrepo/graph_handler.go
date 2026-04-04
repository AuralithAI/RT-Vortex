// Package crossrepo — HTTP Handlers for Dep Graph and Federated Search
//
// These handlers are separate from the link-management Handler because they
// depend on the engine client and the orchestrator services. They are wired
// into the router from server.go.
package crossrepo

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/audit"
	"github.com/AuralithAI/rtvortex-server/internal/auth"
	"github.com/AuralithAI/rtvortex-server/internal/engine"
)

// ── GraphHandler ────────────────────────────────────────────────────────────

// GraphHandler serves cross-repo dependency graph and federated search endpoints.
type GraphHandler struct {
	depGraph        *DepGraphService
	federatedSearch *FederatedSearchService
	auditLogger     *audit.Logger
}

// NewGraphHandler creates a new handler for graph + search endpoints.
func NewGraphHandler(
	depGraph *DepGraphService,
	federatedSearch *FederatedSearchService,
	auditLogger *audit.Logger,
) *GraphHandler {
	return &GraphHandler{
		depGraph:        depGraph,
		federatedSearch: federatedSearch,
		auditLogger:     auditLogger,
	}
}

// RegisterRepoRoutes mounts repo-level graph + search routes.
// Expected mount point: /api/v1/repos/{repoID}/cross-repo
func (h *GraphHandler) RegisterRepoRoutes(r chi.Router) {
	r.Get("/manifest", h.GetManifest)
	r.Get("/dependencies", h.GetDependencies)
	r.Post("/search", h.FederatedSearch)
}

// RegisterOrgRoutes mounts org-level graph routes.
// Expected mount point: /api/v1/orgs/{orgID}/cross-repo
func (h *GraphHandler) RegisterOrgRoutes(r chi.Router) {
	r.Post("/graph", h.BuildGraph)
}

// ── GetManifest ─────────────────────────────────────────────────────────────

// GetManifest returns the structural manifest for a repo.
// GET /api/v1/repos/{repoID}/cross-repo/manifest
func (h *GraphHandler) GetManifest(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonErr(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		jsonErr(w, "invalid repo ID", http.StatusBadRequest)
		return
	}

	result, err := h.depGraph.GetManifest(r.Context(), userID, repoID)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// ── GetDependencies ─────────────────────────────────────────────────────────

// GetDependencies resolves cross-repo dependency edges from a source repo.
// GET /api/v1/repos/{repoID}/cross-repo/dependencies?target_repo_ids=...&max_depth=...
func (h *GraphHandler) GetDependencies(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonErr(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sourceRepoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		jsonErr(w, "invalid repo ID", http.StatusBadRequest)
		return
	}

	// Parse optional target_repo_ids query param (comma-separated).
	var targetIDs []uuid.UUID
	if ids := r.URL.Query().Get("target_repo_ids"); ids != "" {
		for _, idStr := range splitComma(ids) {
			id, err := uuid.Parse(idStr)
			if err != nil {
				continue
			}
			targetIDs = append(targetIDs, id)
		}
	}

	maxDepth := uint32(parseIntQuery(r, "max_depth", 0))

	result, err := h.depGraph.GetDependencies(r.Context(), GetDependenciesRequest{
		UserID:        userID,
		SourceRepoID:  sourceRepoID,
		TargetRepoIDs: targetIDs,
		MaxDepth:      maxDepth,
	})
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// ── BuildGraph ──────────────────────────────────────────────────────────────

type buildGraphRequest struct {
	RepoIDs     []string `json:"repo_ids"`
	ForceRescan bool     `json:"force_rescan"`
}

// BuildGraph constructs the org-level dependency graph.
// POST /api/v1/orgs/{orgID}/cross-repo/graph
func (h *GraphHandler) BuildGraph(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonErr(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		jsonErr(w, "invalid org ID", http.StatusBadRequest)
		return
	}

	var req buildGraphRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var repoIDs []uuid.UUID
	for _, idStr := range req.RepoIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		repoIDs = append(repoIDs, id)
	}

	result, err := h.depGraph.BuildGraph(r.Context(), BuildGraphRequest{
		UserID:      userID,
		OrgID:       orgID,
		RepoIDs:     repoIDs,
		ForceRescan: req.ForceRescan,
	})
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// ── FederatedSearch ─────────────────────────────────────────────────────────

type federatedSearchRequest struct {
	Query              string   `json:"query"`
	TouchedSymbols     []string `json:"touched_symbols"`
	TopK               uint32   `json:"top_k"`
	MaxTotalResults    uint32   `json:"max_total_results"`
	MaxConcurrent      uint32   `json:"max_concurrent"`
	ScoreNormalization string   `json:"score_normalization"`
}

// FederatedSearch executes a cross-repo federated search.
// POST /api/v1/repos/{repoID}/cross-repo/search
func (h *GraphHandler) FederatedSearch(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		jsonErr(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sourceRepoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		jsonErr(w, "invalid repo ID", http.StatusBadRequest)
		return
	}

	var req federatedSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Query == "" {
		jsonErr(w, "query is required", http.StatusBadRequest)
		return
	}

	cfg := engine.FederatedSearchConfig{
		MaxTotalResults:    req.MaxTotalResults,
		MaxConcurrent:      req.MaxConcurrent,
		ScoreNormalization: req.ScoreNormalization,
	}
	if req.TopK > 0 {
		cfg.TopK = req.TopK
	}

	result, err := h.federatedSearch.Search(r.Context(), FederatedSearchRequest{
		UserID:         userID,
		SourceRepoID:   sourceRepoID,
		Query:          req.Query,
		TouchedSymbols: req.TouchedSymbols,
		Config:         cfg,
	})
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Audit the search.
	go h.auditLogger.LogRequest(r,
		"crossrepo.federated_search",
		"repository",
		sourceRepoID.String(),
		map[string]interface{}{
			"query":            req.Query,
			"repos_authorized": result.ReposAuthorized,
			"repos_denied":     result.ReposDenied,
			"results_count":    len(result.Chunks),
			"duration_ms":      result.TotalDuration.Milliseconds(),
		},
	)

	writeJSON(w, http.StatusOK, result)
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func jsonErr(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func parseIntQuery(r *http.Request, name string, defaultVal int) int {
	s := r.URL.Query().Get(name)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return defaultVal
	}
	return v
}

func splitComma(s string) []string {
	var result []string
	current := ""
	for _, c := range s {
		if c == ',' {
			if current != "" {
				result = append(result, current)
			}
			current = ""
		} else if c != ' ' {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
