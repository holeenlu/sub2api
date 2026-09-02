//go:build unit

package service

// 无可用账号兜底分组由调度层在选号失败时从认证快照读取。快照 build →
// L2 JSON 往返 → 还原这条链上任何一处漏了该字段，兜底都会静默失效——
// 请求照常返回 503，没有任何报错指向配置丢失。这里锁住全链路保真。

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotNoAccountFallbackRoundtrip(t *testing.T) {
	svc := &APIKeyService{}

	groupID := int64(50)
	fallbackGroupID := int64(51)
	apiKey := &APIKey{
		ID:      82,
		UserID:  40,
		GroupID: &groupID,
		Key:     "sk-no-account-fallback-roundtrip",
		Name:    "no-account-fallback-roundtrip",
		Status:  StatusActive,
		User: &User{
			ID:          40,
			Email:       "fallback@test.local",
			Status:      StatusActive,
			Concurrency: 5,
		},
		Group: &Group{
			ID:                         groupID,
			Name:                       "primary-roundtrip",
			Platform:                   PlatformAnthropic,
			Status:                     StatusActive,
			Hydrated:                   true,
			RateMultiplier:             1,
			SubscriptionType:           SubscriptionTypeStandard,
			FallbackGroupIDOnNoAccount: &fallbackGroupID,
		},
	}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.NotNil(t, snapshot.Group)
	require.NotNil(t, snapshot.Group.FallbackGroupIDOnNoAccount)
	require.Equal(t, fallbackGroupID, *snapshot.Group.FallbackGroupIDOnNoAccount)
	require.Equal(t, apiKeyAuthSnapshotVersion, snapshot.Version)

	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	var restored APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &restored))

	materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &restored)
	require.NoError(t, err)
	require.True(t, used)
	require.NotNil(t, materialized.Group)
	require.NotNil(t, materialized.Group.FallbackGroupIDOnNoAccount,
		"快照漏了兜底分组时本断言最先失败：线上表现是配置了兜底却永远不兜底")
	require.Equal(t, fallbackGroupID, *materialized.Group.FallbackGroupIDOnNoAccount)
}

// 未配置兜底的分组必须保持 nil，不能被序列化往返变成 0。
func TestAPIKeyAuthSnapshotNoAccountFallbackAbsentStaysNil(t *testing.T) {
	svc := &APIKeyService{}

	groupID := int64(60)
	apiKey := &APIKey{
		ID:      83,
		UserID:  41,
		GroupID: &groupID,
		Key:     "sk-no-account-fallback-absent",
		Name:    "no-account-fallback-absent",
		Status:  StatusActive,
		User:    &User{ID: 41, Email: "absent@test.local", Status: StatusActive, Concurrency: 5},
		Group: &Group{
			ID:               groupID,
			Name:             "primary-absent",
			Platform:         PlatformAnthropic,
			Status:           StatusActive,
			Hydrated:         true,
			RateMultiplier:   1,
			SubscriptionType: SubscriptionTypeStandard,
		},
	}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)

	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	var restored APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &restored))

	materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &restored)
	require.NoError(t, err)
	require.True(t, used)
	require.NotNil(t, materialized.Group)
	require.Nil(t, materialized.Group.FallbackGroupIDOnNoAccount)
}
