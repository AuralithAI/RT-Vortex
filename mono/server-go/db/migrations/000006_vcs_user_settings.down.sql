-- ─── Rollback Migration 000006 ─────────────────────────────────────────────
DROP TRIGGER IF EXISTS trg_user_vcs_platforms_updated_at ON user_vcs_platforms;
DROP TABLE IF EXISTS user_vcs_platforms;
DROP INDEX IF EXISTS idx_users_vault_token;
ALTER TABLE users DROP COLUMN IF EXISTS vault_token;
DELETE FROM schema_info WHERE version = 6;
