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
	Observations map[string]UsageObservation `json:"observations"`
	Identity     string                      `json:"-"`
}

type ClaudeUsageLimit struct {
	Kind     string          `json:"kind"`
	Percent  *float64        `json:"percent,omitempty"`
	ResetsAt *string         `json:"resets_at,omitempty"`
	Severity string          `json:"severity,omitempty"`
	IsActive *bool           `json:"is_active,omitempty"`
	Scope    json.RawMessage `json:"scope,omitempty"`
}

type ClaudeUsageHTTPError struct {
	StatusCode int
	Reason     string
}

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

// AnthropicAccountStatus is a display-only projection. It never changes native
// scheduling, credential or account state. Normal metadata is not a warning.
type AnthropicAccountStatus struct {
	ErrorCode        string                `json:"error_code,omitempty"`
	RateLimited      bool                  `json:"rate_limited,omitempty"`
	RateLimitResetAt *time.Time            `json:"rate_limit_reset_at,omitempty"`
	ModelRateLimits  map[string]*time.Time `json:"model_rate_limits,omitempty"`
}

func AnthropicAccountStatusFromAccount(a *Account) *AnthropicAccountStatus {
	if !supportsAnthropicPassiveUsage(a) {
		return nil
	}
	return anthropicAccountStatus(storedAnthropicObservations(a), time.Now())
}

func anthropicAccountStatus(fields map[string]UsageObservation, now time.Time) *AnthropicAccountStatus {
	out := &AnthropicAccountStatus{}
	observed := func(key string) UsageObservation {
		v := fields[key]
		if v.SampledAt == nil || v.SampledAt.After(now.Add(time.Minute)) || (v.Source != "upstream_api" && v.Source != "upstream_headers") {
			return UsageObservation{}
		}
		return v
	}
	text := func(key string) string { v, _ := observed(key).Value.(string); return strings.ToLower(v) }
	// A later authenticated response supersedes an older failed query.
	var success time.Time
	for _, key := range []string{"usage.query_status", "limit.status"} {
		v := text(key)
		if v == "ok" || v == "allowed" || v == "allowed_warning" || v == "rejected" {
			if at := observed(key).SampledAt; at != nil && at.After(success) {
				success = *at
			}
		}
	}
	failure := observed("usage.query_reason")
	if failure.SampledAt != nil && !failure.SampledAt.Before(success) {
		switch reason := text("usage.query_reason"); reason {
		case "authentication", "revoked", "disabled":
			out.ErrorCode = reason
		}
	}
	blocked := func(name string) (bool, *time.Time) {
		status, severity := observed(name+".status"), observed(name+".severity")
		chosen := status
		if severity.SampledAt != nil && (chosen.SampledAt == nil || !severity.SampledAt.Before(*chosen.SampledAt)) {
			chosen = severity
		}
		value, _ := chosen.Value.(string)
		if value != "rejected" && value != "blocked" && value != "exhausted" {
			return false, nil
		}
		active := observed(name + ".is_active")
		if active.Value == false && active.SampledAt != nil && chosen.SampledAt != nil && !active.SampledAt.Before(*chosen.SampledAt) {
			return false, nil
		}
		return chosen.SampledAt != nil, chosen.SampledAt
	}
	setLimit := func(name string, at *time.Time, model bool) {
		var reset *time.Time
		if raw, _ := observed(name + ".resets_at").Value.(string); raw != "" {
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil || !t.After(now) {
				return
			}
			duration := 7 * 24 * time.Hour
			if name == "five_hour" {
				duration = 5 * time.Hour
			}
			if name != "limit" && at != nil && at.Before(t.Add(-duration)) {
				return
			}
			reset = &t
		} else if at == nil || now.Sub(*at) > 5*time.Minute {
			return
		}
		if util := observed(name + ".utilization"); at != nil && util.Value != nil && util.SampledAt != nil && util.SampledAt.After(*at) && parseExtraFloat64(util.Value) < 100 {
			return
		}
		if model {
			if out.ModelRateLimits == nil {
				out.ModelRateLimits = make(map[string]*time.Time)
			}
			out.ModelRateLimits[anthropicFableRateLimitKey] = reset
		} else {
			out.RateLimited = true
			if reset != nil && (out.RateLimitResetAt == nil || reset.After(*out.RateLimitResetAt)) {
				out.RateLimitResetAt = reset
			}
		}
	}
	if rejected, at := blocked("limit"); rejected {
		claim := text("limit.representative_claim")
		name := map[string]string{"5h": "five_hour", "five_hour": "five_hour", "7d": "seven_day", "seven_day": "seven_day", "7d_oi": "seven_day_fable", "seven_day_fable": "seven_day_fable"}[claim]
		if name == "" || text(name+".resets_at") == "" {
			name = "limit"
		}
		setLimit(name, at, claim == "7d_oi" || claim == "seven_day_fable")
	}
	for _, name := range []string{"five_hour", "seven_day", "seven_day_fable"} {
		if rejected, at := blocked(name); rejected {
			if name != "seven_day_fable" && (text("limit.status") == "allowed" || text("limit.status") == "allowed_warning") {
				if overall := observed("limit.status").SampledAt; overall != nil && !overall.Before(*at) {
					continue
				}
			}
			setLimit(name, at, name == "seven_day_fable")
		}
	}
	if out.ErrorCode == "" && !out.RateLimited && len(out.ModelRateLimits) == 0 {
		return nil
	}
	return out
}

func (s *AccountUsageService) recordAnthropicQueryFailure(ctx context.Context, a *Account, err error, at time.Time) {
	fields := make(map[string]UsageObservation)
	putUsageObservation(fields, "usage.query_status", anthropicQueryErrorCode(err), "upstream_api", &at)
	reason := ""
	var upstream *ClaudeUsageHTTPError
	if errors.As(err, &upstream) {
		reason = upstream.Reason
		if reason == "" && upstream.StatusCode == http.StatusUnauthorized {
			reason = "authentication"
		}
	}
	if reason != "" {
		putUsageObservation(fields, "usage.query_reason", reason, "upstream_api", &at)
	}
	updates := persistableAnthropicObservations(fields, anthropicCredentialIdentity(a))
	if s.accountRepo != nil {
		_ = s.accountRepo.UpdateExtra(ctx, a.ID, updates)
	}
	mergeAccountExtra(a, updates)
}

func (r *ClaudeUsageResponse) UnmarshalJSON(data []byte) error {
	type plain ClaudeUsageResponse
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = ClaudeUsageResponse(decoded)
	if err := json.Unmarshal(data, &r.RawFields); err != nil {
		return err
	}
	for key := range r.RawFields {
		switch key {
		case "five_hour", "seven_day", "seven_day_sonnet", "seven_day_overage_included":
		case "limits", "error":
			// Typed limits carry only the required input fields; retain presence,
			// not a second copy of arbitrary upstream descriptions/error bodies.
			r.RawFields[key] = json.RawMessage("null")
		default:
			delete(r.RawFields, key)
		}
	}
	return nil
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

func putUsageObservation(fields map[string]UsageObservation, key string, value any, source string, at *time.Time) {
	fields[key] = UsageObservation{Value: value, Source: source, SampledAt: at}
}

// Only fields consumed by quota windows, channel monitoring or status display
// may enter persistence. Retired panel metadata is not replayed from old JSONB.
func isAnthropicObservationField(key string) bool {
	switch key {
	case "usage.query_status", "usage.query_reason",
		"limit.status", "limit.resets_at", "limit.representative_claim":
		return true
	}
	name, field, ok := strings.Cut(key, ".")
	if !ok {
		return false
	}
	switch name {
	case "five_hour", "seven_day", "seven_day_fable":
		switch field {
		case "utilization", "resets_at", "status", "severity", "is_active", "cleared_at":
			return true
		}
	case "seven_day_sonnet":
		return field == "utilization" || field == "resets_at"
	}
	return false
}

// Individual JSONB keys let sparse header samples and active queries update
// unrelated fields without replacing each other's observation maps.
func persistableAnthropicObservations(fields map[string]UsageObservation, identity string) map[string]any {
	updates := make(map[string]any, len(fields))
	for key, observation := range fields {
		if !isAnthropicObservationField(key) || (observation.Source != "upstream_headers" && observation.Source != "upstream_api") {
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
	return storedAnthropicObservationsForIdentity(a, anthropicCredentialIdentity(a))
}

func anthropicLegacyUsageAllowed(a *Account, fields map[string]UsageObservation, key string) bool {
	if _, exists := a.Extra[anthropicObservationPrefix+key]; exists {
		return false
	}
	for storedKey := range a.Extra {
		field := strings.TrimPrefix(storedKey, anthropicObservationPrefix)
		if field != storedKey && isAnthropicObservationField(field) {
			if _, current := fields[field]; !current {
				return false
			}
		}
	}
	return true
}

func anthropicThresholdUsageFields(a *Account) map[string]UsageObservation {
	identity := anthropicCredentialIdentity(a)
	if identity == "" {
		identity, _ = a.Extra[schedulingQuotaAnthropicIdentityKey].(string)
	}
	fields := storedAnthropicObservationsForIdentity(a, identity)
	for field, legacy := range map[string]string{
		"five_hour.utilization":       "session_window_utilization",
		"seven_day.utilization":       "passive_usage_7d_utilization",
		"seven_day.resets_at":         "passive_usage_7d_reset",
		"seven_day_fable.utilization": "passive_usage_7d_oi_utilization",
		"seven_day_fable.resets_at":   "passive_usage_7d_oi_reset",
	} {
		if _, current := fields[field]; current || !anthropicLegacyUsageAllowed(a, fields, field) {
			continue
		}
		value := a.Extra[legacy]
		if strings.HasSuffix(field, ".utilization") {
			percent, valid := utilizationAsPercentValue(value)
			if !valid {
				continue
			}
			value = percent
		}
		putUsageObservation(fields, field, value, "stored_snapshot", nil)
	}
	if _, current := fields["five_hour.resets_at"]; !current && anthropicLegacyUsageAllowed(a, fields, "five_hour.resets_at") && a.SessionWindowEnd != nil {
		putUsageObservation(fields, "five_hour.resets_at", a.SessionWindowEnd.UTC().Format(time.RFC3339), "stored_snapshot", nil)
	}
	return fields
}

func storedAnthropicObservationsForIdentity(a *Account, identity string) map[string]UsageObservation {
	fields := make(map[string]UsageObservation)
	for key, raw := range a.Extra {
		if !strings.HasPrefix(key, anthropicObservationPrefix) || !isAnthropicObservationField(strings.TrimPrefix(key, anthropicObservationPrefix)) {
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
	applyAnthropicUsageResets(fields)
	return fields
}

func anthropicWindowClearedAt(fields map[string]UsageObservation, name string) time.Time {
	marker := fields[name+".cleared_at"]
	if marker.Source != "upstream_api" || marker.SampledAt == nil || marker.SampledAt.After(time.Now().Add(time.Minute)) {
		return time.Time{}
	}
	return *marker.SampledAt
}

// A confirmed redemption supersedes only older observations of cleared meters.
// Retain the independent marker so a delayed pre-reset write cannot revive them.
func applyAnthropicUsageResets(fields map[string]UsageObservation) {
	for _, name := range []string{"five_hour", "seven_day", "seven_day_fable"} {
		at := anthropicWindowClearedAt(fields, name)
		if at.IsZero() {
			continue
		}
		for _, field := range []string{"utilization", "resets_at", "status", "severity", "is_active"} {
			key := name + "." + field
			if old := fields[key]; old.SampledAt != nil && old.SampledAt.After(at) {
				continue
			}
			var value any
			if field == "utilization" {
				value = float64(0)
			} else if field == "status" {
				value = "allowed"
			}
			putUsageObservation(fields, key, value, "upstream_api", &at)
		}
		claim, _ := fields["limit.representative_claim"].Value.(string)
		if claim == name || (name == "seven_day_fable" && (claim == "7d_oi" || claim == "seven_day_overage_included")) ||
			(name == "five_hour" && claim == "5h") || (name == "seven_day" && claim == "7d") {
			if old := fields["limit.status"]; old.SampledAt == nil || !old.SampledAt.After(at) {
				putUsageObservation(fields, "limit.status", "allowed", "upstream_api", &at)
			}
		}
	}
}

func latestAnthropicUsageReset(account *Account) time.Time {
	fields := storedAnthropicObservations(account)
	var latest time.Time
	for _, name := range []string{"five_hour", "seven_day", "seven_day_fable"} {
		if at := anthropicWindowClearedAt(fields, name); at.After(latest) {
			latest = at
		}
	}
	return latest
}

func anthropicHeaderObservations(headers http.Header, at time.Time) map[string]UsageObservation {
	fields := make(map[string]UsageObservation)
	for wire, name := range map[string]string{"5h": "five_hour", "7d": "seven_day", "7d_oi": "seven_day_fable", "": "limit"} {
		prefix := "anthropic-ratelimit-unified-"
		if wire != "" {
			prefix += wire + "-"
		}
		keys := []string{"status", "utilization", "reset"}
		if name == "limit" {
			keys = []string{"status", "representative-claim", "reset"}
		}
		for _, key := range keys {
			raw := strings.TrimSpace(headers.Get(prefix + key))
			if raw == "" || len(raw) > 128 {
				continue
			}
			var value any = raw
			field := strings.ReplaceAll(key, "-", "_")
			switch key {
			case "utilization":
				n, err := strconv.ParseFloat(raw, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
					continue
				}
				if math.IsInf(n*100, 0) {
					continue
				}
				value = n * 100
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
	if len(row.Scope) > 0 && json.Unmarshal(row.Scope, &scope) != nil {
		return ""
	}
	// The usage contract allows absent model/surface dimensions as explicit null.
	// Unknown dimensions and non-null surface scopes must remain scoped.
	for _, dimension := range []string{"model", "surface"} {
		if strings.TrimSpace(string(scope[dimension])) == "null" {
			delete(scope, dimension)
		}
	}
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
		Name string `json:"display_name"`
	}
	if json.Unmarshal(scope["model"], &model) != nil {
		return ""
	}
	for _, family := range []string{"fable", "sonnet"} {
		if strings.EqualFold(model.Name, family) {
			return "seven_day_" + family
		}
	}
	return ""
}

func collectAnthropicAPIObservations(resp *ClaudeUsageResponse, at *time.Time) map[string]UsageObservation {
	fields := make(map[string]UsageObservation)
	putUsageObservation(fields, "usage.query_status", "ok", "upstream_api", at)
	putUsageObservation(fields, "usage.query_reason", "", "upstream_api", at)
	for wire, name := range map[string]string{"five_hour": "five_hour", "seven_day": "seven_day", "seven_day_sonnet": "seven_day_sonnet", "seven_day_overage_included": "seven_day_fable"} {
		raw, present := resp.RawFields[wire]
		if !present {
			continue
		}
		keys := []string{"utilization", "resets_at"}
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
	// Only canonical windows are retained. Raw model/surface descriptors have
	// no consumer after the detail panel was removed.
	if len(resp.Limits) <= 32 {
		counts := make(map[string]int)
		for _, row := range resp.Limits {
			counts[canonicalAnthropicLimit(row)]++
		}
		for _, row := range resp.Limits {
			name := canonicalAnthropicLimit(row)
			if name == "" || counts[name] != 1 {
				continue
			}
			putUsageObservation(fields, name+".utilization", nil, "upstream_api", at)
			putUsageObservation(fields, name+".resets_at", nil, "upstream_api", at)
			if validUsageNumber(row.Percent) {
				putUsageObservation(fields, name+".utilization", *row.Percent, "upstream_api", at)
			}
			if row.ResetsAt != nil {
				if reset, err := parseTime(*row.ResetsAt); err == nil {
					putUsageObservation(fields, name+".resets_at", reset.UTC().Format(time.RFC3339), "upstream_api", at)
				}
			}
			if name == "seven_day_sonnet" {
				continue
			}
			// Authoritative current window metadata clears older severity too.
			putUsageObservation(fields, name+".status", nil, "upstream_api", at)
			putUsageObservation(fields, name+".severity", nil, "upstream_api", at)
			putUsageObservation(fields, name+".is_active", nil, "upstream_api", at)
			if len(row.Severity) <= 128 && row.Severity != "" {
				putUsageObservation(fields, name+".severity", row.Severity, "upstream_api", at)
			}
			if row.IsActive != nil {
				putUsageObservation(fields, name+".is_active", *row.IsActive, "upstream_api", at)
			}
		}
	}

	return fields
}

func enrichAnthropicUsage(a *Account, info *UsageInfo) {
	if !supportsAnthropicPassiveUsage(a) {
		return
	}
	// Header snapshots are still read by scheduling and the vendor portal.
	// Feed their fallback through the same identity check as all observations.
	if info.SevenDayFable == nil {
		info.SevenDayFable = buildPassiveUsageWindow(a.Extra, "passive_usage_7d_oi_utilization", "passive_usage_7d_oi_reset")
	}
	fields := storedAnthropicObservations(a)
	if info.Anthropic != nil {
		for key, fresh := range info.Anthropic.Observations {
			old, exists := fields[key]
			if !exists || old.SampledAt == nil || (fresh.SampledAt != nil && !fresh.SampledAt.Before(*old.SampledAt)) {
				fields[key] = fresh
			}
		}
	}
	info.Anthropic = &AnthropicUsageDetails{Observations: fields, Identity: anthropicCredentialIdentity(a)}

	for _, window := range []struct {
		name     string
		progress **UsageProgress
	}{
		{"five_hour", &info.FiveHour}, {"seven_day", &info.SevenDay}, {"seven_day_sonnet", &info.SevenDaySonnet}, {"seven_day_fable", &info.SevenDayFable},
	} {
		utilKey, resetKey := window.name+".utilization", window.name+".resets_at"
		progress := *window.progress
		if progress != nil {
			allowUtil := anthropicLegacyUsageAllowed(a, fields, utilKey)
			allowReset := anthropicLegacyUsageAllowed(a, fields, resetKey)
			if !allowUtil && !allowReset {
				*window.progress = nil
				continue
			}
			if !allowUtil {
				progress.Utilization = 0
			}
			if !allowReset {
				progress.ResetsAt, progress.RemainingSeconds = nil, 0
			}
			// Old installations did not track provenance per field. Do not relabel
			// those values with the global timestamp of an unrelated header sample.
			if _, exists := fields[utilKey]; !exists && allowUtil {
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
			if _, exists := fields[resetKey]; !exists && allowReset && progress.ResetsAt != nil {
				putUsageObservation(fields, resetKey, progress.ResetsAt.UTC().Format(time.RFC3339), "stored_snapshot", nil)
			}
		}
	}
	applyAnthropicUsageWindows(info)

	info.AnthropicStatus = anthropicAccountStatus(fields, time.Now())
}

// Shared by admin queries, passive snapshots and vendor quota refresh. The
// canonical observations are the only source of window values.
func applyAnthropicUsageWindows(info *UsageInfo) {
	fields := info.Anthropic.Observations
	applyAnthropicUsageResets(fields)
	for _, window := range []struct {
		name     string
		progress **UsageProgress
	}{
		{"five_hour", &info.FiveHour}, {"seven_day", &info.SevenDay}, {"seven_day_sonnet", &info.SevenDaySonnet}, {"seven_day_fable", &info.SevenDayFable},
	} {
		utilKey, resetKey := window.name+".utilization", window.name+".resets_at"
		progress := *window.progress
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

func annotateAnthropicWindowStats(info *UsageInfo, name string, stats *WindowStats, sampledAt time.Time) {
	if info.Anthropic == nil || stats == nil {
		return
	}
	fields := info.Anthropic.Observations
	putUsageObservation(fields, name+".window_stats", stats, "local_statistics", &sampledAt)
}

// Observation keys are written only by upstream sampling, never by account forms.
// Historical values are preserved on existing accounts; data removal is separate.
func stripAnthropicObservationExtra(extra map[string]any) {
	for key := range extra {
		if strings.HasPrefix(key, anthropicObservationPrefix) {
			delete(extra, key)
		}
	}
}
