package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type responseModeTestCache struct {
	stubGatewayCache
	readErr error
}

func (c *responseModeTestCache) GetSessionAccountID(ctx context.Context, group int64, key string) (int64, error) {
	if c.readErr != nil {
		return 0, c.readErr
	}
	c.ownerMu.Lock()
	defer c.ownerMu.Unlock()
	if value, ok := c.sessionBindings[key]; ok {
		return value, nil
	}
	return 0, ErrStickySessionNotFound
}

func TestResponseModeSharedAuthorityAndContinuationFence(t *testing.T) {
	cache := &responseModeTestCache{}
	writer := NewOpenAIWSStateStore(cache)
	reader := NewOpenAIWSStateStore(cache)
	ctx := t.Context()
	require.NoError(t, writer.BindResponseMode(ctx, 7, "resp_bps", ResponseModeBPS, time.Hour))
	require.NoError(t, writer.BindResponseAccount(ctx, 7, "resp_bps", 42, time.Hour))
	mode, err := reader.GetResponseMode(ctx, 7, "resp_bps")
	require.NoError(t, err)
	require.Equal(t, ResponseModeBPS, mode)
	svc := &OpenAIGatewayService{openaiWSStateStore: reader}
	require.NoError(t, svc.checkResponseContinuationMode(ctx, 7, "resp_bps", ResponseModeBPS))
	require.ErrorIs(t, svc.checkResponseContinuationMode(ctx, 7, "resp_bps", ResponseModeNative), ErrOpenAIContinuationUnavailable)
	require.ErrorIs(t, svc.checkResponseContinuationMode(ctx, 7, "resp_bps", ResponseModeCloud), ErrOpenAIContinuationUnavailable)
	require.NoError(t, svc.checkResponseContinuationMode(ctx, 7, "resp_external", ResponseModeNative))
	require.ErrorIs(t, svc.checkResponseContinuationMode(ctx, 7, "resp_external", ResponseModeBPS, true), ErrResponseModeUnavailable)

	// An older gateway-owned response cannot borrow the unknown-external exception.
	require.NoError(t, writer.BindResponseAccount(ctx, 7, "resp_old", 42, time.Hour))
	_, err = reader.GetResponseMode(ctx, 7, "resp_old")
	require.ErrorIs(t, err, ErrResponseModeUnavailable)
	cache.readErr = errors.New("shared state unavailable")
	require.ErrorIs(t, svc.checkResponseContinuationMode(ctx, 7, "resp_bps", ResponseModeBPS), cache.readErr)
}

func TestResponseModeLocalStoreExpiryFailsClosed(t *testing.T) {
	store := NewOpenAIWSStateStore(nil).(*defaultOpenAIWSStateStore)
	require.NoError(t, store.BindResponseMode(t.Context(), 1, "resp_local", ResponseModeBPS, time.Hour))
	require.NoError(t, store.BindResponseAccount(t.Context(), 1, "resp_local", 42, time.Hour))
	store.responseModeMu.Lock()
	key := openAIWSResponseAccountMapKey(1, "resp_local")
	binding := store.responseModes[key]
	binding.expiresAt = time.Now().Add(-time.Second)
	store.responseModes[key] = binding
	store.responseModeMu.Unlock()
	_, err := store.GetResponseMode(t.Context(), 1, "resp_local")
	require.ErrorIs(t, err, ErrResponseModeUnavailable)
}
