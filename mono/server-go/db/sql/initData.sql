-- ============================================================================
-- RTVortex Database Schema — Step 2: Initialize Tables
-- ============================================================================
-- Run this as the 'rtvortex' user on the 'rtvortex' database:
--
--   psql -U rtvortex -d rtvortex -f initData.sql
--
-- Prerequisites:
--   - PostgreSQL 15+
--   - create_database.sql has been run by the postgres superuser
--
-- This script is idempotent (safe to re-run — uses IF NOT EXISTS).
-- ============================================================================

-- Enable extensions (requires superuser to have already allowed them,
-- or the rtvortex role to have CREATEDB privilege)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";      -- trigram similarity for text search
CREATE EXTENSION IF NOT EXISTS "pgcrypto";      -- encryption functions

-- ============================================================================
-- USERS
-- ============================================================================

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    display_name VARCHAR(255) NOT NULL,
    avatar_url TEXT,
    vault_token VARCHAR(64) NOT NULL DEFAULT encode(gen_random_bytes(32), 'hex'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_vault_token ON users(vault_token);

-- ============================================================================
-- OAUTH IDENTITIES
-- ============================================================================
-- A user can have multiple OAuth identities (login via Google + link GitHub)

CREATE TABLE IF NOT EXISTS oauth_identities (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(50) NOT NULL,               -- github, google, microsoft, linkedin, gitlab, bitbucket
    provider_user_id VARCHAR(255) NOT NULL,
    access_token_enc TEXT,                        -- AES-256-GCM encrypted
    refresh_token_enc TEXT,                       -- AES-256-GCM encrypted
    scopes TEXT,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(provider, provider_user_id)
);

CREATE INDEX IF NOT EXISTS idx_oauth_user_id ON oauth_identities(user_id);
CREATE INDEX IF NOT EXISTS idx_oauth_provider ON oauth_identities(provider, provider_user_id);

-- ============================================================================
-- ORGANIZATIONS
-- ============================================================================

CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(100) NOT NULL UNIQUE,
    plan VARCHAR(50) NOT NULL DEFAULT 'free',     -- free, pro, enterprise
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_organizations_slug ON organizations(slug);

-- ============================================================================
-- ORGANIZATION MEMBERS
-- ============================================================================

CREATE TABLE IF NOT EXISTS org_members (
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(50) NOT NULL DEFAULT 'member',   -- owner, admin, member, viewer
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_org_members_user ON org_members(user_id);

-- ============================================================================
-- REPOSITORIES
-- ============================================================================

CREATE TABLE IF NOT EXISTS repositories (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    platform VARCHAR(50) NOT NULL,                -- github, gitlab, bitbucket, azure_devops
    external_id VARCHAR(255) NOT NULL,
    owner VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    default_branch VARCHAR(100) DEFAULT 'main',
    clone_url TEXT,
    webhook_secret VARCHAR(255),
    config JSONB NOT NULL DEFAULT '{}',
    indexed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(platform, external_id)
);

CREATE INDEX IF NOT EXISTS idx_repositories_org ON repositories(org_id);
CREATE INDEX IF NOT EXISTS idx_repositories_platform ON repositories(platform);

-- ============================================================================
-- REPOSITORY MEMBERS (fine-grained access control)
-- ============================================================================

CREATE TABLE IF NOT EXISTS repo_members (
    repo_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(50) NOT NULL DEFAULT 'viewer',    -- admin, reviewer, viewer
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (repo_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_repo_members_user ON repo_members(user_id);
CREATE INDEX IF NOT EXISTS idx_repo_members_repo ON repo_members(repo_id);

-- ============================================================================
-- REVIEWS
-- ============================================================================

CREATE TABLE IF NOT EXISTS reviews (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repo_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    triggered_by UUID REFERENCES users(id),
    platform VARCHAR(50) NOT NULL,
    pr_number INTEGER NOT NULL,
    pr_title TEXT,
    pr_author VARCHAR(255),
    base_branch VARCHAR(255),
    head_branch VARCHAR(255),
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- pending, in_progress, completed, failed, cancelled
    files_changed INTEGER DEFAULT 0,
    additions INTEGER DEFAULT 0,
    deletions INTEGER DEFAULT 0,
    metadata JSONB DEFAULT '{}',                    -- timing, LLM model, tokens used, etc.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_reviews_repo ON reviews(repo_id);
CREATE INDEX IF NOT EXISTS idx_reviews_status ON reviews(status);
CREATE INDEX IF NOT EXISTS idx_reviews_created ON reviews(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reviews_repo_pr ON reviews(repo_id, pr_number);

-- ============================================================================
-- REVIEW COMMENTS
-- ============================================================================

CREATE TABLE IF NOT EXISTS review_comments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    review_id UUID NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
    file_path TEXT NOT NULL,
    line_number INTEGER,
    end_line INTEGER,
    severity VARCHAR(20) NOT NULL DEFAULT 'info',  -- critical, high, medium, low, info
    category VARCHAR(50),                           -- security, performance, style, bug, logic, etc.
    title TEXT,
    body TEXT NOT NULL,
    suggestion TEXT,                                -- suggested code fix
    posted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_review_comments_review ON review_comments(review_id);
CREATE INDEX IF NOT EXISTS idx_review_comments_severity ON review_comments(severity);

-- ============================================================================
-- USAGE / BILLING
-- ============================================================================

CREATE TABLE IF NOT EXISTS usage_daily (
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    date DATE NOT NULL,
    reviews_count INTEGER NOT NULL DEFAULT 0,
    tokens_used BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (org_id, date)
);

-- ============================================================================
-- WEBHOOK EVENTS (audit log)
-- ============================================================================

CREATE TABLE IF NOT EXISTS webhook_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    platform VARCHAR(50) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    repo_id UUID REFERENCES repositories(id),
    payload JSONB,
    processed BOOLEAN NOT NULL DEFAULT FALSE,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_events_created ON webhook_events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_webhook_events_repo ON webhook_events(repo_id);

-- ============================================================================
-- AUDIT LOG
-- ============================================================================
-- Tracks security-relevant events: logins, config changes, review triggers, etc.

CREATE TABLE IF NOT EXISTS audit_log (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action VARCHAR(100) NOT NULL,                  -- login, logout, review.trigger, repo.create, repo.delete, org.create, admin.config_change, etc.
    resource_type VARCHAR(100),                    -- user, repo, review, org, webhook, system
    resource_id VARCHAR(255),                      -- UUID or other identifier of the affected resource
    ip_address INET,
    user_agent TEXT,
    metadata JSONB DEFAULT '{}',                   -- additional context (provider, old_value, new_value, etc.)
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_user ON audit_log(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_action ON audit_log(action);
CREATE INDEX IF NOT EXISTS idx_audit_log_resource ON audit_log(resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at DESC);

-- ============================================================================
-- TRACKED PULL REQUESTS
-- ============================================================================
-- Stores PRs discovered from connected VCS platforms for background syncing,
-- pre-embedding via the C++ engine, and one-click review triggering.

CREATE TABLE IF NOT EXISTS tracked_pull_requests (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repo_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    platform VARCHAR(50) NOT NULL,
    pr_number INTEGER NOT NULL,
    external_id VARCHAR(255) NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    description TEXT DEFAULT '',
    author VARCHAR(255) NOT NULL DEFAULT '',
    source_branch VARCHAR(255) NOT NULL DEFAULT '',
    target_branch VARCHAR(255) NOT NULL DEFAULT '',
    head_sha VARCHAR(64) NOT NULL DEFAULT '',
    base_sha VARCHAR(64) NOT NULL DEFAULT '',
    pr_url TEXT DEFAULT '',

    -- Sync metadata
    sync_status VARCHAR(30) NOT NULL DEFAULT 'open',
    review_status VARCHAR(30) NOT NULL DEFAULT 'none',
    last_review_id UUID REFERENCES reviews(id) ON DELETE SET NULL,

    -- File change statistics
    files_changed INTEGER NOT NULL DEFAULT 0,
    additions INTEGER NOT NULL DEFAULT 0,
    deletions INTEGER NOT NULL DEFAULT 0,

    -- Engine embedding metadata
    embedded_at TIMESTAMPTZ,
    embed_error TEXT,

    -- Timestamps
    synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- A PR is uniquely identified by repo + platform + number
    UNIQUE(repo_id, platform, pr_number)
);

CREATE INDEX IF NOT EXISTS idx_tracked_prs_repo ON tracked_pull_requests(repo_id);
CREATE INDEX IF NOT EXISTS idx_tracked_prs_status ON tracked_pull_requests(sync_status);
CREATE INDEX IF NOT EXISTS idx_tracked_prs_review_status ON tracked_pull_requests(review_status);
CREATE INDEX IF NOT EXISTS idx_tracked_prs_repo_status ON tracked_pull_requests(repo_id, sync_status);
CREATE INDEX IF NOT EXISTS idx_tracked_prs_synced ON tracked_pull_requests(synced_at DESC);
CREATE INDEX IF NOT EXISTS idx_tracked_prs_updated ON tracked_pull_requests(updated_at DESC);

-- ============================================================================
-- USER VCS PLATFORMS (per-user non-secret VCS configuration)
-- ============================================================================
-- Stores URLs, usernames, org names — not secrets.
-- Actual tokens / passwords are stored in the encrypted file vault.

CREATE TABLE IF NOT EXISTS user_vcs_platforms (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform     VARCHAR(50) NOT NULL,                  -- github, gitlab, bitbucket, azure_devops
    base_url     TEXT NOT NULL DEFAULT '',               -- base URL (empty = use platform default)
    api_url      TEXT NOT NULL DEFAULT '',               -- API URL (empty = derive from base_url)
    organization VARCHAR(255) NOT NULL DEFAULT '',       -- Azure DevOps org name
    username     VARCHAR(255) NOT NULL DEFAULT '',       -- Bitbucket username
    tenant_id    VARCHAR(255) NOT NULL DEFAULT '',       -- Azure AD tenant ID
    client_id    VARCHAR(255) NOT NULL DEFAULT '',       -- Azure AD client ID
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, platform)
);

CREATE INDEX IF NOT EXISTS idx_user_vcs_platforms_user ON user_vcs_platforms(user_id);

-- ============================================================================
-- SWARM AGENTS
-- ============================================================================
-- Agent Swarm system: agents, teams, tasks, diffs, feedback, task logs.

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

-- Partial index for heartbeat monitoring (online agents only).
CREATE INDEX IF NOT EXISTS idx_swarm_agents_online
    ON swarm_agents(status)
    WHERE status != 'offline';

CREATE TABLE IF NOT EXISTS swarm_teams (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT,
    lead_agent_id  UUID REFERENCES swarm_agents(id) ON DELETE SET NULL,
    status         TEXT DEFAULT 'idle',   -- idle, busy, offline
    agent_ids      UUID[],
    formed_at      TIMESTAMPTZ DEFAULT NOW()
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_swarm_agents_team'
          AND table_name = 'swarm_agents'
    ) THEN
        ALTER TABLE swarm_agents
            ADD CONSTRAINT fk_swarm_agents_team
            FOREIGN KEY (team_id) REFERENCES swarm_teams(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS swarm_tasks (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id           TEXT NOT NULL,
    title             TEXT DEFAULT '',
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
    retry_count       INT DEFAULT 0,
    failure_reason    TEXT,
    team_formation    JSONB DEFAULT NULL,
    created_at        TIMESTAMPTZ DEFAULT NOW(),
    completed_at      TIMESTAMPTZ,
    timeout_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_swarm_tasks_repo_id ON swarm_tasks(repo_id);
CREATE INDEX IF NOT EXISTS idx_swarm_tasks_status  ON swarm_tasks(status);

-- Partial indexes for task history, timeout checking, and agent monitoring.
CREATE INDEX IF NOT EXISTS idx_swarm_tasks_history
    ON swarm_tasks(status, created_at DESC)
    WHERE status IN ('completed', 'failed', 'timed_out', 'cancelled');

CREATE INDEX IF NOT EXISTS idx_swarm_tasks_timeout
    ON swarm_tasks(timeout_at)
    WHERE timeout_at IS NOT NULL
      AND status NOT IN ('completed', 'cancelled', 'failed', 'timed_out');

CREATE INDEX IF NOT EXISTS idx_swarm_tasks_team_formation
    ON swarm_tasks USING GIN (team_formation)
    WHERE team_formation IS NOT NULL;

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

-- ── Agent Memory (MTM) ──────────────────────────────────────────────────────
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

CREATE INDEX IF NOT EXISTS idx_swarm_memory_repo_role
    ON swarm_agent_memory (repo_id, agent_role);
CREATE INDEX IF NOT EXISTS idx_swarm_memory_updated
    ON swarm_agent_memory (updated_at);

-- ── Agent Tier (ELO auto-promotion) ─────────────────────────────────────────
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'swarm_agents' AND column_name = 'tier'
    ) THEN
        ALTER TABLE swarm_agents ADD COLUMN tier TEXT NOT NULL DEFAULT 'standard';
    END IF;
END $$;

-- ── HITL Log ────────────────────────────────────────────────────────────────
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

-- ============================================================================
-- BENCHMARK TABLES
-- ============================================================================
-- A/B testing harness: runs, per-task results, and ELO ratings.

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

CREATE INDEX IF NOT EXISTS idx_benchmark_results_run ON benchmark_results(run_id);
CREATE INDEX IF NOT EXISTS idx_benchmark_results_task ON benchmark_results(task_id);

CREATE TABLE IF NOT EXISTS benchmark_elo_ratings (
    mode          TEXT PRIMARY KEY,
    rating        DOUBLE PRECISION NOT NULL DEFAULT 1500,
    wins          INTEGER NOT NULL DEFAULT 0,
    losses        INTEGER NOT NULL DEFAULT 0,
    draws         INTEGER NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO benchmark_elo_ratings (mode, rating) VALUES
    ('swarm', 1500),
    ('single_agent', 1500)
ON CONFLICT (mode) DO NOTHING;

-- ============================================================================
-- MULTIMODAL ASSETS
-- ============================================================================
-- Track uploaded/ingested assets (PDFs, images, audio, URLs) and per-modality
-- embedding model configuration.

CREATE TABLE IF NOT EXISTS repo_assets (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id       UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    asset_type    TEXT NOT NULL CHECK (asset_type IN ('pdf', 'image', 'audio', 'video', 'webpage', 'document')),
    source_url    TEXT,
    file_name     TEXT,
    mime_type     TEXT,
    size_bytes    BIGINT DEFAULT 0,
    chunks_count  INT DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'ready', 'error')),
    error_message TEXT,
    metadata      JSONB DEFAULT '{}',
    created_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_repo_assets_repo        ON repo_assets(repo_id);
CREATE INDEX IF NOT EXISTS idx_repo_assets_repo_type   ON repo_assets(repo_id, asset_type);
CREATE INDEX IF NOT EXISTS idx_repo_assets_status      ON repo_assets(repo_id, status);

CREATE TABLE IF NOT EXISTS embedding_model_config (
    id            SERIAL PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    modality      TEXT NOT NULL CHECK (modality IN ('text', 'image', 'audio')),
    model_name    TEXT NOT NULL,
    backend       TEXT NOT NULL DEFAULT 'onnx' CHECK (backend IN ('onnx', 'http', 'mock')),
    dimension     INT NOT NULL,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    config_json   JSONB DEFAULT '{}',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, modality)
);

CREATE TABLE IF NOT EXISTS model_download_status (
    model_name    TEXT PRIMARY KEY,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'downloading', 'ready', 'error')),
    progress      INT DEFAULT 0,
    size_bytes    BIGINT DEFAULT 0,
    error_message TEXT,
    started_at    TIMESTAMPTZ,
    completed_at  TIMESTAMPTZ,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO model_download_status (model_name, status, progress, completed_at)
VALUES
    ('bge-m3',      'ready',   100, NOW()),
    ('minilm',      'ready',   100, NOW()),
    ('siglip-base', 'pending',   0, NULL),
    ('clap-general','pending',   0, NULL)
ON CONFLICT (model_name) DO NOTHING;

-- ============================================================================
-- MCP INTEGRATIONS
-- ============================================================================

CREATE TABLE IF NOT EXISTS mcp_connections (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id            UUID REFERENCES organizations(id) ON DELETE CASCADE,
    is_org_level      BOOLEAN NOT NULL DEFAULT false,
    provider          TEXT NOT NULL CHECK (provider IN (
        'gmail', 'google_calendar', 'google_drive',
        'ms365',
        'jira', 'confluence',
        'github', 'gitlab',
        'slack', 'discord',
        'notion',
        'linear', 'asana', 'trello',
        'figma',
        'zendesk',
        'pagerduty', 'datadog',
        'stripe',
        'hubspot', 'salesforce',
        'twilio'
    )),
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'expired', 'revoked', 'error')),
    vault_key         TEXT NOT NULL,
    refresh_vault_key TEXT NOT NULL DEFAULT '',
    scopes            TEXT[] NOT NULL DEFAULT '{}',
    metadata          JSONB NOT NULL DEFAULT '{}',
    last_used_at      TIMESTAMPTZ,
    connected_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_connections_user_provider
    ON mcp_connections(user_id, provider) WHERE NOT is_org_level;
CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_connections_org_provider
    ON mcp_connections(org_id, provider) WHERE is_org_level;
CREATE INDEX IF NOT EXISTS idx_mcp_connections_user     ON mcp_connections(user_id);
CREATE INDEX IF NOT EXISTS idx_mcp_connections_org      ON mcp_connections(org_id) WHERE org_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_mcp_connections_provider ON mcp_connections(provider, status);
CREATE INDEX IF NOT EXISTS idx_mcp_connections_expires  ON mcp_connections(expires_at) WHERE expires_at IS NOT NULL AND status = 'active';

CREATE TABLE IF NOT EXISTS mcp_call_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL REFERENCES mcp_connections(id) ON DELETE CASCADE,
    agent_id      TEXT,
    task_id       TEXT,
    action        TEXT NOT NULL,
    input_hash    TEXT NOT NULL DEFAULT '',
    output_hash   TEXT NOT NULL DEFAULT '',
    latency_ms    INT NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'error', 'rate_limited', 'consent_denied')),
    error_message TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mcp_call_log_conn    ON mcp_call_log(connection_id);
CREATE INDEX IF NOT EXISTS idx_mcp_call_log_task    ON mcp_call_log(task_id) WHERE task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_mcp_call_log_created ON mcp_call_log(created_at DESC);

-- ============================================================================
-- CUSTOM MCP TEMPLATES
-- ============================================================================

CREATE TABLE IF NOT EXISTS mcp_custom_templates (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    label       TEXT NOT NULL,
    category    TEXT NOT NULL DEFAULT 'custom',
    description TEXT NOT NULL DEFAULT '',
    base_url    TEXT NOT NULL,
    auth_type   TEXT NOT NULL CHECK (auth_type IN ('bearer', 'basic', 'header', 'query')),
    auth_header TEXT NOT NULL DEFAULT '',
    actions     JSONB NOT NULL DEFAULT '[]',
    created_by  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id      UUID REFERENCES organizations(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_custom_templates_name
    ON mcp_custom_templates(name);
CREATE INDEX IF NOT EXISTS idx_mcp_custom_templates_creator
    ON mcp_custom_templates(created_by);
CREATE INDEX IF NOT EXISTS idx_mcp_custom_templates_org
    ON mcp_custom_templates(org_id) WHERE org_id IS NOT NULL;

-- ============================================================================
-- KEYCHAIN VAULT (encrypted per-user secret storage)
-- ============================================================================
-- End-to-end encrypted per-user secret vault.
-- The server never stores plaintext secrets. Every value in keychain_secrets
-- is encrypted with a random DEK, which is wrapped by the user's encryption
-- key (derived from the master key via HKDF-SHA256).

-- Per-user keychain metadata
CREATE TABLE IF NOT EXISTS user_keychains (
    user_id              UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    salt                 BYTEA NOT NULL,
    auth_key_hash        TEXT NOT NULL,
    recovery_hint        TEXT NOT NULL DEFAULT '',
    recovery_salt        BYTEA,
    recovery_wrapped_key BYTEA,
    key_version          INTEGER NOT NULL DEFAULT 1,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Encrypted secrets with CRDT-style versioning
CREATE TABLE IF NOT EXISTS keychain_secrets (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    ciphertext      BYTEA NOT NULL,
    wrapped_dek     BYTEA,
    version         BIGINT NOT NULL DEFAULT 1,
    category        TEXT NOT NULL DEFAULT 'custom',
    metadata        TEXT NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, name)
);

CREATE INDEX IF NOT EXISTS idx_keychain_secrets_user
    ON keychain_secrets(user_id);
CREATE INDEX IF NOT EXISTS idx_keychain_secrets_user_category
    ON keychain_secrets(user_id, category);
CREATE INDEX IF NOT EXISTS idx_keychain_secrets_user_version
    ON keychain_secrets(user_id, version);

-- Immutable audit log
CREATE TABLE IF NOT EXISTS keychain_audit_log (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action          TEXT NOT NULL,
    secret_name     TEXT NOT NULL DEFAULT '',
    ip_addr         TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_keychain_audit_user
    ON keychain_audit_log(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_keychain_audit_action
    ON keychain_audit_log(action, created_at DESC);

-- ============================================================================
-- SCHEMA VERSION TRACKING
-- ============================================================================
-- Simple version tracking table (no migration runner needed).

CREATE TABLE IF NOT EXISTS schema_info (
    version INTEGER NOT NULL,
    description TEXT,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO schema_info (version, description)
SELECT 1, 'Initial schema — users, orgs, repos, reviews, webhooks'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 1);

INSERT INTO schema_info (version, description)
SELECT 2, 'Add audit_log table for security event tracking'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 2);

INSERT INTO schema_info (version, description)
SELECT 5, 'Add tracked_pull_requests table for PR discovery, sync, and pre-embedding'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 5);

INSERT INTO schema_info (version, description)
SELECT 6, 'Add vault_token to users + user_vcs_platforms table for VCS settings'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 6);

INSERT INTO schema_info (version, description)
SELECT 7, 'Add swarm agent tables — agents, teams, tasks, diffs, feedback, task logs'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 7);

INSERT INTO schema_info (version, description)
SELECT 8, 'Add retry_count, failure_reason columns + partial indexes for history, timeout, heartbeat'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 8);

INSERT INTO schema_info (version, description)
SELECT 10, 'Add benchmark tables — runs, results, ELO ratings for A/B testing harness'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 10);

INSERT INTO schema_info (version, description)
SELECT 11, 'Add swarm_agent_memory, swarm_hitl_log tables and tier column on swarm_agents'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 11);

INSERT INTO schema_info (version, description)
SELECT 12, 'Add multimodal asset tables — repo_assets, embedding_model_config, model_download_status'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 12);

INSERT INTO schema_info (version, description)
SELECT 13, 'Add MCP integration tables — mcp_connections, mcp_call_log'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 13);

INSERT INTO schema_info (version, description)
SELECT 14, 'Add custom MCP templates table, relax provider CHECK constraint'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 14);

INSERT INTO schema_info (version, description)
SELECT 15, 'Expand MCP provider list with 10 additional providers'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 15);

INSERT INTO schema_info (version, description)
SELECT 16, 'Add keychain tables — user_keychains, keychain_secrets, keychain_audit_log'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 16);

INSERT INTO schema_info (version, description)
SELECT 17, 'Add recovery_salt and recovery_wrapped_key columns to user_keychains'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 17);

INSERT INTO schema_info (version, description)
SELECT 18, 'Add cross-repo links tables — repo_links, repo_link_events'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 18);

INSERT INTO schema_info (version, description)
SELECT 19, 'Add swarm_consensus_insights table for cross-task learning from multi-LLM decisions'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 19);

INSERT INTO schema_info (version, description)
SELECT 20, 'Add swarm_role_elo and swarm_role_elo_history tables for role-based ELO tracking'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 20);

INSERT INTO schema_info (version, description)
SELECT 21, 'Add swarm_ci_signals table for automatic CI signal ingestion into role ELO'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 21);

INSERT INTO schema_info (version, description)
SELECT 22, 'Add team_formation JSONB column to swarm_tasks for dynamic team formation'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 22);

INSERT INTO schema_info (version, description)
SELECT 23, 'Add swarm_probe_configs and swarm_probe_history tables for adaptive probe tuning'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 23);

INSERT INTO schema_info (version, description)
SELECT 24, 'Add swarm_provider_circuit_state and swarm_self_heal_events tables for self-healing pipeline'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 24);

INSERT INTO schema_info (version, description)
SELECT 25, 'Add swarm_metrics_snapshots, swarm_provider_perf_log, swarm_cost_budget for observability dashboard'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 25);

INSERT INTO schema_info (version, description)
SELECT 26, 'Add app_config table for system-wide non-secret settings (e.g. routes_enabled)'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 26);

-- ============================================================================
-- UPDATED_AT TRIGGER
-- ============================================================================

CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Drop triggers first (safe for re-run)
DROP TRIGGER IF EXISTS trg_users_updated_at ON users;
DROP TRIGGER IF EXISTS trg_oauth_identities_updated_at ON oauth_identities;
DROP TRIGGER IF EXISTS trg_organizations_updated_at ON organizations;
DROP TRIGGER IF EXISTS trg_repositories_updated_at ON repositories;

CREATE TRIGGER trg_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_oauth_identities_updated_at
    BEFORE UPDATE ON oauth_identities
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_organizations_updated_at
    BEFORE UPDATE ON organizations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_repositories_updated_at
    BEFORE UPDATE ON repositories
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS trg_tracked_prs_updated_at ON tracked_pull_requests;
CREATE TRIGGER trg_tracked_prs_updated_at
    BEFORE UPDATE ON tracked_pull_requests
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS trg_user_vcs_platforms_updated_at ON user_vcs_platforms;
CREATE TRIGGER trg_user_vcs_platforms_updated_at
    BEFORE UPDATE ON user_vcs_platforms
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS trg_benchmark_elo_ratings_updated_at ON benchmark_elo_ratings;
CREATE TRIGGER trg_benchmark_elo_ratings_updated_at
    BEFORE UPDATE ON benchmark_elo_ratings
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS trg_repo_assets_updated_at ON repo_assets;
CREATE TRIGGER trg_repo_assets_updated_at
    BEFORE UPDATE ON repo_assets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS trg_embedding_model_config_updated_at ON embedding_model_config;
CREATE TRIGGER trg_embedding_model_config_updated_at
    BEFORE UPDATE ON embedding_model_config
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS trg_model_download_status_updated_at ON model_download_status;
CREATE TRIGGER trg_model_download_status_updated_at
    BEFORE UPDATE ON model_download_status
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS trg_mcp_connections_updated_at ON mcp_connections;
CREATE TRIGGER trg_mcp_connections_updated_at
    BEFORE UPDATE ON mcp_connections
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS trg_mcp_custom_templates_updated_at ON mcp_custom_templates;
CREATE TRIGGER trg_mcp_custom_templates_updated_at
    BEFORE UPDATE ON mcp_custom_templates
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS trg_user_keychains_updated_at ON user_keychains;
CREATE TRIGGER trg_user_keychains_updated_at
    BEFORE UPDATE ON user_keychains
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS trg_keychain_secrets_updated_at ON keychain_secrets;
CREATE TRIGGER trg_keychain_secrets_updated_at
    BEFORE UPDATE ON keychain_secrets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- ============================================================================
-- CROSS-REPO LINKS
-- ============================================================================
-- Foundation for the Cross-Repo Observatory: directed repo-to-repo links
-- with share profiles controlling data exposure scope.

CREATE TABLE IF NOT EXISTS repo_links (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    source_repo_id   UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    target_repo_id   UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    share_profile    VARCHAR(20) NOT NULL DEFAULT 'metadata'
        CHECK (share_profile IN ('full', 'symbols', 'metadata', 'none')),
    label            TEXT NOT NULL DEFAULT '',
    created_by       UUID NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(org_id, source_repo_id, target_repo_id)
);

CREATE INDEX IF NOT EXISTS idx_repo_links_source ON repo_links(source_repo_id);
CREATE INDEX IF NOT EXISTS idx_repo_links_target ON repo_links(target_repo_id);
CREATE INDEX IF NOT EXISTS idx_repo_links_org    ON repo_links(org_id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'chk_repo_links_no_self_link'
          AND table_name = 'repo_links'
    ) THEN
        ALTER TABLE repo_links
            ADD CONSTRAINT chk_repo_links_no_self_link
            CHECK (source_repo_id != target_repo_id);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS repo_link_events (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    link_id          UUID REFERENCES repo_links(id) ON DELETE SET NULL,
    org_id           UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    source_repo_id   UUID NOT NULL,
    target_repo_id   UUID NOT NULL,
    action           VARCHAR(50) NOT NULL,
    actor_id         UUID REFERENCES users(id) ON DELETE SET NULL,
    metadata         JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_repo_link_events_link    ON repo_link_events(link_id);
CREATE INDEX IF NOT EXISTS idx_repo_link_events_org     ON repo_link_events(org_id);
CREATE INDEX IF NOT EXISTS idx_repo_link_events_created ON repo_link_events(created_at DESC);

DROP TRIGGER IF EXISTS trg_repo_links_updated_at ON repo_links;
CREATE TRIGGER trg_repo_links_updated_at
    BEFORE UPDATE ON repo_links
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- ============================================================================
-- SWARM CONSENSUS INSIGHTS (Cross-Task Learning)
-- ============================================================================
-- Durable per-repo insights extracted from multi-LLM consensus decisions.
-- Categories: provider_reliability, strategy_effectiveness, code_pattern,
--             provider_agreement, quality_signal.
-- TTL: 30 days (cleaned by the janitor).

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

CREATE INDEX IF NOT EXISTS idx_consensus_insights_repo_cat
    ON swarm_consensus_insights (repo_id, category);
CREATE INDEX IF NOT EXISTS idx_consensus_insights_updated
    ON swarm_consensus_insights (updated_at);
CREATE INDEX IF NOT EXISTS idx_consensus_insights_provider
    ON swarm_consensus_insights (provider)
    WHERE provider != '';

DROP TRIGGER IF EXISTS trg_consensus_insights_updated_at ON swarm_consensus_insights;
CREATE TRIGGER trg_consensus_insights_updated_at
    BEFORE UPDATE ON swarm_consensus_insights
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- ============================================================================
-- SWARM ROLE ELO (Phase 8)
-- ============================================================================
-- ELO tracking at the (role, repo_id) level instead of ephemeral agent UUIDs.
-- When a new agent registers for a role+repo it inherits the accumulated
-- score. Performance data survives agent lifecycles.

CREATE TABLE IF NOT EXISTS swarm_role_elo (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role            TEXT NOT NULL,
    repo_id         TEXT NOT NULL,
    elo_score       DOUBLE PRECISION NOT NULL DEFAULT 1200,
    tier            TEXT NOT NULL DEFAULT 'standard',  -- standard, expert, restricted
    tasks_done      INT NOT NULL DEFAULT 0,
    tasks_rated     INT NOT NULL DEFAULT 0,
    avg_rating      DOUBLE PRECISION NOT NULL DEFAULT 0,
    wins            INT NOT NULL DEFAULT 0,
    losses          INT NOT NULL DEFAULT 0,
    consensus_avg   DOUBLE PRECISION NOT NULL DEFAULT 0,
    best_strategy   TEXT NOT NULL DEFAULT '',
    training_probes INT NOT NULL DEFAULT 0,
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

-- Append-only log of every ELO change for audit, charting, and debugging.

CREATE TABLE IF NOT EXISTS swarm_role_elo_history (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role        TEXT NOT NULL,
    repo_id     TEXT NOT NULL,
    task_id     TEXT NOT NULL DEFAULT '',
    event_type  TEXT NOT NULL,
    old_elo     DOUBLE PRECISION NOT NULL,
    new_elo     DOUBLE PRECISION NOT NULL,
    delta       DOUBLE PRECISION NOT NULL,
    detail      JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_role_elo_hist_role_repo ON swarm_role_elo_history (role, repo_id);
CREATE INDEX IF NOT EXISTS idx_role_elo_hist_created   ON swarm_role_elo_history (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_role_elo_hist_event     ON swarm_role_elo_history (event_type);

DROP TRIGGER IF EXISTS trg_swarm_role_elo_updated_at ON swarm_role_elo;
CREATE TRIGGER trg_swarm_role_elo_updated_at
    BEFORE UPDATE ON swarm_role_elo
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- ============================================================================
-- SWARM CI SIGNALS
-- ============================================================================
-- Tracks per-task CI signals (PR merge state, build/test CI checks).
-- The background CISignalPoller queries completed tasks with PRs and fills
-- this table, then feeds the signals into the role-based ELO system.

CREATE TABLE IF NOT EXISTS swarm_ci_signals (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID NOT NULL REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    repo_id         TEXT NOT NULL,
    pr_number       INT,
    pr_state        TEXT NOT NULL DEFAULT 'unknown',
    pr_merged       BOOLEAN NOT NULL DEFAULT FALSE,
    ci_state        TEXT NOT NULL DEFAULT 'unknown',
    ci_total        INT NOT NULL DEFAULT 0,
    ci_passed       INT NOT NULL DEFAULT 0,
    ci_failed       INT NOT NULL DEFAULT 0,
    ci_pending      INT NOT NULL DEFAULT 0,
    ci_details      JSONB DEFAULT '[]'::jsonb,
    elo_ingested    BOOLEAN NOT NULL DEFAULT FALSE,
    elo_ingested_at TIMESTAMPTZ,
    poll_count      INT NOT NULL DEFAULT 0,
    last_polled_at  TIMESTAMPTZ,
    finalized       BOOLEAN NOT NULL DEFAULT FALSE,
    finalized_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_swarm_ci_signals_task
    ON swarm_ci_signals (task_id);
CREATE INDEX IF NOT EXISTS idx_swarm_ci_signals_unfinalized
    ON swarm_ci_signals (finalized, last_polled_at) WHERE NOT finalized;
CREATE INDEX IF NOT EXISTS idx_swarm_ci_signals_repo
    ON swarm_ci_signals (repo_id, created_at DESC);

DROP TRIGGER IF EXISTS trg_swarm_ci_signals_updated_at ON swarm_ci_signals;
CREATE TRIGGER trg_swarm_ci_signals_updated_at
    BEFORE UPDATE ON swarm_ci_signals
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- ============================================================================
-- SWARM PROBE CONFIGS (Adaptive Probe Tuning)
-- ============================================================================
-- Per-(role, repo_id, action_type) tuning parameters for the multi-LLM probe.
-- The adaptive tuning engine updates these periodically based on observed
-- probe outcomes.

CREATE TABLE IF NOT EXISTS swarm_probe_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role            TEXT NOT NULL,
    repo_id         TEXT NOT NULL DEFAULT '',
    action_type     TEXT NOT NULL DEFAULT '',
    num_models      INT NOT NULL DEFAULT 3,
    preferred_providers TEXT[] NOT NULL DEFAULT '{}',
    excluded_providers  TEXT[] NOT NULL DEFAULT '{}',
    temperature     DOUBLE PRECISION NOT NULL DEFAULT 0.2,
    max_tokens      INT NOT NULL DEFAULT 4096,
    timeout_ms      INT NOT NULL DEFAULT 60000,
    budget_cap_usd  DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    tokens_spent    BIGINT NOT NULL DEFAULT 0,
    strategy        TEXT NOT NULL DEFAULT 'adaptive',
    confidence_threshold DOUBLE PRECISION NOT NULL DEFAULT 0.6,
    max_retries     INT NOT NULL DEFAULT 1,
    reasoning       TEXT NOT NULL DEFAULT '',
    last_tuned_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (role, repo_id, action_type)
);

CREATE INDEX IF NOT EXISTS idx_swarm_probe_cfg_role_repo
    ON swarm_probe_configs (role, repo_id);

DROP TRIGGER IF EXISTS trg_swarm_probe_configs_updated_at ON swarm_probe_configs;
CREATE TRIGGER trg_swarm_probe_configs_updated_at
    BEFORE UPDATE ON swarm_probe_configs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- ============================================================================
-- SWARM PROBE HISTORY (Adaptive Probe Tuning)
-- ============================================================================
-- Append-only log of every probe outcome.  The adaptive tuning engine
-- consumes recent rows to compute provider statistics and adjust configs.

CREATE TABLE IF NOT EXISTS swarm_probe_history (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role                  TEXT NOT NULL,
    repo_id               TEXT NOT NULL DEFAULT '',
    action_type           TEXT NOT NULL DEFAULT '',
    task_id               TEXT NOT NULL DEFAULT '',
    providers_queried     TEXT[] NOT NULL DEFAULT '{}',
    providers_succeeded   TEXT[] NOT NULL DEFAULT '{}',
    provider_winner       TEXT NOT NULL DEFAULT '',
    strategy_used         TEXT NOT NULL DEFAULT '',
    consensus_confidence  DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    provider_latencies    JSONB NOT NULL DEFAULT '{}',
    provider_tokens       JSONB NOT NULL DEFAULT '{}',
    total_ms              INT NOT NULL DEFAULT 0,
    total_tokens          INT NOT NULL DEFAULT 0,
    estimated_cost_usd    DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    success               BOOLEAN NOT NULL DEFAULT FALSE,
    complexity_label      TEXT NOT NULL DEFAULT '',
    num_models_used       INT NOT NULL DEFAULT 0,
    temperature_used      DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_swarm_probe_hist_role_repo
    ON swarm_probe_history (role, repo_id);
CREATE INDEX IF NOT EXISTS idx_swarm_probe_hist_task
    ON swarm_probe_history (task_id);
CREATE INDEX IF NOT EXISTS idx_swarm_probe_hist_created
    ON swarm_probe_history (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_swarm_probe_hist_winner
    ON swarm_probe_history (provider_winner);

-- ============================================================================
-- Self-Healing Pipeline Tables
-- ============================================================================

CREATE TABLE IF NOT EXISTS swarm_provider_circuit_state (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    provider        TEXT        NOT NULL UNIQUE,
    state           TEXT        NOT NULL DEFAULT 'closed',
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

CREATE INDEX IF NOT EXISTS idx_provider_circuit_provider
    ON swarm_provider_circuit_state (provider);
CREATE INDEX IF NOT EXISTS idx_provider_circuit_state
    ON swarm_provider_circuit_state (state);

DROP TRIGGER IF EXISTS trg_provider_circuit_updated_at ON swarm_provider_circuit_state;
CREATE TRIGGER trg_provider_circuit_updated_at
    BEFORE UPDATE ON swarm_provider_circuit_state
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TABLE IF NOT EXISTS swarm_self_heal_events (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type      TEXT        NOT NULL,
    provider        TEXT,
    task_id         UUID,
    agent_id        UUID,
    details         JSONB       NOT NULL DEFAULT '{}',
    severity        TEXT        NOT NULL DEFAULT 'info',
    resolved        BOOLEAN     NOT NULL DEFAULT false,
    resolved_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_self_heal_type
    ON swarm_self_heal_events (event_type);
CREATE INDEX IF NOT EXISTS idx_self_heal_provider
    ON swarm_self_heal_events (provider);
CREATE INDEX IF NOT EXISTS idx_self_heal_task
    ON swarm_self_heal_events (task_id);
CREATE INDEX IF NOT EXISTS idx_self_heal_severity
    ON swarm_self_heal_events (severity);
CREATE INDEX IF NOT EXISTS idx_self_heal_created
    ON swarm_self_heal_events (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_self_heal_unresolved
    ON swarm_self_heal_events (resolved) WHERE resolved = false;

-- ============================================================================
-- SWARM OBSERVABILITY — METRIC SNAPSHOTS
-- ============================================================================

CREATE TABLE IF NOT EXISTS swarm_metrics_snapshots (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    active_tasks    INT         NOT NULL DEFAULT 0,
    pending_tasks   INT         NOT NULL DEFAULT 0,
    completed_tasks BIGINT      NOT NULL DEFAULT 0,
    failed_tasks    BIGINT      NOT NULL DEFAULT 0,
    online_agents   INT         NOT NULL DEFAULT 0,
    busy_agents     INT         NOT NULL DEFAULT 0,
    active_teams    INT         NOT NULL DEFAULT 0,
    busy_teams      INT         NOT NULL DEFAULT 0,
    llm_calls       BIGINT      NOT NULL DEFAULT 0,
    llm_tokens      BIGINT      NOT NULL DEFAULT 0,
    llm_avg_latency_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
    llm_error_rate  DOUBLE PRECISION NOT NULL DEFAULT 0,
    probe_calls     BIGINT      NOT NULL DEFAULT 0,
    consensus_runs  BIGINT      NOT NULL DEFAULT 0,
    consensus_avg_confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    open_circuits   INT         NOT NULL DEFAULT 0,
    heal_events     INT         NOT NULL DEFAULT 0,
    estimated_cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    queue_depth     INT         NOT NULL DEFAULT 0,
    agent_utilisation DOUBLE PRECISION NOT NULL DEFAULT 0,
    health_score    INT         NOT NULL DEFAULT 100,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_metrics_snap_created
    ON swarm_metrics_snapshots (created_at DESC);

-- ============================================================================
-- SWARM OBSERVABILITY — PROVIDER PERFORMANCE LOG
-- ============================================================================

CREATE TABLE IF NOT EXISTS swarm_provider_perf_log (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    provider        TEXT        NOT NULL,
    calls           INT         NOT NULL DEFAULT 0,
    successes       INT         NOT NULL DEFAULT 0,
    failures        INT         NOT NULL DEFAULT 0,
    tokens_used     BIGINT      NOT NULL DEFAULT 0,
    avg_latency_ms  DOUBLE PRECISION NOT NULL DEFAULT 0,
    p95_latency_ms  DOUBLE PRECISION NOT NULL DEFAULT 0,
    p99_latency_ms  DOUBLE PRECISION NOT NULL DEFAULT 0,
    error_rate      DOUBLE PRECISION NOT NULL DEFAULT 0,
    estimated_cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    consensus_wins  INT         NOT NULL DEFAULT 0,
    consensus_total INT         NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_provider_perf_created
    ON swarm_provider_perf_log (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_provider_perf_provider
    ON swarm_provider_perf_log (provider, created_at DESC);

-- ============================================================================
-- SWARM OBSERVABILITY — COST BUDGET
-- ============================================================================

CREATE TABLE IF NOT EXISTS swarm_cost_budget (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    scope           TEXT        NOT NULL DEFAULT 'global',
    month           DATE        NOT NULL,
    budget_usd      DOUBLE PRECISION NOT NULL DEFAULT 0,
    spent_usd       DOUBLE PRECISION NOT NULL DEFAULT 0,
    alert_threshold DOUBLE PRECISION NOT NULL DEFAULT 0.8,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(scope, month)
);

CREATE INDEX IF NOT EXISTS idx_cost_budget_scope
    ON swarm_cost_budget (scope, month);

DROP TRIGGER IF EXISTS trg_cost_budget_updated_at ON swarm_cost_budget;
CREATE TRIGGER trg_cost_budget_updated_at
    BEFORE UPDATE ON swarm_cost_budget
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- ============================================================================
-- APP CONFIG — system-wide key/value settings (NOT for secrets)
-- ============================================================================
-- Stores non-secret application configuration such as feature flags and
-- runtime toggles. Secrets and API keys belong in the keychain, NOT here.

CREATE TABLE IF NOT EXISTS app_config (
    key         TEXT        PRIMARY KEY,
    value       TEXT        NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS trg_app_config_updated_at ON app_config;
CREATE TRIGGER trg_app_config_updated_at
    BEFORE UPDATE ON app_config
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- Seed defaults (idempotent).
INSERT INTO app_config (key, value)
SELECT 'llm_routes_enabled', 'false'
WHERE NOT EXISTS (SELECT 1 FROM app_config WHERE key = 'llm_routes_enabled');

-- ============================================================================
-- GRANT ACCESS TO rtvortex ROLE
-- ============================================================================
-- Ensures the rtvortex application role has full access to all tables.
-- Safe to re-run — will not fail if grants already exist.

DO $$
BEGIN
    GRANT ALL PRIVILEGES ON TABLE users TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE oauth_identities TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE organizations TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE org_members TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE repositories TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE repo_members TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE reviews TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE review_comments TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE usage_daily TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE webhook_events TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE audit_log TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE tracked_pull_requests TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE schema_info TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE user_vcs_platforms TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_agents TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_teams TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_tasks TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_task_diffs TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_diff_comments TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE agent_feedback TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE agent_task_log TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_agent_memory TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_hitl_log TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE benchmark_runs TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE benchmark_results TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE benchmark_elo_ratings TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE repo_assets TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE embedding_model_config TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE model_download_status TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE mcp_connections TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE mcp_call_log TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE mcp_custom_templates TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE user_keychains TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE keychain_secrets TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE keychain_audit_log TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE repo_links TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE repo_link_events TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_consensus_insights TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_role_elo TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_role_elo_history TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_ci_signals TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_probe_configs TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_probe_history TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_provider_circuit_state TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_self_heal_events TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_metrics_snapshots TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_provider_perf_log TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE swarm_cost_budget TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE app_config TO rtvortex;
    GRANT ALL PRIVILEGES ON SEQUENCE embedding_model_config_id_seq TO rtvortex;
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'GRANT failed (non-fatal): %', SQLERRM;
END $$;
