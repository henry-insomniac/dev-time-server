CREATE TABLE IF NOT EXISTS repositories (
    id text PRIMARY KEY,
    github_id bigint NOT NULL UNIQUE,
    owner text NOT NULL,
    name text NOT NULL,
    full_name text NOT NULL UNIQUE,
    imported_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS projects (
    id text PRIMARY KEY,
    repository_id text NOT NULL UNIQUE REFERENCES repositories(id) ON DELETE CASCADE,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS github_events (
    id text PRIMARY KEY,
    repository_id text NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    delivery_id text NOT NULL UNIQUE,
    event_type text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS risk_signals (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    category text NOT NULL,
    severity integer NOT NULL CHECK (severity >= 0 AND severity <= 100),
    reason text NOT NULL,
    evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS risk_assessments (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    score integer NOT NULL CHECK (score >= 0 AND score <= 100),
    level text NOT NULL,
    trend text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_jobs (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    risk_assessment_id text REFERENCES risk_assessments(id) ON DELETE SET NULL,
    agent_type text NOT NULL,
    status text NOT NULL,
    trigger text NOT NULL,
    error_summary text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_artifacts (
    id text PRIMARY KEY,
    agent_job_id text NOT NULL REFERENCES agent_jobs(id) ON DELETE CASCADE,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    artifact jsonb NOT NULL,
    evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    model text,
    prompt_version text,
    token_usage jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS action_suggestions (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_artifact_id text REFERENCES agent_artifacts(id) ON DELETE SET NULL,
    action_type text NOT NULL,
    status text NOT NULL,
    target_ref text NOT NULL,
    draft_body text NOT NULL,
    evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
