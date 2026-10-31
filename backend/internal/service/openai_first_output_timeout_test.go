package service

import (
	"bytes"
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type blockingOpenAIResponseHeaderUpstream struct {
	canceled chan struct{}
	once     sync.Once
}

func TestOpenAITurnBudgetSharedAcrossRetries(t *testing.T) {
	t.Run("explicit refusal and committed output stop all retry routes", func(t *testing.T) {
		refusal := []byte(`{"error":{"code":"content_policy_violation"}}`)
		require.False(t, OpenAITurnRetryAllowed(t.Context(), 1, refusal), "refusal stops even without a budget")
		err := &UpstreamFailoverError{ResponseBody: refusal}
		require.False(t, err.ShouldReportAccountScheduleFailure())
		ctx := EnsureOpenAITurnBudget(t.Context(), nil)
		require.True(t, OpenAITurnRetryAllowed(ctx, 1, nil))
		MarkTurnOutputCommitted(ctx)
		require.False(t, OpenAITurnRetryAllowed(ctx, 1, nil))
		fresh := WithOpenAITurnBudget(ctx, NewOpenAITurnBudget(nil))
		require.True(t, OpenAITurnRetryAllowed(fresh, 1, nil))
	})
	for _, tc := range []struct{ limit, switches, want int }{{0, 10, 8}, {3, 10, 3}, {3, 1, 3}} {
		cfg := &config.Config{}
		cfg.Gateway.OpenAITurnMaxAttempts = tc.limit
		cfg.Gateway.MaxAccountSwitches = tc.switches
		ctx := EnsureOpenAITurnBudget(context.Background(), cfg)
		for i := 0; i < tc.want; i++ {
			require.NoError(t, TakeOpenAITurnAttempt(EnsureOpenAITurnBudget(ctx, cfg)))
		}
		if tc.limit > 0 {
			require.ErrorIs(t, TakeOpenAITurnAttempt(ctx), ErrOpenAITurnAttemptBudgetExhausted)
		}
		// New logical requests do not inherit the exhausted budget.
		require.NoError(t, TakeOpenAITurnAttempt(WithOpenAITurnBudget(ctx, NewOpenAITurnBudget(cfg))))
	}
	t.Run("preparation and generation have independent limits", func(t *testing.T) {
		ctx := WithOpenAITurnBudget(t.Context(), &OpenAITurnBudget{attemptBudget: newOpenAITurnAttemptBudget(2)})
		require.NoError(t, TakeTurnPreparationRetry(ctx))
		require.NoError(t, TakeTurnPreparationRetry(ctx))
		require.ErrorIs(t, TakeTurnPreparationRetry(ctx), ErrTurnPreparationExhausted)
		require.NoError(t, TakeOpenAITurnAttempt(ctx))
		require.NoError(t, TakeOpenAITurnAttempt(ctx))
		require.ErrorIs(t, TakeOpenAITurnAttempt(ctx), ErrOpenAITurnAttemptBudgetExhausted)
	})
	t.Run("independent WS operation starts without the previous BPS selection", func(t *testing.T) {
		old := NewOpenAITurnBudget(nil)
		ctx := WithOpenAITurnBudget(t.Context(), old)
		pinExcelBPSExecution(ctx)
		next := NewOpenAITurnBudget(nil)
		nextCtx := WithOpenAITurnBudget(ctx, next)
		require.True(t, excelBPSExecutionPinned(ctx), "the original operation keeps its selected mode")
		require.False(t, excelBPSExecutionPinned(nextCtx))
		require.Zero(t, next.attemptBudget.Snapshot().Attempts)
	})
	protection := []byte(`{"error":{"message":"response protection is unavailable"}}`)
	for _, limit := range []int{0, 3} {
		ctx := EnsureOpenAITurnBudget(context.Background(), &config.Config{Gateway: config.GatewayConfig{OpenAITurnMaxAttempts: limit}})
		require.True(t, OpenAITurnRetryAllowed(ctx, 1, protection))
		require.True(t, OpenAITurnRetryAllowed(ctx, 1, protection))
		require.False(t, OpenAITurnRetryAllowed(ctx, 2, protection))
	}
	parent, cancel := context.WithCancel(context.Background())
	bound := EnsureOpenAITurnBudget(parent, nil)
	cancel()
	require.ErrorIs(t, TakeOpenAITurnAttempt(context.WithoutCancel(bound)), context.Canceled, "detached usage drain cannot authorize a new generation after client cancellation")
}

type firstOutputCloseTrackingBody struct {
	io.ReadCloser
	closed chan struct{}
	once   sync.Once
}

func (b *firstOutputCloseTrackingBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return b.ReadCloser.Close()
}

func (u *blockingOpenAIResponseHeaderUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	select {
	case <-req.Context().Done():
		u.once.Do(func() { close(u.canceled) })
		return nil, req.Context().Err()
	case <-time.After(1500 * time.Millisecond):
		return nil, errors.New("test upstream was not canceled before response headers")
	}
}

func (u *blockingOpenAIResponseHeaderUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, "", 0, 0)
}

func TestOpenAIForwardFirstOutputTimeoutIncludesResponseHeaderWait(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &blockingOpenAIResponseHeaderUpstream{canceled: make(chan struct{})}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			OpenAIFirstOutputTimeoutSeconds: 1,
			MaxLineSize:                     defaultMaxLineSize,
		}},
		httpUpstream: upstream,
	}
	body := []byte(`{"model":"gpt-5.5","stream":true,"reasoning":{"effort":"low"},"input":"hello"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	account := &Account{
		ID: 1, Name: "oauth-test", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token", "chatgpt_account_id": "test-account"},
	}

	started := time.Now()
	_, err := svc.Forward(context.Background(), c, account, body)

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), "first_output_timeout")
	require.True(t, failoverErr.SafeToFailoverAfterWrite)
	require.Less(t, time.Since(started), 1300*time.Millisecond)
	require.Empty(t, rec.Body.String())
	select {
	case <-upstream.canceled:
	default:
		t.Fatal("response-header timeout did not cancel the upstream request context")
	}
}

func TestOpenAINativeFirstOutputTimeoutDisabledPreservesSynchronousStream(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		OpenAIFirstOutputTimeoutSeconds: 0,
		MaxLineSize:                     defaultMaxLineSize,
	}}}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_disabled"}}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_disabled","usage":{"input_tokens":1,"output_tokens":1}}}`,
		"",
		"",
	}, "\n")))}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	result, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, rec.Body.String(), "response.completed")
}

func TestOpenAINativeFirstOutputTimeoutIgnoresPreambleAndCleansReader(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		OpenAIFirstOutputTimeoutSeconds: 1,
		MaxLineSize:                     defaultMaxLineSize,
	}}}
	pr, pw := io.Pipe()
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_slow\"}}\n\n"))
		_, _ = pw.Write([]byte("data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"resp_slow\"}}\n\n"))
		time.Sleep(200 * time.Millisecond)
	}()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := &firstOutputCloseTrackingBody{ReadCloser: pr, closed: make(chan struct{})}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now().Add(-2*time.Second), "model", "model")

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), "first_output_timeout")
	require.True(t, failoverErr.SafeToFailoverAfterWrite)
	require.Empty(t, rec.Body.String())
	select {
	case <-body.closed:
	default:
		t.Fatal("first-output timeout did not close the upstream response body")
	}
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("stream reader/writer goroutine did not exit after first-output timeout")
	}
}

func TestNewOpenAIFirstOutputTimeoutErrorRecordsCallerProxyAttribution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	svc := &OpenAIGatewayService{}
	proxyID := int64(10060)
	account := &Account{ID: 1, Name: "acc", Platform: PlatformOpenAI, ProxyID: &proxyID, Proxy: &Proxy{ID: proxyID, Name: "ws-proxy"}}

	// WebSocket callers pass the WS attribution; HTTP callers pass the HTTP one.
	wsID, wsName := opsUpstreamWSProxyAttribution(&Account{ID: 2, Platform: PlatformOpenAI})
	_ = svc.newOpenAIFirstOutputTimeoutError(context.Background(), c, &Account{ID: 2, Name: "ws", Platform: PlatformOpenAI},
		wsID, wsName, time.Now(), "gpt-5.5", "", time.Second, "websocket_first_semantic_output", nil)
	_ = svc.newOpenAIFirstOutputTimeoutError(context.Background(), c, account,
		opsUpstreamProxyID(account), opsUpstreamProxyName(account), time.Now(), "gpt-5.5", "", time.Second, "semantic_output", nil)

	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := raw.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 2)
	require.Equal(t, "first_output_timeout", events[0].Kind)
	require.Nil(t, events[0].ProxyID)
	require.Equal(t, opsProxyNameUnknown, events[0].ProxyName)
	require.NotNil(t, events[1].ProxyID)
	require.Equal(t, proxyID, *events[1].ProxyID)
	require.Equal(t, "ws-proxy", events[1].ProxyName)
}

func TestOpenAIFirstOutputTimeoutForReasoningEffort(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		OpenAIFirstOutputTimeoutSeconds:           120,
		OpenAIHighEffortFirstOutputTimeoutSeconds: 300,
	}}}

	require.Equal(t, 120*time.Second, svc.openAIFirstOutputTimeout("low"))
	require.Equal(t, 300*time.Second, svc.openAIFirstOutputTimeout("high"))
	require.Equal(t, 300*time.Second, svc.openAIFirstOutputTimeout("xhigh"))
	require.Equal(t, 300*time.Second, svc.openAIFirstOutputTimeout("max"))
	for _, effort := range []string{"ultra", "x-high", "extra-high", "extra_high", " ULTRA "} {
		require.Equal(t, 300*time.Second, svc.openAIFirstOutputTimeout(effort), effort)
		ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg, effort)
		budget := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
		require.Equal(t, 300*time.Second, budget.firstOutput.Snapshot().Deadline.Sub(budget.firstOutput.Snapshot().Started))
		require.NoError(t, TakeOpenAITurnAttempt(ctx))
		deadline := budget.firstOutput.Snapshot().Deadline
		ctx = EnsureOpenAITurnBudget(ctx, svc.cfg, "high")
		require.Equal(t, deadline, ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget).firstOutput.Snapshot().Deadline, "retries never reset the deadline")
	}
}

func TestOpenAIFirstOutputStageDefaultLimitIsIndependentFromScannerLimit(t *testing.T) {
	stage := newDefaultOpenAIFirstOutputStage()
	defer func() { require.NoError(t, stage.Close()) }()

	require.EqualValues(t, 32*1024*1024, stage.limit)
	require.Greater(t, stage.limit, int64(68106))
	require.Less(t, stage.limit, int64(defaultMaxLineSize))
}

func TestOpenAIFirstOutputDynamicScannerLimitsOnlyWhileGuardIsActive(t *testing.T) {
	var guardActive atomic.Bool
	guardActive.Store(true)
	split := openAIFirstOutputDynamicScanLines(&guardActive)
	guardLimit := openAIFirstOutputStageMaxBytes + openAIFirstOutputScannerFramingAllowance
	undelimited := bytes.Repeat([]byte("x"), guardLimit)

	_, _, err := split(undelimited, false)
	require.ErrorIs(t, err, errOpenAIFirstOutputScannerLimit)

	guardActive.Store(false)
	advance, token, err := split(undelimited, false)
	require.NoError(t, err)
	require.Zero(t, advance)
	require.Nil(t, token)
}

func TestOpenAIFirstOutputStageOverflowIsAtomicAndCleanupRemovesSpool(t *testing.T) {
	stage := newOpenAIFirstOutputStage(70 * 1024)
	payload := bytes.Repeat([]byte("x"), 68*1024)
	n, err := stage.Write(payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	if runtime.GOOS == "windows" {
		require.Nil(t, stage.tempFile)
		require.Empty(t, stage.tempPath)
	} else {
		require.NotNil(t, stage.tempFile)
		require.NotEmpty(t, stage.tempPath)
		_, err = os.Stat(stage.tempPath)
		require.ErrorIs(t, err, os.ErrNotExist)
		stat, statErr := stage.tempFile.Stat()
		require.NoError(t, statErr)
		require.Equal(t, os.FileMode(0o600), stat.Mode().Perm())
	}

	n, err = stage.Write(bytes.Repeat([]byte("y"), 3*1024))
	require.Zero(t, n)
	require.ErrorIs(t, err, errOpenAIFirstOutputStageLimit)
	require.EqualValues(t, len(payload), stage.Buffered())
	path := stage.tempPath
	require.NoError(t, stage.Close())
	require.True(t, stage.closed)
	require.Nil(t, stage.tempFile)
	require.Empty(t, stage.tempPath)
	if path != "" {
		_, err = os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestOpenAIFirstOutputStageCommitCopiesSpoolAndRemovesTemp(t *testing.T) {
	stage := newOpenAIFirstOutputStage(80 * 1024)
	payload := bytes.Repeat([]byte("z"), 68*1024)
	_, err := stage.Write(payload)
	require.NoError(t, err)
	path := stage.tempPath
	if runtime.GOOS == "windows" {
		require.Empty(t, path)
		require.Nil(t, stage.tempFile)
	} else {
		require.NotEmpty(t, path)
		require.NotNil(t, stage.tempFile)
		_, statErr := os.Stat(path)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	}

	var downstream bytes.Buffer
	require.NoError(t, stage.CommitTo(&downstream))
	require.Equal(t, payload, downstream.Bytes())
	require.Zero(t, stage.Buffered())
	if path != "" {
		_, err = os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	require.NoError(t, stage.Close())
}

func TestOpenAIFirstOutputStageUnlinkFailurePermanentlyFallsBackToMemoryAndRetriesCleanup(t *testing.T) {
	stage := newDefaultOpenAIFirstOutputStage()
	stage.memoryOnly = false
	t.Cleanup(func() {
		stage.removeFile = os.Remove
		_ = stage.Close()
	})
	createCalls := 0
	stage.createTemp = func() (*os.File, error) {
		createCalls++
		return os.CreateTemp("", "sub2api-openai-first-output-fallback-*")
	}
	removeCalls := 0
	stage.removeFile = func(path string) error {
		removeCalls++
		if removeCalls <= 2 {
			return errors.New("forced remove failure")
		}
		return os.Remove(path)
	}

	payload := bytes.Repeat([]byte("m"), 68*1024)
	_, err := stage.Write(payload)
	require.NoError(t, err)
	require.True(t, stage.memoryOnly)
	require.Nil(t, stage.tempFile)
	require.NotEmpty(t, stage.tempPath)
	require.Equal(t, 1, createCalls)
	stat, statErr := os.Stat(stage.tempPath)
	require.NoError(t, statErr)
	require.Zero(t, stat.Size(), "failed-unlink fallback must never write plaintext to the named file")

	_, err = stage.WriteString("more")
	require.NoError(t, err)
	require.Equal(t, 1, createCalls, "memory-only fallback must not retry CreateTemp")
	path := stage.tempPath
	cleanupErr := stage.Close()
	require.ErrorContains(t, cleanupErr, "forced remove failure")
	require.Empty(t, stage.tempPath)
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, stage.Close())
}

func TestOpenAINativeFirstOutputTimeoutDisarmsAfterSemanticOutput(t *testing.T) {
	cfg := &config.Config{Gateway: config.GatewayConfig{
		OpenAIFirstOutputTimeoutSeconds: 1,
		MaxLineSize:                     defaultMaxLineSize,
	}}
	svc := &OpenAIGatewayService{cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg)}
	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_ok\"}}\n\n"))
		_, _ = pw.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"))
		time.Sleep(1100 * time.Millisecond)
		_, _ = pw.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"))
	}()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{
		"X-Request-Id":                   []string{"request-winning"},
		"X-Ratelimit-Remaining-Requests": []string{"42"},
	}, Body: pr}

	result, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.firstTokenMs)
	require.Contains(t, rec.Body.String(), "response.output_text.delta")
	require.Contains(t, rec.Body.String(), "response.completed")
	require.Equal(t, "request-winning", rec.Result().Header.Get("X-Request-Id"))
	require.Equal(t, "42", rec.Result().Header.Get("X-Ratelimit-Remaining-Requests"))
}

func TestOpenAINativeFirstOutputTimeoutWaitsForCompleteSemanticEvent(t *testing.T) {
	const lineSize = 68106
	prefix := `data: {"type":"response.output_text.delta","delta":"`
	suffix := `"}`
	line := prefix + strings.Repeat("x", lineSize-len(prefix)-len(suffix)) + suffix
	require.Len(t, line, lineSize)
	assertOpenAINativeLargeOpenEventTimesOutWithoutLeak(t, line)
}

func TestOpenAINativeFirstOutputTimeoutDoesNotLeakLargePreambleEvent(t *testing.T) {
	const lineSize = 68106
	prefix := `data: {"type":"response.created","response":{"id":"resp_partial","padding":"`
	suffix := `"}}`
	line := prefix + strings.Repeat("x", lineSize-len(prefix)-len(suffix)) + suffix
	require.Len(t, line, lineSize)
	assertOpenAINativeLargeOpenEventTimesOutWithoutLeak(t, line)
}

func assertOpenAINativeLargeOpenEventTimesOutWithoutLeak(t *testing.T, line string) {
	t.Helper()
	cfg := &config.Config{Gateway: config.GatewayConfig{
		OpenAIFirstOutputTimeoutSeconds: 1,
		StreamKeepaliveInterval:         1,
		MaxLineSize:                     defaultMaxLineSize,
	}}
	svc := &OpenAIGatewayService{cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg)}
	pr, pw := io.Pipe()
	body := &firstOutputCloseTrackingBody{ReadCloser: pr, closed: make(chan struct{})}
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte(line + "\n"))
		select {
		case <-body.closed:
		case <-time.After(2 * time.Second):
		}
	}()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{
		"X-Request-Id":                   []string{"request-partial"},
		"X-Ratelimit-Remaining-Requests": []string{"1"},
	}, Body: body}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), "first_output_timeout")
	require.True(t, failoverErr.SafeToFailoverAfterWrite)
	require.NotContains(t, rec.Body.String(), "data:", "attempt JSON must remain private before the SSE boundary")
	require.NotContains(t, rec.Body.String(), `"type"`, "attempt JSON must remain private before the SSE boundary")
	for _, outputLine := range strings.Split(strings.TrimSpace(rec.Body.String()), "\n") {
		if outputLine != "" {
			require.True(t, strings.HasPrefix(outputLine, ":"), "only keepalive comments may precede failover: %q", outputLine)
		}
	}
	require.Empty(t, rec.Header().Values("X-Request-Id"))
	require.Empty(t, rec.Header().Values("X-Ratelimit-Remaining-Requests"))
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("partial-event writer did not exit after timeout closed the body")
	}
}

func TestOpenAINativeFirstOutputEOFRejectsTerminalEventWithoutBlankLine(t *testing.T) {
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{
		openAITTFTMode: OpenAITTFTModeVisible,
		expiresAt:      time.Now().Add(time.Minute).UnixNano(),
	})
	t.Cleanup(func() {
		gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{
			openAITTFTMode: OpenAITTFTModeSemantic,
			expiresAt:      time.Now().Add(time.Minute).UnixNano(),
		})
	})
	cfg := &config.Config{Gateway: config.GatewayConfig{
		OpenAIFirstOutputTimeoutSeconds: 1,
		MaxLineSize:                     defaultMaxLineSize,
	}}
	svc := &OpenAIGatewayService{cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg)}
	payload := `data: {"type":"response.completed","response":{"id":"resp_eof","usage":{"input_tokens":3,"output_tokens":2}}}`
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Request-Id":                   []string{"request-eof"},
			"X-Ratelimit-Remaining-Requests": []string{"17"},
		},
		Body: io.NopCloser(strings.NewReader(payload)),
	}

	result, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")

	require.Error(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.firstTokenMs)
	require.Empty(t, result.responseID)
	require.Zero(t, result.usage.InputTokens)
	require.Zero(t, result.usage.OutputTokens)
	require.Empty(t, rec.Body.String())
	require.Empty(t, rec.Result().Header.Get("X-Request-Id"))
	require.Empty(t, rec.Result().Header.Get("X-Ratelimit-Remaining-Requests"))
}

func TestOpenAINativeFirstOutputStageOverflowFailsOverWithoutAttemptBytes(t *testing.T) {
	cfg := &config.Config{Gateway: config.GatewayConfig{
		OpenAIFirstOutputTimeoutSeconds: 30,
		MaxLineSize:                     2 * 1024 * 1024,
	}}
	svc := &OpenAIGatewayService{cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg)}
	const lineSize = 1024*1024 - 256
	prefix := `data: {"type":"response.output_text.delta","delta":"`
	suffix := `"}`
	line := prefix + strings.Repeat("x", lineSize-len(prefix)-len(suffix)) + suffix
	body := strings.Repeat(line+"\n", 9)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Request-Id":                   []string{"request-overflow"},
			"X-Ratelimit-Remaining-Requests": []string{"1"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.True(t, failoverErr.SafeToFailoverAfterWrite)
	require.Contains(t, string(failoverErr.ResponseBody), "first-output limit")
	require.Empty(t, rec.Body.String())
	require.Empty(t, rec.Header().Values("X-Request-Id"))
	require.Empty(t, rec.Header().Values("X-Ratelimit-Remaining-Requests"))
}

func TestOpenAINativeFirstOutputScannerRejectsOversizedLineWithoutLeak(t *testing.T) {
	cfg := &config.Config{Gateway: config.GatewayConfig{
		OpenAIFirstOutputTimeoutSeconds: 30,
		MaxLineSize:                     defaultMaxLineSize,
	}}
	svc := &OpenAIGatewayService{cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg)}
	oversizedLine := "data: " + strings.Repeat("x", openAIFirstOutputStageMaxBytes+openAIFirstOutputScannerFramingAllowance+1024)
	body := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_private\"}}\n\n" + oversizedLine + "\n"
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Request-Id":                   []string{"request-too-large"},
			"X-Ratelimit-Remaining-Requests": []string{"1"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.True(t, failoverErr.SafeToFailoverAfterWrite)
	require.Contains(t, string(failoverErr.ResponseBody), "line exceeds guarded first-output limit")
	require.Empty(t, rec.Body.String())
	require.Empty(t, rec.Header().Values("X-Request-Id"))
	require.Empty(t, rec.Header().Values("X-Ratelimit-Remaining-Requests"))
}

func TestOpenAINativeFirstOutputScannerAllowsLargeEventAfterSemanticBoundary(t *testing.T) {
	cfg := &config.Config{Gateway: config.GatewayConfig{
		OpenAIFirstOutputTimeoutSeconds: 30,
		MaxLineSize:                     defaultMaxLineSize,
	}}
	svc := &OpenAIGatewayService{cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg)}
	largeDelta := strings.Repeat("i", openAIFirstOutputStageMaxBytes+openAIFirstOutputScannerFramingAllowance+1024)
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"ready"}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"` + largeDelta + `"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_large_image","usage":{"input_tokens":4,"output_tokens":3}}}`,
		"",
		"",
	}, "\n")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"request-large-image"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	result, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.firstTokenMs)
	require.Equal(t, "resp_large_image", result.responseID)
	require.Equal(t, 4, result.usage.InputTokens)
	require.Equal(t, 3, result.usage.OutputTokens)
	require.Contains(t, rec.Body.String(), `"delta":"ready"`)
	require.Contains(t, rec.Body.String(), `"id":"resp_large_image"`)
	require.Contains(t, rec.Body.String(), strings.Repeat("i", 1024))
	require.Equal(t, "request-large-image", rec.Result().Header.Get("X-Request-Id"))
}

func TestOpenAINativeFirstOutputTimeoutDisabledKeepsPreamblePrivateAcrossKeepalive(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		StreamKeepaliveInterval: 1,
		MaxLineSize:             defaultMaxLineSize,
	}}}
	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_stalled\"}}\n\n"))
		_, _ = pw.Write([]byte("data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"resp_stalled\"}}\n\n"))
		time.Sleep(2100 * time.Millisecond)
	}()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: pr}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Contains(t, rec.Body.String(), ":\n\n")
	require.NotContains(t, rec.Body.String(), "response.created")
	require.NotContains(t, rec.Body.String(), "response.in_progress")
}

func TestOpenAINativeFirstOutputFailoverKeepsAttemptHeadersPrivateAfterKeepaliveCommit(t *testing.T) {
	cfg := &config.Config{Gateway: config.GatewayConfig{
		OpenAIFirstOutputTimeoutSeconds: 2,
		StreamKeepaliveInterval:         1,
		MaxLineSize:                     defaultMaxLineSize,
	}}
	svc := &OpenAIGatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	firstBody, firstWriter := io.Pipe()
	trackedFirstBody := &firstOutputCloseTrackingBody{ReadCloser: firstBody, closed: make(chan struct{})}
	firstWriterDone := make(chan struct{})
	go func() {
		defer close(firstWriterDone)
		defer func() { _ = firstWriter.Close() }()
		_, _ = firstWriter.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_first\"}}\n\n"))
		select {
		case <-trackedFirstBody.closed:
		case <-time.After(4 * time.Second):
		}
	}()
	firstResp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":                   []string{"text/event-stream"},
			"X-Request-Id":                   []string{"request-first"},
			"X-Ratelimit-Remaining-Requests": []string{"1"},
		},
		Body: trackedFirstBody,
	}

	_, firstErr := svc.handleStreamingResponse(c.Request.Context(), firstResp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, firstErr, &failoverErr)
	require.Contains(t, rec.Body.String(), ":\n\n", "first attempt should have committed only a stable keepalive")
	require.NotContains(t, rec.Body.String(), "resp_first")

	secondResp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":                   []string{"text/event-stream"},
			"X-Request-Id":                   []string{"request-second"},
			"X-Ratelimit-Remaining-Requests": []string{"99"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"hello"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_second","usage":{"input_tokens":1,"output_tokens":1}}}`,
			"",
			"",
		}, "\n"))),
	}
	result, secondErr := svc.handleStreamingResponse(c.Request.Context(), secondResp, c, &Account{ID: 2, Platform: PlatformOpenAI}, time.Now(), "model", "model")

	require.NoError(t, secondErr)
	require.NotNil(t, result)
	require.Contains(t, rec.Body.String(), "resp_second")
	wireHeaders := rec.Result().Header
	require.Empty(t, wireHeaders.Values("X-Request-Id"))
	require.Empty(t, wireHeaders.Values("X-Ratelimit-Remaining-Requests"))
	require.Empty(t, rec.Header().Values("X-Request-Id"))
	require.Empty(t, rec.Header().Values("X-Ratelimit-Remaining-Requests"))
	select {
	case <-firstWriterDone:
	case <-time.After(time.Second):
		t.Fatal("first account writer did not exit after timeout")
	}
}

func TestOpenAITurnFirstOutputDeadlineDefaultAndEnabled(t *testing.T) {
	for _, limit := range []int{0, 3} {
		budget := NewOpenAITurnBudget(&config.Config{Gateway: config.GatewayConfig{OpenAITurnMaxAttempts: limit}})
		budget.firstOutput = execution.NewFirstOutput(time.Now().Add(-2 * time.Second))
		ctx := WithOpenAITurnBudget(context.Background(), budget)
		forwardB := time.Now()
		deadline := openAITurnFirstOutputDeadline(ctx, time.Second, forwardB)
		require.Equal(t, budget.firstOutput.Snapshot().Started.Add(time.Second), deadline)
		require.ErrorIs(t, TakeOpenAITurnAttempt(ctx), ErrOpenAIWSFirstOutputBudgetExhausted)
	}
}

func TestOpenAITurnDeadlineBoundsWaitsWithoutCancelingResponseContext(t *testing.T) {
	cfg := &config.Config{Gateway: config.GatewayConfig{OpenAITurnMaxAttempts: 3, OpenAIFirstOutputTimeoutSeconds: 60, OpenAIHighEffortFirstOutputTimeoutSeconds: 180}}
	ctx := EnsureOpenAITurnBudget(context.Background(), cfg, "high")
	budget := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	require.Equal(t, 180*time.Second, budget.firstOutput.Snapshot().Deadline.Sub(budget.firstOutput.Snapshot().Started))
	setTurnOutputDeadlineForTest(budget, time.Now().Add(25*time.Millisecond))
	require.ErrorIs(t, WaitOpenAITurnRetry(ctx, time.Second), ErrOpenAIWSFirstOutputBudgetExhausted)
	require.NoError(t, ctx.Err(), "a finished pre-output wait must not cancel an already streaming response")
	require.ErrorIs(t, TakeOpenAITurnAttempt(ctx), ErrOpenAIWSFirstOutputBudgetExhausted)
	require.Zero(t, budget.attemptBudget.Snapshot().Attempts, "admission exhaustion never sends or penalizes another account")
	cfg.Gateway.OpenAITurnMaxAttempts = 0
	legacy := EnsureOpenAITurnBudget(context.Background(), cfg, "high")
	wait, cancel := OpenAITurnWaitContext(legacy)
	defer cancel()
	_, hasDeadline := wait.Deadline()
	require.True(t, hasDeadline, "default zero uses the shared deadline")
}

func TestOpenAIHTTPFirstOutputDeadlineCoversHeadersAndBody(t *testing.T) {
	for _, mode := range []string{"bridge", "passthrough"} {
		for _, headers := range []bool{false, true} {
			t.Run(mode+"/"+map[bool]string{false: "body", true: "headers"}[headers], func(t *testing.T) {
				cfg := &config.Config{Gateway: config.GatewayConfig{OpenAITurnMaxAttempts: 3, OpenAIFirstOutputTimeoutSeconds: 60}}
				parent, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				ctx := EnsureOpenAITurnBudget(parent, cfg, "medium")
				budget := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
				setTurnOutputDeadlineForTest(budget, time.Now().Add(40*time.Millisecond))
				var upstream HTTPUpstream
				closed := make(chan struct{})
				if headers {
					upstream = &blockingOpenAIResponseHeaderUpstream{canceled: closed}
				} else {
					pr, pw := io.Pipe()
					defer pw.Close()
					body := &firstOutputCloseTrackingBody{ReadCloser: pr, closed: closed}
					upstream = &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body}}
					go func() {
						_, _ = io.WriteString(pw, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_discarded\"}}\n\n: heartbeat\n\n")
					}()
				}
				svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
				account := &Account{ID: 41, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				payload := []byte(`{"type":"response.create","model":"gpt-6.1-sol","input":"hello"}`)
				writes := 0
				var err error
				if mode == "bridge" {
					_, err = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, nil, account, "token", payload, len(payload), "gpt-6.1-sol", "", "", "", "", 1, func([]byte) error { writes++; return nil })
				} else {
					account.Type = AccountTypeAPIKey
					account.Credentials = map[string]any{"api_key": "test"}
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
					_, err = svc.forwardOpenAIPassthrough(ctx, c, account, []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"hello"}`), nil, "gpt-6.1-sol", false, nil, true, time.Now())
					require.NotContains(t, rec.Body.String(), "resp_discarded")
				}
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Equal(t, http.StatusGatewayTimeout, failover.StatusCode)
				require.Zero(t, writes, "uncommitted identity and heartbeats do not end the first-output budget")
				select {
				case <-closed:
				case <-parent.Done():
					t.Fatal("upstream reader was not canceled")
				}
				require.Equal(t, 1, budget.attemptBudget.Snapshot().Attempts)
			})
		}
	}
}

func TestOpenAIHTTPFirstOutputDeadlineStopsAfterContent(t *testing.T) {
	for _, mode := range []string{"bridge", "passthrough"} {
		t.Run(mode, func(t *testing.T) {
			cfg := &config.Config{Gateway: config.GatewayConfig{OpenAITurnMaxAttempts: 3, OpenAIFirstOutputTimeoutSeconds: 60}}
			ctx := EnsureOpenAITurnBudget(context.Background(), cfg, "medium")
			budget := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
			setTurnOutputDeadlineForTest(budget, time.Now().Add(40*time.Millisecond))
			pr, pw := io.Pipe()
			defer pw.Close()
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: pr}}
			writerDone := make(chan struct{})
			go func() {
				defer close(writerDone)
				defer pw.Close()
				_, _ = io.WriteString(pw, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"working\"}\n\n")
				time.Sleep(80 * time.Millisecond)
				_, _ = io.WriteString(pw, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_kept\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
			}()
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			account := &Account{ID: 41, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
			payload := []byte(`{"type":"response.create","model":"gpt-6.1-sol","input":"hello"}`)
			if mode == "bridge" {
				result, err := svc.proxyOpenAIWSHTTPBridgeTurn(ctx, nil, account, "token", payload, len(payload), "gpt-6.1-sol", "", "", "", "", 1, func([]byte) error { return nil })
				require.NoError(t, err)
				require.Equal(t, "response.completed", result.UpstreamTerminalEvent)
			} else {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
				result, err := svc.handleStreamingResponsePassthrough(ctx, upstream.resp, c, account, time.Now(), "gpt-6.1-sol", "gpt-6.1-sol")
				require.NoError(t, err)
				require.Equal(t, 1, result.usage.OutputTokens)
				require.Contains(t, rec.Body.String(), "resp_kept")
			}
			<-writerDone
		})
	}
}

// takeOpenAITurnAttempts prepares consumed accounting through its public API.
func takeOpenAITurnAttempts(t *testing.T, budget *OpenAITurnBudget, count int) {
	t.Helper()
	for range count {
		require.NoError(t, budget.attemptBudget.Take(nil))
	}
}

// Tests use the production deadline API instead of mutating private fields.
func setTurnOutputDeadlineForTest(b *OpenAITurnBudget, deadline time.Time) {
	state := b.firstOutput.Snapshot()
	accepted := state.Started
	if latest := deadline.Add(-state.Paused - time.Nanosecond); latest.Before(accepted) {
		accepted = latest
	}
	b.firstOutput.Limit(deadline.Sub(accepted)-state.Paused, accepted)
}

func newTurnOutputBudgetForTest(started time.Time, timeout time.Duration) *OpenAITurnBudget {
	b := NewOpenAITurnBudget(nil)
	b.firstOutput = execution.NewFirstOutput(started)
	b.firstOutput.Configure(timeout, true, false)
	return b
}
