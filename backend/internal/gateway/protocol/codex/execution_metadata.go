// Package codex decodes Codex wire metadata without transport or session state.
package codex

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// TurnMetadata contains only the execution identity fields in
// x-codex-turn-metadata. Empty fields are valid and do not grant ownership.
type TurnMetadata struct {
	ThreadID    string `json:"thread_id"`
	RequestKind string `json:"request_kind"`
}

// ParseTurnMetadata trims identity fields and normalizes the request kind.
// Valid empty objects and null return true; malformed or wrongly typed fields
// return an empty value and false, never partially decoded identity.
func ParseTurnMetadata(raw string) (TurnMetadata, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return TurnMetadata{}, false
	}
	var metadata TurnMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return TurnMetadata{}, false
	}
	metadata.ThreadID = strings.TrimSpace(metadata.ThreadID)
	metadata.RequestKind = strings.ToLower(strings.TrimSpace(metadata.RequestKind))
	return metadata, true
}

// ClientMetadata contains the existing top-level client_metadata identity
// signals. A missing or malformed field contributes an empty value.
type ClientMetadata struct {
	ThreadID string
	Turn     TurnMetadata
	Subagent string
}

// ParseClientMetadata reads the supplied wire without modifying or unwrapping
// it. Nested response envelopes are not a new source of Codex thread identity.
func ParseClientMetadata(body []byte) ClientMetadata {
	turn, _ := ParseTurnMetadata(gjson.GetBytes(body, "client_metadata.x-codex-turn-metadata").String())
	return ClientMetadata{
		ThreadID: strings.TrimSpace(gjson.GetBytes(body, "client_metadata.thread_id").String()),
		Turn:     turn,
		Subagent: strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "client_metadata.x-openai-subagent").String())),
	}
}
