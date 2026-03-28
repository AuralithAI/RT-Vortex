-- ============================================================================
-- RTVortex Database Schema — Migration 003: Repository Member Access
-- ============================================================================
-- Adds repo_members table for fine-grained per-repo access control.
-- When no rows exist for a repo, all org members can access it (default open).
-- When rows exist, only listed members have access.
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
