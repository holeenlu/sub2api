-- Internal account-probe consumption facts, not customer billing commands.
-- Claim and outcome observations are cumulative snapshots, never additive
-- charges. No account FK: deleting or reauthorizing an account must not erase
-- already accepted consumption. This repository only inserts and reads facts.
CREATE TABLE IF NOT EXISTS account_probe_usage (
    probe_id TEXT NOT NULL CHECK (length(probe_id) = 64),
    account_id BIGINT NOT NULL CHECK (account_id > 0),
    claim_at TEXT NOT NULL CHECK (claim_at <> ''),
    scope TEXT NOT NULL CHECK (scope = 'internal_account_bps_recovery'),
    model TEXT NOT NULL CHECK (model <> ''),
    observation_kind TEXT NOT NULL CHECK (observation_kind IN ('claim', 'outcome')),
    payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64),
    payload BYTEA NOT NULL CHECK (octet_length(payload) > 0 AND octet_length(payload) <= 16384),
    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (probe_id, observation_kind, payload_sha256)
);

CREATE UNIQUE INDEX IF NOT EXISTS account_probe_usage_one_claim
    ON account_probe_usage (probe_id) WHERE observation_kind = 'claim';

COMMENT ON TABLE account_probe_usage IS
    'Immutable internal account/probe evidence; unknown is not zero, observations are not summed, and no row authorizes debit or recovery';
