//go:build unit

package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAPIKeyCreateRequest(t *testing.T) {
	zero, large, negative, nan, inf := 0.0, 1e100, -1.0, math.NaN(), math.Inf(1)
	positiveDays, zeroDays, negativeDays := 1, 0, -1
	require.NoError(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{}))
	require.NoError(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{Quota: &zero, RateLimit5h: &large, ExpiresInDays: &positiveDays}))

	for _, req := range []CreateAPIKeyRequest{
		{Quota: &negative},
		{Quota: &nan},
		{RateLimit5h: &inf},
		{RateLimit1d: &negative},
		{RateLimit7d: &negative},
		{ExpiresInDays: &zeroDays},
		{ExpiresInDays: &negativeDays},
	} {
		require.Error(t, validateAPIKeyCreateRequest(req))
	}
}

func TestValidateAPIKeyUpdateRequest(t *testing.T) {
	zero, large, negative, nan, inf := 0.0, 1e100, -1.0, math.NaN(), math.Inf(-1)
	require.NoError(t, validateAPIKeyUpdateRequest(UpdateAPIKeyRequest{Quota: &zero, RateLimit7d: &large}))

	for _, req := range []UpdateAPIKeyRequest{
		{Quota: &negative},
		{RateLimit5h: &nan},
		{RateLimit1d: &inf},
		{RateLimit7d: &negative},
	} {
		require.Error(t, validateAPIKeyUpdateRequest(req))
	}
}

type peerKeyReadRepo struct {
	listFilters *service.APIKeyListFilters
	service.APIKeyRepository
	permitted bool
	checks    int
}

func (s *peerKeyReadRepo) GetByID(context.Context, int64) (*service.APIKey, error) {
	return &service.APIKey{ID: 2, UserID: 3, Key: "peer-super-key", Status: service.StatusActive}, nil
}
func (s *peerKeyReadRepo) VerifyOwnership(_ context.Context, actor int64, ids []int64) ([]int64, error) {
	s.checks++
	if s.permitted && actor == 1 {
		return ids, nil
	}
	return nil, nil
}
func TestAPIKeyGetByIDUsesPeerManagementScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, allowed := range []bool{true, false} {
		repo := &peerKeyReadRepo{permitted: allowed}
		handler := &APIKeyHandler{apiKeyService: service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, &config.Config{})}
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
			c.Next()
		})
		router.GET("/keys/:id", handler.GetByID)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/keys/2", nil))
		if allowed {
			require.Equal(t, 200, w.Code)
		} else {
			require.Equal(t, 404, w.Code)
			require.NotContains(t, w.Body.String(), "peer-super-key")
		}
		require.Equal(t, 1, repo.checks)
	}
}

func (s *peerKeyReadRepo) ListByUserID(_ context.Context, _ int64, _ pagination.PaginationParams, filters service.APIKeyListFilters) ([]service.APIKey, *pagination.PaginationResult, error) {
	s.listFilters = &filters
	return []service.APIKey{}, &pagination.PaginationResult{}, nil
}
func TestAPIKeyListUsesSuperAdminSharingOnlyForSuperAdmins(t *testing.T) {
	for _, role := range []string{service.RoleSuperAdmin, service.RoleAdmin, service.RoleUser} {
		t.Run(role, func(t *testing.T) {
			repo := &peerKeyReadRepo{}
			handler := &APIKeyHandler{apiKeyService: service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, &config.Config{})}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
				c.Set(string(middleware.ContextKeyUserRole), role)
				c.Next()
			})
			r.GET("/keys", handler.List)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/keys", nil))
			require.Equal(t, 200, w.Code)
			require.NotNil(t, repo.listFilters)
			require.Equal(t, role == service.RoleSuperAdmin, repo.listFilters.IncludeSuperAdminKeys)
		})
	}
}
