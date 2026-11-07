-- Separate account model references from account-specific upstream rewrites.
-- Existing public keys (including rewrite aliases) retain their exact restriction.
-- Passthrough continues to ignore both legacy and new account restrictions.
-- Other platforms retain their protocol-specific default mapping contracts.
WITH legacy AS (
 SELECT id,
   (SELECT COALESCE(jsonb_agg(k ORDER BY k),'[]'::jsonb)
    FROM jsonb_each(credentials->'model_mapping') kv(k,v) WHERE jsonb_typeof(v)='string') refs,
   (SELECT COALESCE(jsonb_object_agg(k,v),'{}'::jsonb)
    FROM jsonb_each(credentials->'model_mapping') kv(k,v) WHERE jsonb_typeof(v)='string' AND to_jsonb(k)<>v) rewrites
 FROM accounts
 WHERE platform IN ('anthropic','openai')
 AND NOT credentials ? 'model_allowlist'
 AND jsonb_typeof(credentials->'model_mapping')='object'
)
UPDATE accounts a SET credentials=jsonb_set(jsonb_set(a.credentials,'{model_allowlist}',legacy.refs),'{model_mapping}',legacy.rewrites)
FROM legacy WHERE a.id=legacy.id;

INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload)
SELECT 'account_changed',id,NULL,NULL FROM accounts WHERE credentials ? 'model_allowlist';
