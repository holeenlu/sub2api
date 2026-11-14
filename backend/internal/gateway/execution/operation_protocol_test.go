package execution

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

type operationProtocolPrepared struct {
	identity model.PreparedIdentity
	target   model.AttemptTarget
	opens    *int
}

func (p *operationProtocolPrepared) Identity() model.PreparedIdentity { return p.identity }
func (p *operationProtocolPrepared) Target() model.AttemptTarget      { return p.target }
func (p *operationProtocolPrepared) Close() error                     { return nil }
func (p *operationProtocolPrepared) Reprepare(context.Context) (PreparedAttempt, error) {
	return nil, errors.New("unexpected transport reprepare")
}
func (p *operationProtocolPrepared) Open(ctx context.Context, permit *SendPermit) (model.SendState, PreRequestFailure, error) {
	if err := permit.Consume(ctx, p.identity); err != nil {
		return model.SendNotSent, PreRequestFailure{}, err
	}
	*p.opens++
	return model.SendSent, PreRequestFailure{}, nil
}

func TestOperationProtocolRecoveryUsesOriginalPermitBudget(t *testing.T) {
	d, identity, target := dispatchFixture(t, t.Context(), nil)
	o, err := NewOperation(d, model.OperationResult{OperationID: identity.OperationID})
	require.NoError(t, err)
	callbacks, opens, retries := 0, 0, 0
	err = o.RunProtocolAttempts(t.Context(), func() ProtocolAttemptResult {
		callbacks++
		wire := identity
		wire.WireDigest[0] = byte(callbacks)
		_, _, err := o.OpenPrepared(t.Context(), &operationProtocolPrepared{identity: wire, target: target, opens: &opens}, &retries, func(context.Context) error {
			t.Fatal("wire repair must not become a TLS retry")
			return nil
		})
		if err != nil {
			return ProtocolAttemptResult{Err: err}
		}
		return ProtocolAttemptResult{ReprepareReason: "existing rejected-field decision"}
	})
	require.ErrorIs(t, err, ErrTargetBudgetExhausted)
	require.Equal(t, 3, callbacks)
	require.Equal(t, 2, opens)
	require.Equal(t, opens, d.budget.Snapshot().Attempts)
	require.Len(t, o.Snapshot().Attempts, opens)
	require.Zero(t, retries)
}

func TestOperationProtocolRecoveryStopsOnCommitOrOriginalCancellation(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "original_cancel", true: "actual_delivery"}[committed], func(t *testing.T) {
			control, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			d, identity, target := dispatchFixture(t, control, nil)
			o, err := NewOperation(d, model.OperationResult{OperationID: identity.OperationID})
			require.NoError(t, err)
			calls, opens, retries := 0, 0, 0
			cause := errors.New("original operation canceled")
			err = o.RunProtocolAttempts(context.WithoutCancel(control), func() ProtocolAttemptResult {
				calls++
				_, sequence, err := o.OpenPrepared(control, &operationProtocolPrepared{identity: identity, target: target, opens: &opens}, &retries, func(context.Context) error { return nil })
				require.NoError(t, err)
				if committed {
					o.Delivered(sequence, model.DeliveryReceipt{BytesWritten: 3, BusinessCommitted: true})
				} else {
					cancel(cause)
				}
				return ProtocolAttemptResult{ReprepareReason: "existing compact fallback decision"}
			})
			if committed {
				require.ErrorIs(t, err, ErrOperationCommitted)
			} else {
				require.ErrorIs(t, err, cause)
			}
			require.Equal(t, 1, calls)
			require.Equal(t, 1, opens)
		})
	}
}

func TestOperationProtocolFailurePreservesCauseWithoutAnotherAttempt(t *testing.T) {
	d, identity, _ := dispatchFixture(t, t.Context(), nil)
	o, err := NewOperation(d, model.OperationResult{OperationID: identity.OperationID})
	require.NoError(t, err)
	want := errors.New("real protocol conversion failure")
	calls := 0
	err = o.RunProtocolAttempts(t.Context(), func() ProtocolAttemptResult {
		calls++
		return ProtocolAttemptResult{Err: want}
	})
	require.ErrorIs(t, err, want)
	require.Equal(t, 1, calls)
	require.Zero(t, d.budget.Snapshot().Attempts)
}
