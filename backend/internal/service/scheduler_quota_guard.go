package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

const (
	SchedulingQuotaRequestTooLarge = "quota_guard_request_too_large"
)

type schedulingQuotaRequestKey struct{}
type schedulingQuotaRequest struct {
	once         sync.Once
	healthOnce   sync.Once
	healthBand   string
	blocked      atomic.Bool
	body         []byte
	protocol     string
	model        string
	inputTokens  int
	continuation bool
	scope        string
	prefixes     [][32]byte
}

// WithSchedulingQuotaRequest installs input-only admission evidence for one
// generation request. Use the ContentModerationProtocol constants. Reinstall it
// after changing the body and for each WS turn. The body must remain immutable.
// Conversion/tokenization is lazy, shared by all candidates and retries, and only
// needed after a candidate has trustworthy critical-window evidence.
func WithSchedulingQuotaRequest(ctx context.Context, body []byte, protocol string, keys ...*APIKey) context.Context {
	scope := ""
	if previous, _ := ctx.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest); previous != nil {
		scope = previous.scope
	}
	if len(keys) > 0 && keys[0] != nil {
		scope = fmt.Sprintf("%d:%d:%d", keys[0].UserID, keys[0].ID, derefGroupID(keys[0].GroupID))
	}
	return context.WithValue(ctx, schedulingQuotaRequestKey{}, &schedulingQuotaRequest{body: body, protocol: protocol, scope: scope})
}

// SchedulingQuotaRequestWasBlocked lets the existing no-account response mapper
// distinguish request quota protection (429) from unavailable capacity (503).
// Candidate rejection sets this flag; a new body/WS turn starts with a clear flag.
func SchedulingQuotaRequestWasBlocked(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	risk, _ := ctx.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest)
	return risk != nil && risk.blocked.Load()
}

func (risk *schedulingQuotaRequest) estimate() {
	risk.once.Do(risk.estimateOnce)
}

func (risk *schedulingQuotaRequest) estimateOnce() {
	body := risk.body
	var request openAIInputTokensCountRequest
	var converted *apicompat.ResponsesRequest
	var err error
	switch risk.protocol {
	case ContentModerationProtocolAnthropicMessages:
		var input apicompat.AnthropicRequest
		err = json.Unmarshal(body, &input)
		if err == nil {
			converted, err = apicompat.AnthropicToResponses(&input)
		}
	case ContentModerationProtocolOpenAIChat:
		var input apicompat.ChatCompletionsRequest
		err = json.Unmarshal(body, &input)
		if err == nil {
			converted, err = apicompat.ChatCompletionsToResponses(&input)
		}
	case ContentModerationProtocolOpenAIResponses:
		err = json.Unmarshal(body, &request)
	default:
		return
	}
	if err != nil {
		return
	}
	if converted != nil {
		request = openAIInputTokensCountRequest{Model: converted.Model, Instructions: converted.Instructions,
			Input: converted.Input, Tools: converted.Tools, ToolChoice: converted.ToolChoice}
	}
	risk.model = request.Model
	// History is not cache evidence. Estimate all visible input; only a prior
	// successful, same-credential prefix with observed cached usage can discount it.
	parsed := gjson.ParseBytes(body)
	risk.continuation = parsed.Get("previous_response_id").String() != "" || parsed.Get("conversation").Exists()
	if request.Model != "" {
		risk.inputTokens, _ = estimateOpenAIInputTokens(request)
	}
	prefix := sha256.New()
	prefix.Write([]byte(risk.protocol + "\x00" + risk.model))
	for _, field := range []string{"system", "instructions", "tools", "tool_choice", "reasoning", "thinking", "previous_response_id", "conversation"} {
		prefix.Write([]byte(parsed.Get(field).Raw))
		prefix.Write([]byte{0})
	}
	var items []gjson.Result
	for _, field := range []string{"messages", "input"} {
		value := parsed.Get(field)
		if value.IsArray() {
			items = value.Array()
			break
		}
		if value.Exists() {
			items = []gjson.Result{value}
		}
	}
	for _, item := range items {
		canonical := []byte(item.Raw)
		// Raw token bytes are hashed without coercing JSON numbers to float64.
		prefix.Write(canonical)
		prefix.Write([]byte{0})
		var digest [32]byte
		copy(digest[:], prefix.Sum(nil))
		risk.prefixes = append(risk.prefixes, digest)
	}

}

// IsRequestBlockedBySchedulingQuota is a request-local filter. It must run at
// candidate filtering and the existing final eligibility recheck, before slot
// dispatch. It never parks an account, alters billing, or writes usage samples.
func (s *RateLimitService) IsRequestBlockedBySchedulingQuota(ctx context.Context, account *Account) bool {
	var settings *SettingService
	if s != nil {
		settings = s.settingService
	}
	return schedulingQuotaRequestBlockReason(ctx, account, settings) != ""
}

func schedulingQuotaRequestBlockReason(ctx context.Context, account *Account, settings *SettingService) string {
	if ctx == nil || account == nil || account.IsSIWCEnabled() || len(account.Extra) == 0 {
		return ""
	}
	risk, ok := ctx.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest)
	if !ok || risk == nil {
		return ""
	}
	if account.IsExcelBPSEnabled() && account.IsExcelBPSEnabledForModel(gjson.GetBytes(risk.body, "model").String()) {
		return ""
	}
	var thresholds map[string]int
	if settings != nil {
		thresholds, _ = settings.GetAccountSchedulingThresholds(ctx)
	}
	policy := settings.GetSchedulingQuotaProtection(ctx)
	if !policy.GuardLargeCold {
		return ""
	}
	window := schedulingQuotaCriticalWindow(ctx, account, thresholds, time.Now(), policy)
	if window == "" {
		return ""
	}
	risk.estimate()
	if schedulingQuotaUncachedTokens(risk, account) < policy.LargeColdInputTokens || account.IsExcelBPSEnabledForModel(risk.model) {
		return ""
	}
	risk.blocked.Store(true)
	RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "filter", Action: "rejected", Reason: SchedulingQuotaRequestTooLarge + "_" + window, AccountID: account.ID})
	return SchedulingQuotaRequestTooLarge
}

type schedulingQuotaObservation struct {
	UsedPercent float64   `json:"used_percent"`
	ResetAt     time.Time `json:"reset_at"`
	SampledAt   time.Time `json:"sampled_at"`
}

const schedulingQuotaAnthropicIdentityKey = "scheduler_quota_anthropic_identity"

// SchedulingQuotaMetadataExtra retains only the quota evidence that candidate
// filtering needs. The Redis metadata projection deliberately drops OAuth tokens;
// keep their derived identity, never the credential, and regenerate it whenever
// the full account changes. A hydrated account still validates its real identity.
func SchedulingQuotaMetadataExtra(account *Account) map[string]any {
	if account == nil || account.IsSIWCEnabled() {
		return nil
	}
	extra := make(map[string]any)
	if account.IsAnthropicOAuthOrSetupToken() {
		identity := anthropicCredentialIdentity(account)
		if identity == "" {
			return nil
		}
		for key, value := range account.Extra {
			field := strings.TrimPrefix(key, anthropicObservationPrefix)
			if field != key && isAnthropicObservationField(field) {
				extra[key] = value
			}
		}
		extra[schedulingQuotaAnthropicIdentityKey] = identity
		extra[schedulingQuotaAnthropicTierKey] = anthropicSchedulingMaxPlan(account)
	} else if account.IsOpenAI() && openAICodexSnapshotIdentityTrusted(account) {
		for _, window := range []string{"5h", "7d"} {
			key := "codex_" + window + "_quota_observation"
			if value := account.Extra[key]; value != nil {
				extra[key] = value
			}
		}
	}
	return extra
}

// appendSchedulingQuotaObservations consumes ONLY the current Codex snapshot
// patch, never merged account.Extra. Call at the end of buildCodexUsageExtraUpdates.
// Sparse updates invalidate that window's evidence instead of borrowing an old
// utilization/reset. An unrelated window update does not refresh its timestamp.
func appendSchedulingQuotaObservations(updates map[string]any) {
	if updates == nil {
		return
	}
	for _, window := range []string{"5h", "7d"} {
		prefix := "codex_" + window + "_"
		_, hasUsed := updates[prefix+"used_percent"]
		_, hasReset := updates[prefix+"reset_at"]
		_, hasRelative := updates[prefix+"reset_after_seconds"]
		if !hasUsed && !hasReset && !hasRelative {
			continue
		}
		key := prefix + "quota_observation"
		updates[key] = nil
		used, valid := resolveAccountExtraNumber(updates, prefix+"used_percent")
		at := parseSchedulingResetAt(updates["codex_usage_updated_at"])
		reset := parseSchedulingResetAt(updates[prefix+"reset_at"])
		if !valid || !validSchedulingQuotaPercent(used) || at == nil || reset == nil || !reset.After(*at) {
			continue
		}
		updates[key] = schedulingQuotaObservation{UsedPercent: used, ResetAt: *reset, SampledAt: *at}
	}
}

func schedulingQuotaCriticalWindow(ctx context.Context, account *Account, thresholds map[string]int, now time.Time, policies ...SchedulingQuotaProtection) string {
	policy := effectiveSchedulingQuotaProtection(policies...)
	// SIWC has an independently authorized subject/grant. Native Codex quota
	// samples cannot establish its upstream budget or a draining threshold.
	if account == nil || account.IsSIWCEnabled() {
		return ""
	}
	var observations map[string]UsageObservation
	switch {
	case account.IsAnthropicOAuthOrSetupToken():
		// Only provenance attached to the current credential is admissible. The
		// legacy aggregate timestamp cannot date independently sampled windows.
		identity := anthropicCredentialIdentity(account)
		if identity == "" {
			identity, _ = account.Extra[schedulingQuotaAnthropicIdentityKey].(string)
		}
		if identity == "" {
			return ""
		}
		observations = storedAnthropicObservationsForIdentity(account, identity)
	case account.IsOpenAI() && (account.Type == AccountTypeOAuth || account.Type == AccountTypeSetupToken):
		if !openAICodexSnapshotIdentityTrusted(account) {
			return ""
		}
	default:
		return ""
	}
	for _, spec := range []struct {
		name, anthropic string
		duration        time.Duration
	}{
		{"5h", "five_hour", 5 * time.Hour}, {"7d", "seven_day", 7 * 24 * time.Hour},
	} {
		threshold := schedulingQuotaWindowThreshold(ctx, account, thresholds, spec.name, now, policy)
		if threshold <= 0 {
			continue
		}
		var sample schedulingQuotaObservation
		if observations != nil {
			used, reset := observations[spec.anthropic+".utilization"], observations[spec.anthropic+".resets_at"]
			// Same response/query and source, not two fields from different windows.
			if used.SampledAt == nil || reset.SampledAt == nil || !used.SampledAt.Equal(*reset.SampledAt) || used.Source != reset.Source || used.Value == nil {
				continue
			}
			value, valid := resolveAccountExtraNumber(map[string]any{"used": used.Value}, "used")
			until := parseSchedulingResetAt(reset.Value)
			if !valid || until == nil {
				continue
			}
			sample = schedulingQuotaObservation{UsedPercent: value, ResetAt: *until, SampledAt: *used.SampledAt}
		} else {
			prefix := "codex_" + spec.name + "_"
			observation := account.Extra[prefix+"quota_observation"]
			if observation == nil {
				continue
			}
			raw, err := json.Marshal(observation)
			if err != nil || json.Unmarshal(raw, &sample) != nil {
				continue
			}
			used, valid := resolveAccountExtraNumber(account.Extra, prefix+"used_percent")
			reset := parseSchedulingResetAt(account.Extra[prefix+"reset_at"])
			if !valid || reset == nil || used != sample.UsedPercent || !reset.Equal(sample.ResetAt) {
				continue
			}
		}
		if !validSchedulingQuotaPercent(sample.UsedPercent) || sample.SampledAt.IsZero() || sample.SampledAt.After(now) || now.Sub(sample.SampledAt) >= time.Duration(policy.SampleMaxAgeSeconds)*time.Second ||
			!sample.ResetAt.After(now) || sample.ResetAt.After(sample.SampledAt.Add(spec.duration+time.Minute)) {
			continue
		}
		if sample.UsedPercent >= math.Max(0, threshold-policy.CriticalBandPercent) {
			return spec.name
		}
	}
	return ""
}

func validSchedulingQuotaPercent(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}

func schedulingQuotaWindowThreshold(ctx context.Context, account *Account, thresholds map[string]int, window string, now time.Time, policies ...SchedulingQuotaProtection) float64 {
	if account == nil || account.IsSIWCEnabled() {
		return 0
	}
	policy := effectiveSchedulingQuotaProtection(policies...)
	if account.Platform == PlatformAnthropic {
		value := anthropicSchedulingWindowThreshold(account, thresholds, window, policy)
		if value > 0 && value < 100 {
			return float64(value)
		}
		if _, override := accountSchedulingThresholdOverride(account, accountSchedulingThresholdCredentialKey); !override && policy.AnthropicTierThresholdsEnabled && value == 100 {
			return 100
		}
		return 0
	}
	threshold := 0.0
	if value, ok := resolveEffectiveAccountSchedulingThreshold(account, thresholds, account.Platform); ok && value > 0 && value < 100 {
		threshold = float64(value)
	}
	if !account.IsOpenAI() {
		return threshold
	}
	// Reuse native per-window disable/default and reset-credit rules. Do not
	// introduce an overshoot or rewrite the administrator's stopping threshold.
	five, seven := resolveOpenAIQuotaAutoPauseThresholds(ctx, account)
	native := five
	if window == "7d" {
		native = seven
	}
	if resolveAccountExtraBool(account.Extra, "auto_pause_"+window+"_disabled") {
		native = 0
	}
	if reset := ResolveOpenAIAutoResetCreditConfig(account); reset.Enabled {
		stop := reset.Threshold5h
		if window == "7d" {
			stop = reset.Threshold7d
		}
		state := openAIAutoResetStateFromExtra(account.Extra)
		if native <= 0 || stop < native || (state != nil && state.Status == OpenAIAutoResetStatusAvailable && state.AvailableCount > 0 && !openAIAutoResetStateStale(state, now)) {
			native = stop
		}
	}
	if native > 0 && native <= 1 && (threshold == 0 || native*100 < threshold) {
		threshold = native * 100
	}
	return threshold
}

// Bounded positive evidence only. A miss (including another replica or restart)
// is conservatively cold, never permission to bypass quota admission.
var schedulingQuotaPrefixes = struct {
	sync.Mutex
	entries map[[32]byte]schedulingQuotaPrefix
}{entries: make(map[[32]byte]schedulingQuotaPrefix)}

type schedulingQuotaPrefix struct {
	tokens  int
	expires time.Time
}

func schedulingQuotaPrefixKey(risk *schedulingQuotaRequest, account *Account, prefix [32]byte) [32]byte {
	identity := telemetryCredential(account, "account_uuid", "chatgpt_account_id")
	credential := telemetryCredential(account, "access_token", "api_key")
	return sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s:%s:%x", risk.scope, account.ID, identity, telemetryDigest([]byte(credential)), prefix)))
}
func schedulingQuotaUncachedTokens(risk *schedulingQuotaRequest, account *Account) int {
	if risk.scope == "" || account == nil || account.IsSIWCEnabled() {
		return risk.inputTokens
	}
	now := time.Now()
	schedulingQuotaPrefixes.Lock()
	defer schedulingQuotaPrefixes.Unlock()
	for i := len(risk.prefixes) - 1; i >= 0 && i >= len(risk.prefixes)-64; i-- {
		key := schedulingQuotaPrefixKey(risk, account, risk.prefixes[i])
		if entry, ok := schedulingQuotaPrefixes.entries[key]; ok {
			if !entry.expires.After(now) {
				delete(schedulingQuotaPrefixes.entries, key)
				continue
			}
			return max(0, risk.inputTokens-entry.tokens)
		}
	}
	return risk.inputTokens
}
func confirmSchedulingQuotaPrefix(ctx context.Context, account *Account, cached int) {
	if ctx == nil || account == nil || account.IsSIWCEnabled() || cached <= 0 {
		return
	}
	risk, _ := ctx.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest)
	if risk == nil || risk.scope == "" {
		return
	}
	risk.estimate()
	if len(risk.prefixes) == 0 || risk.inputTokens <= 0 {
		return
	}
	now := time.Now()
	schedulingQuotaPrefixes.Lock()
	defer schedulingQuotaPrefixes.Unlock()
	for key, entry := range schedulingQuotaPrefixes.entries {
		if !entry.expires.After(now) {
			delete(schedulingQuotaPrefixes.entries, key)
		}
	}
	if len(schedulingQuotaPrefixes.entries) >= 4096 {
		var oldest [32]byte
		var expiry time.Time
		for key, entry := range schedulingQuotaPrefixes.entries {
			if expiry.IsZero() || entry.expires.Before(expiry) {
				oldest, expiry = key, entry.expires
			}
		}
		delete(schedulingQuotaPrefixes.entries, oldest)
	}
	key := schedulingQuotaPrefixKey(risk, account, risk.prefixes[len(risk.prefixes)-1])
	schedulingQuotaPrefixes.entries[key] = schedulingQuotaPrefix{tokens: min(cached, risk.inputTokens), expires: now.Add(5 * time.Minute)}
}

// The native distributed semaphore owns draining; no second queue or slot book.
func nativeSchedulingConcurrency(ctx context.Context, account *Account, settings *SettingService) int {
	if account == nil {
		return 0
	}
	if account.IsSIWCEnabled() || account.Concurrency == 1 || ctx == nil || ctx.Value(schedulingQuotaRequestKey{}) == nil {
		return account.Concurrency
	}
	if account.Platform != PlatformAnthropic && account.Platform != PlatformOpenAI {
		return account.Concurrency
	}
	if risk, _ := ctx.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest); risk != nil && account.IsExcelBPSEnabledForModel(gjson.GetBytes(risk.body, "model").String()) {
		return account.Concurrency
	}
	var thresholds map[string]int
	if settings != nil {
		thresholds, _ = settings.GetAccountSchedulingThresholds(ctx)
	}
	if schedulingQuotaCriticalWindow(ctx, account, thresholds, time.Now(), settings.GetSchedulingQuotaProtection(ctx)) != "" {
		return 1
	}
	return account.Concurrency
}
