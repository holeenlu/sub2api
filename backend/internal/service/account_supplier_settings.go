package service

// These are routing options stored beside OAuth credentials. All other keys
// remain bound to the verified authorization, including tokens and identity.
func SupplierOperationalCredentialKeys() []string {
	return []string{
		"model_mapping", "compact_model_mapping", "model_whitelist",
		"custom_error_codes", "custom_error_codes_enabled", "intercept_warmup_requests",
		"temp_unschedulable_enabled", "temp_unschedulable_rules",
		"account_scheduling_threshold", "anthropic_fable_scheduling_threshold",
	}
}
