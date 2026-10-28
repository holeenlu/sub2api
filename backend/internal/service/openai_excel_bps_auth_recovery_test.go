//go:build unit

package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExcelBPSAuthRecoveryPreservesOnlyCompleteEncryptedHistory(t *testing.T) {
	complete := []byte(`{"input":[{"type":"compaction","encrypted_content":"opaque"},{"type":"function_call","call_id":"call_1","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"done"}]}`)
	for _, failure := range []*UpstreamFailoverError{
		{StatusCode: 429, Reason: ExcelBPSRateLimitedReason, RequestNotSent: true},
		{StatusCode: 503, Stage: GatewayFailureStageAccountAuth, Reason: "basispoints_credentials_unavailable", RequestNotSent: true},
		{StatusCode: 401, Stage: GatewayFailureStageAccountAuth, Reason: "basispoints_credentials_rejected", RejectedBeforeGeneration: true},
		{StatusCode: 403, Stage: GatewayFailureStageAccountAuth, Reason: "basispoints_credentials_rejected", RejectedBeforeGeneration: true},
		{StatusCode: 403, Reason: ExcelBPSUpstreamFailureReason, RejectedBeforeGeneration: true},
		{StatusCode: 403, Reason: ExcelBPSModelPermissionReason, RejectedBeforeGeneration: true},
	} {
		t.Run(fmt.Sprintf("%s/%d", failure.Reason, failure.StatusCode), func(t *testing.T) {
			require.True(t, OpenAIResponsesBPSPreGenerationReplayAllowed(complete, failure))
			require.False(t, OpenAIResponsesAccountReplayAllowed(complete), "Native replay remains unchanged")
			for _, body := range []string{
				`{"input":[{"type":"compaction","encrypted_content":"opaque"}],"previous_response_id":"resp_owner"}`,
				`{"input":[{"type":"reasoning","encrypted_content":"opaque"},{"role":"user","content":[{"type":"input_image","file_id":"file_owner"}]}]}`,
				`{"input":[{"type":"compaction","encrypted_content":"opaque"},{"type":"function_call_output","call_id":"missing","output":"done"}]}`,
				`{"input":[{"type":"agent_message","content":[{"type":"encrypted_content","encrypted_content":"task"}]}]}`,
			} {
				require.False(t, OpenAIResponsesBPSPreGenerationReplayAllowed([]byte(body), failure), "%s", body)
			}
			unproven := *failure
			unproven.RequestNotSent, unproven.RejectedBeforeGeneration = false, false
			require.False(t, OpenAIResponsesBPSPreGenerationReplayAllowed(complete, &unproven))
			for _, raw := range []string{
				`{"error":{"code":"content_policy_violation"}}`,
				`{"error":{"type":"safety_error"}}`,
				`{"usage":{"input_tokens":0}}`,
				`{"response":{"usage":{"output_tokens":1}}}`,
			} {
				refused := *failure
				refused.ResponseBody = []byte(raw)
				require.False(t, OpenAIResponsesBPSPreGenerationReplayAllowed(complete, &refused), "%s", raw)
			}
		})
	}
	for _, failure := range []*UpstreamFailoverError{
		nil,
		{StatusCode: 503, Reason: "credential_preparation_failed", RequestNotSent: true},
		{StatusCode: 503, Reason: "basispoints_credentials_unavailable", RequestNotSent: true},
		{StatusCode: 503, Stage: GatewayFailureStageAccountAuth, Reason: "basispoints_credentials_unavailable", RejectedBeforeGeneration: true},
		{StatusCode: 503, Stage: GatewayFailureStageAccountAuth, Reason: "basispoints_credentials_rejected", RequestNotSent: true},
		{StatusCode: 503, Reason: ExcelBPSRateLimitedReason, RequestNotSent: true},
		{StatusCode: 503, Reason: ExcelBPSUpstreamFailureReason, RequestNotSent: true},
	} {
		require.False(t, OpenAIResponsesBPSPreGenerationReplayAllowed(complete, failure), "%+v", failure)
	}
}

func TestExcelBPSCredentialDiagnosticsReplacePreviousAttachmentSummary(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		cause        error
	}{
		{"refresh", "OPENAI_EXCEL_AUTH_REFRESH_FAILED", infraerrors.ServiceUnavailable("OPENAI_EXCEL_AUTH_REFRESH_FAILED", "PRIVATE_REFRESH_PROSE")},
		{"unknown", "unavailable", errors.New("PRIVATE_CREDENTIAL=value")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{AccountID: 7, Platform: PlatformOpenAI, UpstreamStatusCode: 429, UpstreamURL: "https://bps.openai.com/basispoints/api/attachments", Kind: "failover"})
			SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/attachments")
			c.Set(OpsUpstreamStatusCodeKey, 429)
			c.Set(OpsUpstreamErrorDetailKey, "previous attachment failure")
			failure := &UpstreamFailoverError{Stage: GatewayFailureStageAccountAuth, Scope: GatewayFailureScopeProvider, Reason: "basispoints_credentials_unavailable", Cause: tc.cause, ClientMessage: "PRIVATE_CREDENTIAL_MESSAGE"}
			recordExcelBPSCredentialFailure(c, &Account{ID: 40, Platform: PlatformOpenAI}, failure)
			require.Empty(t, GetActualOpenAIUpstreamEndpoint(c))
			require.Zero(t, c.GetInt(OpsUpstreamStatusCodeKey))
			require.Equal(t, tc.reason, gjson.Get(c.GetString(OpsUpstreamErrorDetailKey), "credential_reason").String())
			raw, ok := c.Get(OpsUpstreamErrorsKey)
			require.True(t, ok)
			events := raw.([]*OpsUpstreamErrorEvent)
			require.Len(t, events, 2)
			require.Equal(t, int64(7), events[0].AccountID)
			require.Equal(t, 429, events[0].UpstreamStatusCode)
			require.Contains(t, events[0].UpstreamURL, "/attachments")
			require.Equal(t, int64(40), events[1].AccountID)
			require.Equal(t, "account_auth", events[1].Stage)
			require.Equal(t, "credential_failover", events[1].Kind)
			require.Empty(t, events[1].UpstreamURL)
			require.Zero(t, events[1].UpstreamStatusCode)
			require.NotContains(t, fmt.Sprint(events[1]), "PRIVATE_")
			require.NotContains(t, c.GetString(OpsUpstreamErrorMessageKey)+c.GetString(OpsUpstreamErrorDetailKey), "PRIVATE_")
		})
	}
}

func TestExcelBPSCredentialIngressRecordsCurrentFailure(t *testing.T) {
	svc := excelBPSGatewayTestService(nil)
	account := excelAccount()
	delete(account.Credentials, "client_id") // Requires the independent Excel grant provider, which is unavailable.
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/attachments")
	c.Set(OpsUpstreamStatusCodeKey, 429)
	_, _, err := svc.GetRequestCredential(t.Context(), c, account, "gpt-6-astra")
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.True(t, failure.IsCredentialFailure())
	require.Empty(t, GetActualOpenAIUpstreamEndpoint(c))
	require.Zero(t, c.GetInt(OpsUpstreamStatusCodeKey))
	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events := raw.([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.Equal(t, account.ID, events[0].AccountID)
	require.Equal(t, "account_auth", events[0].Stage)
}

func TestExcelBPSFailureHealthUsesCentralExclusions(t *testing.T) {
	svc := &OpenAIGatewayService{openaiAccountStats: newOpenAIAccountRuntimeStats()}
	account := &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	// Do not set RequestScopedTransient: the central method must respect each
	// established BPS reason even when another consumer did not copy that flag.
	for _, failure := range []*UpstreamFailoverError{
		{StatusCode: 429, Scope: GatewayFailureScopeAccount, Reason: ExcelBPSRateLimitedReason},
		{StatusCode: 403, Scope: GatewayFailureScopeAccount, Reason: ExcelBPSModelPermissionReason},
		{StatusCode: 503, Scope: GatewayFailureScopeAccount, Reason: ExcelBPSUpstreamFailureReason},
		{StatusCode: 503, Stage: GatewayFailureStageAccountAuth, Scope: GatewayFailureScopeAccount, Reason: "basispoints_credentials_unavailable"},
		{StatusCode: 503, ResponseBody: []byte(`{"error":{"code":"content_policy_violation"}}`)},
	} {
		require.False(t, failure.ShouldReportAccountScheduleFailure())
		attribution := classifyOpenAIAccountScheduleFailure(t.Context(), fmt.Errorf("consumer: %w", failure))
		require.NotEmpty(t, attribution.reason)
		require.False(t, attribution.observeProvider, "BPS rejection cannot enter shared Native health observation")
		svc.ReportOpenAIAccountScheduleFailure(t.Context(), account, "gpt-6-astra", failure)
		svc.ObserveOpenAIAccountHealthFailure(t.Context(), account, failure)
	}
	require.Zero(t, svc.openaiAccountStats.size())
	for range openAIHealthMinSamples {
		svc.ReportOpenAIAccountScheduleFailure(t.Context(), account, "gpt-6-astra", errors.New("native upstream reset"))
	}
	rate, _, _ := svc.openaiAccountStats.snapshot(account.ID, "gpt-6-astra")
	require.Greater(t, rate, 0.0, "actual Native health failures must still be recorded")
}
