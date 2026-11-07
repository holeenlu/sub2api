package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/stretchr/testify/require"
)

type responseModeServiceAuthority struct{ checks int }

func (a *responseModeServiceAuthority) RevalidateDispatch(_ context.Context, identity model.PreparedIdentity) (execution.DispatchValidation, error) {
	a.checks++
	return execution.DispatchValidation{Purpose: identity.Purpose, Authorization: [32]byte{1}, Leases: [32]byte{2}}, nil
}

type responseModeHTTPTransport struct{ sends int }

func (p *responseModeHTTPTransport) RoundTripOnce(request *http.Request, _ native.TransportOptions) (*http.Response, error) {
	p.sends++
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}")), Request: request}, nil
}

func responseModeServiceFixture(t *testing.T) (context.Context, *ResponseModeExecution, *OpenAITurnBudget, *responseModeServiceAuthority) {
	t.Helper()
	budget := NewOpenAITurnBudget(nil)
	ctx := WithOpenAITurnBudget(t.Context(), budget)
	gate := &responseModeServiceAuthority{}
	d, err := execution.NewDispatcher(budget.control, "operation", budget.attemptBudget, budget.firstOutput, gate)
	require.NoError(t, err)
	core, err := execution.NewOperation(d, model.OperationResult{OperationID: "operation", UserID: 1, APIKeyID: 2, Protocol: model.ProtocolResponses, OriginalWireDigest: [32]byte{3}})
	require.NoError(t, err)
	require.NoError(t, core.Begin(ctx))
	controller, err := NewResponseModeExecution(core, budget, "bps", gate)
	require.NoError(t, err)
	return ctx, controller, budget, gate
}

func responseModeHTTPPreparation(t *testing.T, ctx context.Context, transport native.Transport) *ResponseModeHTTPAttempt {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/responses", strings.NewReader(`{"model":"selected-model","input":"hi"}`))
	require.NoError(t, err)
	identity := model.PreparedIdentity{OperationID: "operation", ResourceID: 4, Owner: model.ResourceOwner{AccountID: 4}, UserID: 1, APIKeyID: 2, Mode: "bps", Transport: "http", Role: model.AttemptGenerate, Binding: model.ModeBinding{BillingScope: "operation"}, GrantDigest: [32]byte{1}, BodyKind: model.BodyReplayable}
	p, err := PrepareResponseModeHTTP(ctx, native.Spec{Request: req, Identity: identity, Transport: native.TransportOptions{Profile: native.ProfileExcelBPS, AccountID: 4, AccountConcurrency: 2}, MaxReplayBodyBytes: 4096}, transport, model.AttemptTarget{Owner: identity.Owner, Mode: "bps", Endpoint: "/responses", Limit: 3})
	require.NoError(t, err)
	return p
}

func TestResponseModeHTTPConsumerUsesSealedSingleTransportAndOriginalBudget(t *testing.T) {
	ctx, controller, budget, gate := responseModeServiceFixture(t)
	transport := &responseModeHTTPTransport{}
	prepared := responseModeHTTPPreparation(t, ctx, transport)
	attempt, err := controller.Register(prepared)
	require.NoError(t, err)
	require.NoError(t, attempt.Start(ctx))
	require.Equal(t, http.StatusOK, prepared.Result().Response.StatusCode)
	require.Equal(t, 1, transport.sends)
	require.Equal(t, 1, budget.attemptBudget.Snapshot().Attempts, "no legacy HTTPUpstream.Do debit")
	require.GreaterOrEqual(t, gate.checks, 2, "admission and actual Open revalidate")
	require.ErrorIs(t, attempt.Start(ctx), execution.ErrOperationConsumed)
	require.Nil(t, controller.Snapshot().Attempts[0].Usage)
	require.Empty(t, controller.Snapshot().Attempts[0].BillingRoot)
	require.NoError(t, attempt.Observe(model.FactDelta{Usage: &model.UsageFact{Input: &model.TokenMeasurement{Value: 7}, Confidence: model.UsagePartial}}))
	require.NoError(t, attempt.Delivered(model.DeliveryReceipt{BusinessCommitted: true, BytesWritten: 3}))
	require.NoError(t, attempt.Close())
	next, err := controller.Register(responseModeHTTPPreparation(t, ctx, transport))
	require.NoError(t, err)
	require.ErrorIs(t, next.Start(ctx), execution.ErrDispatchOutputCommitted)
	require.Equal(t, 1, transport.sends)
	require.NoError(t, controller.Close())
}

func TestResponseModeOriginalCancellationStopsDetachedSend(t *testing.T) {
	ctx, controller, budget, _ := responseModeServiceFixture(t)
	transport := &responseModeHTTPTransport{}
	prepared := responseModeHTTPPreparation(t, context.WithoutCancel(ctx), transport)
	attempt, err := controller.Register(prepared)
	require.NoError(t, err)
	cause := errors.New("actual client cancellation")
	controller.Cancel(cause)
	require.ErrorIs(t, attempt.Start(context.WithoutCancel(ctx)), cause)
	require.Zero(t, transport.sends)
	require.Zero(t, budget.attemptBudget.Snapshot().Attempts)
	require.NoError(t, controller.Close())
}

type responseModeCapacityStore struct {
	requests  []routing.CapacityRequest
	remaining time.Duration
}

func (s *responseModeCapacityStore) CheckCapacityLease(_ context.Context, request routing.CapacityRequest) (time.Duration, error) {
	s.requests = append(s.requests, request)
	return s.remaining, nil
}
func (*responseModeCapacityStore) RefreshCapacityLease(context.Context, routing.CapacityRequest) (time.Duration, error) {
	return 0, errors.New("receiving owner must not renew source tokens")
}

func TestResponseModeHandoffChecksRealCapacityTokensAndCurrentPolicy(t *testing.T) {
	handoff := ResponseModeHandoff{
		Execution: execution.OperationTransfer{Mode: "cloud", Operation: model.OperationResult{OperationID: "op", UserID: 1, APIKeyID: 2}},
		Request:   NativeDispatchRequest{OperationID: "op", UserID: 1, APIKeyID: 2, APIKeyDigest: [32]byte{1}, RequestedModel: "original-client-model", ClientIP: "127.0.0.1", Transport: "http"},
		Capacity:  [2]routing.CapacityRequest{{ID: 1, Token: "actual-user-token", Limit: 2, User: true}, {ID: 4, Token: "actual-account-token", Limit: 3}},
	}
	handoff.Execution.ContextDigest = handoff.contextDigest()
	store := &responseModeCapacityStore{remaining: time.Minute}
	proof, err := handoff.VerifyCapacity(t.Context(), store, 2, 3)
	require.NoError(t, err)
	require.NotEqual(t, [32]byte{}, proof.Fingerprint)
	require.Equal(t, handoff.Capacity[:], store.requests)
	require.False(t, proof.ValidUntil.IsZero())
	_, err = handoff.VerifyCapacity(t.Context(), store, 2, 4)
	require.ErrorIs(t, err, routing.ErrCapacityPolicyChanged)
	store.remaining = 0
	_, err = handoff.VerifyCapacity(t.Context(), store, 2, 3)
	require.ErrorIs(t, err, routing.ErrCapacityLeaseLost)
	handoff.Request.RequestedModel = "changed-after-handoff"
	_, err = handoff.VerifyCapacity(t.Context(), store, 2, 3)
	require.ErrorIs(t, err, ErrNativeDispatchLeaseUnproven)
}
