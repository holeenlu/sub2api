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
	// RateLimit is populated only when every model-capable account in the
	// diagnosed pool shares the same precise model-level limit attribution.
	// Account-wide limits intentionally remain unattributed until their window
	// is persisted separately.
	RateLimit *RateLimitAttribution
}

// RateLimitAttribution identifies a precise, client-actionable limit.
// ResetAt is copied before returning so callers can safely retain the result.
type RateLimitAttribution struct {
	Scope   string
	Window  string
	Reason  string
	Model   string
	ResetAt *time.Time
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

// modelRateLimitAttributionForRequest returns the precise Fable model-level
// attribution when it is the only active limit. Account-wide cooldowns and
// local threshold pauses deliberately stay unattributed for this vertical
// slice; labeling either as an upstream Fable exhaustion would mislead the
// client.
func modelRateLimitAttributionForRequest(acc *Account, requestedModel string) *RateLimitAttribution {
	if acc == nil || acc.Platform != PlatformAnthropic {
		return nil
	}
	modelKey := strings.TrimSpace(acc.GetMappedModel(requestedModel))
	if !isAnthropicFableModel(modelKey) {
		return nil
	}
	now := time.Now()
	if acc.RateLimitResetAt != nil && now.Before(*acc.RateLimitResetAt) {
		return nil
	}
	resetAt := acc.modelRateLimitResetAt(anthropicFableRateLimitKey)
	if resetAt == nil || !now.Before(*resetAt) {
		return nil
	}
	if strings.TrimSpace(acc.modelRateLimitReason(anthropicFableRateLimitKey)) != AnthropicFableWindowExhaustedReason {
		return nil
	}
	return &RateLimitAttribution{
		Scope:   "model",
		Window:  "7d_oi",
		Reason:  AnthropicFableWindowExhaustedReason,
		Model:   strings.TrimSpace(requestedModel),
		ResetAt: resetAt,
	}
}

// modelCapableCooldownTracker accumulates the "is every model-capable account
// cooling down" verdict across a candidate pool. Both the generic and the
// OpenAI diagnoser feed it so the two stay in lockstep.
type modelCapableCooldownTracker struct {
	capable               int
	cooling               int
	earliest              *time.Time
	attribution           *RateLimitAttribution
	attributionConsistent bool
	attributionSeen       bool
}

// observe records one model-capable account. Callers must skip accounts that
// stay unschedulable after their cooldown before calling this.
func (t *modelCapableCooldownTracker) observe(cooldownEnd *time.Time, attribution *RateLimitAttribution) {
	t.capable++
	if cooldownEnd == nil {
		return
	}
	t.cooling++
	if t.earliest == nil || cooldownEnd.Before(*t.earliest) {
		t.earliest = cooldownEnd
	}
	if !t.attributionSeen {
		t.attributionSeen = true
		t.attributionConsistent = attribution != nil
		if attribution != nil {
			t.attribution = cloneRateLimitAttribution(attribution)
		}
		return
	}
	if attribution == nil || !sameRateLimitAttribution(t.attribution, attribution) {
		t.attributionConsistent = false
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
	if diag.AllModelCapableRateLimited && t.attributionConsistent && t.attribution != nil {
		attribution := cloneRateLimitAttribution(t.attribution)
		if attribution.ResetAt == nil || t.earliest.Before(*attribution.ResetAt) {
			resetAt := *t.earliest
			attribution.ResetAt = &resetAt
		}
		diag.RateLimit = attribution
	}
}

func cloneRateLimitAttribution(attribution *RateLimitAttribution) *RateLimitAttribution {
	if attribution == nil {
		return nil
	}
	clone := *attribution
	if attribution.ResetAt != nil {
		resetAt := *attribution.ResetAt
		clone.ResetAt = &resetAt
	}
	return &clone
}

func sameRateLimitAttribution(left, right *RateLimitAttribution) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Scope == right.Scope && left.Window == right.Window &&
		left.Reason == right.Reason && left.Model == right.Model
}

// isFinalWithoutFallbackChain reports whether the fallback groups can no
// longer change the verdict this diagnosis leads to. A pool that serves the
// model and is not fully cooling already answers 503 "temporarily exhausted";
// borrowing a fallback group's accounts can neither turn that into a 404 nor
// into a pool-wide cooldown. It also covers the conservative {true,true} the
// diagnosers return on a lookup failure, so a database hiccup does not fan out
// into one extra query per hop on the error path.
func (d ModelAvailabilityDiagnosis) isFinalWithoutFallbackChain() bool {
	return d.HasModelSupport && !d.AllModelCapableRateLimited
}

// diagnoseAcrossNoAccountFallback merges the per-group diagnoses along the
// no-account fallback chain, because account selection walks that same chain
// before it gives up (see runWithNoAccountFallback). Diagnosing only the API
// key's own group answers a question the request no longer asked: a group that
// cannot serve the model but falls back to one that can yields 404
// model_not_found, which tells the client to stop retrying even though the
// fallback pool would serve it once its cooldown lifts.
//
// The merge is deliberately conservative on the 429 side: every hop that has
// model-capable accounts must report them all cooling, so a single hop with a
// live capable account keeps the verdict on 503. EarliestRateLimitResetAt
// becomes the soonest reset anywhere on the chain, which is when the request
// first has a chance of succeeding.
func diagnoseAcrossNoAccountFallback(
	ctx context.Context,
	chain *noAccountFallbackChain,
	origin ModelAvailabilityDiagnosis,
	hop func(ctx context.Context, groupID *int64) ModelAvailabilityDiagnosis,
) ModelAvailabilityDiagnosis {
	if chain == nil || hop == nil {
		return origin
	}

	merged := ModelAvailabilityDiagnosis{}
	supporting, allCooling := 0, 0
	preciseAttribution := true
	var attribution *RateLimitAttribution
	absorb := func(d ModelAvailabilityDiagnosis) {
		merged.HasAccountsInPool = merged.HasAccountsInPool || d.HasAccountsInPool
		merged.HasModelSupport = merged.HasModelSupport || d.HasModelSupport
		if d.HasModelSupport {
			supporting++
			if d.AllModelCapableRateLimited {
				allCooling++
			} else {
				preciseAttribution = false
			}
			if d.RateLimit == nil {
				preciseAttribution = false
			} else if attribution == nil {
				attribution = cloneRateLimitAttribution(d.RateLimit)
			} else if !sameRateLimitAttribution(attribution, d.RateLimit) {
				preciseAttribution = false
			}
		}
		if d.EarliestRateLimitResetAt != nil &&
			(merged.EarliestRateLimitResetAt == nil || d.EarliestRateLimitResetAt.Before(*merged.EarliestRateLimitResetAt)) {
			merged.EarliestRateLimitResetAt = d.EarliestRateLimitResetAt
		}
	}

	absorb(origin)
	for {
		target := chain.next(ctx)
		if target == nil {
			break
		}
		targetID := target.ID
		absorb(hop(ctx, &targetID))
	}
	merged.AllModelCapableRateLimited = supporting > 0 && supporting == allCooling
	if merged.AllModelCapableRateLimited && preciseAttribution && attribution != nil {
		attribution.ResetAt = merged.EarliestRateLimitResetAt
		merged.RateLimit = attribution
	}
	return merged
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
//
// The diagnosis spans the no-account fallback chain, matching the groups
// account selection would have tried; see diagnoseAcrossNoAccountFallback.
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

	origin := s.diagnoseModelAvailabilityInGroup(ctx, groupID, requestedModel, platform)
	if origin.isFinalWithoutFallbackChain() {
		return origin
	}
	return diagnoseAcrossNoAccountFallback(ctx, s.noAccountFallbackChain(groupID), origin,
		func(ctx context.Context, hopGroupID *int64) ModelAvailabilityDiagnosis {
			return s.diagnoseModelAvailabilityInGroup(ctx, hopGroupID, requestedModel, platform)
		})
}

// diagnoseModelAvailabilityInGroup is DiagnoseModelAvailabilityForPlatform for
// a single group, with the input guards already applied.
func (s *GatewayService) diagnoseModelAvailabilityInGroup(
	ctx context.Context,
	groupID *int64,
	requestedModel string,
	platform string,
) ModelAvailabilityDiagnosis {
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
		cooldown.observe(
			accountRateLimitCooldownEnd(ctx, acc, requestedModel),
			modelRateLimitAttributionForRequest(acc, requestedModel),
		)
	}
	cooldown.apply(&diag)
	return diag
}
