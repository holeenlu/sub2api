package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type VendorSettlementHandler struct{ service *service.VendorService }

func NewVendorSettlementHandler(s *service.VendorService) *VendorSettlementHandler {
	return &VendorSettlementHandler{service: s}
}
func vendorSettlementJSON(c *gin.Context, dest any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(dest) != nil {
		return service.ErrVendorUsageInput
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return service.ErrVendorUsageInput
	}
	return nil
}
func (h *VendorSettlementHandler) list(c *gin.Context, admin bool, direction string) {
	f, err := vendorUsageQuery(c)
	if response.ErrorFrom(c, err) {
		return
	}
	ctx, cancel := vendorUsageRequestContext(c)
	defer cancel()
	out, err := h.service.ListVendorSettlements(ctx, service.VendorSettlementListFilter{Direction: direction, PayerVendorID: f.PayerVendorID, VendorID: f.VendorID, Status: c.Query("status"), StartDate: f.StartDate, EndDate: f.EndDate, Timezone: f.Timezone, Page: f.Page, PageSize: f.PageSize}, admin)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, out)
}
func (h *VendorSettlementHandler) List(c *gin.Context)      { h.list(c, false, "receivable") }
func (h *VendorSettlementHandler) AdminList(c *gin.Context) { h.list(c, true, "payable") }
func (h *VendorSettlementHandler) get(c *gin.Context, admin bool, direction string) {
	id, err := vendorUsagePathID(c, "id")
	if response.ErrorFrom(c, err) {
		return
	}
	f, err := vendorUsageQuery(c)
	if response.ErrorFrom(c, err) {
		return
	}
	ctx, cancel := vendorUsageRequestContext(c)
	defer cancel()
	out, err := h.service.GetVendorSettlement(ctx, id, f.Page, f.PageSize, admin, direction)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, out)
}
func (h *VendorSettlementHandler) Get(c *gin.Context)      { h.get(c, false, "receivable") }
func (h *VendorSettlementHandler) AdminGet(c *gin.Context) { h.get(c, true, "receivable") }
func (h *VendorSettlementHandler) export(c *gin.Context, admin bool, direction string) {
	id, err := vendorUsagePathID(c, "id")
	if response.ErrorFrom(c, err) {
		return
	}
	ctx, cancel := vendorUsageRequestContext(c)
	defer cancel()
	data, err := h.service.VendorSettlementCSV(ctx, id, admin, direction)
	if response.ErrorFrom(c, err) {
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=vendor-settlement-%d.csv", id))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}
func (h *VendorSettlementHandler) Export(c *gin.Context)      { h.export(c, false, "receivable") }
func (h *VendorSettlementHandler) AdminExport(c *gin.Context) { h.export(c, true, "receivable") }
func (h *VendorSettlementHandler) Preview(c *gin.Context) {
	var in service.VendorSettlementInput
	if response.ErrorFrom(c, vendorSettlementJSON(c, &in)) {
		return
	}
	ctx, cancel := vendorUsageRequestContext(c)
	defer cancel()
	out, err := h.service.PreviewVendorSettlements(ctx, in)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, out)
}
func (h *VendorSettlementHandler) Create(c *gin.Context) {
	var in struct {
		Drafts []service.VendorSettlementDraftInput `json:"drafts"`
	}
	if response.ErrorFrom(c, vendorSettlementJSON(c, &in)) {
		return
	}
	ctx, cancel := vendorUsageRequestContext(c)
	defer cancel()
	out, err := h.service.CreateVendorSettlements(ctx, in.Drafts)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, out)
}
func (h *VendorSettlementHandler) Confirm(c *gin.Context) { h.versionAction(c, false) }
func (h *VendorSettlementHandler) Void(c *gin.Context)    { h.versionAction(c, true) }
func (h *VendorSettlementHandler) versionAction(c *gin.Context, void bool) {
	id, err := vendorUsagePathID(c, "id")
	if response.ErrorFrom(c, err) {
		return
	}
	var in service.VendorSettlementVersionInput
	if response.ErrorFrom(c, vendorSettlementJSON(c, &in)) {
		return
	}
	ctx, cancel := vendorUsageRequestContext(c)
	defer cancel()
	var out *service.VendorSettlement
	if void {
		out, err = h.service.VoidVendorSettlement(ctx, id, in)
	} else {
		out, err = h.service.ConfirmVendorSettlement(ctx, id, in)
	}
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, out)
}
func (h *VendorSettlementHandler) Payment(c *gin.Context) {
	id, err := vendorUsagePathID(c, "id")
	if response.ErrorFrom(c, err) {
		return
	}
	var in service.VendorSettlementPaymentInput
	if response.ErrorFrom(c, vendorSettlementJSON(c, &in)) {
		return
	}
	ctx, cancel := vendorUsageRequestContext(c)
	defer cancel()
	out, err := h.service.RegisterVendorPayment(ctx, id, in)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, out)
}
func (h *VendorSettlementHandler) Reverse(c *gin.Context) {
	id, err := vendorUsagePathID(c, "id")
	if response.ErrorFrom(c, err) {
		return
	}
	paymentID, err := vendorUsagePathID(c, "payment_id")
	if response.ErrorFrom(c, err) {
		return
	}
	var in service.VendorSettlementAdjustmentInput
	if response.ErrorFrom(c, vendorSettlementJSON(c, &in)) {
		return
	}
	ctx, cancel := vendorUsageRequestContext(c)
	defer cancel()
	out, err := h.service.ReverseVendorPayment(ctx, id, paymentID, in)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, out)
}
func (h *VendorSettlementHandler) Adjustment(c *gin.Context) {
	id, err := vendorUsagePathID(c, "id")
	if response.ErrorFrom(c, err) {
		return
	}
	var in service.VendorSettlementAdjustmentInput
	if response.ErrorFrom(c, vendorSettlementJSON(c, &in)) {
		return
	}
	ctx, cancel := vendorUsageRequestContext(c)
	defer cancel()
	out, err := h.service.AdjustVendorSettlement(ctx, id, in)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Created(c, out)
}

// These paths use the authenticated L1 as payer. vendor_id filters only payees.
func (h *VendorSettlementHandler) PayableList(c *gin.Context)   { h.list(c, false, "payable") }
func (h *VendorSettlementHandler) PayableGet(c *gin.Context)    { h.get(c, false, "payable") }
func (h *VendorSettlementHandler) PayableExport(c *gin.Context) { h.export(c, false, "payable") }
