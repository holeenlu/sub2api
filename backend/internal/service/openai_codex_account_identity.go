package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const codexAccountIdentityNamespaceVersion = "v1"

const codexAccountIdentitySourceContextKey = "openai_codex_account_identity_source"

// prepareCodexAccountIdentitySource resolves credential shadows once per selected
// attempt. The handler reuses gin.Context across failover attempts, so every entry
// point overwrites the staged source before projecting outbound identity.
func (s *OpenAIGatewayService) prepareCodexAccountIdentitySource(ctx context.Context, c *gin.Context, account *Account) (*Account, error) {
	source := account
	if account != nil && account.IsShadow() {
		resolved, err := resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			return nil, err
		}
		source = resolved
	}
	if c != nil {
		c.Set(codexAccountIdentitySourceContextKey, source)
	}
	return source, nil
}

func codexAccountIdentitySource(c *gin.Context, fallback *Account) *Account {
	if c != nil {
		if staged, ok := c.Get(codexAccountIdentitySourceContextKey); ok {
			if source, ok := staged.(*Account); ok && source != nil {
				return source
			}
		}
	}
	return fallback
}

// codexAccountIdentityNamespace returns a stable, credential-scoped namespace.
// Multiple local rows that use the same ChatGPT account intentionally share the
// same namespace. Setup tokens use an irreversible bearer fingerprint because
// they have no refresh lifecycle or imported account metadata. Refreshable OAuth
// otherwise falls back only to a persistent fingerprint seed: local row IDs are
// deployment-relative and must never become upstream identity.
func codexAccountIdentityNamespace(account *Account) string {
	if account == nil || !account.IsOpenAIOAuthLike() {
		return ""
	}
	if upstreamAccountID := strings.TrimSpace(account.GetChatGPTAccountID()); upstreamAccountID != "" {
		if upstreamUserID := strings.TrimSpace(account.GetCredential("chatgpt_user_id")); upstreamUserID != "" {
			return "chatgpt:" + upstreamAccountID + ":user:" + upstreamUserID
		}
		return "chatgpt:" + upstreamAccountID
	}
	if seed, ok := codexFingerprintSeed(account.Extra); ok {
		return "seed:" + seed
	}
	if account.Type == AccountTypeSetupToken {
		if token := strings.TrimSpace(account.GetOpenAIAccessToken()); token != "" {
			sum := sha256.Sum256([]byte("openai-setup-token:" + token))
			return fmt.Sprintf("setup-token:%x", sum[:16])
		}
	}
	return ""
}

// isolateOpenAIUpstreamSessionID preserves the existing API-key isolation while
// adding the selected OAuth credential namespace. A scheduler failover therefore
// cannot send the same session/conversation identity through two upstream accounts.
func isolateOpenAIUpstreamSessionID(apiKeyID int64, account *Account, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	namespace := codexAccountIdentityNamespace(account)
	if namespace == "" {
		return isolateOpenAISessionID(apiKeyID, raw)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("u%d:a%s:%s", apiKeyID, namespace, raw)))
	return fmt.Sprintf("%x", sum[:8])
}

// codexScopedBodySessionIdentity 从**已经作用域化**的请求体里取回最终会话身份。
//
// 身份迁移方案 B 的核心约束是「复用同一个最终值，而不是调用同一个函数」：
// 请求体的 client_metadata 在构造出站请求之前已由
// applyCodexAccountIdentityClientMetadataMap / ...Raw 作用域化，这里原样读回，
// 不做任何二次派生，用来补齐缺失的官方头；明确的客户端 affinity 头由调用方
// 优先保留，不能把其与真实 body session 的合法差异强行覆盖。
//
// 调用方必须自行确认 codexAccountIdentityNamespace(account) != ""：namespace
// 为空时作用域化整体跳过，body 里留的是**客户端原始值**，读出来写到出站头上
// 等于把客户端真实会话 ID 泄露给上游。
func codexScopedBodySessionIdentity(body []byte) (sessionID, threadID string) {
	if len(body) == 0 {
		return "", ""
	}
	metadata := gjson.GetBytes(body, "client_metadata")
	if !metadata.IsObject() {
		return "", ""
	}
	if value := metadata.Get("session_id"); value.Type == gjson.String {
		sessionID = strings.TrimSpace(value.String())
	}
	if value := metadata.Get("thread_id"); value.Type == gjson.String {
		threadID = strings.TrimSpace(value.String())
	}
	// Native clients may carry their identities only in the embedded turn
	// metadata. It has already been scoped alongside the flat metadata fields.
	if embedded, err := decodeCodexTurnMetadata(metadata.Get(openAIWSTurnMetadataHeader).String()); err == nil {
		if sessionID == "" {
			sessionID, _ = embedded["session_id"].(string)
		}
		if threadID == "" {
			threadID, _ = embedded["thread_id"].(string)
		}
	}
	return strings.TrimSpace(sessionID), strings.TrimSpace(threadID)
}

// applyCodexOfficialSessionHeaders 写入官方形态的会话头（连字符）。
//
// 身份迁移第 1 步「补齐，不删除」：下划线的 session_id / conversation_id 保持
// 既有取值不变，这里只补官方实际发送的名字。取不到最终值就什么都不写——
// 不回退到旧的 16-hex（形状不是 UUID），更不回退到客户端原始值。
//
// thread-id 保留独立语义：没有真实 thread 信号时不写，不把所有 thread 等同于
// session。x-client-request-id 在官方 HTTP 上承载 thread_id，同源写入。
func applyCodexOfficialSessionHeaders(h http.Header, sessionID, threadID string) {
	if h == nil {
		return
	}
	// An explicit, already scoped wire identity wins. Native Codex can use a
	// cache-affinity override for session-id while retaining the actual session
	// in turn metadata; those two values need not be identical.
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" && h.Get("session-id") == "" {
		h.Set("session-id", sessionID)
	}
	if threadID = strings.TrimSpace(threadID); threadID != "" && h.Get("thread-id") == "" {
		h.Set("thread-id", threadID)
	}
	if threadID = strings.TrimSpace(h.Get("thread-id")); threadID != "" {
		h.Set("x-client-request-id", threadID)
	}
}

// codexScopedTurnMetadataSessionIdentity 从**已经作用域化**的 x-codex-turn-metadata
// 头里取回最终会话身份。首帧未携带身份时，握手使用这个已有来源补齐。
// applyCodexAccountIdentityHeaders 已经作用域化其中的 session_id / thread_id，
// 这里读回同一份值，不再派生。
func codexScopedTurnMetadataSessionIdentity(h http.Header) (sessionID, threadID string) {
	if h == nil {
		return "", ""
	}
	raw := strings.TrimSpace(h.Get(openAIWSTurnMetadataHeader))
	if raw == "" {
		return "", ""
	}
	metadata, err := decodeCodexTurnMetadata(raw)
	if err != nil || metadata == nil {
		return "", ""
	}
	session, _ := metadata["session_id"].(string)
	thread, _ := metadata["thread_id"].(string)
	return strings.TrimSpace(session), strings.TrimSpace(thread)
}

// applyCodexOfficialSessionHeadersFromScopedTurnMetadata 是 WS 握手侧的组合，带同样的 namespace 守卫。
func applyCodexOfficialSessionHeadersFromScopedTurnMetadata(h http.Header, account *Account) {
	if h == nil || account == nil || !account.UsesOpenAICodexProtocol() {
		return
	}
	if codexAccountIdentityNamespace(account) == "" {
		return
	}
	sessionID, threadID := codexScopedTurnMetadataSessionIdentity(h)
	applyCodexOfficialSessionHeaders(h, sessionID, threadID)
}

// applyCodexOfficialSessionHeadersFromScopedBody 是上面两者的组合，带 namespace 守卫。
func applyCodexOfficialSessionHeadersFromScopedBody(h http.Header, account *Account, scopedBody []byte) {
	if h == nil || account == nil || !account.UsesOpenAICodexProtocol() {
		return
	}
	if codexAccountIdentityNamespace(account) == "" {
		// 没有稳定命名空间的账号本轮不进入迁移，保留旧隔离行为。
		return
	}
	sessionID, threadID := codexScopedBodySessionIdentity(scopedBody)
	applyCodexOfficialSessionHeaders(h, sessionID, threadID)
}

// applyCodexOfficialSessionHeaderFallback is only for original client values.
// Never pass the rebuilt underscore header or an account-scoped prompt cache
// key: either would derive the identity a second time.
func applyCodexOfficialSessionHeaderFallback(h http.Header, account *Account, apiKeyID int64, rawSessionID string) {
	if h == nil || account == nil || !account.UsesOpenAICodexProtocol() ||
		codexAccountIdentityNamespace(account) == "" || h.Get("session-id") != "" {
		return
	}
	applyCodexOfficialSessionHeaders(h, scopeCodexAccountIdentityValue(account, apiKeyID, "session", rawSessionID), "")
}

func scopeCodexAccountIdentityValue(account *Account, apiKeyID int64, kind, raw string) string {
	raw = strings.TrimSpace(raw)
	namespace := codexAccountIdentityNamespace(account)
	if raw == "" || namespace == "" {
		return raw
	}
	// Preserve the native thread:ordinal window shape without changing B-scheme
	// session/thread/cache-key derivation (sub4api a3eaf63, local v4 adaptation).
	if kind == "window" {
		if thread, ordinal, ok := splitCodexWindowID(raw); ok {
			return scopeCodexAccountIdentityValue(account, apiKeyID, "thread", thread) + ":" + ordinal
		}
	}
	return deriveStableUUIDv4(fmt.Sprintf(
		"sub2api:codex-account-identity:%s:user:%d:account:%s:kind:%s:value:%s",
		codexAccountIdentityNamespaceVersion,
		apiKeyID,
		namespace,
		kind,
		raw,
	))
}

var codexAccountIdentityFields = []struct {
	name string
	kind string
}{
	{name: "installation_id", kind: "installation"},
	{name: "x-codex-installation-id", kind: "installation"},
	{name: "session_id", kind: "session"},
	{name: "session-id", kind: "session"},
	{name: "thread_id", kind: "thread"},
	{name: "thread-id", kind: "thread"},
	{name: "parent_thread_id", kind: "thread"},
	{name: "forked_from_thread_id", kind: "thread"},
	{name: "x-codex-parent-thread-id", kind: "thread"},
	{name: "turn_id", kind: "turn"},
	{name: "turn-id", kind: "turn"},
	{name: "parent_turn_id", kind: "turn"},
	{name: "root_turn_id", kind: "turn"},
	{name: "context_window_id", kind: "context-window"},
	{name: "window_id", kind: "window"},
	{name: "x-codex-window-id", kind: "window"},
	{name: "x-client-request-id", kind: "request"},
}

func applyCodexAccountIdentityFields(values map[string]any, account *Account, apiKeyID int64) bool {
	if values == nil || codexAccountIdentityNamespace(account) == "" {
		return false
	}
	changed := false
	for _, field := range codexAccountIdentityFields {
		raw, ok := values[field.name].(string)
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		next := scopeCodexAccountIdentityValue(account, apiKeyID, field.kind, raw)
		if next != raw {
			values[field.name] = next
			changed = true
		}
	}
	return changed
}

func applyCodexAccountIdentityEmbeddedMetadata(values map[string]any, account *Account, apiKeyID int64) bool {
	raw, ok := values[openAIWSTurnMetadataHeader].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return false
	}
	metadata, err := decodeCodexTurnMetadata(raw)
	if err != nil || metadata == nil {
		return false
	}
	if !applyCodexAccountIdentityFields(metadata, account, apiKeyID) {
		return false
	}
	rebuilt, err := marshalCodexTurnMetadata(metadata)
	if err != nil {
		return false
	}
	values[openAIWSTurnMetadataHeader] = string(rebuilt)
	return true
}

func applyCodexAccountIdentityClientMetadataMap(requestBody map[string]any, account *Account, apiKeyID int64) bool {
	if requestBody == nil || codexAccountIdentityNamespace(account) == "" {
		return false
	}
	changed := false
	clientMetadata, _ := requestBody["client_metadata"].(map[string]any)
	originalBodySessionID := ""
	if clientMetadata != nil {
		originalBodySessionID, _ = clientMetadata["session_id"].(string)
		if applyCodexAccountIdentityFields(clientMetadata, account, apiKeyID) {
			changed = true
		}
		if applyCodexAccountIdentityEmbeddedMetadata(clientMetadata, account, apiKeyID) {
			changed = true
		}
	}
	if raw, ok := requestBody["prompt_cache_key"].(string); ok && strings.TrimSpace(raw) != "" {
		kind := "prompt-cache"
		if strings.TrimSpace(originalBodySessionID) != "" && raw == originalBodySessionID {
			kind = "session"
		}
		next := scopeCodexAccountIdentityValue(account, apiKeyID, kind, raw)
		if next != raw {
			requestBody["prompt_cache_key"] = next
			changed = true
		}
	}
	return changed
}

// applyCodexAccountIdentityClientMetadataRaw scopes only the small identity
// subobjects with gjson/sjson. The passthrough hot path never unmarshals the
// potentially multi-megabyte request body.
func applyCodexAccountIdentityClientMetadataRaw(body []byte, account *Account, apiKeyID int64) ([]byte, bool, error) {
	if len(body) == 0 || codexAccountIdentityNamespace(account) == "" {
		return body, false, nil
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return body, false, nil
	}

	next := body
	changed := false
	originalBodySessionID := ""
	if cm := gjson.GetBytes(body, "client_metadata"); cm.IsObject() {
		clientMetadata := map[string]any{}
		if err := decodeOpenAIJSONUseNumber([]byte(cm.Raw), &clientMetadata); err != nil {
			return body, false, fmt.Errorf("decode client_metadata for account identity: %w", err)
		}
		originalBodySessionID, _ = clientMetadata["session_id"].(string)
		metadataChanged := applyCodexAccountIdentityFields(clientMetadata, account, apiKeyID)
		if applyCodexAccountIdentityEmbeddedMetadata(clientMetadata, account, apiKeyID) {
			metadataChanged = true
		}
		if metadataChanged {
			raw, err := json.Marshal(clientMetadata)
			if err != nil {
				return body, false, fmt.Errorf("encode account-scoped client_metadata: %w", err)
			}
			var setErr error
			next, setErr = sjson.SetRawBytes(next, "client_metadata", raw)
			if setErr != nil {
				return body, false, fmt.Errorf("splice account-scoped client_metadata: %w", setErr)
			}
			changed = true
		}
	}
	if promptCacheKey := gjson.GetBytes(body, "prompt_cache_key"); promptCacheKey.Type == gjson.String && strings.TrimSpace(promptCacheKey.String()) != "" {
		raw := promptCacheKey.String()
		kind := "prompt-cache"
		if strings.TrimSpace(originalBodySessionID) != "" && raw == originalBodySessionID {
			kind = "session"
		}
		scoped := scopeCodexAccountIdentityValue(account, apiKeyID, kind, raw)
		if scoped != raw {
			rewritten, err := sjson.SetBytes(next, "prompt_cache_key", scoped)
			if err != nil {
				return body, false, fmt.Errorf("splice account-scoped prompt_cache_key: %w", err)
			}
			next = rewritten
			changed = true
		}
	}
	return next, changed, nil
}

func applyCodexAccountIdentityHeaders(headers http.Header, account *Account, apiKeyID int64) {
	if headers == nil {
		return
	}
	if codexAccountIdentityNamespace(account) == "" {
		if account != nil && account.UsesOpenAICodexProtocol() {
			// These newly admitted headers must not bypass the legacy API-key
			// isolation when credential identity is unavailable.
			if raw := strings.TrimSpace(headers.Get("session-id")); raw != "" && headers.Get("session_id") == "" {
				headers.Set("session_id", isolateOpenAIUpstreamSessionID(apiKeyID, account, raw))
			}
			headers.Del("session-id")
			headers.Del("thread-id")
			headers.Del("x-client-request-id")
			headers.Del("x-codex-parent-thread-id")
		}
		return
	}
	for _, field := range codexAccountIdentityFields {
		// Underscore session/conversation headers are rebuilt separately from the
		// prompt cache key by each request builder.
		if field.name == "session_id" {
			continue
		}
		raw := strings.TrimSpace(headers.Get(field.name))
		if raw != "" {
			headers.Set(field.name, scopeCodexAccountIdentityValue(account, apiKeyID, field.kind, raw))
		}
	}
	if raw := strings.TrimSpace(headers.Get(openAIWSTurnMetadataHeader)); raw != "" {
		metadata, err := decodeCodexTurnMetadata(raw)
		if err == nil && metadata != nil && applyCodexAccountIdentityFields(metadata, account, apiKeyID) {
			if rebuilt, err := marshalCodexTurnMetadata(metadata); err == nil {
				headers.Set(openAIWSTurnMetadataHeader, string(rebuilt))
			}
		}
	}
	applyCodexOfficialSessionHeaders(headers, "", "")
}
