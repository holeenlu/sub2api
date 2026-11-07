package service

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
)

// BPS accepts at most this many input tokens per request for gpt-6-astra: the
// 1,050,000-token model window minus its 128,000-token output reservation.
// Production evidence (2026-10-22): a BPS session succeeded at 920,360 input
// tokens and every following turn (~926k) failed with context_length_exceeded,
// while the native catalog advertised 1,050,000 and Codex therefore did not
// auto-compact until about 945k. Usage reported by BPS already includes its
// injected Excel tool catalog, which is what Codex compares against the limit.
const (
	excelBPSCodexContextWindow = 922_000
	// Codex's own default is 90% of the window; keep that ratio explicitly so
	// the turn after the check, and the compaction request itself, still fit.
	excelBPSCodexAutoCompactTokenLimit = basispoints.DefaultAutoCompactTokens
)

// clampExcelBPSCodexContext lowers advertised windows above the BPS input limit
// and sets an auto-compaction threshold for them. Smaller windows and explicit
// lower thresholds are left unchanged; missing windows are not invented.
func clampExcelBPSCodexContext(model map[string]json.RawMessage) bool {
	clamped := false
	for _, field := range []string{"context_window", "max_context_window"} {
		if value, ok := codexManifestInt(model[field]); ok && value > excelBPSCodexContextWindow {
			model[field] = json.RawMessage(strconv.Itoa(excelBPSCodexContextWindow))
			clamped = true
		}
	}
	if !clamped {
		return false
	}
	threshold := int64(excelBPSCodexAutoCompactTokenLimit)
	// max_context_window may exceed the currently usable context_window.
	for _, field := range []string{"context_window", "max_context_window"} {
		if window, ok := codexManifestInt(model[field]); ok && window < excelBPSCodexContextWindow {
			threshold = min(threshold, window*9/10)
		}
	}
	if value, ok := codexManifestInt(model["auto_compact_token_limit"]); !ok || value > threshold {
		model["auto_compact_token_limit"] = json.RawMessage(strconv.FormatInt(threshold, 10))
	}
	return true
}

func codexManifestInt(raw json.RawMessage) (int64, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, false
	}
	value, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		var number float64
		if json.Unmarshal(raw, &number) != nil || number <= 0 {
			return 0, false
		}
		return int64(number), true
	}
	return value, value > 0
}

// A fetched or pinned native manifest is not evidence of the inference route's
// capabilities. Restrict only models that this group can send through BPS.
// Operate on the caller's copy and update its ETag after all group transforms.
func restrictExcelBPSCodexModelsManifest(body []byte, accounts []Account, group *Group) ([]byte, bool, error) {
	hasBPS := false
	for i := range accounts {
		hasBPS = hasBPS || accounts[i].IsExcelBPSEnabled()
	}
	if !hasBPS {
		return body, false, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, false, err
	}
	var models []json.RawMessage
	if err := json.Unmarshal(envelope["models"], &models); err != nil {
		return nil, false, err
	}
	changed := false
	for index, raw := range models {
		var model map[string]json.RawMessage
		if json.Unmarshal(raw, &model) != nil {
			continue
		}
		var slug string
		if json.Unmarshal(model["slug"], &slug) != nil || !codexGroupModelUsesExcelBPS(strings.TrimSpace(slug), accounts, group) {
			continue
		}
		updated := false
		for _, field := range []string{"multi_agent_version", "multi_agent_reasoning_effort"} {
			if string(model[field]) != "null" {
				model[field] = json.RawMessage("null")
				updated = true
			}
		}
		if clampExcelBPSCodexContext(model) {
			updated = true
		}
		if updated {
			var err error
			models[index], err = json.Marshal(model)
			if err != nil {
				return nil, false, err
			}
			changed = true
		}
	}
	if !changed {
		return body, false, nil
	}
	var err error
	envelope["models"], err = json.Marshal(models)
	if err != nil {
		return nil, false, err
	}
	result, err := json.Marshal(envelope)
	return result, err == nil, err
}

func codexGroupModelUsesExcelBPS(model string, accounts []Account, group *Group) bool {
	if model == "" {
		return false
	}
	routed := codexModelRoutingAccountIDs(group, model)
	for i := range accounts {
		account := &accounts[i]
		if len(routed) > 0 && !containsInt64(routed, account.ID) {
			continue
		}
		if account.IsModelSupported(model) && account.IsExcelBPSEnabledForModel(model) {
			return true
		}
	}
	return false
}
