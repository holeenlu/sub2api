package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Observe the real provider's sealed write after Begin revalidates its permit.
// Only the peer and journal storage are local substitutes; no PG commit or
// upstream acceptance is claimed by these send-boundary assertions.
type nativeWSPricingConn struct {
	*nativeWSRecordedConn
	beforeFrame func(context.Context, []byte)
}

func (c *nativeWSPricingConn) WriteWS(ctx context.Context, write *native.WSWrite) (model.SendState, error) {
	if nativeDispatchFromContext(ctx) != c.operation {
		return model.SendNotSent, execution.ErrDispatchChanged
	}
	wire, err := write.Begin(ctx)
	if err != nil {
		return model.SendNotSent, err
	}
	if c.beforeFrame != nil {
		c.beforeFrame(ctx, wire)
	}
	c.frames++
	c.bytes += len(wire)
	return model.SendSent, nil
}

func TestNativeWSPricingFinalWirePreservedBeforeSend(t *testing.T) {
	for _, tc := range []struct {
		name, wire, tier, effort string
		billingModel, imageModel string
	}{
		{
			name: "nested_effort_wins",
			wire: `{"type":"response.create","model":"gpt-5.1","service_tier":"priority","reasoning":{"effort":"high"},"reasoning_effort":"low","input":"hello"}`,
			tier: "priority", effort: "high", billingModel: "gpt-4o", imageModel: "gpt-image-1",
		},
		{
			name: "legacy_effort",
			wire: `{"type":"response.create","model":"gpt-5.1","service_tier":"flex","reasoning_effort":"medium","input":"hello"}`,
			tier: "flex", effort: "medium", billingModel: "gpt-4o", imageModel: "gpt-image-1",
		},
		{
			name:         "removed_wire_fields_clear_stale_facts",
			wire:         `{"type":"response.create","model":"gpt-5.1","input":"hello"}`,
			billingModel: "gpt-4o", imageModel: "gpt-image-1",
		},
		{
			name: "no_producer_billing_or_image_override",
			wire: `{"type":"response.create","model":"gpt-5.1","input":"hello"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, recorded := nativeWSLifecycleFixture(t, t.Context())
			journal := f.service.usageBillingRepo.(*nativeApplicationJournal)
			producer := NativeIntentModelFacts{
				Model: "producer-request-model", UpstreamModel: "stale-wire-model",
				BillingModel: tc.billingModel, ImageBillingModel: tc.imageModel,
				ServiceTier: "stale-tier", ReasoningEffort: "stale-effort",
			}
			f.ctx = WithNativeIntentModelFacts(f.ctx, producer)
			owner := nativeHTTPFromContext(f.ctx)
			budget := f.op.budget
			deadline := budget.firstOutput.Snapshot().Deadline
			originalDigest := owner.core.Snapshot().OriginalWireDigest
			payload := []byte(`{"stream_id":"lane-a",` + tc.wire[1:])
			originalPayload := append([]byte(nil), payload...)
			conn := &nativeWSPricingConn{nativeWSRecordedConn: recorded}
			preserveCalls, writeCalls := 0, 0
			journal.beforeStore = func(ctx context.Context, intent *NativeUsageIntent) {
				preserveCalls++
				require.Zero(t, conn.frames, "preserve must precede the actual frame")
				require.Zero(t, journal.stores)
				require.Same(t, owner, nativeHTTPFromContext(ctx))
				require.Equal(t, originalDigest, intent.OriginalWireDigest)
				require.Equal(t, owner.intent.request.Scope.PricingAt, intent.Request.PricingAt)
				require.Equal(t, sha256.Sum256([]byte(tc.wire)), intent.Attempt.Prepared.BodyDigest)
				require.Equal(t, model.DispatchGeneration, intent.Attempt.Prepared.Purpose)
				require.Equal(t, f.op.request.OperationID, intent.Key.OperationID)
				require.Equal(t, f.key.ID, intent.Key.APIKeyID)
				require.Equal(t, tc.tier, intent.Attempt.ServiceTier)
				require.Equal(t, tc.effort, intent.Attempt.ReasoningEffort)
				require.Contains(t, intent.Attempt.CandidateModels, producer.Model)
				require.Contains(t, intent.Attempt.CandidateModels, "gpt-5.1")
				require.NotContains(t, intent.Attempt.CandidateModels, producer.UpstreamModel)
				baseline := tc.billingModel
				if baseline == "" {
					// forwardResultBillingModel prefers the nonempty producer Model;
					// the actual upstream slug remains a separate fallback candidate.
					baseline = producer.Model
				}
				require.Equal(t, baseline, intent.Attempt.BaselineModel)
				require.Equal(t, tc.imageModel, intent.Attempt.ImageBillingModel)
				require.NotContains(t, intent.Attempt.CandidateModels, "gpt-image-1", "an image candidate is not a text billing override")
			}
			conn.beforeFrame = func(ctx context.Context, wire []byte) {
				writeCalls++
				require.Equal(t, 1, preserveCalls)
				require.Equal(t, 1, journal.stores, "a successful preserve ACK must precede the frame")
				require.Same(t, owner, nativeHTTPFromContext(ctx))
				require.Same(t, budget, nativeDispatchFromContext(ctx).budget)
				require.Equal(t, deadline, budget.firstOutput.Snapshot().Deadline)
				require.Equal(t, tc.wire, string(wire))
				require.False(t, gjson.GetBytes(wire, "stream_id").Exists())
				stored, err := journal.LoadNativeUsageIntent(ctx, billing.NativeIntentKey{OperationID: f.op.request.OperationID, APIKeyID: f.key.ID})
				require.NoError(t, err)
				require.Len(t, stored.Attempts, 1)
				require.Equal(t, sha256.Sum256(wire), stored.Attempts[0].Prepared.BodyDigest)
				require.Equal(t, conn.attempt.sequence, stored.Attempts[0].Sequence)
			}
			require.NoError(t, f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText, payload))
			require.Equal(t, 1, preserveCalls)
			require.Equal(t, 1, writeCalls)
			require.Equal(t, 1, conn.frames)
			require.Equal(t, originalPayload, payload, "removing the local envelope must not mutate the caller's payload")
			require.Equal(t, producer, f.ctx.Value(nativeIntentModelFactsKey{}), "final wire facts must not mutate the producer projection")
			require.Equal(t, 1, budget.attemptBudget.Snapshot().Attempts)
			require.Zero(t, budget.attemptBudget.Snapshot().PreparationRetries)
			require.Zero(t, journal.records, "preserving an intent must not invent final usage")
			require.Zero(t, journal.old)
			conn.attempt.close(nil)
		})
	}
}

func TestNativeWSPricingMissingProducerFactsNeverSend(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts NativeIntentModelFacts
	}{
		{name: "no_producer_facts"},
		{name: "billing_decisions_without_request_model", facts: NativeIntentModelFacts{BillingModel: "gpt-4o", ImageBillingModel: "gpt-image-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, conn := nativeWSLifecycleFixture(t, t.Context())
			journal := f.service.usageBillingRepo.(*nativeApplicationJournal)
			// Mask the fixture's producer projection without removing the real root,
			// request pricing, authority or leases. Neither root nor wire is a substitute.
			ctx := WithNativeIntentModelFacts(f.ctx, tc.facts)
			require.NotEmpty(t, f.op.request.RequestedModel)
			err := f.service.sendNativeWS(ctx, f.resource, conn, coderws.MessageText,
				[]byte(`{"type":"response.create","model":"gpt-5.1","service_tier":"priority","reasoning":{"effort":"high"}}`))
			require.ErrorIs(t, err, billing.ErrNativeIntentInvalid)
			var attemptErr *OpenAITransportAttemptError
			require.ErrorAs(t, err, &attemptErr)
			require.Equal(t, model.SendNotSent, attemptErr.SendState)
			require.Zero(t, conn.frames)
			require.Zero(t, journal.stores)
			require.Empty(t, journal.intents)
			require.Zero(t, journal.records)
			require.Zero(t, journal.old)
			snapshot := nativeHTTPFromContext(ctx).core.Snapshot()
			require.Len(t, snapshot.Attempts, 1)
			require.Equal(t, model.SendNotSent, snapshot.Attempts[0].Send)
			require.True(t, snapshot.Attempts[0].Closed)
		})
	}
}

func TestNativeWSPricingJournalFailureNeverSends(t *testing.T) {
	f, conn := nativeWSLifecycleFixture(t, t.Context())
	journal := f.service.usageBillingRepo.(*nativeApplicationJournal)
	failed := errors.New("native WS pricing intent storage unavailable")
	journal.storeErr = failed
	err := f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText,
		[]byte(`{"type":"response.create","model":"mapped-provider-model"}`))
	require.ErrorIs(t, err, failed)
	var attemptErr *OpenAITransportAttemptError
	require.ErrorAs(t, err, &attemptErr)
	require.Equal(t, model.SendNotSent, attemptErr.SendState)
	require.Zero(t, conn.frames)
	require.Zero(t, journal.stores)
	require.Empty(t, journal.intents)
	require.Zero(t, journal.records)
	require.Zero(t, journal.old)
}

func TestNativeWSPricingInternalPreparationDoesNotCreateIntent(t *testing.T) {
	for _, bound := range []bool{true, false} {
		name := "bound_intent"
		if !bound {
			name = "no_intent"
		}
		t.Run(name, func(t *testing.T) {
			f, conn := nativeWSLifecycleFixture(t, t.Context())
			journal := f.service.usageBillingRepo.(*nativeApplicationJournal)
			owner := nativeHTTPFromContext(f.ctx)
			if !bound {
				owner.intent = nil
			}
			ctx := WithNativeIntentModelFacts(f.ctx, NativeIntentModelFacts{})
			journal.beforeStore = func(context.Context, *NativeUsageIntent) {
				t.Fatal("internal preparation must not synthesize a generation pricing intent")
			}
			payload := []byte(`{"type":"response.create","stream_id":"warmup-lane","model":"mapped-provider-model","generate":false}`)
			require.NoError(t, f.service.sendNativeWSPreparation(ctx, f.resource, conn, payload))
			require.Equal(t, 1, conn.frames)
			conn.attempt.close(nil)
			snapshot := owner.core.Snapshot()
			require.Len(t, snapshot.Attempts, 1)
			require.Equal(t, model.DispatchInternalPreparation, snapshot.Attempts[0].Identity.Purpose)
			require.Equal(t, model.SendSent, snapshot.Attempts[0].Send)
			require.Nil(t, snapshot.Attempts[0].Usage)
			require.Zero(t, f.op.budget.attemptBudget.Snapshot().Attempts)
			require.Equal(t, 1, f.op.budget.attemptBudget.Snapshot().PreparationRetries)
			// The identical client generate:false frame must still pass generation
			// pricing checks; the internal-preparation exception cannot leak to it.
			err := f.service.sendNativeWS(ctx, f.resource, conn, coderws.MessageText, payload)
			if bound {
				require.ErrorIs(t, err, billing.ErrNativeIntentInvalid)
			} else {
				require.ErrorIs(t, err, billing.ErrNativeIntentUnavailable)
			}
			require.Equal(t, 1, conn.frames)
			require.Equal(t, 1, f.op.budget.attemptBudget.Snapshot().Attempts)
			require.Equal(t, 1, f.op.budget.attemptBudget.Snapshot().PreparationRetries)
			require.Zero(t, journal.stores)
			require.Empty(t, journal.intents)
			require.Zero(t, journal.records)
			require.Nil(t, journal.input)
			require.Zero(t, journal.old)
		})
	}
}
