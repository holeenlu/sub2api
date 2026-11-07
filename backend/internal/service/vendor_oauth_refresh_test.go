//go:build unit

package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type vendorRefreshRepoStub struct {
	*tokenRefreshAccountRepo
	allowed         bool
	saved, disabled int
}

type vendorRefreshRevokeGate struct{ repo *vendorRefreshRepoStub }

func (g vendorRefreshRevokeGate) acquire(context.Context) (func(), error) {
	g.repo.allowed = false
	return func() {}, nil
}

func (r *vendorRefreshRepoStub) ValidateVendorOAuthRefresh(_ context.Context, a *Account) error {
	current := r.accountsByID[a.ID]
	if !r.allowed || current == nil || !reflect.DeepEqual(current.Credentials, a.Credentials) ||
		current.GetExtraString("vendor_authorization_version") != a.GetExtraString("vendor_authorization_version") {
		return ErrVendorSupplyUnavailable
	}
	return nil
}

func (r *vendorRefreshRepoStub) UpdateVendorOAuthCredentialsIfUnchanged(ctx context.Context, a *Account, credentials map[string]any) (bool, error) {
	if r.ValidateVendorOAuthRefresh(ctx, a) != nil {
		return false, nil
	}
	r.accountsByID[a.ID].Credentials = shallowCopyMap(credentials)
	r.saved++
	return true, nil
}

func (r *vendorRefreshRepoStub) DisableVendorOAuthAuthorizationIfUnchanged(ctx context.Context, a *Account) (bool, error) {
	if r.ValidateVendorOAuthRefresh(ctx, a) != nil {
		return false, nil
	}
	current := r.accountsByID[a.ID]
	if a.IsExcelOAuth() {
		current.Extra["openai_excel_bps"] = false
	} else {
		current.Extra["vendor_native_disabled"] = true
	}
	r.disabled++
	return true, nil
}

func vendorRefreshFixture() (*Account, *vendorRefreshRepoStub) {
	owner := int64(7)
	a := &Account{ID: 1, VendorID: &owner, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "old-at", "refresh_token": "old-rt", "plan_type": "plus"},
		Extra:       map[string]any{"openai_excel_bps": true, "vendor_authorization_version": "v1"}}
	r := &vendorRefreshRepoStub{tokenRefreshAccountRepo: &tokenRefreshAccountRepo{
		mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{1: a}}, snapshotReads: true}, allowed: true}
	return a, r
}

func TestVendorOAuthRefreshDeniedBeforeExchange(t *testing.T) {
	for _, unified := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "unified"}[unified], func(t *testing.T) {
			a, repo := vendorRefreshFixture()
			repo.allowed = false
			svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
			executor := &refreshAPIExecutorStub{needsRefresh: true}
			refresher := &tokenRefresherStub{}
			if unified {
				svc.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil))
			}
			require.ErrorIs(t, svc.refreshWithRetry(t.Context(), a, refresher, executor, time.Minute), errRefreshSkipped)
			require.Zero(t, executor.refreshCalls)
			require.Zero(t, refresher.calls)
			require.Zero(t, repo.setErrorCalls)
			require.Zero(t, repo.setTempUnschedCalls)
		})
	}
	// Revoke during rate admission after the API's first permission check. The
	// concrete executor must recheck before reaching its nil upstream service.
	a, repo := vendorRefreshFixture()
	a.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
	svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
	svc.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil))
	refresher := &OpenAITokenRefresher{accountRepo: repo}
	err := svc.refreshWithRetryWithRateGate(t.Context(), a, refresher, refresher, time.Minute, vendorRefreshRevokeGate{repo})
	require.ErrorIs(t, err, errRefreshSkipped)
	require.False(t, repo.allowed)
	require.Zero(t, repo.disabled)
}

func TestVendorOAuthRefreshInFlightChangesCannotWriteOrDisable(t *testing.T) {
	for _, change := range []string{"revoked", "reauthorized", "mode_edited"} {
		for _, failure := range []bool{false, true} {
			name := change + map[bool]string{false: "/success", true: "/invalid_grant"}[failure]
			t.Run(name, func(t *testing.T) {
				a, repo := vendorRefreshFixture()
				svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
				svc.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil))
				executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "rotated-at", "refresh_token": "old-rt"}, onRefresh: func() {
					switch change {
					case "revoked":
						repo.allowed = false
					case "reauthorized":
						a.Credentials["access_token"] = "new-authorization-at"
					case "mode_edited":
						a.Extra["vendor_authorization_version"] = "v2"
					}
				}}
				if failure {
					executor.err = errors.New("invalid_grant")
				}
				require.ErrorIs(t, svc.refreshWithRetry(t.Context(), a, nil, executor, time.Minute), errRefreshSkipped)
				require.Equal(t, 1, executor.refreshCalls)
				require.Zero(t, repo.saved)
				require.Zero(t, repo.disabled)
				require.Zero(t, repo.setErrorCalls)
				require.Zero(t, repo.setTempUnschedCalls)
				require.Equal(t, StatusActive, a.Status)
			})
		}
	}
}

func TestVendorOAuthRefreshFailureIsModeScoped(t *testing.T) {
	for _, mode := range []string{"native", "bps"} {
		t.Run(mode, func(t *testing.T) {
			a, repo := vendorRefreshFixture()
			if mode == "bps" {
				a.Credentials["client_id"] = "app_fnr0pYvVwwFDocDumLG3H2Bp"
			}
			cache := &tokenCacheInvalidatorStub{}
			svc := NewTokenRefreshService(repo, nil, nil, nil, nil, cache, nil, &config.Config{}, nil)
			blocker := &tokenRefreshRuntimeBlocker{}
			svc.SetAccountRuntimeBlocker(blocker)
			svc.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil))
			executor := &refreshAPIExecutorStub{needsRefresh: true, err: errors.New("invalid_grant")}
			require.Error(t, svc.refreshWithRetry(t.Context(), a, nil, executor, time.Minute))
			require.Equal(t, 1, repo.disabled)
			require.Equal(t, 1, cache.calls)
			require.Zero(t, repo.setErrorCalls)
			require.Zero(t, repo.setTempUnschedCalls)
			require.Zero(t, blocker.blockCalls)
			require.Equal(t, StatusActive, a.Status)
			require.True(t, a.Schedulable)
			require.Equal(t, mode == "native", a.Extra["vendor_native_disabled"] == true)
			require.Equal(t, mode == "native", a.Extra["openai_excel_bps"])
			v := &Vendor{Platforms: []string{PlatformOpenAI}, OpenAIAuthorizationModes: []string{"native", "bps"}}
			state := vendorAuthorizationState(a, v, mode, a.Credentials)
			require.Equal(t, "disabled", state.Status, "existing authorization UI reports the failed mode as stopped")
			require.False(t, state.Enabled)
			if mode == "native" {
				require.Equal(t, "active", vendorAuthorizationState(a, v, "bps", map[string]any{"access_token": "independent-bps-at"}).Status)
			}
		})
	}
	a, repo := vendorRefreshFixture()
	svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
	err := svc.refreshWithRetry(t.Context(), a, &tokenRefresherStub{err: errors.New("upstream timeout")}, nil, time.Minute)
	require.Error(t, err)
	require.Zero(t, repo.disabled)
	require.Zero(t, repo.setTempUnschedCalls)
}

func TestVendorOAuthRefreshSuccessPreservesIndependentSchedulingState(t *testing.T) {
	a, repo := vendorRefreshFixture()
	until := time.Now().Add(time.Hour)
	a.TempUnschedulableUntil = &until
	scheduler := &tokenRefreshSchedulerCache{}
	svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, scheduler, &config.Config{}, nil)
	svc.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil))
	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: shallowCopyMap(a.Credentials), onRefresh: func() { a.Schedulable = false }}
	executor.credentials["access_token"] = "new-at"
	require.NoError(t, svc.refreshWithRetry(t.Context(), a, nil, executor, time.Minute))
	require.Equal(t, 1, repo.saved)
	require.Zero(t, repo.updateCredentialsCalls)
	require.Zero(t, repo.clearTempCalls)
	require.Equal(t, 2, repo.getByIDCalls, "one locked read and one durable cache read")
	require.Equal(t, "new-at", scheduler.lastAccount.GetCredential("access_token"))
	require.False(t, scheduler.lastAccount.Schedulable)
	require.Equal(t, &until, scheduler.lastAccount.TempUnschedulableUntil)
}
