package billing

import (
	"context"
	"errors"
	"testing"
)

type nativeIntentRecoveryPorts struct {
	*ledgerPorts
	recovered, retried []NativeIntentKey
	done               bool
	err                error
}

func (p *nativeIntentRecoveryPorts) RecoverNativePreparation(_ context.Context, event Event) (bool, error) {
	if event.Kind != NativePreparationEvent || event.Identity != (Identity{}) {
		return false, ErrNativeIntentConflict
	}
	p.recovered = append(p.recovered, event.IntentKey)
	return p.done, p.err
}
func (p *nativeIntentRecoveryPorts) RetryNativePreparation(_ context.Context, key NativeIntentKey, _ error) {
	p.retried = append(p.retried, key)
}

func TestNativeIntentRecoveryUsesTechnicalIdentityAndRealCompletion(t *testing.T) {
	for _, stage := range []string{"complete", "pending", "failed"} {
		t.Run(stage, func(t *testing.T) {
			key := NativeIntentKey{OperationID: "technical-operation", APIKeyID: 2}
			p := &nativeIntentRecoveryPorts{ledgerPorts: newLedgerPorts(), done: stage == "complete"}
			p.events = []Event{{Kind: NativePreparationEvent, IntentKey: key, Payload: []byte("durable preparation")}}
			if stage == "failed" {
				p.err = errors.New("original builder unavailable")
			}
			count, err := (Ledger{Journal: p, Transaction: p}).Recover(t.Context(), 64)
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if p.done {
				want = 1
			}
			if count != want {
				t.Fatalf("count=%d, want %d", count, want)
			}
			wantCalls(t, p.ledgerPorts, "claim", "compact")
			if len(p.recovered) != 1 || p.recovered[0] != key {
				t.Fatalf("wrong recovery identity: %+v", p.recovered)
			}
			if stage == "failed" && (len(p.retried) != 1 || p.retried[0] != key) {
				t.Fatal("technical retry lost its operation identity")
			}
			if stage != "failed" && len(p.retried) != 0 {
				t.Fatal("unexpected technical retry")
			}
		})
	}
}
