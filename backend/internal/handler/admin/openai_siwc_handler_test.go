package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type siwcHandlerAdmin struct {
	service.AdminService
	account *service.Account
	calls   int
}

func (s *siwcHandlerAdmin) GetAccount(context.Context, int64) (*service.Account, error) {
	s.calls++
	return s.account, nil
}
func TestSIWCHandlerRejectsOtherAccountKindsAndAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, kind := range []string{"apikey", "vendor", "shadow", "excel", "missing-permission", "vendor-scope", "invalid-id"} {
		t.Run(kind, func(t *testing.T) {
			a := &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{}}
			actor := authz.Subject{UserID: 2, Role: authz.Admin, PolicyVersion: 1, Permissions: []string{"accounts.authorize"}}
			switch kind {
			case "apikey":
				a.Type = service.AccountTypeAPIKey
			case "vendor":
				id := int64(4)
				a.VendorID = &id
			case "shadow":
				id := int64(4)
				a.ParentAccountID = &id
			case "excel":
				a.Credentials["client_id"] = "app_fnr0pYvVwwFDocDumLG3H2Bp"
			case "missing-permission":
				actor.Permissions = nil
			}
			ctx := authz.WithSubject(t.Context(), actor)
			if kind == "vendor-scope" {
				ctx = authz.WithVendorScope(ctx, authz.VendorScope{VendorID: 4})
			}
			stub := &siwcHandlerAdmin{account: a}
			h := &OpenAIOAuthHandler{adminService: stub}
			router := gin.New()
			router.GET("/accounts/:id/siwc", h.GetSIWCAuthorization)
			rec := httptest.NewRecorder()
			id := "1"
			if kind == "invalid-id" {
				id = "bad"
			}
			request := httptest.NewRequest(http.MethodGet, "/accounts/"+id+"/siwc", nil).WithContext(ctx)
			router.ServeHTTP(rec, request)
			require.GreaterOrEqual(t, rec.Code, 400)
			require.NotContains(t, rec.Body.String(), "access_token")
			if kind == "missing-permission" || kind == "vendor-scope" {
				require.Zero(t, stub.calls)
			}
		})
	}
}
func TestSIWCHandlerBoundsCallbackBodyBeforeService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	a := &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth}
	h := &OpenAIOAuthHandler{adminService: &siwcHandlerAdmin{account: a}}
	router := gin.New()
	router.POST("/accounts/:id/siwc/callback", h.CompleteSIWCAuthorization)
	for _, body := range []string{"{}", strings.Repeat("x", 49<<10), "{\"session_id\":\"" + strings.Repeat("x", 257) + "\",\"callback_url\":\"http://127.0.0.1:1455/auth/callback\"}"} {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/accounts/1/siwc/callback", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, r)
		require.Equal(t, 400, rec.Code)
	}
}
