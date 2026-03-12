-- ==============================================================================
-- 000007_swarm_tables.down.sql — Roll back Vortex Agent Swarm schema
-- ==============================================================================

DROP TABLE IF EXISTS agent_task_log CASCADE;
DROP TABLE IF EXISTS agent_feedback CASCADE;
DROP TABLE IF EXISTS swarm_diff_comments CASCADE;
DROP TABLE IF EXISTS swarm_task_diffs CASCADE;
DROP TABLE IF EXISTS swarm_tasks CASCADE;
DROP TABLE IF EXISTS swarm_teams CASCADE;
DROP TABLE IF EXISTS swarm_agents CASCADE;

DELETE FROM schema_info WHERE version = 7;
