-- A technical operation binds at most one canonical request/key; the 299
-- proposal equality/FK already enforces that direction. Different operations
-- cannot silently share a charge merely because provider IDs or prices match.
-- No existing money row, fingerprint, amount or payload is rewritten.
CREATE UNIQUE INDEX usage_billing_events_native_canonical_owner_key
 ON usage_billing_events(canonical_request_id,api_key_id)
 WHERE event_type='native_intent' AND canonical_request_id IS NOT NULL;
