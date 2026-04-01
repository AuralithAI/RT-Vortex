-- ============================================================================
-- Migration 017 (down): Remove recovery columns from user_keychains
-- ============================================================================

ALTER TABLE user_keychains
    DROP COLUMN IF EXISTS recovery_salt,
    DROP COLUMN IF EXISTS recovery_wrapped_key;
