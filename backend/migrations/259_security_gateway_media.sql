-- Durable tenant ownership and asynchronous media settlement. No TTL: unknown
-- submissions retain their capacity until an operator reconciles them.
CREATE TABLE IF NOT EXISTS gateway_media_voices (
 account_id BIGINT NOT NULL, voice_id TEXT NOT NULL,
 group_id BIGINT NOT NULL, user_id BIGINT NOT NULL,
 metadata JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(account_id, voice_id)
);
CREATE INDEX IF NOT EXISTS gateway_media_voices_owner ON gateway_media_voices(user_id,group_id);
CREATE TABLE IF NOT EXISTS gateway_media_jobs (
 id TEXT PRIMARY KEY, version BIGINT NOT NULL DEFAULT 0, user_id BIGINT NOT NULL, group_id BIGINT NOT NULL,
 account_id BIGINT NOT NULL, task_id TEXT,
 state TEXT NOT NULL CHECK(state IN ('reserving','submitting','pending','billing','releasing','settled','failed')),
 snapshot JSONB NOT NULL,
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- One outstanding paid video per user across keys, groups and providers bounds
-- uncharged work for balance, subscription, API-key and platform quota alike.
CREATE UNIQUE INDEX IF NOT EXISTS gateway_media_jobs_pending_user ON gateway_media_jobs(user_id)
 WHERE state IN ('reserving','submitting','pending','billing','releasing');
CREATE UNIQUE INDEX IF NOT EXISTS gateway_media_jobs_task ON gateway_media_jobs(account_id,task_id) WHERE task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS gateway_media_jobs_due ON gateway_media_jobs(next_attempt_at) WHERE state IN ('reserving','pending','billing','releasing');
