package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type identityCacheStub struct {
	mu              sync.Mutex
	maskedSessionID string
	controlHeaders  map[int64]http.Header
	cacheErr        error
}

func (s *identityCacheStub) GetFingerprint(_ context.Context, _ int64) (*Fingerprint, error) {
	return nil, nil
}
func (s *identityCacheStub) SetFingerprint(_ context.Context, _ int64, _ *Fingerprint) error {
	return nil
}
func (s *identityCacheStub) GetOrCreateSessionMaskSeed(_ context.Context, _ int64, candidate string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cacheErr != nil {
		return "", s.cacheErr
	}
	if s.maskedSessionID == "" {
		s.maskedSessionID = candidate
	}
	return s.maskedSessionID, nil
}
func (s *identityCacheStub) GetClaudeCodeHeaders(_ context.Context, id int64) (http.Header, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.controlHeaders[id].Clone(), s.cacheErr
}
func (s *identityCacheStub) UpdateClaudeCodeHeaders(_ context.Context, id int64, headers http.Header) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cacheErr != nil {
		return s.cacheErr
	}
	if s.controlHeaders == nil {
		s.controlHeaders = make(map[int64]http.Header)
	}
	if s.controlHeaders[id] == nil {
		s.controlHeaders[id] = http.Header{}
	}
	for k, v := range headers.Clone() {
		s.controlHeaders[id][k] = v
	}
	return nil
}

func TestIdentityService_RewriteUserID_PreservesTopLevelFieldOrder(t *testing.T) {
	cache := &identityCacheStub{}
	svc := NewIdentityService(cache)

	originalUserID := FormatMetadataUserID(
		"d61f76d0730d2b920763648949bad5c79742155c27037fc77ac3f9805cb90169",
		"",
		"7578cf37-aaca-46e4-a45c-71285d9dbb83",
		"2.1.78",
	)
	body := []byte(`{"alpha":1,"messages":[],"metadata":{"user_id":` + strconvQuote(originalUserID) + `},"max_tokens":64000,"thinking":{"type":"adaptive"},"output_config":{"effort":"high"},"stream":true}`)

	result, err := svc.RewriteUserID(body, 123, "acc-uuid", "client-xyz", "claude-cli/2.1.78 (external, cli)")
	require.NoError(t, err)
	resultStr := string(result)

	assertJSONTokenOrder(t, resultStr, `"alpha"`, `"messages"`, `"metadata"`, `"max_tokens"`, `"thinking"`, `"output_config"`, `"stream"`)
	require.NotContains(t, resultStr, originalUserID)
	require.Contains(t, resultStr, `"metadata":{"user_id":"`)
}

func TestIdentityService_RewriteUserIDWithMasking_PreservesTopLevelFieldOrder(t *testing.T) {
	cache := &identityCacheStub{maskedSessionID: "11111111-2222-4333-8444-555555555555"}
	svc := NewIdentityService(cache)

	originalUserID := FormatMetadataUserID(
		"d61f76d0730d2b920763648949bad5c79742155c27037fc77ac3f9805cb90169",
		"",
		"7578cf37-aaca-46e4-a45c-71285d9dbb83",
		"2.1.78",
	)
	body := []byte(`{"alpha":1,"messages":[],"metadata":{"user_id":` + strconvQuote(originalUserID) + `},"max_tokens":64000,"thinking":{"type":"adaptive"},"output_config":{"effort":"high"},"stream":true}`)

	account := &Account{
		ID:       123,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"session_id_masking_enabled": true,
		},
	}

	result, err := svc.RewriteUserIDWithMasking(context.Background(), body, account, "acc-uuid", "client-xyz", "claude-cli/2.1.78 (external, cli)")
	require.NoError(t, err)
	resultStr := string(result)

	assertJSONTokenOrder(t, resultStr, `"alpha"`, `"messages"`, `"metadata"`, `"max_tokens"`, `"thinking"`, `"output_config"`, `"stream"`)
	require.Contains(t, resultStr, generateUUIDFromSeed("123::"+cache.maskedSessionID+"::7578cf37-aaca-46e4-a45c-71285d9dbb83"))
	require.True(t, strings.Contains(resultStr, `"metadata":{"user_id":"`))
}

func strconvQuote(v string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`) + `"`
}

func TestIdentityServiceMaskingCacheFailureKeepsAccountIsolation(t *testing.T) {
	uid := `{"device_id":"client","account_uuid":"","session_id":"child","parent_session_id":"parent","unknown":9007199254740993}`
	body := []byte(`{"metadata":{"user_id":` + strconvQuote(uid) + `}}`)
	for _, cache := range []IdentityCache{nil, &identityCacheStub{cacheErr: errors.New("unavailable")}} {
		svc := NewIdentityService(cache)
		account := &Account{ID: 11, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"session_id_masking_enabled": true}}
		want, err := svc.RewriteUserID(body, 11, "account", "cached-device", "claude-cli/2.1.301")
		require.NoError(t, err)
		got, err := svc.RewriteUserIDWithMasking(t.Context(), body, account, "account", "cached-device", "claude-cli/2.1.301")
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.NotContains(t, string(got), strconvQuote(uid))
		require.Contains(t, string(got), "9007199254740993")
		account.ID++
		other, err := svc.RewriteUserIDWithMasking(t.Context(), body, account, "account", "cached-device", "claude-cli/2.1.301")
		require.NoError(t, err)
		require.NotEqual(t, got, other)
	}
}
