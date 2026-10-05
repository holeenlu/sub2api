package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) SetCodexDiagnosticMonitor(s *service.CodexDiagnosticMonitor) {
	h.codexDiagnosticMonitor = s
}
func (h *AccountHandler) diagnosticMonitorReady(c *gin.Context) bool {
	if h.codexDiagnosticMonitor == nil {
		response.Error(c, http.StatusServiceUnavailable, "Diagnostic monitor unavailable")
		return false
	}
	return true
}
func diagnosticMonitorError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrDiagnosticBusy):
		response.ErrorWithDetails(c, 409, "A diagnostic is already running", "DIAGNOSTIC_BUSY", nil)
	case errors.Is(err, service.ErrDiagnosticInvalid):
		response.ErrorWithDetails(c, 400, err.Error(), "DIAGNOSTIC_INVALID", nil)
	case errors.Is(err, service.ErrDiagnosticNotFound):
		response.NotFound(c, "Diagnostic not found")
	default:
		response.ErrorFrom(c, err)
	}
}
func (h *AccountHandler) GetCodexDiagnosticPlan(c *gin.Context) {
	id, ok := codexTicketAccountID(c)
	if !ok || !h.diagnosticMonitorReady(c) {
		return
	}
	plan, err := h.codexDiagnosticMonitor.GetPlan(c.Request.Context(), id)
	if err != nil {
		diagnosticMonitorError(c, err)
		return
	}
	summary, err := h.codexDiagnosticMonitor.Summaries(c.Request.Context(), []int64{id})
	if err != nil {
		diagnosticMonitorError(c, err)
		return
	}
	response.Success(c, gin.H{"plan": plan, "summary": summary[id], "default_interval_minutes": service.CodexDiagnosticDefaultIntervalMinutes, "confidence_threshold": service.CodexDiagnosticConfidence})
}
func (h *AccountHandler) SaveCodexDiagnosticPlan(c *gin.Context) {
	id, ok := codexTicketAccountID(c)
	if !ok || !h.diagnosticMonitorReady(c) {
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Error(c, 403, "Admin user required")
		return
	}
	var input struct {
		IntervalMinutes int      `json:"interval_minutes"`
		APIKeyID        int64    `json:"api_key_id"`
		Models          []string `json:"models"`
		Enabled         bool     `json:"enabled"`
	}
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "Invalid diagnostic configuration")
		return
	}
	plan := &service.CodexDiagnosticPlan{AccountID: id, OwnerID: subject.UserID, APIKeyID: input.APIKeyID, Models: input.Models, Enabled: input.Enabled, IntervalMinutes: input.IntervalMinutes}
	if err := h.codexDiagnosticMonitor.SavePlan(c.Request.Context(), plan); err != nil {
		diagnosticMonitorError(c, err)
		return
	}
	response.Success(c, plan)
}
func (h *AccountHandler) StartCodexDiagnosticRun(c *gin.Context) {
	id, ok := codexTicketAccountID(c)
	if !ok || !h.diagnosticMonitorReady(c) {
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Error(c, 403, "Admin user required")
		return
	}
	run, err := h.codexDiagnosticMonitor.RunNow(c.Request.Context(), id, subject.UserID)
	if err != nil {
		diagnosticMonitorError(c, err)
		return
	}
	response.Success(c, run)
}
func (h *AccountHandler) ListCodexDiagnosticRuns(c *gin.Context) {
	id, ok := codexTicketAccountID(c)
	if !ok || !h.diagnosticMonitorReady(c) {
		return
	}
	runs, err := h.codexDiagnosticMonitor.ListRuns(c.Request.Context(), id, 0, 10)
	if err != nil {
		diagnosticMonitorError(c, err)
		return
	}
	response.Success(c, gin.H{"items": runs})
}
func (h *AccountHandler) GetCodexDiagnosticRun(c *gin.Context) {
	id, ok := codexTicketAccountID(c)
	if !ok || !h.diagnosticMonitorReady(c) {
		return
	}
	runID, err := strconv.ParseInt(c.Param("run_id"), 10, 64)
	if err != nil || runID <= 0 {
		response.BadRequest(c, "Invalid run")
		return
	}
	run, err := h.codexDiagnosticMonitor.GetRun(c.Request.Context(), id, runID)
	if err != nil {
		diagnosticMonitorError(c, err)
		return
	}
	response.Success(c, run)
}
func (h *AccountHandler) CancelCodexDiagnosticRun(c *gin.Context) {
	id, ok := codexTicketAccountID(c)
	if !ok || !h.diagnosticMonitorReady(c) {
		return
	}
	runID, err := strconv.ParseInt(c.Param("run_id"), 10, 64)
	if err != nil || runID <= 0 {
		response.BadRequest(c, "Invalid run")
		return
	}
	if err := h.codexDiagnosticMonitor.Cancel(c.Request.Context(), id, runID); err != nil {
		diagnosticMonitorError(c, err)
		return
	}
	response.Success(c, gin.H{"cancel_requested": true})
}

func (h *AccountHandler) GetCodexDiagnosticModels(c *gin.Context) {
	id, ok := codexTicketAccountID(c)
	if !ok || !h.diagnosticMonitorReady(c) {
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Error(c, 403, "Admin user required")
		return
	}
	keyID, err := strconv.ParseInt(c.Query("api_key_id"), 10, 64)
	if err != nil || keyID <= 0 {
		response.BadRequest(c, "Select a billing key")
		return
	}
	choices, err := h.codexDiagnosticMonitor.AvailableModels(c.Request.Context(), id, subject.UserID, keyID)
	if err != nil {
		diagnosticMonitorError(c, err)
		return
	}
	response.Success(c, choices)
}
