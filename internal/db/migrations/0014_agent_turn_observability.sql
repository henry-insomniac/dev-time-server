ALTER TABLE agent_conversation_turns
ADD COLUMN IF NOT EXISTS trace_id text;

ALTER TABLE agent_conversation_turns
ADD COLUMN IF NOT EXISTS model_calls jsonb NOT NULL DEFAULT '[]'::jsonb;
