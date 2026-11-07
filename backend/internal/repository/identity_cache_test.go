//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestFingerprintKey(t *testing.T) {
	tests := []struct {
		name      string
		accountID int64
		expected  string
	}{
		{
			name:      "normal_account_id",
			accountID: 123,
			expected:  "fingerprint:123",
		},
		{
			name:      "zero_account_id",
			accountID: 0,
			expected:  "fingerprint:0",
		},
		{
			name:      "negative_account_id",
			accountID: -1,
			expected:  "fingerprint:-1",
		},
		{
			name:      "max_int64",
			accountID: math.MaxInt64,
			expected:  "fingerprint:9223372036854775807",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fingerprintKey(tc.accountID)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestIdentityCacheAtomicSeedAndIdleExpiry(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := NewIdentityCache(rdb).(*identityCache)
	ctx := t.Context()
	require.NoError(t, rdb.Set(ctx, "masked_session:1", "obsolete-fixed-id", time.Hour).Err())
	const workers = 40
	raceForSeed := func(prefix string) string {
		seeds, errs := make([]string, workers), make([]error, workers)
		var wg sync.WaitGroup
		for i := range seeds {
			wg.Add(1)
			go func() {
				defer wg.Done()
				seeds[i], errs[i] = cache.GetOrCreateSessionMaskSeed(ctx, 1, fmt.Sprintf("%s%d", prefix, i))
			}()
		}
		wg.Wait()
		for i := range seeds {
			require.NoError(t, errs[i])
			require.Equal(t, seeds[0], seeds[i])
		}
		return seeds[0]
	}
	seed := raceForSeed("initial-")
	require.NotEmpty(t, seed)
	require.NotEqual(t, "obsolete-fixed-id", seed)
	require.Equal(t, sessionMaskSeedTTL, mr.TTL(sessionMaskSeedKey(1)))
	mr.FastForward(14 * time.Minute)
	refreshed, err := cache.GetOrCreateSessionMaskSeed(ctx, 1, "losing-refresh")
	require.NoError(t, err)
	require.Equal(t, seed, refreshed)
	mr.FastForward(2 * time.Minute)
	refreshed, err = cache.GetOrCreateSessionMaskSeed(ctx, 1, "still-active")
	require.NoError(t, err)
	require.Equal(t, seed, refreshed)
	mr.FastForward(sessionMaskSeedTTL + time.Second)
	rotated := raceForSeed("after-expiry-")
	require.NotEqual(t, seed, rotated)
	require.Equal(t, sessionMaskSeedTTL, mr.TTL(sessionMaskSeedKey(1)))
	other, err := cache.GetOrCreateSessionMaskSeed(ctx, 2, "other-account")
	require.NoError(t, err)
	require.Equal(t, "other-account", other)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = cache.GetOrCreateSessionMaskSeed(canceled, 1, "must-not-win")
	require.Error(t, err)
}

func TestIdentityCacheMaskedParentMappingAcrossExpiry(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	svc := service.NewIdentityService(NewIdentityCache(rdb))
	account := &service.Account{ID: 3, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Extra: map[string]any{"session_id_masking_enabled": true}}
	rewrite := func(session, parent string) gjson.Result {
		uid, err := json.Marshal(map[string]any{"device_id": "device", "account_uuid": "client-account", "session_id": session, "parent_session_id": parent, "future": json.RawMessage(`{"n":9007199254740993}`)})
		require.NoError(t, err)
		body, err := json.Marshal(map[string]any{"metadata": map[string]string{"user_id": string(uid)}})
		require.NoError(t, err)
		out, err := svc.RewriteUserIDWithMasking(t.Context(), body, account, "upstream-account", "cached-device", "claude-cli/2.1.301")
		require.NoError(t, err)
		result := gjson.Parse(gjson.GetBytes(out, "metadata.user_id").String())
		require.Equal(t, "9007199254740993", result.Get("future.n").Raw)
		return result
	}
	parent := rewrite("parent", "")
	child := rewrite("child", "parent")
	require.Equal(t, parent.Get("session_id").String(), child.Get("parent_session_id").String())
	require.NotEqual(t, child.Get("session_id").String(), child.Get("parent_session_id").String())
	require.NotEqual(t, "child", child.Get("session_id").String())
	mr.FastForward(4 * time.Minute)
	remainingTTL := mr.TTL(sessionMaskSeedKey(account.ID))
	account.Extra["session_id_masking_enabled"] = false
	disabledChild, disabledParent := rewrite("child", "parent"), rewrite("parent", "")
	require.NotEqual(t, child.Get("session_id").String(), disabledChild.Get("session_id").String())
	require.Equal(t, disabledParent.Get("session_id").String(), disabledChild.Get("parent_session_id").String())
	require.Equal(t, remainingTTL, mr.TTL(sessionMaskSeedKey(account.ID)), "disabled masking does not refresh the existing seed")
	account.Extra["session_id_masking_enabled"] = true
	require.Equal(t, child.Raw, rewrite("child", "parent").Raw, "re-enable within idle expiry uses the surviving seed")
	require.Equal(t, sessionMaskSeedTTL, mr.TTL(sessionMaskSeedKey(account.ID)))
	mr.FastForward(14 * time.Minute)
	require.Equal(t, child.Raw, rewrite("child", "parent").Raw)
	mr.FastForward(2 * time.Minute)
	require.Equal(t, child.Raw, rewrite("child", "parent").Raw)
	mr.FastForward(sessionMaskSeedTTL + time.Second)
	newParent, newChild := rewrite("parent", ""), rewrite("child", "parent")
	require.NotEqual(t, child.Get("session_id").String(), newChild.Get("session_id").String())
	require.Equal(t, newParent.Get("session_id").String(), newChild.Get("parent_session_id").String())
	account.Extra["session_id_masking_enabled"] = false
	mr.FastForward(sessionMaskSeedTTL + time.Second)
	require.Equal(t, disabledChild.Raw, rewrite("child", "parent").Raw, "disabled mapping is independent of mask seed lifetime")
	require.False(t, mr.Exists(sessionMaskSeedKey(account.ID)), "disabled requests do not create a seed after expiry")
	account.Extra["session_id_masking_enabled"] = true
	reenabledParent, reenabledChild := rewrite("parent", ""), rewrite("child", "parent")
	require.NotEqual(t, newChild.Get("session_id").String(), reenabledChild.Get("session_id").String())
	require.Equal(t, reenabledParent.Get("session_id").String(), reenabledChild.Get("parent_session_id").String())
	account.ID++
	require.NotEqual(t, reenabledChild.Get("session_id").String(), rewrite("child", "parent").Get("session_id").String())
}

func TestIdentityCacheControlFeedbackMergeAndExpiry(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := NewIdentityCache(rdb).(*identityCache)
	ctx := t.Context()
	fields := http.Header{"anthropic-usage-limit": {"first", "second"}, "x-claude-code-prev-tool-durations": {""}, "x-claude-code-context-compacted": {}, "x-cc-context-compacted": nil, "x-claude-code-compaction": {"upstream"}, "x-cc-compaction-request": {"request"}}
	errs := make(chan error, len(fields))
	for name, values := range fields {
		go func() { errs <- cache.UpdateClaudeCodeHeaders(ctx, 1, http.Header{name: values}) }()
	}
	for range fields {
		require.NoError(t, <-errs)
	}
	got, err := cache.GetClaudeCodeHeaders(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, fields, got)
	require.NoError(t, cache.UpdateClaudeCodeHeaders(ctx, 1, http.Header{"anthropic-usage-limit": {""}}))
	got, err = cache.GetClaudeCodeHeaders(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []string{""}, got["anthropic-usage-limit"])
	require.Equal(t, fields["x-claude-code-compaction"], got["x-claude-code-compaction"])
	other, err := cache.GetClaudeCodeHeaders(ctx, 2)
	require.NoError(t, err)
	require.Empty(t, other)
	mr.FastForward(fingerprintTTL - time.Minute)
	require.NoError(t, cache.UpdateClaudeCodeHeaders(ctx, 1, nil))
	mr.FastForward(2 * time.Minute)
	got, err = cache.GetClaudeCodeHeaders(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, got, "requests without upstream feedback must not extend expiry")
}
