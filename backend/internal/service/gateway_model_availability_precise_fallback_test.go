//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDiagnoseModelAvailabilityPreciseFallback(t *testing.T) {
	for _, tt := range []struct {
		name          string
		fallback      bool
		fallbackFable bool
		missingGroup  int64
		wantPrecise   bool
	}{
		{name: "single Fable pool", wantPrecise: true},
		{name: "ordinary fallback", fallback: true},
		{name: "Fable fallback", fallback: true, fallbackFable: true, wantPrecise: true},
		{name: "unreadable fallback", fallback: true, missingGroup: 2},
		{name: "unreadable origin", fallback: true, missingGroup: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			origin, fallback := int64(1), int64(2)
			early, late := time.Now().Add(30*time.Second), time.Now().Add(48*time.Hour)
			model := "claude-fable-5-1"
			account := func(id, group int64, reset time.Time, fable bool) Account {
				acc := Account{
					ID: id, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: true,
					AccountGroups: []AccountGroup{{GroupID: group}},
					Credentials:   map[string]any{"model_mapping": map[string]any{model: model}},
				}
				if fable {
					acc.Extra = map[string]any{modelRateLimitsKey: map[string]any{
						anthropicFableRateLimitKey: map[string]any{
							"rate_limit_reset_at": reset.UTC().Format(time.RFC3339),
							"reason":              AnthropicFableWindowExhaustedReason,
						},
					}}
				} else {
					acc.RateLimitResetAt = &reset
				}
				return acc
			}
			repo := &mockAccountRepoForPlatform{accounts: []Account{account(10, origin, late, true)}}
			groups := map[int64]*Group{origin: noAccountFallbackGroup(origin, PlatformAnthropic, nil)}
			if tt.fallback {
				groups[origin].FallbackGroupIDOnNoAccount = &fallback
				groups[fallback] = noAccountFallbackGroup(fallback, PlatformAnthropic, nil)
				repo.accounts = append(repo.accounts, account(20, fallback, early, tt.fallbackFable))
			}
			delete(groups, tt.missingGroup)
			svc := &GatewayService{accountRepo: repo, cfg: testConfig(), groupRepo: &mockGroupRepoForGateway{groups: groups}}
			diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &origin, model, PlatformAnthropic)
			require.True(t, diag.AllModelCapableRateLimited)
			require.NotNil(t, diag.EarliestRateLimitResetAt)
			wantReset := late
			if tt.fallback && tt.missingGroup == 0 {
				wantReset = early
			}
			require.WithinDuration(t, wantReset, *diag.EarliestRateLimitResetAt, time.Second)
			if tt.wantPrecise {
				require.NotNil(t, diag.RateLimit)
				require.Equal(t, AnthropicFableWindowExhaustedReason, diag.RateLimit.Reason)
				require.WithinDuration(t, wantReset, *diag.RateLimit.ResetAt, time.Second)
			} else {
				require.Nil(t, diag.RateLimit, "an ordinary or unreadable fallback must not inherit the origin's precise Fable attribution")
			}
		})
	}
}
