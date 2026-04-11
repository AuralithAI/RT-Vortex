-- ============================================================================
-- RTVortex Database Schema — Migration 027: Keychain Repo-Scoped Secrets
-- ============================================================================
-- Adds repo_id to keychain_secrets so secrets can be scoped to a specific
-- repository.  When repo_id IS NULL the secret is a global user secret
-- (backwards-compatible with existing behaviour).  When set, the secret
-- applies only to builds for that repository.
--
-- Also creates the swarm_builds table to store build execution results,
-- logs, and secret audit trails for the sandbox builder feature.
-- ============================================================================

-- ── Repo-scoped secrets ─────────────────────────────────────────────────────

ALTER TABLE keychain_secrets
    ADD COLUMN IF NOT EXISTS repo_id UUID REFERENCES repositories(id) ON DELETE CASCADE;

-- Partial index: fast lookup of secrets scoped to a specific repo.
CREATE INDEX IF NOT EXISTS idx_keychain_secrets_repo
    ON keychain_secrets(user_id, repo_id) WHERE repo_id IS NOT NULL;

-- Drop the existing unique constraint (user_id, name) and replace it with
-- a broader one that allows the same secret name for different repos.
-- The original constraint is:  UNIQUE (user_id, name)
-- The new one is:              UNIQUE (user_id, name, COALESCE(repo_id, '00000000-...'))
-- This lets a user have e.g. "DATABASE_URL" globally AND per-repo.
ALTER TABLE keychain_secrets
    DROP CONSTRAINT IF EXISTS keychain_secrets_user_id_name_key;

CREATE UNIQUE INDEX IF NOT EXISTS idx_keychain_secrets_user_name_repo
    ON keychain_secrets(user_id, name, COALESCE(repo_id, '00000000-0000-0000-0000-000000000000'));

-- ── Build results table ─────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS swarm_builds (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id         UUID NOT NULL REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    repo_id         TEXT NOT NULL,
    user_id         UUID REFERENCES users(id) ON DELETE SET NULL,
    build_system    TEXT NOT NULL DEFAULT 'unknown',
    command         TEXT NOT NULL DEFAULT '',
    base_image      TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending, running, success, failed, blocked
    exit_code       INTEGER,
    log_summary     TEXT NOT NULL DEFAULT '',
    secret_names    TEXT[] NOT NULL DEFAULT '{}',       -- names only, never values
    sandbox_mode    BOOLEAN NOT NULL DEFAULT true,
    retry_count     INTEGER NOT NULL DEFAULT 0,
    duration_ms     INTEGER,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_swarm_builds_task
    ON swarm_builds(task_id);
CREATE INDEX IF NOT EXISTS idx_swarm_builds_repo
    ON swarm_builds(repo_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_swarm_builds_status
    ON swarm_builds(status) WHERE status NOT IN ('success', 'failed');
