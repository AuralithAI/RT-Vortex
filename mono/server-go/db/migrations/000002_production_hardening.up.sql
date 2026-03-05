-- ============================================================================
-- RTVortex Database Schema — Migration 002: Production Hardening
-- ============================================================================
-- Adds webhook_deliveries table for reliable retry delivery, and
-- plan_limits table for configurable quota enforcement.
-- ============================================================================

-- Webhook Deliveries: reliable retry queue for webhook-triggered reviews
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    webhook_event_id UUID REFERENCES webhook_events(id) ON DELETE SET NULL,
    repo_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    platform VARCHAR(50) NOT NULL,
    pr_number INTEGER NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',     -- pending, processing, completed, failed, dead
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    last_error TEXT,
    next_retry_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_status ON webhook_deliveries(status);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_retry ON webhook_deliveries(status, next_retry_at)
    WHERE status = 'failed';
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_repo ON webhook_deliveries(repo_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_created ON webhook_deliveries(created_at DESC);

-- Plan Limits: configurable per-plan resource limits (overrides defaults)
CREATE TABLE IF NOT EXISTS plan_limits (
    plan VARCHAR(50) PRIMARY KEY,
    reviews_per_day INTEGER NOT NULL DEFAULT 10,
    repos_per_org INTEGER NOT NULL DEFAULT 5,
    members_per_org INTEGER NOT NULL DEFAULT 5,
    tokens_per_day BIGINT NOT NULL DEFAULT 100000,
    max_file_size_kb INTEGER NOT NULL DEFAULT 256,
    indexing_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed default plan limits
INSERT INTO plan_limits (plan, reviews_per_day, repos_per_org, members_per_org, tokens_per_day, max_file_size_kb, indexing_enabled)
VALUES
    ('free', 10, 5, 5, 100000, 256, FALSE),
    ('pro', 100, 50, 50, 1000000, 512, TRUE),
    ('enterprise', -1, -1, -1, -1, 1024, TRUE)
ON CONFLICT (plan) DO NOTHING;

-- Schema version tracking
INSERT INTO schema_info (version, description)
SELECT 3, 'Add webhook_deliveries, plan_limits tables for production hardening'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 3);
