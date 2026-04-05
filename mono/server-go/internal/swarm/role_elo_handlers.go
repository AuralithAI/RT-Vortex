package swarm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// ── Role ELO HTTP Handlers ────────────────────────────────────────
//
// Internal endpoints (agent JWT):
//   POST /internal/swarm/role-elo/outcome   — record task outcome for a role
//   GET  /internal/swarm/role-elo/{role}     — get role ELO for a role+repo
//
// Public endpoints (user JWT):
//   GET  /api/v1/swarm/role-elo              — leaderboard (all roles or filtered by repo)
//   GET  /api/v1/swarm/role-elo/{role}/history — ELO change history for a role+repo

// HandleRoleELOOutcome handles POST /internal/swarm/role-elo/outcome.
// Python agents report task outcomes for role-based ELO updates.
func (h *Handler) HandleRoleELOOutcome(w http.ResponseWriter, r *http.Request) {
	if h.RoleELO == nil {
		http.Error(w, `{"error":"role ELO service not available"}`, http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Role              string  `json:"role"`
		RepoID            string  `json:"repo_id"`
		TaskID            string  `json:"task_id"`
		HumanRating       int     `json:"human_rating,omitempty"`
		ConsensusConf     float64 `json:"consensus_confidence,omitempty"`
		ConsensusStrategy string  `json:"consensus_strategy,omitempty"`
		ConsensusWin      bool    `json:"consensus_win,omitempty"`
		TestsPassed       bool    `json:"tests_passed,omitempty"`
		PRAccepted        bool    `json:"pr_accepted,omitempty"`
		BuildSuccess      bool    `json:"build_success,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if body.Role == "" || body.RepoID == "" {
		http.Error(w, `{"error":"role and repo_id are required"}`, http.StatusBadRequest)
		return
	}

	outcome := TaskOutcome{
		TaskID:            body.TaskID,
		HumanRating:       body.HumanRating,
		ConsensusConf:     body.ConsensusConf,
		ConsensusStrategy: body.ConsensusStrategy,
		ConsensusWin:      body.ConsensusWin,
		TestsPassed:       body.TestsPassed,
		PRAccepted:        body.PRAccepted,
		BuildSuccess:      body.BuildSuccess,
	}

	if err := h.RoleELO.RecordRoleOutcome(r.Context(), body.Role, body.RepoID, outcome); err != nil {
		slog.Error("role ELO outcome failed", "role", body.Role, "repo_id", body.RepoID, "error", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Fetch the updated record to return it.
	re, err := h.RoleELO.GetRoleELO(r.Context(), body.Role, body.RepoID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(re)
}

// HandleGetRoleELO handles GET /internal/swarm/role-elo/{role}.
// Returns the ELO record for a specific role in a repo.
func (h *Handler) HandleGetRoleELO(w http.ResponseWriter, r *http.Request) {
	if h.RoleELO == nil {
		http.Error(w, `{"error":"role ELO service not available"}`, http.StatusServiceUnavailable)
		return
	}

	role := chi.URLParam(r, "role")
	repoID := r.URL.Query().Get("repo_id")

	if role == "" || repoID == "" {
		http.Error(w, `{"error":"role (path) and repo_id (query) are required"}`, http.StatusBadRequest)
		return
	}

	re, err := h.RoleELO.GetRoleELO(r.Context(), role, repoID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(re)
}

// HandleRoleELOLeaderboard handles GET /api/v1/swarm/role-elo.
// Returns the leaderboard of role ELO records, optionally filtered by repo.
func (h *Handler) HandleRoleELOLeaderboard(w http.ResponseWriter, r *http.Request) {
	if h.RoleELO == nil {
		http.Error(w, `{"error":"role ELO service not available"}`, http.StatusServiceUnavailable)
		return
	}

	repoID := r.URL.Query().Get("repo_id")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	records, err := h.RoleELO.ListRoleELOs(r.Context(), repoID, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if records == nil {
		records = []RoleELO{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"records": records,
		"repo_id": repoID,
	})
}

// HandleRoleELOHistory handles GET /api/v1/swarm/role-elo/{role}/history.
// Returns the ELO change history for a (role, repo_id).
func (h *Handler) HandleRoleELOHistory(w http.ResponseWriter, r *http.Request) {
	if h.RoleELO == nil {
		http.Error(w, `{"error":"role ELO service not available"}`, http.StatusServiceUnavailable)
		return
	}

	role := chi.URLParam(r, "role")
	repoID := r.URL.Query().Get("repo_id")

	if role == "" || repoID == "" {
		http.Error(w, `{"error":"role (path) and repo_id (query) are required"}`, http.StatusBadRequest)
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	history, err := h.RoleELO.GetRoleELOHistory(r.Context(), role, repoID, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if history == nil {
		history = []RoleELOHistory{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"history": history,
		"role":    role,
		"repo_id": repoID,
	})
}
