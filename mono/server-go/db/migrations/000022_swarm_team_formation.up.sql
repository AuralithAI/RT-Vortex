-- ─── Dynamic Team Formation ──────────────────────────────
-- Adds a team_formation JSONB column to swarm_tasks.
-- Stores the computed complexity score, recommended roles, ELO tiers,
-- and reasoning for the team composition decision.
--
-- Example JSON:
-- {
--   "complexity_score": 0.72,
--   "complexity_label": "large",
--   "input_signals": {
--     "file_count": 12,
--     "step_count": 8,
--     "description_length": 450,
--     "language_count": 3,
--     "test_files": 2,
--     "has_migrations": true,
--     "cross_package": true
--   },
--   "recommended_roles": ["architect","senior_dev","junior_dev","qa","security"],
--   "role_elos": {
--     "architect": {"elo": 1350.0, "tier": "standard"},
--     "senior_dev": {"elo": 1420.0, "tier": "expert"}
--   },
--   "team_size": 6,
--   "reasoning": "Large complexity (0.72): 12 files across 3 languages ...",
--   "strategy": "elo_weighted",
--   "created_at": "2026-04-05T..."
-- }

ALTER TABLE swarm_tasks
  ADD COLUMN IF NOT EXISTS team_formation JSONB DEFAULT NULL;

-- Index for querying tasks by team formation strategy.
CREATE INDEX IF NOT EXISTS idx_swarm_tasks_team_formation
  ON swarm_tasks USING gin (team_formation) WHERE team_formation IS NOT NULL;

-- Update schema_migrations version.
INSERT INTO schema_migrations (version, dirty)
VALUES (22, false)
ON CONFLICT (version) DO NOTHING;
