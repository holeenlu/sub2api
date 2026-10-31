//go:build unit

package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSAttachment429ThenCredentialFailureKeepsBPSMode(t *testing.T) {
	for _, scenario := range []string{"json", "sse", "ws", "ws_with_request_id"} {
		t.Run(scenario, func(t *testing.T) {
			protocol := scenario
			if protocol == "ws_with_request_id" {
				protocol = "ws"
			}
			var accounts []service.Account
			for _, id := range []int64{10, 20, 25, 30} {
				clientID := openai.ExcelClientID
				if id == 20 {
					clientID = openai.ClientID // A healthy Native candidate must remain ineligible.
				}
				credentials := map[string]any{"client_id": clientID, "access_token": fmt.Sprintf("token-%d", id), "chatgpt_account_id": fmt.Sprintf("workspace-%d", id)}
				if id == 25 {
					credentials["expires_at"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
				}
				accounts = append(accounts, service.Account{
					ID: id, Name: fmt.Sprintf("bps-recovery-%d", id), Priority: int(id), Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
					Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{nativeHandlerFixtureGroupID}, Credentials: credentials,
					Extra: map[string]any{"openai_excel_bps": id != 20, "openai_excel_bps_models": []string{"gpt-6-astra"}, "openai_oauth_responses_websockets_v2_mode": "http_bridge"},
				})
			}
			var imageBytes bytes.Buffer
			require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 3))))
			var body map[string]any
			require.NoError(t, json.Unmarshal([]byte(fmt.Sprintf(excelBPSFailoverRequestBody, excelBPSHandlerStream(protocol))), &body))
			body["input"] = append([]any{map[string]any{"type": "compaction", "encrypted_content": "PRIVATE_COMPLETE_HISTORY"}}, body["input"].([]any)...)
			body["input"] = append(body["input"].([]any), map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes.Bytes())}}})
			if protocol == "ws" {
				body["type"] = "response.create"
			}
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			upstream := &excelBPSFailoverUpstream{}
			upstream.answer = func(call int) *http.Response {
				id := upstream.calls()[call]
				if id == 10 {
					return excelBPS429("30")
				}
				if id != 30 {
					t.Errorf("unavailable grant or wrong mode reached upstream: account=%d", id)
				}
				if upstream.urls[call] == basispoints.AttachmentsURL {
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"openai_file_id":"file-recovered-owner"}`))}
				}
				response := excelBPSCompleted()
				if scenario == "ws_with_request_id" {
					response.Header.Set("X-Request-Id", "bps-http-request-correlation")
				}
				return response
			}
			usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 4)}
			journal := &bpsModeHandlerJournal{wsNativeFixtureJournal: &wsNativeFixtureJournal{logSink: usage}}
			h := newExcelBPSFailoverTestHandler(t, upstream, false, excelBPSFailoverTestOptions{
				accounts: accounts, usage: usage, journal: journal,
				settings: map[string]string{service.SettingKeyExcelBPSImageRelayEnabled: "true", service.SettingKeyExcelBPSImageMode: "native"},
			})
			result := runExcelBPSHandlerRequest(t, h, protocol, raw)
			assertExcelBPSHandlerSuccess(t, protocol, result)
			generations := 0
			for _, endpoint := range upstream.urls {
				if endpoint == basispoints.ResponsesURL {
					generations++
				}
			}
			require.Equal(t, 1, generations, "extra generation is forbidden: accounts=%v URLs=%v", upstream.calls(), upstream.urls)
			require.Equal(t, []int64{10, 30, 30}, upstream.calls(), "the failed grant must not send and Native must not become a fallback; URLs=%v", upstream.urls)
			require.Equal(t, []string{basispoints.AttachmentsURL, basispoints.AttachmentsURL, basispoints.ResponsesURL}, upstream.urls)
			generation := string(upstream.bodies[2])
			for _, retained := range []string{"PRIVATE_COMPLETE_HISTORY", "file-recovered-owner", "call_ls", "a.txt", "shell"} {
				require.Contains(t, generation, retained)
			}
			eventValue, ok := result.context.Get(service.OpsUpstreamErrorsKey)
			require.True(t, ok)
			credentialObserved, attachmentObserved := false, false
			for _, event := range eventValue.([]*service.OpsUpstreamErrorEvent) {
				credentialObserved = credentialObserved || event.AccountID == 25 && event.Stage == "account_auth"
				attachmentObserved = attachmentObserved || event.AccountID == 10 && event.UpstreamStatusCode == 429
			}
			require.True(t, attachmentObserved, "keep the prior attachment rejection in the timeline")
			require.True(t, credentialObserved, "prove the real credential failure branch was exercised, not skipped by selection")
			require.NotNil(t, journal.input, "the final real BPS callback must run after the rejected control's durable receipt")
			require.NotNil(t, journal.input.Command)
			require.Equal(t, "resp_bps_failover", journal.input.Command.RequestID)
			require.Equal(t, 3, journal.input.Command.InputTokens)
			require.Equal(t, 1, journal.input.Command.OutputTokens)
			require.False(t, journal.input.Command.UsageLog.UsageLowerBound, "confirmed controls are not missing generation usage")
			require.Len(t, journal.preserved, 1, "same-mode recovery must retain the authenticated root")
			require.Len(t, journal.input.Finalization.Operation.Attempts, 3)
			for i, attempt := range journal.input.Finalization.Operation.Attempts {
				require.EqualValues(t, i+1, attempt.Sequence)
				require.Equal(t, service.VendorOpenAIModeBPS, attempt.Identity.Mode)
				if i < 2 {
					require.Equal(t, model.AttemptControl, attempt.Identity.Role)
					require.Nil(t, attempt.Usage, "attachment controls cannot invent inference usage")
				}
			}
			require.Equal(t, []uint64{2, 3}, journal.input.Finalization.CurrentSequences, "already durable account-10 evidence is not reopened or billed again")
			require.Equal(t, []uint64{3}, journal.input.ChargedSequences)
			select {
			case log := <-usage.created:
				require.Equal(t, "resp_bps_failover", log.RequestID)
				require.Equal(t, int64(30), log.AccountID)
				require.Equal(t, service.UsageAuthorizationModeBPS, log.AuthorizationMode)
				require.Positive(t, log.InputTokens)
				require.Positive(t, log.OutputTokens)
			case <-time.After(3 * time.Second):
				t.Fatal("the successful BPS generation did not record usage")
			}
			require.Empty(t, usage.created, "rejected upload/credential preparation must not produce another bill")
		})
	}
}
