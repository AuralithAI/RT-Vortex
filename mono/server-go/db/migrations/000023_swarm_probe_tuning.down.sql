-- Adaptive Probe Tuning — rollback
DROP TABLE IF EXISTS swarm_probe_history;
DROP TABLE IF EXISTS swarm_probe_configs;
DELETE FROM schema_migrations WHERE version = 23;
