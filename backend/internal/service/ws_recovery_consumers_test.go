package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
)

// Use the real intent validation/merge boundary with an in-memory journal.
// This fixture does not represent a PostgreSQL commit or live-provider test.
func bindWSRecoveryIntentFixture(t *testing.T, svc *OpenAIGatewayService, ctx context.Context, key *APIKey, pricing ...ChannelUsageFields) {
	t.Helper()
	if svc.billingService == nil {
		svc.billingService = NewBillingService(svc.cfg, nil)
	}
	if svc.usageBillingRepo == nil {
		svc.usageBillingRepo = &nativeApplicationJournal{t: t}
	}
	fields := ChannelUsageFields{OriginalModel: "client-model"}
	if len(pricing) > 0 {
		fields = pricing[0]
	}
	require.NoError(t, svc.BindNativeIntentPricing(ctx, NativeIntentPricingInput{
		APIKey: key, User: key.User, APIKeyService: &APIKeyService{},
		PricingAt:          time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC),
		ChannelUsageFields: fields,
	}))
}

// The service-package fixture adapts the actual failure to the same execution
// recovery port as ingress. Policy stays in execution.Recovery; all budget,
// pin/exclusion and scheduler effects below use the production functions.
type wsRecoveryPorts struct {
	t        *testing.T
	svc      *OpenAIGatewayService
	account  *Account
	failure  *UpstreamFailoverError
	oauth429 OpenAIOAuth429FailoverState
	waits    []time.Duration
	waitErr  error
	excluded int
}

func (p *wsRecoveryPorts) OutputCommitted(ctx context.Context) bool { return TurnOutputCommitted(ctx) }
func (p *wsRecoveryPorts) RetryAllowed(ctx context.Context) bool {
	return OpenAITurnRetryAllowed(ctx, p.account.ID, p.failure.ResponseBody)
}
func (p *wsRecoveryPorts) Prepare(ctx context.Context) error {
	return PrepareTurnRecovery(ctx, p.failure)
}
func (p *wsRecoveryPorts) Wait(ctx context.Context, delay time.Duration) error {
	p.waits = append(p.waits, delay)
	p.waitErr = WaitOpenAITurnRetry(ctx, delay)
	return p.waitErr
}
func (p *wsRecoveryPorts) Pin(ctx context.Context) { PinTurnRetryAccount(ctx, p.account) }
func (p *wsRecoveryPorts) Exclude(ctx context.Context) {
	p.excluded++
	ExcludeTurnRecoveryAccount(ctx, p.account, p.failure)
	PinTurnRetryAccount(ctx, nil)
}
func (p *wsRecoveryPorts) TempUnschedule(ctx context.Context) {
	p.t.Error("bound-account recovery must not add a health penalty for wait-budget exhaustion")
}
func (p *wsRecoveryPorts) StopSwitch(_ context.Context, count int) bool {
	return p.svc.ShouldStopOpenAIOAuth429Failover(p.account, p.failure.StatusCode, count, &p.oauth429)
}

// A local WS peer supplies the semantic refusal after a real response.create.
// Recovery consumes its unmodified Retry-After on the original wait allowance;
// no sleep or synthetic budget reset is needed to exercise exhaustion.
func TestWSRecoverySemantic429WaitExhaustionUsesOriginalOwner(t *testing.T) {
	const limited = `{"type":"error","status":429,"headers":{"Retry-After":"67"},"error":{"code":"turn_rate_limit_exceeded","type":"rate_limit_error","message":"rate limit exceeded"}}`
	const completed = `{"type":"response.completed","response":{"id":"resp_recovered","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`
	for _, name := range []string{"portable", "bound_input", "cancel_before_recovery"} {
		t.Run(name, func(t *testing.T) {
			var sends atomic.Int32
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "7200")
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				if _, _, err = conn.Read(r.Context()); err != nil {
					return
				}
				event := limited
				if sends.Add(1) == 2 {
					event = completed
				}
				_ = conn.Write(r.Context(), coderws.MessageText, []byte(event))
			}))
			defer peer.Close()
			f := nativeWSConsumerService(t, OpenAIWSIngressModeCtxPool, peer.URL)
			f.resource.Type = AccountTypeOAuth
			f.resource.Credentials = map[string]any{"access_token": "fixture-owner-one"}
			next := *f.resource
			next.ID = 8
			next.Credentials = map[string]any{"access_token": "fixture-owner-two"}
			f.accounts.accounts[next.ID] = &next
			bindWSRecoveryIntentFixture(t, f.service, f.ctx, f.key)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			// Override only the dial destination. The normal coder transport
			// captures the actual local handshake and consumes the real permit.
			dialer := newDefaultOpenAIWSClientDialer()
			f.service.getOpenAIWSConnPool().setClientDialerForTest(wsLifecycleDialer(func(ctx context.Context, endpoint string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
				if endpoint != "wss://chatgpt.com/backend-api/codex/responses" || proxy != "" {
					return nil, 0, nil, fmt.Errorf("unexpected fixture target %q", endpoint)
				}
				return dialer.Dial(ctx, "ws"+strings.TrimPrefix(peer.URL, "http"), headers, proxy)
			}))
			user, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: f.key.UserID, User: true}, nil)
			require.NoError(t, err)
			defer user.ReleaseFunc()
			for _, account := range []*Account{f.resource, &next} {
				slot, err := routing.AcquireCapacity(ctx, f.capacity, routing.CapacityRequest{ID: account.ID, Token: fmt.Sprint(account.ID), Limit: 2}, nil)
				require.NoError(t, err)
				require.True(t, slot.Acquired)
				defer slot.ReleaseFunc()
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			c.Set("api_key", f.key)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			body := []byte(`{"model":"client-model","input":"hello","stream":true}`)
			if name == "bound_input" {
				body = []byte(`{"model":"client-model","input":[{"role":"user","content":[{"type":"input_file","file_id":"file_owner_one"}]}],"stream":true}`)
			}
			var request map[string]any
			require.NoError(t, decodeOpenAIJSONUseNumber(body, &request))
			owner := nativeHTTPFromContext(ctx)
			budget := owner.dispatch.budget
			deadline := budget.firstOutput.Snapshot().Deadline
			recovery := execution.NewRecovery(1, false)
			selected := f.resource
			var ports *wsRecoveryPorts
			var result *OpenAIForwardResult
			err = owner.core.Execute(ctx, func() NativeOperationStep {
				require.Same(t, owner, nativeHTTPFromContext(ctx))
				require.Same(t, budget, nativeDispatchFromContext(ctx).budget)
				require.Equal(t, deadline, budget.firstOutput.Snapshot().Deadline)
				var forwardErr error
				result, forwardErr = f.service.forwardOpenAIWSV2(ctx, c, selected, request, "", "semantic-wait-recovery", selected.GetOpenAIAccessToken(),
					OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, false, true, "client-model", "client-model", time.Now(), 1, time.Time{}, "", nil)
				if forwardErr == nil {
					return NativeOperationDone()
				}
				var failure *UpstreamFailoverError
				require.ErrorAs(t, forwardErr, &failure)
				require.True(t, failure.RetryableOnSameAccount)
				require.Equal(t, "67", failure.ResponseHeaders.Get("Retry-After"))
				require.False(t, failure.RequestNotSent)
				ports = &wsRecoveryPorts{t: t, svc: f.service, account: selected, failure: failure}
				if name == "cancel_before_recovery" {
					cancel()
				}
				decision := recovery.RecoverBoundAccount(ctx, selected.ID, &execution.RecoveryFailure{
					StatusCode: failure.StatusCode, RetryNextAccount: failure.ShouldRetryNextAccount(),
					RetryableOnSameAccount: failure.RetryableOnSameAccount, SameAccountRetryDelay: failure.SameAccountRetryDelay,
					SameAccountRetryDeadline: failure.SameAccountRetryDeadline, SameAccountRetryMax: failure.SameAccountRetryMax,
				}, OpenAIResponsesAccountReplayAllowed(body), true, selected.GetPoolModeRetryCount(), execution.RecoveryPorts{Budget: ports, Accounts: ports})
				if decision.Action == execution.RecoveryContinue {
					require.Equal(t, "portable", name)
					selected = &next
					return NativeOperationContinue()
				}
				return NativeOperationStep{Err: errors.Join(forwardErr, decision.Err)}
			})
			require.NotNil(t, ports)
			require.Equal(t, "67", ports.failure.ResponseHeaders.Get("Retry-After"), "wait exhaustion must not rewrite the actual provider minimum")
			require.Zero(t, f.transport.sends.Load(), "rate limiting does not permit switching to HTTP")
			require.Zero(t, f.transport.legacy.Load())
			wantAttempts := 1
			if name == "portable" {
				require.NoError(t, err)
				require.Equal(t, "resp_recovered", result.ResponseID)
				require.Equal(t, 1, recovery.SwitchCount)
				require.Equal(t, 1, ports.excluded)
				require.Nil(t, TurnRetryAccount(ctx))
				wantAttempts = 2
			} else {
				require.Error(t, err)
				require.Empty(t, recorder.Body.String())
				require.Zero(t, recovery.SwitchCount)
				require.Zero(t, ports.excluded)
			}
			if name == "cancel_before_recovery" {
				require.Empty(t, ports.waits)
			} else {
				require.Equal(t, []time.Duration{67 * time.Second}, ports.waits)
				require.ErrorIs(t, ports.waitErr, execution.ErrRetryWaitBudgetExhausted)
				require.Equal(t, 1, recovery.SameAccountRetryCount[f.resource.ID])
			}
			require.EqualValues(t, wantAttempts, sends.Load())
			require.Equal(t, wantAttempts, budget.attemptBudget.Snapshot().Attempts)
			facts := owner.core.Snapshot()
			require.Len(t, facts.Attempts, wantAttempts)
			for _, attempt := range facts.Attempts {
				require.Equal(t, facts.OperationID, attempt.Identity.OperationID)
				require.Equal(t, model.SendSent, attempt.Send)
				require.True(t, attempt.Closed)
			}
			if wantAttempts == 2 {
				require.NotEqual(t, facts.Attempts[0].Identity.ResourceID, facts.Attempts[1].Identity.ResourceID)
				require.NotEqual(t, facts.Attempts[0].Sequence, facts.Attempts[1].Sequence)
			}
		})
	}
}
