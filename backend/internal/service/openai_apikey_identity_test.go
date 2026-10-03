//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Adapted from ranxi2001 4549f519e2f97b3f2fbfffd9055fe352ec5d4689 and
// 64885197d8eabb39256b5ab6023f11fa91e37654; defaults retain passthrough.
// Exercise the real builders, including their header copy/override ordering.
// No parallel subtests: the canonical version resolver is process-wide.
func TestOpenAIAPIKeyOutboundIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetCodexCanonicalUserAgentResolver(func() string {
		return openai.CodexDefaultOriginator + "/0.200.1" + codexCLIUserAgentSuffix
	})
	t.Cleanup(func() { SetCodexCanonicalUserAgentResolver(nil) })
	svc := &OpenAIGatewayService{}
	body := []byte(`{"model":"gpt-6-sol","input":[],"stream":true}`)
	builders := map[string]func(*gin.Context, *Account) (http.Header, error){
		"responses": func(c *gin.Context, a *Account) (http.Header, error) {
			r, err := svc.buildUpstreamRequest(c.Request.Context(), c, a, body, "test-token", true, "", false)
			if err != nil {
				return nil, err
			}
			return r.Header, nil
		},
		"passthrough": func(c *gin.Context, a *Account) (http.Header, error) {
			r, err := svc.buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, a, body, "test-token")
			if err != nil {
				return nil, err
			}
			return r.Header, nil
		},
		"input_tokens": func(c *gin.Context, a *Account) (http.Header, error) {
			r, err := svc.buildInputTokensUpstreamRequest(c.Request.Context(), c, a, body, "test-token")
			if err != nil {
				return nil, err
			}
			return r.Header, nil
		},
		"images": func(c *gin.Context, a *Account) (http.Header, error) {
			r, err := svc.buildOpenAIImagesRequest(c.Request.Context(), c, a, body, "application/json", "test-token", openAIImagesGenerationsEndpoint)
			if err != nil {
				return nil, err
			}
			return r.Header, nil
		},
		"websocket": func(c *gin.Context, a *Account) (http.Header, error) {
			h, _, err := svc.buildOpenAIWSHeaders(c.Request.Context(), c, a, "test-token",
				OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
				false, "", "", "", "gpt-6-sol", "")
			return h, err
		},
	}
	for name, build := range builders {
		for _, ua := range []string{"", "Go-http-client/2.0", "python-httpx/0.28.0", "Mozilla/5.0", "codex_cli_rs/0.125.0"} {
			t.Run(name+"/"+ua, func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Request.Header.Set("User-Agent", ua)
				c.Request.Header.Set("Originator", "untrusted-client")
				c.Request.Header.Set("Version", "0.1.0")
				before := c.Request.Header.Clone()
				h, err := build(c, &Account{ID: 991, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{OpenAIAPIKeyCodexIdentityKey: true}})
				require.NoError(t, err)
				require.Equal(t, openai.CodexDefaultOriginator+"/0.200.1"+codexCLIUserAgentSuffix, h.Get("User-Agent"))
				require.Equal(t, openai.CodexDefaultOriginator, h.Get("Originator"))
				require.Equal(t, "0.200.1", h.Get("Version"))
				require.Equal(t, "Bearer test-token", h.Get("Authorization"))
				require.Equal(t, before, c.Request.Header, "inbound audit headers must remain intact")
			})
		}
	}
}

func TestOpenAIAPIKeyChatTestOutboundIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/pelican-test", nil)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n")),
	}}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{ID: 992, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{OpenAIAPIKeyCodexIdentityKey: true}}
	require.NoError(t, svc.testOpenAIChatCompletionsConnection(c, account, "gpt-6-sol", "hi", "https://upstream.example/v1", "test-token"))
	require.Equal(t, CodexCanonicalUserAgent(), upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, openai.CodexDefaultOriginator, upstream.lastReq.Header.Get("Originator"))
	require.Equal(t, CodexCanonicalClientVersion(), upstream.lastReq.Header.Get("Version"))
}

func TestOpenAIAPIKeyChatForwardOutboundIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("User-Agent", "Go-http-client/2.0")
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{ID: 993, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{OpenAIAPIKeyCodexIdentityKey: true}}
	resp, err := svc.sendCCUpstreamRequest(context.Background(), c, account, "https://upstream.example/v1/chat/completions", []byte(`{"model":"gpt-6-sol","messages":[]}`), false, "test-token", "", "")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, CodexCanonicalUserAgent(), upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, openai.CodexDefaultOriginator, upstream.lastReq.Header.Get("Originator"))
	require.Equal(t, CodexCanonicalClientVersion(), upstream.lastReq.Header.Get("Version"))
}

func TestOpenAIAPIKeyIdentityPreservesOverridesAndOptOut(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("User-Agent", "Go-http-client/2.0")
	for _, tc := range []struct {
		name     string
		account  *Account
		force    bool
		disabled bool
		wantUA   string
	}{
		{"account_codex_version_refreshed", &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"user_agent": "codex_vscode/0.125.0 (Linux; x86_64) vscode"}}, false, false, "codex_vscode/" + codexCLIVersion + " (Linux; x86_64) vscode"},
		{"force_cli", &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"user_agent": "codex_vscode/0.125.0 (Linux; x86_64) vscode"}}, true, false, codexCLIUserAgent},
		{"explicit_override", &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{credKeyHeaderOverrideEnabled: true, credKeyHeaderOverrides: map[string]any{"user-agent": "custom-client/1.0", "originator": "custom-client", "version": "1.0"}}}, false, false, "custom-client/1.0"},
		{"disabled", &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{OpenAIAPIKeyCodexIdentityKey: true}}, false, true, "Go-http-client/2.0"},
		{"other_platform", &Account{Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Extra: map[string]any{OpenAIAPIKeyCodexIdentityKey: true}}, false, false, "Go-http-client/2.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.account.Extra = map[string]any{OpenAIAPIKeyCodexIdentityKey: !tc.disabled}
			SetCodexIdentityEnforcementEnabled(false)
			t.Cleanup(func() { SetCodexIdentityEnforcementEnabled(true) })
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{ForceCodexCLI: tc.force}}}
			r, err := svc.buildUpstreamRequest(c.Request.Context(), c, tc.account, []byte(`{"model":"gpt-6-sol"}`), "test-token", true, "", false)
			require.NoError(t, err)
			require.Equal(t, tc.wantUA, r.Header.Get("User-Agent"))
			if tc.name == "explicit_override" {
				require.Equal(t, "custom-client", getHeaderRaw(r.Header, "originator"))
				require.Equal(t, "1.0", getHeaderRaw(r.Header, "version"))
			}
		})
	}
}

func TestOpenAIAPIKeyIdentityOptInIsolation(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, value := range []any{nil, false, "true", true} {
			a := &Account{Platform: PlatformOpenAI, Type: kind, Extra: map[string]any{OpenAIAPIKeyCodexIdentityKey: value}}
			h := http.Header{"user-agent": {"library/1"}, "Originator": {"original"}, "version": {"old"}}
			before := h.Clone()
			applyOpenAIAPIKeyIdentityHeaders(h, a, "")
			if kind != AccountTypeAPIKey || value != true {
				require.Equal(t, before, h)
			} else {
				require.Equal(t, CodexCanonicalUserAgent(), h.Get("User-Agent"))
				require.Len(t, h, 3)
			}
		}
	}
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{}}
	before := openAITurnRouteFingerprint(a)
	a.Extra[OpenAIAPIKeyCodexIdentityKey] = true
	require.NotEqual(t, before, openAITurnRouteFingerprint(a), "changing handshake identity requires revalidation")
}

func TestAPIKeyCodexIdentityBulkValidation(t *testing.T) {
	for _, value := range []any{true, false, "true", nil, 1} {
		repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}}}
		svc := &adminServiceImpl{accountRepo: repo}
		_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Extra: map[string]any{OpenAIAPIKeyCodexIdentityKey: value}})
		if _, ok := value.(bool); ok {
			require.NoError(t, err)
			require.Equal(t, value, repo.lastBulkUpdate.Extra[OpenAIAPIKeyCodexIdentityKey])
		} else {
			require.Error(t, err)
			require.Zero(t, repo.bulkUpdateCalls)
		}
	}
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, {ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}}
	_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2}, Extra: map[string]any{OpenAIAPIKeyCodexIdentityKey: true}})
	require.Error(t, err)
	require.Zero(t, repo.bulkUpdateCalls)
}

func TestAPIKeyCodexIdentityValidation(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		a := &Account{Platform: PlatformOpenAI, Type: kind}
		require.NoError(t, validateOpenAIAPIKeyIdentityExtra(a, nil))
		for _, value := range []any{true, false, nil, "true"} {
			err := validateOpenAIAPIKeyIdentityExtra(a, map[string]any{OpenAIAPIKeyCodexIdentityKey: value})
			_, valid := value.(bool)
			if kind == AccountTypeAPIKey && valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		}
	}
}

func TestAPIKeyIdentityProviderOverrideWins(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{OpenAIAPIKeyCodexIdentityKey: true}, Credentials: map[string]any{"base_url": "https://opencode.ai/zen/go/v1"}}
	svc := &OpenAIGatewayService{}
	req, err := svc.buildUpstreamRequest(c.Request.Context(), c, a, []byte(`{"model":"gpt-6-sol"}`), "token", true, "", false)
	require.NoError(t, err)
	require.Equal(t, openCodeUpstreamUserAgent, req.Header.Get("User-Agent"))
	a.Credentials[credKeyHeaderOverrideEnabled] = true
	a.Credentials[credKeyHeaderOverrides] = map[string]any{"user-agent": "explicit/1"}
	req, err = svc.buildUpstreamRequest(c.Request.Context(), c, a, []byte(`{"model":"gpt-6-sol"}`), "token", true, "", false)
	require.NoError(t, err)
	require.Equal(t, "explicit/1", getHeaderRaw(req.Header, "user-agent"))
}

func TestAPIKeyIdentityEmbeddings(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[],"usage":{"prompt_tokens":1}}`))}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}, Extra: map[string]any{OpenAIAPIKeyCodexIdentityKey: true}}
	_, err := svc.ForwardEmbeddings(c.Request.Context(), c, a, []byte(`{"model":"text-embedding-3-small","input":"hi"}`), "")
	require.NoError(t, err)
	require.Equal(t, CodexCanonicalUserAgent(), upstream.lastReq.Header.Get("User-Agent"))
}
