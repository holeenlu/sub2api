package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountCostMultiplierDefaultsAndValidation(t *testing.T) {
	var missing *Account
	require.Equal(t, 0.1, missing.CostMultiplier())
	billing := 7.0
	a := &Account{RateMultiplier: &billing}
	require.Equal(t, 0.1, a.CostMultiplier())
	for _, value := range []any{0.0, 0, 0.25, json.Number("0.25"), int64(2), float32(0.5)} {
		a.Extra = map[string]any{AccountCostMultiplierExtraKey: value}
		require.NoError(t, ValidateAccountCostMultiplierExtra(a.Extra))
		require.Equal(t, a.Extra[AccountCostMultiplierExtraKey], a.CostMultiplier())
		require.Equal(t, 7.0, a.BillingRateMultiplier())
	}
	for _, value := range []any{-0.1, math.NaN(), math.Inf(1), 1000001.0, "0.1", true, json.Number("invalid")} {
		a.Extra = map[string]any{AccountCostMultiplierExtraKey: value}
		require.Error(t, ValidateAccountCostMultiplierExtra(a.Extra))
		require.Equal(t, 0.1, a.CostMultiplier(), "malformed legacy metadata must not poison scores")
	}
	a.Extra = map[string]any{AccountCostMultiplierExtraKey: nil}
	require.NoError(t, ValidateAccountCostMultiplierExtra(a.Extra))
	require.Equal(t, 0.1, a.CostMultiplier())
}

func TestAccountCostMultiplierRejectsInvalidWritesBeforeRepositoryAccess(t *testing.T) {
	s := &adminServiceImpl{}
	ctx := context.Background()
	extra := map[string]any{AccountCostMultiplierExtraKey: -1.0}
	_, err := s.CreateAccount(ctx, &CreateAccountInput{Extra: extra})
	require.ErrorContains(t, err, "cost_multiplier")
	_, err = s.UpdateAccount(ctx, 1, &UpdateAccountInput{Extra: extra})
	require.ErrorContains(t, err, "cost_multiplier")
	_, err = s.BulkUpdateAccounts(ctx, &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Extra: extra})
	require.ErrorContains(t, err, "cost_multiplier")
	require.ErrorContains(t, s.UpdateAccountExtra(ctx, 1, extra), "cost_multiplier")
}

func TestUpstreamProbeCostMultiplierToSync(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name   string
		status string
		rate   any
		want   float64
		valid  bool
	}{
		{"success", UpstreamBillingProbeStatusOK, 0.14, 0.14, true},
		{"zero", UpstreamBillingProbeStatusOK, 0.0, 0, true},
		{"failed with cached data", UpstreamBillingProbeStatusFailed, 0.14, 0, false},
		{"unsupported with cached data", UpstreamBillingProbeStatusUnsupported, 0.14, 0, false},
		{"negative", UpstreamBillingProbeStatusOK, -1.0, 0, false},
		{"out of range", UpstreamBillingProbeStatusOK, 1000001.0, 1000001, false},
		{"missing", UpstreamBillingProbeStatusOK, nil, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := &UpstreamBillingProbeSnapshot{Status: tt.status, LastAttemptAt: now, Data: map[string]any{
				"billing_scope": "token", "resolved_rate_multiplier": tt.rate, "peak_rate_enabled": false,
			}}
			value, ok := snapshot.CostMultiplierToSync()
			require.Equal(t, tt.valid, ok)
			if ok {
				require.Equal(t, tt.want, value)
			}
		})
	}
	snapshot := &UpstreamBillingProbeSnapshot{Status: UpstreamBillingProbeStatusOK, LastAttemptAt: now, Data: map[string]any{
		"billing_scope": "token", "resolved_rate_multiplier": 0.14, "peak_rate_enabled": true,
		"peak_start": "09:00", "peak_end": "18:00", "peak_rate_multiplier": 2.0, "timezone": "UTC",
	}}
	value, ok := snapshot.CostMultiplierToSync()
	require.True(t, ok)
	require.InDelta(t, 0.28, value, 1e-9)
	billing := 7.0
	account := &Account{RateMultiplier: &billing, Extra: map[string]any{AccountCostMultiplierExtraKey: value, UpstreamBillingProbeExtraKey: snapshot}}
	require.InDelta(t, 0.28, account.CostMultiplier(), 1e-9)
	require.Equal(t, billing, account.BillingRateMultiplier())
}

func TestAccountCostAutoSyncSetting(t *testing.T) {
	for _, value := range []any{nil, true, false} {
		extra := map[string]any{AccountCostAutoSyncExtraKey: value}
		require.NoError(t, ValidateAccountCostMultiplierExtra(extra))
		account := &Account{Extra: extra}
		require.Equal(t, value == true, account.CostMultiplierAutoSyncEnabled())
	}
	for _, value := range []any{"false", 0, 1.0, map[string]any{}} {
		extra := map[string]any{AccountCostAutoSyncExtraKey: value}
		require.ErrorContains(t, ValidateAccountCostMultiplierExtra(extra), "cost_multiplier_auto_sync")
		s := &adminServiceImpl{}
		_, err := s.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Extra: extra})
		require.ErrorContains(t, err, "cost_multiplier_auto_sync")
	}
	require.False(t, (&Account{}).CostMultiplierAutoSyncEnabled())
}
