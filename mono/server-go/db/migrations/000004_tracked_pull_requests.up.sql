-- ============================================================================
-- RTVortex Database Schema — Migration 004: Tracked Pull Requests
-- ============================================================================
-- Adds pull_requests table for background PR discovery, syncing,
-- pre-embedding via the C++ engine, and one-click review triggering.
-- ============================================================================

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

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_tracked_prs_repo ON tracked_pull_requests(repo_id);
CREATE INDEX IF NOT EXISTS idx_tracked_prs_status ON tracked_pull_requests(sync_status);
CREATE INDEX IF NOT EXISTS idx_tracked_prs_review_status ON tracked_pull_requests(review_status);
CREATE INDEX IF NOT EXISTS idx_tracked_prs_repo_status ON tracked_pull_requests(repo_id, sync_status);
CREATE INDEX IF NOT EXISTS idx_tracked_prs_synced ON tracked_pull_requests(synced_at DESC);
CREATE INDEX IF NOT EXISTS idx_tracked_prs_updated ON tracked_pull_requests(updated_at DESC);

-- Auto-update updated_at
CREATE TRIGGER trg_tracked_prs_updated_at
    BEFORE UPDATE ON tracked_pull_requests FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- Schema version tracking
INSERT INTO schema_info (version, description)
SELECT 5, 'Add tracked_pull_requests table for PR discovery, sync, and pre-embedding'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 5);
