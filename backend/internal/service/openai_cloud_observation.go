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
	logger.FromContext(ctx).Warn("Cloud adaptation decisions", zap.ByteString("decisions", raw))
}
func cloudPublicErrorFields(err error, code, message string) map[string]string {
	fields := map[string]string{"code": code, "message": message}
	var rejected *cloudRejection
	if errors.As(err, &rejected) {
		fields["param"] = rejected.Param
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
