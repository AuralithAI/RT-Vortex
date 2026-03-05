-- ============================================================================
-- RTVortex Database Schema — Migration 002 DOWN: Rollback Production Hardening
-- ============================================================================

DROP TABLE IF EXISTS plan_limits;
DROP TABLE IF EXISTS webhook_deliveries;

DELETE FROM schema_info WHERE version = 3;
