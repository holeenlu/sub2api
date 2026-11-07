ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS fallback_group_id_on_no_account BIGINT REFERENCES groups(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_groups_fallback_group_id_on_no_account
    ON groups(fallback_group_id_on_no_account)
    WHERE deleted_at IS NULL AND fallback_group_id_on_no_account IS NOT NULL;

COMMENT ON COLUMN groups.fallback_group_id_on_no_account IS
    'Group whose account pool is borrowed when this group has no schedulable account; billing stays with this group';
