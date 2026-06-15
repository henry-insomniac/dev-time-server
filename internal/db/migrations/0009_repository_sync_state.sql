ALTER TABLE repositories
ADD COLUMN IF NOT EXISTS sync_status text NOT NULL DEFAULT 'not_synced',
ADD COLUMN IF NOT EXISTS last_synced_at timestamptz,
ADD COLUMN IF NOT EXISTS sync_failure_reason text;
