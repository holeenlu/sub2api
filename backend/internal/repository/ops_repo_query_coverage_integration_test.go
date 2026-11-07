//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestOpsPreaggCoverageActualPostgres(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID})
	account := mustCreateAccount(t, client, &service.Account{Name: "coverage", Platform: service.PlatformOpenAI})
	group := mustCreateGroup(t, client, &service.Group{Name: "coverage", Platform: service.PlatformOpenAI})
	start := time.Date(2021, 2, 18, 0, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)
	var errorID int64
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE id = $1", errorID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM usage_logs WHERE user_id = $1", user.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_metrics_hourly WHERE bucket_start >= $1 AND bucket_start < $2", start, end)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE id = $1", key.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id = $1", account.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM groups WHERE id = $1", group.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	for i, at := range []time.Time{start.Add(15 * time.Minute), start.Add(2*time.Hour + 15*time.Minute)} {
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO usage_logs
 (user_id, api_key_id, account_id, group_id, request_id, model, input_tokens, output_tokens, duration_ms, request_type, created_at)
 VALUES ($1,$2,$3,$4,$5,'coverage-test',10,20,1000,$6,$7)`,
			user.ID, key.ID, account.ID, group.ID, fmt.Sprintf("coverage-%d-%d", user.ID, i), service.RequestTypeSync, at)
		require.NoError(t, err)
	}
	repo := NewOpsRepository(integrationDB).(*opsRepository)
	filter := &service.OpsDashboardFilter{StartTime: start, EndTime: end, Platform: " OpenAI ", GroupID: &group.ID, QueryMode: service.OpsQueryModePreagg}
	require.NoError(t, repo.UpsertHourlyMetrics(ctx, start, start.Add(time.Hour)))
	_, err := repo.GetDashboardOverview(ctx, filter)
	require.ErrorIs(t, err, service.ErrOpsPreaggregatedNotPopulated, "the second populated raw hour has no projection yet")
	filter.QueryMode = service.OpsQueryModeAuto
	raw, err := repo.GetDashboardOverview(ctx, filter)
	require.NoError(t, err)
	require.EqualValues(t, 2, raw.SuccessCount)
	require.NoError(t, repo.UpsertHourlyMetrics(ctx, start.Add(2*time.Hour), start.Add(3*time.Hour)))
	filter.QueryMode = service.OpsQueryModePreagg
	complete, err := repo.GetDashboardOverview(ctx, filter)
	require.NoError(t, err, "the two truly empty hours do not require synthetic aggregate rows")
	require.EqualValues(t, 2, complete.SuccessCount)
	status := 503
	errorID, err = repo.InsertErrorLog(ctx, &service.OpsInsertErrorLogInput{
		GroupID: &group.ID, Platform: service.PlatformOpenAI, ErrorPhase: "upstream", ErrorType: "upstream_error",
		StatusCode: status, ErrorOwner: "provider", CreatedAt: start.Add(90 * time.Minute),
	})
	require.NoError(t, err)
	_, err = repo.GetDashboardOverview(ctx, filter)
	require.ErrorIs(t, err, service.ErrOpsPreaggregatedNotPopulated, "error-only hours also require coverage")
	require.NoError(t, repo.UpsertHourlyMetrics(ctx, start.Add(time.Hour), start.Add(2*time.Hour)))
	complete, err = repo.GetDashboardOverview(ctx, filter)
	require.NoError(t, err)
	require.EqualValues(t, 2, complete.SuccessCount)
	require.EqualValues(t, 1, complete.ErrorCountTotal)
}
