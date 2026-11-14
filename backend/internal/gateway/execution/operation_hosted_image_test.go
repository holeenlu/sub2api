package execution

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

func TestOperationHostedImageMeasurementsRetainPresence(t *testing.T) {
	for _, value := range []int64{-1, 0, 8} {
		d, id, target := dispatchFixture(t, t.Context(), nil)
		o, err := NewOperation(d, model.OperationResult{OperationID: id.OperationID})
		require.NoError(t, err)
		opens, retries := 0, 0
		_, sequence, err := o.OpenPrepared(t.Context(), &operationProtocolPrepared{identity: id, target: target, opens: &opens}, &retries, nil)
		require.NoError(t, err)
		raw := &model.TokenMeasurement{Value: value}
		err = o.Observe(sequence, model.FactDelta{Usage: &model.UsageFact{HostedImageOutput: raw, Confidence: model.UsageFinal, TerminalConfirmed: true}})
		if value < 0 {
			require.ErrorIs(t, err, ErrOperationFacts)
			require.Nil(t, o.Snapshot().Attempts[0].Usage)
			continue
		}
		require.NoError(t, err)
		raw.Value = 99
		saved := o.Snapshot().Attempts[0].Usage
		require.Equal(t, value, saved.HostedImageOutput.Value)
		require.Nil(t, saved.ImageOutput)
		require.Nil(t, saved.Input)
		require.Nil(t, saved.Output)
		saved.HostedImageOutput.Value = 100
		require.Equal(t, value, o.Snapshot().Attempts[0].Usage.HostedImageOutput.Value)
	}
}
