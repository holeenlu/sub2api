//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func findJobHeartbeat(t *testing.T, list []*service.OpsJobHeartbeat, jobName string) *service.OpsJobHeartbeat {
	t.Helper()
	for _, hb := range list {
		if hb != nil && hb.JobName == jobName {
			return hb
		}
	}
	require.Failf(t, "heartbeat missing", "job %q not found", jobName)
	return nil
}

func TestUpsertJobHeartbeat_ExpectedIntervalRoundTrip(t *testing.T) {
	ctx := context.Background()
	_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_job_heartbeats WHERE job_name LIKE 'it_hb_%'")
	repo := NewOpsRepository(integrationDB)

	now := time.Now().UTC().Truncate(time.Second)
	interval := int64(600)
	require.NoError(t, repo.UpsertJobHeartbeat(ctx, &service.OpsUpsertJobHeartbeatInput{
		JobName:                 "it_hb_declared",
		LastRunAt:               &now,
		LastSuccessAt:           &now,
		ExpectedIntervalSeconds: &interval,
	}))

	// 旧写入方不带周期：列保持 NULL。
	require.NoError(t, repo.UpsertJobHeartbeat(ctx, &service.OpsUpsertJobHeartbeatInput{
		JobName:       "it_hb_legacy",
		LastRunAt:     &now,
		LastSuccessAt: &now,
	}))

	// 显式 0 表示任务当前没有调度，必须落库为 0 而不是 NULL。
	unscheduled := int64(0)
	require.NoError(t, repo.UpsertJobHeartbeat(ctx, &service.OpsUpsertJobHeartbeatInput{
		JobName:                 "it_hb_unscheduled",
		ExpectedIntervalSeconds: &unscheduled,
	}))

	list, err := repo.ListJobHeartbeats(ctx)
	require.NoError(t, err)

	declared := findJobHeartbeat(t, list, "it_hb_declared")
	require.NotNil(t, declared.ExpectedIntervalSeconds)
	require.Equal(t, int64(600), *declared.ExpectedIntervalSeconds)

	require.Nil(t, findJobHeartbeat(t, list, "it_hb_legacy").ExpectedIntervalSeconds)

	unsched := findJobHeartbeat(t, list, "it_hb_unscheduled")
	require.NotNil(t, unsched.ExpectedIntervalSeconds)
	require.Equal(t, int64(0), *unsched.ExpectedIntervalSeconds)
	require.Nil(t, unsched.LastSuccessAt)

	// 只报错不带周期的心跳不能把已声明的周期抹掉。
	errAt := now.Add(time.Minute)
	msg := "boom"
	require.NoError(t, repo.UpsertJobHeartbeat(ctx, &service.OpsUpsertJobHeartbeatInput{
		JobName:     "it_hb_declared",
		LastRunAt:   &errAt,
		LastErrorAt: &errAt,
		LastError:   &msg,
	}))
	list, err = repo.ListJobHeartbeats(ctx)
	require.NoError(t, err)
	declared = findJobHeartbeat(t, list, "it_hb_declared")
	require.NotNil(t, declared.ExpectedIntervalSeconds)
	require.Equal(t, int64(600), *declared.ExpectedIntervalSeconds)
	require.NotNil(t, declared.LastErrorAt)
}

// 「本轮跳过」的心跳只更新 last_run_at/last_result，不能清掉上一次真实失败。
func TestUpsertJobHeartbeat_SkippedTickKeepsLastError(t *testing.T) {
	ctx := context.Background()
	_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_job_heartbeats WHERE job_name LIKE 'it_hb_%'")
	repo := NewOpsRepository(integrationDB)

	now := time.Now().UTC().Truncate(time.Second)
	msg := "cleanup failed"
	require.NoError(t, repo.UpsertJobHeartbeat(ctx, &service.OpsUpsertJobHeartbeatInput{
		JobName:     "it_hb_skipped",
		LastRunAt:   &now,
		LastErrorAt: &now,
		LastError:   &msg,
	}))

	later := now.Add(time.Hour)
	skipped := "skipped: cleanup disabled by settings"
	unscheduled := int64(0)
	require.NoError(t, repo.UpsertJobHeartbeat(ctx, &service.OpsUpsertJobHeartbeatInput{
		JobName:                 "it_hb_skipped",
		LastRunAt:               &later,
		LastResult:              &skipped,
		ExpectedIntervalSeconds: &unscheduled,
	}))

	list, err := repo.ListJobHeartbeats(ctx)
	require.NoError(t, err)
	hb := findJobHeartbeat(t, list, "it_hb_skipped")
	require.NotNil(t, hb.LastErrorAt, "跳过的一轮不能把上一次失败抹掉")
	require.NotNil(t, hb.LastError)
	require.Equal(t, msg, *hb.LastError)
	require.Nil(t, hb.LastSuccessAt)
	require.NotNil(t, hb.LastResult)
	require.Equal(t, skipped, *hb.LastResult, "跳过原因要覆盖上一条 last_result")
	require.NotNil(t, hb.LastRunAt)
	require.True(t, hb.LastRunAt.Equal(later))

	// 真正成功时才清错误。
	success := later.Add(time.Hour)
	require.NoError(t, repo.UpsertJobHeartbeat(ctx, &service.OpsUpsertJobHeartbeatInput{
		JobName:       "it_hb_skipped",
		LastRunAt:     &success,
		LastSuccessAt: &success,
	}))
	list, err = repo.ListJobHeartbeats(ctx)
	require.NoError(t, err)
	hb = findJobHeartbeat(t, list, "it_hb_skipped")
	require.Nil(t, hb.LastErrorAt)
	require.Nil(t, hb.LastError)
}
