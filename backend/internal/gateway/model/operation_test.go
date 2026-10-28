package model

import "testing"

func TestCloneUsageFactPreservesIndependentImageMeasurements(t *testing.T) {
	count := int64(2)
	original := &UsageFact{Total: &TokenMeasurement{Value: 10}, Reasoning: &TokenMeasurement{Value: 0}, ImageInput: &TokenMeasurement{Value: 0}, ImageOutput: &TokenMeasurement{Value: 7}, ImageCount: &count,
		HostedImageInput: &TokenMeasurement{Value: 3}, HostedImageOutput: &TokenMeasurement{Value: 8},
		Confidence: UsageConfirmedLowerBound, TerminalConfirmed: true}
	cloned := CloneUsageFact(original)
	if cloned.Total == original.Total || cloned.Reasoning == original.Reasoning || cloned.ImageInput == original.ImageInput || cloned.ImageOutput == original.ImageOutput || cloned.HostedImageInput == original.HostedImageInput || cloned.HostedImageOutput == original.HostedImageOutput || cloned.ImageCount == original.ImageCount {
		t.Fatal("image measurements must not alias a prior snapshot")
	}
	cloned.ImageInput.Value, cloned.ImageOutput.Value, *cloned.ImageCount = 4, 9, 3
	cloned.Total.Value, cloned.Reasoning.Value = 12, 1
	cloned.HostedImageInput.Value, cloned.HostedImageOutput.Value = 99, 99
	if original.Total.Value != 10 || original.Reasoning.Value != 0 || original.ImageInput.Value != 0 || original.ImageOutput.Value != 7 || *original.ImageCount != 2 {
		t.Fatal("mutating a cloned image observation changed its source")
	}
	if cloned.Input != nil || cloned.Output != nil || cloned.Confidence != UsageConfirmedLowerBound || !cloned.TerminalConfirmed {
		t.Fatal("image components must not manufacture total tokens or change confidence")
	}
	if original.HostedImageInput.Value != 3 || original.HostedImageOutput.Value != 8 {
		t.Fatal("hosted image observations must retain independent ownership")
	}
	if CloneUsageFact(nil) != nil {
		t.Fatal("absent usage must remain absent")
	}
	empty := CloneUsageFact(&UsageFact{})
	if empty.Total != nil || empty.Reasoning != nil || empty.ImageInput != nil || empty.ImageOutput != nil || empty.HostedImageInput != nil || empty.HostedImageOutput != nil || empty.ImageCount != nil {
		t.Fatal("absent image measurements must remain absent")
	}
}
