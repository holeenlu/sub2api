package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type nativeOperationBillingFunc func(context.Context, NativeOperationFinalization) (NativeIntentFinalizationResult, error)

func (f nativeOperationBillingFunc) RecordNativeOperationUsage(ctx context.Context, in NativeOperationFinalization) (NativeIntentFinalizationResult, error) {
	return f(ctx, in)
}

type nativeFinalizationBalance struct{ releases atomic.Int32 }

func TestNativeWSFinalizationSeparatesLanesWithSharedConnectionIDs(t *testing.T) {
	for _, withResult := range []bool{false, true} {
		t.Run(map[bool]string{false: "no_result", true: "observed_result"}[withResult], func(t *testing.T) {
			parent := context.WithValue(t.Context(), ctxkey.ClientRequestID, "same-client-connection")
			parent = context.WithValue(parent, ctxkey.RequestID, "same-local-connection")
			identities := make(map[billing.NativeIntentKey]bool)
			for _, lane := range []string{"lane-a", "lane-b"} {
				f, conn := nativeWSLifecycleFixture(t, parent, lane)
				require.NoError(t, f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText, []byte(`{"type":"response.create","model":"mapped-provider-model"}`)))
				conn.attempt.close(nil)
				input := &OpenAIRecordUsageInput{APIKey: f.key, User: f.key.User, Account: f.resource}
				expected := "" // An unobserved upstream ID is not replaced by the operation ID.
				if withResult {
					expected = "openai-ws-usage-" + lane
					input.Result = &OpenAIForwardResult{RequestID: expected, OpenAIWSMode: true}
				}
				calls := 0
				port := nativeOperationBillingFunc(func(_ context.Context, in NativeOperationFinalization) (NativeIntentFinalizationResult, error) {
					calls++
					require.Equal(t, expected, in.Identity.RequestID)
					require.Equal(t, lane, in.Intent.Key.OperationID)
					require.False(t, identities[in.Intent.Key], "independent technical intents must not deduplicate under the connection ID")
					identities[in.Intent.Key] = true
					require.Equal(t, input.Result, in.UsageInput.Result)
					return nativeFixtureTechnicalReceipt(in), nil
				})
				for range 2 {
					handled, err := finalizeNativeWSUsage(f.ctx, input, nil, port, time.Second)
					require.True(t, handled)
					require.NoError(t, err)
				}
				require.Equal(t, 1, calls, "a repeated callback must keep the same acknowledged root")
				require.NoError(t, FinishNativeWSOperation(f.ctx, nil))
			}
			require.Len(t, identities, 2)
		})
	}
}

func (b *nativeFinalizationBalance) ReleaseInflightBalance(context.Context, int64, string) error {
	b.releases.Add(1)
	return nil
}

type nativeCyclePrepareCounter struct {
	*nativeOperationTransport
	prepares atomic.Int32
}

func (t *nativeCyclePrepareCounter) PrepareNativeRequest(request *http.Request, limit int64) (*http.Request, error) {
	t.prepares.Add(1)
	return t.nativeOperationTransport.PrepareNativeRequest(request, limit)
}

func TestNativeHTTPCycleCannotRetryAfterUnconfirmedFinalization(t *testing.T) {
	for _, state := range []model.SendState{model.SendSent, model.SendAmbiguous} {
		for _, noReceipt := range []bool{false, true} {
			name := map[model.SendState]string{model.SendSent: "sent", model.SendAmbiguous: "ambiguous"}[state]
			if noReceipt {
				name += "/no_receipt"
			} else {
				name += "/persistence_error"
			}
			t.Run(name, func(t *testing.T) {
				f, owner, c := nativeHTTPObservedFixture(t, "", "application/json")
				transport := &nativeCyclePrepareCounter{nativeOperationTransport: f.transport}
				f.op.transport = transport
				f.transport.reply = func(request *http.Request, _ native.TransportOptions) (*http.Response, error) {
					trace := httptrace.ContextClientTrace(request.Context())
					trace.GetConn(request.URL.Host)
					trace.GotConn(httptrace.GotConnInfo{})
					if state == model.SendSent {
						trace.WroteHeaders()
						trace.WroteRequest(httptrace.WroteRequestInfo{})
					}
					return nil, io.ErrUnexpectedEOF
				}
				persistErr := errors.New("journal persistence failed")
				port := nativeOperationBillingFunc(func(context.Context, NativeOperationFinalization) (NativeIntentFinalizationResult, error) {
					if noReceipt {
						return NativeIntentFinalizationResult{}, nil
					}
					return NativeIntentFinalizationResult{}, persistErr
				})
				cycles := 0
				err := RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
					cycles++
					_, sendErr := TrackNativeHTTPForward(f.ctx, func() (*OpenAIForwardResult, error) {
						_, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
						return nil, err
					})
					require.ErrorIs(t, sendErr, io.ErrUnexpectedEOF)
					handled, finalErr := finalizeNativeHTTPUsage(f.ctx, nil, port, time.Second)
					require.True(t, handled)
					require.Error(t, finalErr)
					return NativeOperationContinue()
				})
				require.ErrorIs(t, err, ErrNativeOperationBillingReceipt)
				if !noReceipt {
					require.ErrorIs(t, err, persistErr)
				}
				require.Equal(t, 1, cycles)
				require.EqualValues(t, 1, transport.prepares.Load(), "a failed finalizer must stop before another prepare")
				require.EqualValues(t, 1, f.transport.sends.Load(), "a failed finalizer must stop before another send")
				snapshot := owner.core.Snapshot()
				require.Len(t, snapshot.Attempts, 1)
				require.Equal(t, state, snapshot.Attempts[0].Send)
				require.Equal(t, model.OperationFailed, snapshot.State)
				require.ErrorIs(t, snapshot.Failure, ErrNativeOperationBillingReceipt)
				require.Equal(t, model.BillingPendingUsage, snapshot.Billing.Disposition)
				require.False(t, snapshot.Billing.Durable)
			})
		}
	}
}

func TestNativeHTTPCycleDelegatedRecoveryKeepsItsExistingContract(t *testing.T) {
	f, owner, c := nativeHTTPObservedFixture(t, "", "application/json")
	cycles := 0
	err := RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
		cycles++
		_, err := TrackNativeHTTPForward(f.ctx, func() (*OpenAIForwardResult, error) {
			return &OpenAIForwardResult{CloudManagedBilling: true}, nil
		})
		require.NoError(t, err)
		handled, err := finalizeNativeHTTPUsage(f.ctx, nil, nil, time.Second)
		require.False(t, handled)
		require.NoError(t, err)
		if cycles == 1 {
			return NativeOperationContinue()
		}
		return NativeOperationDone()
	})
	require.NoError(t, err)
	require.Equal(t, 2, cycles)
	require.Zero(t, f.transport.sends.Load())
	require.Empty(t, owner.core.Snapshot().Attempts)
	require.Equal(t, 2, owner.core.Snapshot().DelegatedCycles)
}

func TestNativeHTTPFinalizationUsesBoundedContextAndActualEvidenceBeforeRelease(t *testing.T) {
	f, owner, _ := nativeHTTPObservedFixture(t, "", "application/json")
	response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	cache := &nativeFinalizationBalance{}
	hold := newInflightReservation(cache, f.key.UserID, "existing-hold", 3, time.Minute)
	ctx := WithInflightReservation(context.WithValue(f.ctx, ctxkey.RequestID, "native-finalize"), hold)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	accountReleased := false
	lease := RetainNativeHTTPRelease(func() { accountReleased = true })
	calls := 0
	port := nativeOperationBillingFunc(func(cleanup context.Context, in NativeOperationFinalization) (NativeIntentFinalizationResult, error) {
		calls++
		require.NoError(t, cleanup.Err(), "client cancellation must not discard existing send evidence")
		deadline, ok := cleanup.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), time.Second)
		require.False(t, accountReleased)
		require.Zero(t, cache.releases.Load())
		require.Equal(t, "local:fixture-admitted-http", in.Identity.RequestID, "cleanup cannot replace the identity frozen before dispatch")
		require.Equal(t, f.key.ID, in.Identity.APIKeyID)
		require.Equal(t, owner.core.Snapshot().OperationID, in.Operation.OperationID)
		require.Len(t, in.Operation.Attempts, 1)
		require.Equal(t, []uint64{in.Operation.Attempts[0].Sequence}, in.CurrentSequences)
		require.Equal(t, model.SendSent, in.Operation.Attempts[0].Send)
		require.Nil(t, in.Operation.Attempts[0].Usage, "no terminal/usage was observed")
		require.Nil(t, in.UsageInput.Result, "must not manufacture a zero-token forward result")
		return nativeFixtureTechnicalReceipt(in), nil
	})
	input := &OpenAIRecordUsageInput{APIKey: f.key, User: f.key.User, Account: f.resource}
	handled, err := finalizeNativeHTTPUsage(ctx, input, port, time.Second)
	require.True(t, handled)
	require.NoError(t, err)
	lease.Release()
	require.True(t, accountReleased)
	hold.HandlerDone()
	require.EqualValues(t, 1, cache.releases.Load())
	require.True(t, owner.core.Snapshot().Billing.Durable)
	require.Equal(t, model.BillingPendingUsage, owner.core.Snapshot().Billing.Disposition)
	_, err = finalizeNativeHTTPUsage(ctx, input, port, time.Second)
	require.NoError(t, err)
	require.Equal(t, 1, calls, "the same cycle must not issue another billing command")
}

func TestNativeHTTPFinalizationNeverConfirmsMissingWrongOrFailedReceipts(t *testing.T) {
	for _, name := range []string{"unavailable", "zero_receipt", "foreign_identity", "deadline"} {
		t.Run(name, func(t *testing.T) {
			f, owner, _ := nativeHTTPObservedFixture(t, "", "application/json")
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			cache := &nativeFinalizationBalance{}
			hold := newInflightReservation(cache, f.key.UserID, "existing-hold", 3, time.Minute)
			t.Cleanup(hold.Release)
			ctx := WithInflightReservation(f.ctx, hold)
			var port NativeOperationBilling
			if name != "unavailable" {
				port = nativeOperationBillingFunc(func(cleanup context.Context, in NativeOperationFinalization) (NativeIntentFinalizationResult, error) {
					switch name {
					case "foreign_identity":
						id := in.Identity
						id.APIKeyID++
						result := nativeFixtureTechnicalReceipt(in)
						result.Intent.Disposition = billing.NativeIntentBound
						result.Canonical = &NativeUsageRecordResult{Receipt: billing.NativeUsageReceipt{Identity: id, EvidenceFingerprint: "foreign-ack", Disposition: billing.NativeSettled}}
						return result, nil
					case "deadline":
						<-cleanup.Done()
						return NativeIntentFinalizationResult{}, context.Cause(cleanup)
					default:
						return NativeIntentFinalizationResult{}, errors.New("journal write unconfirmed")
					}
				})
			}
			handled, err := finalizeNativeHTTPUsage(ctx, nil, port, 5*time.Millisecond)
			require.True(t, handled)
			require.Error(t, err)
			if name == "deadline" {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
			hold.HandlerDone()
			require.Zero(t, cache.releases.Load(), "unconfirmed evidence keeps the existing reservation until its TTL")
			require.False(t, owner.core.Snapshot().Billing.Durable)
			require.Equal(t, model.BillingPendingUsage, owner.core.Snapshot().Billing.Disposition)
		})
	}
}

func TestNativeHTTPFinalizationPreservesDelegatedAccounting(t *testing.T) {
	f, owner, _ := nativeHTTPObservedFixture(t, "", "application/json")
	owner.delegated = true
	handled, err := finalizeNativeHTTPUsage(f.ctx, nil, nil, time.Second)
	require.False(t, handled)
	require.NoError(t, err)
	require.False(t, owner.core.Snapshot().Billing.Durable)
	require.Zero(t, f.transport.sends.Load())
}

func TestNativeHTTPAccountingOwnershipUsesActualModes(t *testing.T) {
	for _, sample := range []struct {
		name, mode string
		result     *OpenAIForwardResult
		owned      bool
	}{
		{"native", UsageAuthorizationModeNative, &OpenAIForwardResult{AuthorizationMode: UsageAuthorizationModeNative}, true},
		{"native_unknown", UsageAuthorizationModeNative, nil, true},
		{"bps_result", UsageAuthorizationModeBPS, &OpenAIForwardResult{AuthorizationMode: UsageAuthorizationModeBPS}, false},
		{"bps_failure_without_result", UsageAuthorizationModeBPS, nil, false},
		{"cloud_failure_without_result", UsageAuthorizationModeCloud, nil, false},
		{"native_ws_result", UsageAuthorizationModeNative, &OpenAIForwardResult{OpenAIWSMode: true}, true},
		{"native_ws_bps_result", UsageAuthorizationModeNative, &OpenAIForwardResult{OpenAIWSMode: true, AuthorizationMode: UsageAuthorizationModeBPS}, false},
		{"native_ws_cloud_result", UsageAuthorizationModeNative, &OpenAIForwardResult{OpenAIWSMode: true, AuthorizationMode: UsageAuthorizationModeCloud}, false},
		{"native_ws_cloud_managed", UsageAuthorizationModeNative, &OpenAIForwardResult{OpenAIWSMode: true, CloudManagedBilling: true}, false},
		{"prior_native_ws_usage", UsageAuthorizationModeNative, &OpenAIForwardResult{AuthorizationMode: UsageAuthorizationModeNative, PriorAuthorizationResults: []*OpenAIForwardResult{{OpenAIWSMode: true}}}, true},
		{"prior_bps_usage", UsageAuthorizationModeNative, &OpenAIForwardResult{AuthorizationMode: UsageAuthorizationModeNative, PriorAuthorizationResults: []*OpenAIForwardResult{{AuthorizationMode: UsageAuthorizationModeBPS}}}, false},
		{"native_ws_prior_bps_usage", UsageAuthorizationModeNative, &OpenAIForwardResult{OpenAIWSMode: true, PriorAuthorizationResults: []*OpenAIForwardResult{{AuthorizationMode: UsageAuthorizationModeBPS}}}, false},
		{"native_ws_prior_cloud_usage", UsageAuthorizationModeNative, &OpenAIForwardResult{OpenAIWSMode: true, PriorAuthorizationResults: []*OpenAIForwardResult{{AuthorizationMode: UsageAuthorizationModeCloud}}}, false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			f, _, _ := nativeHTTPObservedFixture(t, "", "application/json")
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			_, _ = TrackNativeHTTPForward(f.ctx, func() (*OpenAIForwardResult, error) { return sample.result, nil })
			TrackNativeHTTPAuthorization(f.ctx, sample.mode)
			require.Equal(t, sample.owned, NativeHTTPFinalizesUsage(f.ctx))
		})
	}
}

func TestNativeHTTPWSResultWithoutOwnedAttemptCannotTakeAccounting(t *testing.T) {
	f, owner, _ := nativeHTTPObservedFixture(t, "", "application/json")
	_, err := TrackNativeHTTPForward(f.ctx, func() (*OpenAIForwardResult, error) {
		return &OpenAIForwardResult{OpenAIWSMode: true, AuthorizationMode: UsageAuthorizationModeNative}, nil
	})
	require.NoError(t, err)
	require.False(t, NativeHTTPFinalizesUsage(f.ctx), "transport flag cannot manufacture a prepared attempt")
	handled, err := finalizeNativeHTTPUsage(f.ctx, nil, nil, time.Second)
	require.False(t, handled)
	require.NoError(t, err)
	require.Empty(t, owner.core.Snapshot().Attempts)
	require.False(t, owner.core.Snapshot().Billing.Durable)
}
