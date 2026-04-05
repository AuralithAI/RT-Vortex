-- Cross-task consensus insights — persistent memory from multi-LLM decisions.

CREATE TABLE IF NOT EXISTS swarm_consensus_insights (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id     TEXT NOT NULL,
    task_id     TEXT NOT NULL DEFAULT '',
    thread_id   TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL,
    key         TEXT NOT NULL,
    insight     TEXT NOT NULL,
    confidence  DOUBLE PRECISION NOT NULL DEFAULT 0.8,
    strategy    TEXT NOT NULL DEFAULT '',
    provider    TEXT NOT NULL DEFAULT '',
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repo_id, category, key)
);

CREATE INDEX idx_consensus_insights_repo_cat
    ON swarm_consensus_insights (repo_id, category);

CREATE INDEX idx_consensus_insights_updated
    ON swarm_consensus_insights (updated_at);

CREATE INDEX idx_consensus_insights_provider
    ON swarm_consensus_insights (provider)
    WHERE provider != '';
