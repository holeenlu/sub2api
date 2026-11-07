package routing

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCalcLoadSkewByMoments_Branches(t *testing.T) {
	require.Equal(t, 0.0, loadSkew(1, 1, 1))
	// variance < 0 分支：sumSquares/count - mean^2 为负值时应钳制为 0。
	require.Equal(t, 0.0, loadSkew(1, 0, 2))
	require.GreaterOrEqual(t, loadSkew(6, 20, 3), 0.0)
}

func TestScoreBatchPreservesObservedWeightsAndNeutralUnknownLatency(t *testing.T) {
	batch := []ScoreObservation{
		{LoadRate: 20, WaitingCount: 2, ErrorRate: 0.25, LatencyFactor: 0.4, QuotaHeadroom: 0.3, ResetKnown: true, ResetRemaining: 10},
		{LoadRate: 60, WaitingCount: 4, QuotaHeadroom: 0.6, ResetKnown: true, ResetRemaining: 20},
		{QuotaHeadroom: 0.5},
	}
	scores, skew := ScoreBatch(batch, ScoreWeights{Load: 1, Queue: 0.7, ErrorRate: 0.8, TTFT: 0.5, Reset: 0.2, QuotaHeadroom: 0.4})
	for i, want := range []float64{2.27, 1.69, 2.95} {
		require.InDelta(t, want, scores[i], 1e-12)
	}
	require.InDelta(t, 24.94438257849294, skew, 1e-12)
	require.Zero(t, batch[1].LatencyFactor, "snapshot stays immutable")
}

func TestScoreBatchDistinguishesMissingEqualAndDisabledResetWindows(t *testing.T) {
	batch := []ScoreObservation{{ResetKnown: true, ResetRemaining: 10}, {ResetKnown: true, ResetRemaining: 10}, {}}
	scores, _ := ScoreBatch(batch, ScoreWeights{Reset: 1})
	require.Equal(t, []float64{1, 1, 0}, scores)
	scores, _ = ScoreBatch(batch, ScoreWeights{})
	require.Equal(t, []float64{0, 0, 0}, scores)
	scores, skew := ScoreBatch(nil, ScoreWeights{Reset: 1})
	require.Nil(t, scores)
	require.Zero(t, skew)
}
