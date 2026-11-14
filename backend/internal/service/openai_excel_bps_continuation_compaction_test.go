package service

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func excelBPSCompactedContinuationFixture(t *testing.T) ([]byte, []byte, []any, []any) {
	t.Helper()
	body := []byte(`{"model":"parent-model","instructions":"PARENT_INSTRUCTIONS","tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"n":{"const":9007199254740993123456789}}}}],"input":[{"role":"user","content":"OLD_UNCOMPACTED_PREFIX"},{"type":"function_call","call_id":"call_tail","name":"lookup","arguments":"{\"n\":9007199254740993123456789}"},{"type":"function_call_output","call_id":"call_tail","output":[{"type":"input_image","image_url":"data:image/png;base64,ORIGINAL+/="},{"type":"input_text","text":"tail result"}],"n":9007199254740993123456789},{"role":"user","content":"new uncompressed tail"}]}`)
	windowRaw := []byte(`[{"type":"message","role":"user","content":"retained in compacted window"},{"type":"compaction","id":"cmp_exact","encrypted_content":"CIPHERTEXT+/=","n":9007199254740993123456789}]`)
	var window []any
	require.NoError(t, decodeJSONUseNumber(windowRaw, &window))
	history, err := basispoints.InspectImageHistory(body)
	require.NoError(t, err)
	response := []byte(`{"id":"resp_compacted","status":"completed","error":null,"output":[{"type":"function_call","id":"fc_new","call_id":"call_new","name":"lookup","arguments":"{\"n\":9007199254740993123456789}"},{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"generated answer"}]}]}`)
	var generated []json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(gjson.GetBytes(response, "output").Raw), &generated))
	var emitted []json.RawMessage
	require.NoError(t, json.Unmarshal(windowRaw, &emitted))
	emitted = append(emitted, generated...)
	output, err := json.Marshal(emitted)
	require.NoError(t, err)
	response, err = sjson.SetRawBytes(response, "output", output)
	require.NoError(t, err)
	return body, response, window, history.Input[1:]
}

func TestExcelBPSCompactedContinuationReconnect(t *testing.T) {
	body, response, window, tail := excelBPSCompactedContinuationFixture(t)
	bodyBefore, responseBefore := bytes.Clone(body), bytes.Clone(response)
	snapshotBody, snapshotResponse, err := excelBPSCompactedContinuationSources(body, response, window, tail)
	require.NoError(t, err)
	require.Equal(t, bodyBefore, body)
	require.Equal(t, responseBefore, response)
	require.NotContains(t, string(snapshotBody), "OLD_UNCOMPACTED_PREFIX")
	require.Len(t, gjson.GetBytes(snapshotBody, "input").Array(), 5)
	require.Len(t, gjson.GetBytes(snapshotResponse, "output").Array(), 2)
	require.Equal(t, "function_call", gjson.GetBytes(snapshotResponse, "output.0.type").String())
	require.Equal(t, gjson.GetBytes(body, "tools").Raw, gjson.GetBytes(snapshotBody, "tools").Raw)
	require.Equal(t, "PARENT_INSTRUCTIONS", gjson.GetBytes(snapshotBody, "instructions").String())

	writer, store := excelBPSContinuationTestService()
	c, account := excelBPSContinuationTestContext(), excelAccount()
	c.Request.Header.Set("session_id", "before-compaction-reconnect")
	finish, err := writer.prepareExcelBPSContinuation(t.Context(), c, account, snapshotBody, snapshotResponse)
	require.NoError(t, err)
	requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationCommitted)
	require.Len(t, store.entries, 1)
	reader := &OpenAIGatewayService{cache: store, cfg: writer.cfg}
	nextContext := excelBPSContinuationTestContext()
	nextContext.Request.Header.Set("session_id", "after-compaction-reconnect")
	next := []byte(`{"model":"next-model","previous_response_id":"resp_compacted","input":[{"type":"function_call_output","call_id":"call_new","output":{"n":9007199254740993123456789}}]}`)
	restored, err := reader.restoreExcelBPSContinuation(t.Context(), nextContext, account, next)
	require.NoError(t, err)
	require.Len(t, gjson.GetBytes(restored, "input").Array(), 8)
	require.NotContains(t, string(restored), "OLD_UNCOMPACTED_PREFIX")
	require.NotContains(t, string(restored), "PARENT_INSTRUCTIONS")
	require.False(t, gjson.GetBytes(restored, "previous_response_id").Exists())
	require.Equal(t, "next-model", gjson.GetBytes(restored, "model").String())
	require.Equal(t, "compaction", gjson.GetBytes(restored, "input.1.type").String())
	require.Equal(t, "CIPHERTEXT+/=", gjson.GetBytes(restored, "input.1.encrypted_content").String())
	require.Equal(t, "9007199254740993123456789", gjson.GetBytes(restored, "input.1.n").Raw)
	require.Equal(t, `{"n":9007199254740993123456789}`, gjson.GetBytes(restored, "input.2.arguments").String())
	require.Equal(t, "data:image/png;base64,ORIGINAL+/=", gjson.GetBytes(restored, "input.3.output.0.image_url").String())
	require.Equal(t, "9007199254740993123456789", gjson.GetBytes(restored, "input.3.n").Raw)
	require.Equal(t, `{"n":9007199254740993123456789}`, gjson.GetBytes(restored, "input.5.arguments").String())
	require.Equal(t, "generated answer", gjson.GetBytes(restored, "input.6.content.0.text").String())
	require.Equal(t, "9007199254740993123456789", gjson.GetBytes(restored, "input.7.output.n").Raw)
	require.Equal(t, "9007199254740993123456789", gjson.GetBytes(restored, "tools.0.parameters.properties.n.const").Raw)
}

func TestExcelBPSCompactedContinuationRejectsUnprovenWindow(t *testing.T) {
	body, response, window, tail := excelBPSCompactedContinuationFixture(t)
	for _, output := range []string{`[]`, `null`, `{}`, `[{"type":"message","content":"different prefix"}]`, `[{"type":"message","role":"user","content":"retained in compacted window"},{"type":"compaction","id":"cmp_exact","encrypted_content":"OTHER_CIPHERTEXT","n":9007199254740993123456789}]`} {
		t.Run(output, func(t *testing.T) {
			changed, err := sjson.SetRawBytes(response, "output", []byte(output))
			require.NoError(t, err)
			_, _, err = excelBPSCompactedContinuationSources(body, changed, window, tail)
			require.ErrorIs(t, err, errExcelBPSContinuationInvalid)
		})
	}
	_, _, err := excelBPSCompactedContinuationSources(body, response, window, nil)
	require.ErrorIs(t, err, errExcelBPSContinuationInvalid)
	unchangedBody, unchangedResponse, err := excelBPSCompactedContinuationSources(body, response, nil, nil)
	require.NoError(t, err)
	require.Equal(t, body, unchangedBody)
	require.Equal(t, response, unchangedResponse)
}

func TestExcelBPSCompactedContinuationKeepsOpaqueContextGuard(t *testing.T) {
	for _, field := range []string{"prompt", "conversation", "previous_response_id"} {
		t.Run(field, func(t *testing.T) {
			body, response, window, tail := excelBPSCompactedContinuationFixture(t)
			body, err := sjson.SetBytes(body, field, "unresolved-context")
			require.NoError(t, err)
			snapshotBody, snapshotResponse, err := excelBPSCompactedContinuationSources(body, response, window, tail)
			require.NoError(t, err)
			require.Equal(t, "unresolved-context", gjson.GetBytes(snapshotBody, field).String())
			svc, store := excelBPSContinuationTestService()
			finish, err := svc.prepareExcelBPSContinuation(t.Context(), excelBPSContinuationTestContext(), excelAccount(), snapshotBody, snapshotResponse)
			require.NoError(t, err)
			requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationNotApplicable)
			require.Zero(t, store.saves)
		})
	}
}
