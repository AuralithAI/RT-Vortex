-- ============================================================================
-- RTVortex Database Schema — Migration 016: Keychain Vault
-- ============================================================================
-- Creates the keychain tables for encrypted per-user secret storage.
--
-- Design:
--   - user_keychains: one row per user, holds HKDF salt, auth verifier, key version
--   - keychain_secrets: encrypted secrets with per-secret DEK envelope encryption
--   - keychain_audit_log: immutable log of keychain operations
--
-- The server never stores plaintext secrets. Every value in keychain_secrets
-- is encrypted with a random DEK, which itself is wrapped by the user's
-- encryption key (derived from the master key via HKDF).
-- ============================================================================

-- Per-user keychain metadata
CREATE TABLE IF NOT EXISTS user_keychains (
    user_id         UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    salt            BYTEA NOT NULL,
    auth_key_hash   TEXT NOT NULL,
    recovery_hint   TEXT NOT NULL DEFAULT '',
    key_version     INTEGER NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Encrypted secrets with CRDT-style versioning
CREATE TABLE IF NOT EXISTS keychain_secrets (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    ciphertext      BYTEA NOT NULL,
    wrapped_dek     BYTEA,
    version         BIGINT NOT NULL DEFAULT 1,
    category        TEXT NOT NULL DEFAULT 'custom',
    metadata        TEXT NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, name)
);

CREATE INDEX IF NOT EXISTS idx_keychain_secrets_user
    ON keychain_secrets(user_id);
CREATE INDEX IF NOT EXISTS idx_keychain_secrets_user_category
    ON keychain_secrets(user_id, category);
CREATE INDEX IF NOT EXISTS idx_keychain_secrets_user_version
    ON keychain_secrets(user_id, version);

-- Immutable audit log
CREATE TABLE IF NOT EXISTS keychain_audit_log (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action          TEXT NOT NULL,
    secret_name     TEXT NOT NULL DEFAULT '',
    ip_addr         TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_keychain_audit_user
    ON keychain_audit_log(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_keychain_audit_action
    ON keychain_audit_log(action, created_at DESC);
