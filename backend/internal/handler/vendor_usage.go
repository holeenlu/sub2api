package handler

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type VendorUsageHandler struct{ service *service.VendorService }

func NewVendorUsageHandler(s *service.VendorService) *VendorUsageHandler {
	return &VendorUsageHandler{service: s}
}
func vendorUsageQuery(c *gin.Context) (service.VendorUsageFilter, error) {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return service.VendorUsageFilter{}, service.ErrVendorUsageInput
	}
	f := service.VendorUsageFilter{StartDate: c.Query("start_date"), EndDate: c.Query("end_date"), Timezone: c.Query("timezone"), Platform: c.Query("platform"), Dimension: c.Query("dimension")}
	if values, present := query["authorization_mode"]; present {
		if len(values) != 1 || service.UsageAuthorizationModePtr(values[0]) == nil {
			return f, service.ErrVendorUsageInput
		}
		f.AuthorizationMode = values[0]
	}
	for key, target := range map[string]*string{"sort_by": &f.SortBy, "order": &f.Order} {
		if values, present := query[key]; present {
			if len(values) != 1 || values[0] == "" {
				return f, service.ErrVendorUsageInput
			}
			*target = values[0]
		}
	}
	if values, present := query["all_history"]; present {
		if len(values) != 1 || (values[0] != "true" && values[0] != "false") {
			return f, service.ErrVendorUsageInput
		}
		f.AllHistory = values[0] == "true"
		if f.AllHistory && (f.StartDate != "" || f.EndDate != "") {
			return f, service.ErrVendorUsageInput
		}
	}
	for key, dest := range map[string]*int64{"vendor_id": &f.VendorID, "account_id": &f.AccountID, "group_id": &f.GroupID, "watermark": &f.Watermark} {
		if val, ok := c.GetQuery(key); ok {
			n, e := strconv.ParseInt(val, 10, 64)
			if e != nil || n < 0 {
				return f, service.ErrVendorUsageInput
			}
			*dest = n
			if key == "watermark" {
				f.WatermarkSet = true
			}
		}
	}
	if values, ok := query["payer_vendor_id"]; ok {
		if len(values) != 1 {
			return f, service.ErrVendorUsageInput
		}
		n, e := strconv.ParseInt(values[0], 10, 64)
		if e != nil || n <= 0 {
			return f, service.ErrVendorUsageInput
		}
		f.PayerVendorID = &n
	}
	for key, dest := range map[string]*int{"page": &f.Page, "page_size": &f.PageSize, "model_page": &f.ModelPage} {
		if values, ok := query[key]; ok {
			if len(values) != 1 {
				return f, service.ErrVendorUsageInput
			}
			n, e := strconv.Atoi(values[0])
			if e != nil || n < 1 {
				return f, service.ErrVendorUsageInput
			}
			*dest = n
		}
	}
	if val, ok := c.GetQuery("snapshot_at"); ok {
		t, e := time.Parse(time.RFC3339Nano, val)
		if e != nil {
			return f, service.ErrVendorUsageInput
		}
		f.SnapshotAt = t
	}
	return f, nil
}
func vendorUsageRequestContext(c *gin.Context) (context.Context, context.CancelFunc) {
	c.Header("Cache-Control", "no-store")
	return context.WithTimeout(c.Request.Context(), 45*time.Second)
}
func vendorUsagePathID(c *gin.Context, key string) (int64, error) {
	id, err := strconv.ParseInt(c.Param(key), 10, 64)
	if err != nil || id <= 0 {
		return 0, service.ErrVendorUsageInput
	}
	return id, nil
}
func (h *VendorUsageHandler) report(c *gin.Context, dimension string, admin bool) {
	f, err := vendorUsageQuery(c)
	if response.ErrorFrom(c, err) {
		return
	}
	if f.Dimension == "request" && !admin {
		response.ErrorFrom(c, service.ErrVendorUsageInput)
		return
	}
	if dimension != "" {
		f.Dimension = dimension
	}
	ctx, cancel := vendorUsageRequestContext(c)
	defer cancel()
	out, err := h.service.VendorUsage(ctx, f, admin)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, out)
}
func (h *VendorUsageHandler) Summary(c *gin.Context)    { h.report(c, "date", false) }
func (h *VendorUsageHandler) Trend(c *gin.Context)      { h.report(c, "date", false) }
func (h *VendorUsageHandler) Accounts(c *gin.Context)   { h.report(c, "account", false) }
func (h *VendorUsageHandler) AdminUsage(c *gin.Context) { h.report(c, "", true) }
func (h *VendorUsageHandler) AccountUsage(c *gin.Context) {
	f, err := vendorUsageQuery(c)
	if response.ErrorFrom(c, err) {
		return
	}
	f.AccountID, err = vendorUsagePathID(c, "id")
	if response.ErrorFrom(c, err) {
		return
	}
	ctx, cancel := vendorUsageRequestContext(c)
	defer cancel()
	// Native admin dispatch binds a live vendor scope too. Keep this scoped path
	// so vendor_id=0 resolves to that owner instead of becoming an aggregate read.
	out, err := h.service.VendorAccountUsage(ctx, f, false)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, out)
}
func (h *VendorUsageHandler) AdminExport(c *gin.Context) {
	f, err := vendorUsageQuery(c)
	if response.ErrorFrom(c, err) {
		return
	}
	ctx, cancel := vendorUsageRequestContext(c)
	defer cancel()
	data, err := h.service.VendorUsageCSV(ctx, f)
	if response.ErrorFrom(c, err) {
		return
	}
	c.Header("Content-Disposition", "attachment; filename=vendor-usage.csv")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}
