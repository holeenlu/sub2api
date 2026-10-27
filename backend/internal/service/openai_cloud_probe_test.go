package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCloudProbePublicErrorKeepsStageAndSafeUpstreamCode(t *testing.T) {
	for _, test := range []struct {
		name, stage, check, code, detail string
		cause                            error
		result                           *OpenAICloudResult
	}{
		{"settlement", "client_tool", "settlement", "cloud_probe_settlement_failed", "settlement was rejected", ErrOpenAICloudProbeSettlementFailed, nil},
		{"timeout", "tool_image", "request", "cloud_verification_timeout", "timed out", context.DeadlineExceeded, nil},
		{"canceled", "image_generation", "request", "cloud_verification_canceled", "canceled", context.Canceled, nil},
		{"handshake", "web_search", "request", "server_is_overloaded", "HTTP 503", &cloudHandshakeError{StatusCode: 503}, nil},
		{"terminal", "encrypted_context_replay", "result", "invalid_encrypted_content", "HTTP 400", errors.New("private-secret"), &OpenAICloudResult{ErrorStatus: 400, Response: json.RawMessage(`{"error":{"code":"invalid_encrypted_content","message":"private-secret"}}`)}},
		{"terminal authorization", "client_tool", "result", "cloud_authorization_failed", "Reconnect", errors.New("private-secret"), &OpenAICloudResult{ErrorStatus: 403, Response: json.RawMessage(`{"error":{"code":"cloud_authorization_failed"}}`)}},
		{"terminal RPC", "client_tool", "result", "cloud_method_unavailable", "protocol method", errors.New("private-secret"), &OpenAICloudResult{ErrorStatus: 502, Response: json.RawMessage(`{"error":{"code":"cloud_method_unavailable"}}`)}},
		{"terminal unknown code", "client_tool", "result", "cloud_upstream_error", "HTTP 502", errors.New("private-secret"), &OpenAICloudResult{ErrorStatus: 502, Response: json.RawMessage(`{"error":{"code":"private-secret","message":"private-secret"}}`)}},
		{"local", "client_tool", "close_session", "cloud_verification_failed", "close_session", errors.New("private-secret"), nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := &cloudProbeFailure{stage: test.stage, check: test.check, cause: test.cause}
			diagnostic := cloudProbePublicError(err, test.result)
			require.Equal(t, test.code, diagnostic["code"])
			require.Contains(t, diagnostic["message"], test.stage+"/"+test.check)
			require.Contains(t, diagnostic["message"], test.detail)
			require.NotContains(t, diagnostic["message"], "private-secret")
		})
	}
}

func cloudProbeEvidenceResult(t *testing.T, output []any) *OpenAICloudResult {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"id": "resp_probe", "model": "model", "status": "completed", "output": output})
	require.NoError(t, err)
	return &OpenAICloudResult{Response: raw, SessionID: "session", ThreadID: "thread", RootRequestID: "root", Terminal: true, UsageCredible: true, Usage: &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}}
}

func TestCloudProbeRequiresCompletedMeasuredResult(t *testing.T) {
	valid := cloudProbeEvidenceResult(t, nil)
	require.NoError(t, cloudProbeCompleted(valid, "root", "model"))
	for name, mutate := range map[string]func(*OpenAICloudResult){
		"unknown usage": func(r *OpenAICloudResult) { r.UsageCredible = false },
		"missing usage": func(r *OpenAICloudResult) { r.Usage = nil },
		"pending tool":  func(r *OpenAICloudResult) { r.AwaitingTools = true },
		"not terminal":  func(r *OpenAICloudResult) { r.Terminal = false },
		"wrong root":    func(r *OpenAICloudResult) { r.RootRequestID = "other" },
		"missing thread": func(r *OpenAICloudResult) {
			r.ThreadID = ""
		},
		"upstream error": func(r *OpenAICloudResult) { r.ErrorStatus = 429 },
		"unmeasured zero": func(r *OpenAICloudResult) {
			r.Usage = &OpenAICloudUsage{}
		},
		"inconsistent total": func(r *OpenAICloudResult) {
			r.Usage = &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 10}
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := *valid
			mutate(&copy)
			require.Error(t, cloudProbeCompleted(&copy, "root", "model"))
		})
	}
	require.Error(t, cloudProbeCompleted(valid, "root", "different-model"))
}

func TestCloudProbeToolRequiresExactPendingCall(t *testing.T) {
	item := map[string]any{"type": "function_call", "name": "echo", "call_id": "call_probe", "arguments": `{"value":"expected"}`}
	result := cloudProbeEvidenceResult(t, []any{item})
	result.AwaitingTools, result.Terminal, result.Usage = true, false, nil
	responseID, callID, err := cloudProbeToolCall(result, "echo", map[string]any{"value": "expected"})
	require.NoError(t, err)
	require.Equal(t, "resp_probe", responseID)
	require.Equal(t, "call_probe", callID)
	_, _, err = cloudProbeToolCall(result, "echo", map[string]any{"value": "wrong"})
	require.Error(t, err)
	_, _, err = cloudProbeToolCall(result, "another-tool", map[string]any{"value": "expected"})
	require.Error(t, err)
	result.Usage = &OpenAICloudUsage{}
	_, _, err = cloudProbeToolCall(result, "echo", map[string]any{"value": "expected"})
	require.Error(t, err, "premature zero usage cannot certify the pending tool path")
}

func TestCloudProbePNGAndEncryptedHistoryEvidence(t *testing.T) {
	imageURL, err := cloudProbePNG()
	require.NoError(t, err)
	data, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(imageURL, "data:image/png;base64,"))
	require.NoError(t, err)
	bitmap, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, color.RGBA{B: 255, A: 255}, color.RGBAModel.Convert(bitmap.At(20, 20)))
	require.Equal(t, color.RGBA{R: 255, G: 255, A: 255}, color.RGBAModel.Convert(bitmap.At(140, 20)))
	initial := map[string]any{"role": "user", "content": "original input"}
	raw := json.RawMessage(`{"id":"reasoning","type":"reasoning","summary":[],"encrypted_content":" opaque+/=\n内容 "}`)
	result := cloudProbeEvidenceResult(t, []any{raw, map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "420"}}}})
	history, err := cloudProbeEncryptedHistory(result, initial)
	require.NoError(t, err)
	require.Len(t, history, 3)
	require.Equal(t, initial, history[0])
	require.Equal(t, raw, history[1], "opaque encrypted public item must be replayed unchanged")
	for _, invalid := range []json.RawMessage{
		json.RawMessage(`{"type":"reasoning","summary":[]}`),
		json.RawMessage(`{"type":"reasoning","summary":[],"encrypted_content":""}`),
		json.RawMessage(`{"type":"reasoning","summary":[],"encrypted_content":"cipher","content":"private"}`),
		json.RawMessage(`{"type":"reasoning","summary":[],"encrypted_content":"cipher","internal_metadata":{}}`),
	} {
		_, err := cloudProbeEncryptedHistory(cloudProbeEvidenceResult(t, []any{invalid}), initial)
		require.Error(t, err)
	}
}

func TestCloudProbeHostedEvidenceRejectsTextWithoutRealResults(t *testing.T) {
	result := cloudProbeEvidenceResult(t, []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "I searched the web and generated an image."}}}})
	result.Usage.WebSearchCalls, result.Usage.ImageCount = 1, 1
	events := map[string]int{"response.web_search_call.completed": 1, "response.image_generation_call.completed": 1}
	require.Error(t, cloudProbeSearchEvidence(result, events))
	require.Error(t, cloudProbeImageEvidence(result, events))
	search := map[string]any{"type": "web_search_call", "status": "completed", "action": map[string]any{"type": "search", "query": "official documentation", "sources": []any{map[string]any{"type": "url", "url": "https://platform.openai.com/docs/api-reference/responses"}}}}
	result = cloudProbeEvidenceResult(t, []any{search})
	result.Usage.WebSearchCalls = 1
	require.NoError(t, cloudProbeSearchEvidence(result, events))
	for _, action := range []map[string]any{{"type": "bogus"}, {"type": "open_page", "url": "not-a-url"}, {"type": "find_in_page", "url": "https://example.com"}} {
		action["sources"] = []any{map[string]any{"type": "url", "url": "https://example.com"}}
		invalid := cloudProbeEvidenceResult(t, []any{map[string]any{"type": "web_search_call", "status": "completed", "action": action}})
		invalid.Usage.WebSearchCalls = 1
		require.Error(t, cloudProbeSearchEvidence(invalid, events))
	}
	require.Error(t, cloudProbeSearchEvidence(result, map[string]int{}))
	result.Usage.WebSearchCalls = 2
	require.Error(t, cloudProbeSearchEvidence(result, events))
	search["action"] = map[string]any{"type": "search", "query": "documentation", "sources": []any{}}
	result = cloudProbeEvidenceResult(t, []any{search})
	result.Usage.WebSearchCalls = 1
	require.Error(t, cloudProbeSearchEvidence(result, events))
}

func TestCloudProbeImageRequiresFullyDecodedPixelsAndMeasuredDimensions(t *testing.T) {
	imageURL, err := cloudProbePNG()
	require.NoError(t, err)
	encoded := strings.TrimPrefix(imageURL, "data:image/png;base64,")
	tier, ok := ClassifyImageBillingTier("160x96")
	require.True(t, ok)
	item := map[string]any{"type": "image_generation_call", "status": "completed", "result": encoded, "output_format": "png", "size": "160x96"}
	result := cloudProbeEvidenceResult(t, []any{item})
	result.Usage.ImageCount, result.Usage.ImageSizes, result.Usage.ImageOutputSizes = 1, map[string]int{tier: 1}, []string{"160x96"}
	events := map[string]int{"response.image_generation_call.completed": 1}
	require.NoError(t, cloudProbeImageEvidence(result, events))
	require.Error(t, cloudProbeImageEvidence(result, nil))
	result.Usage.ImageOutputSizes[0] = "1024x1024"
	require.Error(t, cloudProbeImageEvidence(result, events))
	result.Usage.ImageOutputSizes[0] = "160x96"
	data, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	for _, invalid := range []string{"not-base64", base64.StdEncoding.EncodeToString(data[:40]), base64.StdEncoding.EncodeToString(append(append([]byte(nil), data...), 'x'))} {
		item["result"] = invalid
		changed := cloudProbeEvidenceResult(t, []any{item})
		changed.Usage = result.Usage
		require.Error(t, cloudProbeImageEvidence(changed, events))
	}
}

func TestCloudAnswerTextPrefersDeclaredFinalAnswer(t *testing.T) {
	message := func(role, phase, text string) map[string]any {
		item := map[string]any{"type": "message", "role": role, "content": []any{map[string]any{"type": "output_text", "text": text}}}
		if phase != "" {
			item["phase"] = phase
		}
		return item
	}
	for _, test := range []struct {
		name   string
		output []any
		want   string
	}{
		{"final", []any{message("assistant", "commentary", "Working"), message("assistant", "partial_answer", "Partial"), message("assistant", "", "Unknown"), message("assistant", "final_answer", "Answer"), message("assistant", "final_answer", "Two")}, "AnswerTwo"},
		{"legacy", []any{message("assistant", "commentary", "Working"), message("assistant", "partial_answer", "Partial"), message("assistant", "", "Unknown")}, "WorkingPartialUnknown"},
		{"foreign final", []any{message("user", "final_answer", "User"), message("assistant", "", "Legacy")}, "Legacy"},
		{"empty final", []any{message("assistant", "commentary", "Working"), message("assistant", "final_answer", "")}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"output": test.output})
			require.NoError(t, err)
			require.Equal(t, test.want, cloudAnswerText(raw))
		})
	}
}

func TestCloudProbeEncryptedMissingDiagnostic(t *testing.T) {
	result := cloudProbeEvidenceResult(t, []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "420"}}}})
	_, err := cloudProbeEncryptedHistory(result, map[string]any{"role": "user", "content": "simple arithmetic"})
	require.ErrorIs(t, err, errCloudProbeEncryptedMissing)
	diagnostic := cloudProbePublicError(&cloudProbeFailure{stage: "encrypted_context_create", check: "evidence", cause: err}, result)
	require.Equal(t, "cloud_encrypted_context_missing", diagnostic["code"])
	require.Contains(t, diagnostic["message"], "does not establish that the model is unavailable")
}

func TestCloudProbeEncryptedRetryRequiresSettlementCleanupAndActualCipher(t *testing.T) {
	for _, scenario := range []string{"first cipher", "second cipher", "no cipher", "cleanup failure", "request failure", "invalid evidence"} {
		t.Run(scenario, func(t *testing.T) {
			runs, finishes := 0, 0
			stop := errors.New("deliberate failure")
			history, replay, err := cloudProbeEncryptedContext(func(body map[string]any) (*OpenAICloudResult, error) {
				runs++
				require.Equal(t, runs-1, finishes, "settle and close previous root before retry")
				require.Equal(t, "high", body["reasoning"].(map[string]string)["effort"])
				if scenario == "request failure" {
					return nil, stop
				}
				var output []any
				if scenario == "first cipher" || scenario == "second cipher" && runs == 2 {
					output = []any{map[string]any{"type": "reasoning", "summary": []any{}, "encrypted_content": "opaque+/="}}
				}
				if scenario == "invalid evidence" {
					output = []any{map[string]any{"type": "reasoning", "encrypted_content": "opaque", "content": "private"}}
				}
				return cloudProbeEvidenceResult(t, output), nil
			}, func(_ *OpenAICloudResult, expected string) error {
				finishes++
				if finishes == 1 {
					require.Equal(t, "420", expected)
				} else {
					require.Equal(t, "532", expected)
				}
				if scenario == "cleanup failure" {
					return stop
				}
				return nil
			})
			switch scenario {
			case "first cipher":
				require.NoError(t, err)
				require.Equal(t, "543", replay)
				require.Equal(t, 1, runs)
				require.Len(t, history, 2)
			case "second cipher":
				require.NoError(t, err)
				require.Equal(t, "655", replay)
				require.Equal(t, 2, runs)
				require.Len(t, history, 2)
			case "no cipher":
				require.ErrorIs(t, err, errCloudProbeEncryptedMissing)
				require.Equal(t, 2, runs)
				require.Equal(t, 2, finishes)
			case "invalid evidence":
				require.Error(t, err)
				require.NotErrorIs(t, err, errCloudProbeEncryptedMissing)
				require.Equal(t, 1, runs)
			default:
				require.ErrorIs(t, err, stop)
				require.Equal(t, 1, runs)
			}
		})
	}
}

func TestCloudProbeResolvesOrdinaryAliasesWithoutChangingSelectedProtocol(t *testing.T) {
	for _, extra := range []map[string]any{
		{"openai_passthrough": true},
		{"openai_excel_bps": true, "openai_excel_bps_models": []string{"other-model"}},
		{OpenAICloudEnabledKey: true, "openai_passthrough": true},
	} {
		t.Run(fmt.Sprint(extra), func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: extra, Credentials: map[string]any{"model_mapping": map[string]any{"public-alias": "gpt-6-luna", "wild-*": "gpt-6-sol"}}}
			before, _ := json.Marshal(account.Extra)
			upstream, err := cloudProbeModel(account, "public-alias")
			require.NoError(t, err)
			require.Equal(t, "gpt-6-luna", upstream)
			upstream, err = cloudProbeModel(account, "wild-small")
			require.NoError(t, err)
			require.Equal(t, "gpt-6-sol", upstream)
			_, err = cloudProbeModel(account, "gpt-6-luna")
			require.ErrorIs(t, err, ErrOpenAICloudSettings, "upstream targets are not extra public whitelist entries")
			_, err = cloudProbeModel(account, "not-allowed")
			require.ErrorIs(t, err, ErrOpenAICloudSettings)
			after, _ := json.Marshal(account.Extra)
			require.Equal(t, string(before), string(after), "manual test cannot change active protocol or passthrough")
		})
	}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	upstream, err := cloudProbeModel(account, "gpt-6-luna")
	require.NoError(t, err)
	require.Equal(t, "gpt-6-luna", upstream)
	require.Nil(t, account.Extra)
}
