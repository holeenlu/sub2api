package billing

import (
	"context"
	"encoding/hex"
	"errors"
)

var (
	ErrNativeIntentInvalid     = errors.New("native billing intent is invalid")
	ErrNativeIntentConflict    = errors.New("native billing intent conflicts with preserved scope")
	ErrNativeIntentUnavailable = errors.New("native billing intent journal is unavailable")
	ErrNativeIntentClosed      = errors.New("native billing intent is already closed")
)

// NativeIntentKey identifies a technical journal event, never a money event.
// A provider request ID is deliberately absent until the real terminal binding.
type NativeIntentKey struct {
	OperationID string
	APIKeyID    int64
}

// NativeIntentClosureReference identifies a terminal technical observation,
// not a monetary transaction. Its fingerprint covers every actual attempt.
type NativeIntentClosureReference struct {
	Key                NativeIntentKey
	IntentFingerprint  string
	ClosureFingerprint string
}

type NativeIntentClosureReceipt struct {
	NativeIntentClosureReference
	ClosedNotSent bool
}

func (r NativeIntentClosureReceipt) Durable() bool {
	return r.ClosedNotSent && (NativeIntentReference{Key: r.Key, AttemptSequence: 1,
		IntentFingerprint: r.IntentFingerprint, ScopeDigest: r.ClosureFingerprint}).valid()
}

type NativeIntentClosureCommand interface {
	NativeIntentClosureReference() (NativeIntentClosureReference, error)
}

type NativeIntentClosureJournal interface {
	CloseNativeIntent(context.Context, NativeIntentClosureCommand) (NativeIntentClosureReceipt, error)
}

func (l Ledger) CloseNativeIntent(ctx context.Context, command NativeIntentClosureCommand) (NativeIntentClosureReceipt, error) {
	var empty NativeIntentClosureReceipt
	if ctx == nil || command == nil {
		return empty, ErrNativeIntentInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	expected, err := command.NativeIntentClosureReference()
	if err != nil {
		return empty, err
	}
	if !(NativeIntentClosureReceipt{NativeIntentClosureReference: expected, ClosedNotSent: true}).Durable() {
		return empty, ErrNativeIntentInvalid
	}
	journal, ok := l.Journal.(NativeIntentClosureJournal)
	if !ok {
		return empty, ErrNativeIntentUnavailable
	}
	receipt, err := journal.CloseNativeIntent(ctx, command)
	if err != nil {
		return empty, err
	}
	if !receipt.Durable() || receipt.NativeIntentClosureReference != expected {
		return empty, ErrNativeIntentConflict
	}
	return receipt, nil
}

// NativeIntentReference identifies an actual prepared scope on that technical
// event. It contains neither a credential nor authority to send or charge.
type NativeIntentReference struct {
	Key               NativeIntentKey
	AttemptSequence   uint64
	IntentFingerprint string
	ScopeDigest       string
}

func (r NativeIntentReference) valid() bool {
	if r.Key.OperationID == "" || r.Key.APIKeyID <= 0 || r.AttemptSequence == 0 {
		return false
	}
	for _, value := range []string{r.IntentFingerprint, r.ScopeDigest} {
		if len(value) != 64 {
			return false
		}
		if _, err := hex.DecodeString(value); err != nil {
			return false
		}
	}
	return true
}

type NativeIntentDisposition uint8

const (
	NativeIntentNotPreserved NativeIntentDisposition = iota
	NativeIntentPreserved
)

// NativeIntentReceipt confirms only durable price/intent preservation. It is
// not a usage receipt, a SendPermit, an Applied result or proof of NotSent.
type NativeIntentReceipt struct {
	NativeIntentReference
	Disposition NativeIntentDisposition
}

func (r NativeIntentReceipt) Durable() bool {
	return r.Disposition == NativeIntentPreserved && r.NativeIntentReference.valid()
}

type NativeIntentCommand interface {
	NativeIntentReference() (NativeIntentReference, error)
}

type NativeIntentJournal interface {
	PreserveNativeIntent(context.Context, NativeIntentCommand) (NativeIntentReceipt, error)
}

// PreserveNativeIntent uses the existing journal without a monetary transaction.
// A storage error, including an uncertain commit, never confirms durability.
func (l Ledger) PreserveNativeIntent(ctx context.Context, command NativeIntentCommand) (NativeIntentReceipt, error) {
	var empty NativeIntentReceipt
	if ctx == nil || command == nil {
		return empty, ErrNativeIntentInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	expected, err := command.NativeIntentReference()
	if err != nil {
		return empty, err
	}
	if !expected.valid() {
		return empty, ErrNativeIntentInvalid
	}
	journal, ok := l.Journal.(NativeIntentJournal)
	if !ok {
		return empty, ErrNativeIntentUnavailable
	}
	receipt, err := journal.PreserveNativeIntent(ctx, command)
	if err != nil {
		return empty, err
	}
	if !receipt.Durable() || receipt.NativeIntentReference != expected {
		return empty, ErrNativeIntentConflict
	}
	return receipt, nil
}
