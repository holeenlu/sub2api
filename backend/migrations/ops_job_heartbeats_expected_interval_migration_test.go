package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpsJobHeartbeatsExpectedIntervalMigration(t *testing.T) {
	content, err := FS.ReadFile("234_ops_job_heartbeats_add_expected_interval.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql,
		"ALTER TABLE IF EXISTS ops_job_heartbeats ADD COLUMN IF NOT EXISTS expected_interval_seconds BIGINT")
	require.Contains(t, sql, "COMMENT ON COLUMN ops_job_heartbeats.expected_interval_seconds")
}
