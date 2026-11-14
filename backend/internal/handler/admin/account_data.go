package admin

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"log/slog"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	dataType       = "sub2api-data"
	legacyDataType = "sub2api-bundle"
	dataVersion    = 2
	dataPageCap    = 1000
)

type DataPayload struct {
	Type       string        `json:"type,omitempty" authz:"base"`
	Version    int           `json:"version,omitempty" authz:"base"`
	ExportedAt string        `json:"exported_at" authz:"base"`
	Proxies    []DataProxy   `json:"proxies" authz:"proxies.manage,nonempty"`
	Accounts   []DataAccount `json:"accounts" authz:"object"`
	// SkippedShadows 记录导出时被排除的 spark 影子账号数量(见 ExportData)。仅作可见性提示,
	// 导入侧忽略该字段;omitempty 保持向后兼容。
	SkippedShadows int `json:"skipped_shadows,omitempty" authz:"base"`
}

type DataProxy = service.AccountDataProxy

// DataAccount 是管理员显式备份导出使用的账号结构，故意不走 dto.Account 的脱敏路径，
// Credentials 原文返回。这是"管理员备份"这一显式行为的一部分；如未来需要导出脱敏版本，
// 应新增独立结构而非修改这里。
// 注意:本结构不含 parent_account_id/quota_dimension——spark 影子账号在 ExportData 处被显式
// 排除(影子不持凭据、通用凭据型导入强制 credentials 非空无法重建父子链接),不在此表达。
// 影子的独立调度配置(priority/并发/分组/status 管理员可单独调)亦不在本备份范围,属已知局限
// (外审第6轮裁决:保持排除 + 前端警告,而非升级格式做完整往返)。
type DataAccount struct {
	Name               string             `json:"name" authz:"base"`
	Notes              *string            `json:"notes,omitempty" authz:"base"`
	Platform           string             `json:"platform" authz:"base"`
	Type               string             `json:"type" authz:"base"`
	Credentials        map[string]any     `json:"credentials" authz:"accounts.authorize"`
	Extra              map[string]any     `json:"extra,omitempty" authz:"account_create_extra"`
	ProxyKey           *string            `json:"proxy_key,omitempty" authz:"base"`
	Concurrency        int                `json:"concurrency" authz:"base"`
	Priority           int                `json:"priority" authz:"base"`
	RateMultiplier     *float64           `json:"rate_multiplier,omitempty" authz:"billing.rates.update"`
	ExpiresAt          *int64             `json:"expires_at,omitempty" authz:"base"`
	AutoPauseOnExpired *bool              `json:"auto_pause_on_expired,omitempty" authz:"base"`
	BPSCredentials     map[string]any     `json:"bps_credentials,omitempty" authz:"super_admin,accounts.authorize"`
	Status             string             `json:"status,omitempty" authz:"super_admin"`
	Schedulable        *bool              `json:"schedulable,omitempty" authz:"super_admin"`
	LoadFactor         *int               `json:"load_factor,omitempty" authz:"base"`
	Groups             []DataAccountGroup `json:"groups,omitempty" authz:"super_admin"`
	BPS403TargetGroup  *DataAccountGroup  `json:"bps_403_target_group,omitempty" authz:"super_admin"`
}

// Group IDs are installation-local. Match an existing destination group by
// platform and exact name; never silently create a different billing policy.
type DataAccountGroup struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Priority int    `json:"priority"`
}

type DataImportRequest struct {
	Data                 DataPayload `json:"data" authz:"object"`
	SkipDefaultGroupBind *bool       `json:"skip_default_group_bind" authz:"base"`
}

type DataImportResult struct {
	ProxyCreated   int               `json:"proxy_created"`
	ProxyReused    int               `json:"proxy_reused"`
	ProxyFailed    int               `json:"proxy_failed"`
	AccountCreated int               `json:"account_created"`
	AccountFailed  int               `json:"account_failed"`
	Errors         []DataImportError `json:"errors,omitempty"`
}

type DataImportError struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	ProxyKey string `json:"proxy_key,omitempty"`
	Message  string `json:"message"`
}

func buildProxyKey(protocol, host string, port int, username, password string) string {
	return service.NativeDataProxyKey(protocol, host, port, username, password)
}

func (h *AccountHandler) ExportData(c *gin.Context) {
	ctx := c.Request.Context()
	if err := authz.Revalidate(ctx); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	selectedIDs, err := parseAccountIDs(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	accounts, err := h.resolveExportAccounts(ctx, selectedIDs, c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 排除 spark 影子账号:影子不持凭据,通用凭据型导出无法表达父子链接、导入侧又强制 credentials
	// 非空——若混入会产出无法还原的坏备份(导入即失败)。影子的独立调度配置(priority/并发/分组/
	// status,管理员可单独调)随之不进备份,还原后需在重建的影子上重新调优;前端按 skipped_shadows
	// 提示用户(外审第5轮发现、第6轮裁决:保持排除 + 警告,不做完整往返)。
	skippedShadows := 0
	exportable := make([]service.Account, 0, len(accounts))
	for i := range accounts {
		// Supplier exports require the scoped, step-up protected transaction,
		// which locks and rechecks archive before reading any secrets.
		if accounts[i].VendorID != nil || accounts[i].IsVendorArchived() {
			response.ErrorFrom(c, authz.ErrPermissionDenied)
			return
		}
		if accounts[i].IsCredentialShadow() {
			skippedShadows++
			continue
		}
		exportable = append(exportable, accounts[i])
	}
	accounts = exportable
	if skippedShadows > 0 {
		slog.Info("export_skipped_spark_shadows", "count", skippedShadows)
	}

	includeProxies, err := parseIncludeProxies(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	var proxies []service.Proxy
	if includeProxies {
		proxies, err = h.resolveExportProxies(ctx, accounts)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	} else {
		proxies = []service.Proxy{}
	}

	// 构建 id→name 映射，用于导出备用代理 name
	proxyNameByID := make(map[int64]string, len(proxies))
	for i := range proxies {
		proxyNameByID[proxies[i].ID] = proxies[i].Name
	}

	proxyKeyByID := make(map[int64]string, len(proxies))
	for _, p := range proxies {
		proxyKeyByID[p.ID] = buildProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password)
	}
	dataProxies := make([]DataProxy, 0, len(proxies))
	for i := range proxies {
		p := proxies[i]
		key := buildProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password)
		proxyKeyByID[p.ID] = key

		var expiresAt *int64
		if p.ExpiresAt != nil {
			v := p.ExpiresAt.Unix()
			expiresAt = &v
		}
		var backupProxyName string
		var backupProxyKey string
		if p.BackupProxyID != nil {
			backupProxyName = proxyNameByID[*p.BackupProxyID]
			backupProxyKey = proxyKeyByID[*p.BackupProxyID]
		}
		dataProxies = append(dataProxies, DataProxy{
			ProxyKey:        key,
			Name:            p.Name,
			Protocol:        p.Protocol,
			Host:            p.Host,
			Port:            p.Port,
			Username:        p.Username,
			Password:        p.Password,
			Status:          p.Status,
			ExpiresAt:       expiresAt,
			FallbackMode:    p.FallbackMode,
			BackupProxyName: backupProxyName,
			BackupProxyKey:  backupProxyKey,
			ExpiryWarnDays:  p.ExpiryWarnDays,
		})
	}

	dataAccounts := make([]DataAccount, 0, len(accounts))
	groups, err := h.adminService.GetAllGroupsIncludingInactive(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	groupByID := make(map[int64]service.Group, len(groups))
	for _, group := range groups {
		groupByID[group.ID] = group
	}
	for i := range accounts {
		acc := accounts[i]
		var authorization *service.AccountAuthorizationData
		if acc.IsOpenAIOAuth() {
			if h.accountDataAuthorizations == nil {
				response.InternalError(c, "Account authorization backup is unavailable")
				return
			}
			authorization, err = h.accountDataAuthorizations.ExportAccountAuthorizations(ctx, &acc)
			if err != nil {
				response.ErrorFrom(c, err)
				return
			}
		}
		var proxyKey *string
		if acc.ProxyID != nil {
			if key, ok := proxyKeyByID[*acc.ProxyID]; ok {
				proxyKey = &key
			}
		}
		var expiresAt *int64
		if acc.ExpiresAt != nil {
			v := acc.ExpiresAt.Unix()
			expiresAt = &v
		}
		extra := make(map[string]any, len(acc.Extra))
		for key, value := range acc.Extra {
			extra[key] = value
		}
		row := DataAccount{
			Name:               acc.Name,
			Notes:              acc.Notes,
			Platform:           acc.Platform,
			Type:               acc.Type,
			Credentials:        acc.Credentials,
			Extra:              extra,
			ProxyKey:           proxyKey,
			Concurrency:        acc.Concurrency,
			Priority:           acc.Priority,
			RateMultiplier:     acc.RateMultiplier,
			ExpiresAt:          expiresAt,
			AutoPauseOnExpired: &acc.AutoPauseOnExpired,
			Status:             acc.Status, Schedulable: &acc.Schedulable, LoadFactor: acc.LoadFactor,
		}
		if authorization != nil {
			row.BPSCredentials = authorization.BPSCredentials
		}
		for _, id := range acc.GroupIDs {
			group, ok := groupByID[id]
			if !ok {
				response.BadRequest(c, "An account group is unavailable for backup")
				return
			}
			priority := acc.Priority
			for _, binding := range acc.AccountGroups {
				if binding.GroupID == id {
					priority = binding.Priority
					break
				}
			}
			row.Groups = append(row.Groups, DataAccountGroup{Name: group.Name, Platform: group.Platform, Priority: priority})
		}
		if target, ok := acc.Extra[service.ExcelBPS403TargetGroupIDKey]; ok && fmt.Sprint(target) != "0" && target != nil {
			id, parseErr := strconv.ParseInt(fmt.Sprint(target), 10, 64)
			group, found := groupByID[id]
			if parseErr != nil || !found {
				response.BadRequest(c, "BPS destination group is unavailable for backup")
				return
			}
			row.BPS403TargetGroup = &DataAccountGroup{Name: group.Name, Platform: group.Platform}
			delete(row.Extra, service.ExcelBPS403TargetGroupIDKey)
		}
		dataAccounts = append(dataAccounts, row)
	}

	payload := DataPayload{
		Type: dataType, Version: dataVersion,
		ExportedAt:     time.Now().UTC().Format(time.RFC3339),
		Proxies:        dataProxies,
		Accounts:       dataAccounts,
		SkippedShadows: skippedShadows,
	}

	if err := authz.Revalidate(ctx); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, payload)
}

func (h *AccountHandler) ImportData(c *gin.Context) {
	var req DataImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	if err := validateDataHeader(req.Data); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	executeAdminIdempotentJSON(c, "admin.accounts.import_data", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		return h.importData(ctx, req)
	})
}

func (h *AccountHandler) importData(ctx context.Context, req DataImportRequest) (DataImportResult, error) {
	skipDefaultGroupBind := true
	if req.SkipDefaultGroupBind != nil {
		skipDefaultGroupBind = *req.SkipDefaultGroupBind
	}

	dataPayload := req.Data
	result := DataImportResult{}
	groups, err := h.adminService.GetAllGroupsIncludingInactive(ctx)
	if err != nil {
		return result, err
	}

	proxyKeyToID, err := h.importDataProxies(ctx, dataPayload.Proxies, &result)
	if err != nil {
		return result, err
	}

	// 收集需要异步设置隐私的 Antigravity OAuth 账号
	var privacyAccounts []*service.Account

	for i := range dataPayload.Accounts {
		item := dataPayload.Accounts[i]
		if err := validateDataAccount(item); err != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:    "account",
				Name:    item.Name,
				Message: err.Error(),
			})
			continue
		}

		var proxyID *int64
		if item.ProxyKey != nil && *item.ProxyKey != "" {
			if id, ok := proxyKeyToID[*item.ProxyKey]; ok {
				proxyID = &id
			} else {
				result.AccountFailed++
				result.Errors = append(result.Errors, DataImportError{
					Kind:     "account",
					Name:     item.Name,
					ProxyKey: *item.ProxyKey,
					Message:  "proxy_key not found",
				})
				continue
			}
		}

		enrichCredentialsFromIDToken(&item)
		groupIDs, bindings, err := resolveDataAccountGroups(&item, groups)
		if err != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, DataImportError{Kind: "account", Name: item.Name, Message: err.Error()})
			continue
		}
		var encrypted *service.AccountAuthorizationCiphertexts
		if item.BPSCredentials != nil {
			if h.accountDataAuthorizations == nil {
				return result, errors.New("Account authorization import is unavailable")
			}
			account := &service.Account{Platform: item.Platform, Type: item.Type, Credentials: item.Credentials, Extra: item.Extra}
			encrypted, err = h.accountDataAuthorizations.PrepareAccountAuthorizations(account, &service.AccountAuthorizationData{BPSCredentials: item.BPSCredentials})
			if err != nil {
				result.AccountFailed++
				result.Errors = append(result.Errors, DataImportError{Kind: "account", Name: item.Name, Message: err.Error()})
				continue
			}
		}

		accountInput := &service.CreateAccountInput{
			Name:                 item.Name,
			Notes:                item.Notes,
			Platform:             item.Platform,
			Type:                 item.Type,
			Credentials:          item.Credentials,
			Extra:                item.Extra,
			ProxyID:              proxyID,
			Concurrency:          item.Concurrency,
			Priority:             item.Priority,
			RateMultiplier:       item.RateMultiplier,
			GroupIDs:             groupIDs,
			LoadFactor:           item.LoadFactor,
			ImportOptions:        &service.AccountImportOptions{Status: item.Status, Schedulable: item.Schedulable, Groups: bindings, Authorizations: encrypted},
			ExpiresAt:            item.ExpiresAt,
			AutoPauseOnExpired:   item.AutoPauseOnExpired,
			SkipDefaultGroupBind: skipDefaultGroupBind,
		}

		created, err := h.adminService.CreateAccount(ctx, accountInput)
		if err != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:    "account",
				Name:    item.Name,
				Message: err.Error(),
			})
			continue
		}
		// 收集 Antigravity OAuth 账号，稍后异步设置隐私
		if created.Platform == service.PlatformAntigravity && created.Type == service.AccountTypeOAuth {
			privacyAccounts = append(privacyAccounts, created)
		}
		h.scheduleGrokImportProbe(created)
		result.AccountCreated++
	}

	// 异步设置 Antigravity 隐私，避免大量导入时阻塞请求
	if len(privacyAccounts) > 0 {
		adminSvc := h.adminService
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("import_antigravity_privacy_panic", "recover", r)
				}
			}()
			bgCtx := context.Background()
			for _, acc := range privacyAccounts {
				adminSvc.ForceAntigravityPrivacy(bgCtx, acc)
			}
			slog.Info("import_antigravity_privacy_done", "count", len(privacyAccounts))
		}()
	}

	return result, nil
}

func (h *AccountHandler) listAllProxies(ctx context.Context) ([]service.Proxy, error) {
	page := 1
	pageSize := dataPageCap
	var out []service.Proxy
	for {
		items, total, err := h.adminService.ListProxies(ctx, page, pageSize, "", "", "", "created_at", "desc")
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(out) >= int(total) || len(items) == 0 {
			break
		}
		page++
	}
	return out, nil
}

func (h *AccountHandler) listAccountsFiltered(ctx context.Context, platform, accountType, status, search string, groupID int64, privacyMode, sortBy, sortOrder string) ([]service.Account, error) {
	page := 1
	pageSize := dataPageCap
	var out []service.Account
	for {
		items, total, err := h.adminService.ListAccounts(ctx, page, pageSize, platform, accountType, status, search, groupID, privacyMode, sortBy, sortOrder)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(out) >= int(total) || len(items) == 0 {
			break
		}
		page++
	}
	return out, nil
}

func (h *AccountHandler) resolveExportAccounts(ctx context.Context, ids []int64, c *gin.Context) ([]service.Account, error) {
	if len(ids) > 0 {
		accounts, err := h.adminService.GetAccountsByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		out := make([]service.Account, 0, len(accounts))
		for _, acc := range accounts {
			if acc == nil {
				continue
			}
			out = append(out, *acc)
		}
		return out, nil
	}

	platform := c.Query("platform")
	accountType := c.Query("type")
	status := c.Query("status")
	privacyMode := strings.TrimSpace(c.Query("privacy_mode"))
	search := strings.TrimSpace(c.Query("search"))
	sortBy := c.DefaultQuery("sort_by", "name")
	sortOrder := c.DefaultQuery("sort_order", "asc")
	if len(search) > 100 {
		search = search[:100]
	}

	groupID := int64(0)
	if groupIDStr := c.Query("group"); groupIDStr != "" {
		if groupIDStr == accountListGroupUngroupedQueryValue {
			groupID = service.AccountListGroupUngrouped
		} else {
			parsedGroupID, parseErr := strconv.ParseInt(groupIDStr, 10, 64)
			if parseErr != nil || parsedGroupID <= 0 {
				return nil, infraerrors.BadRequest("INVALID_GROUP_FILTER", "invalid group filter")
			}
			groupID = parsedGroupID
		}
	}

	return h.listAccountsFiltered(ctx, platform, accountType, status, search, groupID, privacyMode, sortBy, sortOrder)
}

func (h *AccountHandler) resolveExportProxies(ctx context.Context, accounts []service.Account) ([]service.Proxy, error) {
	if len(accounts) == 0 {
		return []service.Proxy{}, nil
	}

	seen := make(map[int64]struct{})
	ids := make([]int64, 0)
	for i := range accounts {
		if accounts[i].ProxyID == nil {
			continue
		}
		id := *accounts[i].ProxyID
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []service.Proxy{}, nil
	}

	// A portable account backup must include its fallback chain as well as
	// the selected accounts' primary proxies. Otherwise backup_proxy_name
	// points outside the file and cannot be safely restored by a supplier.
	proxies := make([]service.Proxy, 0, len(ids))
	for len(ids) > 0 {
		batch, err := h.adminService.GetProxiesByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		if len(batch) != len(ids) {
			return nil, service.ErrProxyNotFound
		}
		ids = nil
		for _, proxy := range batch {
			if proxy.VendorID != nil {
				return nil, authz.ErrPermissionDenied
			}
			proxies = append(proxies, proxy)
			if proxy.FallbackMode != service.FallbackModeProxy || proxy.BackupProxyID == nil {
				continue
			}
			if _, ok := seen[*proxy.BackupProxyID]; !ok {
				seen[*proxy.BackupProxyID] = struct{}{}
				ids = append(ids, *proxy.BackupProxyID)
			}
		}
	}
	return proxies, nil
}

func parseAccountIDs(c *gin.Context) ([]int64, error) {
	values := c.QueryArray("ids")
	if len(values) == 0 {
		raw := strings.TrimSpace(c.Query("ids"))
		if raw != "" {
			values = []string{raw}
		}
	}
	if len(values) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(values))
	for _, item := range values {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil || id <= 0 {
				return nil, fmt.Errorf("invalid account id: %s", part)
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func parseIncludeProxies(c *gin.Context) (bool, error) {
	raw := strings.TrimSpace(strings.ToLower(c.Query("include_proxies")))
	if raw == "" {
		return true, nil
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return true, fmt.Errorf("invalid include_proxies value: %s", raw)
	}
}

func validateDataHeader(payload DataPayload) error {
	if err := service.ValidateNativeDataVersion(payload.Type, payload.Version); err != nil {
		return err
	}
	if payload.Proxies == nil {
		return errors.New("proxies is required")
	}
	if payload.Accounts == nil {
		return errors.New("accounts is required")
	}
	return nil
}

func validateDataProxy(item DataProxy) error {
	if strings.TrimSpace(item.Protocol) == "" {
		return errors.New("proxy protocol is required")
	}
	if strings.TrimSpace(item.Host) == "" {
		return errors.New("proxy host is required")
	}
	if item.Port <= 0 || item.Port > 65535 {
		return errors.New("proxy port is invalid")
	}
	switch item.Protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("proxy protocol is invalid: %s", item.Protocol)
	}
	if item.Status != "" {
		normalizedStatus := normalizeProxyStatus(item.Status)
		if normalizedStatus != service.StatusActive && normalizedStatus != "inactive" {
			return fmt.Errorf("proxy status is invalid: %s", item.Status)
		}
	}
	return nil
}

func validateDataAccount(item DataAccount) error {
	if strings.TrimSpace(item.Name) == "" {
		return errors.New("account name is required")
	}
	if strings.TrimSpace(item.Platform) == "" {
		return errors.New("account platform is required")
	}
	if strings.TrimSpace(item.Type) == "" {
		return errors.New("account type is required")
	}
	if len(item.Credentials) == 0 {
		return errors.New("account credentials is required")
	}
	switch item.Type {
	case service.AccountTypeOAuth, service.AccountTypeSetupToken, service.AccountTypeAPIKey, service.AccountTypeUpstream, service.AccountTypeBedrock, service.AccountTypeServiceAccount:
	default:
		return fmt.Errorf("account type is invalid: %s", item.Type)
	}
	if item.RateMultiplier != nil && *item.RateMultiplier < 0 {
		return errors.New("rate_multiplier must be >= 0")
	}
	if item.Concurrency < 0 {
		return errors.New("concurrency must be >= 0")
	}
	if item.Priority < 0 {
		return errors.New("priority must be >= 0")
	}
	if item.Status != "" && item.Status != service.StatusActive && item.Status != service.StatusDisabled && item.Status != service.StatusError {
		return errors.New("account status is invalid")
	}
	return nil
}

func resolveDataAccountGroups(item *DataAccount, groups []service.Group) ([]int64, []service.AccountGroup, error) {
	if value, exists := item.Extra[service.ExcelBPS403TargetGroupIDKey]; exists && value != nil && fmt.Sprint(value) != "0" && item.BPS403TargetGroup == nil {
		return nil, nil, errors.New("BPS destination must be mapped by group name and platform, not a source group ID")
	}
	resolve := func(ref DataAccountGroup) (int64, error) {
		var id int64
		for _, group := range groups {
			if group.Name == ref.Name && group.Platform == ref.Platform {
				if id != 0 {
					return 0, errors.New("Account backup group name is ambiguous")
				}
				id = group.ID
			}
		}
		if id == 0 {
			return 0, fmt.Errorf("Create or map destination group %q (%s) before importing", ref.Name, ref.Platform)
		}
		return id, nil
	}
	var ids []int64
	var bindings []service.AccountGroup
	seen := map[int64]bool{}
	for _, ref := range item.Groups {
		id, err := resolve(ref)
		if err != nil {
			return nil, nil, err
		}
		if seen[id] || ref.Priority < 0 {
			return nil, nil, errors.New("Invalid or duplicate account backup group")
		}
		seen[id] = true
		ids = append(ids, id)
		bindings = append(bindings, service.AccountGroup{GroupID: id, Priority: ref.Priority})
	}
	if item.BPS403TargetGroup != nil {
		id, err := resolve(*item.BPS403TargetGroup)
		if err != nil {
			return nil, nil, err
		}
		if item.Extra == nil {
			item.Extra = map[string]any{}
		}
		item.Extra[service.ExcelBPS403TargetGroupIDKey] = id
	}
	return ids, bindings, nil
}

func defaultProxyName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "imported-proxy"
	}
	return name
}

// enrichCredentialsFromIDToken performs best-effort extraction of user info fields
// (email, plan_type, chatgpt_account_id, etc.) from id_token in credentials.
// Only applies to OpenAI OAuth accounts. Skips expired token errors silently.
// Existing credential values are never overwritten — only missing fields are filled.
func enrichCredentialsFromIDToken(item *DataAccount) {
	if item.Credentials == nil {
		return
	}
	// Only enrich OpenAI OAuth accounts
	platform := strings.ToLower(strings.TrimSpace(item.Platform))
	if platform != service.PlatformOpenAI {
		return
	}
	if strings.ToLower(strings.TrimSpace(item.Type)) != service.AccountTypeOAuth {
		return
	}

	idToken, _ := item.Credentials["id_token"].(string)
	if strings.TrimSpace(idToken) == "" {
		return
	}

	// DecodeIDToken skips expiry validation — safe for imported data
	claims, err := openai.DecodeIDToken(idToken)
	if err != nil {
		slog.Debug("import_enrich_id_token_decode_failed", "account", item.Name, "error", err)
		return
	}

	userInfo := claims.GetUserInfo()
	if userInfo == nil {
		return
	}

	// Fill missing fields only (never overwrite existing values)
	setIfMissing := func(key, value string) {
		if value == "" {
			return
		}
		if existing, _ := item.Credentials[key].(string); existing == "" {
			item.Credentials[key] = value
		}
	}

	setIfMissing("email", userInfo.Email)
	setIfMissing("plan_type", userInfo.PlanType)
	setIfMissing("chatgpt_account_id", userInfo.ChatGPTAccountID)
	setIfMissing("chatgpt_user_id", userInfo.ChatGPTUserID)
	setIfMissing("organization_id", userInfo.OrganizationID)
}

func normalizeProxyStatus(status string) string {
	normalized := strings.TrimSpace(strings.ToLower(status))
	switch normalized {
	case "":
		return ""
	case service.StatusActive:
		return service.StatusActive
	case "inactive", service.StatusDisabled:
		return "inactive"
	case "expired":
		// 导入 expired 代理按 inactive 处理，避免导入即触发到期改投逻辑
		return "inactive"
	default:
		return normalized
	}
}
