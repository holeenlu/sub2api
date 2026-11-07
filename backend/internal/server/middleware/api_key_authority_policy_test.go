//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// HTTP and Google consume the same access policy while retaining their own
// protocol errors and billing-stage ordering. Upgraded sockets return errors
// without writing a second HTTP response.
func TestKeyAuthorityPolicyHTTPGoogleAndContinuation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, status                          string
		expired, quota, simple, groupDisabled bool
		wantStatus                            int
		wantText                              string
	}{
		{name: "stored quota before runtime expiry", status: service.StatusAPIKeyQuotaExhausted, expired: true, wantStatus: 429, wantText: "API key 额度已用完"},
		{name: "stored expiry before runtime quota", status: service.StatusAPIKeyExpired, quota: true, wantStatus: 403, wantText: "API key 已过期"},
		{name: "runtime expiry before quota", status: service.StatusActive, expired: true, quota: true, wantStatus: 403, wantText: "API key 已过期"},
		{name: "group before quota", status: service.StatusAPIKeyQuotaExhausted, groupDisabled: true, wantStatus: 403, wantText: "API Key 所属分组已停用"},
		{name: "simple keeps expired key", status: service.StatusAPIKeyExpired, expired: true, quota: true, simple: true, wantStatus: 200},
		{name: "simple does not bypass group", status: service.StatusAPIKeyExpired, simple: true, groupDisabled: true, wantStatus: 403, wantText: "API Key 所属分组已停用"},
		{name: "simple does not bypass disabled key", status: service.StatusDisabled, simple: true, wantStatus: 401, wantText: "API key is disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, google := range []bool{false, true} {
				name := "http"
				if google {
					name = "google"
				}
				t.Run(name, func(t *testing.T) {
					group := &service.Group{ID: 9, Status: service.StatusActive, Hydrated: true}
					if tc.groupDisabled {
						group.Status = service.StatusDisabled
					}
					key := &service.APIKey{ID: 2, UserID: 3, Key: "synthetic-key", Status: tc.status, GroupID: &group.ID, Group: group, User: &service.User{ID: 3, Role: service.RoleUser, Status: service.StatusActive, Balance: 10}}
					if tc.expired {
						at := time.Now().Add(-time.Hour)
						key.ExpiresAt = &at
					}
					if tc.quota {
						key.Quota = 1
						key.QuotaUsed = 1
					}
					cfg := &config.Config{RunMode: config.RunModeStandard}
					if tc.simple {
						cfg.RunMode = config.RunModeSimple
					}
					repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
					svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
					router := gin.New()
					if google {
						router.Use(APIKeyAuthGoogle(svc, cfg))
					} else {
						router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
					}
					router.GET("/v1beta/models", func(c *gin.Context) { c.Status(200) })
					request := httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
					request.Header.Set("Authorization", "Bearer "+key.Key)
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, request)
					require.Equal(t, tc.wantStatus, recorder.Code)
					if tc.wantText != "" {
						require.Contains(t, recorder.Body.String(), tc.wantText)
					}
					wsWriter := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(wsWriter)
					c.Request = request
					c.Status(http.StatusSwitchingProtocols)
					c.Writer.WriteHeaderNow()
					err := ValidateAPIKeyContinuation(c, key, cfg)
					if tc.wantStatus == 200 {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
					require.Equal(t, http.StatusSwitchingProtocols, wsWriter.Code)
					require.Empty(t, wsWriter.Body.String())
				})
			}
		})
	}
}
