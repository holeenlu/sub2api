package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ModelCatalogHandler struct {
	catalog  *service.ModelCatalogService
	groups   service.GroupRepository
	accounts service.AccountRepository
}

func NewModelCatalogHandler(catalog *service.ModelCatalogService, accounts service.AccountRepository, groups service.GroupRepository) *ModelCatalogHandler {
	return &ModelCatalogHandler{catalog: catalog, accounts: accounts, groups: groups}
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
		snapshot, err := h.catalog.Account(c.Request.Context(), a)
		if err != nil {
			response.InternalError(c, "Failed to read model catalog")
			return
		}
		c.Header("Cache-Control", "private, no-store")
		response.Success(c, snapshot)
		return
	}
	platform := strings.TrimSpace(c.Query("platform"))
	snapshot, err := h.catalog.Platform(c.Request.Context(), platform)
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

func (h *ModelCatalogHandler) Job(c *gin.Context) {
	job, ok := h.catalog.Job(c.Request.Context(), c.Param("job_id"))
	if !ok {
		response.NotFound(c, "Refresh job not found; read the persisted catalog")
		return
	}
	response.Success(c, job)
}

func (h *ModelCatalogHandler) Settings(c *gin.Context) {
	response.Success(c, h.catalog.Settings(c.Request.Context()))
}
func (h *ModelCatalogHandler) SaveSettings(c *gin.Context) {
	var input service.ModelCatalogSettings
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "Invalid synchronization settings")
		return
	}
	if err := h.catalog.SaveSettings(c.Request.Context(), input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, input)
}
func (h *ModelCatalogHandler) Prices(c *gin.Context) {
	response.Success(c, h.catalog.ReferencePrices())
}
func (h *ModelCatalogHandler) SavePrices(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<20)
	var input struct {
		Models json.RawMessage `json:"models"`
	}
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "Invalid supplemental reference prices")
		return
	}
	if err := h.catalog.ImportPrices(c.Request.Context(), input.Models); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, h.catalog.ReferencePrices())
}
func (h *ModelCatalogHandler) Registry(c *gin.Context) {
	response.Success(c, h.catalog.Registry(c.Request.Context()))
}
func (h *ModelCatalogHandler) SaveRegistry(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20)
	var input struct {
		Models []service.ModelCatalogEntry `json:"models"`
	}
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "Invalid model registry")
		return
	}
	if err := h.catalog.SaveRegistry(c.Request.Context(), input.Models); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"models": h.catalog.Registry(c.Request.Context())})
}

func (h *ModelCatalogHandler) SaveAccountPolicy(c *gin.Context) {
	id, ok := codexTicketAccountID(c)
	if !ok {
		return
	}
	var input service.ModelCatalogPolicy
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "Invalid model policy")
		return
	}
	if err := h.catalog.SaveAccountPolicy(c.Request.Context(), id, input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, input)
}

func (h *ModelCatalogHandler) History(c *gin.Context) {
	id, ok := codexTicketAccountID(c)
	if !ok {
		return
	}
	items, err := h.catalog.History(c.Request.Context(), id)
	if err != nil {
		response.BadRequest(c, "Catalog history unavailable")
		return
	}
	response.Success(c, gin.H{"items": items})
}
func (h *ModelCatalogHandler) Rollback(c *gin.Context) {
	id, ok := codexTicketAccountID(c)
	if !ok {
		return
	}
	var input struct {
		Revision string `json:"revision" binding:"required"`
	}
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "revision is required")
		return
	}
	if err := h.catalog.Rollback(c.Request.Context(), id, input.Revision); err != nil {
		response.BadRequest(c, "Cannot restore revision outside the current account access scope")
		return
	}
	response.Success(c, gin.H{"revision": input.Revision})
}

func (h *ModelCatalogHandler) Explain(c *gin.Context) {
	id, err := strconv.ParseInt(c.Query("group_id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "group_id is required")
		return
	}
	group, err := h.groups.GetByID(c.Request.Context(), id)
	if err != nil || group == nil {
		response.NotFound(c, "Group not found")
		return
	}
	result, err := h.catalog.Explain(c.Request.Context(), group)
	if err != nil {
		response.InternalError(c, "Catalog comparison unavailable")
		return
	}
	response.Success(c, result)
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
