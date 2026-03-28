-- ============================================================================
-- RTVortex Database Schema — Migration 004 (Down): Drop Tracked Pull Requests
-- ============================================================================

DROP TRIGGER IF EXISTS trg_tracked_prs_updated_at ON tracked_pull_requests;
DROP TABLE IF EXISTS tracked_pull_requests;

DELETE FROM schema_info WHERE version = 5;
