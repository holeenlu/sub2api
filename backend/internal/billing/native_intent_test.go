package billing

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type intentTestCommand struct{ ref NativeIntentReference }

func (c intentTestCommand) NativeIntentReference() (NativeIntentReference, error) { return c.ref, nil }

type intentTestJournal struct {
	Journal
	receipt NativeIntentReceipt
	err     error
	calls   int
}

func (j *intentTestJournal) PreserveNativeIntent(context.Context, NativeIntentCommand) (NativeIntentReceipt, error) {
	j.calls++
	return j.receipt, j.err
}

func TestNativeIntentLedgerRequiresActualJournalAcknowledgement(t *testing.T) {
	ref := NativeIntentReference{Key: NativeIntentKey{OperationID: "operation", APIKeyID: 2}, AttemptSequence: 1, IntentFingerprint: strings.Repeat("a", 64), ScopeDigest: strings.Repeat("b", 64)}
	for _, stage := range []string{"success", "missing_ack", "wrong_scope", "sql_error"} {
		t.Run(stage, func(t *testing.T) {
			j := &intentTestJournal{receipt: NativeIntentReceipt{NativeIntentReference: ref, Disposition: NativeIntentPreserved}}
			switch stage {
			case "missing_ack":
				j.receipt.Disposition = NativeIntentNotPreserved
			case "wrong_scope":
				j.receipt.ScopeDigest = strings.Repeat("c", 64)
			case "sql_error":
				j.err = errors.New("uncertain SQL commit")
			}
			got, err := (Ledger{Journal: j}).PreserveNativeIntent(t.Context(), intentTestCommand{ref})
			if stage == "success" {
				if err != nil || !got.Durable() {
					t.Fatalf("receipt=%+v err=%v", got, err)
				}
			} else if err == nil || got.Durable() {
				t.Fatalf("unconfirmed preservation: receipt=%+v err=%v", got, err)
			}
			if j.calls != 1 {
				t.Fatalf("journal calls=%d", j.calls)
			}
		})
	}
	j := &intentTestJournal{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := (Ledger{Journal: j}).PreserveNativeIntent(ctx, intentTestCommand{ref})
	if !errors.Is(err, context.Canceled) || got.Durable() || j.calls != 0 {
		t.Fatalf("cancel receipt=%+v err=%v calls=%d", got, err, j.calls)
	}
}
