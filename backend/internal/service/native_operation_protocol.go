package service

import (
	"context"

	responseswire "github.com/Wei-Shaw/sub2api/internal/gateway/protocol/responses"
)

// A family describes the encoder for one actual upstream attempt. It is not
// the client-facing protocol, a URL heuristic, or a property of received JSON.
type nativeUpstreamFamily string

const (
	nativeUpstreamUnknown   nativeUpstreamFamily = ""
	nativeUpstreamResponses nativeUpstreamFamily = "responses"
	nativeUpstreamChat      nativeUpstreamFamily = "chat"
	nativeUpstreamAnthropic nativeUpstreamFamily = "anthropic"
)

type nativeUpstreamFamilyKey struct{}

func withNativeUpstreamFamily(ctx context.Context, family nativeUpstreamFamily) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, nativeUpstreamFamilyKey{}, family)
}

func nativeUpstreamFamilyFromContext(ctx context.Context) nativeUpstreamFamily {
	if ctx == nil {
		return nativeUpstreamUnknown
	}
	family, _ := ctx.Value(nativeUpstreamFamilyKey{}).(nativeUpstreamFamily)
	switch family {
	case nativeUpstreamResponses, nativeUpstreamChat, nativeUpstreamAnthropic:
		return family
	default:
		return nativeUpstreamUnknown
	}
}

// Only events understood by the current lifecycle/output consumers can assert
// response ownership. Namespace membership is not authority: future events
// remain transparent wire until their semantics and owner fields are reviewed.
// This does not grant token-usage authority to every recognized output event.
func nativeResponsesObservationEvent(kind string) bool {
	if responseswire.IsTerminalEvent(kind) {
		return true
	}
	switch kind {
	case "response.created", "response.queued", "response.in_progress",
		"response.metadata", "response.interrupt.accepted", "response.interrupt.failed",
		"response.output_item.added", "response.output_item.done",
		"response.content_part.added", "response.content_part.done",
		"response.output_text.delta", "response.output_text.done", "response.output_text.annotation.added",
		"response.refusal.delta", "response.refusal.done",
		"response.function_call_arguments.delta", "response.function_call_arguments.done",
		"response.custom_tool_call_input.delta", "response.custom_tool_call_input.done",
		"response.reasoning_summary_part.added", "response.reasoning_summary_part.done",
		"response.reasoning_summary_text.delta", "response.reasoning_summary_text.done",
		"response.reasoning_text.delta", "response.reasoning_text.done",
		"response.audio_transcript.delta", "response.audio_transcript.done",
		"response.image_generation_call.partial_image", "response.image_generation_call.completed",
		"response.web_search_call.completed":
		return true
	default:
		return false
	}
}
