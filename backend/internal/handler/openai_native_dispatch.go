package handler

import (
	"crypto/sha256"
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/access"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// bindNativeHTTPDispatch runs after client authentication and model parsing,
// before the first queue/acquisition. Provider-mapped names and forwarded IP
// assertions never replace these original authorization facts.
func (h *OpenAIGatewayHandler) bindNativeHTTPDispatch(c *gin.Context, key *service.APIKey, requestedModel string) error {
	if c == nil || c.Request == nil || key == nil || key.User == nil || h.cfg == nil {
		return service.ErrNativeDispatchScopeChanged
	}
	if openAICompatibleRequestPlatform(c.Request.Context(), key) != service.PlatformOpenAI {
		return nil
	}
	// Composite middleware has already rewritten the working model. Its
	// trusted public identity, not the upstream name, owns key authorization.
	if original, ok := service.RequestedPublicModelFromContext(c.Request.Context()); ok {
		requestedModel = original
	}
	request := service.NativeDispatchRequest{OperationID: uuid.NewString(),
		UserID: key.UserID, APIKeyID: key.ID, APIKeyDigest: sha256.Sum256([]byte(key.Key)),
		RequestedModel: requestedModel, ClientIP: ip.GetSecurityClientIP(c, h.cfg.TrustForwardedIPForAPIKeyACL()),
		Transport: "http", SimpleMode: h.cfg.RunMode == config.RunModeSimple}
	if key.GroupID != nil {
		request.GroupBound, request.GroupID = true, *key.GroupID
	}
	ctx, err := h.gatewayService.BindNativeHTTPDispatch(c.Request.Context(), h.apiKeyService, request)
	if err == nil {
		c.Request = c.Request.WithContext(ctx)
	}
	return err
}

// A failed local authority is never an upstream-health fault or permission to
// retry using a different account. Preserve each ingress protocol's envelope.
func (h *OpenAIGatewayHandler) writeNativeHTTPDispatchError(c *gin.Context, err error, streamStarted, anthropic bool) bool {
	if errors.Is(err, service.ErrManagedModelNotFound) {
		message := "The requested model does not exist or is not available for this group"
		if anthropic {
			h.anthropicStreamingAwareError(c, http.StatusNotFound, "model_not_found", message, streamStarted, "model_not_found")
		} else {
			h.handleStreamingAwareErrorWithCode(c, http.StatusNotFound, "invalid_request_error", "model_not_found", message, streamStarted, true)
		}
		return true
	}
	if !service.IsNativeDispatchDenied(err) {
		return false
	}
	if failoverClientGone(c) {
		return true
	}
	status, kind, message := http.StatusServiceUnavailable, "api_error", "Request authorization or capacity could not be confirmed before upstream dispatch"
	if nativeClientAuthorizationChanged(err) {
		status, kind, message = http.StatusForbidden, "permission_error", "Request authorization changed before upstream dispatch"
	}
	if anthropic {
		h.anthropicStreamingAwareError(c, status, kind, message, streamStarted)
	} else {
		h.handleStreamingAwareError(c, status, kind, message, streamStarted)
	}
	return true
}

// Use the same current-principal classification at HTTP and WS boundaries.
// Storage/transport outages are deliberately not classified as client revocation.
func nativeClientAuthorizationChanged(err error) bool {
	if !service.IsNativeDispatchPrincipalFailure(err) {
		return false
	}
	return errors.Is(err, service.ErrNativeDispatchScopeChanged) || errors.Is(err, service.ErrGroupNotAllowed) ||
		errors.Is(err, service.ErrAPIKeyNotFound) ||
		errors.Is(err, access.ErrKeyDisabled) || errors.Is(err, access.ErrKeyActorChanged) ||
		errors.Is(err, access.ErrKeyUserInactive) || errors.Is(err, access.ErrKeyVendorDenied) ||
		errors.Is(err, access.ErrKeyExpired) || errors.Is(err, access.ErrKeyQuotaExhausted)
}
