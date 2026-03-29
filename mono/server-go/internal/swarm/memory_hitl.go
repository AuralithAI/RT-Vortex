package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── MTM (Medium-Term Memory) ────────────────────────────────────────────────
// Stores per-agent-role, per-repo insights that persist across tasks.
// TTL: 7 days (cleaned up by the janitor).

// MTMInsight represents a single medium-term memory entry.
type MTMInsight struct {
	ID         uuid.UUID `json:"id"`
	RepoID     string    `json:"repo_id"`
	AgentRole  string    `json:"agent_role"`
	Key        string    `json:"key"`
	Insight    string    `json:"insight"`
	Confidence float64   `json:"confidence"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// MemoryService manages the agent memory hierarchy (MTM in Postgres).
// STM is handled in Python (Redis), LTM is handled by the C++ engine.
type MemoryService struct {
	db *pgxpool.Pool
}

// NewMemoryService creates a MemoryService backed by Postgres.
func NewMemoryService(db *pgxpool.Pool) *MemoryService {
	return &MemoryService{db: db}
}

// StoreMTM upserts a medium-term memory insight. If the same key already
// exists for the repo+role, the insight is updated (idempotent).
func (m *MemoryService) StoreMTM(ctx context.Context, insight MTMInsight) error {
	insight.UpdatedAt = time.Now().UTC()
	if insight.ID == uuid.Nil {
		insight.ID = uuid.New()
	}
	if insight.CreatedAt.IsZero() {
		insight.CreatedAt = insight.UpdatedAt
	}

	_, err := m.db.Exec(ctx, `
		INSERT INTO swarm_agent_memory (id, repo_id, agent_role, key, insight, confidence, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (repo_id, agent_role, key) DO UPDATE SET
			insight = EXCLUDED.insight,
			confidence = GREATEST(swarm_agent_memory.confidence, EXCLUDED.confidence),
			updated_at = EXCLUDED.updated_at`,
		insight.ID, insight.RepoID, insight.AgentRole, insight.Key,
		insight.Insight, insight.Confidence, insight.CreatedAt, insight.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("storing MTM insight: %w", err)
	}
	return nil
}

// RecallMTM retrieves MTM insights for a repo+role, ordered by recency.
func (m *MemoryService) RecallMTM(ctx context.Context, repoID, agentRole string, limit int) ([]MTMInsight, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := m.db.Query(ctx, `
		SELECT id, repo_id, agent_role, key, insight, confidence, created_at, updated_at
		FROM swarm_agent_memory
		WHERE repo_id = $1 AND agent_role = $2
		ORDER BY updated_at DESC LIMIT $3`,
		repoID, agentRole, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("recalling MTM: %w", err)
	}
	defer rows.Close()

	var insights []MTMInsight
	for rows.Next() {
		var i MTMInsight
		if err := rows.Scan(&i.ID, &i.RepoID, &i.AgentRole, &i.Key,
			&i.Insight, &i.Confidence, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning MTM insight: %w", err)
		}
		insights = append(insights, i)
	}
	return insights, nil
}

// PruneStaleMTM deletes MTM entries older than the given threshold.
func (m *MemoryService) PruneStaleMTM(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	tag, err := m.db.Exec(ctx, `
		DELETE FROM swarm_agent_memory WHERE updated_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("pruning stale MTM: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ── HITL (Human-in-the-Loop) ────────────────────────────────────────────────
// Agents post questions to the human via WebSocket; the response arrives via Redis.

const (
	hitlQuestionPrefix = "swarm:hitl:question:"
	hitlResponsePrefix = "swarm:hitl:response:"
)

// HITLQuestion represents a question from an agent to the human.
type HITLQuestion struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	AgentID   string `json:"agent_id"`
	AgentRole string `json:"agent_role"`
	Question  string `json:"question"`
	Context   string `json:"context"`
	Urgency   string `json:"urgency"` // low, normal, high
	Timestamp int64  `json:"timestamp"`
}

// HITLResponse represents the human's answer to an agent's question.
type HITLResponse struct {
	QuestionID string `json:"question_id"`
	Response   string `json:"response"`
	Timestamp  int64  `json:"timestamp"`
}

// ── HTTP Handlers ───────────────────────────────────────────────────────────

// HandleMTMStore handles POST /internal/swarm/memory/mtm.
func (h *Handler) HandleMTMStore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RepoID     string  `json:"repo_id"`
		AgentRole  string  `json:"agent_role"`
		Key        string  `json:"key"`
		Insight    string  `json:"insight"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.RepoID == "" || body.AgentRole == "" || body.Key == "" {
		http.Error(w, `{"error":"repo_id, agent_role, and key are required"}`, http.StatusBadRequest)
		return
	}

	if h.MemorySvc == nil {
		http.Error(w, `{"error":"memory service not available"}`, http.StatusServiceUnavailable)
		return
	}

	insight := MTMInsight{
		RepoID:     body.RepoID,
		AgentRole:  body.AgentRole,
		Key:        body.Key,
		Insight:    body.Insight,
		Confidence: body.Confidence,
	}

	if err := h.MemorySvc.StoreMTM(r.Context(), insight); err != nil {
		slog.Error("swarm MTM store failed", "error", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"status":"stored"}`))
}

// HandleMTMRecall handles GET /internal/swarm/memory/mtm.
func (h *Handler) HandleMTMRecall(w http.ResponseWriter, r *http.Request) {
	repoID := r.URL.Query().Get("repo_id")
	agentRole := r.URL.Query().Get("agent_role")
	limitStr := r.URL.Query().Get("limit")

	if repoID == "" || agentRole == "" {
		http.Error(w, `{"error":"repo_id and agent_role are required"}`, http.StatusBadRequest)
		return
	}
	if h.MemorySvc == nil {
		http.Error(w, `{"error":"memory service not available"}`, http.StatusServiceUnavailable)
		return
	}

	limit := 10
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	insights, err := h.MemorySvc.RecallMTM(r.Context(), repoID, agentRole, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if insights == nil {
		insights = []MTMInsight{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"insights": insights,
	})
}

// HandleHITLAsk handles POST /internal/swarm/hitl/ask.
// Agent posts a question; Go broadcasts to WS and polls Redis for response.
func (h *Handler) HandleHITLAsk(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Question       string `json:"question"`
		Context        string `json:"context"`
		Urgency        string `json:"urgency"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.Question == "" {
		http.Error(w, `{"error":"question is required"}`, http.StatusBadRequest)
		return
	}
	if body.TimeoutSeconds <= 0 {
		body.TimeoutSeconds = 300
	}
	if body.Urgency == "" {
		body.Urgency = "normal"
	}

	// Generate a unique question ID.
	qID := uuid.New().String()

	// Extract task ID from the URL if present, or from claims.
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		taskID = "unknown"
	}

	// Build the question.
	question := HITLQuestion{
		ID:        qID,
		TaskID:    taskID,
		Question:  body.Question,
		Context:   body.Context,
		Urgency:   body.Urgency,
		Timestamp: time.Now().Unix(),
	}

	// Store question in Redis so the UI can fetch it.
	qJSON, _ := json.Marshal(question)
	if h.TaskMgr != nil && h.TaskMgr.redis != nil {
		h.TaskMgr.redis.Set(r.Context(), hitlQuestionPrefix+qID, string(qJSON),
			time.Duration(body.TimeoutSeconds)*time.Second)
	}

	// Broadcast via WebSocket so the human sees it immediately.
	if h.WS != nil {
		h.WS.BroadcastTaskEvent("hitl_question", taskID, map[string]interface{}{
			"question_id": qID,
			"question":    body.Question,
			"context":     body.Context,
			"urgency":     body.Urgency,
		})
	}

	// Extend the HTTP write deadline since we're long-polling.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(time.Duration(body.TimeoutSeconds+10) * time.Second))

	// Poll Redis for the response with a detached context (bypass chi timeout).
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(body.TimeoutSeconds)*time.Second)
	defer cancel()

	pollInterval := 2 * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"response":  fmt.Sprintf("Human did not respond within %ds. Proceeding with best judgment.", body.TimeoutSeconds),
				"timed_out": "true",
			})
			return

		case <-ticker.C:
			if h.TaskMgr == nil || h.TaskMgr.redis == nil {
				continue
			}
			val, err := h.TaskMgr.redis.Get(ctx, hitlResponsePrefix+qID).Result()
			if err != nil {
				continue // Not yet responded.
			}

			// Response found!
			var resp HITLResponse
			if jsonErr := json.Unmarshal([]byte(val), &resp); jsonErr != nil {
				resp.Response = val
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"response": resp.Response,
			})
			return
		}
	}
}

// HandleHITLRespond handles POST /api/v1/swarm/hitl/respond (user JWT).
// Human posts their response to an agent's question.
func (h *Handler) HandleHITLRespond(w http.ResponseWriter, r *http.Request) {
	var body struct {
		QuestionID string `json:"question_id"`
		Response   string `json:"response"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.QuestionID == "" || body.Response == "" {
		http.Error(w, `{"error":"question_id and response are required"}`, http.StatusBadRequest)
		return
	}

	resp := HITLResponse{
		QuestionID: body.QuestionID,
		Response:   body.Response,
		Timestamp:  time.Now().Unix(),
	}

	respJSON, _ := json.Marshal(resp)
	if h.TaskMgr != nil && h.TaskMgr.redis != nil {
		h.TaskMgr.redis.Set(r.Context(), hitlResponsePrefix+body.QuestionID,
			string(respJSON), 10*time.Minute)
	}

	// Broadcast response event so the agent-side WS sees it too.
	if h.WS != nil {
		h.WS.BroadcastTaskEvent("hitl_response", "", map[string]interface{}{
			"question_id": body.QuestionID,
			"response":    body.Response,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"delivered"}`))
}

// HandleCIRun handles POST /internal/swarm/ci/run.
// Proxies test/build/lint commands for the agent swarm.
// Currently returns a structured placeholder — actual CI integration
// depends on the repo's CI provider (GitHub Actions, etc.).
func (h *Handler) HandleCIRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CommandType    string   `json:"command_type"` // test, build, lint
		Command        string   `json:"command"`
		Files          []string `json:"files"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// CI proxy is a structured placeholder.
	// Actual CI execution requires integration with the repo's CI system.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"output":      fmt.Sprintf("[CI proxy] %s command queued. Actual CI execution requires repo-specific CI integration.", body.CommandType),
		"exit_code":   0,
		"duration_ms": 0,
		"note":        "CI proxy integration is a placeholder. Connect to your CI system for real execution.",
	})
}
