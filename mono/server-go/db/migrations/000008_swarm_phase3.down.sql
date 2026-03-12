-- ==============================================================================
-- 000008_swarm_phase3.down.sql — Rollback Phase 3 additions
-- ==============================================================================

DROP INDEX IF EXISTS idx_swarm_agents_online;
DROP INDEX IF EXISTS idx_swarm_tasks_timeout;
DROP INDEX IF EXISTS idx_swarm_tasks_history;

ALTER TABLE swarm_tasks DROP COLUMN IF EXISTS failure_reason;
ALTER TABLE swarm_tasks DROP COLUMN IF EXISTS retry_count;
