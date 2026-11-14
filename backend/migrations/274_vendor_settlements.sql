-- Capture future usage attribution only. Never backfill from a mutable current owner.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS vendor_id_snapshot BIGINT;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS account_name_snapshot TEXT;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS group_name_snapshot TEXT;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS platform_snapshot TEXT;
CREATE INDEX IF NOT EXISTS idx_usage_logs_vendor_time ON usage_logs(vendor_id_snapshot,created_at,id) WHERE vendor_id_snapshot IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_usage_logs_vendor_account_time ON usage_logs(vendor_id_snapshot,account_id,created_at,id) WHERE vendor_id_snapshot IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_usage_logs_vendor_watermark ON usage_logs(vendor_id_snapshot,id) WHERE vendor_id_snapshot IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_usage_logs_supply_time ON usage_logs(created_at,id) WHERE vendor_id_snapshot IS NOT NULL;

-- Durable supplier accounting evidence; no FK to disposable usage_logs.
CREATE TABLE IF NOT EXISTS vendor_settlements (
 id BIGSERIAL PRIMARY KEY,
 number VARCHAR(64) NOT NULL UNIQUE,
 vendor_id BIGINT NOT NULL REFERENCES vendors(id) ON DELETE RESTRICT,
 kind VARCHAR(16) NOT NULL DEFAULT 'usage' CHECK (kind IN ('usage','adjustment','credit')),
 parent_statement_id BIGINT REFERENCES vendor_settlements(id) ON DELETE RESTRICT,
 status VARCHAR(24) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','pending_payment','partially_paid','paid','void')),
 version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
 start_at TIMESTAMPTZ NOT NULL,
 end_at TIMESTAMPTZ NOT NULL CHECK (end_at > start_at),
 timezone TEXT NOT NULL,
 snapshot_at TIMESTAMPTZ NOT NULL,
 watermark BIGINT NOT NULL CHECK (watermark >= 0),
 contract_version BIGINT NOT NULL CHECK (contract_version > 0),
 policy_version BIGINT NOT NULL CHECK (policy_version > 0),
 snapshot JSONB NOT NULL,
 snapshot_hash VARCHAR(64) NOT NULL,
 total NUMERIC(30,2) NOT NULL CHECK (total >= 0),
 paid NUMERIC(30,2) NOT NULL DEFAULT 0 CHECK (paid >= 0 AND paid <= total),
 idempotency_key VARCHAR(128) NOT NULL,
 input_hash VARCHAR(64) NOT NULL,
 created_by BIGINT NOT NULL,
 confirmed_by BIGINT,
 confirmed_at TIMESTAMPTZ,
 void_reason TEXT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(vendor_id, idempotency_key),
 CHECK ((kind='usage') OR parent_statement_id IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS vendor_settlements_vendor_period_idx ON vendor_settlements(vendor_id,start_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS vendor_settlements_status_idx ON vendor_settlements(status,id DESC);
CREATE INDEX IF NOT EXISTS vendor_settlements_parent_idx ON vendor_settlements(parent_statement_id) WHERE parent_statement_id IS NOT NULL;
CREATE TABLE IF NOT EXISTS vendor_settlement_lines (
 id BIGSERIAL PRIMARY KEY,
 statement_id BIGINT NOT NULL REFERENCES vendor_settlements(id) ON DELETE RESTRICT,
 group_id BIGINT,
 group_name TEXT NOT NULL,
 contract_version BIGINT NOT NULL CHECK (contract_version > 0),
 coefficient NUMERIC(30,10) NOT NULL CHECK (coefficient >= 0),
 cost_usd NUMERIC(38,20) NOT NULL CHECK (cost_usd >= 0),
 amount NUMERIC(30,2) NOT NULL,
 usage_count BIGINT NOT NULL CHECK (usage_count >= 0),
 kind VARCHAR(16) NOT NULL DEFAULT 'usage',
 reason TEXT NOT NULL DEFAULT '',
 UNIQUE(statement_id, id)
);
CREATE INDEX IF NOT EXISTS vendor_settlement_lines_statement_idx ON vendor_settlement_lines(statement_id,id);
CREATE TABLE IF NOT EXISTS vendor_settlement_allocations (
 id BIGSERIAL PRIMARY KEY,
 statement_id BIGINT NOT NULL REFERENCES vendor_settlements(id) ON DELETE RESTRICT,
 line_id BIGINT NOT NULL,
 usage_log_id BIGINT NOT NULL,
 source_fingerprint VARCHAR(64),
 vendor_id BIGINT NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 snapshot JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 FOREIGN KEY(statement_id,line_id) REFERENCES vendor_settlement_lines(statement_id,id) ON DELETE RESTRICT,
 UNIQUE(statement_id,usage_log_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS vendor_settlement_allocations_active_usage_idx ON vendor_settlement_allocations(usage_log_id) WHERE active;
CREATE UNIQUE INDEX IF NOT EXISTS vendor_settlement_allocations_active_source_idx ON vendor_settlement_allocations(source_fingerprint) WHERE active AND source_fingerprint IS NOT NULL;
CREATE INDEX IF NOT EXISTS vendor_settlement_allocations_statement_idx ON vendor_settlement_allocations(statement_id,id);
CREATE TABLE IF NOT EXISTS vendor_settlement_payments (
 id BIGSERIAL PRIMARY KEY,
 statement_id BIGINT NOT NULL REFERENCES vendor_settlements(id) ON DELETE RESTRICT,
 amount NUMERIC(30,2) NOT NULL CHECK (amount <> 0),
 currency VARCHAR(8) NOT NULL CHECK (currency='USDT'),
 paid_at TIMESTAMPTZ NOT NULL,
 channel VARCHAR(64) NOT NULL,
 reference VARCHAR(128) NOT NULL,
 actor_id BIGINT NOT NULL,
 idempotency_key VARCHAR(128) NOT NULL,
 input_hash VARCHAR(64) NOT NULL,
 reversal_of_id BIGINT REFERENCES vendor_settlement_payments(id) ON DELETE RESTRICT,
 reason TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(statement_id,idempotency_key),
 CHECK ((reversal_of_id IS NULL AND amount>0) OR (reversal_of_id IS NOT NULL AND amount<0 AND length(reason)>0))
);
CREATE UNIQUE INDEX IF NOT EXISTS vendor_settlement_payments_receipt_idx ON vendor_settlement_payments(channel,reference) WHERE reversal_of_id IS NULL;
CREATE INDEX IF NOT EXISTS vendor_settlement_payments_statement_idx ON vendor_settlement_payments(statement_id,id);
CREATE INDEX IF NOT EXISTS vendor_settlement_payments_reversal_idx ON vendor_settlement_payments(reversal_of_id) WHERE reversal_of_id IS NOT NULL;
