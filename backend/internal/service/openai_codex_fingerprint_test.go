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
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const testCodexFingerprintSeed = "11111111-1111-4111-8111-111111111111"

func newTestOAuthAccount(id int64, extra map[string]any) *Account {
	if codexFingerprintModeRequiresSeed(codexFingerprintModeFromExtra(extra)) {
		if extra == nil {
			extra = make(map[string]any)
		}
		if _, exists := extra[codexFingerprintSeedExtraKey]; !exists {
			extra[codexFingerprintSeedExtraKey] = testCodexFingerprintSeed
		}
	}
	return &Account{
		ID:       id,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    extra,
	}
}

// --- deriveStableUUIDv4 ---

func TestDeriveStableUUIDv4_Deterministic(t *testing.T) {
	a := deriveStableUUIDv4("test-seed-1")
	b := deriveStableUUIDv4("test-seed-1")
	assert.Equal(t, a, b, "同一种子应返回相同结果")
}

func TestCodexFingerprintCallerIsolationAndScopedWindows(t *testing.T) {
	for _, mode := range []string{"session", "full"} {
		t.Run(mode, func(t *testing.T) {
			account := newTestOAuthAccount(1, map[string]any{codexFingerprintModeExtraKey: mode})
			account.Credentials = map[string]any{"chatgpt_account_id": "acct-A"}
			ids := resolveCodexFingerprintIDsForCaller(account, nil, 77)
			other := resolveCodexFingerprintIDsForCaller(account, nil, 88)
			repeat := resolveCodexFingerprintIDsForCaller(account, nil, 77)
			require.NotEqual(t, ids.threadID, other.threadID, "missing headers must still isolate callers")
			require.Equal(t, ids.threadID, repeat.threadID)
			require.Equal(t, ids.sessionID, other.sessionID, "explicit opt-in converges session only")
			otherAccount := *account
			otherAccount.Credentials = map[string]any{"chatgpt_account_id": "acct-B"}
			require.NotEqual(t, ids.threadID, resolveCodexFingerprintIDsForCaller(&otherAccount, nil, 77).threadID, "credential failover must isolate even a copied convergence seed")
			original := map[string]any{"client_metadata": map[string]any{
				"session_id": "body-session", "thread_id": "child-thread", "turn_id": "turn",
				"x-codex-window-id": "child-thread:23", "window_id": "child-thread:23",
				openAIWSTurnMetadataHeader: `{"session_id":"body-session","thread_id":"child-thread","window_id":"child-thread:23","window_number":23,"parent_thread_id":"parent","huge":900719925474099312345}`,
			}, "prompt_cache_key": "body-session"}
			require.True(t, applyCodexAccountIdentityClientMetadataMap(original, account, 77))
			cm := original["client_metadata"].(map[string]any)
			scopedSession := cm["session_id"].(string)
			scopedThread := cm["thread_id"].(string)
			scopedTurn := cm["turn_id"].(string)
			h := make(http.Header)
			h.Set("session-id", scopeCodexAccountIdentityValue(account, 77, "session", "explicit-affinity"))
			h.Set(openAIWSTurnMetadataHeader, cm[openAIWSTurnMetadataHeader].(string))
			affinity := h.Get("session-id")
			require.True(t, applyCodexFingerprintClientMetadata(original, ids))
			applyCodexFingerprintHeaders(h, ids)
			require.Equal(t, affinity, h.Get("session-id"))
			require.Equal(t, scopedThread, h.Get("thread-id"))
			require.Equal(t, scopedThread+":23", h.Get("x-codex-window-id"))
			require.Equal(t, h.Get("thread-id"), cm["thread_id"])
			require.Equal(t, scopedTurn, cm["turn_id"], "preserve B-scheme scoped v4 turn identity")
			require.Equal(t, h.Get("x-codex-window-id"), cm["window_id"])
			require.Equal(t, ids.sessionID, original["prompt_cache_key"])
			require.NotEqual(t, scopedSession, ids.sessionID)
			embedded, err := decodeCodexTurnMetadata(cm[openAIWSTurnMetadataHeader].(string))
			require.NoError(t, err)
			require.Equal(t, scopeCodexAccountIdentityValue(account, 77, "thread", "parent"), embedded["parent_thread_id"])
			require.Equal(t, json.Number("23"), embedded["window_number"])
			require.Equal(t, json.Number("900719925474099312345"), embedded["huge"])
		})
	}
}

func TestCodexFingerprintCallerHeaderOnlyAndRawParity(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{codexFingerprintModeExtraKey: "session"})
	account.Credentials = map[string]any{"chatgpt_account_id": "acct-A"}
	client := make(http.Header)
	client.Set("thread-id", "thread")
	client.Set("session-id", "session")
	client.Set("x-codex-window-id", "thread:9")
	client.Set(openAIWSTurnMetadataHeader, `{"thread_id":"thread","turn_id":"turn","window_id":"thread:9","window_number":9,"turn_started_at_unix_ms":1700000000123}`)
	ids := resolveCodexFingerprintIDsForCaller(account, client, 77)
	original := []byte(`{"client_metadata":{"extra":900719925474099312345},"input":"hi"}`)
	body, changed, err := applyCodexFingerprintClientMetadataRaw(original, ids)
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, string(body), "900719925474099312345")
	h := client.Clone()
	applyCodexAccountIdentityHeaders(h, account, 77)
	applyCodexFingerprintHeaders(h, ids)
	require.Equal(t, scopeCodexAccountIdentityValue(account, 77, "thread", "thread"), h.Get("thread-id"))
	require.True(t, strings.HasSuffix(h.Get("x-codex-window-id"), ":9"))
	var payload map[string]any
	require.NoError(t, decodeOpenAIJSONUseNumber(body, &payload))
	cm := payload["client_metadata"].(map[string]any)
	require.Equal(t, cm["thread_id"], h.Get("thread-id"))
	require.Equal(t, cm["x-codex-window-id"], h.Get("x-codex-window-id"))
	require.Equal(t, scopeCodexAccountIdentityValue(account, 77, "turn", "turn"), cm["turn_id"])
	require.Equal(t, int64(1700000000123), ids.turnStartedAtUnixMs)
	var mapped map[string]any
	require.NoError(t, decodeOpenAIJSONUseNumber(original, &mapped))
	require.True(t, applyCodexFingerprintClientMetadata(mapped, resolveCodexFingerprintIDsForCaller(account, client, 77)))
	require.Equal(t, payload, mapped, "map and raw paths capture the same real headers")
	for range 2 {
		retried, _, err := applyCodexFingerprintClientMetadataRaw(body, ids)
		require.NoError(t, err)
		require.Equal(t, body, retried, "projected body must not be scoped or captured again")
	}
	for _, mode := range []string{"session", "full"} {
		t.Run(mode+"/defaults_are_not_caller_identity", func(t *testing.T) {
			account.Extra[codexFingerprintModeExtraKey] = mode
			missing := resolveCodexFingerprintIDsForCaller(account, nil, 77)
			defaultBody, _, err := applyCodexFingerprintClientMetadataRaw(original, missing)
			require.NoError(t, err)
			require.False(t, missing.threadIdentityCaptured)
			retried, _, err := applyCodexFingerprintClientMetadataRaw(defaultBody, missing)
			require.NoError(t, err)
			require.Equal(t, defaultBody, retried)
			require.False(t, missing.threadIdentityCaptured, "retry defaults are not real identity either")
		})
	}
	for _, raw := range []string{"t:-1", "t:1.5", "t:18446744073709551616", "t:", ":1"} {
		_, _, ok := splitCodexWindowID(raw)
		require.False(t, ok, raw)
	}
}

// Exercise both production Forward branches and their actual builders with the
// same recorder as the passthrough tests. The upstream stops after capture.
func TestCodexFingerprintForwardMetadata(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		path := "normal"
		if passthrough {
			path = "passthrough"
		}
		for _, mode := range []string{"off", "device", "session", "full"} {
			for _, variant := range []string{"header_only", "independent_headers", "body_conflict", "partial_body", "flat_body"} {
				t.Run(path+"/"+mode+"/"+variant, func(t *testing.T) {
					account := newTestOAuthAccount(1, map[string]any{codexFingerprintModeExtraKey: mode, "openai_oauth_passthrough": passthrough})
					account.Credentials = map[string]any{"chatgpt_account_id": "metadata-account", "access_token": "test-token"}
					metadata := map[string]any{
						"session_id": "header-session", "thread_id": "header-thread", "turn_id": "header-turn",
						"window_id": "header-thread:9", "window_number": json.Number("9"),
						"turn_started_at_unix_ms": json.Number("9007199254740993"),
					}
					headerRaw, err := marshalCodexTurnMetadata(metadata)
					require.NoError(t, err)
					payload := map[string]any{"model": "gpt-5.6-codex", "instructions": "local test", "input": "hello", "stream": false, "prompt_cache_key": "independent-cache"}
					if variant == "body_conflict" || variant == "flat_body" {
						metadata["session_id"], metadata["thread_id"], metadata["turn_id"] = "body-session", "body-thread", "body-turn"
						metadata["window_id"], metadata["window_number"] = "body-thread:23", json.Number("23")
						metadata["turn_started_at_unix_ms"] = json.Number("1700000000123")
						if variant == "flat_body" {
							payload["client_metadata"] = metadata
						} else {
							bodyRaw, err := marshalCodexTurnMetadata(metadata)
							require.NoError(t, err)
							payload["client_metadata"] = map[string]any{openAIWSTurnMetadataHeader: string(bodyRaw)}
						}
					} else if variant == "partial_body" {
						metadata["thread_id"] = "body-thread"
						if mode == "session" || mode == "full" {
							metadata["window_id"] = "body-thread:9"
						}
						payload["client_metadata"] = map[string]any{"thread_id": "body-thread"}
					}
					body, err := json.Marshal(payload)
					require.NoError(t, err)
					var previousCallerThread string
					for _, caller := range []int64{77, 88} {
						var firstBody []byte
						var firstHeader http.Header
						for range 2 {
							c, _ := gin.CreateTestContext(httptest.NewRecorder())
							c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
							c.Request.Header.Set("User-Agent", "codex_cli_rs/0.159.3")
							c.Request.Header.Set(openAIWSTurnMetadataHeader, string(headerRaw))
							c.Set("api_key", &APIKey{ID: caller})
							if variant == "independent_headers" {
								c.Request.Header.Set("session-id", "independent-affinity")
								c.Request.Header.Set("thread-id", "header-thread")
								c.Request.Header.Set("x-codex-window-id", "header-thread:9")
							}
							originalHeaders := c.Request.Header.Clone()
							upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusBadRequest,
								Header: http.Header{"Content-Type": []string{"application/json"}},
								Body:   io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"stop after capture"}}`)),
							}}
							svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
							_, err := svc.Forward(context.Background(), c, account, body)
							require.Error(t, err)
							require.Len(t, upstream.requests, 1)
							require.Equal(t, originalHeaders, c.Request.Header, "raw caller headers remain immutable")
							cm := gjson.GetBytes(upstream.lastBody, "client_metadata")
							sent, err := decodeCodexTurnMetadata(cm.Get(openAIWSTurnMetadataHeader).String())
							require.NoError(t, err)
							wire, err := decodeCodexTurnMetadata(upstream.lastReq.Header.Get(openAIWSTurnMetadataHeader))
							require.NoError(t, err)
							for field, kind := range map[string]string{"thread_id": "thread", "turn_id": "turn", "window_id": "window"} {
								want := scopeCodexAccountIdentityValue(account, caller, kind, metadata[field].(string))
								require.Equal(t, want, sent[field], field)
								require.Equal(t, want, wire[field], field)
							}
							for _, field := range []string{"window_number", "turn_started_at_unix_ms"} {
								require.Equal(t, metadata[field], sent[field], field)
								require.Equal(t, sent[field], wire[field], field)
							}
							require.Equal(t, sent["thread_id"], cm.Get("thread_id").String())
							require.Equal(t, sent["turn_id"], cm.Get("turn_id").String())
							require.Equal(t, sent["window_id"], upstream.lastReq.Header.Get("x-codex-window-id"))
							require.Equal(t, scopeCodexAccountIdentityValue(account, caller, "prompt-cache", "independent-cache"), gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String())
							if variant == "independent_headers" {
								require.Equal(t, scopeCodexAccountIdentityValue(account, caller, "session", "independent-affinity"), upstream.lastReq.Header.Get("session-id"))
							}
							if firstBody != nil {
								require.JSONEq(t, string(firstBody), string(upstream.lastBody), "same raw request retry must preserve identity")
								require.Equal(t, firstHeader.Get(openAIWSTurnMetadataHeader), upstream.lastReq.Header.Get(openAIWSTurnMetadataHeader))
							} else {
								firstBody, firstHeader = upstream.lastBody, upstream.lastReq.Header.Clone()
							}
							if upstream.lastReq.GetBody != nil {
								replay, err := upstream.lastReq.GetBody()
								require.NoError(t, err)
								replayBody, err := io.ReadAll(replay)
								require.NoError(t, err)
								require.NoError(t, replay.Close())
								require.Equal(t, upstream.lastBody, replayBody)
							}
						}
						thread := firstHeader.Get("thread-id")
						require.NotEqual(t, previousCallerThread, thread)
						previousCallerThread = thread
					}
				})
			}
		}
	}
}

func TestDeriveStableUUIDv4_DifferentSeeds(t *testing.T) {
	a := deriveStableUUIDv4("seed-a")
	b := deriveStableUUIDv4("seed-b")
	assert.NotEqual(t, a, b, "不同种子应返回不同结果")
}

func TestDeriveStableUUIDv4_ValidFormat(t *testing.T) {
	result := deriveStableUUIDv4("test-seed")
	parsed, err := uuid.Parse(result)
	require.NoError(t, err, "应返回合法 UUID 格式")
	assert.Equal(t, uuid.Version(4), parsed.Version(), "应为 UUIDv4")
	assert.Equal(t, uuid.RFC4122, parsed.Variant(), "应为 RFC4122 变体")
}

// --- GetCodexFingerprintMode ---

func TestGetCodexFingerprintMode(t *testing.T) {
	tests := []struct {
		name     string
		account  *Account
		expected codexFingerprintMode
	}{
		{"nil 账号", nil, codexFingerprintOff},
		{"非 OAuth 账号", &Account{Platform: PlatformOpenAI, Type: "api_key"}, codexFingerprintOff},
		{"OpenAI setup token", &Account{Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Extra: map[string]any{codexFingerprintModeExtraKey: "session"}}, codexFingerprintSession},
		{"Anthropic setup token", &Account{Platform: PlatformAnthropic, Type: AccountTypeSetupToken, Extra: map[string]any{codexFingerprintModeExtraKey: "session"}}, codexFingerprintOff},
		// 收敛是显式 opt-in：缺省/空/非法一律 off（#5610）。存量账号普遍没有这个
		// extra 键，升级不得把它们静默切进收敛。
		{"无 extra 默认 off", newTestOAuthAccount(1, nil), codexFingerprintOff},
		{"空值默认 off", newTestOAuthAccount(1, map[string]any{codexFingerprintModeExtraKey: ""}), codexFingerprintOff},
		{"非法值默认 off", newTestOAuthAccount(1, map[string]any{codexFingerprintModeExtraKey: "invalid"}), codexFingerprintOff},
		{"显式 off", newTestOAuthAccount(1, map[string]any{codexFingerprintModeExtraKey: "off"}), codexFingerprintOff},
		{"device", newTestOAuthAccount(1, map[string]any{codexFingerprintModeExtraKey: "device"}), codexFingerprintDevice},
		{"session", newTestOAuthAccount(1, map[string]any{codexFingerprintModeExtraKey: "session"}), codexFingerprintSession},
		{"full", newTestOAuthAccount(1, map[string]any{codexFingerprintModeExtraKey: "full"}), codexFingerprintFull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.account.GetCodexFingerprintMode())
		})
	}
}

// --- resolveConvergedInstallationID ---

func TestResolveConvergedInstallationID_UsesDeviceID(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{"openai_device_id": "real-device-id"})
	assert.Equal(t, "real-device-id", resolveConvergedInstallationID(account, testCodexFingerprintSeed))
}

func TestResolveConvergedInstallationID_DerivesFromSeed(t *testing.T) {
	account := newTestOAuthAccount(42, nil)
	result := resolveConvergedInstallationID(account, testCodexFingerprintSeed)
	_, err := uuid.Parse(result)
	require.NoError(t, err, "派生值应为合法 UUID")
	assert.Equal(t, result, resolveConvergedInstallationID(account, testCodexFingerprintSeed), "确定性")
}

func TestResolveConvergedInstallationID_DifferentSeeds(t *testing.T) {
	account := newTestOAuthAccount(1, nil)
	a := resolveConvergedInstallationID(account, testCodexFingerprintSeed)
	b := resolveConvergedInstallationID(account, "22222222-2222-4222-8222-222222222222")
	assert.NotEqual(t, a, b)
}

// --- resolveConvergedThreadID ---

func TestResolveConvergedThreadID_PerClientSession(t *testing.T) {
	a := resolveConvergedThreadID(testCodexFingerprintSeed, "session-aaa")
	b := resolveConvergedThreadID(testCodexFingerprintSeed, "session-bbb")
	assert.NotEqual(t, a, b, "不同客户端 session 应得到不同 thread_id")
}

func TestResolveConvergedThreadID_Deterministic(t *testing.T) {
	a := resolveConvergedThreadID(testCodexFingerprintSeed, "session-aaa")
	b := resolveConvergedThreadID(testCodexFingerprintSeed, "session-aaa")
	assert.Equal(t, a, b, "同一客户端 session 应得到相同 thread_id")
}

func TestResolveConvergedThreadID_EmptySession(t *testing.T) {
	assert.Equal(t, "", resolveConvergedThreadID(testCodexFingerprintSeed, ""))
}

// --- off 模式：resolveCodexFingerprintIDsForCaller 返回 nil ---

func TestResolveCodexFingerprintIDsForCaller_ExplicitOff(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{codexFingerprintModeExtraKey: "off"})
	ids := resolveCodexFingerprintIDsForCaller(account, nil, 0)
	assert.Nil(t, ids, "显式 off 模式应返回 nil")
}

// 未显式配置的存量账号不得被收敛（#5610）：默认返回 nil，出站身份保持
// v0.1.175 之前的客户端原值。
func TestResolveCodexFingerprintIDsForCaller_DefaultIsOff(t *testing.T) {
	account := newTestOAuthAccount(1, nil)
	assert.Nil(t, resolveCodexFingerprintIDsForCaller(account, nil, 0), "无 extra 应视为 off")
}

// 管理员显式 opt-in 的账号行为不变。
func TestResolveCodexFingerprintIDsForCaller_ExplicitOptInHonored(t *testing.T) {
	for _, mode := range []string{"device", "session", "full"} {
		t.Run(mode, func(t *testing.T) {
			account := newTestOAuthAccount(1, map[string]any{codexFingerprintModeExtraKey: mode})
			ids := resolveCodexFingerprintIDsForCaller(account, nil, 0)
			require.NotNil(t, ids, "显式配置必须生效")
			assert.Equal(t, codexFingerprintMode(mode), ids.mode)
			assert.NotEmpty(t, ids.installationID)
		})
	}
}

func TestResolveCodexFingerprintIDsForCaller_EnabledModesRequireValidSeed(t *testing.T) {
	for _, tt := range []struct {
		name  string
		extra map[string]any
	}{
		{name: "missing", extra: map[string]any{codexFingerprintModeExtraKey: "device"}},
		{name: "missing with device override", extra: map[string]any{codexFingerprintModeExtraKey: "device", "openai_device_id": "real-device"}},
		{name: "blank", extra: map[string]any{codexFingerprintModeExtraKey: "session", codexFingerprintSeedExtraKey: ""}},
		{name: "uppercase", extra: map[string]any{codexFingerprintModeExtraKey: "full", codexFingerprintSeedExtraKey: "11111111-1111-4111-8111-AAAAAAAAAAAA"}},
		{name: "nil uuid", extra: map[string]any{codexFingerprintModeExtraKey: "device", codexFingerprintSeedExtraKey: "00000000-0000-0000-0000-000000000000"}},
		{name: "non string", extra: map[string]any{codexFingerprintModeExtraKey: "session", codexFingerprintSeedExtraKey: 123}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: tt.extra}
			require.Nil(t, resolveCodexFingerprintIDsForCaller(account, nil, 0))
		})
	}
}

// --- applyCodexFingerprintHeaders: off 模式 ---

func TestApplyCodexFingerprintHeaders_OffMode(t *testing.T) {
	h := http.Header{}
	h.Set("x-codex-installation-id", "original-install-id")
	h.Set("x-codex-window-id", "original-window-id")

	applyCodexFingerprintHeaders(h, nil)

	assert.Equal(t, "original-install-id", h.Get("x-codex-installation-id"), "nil ids 不改写")
	assert.Equal(t, "original-window-id", h.Get("x-codex-window-id"), "nil ids 不改写")
}

// --- applyCodexFingerprintHeaders: device 模式 ---

func TestApplyCodexFingerprintHeaders_DeviceMode(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		codexFingerprintModeExtraKey: "device",
		"openai_device_id":           "converged-device",
	})
	turnMetadata := `{"installation_id":"user-install","session_id":"user-session","sandbox":"seccomp"}`
	h := http.Header{}
	h.Set("x-codex-installation-id", "user-install")
	h.Set("x-codex-window-id", "user-window:0")
	h.Set("x-codex-turn-metadata", turnMetadata)

	ids := resolveCodexFingerprintIDsForCaller(account, nil, 0)
	applyCodexFingerprintHeaders(h, ids)

	assert.Equal(t, "converged-device", h.Get("x-codex-installation-id"), "installation_id 应收敛")
	assert.Equal(t, "user-window:0", h.Get("x-codex-window-id"), "device 模式不改写 window_id")

	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(h.Get("x-codex-turn-metadata")), &meta))
	assert.Equal(t, "converged-device", meta["installation_id"])
	assert.Equal(t, "user-session", meta["session_id"], "device 模式不改写 session_id")
	assert.Equal(t, "seccomp", meta["sandbox"], "非指纹字段保留原样")
}

// --- applyCodexFingerprintHeaders: session 模式 ---

func TestApplyCodexFingerprintHeaders_SessionMode(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		codexFingerprintModeExtraKey: "session",
	})
	clientHeaders := http.Header{}
	clientHeaders.Set("session-id", "client-session-aaa")

	turnMetadata := `{"installation_id":"user-install","session_id":"user-session","thread_id":"user-thread","turn_id":"user-turn","window_id":"user-thread:0","sandbox":"seccomp","thread_source":"user"}`
	h := http.Header{}
	h.Set("x-codex-installation-id", "user-install")
	h.Set("x-codex-window-id", "user-thread:0")
	h.Set("x-codex-turn-metadata", turnMetadata)
	h.Set("x-client-request-id", "user-thread")

	ids := resolveCodexFingerprintIDsForCaller(account, clientHeaders, 0)
	applyCodexFingerprintHeaders(h, ids)

	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	convergedInstall := resolveConvergedInstallationID(account, seed)
	convergedSession := resolveConvergedSessionID(seed)
	convergedThread := ids.threadID

	assert.Equal(t, convergedInstall, h.Get("x-codex-installation-id"))
	assert.Equal(t, convergedSession, h.Get("session-id"))
	assert.Equal(t, convergedSession, h.Get("session_id"), "下划线形式也应被改写")
	assert.Equal(t, convergedThread, h.Get("thread-id"))
	assert.Equal(t, convergedThread, h.Get("x-client-request-id"))
	assert.Equal(t, convergedThread+":0", h.Get("x-codex-window-id"))

	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(h.Get("x-codex-turn-metadata")), &meta))
	assert.Equal(t, convergedInstall, meta["installation_id"])
	assert.Equal(t, convergedSession, meta["session_id"])
	assert.Equal(t, convergedThread, meta["thread_id"])
	assert.Equal(t, "user-turn", meta["turn_id"], "caller-established turn identity must be preserved")
	assert.Equal(t, "seccomp", meta["sandbox"], "sandbox 保留原样")
	assert.Equal(t, "user", meta["thread_source"], "thread_source 保留原样")
}

// --- session 模式：不同客户端得到不同 thread ---

func TestApplyCodexFingerprintHeaders_SessionMode_DifferentClients(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		codexFingerprintModeExtraKey: "session",
	})

	makeTurnMeta := func() string {
		return `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0"}`
	}

	clientA := http.Header{}
	clientA.Set("session-id", "client-A")
	idsA := resolveCodexFingerprintIDsForCaller(account, clientA, 0)
	hA := http.Header{}
	hA.Set("x-codex-turn-metadata", makeTurnMeta())
	applyCodexFingerprintHeaders(hA, idsA)

	clientB := http.Header{}
	clientB.Set("session-id", "client-B")
	idsB := resolveCodexFingerprintIDsForCaller(account, clientB, 0)
	hB := http.Header{}
	hB.Set("x-codex-turn-metadata", makeTurnMeta())
	applyCodexFingerprintHeaders(hB, idsB)

	assert.Equal(t, hA.Get("session-id"), hB.Get("session-id"), "session_id 应相同")
	assert.NotEqual(t, hA.Get("thread-id"), hB.Get("thread-id"), "不同客户端 thread_id 应不同")
	assert.NotEqual(t, hA.Get("x-codex-window-id"), hB.Get("x-codex-window-id"), "不同客户端 window_id 应不同")
	assert.Equal(t, hA.Get("x-codex-installation-id"), hB.Get("x-codex-installation-id"))
}

// --- full 模式 ---

func TestApplyCodexFingerprintHeaders_FullMode(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		codexFingerprintModeExtraKey: "full",
	})
	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	convergedSession := resolveConvergedSessionID(seed)

	clientA := http.Header{}
	clientA.Set("session-id", "client-A")
	idsA := resolveCodexFingerprintIDsForCaller(account, clientA, 0)
	hA := http.Header{}
	hA.Set("x-codex-turn-metadata", `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0"}`)
	applyCodexFingerprintHeaders(hA, idsA)

	clientB := http.Header{}
	clientB.Set("session-id", "client-B")
	idsB := resolveCodexFingerprintIDsForCaller(account, clientB, 0)
	hB := http.Header{}
	hB.Set("x-codex-turn-metadata", `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0"}`)
	applyCodexFingerprintHeaders(hB, idsB)

	assert.NotEqual(t, hA.Get("thread-id"), hB.Get("thread-id"), "full mode must preserve distinct caller threads")
	assert.Equal(t, convergedSession, hA.Get("session-id"))
	assert.NotEqual(t, convergedSession, hA.Get("thread-id"))
	assert.NotEqual(t, hA.Get("x-codex-window-id"), hB.Get("x-codex-window-id"), "distinct threads must not share windows")
}

// --- H1 修复验证：头和体的 turn_id 一致性 ---

func TestFingerprintIDs_HeaderAndBody_TurnID_Consistent(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		codexFingerprintModeExtraKey: "session",
	})
	clientHeaders := http.Header{}
	clientHeaders.Set("session-id", "client-session-xyz")

	ids := resolveCodexFingerprintIDsForCaller(account, clientHeaders, 0)
	require.NotNil(t, ids)

	// Prepare headers, then rewrite the body first as production does.
	h := http.Header{}
	h.Set("x-codex-turn-metadata", `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0"}`)

	// Body and headers share the same captured caller identity snapshot.
	reqBody := map[string]any{
		"client_metadata": map[string]any{
			"x-codex-installation-id": "x",
			"session_id":              "x",
			"turn_id":                 "x",
			"x-codex-turn-metadata":   `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0"}`,
		},
	}
	applyCodexFingerprintClientMetadata(reqBody, ids)

	// 从头 turn-metadata JSON 提取 turn_id
	applyCodexFingerprintHeaders(h, ids)
	var headerMeta map[string]any
	require.NoError(t, json.Unmarshal([]byte(h.Get("x-codex-turn-metadata")), &headerMeta))
	headerTurnID, ok := headerMeta["turn_id"].(string)
	require.True(t, ok, "头 turn-metadata 应包含 string 类型的 turn_id")

	// 从体 client_metadata 提取 turn_id
	cm, ok := reqBody["client_metadata"].(map[string]any)
	require.True(t, ok, "请求体应包含 client_metadata")
	bodyTurnID, ok := cm["turn_id"].(string)
	require.True(t, ok, "体 client_metadata 应包含 string 类型的 turn_id")

	// 从体内嵌 turn-metadata JSON 提取 turn_id
	embeddedRaw, ok := cm["x-codex-turn-metadata"].(string)
	require.True(t, ok, "体 client_metadata 应包含 x-codex-turn-metadata 字符串")
	var bodyMeta map[string]any
	require.NoError(t, json.Unmarshal([]byte(embeddedRaw), &bodyMeta))
	bodyEmbeddedTurnID, ok := bodyMeta["turn_id"].(string)
	require.True(t, ok, "体内嵌 turn-metadata 应包含 string 类型的 turn_id")

	assert.Equal(t, headerTurnID, bodyTurnID, "头和体的 turn_id 必须一致")
	assert.Equal(t, headerTurnID, bodyEmbeddedTurnID, "头和体内嵌 turn-metadata 的 turn_id 必须一致")
	assert.Equal(t, ids.turnID, headerTurnID, "所有 turn_id 都应来自同一份 ids")
	assert.Equal(t, headerMeta["turn_started_at_unix_ms"], bodyMeta["turn_started_at_unix_ms"], "头和体的 timestamp 必须一致")
	assert.Equal(t, float64(ids.turnStartedAtUnixMs), headerMeta["turn_started_at_unix_ms"])
}

func TestFingerprintIDs_MalformedEmbeddedMetadataRebuiltConsistently(t *testing.T) {
	account := newTestOAuthAccount(2, map[string]any{codexFingerprintModeExtraKey: "session"})
	clientHeaders := make(http.Header)
	clientHeaders.Set("session-id", "client-session-malformed")
	ids := resolveCodexFingerprintIDsForCaller(account, clientHeaders, 0)
	require.NotNil(t, ids)

	h := make(http.Header)
	h.Set("x-codex-turn-metadata", "{malformed")
	applyCodexFingerprintHeaders(h, ids)

	reqBody := map[string]any{
		"client_metadata": map[string]any{
			"session_id":            "client-session-malformed",
			"x-codex-turn-metadata": "[malformed",
		},
	}
	require.True(t, applyCodexFingerprintClientMetadata(reqBody, ids))

	var headerMeta map[string]any
	require.NoError(t, json.Unmarshal([]byte(h.Get("x-codex-turn-metadata")), &headerMeta))
	clientMetadata, ok := reqBody["client_metadata"].(map[string]any)
	require.True(t, ok)
	bodyRaw, ok := clientMetadata["x-codex-turn-metadata"].(string)
	require.True(t, ok)
	var bodyMeta map[string]any
	require.NoError(t, json.Unmarshal([]byte(bodyRaw), &bodyMeta))

	for _, key := range []string{"installation_id", "session_id", "thread_id", "turn_id", "window_id", "turn_started_at_unix_ms"} {
		assert.Equal(t, headerMeta[key], bodyMeta[key], "rebuilt metadata field %s must match", key)
	}
}

// --- applyCodexFingerprintClientMetadata ---

func TestApplyCodexFingerprintClientMetadata_OffMode(t *testing.T) {
	reqBody := map[string]any{
		"client_metadata": map[string]any{
			"x-codex-installation-id": "original",
		},
	}
	modified := applyCodexFingerprintClientMetadata(reqBody, nil)
	assert.False(t, modified, "nil ids 不改写")
}

func TestApplyCodexFingerprintClientMetadata_DeviceMode(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		codexFingerprintModeExtraKey: "device",
		"openai_device_id":           "converged-device",
	})
	ids := resolveCodexFingerprintIDsForCaller(account, nil, 0)
	require.NotNil(t, ids)

	embeddedMeta := `{"installation_id":"x","session_id":"user-session","sandbox":"seccomp"}`
	reqBody := map[string]any{
		"client_metadata": map[string]any{
			"x-codex-installation-id": "original-install",
			"session_id":              "user-session",
			"x-codex-turn-metadata":   embeddedMeta,
		},
	}

	modified := applyCodexFingerprintClientMetadata(reqBody, ids)
	require.True(t, modified)

	cm, ok := reqBody["client_metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "converged-device", cm["x-codex-installation-id"])
	assert.Equal(t, "user-session", cm["session_id"], "device 模式不改 session_id")

	turnMetaStr, ok := cm["x-codex-turn-metadata"].(string)
	require.True(t, ok)
	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(turnMetaStr), &meta))
	assert.Equal(t, "converged-device", meta["installation_id"])
	assert.Equal(t, "seccomp", meta["sandbox"], "非指纹字段保留原样")
}

func TestApplyCodexFingerprintClientMetadata_SessionMode(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		codexFingerprintModeExtraKey: "session",
	})
	clientHeaders := http.Header{}
	clientHeaders.Set("session-id", "client-session-aaa")

	ids := resolveCodexFingerprintIDsForCaller(account, clientHeaders, 0)
	require.NotNil(t, ids)

	embeddedMeta := `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0","sandbox":"seccomp"}`
	reqBody := map[string]any{
		"client_metadata": map[string]any{
			"x-codex-installation-id": "original-install",
			"session_id":              "original-session",
			"x-codex-turn-metadata":   embeddedMeta,
		},
	}

	modified := applyCodexFingerprintClientMetadata(reqBody, ids)
	require.True(t, modified)

	cm, ok := reqBody["client_metadata"].(map[string]any)
	require.True(t, ok)
	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	convergedInstall := resolveConvergedInstallationID(account, seed)
	convergedSession := resolveConvergedSessionID(seed)
	convergedThread := ids.threadID

	assert.Equal(t, convergedInstall, cm["x-codex-installation-id"])
	assert.Equal(t, convergedSession, cm["session_id"])
	assert.Equal(t, convergedThread, cm["thread_id"])
	assert.Equal(t, convergedThread+":0", cm["x-codex-window-id"])

	turnMetaStr, ok := cm["x-codex-turn-metadata"].(string)
	require.True(t, ok)
	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(turnMetaStr), &meta))
	assert.Equal(t, convergedInstall, meta["installation_id"])
	assert.Equal(t, convergedSession, meta["session_id"])
	assert.Equal(t, "seccomp", meta["sandbox"], "非指纹字段保留原样")
}

func TestApplyCodexFingerprintClientMetadata_FullMode(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		codexFingerprintModeExtraKey: "full",
	})
	clientHeaders := http.Header{}
	clientHeaders.Set("session-id", "any-client")

	ids := resolveCodexFingerprintIDsForCaller(account, clientHeaders, 0)
	require.NotNil(t, ids)

	reqBody := map[string]any{
		"client_metadata": map[string]any{
			"session_id":            "x",
			"thread_id":             "x",
			"x-codex-turn-metadata": `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0"}`,
		},
	}

	modified := applyCodexFingerprintClientMetadata(reqBody, ids)
	require.True(t, modified)

	cm, ok := reqBody["client_metadata"].(map[string]any)
	require.True(t, ok)
	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	convergedSession := resolveConvergedSessionID(seed)

	assert.Equal(t, convergedSession, cm["session_id"])
	assert.Equal(t, ids.threadID, cm["thread_id"])
	assert.NotEqual(t, convergedSession, cm["thread_id"], "full mode keeps caller-aware fallback thread independent")
}

// --- extractClientSessionID ---

func TestExtractClientSessionID(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		expected string
	}{
		{"连字符形式优先", func() http.Header {
			h := http.Header{}
			h.Set("session-id", "hyphen-form")
			h.Set("session_id", "underscore-form")
			return h
		}(), "hyphen-form"},
		{"回退到下划线形式", func() http.Header {
			h := http.Header{}
			h.Set("session_id", "underscore-form")
			return h
		}(), "underscore-form"},
		{"都没有", http.Header{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractClientSessionID(tt.headers))
		})
	}
}

// --- 透传路径：raw 字节版 client_metadata 改写 ---

// rawVsMapClientMetadata 用同一份 ids 分别跑 map 版与 raw 字节版，
// 返回两侧最终的 client_metadata 解码结果。
func rawVsMapClientMetadata(t *testing.T, body []byte, ids *codexFingerprintIDs) (map[string]any, map[string]any) {
	t.Helper()

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	applyCodexFingerprintClientMetadata(decoded, ids)
	mapCM, _ := decoded["client_metadata"].(map[string]any)

	rawBody, changed, err := applyCodexFingerprintClientMetadataRaw(body, ids)
	require.NoError(t, err)
	require.True(t, changed)
	var rawDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawBody, &rawDecoded))
	rawCM, _ := rawDecoded["client_metadata"].(map[string]any)
	return mapCM, rawCM
}

func cloneCodexFingerprintIDsForTest(ids *codexFingerprintIDs) *codexFingerprintIDs {
	if ids == nil {
		return nil
	}
	cloned := *ids
	cloned.originalBodySessionID = ""
	cloned.originalBodySessionIDCaptured = false
	return &cloned
}

func applyMapAndRawFingerprintBodiesForTest(t *testing.T, body []byte, ids *codexFingerprintIDs) (map[string]any, map[string]any) {
	t.Helper()

	mapIDs := cloneCodexFingerprintIDsForTest(ids)
	rawIDs := cloneCodexFingerprintIDsForTest(ids)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	applyCodexFingerprintClientMetadata(decoded, mapIDs)

	rawBody, _, err := applyCodexFingerprintClientMetadataRaw(body, rawIDs)
	require.NoError(t, err)
	var rawDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawBody, &rawDecoded))
	return decoded, rawDecoded
}

func TestApplyCodexFingerprintPromptCacheKey_MapRawEquivalence(t *testing.T) {
	for _, mode := range []codexFingerprintMode{codexFingerprintSession, codexFingerprintFull} {
		t.Run(string(mode)+"/default", func(t *testing.T) {
			account := newTestOAuthAccount(4300, map[string]any{codexFingerprintModeExtraKey: string(mode)})
			ids := resolveCodexFingerprintIDs(account, "header-session", mode)
			require.NotNil(t, ids)

			body := []byte(`{"model":"gpt-5.6-sol","prompt_cache_key":"body-session","client_metadata":{"session_id":" body-session ","trace":"keep"},"input":[]}`)
			mapBody, rawBody := applyMapAndRawFingerprintBodiesForTest(t, body, ids)

			require.Equal(t, mapBody["prompt_cache_key"], rawBody["prompt_cache_key"])
			require.Equal(t, ids.sessionID, mapBody["prompt_cache_key"])
			mapCM, _ := mapBody["client_metadata"].(map[string]any)
			rawCM, _ := rawBody["client_metadata"].(map[string]any)
			require.Equal(t, ids.sessionID, mapCM["session_id"])
			require.Equal(t, mapCM["session_id"], rawCM["session_id"])
			require.Equal(t, "keep", rawCM["trace"])
		})
	}

	t.Run("explicit override", func(t *testing.T) {
		account := newTestOAuthAccount(4301, map[string]any{codexFingerprintModeExtraKey: "session"})
		ids := resolveCodexFingerprintIDs(account, "header-session", codexFingerprintSession)
		require.NotNil(t, ids)

		body := []byte(`{"model":"gpt-5.6-sol","prompt_cache_key":"explicit-cache","client_metadata":{"session_id":"body-session"},"input":[]}`)
		mapBody, rawBody := applyMapAndRawFingerprintBodiesForTest(t, body, ids)

		require.Equal(t, "explicit-cache", mapBody["prompt_cache_key"])
		require.Equal(t, "explicit-cache", rawBody["prompt_cache_key"])
		mapCM, _ := mapBody["client_metadata"].(map[string]any)
		rawCM, _ := rawBody["client_metadata"].(map[string]any)
		require.Equal(t, ids.sessionID, mapCM["session_id"])
		require.Equal(t, ids.sessionID, rawCM["session_id"])
	})
}

func TestApplyCodexFingerprintPromptCacheKey_Negatives(t *testing.T) {
	sessionAccount := newTestOAuthAccount(4310, map[string]any{codexFingerprintModeExtraKey: "session"})
	sessionIDs := resolveCodexFingerprintIDs(sessionAccount, "header-session", codexFingerprintSession)
	require.NotNil(t, sessionIDs)
	deviceAccount := newTestOAuthAccount(4311, map[string]any{codexFingerprintModeExtraKey: "device"})
	deviceIDs := resolveCodexFingerprintIDs(deviceAccount, "header-session", codexFingerprintDevice)
	require.NotNil(t, deviceIDs)

	tests := []struct {
		name          string
		body          []byte
		ids           *codexFingerprintIDs
		wantExists    bool
		wantCacheKey  any
		wantRawString string
	}{
		{
			name:       "missing key is not injected",
			body:       []byte(`{"client_metadata":{"session_id":"body-session"}}`),
			ids:        sessionIDs,
			wantExists: false,
		},
		{
			name:         "empty key preserved",
			body:         []byte(`{"prompt_cache_key":"","client_metadata":{"session_id":"body-session"}}`),
			ids:          sessionIDs,
			wantExists:   true,
			wantCacheKey: "",
		},
		{
			name:         "whitespace-different key is an explicit override",
			body:         []byte(`{"prompt_cache_key":" body-session ","client_metadata":{"session_id":"body-session"}}`),
			ids:          sessionIDs,
			wantExists:   true,
			wantCacheKey: " body-session ",
		},
		{
			name:         "non-string key preserved",
			body:         []byte(`{"prompt_cache_key":123,"client_metadata":{"session_id":"body-session"}}`),
			ids:          sessionIDs,
			wantExists:   true,
			wantCacheKey: float64(123),
		},
		{
			name:         "missing source metadata preserves key",
			body:         []byte(`{"prompt_cache_key":"body-session"}`),
			ids:          sessionIDs,
			wantExists:   true,
			wantCacheKey: "body-session",
		},
		{
			name:         "non-string source session preserves key",
			body:         []byte(`{"prompt_cache_key":"123","client_metadata":{"session_id":123}}`),
			ids:          sessionIDs,
			wantExists:   true,
			wantCacheKey: "123",
		},
		{
			name:         "non-object source metadata preserves key",
			body:         []byte(`{"prompt_cache_key":"body-session","client_metadata":"bad"}`),
			ids:          sessionIDs,
			wantExists:   true,
			wantCacheKey: "body-session",
		},
		{
			name:         "device mode preserves key",
			body:         []byte(`{"prompt_cache_key":"body-session","client_metadata":{"session_id":"body-session"}}`),
			ids:          deviceIDs,
			wantExists:   true,
			wantCacheKey: "body-session",
		},
		{
			name:          "off mode preserves body",
			body:          []byte(`{"prompt_cache_key":"body-session","client_metadata":{"session_id":"body-session"}}`),
			ids:           nil,
			wantExists:    true,
			wantCacheKey:  "body-session",
			wantRawString: `{"prompt_cache_key":"body-session","client_metadata":{"session_id":"body-session"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mapBody map[string]any
			require.NoError(t, json.Unmarshal(tt.body, &mapBody))
			changedMap := applyCodexFingerprintClientMetadata(mapBody, cloneCodexFingerprintIDsForTest(tt.ids))

			rawBody, changedRaw, err := applyCodexFingerprintClientMetadataRaw(tt.body, cloneCodexFingerprintIDsForTest(tt.ids))
			require.NoError(t, err)
			if tt.ids == nil {
				require.False(t, changedMap)
				require.False(t, changedRaw)
				require.JSONEq(t, tt.wantRawString, string(rawBody))
				return
			}
			require.True(t, changedMap)
			require.True(t, changedRaw)

			rawDecoded := map[string]any{}
			require.NoError(t, json.Unmarshal(rawBody, &rawDecoded))
			_, mapExists := mapBody["prompt_cache_key"]
			_, rawExists := rawDecoded["prompt_cache_key"]
			require.Equal(t, tt.wantExists, mapExists)
			require.Equal(t, tt.wantExists, rawExists)
			if tt.wantExists {
				require.Equal(t, tt.wantCacheKey, mapBody["prompt_cache_key"])
				require.Equal(t, tt.wantCacheKey, rawDecoded["prompt_cache_key"])
			}
		})
	}
}

func TestApplyCodexFingerprintClientMetadataRaw_MatchesMapVariant(t *testing.T) {
	embedded := `{\"installation_id\":\"real-install\",\"session_id\":\"real-session\",\"sandbox\":\"seatbelt\"}`
	bodies := map[string]string{
		"no_client_metadata": `{"model":"gpt-5.6-sol","input":[],"stream":true}`,
		"object_with_extras": `{"model":"gpt-5.6-sol","client_metadata":{"session_id":"client-session","traceparent":"00-abc-def-01","x-codex-turn-metadata":"` + embedded + `"},"stream":true}`,
		"non_object_value":   `{"model":"gpt-5.6-sol","client_metadata":"bogus","stream":true}`,
	}
	for _, mode := range []codexFingerprintMode{codexFingerprintDevice, codexFingerprintSession, codexFingerprintFull} {
		account := newTestOAuthAccount(4242, map[string]any{codexFingerprintModeExtraKey: string(mode)})
		ids := resolveCodexFingerprintIDs(account, "client-sess-raw", mode)
		require.NotNil(t, ids)
		for name, body := range bodies {
			t.Run(string(mode)+"/"+name, func(t *testing.T) {
				mapCM, rawCM := rawVsMapClientMetadata(t, []byte(body), ids)
				assert.Equal(t, mapCM, rawCM, "raw 字节版与 map 版的 client_metadata 结果必须逐点一致")
			})
		}
	}
}

func TestApplyCodexFingerprintClientMetadataRaw_PreservesUnrelatedFields(t *testing.T) {
	account := newTestOAuthAccount(4243, map[string]any{codexFingerprintModeExtraKey: "session"})
	ids := resolveCodexFingerprintIDs(account, "client-sess-preserve", codexFingerprintSession)
	require.NotNil(t, ids)

	body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"message","role":"user","content":"hi"}],"stream":true,"prompt_cache_key":"pck-1"}`)
	out, changed, err := applyCodexFingerprintClientMetadataRaw(body, ids)
	require.NoError(t, err)
	require.True(t, changed)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out, &decoded))
	assert.Equal(t, "gpt-5.6-sol", decoded["model"])
	assert.Equal(t, "pck-1", decoded["prompt_cache_key"])
	assert.Equal(t, true, decoded["stream"])
	cm, _ := decoded["client_metadata"].(map[string]any)
	require.NotNil(t, cm)
	assert.Equal(t, ids.sessionID, cm["session_id"])
	assert.Equal(t, ids.turnID, cm["turn_id"])
}

func TestApplyCodexFingerprintClientMetadataRaw_Noop(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol"}`)
	out, changed, err := applyCodexFingerprintClientMetadataRaw(body, nil)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, body, out)

	out, changed, err = applyCodexFingerprintClientMetadataRaw(nil, &codexFingerprintIDs{mode: codexFingerprintSession, installationID: "x"})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Nil(t, out)
}

// --- context 暂存与出站头应用（透传/非透传共用 seam）---

func newFingerprintStageTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c
}

func TestStageCodexFingerprintIDs_NilOverwritesPreviousAccount(t *testing.T) {
	c := newFingerprintStageTestContext(t)
	accountA := newTestOAuthAccount(1001, map[string]any{codexFingerprintModeExtraKey: "session"})
	idsA := resolveCodexFingerprintIDs(accountA, "sess-x", codexFingerprintSession)
	require.NotNil(t, idsA)
	stageCodexFingerprintIDs(c, idsA)

	// failover 切到 off 模式账号：无条件覆写为 nil，上一账号 IDs 不得残留
	stageCodexFingerprintIDs(c, nil)

	h := http.Header{}
	h.Set("session_id", "isolated-session")
	accountB := newTestOAuthAccount(1002, map[string]any{"codex_fingerprint_mode": "off"})
	applyStagedCodexFingerprintHeaders(c, accountB, h)
	assert.Equal(t, "isolated-session", h.Get("session_id"), "off 账号不得应用上一账号的收敛 ID")
	assert.Empty(t, h.Get("x-codex-installation-id"))
}

func TestApplyStagedCodexFingerprintRejectsDifferentOAuthAccount(t *testing.T) {
	c := newFingerprintStageTestContext(t)
	accountA := newTestOAuthAccount(1003, map[string]any{codexFingerprintModeExtraKey: "session"})
	idsA := resolveCodexFingerprintIDs(accountA, "sess-a", codexFingerprintSession)
	require.NotNil(t, idsA)
	stageCodexFingerprintIDs(c, idsA)

	accountB := newTestOAuthAccount(1004, map[string]any{codexFingerprintModeExtraKey: "session"})
	h := make(http.Header)
	h.Set("session-id", "account-b-session")
	applyStagedCodexFingerprintHeaders(c, accountB, h)
	assert.Equal(t, "account-b-session", h.Get("session-id"))
	assert.Empty(t, h.Get("x-codex-installation-id"))

	body := map[string]any{"client_metadata": map[string]any{"session_id": "account-b-session"}}
	assert.False(t, applyStagedCodexFingerprintClientMetadata(c, accountB, body))
	clientMetadata, ok := body["client_metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "account-b-session", clientMetadata["session_id"])
}

func TestApplyStagedCodexFingerprintHeaders_SkipsNonOAuthAccount(t *testing.T) {
	c := newFingerprintStageTestContext(t)
	oauthIDs := resolveCodexFingerprintIDs(newTestOAuthAccount(1003, map[string]any{codexFingerprintModeExtraKey: "session"}), "sess-y", codexFingerprintSession)
	require.NotNil(t, oauthIDs)
	stageCodexFingerprintIDs(c, oauthIDs)

	h := http.Header{}
	apiKeyAccount := &Account{ID: 1004, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	applyStagedCodexFingerprintHeaders(c, apiKeyAccount, h)
	assert.Empty(t, h.Get("x-codex-installation-id"), "stale 收敛 ID 不得应用到非 OAuth 账号")
}

func TestBuildUpstreamRequestOpenAIPassthrough_AppliesStagedFingerprint(t *testing.T) {
	svc := &OpenAIGatewayService{}
	// 收敛是显式 opt-in（#5610）：显式开启后验证透传路径的出站头收敛。
	account := newTestOAuthAccount(2001, map[string]any{
		"openai_oauth_passthrough": true,
		"codex_fingerprint_mode":   "session",
	})

	c := newFingerprintStageTestContext(t)
	c.Request.Header.Set("session_id", "real-client-session")
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color")
	c.Request.Header.Set("originator", "codex_cli_rs")
	c.Request.Header.Set("x-codex-turn-metadata", `{"installation_id":"real-install","session_id":"real-session","sandbox":"seatbelt"}`)

	// 复刻 forwardOpenAIPassthrough 的解析+暂存 seam（默认 session 模式）
	ids := resolveCodexFingerprintIDsForCaller(account, c.Request.Header, 0)
	require.NotNil(t, ids)
	stageCodexFingerprintIDs(c, ids)

	body := []byte(`{"model":"gpt-5.6-sol","input":[{"role":"user","content":"hello"}],"stream":true}`)
	var payload map[string]any
	require.NoError(t, decodeOpenAIJSONUseNumber(body, &payload))
	applyCodexFingerprintClientMetadata(payload, ids)
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req, err := svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test-token")
	require.NoError(t, err)

	assert.Equal(t, ids.sessionID, req.Header.Get("session-id"), "default affinity follows the converged session")
	metadata, decodeErr := decodeCodexTurnMetadata(req.Header.Get(openAIWSTurnMetadataHeader))
	require.NoError(t, decodeErr)
	assert.Equal(t, ids.installationID, metadata["installation_id"])
	assert.Equal(t, ids.windowID, req.Header.Get("x-codex-window-id"))
	assert.Equal(t, ids.threadID, req.Header.Get("x-client-request-id"))
	turnMetadata := req.Header.Get("x-codex-turn-metadata")
	require.NotEmpty(t, turnMetadata)
	assert.Contains(t, turnMetadata, ids.sessionID, "turn-metadata JSON 中的 session_id 应被收敛")
	assert.Contains(t, turnMetadata, `"sandbox":"seatbelt"`, "turn-metadata 未指定字段应原样保留")
}

func TestBuildUpstreamRequestOpenAIPassthrough_OffModeKeepsIsolatedSession(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := newTestOAuthAccount(2002, map[string]any{
		"openai_oauth_passthrough": true,
		"codex_fingerprint_mode":   "off",
	})

	c := newFingerprintStageTestContext(t)
	c.Request.Header.Set("session_id", "real-client-session")
	c.Request.Header.Set("originator", "codex_cli_rs")

	ids := resolveCodexFingerprintIDsForCaller(account, c.Request.Header, 0)
	require.Nil(t, ids)
	stageCodexFingerprintIDs(c, ids)

	body := []byte(`{"model":"gpt-5.6-sol","input":[{"role":"user","content":"hello"}],"stream":true}`)
	req, err := svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test-token")
	require.NoError(t, err)

	assert.NotEmpty(t, req.Header.Get("session_id"))
	assert.NotEqual(t, resolveConvergedSessionID(testCodexFingerprintSeed), req.Header.Get("session_id"), "off 模式不得收敛 session_id")
	assert.Empty(t, req.Header.Get("x-codex-window-id"))
}

func TestApplyCodexFingerprintClientMetadataRaw_NonObjectBodyUntouched(t *testing.T) {
	account := newTestOAuthAccount(4244, map[string]any{codexFingerprintModeExtraKey: "session"})
	ids := resolveCodexFingerprintIDs(account, "client-sess-nonobj", codexFingerprintSession)
	require.NotNil(t, ids)

	for _, body := range []string{`[1,2,3]`, `"plain string"`, `not json at all`} {
		out, changed, err := applyCodexFingerprintClientMetadataRaw([]byte(body), ids)
		require.NoError(t, err)
		assert.False(t, changed, "非 JSON 对象 body 不应被改写: %s", body)
		assert.Equal(t, []byte(body), out)
	}
}
