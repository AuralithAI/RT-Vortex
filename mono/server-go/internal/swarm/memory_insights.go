package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// ── Consensus Insight (Cross-Task Learning) ─────────────────────────────────
//
// After every consensus decision the system extracts durable insights:
//   - Provider reliability patterns ("grok responds fastest for Go repos")
//   - Strategy effectiveness ("multi_judge_panel improved confidence by 15%")
//   - Code pattern observations ("this repo uses error wrapping")
//   - Provider agreement/disagreement tendencies
//
// These are stored per-repo in swarm_consensus_insights and recalled into
// agent prompts on subsequent tasks, enabling genuine cross-task learning.
//
// TTL: 30 days (cleaned up by the janitor).

// InsightCategory classifies what kind of cross-task learning an insight
// represents so agents can filter for relevant knowledge.
type InsightCategory string

const (
	InsightProviderReliability InsightCategory = "provider_reliability"
	InsightStrategyEffective   InsightCategory = "strategy_effectiveness"
	InsightCodePattern         InsightCategory = "code_pattern"
	InsightProviderAgreement   InsightCategory = "provider_agreement"
	InsightQualitySignal       InsightCategory = "quality_signal"
)

// ConsensusInsight represents a durable cross-task learning extracted from
// a consensus decision. Scoped to a repository so insights stay relevant.
type ConsensusInsight struct {
	ID         uuid.UUID       `json:"id"`
	RepoID     string          `json:"repo_id"`
	TaskID     string          `json:"task_id"`     // source task
	ThreadID   string          `json:"thread_id"`   // source discussion thread
	Category   InsightCategory `json:"category"`
	Key        string          `json:"key"`         // dedup key within repo+category
	Insight    string          `json:"insight"`     // human-readable insight text
	Confidence float64         `json:"confidence"`  // 0.0-1.0
	Strategy   string          `json:"strategy"`    // consensus strategy that produced this
	Provider   string          `json:"provider"`    // winning provider (or "consensus")
	Metadata   json.RawMessage `json:"metadata,omitempty"` // additional structured data
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// ProviderReliabilityStats aggregates cross-task performance for a provider
// in a specific repository.
type ProviderReliabilityStats struct {
	Provider       string  `json:"provider"`
	WinCount       int     `json:"win_count"`
	TotalAppear    int     `json:"total_appearances"`
	AvgConfidence  float64 `json:"avg_confidence"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	WinRate        float64 `json:"win_rate"`
	LastSeen       string  `json:"last_seen"`
}

// ── Database Operations ─────────────────────────────────────────────────────

// StoreConsensusInsight upserts a consensus-derived insight. If the same
// key+category already exists for the repo, the insight text and confidence
// are updated (keeps the most confident observation).
func (m *MemoryService) StoreConsensusInsight(ctx context.Context, insight ConsensusInsight) error {
	insight.UpdatedAt = time.Now().UTC()
	if insight.ID == uuid.Nil {
		insight.ID = uuid.New()
	}
	if insight.CreatedAt.IsZero() {
		insight.CreatedAt = insight.UpdatedAt
	}
	if insight.Metadata == nil {
		insight.Metadata = json.RawMessage("{}")
	}

	_, err := m.db.Exec(ctx, `
		INSERT INTO swarm_consensus_insights
			(id, repo_id, task_id, thread_id, category, key, insight,
			 confidence, strategy, provider, metadata, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (repo_id, category, key) DO UPDATE SET
			insight    = CASE WHEN EXCLUDED.confidence >= swarm_consensus_insights.confidence
			             THEN EXCLUDED.insight ELSE swarm_consensus_insights.insight END,
			confidence = GREATEST(swarm_consensus_insights.confidence, EXCLUDED.confidence),
			task_id    = EXCLUDED.task_id,
			thread_id  = EXCLUDED.thread_id,
			strategy   = EXCLUDED.strategy,
			provider   = EXCLUDED.provider,
			metadata   = EXCLUDED.metadata,
			updated_at = EXCLUDED.updated_at`,
		insight.ID, insight.RepoID, insight.TaskID, insight.ThreadID,
		string(insight.Category), insight.Key, insight.Insight,
		insight.Confidence, insight.Strategy, insight.Provider,
		insight.Metadata, insight.CreatedAt, insight.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("storing consensus insight: %w", err)
	}
	return nil
}

// RecallConsensusInsights retrieves cross-task insights for a repository,
// optionally filtered by category, ordered by confidence descending.
func (m *MemoryService) RecallConsensusInsights(
	ctx context.Context,
	repoID string,
	category string,
	limit int,
) ([]ConsensusInsight, error) {
	if limit <= 0 {
		limit = 20
	}

	var query string
	var args []interface{}
	if category != "" {
		query = `
			SELECT id, repo_id, task_id, thread_id, category, key, insight,
			       confidence, strategy, provider, metadata, created_at, updated_at
			FROM swarm_consensus_insights
			WHERE repo_id = $1 AND category = $2
			ORDER BY confidence DESC, updated_at DESC
			LIMIT $3`
		args = []interface{}{repoID, category, limit}
	} else {
		query = `
			SELECT id, repo_id, task_id, thread_id, category, key, insight,
			       confidence, strategy, provider, metadata, created_at, updated_at
			FROM swarm_consensus_insights
			WHERE repo_id = $1
			ORDER BY confidence DESC, updated_at DESC
			LIMIT $2`
		args = []interface{}{repoID, limit}
	}

	rows, err := m.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("recalling consensus insights: %w", err)
	}
	defer rows.Close()

	var insights []ConsensusInsight
	for rows.Next() {
		var i ConsensusInsight
		var cat string
		if err := rows.Scan(
			&i.ID, &i.RepoID, &i.TaskID, &i.ThreadID, &cat, &i.Key,
			&i.Insight, &i.Confidence, &i.Strategy, &i.Provider,
			&i.Metadata, &i.CreatedAt, &i.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning consensus insight: %w", err)
		}
		i.Category = InsightCategory(cat)
		insights = append(insights, i)
	}
	return insights, nil
}

// GetProviderReliabilityStats computes aggregated provider performance
// across all consensus decisions for a repository.
func (m *MemoryService) GetProviderReliabilityStats(
	ctx context.Context,
	repoID string,
) ([]ProviderReliabilityStats, error) {
	rows, err := m.db.Query(ctx, `
		SELECT provider,
		       COUNT(*) FILTER (WHERE provider = ci.provider) AS win_count,
		       COUNT(*) AS total_appearances,
		       AVG(confidence) AS avg_confidence,
		       MAX(updated_at) AS last_seen
		FROM swarm_consensus_insights ci
		WHERE repo_id = $1 AND category = 'provider_reliability'
		GROUP BY provider
		ORDER BY win_count DESC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying provider reliability: %w", err)
	}
	defer rows.Close()

	var stats []ProviderReliabilityStats
	for rows.Next() {
		var s ProviderReliabilityStats
		var lastSeen time.Time
		if err := rows.Scan(&s.Provider, &s.WinCount, &s.TotalAppear,
			&s.AvgConfidence, &lastSeen); err != nil {
			return nil, fmt.Errorf("scanning provider reliability: %w", err)
		}
		s.LastSeen = lastSeen.Format(time.RFC3339)
		if s.TotalAppear > 0 {
			s.WinRate = float64(s.WinCount) / float64(s.TotalAppear)
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// PruneStaleInsights removes consensus insights older than the threshold.
func (m *MemoryService) PruneStaleInsights(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	tag, err := m.db.Exec(ctx, `
		DELETE FROM swarm_consensus_insights WHERE updated_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("pruning stale insights: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ── Auto-Extract Insights from Consensus Events ────────────────────────────

// ExtractAndStoreInsights analyses a consensus event and extracts durable
// insights for cross-task learning. Called automatically from ConsensusEvent.
func (m *MemoryService) ExtractAndStoreInsights(
	ctx context.Context,
	repoID, taskID, threadID string,
	strategy, provider, model string,
	confidence float64,
	scores map[string]float64,
	judgeCount int,
	judgeAgreement float64,
) {
	if repoID == "" {
		return
	}

	// 1. Provider reliability: record that this provider won.
	if provider != "" && provider != "consensus" {
		meta, _ := json.Marshal(map[string]interface{}{
			"model":      model,
			"confidence": confidence,
			"strategy":   strategy,
		})
		_ = m.StoreConsensusInsight(ctx, ConsensusInsight{
			RepoID:     repoID,
			TaskID:     taskID,
			ThreadID:   threadID,
			Category:   InsightProviderReliability,
			Key:        fmt.Sprintf("winner:%s", provider),
			Insight:    fmt.Sprintf("%s (model: %s) won consensus with %.0f%% confidence using %s strategy.", provider, model, confidence*100, strategy),
			Confidence: confidence,
			Strategy:   strategy,
			Provider:   provider,
			Metadata:   meta,
		})
	}

	// 2. Strategy effectiveness: record which strategy was used and its confidence.
	{
		meta, _ := json.Marshal(map[string]interface{}{
			"provider":        provider,
			"confidence":      confidence,
			"judge_count":     judgeCount,
			"judge_agreement": judgeAgreement,
		})
		_ = m.StoreConsensusInsight(ctx, ConsensusInsight{
			RepoID:     repoID,
			TaskID:     taskID,
			ThreadID:   threadID,
			Category:   InsightStrategyEffective,
			Key:        fmt.Sprintf("strategy:%s:task:%s", strategy, taskID[:8]),
			Insight:    fmt.Sprintf("Strategy %s selected %s with %.0f%% confidence.", strategy, provider, confidence*100),
			Confidence: confidence,
			Strategy:   strategy,
			Provider:   provider,
			Metadata:   meta,
		})
	}

	// 3. Provider agreement: if we have multi-judge data, record agreement patterns.
	if judgeCount >= 2 {
		agreementLevel := "high"
		if judgeAgreement < 0.5 {
			agreementLevel = "low"
		} else if judgeAgreement < 0.75 {
			agreementLevel = "moderate"
		}
		meta, _ := json.Marshal(map[string]interface{}{
			"judge_count":     judgeCount,
			"judge_agreement": judgeAgreement,
			"winner":          provider,
		})
		_ = m.StoreConsensusInsight(ctx, ConsensusInsight{
			RepoID:     repoID,
			TaskID:     taskID,
			ThreadID:   threadID,
			Category:   InsightProviderAgreement,
			Key:        fmt.Sprintf("agreement:task:%s", taskID[:8]),
			Insight:    fmt.Sprintf("Multi-judge panel (%d judges) showed %s agreement (%.0f%%) — winner: %s.", judgeCount, agreementLevel, judgeAgreement*100, provider),
			Confidence: judgeAgreement,
			Strategy:   strategy,
			Provider:   provider,
			Metadata:   meta,
		})
	}

	// 4. Score-based quality signal: if scores show clear differentiation.
	if len(scores) >= 2 {
		var bestProvider string
		var bestScore, worstScore float64
		worstScore = 1.0
		for p, s := range scores {
			if s > bestScore {
				bestScore = s
				bestProvider = p
			}
			if s < worstScore {
				worstScore = s
			}
		}
		spread := bestScore - worstScore
		if spread > 0.2 { // meaningful differentiation
			meta, _ := json.Marshal(scores)
			_ = m.StoreConsensusInsight(ctx, ConsensusInsight{
				RepoID:     repoID,
				TaskID:     taskID,
				ThreadID:   threadID,
				Category:   InsightQualitySignal,
				Key:        fmt.Sprintf("quality:task:%s", taskID[:8]),
				Insight:    fmt.Sprintf("Provider quality spread of %.0f%% — %s scored highest (%.0f%%), lowest was %.0f%%.", spread*100, bestProvider, bestScore*100, worstScore*100),
				Confidence: confidence,
				Strategy:   strategy,
				Provider:   bestProvider,
				Metadata:   meta,
			})
		}
	}

	slog.Debug("consensus insights extracted",
		"repo_id", repoID,
		"task_id", taskID,
		"strategy", strategy,
		"provider", provider,
	)
}

// ── HTTP Handlers ───────────────────────────────────────────────────────────

// HandleInsightStore handles POST /internal/swarm/memory/insights.
func (h *Handler) HandleInsightStore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RepoID     string          `json:"repo_id"`
		TaskID     string          `json:"task_id"`
		ThreadID   string          `json:"thread_id"`
		Category   string          `json:"category"`
		Key        string          `json:"key"`
		Insight    string          `json:"insight"`
		Confidence float64         `json:"confidence"`
		Strategy   string          `json:"strategy"`
		Provider   string          `json:"provider"`
		Metadata   json.RawMessage `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.RepoID == "" || body.Category == "" || body.Key == "" {
		http.Error(w, `{"error":"repo_id, category, and key are required"}`, http.StatusBadRequest)
		return
	}
	if h.MemorySvc == nil {
		http.Error(w, `{"error":"memory service not available"}`, http.StatusServiceUnavailable)
		return
	}

	insight := ConsensusInsight{
		RepoID:     body.RepoID,
		TaskID:     body.TaskID,
		ThreadID:   body.ThreadID,
		Category:   InsightCategory(body.Category),
		Key:        body.Key,
		Insight:    body.Insight,
		Confidence: body.Confidence,
		Strategy:   body.Strategy,
		Provider:   body.Provider,
		Metadata:   body.Metadata,
	}

	if err := h.MemorySvc.StoreConsensusInsight(r.Context(), insight); err != nil {
		slog.Error("insight store failed", "error", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"status":"stored"}`))
}

// HandleInsightRecall handles GET /internal/swarm/memory/insights.
func (h *Handler) HandleInsightRecall(w http.ResponseWriter, r *http.Request) {
	repoID := r.URL.Query().Get("repo_id")
	category := r.URL.Query().Get("category")
	limitStr := r.URL.Query().Get("limit")

	if repoID == "" {
		http.Error(w, `{"error":"repo_id is required"}`, http.StatusBadRequest)
		return
	}
	if h.MemorySvc == nil {
		http.Error(w, `{"error":"memory service not available"}`, http.StatusServiceUnavailable)
		return
	}

	limit := 20
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	insights, err := h.MemorySvc.RecallConsensusInsights(r.Context(), repoID, category, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if insights == nil {
		insights = []ConsensusInsight{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"insights": insights,
	})
}

// HandleProviderStats handles GET /internal/swarm/memory/provider-stats.
func (h *Handler) HandleProviderStats(w http.ResponseWriter, r *http.Request) {
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		http.Error(w, `{"error":"repo_id is required"}`, http.StatusBadRequest)
		return
	}
	if h.MemorySvc == nil {
		http.Error(w, `{"error":"memory service not available"}`, http.StatusServiceUnavailable)
		return
	}

	stats, err := h.MemorySvc.GetProviderReliabilityStats(r.Context(), repoID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if stats == nil {
		stats = []ProviderReliabilityStats{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"stats": stats,
	})
}

// HandleInsightRecallPublic handles GET /api/v1/swarm/tasks/{id}/insights.
// User-facing endpoint that returns cross-task insights for the task's repo.
func (h *Handler) HandleInsightRecallPublic(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	repoID := r.URL.Query().Get("repo_id")
	category := r.URL.Query().Get("category")
	limitStr := r.URL.Query().Get("limit")

	// If task_id is provided, look up the repo_id.
	if taskID != "" && repoID == "" {
		tid, err := uuid.Parse(taskID)
		if err == nil && h.TaskMgr != nil {
			task, taskErr := h.TaskMgr.GetTask(r.Context(), tid)
			if taskErr == nil && task != nil {
				repoID = task.RepoID
			}
		}
	}

	if repoID == "" {
		http.Error(w, `{"error":"repo_id or task_id is required"}`, http.StatusBadRequest)
		return
	}
	if h.MemorySvc == nil {
		http.Error(w, `{"error":"memory service not available"}`, http.StatusServiceUnavailable)
		return
	}

	limit := 20
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	insights, err := h.MemorySvc.RecallConsensusInsights(r.Context(), repoID, category, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if insights == nil {
		insights = []ConsensusInsight{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"insights": insights,
		"repo_id":  repoID,
	})
}
