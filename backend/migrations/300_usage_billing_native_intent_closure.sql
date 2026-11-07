-- 299 has already run in isolated test databases. Extend it forward, keeping
-- technical completion distinct from monetary settlement and unknown usage.
ALTER TABLE usage_billing_events ADD COLUMN native_intent_closure JSONB;
ALTER TABLE usage_billing_events ADD COLUMN native_intent_closure_fingerprint TEXT;
ALTER TABLE usage_billing_events DROP CONSTRAINT usage_billing_events_status_check;
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_status_check CHECK (
 (event_type='money' AND status IN ('pending','pending_pricing','settled','applied','failed','pending_usage','usage_recorded')) OR
 (event_type='native_intent' AND status IN ('intent_preserved','pending_usage','pending_preparation','intent_bound','intent_conflict','intent_not_sent'))
);
ALTER TABLE usage_billing_events ADD CONSTRAINT usage_billing_events_intent_closure_check CHECK (
 (status<>'intent_not_sent' AND native_intent_closure IS NULL AND native_intent_closure_fingerprint IS NULL) OR
 (event_type='native_intent' AND status='intent_not_sent' AND native_evidence IS NULL AND canonical_request_id IS NULL
  AND native_intent_closure IS NOT NULL AND native_intent_closure_fingerprint IS NOT NULL
  AND native_intent_closure_fingerprint ~ '^[0-9a-f]{64}$'
  AND COALESCE(native_intent_closure->>'Version'='1',false)
  AND COALESCE(native_intent_closure->'Key'->>'OperationID'=operation_id,false)
  AND COALESCE(native_intent_closure->'Key'->>'APIKeyID'=api_key_id::text,false)
  AND COALESCE(native_intent_closure->>'IntentFingerprint'=native_intent_fingerprint,false)
  AND CASE WHEN jsonb_typeof(native_intent_closure->'Attempts')='array'
    THEN jsonb_array_length(native_intent_closure->'Attempts') BETWEEN 1 AND 64
     AND NOT jsonb_path_exists(native_intent_closure,'$.Attempts[*] ? (!exists(@.Send) || @.Send != 1)')
    ELSE false END)
);
CREATE INDEX usage_billing_events_not_sent_retention_idx ON usage_billing_events(updated_at,journal_event_id)
 WHERE event_type='native_intent' AND status='intent_not_sent';
