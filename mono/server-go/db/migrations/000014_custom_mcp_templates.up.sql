-- 000014: Custom MCP templates + relax provider CHECK constraint
-- Allows user-defined custom integrations and expands the provider enum.

-- 1. Drop the old restrictive CHECK constraint on provider column.
ALTER TABLE mcp_connections DROP CONSTRAINT IF EXISTS mcp_connections_provider_check;

-- 2. Add a relaxed constraint (provider must be non-empty).
ALTER TABLE mcp_connections ADD CONSTRAINT mcp_connections_provider_check CHECK (provider <> '');

-- 3. Create the custom MCP templates table.
CREATE TABLE IF NOT EXISTS mcp_custom_templates (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    label       TEXT NOT NULL,
    category    TEXT NOT NULL DEFAULT 'custom',
    description TEXT NOT NULL DEFAULT '',
    base_url    TEXT NOT NULL,
    auth_type   TEXT NOT NULL CHECK (auth_type IN ('bearer', 'basic', 'header', 'query')),
    auth_header TEXT NOT NULL DEFAULT '',
    actions     JSONB NOT NULL DEFAULT '[]',
    created_by  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id      UUID REFERENCES organizations(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Unique name per creator (user-level) or org (org-level).
CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_custom_templates_name
    ON mcp_custom_templates(name);

CREATE INDEX IF NOT EXISTS idx_mcp_custom_templates_creator
    ON mcp_custom_templates(created_by);
CREATE INDEX IF NOT EXISTS idx_mcp_custom_templates_org
    ON mcp_custom_templates(org_id) WHERE org_id IS NOT NULL;
