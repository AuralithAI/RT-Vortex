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
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'GRANT failed (non-fatal): %', SQLERRM;
END $$;
