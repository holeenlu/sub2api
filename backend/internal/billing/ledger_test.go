package billing

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type ledgerCommand struct {
	id             Identity
	pending, usage bool
	price          int
}

func (c *ledgerCommand) BillingIdentity() Identity { return c.id }
func (c *ledgerCommand) NeedsPricing() bool        { return c.pending }
func (c *ledgerCommand) HasUsageRecord() bool      { return c.usage }

type ledgerPorts struct {
	calls                                                                             []string
	command                                                                           *ledgerCommand
	events                                                                            []Event
	frozen                                                                            Command
	recordErr, commitErr, decodeErr, pricingErr, freezeErr, invalidationErr, claimErr error
	unresolved, clearFrozen                                                           bool
	limit                                                                             int
	committed                                                                         Command
	retryIdentity                                                                     Identity
	retryPending                                                                      bool
	afterCommit                                                                       func()
}

func (p *ledgerPorts) Record(context.Context, Command) error {
	p.calls = append(p.calls, "record")
	return p.recordErr
}
func (p *ledgerPorts) Claim(_ context.Context, limit int) ([]Event, error) {
	p.calls = append(p.calls, "claim")
	p.limit = limit
	return p.events, p.claimErr
}
func (p *ledgerPorts) Decode(Event) (Command, error) {
	p.calls = append(p.calls, "decode")
	return p.command, p.decodeErr
}
func (p *ledgerPorts) FreezePrice(_ context.Context, c Command) (Command, error) {
	p.calls = append(p.calls, "freeze")
	if p.clearFrozen {
		return nil, p.freezeErr
	}
	if p.frozen != nil {
		return p.frozen, p.freezeErr
	}
	return c, p.freezeErr
}
func (p *ledgerPorts) Retry(_ context.Context, id Identity, pending bool, _ error) {
	p.calls = append(p.calls, "retry")
	p.retryIdentity = id
	p.retryPending = pending
}
func (p *ledgerPorts) Acknowledge(context.Context, Identity) { p.calls = append(p.calls, "ack") }
func (p *ledgerPorts) Compact(context.Context)               { p.calls = append(p.calls, "compact") }
func (p *ledgerPorts) Commit(_ context.Context, c Command) error {
	p.calls = append(p.calls, "commit")
	p.committed = c
	if p.afterCommit != nil {
		p.afterCommit()
	}
	return p.commitErr
}
func (p *ledgerPorts) Repair(_ context.Context, c Command) error {
	p.calls = append(p.calls, "price")
	if p.pricingErr == nil && !p.unresolved {
		c.(*ledgerCommand).pending = false
	}
	return p.pricingErr
}
func (p *ledgerPorts) Invalidate(context.Context, Command) error {
	p.calls = append(p.calls, "invalidate")
	return p.invalidationErr
}
func (p *ledgerPorts) ledger() Ledger {
	return Ledger{Journal: p, Transaction: p, Pricing: p, Invalidator: p}
}
func newLedgerPorts() *ledgerPorts {
	id := Identity{"stable-root", 7}
	return &ledgerPorts{command: &ledgerCommand{id: id, usage: true}, events: []Event{{Identity: id, Payload: []byte("frozen-v1")}}}
}
func wantCalls(t *testing.T, p *ledgerPorts, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(p.calls, want) {
		t.Fatalf("calls %v, want %v", p.calls, want)
	}
}

func TestLedgerLiveSettlementFailureWindows(t *testing.T) {
	failure := errors.New("storage failed")
	for _, tc := range []struct {
		name  string
		setup func(*ledgerPorts)
		want  []string
		err   error
	}{
		{"complete", func(*ledgerPorts) {}, []string{"record", "commit", "invalidate", "ack"}, nil},
		{"journal failure prevents debit", func(p *ledgerPorts) { p.recordErr = failure }, []string{"record"}, failure},
		{"pending price remains durable", func(p *ledgerPorts) { p.command.pending = true }, []string{"record"}, ErrPricingPending},
		{"rollback retains durable recovery", func(p *ledgerPorts) { p.commitErr = failure }, []string{"record", "commit", "retry"}, failure},
		{"cache failure keeps settled payload", func(p *ledgerPorts) { p.invalidationErr = failure }, []string{"record", "commit", "invalidate"}, nil},
		{"missing facts never debit", func(p *ledgerPorts) { p.command.usage = false }, nil, ErrUsageRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newLedgerPorts()
			tc.setup(p)
			err := p.ledger().Settle(context.Background(), p.command)
			if !errors.Is(err, tc.err) {
				t.Fatalf("error %v, want %v", err, tc.err)
			}
			wantCalls(t, p, tc.want...)
		})
	}
}

func TestLedgerRecoveryRetainsUnsettleableEvidence(t *testing.T) {
	failure := errors.New("unavailable")
	for _, tc := range []struct {
		name  string
		setup func(*ledgerPorts)
		calls []string
	}{
		{"corrupt or unsupported snapshot", func(p *ledgerPorts) { p.decodeErr = failure }, []string{"claim", "compact", "decode", "retry"}},
		{"price unavailable", func(p *ledgerPorts) { p.command.pending = true; p.pricingErr = ErrPricingPending }, []string{"claim", "compact", "decode", "price", "retry"}},
		{"successful hook leaves price pending", func(p *ledgerPorts) { p.command.pending = true; p.unresolved = true }, []string{"claim", "compact", "decode", "price", "retry"}},
		{"freeze failed", func(p *ledgerPorts) { p.command.pending = true; p.freezeErr = failure }, []string{"claim", "compact", "decode", "price", "freeze"}},
		{"other worker already acknowledged", func(p *ledgerPorts) { p.command.pending = true; p.clearFrozen = true }, []string{"claim", "compact", "decode", "price", "freeze"}},
		{"saved price still pending", func(p *ledgerPorts) {
			p.command.pending = true
			p.frozen = &ledgerCommand{id: p.command.id, usage: true, pending: true}
		}, []string{"claim", "compact", "decode", "price", "freeze", "retry"}},
		{"transaction rolled back", func(p *ledgerPorts) { p.commitErr = failure }, []string{"claim", "compact", "decode", "commit", "retry"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newLedgerPorts()
			tc.setup(p)
			n, err := p.ledger().Recover(context.Background(), 16)
			if err != nil || n != 0 {
				t.Fatalf("recover %d: %v", n, err)
			}
			wantCalls(t, p, tc.calls...)
		})
	}
}

func TestLedgerRecoveryUsesPersistedWinningPrice(t *testing.T) {
	p := newLedgerPorts()
	p.command.pending = true
	p.command.price = 99
	winner := &ledgerCommand{id: p.command.id, usage: true, price: 3}
	p.frozen = winner
	n, err := p.ledger().Recover(context.Background(), 1000)
	if err != nil || n != 1 {
		t.Fatalf("recover %d: %v", n, err)
	}
	if p.limit != 64 || p.committed != winner {
		t.Fatalf("limit=%d command=%v", p.limit, p.committed)
	}
	wantCalls(t, p, "claim", "compact", "decode", "price", "freeze", "commit", "invalidate", "ack")
}

func TestLedgerRecoveryWithoutHooksKeepsEvidence(t *testing.T) {
	p := newLedgerPorts()
	p.command.pending = true
	l := p.ledger()
	l.Pricing = nil
	n, err := l.Recover(context.Background(), 1)
	if n != 0 || err != nil || !p.retryPending {
		t.Fatalf("%d %v pending=%v", n, err, p.retryPending)
	}
	wantCalls(t, p, "claim", "compact", "decode", "retry")
	p = newLedgerPorts()
	l = p.ledger()
	l.Invalidator = nil
	n, err = l.Recover(context.Background(), 1)
	if n != 1 || err != nil {
		t.Fatalf("%d %v", n, err)
	}
	wantCalls(t, p, "claim", "compact", "decode", "commit")
}

func TestLedgerRecoveryCancellationLeavesNextEventClaimed(t *testing.T) {
	p := newLedgerPorts()
	p.events = append(p.events, p.events[0])
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.afterCommit = cancel
	n, err := p.ledger().Recover(ctx, 16)
	if n != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("%d %v", n, err)
	}
	wantCalls(t, p, "claim", "compact", "decode", "commit", "invalidate", "ack")
}

func TestLedgerRecoveryClaimBoundaries(t *testing.T) {
	p := newLedgerPorts()
	n, err := p.ledger().Recover(context.Background(), 0)
	if n != 0 || err != nil || len(p.calls) != 0 {
		t.Fatal("zero limit performed work")
	}
	p.claimErr = errors.New("database down")
	n, err = p.ledger().Recover(context.Background(), 1)
	if n != 0 || !errors.Is(err, p.claimErr) {
		t.Fatalf("%d %v", n, err)
	}
	wantCalls(t, p, "claim")
}
