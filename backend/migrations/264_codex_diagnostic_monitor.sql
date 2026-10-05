-- ModelTrace monitoring is separate from connectivity tests and ticket harvesting.
CREATE TABLE codex_diagnostic_plans (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key_id BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    models JSONB NOT NULL CHECK (jsonb_typeof(models) = 'array'),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    interval_minutes INTEGER NOT NULL DEFAULT 60 CHECK (interval_minutes BETWEEN 60 AND 10080 AND interval_minutes % 60 = 0),
    revision BIGINT NOT NULL DEFAULT 1,
    next_run_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX codex_diagnostic_plans_due ON codex_diagnostic_plans(next_run_at) WHERE enabled;

CREATE TABLE codex_diagnostic_runs (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    owner_id BIGINT NOT NULL,
    api_key_id BIGINT,
    api_key_name TEXT NOT NULL DEFAULT '',
    plan_revision BIGINT NOT NULL,
    models JSONB NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('manual','scheduled')),
    status TEXT NOT NULL CHECK (status IN ('queued','running','normal','degraded','uncertain','failed','canceled')),
    reason TEXT NOT NULL DEFAULT '',
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    lease_until TIMESTAMPTZ,
    worker_token TEXT,
    cancel_requested BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE UNIQUE INDEX codex_diagnostic_runs_active ON codex_diagnostic_runs(account_id)
    WHERE status IN ('queued','running');
CREATE INDEX codex_diagnostic_runs_history ON codex_diagnostic_runs(account_id, id DESC);
