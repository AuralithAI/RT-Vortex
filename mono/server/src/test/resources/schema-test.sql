-- H2-compatible schema for integration tests (MODE=PostgreSQL)
-- Mirrors the production PostgreSQL schema but uses H2-compatible types

CREATE TABLE IF NOT EXISTS users (
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

CREATE TABLE IF NOT EXISTS user_sessions (
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

CREATE TABLE IF NOT EXISTS reviews (
    id UUID DEFAULT RANDOM_UUID() PRIMARY KEY,
    repo_id VARCHAR(500) NOT NULL,
    pr_number INTEGER,
    status VARCHAR(50),
    summary TEXT,
    overall_assessment VARCHAR(100),
    review_data CLOB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS index_jobs (
    id UUID DEFAULT RANDOM_UUID() PRIMARY KEY,
    repo_id VARCHAR(500) NOT NULL,
    state VARCHAR(50) DEFAULT 'PENDING',
    progress INTEGER DEFAULT 0,
    message VARCHAR(1024),
    error TEXT,
    files_processed INTEGER DEFAULT 0,
    start_time TIMESTAMP,
    end_time TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS subscription_history (
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

