package execution

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

type dispatchAuthorityFunc func(context.Context, model.PreparedIdentity) (DispatchValidation, error)

func (f dispatchAuthorityFunc) RevalidateDispatch(ctx context.Context, i model.PreparedIdentity) (DispatchValidation, error) {
	return f(ctx, i)
}

func dispatchFixture(t *testing.T, ctx context.Context, gate DispatchAuthority) (*Dispatcher, model.PreparedIdentity, model.AttemptTarget) {
	t.Helper()
	i := model.PreparedIdentity{OperationID: "operation-a", ResourceID: 7, Owner: model.ResourceOwner{AccountID: 7}, UserID: 3, APIKeyID: 4,
		Mode: "native", Transport: "http", GrantDigest: [32]byte{1}, TargetDigest: [32]byte{2}, WireDigest: [32]byte{3}, BodyKind: model.BodyReplayable, BodyDigest: [32]byte{4}}
	budget := NewAttemptBudget(AttemptLimits{Total: 8, PerOwner: 3, Preparation: 2, RetryWait: time.Second})
	if gate == nil {
		gate = dispatchAuthorityFunc(func(context.Context, model.PreparedIdentity) (DispatchValidation, error) {
			return DispatchValidation{Authorization: [32]byte{1}, Leases: [32]byte{2}}, nil
		})
	}
	d, err := NewDispatcher(ctx, i.OperationID, budget, NewFirstOutput(time.Now()), gate)
	require.NoError(t, err)
	return d, i, model.AttemptTarget{Owner: i.Owner, Mode: "http", Limit: 2}
}

func TestDispatcherRejectsMissingAuthorityAndUnknownIdentity(t *testing.T) {
	d, identity, target := dispatchFixture(t, context.Background(), nil)
	var nilFunc dispatchAuthorityFunc
	_, err := NewDispatcher(context.Background(), "operation-a", d.budget, d.output, nilFunc)
	require.ErrorIs(t, err, ErrDispatchUnavailable)
	_, err = d.Admit(context.Background(), model.PreparedIdentity{}, target)
	require.ErrorIs(t, err, ErrInvalidPreparedIdentity)
	wrongOwner := target
	wrongOwner.Owner.AccountID++
	_, err = d.Admit(context.Background(), identity, wrongOwner)
	require.ErrorIs(t, err, ErrInvalidPreparedIdentity)
	identity.OperationID = "another-operation"
	_, err = d.Admit(context.Background(), identity, target)
	require.ErrorIs(t, err, ErrInvalidPreparedIdentity)
	require.Zero(t, d.budget.Snapshot().Attempts)
	var empty SendPermit
	require.ErrorIs(t, empty.Consume(context.Background(), identity), ErrDispatchUnavailable)
}

func TestDispatcherRejectionDoesNotSpendAndKeepsErrorIdentity(t *testing.T) {
	denied := errors.New("authority revoked")
	d, identity, target := dispatchFixture(t, context.Background(), dispatchAuthorityFunc(func(context.Context, model.PreparedIdentity) (DispatchValidation, error) {
		return DispatchValidation{}, denied
	}))
	_, err := d.Admit(context.Background(), identity, target)
	require.ErrorIs(t, err, denied)
	require.Zero(t, d.budget.Snapshot().Attempts)
	d.authority = dispatchAuthorityFunc(func(context.Context, model.PreparedIdentity) (DispatchValidation, error) {
		return DispatchValidation{}, nil
	})
	_, err = d.Admit(context.Background(), identity, target)
	require.ErrorIs(t, err, ErrDispatchUnavailable)
	require.Zero(t, d.budget.Snapshot().Attempts)
}

func TestSendPermitCopyAndConcurrentConsumptionShareOneRight(t *testing.T) {
	var checks atomic.Int64
	d, identity, target := dispatchFixture(t, context.Background(), dispatchAuthorityFunc(func(context.Context, model.PreparedIdentity) (DispatchValidation, error) {
		checks.Add(1)
		return DispatchValidation{Authorization: [32]byte{1}, Leases: [32]byte{2}}, nil
	}))
	permit, err := d.Admit(context.Background(), identity, target)
	require.NoError(t, err)
	copyOfPermit := *permit
	var success atomic.Int64
	var repeated atomic.Int64
	var wg sync.WaitGroup
	for index := range 24 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			p := permit
			if index%2 == 0 {
				p = &copyOfPermit
			}
			err := p.Consume(context.Background(), identity)
			if err == nil {
				success.Add(1)
			} else if errors.Is(err, ErrSendPermitConsumed) {
				repeated.Add(1)
			}
		}(index)
	}
	wg.Wait()
	require.EqualValues(t, 1, success.Load())
	require.EqualValues(t, 23, repeated.Load())
	require.EqualValues(t, 2, checks.Load(), "admission and the sole successful Open revalidate")
	require.Equal(t, 1, d.budget.Snapshot().Attempts)
	require.EqualValues(t, 1, permit.AttemptSequence())
}

func TestSendPermitBindsEveryPreparedField(t *testing.T) {
	changes := map[string]func(*model.PreparedIdentity){
		"operation": func(i *model.PreparedIdentity) { i.OperationID = "other" },
		"principal": func(i *model.PreparedIdentity) { i.APIKeyID++ },
		"resource":  func(i *model.PreparedIdentity) { i.ResourceID++ },
		"grant":     func(i *model.PreparedIdentity) { i.GrantDigest[0]++ },
		"target":    func(i *model.PreparedIdentity) { i.TargetDigest[0]++ },
		"wire":      func(i *model.PreparedIdentity) { i.WireDigest[0]++ },
		"body":      func(i *model.PreparedIdentity) { i.BodyDigest[0]++ },
		"mode":      func(i *model.PreparedIdentity) { i.Mode = "bps" },
		"transport": func(i *model.PreparedIdentity) { i.Transport = "websocket" },
	}
	for name, mutate := range changes {
		t.Run(name, func(t *testing.T) {
			d, identity, target := dispatchFixture(t, context.Background(), nil)
			permit, err := d.Admit(context.Background(), identity, target)
			require.NoError(t, err)
			changed := identity
			mutate(&changed)
			require.ErrorIs(t, permit.Consume(context.Background(), changed), ErrInvalidPreparedIdentity)
			require.ErrorIs(t, permit.Consume(context.Background(), identity), ErrSendPermitConsumed)
		})
	}
}

func TestSendPermitRevalidatesCancellationRevocationAndLeases(t *testing.T) {
	for _, scenario := range []string{"original cancellation", "caller cancellation", "authorization change", "lease replacement", "committed output"} {
		t.Run(scenario, func(t *testing.T) {
			original, cancel := context.WithCancel(context.Background())
			defer cancel()
			stamp := DispatchValidation{Authorization: [32]byte{1}, Leases: [32]byte{2}}
			d, identity, target := dispatchFixture(t, original, dispatchAuthorityFunc(func(context.Context, model.PreparedIdentity) (DispatchValidation, error) { return stamp, nil }))
			permit, err := d.Admit(context.Background(), identity, target)
			require.NoError(t, err)
			ctx := context.Background()
			want := ErrDispatchChanged
			switch scenario {
			case "original cancellation":
				cancel()
				want = context.Canceled
			case "caller cancellation":
				var done context.CancelFunc
				ctx, done = context.WithCancel(ctx)
				done()
				want = context.Canceled
			case "authorization change":
				stamp.Authorization[0]++
			case "lease replacement":
				stamp.Leases[0]++
			case "committed output":
				d.output.Commit()
				want = ErrDispatchOutputCommitted
			}
			require.ErrorIs(t, permit.Consume(ctx, identity), want)
		})
	}
}

func TestSendPermitDoesNotExtendOldDeadlineDuringRevalidation(t *testing.T) {
	now := time.Now()
	initial := now.Add(time.Second)
	calls := 0
	d, identity, target := dispatchFixture(t, context.Background(), dispatchAuthorityFunc(func(context.Context, model.PreparedIdentity) (DispatchValidation, error) {
		calls++
		deadline := initial
		if calls == 2 {
			now = initial
			deadline = initial.Add(time.Hour)
		}
		return DispatchValidation{Authorization: [32]byte{1}, Leases: [32]byte{2}, ValidUntil: deadline}, nil
	}))
	d.now = func() time.Time { return now }
	permit, err := d.Admit(context.Background(), identity, target)
	require.NoError(t, err)
	require.ErrorIs(t, permit.Consume(context.Background(), identity), ErrDispatchExpired)
}

func TestDispatcherUsesExistingOwnerAndTargetBudgets(t *testing.T) {
	d, identity, target := dispatchFixture(t, context.Background(), nil)
	for range 2 {
		_, err := d.Admit(context.Background(), identity, target)
		require.NoError(t, err)
	}
	_, err := d.Admit(context.Background(), identity, target)
	require.ErrorIs(t, err, ErrTargetBudgetExhausted)
	target.Endpoint = "changed-preparation"
	_, err = d.Admit(context.Background(), identity, target)
	require.NoError(t, err)
	target.Endpoint = "third-preparation"
	_, err = d.Admit(context.Background(), identity, target)
	require.ErrorIs(t, err, ErrTargetBudgetExhausted)
	require.Equal(t, 3, d.budget.Snapshot().Attempts)
}

func TestPreparedBodyIdentityDistinguishesUnknownFromEmpty(t *testing.T) {
	_, identity, _ := dispatchFixture(t, context.Background(), nil)
	require.True(t, identity.Valid())
	identity.BodyKind = model.BodyUnknown
	require.False(t, identity.Valid())
	identity.BodyKind = model.BodyExclusiveStream
	require.False(t, identity.Valid())
	identity.BodyDigest = [32]byte{}
	identity.BodyHandle = [16]byte{1}
	require.True(t, identity.Valid())
	identity.BodyDigest[0] = 1
	require.False(t, identity.Valid(), "a stream must not claim a fabricated known digest")
}
