-- First client event samples; duplicates only renew counters and last_seen_at.
CREATE TABLE IF NOT EXISTS ops_client_telemetry_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    group_id BIGINT,
    account_id BIGINT,
    client VARCHAR(16) NOT NULL,
    client_key VARCHAR(64) NOT NULL DEFAULT '',
    payload_hash VARCHAR(64) NOT NULL,
    request_path VARCHAR(128) NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    device_id TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL CHECK (octet_length(payload_json) <= 1048576),
    headers JSONB NOT NULL DEFAULT '{}',
    received_count BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status VARCHAR(32) NOT NULL DEFAULT 'received',
    upstream_status INTEGER,
    error_code VARCHAR(64) NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS ops_client_telemetry_payload_unique
    ON ops_client_telemetry_logs (user_id, client, payload_hash);
CREATE UNIQUE INDEX IF NOT EXISTS ops_client_telemetry_device_unique
    ON ops_client_telemetry_logs (user_id, client, client_key) WHERE client_key <> '';
CREATE INDEX IF NOT EXISTS ops_client_telemetry_seen_idx ON ops_client_telemetry_logs (last_seen_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS ops_client_telemetry_user_idx ON ops_client_telemetry_logs (user_id, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS ops_client_telemetry_key_idx ON ops_client_telemetry_logs (api_key_id, last_seen_at DESC);
