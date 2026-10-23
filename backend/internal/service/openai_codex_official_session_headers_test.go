//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const (
	migClientSession = "0199a1f2-3d4e-7f80-9a1b-2c3d4e5f6071"
	migClientThread  = "0199a1f2-3d4e-7f80-9a1b-2c3d4e5f6072"
)

// 带稳定 namespace 的账号：有 chatgpt_account_id 才会进入迁移。
func newMigrationAccount(t *testing.T, id int64, chatgptAccountID string) *Account {
	t.Helper()
	account := newTestOAuthAccount(id, nil)
	account.Credentials = map[string]any{"chatgpt_account_id": chatgptAccountID}
	require.NotEmpty(t, codexAccountIdentityNamespace(account))
	return account
}

func migrationBody(t *testing.T, session, thread string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"model": "gpt-6-astra",
		"client_metadata": map[string]any{
			"session_id": session,
			"thread_id":  thread,
		},
	})
	require.NoError(t, err)
	return raw
}

// 把 body 过一遍真实的作用域化流程，拿到「已作用域化的最终值」。
func scopedMigrationBody(t *testing.T, account *Account, apiKeyID int64, session, thread string) []byte {
	t.Helper()
	body := migrationBody(t, session, thread)
	scoped, changed, err := applyCodexAccountIdentityClientMetadataRaw(body, account, apiKeyID)
	require.NoError(t, err)
	require.True(t, changed)
	return scoped
}

// 核心：header 与 body 同值，且不是二次派生的结果。
func TestOfficialSessionHeaders_ReusesScopedBodyValue(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")
	scoped := scopedMigrationBody(t, account, 77, migClientSession, migClientThread)

	h := make(http.Header)
	applyCodexOfficialSessionHeadersFromScopedBody(h, account, scoped)

	bodySession := gjson.GetBytes(scoped, "client_metadata.session_id").String()
	bodyThread := gjson.GetBytes(scoped, "client_metadata.thread_id").String()

	require.Equal(t, bodySession, h.Get("session-id"), "header 必须与 body 同值")
	require.Equal(t, bodyThread, h.Get("thread-id"))
	require.Equal(t, bodyThread, h.Get("x-client-request-id"), "官方 HTTP 上 x-client-request-id 承载 thread_id")

	// 不得等于客户端原始值
	require.NotEqual(t, migClientSession, h.Get("session-id"))
	require.NotEqual(t, migClientThread, h.Get("thread-id"))

	// 形状：UUID（方案 B 承诺的是统一 UUID 表示，不是官方 v7 时序）
	require.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, h.Get("session-id"))

	// 旧的下划线头在本步骤里不由该函数负责，必须保持未设置
	require.Empty(t, h.Get("session_id"))
	require.Empty(t, h.Get("conversation_id"))
}

func TestCodexFinalProjectionActualWSHeadersAndCallerSemantics(t *testing.T) {
	for _, namespaced := range []bool{true, false} {
		account := newTestOAuthAccount(1, nil)
		if namespaced {
			account = newMigrationAccount(t, 1, "acct-A")
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
		c.Set("api_key", &APIKey{ID: 77})
		for key, value := range map[string]string{
			"session-id": "affinity", "thread-id": "child", "x-codex-parent-thread-id": "parent",
			"x-codex-installation-id": "install", "accept-language": "zh-CN",
			"x-openai-subagent": "review", "x-openai-memgen-request": "false", "x-responsesapi-include-timing-metrics": "true",
		} {
			c.Request.Header.Set(key, value)
		}
		svc := &OpenAIGatewayService{}
		h, _, err := svc.buildOpenAIWSHeaders(context.Background(), c, account, "token", OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, false, "", "", "cache", "", "", nil)
		require.NoError(t, err)
		// Exercise the final outbound sequence: preserve header-only metadata in
		// the real frame before removing legacy wire headers.
		frame, _, err := normalizeCodexRequestMetadata([]byte("{}"), h, account, 77, false)
		require.NoError(t, err)
		projectCodexOutboundHeaders(h, account, true)
		require.Equal(t, "review", h.Get("x-openai-subagent"))
		require.Equal(t, "false", h.Get("x-openai-memgen-request"))
		require.Equal(t, "true", h.Get("x-responsesapi-include-timing-metrics"))
		require.Equal(t, openAIWSBetaV2Value, h.Get("OpenAI-Beta"))
		if namespaced {
			require.Equal(t, scopeCodexAccountIdentityValue(account, 77, "installation", "install"), gjson.GetBytes(frame, "client_metadata.x-codex-installation-id").String())
			require.Equal(t, scopeCodexAccountIdentityValue(account, 77, "thread", "parent"), h.Get("x-codex-parent-thread-id"))
			for _, key := range []string{"session_id", "conversation_id", "x-codex-installation-id", "accept-language"} {
				require.Empty(t, h.Get(key), key)
			}
			require.Equal(t, scopeCodexAccountIdentityValue(account, 77, "session", "affinity"), h.Get("session-id"))
		} else {
			require.Empty(t, h.Get("session-id"))
			require.Empty(t, h.Get("x-codex-parent-thread-id"))
			require.NotEmpty(t, h.Get("session_id"))
		}
		c.Request.Header.Del("x-openai-subagent")
		c.Request.Header.Del("x-openai-memgen-request")
		c.Request.Header.Del("x-responsesapi-include-timing-metrics")
		h, _, err = svc.buildOpenAIWSHeaders(context.Background(), c, account, "token", OpenAIWSProtocolDecision{}, false, "", "", "cache", "", "", nil)
		require.NoError(t, err)
		for _, key := range []string{"x-openai-subagent", "x-openai-memgen-request", "x-responsesapi-include-timing-metrics"} {
			require.Empty(t, h.Get(key), "must not invent caller intent")
		}
	}
}

func TestCodexFinalProjectionHTTPGuardsAndCompactionBeta(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")
	for _, session := range []bool{false, true} {
		h := http.Header{"session_id": {"legacy"}, "CONVERSATION_ID": {"legacy"}, "accept-language": {"en"}, "INSTALLATION_ID": {"raw"}}
		h.Set("OpenAI-Beta", "responses_websockets=2026-02-06, responses=experimental, future=1")
		h.Set("x-codex-beta-features", "remote_compaction_v2")
		if session {
			applyCodexOfficialSessionHeaderFallback(h, account, 77, "session")
		}
		projectCodexOutboundHeaders(h, account, false)
		require.Equal(t, "future=1", h.Get("OpenAI-Beta"))
		require.Equal(t, "remote_compaction_v2", h.Get("x-codex-beta-features"))
		if session {
			for key := range h {
				require.NotContains(t, []string{"session_id", "conversation_id", "accept-language", "installation_id"}, strings.ToLower(key))
			}
			require.Empty(t, h.Get("thread-id"), "projection must not fabricate thread from affinity")
		} else {
			require.Contains(t, h, "session_id", "migration cannot remove the only session")
		}
	}
	h := make(http.Header)
	h.Set("session_id", "legacy")
	h.Set("session-id", "unscoped")
	projectCodexOutboundHeaders(h, newTestOAuthAccount(2, nil), false)
	require.Equal(t, "legacy", h.Get("session_id"))
}

// 不同 API Key → 不同值。
func TestOfficialSessionHeaders_IsolatedPerAPIKey(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")

	first := make(http.Header)
	applyCodexOfficialSessionHeadersFromScopedBody(first, account, scopedMigrationBody(t, account, 77, migClientSession, migClientThread))
	second := make(http.Header)
	applyCodexOfficialSessionHeadersFromScopedBody(second, account, scopedMigrationBody(t, account, 88, migClientSession, migClientThread))

	require.NotEmpty(t, first.Get("session-id"))
	require.NotEqual(t, first.Get("session-id"), second.Get("session-id"))
}

// 不同上游账号 → 不同值（scheduler failover 后不得复用同一身份）。
func TestOfficialSessionHeaders_IsolatedPerUpstreamAccount(t *testing.T) {
	accountA := newMigrationAccount(t, 1, "acct-A")
	accountB := newMigrationAccount(t, 2, "acct-B")

	first := make(http.Header)
	applyCodexOfficialSessionHeadersFromScopedBody(first, accountA, scopedMigrationBody(t, accountA, 77, migClientSession, migClientThread))
	second := make(http.Header)
	applyCodexOfficialSessionHeadersFromScopedBody(second, accountB, scopedMigrationBody(t, accountB, 77, migClientSession, migClientThread))

	require.NotEqual(t, first.Get("session-id"), second.Get("session-id"))
}

// 同一会话连续多轮 → 稳定不变。
func TestOfficialSessionHeaders_StableAcrossTurns(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")

	var previous string
	for turn := range 3 {
		h := make(http.Header)
		applyCodexOfficialSessionHeadersFromScopedBody(h, account, scopedMigrationBody(t, account, 77, migClientSession, migClientThread))
		if turn == 0 {
			previous = h.Get("session-id")
			require.NotEmpty(t, previous)
			continue
		}
		require.Equal(t, previous, h.Get("session-id"))
	}
}

// namespace 缺失的账号本轮不迁移：什么都不写，绝不输出客户端原始值。
func TestOfficialSessionHeaders_SkipsAccountWithoutNamespace(t *testing.T) {
	account := newTestOAuthAccount(1, nil) // 无 chatgpt_account_id、无指纹种子
	require.Empty(t, codexAccountIdentityNamespace(account))

	// namespace 为空时作用域化整体跳过，body 里留的就是客户端原始值。
	body := migrationBody(t, migClientSession, migClientThread)
	_, changed, err := applyCodexAccountIdentityClientMetadataRaw(body, account, 77)
	require.NoError(t, err)
	require.False(t, changed, "namespace 为空时不作用域化")

	h := make(http.Header)
	applyCodexOfficialSessionHeadersFromScopedBody(h, account, body)
	require.Empty(t, h.Get("session-id"), "不得把客户端原始 session 写到出站头")
	require.Empty(t, h.Get("thread-id"))
	require.Empty(t, h.Get("x-client-request-id"))
}

// 取不到最终值时什么都不写，不回退到旧的 16-hex、也不回退到原始值。
func TestOfficialSessionHeaders_NoFallbackWhenBodyLacksClientMetadata(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")

	for name, body := range map[string][]byte{
		"无 client_metadata":  []byte(`{"model":"gpt-6-astra"}`),
		"空对象":                []byte(`{"client_metadata":{}}`),
		"session 为空串":        []byte(`{"client_metadata":{"session_id":""}}`),
		"non-string session": []byte(`{"client_metadata":{"session_id":123,"thread_id":true}}`),
		"非法 JSON":            []byte(`not json`),
		"空 body":             nil,
	} {
		t.Run(name, func(t *testing.T) {
			h := make(http.Header)
			applyCodexOfficialSessionHeadersFromScopedBody(h, account, body)
			require.Empty(t, h.Get("session-id"))
		})
	}
}

// 只有 session 没有 thread 时不得把 thread 等同于 session。
func TestOfficialSessionHeaders_DoesNotAliasThreadToSession(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")
	body := []byte(`{"client_metadata":{"session_id":"` + migClientSession + `"}}`)
	scoped, _, err := applyCodexAccountIdentityClientMetadataRaw(body, account, 77)
	require.NoError(t, err)

	h := make(http.Header)
	applyCodexOfficialSessionHeadersFromScopedBody(h, account, scoped)
	require.NotEmpty(t, h.Get("session-id"))
	require.Empty(t, h.Get("thread-id"), "没有真实 thread 信号时不写 thread-id")
	require.Empty(t, h.Get("x-client-request-id"))
}

// WS 握手侧：从已作用域化的 turn-metadata 头读回同一个值。
func TestOfficialSessionHeaders_FromScopedTurnMetadata(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")

	h := make(http.Header)
	raw, err := marshalCodexTurnMetadata(map[string]any{
		"session_id": migClientSession,
		"thread_id":  migClientThread,
	})
	require.NoError(t, err)
	h.Set(openAIWSTurnMetadataHeader, string(raw))

	// 真实顺序：先 applyCodexAccountIdentityHeaders 作用域化，再补官方头。
	applyCodexAccountIdentityHeaders(h, account, 77)
	applyCodexOfficialSessionHeadersFromScopedTurnMetadata(h, account)

	metadata, err := decodeCodexTurnMetadata(h.Get(openAIWSTurnMetadataHeader))
	require.NoError(t, err)
	require.Equal(t, metadata["session_id"], h.Get("session-id"), "握手头必须与 turn-metadata 同值")
	require.Equal(t, metadata["thread_id"], h.Get("thread-id"))
	require.NotEqual(t, migClientSession, h.Get("session-id"))
}

// 回归守卫：补齐官方头不得改变最终 prompt_cache_key（约束 3）。
// 覆盖「缓存键与 body session 相等 / 不等 / session 缺失」三种情况。
func TestOfficialSessionHeaders_DoesNotChangePromptCacheKey(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")

	for name, body := range map[string][]byte{
		"相等": []byte(`{"client_metadata":{"session_id":"` + migClientSession + `"},"prompt_cache_key":"` + migClientSession + `"}`),
		"不等": []byte(`{"client_metadata":{"session_id":"` + migClientSession + `"},"prompt_cache_key":"other-key"}`),
		"缺失": []byte(`{"prompt_cache_key":"` + migClientSession + `"}`),
	} {
		t.Run(name, func(t *testing.T) {
			scoped, _, err := applyCodexAccountIdentityClientMetadataRaw(body, account, 77)
			require.NoError(t, err)
			before := gjson.GetBytes(scoped, "prompt_cache_key").String()
			require.NotEmpty(t, before)

			h := make(http.Header)
			applyCodexOfficialSessionHeadersFromScopedBody(h, account, scoped)

			after := gjson.GetBytes(scoped, "prompt_cache_key").String()
			require.Equal(t, before, after, "补齐官方头不得触碰 prompt_cache_key")
		})
	}
}

// 非 Codex 协议账号不受影响。
func TestOfficialSessionHeaders_SkipsNonCodexAccount(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	h := make(http.Header)
	applyCodexOfficialSessionHeadersFromScopedBody(h, account, []byte(`{"client_metadata":{"session_id":"x"}}`))
	require.Empty(t, h.Get("session-id"))
}

// Exercise the final builders: helper-only tests do not catch whitelist loss,
// a missing body identity, or unscoped headers escaping through WS handshakes.
func TestOfficialSessionHeaders_FinalBuilders(t *testing.T) {
	for _, path := range []string{"responses", "compact", "passthrough", "ws"} {
		for _, namespaced := range []bool{true, false} {
			name := path + "/without_namespace"
			if namespaced {
				name = path + "/with_namespace"
			}
			t.Run(name, func(t *testing.T) {
				account := newTestOAuthAccount(1, nil)
				if namespaced {
					account = newMigrationAccount(t, 1, "acct-A")
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				url := "/v1/responses"
				if path == "compact" {
					url += "/compact"
				}
				c.Request = httptest.NewRequest(http.MethodPost, url, nil)
				c.Set("api_key", &APIKey{ID: 77})
				c.Request.Header.Set("session-id", migClientSession)
				c.Request.Header.Set("thread-id", migClientThread)
				c.Request.Header.Set("x-client-request-id", migClientThread)
				svc := &OpenAIGatewayService{}
				body := []byte("{\"model\":\"gpt-5.6-codex\",\"input\":[{\"role\":\"user\",\"content\":\"hello\"}]}")
				var headers http.Header
				if path == "ws" {
					h, _, err := svc.buildOpenAIWSHeaders(context.Background(), c, account, "test-token", OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, true, "", "", migClientSession, "", "", nil)
					require.NoError(t, err)
					headers = h
				} else {
					var req *http.Request
					var err error
					if path == "passthrough" {
						req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test-token")
					} else {
						req, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "test-token", true, migClientSession, true)
					}
					require.NoError(t, err)
					headers = req.Header
				}
				if !namespaced {
					for _, key := range []string{"session-id", "thread-id", "x-client-request-id"} {
						require.Empty(t, headers.Get(key), "unscoped identity must not escape: %s", key)
					}
					require.Equal(t, isolateOpenAISessionID(77, migClientSession), headers.Get("session_id"))
					return
				}
				require.Equal(t, scopeCodexAccountIdentityValue(account, 77, "session", migClientSession), headers.Get("session-id"))
				require.Equal(t, scopeCodexAccountIdentityValue(account, 77, "thread", migClientThread), headers.Get("thread-id"))
				require.Equal(t, headers.Get("thread-id"), headers.Get("x-client-request-id"))
			})
		}
	}
}

func TestOfficialSessionHeaders_ForwardPreservesFinalIdentityAndCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"responses", "passthrough", "compact", "compact_passthrough"} {
		for _, variant := range []string{"same_cache", "different_cache", "no_metadata", "embedded_metadata", "explicit_affinity"} {
			t.Run(path+"/"+variant, func(t *testing.T) {
				for _, owner := range []struct {
					key     int64
					account string
				}{{77, "acct-A"}, {88, "acct-A"}, {77, "acct-B"}} {
					account := newMigrationAccount(t, 1, owner.account)
					account.Credentials["access_token"] = "test-token"
					account.Extra = map[string]any{"openai_passthrough": strings.Contains(path, "passthrough")}
					cache := migClientSession
					if variant == "different_cache" || variant == "explicit_affinity" {
						cache = "separate-cache-key"
					}
					metadata := map[string]any{"session_id": migClientSession, "thread_id": migClientThread}
					turnMetadata, err := marshalCodexTurnMetadata(metadata)
					require.NoError(t, err)
					metadata[openAIWSTurnMetadataHeader] = string(turnMetadata)
					if variant == "embedded_metadata" {
						delete(metadata, "session_id")
						delete(metadata, "thread_id")
					}
					payload := map[string]any{"model": "gpt-5.6-codex", "instructions": "test", "input": "hello", "stream": false, "prompt_cache_key": cache}
					if variant != "no_metadata" {
						payload["client_metadata"] = metadata
					}
					body, err := json.Marshal(payload)
					require.NoError(t, err)
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					url := "/v1/responses"
					if strings.HasPrefix(path, "compact") {
						url += "/compact"
					}
					c.Request = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
					c.Request.Header.Set("User-Agent", "codex_cli_rs/0.159.3")
					c.Set("api_key", &APIKey{ID: owner.key})
					if variant == "explicit_affinity" {
						c.Request.Header.Set("session-id", cache)
					}
					upstream := &httpUpstreamRecorder{resp: &http.Response{
						StatusCode: http.StatusBadRequest,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader("{\"error\":{\"type\":\"invalid_request_error\",\"message\":\"stop after request capture\"}}")),
					}}
					svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
					_, err = svc.Forward(context.Background(), c, account, body)
					require.Error(t, err)
					require.Len(t, upstream.requests, 1)
					require.Equal(t, strings.Contains(path, "passthrough"), c.GetBool("openai_passthrough"), "must exercise the intended production path")
					wire := upstream.lastReq.Header
					for _, name := range []string{"session_id", "conversation_id", "x-codex-installation-id", "accept-language"} {
						require.Empty(t, wire.Get(name), name)
					}
					require.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, wire.Get("thread-id"))
					wantSession := migClientSession
					if variant == "explicit_affinity" || variant == "no_metadata" {
						wantSession = cache
					}
					require.Equal(t, scopeCodexAccountIdentityValue(account, owner.key, "session", wantSession), wire.Get("session-id"))
					if variant != "no_metadata" {
						wantThread := scopeCodexAccountIdentityValue(account, owner.key, "thread", migClientThread)
						require.Equal(t, wantThread, wire.Get("thread-id"))
						require.Equal(t, wantThread, wire.Get("x-client-request-id"))
						bodySession, bodyThread := codexScopedBodySessionIdentity(upstream.lastBody)
						require.Equal(t, scopeCodexAccountIdentityValue(account, owner.key, "session", migClientSession), bodySession)
						require.Equal(t, wantThread, bodyThread)
						embedded, err := decodeCodexTurnMetadata(gjson.GetBytes(upstream.lastBody, "client_metadata.x-codex-turn-metadata").String())
						require.NoError(t, err)
						require.Equal(t, bodySession, embedded["session_id"])
					} else {
						require.NotEmpty(t, wire.Get("thread-id"), "metadata defaults include an independent thread")
						require.NotEqual(t, wire.Get("session-id"), wire.Get("thread-id"), "cache affinity is not a thread identity")
						require.Equal(t, wire.Get("thread-id"), wire.Get("x-client-request-id"))
						require.Equal(t, wire.Get("thread-id"), gjson.Get(wire.Get(openAIWSTurnMetadataHeader), "thread_id").String())
						if !strings.HasPrefix(path, "compact") {
							_, bodyThread := codexScopedBodySessionIdentity(upstream.lastBody)
							require.Equal(t, wire.Get("thread-id"), bodyThread)
						}
					}
					kind := "prompt-cache"
					if variant != "no_metadata" && variant != "embedded_metadata" && cache == migClientSession {
						kind = "session"
					}
					wantCache := scopeCodexAccountIdentityValue(account, owner.key, kind, cache)
					if path == "compact" {
						wantCache = cache // Preserve the existing compact cache-key behavior.
					}
					require.Equal(t, wantCache, gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String())
					require.Equal(t, "hello", gjson.GetBytes(body, "input").String(), "caller body must remain immutable")
				}
			})
		}
	}
}

func TestOfficialSessionHeaders_WSUsesScopedFirstFrameBeforeCacheFallback(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")
	scoped := scopedMigrationBody(t, account, 77, migClientSession, migClientThread)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Set("api_key", &APIKey{ID: 77})
	svc := &OpenAIGatewayService{}
	h, _, err := svc.buildOpenAIWSHeaders(context.Background(), c, account, "test-token",
		OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
		true, "", "", "different-cache-key", "", "", scoped)
	require.NoError(t, err)
	require.Equal(t, gjson.GetBytes(scoped, "client_metadata.session_id").String(), h.Get("session-id"))
	require.Equal(t, gjson.GetBytes(scoped, "client_metadata.thread_id").String(), h.Get("thread-id"))
	require.Equal(t, h.Get("thread-id"), h.Get("x-client-request-id"))
	// Recovery fallback without a frame must still send a scoped official ID.
	h, _, err = svc.buildOpenAIWSHeaders(context.Background(), c, account, "test-token",
		OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
		true, "", "", migClientSession, "", "", nil)
	require.NoError(t, err)
	require.Equal(t, scopeCodexAccountIdentityValue(account, 77, "session", migClientSession), h.Get("session-id"))
	require.Empty(t, h.Get("thread-id"))
}

func TestOfficialSessionHeaders_CompatibilityBridgesPreserveOfficialIdentity(t *testing.T) {
	for _, path := range []string{"chat/completions", "messages"} {
		t.Run(path, func(t *testing.T) {
			account := newMigrationAccount(t, 1, "acct-A")
			account.Credentials["access_token"] = "test-token"
			body := []byte(`{"model":"gpt-5.6-codex","messages":[{"role":"user","content":"hello"}],"max_tokens":32,"stream":false}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+path, bytes.NewReader(body))
			c.Set("api_key", &APIKey{ID: 77})
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"stop after request capture"}}`)),
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			var err error
			if path == "messages" {
				_, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, migClientSession, "")
			} else {
				_, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, migClientSession, "")
			}
			require.Error(t, err)
			require.Len(t, upstream.requests, 1)
			h := upstream.lastReq.Header
			require.Equal(t, scopeCodexAccountIdentityValue(account, 77, "session", migClientSession), h.Get("session-id"))
			for _, name := range []string{"session_id", "conversation_id", "x-codex-installation-id", "accept-language"} {
				require.Empty(t, h.Get(name), "compatibility bridges must also finalize outbound headers: %s", name)
			}
			require.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, h.Get("thread-id"))
			require.NotEqual(t, h.Get("session-id"), h.Get("thread-id"))
			require.Equal(t, h.Get("thread-id"), gjson.GetBytes(upstream.lastBody, "client_metadata.thread_id").String())
		})
	}
}

func TestOfficialSessionHeaders_APIKeyRelayKeepsClientValues(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		for _, key := range []string{"session-id", "thread-id", "x-client-request-id"} {
			c.Request.Header.Set(key, "original-"+key)
		}
		account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
		svc := &OpenAIGatewayService{}
		body := []byte(`{"model":"gpt-5.6-codex","input":"hello"}`)
		var req *http.Request
		var err error
		if passthrough {
			req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test-key")
		} else {
			req, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "test-key", false, "", false)
		}
		require.NoError(t, err)
		for _, key := range []string{"session-id", "thread-id", "x-client-request-id"} {
			require.Equal(t, "original-"+key, req.Header.Get(key))
		}
	}
}

func TestOfficialSessionHeaders_WSRetryKeepsSourceMetadataImmutable(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")
	rawTurn, err := marshalCodexTurnMetadata(map[string]any{"session_id": migClientSession, "thread_id": migClientThread})
	require.NoError(t, err)
	request := map[string]any{"client_metadata": map[string]any{"session_id": scopeCodexAccountIdentityValue(account, 77, "session", migClientSession), "unchanged": "client-marker"}}
	before, err := json.Marshal(request)
	require.NoError(t, err)
	svc := &OpenAIGatewayService{}
	for range 2 {
		payload := svc.buildOpenAIWSCreatePayload(request, account)
		setOpenAIWSTurnMetadata(payload, string(rawTurn))
		metadata := payload["client_metadata"].(map[string]any)
		require.True(t, applyCodexAccountIdentityEmbeddedMetadata(metadata, account, 77))
		turn, err := decodeCodexTurnMetadata(metadata[openAIWSTurnMetadataHeader].(string))
		require.NoError(t, err)
		require.Equal(t, metadata["session_id"], turn["session_id"])
		after, err := json.Marshal(request)
		require.NoError(t, err)
		require.Equal(t, before, after, "each retry must start from the original identity snapshot")
	}
}
