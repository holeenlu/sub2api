-- A policy is shared by every limited admin; existing full administrators are
-- promoted only when the policy is first installed, never on later startups.
WITH installed AS (
    INSERT INTO settings (key, value, updated_at)
    VALUES ('admin_role_policy', jsonb_build_object(
        'version', 1,
        'legacy_audit_max_id', (SELECT COALESCE(MAX(id),0) FROM audit_logs),
        'permissions', jsonb_build_array('staff.manage', 'users.read', 'users.create', 'users.update', 'users.delete'),
        'updated_by', 0,
        'updated_at', NOW()
    )::text, NOW())
    ON CONFLICT (key) DO NOTHING
    RETURNING key
)
UPDATE users SET role = 'super_admin', session_generation = session_generation + 1
WHERE role = 'admin' AND EXISTS (SELECT 1 FROM installed);

ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS visibility VARCHAR(16) NOT NULL DEFAULT 'super_admin';
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'audit_logs'::regclass AND conname = 'audit_logs_visibility_check') THEN
        ALTER TABLE audit_logs ADD CONSTRAINT audit_logs_visibility_check CHECK (visibility IN ('staff', 'super_admin'));
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_audit_logs_staff_created
    ON audit_logs (created_at DESC, id DESC) WHERE visibility = 'staff';


-- Existing connectivity plans were installed by full administrators. Preserve
-- those as explicitly system-owned jobs; new delegated plans record their actor.
ALTER TABLE scheduled_test_plans ADD COLUMN IF NOT EXISTS management_authorization JSONB;
UPDATE scheduled_test_plans SET management_authorization = '{"system":true}'::jsonb
WHERE management_authorization IS NULL;
-- Diagnostic jobs already identify their billing owner; preserve that identity.
UPDATE scheduled_test_plans p SET diagnostic_config = p.diagnostic_config ||
 jsonb_build_object('authorization', jsonb_build_object('user_id',u.id,'role',u.role,
 'session_generation',u.session_generation))
FROM users u WHERE p.diagnostic_config IS NOT NULL
 AND (p.diagnostic_config->>'owner_id')::bigint=u.id
 AND NOT (p.diagnostic_config ? 'authorization');
UPDATE scheduled_test_results r SET diagnostic_run = r.diagnostic_run ||
 jsonb_build_object('authorization',p.diagnostic_config->'authorization')
FROM scheduled_test_plans p WHERE r.plan_id=p.id AND r.diagnostic_run IS NOT NULL
 AND NOT (r.diagnostic_run ? 'authorization');
