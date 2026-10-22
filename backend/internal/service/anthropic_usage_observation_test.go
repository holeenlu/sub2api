package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type anthropicDetailsFetcher struct {
	ClaudeUsageFetcher
	response   *ClaudeUsageResponse
	queryError error
	usageCalls int
}

func (f *anthropicDetailsFetcher) FetchUsageWithOptions(context.Context, *ClaudeUsageFetchOptions) (*ClaudeUsageResponse, error) {
	f.usageCalls++
	return f.response, f.queryError
}
func TestAnthropicUsageWindowsKeepCachedObservationTime(t *testing.T) {
	a := Account{ID: 12, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"scope": "user:profile user:inference", "access_token": "secret-token"}}
	repo := &claudeUsageSnapshotRepo{account: a}
	var response ClaudeUsageResponse
	require.NoError(t, json.Unmarshal([]byte(`{"seven_day":{"utilization":42,"resets_at":null}}`), &response))
	fetcher := &anthropicDetailsFetcher{response: &response}
	svc := &AccountUsageService{accountRepo: repo, usageFetcher: fetcher, cache: NewUsageCache()}
	info, err := svc.GetUsage(t.Context(), 12, true)
	require.NoError(t, err)
	require.Equal(t, 42.0, info.SevenDay.Utilization)
	require.Nil(t, info.AnthropicStatus)
	require.NotContains(t, info.Anthropic.Observations, "profile.query_status")
	sampled := info.Anthropic.Observations["seven_day.utilization"].SampledAt
	again, err := svc.GetUsage(t.Context(), 12)
	require.NoError(t, err)
	require.Equal(t, 1, fetcher.usageCalls)
	require.True(t, again.Anthropic.Observations["seven_day.utilization"].SampledAt.Equal(*sampled))
	raw, err := json.Marshal(again)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret-token")
}

func TestAnthropicUsagePermissions(t *testing.T) {
	for _, tc := range []struct {
		name, kind, scope string
		allowed           bool
	}{
		{"profile", AccountTypeOAuth, "user:inference user:profile", true},
		{"inference only", AccountTypeOAuth, "user:inference", false},
		{"missing", AccountTypeOAuth, "", false},
		{"substring", AccountTypeOAuth, "other:user:profile user:profile:read", false},
		{"setup token", AccountTypeSetupToken, "user:profile user:inference", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{Platform: PlatformAnthropic, Type: tc.kind, Credentials: map[string]any{"scope": tc.scope}}
			require.Equal(t, tc.allowed, a.CanGetUsage())
			if !tc.allowed {
				fetcher := &claudeSnapshotFetcher{}
				svc := &AccountUsageService{usageFetcher: fetcher, cache: NewUsageCache()}
				info, err := svc.GetUsageForAccount(t.Context(), a, true)
				require.NoError(t, err)
				require.Zero(t, fetcher.calls, "must not probe privileged endpoints")
				for key := range info.Anthropic.Observations {
					require.NotContains(t, key, "authorization.")
				}
			}
		})
	}
}

func TestAnthropicUsageObservations_PersistReloadAndSparseHeaders(t *testing.T) {
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	reset := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	raw := `{
  "limits":[
   {"kind":"session","percent":0,"resets_at":null},
   {"kind":"weekly_all","percent":42,"resets_at":"` + reset.Format(time.RFC3339) + `"},
   {"kind":"weekly_scoped","percent":65,"scope":{"model":{"display_name":"Fable"}},"is_active":false},
   {"kind":"weekly_all","percent":99,"scope":{"surface":{"display_name":"Cowork"}}}
  ],
  "extra_usage":{"is_enabled":false,"monthly_limit":0,"used_credits":0,"utilization":null}
 }`
	var resp ClaudeUsageResponse
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	repo := &claudeUsageSnapshotRepo{account: Account{ID: 12, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "token-a", "scope": "user:profile user:inference"}}}
	svc := &AccountUsageService{accountRepo: repo, cache: NewUsageCache()}
	info := svc.buildUsageInfo(&resp, &at)
	enrichAnthropicUsage(&repo.account, info)
	require.Equal(t, 42.0, info.SevenDay.Utilization)
	require.Equal(t, 65.0, info.SevenDayFable.Utilization)
	svc.syncActiveToPassive(t.Context(), 12, info)
	reloaded := &AccountUsageService{accountRepo: repo, cache: NewUsageCache()}
	loaded, err := reloaded.GetPassiveUsage(t.Context(), 12)
	require.NoError(t, err)
	fields := loaded.Anthropic.Observations
	require.NotContains(t, resp.RawFields, "extra_usage", "retired data must not remain in the successful API cache")
	require.NotContains(t, fields, "extra_usage.is_enabled")
	require.NotContains(t, fields, "extra_usage.monthly_limit")
	require.NotContains(t, fields, "extra_usage.utilization")
	require.Equal(t, "upstream_api", fields["seven_day.utilization"].Source)
	require.True(t, fields["seven_day.utilization"].SampledAt.Equal(at))
	require.NotContains(t, fields, "profile.rate_limit_tier")
	require.NotContains(t, fields, "profile.secret")
	require.NotContains(t, fields, "limits")

	fresh, err := repo.GetByID(t.Context(), 12)
	require.NoError(t, err)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.25")
	headers.Set("anthropic-ratelimit-unified-overage-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-overage-disabled-reason", "org_level_disabled")
	(&RateLimitService{accountRepo: repo}).samplePassiveUsageFromHeaders(t.Context(), fresh, headers)
	loaded, err = reloaded.GetPassiveUsage(t.Context(), 12)
	require.NoError(t, err)
	fields = loaded.Anthropic.Observations
	require.Equal(t, 25.0, loaded.FiveHour.Utilization)
	require.Equal(t, "upstream_headers", fields["five_hour.utilization"].Source)
	require.True(t, fields["five_hour.utilization"].SampledAt.After(at))
	require.True(t, fields["seven_day.utilization"].SampledAt.Equal(at), "unrelated header must not restamp weekly data")
	require.NotContains(t, fields, "overage.disabled_reason")

	repo.account.Credentials["access_token"] = "different-owner"
	loaded, err = reloaded.GetPassiveUsage(t.Context(), 12)
	require.NoError(t, err)
	require.NotContains(t, loaded.Anthropic.Observations, "profile.rate_limit_tier")
	require.Nil(t, loaded.SevenDay, "replaced credentials must not inherit old windows")
}

func TestAnthropicUsageObservations_NullableScopes(t *testing.T) {
	for _, tc := range []struct {
		name, kind, scope, want string
	}{
		{"null dimensions", "weekly_all", `{"model":null,"surface":null}`, "seven_day"},
		{"session", "session", `{"surface":null}`, "five_hour"},
		{"model with null surface", "weekly_scoped", `{"model":{"display_name":"Fable"},"surface":null}`, "seven_day_fable"},
		{"opaque model id", "weekly_scoped", `{"model":{"id":"opaque-id","display_name":"Fable"}}`, "seven_day_fable"},
		{"sonnet label", "weekly_scoped", `{"model":{"id":"opaque-id","display_name":"Sonnet"},"surface":null}`, "seven_day_sonnet"},
		{"mixed scope", "weekly_scoped", `{"model":{"display_name":"Fable"},"surface":{"display_name":"Cowork"}}`, ""},
		{"surface only", "weekly_all", `{"model":null,"surface":{"display_name":"Cowork"}}`, ""},
		{"unknown dimension", "weekly_scoped", `{"model":{"display_name":"Fable"},"team":null}`, ""},
		{"id without label", "weekly_scoped", `{"model":{"id":"claude-fable-5"}}`, ""},
		{"unknown model label", "weekly_scoped", `{"model":{"id":"claude-fable-5","display_name":"Other"}}`, ""},
		{"missing model", "weekly_scoped", `{"model":null,"surface":null}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, canonicalAnthropicLimit(ClaudeUsageLimit{Kind: tc.kind, Scope: json.RawMessage(tc.scope)}))
		})
	}
}

func TestAnthropicUsageObservations_QueryReplacesHistoricalFable(t *testing.T) {
	for _, percent := range []float64{0, 12} {
		t.Run(strconv.FormatFloat(percent, 'f', 0, 64), func(t *testing.T) {
			reset := time.Now().UTC().Add(4 * 24 * time.Hour).Truncate(time.Second)
			repo := &claudeUsageSnapshotRepo{account: Account{ID: 12, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "token-a", "scope": "user:profile user:inference"},
				Extra:       map[string]any{"passive_usage_7d_oi_utilization": 0.5, "passive_usage_7d_oi_reset": reset.Unix()}}}
			// This is an ordinary query, with no reset redemption or cleared_at evidence.
			fetcher := &claudeSnapshotFetcher{response: claudeUsageFixture(t, map[string]any{"limits": []any{
				map[string]any{"kind": "weekly_all", "percent": 22, "resets_at": reset.Format(time.RFC3339), "scope": map[string]any{"model": nil, "surface": nil}},
				map[string]any{"kind": "weekly_scoped", "percent": percent, "resets_at": reset.Format(time.RFC3339), "scope": map[string]any{"model": map[string]any{"id": "opaque-id", "display_name": "Fable"}, "surface": nil}},
			}})}
			svc := &AccountUsageService{accountRepo: repo, usageFetcher: fetcher, cache: NewUsageCache()}
			before, err := svc.GetPassiveUsage(t.Context(), 12)
			require.NoError(t, err)
			require.Equal(t, 50.0, before.SevenDayFable.Utilization)
			active, err := svc.GetUsage(t.Context(), 12, true)
			require.NoError(t, err)
			require.NotNil(t, active.SevenDay)
			require.Equal(t, 22.0, active.SevenDay.Utilization)
			require.Equal(t, percent, active.SevenDayFable.Utilization)
			require.Equal(t, 1, fetcher.calls)
			reloaded, err := (&AccountUsageService{accountRepo: repo, cache: NewUsageCache()}).GetPassiveUsage(t.Context(), 12)
			require.NoError(t, err)
			require.Equal(t, percent, reloaded.SevenDayFable.Utilization)
			require.True(t, reset.Equal(*reloaded.SevenDayFable.ResetsAt))
			observation := reloaded.Anthropic.Observations["seven_day_fable.utilization"]
			require.Equal(t, "upstream_api", observation.Source)
			require.NotNil(t, observation.SampledAt)
			require.True(t, observation.SampledAt.Equal(*active.Anthropic.Observations["seven_day_fable.utilization"].SampledAt))
			require.NotContains(t, reloaded.Anthropic.Observations, "seven_day_fable.cleared_at")
		})
	}
}

func TestAnthropicUsageObservations_NullAndInvalidValues(t *testing.T) {
	at := time.Now().UTC()
	var resp ClaudeUsageResponse
	require.NoError(t, json.Unmarshal([]byte(`{"extra_usage":null,"seven_day":null,"limits":[]}`), &resp))
	fields := collectAnthropicAPIObservations(&resp, &at)
	require.NotContains(t, fields, "extra_usage.is_enabled")
	require.Contains(t, fields, "seven_day.resets_at")
	require.Nil(t, fields["seven_day.resets_at"].Value)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "NaN")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "-1")
	headers.Set("anthropic-ratelimit-unified-7d_oi-utilization", "Inf")
	require.Empty(t, anthropicHeaderObservations(headers, at))

	require.NoError(t, json.Unmarshal([]byte("{\"seven_day\":{\"utilization\":80,\"resets_at\":\"2099-01-01T00:00:00Z\"},\"limits\":[{\"kind\":\"weekly_all\",\"percent\":null,\"resets_at\":null}]}"), &resp))
	fields = collectAnthropicAPIObservations(&resp, &at)
	require.Nil(t, fields["seven_day.utilization"].Value, "a current null must not inherit the previous window")
	require.Nil(t, fields["seven_day.resets_at"].Value)
	require.NoError(t, json.Unmarshal([]byte("{\"limits\":[{\"kind\":\"weekly_scoped\",\"percent\":20,\"scope\":{\"model\":{\"id\":\"claude-fable-5\",\"display_name\":\"Fable\"}}},{\"kind\":\"weekly_scoped\",\"percent\":90,\"scope\":{\"model\":{\"id\":\"claude-fable-5-1\",\"display_name\":\"Fable\"}}}]}"), &resp))
	fields = collectAnthropicAPIObservations(&resp, &at)
	require.NotContains(t, fields, "seven_day_fable.utilization", "different model meters must not collapse into a family-wide quota")
}

func TestAnthropicUsageObservations_WindowChangeInvalidatesMissingUtilization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	oldReset := now.Add(24 * time.Hour)
	repo := &claudeUsageSnapshotRepo{account: Account{ID: 12, Platform: PlatformAnthropic, Type: AccountTypeSetupToken, Extra: map[string]any{"passive_usage_7d_utilization": 0.9, "passive_usage_7d_reset": oldReset.Unix()}}}
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(oldReset.Add(24*time.Hour).Unix(), 10))
	(&RateLimitService{accountRepo: repo}).samplePassiveUsageFromHeaders(t.Context(), &repo.account, headers)
	loaded, err := (&AccountUsageService{accountRepo: repo, cache: NewUsageCache()}).GetPassiveUsage(t.Context(), 12)
	require.NoError(t, err)
	require.Nil(t, loaded.Anthropic.Observations["seven_day.utilization"].Value)
	require.Zero(t, loaded.SevenDay.Utilization)
}

func TestAnthropicUsageObservations_LocalStatisticsTime(t *testing.T) {
	at := time.Now().UTC().Add(-30 * time.Second)
	info := &UsageInfo{SevenDay: &UsageProgress{Utilization: 40}, Anthropic: &AnthropicUsageDetails{Observations: map[string]UsageObservation{}}}
	annotateAnthropicWindowStats(info, "seven_day", &WindowStats{Cost: 12}, at)
	require.Equal(t, "local_statistics", info.Anthropic.Observations["seven_day.window_stats"].Source)
	require.True(t, info.Anthropic.Observations["seven_day.window_stats"].SampledAt.Equal(at))
	require.NotContains(t, info.Anthropic.Observations, "seven_day.estimated_total_cost")
}

func TestAnthropicAccountStatus_OnlyCurrentExceptions(t *testing.T) {
	now := time.Now().UTC()
	makeFields := func(values map[string]any) map[string]UsageObservation {
		fields := make(map[string]UsageObservation)
		for key, value := range values {
			fields[key] = UsageObservation{Value: value, Source: "upstream_headers", SampledAt: &now}
		}
		return fields
	}
	for _, tc := range []struct {
		name    string
		values  map[string]any
		limited bool
		model   bool
		code    string
	}{
		{"normal overage disabled is not a fault", map[string]any{"limit.status": "allowed", "limit.representative_claim": "five_hour", "overage.status": "rejected", "overage.disabled_reason": "org_level_disabled"}, false, false, ""},
		{"profile 403 is not inference revocation", map[string]any{"profile.query_status": "http_403"}, false, false, ""},
		{"limit reset in future", map[string]any{"limit.status": "rejected", "limit.representative_claim": "five_hour", "five_hour.resets_at": now.Add(time.Hour).Format(time.RFC3339)}, true, false, ""},
		{"expired limit", map[string]any{"limit.status": "rejected", "limit.representative_claim": "five_hour", "five_hour.resets_at": now.Add(-time.Hour).Format(time.RFC3339)}, false, false, ""},
		{"fable scope", map[string]any{"seven_day_fable.status": "rejected", "seven_day_fable.resets_at": now.Add(time.Hour).Format(time.RFC3339)}, false, true, ""},
		{"inactive modern window", map[string]any{"seven_day_fable.severity": "blocked", "seven_day_fable.is_active": false}, false, false, ""},
		{"overage permits global traffic", map[string]any{"limit.status": "allowed", "five_hour.status": "rejected"}, false, false, ""},
		{"authentication rejected", map[string]any{"usage.query_reason": "authentication"}, false, false, "authentication"},
		{"authorization revoked", map[string]any{"usage.query_reason": "revoked"}, false, false, "revoked"},
		{"account disabled", map[string]any{"usage.query_reason": "disabled"}, false, false, "disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := anthropicAccountStatus(makeFields(tc.values), now)
			if !tc.limited && !tc.model && tc.code == "" {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tc.limited, got.RateLimited)
			require.Equal(t, tc.model, len(got.ModelRateLimits) > 0)
			require.Equal(t, tc.code, got.ErrorCode)
		})
	}
	fields := makeFields(map[string]any{"usage.query_reason": "revoked"})
	later := now.Add(time.Second)
	fields["usage.query_status"] = UsageObservation{Value: "ok", Source: "upstream_api", SampledAt: &later}
	require.Nil(t, anthropicAccountStatus(fields, later), "successful query clears older authorization failure")
	fields = makeFields(map[string]any{"limit.status": "rejected"})
	require.Nil(t, anthropicAccountStatus(fields, now.Add(6*time.Minute)), "unknown reset must not persist forever")
	cleared := makeFields(map[string]any{"seven_day_fable.severity": "blocked", "seven_day_fable.resets_at": now.Add(time.Hour).Format(time.RFC3339)})
	cleared["seven_day_fable.status"] = UsageObservation{Value: "allowed", Source: "upstream_headers", SampledAt: &later}
	require.Nil(t, anthropicAccountStatus(cleared, later), "later header status supersedes earlier API severity")
	stale := now.Add(-6 * time.Hour)
	newerWindow := makeFields(map[string]any{"five_hour.status": "rejected", "five_hour.resets_at": now.Add(time.Hour).Format(time.RFC3339)})
	newerWindow["five_hour.status"] = UsageObservation{Value: "rejected", Source: "upstream_headers", SampledAt: &stale}
	require.Nil(t, anthropicAccountStatus(newerWindow, now), "a new window cannot inherit the previous window's rejection")
	recovered := makeFields(map[string]any{"seven_day_fable.status": "rejected", "seven_day_fable.resets_at": now.Add(time.Hour).Format(time.RFC3339)})
	recovered["seven_day_fable.utilization"] = UsageObservation{Value: 42.0, Source: "upstream_api", SampledAt: &later}
	require.Nil(t, anthropicAccountStatus(recovered, later), "fresh lower usage clears the older exhausted-window hint")
	fields["limit.status"] = UsageObservation{Value: "rejected", Source: "estimate", SampledAt: &now}
	require.Nil(t, anthropicAccountStatus(fields, now), "only measured upstream status may raise a warning")
}

func TestAnthropicAccountStatus_IdentityAndQueryFailure(t *testing.T) {
	repo := &claudeUsageSnapshotRepo{account: Account{ID: 12, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "a", "scope": "user:profile"}}}
	svc := &AccountUsageService{accountRepo: repo, cache: NewUsageCache()}
	at := time.Now()
	svc.recordAnthropicQueryFailure(t.Context(), &repo.account, &ClaudeUsageHTTPError{StatusCode: 401, Reason: "revoked"}, at)
	require.Equal(t, "revoked", AnthropicAccountStatusFromAccount(&repo.account).ErrorCode)
	// Re-reading a cached/failed request keeps the original sample.
	sample := storedAnthropicObservations(&repo.account)["usage.query_reason"]
	require.True(t, sample.SampledAt.Equal(at))
	repo.account.Credentials["access_token"] = "replacement"
	require.Nil(t, AnthropicAccountStatusFromAccount(&repo.account))
}

func TestAnthropicAccountStatus_QueryCacheReloadAndRecovery(t *testing.T) {
	account := Account{ID: 12, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "secret", "scope": "user:profile"}}
	repo := &claudeUsageSnapshotRepo{account: account}
	fetcher := &anthropicDetailsFetcher{queryError: &ClaudeUsageHTTPError{StatusCode: 401, Reason: "revoked"}}
	svc := &AccountUsageService{accountRepo: repo, usageFetcher: fetcher, cache: NewUsageCache()}
	first, err := svc.GetUsage(t.Context(), 12)
	require.NoError(t, err)
	require.Equal(t, "revoked", first.AnthropicStatus.ErrorCode)
	sampled := storedAnthropicObservations(&repo.account)["usage.query_reason"].SampledAt
	second, err := svc.GetUsage(t.Context(), 12)
	require.NoError(t, err)
	require.Equal(t, "revoked", second.AnthropicStatus.ErrorCode)
	require.Equal(t, 1, fetcher.usageCalls)
	require.True(t, sampled.Equal(*storedAnthropicObservations(&repo.account)["usage.query_reason"].SampledAt))
	reloaded, err := (&AccountUsageService{accountRepo: repo, cache: NewUsageCache()}).GetPassiveUsage(t.Context(), 12)
	require.NoError(t, err)
	require.Equal(t, "revoked", reloaded.AnthropicStatus.ErrorCode)
	require.Equal(t, StatusActive, repo.account.Status, "display hints must not disable native account state")
	fetcher.queryError = nil
	fetcher.response = &ClaudeUsageResponse{}
	svc.cache.apiCache.Delete(int64(12))
	recovered, err := svc.GetUsage(t.Context(), 12)
	require.NoError(t, err)
	require.Nil(t, recovered.AnthropicStatus)
	require.Nil(t, AnthropicAccountStatusFromAccount(&repo.account))
}

func TestAnthropicUsageCleanup_DoesNotReplayRetiredMetadata(t *testing.T) {
	now := time.Now().UTC()
	a := Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "owner"}, Extra: map[string]any{"passive_usage_7d_utilization": 0.4}}
	for _, key := range []string{"profile.rate_limit_tier", "extra_usage.used_credits", "authorization.scopes", "overage.disabled_reason", "limits", "seven_day_opus.utilization"} {
		a.Extra[anthropicObservationPrefix+key] = map[string]any{"value": "retired", "source": "upstream_api", "sampled_at": now, "identity": anthropicCredentialIdentity(&a)}
	}
	require.Empty(t, storedAnthropicObservations(&a))
	info, err := (&AccountUsageService{cache: NewUsageCache()}).getPassiveUsageForAccount(t.Context(), &a)
	require.NoError(t, err)
	require.Equal(t, 40.0, info.SevenDay.Utilization, "retired keys cannot invalidate a valid pre-observation snapshot")
	for key := range info.Anthropic.Observations {
		require.True(t, isAnthropicObservationField(key))
	}
	require.Len(t, a.Extra, 7, "reading never deletes database fields")
	require.Empty(t, persistableAnthropicObservations(map[string]UsageObservation{"profile.rate_limit_tier": {Value: "unused", Source: "upstream_api", SampledAt: &now}}, anthropicCredentialIdentity(&a)))
}

func TestAnthropicUsageCleanup_SingleWindowProjection(t *testing.T) {
	now := time.Now().UTC()
	reset := now.Add(24 * time.Hour)
	resp := claudeUsageFixture(t, map[string]any{"limits": []map[string]any{
		{"kind": "weekly_all", "percent": 42, "resets_at": reset.Format(time.RFC3339)},
		{"kind": "weekly_scoped", "percent": 65, "scope": map[string]any{"model": map[string]any{"display_name": "Fable"}}},
		{"kind": "weekly_scoped", "percent": 23, "scope": map[string]any{"model": map[string]any{"display_name": "Sonnet"}}},
	}})
	svc := &AccountUsageService{}
	require.Equal(t, "null", string(resp.RawFields["limits"]))
	info := svc.buildUsageInfo(resp, &now)
	require.Equal(t, 42.0, info.SevenDay.Utilization, "vendor refresh must receive normalized modern windows before enrichment")
	require.Equal(t, 65.0, info.SevenDayFable.Utilization)
	require.Equal(t, 23.0, info.SevenDaySonnet.Utilization, "independent channel monitor retains Sonnet")
	a := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "new-owner"}, Extra: map[string]any{
		"passive_usage_7d_oi_utilization":                          0.99,
		"passive_usage_7d_oi_reset":                                reset.Unix(),
		anthropicObservationPrefix + "seven_day_fable.utilization": map[string]any{"value": 99, "source": "upstream_headers", "sampled_at": now, "identity": "old-owner"},
	}}
	fresh := svc.buildUsageInfo(claudeUsageFixture(t, map[string]any{"five_hour": map[string]any{"utilization": 10}}), &now)
	enrichAnthropicUsage(a, fresh)
	require.Nil(t, fresh.SevenDayFable, "no second legacy fallback may reintroduce previous identity data")
	require.Equal(t, 10.0, fresh.FiveHour.Utilization)
}

func TestAnthropicUsageCleanup_ManagedStateSurvivesOrdinaryEdit(t *testing.T) {
	key := anthropicObservationPrefix + "seven_day.utilization"
	retired := anthropicObservationPrefix + "profile.rate_limit_tier"
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		12: {ID: 12, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "token"},
			Extra: map[string]any{key: "stored", retired: "historical"}},
	}}
	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(t.Context(), 12, &UpdateAccountInput{Extra: map[string]any{key: "forged", "ordinary": "value"}})
	require.NoError(t, err)
	require.Equal(t, "stored", updated.Extra[key])
	require.Equal(t, "historical", updated.Extra[retired], "no physical data deletion during ordinary edit")
	require.Equal(t, "value", updated.Extra["ordinary"])
	updates := map[string]any{key: "forged", retired: "forged"}
	require.NoError(t, svc.UpdateAccountExtra(t.Context(), 12, updates))
	require.Empty(t, updates, "generic patch must not reactivate retired observation fields")
}
