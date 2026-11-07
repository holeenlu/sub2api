package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
)

func newNativePATFixture(t *testing.T, classification string) *nativeAuthorityFixture {
	t.Helper()
	f := newNativeAuthorityFixture(t)
	f.resource.Type = AccountTypeOAuth
	f.resource.Credentials = map[string]any{"access_token": "actual-pat-provider-token", "chatgpt_account_id": "durable-pat-owner"}
	if classification == openAIAuthModeLegacyCredentialKey {
		f.resource.Credentials[classification] = "personal_access_token"
	} else {
		f.resource.Credentials[classification] = OpenAIAuthModePersonalAccessToken
	}
	f.gate.tokens = NewOpenAITokenProvider(f.accounts, nil, nil)
	return f
}

func TestNativeDispatchAuthorityPATWithoutExpiryUsesDurableClassification(t *testing.T) {
	for _, classification := range []string{openAIAuthModeCredentialKey, openAIAuthModeLegacyCredentialKey} {
		t.Run(classification, func(t *testing.T) {
			f := newNativePATFixture(t, classification)
			user, err := routing.AcquireCapacity(t.Context(), nil, routing.CapacityRequest{ID: f.key.User.ID, User: true, Limit: 0}, nil)
			require.NoError(t, err)
			t.Cleanup(user.ReleaseFunc)
			account, err := routing.AcquireCapacity(t.Context(), nil, routing.CapacityRequest{ID: f.resource.ID, Limit: 0}, nil)
			require.NoError(t, err)
			t.Cleanup(account.ReleaseFunc)
			f.gate.leases = &nativeRoutingEvidenceVerifier{user: user, account: account, currentUser: f.key.User, currentAccount: f.resource}
			credential, err := f.gate.ResolveCredential(t.Context(), f.resource.ID)
			require.NoError(t, err)
			token, kind := credential.Bearer()
			require.Equal(t, "actual-pat-provider-token", token)
			require.Equal(t, "oauth", kind)
			require.True(t, credential.ValidUntil.IsZero(), "PAT must not acquire a fabricated TTL")
			identity := f.identity(t)
			proof, err := f.gate.RevalidateDispatch(t.Context(), identity)
			require.NoError(t, err)
			require.True(t, proof.ValidUntil.IsZero())
			dispatcher, budget := f.dispatcher(t)
			permit, err := dispatcher.Admit(t.Context(), identity, model.AttemptTarget{Owner: identity.Owner, Limit: 2})
			require.NoError(t, err)
			require.NoError(t, permit.Consume(t.Context(), identity))
			require.Equal(t, 1, budget.Snapshot().Attempts)
		})
	}
}

func TestNativeDispatchAuthorityPATClassificationOrTokenChangeRevokesPermit(t *testing.T) {
	for _, change := range []string{"classification removed", "classification representation changed", "token replaced", "physical owner transferred"} {
		t.Run(change, func(t *testing.T) {
			f := newNativePATFixture(t, openAIAuthModeCredentialKey)
			identity := f.identity(t)
			dispatcher, _ := f.dispatcher(t)
			permit, err := dispatcher.Admit(t.Context(), identity, model.AttemptTarget{Owner: identity.Owner, Limit: 2})
			require.NoError(t, err)
			switch change {
			case "classification removed":
				delete(f.resource.Credentials, openAIAuthModeCredentialKey)
			case "classification representation changed":
				delete(f.resource.Credentials, openAIAuthModeCredentialKey)
				f.resource.Credentials[openAIAuthModeLegacyCredentialKey] = "personal_access_token"
			case "token replaced":
				f.resource.Credentials["access_token"] = "replacement-pat-token"
			case "physical owner transferred":
				vendor := int64(17)
				f.resource.VendorID = &vendor
			}
			err = permit.Consume(t.Context(), identity)
			require.ErrorIs(t, err, ErrNativeDispatchDenied)
			if change == "physical owner transferred" {
				require.ErrorIs(t, err, ErrNativeDispatchScopeChanged)
			} else {
				require.ErrorIs(t, err, ErrNativeDispatchGrantUnproven)
			}
			require.ErrorIs(t, permit.Consume(t.Context(), identity), execution.ErrSendPermitConsumed)
		})
	}
}

func TestNativeDispatchAuthorityPATClassificationRecheckedAfterProvider(t *testing.T) {
	f := newNativePATFixture(t, openAIAuthModeCredentialKey)
	f.gate.tokens = nativeAuthorityTokenFunc(func(_ context.Context, owner *Account) (string, error) {
		token := owner.GetOpenAIAccessToken()
		// Replace the map so the provider's input still carries its old PAT snapshot.
		f.resource.Credentials = map[string]any{"access_token": token}
		require.True(t, owner.IsOpenAIPersonalAccessToken())
		return token, nil
	})
	_, err := f.gate.ResolveCredential(t.Context(), f.resource.ID)
	require.ErrorIs(t, err, ErrNativeDispatchGrantUnproven)
}

func TestNativeDispatchAuthorityPATActualExpiryAndBearerMustMatch(t *testing.T) {
	future := time.Now().Add(2 * time.Hour)
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{name: "explicit future expiry", value: future.Format(time.RFC3339Nano), valid: true},
		{name: "explicit expired", value: time.Now().Add(-time.Hour).Format(time.RFC3339Nano)},
		{name: "explicit zero date", value: time.Time{}.Format(time.RFC3339)},
		{name: "malformed expiry", value: "not-a-time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNativePATFixture(t, openAIAuthModeCredentialKey)
			f.resource.Credentials["expires_at"] = tc.value
			grant, err := f.gate.ResolveCredential(t.Context(), f.resource.ID)
			if tc.valid {
				require.NoError(t, err)
				require.True(t, grant.ValidUntil.Equal(future))
			} else {
				require.ErrorIs(t, err, ErrNativeDispatchDenied)
			}
		})
	}
	t.Run("stale cached PAT", func(t *testing.T) {
		f := newNativePATFixture(t, openAIAuthModeCredentialKey)
		f.gate.tokens = NewOpenAITokenProvider(f.accounts, &nativeAuthorityCache{token: "stale-cached-pat"}, nil)
		_, err := f.gate.ResolveCredential(t.Context(), f.resource.ID)
		require.ErrorIs(t, err, ErrNativeDispatchGrantUnproven)
	})
}
