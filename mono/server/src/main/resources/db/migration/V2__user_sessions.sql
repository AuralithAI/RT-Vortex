-- V2: User Session Management
-- Adds tables for user sessions with remote gRPC mapping

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
    preferences JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_login_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_users_platform ON users(platform);
CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_external ON users(external_id);

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
-- GRPC CONNECTIONS (for tracking remote connections)
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
-- LLM PROVIDER CONNECTIONS (per user/session)
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
-- TRIGGERS
-- ============================================================================

CREATE TRIGGER update_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_user_llm_configs_updated_at
    BEFORE UPDATE ON user_llm_configs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================================
-- FUNCTIONS
-- ============================================================================

-- Clean up expired sessions
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

-- Get active session by token
CREATE OR REPLACE FUNCTION get_active_session(p_token VARCHAR)
RETURNS TABLE (
    session_id UUID,
    user_id UUID,
    grpc_channel_id VARCHAR,
    expires_at TIMESTAMP WITH TIME ZONE
) AS $$
BEGIN
    -- Update last activity
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
