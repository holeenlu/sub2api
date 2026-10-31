package service

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNativeOperationTenantHeadersUseSameFreshCredentialOwner(t *testing.T) {
	for _, workspace := range []string{"current-workspace", ""} {
		t.Run("workspace_"+workspace, func(t *testing.T) {
			f := newNativeOperationFixture(t, true)
			f.resource.Type = AccountTypeSetupToken
			f.resource.Credentials = map[string]any{"access_token": "fixture-token", "chatgpt_account_id": workspace}
			stale := *f.resource
			stale.Credentials = map[string]any{"access_token": "fixture-token", "chatgpt_account_id": "old-workspace"}
			request := f.request(t)
			request.Header.Set("Chatgpt-Account-Id", "old-workspace")
			// Noncanonical duplicate map keys must not survive alongside the
			// authoritative header when the transport serializes this request.
			request.Header["chatgpt-account-id"] = []string{"old-lowercase"}
			request.Header["authorization"] = []string{"Bearer stale-duplicate"}
			request.Header.Set("X-Openai-Fedramp", "true")
			request.Header["x-openai-fedramp"] = []string{"true"}
			f.transport.inspect = func(wire *http.Request, _ native.TransportOptions) {
				require.Equal(t, "Bearer fixture-token", wire.Header.Get("Authorization"))
				require.NotContains(t, wire.Header, "authorization")
				require.Equal(t, workspace, wire.Header.Get("Chatgpt-Account-Id"))
				count := 0
				for name := range wire.Header {
					if strings.EqualFold(name, "chatgpt-account-id") {
						count++
					}
					require.False(t, strings.EqualFold(name, "x-openai-fedramp"))
				}
				if workspace == "" {
					require.Zero(t, count)
				} else {
					require.Equal(t, 1, count)
				}
			}
			response, err := f.service.doOpenAIUpstream(request, "", &stale, f.key.GroupID)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.EqualValues(t, 1, f.transport.sends.Load())
			require.Equal(t, "old-workspace", request.Header.Get("Chatgpt-Account-Id"), "do not mutate the caller's request headers")
		})
	}
}

func TestNativeOperationCredentialChangesBeforePrepareCannotReuseGrant(t *testing.T) {
	f := newNativeOperationFixture(t, true)
	f.resource.Type = AccountTypeSetupToken
	f.resource.Credentials = map[string]any{"access_token": "fixture-token", "chatgpt_account_id": "first-workspace"}
	request := f.request(t)
	f.resource.Credentials["chatgpt_account_id"] = "replacement-workspace"
	_, err := f.service.doOpenAIUpstream(request, "", f.resource, f.key.GroupID)
	require.ErrorIs(t, err, ErrNativeDispatchGrantUnproven)
	require.Zero(t, f.transport.sends.Load())
	require.Zero(t, f.op.budget.attemptBudget.Snapshot().Attempts)
}

func TestNativeOperationAnonymousRedirectDoesNotRestoreTenantHeaders(t *testing.T) {
	f := newNativeOperationFixture(t, true, 3)
	f.resource.Type = AccountTypeSetupToken
	f.resource.Credentials = map[string]any{"access_token": "fixture-token", "chatgpt_account_id": "current-workspace"}
	calls := 0
	f.transport.reply = func(request *http.Request, _ native.TransportOptions) (*http.Response, error) {
		calls++
		if calls == 1 {
			require.Equal(t, "current-workspace", request.Header.Get("Chatgpt-Account-Id"))
			return &http.Response{StatusCode: 307, Header: http.Header{"Location": []string{"https://redirected.invalid/continued"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
		}
		require.Empty(t, request.Header.Get("Authorization"))
		require.Empty(t, request.Header.Get("Chatgpt-Account-Id"))
		require.Empty(t, request.Header.Get("X-Openai-Fedramp"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("done"))}, nil
	}
	response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 2, calls)
}

func TestNativeOperationSendFactsSurviveErrorAndRecoveryBoundaries(t *testing.T) {
	for _, state := range []model.SendState{model.SendNotSent, model.SendAmbiguous, model.SendSent} {
		t.Run(map[model.SendState]string{model.SendNotSent: "not_sent", model.SendAmbiguous: "ambiguous", model.SendSent: "sent"}[state], func(t *testing.T) {
			f := newNativeOperationFixture(t, true)
			f.transport.reply = func(request *http.Request, _ native.TransportOptions) (*http.Response, error) {
				trace := httptrace.ContextClientTrace(request.Context())
				trace.GetConn(request.URL.Host)
				if state != model.SendNotSent {
					trace.GotConn(httptrace.GotConnInfo{})
				}
				if state == model.SendSent {
					trace.WroteHeaders()
					trace.WroteRequest(httptrace.WroteRequestInfo{})
				}
				return nil, io.ErrUnexpectedEOF
			}
			_, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			var attempt *OpenAITransportAttemptError
			require.ErrorAs(t, err, &attempt)
			require.Equal(t, state, attempt.SendState)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Set("openai_uncertain_replay_safe", false)
			routed := f.service.handleOpenAIUpstreamTransportError(f.ctx, c, f.resource, err, false)
			require.ErrorIs(t, routed, io.ErrUnexpectedEOF)
			var failover *UpstreamFailoverError
			if state == model.SendNotSent {
				require.ErrorAs(t, routed, &failover)
				require.True(t, failover.RequestNotSent)
			} else {
				require.False(t, errors.As(routed, &failover), "unsafe work must not automatically fail over")
			}
		})
	}
}

func TestNativeOperationRedirectFailureCannotEraseEarlierSend(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "second_hop_unsent", true: "redirect_policy_denied"}[denied], func(t *testing.T) {
			f := newNativeOperationFixture(t, true, 3)
			calls := 0
			f.transport.reply = func(request *http.Request, _ native.TransportOptions) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: 307, Header: http.Header{"Location": []string{"/next"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
				}
				httptrace.ContextClientTrace(request.Context()).GetConn(request.URL.Host)
				return nil, io.ErrUnexpectedEOF
			}
			if denied {
				f.transport.redirect = func(*http.Request, []*http.Request, string) error { return io.ErrUnexpectedEOF }
			}
			_, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			var attempt *OpenAITransportAttemptError
			require.ErrorAs(t, err, &attempt)
			require.Equal(t, model.SendSent, attempt.SendState)
			require.False(t, requestDefinitelyUnsent(err))
		})
	}
}
