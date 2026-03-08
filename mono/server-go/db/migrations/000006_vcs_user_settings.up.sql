-- ─── Migration 000006: VCS User Settings ──────────────────────────────────
-- 1. Adds vault_token column to users table for per-user secret isolation.
--    Uses a 64-char hex string (32 bytes of crypto-random) instead of UUID
--    for stronger entropy and no UUID structure leakage.
-- 2. Creates user_vcs_platforms table for non-secret VCS config (URLs,
--    usernames, org names). Actual tokens/secrets stay in the file vault.
-- ───────────────────────────────────────────────────────────────────────────

-- ─── 1. vault_token on users ────────────────────────────────────────────────

-- Add vault_token column — 64-char hex (32 bytes crypto-random)
ALTER TABLE users ADD COLUMN IF NOT EXISTS vault_token VARCHAR(64);

-- Back-fill existing users with crypto-random tokens
UPDATE users SET vault_token = encode(gen_random_bytes(32), 'hex') WHERE vault_token IS NULL;

-- Make it NOT NULL after back-fill
ALTER TABLE users ALTER COLUMN vault_token SET NOT NULL;

-- Unique index
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_vault_token ON users(vault_token);

-- ─── 2. user_vcs_platforms table ────────────────────────────────────────────

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

-- Updated-at trigger
DROP TRIGGER IF EXISTS trg_user_vcs_platforms_updated_at ON user_vcs_platforms;
CREATE TRIGGER trg_user_vcs_platforms_updated_at
    BEFORE UPDATE ON user_vcs_platforms
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- ─── 3. GRANT access to rtvortex role ───────────────────────────────────────

DO $$
BEGIN
    -- GRANTs on all tables for the rtvortex role
    GRANT ALL PRIVILEGES ON TABLE users TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE user_vcs_platforms TO rtvortex;
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
    GRANT ALL PRIVILEGES ON TABLE chat_sessions TO rtvortex;
    GRANT ALL PRIVILEGES ON TABLE chat_messages TO rtvortex;
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'GRANT failed (non-fatal): %', SQLERRM;
END $$;

-- Record schema version
INSERT INTO schema_info (version, description)
SELECT 6, 'Add vault_token to users + user_vcs_platforms table for VCS settings'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 6);
