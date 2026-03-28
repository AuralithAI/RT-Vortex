-- 000009_swarm_task_title.up.sql — Add title column to swarm_tasks.

ALTER TABLE swarm_tasks ADD COLUMN IF NOT EXISTS title TEXT DEFAULT '';
