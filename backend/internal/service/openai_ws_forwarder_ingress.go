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
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type openAIWSRejectedFieldRetryError struct {
	body      []byte
	reason    string
	reconnect bool
}

type openAIWSBridgeReplayKey struct{}

func (e *openAIWSRejectedFieldRetryError) Error() string {
	if e == nil || strings.TrimSpace(e.reason) == "" {
		return "retry websocket turn after rejected field normalization"
	}
	return "retry websocket turn after rejected field normalization: " + e.reason
}

func openAIWSRejectedFieldRetryHTTPStatus(message []byte) int {
	for _, value := range gjson.GetManyBytes(message, "status", "status_code", "error.status", "error.status_code", "response.error.status", "response.error.status_code") {
		status := int(value.Int())
		if status >= 100 && status <= 599 {
			return status
		}
	}
	return openAIWSErrorHTTPStatus(message)
}

func (s *OpenAIGatewayService) openAIWSIngressInterTurnIdleTimeout() time.Duration {
	if s == nil || s.cfg == nil || s.cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds <= 0 {
		return 0
	}
	return time.Duration(s.cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds) * time.Second
}

// newOpenAIWSDownstreamWriteContext binds writes directly to the client
// lifecycle while excluding the separate ingress-lease cancellation signal.
// This lets a lease-loss path finish its current client write before
// ReadOpenAIWSClientMessage sends the retryable close frame.
func newOpenAIWSDownstreamWriteContext(controlCtx context.Context, hooks *OpenAIWSIngressHooks, timeout time.Duration) (context.Context, context.CancelFunc) {
	writeParent := controlCtx
	if hooks != nil && hooks.ClientLifecycleContext != nil {
		writeParent = hooks.ClientLifecycleContext
	}
	if writeParent == nil {
		writeParent = context.Background()
	}
	return context.WithTimeout(writeParent, timeout)
}

func (s *OpenAIGatewayService) ProxyResponsesWebSocketFromClient(
	ctx context.Context,
	c *gin.Context,
	clientConn OpenAIWSDownstreamConn,
	account *Account,
	token string,
	firstClientMessage []byte,
	hooks *OpenAIWSIngressHooks,
) (returnErr error) {
	if s == nil {
		return errors.New("service is nil")
	}
	if c == nil {
		return errors.New("gin context is nil")
	}
	if clientConn == nil {
		return errors.New("client websocket is nil")
	}
	if account == nil {
		return errors.New("account is nil")
	}
	downstream, managed := clientConn.(*OpenAIWSDownstream)
	if !managed {
		if cached, ok := c.Get("openai_ws_downstream_owner"); ok {
			downstream, _ = cached.(*OpenAIWSDownstream)
		}
		if downstream == nil || downstream.conn != clientConn {
			downstream = NewOpenAIWSDownstream(clientConn, ResolveOpenAIWSClientReadLimitBytes(s.cfg))
			c.Set("openai_ws_downstream_owner", downstream)
		}
		clientConn = downstream
		defer func() {
			var failover *UpstreamFailoverError
			var closeErr *OpenAIWSClientCloseError
			// Legacy direct callers own protocol closes and may inspect the
			// returned error before reading the peer. Keep their reader alive
			// until they close the socket; production passes an owned connection.
			var changed *OpenAIWSAccountChangedError
			if !errors.As(returnErr, &failover) && !errors.As(returnErr, &closeErr) && !errors.As(returnErr, &changed) {
				_ = downstream.CloseNow()
			}
		}()
	}
	downstream.controlsEnabled.Store(true)
	ctx = downstream.TurnBudgetContext(ctx, s.cfg, firstClientMessage)
	s.installSchedulingRecovery(ctx, gjson.GetBytes(firstClientMessage, "model").String())
	if err := downstream.bindRequestEnvelope(firstClientMessage); err != nil {
		return err
	}
	if downstream.turn.Load() == 0 {
		downstream.turn.Add(1)
		downstream.active.Store(true)
	}
	if err := s.validateWSContinuation(ctx, c, firstClientMessage); err != nil {
		return err
	}
	writeCodexRestrictionEvent := func(event []byte) error {
		writeCtx, cancel := newOpenAIWSDownstreamWriteContext(ctx, hooks, s.openAIWSWriteTimeout())
		defer cancel()
		return clientConn.Write(writeCtx, coderws.MessageText, event)
	}
	if err := s.checkOpenAIWSCodexClientRestriction(ctx, c, account, firstClientMessage, writeCodexRestrictionEvent); err != nil {
		return err
	}
	// A handler may reuse the same gin context across account failover attempts.
	// Never let an OAuth attempt's response aliases leak into the next account.
	setCodexToolNameReverse(c, nil)
	if !account.IsSIWCEnabled() {
		if _, err := s.prepareCodexAccountIdentitySource(ctx, c, account); err != nil {
			return err
		}
	}
	if err := validateOpenAIWSBearerToken(account, token); err != nil {
		return err
	}

	// 预取一次 OpenAI Fast Policy settings，绑定到 ctx，让该 WS session
	// 内所有帧的 evaluateOpenAIFastPolicy 调用复用同一份快照，避免每帧
	// 进入 DB / settingRepo。Trade-off 见 withOpenAIFastPolicyContext 注释。
	if s.settingService != nil {
		if settings, err := s.settingService.GetOpenAIFastPolicySettings(ctx); err == nil && settings != nil {
			ctx = withOpenAIFastPolicyContext(ctx, settings)
		}
	}

	wsDecision := s.getOpenAIWSProtocolResolver().Resolve(account)
	turnParentContext := ctx // do not retain a chain of budgets across a long session
	firstBPSRequestModel := gjson.GetBytes(firstClientMessage, "model").String()
	firstBPSModelMapped := false
	if s.excelBPSGloballyEnabled(ctx) && account.IsExcelBPSEnabled() && hooks != nil && hooks.MapRequestModel != nil {
		mapped, err := hooks.MapRequestModel(1, firstBPSRequestModel)
		if err != nil {
			return err
		}
		if strings.TrimSpace(mapped) != "" {
			firstBPSRequestModel = mapped
		}
		firstBPSModelMapped = true
	}
	bpsBridge := !account.IsSIWCEnabled() && s.excelBPSGloballyEnabled(ctx) && account.IsExcelBPSEnabledForModel(firstBPSRequestModel)
	// The handler normally owns this registration across retry attempts. Direct
	// callers still get the same session-scoped preemption behavior here.
	if preemptCtx, cleanupPreempt, armed := s.BeginOpenAIWSIngressSessionPreemptionWithClient(ctx, c, account, firstClientMessage, clientConn, firstBPSRequestModel); armed {
		ctx = preemptCtx
		defer cleanupPreempt()
		defer func() {
			if isOpenAIWSSessionPreempted(ctx) {
				returnErr = errOpenAIWSSessionPreempted
			}
		}()
	}

	if account.IsExcelOAuth() && !bpsBridge {
		return newOpenAIWSNativeModelSwitchError(firstBPSRequestModel)
	}
	forceHTTPBridge := account.IsSIWCEnabled() || bpsBridge || account.Platform == PlatformGrok ||
		(s.pluginManager != nil && s.pluginManager.ShouldRouteOpenAIOAuth(account))
	modeRouterV2Enabled := s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled
	ingressMode := OpenAIWSIngressModeCtxPool
	if modeRouterV2Enabled && !forceHTTPBridge {
		ingressMode = account.ResolveOpenAIResponsesWebSocketV2Mode(s.cfg.Gateway.OpenAIWS.IngressModeDefault)
		if ingressMode == OpenAIWSIngressModeOff {
			return NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				"websocket mode is disabled for this account",
				nil,
			)
		}
		switch ingressMode {
		case OpenAIWSIngressModePassthrough:
			if wsDecision.Transport != OpenAIUpstreamTransportResponsesWebsocketV2 {
				return fmt.Errorf("websocket ingress requires ws_v2 transport, got=%s", wsDecision.Transport)
			}
			if s.shouldBridgeOpenAIWSPassthroughFirstMessage(account, firstClientMessage) {
				forceHTTPBridge = true
				break
			}
			downstream.controlsEnabled.Store(false)
			downstream.modeSelected.Store(true)
			// 首轮准入由握手路径完成；后续 response.create 会在写入上游前
			// 依次回调 BeforeRequest 和 BeforeTurn，并在终止或失败时回调
			// AfterTurn，从而覆盖 turn 级利润复核、定价冻结和并发槽位释放。
			return s.proxyResponsesWebSocketV2Passthrough(
				ctx,
				c,
				clientConn,
				account,
				token,
				firstClientMessage,
				hooks,
				wsDecision,
			)
		case OpenAIWSIngressModeHTTPBridge:
			forceHTTPBridge = true
		case OpenAIWSIngressModeCtxPool, OpenAIWSIngressModeShared, OpenAIWSIngressModeDedicated:
			// continue
		default:
			return NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				"websocket mode only supports ctx_pool/passthrough/http_bridge",
				nil,
			)
		}
	}
	downstream.modeSelected.Store(true)
	if !forceHTTPBridge && wsDecision.Transport != OpenAIUpstreamTransportResponsesWebsocketV2 {
		return fmt.Errorf("websocket ingress requires ws_v2 transport, got=%s", wsDecision.Transport)
	}
	dedicatedMode := modeRouterV2Enabled && ingressMode == OpenAIWSIngressModeDedicated

	wsURL := ""
	wsHost := "-"
	wsPath := "-"
	if forceHTTPBridge {
		wsHost = "xai-http-bridge"
		wsPath = "/v1/responses"
		if account.IsSIWCEnabled() {
			wsHost = "api.openai.com"
		} else if bpsBridge {
			wsHost = "bps.openai.com"
			wsPath = "/basispoints/api/responses"
		}
	} else {
		var err error
		wsURL, err = s.buildOpenAIResponsesWSURL(account)
		if err != nil {
			return fmt.Errorf("build ws url: %w", err)
		}
		if parsedURL, parseErr := url.Parse(wsURL); parseErr == nil && parsedURL != nil {
			wsHost = normalizeOpenAIWSLogValue(parsedURL.Host)
			wsPath = normalizeOpenAIWSLogValue(parsedURL.Path)
		}
	}
	debugEnabled := isOpenAIWSModeDebugEnabled()
	isCodexCLI := openai.IsCodexOfficialClientByHeaders(c.GetHeader("User-Agent"), c.GetHeader("originator")) || (s.cfg != nil && s.cfg.Gateway.ForceCodexCLI)

	type openAIWSClientPayload struct {
		payloadRaw               []byte
		accountIdentitySourceRaw []byte
		rawForHash               []byte
		promptCacheKey           string
		previousResponseID       string
		originalModel            string
		imageBillingModel        string
		imageSizeTier            string
		imageInputSize           string
		payloadBytes             int
		requestedReasoningEffort *string
	}
	ingressSessionOriginalModel := ""

	applyPayloadMutation := func(current []byte, path string, value any) ([]byte, error) {
		next, err := sjson.SetBytes(current, path, value)
		if err == nil {
			return next, nil
		}

		// 仅在确实需要修改 payload 且 sjson 失败时，退回 map 路径确保兼容性。
		payload := make(map[string]any)
		if unmarshalErr := json.Unmarshal(current, &payload); unmarshalErr != nil {
			return nil, err
		}
		switch path {
		case "type", "model":
			payload[path] = value
		case "client_metadata." + openAIWSTurnMetadataHeader:
			setOpenAIWSTurnMetadata(payload, fmt.Sprintf("%v", value))
		default:
			return nil, err
		}
		rebuilt, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return nil, marshalErr
		}
		return rebuilt, nil
	}

	parseClientPayload := func(turn int, raw []byte) (openAIWSClientPayload, error) {
		if err := s.validateWSContinuation(ctx, c, raw); err != nil {
			return openAIWSClientPayload{}, err
		}
		if turn > 1 {
			if err := s.checkOpenAIWSCodexClientRestriction(ctx, c, account, raw, writeCodexRestrictionEvent); err != nil {
				return openAIWSClientPayload{}, err
			}
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "empty websocket request payload", nil)
		}
		if !gjson.ValidBytes(trimmed) {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", errors.New("invalid json"))
		}
		if err := downstream.bindRequestEnvelope(trimmed); err != nil {
			return openAIWSClientPayload{}, err
		}

		values := gjson.GetManyBytes(trimmed, "type", "model", "prompt_cache_key", "previous_response_id")
		eventType := strings.TrimSpace(values[0].String())
		normalized := trimmed
		switch eventType {
		case "":
			eventType = "response.create"
			next, setErr := applyPayloadMutation(normalized, "type", eventType)
			if setErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", setErr)
			}
			normalized = next
		case "response.create":
		case "response.append":
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				"response.append is not supported in ws v2; use response.create with previous_response_id",
				nil,
			)
		default:
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				fmt.Sprintf("unsupported websocket request type: %s", eventType),
				nil,
			)
		}
		requestedReasoningEffort := CanonicalRequestedReasoningEffort(normalized, strings.TrimSpace(values[1].String()))
		if next, policyErr := applyOpenAIWSReasoningEffortPolicy(normalized, hooks); policyErr != nil {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, policyErr.Error(), policyErr)
		} else {
			normalized = next
		}
		responsesLite := isOpenAIResponsesLiteWebSocketPayload(normalized)
		if compatibilityBody, compatibilityChanged, compatibilityErr := normalizeOpenAIResponsesWebSocketCompatibilityBodyWithConfig(s.cfg, normalized, account, responsesLite); compatibilityErr != nil {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", compatibilityErr)
		} else if compatibilityChanged {
			normalized = compatibilityBody
		}
		if account.IsOpenAI() && gjson.GetBytes(normalized, "generate").Type != gjson.False {
			if inputErr := validateOpenAIResponsesInputTransition(trimmed, normalized); inputErr != nil {
				return openAIWSClientPayload{}, inputErr
			}
		}
		if account.IsOpenAIOAuthLike() && !account.IsSIWCEnabled() {
			aliasedBody, reverse, aliased, aliasErr := aliasOpenAIOAuthReservedToolNamesBody(normalized)
			if aliasErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, aliasErr.Error(), aliasErr)
			}
			updateCodexToolNameReverseForWSFrame(c, normalized, reverse)
			if aliased {
				normalized = aliasedBody
			}
		}

		originalModel := strings.TrimSpace(values[1].String())
		modelMissing := originalModel == ""
		if originalModel == "" {
			// 入站 WS 长会话里，部分客户端只在第一轮 response.create 上声明
			// model，后续 turn 复用同一 session-level model。为避免因省略
			// model 直接断开用户连接，这里回落到上一轮已通过校验的客户端模型，
			// 并在下方写回上游 payload，保证账号模型映射/fast policy/图片权限
			// 仍按同一模型执行。
			originalModel = ingressSessionOriginalModel
			if originalModel == "" {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(
					coderws.StatusPolicyViolation,
					"model is required in response.create payload",
					nil,
				)
			}
		}
		promptCacheKey := strings.TrimSpace(values[2].String())
		previousResponseID := strings.TrimSpace(values[3].String())
		previousResponseIDKind := ClassifyOpenAIPreviousResponseIDKind(previousResponseID)
		if previousResponseID != "" && previousResponseIDKind == OpenAIPreviousResponseIDKindMessageID {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				"previous_response_id must be a response.id (resp_*), not a message id",
				nil,
			)
		}
		if turnMetadata := strings.TrimSpace(c.GetHeader(openAIWSTurnMetadataHeader)); turn == 1 && turnMetadata != "" && !gjson.GetBytes(normalized, "client_metadata."+openAIWSTurnMetadataHeader).Exists() {
			next, setErr := applyPayloadMutation(normalized, "client_metadata."+openAIWSTurnMetadataHeader, turnMetadata)
			if setErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", setErr)
			}
			normalized = next
		}
		accountIdentitySourceRaw := append([]byte(nil), normalized...)
		if !account.IsSIWCEnabled() {
			accountScopedPayload, accountScoped, scopeErr := applyCodexAccountIdentityClientMetadataRaw(normalized, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c))
			if scopeErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket identity metadata", scopeErr)
			}
			if accountScoped {
				normalized = accountScopedPayload
			}
			ids := resolveCodexFingerprintIDsForCaller(codexAccountIdentitySource(c, account), c.Request.Header, getAPIKeyIDFromContext(c))
			if next, _, fpErr := applyCodexFingerprintClientMetadataRaw(normalized, ids); fpErr != nil {
				return openAIWSClientPayload{}, fpErr
			} else {
				normalized = next
			}
			stageCodexFingerprintIDs(c, ids)
			if next, _, metadataErr := normalizeCodexRequestMetadata(normalized, nil, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c), false); metadataErr != nil {
				return openAIWSClientPayload{}, metadataErr
			} else {
				normalized = next
			}
		}

		if responsesLite {
			litePayload, _, liteErr := normalizeOpenAIResponsesLitePayloadForAccount(normalized, account)
			if liteErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(
					coderws.StatusPolicyViolation,
					liteErr.Error(),
					liteErr,
				)
			}
			normalized = litePayload
		}
		apiKey := getAPIKeyFromContext(c)
		imageGenerationAllowed := GroupAllowsImageGeneration(apiKeyGroup(apiKey))
		codexImageGenerationExplicitToolPolicy := codexImageGenerationExplicitToolPolicyAllow
		if isCodexCLI {
			codexImageGenerationExplicitToolPolicy = account.CodexImageGenerationExplicitToolPolicy()
		}
		codexBridgeEnabled := !account.IsSIWCEnabled() && !bpsBridge && isCodexCLI &&
			!isOpenAIResponsesLiteWebSocketPayload(normalized) &&
			imageGenerationAllowed &&
			codexImageGenerationExplicitToolPolicy != codexImageGenerationExplicitToolPolicyStrip &&
			s.isCodexImageGenerationBridgeEnabled(ctx, account, apiKey)
		if codexBridgeEnabled {
			payloadMap := make(map[string]any)
			if err := decodeOpenAIJSONUseNumber(normalized, &payloadMap); err != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", err)
			}
			bridgeModified := false
			if ensureOpenAIResponsesImageGenerationTool(payloadMap) {
				bridgeModified = true
				logOpenAIWSModeInfo("ingress_ws_codex_image_tool_injected account_id=%d", account.ID)
			}
			if ensureOpenAIResponsesImageGenerationToolChoiceAuto(payloadMap) {
				bridgeModified = true
				logOpenAIWSModeInfo("ingress_ws_codex_image_tool_choice_auto account_id=%d", account.ID)
			}
			if normalizeOpenAIResponsesImageGenerationTools(payloadMap) {
				bridgeModified = true
			}
			if applyCodexImageGenerationBridgeInstructions(payloadMap) {
				bridgeModified = true
				logOpenAIWSModeInfo("ingress_ws_codex_image_bridge_instructions_added account_id=%d", account.ID)
			}
			if bridgeModified {
				rebuilt, marshalErr := json.Marshal(payloadMap)
				if marshalErr != nil {
					return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", marshalErr)
				}
				normalized = rebuilt
			}
		}
		requestModel := originalModel
		if turn == 1 && firstBPSModelMapped {
			requestModel = firstBPSRequestModel
		} else if hooks != nil && hooks.MapRequestModel != nil {
			mappedModel, mapErr := hooks.MapRequestModel(turn, originalModel)
			if mapErr != nil {
				return openAIWSClientPayload{}, mapErr
			}
			if mappedModel = strings.TrimSpace(mappedModel); mappedModel != "" {
				requestModel = mappedModel
			}
		}
		upstreamModel := normalizeOpenAIModelForUpstream(account, account.GetMappedModel(requestModel))
		if modelMissing || upstreamModel != originalModel {
			next, setErr := applyPayloadMutation(normalized, "model", upstreamModel)
			if setErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", setErr)
			}
			normalized = next
		}
		SetOpsUpstreamModel(c, upstreamModel)
		if isCodexCLI && codexImageGenerationExplicitToolPolicy == codexImageGenerationExplicitToolPolicyStrip {
			if stripped, changed, stripErr := stripOpenAIImageGenerationToolsFromRawPayload(normalized); stripErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", stripErr)
			} else if changed {
				normalized = stripped
				logOpenAIWSModeInfo("ingress_ws_codex_image_tool_stripped_by_policy account_id=%d", account.ID)
			}
		}
		if stripped, changed, stripErr := stripCodexSparkImageGenerationToolFromRawPayload(normalized, upstreamModel); stripErr != nil {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", stripErr)
		} else if changed {
			normalized = stripped
			logOpenAIWSModeInfo("ingress_ws_codex_spark_image_tool_stripped account_id=%d", account.ID)
		}
		imageIntent := IsImageGenerationIntentForPlatform(openAIResponsesEndpoint, originalModel, normalized, account.Platform)
		if imageIntent && !imageGenerationAllowed {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, ImageGenerationPermissionMessage(), nil)
		}
		imageBillingModel := ""
		imageSizeTier := ""
		imageInputSize := ""
		if imageIntent {
			var imageCfgErr error
			imageCfg, imageCfgErr := resolveOpenAIResponsesImageBillingConfigDetailedFromBody(normalized, originalModel)
			if imageCfgErr != nil {
				return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, imageCfgErr.Error(), imageCfgErr)
			}
			imageBillingModel = imageCfg.Model
			imageSizeTier = imageCfg.SizeTier
			imageInputSize = imageCfg.InputSize
		}

		// Apply OpenAI Fast Policy on the response.create frame using the same
		// evaluator/normalize/scope rules as the HTTP entrypoints. This is the
		// single integration point for all WS ingress turns (first + follow-up
		// frames flow through here).
		//
		// Model fallback: first turn still requires model at the handler layer；
		// follow-up response.create frames may omit it and then reuse
		// ingressSessionOriginalModel. We always write a concrete upstream model
		// before evaluating policy, so whitelist / filter behavior remains stable.
		policyApplied, blocked, policyErr := s.applyOpenAIFastPolicyToWSResponseCreate(ctx, account, upstreamModel, normalized)
		if policyErr != nil {
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", policyErr)
		}
		if blocked != nil {
			MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
			// Send a Realtime-style error event to the client first, then
			// signal the handler to close the connection with PolicyViolation.
			// We intentionally do NOT forward this frame upstream.
			//
			// coder/websocket@v1.8.14 Conn.Write is synchronous and flushes
			// the underlying bufio writer before returning (write.go:42 →
			// 307-311), and the subsequent close handshake re-acquires the
			// same writeFrameMu, so the error event is guaranteed to reach
			// the kernel send buffer before any close frame is queued.
			eventBytes := buildOpenAIFastPolicyBlockedWSEvent(blocked)
			if eventBytes != nil {
				writeCtx, cancel := newOpenAIWSDownstreamWriteContext(ctx, hooks, s.openAIWSWriteTimeout())
				_ = clientConn.Write(writeCtx, coderws.MessageText, eventBytes)
				cancel()
			}
			return openAIWSClientPayload{}, NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				blocked.Message,
				blocked,
			)
		}
		normalized = policyApplied
		if bpsBridge && (!s.excelBPSGloballyEnabled(ctx) || !account.IsExcelBPSEnabledForModel(requestModel)) {
			return openAIWSClientPayload{}, newOpenAIWSNativeModelSwitchError(originalModel)
		}
		if !forceHTTPBridge && s.excelBPSGloballyEnabled(ctx) && account.IsExcelBPSEnabledForModel(requestModel) {
			return openAIWSClientPayload{}, newOpenAIWSBPSModelSwitchError(originalModel)
		}
		ingressSessionOriginalModel = originalModel

		return openAIWSClientPayload{
			payloadRaw:               normalized,
			accountIdentitySourceRaw: accountIdentitySourceRaw,
			rawForHash:               trimmed,
			promptCacheKey:           promptCacheKey,
			previousResponseID:       previousResponseID,
			originalModel:            originalModel,
			imageBillingModel:        imageBillingModel,
			imageSizeTier:            imageSizeTier,
			imageInputSize:           imageInputSize,
			payloadBytes:             len(normalized),
			requestedReasoningEffort: requestedReasoningEffort,
		}, nil
	}

	writeClientMessage := func(message []byte) error {
		writeCtx, cancel := newOpenAIWSDownstreamWriteContext(ctx, hooks, s.openAIWSWriteTimeout())
		defer cancel()
		message = restoreCodexToolNamesFromContext(c, message)
		if err := clientConn.Write(writeCtx, coderws.MessageText, message); err != nil {
			return err
		}
		if id := strings.TrimSpace(gjson.GetBytes(message, "response.id").String()); id != "" {
			s.bindHTTPResponseAccount(ctx, c, account, id)
		}
		return nil
	}

	readClientMessage := func() ([]byte, error) {
		idleTimeout := s.openAIWSIngressInterTurnIdleTimeout()
		msgType, payload, readErr := ReadOpenAIWSClientMessage(
			ctx,
			clientConn,
			idleTimeout,
			coderws.StatusNormalClosure,
			"websocket idle timeout",
		)
		if readErr != nil {
			var closeErr *OpenAIWSClientCloseError
			if errors.As(readErr, &closeErr) && closeErr.StatusCode() == coderws.StatusNormalClosure {
				logOpenAIWSModeInfo("ingress_ws_inter_turn_idle_timeout account_id=%d timeout_seconds=%d", account.ID, int(idleTimeout.Seconds()))
			}
			return nil, readErr
		}
		if msgType != coderws.MessageText && msgType != coderws.MessageBinary {
			return nil, NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation,
				fmt.Sprintf("unsupported websocket client message type: %s", msgType.String()),
				nil,
			)
		}
		return payload, nil
	}

	// Invalid context rejects one request, not the authenticated connection.
	// Read through the existing single reader and serialize errors through the
	// existing writer; accepted turn/history/budget state is unchanged.
	parseNextValidPayload := func(turn int, raw []byte) (openAIWSClientPayload, error) {
		for {
			payload, err := parseClientPayload(turn, raw)
			var inputErr *openAIResponsesInputError
			if !errors.As(err, &inputErr) {
				return payload, err
			}
			event, err := inputErr.websocketEvent(raw)
			if err != nil {
				return openAIWSClientPayload{}, err
			}
			if err = writeClientMessage(event); err != nil {
				return openAIWSClientPayload{}, err
			}
			raw, err = readClientMessage()
			if err != nil {
				return openAIWSClientPayload{}, err
			}
		}
	}

	firstPayload, err := parseNextValidPayload(1, firstClientMessage)
	if err != nil {
		return err
	}

	useHTTPBridge := forceHTTPBridge || s.shouldBridgeOpenAIWSHTTP(account, firstPayload.payloadBytes, firstPayload.previousResponseID)
	turnState := strings.TrimSpace(c.GetHeader(openAIWSTurnStateHeader))
	stateStore := s.getOpenAIWSStateStore()
	groupID := getOpenAIGroupIDFromContext(c)
	apiKeyID := getAPIKeyIDFromContext(c)
	sessionHash := ""
	preferredConnID := ""
	storeDisabled := false
	refreshIngressRouteState := func(payload openAIWSClientPayload) {
		// 会话级状态按执行作用域隔离：codex 多智能体共用 session-id，只有线程标识能把
		// 父线程与子智能体区分开；没有声明身份时沿用原会话哈希。账号粘性仍由 handler 决定。
		sessionHash = s.GenerateSessionHash(c, payload.rawForHash)
		if scope, _ := resolveOpenAIWSExecutionScope(c, payload.rawForHash, apiKeyID); scope != "" {
			sessionHash = scope
		}
		preferredConnID = ""
		storeDisabled = s.isOpenAIWSStoreDisabledInRequestRaw(payload.payloadRaw, account)
		if useHTTPBridge {
			// Sticky account affinity may be shared, but an HTTP bridge must not
			// inherit another connection's native WS turn state or socket binding.
			return
		}
		if turnState == "" && stateStore != nil && sessionHash != "" {
			if savedTurnState, ok := stateStore.GetSessionTurnState(groupID, sessionHash); ok {
				turnState = savedTurnState
			}
		}

		if stateStore != nil && payload.previousResponseID != "" {
			if connID, ok := stateStore.GetResponseConn(payload.previousResponseID); ok {
				preferredConnID = connID
			}
		}

		if stateStore != nil && storeDisabled && payload.previousResponseID == "" && sessionHash != "" {
			if connID, ok := stateStore.GetSessionConn(groupID, sessionHash); ok {
				preferredConnID = connID
			}
		}
	}
	refreshIngressRouteState(firstPayload)

	if useHTTPBridge {
		logOpenAIWSModeInfo(
			"ingress_ws_http_bridge_start account_id=%d account_type=%s payload_bytes=%d threshold_bytes=%d has_session_hash=%v store_disabled=%v",
			account.ID,
			account.Type,
			firstPayload.payloadBytes,
			s.openAIWSHTTPBridgeThresholdBytes(),
			sessionHash != "",
			storeDisabled,
		)
		// Keep the existing SIWC owner/history/turn coordinator. Only the
		// upstream transport changes when the existing WS settings permit it.
		var siwcSession *openAIWSSIWCSession
		if account.IsSIWCEnabled() && wsDecision.Transport == OpenAIUpstreamTransportResponsesWebsocketV2 {
			siwcSession = newOpenAIWSSIWCSession(c, account)
			defer siwcSession.close()
		}
		currentBridgePayload := firstPayload
		// Keep the first turn as the stable conversation seed. The mapped model
		// is resolved again for each turn below so an in-connection model switch
		// cannot reuse another model's upstream cache identity.
		grokCacheSeedPayload := firstPayload.payloadRaw
		var bridgeReplayInput []json.RawMessage
		bridgeReplayInputExists := false
		var bridgeAccountFailoverInput []json.RawMessage
		bridgeAccountFailoverInputExists := false
		bridgeHistoryComplete := firstPayload.previousResponseID == ""
		bridgeLastResponseID := ""
		var siwcTools json.RawMessage
		bridgeOwner, ownerErr := s.openAIWSSIWCBridgeOwner(ctx, c, account)
		if ownerErr != nil {
			return ownerErr
		}
		if saved := downstream.bridgeHistory; saved.accountID == account.ID && saved.siwcOwner == bridgeOwner {
			bridgeReplayInput, bridgeReplayInputExists = saved.input, saved.exists
			bridgeAccountFailoverInput, bridgeAccountFailoverInputExists = saved.portableInput, saved.portableExists
			bridgeHistoryComplete, bridgeLastResponseID = saved.complete, saved.responseID
			siwcTools = saved.siwcTools
		}
		defer func() {
			downstream.bridgeHistory = openAIWSRecoveryHistory{accountID: account.ID, siwcOwner: bridgeOwner, siwcTools: siwcTools, input: bridgeReplayInput, exists: bridgeReplayInputExists, portableInput: bridgeAccountFailoverInput, portableExists: bridgeAccountFailoverInputExists, complete: bridgeHistoryComplete, responseID: bridgeLastResponseID}
		}()

		for turn := 1; ; turn++ {
			if turn > 1 {
				downstream.budget.Store(NewOpenAITurnBudget(s.cfg))
				ctx = downstream.TurnBudgetContext(turnParentContext, s.cfg, currentBridgePayload.payloadRaw)
				s.installSchedulingRecovery(ctx, gjson.GetBytes(currentBridgePayload.payloadRaw, "model").String())
			}
			var accountRefreshErr error
			if turn > 1 && hooks != nil && hooks.BeforeRequest != nil {
				if err := hooks.BeforeRequest(turn, currentBridgePayload.rawForHash, currentBridgePayload.originalModel); err != nil {
					var changed *OpenAIWSAccountChangedError
					if !errors.As(err, &changed) {
						return err
					}
					accountRefreshErr = err
				}
			}
			if accountRefreshErr == nil && hooks != nil && hooks.BeforeTurn != nil {
				if err := hooks.BeforeTurn(turn); err != nil {
					return err
				}
			}
			if turnState != "" && c != nil && c.Request != nil {
				c.Request.Header.Set(openAIWSTurnStateHeader, turnState)
			}
			bridgePayloadRaw := currentBridgePayload.payloadRaw
			bridgePayloadBytes := currentBridgePayload.payloadBytes
			toolOutputCoverage := AnalyzeToolCallOutputContextCoverageBytes(currentBridgePayload.payloadRaw)
			bridgeWarmup := gjson.GetBytes(bridgePayloadRaw, "generate").Type == gjson.False
			bridgePreviousID := currentBridgePayload.previousResponseID
			matchingPrevious := bridgePreviousID != "" && bridgePreviousID == bridgeLastResponseID
			inferToolHistory := bridgePreviousID == "" && toolOutputCoverage.HasFunctionCallOutput && !toolOutputCoverage.ContextCoversAllCallIDs
			needsBridgeReplay := (matchingPrevious || inferToolHistory) && bridgeHistoryComplete && bridgeReplayInputExists
			turnHistoryComplete := (bridgePreviousID == "" && !inferToolHistory) || needsBridgeReplay
			if account.Platform == PlatformOpenAI {
				for _, field := range []string{"prompt", "conversation"} {
					value := gjson.GetBytes(bridgePayloadRaw, field)
					if value.Exists() && value.Type != gjson.Null && value.Raw != `""` {
						turnHistoryComplete = false // the HTTP provider owns the expanded context
					}
				}
			}
			if account.Platform == PlatformGrok {
				needsBridgeReplay = bridgePreviousID != "" || inferToolHistory
				turnHistoryComplete = bridgeHistoryComplete
			}
			// The current request's store=false does not tell us whether its
			// predecessor was persisted. Leave real unresolved IDs for HTTP to
			// resolve; locally acknowledged warmup IDs have no upstream state.
			if account.Platform == PlatformOpenAI && bridgePreviousID != "" && !needsBridgeReplay && (account.IsSIWCEnabled() || bridgeWarmup || strings.HasPrefix(bridgePreviousID, "resp_bridge_warmup_")) {
				return NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater,
					"continuation context unavailable; reconnect with full input context", ErrOpenAIWSReplayUnavailable)
			}
			currentBridgeOwner, ownerErr := s.openAIWSSIWCBridgeOwner(ctx, c, account)
			if ownerErr != nil {
				return ownerErr
			}
			if account.IsSIWCEnabled() && (!turnHistoryComplete || bridgeOwner != currentBridgeOwner) {
				return NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater,
					"SIWC continuation requires the same authorized owner and full input context", ErrOpenAIWSReplayUnavailable)
			}
			// 一次解析当前 input，正常 replay 与 account-failover 两份序列共享同一批正文。
			bridgeCurrentItems, bridgeCurrentItemsExist, extractErr := openAIWSExtractNormalizedInputSequence(
				currentBridgePayload.payloadRaw,
			)
			if extractErr != nil {
				return fmt.Errorf("build websocket http bridge replay input: %w", extractErr)
			}
			if bridgeWarmup && bridgePreviousID == "" && (!bridgeCurrentItemsExist || gjson.GetBytes(bridgePayloadRaw, "input").Type == gjson.Null) {
				bridgeCurrentItems = nil
				bridgeCurrentItemsExist = true // an empty prewarm is a complete local context
			}
			turnReplayInput, turnReplayInputExists := buildOpenAIWSReplayInputSequenceFromItems(
				bridgeReplayInput,
				bridgeReplayInputExists,
				bridgeCurrentItems,
				bridgeCurrentItemsExist,
				needsBridgeReplay,
			)
			turnAccountFailoverInput, turnAccountFailoverInputExists := buildOpenAIWSReplayInputSequenceFromItems(
				bridgeAccountFailoverInput,
				bridgeAccountFailoverInputExists,
				bridgeCurrentItems,
				bridgeCurrentItemsExist,
				needsBridgeReplay,
			)
			if needsBridgeReplay && turnReplayInputExists {
				updatedPayload, setInputErr := setOpenAIWSPayloadInputSequence(
					currentBridgePayload.payloadRaw,
					turnReplayInput,
					true,
				)
				if setInputErr != nil {
					return fmt.Errorf("set websocket http bridge replay input: %w", setInputErr)
				}
				bridgePayloadRaw = RemovePreviousResponseIDFromBody(updatedPayload)
				bridgePayloadBytes = len(bridgePayloadRaw)
				logOpenAIWSModeInfo(
					"ingress_ws_http_bridge_replay_input account_id=%d turn=%d input_items=%d previous_response_id_present=%v has_tool_output=%v",
					account.ID,
					turn,
					len(turnReplayInput),
					currentBridgePayload.previousResponseID != "",
					openAIWSRawPayloadHasToolCallOutput(currentBridgePayload.payloadRaw),
				)
			}
			var currentSIWCTools json.RawMessage
			if account.IsSIWCEnabled() {
				var present bool
				currentSIWCTools, present = openAIWSHTTPBridgeRawField(bridgePayloadRaw, "tools")
				if !present && needsBridgeReplay && len(siwcTools) > 0 {
					currentSIWCTools = siwcTools
					bridgePayloadRaw, err = sjson.SetRawBytes(bridgePayloadRaw, "tools", currentSIWCTools)
					if err != nil {
						return err
					}
					bridgePayloadBytes = len(bridgePayloadRaw)
				}
			}
			if accountRefreshErr != nil {
				return s.openAIWSAccountRefreshRetry(account, accountRefreshErr, currentBridgePayload.accountIdentitySourceRaw, turnAccountFailoverInput, turnHistoryComplete && turnAccountFailoverInputExists, currentBridgePayload.originalModel)
			}
			grokCacheIdentity := ""
			if account.Platform == PlatformGrok {
				grokCacheIdentity, err = resolveGrokWSCacheIdentity(
					c,
					account,
					grokCacheSeedPayload,
					currentBridgePayload.payloadRaw,
					currentBridgePayload.originalModel,
				)
				if err != nil {
					return fmt.Errorf("resolve Grok websocket cache identity: %w", err)
				}
			}
			if !downstream.alive() {
				if hooks != nil && hooks.AfterTurn != nil {
					hooks.AfterTurn(turn, nil, nil)
				}
				return nil
			}
			bridgeDrainCtx, finishBridgeDrain := downstream.turnContext(ctx)
			_, bridgeReplaySafe, _ := buildOpenAIWSCurrentTurnRetryPayload(currentBridgePayload.accountIdentitySourceRaw, turnAccountFailoverInput, turnHistoryComplete && turnAccountFailoverInputExists, currentBridgePayload.originalModel, true)
			bridgeDrainCtx = context.WithValue(bridgeDrainCtx, openAIWSBridgeReplayKey{}, bridgeReplaySafe)
			bridgeDrainCtx = context.WithValue(bridgeDrainCtx, openAIWSSIWCBridgeOwnerKey{}, bridgeOwner)
			if siwcSession != nil {
				bridgeDrainCtx = context.WithValue(bridgeDrainCtx, openAIWSSIWCSessionKey{}, siwcSession)
			}
			if currentBridgePayload.requestedReasoningEffort != nil {
				bridgeDrainCtx = WithRequestedReasoningEffort(bridgeDrainCtx, *currentBridgePayload.requestedReasoningEffort)
			}
			bridgeCtx, cancelBridge := context.WithCancelCause(bridgeDrainCtx)
			var bridgeResponseID atomic.Pointer[string]
			var bridgeTerminalSent atomic.Bool
			bridgeResult := struct {
				result *OpenAIForwardResult
				err    error
			}{}
			bridgeDone := make(chan struct{})
			go func() {
				defer close(bridgeDone)
				bridgeResult.result, bridgeResult.err = s.proxyOpenAIWSHTTPBridgeTurn(
					bridgeCtx,
					c,
					account,
					token,
					bridgePayloadRaw,
					bridgePayloadBytes,
					currentBridgePayload.originalModel,
					currentBridgePayload.imageBillingModel,
					currentBridgePayload.imageSizeTier,
					currentBridgePayload.imageInputSize,
					grokCacheIdentity,
					turn,
					func(event []byte) error {
						if id := gjson.GetBytes(event, "response.id").String(); id != "" {
							bridgeResponseID.Store(&id)
						}
						err := writeClientMessage(event)
						kind := gjson.GetBytes(event, "type").String()
						if err == nil && (isOpenAIWSTerminalEvent(kind) || kind == "error") {
							bridgeTerminalSent.Store(true)
						}
						return err
					},
				)
			}()
			bridgeCancelled := false
			bridgeInterrupted := false
			var bridgeCancelEventID string
		bridgeWait:
			for {
				select {
				case <-bridgeDone:
					break bridgeWait
				case control := <-downstream.controls:
					id := gjson.GetBytes(control.payload, "response_id").String()
					current := bridgeResponseID.Load()
					lane := gjson.GetBytes(control.payload, "stream_id").String()
					if !bridgeTerminalSent.Load() && control.turn == downstream.turn.Load() && (id == "" || current != nil && *current == id) && (lane == "" || lane == gjson.GetBytes(currentBridgePayload.payloadRaw, "stream_id").String()) {
						bridgeCancelled = true
						bridgeInterrupted = control.interrupt
						bridgeCancelEventID = gjson.GetBytes(control.payload, "event_id").String()
						cancelBridge(errOpenAIWSTurnCancelled)
					} else if !control.interrupt {
						_ = downstream.writeControlError(ctx, control.payload, "response does not belong to the active turn")
					}
				}
			}
			cancelBridge(context.Canceled)
			finishBridgeDrain()
			result, bridgeErr := bridgeResult.result, bridgeResult.err
			// Completion and cancellation can race. Never send a second terminal
			// after the provider's terminal event has already reached the client.
			bridgeCancelled = bridgeCancelled && !bridgeTerminalSent.Load()
			if bridgeCancelled {
				id := ""
				if v := bridgeResponseID.Load(); v != nil {
					id = *v
				}
				event, _ := json.Marshal(map[string]any{"type": "response.cancelled", "response": map[string]any{"id": id, "status": "cancelled"}})
				if id == "" {
					event, _ = json.Marshal(map[string]any{"type": "error", "event_id": bridgeCancelEventID, "error": map[string]string{"code": "request_cancelled", "type": "invalid_request_error", "message": "request cancelled before a response was identified"}})
					downstream.active.Store(false)
				}
				if id != "" || !bridgeInterrupted {
					_ = writeClientMessage(event)
				} else {
					downstream.terminalTurn.Store(downstream.turn.Load())
				}
				if result == nil {
					result = &OpenAIForwardResult{Model: currentBridgePayload.originalModel, OpenAIWSMode: true}
				}
				result.UpstreamTerminalEvent = "response.cancelled"
				if account.IsSIWCEnabled() {
					result.wsReplayIncomplete = true
				}
				bridgeErr = nil
			} else if !downstream.alive() {
				if result != nil {
					result.ClientDisconnect = true
				}
				if siwcSession != nil {
					siwcSession.finish(result, context.Canceled)
				}
				if hooks != nil && hooks.AfterTurn != nil {
					if result != nil && result.wsLocalWarmup {
						hooks.AfterTurn(turn, nil, nil)
					} else {
						hooks.AfterTurn(turn, result, nil)
					}
				}
				return nil
			}

			if siwcSession != nil {
				siwcSession.finish(result, bridgeErr)
			}

			if bridgeErr != nil && isOpenAIWSSessionPreempted(ctx) {
				if result != nil && (openAIUsageHasTokens(&result.Usage) || result.ImageCount > 0) && hooks != nil && hooks.AfterTurn != nil {
					hooks.AfterTurn(turn, result, errOpenAIWSSessionPreempted)
				}
				return errOpenAIWSSessionPreempted
			}
			if hooks != nil && hooks.AfterTurn != nil {
				if result != nil && result.wsLocalWarmup {
					hooks.AfterTurn(turn, nil, bridgeErr)
				} else {
					hooks.AfterTurn(turn, result, bridgeErr)
				}
			}
			var rejected *openAIWSTurnRejectedError
			turnRejected := errors.As(bridgeErr, &rejected) || (result != nil && result.UpstreamTerminalEvent == "response.failed")
			if bridgeErr != nil && !turnRejected {
				var failoverErr *UpstreamFailoverError
				if turn > 1 && errors.As(bridgeErr, &failoverErr) && failoverErr != nil {
					retryPayload, retrySafe, retryPayloadErr := buildOpenAIWSCurrentTurnRetryPayload(
						currentBridgePayload.accountIdentitySourceRaw,
						turnAccountFailoverInput,
						turnAccountFailoverInputExists,
						currentBridgePayload.originalModel,
						true,
					)
					if !turnHistoryComplete {
						retrySafe = false
					}
					if retryPayloadErr != nil {
						return fmt.Errorf("build websocket current-turn failover payload: %w", retryPayloadErr)
					}
					if !retrySafe {
						retryPayload = nil
					}
					return newOpenAIWSCurrentTurnFailoverError(bridgeErr, retryPayload)
				}
				return bridgeErr
			}
			if !turnRejected {
				if result == nil {
					return errors.New("websocket http bridge turn result is nil")
				}
				// turnReplayInput/turnAccountFailoverInput 可能共享同一头数组（转移自
				// bridgeCurrentItems），保存历史必须经 combine 新建头，禁止就地 append。
				if !bridgeCancelled || bridgeResponseID.Load() != nil {
					bridgeHistoryComplete = turnHistoryComplete && !result.wsReplayIncomplete
					siwcTools = append(json.RawMessage(nil), currentSIWCTools...)
					bridgeLastResponseID = ""
					if id := bridgeResponseID.Load(); id != nil {
						bridgeLastResponseID = *id // response lineage is not the billing/header request ID
					}
					bridgeReplayInput = turnReplayInput
					bridgeReplayInputExists = turnReplayInputExists
					if result.wsReplayInputExists {
						bridgeReplayInput = combineOpenAIWSReplayItems(bridgeReplayInput, result.wsReplayInput)
						bridgeReplayInputExists = true
					}
					bridgeAccountFailoverInput = turnAccountFailoverInput
					bridgeAccountFailoverInputExists = turnAccountFailoverInputExists
					if len(result.wsAccountFailoverReplayInput) > 0 {
						bridgeAccountFailoverInput = combineOpenAIWSReplayItems(
							bridgeAccountFailoverInput,
							result.wsAccountFailoverReplayInput,
						)
						bridgeAccountFailoverInputExists = true
					}
				}

				if bridgeTurnState := strings.TrimSpace(result.ResponseHeaders.Get(openAIWSTurnStateHeader)); bridgeTurnState != "" {
					// Follow-up turns on this bridge retain their own upstream state;
					// publishing it by session hash would leak it to independent bridges.
					turnState = bridgeTurnState
				}
				responseID := ""
				if id := bridgeResponseID.Load(); id != nil {
					responseID = *id
				}
				s.bindHTTPResponseAccount(ctx, c, account, responseID)
				if responseID != "" && stateStore != nil {
					ttl := s.openAIWSResponseStickyTTL()
					logOpenAIWSBindResponseAccountWarn(groupID, account.ID, responseID, stateStore.BindResponseAccount(ctx, groupID, responseID, account.ID, ttl))
				}
			}
			downstream.active.Store(false)
			nextClientMessage, readErr := readClientMessage()
			if readErr != nil {
				if isOpenAIWSSessionPreempted(ctx) {
					return errOpenAIWSSessionPreempted
				}
				if isOpenAIWSClientDisconnectError(readErr) {
					closeStatus, closeReason := summarizeOpenAIWSReadCloseError(readErr)
					logOpenAIWSModeInfo(
						"ingress_ws_http_bridge_client_closed account_id=%d close_status=%s close_reason=%s",
						account.ID,
						closeStatus,
						truncateOpenAIWSLogValue(closeReason, openAIWSHeaderValueMaxLen),
					)
					return nil
				}
				return fmt.Errorf("read client websocket request: %w", readErr)
			}
			nextPayload, parseErr := parseNextValidPayload(turn+1, nextClientMessage)
			if parseErr != nil {
				return parseErr
			}
			currentBridgePayload = nextPayload
			if nextPayload.previousResponseID == "" {
				bridgeHistoryComplete = true
			}
		}
	}

	firstRoutingFields := gjson.GetManyBytes(firstPayload.payloadRaw, "model", "service_tier")
	wsHeaders, _, buildHdrErr := s.buildOpenAIWSHeaders(
		ctx,
		c,
		account,
		token,
		wsDecision,
		isCodexCLI,
		turnState,
		strings.TrimSpace(c.GetHeader(openAIWSTurnMetadataHeader)),
		firstPayload.promptCacheKey,
		firstRoutingFields[0].String(),
		firstRoutingFields[1].String(),
		firstPayload.payloadRaw,
	)
	if buildHdrErr != nil {
		return fmt.Errorf("build ws headers: %w", buildHdrErr)
	}
	if next, _, metadataErr := normalizeCodexRequestMetadata(firstPayload.payloadRaw, wsHeaders, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c), false); metadataErr != nil {
		return metadataErr
	} else {
		firstPayload.payloadRaw = next
	}
	projectCodexOutboundHeaders(wsHeaders, codexAccountIdentitySource(c, account), true)

	baseAcquireReq := openAIWSAcquireRequest{
		TenantScope: openAIWSTenantScope(c),
		Account:     account,
		WSURL:       wsURL,
		Headers:     wsHeaders,
		HeadersFactory: func(factoryCtx context.Context, headers http.Header) (http.Header, error) {
			return s.refreshOpenAIAgentIdentityHeaders(factoryCtx, account, headers)
		},
		ProxyURL: func() string {
			if account.ProxyID != nil && account.Proxy != nil {
				return account.Proxy.URL()
			}
			return ""
		}(),
		ForceNewConn: false,
	}
	pool := s.getOpenAIWSConnPool()
	if pool == nil {
		return errors.New("openai ws conn pool is nil")
	}

	logOpenAIWSModeInfo(
		"ingress_ws_protocol_confirm account_id=%d account_type=%s transport=%s ws_host=%s ws_path=%s ws_mode=%s store_disabled=%v has_session_hash=%v has_previous_response_id=%v",
		account.ID,
		account.Type,
		normalizeOpenAIWSLogValue(string(wsDecision.Transport)),
		wsHost,
		wsPath,
		normalizeOpenAIWSLogValue(ingressMode),
		storeDisabled,
		sessionHash != "",
		firstPayload.previousResponseID != "",
	)

	if debugEnabled {
		logOpenAIWSModeDebug(
			"ingress_ws_start account_id=%d account_type=%s transport=%s ws_host=%s preferred_conn_id=%s has_session_hash=%v has_previous_response_id=%v store_disabled=%v",
			account.ID,
			account.Type,
			normalizeOpenAIWSLogValue(string(wsDecision.Transport)),
			wsHost,
			truncateOpenAIWSLogValue(preferredConnID, openAIWSIDValueMaxLen),
			sessionHash != "",
			firstPayload.previousResponseID != "",
			storeDisabled,
		)
	}
	if firstPayload.previousResponseID != "" {
		firstPreviousResponseIDKind := ClassifyOpenAIPreviousResponseIDKind(firstPayload.previousResponseID)
		logOpenAIWSModeInfo(
			"ingress_ws_continuation_probe account_id=%d turn=%d previous_response_id=%s previous_response_id_kind=%s preferred_conn_id=%s session_hash=%s header_session_id=%s header_conversation_id=%s has_turn_state=%v turn_state_len=%d has_prompt_cache_key=%v store_disabled=%v",
			account.ID,
			1,
			truncateOpenAIWSLogValue(firstPayload.previousResponseID, openAIWSIDValueMaxLen),
			normalizeOpenAIWSLogValue(firstPreviousResponseIDKind),
			truncateOpenAIWSLogValue(preferredConnID, openAIWSIDValueMaxLen),
			truncateOpenAIWSLogValue(sessionHash, 12),
			openAIWSHeaderValueForLog(baseAcquireReq.Headers, "session_id"),
			openAIWSHeaderValueForLog(baseAcquireReq.Headers, "conversation_id"),
			turnState != "",
			len(turnState),
			firstPayload.promptCacheKey != "",
			storeDisabled,
		)
	}

	acquireTimeout := s.openAIWSAcquireTimeout()
	if acquireTimeout <= 0 {
		acquireTimeout = 30 * time.Second
	}

	currentPayload := firstPayload.payloadRaw
	currentPayloadBytes := len(currentPayload)
	agentTaskRecoveryTried := false
	var timing *openAIForwardTiming
	var pendingPrewarm time.Duration
	defer func() { timing.Finish(returnErr) }()
	var acquireTurnLease func(int, string, bool, bool) (*openAIWSConnLease, error)
	acquireTurnLease = func(turn int, preferred string, forcePreferredConn bool, forceNewConn bool) (*openAIWSConnLease, error) {
		if turn > 1 || forceNewConn {
			fields := gjson.GetManyBytes(currentPayload, "prompt_cache_key", "model", "service_tier")
			// Reconnect from this turn's already-scoped body, never the first
			// handshake's raw metadata. Passing it as a raw header would scope twice.
			headers, _, err := s.buildOpenAIWSHeaders(ctx, c, account, token, wsDecision, isCodexCLI,
				turnState, "", fields[0].String(), fields[1].String(), fields[2].String(), currentPayload)
			if err != nil {
				return nil, fmt.Errorf("build ws reconnect headers: %w", err)
			}
			next, _, err := normalizeCodexRequestMetadata(currentPayload, headers, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c), false)
			if err != nil {
				return nil, err
			}
			currentPayload = next
			currentPayloadBytes = len(next)
			projectCodexOutboundHeaders(headers, codexAccountIdentitySource(c, account), true)
			baseAcquireReq.Headers = headers
		}
		req := cloneOpenAIWSAcquireRequest(baseAcquireReq)
		req.PreferredConnID = strings.TrimSpace(preferred)
		req.ForcePreferredConn = forcePreferredConn
		// dedicated 模式下每次获取均新建连接，避免跨会话复用残留上下文；
		// 上游读写失败后的重试同样新建，避免再拿到同批陈旧的空闲连接。
		req.ForceNewConn = dedicatedMode || forceNewConn
		waitCtx, stopBudget := OpenAITurnWaitContext(ctx)
		acquireCtx, acquireCancel := context.WithTimeout(waitCtx, acquireTimeout)
		if err := validateVendorNativeDispatch(ctx, s.accountRepo, account, vendorDispatchGroup(c)); err != nil {
			acquireCancel()
			stopBudget()
			return nil, err
		}
		if gjson.GetBytes(currentPayload, "generate").Type != gjson.False {
			timing = newOpenAITransportTiming(c, account, "websocket", int64(len(currentPayload)))
			timing.SetTransportReason("ctx_pool_ingress")
		}
		lease, acquireErr := pool.Acquire(acquireCtx, req)
		if acquireErr != nil {
			timing.Finish(acquireErr)
		}
		if errors.Is(context.Cause(waitCtx), ErrOpenAIWSFirstOutputBudgetExhausted) {
			if lease != nil {
				lease.Release()
			}
			acquireCancel()
			stopBudget()
			return nil, ErrOpenAIWSFirstOutputBudgetExhausted
		}
		acquireCancel()
		stopBudget()
		var dialErr *openAIWSDialError
		if errors.As(acquireErr, &dialErr) && dialErr.StatusCode == http.StatusUnauthorized {
			if s.openAITokenProvider != nil {
				if fresh, ok := refreshRejectedCredential(ctx, s.accountRepo, account, token, s.openAITokenProvider.refreshAPI, s.openAITokenProvider.executor); ok {
					token = fresh
					baseAcquireReq.Headers = baseAcquireReq.Headers.Clone()
					baseAcquireReq.Headers.Set("Authorization", "Bearer "+fresh)
					return acquireTurnLease(turn, preferred, forcePreferredConn, true)
				}
			}
			return nil, &UpstreamFailoverError{StatusCode: http.StatusUnauthorized, Stage: GatewayFailureStageAccountAuth, Scope: GatewayFailureScopeAccount, Reason: "upstream_credentials_rejected", Cause: acquireErr, RequestNotSent: true, RejectedBeforeGeneration: true}
		}
		if acquireErr != nil && s.isAgentIdentityAccount(ctx, account) && errors.As(acquireErr, &dialErr) && isAgentIdentityTaskInvalidWSDialError(dialErr) && !agentTaskRecoveryTried {
			agentTaskRecoveryTried = true
			if recoveryErr := s.recoverAgentIdentityTask(ctx, account, account.GetCredential("task_id")); recoveryErr != nil {
				return nil, fmt.Errorf("agent identity task recovery failed: %w", recoveryErr)
			}
			return acquireTurnLease(turn, preferred, forcePreferredConn, forceNewConn)
		}
		if acquireErr != nil {
			if isOpenAIWSSessionPreempted(ctx) {
				return nil, errOpenAIWSSessionPreempted
			}
			canonicalModel := canonicalOpenAIAccountSchedulingModel(account, ingressSessionOriginalModel)
			s.handleOpenAIWSDialTransientFailure(ctx, account, canonicalModel, acquireErr)
			dialStatus, dialClass, dialCloseStatus, dialCloseReason, dialRespServer, dialRespVia, dialRespCFRay, dialRespReqID := summarizeOpenAIWSDialError(acquireErr)
			logOpenAIWSModeInfo(
				"ingress_ws_upstream_acquire_fail account_id=%d turn=%d reason=%s dial_status=%d dial_class=%s dial_close_status=%s dial_close_reason=%s dial_resp_server=%s dial_resp_via=%s dial_resp_cf_ray=%s dial_resp_x_request_id=%s cause=%s preferred_conn_id=%s force_preferred_conn=%v ws_host=%s ws_path=%s proxy_enabled=%v",
				account.ID,
				turn,
				normalizeOpenAIWSLogValue(classifyOpenAIWSAcquireError(acquireErr)),
				dialStatus,
				dialClass,
				dialCloseStatus,
				truncateOpenAIWSLogValue(dialCloseReason, openAIWSHeaderValueMaxLen),
				dialRespServer,
				dialRespVia,
				dialRespCFRay,
				dialRespReqID,
				truncateOpenAIWSLogValue(acquireErr.Error(), openAIWSLogValueMaxLen),
				truncateOpenAIWSLogValue(preferred, openAIWSIDValueMaxLen),
				forcePreferredConn,
				wsHost,
				wsPath,
				account.ProxyID != nil && account.Proxy != nil,
			)
			var dialErr *openAIWSDialError
			if errors.As(acquireErr, &dialErr) && dialErr != nil && dialErr.StatusCode == http.StatusTooManyRequests {
				s.persistOpenAIWSRateLimitSignal(ctx, account, dialErr.ResponseHeaders, nil, "rate_limit_exceeded", "rate_limit_error", strings.TrimSpace(acquireErr.Error()), canonicalModel)
				return nil, s.newOpenAIWSRateLimitFailoverError(account, dialErr.ResponseHeaders, nil, acquireErr.Error())
			}
			if errors.Is(acquireErr, errOpenAIWSPreferredConnUnavailable) {
				return nil, NewOpenAIWSClientCloseError(
					coderws.StatusPolicyViolation,
					"upstream continuation connection is unavailable; please restart the conversation",
					acquireErr,
				)
			}
			if errors.Is(acquireErr, context.DeadlineExceeded) || errors.Is(acquireErr, errOpenAIWSConnQueueFull) {
				return nil, NewOpenAIWSClientCloseError(
					coderws.StatusTryAgainLater,
					"upstream websocket is busy, please retry later",
					acquireErr,
				)
			}
			return nil, acquireErr
		}
		timing.ConnectionReady(lease.Reused(), lease.ConnPickDuration(), lease.QueueWaitDuration(), lease.AgeBefore(), lease.IdleBefore())
		connID := strings.TrimSpace(lease.ConnID())
		if handshakeTurnState := strings.TrimSpace(lease.HandshakeHeader(openAIWSTurnStateHeader)); handshakeTurnState != "" {
			s.noteOpenAICodexTurnStateProvenance(c, account, handshakeTurnState)
			turnState = handshakeTurnState
			if stateStore != nil && sessionHash != "" {
				stateStore.BindSessionTurnState(groupID, sessionHash, handshakeTurnState, s.openAIWSSessionStickyTTL())
			}
			updatedHeaders := cloneHeader(baseAcquireReq.Headers)
			if updatedHeaders == nil {
				updatedHeaders = make(http.Header)
			}
			updatedHeaders.Set(openAIWSTurnStateHeader, handshakeTurnState)
			baseAcquireReq.Headers = updatedHeaders
		}
		logOpenAIWSModeInfo(
			"ingress_ws_upstream_connected account_id=%d turn=%d conn_id=%s conn_reused=%v conn_idle_ms=%d conn_age_ms=%d upstream_pings=%d preferred_conn_id=%s",
			account.ID,
			turn,
			truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
			lease.Reused(),
			lease.IdleBefore().Milliseconds(),
			lease.AgeBefore().Milliseconds(),
			lease.UpstreamPingCount(),
			truncateOpenAIWSLogValue(preferred, openAIWSIDValueMaxLen),
		)
		return lease, nil
	}

	var rejectedFieldRetryState *openAIResponsesRejectedFieldRetryState
	var currentTurnReplayInput []json.RawMessage
	var currentTurnReplayInputExists, currentTurnReplayComplete bool
	turnRetry := 0
	turnStartedAt := downstream.budget.Load().started
	if hooks != nil && !hooks.InitialTurnStartedAt.IsZero() {
		turnStartedAt = hooks.InitialTurnStartedAt
	}
	sendAndRelay := func(turn int, lease *openAIWSConnLease, payload []byte, payloadBytes int, originalModel string, imageBillingModel string, imageSizeTier string, imageInputSize string, requestedReasoningEffort *string) (out *OpenAIForwardResult, returnErr error) {
		ctx, finishDrain := downstream.turnContext(ctx)
		defer finishDrain()
		ctx = WithSchedulingQuotaRequest(ctx, payload, ContentModerationProtocolOpenAIResponses)
		defer func() {
			if returnErr == nil && out != nil {
				confirmSchedulingQuotaPrefix(ctx, account, out.Usage.CacheReadInputTokens)
			}
		}()
		responseModelObserver := newUpstreamResponseModelObserver(c)
		if lease == nil {
			return nil, errors.New("upstream websocket lease is nil")
		}
		turnStart := time.Now()
		attemptTiming := timing
		if gjson.GetBytes(payload, "generate").Type == gjson.False {
			prewarmTiming := openAIForwardTimingFromContext(c)
			defer func() {
				elapsed := time.Since(turnStart)
				if prewarmTiming != nil {
					prewarmTiming.MarkPrewarm(elapsed)
				} else {
					pendingPrewarm += elapsed
				}
			}()
		}
		var timingErr error
		defer func() {
			if out != nil {
				// WS delivers events even when the compatibility payload carries
				// stream=false. Keep that legacy flag without suppressing observations.
				out.LatencyMetrics = attemptTiming.UsageLatencyMetrics(out.Stream || out.OpenAIWSMode)
			}
			if returnErr != nil {
				timingErr = returnErr
			}
			if timingErr == nil {
				timingErr = ctx.Err()
			}
			if returnErr == nil && out != nil && out.UpstreamTerminalEvent != "" {
				attemptTiming.FinishTurn(timingErr)
			} else {
				attemptTiming.Finish(timingErr)
			}
		}()
		wroteDownstream := false
		clientDisconnected := false
		progress := beginOpenAIResponseProgress(c, ingressMode, "websocket", nil)
		progress.countsKnown = true
		var responseCreatedAt time.Time
		replayBlocker := openAIWSUncertainExecutionReplayBlocker(payload)
		if currentTurnReplayComplete {
			replayBlocker = openAIWSUncertainExecutionReplayBlocker(payload, currentTurnReplayInput)
		}
		c.Set(openAIReplayBlockerContextKey, replayBlocker)
		retryDetail := func() string {
			data := progress.details()
			data["blocking_tool_kind"] = replayBlocker
			data["history_complete"] = currentTurnReplayComplete
			encoded, _ := json.Marshal(data)
			return string(encoded)
		}
		defer func() {
			var turnErr *openAIWSIngressTurnError
			if errors.As(returnErr, &turnErr) {
				if IsOpenAIWSUpstreamTransportError(returnErr) {
					progress.upstreamCloseCode = int(coderws.CloseStatus(returnErr))
				}
				turnErr.retryDetail = retryDetail()
			}
		}()
		observeWrite := func(err error) error {
			if err != nil && isOpenAIWSClientDisconnectError(err) {
				clientDisconnected = true
				_ = downstream.CloseNow()
				return nil
			}
			return err
		}
		writeTurnMessage := func(message []byte) error {
			if clientDisconnected {
				return nil
			}
			writeErr := writeClientMessage(message)
			if writeErr != nil {
				timingErr = writeErr
			}
			err := observeWrite(writeErr)
			if err == nil && !clientDisconnected {
				attemptTiming.MarkDownstream(message)
				progress.observeDownstream(message)
				if gjson.GetBytes(message, "type").String() == "codex.response.metadata" {
					if state := gjson.GetBytes(message, "headers.x-codex-turn-state").String(); state != "" {
						s.noteOpenAICodexTurnStateProvenance(c, account, state)
					}
				}
			}

			return err
		}
		var bareErrorDeadline time.Time
		var bareErrorPayload []byte
		contextRejected := false
		var retryStopReason OpenAIRetryStopReason
		var retryStopDetail string
		// ChatGPT upstreams open every turn with codex.rate_limits and
		// codex.response.metadata before any response.* event, including turns
		// that then fail with a request rejection. Forwarding them straight away
		// set wroteDownstream and silently disabled every pre-output recovery
		// below (rejected-field retry, rate-limit failover, previous-response
		// recovery). Hold them until the first real event instead: they are
		// flushed in order ahead of it, and dropped with the attempt when the
		// turn is retried. Dropping matters, not just order — Codex stores the
		// metadata's x-codex-turn-state in a OnceLock, so a duplicate from a
		// discarded attempt would pin the rest of the turn to the routing that
		// just rejected it.
		var heldTurnMetadata [][]byte
		heldTurnMetadataBytes := 0
		var heldUntil time.Time
		flushHeld := func(reason string) error {
			if len(heldTurnMetadata) > 0 && progress.commitReason == "" {
				progress.commitReason = reason
			}
			for _, event := range heldTurnMetadata {
				// A failed write can have delivered a prefix. Never retry it.
				wroteDownstream = true
				if err := writeTurnMessage(event); err != nil {
					return err
				}
			}
			heldTurnMetadata = nil
			heldTurnMetadataBytes = 0
			heldUntil = time.Time{}
			return nil
		}
		firstOutputTimeout := s.openAIFirstOutputTimeout(gjson.GetBytes(payload, "reasoning.effort").String())
		if firstOutputTimeout <= 0 {
			firstOutputTimeout = s.openAIWSReadTimeout()
		}
		firstOutputDeadline := openAITurnFirstOutputDeadline(ctx, firstOutputTimeout, turnStartedAt)
		if !time.Now().Before(firstOutputDeadline) {
			return nil, wrapOpenAIWSIngressTurnError("before_upstream_write", ErrOpenAIWSFirstOutputBudgetExhausted, false)
		}
		// A cancel received while selecting/acquiring the account must not be
		// followed by a new generation. Its turn was stamped by the sole reader.
		for len(downstream.controls) > 0 {
			control := <-downstream.controls
			id := gjson.GetBytes(control.payload, "response_id").String()
			lane := gjson.GetBytes(control.payload, "stream_id").String()
			if control.turn == downstream.turn.Load() && (id == "" || downstream.responseIDs[id]) && (lane == "" || lane == gjson.GetBytes(payload, "stream_id").String()) {
				event := map[string]any{"type": "error", "event_id": gjson.GetBytes(control.payload, "event_id").String(), "error": map[string]string{"type": "invalid_request_error", "code": "request_cancelled", "message": "request cancelled before upstream send"}}
				message, _ := json.Marshal(event)
				downstream.active.Store(false)
				if !control.interrupt {
					if err := writeTurnMessage(message); err != nil {
						return nil, wrapOpenAIWSIngressTurnError("write_client", err, true)
					}
				} else {
					downstream.terminalTurn.Store(downstream.turn.Load())
				}
				return &OpenAIForwardResult{Model: originalModel, OpenAIWSMode: true, UpstreamTerminalEvent: "response.cancelled"}, nil
			}
			if control.interrupt {
				continue
			}
			if err := downstream.writeControlError(ctx, control.payload, "response does not belong to the active turn"); err != nil {
				return nil, wrapOpenAIWSIngressTurnError("write_client", err, true)
			}
		}
		if !downstream.alive() {
			return nil, context.Canceled
		}
		if err := validateVendorNativeDispatch(ctx, s.accountRepo, account, vendorDispatchGroup(c)); err != nil {
			return nil, err
		}
		attemptTiming.SetRequestMetadata(payload)
		logGatewayCacheTransform(ctx, account, "", payload)
		if s.rateLimitService.IsRequestBlockedBySchedulingQuota(ctx, account) {
			return nil, &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests, Stage: "selection", Scope: GatewayFailureScopeRequest, Reason: SchedulingQuotaRequestTooLarge, RequestNotSent: true, ClientMessage: "Large uncached input is blocked near the account quota limit"}
		}
		if err := lease.WriteJSONWithContextTimeout(ctx, json.RawMessage(payload), s.openAIWSWriteTimeout()); err != nil {
			return nil, wrapOpenAIWSIngressTurnError(
				"write_upstream",
				fmt.Errorf("write upstream websocket request: %w", err),
				false,
			)
		}
		attemptTiming.MarkRequestWritten(int64(len(payload)))
		if debugEnabled {
			logOpenAIWSModeDebug(
				"ingress_ws_turn_request_sent account_id=%d turn=%d conn_id=%s payload_bytes=%d",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(lease.ConnID(), openAIWSIDValueMaxLen),
				payloadBytes,
			)
		}

		responseID := ""
		var createdAtTracker responsesCreatedAtStamper
		var cancelDeadline time.Time
		cancelEventID := ""
		interrupted := false
		if downstream.responseIDs == nil {
			downstream.responseIDs = make(map[string]bool)
		}
		attemptResponseIDs := downstream.responseIDs
		usage := OpenAIUsage{}
		imageCounter := newOpenAIImageOutputCounter()
		var firstTokenMs *int
		semanticSeen := false
		reqStream := openAIWSPayloadBoolFromRaw(payload, "stream", true)
		turnPreviousResponseID := openAIWSPayloadStringFromRaw(payload, "previous_response_id")
		turnPreviousResponseIDKind := ClassifyOpenAIPreviousResponseIDKind(turnPreviousResponseID)
		turnPromptCacheKey := openAIWSPayloadStringFromRaw(payload, "prompt_cache_key")
		turnStoreDisabled := s.isOpenAIWSStoreDisabledInRequestRaw(payload, account)
		eventCount := 0
		tokenEventCount := 0
		terminalEventCount := 0
		replayCollector := &openAIWSToolCallReplayCollector{}
		firstEventType := ""
		lastEventType := ""
		needModelReplace := false
		mappedModel := ""
		var mappedModelBytes []byte
		if originalModel != "" {
			mappedModel = strings.TrimSpace(gjson.GetBytes(payload, "model").String())
			if mappedModel == "" {
				mappedModel = normalizeOpenAIModelForUpstream(account, account.GetMappedModel(originalModel))
			}
			needModelReplace = mappedModel != "" && mappedModel != originalModel
			if needModelReplace {
				mappedModelBytes = []byte(mappedModel)
			}
		}
		resultWithUsage := func(terminalEvent, incompleteReason string) *OpenAIForwardResult {
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
				ServiceTier:                   resolvedOpenAIUpstreamServiceTierFromObserver(responseModelObserver, extractOpenAIServiceTierFromBody(payload)),
				ReasoningEffort:               ApplyThinkingEnabledFallback(extractOpenAIReasoningEffortFromBody(payload, mappedModel, originalModel), payload, mappedModel),
				RequestedReasoningEffort:      requestedReasoningEffort,
				Stream:                        reqStream,
				OpenAIWSMode:                  true,
				ClientDisconnect:              clientDisconnected || !downstream.alive(),
				UpstreamTerminalEvent:         terminalEvent,
				wsContextRejected:             terminalEvent == "response.failed" && contextRejected,
				UpstreamIncompleteReason:      incompleteReason,
				ResponseHeaders:               lease.HandshakeHeaders(),
				Duration:                      time.Since(turnStart),
				FirstTokenMs:                  firstTokenMs,
			}
			if replayInput := replayCollector.AllItems(); len(replayInput) > 0 {
				result.wsReplayInput = replayInput
				result.wsReplayInputExists = true
			}
			result.wsReplayIncomplete = len(replayCollector.pending) > 0 || replayCollector.unidentifiedOutput
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
			if out == nil && returnErr != nil && (openAIUsageHasTokens(&usage) || imageCounter.Count() > 0) {
				out = resultWithUsage("", "")
			}
		}()
		// One bounded read remains pending while control/staging timers fire.
		// Cancelling a short socket read would destroy the upstream connection.
		readCtx, cancelRead := context.WithCancel(ctx)
		readRequests := make(chan struct{})
		readResults := make(chan openAIWSClientReadResult, 1)
		readerDone := make(chan struct{})
		go func() {
			defer close(readerDone)
			for {
				select {
				case <-readCtx.Done():
					return
				case <-readRequests:
				}
				message, err := lease.ReadMessageWithContextTimeout(readCtx, s.openAIWSReadTimeout())
				readResults <- openAIWSClientReadResult{payload: message, err: err}
				if err != nil {
					return
				}
			}
		}()
		defer func() { cancelRead(); <-readerDone }()
		nextEvent := func(timeout time.Duration) ([]byte, error) {
			select {
			case readRequests <- struct{}{}:
			case <-ctx.Done():
				return nil, context.Cause(ctx)
			}
			deadline := time.Now().Add(timeout)
			for {
				until := deadline
				if !cancelDeadline.IsZero() && cancelDeadline.Before(until) {
					until = cancelDeadline
				}
				if !heldUntil.IsZero() && heldUntil.Before(until) {
					until = heldUntil
				}
				remaining := time.Until(until)
				if remaining <= 0 {
					remaining = time.Nanosecond
				}
				timer := time.NewTimer(remaining)
				select {
				case event := <-readResults:
					timer.Stop()
					return event.payload, event.err
				case <-ctx.Done():
					timer.Stop()
					<-readerDone
					select {
					case event := <-readResults:
						if event.err == nil {
							return event.payload, nil
						}
					default:
					}
					return nil, context.Cause(ctx)
				case control := <-downstream.controls:
					timer.Stop()
					id := gjson.GetBytes(control.payload, "response_id").String()
					lane := gjson.GetBytes(control.payload, "stream_id").String()
					if control.turn != downstream.turn.Load() || (lane != "" && lane != gjson.GetBytes(payload, "stream_id").String()) || (id != "" && !attemptResponseIDs[id]) {
						if control.interrupt {
							continue
						}
						if err := observeWrite(downstream.writeControlError(ctx, control.payload, "response does not belong to the active turn")); err != nil {
							return nil, wrapOpenAIWSIngressTurnError("write_client", err, true)
						}
						continue
					}
					if !cancelDeadline.IsZero() {
						continue
					}
					cancelEventID = gjson.GetBytes(control.payload, "event_id").String()
					interrupted = control.interrupt
					if responseID == "" {
						return nil, errOpenAIWSTurnCancelled
					}
					cancelDeadline = time.Now().Add(openAIWSUsageDrainTimeout)
					cancelPayload := map[string]any{"type": "response.cancel", "response_id": responseID}
					if cancelEventID != "" {
						cancelPayload["event_id"] = cancelEventID
					}
					if err := lease.WriteJSONWithContextTimeout(ctx, cancelPayload, openAIWSUsageDrainTimeout); err != nil {
						return nil, errOpenAIWSTurnCancelled
					}
					continue
				case <-timer.C:
					if !cancelDeadline.IsZero() && !time.Now().Before(cancelDeadline) {
						return nil, errOpenAIWSTurnCancelled
					}
					if !time.Now().Before(deadline) {
						return nil, context.DeadlineExceeded
					}
					if err := flushHeld("staging_timeout"); err != nil {
						return nil, wrapOpenAIWSIngressTurnError("write_client", err, true)
					}
				}
			}
		}
		for {
			readTimeout := s.openAIWSReadTimeout()
			if !bareErrorDeadline.IsZero() {
				remaining := time.Until(bareErrorDeadline)
				if remaining <= 0 {
					remaining = time.Nanosecond
				}
				readTimeout = min(readTimeout, remaining)
			}
			if !semanticSeen && bareErrorDeadline.IsZero() {
				remaining := time.Until(firstOutputDeadline)
				if remaining <= 0 {
					lease.MarkBroken()
					if !downstream.alive() {
						return resultWithUsage("", ""), context.DeadlineExceeded
					}
					return nil, wrapOpenAIWSIngressTurnError("read_upstream", context.DeadlineExceeded, wroteDownstream)
				}
				readTimeout = min(readTimeout, remaining)
			}
			upstreamMessage, readErr := nextEvent(readTimeout)
			synthesizedRejection := false
			if readErr != nil {
				lease.MarkBroken()
				if !cancelDeadline.IsZero() {
					readErr = errOpenAIWSTurnCancelled
				}
				if errors.Is(readErr, errOpenAIWSTurnCancelled) {
					if responseID == "" {
						message, _ := json.Marshal(map[string]any{"type": "error", "event_id": cancelEventID, "error": map[string]string{"code": "request_cancelled", "type": "invalid_request_error", "message": "request cancelled before a response was identified"}})
						if !interrupted {
							_ = writeTurnMessage(message)
						} else {
							downstream.terminalTurn.Store(downstream.turn.Load())
						}
						downstream.active.Store(false)
						return &OpenAIForwardResult{Model: originalModel, OpenAIWSMode: true, UpstreamTerminalEvent: "response.cancelled"}, nil
					}
					bareErrorPayload = nil
					bareErrorDeadline = time.Time{}
					upstreamMessage, _ = json.Marshal(map[string]any{"type": "response.cancelled", "response": map[string]any{"id": responseID, "status": "cancelled", "model": originalModel}})
				} else if errors.Is(readErr, errOpenAIWSUsageDrainExpired) || !downstream.alive() {
					return resultWithUsage("", ""), readErr
				} else if openAIWSIngressTurnRetryReason(readErr) == "write_client" {
					return nil, readErr
				} else if len(bareErrorPayload) > 0 && ctx.Err() == nil {
					// Complete this turn on the client socket, but never reuse the
					// upstream: a late terminal must not leak into the next turn.
					upstreamMessage = buildOpenAIWSHTTPBridgeFailedEvent(responseID, originalModel,
						createdAtTracker.Value(), bareErrorPayload, extractOpenAISSEErrorMessage(bareErrorPayload))
					synthesizedRejection = true
				} else {
					return nil, wrapOpenAIWSIngressTurnError("read_upstream",
						fmt.Errorf("read upstream websocket event: %w", readErr), wroteDownstream)
				}
			}
			if normalized, changed := normalizeCompletedImageGenerationStatus(upstreamMessage); changed {
				upstreamMessage = normalized
			}

			eventType, eventResponseID, _ := parseOpenAIWSEventEnvelope(upstreamMessage)
			progress.observeUpstream(eventType)
			if previous := downstream.lastTerminalID.Load(); previous != nil && eventResponseID != "" && eventResponseID == *previous {
				continue
			}
			if readErr == nil {
				attemptTiming.ObserveEvent(upstreamMessage)
			} else {
				timingErr = readErr
			}
			createdAtTracker.Observe(upstreamMessage, eventType)
			if eventType == "error" && !cancelDeadline.IsZero() {
				code := gjson.GetBytes(upstreamMessage, "error.code").String()
				if code == "response_not_found" || code == "response_not_active" || code == "response_already_completed" || (cancelEventID != "" && gjson.GetBytes(upstreamMessage, "event_id").String() == cancelEventID) {
					cancelDeadline = time.Time{}
					if err := flushHeld("control_response"); err != nil {
						return nil, wrapOpenAIWSIngressTurnError("write_client", err, true)
					}
					if err := writeTurnMessage(upstreamMessage); err != nil {
						return nil, wrapOpenAIWSIngressTurnError("write_client", err, true)
					}
					continue
				}
			}
			responseModelObserver.ObserveOpenAI(upstreamMessage, eventType)
			if eventType == "response.created" && responseCreatedAt.IsZero() {
				responseCreatedAt = time.Now()
			}
			if responseID == "" && eventResponseID != "" {
				responseID = eventResponseID
				attemptResponseIDs[responseID] = true
			}
			if eventType != "" {
				eventCount++
				if firstEventType == "" {
					firstEventType = eventType
				}
				lastEventType = eventType
			}
			if openAIWSMessageShouldParseUsage(eventType, upstreamMessage) {
				parseOpenAIWSResponseUsageFromCompletedEvent(upstreamMessage, &usage)
			}
			if eventType == "error" || eventType == "response.failed" {
				contextRejected = isOpenAIEncryptedContextRejection(upstreamMessage)
				markOpenAICyberPolicyEvent(c, upstreamMessage, http.StatusOK, &usage)
			}
			if eventType == "error" || eventType == "response.failed" {
				if eventType == "response.failed" {
					s.handleOpenAIWSErrorEventTransientFailure(ctx, account, mappedModel, lease.HandshakeHeaders(), upstreamMessage)
				}
				errCodeRaw, errTypeRaw, errMsgRaw := parseOpenAIWSErrorEventFields(upstreamMessage)
				statusCode := openAIWSRejectedFieldRetryHTTPStatus(upstreamMessage)
				if !wroteDownstream && rejectedFieldRetryState != nil {
					retryBody, retryReason, changed, retryErr := normalizeOpenAIResponsesRejectedFieldRetryBody(
						statusCode,
						payload,
						upstreamMessage,
					)
					if retryErr != nil {
						return nil, fmt.Errorf("normalize websocket rejected field retry: %w", retryErr)
					}
					if !changed && retryReason != "" {
						// Persist only the safe item index/type reason, not request data.
						setOpsUpstreamError(c, statusCode, errMsgRaw, "input repair declined: "+retryReason)
					}
					if changed && rejectedFieldRetryState.Allow(retryBody) {
						logOpenAIWSModeInfo(
							"ingress_ws_rejected_field_retry account_id=%d turn=%d conn_id=%s reason=%s",
							account.ID,
							turn,
							truncateOpenAIWSLogValue(lease.ConnID(), openAIWSIDValueMaxLen),
							truncateOpenAIWSLogValue(retryReason, openAIWSLogValueMaxLen),
						)
						return nil, &openAIWSRejectedFieldRetryError{
							body:   append([]byte(nil), retryBody...),
							reason: retryReason,
						}
					}
				}
				s.persistOpenAIWSRateLimitSignal(ctx, account, lease.HandshakeHeaders(), upstreamMessage, errCodeRaw, errTypeRaw, errMsgRaw, mappedModel)
				fallbackReason, _ := classifyOpenAIWSErrorEventFromRaw(errCodeRaw, errTypeRaw, errMsgRaw)
				if fallbackReason == "ws_connection_limit_reached" && !wroteDownstream && turnRetry < 1 {
					replay, safe, replayErr := payload, true, error(nil)
					if turnStoreDisabled {
						replay, safe, replayErr = buildOpenAIWSCurrentTurnRetryPayload(payload,
							currentTurnReplayInput, currentTurnReplayComplete, mappedModel, true)
					}
					if replayErr != nil {
						return nil, replayErr
					}
					if safe {
						turnRetry++
						lease.MarkBroken()
						return nil, &openAIWSRejectedFieldRetryError{body: replay, reason: fallbackReason, reconnect: true}
					}
				}
				errCode, errType, errMessage := summarizeOpenAIWSErrorEventFieldsFromRaw(errCodeRaw, errTypeRaw, errMsgRaw)
				createdToErrorMs := int64(-1)
				if !responseCreatedAt.IsZero() {
					createdToErrorMs = time.Since(responseCreatedAt).Milliseconds()
				}
				replaySafe := replayBlocker == ""
				recoverablePrevNotFound := fallbackReason == openAIWSIngressStagePreviousResponseNotFound &&
					turnPreviousResponseID != "" &&
					currentTurnReplayComplete &&
					s.openAIWSIngressPreviousResponseRecoveryEnabled() &&
					!wroteDownstream
				if recoverablePrevNotFound {
					_, safe, err := buildOpenAIWSCurrentTurnRetryPayload(payload,
						currentTurnReplayInput, currentTurnReplayComplete, mappedModel, true)
					recoverablePrevNotFound = err == nil && safe
				}
				if recoverablePrevNotFound {
					// 可恢复场景使用非 error 关键字日志，避免被 LegacyPrintf 误判为 ERROR 级别。
					logOpenAIWSModeInfo(
						"ingress_ws_prev_response_recoverable account_id=%d turn=%d conn_id=%s idx=%d reason=%s code=%s type=%s message=%s previous_response_id=%s previous_response_id_kind=%s response_id=%s store_disabled=%v has_prompt_cache_key=%v",
						account.ID,
						turn,
						truncateOpenAIWSLogValue(lease.ConnID(), openAIWSIDValueMaxLen),
						eventCount,
						truncateOpenAIWSLogValue(fallbackReason, openAIWSLogValueMaxLen),
						errCode,
						errType,
						errMessage,
						truncateOpenAIWSLogValue(turnPreviousResponseID, openAIWSIDValueMaxLen),
						normalizeOpenAIWSLogValue(turnPreviousResponseIDKind),
						truncateOpenAIWSLogValue(responseID, openAIWSIDValueMaxLen),
						turnStoreDisabled,
						turnPromptCacheKey != "",
					)
				} else {
					logOpenAIWSModeInfo(
						"ingress_ws_error_event account_id=%d turn=%d conn_id=%s idx=%d fallback_reason=%s err_code=%s err_type=%s err_message=%s previous_response_id=%s previous_response_id_kind=%s response_id=%s store_disabled=%v has_prompt_cache_key=%v event_type=%s created_to_error_ms=%d downstream_committed=%v token_events=%d replay_safe=%v",
						account.ID,
						turn,
						truncateOpenAIWSLogValue(lease.ConnID(), openAIWSIDValueMaxLen),
						eventCount,
						truncateOpenAIWSLogValue(fallbackReason, openAIWSLogValueMaxLen),
						errCode,
						errType,
						errMessage,
						truncateOpenAIWSLogValue(turnPreviousResponseID, openAIWSIDValueMaxLen),
						normalizeOpenAIWSLogValue(turnPreviousResponseIDKind),
						truncateOpenAIWSLogValue(responseID, openAIWSIDValueMaxLen),
						turnStoreDisabled,
						turnPromptCacheKey != "",
						eventType,
						createdToErrorMs,
						wroteDownstream,
						tokenEventCount,
						replaySafe,
					)
				}
				// previous_response_not_found 在 ingress 模式支持单次恢复重试：
				// 不把该 error 直接下发客户端，而是由上层去掉 previous_response_id 后重放当前 turn。
				if recoverablePrevNotFound {
					lease.MarkBroken()
					errMsg := strings.TrimSpace(errMsgRaw)
					if errMsg == "" {
						errMsg = "previous response not found"
					}
					return nil, wrapOpenAIWSIngressTurnError(
						openAIWSIngressStagePreviousResponseNotFound,
						errors.New(errMsg),
						false,
					)
				}
				if !wroteDownstream && isOpenAIWSRateLimitError(errCodeRaw, errTypeRaw, errMsgRaw) {
					lease.MarkBroken()
					return nil, s.newOpenAIWSRateLimitFailoverError(account, lease.HandshakeHeaders(), upstreamMessage, errMsgRaw)
				}
				// Nothing has reached the client yet, so recover the turn instead
				// of ending it. Both the decision and the failover error are the
				// ones the HTTP stream applies to the same bare error frame, so a
				// turn recovers on WS exactly as it would over HTTP: the canonical
				// server_error and the response-protection outage move to another
				// account, capacity shed retries on the same one, and
				// request-scoped or policy errors still reach the client. Without
				// this Codex replays the turn on the same sticky account and hits
				// the same failure up to five more times.
				retryStopReason = ""
				retryStopDetail = retryDetail()
				shouldFailover := openAIStreamErrorEventShouldFailover(upstreamMessage, errMsgRaw)
				if shouldFailover {
					if wroteDownstream {
						retryStopReason = OpenAIRetryStopDownstreamCommitted
					} else if !currentTurnReplayComplete {
						retryStopReason = OpenAIRetryStopHistoryIncomplete
					} else if !replaySafe {
						retryStopReason = OpenAIRetryStopReplayUnsafe
					}
				}
				if !wroteDownstream && replaySafe && shouldFailover {
					lease.MarkBroken()
					headers := lease.HandshakeHeaders()
					return nil, s.newOpenAIStreamFailoverErrorWithModel(
						c,
						account,
						false,
						strings.TrimSpace(headers.Get("x-request-id")),
						upstreamMessage,
						errMsgRaw,
						mappedModel,
						headers,
					)
				}
			}
			if eventType != "error" && openAIStreamDataStartsClientOutput(string(upstreamMessage), eventType) {
				semanticSeen = true
			}
			isTokenEvent := isOpenAIWSTokenEvent(eventType)
			if isTokenEvent {
				tokenEventCount++
			}
			isTerminalEvent := isOpenAIWSTerminalEvent(eventType)
			if eventType == "error" {
				// Retain the provider's attempt evidence without committing a
				// client failure. AfterTurn confirms recovery or final failure.
				setOpsUpstreamError(c, openAIWSRejectedFieldRetryHTTPStatus(upstreamMessage), extractOpenAISSEErrorMessage(upstreamMessage), openAIWSProvisionalErrorDetail)
				if synthesizedRejection || openAIWSDefinitiveRequestRejection(upstreamMessage, responseID) {
					lease.MarkBroken()
					upstreamMessage = buildOpenAIWSHTTPBridgeFailedEvent(responseID, originalModel, createdAtTracker.Value(), upstreamMessage, "")
					eventType, isTerminalEvent = "response.failed", true
				} else {
					bareErrorPayload = append(bareErrorPayload[:0], upstreamMessage...)
					bareErrorDeadline = time.Now().Add(s.openAIWSErrorIdleTimeout(payload))
					continue
				}
			}
			if len(bareErrorPayload) > 0 {
				if isTokenEvent || isTerminalEvent || eventType == "response.output_item.done" {
					bareErrorPayload = nil
					bareErrorDeadline = time.Time{}
					if eventType != "response.failed" {
						retryStopReason = ""
					}
				} else {
					bareErrorDeadline = time.Now().Add(s.openAIWSErrorIdleTimeout(payload))
				}
			}
			if isTerminalEvent {
				terminalEventCount++
			}
			if firstTokenMs == nil && isTokenEvent {
				ms := int(time.Since(turnStart).Milliseconds())
				firstTokenMs = &ms
			}
			imageCounter.AddSSEData(upstreamMessage)

			if !clientDisconnected {
				if needModelReplace && len(mappedModelBytes) > 0 && openAIWSEventMayContainModel(eventType) && bytes.Contains(upstreamMessage, mappedModelBytes) {
					upstreamMessage = replaceOpenAIWSMessageModel(upstreamMessage, mappedModel, originalModel)
				}
				if openAIWSEventMayContainToolCalls(eventType) && openAIWSMessageLikelyContainsToolCalls(upstreamMessage) {
					if corrected, changed := s.toolCorrector.CorrectToolCallsInSSEBytes(upstreamMessage); changed {
						upstreamMessage = corrected
					}
				}
				replayCollector.AddEvent(eventType, upstreamMessage)
				// 客户端写出副本改写容量降载码：Codex 对 error/response.failed 中的
				// server_is_overloaded / slow_down 判致命并终止会话，改写后走客户端
				// 内置退避重试。HTTP/SSE（openai_gateway_response_handling.go）与
				// http_bridge（openai_ws_http_bridge.go）两条路径早已这么做，
				// ctx_pool 的 ingress 直写路径是唯一漏掉的一条 —— 同一个上游降载
				// 事件在这里会让会话就地终止，切到 http_bridge 却能正常退避重试。
				//
				// 必须写进独立变量而不是原地改 upstreamMessage：下面的
				// markOpenAIWSClientVisibleFailure 与 handleOpenAIWSTerminalTransientFailure
				// 仍要按未改写的原始 payload 判定账号状态，这正是
				// sanitizeOpenAICapacityShedErrorCodeForClient 注释里写明的前提。
				clientMessage := upstreamMessage
				if eventType == "error" || eventType == "response.failed" {
					if rewritten, changed := sanitizeOpenAICapacityShedErrorCodeForClient(clientMessage); changed {
						clientMessage = rewritten
					}
				}
				holdMetadata := !wroteDownstream && (isOpenAIWSTurnMetadataEvent(eventType) || eventType == "response.created" || eventType == "response.in_progress")
				var writeErr error
				if holdMetadata {
					if heldTurnMetadataBytes+len(clientMessage) > openAIFirstOutputStageMaxBytes {
						writeErr = flushHeld("staging_bytes_limit")
						holdMetadata = false
						if writeErr == nil {
							wroteDownstream = true
							writeErr = writeTurnMessage(clientMessage)
						}
					} else {
						if heldUntil.IsZero() && (eventType == "response.created" || eventType == "response.in_progress") {
							if downstream.stagingDeadline.IsZero() {
								downstream.stagingDeadline = time.Now().Add(time.Second)
							}
							heldUntil = downstream.stagingDeadline
						}
						heldTurnMetadata = append(heldTurnMetadata, append([]byte(nil), clientMessage...))
						heldTurnMetadataBytes += len(clientMessage)
					}
				} else {
					reason := "upstream_event"
					if isOpenAIWSTokenEvent(eventType) {
						reason = "semantic_output"
					} else if isOpenAIWSTerminalEvent(eventType) {
						reason = "terminal_event"
					}
					writeErr = flushHeld(reason)
					if progress.commitReason == "" {
						progress.commitReason = reason
					}
					if writeErr == nil {
						wroteDownstream = true
						writeErr = writeTurnMessage(clientMessage)
					}
				}
				if !holdMetadata {
					if writeErr != nil {
						return nil, wrapOpenAIWSIngressTurnError("write_client", fmt.Errorf("write client websocket event: %w", writeErr), wroteDownstream)
					}
					wroteDownstream = true
					if !clientDisconnected {
						if eventType == "response.failed" {
							RecordOpenAIRetryStop(c, account, retryStopReason, retryStopDetail)
						}
						markOpenAIWSClientVisibleFailure(c, eventType, upstreamMessage)
					}
				}
			}
			if isTerminalEvent {
				if responseID == "" {
					downstream.active.Store(false)
				}
				terminalEvent := s.handleOpenAIWSTerminalTransientFailure(ctx, account, mappedModel, lease.HandshakeHeaders(), upstreamMessage)
				if eventType == "response.failed" && terminalEvent == "" {
					// A rejected create has no response identity. Its wire error is
					// nevertheless a finished local turn, with no replayable output.
					s.handleOpenAIWSFailureAccountSideEffects(ctx, account, mappedModel, lease.HandshakeHeaders(), upstreamMessage)
					terminalEvent = "response.failed"
				}
				// 客户端已断连时，上游连接的 session 状态不可信，标记 broken 避免回池复用。
				if clientDisconnected {
					lease.MarkBroken()
				}
				if debugEnabled {
					logOpenAIWSModeDebug(
						"ingress_ws_turn_completed account_id=%d turn=%d conn_id=%s response_id=%s events=%d token_events=%d terminal_events=%d first_event=%s last_event=%s client_disconnected=%v",
						account.ID,
						turn,
						truncateOpenAIWSLogValue(lease.ConnID(), openAIWSIDValueMaxLen),
						truncateOpenAIWSLogValue(responseID, openAIWSIDValueMaxLen),
						eventCount,
						tokenEventCount,
						terminalEventCount,
						truncateOpenAIWSLogValue(firstEventType, openAIWSLogValueMaxLen),
						truncateOpenAIWSLogValue(lastEventType, openAIWSLogValueMaxLen),
						clientDisconnected,
					)
				}
				return resultWithUsage(terminalEvent, gjson.GetBytes(upstreamMessage, "response.incomplete_details.reason").String()), nil
			}
		}
	}

	// Admission hooks must see client model candidates before upstream mapping.
	currentClientPayload := firstPayload.rawForHash
	currentOriginalModel := firstPayload.originalModel
	currentImageBillingModel := firstPayload.imageBillingModel
	currentImageSizeTier := firstPayload.imageSizeTier
	currentImageInputSize := firstPayload.imageInputSize
	currentRequestedReasoningEffort := firstPayload.requestedReasoningEffort
	isStrictAffinityTurn := func(payload []byte) bool {
		if !storeDisabled {
			return false
		}
		return strings.TrimSpace(openAIWSPayloadStringFromRaw(payload, "previous_response_id")) != ""
	}
	var sessionLease *openAIWSConnLease
	sessionConnID := ""
	pinnedSessionConnID := ""
	unpinSessionConn := func(connID string) {
		connID = strings.TrimSpace(connID)
		if connID == "" || pinnedSessionConnID != connID {
			return
		}
		pool.UnpinConn(account.ID, connID)
		pinnedSessionConnID = ""
	}
	pinSessionConn := func(connID string) {
		if !storeDisabled {
			return
		}
		connID = strings.TrimSpace(connID)
		if connID == "" || pinnedSessionConnID == connID {
			return
		}
		if pinnedSessionConnID != "" {
			pool.UnpinConn(account.ID, pinnedSessionConnID)
			pinnedSessionConnID = ""
		}
		if pool.PinConn(account.ID, connID) {
			pinnedSessionConnID = connID
		}
	}
	// lastTurnClean 标记最后一轮 sendAndRelay 是否正常完成（收到终端事件且客户端未断连）。
	// 所有异常路径（读写错误、error 事件、客户端断连）已在各自分支或上层（L3403）中 MarkBroken，
	// 因此 releaseSessionLease 中只需在非正常结束时 MarkBroken。
	lastTurnClean := false
	releaseSessionLease := func() {
		if sessionLease == nil {
			return
		}
		if !lastTurnClean {
			sessionLease.MarkBroken()
		}
		unpinSessionConn(sessionConnID)
		sessionLease.Release()
		if debugEnabled {
			logOpenAIWSModeDebug(
				"ingress_ws_upstream_released account_id=%d conn_id=%s",
				account.ID,
				truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
			)
		}
	}
	defer releaseSessionLease()

	turn := 1
	rejectedFieldRetryState = newOpenAIResponsesRejectedFieldRetryState(currentPayload)
	turnPrevRecoveryTried := false
	lastTurnFinishedAt := time.Time{}
	lastTurnResponseID := ""
	lastTurnWindowID := ""
	lastTurnPayload := []byte(nil)
	var lastTurnStrictState *openAIWSIngressPreviousTurnStrictState
	lastTurnReplayInput := []json.RawMessage(nil)
	lastTurnReplayInputExists := false
	lastTurnReplayComplete := false
	if saved := downstream.nativeHistory; saved.accountID == account.ID {
		lastTurnResponseID, lastTurnWindowID = saved.responseID, saved.windowID
		lastTurnPayload, lastTurnStrictState = saved.payload, saved.strict
		lastTurnReplayInput, lastTurnReplayInputExists, lastTurnReplayComplete = saved.input, saved.exists, saved.complete
	}
	defer func() {
		downstream.nativeHistory = openAIWSRecoveryHistory{accountID: account.ID, responseID: lastTurnResponseID, windowID: lastTurnWindowID, payload: lastTurnPayload, strict: lastTurnStrictState, input: lastTurnReplayInput, exists: lastTurnReplayInputExists, complete: lastTurnReplayComplete}
	}()

	skipBeforeTurn := false
	resetSessionLease := func(markBroken bool) {
		if sessionLease == nil {
			return
		}
		if markBroken {
			sessionLease.MarkBroken()
			baseAcquireReq.Headers.Del(openAIWSTurnStateHeader)
			if stateStore != nil {
				// Another socket may already have replaced these shared bindings.
				stateStore.DeleteResponseConn(lastTurnResponseID, sessionConnID)
				stateStore.DeleteSessionConn(groupID, sessionHash, sessionConnID)
				stateStore.DeleteSessionTurnState(groupID, sessionHash, turnState)
			}
			turnState = ""
		}
		releaseSessionLease()
		sessionLease = nil
		sessionConnID = ""
		preferredConnID = ""
	}
	recoverIngressPrevResponseNotFound := func(relayErr error, turn int, connID string) bool {
		if !isOpenAIWSIngressPreviousResponseNotFound(relayErr) || turnPrevRecoveryTried ||
			!s.openAIWSIngressPreviousResponseRecoveryEnabled() || !currentTurnReplayComplete {
			return false
		}
		replay, safe, err := buildOpenAIWSCurrentTurnRetryPayload(currentPayload,
			currentTurnReplayInput, currentTurnReplayComplete, openAIWSPayloadStringFromRaw(currentPayload, "model"), true)
		if err != nil || !safe {
			return false
		}
		turnPrevRecoveryTried = true
		logOpenAIWSModeInfo("ingress_ws_prev_response_recovery account_id=%d turn=%d conn_id=%s action=full_context_replay", account.ID, turn, connID)
		currentPayload = replay
		currentPayloadBytes = len(replay)
		resetSessionLease(true)
		skipBeforeTurn = true
		return true
	}
	retryIngressTurn := func(relayErr error, turn int, connID string) bool {
		if !downstream.alive() {
			return false
		}
		var turnErr *openAIWSIngressTurnError
		if IsOpenAIWSUpstreamTransportError(relayErr) && errors.As(relayErr, &turnErr) && turnErr.wroteDownstream {
			RecordOpenAIRetryStop(c, account, OpenAIRetryStopDownstreamCommitted, turnErr.retryDetail)
		}
		if !isOpenAIWSIngressTurnRetryable(relayErr) {
			return false
		}
		blocker := openAIWSUncertainExecutionReplayBlocker(currentPayload)
		if currentTurnReplayComplete {
			blocker = openAIWSUncertainExecutionReplayBlocker(currentPayload, currentTurnReplayInput)
		}
		if blocker != "" {
			RecordOpenAIRetryStop(c, account, OpenAIRetryStopReplayUnsafe, turnErr.retryDetail)
			return false
		}
		if turnRetry >= 1 {
			RecordOpenAIRetryStop(c, account, OpenAIRetryStopTurnBudget, turnErr.retryDetail)
			return false
		}
		if isStrictAffinityTurn(currentPayload) {
			replay, safe, err := buildOpenAIWSCurrentTurnRetryPayload(currentPayload, currentTurnReplayInput, currentTurnReplayComplete, openAIWSPayloadStringFromRaw(currentPayload, "model"), true)
			if err != nil || !safe {
				RecordOpenAIRetryStop(c, account, OpenAIRetryStopReplayUnsafe, turnErr.retryDetail)
				return false
			}
			currentPayload = replay
			currentPayloadBytes = len(replay)
		}
		turnRetry++
		logOpenAIWSModeInfo(
			"ingress_ws_turn_retry account_id=%d turn=%d retry=%d reason=%s conn_id=%s",
			account.ID,
			turn,
			turnRetry,
			truncateOpenAIWSLogValue(openAIWSIngressTurnRetryReason(relayErr), openAIWSLogValueMaxLen),
			truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
		)
		resetSessionLease(true)
		skipBeforeTurn = true
		return true
	}
	for {
		var accountRefreshErr error
		if turn > 1 && !skipBeforeTurn && hooks != nil && hooks.BeforeRequest != nil {
			if err := hooks.BeforeRequest(turn, currentClientPayload, currentOriginalModel); err != nil {
				var changed *OpenAIWSAccountChangedError
				if !errors.As(err, &changed) {
					return err
				}
				accountRefreshErr = err
			}
		}
		if accountRefreshErr == nil && !skipBeforeTurn && hooks != nil && hooks.BeforeTurn != nil {
			if err := hooks.BeforeTurn(turn); err != nil {
				return err
			}
		}
		skipBeforeTurn = false
		boundaryPayload, contextWindowBoundary, boundaryErr := normalizeOpenAIWSContextWindowBoundary(
			currentPayload,
			lastTurnWindowID,
		)
		if boundaryErr != nil {
			return fmt.Errorf("normalize Codex websocket context-window boundary: %w", boundaryErr)
		}
		if contextWindowBoundary.Changed {
			currentPayload = boundaryPayload
			currentPayloadBytes = len(boundaryPayload)
			logOpenAIWSModeInfo(
				"ingress_ws_context_window_changed account_id=%d turn=%d conn_id=%s action=break_previous_response_chain previous_window_id=%s current_window_id=%s previous_response_id_removed=%v",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
				truncateOpenAIWSLogValue(lastTurnWindowID, openAIWSIDValueMaxLen),
				truncateOpenAIWSLogValue(contextWindowBoundary.WindowID, openAIWSIDValueMaxLen),
				contextWindowBoundary.PreviousResponseIDRemoved,
			)
		}
		currentPreviousResponseID := openAIWSPayloadStringFromRaw(currentPayload, "previous_response_id")
		expectedPrev := strings.TrimSpace(lastTurnResponseID)
		if contextWindowBoundary.Changed {
			// A context-window rollover is a new Responses root. Do not infer a
			// continuation anchor from the response produced in the old window.
			expectedPrev = ""
		}
		toolSignals := ToolContinuationSignals{
			HasFunctionCallOutput: openAIWSRawPayloadHasToolCallOutput(currentPayload),
		}
		if toolSignals.HasFunctionCallOutput {
			var currentReqBody map[string]any
			if err := json.Unmarshal(currentPayload, &currentReqBody); err == nil {
				toolSignals = AnalyzeToolContinuationSignals(currentReqBody)
			}
		}
		hasFunctionCallOutput := toolSignals.HasFunctionCallOutput
		// store=false + function_call_output 场景必须有续链锚点。
		// 若客户端未传 previous_response_id，优先回填上一轮响应 ID，避免上游报 call_id 无法关联。
		if shouldInferIngressFunctionCallOutputPreviousResponseID(
			storeDisabled,
			turn,
			toolSignals,
			currentPreviousResponseID,
			expectedPrev,
		) {
			updatedPayload, setPrevErr := setPreviousResponseIDToRawPayload(currentPayload, expectedPrev)
			if setPrevErr != nil {
				logOpenAIWSModeInfo(
					"ingress_ws_function_call_output_prev_infer_skip account_id=%d turn=%d conn_id=%s reason=set_previous_response_id_error cause=%s expected_previous_response_id=%s",
					account.ID,
					turn,
					truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
					truncateOpenAIWSLogValue(setPrevErr.Error(), openAIWSLogValueMaxLen),
					truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
				)
			} else {
				currentPayload = updatedPayload
				currentPayloadBytes = len(updatedPayload)
				currentPreviousResponseID = expectedPrev
				logOpenAIWSModeInfo(
					"ingress_ws_function_call_output_prev_infer account_id=%d turn=%d conn_id=%s action=set_previous_response_id previous_response_id=%s",
					account.ID,
					turn,
					truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
					truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
				)
			}
		}
		nextReplayInput, nextReplayInputExists, replayInputErr := buildOpenAIWSReplayInputSequence(
			lastTurnReplayInput,
			lastTurnReplayInputExists,
			currentPayload,
			currentPreviousResponseID != "",
		)
		if replayInputErr != nil {
			logOpenAIWSModeInfo(
				"ingress_ws_replay_input_skip account_id=%d turn=%d conn_id=%s reason=build_error cause=%s",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
				truncateOpenAIWSLogValue(replayInputErr.Error(), openAIWSLogValueMaxLen),
			)
			currentTurnReplayInput = nil
			currentTurnReplayInputExists = false
		} else {
			currentTurnReplayInput = nextReplayInput
			currentTurnReplayInputExists = nextReplayInputExists
		}
		// A delta anchored before this socket has no locally known full history.
		// Never confuse "input exists" with a complete cross-connection replay.
		currentTurnReplayComplete = replayInputErr == nil && (currentPreviousResponseID == "" ||
			(lastTurnReplayComplete && currentPreviousResponseID == lastTurnResponseID))
		if accountRefreshErr != nil {
			resetSessionLease(true)
			return s.openAIWSAccountRefreshRetry(account, accountRefreshErr, currentClientPayload, currentTurnReplayInput, currentTurnReplayComplete && currentTurnReplayInputExists, currentOriginalModel)
		}
		replayHasFunctionCallOutput := currentTurnReplayInputExists &&
			openAIWSRawItemsHasFunctionCallOutput(currentTurnReplayInput)
		hasFunctionCallOutput = hasFunctionCallOutput || replayHasFunctionCallOutput
		if storeDisabled && turn > 1 && currentPreviousResponseID != "" {
			shouldKeepPreviousResponseID := false
			strictReason := ""
			var strictErr error
			if lastTurnStrictState != nil {
				shouldKeepPreviousResponseID, strictReason, strictErr = shouldKeepIngressPreviousResponseIDWithStrictState(
					lastTurnStrictState,
					currentPayload,
					lastTurnResponseID,
					hasFunctionCallOutput,
				)
			} else {
				shouldKeepPreviousResponseID, strictReason, strictErr = shouldKeepIngressPreviousResponseID(
					lastTurnPayload,
					currentPayload,
					lastTurnResponseID,
					hasFunctionCallOutput,
				)
			}
			if sessionLease == nil && preferredConnID == "" && currentPreviousResponseID == lastTurnResponseID {
				shouldKeepPreviousResponseID, strictReason, strictErr = false, "upstream_connection_lost", nil
			}
			if strictErr != nil {
				logOpenAIWSModeInfo(
					"ingress_ws_prev_response_strict_eval account_id=%d turn=%d conn_id=%s action=keep_previous_response_id reason=%s cause=%s previous_response_id=%s expected_previous_response_id=%s has_function_call_output=%v",
					account.ID,
					turn,
					truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
					normalizeOpenAIWSLogValue(strictReason),
					truncateOpenAIWSLogValue(strictErr.Error(), openAIWSLogValueMaxLen),
					truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
					truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
					hasFunctionCallOutput,
				)
			} else if !shouldKeepPreviousResponseID {
				if !currentTurnReplayComplete {
					return NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater,
						"continuation context unavailable; reconnect with full input context", ErrOpenAIWSReplayUnavailable)
				}
				updatedPayload, removed, dropErr := dropPreviousResponseIDFromRawPayload(currentPayload)
				if dropErr != nil || !removed {
					dropReason := "not_removed"
					if dropErr != nil {
						dropReason = "drop_error"
					}
					logOpenAIWSModeInfo(
						"ingress_ws_prev_response_strict_eval account_id=%d turn=%d conn_id=%s action=keep_previous_response_id reason=%s drop_reason=%s previous_response_id=%s expected_previous_response_id=%s has_function_call_output=%v",
						account.ID,
						turn,
						truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
						normalizeOpenAIWSLogValue(strictReason),
						normalizeOpenAIWSLogValue(dropReason),
						truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
						truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
						hasFunctionCallOutput,
					)
				} else {
					updatedWithInput, setInputErr := setOpenAIWSPayloadInputSequence(
						updatedPayload,
						currentTurnReplayInput,
						currentTurnReplayInputExists,
					)
					if setInputErr != nil {
						logOpenAIWSModeInfo(
							"ingress_ws_prev_response_strict_eval account_id=%d turn=%d conn_id=%s action=keep_previous_response_id reason=%s drop_reason=set_full_input_error previous_response_id=%s expected_previous_response_id=%s cause=%s has_function_call_output=%v",
							account.ID,
							turn,
							truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
							normalizeOpenAIWSLogValue(strictReason),
							truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
							truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
							truncateOpenAIWSLogValue(setInputErr.Error(), openAIWSLogValueMaxLen),
							hasFunctionCallOutput,
						)
					} else {
						currentPayload = updatedWithInput
						currentPayloadBytes = len(updatedWithInput)
						logOpenAIWSModeInfo(
							"ingress_ws_prev_response_strict_eval account_id=%d turn=%d conn_id=%s action=drop_previous_response_id_full_create reason=%s previous_response_id=%s expected_previous_response_id=%s has_function_call_output=%v",
							account.ID,
							turn,
							truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
							normalizeOpenAIWSLogValue(strictReason),
							truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
							truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
							hasFunctionCallOutput,
						)
						currentPreviousResponseID = ""
					}
				}
			}
		}
		timing.Finish(nil)
		timing = nil
		attemptReserved := false
		reserveAttempt := func() error {
			if attemptReserved {
				return nil
			}
			s.installSchedulingRecovery(ctx, gjson.GetBytes(currentPayload, "model").String())
			if err := TakeOpenAITurnAttempt(withTurnRecoveryTarget(ctx, account, "websocket", "/v1/responses", resolveAccountProxyURL(account), token, currentPayload)); err != nil {
				return wrapOpenAIWSIngressTurnError("before_upstream_write", err, false)
			}
			attemptReserved = true
			return nil
		}
		forcePreferredConn := isStrictAffinityTurn(currentPayload)
		if sessionLease == nil {
			if err := reserveAttempt(); err != nil {
				return err
			}
			acquiredLease, acquireErr := acquireTurnLease(turn, preferredConnID, forcePreferredConn, turnRetry > 0)
			if acquireErr != nil {
				return fmt.Errorf("acquire upstream websocket: %w", acquireErr)
			}
			sessionLease = acquiredLease
			sessionConnID = strings.TrimSpace(sessionLease.ConnID())
			if storeDisabled {
				pinSessionConn(sessionConnID)
			} else {
				unpinSessionConn(sessionConnID)
			}
		}
		// A pinned/leased connection cannot be retired by idle pool cleanup.
		// Rotate between turns using the upstream age, not the client socket age.
		if sessionLease.conn != nil && sessionLease.conn.age(time.Now()) >= openAIWSConnMaxAge {
			replay, safe, replayErr := currentPayload, true, error(nil)
			if storeDisabled {
				replay, safe, replayErr = buildOpenAIWSCurrentTurnRetryPayload(currentPayload,
					currentTurnReplayInput, currentTurnReplayComplete, openAIWSPayloadStringFromRaw(currentPayload, "model"), true)
			}
			if replayErr != nil {
				return replayErr
			}
			resetSessionLease(true)
			if !safe || turnRetry >= 1 {
				return NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater,
					"upstream connection expired; reconnect with full input context", ErrOpenAIWSReplayUnavailable)
			}
			turnRetry++
			currentPayload = replay
			currentPayloadBytes = len(replay)
			skipBeforeTurn = true
			continue
		}
		shouldPreflightPing := turn > 1 && sessionLease != nil && sessionLease.SupportsIdlePingWithoutReader() && turnRetry == 0
		if shouldPreflightPing && openAIWSIngressPreflightPingIdle > 0 && !lastTurnFinishedAt.IsZero() {
			if time.Since(lastTurnFinishedAt) < openAIWSIngressPreflightPingIdle {
				shouldPreflightPing = false
			}
		}
		if shouldPreflightPing {
			if pingErr := sessionLease.PingWithTimeout(openAIWSProbePingTO); pingErr != nil {
				logOpenAIWSModeInfo(
					"ingress_ws_upstream_preflight_ping_fail account_id=%d turn=%d conn_id=%s cause=%s",
					account.ID,
					turn,
					truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
					truncateOpenAIWSLogValue(pingErr.Error(), openAIWSLogValueMaxLen),
				)
				if forcePreferredConn {
					if !turnPrevRecoveryTried && currentTurnReplayComplete {
						replay, safe, buildErr := buildOpenAIWSCurrentTurnRetryPayload(currentPayload,
							currentTurnReplayInput, currentTurnReplayComplete, openAIWSPayloadStringFromRaw(currentPayload, "model"), true)
						if buildErr != nil {
							return buildErr
						}
						if safe {
							turnPrevRecoveryTried = true
							currentPayload = replay
							currentPayloadBytes = len(replay)
							resetSessionLease(true)
							skipBeforeTurn = true
							continue
						}
					}
					resetSessionLease(true)
					return NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater,
						"upstream continuation unavailable; reconnect with full input context", ErrOpenAIWSReplayUnavailable)
				}
				resetSessionLease(true)

				if err := reserveAttempt(); err != nil {
					return err
				}
				acquiredLease, acquireErr := acquireTurnLease(turn, preferredConnID, forcePreferredConn, false)
				if acquireErr != nil {
					return fmt.Errorf("acquire upstream websocket after preflight ping fail: %w", acquireErr)
				}
				sessionLease = acquiredLease
				sessionConnID = strings.TrimSpace(sessionLease.ConnID())
				if storeDisabled {
					pinSessionConn(sessionConnID)
				}
			}
		}
		connID := sessionConnID
		if currentPreviousResponseID != "" {
			chainedFromLast := expectedPrev != "" && currentPreviousResponseID == expectedPrev
			currentPreviousResponseIDKind := ClassifyOpenAIPreviousResponseIDKind(currentPreviousResponseID)
			logOpenAIWSModeInfo(
				"ingress_ws_turn_chain account_id=%d turn=%d conn_id=%s previous_response_id=%s previous_response_id_kind=%s last_turn_response_id=%s chained_from_last=%v preferred_conn_id=%s header_session_id=%s header_conversation_id=%s has_turn_state=%v turn_state_len=%d has_prompt_cache_key=%v store_disabled=%v",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
				truncateOpenAIWSLogValue(currentPreviousResponseID, openAIWSIDValueMaxLen),
				normalizeOpenAIWSLogValue(currentPreviousResponseIDKind),
				truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
				chainedFromLast,
				truncateOpenAIWSLogValue(preferredConnID, openAIWSIDValueMaxLen),
				openAIWSHeaderValueForLog(baseAcquireReq.Headers, "session_id"),
				openAIWSHeaderValueForLog(baseAcquireReq.Headers, "conversation_id"),
				turnState != "",
				len(turnState),
				openAIWSPayloadStringFromRaw(currentPayload, "prompt_cache_key") != "",
				storeDisabled,
			)
		}

		if err := reserveAttempt(); err != nil {
			return err
		}
		if timing == nil && gjson.GetBytes(currentPayload, "generate").Type != gjson.False {
			timing = newOpenAITransportTiming(c, account, "websocket", int64(len(currentPayload)))
			timing.SetTransportReason("ctx_pool_ingress")
			timing.ConnectionReady(true, 0, 0, -1, -1)
		}
		if timing != nil && pendingPrewarm > 0 {
			timing.MarkPrewarm(pendingPrewarm)
			pendingPrewarm = 0
		}
		result, relayErr := sendAndRelay(turn, sessionLease, currentPayload, currentPayloadBytes, currentOriginalModel, currentImageBillingModel, currentImageSizeTier, currentImageInputSize, currentRequestedReasoningEffort)
		if relayErr != nil {
			meteredFailure := result != nil && (openAIUsageHasTokens(&result.Usage) || result.ImageCount > 0)
			if meteredFailure {
				relayErr = stopOpenAIMeteredReplay(relayErr)
			}
			lastTurnClean = false
			if isOpenAIWSSessionPreempted(ctx) {
				if meteredFailure && hooks != nil && hooks.AfterTurn != nil {
					hooks.AfterTurn(turn, result, errOpenAIWSSessionPreempted)
				}
				sessionLease.MarkBroken()
				return errOpenAIWSSessionPreempted
			}
			if !downstream.alive() {
				if result != nil {
					result.ClientDisconnect = true
				}
				if hooks != nil && hooks.AfterTurn != nil {
					hooks.AfterTurn(turn, result, nil)
				}
				resetSessionLease(true)
				return nil
			}
			var rejectedFieldErr *openAIWSRejectedFieldRetryError
			if !meteredFailure && errors.As(relayErr, &rejectedFieldErr) && rejectedFieldErr != nil && len(rejectedFieldErr.body) > 0 {
				currentPayload = append([]byte(nil), rejectedFieldErr.body...)
				currentPayloadBytes = len(currentPayload)
				if rejectedFieldErr.reconnect {
					resetSessionLease(true)
				}
				skipBeforeTurn = true
				continue
			}
			if !meteredFailure && recoverIngressPrevResponseNotFound(relayErr, turn, connID) {
				continue
			}
			if !meteredFailure && retryIngressTurn(relayErr, turn, connID) {
				continue
			}
			finalErr := relayErr
			var failoverErr *UpstreamFailoverError
			if !meteredFailure && turn > 1 && errors.As(finalErr, &failoverErr) {
				retryPayload, safe, buildErr := buildOpenAIWSCurrentTurnRetryPayload(
					currentClientPayload, currentTurnReplayInput, currentTurnReplayComplete, currentOriginalModel, true)
				if buildErr != nil {
					return fmt.Errorf("build websocket current-turn failover payload: %w", buildErr)
				}
				if !safe {
					retryPayload = nil
				}
				// A missing payload explicitly forbids replay; it must never cause
				// the handler to reuse the first response.create of this connection.
				finalErr = newOpenAIWSCurrentTurnFailoverError(finalErr, retryPayload)
			}
			if hooks != nil && hooks.AfterTurn != nil {
				hooks.AfterTurn(turn, result, finalErr)
			}
			resetSessionLease(true)
			return finalErr
		}
		turnRetry = 0
		turnPrevRecoveryTried = false
		lastTurnFinishedAt = time.Now()
		lastTurnClean = true
		if hooks != nil && hooks.AfterTurn != nil {
			hooks.AfterTurn(turn, result, nil)
		}
		if result == nil {
			return errors.New("websocket turn result is nil")
		}
		responseID := strings.TrimSpace(result.RequestID)
		cancelWithoutResponse := responseID == "" && (result.UpstreamTerminalEvent == "response.cancelled" || result.UpstreamTerminalEvent == "response.canceled")
		if !cancelWithoutResponse && result.UpstreamTerminalEvent != "response.failed" {
			lastTurnResponseID = responseID
			if contextWindowBoundary.WindowID != "" {
				lastTurnWindowID = contextWindowBoundary.WindowID
			}
			// 正文共享：currentPayload/currentTurnReplayInput 均不可变，历史直接引用；
			// collector 增量经 combine 合并（新头数组）。
			lastTurnReplayInput = currentTurnReplayInput
			lastTurnReplayInputExists = currentTurnReplayInputExists
			lastTurnReplayComplete = currentTurnReplayComplete && !result.wsReplayIncomplete
			if result.wsReplayInputExists {
				lastTurnReplayInput = combineOpenAIWSReplayItems(lastTurnReplayInput, result.wsReplayInput)
				lastTurnReplayInputExists = true
			}
			nextStrictState, strictStateErr := buildOpenAIWSIngressPreviousTurnStrictState(currentPayload)
			if strictStateErr != nil {
				lastTurnStrictState = nil
				// strict 状态不可用时保留整份上一轮 payload 供慢路径比较。
				lastTurnPayload = currentPayload
				logOpenAIWSModeInfo(
					"ingress_ws_prev_response_strict_state_skip account_id=%d turn=%d conn_id=%s reason=build_error cause=%s",
					account.ID,
					turn,
					truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
					truncateOpenAIWSLogValue(strictStateErr.Error(), openAIWSLogValueMaxLen),
				)
			} else {
				lastTurnStrictState = nextStrictState
				lastTurnPayload = nil
			}

			if responseID != "" && stateStore != nil {
				ttl := s.openAIWSResponseStickyTTL()
				logOpenAIWSBindResponseAccountWarn(groupID, account.ID, responseID, stateStore.BindResponseAccount(ctx, groupID, responseID, account.ID, ttl))
				stateStore.BindResponseConn(responseID, connID, ttl)
			}
			if stateStore != nil && storeDisabled && sessionHash != "" {
				stateStore.BindSessionConn(groupID, sessionHash, connID, s.openAIWSSessionStickyTTL())
			}
			if connID != "" {
				preferredConnID = connID
			}

		}

		if sessionLease.conn.isClosed() {
			resetSessionLease(true)
			lastTurnClean = false
		}

		nextClientMessage, readErr := readClientMessage()
		if readErr != nil {
			if isOpenAIWSSessionPreempted(ctx) {
				return errOpenAIWSSessionPreempted
			}
			if isOpenAIWSClientDisconnectError(readErr) {
				closeStatus, closeReason := summarizeOpenAIWSReadCloseError(readErr)
				logOpenAIWSModeInfo(
					"ingress_ws_client_closed account_id=%d conn_id=%s close_status=%s close_reason=%s",
					account.ID,
					truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
					closeStatus,
					truncateOpenAIWSLogValue(closeReason, openAIWSHeaderValueMaxLen),
				)
				return nil
			}
			return fmt.Errorf("read client websocket request: %w", readErr)
		}

		nextPayload, parseErr := parseNextValidPayload(turn+1, nextClientMessage)
		if parseErr != nil {
			return parseErr
		}
		if nextPayload.previousResponseID != "" {
			expectedPrev := strings.TrimSpace(lastTurnResponseID)
			chainedFromLast := expectedPrev != "" && nextPayload.previousResponseID == expectedPrev
			nextPreviousResponseIDKind := ClassifyOpenAIPreviousResponseIDKind(nextPayload.previousResponseID)
			logOpenAIWSModeInfo(
				"ingress_ws_next_turn_chain account_id=%d turn=%d next_turn=%d conn_id=%s previous_response_id=%s previous_response_id_kind=%s last_turn_response_id=%s chained_from_last=%v has_prompt_cache_key=%v store_disabled=%v",
				account.ID,
				turn,
				turn+1,
				truncateOpenAIWSLogValue(connID, openAIWSIDValueMaxLen),
				truncateOpenAIWSLogValue(nextPayload.previousResponseID, openAIWSIDValueMaxLen),
				normalizeOpenAIWSLogValue(nextPreviousResponseIDKind),
				truncateOpenAIWSLogValue(expectedPrev, openAIWSIDValueMaxLen),
				chainedFromLast,
				nextPayload.promptCacheKey != "",
				storeDisabled,
			)
		}
		if stateStore != nil && nextPayload.previousResponseID != "" {
			if stickyConnID, ok := stateStore.GetResponseConn(nextPayload.previousResponseID); ok {
				if sessionConnID != "" && stickyConnID != "" && stickyConnID != sessionConnID {
					logOpenAIWSModeInfo(
						"ingress_ws_keep_session_conn account_id=%d turn=%d conn_id=%s sticky_conn_id=%s previous_response_id=%s",
						account.ID,
						turn,
						truncateOpenAIWSLogValue(sessionConnID, openAIWSIDValueMaxLen),
						truncateOpenAIWSLogValue(stickyConnID, openAIWSIDValueMaxLen),
						truncateOpenAIWSLogValue(nextPayload.previousResponseID, openAIWSIDValueMaxLen),
					)
				} else {
					preferredConnID = stickyConnID
				}
			}
		}
		currentPayload = nextPayload.payloadRaw
		currentClientPayload = nextPayload.rawForHash
		currentOriginalModel = nextPayload.originalModel
		currentImageBillingModel = nextPayload.imageBillingModel
		currentImageSizeTier = nextPayload.imageSizeTier
		currentImageInputSize = nextPayload.imageInputSize
		currentPayloadBytes = nextPayload.payloadBytes
		currentRequestedReasoningEffort = nextPayload.requestedReasoningEffort
		downstream.budget.Store(NewOpenAITurnBudget(s.cfg))
		downstream.stagingDeadline = time.Time{}
		downstream.responseIDs = nil
		ctx = downstream.TurnBudgetContext(turnParentContext, s.cfg, currentPayload)
		s.installSchedulingRecovery(ctx, gjson.GetBytes(currentPayload, "model").String())
		turnStartedAt = time.Now()
		rejectedFieldRetryState = newOpenAIResponsesRejectedFieldRetryState(currentPayload)
		storeDisabled = s.isOpenAIWSStoreDisabledInRequestRaw(currentPayload, account)
		if !storeDisabled {
			unpinSessionConn(sessionConnID)
		}
		turn++
	}
}
