package service

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ModelAvailabilityDiagnosis describes whether the requested model can be
// served by any persistently eligible account in the group (active with its
// schedulable setting enabled), ignoring transient state such as rate limits,
// overload, temporary unschedulability, and runtime blocks. Handlers use this
// on the "no available accounts" error path to distinguish 404
// model_not_found from 503 service_unavailable, and to recognise the one
// transient case that deserves a 429 with a Retry-After: every account that
// could serve the model is waiting out a rate-limit cooldown.
type ModelAvailabilityDiagnosis struct {
	// HasAccountsInPool is true if the group has at least one persistently
	// eligible account on the queried platform (or, for Anthropic/Gemini, on
	// the platform plus mixed-scheduled Antigravity accounts).
	HasAccountsInPool bool
	// HasModelSupport is true if at least one account's model mapping admits
	// the requested model.
	HasModelSupport bool
	// AllModelCapableRateLimited is true when at least one model-capable
	// account would be schedulable once its cooldown lifts AND every such
	// account is currently inside a rate-limit cooldown (account-wide
	// RateLimitResetAt or a model-scoped limit). Accounts that stay
	// unschedulable after the cooldown (overload, temporary unschedulability,
	// expiry auto-pause, quota exhaustion) are left out of the tally entirely:
	// their recovery is not governed by the rate limit, so they can neither
	// veto the verdict nor supply its Retry-After.
	//
	// Only meaningful when HasModelSupport is true.
	AllModelCapableRateLimited bool
	// EarliestRateLimitResetAt is the soonest moment a cooling model-capable
	// account (counted as above) leaves its cooldown, or nil when none is
	// cooling. When AllModelCapableRateLimited is true this is when the pool
	// regains its first candidate; otherwise it is only a hint about when the
	// next rate-limited account returns.
	EarliestRateLimitResetAt *time.Time
}

// accountRateLimitCooldownEnd reports when the account leaves its current
// rate-limit cooldown for requestedModel, or nil when it is not rate limited.
// Both the account-wide reset and any model-scoped limit must clear before the
// account can serve the model, so the later of the two wins. A model-scoped
// limit that the scheduler bypasses (Antigravity overages with credits left)
// does not count as a cooldown.
func accountRateLimitCooldownEnd(ctx context.Context, acc *Account, requestedModel string) *time.Time {
	if acc == nil {
		return nil
	}
	now := time.Now()
	var end time.Time
	if acc.RateLimitResetAt != nil && now.Before(*acc.RateLimitResetAt) {
		end = *acc.RateLimitResetAt
	}
	if !acc.modelRateLimitBypassedByOverages() {
		if remaining := acc.GetModelRateLimitRemainingTimeWithContext(ctx, requestedModel); remaining > 0 {
			if modelEnd := now.Add(remaining); modelEnd.After(end) {
				end = modelEnd
			}
		}
	}
	if end.IsZero() {
		return nil
	}
	return &end
}

// modelCapableCooldownTracker accumulates the "is every model-capable account
// cooling down" verdict across a candidate pool. Both the generic and the
// OpenAI diagnoser feed it so the two stay in lockstep.
type modelCapableCooldownTracker struct {
	capable  int
	cooling  int
	earliest *time.Time
}

// observe records one model-capable account. Callers must skip accounts that
// stay unschedulable after their cooldown before calling this.
func (t *modelCapableCooldownTracker) observe(cooldownEnd *time.Time) {
	t.capable++
	if cooldownEnd == nil {
		return
	}
	t.cooling++
	if t.earliest == nil || cooldownEnd.Before(*t.earliest) {
		t.earliest = cooldownEnd
	}
}

// apply writes the verdict onto diag. A pool with no counted model-capable
// account never reports a cooldown: that case belongs to the 404 or plain
// 503 branch.
func (t *modelCapableCooldownTracker) apply(diag *ModelAvailabilityDiagnosis) {
	if diag == nil || t.cooling == 0 {
		return
	}
	diag.EarliestRateLimitResetAt = t.earliest
	diag.AllModelCapableRateLimited = t.cooling == t.capable
}

// ModelAvailabilityDiagnoser is implemented by gateway services that can
// report whether the requested model is configured to be served by any
// account. Both *GatewayService and *OpenAIGatewayService implement this so
// handlers in either package can share a single classifier.
type ModelAvailabilityDiagnoser interface {
	DiagnoseModelAvailabilityForPlatform(
		ctx context.Context,
		groupID *int64,
		requestedModel string,
		platform string,
	) ModelAvailabilityDiagnosis
}

// DiagnoseModelAvailabilityForPlatform inspects accounts enabled for scheduling
// by persistent configuration and returns whether the requested model is
// configured to be served by any of them. The dedicated repository query
// bypasses scheduler snapshots and deliberately ignores transient rate-limit,
// overload, temporary-unschedulable, expiry, quota, and runtime-block state,
// which is exactly what lets the diagnosis also see the accounts a fully
// rate-limited pool has hidden from the scheduler and report their cooldown.
//
// Safe to call on the error path: returns {true,true} on any internal failure
// or when the inputs preclude meaningful diagnosis (empty model, etc.), so
// callers stay on the 503 fallback branch.
func (s *GatewayService) DiagnoseModelAvailabilityForPlatform(
	ctx context.Context,
	groupID *int64,
	requestedModel string,
	platform string,
) ModelAvailabilityDiagnosis {
	if s == nil {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		// No model specified — cannot decide model_not_found. Caller falls back to 503.
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}
	if strings.TrimSpace(platform) == "" {
		// Without a platform we cannot scope the lookup; bail out to the
		// 503 branch rather than make an unscoped scan.
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	if s.accountRepo == nil {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	useMixed := platform == PlatformAnthropic || platform == PlatformGemini
	platforms := []string{platform}
	if useMixed {
		platforms = append(platforms, PlatformAntigravity)
	}

	queryGroupID := groupID
	includeGrouped := false
	if useMixed {
		// Preserve the generic scheduler's scope rules: an explicit group wins
		// for mixed scheduling, while group-less simple mode scans all accounts.
		if groupID == nil && s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
			includeGrouped = true
		}
	} else if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		queryGroupID = nil
		includeGrouped = true
	}

	accounts, err := s.accountRepo.ListModelAvailabilityCandidates(ctx, queryGroupID, platforms, includeGrouped)
	if err != nil {
		// Conservative fallback: pretend everything is fine so the caller
		// returns 503 (we don't want to flip to 404 just because a lookup
		// hiccup'd).
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	diag := ModelAvailabilityDiagnosis{}
	var cooldown modelCapableCooldownTracker
	for i := range accounts {
		acc := &accounts[i]
		if useMixed && acc.Platform == PlatformAntigravity && !acc.IsMixedSchedulingEnabled() {
			continue
		}
		diag.HasAccountsInPool = true
		if !s.isModelSupportedByAccountWithContext(ctx, acc, requestedModel) {
			continue
		}
		diag.HasModelSupport = true
		// 全池冷却的判定需要看完整个池子，不能命中第一个支持该模型的账号就返回。
		// 冷却结束后仍不可调度的账号（过载、临时停调、到期、额度）不参与判定：
		// 它们的恢复时刻与限流无关，Retry-After 不能指向它们。
		if !acc.isSchedulableIgnoringRateLimit() {
			continue
		}
		cooldown.observe(accountRateLimitCooldownEnd(ctx, acc, requestedModel))
	}
	cooldown.apply(&diag)
	return diag
}
