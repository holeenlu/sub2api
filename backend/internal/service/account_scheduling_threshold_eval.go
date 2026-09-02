package service

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// AccountSchedulingThresholdDecision captures the pure pause decision for one account.
type AccountSchedulingThresholdDecision struct {
	ShouldPause bool
	// HasEvidence 区分「确认没越线」与「这份账号副本上没有判据」。ShouldPause=false
	// 两种情况都会出现，但只有前者可以据此解除已有限流：采样缺失时解除等于放任一个
	// 可能已越线的账号继续接请求。
	HasEvidence      bool
	Platform         string
	Window           string
	Scope            string
	ThresholdPercent int
	UsedPercent      float64
	Until            *time.Time
	// UntilSource 见 accountSchedulingThresholdCandidate.untilSource。
	UntilSource string
}

type accountSchedulingThresholdCandidate struct {
	window      string
	scope       string
	usedPercent float64
	until       *time.Time
	// untilSource 标记 until 并非取自本窗口自己的 reset 采样，而是回退来源的窗口名。
	// 只在回退时非空，写进限流 reason 供排障时区分"真的封到 7d_oi 重置"与"7d_oi
	// 没有 reset，借用了 7d 的重置时刻"。
	untilSource string
}

const accountSchedulingThresholdCredentialKey = "account_scheduling_threshold"

// anthropicFableSchedulingThresholdCredentialKey 是 Fable 阈值的账号级覆盖键，
// 与全局的 SchedulingThresholdScopeAnthropicFable 一一对应。
const anthropicFableSchedulingThresholdCredentialKey = "anthropic_fable_scheduling_threshold"

// EvaluateAccountSchedulingThreshold evaluates whether an account should be paused
// based on the current per-platform scheduling threshold snapshot.
func EvaluateAccountSchedulingThreshold(account *Account, thresholds map[string]int, now time.Time) AccountSchedulingThresholdDecision {
	decision := AccountSchedulingThresholdDecision{}
	if account == nil {
		return decision
	}

	decision.Platform = strings.ToLower(strings.TrimSpace(account.Platform))
	if decision.Platform == "" {
		return decision
	}
	if !isAllowedSchedulingThresholdPlatform(decision.Platform) {
		return decision
	}

	threshold, ok := resolveEffectiveAccountSchedulingThreshold(account, thresholds, decision.Platform)
	decision.ThresholdPercent = threshold
	if !ok || threshold >= 100 {
		return decision
	}

	var winner *accountSchedulingThresholdCandidate
	switch decision.Platform {
	case PlatformOpenAI:
		winner = pickLatestResetSchedulingCandidate(openAIThresholdCandidates(account, now), threshold, now)
	case PlatformAnthropic:
		winner = pickLatestResetSchedulingCandidate(anthropicThresholdCandidates(account), threshold, now)
	case PlatformGrok:
		winner = pickLatestResetSchedulingCandidate(grokThresholdCandidates(account), threshold, now)
	case PlatformKimi:
		winner = pickLatestResetSchedulingCandidate(cnProviderThresholdCandidates(account, PlatformKimi), threshold, now)
	case PlatformZhipu:
		winner = pickLatestResetSchedulingCandidate(cnProviderThresholdCandidates(account, PlatformZhipu), threshold, now)
	default:
		return decision
	}

	if winner == nil {
		return decision
	}

	decision.ShouldPause = true
	decision.Window = winner.window
	decision.Scope = winner.scope
	decision.UsedPercent = winner.usedPercent
	decision.Until = winner.until
	return decision
}

// evaluateAnthropicFableSchedulingThreshold 求值 Fable 停调。
//
// thresholdsResolved 报告 thresholds 是否真的来自配置：读取失败时上游给的是全 100 的
// 兜底默认值，取值与「运维关掉了阈值」完全一样，只有这个标志能把两者分开。
func evaluateAnthropicFableSchedulingThreshold(account *Account, thresholds map[string]int, thresholdsResolved bool, now time.Time) AccountSchedulingThresholdDecision {
	decision := AccountSchedulingThresholdDecision{}
	if account == nil || !strings.EqualFold(strings.TrimSpace(account.Platform), PlatformAnthropic) {
		return decision
	}

	decision.Platform = PlatformAnthropic
	threshold, ok := resolveEffectiveAnthropicFableSchedulingThreshold(account, thresholds)
	decision.ThresholdPercent = threshold
	if !ok || threshold >= 100 {
		// 配置本身就是判据：阈值没开或不再收紧，之前按阈值打的限流该解除。但这只在
		// 确实读到了配置时成立——读取失败的兜底默认恰好也是全 100，当成判据就等于
		// 「配置系统抖一下 = 运维关掉阈值」，会对全池执行一次解除。
		decision.HasEvidence = thresholdsResolved
		return decision
	}

	candidates := anthropicFableThresholdCandidates(account)
	decision.HasEvidence = anthropicFableThresholdReleaseEvidence(candidates, threshold)

	winner := pickLatestResetSchedulingCandidate(candidates, threshold, now)
	if winner == nil {
		return decision
	}

	decision.ShouldPause = true
	decision.Window = winner.window
	decision.Scope = winner.scope
	decision.UsedPercent = winner.usedPercent
	decision.Until = winner.until
	decision.UntilSource = winner.untilSource
	return decision
}

func isAllowedSchedulingThresholdPlatform(platform string) bool {
	for _, allowed := range AllowedSchedulingThresholdPlatforms {
		if platform == allowed {
			return true
		}
	}
	return false
}

func resolveEffectiveAccountSchedulingThreshold(account *Account, thresholds map[string]int, platform string) (int, bool) {
	if threshold, ok := accountSchedulingThresholdOverride(account, accountSchedulingThresholdCredentialKey); ok {
		return threshold, true
	}
	return lookupAccountSchedulingThreshold(thresholds, platform)
}

// resolveEffectiveAnthropicFableSchedulingThreshold 计算 Fable 停调的有效阈值：
// 显式启用了 Fable 阈值就独立使用它，否则沿用 Anthropic 通用阈值。
//
// 不与通用阈值取更严者。两者约束的窗口撞满后代价并不对称：Fable 专属的 7d_oi 撞满
// 只让上游对 Fable 返 429，账号继续服务其他模型；5h/7d 撞满则是账号级硬限流，全模型
// 停最长七天。也就是说 7d_oi 的余量用不完就是白白浪费，把它截在通用阈值处没有收益。
// 运维要表达「账号在 7d 70% 停，但 Fable 可以用满专属窗口的 95%」时，取更严者会让
// 后者悄悄变成 70。
//
// 向后兼容：Fable 阈值未启用（未配置或 >=100）时沿用通用阈值，与本 scope 引入前的
// 行为逐字相同。两侧各自支持账号级 credentials 覆盖，互不影响。
func resolveEffectiveAnthropicFableSchedulingThreshold(account *Account, thresholds map[string]int) (int, bool) {
	if fable, ok := resolveAnthropicFableSchedulingThresholdScope(account, thresholds); ok && fable > 0 && fable < 100 {
		return fable, true
	}
	return resolveEffectiveAccountSchedulingThreshold(account, thresholds, PlatformAnthropic)
}

func resolveAnthropicFableSchedulingThresholdScope(account *Account, thresholds map[string]int) (int, bool) {
	if threshold, ok := accountSchedulingThresholdOverride(account, anthropicFableSchedulingThresholdCredentialKey); ok {
		return threshold, true
	}
	return lookupAccountSchedulingThreshold(thresholds, SchedulingThresholdScopeAnthropicFable)
}

// accountSchedulingThresholdOverride 读取账号级阈值覆盖。通用阈值与 Fable 阈值各有
// 一个覆盖键，除键名外解析规则完全相同，因此按键参数化而不是各写一份。
func accountSchedulingThresholdOverride(account *Account, credentialKey string) (int, bool) {
	if account == nil || len(account.Credentials) == 0 {
		return 0, false
	}
	raw, ok := account.Credentials[credentialKey]
	if !ok {
		return 0, false
	}
	return parseAccountSchedulingThresholdValue(raw)
}

func parseAccountSchedulingThresholdValue(raw any) (int, bool) {
	var value int
	switch v := raw.(type) {
	case int:
		value = v
	case int64:
		value = int(v)
	case float64:
		value = int(math.Round(v))
	case float32:
		value = int(math.Round(float64(v)))
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return 0, false
		}
		value = int(math.Round(parsed))
	case string:
		raw := strings.TrimSpace(v)
		parsed, err := strconv.Atoi(raw)
		if err == nil {
			value = parsed
			break
		}
		parsedFloat, floatErr := strconv.ParseFloat(raw, 64)
		if floatErr != nil {
			return 0, false
		}
		value = int(math.Round(parsedFloat))
	default:
		return 0, false
	}
	if value < 1 || value > 100 {
		return 0, false
	}
	return value, true
}

func lookupAccountSchedulingThreshold(thresholds map[string]int, platform string) (int, bool) {
	if len(thresholds) == 0 {
		return 0, false
	}
	value, ok := thresholds[platform]
	return value, ok
}

func openAIThresholdCandidates(account *Account, now time.Time) []*accountSchedulingThresholdCandidate {
	if account == nil {
		return nil
	}
	if !openAICodexSnapshotIdentityTrusted(account) {
		return nil
	}
	return []*accountSchedulingThresholdCandidate{
		openAIThresholdCandidate(account.Extra, "5h", now),
		openAIThresholdCandidate(account.Extra, "7d", now),
	}
}

func openAICodexSnapshotIdentityTrusted(account *Account) bool {
	if account == nil || !account.IsOpenAIOAuth() || len(account.Extra) == 0 {
		return true
	}

	if identityValuesConflict(
		firstStringValue(account.Credentials, "email"),
		firstStringValue(account.Extra, "email", "email_address"),
	) {
		return false
	}
	if identityValuesConflict(
		firstStringValue(account.Credentials, "chatgpt_account_id"),
		firstStringValue(account.Extra, "chatgpt_account_id", "account_id"),
	) {
		return false
	}
	if identityValuesConflict(
		firstStringValue(account.Credentials, "workspace_id", "chatgpt_workspace_id", "organization_id", "org_id"),
		firstStringValue(account.Extra, "workspace_id", "chatgpt_workspace_id", "organization_id", "org_id"),
	) {
		return false
	}
	return true
}

func identityValuesConflict(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	return left != "" && right != "" && !strings.EqualFold(left, right)
}

// firstStringValue returns the first non-empty string among the given map keys.
// Used by OpenAI codex snapshot identity matching for scheduling thresholds.
func firstStringValue(values map[string]any, keys ...string) string {
	if len(values) == 0 {
		return ""
	}
	for _, key := range keys {
		raw, ok := values[key]
		if !ok || raw == nil {
			continue
		}
		switch typed := raw.(type) {
		case string:
			if v := strings.TrimSpace(typed); v != "" {
				return v
			}
		default:
			if v := strings.TrimSpace(stringValue(raw)); v != "" {
				return v
			}
		}
	}
	return ""
}

func openAIThresholdCandidate(extra map[string]any, window string, now time.Time) *accountSchedulingThresholdCandidate {
	if len(extra) == 0 {
		return nil
	}

	var (
		usedPercentKey string
		resetAtKey     string
	)
	switch window {
	case "5h":
		usedPercentKey = "codex_5h_used_percent"
		resetAtKey = "codex_5h_reset_at"
	case "7d":
		usedPercentKey = "codex_7d_used_percent"
		resetAtKey = "codex_7d_reset_at"
	default:
		return nil
	}

	usedPercent, ok := extra[usedPercentKey]
	if !ok {
		return nil
	}
	if openAIQuotaWindowReset(extra, window, now) || openAICodexSnapshotStaleForPause(extra, now) {
		return nil
	}
	return &accountSchedulingThresholdCandidate{
		window:      window,
		usedPercent: schedulingPercentValue(usedPercent),
		until:       parseSchedulingResetAt(extra[resetAtKey]),
	}
}

func anthropicThresholdCandidates(account *Account) []*accountSchedulingThresholdCandidate {
	if account == nil {
		return nil
	}

	var candidates []*accountSchedulingThresholdCandidate
	if usedPercent := utilizationAsPercent(account.Extra["session_window_utilization"]); usedPercent > 0 {
		candidates = append(candidates, &accountSchedulingThresholdCandidate{
			window:      "5h",
			usedPercent: usedPercent,
			until:       cloneTimePtr(account.SessionWindowEnd),
		})
	}
	if usedPercent := utilizationAsPercent(account.Extra["passive_usage_7d_utilization"]); usedPercent > 0 {
		candidates = append(candidates, &accountSchedulingThresholdCandidate{
			window:      "7d",
			usedPercent: usedPercent,
			until:       parseSchedulingResetAt(account.Extra["passive_usage_7d_reset"]),
		})
	}
	return candidates
}

// anthropicFableThresholdCandidates 只从 Fable 专属 7d_oi 窗口构造模型级停调候选。
// Anthropic 共享 7d 属于账号级预算，只能由普通 anthropic 阈值处理；把它混进这里会让
// 与 Fable 无关的 Sonnet/Haiku 用量在 Fable 自己几乎没使用时仍触发 CFable5。
//
// 有采样就产出候选，用量为 0 也不例外：0 同样是有效判据，解除判定要靠它。until
// 缺失时打不了限流，但用量仍可证明已有阈值限流是否应该解除。
func anthropicFableThresholdCandidates(account *Account) []*accountSchedulingThresholdCandidate {
	if account == nil {
		return nil
	}

	// 7d_oi 的 reset 采样缺失时（上游未下发 anthropic-ratelimit-unified-7d_oi-reset 头）
	// 退回同为 7 天窗口的 passive_usage_7d_reset，与 429 路径缺 7d_oi reset 时退回聚合
	// reset 的处理同构。两者都没有就留空 until——宁可不停调，也不用一个凭空捏造的解除
	// 时间把账号的 Fable 封住。
	if usedPercent, ok := utilizationAsPercentValue(account.Extra["passive_usage_7d_oi_utilization"]); ok {
		until := parseSchedulingResetAt(account.Extra["passive_usage_7d_oi_reset"])
		untilSource := ""
		if until == nil {
			until = parseSchedulingResetAt(account.Extra["passive_usage_7d_reset"])
			if until != nil {
				untilSource = "7d"
			}
		}
		return []*accountSchedulingThresholdCandidate{{
			window:      "7d_oi",
			scope:       anthropicFableRateLimitKey,
			usedPercent: usedPercent,
			until:       until,
			untilSource: untilSource,
		}}
	}
	return nil
}

// anthropicFableThresholdReleaseEvidence 判断这份账号副本是否足以断言「Fable 没越线」，
// 也就是能不能据此解除按阈值打上的限流。
//
// 要求参与判定的每个窗口都有用量采样，且都低于阈值：
//   - 采样缺失（调度投影裁掉了键、5h 窗口滚动把被动采样清空、账号刚建）时无从断言，
//     另一个窗口可能已经越线，解除等于放它继续接 Fable 请求。此时不解除，限流按自己
//     的 until 自然过期。
//   - 采样存在但用量为 0 是有效判据：从没跑过 Fable 的账号 7d_oi 就是 0。按候选条数
//     算判据会把这类账号永远卡住——运维把阈值从 50 调宽到 70 也解除不了，只能等最长
//     七天的窗口过期。
//
// reset 采样不参与：它只决定"封到什么时候"。窗口内用量只增不减，采样陈旧只会高估
// 用量，因此"采样低于阈值"这个结论不会因为缺 reset 而变得不安全。
func anthropicFableThresholdReleaseEvidence(candidates []*accountSchedulingThresholdCandidate, threshold int) bool {
	if len(candidates) < 1 {
		return false
	}
	for _, candidate := range candidates {
		if candidate == nil || candidate.usedPercent >= float64(threshold) {
			return false
		}
	}
	return true
}

// NOTE: Gemini / Kiro / Antigravity are intentionally NOT threshold-pausing
// platforms (see AllowedSchedulingThresholdPlatforms and the evaluator switch,
// asserted by TestEvaluateAccountSchedulingThreshold_UnsupportedPlatformsDoNotPause).
// Their former per-platform candidate readers were dead code — never reachable
// from EvaluateAccountSchedulingThreshold — and have been removed to avoid the
// false impression that configuring a threshold for them has any effect. The
// kiro_sched_* / antigravity_sched_* extras are still written purely as
// observability snapshots.

// grokThresholdCandidates uses only header-projected
// grok_sched_utilization / grok_sched_reset_at (rolling quota window, reset
// capped at ~25h when written). Official billing 7d/30d windows are not used
// for auto-pause here.
func grokThresholdCandidates(account *Account) []*accountSchedulingThresholdCandidate {
	if account == nil {
		return nil
	}
	return []*accountSchedulingThresholdCandidate{
		{
			window:      "quota",
			scope:       "grok",
			usedPercent: schedulingPercentValue(account.Extra["grok_sched_utilization"]),
			until:       parseSchedulingResetAt(account.Extra["grok_sched_reset_at"]),
		},
	}
}

// cnProviderThresholdCandidates 读取国产供应商 Coding Plan 账号的 5h / weekly 滚动窗口
// 用量快照（由 CNProviderQuotaService 写入 account.Extra，键形如
// <provider>_5h_used_percent / <provider>_weekly_reset_at）。payg 账号无此快照，
// 候选为空 → 不触发阈值停调（余额型走余额检测）。与 openai 的快照驱动停调一致：
// 仅当用量超阈值且窗口尚未重置时才停调。
func cnProviderThresholdCandidates(account *Account, provider string) []*accountSchedulingThresholdCandidate {
	if account == nil || len(account.Extra) == 0 {
		return nil
	}
	return []*accountSchedulingThresholdCandidate{
		cnThresholdCandidate(account.Extra, provider, "5h"),
		cnThresholdCandidate(account.Extra, provider, "weekly"),
	}
}

func cnThresholdCandidate(extra map[string]any, provider, window string) *accountSchedulingThresholdCandidate {
	var usedKey, resetKey string
	switch window {
	case "5h":
		usedKey = cnExtraKey(provider, cnExtraSuffix5hUsed)
		resetKey = cnExtraKey(provider, cnExtraSuffix5hReset)
	case "weekly":
		usedKey = cnExtraKey(provider, cnExtraSuffixWeeklyUsed)
		resetKey = cnExtraKey(provider, cnExtraSuffixWeeklyReset)
	default:
		return nil
	}
	usedPercent, ok := extra[usedKey]
	if !ok {
		return nil
	}
	return &accountSchedulingThresholdCandidate{
		window:      window,
		scope:       provider,
		usedPercent: schedulingPercentValue(usedPercent),
		until:       parseSchedulingResetAt(extra[resetKey]),
	}
}

func pickLatestResetSchedulingCandidate(candidates []*accountSchedulingThresholdCandidate, threshold int, now time.Time) *accountSchedulingThresholdCandidate {
	var winner *accountSchedulingThresholdCandidate
	for _, candidate := range candidates {
		if !candidateMatchesThreshold(candidate, threshold, now) {
			continue
		}
		if winner == nil || candidate.until.After(*winner.until) {
			winner = candidate
			continue
		}
		if winner.until.Equal(*candidate.until) && candidate.usedPercent > winner.usedPercent {
			winner = candidate
		}
	}
	return winner
}

func candidateMatchesThreshold(candidate *accountSchedulingThresholdCandidate, threshold int, now time.Time) bool {
	if candidate == nil || candidate.until == nil || !candidate.until.After(now) {
		return false
	}
	return candidate.usedPercent >= float64(threshold)
}

// utilizationFractionCeiling 是「这个值是 0-1 小数」的判定上限。
//
// 写入方（samplePassiveUsageFromHeaders 的响应头采样、syncActiveToPassive 的
// Utilization/100 回写）存的都是 0-1 小数，而窗口撞满时上游会给出略大于 1 的值
// （1.02 = 102%）。以 1.0 为硬边界会把 1.02 读成 1.02%——恰好在越线那一刻把结论
// 反过来。反方向的误判则无害：百分比口径下 1-2 这一段本来就没有意义（1% 的用量
// 触发不了任何阈值），所以整段按小数处理。
const utilizationFractionCeiling = 2.0

func utilizationAsPercent(raw any) float64 {
	value, _ := utilizationAsPercentValue(raw)
	return value
}

// utilizationAsPercentValue 与 utilizationAsPercent 相同，另外报告这份 raw 是否真的是
// 一次用量采样。解除判定必须区分「采样为 0」与「没有采样」——两者都会算出 0。
func utilizationAsPercentValue(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return utilizationFractionAsPercent(v), true
	case float32:
		return utilizationFractionAsPercent(float64(v)), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		value, err := v.Float64()
		if err != nil {
			return 0, false
		}
		if strings.Contains(v.String(), ".") {
			return utilizationFractionAsPercent(value), true
		}
		return value, true
	case string:
		trimmed := strings.TrimSpace(v)
		value, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, false
		}
		if strings.Contains(trimmed, ".") {
			return utilizationFractionAsPercent(value), true
		}
		return value, true
	default:
		return 0, false
	}
}

func utilizationFractionAsPercent(value float64) float64 {
	if value >= 0 && value <= utilizationFractionCeiling {
		return value * 100
	}
	return value
}

func schedulingPercentValue(raw any) float64 {
	switch v := raw.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		value, err := v.Float64()
		if err != nil {
			return 0
		}
		return value
	case string:
		value, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0
		}
		return value
	default:
		return 0
	}
}

func parseSchedulingResetAt(raw any) *time.Time {
	switch v := raw.(type) {
	case nil:
		return nil
	case time.Time:
		ts := v
		return &ts
	case *time.Time:
		return cloneTimePtr(v)
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil
		}
		ts, err := parseSchedulingTime(trimmed)
		if err != nil {
			return nil
		}
		return &ts
	case json.Number:
		if value, err := v.Int64(); err == nil && value > 0 {
			ts := time.Unix(value, 0)
			return &ts
		}
		if value, err := v.Float64(); err == nil && value > 0 {
			ts := time.Unix(int64(value), 0)
			return &ts
		}
	case float64:
		if v > 0 {
			ts := time.Unix(int64(v), 0)
			return &ts
		}
	case float32:
		if v > 0 {
			ts := time.Unix(int64(v), 0)
			return &ts
		}
	case int:
		if v > 0 {
			ts := time.Unix(int64(v), 0)
			return &ts
		}
	case int64:
		if v > 0 {
			ts := time.Unix(v, 0)
			return &ts
		}
	}
	return nil
}

func parseSchedulingTime(raw string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
	}
	for _, format := range formats {
		if ts, err := time.Parse(format, raw); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, strconv.ErrSyntax
}

func cloneTimePtr(src *time.Time) *time.Time {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}
