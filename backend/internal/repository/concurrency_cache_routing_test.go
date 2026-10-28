package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRoutingIngressLeaseUsesExistingRedisTokenContract(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewConcurrencyCache(client, 15, 900).(*concurrencyCache)
	now := time.Now()
	server.SetTime(now)
	lease, acquired, err := routing.AcquireIngressLease(t.Context(), store, 81, 1, "first-owner", nil)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(lease.Release)
	_, acquired, err = routing.AcquireIngressLease(t.Context(), store, 81, 1, "rejected-owner", nil)
	require.NoError(t, err)
	require.False(t, acquired)
	// Existing Redis TIME expiry permits replacement. A delayed old release
	// removes only its original token and cannot free the new connection.
	server.SetTime(now.Add(61 * time.Second))
	acquired, err = store.AcquireOpenAIWSIngressLease(t.Context(), 81, 1, "new-owner")
	require.NoError(t, err)
	require.True(t, acquired)
	lease.Release()
	owned, err := store.RefreshOpenAIWSIngressLease(t.Context(), 81, "new-owner")
	require.NoError(t, err)
	require.True(t, owned)
	require.NoError(t, store.ReleaseOpenAIWSIngressLease(t.Context(), 81, "new-owner"))
}

func TestRoutingRecoveryFinishesOnlyExistingRedisEpochOwner(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewConcurrencyCache(client, 15, 900).(*concurrencyCache)
	now := time.Now()
	server.SetTime(now)
	require.NoError(t, store.BlockRecoveryScope(t.Context(), "model:81:canonical", 0))
	root, cancel := context.WithCancel(t.Context())
	defer cancel()
	recovery := routing.NewRecovery(root, store, nil, nil)
	require.NoError(t, recovery.Acquire(t.Context(), 81, "first-owner", []routing.RecoveryScope{{Key: "model:81:canonical"}}, true))
	server.SetTime(now.Add(61 * time.Second))
	allowed, probe, err := store.AcquireRecoveryProbe(t.Context(), "model:81:canonical", 0, "new-owner")
	require.NoError(t, err)
	require.True(t, allowed && probe)
	recovery.Finish(81, true, 0)
	owned, err := store.RefreshRecoveryProbe(t.Context(), "model:81:canonical", "new-owner")
	require.NoError(t, err)
	require.True(t, owned)
	require.NoError(t, store.FinishRecoveryProbe(t.Context(), "model:81:canonical", "new-owner", 0, true))
}
