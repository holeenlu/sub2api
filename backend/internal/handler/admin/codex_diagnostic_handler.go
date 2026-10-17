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

func (h *AccountHandler) SetScheduledTests(s *service.ScheduledTestService, r *service.ScheduledTestRunnerService) {
	h.scheduledTests = s
	h.scheduledRunner = r
}
func (h *AccountHandler) SetDiagnosticRouter(router http.Handler) {
	if h.scheduledTests != nil {
		h.scheduledTests.SetDiagnosticRouter(router)
	}
}
func (h *AccountHandler) StartScheduledTests() {
	if h.scheduledRunner != nil {
		h.scheduledRunner.Start()
	}
}

func (h *AccountHandler) diagnosticReady(c *gin.Context) bool {
	if h.scheduledTests == nil {
		response.Error(c, http.StatusServiceUnavailable, "Diagnostic monitor unavailable")
		return false
	}
	return true
}
func diagnosticError(c *gin.Context, err error) {
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
	id, ok := diagnosticAccountID(c)
	if !ok || !h.diagnosticReady(c) {
		return
	}
	plan, err := h.scheduledTests.GetDiagnosticPlan(c.Request.Context(), id)
	if err != nil {
		diagnosticError(c, err)
		return
	}
	summary, err := h.scheduledTests.DiagnosticSummaries(c.Request.Context(), []int64{id})
	if err != nil {
		diagnosticError(c, err)
		return
	}
	state := gin.H{"plan": plan, "summary": summary[id], "rules": service.DiagnosticRules()}
	if key := c.Query("api_key_id"); key != "" {
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok {
			response.Error(c, 403, "Admin user required")
			return
		}
		keyID, parseErr := strconv.ParseInt(key, 10, 64)
		if parseErr != nil || keyID <= 0 {
			response.BadRequest(c, "Select a billing key")
			return
		}
		choices, choiceErr := h.scheduledTests.DiagnosticModels(c.Request.Context(), id, subject.UserID, keyID)
		if choiceErr != nil {
			diagnosticError(c, choiceErr)
			return
		}
		state["choices"] = choices
	}
	response.Success(c, state)
}
func (h *AccountHandler) SaveCodexDiagnosticPlan(c *gin.Context) {
	id, ok := diagnosticAccountID(c)
	if !ok || !h.diagnosticReady(c) {
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
	if err := h.scheduledTests.SaveDiagnosticPlan(c.Request.Context(), plan); err != nil {
		diagnosticError(c, err)
		return
	}
	response.Success(c, plan)
}
func (h *AccountHandler) StartCodexDiagnosticRun(c *gin.Context) {
	id, ok := diagnosticAccountID(c)
	if !ok || !h.diagnosticReady(c) {
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Error(c, 403, "Admin user required")
		return
	}
	run, err := h.scheduledTests.RunDiagnosticNow(c.Request.Context(), id, subject.UserID)
	if err != nil {
		diagnosticError(c, err)
		return
	}
	response.Success(c, run)
}
func (h *AccountHandler) ListCodexDiagnosticRuns(c *gin.Context) {
	id, ok := diagnosticAccountID(c)
	if !ok || !h.diagnosticReady(c) {
		return
	}
	runs, err := h.scheduledTests.ListDiagnosticRuns(c.Request.Context(), id)
	if err != nil {
		diagnosticError(c, err)
		return
	}
	response.Success(c, gin.H{"items": runs})
}

func (h *AccountHandler) CancelCodexDiagnosticRun(c *gin.Context) {
	id, ok := diagnosticAccountID(c)
	if !ok || !h.diagnosticReady(c) {
		return
	}
	runID, err := strconv.ParseInt(c.Param("run_id"), 10, 64)
	if err != nil || runID <= 0 {
		response.BadRequest(c, "Invalid run")
		return
	}
	if err := h.scheduledTests.CancelDiagnostic(c.Request.Context(), id, runID); err != nil {
		diagnosticError(c, err)
		return
	}
	response.Success(c, gin.H{"cancel_requested": true})
}

func diagnosticAccountID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return 0, false
	}
	return id, true
}

func (h *AccountHandler) RefreshDiagnosticFingerprint(c *gin.Context) {
	if !h.diagnosticReady(c) {
		return
	}
	commit, models, err := h.scheduledTests.RefreshDiagnosticFingerprint(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"commit": commit, "models": models})
}
