package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type cloudOwnerValidationRepo struct {
	*cloudAuthorizationRepoFixture
	accountError error
	grantError   error
}

func (r *cloudOwnerValidationRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	if r.accountError != nil {
		return nil, r.accountError
	}
	return r.cloudAuthorizationRepoFixture.GetByID(ctx, id)
}
func (r *cloudOwnerValidationRepo) GetCloudAuthorization(ctx context.Context, id int64) (*OpenAICloudAuthorizationBinding, error) {
	if r.grantError != nil {
		return nil, r.grantError
	}
	return r.cloudAuthorizationRepoFixture.GetCloudAuthorization(ctx, id)
}
func cloudOwnerValidationFixture() (*OpenAIGatewayService, *cloudOwnerValidationRepo, OpenAICloudOwner) {
	account := &Account{ID: 40, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"chatgpt_account_id": "workspace", "chatgpt_user_id": "user"},
		Extra:       map[string]any{OpenAICloudEnabledKey: true}}
	repo := &cloudOwnerValidationRepo{cloudAuthorizationRepoFixture: &cloudAuthorizationRepoFixture{
		account: account, binding: &OpenAICloudAuthorizationBinding{Generation: "grant", AccountID: "workspace", UserID: "user", Subject: "subject"}}}
	// Deliberately no TokenProvider: an already authenticated socket must not
	// acquire/refresh/reverify credentials merely to renew its durable owner.
	cloud := NewOpenAICloudService(repo, nil, nil)
	svc := &OpenAIGatewayService{cloudService: cloud, cache: &cloudForwardCache{GatewayCache: &stubGatewayCache{}, cloudLiveRoutingStore: newCloudLiveRoutingStore()}}
	owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, AccountID: 40, GrantGeneration: "grant", Model: "gpt-5.1", SettingsDigest: cloudSettingsDigest(account)}
	return svc, repo, owner
}

func TestCloudOwnerValidationDoesNotReacquireCredentials(t *testing.T) {
	svc, repo, owner := cloudOwnerValidationFixture()
	require.NoError(t, svc.validateCloudSessionOwner(t.Context(), owner))
	repo.account.Credentials["access_token"] = "rotated-but-not-used-by-the-established-socket"
	repo.account.Credentials["refresh_token"] = "rotated-refresh"
	repo.account.Credentials["expires_at"] = "2000-01-01T00:00:00Z"
	require.NoError(t, svc.validateCloudSessionOwner(t.Context(), owner), "token lifetime does not change a durable execution identity")
	require.Zero(t, repo.writes, "renewal cannot mint a new grant or revive a revoked one")
	rpc, err := svc.connectCloudSession(t.Context(), owner)
	require.ErrorIs(t, err, ErrOpenAICloudAuthorization, "a new connection still requires the complete credential path")
	require.Nil(t, rpc)
}

func TestCloudOwnerValidationRejectsProvenIdentityOrSettingsChange(t *testing.T) {
	cases := map[string]func(*cloudOwnerValidationRepo){
		"revoked":       func(r *cloudOwnerValidationRepo) { r.binding.Revoked = true },
		"inactive":      func(r *cloudOwnerValidationRepo) { r.account.Status = "inactive" },
		"unschedulable": func(r *cloudOwnerValidationRepo) { r.account.Schedulable = false },
		"expired": func(r *cloudOwnerValidationRepo) {
			at := time.Now().Add(-time.Second)
			r.account.AutoPauseOnExpired, r.account.ExpiresAt = true, &at
		},
		"generation":      func(r *cloudOwnerValidationRepo) { r.binding.Generation = "replacement" },
		"missing grant":   func(r *cloudOwnerValidationRepo) { r.binding = nil },
		"missing subject": func(r *cloudOwnerValidationRepo) { r.binding.Subject = "" },
		"workspace":       func(r *cloudOwnerValidationRepo) { r.account.Credentials["chatgpt_account_id"] = "replacement" },
		"user":            func(r *cloudOwnerValidationRepo) { r.account.Credentials["chatgpt_user_id"] = "replacement" },
		"disabled":        func(r *cloudOwnerValidationRepo) { r.account.Extra[OpenAICloudEnabledKey] = false },
		"ineligible":      func(r *cloudOwnerValidationRepo) { r.account.Type = AccountTypeAPIKey },
		"model mapping": func(r *cloudOwnerValidationRepo) {
			r.account.Credentials["model_mapping"] = map[string]any{"gpt-5.1": "gpt-6.1-sol"}
		},
		"proxy":   func(r *cloudOwnerValidationRepo) { id := int64(7); r.account.ProxyID = &id },
		"deleted": func(r *cloudOwnerValidationRepo) { r.accountError = ErrAccountNotFound },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			svc, repo, owner := cloudOwnerValidationFixture()
			change(repo)
			err := svc.validateCloudSessionOwner(t.Context(), owner)
			require.ErrorIs(t, err, errCloudAuthorizationChanged)
			require.Zero(t, repo.writes)
		})
	}
}

func TestCloudOwnerUnavailableIsNotAccountFailover(t *testing.T) {
	err := cloudOwnerValidationFailure(errors.New("private database detail"))
	require.ErrorIs(t, err, errCloudStateUnavailable)
	require.NotContains(t, err.Error(), "private")
	require.Nil(t, cloudPreInferenceFailover(t.Context(), []byte("{}"), 503, "cloud_state_unavailable", err.Error(), err, true))
	require.ErrorIs(t, cloudOwnerValidationFailure(errCloudAuthorizationChanged), errCloudAuthorizationChanged)
}

func TestCloudOwnerValidationDependencyFailureIsNotRevocation(t *testing.T) {
	for _, source := range []string{"account", "grant"} {
		t.Run(source, func(t *testing.T) {
			svc, repo, owner := cloudOwnerValidationFixture()
			unavailable := errors.New("database unavailable: must-not-leak")
			if source == "account" {
				repo.accountError = unavailable
			} else {
				repo.grantError = unavailable
			}
			err := svc.validateCloudSessionOwner(t.Context(), owner)
			require.Error(t, err)
			require.NotErrorIs(t, err, errCloudAuthorizationChanged)
			status, code, message, known := cloudPublicError(err)
			require.True(t, known)
			require.Equal(t, http.StatusServiceUnavailable, status)
			require.Equal(t, "cloud_state_unavailable", code)
			require.NotContains(t, message, "must-not-leak")
			require.Zero(t, repo.writes)
		})
	}
}
