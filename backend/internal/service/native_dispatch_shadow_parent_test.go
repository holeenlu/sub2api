package service

import (
	"net/http"
	"testing"
	"time"

	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/stretchr/testify/require"
)

// Credentials and key/account reads are existing isolated fixture repositories.
// Capacity is the original routing unlimited receipt, not a fake fingerprint.
func TestNativeShadowParentEligibilityAtConsume(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Account)
		deny   bool
	}{
		{"shared_credential_cooldown", func(a *Account) {
			until := time.Now().Add(time.Hour)
			a.TempUnschedulableUntil = &until
			a.TempUnschedulableReason = "shared credential/transport failure"
		}, true},
		{"parent_account_expired", func(a *Account) {
			at := time.Now().Add(-time.Minute)
			a.AutoPauseOnExpired, a.ExpiresAt = true, &at
		}, true},
		// These are intentionally independent from the shadow's spark lane.
		{"parent_manual_unschedulable", func(a *Account) { a.Schedulable = false }, false},
		{"parent_global_rate_limit", func(a *Account) {
			until := time.Now().Add(time.Hour)
			a.RateLimitResetAt, a.OverloadUntil = &until, &until
		}, false},
		{"parent_disabled", func(a *Account) { a.Status = StatusDisabled }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNativeOperationFixture(t, true, 3)
			parent := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Status: StatusActive, Schedulable: true,
				Credentials: map[string]any{"access_token": "review-parent-token", "expires_at": time.Now().Add(2 * time.Hour).Format(time.RFC3339Nano)}}
			f.accounts.accounts[parent.ID] = parent
			f.resource.ParentAccountID, f.resource.QuotaDimension, f.resource.Type = &parent.ID, "spark", AccountTypeOAuth
			f.resource.Credentials = nil
			f.op.authority.tokens = NewOpenAITokenProvider(f.accounts, nil, nil)
			f.service.openAITokenProvider = f.op.authority.tokens.(*OpenAITokenProvider)
			require.True(t, parentHealthyForShadow(f.resource, func(int64) *Account { return parent }))
			prepared, target, err := f.op.prepare(f.request(t), f.resource, "", false)
			require.NoError(t, err)
			defer prepared.Close()
			require.Equal(t, f.resource.ID, prepared.Identity().ResourceID)
			require.Equal(t, parent.ID, prepared.Identity().Owner.AccountID)
			permit, err := f.op.dispatcher.Admit(f.ctx, prepared.Identity(), target)
			require.NoError(t, err)
			tc.change(parent) // only authoritative parent state changes after Admit
			require.Equal(t, !tc.deny, parentHealthyForShadow(f.resource, func(int64) *Account { return parent }), "existing scheduler contract")
			result, err := native.Open(f.ctx, prepared, permit)
			if result != nil {
				defer result.Close()
			}
			t.Logf("deny=%v actual_error=%v physical_port_calls=%d", tc.deny, err, f.transport.sends.Load())
			if tc.deny {
				require.Error(t, err, "the real Consume boundary must enforce the existing parent eligibility contract")
				require.Zero(t, f.transport.sends.Load())
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 1, f.transport.sends.Load())
			}
			require.Equal(t, 1, f.op.budget.attemptBudget.Snapshot().Attempts)
			require.Zero(t, f.transport.legacy.Load())
		})
	}
}

func TestNativeShadowParentAccountExpiryBoundsHTTPPermit(t *testing.T) {
	f := newNativeAuthorityFixture(t)
	expires := time.Now().Add(10 * time.Minute)
	parent := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		AutoPauseOnExpired: true, ExpiresAt: &expires,
		Credentials: map[string]any{"access_token": "parent-token", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339Nano)}}
	f.accounts.accounts[parent.ID] = parent
	f.resource.ParentAccountID, f.resource.QuotaDimension, f.resource.Type = &parent.ID, "spark", AccountTypeOAuth
	f.resource.Credentials = nil
	f.gate.tokens = NewOpenAITokenProvider(f.accounts, nil, nil)
	id := f.identity(t)
	validation, err := f.gate.RevalidateDispatch(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, expires, validation.ValidUntil, "HTTP permit must not outlive the parent account")
}

func TestNativeShadowParentHeldWSUsesCredentialEligibility(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		f := newNativeAuthorityFixture(t)
		parent := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
			Credentials: map[string]any{"access_token": "parent-token", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339Nano)}}
		f.accounts.accounts[parent.ID] = parent
		f.resource.ParentAccountID, f.resource.QuotaDimension, f.resource.Type = &parent.ID, "spark", AccountTypeOAuth
		f.resource.Credentials = nil
		f.gate.tokens = NewOpenAITokenProvider(f.accounts, nil, nil)
		id := f.identity(t)
		id.Transport = "ws"
		headers := http.Header{"Authorization": {"Bearer parent-token"}}
		h, err := nativeWSHandshakeSnapshot(f.resource, parent, "wss://chatgpt.com/backend-api/codex/responses", "", headers)
		require.NoError(t, err)
		h.succeeded = true
		id.GrantDigest = h.grant
		_, err = f.gate.revalidateWS(t.Context(), id, h, func() bool { return true }, nil)
		require.NoError(t, err)
		until := time.Now().Add(time.Hour)
		if blocked {
			parent.TempUnschedulableUntil = &until
		} else {
			parent.Schedulable = false
			parent.RateLimitResetAt, parent.OverloadUntil = &until, &until
		}
		_, err = f.gate.revalidateWS(t.Context(), id, h, func() bool { return true }, nil)
		if blocked {
			require.ErrorIs(t, err, ErrNativeDispatchScopeChanged)
			require.False(t, IsNativeDispatchPrincipalFailure(err))
		} else {
			require.NoError(t, err, "held shadow socket keeps its independent global quota lane")
		}
	}
}
