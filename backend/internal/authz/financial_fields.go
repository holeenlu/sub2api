package authz

// Financial meaning is explicit. The same JSON name may describe a customer
// charge/quota or an upstream account's cost, so object scope is part of the rule.
type FinancialClass uint8

const (
	FinancialPublic FinancialClass = iota + 1
	FinancialCost
	FinancialAccountCost
)

var financialFields = map[string]FinancialClass{
	"account_cost":                    FinancialCost,
	"account_rate_multiplier":         FinancialCost,
	"account_stats_cost":              FinancialCost,
	"total_account_cost":              FinancialCost,
	"today_account_cost":              FinancialCost,
	"account_stats_pricing_rules":     FinancialCost,
	"apply_pricing_to_account_stats":  FinancialCost,
	"profit_control_enabled":          FinancialCost,
	"profit_min_margin":               FinancialCost,
	"profit_safety_buffer":            FinancialCost,
	"upstream_billing_probe":          FinancialCost,
	"upstream_billing_rate":           FinancialCost,
	"ollama_cloud_usage":              FinancialCost,
	"opencode_go_usage":               FinancialCost,
	"usage_snapshot":                  FinancialCost,
	"upstream_usage_snapshot":         FinancialCost,
	"account_usage_snapshot":          FinancialCost,
	"upstream_total_cost":             FinancialCost,
	"cost_rate_multiplier":            FinancialCost,
	"cost_multiplier":                 FinancialCost,
	"profit":                          FinancialCost,
	"rate_multiplier":                 FinancialAccountCost,
	"quota_used":                      FinancialAccountCost,
	"quota_daily_used":                FinancialAccountCost,
	"quota_weekly_used":               FinancialAccountCost,
	"quota_daily_limit":               FinancialAccountCost,
	"quota_weekly_limit":              FinancialAccountCost,
	"quota_limit":                     FinancialAccountCost,
	"quota_notify_daily_threshold":    FinancialAccountCost,
	"quota_notify_weekly_threshold":   FinancialAccountCost,
	"quota_notify_total_threshold":    FinancialAccountCost,
	"quota_daily_reset_at":            FinancialAccountCost,
	"quota_weekly_reset_at":           FinancialAccountCost,
	"quota_daily_reset_hour":          FinancialAccountCost,
	"quota_weekly_reset_hour":         FinancialAccountCost,
	"quota_weekly_reset_day":          FinancialAccountCost,
	"quota_daily_reset_mode":          FinancialAccountCost,
	"quota_weekly_reset_mode":         FinancialAccountCost,
	"quota_reset_timezone":            FinancialAccountCost,
	"quota_notify_daily_enabled":      FinancialAccountCost,
	"quota_notify_weekly_enabled":     FinancialAccountCost,
	"quota_notify_total_enabled":      FinancialAccountCost,
	"window_cost_limit":               FinancialAccountCost,
	"window_cost_sticky_reserve":      FinancialAccountCost,
	"cost":                            FinancialAccountCost,
	"actual_cost":                     FinancialAccountCost,
	"total_cost":                      FinancialAccountCost,
	"standard_cost":                   FinancialAccountCost,
	"total_actual_cost":               FinancialAccountCost,
	"total_standard_cost":             FinancialAccountCost,
	"avg_daily_cost":                  FinancialAccountCost,
	"highest_cost_day":                FinancialAccountCost,
	"account_quota_notify_emails":     FinancialPublic,
	"account_quota_notify_enabled":    FinancialPublic,
	"antigravity_quota":               FinancialPublic,
	"antigravity_quota_details":       FinancialPublic,
	"avg_daily_user_cost":             FinancialPublic,
	"batch_image_discount_multiplier": FinancialPublic,
	"batch_image_hold_multiplier":     FinancialPublic,
	"cache_creation_cost":             FinancialPublic,
	"cache_read_cost":                 FinancialPublic,
	"cache_read_multiplier":           FinancialPublic,
	"cache_write_multiplier":          FinancialPublic,
	"channel_monitor_show_quota":      FinancialPublic,
	"default_platform_quotas":         FinancialPublic,
	"fast_multiplier":                 FinancialPublic,
	"flex_multiplier":                 FinancialPublic,
	"grok_last_quota_probe_at":        FinancialPublic,
	"grok_quota_snapshot_state":       FinancialPublic,
	"grok_request_quota":              FinancialPublic,
	"grok_token_quota":                FinancialPublic,
	"image_input_cost":                FinancialPublic,
	"image_output_cost":               FinancialPublic,
	"image_rate_multiplier":           FinancialPublic,
	"input_cost":                      FinancialPublic,
	"input_multiplier":                FinancialPublic,
	"long_context_pricing_enabled":    FinancialPublic,
	"model_pricing":                   FinancialPublic,
	"multiplier":                      FinancialPublic,
	"openai_advanced_scheduler_effective_weight_quota_headroom": FinancialPublic,
	"openai_advanced_scheduler_effective_weight_upstream_cost":  FinancialPublic,
	"openai_advanced_scheduler_weight_quota_headroom":           FinancialPublic,
	"openai_advanced_scheduler_weight_upstream_cost":            FinancialPublic,
	"openai_oauth_scheduling_rate_multiplier":                   FinancialPublic,
	"output_cost":                         FinancialPublic,
	"output_multiplier":                   FinancialPublic,
	"payment_balance_recharge_multiplier": FinancialPublic,
	"peak_rate_multiplier":                FinancialPublic,
	"pricing":                             FinancialPublic,
	"quota":                               FinancialPublic,
	"quota_dimension":                     FinancialPublic,
	"reasoning_effort_multipliers":        FinancialPublic,
	"registration_email_domain_quota_enabled": FinancialPublic,
	"time_pricing":          FinancialPublic,
	"today_actual_cost":     FinancialPublic,
	"today_cost":            FinancialPublic,
	"total_user_cost":       FinancialPublic,
	"user_cost":             FinancialPublic,
	"video_rate_multiplier": FinancialPublic,
	"yesterday_cost":        FinancialPublic,
}

var normalizedFinancialFields = func() map[string]FinancialClass {
	result := map[string]FinancialClass{}
	for key, value := range financialFields {
		result[FieldName(key)] = value
	}
	return result
}()

func FinancialFieldClass(key string) (FinancialClass, bool) {
	kind, ok := normalizedFinancialFields[FieldName(key)]
	return kind, ok
}
func IsCostField(key string) bool { kind, _ := FinancialFieldClass(key); return kind == FinancialCost }
func IsAccountCostField(key string, account bool) bool {
	kind, _ := FinancialFieldClass(key)
	return kind == FinancialCost || (account && kind == FinancialAccountCost)
}
