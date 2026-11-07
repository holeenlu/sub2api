package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Main's export route sets this server-only marker after the existing shared
// step-up gate succeeds in the same live actor/session/vendor scope.
const VendorAccountExportStepUpMarker = "vendor_accounts_export_step_up"

type VendorAccountHandler struct {
	service     *service.VendorService
	test        *service.AccountTestService
	concurrency *service.ConcurrencyService
}

func NewVendorAccountHandler(s *service.VendorService) *VendorAccountHandler {
	return &VendorAccountHandler{service: s}
}

// SetAccountTestService connects the native controlled account probe; absence
// fails closed rather than returning a simulated health result.
func (h *VendorAccountHandler) SetAccountTestService(s *service.AccountTestService) { h.test = s }
func (h *VendorAccountHandler) SetConcurrencyService(s *service.ConcurrencyService) {
	h.concurrency = s
}
func vendorAccountBind(c *gin.Context, out any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		response.Error(c, 422, "Invalid request fields or body")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		response.Error(c, 422, "Expected one JSON object")
		return false
	}
	return true
}
func vendorAccountID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.NotFound(c, "Account not found")
		return 0, false
	}
	return id, true
}
func vendorAccountReply(c *gin.Context, data any, err error) {
	if err != nil {
		// Known public errors are stable and credential-free. Raw SQL/upstream errors
		// are never echoed or attached as causes to the supplier response.
		var typed *infraerrors.Error
		if errors.Is(err, authz.ErrAuthorizationExpired) {
			response.Unauthorized(c, "Authorization expired; authenticate again")
		} else if errors.As(err, &typed) {
			response.ErrorFrom(c, err)
		} else {
			response.Error(c, 503, "Account operation unavailable; retry or authorize again")
		}
		return
	}
	response.Success(c, data)
}
func (h *VendorAccountHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	group, _ := strconv.ParseInt(c.Query("group_id"), 10, 64)
	parseTime := func(value string, end bool) *time.Time {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err == nil {
			return &parsed
		}
		if day, err := time.Parse("2006-01-02", value); err == nil {
			if end {
				day = day.Add(24 * time.Hour)
			}
			return &day
		}
		return nil
	}
	data, err := h.service.ListVendorAccounts(c.Request.Context(), service.VendorAccountListInput{Page: page, PageSize: size, Search: c.Query("search"), Platform: c.Query("platform"), Status: c.Query("status"), Method: c.Query("method"), AuthorizationMode: c.Query("authorization_mode"), GroupID: group, SortBy: c.Query("sort_by"), SortOrder: c.Query("sort_order"), FailureCategory: c.Query("failure_category"), Cooldown: c.Query("cooldown"), Schedulability: c.Query("schedulability"), CreatedFrom: parseTime(c.Query("created_from"), false), CreatedTo: parseTime(c.Query("created_to"), true)})
	if err == nil {
		h.service.PopulateVendorAccountConcurrency(c.Request.Context(), data.Items, &data.Overview, h.concurrency)
		data.GlobalOverview, _ = h.service.GlobalAccountPoolOverview(c.Request.Context(), h.concurrency)
	}
	vendorAccountReply(c, data, err)
}
func (h *VendorAccountHandler) Get(c *gin.Context) {
	id, ok := vendorAccountID(c)
	if !ok {
		return
	}
	data, err := h.service.GetVendorAccount(c.Request.Context(), id)
	if err == nil {
		h.service.PopulateVendorAccountConcurrency(c.Request.Context(), []*service.VendorAccountDTO{data}, nil, h.concurrency)
	}
	vendorAccountReply(c, data, err)
}
func (h *VendorAccountHandler) Create(c *gin.Context) {
	var input service.VendorAccountInput
	if !vendorAccountBind(c, &input) {
		return
	}
	data, err := h.service.CreateVendorAccount(c.Request.Context(), input)
	vendorAccountReply(c, data, err)
}
func (h *VendorAccountHandler) Update(c *gin.Context) {
	id, ok := vendorAccountID(c)
	if !ok {
		return
	}
	var input service.VendorAccountUpdate
	if !vendorAccountBind(c, &input) {
		return
	}
	data, err := h.service.UpdateVendorAccount(c.Request.Context(), id, input)
	vendorAccountReply(c, data, err)
}
func (h *VendorAccountHandler) Pause(c *gin.Context)  { h.pause(c, true) }
func (h *VendorAccountHandler) Resume(c *gin.Context) { h.pause(c, false) }
func (h *VendorAccountHandler) pause(c *gin.Context, paused bool) {
	id, ok := vendorAccountID(c)
	if !ok {
		return
	}
	data, err := h.service.SetVendorAccountPaused(c.Request.Context(), id, paused)
	vendorAccountReply(c, data, err)
}
func (h *VendorAccountHandler) Delete(c *gin.Context) {
	id, ok := vendorAccountID(c)
	if !ok {
		return
	}
	err := h.service.DeleteVendorAccount(c.Request.Context(), id)
	vendorAccountReply(c, gin.H{"archived": err == nil}, err)
}
func (h *VendorAccountHandler) Refresh(c *gin.Context) {
	id, ok := vendorAccountID(c)
	if !ok {
		return
	}
	data, err := h.service.RefreshVendorAccountAuthorization(c.Request.Context(), id, c.Query("authorization_mode"))
	vendorAccountReply(c, data, err)
}
func (h *VendorAccountHandler) UsageRefresh(c *gin.Context) {
	id, ok := vendorAccountID(c)
	if !ok {
		return
	}
	data, err := h.service.RefreshVendorAccountUsage(c.Request.Context(), id)
	vendorAccountReply(c, data, err)
}
func (h *VendorAccountHandler) Sync(c *gin.Context) {
	id, ok := vendorAccountID(c)
	if !ok {
		return
	}
	data, err := h.service.SyncVendorAccount(c.Request.Context(), id)
	vendorAccountReply(c, data, err)
}
func (h *VendorAccountHandler) Probe(c *gin.Context) {
	id, ok := vendorAccountID(c)
	if !ok {
		return
	}
	data, err := h.service.ProbeVendorAccount(c.Request.Context(), id, h.test)
	vendorAccountReply(c, data, err)
}
func (h *VendorAccountHandler) StartOAuth(c *gin.Context) {
	var input service.VendorAccountInput
	if !vendorAccountBind(c, &input) {
		return
	}
	if input.Platform != "" && input.Platform != c.Param("platform") {
		response.Error(c, 422, "Platform mismatch")
		return
	}
	input.Platform = c.Param("platform")
	data, err := h.service.StartVendorOAuth(c.Request.Context(), input)
	vendorAccountReply(c, data, err)
}
func (h *VendorAccountHandler) CompleteOAuth(c *gin.Context) {
	var input service.VendorOAuthCompleteInput
	if !vendorAccountBind(c, &input) {
		return
	}
	data, err := h.service.CompleteVendorOAuth(c.Request.Context(), c.Param("platform"), c.Param("session_id"), input)
	vendorAccountReply(c, data, err)
}
func (h *VendorAccountHandler) RebindOAuthProxy(c *gin.Context) {
	var input service.VendorOAuthProxyInput
	if !vendorAccountBind(c, &input) {
		return
	}
	err := h.service.RebindVendorOAuthProxy(c.Request.Context(), c.Param("platform"), c.Param("session_id"), input)
	vendorAccountReply(c, gin.H{"rebound": err == nil}, err)
}
func (h *VendorAccountHandler) ImportPreview(c *gin.Context) {
	var input service.VendorAccountImportInput
	if !vendorAccountBind(c, &input) {
		return
	}
	data, err := h.service.PreviewVendorAccountImport(c.Request.Context(), input)
	vendorAccountReply(c, data, err)
}
func (h *VendorAccountHandler) Import(c *gin.Context) {
	var input service.VendorAccountImportInput
	if !vendorAccountBind(c, &input) {
		return
	}
	data, err := h.service.ImportVendorAccounts(c.Request.Context(), input)
	vendorAccountReply(c, data, err)
}
func (h *VendorAccountHandler) Export(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
	if !c.GetBool(VendorAccountExportStepUpMarker) {
		response.Forbidden(c, "Scoped sensitive-operation verification required")
		return
	}
	var input struct {
		IDs                []int64 `json:"ids"`
		SensitiveConfirmed bool    `json:"sensitive_confirmed"`
	}
	if !vendorAccountBind(c, &input) {
		return
	}
	if !input.SensitiveConfirmed {
		response.Error(c, 422, "Confirm that this export contains sensitive authorization material")
		return
	}
	data, err := h.service.ExportVendorAccounts(c.Request.Context(), input.IDs)
	vendorAccountReply(c, data, err)
}

// ReconcileIdentity belongs only on the live SuperOnly administrative route.
// With account_id/access_token it resolves one expired legacy identity without
// retaining supplied authorization; otherwise it scans the bounded cursor page.
func (h *VendorAccountHandler) ReconcileIdentity(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input struct {
		AfterID     int64  `json:"after_id"`
		Limit       int    `json:"limit"`
		AccountID   int64  `json:"account_id"`
		AccessToken string `json:"access_token"`
		ProxyID     *int64 `json:"proxy_id"`
		Direct      bool   `json:"direct"`
	}
	if !vendorAccountBind(c, &input) {
		return
	}
	if input.AccountID != 0 || input.AccessToken != "" {
		data, err := h.service.ResolveSupplyIdentity(c.Request.Context(), input.AccountID, input.AccessToken, input.ProxyID, input.Direct)
		vendorAccountReply(c, data, err)
		return
	}
	if input.Limit == 0 {
		input.Limit = 10
	}
	data, err := h.service.ReconcileSupplyIdentities(c.Request.Context(), input.AfterID, input.Limit)
	vendorAccountReply(c, data, err)
}

func (h *VendorAccountHandler) RevokeAuthorization(c *gin.Context) {
	id, ok := vendorAccountID(c)
	if !ok {
		return
	}
	data, err := h.service.RevokeVendorAuthorization(c.Request.Context(), id, c.Param("mode"))
	vendorAccountReply(c, data, err)
}
