-- Technical pre-send intents and canonical billing events share one journal.
-- No technical operation ID is inserted into the canonical request_id column.
-- Existing money rows and their (request_id,api_key_id) uniqueness are preserved.
ALTER TABLE usage_billing_events ADD COLUMN journal_event_id BIGINT GENERATED ALWAYS AS IDENTITY;
ALTER TABLE usage_billing_events ADD COLUMN event_type TEXT NOT NULL DEFAULT 'money';
ALTER TABLE usage_billing_events ADD COLUMN operation_id TEXT;
ALTER TABLE usage_billing_events ADD COLUMN native_intent JSONB;
ALTER TABLE usage_billing_events ADD COLUMN native_intent_fingerprint TEXT;
ALTER TABLE usage_billing_events ADD COLUMN proposed_request_id TEXT;
ALTER TABLE usage_billing_events ADD COLUMN canonical_request_id TEXT;
CREATE UNIQUE INDEX usage_billing_events_money_key ON usage_billing_events(request_id,api_key_id);
ALTER TABLE usage_billing_events DROP CONSTRAINT usage_billing_events_pkey;
ALTER TABLE usage_billing_events ADD PRIMARY KEY (journal_event_id);
ALTER TABLE usage_billing_events ALTER COLUMN request_id DROP NOT NULL;
CREATE UNIQUE INDEX usage_billing_events_intent_key ON usage_billing_events(operation_id,api_key_id);
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_intent_pair_check
 CHECK ((native_intent IS NULL) = (native_intent_fingerprint IS NULL));
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_event_shape_check CHECK (
 (event_type='money' AND request_id IS NOT NULL AND operation_id IS NULL
  AND native_intent IS NULL AND native_intent_fingerprint IS NULL
  AND proposed_request_id IS NULL AND canonical_request_id IS NULL) OR
 (event_type='native_intent' AND request_id IS NULL AND operation_id IS NOT NULL AND btrim(operation_id)<>''
  AND native_intent IS NOT NULL AND native_intent_fingerprint IS NOT NULL
  AND payload IS NULL AND request_fingerprint='')
);
ALTER TABLE usage_billing_events DROP CONSTRAINT IF EXISTS usage_billing_events_status_check;
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_status_check CHECK (
 (event_type='money' AND status IN ('pending','pending_pricing','settled','applied','failed','pending_usage','usage_recorded')) OR
 (event_type='native_intent' AND status IN ('intent_preserved','pending_usage','pending_preparation','intent_bound','intent_conflict'))
);
ALTER TABLE usage_billing_events DROP CONSTRAINT IF EXISTS usage_billing_events_native_state_check;
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_native_state_check CHECK (
 event_type='native_intent' OR native_evidence IS NULL OR
 (status IN ('pending_usage','usage_recorded') AND payload IS NULL AND request_fingerprint='') OR
 (status IN ('pending','pending_pricing','settled','failed') AND payload IS NOT NULL AND request_fingerprint<>'') OR
 (status='applied' AND request_fingerprint<>'')
);
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_preparation_state_check CHECK (
 status<>'pending_preparation' OR (event_type='native_intent' AND native_evidence IS NOT NULL
  AND proposed_request_id IS NOT NULL AND btrim(proposed_request_id)<>'')
);
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_binding_check CHECK (
 (status='intent_bound')=(canonical_request_id IS NOT NULL)
 AND (canonical_request_id IS NULL OR (proposed_request_id IS NOT NULL AND canonical_request_id=proposed_request_id AND native_evidence IS NOT NULL))
);
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_canonical_fk
 FOREIGN KEY(canonical_request_id,api_key_id) REFERENCES usage_billing_events(request_id,api_key_id) ON DELETE CASCADE;
CREATE INDEX usage_billing_events_native_preparation_ready_idx
 ON usage_billing_events(available_at,created_at) WHERE event_type='native_intent' AND status='pending_preparation';

-- Bound completed technical facts follow their canonical event's existing
-- retention. Unbound/unknown/preparation-pending intents have no canonical FK
-- and must not be removed by monetary tombstone compaction.
CREATE INDEX usage_billing_events_canonical_link_idx ON usage_billing_events(canonical_request_id,api_key_id) WHERE canonical_request_id IS NOT NULL;
