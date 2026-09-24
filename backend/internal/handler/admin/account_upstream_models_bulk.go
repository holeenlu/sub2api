package admin

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type syncUpstreamModelsBulkRequest struct {
	AccountIDs []int64                   `json:"account_ids"`
	Filters    *BulkUpdateAccountFilters `json:"filters"`
}

type upstreamModelsBulkFailure struct {
	AccountID int64  `json:"account_id"`
	Name      string `json:"name"`
	Error     string `json:"error"`
}

// SyncUpstreamModelsBulk fetches fresh model IDs without changing account
// whitelists. Only a complete intersection can be applied to every target.
func (h *AccountHandler) SyncUpstreamModelsBulk(c *gin.Context) {
	var req syncUpstreamModelsBulkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len(req.AccountIDs) == 0 && req.Filters == nil {
		response.BadRequest(c, "account_ids or filters is required")
		return
	}
	if h.accountTestService == nil {
		response.InternalError(c, "Account test service is not configured")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	ids := req.AccountIDs
	if len(ids) == 0 {
		var err error
		ids, err = h.adminService.ResolveBulkUpdateTargetIDs(ctx, toServiceBulkUpdateAccountFilters(req.Filters))
		if err != nil {
			slog.Warn("bulk_upstream_models_resolve_failed", "error", err)
			response.InternalError(c, "Failed to resolve the selected accounts")
			return
		}
	}
	uniqueIDs := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 {
			response.BadRequest(c, "Invalid account ID")
			return
		}
		if !seen[id] {
			uniqueIDs = append(uniqueIDs, id)
			seen[id] = true
		}
	}
	if len(uniqueIDs) == 0 {
		response.BadRequest(c, "No accounts matched the selection")
		return
	}
	accounts, err := h.adminService.GetAccountsByIDs(ctx, uniqueIDs)
	if err != nil {
		slog.Warn("bulk_upstream_models_load_failed", "error", err)
		response.InternalError(c, "Failed to load the selected accounts")
		return
	}
	byID := make(map[int64]*service.Account, len(accounts))
	for _, account := range accounts {
		if account != nil {
			byID[account.ID] = account
		}
	}
	// Keep missing/deleted targets in the result instead of silently narrowing
	// the selection and returning models not verified for every selected ID.
	targets := make([]*service.Account, len(uniqueIDs))
	for i, id := range uniqueIDs {
		targets[i] = byID[id]
	}
	modelLists, failures := fetchBulkUpstreamModels(ctx, uniqueIDs, targets, h.accountTestService.FetchUpstreamSupportedModels)
	models := []string{}
	errMessage := ""
	if len(failures) > 0 {
		errMessage = "Failed to fetch upstream models for every selected account"
	} else {
		models = intersectSyncedModelIDs(modelLists)
		if len(models) == 0 {
			errMessage = "Selected accounts have no common upstream models"
		}
	}
	// Use the same structured failure convention as Anthropic bulk sync, so
	// the UI can display per-account errors without modifying the whitelist.
	body := gin.H{"models": models, "failures": failures, "account_count": len(uniqueIDs), "aggregation": "intersection", "source": "upstream_models"}
	if errMessage != "" {
		body["error"] = errMessage
	}
	response.Success(c, body)
}

func fetchBulkUpstreamModels(ctx context.Context, ids []int64, accounts []*service.Account, fetch func(context.Context, *service.Account) ([]string, error)) ([][]string, []upstreamModelsBulkFailure) {
	modelLists := make([][]string, len(ids))
	errors := make([]string, len(ids))
	jobs := make(chan int, len(ids))
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	for range min(4, len(ids)) {
		wg.Go(func() {
			for i := range jobs {
				account := accounts[i]
				if account == nil {
					errors[i] = "Account not found"
					continue
				}
				accountCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				if err := accountCtx.Err(); err != nil {
					errors[i] = anthropicModelSyncFailureMessage(err)
				} else {
					models, err := fetch(accountCtx, account)
					if err != nil {
						errors[i] = anthropicModelSyncFailureMessage(err)
					} else if len(models) == 0 {
						errors[i] = "Upstream returned no supported models"
					} else {
						modelLists[i] = models
					}
				}
				cancel()
			}
		})
	}
	wg.Wait()
	failures := make([]upstreamModelsBulkFailure, 0)
	for i, message := range errors {
		if message == "" {
			continue
		}
		failure := upstreamModelsBulkFailure{AccountID: ids[i], Error: message}
		if accounts[i] != nil {
			failure.Name = accounts[i].Name
		}
		failures = append(failures, failure)
	}
	return modelLists, failures
}
