package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unsafe"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func validateOpenAIWSBearerToken(account *Account, token string) error {
	if account == nil {
		return errors.New("account is nil")
	}
	if strings.TrimSpace(token) == "" && !account.IsOpenAIAgentIdentity() {
		return errors.New("token is empty")
	}
	return nil
}

func (s *OpenAIGatewayService) buildOpenAIResponsesWSURL(account *Account) (string, error) {
	if account == nil {
		return "", errors.New("account is nil")
	}
	var targetURL string
	switch account.Type {
	case AccountTypeOAuth:
		targetURL = chatgptCodexURL
	case AccountTypeSetupToken:
		if account.IsOpenAIOAuthLike() {
			targetURL = chatgptCodexURL
		} else {
			targetURL = openaiPlatformAPIURL
		}
	case AccountTypeAPIKey:
		baseURL := account.GetOpenAIBaseURL()
		if account.UsesNativeCNResponses() && account.IsAdaptiveAPIProtocol() {
			baseURL = account.GetCNProtocolBaseURL(APIProtocolResponses)
		}
		if baseURL == "" {
			targetURL = openaiPlatformAPIURL
		} else {
			validatedURL, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return "", err
			}
			targetURL = buildOpenAIResponsesURLForPlatform(account.Platform, validatedURL)
		}
	default:
		targetURL = openaiPlatformAPIURL
	}

	parsed, err := url.Parse(strings.TrimSpace(targetURL))
	if err != nil {
		return "", fmt.Errorf("invalid target url: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "wss", "ws":
		// 保持不变
	default:
		return "", fmt.Errorf("unsupported scheme for ws: %s", parsed.Scheme)
	}
	return parsed.String(), nil
}

func (s *OpenAIGatewayService) buildOpenAIWSHeaders(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token string,
	decision OpenAIWSProtocolDecision,
	isCodexCLI bool,
	turnState string,
	turnMetadata string,
	promptCacheKey string,
	routingModel string,
	routingServiceTier string,
	scopedBody []byte,
) (http.Header, openAIWSSessionHeaderResolution, error) {
	headers := make(http.Header)
	if account == nil || !account.IsOpenAIAgentIdentity() {
		headers.Set("authorization", "Bearer "+token)
	}

	sessionResolution := resolveOpenAIWSSessionHeaders(c, promptCacheKey)
	if c != nil && c.Request != nil {
		if v := strings.TrimSpace(c.Request.Header.Get("accept-language")); v != "" {
			headers.Set("accept-language", v)
		}
		for _, value := range c.Request.Header.Values("x-codex-beta-features") {
			if value = strings.TrimSpace(value); value != "" {
				headers.Add("x-codex-beta-features", value)
			}
		}
		for _, name := range [...]string{
			"x-codex-window-id",
			"x-codex-installation-id",
			"session-id",
			"thread-id",
			"x-client-request-id",
		} {
			if value := c.Request.Header.Get(name); strings.TrimSpace(value) != "" {
				headers.Set(name, value)
			}
		}
		copyCodexCallerIntentHeaders(headers, c.Request.Header, account)
	}
	// 真实 Codex 的 WS 握手同样携带会话级 x-codex-beta-features
	// （client.rs build_websocket_headers 复用 build_responses_headers），
	// 客户端未声明时补成默认形态，与 HTTP 出站保持一致。放在客户端头拷贝
	// 之外：该头是账号/会话级属性，不依赖入站请求是否存在，也避免预热与
	// 实际请求因头差异落进不同的连接池兼容分桶。
	applyOpenAICodexBetaFeatures(c, account, headers)
	// OAuth 账号：将 apiKeyID 混入 session 标识符，防止跨用户会话碰撞。
	if account != nil && account.UsesOpenAICodexProtocol() {
		apiKeyID := getAPIKeyIDFromContext(c)
		if sessionResolution.SessionID != "" {
			headers.Set("session_id", isolateOpenAIUpstreamSessionID(apiKeyID, codexAccountIdentitySource(c, account), sessionResolution.SessionID))
		}
		if sessionResolution.ConversationID != "" {
			headers.Set("conversation_id", isolateOpenAIUpstreamSessionID(apiKeyID, codexAccountIdentitySource(c, account), sessionResolution.ConversationID))
		}
	} else {
		if sessionResolution.SessionID != "" {
			headers.Set("session_id", sessionResolution.SessionID)
		}
		if sessionResolution.ConversationID != "" {
			headers.Set("conversation_id", sessionResolution.ConversationID)
		}
	}
	if state := strings.TrimSpace(turnState); state != "" {
		headers.Set(openAIWSTurnStateHeader, state)
	}
	if metadata := strings.TrimSpace(turnMetadata); metadata != "" {
		headers.Set(openAIWSTurnMetadataHeader, metadata)
	}
	applyCodexAccountIdentityHeaders(headers, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c))
	// The ingress has already normalized and scoped its first frame before
	// dialing. Reuse that identity even when no turn-metadata header was sent.
	applyCodexOfficialSessionHeadersFromScopedBody(headers, codexAccountIdentitySource(c, account), scopedBody)
	applyCodexOfficialSessionHeadersFromScopedTurnMetadata(headers, codexAccountIdentitySource(c, account))
	applyCodexOfficialSessionHeaderFallback(headers, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c), sessionResolution.SessionID)
	applyStagedCodexFingerprintHeaders(c, account, headers)

	if account != nil && account.UsesOpenAICodexProtocol() {
		if err := resolveAndSetOpenAIChatGPTAccountHeaders(ctx, s.accountRepo, headers, account); err != nil {
			return nil, sessionResolution, fmt.Errorf("resolve chatgpt account headers: %w", err)
		}
		headers.Set("originator", resolveOpenAIUpstreamOriginator(c, isCodexCLI))
	}

	betaValue := openAIWSBetaV2Value
	if decision.Transport == OpenAIUpstreamTransportResponsesWebsocket {
		betaValue = openAIWSBetaV1Value
	}
	headers.Set("OpenAI-Beta", betaValue)

	customUA := ""
	if account != nil {
		customUA = account.GetOpenAIUserAgent()
	}
	if strings.TrimSpace(customUA) != "" {
		headers.Set("user-agent", customUA)
	} else if c != nil {
		if ua := strings.TrimSpace(c.GetHeader("User-Agent")); ua != "" {
			headers.Set("user-agent", ua)
		}
	}
	if s != nil && s.cfg != nil && s.cfg.Gateway.ForceCodexCLI {
		headers.Set("user-agent", CodexCanonicalUserAgent())
	}
	// 终态收口：WS 握手与 HTTP 出站共用同一套身份语义，账号级自定义 UA 同样作为
	// 管理员显式配置传入（上面写进 headers 的值只在强制统一被关闭时才参与配对）。
	if account != nil && account.UsesOpenAICodexProtocol() {
		enforceCodexIdentityHeadersWithUA(headers, s.codexIdentityOverrideUA(account))
	}

	// 账号级请求头覆写（仅 openai api_key 账号启用时生效；OAuth 路径 no-op）。
	// 覆盖所有 WS 模式（ctx_pool/dedicated/passthrough）的握手头。
	account.ApplyHeaderOverrides(headers)
	setOpenAICodexRoutingHint(headers, account, routingModel, routingServiceTier)
	logOpenAIRoutingDiagnostics(
		ctx,
		account,
		string(decision.Transport),
		routingModel,
		routingServiceTier,
		strings.TrimSpace(headers.Get(openAICodexRoutingHintHeader)) != "",
		"soft_routing_hint",
	)

	// The caller normalizes the actual frame with these already-scoped headers,
	// then projects the final wire headers immediately before dialing. Projecting
	// here would discard header-only installation metadata before it reaches body.
	s.guardOpenAICodexTurnStateEcho(c, account, headers)
	return headers, sessionResolution, nil
}

func (s *OpenAIGatewayService) buildOpenAIWSCreatePayload(reqBody map[string]any, account *Account) map[string]any {
	// OpenAI WS Mode 协议：response.create 字段与 HTTP /responses 基本一致。
	// 保留 stream 字段（与 Codex CLI 一致），仅移除 background。
	payload := make(map[string]any, len(reqBody)+1)
	for k, v := range reqBody {
		payload[k] = v
	}

	delete(payload, "background")
	if _, exists := payload["stream"]; !exists {
		payload["stream"] = true
	}
	payload["type"] = "response.create"

	// OAuth 默认保持 store=false，避免误依赖服务端历史。
	if account != nil && account.UsesOpenAICodexProtocol() && !s.isOpenAIWSStoreRecoveryAllowed(account) {
		payload["store"] = false
	}
	return payload
}

func setOpenAIWSTurnMetadata(payload map[string]any, turnMetadata string) {
	if len(payload) == 0 {
		return
	}
	metadata := strings.TrimSpace(turnMetadata)
	if metadata == "" {
		return
	}

	switch existing := payload["client_metadata"].(type) {
	case map[string]any:
		// buildOpenAIWSCreatePayload shallow-copies the request. Detach this
		// mutable object before turn metadata or scoped identities are applied,
		// so a retry never starts from a previous attempt's rewritten map.
		next := make(map[string]any, len(existing)+1)
		for k, v := range existing {
			next[k] = v
		}
		next[openAIWSTurnMetadataHeader] = metadata
		payload["client_metadata"] = next
	case map[string]string:
		next := make(map[string]any, len(existing)+1)
		for k, v := range existing {
			next[k] = v
		}
		next[openAIWSTurnMetadataHeader] = metadata
		payload["client_metadata"] = next
	default:
		payload["client_metadata"] = map[string]any{
			openAIWSTurnMetadataHeader: metadata,
		}
	}
}

func (s *OpenAIGatewayService) isOpenAIWSStoreRecoveryAllowed(account *Account) bool {
	if account != nil && account.IsOpenAIWSAllowStoreRecoveryEnabled() {
		return true
	}
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.AllowStoreRecovery {
		return true
	}
	return false
}

func (s *OpenAIGatewayService) isOpenAIWSStoreDisabledInRequest(reqBody map[string]any, account *Account) bool {
	if account != nil && account.UsesOpenAICodexProtocol() && !s.isOpenAIWSStoreRecoveryAllowed(account) {
		return true
	}
	if len(reqBody) == 0 {
		return false
	}
	rawStore, ok := reqBody["store"]
	if !ok {
		return false
	}
	storeEnabled, ok := rawStore.(bool)
	if !ok {
		return false
	}
	return !storeEnabled
}

func (s *OpenAIGatewayService) isOpenAIWSStoreDisabledInRequestRaw(reqBody []byte, account *Account) bool {
	if account != nil && account.UsesOpenAICodexProtocol() && !s.isOpenAIWSStoreRecoveryAllowed(account) {
		return true
	}
	if len(reqBody) == 0 {
		return false
	}
	storeValue := gjson.GetBytes(reqBody, "store")
	if !storeValue.Exists() {
		return false
	}
	if storeValue.Type != gjson.True && storeValue.Type != gjson.False {
		return false
	}
	return !storeValue.Bool()
}

func (s *OpenAIGatewayService) openAIWSStoreDisabledConnMode() string {
	if s == nil || s.cfg == nil {
		return openAIWSStoreDisabledConnModeStrict
	}
	mode := strings.ToLower(strings.TrimSpace(s.cfg.Gateway.OpenAIWS.StoreDisabledConnMode))
	switch mode {
	case openAIWSStoreDisabledConnModeStrict, openAIWSStoreDisabledConnModeAdaptive, openAIWSStoreDisabledConnModeOff:
		return mode
	case "":
		// 兼容旧配置：仅配置了布尔开关时按旧语义推导。
		if s.cfg.Gateway.OpenAIWS.StoreDisabledForceNewConn {
			return openAIWSStoreDisabledConnModeStrict
		}
		return openAIWSStoreDisabledConnModeOff
	default:
		return openAIWSStoreDisabledConnModeStrict
	}
}

func shouldForceNewConnOnStoreDisabled(mode, lastFailureReason string) bool {
	switch mode {
	case openAIWSStoreDisabledConnModeOff:
		return false
	case openAIWSStoreDisabledConnModeAdaptive:
		reason := strings.TrimPrefix(strings.TrimSpace(lastFailureReason), "prewarm_")
		switch reason {
		case "policy_violation", "message_too_big", "auth_failed", openAIWSFailureWriteRequest, "write":
			return true
		default:
			return false
		}
	default:
		return true
	}
}

func dropPreviousResponseIDFromRawPayload(payload []byte) ([]byte, bool, error) {
	return dropPreviousResponseIDFromRawPayloadWithDeleteFn(payload, sjson.DeleteBytes)
}

func dropPreviousResponseIDFromRawPayloadWithDeleteFn(
	payload []byte,
	deleteFn func([]byte, string) ([]byte, error),
) ([]byte, bool, error) {
	if len(payload) == 0 {
		return payload, false, nil
	}
	if !gjson.GetBytes(payload, "previous_response_id").Exists() {
		return payload, false, nil
	}
	if deleteFn == nil {
		deleteFn = sjson.DeleteBytes
	}

	updated := payload
	for i := 0; i < openAIWSMaxPrevResponseIDDeletePasses &&
		gjson.GetBytes(updated, "previous_response_id").Exists(); i++ {
		next, err := deleteFn(updated, "previous_response_id")
		if err != nil {
			return payload, false, err
		}
		updated = next
	}
	return updated, !gjson.GetBytes(updated, "previous_response_id").Exists(), nil
}

func setPreviousResponseIDToRawPayload(payload []byte, previousResponseID string) ([]byte, error) {
	normalizedPrevID := strings.TrimSpace(previousResponseID)
	if len(payload) == 0 || normalizedPrevID == "" {
		return payload, nil
	}
	updated, err := sjson.SetBytes(payload, "previous_response_id", normalizedPrevID)
	if err == nil {
		return updated, nil
	}

	var reqBody map[string]any
	if unmarshalErr := decodeOpenAIJSONUseNumber(payload, &reqBody); unmarshalErr != nil {
		return nil, err
	}
	reqBody["previous_response_id"] = normalizedPrevID
	rebuilt, marshalErr := json.Marshal(reqBody)
	if marshalErr != nil {
		return nil, marshalErr
	}
	return restoreOpenAIUpstreamJSONTopLevelOrder(payload, rebuilt), nil
}

type openAIWSContextWindowBoundary struct {
	WindowID                  string
	Changed                   bool
	PreviousResponseIDRemoved bool
}

func openAIWSPayloadCodexWindowID(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	if windowID := strings.TrimSpace(gjson.GetBytes(payload, "client_metadata.x-codex-window-id").String()); windowID != "" {
		return windowID
	}
	turnMetadata := strings.TrimSpace(gjson.GetBytes(payload, "client_metadata.x-codex-turn-metadata").String())
	if turnMetadata == "" {
		return ""
	}
	return strings.TrimSpace(gjson.Get(turnMetadata, "window_id").String())
}

// normalizeOpenAIWSContextWindowBoundary breaks a Responses continuation chain
// when Codex moves to a new local context window. WebSocket response.create can
// still carry the previous window's previous_response_id after new_context,
// while HTTP starts the new window without that continuation anchor.
func normalizeOpenAIWSContextWindowBoundary(
	payload []byte,
	previousWindowID string,
) ([]byte, openAIWSContextWindowBoundary, error) {
	currentWindowID := openAIWSPayloadCodexWindowID(payload)
	boundary := openAIWSContextWindowBoundary{WindowID: currentWindowID}
	if previousWindowID == "" || currentWindowID == "" || currentWindowID == previousWindowID {
		return payload, boundary, nil
	}
	boundary.Changed = true
	updated, removed, err := dropPreviousResponseIDFromRawPayload(payload)
	if err != nil {
		return payload, boundary, err
	}
	boundary.PreviousResponseIDRemoved = removed
	return updated, boundary, nil
}

func shouldInferIngressFunctionCallOutputPreviousResponseID(
	storeDisabled bool,
	turn int,
	signals ToolContinuationSignals,
	currentPreviousResponseID string,
	expectedPreviousResponseID string,
) bool {
	if !storeDisabled || turn <= 1 || !signals.HasFunctionCallOutput {
		return false
	}
	if strings.TrimSpace(currentPreviousResponseID) != "" {
		return false
	}
	if signals.HasFunctionCallOutputMissingCallID {
		return false
	}
	// If the client already sent the actual tool-call context, treat this as
	// a full replay / self-contained continuation payload rather than
	// downgrading it into an inferred delta continuation. item_reference alone
	// is not enough on the store=false WS path: it still needs a valid prior
	// response anchor so upstream can resolve the referenced function_call.
	if signals.HasToolCallContext {
		return false
	}
	return strings.TrimSpace(expectedPreviousResponseID) != ""
}

// Replay 状态所有权不变式：replay 序列中的 json.RawMessage 正文一经放入即视为
// 不可变，所有持有者共享同一份字节，任何修改都必须整体替换元素或重建 payload。
// 序列头数组在跨持有者保存时必须新建（combineOpenAIWSReplayItems），禁止通过
// 共享头 append，否则会写入其他持有者可见的底层数组。

// combineOpenAIWSReplayItems 合并历史与增量为新头数组，正文共享不复制。
func combineOpenAIWSReplayItems(history, delta []json.RawMessage) []json.RawMessage {
	if len(delta) == 0 {
		return history
	}
	combined := make([]json.RawMessage, 0, len(history)+len(delta))
	combined = append(combined, history...)
	return append(combined, delta...)
}

// openAIWSPayloadStringView 返回与 payload 共享底层数组的零拷贝 string 视图，
// 供 gjson.Get 使用（gjson.GetBytes 会整段复制结果 Raw，对 input 这类占
// payload 主体的字段是每次 O(payload) 分配）。调用方必须保证 payload 在结果
// 存活期间不可变（replay 所有权不变式）。
func openAIWSPayloadStringView(payload []byte) string {
	return unsafe.String(unsafe.SliceData(payload), len(payload))
}

// openAIWSRawMessageFromResult 优先返回 parent 的子切片（gjson 值零拷贝共享），
// Index 不可用时回退为复制。共享要求 parent 遵守上面的不可变约定。
func openAIWSRawMessageFromResult(parent []byte, value gjson.Result) json.RawMessage {
	idx := value.Index
	if idx > 0 && idx+len(value.Raw) <= len(parent) && string(parent[idx:idx+len(value.Raw)]) == value.Raw {
		return json.RawMessage(parent[idx : idx+len(value.Raw)])
	}
	return json.RawMessage(value.Raw)
}

func normalizeOpenAIWSJSONForCompare(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("json is empty")
	}
	var decoded any
	if err := decodeOpenAIJSONUseNumber(trimmed, &decoded); err != nil {
		return nil, err
	}
	return json.Marshal(decoded)
}

func normalizeOpenAIWSJSONForCompareOrRaw(raw []byte) []byte {
	normalized, err := normalizeOpenAIWSJSONForCompare(raw)
	if err != nil {
		return bytes.TrimSpace(raw)
	}
	return normalized
}

func normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("payload is empty")
	}
	var decoded map[string]any
	if err := decodeOpenAIJSONUseNumber(payload, &decoded); err != nil {
		return nil, err
	}
	delete(decoded, "input")
	delete(decoded, "previous_response_id")
	// Codex changes transport-only metadata for every response.create. These fields
	// do not alter the context referenced by previous_response_id and are excluded
	// from Codex's own websocket reuse comparison.
	delete(decoded, "client_metadata")
	delete(decoded, "stream_options")
	// Official Codex prewarms a connection with generate=false, then omits the
	// field on the business request that continues from the prewarm response.
	// Only normalize false so a meaningful generate=true change remains visible.
	if generate, ok := decoded["generate"].(bool); ok && !generate {
		delete(decoded, "generate")
	}
	return json.Marshal(decoded)
}

// openAIWSExtractNormalizedInputSequence 拆出 input 序列。返回的正文尽可能与
// payload 共享底层数组（零拷贝），受 replay 所有权不变式保护。
func openAIWSExtractNormalizedInputSequence(payload []byte) ([]json.RawMessage, bool, error) {
	if len(payload) == 0 {
		return nil, false, nil
	}
	inputValue := gjson.Get(openAIWSPayloadStringView(payload), "input")
	if !inputValue.Exists() {
		return nil, false, nil
	}
	if inputValue.Type == gjson.JSON {
		if inputValue.IsArray() {
			// gjson 宽容解析；数组整体先做零分配合法性校验，避免把断裂
			// JSON 塞进 replay 历史。
			arrayRaw := openAIWSRawMessageFromResult(payload, inputValue)
			if !json.Valid(arrayRaw) {
				return nil, true, errors.New("input array json is invalid")
			}
			elems := inputValue.Array()
			items := make([]json.RawMessage, 0, len(elems))
			for _, elem := range elems {
				items = append(items, openAIWSRawMessageFromResult(payload, elem))
			}
			return items, true, nil
		}
		return []json.RawMessage{openAIWSRawMessageFromResult(payload, inputValue)}, true, nil
	}
	if inputValue.Type == gjson.String {
		// A scalar input is a user message. Array replay requires an input
		// item, not a bare JSON string beside the subsequent output items.
		encoded, _ := json.Marshal(map[string]any{"role": "user", "content": inputValue.String()})
		return []json.RawMessage{encoded}, true, nil
	}
	return []json.RawMessage{openAIWSRawMessageFromResult(payload, inputValue)}, true, nil
}

func openAIWSRawItemsHasPrefix(items []json.RawMessage, prefix []json.RawMessage) bool {
	if len(prefix) == 0 {
		return true
	}
	if len(items) < len(prefix) {
		return false
	}
	for idx := range prefix {
		// 快路径：客户端逐字节重发历史时直接比较，避免整轮历史的解码/再编码。
		if bytes.Equal(bytes.TrimSpace(prefix[idx]), bytes.TrimSpace(items[idx])) {
			continue
		}
		previousNormalized := normalizeOpenAIWSJSONForCompareOrRaw(prefix[idx])
		currentNormalized := normalizeOpenAIWSJSONForCompareOrRaw(items[idx])
		if !bytes.Equal(previousNormalized, currentNormalized) {
			return false
		}
	}
	return true
}

func openAIWSRawItemsHasFunctionCallOutput(items []json.RawMessage) bool {
	for _, item := range items {
		if isCodexToolCallOutputItemType(gjson.GetBytes(item, "type").String()) {
			return true
		}
	}
	return false
}

// sanitizeOpenAIWSHistoricalReplayToolCalls 返回的新头数组与 previousItems 共享正文。
func sanitizeOpenAIWSHistoricalReplayToolCalls(
	previousItems []json.RawMessage,
	currentItems []json.RawMessage,
) []json.RawMessage {
	if len(previousItems) == 0 {
		return previousItems
	}
	outputCallIDs := make(map[string]struct{})
	collectOutputCallIDs := func(items []json.RawMessage) {
		for _, item := range items {
			if !isCodexToolCallOutputItemType(gjson.GetBytes(item, "type").String()) {
				continue
			}
			if callID := strings.TrimSpace(gjson.GetBytes(item, "call_id").String()); callID != "" {
				outputCallIDs[callID] = struct{}{}
			}
		}
	}
	collectOutputCallIDs(previousItems)
	collectOutputCallIDs(currentItems)

	sanitized := make([]json.RawMessage, 0, len(previousItems))
	for _, item := range previousItems {
		if isCodexToolCallContextItemType(gjson.GetBytes(item, "type").String()) {
			callID := strings.TrimSpace(gjson.GetBytes(item, "call_id").String())
			if _, paired := outputCallIDs[callID]; !paired {
				continue
			}
		}
		sanitized = append(sanitized, item)
	}
	return sanitized
}

func openAIWSRawPayloadHasToolCallOutput(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	input := gjson.Get(openAIWSPayloadStringView(payload), "input")
	if !input.Exists() {
		return false
	}
	if input.IsArray() {
		for _, item := range input.Array() {
			if isCodexToolCallOutputItemType(item.Get("type").String()) {
				return true
			}
		}
		return false
	}
	if input.Type == gjson.JSON {
		return isCodexToolCallOutputItemType(input.Get("type").String())
	}
	return false
}

// buildOpenAIWSReplayInputSequenceFromItems 基于已解析的当前 turn input 构建
// replay 序列。返回序列的正文与 previousFullInput/currentItems 共享所有权
// （见 combineOpenAIWSReplayItems 上方的所有权不变式），头数组可能直接转移自
// currentItems。
func buildOpenAIWSReplayInputSequenceFromItems(
	previousFullInput []json.RawMessage,
	previousFullInputExists bool,
	currentItems []json.RawMessage,
	currentExists bool,
	hasPreviousResponseID bool,
) ([]json.RawMessage, bool) {
	if !hasPreviousResponseID || !previousFullInputExists {
		return currentItems, currentExists
	}
	previousFullInput = sanitizeOpenAIWSHistoricalReplayToolCalls(previousFullInput, currentItems)
	if !currentExists || len(currentItems) == 0 {
		return previousFullInput, true
	}
	if openAIWSRawItemsHasPrefix(currentItems, previousFullInput) {
		return currentItems, true
	}
	merged := make([]json.RawMessage, 0, len(previousFullInput)+len(currentItems))
	merged = append(merged, previousFullInput...)
	merged = append(merged, currentItems...)
	return merged, true
}

func buildOpenAIWSReplayInputSequence(
	previousFullInput []json.RawMessage,
	previousFullInputExists bool,
	currentPayload []byte,
	hasPreviousResponseID bool,
) ([]json.RawMessage, bool, error) {
	currentItems, currentExists, currentErr := openAIWSExtractNormalizedInputSequence(currentPayload)
	if currentErr != nil {
		return nil, false, currentErr
	}
	items, exists := buildOpenAIWSReplayInputSequenceFromItems(
		previousFullInput,
		previousFullInputExists,
		currentItems,
		currentExists,
		hasPreviousResponseID,
	)
	return items, exists, nil
}

func setOpenAIWSPayloadInputSequence(
	payload []byte,
	fullInput []json.RawMessage,
	fullInputExists bool,
) ([]byte, error) {
	if !fullInputExists {
		return payload, nil
	}
	// Preserve [] vs null semantics when input exists but is empty.
	inputForMarshal := fullInput
	if inputForMarshal == nil {
		inputForMarshal = []json.RawMessage{}
	}
	inputRaw, marshalErr := json.Marshal(inputForMarshal)
	if marshalErr != nil {
		return nil, marshalErr
	}
	return sjson.SetRawBytes(payload, "input", inputRaw)
}

// Carry only confirmed client declarations into a complete replay. Target RPC
// tools and BPS transport wrappers are never a portable client catalog.
func openAIWSReplaySourceWithTools(payload []byte, inherited json.RawMessage, inherit bool) ([]byte, json.RawMessage, error) {
	if declared := gjson.GetBytes(payload, "tools"); declared.Exists() {
		return payload, json.RawMessage(declared.Raw), nil
	}
	if !inherit || len(inherited) == 0 || gjson.GetBytes(payload, "functions").Exists() {
		return payload, nil, nil
	}
	// Lite carries its catalog in history. Adding an artificial top-level tools
	// field would override its incremental declarations in the target adapter.
	carrier := false
	gjson.GetBytes(payload, "input").ForEach(func(_, item gjson.Result) bool {
		carrier = item.Get("type").String() == "additional_tools"
		return !carrier
	})
	if carrier {
		return payload, nil, nil
	}
	updated, err := sjson.SetRawBytes(payload, "tools", inherited)
	return updated, inherited, err
}

func buildOpenAIWSCurrentTurnRetryPayload(
	payload []byte,
	fullInput []json.RawMessage,
	fullInputExists bool,
	originalModel string,
	sameAccount ...bool,
) ([]byte, bool, error) {
	if !fullInputExists {
		return nil, false, nil
	}
	bytes := len(payload)
	for _, raw := range fullInput {
		bytes += len(raw)
		if bytes > int(openAIWSMessageReadLimitBytes) {
			return nil, false, nil
		}
		item := gjson.ParseBytes(raw)
		if !gjson.ValidBytes(raw) {
			return nil, false, nil
		}
		typ := item.Get("type").String()
		encrypted := item.Get("encrypted_content")
		if len(sameAccount) == 0 || !sameAccount[0] {
			if openAIWSNonPortableBlocker(item, 0) != "" {
				return nil, false, nil
			}
		}
		if typ == "item_reference" ||
			((typ == "reasoning" || typ == "compaction" || typ == "compaction_summary") && (encrypted.Type != gjson.String || strings.TrimSpace(encrypted.String()) == "")) {
			return nil, false, nil
		}
		// Uploaded files belong to an upstream account; possession of their
		// identifiers does not make a cross-account replay self-contained.
		if item.Get("file_id").Exists() && (len(sameAccount) == 0 || !sameAccount[0]) {
			return nil, false, nil
		}
		for _, content := range item.Get("content").Array() {
			if content.Get("file_id").Exists() && (len(sameAccount) == 0 || !sameAccount[0]) {
				return nil, false, nil
			}
		}
	}
	retryPayload, err := setOpenAIWSPayloadInputSequence(payload, fullInput, true)
	if err != nil {
		return nil, false, err
	}
	retryPayload = RemovePreviousResponseIDFromBody(retryPayload)
	if model := strings.TrimSpace(originalModel); model != "" {
		retryPayload, err = sjson.SetBytes(retryPayload, "model", model)
		if err != nil {
			return nil, false, err
		}
	}
	coverage := AnalyzeToolCallOutputContextCoverageBytes(retryPayload)
	if coverage.HasFunctionCallOutput && !coverage.ContextCoversAllCallIDs {
		return nil, false, nil
	}
	if (len(sameAccount) == 0 || !sameAccount[0]) && !OpenAIResponsesAccountReplayAllowed(retryPayload) {
		return nil, false, nil
	}
	return retryPayload, true, nil
}

// OpenAIResponsesAccountReplayAllowed checks the complete request for state
// owned by an upstream credential. It does not restrict retries on that same
// account, and does not infer ownership from a resource ID's spelling.
func OpenAIResponsesAccountReplayAllowed(payload []byte) bool {
	return openAIResponsesAccountReplayBlocker(payload) == ""
}

// Fixed structural reasons share the ownership decision; never log resource
// identifiers, encrypted content or arbitrary request fields.
func openAIResponsesAccountReplayBlocker(payload []byte) string {
	return openAIResponsesReplayBlocker(payload, false)
}

// allowEncryptedState is only for explicit pre-generation BPS recovery.
// The public native/uncertain-execution replay policy remains unchanged.
func openAIResponsesReplayBlocker(payload []byte, allowEncryptedState bool) string {
	if !gjson.ValidBytes(payload) {
		return "malformed_request"
	}
	root := gjson.ParseBytes(payload)
	if !root.IsObject() {
		return "malformed_request"
	}
	blocker := ""
	inputs := 0
	root.ForEach(func(key, value gjson.Result) bool {
		switch key.String() {
		case "input":
			inputs++
			if allowEncryptedState && inputs > 1 {
				// Coverage and upstream parsers may choose different occurrences.
				blocker = "ambiguous_input"
			}
		case "previous_response_id", "conversation", "prompt":
			if value.Type != gjson.Null && !(value.Type == gjson.String && strings.TrimSpace(value.String()) == "") {
				blocker = key.String()
			}
		}
		return blocker == ""
	})
	if blocker != "" {
		return blocker
	}
	return openAIWSNonPortableBlocker(root, 0, allowEncryptedState)
}

// Scan wire values without decoding or copying large message/attachment bodies.
func openAIWSNonPortableBlocker(value gjson.Result, depth int, encryptedState ...bool) string {
	if depth > 64 {
		return "context_depth"
	}
	if !value.IsObject() && !value.IsArray() {
		return ""
	}
	if value.IsObject() && value.Get("type").String() == "item_reference" {
		return "item_reference"
	}
	allowState := len(encryptedState) > 0 && encryptedState[0]
	allowOwnState := false
	if allowState && depth == 2 && value.IsObject() {
		types := 0
		value.ForEach(func(key, _ gjson.Result) bool {
			if key.String() == "type" {
				types++
			}
			return true
		})
		kind := value.Get("type").String()
		allowOwnState = types == 1 && (kind == "reasoning" || kind == "compaction")
	}
	blocker := ""
	value.ForEach(func(key, child gjson.Result) bool {
		switch key.String() {
		case "type":
			if child.String() == "item_reference" {
				blocker = "item_reference"
			}
		case "encrypted_content", "file_id", "vector_store_id", "container_id":
			if child.Type == gjson.String && strings.TrimSpace(child.String()) != "" && (key.String() != "encrypted_content" || !allowOwnState) {
				blocker = key.String()
			}
		case "vector_store_ids":
			if child.IsArray() && len(child.Array()) > 0 {
				blocker = "vector_store_ids"
			}
		}
		if blocker == "" {
			// Only direct input items may carry replayable encrypted state.
			// Nested messages, agent assignments and tool arguments stay blocked.
			allowChildState := allowState && ((depth == 0 && key.String() == "input" && child.IsArray()) || (depth == 1 && value.IsArray()))
			blocker = openAIWSNonPortableBlocker(child, depth+1, allowChildState)
		}
		return blocker == ""
	})
	return blocker
}

// Transport failure after a write does not prove the provider did no work.
// Before output is delivered, function/custom calls have not run on the client.
// Read-only hosted searches may be replayed at the cost of repeated search usage;
// other hosted tools and unknown tool kinds may already have side effects.
func openAIWSUncertainExecutionReplaySafe(payload []byte) bool {
	return openAIWSUncertainExecutionReplayBlocker(payload) == ""
}

const openAIReplayBlockerContextKey = "openai_replay_blocker"
const openAIReplayUnresolvedContextKey = "openai_replay_unresolved_context"
const openAIAccountReplayBlockerContextKey = "openai_account_replay_blocker"
const OpenAIAccountReplayAllowedContextKey = "openai_account_replay_allowed"

func setOpenAIReplaySafety(c *gin.Context, body []byte) {
	if c == nil {
		return
	}
	blocker, unresolved := openAIWSExecutionReplayAssessment(body, false)
	c.Set(openAIReplayUnresolvedContextKey, unresolved)
	c.Set("openai_uncertain_replay_safe", blocker == "")
	c.Set(openAIReplayBlockerContextKey, blocker)
	accountBlocker := openAIResponsesAccountReplayBlocker(body)
	c.Set(openAIAccountReplayBlockerContextKey, accountBlocker)
	c.Set(OpenAIAccountReplayAllowedContextKey, accountBlocker == "")
}

// Return a bounded, fixed diagnostic code, never tool names, descriptions or
// arguments. Namespace membership does not change where a function is executed.
func openAIWSUncertainExecutionReplayBlocker(payload []byte, completeHistory ...[]json.RawMessage) string {
	return openAIWSExecutionReplayBlocker(payload, false, completeHistory...)
}

func openAIWSExecutionReplayBlocker(payload []byte, beforeGeneration bool, completeHistory ...[]json.RawMessage) string {
	blocker, _ := openAIWSExecutionReplayAssessment(payload, beforeGeneration, completeHistory...)
	return blocker
}

// Keep the decision and its bounded diagnostic source in the same walk.
// An opaque item is not necessarily an unsupported tool; its inventory cannot
// be verified here. Never include IDs, encrypted bytes or provider names.
func openAIWSExecutionReplayAssessment(payload []byte, beforeGeneration bool, completeHistory ...[]json.RawMessage) (blocker, unresolved string) {
	if !gjson.ValidBytes(payload) {
		return "malformed_request", ""
	}
	root := gjson.ParseBytes(payload)
	if !root.IsObject() {
		return "malformed_request", ""
	}
	choiceCount := 0
	root.ForEach(func(key, value gjson.Result) bool {
		if key.String() == "tool_choice" {
			choiceCount++
		}
		return true
	})
	if choiceCount > 1 {
		return "ambiguous_tool_choice", ""
	}
	if root.Get("tool_choice").String() == "none" {
		return "", ""
	}
	var checkTools func(gjson.Result, int) string
	checkTools = func(tools gjson.Result, depth int) string {
		if depth > 16 {
			return "namespace_depth"
		}
		if !tools.IsArray() {
			return "malformed_tools"
		}
		blocker := ""
		tools.ForEach(func(_, tool gjson.Result) bool {
			if !tool.IsObject() {
				blocker = "malformed_tools"
				return false
			}
			// Ambiguous execution fields cannot establish replay safety. Other
			// tool properties (names, schemas, arguments) are never logged.
			seen := map[string]bool{}
			tool.ForEach(func(key, _ gjson.Result) bool {
				switch field := key.String(); field {
				case "type", "execution", "tools":
					if seen[field] {
						blocker = "malformed_tools"
					}
					seen[field] = true
				}
				return blocker == ""
			})
			if blocker != "" {
				return false
			}
			switch kind := tool.Get("type").String(); kind {
			case "function", "custom", "web_search", "file_search",
				"web_search_preview", "web_search_preview_2025_03_11":
			case "namespace":
				blocker = checkTools(tool.Get("tools"), depth+1)
			case "tool_search":
				// Discovery does not itself execute a discovered tool. Both
				// server and client discovery still require checking its inventory.
				switch tool.Get("execution").String() {
				case "", "server", "client":
				default:
					blocker = "tool_search_execution"
				}
			case "mcp", "computer", "computer_use", "computer_use_preview", "code_interpreter",
				"image_generation", "local_shell", "shell":
				blocker = kind
			default:
				blocker = "unknown_tool"
			}
			return blocker == ""
		})
		return blocker
	}
	checkItem := func(item gjson.Result) string {
		if item.Type == gjson.String {
			return "" // textual input cannot introduce executable tools
		}
		if !item.IsObject() {
			return "malformed_tool_context"
		}
		types, callIDs := 0, 0
		item.ForEach(func(key, _ gjson.Result) bool {
			if key.String() == "type" {
				types++
			}
			if key.String() == "call_id" {
				callIDs++
			}
			return true
		})
		if types > 1 || (beforeGeneration && callIDs > 1) {
			return "malformed_tool_context"
		}
		switch item.Get("type").String() {
		case "additional_tools", "tool_search_output":
			blocker, found := "", false
			item.ForEach(func(key, value gjson.Result) bool {
				if key.String() == "tools" {
					found = true
					blocker = checkTools(value, 0)
				}
				return blocker == ""
			})
			if !found {
				return "malformed_tools"
			}
			return blocker
		case "item_reference", "compaction_summary":
			unresolved = item.Get("type").String()
			return "unresolved_tool_context"
		case "compaction":
			if !beforeGeneration {
				unresolved = "compaction"
				return "unresolved_tool_context"
			}
		}
		return ""
	}
	// Walk every wire occurrence: a duplicate tools/input field must not hide
	// a later MCP definition from a parser that would otherwise read only first.
	root.ForEach(func(key, value gjson.Result) bool {
		switch key.String() {
		case "tools":
			if value.Type != gjson.Null {
				blocker = checkTools(value, 0)
			}
		case "input":
			if value.IsArray() {
				value.ForEach(func(_, item gjson.Result) bool {
					blocker = checkItem(item)
					return blocker == ""
				})
			} else if value.IsObject() {
				blocker = checkItem(value)
			}
		case "previous_response_id", "conversation", "prompt":
			if key.String() == "previous_response_id" && len(completeHistory) > 0 {
				break
			}
			if value.Type != gjson.Null && value.Raw != `""` {
				blocker, unresolved = "unresolved_tool_context", key.String()
			}
		}
		return blocker == ""
	})
	if blocker == "" && len(completeHistory) > 0 {
		for _, raw := range completeHistory[0] {
			if blocker = checkItem(gjson.ParseBytes(raw)); blocker != "" {
				break
			}
		}
	}
	return blocker, unresolved
}

func shouldKeepIngressPreviousResponseID(
	previousPayload []byte,
	currentPayload []byte,
	lastTurnResponseID string,
	hasFunctionCallOutput bool,
) (bool, string, error) {
	if hasFunctionCallOutput {
		return true, "has_function_call_output", nil
	}
	currentPreviousResponseID := strings.TrimSpace(openAIWSPayloadStringFromRaw(currentPayload, "previous_response_id"))
	if currentPreviousResponseID == "" {
		return false, "missing_previous_response_id", nil
	}
	expectedPreviousResponseID := strings.TrimSpace(lastTurnResponseID)
	if expectedPreviousResponseID == "" {
		return false, "missing_last_turn_response_id", nil
	}
	if currentPreviousResponseID != expectedPreviousResponseID {
		return false, "previous_response_id_mismatch", nil
	}
	if len(previousPayload) == 0 {
		return false, "missing_previous_turn_payload", nil
	}

	previousComparable, previousComparableErr := normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(previousPayload)
	if previousComparableErr != nil {
		return false, "non_input_compare_error", previousComparableErr
	}
	currentComparable, currentComparableErr := normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(currentPayload)
	if currentComparableErr != nil {
		return false, "non_input_compare_error", currentComparableErr
	}
	if !bytes.Equal(previousComparable, currentComparable) {
		return false, "non_input_changed", nil
	}
	return true, "strict_incremental_ok", nil
}

type openAIWSIngressPreviousTurnStrictState struct {
	nonInputComparable []byte
}

func buildOpenAIWSIngressPreviousTurnStrictState(payload []byte) (*openAIWSIngressPreviousTurnStrictState, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	nonInputComparable, nonInputErr := normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(payload)
	if nonInputErr != nil {
		return nil, nonInputErr
	}
	return &openAIWSIngressPreviousTurnStrictState{
		nonInputComparable: nonInputComparable,
	}, nil
}

func shouldKeepIngressPreviousResponseIDWithStrictState(
	previousState *openAIWSIngressPreviousTurnStrictState,
	currentPayload []byte,
	lastTurnResponseID string,
	hasFunctionCallOutput bool,
) (bool, string, error) {
	if hasFunctionCallOutput {
		return true, "has_function_call_output", nil
	}
	currentPreviousResponseID := strings.TrimSpace(openAIWSPayloadStringFromRaw(currentPayload, "previous_response_id"))
	if currentPreviousResponseID == "" {
		return false, "missing_previous_response_id", nil
	}
	expectedPreviousResponseID := strings.TrimSpace(lastTurnResponseID)
	if expectedPreviousResponseID == "" {
		return false, "missing_last_turn_response_id", nil
	}
	if currentPreviousResponseID != expectedPreviousResponseID {
		return false, "previous_response_id_mismatch", nil
	}
	if previousState == nil {
		return false, "missing_previous_turn_payload", nil
	}

	currentComparable, currentComparableErr := normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(currentPayload)
	if currentComparableErr != nil {
		return false, "non_input_compare_error", currentComparableErr
	}
	if !bytes.Equal(previousState.nonInputComparable, currentComparable) {
		return false, "non_input_changed", nil
	}
	return true, "strict_incremental_ok", nil
}
