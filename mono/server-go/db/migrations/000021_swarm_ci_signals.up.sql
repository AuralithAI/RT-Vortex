-- ─── Automatic CI Signal Ingestion ────────────────────────────────
-- Tracks per-task CI signals (PR merge state, build/test CI checks).
-- The background CISignalPoller queries completed tasks with PRs and fills
-- this table, then feeds the signals into the role-based ELO system.

CREATE TABLE IF NOT EXISTS swarm_ci_signals (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID NOT NULL REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    repo_id         TEXT NOT NULL,

    -- PR merge state
    pr_number       INT,
    pr_state        TEXT NOT NULL DEFAULT 'unknown',      -- open, merged, closed, unknown
    pr_merged       BOOLEAN NOT NULL DEFAULT FALSE,

    -- CI / commit status
    ci_state        TEXT NOT NULL DEFAULT 'unknown',      -- pending, success, failure, error, unknown
    ci_total        INT NOT NULL DEFAULT 0,
    ci_passed       INT NOT NULL DEFAULT 0,
    ci_failed       INT NOT NULL DEFAULT 0,
    ci_pending      INT NOT NULL DEFAULT 0,
    ci_details      JSONB DEFAULT '[]'::jsonb,            -- array of individual checks

    -- Whether signals have been ingested into role ELO
    elo_ingested    BOOLEAN NOT NULL DEFAULT FALSE,
    elo_ingested_at TIMESTAMPTZ,

    -- Polling metadata
    poll_count      INT NOT NULL DEFAULT 0,
    last_polled_at  TIMESTAMPTZ,
    finalized       BOOLEAN NOT NULL DEFAULT FALSE,       -- no more polling needed
    finalized_at    TIMESTAMPTZ,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One CI signal record per task.
CREATE UNIQUE INDEX IF NOT EXISTS idx_swarm_ci_signals_task
    ON swarm_ci_signals (task_id);

-- For the poller: find tasks that still need polling.
CREATE INDEX IF NOT EXISTS idx_swarm_ci_signals_unfinalized
    ON swarm_ci_signals (finalized, last_polled_at)
    WHERE NOT finalized;

-- For querying by repo.
CREATE INDEX IF NOT EXISTS idx_swarm_ci_signals_repo
    ON swarm_ci_signals (repo_id, created_at DESC);

-- Update schema_migrations version.
INSERT INTO schema_migrations (version, dirty)
VALUES (21, false)
ON CONFLICT (version) DO NOTHING;
