package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	provider "github.com/Wei-Shaw/sub2api/internal/providers/typesafe"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const systemOneOnlyPlatformMessage = "TypeSafe models are only available through POST /v1/systemone"

// rejectSystemOneOnlyPlatform stops TypeSafe traffic from entering a non System
// One protocol chain. TypeSafe accounts only speak the native System One
// protocol; letting them reach the Anthropic/OpenAI converters would send
// foreign payloads (and the account key) to the wrong upstream path and feed
// the resulting auth failures back into account state.
func rejectSystemOneOnlyPlatform(c *gin.Context, apiKey *service.APIKey, writeError func(*gin.Context, int, string, string)) bool {
	if _, forced := middleware2.GetForcePlatformFromContext(c); forced {
		return false
	}
	if effectiveAPIKeyPlatform(c, apiKey) != service.PlatformTypeSafe {
		return false
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
	writeError(c, http.StatusNotFound, "not_found_error", systemOneOnlyPlatformMessage)
	return true
}

// SystemOne proxies TypeSafe's native, non-streaming System One protocol.
func (h *GatewayHandler) SystemOne(c *gin.Context) {
	requestStart := time.Now()
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(c, "handler.gateway.systemone",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		writeRequestBodyReadError(c, err, func(status int, errType, message string) {
			h.errorResponse(c, status, errType, message)
		})
		return
	}
	model, err := typesafe.ValidateSystemOneRequest(body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	ensureCompositeTargetPlatform(c, apiKey, model)
	if apiKey.Group.Platform != service.PlatformTypeSafe &&
		(apiKey.Group.Platform != service.PlatformComposite || !compositeTargetPlatformAllowed(c, apiKey, model, service.PlatformTypeSafe)) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "System One is only available for TypeSafe and compatible Composite groups")
		return
	}
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolTypeSafeSystemOne, model, body); decision != nil && !decision.AllowNextStage {
		h.anthropicSecurityAuditError(c, decision)
		return
	}

	setOpsRequestContext(c, model, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	pricingCtx, pricingAt := service.WithGatewayTokenRequestPricing(c.Request.Context())
	c.Request = c.Request.WithContext(pricingCtx)
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, model)
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	ctx, bindErr := h.gatewayService.SystemOneOperationContext(c.Request.Context())
	if bindErr != nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "System One execution is unavailable")
		return
	}
	c.Request = c.Request.WithContext(ctx)

	streamStarted := false
	userRelease, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, false, &streamStarted)
	if err != nil {
		reqLog.Warn("systemone.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", false)
		return
	}
	// 在请求结束或 Context 取消时确保释放槽位，避免客户端断开造成泄漏。
	userRelease = wrapReleaseOnDone(c.Request.Context(), userRelease)
	if userRelease != nil {
		defer userRelease()
	}
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("systemone.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}
	// 余额模式在途预留：防止并发请求在预检时看到同一份余额而集体透支。
	inflightRelease, err := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, tokenInflightEstimate(model, body))
	if err != nil {
		reqLog.Info("systemone.inflight_reservation_rejected", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}
	defer inflightRelease()
	h.executeSystemOneOperation(c, apiKey, subscription, channelMapping, body, pricingAt)
}

func (h *GatewayHandler) executeSystemOneOperation(c *gin.Context, key *service.APIKey, subscription *service.UserSubscription, mapping service.ChannelMappingResult, body []byte, pricingAt time.Time) {
	wire, ok := pkghttputil.RequestWireDigest(c.Request)
	if !ok || wire == ([32]byte{}) {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Original request identity is unavailable")
		return
	}
	requested := typesafe.JevLatestModel
	if public, exists := service.RequestedPublicModelFromContext(c.Request.Context()); exists {
		requested = public
	}
	in := service.SystemOneExecutionInput{OperationID: uuid.NewString(), OriginalWireDigest: wire, Body: body, RequestedModel: requested, APIKey: key, KeyService: h.apiKeyService,
		Subscription: subscription, PricingAt: pricingAt, SecurityClientIP: ip.GetSecurityClientIP(c, h.cfg.TrustForwardedIPForAPIKeyACL()), IPAddress: ip.GetClientIP(c), UserAgent: c.GetHeader("User-Agent"),
		SessionID: service.ExtractClientSessionID(c), InboundEndpoint: GetInboundEndpoint(c), UpstreamEndpoint: typesafe.SystemOnePath, QuotaPlatform: service.QuotaPlatform(c.Request.Context(), key),
		RequestPayloadHash: service.HashUsageRequestPayload(body), MaxAccountSwitches: h.maxAccountSwitches, ChannelUsageFields: clientRequestedUsageFields(c, mapping, typesafe.JevLatestModel, "")}
	result, err := h.gatewayService.ExecuteSystemOne(c.Request.Context(), in, service.SystemOneIngressPorts{
		AcquireAccount: func(ctx context.Context, selection *service.AccountSelectionResult) (func(), error) {
			if selection.Acquired {
				return selection.ReleaseFunc, nil
			}
			if selection.WaitPlan == nil {
				return nil, service.ErrNoAvailableAccounts
			}
			streamStarted := false
			return h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(c, selection.Account.ID, selection.WaitPlan.MaxConcurrency, selection.WaitPlan.Timeout, false, &streamStarted, selection.WaitPlan.RefreshLimit)
		},
		Selected: func(account *service.Account) {
			setOpsSelectedAccount(c, account.ID, account.Platform)
			service.SetOpsUpstreamModel(c, typesafe.JevLatestModel)
		},
		Deliver: func(ctx context.Context, response *provider.Response) (model.DeliveryReceipt, error) {
			if err := ctx.Err(); err != nil {
				return model.DeliveryReceipt{}, context.Cause(ctx)
			}
			c.Header("Content-Type", response.ContentType)
			c.Status(response.StatusCode)
			n, err := c.Writer.Write(response.Body)
			return model.DeliveryReceipt{BytesWritten: int64(n), HeadersCommitted: c.Writer.Written(), BusinessCommitted: n > 0, PossiblyCommitted: err != nil}, err
		},
	})
	if err == nil {
		return
	}
	if c.Writer.Written() {
		// The response may already be delivered while journal persistence or
		// closeout failed. Preserve diagnostics without emitting a second body.
		logger.FromContext(c.Request.Context()).Error("systemone.completion_failed",
			zap.String("operation_id", result.OperationID),
			zap.String("billing_disposition", string(result.Billing.Disposition)),
			zap.Bool("billing_durable", result.Billing.Durable), zap.Error(err))
		return
	}
	if failoverClientGone(c) {
		return
	}
	var upstream *service.SystemOneUpstreamError
	if errors.As(err, &upstream) {
		status := http.StatusBadGateway
		if service.IsSystemOneRequestErrorStatus(upstream.StatusCode) {
			status = upstream.StatusCode
		}
		h.errorResponse(c, status, "upstream_error", "TypeSafe rejected the request")
		return
	}
	var failover *service.UpstreamFailoverError
	if errors.As(err, &failover) {
		h.handleFailoverExhausted(c, failover, service.PlatformTypeSafe, false)
		return
	}
	if len(result.Attempts) == 0 && errors.Is(err, service.ErrNoAvailableAccounts) {
		cls := classifyNoAccountErrorFromGin(c, h.gatewayService, key, typesafe.JevLatestModel, typesafe.JevLatestModel, service.PlatformTypeSafe)
		h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
		return
	}
	if errors.Is(err, typesafe.ErrSystemOneResponseTooLarge) {
		h.errorResponse(c, http.StatusBadGateway, "upstream_error", "TypeSafe response exceeds the gateway size limit")
		return
	}
	logger.FromContext(c.Request.Context()).Warn("systemone.execution_failed", zap.Error(err))
	h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "System One execution could not be completed")
}
