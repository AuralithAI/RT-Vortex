-- App Config — system-wide key/value settings (NOT for secrets).
-- Secrets and API keys belong in the keychain, NOT here.

CREATE TABLE IF NOT EXISTS app_config (
    key         TEXT        PRIMARY KEY,
    value       TEXT        NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS trg_app_config_updated_at ON app_config;
CREATE TRIGGER trg_app_config_updated_at
    BEFORE UPDATE ON app_config
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- Seed defaults (idempotent).
INSERT INTO app_config (key, value)
SELECT 'llm_routes_enabled', 'false'
WHERE NOT EXISTS (SELECT 1 FROM app_config WHERE key = 'llm_routes_enabled');

-- Record schema version (matches initData.sql schema_info).
INSERT INTO schema_info (version, description)
SELECT 26, 'Add app_config table for system-wide non-secret settings (e.g. routes_enabled)'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 26);
