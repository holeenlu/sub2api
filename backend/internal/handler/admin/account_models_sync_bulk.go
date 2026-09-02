package admin

import (
	"errors"
	"log/slog"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type syncAnthropicModelsBulkRequest struct {
	AccountIDs  []int64                   `json:"account_ids"`
	Filters     *BulkUpdateAccountFilters `json:"filters"`
	Aggregation string                    `json:"aggregation" binding:"omitempty,oneof=union intersection"`
	// RequireAll 为 true 时，任何一个账号拉不到 /v1/models 都让整批失败。默认
	// false：部分账号失效是常态，把能拿到的结果连同失败明细一起返回，管理员
	// 自己判断要不要用。
	RequireAll bool `json:"require_all"`
}

// SyncAnthropicModelsBulk returns the live Anthropic /v1/models catalog for a
// batch of accounts. Intersection is meant for applying one whitelist to every
// selected account; union is meant for group-level routing catalogs.
// POST /api/v1/admin/accounts/models/sync-anthropic-bulk
func (h *AccountHandler) SyncAnthropicModelsBulk(c *gin.Context) {
	var req syncAnthropicModelsBulkRequest
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

	accounts, ok := h.resolveAnthropicModelSyncAccounts(c, req)
	if !ok {
		return
	}

	aggregation := anthropicModelAggregationIntersection
	if req.Aggregation == string(anthropicModelAggregationUnion) {
		aggregation = anthropicModelAggregationUnion
	}
	result, err := fetchAnthropicModelsFromAccounts(
		c.Request.Context(),
		h.accountTestService,
		accounts,
		aggregation,
		req.RequireAll,
	)
	if err != nil {
		slog.Warn("sync_anthropic_models_bulk_failed", "account_count", len(accounts), "error", err)
		// 这些错误全部来自本文件与 anthropic_models_sync.go 的固定文案，逐账号的
		// 上游错误已经在 SafeMessage 里脱敏过，可以原样回给管理端。
		if errors.Is(err, errAnthropicModelSyncNoAccounts) {
			response.BadRequest(c, err.Error())
			return
		}
		// 「具体哪个账号、什么原因」正是这个接口的价值所在，而错误响应没有 data
		// 字段可以携带明细。整批失败时改为 200 带上 error 与 failures，模型列表为
		// 空——前端据 error 判定失败，同时还能把逐账号明细摆出来。
		writeAnthropicModelsBulkResponse(c, anthropicModelSyncResult{Failures: result.Failures},
			len(accounts), aggregation, err.Error())
		return
	}

	writeAnthropicModelsBulkResponse(c, result, len(accounts), aggregation, "")
}

func writeAnthropicModelsBulkResponse(
	c *gin.Context,
	result anthropicModelSyncResult,
	accountCount int,
	aggregation anthropicModelAggregation,
	errMessage string,
) {
	models := result.Models
	if models == nil {
		models = []string{}
	}
	failures := result.Failures
	if failures == nil {
		failures = []anthropicAccountModelFailure{}
	}
	body := gin.H{
		"models":        models,
		"failures":      failures,
		"account_count": accountCount,
		"aggregation":   aggregation,
		"source":        "anthropic_v1_models",
	}
	if errMessage != "" {
		body["error"] = errMessage
	}
	response.Success(c, body)
}

// resolveAnthropicModelSyncAccounts 把请求的选择条件解成账号列表，并在写响应后
// 返回 ok=false。筛选条件走 ResolveBulkUpdateTargetIDs，与批量编辑同一套语义，
// 免得两个入口对 "ungrouped"、隐私模式这些边角条件各有一套理解。
func (h *AccountHandler) resolveAnthropicModelSyncAccounts(c *gin.Context, req syncAnthropicModelsBulkRequest) ([]*service.Account, bool) {
	ctx := c.Request.Context()

	accountIDs := req.AccountIDs
	if len(accountIDs) == 0 {
		resolved, err := h.adminService.ResolveBulkUpdateTargetIDs(ctx, toServiceBulkUpdateAccountFilters(req.Filters))
		if err != nil {
			// 展开筛选条件的失败可能来自数据库，错误原文不回显给客户端。
			slog.Warn("sync_anthropic_models_bulk_resolve_targets_failed", "error", err)
			response.InternalError(c, "Failed to resolve the selected accounts")
			return nil, false
		}
		accountIDs = resolved
	}
	if len(accountIDs) == 0 {
		response.BadRequest(c, "No Anthropic accounts matched the selection")
		return nil, false
	}

	accounts, err := h.adminService.GetAccountsByIDs(ctx, accountIDs)
	if err != nil {
		slog.Warn("sync_anthropic_models_bulk_load_accounts_failed", "error", err)
		response.InternalError(c, "Failed to load the selected accounts")
		return nil, false
	}
	if len(accounts) == 0 {
		response.BadRequest(c, "No Anthropic accounts matched the selection")
		return nil, false
	}
	for _, account := range accounts {
		if account == nil || !account.IsAnthropic() {
			response.BadRequest(c, "Live model sync requires an Anthropic-only account selection")
			return nil, false
		}
	}
	return accounts, true
}
