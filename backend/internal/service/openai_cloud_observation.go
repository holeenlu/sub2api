package service

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const cloudAdaptationKind = "cloud_adaptation"
const cloudFailureKind = "cloud_failure"
const cloudAdaptationMaxDecisions = 64
const cloudAdaptationMaxBytes = 16 << 10

type cloudAdaptationDetail struct {
	Decisions []cloudAdaptationDecision `json:"decisions"`
	Omitted   int                       `json:"omitted,omitempty"`
}

var cloudDiagnosticPath = regexp.MustCompile(`^[a-zA-Z0-9_$.*\[\]]{1,128}$`)

func cloudSafeAdaptationDetail(raw string) *cloudAdaptationDetail {
	if len(raw) > cloudAdaptationMaxBytes {
		return nil
	}
	var detail cloudAdaptationDetail
	if json.Unmarshal([]byte(raw), &detail) != nil {
		return nil
	}
	safe := &cloudAdaptationDetail{Omitted: max(0, detail.Omitted)}
	for _, d := range detail.Decisions {
		known := false
		for _, rule := range cloudCapabilities {
			if d.MatrixID == rule.ID {
				known = true
				break
			}
		}
		if d.Omitted < 0 || !known || !cloudDiagnosticPath.MatchString(d.Path) || (d.Action != "pass" && d.Action != "map" && d.Action != "drop" && d.Action != "reject") {
			safe.Omitted++
			continue
		}
		if len(safe.Decisions) >= cloudAdaptationMaxDecisions {
			safe.Omitted++
			continue
		}
		safe.Decisions = append(safe.Decisions, d)
	}
	return safe
}
func recordCloudAdaptation(c *gin.Context, decisions []cloudAdaptationDecision, err error) {
	// Local owner/continuation failures may have no adaptation decisions.
	// Preserve their closed reason/parameter vocabulary before HTTP/WS error
	// presentation reduces the envelope to a generic code and message.
	if cloudFailureReason(err) != "" {
		if _, code, message, known := cloudPublicError(err); known {
			raw, _ := json.Marshal(map[string]any{"error": cloudPublicErrorFields(err, code, message)})
			recordCloudFailure(c, raw)
		}
	}
	var rejected *cloudRejection
	if errors.As(err, &rejected) {
		decisions = append(append([]cloudAdaptationDecision(nil), decisions...), rejected.Decisions...)
	}
	if len(decisions) == 0 {
		return
	}
	detail := cloudAdaptationDetail{}
	seen := map[cloudAdaptationDecision]bool{}
	for _, d := range decisions {
		if seen[d] {
			continue
		}
		seen[d] = true
		if len(detail.Decisions) >= cloudAdaptationMaxDecisions {
			detail.Omitted++
			continue
		}
		detail.Decisions = append(detail.Decisions, d)
	}
	raw, _ := json.Marshal(detail)
	safe := cloudSafeAdaptationDetail(string(raw))
	if safe == nil {
		return
	}
	raw, _ = json.Marshal(safe)
	if err != nil && c != nil {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Kind: cloudAdaptationKind, Platform: PlatformOpenAI, Stage: "adaptation", Detail: string(raw), SkipMonitoring: true})
		return
	}
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	logger.FromContext(ctx).Debug("Cloud adaptation decisions", zap.ByteString("decisions", raw))
}

// Reasons are a closed diagnostic vocabulary, never upstream or client text.
// Wrapping preserves errors.Is and the shared retry/account-health contract.
type cloudReasonError struct {
	cause         error
	reason, param string
}

func (e *cloudReasonError) Error() string { return e.cause.Error() }
func (e *cloudReasonError) Unwrap() error { return e.cause }
func cloudKnownFailureReason(reason string) string {
	switch reason {
	case "account_read_failed", "grant_read_failed", "grant_write_failed", "signing_keys_unavailable", "native_refresh_unavailable", "session_lookup_failed", "session_create_failed", "command_enqueue_failed", "event_read_failed", "event_decode_failed", "event_publish_failed", "request_state_failed", "owner_lease_expired", "owner_record_missing", "account_inactive", "account_unschedulable", "account_expired", "account_deleted", "cloud_disabled", "grant_revoked", "identity_changed", "settings_changed", "native_credentials_rejected", "current_turn_settings_changed", "parent_not_latest", "pending_results_required", "user_turn_requires_message", "response_already_active", "pending_tool_unknown", "pending_tool_unpublished", "turn_input_too_large":
		return reason
	}
	return ""
}
func cloudWithFailureReason(err error, reason, param string) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if reason = cloudKnownFailureReason(reason); reason == "" {
		return err
	}
	return &cloudReasonError{cause: err, reason: reason, param: cloudSafeParameter(param)}
}
func cloudFailureReason(err error) string {
	var detail *cloudReasonError
	if errors.As(err, &detail) {
		return cloudKnownFailureReason(detail.reason)
	}
	return ""
}

func cloudPublicErrorFields(err error, code, message string) map[string]string {
	fields := map[string]string{"code": code, "message": message}
	var detail *cloudReasonError
	if errors.As(err, &detail) {
		if reason := cloudKnownFailureReason(detail.reason); reason != "" {
			fields["reason"] = reason
		}
		if param := cloudSafeParameter(detail.param); param != "" {
			fields["param"] = param
		}
	}
	var scoped interface{ RequestScoped() bool }
	if errors.As(err, &scoped) && scoped.RequestScoped() {
		status, _, _, _ := cloudPublicError(err)
		fields["type"] = "invalid_request_error"
		if status >= 500 {
			fields["type"] = "api_error"
		}
	}
	var rejected *cloudRejection
	if errors.As(err, &rejected) {
		fields["param"] = rejected.Param
	}
	var upstream *cloudUpstreamError
	var rpc *cloudRPCError
	if errors.As(err, &rpc) {
		upstream = rpc.Upstream
	} else {
		_ = errors.As(err, &upstream)
	}
	if upstream != nil && upstream.Param != "" {
		fields["param"] = upstream.Param
	}
	return fields
}

// WriteOpenAICloudAdaptationError preserves the same error/diagnostic contract
// when continuation validation ends at the public handler before Forward.
func WriteOpenAICloudAdaptationError(c *gin.Context, err error) bool {
	var decisions []cloudAdaptationDecision
	if c != nil && c.Request != nil {
		decisions = cloudIngressAdaptations(c.Request.Context())
	}
	recordCloudAdaptation(c, decisions, err)
	var rejected *cloudRejection
	if !errors.As(err, &rejected) {
		return false
	}
	writeCloudGatewayError(c, err)
	return true
}

func recordCloudFailure(c *gin.Context, payload []byte) {
	if c == nil {
		return
	}
	facts := cloudDiagnosticFailure(string(payload))
	if facts == nil {
		return
	}
	raw, err := json.Marshal(map[string]any{"error": facts})
	if err != nil {
		return
	}
	status, _ := facts["upstream_http_status"].(int)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Kind: cloudFailureKind, Platform: PlatformOpenAI, Stage: "response", UpstreamStatusCode: status, Detail: string(raw), SkipMonitoring: true})
}
