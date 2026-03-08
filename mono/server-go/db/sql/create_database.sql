-- ============================================================================
-- RTVortex Database Setup — Step 1: Create Role and Database
-- ============================================================================
-- Run this as the PostgreSQL superuser (typically 'postgres'):
--
--   psql -U postgres -f create_database.sql
--
-- This script:
--   1. Creates the 'rtvortex' role (user) with a password
--   2. Creates the 'rtvortex' database owned by that role
--   3. Grants all necessary privileges
--
-- After this, run initData.sql to create the schema:
--
--   psql -U rtvortex -d rtvortex -f initData.sql
-- ============================================================================

-- Create the application role (if it doesn't already exist)
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'rtvortex') THEN
        CREATE ROLE rtvortex WITH LOGIN PASSWORD 'rtvortex';
        RAISE NOTICE 'Role "rtvortex" created.';
    ELSE
        RAISE NOTICE 'Role "rtvortex" already exists — skipping.';
    END IF;
END
$$;

-- Allow the role to create databases (needed for test DBs)
ALTER ROLE rtvortex CREATEDB;

-- Create the application database (if it doesn't already exist)
SELECT 'CREATE DATABASE rtvortex OWNER rtvortex'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'rtvortex')\gexec

-- Grant connect privilege
GRANT ALL PRIVILEGES ON DATABASE rtvortex TO rtvortex;

-- Set default privileges so tables/sequences/functions created by any role
-- (e.g. postgres running migrations) are automatically accessible by rtvortex.
\connect rtvortex
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO rtvortex;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO rtvortex;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON FUNCTIONS TO rtvortex;
