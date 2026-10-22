package service

import (
	"container/heap"
	"context"
	"math"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
)

// Native adapters retain priority/reset/load/LRU inside each observed batch.
// Sampling before Redis removes ID-prefix bias without adding a second cache.
func (s *GatewayService) tryGatewayLoadBatches(ctx context.Context, accounts []*Account, groupID *int64, session, model string, preferOAuth, preferReset bool, probes *int) (*AccountSelectionResult, []*Account, error) {
	if home := schedulingAffinityFrom(ctx); home != nil && home.home != nil {
		peers := make([]*Account, 0, len(accounts))
		for _, account := range accounts {
			if account.ID != home.home.ID && account.Priority == home.home.Priority && (!preferOAuth || account.IsOAuth() == home.home.IsOAuth()) {
				peers = append(peers, account)
			}
		}
		accounts = peers
	}
	if !s.schedulingConfig().LoadBatchEnabled {
		result, _, err := s.tryAcquireByLegacyOrder(ctx, accounts, groupID, session, preferOAuth, probes)
		return result, nil, err
	}
	loadRequests := gatewayAccountLoadRequest(ctx, accounts, preferOAuth, s.settingService)
	byID := make(map[int64]AccountWithConcurrency, len(loadRequests))
	for _, request := range loadRequests {
		byID[request.ID] = request
	}
	type sampled struct {
		account *Account
		key     float64
	}
	order := make([]sampled, 0, len(accounts))
	seed := deriveOpenAISelectionSeed(OpenAIAccountScheduleRequest{GroupID: groupID, SessionHash: session, RequestedModel: model})
	for _, account := range accounts {
		rng := openAISelectionRNG{state: seed ^ uint64(account.ID)}
		u := math.Max(rng.nextFloat64(), math.SmallestNonzeroFloat64)
		order = append(order, sampled{account, -math.Log(u) / float64(max(1, byID[account.ID].MaxConcurrency))})
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.account.Priority != b.account.Priority {
			return a.account.Priority < b.account.Priority
		}
		if preferOAuth && a.account.IsOAuth() != b.account.IsOAuth() {
			return a.account.IsOAuth()
		}
		if a.key != b.key {
			return a.key < b.key
		}
		return a.account.ID < b.account.ID
	})
	var waiting []*Account
	batches := 0
	for start := 0; start < len(order); {
		if start > 0 {
			RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "selection", Action: "next_layer"})
		}
		end := start + 1
		for end < len(order) && order[end].account.Priority == order[start].account.Priority && (!preferOAuth || order[end].account.IsOAuth() == order[start].account.IsOAuth()) {
			end++
		}
		layerCtx := ctx
		ceiling := openAIAccountSelectionProbeLimit
		if end < len(order) {
			layerCtx = selectionbudget.Reserve(ctx, min(selectionbudget.Remaining(ctx), max(12, (selectionbudget.Remaining(ctx)+3)/4)))
			ceiling -= (ceiling - *probes + 3) / 4
		}
		for offset := start; offset < end; offset += schedulerLoadBatchSize {
			if ctx.Err() != nil {
				return nil, waiting, ctx.Err()
			}
			if selectionbudget.Remaining(layerCtx) < 8 || *probes >= ceiling || batches >= openAIAccountSelectionProbeLimit {
				break
			}
			batchEnd := min(end, offset+schedulerLoadBatchSize)
			requests := make([]AccountWithConcurrency, 0, batchEnd-offset)
			for _, item := range order[offset:batchEnd] {
				requests = append(requests, byID[item.account.ID])
			}
			loads, err := s.concurrencyService.GetAccountsLoadBatch(layerCtx, requests)
			batches++
			if err != nil {
				result, _, fallbackErr := s.tryAcquireByLegacyOrder(layerCtx, accounts, groupID, session, preferOAuth, probes)
				return result, waiting, fallbackErr
			}
			available := make([]accountWithLoad, 0, len(requests))
			for _, item := range order[offset:batchEnd] {
				load := loads[item.account.ID]
				if load == nil {
					load = &AccountLoadInfo{AccountID: item.account.ID}
				}
				limit := nativeSchedulingConcurrency(ctx, item.account, s.settingService)
				if limit <= 0 || load.CurrentConcurrency < limit {
					available = append(available, accountWithLoad{account: item.account, loadInfo: load})
				}
			}
			orderGatewayLoadCandidates(available, preferOAuth, preferReset)
			for _, item := range available {
				waiting = append(waiting, item.account)
				if selectionbudget.Remaining(layerCtx) < 8 || *probes >= ceiling {
					break
				}
				*probes++
				slot, err := s.tryAcquireAccountSlot(layerCtx, item.account.ID, nativeSchedulingConcurrency(ctx, item.account, s.settingService))
				if err != nil {
					return nil, waiting, err
				}
				if slot == nil || !slot.Acquired {
					continue
				}
				if !s.checkAndRegisterSession(layerCtx, item.account, session) {
					slot.ReleaseFunc()
					continue
				}
				result, err := s.newSelectionResult(layerCtx, item.account, true, slot.ReleaseFunc, nil)
				if err != nil {
					return nil, waiting, err
				}
				_ = s.bindGatewayStickySessionDuringSelection(layerCtx, groupID, session, result.Account)
				return result, waiting, nil
			}
		}
		start = end
	}
	return nil, waiting, nil
}

// Heapify the same weighted candidates instead of fully sorting and copying a
// large pool when the first small batch usually satisfies the request.
type schedulerHeapLayer struct {
	degraded                            bool
	priority, preference, cost, compact int
}
type schedulerCandidateHeap struct {
	items   []openAIAccountCandidateScore
	compact bool
	layers  map[schedulerHeapLayer]int
}

func newSchedulerCandidateHeap(items []openAIAccountCandidateScore, compact bool) *schedulerCandidateHeap {
	q := &schedulerCandidateHeap{items: items, compact: compact, layers: map[schedulerHeapLayer]int{}}
	for _, item := range items {
		q.layers[q.layer(item)]++
	}
	heap.Init(q)
	return q
}
func (q *schedulerCandidateHeap) layer(item openAIAccountCandidateScore) schedulerHeapLayer {
	tier := 0
	if q.compact {
		tier = openAICompactSupportTier(item.account)
	}
	return schedulerHeapLayer{item.degraded, item.account.Priority, item.preference, item.costBand, tier}
}
func (q *schedulerCandidateHeap) Len() int { return len(q.items) }
func (q *schedulerCandidateHeap) Less(i, j int) bool {
	if q.compact {
		a, b := openAICompactSupportTier(q.items[i].account), openAICompactSupportTier(q.items[j].account)
		if a != b {
			return a > b
		}
	}
	return isOpenAIAccountCandidateBetter(q.items[i], q.items[j])
}
func (q *schedulerCandidateHeap) Swap(i, j int) { q.items[i], q.items[j] = q.items[j], q.items[i] }
func (q *schedulerCandidateHeap) Push(value any) {
	item, _ := value.(openAIAccountCandidateScore)
	q.items = append(q.items, item)
	q.layers[q.layer(item)]++
}
func (q *schedulerCandidateHeap) Pop() any {
	n := len(q.items) - 1
	item := q.items[n]
	q.items[n] = openAIAccountCandidateScore{}
	q.items = q.items[:n]
	key := q.layer(item)
	q.layers[key]--
	if q.layers[key] == 0 {
		delete(q.layers, key)
	}
	return item
}
func (q *schedulerCandidateHeap) dropLayer(layer schedulerHeapLayer) {
	kept := q.items[:0]
	for _, item := range q.items {
		if q.layer(item) != layer {
			kept = append(kept, item)
		}
	}
	clear(q.items[len(kept):])
	q.items = kept
	delete(q.layers, layer)
	heap.Init(q)
}
