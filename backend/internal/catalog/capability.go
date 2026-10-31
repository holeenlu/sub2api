package catalog

import (
	"bytes"
	"encoding/json"
)

// CapabilityEvidence records an explicit declaration, not model admission.
// Its zero value is unknown; absence must never grant a capability.
type CapabilityEvidence uint8

const (
	CapabilityUnknown CapabilityEvidence = iota
	CapabilitySupported
	CapabilityUnsupported
)

// BooleanCapability accepts only JSON booleans. Missing, null and malformed
// declarations are unknown, distinct from an explicit false.
func BooleanCapability(raw json.RawMessage) CapabilityEvidence {
	switch string(bytes.TrimSpace(raw)) {
	case "true":
		return CapabilitySupported
	case "false":
		return CapabilityUnsupported
	default:
		return CapabilityUnknown
	}
}
