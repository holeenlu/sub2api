package service

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// compactProbeSSESuccessBody 是原生 v2 压缩成功的最小 SSE 形态：
// output_item.done 携带 compaction item + response.completed。
const compactProbeSSESuccessBody = "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"compaction\",\"id\":\"cmp_probe\",\"encrypted_content\":\"blob\"}}\n\n" +
	"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_probe\",\"output\":[]}}\n\n"

func TestAccountTestService_TestAccountConnection_OpenAICompactOAuthSuccessPersistsSupport(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          1,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":               "oauth-token",
			"chatgpt_account_id":         "chatgpt-acc",
			"chatgpt_account_is_fedramp": true,
		},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid-probe"}},
		Body:       io.NopCloser(strings.NewReader(compactProbeSSESuccessBody)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", bytes.NewReader(nil))

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact)
	require.NoError(t, err)

	// 原生 v2：探测普通 /responses 线，不再打已下线的 /responses/compact。
	require.Equal(t, chatgptCodexAPIURL, upstream.lastReq.URL.String())
	require.Equal(t, "chatgpt.com", upstream.lastReq.Host)
	require.Equal(t, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
	require.Contains(t, upstream.lastReq.Header.Get("x-codex-beta-features"), "remote_compaction_v2")
	require.Empty(t, upstream.lastReq.Header.Get("session_id"))
	require.Equal(t, scopeCodexAccountIdentityValue(&account, 0, "session", compactProbeSessionID(account.ID)), upstream.lastReq.Header.Get("session-id"))
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.Equal(t, codexCLIUserAgent, upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, "chatgpt-acc", upstream.lastReq.Header.Get("chatgpt-account-id"))
	require.Equal(t, "true", upstream.lastReq.Header.Get("x-openai-fedramp"))
	require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.lastBody, "model").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.False(t, gjson.GetBytes(upstream.lastBody, "store").Bool())
	inputItems := gjson.GetBytes(upstream.lastBody, "input").Array()
	require.NotEmpty(t, inputItems)
	require.Equal(t, "compaction_trigger", inputItems[len(inputItems)-1].Get("type").String())

	updates := <-updateCalls
	require.Equal(t, true, updates["openai_compact_supported"])
	require.Equal(t, http.StatusOK, updates["openai_compact_last_status"])
	require.Contains(t, rec.Body.String(), `"type":"test_complete"`)
}

func TestAccountTestService_TestAccountConnection_OpenAICompactOAuth404MarksUnsupported(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          2,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`404 page not found`)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/2/test", bytes.NewReader(nil))

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact)
	require.Error(t, err)

	updates := <-updateCalls
	require.Equal(t, false, updates["openai_compact_supported"])
	require.Equal(t, http.StatusNotFound, updates["openai_compact_last_status"])
	require.Contains(t, rec.Body.String(), `"type":"error"`)
}

func TestAccountTestService_TestAccountConnection_OpenAICompactAPIKeyUsesNativeResponsesPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          3,
		Name:        "openai-apikey",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://example.com/v1",
			// post-#5641：compact_model_mapping 仅作用于 legacy /responses/compact，
			// 原生 v2 探测不应用它。
			"compact_model_mapping": map[string]any{"gpt-5.4": "gpt-5.4-openai-compact"},
		},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(compactProbeSSESuccessBody)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/3/test", bytes.NewReader(nil))
	c.Set("api_key", &APIKey{ID: 77})

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact)
	require.NoError(t, err)

	require.Equal(t, "https://example.com/v1/responses", upstream.lastReq.URL.String())
	requireOpenAICodexProbeHeaders(t, upstream.lastReq.Header)
	require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, compactProbeSessionID(account.ID), upstream.lastReq.Header.Get("session_id"))
	require.Equal(t, compactProbeSessionID(account.ID), upstream.lastReq.Header.Get("conversation_id"))
	require.Empty(t, upstream.lastReq.Header.Get("session-id"))
	require.Empty(t, upstream.lastReq.Header.Get("thread-id"))
	require.Empty(t, upstream.lastReq.Header.Get(openAIWSTurnMetadataHeader))
	expectedBody, err := json.Marshal(createOpenAICompactProbePayload("gpt-5.4", false))
	require.NoError(t, err)
	require.Equal(t, expectedBody, upstream.lastBody, "API-key probe body must remain unchanged")
	require.Contains(t, upstream.lastReq.Header.Get("x-codex-beta-features"), "remote_compaction_v2")
	require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.lastBody, "model").String(),
		"原生 v2 探测不应用 compact_model_mapping")
	updates := <-updateCalls
	require.Equal(t, true, updates["openai_compact_supported"])
}

func TestAccountTestService_TestAccountConnection_OpenAICompactAPIKeyDefaultBaseURLUsesResponsesPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          4,
		Name:        "openai-apikey-default",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "sk-test",
		},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(compactProbeSSESuccessBody)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/4/test", bytes.NewReader(nil))

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact)
	require.NoError(t, err)
	require.Equal(t, "https://api.openai.com/v1/responses", upstream.lastReq.URL.String())
	<-updateCalls
}

func TestAccountTestService_TestAccountConnection_OpenAICompact2xxWithoutItemMarksUnsupported(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          5,
		Name:        "openai-oauth-no-item",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	// 200 但流里没有 compaction item：链路吞掉了 compaction_trigger 的形态
	//（#5478 的 "got 0 items"），必须判定为不支持。
	noItemBody := "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"id\":\"msg_1\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_x\",\"output\":[]}}\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(noItemBody)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/5/test", bytes.NewReader(nil))

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact)
	require.Error(t, err)

	updates := <-updateCalls
	require.Equal(t, false, updates["openai_compact_supported"])
	require.Contains(t, rec.Body.String(), `"type":"error"`)
}

// Capture the actual doOpenAIAccountTestUpstream request, including its replay
// body: native v2 must share one final identity across body and official headers.
func TestAccountTestService_TestAccountConnection_OpenAICompactProbeIdentityMatchesRealTraffic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"off", "device", "session", "full"} {
		t.Run(mode, func(t *testing.T) {
			captured := map[string]http.Header{}
			for _, tc := range []struct {
				name, upstreamAccount string
				caller                int64
				shadow                bool
			}{
				{"direct", "parent-a", 77, false},
				{"shadow", "parent-a", 77, true},
				{"other_caller", "parent-a", 88, true},
				{"other_parent", "parent-b", 77, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					credential := Account{
						ID: 50, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
						Status: StatusActive, Schedulable: true, Concurrency: 1,
						Credentials: map[string]any{"access_token": "parent-token", "chatgpt_account_id": tc.upstreamAccount},
						Extra:       map[string]any{"codex_fingerprint_mode": mode, codexFingerprintSeedExtraKey: testCodexFingerprintSeed},
					}
					account := credential
					account.ID = 6 // Keep the raw probe identity fixed across caller/credential changes.
					if tc.shadow {
						account.ParentAccountID = &credential.ID
						account.Credentials = nil
						account.Extra = nil // Convergence must come from the credential parent.
					}
					repo := &snapshotUpdateAccountRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account, credential}}}
					upstream := &httpUpstreamRecorder{resp: &http.Response{
						StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
						Body: io.NopCloser(strings.NewReader(compactProbeSSESuccessBody)),
					}}
					svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/6/test", nil)
					c.Set("api_key", &APIKey{ID: tc.caller})
					require.NoError(t, svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact))
					require.Len(t, upstream.requests, 1)
					req := upstream.lastReq
					h := req.Header
					captured[tc.name] = h
					require.Equal(t, chatgptCodexAPIURL, req.URL.String())
					require.Equal(t, "Bearer parent-token", h.Get("Authorization"))
					require.Equal(t, tc.upstreamAccount, h.Get("chatgpt-account-id"))
					require.Equal(t, "text/event-stream", h.Get("Accept"))
					require.Contains(t, h.Get("x-codex-beta-features"), "remote_compaction_v2")
					require.Empty(t, h.Values("OpenAI-Beta"), "HTTP must not carry legacy/WS responses negotiation")
					for _, key := range []string{"session_id", "conversation_id", "installation_id", "x-codex-installation-id", "accept-language"} {
						require.Empty(t, h.Get(key), key)
					}
					cm := gjson.GetBytes(upstream.lastBody, "client_metadata")
					require.True(t, cm.IsObject(), "native v2 admits normalized body metadata")
					for _, field := range []string{"session_id", "thread_id", "turn_id", "x-codex-installation-id"} {
						id, err := uuid.Parse(cm.Get(field).String())
						require.NoError(t, err, field)
						if field == "session_id" || field == "thread_id" {
							require.Equal(t, uuid.Version(4), id.Version(), "preserve the local v4 identity contract")
						}
					}
					require.Equal(t, cm.Get("session_id").String(), h.Get("session-id"))
					require.Equal(t, cm.Get("thread_id").String(), h.Get("thread-id"))
					require.Equal(t, h.Get("thread-id"), h.Get("x-client-request-id"))
					require.NotEqual(t, h.Get("session-id"), h.Get("thread-id"))
					if mode == "session" || mode == "full" {
						require.Equal(t, h.Get("thread-id")+":0", h.Get("x-codex-window-id"))
					} else {
						_, err := uuid.Parse(h.Get("x-codex-window-id"))
						require.NoError(t, err, "off/device preserve the scoped opaque probe window")
					}
					metadata := cm.Get(openAIWSTurnMetadataHeader).String()
					require.JSONEq(t, metadata, h.Get(openAIWSTurnMetadataHeader))
					for field, flat := range map[string]string{"session_id": "session_id", "thread_id": "thread_id", "turn_id": "turn_id", "installation_id": "x-codex-installation-id", "window_id": "x-codex-window-id"} {
						require.Equal(t, cm.Get(flat).String(), gjson.Get(metadata, field).String(), field)
					}
					require.Equal(t, "compaction", gjson.Get(metadata, "request_kind").String())
					require.Positive(t, gjson.Get(metadata, "turn_started_at_unix_ms").Int())
					require.NotEmpty(t, gjson.Get(metadata, "context_window_id").String())
					rawSession := compactProbeSessionID(account.ID)
					if mode == "off" || mode == "device" {
						require.Equal(t, scopeCodexAccountIdentityValue(&credential, tc.caller, "session", rawSession), h.Get("session-id"))
						require.NotEqual(t, scopeCodexAccountIdentityValue(&credential, tc.caller, "session", isolateOpenAIUpstreamSessionID(tc.caller, &credential, rawSession)), h.Get("session-id"), "do not scope a legacy hash")
					} else {
						ids := resolveCodexFingerprintIDsForCaller(&credential, http.Header{"Session_id": []string{rawSession}}, tc.caller)
						require.NotNil(t, ids)
						require.Equal(t, ids.sessionID, h.Get("session-id"))
						require.Equal(t, ids.threadID, h.Get("thread-id"))
					}
					if mode != "off" {
						require.Equal(t, resolveConvergedInstallationID(&credential, testCodexFingerprintSeed), cm.Get("x-codex-installation-id").String())
					}
					var body map[string]any
					require.NoError(t, json.Unmarshal(upstream.lastBody, &body))
					delete(body, "client_metadata")
					require.Equal(t, createOpenAICompactProbePayload("gpt-5.4", true), body, "normalization must preserve probe payload and compaction_trigger")
					replay, err := req.GetBody()
					require.NoError(t, err)
					replayBody, err := io.ReadAll(replay)
					require.NoError(t, err)
					require.NoError(t, replay.Close())
					require.Equal(t, upstream.lastBody, replayBody)
					require.Equal(t, int64(len(replayBody)), req.ContentLength)
				})
			}
			require.Equal(t, captured["direct"].Get("thread-id"), captured["shadow"].Get("thread-id"), "the same credential must own both identities")
			require.NotEqual(t, captured["shadow"].Get("thread-id"), captured["other_caller"].Get("thread-id"))
			require.NotEqual(t, captured["shadow"].Get("thread-id"), captured["other_parent"].Get("thread-id"), "parent isolation must survive shared convergence seeds")
		})
	}
	t.Run("namespace_missing", func(t *testing.T) {
		account := Account{
			ID: 6, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Credentials: map[string]any{"access_token": "token-without-account-identity"},
		}
		repo := &snapshotUpdateAccountRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}}}
		upstream := &httpUpstreamRecorder{resp: &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(compactProbeSSESuccessBody)),
		}}
		svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/6/test", nil)
		c.Set("api_key", &APIKey{ID: 77})
		require.NoError(t, svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact))
		require.Empty(t, codexAccountIdentityNamespace(&account))
		for _, key := range []string{"session-id", "thread-id", openAIWSTurnMetadataHeader} {
			require.Empty(t, upstream.lastReq.Header.Get(key), "missing namespace must not expose an unscoped official identity")
		}
		legacy := isolateOpenAIUpstreamSessionID(77, &account, compactProbeSessionID(account.ID))
		require.Equal(t, legacy, upstream.lastReq.Header.Get("session_id"))
		require.Equal(t, legacy, upstream.lastReq.Header.Get("conversation_id"))
		require.False(t, gjson.GetBytes(upstream.lastBody, "client_metadata").Exists())
	})
}

func TestCompactProbeSessionID_IsUUIDShaped(t *testing.T) {
	for _, id := range []int64{0, 1, 987654} {
		got := compactProbeSessionID(id)
		_, err := uuid.Parse(got)
		require.NoError(t, err, "探测会话标识必须是 UUID 形态: %s", got)
	}
	require.Equal(t, compactProbeSessionID(7), compactProbeSessionID(7), "同账号应稳定复用同一会话")
	require.NotEqual(t, compactProbeSessionID(7), compactProbeSessionID(8))
}
