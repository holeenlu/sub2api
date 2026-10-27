package admin

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ModelManagementHandler struct {
	settings  *service.SettingService
	accounts  service.AccountRepository
	admin     service.AdminService
	discovery *service.AccountTestService
	billing   *service.BillingService
}

func NewModelManagementHandler(settings *service.SettingService, accounts service.AccountRepository, admin service.AdminService, discovery *service.AccountTestService, billing *service.BillingService) *ModelManagementHandler {
	return &ModelManagementHandler{settings, accounts, admin, discovery, billing}
}
func (h *ModelManagementHandler) Get(c *gin.Context) {
	p, err := h.settings.GetModelPolicy(c.Request.Context(), c.Param("platform"), true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, p)
}
func (h *ModelManagementHandler) Save(c *gin.Context) {
	var p service.ModelPolicy
	if err := c.ShouldBindJSON(&p); err != nil {
		response.BadRequest(c, "Invalid model policy")
		return
	}
	saved, err := h.settings.SaveModelPolicy(c.Request.Context(), c.Param("platform"), p)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, saved)
}
func (h *ModelManagementHandler) Reset(c *gin.Context) {
	var req struct {
		Version int64 `json:"version"`
	}
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "Invalid version")
		return
	}
	p, err := h.settings.ResetModelPolicy(c.Request.Context(), c.Param("platform"), req.Version)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, p)
}
func (h *ModelManagementHandler) Catalog(c *gin.Context) {
	p, err := h.settings.ModelReferenceCatalog(c.Request.Context(), c.DefaultQuery("platform", service.PlatformAnthropic))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, p)
}
func (h *ModelManagementHandler) Sources(c *gin.Context) {
	accounts, err := h.accounts.ListByPlatform(c.Request.Context(), c.Query("platform"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	rows := []gin.H{}
	for _, a := range accounts {
		rows = append(rows, gin.H{"id": a.ID, "name": a.Name, "platform": a.Platform, "type": a.Type})
	}
	response.Success(c, rows)
}
func (h *ModelManagementHandler) Sync(c *gin.Context) {
	var req struct {
		Version    int64   `json:"version"`
		AccountIDs []int64 `json:"account_ids"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.AccountIDs) == 0 || len(req.AccountIDs) > 20 {
		response.BadRequest(c, "Select between one and twenty catalog sources")
		return
	}
	platform := c.Param("platform")
	// Resolve and validate every source before contacting any upstream.
	accounts := make([]*service.Account, 0, len(req.AccountIDs))
	seen := map[int64]bool{}
	for _, id := range req.AccountIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		a, err := h.admin.GetAccount(c.Request.Context(), id)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if a.Platform != platform {
			response.BadRequest(c, "Catalog source platform mismatch")
			return
		}
		accounts = append(accounts, a)
	}
	ctx, cancel := contextWithModelSyncTimeout(c)
	defer cancel()
	catalogs := map[int64]*service.UpstreamModelCatalog{}
	for _, a := range accounts {
		catalog, err := h.discovery.SyncUpstreamModelCatalog(ctx, a)
		if err != nil {
			response.Error(c, 502, "Failed to sync model catalog; saved policy is unchanged")
			return
		}
		catalogs[a.ID] = catalog
	}
	p, err := h.settings.SyncModelPolicy(ctx, platform, req.Version, catalogs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, p)
}
func (h *ModelManagementHandler) GetMappings(c *gin.Context) {
	p, err := h.settings.GetModelMappings(c.Request.Context(), true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, p)
}
func (h *ModelManagementHandler) SaveMappings(c *gin.Context) {
	var p service.ModelMappings
	if c.ShouldBindJSON(&p) != nil {
		response.BadRequest(c, "Invalid model mappings")
		return
	}
	saved, err := h.settings.SaveModelMappings(c.Request.Context(), p)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, saved)
}
func (h *ModelManagementHandler) Prices(c *gin.Context) {
	p, err := h.settings.GetManagedPrices(c.Request.Context(), true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	policy, err := h.settings.GetModelPolicy(c.Request.Context(), c.DefaultQuery("platform", service.PlatformAnthropic), false)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	rows := []gin.H{}
	for _, id := range policy.IDs(false) {
		row := gin.H{"id": id, "source": "builtin", "effective": nil, "override": nil}
		if price, err := h.billing.GetModelPricing(id); err == nil {
			row["effective"] = gin.H{"input": price.InputPricePerToken * 1e6, "output": price.OutputPricePerToken * 1e6, "cache_read": price.CacheReadPricePerToken * 1e6, "cache_5m": price.CacheCreation5mPrice * 1e6, "cache_1h": price.CacheCreation1hPrice * 1e6}
		}
		if external, ok := p.External[id]; ok {
			row["effective"] = external
			row["source"] = "models.dev"
		}
		if override, ok := p.Overrides[id]; ok {
			row["effective"] = override
			row["override"] = override
			row["source"] = "override"
		}
		rows = append(rows, row)
	}
	response.Success(c, gin.H{"version": p.Version, "synced_at": p.SyncedAt, "entries": rows})
}
func (h *ModelManagementHandler) SavePrice(c *gin.Context) {
	var req struct {
		Platform string                `json:"platform"`
		Model    string                `json:"model"`
		Version  int64                 `json:"version"`
		Price    *service.ManagedPrice `json:"price"`
	}
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "Invalid price")
		return
	}
	p, err := h.settings.SaveManagedPrice(c.Request.Context(), req.Platform, req.Model, req.Version, req.Price)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"version": p.Version})
}
func (h *ModelManagementHandler) SyncPrices(c *gin.Context) {
	var req struct {
		Platform string `json:"platform"`
		Version  int64  `json:"version"`
	}
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "Invalid price sync request")
		return
	}
	accounts, listErr := h.accounts.ListActive(c.Request.Context())
	if listErr != nil {
		response.ErrorFrom(c, listErr)
		return
	}
	var source *service.Account
	for i := range accounts {
		if accounts[i].Platform == req.Platform {
			source = &accounts[i]
			break
		}
	}
	if source == nil {
		response.BadRequest(c, "No active account is available for this provider")
		return
	}
	p, err := h.discovery.SyncManagedPrices(c.Request.Context(), h.settings, req.Platform, req.Version, source)
	if err != nil {
		response.Error(c, 502, "Failed to sync complete model prices")
		return
	}
	response.Success(c, gin.H{"version": p.Version})
}

// A narrow endpoint keeps checkbox saves independent of the account form.
func (h *ModelManagementHandler) AccountAllowlist(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	a, err := h.admin.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var req struct {
		Models   []string `json:"models"`
		Expected []string `json:"expected"`
	}
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "Invalid model references")
		return
	}
	if err = h.settings.UpdateAccountModelReferences(c.Request.Context(), h.accounts, a, req.Expected, req.Models); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"models": a.ModelReferences()})
}

// Keep catalog operations bounded even when an upstream never responds.
func contextWithModelSyncTimeout(c *gin.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Request.Context(), 60*time.Second)
}
