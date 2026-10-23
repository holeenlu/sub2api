package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type excelBPSErrorUpstream struct {
	service.HTTPUpstream
	status int
	body   string
}

func (u *excelBPSErrorUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return &http.Response{StatusCode: u.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}

func TestExcelBPSErrorDoesNotAppendFallback(t *testing.T) {
	for _, tt := range []struct {
		name, input, body string
		status            int
		stream            bool
	}{
		{"upstream_stream", `"hello"`, `{"error":{"message":"bad input"}}`, 400, true},
		{"upstream_json", `"hello"`, `{"error":{"message":"bad input"}}`, 400, false},
		{"model_denied", `"hello"`, `{"error":{"code":"basispoints_model_access_changed"}}`, 403, true},
		{"local_validation", `[{"type":"function_call_output","call_id":"missing","output":"x"}]`, "", 400, true},
		{"incomplete_stream", `"hello"`, "", 200, true},
		{"incomplete_json", `"hello"`, "", 200, false},
		{"failed_stream", `"hello"`, "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"message\":\"rejected\"}}}\n\n", 200, true},
		{"failed_json", `"hello"`, "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"message\":\"rejected\"}}}\n\n", 200, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			upstream := &excelBPSErrorUpstream{status: tt.status, body: tt.body}
			cfg := &config.Config{}
			account := &service.Account{ID: 300, Status: service.StatusActive, Schedulable: true, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Credentials: map[string]any{"client_id": openai.ExcelClientID, "access_token": "test-token", "chatgpt_account_id": "test-account"},
				Extra:       map[string]any{"openai_excel_bps": true, "openai_passthrough": true}}
			repo := excelBPSErrorAccountRepo{account: account}
			gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, service.NewSettingService(excelBPSErrorSettings{}, cfg), nil)
			t.Cleanup(gateway.StopBPS403Recovery)
			body := `{"model":"gpt-6-astra","stream":` + map[bool]string{true: "true", false: "false"}[tt.stream] + `,"input":` + tt.input + `}`
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
			before := c.Writer.Size()
			_, err := gateway.Forward(context.Background(), c, account, []byte(body))
			require.Error(t, err)
			if tt.name == "model_denied" {
				var failover *service.UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Empty(t, rec.Body.String())
				require.False(t, c.Writer.Written())
				h := &OpenAIGatewayHandler{}
				h.handleFailoverExhausted(c, failover, false)
				require.Equal(t, http.StatusForbidden, rec.Code)
				require.True(t, json.Valid(rec.Body.Bytes()))
				return
			}
			if strings.HasPrefix(tt.name, "incomplete_") {
				var recovery *service.UpstreamFailoverError
				require.ErrorAs(t, err, &recovery)
				require.False(t, c.Writer.Written(), "metadata-free failures remain recoverable at handler level")
				return
			}
			response := rec.Body.String()
			require.NotEmpty(t, response, "forward error: %v", err)
			h := &OpenAIGatewayHandler{}
			if !openAIForwardErrorAlreadyCommunicated(c, before, err) {
				require.False(t, h.ensureForwardErrorResponse(c, false))
			}
			require.Equal(t, response, rec.Body.String())
			if tt.name == "incomplete_stream" {
				require.Equal(t, 1, strings.Count(response, "event: error"))
			} else if tt.name == "failed_stream" {
				require.Contains(t, response, `"type":"error"`)
			} else {
				require.True(t, json.Valid([]byte(response)), response)
				require.NotContains(t, response, "event:")
			}
		})
	}
}

func TestExcelBPSFailoverExhaustedAfterKeepaliveRecordsFailure(t *testing.T) {
	for _, tt := range []struct {
		name   string
		reason service.GatewayFailureReason
		status int
	}{
		{"upstream_failure", service.ExcelBPSUpstreamFailureReason, http.StatusInternalServerError},
		{"model_permission", service.ExcelBPSModelPermissionReason, http.StatusForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Header("Content-Type", "text/event-stream")
			_, err := c.Writer.WriteString(": keepalive\n\n")
			require.NoError(t, err)
			c.Writer.Flush()
			h := &OpenAIGatewayHandler{}
			h.handleFailoverExhausted(c, &service.UpstreamFailoverError{
				StatusCode: tt.status, ClientStatusCode: tt.status, Reason: tt.reason,
				ClientMessage: "Excel BPS request failed",
			}, true)
			require.Equal(t, http.StatusOK, rec.Code, "keepalive already committed HTTP status")
			require.Contains(t, rec.Body.String(), "event: response.failed")
			require.Contains(t, rec.Body.String(), string(tt.reason))
			streamErr, ok := service.GetOpsStreamError(c)
			require.True(t, ok)
			require.True(t, streamErr.CountTowardsSLA, "exhausted provider failure must not be recorded as HTTP 200")
			require.Equal(t, tt.status, streamErr.IntendedStatus)
			require.Equal(t, string(tt.reason), streamErr.Code)
		})
	}
}

type excelBPSErrorAccountRepo struct {
	service.AccountRepository
	account *service.Account
}

func (r excelBPSErrorAccountRepo) GetOpenAITurnAdmission(context.Context, int64) (*service.Account, *service.Account, error) {
	return r.account, nil, nil
}

type excelBPSErrorSettings struct{ service.SettingRepository }

func (excelBPSErrorSettings) GetValue(_ context.Context, key string) (string, error) {
	if key == service.SettingKeyExcelBPSEnabled {
		return "true", nil
	}
	return "", nil
}
func (excelBPSErrorSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{service.SettingKeyExcelBPSEnabled: "true"}, nil
}

func TestExcelBPSImageAdmissionExhaustionKeepsLocalError(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusRequestEntityTooLarge} {
		for _, stream := range []bool{false, true} {
			for _, messages := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/stream=%t/messages=%t", status, stream, messages), func(t *testing.T) {
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					if messages {
						c.Request.URL.Path = "/v1/messages"
					}
					if stream {
						_, err := c.Writer.WriteString(": keepalive\n\n")
						require.NoError(t, err)
						c.Writer.Flush()
					}
					failure := &service.UpstreamFailoverError{StatusCode: status, ClientStatusCode: status, Reason: service.ExcelBPSImageRequestBusyReason, ClientMessage: "Local BPS image limit", ResponseHeaders: http.Header{"Retry-After": {"1"}}}
					if status == 413 {
						failure.Reason = service.ExcelBPSImageBodyTooLargeReason
						failure.ResponseHeaders = nil
					}
					h := &OpenAIGatewayHandler{}
					if messages {
						h.handleAnthropicFailoverExhausted(c, failure, stream)
					} else {
						h.handleFailoverExhausted(c, failure, stream)
					}
					if stream {
						require.Equal(t, 200, rec.Code)
					} else {
						require.Equal(t, status, rec.Code)
					}
					require.Contains(t, rec.Body.String(), string(failure.Reason))
					logical, ok := service.GetOpsStreamError(c)
					require.True(t, ok)
					require.Equal(t, status, logical.IntendedStatus)
					require.Equal(t, string(failure.Reason), logical.Code)
					require.True(t, logical.RequestScoped)
					require.Equal(t, !stream, logical.NonStream)
					require.Zero(t, logical.UpstreamStatus)
				})
			}
		}
	}
}

func TestExcelBPSImageAdmissionWSBodyLimitIsRequestScoped(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	service.SetOpenAIClientTransport(c, service.OpenAIClientTransportWS)
	service.SetOpsUpstreamError(c, 500, "earlier attempt", "")
	markOpenAIWSTurnFailure(c, &service.UpstreamFailoverError{
		StatusCode: http.StatusRequestEntityTooLarge, ClientStatusCode: http.StatusRequestEntityTooLarge,
		Reason: service.ExcelBPSImageBodyTooLargeReason, ClientMessage: "Request body exceeds the image relay ingress limit",
	})
	failure, ok := service.GetOpsStreamError(c)
	require.True(t, ok)
	require.Equal(t, http.StatusRequestEntityTooLarge, failure.IntendedStatus)
	require.Equal(t, "invalid_request_error", failure.ErrType)
	require.Equal(t, string(service.ExcelBPSImageBodyTooLargeReason), failure.Code)
	require.True(t, failure.RequestScoped)
	require.Zero(t, failure.UpstreamStatus, "a previous attempt must not contaminate the local rejection")
}
