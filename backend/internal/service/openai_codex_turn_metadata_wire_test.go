//go:build unit

package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func marshalCodexTurnMetadataString(t *testing.T, raw string) string {
	t.Helper()
	metadata, err := decodeCodexTurnMetadata(raw)
	require.NoError(t, err)
	out, err := marshalCodexTurnMetadata(metadata)
	require.NoError(t, err)
	return string(out)
}

// 官方 serde 按 struct 声明顺序输出；Go 的 map 序列化恒为字母序。
func TestMarshalCodexTurnMetadata_FollowsOfficialFieldOrder(t *testing.T) {
	// 故意按字母序的反序喂进去，确认输出不是简单排序的结果。
	out := marshalCodexTurnMetadataString(t, `{
		"compaction":{"trigger":"auto"},
		"turn_started_at_unix_ms":1789302780858,
		"window_id":"t:0",
		"turn_id":"turn-1",
		"thread_id":"thread-1",
		"session_id":"session-1",
		"request_kind":"turn",
		"installation_id":"install-1"
	}`)

	require.Equal(t,
		`{"installation_id":"install-1","session_id":"session-1","thread_id":"thread-1",`+
			`"turn_id":"turn-1","window_id":"t:0","request_kind":"turn",`+
			`"turn_started_at_unix_ms":1789302780858,"compaction":{"trigger":"auto"}}`,
		out)

	// 防回退对照组：agent_name 的官方声明序在 session_id 之后，字母序则相反。
	// 有人把 marshalCodexTurnMetadata 改回 json.Marshal(map) 这条就会红。
	contrast := map[string]any{"agent_name": "a", "session_id": "s"}
	ordered, err := marshalCodexTurnMetadata(contrast)
	require.NoError(t, err)
	require.Equal(t, `{"session_id":"s","agent_name":"a"}`, string(ordered))

	stdlib, err := json.Marshal(contrast)
	require.NoError(t, err)
	require.Equal(t, `{"agent_name":"a","session_id":"s"}`, string(stdlib),
		"标准库恒为字母序，正是本编码器要绕开的行为")
}

// order 之外的未知键复刻 flatten(BTreeMap)：字典序、排在已知键之后。
func TestMarshalCodexTurnMetadata_UnknownKeysSortedAfterKnown(t *testing.T) {
	out := marshalCodexTurnMetadataString(t,
		`{"zz_future":1,"aa_future":2,"session_id":"s","installation_id":"i"}`)

	require.Equal(t,
		`{"installation_id":"i","session_id":"s","aa_future":2,"zz_future":1}`,
		out)
}

// 嵌套对象各有形状：workspaces/tool_namespaces_info/functions 是 BTreeMap
// （字典序），workspace/namespace/function/compaction 是 struct（声明序）。
func TestMarshalCodexTurnMetadata_NestedShapes(t *testing.T) {
	out := marshalCodexTurnMetadataString(t, `{
		"workspaces":{
			"/b":{"has_changes":true,"latest_git_commit_hash":"deadbeef","associated_remote_urls":{"upstream":"u","origin":"o"}},
			"/a":{"has_changes":false}
		},
		"tool_namespaces_info":{
			"shell":{"functions":{"run":{"source":"builtin","name":"run","direct":true}},"name":"shell"}
		},
		"compaction":{"strategy":"s","trigger":"auto","phase":"p"}
	}`)

	require.Equal(t,
		`{"workspaces":{"/a":{"has_changes":false},`+
			`"/b":{"associated_remote_urls":{"origin":"o","upstream":"u"},"latest_git_commit_hash":"deadbeef","has_changes":true}},`+
			`"tool_namespaces_info":{"shell":{"name":"shell","functions":{"run":{"name":"run","direct":true,"source":"builtin"}}}},`+
			`"compaction":{"trigger":"auto","phase":"p","strategy":"s"}}`,
		out)
}

// Go 标准库默认把 & < > 转成 \u0026 等；serde 不转义，这是可判别特征。
func TestMarshalCodexTurnMetadata_DoesNotHTMLEscape(t *testing.T) {
	out := marshalCodexTurnMetadataString(t,
		`{"installation_id":"a&b<c>d","workspaces":{"C:\\A & B":{}}}`)

	require.Contains(t, out, `"a&b<c>d"`)
	require.Contains(t, out, `"C:\\A & B"`)
	require.NotContains(t, out, `\u0026`)
	require.NotContains(t, out, `\u003c`)
	require.NotContains(t, out, `\u003e`)
}

// 整数必须原样往返：默认解码会变成 float64 并按浮点规则重新格式化。
func TestMarshalCodexTurnMetadata_PreservesIntegerLiterals(t *testing.T) {
	out := marshalCodexTurnMetadataString(t,
		`{"turn_started_at_unix_ms":1789302780858,"window_number":0,"huge":9007199254740993}`)

	require.Contains(t, out, `"turn_started_at_unix_ms":1789302780858`)
	require.Contains(t, out, `"window_number":0`)
	require.Contains(t, out, `"huge":9007199254740993`, "超过 2^53 的整数不得被浮点化")
}

func TestMarshalCodexTurnMetadata_SerdeControlEscapes(t *testing.T) {
	raw := `{"agent_name":"a\b\f\n\r\t\u0001\u007f","extension":{"value":"\b\f"}}`
	require.Equal(t, raw, marshalCodexTurnMetadataString(t, raw))
}

func TestNormalizeCodexRequestMetadata_FinalWirePreservesBodyAndAffinity(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")
	body := []byte(`{"input":[ {"role":"user", "content":"x", "large":900719925474099312345} ],"prompt_cache_key":"cache","client_metadata":{"session_id":"body-session","thread_id":"body-thread","x-codex-turn-metadata":"{\"session_id\":\"body-session\",\"thread_id\":\"body-thread\",\"window_id\":\"body-thread:17\",\"tool_namespaces_info\":{\"python\":{\"name\":\"python\"}},\"custom\":900719925474099312345}"}}`)
	scoped, _, err := applyCodexAccountIdentityClientMetadataRaw(body, account, 77)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", bytes.NewReader(scoped))
	require.NoError(t, err)
	req.Header.Set("session-id", "cache-affinity")
	req.Header.Set(openAIWSTurnMetadataHeader, `{"thread_source":"vscode","session_id":"header-session"}`)
	applyCodexAccountIdentityHeaders(req.Header, account, 77)
	affinity := req.Header.Get("session-id")
	normalized, err := applyCodexRequestMetadata(req, scoped, account, 77, false)
	require.NoError(t, err)
	require.Equal(t, affinity, req.Header.Get("session-id"))
	require.Equal(t, gjson.GetBytes(scoped, "prompt_cache_key").String(), gjson.GetBytes(normalized, "prompt_cache_key").String())
	require.Equal(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(normalized, "input").Raw, "large payload bytes must not be rebuilt")
	cm := gjson.GetBytes(normalized, "client_metadata")
	embedded, err := decodeCodexTurnMetadata(cm.Get(openAIWSTurnMetadataHeader).String())
	require.NoError(t, err)
	header, err := decodeCodexTurnMetadata(req.Header.Get(openAIWSTurnMetadataHeader))
	require.NoError(t, err)
	require.Contains(t, embedded, "tool_namespaces_info")
	require.NotContains(t, header, "tool_namespaces_info")
	require.Equal(t, "900719925474099312345", embedded["custom"].(json.Number).String())
	require.Equal(t, scopeCodexAccountIdentityValue(account, 77, "session", "body-session"), embedded["session_id"])
	require.Equal(t, embedded["thread_id"], req.Header.Get("thread-id"))
	require.Equal(t, embedded["thread_id"].(string)+":17", embedded["window_id"])
	require.Equal(t, json.Number("17"), embedded["window_number"])
	require.Equal(t, "vscode", embedded["thread_source"])
	for _, key := range []string{"installation_id", "turn_id", "context_window_id", "turn_started_at_unix_ms", "request_kind"} {
		require.Contains(t, embedded, key)
	}
	actual, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	replay, err := req.GetBody()
	require.NoError(t, err)
	defer replay.Close()
	replayed, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.Equal(t, normalized, actual)
	require.Equal(t, normalized, replayed)
	require.Same(t, &normalized[0], &req.Body.(*httputil.PrereadBody).Bytes()[0], "final semantic body must retain the prepared bytes")
	require.Same(t, &normalized[0], &replay.(*httputil.PrereadBody).Bytes()[0], "compression replay must not copy the prepared bytes")
	require.Equal(t, int64(len(normalized)), req.ContentLength)
	require.Equal(t, normalized, codexNormalizedRequestBody(req, scoped))
	again, changed, err := normalizeCodexRequestMetadata(normalized, req.Header, account, 77, false)
	require.NoError(t, err)
	require.False(t, changed, "normalization must be idempotent, never re-scope")
	require.Equal(t, normalized, again)
	retry, err := http.NewRequest(http.MethodPost, req.URL.String(), bytes.NewReader(normalized))
	require.NoError(t, err)
	retry.Header = req.Header.Clone()
	retried, err := applyCodexRequestMetadata(retry, codexNormalizedRequestBody(req, scoped), account, 77, false)
	require.NoError(t, err)
	require.Equal(t, normalized, retried, "same-account retry reuses turn UUID/time and embedded body")
	require.Same(t, &normalized[0], &retry.Body.(*httputil.PrereadBody).Bytes()[0], "unchanged retries retain the same immutable bytes")
	require.Equal(t, normalized, codexNormalizedRequestBody(retry, retried))
	require.Equal(t, scoped, codexNormalizedRequestBody(nil, scoped))
}

func TestNormalizeCodexRequestMetadataMap_SessionAvoidsInputSerialization(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")
	for _, cm := range []map[string]any{
		{"session_id": "scoped-session"},
		{openAIWSTurnMetadataHeader: `{"session_id":"embedded-scoped-session"}`},
	} {
		// A sentinel Marshaler fails if normalization touches input at all. The
		// actual sender still owns payload encoding/error handling afterward.
		payload := map[string]any{"client_metadata": cm, "input": codexMetadataUnserializableInput{}}
		require.NoError(t, normalizeCodexRequestMetadataMap(payload, nil, account, 77))
	}
	var previous string
	for _, input := range []string{"first conversation", "second conversation"} {
		payload := map[string]any{"input": input, "client_metadata": map[string]any{
			openAIWSTurnMetadataHeader: `{"session_id":"invalid-document"} true`,
		}}
		require.NoError(t, normalizeCodexRequestMetadataMap(payload, nil, account, 77))
		session := payload["client_metadata"].(map[string]any)["session_id"].(string)
		require.NotEqual(t, previous, session, "invalid embedded JSON cannot suppress fallback body anchoring")
		previous = session
	}
	var firstThread string
	for _, input := range []string{"first topic", "second topic"} {
		h := make(http.Header)
		h.Set("session-id", "shared-cache-affinity")
		payload := map[string]any{"input": input}
		require.NoError(t, normalizeCodexRequestMetadataMap(payload, h, account, 77))
		thread := h.Get("thread-id")
		require.Equal(t, "shared-cache-affinity", h.Get("session-id"))
		require.NotEqual(t, firstThread, thread, "shared cache affinity cannot collapse unrelated fallback threads")
		firstThread = thread
	}
}

type codexMetadataUnserializableInput struct{}

func (codexMetadataUnserializableInput) MarshalJSON() ([]byte, error) {
	return nil, errors.New("metadata normalization must not serialize input with a known session")
}

func TestNormalizeCodexRequestMetadata_DefaultsIsolationAndCompaction(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":[{"role":"user","content":"hello"}]}`)
	first := newMigrationAccount(t, 1, "acct-A")
	second := newMigrationAccount(t, 2, "acct-B")
	identities := map[string]bool{}
	for _, owner := range []struct {
		account *Account
		key     int64
	}{{first, 77}, {first, 88}, {second, 77}} {
		normalized, changed, err := normalizeCodexRequestMetadata(body, nil, owner.account, owner.key, false)
		require.NoError(t, err)
		require.True(t, changed)
		session := gjson.GetBytes(normalized, "client_metadata.session_id").String()
		thread := gjson.GetBytes(normalized, "client_metadata.thread_id").String()
		require.NotEqual(t, session, thread)
		require.False(t, identities[session])
		identities[session] = true
		require.Regexp(t, `^[0-9a-f-]{14}4[0-9a-f-]{21}$`, session)
	}
	for _, account := range []*Account{nil, newTestOAuthAccount(3, nil), {Platform: PlatformOpenAI, Type: AccountTypeAPIKey}} {
		next, changed, err := normalizeCodexRequestMetadata(body, nil, account, 77, false)
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, body, next)
	}
	for _, legacy := range []bool{true, false} {
		compactBody := []byte(`{"input":[{"type":"compaction_trigger"}],"prompt_cache_key":"keep"}`)
		h := make(http.Header)
		next, changed, err := normalizeCodexRequestMetadata(compactBody, h, first, 77, legacy)
		require.NoError(t, err)
		require.Equal(t, !legacy, changed)
		if legacy {
			require.Equal(t, compactBody, next)
		}
		require.Equal(t, "compaction", gjson.Get(h.Get(openAIWSTurnMetadataHeader), "request_kind").String())
		require.Equal(t, "keep", gjson.GetBytes(next, "prompt_cache_key").String())
	}
}

func TestNormalizeCodexRequestMetadataMap_DetachesRetrySource(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")
	cm := map[string]any{"session_id": "already-scoped", "context_window_id": "keep-window", "window_number": json.Number("18446744073709551615")}
	payload := map[string]any{"client_metadata": cm, "input": []any{map[string]any{"type": "compaction_trigger"}}}
	h := make(http.Header)
	h.Set("session-id", "separate-affinity")
	h.Set("thread-id", "header-thread")
	h.Set(openAIWSTurnMetadataHeader, `{"session_id":"header-session","tool_namespaces_info":{"kept":{"name":"kept"}}}`)
	require.NoError(t, normalizeCodexRequestMetadataMap(payload, h, account, 77))
	require.Len(t, cm, 3)
	require.Equal(t, "already-scoped", cm["session_id"])
	require.Equal(t, "separate-affinity", h.Get("session-id"))
	metadata, err := decodeCodexTurnMetadata(h.Get(openAIWSTurnMetadataHeader))
	require.NoError(t, err)
	require.Equal(t, "already-scoped", metadata["session_id"], "flat body identity wins over header-only metadata")
	require.NotContains(t, metadata, "tool_namespaces_info")
	bodyMetadata := payload["client_metadata"].(map[string]any)[openAIWSTurnMetadataHeader].(string)
	require.True(t, gjson.Get(bodyMetadata, "tool_namespaces_info.kept").Exists(), "header-only namespaces must be retained in body before pruning")
	require.Equal(t, "keep-window", metadata["context_window_id"])
	require.Equal(t, json.Number("18446744073709551615"), metadata["window_number"])
	require.True(t, strings.HasSuffix(metadata["window_id"].(string), ":18446744073709551615"))
	require.Equal(t, "compaction", metadata["request_kind"])
}

func TestNormalizeCodexRequestMetadata_WSRetriesReuseHeaderlessDefaults(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")
	frame := []byte(`{"type":"response.create","input":[{"role":"user","content":"hello"}]}`)
	first, changed, err := normalizeCodexRequestMetadata(frame, nil, account, 77, false)
	require.NoError(t, err)
	require.True(t, changed)
	initialMetadata := gjson.GetBytes(first, "client_metadata.x-codex-turn-metadata").String()
	require.NotEmpty(t, gjson.Get(initialMetadata, "turn_id").String())
	require.NotEmpty(t, gjson.Get(initialMetadata, "turn_started_at_unix_ms").Raw)
	for attempt := 0; attempt < 3; attempt++ {
		h := make(http.Header)
		next, changed, err := normalizeCodexRequestMetadata(first, h, account, 77, false)
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, first, next)
		require.Equal(t, initialMetadata, h.Get(openAIWSTurnMetadataHeader))
		var payload map[string]any
		require.NoError(t, decodeOpenAIJSONUseNumber(next, &payload))
		require.NoError(t, normalizeCodexRequestMetadataMap(payload, h, account, 77))
		require.Equal(t, initialMetadata, payload["client_metadata"].(map[string]any)[openAIWSTurnMetadataHeader])
		projectCodexOutboundHeaders(h, account, true)
		require.Equal(t, initialMetadata, h.Get(openAIWSTurnMetadataHeader))
	}
}

func TestNormalizeCodexRequestMetadata_LegacyCompactUsesExistingBodyIdentity(t *testing.T) {
	account := newMigrationAccount(t, 1, "acct-A")
	body := scopedMigrationBody(t, account, 77, "body-session", "body-thread")
	h := make(http.Header)
	affinity := scopeCodexAccountIdentityValue(account, 77, "session", "cache-affinity")
	h.Set("session-id", affinity)
	next, changed, err := normalizeCodexRequestMetadata(body, h, account, 77, true)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, next, "legacy compact body must remain byte-identical")
	require.Equal(t, affinity, h.Get("session-id"))
	session, thread := codexScopedBodySessionIdentity(body)
	metadata, err := decodeCodexTurnMetadata(h.Get(openAIWSTurnMetadataHeader))
	require.NoError(t, err)
	require.Equal(t, session, metadata["session_id"])
	require.NotEqual(t, affinity, metadata["session_id"])
	require.Equal(t, thread, h.Get("thread-id"))
	require.Equal(t, thread, metadata["thread_id"])
	require.Equal(t, "compaction", metadata["request_kind"])
}

// 非 ASCII 一律写成小写 \uXXXX，代理对拆成两个转义；输出必须全是可打印 ASCII。
func TestMarshalCodexTurnMetadata_EscapesNonASCIIAndStaysHeaderSafe(t *testing.T) {
	out := marshalCodexTurnMetadataString(t,
		`{"installation_id":"caf\u00e9","workspaces":{"C:\\work\\中文🚀":{}},"ctl":"\u0000\u007f\n"}`)

	require.Contains(t, out, `caf\u00e9`)
	require.Contains(t, out, `\u4e2d\u6587`)
	require.Contains(t, out, `\ud83d\ude80`, "星际平面码点必须拆成代理对")
	require.Contains(t, out, `\u0000`)
	require.Contains(t, out, `\u007f`)
	require.Contains(t, out, `\n`)
	for _, b := range []byte(out) {
		require.Truef(t, b >= 0x20 && b < 0x7f, "non-printable ASCII byte 0x%02x in %q", b, out)
	}
}

// 一条按官方线序写好的完整 blob 必须逐字节往返，不被重排或重新转义。
func TestMarshalCodexTurnMetadata_ByteIdenticalRoundTrip(t *testing.T) {
	blob := `{"installation_id":"5f2b1d4e-0a1b-4c3d-8e9f-001122334455",` +
		`"session_id":"019a1f2c-3d4e-7f80-9a1b-2c3d4e5f6071",` +
		`"thread_id":"019a1f2c-3d4e-7f80-9a1b-2c3d4e5f6072",` +
		`"turn_id":"019a1f2c-3d4e-7f80-9a1b-2c3d4e5f6073",` +
		`"window_id":"019a1f2c-3d4e-7f80-9a1b-2c3d4e5f6072:0",` +
		`"window_number":0,` +
		`"context_window_id":"019a1f2c-3d4e-7f80-9a1b-2c3d4e5f6074",` +
		`"request_kind":"turn",` +
		`"sandbox":"workspace-write",` +
		`"sandbox_mode":"workspace-write",` +
		`"auto_review_enabled":false,` +
		`"workspaces":{"/home/dev/proj":{"associated_remote_urls":{"origin":"github.com/acme/proj"},"latest_git_commit_hash":"0f1e2d3c","has_changes":true}},` +
		`"tool_namespaces_info":{"shell":{"name":"shell","functions":{"exec":{"name":"exec","direct":true,"source":"builtin"}}}},` +
		`"turn_started_at_unix_ms":1789302780858,` +
		`"analytics_enabled":true}`

	require.Equal(t, blob, marshalCodexTurnMetadataString(t, blob))
}

func TestDecodeCodexTurnMetadata_RejectsTrailingValues(t *testing.T) {
	_, err := decodeCodexTurnMetadata(`{"a":1} {"b":2}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "multiple JSON values")

	_, err = decodeCodexTurnMetadata(`not json`)
	require.Error(t, err)
}

// 非法 json.Number 必须报错而不是拼出坏 JSON。
func TestMarshalCodexTurnMetadata_RejectsInvalidNumberLiteral(t *testing.T) {
	_, err := marshalCodexTurnMetadata(map[string]any{"bad": json.Number("not-a-number")})
	require.Error(t, err)
}

func TestMarshalCodexTurnMetadata_NilAndEmpty(t *testing.T) {
	out, err := marshalCodexTurnMetadata(nil)
	require.NoError(t, err)
	require.Equal(t, "{}", string(out))

	out, err = marshalCodexTurnMetadata(map[string]any{})
	require.NoError(t, err)
	require.Equal(t, "{}", string(out))
}

// 数组、布尔、null 等叶子类型照常输出，且不被 HTML 转义。
func TestMarshalCodexTurnMetadata_LeafTypes(t *testing.T) {
	out := marshalCodexTurnMetadataString(t,
		`{"installation_id":"i","arr":["a<b",1,true,null],"flag":false,"nil":null}`)

	require.Contains(t, out, `"arr":["a<b",1,true,null]`)
	require.Contains(t, out, `"flag":false`)
	require.Contains(t, out, `"nil":null`)
	require.False(t, strings.Contains(out, `\u003c`))
}
