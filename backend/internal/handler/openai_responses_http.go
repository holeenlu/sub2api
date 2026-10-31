package handler

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	gatewaymodel "github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (h *OpenAIGatewayHandler) Responses(c *gin.Context) {
	service.BeginOpenAIRequestTiming(c)
	// 局部兜底：确保该 handler 内部任何 panic 都不会击穿到进程级。
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)
	compactStartedAt := time.Now()
	defer h.logOpenAIRemoteCompactOutcome(c, compactStartedAt)
	setOpenAIClientTransportHTTP(c)

	requestStart := time.Now()

	// Get apiKey and user from context (set by ApiKeyAuth middleware)
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.responses",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	// Read request body
	body, originalWireDigest, err := readNativeHTTPOperationBody(c.Request, h.cfg)
	if err != nil {
		logRequestBodyReadFailure(reqLog, c.Request, err)
		writeRequestBodyReadError(c, err, func(status int, errType, message string) {
			h.errorResponse(c, status, errType, message)
		})
		return
	}

	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	setOpsRequestContext(c, "", false)
	sessionHashBody := body
	body, ok = h.normalizeOpenAIResponsesCompactRequest(c, reqLog, body)
	if !ok {
		return
	}
	legacyCompact := service.IsOpenAIResponsesCompactPath(c)
	nativeV2 := isBareOpenAIResponsesPath(c) && isOpenAIRemoteCompactionV2Request(body)
	if nativeV2 {
		// 原生 v2 压缩出站前补注 x-codex-beta-features: remote_compaction_v2，
		// 与真实 Codex 线型一致（网关链剥头后本级负责恢复，#5586）。
		service.MarkOpenAINativeCompactionV2(c)
	}
	// body-signal compact：上游 unary 等待期间向下游发 SSE 注释行心跳，防止
	// 反向代理空闲超时掐断长压缩连接（#3887）。首拍延迟一个心跳间隔，快速
	// 失败仍走 JSON+状态码链路；未标记客户端流式或间隔为 0 时是 no-op。
	stopCompactKeepalive := service.StartOpenAICompactSSEKeepalive(c, h.openAICompactKeepaliveInterval())
	defer stopCompactKeepalive()

	// 校验请求体 JSON 合法性
	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	// 使用 gjson 只读提取字段做校验，避免完整 Unmarshal
	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	cloudGroupID := int64(0)
	if apiKey.GroupID != nil {
		cloudGroupID = *apiKey.GroupID
	}
	cloudContext, cloudErr := h.gatewayService.PrepareCloudContinuation(c.Request.Context(), subject.UserID, apiKey.ID, cloudGroupID, body)
	if cloudErr != nil {
		if cloudContext != nil {
			c.Request = c.Request.WithContext(cloudContext)
		}
		if service.WriteOpenAICloudAdaptationError(c, cloudErr) {
			return
		}
		if status, code, message, ok := service.OpenAICloudContinuationError(cloudErr); ok {
			c.JSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "code": code, "message": message}})
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", cloudErr.Error())
		return
	}
	c.Request = c.Request.WithContext(cloudContext)
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !openAICompatibleTextTargetAllowed(c, apiKey, reqModel) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}
	if cappedBody, changed, err := applyOpenAIReasoningEffortPolicyForRequest(c, apiKey, body); err != nil {
		respondOpenAIReasoningEffortPolicyError(c, err, h.errorResponse)
		return
	} else if changed {
		body = cappedBody
		service.SyncCloudReasoningEffort(c.Request.Context(), gjson.GetBytes(cappedBody, "reasoning.effort").String())
	}
	if normalizedBody, changed := normalizeCodexAutomationBootstrap(body); changed && !service.HasOpenAICloudContinuation(c.Request.Context()) {
		body = normalizedBody
		reqLog.Info("openai.codex_automation_bootstrap_normalized",
			zap.String("normalization", "call_output_to_user_message"),
		)
	}
	if normalizedBody, changed := normalizeCodexDelegationBootstrap(body); changed && !service.HasOpenAICloudContinuation(c.Request.Context()) {
		body = normalizedBody
		reqLog.Info("openai.codex_delegation_bootstrap_normalized",
			zap.String("normalization", "call_output_to_user_message"),
		)
	}

	reqStream, ok := parseOpenAICompatibleStream(body)
	if !ok {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
		return
	}
	if _, err := service.ValidateOpenAIServiceTierField(body); err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))
	previousResponseID := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String())
	if previousResponseID != "" {
		previousResponseIDKind := service.ClassifyOpenAIPreviousResponseIDKind(previousResponseID)
		reqLog = reqLog.With(
			zap.Bool("has_previous_response_id", true),
			zap.String("previous_response_id_kind", previousResponseIDKind),
			zap.Int("previous_response_id_len", len(previousResponseID)),
		)
		if previousResponseIDKind == service.OpenAIPreviousResponseIDKindMessageID {
			reqLog.Warn("openai.request_validation_failed",
				zap.String("reason", "previous_response_id_looks_like_message_id"),
			)
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "previous_response_id must be a response.id (resp_*), not a message id")
			return
		}
		groupID := int64(0)
		if apiKey.GroupID != nil {
			groupID = *apiKey.GroupID
		}
		owned, ownershipErr := h.gatewayService.ValidateOpenAIHTTPResponseOwner(
			c.Request.Context(),
			groupID,
			previousResponseID,
			subject.UserID,
			apiKey.ID,
		)
		if ownershipErr != nil {
			reqLog.Warn("openai.previous_response_owner_lookup_failed", zap.Error(ownershipErr))
		}
		if !owned {
			reqLog.Warn("openai.request_validation_failed", zap.String("reason", "previous_response_owner_mismatch"))
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "previous_response_id is not available for this user")
			return
		}
	}
	service.SetOpenAIHTTPResponseOwner(c, subject.UserID, apiKey.ID)

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}

	// 使用 IsExplicitImageGenerationIntent 排除被动 image_gen namespace 声明。
	// Codex 在所有请求中被动声明 image_gen namespace，宽泛检测会导致禁了生图的
	// 分组中所有 Codex 请求被 403（#4447），并误占生图并发槽位。
	imageIntent := service.IsExplicitImageGenerationIntent("/v1/responses", reqModel, body)
	c.Request = c.Request.WithContext(service.WithOpenAIImageGenerationRequest(c.Request.Context(), reqModel, body))
	if imageIntent && !service.GroupAllowsImageGeneration(apiKey.Group) {
		h.errorResponse(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return
	}
	var imageReleaseFunc func()
	if imageIntent {
		var imageAcquired bool
		imageReleaseFunc, imageAcquired = h.acquireImageGenerationSlot(c, streamStarted)
		if !imageAcquired {
			return
		}
		if imageReleaseFunc != nil {
			defer imageReleaseFunc()
		}
	}

	// 解析渠道级模型映射
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
	forwardBody := openAIModelMappedBody(body, channelMapping.Mapped, channelMapping.MappedModel, h.gatewayService.ReplaceModelInBody)
	seedOpenAIForwardImageIntentHint(c, channelMapping.Mapped, imageIntent)
	forwardModel := openAIChannelForwardModel(channelMapping, reqModel)
	c.Request = c.Request.WithContext(service.WithOpenAIForwardModel(
		c.Request.Context(),
		forwardModel,
		legacyCompact,
	))

	// 提前校验 function_call_output 是否具备可关联上下文，避免上游 400。
	if !h.validateFunctionCallOutputRequest(c, body, reqLog) {
		return
	}

	// 绑定错误透传服务，允许 service 层在非 failover 错误场景复用规则。
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	// Get subscription info (may be nil)
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	requestPlatform := openAICompatibleRequestPlatform(c.Request.Context(), apiKey)
	c.Request = c.Request.WithContext(service.EnsureOpenAITurnBudget(c.Request.Context(), h.cfg))
	if requestPlatform == service.PlatformOpenAI && reqStream {
		c.Request = c.Request.WithContext(service.EnsureOpenAITurnBudget(c.Request.Context(), h.cfg, gjson.GetBytes(body, "reasoning.effort").String()))
	}

	if err := h.bindNativeHTTPOperation(c, apiKey, reqModel, gatewaymodel.ProtocolResponses, originalWireDigest); err != nil {
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Native dispatch dependencies are unavailable", streamStarted)
		return
	}

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted, reqLog, service.NativeHTTPOperationBound(c.Request.Context()))
	if !acquired {
		return
	}
	// 确保请求取消时也会释放槽位，避免长连接被动中断造成泄漏
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2. Re-check billing eligibility after wait
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("openai.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	// 余额模式在途预留：防止并发请求在预检时看到同一份余额而集体透支。
	inflightRelease, err := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, tokenInflightEstimate(reqModel, body))
	if err != nil {
		reqLog.Info("openai.inflight_reservation_rejected", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}
	defer inflightRelease()

	// Generate session hash (header first; fallback to prompt_cache_key)
	sessionHash := h.gatewayService.GenerateSessionHash(c, sessionHashBody)
	if h.rejectIfCyberSessionBlocked(c, apiKey, sessionHashBody, reqModel, cyberBlockFormatResponses) {
		return
	}
	c.Request = c.Request.WithContext(service.WithOpenAIGuardianParentAffinity(
		c.Request.Context(), c, sessionHashBody, reqModel,
	))
	requireCompact := legacyCompact

	c.Request = c.Request.WithContext(service.EnsureOpenAITurnBudget(c.Request.Context(), h.cfg))
	maxAccountSwitches := h.maxAccountSwitches
	recovery := NewFailoverState(maxAccountSwitches, false)
	firstOutputTimeoutSwitchCount := 0

	// 生图意图的 /v1/responses 请求必须调度到确实支持 Responses API 的账号，否则
	// 会在 forward 阶段被静默降级为无法生图的 Chat Completions 直转（#4417）。
	// 仅对 OpenAI 平台生效：Grok 生图走独立的 forwardGrokResponses 路径，不应被过滤。
	// 复用前置权限与并发阶段在未修改 body 上确认的显式生图意图，避免大 tools 请求重复扫描。
	// 该判断已排除 Codex 被动 image_gen namespace，避免 CC-only 账号被误过滤（#4476）。
	needsResponses := nativeV2 || legacyCompact
	requiredCapability := openAIResponsesRequiredCapabilityForRequest(imageIntent, needsResponses, requestPlatform)

	// 分组利润控制：请求级装配定价上下文——pricingAt 固定本请求的
	// D 与计费高峰因子，选号、槽位终检与全部 failover 重入共用同一门与阈值。
	// 生图意图只影响能力路由与图片计费，不关门：混合 /v1/responses 请求的
	// token 计费部分仍受利润门保护，独立图片/视频端点才在门外。
	pricingCtx, pricingAt := h.gatewayService.WithOpenAIRequestPricingContext(c.Request.Context(), apiKey.GroupID)
	c.Request = c.Request.WithContext(pricingCtx)
	if err := h.gatewayService.BindNativeIntentPricing(c.Request.Context(), service.NativeIntentPricingInput{
		APIKey: apiKey, User: apiKey.User, Subscription: subscription, APIKeyService: h.apiKeyService, PricingAt: pricingAt,
		ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, ""),
		QuotaPlatform:      service.QuotaPlatform(c.Request.Context(), apiKey), RequestPayloadHash: service.HashUsageRequestPayload(body),
	}); err != nil {
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Native pricing intent could not be established", streamStarted)
		return
	}

	operationErr := service.RunNativeHTTPOperation(c.Request.Context(), c, func() service.NativeOperationStep {
		// Streaming Forward intentionally detaches the upstream request so usage can
		// be drained after a disconnect. Re-check the client context before every
		// account attempt so a canceled request never starts a failover replay.
		if !openAIRequestAllowsFailoverReplay(c) {
			return service.NativeOperationDone()
		}
		// Select account supporting the requested model
		reqLog.Debug("openai.account_selecting", zap.Int("excluded_account_count", len(recovery.FailedAccountIDs)))
		selection, scheduleDecision, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(),
			apiKey.GroupID,
			previousResponseID,
			sessionHash,
			forwardModel,
			recovery.FailedAccountIDs,
			service.OpenAIUpstreamTransportAny,
			requiredCapability,
			requireCompact,
			false,
			!imageIntent,
			requestPlatform,
		)
		if err != nil {
			if errors.Is(err, service.ErrOpenAIContinuationUnavailable) {
				service.WriteOpenAIContinuationError(c)
				return service.NativeOperationDone()
			}
			if action, handled := recovery.RecoverOpenAISelection(c.Request.Context(), c, err, body, h.gatewayService, reqLog); handled {
				if action == execution.RecoveryContinue {
					return service.NativeOperationContinue()
				}
				h.handleFailoverExhausted(c, recovery.LastFailoverErr, streamStarted)
				return service.NativeOperationDone()
			}
			if h.handleOpenAITurnBudgetError(c, err, streamStarted) {
				return service.NativeOperationDone()
			}
			if failoverClientGone(c) {
				reqLog.Info("openai.account_select_aborted_client_disconnected", zap.Error(err))
				return service.NativeOperationDone()
			}
			reqLog.Warn("openai.account_select_failed",
				zap.Error(openAICompatibleSelectionErrorForLog(err, requestPlatform)),
				zap.Int("excluded_account_count", len(recovery.FailedAccountIDs)),
			)
			if (len(recovery.FailedAccountIDs) == 0 && recovery.LastFailoverErr == nil) || (!errors.Is(err, service.ErrNoAvailableAccounts) && !errors.Is(err, service.ErrNoAvailableCompactAccounts)) {
				if legacyCompact && errors.Is(err, service.ErrNoAvailableCompactAccounts) {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
					h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "compact_not_supported", "No available accounts support /responses/compact", streamStarted)
					return service.NativeOperationDone()
				}
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, forwardModel, reqModel, requestPlatform)
				cls = classifySelectionFailureError(err, cls)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
				return service.NativeOperationDone()
			}
			if recovery.LastFailoverErr != nil {
				service.RecordOpenAIRetryStop(c, nil, service.OpenAIRetryStopNoAvailableAccount)
				h.handleFailoverExhausted(c, recovery.LastFailoverErr, streamStarted)
			} else {
				h.handleFailoverExhaustedSimple(c, 502, streamStarted)
			}
			return service.NativeOperationDone()
		}
		if selection == nil || selection.Account == nil {
			if recovery.LastFailoverErr != nil {
				service.RecordOpenAIRetryStop(c, nil, service.OpenAIRetryStopNoAvailableAccount)
			}
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, forwardModel, reqModel, requestPlatform)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
			return service.NativeOperationDone()
		}
		if previousResponseID != "" && selection != nil && selection.Account != nil {
			reqLog.Debug("openai.account_selected_with_previous_response_id", zap.Int64("account_id", selection.Account.ID))
		}
		reqLog.Debug("openai.account_schedule_decision",
			zap.String("layer", scheduleDecision.Layer),
			zap.Bool("sticky_previous_hit", scheduleDecision.StickyPreviousHit),
			zap.Bool("sticky_session_hit", scheduleDecision.StickySessionHit),
			zap.Int("candidate_count", scheduleDecision.CandidateCount),
			zap.Int("top_k", scheduleDecision.TopK),
			zap.Int64("latency_ms", scheduleDecision.LatencyMs),
			zap.Float64("load_skew", scheduleDecision.LoadSkew),
		)
		account := selection.Account
		if previousResponseID != "" && requestPlatform == service.PlatformOpenAI && !account.IsOpenAIApiKey() &&
			!h.gatewayService.IsExcelBPSRouteEnabled(c.Request.Context(), account, forwardModel) &&
			!(account.IsOpenAICloudEnabled() && service.HasOpenAICloudContinuation(c.Request.Context())) {
			// The public Responses HTTP API supports previous_response_id on API-key
			// accounts; the actual BPS route expands authenticated history locally.
			// Other OAuth/SetupToken upstreams do not, so keep searching instead
			// of silently deleting continuation state from a mixed account pool.
			service.PinTurnRetryAccount(c.Request.Context(), nil)
			recovery.FailedAccountIDs[account.ID] = struct{}{}
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
				selection.ReleaseFunc = nil
			}
			recovery.LastFailoverErr = &service.UpstreamFailoverError{
				StatusCode:       http.StatusBadRequest,
				Stage:            service.GatewayFailureStageInference,
				Scope:            service.GatewayFailureScopeRequest,
				Reason:           service.OpenAIHTTPContinuationUnsupportedReason,
				ClientStatusCode: http.StatusBadRequest,
				ClientMessage:    "previous_response_id requires an OpenAI API-key account for HTTP requests",
			}
			reqLog.Debug("openai.account_skipped_http_continuation_unsupported",
				zap.Int64("account_id", account.ID),
				zap.String("account_type", account.Type),
			)
			return service.NativeOperationContinue()
		}
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		reqLog.Debug("openai.account_selected", zap.Int64("account_id", account.ID), zap.String("account_name", account.Name))
		setOpsSelectedAccount(c, account.ID, account.Platform)

		accountReleaseFunc, slotResult := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, reqStream, &streamStarted, reqLog, service.NativeHTTPOperationBound(c.Request.Context()))
		if slotResult == openAISlotAcquireProfitVetoed {
			// 利润终检否决：排除该账号重新选号，全池耗尽由下一轮选号报错；
			// 否决次数达上限则直接终止，避免排队抢槽后才终检的延迟放大。
			if recovery.RecordProfitVeto(account.ID) == execution.RecoveryExhausted {
				h.handleOpenAIProfitVetoExhausted(c, streamStarted, reqLog, recovery.ProfitVetoCount())
				return service.NativeOperationDone()
			}
			return service.NativeOperationContinue()
		}
		if slotResult != openAISlotAcquireOK {
			return service.NativeOperationDone()
		}
		accountLease := service.RetainNativeHTTPRelease(accountReleaseFunc)
		defer accountLease.Release()

		// Forward request
		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()
		// 用扣除非语义心跳字节的口径快照：心跳注释不构成语义响应，
		// 不能因心跳字节变化而放弃 failover 换号（#3887）。
		writerSizeBeforeForward := service.OpenAICompactKeepaliveAdjustedWrittenSize(c)
		// 跨 passthrough 边界的 failover：从 Kiro 等透传账号切到 Bedrock 等非透传账号前，
		// 从不可变的 canonical forwardBody 派生本次尝试 body 并整块剔除上游私有的加密
		// reasoning item（含耦合的 id/summary），避免非透传上游 400 拒绝 Kiro reasoning 形态。
		attemptBody := forwardBody
		if account.IsOpenAICloudEnabled() {
			tier, effort := gjson.GetBytes(attemptBody, "service_tier").String(), gjson.GetBytes(attemptBody, "reasoning.effort").String()
			billingResult := &service.OpenAIForwardResult{Model: reqModel, UpstreamModel: account.GetMappedModel(forwardModel), ServiceTier: &tier, ReasoningEffort: &effort, Stream: reqStream}
			stampOpenAIRequestedReasoningEffort(billingResult, c)
			billingCtx, captureErr := h.gatewayService.CaptureCloudBillingSnapshot(c.Request.Context(), &service.OpenAIRecordUsageInput{
				Result: billingResult, APIKey: apiKey, User: apiKey.User, Account: account, Subscription: subscription,
				InboundEndpoint: GetInboundEndpoint(c), UserAgent: c.GetHeader("User-Agent"), IPAddress: ip.GetClientIP(c),
				SessionID: service.ExtractClientSessionID(c), RequestPayloadHash: service.HashUsageRequestPayload(body),
				QuotaPlatform: service.QuotaPlatform(c.Request.Context(), apiKey), PricingAt: pricingAt, NativeCompactionV2: nativeV2,
				ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, billingResult.UpstreamModel),
			})
			if captureErr != nil {
				accountLease.Release()
				h.errorResponse(c, http.StatusServiceUnavailable, "cloud_billing_unavailable", "Cloud billing could not be prepared")
				return service.NativeOperationDone()
			}
			c.Request = c.Request.WithContext(billingCtx)
		}
		result, err := service.TrackNativeHTTPForward(c.Request.Context(), func() (*service.OpenAIForwardResult, error) {
			return h.gatewayService.Forward(c.Request.Context(), c, account, attemptBody)
		})
		var cyberBlockBodyHTTP []byte
		if service.GetOpsCyberPolicy(c) != nil {
			cyberBlockBodyHTTP = sessionHashBody
		}
		service.TrackNativeHTTPAuthorization(c.Request.Context(), service.ActualOpenAIUsageAuthorizationMode(c))
		nativeUsageOwned := service.NativeHTTPFinalizesUsage(c.Request.Context())
		h.recordCyberPolicyIfMarked(c, apiKey, account, subscription, reqModel, err != nil && result == nil && !nativeUsageOwned, cyberBlockBodyHTTP, clientRequestedUsageFields(c, channelMapping, reqModel, ""), service.HashUsageRequestPayload(body))
		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)
		if err == nil && result != nil && result.FirstTokenMs != nil {
			service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
		}
		// #5148 对齐：错误返回携带的部分 result（流中断前上游已计量的 usage）照常
		// 入账；BPS failover 也可能携带已完成 compact 的独立用量。
		submitResponsesUsage := func(res *service.OpenAIForwardResult) {
			defer accountLease.Release()
			stampOpenAIRequestedReasoningEffort(res, c)
			userAgent := c.GetHeader("User-Agent")
			clientIP := ip.GetClientIP(c)
			requestPayloadHash := service.HashUsageRequestPayload(body)
			inboundEndpoint := GetInboundEndpoint(c)
			upstreamEndpoint := resolveOpenAIUpstreamEndpoint(c, account, res)
			quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
			sessionID := service.ExtractClientSessionID(c)
			cyberBlocked := service.GetOpsCyberPolicy(c) != nil
			upstreamModel := ""
			if res != nil {
				upstreamModel = res.UpstreamModel
			}
			usageFields := clientRequestedUsageFields(c, channelMapping, reqModel, upstreamModel)
			usageInput := &service.OpenAIRecordUsageInput{
				Result:             res,
				APIKey:             apiKey,
				User:               apiKey.User,
				Account:            account,
				Subscription:       subscription,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIP,
				RequestPayloadHash: requestPayloadHash,
				APIKeyService:      h.apiKeyService,
				QuotaPlatform:      quotaPlatform,
				SessionID:          sessionID,
				ChannelUsageFields: usageFields,
				PricingAt:          pricingAt,
				CyberBlocked:       cyberBlocked,
				NativeCompactionV2: nativeV2,
			}
			if h.finalizeNativeHTTPOperationUsage(c, usageInput) {
				return
			}
			if res == nil {
				return
			}

			h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
				if err := h.gatewayService.RecordUsage(ctx, usageInput); err != nil {
					logger.L().With(
						zap.String("component", "handler.openai_gateway.responses"),
						zap.Int64("user_id", subject.UserID),
						zap.Int64("api_key_id", apiKey.ID),
						zap.Any("group_id", apiKey.GroupID),
						zap.String("model", reqModel),
						zap.Int64("account_id", account.ID),
					).Error("openai.record_usage_failed", zap.Error(err))
				}
			})
		}
		if err != nil {
			if h.writeNativeHTTPDispatchError(c, err, streamStarted, false) {
				submitResponsesUsage(result)
				return service.NativeOperationDone()
			}
			if errors.Is(err, service.ErrOpenAITurnAttemptBudgetExhausted) || errors.Is(err, service.ErrOpenAIWSFirstOutputBudgetExhausted) {
				// BPS may have already metered image compaction before this send limit.
				submitResponsesUsage(result)
				service.RecordOpenAIRetryStop(c, account, service.OpenAIRetryStopTurnBudget)
				h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "retry_budget_exhausted", err.Error(), streamStarted)
				return service.NativeOperationDone()
			}
			if result != nil && result.ClientDisconnect {
				reqLog.Info("openai.client_disconnected",
					zap.Int64("account_id", account.ID),
					zap.Error(err),
				)
				submitResponsesUsage(result)
				return service.NativeOperationDone()
			}
			if failoverClientGone(c) {
				reqLog.Info("openai.client_disconnected",
					zap.Int64("account_id", account.ID),
					zap.Error(err),
				)
				submitResponsesUsage(result)
				return service.NativeOperationDone()
			}
			if result != nil && result.ImageCount > 0 {
				reqLog.Warn("openai.forward_partial_error_with_image_result",
					zap.Int64("account_id", account.ID),
					zap.Int("image_count", result.ImageCount),
					zap.Error(err),
				)
			} else {
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					submitResponsesUsage(result)
					if failoverClientGone(c) {
						reqLog.Info("openai.failover_aborted_client_disconnected",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						return service.NativeOperationDone()
					}
					if !service.OpenAITurnRetryAllowed(c.Request.Context(), account.ID, failoverErr.ResponseBody) {
						service.RecordOpenAIRetryStop(c, account, service.OpenAIRetryStopTurnBudget)
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return service.NativeOperationDone()
					}
					if reason := openAIForwardRetryStopReason(c, writerSizeBeforeForward, failoverErr); reason != "" {
						service.RecordOpenAIRetryStop(c, account, reason)
						h.gatewayService.ObserveOpenAIAccountHealthFailure(c.Request.Context(), account, err)
						h.handleFailoverExhausted(c, failoverErr, true)
						return service.NativeOperationDone()
					}
					// openAIForwardRetryStopReason 已确认写出的字节不含语义输出，
					// 但重试耗尽时仍须按已提交的 SSE 响应返回流内错误。
					if c.Writer.Written() {
						streamStarted = true
					}
					if failoverErr.ShouldReportAccountScheduleFailure() {
						h.gatewayService.ReportOpenAIAccountScheduleFailure(c.Request.Context(), account, openAIAccountScheduleModel(c, account, forwardModel, requireCompact, nil), err)
					}
					if !failoverErr.ShouldRetryNextAccount() && !failoverErr.RetryableOnSameAccount {
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return service.NativeOperationDone()
					}
					if openAIFirstOutputFailoverExhausted(failoverErr, &firstOutputTimeoutSwitchCount) {
						service.RecordOpenAIRetryStop(c, account, service.OpenAIRetryStopTurnBudget)
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return service.NativeOperationDone()
					}
					if finalErr := service.NativeHTTPFinalizationError(c.Request.Context()); finalErr != nil {
						return service.NativeOperationStep{Err: finalErr}
					}
					if len(failoverErr.ContinuationReplayBody) > 0 {
						// The selected BPS path already verified and expanded this
						// history. Retain it on the existing HTTP operation so the
						// next cycle cannot fall back to the original opaque delta.
						forwardBody = failoverErr.ContinuationReplayBody
						body = forwardBody
						previousResponseID = ""
					}
					replayPortable := openAIForwardAccountReplayAllowed(c, account, body, failoverErr)
					action, recoveryErr := recovery.RecoverOpenAI(c.Request.Context(), c, account, failoverErr, replayPortable, true, h.gatewayService, reqLog)
					if recoveryErr != nil {
						h.handleOpenAITurnBudgetError(c, recoveryErr, streamStarted)
						return service.NativeOperationDone()
					}
					if action == execution.RecoveryCanceled {
						return service.NativeOperationDone()
					}
					if action == execution.RecoveryExhausted {
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return service.NativeOperationDone()
					}
					return service.NativeOperationContinue()
				}
				h.gatewayService.ReportOpenAIAccountScheduleFailure(c.Request.Context(), account, openAIAccountScheduleModel(c, account, forwardModel, requireCompact, result), err)
				upstreamErrorAlreadyCommunicated := openAIForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
				wroteFallback := false
				if !upstreamErrorAlreadyCommunicated {
					wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
				}
				fields := []zap.Field{
					zap.Int64("account_id", account.ID),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
					zap.Error(err),
				}
				submitResponsesUsage(result)
				if shouldLogOpenAIForwardFailureAsWarn(c, wroteFallback) {
					reqLog.Warn("openai.forward_failed", fields...)
					return service.NativeOperationDone()
				}
				reqLog.Error("openai.forward_failed", fields...)
				return service.NativeOperationDone()
			}
		}
		if result != nil {
			// 排除 spark 影子:其 codex_* 仅由 QueryUsage(/wham/usage bengalfox)更新(外审第7轮 P1)。
			if account.Type == service.AccountTypeOAuth && !account.IsShadow() {
				h.gatewayService.UpdateCodexUsageSnapshotFromHeaders(c.Request.Context(), account.ID, result.ResponseHeaders)
			}
			reportOpenAICompletion(c.Request.Context(), h.gatewayService, account, openAIAccountScheduleModel(c, account, forwardModel, requireCompact, result), result)
		} else {
			reportOpenAICompletion(c.Request.Context(), h.gatewayService, account, openAIAccountScheduleModel(c, account, forwardModel, requireCompact, result), result)
		}

		// 使用量记录通过有界 worker 池提交，避免请求热路径创建无界 goroutine。
		submitResponsesUsage(result)
		reqLog.Debug("openai.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", recovery.SwitchCount),
		)
		return service.NativeOperationDone()
	})
	h.finishNativeHTTPOperation(c, operationErr, gatewaymodel.ProtocolResponses)
}
