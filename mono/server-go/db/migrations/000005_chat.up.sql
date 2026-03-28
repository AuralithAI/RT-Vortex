-- ============================================================================
-- RTVortex Database Schema — Migration 005: Repository Chat
-- ============================================================================
-- Adds chat_sessions and chat_messages tables for the repo-aware AI chat
-- feature. Messages are stored E2E-encrypted (AES-256-GCM, key held by the
-- Go server, never sent to the LLM).
-- ============================================================================

CREATE TABLE IF NOT EXISTS chat_sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repo_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT 'New Chat',

    -- Session metadata
    message_count INTEGER NOT NULL DEFAULT 0,
    last_message_at TIMESTAMPTZ,
    model TEXT DEFAULT '',           -- LLM model used (e.g. "gpt-4o")
    provider TEXT DEFAULT '',        -- LLM provider (e.g. "openai")

    -- E2E encryption: the per-session symmetric key is encrypted with the
    -- server's master key and stored here. Only the server can decrypt
    -- messages — the LLM never sees conversation history.
    encrypted_session_key TEXT,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS chat_messages (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id UUID NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    role VARCHAR(20) NOT NULL CHECK (role IN ('user', 'assistant', 'system')),

    -- Content is AES-256-GCM encrypted at rest using the session key.
    -- Plaintext is only in memory during request processing.
    content TEXT NOT NULL,
    encrypted BOOLEAN NOT NULL DEFAULT FALSE,

    -- For assistant messages: which engine chunks were used as context.
    -- Stored as JSONB so the UI can render citation links.
    citations JSONB DEFAULT '[]'::jsonb,

    -- Attachments (file drops, code snippets): stored as JSON metadata.
    attachments JSONB DEFAULT '[]'::jsonb,

    -- Token usage tracking (for the assistant message)
    prompt_tokens INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,

    -- Engine search metrics (for the assistant message)
    search_time_ms INTEGER DEFAULT 0,
    chunks_retrieved INTEGER DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_chat_sessions_repo ON chat_sessions(repo_id);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_user ON chat_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_repo_user ON chat_sessions(repo_id, user_id);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_updated ON chat_sessions(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_chat_messages_session ON chat_messages(session_id);
CREATE INDEX IF NOT EXISTS idx_chat_messages_created ON chat_messages(session_id, created_at ASC);

-- Auto-update updated_at on chat_sessions
CREATE TRIGGER trg_chat_sessions_updated_at
    BEFORE UPDATE ON chat_sessions FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- Schema version tracking
INSERT INTO schema_info (version, description)
SELECT 6, 'Add chat_sessions and chat_messages tables for repo-aware AI chat'
WHERE NOT EXISTS (SELECT 1 FROM schema_info WHERE version = 6);

-- Ensure the application role has full access even when migration is
-- executed by a superuser (e.g. postgres) instead of the rtvortex role.
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'rtvortex') THEN
        EXECUTE 'ALTER TABLE IF EXISTS chat_sessions OWNER TO rtvortex';
        EXECUTE 'ALTER TABLE IF EXISTS chat_messages OWNER TO rtvortex';
        EXECUTE 'GRANT ALL PRIVILEGES ON TABLE chat_sessions, chat_messages TO rtvortex';
    END IF;
END
$$;
