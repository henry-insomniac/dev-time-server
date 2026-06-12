CREATE TABLE IF NOT EXISTS agent_trace_events (
    id text PRIMARY KEY,
    conversation_id text NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
    turn_id text NOT NULL REFERENCES agent_conversation_turns(id) ON DELETE CASCADE,
    event_type text NOT NULL,
    title text NOT NULL,
    body text NOT NULL,
    intent text NOT NULL,
    evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS agent_trace_events_conversation_id_idx
ON agent_trace_events(conversation_id);
