-- Rollback Automatic CI Signal Ingestion

DROP TABLE IF EXISTS swarm_ci_signals CASCADE;

DELETE FROM schema_migrations WHERE version = 21;
