-- ==============================================================================
-- 000007_swarm_tables.up.sql — Vortex Agent Swarm schema
-- ==============================================================================

-- ── Agents ──────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS swarm_agents (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role          TEXT NOT NULL,
    team_id       UUID,
    status        TEXT DEFAULT 'offline',   -- offline, idle, busy, errored
    elo_score     FLOAT DEFAULT 1200,
    tasks_done    INT DEFAULT 0,
    tasks_rated   INT DEFAULT 0,
    avg_rating    FLOAT DEFAULT 0,
    hostname      TEXT,
    version       TEXT,
    registered_at TIMESTAMPTZ DEFAULT NOW()
);

-- ── Teams ───────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS swarm_teams (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT,
    lead_agent_id  UUID REFERENCES swarm_agents(id) ON DELETE SET NULL,
    status         TEXT DEFAULT 'idle',   -- idle, busy, offline
    agent_ids      UUID[],               -- current active members
    formed_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Back-reference: agent → team
DO $$ BEGIN
    ALTER TABLE swarm_agents
        ADD CONSTRAINT fk_swarm_agents_team
        FOREIGN KEY (team_id) REFERENCES swarm_teams(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- ── Tasks ───────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS swarm_tasks (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id           TEXT NOT NULL,
    description       TEXT NOT NULL,
    status            TEXT DEFAULT 'submitted',
    plan_document     JSONB,
    assigned_team_id  UUID REFERENCES swarm_teams(id) ON DELETE SET NULL,
    assigned_agents   UUID[],
    pr_url            TEXT,
    pr_number         INT,
    human_rating      INT,
    human_comment     TEXT,
    submitted_by      UUID,
    created_at        TIMESTAMPTZ DEFAULT NOW(),
    completed_at      TIMESTAMPTZ,
    timeout_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_swarm_tasks_repo_id ON swarm_tasks(repo_id);
CREATE INDEX IF NOT EXISTS idx_swarm_tasks_status  ON swarm_tasks(status);

-- ── Task Diffs ──────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS swarm_task_diffs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id       UUID REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    file_path     TEXT NOT NULL,
    change_type   TEXT,
    original      TEXT,
    proposed      TEXT,
    unified_diff  TEXT,
    agent_id      UUID REFERENCES swarm_agents(id) ON DELETE SET NULL,
    status        TEXT DEFAULT 'pending',
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_swarm_task_diffs_task ON swarm_task_diffs(task_id);

-- ── Diff Comments ───────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS swarm_diff_comments (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    diff_id       UUID REFERENCES swarm_task_diffs(id) ON DELETE CASCADE,
    author_type   TEXT,
    author_id     TEXT,
    line_number   INT,
    content       TEXT,
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_swarm_diff_comments_diff ON swarm_diff_comments(diff_id);

-- ── Agent Feedback ──────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS agent_feedback (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id       UUID REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    agent_id      UUID REFERENCES swarm_agents(id) ON DELETE SET NULL,
    rating        INT CHECK (rating BETWEEN 1 AND 5),
    comment       TEXT,
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

-- ── Agent Task Log ──────────────────────────────────────────────────────────

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
