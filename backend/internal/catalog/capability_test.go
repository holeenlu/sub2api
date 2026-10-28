package catalog

import (
	"encoding/json"
	"testing"
)

func TestBooleanCapabilityEvidence(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want CapabilityEvidence
	}{
		{"", CapabilityUnknown}, {"null", CapabilityUnknown}, {"{}", CapabilityUnknown},
		{`"true"`, CapabilityUnknown}, {"1", CapabilityUnknown},
		{" true ", CapabilitySupported}, {"false", CapabilityUnsupported},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			if got := BooleanCapability(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
