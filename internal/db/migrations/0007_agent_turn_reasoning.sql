ALTER TABLE agent_conversation_turns
ADD COLUMN IF NOT EXISTS tool_calls jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE agent_conversation_turns
ADD COLUMN IF NOT EXISTS approval_request jsonb;

ALTER TABLE agent_conversation_turns
ADD COLUMN IF NOT EXISTS reasoning_trace jsonb NOT NULL DEFAULT '[]'::jsonb;
