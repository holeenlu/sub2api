package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/tidwall/gjson"
)

// PolicyCapacityUnavailableUntilKey is a passive account observation, in Unix
// milliseconds. It never participates in scheduling, billing or active probes.
const PolicyCapacityUnavailableUntilKey = "policy_capacity_unavailable_until"
const policyCapacityUnavailableWindow = time.Hour

// Match structured upstream error codes only, never generated text or messages
// quoting an error. HTTP errors and both Responses terminal envelopes share it.
func isPolicyCapacityUnavailable(payload []byte) bool {
	kind := gjson.GetBytes(payload, "type").String()
	if kind != "" && kind != "error" && kind != "response.failed" {
		return false
	}
	for _, path := range []string{"error.code", "response.error.code", "code"} {
		if gjson.GetBytes(payload, path).String() == "policy_capacity_unavailable" {
			return gjson.ValidBytes(payload)
		}
	}
	return false
}

func recordPolicyCapacityUnavailable(ctx context.Context, repo AccountRepository, account *Account, payload []byte) {
	if repo == nil || account == nil || account.ID <= 0 || !isPolicyCapacityUnavailable(payload) {
		return
	}
	// Once received, a client disconnect must not discard the observation. The
	// bounded write uses the existing atomic extra merge and snapshot publication.
	stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	until := time.Now().Add(policyCapacityUnavailableWindow).UnixMilli()
	if err := repo.UpdateExtra(stateCtx, account.ID, map[string]any{PolicyCapacityUnavailableUntilKey: until}); err != nil {
		slog.Warn("record policy capacity unavailable", "account_id", account.ID, "error", err)
	}
}

func (s *OpenAIGatewayService) observePolicyCapacityUnavailable(ctx context.Context, account *Account, payload []byte) {
	if s != nil {
		recordPolicyCapacityUnavailable(ctx, s.accountRepo, account, payload)
	}
}

// PolicyCapacityUnavailableUntil omits expired observations at read time; no
// background job or successful request is required to clear the visible badge.
func (a *Account) PolicyCapacityUnavailableUntil(now time.Time) *time.Time {
	if a == nil {
		return nil
	}
	var millis int64
	switch value := a.Extra[PolicyCapacityUnavailableUntilKey].(type) {
	case int64:
		millis = value
	case int:
		millis = int64(value)
	case float64:
		millis = int64(value)
	case json.Number:
		millis, _ = value.Int64()
	default:
		return nil
	}
	until := time.UnixMilli(millis).UTC()
	if !until.After(now) {
		return nil
	}
	return &until
}

// Cloud retry notices are real received failures even when the turn later
// succeeds. The event actor checks thread/turn ownership before invoking this;
// credential generation is checked again so old executions cannot mark a new grant.
func (s *OpenAIGatewayService) observeCloudPolicyCapacityUnavailable(ctx context.Context, owner OpenAICloudOwner, failure *cloudUpstreamError) {
	if s == nil || s.cloudService == nil || failure == nil ||
		(failure.Code != "policy_capacity_unavailable" && failure.CodexErrorInfo != "policyCapacityUnavailable") {
		return
	}
	stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	account, err := s.cloudService.account(stateCtx, owner.AccountID)
	if err != nil {
		return
	}
	binding, err := s.cloudService.repo.GetCloudAuthorization(stateCtx, owner.AccountID)
	if err != nil || !cloudBindingMatches(account, binding) || binding.Generation != owner.GrantGeneration {
		return
	}
	payload, _ := json.Marshal(map[string]any{"error": map[string]any{"code": "policy_capacity_unavailable"}})
	recordPolicyCapacityUnavailable(stateCtx, s.cloudService.accounts, account, payload)
}
