DROP TRIGGER IF EXISTS trg_app_config_updated_at ON app_config;
DROP TABLE IF EXISTS app_config;
DELETE FROM schema_info WHERE version = 26;
