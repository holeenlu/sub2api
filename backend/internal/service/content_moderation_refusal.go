package service

import (
	"strings"

	"github.com/tidwall/gjson"
)

// IsUpstreamContentRefusal classifies a complete upstream JSON response or one
// SSE event's JSON data. Only protocol fields are trusted, never prose, tool
// arguments, echoed requests, or arbitrary nested objects. Admission, feedback,
// and failover must share this classifier.
func IsUpstreamContentRefusal(body []byte) bool {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return false
	}
	policyCode := func(value gjson.Result) bool {
		if value.Type != gjson.String {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(value.String())) {
		case "content_policy_violation", "content_policy", "content_filter", "content_filter_refusal", "cyber_policy", "moderation_blocked", "safety_violation", "usage_policy":
			return true
		}
		return false
	}
	for _, path := range []string{"error.code", "error.type", "response.error.code", "response.error.type"} {
		if policyCode(root.Get(path)) {
			return true
		}
	}
	if root.Get("type").String() == "error" && policyCode(root.Get("code")) {
		return true
	}
	for _, path := range []string{"stop_reason", "delta.stop_reason", "message.stop_reason"} {
		switch root.Get(path).String() {
		case "refusal", "content_filter":
			return true
		}
	}
	for _, path := range []string{"incomplete_details.reason", "response.incomplete_details.reason"} {
		if root.Get(path).String() == "content_filter" {
			return true
		}
	}
	hasRefusalBlock := func(blocks gjson.Result) bool {
		if !blocks.IsArray() {
			return false
		}
		for _, block := range blocks.Array() {
			if block.Get("type").String() == "refusal" {
				return true
			}
		}
		return false
	}
	if hasRefusalBlock(root.Get("content")) || root.Get("content_block.type").String() == "refusal" {
		return true
	}
	for _, path := range []string{"output", "response.output"} {
		for _, item := range root.Get(path).Array() {
			if item.Get("type").String() == "message" && hasRefusalBlock(item.Get("content")) {
				return true
			}
		}
	}
	switch root.Get("type").String() {
	case "response.refusal.delta", "response.refusal.done":
		return true
	case "response.content_part.added", "response.content_part.done":
		if root.Get("part.type").String() == "refusal" {
			return true
		}
	case "response.output_item.added", "response.output_item.done":
		if root.Get("item.type").String() == "message" && hasRefusalBlock(root.Get("item.content")) {
			return true
		}
	}
	for _, choice := range root.Get("choices").Array() {
		if choice.Get("finish_reason").String() == "content_filter" {
			return true
		}
		for _, path := range []string{"message.refusal", "delta.refusal"} {
			value := choice.Get(path)
			if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
				return true
			}
		}
	}
	for _, candidate := range root.Get("candidates").Array() {
		switch candidate.Get("finishReason").String() {
		case "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
			return true
		}
	}
	switch root.Get("promptFeedback.blockReason").String() {
	case "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT":
		return true
	}
	return false
}
