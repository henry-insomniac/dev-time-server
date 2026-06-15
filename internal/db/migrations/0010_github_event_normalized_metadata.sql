ALTER TABLE github_events
ADD COLUMN IF NOT EXISTS github_object_type text NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS github_object_id text NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS normalized_summary text NOT NULL DEFAULT '';
