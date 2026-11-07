package routing

import "math"

// ScoreWeights are the existing layer-local scheduler weights. The configuration
// owner supplies them; this package defines no new preference or global score.
type ScoreWeights struct{ Load, Queue, ErrorRate, TTFT, Reset, QuotaHeadroom float64 }

// ScoreObservation is a read-only projection of current load and health. A zero
// latency factor retains the existing neutral 0.5 behavior. ResetKnown separates
// missing windows from a real zero remaining duration. Units are seconds.
type ScoreObservation struct {
	LoadRate, WaitingCount                                  int
	ErrorRate, LatencyFactor, QuotaHeadroom, ResetRemaining float64
	ResetKnown                                              bool
}

// ScoreBatch applies the original score to one observed batch. Scheduler and
// diagnostic projection both use this function; neither maintains its own
// reset/queue normalization. The second result is the existing load deviation.
func ScoreBatch(batch []ScoreObservation, weights ScoreWeights) ([]float64, float64) {
	if len(batch) == 0 {
		return nil, 0
	}
	maxWaiting := 1
	sum, sumSquares := 0.0, 0.0
	minReset, maxReset := 0.0, 0.0
	hasReset := false
	for _, item := range batch {
		maxWaiting = max(maxWaiting, item.WaitingCount)
		load := float64(item.LoadRate)
		sum += load
		sumSquares += load * load
		if weights.Reset > 0 && item.ResetKnown {
			if !hasReset {
				minReset, maxReset = item.ResetRemaining, item.ResetRemaining
				hasReset = true
			} else {
				minReset = math.Min(minReset, item.ResetRemaining)
				maxReset = math.Max(maxReset, item.ResetRemaining)
			}
		}
	}
	scores := make([]float64, len(batch))
	for index, item := range batch {
		loadFactor := 1 - clamp01(float64(item.LoadRate)/100.0)
		queueFactor := 1 - clamp01(float64(item.WaitingCount)/float64(maxWaiting))
		errorFactor := 1 - clamp01(item.ErrorRate)
		latencyFactor := item.LatencyFactor
		if latencyFactor == 0 {
			latencyFactor = 0.5
		}
		resetFactor := 0.0
		if weights.Reset > 0 && hasReset && item.ResetKnown {
			if maxReset > minReset {
				resetFactor = 1 - clamp01((item.ResetRemaining-minReset)/(maxReset-minReset))
			} else {
				resetFactor = 1
			}
		}
		headroom := 0.0
		if weights.QuotaHeadroom > 0 {
			headroom = item.QuotaHeadroom
		}
		scores[index] = weights.Load*loadFactor + weights.Queue*queueFactor + weights.ErrorRate*errorFactor + weights.TTFT*latencyFactor + weights.Reset*resetFactor + weights.QuotaHeadroom*headroom
	}
	return scores, loadSkew(sum, sumSquares, len(batch))
}

func clamp01(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func loadSkew(sum, sumSquares float64, count int) float64 {
	if count <= 1 {
		return 0
	}
	mean := sum / float64(count)
	variance := sumSquares/float64(count) - mean*mean
	if variance < 0 {
		variance = 0
	}
	return math.Sqrt(variance)
}
