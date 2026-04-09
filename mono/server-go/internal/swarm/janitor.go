package swarm

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Background cleanup goroutine: kills idle teams, clears stale heartbeats,
// prunes old MTM entries, cleans orphaned STM keys, recycles offline agents.

const (
	JanitorInterval        = 60 * time.Second
	IdleTeamTimeout        = 10 * time.Minute
	OfflineAgentRecycleAge = 1 * time.Hour
	MTMMaxAge              = 7 * 24 * time.Hour  // 7 days
	InsightMaxAge          = 30 * 24 * time.Hour // 30 days
	STMKeyPattern          = "swarm:stm:*"
)

// Janitor runs periodic cleanup tasks for the swarm infrastructure.
type Janitor struct {
	db        *pgxpool.Pool
	redis     *redis.Client
	memorySvc *MemoryService
}

// NewJanitor creates a new background janitor.
func NewJanitor(db *pgxpool.Pool, redis *redis.Client, memorySvc *MemoryService) *Janitor {
	return &Janitor{
		db:        db,
		redis:     redis,
		memorySvc: memorySvc,
	}
}

// Start begins the periodic cleanup loop. Call in a goroutine.
func (j *Janitor) Start(ctx context.Context) {
	slog.Info("swarm janitor started", "interval", JanitorInterval)
	ticker := time.NewTicker(JanitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("swarm janitor stopped")
			return
		case <-ticker.C:
			j.runCycle(ctx)
		}
	}
}

func (j *Janitor) runCycle(ctx context.Context) {
	start := time.Now()

	idleTeams := j.killIdleTeams(ctx)
	staleHBKeys := j.clearStaleHeartbeats(ctx)
	prunedMTM := j.pruneStaleMTM(ctx)
	prunedInsights := j.pruneStaleInsights(ctx)
	cleanedSTM := j.cleanOrphanedSTM(ctx)
	recycledAgents := j.recycleOfflineAgents(ctx)

	duration := time.Since(start)

	total := idleTeams + staleHBKeys + prunedMTM + prunedInsights + cleanedSTM + recycledAgents
	if total > 0 {
		slog.Info("swarm janitor cycle complete",
			"duration_ms", duration.Milliseconds(),
			"idle_teams_killed", idleTeams,
			"stale_hb_keys", staleHBKeys,
			"pruned_mtm", prunedMTM,
			"pruned_insights", prunedInsights,
			"cleaned_stm", cleanedSTM,
			"recycled_agents", recycledAgents,
		)
	}
}

// killIdleTeams disbands teams that have been idle for too long.
func (j *Janitor) killIdleTeams(ctx context.Context) int64 {
	cutoff := time.Now().UTC().Add(-IdleTeamTimeout)

	// Find idle teams that are not warm-pool teams and have no active task.
	tag, err := j.db.Exec(ctx, `
		UPDATE swarm_teams SET status = 'offline'
		WHERE status = 'idle'
		  AND id NOT IN ($1::uuid, $2::uuid)
		  AND NOT EXISTS (
			SELECT 1 FROM swarm_tasks
			WHERE assigned_team_id = swarm_teams.id
			  AND status NOT IN ('completed', 'cancelled', 'failed', 'timed_out')
		  )
		  AND formed_at < $3`,
		warmPoolTeamIDs[0], warmPoolTeamIDs[1], cutoff,
	)
	if err != nil {
		slog.Warn("janitor: kill idle teams failed", "error", err)
		return 0
	}
	return tag.RowsAffected()
}

// clearStaleHeartbeats removes Redis heartbeat keys for agents marked offline.
func (j *Janitor) clearStaleHeartbeats(ctx context.Context) int64 {
	if j.redis == nil {
		return 0
	}

	// Find offline agent IDs.
	rows, err := j.db.Query(ctx, `
		SELECT id FROM swarm_agents WHERE status = 'offline'`)
	if err != nil {
		return 0
	}
	defer rows.Close()

	var cleaned int64
	for rows.Next() {
		var agentID string
		if rows.Scan(&agentID) != nil {
			continue
		}
		key := fmt.Sprintf("swarm:agent:heartbeat:%s", agentID)
		deleted, err := j.redis.Del(ctx, key).Result()
		if err == nil {
			cleaned += deleted
		}
	}
	return cleaned
}

// pruneStaleMTM removes MTM entries older than 7 days.
func (j *Janitor) pruneStaleMTM(ctx context.Context) int64 {
	if j.memorySvc == nil {
		return 0
	}
	count, err := j.memorySvc.PruneStaleMTM(ctx, MTMMaxAge)
	if err != nil {
		slog.Warn("janitor: prune MTM failed", "error", err)
		return 0
	}
	return count
}

// pruneStaleInsights removes consensus insight entries older than 30 days.
func (j *Janitor) pruneStaleInsights(ctx context.Context) int64 {
	if j.memorySvc == nil {
		return 0
	}
	count, err := j.memorySvc.PruneStaleInsights(ctx, InsightMaxAge)
	if err != nil {
		slog.Warn("janitor: prune insights failed", "error", err)
		return 0
	}
	return count
}

// cleanOrphanedSTM removes stale swarm:stm:* keys from Redis.
// STM keys have a 30-min TTL, but if the Python process crashes mid-task
// they might linger. We clean up any that don't match an active task.
func (j *Janitor) cleanOrphanedSTM(ctx context.Context) int64 {
	if j.redis == nil {
		return 0
	}

	// Get active task IDs.
	rows, err := j.db.Query(ctx, `
		SELECT id::text FROM swarm_tasks
		WHERE status NOT IN ('completed', 'cancelled', 'failed', 'timed_out')`)
	if err != nil {
		return 0
	}
	defer rows.Close()

	activeTaskIDs := map[string]bool{}
	for rows.Next() {
		var tid string
		if rows.Scan(&tid) == nil {
			activeTaskIDs[tid] = true
		}
	}

	// Scan for STM keys and check if their task is still active.
	var cleaned int64
	cursor := uint64(0)
	for {
		keys, nextCursor, err := j.redis.Scan(ctx, cursor, STMKeyPattern, 100).Result()
		if err != nil {
			break
		}
		for _, key := range keys {
			// Key format: swarm:stm:{task_id}:{agent_id}:*
			// Extract task_id (3rd segment).
			parts := splitKey(key)
			if len(parts) >= 3 {
				taskID := parts[2]
				if !activeTaskIDs[taskID] {
					j.redis.Del(ctx, key)
					cleaned++
				}
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return cleaned
}

// recycleOfflineAgents deletes agents that have been offline for > 1 hour.
func (j *Janitor) recycleOfflineAgents(ctx context.Context) int64 {
	cutoff := time.Now().UTC().Add(-OfflineAgentRecycleAge)
	tag, err := j.db.Exec(ctx, `
		DELETE FROM swarm_agents
		WHERE status = 'offline' AND registered_at < $1`, cutoff)
	if err != nil {
		slog.Warn("janitor: recycle offline agents failed", "error", err)
		return 0
	}
	return tag.RowsAffected()
}

// splitKey splits a Redis key by ":".
func splitKey(key string) []string {
	parts := []string{}
	start := 0
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			parts = append(parts, key[start:i])
			start = i + 1
		}
	}
	if start < len(key) {
		parts = append(parts, key[start:])
	}
	return parts
}
