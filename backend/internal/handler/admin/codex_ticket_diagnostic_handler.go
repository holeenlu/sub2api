package admin

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) SetCodexTicketDiagnosticRouter(router http.Handler, keys *service.APIKeyService) {
	h.codexTicketRouter = router
	h.codexTicketAPIKeys = keys
	if h.codexDiagnosticMonitor != nil {
		h.codexDiagnosticMonitor.SetRouter(router)
	}
}

type codexDiagnosticItem = service.CodexDiagnosticItem

func codexDiagnosticGatewayError(raw []byte) string   { return service.CodexDiagnosticGatewayError(raw) }
func codexDiagnosticOutput(raw []byte) (string, bool) { return service.CodexDiagnosticOutput(raw) }

func (h *AccountHandler) DiagnoseCodexModels(c *gin.Context) {
	h.diagnoseCodexModels(c, service.NewModelTraceChallenge)
}

func (h *AccountHandler) diagnoseCodexModels(c *gin.Context, generateChallenge func() (service.ModelTraceChallenge, error)) {
	accountID, ok := codexTicketAccountID(c)
	if !ok {
		return
	}
	if h.codexTicketRouter == nil || h.codexTicketAPIKeys == nil || h.codexTicketGateway == nil {
		codexTicketError(c, service.ErrCodexTicketUnavailable)
		return
	}
	var input struct {
		APIKeyID int64    `json:"api_key_id"`
		Models   []string `json:"models"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.APIKeyID <= 0 || len(input.Models) == 0 || len(input.Models) > 32 {
		response.BadRequest(c, "Select an API key and up to 32 models")
		return
	}
	subject, authorized := middleware.GetAuthSubjectFromContext(c)
	if !authorized || subject.UserID <= 0 {
		response.ErrorWithDetails(c, http.StatusForbidden, "Admin user required", "FORBIDDEN", nil)
		return
	}
	key, err := h.codexTicketAPIKeys.GetByID(c.Request.Context(), input.APIKeyID)
	if err != nil || key == nil || key.UserID != subject.UserID || !key.IsActive() || key.IsExpired() || key.IsQuotaExhausted() || key.Key == "" {
		response.ErrorWithDetails(c, http.StatusForbidden, "API key unavailable or not owned by this admin", "API_KEY_UNAVAILABLE", nil)
		return
	}
	allowed, err := service.ModelTraceGPTModels()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	modelSet := make(map[string]bool, len(allowed))
	for _, model := range allowed {
		modelSet[model] = true
	}
	seen := make(map[string]bool, len(input.Models))
	for _, model := range input.Models {
		if !modelSet[model] || seen[model] {
			response.BadRequest(c, "Invalid or duplicate GPT model")
			return
		}
		seen[model] = true
	}
	results := make([]codexDiagnosticItem, 0, len(input.Models))
	for _, model := range input.Models {
		if c.Request.Context().Err() != nil {
			break
		}
		item := service.RunCodexDiagnosticProbe(c.Request.Context(), h.codexTicketGateway, h.codexTicketRouter, key, accountID, model, c.Request.RemoteAddr, c.Request.Host, generateChallenge)
		results = append(results, item)
	}
	response.Success(c, gin.H{"items": results, "canceled": c.Request.Context().Err() != nil})
}

func (h *AccountHandler) StartCodexDiagnosticMonitor() {
	if h.codexDiagnosticMonitor != nil {
		h.codexDiagnosticMonitor.Start()
	}
}
