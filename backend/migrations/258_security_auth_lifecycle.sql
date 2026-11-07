-- Zero preserves pre-migration JWT credential fingerprints. Revocation is durable.
ALTER TABLE users ADD COLUMN IF NOT EXISTS session_generation BIGINT NOT NULL DEFAULT 0;
-- Old pending capabilities carry no originating credential snapshot; restart their short flow.
UPDATE pending_auth_sessions SET consumed_at = CURRENT_TIMESTAMP
WHERE consumed_at IS NULL AND target_user_id IS NOT NULL;
ALTER TABLE pending_auth_sessions DROP CONSTRAINT IF EXISTS pending_auth_sessions_intent_check;
ALTER TABLE pending_auth_sessions ADD CONSTRAINT pending_auth_sessions_intent_check
CHECK (intent IN ('login', 'bind_current_user', 'adopt_existing_user_by_email', 'authorize_bind'));
