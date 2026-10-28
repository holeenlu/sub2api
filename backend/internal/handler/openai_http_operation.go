package handler

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// The common bounded reader owns original wire before admission prereads.
// This root consumes that proof without hashing the transformed body again.
func readNativeHTTPOperationBody(request *http.Request, cfg *config.Config) ([]byte, [32]byte, error) {
	body, err := readLenientJSONRequestBodyWithPrealloc(request, cfg)
	wire, _ := httputil.RequestWireDigest(request)
	return body, wire, err
}

// Reuse the existing fresh dispatch authority; the additional binding supplies
// protocol and immutable wire identity to its operation, not another permit.
func (h *OpenAIGatewayHandler) bindNativeHTTPOperation(c *gin.Context, key *service.APIKey, requestedModel string, protocol model.ClientProtocol, wire [32]byte) error {
	if err := h.bindNativeHTTPDispatch(c, key, requestedModel); err != nil {
		return err
	}
	ctx, err := h.gatewayService.BindNativeHTTPOperation(c.Request.Context(), protocol, wire)
	if err == nil {
		c.Request = c.Request.WithContext(ctx)
	}
	return err
}

func (h *OpenAIGatewayHandler) finalizeNativeHTTPOperationUsage(c *gin.Context, input *service.OpenAIRecordUsageInput) bool {
	handled, err := h.gatewayService.FinalizeNativeHTTPUsage(c.Request.Context(), input)
	if err != nil {
		logger.L().With(
			zap.String("component", "handler.native_http_operation"),
			zap.Int64("api_key_id", input.APIKey.ID),
			zap.Int64("account_id", input.Account.ID),
		).Error("native_http.usage_finalization_failed", zap.Error(err))
	}
	return handled
}

// A finalization failure may override a planned retry before the callback has
// written its own error. Do not return an implicit empty 200 in that case, or
// append a second response after an existing protocol response has committed.
func (h *OpenAIGatewayHandler) finishNativeHTTPOperation(c *gin.Context, err error, protocol model.ClientProtocol) {
	if err == nil || failoverClientGone(c) || service.NativeHTTPOutputCommitted(c.Request.Context()) {
		return
	}
	if c.Writer.Written() {
		// A heartbeat can commit SSE headers without delivering business
		// output. Finish that stream with the established protocol error;
		// delegated writers without Native receipts retain their own policy.
		if !service.NativeHTTPFinalizesUsage(c.Request.Context()) || !strings.Contains(strings.ToLower(c.Writer.Header().Get("Content-Type")), "text/event-stream") {
			return
		}
		if protocol == model.ProtocolMessages {
			h.anthropicStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Request execution could not be completed", true)
		} else {
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "Request execution could not be completed", true)
		}
		return
	}
	if protocol == model.ProtocolMessages {
		h.anthropicErrorResponse(c, http.StatusServiceUnavailable, "api_error", "Request execution could not be completed")
		return
	}
	h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Request execution could not be completed")
}
