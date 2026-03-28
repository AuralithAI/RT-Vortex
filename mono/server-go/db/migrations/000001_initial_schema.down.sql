-- ============================================================================
-- RTVortex Database Schema — Migration 001: Rollback
-- ============================================================================
-- WARNING: This drops ALL tables. Only use in development.
-- ============================================================================

DROP TRIGGER IF EXISTS trg_repositories_updated_at ON repositories;
DROP TRIGGER IF EXISTS trg_organizations_updated_at ON organizations;
DROP TRIGGER IF EXISTS trg_oauth_identities_updated_at ON oauth_identities;
DROP TRIGGER IF EXISTS trg_users_updated_at ON users;
DROP FUNCTION IF EXISTS update_updated_at();

DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS webhook_events;
DROP TABLE IF EXISTS usage_daily;
DROP TABLE IF EXISTS review_comments;
DROP TABLE IF EXISTS reviews;
DROP TABLE IF EXISTS repositories;
DROP TABLE IF EXISTS org_members;
DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS oauth_identities;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS schema_info;

DROP EXTENSION IF EXISTS "pgcrypto";
DROP EXTENSION IF EXISTS "pg_trgm";
DROP EXTENSION IF EXISTS "uuid-ossp";
