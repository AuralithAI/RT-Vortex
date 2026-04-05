-- ==============================================================================
-- 000020_swarm_role_elo.up.sql — Role-Based ELO 
--
-- ELO tracking moves from ephemeral agent UUIDs to persistent (role, repo_id)
-- pairs. When a new agent registers for a role+repo it inherits the
-- accumulated score. Performance data survives agent lifecycles.
-- ==============================================================================

-- ── Role ELO Table ──────────────────────────────────────────────────────────
--
-- One row per (role, repo_id). Unique constraint ensures a role's performance
-- in a specific repository is tracked as a single continuous record.

CREATE TABLE IF NOT EXISTS swarm_role_elo (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role            TEXT NOT NULL,
    repo_id         TEXT NOT NULL,
    elo_score       DOUBLE PRECISION NOT NULL DEFAULT 1200,
    tier            TEXT NOT NULL DEFAULT 'standard',  -- standard, expert, restricted
    tasks_done      INT NOT NULL DEFAULT 0,
    tasks_rated     INT NOT NULL DEFAULT 0,
    avg_rating      DOUBLE PRECISION NOT NULL DEFAULT 0,
    wins            INT NOT NULL DEFAULT 0,             -- consensus wins
    losses          INT NOT NULL DEFAULT 0,             -- consensus losses
    consensus_avg   DOUBLE PRECISION NOT NULL DEFAULT 0, -- avg consensus confidence
    best_strategy   TEXT NOT NULL DEFAULT '',            -- most effective strategy for this role+repo
    training_probes INT NOT NULL DEFAULT 0,             -- extra training probes issued
    last_active     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_role_elo_role_repo UNIQUE (role, repo_id)
);

CREATE INDEX IF NOT EXISTS idx_role_elo_role     ON swarm_role_elo (role);
CREATE INDEX IF NOT EXISTS idx_role_elo_repo     ON swarm_role_elo (repo_id);
CREATE INDEX IF NOT EXISTS idx_role_elo_tier     ON swarm_role_elo (tier);
CREATE INDEX IF NOT EXISTS idx_role_elo_score    ON swarm_role_elo (elo_score DESC);
CREATE INDEX IF NOT EXISTS idx_role_elo_active   ON swarm_role_elo (last_active);

-- ── Role ELO History ────────────────────────────────────────────────────────
--
-- Append-only log of every ELO change for audit, charting, and debugging.

CREATE TABLE IF NOT EXISTS swarm_role_elo_history (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role        TEXT NOT NULL,
    repo_id     TEXT NOT NULL,
    task_id     TEXT NOT NULL DEFAULT '',
    event_type  TEXT NOT NULL,   -- 'rating', 'consensus', 'auto_metric', 'decay', 'training_probe'
    old_elo     DOUBLE PRECISION NOT NULL,
    new_elo     DOUBLE PRECISION NOT NULL,
    delta       DOUBLE PRECISION NOT NULL,
    detail      JSONB,           -- structured event details (rating, confidence, strategy, etc.)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_role_elo_hist_role_repo ON swarm_role_elo_history (role, repo_id);
CREATE INDEX IF NOT EXISTS idx_role_elo_hist_created   ON swarm_role_elo_history (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_role_elo_hist_event     ON swarm_role_elo_history (event_type);
