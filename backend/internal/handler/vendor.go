package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type VendorHandler struct {
	service *service.VendorService
	totp    *service.TotpService
	users   *service.UserService
}

func NewVendorHandler(s *service.VendorService, t *service.TotpService, u *service.UserService) *VendorHandler {
	return &VendorHandler{service: s, totp: t, users: u}
}
func (h *VendorHandler) SuperOnly(c *gin.Context) {
	if err := h.service.RequireSuperAdmin(c.Request.Context()); err != nil {
		vendorAccountReply(c, nil, err)
		c.Abort()
		return
	}
	c.Next()
}
func (h *VendorHandler) bind(c *gin.Context, v *service.Vendor, admin bool) bool {
	base := c.Request.Context()
	actor, ok := authz.FromContext(base)
	if !ok {
		response.Unauthorized(c, "Authentication required")
		return false
	}
	readOnly := !admin && v.UserID != actor.UserID
	scope := authz.VendorScope{VendorID: v.ID, PolicyVersion: v.PolicyVersion, ActorUserID: actor.UserID, Admin: admin, ReadOnly: readOnly}
	scoped := authz.WithVendorScope(base, scope)
	scoped = authz.WithRevalidation(scoped, func(_ context.Context) error {
		var fresh *service.Vendor
		var err error
		if readOnly {
			fresh, err = h.service.ReadVendorScope(base, v.ID)
		} else {
			fresh, err = h.service.RequireVendor(base, v.ID)
		}
		if err != nil {
			return err
		}
		if fresh.PolicyVersion != v.PolicyVersion {
			return service.ErrVendorConflict
		}
		return nil
	})
	c.Request = c.Request.WithContext(scoped)
	c.Header("Cache-Control", "no-store")
	return true
}
func (h *VendorHandler) Scope(c *gin.Context) {
	actor, ok := authz.FromContext(c.Request.Context())
	if !ok || !authz.IsVendorRole(actor.Role) {
		response.Forbidden(c, "Supplier login required")
		c.Abort()
		return
	}
	v, err := h.service.CurrentVendor(c.Request.Context())
	if inventoryScope, chosen := c.GetQuery("scope"); chosen {
		_, hasVendor := c.GetQuery("vendor_id")
		if err != nil || inventoryScope != "children" || hasVendor || actor.Role != authz.VendorLevel1 || c.Request.Method != http.MethodGet || (c.FullPath() != "/api/v1/vendor/accounts" && c.FullPath() != "/api/v1/vendor/proxies") {
			vendorAccountReply(c, nil, service.ErrVendorDenied)
			c.Abort()
			return
		}
		if !h.bind(c, v, false) {
			c.Abort()
			return
		}
		scope, _ := authz.VendorScopeFromContext(c.Request.Context())
		scope.ReadOnly, scope.Children = true, true
		c.Request = c.Request.WithContext(authz.WithVendorScope(c.Request.Context(), scope))
		c.Next()
		return
	}
	if raw, chosen := c.GetQuery("vendor_id"); chosen {
		id, e := strconv.ParseInt(raw, 10, 64)
		financialRead := c.Request.Method == http.MethodGet && c.FullPath() == "/api/v1/vendor/payables"
		if e != nil || id <= 0 || c.Request.Method != http.MethodGet || (!financialRead && !vendorChildReadRoute(c.FullPath())) {
			vendorAccountReply(c, nil, service.ErrVendorDenied)
			c.Abort()
			return
		}
		if err == nil && !financialRead {
			v, err = h.service.ReadVendorScope(c.Request.Context(), id)
		}
	}
	if err != nil {
		vendorAccountReply(c, nil, err)
		c.Abort()
		return
	}
	if !h.bind(c, v, false) {
		c.Abort()
		return
	}
	c.Next()
}

// AdminScope accepts a supplier only after live SuperOnly validation. The browser
// can choose a scope; it never supplies ownership or an authorization lease.
func (h *VendorHandler) AdminScope(c *gin.Context) {
	h.adminScope(c, false)
}

// AdminReadScope permits an aggregate inventory read, never an unowned mutation.
func (h *VendorHandler) AdminReadScope(c *gin.Context) {
	h.adminScope(c, true)
}

func (h *VendorHandler) adminScope(c *gin.Context, aggregate bool) {
	if err := h.service.RequireSuperAdmin(c.Request.Context()); err != nil {
		vendorAccountReply(c, nil, err)
		c.Abort()
		return
	}
	id, err := strconv.ParseInt(c.DefaultQuery("vendor_id", "0"), 10, 64)
	if err != nil || id < 0 || (id == 0 && !aggregate) {
		vendorAccountReply(c, nil, service.ErrVendorInput)
		c.Abort()
		return
	}
	if id == 0 {
		actor, _ := authz.FromContext(c.Request.Context())
		c.Request = c.Request.WithContext(authz.WithVendorScope(c.Request.Context(), authz.VendorScope{ActorUserID: actor.UserID, Admin: true}))
		c.Header("Cache-Control", "no-store")
		c.Next()
		return
	}
	v, err := h.service.RequireVendor(c.Request.Context(), id)
	if err != nil {
		vendorAccountReply(c, nil, err)
		c.Abort()
		return
	}
	if !h.bind(c, v, true) {
		c.Abort()
		return
	}
	c.Next()
}
func (h *VendorHandler) Dispatch(kind string, native, scoped gin.HandlerFunc) gin.HandlerFunc {
	if h == nil {
		return native
	}
	return func(c *gin.Context) {
		raw, chosen := c.GetQuery("vendor_id")
		if !chosen {
			native(c)
			return
		}
		if err := h.service.RequireSuperAdmin(c.Request.Context()); err != nil {
			vendorAccountReply(c, nil, err)
			return
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 0 {
			vendorAccountReply(c, nil, service.ErrVendorInput)
			return
		}
		if object, err := strconv.ParseInt(c.Param("id"), 10, 64); err == nil && object > 0 {
			owner, e := h.service.ResourceVendorID(c.Request.Context(), kind, object)
			if e != nil {
				vendorAccountReply(c, nil, e)
				return
			}
			if id != 0 && owner != id {
				vendorAccountReply(c, nil, service.ErrVendorNotFound)
				return
			}
			id = owner
		}
		if id == 0 {
			actor, _ := authz.FromContext(c.Request.Context())
			c.Request = c.Request.WithContext(authz.WithVendorScope(c.Request.Context(), authz.VendorScope{ActorUserID: actor.UserID, Admin: true}))
			scoped(c)
			return
		}
		v, err := h.service.RequireVendor(c.Request.Context(), id)
		if err != nil {
			vendorAccountReply(c, nil, err)
			return
		}
		if !h.bind(c, v, true) {
			return
		}
		scoped(c)
	}
}
func (h *VendorHandler) ExportStepUp(c *gin.Context) {
	if !middleware.EnforceStepUpAlways(c, h.totp, h.users) {
		c.Abort()
		return
	}
	if err := authz.Revalidate(c.Request.Context()); err != nil {
		vendorAccountReply(c, nil, err)
		c.Abort()
		return
	}
	c.Set(VendorAccountExportStepUpMarker, true)
	c.Next()
}
func (h *VendorHandler) Me(c *gin.Context) {
	v, err := h.service.CurrentVendor(c.Request.Context())
	if err != nil {
		vendorAccountReply(c, nil, err)
		return
	}
	response.Success(c, struct {
		*service.Vendor
		SupportedAuthMethods map[string][]string `json:"supported_auth_methods"`
		ProxyProtocols       []string            `json:"proxy_protocols"`
	}{v, map[string][]string{"anthropic": {"oauth", "setup-token", "setup-token-manual"}, "openai": {"oauth", "refresh_token"}}, []string{"http", "https", "socks5"}})
}
func vendorPage(c *gin.Context) (int, int) {
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	n, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	return p, n
}
func (h *VendorHandler) List(c *gin.Context) {
	p, n := vendorPage(c)
	filter := service.VendorListFilter{}
	if raw, ok := c.GetQuery("tier"); ok {
		tier, err := strconv.Atoi(raw)
		if err != nil || (tier != 1 && tier != 2) {
			vendorAccountReply(c, nil, service.ErrVendorInput)
			return
		}
		filter.Tier = tier
	}
	if raw, ok := c.GetQuery("parent_vendor_id"); ok {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			vendorAccountReply(c, nil, service.ErrVendorInput)
			return
		}
		filter.ParentVendorID = &id
	}
	items, total, err := h.service.ListVendors(c.Request.Context(), p, n, c.Query("search"), c.Query("status"), filter)
	vendorAccountReply(c, gin.H{"items": items, "total": total, "page": p, "page_size": n}, err)
}
func (h *VendorHandler) Get(c *gin.Context) {
	id, ok := vendorAccountID(c)
	if !ok {
		return
	}
	v, err := h.service.ManagedVendor(c.Request.Context(), id)
	vendorAccountReply(c, v, err)
}
func (h *VendorHandler) Create(c *gin.Context) { h.save(c, 0) }
func (h *VendorHandler) Update(c *gin.Context) {
	id, ok := vendorAccountID(c)
	if ok {
		h.save(c, id)
	}
}
func (h *VendorHandler) save(c *gin.Context, id int64) {
	var raw map[string]json.RawMessage
	if !vendorAccountBind(c, &raw) {
		return
	}
	if raw == nil {
		vendorAccountReply(c, nil, service.ErrVendorInput)
		return
	}
	// Ownership and server versions cannot be supplied as new policy values.
	for _, key := range []string{"id", "user_id", "contract_version", "account_count", "proxy_count", "groups", "created_at", "updated_at"} {
		if _, ok := raw[key]; ok {
			vendorAccountReply(c, nil, service.ErrVendorInput)
			return
		}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		vendorAccountReply(c, nil, service.ErrVendorInput)
		return
	}
	if id == 0 {
		if _, ok := raw["policy_version"]; ok {
			vendorAccountReply(c, nil, service.ErrVendorInput)
			return
		}
		policy := service.NewVendorPolicy()
		actor, _ := authz.FromContext(c.Request.Context())
		if actor.Role == authz.VendorLevel1 {
			policy.Tier = 2
		}
		in := service.VendorCreateInput{Vendor: policy}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&in); err != nil {
			vendorAccountReply(c, nil, service.ErrVendorInput)
			return
		}
		out, err := h.service.CreateVendor(c.Request.Context(), in)
		vendorAccountReply(c, out, err)
		return
	}
	for _, key := range []string{"email", "password", "username"} {
		if _, ok := raw[key]; ok {
			vendorAccountReply(c, nil, service.ErrVendorInput)
			return
		}
	}
	current, err := h.service.ManagedVendor(c.Request.Context(), id)
	if err != nil {
		vendorAccountReply(c, nil, err)
		return
	}
	input := *current
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		vendorAccountReply(c, nil, service.ErrVendorInput)
		return
	}
	out, err := h.service.SaveVendor(c.Request.Context(), id, input)
	vendorAccountReply(c, out, err)
}

func vendorChildReadRoute(path string) bool {
	switch path {
	case "/api/v1/vendor/accounts", "/api/v1/vendor/accounts/:id", "/api/v1/vendor/accounts/:id/usage",
		"/api/v1/vendor/proxies", "/api/v1/vendor/proxies/:id",
		"/api/v1/vendor/usage/summary", "/api/v1/vendor/usage/trend", "/api/v1/vendor/usage/accounts":
		return true
	}
	return false
}
func (h *VendorHandler) Audit(c *gin.Context) {
	p, n := vendorPage(c)
	id, _ := strconv.ParseInt(c.Query("vendor_id"), 10, 64)
	items, total, err := h.service.VendorAuditLogs(c.Request.Context(), id, p, n, c.Query("action"), c.Query("start_date"), c.Query("end_date"))
	vendorAccountReply(c, gin.H{"items": items, "total": total, "page": p, "page_size": n}, err)
}
func (h *VendorHandler) GroupSupply(c *gin.Context) {
	p, n := vendorPage(c)
	id, _ := strconv.ParseInt(c.Query("vendor_id"), 10, 64)
	items, total, err := h.service.VendorGroupSupply(c.Request.Context(), id, p, n, c.Query("search"))
	vendorAccountReply(c, gin.H{"items": items, "total": total, "page": p, "page_size": n}, err)
}
