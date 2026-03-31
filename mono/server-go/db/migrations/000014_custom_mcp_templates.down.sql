-- Rollback 000014: Custom MCP templates

DROP INDEX IF EXISTS idx_mcp_custom_templates_org;
DROP INDEX IF EXISTS idx_mcp_custom_templates_creator;
DROP INDEX IF EXISTS idx_mcp_custom_templates_name;
DROP TABLE IF EXISTS mcp_custom_templates;

-- Restore the original restrictive CHECK constraint.
ALTER TABLE mcp_connections DROP CONSTRAINT IF EXISTS mcp_connections_provider_check;
ALTER TABLE mcp_connections ADD CONSTRAINT mcp_connections_provider_check
    CHECK (provider IN ('slack', 'ms365', 'gmail', 'discord'));
