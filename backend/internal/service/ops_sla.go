package service

import "strings"

// Keep new writes and historical SQL reads on the same narrow SLA rules.
// Adapted from ranxi2001/sub2api #439; broader capability rejection and
// cross-request deduplication changes are deliberately not part of this rule.
const opsPayloadTooLargeMessage = "request payload is too large"

func normalizeOpsSLA(entry *OpsInsertErrorLogInput) {
	if entry == nil || entry.StatusCode < 400 {
		return
	}
	if entry.ErrorOwner == "platform" && entry.ErrorPhase == "routing" &&
		entry.ErrorType != "model_not_found" && (entry.StatusCode == 429 || entry.StatusCode == 503) {
		entry.IsBusinessLimited = false
		return
	}
	if entry.ErrorPhase == "account_auth" || entry.ErrorType == "model_not_found" {
		return
	}
	status := entry.StatusCode
	// A stream may fail after an HTTP 200 handshake. Keep that wire status
	// as evidence while classifying the separately recorded logical failure.
	if entry.UpstreamStatusCode != nil && *entry.UpstreamStatusCode >= 400 {
		status = *entry.UpstreamStatusCode
	}
	if status != 400 && status != 413 && status != 422 {
		return
	}
	message := entry.ErrorMessage
	if entry.UpstreamErrorMessage != nil {
		message += " " + *entry.UpstreamErrorMessage
	}
	if !strings.Contains(strings.ToLower(message), opsPayloadTooLargeMessage) {
		return
	}
	entry.IsBusinessLimited = true
	entry.ErrorOwner = "client"
	entry.ErrorPhase = "request"
	entry.ErrorType = "context_limit"
}

func opsSLAColumnPrefix(alias string) string {
	if alias == "" {
		return ""
	}
	return alias + "."
}

func opsSLACapacitySQL(alias string) string {
	p := opsSLAColumnPrefix(alias)
	return `(COALESCE(` + p + `error_owner, '') = 'platform'
 AND COALESCE(` + p + `error_phase, '') = 'routing'
 AND COALESCE(` + p + `error_type, '') <> 'model_not_found'
 AND COALESCE(` + p + `status_code, 0) IN (429, 503))`
}

func opsSLAPayloadLimitSQL(alias string) string {
	p := opsSLAColumnPrefix(alias)
	return `(COALESCE(` + p + `status_code, 0) >= 400
 AND COALESCE(` + p + `error_phase, '') <> 'account_auth'
 AND COALESCE(` + p + `error_type, '') <> 'model_not_found'
 AND NOT ` + opsSLACapacitySQL(alias) + `
 AND (CASE WHEN ` + p + `upstream_status_code >= 400 THEN ` + p + `upstream_status_code ELSE ` + p + `status_code END) IN (400, 413, 422)
 AND strpos(lower(CONCAT_WS(' ', ` + p + `error_message, ` + p + `upstream_error_message)), '` + opsPayloadTooLargeMessage + `') > 0)`
}

// OpsSLABusinessLimitedSQL corrects historical classifications without rewriting
// raw rows. alias is an internal SQL identifier, never request input.
func OpsSLABusinessLimitedSQL(alias string) string {
	return `(CASE WHEN ` + opsSLACapacitySQL(alias) + ` THEN FALSE
 WHEN ` + opsSLAPayloadLimitSQL(alias) + ` THEN TRUE
 ELSE COALESCE(` + opsSLAColumnPrefix(alias) + `is_business_limited, FALSE) END)`
}

// OpsSLAEffectiveColumnSQL aligns displayed/filtered responsibility with the SLA
// predicate. Original status, source, messages and attempt evidence stay intact.
// Both identifiers are supplied by internal query builders only.
func OpsSLAEffectiveColumnSQL(alias, column string) string {
	replacement := ""
	switch column {
	case "error_owner":
		replacement = "client"
	case "error_phase":
		replacement = "request"
	case "error_type":
		replacement = "context_limit"
	default:
		panic("unsupported SLA responsibility column")
	}
	return `(CASE WHEN ` + opsSLAPayloadLimitSQL(alias) + ` THEN '` + replacement + `' ELSE COALESCE(` + opsSLAColumnPrefix(alias) + column + `, '') END)`
}
