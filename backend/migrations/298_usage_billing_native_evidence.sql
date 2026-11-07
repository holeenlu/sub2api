-- Native unknown/partial facts use the existing journal, never a zero charge.
-- Deploy only with readers that exclude non-settlement states from monetary Claim.
ALTER TABLE usage_billing_events ADD COLUMN IF NOT EXISTS native_evidence JSONB;
ALTER TABLE usage_billing_events ADD COLUMN IF NOT EXISTS native_evidence_fingerprint TEXT;
ALTER TABLE usage_billing_events DROP CONSTRAINT IF EXISTS usage_billing_events_status_check;
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_status_check
 CHECK (status IN ('pending','pending_pricing','settled','applied','failed','pending_usage','usage_recorded'));
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_native_pair_check
 CHECK ((native_evidence IS NULL) = (native_evidence_fingerprint IS NULL));
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_native_state_check CHECK (
 native_evidence IS NULL OR
 (status IN ('pending_usage','usage_recorded') AND payload IS NULL AND request_fingerprint='') OR
 (status IN ('pending','pending_pricing','settled','failed') AND payload IS NOT NULL AND request_fingerprint<>'') OR
 (status='applied' AND request_fingerprint<>'')
);
CREATE INDEX IF NOT EXISTS usage_billing_events_settlement_ready_idx ON usage_billing_events(available_at,created_at)
 WHERE status IN ('pending','pending_pricing','settled','failed') AND payload IS NOT NULL;
DROP INDEX IF EXISTS usage_billing_events_ready_idx;
