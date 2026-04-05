-- Self-Healing Pipeline
-- Tracks provider circuit-breaker state and self-healing events (auto-retries,
-- stuck-task recovery, provider failovers, and circuit-breaker transitions).

-- ── Provider Circuit State ──────────────────────────────────────────────────
-- One row per LLM provider.  Updated by the SelfHealService goroutine.
CREATE TABLE IF NOT EXISTS swarm_provider_circuit_state (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    provider        TEXT        NOT NULL UNIQUE,
    state           TEXT        NOT NULL DEFAULT 'closed',  -- closed | half_open | open
    consecutive_failures INT   NOT NULL DEFAULT 0,
    total_failures  BIGINT     NOT NULL DEFAULT 0,
    total_successes BIGINT     NOT NULL DEFAULT 0,
    last_failure_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    open_until      TIMESTAMPTZ,
    half_open_probes INT       NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_provider_circuit_provider ON swarm_provider_circuit_state (provider);
CREATE INDEX IF NOT EXISTS idx_provider_circuit_state    ON swarm_provider_circuit_state (state);

-- ── Self-Heal Events ────────────────────────────────────────────────────────
-- Audit log for every self-healing action the system takes.
CREATE TABLE IF NOT EXISTS swarm_self_heal_events (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type      TEXT        NOT NULL,  -- circuit_opened | circuit_closed | circuit_half_open |
                                           -- task_retry | task_timeout_recovery | provider_failover |
                                           -- agent_restarted | stuck_task_detected
    provider        TEXT,
    task_id         UUID,
    agent_id        UUID,
    details         JSONB       NOT NULL DEFAULT '{}',
    severity        TEXT        NOT NULL DEFAULT 'info',  -- info | warn | critical
    resolved        BOOLEAN     NOT NULL DEFAULT false,
    resolved_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_self_heal_type      ON swarm_self_heal_events (event_type);
CREATE INDEX IF NOT EXISTS idx_self_heal_provider  ON swarm_self_heal_events (provider);
CREATE INDEX IF NOT EXISTS idx_self_heal_task      ON swarm_self_heal_events (task_id);
CREATE INDEX IF NOT EXISTS idx_self_heal_severity  ON swarm_self_heal_events (severity);
CREATE INDEX IF NOT EXISTS idx_self_heal_created   ON swarm_self_heal_events (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_self_heal_unresolved ON swarm_self_heal_events (resolved) WHERE resolved = false;
