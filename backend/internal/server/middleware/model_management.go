package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ManagedModels runs after public group admission and Composite dispatch. The
// policy is additive: account/resource qualification remains in the scheduler.
func ManagedModels(settings *service.SettingService, maxBodySize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if settings == nil {
			c.Next()
			return
		}
		c.Request = c.Request.WithContext(service.WithModelManagementContext(c.Request.Context(), settings))
		if c.Request.Method == http.MethodGet && strings.Contains(c.FullPath(), "/models") {
			c.Set("managed-models-if-none-match", c.GetHeader("If-None-Match"))
			c.Request.Header.Del("If-None-Match")
		}
		if isResponsesWebSocketRoute(c) {
			c.Next()
			return
		}
		key, ok := GetAPIKeyFromContext(c)
		if !ok || key == nil {
			c.Next()
			return
		}
		platform := ""
		if key.Group != nil {
			platform = key.Group.Platform
		}
		if forced, ok := GetForcePlatformFromContext(c); ok {
			platform = forced
		}
		models := []string{}
		var body []byte
		if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodPatch {
			var done bool
			models, done = groupModelAllowlistModelsFromBody(c, maxBodySize)
			if !done {
				return
			}
			// PrereadBody owns request replay; read again only for Claude parameter edits.
			if platform == service.PlatformAnthropic && !requestmodel.IsMultipartContentType(c.GetHeader("Content-Type")) {
				var err error
				body, err = readGroupModelAllowlistBody(c, maxBodySize)
				if err != nil {
					groupModelAllowlistErrorWriter(c)(c, 400, "Invalid model request")
					c.Abort()
					return
				}
				requestmodel.ResetRequestBody(c.Request, body)
			}
		} else if model := groupModelAllowlistModelFromParams(c); model != "" {
			models = append(models, model)
		} else if model := c.Query("model"); model != "" {
			models = append(models, model)
		}
		for _, model := range models {
			if err := settings.CheckManagedModel(c.Request.Context(), platform, model); err != nil {
				status := http.StatusNotFound
				if errors.Is(err, service.ErrModelPolicyUnavailable) {
					status = http.StatusServiceUnavailable
				}
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalModelConfiguration)
				MarkIngressRejected(c, IngressRejectModelNotAllowed)
				groupModelAllowlistErrorWriter(c)(c, status, err.Error())
				c.Abort()
				return
			}
		}
		if len(body) > 0 && strings.Contains(c.FullPath(), "messages") {
			patched, betas, err := settings.ApplyClaudeModelParams(c.Request.Context(), body, c.GetHeader("Anthropic-Beta"))
			if err != nil {
				groupModelAllowlistErrorWriter(c)(c, 503, "Model configuration unavailable")
				c.Abort()
				return
			}
			requestmodel.ResetRequestBody(c.Request, patched)
			c.Request.Header.Set("Anthropic-Beta", betas)
		}
		c.Next()
	}
}
