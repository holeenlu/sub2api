//go:build unit

package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloudLateUsageBoundsStopProducerAndKeepOriginal(t *testing.T) {
	for _, budget := range []string{"owner_bytes", "hosted_projection", "escaped_projection"} {
		t.Run(budget, func(t *testing.T) {
			a, store, _ := cloudHostedActorForTest(t)
			a.record.Status, a.record.Usage = "unknown", nil
			a.uncertain = true
			a.record.RootRequestID = "original-unknown-root"
			require.NoError(t, a.save(nil, a.settlement("unknown", true)))
			original := a.terminalSettlement
			switch budget {
			case "owner_bytes":
				a.retainedBytes = OpenAICloudMaxRetainedBytes - cloudLateUsageMarkerReserveBytes
			case "hosted_projection":
				a.hostedUsage.ImageOutputSizes = []string{strings.Repeat("x", OpenAICloudMaxEnvelopeBytes)}
			case "escaped_projection":
				a.hostedUsage.ImageOutputSizes = []string{strings.Repeat("<", OpenAICloudMaxEnvelopeBytes/6)}
			}
			producerStopped := false
			a.rpcCancel = func() { producerStopped = true }
			err := cloudHostedEventForTest(t, a, "thread/tokenUsage/updated", map[string]any{"tokenUsage": map[string]any{"total": map[string]any{
				"inputTokens": 9007199254740000, "outputTokens": 90, "cachedInputTokens": 12, "cacheWriteInputTokens": 7,
				"totalTokens": 9007199254740090, "reasoningOutputTokens": 30,
			}}}, "")
			require.ErrorIs(t, err, errCloudLateUsageLimit)
			require.ErrorIs(t, err, ErrOpenAICloudFactsUnavailable)
			require.True(t, producerStopped)
			select {
			case <-a.rpc.Done():
			default:
				t.Fatal("the transport producer was not stopped")
			}
			require.Len(t, a.record.LateUsage, 1)
			require.Empty(t, a.pendingLateUsage)
			marker := a.record.LateUsage[0]
			require.True(t, marker.ObservationStopped)
			require.Equal(t, &OpenAICloudUsage{InputTokens: 9007199254740000, OutputTokens: 90, CachedInputTokens: 12,
				CacheWriteInputTokens: 7, TotalTokens: 9007199254740090, ReasoningOutputTokens: 30}, marker.Total,
				"preserve every observed counter exactly, including large integers")
			require.Nil(t, marker.Usage, "an omitted projection is unknown, never zero")
			raw, err := json.Marshal(a.record.LateUsage)
			require.NoError(t, err)
			require.Less(t, len(raw), cloudLateUsageMarkerReserveBytes)
			require.Equal(t, "original-unknown-root", original.RootRequestID)
			require.Nil(t, original.Usage)
			require.Nil(t, a.record.Usage)
			require.Len(t, store.settlements, 1)
			require.True(t, store.settlements[0].Unknown)
			require.Nil(t, store.settlements[0].Usage)
			var archivedOriginal OpenAICloudSettlement
			require.NoError(t, json.Unmarshal(store.settlements[0].OriginalPayload, &archivedOriginal))
			require.Equal(t, original.RootRequestID, archivedOriginal.RootRequestID)
			require.Nil(t, archivedOriginal.Usage)
			for i := 1; i <= 8; i++ {
				require.ErrorIs(t, a.preserveLateUsage(&OpenAICloudUsage{InputTokens: i, TotalTokens: i}), ErrOpenAICloudFactsUnavailable)
			}
			require.Equal(t, []OpenAICloudLateUsage{marker}, a.record.LateUsage)
		})
	}
}

func TestCloudLateUsageUsesByteBudgetNotFactCount(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t, 30*time.Second)
	a.record.Status, a.record.Usage = "unknown", nil
	a.uncertain = true
	require.NoError(t, a.save(nil, a.settlement("unknown", true)))
	for i := 1; i <= 256; i++ {
		require.NoError(t, a.preserveLateUsage(&OpenAICloudUsage{InputTokens: i, TotalTokens: i}))
	}
	require.Len(t, a.record.LateUsage, 256, "small observations must not trip an independent item cap")
	raw, err := json.Marshal(a.record.LateUsage)
	require.NoError(t, err)
	require.Greater(t, len(raw), 64<<10, "the replaced 64KiB sub-budget must not survive")
	budget, err := a.lateUsageByteBudget()
	require.NoError(t, err)
	require.Less(t, len(raw), budget-cloudLateUsageMarkerReserveBytes)
	require.False(t, a.lateUsageStopped())
	require.Len(t, store.settlements, 1)
}

func TestCloudLateUsageEncodedSizeMatchesJSON(t *testing.T) {
	usage := &OpenAICloudUsage{InputTokens: 9007199254740000, OutputTokens: 90, TotalTokens: 9007199254740090,
		CachedInputTokens: 12, CacheWriteInputTokens: 7, ReasoningOutputTokens: 30, WebSearchCalls: 2, ImageCount: 3,
		ImageSizes:       map[string]int{"<>&\x00\"\\\b\f\n\r\t": 2, "\xff\u2028\u2029中": 1},
		ImageOutputSizes: []string{"1024x1024", "<>&\x00\"\\\b\f\n\r\t", "\xff\u2028\u2029🦀"}}
	fact := OpenAICloudLateUsage{Total: usage, TurnBase: usage, PreviousThreadUsage: usage, Usage: usage,
		ObservedAt: time.Now().UTC(), Conflict: true, ObservationStopped: true}
	for _, facts := range [][]OpenAICloudLateUsage{nil, {}, {fact}, {fact, fact}} {
		for _, extra := range []*OpenAICloudLateUsage{nil, &fact} {
			combined := facts
			if extra != nil {
				combined = append(append([]OpenAICloudLateUsage{}, facts...), *extra)
			}
			raw, err := json.Marshal(combined)
			require.NoError(t, err)
			n, err := OpenAICloudLateUsageEncodedSize(facts, extra, len(raw))
			require.NoError(t, err)
			require.Equal(t, len(raw), n, "escaping, nil/empty, commas and all hosted fields use exact JSON bytes")
			_, err = OpenAICloudLateUsageEncodedSize(facts, extra, len(raw)-1)
			require.Error(t, err)
		}
	}
}

func TestCloudLateUsageInvalidUTF8WidthMatchesActiveEncoder(t *testing.T) {
	for _, value := range []string{"\xff", "\xff\xfe", "\xe2\x82", "\ufffd", "\xff\u2028\u2029<>&🦀"} {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		n, ok := cloudLateUsageStringSize(value, len(raw))
		require.True(t, ok, "exact string boundary must fit the active encoder")
		require.Equal(t, len(raw), n)
		_, ok = cloudLateUsageStringSize(value, len(raw)-1)
		require.False(t, ok)
		fact := OpenAICloudLateUsage{Total: &OpenAICloudUsage{ImageSizes: map[string]int{value: 1}, ImageOutputSizes: []string{value}}}
		encoded, err := json.Marshal([]OpenAICloudLateUsage{fact})
		require.NoError(t, err)
		n, err = OpenAICloudLateUsageEncodedSize(nil, &fact, len(encoded))
		require.NoError(t, err)
		require.Equal(t, len(encoded), n, "map keys and array values share the same active-encoder width")
	}
}

func TestCloudLateUsageByteBudgetIncludesOriginalPayload(t *testing.T) {
	a, _, _ := cloudHostedActorForTest(t)
	a.record.Status, a.record.Usage = "unknown", nil
	a.uncertain = true
	a.terminalSettlement = a.settlement("unknown", true)
	fact := OpenAICloudLateUsage{Total: &OpenAICloudUsage{InputTokens: 5, TotalTokens: 5}, ObservedAt: time.Now().UTC()}
	for _, retained := range []int{0, OpenAICloudMaxRetainedBytes - 4096} {
		a.retainedBytes = retained
		budget, err := a.lateUsageByteBudget()
		require.NoError(t, err)
		next := *a.record
		next.Sequence++
		next.LateUsage = []OpenAICloudLateUsage{fact}
		pending := *a.terminalSettlement
		pending.OriginalPayload, err = json.Marshal(a.terminalSettlement)
		require.NoError(t, err)
		pending.ReconciliationOnly, pending.LateUsage = true, next.LateUsage
		record, err := json.Marshal(&next)
		require.NoError(t, err)
		journal, err := json.Marshal(&pending)
		require.NoError(t, err)
		facts, err := json.Marshal(next.LateUsage)
		require.NoError(t, err)
		require.Equal(t, min(OpenAICloudMaxRetainedBytes-retained,
			OpenAICloudMaxEnvelopeBytes-len(record)+len(facts), OpenAICloudMaxEnvelopeBytes-len(journal)+len(facts)), budget)
	}
}

func TestCloudLateUsageOversizePreflightDoesNotAllocateHostedProjection(t *testing.T) {
	// Allocation is measured only for the preflight; these large source
	// collections already belong to the actor and must not be cloned/encoded.
	large := strings.Repeat("<", 2<<20)
	facts := []OpenAICloudLateUsage{{Usage: &OpenAICloudUsage{ImageOutputSizes: []string{large, large, large}}, ObservedAt: time.Now().UTC()}}
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			if _, err := OpenAICloudLateUsageEncodedSize(facts, nil, OpenAICloudMaxEnvelopeBytes); err == nil {
				b.Fatal("oversized escaped hosted projection was accepted")
			}
		}
	})
	require.Less(t, result.AllocedBytesPerOp(), int64(64<<10), "preflight may encode scalar metadata only, not the multi-MiB hosted projection")
}

func TestCloudLateUsageFailureBackpressuresBothDrains(t *testing.T) {
	for _, queuedOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "terminal_drain", true: "queued_drain"}[queuedOnly], func(t *testing.T) {
			a, store, _ := cloudHostedActorForTest(t)
			a.record.Status, a.record.Usage = "unknown", nil
			a.uncertain = true
			a.lateUsageDrained = queuedOnly
			failed := &cloudLateUsageFailStore{cloudActorTestStore: store, fail: true}
			a.service.store = failed
			producerStopped := false
			a.rpcCancel = func() { producerStopped = true }
			deliveries := make(chan cloudRPCDelivery, 8)
			a.upstream = deliveries
			for i := 1; i <= cap(deliveries); i++ {
				raw, err := json.Marshal(map[string]any{"threadId": "thread", "turnId": "turn", "tokenUsage": map[string]any{"total": map[string]any{
					"inputTokens": i, "outputTokens": 0, "cachedInputTokens": 0, "totalTokens": i,
				}}})
				require.NoError(t, err)
				deliveries <- cloudRPCDelivery{event: cloudRPCMessage{Method: "thread/tokenUsage/updated", Params: raw}}
			}
			if queuedOnly {
				a.drainLateUsage()
			} else {
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				a.drainTerminalFacts(ctx)
			}
			require.True(t, producerStopped)
			require.Len(t, deliveries, 7, "drain must stop reading immediately on persistence failure")
			require.Len(t, a.pendingLateUsage, 1)
			first := a.pendingLateUsage[0]
			for i := 10; i < 100; i++ {
				require.ErrorIs(t, a.preserveLateUsage(&OpenAICloudUsage{InputTokens: i, TotalTokens: i}), ErrOpenAICloudFactsUnavailable)
			}
			a.drainLateUsage()
			require.Len(t, deliveries, 7, "later drain calls retry the pending write without accepting another counter")
			require.Equal(t, []OpenAICloudLateUsage{first}, a.pendingLateUsage)
			require.Empty(t, a.record.LateUsage)
			failed.fail = false
			a.drainLateUsage()
			require.Len(t, deliveries, 7)
			require.Empty(t, a.pendingLateUsage)
			require.NoError(t, a.persistenceErr)
			require.Len(t, a.record.LateUsage, 1)
			require.Equal(t, first, a.record.LateUsage[0], "retry keeps the first observation and its timestamp")
			require.Equal(t, "root", store.settlements[0].RootRequestID)
			require.Nil(t, store.settlements[0].Usage)
		})
	}
}

type cloudLateUsageRefillStore struct {
	*cloudActorTestStore
	deliveries chan cloudRPCDelivery
}

func (s *cloudLateUsageRefillStore) PreserveCloudLateUsage(ctx context.Context, r *OpenAICloudSessionRecord, original *OpenAICloudSettlement) error {
	if err := s.cloudActorTestStore.PreserveCloudLateUsage(ctx, r, original); err != nil {
		return err
	}
	// Refill synchronously to make a continuously nonempty producer deterministic.
	count := len(r.LateUsage) + 1
	raw, _ := json.Marshal(map[string]any{"threadId": r.ThreadID, "turnId": r.TurnID, "tokenUsage": map[string]any{"total": map[string]any{
		"inputTokens": count, "outputTokens": 0, "cachedInputTokens": 0, "totalTokens": count,
	}}})
	s.deliveries <- cloudRPCDelivery{event: cloudRPCMessage{Method: "thread/tokenUsage/updated", Params: raw}}
	return nil
}

func TestCloudLateUsageQueuedDrainOnlyConsumesInitialBatch(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	a.record.Status, a.record.Usage = "unknown", nil
	a.uncertain, a.lateUsageDrained = true, true
	deliveries := make(chan cloudRPCDelivery, 1)
	a.upstream = deliveries
	a.service.store = &cloudLateUsageRefillStore{cloudActorTestStore: store, deliveries: deliveries}
	deliveries <- cloudRPCDelivery{event: cloudRPCMessage{Method: "thread/tokenUsage/updated", Params: json.RawMessage(`{"threadId":"thread","turnId":"turn","tokenUsage":{"total":{"inputTokens":1,"outputTokens":0,"cachedInputTokens":0,"totalTokens":1}}}`)}}
	a.drainLateUsage()
	require.Len(t, a.record.LateUsage, 1)
	require.Len(t, deliveries, 1, "do not chase fresh producer deliveries beyond the initial queue")
	require.Empty(t, a.pendingLateUsage)
}
