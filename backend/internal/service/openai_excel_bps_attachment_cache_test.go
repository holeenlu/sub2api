package service

import (
	"bytes"
	"context"
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

func excelBPSCachedOwnershipService(t *testing.T, rejectCached func(stream bool) *http.Response) (*OpenAIGatewayService, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	svc := excelBPSGatewayTestService(nil)
	enableNativeAttachments(svc)
	var uploads, generations atomic.Int64
	svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if req.URL.String() == basispoints.AttachmentsURL {
			n := uploads.Add(1)
			id := "file-own" + string(rune('a'+n))
			return bpsCompletionResponse(200, `{"openai_file_id":"`+id+`"}`), nil
		}
		generations.Add(1)
		raw, err := io.ReadAll(req.Body)
		require.NoError(t, err)
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
		name   string
		stream bool
		reject func(stream bool) *http.Response
	}{
		{name: "HTTP status", reject: func(bool) *http.Response { return bpsCompletionResponse(500, excelBPSOwnershipEnvelope) }},
		{name: "in-stream error", stream: true, reject: func(bool) *http.Response {
			return bpsCompletionResponse(200, "event: error\ndata: {\"type\":\"error\",\"status\":500,\"error\":{\"type\":\"server_error\",\"message\":\"Unknown error while validating file ownership.\"}}\n\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, uploads, generations := excelBPSCachedOwnershipService(t, tc.reject)
			body, _ := nativeGatewayBody(t)
			if tc.stream {
				body, _ = sjson.SetBytes(body, "stream", true)
			}
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
			require.EqualValues(t, "basispoints_attachment_ownership", failover.Reason)
			require.Equal(t, GatewayFailureScopeRequest, failover.Scope, "a stale cached ID is not an account failure")
			require.NotContains(t, rec.Body.String(), "file ownership", "nothing is delivered before the retry")
			require.Zero(t, svc.excelBPSAttachmentCache.Len(), "the stale cached IDs are invalidated")

			// The handler's same-account retry uploads the original again.
			_, err = sendExcelBPSCachedTurn(t, svc, body, session)
			require.NoError(t, err)
			require.Equal(t, int64(3), uploads.Load())
			require.Equal(t, int64(3), generations.Load())
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
