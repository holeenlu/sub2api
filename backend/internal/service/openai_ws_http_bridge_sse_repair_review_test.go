//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const bridgeSSEReviewPayload = `{"type":"response.create","model":"client-model","store":false,"input":[{"type":"function_call","id":"fc_old","call_id":"call_old","name":"echo","arguments":"{\"ticket\":9007199254740993}"},{"type":"function_call_output","call_id":"call_old","output":"answer"},{"type":"message","role":"user","id":"msg_old","content":"continue"}]}`
const bridgeSSEReviewProblem = `{"code":"unsupported_parameter","type":"invalid_request_error","param":"input","message":"Supplied input item IDs require persisted-item lookup that is not supported by rustponsesapi."}`
const bridgeSSEReviewCompleted = `{"type":"response.completed","response":{"id":"fixed","status":"completed","model":"client-model","output":[],"usage":{"input_tokens":3,"output_tokens":1}}}`

func bridgeSSEReviewRejection(failed bool, usage string) string {
	if failed {
		return `{"type":"response.failed","response":{"id":"rejected","status":"failed","model":"rejected-model","error":` + bridgeSSEReviewProblem + usage + `}}`
	}
	return `{"type":"error","error":` + bridgeSSEReviewProblem + usage + `}`
}

func bridgeSSEReviewWire(event string) string { return "data: " + event + "\n\n" }

type bridgeSSEReviewBody struct {
	io.Reader
	onClose func()
	once    sync.Once
	closed  atomic.Bool
	closes  atomic.Int32
}

func (b *bridgeSSEReviewBody) Close() error {
	b.once.Do(func() {
		if b.onClose != nil {
			b.onClose()
		}
		b.closes.Add(1)
		b.closed.Store(true)
	})
	return nil
}

// Reuse the existing OAuth authority/transport fixture, but bind the actual
// test payload as this WS operation's original wire. No permit or receipt is
// synthesized, and these local journal ports do not prove a PG commit.
type bridgeSSEReviewFixture struct {
	*wsBridgeCredentialFixture
	ctx                   context.Context
	c                     *gin.Context
	payload               []byte
	userSlot, accountSlot *routing.AcquireResult
}

func newBridgeSSEReviewFixture(t *testing.T, parent context.Context, payload string, attempts int, oauth bool) *bridgeSSEReviewFixture {
	t.Helper()
	base := newWSBridgeCredentialFixture(t, attempts)
	base.service.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 30
	if !oauth {
		base.resource.Type = AccountTypeAPIKey
		base.resource.Credentials = map[string]any{"api_key": "synthetic-upstream-key"}
	}
	request := base.gate.request
	request.Transport, request.OperationID = "ws", "ws-sse-repair-review"
	ctx, err := base.service.BindNativeWSDispatch(EnsureOpenAITurnBudget(parent, base.service.cfg), &APIKeyService{apiKeyRepo: base.keys}, request)
	require.NoError(t, err)
	ctx, err = base.service.BindNativeWSOperation(ctx, []byte(payload))
	require.NoError(t, err)
	bindWSRecoveryIntentFixture(t, base.service, ctx, base.key)
	user, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: base.key.UserID, User: true}, nil)
	require.NoError(t, err)
	account, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: base.resource.ID}, nil)
	require.NoError(t, err)
	t.Cleanup(user.ReleaseFunc)
	t.Cleanup(account.ReleaseFunc)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(ctx)
	c.Set("api_key", base.key)
	SetOpenAIClientTransport(c, OpenAIClientTransportWS)
	if previous := gjson.Get(payload, "previous_response_id").String(); previous != "" {
		require.NoError(t, base.service.bindResponseMode(ctx, *base.key.GroupID, previous, UsageAuthorizationModeNative, time.Minute))
	}
	return &bridgeSSEReviewFixture{wsBridgeCredentialFixture: base, ctx: ctx, c: c, payload: []byte(payload), userSlot: user, accountSlot: account}
}

func (f *bridgeSSEReviewFixture) forward(write func([]byte) error) (*OpenAIForwardResult, error) {
	token := f.resource.GetOpenAIAccessToken()
	if f.resource.Type == AccountTypeAPIKey {
		token = "synthetic-upstream-key"
	}
	selected := *f.resource
	return f.service.proxyOpenAIWSHTTPBridgeTurn(f.ctx, f.c, &selected, token, f.payload, len(f.payload), "client-model", "", "", "", "", 1, write)
}

func TestNativeHTTPBridgeSSERepairReviewLegalAttempts(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "error"
		if failed {
			name = "response_failed"
		}
		t.Run(name, func(t *testing.T) {
			f := newBridgeSSEReviewFixture(t, t.Context(), bridgeSSEReviewPayload, 3, true)
			owner := nativeHTTPFromContext(f.ctx)
			budget, pricing := owner.dispatch.budget, owner.intent.request
			deadline := budget.firstOutput.Snapshot().Deadline
			journal := f.service.usageBillingRepo.(*nativeApplicationJournal)
			first := &bridgeSSEReviewBody{Reader: strings.NewReader(bridgeSSEReviewWire(bridgeSSEReviewRejection(failed, "")))}
			last := &bridgeSSEReviewBody{Reader: strings.NewReader(bridgeSSEReviewWire(bridgeSSEReviewCompleted))}
			var scopes []NativeIntentAttemptScope
			journal.beforeStore = func(ctx context.Context, in *NativeUsageIntent) {
				require.EqualValues(t, len(scopes), f.transport.sends.Load(), "preserve the actual scope before every network send")
				require.Same(t, owner, nativeHTTPFromContext(ctx))
				require.Equal(t, sha256.Sum256(f.payload), in.OriginalWireDigest)
				require.Equal(t, pricing.Scope.PricingAt, in.Request.PricingAt)
				if len(scopes) > 0 {
					require.True(t, first.closed.Load(), "old body must finish closing before new preservation")
				}
				scopes = append(scopes, in.Attempt)
			}
			var sent [][]byte
			f.transport.reply = func(req *http.Request, options native.TransportOptions) (*http.Response, error) {
				require.Same(t, owner, nativeHTTPFromContext(req.Context()))
				require.Same(t, budget, nativeDispatchFromContext(req.Context()).budget)
				require.Same(t, pricing, owner.intent.request)
				require.Equal(t, deadline, budget.firstOutput.Snapshot().Deadline, "repair must not advance first-output time")
				require.Equal(t, f.resource.ID, options.AccountID)
				for _, slot := range []*routing.AcquireResult{f.userSlot, f.accountSlot} {
					_, err := slot.Verify(req.Context(), slot.Policy())
					require.NoError(t, err)
				}
				wire, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				sent = append(sent, wire)
				require.Len(t, scopes, len(sent))
				require.Equal(t, sha256.Sum256(wire), scopes[len(sent)-1].Prepared.BodyDigest)
				stored, err := journal.LoadNativeUsageIntent(req.Context(), billing.NativeIntentKey{OperationID: owner.dispatch.request.OperationID, APIKeyID: f.key.ID})
				require.NoError(t, err)
				require.Len(t, stored.Attempts, len(sent))
				body := first
				if len(sent) == 2 {
					require.True(t, first.closed.Load())
					require.True(t, owner.core.Snapshot().Attempts[0].Closed)
					body = last
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body}, nil
			}
			var writes [][]byte
			result, err := f.forward(func(b []byte) error { writes = append(writes, append([]byte(nil), b...)); return nil })
			require.NoError(t, err)
			require.NotNil(t, result)
			require.EqualValues(t, 2, f.transport.sends.Load())
			require.Zero(t, f.transport.legacy.Load())
			require.Zero(t, f.executor.refreshCalls)
			require.Len(t, writes, 1, "do not deliver the rejected attempt before repair")
			require.Equal(t, "response.completed", gjson.GetBytes(writes[0], "type").String())
			require.Equal(t, 3, result.Usage.InputTokens)
			require.Equal(t, 1, result.Usage.OutputTokens)
			require.Equal(t, "client-model", result.UpstreamResponseModel)
			require.False(t, result.UpstreamResponseModelConflict)
			require.True(t, gjson.GetBytes(sent[0], "input.0.id").Exists())
			require.False(t, gjson.GetBytes(sent[1], "input.0.id").Exists())
			require.Contains(t, string(sent[1]), "9007199254740993")
			require.True(t, AnalyzeToolCallOutputContextCoverageBytes(sent[1]).ContextCoversAllCallIDs)
			require.NotEqual(t, scopes[0].Prepared.BodyDigest, scopes[1].Prepared.BodyDigest)
			require.Equal(t, scopes[0].Prepared.GrantDigest, scopes[1].Prepared.GrantDigest)
			require.NotEqual(t, scopes[0].Sequence, scopes[1].Sequence)
			snapshot := owner.core.Snapshot()
			require.Len(t, snapshot.Attempts, 2)
			for _, attempt := range snapshot.Attempts {
				require.Equal(t, snapshot.OperationID, attempt.Identity.OperationID)
				require.Equal(t, "http", attempt.Identity.Transport)
				require.Equal(t, model.SendSent, attempt.Send)
				require.True(t, attempt.Closed)
			}
			require.Equal(t, "ws", owner.dispatch.request.Transport)
			require.Equal(t, 2, budget.attemptBudget.Snapshot().Attempts)
			require.Zero(t, budget.attemptBudget.Snapshot().PreparationRetries)
			require.EqualValues(t, 1, first.closes.Load())
			require.EqualValues(t, 1, last.closes.Load())
		})
	}
}

func TestNativeHTTPBridgeSSERepairReviewGuards(t *testing.T) {
	for _, tc := range []struct {
		name, prelude, usage, tail     string
		apiKey, previous, writeFailure bool
	}{
		{name: "input_usage", usage: `,"usage":{"input_tokens":7}`},
		{name: "cache_only_usage", usage: `,"usage":{"input_tokens_details":{"cached_tokens":7}}`},
		{name: "image_output", prelude: `{"type":"response.output_item.done","item":{"id":"img_seen","type":"image_generation_call","status":"completed","result":"cGl4ZWw="}}`},
		{name: "delivered_output", prelude: `{"type":"response.output_text.delta","delta":"delivered"}`},
		{name: "staged_metadata", prelude: `{"type":"response.created","response":{"id":"rejected"}}`},
		{name: "concatenated_tail", tail: bridgeSSEReviewCompleted},
		{name: "malformed_tail", tail: "broken-json"},
		{name: "api_key_relay", apiKey: true},
		{name: "previous_response", previous: true},
		{name: "write_failure", prelude: `{"type":"response.output_text.delta","delta":"delivered"}`, writeFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := bridgeSSEReviewPayload
			if tc.previous {
				payload = strings.TrimSuffix(payload, "}") + `,"previous_response_id":"resp_previous"}`
			}
			f := newBridgeSSEReviewFixture(t, t.Context(), payload, 3, !tc.apiKey)
			wire := bridgeSSEReviewWire(bridgeSSEReviewRejection(false, tc.usage) + tc.tail)
			if tc.prelude != "" {
				wire = bridgeSSEReviewWire(tc.prelude) + wire
			}
			first := &bridgeSSEReviewBody{Reader: strings.NewReader(wire)}
			f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: first}, nil
			}
			var writes [][]byte
			result, err := f.forward(func(b []byte) error {
				writes = append(writes, append([]byte(nil), b...))
				if tc.writeFailure {
					return io.ErrClosedPipe
				}
				return nil
			})
			require.EqualValues(t, 1, f.transport.sends.Load(), "the guard must be exercised after a real HTTP 200 send")
			require.Zero(t, f.transport.legacy.Load())
			require.True(t, first.closed.Load())
			require.Len(t, nativeHTTPFromContext(f.ctx).core.Snapshot().Attempts, 1)
			if tc.name == "input_usage" {
				require.NotNil(t, result)
				require.Equal(t, 7, result.Usage.InputTokens)
			}
			if tc.name == "cache_only_usage" {
				require.NotNil(t, result)
				require.Equal(t, 7, result.Usage.CacheReadInputTokens)
			}
			if tc.name == "image_output" {
				require.NotNil(t, result)
				require.Equal(t, 1, result.ImageCount)
			}
			if tc.name == "delivered_output" || tc.writeFailure {
				require.NotEmpty(t, writes)
				require.True(t, nativeHTTPFromContext(f.ctx).core.Committed())
			}
			if tc.name == "malformed_tail" {
				require.Error(t, err)
			}
		})
	}
}

func TestNativeHTTPBridgeSSERepairReviewRecoveryBoundaries(t *testing.T) {
	for _, kind := range []string{"cancel_after_close", "attempt_budget", "first_output_deadline", "authority_revoked", "capacity_released"} {
		t.Run(kind, func(t *testing.T) {
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			attempts := 3
			if kind == "attempt_budget" {
				attempts = 1
			}
			f := newBridgeSSEReviewFixture(t, parent, bridgeSSEReviewPayload, attempts, true)
			owner := nativeHTTPFromContext(f.ctx)
			budget, pricing := owner.dispatch.budget, owner.intent.request
			if kind == "first_output_deadline" {
				budget.firstOutput.Limit(time.Second, time.Now())
			}
			deadline := budget.firstOutput.Snapshot().Deadline
			first := &bridgeSSEReviewBody{Reader: strings.NewReader(bridgeSSEReviewWire(bridgeSSEReviewRejection(true, "")))}
			first.onClose = func() {
				switch kind {
				case "cancel_after_close":
					cancel()
				case "first_output_deadline":
					wait, end := context.WithDeadline(t.Context(), deadline.Add(time.Millisecond))
					defer end()
					<-wait.Done()
				case "authority_revoked":
					f.key.Status = StatusDisabled
				case "capacity_released":
					f.accountSlot.ReleaseFunc()
				}
			}
			f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: first}, nil
			}
			var writes [][]byte
			_, err := f.forward(func(b []byte) error { writes = append(writes, append([]byte(nil), b...)); return nil })
			require.Error(t, err)
			require.EqualValues(t, 1, f.transport.sends.Load())
			require.Zero(t, f.transport.legacy.Load())
			require.True(t, first.closed.Load())
			require.Empty(t, writes, "the repair proposal must not expose its rejection before the owner decides")
			require.Same(t, owner, nativeHTTPFromContext(f.ctx))
			require.Same(t, budget, owner.dispatch.budget)
			require.Same(t, pricing, owner.intent.request)
			require.Equal(t, deadline, budget.firstOutput.Snapshot().Deadline)
			require.Equal(t, 1, budget.attemptBudget.Snapshot().Attempts)
			require.Len(t, owner.core.Snapshot().Attempts, 1)
			switch kind {
			case "cancel_after_close":
				require.ErrorIs(t, err, context.Canceled)
			case "attempt_budget":
				require.ErrorIs(t, err, execution.ErrAttemptBudgetExhausted)
			case "first_output_deadline":
				require.True(t, errors.Is(err, ErrOpenAIWSFirstOutputBudgetExhausted) || errors.Is(err, execution.ErrDispatchExpired), "%v", err)
			case "authority_revoked", "capacity_released":
				require.True(t, IsNativeDispatchDenied(err), "%v", err)
			}
		})
	}
}

func TestNativeHTTPBridgeSSERepairReviewMixedStatusAndSSEDoNotLoop(t *testing.T) {
	f := newBridgeSSEReviewFixture(t, t.Context(), bridgeSSEReviewPayload, 3, true)
	owner := nativeHTTPFromContext(f.ctx)
	var previous *bridgeSSEReviewBody
	var sent [][]byte
	f.transport.reply = func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
		wire, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		sent = append(sent, wire)
		require.Same(t, owner, nativeHTTPFromContext(req.Context()))
		if previous != nil {
			require.True(t, previous.closed.Load())
		}
		status, body := http.StatusBadRequest, `{"error":`+bridgeSSEReviewProblem+`}`
		contentType := "application/json"
		if len(sent) > 1 {
			status, body, contentType = http.StatusOK, bridgeSSEReviewWire(bridgeSSEReviewRejection(true, "")), "text/event-stream"
		}
		previous = &bridgeSSEReviewBody{Reader: strings.NewReader(body)}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}}, Body: previous}, nil
	}
	var writes [][]byte
	_, _ = f.forward(func(b []byte) error { writes = append(writes, append([]byte(nil), b...)); return nil })
	require.EqualValues(t, 2, f.transport.sends.Load(), "the same unrepairable rejection must not gain another attempt")
	require.True(t, gjson.GetBytes(sent[0], "input.0.id").Exists())
	require.False(t, gjson.GetBytes(sent[1], "input.0.id").Exists())
	require.NotEmpty(t, writes)
	require.Equal(t, "response.failed", gjson.GetBytes(writes[len(writes)-1], "type").String())
	require.Len(t, owner.core.Snapshot().Attempts, 2)
	require.True(t, previous.closed.Load())
	require.Zero(t, f.transport.legacy.Load())
}
