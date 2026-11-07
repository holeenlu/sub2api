package handler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// These use the real handler, dispatcher and finalizer with the shared local
// peers/journal. They do not manually bind an intent or claim PG durability.
func nativeWSBoundIntentForPayload(t *testing.T, journal *wsNativeFixtureJournal, payload string) *service.NativeUsageIntentRecord {
	t.Helper()
	journal.mu.Lock()
	keys := make([]billing.NativeIntentKey, 0, len(journal.intents))
	for key := range journal.intents {
		keys = append(keys, key)
	}
	journal.mu.Unlock()
	var matched *service.NativeUsageIntentRecord
	for _, key := range keys {
		intent, err := journal.LoadNativeUsageIntent(t.Context(), key)
		require.NoError(t, err)
		if intent.Request.Billing.RequestPayloadHash == service.HashUsageRequestPayload([]byte(payload)) {
			require.Nil(t, matched, "an account retry must not create another intent root")
			matched = intent
		}
	}
	require.NotNil(t, matched, "the actual handler must bind the submitted payload")
	return matched
}

func TestNativeWSIntentBindingEachLogicalTurn(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeHTTPBridge} {
		t.Run(mode, func(t *testing.T) {
			const first = `{"type":"response.create","model":"gpt-5.6-sol","input":"first turn"}`
			const second = `{"type":"response.create","model":"gpt-5.6-sol","input":"second turn"}`
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				ingressMode: mode, firstPayload: first, secondPayload: second,
			})
			require.Len(t, got.logs, 2)
			require.Len(t, got.upstreamPayloads, 2)
			a := nativeWSBoundIntentForPayload(t, got.nativeJournal, first)
			b := nativeWSBoundIntentForPayload(t, got.nativeJournal, second)
			require.NotEqual(t, a.Key.OperationID, b.Key.OperationID)
			require.Equal(t, a.Key.APIKeyID, b.Key.APIKeyID)
			require.True(t, b.Request.PricingAt.After(a.Request.PricingAt))
			for _, intent := range []*service.NativeUsageIntentRecord{a, b} {
				require.False(t, intent.Request.PricingAt.IsZero())
				require.Equal(t, "gpt-5.6-sol", intent.Request.OriginalModel)
				require.Nil(t, intent.Request.Billing.KnownCanonical, "a WS connection ID is not the turn's canonical billing identity")
				require.Len(t, intent.Attempts, 1)
				attempt := intent.Attempts[0]
				require.Equal(t, intent.Key.OperationID, attempt.Prepared.OperationID)
				require.Equal(t, model.DispatchGeneration, attempt.Prepared.Purpose)
				transport := "ws"
				if mode == service.OpenAIWSIngressModeHTTPBridge {
					transport = "http"
				}
				require.Equal(t, transport, attempt.Prepared.Transport)
			}
			got.nativeJournal.assertNoLegacyApply(t)
		})
	}
}

func TestNativeWSIntentBindingNextTurnRefreshesPolicy(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			const first = `{"type":"response.create","model":"sol","input":"first turn"}`
			const second = `{"type":"response.create","model":"sol","input":"second turn"}`
			repo := newWSGroupPricingAPIKeyRepoStub(3)
			keys := newWSGroupPricingAPIKeyService(repo)
			var changedAt atomic.Pointer[time.Time]
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				ingressMode: mode, firstPayload: first, secondPayload: second,
				apiKeyService: keys, apiKeyCredential: wsGroupPricingKey,
				channelMapping: map[string]string{"sol": "gpt-5.6-sol"}, billingModelSource: service.BillingModelSourceChannelMapped,
				afterFirstUpstreamRequest: func(channels *service.ChannelService) error {
					if err := repo.adminUpdate(keys, func(_ *service.APIKey, group *service.Group) { group.RateMultiplier = 0.3 }); err != nil {
						return err
					}
					_, err := channels.Update(t.Context(), 7701, &service.UpdateChannelInput{ModelMapping: map[string]map[string]string{
						service.PlatformOpenAI: {"sol": "gpt-5.6-terra"},
					}})
					at := time.Now()
					changedAt.Store(&at)
					return err
				},
			})
			a := nativeWSBoundIntentForPayload(t, got.nativeJournal, first)
			b := nativeWSBoundIntentForPayload(t, got.nativeJournal, second)
			require.NotNil(t, changedAt.Load())
			require.True(t, a.Request.PricingAt.Before(*changedAt.Load()))
			require.True(t, b.Request.PricingAt.After(*changedAt.Load()))
			require.InDelta(t, 3, a.Request.BaseMultiplier, 1e-12)
			require.InDelta(t, 0.3, b.Request.BaseMultiplier, 1e-12)
			require.Equal(t, "gpt-5.6-sol", a.Attempts[0].ChannelMappedModel)
			require.Equal(t, "gpt-5.6-terra", b.Attempts[0].ChannelMappedModel)
			require.Len(t, got.logs, 2)
			require.InDelta(t, a.Request.BaseMultiplier, got.logs[0].RateMultiplier, 1e-12)
			require.InDelta(t, b.Request.BaseMultiplier, got.logs[1].RateMultiplier, 1e-12)
			require.Equal(t, "sol", got.logs[0].RequestedModel)
			require.Equal(t, "sol", got.logs[1].RequestedModel)
		})
	}
}

func TestNativeWSIntentBindingRetryKeepsOriginalPolicy(t *testing.T) {
	const payload = `{"type":"response.create","model":"gpt-5.1","input":"retry once","store":false}`
	repo := newWSGroupPricingAPIKeyRepoStub(3)
	keys := newWSGroupPricingAPIKeyService(repo)
	var accountAcquisitions atomic.Int32
	var changedAt atomic.Pointer[time.Time]
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		// Explicit capacity shed exercises the handler's account-slot reacquisition.
		// The separate safe-EOF case retains the held capacity while reconnecting,
		// under the existing Native replay predicates and operation budget.
		ingressMode: service.OpenAIWSIngressModeCtxPool, firstPayload: payload,
		firstAttemptErrorCode: "slow_down", firstAttemptErrorDelay: 100 * time.Millisecond,
		apiKeyService: keys, apiKeyCredential: wsGroupPricingKey,
		beforeCapacityAcquire: func(_ context.Context, user bool) {
			if !user && accountAcquisitions.Add(1) == 2 {
				require.NoError(t, repo.adminUpdate(keys, func(_ *service.APIKey, group *service.Group) { group.RateMultiplier = 0.3 }))
				at := time.Now()
				changedAt.Store(&at)
			}
		},
	})
	intent := nativeWSBoundIntentForPayload(t, got.nativeJournal, payload)
	require.Equal(t, 2, got.upstreamAttempts)
	require.NotNil(t, changedAt.Load(), "exercise a real second account acquisition")
	require.True(t, intent.Request.PricingAt.Before(*changedAt.Load()))
	require.InDelta(t, 3, intent.Request.BaseMultiplier, 1e-12)
	require.Len(t, intent.Attempts, 2)
	for _, attempt := range intent.Attempts {
		require.Equal(t, intent.Key.OperationID, attempt.Prepared.OperationID)
	}
	require.NotEqual(t, intent.Attempts[0].Sequence, intent.Attempts[1].Sequence)
	require.Len(t, got.logs, 1)
	require.InDelta(t, 3, got.logs[0].RateMultiplier, 1e-12)
}

func TestNativeWSIntentBindingAcceptedResponseSafeEOFRecoversOriginalOperation(t *testing.T) {
	const payload = `{"type":"response.create","model":"gpt-5.1","input":"safe accepted response then EOF","store":false}`
	repo := newWSGroupPricingAPIKeyRepoStub(3)
	keys := newWSGroupPricingAPIKeyService(repo)
	var accountAcquisitions atomic.Int32
	var changedAt atomic.Pointer[time.Time]
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		// Preserve the maintenance regression's exact created-then-EOF trigger.
		ingressMode: service.OpenAIWSIngressModeCtxPool, firstPayload: payload, failFirstBeforeContent: true,
		apiKeyService: keys, apiKeyCredential: wsGroupPricingKey,
		beforeCapacityAcquire: func(_ context.Context, user bool) {
			if !user && accountAcquisitions.Add(1) == 1 {
				require.NoError(t, repo.adminUpdate(keys, func(_ *service.APIKey, group *service.Group) { group.RateMultiplier = 0.3 }))
				at := time.Now()
				changedAt.Store(&at)
			}
		},
	})
	intent := nativeWSBoundIntentForPayload(t, got.nativeJournal, payload)
	require.Equal(t, 2, got.upstreamAttempts)
	require.EqualValues(t, 1, accountAcquisitions.Load(), "safe reconnect retains the original acquired account capacity")
	require.NotNil(t, changedAt.Load())
	require.True(t, intent.Request.PricingAt.Before(*changedAt.Load()))
	require.InDelta(t, 3, intent.Request.BaseMultiplier, 1e-12)
	require.Len(t, intent.Attempts, 2)
	for _, attempt := range intent.Attempts {
		require.Equal(t, intent.Key.OperationID, attempt.Prepared.OperationID)
	}
	require.NotEqual(t, intent.Attempts[0].Sequence, intent.Attempts[1].Sequence)
	require.Len(t, got.logs, 1)
	require.Equal(t, "resp_usage_e2e_1", got.logs[0].RequestID)
	require.InDelta(t, 3, got.logs[0].RateMultiplier, 1e-12)
	require.Len(t, got.clientEvents, 1)
	require.NotContains(t, string(got.clientEvents[0]), "resp_discarded_attempt")
	got.nativeJournal.mu.Lock()
	defer got.nativeJournal.mu.Unlock()
	final := got.nativeJournal.finalizations[intent.Key]
	require.NotNil(t, final)
	require.Len(t, final.Evidence.Observations, 2)
	for _, observed := range final.Evidence.Observations {
		require.Equal(t, model.SendSent, observed.SendState, "safe replay must not relabel the earlier send as NotSent")
		if observed.AttemptSequence == intent.Attempts[0].Sequence {
			require.Equal(t, "resp_discarded_attempt", observed.UpstreamResponseID)
			require.Equal(t, billing.NativeUsageUnknown, observed.Confidence)
			require.False(t, observed.TerminalConfirmed)
			require.Nil(t, observed.Usage)
		}
	}
	require.Equal(t, intent.Attempts[1].Sequence, final.Intent.AttemptSequence)
	require.Equal(t, billing.NativeUsageFinal, final.Evidence.Evidence.Confidence)
	require.Zero(t, got.nativeJournal.applyCalls)
}

func TestNativeWSIntentBindingAcceptedResponseUnsafeHostedToolEOFDoesNotReplay(t *testing.T) {
	// response.created alone does not forbid recovery. This declared hosted tool
	// is the explicit side-effect blocker from PROTOCOLS section 4.1.
	const payload = `{"type":"response.create","model":"gpt-5.1","input":"accepted then lost","store":false,"tools":[{"type":"code_interpreter","container":{"type":"auto"}}]}`
	zero := 0
	var accountAcquisitions atomic.Int32
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		ingressMode: service.OpenAIWSIngressModeCtxPool, firstPayload: payload, expectedUsageCount: &zero,
		beforeCapacityAcquire: func(_ context.Context, user bool) {
			if !user {
				accountAcquisitions.Add(1)
			}
		},
		upstreamScenario: func(ctx context.Context, conn *coderws.Conn, _ int, _ []byte) error {
			if err := conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.created","response":{"id":"resp_accepted_before_eof"}}`)); err != nil {
				return err
			}
			_ = conn.CloseNow()
			return nil
		},
		clientScenario: func(t *testing.T, conn *coderws.Conn) [][]byte {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			_, event, err := conn.Read(ctx)
			require.NoError(t, err)
			require.Equal(t, "error", gjson.GetBytes(event, "type").String())
			require.Equal(t, "upstream_recovery_exhausted", gjson.GetBytes(event, "error.code").String())
			return [][]byte{event}
		},
	})
	intent := nativeWSBoundIntentForPayload(t, got.nativeJournal, payload)
	require.Equal(t, 1, got.upstreamAttempts)
	require.Len(t, got.upstreamPayloads, 1)
	require.EqualValues(t, 1, accountAcquisitions.Load(), "unknown hosted-tool execution must not acquire capacity for a replay")
	require.Len(t, intent.Attempts, 1)
	require.Empty(t, got.logs, "missing terminal usage must not manufacture a bill")
	got.nativeJournal.mu.Lock()
	defer got.nativeJournal.mu.Unlock()
	final := got.nativeJournal.finalizations[intent.Key]
	require.NotNil(t, final)
	require.Equal(t, "resp_accepted_before_eof", final.Evidence.Evidence.UpstreamResponseID)
	require.Equal(t, billing.NativeUsageUnknown, final.Evidence.Evidence.Confidence)
	require.False(t, final.Evidence.Evidence.TerminalConfirmed)
	require.Nil(t, final.Evidence.Evidence.Usage)
	require.Zero(t, got.nativeJournal.applyCalls)
}

func TestNativeWSIntentBindingMissingCanonicalRemainsPending(t *testing.T) {
	const payload = `{"type":"response.create","model":"gpt-5.1","input":"metered error"}`
	zero := 0
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		ingressMode: service.OpenAIWSIngressModePassthrough, firstPayload: payload,
		meteredRateLimitUsage: `{"input_tokens":5,"output_tokens":1}`, expectedUsageCount: &zero,
	})
	intent := nativeWSBoundIntentForPayload(t, got.nativeJournal, payload)
	require.Len(t, got.upstreamPayloads, 1)
	require.Empty(t, got.logs)
	got.nativeJournal.mu.Lock()
	defer got.nativeJournal.mu.Unlock()
	require.Empty(t, got.nativeJournal.records, "no random canonical identity for Native")
	final := got.nativeJournal.finalizations[intent.Key]
	require.NotNil(t, final)
	require.Empty(t, final.Evidence.Evidence.Identity.RequestID)
	require.Equal(t, billing.NativeCanonicalIdentityUnavailable, final.Reason)
	require.Equal(t, billing.NativeUsagePartial, final.Evidence.Evidence.Confidence)
	require.NotNil(t, final.Evidence.Evidence.Usage)
	require.EqualValues(t, 5, *final.Evidence.Evidence.Usage.InputTokens)
	require.EqualValues(t, 1, *final.Evidence.Evidence.Usage.OutputTokens)
	require.Zero(t, got.nativeJournal.applyCalls)
}
