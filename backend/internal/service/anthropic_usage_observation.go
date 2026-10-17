package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const anthropicObservationPrefix = "anthropic_observation_"

// UsageObservation keeps provenance with each independently updated value. A nil
// sample time is deliberately unknown, never the time an old record was read.
type UsageObservation struct {
	Value     any        `json:"value"`
	Source    string     `json:"source"`
	SampledAt *time.Time `json:"sampled_at"`
}

type AnthropicUsageDetails struct {
	ProfileAccess string                      `json:"profile_access"`
	Observations  map[string]UsageObservation `json:"observations"`
	Identity      string                      `json:"-"`
}

type ClaudeExtraUsage struct {
	IsEnabled    *bool    `json:"is_enabled"`
	MonthlyLimit *float64 `json:"monthly_limit"`
	UsedCredits  *float64 `json:"used_credits"`
	Utilization  *float64 `json:"utilization"`
}

type ClaudeUsageLimit struct {
	Kind     string          `json:"kind"`
	Group    string          `json:"group,omitempty"`
	Percent  *float64        `json:"percent,omitempty"`
	ResetsAt *string         `json:"resets_at,omitempty"`
	Severity string          `json:"severity,omitempty"`
	IsActive *bool           `json:"is_active,omitempty"`
	Scope    json.RawMessage `json:"scope,omitempty"`
}

type ClaudeProfileResponse struct {
	Organization map[string]json.RawMessage `json:"organization"`
	Account      map[string]json.RawMessage `json:"account"`
}

// Optional extension of the existing fetcher: profile failures must not discard
// a successful usage response or force other providers/test doubles to implement it.
type claudeProfileFetcher interface {
	FetchProfileWithOptions(context.Context, *ClaudeUsageFetchOptions) (*ClaudeProfileResponse, error)
}

type ClaudeUsageHTTPError struct{ StatusCode int }

func (e *ClaudeUsageHTTPError) Error() string {
	return "Anthropic account query returned HTTP " + strconv.Itoa(e.StatusCode)
}

func anthropicQueryErrorCode(err error) string {
	var upstream *ClaudeUsageHTTPError
	if errors.As(err, &upstream) {
		return "http_" + strconv.Itoa(upstream.StatusCode)
	}
	return "query_failed"
}

func (s *AccountUsageService) anthropicUsageQueryFallback(ctx context.Context, a *Account, err error) (*UsageInfo, error) {
	info, fallbackErr := s.getPassiveUsageForAccount(ctx, a)
	if fallbackErr != nil {
		return nil, err
	}
	info.ErrorCode = anthropicQueryErrorCode(err)
	info.Error = "Anthropic usage query failed; showing stored observations (" + info.ErrorCode + ")"
	return info, nil
}

func (r *ClaudeUsageResponse) UnmarshalJSON(data []byte) error {
	type plain ClaudeUsageResponse
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = ClaudeUsageResponse(decoded)
	return json.Unmarshal(data, &r.RawFields)
}

func anthropicCredentialIdentity(a *Account) string {
	identity := a.GetCredential("org_uuid") + "|" + a.GetCredential("account_uuid")
	if a.GetCredential("org_uuid") == "" || a.GetCredential("account_uuid") == "" {
		identity = a.GetCredential("access_token")
	}
	if identity == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

func anthropicProfileAccess(a *Account) string {
	if a.Type == AccountTypeSetupToken {
		return "denied"
	}
	if strings.TrimSpace(a.GetCredential("scope")) == "" {
		return "unknown"
	}
	if a.HasOAuthScope("user:profile") {
		return "granted"
	}
	return "denied"
}

func putUsageObservation(fields map[string]UsageObservation, key string, value any, source string, at *time.Time) {
	fields[key] = UsageObservation{Value: value, Source: source, SampledAt: at}
}

// Individual JSONB keys let sparse header samples and active queries update
// unrelated fields without replacing each other's observation maps.
func persistableAnthropicObservations(fields map[string]UsageObservation, identity string) map[string]any {
	updates := make(map[string]any, len(fields))
	for key, observation := range fields {
		if observation.Source != "upstream_headers" && observation.Source != "upstream_api" {
			continue
		}
		updates[anthropicObservationPrefix+key] = struct {
			UsageObservation
			Identity string `json:"identity,omitempty"`
		}{observation, identity}
	}
	return updates
}

func storedAnthropicObservations(a *Account) map[string]UsageObservation {
	fields := make(map[string]UsageObservation)
	identity := anthropicCredentialIdentity(a)
	for key, raw := range a.Extra {
		if !strings.HasPrefix(key, anthropicObservationPrefix) {
			continue
		}
		data, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var stored struct {
			UsageObservation
			Identity string `json:"identity"`
		}
		if json.Unmarshal(data, &stored) != nil || stored.Identity != identity {
			continue
		}
		if stored.Source != "upstream_headers" && stored.Source != "upstream_api" {
			continue
		}
		fields[strings.TrimPrefix(key, anthropicObservationPrefix)] = stored.UsageObservation
	}
	return fields
}

func anthropicHeaderObservations(headers http.Header, at time.Time) map[string]UsageObservation {
	fields := make(map[string]UsageObservation)
	for wire, name := range map[string]string{"5h": "five_hour", "7d": "seven_day", "7d_oi": "seven_day_fable", "overage": "overage", "": "limit"} {
		prefix := "anthropic-ratelimit-unified-"
		if wire != "" {
			prefix += wire + "-"
		}
		for _, key := range []string{"status", "surpassed-threshold", "disabled-reason", "representative-claim", "fallback-percentage", "utilization", "reset"} {
			raw := strings.TrimSpace(headers.Get(prefix + key))
			if raw == "" || len(raw) > 128 {
				continue
			}
			var value any = raw
			field := strings.ReplaceAll(key, "-", "_")
			switch key {
			case "utilization", "fallback-percentage":
				n, err := strconv.ParseFloat(raw, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
					continue
				}
				value = n
				if key == "utilization" {
					if math.IsInf(n*100, 0) {
						continue
					}
					value = n * 100
				}
			case "reset":
				ts, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || ts <= 0 {
					continue
				}
				if ts > 1e11 {
					ts /= 1000
				}
				if ts > at.Add(366*24*time.Hour).Unix() {
					continue
				}
				value = time.Unix(ts, 0).UTC().Format(time.RFC3339)
				field = "resets_at"
			}
			putUsageObservation(fields, name+"."+field, value, "upstream_headers", &at)
		}
	}
	return fields
}

func validUsageNumber(n *float64) bool {
	return n != nil && !math.IsNaN(*n) && !math.IsInf(*n, 0) && *n >= 0
}

// A model scope can never turn a scoped meter into an account-wide limit.
func canonicalAnthropicLimit(row ClaudeUsageLimit) string {
	var scope map[string]json.RawMessage
	_ = json.Unmarshal(row.Scope, &scope)
	if len(scope) == 0 {
		switch row.Kind {
		case "session":
			return "five_hour"
		case "weekly_all":
			return "seven_day"
		}
		return ""
	}
	if row.Kind != "weekly_scoped" || len(scope) != 1 {
		return ""
	}
	var model struct {
		ID   string `json:"id"`
		Name string `json:"display_name"`
	}
	if json.Unmarshal(scope["model"], &model) != nil {
		return ""
	}
	for _, family := range []string{"fable", "sonnet", "opus"} {
		if strings.HasPrefix(strings.ToLower(model.ID), "claude-"+family+"-") || (model.ID == "" && strings.EqualFold(model.Name, family)) {
			return "seven_day_" + family
		}
	}
	return ""
}

func normalizeAnthropicLimits(rows []ClaudeUsageLimit) []ClaudeUsageLimit {
	result := make([]ClaudeUsageLimit, 0, len(rows))
	for _, row := range rows {
		if len(result) == 32 {
			break
		}
		if row.Kind == "" || len(row.Kind) > 128 || len(row.Group) > 128 || len(row.Severity) > 128 || len(row.Scope) > 2048 {
			continue
		}
		if !validUsageNumber(row.Percent) {
			row.Percent = nil
		}
		if row.ResetsAt != nil {
			if parsed, err := parseTime(*row.ResetsAt); err == nil {
				value := parsed.UTC().Format(time.RFC3339)
				row.ResetsAt = &value
			} else {
				row.ResetsAt = nil
			}
		}
		// Only harmless scope descriptors cross the API; arbitrary upstream fields do not.
		var scopes map[string]json.RawMessage
		if json.Unmarshal(row.Scope, &scopes) == nil && len(scopes) > 0 {
			clean := make(map[string]map[string]string)
			for kind, raw := range scopes {
				if kind != "model" && kind != "surface" {
					clean["unknown"] = map[string]string{}
					continue
				}
				var attrs map[string]any
				if json.Unmarshal(raw, &attrs) != nil {
					clean[kind] = map[string]string{}
					continue
				}
				clean[kind] = map[string]string{}
				for _, key := range []string{"id", "display_name"} {
					if value, ok := attrs[key].(string); ok && len(value) <= 128 {
						clean[kind][key] = value
					}
				}
			}
			row.Scope, _ = json.Marshal(clean)
		} else if len(row.Scope) > 0 && string(row.Scope) != "null" && string(row.Scope) != "{}" {
			row.Scope = json.RawMessage(`{"unknown":{}}`)
		}
		result = append(result, row)
	}
	return result
}

func collectAnthropicAPIObservations(resp *ClaudeUsageResponse, at *time.Time) map[string]UsageObservation {
	fields := make(map[string]UsageObservation)
	// Programmatic fetchers have no raw JSON presence map. Preserve only populated
	// legacy windows; never manufacture observations for omitted zero-value fields.
	if resp.RawFields == nil {
		for name, window := range map[string]ClaudeUsageWindow{
			"five_hour":        {Utilization: resp.FiveHour.Utilization, ResetsAt: resp.FiveHour.ResetsAt},
			"seven_day":        {Utilization: resp.SevenDay.Utilization, ResetsAt: resp.SevenDay.ResetsAt},
			"seven_day_sonnet": {Utilization: resp.SevenDaySonnet.Utilization, ResetsAt: resp.SevenDaySonnet.ResetsAt},
			"seven_day_fable":  resp.SevenDayOverageIncluded, "seven_day_opus": resp.SevenDayOpus,
		} {
			if window.ResetsAt == "" && window.Utilization == 0 {
				continue
			}
			if validUsageNumber(&window.Utilization) {
				putUsageObservation(fields, name+".utilization", window.Utilization, "upstream_api", at)
			}
			if reset, err := parseTime(window.ResetsAt); err == nil {
				putUsageObservation(fields, name+".resets_at", reset.UTC().Format(time.RFC3339), "upstream_api", at)
			}
		}
	}
	for wire, name := range map[string]string{"five_hour": "five_hour", "seven_day": "seven_day", "seven_day_sonnet": "seven_day_sonnet", "seven_day_opus": "seven_day_opus", "seven_day_overage_included": "seven_day_fable", "extra_usage": "extra_usage"} {
		raw, present := resp.RawFields[wire]
		if !present {
			continue
		}
		keys := []string{"utilization", "resets_at"}
		if wire == "extra_usage" {
			keys = []string{"is_enabled", "monthly_limit", "used_credits", "utilization"}
		}
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			continue
		}
		for _, key := range keys {
			v, ok := values[key]
			if !ok && values != nil {
				continue
			}
			var value any
			if v != nil && json.Unmarshal(v, &value) != nil {
				continue
			}
			if value != nil {
				switch key {
				case "resets_at":
					str, ok := value.(string)
					if !ok {
						continue
					}
					parsed, err := parseTime(str)
					if err != nil {
						continue
					}
					value = parsed.UTC().Format(time.RFC3339)
				case "is_enabled":
					if _, ok := value.(bool); !ok {
						continue
					}
				default:
					n, ok := value.(float64)
					if !ok || !validUsageNumber(&n) {
						continue
					}
				}
			}
			putUsageObservation(fields, name+"."+key, value, "upstream_api", at)
		}
	}
	if _, present := resp.RawFields["limits"]; present || resp.Limits != nil {
		rows := normalizeAnthropicLimits(resp.Limits)
		putUsageObservation(fields, "limits", rows, "upstream_api", at)
		counts := make(map[string]int)
		for _, row := range rows {
			counts[canonicalAnthropicLimit(row)]++
		}
		for _, row := range rows {
			name := canonicalAnthropicLimit(row)
			if name == "" || counts[name] != 1 {
				continue
			}
			// A present modern row is authoritative even when its optional values
			// are null: an idle/reset window must not inherit an older reset time.
			putUsageObservation(fields, name+".utilization", nil, "upstream_api", at)
			putUsageObservation(fields, name+".resets_at", nil, "upstream_api", at)
			if row.Percent != nil {
				putUsageObservation(fields, name+".utilization", *row.Percent, "upstream_api", at)
			}
			if row.ResetsAt != nil {
				putUsageObservation(fields, name+".resets_at", *row.ResetsAt, "upstream_api", at)
			}
			if row.Severity != "" {
				putUsageObservation(fields, name+".severity", row.Severity, "upstream_api", at)
			}
		}
	}
	if resp.Profile != nil {
		for key, raw := range resp.Profile.Organization {
			switch key {
			case "organization_type", "rate_limit_tier", "billing_type", "subscription_status", "has_extra_usage_enabled":
			default:
				continue
			}
			var value any
			if json.Unmarshal(raw, &value) != nil {
				continue
			}
			if value != nil {
				if key == "has_extra_usage_enabled" {
					if _, ok := value.(bool); !ok {
						continue
					}
				} else if str, ok := value.(string); !ok || len(str) > 128 {
					continue
				}
			}
			putUsageObservation(fields, "profile."+key, value, "upstream_api", at)
		}
		for _, key := range []string{"has_claude_max", "has_claude_pro"} {
			var value bool
			if raw, ok := resp.Profile.Account[key]; ok && string(raw) != "null" && json.Unmarshal(raw, &value) == nil {
				putUsageObservation(fields, "profile."+key, value, "upstream_api", at)
			}
		}
		putUsageObservation(fields, "profile.query_status", "ok", "upstream_api", at)
	} else if resp.ProfileError != "" {
		putUsageObservation(fields, "profile.query_status", resp.ProfileError, "upstream_api", at)
	}
	return fields
}

func enrichAnthropicUsage(a *Account, info *UsageInfo) {
	if !supportsAnthropicPassiveUsage(a) {
		return
	}
	fields := storedAnthropicObservations(a)
	// Replaced credentials must not inherit observations from the prior identity.
	identityChanged := false
	if len(fields) == 0 {
		for key := range a.Extra {
			if strings.HasPrefix(key, anthropicObservationPrefix) {
				identityChanged = true
				break
			}
		}
	}
	if identityChanged {
		info.FiveHour, info.SevenDay, info.SevenDaySonnet, info.SevenDayFable = nil, nil, nil, nil
	}
	if info.Anthropic != nil {
		for key, fresh := range info.Anthropic.Observations {
			old, exists := fields[key]
			if !exists || old.SampledAt == nil || (fresh.SampledAt != nil && !fresh.SampledAt.Before(*old.SampledAt)) {
				fields[key] = fresh
			}
		}
	}
	info.Anthropic = &AnthropicUsageDetails{ProfileAccess: anthropicProfileAccess(a), Observations: fields, Identity: anthropicCredentialIdentity(a)}
	putUsageObservation(fields, "authorization.scopes", strings.Fields(a.GetCredential("scope")), "local_credentials", nil)
	putUsageObservation(fields, "authorization.profile_access", info.Anthropic.ProfileAccess, "local_credentials", nil)
	putUsageObservation(fields, "authorization.refreshable", a.Type == AccountTypeOAuth && a.GetCredential("refresh_token") != "", "local_credentials", nil)
	if expires := a.GetCredentialAsTime("expires_at"); expires != nil {
		putUsageObservation(fields, "authorization.expires_at", expires.UTC().Format(time.RFC3339), "local_credentials", nil)
	}

	for _, window := range []struct {
		name     string
		progress **UsageProgress
	}{
		{"five_hour", &info.FiveHour}, {"seven_day", &info.SevenDay}, {"seven_day_sonnet", &info.SevenDaySonnet}, {"seven_day_fable", &info.SevenDayFable},
	} {
		utilKey, resetKey := window.name+".utilization", window.name+".resets_at"
		progress := *window.progress
		if progress != nil {
			// Old installations did not track provenance per field. Do not relabel
			// those values with the global timestamp of an unrelated header sample.
			if _, exists := fields[utilKey]; !exists {
				var value any = progress.Utilization
				source := "stored_snapshot"
				if window.name == "five_hour" {
					if raw, ok := a.Extra["session_window_utilization"]; !ok || raw == nil {
						source = "estimate"
						if a.SessionWindowEnd == nil {
							value = nil
						}
					}
				}
				putUsageObservation(fields, utilKey, value, source, nil)
			}
			if _, exists := fields[resetKey]; !exists && progress.ResetsAt != nil {
				putUsageObservation(fields, resetKey, progress.ResetsAt.UTC().Format(time.RFC3339), "stored_snapshot", nil)
			}
		}
		util, hasUtil := fields[utilKey]
		reset, hasReset := fields[resetKey]
		if !hasUtil && !hasReset {
			continue
		}
		if progress == nil {
			progress = &UsageProgress{}
			*window.progress = progress
		}
		if hasUtil {
			progress.Utilization = parseExtraFloat64(util.Value)
		}
		if hasReset {
			progress.ResetsAt, progress.RemainingSeconds = nil, 0
			if raw, ok := reset.Value.(string); ok {
				if parsed, err := parseTime(raw); err == nil {
					if time.Now().Before(parsed) {
						progress.ResetsAt = &parsed
						progress.RemainingSeconds = int(time.Until(parsed).Seconds())
					} else {
						now := time.Now().UTC()
						progress.Utilization = 0
						putUsageObservation(fields, utilKey, 0, "estimate", &now)
					}
				}
			}
		}
	}
}

func annotateAnthropicWindowStats(info *UsageInfo, duration time.Duration, stats *WindowStats, sampledAt time.Time) {
	if info.Anthropic == nil || stats == nil {
		return
	}
	name := "five_hour"
	if duration == 7*24*time.Hour {
		name = "seven_day"
	}
	fields := info.Anthropic.Observations
	putUsageObservation(fields, name+".window_stats", stats, "local_statistics", &sampledAt)
	if name != "seven_day" || info.SevenDay == nil || stats.Cost <= 0 {
		return
	}
	util := info.SevenDay.Utilization
	if util <= 0 {
		return
	}
	value := stats.Cost * 100 / util
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	now := time.Now().UTC()
	putUsageObservation(fields, name+".estimated_total_cost", value, "estimate", &now)
}
