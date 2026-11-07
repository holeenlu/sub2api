package service

import (
	"encoding/json"
	"errors"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

// Credential preparation has no inference endpoint/status. Publish the current
// account-auth fact while retaining prior attempts in the existing Ops timeline.
// Only allowlisted reason codes may cross this boundary, never refresh prose.
func recordExcelBPSCredentialFailure(c *gin.Context, account *Account, err error) {
	var failure *UpstreamFailoverError
	if c == nil || account == nil || !errors.As(err, &failure) || !failure.IsCredentialFailure() || failure.Reason != "basispoints_credentials_unavailable" {
		return
	}
	reason := "unavailable"
	switch code := infraerrors.Reason(failure.Cause); code {
	case "OPENAI_EXCEL_AUTH_REQUIRED", "OPENAI_EXCEL_AUTH_STORAGE_UNAVAILABLE", "OPENAI_EXCEL_AUTH_REFRESH_FAILED", "OPENAI_EXCEL_AUTH_REAUTHORIZE_REQUIRED":
		reason = code
	}
	detail, _ := json.Marshal(map[string]string{"credential_reason": reason})
	const message = "Excel BPS credentials are unavailable"
	ClearActualOpenAIUpstreamEndpoint(c)
	c.Set(OpsUpstreamStatusCodeKey, 0)
	c.Set(OpsUpstreamErrorMessageKey, message)
	c.Set(OpsUpstreamErrorDetailKey, string(detail))
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Kind: "credential_failover", Stage: string(failure.Stage), Scope: string(failure.Scope),
		Reason: string(failure.Reason), Platform: account.Platform, AccountID: account.ID,
		Message: message, Detail: string(detail), ProxyName: opsProxyNameUnknown,
	})
}
