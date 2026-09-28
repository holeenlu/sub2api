//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type qualityBPSFixture struct {
	t       *testing.T
	plans   service.ScheduledTestPlanRepository
	account int64
	plan    *service.ScheduledTestPlan
	until   time.Time
}

func newQualityBPSFixture(t *testing.T, extra string, policy *service.QualityPolicy) *qualityBPSFixture {
	t.Helper()
	ctx := context.Background()
	f := &qualityBPSFixture{t: t, plans: NewScheduledTestPlanRepository(integrationDB)}
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,status,schedulable,extra) VALUES('quality-bps','openai','oauth','active',true,$1::jsonb) RETURNING id`, extra).Scan(&f.account))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM scheduler_outbox WHERE account_id=$1`, f.account)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM scheduled_test_plans WHERE account_id=$1`, f.account)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, f.account)
	})
	svc := service.NewScheduledTestService(f.plans, NewScheduledTestResultRepository(integrationDB))
	plan, err := svc.CreatePlan(ctx, &service.ScheduledTestPlan{AccountID: f.account, ModelID: "gpt-6-astra", CronExpression: "*/30 * * * *", Enabled: true, MaxResults: 100,
		PelicanConfig: &service.PelicanTestConfig{QuestionKind: service.OpenAICodexStateProbeQuestionKind, ReasoningEffort: "medium", ParallelCount: 1, Quality: policy}})
	require.NoError(t, err)
	f.plan = plan
	f.claim()
	return f
}

func (f *qualityBPSFixture) claim() {
	f.t.Helper()
	ctx := context.Background()
	require.NoError(f.t, f.plans.TriggerQuality(ctx, f.plan.ID))
	plan, err := f.plans.GetByID(ctx, f.plan.ID)
	require.NoError(f.t, err)
	// Windows 上 Go 的墙钟按系统时钟节拍更新，可能略早于数据库 NOW()，留一秒余量。
	now := time.Now().Add(time.Second).Truncate(time.Microsecond)
	f.until = now.Add(15 * time.Minute)
	ok, err := f.plans.ClaimPelican(ctx, plan, now, f.until, now.Add(30*time.Minute))
	require.NoError(f.t, err)
	require.True(f.t, ok)
	f.plan = plan
}

func (f *qualityBPSFixture) apply(outcome string) string {
	f.t.Helper()
	got, err := f.plans.ApplyQualityOutcome(context.Background(), f.plan, f.until, outcome)
	require.NoError(f.t, err)
	return got
}

func (f *qualityBPSFixture) exec(query string, args ...any) {
	f.t.Helper()
	_, err := integrationDB.ExecContext(context.Background(), query, append([]any{f.account}, args...)...)
	require.NoError(f.t, err)
}

func (f *qualityBPSFixture) extra() map[string]any {
	f.t.Helper()
	var raw []byte
	require.NoError(f.t, integrationDB.QueryRowContext(context.Background(), `SELECT extra FROM accounts WHERE id=$1`, f.account).Scan(&raw))
	extra := map[string]any{}
	require.NoError(f.t, json.Unmarshal(raw, &extra))
	return extra
}

func (f *qualityBPSFixture) count(query string) int {
	f.t.Helper()
	var n int
	require.NoError(f.t, integrationDB.QueryRowContext(context.Background(), query, f.account).Scan(&n))
	return n
}

// events 返回待处理的 account_changed 事件数并清空，模拟调度器已消费（未消费的同类事件会去重合并）。
func (f *qualityBPSFixture) events() int {
	f.t.Helper()
	n := f.count(`SELECT count(*) FROM scheduler_outbox WHERE account_id=$1 AND event_type='account_changed'`)
	f.exec(`DELETE FROM scheduler_outbox WHERE account_id=$1`)
	return n
}

func TestQualityEnableBPSLifecycle(t *testing.T) {
	f := newQualityBPSFixture(t, `{"codex_7d_used_percent":10,"unrelated":"kept","openai_excel_bps_ignore_images":true}`, &service.QualityPolicy{
		Action: service.QualityActionEnableBPS, AutoRestore: true,
		BPS: &service.QualityBPSPolicy{FailureThreshold: 2, UsagePercent: 80, PassThreshold: 2, HoldOnUsage: true,
			Models: []string{"gpt-6-astra"}, OmitUnsupportedTools: true, IgnoreEncryptedContent: true},
	})
	states := `SELECT count(*) FROM account_quality_states s JOIN scheduled_test_plans p ON p.id=s.plan_id WHERE p.account_id=$1`
	passStreak := `SELECT COALESCE((s.state->>'pass_streak')::int,0) FROM account_quality_states s JOIN scheduled_test_plans p ON p.id=s.plan_id WHERE p.account_id=$1`

	// 连续次数：降智累加、无法判断不打断、通过清零。
	require.Equal(t, "failure_counted:1/2", f.apply("failed"))
	require.Equal(t, "inconclusive", f.apply("inconclusive"))
	require.Equal(t, "passed", f.apply("passed"))
	require.Zero(t, f.count(states), "a zero streak leaves no state row")
	require.Zero(t, f.events())
	require.Equal(t, "failure_counted:1/2", f.apply("failed"))
	require.Equal(t, "inconclusive", f.apply("inconclusive"))
	require.Equal(t, "bps_enabled", f.apply("failed"))
	extra := f.extra()
	require.Equal(t, true, extra["openai_excel_bps"])
	require.Equal(t, []any{"gpt-6-astra"}, extra["openai_excel_bps_models"])
	require.Equal(t, true, extra[service.ExcelBPSOmitUnsupportedToolsKey])
	require.Equal(t, false, extra[service.ExcelBPSIgnoreImagesKey])
	require.Equal(t, true, extra[service.ExcelBPSIgnoreEncryptedContentKey])
	require.NotContains(t, extra, service.ExcelBPS403TargetGroupIDKey)
	require.Equal(t, "kept", extra["unrelated"])
	require.Equal(t, 1, f.events())

	// 关闭：连续满血够次数才关；降智清零、无法判断不打断；满血次数不看用量，用量高只挡住最后的关闭。
	require.Equal(t, "already_quarantined", f.apply("failed"))
	require.Equal(t, "inconclusive", f.apply("inconclusive"))
	f.exec(`UPDATE accounts SET extra=extra||'{"codex_7d_used_percent":95}' WHERE id=$1`)
	require.Equal(t, "restore_counted:1/2", f.apply("passed"))
	require.Equal(t, 1, f.count(passStreak))
	require.Equal(t, "already_quarantined", f.apply("failed"))
	require.Zero(t, f.count(passStreak), "a degraded round restarts the healthy count")
	require.Equal(t, "restore_counted:1/2", f.apply("passed"))
	require.Equal(t, "inconclusive", f.apply("inconclusive"))
	require.Equal(t, "bps_kept_usage", f.apply("passed"), "usage still over the trigger keeps BPS on")
	require.Equal(t, "bps_kept_usage", f.apply("passed"))
	require.Equal(t, 2, f.count(passStreak), "the healthy count stops at the threshold")
	require.Equal(t, true, f.extra()["openai_excel_bps"])
	require.Zero(t, f.events(), "counting rounds leave the account untouched")
	f.exec(`UPDATE accounts SET extra=extra||'{"codex_7d_used_percent":10}', name='edited elsewhere' WHERE id=$1`)
	require.Equal(t, "restored", f.apply("passed"))
	extra = f.extra()
	for _, key := range service.QualityBPSManagedKeys {
		if key != service.ExcelBPSIgnoreImagesKey {
			require.NotContains(t, extra, key, "keys absent before BPS are removed on restore")
		}
	}
	require.Equal(t, true, extra[service.ExcelBPSIgnoreImagesKey], "previous values come back")
	require.Equal(t, "kept", extra["unrelated"])
	require.Zero(t, f.count(states))
	require.Equal(t, 1, f.events())

	// 用量触发：探针满血也会开，并按用量命中单独标记。
	f.exec(`UPDATE accounts SET extra=extra||'{"codex_7d_used_percent":85}' WHERE id=$1`)
	require.Equal(t, "bps_enabled_usage", f.apply("passed"))
	require.Equal(t, 1, f.events())
	// 管理员改过 BPS 选项后不自动覆盖。
	f.exec(`UPDATE accounts SET extra=extra||'{"codex_7d_used_percent":5,"openai_excel_bps_models":["gpt-5.6-sol"]}' WHERE id=$1`)
	require.Equal(t, "restore_counted:1/2", f.apply("passed"))
	require.Equal(t, "restore_conflict", f.apply("passed"))
	require.Equal(t, []any{"gpt-5.6-sol"}, f.extra()["openai_excel_bps_models"])

	// 403 自动关闭后，规则不再自动重新打开，直到管理员手动开启。
	f.exec(`UPDATE accounts SET extra=extra||'{"openai_excel_bps":false,"openai_excel_bps_403_disabled_at":"2026-09-28T00:00:00Z"}' WHERE id=$1`)
	require.Equal(t, "bps_blocked_403", f.apply("failed"))
	f.exec(`DELETE FROM account_quality_states WHERE plan_id=(SELECT id FROM scheduled_test_plans WHERE account_id=$1)`)
	require.Equal(t, "failure_counted:1/2", f.apply("failed"))
	require.Equal(t, "bps_blocked_403", f.apply("failed"))
	require.Equal(t, false, f.extra()["openai_excel_bps"])

	// 管理员自己开着 BPS：规则不接管也不覆盖。
	f.exec(`UPDATE accounts SET extra=(extra||'{"openai_excel_bps":true}') - 'openai_excel_bps_403_disabled_at' WHERE id=$1`)
	require.Equal(t, "bps_already_enabled", f.apply("failed"))
	require.Equal(t, []any{"gpt-5.6-sol"}, f.extra()["openai_excel_bps_models"])

	f.exec(`UPDATE accounts SET type='apikey', extra=extra - 'openai_excel_bps' WHERE id=$1`)
	require.Equal(t, "bps_unsupported", f.apply("failed"))
	require.NotContains(t, f.extra(), "openai_excel_bps")
	require.Zero(t, f.events(), "conflicts and blocked rounds leave the account untouched")
}

func TestQualityBPSRetiredActionCannotRun(t *testing.T) {
	f := newQualityBPSFixture(t, `{}`, &service.QualityPolicy{Action: service.QualityActionEnableBPS, AutoRestore: true, BPS: &service.QualityBPSPolicy{FailureThreshold: 1, AllModels: true}})
	require.Equal(t, "bps_enabled", f.apply("failed"))
	f.exec(`UPDATE scheduled_test_plans SET pelican_config=jsonb_set(pelican_config,'{quality,action}','"disable_scheduling"'),running_until=NULL,updated_at=NOW() WHERE account_id=$1`)
	require.Error(t, f.plans.TriggerQuality(context.Background(), f.plan.ID))
	_, err := f.plans.GetByID(context.Background(), f.plan.ID)
	require.Error(t, err)
	require.Equal(t, true, f.extra()["openai_excel_bps"])
}

func TestQualityBPSIgnoresInconclusiveAndAccountEdits(t *testing.T) {
	f := newQualityBPSFixture(t, `{"codex_7d_used_percent":95}`, &service.QualityPolicy{Action: service.QualityActionEnableBPS, AutoRestore: true, BPS: &service.QualityBPSPolicy{UsagePercent: 80, AllModels: true}})
	require.Equal(t, "inconclusive", f.apply("inconclusive"))
	require.NotContains(t, f.extra(), "openai_excel_bps")
	var version time.Time
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `SELECT updated_at FROM accounts WHERE id=$1`, f.account).Scan(&version))
	f.plan.BPSAccountVersion = &version
	f.exec(`UPDATE accounts SET name='changed during probe',updated_at=clock_timestamp() WHERE id=$1`)
	require.Equal(t, "account_changed", f.apply("passed"))
	require.NotContains(t, f.extra(), "openai_excel_bps")
	f.plan.BPSAccountVersion = nil
	f.exec(`UPDATE accounts SET schedulable=false WHERE id=$1`)
	require.Equal(t, "account_ineligible", f.apply("passed"))
	require.NotContains(t, f.extra(), "openai_excel_bps")
	require.Zero(t, f.events())
}

func TestQualityEnableBPSRestoreWithoutUsageHold(t *testing.T) {
	f := newQualityBPSFixture(t, `{"codex_5h_used_percent":90}`, &service.QualityPolicy{
		Action: service.QualityActionEnableBPS, AutoRestore: true,
		BPS: &service.QualityBPSPolicy{FailureThreshold: 1, UsagePercent: 80, AllModels: true},
	})
	passStreak := `SELECT COALESCE((s.state->>'pass_streak')::int,0) FROM account_quality_states s JOIN scheduled_test_plans p ON p.id=s.plan_id WHERE p.account_id=$1`
	require.Equal(t, "bps_enabled", f.apply("failed"))

	// 未开自动关闭时不计满血次数。
	f.exec(`UPDATE scheduled_test_plans SET pelican_config=jsonb_set(pelican_config,'{quality,auto_restore}','false'), running_until=NULL, updated_at=NOW() WHERE account_id=$1`)
	f.claim()
	require.Equal(t, "passed", f.apply("passed"))
	require.Zero(t, f.count(passStreak))

	// 没勾「用量高时不关」：旧规则（未填次数）满血一次就关，用量仍高也关；下一轮再按用量重新开启。
	f.exec(`UPDATE scheduled_test_plans SET pelican_config=jsonb_set(pelican_config,'{quality,auto_restore}','true'), running_until=NULL, updated_at=NOW() WHERE account_id=$1`)
	f.claim()
	require.Equal(t, "restored", f.apply("passed"))
	require.NotContains(t, f.extra(), "openai_excel_bps")
	require.Equal(t, "bps_enabled_usage", f.apply("passed"))
}

func TestQualityEnableBPSRestoreAfterAccountEditorSave(t *testing.T) {
	f := newQualityBPSFixture(t, `{}`, &service.QualityPolicy{
		Action: service.QualityActionEnableBPS, AutoRestore: true,
		BPS: &service.QualityBPSPolicy{FailureThreshold: 1, PassThreshold: 1, AllModels: true, OmitUnsupportedTools: true, IgnoreEncryptedContent: true},
	})
	require.Equal(t, "bps_enabled", f.apply("failed"))
	repo := newAccountRepositoryWithSQL(testEntClient(t), integrationDB, nil)
	account, err := repo.GetByID(context.Background(), f.account)
	require.NoError(t, err)
	// EditAccountModal emits absent keys for unchecked options and the default
	// proxy source even when the operator only changes the account's name.
	account.Name = "renamed without changing BPS settings"
	for key, value := range account.Extra {
		if value == false {
			delete(account.Extra, key)
		}
	}
	require.True(t, account.IsExcelBPSEnabledForModel("gpt-6-astra"))
	require.NoError(t, repo.Update(context.Background(), account))
	require.Equal(t, "restored", f.apply("passed"))
}

func TestQualityBPSLeaseAndPauseProtectAccount(t *testing.T) {
	f := newQualityBPSFixture(t, `{}`, &service.QualityPolicy{Action: service.QualityActionEnableBPS, AutoRestore: true, BPS: &service.QualityBPSPolicy{FailureThreshold: 1, AllModels: true}})
	ctx := context.Background()
	require.NoError(t, f.plans.FinishPelican(ctx, f.plan.ID, f.until, time.Now()))
	require.NoError(t, f.plans.TriggerQuality(ctx, f.plan.ID))
	plan, err := f.plans.GetByID(ctx, f.plan.ID)
	require.NoError(t, err)
	var wg sync.WaitGroup
	outcomes := make(chan bool, 8)
	failures := make(chan error, 8)
	now := time.Now().Add(time.Second).Truncate(time.Microsecond)
	until := now.Add(15 * time.Minute)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot := *plan
			claimed, e := f.plans.ClaimPelican(ctx, &snapshot, now, until, now.Add(30*time.Minute))
			outcomes <- claimed
			failures <- e
		}()
	}
	wg.Wait()
	close(outcomes)
	close(failures)
	winners := 0
	for ok := range outcomes {
		if ok {
			winners++
		}
	}
	require.Equal(t, 1, winners)
	for err := range failures {
		require.NoError(t, err)
	}
	// Pausing a rule during the probe must invalidate its result.
	f.exec(`UPDATE scheduled_test_plans SET enabled=false,updated_at=clock_timestamp() WHERE account_id=$1`)
	got, err := f.plans.ApplyQualityOutcome(ctx, plan, until, "failed")
	require.NoError(t, err)
	require.Equal(t, "stale_run", got)
	require.NotContains(t, f.extra(), "openai_excel_bps")
	require.Zero(t, f.events())
}
