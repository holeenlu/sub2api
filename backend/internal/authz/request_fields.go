package authz

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Request schemas are the actual handler binding types. Permissions live on
// their JSON fields; an unclassified/new field defaults to super-admin only.
var requestSchemas = map[string]reflect.Type{}

func DefineRequestSchema(name string, sample any) {
	typ := reflect.TypeOf(sample)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if _, exists := requestSchemas[name]; exists {
		panic("duplicate request schema: " + name)
	}
	requestSchemas[name] = typ
}
func RequestSchemas() map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for key, typ := range requestSchemas {
		out[key] = typ
	}
	return out
}
func ValidateRequestFields(name string, body map[string]any, actor Subject) (bool, error) {
	typ, ok := requestSchemas[name]
	if !ok {
		return false, fmt.Errorf("Request field policy is unavailable: %s", name)
	}
	return validateRequestObject(typ, body, actor, "")
}
func validateRequestObject(typ reflect.Type, value any, actor Subject, prefix string) (bool, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	sensitive := false
	if typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
		items, ok := value.([]any)
		if !ok {
			return false, nil
		}
		for _, item := range items {
			found, err := validateRequestObject(typ.Elem(), item, actor, prefix+"[]")
			if err != nil {
				return false, err
			}
			sensitive = sensitive || found
		}
		return sensitive, nil
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return false, nil
	}
	schema := map[string]reflect.StructField{}
	if typ.Kind() == reflect.Struct {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				schema[FieldName(name)] = f
			}
		}
	}
	for key, item := range fields {
		field, known := schema[FieldName(key)]
		policy := field.Tag.Get("authz")
		if !known || policy == "" {
			if actor.Role != SuperAdmin {
				return false, fmt.Errorf("Field %s%s requires super administrator", prefix, key)
			}
			continue
		}
		parts := strings.Split(policy, ",")
		nested, skipEmpty := false, false
		for _, part := range parts {
			nested = nested || part == "object"
			skipEmpty = skipEmpty || part == "nonempty"
		}
		if skipEmpty && (item == nil || (reflect.ValueOf(item).Kind() == reflect.Slice || reflect.ValueOf(item).Kind() == reflect.Map) && reflect.ValueOf(item).Len() == 0) {
			continue
		}
		for _, permission := range parts {
			switch permission {
			case "base", "object", "nonempty":
				continue
			case "account_create_extra":
				found, err := validateNewAccountExtra(item, actor)
				if err != nil {
					return false, err
				}
				sensitive = sensitive || found
			case "super_admin":
				if actor.Role != SuperAdmin {
					return false, fmt.Errorf("Field %s%s requires super administrator", prefix, key)
				}
			default:
				if !actor.Can(permission) {
					return false, fmt.Errorf("Field %s%s requires %s", prefix, key, permission)
				}
				sensitive = sensitive || IsSensitive([]string{permission})
			}
		}
		if nested {
			found, err := validateRequestObject(field.Type, item, actor, prefix+key+".")
			if err != nil {
				return false, err
			}
			sensitive = sensitive || found
		}
	}
	return sensitive, nil
}

// Extra is an untyped provider map, so reflection cannot classify its fields.
// This is the sole allowlist for a NEW account. Updates retain the native
// replacement semantics and are root-only: omitted hidden fields would be erased.
var newAccountExtraPermissions = map[string]string{
	"org_uuid":                        "",
	"account_uuid":                    "",
	"email_address":                   "",
	"email":                           "",
	"name":                            "",
	"privacy_mode":                    "",
	"subscription_tier":               "",
	"entitlement_status":              "",
	"drive_storage_limit":             "",
	"drive_storage_usage":             "",
	"drive_tier_updated_at":           "",
	"mixed_scheduling":                "",
	"anthropic_apikey_auth_scheme":    "",
	"anthropic_passthrough":           "",
	"base_rpm":                        "",
	"cache_ttl_override_enabled":      "",
	"cache_ttl_override_target":       "",
	"codex_cli_only":                  "",
	"codex_cli_only_allow_app_server": "",
	"codex_fingerprint_mode":          "",
	"custom_base_url":                 "",
	"custom_base_url_enabled":         "",
	"enable_tls_fingerprint":          "",
	"images_url_to_b64_json":          "",
	"max_sessions":                    "",
	"openai_apikey_responses_websockets_v2_enabled": "",
	"openai_apikey_responses_websockets_v2_mode":    "",
	"openai_compact_mode":                           "",
	"openai_oauth_responses_websockets_v2_enabled":  "",
	"openai_oauth_responses_websockets_v2_mode":     "",
	"openai_passthrough":                            "",
	"openai_responses_flatten_namespaces":           "",
	"openai_responses_mode":                         "",
	"rpm_sticky_buffer":                             "",
	"rpm_strategy":                                  "",
	"session_id_masking_enabled":                    "",
	"session_idle_timeout_minutes":                  "",
	"tls_fingerprint_profile_id":                    "",
	"user_msg_queue_mode":                           "",
	"web_search_emulation":                          "",
	"upstream_request_id_header":                    "",
	"allow_overages":                                "billing.rates.update",
	"openai_long_context_billing_enabled":           "billing.rates.update",
	"quota_daily_limit":                             "billing.rates.update",
	"quota_daily_reset_hour":                        "billing.rates.update",
	"quota_daily_reset_mode":                        "billing.rates.update",
	"quota_limit":                                   "billing.rates.update",
	"quota_reset_timezone":                          "billing.rates.update",
	"quota_weekly_limit":                            "billing.rates.update",
	"quota_weekly_reset_day":                        "billing.rates.update",
	"quota_weekly_reset_hour":                       "billing.rates.update",
	"quota_weekly_reset_mode":                       "billing.rates.update",
	"window_cost_limit":                             "billing.rates.update",
	"window_cost_sticky_reserve":                    "billing.rates.update",
	"quota_notify_daily_enabled":                    "billing.rates.update",
	"quota_notify_daily_threshold":                  "billing.rates.update",
	"quota_notify_daily_threshold_type":             "billing.rates.update",
	"quota_notify_weekly_enabled":                   "billing.rates.update",
	"quota_notify_weekly_threshold":                 "billing.rates.update",
	"quota_notify_weekly_threshold_type":            "billing.rates.update",
	"quota_notify_total_enabled":                    "billing.rates.update",
	"quota_notify_total_threshold":                  "billing.rates.update",
	"quota_notify_total_threshold_type":             "billing.rates.update",
}

func validateNewAccountExtra(value any, actor Subject) (bool, error) {
	fields, ok := value.(map[string]any)
	if !ok {
		return false, nil
	} // The native binder validates non-object values.
	sensitive := false
	for key := range fields {
		permission, known := newAccountExtraPermissions[key]
		if !known {
			if actor.Role != SuperAdmin {
				return false, fmt.Errorf("Unclassified account extra field %s requires super administrator", key)
			}
			continue
		}
		if permission != "" {
			if !actor.Can(permission) {
				return false, fmt.Errorf("Account extra field %s requires %s", key, permission)
			}
			sensitive = sensitive || IsSensitive([]string{permission})
		}
	}
	return sensitive, nil
}

// EditableRequestFields projects the binding declarations into the existing
// identity response. Forms never need a second financial-field permission list.
// This is presentation metadata; every submitted field is validated again.
func EditableRequestFields(actor Subject) map[string][]string {
	if actor.Role != Admin {
		return nil
	}
	result := map[string][]string{}
	for name, typ := range requestSchemas {
		fields := []string{}
		for i := 0; i < typ.NumField(); i++ {
			key := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			if key == "" || key == "-" {
				continue
			}
			if _, err := ValidateRequestFields(name, map[string]any{key: nil}, actor); err == nil {
				fields = append(fields, key)
			}
		}
		sort.Strings(fields)
		result[name] = fields
	}
	return result
}
