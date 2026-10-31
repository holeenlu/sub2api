package service

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strconv"
	"time"
	"unicode/utf8"
)

// Leave room for a token-only marker before accepting another full projection.
// Its three fixed-size counters and timestamp fit independently of hosted data.
const cloudLateUsageMarkerReserveBytes = 2 << 10

var errCloudLateUsageLimit = errors.New("Cloud late usage observation limit reached")

// Go's legacy JSON encoder escapes a replacement as six bytes, while the
// jsonv2-backed encoding/json (default in Go 1.27) emits its three UTF-8 bytes.
// Probe only this fixed scalar once; never encode an unbounded hosted string.
var cloudLateUsageInvalidUTF8Bytes = func() int {
	raw, _ := json.Marshal("\xff")
	return len(raw) - 2
}()

func cloudTerminalStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "interrupted" || status == "unknown"
}

// Called by the actor only after the normal event ownership and usage parser
// checks. Keep the terminal result, sampling steps and original bill immutable.
func (a *cloudSessionActor) preserveLateUsage(total *OpenAICloudUsage) error {
	if len(a.pendingLateUsage) > 0 {
		if cloudRecoveryUsageEqual(total, a.pendingLateUsage[0].Total) {
			return a.flushLateUsage()
		}
		return a.stopLateUsage(ErrOpenAICloudFactsUnavailable)
	}
	if a.lateUsageStopped() {
		return a.stopLateUsage(errCloudLateUsageLimit)
	}
	if cloudRecoveryUsageEqual(total, a.threadTotal) {
		return a.flushLateUsage()
	}
	for _, facts := range [][]OpenAICloudLateUsage{a.record.LateUsage, a.pendingLateUsage} {
		for _, fact := range facts {
			if cloudRecoveryUsageEqual(fact.Total, total) {
				return a.flushLateUsage()
			}
		}
	}
	fact := OpenAICloudLateUsage{Total: total, TurnBase: a.turnBase, PreviousThreadUsage: a.previousThreadUsage, ObservedAt: time.Now().UTC()}
	usage, err := cloudUsageDifference(total, a.turnBase)
	if err == nil {
		// Borrow immutable actor data for sizing. Clone hosted collections only
		// after their actual encoded bytes fit the existing owner/journal budget.
		projected := *cloudSumTokenUsage(a.previousThreadUsage, usage)
		projected.WebSearchCalls, projected.ImageCount = a.hostedUsage.WebSearchCalls, a.hostedUsage.ImageCount
		projected.ImageSizes, projected.ImageOutputSizes = a.hostedUsage.ImageSizes, a.hostedUsage.ImageOutputSizes
		fact.Usage = &projected
	}
	_, stepErr := cloudUsageDifference(total, a.threadTotal)
	fact.Conflict = err != nil || stepErr != nil || (a.record.Usage != nil && !cloudRecoveryUsageEqual(fact.Usage, a.record.Usage))
	for _, facts := range [][]OpenAICloudLateUsage{a.record.LateUsage, a.pendingLateUsage} {
		for _, prior := range facts {
			if _, err := cloudUsageDifference(total, prior.Total); err != nil {
				fact.Conflict = true
			}
		}
	}
	budget, budgetErr := a.lateUsageByteBudget()
	_, sizeErr := OpenAICloudLateUsageEncodedSize(a.record.LateUsage, &fact, budget-cloudLateUsageMarkerReserveBytes)
	if budgetErr != nil || sizeErr != nil {
		// Preserve the newly observed counter and token baselines, without
		// duplicating potentially large hosted projections. The original bill
		// and all earlier facts remain intact in the same journal/archive.
		fact.Total = cloudLateUsageTokenCounters(total)
		fact.TurnBase = cloudLateUsageTokenCounters(a.turnBase)
		fact.PreviousThreadUsage = cloudLateUsageTokenCounters(a.previousThreadUsage)
		fact.Usage, fact.ObservationStopped = nil, true
	} else {
		fact.Total = cloudCloneLateUsage(fact.Total)
		fact.TurnBase = cloudCloneLateUsage(fact.TurnBase)
		fact.PreviousThreadUsage = cloudCloneLateUsage(fact.PreviousThreadUsage)
		fact.Usage = cloudCloneLateUsage(fact.Usage)
	}
	// Exactly one observation may wait for persistence. Never consume another
	// delivery while it is pending; an ambiguous write retries these same bytes.
	a.pendingLateUsage = []OpenAICloudLateUsage{fact}
	return a.flushLateUsage()
}

func cloudLateUsageTokenCounters(usage *OpenAICloudUsage) *OpenAICloudUsage {
	if usage == nil {
		return nil
	}
	return &OpenAICloudUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CachedInputTokens: usage.CachedInputTokens, CacheWriteInputTokens: usage.CacheWriteInputTokens,
		TotalTokens: usage.TotalTokens, ReasoningOutputTokens: usage.ReasoningOutputTokens}
}

func (a *cloudSessionActor) lateUsageStopped() bool {
	return len(a.record.LateUsage) > 0 && a.record.LateUsage[len(a.record.LateUsage)-1].ObservationStopped
}

func cloudCloneLateUsage(usage *OpenAICloudUsage) *OpenAICloudUsage {
	if usage == nil {
		return nil
	}
	copy := *usage
	copy.ImageSizes = maps.Clone(usage.ImageSizes)
	copy.ImageOutputSizes = append([]string(nil), usage.ImageOutputSizes...)
	return &copy
}

// Share the existing byte owners. The actor's retained history/result bytes,
// the session envelope, and the journal (including the original base64 payload)
// all consume real capacity; there is no independent fact-count admission gate.
func (a *cloudSessionActor) lateUsageByteBudget() (int, error) {
	base := *a.record
	base.Sequence++
	base.LateUsage = nil
	record, err := json.Marshal(&base)
	if err != nil {
		return 0, err
	}
	original := a.terminalSettlement
	if original == nil {
		original = a.settlement(a.record.Status, a.uncertain)
	}
	raw, err := json.Marshal(original)
	if err != nil {
		return 0, err
	}
	journal := *original
	journal.ReconciliationOnly, journal.LateUsage, journal.OriginalPayload = true, nil, nil
	encoded, err := json.Marshal(&journal)
	if err != nil {
		return 0, err
	}
	journalBytes := len(encoded) + len(`,"original_payload":""`) + 4*((len(raw)+2)/3)
	return min(OpenAICloudMaxRetainedBytes-a.retainedBytes,
		OpenAICloudMaxEnvelopeBytes-len(record)-len(`,"late_usage":`),
		OpenAICloudMaxEnvelopeBytes-journalBytes-len(`,"late_usage":`)), nil
}

// OpenAICloudLateUsageEncodedSize measures the exact json.Marshal byte count
// without assembling the array or copying/marshalling hosted strings/maps.
// Only the fixed-size scalar part of one fact is marshalled at a time. The
// repository calls the same preflight before encoding its bounded envelope.
func OpenAICloudLateUsageEncodedSize(facts []OpenAICloudLateUsage, extra *OpenAICloudLateUsage, limit int) (int, error) {
	used := 2 // brackets, including an empty nonnil array
	if facts == nil && extra == nil {
		used = 4 // null
	}
	add := func(n int) bool {
		if n < 0 || used > limit || n > limit-used {
			return false
		}
		used += n
		return true
	}
	if !add(0) {
		return 0, errCloudLateUsageLimit
	}
	count := len(facts)
	if extra != nil {
		count++
	}
	for i := 0; i < count; i++ {
		fact := extra
		if i < len(facts) {
			fact = &facts[i]
		}
		if i > 0 && !add(1) {
			return 0, errCloudLateUsageLimit
		}
		scalar := *fact
		strip := func(usage *OpenAICloudUsage) *OpenAICloudUsage {
			if usage == nil {
				return nil
			}
			copy := *usage
			copy.ImageSizes, copy.ImageOutputSizes = nil, nil
			return &copy
		}
		scalar.Total, scalar.TurnBase = strip(fact.Total), strip(fact.TurnBase)
		scalar.PreviousThreadUsage, scalar.Usage = strip(fact.PreviousThreadUsage), strip(fact.Usage)
		raw, err := json.Marshal(&scalar)
		if err != nil {
			return 0, err
		}
		if !add(len(raw)) {
			return 0, errCloudLateUsageLimit
		}
		for _, usage := range []*OpenAICloudUsage{fact.Total, fact.TurnBase, fact.PreviousThreadUsage, fact.Usage} {
			if usage == nil {
				continue
			}
			if len(usage.ImageSizes) > 0 {
				if !add(len(`,"image_sizes":{}`)) {
					return 0, errCloudLateUsageLimit
				}
				first := true
				for key, value := range usage.ImageSizes {
					n, ok := cloudLateUsageStringSize(key, limit-used)
					if !ok || !add(n+1+len(strconv.Itoa(value))) || (!first && !add(1)) {
						return 0, errCloudLateUsageLimit
					}
					first = false
				}
			}
			if len(usage.ImageOutputSizes) > 0 {
				if !add(len(`,"image_output_sizes":[]`)) {
					return 0, errCloudLateUsageLimit
				}
				for j, value := range usage.ImageOutputSizes {
					n, ok := cloudLateUsageStringSize(value, limit-used)
					if !ok || !add(n) || (j > 0 && !add(1)) {
						return 0, errCloudLateUsageLimit
					}
				}
			}
		}
	}
	return used, nil
}

// encoding/json's default string escaping, counted without allocating the
// escaped string. Reject oversized input before even scanning its contents.
func cloudLateUsageStringSize(value string, limit int) (int, bool) {
	if limit < 2 || len(value) > limit-2 {
		return 0, false
	}
	n := 2
	for i := 0; i < len(value); {
		b := value[i]
		if b < utf8.RuneSelf {
			i++
			switch b {
			case '\\', '"', '\b', '\f', '\n', '\r', '\t':
				n += 2
			default:
				if b < 0x20 || b == '<' || b == '>' || b == '&' {
					n += 6
				} else {
					n++
				}
			}
		} else {
			r, size := utf8.DecodeRuneInString(value[i:])
			i += size
			if r == utf8.RuneError && size == 1 {
				n += cloudLateUsageInvalidUTF8Bytes
			} else if r == '\u2028' || r == '\u2029' {
				n += 6
			} else {
				n += size
			}
		}
		if n > limit {
			return 0, false
		}
	}
	return n, true
}

func (a *cloudSessionActor) stopLateUsage(cause error) error {
	a.persistenceErr = ErrOpenAICloudFactsUnavailable
	if a.rpcCancel != nil {
		a.rpcCancel()
	}
	if a.rpc != nil {
		// Nonjoining shutdown: the actor's existing defer owns transport join.
		a.rpc.fail(ErrOpenAICloudFactsUnavailable)
	}
	return errors.Join(ErrOpenAICloudFactsUnavailable, cause)
}

func (a *cloudSessionActor) flushLateUsage() error {
	if len(a.pendingLateUsage) == 0 {
		return nil
	}
	next := *a.record
	next.Sequence++
	next.LateUsage = append(append([]OpenAICloudLateUsage{}, a.record.LateUsage...), a.pendingLateUsage...)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx), 3*time.Second)
	defer cancel()
	original := a.terminalSettlement
	if original == nil {
		original = a.settlement(a.record.Status, a.uncertain)
	}
	if err := a.service.store.PreserveCloudLateUsage(ctx, &next, original); err != nil {
		return a.stopLateUsage(err)
	}
	*a.record = next
	a.pendingLateUsage = nil
	a.persistenceErr = nil
	if a.lateUsageStopped() {
		return a.stopLateUsage(errCloudLateUsageLimit)
	}
	return nil
}

// Drain on the existing actor, with its original socket ownership and a bounded
// context independent of downstream cancellation. A terminal event is not the
// end of the FIFO: token facts may follow it. No output/tool handoff is allowed.
func (a *cloudSessionActor) drainTerminalFacts(ctx context.Context) {
	defer func() {
		a.lateUsageDrained = true
		_ = a.flushLateUsage() // failure remains visible to the manager's Stop/Drain
	}()
	if len(a.pendingLateUsage) > 0 || a.lateUsageStopped() {
		return
	}
	if a.upstream == nil {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case delivery, ok := <-a.upstream:
			if !ok || delivery.err != nil {
				return
			}
			event := delivery.event
			if event.Method == "thread/tokenUsage/updated" || (!cloudTerminalStatus(a.record.Status) && event.Method == "turn/completed") {
				if err := a.handleEvent(event); err != nil && errors.Is(err, ErrOpenAICloudFactsUnavailable) {
					a.persistenceErr = ErrOpenAICloudFactsUnavailable
					return
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (a *cloudSessionActor) drainLateUsage() {
	if len(a.pendingLateUsage) > 0 || a.lateUsageStopped() {
		_ = a.flushLateUsage()
		return
	}
	if !a.lateUsageDrained && a.record.ThreadID != "" && a.record.TurnID != "" {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx), 3*time.Second)
		a.drainTerminalFacts(ctx)
		cancel()
	} else {
		// stop already spent its interrupt budget. Archival may have queued
		// additional facts meanwhile. Snapshot that bounded channel occupancy;
		// a continuously refilling producer must not turn this into another loop.
	queued:
		for remaining := len(a.upstream); remaining > 0; remaining-- {
			select {
			case delivery, ok := <-a.upstream:
				if !ok || delivery.err != nil {
					break queued
				}
				if delivery.event.Method == "thread/tokenUsage/updated" {
					if err := a.handleEvent(delivery.event); errors.Is(err, ErrOpenAICloudFactsUnavailable) {
						break queued
					}
				}
			default:
				break queued
			}
		}
		_ = a.flushLateUsage()
	}
}

// The next root starts at the latest monotonic measured thread watermark. It
// must not charge an earlier root's late tokens again. Conflicts remain archived.
func (a *cloudSessionActor) advanceLateUsageWatermark() {
	for _, fact := range a.record.LateUsage {
		if _, err := cloudUsageDifference(fact.Total, a.threadTotal); err == nil {
			a.threadTotal = fact.Total
		}
	}
}
