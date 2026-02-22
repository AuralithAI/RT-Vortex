-- AI-PR-Reviewer Database Schema
-- PostgreSQL 15+

-- Enable extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";   -- gen_random_bytes() for token hashing

-- ============================================================================
-- REPOSITORIES
-- ============================================================================

CREATE TABLE repositories (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    external_id VARCHAR(255) NOT NULL UNIQUE,
    platform VARCHAR(50) NOT NULL,
    owner VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    default_branch VARCHAR(100) DEFAULT 'main',
    clone_url TEXT,
    webhook_secret VARCHAR(255),
    config JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_repositories_platform ON repositories(platform);
CREATE INDEX idx_repositories_owner ON repositories(owner);

-- ============================================================================
-- INDEX STATUS
-- ============================================================================

CREATE TABLE index_jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    job_type VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    progress DECIMAL(5, 2) DEFAULT 0,
    files_total INTEGER DEFAULT 0,
    files_processed INTEGER DEFAULT 0,
    error_message TEXT,
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_index_jobs_repository ON index_jobs(repository_id);
CREATE INDEX idx_index_jobs_status ON index_jobs(status);

CREATE TABLE index_stats (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repository_id UUID NOT NULL UNIQUE REFERENCES repositories(id) ON DELETE CASCADE,
    index_version VARCHAR(50),
    total_files INTEGER DEFAULT 0,
    indexed_files INTEGER DEFAULT 0,
    total_chunks INTEGER DEFAULT 0,
    total_symbols INTEGER DEFAULT 0,
    index_size_bytes BIGINT DEFAULT 0,
    last_commit VARCHAR(64),
    files_by_language JSONB DEFAULT '{}',
    last_indexed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- ============================================================================
-- REVIEWS
-- ============================================================================

CREATE TABLE reviews (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    pr_number INTEGER NOT NULL,
    pr_title TEXT,
    pr_description TEXT,
    base_branch VARCHAR(255),
    head_branch VARCHAR(255),
    base_commit VARCHAR(64),
    head_commit VARCHAR(64),
    author VARCHAR(255),
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    summary TEXT,
    llm_provider VARCHAR(50),
    llm_model VARCHAR(100),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,

    UNIQUE(repository_id, pr_number, head_commit)
);

CREATE INDEX idx_reviews_repository ON reviews(repository_id);
CREATE INDEX idx_reviews_status ON reviews(status);
CREATE INDEX idx_reviews_created ON reviews(created_at DESC);

CREATE TABLE review_comments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    review_id UUID NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
    file_path TEXT NOT NULL,
    line_number INTEGER,
    end_line_number INTEGER,
    severity VARCHAR(20) NOT NULL,
    category VARCHAR(50) NOT NULL,
    source VARCHAR(50) NOT NULL,
    rule_id VARCHAR(100),
    message TEXT NOT NULL,
    suggestion TEXT,
    code_snippet TEXT,
    confidence DECIMAL(3, 2),
    posted_to_platform BOOLEAN DEFAULT FALSE,
    platform_comment_id VARCHAR(100),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_review_comments_review ON review_comments(review_id);
CREATE INDEX idx_review_comments_severity ON review_comments(severity);

-- ============================================================================
-- METRICS
-- ============================================================================

CREATE TABLE review_metrics (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    review_id UUID NOT NULL UNIQUE REFERENCES reviews(id) ON DELETE CASCADE,
    total_files INTEGER DEFAULT 0,
    lines_added INTEGER DEFAULT 0,
    lines_deleted INTEGER DEFAULT 0,
    context_chunks_used INTEGER DEFAULT 0,
    tokens_prompt INTEGER DEFAULT 0,
    tokens_completion INTEGER DEFAULT 0,
    llm_latency_ms INTEGER,
    total_latency_ms INTEGER,
    heuristic_findings INTEGER DEFAULT 0,
    llm_findings INTEGER DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- ============================================================================
-- API KEYS
-- ============================================================================

CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    key_hash VARCHAR(64) NOT NULL UNIQUE,
    key_prefix VARCHAR(10) NOT NULL,
    scopes JSONB DEFAULT '["read", "write"]',
    rate_limit_per_minute INTEGER DEFAULT 100,
    expires_at TIMESTAMP WITH TIME ZONE,
    last_used_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    revoked_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_api_keys_prefix ON api_keys(key_prefix);
CREATE INDEX idx_api_keys_hash ON api_keys(key_hash);

-- ============================================================================
-- WEBHOOK EVENTS
-- ============================================================================

CREATE TABLE webhook_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    platform VARCHAR(50) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    delivery_id VARCHAR(100),
    payload JSONB NOT NULL,
    signature VARCHAR(255),
    processed BOOLEAN DEFAULT FALSE,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    processed_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_webhook_events_platform ON webhook_events(platform);
CREATE INDEX idx_webhook_events_processed ON webhook_events(processed);
CREATE INDEX idx_webhook_events_created ON webhook_events(created_at DESC);

-- ============================================================================
-- CONFIGURATION
-- ============================================================================

CREATE TABLE configurations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    scope VARCHAR(50) NOT NULL,
    scope_id VARCHAR(255),
    key VARCHAR(255) NOT NULL,
    value JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    UNIQUE(scope, scope_id, key)
);

CREATE INDEX idx_configurations_scope ON configurations(scope, scope_id);

-- ============================================================================
-- AUDIT LOG
-- ============================================================================

CREATE TABLE audit_log (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    actor VARCHAR(255),
    action VARCHAR(100) NOT NULL,
    resource_type VARCHAR(100),
    resource_id VARCHAR(255),
    details JSONB,
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_audit_log_actor ON audit_log(actor);
CREATE INDEX idx_audit_log_action ON audit_log(action);
CREATE INDEX idx_audit_log_created ON audit_log(created_at DESC);

-- ============================================================================
-- FUNCTIONS
-- ============================================================================

-- Update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Apply to tables
CREATE TRIGGER update_repositories_updated_at
    BEFORE UPDATE ON repositories
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_index_stats_updated_at
    BEFORE UPDATE ON index_stats
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_configurations_updated_at
    BEFORE UPDATE ON configurations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================================
-- INITIAL DATA
-- ============================================================================

-- Default global configuration
INSERT INTO configurations (scope, scope_id, key, value) VALUES
    ('global', NULL, 'review.default_checks', '["security", "performance", "style"]'),
    ('global', NULL, 'review.min_severity', '"warning"'),
    ('global', NULL, 'index.max_file_size_kb', '1024'),
    ('global', NULL, 'index.chunk_size', '512'),
    ('global', NULL, 'llm.default_provider', '"openai"'),
    ('global', NULL, 'llm.default_model', '"gpt-4-turbo"'),
    ('global', NULL, 'rate_limit.reviews_per_hour', '500'),
    ('global', NULL, 'rate_limit.index_per_hour', '50');
