package swarm

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ── CI Signal HTTP Handlers ─────────────────────────────────────────────────
//
// These handlers expose CI signal data to the UI and accept webhook-based
// CI signal ingestion from external systems.
//
//   GET  /api/v1/swarm/tasks/{id}/ci-signal   — get CI signal for a task
//   GET  /api/v1/swarm/ci-signals              — list CI signals for a repo
//   POST /internal/swarm/ci-signal/webhook     — ingest CI signal from webhook
//   POST /internal/swarm/ci-signal/report      — agent reports CI result

// HandleGetCISignal returns the CI signal record for a specific task.
// GET /api/v1/swarm/tasks/{id}/ci-signal
func (h *Handler) HandleGetCISignal(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	signal, err := GetCISignal(r.Context(), h.DB, taskID)
	if err != nil {
		slog.Error("get CI signal failed", "task_id", taskID, "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	if signal == nil {
		// No CI signal record yet — return empty with sensible defaults.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"task_id":  taskID.String(),
			"pr_state": "unknown",
			"ci_state": "unknown",
			"exists":   false,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(signal)
}

// HandleListCISignals returns CI signal summaries for a repo.
// GET /api/v1/swarm/ci-signals?repo_id=xxx&limit=50
func (h *Handler) HandleListCISignals(w http.ResponseWriter, r *http.Request) {
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		http.Error(w, `{"error":"repo_id is required"}`, http.StatusBadRequest)
		return
	}

	limit := parseIntParam(r, "limit", 50)

	signals, err := ListCISignals(r.Context(), h.DB, repoID, limit)
	if err != nil {
		slog.Error("list CI signals failed", "repo_id", repoID, "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"signals": signals,
		"count":   len(signals),
	})
}

// ── Webhook Ingestion ───────────────────────────────────────────────────────

// CIWebhookPayload is the expected body for POST /internal/swarm/ci-signal/webhook.
type CIWebhookPayload struct {
	TaskID       string `json:"task_id"`
	PRState      string `json:"pr_state,omitempty"` // open, merged, closed
	PRMerged     *bool  `json:"pr_merged,omitempty"`
	CIState      string `json:"ci_state,omitempty"` // pending, success, failure, error
	BuildSuccess *bool  `json:"build_success,omitempty"`
	TestsPassed  *bool  `json:"tests_passed,omitempty"`
	Source       string `json:"source,omitempty"` // "github_actions", "jenkins", etc.
}

// HandleCISignalWebhook accepts CI signal updates from external webhooks or CI systems.
// POST /internal/swarm/ci-signal/webhook
func (h *Handler) HandleCISignalWebhook(w http.ResponseWriter, r *http.Request) {
	var payload CIWebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	taskID, err := uuid.Parse(payload.TaskID)
	if err != nil {
		http.Error(w, `{"error":"invalid task_id"}`, http.StatusBadRequest)
		return
	}

	// Look up the task to get repo_id and pr_number.
	task, err := h.TaskMgr.GetTask(r.Context(), taskID)
	if err != nil || task == nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	ctx := r.Context()

	// Upsert the CI signal.
	prState := payload.PRState
	if prState == "" {
		prState = "unknown"
	}
	prMerged := false
	if payload.PRMerged != nil {
		prMerged = *payload.PRMerged
	}
	if prState == "merged" {
		prMerged = true
	}

	ciState := payload.CIState
	if ciState == "" {
		ciState = "unknown"
	}
	// Infer from build/test signals if CI state not provided.
	if ciState == "unknown" {
		if payload.BuildSuccess != nil && payload.TestsPassed != nil {
			if *payload.BuildSuccess && *payload.TestsPassed {
				ciState = "success"
			} else {
				ciState = "failure"
			}
		}
	}

	now := "NOW()"
	_, err = h.DB.Exec(ctx, `
		INSERT INTO swarm_ci_signals (task_id, repo_id, pr_number, pr_state, pr_merged, ci_state, last_polled_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (task_id) DO UPDATE SET
			pr_state       = COALESCE(NULLIF($4, 'unknown'), swarm_ci_signals.pr_state),
			pr_merged      = $5 OR swarm_ci_signals.pr_merged,
			ci_state       = COALESCE(NULLIF($6, 'unknown'), swarm_ci_signals.ci_state),
			last_polled_at = NOW(),
			updated_at     = NOW()`,
		taskID, task.RepoID, task.PRNumber, prState, prMerged, ciState,
	)
	_ = now // suppress unused
	if err != nil {
		slog.Error("CI signal webhook upsert failed", "task_id", taskID, "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// If we received definitive signals, try to ingest into ELO immediately.
	prSettled := prState == "merged" || prState == "closed"
	ciSettled := ciState == "success" || ciState == "failure" || ciState == "error"

	if prSettled && ciSettled && h.RoleELO != nil {
		roles := h.resolveAgentRoles(ctx, task.AssignedAgents)
		outcome := TaskOutcome{
			TaskID:       taskID.String(),
			PRAccepted:   prMerged,
			BuildSuccess: ciState == "success",
			TestsPassed:  ciState == "success",
		}
		for _, role := range roles {
			if roleErr := h.RoleELO.RecordRoleOutcome(ctx, role, task.RepoID, outcome); roleErr != nil {
				slog.Error("CI signal webhook: RecordRoleOutcome failed",
					"role", role, "task_id", taskID, "error", roleErr)
			}
		}

		// Mark as ingested + finalized.
		_, _ = h.DB.Exec(ctx, `
			UPDATE swarm_ci_signals SET
				elo_ingested    = TRUE,
				elo_ingested_at = NOW(),
				finalized       = TRUE,
				finalized_at    = NOW(),
				updated_at      = NOW()
			WHERE task_id = $1`,
			taskID,
		)

		SwarmCISignalIngested.Add(float64(len(roles)))
	}

	slog.Info("CI signal webhook received",
		"task_id", taskID,
		"source", payload.Source,
		"pr_state", prState,
		"ci_state", ciState,
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"task_id":  taskID.String(),
		"pr_state": prState,
		"ci_state": ciState,
	})
}

// ── Agent Report ────────────────────────────────────────────────────────────

// CISignalReportPayload is the body for POST /internal/swarm/ci-signal/report.
type CISignalReportPayload struct {
	TaskID       string `json:"task_id"`
	BuildSuccess bool   `json:"build_success"`
	TestsPassed  bool   `json:"tests_passed"`
	PRAccepted   bool   `json:"pr_accepted"`
	Details      string `json:"details,omitempty"`
}

// HandleCISignalReport accepts CI signal reports from swarm agents.
// POST /internal/swarm/ci-signal/report
func (h *Handler) HandleCISignalReport(w http.ResponseWriter, r *http.Request) {
	var payload CISignalReportPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	taskID, err := uuid.Parse(payload.TaskID)
	if err != nil {
		http.Error(w, `{"error":"invalid task_id"}`, http.StatusBadRequest)
		return
	}

	task, err := h.TaskMgr.GetTask(r.Context(), taskID)
	if err != nil || task == nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	ctx := r.Context()

	ciState := "failure"
	if payload.BuildSuccess && payload.TestsPassed {
		ciState = "success"
	}

	prState := "open"
	if payload.PRAccepted {
		prState = "merged"
	}

	_, err = h.DB.Exec(ctx, `
		INSERT INTO swarm_ci_signals (task_id, repo_id, pr_number, pr_state, pr_merged, ci_state, last_polled_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (task_id) DO UPDATE SET
			pr_state       = $4,
			pr_merged      = $5,
			ci_state       = $6,
			last_polled_at = NOW(),
			updated_at     = NOW()`,
		taskID, task.RepoID, task.PRNumber, prState, payload.PRAccepted, ciState,
	)
	if err != nil {
		slog.Error("CI signal report upsert failed", "task_id", taskID, "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Ingest into role ELO immediately.
	if h.RoleELO != nil {
		roles := h.resolveAgentRoles(ctx, task.AssignedAgents)
		outcome := TaskOutcome{
			TaskID:       taskID.String(),
			PRAccepted:   payload.PRAccepted,
			BuildSuccess: payload.BuildSuccess,
			TestsPassed:  payload.TestsPassed,
		}
		for _, role := range roles {
			if roleErr := h.RoleELO.RecordRoleOutcome(ctx, role, task.RepoID, outcome); roleErr != nil {
				slog.Error("CI signal report: RecordRoleOutcome failed",
					"role", role, "task_id", taskID, "error", roleErr)
			}
		}

		_, _ = h.DB.Exec(ctx, `
			UPDATE swarm_ci_signals SET
				elo_ingested    = TRUE,
				elo_ingested_at = NOW(),
				finalized       = TRUE,
				finalized_at    = NOW(),
				updated_at      = NOW()
			WHERE task_id = $1`,
			taskID,
		)
	}

	slog.Info("CI signal report received",
		"task_id", taskID,
		"build_success", payload.BuildSuccess,
		"tests_passed", payload.TestsPassed,
		"pr_accepted", payload.PRAccepted,
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"task_id": taskID.String(),
	})
}
