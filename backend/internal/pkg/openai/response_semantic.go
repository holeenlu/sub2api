package openai

import (
	responseswire "github.com/Wei-Shaw/sub2api/internal/gateway/protocol/responses"
	"github.com/tidwall/gjson"
)

// ResponseStartsSemanticOutput reports the first Responses event that carries
// reasoning or usable output. Transport metadata and terminal events never
// start the first-token clock.
func ResponseStartsSemanticOutput(message []byte, eventType string) bool {
	if len(message) == 0 || !gjson.ValidBytes(message) {
		return false
	}
	switch eventType {
	case "response.output_item.added", "response.output_item.done":
		if gjson.GetBytes(message, "item.type").String() == "reasoning" {
			return true
		}
	case "response.reasoning_summary_part.added", "response.reasoning_summary_part.done", "response.content_part.added", "response.content_part.done":
		switch gjson.GetBytes(message, "part.type").String() {
		case "summary_text", "reasoning_text":
			return true
		}
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
		return false
	}
	return responseswire.StartsVisibleOutput(string(message), eventType)
}
