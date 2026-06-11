CREATE TABLE IF NOT EXISTS agent_conversations (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    latest_risk_assessment_id text NOT NULL REFERENCES risk_assessments(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_conversation_turns (
    id text PRIMARY KEY,
    conversation_id text NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
    role text NOT NULL,
    user_message text NOT NULL,
    agent_response text NOT NULL,
    evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
