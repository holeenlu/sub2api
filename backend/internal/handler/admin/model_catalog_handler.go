package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ModelCatalogHandler struct {
	catalog  *service.ModelCatalogService
	accounts service.AccountRepository
}

func NewModelCatalogHandler(catalog *service.ModelCatalogService, accounts service.AccountRepository) *ModelCatalogHandler {
	return &ModelCatalogHandler{catalog: catalog, accounts: accounts}
}

func (h *ModelCatalogHandler) Catalog(c *gin.Context) {
	if h == nil || h.catalog == nil {
		response.Error(c, http.StatusServiceUnavailable, "Model catalog is unavailable")
		return
	}
	if raw := c.Query("account_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid account ID")
			return
		}
		a, err := h.accounts.GetByID(c.Request.Context(), id)
		if err != nil || a == nil {
			response.NotFound(c, "Account not found")
			return
		}
		var snapshot *service.ModelCatalogSnapshot
		if c.Query("view") == "selection" {
			snapshot, err = h.catalog.SelectionCatalog(c.Request.Context(), a, a.Platform)
		} else {
			snapshot, err = h.catalog.Account(c.Request.Context(), a)
		}
		if err != nil {
			response.InternalError(c, "Failed to read model catalog")
			return
		}
		c.Header("Cache-Control", "private, no-store")
		response.Success(c, snapshot)
		return
	}
	platform := strings.TrimSpace(c.Query("platform"))
	var snapshot *service.ModelCatalogSnapshot
	var err error
	if c.Query("view") == "selection" {
		snapshot, err = h.catalog.SelectionCatalog(c.Request.Context(), nil, platform)
	} else {
		snapshot, err = h.catalog.Inventory(c.Request.Context(), platform)
	}
	if err != nil {
		response.InternalError(c, "Failed to read model catalog")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, snapshot)
}

func (h *ModelCatalogHandler) Refresh(c *gin.Context) {
	var input struct {
		AccountID int64 `json:"account_id" binding:"required,gt=0"`
	}
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "account_id is required")
		return
	}
	a, err := h.accounts.GetByID(c.Request.Context(), input.AccountID)
	if err != nil || a == nil {
		response.NotFound(c, "Account not found")
		return
	}
	response.Success(c, h.catalog.QueueRefresh(input.AccountID))
}

func (h *ModelCatalogHandler) Sync(c *gin.Context) {
	response.Success(c, h.catalog.QueueSync())
}

func (h *ModelCatalogHandler) Job(c *gin.Context) {
	job, ok := h.catalog.Job(c.Request.Context(), c.Param("job_id"))
	if !ok {
		response.NotFound(c, "Refresh job not found; read the persisted catalog")
		return
	}
	response.Success(c, job)
}

func (h *ModelCatalogHandler) Settings(c *gin.Context) {
	cfg := h.catalog.Settings(c.Request.Context())
	response.Success(c, gin.H{"enabled": cfg.Enabled, "interval_seconds": cfg.IntervalSeconds})
}
func (h *ModelCatalogHandler) SaveSettings(c *gin.Context) {
	var input struct {
		Enabled         bool `json:"enabled"`
		IntervalSeconds int  `json:"interval_seconds"`
	}
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "Invalid synchronization settings")
		return
	}
	cfg := h.catalog.Settings(c.Request.Context())
	cfg.Enabled, cfg.IntervalSeconds = input.Enabled, input.IntervalSeconds
	if err := h.catalog.SaveSettings(c.Request.Context(), cfg); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, input)
}
func (h *ModelCatalogHandler) TicketModels(c *gin.Context) {
	id, ok := codexTicketAccountID(c)
	if !ok {
		return
	}
	a, err := h.accounts.GetByID(c.Request.Context(), id)
	if err != nil || a == nil {
		response.NotFound(c, "Account not found")
		return
	}
	models, err := h.catalog.TicketCandidates(c.Request.Context(), a)
	if err != nil {
		response.InternalError(c, "Catalog unavailable")
		return
	}
	response.Success(c, models)
}

func (h *ModelCatalogHandler) PricingAudits(c *gin.Context) {
	rows, err := h.catalog.PricingAudits(c.Request.Context(), c.Query("pending") == "true")
	if err != nil {
		response.InternalError(c, "Pricing evidence unavailable")
		return
	}
	response.Success(c, rows)
}

func (h *ModelCatalogHandler) SaveModel(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var input service.CatalogModelInput
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "Invalid model")
		return
	}
	if err := h.catalog.SaveInventoryModel(c.Request.Context(), input); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, input)
}
