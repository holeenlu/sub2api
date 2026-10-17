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
	response                 *ClaudeUsageResponse
	profile                  *ClaudeProfileResponse
	queryError, profileError error
	usageCalls, profileCalls int
}

func (f *anthropicDetailsFetcher) FetchUsageWithOptions(context.Context, *ClaudeUsageFetchOptions) (*ClaudeUsageResponse, error) {
	f.usageCalls++
	return f.response, f.queryError
}
func (f *anthropicDetailsFetcher) FetchProfileWithOptions(context.Context, *ClaudeUsageFetchOptions) (*ClaudeProfileResponse, error) {
	f.profileCalls++
	return f.profile, f.profileError
}

func TestAnthropicUsageProfileFailureKeepsStoredTierAndCachedObservationTime(t *testing.T) {
	a := Account{ID: 12, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"scope": "user:profile user:inference", "access_token": "secret-token"}}
	old := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	a.Extra = persistableAnthropicObservations(map[string]UsageObservation{"profile.rate_limit_tier": {Value: "default_claude_max_20x", Source: "upstream_api", SampledAt: &old}}, anthropicCredentialIdentity(&a))
	repo := &claudeUsageSnapshotRepo{account: a}
	var response ClaudeUsageResponse
	require.NoError(t, json.Unmarshal([]byte(`{"seven_day":{"utilization":42,"resets_at":null}}`), &response))
	fetcher := &anthropicDetailsFetcher{response: &response, profileError: &ClaudeUsageHTTPError{StatusCode: 403}}
	svc := &AccountUsageService{accountRepo: repo, usageFetcher: fetcher, cache: NewUsageCache()}
	info, err := svc.GetUsage(t.Context(), 12, true)
	require.NoError(t, err)
	require.Equal(t, 42.0, info.SevenDay.Utilization)
	require.Equal(t, "http_403", info.Anthropic.Observations["profile.query_status"].Value)
	require.True(t, info.Anthropic.Observations["profile.rate_limit_tier"].SampledAt.Equal(old))
	sampled := info.Anthropic.Observations["seven_day.utilization"].SampledAt
	again, err := svc.GetUsage(t.Context(), 12)
	require.NoError(t, err)
	require.Equal(t, 1, fetcher.usageCalls)
	require.Equal(t, 1, fetcher.profileCalls)
	require.True(t, again.Anthropic.Observations["seven_day.utilization"].SampledAt.Equal(*sampled))
	raw, err := json.Marshal(again)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret-token")
}

func TestAnthropicUsagePermissions(t *testing.T) {
	for _, tc := range []struct {
		name, kind, scope, access string
		allowed                   bool
	}{
		{"profile", AccountTypeOAuth, "user:inference user:profile", "granted", true},
		{"inference only", AccountTypeOAuth, "user:inference", "denied", false},
		{"missing", AccountTypeOAuth, "", "unknown", false},
		{"substring", AccountTypeOAuth, "other:user:profile user:profile:read", "denied", false},
		{"setup token", AccountTypeSetupToken, "user:profile user:inference", "denied", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{Platform: PlatformAnthropic, Type: tc.kind, Credentials: map[string]any{"scope": tc.scope}}
			require.Equal(t, tc.allowed, a.CanGetUsage())
			require.Equal(t, tc.access, anthropicProfileAccess(a))
			if !tc.allowed {
				fetcher := &claudeSnapshotFetcher{}
				svc := &AccountUsageService{usageFetcher: fetcher, cache: NewUsageCache()}
				info, err := svc.GetUsageForAccount(t.Context(), a, true)
				require.NoError(t, err)
				require.Zero(t, fetcher.calls, "must not probe privileged endpoints")
				require.Equal(t, tc.access, info.Anthropic.ProfileAccess)
				require.Nil(t, info.Anthropic.Observations["authorization.scopes"].SampledAt)
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
	require.NoError(t, json.Unmarshal([]byte(`{"organization":{"rate_limit_tier":"default_claude_max_20x","has_extra_usage_enabled":false,"secret":"do-not-expose"}}`), &resp.Profile))
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
	require.Equal(t, false, fields["extra_usage.is_enabled"].Value)
	require.Equal(t, float64(0), fields["extra_usage.monthly_limit"].Value)
	require.Nil(t, fields["extra_usage.utilization"].Value)
	require.Equal(t, "upstream_api", fields["seven_day.utilization"].Source)
	require.True(t, fields["seven_day.utilization"].SampledAt.Equal(at))
	require.Equal(t, "default_claude_max_20x", fields["profile.rate_limit_tier"].Value)
	require.NotContains(t, fields, "profile.secret")
	require.Len(t, fields["limits"].Value, 4)

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
	require.Equal(t, "org_level_disabled", fields["overage.disabled_reason"].Value)

	repo.account.Credentials["access_token"] = "different-owner"
	loaded, err = reloaded.GetPassiveUsage(t.Context(), 12)
	require.NoError(t, err)
	require.NotContains(t, loaded.Anthropic.Observations, "profile.rate_limit_tier")
	require.Nil(t, loaded.SevenDay, "replaced credentials must not inherit old windows")
}

func TestAnthropicUsageObservations_NullAndInvalidValues(t *testing.T) {
	at := time.Now().UTC()
	var resp ClaudeUsageResponse
	require.NoError(t, json.Unmarshal([]byte(`{"extra_usage":null,"seven_day":null,"limits":[]}`), &resp))
	fields := collectAnthropicAPIObservations(&resp, &at)
	require.Contains(t, fields, "extra_usage.is_enabled")
	require.Nil(t, fields["extra_usage.is_enabled"].Value)
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
	require.NoError(t, json.Unmarshal([]byte("{\"limits\":[{\"kind\":\"weekly_scoped\",\"percent\":20,\"scope\":{\"model\":{\"id\":\"claude-fable-5\"}}},{\"kind\":\"weekly_scoped\",\"percent\":90,\"scope\":{\"model\":{\"id\":\"claude-fable-5-1\"}}}]}"), &resp))
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

func TestAnthropicUsageObservations_LocalStatisticsTimeAndEstimate(t *testing.T) {
	at := time.Now().UTC().Add(-30 * time.Second)
	info := &UsageInfo{SevenDay: &UsageProgress{Utilization: 40}, Anthropic: &AnthropicUsageDetails{Observations: map[string]UsageObservation{}}}
	annotateAnthropicWindowStats(info, 7*24*time.Hour, &WindowStats{Cost: 12}, at)
	require.Equal(t, "local_statistics", info.Anthropic.Observations["seven_day.window_stats"].Source)
	require.True(t, info.Anthropic.Observations["seven_day.window_stats"].SampledAt.Equal(at))
	require.Equal(t, 30.0, info.Anthropic.Observations["seven_day.estimated_total_cost"].Value)
	require.Equal(t, "estimate", info.Anthropic.Observations["seven_day.estimated_total_cost"].Source)
}
