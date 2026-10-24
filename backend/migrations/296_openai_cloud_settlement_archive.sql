-- Retain unresolved Cloud accounting facts outside hot Redis state. These are
-- not billable commands, successful settlements or manual review requests.
-- No foreign keys: deleting a customer/account must not erase accepted facts.
CREATE TABLE openai_cloud_settlement_archive (
    root_id TEXT NOT NULL,
    payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64),
    reason TEXT NOT NULL CHECK (reason IN ('unknown_usage', 'invalid_record', 'invalid_settlement', 'owner_missing', 'owner_invalid', 'billing_conflict')),
    payload BYTEA NOT NULL,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (root_id, payload_sha256)
);

COMMENT ON TABLE openai_cloud_settlement_archive IS
    'Immutable Cloud accounting evidence; does not imply zero usage, debit, or successful settlement';
