-- Creator identity is durable authorization data, independent of audit retention.
ALTER TABLE users ADD COLUMN IF NOT EXISTS created_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
WITH creators AS (
 SELECT DISTINCT ON (u.id) u.id, a.actor_user_id
 FROM users u JOIN audit_logs a ON a.extra->>'target_user_id'=u.id::text
 JOIN users creator ON creator.id=a.actor_user_id
 WHERE a.action='admin.users.create' AND a.status_code=200
 AND a.actor_role IN ('admin','super_admin') AND a.actor_user_id<>u.id
 ORDER BY u.id,a.id
)
UPDATE users u SET created_by=c.actor_user_id FROM creators c WHERE u.id=c.id AND u.created_by IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_created_by_active ON users(created_by,id) WHERE deleted_at IS NULL;
-- Users without a trustworthy creation receipt remain unowned and super-admin-only.
