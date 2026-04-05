-- Rollback Dynamic Team Formation

ALTER TABLE swarm_tasks DROP COLUMN IF EXISTS team_formation;
DROP INDEX IF EXISTS idx_swarm_tasks_team_formation;

DELETE FROM schema_migrations WHERE version = 22;
