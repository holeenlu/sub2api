package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type cloudAuthorizationRepoFixture struct {
	AccountRepository
	account      *Account
	binding      *OpenAICloudAuthorizationBinding
	writes       int
	loseFirstCAS bool
}

func (r *cloudAuthorizationRepoFixture) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *cloudAuthorizationRepoFixture) GetCloudAuthorization(context.Context, int64) (*OpenAICloudAuthorizationBinding, error) {
	if r.binding == nil {
		return nil, nil
	}
	b := *r.binding
	return &b, nil
}
func (r *cloudAuthorizationRepoFixture) StoreCloudAuthorization(_ context.Context, _ *Account, previous string, next *OpenAICloudAuthorizationBinding) (bool, error) {
	if r.binding != nil && (r.binding.Generation != previous || r.binding.Revision != next.Revision) {
		return false, nil
	}
	r.writes++
	b := *next
	b.Revision++
	r.binding = &b
	if b.Revoked {
		if r.account.Extra == nil {
			r.account.Extra = map[string]any{}
		}
		r.account.Extra[OpenAICloudEnabledKey] = false
	}
	if r.loseFirstCAS {
		r.loseFirstCAS = false
		r.binding.Generation = "concurrent-winner"
		return false, nil
	}
	return true, nil
}

func TestCloudNativeAuthorizationCreatesBindingWithoutLogin(t *testing.T) {
	for _, scenario := range []string{"first use", "old revoked binding", "concurrent first use", "foreign identity", "Excel token"} {
		t.Run(scenario, func(t *testing.T) {
			f := newVendorOpenAIIdentityFixture(t)
			at, id := f.claims(true), f.claims(false)
			at["client_id"] = openai.ClientID
			if scenario == "Excel token" {
				at = f.claims(true)
			}
			token := f.tokens(t, at, id).AccessToken
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"access_token": token, "refresh_token": "native-refresh", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339), "chatgpt_account_id": "workspace-signed", "chatgpt_user_id": "user-signed"},
				Extra:       map[string]any{OpenAICloudEnabledKey: true, "openai_excel_bps": true}}
			repo := &cloudAuthorizationRepoFixture{account: account}
			if scenario == "old revoked binding" {
				repo.binding = &OpenAICloudAuthorizationBinding{Generation: "old", Revision: 4, Revoked: true}
			}
			if scenario == "concurrent first use" {
				repo.loseFirstCAS = true
			}
			if scenario == "foreign identity" {
				account.Credentials["chatgpt_user_id"] = "another-user"
			}
			svc := NewOpenAICloudService(repo, NewOpenAITokenProvider(repo, nil, nil), nil)
			svc.keys = f.keys
			// Seed the local signing-key fixture; no OAuth exchange or external call.
			_, verifyErr := svc.verifyAccess(t.Context(), token, "http://fixture-proxy.invalid:8080")
			original := cloudLiveClone(account.Credentials)
			access, err := svc.GetAccessToken(t.Context(), account.ID)
			if scenario == "foreign identity" || scenario == "Excel token" {
				require.ErrorIs(t, err, ErrOpenAICloudAuthorization)
				require.Nil(t, access)
				require.Zero(t, repo.writes)
				return
			}
			require.NoError(t, verifyErr)
			require.NoError(t, err)
			require.Equal(t, token, access.AccessToken)
			require.Equal(t, original, account.Credentials)
			require.True(t, account.IsOpenAICloudEnabled())
			require.Equal(t, true, account.Extra["openai_excel_bps"])
			require.Equal(t, "provider-subject", repo.binding.Subject)
			require.Equal(t, 1, repo.writes)
			if scenario == "concurrent first use" {
				require.Equal(t, "concurrent-winner", access.Generation)
			}
			again, err := svc.GetAccessToken(t.Context(), account.ID)
			require.NoError(t, err)
			require.Equal(t, access.Generation, again.Generation)
			require.Equal(t, 1, repo.writes)
			raw, err := json.Marshal(repo.binding)
			require.NoError(t, err)
			require.NotContains(t, string(raw), token)
			require.NotContains(t, string(raw), "native-refresh")
		})
	}
}
