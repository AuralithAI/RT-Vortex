-- Revert to original 4-provider constraint.
ALTER TABLE mcp_connections DROP CONSTRAINT IF EXISTS mcp_connections_provider_check;
ALTER TABLE mcp_connections ADD CONSTRAINT mcp_connections_provider_check
    CHECK (provider IN ('slack', 'ms365', 'gmail', 'discord'));
