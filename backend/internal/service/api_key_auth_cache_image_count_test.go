package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotImageCountRoundTripAndVersion(t *testing.T) {
	svc := &APIKeyService{}
	for _, enabled := range []bool{false, true} {
		groupID := int64(59)
		key := &APIKey{
			ID: 81, UserID: 42, GroupID: &groupID, Key: "test-image-count", Status: StatusActive,
			User: &User{ID: 42, Role: RoleUser, Status: StatusActive},
			Group: &Group{
				ID: groupID, Platform: PlatformOpenAI, Status: StatusActive,
				AllowImageGeneration: true, ImageCountBilling: enabled,
			},
		}
		entry := &APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), key)}
		encoded, err := json.Marshal(entry)
		require.NoError(t, err)
		var cached APIKeyAuthCacheEntry
		require.NoError(t, json.Unmarshal(encoded, &cached))
		got, used, err := svc.applyAuthCacheEntry(key.Key, &cached)
		require.NoError(t, err)
		require.True(t, used)
		require.NotNil(t, got.Group)
		require.Equal(t, enabled, got.Group.ImageCountBilling)
		require.Equal(t, key.User.Role, got.User.Role, "image policy cannot rewrite authorization")
		require.Equal(t, groupID, got.Group.ID)

		// A pre-field cache record has no way to distinguish disabled from absent.
		cached.Snapshot.Version = 31
		cached.Snapshot.Group.ImageCountBilling = false
		got, used, err = svc.applyAuthCacheEntry(key.Key, &cached)
		require.NoError(t, err)
		require.False(t, used)
		require.Nil(t, got)
	}
}
