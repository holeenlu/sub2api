package service

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelTraceGPTReferenceParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/modeltrace_gpt_reference.json")
	require.NoError(t, err)
	var samples []struct {
		Model         string `json:"model"`
		ExpectedCount int    `json:"expected_count"`
		ParsedCount   int    `json:"parsed_count"`
		Text          string `json:"text"`
	}
	require.NoError(t, json.Unmarshal(raw, &samples))
	require.Len(t, samples, 8)
	for _, sample := range samples {
		t.Run(sample.Model, func(t *testing.T) {
			prediction, commit, err := ModelTracePredictCommitted(sample.Text, sample.ExpectedCount)
			require.NoError(t, err)
			require.Equal(t, sample.Model, prediction.Model)
			require.Equal(t, sample.ParsedCount, prediction.ParsedCount)
			require.Greater(t, prediction.Probability, 0.97)
			require.Equal(t, modelTraceFallbackCommit, commit)
		})
	}
}

func TestModelTraceRejectsTruncatedChallenge(t *testing.T) {
	prediction, _, err := ModelTracePredictCommitted("1 2 3 4 5", 332)
	require.ErrorContains(t, err, "insufficient_numbers")
	require.Equal(t, 5, prediction.ParsedCount)
}

func TestModelTraceBankRejectsInvalidData(t *testing.T) {
	_, err := parseModelTraceBank([]byte(`{"schema":"robust-number-fingerprint-bank","models":[{"id":"gpt-1"}]}`))
	require.Error(t, err)
}

func TestModelTraceReviewUsesValidCountCalibrationAndRejectsDisagreement(t *testing.T) {
	var bank modelTraceBank
	require.NoError(t, json.Unmarshal([]byte(`{"models":[{"id":"gpt-5.4"},{"id":"gpt-5.5"}],"calibration":{"1":{"beta":1},"2":{"beta":2},"3":{"beta":6}}}`), &bank))
	snapshot := &modelTraceVersion{bank: &bank, commit: "pinned-bank"}
	good := CodexDiagnosticItem{PredictedModel: "gpt-5.4", Status: "normal", scores: []float64{1, 0}, ParsedCount: 300, ExpectedCount: 300}
	bad := CodexDiagnosticItem{Status: "failed", Reason: "response_incomplete", ParsedCount: 2}
	for _, tc := range []struct {
		samples        []CodexDiagnosticItem
		probability    float64
		status, reason string
	}{
		{[]CodexDiagnosticItem{good, good, good}, .9975273768433653, "normal", ""},
		{[]CodexDiagnosticItem{good, good, bad}, .8807970779778823, "uncertain", "partial_samples"},
		{[]CodexDiagnosticItem{good, bad, bad}, .7310585786300049, "uncertain", "partial_samples"},
		{[]CodexDiagnosticItem{bad, bad, bad}, 0, "failed", "insufficient_numbers"},
	} {
		result := aggregateDiagnosticSamples("gpt-5.4", tc.samples, snapshot)
		require.InDelta(t, tc.probability, result.Probability, 1e-12)
		require.Equal(t, tc.status, result.Status)
		require.Equal(t, tc.reason, result.Reason)
		require.Len(t, result.Samples, 3)
		raw, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "scores")
	}
	other := good
	other.PredictedModel, other.scores = "gpt-5.5", []float64{0, 1}
	result := aggregateDiagnosticSamples("gpt-5.4", []CodexDiagnosticItem{good, good, other}, snapshot)
	require.Equal(t, "uncertain", result.Status)
	require.Equal(t, "sample_disagreement", result.Reason)
}
