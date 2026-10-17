package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand/v2"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	"github.com/gin-gonic/gin"
)

type openAITurnBudgetKey struct{}

// OpenAITurnBudget is shared through context across account switches and
// transport-local retries. It does not contain request data or credentials.
type OpenAITurnBudget struct {
	mu                 sync.Mutex
	limit, attempts    int
	started            time.Time
	deadline           time.Time
	automaticDeadline  time.Time
	control            context.Context
	protectionAccounts map[int64]bool
	bpsRecovery        excelBPSPoolRecovery
	preparationRetries int
	refreshedGrants    map[turnRecoveryTarget]bool
	retryWait          time.Duration
	pinnedAccount      *Account
	owners             map[turnRecoveryOwner]int
	targets            map[turnRecoveryTarget]int
	excludedModes      map[turnRecoveryOwner]map[string]bool
	failedDomains      map[turnRecoveryOwner]turnRecoveryDomain
	model              string
	outputCommitted    bool
	outputReady        chan struct{}
	selection          selectionbudget.Aggregate
}

var ErrOpenAITurnAttemptBudgetExhausted = errors.New("OpenAI turn attempt budget exhausted before upstream send")

func NewOpenAITurnBudget(cfg *config.Config) *OpenAITurnBudget {
	b := &OpenAITurnBudget{started: time.Now(), limit: 8}
	if cfg != nil && cfg.Gateway.OpenAITurnMaxAttempts > 0 {
		b.limit = min(b.limit, cfg.Gateway.OpenAITurnMaxAttempts)
	}
	return b
}

func WithOpenAITurnBudget(ctx context.Context, budget *OpenAITurnBudget) context.Context {
	managed := false
	if previous, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); previous != nil {
		previous.mu.Lock()
		managed = previous.bpsRecovery.managed
		previous.mu.Unlock()
	}
	if budget != nil {
		budget.mu.Lock()
		budget.bpsRecovery.managed = budget.bpsRecovery.managed || managed
		if budget.control == nil {
			budget.control = ctx
		}
		budget.mu.Unlock()
	}
	if budget != nil {
		ctx = selectionbudget.WithAggregate(ctx, &budget.selection)
	}
	return context.WithValue(ctx, openAITurnBudgetKey{}, budget)
}

func EnsureOpenAITurnBudget(ctx context.Context, cfg *config.Config, reasoningEffort ...string) context.Context {
	b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if b == nil {
		b = NewOpenAITurnBudget(cfg)
		ctx = WithOpenAITurnBudget(ctx, b)
	}
	effort := ""
	if len(reasoningEffort) > 0 {
		effort = reasoningEffort[0]
	}
	b.mu.Lock()
	if !b.outputCommitted && (b.deadline.IsZero() || (len(reasoningEffort) > 0 && b.attempts == 0 && !b.automaticDeadline.IsZero() && b.deadline.Equal(b.automaticDeadline))) {
		b.deadline = b.started.Add(openAIFirstOutputTimeoutForConfig(cfg, effort))
		if len(reasoningEffort) == 0 {
			b.automaticDeadline = b.deadline
		} else {
			b.automaticDeadline = time.Time{}
		}
	}

	b.mu.Unlock()

	return ctx
}

// Bound admission and backoff without putting a pre-output cancellation on the
// entire response stream. Each waiter releases its child context before relay;
// the absolute deadline remains owned by the logical turn.
func OpenAITurnWaitContext(ctx context.Context) (context.Context, context.CancelFunc) {
	b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if b != nil {
		b.mu.Lock()
		deadline := b.deadline
		b.mu.Unlock()
		if !deadline.IsZero() {
			return context.WithDeadlineCause(ctx, deadline, ErrOpenAIWSFirstOutputBudgetExhausted)
		}
	}
	return ctx, func() {}
}

func WaitOpenAITurnRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	if b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); b != nil {
		b.mu.Lock()
		if delay > 10*time.Second-b.retryWait {
			b.mu.Unlock()
			return ErrTurnRetryWaitExhausted
		}
		// Positive jitter never retries before Retry-After and stays inside the hard wait cap.
		jitter := time.Duration(mathrand.Int64N(int64(delay/10) + 1))
		if available := 10*time.Second - b.retryWait - delay; jitter > available {
			jitter = available
		}
		delay += jitter
		b.retryWait += delay
		b.mu.Unlock()
	}
	waitCtx, cancel := OpenAITurnWaitContext(ctx)
	defer cancel()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-waitCtx.Done():
		return context.Cause(waitCtx)
	case <-timer.C:
		return nil
	}
}

func TakeOpenAITurnAttempt(ctx context.Context) error {
	// Preparation retries have their own small allowance; only generation sends
	// consume this counter, across all accounts and transports.
	b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if b == nil {
		return ctx.Err()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.control != nil && b.control.Err() != nil {
		return b.control.Err()
	}
	if !b.deadline.IsZero() && !time.Now().Before(b.deadline) {
		return ErrOpenAIWSFirstOutputBudgetExhausted
	}
	if b.limit > 0 && b.attempts >= b.limit {
		return ErrOpenAITurnAttemptBudgetExhausted
	}
	if target, ok := ctx.Value(turnRecoveryTargetKey{}).(turnRecoveryTarget); ok {
		if b.owners == nil {
			b.owners = make(map[turnRecoveryOwner]int)
			b.targets = make(map[turnRecoveryTarget]int)
		}
		if b.owners[target.owner] >= 3 || (target.limit == 1 && b.owners[target.owner] >= 1) || b.targets[target] >= target.limit {
			return ErrTurnTargetExhausted
		}
		b.owners[target.owner]++
		b.targets[target]++
	}
	b.attempts++
	return nil
}

func openAITurnFirstOutputDeadline(ctx context.Context, timeout time.Duration, fallback time.Time) time.Time {
	b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if b == nil {
		return fallback.Add(timeout)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	started := b.started
	if !fallback.IsZero() && fallback.Before(started) {
		started = fallback // WS acceptance can precede allocation of its budget.
	}
	if !b.outputCommitted && timeout > 0 && (b.deadline.IsZero() || started.Add(timeout).Before(b.deadline)) {
		b.deadline = started.Add(timeout)
	}
	return b.deadline
}

// Correlated protection failures on independent credentials are not repaired
// by exhausting every account. Stop this turn after two distinct accounts;
// future requests remain free to probe recovery without a global outage flag.
func OpenAITurnRetryAllowed(ctx context.Context, accountID int64, response []byte) bool {
	b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.control != nil && b.control.Err() != nil {
		return false
	}
	if b.limit > 0 && b.attempts >= b.limit {
		return false
	}
	if !b.deadline.IsZero() && !time.Now().Before(b.deadline) {
		return false
	}
	if b.limit == 0 || !isOpenAIResponseProtectionUnavailable("", response) {
		return true
	}
	if b.protectionAccounts == nil {
		b.protectionAccounts = make(map[int64]bool)
	}
	b.protectionAccounts[accountID] = true
	return len(b.protectionAccounts) < 2
}

const (
	openAIFirstOutputStageMemoryLimit        = 64 * 1024
	openAIFirstOutputStageMaxBytes           = 8 * 1024 * 1024
	openAIFirstOutputScannerFramingAllowance = 64
	openAIFirstOutputGuardQueueSize          = 1
	openAIDefaultStreamQueueSize             = 16
)

var (
	errOpenAIFirstOutputStageLimit   = errors.New("openai first-output staging limit exceeded")
	errOpenAIFirstOutputScannerLimit = errors.New("openai pre-output scanner token limit exceeded")
)

type openAIFirstOutputStage struct {
	limit      int64
	size       int64
	memory     bytes.Buffer
	tempFile   *os.File
	tempPath   string
	createTemp func() (*os.File, error)
	removeFile func(string) error
	memoryOnly bool
	cleanupErr error
	closed     bool
}

func newOpenAIFirstOutputStage(limit int64) *openAIFirstOutputStage {
	if limit < 1 {
		limit = 1
	}
	return &openAIFirstOutputStage{
		limit:      limit,
		createTemp: func() (*os.File, error) { return os.CreateTemp("", "sub2api-openai-first-output-*") },
		removeFile: os.Remove,
		memoryOnly: runtime.GOOS == "windows",
	}
}

func newDefaultOpenAIFirstOutputStage() *openAIFirstOutputStage {
	return newOpenAIFirstOutputStage(openAIFirstOutputStageMaxBytes)
}

func openAIFirstOutputEventQueueSize(guardFirstOutput bool) int {
	if guardFirstOutput {
		return openAIFirstOutputGuardQueueSize
	}
	return openAIDefaultStreamQueueSize
}

func openAIFirstOutputDynamicScanLines(guardActive *atomic.Bool) bufio.SplitFunc {
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		advance, token, err = bufio.ScanLines(data, atEOF)
		if err != nil || guardActive == nil || !guardActive.Load() {
			return advance, token, err
		}
		limit := openAIFirstOutputStageMaxBytes + openAIFirstOutputScannerFramingAllowance
		if token != nil {
			if len(token) > limit {
				return 0, nil, errOpenAIFirstOutputScannerLimit
			}
			return advance, token, nil
		}
		// At the limit with no delimiter, another byte would necessarily exceed
		// the guarded token budget. Fail before Scanner grows toward MaxLineSize.
		if len(data) >= limit {
			return 0, nil, errOpenAIFirstOutputScannerLimit
		}
		return advance, token, nil
	}
}

func (s *openAIFirstOutputStage) Buffered() int64 {
	if s == nil {
		return 0
	}
	return s.size
}

func (s *openAIFirstOutputStage) WriteString(value string) (int, error) {
	if err := s.prepareWrite(len(value)); err != nil {
		return 0, err
	}
	var n int
	var err error
	if s.tempFile == nil {
		n, err = s.memory.WriteString(value)
	} else {
		n, err = io.WriteString(s.tempFile, value)
	}
	s.size += int64(n)
	if err != nil {
		return n, fmt.Errorf("write first-output stage: %w", err)
	}
	return n, nil
}

func (s *openAIFirstOutputStage) Write(p []byte) (int, error) {
	if err := s.prepareWrite(len(p)); err != nil {
		return 0, err
	}
	var n int
	var err error
	if s.tempFile == nil {
		n, err = s.memory.Write(p)
	} else {
		n, err = s.tempFile.Write(p)
	}
	s.size += int64(n)
	if err != nil {
		return n, fmt.Errorf("write first-output stage: %w", err)
	}
	return n, nil
}

func (s *openAIFirstOutputStage) prepareWrite(incoming int) error {
	if s == nil || s.closed {
		return os.ErrClosed
	}
	if int64(incoming) > s.limit-s.size {
		return fmt.Errorf("%w: buffered=%d incoming=%d limit=%d", errOpenAIFirstOutputStageLimit, s.size, incoming, s.limit)
	}
	if s.tempFile != nil || s.memoryOnly || s.size+int64(incoming) <= openAIFirstOutputStageMemoryLimit {
		return nil
	}
	file, err := s.createTemp()
	if err != nil {
		return fmt.Errorf("create first-output spool: %w", err)
	}
	path := file.Name()
	// Unlink before writing any request data. Unix keeps the file descriptor
	// readable, while crashes and SIGKILL cannot leave a named plaintext spool.
	if unlinkErr := s.removeFile(path); unlinkErr != nil {
		closeErr := file.Close()
		removeErr := s.removeFile(path)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		s.memoryOnly = true
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			s.tempPath = path
		}
		s.cleanupErr = errors.Join(
			s.cleanupErr,
			fmt.Errorf("unlink first-output spool before use: %w", unlinkErr),
			closeErr,
			removeErr,
		)
		return nil
	}
	if _, err := file.Write(s.memory.Bytes()); err != nil {
		_ = file.Close()
		return fmt.Errorf("initialize first-output spool: %w", err)
	}
	s.tempFile = file
	s.tempPath = path
	s.memory.Reset()
	return nil
}

func (s *openAIFirstOutputStage) CommitTo(dst io.Writer) error {
	if s == nil || s.closed {
		return os.ErrClosed
	}
	if s.tempFile == nil {
		if _, err := io.Copy(dst, bytes.NewReader(s.memory.Bytes())); err != nil {
			return err
		}
	} else {
		if _, err := s.tempFile.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("seek first-output spool: %w", err)
		}
		if _, err := io.CopyN(dst, s.tempFile, s.size); err != nil {
			return err
		}
	}
	if err := s.Close(); err != nil {
		// Delivery succeeded. Preserve cleanup failures for the handler's deferred
		// cleanup/logging pass instead of turning committed bytes into a stream error.
		s.cleanupErr = errors.Join(s.cleanupErr, err)
	}
	return nil
}

func (s *openAIFirstOutputStage) Close() error {
	if s == nil {
		return nil
	}
	if s.closed && s.tempFile == nil && s.tempPath == "" && s.cleanupErr == nil {
		return nil
	}
	s.closed = true
	s.size = 0
	s.memory.Reset()
	closeErr := s.cleanupErr
	s.cleanupErr = nil
	if s.tempFile != nil {
		closeErr = errors.Join(closeErr, s.tempFile.Close())
		s.tempFile = nil
	}
	if s.tempPath != "" {
		removeErr := s.removeFile(s.tempPath)
		if removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
			s.tempPath = ""
		} else {
			closeErr = errors.Join(closeErr, removeErr)
		}
	}
	return closeErr
}

func (s *OpenAIGatewayService) openAIFirstOutputTimeout(reasoningEffort string) time.Duration {
	if s == nil {
		return 0
	}
	return openAIFirstOutputTimeoutForConfig(s.cfg, reasoningEffort)
}

func openAIFirstOutputTimeoutForConfig(cfg *config.Config, reasoningEffort string) time.Duration {
	seconds := 300
	if cfg != nil && cfg.Gateway.OpenAIFirstOutputTimeoutSeconds > 0 {
		seconds = cfg.Gateway.OpenAIFirstOutputTimeoutSeconds
	}
	switch strings.ToLower(strings.TrimSpace(reasoningEffort)) {
	case "high", "xhigh", "max":
		if cfg != nil && cfg.Gateway.OpenAIHighEffortFirstOutputTimeoutSeconds > 0 {
			seconds = cfg.Gateway.OpenAIHighEffortFirstOutputTimeoutSeconds
		} else if cfg == nil || cfg.Gateway.OpenAIFirstOutputTimeoutSeconds <= 0 {
			seconds = 600
		}
	}
	return time.Duration(seconds) * time.Second
}

// newOpenAIFirstOutputTimeoutError records the timeout as an upstream attempt
// and returns the failover error. proxyID/proxyName are supplied by the caller
// because the same deadline is enforced over HTTP and WebSocket transports,
// whose direct-route semantics differ (see opsUpstreamWSProxyAttribution).
func (s *OpenAIGatewayService) newOpenAIFirstOutputTimeoutError(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	proxyID *int64,
	proxyName string,
	startTime time.Time,
	originalModel string,
	reasoningEffort string,
	timeout time.Duration,
	phase string,
	responseHeaders http.Header,
) *UpstreamFailoverError {
	elapsed := time.Since(startTime)
	logger.LegacyPrintf(
		"service.openai_gateway",
		"OpenAI first output timeout: account=%d model=%s effort=%s phase=%s elapsed=%s limit=%s",
		account.ID, originalModel, reasoningEffort, phase, elapsed, timeout,
	)
	requestID := strings.TrimSpace(responseHeaders.Get("x-request-id"))
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:   proxyID,
		ProxyName: proxyName,
		Platform:  account.Platform, AccountID: account.ID, AccountName: account.Name,
		UpstreamStatusCode: http.StatusGatewayTimeout, UpstreamRequestID: requestID,
		Kind: "first_output_timeout", Message: "OpenAI upstream produced no semantic output before the deadline",
		Detail: fmt.Sprintf("phase=%s elapsed_ms=%d timeout_ms=%d", phase, elapsed.Milliseconds(), timeout.Milliseconds()),
	})
	if s.rateLimitService != nil {
		s.rateLimitService.HandleStreamTimeout(ctx, account, originalModel)
	}
	return &UpstreamFailoverError{
		StatusCode:      http.StatusGatewayTimeout,
		ResponseBody:    []byte(`{"error":{"type":"first_output_timeout","message":"Upstream produced no output before the deadline"}}`),
		ResponseHeaders: responseHeaders.Clone(), SafeToFailoverAfterWrite: true,
	}
}

type openAIFirstOutputHeaderGuard struct {
	cancel  context.CancelFunc
	release context.CancelFunc
	timer   *time.Timer
	fired   chan struct{}
	once    sync.Once
}

func newOpenAIFirstOutputHeaderGuard(
	ctx context.Context,
	release context.CancelFunc,
	deadline time.Time,
) (context.Context, *openAIFirstOutputHeaderGuard) {
	guardedCtx, cancel := context.WithCancel(ctx)
	guard := &openAIFirstOutputHeaderGuard{cancel: cancel, release: release, fired: make(chan struct{})}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		remaining = time.Nanosecond
	}
	guard.timer = time.AfterFunc(remaining, func() {
		close(guard.fired)
		cancel()
	})
	return guardedCtx, guard
}

func (g *openAIFirstOutputHeaderGuard) stopHeaderWait() bool {
	if g.timer.Stop() {
		return false
	}
	<-g.fired
	return true
}

func (g *openAIFirstOutputHeaderGuard) expired() bool {
	if g == nil {
		return false
	}
	select {
	case <-g.fired:
		return true
	default:
		return false
	}
}

func (g *openAIFirstOutputHeaderGuard) close() {
	g.once.Do(func() {
		g.timer.Stop()
		g.cancel()
		g.release()
	})
}

type openAIRequestContextReadCloser struct {
	io.ReadCloser
	ctx     context.Context
	cleanup func()
	once    sync.Once
	err     error
}

func (r *openAIRequestContextReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err != nil && r.ctx != nil && IsTurnBudgetError(context.Cause(r.ctx)) {
		return n, context.Cause(r.ctx)
	}
	return n, err
}

func (r *openAIRequestContextReadCloser) Close() error {
	r.once.Do(func() {
		r.cleanup()
		r.err = r.ReadCloser.Close()
	})
	return r.err
}
