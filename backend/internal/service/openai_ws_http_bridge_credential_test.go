//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
)

// Share the same durable account between the existing refresh-CAS fixture and
// the actual dispatch authority. Neither a new bearer nor a fake permission
// callback can bypass the real operation's preparation, admission and budget.
type wsBridgeCredentialAccounts struct {
	*nativeAuthorityAccounts
	refresh *refreshAPIAccountRepo
}

func (r *wsBridgeCredentialAccounts) UpdateOpenAIOAuthCredentialsIfUnchanged(ctx context.Context, expected *Account, credentials map[string]any) (bool, error) {
	return r.refresh.UpdateOpenAIOAuthCredentialsIfUnchanged(ctx, expected, credentials)
}

func (r *wsBridgeCredentialAccounts) SetOpenAIOAuthRefreshErrorIfUnchanged(ctx context.Context, expected *Account, reason string) (bool, error) {
	return r.refresh.SetOpenAIOAuthRefreshErrorIfUnchanged(ctx, expected, reason)
}

func (r *wsBridgeCredentialAccounts) SetOpenAIOAuthRefreshTempUnschedulableIfUnchanged(ctx context.Context, expected *Account, until time.Time, reason string) (bool, error) {
	return r.refresh.SetOpenAIOAuthRefreshTempUnschedulableIfUnchanged(ctx, expected, until, reason)
}

type wsBridgeCredentialFixture struct {
	*nativeAuthorityFixture
	service   *OpenAIGatewayService
	transport *nativeOperationTransport
	repo      *wsBridgeCredentialAccounts
	executor  *refreshAPIExecutorStub
	pricing   ChannelUsageFields
}

func newWSBridgeCredentialFixture(t *testing.T, attempts int, pricing ...ChannelUsageFields) *wsBridgeCredentialFixture {
	t.Helper()
	a := newNativeAuthorityFixture(t)
	a.resource.Type = AccountTypeOAuth
	a.resource.Credentials = nativeRefreshTestAccount().Credentials
	a.resource.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	repo := &wsBridgeCredentialAccounts{nativeAuthorityAccounts: a.accounts, refresh: &refreshAPIAccountRepo{account: a.resource}}
	executor := &refreshAPIExecutorStub{}
	executor.onRefresh = func() {
		executor.credentials = map[string]any{
			"access_token":  fmt.Sprintf("native-refreshed-%d", executor.refreshCalls),
			"refresh_token": "native-refresh-next", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
		}
	}
	provider := NewOpenAITokenProvider(repo, nil, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), openAITestRefreshExecutor{executor})
	transport := &nativeOperationTransport{}
	svc := &OpenAIGatewayService{
		cfg:         &config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, OpenAITurnMaxAttempts: attempts}},
		accountRepo: repo, userRepo: &nativeOperationUsers{user: a.key.User}, userSubRepo: &nativeOperationSubscriptions{},
		httpUpstream: transport, openAITokenProvider: provider,
	}
	fields := ChannelUsageFields{OriginalModel: "client-model"}
	if len(pricing) > 0 {
		fields = pricing[0]
	}
	return &wsBridgeCredentialFixture{nativeAuthorityFixture: a, service: svc, transport: transport, repo: repo, executor: executor, pricing: fields}
}

const wsBridgeCredentialPayload = `{"type":"response.create","model":"client-model","input":"hello"}`

func (f *wsBridgeCredentialFixture) turn(t *testing.T, parent context.Context, turn int, originalPayload ...[]byte) (context.Context, *gin.Context) {
	t.Helper()
	payload := []byte(wsBridgeCredentialPayload)
	if len(originalPayload) > 0 {
		payload = originalPayload[0]
	}
	request := f.gate.request
	request.Transport, request.OperationID = "ws", fmt.Sprintf("bridge-credential-turn-%d", turn)
	ctx, err := f.service.BindNativeWSDispatch(EnsureOpenAITurnBudget(parent, f.service.cfg), &APIKeyService{apiKeyRepo: f.keys}, request)
	require.NoError(t, err)
	ctx, err = f.service.BindNativeWSOperation(ctx, payload)
	require.NoError(t, err)
	bindWSRecoveryIntentFixture(t, f.service, ctx, f.key, f.pricing)
	user, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: f.key.UserID, User: true}, nil)
	require.NoError(t, err)
	t.Cleanup(user.ReleaseFunc)
	account, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: f.resource.ID}, nil)
	require.NoError(t, err)
	t.Cleanup(account.ReleaseFunc)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(ctx)
	c.Set("api_key", f.key)
	SetOpenAIClientTransport(c, OpenAIClientTransportWS)
	return ctx, c
}

func (f *wsBridgeCredentialFixture) forward(ctx context.Context, c *gin.Context, token string, turn int) (*OpenAIForwardResult, [][]byte, error) {
	var events [][]byte
	// Keep a selected snapshot separate from the repository's refreshed map.
	selected := *f.resource
	result, err := f.service.proxyOpenAIWSHTTPBridgeTurn(ctx, c, &selected, token,
		[]byte(wsBridgeCredentialPayload), len(wsBridgeCredentialPayload), "client-model", "", "", "", "", turn,
		func(event []byte) error { events = append(events, append([]byte(nil), event...)); return nil })
	return result, events, err
}

func TestOpenAIWSHTTPBridgeNative401RefreshUsesOriginalOperation(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		t.Run(fmt.Sprintf("repeated_401=%t", repeated), func(t *testing.T) {
			f := newWSBridgeCredentialFixture(t, 3)
			ctx, c := f.turn(t, t.Context(), 1)
			owner := nativeHTTPFromContext(ctx)
			budget := owner.dispatch.budget
			deadline := budget.firstOutput.Snapshot().Deadline
			var bearers, bodies []string
			f.transport.reply = func(req *http.Request, options native.TransportOptions) (*http.Response, error) {
				require.Same(t, owner, nativeHTTPFromContext(req.Context()))
				require.Same(t, budget, nativeDispatchFromContext(req.Context()).budget)
				require.Equal(t, deadline, budget.firstOutput.Snapshot().Deadline, "reprepare must preserve the first-output deadline")
				require.Equal(t, f.resource.ID, options.AccountID)
				require.Equal(t, "https://chatgpt.com/backend-api/codex/responses", req.URL.String())
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				bearers, bodies = append(bearers, req.Header.Get("Authorization")), append(bodies, string(body))
				if len(bearers) == 1 || repeated {
					return bpsCompletionResponse(http.StatusUnauthorized, `{"detail":"Unauthorized"}`), nil
				}
				return bpsCompletionResponse(http.StatusOK, bpsCompletedStream("resp_refreshed", "answer")), nil
			}
			result, events, err := f.forward(ctx, c, "old-at", 1)
			require.Equal(t, 1, f.executor.refreshCalls)
			require.Equal(t, 1, f.repo.refresh.successCASCalls)
			require.Equal(t, []string{"Bearer old-at", "Bearer native-refreshed-1"}, bearers)
			require.Equal(t, bodies[0], bodies[1])
			require.Zero(t, f.transport.legacy.Load())
			require.Equal(t, 2, budget.attemptBudget.Snapshot().Attempts)
			require.Equal(t, 1, budget.attemptBudget.Snapshot().PreparationRetries)
			facts := owner.core.Snapshot()
			require.Equal(t, sha256.Sum256([]byte(wsBridgeCredentialPayload)), facts.OriginalWireDigest)
			require.Len(t, facts.Attempts, 2)
			for _, attempt := range facts.Attempts {
				require.Equal(t, facts.OperationID, attempt.Identity.OperationID)
				require.Equal(t, model.SendSent, attempt.Send)
				require.Equal(t, "http", attempt.Identity.Transport)
				require.True(t, attempt.Closed)
			}
			require.Equal(t, "ws", owner.dispatch.request.Transport)
			require.NotEqual(t, facts.Attempts[0].Sequence, facts.Attempts[1].Sequence)
			require.NotEqual(t, facts.Attempts[0].Identity.GrantDigest, facts.Attempts[1].Identity.GrantDigest)
			require.Equal(t, facts.Attempts[0].Identity.BodyDigest, facts.Attempts[1].Identity.BodyDigest)
			if repeated {
				var failure *UpstreamFailoverError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, http.StatusUnauthorized, failure.StatusCode)
				require.Nil(t, result)
				require.Empty(t, events)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "resp_refreshed", result.ResponseID)
				require.NotEmpty(t, events)
				require.Equal(t, "response.completed", gjson.GetBytes(events[len(events)-1], "type").String())
				for _, event := range events {
					require.NotEqual(t, "error", gjson.GetBytes(event, "type").String())
				}
			}
		})
	}
}

func TestOpenAIWSHTTPBridgeCredentialRepreparePreservesEncoderFactsBeforeSend(t *testing.T) {
	for _, tc := range []struct {
		name          string
		pricingSource string
		wantBaseline  string
	}{
		{name: "default", wantBaseline: "client-model"},
		{name: "requested", pricingSource: BillingModelSourceRequested, wantBaseline: "client-model"},
		{name: "channel_mapped", pricingSource: BillingModelSourceChannelMapped, wantBaseline: "mapped-wire-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := ChannelUsageFields{OriginalModel: "client-model", ChannelMappedModel: "mapped-wire-model", BillingModelSource: tc.pricingSource}
			f := newWSBridgeCredentialFixture(t, 3, fields)
			const payload = `{"type":"response.create","model":"mapped-wire-model","service_tier":"priority","reasoning":{"effort":"high"},"input":"hello","tools":[{"type":"image_generation"}]}`
			// turn binds this policy through BindNativeIntentPricing before any
			// prepare/send. Never rewrite the resulting scope to fit an oracle.
			ctx, c := f.turn(t, t.Context(), 1, []byte(payload))
			owner := nativeHTTPFromContext(ctx)
			require.Empty(t, owner.core.Snapshot().Attempts)
			require.Zero(t, f.transport.sends.Load())
			pricing := owner.intent.request
			require.Equal(t, tc.pricingSource, pricing.Scope.BillingModelSource)
			require.Equal(t, "client-model", pricing.Scope.OriginalModel)
			require.Equal(t, "mapped-wire-model", pricing.channelMappedModel)
			// Deliberately stale incoming facts cannot win over this bridge encoder.
			ctx = WithNativeIntentModelFacts(ctx, NativeIntentModelFacts{
				Model: "stale-model", UpstreamModel: "stale-wire", BillingModel: "stale-price", ImageBillingModel: "stale-image",
				ServiceTier: "flex", ReasoningEffort: "low",
			})
			journal := f.service.usageBillingRepo.(*nativeApplicationJournal)
			var scopes []NativeIntentAttemptScope
			journal.beforeStore = func(_ context.Context, input *NativeUsageIntent) {
				require.EqualValues(t, len(scopes), f.transport.sends.Load(), "persist every actual attempt before its send")
				require.Equal(t, sha256.Sum256([]byte(payload)), input.OriginalWireDigest)
				require.Same(t, pricing, owner.intent.request, "credential reprepare retains the original pricing policy")
				require.Equal(t, tc.pricingSource, input.Request.BillingModelSource)
				require.Equal(t, "client-model", input.Request.OriginalModel)
				require.Equal(t, pricing.Scope.PricingAt, input.Request.PricingAt)
				scopes = append(scopes, input.Attempt)
			}
			f.transport.reply = func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
				require.Len(t, scopes, int(f.transport.sends.Load()))
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				scope := scopes[len(scopes)-1]
				require.Equal(t, sha256.Sum256(body), scope.Prepared.BodyDigest)
				require.Equal(t, "mapped-wire-model", gjson.GetBytes(body, "model").String())
				require.Contains(t, scope.CandidateModels, "mapped-wire-model")
				require.Contains(t, scope.CandidateModels, "client-model")
				// Correct the old oracle using prepareOpenAIUsage's policy: the
				// default/requested baseline is the client name; channel_mapped
				// explicitly selects the mapped name. Wire identity stays separate.
				require.Equal(t, tc.wantBaseline, scope.BaselineModel)
				require.Equal(t, tc.wantBaseline, scope.CandidateModels[0])
				require.Equal(t, "mapped-wire-model", scope.ChannelMappedModel)
				require.Equal(t, "gpt-image-1", scope.ImageBillingModel, "image producer comes from the explicit ingress decision, not the text slug")
				require.Equal(t, "priority", scope.ServiceTier)
				require.Equal(t, "high", scope.ReasoningEffort)
				require.NotContains(t, scope.CandidateModels, "stale-price")
				require.NotContains(t, scope.CandidateModels, "stale-model")
				require.NotContains(t, scope.CandidateModels, "stale-wire")
				if f.transport.sends.Load() == 1 {
					return bpsCompletionResponse(http.StatusUnauthorized, `{"detail":"Unauthorized"}`), nil
				}
				return bpsCompletionResponse(http.StatusOK, bpsCompletedStream("resp_encoder_facts", "answer")), nil
			}
			selected := *f.resource
			_, err := f.service.proxyOpenAIWSHTTPBridgeTurn(ctx, c, &selected, "old-at", []byte(payload), len(payload),
				"client-model", "gpt-image-1", "", "", "", 1, func([]byte) error { return nil })
			require.NoError(t, err)
			require.EqualValues(t, 2, f.transport.sends.Load())
			require.Len(t, scopes, 2)
			require.NotEqual(t, scopes[0].Prepared.GrantDigest, scopes[1].Prepared.GrantDigest)
			require.Equal(t, scopes[0].Prepared.BodyDigest, scopes[1].Prepared.BodyDigest)
			require.NotEqual(t, scopes[0].Sequence, scopes[1].Sequence)
			require.Equal(t, 1, f.executor.refreshCalls)
			require.Zero(t, f.transport.legacy.Load())
		})
	}
}

func TestOpenAIWSHTTPBridgeCredentialRecoveryRespectsAttemptBudget(t *testing.T) {
	f := newWSBridgeCredentialFixture(t, 1)
	ctx, c := f.turn(t, t.Context(), 1)
	owner := nativeHTTPFromContext(ctx)
	f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
		return bpsCompletionResponse(http.StatusUnauthorized, `{"detail":"Unauthorized"}`), nil
	}
	result, events, err := f.forward(ctx, c, "old-at", 1)
	require.ErrorIs(t, err, execution.ErrAttemptBudgetExhausted)
	require.Nil(t, result)
	require.Empty(t, events)
	require.Equal(t, 1, f.executor.refreshCalls)
	require.EqualValues(t, 1, f.transport.sends.Load(), "refresh cannot reopen the exhausted generation budget")
	require.Zero(t, f.transport.legacy.Load())
	require.Equal(t, 1, owner.dispatch.budget.attemptBudget.Snapshot().Attempts)
	require.Len(t, owner.core.Snapshot().Attempts, 1)
	require.Equal(t, model.SendSent, owner.core.Snapshot().Attempts[0].Send)
}

func TestOpenAIWSHTTPBridgeCredentialRecoveryResetsForNextTurn(t *testing.T) {
	f := newWSBridgeCredentialFixture(t, 3)
	f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
		if f.transport.sends.Load()%2 == 1 {
			return bpsCompletionResponse(http.StatusUnauthorized, `{"detail":"Unauthorized"}`), nil
		}
		return bpsCompletionResponse(http.StatusOK, bpsCompletedStream("resp_next", "answer")), nil
	}
	var previous *nativeHTTPOperation
	for turn := 1; turn <= 2; turn++ {
		ctx, c := f.turn(t, t.Context(), turn)
		owner := nativeHTTPFromContext(ctx)
		token := f.resource.GetOpenAIAccessToken()
		_, events, err := f.forward(ctx, c, token, turn)
		require.NoError(t, err)
		require.NotEmpty(t, events)
		require.Equal(t, turn, f.executor.refreshCalls)
		require.Equal(t, 2, owner.dispatch.budget.attemptBudget.Snapshot().Attempts)
		require.Equal(t, 1, owner.dispatch.budget.attemptBudget.Snapshot().PreparationRetries)
		require.NoError(t, FinishNativeWSOperation(ctx, nil))
		if previous != nil {
			require.NotSame(t, previous, owner)
			require.NotSame(t, previous.dispatch.budget, owner.dispatch.budget)
			require.NotEqual(t, previous.dispatch.request.OperationID, owner.dispatch.request.OperationID)
		}
		previous = owner
	}
	require.EqualValues(t, 4, f.transport.sends.Load())
	require.Zero(t, f.transport.legacy.Load())
}

func TestOpenAIWSHTTPBridgeRevokedCredentialDoesNotRefresh(t *testing.T) {
	for _, code := range []string{"token_revoked", "token_invalidated"} {
		t.Run(code, func(t *testing.T) {
			f := newWSBridgeCredentialFixture(t, 3)
			ctx, c := f.turn(t, t.Context(), 1)
			f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
				return bpsCompletionResponse(http.StatusUnauthorized, fmt.Sprintf(`{"error":{"code":%q,"message":"Unauthorized"}}`, code)), nil
			}
			_, _, err := f.forward(ctx, c, "old-at", 1)
			require.Error(t, err)
			require.Zero(t, f.executor.refreshCalls)
			require.Zero(t, f.repo.refresh.successCASCalls)
			require.EqualValues(t, 1, f.transport.sends.Load())
			require.Zero(t, nativeDispatchFromContext(ctx).budget.attemptBudget.Snapshot().PreparationRetries)
		})
	}
}

type wsBridgeCredentialBody struct {
	io.Reader
	onClose func()
	once    sync.Once
}

func (b *wsBridgeCredentialBody) Close() error { b.once.Do(b.onClose); return nil }

func TestOpenAIWSHTTPBridgeCanceled401DoesNotRecover(t *testing.T) {
	for _, detached := range []bool{false, true} {
		t.Run(fmt.Sprintf("drain_context=%t", detached), func(t *testing.T) {
			f := newWSBridgeCredentialFixture(t, 3)
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx, c := f.turn(t, parent, 1)
			if detached {
				ctx = context.WithoutCancel(ctx)
			}
			f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: &wsBridgeCredentialBody{
					Reader: strings.NewReader(`{"detail":"Unauthorized"}`), onClose: cancel,
				}}, nil
			}
			_, events, err := f.forward(ctx, c, "old-at", 1)
			require.ErrorIs(t, err, context.Canceled)
			var failure *UpstreamFailoverError
			require.False(t, errors.As(err, &failure))
			require.Empty(t, events)
			require.Zero(t, f.executor.refreshCalls)
			require.EqualValues(t, 1, f.transport.sends.Load())
			require.Zero(t, nativeDispatchFromContext(ctx).budget.attemptBudget.Snapshot().PreparationRetries)
		})
	}
}

func TestOpenAIWSHTTPBridgeCredentialRefreshRechecksAuthority(t *testing.T) {
	f := newWSBridgeCredentialFixture(t, 3)
	ctx, c := f.turn(t, t.Context(), 1)
	onRefresh := f.executor.onRefresh
	f.executor.onRefresh = func() { onRefresh(); f.key.Status = StatusDisabled }
	f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
		return bpsCompletionResponse(http.StatusUnauthorized, `{"detail":"Unauthorized"}`), nil
	}
	_, _, err := f.forward(ctx, c, "old-at", 1)
	require.Error(t, err)
	require.Equal(t, 1, f.executor.refreshCalls)
	require.EqualValues(t, 1, f.transport.sends.Load(), "a persisted new grant cannot bypass current principal admission")
	require.Len(t, nativeHTTPFromContext(ctx).core.Snapshot().Attempts, 1)
	require.Zero(t, f.transport.legacy.Load())
}

func TestOpenAIWSHTTPBridgeStreamFailuresDoNotRefreshOrReplay(t *testing.T) {
	for _, failure := range []string{"stream_401", "stream_401_after_output", "read_error_after_output"} {
		t.Run(failure, func(t *testing.T) {
			f := newWSBridgeCredentialFixture(t, 3)
			ctx, c := f.turn(t, t.Context(), 1)
			prefix := ""
			if failure != "stream_401" {
				prefix = `data: {"type":"response.output_text.delta","response_id":"resp_partial_auth","delta":"partial"}` + "\n\n"
			}
			var body io.Reader
			if failure == "read_error_after_output" {
				body = io.MultiReader(strings.NewReader(prefix), iotest.ErrReader(io.ErrUnexpectedEOF))
			} else {
				body = strings.NewReader(prefix + `data: {"type":"response.failed","response":{"id":"resp_partial_auth","status":"failed","error":{"status_code":401,"code":"unauthorized","message":"access token rejected"}}}` + "\n\n")
			}
			f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(body)}, nil
			}
			_, events, err := f.forward(ctx, c, "old-at", 1)
			require.Zero(t, f.executor.refreshCalls)
			require.EqualValues(t, 1, f.transport.sends.Load())
			require.Zero(t, f.transport.legacy.Load())
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover))
			require.Len(t, nativeHTTPFromContext(ctx).core.Snapshot().Attempts, 1)
			if failure != "stream_401" {
				require.NotEmpty(t, events)
				require.Equal(t, "response.output_text.delta", gjson.GetBytes(events[0], "type").String())
				require.True(t, NativeHTTPOutputCommitted(ctx))
			}
		})
	}
}

func TestOpenAIWSHTTPBridgeAfterBPSDisabledRequiresIndependentNativeGrant(t *testing.T) {
	for _, route := range []string{"current_bps_operation", "independent_native", "unsealed_excel_bearer", "excel_only", "missing_native_grant"} {
		t.Run(route, func(t *testing.T) {
			f := newWSBridgeCredentialFixture(t, 3)
			f.service.settingService = excelBPSSettingsTestService()
			f.resource.Extra = map[string]any{"openai_excel_bps": false}
			if route == "excel_only" {
				f.resource.Credentials["client_id"] = openai.ExcelClientID
			} else if route == "missing_native_grant" {
				delete(f.resource.Credentials, "access_token")
				delete(f.resource.Credentials, "refresh_token")
			}
			ctx, c := f.turn(t, t.Context(), 1)
			if route == "current_bps_operation" {
				pinExcelBPSExecution(ctx)
			}
			f.transport.reply = func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
				require.Equal(t, "Bearer old-at", req.Header.Get("Authorization"), "an ingress Excel bearer cannot supply the native grant")
				require.Equal(t, "/backend-api/codex/responses", req.URL.Path)
				return bpsCompletionResponse(http.StatusOK, bpsCompletedStream("resp_native", "answer")), nil
			}
			token := "independent-excel-bearer"
			if route == "independent_native" {
				// Model selection and ingress resolve the Native credential before
				// invoking this already-selected adapter. An Excel bearer is not
				// evidence of that generation; keep it as a separate negative case.
				var err error
				token, _, err = f.service.GetAccessToken(ctx, f.resource)
				require.NoError(t, err)
				require.Equal(t, "old-at", token)
			}
			result, _, err := f.forward(ctx, c, token, 1)
			if route == "independent_native" {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "resp_native", result.ResponseID)
				require.Equal(t, "/v1/responses", result.UpstreamEndpoint)
				require.EqualValues(t, 1, f.transport.sends.Load())
				require.Equal(t, 1, nativeDispatchFromContext(ctx).budget.attemptBudget.Snapshot().Attempts)
			} else {
				require.Error(t, err)
				require.Nil(t, result)
				require.Zero(t, f.transport.sends.Load(), "a pinned BPS operation or unavailable native grant cannot send Native")
				require.Empty(t, nativeHTTPFromContext(ctx).core.Snapshot().Attempts)
				if route == "unsealed_excel_bearer" {
					require.ErrorIs(t, err, ErrNativeDispatchGrantUnproven)
				}
				if route == "current_bps_operation" {
					var failure *UpstreamFailoverError
					require.ErrorAs(t, err, &failure)
					require.True(t, failure.RequestNotSent)
					require.EqualValues(t, "basispoints_route_unavailable", failure.Reason)
					require.True(t, excelBPSExecutionPinned(ctx))
				}
			}
			require.Zero(t, f.transport.legacy.Load())
			require.Zero(t, f.executor.refreshCalls)
			require.Equal(t, false, f.resource.Extra["openai_excel_bps"], "dispatch cannot reopen an automatically disabled route")
		})
	}
}
