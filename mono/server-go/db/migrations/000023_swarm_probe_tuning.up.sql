-- Adaptive Probe Tuning
-- Stores per-(role, repo, action_type) probe configurations and a rolling
-- history of probe outcomes so the adaptive tuning engine can learn.

-- ── Probe Configurations ────────────────────────────────────────────────────
-- One row per (role, repo_id, action_type) triple.  The tuning engine updates
-- these periodically based on probe_history analysis.
CREATE TABLE IF NOT EXISTS swarm_probe_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role            TEXT        NOT NULL,
    repo_id         TEXT        NOT NULL DEFAULT '',       -- '' = global default
    action_type     TEXT        NOT NULL DEFAULT '',       -- '' = any action
    num_models      INT         NOT NULL DEFAULT 3,        -- how many providers to probe
    preferred_providers TEXT[]  NOT NULL DEFAULT '{}',     -- ordered preference
    excluded_providers  TEXT[]  NOT NULL DEFAULT '{}',     -- providers to skip
    temperature     FLOAT       NOT NULL DEFAULT 0.7,      -- LLM temperature
    max_tokens      INT         NOT NULL DEFAULT 4096,
    timeout_seconds INT         NOT NULL DEFAULT 120,
    budget_cap_usd  FLOAT       NOT NULL DEFAULT 0.0,      -- 0 = unlimited
    tokens_spent    BIGINT      NOT NULL DEFAULT 0,
    strategy        TEXT        NOT NULL DEFAULT 'adaptive', -- adaptive | static | aggressive
    confidence_threshold FLOAT  NOT NULL DEFAULT 0.7,      -- min consensus confidence to stop early
    retries         INT         NOT NULL DEFAULT 1,        -- retry on failure
    reasoning       TEXT        NOT NULL DEFAULT '',        -- why this config was chosen
    last_tuned_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (role, repo_id, action_type)
);

-- ── Probe Outcome History ───────────────────────────────────────────────────
-- Append-only log of every multi-LLM probe outcome, used by the adaptive
-- tuning engine to adjust configs.
CREATE TABLE IF NOT EXISTS swarm_probe_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID        NOT NULL,
    role            TEXT        NOT NULL,
    repo_id         TEXT        NOT NULL DEFAULT '',
    action_type     TEXT        NOT NULL DEFAULT '',
    -- Probe details
    providers_queried   TEXT[]  NOT NULL DEFAULT '{}',
    providers_succeeded TEXT[]  NOT NULL DEFAULT '{}',
    provider_winner     TEXT   NOT NULL DEFAULT '',       -- consensus winner
    strategy_used       TEXT   NOT NULL DEFAULT '',       -- consensus strategy that ran
    consensus_confidence FLOAT NOT NULL DEFAULT 0.0,
    -- Per-provider latencies (JSON: {"grok": 1200, "anthropic": 3400, ...})
    provider_latencies  JSONB  NOT NULL DEFAULT '{}',
    -- Per-provider token usage (JSON: {"grok": {"prompt": 500, "completion": 200}, ...})
    provider_tokens     JSONB  NOT NULL DEFAULT '{}',
    -- Outcome
    total_ms            INT    NOT NULL DEFAULT 0,
    total_tokens        INT    NOT NULL DEFAULT 0,
    estimated_cost_usd  FLOAT  NOT NULL DEFAULT 0.0,
    success             BOOLEAN NOT NULL DEFAULT TRUE,
    error_detail        TEXT   NOT NULL DEFAULT '',
    -- Context
    complexity_label    TEXT   NOT NULL DEFAULT '',       -- from team formation
    num_models_used     INT    NOT NULL DEFAULT 0,
    temperature_used    FLOAT  NOT NULL DEFAULT 0.7,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for efficient querying by the tuning engine.
CREATE INDEX IF NOT EXISTS idx_probe_configs_role_repo
    ON swarm_probe_configs (role, repo_id, action_type);

CREATE INDEX IF NOT EXISTS idx_probe_history_role_repo
    ON swarm_probe_history (role, repo_id, action_type);

CREATE INDEX IF NOT EXISTS idx_probe_history_task
    ON swarm_probe_history (task_id);

CREATE INDEX IF NOT EXISTS idx_probe_history_created
    ON swarm_probe_history (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_probe_history_provider_winner
    ON swarm_probe_history (provider_winner)
    WHERE provider_winner != '';

-- Track schema version.
INSERT INTO schema_migrations (version, dirty) VALUES (23, false)
    ON CONFLICT (version) DO NOTHING;
