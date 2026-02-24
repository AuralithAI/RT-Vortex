-- H2-compatible schema for integration tests (MODE=PostgreSQL)
-- Mirrors the production PostgreSQL schema but uses H2-compatible types

-- Drop tables in reverse dependency order to ensure clean state
DROP TABLE IF EXISTS subscription_history;
DROP TABLE IF EXISTS review_metrics;
DROP TABLE IF EXISTS review_comments;
DROP TABLE IF EXISTS reviews;
DROP TABLE IF EXISTS index_stats;
DROP TABLE IF EXISTS index_jobs;
DROP TABLE IF EXISTS user_sessions;
DROP TABLE IF EXISTS users;

CREATE TABLE users (
    id UUID DEFAULT RANDOM_UUID() PRIMARY KEY,
    platform VARCHAR(50),
    username VARCHAR(255),
    email VARCHAR(255),
    display_name VARCHAR(255),
    avatar_url VARCHAR(1024),
    subscription_tier VARCHAR(50) DEFAULT 'FREE',
    last_login_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE user_sessions (
    id UUID DEFAULT RANDOM_UUID() PRIMARY KEY,
    user_id UUID,
    session_token VARCHAR(255) NOT NULL UNIQUE,
    status VARCHAR(50) DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP,
    last_activity_at TIMESTAMP,
    revoked_at TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE reviews (
    id UUID DEFAULT RANDOM_UUID() PRIMARY KEY,
    repo_id VARCHAR(500),
    repository_id VARCHAR(500),
    pr_number INTEGER,
    status VARCHAR(50),
    summary TEXT,
    overall_assessment VARCHAR(100),
    review_data CLOB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE review_comments (
    id UUID DEFAULT RANDOM_UUID() PRIMARY KEY,
    review_id UUID,
    file_path TEXT,
    line_number INTEGER,
    end_line_number INTEGER,
    severity VARCHAR(20),
    category VARCHAR(50),
    source VARCHAR(50),
    rule_id VARCHAR(100),
    message TEXT,
    suggestion TEXT,
    code_snippet TEXT,
    confidence DOUBLE,
    posted_to_platform BOOLEAN DEFAULT FALSE,
    platform_comment_id VARCHAR(100),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (review_id) REFERENCES reviews(id) ON DELETE CASCADE
);

CREATE TABLE review_metrics (
    id UUID DEFAULT RANDOM_UUID() PRIMARY KEY,
    review_id UUID UNIQUE,
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
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (review_id) REFERENCES reviews(id) ON DELETE CASCADE
);

CREATE TABLE index_jobs (
    id UUID DEFAULT RANDOM_UUID() PRIMARY KEY,
    repo_id VARCHAR(500),
    repository_id VARCHAR(500),
    job_type VARCHAR(50) DEFAULT 'full',
    status VARCHAR(50) DEFAULT 'pending',
    state VARCHAR(50) DEFAULT 'PENDING',
    progress DOUBLE DEFAULT 0,
    message VARCHAR(1024),
    error TEXT,
    error_message TEXT,
    files_processed INTEGER DEFAULT 0,
    start_time TIMESTAMP,
    started_at TIMESTAMP,
    end_time TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE index_stats (
    id UUID DEFAULT RANDOM_UUID() PRIMARY KEY,
    repository_id VARCHAR(500) UNIQUE,
    index_version VARCHAR(100),
    total_files INTEGER DEFAULT 0,
    indexed_files INTEGER DEFAULT 0,
    total_chunks INTEGER DEFAULT 0,
    total_symbols INTEGER DEFAULT 0,
    last_commit VARCHAR(255),
    last_indexed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE subscription_history (
    id UUID DEFAULT RANDOM_UUID() PRIMARY KEY,
    user_id UUID,
    old_tier VARCHAR(50),
    new_tier VARCHAR(50),
    changed_by VARCHAR(255),
    reason VARCHAR(1024),
    changed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- H2-compatible function to mimic update_subscription_tier stored procedure
CREATE ALIAS IF NOT EXISTS update_subscription_tier FOR "ai.aipr.server.test.H2Functions.updateSubscriptionTier";

