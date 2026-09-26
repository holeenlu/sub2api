-- Retire non-ticket fork integrations without deleting configuration or history.
-- Historical migration files remain immutable for already deployed databases.
-- Explicitly selected BPS/Copilot accounts must not silently switch protocols.
WITH retired_accounts AS (
    UPDATE accounts
    SET status = 'disabled', schedulable = FALSE, updated_at = NOW()
    WHERE deleted_at IS NULL
      AND (platform = 'openai_bps'
           OR (platform = 'openai' AND (
               (type = 'apikey' AND extra->'openai_copilot_sdk' = 'true'::jsonb)
               OR (type = 'oauth' AND extra->'openai_excel_bps' = 'true'::jsonb))))
      AND (status IS DISTINCT FROM 'disabled' OR schedulable IS DISTINCT FROM FALSE)
    RETURNING id
)
INSERT INTO scheduler_outbox (event_type, account_id)
SELECT 'account_changed', id FROM retired_accounts;

WITH retired_groups AS (
    UPDATE groups
    SET status = 'disabled', updated_at = NOW()
    WHERE platform = 'openai_bps' AND deleted_at IS NULL
      AND status IS DISTINCT FROM 'disabled'
    RETURNING id
)
INSERT INTO scheduler_outbox (event_type, group_id)
SELECT 'group_changed', id FROM retired_groups;

UPDATE channel_monitors
SET enabled = FALSE, updated_at = NOW()
WHERE enabled AND (provider = 'openai_bps' OR account_id IN (
    SELECT id FROM accounts
    WHERE platform = 'openai_bps'
       OR (platform = 'openai' AND (
           (type = 'apikey' AND extra->'openai_copilot_sdk' = 'true'::jsonb)
           OR (type = 'oauth' AND extra->'openai_excel_bps' = 'true'::jsonb)))
));

UPDATE composite_model_routes
SET enabled = FALSE, updated_at = NOW()
WHERE target_platform = 'openai_bps' AND enabled;

-- The remaining scheduled-test runner only understands connectivity plans.
-- Keep retired plans and results, but never reinterpret them as normal probes.
UPDATE scheduled_test_plans
SET enabled = FALSE, auto_recover = FALSE, next_run_at = NULL,
    running_until = NULL, updated_at = NOW()
WHERE (pelican_config IS NOT NULL OR account_id IN (
    SELECT id FROM accounts
    WHERE platform = 'openai_bps'
       OR (platform = 'openai' AND (
           (type = 'apikey' AND extra->'openai_copilot_sdk' = 'true'::jsonb)
           OR (type = 'oauth' AND extra->'openai_excel_bps' = 'true'::jsonb)))
))
  AND (enabled OR auto_recover OR next_run_at IS NOT NULL OR running_until IS NOT NULL);

-- Removing the observer role revokes account-management access, while preserving
-- ordinary user access, balances, allowed_groups, and the previous scope history.
-- The existing users trigger invalidates API-key auth snapshots transactionally.
UPDATE users SET role = 'user', updated_at = NOW() WHERE role = 'observer';

-- Preserve targeting JSON. Old editors cannot round-trip user_ids, so archive
-- these announcements instead of allowing a later edit to widen their audience.
UPDATE announcements
SET status = 'archived', updated_at = NOW()
WHERE status IS DISTINCT FROM 'archived'
  AND jsonb_path_exists(targeting, '$.any_of[*].all_of[*] ? (@.type == "user")');
