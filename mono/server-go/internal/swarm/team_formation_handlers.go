package swarm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ── Team Formation HTTP Handlers ────────────────────────────────────────────
//
// POST /internal/swarm/team-recommend   — agents call this to get an optimal
//                                         team composition for a task.
// GET  /api/v1/swarm/tasks/{id}/team-formation — user-facing: view the
//                                         stored team formation for a task.

// HandleTeamRecommend handles POST /internal/swarm/team-recommend.
// The Python orchestrator calls this after producing a plan; the response
// tells it which roles to instantiate and how many agents to request.
func (h *Handler) HandleTeamRecommend(w http.ResponseWriter, r *http.Request) {
	if h.TeamFormSvc == nil {
		http.Error(w, `{"error":"team formation service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var req TeamRecommendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.TaskID == "" || req.RepoID == "" {
		http.Error(w, `{"error":"task_id and repo_id are required"}`, http.StatusBadRequest)
		return
	}

	taskID, err := uuid.Parse(req.TaskID)
	if err != nil {
		http.Error(w, `{"error":"invalid task_id"}`, http.StatusBadRequest)
		return
	}

	// If no plan is provided in the request body, try to load it from the task.
	plan := req.Plan
	if len(plan) == 0 {
		task, tErr := h.TaskMgr.GetTask(r.Context(), taskID)
		if tErr != nil {
			http.Error(w, fmt.Sprintf(`{"error":"task not found: %s"}`, tErr.Error()), http.StatusNotFound)
			return
		}
		plan = task.PlanDocument
	}

	// Compute recommendation.
	formation, err := h.TeamFormSvc.RecommendTeam(r.Context(), req.RepoID, plan, req.Signals)
	if err != nil {
		slog.Error("team-recommend failed", "task_id", req.TaskID, "error", err)
		http.Error(w, fmt.Sprintf(`{"error":"recommendation failed: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Persist on the task row.
	if err := h.TeamFormSvc.StoreFormation(r.Context(), taskID, formation); err != nil {
		slog.Error("team-recommend store failed", "task_id", req.TaskID, "error", err)
		// Non-fatal — still return the recommendation.
	}

	// Broadcast to WebSocket.
	if h.WS != nil {
		h.WS.BroadcastTaskEvent("team_formation", taskID.String(), map[string]interface{}{
			"complexity_score":  formation.ComplexityScore,
			"complexity_label":  formation.ComplexityLabel,
			"recommended_roles": formation.RecommendedRoles,
			"team_size":         formation.TeamSize,
			"strategy":          formation.Strategy,
			"reasoning":         formation.Reasoning,
		})
	}

	// Record Prometheus metric.
	SwarmTeamFormationsTotal.WithLabelValues(formation.ComplexityLabel, formation.Strategy).Inc()

	slog.Info("team formation recommended",
		"task_id", req.TaskID,
		"repo_id", req.RepoID,
		"complexity", formation.ComplexityLabel,
		"score", formation.ComplexityScore,
		"team_size", formation.TeamSize,
		"roles", formation.RecommendedRoles,
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(formation)
}

// HandleGetTeamFormation handles GET /api/v1/swarm/tasks/{id}/team-formation.
// Returns the stored team formation for a task (if any).
func (h *Handler) HandleGetTeamFormation(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	task, err := h.TaskMgr.GetTask(r.Context(), taskID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"task not found: %s"}`, err.Error()), http.StatusNotFound)
		return
	}

	if len(task.TeamFormation) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"team_formation":null}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"team_formation":%s}`, string(task.TeamFormation))
}
