-- ============================================================================
-- RTVortex Database Schema — Migration 027 (DOWN): Keychain Repo-Scoped Secrets
-- ============================================================================

-- Drop the builds table.
DROP TABLE IF EXISTS swarm_builds;

-- Restore the original unique constraint.
DROP INDEX IF EXISTS idx_keychain_secrets_user_name_repo;
ALTER TABLE keychain_secrets
    ADD CONSTRAINT keychain_secrets_user_id_name_key UNIQUE (user_id, name);

-- Drop the repo index and column.
DROP INDEX IF EXISTS idx_keychain_secrets_repo;
ALTER TABLE keychain_secrets
    DROP COLUMN IF EXISTS repo_id;
