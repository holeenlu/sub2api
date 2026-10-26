package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type responseEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type opsSystemLogCaptureRepo struct {
	service.OpsRepository
	listFilter    *service.OpsSystemLogFilter
	cleanupFilter *service.OpsSystemLogCleanupFilter
}

func (r *opsSystemLogCaptureRepo) ListSystemLogs(_ context.Context, filter *service.OpsSystemLogFilter) (*service.OpsSystemLogList, error) {
	r.listFilter = filter
	return &service.OpsSystemLogList{Logs: []*service.OpsSystemLog{}, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (r *opsSystemLogCaptureRepo) DeleteSystemLogs(_ context.Context, filter *service.OpsSystemLogCleanupFilter) (int64, error) {
	r.cleanupFilter = filter
	return 1, nil
}

func (r *opsSystemLogCaptureRepo) InsertSystemLogCleanupAudit(_ context.Context, _ *service.OpsSystemLogCleanupAudit) error {
	return nil
}

func newOpsSystemLogTestRouter(handler *OpsHandler, withUser bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if withUser {
		r.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99})
			c.Next()
		})
	}
	r.GET("/logs", handler.ListSystemLogs)
	r.POST("/logs/cleanup", handler.CleanupSystemLogs)
	r.GET("/logs/health", handler.GetSystemLogIngestionHealth)
	return r
}

func TestOpsSystemLogHandler_ListUnavailable(t *testing.T) {
	h := NewOpsHandler(nil)
	r := newOpsSystemLogTestRouter(h, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", w.Code)
	}
}

func TestOpsSystemLogHandler_ListInvalidUserID(t *testing.T) {
	svc := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs?user_id=abc", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

func TestOpsSystemLogHandler_ListInvalidAccountID(t *testing.T) {
	svc := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs?account_id=-1", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

func TestOpsSystemLogHandler_ListInvalidAPIKeyID(t *testing.T) {
	svc := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs?api_key_id=abc", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

func TestOpsSystemLogHandler_ListMonitoringDisabled(t *testing.T) {
	svc := service.NewOpsService(nil, nil, &config.Config{
		Ops: config.OpsConfig{Enabled: false},
	}, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

func TestOpsSystemLogHandler_ListSuccess(t *testing.T) {
	svc := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs?time_range=30m&page=1&page_size=20", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var resp responseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("unexpected response code: %+v", resp)
	}
}

func TestOpsSystemLogHandler_ListAcceptsHost(t *testing.T) {
	repo := &opsSystemLogCaptureRepo{}
	svc := service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs?host=api-node-1", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	if repo.listFilter == nil || repo.listFilter.Host != "api-node-1" {
		t.Fatalf("host filter = %+v, want api-node-1", repo.listFilter)
	}
}

func TestOpsSystemLogHandler_CleanupUnauthorized(t *testing.T) {
	svc := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logs/cleanup", bytes.NewBufferString(`{"request_id":"r1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", w.Code)
	}
}

func TestOpsSystemLogHandler_CleanupInvalidPayload(t *testing.T) {
	svc := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logs/cleanup", bytes.NewBufferString(`{bad-json`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

func TestOpsSystemLogHandler_CleanupInvalidTime(t *testing.T) {
	svc := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logs/cleanup", bytes.NewBufferString(`{"start_time":"bad","request_id":"r1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

func TestOpsSystemLogHandler_CleanupInvalidEndTime(t *testing.T) {
	svc := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logs/cleanup", bytes.NewBufferString(`{"end_time":"bad","request_id":"r1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

func TestOpsSystemLogHandler_CleanupServiceUnavailable(t *testing.T) {
	svc := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logs/cleanup", bytes.NewBufferString(`{"request_id":"r1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", w.Code)
	}
}

func TestOpsSystemLogHandler_CleanupAcceptsAPIKeyID(t *testing.T) {
	svc := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logs/cleanup", bytes.NewBufferString(`{"api_key_id":123}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", w.Code)
	}
}

func TestOpsSystemLogHandler_CleanupAcceptsHost(t *testing.T) {
	repo := &opsSystemLogCaptureRepo{}
	svc := service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logs/cleanup", bytes.NewBufferString(`{"host":"api-node-1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	if repo.cleanupFilter == nil || repo.cleanupFilter.Host != "api-node-1" {
		t.Fatalf("host filter = %+v, want api-node-1", repo.cleanupFilter)
	}
}

func TestOpsSystemLogHandler_CleanupInvalidAPIKeyID(t *testing.T) {
	svc := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logs/cleanup", bytes.NewBufferString(`{"api_key_id":0}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

func TestOpsSystemLogHandler_CleanupMonitoringDisabled(t *testing.T) {
	svc := service.NewOpsService(nil, nil, &config.Config{
		Ops: config.OpsConfig{Enabled: false},
	}, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logs/cleanup", bytes.NewBufferString(`{"request_id":"r1"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

func TestOpsSystemLogHandler_Health(t *testing.T) {
	sink := service.NewOpsSystemLogSink(nil)
	svc := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, sink)
	h := NewOpsHandler(svc)
	r := newOpsSystemLogTestRouter(h, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs/health", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

func TestOpsSystemLogHandler_HealthUnavailableAndMonitoringDisabled(t *testing.T) {
	h := NewOpsHandler(nil)
	r := newOpsSystemLogTestRouter(h, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logs/health", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", w.Code)
	}

	svc := service.NewOpsService(nil, nil, &config.Config{
		Ops: config.OpsConfig{Enabled: false},
	}, nil, nil, nil, nil, nil, nil, nil, nil)
	h = NewOpsHandler(svc)
	r = newOpsSystemLogTestRouter(h, false)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/logs/health", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// Downloads reuse the full detail endpoints and their existing permission projection.
type opsDiagnosticRepo struct {
	service.OpsRepository
}

func (r *opsDiagnosticRepo) GetErrorLogByID(_ context.Context, id int64) (*service.OpsErrorLogDetail, error) {
	accountID, upstreamStatus := int64(7), 200
	trace, _ := json.Marshal(map[string]any{"events": []any{map[string]any{"stage": "capacity", "account_id": accountID, "configured_capacity": 10}}, "summary": map[string]any{"stop_reason": "complete"}})
	events, _ := json.Marshal([]any{
		map[string]any{"kind": "bridge_error", "scope": "excel_bps", "stage": "stream", "reason": "unexpected_eof", "account_id": accountID, "upstream_status_code": upstreamStatus, "detail": "diagnostic facts", "upstream_response_body": "stored provider body", "future_field": "preserve-me"},
		map[string]any{"kind": "scheduling_trace", "detail": string(trace)},
	})
	return &service.OpsErrorLogDetail{OpsErrorLog: service.OpsErrorLog{ID: id, RequestID: "request-62068", StatusCode: 502, AccountID: &accountID, AccountName: "upstream-owner", Platform: "openai", Model: "gpt-6-astra", CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), InboundEndpoint: "/v1/responses", UpstreamEndpoint: "/basispoints/api/responses", Message: "basispoints returned a tool outside the client's catalog"}, ErrorBody: "event: response.failed\ndata: {\"response\":{\"error\":{\"code\":\"basispoints_protocol_error\",\"message\":\"invalid tool\"}}}\n\n", UpstreamStatusCode: &upstreamStatus, UpstreamErrorMessage: "invalid tool", UpstreamErrorDetail: "{\"reason\":\"unknown_tool\",\"catalog_tool_count\":5}", UpstreamErrors: string(events)}, nil
}
func TestOpsCompleteErrorDetailsKeepPayloadAndPermissions(t *testing.T) {
	for _, role := range []string{authz.Admin, authz.SuperAdmin} {
		for _, path := range []string{"/api/v1/admin/ops/errors", "/api/v1/admin/ops/request-errors", "/api/v1/admin/ops/upstream-errors"} {
			t.Run(path+"/"+role, func(t *testing.T) {
				repo := &opsDiagnosticRepo{}
				svc := service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
				svc.SetMonitoringEnabled(true)
				router := gin.New()
				actor := authz.Subject{Role: role, ManagementRequest: true, Permissions: []string{"ops.read"}}
				router.Use(func(c *gin.Context) {
					c.Request = c.Request.WithContext(authz.WithSubject(c.Request.Context(), actor))
					c.Next()
				})
				router.GET(path+"/:id", NewOpsHandler(svc).GetErrorLogByID)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path+"/62068", nil))
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				for _, fact := range []string{"basispoints_protocol_error", "response.failed", "/basispoints/api/responses", "unknown_tool", "catalog_tool_count", "bridge_error", "unexpected_eof", "configured_capacity", "stop_reason", "future_field", "preserve-me"} {
					require.Contains(t, w.Body.String(), fact)
				}
				var envelope struct{ Data map[string]json.RawMessage }
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
				var events string
				require.NoError(t, json.Unmarshal(envelope.Data["upstream_errors"], &events))
				if role == authz.SuperAdmin {
					require.Contains(t, w.Body.String(), "upstream-owner")
					require.Contains(t, events, "stored provider body")
				} else {
					require.NotContains(t, w.Body.String(), "upstream-owner")
					require.NotContains(t, events, "account_id")
					require.NotContains(t, events, "upstream_response_body")
				}
				svc.SetMonitoringEnabled(false)
				w = httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path+"/62068", nil))
				require.NotEqual(t, http.StatusOK, w.Code)
			})
		}
	}
}
