DROP TABLE IF EXISTS agent_task_log;
DROP TABLE IF EXISTS agent_feedback;
DROP TABLE IF EXISTS swarm_hitl_log;
DROP TABLE IF EXISTS swarm_agent_memory;
ALTER TABLE swarm_agents DROP COLUMN IF EXISTS tier;
