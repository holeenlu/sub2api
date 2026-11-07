-- Retire only the sub4api standalone provider. Preserve credentials/history and
-- all ranxi OpenAI OAuth Excel/BPS settings. Historical migrations are immutable.
WITH retired_accounts AS (
    UPDATE accounts SET status = 'disabled', schedulable = FALSE, updated_at = NOW()
    WHERE platform = 'openai_bps' AND deleted_at IS NULL
      AND (status IS DISTINCT FROM 'disabled' OR schedulable IS DISTINCT FROM FALSE)
    RETURNING id
)
INSERT INTO scheduler_outbox (event_type, account_id)
SELECT 'account_changed', id FROM retired_accounts;

WITH retired_groups AS (
    UPDATE groups SET status = 'disabled', updated_at = NOW()
    WHERE platform = 'openai_bps' AND deleted_at IS NULL
      AND status IS DISTINCT FROM 'disabled'
    RETURNING id
)
INSERT INTO scheduler_outbox (event_type, group_id)
SELECT 'group_changed', id FROM retired_groups;

UPDATE channel_monitors SET enabled = FALSE, updated_at = NOW()
WHERE enabled AND (provider = 'openai_bps' OR account_id IN (
    SELECT id FROM accounts WHERE platform = 'openai_bps'
));
UPDATE composite_model_routes SET enabled = FALSE, updated_at = NOW()
WHERE target_platform = 'openai_bps' AND enabled;
UPDATE scheduled_test_plans
SET enabled = FALSE, auto_recover = FALSE, next_run_at = NULL,
    running_until = NULL, updated_at = NOW()
WHERE account_id IN (SELECT id FROM accounts WHERE platform = 'openai_bps')
  AND (enabled OR auto_recover OR next_run_at IS NOT NULL OR running_until IS NOT NULL);
