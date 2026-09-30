-- Discovery snapshots never grant account access or override sales prices.
CREATE TABLE IF NOT EXISTS model_catalog_sources (
    source_key TEXT PRIMARY KEY,
    account_id BIGINT REFERENCES accounts(id) ON DELETE CASCADE,
    platform TEXT NOT NULL,
    scope_revision TEXT NOT NULL,
    current_revision TEXT,
    checked_at TIMESTAMPTZ,
    next_sync_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    failure_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    lease_token TEXT,
    lease_until TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS model_catalog_sources_due_idx ON model_catalog_sources(next_sync_at);
CREATE TABLE IF NOT EXISTS model_catalog_snapshots (
    source_key TEXT NOT NULL REFERENCES model_catalog_sources(source_key) ON DELETE CASCADE,
    revision TEXT NOT NULL,
    scope_revision TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    PRIMARY KEY(source_key, revision)
);
CREATE TABLE IF NOT EXISTS model_catalog_entries (
    source_key TEXT NOT NULL,
    revision TEXT NOT NULL,
    model_id TEXT NOT NULL,
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    PRIMARY KEY(source_key, revision, model_id),
    FOREIGN KEY(source_key, revision) REFERENCES model_catalog_snapshots(source_key, revision) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS official_price_versions (
    revision TEXT PRIMARY KEY,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object')
);
CREATE TABLE IF NOT EXISTS model_catalog_releases (
    id BIGSERIAL PRIMARY KEY,
    source_key TEXT NOT NULL REFERENCES model_catalog_sources(source_key) ON DELETE CASCADE,
    revision TEXT NOT NULL,
    previous_revision TEXT,
    price_revision TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    operation TEXT NOT NULL DEFAULT 'publish',
    FOREIGN KEY(source_key, revision) REFERENCES model_catalog_snapshots(source_key, revision)
);
CREATE INDEX IF NOT EXISTS model_catalog_releases_source_idx ON model_catalog_releases(source_key, id DESC);

-- Request-scoped pricing evidence follows usage_logs' logical deduplication key.
CREATE TABLE IF NOT EXISTS model_request_price_versions (
    api_key_id BIGINT NOT NULL,
    request_id TEXT NOT NULL,
    price_revision TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(api_key_id, request_id)
);
CREATE INDEX IF NOT EXISTS model_request_price_versions_created_idx ON model_request_price_versions(created_at);

CREATE TABLE IF NOT EXISTS model_catalog_jobs (
    id TEXT PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    payload JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS model_catalog_jobs_updated_idx ON model_catalog_jobs(updated_at);

CREATE TABLE IF NOT EXISTS model_catalog_observations (
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    scope_revision TEXT NOT NULL,
    model_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    driver_model TEXT NOT NULL DEFAULT '',
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(account_id, scope_revision, model_id, operation)
);
