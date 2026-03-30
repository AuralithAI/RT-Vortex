-- Agent memory (MTM), HITL audit log, tier column, feedback and task log tables.

CREATE TABLE IF NOT EXISTS swarm_agent_memory (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id     TEXT NOT NULL,
    agent_role  TEXT NOT NULL,
    key         TEXT NOT NULL,
    insight     TEXT NOT NULL,
    confidence  DOUBLE PRECISION NOT NULL DEFAULT 0.8,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repo_id, agent_role, key)
);

CREATE INDEX idx_swarm_memory_repo_role ON swarm_agent_memory (repo_id, agent_role);
CREATE INDEX idx_swarm_memory_updated   ON swarm_agent_memory (updated_at);

ALTER TABLE swarm_agents ADD COLUMN IF NOT EXISTS tier TEXT NOT NULL DEFAULT 'standard';

CREATE TABLE IF NOT EXISTS swarm_hitl_log (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id      TEXT NOT NULL,
    agent_id     TEXT NOT NULL,
    agent_role   TEXT NOT NULL DEFAULT '',
    question     TEXT NOT NULL,
    context      TEXT NOT NULL DEFAULT '',
    urgency      TEXT NOT NULL DEFAULT 'normal',
    response     TEXT,
    asked_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    responded_at TIMESTAMPTZ
);

CREATE INDEX idx_hitl_log_task ON swarm_hitl_log (task_id);

CREATE TABLE IF NOT EXISTS agent_feedback (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id       UUID REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    agent_id      UUID REFERENCES swarm_agents(id) ON DELETE SET NULL,
    rating        INT CHECK (rating BETWEEN 1 AND 5),
    comment       TEXT,
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS agent_task_log (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id            UUID REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    agent_id           UUID REFERENCES swarm_agents(id) ON DELETE SET NULL,
    role               TEXT,
    phase              TEXT,
    contribution_type  TEXT,
    tokens_used        INT DEFAULT 0,
    llm_calls          INT DEFAULT 0,
    rag_calls          INT DEFAULT 0,
    started_at         TIMESTAMPTZ,
    finished_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agent_task_log_task  ON agent_task_log(task_id);
CREATE INDEX IF NOT EXISTS idx_agent_task_log_agent ON agent_task_log(agent_id);
