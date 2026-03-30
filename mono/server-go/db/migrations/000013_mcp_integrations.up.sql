CREATE TABLE IF NOT EXISTS mcp_connections (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id            UUID REFERENCES organizations(id) ON DELETE CASCADE,
    is_org_level      BOOLEAN NOT NULL DEFAULT false,
    provider          TEXT NOT NULL CHECK (provider IN ('slack', 'ms365', 'gmail', 'discord')),
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'expired', 'revoked', 'error')),
    vault_key         TEXT NOT NULL,
    refresh_vault_key TEXT NOT NULL DEFAULT '',
    scopes            TEXT[] NOT NULL DEFAULT '{}',
    metadata          JSONB NOT NULL DEFAULT '{}',
    last_used_at      TIMESTAMPTZ,
    connected_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_connections_user_provider
    ON mcp_connections(user_id, provider) WHERE NOT is_org_level;
CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_connections_org_provider
    ON mcp_connections(org_id, provider) WHERE is_org_level;

CREATE INDEX IF NOT EXISTS idx_mcp_connections_user     ON mcp_connections(user_id);
CREATE INDEX IF NOT EXISTS idx_mcp_connections_org      ON mcp_connections(org_id) WHERE org_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_mcp_connections_provider ON mcp_connections(provider, status);
CREATE INDEX IF NOT EXISTS idx_mcp_connections_expires  ON mcp_connections(expires_at) WHERE expires_at IS NOT NULL AND status = 'active';

CREATE TABLE IF NOT EXISTS mcp_call_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL REFERENCES mcp_connections(id) ON DELETE CASCADE,
    agent_id      TEXT,
    task_id       TEXT,
    action        TEXT NOT NULL,
    input_hash    TEXT NOT NULL DEFAULT '',
    output_hash   TEXT NOT NULL DEFAULT '',
    latency_ms    INT NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'error', 'rate_limited', 'consent_denied')),
    error_message TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mcp_call_log_conn    ON mcp_call_log(connection_id);
CREATE INDEX IF NOT EXISTS idx_mcp_call_log_task    ON mcp_call_log(task_id) WHERE task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_mcp_call_log_created ON mcp_call_log(created_at DESC);
