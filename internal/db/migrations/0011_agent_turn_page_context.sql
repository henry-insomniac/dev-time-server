ALTER TABLE agent_conversation_turns
ADD COLUMN IF NOT EXISTS page_context jsonb;
