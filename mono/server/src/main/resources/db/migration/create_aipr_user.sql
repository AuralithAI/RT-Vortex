-- =============================================================================
-- AI-PR-Reviewer — PostgreSQL Bootstrap Script
-- =============================================================================
-- Creates the database role, database, and required extensions.
-- Run this ONCE as a PostgreSQL superuser before starting the server.
-- Flyway (triggered at server startup) applies the schema migrations.
--
-- USAGE
-- -----
-- Simplest — uses all defaults (role: aipr, password: aipr_secret, db: aipr):
--
--   psql -U postgres -f scripts/create_aipr_user.sql
--
-- Custom password:
--
--   psql -U postgres \
--        -v aipr_password='s3cur3P@ss!' \
--        -f scripts/create_aipr_user.sql
--
-- Fully custom (different user/db per environment):
--
--   psql -U postgres \
--        -v aipr_user=aipr_dev \
--        -v aipr_password='devpassword' \
--        -v aipr_db=aipr_dev \
--        -f scripts/create_aipr_user.sql
--
-- Non-interactive CI / Docker:
--
--   PGPASSWORD="$POSTGRES_PASSWORD" psql \
--        -U postgres -h localhost \
--        -v aipr_user="$DB_USER" \
--        -v aipr_password="$DB_PASSWORD" \
--        -v aipr_db="$DB_NAME" \
--        -f scripts/create_aipr_user.sql
--
-- VARIABLES (override with -v name=value)
--   aipr_user      — PostgreSQL role name   (default: aipr)
--   aipr_password  — role password          (default: aipr_secret)
--   aipr_db        — database name          (default: aipr)
-- =============================================================================

-- Set defaults for any variable not supplied via -v.
-- psql leaves variables unset if -v was not passed; \set only applies when
-- the variable is truly absent (does not overwrite an explicit -v value).
\if :{?aipr_user}
\else
    \set aipr_user aipr
\endif

\if :{?aipr_password}
\else
    \set aipr_password aipr_secret
\endif

\if :{?aipr_db}
\else
    \set aipr_db aipr
\endif

\echo '------------------------------------------------------------'
\echo ' AI-PR-Reviewer PostgreSQL Bootstrap'
\echo '------------------------------------------------------------'
\echo 'Role     :' :aipr_user
\echo 'Database :' :aipr_db
\echo '------------------------------------------------------------'

-- =============================================================================
-- ROLE
-- =============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT FROM pg_catalog.pg_roles WHERE rolname = :'aipr_user'
    ) THEN
        EXECUTE format(
            'CREATE ROLE %I WITH LOGIN PASSWORD %L',
            :'aipr_user',
            :'aipr_password'
        );
        RAISE NOTICE 'Role "%" created.', :'aipr_user';
    ELSE
        -- Role already exists — refresh the password in case it changed.
        EXECUTE format(
            'ALTER ROLE %I WITH LOGIN PASSWORD %L',
            :'aipr_user',
            :'aipr_password'
        );
        RAISE NOTICE 'Role "%" already exists — password updated.', :'aipr_user';
    END IF;
END;
$$;

-- =============================================================================
-- DATABASE
-- CREATE DATABASE must run outside a transaction block.
-- We use \gexec to execute a dynamically built statement.
-- =============================================================================

-- Build and execute CREATE DATABASE only if it doesn't exist yet.
SELECT format(
    'CREATE DATABASE %I
         OWNER    %I
         ENCODING ''UTF8''
         LC_COLLATE ''en_US.UTF-8''
         LC_CTYPE  ''en_US.UTF-8''
         TEMPLATE  template0',
    :'aipr_db',
    :'aipr_user'
)
WHERE NOT EXISTS (
    SELECT FROM pg_catalog.pg_database WHERE datname = :'aipr_db'
) \gexec

-- If the database already existed, make sure ownership is correct.
SELECT format('ALTER DATABASE %I OWNER TO %I', :'aipr_db', :'aipr_user')
WHERE EXISTS (
    SELECT FROM pg_catalog.pg_database WHERE datname = :'aipr_db'
) \gexec

-- =============================================================================
-- CONNECT AND CONFIGURE
-- =============================================================================

\c :aipr_db

-- Extensions — require superuser, install before handing off to the app role.
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";   -- uuid_generate_v4()
CREATE EXTENSION IF NOT EXISTS "pg_trgm";     -- trigram text search indexes
CREATE EXTENSION IF NOT EXISTS "pgcrypto";    -- gen_random_bytes() for token hashing

-- Grant schema-level privileges to the app role.
GRANT ALL PRIVILEGES ON DATABASE :aipr_db TO :aipr_user;
GRANT ALL ON SCHEMA public TO :aipr_user;

-- Any future tables/sequences created by Flyway will automatically be
-- accessible to the app role without needing further GRANT statements.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO :aipr_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO :aipr_user;

-- =============================================================================
-- SUMMARY
-- =============================================================================
\echo ''
\echo '============================================================'
\echo ' Bootstrap complete. Connection string for application.yml:'
SELECT format(
    'DATABASE_URL=jdbc:postgresql://localhost:5432/%s',
    :'aipr_db'
) AS "DATABASE_URL";
SELECT format('DATABASE_USER=%s', :'aipr_user')  AS "DATABASE_USER";
SELECT 'DATABASE_PASSWORD=<the password you provided>'  AS "DATABASE_PASSWORD";
\echo ''
\echo 'Next: start the server — Spring Boot will run Flyway and'
\echo 'apply V1__initial_schema.sql and V2__user_sessions.sql.'
\echo '============================================================'
