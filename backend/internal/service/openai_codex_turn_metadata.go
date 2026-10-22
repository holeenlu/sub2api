package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Turn metadata is also used as an HTTP header, including when carried in WS JSON.
//
// 官方 Codex 用 Rust serde 序列化 CodexTurnMetadataPayload，线序由 struct 的
// 字段声明顺序决定，未知键走 #[serde(flatten)] 的 BTreeMap（字典序、排在末尾）。
// Go 的 json.Marshal 对 map 恒为字母序，与官方线序不一致；同时 Go 默认会把
// & < > 转义成 & < >，serde 不会。两者都是本代理独有的可判别
// 特征，所以这里不用标准库的 map 序列化，显式复刻官方线序。

// codexTurnMetadataKeyOrder 复刻官方 CodexTurnMetadataPayload 的字段声明顺序
// （codex-rs core/src/responses_metadata.rs，全部 skip_serializing_if=none）。
var codexTurnMetadataKeyOrder = [...]string{
	"installation_id",
	"session_id",
	"thread_id",
	"agent_name",
	"turn_id",
	"window_id",
	"window_number",
	"context_window_id",
	"request_kind",
	"history_initialization",
	"forked_from_thread_id",
	"forked_from_ordinal_exclusive",
	"parent_thread_id",
	"parent_turn_id",
	"root_turn_id",
	"subagent_kind",
	"thread_source",
	"turn_trigger",
	"sandbox",
	"sandbox_mode",
	"auto_review_enabled",
	"node_repl_auto_review_required",
	"node_repl_disabled",
	"workspaces",
	"tool_namespaces_info",
	"turn_started_at_unix_ms",
	"history_ingest_requested",
	"analytics_enabled",
	"compaction",
}

// codexJSONShape 描述一类官方 JSON 对象的线序形状。serde 语义下：
//   - order 非空：该层是 struct，键按 order（声明序）输出，order 之外的未知键
//     按字典序追加（复刻 flatten 的 BTreeMap）。
//   - order 为空：该层是 BTreeMap，键按字典序输出。
//   - fields：struct 内某键指向的子对象形状（缺省为叶子）。
//   - values：BTreeMap 的每个 value 指向的子对象形状（缺省为叶子）。
type codexJSONShape struct {
	order  []string
	fields map[string]string
	values string
}

// codexJSONShapes 复刻官方 turn metadata 里出现的各类对象形状。
// 依据 codex-rs core/src/responses_metadata.rs（payload、workspace、
// tool namespace）与 analytics 侧的 compaction 枚举。
var codexJSONShapes = map[string]codexJSONShape{
	"": { // CodexTurnMetadataPayload 顶层
		order: codexTurnMetadataKeyOrder[:],
		fields: map[string]string{
			"workspaces":           "workspaces",
			"tool_namespaces_info": "tool_namespaces_info",
			"compaction":           "compaction",
		},
	},
	"workspaces":           {values: "workspace"}, // BTreeMap<path, TurnMetadataWorkspace>
	"workspace":            {order: []string{"associated_remote_urls", "latest_git_commit_hash", "has_changes"}, fields: map[string]string{"associated_remote_urls": "remote_urls"}},
	"remote_urls":          {},                    // BTreeMap<name, SanitizedGitUrl(String)>
	"tool_namespaces_info": {values: "namespace"}, // BTreeMap<name, TurnToolNamespaceInfo>
	"namespace":            {order: []string{"name", "functions"}, fields: map[string]string{"functions": "functions"}},
	"functions":            {values: "function"}, // BTreeMap<name, TurnToolFunctionInfo>
	"function":             {order: []string{"name", "direct", "code_mode_name", "deferred", "source"}},
	"compaction":           {order: []string{"trigger", "reason", "implementation", "phase", "strategy"}},
}

// orderedCodexJSONKeys 先按官方顺序取已出现的键，再把未知键按字典序追加，
// 复刻 serde struct + flatten(BTreeMap) 的混合输出。order 为空时退化为纯字典序。
func orderedCodexJSONKeys(object map[string]any, order []string) []string {
	keys := make([]string, 0, len(object))
	var seen map[string]struct{}
	if len(order) > 0 {
		seen = make(map[string]struct{}, len(order))
		for _, key := range order {
			if _, exists := object[key]; exists {
				keys = append(keys, key)
				seen[key] = struct{}{}
			}
		}
	}
	rest := make([]string, 0, len(object)-len(seen))
	for key := range object {
		if _, ok := seen[key]; !ok {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

// writeCodexJSONString 按 Go 标准库的转义规则写出一个 JSON 字符串，但不转义
// & < >（官方 serde 也不转义；标准库的 HTML 转义是本代理独有的特征）。
// 非 ASCII 留给 escapeCodexNonASCII 统一处理。
//
// 控制字符采用 serde 的短转义；DEL 仍由 header-safe 后处理转义。
func writeCodexJSONString(buf *bytes.Buffer, value string) {
	buf.WriteByte('"')
	start := 0
	for i := range len(value) {
		switch b := value[i]; b {
		case '"', '\\':
			buf.WriteString(value[start:i])
			buf.WriteByte('\\')
			buf.WriteByte(b)
			start = i + 1
		case '\b':
			buf.WriteString(value[start:i])
			buf.WriteString(`\b`)
			start = i + 1
		case '\f':
			buf.WriteString(value[start:i])
			buf.WriteString(`\f`)
			start = i + 1
		case '\n':
			buf.WriteString(value[start:i])
			buf.WriteString(`\n`)
			start = i + 1
		case '\r':
			buf.WriteString(value[start:i])
			buf.WriteString(`\r`)
			start = i + 1
		case '\t':
			buf.WriteString(value[start:i])
			buf.WriteString(`\t`)
			start = i + 1
		default:
			if b < 0x20 {
				buf.WriteString(value[start:i])
				fmt.Fprintf(buf, `\u%04x`, b)
				start = i + 1
			}
		}
	}
	buf.WriteString(value[start:])
	buf.WriteByte('"')
}

// writeCodexJSONLeaf 输出标量与数组。字符串走 writeCodexJSONString；其余类型
// （数字、布尔、null、数组）交给关闭 HTML 转义的编码器，由它负责校验合法性
// ——json.Number 可能是调用方自行构造的非法字面量，不能直接拼进缓冲区。
func writeCodexJSONLeaf(buf *bytes.Buffer, value any) error {
	if s, ok := value.(string); ok {
		writeCodexJSONString(buf, s)
		return nil
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	out := encoded.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	buf.Write(out)
	return nil
}

func writeCodexJSONObject(buf *bytes.Buffer, object map[string]any, kind string) error {
	shape := codexJSONShapes[kind]
	keys := orderedCodexJSONKeys(object, shape.order)
	buf.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		writeCodexJSONString(buf, key)
		buf.WriteByte(':')
		child, ok := object[key].(map[string]any)
		if !ok {
			if err := writeCodexJSONLeaf(buf, object[key]); err != nil {
				return err
			}
			continue
		}
		childKind := shape.fields[key]
		if childKind == "" {
			childKind = shape.values
		}
		if err := writeCodexJSONObject(buf, child, childKind); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

// escapeCodexNonASCII 把非 ASCII 码点写成小写 \uXXXX，复刻官方
// to_ascii_json_string（codex-rs utils/string/src/json.rs）。
//
// U+007F 官方按 ASCII 原样保留，这里一并转义：该值要作为 HTTP 头发送，
// 裸 DEL 字节在头里不安全。这是本仓库刻意保留的本地取舍，由
// TestCodexTurnMetadataRewritePreservesHeaderSafeJSON 守住。
func escapeCodexNonASCII(raw []byte) []byte {
	for i, b := range raw {
		if b < 0x7f {
			continue
		}
		out := make([]byte, 0, len(raw)+16)
		out = append(out, raw[:i]...)
		appendEscape := func(r rune) {
			const hex = "0123456789abcdef"
			out = append(out, '\\', 'u', hex[r>>12&15], hex[r>>8&15], hex[r>>4&15], hex[r&15])
		}
		for _, r := range string(raw[i:]) {
			switch {
			case r < 0x7f:
				out = append(out, byte(r))
			case r <= 0xffff:
				appendEscape(r)
			default:
				high, low := utf16.EncodeRune(r)
				appendEscape(high)
				appendEscape(low)
			}
		}
		return out
	}
	return raw
}

func marshalCodexTurnMetadata(metadata map[string]any) ([]byte, error) {
	if metadata == nil {
		return []byte("{}"), nil
	}
	var buf bytes.Buffer
	if err := writeCodexJSONObject(&buf, metadata, ""); err != nil {
		return nil, err
	}
	return escapeCodexNonASCII(buf.Bytes()), nil
}

// decodeCodexTurnMetadata 解析客户端带来的 turn metadata。
//
// 必须用 UseNumber：默认解码把所有数字变成 float64，重新序列化时
// turn_started_at_unix_ms 这类整数会被按浮点规则重新格式化，超过 2^53 的值
// 还会直接丢精度。只改写身份字段时，其余字段必须原样往返。
// 尾随多余 JSON 值视为非法输入，不静默取第一个。
func decodeCodexTurnMetadata(raw string) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var metadata map[string]any
	if err := decoder.Decode(&metadata); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("multiple JSON values in Codex turn metadata")
	}
	return metadata, nil
}

// The normalization/defaulting and header-only namespace pruning are adapted
// from sub4api a3eaf63afc672ffedf7844e6cfc5cb6750d52a3e. Inputs are already scoped
// (and optionally converged). Existing IDs and cache keys are never re-derived.
// Unlike that source, fallback IDs use our credential/caller namespace and v4
// derivation; no account-row identity, v7 migration or fabricated telemetry.
func normalizeCodexRequestMetadata(body []byte, headers http.Header, account *Account, apiKeyID int64, isCompact bool) ([]byte, bool, error) {
	if account == nil || !account.UsesOpenAICodexProtocol() || codexAccountIdentityNamespace(account) == "" || !gjson.ParseBytes(body).IsObject() {
		return body, false, nil
	}
	cm := map[string]any{}
	if raw := gjson.GetBytes(body, "client_metadata"); raw.IsObject() {
		// Legacy compact must not gain/rewrite client_metadata, but any existing
		// scoped body identity still owns header metadata over cache affinity.
		if err := decodeOpenAIJSONUseNumber([]byte(raw.Raw), &cm); err != nil {
			return body, false, err
		}
	} else if raw.Exists() && raw.Type != gjson.Null && !isCompact {
		return body, false, nil
	}
	changed, err := normalizeCodexClientMetadata(cm, headers, body, account, apiKeyID, isCompact || HasCompactionTriggerInInput(body))
	if err != nil || !changed || isCompact {
		// Legacy compact does not admit client_metadata. Project only headers.
		return body, false, err
	}
	raw, err := json.Marshal(cm)
	if err != nil {
		return body, false, err
	}
	next, err := sjson.SetRawBytes(body, "client_metadata", raw)
	if err != nil {
		return body, false, err
	}
	return next, true, nil
}

func normalizeCodexRequestMetadataMap(payload map[string]any, headers http.Header, account *Account, apiKeyID int64) error {
	if payload == nil || account == nil || !account.UsesOpenAICodexProtocol() || codexAccountIdentityNamespace(account) == "" {
		return nil
	}
	cm, ok := payload["client_metadata"].(map[string]any)
	if !ok && payload["client_metadata"] != nil {
		return nil
	}
	cm = maps.Clone(cm) // WS payloads can share their source map with a retry.
	if cm == nil {
		cm = map[string]any{}
	}
	// Most frames already have a scoped session. Do not serialize their full
	// input once here and again at send time just to fill small metadata fields.
	var body []byte
	flatSession, _ := cm["session_id"].(string)
	hasMetadataSession := strings.TrimSpace(flatSession) != ""
	flatThread, _ := cm["thread_id"].(string)
	hasThread := strings.TrimSpace(flatThread) != "" || strings.TrimSpace(headers.Get("thread-id")) != ""
	for _, raw := range []string{firstNonEmptyString(cm[openAIWSTurnMetadataHeader]), headers.Get(openAIWSTurnMetadataHeader)} {
		if hasMetadataSession {
			break
		}
		if metadata, err := decodeCodexTurnMetadata(raw); err == nil {
			if session, ok := metadata["session_id"].(string); ok && strings.TrimSpace(session) != "" {
				hasMetadataSession = true
			}
			if thread, ok := metadata["thread_id"].(string); ok && strings.TrimSpace(thread) != "" {
				hasThread = true
			}
		}
	}
	hasSession := hasMetadataSession || strings.TrimSpace(headers.Get("session-id")) != ""
	if !hasSession || (!hasMetadataSession && !hasThread) {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	isCompact := false
	if input, ok := payload["input"].([]any); ok {
		for _, raw := range input {
			if item, ok := raw.(map[string]any); ok && item["type"] == "compaction_trigger" {
				isCompact = true
				break
			}
		}
	}
	changed, err := normalizeCodexClientMetadata(cm, headers, body, account, apiKeyID, isCompact)
	if err == nil && changed {
		payload["client_metadata"] = cm
	}
	return err
}

// mergeCodexTurnMetadata reads already-scoped metadata without adding defaults
// or mutating either source. Body fields take precedence over header additions.
func mergeCodexTurnMetadata(cm map[string]any, headers http.Header) map[string]any {
	metadata := map[string]any{}
	if raw, ok := cm[openAIWSTurnMetadataHeader].(string); ok {
		if decoded, err := decodeCodexTurnMetadata(raw); err == nil && decoded != nil {
			metadata = decoded
		}
	}
	for _, key := range codexTurnMetadataKeyOrder {
		if _, exists := metadata[key]; !exists {
			if value, present := cm[key]; present {
				metadata[key] = value
			}
		}
	}
	for _, pair := range [][2]string{{"installation_id", "x-codex-installation-id"}, {"window_id", "x-codex-window-id"}} {
		if _, exists := metadata[pair[0]]; !exists {
			if value, present := cm[pair[1]]; present {
				metadata[pair[0]] = value
			}
		}
	}
	if decoded, err := decodeCodexTurnMetadata(headers.Get(openAIWSTurnMetadataHeader)); err == nil {
		for key, value := range decoded {
			if _, exists := metadata[key]; !exists {
				metadata[key] = value
			}
		}
	}
	return metadata
}

// normalizeCodexClientMetadata merges embedded body metadata before header-only
// additions. An explicit official session-id is cache affinity and is never
// overwritten by the body's session. Header pruning never mutates body data.
func normalizeCodexClientMetadata(cm map[string]any, headers http.Header, body []byte, account *Account, apiKeyID int64, isCompact bool) (bool, error) {
	metadata := mergeCodexTurnMetadata(cm, headers)
	first := func(values ...any) string {
		for _, value := range values {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		return ""
	}
	installation := first(metadata["installation_id"], cm["x-codex-installation-id"], cm["installation_id"], headers.Get("x-codex-installation-id"))
	if installation == "" {
		if device := strings.TrimSpace(account.GetOpenAIDeviceID()); device != "" {
			installation = scopeCodexAccountIdentityValue(account, apiKeyID, "installation", device)
		} else {
			installation = deriveStableUUIDv4("sub2api:codex-default-installation:v1:" + codexAccountIdentityNamespace(account))
		}
	}
	metadataSession := first(metadata["session_id"], cm["session_id"])
	session := first(metadataSession, headers.Get("session-id"))
	thread := first(metadata["thread_id"], cm["thread_id"], headers.Get("thread-id"))
	threadSeed := "fallback:" + metadataSession
	if session == "" || (thread == "" && metadataSession == "") {
		anchor := deriveOpenAIAnchoredContentSessionSeed(body)
		if previous := gjson.GetBytes(body, "previous_response_id"); previous.Type == gjson.String && previous.String() != "" {
			anchor = "response:" + previous.String()
		}
		if anchor == "" {
			anchor = fmt.Sprintf("body:%x", sha256.Sum256(body))
		}
		threadSeed = "fallback:" + anchor
		if session == "" {
			session = scopeCodexAccountIdentityValue(account, apiKeyID, "session", threadSeed)
		}
	}
	if thread == "" {
		// A wire session may only be cache affinity. Without actual metadata
		// session/thread identity, derive a caller-scoped thread from body anchor.
		thread = scopeCodexAccountIdentityValue(account, apiKeyID, "thread", threadSeed)
	}
	turn := first(metadata["turn_id"], cm["turn_id"], headers.Get("turn-id"))
	if turn == "" {
		turn = uuid.NewString()
	}
	window := first(metadata["window_id"], cm["x-codex-window-id"], cm["window_id"], headers.Get("x-codex-window-id"))
	ordinal := "0"
	if _, n, ok := splitCodexWindowID(window); ok {
		ordinal = n
	} else if number, exists := metadata["window_number"]; exists {
		if n := fmt.Sprint(number); validCodexWindowOrdinal(n) {
			ordinal = n
		}
	}
	if window == "" {
		window = thread + ":" + ordinal
	}
	changed := false
	set := func(values map[string]any, key, value string) {
		if old, ok := values[key].(string); !ok || old != value {
			values[key] = value
			changed = true
		}
	}
	for _, field := range []struct{ name, flat, value string }{
		{"installation_id", "x-codex-installation-id", installation},
		{"session_id", "session_id", session}, {"thread_id", "thread_id", thread},
		{"turn_id", "turn_id", turn}, {"window_id", "x-codex-window-id", window},
	} {
		set(cm, field.flat, field.value)
		if _, exists := cm[field.name]; exists {
			set(cm, field.name, field.value)
		}
		set(metadata, field.name, field.value)
	}
	for _, field := range []string{"parent_thread_id", "parent_turn_id", "root_turn_id", "forked_from_thread_id", "subagent_kind"} {
		if value := first(metadata[field], cm[field]); value != "" {
			set(metadata, field, value)
		}
	}
	if parent := first(metadata["parent_thread_id"], headers.Get("x-codex-parent-thread-id")); parent != "" {
		set(metadata, "parent_thread_id", parent)
	}
	if isCompact {
		set(metadata, "request_kind", "compaction")
		if _, exists := cm["request_kind"]; exists {
			set(cm, "request_kind", "compaction")
		}
	} else if first(metadata["request_kind"]) == "" {
		set(metadata, "request_kind", "turn")
	}
	if _, exists := metadata["window_number"]; !exists {
		// Parse as uint64, then store the exact literal, including large ordinals.
		number, _ := strconv.ParseUint(ordinal, 10, 64)
		metadata["window_number"] = json.Number(strconv.FormatUint(number, 10))
	}
	if first(metadata["context_window_id"]) == "" {
		metadata["context_window_id"] = scopeCodexAccountIdentityValue(account, apiKeyID, "context-window", window)
	}
	if _, exists := metadata["turn_started_at_unix_ms"]; !exists {
		metadata["turn_started_at_unix_ms"] = time.Now().UnixMilli()
	}
	raw, err := marshalCodexTurnMetadata(metadata)
	if err != nil {
		return false, err
	}
	set(cm, openAIWSTurnMetadataHeader, string(raw))
	if headers != nil {
		applyCodexOfficialSessionHeaders(headers, session, thread)
		// Body metadata owns thread identity; only session-id has independent
		// explicit cache-affinity semantics.
		headers.Set("thread-id", thread)
		headers.Set("x-client-request-id", thread)
		headers.Set("x-codex-window-id", window)
		if parent := first(metadata["parent_thread_id"]); parent != "" {
			headers.Set("x-codex-parent-thread-id", parent)
		}
		delete(metadata, "tool_namespaces_info") // decoded copy; cm keeps full raw.
		headerRaw, err := marshalCodexTurnMetadata(metadata)
		if err != nil {
			return false, err
		}
		headers.Set(openAIWSTurnMetadataHeader, string(headerRaw))
	}
	return changed, nil
}

type codexRequestMetadataBodyKey struct{}

// codexNormalizedRequestBody returns the immutable final body staged by normalization.
// The request builder cannot return a second byte slice through its existing API;
// its caller retrieves this immediately after building and reuses it for retries
// on the same account. A credential failover must still start from client input.
func codexNormalizedRequestBody(req *http.Request, fallback []byte) []byte {
	if req != nil {
		if final, ok := req.Context().Value(codexRequestMetadataBodyKey{}).([]byte); ok {
			return final
		}
	}
	return fallback
}

// applyCodexRequestMetadata keeps the actual HTTP body, replay reader and caller
// snapshot identical before the parent's final header/transport projection.
func applyCodexRequestMetadata(req *http.Request, body []byte, account *Account, apiKeyID int64, isCompact bool) ([]byte, error) {
	if req == nil {
		return body, nil
	}
	next, changed, err := normalizeCodexRequestMetadata(body, req.Header, account, apiKeyID, isCompact)
	if err != nil || !changed {
		return body, err
	}
	if req.Body != nil {
		_ = req.Body.Close()
	}
	req.Body = io.NopCloser(bytes.NewReader(next))
	req.ContentLength = int64(len(next))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(next)), nil }
	*req = *req.WithContext(context.WithValue(req.Context(), codexRequestMetadataBodyKey{}, next))
	return next, nil
}
