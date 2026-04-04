-- ============================================================================
-- Migration 018: Cross-Repo Links & Share Profiles
-- ============================================================================
-- Foundation for the Cross-Repo Observatory feature.
-- A "repo link" represents a directed relationship between two repositories
-- within the same organization, with a share profile that controls what data
-- from the target repo is visible to users operating in the source repo.
--
-- The authorizer checks: (user_role_in_source × link_share_profile × user_role_in_target)
-- before allowing any cross-repo data access.
-- ============================================================================

-- ── repo_links ──────────────────────────────────────────────────────────────
-- Directed edge: source_repo_id → target_repo_id (within same org).
-- share_profile controls the scope of data exposure.

CREATE TABLE IF NOT EXISTS repo_links (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    source_repo_id   UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    target_repo_id   UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,

    -- Share profile: controls what data from target is visible in source context.
    --   "full"        — all indexed data (search, symbols, file content)
    --   "symbols"     — only exported symbols, function signatures, types
    --   "metadata"    — repo manifest only (language, build system, dependencies)
    --   "none"        — link exists but sharing is paused (soft-disable)
    share_profile    VARCHAR(20) NOT NULL DEFAULT 'metadata'
        CHECK (share_profile IN ('full', 'symbols', 'metadata', 'none')),

    -- Optional label for humans (e.g. "api-server depends on shared-lib")
    label            TEXT NOT NULL DEFAULT '',

    -- Who created the link and when
    created_by       UUID NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- A link between two repos (in either direction) is unique within an org.
    -- We enforce source < target ordering at the application layer for
    -- bidirectional lookups, but allow directed links in the table.
    UNIQUE(org_id, source_repo_id, target_repo_id)
);

-- Fast lookups: "what repos are linked FROM this repo?" and "TO this repo?"
CREATE INDEX IF NOT EXISTS idx_repo_links_source ON repo_links(source_repo_id);
CREATE INDEX IF NOT EXISTS idx_repo_links_target ON repo_links(target_repo_id);
CREATE INDEX IF NOT EXISTS idx_repo_links_org    ON repo_links(org_id);

-- Prevent self-links at the database level.
ALTER TABLE repo_links
    ADD CONSTRAINT chk_repo_links_no_self_link
    CHECK (source_repo_id != target_repo_id);

-- ── repo_link_events (lightweight audit trail for link mutations) ───────────
-- Separate from the main audit_log to avoid polluting it with high-frequency
-- cross-repo events. This table is append-only and can be partitioned later.

CREATE TABLE IF NOT EXISTS repo_link_events (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    link_id          UUID REFERENCES repo_links(id) ON DELETE SET NULL,
    org_id           UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    source_repo_id   UUID NOT NULL,
    target_repo_id   UUID NOT NULL,
    action           VARCHAR(50) NOT NULL,   -- link.created, link.updated, link.deleted, link.access_denied, link.access_granted
    actor_id         UUID REFERENCES users(id) ON DELETE SET NULL,
    metadata         JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_repo_link_events_link    ON repo_link_events(link_id);
CREATE INDEX IF NOT EXISTS idx_repo_link_events_org     ON repo_link_events(org_id);
CREATE INDEX IF NOT EXISTS idx_repo_link_events_created ON repo_link_events(created_at DESC);
