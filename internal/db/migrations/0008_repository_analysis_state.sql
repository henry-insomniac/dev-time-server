ALTER TABLE repositories
ADD COLUMN IF NOT EXISTS analysis_enabled boolean NOT NULL DEFAULT true;
