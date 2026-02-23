-- ============================================================================
-- AI-PR-Reviewer Database Schema
-- PostgreSQL 15+
-- ============================================================================

-- Enable extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

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
-- USERS
-- ============================================================================

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    external_id VARCHAR(255) UNIQUE,
    platform VARCHAR(50) NOT NULL,
    username VARCHAR(255) NOT NULL,
    email VARCHAR(255),
    display_name VARCHAR(255),
    avatar_url TEXT,
    subscription_tier VARCHAR(20) NOT NULL DEFAULT 'FREE',
    preferences JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_login_at TIMESTAMP WITH TIME ZONE,

    CONSTRAINT chk_users_subscription_tier
        CHECK (subscription_tier IN ('FREE', 'PRO', 'ENTERPRISE'))
);

CREATE INDEX idx_users_platform ON users(platform);
CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_external ON users(external_id);
CREATE INDEX idx_users_tier ON users(subscription_tier);

-- ============================================================================
-- SUBSCRIPTION HISTORY
-- ============================================================================

CREATE TABLE subscription_history (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    previous_tier VARCHAR(20),
    new_tier VARCHAR(20) NOT NULL,
    changed_by VARCHAR(255),
    reason TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_subscription_history_user ON subscription_history(user_id);

-- ============================================================================
-- USER SESSIONS
-- ============================================================================

CREATE TABLE user_sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_token VARCHAR(64) NOT NULL UNIQUE,
    grpc_channel_id VARCHAR(100),
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    ip_address INET,
    user_agent TEXT,
    device_info JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    last_activity_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    revoked_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_user_sessions_user ON user_sessions(user_id);
CREATE INDEX idx_user_sessions_token ON user_sessions(session_token);
CREATE INDEX idx_user_sessions_status ON user_sessions(status);
CREATE INDEX idx_user_sessions_expires ON user_sessions(expires_at);

-- ============================================================================
-- SESSION ACTIVITY LOG
-- ============================================================================

CREATE TABLE session_activity (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id UUID NOT NULL REFERENCES user_sessions(id) ON DELETE CASCADE,
    action VARCHAR(100) NOT NULL,
    resource_type VARCHAR(50),
    resource_id VARCHAR(255),
    request_id VARCHAR(100),
    grpc_method VARCHAR(255),
    latency_ms INTEGER,
    success BOOLEAN DEFAULT TRUE,
    error_code VARCHAR(50),
    error_message TEXT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_session_activity_session ON session_activity(session_id);
CREATE INDEX idx_session_activity_action ON session_activity(action);
CREATE INDEX idx_session_activity_created ON session_activity(created_at DESC);

-- ============================================================================
-- GRPC CONNECTIONS
-- ============================================================================

CREATE TABLE grpc_connections (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    channel_id VARCHAR(100) NOT NULL UNIQUE,
    session_id UUID REFERENCES user_sessions(id) ON DELETE SET NULL,
    server_instance VARCHAR(255),
    status VARCHAR(20) NOT NULL DEFAULT 'connected',
    client_type VARCHAR(50),
    client_version VARCHAR(50),
    connected_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_heartbeat_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    disconnected_at TIMESTAMP WITH TIME ZONE,
    metadata JSONB DEFAULT '{}'
);

CREATE INDEX idx_grpc_connections_session ON grpc_connections(session_id);
CREATE INDEX idx_grpc_connections_status ON grpc_connections(status);

-- ============================================================================
-- USER REPOSITORY ACCESS
-- ============================================================================

CREATE TABLE user_repository_access (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    role VARCHAR(50) NOT NULL DEFAULT 'viewer',
    granted_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    granted_by UUID REFERENCES users(id),

    UNIQUE(user_id, repository_id)
);

CREATE INDEX idx_user_repo_access_user ON user_repository_access(user_id);
CREATE INDEX idx_user_repo_access_repo ON user_repository_access(repository_id);

-- ============================================================================
-- USER LLM CONFIGS
-- ============================================================================

CREATE TABLE user_llm_configs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(50) NOT NULL,
    config_name VARCHAR(100) NOT NULL,
    is_default BOOLEAN DEFAULT FALSE,
    config JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    UNIQUE(user_id, config_name)
);

CREATE INDEX idx_user_llm_configs_user ON user_llm_configs(user_id);

-- ============================================================================
-- INDEX JOBS
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

-- ============================================================================
-- INDEX STATS
-- ============================================================================

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

-- ============================================================================
-- REVIEW COMMENTS
-- ============================================================================

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
-- REVIEW METRICS
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

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION update_subscription_tier(
    p_user_id UUID,
    p_new_tier VARCHAR(20),
    p_changed_by VARCHAR(255) DEFAULT 'system',
    p_reason TEXT DEFAULT NULL
) RETURNS VOID AS $$
DECLARE
    v_old_tier VARCHAR(20);
BEGIN
    SELECT subscription_tier INTO v_old_tier FROM users WHERE id = p_user_id;
    IF v_old_tier IS NULL THEN
        RAISE EXCEPTION 'User % not found', p_user_id;
    END IF;
    IF v_old_tier = p_new_tier THEN RETURN; END IF;

    UPDATE users SET subscription_tier = p_new_tier, updated_at = NOW()
    WHERE id = p_user_id;

    INSERT INTO subscription_history (user_id, previous_tier, new_tier, changed_by, reason)
    VALUES (p_user_id, v_old_tier, p_new_tier, p_changed_by, p_reason);
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION cleanup_expired_sessions()
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    UPDATE user_sessions
    SET status = 'expired'
    WHERE status = 'active' AND expires_at < NOW();
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION get_active_session(p_token VARCHAR)
RETURNS TABLE (
    session_id UUID,
    user_id UUID,
    grpc_channel_id VARCHAR,
    expires_at TIMESTAMP WITH TIME ZONE
) AS $$
BEGIN
    UPDATE user_sessions
    SET last_activity_at = NOW()
    WHERE session_token = p_token AND status = 'active' AND expires_at > NOW();

    RETURN QUERY
    SELECT us.id, us.user_id, us.grpc_channel_id, us.expires_at
    FROM user_sessions us
    WHERE us.session_token = p_token
      AND us.status = 'active'
      AND us.expires_at > NOW();
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- TRIGGERS
-- ============================================================================

CREATE TRIGGER update_repositories_updated_at
    BEFORE UPDATE ON repositories
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_index_stats_updated_at
    BEFORE UPDATE ON index_stats
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_configurations_updated_at
    BEFORE UPDATE ON configurations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_user_llm_configs_updated_at
    BEFORE UPDATE ON user_llm_configs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================================
-- INITIAL DATA
-- ============================================================================

INSERT INTO configurations (scope, scope_id, key, value) VALUES
    ('global', NULL, 'review.default_checks', '["security", "performance", "style"]'),
    ('global', NULL, 'review.min_severity', '"warning"'),
    ('global', NULL, 'index.max_file_size_kb', '1024'),
    ('global', NULL, 'index.chunk_size', '512'),
    ('global', NULL, 'llm.default_provider', '"openai"'),
    ('global', NULL, 'llm.default_model', '"gpt-4-turbo"'),
    ('global', NULL, 'rate_limit.reviews_per_hour', '500'),
    ('global', NULL, 'rate_limit.index_per_hour', '50');

