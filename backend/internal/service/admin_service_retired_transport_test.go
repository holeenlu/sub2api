//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminBulkUpdateIgnoresRetiredWSAcceleration(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
	}}
	svc := &adminServiceImpl{accountRepo: repo}
	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1, 2}, Extra: map[string]any{
			"openai_oauth_ws_sse_acceleration": true,
			"unrelated":                        "preserve",
		},
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.Success)
	require.Equal(t, map[string]any{"unrelated": "preserve"}, repo.lastBulkUpdate.Extra)
}
