CREATE TABLE IF NOT EXISTS swarm_audit_events (
    id         UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    action     TEXT           NOT NULL,
    user_id    TEXT           NOT NULL DEFAULT '',
    repo_id    TEXT           NOT NULL DEFAULT '',
    build_id   TEXT           NOT NULL DEFAULT '',
    detail     JSONB          NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ    NOT NULL DEFAULT now()
);

CREATE INDEX idx_swarm_audit_events_build   ON swarm_audit_events (build_id)  WHERE build_id != '';
CREATE INDEX idx_swarm_audit_events_user    ON swarm_audit_events (user_id)   WHERE user_id  != '';
CREATE INDEX idx_swarm_audit_events_action  ON swarm_audit_events (action);
CREATE INDEX idx_swarm_audit_events_created ON swarm_audit_events (created_at DESC);
