package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
)

type bpsCauseCancelBody struct{ cancel context.CancelCauseFunc }

func (b *bpsCauseCancelBody) Read([]byte) (int, error) {
	b.cancel(errOpenAIWSTurnCancelled)
	return 0, context.Canceled
}
func (b *bpsCauseCancelBody) Close() error { return nil }

// A client response.cancel on the WS bridge cancels only the forwarder context
// (with errOpenAIWSTurnCancelled as cause); the Gin request context belongs to
// the connection and stays alive. The turn must be recorded as a client
// cancellation, not as a BPS transport failure or provider error.
func TestOpenAIWSBPSBridgeTurnCancelIsClientCancellation(t *testing.T) {
	for _, stage := range []string{"headers", "stream"} {
		t.Run(stage, func(t *testing.T) {
			turnCtx, cancelTurn := context.WithCancelCause(context.Background())
			defer cancelTurn(nil)
			calls := 0
			upstream := &bpsTestUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
				calls++
				require.NoError(t, req.Body.Close())
				if stage == "headers" {
					cancelTurn(errOpenAIWSTurnCancelled)
					return nil, &url.Error{Op: "Post", URL: "https://invalid.test", Err: context.Canceled}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: &bpsCauseCancelBody{cancel: cancelTurn}}, nil
			}}
			svc := excelBPSGatewayTestService(nil)
			svc.httpUpstream = upstream
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			var events [][]byte
			_, err := svc.proxyOpenAIWSExcelBPSTurn(turnCtx, c, excelAccount(), []byte(`{"model":"gpt-6-astra","stream":true,"input":"test"}`), "gpt-6-astra", func(payload []byte) error {
				events = append(events, append([]byte(nil), payload...))
				return nil
			})
			require.ErrorIs(t, err, errOpenAIWSTurnCancelled, "the bridge reports the client's cancellation cause")
			require.NoError(t, c.Request.Context().Err(), "the connection context is untouched")
			require.Equal(t, 1, calls, "a cancelled turn is never replayed")
			marked, ok := GetOpsStreamError(c)
			require.True(t, ok, "cancellation must be classified for Ops")
			require.Equal(t, OpsClientCanceledCode, marked.Code)
			require.Equal(t, 499, marked.IntendedStatus)
			require.True(t, marked.RequestScoped)
			_, providerFailure := c.Get(OpsUpstreamErrorMessageKey)
			require.False(t, providerFailure, "a client cancel is not a provider failure")
			_, attempts := c.Get(OpsUpstreamErrorsKey)
			require.False(t, attempts, "no request_error transport event for a client cancel")
			for _, event := range events {
				require.NotEqual(t, "error", gjson.GetBytes(event, "type").String(), "no synthesized BPS error frame: %s", event)
			}
		})
	}
}

type bpsStateCapacityStore struct {
	GatewayCache
	saveErr error
}

func (s *bpsStateCapacityStore) LoadBPSState(context.Context, string, string) ([]byte, uint64, error) {
	return nil, 0, nil
}
func (s *bpsStateCapacityStore) SaveBPSState(context.Context, string, string, *uint64, []byte) (bool, error) {
	return false, s.saveErr
}

// Shared-state budget errors are a server-side limit: report them as 503 like
// an unavailable store, never as an invalid request the client should fix.
func TestExcelBPSSharedStateErrorsAreNotClientErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		saveErr  error
		wantCode string
	}{
		{"capacity", basispoints.ErrStateCapacity, "basispoints_state_capacity"},
		{"unavailable", basispoints.ErrStateUnavailable, "basispoints_state_unavailable"},
		{"wrapped capacity", fmt.Errorf("redis: %w", basispoints.ErrStateCapacity), "basispoints_state_capacity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{}
			svc := excelBPSGatewayTestService(upstream)
			svc.cfg.Gateway.ExcelBPSStateRedisEnabled = true
			svc.cache = &bpsStateCapacityStore{saveErr: tc.saveErr}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			body := []byte(`{"model":"gpt-6-astra","input":"test","prompt_cache_key":"bps-state-test","tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}}}]}`)
			_, err := svc.Forward(context.Background(), c, excelAccount(), body)
			var forwardErr *excelBPSForwardError
			require.ErrorAs(t, err, &forwardErr, "forward error: %v", err)
			require.Equal(t, http.StatusServiceUnavailable, rec.Code)
			require.Equal(t, http.StatusServiceUnavailable, forwardErr.status)
			require.False(t, forwardErr.RequestScoped(), "state failures count against the account route, not the request")
			require.Equal(t, tc.wantCode, gjson.Get(rec.Body.String(), "error.code").String())
			require.Equal(t, "server_error", gjson.Get(rec.Body.String(), "error.type").String())
			require.Empty(t, upstream.requests, "no generation is attempted without a consistent session state")
			require.False(t, errors.Is(err, context.Canceled))
		})
	}
}
