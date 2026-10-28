package handler

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/catalog"
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

func (h *OpenAIGatewayHandler) Messages(c *gin.Context) {
	service.BeginOpenAIRequestTiming(c)
	streamStarted := false
	defer h.recoverAnthropicMessagesPanic(c, &streamStarted)

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.anthropicErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.anthropicErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.messages",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	// 检查分组是否允许 /v1/messages 调度
	if !allowOpenAICompatibleMessagesDispatch(c, apiKey) {
		h.anthropicErrorResponse(c, http.StatusForbidden, "permission_error",
			"This group does not allow /v1/messages dispatch")
		return
	}

	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, originalWireDigest, err := readNativeHTTPOperationBody(c.Request, h.cfg)
	if err != nil {
		logRequestBodyReadFailure(reqLog, c.Request, err)
		writeRequestBodyReadError(c, err, func(status int, errType, message string) {
			h.anthropicErrorResponse(c, status, errType, message)
		})
		return
	}
	if len(body) == 0 {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !openAICompatibleTextTargetAllowed(c, apiKey, reqModel) {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}
	bindOpenAIReasoningEffortPolicyForMessagesRequest(c, apiKey, body)
	routingModel := catalog.NormalizeOpenAICompatRequestedModel(reqModel)
	preferredMappedModel := resolveOpenAIMessagesDispatchMappedModel(c, apiKey, reqModel)
	reqStream := gjson.GetBytes(body, "stream").Bool()

	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolAnthropicMessages, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.anthropicSecurityAuditError(c, decision)
		return
	}

	// 解析渠道级模型映射
	channelMappingMsg, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
	mappedBodyForMessages := newOpenAIModelMappedBodyCache(body, h.gatewayService.ReplaceModelInBody)

	// 绑定错误透传服务，允许 service 层在非 failover 错误场景复用规则。
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	requestPlatform := openAICompatibleRequestPlatform(c.Request.Context(), apiKey)
	c.Request = c.Request.WithContext(service.EnsureOpenAITurnBudget(c.Request.Context(), h.cfg))
	if requestPlatform == service.PlatformOpenAI && reqStream {
		c.Request = c.Request.WithContext(service.EnsureOpenAITurnBudget(c.Request.Context(), h.cfg, gjson.GetBytes(body, "reasoning.effort").String()))
	}

	if err := h.bindNativeHTTPOperation(c, apiKey, reqModel, gatewaymodel.ProtocolMessages, originalWireDigest); err != nil {
		h.anthropicStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Native dispatch dependencies are unavailable", streamStarted)
		return
	}

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted, reqLog, service.NativeHTTPOperationBound(c.Request.Context()))
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("openai_messages.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.anthropicStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	// 余额模式在途预留（与计费同口径估算；计费任务扣减余额缓存后才释放）。
	inflightDone, inflightErr := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, tokenInflightEstimate(reqModel, body))
	if inflightErr != nil {
		status, code, message, retryAfter := billingErrorDetails(inflightErr)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.anthropicStreamingAwareError(c, status, code, message, streamStarted)
		return
	}
	defer inflightDone()

	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	promptCacheKey := h.gatewayService.ExtractSessionID(c, body)
	sessionHash, promptCacheKey = resolveOpenAIMessagesMetadataSession(c, sessionHash, promptCacheKey, reqModel, body)
	if h.rejectIfCyberSessionBlocked(c, apiKey, body, reqModel, cyberBlockFormatAnthropic) {
		return
	}

	c.Request = c.Request.WithContext(service.EnsureOpenAITurnBudget(c.Request.Context(), h.cfg))
	maxAccountSwitches := h.maxAccountSwitches
	recovery := NewFailoverState(maxAccountSwitches, false)
	effectiveMappedModel := preferredMappedModel

	// 分组利润控制：Messages 文本入口同样请求级装门并固定 pricingAt。
	msgPricingCtx, pricingAt := h.gatewayService.WithOpenAIRequestPricingContext(c.Request.Context(), apiKey.GroupID)
	c.Request = c.Request.WithContext(msgPricingCtx)
	if err := h.gatewayService.BindNativeIntentPricing(c.Request.Context(), service.NativeIntentPricingInput{
		APIKey: apiKey, User: apiKey.User, Subscription: subscription, APIKeyService: h.apiKeyService, PricingAt: pricingAt,
		ChannelUsageFields: clientRequestedUsageFields(c, channelMappingMsg, reqModel, ""),
		QuotaPlatform:      service.QuotaPlatform(c.Request.Context(), apiKey), RequestPayloadHash: service.HashUsageRequestPayload(body),
	}); err != nil {
		h.anthropicStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Native pricing intent could not be established", streamStarted)
		return
	}

	operationErr := service.RunNativeHTTPOperation(c.Request.Context(), c, func() service.NativeOperationStep {
		if failoverClientGone(c) {
			return service.NativeOperationDone()
		}
		currentRoutingModel := routingModel
		if effectiveMappedModel != "" {
			currentRoutingModel = effectiveMappedModel
		}
		reqLog.Debug("openai_messages.account_selecting", zap.Int("excluded_account_count", len(recovery.FailedAccountIDs)))
		selection, scheduleDecision, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(),
			apiKey.GroupID,
			"", // no previous_response_id
			sessionHash,
			currentRoutingModel,
			recovery.FailedAccountIDs,
			service.OpenAIUpstreamTransportAny,
			service.OpenAIEndpointCapabilityChatCompletions,
			false,
			false,
			true,
			requestPlatform,
		)
		if err != nil {
			if action, handled := recovery.RecoverOpenAISelection(c.Request.Context(), c, err, body, h.gatewayService, reqLog); handled {
				if action == execution.RecoveryContinue {
					return service.NativeOperationContinue()
				}
				h.handleAnthropicFailoverExhausted(c, recovery.LastFailoverErr, streamStarted)
				return service.NativeOperationDone()
			}
			if h.handleOpenAITurnBudgetError(c, err, streamStarted) {
				return service.NativeOperationDone()
			}
			if failoverClientGone(c) {
				reqLog.Info("openai_messages.account_select_aborted_client_disconnected", zap.Error(err))
				return service.NativeOperationDone()
			}
			reqLog.Warn("openai_messages.account_select_failed",
				zap.Error(openAICompatibleSelectionErrorForLog(err, requestPlatform)),
				zap.Int("excluded_account_count", len(recovery.FailedAccountIDs)),
			)
			if (len(recovery.FailedAccountIDs) == 0 && recovery.LastFailoverErr == nil) || (!errors.Is(err, service.ErrNoAvailableAccounts) && !errors.Is(err, service.ErrNoAvailableCompactAccounts)) {
				if err != nil {
					cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, currentRoutingModel, reqModel)
					if !cls.ModelNotFound {
						markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
					}
					h.anthropicStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
					return service.NativeOperationDone()
				}
			} else {
				if recovery.LastFailoverErr != nil {
					service.RecordOpenAIRetryStop(c, nil, service.OpenAIRetryStopNoAvailableAccount)
					h.handleAnthropicFailoverExhausted(c, recovery.LastFailoverErr, streamStarted)
				} else {
					h.anthropicStreamingAwareError(c, http.StatusBadGateway, "api_error", "Upstream request failed", streamStarted)
				}
				return service.NativeOperationDone()
			}
		}
		if selection == nil || selection.Account == nil {
			cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, currentRoutingModel, reqModel)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			h.anthropicStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
			return service.NativeOperationDone()
		}
		account := selection.Account
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		reqLog.Debug("openai_messages.account_selected", zap.Int64("account_id", account.ID), zap.String("account_name", account.Name))
		_ = scheduleDecision
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

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()

		defaultMappedModel := strings.TrimSpace(effectiveMappedModel)
		// 应用渠道模型映射到请求体
		forwardBody := mappedBodyForMessages(channelMappingMsg.Mapped, channelMappingMsg.MappedModel)
		writerSizeBeforeForward := c.Writer.Size()
		result, err := service.TrackNativeHTTPForward(c.Request.Context(), func() (*service.OpenAIForwardResult, error) {
			return h.gatewayService.ForwardAsAnthropic(c.Request.Context(), c, account, forwardBody, promptCacheKey, defaultMappedModel)
		})
		var cyberBlockBodyMsg []byte
		if service.GetOpsCyberPolicy(c) != nil {
			cyberBlockBodyMsg = body
		}
		service.TrackNativeHTTPAuthorization(c.Request.Context(), service.ActualOpenAIUsageAuthorizationMode(c))
		nativeUsageOwned := service.NativeHTTPFinalizesUsage(c.Request.Context())
		h.recordCyberPolicyIfMarked(c, apiKey, account, subscription, reqModel, err != nil && result == nil && !nativeUsageOwned, cyberBlockBodyMsg, clientRequestedUsageFields(c, channelMappingMsg, reqModel, ""), service.HashUsageRequestPayload(body))
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
		// Forward 与错误一起返回的部分结果：流中断/客户端断开排水前上游已计量的
		// usage 照常入账，避免上游已产生消耗的请求完全漏记（#5148，对齐 anthropic
		// 网关同名修复）。BPS failover 也保留已完成 compact 的用量。
		submitMessagesUsage := func(res *service.OpenAIForwardResult) {
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
			usageFields := clientRequestedUsageFields(c, channelMappingMsg, reqModel, upstreamModel)
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
						zap.String("component", "handler.openai_gateway.messages"),
						zap.Int64("user_id", subject.UserID),
						zap.Int64("api_key_id", apiKey.ID),
						zap.Any("group_id", apiKey.GroupID),
						zap.String("model", reqModel),
						zap.Int64("account_id", account.ID),
					).Error("openai_messages.record_usage_failed", zap.Error(err))
				}
			})
		}
		if err != nil {
			if h.writeNativeHTTPDispatchError(c, err, streamStarted, true) {
				submitMessagesUsage(result)
				return service.NativeOperationDone()
			}
			if errors.Is(err, service.ErrOpenAITurnAttemptBudgetExhausted) || errors.Is(err, service.ErrOpenAIWSFirstOutputBudgetExhausted) {
				submitMessagesUsage(result)
				h.anthropicStreamingAwareError(c, http.StatusServiceUnavailable, "retry_budget_exhausted", err.Error(), streamStarted)
				return service.NativeOperationDone()
			}
			if result != nil && result.ImageCount > 0 {
				reqLog.Warn("openai_messages.forward_partial_error_with_image_result",
					zap.Int64("account_id", account.ID),
					zap.Int("image_count", result.ImageCount),
					zap.Error(err),
				)
			} else {
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					submitMessagesUsage(result)
					if failoverClientGone(c) {
						reqLog.Info("openai_messages.failover_aborted_client_disconnected",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						return service.NativeOperationDone()
					}
					if !service.OpenAITurnRetryAllowed(c.Request.Context(), account.ID, failoverErr.ResponseBody) {
						service.RecordOpenAIRetryStop(c, account, service.OpenAIRetryStopTurnBudget)
						h.handleAnthropicFailoverExhausted(c, failoverErr, streamStarted)
						return service.NativeOperationDone()
					}
					if c.Writer.Size() != writerSizeBeforeForward {
						service.RecordOpenAIRetryStop(c, account, service.OpenAIRetryStopDownstreamCommitted)
						h.gatewayService.ObserveOpenAIAccountHealthFailure(c.Request.Context(), account, err)
						h.handleAnthropicFailoverExhausted(c, failoverErr, true)
						return service.NativeOperationDone()
					}
					if failoverErr.ShouldReportAccountScheduleFailure() {
						h.gatewayService.ReportOpenAIAccountScheduleFailure(c.Request.Context(), account, openAIAccountScheduleModel(c, account, currentRoutingModel, false, nil), err)
					}
					if !failoverErr.ShouldRetryNextAccount() && !failoverErr.RetryableOnSameAccount {
						h.handleAnthropicFailoverExhausted(c, failoverErr, streamStarted)
						return service.NativeOperationDone()
					}
					portable := openAIForwardAccountReplayAllowed(c, account, body, failoverErr)
					if finalErr := service.NativeHTTPFinalizationError(c.Request.Context()); finalErr != nil {
						return service.NativeOperationStep{Err: finalErr}
					}
					action, recoveryErr := recovery.RecoverOpenAI(c.Request.Context(), c, account, failoverErr, portable, true, h.gatewayService, reqLog)
					if recoveryErr != nil {
						h.handleOpenAITurnBudgetError(c, recoveryErr, streamStarted)
						return service.NativeOperationDone()
					}
					if action == execution.RecoveryCanceled {
						return service.NativeOperationDone()
					}
					if action == execution.RecoveryExhausted {
						h.handleAnthropicFailoverExhausted(c, failoverErr, streamStarted)
						return service.NativeOperationDone()
					}
					return service.NativeOperationContinue()
				}
				if result != nil && result.ClientDisconnect {
					reqLog.Info("openai_messages.client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Error(err),
					)
					// 断开排水期间上游已计量的 usage 必须入账（此前直接 return 丢弃，
					// payg 上游照常计费而平台漏记）。
					submitMessagesUsage(result)
					return service.NativeOperationDone()
				}
				h.gatewayService.ReportOpenAIAccountScheduleFailure(c.Request.Context(), account, openAIAccountScheduleModel(c, account, currentRoutingModel, false, result), err)
				wroteFallback := h.ensureAnthropicErrorResponse(c, streamStarted)
				reqLog.Warn("openai_messages.forward_failed",
					zap.Int64("account_id", account.ID),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Error(err),
				)
				submitMessagesUsage(result)
				return service.NativeOperationDone()
			}
		}
		if result != nil {
			h.gatewayService.ReportOpenAIAccountScheduleSuccess(c.Request.Context(), account, openAIAccountScheduleModel(c, account, currentRoutingModel, false, result), result.FirstTokenMs)
		} else {
			h.gatewayService.ReportOpenAIAccountScheduleSuccess(c.Request.Context(), account, openAIAccountScheduleModel(c, account, currentRoutingModel, false, result), nil)
		}

		submitMessagesUsage(result)
		reqLog.Debug("openai_messages.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", recovery.SwitchCount),
		)
		return service.NativeOperationDone()
	})
	h.finishNativeHTTPOperation(c, operationErr, gatewaymodel.ProtocolMessages)
}
