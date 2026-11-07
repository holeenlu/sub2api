// Package session owns session identity and continuation rules independently
// of transport, authorization stores and provider execution.
package session

import (
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gateway/protocol/codex"
)

// ExecutionIdentity contains only the boundary's execution identity signals.
// Header fields are raw client declarations; SessionID comes from the existing
// explicit session resolver, never content-derived affinity. ConnectionID and
// StreamID come from the ingress context, not client metadata. The zero value
// has no declared identity. These signals do not authorize session access.
type ExecutionIdentity struct {
	ThreadIDHeader     string
	TurnMetadataHeader string
	WindowIDHeader     string
	SubagentHeader     string
	SessionID          string
	ConnectionID       string
	StreamID           string
}

// ExecutionLane returns the lane whose requests may share execution state.
// An empty lane is the main turn/prewarm/compaction lane. Named streams retain
// their physical connection prefix, including for background request kinds.
func ExecutionLane(identity ExecutionIdentity, body []byte) string {
	_, lane := resolveExecutionIdentity(identity, body)
	return lane
}

// ExecutionScopeSeed returns the existing unhashed execution key and the
// declared thread for diagnostics. Thread identity precedes explicit session
// identity; only a named stream can supply the connection fallback. Without
// any of these, the seed is empty even if request content or a lane is present.
// The boundary applies the existing session hash; apiKeyID remains part of
// every seed and is not itself an authorization decision.
func ExecutionScopeSeed(identity ExecutionIdentity, body []byte, apiKeyID int64) (seed, threadID string) {
	threadID, lane := resolveExecutionIdentity(identity, body)
	kind, value := "thread", threadID
	if value == "" {
		kind, value = "session", strings.TrimSpace(identity.SessionID)
	}
	if value == "" {
		if identity.StreamID == "" {
			return "", threadID
		}
		kind, value = "connection", identity.ConnectionID
	}
	seed = fmt.Sprintf("openai_ws_exec:%d|%s=%s", apiKeyID, kind, value)
	if lane != "" {
		seed += "|" + lane
	}
	return seed, threadID
}

func resolveExecutionIdentity(identity ExecutionIdentity, body []byte) (threadID, lane string) {
	client := codex.ParseClientMetadata(body)
	metadata, hasHeaderMetadata := codex.ParseTurnMetadata(identity.TurnMetadataHeader)

	// Parent and subagents may share session-id. Preserve the declared thread
	// priority: header, header metadata, window prefix, body, body metadata.
	threadID = strings.TrimSpace(identity.ThreadIDHeader)
	if threadID == "" {
		threadID = metadata.ThreadID
	}
	if threadID == "" {
		threadID = strings.TrimSpace(strings.SplitN(strings.TrimSpace(identity.WindowIDHeader), ":", 2)[0])
	}
	if threadID == "" {
		threadID = client.ThreadID
	}
	if threadID == "" {
		threadID = client.Turn.ThreadID
	}

	// Valid empty header metadata masks body metadata for lane selection,
	// even when the thread itself is obtained from the body.
	if !hasHeaderMetadata {
		metadata = client.Turn
	}
	switch metadata.RequestKind {
	case "", "turn", "prewarm", "compaction":
		// Prewarm and compaction reuse the user's sequential execution lane.
	default:
		lane = "kind=" + metadata.RequestKind
	}
	// Check the selected metadata thread, not the resolved thread: guardian
	// scorers can carry thread-id while remaining independent of user turns.
	if lane == "" && metadata.ThreadID == "" {
		subagent := strings.ToLower(strings.TrimSpace(identity.SubagentHeader))
		if subagent == "" {
			subagent = client.Subagent
		}
		if subagent != "" {
			lane = "subagent=" + subagent
		}
	}
	if identity.StreamID != "" {
		prefix := "connection=" + identity.ConnectionID + "|stream=" + identity.StreamID
		if lane != "" {
			prefix += "|" + lane
		}
		lane = prefix
	}
	return threadID, lane
}
