package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
	swarmauth "github.com/AuralithAI/rtvortex-server/internal/swarm/auth"
	"github.com/AuralithAI/rtvortex-server/internal/vcs"
)

// Handler holds all dependencies needed by swarm HTTP endpoints.
type Handler struct {
	AuthSvc     *swarmauth.Service
	TaskMgr     *TaskManager
	TeamMgr     *TeamManager
	LLMProxy    *LLMProxy
	ELO         *ELOService
	WS          *WSHub
	PRCreator   *PRCreator
	VCSResolver *vcs.Resolver
	DB          *pgxpool.Pool
	MemorySvc   *MemoryService
	MCPSvc      MCPCaller
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
	} else {
		// Seed heartbeat so the monitor doesn't mark the agent offline
		// before its first real heartbeat arrives.
		_ = h.TaskMgr.Heartbeat(r.Context(), agentID)
	}

	// Issue JWT.
	resp, err := h.AuthSvc.Register(r.Context(), req)
	if err != nil {
		slog.Error("swarm: failed to issue agent JWT", "error", err)
		http.Error(w, `{"error":"failed to issue token"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
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

// GetTaskInternal handles GET /internal/swarm/tasks/{id} — returns the full
// task for any authenticated agent (controller or team member).
func (h *Handler) GetTaskInternal(w http.ResponseWriter, r *http.Request) {
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
	_ = json.NewEncoder(w).Encode(task)
}

// GetTaskStatus handles GET /internal/swarm/tasks/{id}/status — lightweight
// status poll for agents waiting on plan/diff approval.
func (h *Handler) GetTaskStatus(w http.ResponseWriter, r *http.Request) {
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": task.Status,
	})
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
		// Controller agent — return the oldest submitted (unassigned) task.
		tasks, err := h.TaskMgr.ListTasks(r.Context(), "", StatusSubmitted, 1)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		if len(tasks) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tasks[0])
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
			_ = json.NewEncoder(w).Encode(t)
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
	_, _ = w.Write([]byte(`{"status":"plan_review"}`))
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
	_ = json.NewEncoder(w).Encode(result)
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

	if diffs == nil {
		diffs = []TaskDiff{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"diffs": diffs})
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
	_ = json.NewEncoder(w).Encode(result)
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

	// Increment tasks_done for all contributing agents (without a rating).
	if h.ELO != nil {
		agentIDs, _ := h.TaskMgr.AgentsForTask(r.Context(), taskID)
		for _, agentID := range agentIDs {
			_ = h.ELO.IncrementTasksDone(r.Context(), agentID)
		}
	}

	// Emit WebSocket event.
	if h.WS != nil {
		h.WS.BroadcastTaskEvent("completed", taskID.String(), map[string]interface{}{
			"task_id": taskID.String(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"completed"}`))
}

// DeclareTeamSize handles POST /internal/swarm/tasks/{id}/declare-size.
func (h *Handler) DeclareTeamSize(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		AdditionalAgents int      `json:"additional_agents"`
		Roles            []string `json:"roles,omitempty"`
		TeamID           string   `json:"team_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	task, err := h.TaskMgr.GetTask(r.Context(), taskID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}

	if task.AssignedTeamID == nil {
		// Task has no team yet. Resolve from (in priority order):
		//   1. Explicit team_id in request body (Python consumer knows it)
		//   2. team_id from the caller's JWT claims
		var teamID uuid.UUID
		if body.TeamID != "" {
			teamID, err = uuid.Parse(body.TeamID)
			if err != nil {
				http.Error(w, `{"error":"invalid team_id in body"}`, http.StatusBadRequest)
				return
			}
		}
		if teamID == uuid.Nil {
			claims, ok := swarmauth.AgentClaimsFromContext(r.Context())
			if ok && claims.TeamID != "" {
				teamID, _ = uuid.Parse(claims.TeamID)
			}
		}
		if teamID == uuid.Nil {
			w.Header().Set("Retry-After", "3")
			http.Error(w, `{"error":"task has no assigned team yet, retry later"}`, http.StatusConflict)
			return
		}
		if err := h.TaskMgr.AssignTaskToTeam(r.Context(), taskID, teamID); err != nil {
			slog.Error("declare-size: auto-assign failed", "task_id", taskID, "team_id", teamID, "error", err)
			http.Error(w, `{"error":"failed to assign task to team"}`, http.StatusInternalServerError)
			return
		}
		slog.Info("declare-size: auto-assigned task to team", "task_id", taskID, "team_id", teamID)
		task.AssignedTeamID = &teamID
	}

	if err := h.TeamMgr.ScaleTeam(r.Context(), *task.AssignedTeamID, body.AdditionalAgents); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Broadcast scaling event.
	if h.WS != nil {
		h.WS.BroadcastTaskEvent("team_scaled", taskID.String(), map[string]interface{}{
			"additional_agents": body.AdditionalAgents,
			"roles":             body.Roles,
		})
	}

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"scaling_acknowledged"}`))
}

// FailTask handles POST /internal/swarm/tasks/{id}/fail.
func (h *Handler) FailTask(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.Reason == "" {
		body.Reason = "agent reported failure"
	}

	if err := h.TaskMgr.FailTask(r.Context(), taskID, body.Reason); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"failed"}`))
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
		Title       string `json:"title"`
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

	task, err := h.TaskMgr.CreateTask(r.Context(), body.RepoID, body.Title, body.Description, userID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Emit WebSocket event.
	if h.WS != nil {
		h.WS.BroadcastTaskEvent("created", task.ID.String(), map[string]interface{}{
			"repo_id":     body.RepoID,
			"title":       body.Title,
			"description": body.Description,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(task)
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
	_ = json.NewEncoder(w).Encode(tasks)
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
	_ = json.NewEncoder(w).Encode(task)
}

// GetTaskAgents handles GET /api/v1/swarm/tasks/{id}/agents.
func (h *Handler) GetTaskAgents(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	agents, err := h.TaskMgr.GetTaskAgents(r.Context(), taskID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if agents == nil {
		agents = []AgentSnapshot{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"agents": agents})
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
	_, _ = w.Write([]byte(fmt.Sprintf(`{"status":"%s"}`, body.Action)))
}

// GetDiffsUser handles GET /api/v1/swarm/tasks/{id}/diffs.
// Returns metadata-only diffs by default. Pass ?full=true for full content.
func (h *Handler) GetDiffsUser(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	if r.URL.Query().Get("full") == "true" {
		h.ListDiffs(w, r)
		return
	}

	metas, err := h.TaskMgr.ListDiffsMeta(r.Context(), taskID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if metas == nil {
		metas = []DiffMeta{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"diffs": metas})
}

// GetDiffContent handles GET /api/v1/swarm/tasks/{id}/diffs/{diffId}/content.
// Returns full content for a single diff (original, proposed, unified_diff).
func (h *Handler) GetDiffContent(w http.ResponseWriter, r *http.Request) {
	diffID, err := uuid.Parse(chi.URLParam(r, "diffId"))
	if err != nil {
		http.Error(w, `{"error":"invalid diff id"}`, http.StatusBadRequest)
		return
	}

	diff, err := h.TaskMgr.GetDiff(r.Context(), diffID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(diff)
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
	_ = json.NewEncoder(w).Encode(result)
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
		// Trigger async PR creation.
		if h.PRCreator != nil {
			go func() {
				if prErr := h.PRCreator.CreatePR(context.Background(), taskID); prErr != nil {
					slog.Error("swarm: async PR creation failed", "task_id", taskID, "error", prErr)
				}
			}()
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
	_, _ = w.Write([]byte(fmt.Sprintf(`{"status":"%s"}`, body.Action)))
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

	// Update ELO scores for all agents that worked on this task.
	if h.ELO != nil {
		task, tErr := h.TaskMgr.GetTask(r.Context(), taskID)
		if tErr == nil {
			// Use assigned agents from the task, or fall back to diff-contributing agents.
			agentIDs := task.AssignedAgents
			if len(agentIDs) == 0 {
				agentIDs, _ = h.TaskMgr.AgentsForTask(r.Context(), taskID)
			}
			for _, agentID := range agentIDs {
				if eloErr := h.ELO.RecordFeedback(r.Context(), agentID, body.Rating); eloErr != nil {
					slog.Error("swarm: ELO update failed", "agent_id", agentID, "error", eloErr)
				}
			}
			if len(agentIDs) > 0 {
				slog.Info("swarm: ELO updated for task agents",
					"task_id", taskID,
					"rating", body.Rating,
					"agents", len(agentIDs),
				)
			}
		}
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
	_ = json.NewEncoder(w).Encode(agents)
}

// ListTeamsUser handles GET /api/v1/swarm/teams.
func (h *Handler) ListTeamsUser(w http.ResponseWriter, r *http.Request) {
	teams, err := h.TeamMgr.ListTeams(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(teams)
}

// RetryTask handles POST /api/v1/swarm/tasks/{id}/retry (user JWT required).
func (h *Handler) RetryTask(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	if err := h.TaskMgr.RetryTask(r.Context(), taskID); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"retried"}`))
}

// CancelTask handles POST /api/v1/swarm/tasks/{id}/cancel (user JWT required).
func (h *Handler) CancelTask(w http.ResponseWriter, r *http.Request) {
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

	// Cannot cancel terminal-state tasks.
	switch task.Status {
	case StatusCompleted, StatusCancelled, StatusFailed, StatusTimedOut:
		http.Error(w, fmt.Sprintf(`{"error":"cannot cancel task in status %q"}`, task.Status), http.StatusBadRequest)
		return
	}

	if err := h.TaskMgr.UpdateStatus(r.Context(), taskID, StatusCancelled); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	SwarmTasksTotal.WithLabelValues(StatusCancelled).Inc()
	SwarmTasksActive.Dec()

	// Release the team.
	if task.AssignedTeamID != nil {
		h.TeamMgr.ReleaseTeam(r.Context(), *task.AssignedTeamID)
	}

	if h.WS != nil {
		h.WS.BroadcastTaskEvent("cancelled", taskID.String(), nil)
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"cancelled"}`))
}

// DeleteTaskUser handles DELETE /api/v1/swarm/tasks/{id} (user JWT required).
func (h *Handler) DeleteTaskUser(w http.ResponseWriter, r *http.Request) {
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

	// Release team before deletion if one is assigned.
	if task.AssignedTeamID != nil {
		h.TeamMgr.ReleaseTeam(r.Context(), *task.AssignedTeamID)
	}

	if err := h.TaskMgr.DeleteTask(r.Context(), taskID); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	if h.WS != nil {
		h.WS.BroadcastTaskEvent("deleted", taskID.String(), nil)
	}

	w.WriteHeader(http.StatusNoContent)
}

// TaskHistory handles GET /api/v1/swarm/tasks/history (user JWT required).
func (h *Handler) TaskHistory(w http.ResponseWriter, r *http.Request) {
	repoID := r.URL.Query().Get("repo_id")
	limit := parseIntParam(r, "limit", 25)
	offset := parseIntParam(r, "offset", 0)

	summaries, total, err := h.TaskMgr.ListTaskHistory(r.Context(), repoID, limit, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"tasks":  summaries,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// SwarmOverview handles GET /api/v1/swarm/overview (user JWT required).
func (h *Handler) SwarmOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.TaskMgr.GetOverview(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(overview)
}

// RecordContribution handles POST /internal/swarm/tasks/{id}/contribution.
func (h *Handler) RecordContribution(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		AgentID          string `json:"agent_id"`
		Role             string `json:"role"`
		Phase            string `json:"phase"`
		ContributionType string `json:"contribution_type"`
		TokensUsed       int    `json:"tokens_used"`
		LLMCalls         int    `json:"llm_calls"`
		RAGCalls         int    `json:"rag_calls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	agentID, _ := uuid.Parse(body.AgentID)
	if agentID == uuid.Nil {
		// Try from JWT claims.
		claims, ok := swarmauth.AgentClaimsFromContext(r.Context())
		if ok {
			agentID, _ = uuid.Parse(claims.Subject)
		}
	}

	if err := h.TaskMgr.RecordAgentContribution(
		r.Context(), taskID, agentID,
		body.Role, body.Phase, body.ContributionType,
		body.TokensUsed, body.LLMCalls, body.RAGCalls,
	); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"status":"recorded"}`))
}

// ── Agent Message (live UI broadcast) ───────────────────────────────────────

// AgentMessage handles POST /internal/swarm/tasks/{id}/agent-message.
// Python agents post their thinking/tool_call/edit messages here.
// Go broadcasts them via WebSocket so the browser UI shows a live chat feed.
func (h *Handler) AgentMessage(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	if taskID == "" {
		http.Error(w, `{"error":"missing task id"}`, http.StatusBadRequest)
		return
	}

	var msg struct {
		AgentID   string                 `json:"agent_id"`
		AgentRole string                 `json:"agent_role"`
		Kind      string                 `json:"kind"`
		Content   string                 `json:"content"`
		Metadata  map[string]interface{} `json:"metadata"`
		Timestamp float64                `json:"timestamp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Broadcast to WebSocket subscribers.
	if h.WS != nil {
		h.WS.BroadcastAgentEvent(msg.Kind, taskID, msg.AgentID, map[string]interface{}{
			"agent_role": msg.AgentRole,
			"content":    msg.Content,
			"metadata":   msg.Metadata,
			"timestamp":  msg.Timestamp,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// ── Multi-LLM Discussion Protocol ──────────────────────────────────────────

// DiscussionEvent handles POST /internal/swarm/tasks/{id}/discussion.
// Python agents post discussion thread lifecycle events here (opened, provider
// response, completed, synthesised). Go broadcasts them via WebSocket so the
// browser UI can render multi-model comparison panels in real time.
func (h *Handler) DiscussionEvent(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	if taskID == "" {
		http.Error(w, `{"error":"missing task id"}`, http.StatusBadRequest)
		return
	}

	var evt struct {
		Event             string                 `json:"event"`               // "thread_opened", "provider_response", "thread_completed", "thread_synthesised"
		ThreadID          string                 `json:"thread_id,omitempty"` // required for all except thread_opened (where it's inside thread)
		Thread            map[string]interface{} `json:"thread,omitempty"`    // full thread dict on thread_opened
		Response          map[string]interface{} `json:"response,omitempty"`  // provider response on provider_response
		Synthesis         string                 `json:"synthesis,omitempty"`
		SynthesisProvider string                 `json:"synthesis_provider,omitempty"`
		ProviderCount     int                    `json:"provider_count,omitempty"`
		SuccessCount      int                    `json:"success_count,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if evt.Event == "" {
		http.Error(w, `{"error":"event field is required"}`, http.StatusBadRequest)
		return
	}

	slog.Info("swarm discussion event",
		"task_id", taskID,
		"event", evt.Event,
		"thread_id", evt.ThreadID,
	)

	// Record discussion metrics.
	SwarmDiscussionEventsTotal.WithLabelValues(evt.Event).Inc()

	// Broadcast to WebSocket subscribers.
	if h.WS != nil {
		data := map[string]interface{}{
			"event": evt.Event,
		}
		// Include all non-zero fields.
		if evt.ThreadID != "" {
			data["thread_id"] = evt.ThreadID
		}
		if evt.Thread != nil {
			data["thread"] = evt.Thread
		}
		if evt.Response != nil {
			data["response"] = evt.Response
		}
		if evt.Synthesis != "" {
			data["synthesis"] = evt.Synthesis
		}
		if evt.SynthesisProvider != "" {
			data["synthesis_provider"] = evt.SynthesisProvider
		}
		if evt.ProviderCount > 0 {
			data["provider_count"] = evt.ProviderCount
		}
		if evt.SuccessCount > 0 {
			data["success_count"] = evt.SuccessCount
		}
		h.WS.BroadcastDiscussionEvent(taskID, evt.Event, data)
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// ConsensusEvent handles POST /internal/swarm/tasks/{id}/consensus.
// The Python consensus engine fires this after deciding on a final answer so
// the Go server can record metrics and broadcast the outcome to the UI.
func (h *Handler) ConsensusEvent(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	if taskID == "" {
		http.Error(w, `{"error":"missing task id"}`, http.StatusBadRequest)
		return
	}

	var evt struct {
		ThreadID       string             `json:"thread_id"`
		Strategy       string             `json:"strategy"`   // "pick_best", "majority_vote", "gpt_as_judge", "multi_judge_panel"
		Provider       string             `json:"provider"`   // winning provider or "consensus"
		Model          string             `json:"model"`
		Confidence     float64            `json:"confidence"`
		Reasoning      string             `json:"reasoning"`
		Scores         map[string]float64 `json:"scores,omitempty"`
		LatencyMs      int64              `json:"latency_ms,omitempty"`      // consensus decision time
		JudgeCount     int                `json:"judge_count,omitempty"`     // multi-judge panel: number of judges
		JudgeAgreement float64            `json:"judge_agreement,omitempty"` // multi-judge panel: inter-judge agreement 0.0-1.0
	}
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if evt.Strategy == "" {
		http.Error(w, `{"error":"strategy field is required"}`, http.StatusBadRequest)
		return
	}

	slog.Info("swarm consensus event",
		"task_id", taskID,
		"thread_id", evt.ThreadID,
		"strategy", evt.Strategy,
		"provider", evt.Provider,
		"confidence", evt.Confidence,
		"judge_count", evt.JudgeCount,
		"judge_agreement", evt.JudgeAgreement,
	)

	// Record consensus metrics.
	SwarmConsensusRunsTotal.WithLabelValues(evt.Strategy).Inc()
	if evt.Provider != "" {
		SwarmConsensusWinnerTotal.WithLabelValues(evt.Strategy, evt.Provider).Inc()
	}
	SwarmConsensusConfidence.WithLabelValues(evt.Strategy).Observe(evt.Confidence)
	if evt.LatencyMs > 0 {
		SwarmConsensusLatency.WithLabelValues(evt.Strategy).Observe(float64(evt.LatencyMs) / 1000.0)
	}

	// Record multi-judge panel metrics when applicable.
	if evt.JudgeCount > 0 {
		SwarmConsensusJudgeCount.WithLabelValues(evt.Strategy).Observe(float64(evt.JudgeCount))
		SwarmConsensusJudgeAgreement.WithLabelValues(evt.Strategy).Observe(evt.JudgeAgreement)
	}

	// Broadcast to WebSocket subscribers.
	if h.WS != nil {
		data := map[string]interface{}{
			"event":      "consensus_result",
			"thread_id":  evt.ThreadID,
			"strategy":   evt.Strategy,
			"provider":   evt.Provider,
			"model":      evt.Model,
			"confidence": evt.Confidence,
			"reasoning":  evt.Reasoning,
		}
		if evt.Scores != nil {
			data["scores"] = evt.Scores
		}
		if evt.JudgeCount > 0 {
			data["judge_count"] = evt.JudgeCount
			data["judge_agreement"] = evt.JudgeAgreement
		}
		h.WS.BroadcastDiscussionEvent(taskID, "consensus_result", data)
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// ── VCS Proxy ───────────────────────────────────────────────────────────────

// VCSReadFile handles POST /internal/swarm/vcs/read-file.
// Reads a file from the repository via the platform API (GitHub/GitLab/etc).
func (h *Handler) VCSReadFile(w http.ResponseWriter, r *http.Request) {
	if h.VCSResolver == nil || h.DB == nil {
		http.Error(w, `{"error":"VCS not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var req struct {
		RepoID string `json:"repo_id"`
		Path   string `json:"path"`
		Ref    string `json:"ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	repoID, err := uuid.Parse(req.RepoID)
	if err != nil {
		http.Error(w, `{"error":"invalid repo_id"}`, http.StatusBadRequest)
		return
	}

	// Resolve VCS platform for this repo.
	platform, err := h.VCSResolver.ForRepo(r.Context(), repoID)
	if err != nil {
		slog.Error("swarm vcs: resolve platform failed", "repo_id", req.RepoID, "error", err)
		http.Error(w, fmt.Sprintf(`{"error":"resolve VCS: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Look up repo owner/name from DB.
	owner, repoName, err := h.getRepoInfo(r.Context(), repoID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"get repo info: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	ref := req.Ref
	if ref == "" {
		ref, err = platform.GetDefaultBranch(r.Context(), owner, repoName)
		if err != nil {
			slog.Warn("swarm vcs: fallback to 'main'", "error", err)
			ref = "main"
		}
	}

	content, err := platform.GetFileContent(r.Context(), owner, repoName, req.Path, ref)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"read file: %s"}`, err.Error()), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"content": string(content),
		"path":    req.Path,
		"ref":     ref,
	})
}

// VCSListDir handles POST /internal/swarm/vcs/list-dir.
// Lists directory contents from the repository via the platform API.
func (h *Handler) VCSListDir(w http.ResponseWriter, r *http.Request) {
	if h.VCSResolver == nil || h.DB == nil {
		http.Error(w, `{"error":"VCS not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var req struct {
		RepoID string `json:"repo_id"`
		Path   string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	repoID, err := uuid.Parse(req.RepoID)
	if err != nil {
		http.Error(w, `{"error":"invalid repo_id"}`, http.StatusBadRequest)
		return
	}

	platform, err := h.VCSResolver.ForRepo(r.Context(), repoID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"resolve VCS: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	owner, repoName, err := h.getRepoInfo(r.Context(), repoID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"get repo info: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Use GetFileContent on the directory path — GitHub API returns directory listings.
	// For a more robust implementation, we'd add a ListDirectory method to the Platform interface.
	// For now, we return a placeholder that works with GitHub's tree API.
	_, err = platform.GetFileContent(r.Context(), owner, repoName, req.Path, "")
	if err != nil {
		// If it fails, the path might genuinely be a directory.
		// Return an empty list rather than an error.
		slog.Debug("swarm vcs: list-dir fallback", "path", req.Path, "error", err)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"entries": []interface{}{},
		})
		return
	}

	// If we got content back, this is a file not a directory.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": []map[string]string{
			{"name": req.Path, "type": "file"},
		},
	})
}

// getRepoInfo looks up repository owner and name from the database.
func (h *Handler) getRepoInfo(ctx context.Context, repoID uuid.UUID) (owner, name string, err error) {
	err = h.DB.QueryRow(ctx,
		`SELECT owner, name FROM repositories WHERE id = $1`, repoID,
	).Scan(&owner, &name)
	return
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

// parseIntParam extracts an integer query parameter with a default fallback.
func parseIntParam(r *http.Request, name string, defaultVal int) int {
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
