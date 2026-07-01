ALTER TABLE agent_conversation_turns
ADD COLUMN IF NOT EXISTS ui_blocks jsonb NOT NULL DEFAULT '[]'::jsonb;
