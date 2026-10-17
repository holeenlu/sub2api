//go:build unit

package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type telemetryCaptureRepo struct {
	service.OpsRepository
	samples []*service.ClientTelemetryLog
}

func (r *telemetryCaptureRepo) CaptureClientTelemetry(_ context.Context, rows []*service.ClientTelemetryLog) error {
	r.samples = rows
	return nil
}

func TestClaudeTelemetryHandlerReturnsLocalEmptyAcknowledgement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &telemetryCaptureRepo{}
	gw := &service.GatewayService{}
	service.NewOpsService(store, nil, nil, nil, nil, nil, gw, nil, nil, nil, nil)
	raw := `{"event_type":"ClaudeCodeInternalEvent","event_data":{"unknown":9007199254740993}}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1, UserID: 11})
	c.Request = httptest.NewRequest(http.MethodPost, service.ClaudeTelemetryBatchPath, strings.NewReader(`{"events":[`+raw+`]}`))
	(&GatewayHandler{gatewayService: gw}).ClaudeTelemetry(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Len(t, store.samples, 1)
	require.Equal(t, raw, store.samples[0].PayloadJSON)
}

func TestClaudeTelemetryHandlerAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name          string
		authenticated bool
		body          string
		status        int
	}{
		{"unauthenticated", false, "{}", 401},
		{"invalid batch", true, "{}", 400},
		{"storage unavailable", true, `{"events":[{"event_type":"ClaudeCodeInternalEvent","event_data":{}}]}`, 503},
		{"oversized", true, strings.Repeat(" ", int(service.ClientTelemetryMaxBodyBytes)+1), 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.POST(service.ClaudeTelemetryBatchPath, middleware.RequestBodyLimit(service.ClientTelemetryMaxBodyBytes), func(c *gin.Context) {
				if tc.authenticated {
					c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1, UserID: 11})
				}
			}, (&GatewayHandler{}).ClaudeTelemetry)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, service.ClaudeTelemetryBatchPath, strings.NewReader(tc.body)))
			require.Equal(t, tc.status, w.Code)
		})
	}
}

func TestClaudeTelemetryHandlerBoundsDecompressionAndConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := io.Copy(writer, strings.NewReader(strings.Repeat(" ", int(service.ClientTelemetryMaxBodyBytes)+1)))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	invoke := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1})
		c.Request = httptest.NewRequest(http.MethodPost, service.ClaudeTelemetryBatchPath, bytes.NewReader(compressed.Bytes()))
		c.Request.Header.Set("Content-Encoding", "gzip")
		(&GatewayHandler{}).ClaudeTelemetry(c)
		return w
	}
	require.Equal(t, 413, invoke().Code)
	for i := 0; i < cap(clientTelemetrySlots); i++ {
		clientTelemetrySlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(clientTelemetrySlots); i++ {
			<-clientTelemetrySlots
		}
	}()
	w := invoke()
	require.Equal(t, 429, w.Code)
	require.Equal(t, "1", w.Header().Get("Retry-After"))
}
