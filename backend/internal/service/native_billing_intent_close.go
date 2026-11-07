package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

// NativeIntentNotSentInput must come from the finished operation snapshot,
// including earlier cycles. Cancellation alone is not transport evidence.
type NativeIntentNotSentInput struct {
	Key               billing.NativeIntentKey
	IntentFingerprint string
	Operation         model.OperationResult
}

type NativeIntentNotSentAttempt struct {
	Sequence uint64
	Prepared model.PreparedIdentity
	Send     model.SendState
}

// NativeIntentNotSentRecord stores only identity and actual transport facts.
// It never stores credentials, request bodies, usage zeros or a money key.
type NativeIntentNotSentRecord struct {
	Version            int
	Key                billing.NativeIntentKey
	IntentFingerprint  string
	UserID             int64
	OriginalWireDigest [32]byte
	State              model.OperationState
	Attempts           []NativeIntentNotSentAttempt
}

func prepareNativeIntentNotSent(input NativeIntentNotSentInput) (*NativeIntentNotSentRecord, error) {
	op := input.Operation
	if input.Key.OperationID == "" || input.Key.APIKeyID <= 0 || op.OperationID != input.Key.OperationID || op.APIKeyID != input.Key.APIKeyID || op.UserID <= 0 || op.OriginalWireDigest == ([32]byte{}) ||
		(op.State != model.OperationFailed && op.State != model.OperationCanceled) || op.DelegatedCycles != 0 || op.Delivery.BusinessCommitted || op.Delivery.PossiblyCommitted || len(op.Attempts) == 0 || len(op.Attempts) > 64 {
		return nil, billing.ErrNativeIntentInvalid
	}
	if _, err := hex.DecodeString(input.IntentFingerprint); err != nil || len(input.IntentFingerprint) != 64 {
		return nil, billing.ErrNativeIntentInvalid
	}
	record := &NativeIntentNotSentRecord{Version: 1, Key: input.Key, IntentFingerprint: input.IntentFingerprint, UserID: op.UserID, OriginalWireDigest: op.OriginalWireDigest, State: op.State}
	seen := make(map[uint64]bool, len(op.Attempts))
	for _, attempt := range op.Attempts {
		p := attempt.Identity
		if attempt.Sequence == 0 || seen[attempt.Sequence] || !p.Valid() || p.OperationID != op.OperationID || p.APIKeyID != op.APIKeyID || p.UserID != op.UserID || p.GroupBound != op.GroupBound || p.GroupID != op.GroupID ||
			p.Mode != "native" || (p.Transport != "http" && p.Transport != "ws") || attempt.Send != model.SendNotSent || attempt.RequestID != "" || attempt.ResponseID != "" || attempt.Usage != nil || attempt.Terminal != model.TerminalUnknown ||
			attempt.Delivery.BusinessCommitted || attempt.Delivery.PossiblyCommitted {
			return nil, billing.ErrNativeIntentConflict
		}
		seen[attempt.Sequence] = true
		record.Attempts = append(record.Attempts, NativeIntentNotSentAttempt{Sequence: attempt.Sequence, Prepared: p, Send: attempt.Send})
	}
	sort.Slice(record.Attempts, func(i, j int) bool { return record.Attempts[i].Sequence < record.Attempts[j].Sequence })
	return record, nil
}

func (input *NativeIntentNotSentInput) NativeIntentClosureReference() (billing.NativeIntentClosureReference, error) {
	if input == nil {
		return billing.NativeIntentClosureReference{}, billing.ErrNativeIntentInvalid
	}
	record, err := prepareNativeIntentNotSent(*input)
	if err != nil {
		return billing.NativeIntentClosureReference{}, err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return billing.NativeIntentClosureReference{}, err
	}
	digest := sha256.Sum256(raw)
	return billing.NativeIntentClosureReference{Key: record.Key, IntentFingerprint: record.IntentFingerprint, ClosureFingerprint: hex.EncodeToString(digest[:])}, nil
}

// ValidateNativeIntentNotSent compares the complete terminal snapshot to the
// locked preserved scopes. A final NotSent attempt cannot hide an older send.
func ValidateNativeIntentNotSent(stored *NativeUsageIntentRecord, input NativeIntentNotSentInput) (*NativeIntentNotSentRecord, error) {
	closure, err := prepareNativeIntentNotSent(input)
	if err != nil {
		return nil, err
	}
	if stored == nil || stored.Key != closure.Key || stored.UserID != closure.UserID || stored.OriginalWireDigest != closure.OriginalWireDigest {
		return nil, billing.ErrNativeIntentConflict
	}
	scopes := make(map[uint64]NativeIntentAttemptScope, len(stored.Attempts))
	for _, scope := range stored.Attempts {
		scopes[scope.Sequence] = scope
	}
	for _, attempt := range closure.Attempts {
		scope, ok := scopes[attempt.Sequence]
		if !ok {
			// Registration precedes price capture/persistence. The complete
			// snapshot above proves this attempt never sent and has no upstream
			// facts. Keep it in the closure instead of fabricating a saved scope.
			continue
		}
		if scope.Prepared != attempt.Prepared {
			return nil, billing.ErrNativeIntentConflict
		}
		preserved := NativeUsageIntent{Key: stored.Key, UserID: stored.UserID, OriginalWireDigest: stored.OriginalWireDigest, Request: stored.Request, Attempt: scope}
		ref, err := preserved.NativeIntentReference()
		if err != nil {
			return nil, err
		}
		if ref.IntentFingerprint != closure.IntentFingerprint {
			return nil, billing.ErrNativeIntentConflict
		}
		delete(scopes, attempt.Sequence)
	}
	if len(scopes) != 0 {
		return nil, billing.ErrNativeIntentConflict
	}
	return closure, nil
}

func (s *NativeBillingIntentService) CloseNotSent(ctx context.Context, input NativeIntentNotSentInput) (billing.NativeIntentClosureReceipt, error) {
	var empty billing.NativeIntentClosureReceipt
	if ctx == nil || s == nil || s.repo == nil {
		return empty, billing.ErrNativeIntentUnavailable
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	// Detach the caller's live slice and pointers before entering storage.
	raw, err := json.Marshal(input)
	if err != nil {
		return empty, err
	}
	var detached NativeIntentNotSentInput
	if err := json.Unmarshal(raw, &detached); err != nil {
		return empty, err
	}
	expected, err := detached.NativeIntentClosureReference()
	if err != nil {
		return empty, err
	}
	receipt, err := s.repo.CloseNativeUsageIntent(ctx, &detached)
	if err != nil {
		return empty, err
	}
	if !receipt.Durable() || receipt.NativeIntentClosureReference != expected {
		return empty, billing.ErrNativeIntentConflict
	}
	return receipt, nil
}
