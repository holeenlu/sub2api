-- Durable, replayable settlement events. No credentials or prompt bodies belong here.
CREATE TABLE IF NOT EXISTS usage_billing_events (
 request_id TEXT NOT NULL,
 api_key_id BIGINT NOT NULL,
 request_fingerprint TEXT NOT NULL,
 payload JSONB,
 status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','pending_pricing','settled','applied','failed')),
 attempts INTEGER NOT NULL DEFAULT 0,
 available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 last_error TEXT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY (request_id, api_key_id)
);
CREATE INDEX IF NOT EXISTS usage_billing_events_ready_idx ON usage_billing_events(available_at,created_at)
 WHERE status <> 'applied';

ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS billing_status TEXT NOT NULL DEFAULT 'unknown';

CREATE INDEX IF NOT EXISTS usage_billing_events_applied_idx ON usage_billing_events(updated_at)
 WHERE status = 'applied';
