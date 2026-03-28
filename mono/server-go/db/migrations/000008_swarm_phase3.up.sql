-- ==============================================================================
-- 000008_swarm_phase3.up.sql — Scale + Polish additions
-- ==============================================================================

-- ── Add retry and failure tracking to swarm_tasks ───────────────────────────

ALTER TABLE swarm_tasks ADD COLUMN IF NOT EXISTS retry_count     INT DEFAULT 0;
ALTER TABLE swarm_tasks ADD COLUMN IF NOT EXISTS failure_reason  TEXT;

-- Index for task history queries (terminal states, ordered by created_at).
CREATE INDEX IF NOT EXISTS idx_swarm_tasks_history
    ON swarm_tasks(status, created_at DESC)
    WHERE status IN ('completed', 'failed', 'timed_out', 'cancelled');

-- Index for timeout checking (active tasks with a timeout_at).
CREATE INDEX IF NOT EXISTS idx_swarm_tasks_timeout
    ON swarm_tasks(timeout_at)
    WHERE timeout_at IS NOT NULL
      AND status NOT IN ('completed', 'cancelled', 'failed', 'timed_out');

-- ── Partial index for heartbeat monitoring ──────────────────────────────────

CREATE INDEX IF NOT EXISTS idx_swarm_agents_online
    ON swarm_agents(status)
    WHERE status != 'offline';
