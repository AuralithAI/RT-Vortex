-- Observability Dashboard
-- Stores periodic metric snapshots for historical time-series charts,
-- provider cost tracking, and system health scoring.

-- ── Metric Snapshots ────────────────────────────────────────────────────────
-- Periodic snapshots of key swarm metrics for time-series visualisation.
-- One row per snapshot interval (default 60s).
CREATE TABLE IF NOT EXISTS swarm_metrics_snapshots (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Task counters at snapshot time
    active_tasks    INT         NOT NULL DEFAULT 0,
    pending_tasks   INT         NOT NULL DEFAULT 0,
    completed_tasks BIGINT      NOT NULL DEFAULT 0,
    failed_tasks    BIGINT      NOT NULL DEFAULT 0,
    -- Agent / team gauges
    online_agents   INT         NOT NULL DEFAULT 0,
    busy_agents     INT         NOT NULL DEFAULT 0,
    active_teams    INT         NOT NULL DEFAULT 0,
    busy_teams      INT         NOT NULL DEFAULT 0,
    -- LLM usage
    llm_calls       BIGINT      NOT NULL DEFAULT 0,
    llm_tokens      BIGINT      NOT NULL DEFAULT 0,
    llm_avg_latency_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
    llm_error_rate  DOUBLE PRECISION NOT NULL DEFAULT 0,
    -- Probe / consensus
    probe_calls     BIGINT      NOT NULL DEFAULT 0,
    consensus_runs  BIGINT      NOT NULL DEFAULT 0,
    consensus_avg_confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    -- Self-heal gauges
    open_circuits   INT         NOT NULL DEFAULT 0,
    heal_events     INT         NOT NULL DEFAULT 0,
    -- Cost tracking (estimated USD)
    estimated_cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    -- Queue depth / utilisation
    queue_depth     INT         NOT NULL DEFAULT 0,
    agent_utilisation DOUBLE PRECISION NOT NULL DEFAULT 0,
    -- System health score (0-100)
    health_score    INT         NOT NULL DEFAULT 100,
    -- Timestamp
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_metrics_snap_created ON swarm_metrics_snapshots (created_at DESC);

-- ── Provider Performance Log ────────────────────────────────────────────────
-- Aggregated per-provider performance stats, updated every snapshot cycle.
CREATE TABLE IF NOT EXISTS swarm_provider_perf_log (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    provider        TEXT        NOT NULL,
    -- Counters since last snapshot
    calls           INT         NOT NULL DEFAULT 0,
    successes       INT         NOT NULL DEFAULT 0,
    failures        INT         NOT NULL DEFAULT 0,
    tokens_used     BIGINT      NOT NULL DEFAULT 0,
    avg_latency_ms  DOUBLE PRECISION NOT NULL DEFAULT 0,
    p95_latency_ms  DOUBLE PRECISION NOT NULL DEFAULT 0,
    p99_latency_ms  DOUBLE PRECISION NOT NULL DEFAULT 0,
    error_rate      DOUBLE PRECISION NOT NULL DEFAULT 0,
    estimated_cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    -- Consensus wins during this period
    consensus_wins  INT         NOT NULL DEFAULT 0,
    consensus_total INT         NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_provider_perf_created  ON swarm_provider_perf_log (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_provider_perf_provider ON swarm_provider_perf_log (provider, created_at DESC);

-- ── Cost Budget ─────────────────────────────────────────────────────────────
-- Optional monthly cost budget per organisation / global.
CREATE TABLE IF NOT EXISTS swarm_cost_budget (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    scope           TEXT        NOT NULL DEFAULT 'global',  -- global | org:<uuid>
    month           DATE        NOT NULL,                   -- first day of month
    budget_usd      DOUBLE PRECISION NOT NULL DEFAULT 0,
    spent_usd       DOUBLE PRECISION NOT NULL DEFAULT 0,
    alert_threshold DOUBLE PRECISION NOT NULL DEFAULT 0.8,  -- fraction (0-1)
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(scope, month)
);

CREATE INDEX IF NOT EXISTS idx_cost_budget_scope ON swarm_cost_budget (scope, month);
