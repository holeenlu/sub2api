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

func pushOperationFixture(t *testing.T) (*Operation, *Dispatcher) {
	t.Helper()
	dispatch, identity, _ := dispatchFixture(t, t.Context(), nil)
	op, err := NewOperation(dispatch, model.OperationResult{OperationID: identity.OperationID})
	require.NoError(t, err)
	return op, dispatch
}

func TestOperationBeginHasOneOwnerAndDoesNotGrantOrDebit(t *testing.T) {
	op, dispatch := pushOperationFixture(t)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := op.Begin(t.Context())
			if err == nil {
				accepted.Add(1)
			} else {
				require.ErrorIs(t, err, ErrOperationConsumed)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, accepted.Load())
	require.Zero(t, dispatch.budget.Snapshot().Attempts)
	require.Zero(t, dispatch.budget.Snapshot().PreparationRetries)
	require.Equal(t, model.OperationWaiting, op.Snapshot().State)
	require.NoError(t, op.Finish(t.Context(), nil))
	require.Equal(t, model.OperationCompleted, op.Snapshot().State)
	require.ErrorIs(t, op.Begin(t.Context()), ErrOperationConsumed)
}

func TestOperationFinishRetiresIssuedAndConsumedSendPermissions(t *testing.T) {
	for _, consumed := range []bool{false, true} {
		t.Run(map[bool]string{false: "issued", true: "consumed"}[consumed], func(t *testing.T) {
			dispatch, identity, target := dispatchFixture(t, t.Context(), nil)
			op, err := NewOperation(dispatch, model.OperationResult{OperationID: identity.OperationID})
			require.NoError(t, err)
			require.NoError(t, op.Begin(t.Context()))
			permit, err := dispatch.Admit(t.Context(), identity, target)
			require.NoError(t, err)
			if consumed {
				require.NoError(t, permit.Consume(t.Context(), identity))
			}
			require.NoError(t, op.Finish(t.Context(), nil))
			require.NoError(t, dispatch.control.Err(), "normal completion must not simulate a disconnected client")
			if consumed {
				require.ErrorIs(t, permit.RevalidateConsumed(t.Context(), identity), ErrOperationConsumed)
			} else {
				require.ErrorIs(t, permit.Consume(t.Context(), identity), ErrOperationConsumed)
			}
			before := dispatch.budget.Snapshot().Attempts
			_, err = dispatch.Admit(t.Context(), identity, target)
			require.ErrorIs(t, err, ErrOperationConsumed)
			opens, retries, callbacks := 0, 0, 0
			_, _, err = op.OpenPrepared(t.Context(), &operationProtocolPrepared{identity: identity, target: target, opens: &opens}, &retries, func(context.Context) error { return nil })
			require.ErrorIs(t, err, ErrOperationConsumed)
			require.ErrorIs(t, op.RunProtocolAttempts(t.Context(), func() ProtocolAttemptResult { callbacks++; return ProtocolAttemptResult{} }), ErrOperationConsumed)
			require.Zero(t, opens)
			require.Zero(t, callbacks)
			require.Equal(t, before, dispatch.budget.Snapshot().Attempts)
			require.Equal(t, model.OperationCompleted, op.Snapshot().State)
		})
	}
}

func TestOperationFinishRejectsAdmissionAlreadyWaitingOnAuthority(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	dispatch, identity, target := dispatchFixture(t, t.Context(), dispatchAuthorityFunc(func(context.Context, model.PreparedIdentity) (DispatchValidation, error) {
		close(entered)
		<-release
		return DispatchValidation{Authorization: [32]byte{1}, Leases: [32]byte{2}}, nil
	}))
	op, err := NewOperation(dispatch, model.OperationResult{OperationID: identity.OperationID})
	require.NoError(t, err)
	require.NoError(t, op.Begin(t.Context()))
	opens, retries := 0, 0
	done := make(chan error, 1)
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("retired admission worker did not join")
		}
	})
	go func() {
		defer close(done)
		_, _, err := op.OpenPrepared(t.Context(), &operationProtocolPrepared{identity: identity, target: target, opens: &opens}, &retries, func(context.Context) error { return nil })
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("admission did not reach the real authority")
	}
	require.NoError(t, op.Finish(t.Context(), nil))
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrOperationConsumed)
	case <-time.After(3 * time.Second):
		t.Fatal("retired admission did not join")
	}
	require.Zero(t, opens)
	require.Zero(t, dispatch.budget.Snapshot().Attempts)
	require.Empty(t, op.Snapshot().Attempts)
	require.Equal(t, model.OperationCompleted, op.Snapshot().State)
}

func TestOperationFinishKeepsFirstTerminalAndCancellationCause(t *testing.T) {
	op, _ := pushOperationFixture(t)
	require.ErrorIs(t, op.Finish(t.Context(), nil), ErrOperationConsumed)
	require.NoError(t, op.Begin(t.Context()))
	failure := errors.New("actual transport failure")
	canceled := errors.New("actual owner cancellation")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(canceled)
	err := op.Finish(ctx, failure)
	require.ErrorIs(t, err, failure)
	require.ErrorIs(t, err, canceled)
	require.Equal(t, model.OperationCanceled, op.Snapshot().State)
	require.Equal(t, err, op.Finish(t.Context(), nil), "a repeated callback cannot rewrite completion")
	require.ErrorIs(t, op.Snapshot().Failure, failure)
}

func TestOperationExecuteAndPushBeginCannotCreateTwoOwners(t *testing.T) {
	op, _ := pushOperationFixture(t)
	require.NoError(t, op.Begin(t.Context()))
	calls := 0
	err := op.Execute(t.Context(), func() CycleResult { calls++; return CycleResult{} })
	require.ErrorIs(t, err, ErrOperationConsumed)
	require.Zero(t, calls)
	require.NoError(t, op.Finish(t.Context(), nil))
}

func TestOperationCanceledBeginIsTerminalWithoutRunningCycle(t *testing.T) {
	op, _ := pushOperationFixture(t)
	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("lane canceled before admission")
	cancel(cause)
	require.ErrorIs(t, op.Begin(ctx), cause)
	require.Equal(t, model.OperationCanceled, op.Snapshot().State)
	require.ErrorIs(t, op.Finish(t.Context(), nil), cause)
	require.ErrorIs(t, op.Begin(t.Context()), ErrOperationConsumed)
}
