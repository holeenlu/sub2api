package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// withCLIVersionResolverForTest 注入解析器并在用例结束时还原（置 nil），避免用例间污染。
func withCLIVersionResolverForTest(t *testing.T, resolver func() string) {
	t.Helper()
	claude.SetCLIVersionResolver(resolver)
	t.Cleanup(func() { claude.SetCLIVersionResolver(nil) })
}

// 一致性铁律（d）：模拟一次 OAuth + mimic 的请求构建（messages 与 count_tokens 两条
// 出站路径），断言出站 User-Agent 头里的版本号与请求体 x-anthropic-billing-header
// 中 cc_version 都固定到已验证基线；运行期发现新版本不得关闭 CCH。
func TestBuildOAuthMimicRequest_RuntimeVersionConsistentBetweenHeaderAndBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const upgraded = "9.9.9"
	withCLIVersionResolverForTest(t, func() string { return upgraded })
	const envCheck = "SUB2API_TEST_MIMIC_VERSION_ENV"
	if os.Getenv(envCheck) == "1" {
		withCLIVersionResolverForTest(t, nil)
		require.Equal(t, "2.1.302", claude.EffectiveCLIVersion())
	} else {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestBuildOAuthMimicRequest_RuntimeVersionConsistentBetweenHeaderAndBilling$")
		cmd.Env = append(os.Environ(), envCheck+"=1", claude.CLIVersionEnv+"=2.1.302")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}

	for _, endpoint := range []string{"messages", "count_tokens"} {
		t.Run(endpoint, func(t *testing.T) {
			resetGatewayForwardingSettingsCacheForTest(t)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			body := []byte(`{"model":"claude-haiku-4-5","system":[{"type":"text","text":""}],"messages":[{"role":"user","content":"hello world"}]}`)
			billing, err := buildBillingAttributionText(body, "2.1.81")
			require.NoError(t, err)
			body, err = sjson.SetBytes(body, "system.0.text", billing)
			require.NoError(t, err)

			svc := &GatewayService{cfg: &config.Config{}}
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

			var req *http.Request
			var wireBody []byte
			if endpoint == "messages" {
				req, wireBody, err = svc.buildUpstreamRequest(context.Background(), c, account,
					body, "test-token", "oauth", "claude-haiku-4-5", false, true)
			} else {
				req, wireBody, err = svc.buildCountTokensRequest(context.Background(), c, account,
					body, "test-token", "oauth", "claude-haiku-4-5", true)
			}
			require.NoError(t, err)

			wantUA := claudeCodeMimicUserAgent()
			outboundUA := getHeaderRaw(req.Header, "User-Agent")
			require.Equal(t, wantUA, outboundUA)

			billingText := gjson.GetBytes(wireBody, "system.0.text").String()
			require.Contains(t, billingText, "x-anthropic-billing-header")
			require.Contains(t, billingText, "cc_version="+claudeCCHVersion+"."+computeClaudeCodeFingerprint(wireBody, claudeCCHVersion)+";")
			require.NotContains(t, billingText, "cc_version=2.1.81")
			require.Regexp(t, `; cch=[0-9a-f]{5};`, billingText)
			require.Equal(t, wireBody, finalizeClaudeCCH(wireBody, req.URL.String()))

			// 头/体版本号必须来自同一字符串。
			headerVersion := ExtractCLIVersion(outboundUA)
			require.Equal(t, claudeCCHVersion, headerVersion)
			require.Contains(t, billingText, "cc_version="+headerVersion+".")
		})
	}
}

// e. 注入更高版本后，floorClaudeCLIUserAgentVersion 把存量低版本指纹抬升到新版本；
//
//	等于或高于新版本的指纹保持不动（只升不降）。
func TestFloorClaudeCLIUserAgentVersion_UsesRuntimeVersion(t *testing.T) {
	const upgraded = "9.9.9"
	withCLIVersionResolverForTest(t, func() string { return upgraded })

	floored, changed := floorClaudeCLIUserAgentVersion("claude-cli/2.1.100 (external, cli)")
	require.True(t, changed)
	require.Equal(t, "claude-cli/"+upgraded+" (external, cli)", floored)

	// 等于运行期版本：不动。
	same, changed := floorClaudeCLIUserAgentVersion("claude-cli/" + upgraded + " (external, cli)")
	require.False(t, changed)
	require.Equal(t, "claude-cli/"+upgraded+" (external, cli)", same)

	// 高于运行期版本：不动。
	greater, changed := floorClaudeCLIUserAgentVersion("claude-cli/99.0.0 (external, cli)")
	require.False(t, changed)
	require.Equal(t, "claude-cli/99.0.0 (external, cli)", greater)

	// resolver 返回非法值时回退内置基线，floor 语义仍然成立。
	withCLIVersionResolverForTest(t, func() string { return "abc" })
	baselineFloored, changed := floorClaudeCLIUserAgentVersion("claude-cli/1.0.0 (external, cli)")
	require.True(t, changed)
	require.Equal(t, "claude-cli/"+claude.CLIVersion()+" (external, cli)", baselineFloored)
}

// 注入更高版本后，isAcceptableFingerprintUserAgent 以运行期版本为基准，允许客户端
// 上报的新版本。
func TestIsAcceptableFingerprintUserAgent_UsesRuntimeVersion(t *testing.T) {
	const upgraded = "9.9.9"
	withCLIVersionResolverForTest(t, func() string { return upgraded })
	require.True(t, isAcceptableFingerprintUserAgent("claude-cli/"+upgraded+" (external, cli)"))
	// 允许的最大主版本超前量是 +2（9.9.9 → 11.x）。
	require.True(t, isAcceptableFingerprintUserAgent("claude-cli/11.0.0 (external, cli)"))
	// 超前 +3 会被拒（与静态基线下的既有语义一致，只是基准换成运行期版本）。
	require.False(t, isAcceptableFingerprintUserAgent("claude-cli/12.0.0 (external, cli)"))
}

// defaultFingerprint 现取运行期版本，不再在 init 固化。
func TestDefaultFingerprintUsesRuntimeVersion(t *testing.T) {
	const upgraded = "9.9.9"
	withCLIVersionResolverForTest(t, func() string { return upgraded })
	require.Equal(t, "claude-cli/"+upgraded+" (external, cli)", defaultFingerprint().UserAgent)
}

func TestOAuthMimicKeepsSuppliedPromptHints(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	const promptID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	c.Request.Header.Set("x-claude-code-prompt-id", promptID)
	c.Request.Header.Set("x-claude-code-request-class", "subagent")
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		applyClaudeCodeMimicHeaders(req, c)
		require.Equal(t, promptID, getHeaderRaw(req.Header, "x-claude-code-prompt-id"))
		require.Equal(t, "subagent", getHeaderRaw(req.Header, "x-claude-code-request-class"))
	}
}

func TestOAuthFutureCLIRewrittenMetadataDropsUnverifiedCCH(t *testing.T) {
	withCLIVersionResolverForTest(t, func() string { return "2.1.302" })
	for _, endpoint := range []string{"messages", "count_tokens"} {
		t.Run(endpoint, func(t *testing.T) {
			fp := defaultFingerprint()
			fp.ClientID = strings.Repeat("a", 64)
			svc := &GatewayService{cfg: &config.Config{}, identityService: NewIdentityService(&stubIdentityCache{fingerprint: &fp})}
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
				Extra: map[string]any{"account_uuid": "11111111-2222-4333-8444-555555555555"}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			c.Request.Header.Set("User-Agent", fp.UserAgent)
			uid := FormatMetadataUserID("client-device", "", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", "2.1.302")
			body := []byte(`{"model":"claude-haiku-4-5","max_tokens":64,"metadata":{"user_id":` + strconvQuote(uid) + `},"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.302.abc; cc_entrypoint=cli; cch=abcde;"}],"messages":[{"role":"user","content":"hello"}]}`)
			var req *http.Request
			var wire []byte
			var err error
			if endpoint == "count_tokens" {
				req, wire, err = svc.buildCountTokensRequest(context.Background(), c, account, body, "token", "oauth", "claude-haiku-4-5", false)
			} else {
				req, wire, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "token", "oauth", "claude-haiku-4-5", false, false)
			}
			require.NoError(t, err)
			metadata := ParseMetadataUserID(gjson.GetBytes(wire, "metadata.user_id").String())
			require.NotNil(t, metadata)
			require.Equal(t, fp.ClientID, metadata.DeviceID)
			require.NotEqual(t, uid, gjson.GetBytes(wire, "metadata.user_id").String())
			require.Equal(t, metadata.SessionID, getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
			require.Equal(t, fp.UserAgent, getHeaderRaw(req.Header, "User-Agent"))
			require.Empty(t, getHeaderRaw(req.Header, "x-client-request-id"), "only mimic generates request IDs")
			require.NotContains(t, gjson.GetBytes(wire, "system.0.text").String(), "cch=")
			require.EqualValues(t, len(wire), req.ContentLength)
		})
	}
}

func TestOAuthRequestsKeepVerifiedMimicProfileAndAccountSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"messages", "stream", "count_tokens"} {
		t.Run(endpoint, func(t *testing.T) {
			fp := defaultFingerprint()
			fp.UserAgent = "claude-cli/2.9.0 (external, claude-desktop-3p)"
			fp.ClientID = strings.Repeat("a", 64)
			fp.StainlessOS, fp.StainlessArch = "Linux", "x64"
			fp.StainlessPackageVersion, fp.StainlessRuntimeVersion = "0.140.0", "v27.0.0"
			svc := &GatewayService{cfg: &config.Config{}, identityService: NewIdentityService(&stubIdentityCache{fingerprint: &fp})}
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
				Extra: map[string]any{"account_uuid": "11111111-2222-4333-8444-555555555555", "session_id_masking_enabled": false}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			for key, value := range map[string]string{
				"User-Agent": "foreign-sdk/1.2.3", "X-Stainless-Lang": "python",
				"X-Stainless-Package-Version": "9.0.0", "Accept": "text/event-stream",
				"Accept-Encoding": "identity", "X-Claude-Code-Session-Id": "wrong-session",
				"X-Client-Request-Id": "reused-downstream-id", "X-Stainless-Helper-Method": "stream",
				"Sec-Fetch-Mode": "cors", "Anthropic-Version": "wrong-version",
			} {
				c.Request.Header.Set(key, value)
			}
			uid := FormatMetadataUserID("client-device", "", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", claude.CLICurrentVersion)
			body := []byte(`{"model":"claude-haiku-4-5","max_tokens":64,"metadata":{"user_id":` + strconvQuote(uid) + `},"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.81.000; cc_entrypoint=cli;"}],"messages":[{"role":"user","content":"hello"}]}`)
			previousRequestID := ""
			previousPromptID := ""
			for i := 0; i < 2; i++ {
				var req *http.Request
				var wire []byte
				var err error
				if endpoint == "count_tokens" {
					req, wire, err = svc.buildCountTokensRequest(context.Background(), c, account, body, "token", "oauth", "claude-haiku-4-5", true)
				} else {
					req, wire, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "token", "oauth", "claude-haiku-4-5", endpoint == "stream", true)
				}
				require.NoError(t, err)
				require.Equal(t, claudeCodeMimicUserAgent(), getHeaderRaw(req.Header, "User-Agent"))
				require.Equal(t, "0.128.0", getHeaderRaw(req.Header, "X-Stainless-Package-Version"))
				require.Equal(t, "v26.3.0", getHeaderRaw(req.Header, "X-Stainless-Runtime-Version"))
				require.Equal(t, "Linux", getHeaderRaw(req.Header, "X-Stainless-OS"))
				require.Equal(t, "arm64", getHeaderRaw(req.Header, "X-Stainless-Arch"))
				require.Equal(t, "application/json", getHeaderRaw(req.Header, "Accept"))
				require.Equal(t, []string{"application/json"}, req.Header["Content-Type"])
				require.Equal(t, []string{"Bearer token"}, req.Header["Authorization"])
				require.Equal(t, "keep-alive", getHeaderRaw(req.Header, "Connection"))
				require.Equal(t, "gzip, deflate, br, zstd", req.Header.Get("Accept-Encoding"))
				require.Equal(t, "2023-06-01", getHeaderRaw(req.Header, "anthropic-version"))
				require.Empty(t, getHeaderRaw(req.Header, "x-stainless-helper-method"))
				require.Empty(t, getHeaderRaw(req.Header, "sec-fetch-mode"))
				metadata := ParseMetadataUserID(gjson.GetBytes(wire, "metadata.user_id").String())
				require.NotNil(t, metadata)
				require.Equal(t, metadata.SessionID, getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
				// Preserve the existing account-scoped session derivation on every request.
				require.Equal(t, "53238107-9677-4479-adac-dbd2caadf16c", metadata.SessionID)
				require.Equal(t, fp.ClientID, metadata.DeviceID)
				require.Contains(t, gjson.GetBytes(wire, "system.0.text").String(), "cc_version="+claudeCCHVersion+".")
				require.EqualValues(t, len(wire), req.ContentLength)
				requestID := getHeaderRaw(req.Header, "x-client-request-id")
				_, err = uuid.Parse(requestID)
				require.NoError(t, err)
				require.NotEqual(t, previousRequestID, requestID)
				previousRequestID = requestID
				promptID := getHeaderRaw(req.Header, "x-claude-code-prompt-id")
				_, err = uuid.Parse(promptID)
				require.NoError(t, err)
				if previousPromptID != "" {
					require.Equal(t, previousPromptID, promptID, "retries retain prompt identity")
				}
				previousPromptID = promptID
				require.Equal(t, "main", getHeaderRaw(req.Header, "x-claude-code-request-class"))
				require.Regexp(t, `; cch=[0-9a-f]{5};`, gjson.GetBytes(wire, "system.0.text").String())
			}
		})
	}
}
