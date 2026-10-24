//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClaudeCodeControlHeadersUpstreamOnlyAndMimicFallback(t *testing.T) {
	cache := &identityCacheStub{}
	svc := &GatewayService{cfg: &config.Config{}, identityService: NewIdentityService(cache)}
	account := &Account{ID: 71, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("anthropic-usage-limit", "client-only")
	body := []byte(`{"model":"claude-haiku-4-5","messages":[]}`)
	_, _, err := svc.buildUpstreamRequest(t.Context(), c, account, body, "token", "oauth", "claude-haiku-4-5", false, true)
	require.NoError(t, err)
	cached, err := cache.GetClaudeCodeHeaders(t.Context(), account.ID)
	require.NoError(t, err)
	require.Empty(t, cached, "client request values cannot become shared defaults")
	feedback := http.Header{"Authorization": {"must-not-cache"}, "Set-Cookie": {"must-not-cache"}, "X-Claude-Code-Session-Id": {"must-not-cache"}}
	for _, name := range claudeCodeControlHeaderNames {
		feedback[http.CanonicalHeaderKey(name)] = []string{"observed-" + name}
	}
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: feedback}
	svc.observeClaudeCodeResponseHeaders(t.Context(), account, resp)
	cached, err = cache.GetClaudeCodeHeaders(t.Context(), account.ID)
	require.NoError(t, err)
	require.Len(t, cached, len(claudeCodeControlHeaderNames))
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	for _, tc := range []struct {
		name, typ, tokenType, platform string
		mimic, fallback                bool
	}{
		{"oauth mimic", AccountTypeOAuth, "oauth", PlatformAnthropic, true, true},
		{"setup mimic", AccountTypeSetupToken, "oauth", PlatformAnthropic, true, true},
		{"real CC", AccountTypeOAuth, "oauth", PlatformAnthropic, false, false},
		{"api key", AccountTypeAPIKey, "apikey", PlatformAnthropic, true, false},
		{"foreign OAuth", AccountTypeOAuth, "oauth", PlatformOpenAI, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{ID: account.ID, Platform: tc.platform, Type: tc.typ}
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			svc.applyClaudeCodeControlHeaders(t.Context(), req, nil, a, tc.tokenType, tc.mimic)
			got := filterClaudeCodeControlHeaders(req.Header)
			if tc.fallback {
				require.Equal(t, cached, got)
			} else {
				require.Empty(t, got)
			}
		})
	}
	for _, name := range claudeCodeControlHeaderNames {
		for _, values := range [][]string{nil, {}, {""}, {"caller", "second"}} {
			for _, countTokens := range []bool{false, true} {
				c.Request.Header = http.Header{http.CanonicalHeaderKey(name): values}
				var req *http.Request
				if countTokens {
					req, _, err = svc.buildCountTokensRequest(t.Context(), c, account, body, "token", "oauth", "claude-haiku-4-5", true)
				} else {
					req, _, err = svc.buildUpstreamRequest(t.Context(), c, account, body, "token", "oauth", "claude-haiku-4-5", false, true)
				}
				require.NoError(t, err)
				got := filterClaudeCodeControlHeaders(req.Header)
				require.Contains(t, got, name)
				require.Equal(t, values, got[name], "explicit empty presence beats cached defaults")
				require.Len(t, got, len(claudeCodeControlHeaderNames))
				for other, want := range cached {
					if other != name {
						require.Equal(t, want, got[other])
					}
				}
			}
		}
	}
	before := cached.Clone()
	svc.observeClaudeCodeResponseHeaders(t.Context(), account, nil)
	svc.observeClaudeCodeResponseHeaders(t.Context(), &Account{ID: 72, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}, resp)
	other, err := cache.GetClaudeCodeHeaders(t.Context(), 72)
	require.NoError(t, err)
	require.Empty(t, other)
	cache.cacheErr = errors.New("cache unavailable")
	svc.observeClaudeCodeResponseHeaders(t.Context(), account, resp)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	svc.applyClaudeCodeControlHeaders(t.Context(), req, http.Header{"anthropic-usage-limit": {"caller"}}, account, "oauth", true)
	require.Equal(t, "caller", getHeaderRaw(req.Header, "anthropic-usage-limit"))
	cache.cacheErr = nil
	cached, err = cache.GetClaudeCodeHeaders(t.Context(), account.ID)
	require.NoError(t, err)
	require.Equal(t, before, cached)
}

// Scenario: OAuth / setup-token 凭据只能走签发方协议，API Key 与其他凭据类型不受限
func TestAnthropicMessagesCredentialAllowed(t *testing.T) {
	cases := []struct {
		platform string
		typ      string
		allowed  bool
	}{
		{PlatformAnthropic, AccountTypeOAuth, true},
		{PlatformAnthropic, AccountTypeSetupToken, true},
		{PlatformAnthropic, AccountTypeServiceAccount, true},
		{PlatformGemini, AccountTypeServiceAccount, false},
		{PlatformAnthropic, AccountTypeBedrock, true},
		{PlatformTypeSafe, AccountTypeOAuth, true},
		{PlatformOpenAI, AccountTypeAPIKey, true},
		{PlatformDeepseek, AccountTypeAPIKey, true},
		{PlatformOpenAI, AccountTypeOAuth, false},
		{PlatformOpenAI, AccountTypeSetupToken, false},
		{PlatformGrok, AccountTypeOAuth, false},
		{PlatformGemini, AccountTypeOAuth, false},
		{PlatformAntigravity, AccountTypeOAuth, false},
	}
	for _, tc := range cases {
		allowed := anthropicMessagesCredentialAllowed(&Account{ID: 1, Platform: tc.platform, Type: tc.typ})
		require.Equal(t, tc.allowed, allowed, "%s/%s", tc.platform, tc.typ)
	}
}

// Scenario: 回归 —— OpenAI OAuth 账号误入 Anthropic 管线时，在构建请求前即被拒绝
// （ChatGPT token 不会发往 api.anthropic.com），且只让 handler 换号、不处罚账号
func TestGatewayBuildUpstreamRequestRejectsForeignOAuthWithoutPenalty(t *testing.T) {
	account := &Account{ID: 25, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	body := []byte(`{"model":"gpt-6-astra","messages":[]}`)

	req, _, err := (&GatewayService{}).buildUpstreamRequest(context.Background(), nil, account, body, "chatgpt-token", "oauth", "gpt-6-astra", true, false)
	require.Nil(t, req)

	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.True(t, failoverErr.ShouldRetryNextAccount())
	require.False(t, failoverErr.RetryableOnSameAccount, "same-account retry would end in temp-unschedulable")
	require.False(t, failoverErr.IsCredentialFailure())
	require.Equal(t, anthropicMessagesForeignOAuthReason, failoverErr.Reason)
}

// Scenario: count_tokens 对不匹配凭据按「平台不支持」返回 404，不取 token、不出站
func TestForwardCountTokensRejectsForeignOAuthAsUnsupported(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)

	account := &Account{ID: 25, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	parsed := &ParsedRequest{Model: "gpt-6-astra", Body: NewRequestBodyRef([]byte(`{"model":"gpt-6-astra","messages":[]}`))}

	err := (&GatewayService{}).ForwardCountTokens(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, w.Code)
}
