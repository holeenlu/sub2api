package service

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// All sends go through the existing operation, dispatcher and provider Open.
// Only the transport peer is a fixture; this file never supplies a send result,
// runs a replacement retry loop, or edits an execution budget.
func TestNativeOperationFollowupRedirectBudgetExhaustionCannotFailover(t *testing.T) {
	for _, tc := range []struct {
		name      string
		wantSends int
	}{
		{name: "target_before_total", wantSends: 2},
		{name: "owner_before_total", wantSends: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNativeOperationFixture(t, true, 6)
			// Zero explicitly selects the existing one-attempt target policy.
			// Merely omitting pool_mode_retry_count would use the default of three.
			f.resource.Credentials["pool_mode"] = true
			f.resource.Credentials["pool_mode_retry_count"] = 0
			calls := 0
			f.transport.reply = func(request *http.Request, _ native.TransportOptions) (*http.Response, error) {
				calls++
				location := "/same-target"
				if tc.name == "owner_before_total" {
					location = fmt.Sprintf("/distinct-target-%d", calls)
				}
				return nativeFollowupRedirect(location), nil
			}
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.Nil(t, response)
			require.ErrorIs(t, err, execution.ErrTargetBudgetExhausted)
			requireNativeFollowupSendFailure(t, err, model.SendSent)
			require.Equal(t, tc.wantSends, calls)
			require.EqualValues(t, tc.wantSends, f.transport.sends.Load())
			snapshot := f.op.budget.attemptBudget.Snapshot()
			require.Equal(t, tc.wantSends, snapshot.Attempts)
			require.Less(t, snapshot.Attempts, snapshot.Limit, "must exhaust the target/owner, not the total allowance")
			require.Equal(t, tc.name == "target_before_total", f.op.budget.attemptBudget.OwnerAvailable(recoveryOwner(f.resource)))

			unsafe := nativeFollowupErrorContext(false)
			handled := f.service.handleOpenAIUpstreamTransportError(f.ctx, unsafe, f.resource, err, false)
			require.ErrorIs(t, handled, execution.ErrTargetBudgetExhausted)
			requireNativeFollowupSendFailure(t, handled, model.SendSent)

			// Explicitly replay-safe work retains the existing recovery reason,
			// but prior redirect sends must never be reclassified as NotSent.
			safe := nativeFollowupErrorContext(true)
			recoverable := f.service.handleOpenAIUpstreamTransportError(f.ctx, safe, f.resource, err, false)
			require.ErrorIs(t, recoverable, execution.ErrTargetBudgetExhausted)
			var failure *UpstreamFailoverError
			require.ErrorAs(t, recoverable, &failure)
			require.Equal(t, GatewayFailureReason("recovery_target_exhausted"), failure.Reason)
			require.Equal(t, http.StatusServiceUnavailable, failure.StatusCode)
			require.False(t, failure.RequestNotSent)
			require.True(t, failure.ShouldRetryNextAccount())
			requireNativeFollowupSendFailure(t, err, model.SendSent)
			require.Zero(t, f.transport.legacy.Load())
		})
	}

	t.Run("not_sent_keeps_existing_recovery_reason", func(t *testing.T) {
		f := newNativeOperationFixture(t, true, 6)
		f.resource.Credentials["pool_mode"] = true
		f.resource.Credentials["pool_mode_retry_count"] = 0
		// A real Admit reserves the target allowance, but no Open occurs.
		// A subsequent exchange must retain this genuine NotSent positive case.
		prepared, target, err := f.op.prepare(f.request(t), f.resource, "", false)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, prepared.Close()) })
		permit, err := f.op.dispatcher.Admit(f.ctx, prepared.Identity(), target)
		require.NoError(t, err)
		require.NotNil(t, permit)
		require.NoError(t, prepared.Close())

		response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
		require.Nil(t, response)
		require.ErrorIs(t, err, execution.ErrTargetBudgetExhausted)
		requireNativeFollowupSendFailure(t, err, model.SendNotSent)
		handled := f.service.handleOpenAIUpstreamTransportError(f.ctx, nativeFollowupErrorContext(false), f.resource, err, false)
		require.ErrorIs(t, handled, execution.ErrTargetBudgetExhausted)
		var failure *UpstreamFailoverError
		require.ErrorAs(t, handled, &failure)
		require.Equal(t, GatewayFailureReason("recovery_target_exhausted"), failure.Reason)
		require.True(t, failure.RequestNotSent)
		require.True(t, failure.ShouldRetryNextAccount())
		require.Equal(t, 1, f.op.budget.attemptBudget.Snapshot().Attempts)
		require.Zero(t, f.transport.sends.Load())
		require.Zero(t, f.transport.legacy.Load())
	})
}

func TestNativeOperationFollowupTLSRetryAndRedirectShareExchangeState(t *testing.T) {
	for _, name := range []string{"redirect_then_tls_retry_admission_denied", "tls_retry_then_redirect_tls_failure"} {
		t.Run(name, func(t *testing.T) {
			f := newNativeOperationFixture(t, true, 6)
			proxy := &Proxy{ID: 23, Protocol: "http", Host: "127.0.0.1", Port: 18080, Status: StatusActive}
			f.resource.ProxyID, f.resource.Proxy = &proxy.ID, proxy
			f.resource.Credentials["pool_mode"] = true
			f.resource.Credentials["pool_mode_retry_count"] = 1
			paths := make([]string, 0, 3)
			f.transport.reply = func(request *http.Request, options native.TransportOptions) (*http.Response, error) {
				require.Equal(t, proxy.URL(), options.ProxyURL)
				require.Equal(t, native.ProfileOpenAI, options.Profile)
				paths = append(paths, request.URL.Path)
				call := len(paths)
				if name == "redirect_then_tls_retry_admission_denied" {
					if call == 1 {
						return nativeFollowupRedirect("/continued"), nil
					}
					require.Equal(t, 2, call, "revoked retry must fail before a third transport call")
					f.key.Status = StatusDisabled
					return nil, nativeFollowupTLSFailure(t, request)
				}
				if call == 2 {
					body, err := io.ReadAll(request.Body)
					require.NoError(t, err)
					require.Equal(t, nativeOperationFixtureBody, string(body), "the real retry must retain sealed bytes")
					return nativeFollowupRedirect("/continued"), nil
				}
				require.Contains(t, []int{1, 3}, call, "redirect must not reset the TLS retry counter")
				return nil, nativeFollowupTLSFailure(t, request)
			}
			waitBefore := f.op.budget.attemptBudget.Snapshot().RetryWait
			exchangeStarted := time.Now()
			response, err := f.service.doOpenAIUpstream(f.request(t), proxy.URL(), f.resource, f.key.GroupID)
			exchangeElapsed := time.Since(exchangeStarted)
			require.Nil(t, response)
			require.Error(t, err)
			if name == "redirect_then_tls_retry_admission_denied" {
				require.True(t, IsNativeDispatchDenied(err), "retry must reach live admission: %v", err)
				require.Equal(t, []string{"/v1/responses", "/continued"}, paths)
			} else {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
				require.Equal(t, []string{"/v1/responses", "/v1/responses", "/continued"}, paths)
			}
			requireNativeFollowupSendFailure(t, err, model.SendSent)
			handled := f.service.handleOpenAIUpstreamTransportError(f.ctx, nativeFollowupErrorContext(false), f.resource, err, false)
			requireNativeFollowupSendFailure(t, handled, model.SendSent)
			snapshot := f.op.budget.attemptBudget.Snapshot()
			require.Equal(t, len(paths), snapshot.Attempts)
			require.EqualValues(t, len(paths), f.transport.sends.Load())
			// The real wait includes the existing jitter. Prove retry count with
			// paths/attempts above, and bound the charged wait by this exchange.
			waitCharged := snapshot.RetryWait - waitBefore
			require.GreaterOrEqual(t, waitCharged, execution.PreRequestRetryDelay)
			require.LessOrEqual(t, waitCharged, exchangeElapsed, "charged retry wait must fit within the exchange lifetime")
			require.Less(t, snapshot.Attempts, snapshot.Limit)
			require.Zero(t, f.transport.legacy.Load())
		})
	}
}

func TestNativeOperationFollowupShadowParentHeadersAndAnonymousRedirect(t *testing.T) {
	f := newNativeOperationFixture(t, true, 3)
	parent := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "fresh-parent-token", "chatgpt_account_id": "fresh-parent-workspace",
			"chatgpt_account_is_fedramp": true, "expires_at": time.Now().Add(2 * time.Hour).Format(time.RFC3339Nano)}}
	f.accounts.accounts[parent.ID] = parent
	f.resource.ParentAccountID, f.resource.QuotaDimension, f.resource.Type = &parent.ID, "spark", AccountTypeOAuth
	f.resource.Credentials = map[string]any{"access_token": "stale-child-token", "chatgpt_account_id": "stale-child-workspace"}
	f.service.openAITokenProvider = NewOpenAITokenProvider(f.accounts, nil, nil)
	f.op.authority.tokens = f.service.openAITokenProvider
	request := f.request(t)
	require.Equal(t, "Bearer fresh-parent-token", request.Header.Get("Authorization"))
	request.Header["authorization"] = []string{"Bearer stale-child-token"}
	request.Header.Set("Chatgpt-Account-Id", "stale-child-workspace")
	request.Header["chatgpt-account-id"] = []string{"stale-duplicate-workspace"}
	request.Header.Set("X-Openai-Fedramp", "false")
	request.Header["x-openai-fedramp"] = []string{"false"}
	originalHeaders := request.Header.Clone()
	calls := 0
	f.transport.reply = func(wire *http.Request, options native.TransportOptions) (*http.Response, error) {
		calls++
		require.Equal(t, f.resource.ID, options.AccountID, "capacity and transport remain bound to the selected shadow")
		if calls == 1 {
			require.Equal(t, []string{"Bearer fresh-parent-token"}, nativeFollowupHeaderValues(wire.Header, "authorization"))
			require.Equal(t, []string{"fresh-parent-workspace"}, nativeFollowupHeaderValues(wire.Header, "chatgpt-account-id"))
			require.Equal(t, []string{"true"}, nativeFollowupHeaderValues(wire.Header, "x-openai-fedramp"))
			return nativeFollowupRedirect("https://anonymous-redirect.invalid/continued"), nil
		}
		require.Equal(t, 2, calls)
		require.Equal(t, "anonymous-redirect.invalid", wire.URL.Hostname())
		for _, name := range []string{"authorization", "chatgpt-account-id", "x-openai-fedramp"} {
			require.Empty(t, nativeFollowupHeaderValues(wire.Header, name), "anonymous hop retained %s", name)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("done"))}, nil
	}
	response, err := f.service.doOpenAIUpstream(request, "", f.resource, f.key.GroupID)
	require.NoError(t, err)
	require.NotNil(t, response)
	t.Cleanup(func() { _ = response.Body.Close() })
	require.NoError(t, response.Body.Close())
	require.Equal(t, originalHeaders, request.Header, "preparation must not mutate the caller's headers")
	require.Equal(t, 2, calls)
	require.Equal(t, 2, f.op.budget.attemptBudget.Snapshot().Attempts)
	require.EqualValues(t, 2, f.transport.sends.Load())
	require.Zero(t, f.transport.legacy.Load())
}

func nativeFollowupRedirect(location string) *http.Response {
	return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": []string{location}}, Body: io.NopCloser(strings.NewReader("redirect"))}
}

func nativeFollowupErrorContext(replaySafe bool) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("openai_uncertain_replay_safe", replaySafe)
	return c
}

func requireNativeFollowupSendFailure(t *testing.T, err error, state model.SendState) {
	t.Helper()
	var attempt *OpenAITransportAttemptError
	require.ErrorAs(t, err, &attempt)
	require.Equal(t, state, attempt.SendState)
	var failure *UpstreamFailoverError
	require.False(t, errors.As(err, &failure), "raw exchange failure must not carry an unclassified inner failover")
}

func nativeFollowupTLSFailure(t *testing.T, request *http.Request) error {
	t.Helper()
	trace := httptrace.ContextClientTrace(request.Context())
	require.NotNil(t, trace)
	require.NotNil(t, trace.GetConn)
	require.NotNil(t, trace.TLSHandshakeDone)
	trace.GetConn(request.URL.Host)
	trace.TLSHandshakeDone(tls.ConnectionState{}, io.ErrUnexpectedEOF)
	// No GotConn, HTTP trace event, or body read: the actual provider must
	// derive NotSent and retry eligibility from the failed handshake itself.
	return io.ErrUnexpectedEOF
}

func nativeFollowupHeaderValues(headers http.Header, name string) []string {
	var values []string
	for key, entries := range headers {
		if strings.EqualFold(key, name) {
			values = append(values, entries...)
		}
	}
	return values
}
