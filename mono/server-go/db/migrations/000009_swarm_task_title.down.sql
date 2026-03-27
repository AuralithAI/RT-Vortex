-- 000009_swarm_task_title.down.sql — Remove title column from swarm_tasks.

ALTER TABLE swarm_tasks DROP COLUMN IF EXISTS title;
