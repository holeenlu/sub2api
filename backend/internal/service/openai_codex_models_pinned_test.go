//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type pinnedAccountRepoStub struct {
	AccountRepository
	members []Account
}

func (r *pinnedAccountRepoStub) ListByGroup(context.Context, int64) ([]Account, error) {
	return append([]Account(nil), r.members...), nil
}

func pinnedOpenAIAccount(id int64) Account {
	return Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
}

func TestMergeCodexModelsManifestBodiesUnionAndConfigOrder(t *testing.T) {
	first := `{"object":"codex.manifest","models":[{"slug":"model-a","display_name":"A from first"},{"slug":"model-b"}]}`
	second := `{"object":"codex.manifest","models":[{"slug":"model-a","display_name":"A from second"},{"slug":"model-c"}]}`

	merged, err := mergeCodexModelsManifestBodies([][]byte{[]byte(first), []byte(second)})
	require.NoError(t, err)
	require.Contains(t, string(merged), `"display_name":"A from first"`)
	require.NotContains(t, string(merged), `"display_name":"A from second"`)
	require.Contains(t, string(merged), `"slug":"model-c"`)
}

func TestRestrictCodexManifestToSourceKeepsGovernedDescriptors(t *testing.T) {
	governed := []byte(`{"models":[{"slug":"model-a","capability":"published"},{"slug":"model-b","capability":"published"}]}`)
	source := []byte(`{"models":[{"slug":"model-b","capability":"live"},{"slug":"model-c"}]}`)
	filtered, err := restrictCodexManifestToSource(governed, source)
	require.NoError(t, err)
	require.Contains(t, string(filtered), `"slug":"model-b"`)
	require.Contains(t, string(filtered), `"capability":"published"`)
	require.NotContains(t, string(filtered), `"slug":"model-a"`)
	require.NotContains(t, string(filtered), `"slug":"model-c"`)
}

func TestFetchPinnedOpenAIModelsIgnoresTransientAccountState(t *testing.T) {
	rateLimited := pinnedOpenAIAccount(1)
	now := time.Now()
	rateLimited.RateLimitedAt = &now
	rateLimited.RateLimitResetAt = func() *time.Time { value := now.Add(time.Hour); return &value }()
	unschedulable := pinnedOpenAIAccount(2)
	unschedulable.Schedulable = false
	expired := pinnedOpenAIAccount(3)
	expired.AutoPauseOnExpired = true
	expired.ExpiresAt = func() *time.Time { value := now.Add(-time.Minute); return &value }()
	repo := &pinnedAccountRepoStub{members: []Account{rateLimited, unschedulable, expired}}
	gateway := &OpenAIGatewayService{accountRepo: repo}
	group := &Group{ID: 9, Platform: PlatformOpenAI, CodexModelsManifestConfig: GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{2, 1, 3}}}
	var fetched []int64
	results, err := gateway.fetchPinnedOpenAIModels(context.Background(), group, func(_ context.Context, account *Account) (*OpenAIModelsResponse, error) {
		fetched = append(fetched, account.ID)
		return &OpenAIModelsResponse{Body: []byte(`{"models":[]}`)}, nil
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, []int64{1}, fetched)
}

func TestFetchPinnedOpenAIModelsReturnsLastErrorWhenAllFetchesFail(t *testing.T) {
	repo := &pinnedAccountRepoStub{members: []Account{pinnedOpenAIAccount(1), pinnedOpenAIAccount(2)}}
	gateway := &OpenAIGatewayService{accountRepo: repo}
	group := &Group{ID: 9, Platform: PlatformOpenAI, CodexModelsManifestConfig: GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{1, 2}}}
	want := errors.New("upstream down")
	_, err := gateway.fetchPinnedOpenAIModels(context.Background(), group, func(_ context.Context, _ *Account) (*OpenAIModelsResponse, error) {
		return nil, want
	})
	require.ErrorIs(t, err, want)
}
