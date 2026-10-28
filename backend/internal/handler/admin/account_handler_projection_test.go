package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type accountProjectionPresenceRepo struct {
	service.AccountRepository
	present  map[int64]bool
	err      error
	batches  [][]int64
	subjects []authz.Subject
}

var _ service.OpenAIExcelAuthorizationPresenceRepository = (*accountProjectionPresenceRepo)(nil)

func (r *accountProjectionPresenceRepo) ExcelAuthorizationPresence(ctx context.Context, ids []int64) (map[int64]bool, error) {
	r.batches = append(r.batches, append([]int64(nil), ids...))
	actor, _ := authz.FromContext(ctx)
	r.subjects = append(r.subjects, actor)
	return r.present, r.err
}

type accountProjectionAdminService struct {
	*stubAdminService
	readErr error
}

func (s *accountProjectionAdminService) ListAccounts(ctx context.Context, page, pageSize int, platform, accountType, status, search string, groupID int64, privacyMode, sortBy, sortOrder string) ([]service.Account, int64, error) {
	if s.readErr != nil {
		return nil, 0, s.readErr
	}
	return s.stubAdminService.ListAccounts(ctx, page, pageSize, platform, accountType, status, search, groupID, privacyMode, sortBy, sortOrder)
}

func (s *accountProjectionAdminService) GetAccount(ctx context.Context, id int64) (*service.Account, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	return s.stubAdminService.GetAccount(ctx, id)
}

func accountProjectionRouter(h *AccountHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/admin/accounts", h.List)
	router.GET("/api/v1/admin/accounts/:id", h.GetByID)
	return router
}

func accountProjectionRequest(router *gin.Engine, path, etag string, actor authz.Subject) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(authz.WithSubject(req.Context(), actor))
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAccountHandlerAuthorizationProjectionList(t *testing.T) {
	for _, role := range []string{authz.SuperAdmin, authz.Admin} {
		for _, lite := range []string{"0", "1"} {
			t.Run(role+"/lite="+lite, func(t *testing.T) {
				actor := authz.Subject{UserID: 42, Role: role, ManagementRequest: true, PolicyVersion: 1, Permissions: []string{"accounts.read"}}
				repo := &accountProjectionPresenceRepo{present: map[int64]bool{101: true, 102: true, 108: true, 999: true}}
				oauth := service.ProvideOpenAIOAuthService(repo, nil, nil, nil, nil, nil, nil)
				svc := newStubAdminService()
				parentID := int64(101)
				svc.accounts = []service.Account{
					{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
						Credentials: map[string]any{"access_token": "secret-native", "refresh_token": "secret-refresh"},
						Extra:       map[string]any{"openai_excel_bps": false, "vendor_native_disabled": true},
						GroupIDs:    []int64{700}, Groups: []*service.Group{{ID: 700, Name: "visible-group", Platform: service.PlatformOpenAI}},
						AccountGroups: []service.AccountGroup{{AccountID: 101, GroupID: 700}}},
					{ID: 102, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
						Credentials: map[string]any{"access_token": "secret-excel", "client_id": openai.ExcelClientID}},
					{ID: 103, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
						Credentials: map[string]any{"access_token": "secret-native-only"},
						Extra:       map[string]any{"openai_cloud": true, "openai_excel_bps": true}},
					{ID: 104, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
						Extra: map[string]any{"openai_cloud": true, "openai_excel_bps": true}},
					{ID: 105, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ParentAccountID: &parentID,
						Credentials: map[string]any{"access_token": "secret-shadow"}},
					{ID: 106, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
						Credentials: map[string]any{"access_token": "secret-anthropic"}},
					{ID: 107, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
						Credentials: map[string]any{"api_key": "secret-api-key"}},
					{ID: 108, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
						Credentials: map[string]any{"access_token": "secret-off-page"}},
				}
				h := &AccountHandler{adminService: svc, openaiOAuthService: oauth}
				router := accountProjectionRouter(h)
				path := "/api/v1/admin/accounts?page=1&page_size=7&lite=" + lite
				rec := accountProjectionRequest(router, path, "", actor)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Equal(t, [][]int64{{101, 102, 103, 104}}, repo.batches, "query only the authorized page's primary OpenAI OAuth rows in one batch")
				require.Equal(t, []authz.Subject{actor}, repo.subjects, "the presence query must retain the actor's visibility scope")
				items := gjson.GetBytes(rec.Body.Bytes(), "data.items").Array()
				require.Len(t, items, 7)
				require.JSONEq(t, `["native","bps"]`, items[0].Get("authorization_modes").Raw)
				require.JSONEq(t, `["bps"]`, items[1].Get("authorization_modes").Raw, "Excel-only primary grants must not imply native access or duplicate BPS")
				require.JSONEq(t, `["native"]`, items[2].Get("authorization_modes").Raw, "enabled Cloud/BPS flags are not grants")
				for _, item := range items[3:] {
					require.False(t, item.Get("authorization_modes").Exists(), item.Raw)
				}
				for index, item := range items {
					require.Equal(t, lite == "0" && index == 0, item.Get("groups").Exists())
					require.Equal(t, lite == "0" && index == 0, item.Get("account_groups").Exists())
					require.Equal(t, role == authz.SuperAdmin, item.Get("rate_multiplier").Exists())
				}
				require.True(t, items[0].Get("credentials_status.has_access_token").Bool())
				require.NotContains(t, rec.Body.String(), "secret-")
				require.Empty(t, svc.accounts[0].Extra["authorization_modes"], "projection must not mutate stored account state")
				require.Equal(t, false, svc.accounts[0].Extra["openai_excel_bps"])

				etag := rec.Header().Get("ETag")
				require.NotEmpty(t, etag)
				rec = accountProjectionRequest(router, path, etag, actor)
				require.Equal(t, http.StatusNotModified, rec.Code)
				repo.present[101] = false
				rec = accountProjectionRequest(router, path, etag, actor)
				require.Equal(t, http.StatusOK, rec.Code)
				require.NotEqual(t, etag, rec.Header().Get("ETag"), "grant presence changes must invalidate the list representation")
				require.JSONEq(t, `["native"]`, gjson.GetBytes(rec.Body.Bytes(), "data.items.0.authorization_modes").Raw)
				require.Len(t, repo.batches, 3, "one presence query per request, never per account")
			})
		}
	}
}

func TestAccountHandlerAuthorizationProjectionDetail(t *testing.T) {
	for _, simple := range []bool{false, true} {
		name := "standard"
		if simple {
			name = "simple"
		}
		t.Run(name, func(t *testing.T) {
			actor := authz.Subject{UserID: 42, Role: authz.Admin, ManagementRequest: true, PolicyVersion: 1, Permissions: []string{"accounts.read"}}
			repo := &accountProjectionPresenceRepo{present: map[int64]bool{101: true}}
			svc := newStubAdminService()
			svc.getAccountResult = &service.Account{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "secret-native", "refresh_token": "secret-refresh"}}
			h := &AccountHandler{adminService: svc, openaiOAuthService: service.ProvideOpenAIOAuthService(repo, nil, nil, nil, nil, nil, nil)}
			if simple {
				h.cfg = &config.Config{RunMode: config.RunModeSimple}
			}
			rec := accountProjectionRequest(accountProjectionRouter(h), "/api/v1/admin/accounts/101", "", actor)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.JSONEq(t, `["native","bps"]`, gjson.GetBytes(rec.Body.Bytes(), "data.authorization_modes").Raw)
			require.Equal(t, [][]int64{{101}}, repo.batches)
			require.Equal(t, []authz.Subject{actor}, repo.subjects)
			require.NotContains(t, rec.Body.String(), "secret-")
			require.False(t, gjson.GetBytes(rec.Body.Bytes(), "data.rate_multiplier").Exists())
		})
	}
}

func TestAccountHandlerAuthorizationProjectionUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		noOAuth  bool
		readErr  error
		grantErr error
		status   int
		queries  int
	}{
		{name: "no OAuth service", noOAuth: true, status: http.StatusOK},
		{name: "presence unavailable", grantErr: errors.New("private-grant-store-error"), status: http.StatusOK, queries: 1},
		{name: "account access denied", readErr: authz.ErrPermissionDenied, status: http.StatusForbidden},
	} {
		for _, path := range []string{"/api/v1/admin/accounts", "/api/v1/admin/accounts?lite=1", "/api/v1/admin/accounts/101"} {
			t.Run(tc.name+path, func(t *testing.T) {
				repo := &accountProjectionPresenceRepo{err: tc.grantErr}
				svc := &accountProjectionAdminService{stubAdminService: newStubAdminService(), readErr: tc.readErr}
				account := service.Account{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
					Credentials: map[string]any{"access_token": "secret-native"}, Extra: map[string]any{"openai_excel_bps": true}}
				svc.accounts = []service.Account{account}
				svc.getAccountResult = &account
				h := &AccountHandler{adminService: svc}
				if !tc.noOAuth {
					h.openaiOAuthService = service.ProvideOpenAIOAuthService(repo, nil, nil, nil, nil, nil, nil)
				}
				rec := accountProjectionRequest(accountProjectionRouter(h), path, "", authz.Subject{Role: authz.SuperAdmin, ManagementRequest: true})
				require.Equal(t, tc.status, rec.Code, rec.Body.String())
				require.Len(t, repo.batches, tc.queries)
				require.NotContains(t, rec.Body.String(), "authorization_modes")
				require.NotContains(t, rec.Body.String(), "private-grant-store-error")
				require.NotContains(t, rec.Body.String(), "secret-native")
			})
		}
	}
}

func TestAccountHandlerAuthorizationProjectionEmptyPage(t *testing.T) {
	repo := &accountProjectionPresenceRepo{}
	svc := newStubAdminService()
	svc.accounts = nil
	h := &AccountHandler{adminService: svc, openaiOAuthService: service.ProvideOpenAIOAuthService(repo, nil, nil, nil, nil, nil, nil)}
	rec := accountProjectionRequest(accountProjectionRouter(h), "/api/v1/admin/accounts?lite=1", "", authz.Subject{Role: authz.SuperAdmin, ManagementRequest: true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `[]`, gjson.GetBytes(rec.Body.Bytes(), "data.items").Raw)
	require.Empty(t, repo.batches)
}
