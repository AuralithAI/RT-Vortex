-- ── Benchmark tables ────────────────────────────────────────────────
-- Stores benchmark runs and results for the A/B testing harness.

CREATE TABLE IF NOT EXISTS benchmark_runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    mode          TEXT NOT NULL CHECK (mode IN ('swarm', 'single_agent', 'both')),
    status        TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed')),
    started_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at      TIMESTAMPTZ,
    summary_json  JSONB,
    created_by    UUID REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS benchmark_results (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id        UUID NOT NULL REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    task_id       TEXT NOT NULL,
    task_name     TEXT NOT NULL,
    mode          TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    started_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at      TIMESTAMPTZ,
    latency_ms    BIGINT DEFAULT 0,
    llm_calls     INTEGER DEFAULT 0,
    tokens_used   INTEGER DEFAULT 0,
    comments_json JSONB,
    score_json    JSONB,
    trace_json    JSONB,
    error_msg     TEXT
);

CREATE INDEX idx_benchmark_results_run ON benchmark_results(run_id);
CREATE INDEX idx_benchmark_results_task ON benchmark_results(task_id);

CREATE TABLE IF NOT EXISTS benchmark_elo_ratings (
    mode          TEXT PRIMARY KEY,
    rating        DOUBLE PRECISION NOT NULL DEFAULT 1500,
    wins          INTEGER NOT NULL DEFAULT 0,
    losses        INTEGER NOT NULL DEFAULT 0,
    draws         INTEGER NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed initial ELO ratings
INSERT INTO benchmark_elo_ratings (mode, rating) VALUES
    ('swarm', 1500),
    ('single_agent', 1500)
ON CONFLICT (mode) DO NOTHING;
