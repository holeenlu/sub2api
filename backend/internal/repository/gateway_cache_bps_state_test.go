package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestBPSSharedStateCrossInstanceCASIsolationExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	first := redis.NewClient(&redis.Options{Addr: server.Addr()})
	second := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = first.Close(); _ = second.Close() })
	a, b := &gatewayCache{rdb: first}, &gatewayCache{rdb: second}
	ctx := context.Background()
	scope := "account:1/key:2/thread:example"
	zero := uint64(0)
	ok, err := a.SaveBPSState(ctx, scope, "catalog", &zero, []byte("first"))
	require.NoError(t, err)
	require.True(t, ok)
	raw, version, err := b.LoadBPSState(ctx, scope, "catalog")
	require.NoError(t, err)
	require.Equal(t, "first", string(raw))
	require.NotZero(t, version)
	ok, err = b.SaveBPSState(ctx, scope, "catalog", &zero, []byte("stale"))
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = b.SaveBPSState(ctx, scope, "catalog", &version, []byte("new"))
	require.NoError(t, err)
	require.True(t, ok)
	for _, other := range []string{"account:2/key:2/thread:example", "account:1/key:3/thread:example", "account:1/key:2/thread:other"} {
		raw, _, err = a.LoadBPSState(ctx, other, "catalog")
		require.NoError(t, err)
		require.Empty(t, raw)
	}
	server.FastForward(time.Hour)
	_, _, err = b.LoadBPSState(ctx, scope, "catalog")
	require.NoError(t, err)
	require.Equal(t, bpsStateTTL, server.TTL(bpsStateKey(scope, "catalog")))
	server.FastForward(bpsStateTTL + time.Second)
	raw, _, err = a.LoadBPSState(ctx, scope, "catalog")
	require.NoError(t, err)
	require.Empty(t, raw)
}

func TestBPSSharedStateLargeToolCrossInstanceContinuation(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	store := &gatewayCache{rdb: client}
	replay, catalog := basispoints.NewPersistentCaches(ctx, store)
	source := map[string]any{
		"model": "gpt-5", "input": "inspect",
		"tools": []any{map[string]any{"type": "function", "name": "shell", "parameters": map[string]any{"type": "object", "description": strings.Repeat("x", (4<<20)-64)}}},
	}
	raw, err := json.Marshal(source)
	require.NoError(t, err)
	_, bridge, err := basispoints.PrepareWithCatalog(raw, "account/key/thread", replay, catalog)
	require.NoError(t, err)
	envelope, err := json.Marshal(map[string]any{"name": "shell", "arguments": map[string]any{"value": strings.Repeat("\"", (4<<20)/2-256)}})
	require.NoError(t, err)
	outer, err := json.Marshal(map[string]any{"code": string(envelope), "summary": "large", "references": []any{}, "destructive": false})
	require.NoError(t, err)
	native := map[string]any{"type": "function_call", "id": "fc_large", "call_id": "call_large", "name": "run_officejs", "arguments": string(outer), "status": "completed"}
	event, err := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_large", "status": "completed", "output": []any{native}}})
	require.NoError(t, err)
	stream := bridge.Stream(io.NopCloser(strings.NewReader("data: " + string(event) + "\n\n")))
	converted, err := io.ReadAll(stream)
	require.NoError(t, stream.Close())
	require.NoError(t, err)
	require.Contains(t, string(converted), "response.completed")
	require.NotContains(t, string(converted), "response.failed")
	stored, _, err := store.LoadBPSState(ctx, "account/key/thread", "replay:call_large")
	require.NoError(t, err)
	require.Greater(t, len(stored), 4<<20, "escaped replay must exercise the larger Redis entry budget")
	// New caches represent another process: both catalog and native identity
	// must come from Redis when the client sends only the tool result.
	replay, catalog = basispoints.NewPersistentCaches(ctx, &gatewayCache{rdb: client})
	delete(source, "tools")
	source["input"] = []any{map[string]any{"type": "function_call_output", "call_id": "call_large", "output": "done"}}
	raw, err = json.Marshal(source)
	require.NoError(t, err)
	next, _, err := basispoints.PrepareWithCatalog(raw, "account/key/thread", replay, catalog)
	require.NoError(t, err)
	require.Contains(t, string(next), "fc_large")
	require.NoError(t, replay.Err())
	require.NoError(t, catalog.Err())
}

func TestBPSSharedStateBudgetsAndErrors(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	c := &gatewayCache{rdb: client}
	ctx := context.Background()
	_, err := c.SaveBPSState(ctx, "scope", "large", nil, []byte(strings.Repeat("x", bpsStateEntryLimit+1)))
	require.ErrorIs(t, err, basispoints.ErrStateCapacity)
	for i := 0; i < 10; i++ {
		_, err = c.SaveBPSState(ctx, fmt.Sprint(i), "replay", nil, []byte(strings.Repeat("x", bpsStateByteLimit/8)))
		require.NoError(t, err)
	}
	size, err := client.HGet(ctx, bpsStatePrefix+"sizes", "__bytes").Int64()
	require.NoError(t, err)
	require.LessOrEqual(t, size, int64(bpsStateByteLimit))
	count, err := client.ZCard(ctx, bpsStatePrefix+"lru").Result()
	require.NoError(t, err)
	require.LessOrEqual(t, count, int64(bpsStateCountLimit))
	// Closed client fails immediately; no live Redis or network outage timing needed.
	require.NoError(t, client.Close())
	_, _, err = c.LoadBPSState(ctx, "scope", "catalog")
	require.ErrorIs(t, err, basispoints.ErrStateUnavailable)
}
