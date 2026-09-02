//go:build unit

package admin

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/stretchr/testify/require"
)

func TestUnionSyncedModelIDsPreservesProviderOrder(t *testing.T) {
	models := unionSyncedModelIDs([][]string{
		{"claude-fable-5", "claude-sonnet-5"},
		{"claude-sonnet-5", "claude-opus-5"},
	})

	require.Equal(t, []string{
		"claude-fable-5",
		"claude-sonnet-5",
		"claude-opus-5",
	}, models)
}

func TestIntersectSyncedModelIDsRemovesModelsMissingFromAnyAccount(t *testing.T) {
	models := intersectSyncedModelIDs([][]string{
		{"claude-fable-5", "claude-haiku-4-5", "claude-sonnet-5"},
		{"claude-fable-5", "claude-sonnet-5"},
	})

	require.Equal(t, []string{
		"claude-fable-5",
		"claude-sonnet-5",
	}, models)
}

// 只有 active 的 oauth/setup-token/apikey 账号持有可用于 /v1/models 的凭据，
// 其余形态发出去也只会攒下失败明细。
func TestAnthropicModelSyncEligibleSelectsCredentialBearingAccounts(t *testing.T) {
	cases := []struct {
		name    string
		account *service.Account
		want    bool
	}{
		{"nil", nil, false},
		{"oauth", &service.Account{Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive}, true},
		{"setup token", &service.Account{Platform: service.PlatformAnthropic, Type: service.AccountTypeSetupToken, Status: service.StatusActive}, true},
		{"api key", &service.Account{Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Status: service.StatusActive}, true},
		{"inactive", &service.Account{Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: "inactive"}, false},
		{"other platform", &service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive}, false},
		{"other type", &service.Account{Platform: service.PlatformAnthropic, Type: "cookie", Status: service.StatusActive}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, anthropicModelSyncEligible(tc.account))
		})
	}
}

func TestFetchAnthropicModelsFromAccountsRejectsUnconfiguredService(t *testing.T) {
	_, err := fetchAnthropicModelsFromAccounts(
		context.Background(), nil, nil, anthropicModelAggregationUnion, false)

	require.ErrorIs(t, err, errAnthropicModelSyncUnavailable)
}

// 全部账号都不合格时不该发出任何上游请求——nil 的 AccountTestService 在这里正是
// 「一旦发请求就会 panic」的探针。
func TestFetchAnthropicModelsFromAccountsSkipsIneligibleAccountsBeforeDialing(t *testing.T) {
	accounts := []*service.Account{
		{ID: 1, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: "inactive"},
		{ID: 2, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive},
	}

	_, err := fetchAnthropicModelsFromAccounts(
		context.Background(), &service.AccountTestService{}, accounts, anthropicModelAggregationUnion, false)

	require.ErrorIs(t, err, errAnthropicModelSyncNoAccounts)
}

func TestAnthropicModelCacheHonoursTTL(t *testing.T) {
	now := time.Now()
	account := &service.Account{ID: 42, Credentials: map[string]any{"api_key": "k"}}
	key := anthropicModelCacheKey(account)

	_, ok := lookupAnthropicModelCache(key, now)
	require.False(t, ok)

	storeAnthropicModelCache(key, []string{"claude-sonnet-5"}, now)
	models, ok := lookupAnthropicModelCache(key, now.Add(time.Minute))
	require.True(t, ok)
	require.Equal(t, []string{"claude-sonnet-5"}, models)

	_, ok = lookupAnthropicModelCache(key, now.Add(anthropicModelSyncCacheTTL+time.Second))
	require.False(t, ok)
}

// 被动采样在每个成功的 Anthropic 响应之后推进 updated_at，所以缓存键不能带上它：
// 带了的话有流量的账号永远命中不到，每次打开编辑弹窗都要重新扇出一轮上游请求。
func TestAnthropicModelCacheKeyIgnoresUpdatedAt(t *testing.T) {
	now := time.Now()
	account := &service.Account{ID: 43, UpdatedAt: now, Credentials: map[string]any{"api_key": "k"}}
	key := anthropicModelCacheKey(account)
	storeAnthropicModelCache(key, []string{"claude-sonnet-5"}, now)

	account.UpdatedAt = now.Add(time.Hour)
	models, ok := lookupAnthropicModelCache(anthropicModelCacheKey(account), now.Add(time.Minute))

	require.True(t, ok)
	require.Equal(t, []string{"claude-sonnet-5"}, models)
}

// 重授权、换 key、换 base_url 仍要让缓存失效——这些才是决定上游返回什么的输入。
func TestAnthropicModelCacheKeyTracksCredentials(t *testing.T) {
	base := &service.Account{ID: 44, Credentials: map[string]any{
		"api_key":  "old",
		"base_url": "https://a.example/v1",
	}}
	key := anthropicModelCacheKey(base)

	rotated := &service.Account{ID: 44, Credentials: map[string]any{
		"api_key":  "new",
		"base_url": "https://a.example/v1",
	}}
	require.NotEqual(t, key, anthropicModelCacheKey(rotated))

	moved := &service.Account{ID: 44, Credentials: map[string]any{
		"api_key":  "old",
		"base_url": "https://b.example/v1",
	}}
	require.NotEqual(t, key, anthropicModelCacheKey(moved))

	// 不同账号即便凭据相同也各自成键。
	other := &service.Account{ID: 45, Credentials: base.Credentials}
	require.NotEqual(t, key, anthropicModelCacheKey(other))
}

func TestAnthropicModelSyncFailureMessageHidesUpstreamInternals(t *testing.T) {
	require.Equal(t, "Timed out while fetching /v1/models",
		anthropicModelSyncFailureMessage(context.DeadlineExceeded))
	require.Equal(t, "Failed to fetch /v1/models",
		anthropicModelSyncFailureMessage(errAnthropicModelSyncAllFailed))
	require.Equal(t, "Upstream rejected the request", anthropicModelSyncFailureMessage(
		&service.UpstreamModelSyncError{
			Kind:    service.UpstreamModelSyncErrorUpstream,
			Message: "Upstream rejected the request",
			Err:     errAnthropicModelSyncAllFailed,
		}))
}
