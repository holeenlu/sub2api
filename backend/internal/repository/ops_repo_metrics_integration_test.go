//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpsSystemMetricsCompletedBucketIsNotDuplicated(t *testing.T) {
	ctx := context.Background()
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	repo := NewOpsRepository(integrationDB).(*opsRepository)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM ops_system_metrics WHERE created_at = $1", createdAt)
		require.NoError(t, err)
	})
	platform := service.PlatformOpenAI
	groupID := int64(1 << 33)
	for _, scope := range []struct {
		name     string
		platform *string
		groupID  *int64
	}{
		{name: "global"},
		{name: "platform", platform: &platform},
		{name: "group", groupID: &groupID},
		{name: "platform and group", platform: &platform, groupID: &groupID},
	} {
		t.Run(scope.name, func(t *testing.T) {
			input := &service.OpsInsertSystemMetricsInput{
				CreatedAt: createdAt, WindowMinutes: 1, SuccessCount: 3,
				Platform: scope.platform, GroupID: scope.groupID,
			}
			require.NoError(t, repo.InsertSystemMetrics(ctx, input))
			input.SuccessCount = 999
			require.NoError(t, repo.InsertSystemMetrics(ctx, input))
			var count, successes int64
			var cpu sql.NullFloat64
			var dbOK sql.NullBool
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*), SUM(success_count), MAX(cpu_usage_percent), BOOL_AND(db_ok)
FROM ops_system_metrics WHERE created_at = $1 AND window_minutes = 1
AND platform IS NOT DISTINCT FROM $2::text
AND group_id IS NOT DISTINCT FROM $3::bigint`, createdAt, scope.platform, scope.groupID).Scan(&count, &successes, &cpu, &dbOK))
			require.EqualValues(t, 1, count)
			require.EqualValues(t, 3, successes, "retry must not overwrite the completed bucket")
			require.False(t, cpu.Valid, "unknown CPU remains NULL")
			require.False(t, dbOK.Valid, "unknown DB health remains NULL")
		})
	}

	input := &service.OpsInsertSystemMetricsInput{CreatedAt: createdAt, WindowMinutes: 5, SuccessCount: 3}
	require.NoError(t, repo.InsertSystemMetrics(ctx, input))
	var count int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM ops_system_metrics WHERE created_at = $1", createdAt).Scan(&count))
	require.EqualValues(t, 5, count, "distinct scopes and windows retain their own buckets")
}
