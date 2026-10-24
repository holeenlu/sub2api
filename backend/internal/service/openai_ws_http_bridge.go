package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	openAIWSClientReadLimitBytesDefault     int64 = 64 * 1024 * 1024
	openAIWSHTTPBridgeThresholdBytesDefault int64 = 15 * 1024 * 1024
	openAIWSHTTPBridgeErrorBodyLimitBytes         = 64 * 1024
)

const openAIWSHTTPBridgeToolStateContextKey = "openai_ws_http_bridge_tool_state"

// A successful fallback establishes native ownership of the connection's
// subsequent response IDs and encrypted reasoning, even after BPS cooldown.
const openAIWSBPSNativeAccountContextKey = "openai_ws_bps_native_account"

type openAIWSHTTPBridgeToolState struct {
	ClientMapping apicompat.ResponsesClientToolMapping
	LoweredTools  json.RawMessage
}

func openAIWSHTTPBridgeToolStateFromContext(c *gin.Context) (openAIWSHTTPBridgeToolState, bool) {
	if c == nil {
		return openAIWSHTTPBridgeToolState{}, false
	}
	value, ok := c.Get(openAIWSHTTPBridgeToolStateContextKey)
	state, typed := value.(openAIWSHTTPBridgeToolState)
	return state, ok && typed
}

func setOpenAIWSHTTPBridgeToolState(c *gin.Context, state openAIWSHTTPBridgeToolState) {
	if c == nil {
		return
	}
	state.LoweredTools = append(json.RawMessage(nil), state.LoweredTools...)
	c.Set(openAIWSHTTPBridgeToolStateContextKey, state)
}

func decodeOpenAIWSHTTPBridgeLoweredTools(raw json.RawMessage) []any {
	if len(raw) == 0 {
		return nil
	}
	var tools []any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil
	}
	return tools
}

func openAIWSHTTPBridgeRawField(body []byte, name string) (json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, false
	}
	raw, present := fields[name]
	return append(json.RawMessage(nil), raw...), present
}

func openAIWSHTTPBridgeToolUpstreamName(account *Account) string {
	if account != nil && account.Platform == PlatformGrok {
		return "Grok WS HTTP bridge"
	}
	return "OpenAI WS HTTP bridge"
}

// ResolveOpenAIWSClientFirstMessageTimeout returns the effective client ingress deadline.
func ResolveOpenAIWSClientFirstMessageTimeout(cfg *config.Config) time.Duration {
	seconds := config.DefaultOpenAIWSClientFirstMessageTimeoutSeconds
	if cfg != nil && cfg.Gateway.OpenAIWS.ClientFirstMessageTimeoutSeconds > 0 {
		seconds = cfg.Gateway.OpenAIWS.ClientFirstMessageTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func ResolveOpenAIWSClientReadLimitBytes(cfg *config.Config) int64 {
	if cfg == nil || cfg.Gateway.OpenAIWS.ClientReadLimitBytes <= 0 {
		return openAIWSClientReadLimitBytesDefault
	}
	return cfg.Gateway.OpenAIWS.ClientReadLimitBytes
}

func (s *OpenAIGatewayService) openAIWSHTTPBridgeEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.HTTPBridgeEnabled
}

func (s *OpenAIGatewayService) openAIWSHTTPBridgeThresholdBytes() int64 {
	if s == nil || s.cfg == nil || s.cfg.Gateway.OpenAIWS.HTTPBridgeThresholdBytes <= 0 {
		return openAIWSHTTPBridgeThresholdBytesDefault
	}
	return s.cfg.Gateway.OpenAIWS.HTTPBridgeThresholdBytes
}

func (s *OpenAIGatewayService) openAIWSRequestPrefersHTTPSize(payloadBytes int) bool {
	return s.openAIWSHTTPBridgeEnabled() && int64(payloadBytes) >= s.openAIWSHTTPBridgeThresholdBytes()
}

func (s *OpenAIGatewayService) shouldBridgeOpenAIWSHTTP(account *Account, payloadBytes int, previousResponseID string) bool {
	if account != nil && account.Platform == PlatformGrok {
		return true
	}
	if strings.TrimSpace(previousResponseID) != "" {
		return false
	}
	return s.openAIWSRequestPrefersHTTPSize(payloadBytes)
}

func (s *OpenAIGatewayService) shouldBridgeOpenAIWSPassthroughFirstMessage(account *Account, payload []byte) bool {
	if account != nil && account.Platform == PlatformGrok {
		return true
	}
	if !s.openAIWSRequestPrefersHTTPSize(len(payload)) {
		return false
	}
	if !json.Valid(payload) {
		return false
	}

	i := skipOpenAIWSJSONSpace(payload, 0)
	if i >= len(payload) || payload[i] != '{' {
		return false
	}
	i++
	eventType := "response.create"
	previousResponseID := ""
	typeSeen, previousResponseIDSeen := false, false
	for {
		i = skipOpenAIWSJSONSpace(payload, i)
		if payload[i] == '}' {
			break
		}
		keyStart := i
		keyEnd := scanOpenAIWSJSONString(payload, keyStart)
		i = skipOpenAIWSJSONSpace(payload, keyEnd)
		i++ // json.Valid guarantees the colon.
		i = skipOpenAIWSJSONSpace(payload, i)
		valueStart := i
		i = skipOpenAIWSJSONValue(payload, i)

		key := ""
		// A critical key is at most 20 decoded bytes. The generous encoded bound
		// covers escaped spellings without allocating attacker-sized key strings.
		if keyEnd-keyStart <= 128 {
			_ = json.Unmarshal(payload[keyStart:keyEnd], &key)
		}
		switch key {
		case "type":
			if typeSeen {
				return false
			}
			typeSeen = true
			var value *string
			if err := json.Unmarshal(payload[valueStart:i], &value); err != nil {
				return false
			}
			if value == nil || strings.TrimSpace(*value) == "" {
				eventType = "response.create"
			} else {
				eventType = strings.TrimSpace(*value)
			}
		case "previous_response_id":
			if previousResponseIDSeen {
				return false
			}
			previousResponseIDSeen = true
			var value *string
			if err := json.Unmarshal(payload[valueStart:i], &value); err != nil {
				return false
			}
			if value != nil {
				previousResponseID = strings.TrimSpace(*value)
			}
		}
		i = skipOpenAIWSJSONSpace(payload, i)
		if payload[i] == ',' {
			i++
		}
	}
	return eventType == "response.create" && previousResponseID == ""
}

func skipOpenAIWSJSONSpace(payload []byte, i int) int {
	for i < len(payload) {
		switch payload[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			return i
		}
	}
	return i
}

func scanOpenAIWSJSONString(payload []byte, i int) int {
	for i++; i < len(payload); i++ {
		switch payload[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(payload)
}

func skipOpenAIWSJSONValue(payload []byte, i int) int {
	if payload[i] == '"' {
		return scanOpenAIWSJSONString(payload, i)
	}
	if payload[i] != '{' && payload[i] != '[' {
		for i < len(payload) && payload[i] != ',' && payload[i] != '}' {
			i++
		}
		return i
	}
	depth := 0
	for ; i < len(payload); i++ {
		switch payload[i] {
		case '"':
			i = scanOpenAIWSJSONString(payload, i) - 1
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(payload)
}

func prepareOpenAIWSHTTPBridgeBody(account *Account, payload []byte) ([]byte, error) {
	var body map[string]any
	if err := decodeOpenAIJSONUseNumber(payload, &body); err != nil {
		return nil, err
	}
	if body == nil {
		return nil, errors.New("response.create payload must be a JSON object")
	}
	delete(body, "type")
	delete(body, "generate")
	delete(body, "stream_id")
	delete(body, "event_id")
	// previous_response_id is also valid over HTTP. Only the ingress owner
	// may remove it after replacing it with complete, matching local history.
	if account != nil && account.Platform == PlatformGrok {
		delete(body, "previous_response_id") // Grok retains its existing conversation/cache bridge contract.
	}
	if account == nil || !account.IsOpenAICloudConfigured() {
		deleteOpenAIResponsesNoneReasoningEffortFromObject(account, body)
	}
	if account != nil && account.Platform != PlatformGrok && !account.IsOpenAICloudConfigured() && account.UsesOpenAICodexProtocol() {
		// The bridge bypasses applyCodexOAuthTransform. Keep its additive
		// reasoning option so later stateless turns can replay complete items.
		ensureCodexReasoningInclude(body)
	}
	body["stream"] = true
	return json.Marshal(body)
}

type openAIWSToolCallReplayCollector struct {
	items              []json.RawMessage
	seen               map[string]struct{}
	allItems           []json.RawMessage
	allSeen            map[string]int
	pending            map[string]struct{}
	unidentifiedOutput bool
}

func (c *openAIWSToolCallReplayCollector) AddEvent(eventType string, message []byte) {
	if eventType == "response.output_item.added" || isOpenAIWSTokenEvent(eventType) {
		id := gjson.GetBytes(message, "item.id").String()
		if id == "" {
			id = gjson.GetBytes(message, "item_id").String()
		}
		if id == "" {
			c.unidentifiedOutput = true
		} else {
			if c.pending == nil {
				c.pending = make(map[string]struct{})
			}
			c.pending[id] = struct{}{}
		}
	}
	switch strings.TrimSpace(eventType) {
	case "response.output_item.done":
		delete(c.pending, gjson.GetBytes(message, "item.id").String())
		item := gjson.GetBytes(message, "item")
		c.addAllItem(item)
		c.addItem(item)
	case "response.completed", "response.done", "response.incomplete", "response.cancelled", "response.canceled":
		output := gjson.GetBytes(message, "response.output")
		if !output.IsArray() {
			return
		}
		c.pending = nil
		c.unidentifiedOutput = false
		for _, item := range output.Array() {
			c.addAllItem(item)
			c.addItem(item)
		}
	}
}

// Items/AllItems 返回浅拷贝头数组；正文由 collector 独立分配且此后不可变，
// 调用方按 replay 所有权不变式共享持有。
func (c *openAIWSToolCallReplayCollector) Items() []json.RawMessage {
	return slices.Clone(c.items)
}

func (c *openAIWSToolCallReplayCollector) AllItems() []json.RawMessage {
	return slices.Clone(c.allItems)
}

func (c *openAIWSToolCallReplayCollector) addAllItem(item gjson.Result) {
	if !item.Exists() || item.Type != gjson.JSON {
		return
	}
	raw := strings.TrimSpace(item.Raw)
	if raw == "" || !strings.HasPrefix(raw, "{") || strings.TrimSpace(item.Get("type").String()) == "" {
		return
	}
	key := strings.TrimSpace(item.Get("id").String())
	if key == "" {
		key = strings.TrimSpace(item.Get("call_id").String())
	}
	if key == "" {
		key = raw
	}
	if c.allSeen == nil {
		c.allSeen = make(map[string]int)
	}
	if index, ok := c.allSeen[key]; ok {
		// Some event sequences complete the encrypted payload only in the
		// terminal response. Preserve that later content without duplicating
		// the item or mutating snapshots already used by another replay owner.
		typ := item.Get("type").String()
		cipher := item.Get("encrypted_content")
		previous := gjson.ParseBytes(c.allItems[index])
		previousCipher := previous.Get("encrypted_content")
		if (typ == "reasoning" || typ == "compaction") && previous.Get("type").String() == typ &&
			cipher.Type == gjson.String && strings.TrimSpace(cipher.String()) != "" &&
			(previousCipher.Type == gjson.Null || previousCipher.Type == gjson.String && strings.TrimSpace(previousCipher.String()) == "") {
			if updated, err := sjson.SetRawBytes(c.allItems[index], "encrypted_content", []byte(cipher.Raw)); err == nil {
				c.allItems[index] = updated
			}
		}
		return
	}
	c.allSeen[key] = len(c.allItems)
	c.allItems = append(c.allItems, json.RawMessage(raw))
}

func (c *openAIWSToolCallReplayCollector) addItem(item gjson.Result) {
	if !item.Exists() || item.Type != gjson.JSON {
		return
	}
	raw := strings.TrimSpace(item.Raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return
	}
	if !isCodexToolCallContextItemType(item.Get("type").String()) {
		return
	}
	key := strings.TrimSpace(item.Get("id").String())
	if key == "" {
		key = strings.TrimSpace(item.Get("call_id").String())
	}
	if key == "" {
		key = raw
	}
	if c.seen == nil {
		c.seen = make(map[string]struct{})
	}
	if _, ok := c.seen[key]; ok {
		return
	}
	c.seen[key] = struct{}{}
	c.items = append(c.items, json.RawMessage(raw))
}

func buildOpenAIWSHTTPBridgeErrorEvent(statusCode int, message string, source ...[]byte) []byte {
	message = strings.TrimSpace(message)
	if message == "" {
		message = http.StatusText(statusCode)
	}
	if message == "" {
		message = "upstream request failed"
	}
	errorBody := map[string]any{"type": "upstream_error", "message": message}
	if len(source) > 0 {
		for _, field := range []string{"code", "type", "param"} {
			value := gjson.GetBytes(source[0], "error."+field)
			if !value.Exists() || value.Type == gjson.Null {
				value = gjson.GetBytes(source[0], "response.error."+field)
			}
			if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
				errorBody[field] = truncateString(value.String(), 256)
			}
		}
	}
	event := map[string]any{
		"type":            "error",
		"sequence_number": 0,
		"status":          statusCode,
		"error":           errorBody,
	}
	body, err := json.Marshal(event)
	if err != nil {
		return []byte(`{"type":"error","sequence_number":0,"error":{"type":"upstream_error","message":"upstream request failed"}}`)
	}
	return body
}

// buildOpenAIWSHTTPBridgeFailedEvent emits a request error until a response ID
// exists; response.failed then includes the required timestamp and sequence.
func buildOpenAIWSHTTPBridgeFailedEvent(responseID, model string, createdAt int64, source []byte, fallbackMessage string) []byte {
	if strings.TrimSpace(responseID) == "" {
		code, typ, message := parseOpenAIWSErrorEventFields(source)
		if message == "" {
			message = fallbackMessage
		}
		return buildOpenAIWSHTTPBridgeErrorEvent(openAIWSErrorHTTPStatusFromRaw(code, typ), message, source)
	}
	errorType := strings.TrimSpace(gjson.GetBytes(source, "error.type").String())
	if errorType == "" {
		errorType = strings.TrimSpace(gjson.GetBytes(source, "response.error.type").String())
	}
	code := strings.TrimSpace(gjson.GetBytes(source, "error.code").String())
	if code == "" {
		code = strings.TrimSpace(gjson.GetBytes(source, "response.error.code").String())
	}
	if code == "" {
		code = "upstream_error"
	}
	message := extractOpenAISSEErrorMessage(source)
	if message == "" {
		message = strings.TrimSpace(fallbackMessage)
	}
	if message == "" {
		message = "Upstream response failed"
	}
	errorBody := map[string]any{"code": code, "message": message}
	if errorType != "" {
		errorBody["type"] = errorType
	}
	if createdAt <= 0 {
		createdAt = time.Now().Unix()
	}
	sequenceNumber := openAIResponsesFailedSequenceNumber(source)
	response := map[string]any{
		"id": responseID, "object": "response", "created_at": createdAt, "status": "failed",
		"output": []any{}, "error": errorBody,
	}
	if model = strings.TrimSpace(model); model != "" {
		response["model"] = model
	}
	body, err := json.Marshal(map[string]any{"type": "response.failed", "sequence_number": sequenceNumber, "response": response})
	if err != nil {
		return []byte(`{"type":"response.failed","sequence_number":` + strconv.FormatInt(sequenceNumber, 10) +
			`,"response":{"object":"response","created_at":` + strconv.FormatInt(createdAt, 10) +
			`,"status":"failed","output":[],"error":{"code":"upstream_error","message":"Upstream response failed"}}}`)
	}
	return body
}

func (s *OpenAIGatewayService) proxyOpenAIWSHTTPBridgeTurn(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token string,
	payload []byte,
	payloadBytes int,
	originalModel string,
	imageBillingModel string,
	imageSizeTier string,
	imageInputSize string,
	grokCacheIdentity string,
	turn int,
	writeClientMessage func([]byte) error,
) (forwardResult *OpenAIForwardResult, forwardErr error) {
	if excelBPSNativeRecoveryActive(ctx) && excelBPSHasNativeFallback(account) {
		c.Set(openAIWSBPSNativeAccountContextKey, account.ID)
		account = excelBPSPoolForwardAccount(ctx, c, account)
	}
	if s == nil {
		return nil, errors.New("service is nil")
	}
	if s.httpUpstream == nil {
		return nil, errors.New("openai http upstream is nil")
	}
	if account == nil {
		return nil, errors.New("account is nil")
	}
	if writeClientMessage == nil {
		return nil, errors.New("client websocket writer is nil")
	}
	lane, requestEventID, envelopeErr := openAIWSRequestEnvelope(payload)
	if envelopeErr != nil {
		return nil, envelopeErr
	}
	progress := beginOpenAIResponseProgress(c, "http_bridge", "", nil)
	progress.countsKnown = true
	var timing *openAIForwardTiming
	var timingErr error
	defer func() {
		if forwardResult != nil && forwardResult.LatencyMetrics == nil {
			forwardResult.LatencyMetrics = timing.UsageLatencyMetrics(forwardResult.Stream || forwardResult.OpenAIWSMode)
		}
		if forwardErr != nil {
			timingErr = forwardErr
		}
		if timingErr == nil {
			timingErr = ctx.Err()
		}
		if forwardErr == nil && forwardResult != nil && forwardResult.UpstreamTerminalEvent != "" {
			timing.FinishTurn(timingErr)
		} else {
			timing.Finish(timingErr)
		}
	}()
	writeEvent := writeClientMessage
	writeClientMessage = func(event []byte) error {
		framed, err := openAIWSEventWithEnvelope(event, lane, requestEventID)
		if err != nil {
			return err
		}
		if err := writeEvent(framed); err != nil {
			timingErr = err
			return err
		}
		timing.MarkDownstream(framed)
		progress.observeDownstream(event)
		return nil
	}

	localWarmup := gjson.GetBytes(payload, "generate").Type == gjson.False
	if localWarmup {
		if int64(len(payload)) > ResolveOpenAIWSClientReadLimitBytes(s.cfg) {
			return nil, NewOpenAIWSClientCloseError(coderws.StatusMessageTooBig, "warmup context exceeds websocket size limit", ErrOpenAIWSLocalClientPolicy)
		}
		// Opaque server-side context cannot be materialized without an upstream
		// request. Never acknowledge a local snapshot that we cannot replay.
		for _, field := range []string{"previous_response_id", "prompt", "conversation"} {
			value := gjson.GetBytes(payload, field)
			if value.Exists() && value.Type != gjson.Null && value.Raw != `""` {
				return nil, NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater,
					"HTTP bridge warmup requires full input context; use native WebSocket for server-side context", ErrOpenAIWSReplayUnavailable)
			}
		}
	}
	responseModelObserver := newUpstreamResponseModelObserver(c)
	// Only the ingress owner can prove earlier history complete. A direct
	// turn>1 helper call has no such proof and retains conservative behavior.
	replayProven, _ := ctx.Value(openAIWSBridgeReplayKey{}).(bool)

	body, err := prepareOpenAIWSHTTPBridgeBody(account, payload)
	if err != nil {
		return nil, fmt.Errorf("prepare http bridge body: %w", err)
	}

	if account.IsOpenAICloudConfigured() && !localWarmup {
		return s.proxyOpenAIWSCloudTurn(ctx, c, account, body, originalModel, writeClientMessage)
	}
	bpsTurn := s.excelBPSGloballyEnabled(ctx) && account.isExcelBPSUpstreamModelEnabled(gjson.GetBytes(body, "model").String())
	var bpsTools json.RawMessage
	if bpsTurn {
		const toolKey = "openai_ws_bps_tools"
		var present bool
		bpsTools, present = openAIWSHTTPBridgeRawField(body, "tools")
		if !present {
			if cached, ok := c.Get(toolKey); ok {
				bpsTools, _ = cached.(json.RawMessage)
			}
			if len(bpsTools) > 0 {
				body, err = sjson.SetRawBytes(body, "tools", bpsTools)
				if err != nil {
					return nil, err
				}
			}
		}
		if !localWarmup {
			var result *OpenAIForwardResult
			var native *Account
			if c.GetInt64(openAIWSBPSNativeAccountContextKey) == account.ID && excelBPSHasNativeFallback(account) {
				native = excelBPSNativeAccount(account)
				SetActualOpenAIUpstreamEndpoint(c, "/v1/responses")
			} else {
				var bpsErr error
				result, bpsErr = s.proxyOpenAIWSExcelBPSTurn(ctx, c, account, body, originalModel, writeClientMessage)
				if bpsErr == nil {
					c.Set(toolKey, append(json.RawMessage(nil), bpsTools...))
					return result, nil
				}
				native = excelBPSNativeFallbackAccount(ctx, c, account, body, bpsErr)
				if native == nil {
					return result, bpsErr
				}
			}
			defer finishExcelBPSNativeFallback(&forwardResult, result, account)
			account = native
			// The ingress token may predate this turn. Resolve only the native
			// grant here; never send the Excel authorization to Codex.
			token, _, err = s.GetAccessToken(ctx, account)
			if err != nil {
				return nil, err
			}
			nativeWrite := writeClientMessage
			writeClientMessage = func(event []byte) error {
				if err := nativeWrite(event); err != nil {
					return err
				}
				// A delivered native response ID retains ownership even if
				// that response later fails after partial output.
				if gjson.GetBytes(event, "response.id").String() != "" {
					c.Set(openAIWSBPSNativeAccountContextKey, account.ID)
				}
				return nil
			}
			defer func() {
				if forwardErr == nil && forwardResult != nil {
					c.Set(toolKey, append(json.RawMessage(nil), bpsTools...))
					c.Set(openAIWSBPSNativeAccountContextKey, account.ID)
				}
			}()
		}
	}
	if !localWarmup && account.IsExcelOAuth() {
		return nil, newOpenAIWSNativeModelSwitchError(originalModel)
	}
	grokIntentSourceBody := append([]byte(nil), body...)
	_, grokExplicitToolsField := openAIWSHTTPBridgeRawField(grokIntentSourceBody, "tools")
	grokExplicitToolIntent := account.Platform == PlatformGrok && hasGrokResponsesToolIntent(grokIntentSourceBody)
	var clientToolMapping apicompat.ResponsesClientToolMapping
	functionToolUpstream := (account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey) || account.Platform == PlatformGrok
	if functionToolUpstream {
		if account.Platform == PlatformGrok {
			body, err = sanitizeGrokResponsesInput(body)
			if err != nil {
				return nil, fmt.Errorf("sanitize Grok WS HTTP bridge input: %w", err)
			}
		}
		inheritedState, _ := openAIWSHTTPBridgeToolStateFromContext(c)
		inheritedLoweredTools := decodeOpenAIWSHTTPBridgeLoweredTools(inheritedState.LoweredTools)
		body, clientToolMapping, err = adaptResponsesClientToolsForFunctionUpstreamWithMapping(
			body,
			openAIWSHTTPBridgeToolUpstreamName(account),
			inheritedState.ClientMapping,
			inheritedLoweredTools,
		)
		if err != nil {
			return nil, fmt.Errorf("adapt %s client tools: %w", openAIWSHTTPBridgeToolUpstreamName(account), err)
		}
		if account.Platform == PlatformGrok && !grokExplicitToolsField && !grokExplicitToolIntent && len(inheritedLoweredTools) > 0 && hasGrokResponsesToolIntent(body) {
			// This continuation omitted tools, so the pre-adapter source cannot
			// represent the effective inherited declarations. Cache routing must
			// see the rehydrated tool intent or it will replace client functions
			// with the native-search tool-free route. Explicit current-turn tool
			// intent still uses the original pre-sanitization source above.
			grokIntentSourceBody = append(grokIntentSourceBody[:0], body...)
		}
		loweredTools := inheritedState.LoweredTools
		if currentTools, present := openAIWSHTTPBridgeRawField(body, "tools"); present {
			loweredTools = currentTools
		}
		setOpenAIWSHTTPBridgeToolState(c, openAIWSHTTPBridgeToolState{
			ClientMapping: clientToolMapping,
			LoweredTools:  loweredTools,
		})
	}
	if account.Platform != PlatformGrok && isOpenAIResponsesLiteWebSocketPayload(payload) {
		liteBody, liteChanged, liteErr := normalizeOpenAIResponsesLitePayloadForAccount(body, account)
		if liteErr != nil {
			return nil, fmt.Errorf("normalize responses Lite payload: %w", liteErr)
		}
		if liteChanged {
			body = liteBody
		}
	}

	requestCtx := ctx
	buildUpstreamRequest := func(requestBody []byte) (*http.Request, error) {
		// The ingress owner already supplies a bounded drain context. A second
		// detach here would discard explicit cancellation and its drain deadline.
		upstreamCtx := requestCtx
		var upstreamReq *http.Request
		var buildErr error
		if account.Platform == PlatformGrok {
			upstreamReq, buildErr = buildGrokResponsesRequest(upstreamCtx, c, account, requestBody, token, grokCacheIdentity, s.cfg, s.settingService)
		} else {
			upstreamReq, buildErr = s.buildUpstreamRequestOpenAIPassthrough(upstreamCtx, c, account, requestBody, token)
		}
		if buildErr != nil {
			return nil, buildErr
		}
		if account.Platform != PlatformGrok && isOpenAIResponsesLiteWebSocketPayload(payload) {
			upstreamReq.Header.Set(responsesLiteHeader, "true")
		}
		if err := applyMappedGPT55LiteCompatibility(upstreamReq, account, requestBody); err != nil {
			return nil, err
		}
		return upstreamReq, nil
	}
	if account.Platform == PlatformGrok {
		upstreamModel := resolveGrokWSUpstreamModel(account, body, originalModel)
		body, err = patchGrokResponsesBody(body, upstreamModel)
		if err != nil {
			return nil, err
		}
		grokMixedCacheIntentBody := append([]byte(nil), body...)
		body, err = applyGrokResponsesCacheIdentity(body, grokIntentSourceBody, grokCacheIdentity, account.IsGrokOAuth())
		if err != nil {
			return nil, fmt.Errorf("apply grok prompt cache identity: %w", err)
		}
		body, err = applyGrokFreeRequestToolCacheRoute(c, body, grokMixedCacheIntentBody, account, grokCacheIdentity)
		if err != nil {
			return nil, fmt.Errorf("apply grok Free function-tool cache route: %w", err)
		}
	}
	setOpenAIReplaySafety(c, body)
	if localWarmup {
		// HTTP has no generate=false operation. A local acknowledgement keeps
		// the context in the owning bridge without performing paid inference.
		responseID := "resp_bridge_warmup_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		result := &OpenAIForwardResult{
			RequestID: responseID, ResponseID: responseID, Model: originalModel,
			Stream: true, OpenAIWSMode: true,
			wsLocalWarmup: true,
		}
		response := map[string]any{
			"id": responseID, "object": "response", "created_at": time.Now().Unix(),
			"model": originalModel, "output": []any{}, "status": "in_progress",
		}
		for sequence, eventType := range []string{"response.created", "response.completed"} {
			if eventType == "response.completed" {
				response["status"] = "completed"
			}
			event := map[string]any{"type": eventType, "sequence_number": sequence, "response": response}
			encoded, err := json.Marshal(event)
			if err != nil {
				return result, err
			}
			if err := writeClientMessage(encoded); err != nil {
				return result, err
			}
		}
		if bpsTurn {
			c.Set("openai_ws_bps_tools", append(json.RawMessage(nil), bpsTools...))
		}
		return result, nil
	}
	actualModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if actualModel == "" {
		actualModel = canonicalOpenAIAccountSchedulingModel(account, originalModel)
	}
	SetOpsUpstreamModel(c, actualModel)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	if c != nil {
		c.Set("openai_passthrough", true)
		c.Set("openai_ws_http_bridge", true)
	}

	turnStart := time.Now()
	firstOutputTimeout := time.Duration(0)
	if account.Platform == PlatformOpenAI {
		firstOutputTimeout = s.openAIFirstOutputTimeout(gjson.GetBytes(body, "reasoning.effort").String())
	}
	var firstOutputGuard *openAIFirstOutputHeaderGuard
	firstOutputStopped := false
	if firstOutputTimeout > 0 {
		deadline := openAITurnFirstOutputDeadline(ctx, firstOutputTimeout, turnStart)
		if !time.Now().Before(deadline) {
			return nil, ErrOpenAIWSFirstOutputBudgetExhausted
		}
		requestCtx, firstOutputGuard = newOpenAIFirstOutputHeaderGuard(ctx, func() {}, deadline)
		defer firstOutputGuard.close()
	}
	stopFirstOutput := func() bool {
		if firstOutputGuard != nil && !firstOutputStopped {
			expired := firstOutputGuard.stopHeaderWait()
			firstOutputStopped = true
			return !expired
		}
		return true
	}
	rejectedFieldRetryState := newOpenAIResponsesRejectedFieldRetryState(body)
	var resp *http.Response
	for {
		upstreamReq, buildErr := buildUpstreamRequest(body)
		if buildErr != nil {
			var inputErr *openAIResponsesInputError
			if errors.As(buildErr, &inputErr) {
				event, eventErr := inputErr.websocketEvent(payload)
				if eventErr != nil {
					return nil, eventErr
				}
				if err := writeClientMessage(event); err != nil {
					return nil, err
				}
				return nil, &openAIWSTurnRejectedError{cause: buildErr}
			}
			return nil, buildErr
		}

		resp, err = s.doOpenAIResponsesUpstream(c, upstreamReq, proxyURL, account, false)
		timing = openAIForwardTimingFromContext(c)
		timing.SetTransportReason("ws_to_http_bridge")
		timing.SetRequestMetadata(body)

		if err != nil {
			if ctx.Err() == nil && firstOutputGuard.expired() {
				var attemptErr *OpenAITransportAttemptError
				if errors.As(err, &attemptErr) && attemptErr.Unsent {
					return nil, ErrOpenAIWSFirstOutputBudgetExhausted
				}
				timeoutErr := s.newOpenAIFirstOutputTimeoutError(ctx, c, account, opsUpstreamProxyID(account), opsUpstreamProxyName(account), turnStart, originalModel, gjson.GetBytes(body, "reasoning.effort").String(), firstOutputTimeout, "http_bridge_response_headers", nil)
				if (turn == 1 || replayProven) && openAIWSUncertainExecutionReplaySafe(body) {
					return nil, timeoutErr
				}
				clientError := buildOpenAIWSHTTPBridgeErrorEvent(timeoutErr.StatusCode, "Upstream produced no output before the deadline", timeoutErr.ResponseBody)
				if writeErr := writeClientMessage(clientError); writeErr != nil {
					return nil, writeErr
				}
				markOpenAIWSClientVisibleFailure(c, "error", clientError)
				return nil, &openAIWSTurnRejectedError{cause: errors.New("upstream http bridge first output timeout")}
			}
			if ctx.Err() != nil || errors.Is(err, ErrOpenAITurnAttemptBudgetExhausted) || errors.Is(err, ErrOpenAIWSFirstOutputBudgetExhausted) {
				return nil, err
			}
			if turn == 1 || replayProven {
				handled := s.handleOpenAIUpstreamTransportError(ctx, c, account, err, true)
				var failover *UpstreamFailoverError
				if errors.As(handled, &failover) {
					return nil, handled
				}
				// Unknown hosted-tool execution cannot replay, but still owes
				// the client the bridge's protocol error event.
			}
			safeErr := sanitizeUpstreamErrorMessage(err.Error())
			clientError := buildOpenAIWSHTTPBridgeErrorEvent(http.StatusBadGateway, "Upstream request failed")
			if writeErr := writeClientMessage(clientError); writeErr != nil {
				return nil, writeErr
			}
			markOpenAIWSClientVisibleFailure(c, "error", clientError)
			return nil, &openAIWSTurnRejectedError{cause: fmt.Errorf("upstream http bridge request failed: %s", safeErr)}
		}
		progress.protocol = resp.Proto
		if resp.StatusCode < 400 {
			break
		}

		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, openAIWSHTTPBridgeErrorBodyLimitBytes))
		_ = resp.Body.Close()

		markOpenAICyberPolicyEvent(c, respBody, resp.StatusCode, nil)
		retryBody, retryReason, changed, retryErr := normalizeOpenAIResponsesRejectedFieldRetryBody(resp.StatusCode, body, respBody)
		if retryErr != nil {
			return nil, fmt.Errorf("normalize websocket http bridge rejected field retry: %w", retryErr)
		}
		if changed && rejectedFieldRetryState.Allow(retryBody) {
			logOpenAIWSModeInfo(
				"ingress_ws_http_bridge_rejected_field_retry account_id=%d turn=%d reason=%s",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(retryReason, openAIWSLogValueMaxLen),
			)
			body = retryBody
			payloadBytes = len(body)
			continue
		}

		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if upstreamMsg == "" {
			upstreamMsg = http.StatusText(resp.StatusCode)
		}
		if !changed && retryReason != "" {
			// The shared repair guard deliberately refuses missing context.
			// Persist its item index/type reason on this request, not only in
			// an uncorrelated warning. Never store the input or cipher here.
			setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, "input repair declined: "+retryReason)
		}
		shouldFailover := s.shouldFailoverOpenAIUpstreamResponse(account, resp.StatusCode, upstreamMsg, respBody)
		if account.Platform == PlatformGrok {
			shouldFailover = s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody)
			s.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(ctx, resolveGrokWSUpstreamModel(account, body, originalModel)), account, resp.StatusCode, resp.Header, respBody)
			if shouldFailover && (turn == 1 || replayProven || resp.StatusCode == http.StatusTooManyRequests) {
				return nil, newOpenAIUpstreamFailoverError(resp.StatusCode, resp.Header, respBody, upstreamMsg, false)
			}
		} else if shouldFailover && (turn == 1 || replayProven || resp.StatusCode == http.StatusTooManyRequests) {
			if resp.StatusCode >= 500 && !openAIWSUncertainExecutionReplaySafe(body) {
				RecordOpenAIRetryStop(c, account, OpenAIRetryStopReplayUnsafe)
			} else {
				return nil, s.handleFailoverErrorResponsePassthrough(ctx, resp, c, account, body, respBody)
			}
		}
		if account.Platform != PlatformGrok && (shouldFailover || shouldCooldownOpenAITransientUpstreamError(resp.StatusCode, respBody)) {
			s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody, actualModel)
		}
		clientError := buildOpenAIWSHTTPBridgeErrorEvent(resp.StatusCode, upstreamMsg, respBody)
		if writeErr := writeClientMessage(clientError); writeErr != nil {
			return nil, writeErr
		}
		markOpenAIWSClientVisibleFailure(c, "error", clientError)
		return nil, &openAIWSTurnRejectedError{cause: fmt.Errorf("upstream http bridge error: status=%d message=%s", resp.StatusCode, upstreamMsg)}
	}
	defer func() { _ = resp.Body.Close() }()
	stopCancelBody := context.AfterFunc(requestCtx, func() { _ = resp.Body.Close() })
	defer stopCancelBody()
	if account.Platform == PlatformGrok {
		s.updateGrokUsageFromResponse(withGrokTeamRateLimitModel(ctx, resolveGrokWSUpstreamModel(account, body, originalModel)), account, resp.Header, resp.StatusCode)
	}

	responseID := ""
	usage := OpenAIUsage{}
	imageCounter := newOpenAIImageOutputCounter()
	var firstTokenMs *int
	reqStream := openAIWSPayloadBoolFromRaw(body, "stream", true)
	eventCount := 0
	tokenEventCount := 0
	terminalEventCount := 0
	replayCollector := &openAIWSToolCallReplayCollector{}
	firstEventType := ""
	lastEventType := ""
	upstreamTerminalEvent := ""
	sawDone := false
	wroteDownstream := false
	pendingClientMessages := make([][]byte, 0, 4)
	pendingClientMessageBytes := int64(0)
	capacityFailoverSuppressedLogged := false
	clientDisconnected := false
	officialOpenAIResponses := account != nil && account.Platform == PlatformOpenAI
	bareErrorPending := false
	var bareErrorTimer *time.Timer
	var bareErrorTimerMu sync.Mutex
	var bareErrorIdleDeadline time.Time
	// Capture the final wrapped body below, before any timer is armed.
	setBareErrorIdle := func(active bool) {
		bareErrorTimerMu.Lock()
		defer bareErrorTimerMu.Unlock()
		if bareErrorTimer != nil {
			bareErrorTimer.Stop()
		}
		bareErrorIdleDeadline = time.Time{}
		if !active {
			return
		}
		timeout := s.openAIWSErrorIdleTimeout(body)
		bareErrorIdleDeadline = time.Now().Add(timeout)
		if bareErrorTimer == nil {
			bodyToClose := resp.Body
			bareErrorTimer = time.AfterFunc(timeout, func() {
				bareErrorTimerMu.Lock()
				defer bareErrorTimerMu.Unlock()
				if bareErrorIdleDeadline.IsZero() {
					return
				}
				if remaining := time.Until(bareErrorIdleDeadline); remaining > 0 {
					bareErrorTimer.Reset(remaining)
					return
				}
				_ = bodyToClose.Close()
			})
		} else {
			bareErrorTimer.Reset(timeout)
		}
	}
	defer func() { setBareErrorIdle(false) }()
	var bareErrorPayload []byte
	contextRejected := false
	var createdAtStamper responsesCreatedAtStamper
	bareErrorMessage := ""
	var retryStopReason OpenAIRetryStopReason
	failureAccountSideEffectsApplied := false
	mappedModel := actualModel
	needModelReplace := false
	var mappedModelBytes []byte
	if originalModel != "" {
		needModelReplace = mappedModel != "" && mappedModel != originalModel
		if needModelReplace {
			mappedModelBytes = []byte(mappedModel)
		}
	}

	upstreamIncompleteReason := ""
	resultWithUsage := func() *OpenAIForwardResult {
		imageCount := imageCounter.Count()
		result := &OpenAIForwardResult{
			AuthorizationMode:             nativeOpenAIUsageAuthorizationMode(account),
			RequestID:                     responseID,
			Usage:                         usage,
			Model:                         originalModel,
			UpstreamModel:                 mappedModel,
			UpstreamResponseModel:         responseModelObserver.Model(),
			UpstreamResponseModelConflict: responseModelObserver.Conflict(),
			UpstreamResponseServiceTier:   responseModelObserver.ServiceTier(),
			ServiceTier:                   resolvedOpenAIUpstreamServiceTierFromObserver(responseModelObserver, extractOpenAIServiceTierFromBody(body)),
			ReasoningEffort:               ApplyThinkingEnabledFallback(extractOpenAIReasoningEffortFromBody(body, mappedModel, originalModel), body, mappedModel),
			RequestedReasoningEffort:      CanonicalRequestedReasoningEffort(body, originalModel, mappedModel),
			Stream:                        reqStream,
			OpenAIWSMode:                  true,
			ClientDisconnect:              clientDisconnected || errors.Is(context.Cause(ctx), errOpenAIWSUsageDrainExpired),
			UpstreamTerminalEvent:         upstreamTerminalEvent,
			wsContextRejected:             upstreamTerminalEvent == "response.failed" && contextRejected,
			UpstreamIncompleteReason:      upstreamIncompleteReason,
			ResponseHeaders:               cloneHeader(resp.Header),
			Duration:                      time.Since(turnStart),
			FirstTokenMs:                  firstTokenMs,
		}
		if replayInput := replayCollector.AllItems(); len(replayInput) > 0 {
			result.wsReplayInput = replayInput
			result.wsReplayInputExists = true
		}
		result.wsReplayIncomplete = len(replayCollector.pending) > 0 || replayCollector.unidentifiedOutput
		result.wsAccountFailoverReplayInput = replayCollector.AllItems()
		if imageCount > 0 {
			result.ImageCount = imageCount
			result.ImageSize = imageSizeTier
			result.ImageInputSize = imageInputSize
			result.ImageOutputSizes = imageCounter.Sizes()
			result.BillingModel = imageBillingModel
		}
		return result
	}
	defer func() {
		if forwardErr != nil && (openAIUsageHasTokens(&usage) || imageCounter.Count() > 0) {
			if forwardResult == nil {
				forwardResult = resultWithUsage()
			}
			forwardErr = stopOpenAIMeteredReplay(forwardErr)
		}
	}()

	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	if hasResponsesClientToolMapping(clientToolMapping) {
		resp.Body = newResponsesClientToolStreamBody(resp.Body, clientToolMapping, maxLineSize)
	}
	rawScanner := bufio.NewScanner(resp.Body)
	rawScanner.Split(newSSEScanLines())
	scanBuf := getSSEScannerBuf64K()
	rawScanner.Buffer(scanBuf[:0], maxLineSize)
	scanner := newOpenAISSEJSONDocumentScanner(rawScanner, maxLineSize)
	defer putSSEScannerBuf64K(scanBuf)

	finalizeBareError := func() error {
		if !bareErrorPending {
			return nil
		}
		if !failureAccountSideEffectsApplied {
			failureAccountSideEffectsApplied = s.handleOpenAIWSFailureAccountSideEffects(ctx, account, mappedModel, resp.Header, bareErrorPayload)
		}
		upstreamTerminalEvent = "response.failed"
		if clientDisconnected {
			return nil
		}
		decisionDetail, _ := json.Marshal(progress.details())
		failedEvent := buildOpenAIWSHTTPBridgeFailedEvent(responseID, originalModel, createdAtStamper.Value(), bareErrorPayload, bareErrorMessage)
		clientMessage := failedEvent
		if rewritten, changed := sanitizeOpenAICapacityShedErrorCodeForClient(clientMessage); changed {
			clientMessage = rewritten
		}
		messages := append(pendingClientMessages, clientMessage)
		pendingClientMessages = nil
		pendingClientMessageBytes = 0
		for _, message := range messages {
			if err := writeClientMessage(message); err != nil {
				if isOpenAIWSClientDisconnectError(err) {
					clientDisconnected = true
					return nil
				}
				return fmt.Errorf("write synthesized websocket response.failed: %w", err)
			}
			wroteDownstream = true
		}
		RecordOpenAIRetryStop(c, account, retryStopReason, string(decisionDetail))

		markOpenAIWSClientVisibleFailure(c, gjson.GetBytes(failedEvent, "type").String(), failedEvent)

		return nil
	}
	firstOutputFailure := func() (*OpenAIForwardResult, error) {
		timeoutErr := s.newOpenAIFirstOutputTimeoutError(ctx, c, account, opsUpstreamProxyID(account), opsUpstreamProxyName(account), turnStart, originalModel, gjson.GetBytes(body, "reasoning.effort").String(), firstOutputTimeout, "http_bridge_first_semantic_output", resp.Header)
		if !clientDisconnected && !wroteDownstream && (turn == 1 || replayProven) && openAIWSUncertainExecutionReplaySafe(body) {
			return nil, timeoutErr
		}
		reason := OpenAIRetryStopReplayUnsafe
		if wroteDownstream {
			reason = OpenAIRetryStopDownstreamCommitted
		} else if turn > 1 && !replayProven {
			reason = OpenAIRetryStopHistoryIncomplete
		}
		RecordOpenAIRetryStop(c, account, reason)
		failed := buildOpenAIWSHTTPBridgeErrorEvent(http.StatusGatewayTimeout, "Upstream produced no output before the deadline", timeoutErr.ResponseBody)
		eventType := "error"
		if responseID != "" {
			failed = buildOpenAIWSHTTPBridgeFailedEvent(responseID, originalModel, createdAtStamper.Value(), timeoutErr.ResponseBody, "")
			eventType = "response.failed"
		}
		if !clientDisconnected {
			for _, event := range append(pendingClientMessages, failed) {
				if err := writeClientMessage(event); err != nil {
					return resultWithUsage(), err
				}
			}

			markOpenAIWSClientVisibleFailure(c, eventType, failed)

		}
		upstreamTerminalEvent = "response.failed"
		result := resultWithUsage()
		result.wsReplayIncomplete = true
		return result, &openAIWSTurnRejectedError{cause: errors.New("upstream http bridge first output timeout")}
	}
	for {
		hasFrame := scanner.Scan()
		if firstOutputGuard.expired() && ctx.Err() == nil && !bareErrorPending {
			return firstOutputFailure()
		}
		if !hasFrame {
			break
		}
		frame := scanner.Frame()
		trimmedData := strings.TrimSpace(frame.data)
		if trimmedData == "" {
			continue
		}
		if trimmedData == "[DONE]" {
			sawDone = true
			continue
		}

		upstreamMessage := []byte(openAICompatPayloadWithEventType(trimmedData, frame.eventType))
		timing.ObserveEvent(upstreamMessage)
		if normalized, changed := normalizeCompletedImageGenerationStatus(upstreamMessage); changed {
			upstreamMessage = normalized
		}
		eventType, eventResponseID, _ := parseOpenAIWSEventEnvelope(upstreamMessage)
		progress.observeUpstream(eventType)
		if eventType != "error" && (isOpenAIWSTerminalEvent(eventType) || openAIStreamDataStartsClientOutput(string(upstreamMessage), eventType)) {
			if !stopFirstOutput() {
				return firstOutputFailure()
			}
		}
		if eventType == "response.incomplete" {
			upstreamIncompleteReason = gjson.GetBytes(upstreamMessage, "response.incomplete_details.reason").String()
		}
		responseModelObserver.ObserveOpenAI(upstreamMessage, eventType)
		if responseID == "" && eventResponseID != "" {
			responseID = eventResponseID
		}
		if eventType != "" {
			eventCount++
			if firstEventType == "" {
				firstEventType = eventType
			}
			lastEventType = eventType
		}
		if isOpenAIWSTokenEvent(eventType) {
			tokenEventCount++
			if firstTokenMs == nil {
				ms := int(time.Since(turnStart).Milliseconds())
				firstTokenMs = &ms
			}
		}
		if openAIWSMessageShouldParseUsage(eventType, upstreamMessage) {
			parseOpenAIWSResponseUsageFromCompletedEvent(upstreamMessage, &usage)
		}
		if eventType == "error" || eventType == "response.failed" {
			contextRejected = isOpenAIEncryptedContextRejection(upstreamMessage)
			markOpenAICyberPolicyEvent(c, upstreamMessage, http.StatusOK, &usage)
		}
		imageCounter.AddSSEData(upstreamMessage)

		if needModelReplace && len(mappedModelBytes) > 0 && openAIWSEventMayContainModel(eventType) && strings.Contains(trimmedData, mappedModel) {
			upstreamMessage = replaceOpenAIWSMessageModel(upstreamMessage, mappedModel, originalModel)
		}
		if s.toolCorrector != nil && openAIWSEventMayContainToolCalls(eventType) && openAIWSMessageLikelyContainsToolCalls(upstreamMessage) {
			if corrected, changed := s.toolCorrector.CorrectToolCallsInSSEBytes(upstreamMessage); changed {
				upstreamMessage = corrected
			}
		}
		if stamped, changed := createdAtStamper.Stamp(upstreamMessage, eventType); changed {
			upstreamMessage = stamped
		}
		replayCollector.AddEvent(eventType, upstreamMessage)

		if bareErrorPending {
			setBareErrorIdle(true)
		}
		var upstreamEventErr error
		if officialOpenAIResponses && bareErrorPending && ((isOpenAIWSTerminalEvent(eventType) && eventType != "response.failed") || isOpenAIWSTokenEvent(eventType)) {
			// Some upstreams emit a recoverable bare error before the authoritative
			// terminal or resume semantic output. Do not replace that with a synthetic
			// failure or retain side effects from the superseded error.
			bareErrorPending = false
			setBareErrorIdle(false)
			bareErrorPayload = nil
			bareErrorMessage = ""
			retryStopReason = ""
		}
		suppressClientMessage := officialOpenAIResponses && bareErrorPending && eventType != "response.failed"
		if eventType == "error" || eventType == "response.failed" {
			errMessage := extractOpenAISSEErrorMessage(upstreamMessage)
			if errMessage == "" {
				errMessage = "upstream error event"
			}
			statusCode := openAIStreamFailureStatus(upstreamMessage, errMessage)
			shouldFailover := openAIStreamFailedEventShouldFailover(upstreamMessage, errMessage)
			if eventType == "error" {
				errCodeRaw, errTypeRaw, _ := parseOpenAIWSErrorEventFields(upstreamMessage)
				shouldFailover = openAIStreamErrorEventShouldFailover(upstreamMessage, errMessage)
				if account.Platform == PlatformGrok {
					statusCode = openAIWSErrorHTTPStatusFromRaw(errCodeRaw, errTypeRaw)
				}
			}

			requestScopedCapacity := isOpenAIUpstreamCapacityShedEvent(upstreamMessage)
			if account.Platform == PlatformGrok && eventType == "error" {
				// SSE error events do not carry an HTTP status. The local status
				// mapper therefore defaults unknown xAI codes (for example
				// new_sensitive) to 502; classify the body as a request-scoped
				// 403 before applying status-based failover or account state.
				if isGrokContentPolicyRejection(http.StatusForbidden, upstreamMessage) {
					shouldFailover = false
				} else {
					shouldFailover = s.shouldFailoverGrokUpstreamError(statusCode, upstreamMessage)
					s.handleGrokAccountUpstreamError(ctx, account, statusCode, resp.Header, upstreamMessage)
				}
			}
			// A disconnected client needs this attempt drained for usage, not replayed,
			// even when only non-semantic heartbeats were delivered.
			retryStopReason = ""
			replaySafe := (turn == 1 || replayProven || statusCode == http.StatusTooManyRequests) &&
				(statusCode < 500 || openAIWSUncertainExecutionReplaySafe(body))
			if shouldFailover {
				if wroteDownstream {
					retryStopReason = OpenAIRetryStopDownstreamCommitted
				} else if !replaySafe {
					retryStopReason = OpenAIRetryStopReplayUnsafe
				}
			}
			if !clientDisconnected && !wroteDownstream && shouldFailover && replaySafe {
				if account.Platform == PlatformGrok {
					return nil, newOpenAIUpstreamFailoverError(statusCode, resp.Header, upstreamMessage, errMessage, false)
				}
				return nil, s.newOpenAIStreamFailoverErrorWithModel(c, account, true, resp.Header.Get("x-request-id"), upstreamMessage, errMessage, mappedModel, resp.Header)
			}
			if account.Platform != PlatformGrok && !failureAccountSideEffectsApplied {
				if eventType == "response.failed" || (!officialOpenAIResponses && shouldFailover && !requestScopedCapacity) {
					failureAccountSideEffectsApplied = s.handleOpenAIWSFailureAccountSideEffects(ctx, account, mappedModel, resp.Header, upstreamMessage)
				}
			}
			if wroteDownstream && requestScopedCapacity && !capacityFailoverSuppressedLogged {
				logOpenAICapacityFailoverSuppressed(ctx, account, "ws_http_bridge", resp.Header.Get("x-request-id"), eventType)
				capacityFailoverSuppressedLogged = true
			}
			if eventType == "error" && !officialOpenAIResponses {
				upstreamEventErr = errors.New(errMessage)
			} else if eventType == "error" {
				setBareErrorIdle(true)
				bareErrorPending = true
				bareErrorPayload = append(bareErrorPayload[:0], upstreamMessage...)
				bareErrorMessage = errMessage
				if openAIWSDefinitiveRequestRejection(upstreamMessage, responseID) {
					if err := finalizeBareError(); err != nil {
						return resultWithUsage(), err
					}
					return resultWithUsage(), &openAIWSTurnRejectedError{cause: errors.New(bareErrorMessage)}
				}
				suppressClientMessage = true
			} else {
				bareErrorPending = false
				setBareErrorIdle(false)
			}
		}

		// 客户端写出副本改写容量降载码：Codex 对 error/response.failed 中的
		// server_is_overloaded / slow_down 判致命并终止会话，改写后走客户端内置
		// 重试。账号状态与终止事件判定（下方 handleOpenAIWSTerminalTransientFailure）
		// 仍使用未改写的 upstreamMessage。
		clientMessage := upstreamMessage
		if eventType == "error" || eventType == "response.failed" {
			if rewritten, changed := sanitizeOpenAICapacityShedErrorCodeForClient(clientMessage); changed {
				clientMessage = rewritten
			}
		}
		if !clientDisconnected && !suppressClientMessage {
			isKeepalive := eventType == "keepalive"
			stageBeforeSemanticOutput := (turn == 1 || replayProven) && account.Platform == PlatformOpenAI && !wroteDownstream
			commitStagedMessages := !stageBeforeSemanticOutput ||
				openAIStreamDataStartsClientOutput(string(clientMessage), eventType) ||
				isOpenAIWSTerminalEvent(eventType)
			if stageBeforeSemanticOutput && !commitStagedMessages && !isKeepalive {
				if pendingClientMessageBytes+int64(len(clientMessage)) > openAIFirstOutputStageMaxBytes {
					return nil, s.newOpenAIStreamFailoverError(
						c,
						account,
						true,
						resp.Header.Get("x-request-id"),
						nil,
						"OpenAI WS HTTP bridge first-output staging limit exceeded",
						resp.Header,
					)
				}
				pendingClientMessages = append(pendingClientMessages, append([]byte(nil), clientMessage...))
				pendingClientMessageBytes += int64(len(clientMessage))
			} else {
				// Keep the client connection alive without committing this attempt
				// or exposing its staged lifecycle metadata.
				var messages [][]byte
				if !isKeepalive {
					messages = pendingClientMessages
					pendingClientMessages = nil
					pendingClientMessageBytes = 0
				}
				messages = append(messages, clientMessage)
				for _, message := range messages {
					if err := writeClientMessage(message); err != nil {
						if isOpenAIWSClientDisconnectError(err) {
							clientDisconnected = true
							closeStatus, closeReason := summarizeOpenAIWSReadCloseError(err)
							logOpenAIWSModeInfo(
								"ingress_ws_http_bridge_client_disconnected_drain account_id=%d turn=%d close_status=%s close_reason=%s",
								account.ID,
								turn,
								closeStatus,
								truncateOpenAIWSLogValue(closeReason, openAIWSHeaderValueMaxLen),
							)
							break
						}
						return nil, wrapOpenAIWSIngressTurnError(
							"write_client",
							fmt.Errorf("write client websocket event: %w", err),
							wroteDownstream,
						)
					}
					if !isKeepalive {
						wroteDownstream = true
					}
				}
			}
		}
		if !clientDisconnected && !suppressClientMessage {
			if eventType == "response.failed" || (eventType == "error" && !officialOpenAIResponses) {
				RecordOpenAIRetryStop(c, account, retryStopReason)
			}

			markOpenAIWSClientVisibleFailure(c, eventType, upstreamMessage)

		}

		if upstreamEventErr != nil {
			return resultWithUsage(), upstreamEventErr
		}
		if isOpenAIWSTerminalEvent(eventType) && !bareErrorPending {
			CompleteHTTPUpstreamResponse(resp)

			if eventType == "response.failed" {
				upstreamTerminalEvent = "response.failed"
			} else {
				upstreamTerminalEvent = s.handleOpenAIWSTerminalTransientFailure(ctx, account, mappedModel, resp.Header, upstreamMessage)
			}
			terminalEventCount++
			logOpenAIWSModeInfo(
				"ingress_ws_http_bridge_turn_completed account_id=%d turn=%d response_id=%s payload_bytes=%d events=%d token_events=%d terminal_events=%d first_event=%s last_event=%s client_disconnected=%v",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(responseID, openAIWSIDValueMaxLen),
				payloadBytes,
				eventCount,
				tokenEventCount,
				terminalEventCount,
				truncateOpenAIWSLogValue(firstEventType, openAIWSLogValueMaxLen),
				truncateOpenAIWSLogValue(lastEventType, openAIWSLogValueMaxLen),
				clientDisconnected,
			)
			return resultWithUsage(), nil
		}
	}
	if ctx.Err() != nil {
		return resultWithUsage(), context.Cause(ctx)
	}
	if bareErrorPending {
		if firstOutputGuard.expired() {
			retryStopReason = OpenAIRetryStopTurnBudget
		}
		if finalizeErr := finalizeBareError(); finalizeErr != nil {
			return resultWithUsage(), finalizeErr
		}
		return resultWithUsage(), &openAIWSTurnRejectedError{cause: errors.New(bareErrorMessage)}
	}
	if firstOutputGuard.expired() {
		return firstOutputFailure()
	}

	if err := scanner.Err(); err != nil {
		streamErr := fmt.Errorf("read upstream http bridge stream: %w", err)
		if (turn == 1 || replayProven) && !clientDisconnected && !wroteDownstream && openAIWSUncertainExecutionReplaySafe(body) {
			return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, streamErr, true)
		}
		return resultWithUsage(), streamErr
	}
	terminalErr := errors.New("upstream http bridge stream ended before terminal event")
	if sawDone {
		terminalErr = errors.New("upstream http bridge stream sent [DONE] before terminal event")
	}
	if (turn == 1 || replayProven) && !clientDisconnected && !wroteDownstream && openAIWSUncertainExecutionReplaySafe(body) {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, terminalErr, true)
	}
	return resultWithUsage(), terminalErr
}

func resolveGrokWSCacheIdentity(c *gin.Context, account *Account, seedPayload, currentPayload []byte, originalModel string) (string, error) {
	body, err := prepareOpenAIWSHTTPBridgeBody(account, seedPayload)
	if err != nil {
		return "", err
	}
	upstreamModel := resolveGrokWSUpstreamModel(account, currentPayload, originalModel)
	body, err = patchGrokResponsesBody(body, upstreamModel)
	if err != nil {
		return "", err
	}
	return resolveGrokCacheIdentity(c, body, "", upstreamModel), nil
}

func resolveGrokWSUpstreamModel(account *Account, body []byte, originalModel string) string {
	upstreamModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	originalModel = strings.TrimSpace(originalModel)
	// Shared ingress has already applied channel and account mappings when the
	// body model differs from the client-facing model. Only resolve from the
	// original model when the body still carries that original value.
	if account != nil && originalModel != "" && (upstreamModel == "" || upstreamModel == originalModel) {
		if mappedModel := normalizeOpenAIModelForUpstream(account, account.GetMappedModel(originalModel)); mappedModel != "" {
			upstreamModel = mappedModel
		}
	}
	if upstreamModel == "" {
		upstreamModel = grokDefaultResponsesModel
	}
	return upstreamModel
}
