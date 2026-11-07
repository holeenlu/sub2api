package execution

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

var (
	ErrInvalidPreparedIdentity = errors.New("prepared dispatch identity is invalid")
	ErrDispatchUnavailable     = errors.New("dispatch authority is unavailable")
	ErrDispatchExpired         = errors.New("dispatch permission expired")
	ErrDispatchChanged         = errors.New("dispatch authorization or lease changed after preparation")
	ErrSendPermitConsumed      = errors.New("send permit was already consumed")
	ErrDispatchOutputCommitted = errors.New("operation output already committed")
)

// DispatchValidation is produced only by the composition root's live authority.
// Authorization and Leases are fingerprints of the actual checked scope/grant
// and owned capacity/owner tokens. An explicitly unlimited capacity policy has
// its own checked fingerprint; zero does not mean no checks are necessary.
// ValidUntil is the earliest authority/lease deadline, zero only when the
// verified policy has no deadline beyond the operation lifetime.
type DispatchValidation struct {
	// Purpose must be independently approved by the root registry. Setting the
	// prepared identity alone cannot switch the debit to preparation.
	Purpose       model.DispatchPurpose
	Authorization [32]byte
	Leases        [32]byte
	ValidUntil    time.Time
}

// DispatchAuthority rechecks current user/key, scope, grant and lease ownership.
// Implementations belong at trusted application/storage boundaries. Providers
// receive a concrete SendPermit, never a caller-supplied authorization callback.
type DispatchAuthority interface {
	RevalidateDispatch(context.Context, model.PreparedIdentity) (DispatchValidation, error)
}

// Dispatcher owns permission issuance for one operation and shares its existing
// attempt budget and first-output clock. It never creates a second allowance,
// selects an account, transmits data, or decides recovery. It is concurrency-safe.
type Dispatcher struct {
	control           context.Context
	operationID       string
	budget            *AttemptBudget
	output            *FirstOutput
	authority         DispatchAuthority
	mode              string
	authorityVersion  uint64
	recoverySelection bool
	mu                sync.Mutex
	sequence          uint64
	now               func() time.Time
	stopped           atomic.Bool
}

// NewDispatcher requires the operation's actual lifetime, budget and authority.
// A nil dependency is rejected rather than replaced by an unbounded/default grant.
func NewDispatcher(control context.Context, operationID string, budget *AttemptBudget, output *FirstOutput, authority DispatchAuthority) (*Dispatcher, error) {
	if control == nil || operationID == "" || budget == nil || output == nil || nilDispatchAuthority(authority) {
		return nil, ErrDispatchUnavailable
	}
	return &Dispatcher{control: control, operationID: operationID, budget: budget, output: output, authority: authority, now: time.Now}, nil
}

func nilDispatchAuthority(authority any) bool {
	if authority == nil {
		return true
	}
	value := reflect.ValueOf(authority)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Admit revalidates the sealed preparation and spends the existing allowance
// exactly once. The recovery target is separate from the final wire fingerprint;
// repairing a wire body must not reset the physical-owner/operation budget.
func (d *Dispatcher) Admit(ctx context.Context, identity model.PreparedIdentity, target model.AttemptTarget) (*SendPermit, error) {
	if d == nil || !identity.Valid() || identity.OperationID != d.operationID || target.Owner != identity.Owner {
		return nil, ErrInvalidPreparedIdentity
	}
	if err := d.check(ctx); err != nil {
		return nil, err
	}
	authority, version, err := d.dispatchAuthority(identity)
	if err != nil {
		return nil, err
	}
	validation, err := authority.RevalidateDispatch(ctx, identity)
	if err != nil {
		return nil, err
	}
	if validation.Purpose != identity.Purpose {
		return nil, ErrInvalidPreparedIdentity
	}
	if err := validDispatch(validation, d.now()); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if version != d.authorityVersion {
		return nil, ErrDispatchChanged
	}
	if err := d.check(ctx); err != nil {
		return nil, err
	}
	deadline := earlierDeadline(d.output.Snapshot().Deadline, validation.ValidUntil)
	if !deadline.IsZero() && !d.now().Before(deadline) {
		return nil, ErrDispatchExpired
	}
	var debitErr error
	switch identity.Purpose {
	case model.DispatchGeneration:
		debitErr = d.budget.Take(&target)
	case model.DispatchInternalPreparation:
		debitErr = d.budget.TakePreparation()
	case model.DispatchControl:
		// Initial non-generating controls do not spend generation or retry
		// allowances. Their provider's existing batch/size bounds still apply.
	default:
		return nil, ErrInvalidPreparedIdentity
	}
	if debitErr != nil {
		return nil, debitErr
	}
	d.sequence++
	return &SendPermit{state: &sendPermitState{owner: d, identity: identity, validation: validation, deadline: deadline, sequence: d.sequence}}, nil
}

func (d *Dispatcher) check(ctx context.Context) error {
	if d.stopped.Load() {
		return ErrOperationConsumed
	}
	if ctx == nil {
		return ErrDispatchUnavailable
	}
	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	if err := d.control.Err(); err != nil {
		return context.Cause(d.control)
	}
	output := d.output.Snapshot()
	if output.Committed {
		return ErrDispatchOutputCommitted
	}
	deadline := output.Deadline
	if !deadline.IsZero() && !d.now().Before(deadline) {
		return ErrDispatchExpired
	}
	return nil
}

// Only the operation owner retires admission. The control context continues
// serving terminal metadata and the next client turn; it is not canceled to
// represent normal completion. Issued permits recheck this same gate.
func (d *Dispatcher) stop() {
	d.mu.Lock()
	d.stopped.Store(true)
	d.mu.Unlock()
}

func validDispatch(v DispatchValidation, now time.Time) error {
	if v.Authorization == ([32]byte{}) || v.Leases == ([32]byte{}) {
		return ErrDispatchUnavailable
	}
	if !v.ValidUntil.IsZero() && !now.Before(v.ValidUntil) {
		return ErrDispatchExpired
	}
	return nil
}

func earlierDeadline(a, b time.Time) time.Time {
	if a.IsZero() || !b.IsZero() && b.Before(a) {
		return b
	}
	return a
}

type sendPermitState struct {
	owner      *Dispatcher
	identity   model.PreparedIdentity
	validation DispatchValidation
	mu         sync.Mutex
	deadline   time.Time
	sequence   uint64
	consumed   atomic.Bool
	validated  atomic.Bool
}

// SendPermit is an execution-issued, one-shot capability. Copying the value
// shares its atomic consumption state. Its zero value grants nothing.
type SendPermit struct{ state *sendPermitState }

// AttemptSequence is an observation for correlation, not dispatch authority.
func (p *SendPermit) AttemptSequence() uint64 {
	if p == nil || p.state == nil {
		return 0
	}
	return p.state.sequence
}

// SendDeadline is the original deadline for beginning this send, not a response
// or stream deadline. A dial/pool wait cannot extend a consumed permission.
// Once headers have been written, the operation's existing first-output and
// live lease owners govern receiving; this deadline must not truncate a valid
// long response whose lease has since been renewed.
func (p *SendPermit) SendDeadline() time.Time {
	if p == nil || p.state == nil {
		return time.Time{}
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	return p.state.deadline
}

// Consume is called at the provider's actual Open boundary. Every invocation,
// even a rejected mismatch, consumes this permission. Original cancellation,
// current deadlines and live scope/lease proofs are rechecked before returning.
// A successful check cannot retract an already-sent request on a later revoke.
func (p *SendPermit) Consume(ctx context.Context, sealed model.PreparedIdentity) error {
	if p == nil || p.state == nil {
		return ErrDispatchUnavailable
	}
	s := p.state
	if !s.consumed.CompareAndSwap(false, true) {
		return ErrSendPermitConsumed
	}
	if err := p.revalidate(ctx, sealed); err != nil {
		return err
	}
	s.validated.Store(true)
	return nil
}

// RevalidateConsumed checks the same owned attempt after connection/pool waits.
// It cannot consume another allowance, resurrect a rejected Consume, or grant
// another send. The provider still fences writes with its single Open owner.
func (p *SendPermit) RevalidateConsumed(ctx context.Context, sealed model.PreparedIdentity) error {
	if p == nil || p.state == nil || !p.state.validated.Load() {
		return ErrDispatchUnavailable
	}
	return p.revalidate(ctx, sealed)
}

func (p *SendPermit) revalidate(ctx context.Context, sealed model.PreparedIdentity) error {
	s := p.state
	if sealed != s.identity {
		return ErrInvalidPreparedIdentity
	}
	if err := s.owner.check(ctx); err != nil {
		return err
	}
	if deadline := p.SendDeadline(); !deadline.IsZero() && !s.owner.now().Before(deadline) {
		return ErrDispatchExpired
	}
	authority, _, err := s.owner.dispatchAuthority(sealed)
	if err != nil {
		return err
	}
	current, err := authority.RevalidateDispatch(ctx, sealed)
	if err != nil {
		return err
	}
	if err := validDispatch(current, s.owner.now()); err != nil {
		return err
	}
	if current.Purpose != sealed.Purpose || current.Purpose != s.validation.Purpose || current.Authorization != s.validation.Authorization || current.Leases != s.validation.Leases {
		return ErrDispatchChanged
	}
	s.mu.Lock()
	s.deadline = earlierDeadline(s.deadline, current.ValidUntil)
	deadline := s.deadline
	s.mu.Unlock()
	if !deadline.IsZero() && !s.owner.now().Before(deadline) {
		return ErrDispatchExpired
	}
	return s.owner.check(ctx)
}
