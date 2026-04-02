-- ============================================================================
-- Migration 017: Add recovery columns to user_keychains
-- ============================================================================
-- Adds the recovery_salt and recovery_wrapped_key columns required for the
-- dual-wrap recovery path. Existing rows will have
-- NULL values, which the service treats as "legacy keychain — re-init required".
-- ============================================================================

ALTER TABLE user_keychains
    ADD COLUMN IF NOT EXISTS recovery_salt        BYTEA,
    ADD COLUMN IF NOT EXISTS recovery_wrapped_key BYTEA;
