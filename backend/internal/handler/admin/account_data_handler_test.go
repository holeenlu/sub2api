package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stubAccountAuthorizations struct {
	data *service.AccountAuthorizationData
	err  error
}

func (s *stubAccountAuthorizations) ExportAccountAuthorizations(context.Context, *service.Account) (*service.AccountAuthorizationData, error) {
	return s.data, s.err
}
func (s *stubAccountAuthorizations) PrepareAccountAuthorizations(_ *service.Account, data *service.AccountAuthorizationData) (*service.AccountAuthorizationCiphertexts, error) {
	s.data = data
	return &service.AccountAuthorizationCiphertexts{Excel: "target-encrypted-bps"}, s.err
}

func TestAccountDataPortableAuthorizationImportAndGroupMapping(t *testing.T) {
	for _, fail := range []bool{false, true} {
		adminSvc := newStubAdminService()
		adminSvc.groups = []service.Group{{ID: 901, Name: "paid", Platform: service.PlatformOpenAI}}
		auth := &stubAccountAuthorizations{}
		if fail {
			auth.err = errors.New("authorization mismatch")
		}
		h := &AccountHandler{adminService: adminSvc, accountDataAuthorizations: auth}
		paused := false
		row := DataAccount{Name: "portable", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "native"}, BPSCredentials: map[string]any{"refresh_token": "excel"}, Status: service.StatusDisabled, Schedulable: &paused, Groups: []DataAccountGroup{{Name: "paid", Platform: service.PlatformOpenAI, Priority: 17}}}
		result, err := h.importData(t.Context(), DataImportRequest{Data: DataPayload{Type: dataType, Version: 2, Proxies: []DataProxy{}, Accounts: []DataAccount{row}}})
		require.NoError(t, err)
		if fail {
			require.Equal(t, 1, result.AccountFailed)
			require.Empty(t, adminSvc.createdAccounts)
			continue
		}
		require.Equal(t, 1, result.AccountCreated)
		require.Len(t, adminSvc.createdAccounts, 1)
		in := adminSvc.createdAccounts[0]
		require.Equal(t, []int64{901}, in.GroupIDs)
		require.Equal(t, 17, in.ImportOptions.Groups[0].Priority)
		require.Equal(t, service.StatusDisabled, in.ImportOptions.Status)
		require.False(t, *in.ImportOptions.Schedulable)
		require.Equal(t, "target-encrypted-bps", in.ImportOptions.Authorizations.Excel)
		require.Equal(t, "excel", auth.data.BPSCredentials["refresh_token"])
	}
	_, err := validateImportAuthorizationFieldsForTest(authz.Subject{Role: authz.Admin})
	require.Error(t, err)
}

func validateImportAuthorizationFieldsForTest(actor authz.Subject) (bool, error) {
	return authz.ValidateRequestFields("account.import", map[string]any{"data": map[string]any{"accounts": []any{map[string]any{"bps_credentials": map[string]any{}}}}}, actor)
}

func TestImportDataRejectsRetiredAuthorizationBeforeWrites(t *testing.T) {
	for _, field := range []string{`"siwc_authorization":{}`, `"siwc_authorization":null`, `"extra":{"openai_siwc":true}`, `"extra":{"openai_siwc":false}`} {
		t.Run(field, func(t *testing.T) {
			router, svc := setupAccountDataRouter()
			body := `{"data":{"type":"sub2api-data","version":2,"proxies":[],"accounts":[{"name":"retired","platform":"openai","type":"oauth","credentials":{"access_token":"native"},` + field + `}]}}`
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Empty(t, svc.createdAccounts)
			require.Empty(t, svc.createdProxies)
		})
	}
}

func TestAccountDataRejectsUnresolvedGroupReferences(t *testing.T) {
	_, _, err := resolveDataAccountGroups(&DataAccount{Extra: map[string]any{service.ExcelBPS403TargetGroupIDKey: 12}}, nil)
	require.Error(t, err)
	ref := DataAccountGroup{Name: "paid", Platform: service.PlatformOpenAI}
	row := DataAccount{Groups: []DataAccountGroup{ref}, BPS403TargetGroup: &ref, Extra: map[string]any{service.ExcelBPS403TargetGroupIDKey: 12}}
	_, _, err = resolveDataAccountGroups(&row, nil)
	require.Error(t, err)
	group := service.Group{ID: 902, Name: "paid", Platform: service.PlatformOpenAI}
	_, _, err = resolveDataAccountGroups(&row, []service.Group{group, group})
	require.Error(t, err)
	ids, _, err := resolveDataAccountGroups(&row, []service.Group{group})
	require.NoError(t, err)
	require.Equal(t, []int64{902}, ids)
	require.Equal(t, int64(902), row.Extra[service.ExcelBPS403TargetGroupIDKey])
}

func TestAccountDataProxyBackupOrderAndFailure(t *testing.T) {
	for _, broken := range []bool{false, true} {
		svc := newStubAdminService()
		h := &AccountHandler{adminService: svc}
		primary := DataProxy{ProxyKey: "primary", Name: "primary", Protocol: "http", Host: "primary.invalid", Port: 8080, FallbackMode: service.FallbackModeProxy, BackupProxyKey: "backup"}
		backup := DataProxy{ProxyKey: "backup", Name: "backup", Protocol: "socks5h", Host: "backup.invalid", Port: 1080}
		if broken {
			backup.FallbackMode = service.FallbackModeProxy
			backup.BackupProxyKey = "primary"
		}
		result := DataImportResult{}
		keys, err := h.importDataProxies(t.Context(), []DataProxy{primary, backup}, &result)
		require.NoError(t, err)
		if broken {
			require.Equal(t, 2, result.ProxyFailed)
			require.Empty(t, svc.createdProxies)
			require.NotContains(t, keys, "primary")
			require.NotContains(t, keys, "backup")
			continue
		}
		require.Zero(t, result.ProxyFailed)
		require.Equal(t, 2, result.ProxyCreated)
		require.Equal(t, "backup", svc.createdProxies[0].Name)
		require.Equal(t, "primary", svc.createdProxies[1].Name)
		require.NotNil(t, svc.createdProxies[1].BackupProxyID)
		require.NotZero(t, keys["primary"])
	}
}

type dataResponse struct {
	Code int         `json:"code"`
	Data dataPayload `json:"data"`
}

type dataPayload struct {
	Type           string        `json:"type"`
	Version        int           `json:"version"`
	Proxies        []dataProxy   `json:"proxies"`
	Accounts       []dataAccount `json:"accounts"`
	SkippedShadows int           `json:"skipped_shadows"`
}

type dataProxy struct {
	ProxyKey string `json:"proxy_key"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Status   string `json:"status"`
}

type dataAccount struct {
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Credentials map[string]any `json:"credentials"`
	Extra       map[string]any `json:"extra"`
	ProxyKey    *string        `json:"proxy_key"`
	Concurrency int            `json:"concurrency"`
	Priority    int            `json:"priority"`
}

func setupAccountDataRouter() (*gin.Engine, *stubAdminService) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()

	h := NewAccountHandler(
		adminSvc,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	h.accountDataAuthorizations = &stubAccountAuthorizations{}

	router.GET("/api/v1/admin/accounts/data", h.ExportData)
	router.POST("/api/v1/admin/accounts/data", h.ImportData)
	return router, adminSvc
}

func TestExportDataIncludesSecrets(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	proxyID := int64(11)
	adminSvc.proxies = []service.Proxy{
		{
			ID:       proxyID,
			Name:     "proxy",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
		{
			ID:       12,
			Name:     "orphan",
			Protocol: "https",
			Host:     "10.0.0.1",
			Port:     443,
			Username: "o",
			Password: "p",
			Status:   service.StatusActive,
		},
	}
	adminSvc.accounts = []service.Account{
		{
			ID:          21,
			Name:        "account",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Credentials: map[string]any{"token": "secret"},
			Extra:       map[string]any{"note": "x", "openai_siwc": true},
			ProxyID:     &proxyID,
			Concurrency: 3,
			Priority:    50,
			Status:      service.StatusDisabled,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "sub2api-data", resp.Data.Type)
	require.Equal(t, 2, resp.Data.Version)
	require.Len(t, resp.Data.Proxies, 1)
	require.Equal(t, "pass", resp.Data.Proxies[0].Password)
	require.Len(t, resp.Data.Accounts, 1)
	require.Equal(t, "secret", resp.Data.Accounts[0].Credentials["token"])
	require.NotContains(t, resp.Data.Accounts[0].Extra, "openai_siwc")
	require.Equal(t, true, adminSvc.accounts[0].Extra["openai_siwc"], "export must not mutate stored account metadata")
}

func TestExportDataCannotBypassSupplierArchiveGuard(t *testing.T) {
	for _, archived := range []bool{false, true} {
		router, adminSvc := setupAccountDataRouter()
		owner := int64(7)
		a := service.Account{ID: 21, VendorID: &owner, Credentials: map[string]any{"access_token": "retained-secret"}}
		if archived {
			a.Extra = map[string]any{service.VendorArchivedAtKey: "2026-10-17T00:00:00Z"}
		}
		adminSvc.accounts = []service.Account{a}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data", nil))
		require.Equal(t, http.StatusForbidden, rec.Code)
		require.NotContains(t, rec.Body.String(), "retained-secret")
	}
}

func TestExportDataIncludesPortableProxyBackupChain(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	primaryID, backupID := int64(11), int64(12)
	adminSvc.proxies = []service.Proxy{
		{ID: primaryID, Name: "primary", Protocol: "http", Host: "primary.invalid", Port: 8080, Status: service.StatusActive, FallbackMode: service.FallbackModeProxy, BackupProxyID: &backupID},
		{ID: backupID, Name: "backup", Protocol: "socks5h", Host: "backup.invalid", Port: 1080, Username: "backup-user", Password: "backup-secret", Status: service.StatusActive},
	}
	adminSvc.accounts = []service.Account{{ID: 21, Name: "account", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "secret"}, ProxyID: &primaryID}}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var response struct {
		Data DataPayload `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Data.Proxies, 2)
	require.Equal(t, "backup", response.Data.Proxies[0].BackupProxyName)
	require.Equal(t, "backup-secret", response.Data.Proxies[1].Password)
	otherOwner := int64(8)
	adminSvc.proxies[1].VendorID = &otherOwner
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data", nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "backup-secret")
}

func TestExportDataWithoutProxies(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	proxyID := int64(11)
	adminSvc.proxies = []service.Proxy{
		{
			ID:       proxyID,
			Name:     "proxy",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
	}
	adminSvc.accounts = []service.Account{
		{
			ID:          21,
			Name:        "account",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Credentials: map[string]any{"token": "secret"},
			ProxyID:     &proxyID,
			Concurrency: 3,
			Priority:    50,
			Status:      service.StatusDisabled,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data?include_proxies=false", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Proxies, 0)
	require.Len(t, resp.Data.Accounts, 1)
	require.Nil(t, resp.Data.Accounts[0].ProxyKey)
}

// TestExportDataExcludesSparkShadow 验证外审第5轮 P1/P2:导出时排除 spark 影子账号
// (影子无凭据、导入侧强制 credentials 非空,混入会产出无法还原的坏备份),并透出跳过计数。
func TestExportDataExcludesSparkShadow(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	parentID := int64(21)
	adminSvc.accounts = []service.Account{
		{
			ID:          parentID,
			Name:        "mother",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Credentials: map[string]any{"token": "secret"},
			Status:      service.StatusActive,
		},
		{
			ID:              22,
			Name:            "mother (Spark)",
			Platform:        service.PlatformOpenAI,
			Type:            service.AccountTypeOAuth,
			Credentials:     map[string]any{}, // 影子恒空凭据
			ParentAccountID: &parentID,        // 影子标记
			QuotaDimension:  service.QuotaDimensionSpark,
			Status:          service.StatusActive,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data?include_proxies=false", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Accounts, 1, "影子应被排除,仅导出母账号")
	require.Equal(t, "mother", resp.Data.Accounts[0].Name)
	require.Equal(t, 1, resp.Data.SkippedShadows, "跳过的影子数量应透出")
}

func TestExportDataPassesAccountFiltersAndSort(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.accounts = []service.Account{
		{ID: 1, Name: "acc-1", Status: service.StatusActive},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/accounts/data?platform=openai&type=oauth&status=active&group=12&privacy_mode=blocked&search=keyword&sort_by=priority&sort_order=desc",
		nil,
	)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, 1, adminSvc.lastListAccounts.calls)
	require.Equal(t, "openai", adminSvc.lastListAccounts.platform)
	require.Equal(t, "oauth", adminSvc.lastListAccounts.accountType)
	require.Equal(t, "active", adminSvc.lastListAccounts.status)
	require.Equal(t, int64(12), adminSvc.lastListAccounts.groupID)
	require.Equal(t, "blocked", adminSvc.lastListAccounts.privacyMode)
	require.Equal(t, "keyword", adminSvc.lastListAccounts.search)
	require.Equal(t, "priority", adminSvc.lastListAccounts.sortBy)
	require.Equal(t, "desc", adminSvc.lastListAccounts.sortOrder)
}

func TestExportDataSelectedIDsOverrideFilters(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/accounts/data?ids=1,2&platform=openai&search=keyword&sort_by=priority&sort_order=desc",
		nil,
	)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Accounts, 2)
	require.Equal(t, 0, adminSvc.lastListAccounts.calls)
}

func TestImportDataReusesProxyAndSkipsDefaultGroup(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	adminSvc.proxies = []service.Proxy{
		{
			ID:       1,
			Name:     "proxy",
			Protocol: "socks5",
			Host:     "1.2.3.4",
			Port:     1080,
			Username: "u",
			Password: "p",
			Status:   service.StatusActive,
		},
	}

	dataPayload := map[string]any{
		"data": map[string]any{
			"type":    dataType,
			"version": dataVersion,
			"proxies": []map[string]any{
				{
					"proxy_key": "socks5|1.2.3.4|1080|u|p",
					"name":      "proxy",
					"protocol":  "socks5",
					"host":      "1.2.3.4",
					"port":      1080,
					"username":  "u",
					"password":  "p",
					"status":    "active",
				},
			},
			"accounts": []map[string]any{
				{
					"name":        "acc",
					"platform":    service.PlatformOpenAI,
					"type":        service.AccountTypeOAuth,
					"credentials": map[string]any{"token": "x"},
					"proxy_key":   "socks5|1.2.3.4|1080|u|p",
					"concurrency": 3,
					"priority":    50,
				},
			},
		},
		"skip_default_group_bind": true,
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, adminSvc.createdProxies, 0)
	require.Len(t, adminSvc.createdAccounts, 1)
	require.True(t, adminSvc.createdAccounts[0].SkipDefaultGroupBind)
}
