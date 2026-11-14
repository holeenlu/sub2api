-- Remove the global OAuth initialization template and the independent cost /
-- alias-mode extensions imported alongside the retired protocols. Keep native
-- account model mappings, billing multipliers, credentials and diagnostics.
DELETE FROM settings WHERE key IN ('oauth_initial_model_mappings', 'priority_scheduling_v1');

WITH changed AS (
    UPDATE accounts
    SET status = CASE
          WHEN platform = 'openai' AND type = 'oauth' AND parent_account_id IS NULL
            AND credentials->>'model_mapping_mode' = 'aliases' AND status = 'active'
          THEN 'disabled' ELSE status END,
        schedulable = CASE
          WHEN platform = 'openai' AND type = 'oauth' AND parent_account_id IS NULL
            AND credentials->>'model_mapping_mode' = 'aliases'
          THEN FALSE ELSE schedulable END,
        credentials = CASE WHEN jsonb_typeof(credentials) = 'object'
          THEN credentials - 'model_mapping_mode' ELSE credentials END,
        extra = CASE WHEN jsonb_typeof(extra) = 'object'
          THEN extra - 'cost_multiplier' - 'cost_multiplier_auto_sync' ELSE extra END,
        updated_at = NOW()
    WHERE credentials ? 'model_mapping_mode'
       OR extra ? 'cost_multiplier'
       OR extra ? 'cost_multiplier_auto_sync'
    RETURNING id
)
INSERT INTO scheduler_outbox (event_type, account_id)
SELECT 'account_changed', id FROM changed;

-- Explicit review is required before resuming former alias-mode OAuth accounts:
-- the original nonempty account mapping also constrains the accepted model set.
-- Never infer which saved mappings were inserted by the old template, and never
-- replace or erase administrator mappings, historical bills or diagnostic data.
