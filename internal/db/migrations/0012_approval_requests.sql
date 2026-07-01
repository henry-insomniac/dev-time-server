CREATE TABLE IF NOT EXISTS approval_requests (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    action_suggestion_id text REFERENCES action_suggestions(id) ON DELETE SET NULL,
    action_type text NOT NULL,
    status text NOT NULL,
    target_ref text NOT NULL,
    draft_body text NOT NULL,
    risk_level text NOT NULL DEFAULT 'medium',
    evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    before_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    after_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    approval_token_hash text NOT NULL,
    approval_token_used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_audit_events (
    id text PRIMARY KEY,
    approval_request_id text REFERENCES approval_requests(id) ON DELETE CASCADE,
    action_suggestion_id text REFERENCES action_suggestions(id) ON DELETE SET NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS agent_audit_events_approval_request_id_idx
ON agent_audit_events(approval_request_id);
