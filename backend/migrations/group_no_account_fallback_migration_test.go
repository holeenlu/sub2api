package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupNoAccountFallbackMigration(t *testing.T) {
	content, err := FS.ReadFile("235_group_no_account_fallback.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql,
		"ADD COLUMN IF NOT EXISTS fallback_group_id_on_no_account BIGINT REFERENCES groups(id) ON DELETE SET NULL")
	require.Contains(t, sql,
		"CREATE INDEX IF NOT EXISTS idx_groups_fallback_group_id_on_no_account ON groups(fallback_group_id_on_no_account) "+
			"WHERE deleted_at IS NULL AND fallback_group_id_on_no_account IS NOT NULL")
	require.Contains(t, sql, "COMMENT ON COLUMN groups.fallback_group_id_on_no_account")
}
