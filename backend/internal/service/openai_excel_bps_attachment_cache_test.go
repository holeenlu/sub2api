package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const excelBPSOwnershipEnvelope = `{"error":{"message":"Unknown error while validating file ownership.","type":"server_error"}}`

func excelBPSCachedOwnershipService(t *testing.T, rejectCached func(stream bool) *http.Response, inspect ...func([]byte)) (*OpenAIGatewayService, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	svc := excelBPSGatewayTestService(nil)
	enableNativeAttachments(svc)
	var uploads, generations atomic.Int64
	svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
		require.Equal(t, excelAccount().ID, accountID, "attachment recovery must stay on the same account")
		require.Equal(t, "test-account", req.Header.Get("Chatgpt-Account-Id"))
		if req.URL.String() == basispoints.AttachmentsURL {
			n := uploads.Add(1)
			id := "file-own" + string(rune('a'+n))
			return bpsCompletionResponse(200, `{"openai_file_id":"`+id+`"}`), nil
		}
		generations.Add(1)
		raw, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		for _, check := range inspect {
			check(raw)
		}
		id := ""
		for _, item := range gjson.GetBytes(raw, "input").Array() {
			for _, part := range item.Get("content").Array() {
				if value := part.Get("file_id").String(); value != "" {
					id = value
				}
			}
		}
		require.NotEmpty(t, id)
		// Upstream forgot every ID uploaded before the latest upload.
		if id != "file-own"+string(rune('a'+uploads.Load())) {
			return rejectCached(gjson.GetBytes(raw, "stream").Bool()), nil
		}
		return bpsCompletionResponse(200, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_own\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"), nil
	}}
	return svc, &uploads, &generations
}

func sendExcelBPSCachedTurn(t *testing.T, svc *OpenAIGatewayService, body []byte, session string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	if session != "" {
		c.Request.Header.Set("session_id", session)
	}
	_, err := svc.Forward(context.Background(), c, excelAccount(), body)
	return rec, err
}

func TestExcelBPSStaleCachedAttachmentRetriesOnceWithFreshUpload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		stream  bool
		history bool
		reject  func(stream bool) *http.Response
	}{
		{name: "HTTP status", reject: func(bool) *http.Response { return bpsCompletionResponse(500, excelBPSOwnershipEnvelope) }},
		{name: "in-stream error", stream: true, reject: func(bool) *http.Response {
			return bpsCompletionResponse(200, "event: error\ndata: {\"type\":\"error\",\"status\":500,\"error\":{\"type\":\"server_error\",\"message\":\"Unknown error while validating file ownership.\"}}\n\n")
		}},
		{name: "HTTP status with reasoning and tool screenshots", history: true, reject: func(bool) *http.Response {
			return bpsCompletionResponse(500, excelBPSOwnershipEnvelope)
		}},
		{name: "in-stream error with reasoning and tool screenshots", stream: true, history: true, reject: func(bool) *http.Response {
			return bpsCompletionResponse(200, "event: error\ndata: {\"type\":\"error\",\"status\":500,\"error\":{\"type\":\"server_error\",\"message\":\"Unknown error while validating file ownership.\"}}\n\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := nativeGatewayBody(t)
			if tc.history {
				// Reproduce the observed counts without claiming these fixture bytes
				// are the production request: one managed image, two tool screenshots
				// and 95 reasoning ciphertext items must survive bounded recovery.
				imageURL := gjson.GetBytes(body, "input.0.content.0.image_url").String()
				image := map[string]any{"type": "input_image", "image_url": imageURL, "detail": "original"}
				input := []any{map[string]any{"role": "user", "content": []any{image}}}
				for i := 0; i < 95; i++ {
					input = append(input, map[string]any{"type": "reasoning", "summary": []any{}, "encrypted_content": fmt.Sprintf("PRIVATE_REASONING_%d", i)})
				}
				input = append(input,
					map[string]any{"type": "function_call", "call_id": "call_image", "name": "view_image", "arguments": "{}"},
					map[string]any{"type": "function_call_output", "call_id": "call_image", "output": []any{image, image}},
				)
				var err error
				body, err = sjson.SetBytes(body, "input", input)
				require.NoError(t, err)
			}
			if tc.stream {
				body, _ = sjson.SetBytes(body, "stream", true)
			}
			original := bytes.Clone(body)
			var forwarded [][]byte
			svc, uploads, generations := excelBPSCachedOwnershipService(t, tc.reject, func(wire []byte) {
				forwarded = append(forwarded, bytes.Clone(wire))
				if !tc.history {
					return
				}
				counts := (*basispoints.NativeImages)(nil).FileReferences(wire)
				require.Equal(t, basispoints.FileReferenceCounts{Unmanaged: 1, Inline: 2, EncryptedReasoningItems: 95}, counts, "without the upload plan only the managed ID's provenance is unavailable")
				require.JSONEq(t, gjson.GetBytes(original, "input.#(type==reasoning)#").Raw, gjson.GetBytes(wire, "input.#(type==reasoning)#").Raw, "reasoning ciphertext must remain unchanged")
				require.JSONEq(t, gjson.GetBytes(original, "input.#(type==function_call_output).output").Raw, gjson.GetBytes(wire, "input.#(type==function_call_output).output").Raw, "tool screenshots must remain inline and unchanged")
			})
			session := t.Name()

			_, err := sendExcelBPSCachedTurn(t, svc, body, session)
			require.NoError(t, err)
			require.Equal(t, int64(1), uploads.Load())
			// Make the cached ID stale upstream.
			uploads.Add(1)

			rec, err := sendExcelBPSCachedTurn(t, svc, body, session)
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.True(t, failover.RetryableOnSameAccount)
			require.Equal(t, 1, failover.SameAccountRetryMax)
			require.False(t, failover.ShouldRetryNextAccount())
			require.False(t, failover.ShouldReportAccountScheduleFailure())
			require.False(t, failover.RequestNotSent, "the ownership error does not prove the request was unsent")
			require.False(t, failover.RejectedBeforeGeneration, "the ownership error does not prove generation never started")
			require.EqualValues(t, "basispoints_attachment_ownership", failover.Reason)
			require.Equal(t, GatewayFailureScopeRequest, failover.Scope, "a stale cached ID is not an account failure")
			require.NotContains(t, rec.Body.String(), "file ownership", "nothing is delivered before the retry")
			require.Zero(t, svc.excelBPSAttachmentCache.Len(), "the stale cached IDs are invalidated")
			require.Equal(t, int64(2), uploads.Load(), "the failed request reused its cached attachment")

			// The handler's same-account retry uploads the original again.
			_, err = sendExcelBPSCachedTurn(t, svc, body, session)
			require.NoError(t, err)
			require.Equal(t, int64(3), uploads.Load())
			require.Equal(t, int64(3), generations.Load())
			require.Len(t, forwarded, 3)
			require.Contains(t, string(forwarded[0]), `"file_id":"file-ownb"`)
			require.Contains(t, string(forwarded[1]), `"file_id":"file-ownb"`)
			require.Contains(t, string(forwarded[2]), `"file_id":"file-ownd"`)
			require.NotContains(t, string(forwarded[2]), "file-ownb")
			require.Equal(t, original, body, "ownership recovery must not mutate the original request")
		})
	}
}

func TestExcelBPSFreshOwnershipFailureRequestsOneOwnerRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := excelBPSGatewayTestService(nil)
	enableNativeAttachments(svc)
	svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if req.URL.String() == basispoints.AttachmentsURL {
			return bpsCompletionResponse(200, `{"openai_file_id":"file-only"}`), nil
		}
		return bpsCompletionResponse(500, excelBPSOwnershipEnvelope), nil
	}}
	body, _ := nativeGatewayBody(t)
	rec, err := sendExcelBPSCachedTurn(t, svc, body, t.Name())
	require.Error(t, err)
	var failover *UpstreamFailoverError
	require.True(t, errorAsUpstreamFailover(err, &failover) && failover.RetryableOnSameAccount)
	require.Equal(t, 1, failover.SameAccountRetryMax)
	require.False(t, failover.ShouldRetryNextAccount(), "file ownership recovery stays on the owner")
	require.False(t, failover.ShouldReportAccountScheduleFailure())
	require.Empty(t, rec.Body.String(), "no error is committed before bounded recovery")
	require.Zero(t, svc.excelBPSAttachmentCache.Len(), "IDs of a failed ownership request are not kept")
}

func TestExcelBPSTransientRequestsDoNotUseAttachmentCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := excelBPSGatewayTestService(nil)
	enableNativeAttachments(svc)
	var uploads atomic.Int64
	svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if req.URL.String() == basispoints.AttachmentsURL {
			uploads.Add(1)
			return bpsCompletionResponse(200, `{"openai_file_id":"file-transient"}`), nil
		}
		return bpsCompletionResponse(200, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_t\",\"status\":\"completed\",\"output\":[]}}\n\n"), nil
	}}
	body, _ := nativeGatewayBody(t)
	for i := 0; i < 2; i++ {
		_, err := sendExcelBPSCachedTurn(t, svc, body, "")
		require.NoError(t, err)
	}
	require.Equal(t, int64(2), uploads.Load(), "requests without a trusted conversation identity upload afresh")
	require.Zero(t, svc.excelBPSAttachmentCache.Len())
}

func errorAsUpstreamFailover(err error, target **UpstreamFailoverError) bool {
	failover, ok := err.(*UpstreamFailoverError)
	if ok {
		*target = failover
	}
	return ok
}
