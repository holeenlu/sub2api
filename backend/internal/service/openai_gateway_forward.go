package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Forward forwards request to OpenAI API
func (s *OpenAIGatewayService) Forward(ctx context.Context, c *gin.Context, account *Account, body []byte) (forwardResult *OpenAIForwardResult, forwardErr error) {
	BeginOpenAIRequestTiming(c)
	defer func() {
		if forwardResult != nil {
			forwardResult.LatencyMetrics = SnapshotOpenAIUsageLatency(c, forwardResult.Stream)
		}
	}()
	defer func() { openAIForwardTimingFromContext(c).Finish(forwardErr) }()
	defer func() {
		if forwardErr == nil && forwardResult != nil {
			confirmSchedulingQuotaPrefix(ctx, account, forwardResult.Usage.CacheReadInputTokens)
		}
	}()
	ctx = withGatewayCachePrefix(ctx, body)
	setActualOpenAIUsageAuthorizationMode(c, "")
	if c != nil {
		c.Set(openAIPromotedCompactWireKey, false)
	}
	setOpenAIReplaySafety(c, body)
	if account.IsOpenAICloudConfigured() || cloudContinuationFromContext(ctx) != nil {
		return s.forwardOpenAICloud(ctx, c, account, body)
	}
	account = excelBPSPoolForwardAccount(ctx, c, account)
	ctx = EnsureOpenAITurnBudget(ctx, s.cfg)
	beginUpstreamResponseModelObservation(c)
	ClearActualOpenAIUpstreamEndpoint(c)
	if shouldForwardOpenAIResponsesViaRawChatCompletions(account) {
		SetActualOpenAIUpstreamEndpoint(c, "/v1/chat/completions")
	}
	filteredBody, filterErr := filterOpenAIResponsesNoneReasoningEffortForAccount(account, body)
	if filterErr != nil {
		return nil, filterErr
	}
	body = filteredBody
	clearGrokResponsesClientToolMapping(c)
	clearOpenAIResponsesClientToolMapping(c)
	clearOpenAIResponsesNamespaceNames(c)
	setCodexToolNameReverse(c, nil)

	if _, err := s.prepareCodexAccountIdentitySource(ctx, c, account); err != nil {
		return nil, err
	}

	// 供应商凭据在准备前检查；普通账号已在选号时读取，发送边界仍实时复核。
	if account.VendorID != nil {
		if err := validateVendorDispatch(ctx, s.accountRepo, account, vendorDispatchGroup(c)); err != nil {
			return nil, vendorDispatchFailure(err)
		}
	}
	startTime := time.Now()
	// 固定渠道映射后的请求级 canonical body；账号 normalize/strip 不得改写跨 failover hint。
	canonicalImageIntentBody := body

	restrictionResult := s.detectCodexClientRestriction(c, account, body)
	apiKeyID := getAPIKeyIDFromContext(c)
	logCodexCLIOnlyDetection(ctx, c, account, apiKeyID, restrictionResult, body)
	if restrictionResult.Enabled && !restrictionResult.Matched {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
		writeOpenAIResponsesErrorJSON(c, http.StatusForbidden, gin.H{
			"error": gin.H{
				"type":    "forbidden_error",
				"message": CodexClientRestrictionMessage(restrictionResult),
			},
		})
		return nil, errors.New("codex_cli_only restriction: only codex official clients are allowed")
	}

	// 执行作用域必须取自客户端原始身份：后面的账号 namespace 改写与指纹收敛会改掉
	// 请求体里的 client_metadata / prompt_cache_key，用改写后的值取键会让不同会话
	// 落到同一个键，也会与 WS 接入路径按原始报文算出的键对不上。
	wsExecutionScope, _ := resolveOpenAIWSExecutionScope(c, body, apiKeyID)
	if c != nil {
		c.Set(openAIIncrementalOriginalScopeKey, wsExecutionScope)
	}

	bpsSelected := s.IsExcelBPSRouteEnabled(ctx, account, gjson.GetBytes(body, "model").String())
	if c.GetBool(bpsAccountProbeRequiredContextKey) && (!bpsSelected || account.excelBPSNativeFallbackReason(body) != "") {
		return nil, errors.New("bps probe path is unavailable")
	}
	if bpsSelected {
		hadContinuation := gjson.GetBytes(body, "previous_response_id").String() != ""
		if restored, restoreErr := s.restoreExcelBPSContinuation(ctx, c, account, body); restoreErr == nil {
			body, canonicalImageIntentBody = restored, restored
		}
		result, err := s.forwardExcelBPS(ctx, c, account, body, startTime)
		native := excelBPSNativeFallbackAccount(ctx, c, account, body, err)
		if native == nil {
			var failure *UpstreamFailoverError
			if hadContinuation && gjson.GetBytes(body, "previous_response_id").String() == "" && errors.As(err, &failure) {
				copy := *failure
				copy.ContinuationReplayBody = body
				return result, &copy
			}
			return result, err
		}
		defer finishExcelBPSNativeFallback(&forwardResult, result, account)
		account = native
	}
	if account.IsExcelOAuth() {
		writeOpenAIResponsesErrorJSON(c, http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "code": "basispoints_model_not_enabled", "message": "Excel-only authorization requires BPS enabled for this model"}})
		return nil, errors.New("Excel-only authorization cannot use native Codex")
	}
	setActualOpenAIUsageAuthorizationMode(c, nativeOpenAIUsageAuthorizationMode(account))
	return s.forwardOpenAIResponses(ctx, c, account, body, canonicalImageIntentBody, wsExecutionScope, startTime)
}

// forwardOpenAIResponses resolves protocol adaptation once. Only Responses
// requests reach native preparation; BPS terminates in Forward.
func (s *OpenAIGatewayService) forwardOpenAIResponses(ctx context.Context, c *gin.Context, account *Account, body, canonicalImageIntentBody []byte, wsExecutionScope string, startTime time.Time) (*OpenAIForwardResult, error) {
	if rejected := s.strictResponsesError(c, account, body); rejected != nil {
		writeOpenAIResponsesErrorJSON(c, http.StatusBadRequest, rejected.response())
		return nil, rejected
	}
	normalizedBody, normalized, err := normalizeOpenAICodexCompactReasoningEffortForAccount(c, account, body)
	if err != nil {
		return nil, err
	}
	if normalized {
		body = normalizedBody
	}
	legacyIngressBody, legacyIngressChanged, legacyIngressErr := normalizeOpenAIResponsesLegacyIngressWithConfig(s.cfg, body)
	if legacyIngressErr != nil {
		return nil, legacyIngressErr
	}
	if legacyIngressChanged {
		body = legacyIngressBody
	}
	// 在分流到 passthrough / Codex transform / 原生 ChatCompletions 之前统一修正
	// 显式为 null 的工具 Schema type，否则 upstream 的 400 会被归一成可重试的 502，
	// 同一份坏定义在账号池里反复重放。
	if sanitizedToolBody, toolSchemaSanitized, toolSchemaErr := sanitizeOpenAIResponsesToolSchemasForPlatform(body, account.Platform); toolSchemaErr != nil {
		return nil, toolSchemaErr
	} else if toolSchemaSanitized {
		body = sanitizedToolBody
	}
	if account.IsOpenAIOAuthLike() {
		reasoningBody, reasoningChanged, reasoningErr := normalizeOpenAIResponsesReasoningMode(body, account.GetMappedModel(gjson.GetBytes(body, "model").String()))
		if reasoningErr != nil {
			return nil, fmt.Errorf("normalize OpenAI Responses reasoning.mode: %w", reasoningErr)
		}
		if reasoningChanged {
			body = reasoningBody
		}
	}
	responsesLite := account.IsOpenAI() && isOpenAIResponsesLiteHeader(c.GetHeader(responsesLiteHeader))
	if responsesLite {
		liteBody, changed, liteErr := normalizeOpenAIResponsesLitePayloadForAccount(body, account)
		if liteErr != nil {
			param := "tools"
			var validationErr *openAIResponsesLiteValidationError
			if errors.As(liteErr, &validationErr) {
				param = validationErr.param
			}
			setOpsUpstreamError(c, http.StatusBadRequest, liteErr.Error(), "")
			writeOpenAIResponsesErrorJSON(c, http.StatusBadRequest, gin.H{"error": gin.H{
				"type": "invalid_request_error", "message": liteErr.Error(), "param": param,
			}})
			return nil, liteErr
		}
		if changed {
			body = liteBody
		}
	}
	wsDecision := s.getOpenAIWSProtocolResolver().Resolve(account)
	// Adapted from huoji120/sub4api a3eaf63a. OAuth Responses HTTP clients
	// may use the existing account-controlled WS path; API-key relays retain
	// their HTTP contract and compact remains a unary HTTP operation.
	if GetOpenAIClientTransport(c) == OpenAIClientTransportHTTP && !account.IsOpenAIOAuthLike() {
		wsDecision = openAIWSHTTPDecision("http_relay_preserved")
	}
	passthroughEnabled := account.IsOpenAIPassthroughEnabled()
	compactPath := isOpenAIResponsesCompactPath(c)
	if compactPath {
		wsDecision = openAIWSHTTPDecision("compact_http")
	} else if passthroughEnabled {
		wsDecision = openAIWSHTTPDecision("passthrough_http")
	}
	if shouldFlattenOpenAIResponsesNamespaces(account, wsDecision.Transport, passthroughEnabled, compactPath) {
		body, err = flattenOpenAIResponsesNamespaces(c, body)
		if err != nil {
			setOpsUpstreamError(c, http.StatusBadRequest, err.Error(), "")
			writeOpenAIResponsesErrorJSON(c, http.StatusBadRequest, gin.H{"error": gin.H{
				"type": "invalid_request_error", "message": err.Error(), "param": "tools",
			}})
			return nil, err
		}
	}
	if shouldStripOpenAIResponsesInputNamespaces(account, wsDecision.Transport, passthroughEnabled) {
		keepToolCallNamespaces := shouldKeepOpenAIResponsesToolCallNamespaces(
			account, wsDecision.Transport, passthroughEnabled, compactPath, body,
		)
		body, err = stripOpenAIResponsesInputNamespaces(body, keepToolCallNamespaces)
		if err != nil {
			setOpsUpstreamError(c, http.StatusBadRequest, err.Error(), "")
			writeOpenAIResponsesErrorJSON(c, http.StatusBadRequest, gin.H{"error": gin.H{
				"type": "invalid_request_error", "message": err.Error(), "param": "input",
			}})
			return nil, err
		}
	}

	nativeCNResponses := account.UsesNativeCNResponses()
	nativeDeepSeekResponses := account.Platform == PlatformDeepseek && nativeCNResponses
	if nativeDeepSeekResponses && account.Type == AccountTypeAPIKey && !compactPath &&
		needsOpenAIResponsesClientToolAdaptation(body) {
		adaptedBody, mapping, adaptErr := adaptOpenAIResponsesClientTools(body)
		if adaptErr != nil {
			return nil, fmt.Errorf("adapt DeepSeek Responses client tools: %w", adaptErr)
		}
		body = adaptedBody
		setOpenAIResponsesClientToolMapping(c, mapping)
	}

	originalBody := body
	rememberOpenCodeInboundBody(c, originalBody)
	requestView := newOpenAIRequestView(body)
	reqModel, reqStream := requestView.Model, requestView.Stream
	markOpenAIResponsesClientStream(c, reqStream)

	if account.Platform == PlatformGrok {
		return s.forwardGrokResponses(ctx, c, account, body, reqModel, reqStream, startTime)
	}

	if account.IsOpenAIApiKey() {
		if normalized, changed, normalizeErr := normalizeOpenAIParallelToolCallsWithoutTools(body, responsesLite); normalizeErr != nil {
			return nil, normalizeErr
		} else if changed {
			body = normalized
			originalBody = normalized
		}
		if normalized, changed, normalizeErr := normalizeOpenAIAPIKeyStoreFalseReasoningReplay(body, isOpenAIResponsesCompactPath(c)); normalizeErr != nil {
			return nil, normalizeErr
		} else if changed {
			body = normalized
			originalBody = normalized
		}
		requestView = newOpenAIRequestView(body)
		reqModel, reqStream = requestView.Model, requestView.Stream
	}

	// 上游协议统一由 resolveUpstreamProtocol 判定（按模型分流时带上游模型目录）。OpenAI API Key 账号只会落到
	// Responses / Chat Completions，上面的归一化对两条路径都生效。
	routingModel := upstreamRoutingModel(account, body, "")
	if account.IsOpenCodeGo() && IsOpenCodeUnsupportedModel(routingModel) {
		return nil, writeOpenCodeUnsupportedModelError(c, false, routingModel)
	}
	switch s.resolveUpstreamProtocolFor(ctx, account, APIProtocolResponses, routingModel) {
	case APIProtocolAnthropic:
		// Responses 客户端 × Anthropic 上游：转成 Anthropic 请求走原生端点。不能落到
		// raw-CC 分支——其 URL 构造会把 anthropic base 当 CC base 用。
		// 账号映射未命中时以去除首尾空白的请求模型兜底，计费名与上游模型名一致。
		return s.forwardResponsesViaNativeAnthropic(ctx, c, account, body, reqModel)
	case APIProtocolChatCompletions:
		return s.forwardResponsesViaRawChatCompletions(ctx, c, account, body, startTime)
	}
	SetActualOpenAIUpstreamEndpoint(c, openAIResponsesUpstreamEndpoint)
	if account.IsOpenAI() && (account.IsOpenAIApiKey() || account.IsOpenAIOAuthLike()) {
		normalizedReasoningBody, reasoningChanged, reasoningErr := normalizeOpenAIResponsesReasoningContentReplay(body)
		if reasoningErr != nil {
			return nil, fmt.Errorf("normalize OpenAI Responses reasoning content replay: %w", reasoningErr)
		}
		if reasoningChanged {
			body = normalizedReasoningBody
			originalBody = normalizedReasoningBody
			requestView = newOpenAIRequestView(normalizedReasoningBody)
			reqModel, reqStream = requestView.Model, requestView.Stream
		}
		sanitizedBody, changed, sanitizeErr := sanitizeOpenAIResponsesInputItemIDs(body)
		if sanitizeErr != nil {
			return nil, fmt.Errorf("sanitize OpenAI Responses input item IDs: %w", sanitizeErr)
		}
		if changed {
			body = sanitizedBody
			originalBody = sanitizedBody
			requestView = newOpenAIRequestView(sanitizedBody)
			reqModel, reqStream = requestView.Model, requestView.Stream
		}
	}

	compatMessagesBridge := isOpenAICompatMessagesBridgeBody(body)
	setOpenAICompatMessagesBridgeContext(c, compatMessagesBridge)

	isCodexCLI := openai.IsCodexOfficialClientByHeaders(c.GetHeader("User-Agent"), c.GetHeader("originator")) || (s.cfg != nil && s.cfg.Gateway.ForceCodexCLI)
	codexImageGenerationExplicitToolPolicy := codexImageGenerationExplicitToolPolicyAllow
	if isCodexCLI {
		codexImageGenerationExplicitToolPolicy = account.CodexImageGenerationExplicitToolPolicy()
	}
	if c != nil {
		c.Set("openai_ws_transport_decision", string(wsDecision.Transport))
		c.Set("openai_ws_transport_reason", wsDecision.Reason)
	}
	if wsDecision.Transport == OpenAIUpstreamTransportResponsesWebsocketV2 {
		logOpenAIWSModeDebug(
			"selected account_id=%d account_type=%s transport=%s reason=%s model=%s stream=%v",
			account.ID,
			account.Type,
			normalizeOpenAIWSLogValue(string(wsDecision.Transport)),
			normalizeOpenAIWSLogValue(wsDecision.Reason),
			reqModel,
			reqStream,
		)
	}
	// 当前仅支持 WSv2；WSv1 命中时直接返回错误，避免出现“配置可开但行为不确定”。
	if wsDecision.Transport == OpenAIUpstreamTransportResponsesWebsocket {
		if c != nil {
			MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalFeatureGate)
			writeOpenAIResponsesErrorJSON(c, http.StatusBadRequest, gin.H{
				"error": gin.H{
					"type":    "invalid_request_error",
					"message": "OpenAI WSv1 is temporarily unsupported. Please enable responses_websockets_v2.",
				},
			})
		}
		return nil, errors.New("openai ws v1 is temporarily unsupported; use ws v2")
	}
	if passthroughEnabled {
		attemptImageIntentInvalidated := false
		if isCodexCLI && codexImageGenerationExplicitToolPolicy == codexImageGenerationExplicitToolPolicyStrip {
			strippedBody, changed, stripErr := stripOpenAIImageGenerationToolsFromRawPayload(body)
			if stripErr != nil {
				return nil, stripErr
			}
			if changed {
				body = strippedBody
				originalBody = strippedBody
				attemptImageIntentInvalidated = true
				logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Stripped /responses image_generation tool for Codex client by account policy")
			}
		}
		// 透传分支只需要轻量提取字段，避免热路径全量 Unmarshal。
		mappedModel := account.GetMappedModel(reqModel)
		reasoningEffort := extractOpenAIReasoningEffortFromBody(body, mappedModel)
		// 国产模型默认 effort 补充：也要用 mappedModel 判定是否是 passback-required 上游。
		reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, mappedModel)
		return s.forwardOpenAIPassthrough(
			ctx,
			c,
			account,
			originalBody,
			canonicalImageIntentBody,
			reqModel,
			attemptImageIntentInvalidated,
			reasoningEffort,
			reqStream,
			startTime,
		)
	}

	prepared, err := s.prepareNativeOpenAIResponses(ctx, c, account, openAIResponsesPreparation{
		body: body, view: requestView, canonicalBody: canonicalImageIntentBody,
		codexClient: isCodexCLI, compact: compactPath, lite: responsesLite,
		nativeCN: nativeCNResponses, messagesBridge: compatMessagesBridge,
		imageToolPolicy: codexImageGenerationExplicitToolPolicy,
	})
	if err != nil {
		return nil, err
	}
	return s.forwardPreparedOpenAIResponses(ctx, c, account, prepared, wsDecision, wsExecutionScope, startTime)
}

// forwardPreparedOpenAIResponses owns transport and delivery, with account
// preparation already complete. Protocol-local repairs still spend turn budget.
func (s *OpenAIGatewayService) forwardPreparedOpenAIResponses(ctx context.Context, c *gin.Context, account *Account, prepared *openAIPreparedResponses, wsDecision OpenAIWSProtocolDecision, wsExecutionScope string, startTime time.Time) (*OpenAIForwardResult, error) {
	body := prepared.body
	originalModel, requestedModel := prepared.originalModel, prepared.originalModel
	billingModel, reqModel, upstreamModel := prepared.billingModel, prepared.billingModel, prepared.upstreamModel
	reqStream, isCodexCLI := prepared.stream, prepared.codexClient
	promptCacheKey, clientPromptCacheKey := prepared.promptCacheKey, prepared.clientPromptCacheKey
	imageBillingModel, imageSizeTier, imageInputSize := prepared.imageBillingModel, prepared.imageSizeTier, prepared.imageInputSize
	// Get access token
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, newCredentialPreparationFailure(err)
	}
	SetOpsUpstreamModel(c, upstreamModel)

	// Recover equivalent HTTP capability only when the WS handshake rejected before generation.
	if wsDecision.Transport == OpenAIUpstreamTransportResponsesWebsocketV2 {
		// WS 分支需要结构化 payload 与重连恢复，命中后再触发 full-map decode。
		wsReqBody, err := prepared.decode(c)
		if err != nil {
			return nil, err
		}
		// Establish missing per-turn metadata once before same-account retries.
		// forwardOpenAIWSV2 may then project headers without minting a new turn.
		if err := normalizeCodexRequestMetadataMap(wsReqBody, nil, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c)); err != nil {
			return nil, err
		}
		_, hasPreviousResponseID := wsReqBody["previous_response_id"]
		logOpenAIWSModeDebug(
			"forward_start account_id=%d account_type=%s model=%s stream=%v has_previous_response_id=%v",
			account.ID,
			account.Type,
			upstreamModel,
			reqStream,
			hasPreviousResponseID,
		)
		wsResult, wsErr, wsAttempts, currentToken := s.forwardOpenAIWSV2WithRecovery(ctx, c, account, wsReqBody,
			clientPromptCacheKey, wsExecutionScope, token, wsDecision, isCodexCLI, reqStream,
			originalModel, upstreamModel, startTime)
		token = currentToken // HTTP fallback must keep a credential refreshed during WS recovery.
		if wsResult != nil {
			wsResult.UpstreamModel = upstreamModel
			if wsResult.BillingModel == "" {
				wsResult.BillingModel = billingModel
			}
			if wsResult.ImageCount > 0 {
				wsResult.ImageSize = imageSizeTier
				wsResult.ImageInputSize = imageInputSize
				wsResult.BillingModel = imageBillingModel
			}
		}
		if wsErr == nil {
			if wsResult != nil {
				// HTTP ingress still needs downstream ownership even when its
				// upstream transport is WS. Otherwise the next HTTP continuation
				// is rejected before it can use the connection-local response.
				s.bindHTTPResponseAccount(ctx, c, account, wsResult.ResponseID)
			}
			firstTokenMs := int64(0)
			hasFirstTokenMs := wsResult != nil && wsResult.FirstTokenMs != nil
			if hasFirstTokenMs {
				firstTokenMs = int64(*wsResult.FirstTokenMs)
			}
			requestID := ""
			if wsResult != nil {
				requestID = strings.TrimSpace(wsResult.RequestID)
			}
			logOpenAIWSModeDebug(
				"forward_succeeded account_id=%d request_id=%s stream=%v has_first_token_ms=%v first_token_ms=%d ws_attempts=%d",
				account.ID,
				requestID,
				reqStream,
				hasFirstTokenMs,
				firstTokenMs,
				wsAttempts,
			)
			return wsResult, nil
		}
		if wsResult != nil {
			if !wsResult.ClientDisconnect {
				s.writeOpenAIWSFallbackErrorResponse(c, account, wsErr)
			}
			return wsResult, wsErr
		}
		canHTTP, recoveryErr := planOpenAIWSTransportRecovery(ctx, c, account, body, wsErr)
		if !canHTTP {
			var failure *UpstreamFailoverError
			if !IsTurnBudgetError(recoveryErr) && !errors.As(recoveryErr, &failure) {
				s.writeOpenAIWSFallbackErrorResponse(c, account, recoveryErr)
			}
			return nil, recoveryErr
		}
		// HTTP 实际发送复用 doOpenAIUpstream 的实时授权检查。
		RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "retry", Action: "switch_mode", Reason: "websocket_to_http", AccountID: account.ID})
		c.Set("openai_ws_transport_decision", string(OpenAIUpstreamTransportHTTPSSE))
		c.Set("openai_ws_transport_reason", "websocket_to_http_before_generation")
		SetActualOpenAIUpstreamEndpoint(c, "/v1/responses")
	}

	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	// 国产模型默认 effort 补充：此处 reqModel 已被 mapping 重写为 billingModel。
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, reqModel)
	reasoningEffortValue := ""
	if reasoningEffort != nil {
		reasoningEffortValue = *reasoningEffort
	}
	firstOutputTimeout := time.Duration(0)
	if reqStream && account.Platform == PlatformOpenAI {
		firstOutputTimeout = s.openAIFirstOutputTimeout(reasoningEffortValue)
	}
	if firstOutputTimeout > 0 {
		openAITurnFirstOutputDeadline(ctx, firstOutputTimeout, startTime)
	}

	compactModelFallbackRetried := false
	agentTaskRecoveryTried := false
	rejectedFieldRetryState := openAIResponsesRejectedFieldRetryStateForRequest(c, body)
	for {
		// Build upstream request
		upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
		var headerGuard *openAIFirstOutputHeaderGuard
		if firstOutputTimeout > 0 {
			upstreamCtx, headerGuard = newOpenAIFirstOutputHeaderGuard(
				upstreamCtx, releaseUpstreamCtx, openAITurnFirstOutputDeadline(ctx, firstOutputTimeout, startTime),
			)
		}
		upstreamReq, err := s.buildUpstreamRequest(upstreamCtx, c, account, body, token, reqStream, promptCacheKey, isCodexCLI)
		if headerGuard == nil {
			releaseUpstreamCtx()
		}
		if err != nil {
			if headerGuard != nil {
				headerGuard.close()
			}
			writeOpenAIResponsesInputError(c, err)
			return nil, err
		}

		body = codexNormalizedRequestBody(upstreamReq, body)

		// Get proxy URL
		proxyURL := ""
		if account.ProxyID != nil && account.Proxy != nil {
			proxyURL = account.Proxy.URL()
		}

		// Send request
		progress := beginOpenAIResponseProgress(c, "http", "", nil)
		upstreamStart := time.Now()
		resp, err := s.doOpenAIResponsesUpstream(c, upstreamReq, proxyURL, account, reqStream)
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		if resp != nil {
			progress.protocol = resp.Proto
		}
		if headerGuard != nil && headerGuard.stopHeaderWait() {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			headerGuard.close()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var transportErr *OpenAITransportAttemptError
			if errors.Is(err, ErrOpenAIWSFirstOutputBudgetExhausted) || (errors.As(err, &transportErr) && transportErr.Unsent) {
				return nil, ErrOpenAIWSFirstOutputBudgetExhausted
			}
			return nil, s.newOpenAIFirstOutputTimeoutError(
				ctx, c, account, opsUpstreamProxyID(account), opsUpstreamProxyName(account),
				startTime, originalModel, reasoningEffortValue,
				firstOutputTimeout, "response_headers", nil,
			)
		}
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if headerGuard != nil {
				headerGuard.close()
			}
			// Transport-level failure (proxy/DNS/TCP/TLS — no HTTP response). Convert to
			// a failover so the handler switches to a healthy account, and temporarily
			// unschedule the account on durable faults (e.g. rejected proxy credentials).
			return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
		}
		if headerGuard != nil {
			resp.Body = &openAIRequestContextReadCloser{ReadCloser: resp.Body, cleanup: headerGuard.close}
		}

		// Handle error response
		if resp.StatusCode >= 400 {
			if resp.StatusCode == http.StatusUnauthorized && s.openAITokenProvider != nil {
				if fresh, ok := refreshRejectedCredential(ctx, s.accountRepo, account, token, s.openAITokenProvider.refreshAPI, s.openAITokenProvider.executor); ok {
					_ = resp.Body.Close()
					token = fresh
					continue
				}
			}

			respBody := s.readUpstreamErrorBody(resp)
			_ = resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(respBody))

			upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
			upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
			upstreamCode := extractUpstreamErrorCode(respBody)
			if !agentTaskRecoveryTried && s.isAgentIdentityAccount(ctx, account) && isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, respBody) {
				agentTaskRecoveryTried = true
				expectedTaskID := account.GetCredential("task_id")
				if err := s.recoverAgentIdentityTask(ctx, account, expectedTaskID); err != nil {
					return nil, fmt.Errorf("agent identity task recovery failed: %w", err)
				}
				continue
			}
			respBody = s.redactAgentIdentitySensitiveBody(ctx, account, respBody)
			resp.Body = io.NopCloser(bytes.NewReader(respBody))
			if IsUpstreamContentRefusal(respBody) {
				return s.handleErrorResponse(ctx, resp, c, account, body, reqModel)
			}
			if retryBody, reason, changed, retryErr := normalizeOpenAIResponsesRejectedFieldRetryBody(resp.StatusCode, body, respBody, prepared.webSearchHistoryToolAdded); retryErr != nil {
				return nil, fmt.Errorf("normalize rejected Responses field retry body: %w", retryErr)
			} else if changed && rejectedFieldRetryState.Allow(retryBody) {
				body = retryBody

				logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Retrying non-WSv2 request after %s (account: %s)", reason, account.Name)
				continue
			}
			if promoted, changed, promoteErr := prepareOpenAILegacyCompactPromotion(c, account, body, resp.StatusCode, respBody); promoteErr != nil {
				return nil, promoteErr
			} else if changed {
				body = promoted

				SetActualOpenAIUpstreamEndpoint(c, "/v1/responses")
				continue
			}
			if retryBody, fallbackModel, retry := s.prepareOpenAICompactFallbackRetry(
				c, account, requestedModel, body, resp.StatusCode, upstreamMsg, respBody, compactModelFallbackRetried,
			); retry {
				s.appendOpenAICompactFallbackRetryOps(c, account, resp, respBody, upstreamMsg, false)
				fromModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
				body = retryBody

				upstreamModel = fallbackModel
				compactModelFallbackRetried = true
				SetOpsUpstreamModel(c, fallbackModel)
				logger.LegacyPrintf(
					"service.openai_gateway",
					"[OpenAI] Retrying explicit compact request once with fallback model (account: %s, from: %s, to: %s, upstream_code: %s)",
					account.Name, fromModel, fallbackModel, upstreamCode,
				)
				continue
			}
			if s.shouldFailoverOpenAIUpstreamResponse(account, resp.StatusCode, upstreamMsg, respBody) {
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					ProxyID:            opsUpstreamProxyID(account),
					ProxyName:          opsUpstreamProxyName(account),
					Platform:           account.Platform,
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  resp.Header.Get("x-request-id"),
					Kind:               "failover",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})

				shouldDisable := s.handleFailoverSideEffects(ctx, resp, account, respBody, upstreamModel)
				return nil, s.newOpenAIAccountFailoverError(
					account,
					resp.StatusCode,
					resp.Header,
					respBody,
					upstreamMsg,
					shouldDisable,
					!shouldDisable && account.IsPoolMode() && (account.IsPoolModeRetryableStatus(resp.StatusCode) || isOpenAITransientProcessingError(resp.StatusCode, upstreamMsg, respBody)),
				)
			}
			return s.handleErrorResponse(ctx, resp, c, account, body, resolveOpenAIErrorSchedulingModel(billingModel, upstreamModel))
		}
		defer func() { _ = resp.Body.Close() }()

		if mapping, ok := openAIResponsesClientToolMapping(c); ok && isEventStreamResponse(resp.Header) {
			maxLineSize := defaultMaxLineSize
			if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
				maxLineSize = s.cfg.Gateway.MaxLineSize
			}
			resp.Body = newResponsesClientToolStreamBody(resp.Body, mapping, maxLineSize)
		}

		serviceTier := extractOpenAIServiceTierFromBody(body)
		// Release the prepared map after the upstream accepts the request.
		prepared.decoded = nil

		// Handle normal response
		var responseErr error
		clientDisconnected := false
		var usage *OpenAIUsage
		var firstTokenMs *int
		responseID := ""
		imageCount := 0
		searchCount := 0
		var imageOutputSizes []string
		if reqStream {
			streamResult, err := s.handleStreamingResponseWithReasoning(ctx, resp, c, account, startTime, originalModel, upstreamModel, reasoningEffortValue)
			if err != nil {
				if signal, ok := asOpenAICompactFallbackSignal(err); ok {
					if retryBody, fallbackModel, retry := s.prepareOpenAICompactFallbackRetry(
						c, account, requestedModel, body, http.StatusBadRequest, signal.message, signal.payload, compactModelFallbackRetried,
					); retry {
						s.appendOpenAICompactFallbackRetryOps(c, account, resp, signal.payload, signal.message, false)
						body = retryBody

						upstreamModel = fallbackModel
						compactModelFallbackRetried = true
						SetOpsUpstreamModel(c, fallbackModel)
						continue
					}
					if resp.Body != nil {
						_ = resp.Body.Close()
					}
					compactResp, compactBody := openAICompactFallbackErrorResponse(resp, signal)
					if s.shouldFailoverOpenAIUpstreamResponse(account, compactResp.StatusCode, signal.message, compactBody) {
						appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
							ProxyID:            opsUpstreamProxyID(account),
							ProxyName:          opsUpstreamProxyName(account),
							Platform:           account.Platform,
							AccountID:          account.ID,
							AccountName:        account.Name,
							UpstreamStatusCode: compactResp.StatusCode,
							UpstreamRequestID:  compactResp.Header.Get("x-request-id"),
							Kind:               "failover",
							Message:            signal.message,
						})
						shouldDisable := s.handleFailoverSideEffects(ctx, compactResp, account, compactBody, upstreamModel)
						return nil, s.newOpenAIAccountFailoverError(
							account, compactResp.StatusCode, compactResp.Header, compactBody, signal.message, shouldDisable,
							!shouldDisable && account.IsPoolMode() && (account.IsPoolModeRetryableStatus(compactResp.StatusCode) || isOpenAITransientProcessingError(compactResp.StatusCode, signal.message, compactBody)),
						)
					}
					return s.handleErrorResponse(ctx, compactResp, c, account, body, resolveOpenAIErrorSchedulingModel(billingModel, upstreamModel))
				}
				if streamResult == nil || (!openAIUsageHasTokens(streamResult.usage) && streamResult.imageCount == 0 && streamResult.searchCount == 0) {
					return nil, err
				}
				responseErr = stopOpenAIMeteredReplay(err)
			}
			clientDisconnected = streamResult.clientDisconnected
			usage = streamResult.usage
			firstTokenMs = streamResult.firstTokenMs
			responseID = strings.TrimSpace(streamResult.responseID)
			imageCount = streamResult.imageCount
			imageOutputSizes = streamResult.imageOutputSizes
			searchCount = streamResult.searchCount
		} else {
			nonStreamResult, err := s.handleNonStreamingResponse(ctx, resp, c, account, originalModel, upstreamModel)
			if err != nil {
				if signal, ok := asOpenAICompactFallbackSignal(err); ok {
					if retryBody, fallbackModel, retry := s.prepareOpenAICompactFallbackRetry(
						c, account, requestedModel, body, http.StatusBadRequest, signal.message, signal.payload, compactModelFallbackRetried,
					); retry {
						s.appendOpenAICompactFallbackRetryOps(c, account, resp, signal.payload, signal.message, false)
						body = retryBody

						upstreamModel = fallbackModel
						compactModelFallbackRetried = true
						SetOpsUpstreamModel(c, fallbackModel)
						continue
					}
				}
				if nonStreamResult == nil || (!openAIUsageHasTokens(nonStreamResult.usage) && nonStreamResult.imageCount == 0 && nonStreamResult.searchCount == 0) {
					return nil, err
				}
				responseErr = stopOpenAIMeteredReplay(err)
			}
			clientDisconnected = nonStreamResult.clientDisconnected
			usage = nonStreamResult.usage
			responseID = strings.TrimSpace(nonStreamResult.responseID)
			imageCount = nonStreamResult.imageCount
			imageOutputSizes = nonStreamResult.imageOutputSizes
			searchCount = nonStreamResult.searchCount
		}
		if responseErr == nil {
			s.bindHTTPResponseAccount(ctx, c, account, responseID)
		}

		// Extract and save Codex usage snapshot from response headers (for OAuth accounts).
		// 排除 spark 影子:其 codex_* 仅由 QueryUsage(/wham/usage bengalfox)更新(外审第7轮 P1)。
		if account.UsesOpenAICodexProtocol() && !account.IsShadow() {
			if snapshot := ParseCodexRateLimitHeaders(resp.Header); snapshot != nil {
				s.updateCodexUsageSnapshot(ctx, account.ID, snapshot)
			}
		} else if account.IsShadow() && account.ParentAccountID != nil {
			notifyOpenAIAutoReset(*account.ParentAccountID)
		}

		if usage == nil {
			usage = &OpenAIUsage{}
		}

		forwardResult := &OpenAIForwardResult{
			AuthorizationMode:             nativeOpenAIUsageAuthorizationMode(account),
			RequestID:                     resp.Header.Get("x-request-id"),
			UpstreamHeaders:               resp.Header,
			ResponseID:                    responseID,
			Usage:                         *usage,
			Model:                         originalModel,
			BillingModel:                  billingModel,
			UpstreamModel:                 upstreamModel,
			UpstreamResponseModel:         observedUpstreamResponseModel(c),
			UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
			UpstreamResponseServiceTier:   observedUpstreamResponseServiceTier(c),
			ServiceTier:                   resolvedOpenAIUpstreamServiceTier(c, serviceTier),
			ReasoningEffort:               reasoningEffort,
			Stream:                        reqStream,
			ClientDisconnect:              clientDisconnected,
			OpenAIWSMode:                  false,
			Duration:                      time.Since(startTime),
			FirstTokenMs:                  firstTokenMs,
		}
		if imageCount > 0 {
			forwardResult.ImageCount = imageCount
			forwardResult.ImageSize = imageSizeTier
			forwardResult.ImageInputSize = imageInputSize
			forwardResult.ImageOutputSizes = imageOutputSizes
			forwardResult.BillingModel = imageBillingModel
		}
		// Grok-native web_search / x_search / tool_search tool invocations (per-1k pricing).
		// Token cost still applies separately when usage is present; search is additive only
		// when search_price_per_1k is configured (nil price → $0 from CalculateSearchCost).
		if searchCount > 0 && account != nil && account.IsGrok() {
			forwardResult.SearchCount = searchCount
		}
		stampOpenAIResponsesUpstreamEndpoint(c, forwardResult)
		return forwardResult, responseErr
	}
}

func shouldForwardOpenAIResponsesViaRawChatCompletions(account *Account) bool {
	if account == nil || account.Type != AccountTypeAPIKey {
		return false
	}
	if account.routesByModel() {
		// Model protocol_rules are the authority. Probe Extra must not collapse
		// Grok/GPT/Muse into Chat Completions.
		return false
	}
	if account.RoutesProtocolByInbound() {
		// 按入站协议分流的供应商（国产厂商等）：显式协议配置优先于异步探针
		// Extra；adaptive 仅在供应商有原生 Responses 端点时直转，否则回退
		// Chat Completions（如 GLM）。
		switch account.GetAPIProtocol() {
		case APIProtocolChatCompletions:
			return true
		case APIProtocolAdaptive:
			return !account.SupportsNativeCNResponses()
		default:
			return false
		}
	}
	return !openai_compat.ShouldUseResponsesAPI(account.Extra)
}

func (s *OpenAIGatewayService) buildUpstreamRequest(ctx context.Context, c *gin.Context, account *Account, body []byte, token string, isStream bool, promptCacheKey string, isCodexCLI bool) (*http.Request, error) {
	// Determine target URL based on account type
	var targetURL string
	switch account.Type {
	case AccountTypeOAuth:
		// OAuth accounts use ChatGPT internal API
		targetURL = chatgptCodexURL
	case AccountTypeSetupToken:
		if account.IsOpenAIOAuthLike() {
			targetURL = chatgptCodexURL
		} else {
			targetURL = openaiPlatformAPIURL
		}
	case AccountTypeAPIKey:
		// API Key accounts use Platform API or custom base URL
		baseURL := account.GetOpenAIBaseURL()
		if account.UsesNativeCNResponses() && account.IsAdaptiveAPIProtocol() {
			baseURL = account.GetCNProtocolBaseURL(APIProtocolResponses)
		}
		if baseURL == "" {
			targetURL = openaiPlatformAPIURL
		} else {
			validatedURL, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return nil, err
			}
			targetURL = buildOpenAIResponsesURLForPlatform(account.Platform, validatedURL)
		}
	default:
		targetURL = openaiPlatformAPIURL
	}
	if !isOpenAIPromotedCompactWire(c) {
		targetURL = appendOpenAIResponsesRequestPathSuffix(targetURL, openAIResponsesRequestPathSuffix(c))
	}

	// DeepSeek / Kimi 原生 Responses 端点为无状态实现：强制 store=false、清除
	// previous_response_id，避免携带状态字段被上游拒绝。
	body = normalizeDeepSeekResponsesRequestBody(account, body)
	if account.IsOpenAI() && !isOpenAIResponsesCompactPath(c) {
		if err := validateOpenAIResponsesInput(body); err != nil {
			return nil, err
		}
	}
	// Account policy can add or remove hosted tools after Forward's initial
	// snapshot. Every attempt must use the final body, including plugin sends.
	setOpenAIReplaySafety(c, body)

	logGatewayCacheTransform(ctx, account, "", body)
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))

	// Build authentication for this request. Agent Identity signs a fresh
	// assertion here; OAuth/PAT/API-key keep their existing Bearer behavior.
	authHeaders, err := s.buildOpenAIAuthenticationHeaders(ctx, account, token)
	if err != nil {
		return nil, fmt.Errorf("build openai authentication headers: %w", err)
	}
	for key, values := range authHeaders {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// This resolution still contains original client values, not rebuilt headers.
	sessionFallback := resolveOpenAIWSSessionHeaders(c, promptCacheKey).SessionID
	// Set headers specific to OAuth accounts (ChatGPT internal API)
	if account.UsesOpenAICodexProtocol() {
		// Required: set Host for ChatGPT API (must use req.Host, not Header.Set)
		req.Host = "chatgpt.com"
		if err := resolveAndSetOpenAIChatGPTAccountHeaders(ctx, s.accountRepo, req.Header, account); err != nil {
			return nil, fmt.Errorf("resolve chatgpt account headers: %w", err)
		}
	}

	// Whitelist passthrough headers
	for key, values := range c.Request.Header {
		lowerKey := strings.ToLower(key)
		if openaiAllowedHeaders[lowerKey] {
			for _, v := range values {
				req.Header.Add(key, v)
			}
		}
	}
	copyCodexCallerIntentHeaders(req.Header, c.Request.Header, account)
	// 客户端回带的 x-codex-turn-state 若已知由其他账号铸造（failover 换号），
	// 剥离后再出站——异账号 blob 与本账号的（指纹收敛后）出站身份自相矛盾。
	s.guardOpenAICodexTurnStateEcho(c, account, req.Header)
	if account.UsesOpenAICodexProtocol() {
		compatMessagesBridge := isOpenAICompatMessagesBridgeContext(c) || isOpenAICompatMessagesBridgeBody(body)
		// 清除客户端透传的 session 头，后续用隔离后的值重新设置，防止跨用户会话碰撞。
		clientConversationID := strings.TrimSpace(req.Header.Get("conversation_id"))
		req.Header.Del("conversation_id")
		req.Header.Del("session_id")

		if compatMessagesBridge {
			req.Header.Del("OpenAI-Beta")
			req.Header.Del("originator")
		} else {
			stripOpenAILegacyResponsesBeta(req.Header)
			req.Header.Set("originator", resolveOpenAIUpstreamOriginator(c, isCodexCLI))
		}
		apiKeyID := getAPIKeyIDFromContext(c)
		if isOpenAIResponsesCompactPath(c) {
			req.Header.Set("accept", "application/json")
			if isOpenAIPromotedCompactWire(c) {
				req.Header.Set("accept", "text/event-stream")
				ensureOpenAIRemoteCompactionV2BetaFeature(req.Header)
			}
			if req.Header.Get("version") == "" {
				req.Header.Set("version", CodexCanonicalClientVersion())
			}
			compactSession := resolveOpenAICompactSessionID(c)
			if sessionFallback == "" {
				sessionFallback = compactSession
			}
			req.Header.Set("session_id", isolateOpenAIUpstreamSessionID(apiKeyID, codexAccountIdentitySource(c, account), compactSession))
		} else {
			req.Header.Set("accept", "text/event-stream")
		}
		if promptCacheKey != "" {
			isolated := isolateOpenAIUpstreamSessionID(apiKeyID, codexAccountIdentitySource(c, account), promptCacheKey)
			req.Header.Set("session_id", isolated)
			if !compatMessagesBridge || clientConversationID != "" {
				req.Header.Set("conversation_id", isolated)
			}
		}
	} else if isOpenAIResponsesCompactPath(c) {
		// compact 上游是 unary JSON 协议：API-key 账号也显式声明 Accept，
		// 避免 OpenAI 兼容网关按 SSE 返回（#3777 期望行为 4）。
		req.Header.Set("accept", "application/json")
	}

	// Apply custom User-Agent if configured
	customUA := account.GetOpenAIUserAgent()
	if customUA != "" {
		req.Header.Set("user-agent", customUA)
	}

	// 若开启 ForceCodexCLI，则强制将上游 User-Agent 伪装为规范 Codex 身份。
	// 用于网关未透传/改写 User-Agent 时，仍能命中 Codex 侧识别逻辑。
	if s.cfg != nil && s.cfg.Gateway.ForceCodexCLI {
		req.Header.Set("user-agent", CodexCanonicalUserAgent())
	}

	// 账号 namespace 不改变客户端身份基数，但确保 scheduler failover 后不会把
	// 同一组 Codex IDs 发送给另一份 OAuth 凭据。可选指纹收敛随后仍可覆盖这些值。
	applyCodexAccountIdentityHeaders(req.Header, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c))

	// 身份迁移第 1 步：补齐官方形态的 session-id / thread-id / x-client-request-id。
	// 取的是 body 里**已经作用域化**的 client_metadata（本函数收到的 body 是
	// Forward 在作用域化之后重新编码的结果），不做二次派生，因此与 body 同值。
	// 必须在 applyCodexAccountIdentityHeaders 之后——否则刚写的值会被它再 scope 一次；
	// 也必须在指纹收敛之前——收敛是管理员显式配置，应当覆盖这里的默认值。
	// 旧的下划线 session_id / conversation_id 保持既有取值不变。
	applyCodexOfficialSessionHeadersFromScopedBody(req.Header, codexAccountIdentitySource(c, account), body)
	applyCodexOfficialSessionHeadersFromScopedTurnMetadata(req.Header, codexAccountIdentitySource(c, account))
	applyCodexOfficialSessionHeaderFallback(req.Header, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c), sessionFallback)

	// 指纹收敛：使用 Forward() 中预计算的收敛 ID 改写出站头，与请求体使用同一份 IDs。
	applyStagedCodexFingerprintHeaders(c, account, req.Header)

	// 终态收口：强制统一 OAuth 出站身份（User-Agent / originator / version 同源自洽）。
	// 客户端自报身份不参与构造，浏览器型 UA 也因此不会再到达上游（原浏览器 UA 兜底已被吸收）。
	if account.UsesOpenAICodexProtocol() {
		enforceCodexIdentityHeadersWithUA(req.Header, s.codexIdentityOverrideUA(account))
	}

	// Ensure required headers exist
	if req.Header.Get("content-type") == "" {
		req.Header.Set("content-type", "application/json")
	}

	// 官方 OpenCode / Command Code 上游收敛为规范客户端 UA：客户端透传的编程库
	// UA 会命中其前置 Cloudflare bot 拦截（CF 1010/403），并被计入账号 403 strike。
	applyOpenCodeUpstreamUserAgent(account, targetURL, req.Header)

	// 账号级请求头覆写（仅 openai api_key 账号启用时生效；OAuth 路径 no-op）
	account.ApplyHeaderOverrides(req.Header)
	applyOpenCodeSessionHeader(c, account, targetURL, req.Header, body, openCodeSessionHintBody(promptCacheKey))
	// x-codex-beta-features：按真实 Codex 的会话级行为补注（在账号级覆写之后，
	// 保证不被覆盖丢失）。
	applyOpenAICodexBetaFeatures(c, account, req.Header)
	setOpenAICodexRoutingHintFromBody(req.Header, account, body)
	logOpenAIRoutingDiagnosticsFromBody(ctx, account, "http", req.Header, body, "not_applicable")

	metadataBody, metadataErr := applyCodexRequestMetadata(req, body, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c), isOpenAIResponsesCompactPath(c) && !isOpenAIPromotedCompactWire(c))
	if metadataErr != nil {
		return nil, metadataErr
	}
	body = metadataBody
	projectCodexOutboundHeaders(req.Header, codexAccountIdentitySource(c, account), false)

	if err := applyMappedGPT55LiteCompatibility(req, account, body); err != nil {
		return nil, err
	}
	return req, nil
}

// codexIdentityOverrideUA 返回账号级显式配置的出站 User-Agent，供强制统一身份时作为覆写来源。
// ForceCodexCLI 语义是「强制使用 Codex CLI 身份」，等价于使用网关规范身份，故返回空串；
// 该优先级与历史行为一致（ForceCodexCLI 在账号自定义 UA 之后生效）。
func (s *OpenAIGatewayService) codexIdentityOverrideUA(account *Account) string {
	if s != nil && s.cfg != nil && s.cfg.Gateway.ForceCodexCLI {
		return ""
	}
	return account.GetOpenAIUserAgent()
}
