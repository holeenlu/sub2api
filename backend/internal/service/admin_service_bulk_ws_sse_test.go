//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminServiceBulkUpdateAccounts_WSSSEAcceleration(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{
			{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
			{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_excel_bps": true}},
		}}
		svc := &adminServiceImpl{accountRepo: repo}
		result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
			AccountIDs: []int64{1, 2}, Extra: map[string]any{OpenAIOAuthWSSSEAccelerationKey: enabled},
		})
		require.NoError(t, err)
		require.Equal(t, 2, result.Success)
		// Only the transport flag is written; BPS settings are a separate choice.
		require.Equal(t, map[string]any{OpenAIOAuthWSSSEAccelerationKey: enabled}, repo.lastBulkUpdate.Extra)
	}
}

func TestAdminServiceBulkUpdateAccounts_RejectsInvalidWSSSEAcceleration(t *testing.T) {
	for _, raw := range []any{"true", nil, 1} {
		repo := &accountRepoStubForBulkUpdate{}
		svc := &adminServiceImpl{accountRepo: repo}
		result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
			AccountIDs: []int64{1}, Extra: map[string]any{OpenAIOAuthWSSSEAccelerationKey: raw},
		})
		require.Nil(t, result)
		requireApplicationErrorReason(t, err, "OPENAI_WS_SSE_ACCELERATION_INVALID")
		require.Zero(t, repo.bulkUpdateCalls)
	}
}

func TestAdminServiceBulkUpdateAccounts_RejectsIneligibleWSSSEAccelerationTargets(t *testing.T) {
	parentID := int64(99)
	for name, account := range map[string]*Account{
		"other platform":        {ID: 2, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		"API key":               {ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		"setup token":           {ID: 2, Platform: PlatformOpenAI, Type: AccountTypeSetupToken},
		"shadow":                {ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID},
		"agent identity":        {ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"auth_mode": OpenAIAuthModeAgentIdentity}},
		"personal access token": {ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"auth_mode": OpenAIAuthModePersonalAccessToken}},
	} {
		t.Run(name, func(t *testing.T) {
			require.False(t, account.supportsOpenAIOAuthWSSSEAcceleration())
			repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, account,
			}}
			svc := &adminServiceImpl{accountRepo: repo}
			result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
				AccountIDs: []int64{1, 2}, Extra: map[string]any{OpenAIOAuthWSSSEAccelerationKey: true},
			})
			require.Nil(t, result)
			requireApplicationErrorReason(t, err, "OPENAI_BULK_TARGET_INVALID")
			require.Zero(t, repo.bulkUpdateCalls, "eligible accounts must not be partially updated")
		})
	}
}
