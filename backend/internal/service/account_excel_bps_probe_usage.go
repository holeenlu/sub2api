package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

const excelBPSRecoveryProbeScope = "internal_account_bps_recovery"

// ExcelBPSProbeTokens contains cumulative upstream measurements. A nil count
// means unreported, not zero; these values are not customer billing commands.
type ExcelBPSProbeTokens struct {
	Input     *int64 `json:"input_tokens,omitempty"`
	Output    *int64 `json:"output_tokens,omitempty"`
	Total     *int64 `json:"total_tokens,omitempty"`
	Cached    *int64 `json:"cached_input_tokens,omitempty"`
	Reasoning *int64 `json:"reasoning_output_tokens,omitempty"`
	// Cache creation is already included in OpenAI input. Its reported total
	// and lifetime buckets are observations, never additional token charges.
	CacheCreation    *int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheCreation5m  *int64 `json:"cache_creation_5m_tokens,omitempty"`
	CacheCreation1h  *int64 `json:"cache_creation_1h_tokens,omitempty"`
	CacheCreation30m *int64 `json:"cache_creation_30m_tokens,omitempty"`
	CacheCreation12h *int64 `json:"cache_creation_12h_tokens,omitempty"`
}

// ExcelBPS403ProbeUsage belongs to the existing last account probe snapshot.
// It carries no customer identity, billing root, price or settlement authority.
type ExcelBPS403ProbeUsage struct {
	Scope         string               `json:"scope"`
	ProbeID       string               `json:"probe_id"`
	AccountID     int64                `json:"account_id"`
	ClaimAt       string               `json:"claim_at"`
	Model         string               `json:"model"`
	SendState     string               `json:"send_state"`
	ResponseID    string               `json:"response_id,omitempty"`
	ResponseModel string               `json:"response_model,omitempty"`
	Terminal      string               `json:"terminal,omitempty"`
	State         string               `json:"state"` // unknown, measured, lower_bound
	Tokens        *ExcelBPSProbeTokens `json:"tokens,omitempty"`
}

// ExcelBPS403ProbeClaimResult is persisted with the durable claim before sending.
// A crashed worker leaves unknown usage, never proof that nothing was sent.
func ExcelBPS403ProbeClaimResult(a *Account, now time.Time) ExcelBPS403ProbeResult {
	claim := now.UTC().Format(time.RFC3339Nano)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d/%s", excelBPSRecoveryProbeScope, a.ID, claim)))
	return ExcelBPS403ProbeResult{Status: "probing", Stage: "claim", Reason: "pending", Usage: &ExcelBPS403ProbeUsage{
		Scope: excelBPSRecoveryProbeScope, ProbeID: hex.EncodeToString(sum[:]), AccountID: a.ID,
		ClaimAt: claim, Model: excelBPS403RecoveryModel(a), SendState: "unknown", State: "unknown",
	}}
}

// MatchesClaim checks the frozen probe identity, not current recovery permission.
func (u *ExcelBPS403ProbeUsage) MatchesClaim(a *Account) bool {
	if u == nil || a == nil {
		return false
	}
	claim, _ := a.Extra[ExcelBPS403LastProbeAtKey].(string)
	at, err := time.Parse(time.RFC3339Nano, claim)
	if err != nil {
		return false
	}
	expected := ExcelBPS403ProbeClaimResult(a, at).Usage
	return u.Scope == expected.Scope && u.ProbeID == expected.ProbeID && u.AccountID == a.ID && u.ClaimAt == claim && u.Model == expected.Model
}

// Consume only real Responses response envelopes. Tool arguments, text deltas
// and error messages cannot introduce measurements or change response identity.
type bpsAccountProbeObservation struct {
	response bpsAccountProbeResponse
	usage    ExcelBPS403ProbeUsage
	terminal bool
}

func (o *bpsAccountProbeObservation) observe(payload []byte) error {
	var event struct {
		Type     string          `json:"type"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}
	switch event.Type {
	case "response.created", "response.in_progress", "response.completed", "response.failed", "response.incomplete", "response.interrupted":
	default:
		return nil
	}
	var response bpsAccountProbeResponse
	if err := json.Unmarshal(event.Response, &response); err != nil {
		return err
	}
	if o.terminal {
		return errors.New("duplicate BPS terminal")
	}
	if response.ID != "" && o.usage.ResponseID != "" && response.ID != o.usage.ResponseID {
		return errors.New("BPS probe response identity changed")
	}
	if len(response.ID) > 512 || len(response.Model) > 200 {
		return errors.New("BPS probe response identity too long")
	}
	if response.ID != "" {
		o.usage.ResponseID = response.ID
	}
	if response.Model != "" {
		o.usage.ResponseModel = response.Model
	}
	tokens, err := bpsProbeUsageTokens(response.Usage)
	if err != nil {
		return err
	}
	if tokens != nil {
		o.usage.Tokens = tokens
	}
	o.usage.State = "unknown"
	terminal := event.Type != "response.created" && event.Type != "response.in_progress"
	if !terminal {
		return nil
	}
	status := event.Type[len("response."):]
	if response.Status != status {
		return errors.New("invalid BPS terminal status")
	}
	o.terminal, o.response, o.usage.Terminal = true, response, status
	if tokens != nil {
		if status == "completed" && tokens.Input != nil && tokens.Output != nil {
			o.usage.State = "measured"
		} else if status != "completed" {
			o.usage.State = "lower_bound"
		}
	}
	if status != "completed" {
		return errors.New("bps response failed")
	}
	return nil
}

func bpsProbeUsageTokens(raw json.RawMessage) (*ExcelBPSProbeTokens, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var usage struct {
		Input        *int64 `json:"input_tokens"`
		Output       *int64 `json:"output_tokens"`
		Total        *int64 `json:"total_tokens"`
		InputDetails *struct {
			Cached *int64 `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputDetails *struct {
			Reasoning *int64 `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	}
	if err := json.Unmarshal(raw, &usage); err != nil {
		return nil, errors.New("invalid BPS probe usage")
	}
	tokens := &ExcelBPSProbeTokens{Input: usage.Input, Output: usage.Output, Total: usage.Total}
	if usage.InputDetails != nil {
		tokens.Cached = usage.InputDetails.Cached
	}
	if usage.OutputDetails != nil {
		tokens.Reasoning = usage.OutputDetails.Reasoning
	}
	value := gjson.ParseBytes(raw)
	writeDetails := value.Get("input_tokens_details.cache_write_tokens_details")
	if !writeDetails.Exists() {
		writeDetails = value.Get("prompt_tokens_details.cache_write_tokens_details")
	}
	// Share aggregate alias precedence with the real usage consumer, but keep
	// raw bucket presence. The legacy TTL helper normalizes to an integer total
	// and fills absent buckets with zero, which is unsuitable for probe facts.
	for _, field := range []struct {
		source gjson.Result
		target **int64
	}{
		{apicompat.OpenAICacheCreationSourceFromUsage(value), &tokens.CacheCreation},
		{value.Get("cache_creation.ephemeral_5m_input_tokens"), &tokens.CacheCreation5m},
		{value.Get("cache_creation.ephemeral_1h_input_tokens"), &tokens.CacheCreation1h},
		{writeDetails.Get("ttl_30m_tokens"), &tokens.CacheCreation30m},
		{writeDetails.Get("ttl_12h_tokens"), &tokens.CacheCreation12h},
	} {
		count, err := bpsProbeCacheUsageCount(field.source)
		if err != nil {
			return nil, err
		}
		*field.target = count
	}
	measured := false
	for _, n := range []*int64{tokens.Input, tokens.Output, tokens.Total, tokens.Cached, tokens.Reasoning,
		tokens.CacheCreation, tokens.CacheCreation5m, tokens.CacheCreation1h, tokens.CacheCreation30m, tokens.CacheCreation12h} {
		if n != nil {
			// Account.Extra is also read through generic JSON maps. Reject a
			// count that those readers would silently round rather than storing
			// a falsely exact consumption fact.
			if *n < 0 || *n > 1<<53-1 {
				return nil, errors.New("invalid BPS probe usage")
			}
			measured = true
		}
	}
	if (tokens.Cached != nil && tokens.Input != nil && *tokens.Cached > *tokens.Input) ||
		(tokens.Reasoning != nil && tokens.Output != nil && *tokens.Reasoning > *tokens.Output) {
		return nil, errors.New("invalid BPS probe usage details")
	}
	if !measured {
		return nil, nil
	}
	return tokens, nil
}

func bpsProbeCacheUsageCount(value gjson.Result) (*int64, error) {
	if !value.Exists() || value.Type == gjson.Null {
		return nil, nil
	}
	var count int64
	if value.Type != gjson.Number || json.Unmarshal([]byte(value.Raw), &count) != nil {
		return nil, errors.New("invalid BPS probe cache usage")
	}
	return &count, nil
}
