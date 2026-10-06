package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpsMetricThresholdsTTFTDefaultMigration(t *testing.T) {
	content, err := FS.ReadFile("236_ops_metric_thresholds_ttft_default.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "UPDATE settings")
	require.Contains(t, sql, "jsonb_set(value::jsonb, '{ttft_p99_ms_max}', to_jsonb(10000))::text")
	require.Contains(t, sql, "WHERE key = 'ops_metric_thresholds'")
	// 只动仍停留在旧默认值上的行，别人改过的阈值不碰。
	require.Contains(t, sql, "IN ('500', '500.0')")
}
