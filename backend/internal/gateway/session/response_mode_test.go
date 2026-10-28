package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type responseModeCache struct {
	mu     sync.Mutex
	values map[string]int64
	err    error
}

func (c *responseModeCache) SetSessionAccountID(_ context.Context, group int64, key string, value int64, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.values[openAIWSResponseAccountMapKey(group, key)] = value
	return nil
}

func (c *responseModeCache) GetSessionAccountID(_ context.Context, group int64, key string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.values[openAIWSResponseAccountMapKey(group, key)], c.err
}

func (c *responseModeCache) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

func (c *responseModeCache) SetHTTPResponseOwner(context.Context, int64, string, int64, int64, time.Duration) error {
	return nil
}

func (c *responseModeCache) GetHTTPResponseOwner(context.Context, int64, string) (int64, int64, bool, error) {
	return 0, 0, false, nil
}

func TestResponseModeSharedRoutingConstraint(t *testing.T) {
	cache := &responseModeCache{values: map[string]int64{}}
	writer, reader := NewStateStore(cache), NewStateStore(cache)
	ctx := t.Context()
	require.NoError(t, writer.BindResponseAccount(ctx, 7, "opaque", 42, time.Hour))
	require.NoError(t, writer.BindResponseMode(ctx, 7, "opaque", ResponseModeBPS, time.Hour))
	mode, err := reader.GetResponseMode(ctx, 7, "opaque")
	require.NoError(t, err)
	require.Equal(t, ResponseModeBPS, mode)
	account, err := reader.GetResponseAccount(ctx, 7, "opaque")
	require.NoError(t, err)
	require.EqualValues(t, 42, account, "mode metadata must not overwrite routing affinity")
	mode, err = reader.GetResponseMode(ctx, 8, "opaque")
	require.NoError(t, err)
	require.Equal(t, ResponseModeUnknown, mode)
	cache.err = errors.New("shared cache unavailable")
	_, err = reader.GetResponseMode(ctx, 7, "opaque")
	require.Error(t, err, "dependency failure must not authorize an unknown-mode continuation")
}

func TestResponseModeLocalExpiryAndIndependentBinding(t *testing.T) {
	store := NewStateStore(nil)
	ctx := t.Context()
	require.NoError(t, store.BindResponseMode(ctx, 1, "response", ResponseModeBPS, time.Hour))
	require.NoError(t, store.BindResponseAccount(ctx, 1, "response", 11, time.Hour))
	mode, err := store.GetResponseMode(ctx, 1, "response")
	require.NoError(t, err)
	require.Equal(t, ResponseModeBPS, mode)
	store.responseModes[openAIWSResponseAccountMapKey(1, "response")] = responseModeBinding{mode: ResponseModeBPS, expiresAt: time.Now().Add(-time.Second)}
	mode, err = store.GetResponseMode(ctx, 1, "response")
	require.Error(t, err, "an expired mode must not reclassify a still-owned response")
	require.Equal(t, ResponseModeUnknown, mode)
	require.Error(t, store.BindResponseMode(ctx, 1, "response", ResponseModeUnknown, time.Hour))
}
