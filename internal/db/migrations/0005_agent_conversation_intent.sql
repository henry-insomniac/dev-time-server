ALTER TABLE agent_conversation_turns
ADD COLUMN IF NOT EXISTS intent text NOT NULL DEFAULT 'risk_explain';
