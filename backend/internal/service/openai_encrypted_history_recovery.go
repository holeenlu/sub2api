package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Ciphertext in replayed history — reasoning, compaction and output-token
// replay — is readable only by the upstream and account that issued it. When a
// conversation moves between protocols or accounts (BPS, Cloud, another native
// account) the new upstream rejects that history before any inference. On
// 2026-10-24 the user chose to recover such a conversation instead of failing
// it: remove what cannot be read and retry on the same account, one reduction
// at a time.
//
//  1. Drop reasoning items that carry ciphertext (nothing of them is usable
//     without it) and strip output-token replay ciphertext from assistant
//     messages and calls, whose plaintext stays.
//  2. Only if still rejected, or if the rejection names a compaction, replace
//     each compaction with a notice. A compaction is the only copy of what it
//     summarized, so it is kept while it may still be readable.
//
// A compaction this gateway issued on Cloud is a readable summary envelope;
// step 1 already installs its summary, so no context is lost for it.
//
// The model loses that hidden context; visible messages, calls and outputs
// stay. Native HTTP/WS, BPS and Cloud share these reductions. Cloud applies them
// only after a proven pre-inference rejection, on a fresh same-account thread.
const openAIEncryptedHistoryRecoveryReason = "encrypted history recovery"

// openAIOmittedCompactionNotice stands in for a compaction the upstream cannot
// read, so the model knows earlier context was summarized rather than seeing a
// silent gap.
const openAIOmittedCompactionNotice = "[Earlier conversation summary omitted: it was produced by another provider and cannot be read here. Messages that are still present above and below remain available.]"

var openAIEncryptedContentItemPattern = regexp.MustCompile(`(?i)encrypted content for item ['"]?([^\s'"]+?)['"]? could not be verified`)

func openAIHistoryItemCiphertext(item map[string]any) bool {
	text, _ := item["encrypted_content"].(string)
	return text != ""
}

func openAIHistoryItemType(item map[string]any) string {
	kind, _ := item["type"].(string)
	if kind == "" && item["role"] != nil {
		return "message"
	}
	return kind
}

func openAICompactionHistoryItem(item map[string]any) bool {
	kind := openAIHistoryItemType(item)
	return isOpenAICompactionType(kind) || kind == "context_compaction"
}

// openAICloudCompactionEnvelope reports a compaction this gateway issued on
// Cloud with a decodable summary; native and BPS upstreams cannot read it.
func openAICloudCompactionEnvelope(item map[string]any) bool {
	if openAIHistoryItemType(item) != "compaction" {
		return false
	}
	summary, summarized, err := cloudCompactionSummary(item)
	return summarized && err == nil && summary != ""
}

// Output-token replay ciphertext sits beside the item's own plaintext.
func openAIReplayCiphertextItem(item map[string]any) bool {
	switch openAIHistoryItemType(item) {
	case "message":
		return item["role"] == "assistant"
	case "function_call", "custom_tool_call", "web_search_call", "image_generation_call", "local_shell_call", "tool_search_call":
		return true
	}
	return false
}

// openAIReduceHistoryCiphertext applies reduction 1, and reduction 2 when
// compactions is set. It returns how many items lost ciphertext and how many
// compactions were replaced; the input slice and its items are not modified.
func openAIReduceHistoryCiphertext(items []any, compactions bool) ([]any, int, int) {
	reduced := make([]any, 0, len(items))
	removed, replaced := 0, 0
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		switch {
		case !ok || !openAIHistoryItemCiphertext(item):
			reduced = append(reduced, raw)
		case openAIHistoryItemType(item) == "reasoning":
			removed++
		case openAICloudCompactionEnvelope(item):
			// This gateway's own Cloud summary is readable here: install it as
			// the message Codex installs after its local compaction.
			summary, _, _ := cloudCompactionSummary(item)
			removed++
			reduced = append(reduced, cloudCompactionMessage(summary))
		case openAICompactionHistoryItem(item):
			if !compactions {
				reduced = append(reduced, raw)
				continue
			}
			replaced++
			reduced = append(reduced, map[string]any{"type": "message", "role": "developer",
				"content": []any{map[string]any{"type": "input_text", "text": openAIOmittedCompactionNotice}}})
		case openAIReplayCiphertextItem(item):
			plain := make(map[string]any, len(item))
			for key, value := range item {
				if key != "encrypted_content" {
					plain[key] = value
				}
			}
			removed++
			reduced = append(reduced, plain)
		default:
			reduced = append(reduced, raw)
		}
	}
	return reduced, removed, replaced
}

// openAIHistoryWithoutReasoningCiphertext applies reduction 1.
func openAIHistoryWithoutReasoningCiphertext(items []any) ([]any, bool) {
	reduced, removed, _ := openAIReduceHistoryCiphertext(items, false)
	return reduced, removed > 0
}

// openAIHistoryWithoutCompactionCiphertext applies both reductions.
func openAIHistoryWithoutCompactionCiphertext(items []any) ([]any, bool) {
	reduced, removed, replaced := openAIReduceHistoryCiphertext(items, true)
	return reduced, removed+replaced > 0
}

// openAIHistoryShape summarizes history for logs: item counts by type, how
// many carry ciphertext and each distinct field set. It never includes
// content, IDs or ciphertext, only type and field names.
func openAIHistoryShape(items []any) string {
	counts := make(map[string]int)
	fields := make(map[string]int)
	encrypted := 0
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		kind := openAIHistoryItemType(item)
		counts[kind]++
		if item != nil && item["encrypted_content"] != nil {
			encrypted++
		}
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fields[kind+"["+strings.Join(keys, ",")+"]"]++
	}
	parts := make([]string, 0, len(counts)+len(fields))
	for kind, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", kind, count))
	}
	sort.Strings(parts)
	signatures := make([]string, 0, len(fields))
	for signature, count := range fields {
		signatures = append(signatures, fmt.Sprintf("%s=%d", signature, count))
	}
	sort.Strings(signatures)
	if len(signatures) > 12 {
		signatures = append(signatures[:12], fmt.Sprintf("+%d more field sets", len(signatures)-12))
	}
	return fmt.Sprintf("items=%d encrypted=%d %s fields: %s", len(items), encrypted, strings.Join(parts, " "), strings.Join(signatures, " "))
}

// openAIEncryptedHistoryRejected reports an upstream refusal of history
// ciphertext it cannot read. responseBody uses the {"error":...} envelope.
func openAIEncryptedHistoryRejected(code string, responseBody []byte) bool {
	return code == "invalid_compaction" || code == "compaction_decode_error" || isOpenAIEncryptedContextRejection(responseBody)
}

func openAICompactionRejected(items []any, code string, responseBody []byte) bool {
	if code == "invalid_compaction" || code == "compaction_decode_error" {
		return true
	}
	match := openAIEncryptedContentItemPattern.FindStringSubmatch(extractUpstreamErrorMessage(responseBody))
	if len(match) != 2 {
		return false
	}
	for _, raw := range items {
		if item, ok := raw.(map[string]any); ok && item["id"] == match[1] && openAICompactionHistoryItem(item) {
			return true
		}
	}
	return false
}

// openAIEncryptedHistoryRecovery returns the next reduction of body when
// rejection (an HTTP body or a WS/SSE error event) refuses unreadable history
// ciphertext. It changes only "input" and never the caller's slice.
func openAIEncryptedHistoryRecovery(body, rejection []byte) ([]byte, string, bool, error) {
	if nested := gjson.GetBytes(rejection, "response.error"); nested.IsObject() {
		rejection = []byte(`{"error":` + nested.Raw + `}`)
	}
	code := strings.ToLower(strings.TrimSpace(extractUpstreamErrorCode(rejection)))
	if !openAIEncryptedHistoryRejected(code, rejection) {
		return nil, "", false, nil
	}
	return openAIEncryptedHistoryRecoveryBody(body, code, rejection)
}

// openAIEncryptedHistoryRecoveryBody declines a request that continues stored
// state (previous_response_id): the rejected context need not be in its body.
func openAIEncryptedHistoryRecoveryBody(body []byte, code string, rejection []byte) ([]byte, string, bool, error) {
	declined := openAIEncryptedHistoryRecoveryReason + " declined: no removable ciphertext"
	if strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()) != "" {
		return nil, openAIEncryptedHistoryRecoveryReason + " declined: the request continues stored upstream state", false, nil
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return nil, declined, false, nil
	}
	decoder := json.NewDecoder(strings.NewReader(input.Raw))
	decoder.UseNumber()
	var items []any
	if err := decoder.Decode(&items); err != nil {
		return nil, "", false, fmt.Errorf("decode rejected history: %w", err)
	}
	reduced, removed, replaced := openAIReduceHistoryCiphertext(items, false)
	if removed == 0 || openAICompactionRejected(items, code, rejection) {
		reduced, removed, replaced = openAIReduceHistoryCiphertext(items, true)
	}
	if removed+replaced == 0 {
		return nil, declined, false, nil
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(reduced); err != nil {
		return nil, "", false, fmt.Errorf("encode recovered history: %w", err)
	}
	retryBody, err := sjson.SetRawBytes(body, "input", bytes.TrimSpace(encoded.Bytes()))
	if err != nil {
		return nil, "", false, fmt.Errorf("set recovered history: %w", err)
	}
	removedWhat := "reasoning ciphertext"
	switch {
	case replaced > 0 && removed > 0:
		removedWhat = "reasoning and compaction ciphertext"
	case replaced > 0:
		removedWhat = "compaction ciphertext"
	}
	return retryBody, fmt.Sprintf("%s: removed %s (%s)", openAIEncryptedHistoryRecoveryReason, removedWhat, openAIHistoryShape(items)), true, nil
}
