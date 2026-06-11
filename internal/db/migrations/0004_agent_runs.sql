CREATE TABLE IF NOT EXISTS agent_runs (
    id text PRIMARY KEY,
    agent_job_id text NOT NULL UNIQUE REFERENCES agent_jobs(id) ON DELETE CASCADE,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    risk_assessment_id text REFERENCES risk_assessments(id) ON DELETE SET NULL,
    agent_type text NOT NULL,
    status text NOT NULL,
    summary text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_steps (
    id text PRIMARY KEY,
    agent_run_id text NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    step_type text NOT NULL,
    status text NOT NULL,
    title text NOT NULL,
    body text NOT NULL,
    evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
