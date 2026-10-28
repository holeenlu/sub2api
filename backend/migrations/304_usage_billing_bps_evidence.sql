-- BPS technical observations share the customer event journal. The monetary
-- request_id remains the actual provider request/response identity, never an
-- operation id. Existing money commands, deduplication and recovery are reused.
ALTER TABLE usage_billing_events ADD COLUMN bps_operation_id TEXT;
ALTER TABLE usage_billing_events ADD COLUMN bps_first_sequence BIGINT;
ALTER TABLE usage_billing_events ADD COLUMN bps_evidence JSONB;
ALTER TABLE usage_billing_events ADD COLUMN bps_evidence_fingerprint TEXT;
ALTER TABLE usage_billing_events ADD COLUMN bps_request_id TEXT;
CREATE UNIQUE INDEX usage_billing_events_bps_operation_key
 ON usage_billing_events(bps_operation_id,api_key_id,bps_first_sequence);
CREATE UNIQUE INDEX usage_billing_events_bps_money_owner_key
 ON usage_billing_events(bps_request_id,api_key_id) WHERE bps_request_id IS NOT NULL;
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_bps_canonical_fk
 FOREIGN KEY(bps_request_id,api_key_id) REFERENCES usage_billing_events(request_id,api_key_id) ON DELETE CASCADE;
ALTER TABLE usage_billing_events DROP CONSTRAINT usage_billing_events_event_shape_check;
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_event_shape_check CHECK (
 (event_type='money' AND request_id IS NOT NULL AND operation_id IS NULL
  AND native_intent IS NULL AND native_intent_fingerprint IS NULL
  AND proposed_request_id IS NULL AND canonical_request_id IS NULL) OR
 (event_type='native_intent' AND request_id IS NULL AND operation_id IS NOT NULL AND btrim(operation_id)<>''
  AND native_intent IS NOT NULL AND native_intent_fingerprint IS NOT NULL
  AND payload IS NULL AND request_fingerprint='') OR
 (event_type='bps_usage' AND request_id IS NULL AND operation_id IS NULL
  AND native_intent IS NULL AND native_intent_fingerprint IS NULL
  AND native_evidence IS NULL AND native_evidence_fingerprint IS NULL
  AND proposed_request_id IS NULL AND canonical_request_id IS NULL
  AND payload IS NULL AND request_fingerprint='')
);
ALTER TABLE usage_billing_events DROP CONSTRAINT usage_billing_events_status_check;
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_status_check CHECK (
 (event_type='money' AND status IN ('pending','pending_pricing','settled','applied','failed','pending_usage','usage_recorded')) OR
 (event_type='native_intent' AND status IN ('intent_preserved','pending_usage','pending_preparation','intent_bound','intent_conflict','intent_not_sent')) OR
 (event_type='bps_usage' AND status IN ('pending_usage','pending','usage_recorded','not_required'))
);
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_bps_shape_check CHECK (
 (event_type<>'bps_usage' AND bps_operation_id IS NULL AND bps_first_sequence IS NULL
  AND bps_evidence IS NULL AND bps_evidence_fingerprint IS NULL AND bps_request_id IS NULL) OR
 (event_type='bps_usage' AND bps_operation_id IS NOT NULL AND btrim(bps_operation_id)<>''
  AND bps_first_sequence IS NOT NULL AND bps_first_sequence>0 AND api_key_id>0 AND bps_evidence IS NOT NULL
  AND bps_evidence_fingerprint IS NOT NULL AND bps_evidence_fingerprint ~ '^[0-9a-f]{64}$'
  AND (status IN ('pending_usage','not_required') OR bps_request_id IS NOT NULL)
  AND (status<>'not_required' OR bps_request_id IS NULL))
);
CREATE INDEX usage_billing_events_bps_ready_idx ON usage_billing_events(available_at,created_at)
 WHERE event_type='bps_usage' AND status IN ('pending','pending_usage');
