CREATE TABLE IF NOT EXISTS llm_provider_configs (
    id text PRIMARY KEY,
    provider text NOT NULL UNIQUE,
    base_url text NOT NULL,
    model text NOT NULL,
    api_key_ciphertext text NOT NULL,
    key_last_four text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
