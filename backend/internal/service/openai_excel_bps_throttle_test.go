package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExcelBPSRateLimitRetryHintBounds(t *testing.T) {
	for _, tc := range []struct{ header, message, want string }{
		{"", "Please try again in 173ms.", "1"},
		{"", "Please try again in 0.1 milliseconds.", "1"},
		{"", "Please try again in 1.1s.", "2"},
		{"", "Please try again in 2.25 minutes.", "135"},
		{"", "Please try again in 999999999999999999999 minutes.", "7200"},
		{"", "Please try again in " + strings.Repeat("9", 400) + "s.", ""},
		{"", "Please try again in -173ms.", ""},
		{"", "Please try again in 0ms.", ""},
		{"", "Please try again in 17months.", ""},
		{"", "Please try again in 17msINVALID.", ""},
		{"", strings.Repeat("a", 4097) + "Please try again in 173ms.", ""},
		{" 30 ", "Please try again in 173ms.", "30"},
		{"bad", "Please try again in 173ms.", "1"},
		{"bad", "no retry hint", ""},
	} {
		t.Run(tc.header+"/"+tc.message, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{"error": map[string]any{"message": tc.message}})
			require.NoError(t, err)
			require.Equal(t, tc.want, excelBPSRateLimitRetryAfter(tc.header, payload))
		})
	}
	date := time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)
	require.Equal(t, date, excelBPSRateLimitRetryAfter(date, []byte(`{"error":{"message":"Please try again in 173ms."}}`)))
	for _, payload := range []string{
		`{"input":"Please try again in 173ms.","error":{"message":"no hint"}}`,
		`{"type":"response.completed","response":{"output":[{"text":"Please try again in 173ms."}]}}`,
		`not JSON Please try again in 173ms.`,
	} {
		require.Empty(t, excelBPSRateLimitRetryAfter("", []byte(payload)))
	}
}

func TestExcelBPSSuccessAndUnrelatedErrorsDoNotCoolDown(t *testing.T) {
	for _, tc := range []struct {
		event      string
		wantStatus int
	}{
		{`{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Rate limit reached for model on tokens per min. Please try again in 173ms."}]}]}}`, http.StatusOK},
		{`{"type":"response.failed","response":{"status":"failed","error":{"code":"context_length_exceeded","message":"Rate limit reached for model on tokens per min. Please try again in 173ms."}}}`, http.StatusBadRequest},
		{`{"type":"error","error":{"code":"server_error","message":"an unrelated failure"}}`, http.StatusInternalServerError},
	} {
		upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, "data: "+tc.event+"\n\n")}
		svc := openAIClientToolsTestService(upstream)
		account := excelAccount()
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		_, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-6-astra","input":"test"}`))
		if tc.wantStatus == http.StatusOK {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
		require.Equal(t, tc.wantStatus, rec.Code)
		_, cooling := svc.excelBPSCooldownUntil.Load(account.ID)
		require.False(t, cooling)
	}
}

func TestExcelBPS429UsesMessageRetryHint(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusTooManyRequests, `{"error":{"code":"rate_limit_exceeded","message":"PRIVATE_UPSTREAM Please try again in 173ms."}}`)}
	svc := openAIClientToolsTestService(upstream)
	account := excelAccount()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-6-astra","input":"test"}`))
	requireExcelBPSRateLimitFailover(t, err, c)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, "1", failover.ResponseHeaders.Get("Retry-After"))
	require.InDelta(t, 1, excelBPSCooldownRemaining(t, svc, account.ID).Seconds(), 0.5)
}

// Production BPS shape: HTTP 200, lifecycle events, then the provider
// organization's TPM rejection before any output.
const bpsOrganizationTPMError = `{"type":"error","sequence_number":2,"error":{"code":"rate_limit_exceeded","type":"tokens","param":null,"headers":{"retry-after":"1","retry-after-ms":"125"},"message":"Rate limit reached for gpt-6-sol in organization org-PRIVATE on tokens per min (TPM): Limit 40000000, Used 40000000, Requested 115720. Please try again in 125ms. Visit https://platform.openai.com/account/rate-limits to learn more."}}`

func bpsStartRateLimitedStream(id, rejection string) string {
	return "event: response.created\ndata: " + `{"type":"response.created","sequence_number":0,"response":{"id":"` + id + `","status":"in_progress","output":[]}}` + "\n\n" +
		"event: response.in_progress\ndata: " + `{"type":"response.in_progress","sequence_number":1,"response":{"id":"` + id + `","status":"in_progress","output":[]}}` + "\n\n" +
		"event: error\ndata: " + rejection + "\n\n"
}

func bpsCompletedStream(id, text string) string {
	return "event: response.created\ndata: " + `{"type":"response.created","sequence_number":0,"response":{"id":"` + id + `","status":"in_progress","output":[]}}` + "\n\n" +
		"event: response.output_text.delta\ndata: " + `{"type":"response.output_text.delta","sequence_number":1,"delta":"` + text + `"}` + "\n\n" +
		"event: response.completed\ndata: " + `{"type":"response.completed","sequence_number":2,"response":{"id":"` + id + `","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` + text + `"}]}],"usage":{"input_tokens":10,"output_tokens":2}}}` + "\n\n"
}

func excelBPSOpsEvents(t *testing.T, c *gin.Context, kind string) []*OpsUpstreamErrorEvent {
	t.Helper()
	raw, _ := c.Get(OpsUpstreamErrorsKey)
	events, _ := raw.([]*OpsUpstreamErrorEvent)
	var matched []*OpsUpstreamErrorEvent
	for _, event := range events {
		if event.Kind == kind {
			matched = append(matched, event)
		}
	}
	return matched
}

func TestExcelBPSStartRateLimitRetriesOnSameAccount(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				bpsCompletionResponse(http.StatusOK, bpsStartRateLimitedStream("resp_throttled", bpsOrganizationTPMError)),
				bpsCompletionResponse(http.StatusOK, bpsCompletedStream("resp_retry", "retried")),
			}}
			svc := openAIClientToolsTestService(upstream)
			svc.cfg.Gateway.LogUpstreamErrorBody = true
			account := excelAccount()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			result, err := svc.Forward(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":"test","stream":%t}`, stream)))
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Len(t, upstream.requests, 2)
			require.Equal(t, upstream.bodies[0], upstream.bodies[1], "the same prepared request is resent")
			require.Equal(t, "resp_retry", result.ResponseID)
			require.Contains(t, rec.Body.String(), "retried")
			require.NotContains(t, rec.Body.String(), "resp_throttled", "the rejected attempt must leave no client-visible event")
			require.NotContains(t, rec.Body.String(), "org-PRIVATE")
			_, cooling := svc.excelBPSCooldownUntil.Load(account.ID)
			require.False(t, cooling, "an organization-wide limit must not cool the account")
			retries := excelBPSOpsEvents(t, c, "rate_limit_retry")
			require.Len(t, retries, 1)
			require.Equal(t, http.StatusTooManyRequests, retries[0].UpstreamStatusCode)
			require.Contains(t, retries[0].Message, "tokens per min")
			require.Equal(t, 1, c.GetInt("excel_bps_upstream_attempt"))
		})
	}
}

func TestExcelBPSStartRateLimitExhaustedReturnsRetryable429(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			var responses []*http.Response
			for i := 0; i <= excelBPSStartRetryMaxAttempts; i++ {
				responses = append(responses, bpsCompletionResponse(http.StatusOK, bpsStartRateLimitedStream(fmt.Sprintf("resp_throttled_%d", i), bpsOrganizationTPMError)))
			}
			upstream := &httpUpstreamRecorder{responses: responses}
			svc := openAIClientToolsTestService(upstream)
			svc.cfg.Gateway.LogUpstreamErrorBody = true
			account := excelAccount()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			_, err := svc.Forward(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":"test","stream":%t}`, stream)))
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.NotErrorAs(t, err, &failover, "every BPS account shares the limit; do not switch accounts")
			require.True(t, IsResponseCommitted(c))
			require.Len(t, upstream.requests, excelBPSStartRetryMaxAttempts+1)
			require.Equal(t, http.StatusTooManyRequests, rec.Code)
			require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
			require.Equal(t, "1", rec.Header().Get("Retry-After"))
			require.Equal(t, "rate_limit_exceeded", gjson.Get(rec.Body.String(), "error.code").String())
			require.Equal(t, "rate_limit_error", gjson.Get(rec.Body.String(), "error.type").String())
			require.Contains(t, gjson.Get(rec.Body.String(), "error.message").String(), "Please try again in 1s.")
			require.NotContains(t, rec.Body.String(), "org-PRIVATE")
			require.NotContains(t, rec.Body.String(), "resp_throttled")
			_, cooling := svc.excelBPSCooldownUntil.Load(account.ID)
			require.False(t, cooling)
			require.Len(t, excelBPSOpsEvents(t, c, "rate_limit_retry"), excelBPSStartRetryMaxAttempts)
			message, _ := c.Get(OpsUpstreamErrorMessageKey)
			require.Contains(t, message, "tokens per min", "Ops keeps the upstream reason")
			status, _ := c.Get(OpsUpstreamStatusCodeKey)
			require.Equal(t, http.StatusTooManyRequests, status)
		})
	}
}

func TestExcelBPSStartRateLimitLongHintIsNotRetried(t *testing.T) {
	rejection := `{"type":"error","error":{"code":"rate_limit_exceeded","headers":{"retry-after-ms":"20000"},"message":"Rate limit reached for gpt-6-sol in organization org-PRIVATE on requests per min (RPM). Please try again in 20s."}}`
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, bpsStartRateLimitedStream("resp_throttled", rejection))}
	svc := openAIClientToolsTestService(upstream)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, err := svc.Forward(context.Background(), c, excelAccount(), []byte(`{"model":"gpt-6-astra","input":"test","stream":true}`))
	require.Error(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "20", rec.Header().Get("Retry-After"))
	require.Contains(t, rec.Body.String(), "Please try again in 20s.")
	require.NotContains(t, rec.Body.String(), "org-PRIVATE")
}

func TestExcelBPSRateLimitAfterOutputIsNotReplayed(t *testing.T) {
	terminals := []string{
		`{"type":"response.failed","response":{"id":"resp_throttled","status":"failed","error":{"code":"rate_limit_exceeded","message":"PRIVATE_UPSTREAM Please try again in 173ms."},"usage":{"input_tokens":12,"output_tokens":2}}}`,
		`{"type":"error","error":{"type":"rate_limit_error","message":"PRIVATE_UPSTREAM Please try again in 173ms."}}`,
		`{"type":"error","code":"rate_limit_exceeded","message":"PRIVATE_UPSTREAM Please try again in 173ms."}`,
		`{"type":"response.failed","response":{"status":"failed","error":{"message":"Rate limit reached for PRIVATE_UPSTREAM on tokens per min (TPM). Please try again in 173ms."}}}`,
	}
	delivered := "event: response.created\ndata: " + `{"type":"response.created","response":{"id":"resp_throttled","status":"in_progress","output":[]}}` + "\n\n" +
		"event: response.output_text.delta\ndata: " + `{"type":"response.output_text.delta","delta":"already delivered"}` + "\n\n"
	for i, terminal := range terminals {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("terminal=%d/stream=%t", i, stream), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, delivered+"data: "+terminal+"\n\n")}
				svc := openAIClientToolsTestService(upstream)
				account := excelAccount()
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				result, err := svc.Forward(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":"test","stream":%t}`, stream)))
				require.Error(t, err)
				var failover *UpstreamFailoverError
				require.NotErrorAs(t, err, &failover, "an accepted stream must not be replayed")
				require.True(t, IsResponseCommitted(c))
				require.NotNil(t, result)
				require.Len(t, upstream.requests, 1)
				require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
				require.Contains(t, rec.Body.String(), `"code":"rate_limit_exceeded"`)
				require.Contains(t, rec.Body.String(), "Please try again in 1s.")
				if !stream {
					require.Equal(t, http.StatusTooManyRequests, rec.Code)
					require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
					require.Equal(t, "1", rec.Header().Get("Retry-After"))
				} else {
					require.Equal(t, http.StatusOK, rec.Code)
					require.Contains(t, rec.Header().Get("Content-Type"), "text/event-stream")
					require.Equal(t, 1, strings.Count(rec.Body.String(), "already delivered"))
					require.Contains(t, rec.Body.String(), "event: response.failed\ndata: ")
					require.NotContains(t, rec.Body.String(), "event: error")
					var failed string
					for _, line := range strings.Split(rec.Body.String(), "\n") {
						if strings.HasPrefix(line, "data: ") && gjson.Get(strings.TrimPrefix(line, "data: "), "type").String() == "response.failed" {
							failed = strings.TrimPrefix(line, "data: ")
						}
					}
					require.Equal(t, "failed", gjson.Get(failed, "response.status").String())
					require.Equal(t, "resp_throttled", gjson.Get(failed, "response.id").String())
					require.Equal(t, "rate_limit_exceeded", gjson.Get(failed, "response.error.code").String())
				}
				require.Equal(t, gjson.Get(terminal, "type").String(), result.UpstreamTerminalEvent)
				if gjson.Get(terminal, "response.usage").Exists() {
					require.EqualValues(t, 12, result.Usage.InputTokens)
					require.EqualValues(t, 2, result.Usage.OutputTokens)
				}
				_, cooling := svc.excelBPSCooldownUntil.Load(account.ID)
				require.False(t, cooling)
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
				require.Nil(t, account.RateLimitResetAt)
				marked, ok := GetOpsStreamError(c)
				require.True(t, ok)
				require.Equal(t, "rate_limit_exceeded", marked.Code)
				require.Equal(t, http.StatusTooManyRequests, marked.IntendedStatus)
				require.True(t, marked.CountTowardsSLA, "a failed HTTP 200 stream must not be recorded as success")
				require.Equal(t, !stream, marked.NonStream)
			})
		}
	}
}

func TestExcelBPSTPMWithMeteredOutputIsNotRetried(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			wire := `{"type":"response.failed","response":{"id":"resp_metered","status":"failed","output":[],"usage":{"input_tokens":12,"output_tokens":3},"error":{"message":"Rate limit reached for PRIVATE on tokens per min (TPM). Please try again in 173ms.","headers":{"retry-after-ms":"2400","authorization":"PRIVATE_TOKEN"}}}}`
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, "data: "+wire+"\n\n")}
			svc := openAIClientToolsTestService(upstream)
			account := excelAccount()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			result, err := svc.Forward(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":"test","stream":%t}`, stream)))
			require.Error(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.requests, 1, "metered generation is never resent")
			require.EqualValues(t, 3, result.Usage.OutputTokens)
			require.Equal(t, http.StatusTooManyRequests, rec.Code)
			require.Equal(t, "3", rec.Header().Get("Retry-After"), "redaction must preserve the validated retry delay")
			require.NotContains(t, rec.Body.String(), "PRIVATE")
			require.Equal(t, http.StatusOK, c.GetInt(OpsUpstreamStatusCodeKey))
			require.False(t, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"))
		})
	}
}

func TestExcelBPSRateLimitDelaySources(t *testing.T) {
	for _, tc := range []struct {
		header, payload string
		want            time.Duration
		ok              bool
	}{
		{"", bpsOrganizationTPMError, 125 * time.Millisecond, true},
		{"", `{"error":{"headers":{"retry-after":"3"},"message":"no hint"}}`, 3 * time.Second, true},
		{"", `{"error":{"message":"Please try again in 173ms."}}`, 173 * time.Millisecond, true},
		{"2", bpsOrganizationTPMError, 2 * time.Second, true},
		{"", `{"error":{"headers":{"retry-after-ms":"-5"},"message":"no hint"}}`, 0, false},
		{"", `{"error":{"message":"no hint"}}`, 0, false},
	} {
		delay, ok := excelBPSRateLimitDelay(tc.header, []byte(tc.payload))
		require.Equal(t, tc.ok, ok, tc.payload)
		require.Equal(t, tc.want, delay, tc.payload)
	}
	require.Equal(t, time.Second, excelBPSClientRateLimitDelay("", []byte(bpsOrganizationTPMError)))
	require.Equal(t, "Upstream BPS rate limit reached. Please try again in 1.5s.", excelBPSClientRateLimitMessage(1500*time.Millisecond))
}

func TestExcelBPSStartPeekReplaysBytesExactly(t *testing.T) {
	wire := ": comment\n" + bpsCompletedStream("resp_peek", "hello")
	peek := startExcelBPSPeek(io.NopCloser(strings.NewReader(wire)))
	require.Nil(t, peek.wait(context.Background(), time.Second))
	got, err := io.ReadAll(&excelBPSPeekBody{peek: peek})
	require.NoError(t, err)
	require.Equal(t, wire, string(got))

	rateLimited := bpsStartRateLimitedStream("resp_peek", bpsOrganizationTPMError)
	peek = startExcelBPSPeek(io.NopCloser(strings.NewReader(rateLimited)))
	require.JSONEq(t, bpsOrganizationTPMError, string(peek.wait(context.Background(), time.Second)))

	partial := "event: response.created\ndata: {\"type\":\"response.created\"}\n\n"
	peek = startExcelBPSPeek(io.NopCloser(strings.NewReader(partial)))
	require.Nil(t, peek.wait(context.Background(), time.Second))
	got, err = io.ReadAll(&excelBPSPeekBody{peek: peek})
	require.NoError(t, err)
	require.Equal(t, partial, string(got))
}

func TestExcelBPSStartPeekWindowExpiryKeepsStream(t *testing.T) {
	reader, writer := io.Pipe()
	peek := startExcelBPSPeek(reader)
	head := "event: response.created\ndata: {\"type\":\"response.created\"}\n\n"
	_, err := writer.Write([]byte(head))
	require.NoError(t, err)
	require.Nil(t, peek.wait(context.Background(), 20*time.Millisecond), "the window expired before any decisive event")
	// A late rejection passes through unchanged; later stages never replay it.
	tail := "event: error\ndata: " + bpsOrganizationTPMError + "\n\n"
	go func() {
		_, _ = writer.Write([]byte(tail))
		_ = writer.Close()
	}()
	body := &excelBPSPeekBody{peek: peek}
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, head+tail, string(got))
	require.NoError(t, body.Close())
}

func TestExcelBPSStartPeekCloseInterruptsPendingRead(t *testing.T) {
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	peek := startExcelBPSPeek(reader)
	body := &excelBPSPeekBody{peek: peek}
	require.Nil(t, peek.wait(context.Background(), 10*time.Millisecond))
	require.NoError(t, body.Close())
	_, err := io.ReadAll(body)
	require.ErrorIs(t, err, io.ErrClosedPipe)
}
