package swarm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
	swarmauth "github.com/AuralithAI/rtvortex-server/internal/swarm/auth"
)

// Handler holds all dependencies needed by swarm HTTP endpoints.
type Handler struct {
	AuthSvc  *swarmauth.Service
	TaskMgr  *TaskManager
	TeamMgr  *TeamManager
	LLMProxy *LLMProxy
	ELO      *ELOService
	WS       *WSHub
}

// ── Agent Auth endpoints ────────────────────────────────────────────────────

// RegisterAgent handles POST /internal/swarm/auth/register.
// Requires X-Service-Secret header (validated by middleware).
func (h *Handler) RegisterAgent(w http.ResponseWriter, r *http.Request) {
	var req swarmauth.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Role == "" {
		http.Error(w, `{"error":"role is required"}`, http.StatusBadRequest)
		return
	}

	// Register agent in DB.
	agentID, _ := uuid.Parse(req.AgentID)
	if agentID == uuid.Nil {
		agentID = uuid.New()
		req.AgentID = agentID.String()
	}

	teamID, _ := uuid.Parse(req.TeamID)
	if err := h.TeamMgr.RegisterAgent(r.Context(), agentID, req.Role, teamID, req.Hostname, req.Version); err != nil {
		slog.Error("swarm: failed to register agent in DB", "error", err)
		// Non-fatal — continue with JWT issuance.
	}

	// Issue JWT.
	resp, err := h.AuthSvc.Register(r.Context(), req)
	if err != nil {
		slog.Error("swarm: failed to issue agent JWT", "error", err)
		http.Error(w, `{"error":"failed to issue token"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// RevokeAgent handles DELETE /internal/swarm/auth/revoke.
// Requires agent JWT.
func (h *Handler) RevokeAgent(w http.ResponseWriter, r *http.Request) {
	claims, ok := swarmauth.AgentClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if err := h.AuthSvc.Revoke(r.Context(), claims.Subject); err != nil {
		slog.Error("swarm: revoke failed", "agent_id", claims.Subject, "error", err)
		http.Error(w, `{"error":"revoke failed"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── Task endpoints (agent-facing) ───────────────────────────────────────────

// GetNextTask handles GET /internal/swarm/tasks/next.
func (h *Handler) GetNextTask(w http.ResponseWriter, r *http.Request) {
	claims, ok := swarmauth.AgentClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	teamID, _ := uuid.Parse(claims.TeamID)
	if teamID == uuid.Nil {
		http.Error(w, `{"error":"agent has no team_id"}`, http.StatusBadRequest)
		return
	}

	// Find a task assigned to this agent's team.
	tasks, err := h.TaskMgr.ListTasks(r.Context(), "", StatusPlanning, 1)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Filter to this team.
	for _, t := range tasks {
		if t.AssignedTeamID != nil && *t.AssignedTeamID == teamID {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(t)
			return
		}
	}

	// No task assigned.
	w.WriteHeader(http.StatusNoContent)
}

// SubmitPlan handles POST /internal/swarm/tasks/{id}/plan.
func (h *Handler) SubmitPlan(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		Plan json.RawMessage `json:"plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if err := h.TaskMgr.SetPlan(r.Context(), taskID, body.Plan); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Emit WebSocket event.
	if h.WS != nil {
		h.WS.BroadcastPlanEvent(taskID.String(), "plan_submitted", map[string]interface{}{
			"task_id": taskID.String(),
		})
		h.WS.BroadcastTaskEvent("status_changed", taskID.String(), map[string]interface{}{
			"new_status": "plan_review",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"plan_review"}`))
}

// SubmitDiff handles POST /internal/swarm/tasks/{id}/diffs.
func (h *Handler) SubmitDiff(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	var diff TaskDiff
	if err := json.NewDecoder(r.Body).Decode(&diff); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Set agent_id from JWT claims.
	claims, _ := swarmauth.AgentClaimsFromContext(r.Context())
	if claims != nil {
		agentID, _ := uuid.Parse(claims.Subject)
		diff.AgentID = &agentID
	}

	result, err := h.TaskMgr.AddDiff(r.Context(), taskID, diff)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Emit WebSocket event.
	if h.WS != nil {
		h.WS.BroadcastDiffEvent(taskID.String(), result.ID.String(), map[string]interface{}{
			"file_path":   diff.FilePath,
			"change_type": diff.ChangeType,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(result)
}

// ListDiffs handles GET /internal/swarm/tasks/{id}/diffs.
func (h *Handler) ListDiffs(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	diffs, err := h.TaskMgr.ListDiffs(r.Context(), taskID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(diffs)
}

// AddDiffComment handles POST /internal/swarm/tasks/{id}/diffs/{diffId}/comments.
func (h *Handler) AddDiffComment(w http.ResponseWriter, r *http.Request) {
	diffID, err := uuid.Parse(chi.URLParam(r, "diffId"))
	if err != nil {
		http.Error(w, `{"error":"invalid diff id"}`, http.StatusBadRequest)
		return
	}

	var c DiffComment
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Set author from JWT claims.
	claims, _ := swarmauth.AgentClaimsFromContext(r.Context())
	if claims != nil {
		c.AuthorType = "agent"
		c.AuthorID = claims.Subject
	}

	result, err := h.TaskMgr.AddDiffComment(r.Context(), diffID, c)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(result)
}

// CompleteTask handles POST /internal/swarm/tasks/{id}/complete.
func (h *Handler) CompleteTask(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	if err := h.TaskMgr.CompleteTask(r.Context(), taskID); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Emit WebSocket event.
	if h.WS != nil {
		h.WS.BroadcastTaskEvent("completed", taskID.String(), map[string]interface{}{
			"task_id": taskID.String(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"completed"}`))
}

// DeclareTeamSize handles POST /internal/swarm/tasks/{id}/declare-size.
func (h *Handler) DeclareTeamSize(w http.ResponseWriter, r *http.Request) {
	// Placeholder — Orchestrator signals team size adjustment.
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"acknowledged"}`))
}

// Heartbeat handles POST /internal/swarm/heartbeat/{id}.
func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid agent id"}`, http.StatusBadRequest)
		return
	}

	if err := h.TaskMgr.Heartbeat(r.Context(), agentID); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── User-facing task endpoints ──────────────────────────────────────────────

// CreateTaskUser handles POST /api/v1/swarm/tasks (user JWT required).
func (h *Handler) CreateTaskUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RepoID      string `json:"repo_id"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.RepoID == "" || body.Description == "" {
		http.Error(w, `{"error":"repo_id and description are required"}`, http.StatusBadRequest)
		return
	}

	// Extract user ID from context (set by user auth middleware).
	userID := userIDFromRequest(r)

	task, err := h.TaskMgr.CreateTask(r.Context(), body.RepoID, body.Description, userID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Emit WebSocket event.
	if h.WS != nil {
		h.WS.BroadcastTaskEvent("created", task.ID.String(), map[string]interface{}{
			"repo_id":     body.RepoID,
			"description": body.Description,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(task)
}

// ListTasksUser handles GET /api/v1/swarm/tasks.
func (h *Handler) ListTasksUser(w http.ResponseWriter, r *http.Request) {
	repoID := r.URL.Query().Get("repo_id")
	status := r.URL.Query().Get("status")
	tasks, err := h.TaskMgr.ListTasks(r.Context(), repoID, status, 50)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

// GetTaskUser handles GET /api/v1/swarm/tasks/{id}.
func (h *Handler) GetTaskUser(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	task, err := h.TaskMgr.GetTask(r.Context(), taskID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

// PlanAction handles POST /api/v1/swarm/tasks/{id}/plan-action.
func (h *Handler) PlanAction(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		Action  string `json:"action"` // approve, reject, comment
		Comment string `json:"comment,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	switch body.Action {
	case "approve":
		if err := h.TaskMgr.UpdateStatus(r.Context(), taskID, StatusImplementing); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		if h.WS != nil {
			h.WS.BroadcastPlanEvent(taskID.String(), "plan_approved", nil)
			h.WS.BroadcastTaskEvent("status_changed", taskID.String(), map[string]interface{}{
				"new_status": "implementing",
			})
		}
	case "reject":
		if err := h.TaskMgr.UpdateStatus(r.Context(), taskID, StatusCancelled); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		if h.WS != nil {
			h.WS.BroadcastPlanEvent(taskID.String(), "plan_rejected", nil)
			h.WS.BroadcastTaskEvent("status_changed", taskID.String(), map[string]interface{}{
				"new_status": "cancelled",
			})
		}
	case "comment":
		// Just add comment — no status change.
		slog.Info("swarm plan comment", "task_id", taskID, "comment", body.Comment)
		if h.WS != nil {
			h.WS.BroadcastPlanEvent(taskID.String(), "plan_commented", map[string]interface{}{
				"comment": body.Comment,
			})
		}
	default:
		http.Error(w, `{"error":"action must be approve, reject, or comment"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(fmt.Sprintf(`{"status":"%s"}`, body.Action)))
}

// GetDiffsUser handles GET /api/v1/swarm/tasks/{id}/diffs.
func (h *Handler) GetDiffsUser(w http.ResponseWriter, r *http.Request) {
	h.ListDiffs(w, r) // Reuses agent endpoint logic.
}

// UserDiffComment handles POST /api/v1/swarm/tasks/{id}/diffs/{diffId}/comments.
func (h *Handler) UserDiffComment(w http.ResponseWriter, r *http.Request) {
	diffID, err := uuid.Parse(chi.URLParam(r, "diffId"))
	if err != nil {
		http.Error(w, `{"error":"invalid diff id"}`, http.StatusBadRequest)
		return
	}

	var c DiffComment
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	c.AuthorType = "user"
	c.AuthorID = userIDFromRequest(r).String()

	result, err := h.TaskMgr.AddDiffComment(r.Context(), diffID, c)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(result)
}

// DiffAction handles POST /api/v1/swarm/tasks/{id}/diff-action.
func (h *Handler) DiffAction(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		Action string `json:"action"` // approve_diff, request_changes, reject
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	switch body.Action {
	case "approve_diff":
		if err := h.TaskMgr.UpdateStatus(r.Context(), taskID, StatusPRCreating); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
	case "request_changes":
		if err := h.TaskMgr.UpdateStatus(r.Context(), taskID, StatusImplementing); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
	case "reject":
		if err := h.TaskMgr.UpdateStatus(r.Context(), taskID, StatusCancelled); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, `{"error":"action must be approve_diff, request_changes, or reject"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(fmt.Sprintf(`{"status":"%s"}`, body.Action)))
}

// RateTask handles POST /api/v1/swarm/tasks/{id}/rate.
func (h *Handler) RateTaskUser(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if body.Rating < 1 || body.Rating > 5 {
		http.Error(w, `{"error":"rating must be between 1 and 5"}`, http.StatusBadRequest)
		return
	}

	if err := h.TaskMgr.RateTask(r.Context(), taskID, body.Rating, body.Comment); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListAgentsUser handles GET /api/v1/swarm/agents.
func (h *Handler) ListAgentsUser(w http.ResponseWriter, r *http.Request) {
	agents, err := h.TeamMgr.ListAgents(r.Context(), "")
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agents)
}

// ListTeamsUser handles GET /api/v1/swarm/teams.
func (h *Handler) ListTeamsUser(w http.ResponseWriter, r *http.Request) {
	teams, err := h.TeamMgr.ListTeams(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(teams)
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// userIDFromRequest extracts the user UUID from auth context.
// Falls back to a nil UUID if no user auth middleware is present.
func userIDFromRequest(r *http.Request) uuid.UUID {
	id, ok := auth.UserIDFromContext(r.Context())
	if ok {
		return id
	}
	return uuid.Nil
}
