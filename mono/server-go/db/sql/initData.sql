-- ============================================================================
-- RTVortex Database Schema — Step 2: Initialize Tables
-- ============================================================================
-- Run this as the 'rtvortex' user on the 'rtvortex' database:
--
--   psql -U rtvortex -d rtvortex -f initData.sql
--
-- Prerequisites:
--   - PostgreSQL 15+
--   - create_database.sql has been run by the postgres superuser
--
-- This script is idempotent (safe to re-run — uses IF NOT EXISTS).
-- ============================================================================

-- Enable extensions (requires superuser to have already allowed them,
-- or the rtvortex role to have CREATEDB privilege)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";      -- trigram similarity for text search
CREATE EXTENSION IF NOT EXISTS "pgcrypto";      -- encryption functions

-- ============================================================================
-- USERS
-- ============================================================================

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    display_name VARCHAR(255) NOT NULL,
    avatar_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);

-- ============================================================================
-- OAUTH IDENTITIES
-- ============================================================================
-- A user can have multiple OAuth identities (login via Google + link GitHub)

CREATE TABLE IF NOT EXISTS oauth_identities (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(50) NOT NULL,               -- github, google, microsoft, linkedin, gitlab, bitbucket
    provider_user_id VARCHAR(255) NOT NULL,
    access_token_enc TEXT,                        -- AES-256-GCM encrypted
    refresh_token_enc TEXT,                       -- AES-256-GCM encrypted
    scopes TEXT,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(provider, provider_user_id)
);

CREATE INDEX IF NOT EXISTS idx_oauth_user_id ON oauth_identities(user_id);
CREATE INDEX IF NOT EXISTS idx_oauth_provider ON oauth_identities(provider, provider_user_id);

-- ============================================================================
-- ORGANIZATIONS
-- ============================================================================

CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(100) NOT NULL UNIQUE,
    plan VARCHAR(50) NOT NULL DEFAULT 'free',     -- free, pro, enterprise
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_organizations_slug ON organizations(slug);

-- ============================================================================
-- ORGANIZATION MEMBERS
-- ============================================================================

CREATE TABLE IF NOT EXISTS org_members (
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(50) NOT NULL DEFAULT 'member',   -- owner, admin, member, viewer
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_org_members_user ON org_members(user_id);

-- ============================================================================
-- REPOSITORIES
-- ============================================================================

CREATE TABLE IF NOT EXISTS repositories (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    platform VARCHAR(50) NOT NULL,                -- github, gitlab, bitbucket, azure_devops
    external_id VARCHAR(255) NOT NULL,
    owner VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    default_branch VARCHAR(100) DEFAULT 'main',
    clone_url TEXT,
    webhook_secret VARCHAR(255),
    config JSONB NOT NULL DEFAULT '{}',
    indexed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(platform, external_id)
);

CREATE INDEX IF NOT EXISTS idx_repositories_org ON repositories(org_id);
CREATE INDEX IF NOT EXISTS idx_repositories_platform ON repositories(platform);

-- ============================================================================
-- REVIEWS
-- ============================================================================

CREATE TABLE IF NOT EXISTS reviews (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repo_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    triggered_by UUID REFERENCES users(id),
    platform VARCHAR(50) NOT NULL,
    pr_number INTEGER NOT NULL,
    pr_title TEXT,
    pr_author VARCHAR(255),
    base_branch VARCHAR(255),
    head_branch VARCHAR(255),
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- pending, in_progress, completed, failed, cancelled
    files_changed INTEGER DEFAULT 0,
    additions INTEGER DEFAULT 0,
    deletions INTEGER DEFAULT 0,
    metadata JSONB DEFAULT '{}',                    -- timing, LLM model, tokens used, etc.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_reviews_repo ON reviews(repo_id);
CREATE INDEX IF NOT EXISTS idx_reviews_status ON reviews(status);
CREATE INDEX IF NOT EXISTS idx_reviews_created ON reviews(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reviews_repo_pr ON reviews(repo_id, pr_number);

-- ============================================================================
-- REVIEW COMMENTS
-- ============================================================================

CREATE TABLE IF NOT EXISTS review_comments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    review_id UUID NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
    file_path TEXT NOT NULL,
    line_number INTEGER,
    end_line INTEGER,
    severity VARCHAR(20) NOT NULL DEFAULT 'info',  -- critical, high, medium, low, info
    category VARCHAR(50),                           -- security, performance, style, bug, logic, etc.
    title TEXT,
    body TEXT NOT NULL,
    suggestion TEXT,                                -- suggested code fix
    posted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_review_comments_review ON review_comments(review_id);
CREATE INDEX IF NOT EXISTS idx_review_comments_severity ON review_comments(severity);

-- ============================================================================
-- USAGE / BILLING
-- ============================================================================

CREATE TABLE IF NOT EXISTS usage_daily (
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    date DATE NOT NULL,
    reviews_count INTEGER NOT NULL DEFAULT 0,
    tokens_used BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (org_id, date)
);

-- ============================================================================
-- WEBHOOK EVENTS (audit log)
-- ============================================================================

CREATE TABLE IF NOT EXISTS webhook_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    platform VARCHAR(50) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    repo_id UUID REFERENCES repositories(id),
    payload JSONB,
    processed BOOLEAN NOT NULL DEFAULT FALSE,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_events_created ON webhook_events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_webhook_events_repo ON webhook_events(repo_id);

-- ============================================================================
-- AUDIT LOG
-- ============================================================================
-- Tracks security-relevant events: logins, config changes, review triggers, etc.

CREATE TABLE IF NOT EXISTS audit_log (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action VARCHAR(100) NOT NULL,                  -- login, logout, review.trigger, repo.create, repo.delete, org.create, admin.config_change, etc.
    resource_type VARCHAR(100),                    -- user, repo, review, org, webhook, system
    resource_id VARCHAR(255),                      -- UUID or other identifier of the affected resource
    ip_address INET,
    user_agent TEXT,
    metadata JSONB DEFAULT '{}',                   -- additional context (provider, old_value, new_value, etc.)
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_user ON audit_log(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_action ON audit_log(action);
CREATE INDEX IF NOT EXISTS idx_audit_log_resource ON audit_log(resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at DESC);

-- ============================================================================
-- SCHEMA VERSION TRACKING
-- ============================================================================
-- Simple version tracking table (no migration runner needed).

CREATE TABLE IF NOT EXISTS schema_info (
    version INTEGER NOT NULL,
    description TEXT,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO schema_info (version, description)
SELECT 1, 'Initial schema — users, orgs, repos, reviews, webhooks'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 1);

INSERT INTO schema_info (version, description)
SELECT 2, 'Add audit_log table for security event tracking'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 2);

-- ============================================================================
-- UPDATED_AT TRIGGER
-- ============================================================================

CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Drop triggers first (safe for re-run)
DROP TRIGGER IF EXISTS trg_users_updated_at ON users;
DROP TRIGGER IF EXISTS trg_oauth_identities_updated_at ON oauth_identities;
DROP TRIGGER IF EXISTS trg_organizations_updated_at ON organizations;
DROP TRIGGER IF EXISTS trg_repositories_updated_at ON repositories;

CREATE TRIGGER trg_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_oauth_identities_updated_at
    BEFORE UPDATE ON oauth_identities
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_organizations_updated_at
    BEFORE UPDATE ON organizations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_repositories_updated_at
    BEFORE UPDATE ON repositories
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

