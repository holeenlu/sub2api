package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type telemetryQueryRepo struct {
	service.OpsRepository
	filter *service.ClientTelemetryLogFilter
}

func (r *telemetryQueryRepo) ListClientTelemetry(_ context.Context, f *service.ClientTelemetryLogFilter) (*service.ClientTelemetryLogList, error) {
	r.filter = f
	return &service.ClientTelemetryLogList{Items: []*service.ClientTelemetryLog{}, Page: f.Page, PageSize: f.PageSize}, nil
}
func (r *telemetryQueryRepo) GetClientTelemetry(_ context.Context, id int64) (*service.ClientTelemetryLog, error) {
	if id != 7 {
		return nil, sql.ErrNoRows
	}
	return &service.ClientTelemetryLog{ID: 7, PayloadJSON: `{ "number":9007199254740993 }`}, nil
}
func TestClientTelemetryAdminQueriesAndAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &telemetryQueryRepo{}
	h := NewOpsHandler(service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil))
	r := gin.New()
	r.GET("/api/v1/admin/ops/client-telemetry", h.ListClientTelemetry)
	r.GET("/api/v1/admin/ops/client-telemetry/:id", h.GetClientTelemetry)
	for _, route := range r.Routes() {
		rule, ok := authz.RuleFor(route.Method, route.Path)
		require.True(t, ok)
		require.True(t, rule.Allows(authz.Subject{Role: authz.SuperAdmin}))
		require.False(t, rule.Allows(authz.Subject{Role: authz.Admin, Permissions: []string{"ops.read"}}))
		require.False(t, rule.Allows(authz.Subject{Role: authz.User}))
	}
	for _, tc := range []struct {
		query  string
		status int
	}{
		{"?user_id=11&api_key_id=2&page_size=500", 200},
		{"?user_id=-1", 400}, {"?api_key_id=bad", 400},
		{"?start_time=bad", 400}, {"/7", 200}, {"/8", 404}, {"/-1", 400},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ops/client-telemetry"+tc.query, nil))
		require.Equal(t, tc.status, w.Code, tc.query)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		if tc.query == "/7" {
			var envelope struct {
				Data service.ClientTelemetryLog `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
			require.Equal(t, `{ "number":9007199254740993 }`, envelope.Data.PayloadJSON)
		}
	}
	require.Equal(t, int64(11), *repo.filter.UserID)
	require.Equal(t, 100, repo.filter.PageSize)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ops/client-telemetry?time_range=all", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Nil(t, repo.filter.StartTime)
	require.Nil(t, repo.filter.EndTime)
}
