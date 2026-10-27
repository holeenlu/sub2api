package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type VendorProxyHandler struct{ service *service.VendorService }

func NewVendorProxyHandler(s *service.VendorService) *VendorProxyHandler {
	return &VendorProxyHandler{service: s}
}

func vendorProxyID(c *gin.Context) (int64, bool) {
	id, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if e != nil || id <= 0 {
		response.NotFound(c, "Proxy not found")
		return 0, false
	}
	return id, true
}
func vendorProxyJSON(c *gin.Context, out any) bool {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2*service.VendorProxyMaxBytes)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		response.Error(c, 422, "invalid request fields or body size")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		response.Error(c, 422, "request must contain one JSON object")
		return false
	}
	return true
}
func vendorProxyRespond(c *gin.Context, v any, err error) {
	c.Header("Cache-Control", "no-store")
	if err != nil {
		var public *infraerrors.Error
		if errors.Is(err, authz.ErrAuthorizationExpired) {
			response.Unauthorized(c, "Authorization expired; authenticate again")
		} else if errors.As(err, &public) {
			response.ErrorFrom(c, err)
		} else {
			response.Error(c, 503, "Proxy operation unavailable; retry")
		}
		return
	}
	response.Success(c, v)
}

// Reuse the native durable write coordinator. Live authorization precedes
// replay, and the key namespace includes actor session, vendor and policy.
func (h *VendorProxyHandler) write(c *gin.Context, action string, id int64, payload any, execute func(context.Context) (any, error)) {
	c.Header("Cache-Control", "no-store")
	ctx := c.Request.Context()
	scope, ok := authz.VendorScopeFromContext(ctx)
	if !ok || scope.VendorID <= 0 {
		vendorProxyRespond(c, nil, service.ErrProxyNotFound)
		return
	}
	if _, err := h.service.RequireVendor(ctx, scope.VendorID); err != nil {
		vendorProxyRespond(c, nil, err)
		return
	}
	actor, _ := authz.FromContext(ctx)
	coordinator := service.DefaultIdempotencyCoordinator()
	var data any
	var err error
	if coordinator == nil {
		data, err = execute(ctx)
	} else {
		var result *service.IdempotencyExecuteResult
		result, err = coordinator.Execute(ctx, service.IdempotencyExecuteOptions{
			Scope:      fmt.Sprintf("vendor.proxy.%s:%d:%d:%d:%d:%d", action, scope.VendorID, scope.PolicyVersion, actor.UserID, actor.SessionGeneration, id),
			ActorScope: fmt.Sprintf("user:%d", actor.UserID), Method: c.Request.Method, Route: c.FullPath(),
			IdempotencyKey: c.GetHeader("Idempotency-Key"), Payload: payload, RequireKey: true, TTL: 24 * time.Hour,
			ExecutionTimeout: 2 * time.Minute,
		}, execute)
		if err == nil {
			data = result.Data
			if result.Replayed {
				c.Header("X-Idempotency-Replayed", "true")
			}
		}
	}
	if err == nil {
		_, err = h.service.RequireVendor(ctx, scope.VendorID)
	}
	if retry := service.RetryAfterSecondsFromError(err); retry > 0 {
		c.Header("Retry-After", strconv.Itoa(retry))
	}
	if action == "import" && err == nil {
		data, err = expandVendorProxyImportReceipt(data)
	}
	vendorProxyRespond(c, data, err)
}

// Compact receipts keep a maximum-size import below the native coordinator's
// 64 KiB stored-response limit. Neither input credentials nor proxy metadata
// need to be stored for a replay; the list is refreshed after a committed import.
type vendorProxyImportReceipt struct {
	Digest string   `json:"digest"`
	Rows   [][3]any `json:"rows"`
}

func compactVendorProxyImportResult(r *service.VendorProxyImportResult) vendorProxyImportReceipt {
	out := vendorProxyImportReceipt{Digest: r.InputDigest, Rows: make([][3]any, 0, len(r.Rows))}
	for _, row := range r.Rows {
		out.Rows = append(out.Rows, [3]any{row.Status, row.Code, row.ProxyID})
	}
	return out
}

func expandVendorProxyImportReceipt(value any) (*service.VendorProxyImportResult, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var receipt struct {
		Digest string               `json:"digest"`
		Rows   [][3]json.RawMessage `json:"rows"`
	}
	if err = json.Unmarshal(b, &receipt); err != nil {
		return nil, err
	}
	out := &service.VendorProxyImportResult{InputDigest: receipt.Digest, Rows: []service.VendorProxyImportRow{}}
	for i, row := range receipt.Rows {
		v := service.VendorProxyImportRow{Line: i + 1}
		if err = json.Unmarshal(row[0], &v.Status); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(row[1], &v.Code); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(row[2], &v.ProxyID); err != nil {
			return nil, err
		}
		switch v.Status {
		case "created":
			out.ValidCount++
		case "duplicate":
			out.DuplicateCount++
		default:
			out.InvalidCount++
		}
		out.Rows = append(out.Rows, v)
	}
	return out, nil
}

func (h *VendorProxyHandler) List(c *gin.Context) {
	var f service.VendorProxyFilter
	if err := c.ShouldBindQuery(&f); err != nil {
		response.Error(c, 422, "invalid filter")
		return
	}
	v, e := h.service.ListVendorProxies(c.Request.Context(), f)
	vendorProxyRespond(c, v, e)
}
func (h *VendorProxyHandler) Get(c *gin.Context) {
	id, ok := vendorProxyID(c)
	if !ok {
		return
	}
	v, e := h.service.GetVendorProxy(c.Request.Context(), id)
	vendorProxyRespond(c, v, e)
}
func (h *VendorProxyHandler) Create(c *gin.Context) {
	var in service.VendorProxyInput
	if !vendorProxyJSON(c, &in) {
		return
	}
	h.write(c, "create", 0, in, func(ctx context.Context) (any, error) { return h.service.CreateVendorProxy(ctx, in) })
}
func (h *VendorProxyHandler) Update(c *gin.Context) {
	id, ok := vendorProxyID(c)
	if !ok {
		return
	}
	var in service.VendorProxyPatch
	if !vendorProxyJSON(c, &in) {
		return
	}
	h.write(c, "update", id, in, func(ctx context.Context) (any, error) { return h.service.UpdateVendorProxy(ctx, id, in) })
}
func (h *VendorProxyHandler) Delete(c *gin.Context) {
	id, ok := vendorProxyID(c)
	if !ok {
		return
	}
	e := h.service.DeleteVendorProxy(c.Request.Context(), id)
	vendorProxyRespond(c, gin.H{"deleted": e == nil}, e)
}
func (h *VendorProxyHandler) Test(c *gin.Context)         { h.probe(c, false) }
func (h *VendorProxyHandler) QualityCheck(c *gin.Context) { h.probe(c, true) }
func (h *VendorProxyHandler) probe(c *gin.Context, quality bool) {
	id, ok := vendorProxyID(c)
	if !ok {
		return
	}
	var in struct {
		Version int64 `json:"config_version"`
	}
	if !vendorProxyJSON(c, &in) {
		return
	}
	v, e := h.service.ProbeVendorProxy(c.Request.Context(), id, in.Version, quality)
	vendorProxyRespond(c, v, e)
}
func (h *VendorProxyHandler) BatchDelete(c *gin.Context) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if !vendorProxyJSON(c, &in) {
		return
	}
	v, e := h.service.BatchDeleteVendorProxies(c.Request.Context(), in.IDs)
	vendorProxyRespond(c, v, e)
}
func (h *VendorProxyHandler) ImportPreview(c *gin.Context) { h.importData(c, true) }
func (h *VendorProxyHandler) Import(c *gin.Context)        { h.importData(c, false) }
func (h *VendorProxyHandler) importData(c *gin.Context, preview bool) {
	var in service.VendorProxyImportRequest
	if !vendorProxyJSON(c, &in) {
		return
	}
	if preview {
		v, e := h.service.ImportVendorProxies(c.Request.Context(), in, true)
		vendorProxyRespond(c, v, e)
		return
	}
	h.write(c, "import", 0, in, func(ctx context.Context) (any, error) {
		r, err := h.service.ImportVendorProxies(ctx, in, false)
		if err != nil {
			return nil, err
		}
		return compactVendorProxyImportResult(r), nil
	})
}
func (h *VendorProxyHandler) Export(c *gin.Context) {
	var in service.VendorProxyExportRequest
	if !vendorProxyJSON(c, &in) {
		return
	}
	b, typ, e := h.service.ExportVendorProxies(c.Request.Context(), in)
	if e != nil {
		vendorProxyRespond(c, nil, e)
		return
	}
	ext := "json"
	if in.Format == "csv" {
		ext = "csv"
	}
	c.Header("Content-Disposition", "attachment; filename=proxies."+ext)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, typ, b)
}
