package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A conversation that ran on another upstream or account (BPS, Cloud, another
// native account) replays ciphertext this upstream cannot read: reasoning,
// output-token replay on assistant messages, and a compaction.
const foreignEncryptedHistoryBody = `{"model":"gpt-5.5","stream":false,"store":false,"include":["reasoning.encrypted_content"],"prompt_cache_key":"foreign-session","input":[` +
	`{"type":"message","role":"user","content":[{"type":"input_text","text":"start"}],"internal_chat_message_metadata_passthrough":{"create_time":1792843227.652608}},` +
	`{"type":"compaction","id":"cmp_foreign","encrypted_content":"gAAAA-foreign-compaction"},` +
	`{"type":"reasoning","id":"rs_foreign","summary":[{"type":"summary_text","text":"thought"}],"content":null,"encrypted_content":"gAAAA-foreign-reasoning"},` +
	`{"type":"message","role":"assistant","id":"msg_foreign","content":[{"type":"output_text","text":"visible answer"}],"encrypted_content":"gAAAA-foreign-replay"},` +
	`{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"n\":1}"},` +
	`{"type":"function_call_output","call_id":"call_1","output":"tool result"},` +
	`{"type":"message","role":"user","content":[{"type":"input_text","text":"next"}]}` +
	`],"max_output_tokens":12345678901234567890}`

func encryptedRejection(item string) string {
	return `{"error":{"message":"The encrypted content for item ` + item + ` could not be verified. Reason: Encrypted content could not be decrypted or parsed.","type":"invalid_request_error","param":"input","code":"invalid_encrypted_content"},"status":400}`
}

func inputTypes(body []byte) []string {
	var types []string
	for _, item := range gjson.GetBytes(body, "input").Array() {
		types = append(types, item.Get("type").String())
	}
	return types
}

// Each reduction is tried once: reasoning ciphertext first, then — only when
// still rejected or when the rejection names the compaction — the compaction
// is replaced by a notice. Visible messages, calls and outputs always stay.
func TestNormalizeRejectedEncryptedHistoryStagesRecovery(t *testing.T) {
	body := []byte(foreignEncryptedHistoryBody)

	stage1, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, []byte(encryptedRejection("rs_foreign")))
	require.NoError(t, err)
	require.True(t, changed, reason)
	require.True(t, strings.HasPrefix(reason, openAIEncryptedHistoryRecoveryReason), reason)
	require.Equal(t, []string{"message", "compaction", "message", "function_call", "function_call_output", "message"}, inputTypes(stage1), "the reasoning item is omitted; the compaction is kept for now")
	require.NotContains(t, string(stage1), "gAAAA-foreign-reasoning")
	require.NotContains(t, string(stage1), "gAAAA-foreign-replay", "output-token replay ciphertext is removed")
	require.Equal(t, "visible answer", gjson.GetBytes(stage1, "input.2.content.0.text").String(), "the visible answer is kept")
	require.Contains(t, string(stage1), "gAAAA-foreign-compaction")
	require.Equal(t, "12345678901234567890", gjson.GetBytes(stage1, "max_output_tokens").Raw, "other fields are byte-identical")
	require.Equal(t, "1792843227.652608", gjson.GetBytes(stage1, "input.0.internal_chat_message_metadata_passthrough.create_time").Raw, "numbers keep their exact form")
	require.Equal(t, `["reasoning.encrypted_content"]`, gjson.GetBytes(stage1, "include").Raw, "the retry still asks for its own reasoning ciphertext")

	stage2, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, stage1, []byte(encryptedRejection("cmp_foreign")))
	require.NoError(t, err)
	require.True(t, changed, reason)
	require.Equal(t, []string{"message", "message", "message", "function_call", "function_call_output", "message"}, inputTypes(stage2))
	require.Equal(t, "developer", gjson.GetBytes(stage2, "input.1.role").String())
	require.Equal(t, openAIOmittedCompactionNotice, gjson.GetBytes(stage2, "input.1.content.0.text").String())
	require.NotContains(t, string(stage2), "encrypted_content\":\"gAAAA", "no ciphertext remains")

	_, reason, changed, err = normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, stage2, []byte(encryptedRejection("unknown")))
	require.NoError(t, err)
	require.False(t, changed, "nothing removable is left; the rejection reaches the client")
	require.Contains(t, reason, "no removable ciphertext")

	for name, rejection := range map[string]string{
		"rejection names the compaction": encryptedRejection("cmp_foreign"),
		"compaction error code":          `{"error":{"code":"invalid_compaction","type":"invalid_request_error","message":"The compacted context could not be verified."}}`,
	} {
		t.Run(name, func(t *testing.T) {
			both, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, []byte(rejection))
			require.NoError(t, err)
			require.True(t, changed, reason)
			require.Equal(t, []string{"message", "message", "message", "function_call", "function_call_output", "message"}, inputTypes(both), "a rejected compaction is replaced in the same single retry")
			require.Equal(t, openAIOmittedCompactionNotice, gjson.GetBytes(both, "input.1.content.0.text").String())
		})
	}
}

func TestNormalizeRejectedEncryptedHistoryRecognizesProviderShapes(t *testing.T) {
	body := []byte(foreignEncryptedHistoryBody)
	for name, rejection := range map[string]string{
		"message only":         `{"error":{"message":"The encrypted content gAAAA...end could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}`,
		"signature code":       `{"error":{"code":"thinking_signature_invalid","message":"The encrypted content gAAAA could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}`,
		"WS bare error":        `{"type":"error","error":{"type":"invalid_request_error","code":"invalid_encrypted_content","message":"The encrypted content could not be verified"}}`,
		"WS response.failed":   `{"type":"response.failed","response":{"id":"resp_x","error":{"code":"invalid_encrypted_content","message":"The encrypted content could not be verified"}}}`,
		"flat event code only": `{"type":"error","code":"invalid_encrypted_content","message":"Please retry"}`,
	} {
		t.Run(name, func(t *testing.T) {
			retry, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, []byte(rejection))
			require.NoError(t, err)
			require.True(t, changed, reason)
			require.NotContains(t, string(retry), "gAAAA-foreign-reasoning")
		})
	}
}

func TestNormalizeRejectedEncryptedHistoryDeclinesUnsafeOrIrrelevantCases(t *testing.T) {
	body := []byte(foreignEncryptedHistoryBody)
	for name, tc := range map[string]struct {
		status    int
		body      string
		rejection string
	}{
		"server error":         {http.StatusInternalServerError, foreignEncryptedHistoryBody, encryptedRejection("rs_foreign")},
		"unrelated 400":        {http.StatusBadRequest, foreignEncryptedHistoryBody, `{"error":{"code":"invalid_value","message":"bad value"}}`},
		"echoed text":          {http.StatusBadRequest, foreignEncryptedHistoryBody, `{"error":{"message":"Invalid input. User said: the encrypted content could not be verified and could not be decrypted or parsed"}}`},
		"stored previous":      {http.StatusBadRequest, strings.Replace(foreignEncryptedHistoryBody, `"store":false,`, `"store":false,"previous_response_id":"resp_prev",`, 1), encryptedRejection("rs_foreign")},
		"plain string input":   {http.StatusBadRequest, `{"model":"gpt-5.5","input":"hello"}`, encryptedRejection("rs_foreign")},
		"no ciphertext at all": {http.StatusBadRequest, `{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":"hello"}]}`, encryptedRejection("rs_foreign")},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(tc.status, []byte(tc.body), []byte(tc.rejection))
			require.NoError(t, err)
			require.False(t, changed)
		})
	}
	require.Equal(t, foreignEncryptedHistoryBody, string(body), "the caller's body is never mutated")
}

func newEncryptedRecoveryPassthroughAccount() *Account {
	account := newOpenAIOAuthNamespaceTestAccount()
	account.ID, account.Name = 5109, "openai-oauth-passthrough"
	account.Extra = map[string]any{"openai_passthrough": true}
	return account
}

// Native HTTP, OAuth and passthrough requests recover on the same account:
// one retry per reduction, the client sees only the successful response.
func TestOpenAIGatewayServiceRecoversForeignEncryptedHistoryOnSameAccount(t *testing.T) {
	sse := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_recovered\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\ndata: [DONE]\n\n"
	for _, tc := range []struct {
		name    string
		account *Account
		stream  bool
	}{
		{name: "api key", account: newOpenAIRejectedFieldTestAccount()},
		{name: "oauth", account: newOpenAIOAuthNamespaceTestAccount(), stream: true},
		{name: "oauth passthrough", account: newEncryptedRecoveryPassthroughAccount(), stream: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(strings.Replace(foreignEncryptedHistoryBody, `"stream":false`, fmt.Sprintf(`"stream":%t`, tc.stream), 1))
			success := newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"id":"resp_recovered","output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`)
			if tc.stream {
				success = newOpenAIRejectedFieldTestResponse(http.StatusOK, sse)
				success.Header.Set("Content-Type", "text/event-stream")
			}
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, encryptedRejection("rs_foreign")),
				newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, encryptedRejection("cmp_foreign")),
				success,
			}}
			svc := newOpenAIRejectedFieldTestService(upstream)
			c := newOpenAIRejectedFieldTestContext(body)
			result, err := svc.Forward(context.Background(), c, tc.account, body)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.bodies, 3, "one retry per reduction on the same account")
			require.Contains(t, string(upstream.bodies[0]), "gAAAA-foreign-reasoning", "the first attempt is the client's history as sent")
			require.NotContains(t, string(upstream.bodies[1]), "gAAAA-foreign-reasoning")
			require.Contains(t, string(upstream.bodies[1]), "gAAAA-foreign-compaction")
			require.NotContains(t, string(upstream.bodies[2]), "gAAAA-foreign-compaction")
			require.Contains(t, string(upstream.bodies[2]), openAIOmittedCompactionNotice)
			require.Contains(t, string(upstream.bodies[2]), "tool result")
			for _, req := range upstream.requests {
				require.Equal(t, upstream.requests[0].URL.Host, req.URL.Host, "every attempt goes to the same account endpoint")
			}
		})
	}
}

// When nothing removable is left the client receives the provider's
// encrypted-context rejection, as before.
func TestOpenAIGatewayServiceEncryptedHistoryRecoveryStopsWhenNothingRemains(t *testing.T) {
	body := []byte(foreignEncryptedHistoryBody)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, encryptedRejection("rs_foreign")),
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, encryptedRejection("cmp_foreign")),
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, encryptedRejection("msg_other")),
	}}
	c := newOpenAIRejectedFieldTestContext(body)
	_, err := newOpenAIRejectedFieldTestService(upstream).Forward(context.Background(), c, newOpenAIRejectedFieldTestAccount(), body)
	require.Error(t, err)
	require.Len(t, upstream.bodies, 3, "two reductions, then the rejection is final")
	require.Equal(t, http.StatusBadRequest, c.Writer.Status())
}

func bpsRecoveredWire() string {
	return "data: " + `{"type":"response.completed","response":{"id":"resp_bps_recovered","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"recovered"}]}],"usage":{"input_tokens":5,"output_tokens":1}}}` + "\n\n"
}

// BPS returns the same pre-inference rejection for history it cannot read; it
// recovers on the same account exactly like native Responses.
func TestExcelBPSRecoversForeignEncryptedHistoryOnSameAccount(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, rejection := range []string{excelBPSInvalidCiphertext, excelBPSInvalidCiphertextNoCode} {
			t.Run(fmt.Sprintf("stream=%t/code=%t", stream, rejection == excelBPSInvalidCiphertext), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{responses: []*http.Response{
					{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(rejection))},
					bpsCompletionResponse(200, bpsRecoveredWire()),
				}}
				svc := excelBPSGatewayTestService(upstream)
				body := excelBPSEncryptedHistoryRequest(stream)
				original := bytes.Clone(body)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				result, err := svc.Forward(context.Background(), c, excelAccount(), body)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, upstream.requests, 2, "one recovery retry on the same BPS account")
				require.Equal(t, original, body, "the caller's body is never mutated")
				require.Contains(t, string(upstream.bodies[0]), "private-ciphertext")
				require.NotContains(t, string(upstream.bodies[1]), "private-ciphertext")
				require.Contains(t, string(upstream.bodies[1]), "original tool result", "tool history stays")
				require.Contains(t, string(upstream.bodies[1]), "continue original task")
				require.Equal(t, 200, rec.Code)
				require.Contains(t, rec.Body.String(), "recovered")
				require.NotContains(t, rec.Body.String(), "invalid_encrypted_content")
			})
		}
	}
}

// BPS can also report the rejection in-band before any content. Nothing was
// delivered, so the same recovery applies.
func TestExcelBPSRecoversInBandEncryptedRejectionBeforeContent(t *testing.T) {
	failed := "data: " + `{"type":"response.created","response":{"id":"resp_bps_rejected","status":"in_progress","output":[]}}` + "\n\n" +
		"data: " + `{"type":"error","status":400,"error":{"code":"invalid_encrypted_content","type":"invalid_request_error","message":"The encrypted content could not be verified"}}` + "\n\n"
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		bpsCompletionResponse(200, failed),
		bpsCompletionResponse(200, bpsRecoveredWire()),
	}}
	svc := excelBPSGatewayTestService(upstream)
	body := excelBPSEncryptedHistoryRequest(true)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	result, err := svc.Forward(context.Background(), c, excelAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requests, 2)
	require.NotContains(t, string(upstream.bodies[1]), "private-ciphertext")
	require.Equal(t, "resp_bps_recovered", result.ResponseID)
	require.NotContains(t, rec.Body.String(), "resp_bps_rejected", "the rejected attempt's lifecycle events are not delivered")
	require.Contains(t, rec.Body.String(), "recovered")
}

// A compaction BPS cannot read is replaced only when stripping reasoning was
// not enough; a history still rejected after both reductions is the client's 400.
func TestExcelBPSEncryptedHistoryRecoveryReplacesCompactionThenStops(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","stream":false,"input":[{"role":"user","content":"continue"},{"type":"reasoning","encrypted_content":"opaque"},{"type":"compaction","encrypted_content":"context"},{"role":"user","content":"and now"}]}`)
	t.Run("recovered by the notice", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{responses: []*http.Response{
			{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(excelBPSInvalidCiphertext))},
			{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(excelBPSInvalidCiphertext))},
			bpsCompletionResponse(200, bpsRecoveredWire()),
		}}
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		_, err := excelBPSGatewayTestService(upstream).Forward(context.Background(), c, excelAccount(), body)
		require.NoError(t, err)
		require.Len(t, upstream.requests, 3)
		require.Contains(t, string(upstream.bodies[1]), "context", "the compaction survives the first reduction")
		require.NotContains(t, string(upstream.bodies[2]), `"context"`)
		require.Contains(t, string(upstream.bodies[2]), "Earlier conversation summary omitted")
		require.Equal(t, 200, rec.Code)
	})
	t.Run("still rejected", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{responses: []*http.Response{
			{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(excelBPSInvalidCiphertext))},
			{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(excelBPSInvalidCiphertext))},
			{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(excelBPSInvalidCiphertext))},
		}}
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		_, err := excelBPSGatewayTestService(upstream).Forward(context.Background(), c, excelAccount(), body)
		require.Error(t, err)
		require.Len(t, upstream.requests, 3, "each reduction once, then stop")
		require.Equal(t, 400, rec.Code)
		require.Equal(t, "invalid_encrypted_content", gjson.Get(rec.Body.String(), "error.code").String())
	})
}

// A client WS turn bridged to BPS rebuilds the full history and recovers the
// same way: the refused attempt's events never reach the client.
func TestOpenAIWSBPSRecoversEncryptedHistoryRejection(t *testing.T) {
	first := strings.Replace(bpsCompletedStream("resp_good", "confirmed answer"), `"output":[`, `"output":[{"type":"reasoning","id":"rs_good","encrypted_content":"opaque-history","summary":[]},`, 1)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		bpsCompletionResponse(200, first), bpsCompletionResponse(400, excelBPSInvalidCiphertext), bpsCompletionResponse(200, bpsCompletedStream("resp_next", "done")),
	}}
	svc := excelBPSGatewayTestService(upstream)
	svc.cfg = passthroughLifecycleConfig()
	turns := make(chan bpsWSTestTurn, 2)
	client, serverErr := startBPSWSMemorySession(t, svc, excelAccount(), `{"type":"response.create","model":"gpt-6-astra","input":"first context"}`, &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }})
	defer client.CloseNow()
	readBPSWSTestTurn(t, client)
	require.NoError(t, (<-turns).err)
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.create","previous_response_id":"resp_good","input":"next-turn"}`)))
	events := readBPSWSTestTurn(t, client)
	terminal := events[len(events)-1]
	require.Equal(t, "response.completed", gjson.GetBytes(terminal, "type").String(), string(terminal))
	require.Equal(t, "resp_next", gjson.GetBytes(terminal, "response.id").String())
	turn := <-turns
	require.NoError(t, turn.err)
	require.Len(t, upstream.requests, 3, "turn 2 is refused once and recovered on the same account")
	require.Contains(t, string(upstream.bodies[1]), "opaque-history")
	recovered := string(upstream.bodies[2])
	require.NotContains(t, recovered, "opaque-history")
	require.Contains(t, recovered, "first context")
	require.Contains(t, recovered, "confirmed answer")
	require.Contains(t, recovered, "next-turn")
	requireBPSWSTestServerExit(t, client, serverErr)
}

// A conversation compacted on Cloud carries this gateway's summary envelope,
// which native and BPS upstreams cannot read. Recovery installs its summary as
// the message Codex itself installs after compaction, not the omission notice.
func TestEncryptedHistoryRecoveryRestoresCloudCompactionSummary(t *testing.T) {
	envelope := cloudCompactionItem("We fixed the parser; next step is the tests.")
	body := map[string]any{"model": "gpt-5.5", "stream": false, "input": []any{
		envelope,
		map[string]any{"type": "reasoning", "id": "rs_1", "summary": []any{}, "encrypted_content": "gAAAA-cloud-reasoning"},
		map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "continue"}}},
	}}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	retry, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, raw, []byte(encryptedRejection("rs_1")))
	require.NoError(t, err)
	require.True(t, changed, reason)
	require.Equal(t, []string{"message", "message"}, inputTypes(retry), "the envelope becomes its summary message and the unreadable reasoning is dropped")
	require.Equal(t, cloudCompactionText("We fixed the parser; next step is the tests."), gjson.GetBytes(retry, "input.0.content.0.text").String())
	require.Equal(t, "user", gjson.GetBytes(retry, "input.0.role").String())
	require.NotContains(t, string(retry), cloudCompactionEnvelope)
	require.NotContains(t, string(retry), openAIOmittedCompactionNotice, "a readable summary is never replaced by the notice")
}

func TestCloudSummaryReachesNativeAndBPSWithoutRejection(t *testing.T) {
	for _, mode := range []string{"native_json", "native_sse", "native_passthrough", "bps_json", "bps_sse"} {
		t.Run(mode, func(t *testing.T) {
			stream := strings.Contains(mode, "sse") || strings.Contains(mode, "passthrough")
			body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "stream": stream, "input": []any{cloudCompactionItem("retained production history"), map[string]any{"role": "user", "content": "continue"}}})
			require.NoError(t, err)
			upstream := &httpUpstreamRecorder{}
			var svc *OpenAIGatewayService
			var account *Account
			if strings.HasPrefix(mode, "bps") {
				upstream.responses = []*http.Response{bpsCompletionResponse(200, bpsCompletedStream("resp_summary", "done"))}
				svc, account = excelBPSGatewayTestService(upstream), excelAccount()
			} else {
				success := newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"id":"resp_summary","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
				account = newOpenAIRejectedFieldTestAccount()
				if stream {
					success = newOpenAIRejectedFieldTestResponse(http.StatusOK, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_summary\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\ndata: [DONE]\n\n")
					success.Header.Set("Content-Type", "text/event-stream")
					account = newOpenAIOAuthNamespaceTestAccount()
				}
				if mode == "native_passthrough" {
					account = newEncryptedRecoveryPassthroughAccount()
				}
				upstream.responses = []*http.Response{success}
				svc = newOpenAIRejectedFieldTestService(upstream)
			}
			original := bytes.Clone(body)
			c := newOpenAIRejectedFieldTestContext(body)
			result, err := svc.Forward(t.Context(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.bodies, 1, "summary expansion needs no rejected upstream attempt")
			require.Contains(t, string(upstream.bodies[0]), "retained production history")
			require.NotContains(t, string(upstream.bodies[0]), cloudCompactionEnvelope)
			require.Equal(t, original, body)
		})
	}
}
