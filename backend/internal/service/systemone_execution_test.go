package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/access"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	provider "github.com/Wei-Shaw/sub2api/internal/providers/typesafe"
)

func TestExecuteSystemOneLocalhostSuccess(t *testing.T) {
	f := newSystemOneExecutionFixture(t)
	// Only loopback is contacted; the money/journal port is simulated.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, typesafe.SystemOnePath, r.URL.Path)
		assert.Equal(t, "Bearer ts-secret", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, []byte(systemOneExecutionBody), body)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("x-request-id", "req-jev")
		_, err = w.Write([]byte(systemOneExecutionResponse))
		assert.NoError(t, err)
	}))
	defer server.Close()
	f.resource.Credentials["base_url"] = server.URL
	f.transport.reply = func(r *http.Request, _ native.TransportOptions) (*http.Response, error) { return server.Client().Do(r) }
	f.transport.inspect = func(_ *http.Request, o native.TransportOptions) {
		require.Equal(t, native.ProfileDefault, o.Profile)
		require.Equal(t, f.resource.ID, o.AccountID)
		require.Equal(t, 1, f.journal.stores, "intent before send")
	}
	result, err := f.execute()
	require.NoError(t, err)
	require.Equal(t, model.OperationCompleted, result.State)
	require.Equal(t, model.ProtocolSystemOne, result.Protocol)
	require.Equal(t, f.in.OriginalWireDigest, result.OriginalWireDigest)
	require.Equal(t, []int64{f.resource.ID}, f.selected)
	require.Positive(t, f.repo.lists)
	require.Len(t, result.Attempts, 1)
	a := result.Attempts[0]
	require.True(t, a.Closed)
	require.Equal(t, model.SendSent, a.Send)
	require.Equal(t, model.TerminalCompleted, a.Terminal)
	require.Equal(t, "req-jev", a.RequestID)
	require.Equal(t, "jev-1.13.0", a.ReturnedModel)
	require.Equal(t, model.UsageFinal, a.Usage.Confidence)
	require.EqualValues(t, 123, a.Usage.Input.Value)
	require.EqualValues(t, 7, a.Usage.Output.Value)
	require.True(t, result.Delivery.BusinessCommitted)
	require.EqualValues(t, len(systemOneExecutionResponse), result.Delivery.BytesWritten)
	require.Equal(t, systemOneExecutionResponse, f.writer.Body.String())
	require.Equal(t, http.StatusOK, f.writer.Code)
	require.Equal(t, model.BillingSettled, result.Billing.Disposition)
	require.True(t, result.Billing.Durable)
	require.Len(t, f.journal.projections, 1)
	p := f.journal.projections[0]
	require.Equal(t, NativeIntentProtocolSystemOne, p.Scope.Request.Protocol)
	require.NotNil(t, p.Settlement)
	require.NotNil(t, p.SettlementUsage.Gateway)
	require.Nil(t, p.SettlementUsage.OpenAI)
	require.Equal(t, p.Identity.RequestID, p.Settlement.RequestID)
	require.NotEqual(t, f.in.OperationID, p.Identity.RequestID)
	require.Equal(t, typesafe.JevLatestModel, p.FinalModel)
	require.InDelta(t, 123*2e-6+7*4e-6, p.Settlement.UsageLog.ActualCost, 1e-12)
	require.EqualValues(t, 1, f.transport.sends.Load())
}
func TestExecuteSystemOneExplicitRejectionSwitchesAccount(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newSystemOneExecutionFixture(t)
			f.secondAccount()
			success := f.transport.reply
			f.transport.reply = func(r *http.Request, o native.TransportOptions) (*http.Response, error) {
				if o.AccountID == f.resource.ID {
					return &http.Response{StatusCode: status, Header: http.Header{"X-Request-Id": {"rejected-request"}}, Body: io.NopCloser(strings.NewReader(`{"error":"rejected"}`))}, nil
				}
				require.Equal(t, "Bearer second-secret", r.Header.Get("Authorization"))
				return success(r, o)
			}
			result, err := f.execute()
			require.NoError(t, err)
			require.Equal(t, model.OperationCompleted, result.State)
			require.Equal(t, []int64{7, 8}, f.selected)
			require.Len(t, result.Attempts, 2)
			rejected, accepted := result.Attempts[0], result.Attempts[1]
			require.Equal(t, model.GenerationRejected, rejected.Generation)
			require.NotEmpty(t, rejected.RejectionEvidenceID)
			require.Equal(t, model.SendSent, rejected.Send)
			require.Nil(t, rejected.Usage)
			require.False(t, rejected.Delivery.BusinessCommitted)
			require.Equal(t, model.TerminalCompleted, accepted.Terminal)
			require.True(t, accepted.Delivery.BusinessCommitted)
			require.Len(t, f.journal.projections, 2)
			require.Nil(t, f.journal.projections[0].Settlement)
			require.NotNil(t, f.journal.projections[1].Settlement)
			require.Equal(t, f.journal.projections[0].Intent.Key, f.journal.projections[1].Intent.Key)
			require.EqualValues(t, 1, rejected.Sequence)
			require.EqualValues(t, 2, accepted.Sequence)
			require.Equal(t, model.BillingSettled, result.Billing.Disposition)
			require.Equal(t, 1, f.delivered)
			require.EqualValues(t, 2, f.transport.sends.Load())
			if status == 429 {
				require.Equal(t, []int64{7}, f.repo.rateLimited)
			} else {
				require.Equal(t, []int64{7}, f.repo.disabled)
			}
		})
	}
}
func TestExecuteSystemOneBeforeSendRevocationAndCancel(t *testing.T) {
	for _, kind := range []string{"revoke_after_prepare", "revoke_after_intent", "cancel_after_intent"} {
		t.Run(kind, func(t *testing.T) {
			f := newSystemOneExecutionFixture(t)
			if kind == "revoke_after_prepare" {
				f.ports.Selected = func(a *Account) { f.selected = append(f.selected, a.ID); f.key.Status = StatusDisabled }
			} else {
				f.journal.afterStore = func() {
					if kind == "cancel_after_intent" {
						f.cancel()
					} else {
						f.key.Status = StatusDisabled
					}
				}
			}
			result, err := f.execute()
			require.Error(t, err)
			if kind == "cancel_after_intent" {
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, model.OperationCanceled, result.State)
			} else {
				require.ErrorIs(t, err, access.ErrKeyDisabled)
				require.Equal(t, model.OperationFailed, result.State)
			}
			require.Zero(t, f.transport.sends.Load())
			require.Zero(t, f.delivered)
			require.Empty(t, f.journal.projections)
			require.Empty(t, f.repo.disabled)
			require.Empty(t, f.repo.rateLimited)
			require.Empty(t, f.repo.overloaded)
			if kind == "revoke_after_prepare" {
				require.Empty(t, result.Attempts)
				require.Zero(t, f.journal.stores)
			} else {
				require.Len(t, result.Attempts, 1)
				require.Equal(t, model.SendNotSent, result.Attempts[0].Send)
				require.Equal(t, 1, f.journal.closures)
				require.Equal(t, model.BillingNotRequired, result.Billing.Disposition)
				require.True(t, result.Billing.Durable)
			}
		})
	}
}
func TestExecuteSystemOneCancelAfterSendPreservesUnknown(t *testing.T) {
	f := newSystemOneExecutionFixture(t)
	f.secondAccount()
	f.transport.reply = func(r *http.Request, _ native.TransportOptions) (*http.Response, error) {
		f.cancel()
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	result, err := f.execute()
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, model.OperationCanceled, result.State)
	require.Len(t, result.Attempts, 1)
	require.NotEqual(t, model.SendNotSent, result.Attempts[0].Send)
	require.True(t, result.Attempts[0].Closed)
	require.EqualValues(t, 1, f.transport.sends.Load())
	require.Zero(t, f.delivered)
	require.Len(t, f.journal.projections, 1)
	p := f.journal.projections[0]
	require.Nil(t, p.InputTokens)
	require.Nil(t, p.OutputTokens)
	require.Nil(t, p.Settlement)
	require.Equal(t, []error{nil}, f.journal.contextErrors, "cleanup outlives caller cancellation")
	require.Equal(t, model.BillingPendingUsage, result.Billing.Disposition)
	require.True(t, result.Billing.Durable)
	require.Empty(t, f.repo.disabled)
	require.Empty(t, f.repo.rateLimited)
	require.Empty(t, f.repo.overloaded)
}
func TestExecuteSystemOneDeliveryFailureDoesNotGenerateAgain(t *testing.T) {
	f := newSystemOneExecutionFixture(t)
	f.secondAccount()
	writeErr := errors.New("client write interrupted")
	f.ports.Deliver = func(_ context.Context, r *provider.Response) (model.DeliveryReceipt, error) {
		f.delivered++
		n, err := f.writer.Write(r.Body[:8])
		require.NoError(t, err)
		return model.DeliveryReceipt{BytesWritten: int64(n), BusinessCommitted: true, Closed: true}, writeErr
	}
	result, err := f.execute()
	require.ErrorIs(t, err, writeErr)
	require.Equal(t, model.OperationFailed, result.State)
	require.Len(t, result.Attempts, 1)
	require.EqualValues(t, 1, f.transport.sends.Load())
	require.True(t, result.Delivery.BusinessCommitted)
	require.Equal(t, model.BillingSettled, result.Billing.Disposition)
	require.NotNil(t, f.journal.projections[0].Settlement)
}
