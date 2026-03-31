-- Expand the mcp_connections.provider CHECK constraint to allow all OAuth-integrated providers.
-- Previously only 'slack', 'ms365', 'gmail', 'discord' were allowed.

ALTER TABLE mcp_connections DROP CONSTRAINT IF EXISTS mcp_connections_provider_check;

ALTER TABLE mcp_connections ADD CONSTRAINT mcp_connections_provider_check
    CHECK (provider IN (
        -- Google Workspace
        'gmail', 'google_calendar', 'google_drive',
        -- Microsoft 365
        'ms365',
        -- Atlassian
        'jira', 'confluence',
        -- DevOps
        'github', 'gitlab',
        -- Communication
        'slack', 'discord',
        -- Productivity
        'notion',
        -- Project Management
        'linear', 'asana', 'trello',
        -- Design
        'figma',
        -- Support
        'zendesk',
        -- Monitoring
        'pagerduty', 'datadog',
        -- Finance
        'stripe',
        -- CRM
        'hubspot', 'salesforce',
        -- Messaging
        'twilio'
    ));

-- Fix table ownership: tables created by earlier migrations may have been owned
-- by 'postgres' if migrations ran under the superuser. Transfer to 'rtvortex'.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'rtvortex') THEN
        EXECUTE 'ALTER TABLE IF EXISTS mcp_connections     OWNER TO rtvortex';
        EXECUTE 'ALTER TABLE IF EXISTS mcp_call_log        OWNER TO rtvortex';
        EXECUTE 'ALTER TABLE IF EXISTS mcp_custom_templates OWNER TO rtvortex';
        EXECUTE 'ALTER TABLE IF EXISTS embedding_model_config OWNER TO rtvortex';
        EXECUTE 'ALTER TABLE IF EXISTS repo_assets         OWNER TO rtvortex';
        EXECUTE 'ALTER TABLE IF EXISTS model_download_status OWNER TO rtvortex';
    END IF;
END
$$;
