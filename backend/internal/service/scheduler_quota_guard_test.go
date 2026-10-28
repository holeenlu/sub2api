package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"maps"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func quotaGuardAnthropicAccount(now time.Time) *Account {
	a := &Account{ID: 71, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"access_token": "quota-test", "account_scheduling_threshold": 80}}
	a.Extra = persistableAnthropicObservations(map[string]UsageObservation{
		"five_hour.utilization": {Value: 79.0, Source: "upstream_headers", SampledAt: &now},
		"five_hour.resets_at":   {Value: now.Add(time.Hour).Format(time.RFC3339), Source: "upstream_headers", SampledAt: &now},
	}, anthropicCredentialIdentity(a))
	return a
}

func quotaGuardOpenAIAccount(now time.Time) *Account {
	a := &Account{ID: 72, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"access_token": "quota-test", "account_scheduling_threshold": 80}}
	used, reset, minutes := 79.0, 3600, 300
	a.Extra = buildCodexUsageExtraUpdates(&OpenAICodexUsageSnapshot{
		PrimaryUsedPercent: &used, PrimaryResetAfterSeconds: &reset, PrimaryWindowMinutes: &minutes,
	}, now)
	appendSchedulingQuotaObservations(a.Extra)
	return a
}

func quotaGuardLargeRequest(ctx context.Context, confirmedWarm bool) context.Context {
	risk := &schedulingQuotaRequest{model: "gpt-5.1", inputTokens: 100000 + 10, scope: generateRandomUUID(), prefixes: [][32]byte{sha256.Sum256([]byte("confirmed-test-prefix"))}}
	risk.once.Do(func() {})
	ctx = context.WithValue(ctx, schedulingQuotaRequestKey{}, risk)
	if confirmedWarm {
		for _, id := range []int64{71, 72} {
			confirmSchedulingQuotaPrefix(ctx, &Account{ID: id, Credentials: map[string]any{"access_token": "quota-test"}}, risk.inputTokens)
		}
	}
	return ctx
}

func TestSchedulingQuotaRequestInputOnly(t *testing.T) {
	for _, tc := range []struct{ protocol, body string }{
		{ContentModerationProtocolAnthropicMessages, `{"model":"claude-sonnet-4-5","max_tokens":128000,"messages":[{"role":"user","content":"hello"}]}`},
		{ContentModerationProtocolOpenAIChat, `{"model":"gpt-5.1","max_tokens":128000,"max_completion_tokens":128000,"messages":[{"role":"user","content":"hello"}]}`},
		{ContentModerationProtocolOpenAIResponses, `{"model":"gpt-5.1","max_output_tokens":128000,"input":"hello"}`},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			ctx := WithSchedulingQuotaRequest(t.Context(), []byte(tc.body), tc.protocol)
			risk := ctx.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest)
			risk.estimate()
			require.Positive(t, risk.inputTokens)
			require.Less(t, risk.inputTokens, 50)
			require.False(t, risk.continuation)
			lower := WithSchedulingQuotaRequest(t.Context(), []byte(strings.ReplaceAll(tc.body, "128000", "1")), tc.protocol)
			lowerRisk := lower.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest)
			lowerRisk.estimate()
			require.Equal(t, risk.inputTokens, lowerRisk.inputTokens)
		})
	}
	for _, tc := range []struct{ protocol, body string }{
		{ContentModerationProtocolAnthropicMessages, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"` + strings.Repeat(" word", 100010) + `"}]}`},
		{ContentModerationProtocolOpenAIChat, `{"model":"gpt-5.1","messages":[{"role":"user","content":"` + strings.Repeat(" word", 100010) + `"}]}`},
		{ContentModerationProtocolOpenAIResponses, `{"model":"gpt-5.1","input":"` + strings.Repeat(" word", 100010) + `"}`},
	} {
		t.Run(tc.protocol+"/large", func(t *testing.T) {
			ctx := WithSchedulingQuotaRequest(t.Context(), []byte(tc.body), tc.protocol)
			require.True(t, (*RateLimitService)(nil).IsRequestBlockedBySchedulingQuota(ctx, quotaGuardAnthropicAccount(time.Now().Add(-time.Second))))
			require.GreaterOrEqual(t, ctx.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest).inputTokens, 100000)
		})
	}
}

func TestSchedulingQuotaGuardFirstTurnAndUnknownContinuation(t *testing.T) {
	now := time.Now().Add(-time.Second)
	a := quotaGuardAnthropicAccount(now)
	guard := (*RateLimitService)(nil)
	cold := quotaGuardLargeRequest(t.Context(), false)
	require.False(t, SchedulingQuotaRequestWasBlocked(cold))
	require.True(t, guard.IsRequestBlockedBySchedulingQuota(cold, a))
	require.True(t, SchedulingQuotaRequestWasBlocked(cold))
	other := *a
	other.ID++
	unknown := quotaGuardLargeRequest(t.Context(), false)
	require.True(t, guard.IsRequestBlockedBySchedulingQuota(unknown, a), "unconfirmed history must be treated as cold")
	require.True(t, SchedulingQuotaRequestWasBlocked(unknown))
	require.True(t, guard.IsRequestBlockedBySchedulingQuota(unknown, &other), "switching accounts cannot inherit cache evidence")
	reset := WithSchedulingQuotaRequest(cold, []byte(`{"model":"gpt-5.1","input":"hi"}`), ContentModerationProtocolOpenAIResponses)
	require.False(t, guard.IsRequestBlockedBySchedulingQuota(reset, a), "a new turn cannot inherit a prior large-input estimate")
	require.False(t, SchedulingQuotaRequestWasBlocked(reset))
	before, err := json.Marshal(a)
	require.NoError(t, err)
	require.True(t, guard.IsRequestBlockedBySchedulingQuota(cold, a))
	after, err := json.Marshal(a)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "request protection must not mutate quota, cooldown or accounting")
}

func TestSchedulingQuotaGuardContinuationAndMalformedEvidence(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-5.1","previous_response_id":"resp_a","input":"hello"}`,
		`{"model":"gpt-5.1","input":[{"role":"assistant","content":"done"},{"role":"user","content":"next"}]}`,
		`{"model":"gpt-5.1","input":[{"type":"function_call_output","call_id":"c","output":"done"}]}`,
		`{"model":"gpt-5.1","conversation":"conv_a","input":"next"}`,
	} {
		ctx := WithSchedulingQuotaRequest(t.Context(), []byte(body), ContentModerationProtocolOpenAIResponses)
		risk := ctx.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest)
		risk.estimate()
		require.Positive(t, risk.inputTokens, "history does not suppress visible input estimation")
	}
	for _, body := range []string{`{`, `null`, `{}`, `{"model":"gpt-5.1","input":42}`} {
		ctx := WithSchedulingQuotaRequest(quotaGuardLargeRequest(t.Context(), false), []byte(body), ContentModerationProtocolOpenAIResponses)
		require.False(t, (*RateLimitService)(nil).IsRequestBlockedBySchedulingQuota(ctx, quotaGuardAnthropicAccount(time.Now())))
	}
}

func TestSchedulingQuotaGuardCountsLazilyAndSharesEstimateAcrossCandidates(t *testing.T) {
	body := []byte(`{"model":"gpt-5.1","input":"` + strings.Repeat(" word", 100010) + `"}`)
	ctx := WithSchedulingQuotaRequest(t.Context(), body, ContentModerationProtocolOpenAIResponses)
	risk := ctx.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest)
	guard := (*RateLimitService)(nil)
	safe := quotaGuardAnthropicAccount(time.Now().Add(-(5 * time.Minute)))
	require.False(t, guard.IsRequestBlockedBySchedulingQuota(ctx, safe))
	require.Zero(t, risk.inputTokens, "normal candidates never tokenize the request")
	require.Empty(t, risk.model, "normal candidates never convert the protocol")
	a := quotaGuardAnthropicAccount(time.Now().Add(-time.Second))
	var workers sync.WaitGroup
	results := make(chan bool, 16)
	for range 16 {
		workers.Go(func() { results <- guard.IsRequestBlockedBySchedulingQuota(ctx, a) })
	}
	workers.Wait()
	close(results)
	for blocked := range results {
		require.True(t, blocked)
	}
	require.GreaterOrEqual(t, risk.inputTokens, 100000)
	again := false
	risk.once.Do(func() { again = true })
	require.False(t, again, "one completed estimate is shared by the entire request")
}

func TestSchedulingQuotaGuardAnthropicRequiresSameFreshWindow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		name   string
		change func(*Account, map[string]UsageObservation)
		want   string
	}{
		{"current 5h", func(*Account, map[string]UsageObservation) {}, "5h"},
		{"current 7d independent", func(a *Account, f map[string]UsageObservation) {
			f["seven_day.utilization"], f["seven_day.resets_at"] = f["five_hour.utilization"], f["five_hour.resets_at"]
			delete(f, "five_hour.utilization")
		}, "7d"},
		{"only Fable", func(a *Account, f map[string]UsageObservation) {
			f["seven_day_fable.utilization"], f["seven_day_fable.resets_at"] = f["five_hour.utilization"], f["five_hour.resets_at"]
			delete(f, "five_hour.utilization")
		}, ""},
		{"stale", func(a *Account, f map[string]UsageObservation) {
			old := now.Add(-(5 * time.Minute))
			for k, v := range f {
				v.SampledAt = &old
				f[k] = v
			}
		}, ""},
		{"fresh reset old usage", func(a *Account, f map[string]UsageObservation) {
			old := now.Add(-time.Minute)
			v := f["five_hour.utilization"]
			v.SampledAt = &old
			f["five_hour.utilization"] = v
		}, ""},
		{"different source", func(a *Account, f map[string]UsageObservation) {
			v := f["five_hour.resets_at"]
			v.Source = "upstream_api"
			f["five_hour.resets_at"] = v
		}, ""},
		{"missing usage", func(a *Account, f map[string]UsageObservation) { delete(f, "five_hour.utilization") }, ""},
		{"missing reset", func(a *Account, f map[string]UsageObservation) { delete(f, "five_hour.resets_at") }, ""},
		{"expired reset", func(a *Account, f map[string]UsageObservation) {
			v := f["five_hour.resets_at"]
			v.Value = now.Format(time.RFC3339)
			f["five_hour.resets_at"] = v
		}, ""},
		{"reset from wrong duration", func(a *Account, f map[string]UsageObservation) {
			v := f["five_hour.resets_at"]
			v.Value = now.Add(6 * 24 * time.Hour).Format(time.RFC3339)
			f["five_hour.resets_at"] = v
		}, ""},
		{"future sample", func(a *Account, f map[string]UsageObservation) {
			future := now.Add(time.Minute)
			for k, v := range f {
				v.SampledAt = &future
				f[k] = v
			}
		}, ""},
		{"threshold disabled", func(a *Account, f map[string]UsageObservation) { a.Credentials["account_scheduling_threshold"] = 100 }, ""},
		{"below band", func(a *Account, f map[string]UsageObservation) {
			v := f["five_hour.utilization"]
			v.Value = 74.99
			f["five_hour.utilization"] = v
		}, ""},
		{"band boundary", func(a *Account, f map[string]UsageObservation) {
			v := f["five_hour.utilization"]
			v.Value = 75.0
			f["five_hour.utilization"] = v
		}, "5h"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := quotaGuardAnthropicAccount(now)
			fields := storedAnthropicObservations(a)
			tc.change(a, fields)
			a.Extra = persistableAnthropicObservations(fields, anthropicCredentialIdentity(a))
			require.Equal(t, tc.want, schedulingQuotaCriticalWindow(t.Context(), a, nil, now))
		})
	}
	a := quotaGuardAnthropicAccount(now)
	a.Credentials["access_token"] = "another-owner"
	require.Empty(t, schedulingQuotaCriticalWindow(t.Context(), a, nil, now))
	a = quotaGuardAnthropicAccount(now)
	a.Extra = map[string]any{"session_window_utilization": 0.79, "passive_usage_sampled_at": now.Format(time.RFC3339)}
	end := now.Add(time.Hour)
	a.SessionWindowEnd = &end
	require.Empty(t, schedulingQuotaCriticalWindow(t.Context(), a, nil, now), "aggregate legacy fields are not same-window evidence")
}

func TestSchedulingQuotaGuardCodexSparseAndIndependentWindows(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	a := quotaGuardOpenAIAccount(now)
	require.Equal(t, "5h", schedulingQuotaCriticalWindow(t.Context(), a, nil, now))
	for _, patch := range []map[string]any{
		{"codex_usage_updated_at": now.Format(time.RFC3339), "codex_5h_reset_at": now.Add(2 * time.Hour).Format(time.RFC3339)},
		{"codex_usage_updated_at": now.Format(time.RFC3339), "codex_5h_used_percent": 79.0},
		{"codex_usage_updated_at": now.Format(time.RFC3339), "codex_5h_used_percent": math.NaN(), "codex_5h_reset_at": now.Add(time.Hour).Format(time.RFC3339)},
	} {
		a = quotaGuardOpenAIAccount(now)
		appendSchedulingQuotaObservations(patch)
		require.Contains(t, patch, "codex_5h_quota_observation")
		require.Nil(t, patch["codex_5h_quota_observation"])
		maps.Copy(a.Extra, patch)
		require.Empty(t, schedulingQuotaCriticalWindow(t.Context(), a, nil, now))
	}
	a = quotaGuardOpenAIAccount(now.Add(-(5 * time.Minute)))
	patch := map[string]any{"codex_usage_updated_at": now.Format(time.RFC3339), "codex_7d_used_percent": 10.0, "codex_7d_reset_at": now.Add(24 * time.Hour).Format(time.RFC3339)}
	appendSchedulingQuotaObservations(patch)
	require.NotContains(t, patch, "codex_5h_quota_observation")
	maps.Copy(a.Extra, patch)
	require.Empty(t, schedulingQuotaCriticalWindow(t.Context(), a, nil, now), "new 7d sample cannot refresh old 5h")
	patch["codex_7d_used_percent"] = 79.0
	appendSchedulingQuotaObservations(patch)
	maps.Copy(a.Extra, patch)
	require.Equal(t, "7d", schedulingQuotaCriticalWindow(t.Context(), a, nil, now))
	a.Extra["codex_7d_reset_at"] = now.Add(48 * time.Hour).Format(time.RFC3339)
	require.Empty(t, schedulingQuotaCriticalWindow(t.Context(), a, nil, now), "changed window cannot borrow an older observation")
	a = quotaGuardOpenAIAccount(now)
	a.Credentials["chatgpt_account_id"] = "current-owner"
	a.Extra["chatgpt_account_id"] = "previous-owner"
	require.Empty(t, schedulingQuotaCriticalWindow(t.Context(), a, nil, now), "preserve native snapshot identity validation")
}

func TestSchedulingQuotaGuardNativeThresholdsAndBPS(t *testing.T) {
	now := time.Now().Add(-time.Second)
	a := quotaGuardOpenAIAccount(now)
	delete(a.Credentials, "account_scheduling_threshold")
	ctx := withOpenAIQuotaAutoPauseSettings(quotaGuardLargeRequest(t.Context(), false), OpsOpenAIAccountQuotaAutoPauseSettings{DefaultThreshold5h: 0.8})
	require.True(t, (*RateLimitService)(nil).IsRequestBlockedBySchedulingQuota(ctx, a))
	a.Extra["auto_pause_5h_disabled"] = true
	require.False(t, (*RateLimitService)(nil).IsRequestBlockedBySchedulingQuota(ctx, a))
	delete(a.Extra, "auto_pause_5h_disabled")
	a.Extra["openai_excel_bps"] = true
	a.Extra["openai_excel_bps_models"] = []string{"gpt-5.1", "gpt-6-astra", "gpt-6.1-sol"}
	require.True(t, a.IsExcelBPSEnabledForModel("gpt-5.1"))
	require.False(t, (*RateLimitService)(nil).IsRequestBlockedBySchedulingQuota(ctx, a), "native Codex quota is not BPS quota")
}

func TestSchedulingQuotaGuardPreservesResetCreditThreshold(t *testing.T) {
	now := time.Now().Add(-time.Second)
	a := quotaGuardOpenAIAccount(now)
	delete(a.Credentials, "account_scheduling_threshold")
	a.Extra[OpenAIAutoResetCreditEnabledExtraKey] = true
	a.Extra[OpenAIAutoResetCredit5hThresholdExtraKey] = 0.95
	ctx := withOpenAIQuotaAutoPauseSettings(t.Context(), OpsOpenAIAccountQuotaAutoPauseSettings{DefaultThreshold5h: 0.8})
	require.Equal(t, 80.0, schedulingQuotaWindowThreshold(ctx, a, nil, "5h", now))
	a.Extra[OpenAIAutoResetCreditStateExtraKey] = &OpenAIAutoResetCreditState{Status: OpenAIAutoResetStatusAvailable, AvailableCount: 1, CheckedAt: now.Format(time.RFC3339)}
	require.Equal(t, 95.0, schedulingQuotaWindowThreshold(ctx, a, nil, "5h", now))
	require.Empty(t, schedulingQuotaCriticalWindow(ctx, a, nil, now), "an available reset credit keeps its native threshold")
	a.Credentials["account_scheduling_threshold"] = 80
	require.Equal(t, 80.0, schedulingQuotaWindowThreshold(ctx, a, nil, "5h", now), "the independent administrator account stop still applies")
}

func TestSchedulingQuotaGuardDBRecheckReleasesSlotAndSelectsNext(t *testing.T) {
	a := quotaGuardOpenAIAccount(time.Now().Add(-time.Second))
	stale := *a
	stale.Extra = nil
	b := stale
	b.ID++
	b.Priority = 5
	var acquired, released []int64
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*a, b}},
		cache:       &schedulerTestGatewayCache{}, cfg: &config.Config{},
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
			snapshotAccounts: []*Account{&stale, &b}, accountsByID: map[int64]*Account{a.ID: &stale, b.ID: &b},
		}},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired, releasedIDs: &released}),
	}
	ctx := quotaGuardLargeRequest(t.Context(), false)
	selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.Equal(t, b.ID, selection.Account.ID)
	require.Contains(t, acquired, a.ID)
	require.Contains(t, released, a.ID, "a stale snapshot must not leak the rejected account's concurrency slot")
	require.True(t, SchedulingQuotaRequestWasBlocked(ctx))
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestSchedulingQuotaGuardDoesNotMaskIncompatibleCandidate(t *testing.T) {
	ctx := WithSchedulingQuotaRequest(t.Context(), []byte(`{"model":"gpt-5.1","input":"hello"}`), ContentModerationProtocolOpenAIResponses)
	scheduler := &defaultOpenAIAccountScheduler{}
	compatible, reason := scheduler.isAccountRequestCompatibleReason(ctx, quotaGuardOpenAIAccount(time.Now().Add(-time.Second)), OpenAIAccountScheduleRequest{
		Platform: PlatformOpenAI, RequestedModel: "gpt-5.1", RequiredCapability: OpenAIEndpointCapabilitySeedance,
	})
	require.False(t, compatible)
	require.Equal(t, "capability_mismatch", reason)
	require.False(t, SchedulingQuotaRequestWasBlocked(ctx), "ineligible accounts cannot turn an unavailable-model response into a quota 429")
	require.Zero(t, ctx.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest).inputTokens)
}

func TestSchedulingQuotaGuardSchedulerFiltersColdButPreservesWarmAndOwner(t *testing.T) {
	now := time.Now().Add(-time.Second)
	a := quotaGuardOpenAIAccount(now)
	a.Extra["openai_oauth_responses_websockets_v2_enabled"] = true
	b := *a
	b.ID++
	b.Extra = nil
	b.Credentials = nil
	cache := &schedulerTestGatewayCache{}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*a, b}}, cache: cache,
		cfg: &config.Config{}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{})}
	svc.cfg.Gateway.OpenAIWS.Enabled = true
	svc.cfg.Gateway.OpenAIWS.OAuthEnabled = true
	svc.cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	ctx := quotaGuardLargeRequest(t.Context(), false)
	require.NoError(t, cache.SetSessionAccountID(ctx, 0, "openai:quota-session", a.ID, time.Hour))
	selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "quota-session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.Equal(t, b.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
	// A positively confirmed same-account prefix retains affinity.
	require.NoError(t, cache.SetSessionAccountID(ctx, 0, "openai:quota-session", a.ID, time.Hour))
	warm := quotaGuardLargeRequest(ctx, true)
	selection, _, err = svc.SelectAccountWithScheduler(warm, nil, "", "quota-session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.Equal(t, a.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
	require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, 0, "resp_quota_owner", a.ID, time.Hour))
	selection, _, err = svc.SelectAccountWithScheduler(ctx, nil, "resp_quota_owner", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.ErrorContains(t, err, SchedulingQuotaRequestTooLarge)
	require.Nil(t, selection, "quota protection must not move an immutable response owner")
}

func TestSchedulingQuotaConfirmedPrefixIsolationAndDrain(t *testing.T) {
	a := quotaGuardAnthropicAccount(time.Now().Add(-time.Second))
	a.Concurrency = 8
	key := &APIKey{ID: 10, UserID: 20}
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"` + strings.Repeat(" word", 100100) + `"}]}`)
	first := WithSchedulingQuotaRequest(t.Context(), body, ContentModerationProtocolAnthropicMessages, key)
	risk := first.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest)
	risk.estimate()
	require.True(t, (*RateLimitService)(nil).IsRequestBlockedBySchedulingQuota(first, a))
	confirmSchedulingQuotaPrefix(first, a, risk.inputTokens)
	nextBody := []byte(strings.TrimSuffix(string(body), "]}") + `,{"role":"assistant","content":"done"},{"role":"user","content":"next"}]}`)
	next := WithSchedulingQuotaRequest(t.Context(), nextBody, ContentModerationProtocolAnthropicMessages, key)
	require.False(t, (*RateLimitService)(nil).IsRequestBlockedBySchedulingQuota(next, a))
	other := *a
	other.ID++
	require.True(t, (*RateLimitService)(nil).IsRequestBlockedBySchedulingQuota(next, &other))
	otherKey := *key
	otherKey.UserID++
	isolated := WithSchedulingQuotaRequest(t.Context(), nextBody, ContentModerationProtocolAnthropicMessages, &otherKey)
	require.True(t, (*RateLimitService)(nil).IsRequestBlockedBySchedulingQuota(isolated, a))
	require.Equal(t, 1, nativeSchedulingConcurrency(next, a, nil))
	require.Equal(t, 8, a.Concurrency, "draining cannot mutate configured capacity")
	a.Extra = nil
	require.Equal(t, 8, nativeSchedulingConcurrency(next, a, nil))
}

type quotaDrainSemaphore struct {
	schedulerTestConcurrencyCache
	limits []int
}

func (c *quotaDrainSemaphore) AcquireAccountSlot(ctx context.Context, id int64, limit int, request string) (bool, error) {
	c.limits = append(c.limits, limit)
	return true, nil
}
func TestSchedulingQuotaDrainUsesNativeSemaphore(t *testing.T) {
	account := quotaGuardOpenAIAccount(time.Now().Add(-time.Second))
	account.Concurrency = 8
	cache := &schedulerTestGatewayCache{}
	semaphore := &quotaDrainSemaphore{}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*account}}, cache: cache, cfg: &config.Config{}, concurrencyService: NewConcurrencyService(semaphore)}
	ctx := WithSchedulingQuotaRequest(t.Context(), []byte(`{"model":"gpt-5.1","input":"hello"}`), ContentModerationProtocolOpenAIResponses)
	selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "quota-drain", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
	require.Equal(t, []int{1}, semaphore.limits, "the existing distributed slot acquisition must use one critical-window slot")
	require.Equal(t, 8, account.Concurrency)
}

func TestSchedulingQuotaDrainRankingUsesEffectiveCapacity(t *testing.T) {
	critical := quotaGuardOpenAIAccount(time.Now().Add(-time.Second))
	critical.Concurrency = 32
	normal := *critical
	normal.ID, normal.Concurrency, normal.Extra = 73, 16, nil
	ctx := WithSchedulingQuotaRequest(t.Context(), []byte(`{"model":"gpt-5.1","input":"hello"}`), ContentModerationProtocolOpenAIResponses)
	scheduler := &defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{}, stats: newOpenAIAccountRuntimeStats()}
	wins := 0
	for i := range 256 {
		req := OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.1", SessionHash: string(rune(i + 33))}
		loads := map[int64]*AccountLoadInfo{critical.ID: {AccountID: critical.ID}, normal.ID: {AccountID: normal.ID}}
		idle := scheduler.buildOpenAIAccountLoadPlan(ctx, req, []*Account{critical, &normal}, loads)
		if idle.selectionOrder[0].account.ID == critical.ID {
			wins++
		}
		loads[critical.ID].CurrentConcurrency = 1
		busy := scheduler.buildOpenAIAccountLoadPlan(ctx, req, []*Account{critical, &normal}, loads)
		require.Equal(t, normal.ID, busy.selectionOrder[0].account.ID, "effective fullness outranks spare configured slots")
	}
	require.Greater(t, wins, 0, "critical accounts retain a bounded share when idle")
	require.Less(t, wins, 40, "one effective slot must not outweigh sixteen")
	for _, configured := range []int{0, 32} {
		critical.Concurrency = configured
		loads := gatewayAccountLoadRequest(ctx, []*Account{critical, &normal}, false, nil)
		require.Equal(t, 1, loads[0].MaxConcurrency, "native load estimates use the same protection")
		require.Equal(t, configured, critical.Concurrency)
	}
}

func TestSchedulingQuotaProfileWindowsOverridesAndNativeDrain(t *testing.T) {
	now := time.Now().Add(-time.Second)
	a := quotaGuardAnthropicAccount(now)
	delete(a.Credentials, accountSchedulingThresholdCredentialKey)
	a.Concurrency = 8
	a.Extra = persistableAnthropicObservations(map[string]UsageObservation{
		"five_hour.utilization": {Value: 84.0, Source: "upstream_headers", SampledAt: &now},
		"five_hour.resets_at":   {Value: now.Add(time.Hour).Format(time.RFC3339), Source: "upstream_headers", SampledAt: &now},
		"seven_day.utilization": {Value: 81.0, Source: "upstream_headers", SampledAt: &now},
		"seven_day.resets_at":   {Value: now.Add(24 * time.Hour).Format(time.RFC3339), Source: "upstream_headers", SampledAt: &now},
	}, anthropicCredentialIdentity(a))
	a.Extra["session_window_utilization"] = .84
	a.Extra["passive_usage_7d_utilization"] = .81
	a.Extra["passive_usage_7d_reset"] = now.Add(24 * time.Hour).Format(time.RFC3339)
	until := now.Add(time.Hour)
	a.SessionWindowEnd = &until
	policy := defaultSchedulingQuotaProtection()
	policy.AnthropicTierThresholdsEnabled = true
	thresholds := map[string]int{PlatformAnthropic: 100}
	decision := EvaluateAccountSchedulingThreshold(a, thresholds, now, policy)
	require.True(t, decision.ShouldPause)
	require.Equal(t, "7d", decision.Window)
	require.Equal(t, 80, decision.ThresholdPercent)
	a.Credentials["subscription_type"] = "max_20x"
	require.False(t, EvaluateAccountSchedulingThreshold(a, thresholds, now, policy).ShouldPause)
	reduced := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: SchedulingQuotaMetadataExtra(a)}
	require.Equal(t, 95, anthropicSchedulingWindowThreshold(reduced, thresholds, "5h", policy))
	require.Equal(t, 70, anthropicSchedulingWindowThreshold(a, map[string]int{PlatformAnthropic: 70}, "5h", policy), "a stricter global stop is never relaxed")
	a.Credentials[accountSchedulingThresholdCredentialKey] = 100
	require.False(t, EvaluateAccountSchedulingThreshold(a, thresholds, now, policy).ShouldPause)
	require.Empty(t, schedulingQuotaCriticalWindow(t.Context(), a, thresholds, now, policy), "explicit per-account disable wins")
	delete(a.Credentials, accountSchedulingThresholdCredentialKey)
	delete(a.Credentials, "subscription_type")
	// Both gating and concurrency consume the same hot-updated policy.
	settings := NewSettingService(nil, &config.Config{})
	settings.SetQuotaSchedulingSettings(OpsOpenAIAccountQuotaAutoPauseSettings{}, policy)
	ctx := quotaGuardLargeRequest(t.Context(), false)
	require.NotEmpty(t, schedulingQuotaRequestBlockReason(ctx, a, settings))
	require.Equal(t, 1, nativeSchedulingConcurrency(ctx, a, settings))
	policy.LargeColdInputTokens = 120000
	settings.SetQuotaSchedulingSettings(OpsOpenAIAccountQuotaAutoPauseSettings{}, policy)
	require.Empty(t, schedulingQuotaRequestBlockReason(ctx, a, settings))
	policy.LargeColdInputTokens = 1000
	policy.GuardLargeCold = false
	settings.SetQuotaSchedulingSettings(OpsOpenAIAccountQuotaAutoPauseSettings{}, policy)
	require.Empty(t, schedulingQuotaRequestBlockReason(ctx, a, settings))
	require.Equal(t, 1, nativeSchedulingConcurrency(ctx, a, settings), "cold-input opt-out does not disable draining")
	policy.AnthropicTierThresholdsEnabled = false
	settings.SetQuotaSchedulingSettings(OpsOpenAIAccountQuotaAutoPauseSettings{}, policy)
	require.Equal(t, 8, nativeSchedulingConcurrency(ctx, a, settings))
	// A custom band and sample TTL change decisions without rewriting evidence.
	b := quotaGuardAnthropicAccount(now)
	policy.CriticalBandPercent = .5
	require.Empty(t, schedulingQuotaCriticalWindow(ctx, b, nil, now, policy))
	policy.CriticalBandPercent = 2
	policy.SampleMaxAgeSeconds = 60
	require.Equal(t, "5h", schedulingQuotaCriticalWindow(ctx, b, nil, now.Add(59*time.Second), policy))
	require.Empty(t, schedulingQuotaCriticalWindow(ctx, b, nil, now.Add(60*time.Second), policy))
}
