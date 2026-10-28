package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"golang.org/x/sync/errgroup"
)

const (
	// Leave headroom before the provider's hard 60-minute lifetime. Active
	// leases rotate at the next turn boundary, never in the middle of output.
	openAIWSConnMaxAge          = 55 * time.Minute
	openAIWSConnHealthCheckIdle = 90 * time.Second
	// 仅对没有常驻读循环的连接实现生效：这类连接空闲时无人应答上游 ping，须在
	// 上游保活窗口到期前回收。coder/websocket 连接由池常驻读循环应答 ping，不受此阈值约束。
	openAIWSConnIdleRecycleAfter = 90 * time.Second
	openAIWSConnHealthCheckTO    = 2 * time.Second
	// 不在请求热路径上的探活（后台巡检、轮次间预检）给经代理链路的 pong 留足余量，
	// 实测最大往返约 1.7s；误判的代价是换连甚至断会话，比多等几秒重得多。
	openAIWSProbePingTO            = 10 * time.Second
	openAIWSConnPrewarmExtraDelay  = 2 * time.Second
	openAIWSAcquireCleanupInterval = 3 * time.Second
	openAIWSBackgroundPingInterval = 30 * time.Second
	openAIWSBackgroundSweepTicker  = 30 * time.Second

	openAIWSPrewarmFailureWindow   = 30 * time.Second
	openAIWSPrewarmFailureSuppress = 2
)

var (
	errOpenAIWSConnClosed                 = errors.New("openai ws connection closed")
	errOpenAIWSConnQueueFull              = errors.New("openai ws connection queue full")
	errOpenAIWSPreferredConnUnavailable   = errors.New("openai ws preferred connection unavailable")
	errOpenAIWSResponseContextUnavailable = errors.New("openai ws response context unavailable")
	errOpenAIWSPoolChanged                = errors.New("openai ws account pool changed")
)

type openAIWSDialError struct {
	StatusCode      int
	ResponseHeaders http.Header
	ResponseBody    []byte
	Err             error
}

func (e *openAIWSDialError) Error() string {
	if e == nil {
		return ""
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("openai ws dial failed: status=%d err=%v", e.StatusCode, e.Err)
	}
	return fmt.Sprintf("openai ws dial failed: %v", e.Err)
}

func (e *openAIWSDialError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Proven edge/policy denials did not reject the account credential. Reuse the
// scheduler's existing scope contract instead of adding a cooldown mechanism.
func (e *openAIWSDialError) RequestScoped() bool {
	_, class, _ := OpenAIWSHandshakeFailure(e)
	return class == "handshake_edge_challenge" || class == "handshake_edge_block" || class == "handshake_request_policy"
}

type openAIWSAcquireRequest struct {
	TenantScope     string   // authenticated tenant, independent of client-controlled headers
	CredentialScope [32]byte // selected resource and credential owner generation; not an authorization grant
	Account         *Account
	WSURL           string
	Headers         http.Header
	// HeadersFactory is evaluated inside dialConn. It exists so credentials
	// whose authorization is per-dial (Agent Identity) are never cached in
	// lastAcquire or delayed prewarm state.
	HeadersFactory        func(context.Context, http.Header) (http.Header, error)
	HandshakeFactsFactory func(context.Context, http.Header) (nativeWSHandshake, error)
	ProxyURL              string
	PreferredConnID       string
	// RequiredResponseID proves a connection-local producer after queueing.
	RequiredResponseID string
	// ForceNewConn: 强制本次获取新连接（避免复用导致连接内续链状态互相污染）。
	ForceNewConn bool
	// ForcePreferredConn: 强制本次只使用 PreferredConnID，禁止漂移到其它连接。
	ForcePreferredConn bool
}

type openAIWSHandshakeCompatibilityKey struct {
	targetURL           string
	proxyURL            string
	credentialScope     [32]byte
	tenantScope         string
	betaFeatures        string
	codexInstallationID string
	sessionIDHyphen     string
	sessionIDUnderscore string
	threadID            string
	clientRequestID     string
	codexWindowID       string
}

type openAIWSConnLease struct {
	pool          *openAIWSConnPool
	accountID     int64
	conn          *openAIWSConn
	queueWait     time.Duration
	connPick      time.Duration
	idleBefore    time.Duration
	ageBefore     time.Duration
	reused        bool
	released      atomic.Bool
	nativeAttempt atomic.Pointer[nativeWSAttempt]
	// Receipt of this lease's actual generation write, using the connection's
	// existing counter. Control writes cannot certify a generation response.
	responseWriteGeneration atomic.Uint64
	requiredResponse        atomic.Pointer[openAIWSConnResponse]
}

func (l *openAIWSConnLease) activeConn() (*openAIWSConn, error) {
	if l == nil || l.conn == nil {
		return nil, errOpenAIWSConnClosed
	}
	if l.released.Load() {
		return nil, errOpenAIWSConnClosed
	}
	return l.conn, nil
}

func (l *openAIWSConnLease) ConnID() string {
	if l == nil || l.conn == nil {
		return ""
	}
	return l.conn.id
}

func (l *openAIWSConnLease) ConfirmResponse(responseID string, generation uint64) {
	conn, err := l.activeConn()
	responseID = stringsTrim(responseID)
	if err != nil || responseID == "" {
		return
	}
	if err := conn.writeMu.lock(context.Background()); err != nil {
		return
	}
	defer conn.writeMu.unlock()
	if !l.released.Load() && conn.isLeased() && !conn.isClosed() && !conn.unusable.Load() &&
		generation > 0 && generation == l.responseWriteGeneration.Load() && generation == conn.writeGeneration.Load() {
		conn.latestResponse.Store(&openAIWSConnResponse{responseID: responseID, writeGeneration: generation})
	}
}

func (l *openAIWSConnLease) HasResponseContext(responseID string) bool {
	conn, err := l.activeConn()
	return err == nil && conn.isLeased() && conn.matchesResponseContext(responseID)
}

func (l *openAIWSConnLease) QueueWaitDuration() time.Duration {
	if l == nil {
		return 0
	}
	return l.queueWait
}

func (l *openAIWSConnLease) ConnPickDuration() time.Duration {
	if l == nil {
		return 0
	}
	return l.connPick
}

func (l *openAIWSConnLease) Reused() bool {
	if l == nil {
		return false
	}
	return l.reused
}

// IdleBefore 返回借出时该连接已空闲的时长。
func (l *openAIWSConnLease) IdleBefore() time.Duration {
	if l == nil {
		return 0
	}
	return l.idleBefore
}

// AgeBefore 返回借出时该连接自建立起的时长。
func (l *openAIWSConnLease) AgeBefore() time.Duration {
	if l == nil {
		return 0
	}
	return l.ageBefore
}

func (l *openAIWSConnLease) UpstreamPingCount() int64 {
	if l == nil || l.conn == nil {
		return 0
	}
	return l.conn.upstreamPingCount()
}

func (l *openAIWSConnLease) HandshakeHeader(name string) string {
	if l == nil || l.conn == nil {
		return ""
	}
	return l.conn.handshakeHeader(name)
}

func (l *openAIWSConnLease) HandshakeHeaders() http.Header {
	if l == nil || l.conn == nil {
		return nil
	}
	return cloneHeader(l.conn.handshakeHeaders)
}

func (l *openAIWSConnLease) IsPrewarmed() bool {
	if l == nil || l.conn == nil {
		return false
	}
	return l.conn.isPrewarmed()
}

func (l *openAIWSConnLease) MarkPrewarmed() {
	if l == nil || l.conn == nil {
		return
	}
	l.conn.markPrewarmed()
}

func (l *openAIWSConnLease) WriteJSON(value any, timeout time.Duration) error {
	conn, err := l.activeConn()
	if err != nil {
		return err
	}
	return conn.writeJSONWithTimeout(context.Background(), value, timeout, l)
}

func (l *openAIWSConnLease) WriteJSONWithContextTimeout(ctx context.Context, value any, timeout time.Duration) error {
	conn, err := l.activeConn()
	if err != nil {
		return err
	}
	return conn.writeJSONWithTimeout(ctx, value, timeout, l)
}

func (l *openAIWSConnLease) WriteJSONContext(ctx context.Context, value any) error {
	conn, err := l.activeConn()
	if err != nil {
		return err
	}
	return conn.writeJSON(value, ctx, l)
}

func (l *openAIWSConnLease) ReadMessage(timeout time.Duration) ([]byte, error) {
	conn, err := l.activeConn()
	if err != nil {
		return nil, err
	}
	body, readErr := conn.readMessageWithTimeout(timeout)
	return body, l.nativeAttempt.Load().observe(body, readErr)
}

func (l *openAIWSConnLease) ReadMessageContext(ctx context.Context) ([]byte, error) {
	conn, err := l.activeConn()
	if err != nil {
		return nil, err
	}
	body, readErr := conn.readMessage(ctx)
	return body, l.nativeAttempt.Load().observe(body, readErr)
}

func (l *openAIWSConnLease) ReadMessageWithContextTimeout(ctx context.Context, timeout time.Duration) ([]byte, error) {
	conn, err := l.activeConn()
	if err != nil {
		return nil, err
	}
	body, readErr := conn.readMessageWithContextTimeout(ctx, timeout)
	return body, l.nativeAttempt.Load().observe(body, readErr)
}

func (l *openAIWSConnLease) PingWithTimeout(timeout time.Duration) error {
	conn, err := l.activeConn()
	if err != nil {
		return err
	}
	return conn.pingWithTimeout(timeout)
}

func (l *openAIWSConnLease) SupportsIdlePingWithoutReader() bool {
	conn, err := l.activeConn()
	if err != nil {
		return false
	}
	return conn.supportsIdlePingWithoutReader()
}

func (l *openAIWSConnLease) MarkBroken() {
	if l == nil || l.pool == nil || l.conn == nil || l.released.Load() {
		return
	}
	l.pool.evictConn(l.accountID, l.conn.id)
}

func (l *openAIWSConnLease) Release() {
	if l == nil || l.conn == nil {
		return
	}
	// Transfer the exclusive lease only after its in-flight writer has left.
	if err := l.conn.writeMu.lock(context.Background()); err != nil {
		return
	}
	defer l.conn.writeMu.unlock()
	if !l.released.CompareAndSwap(false, true) {
		return
	}
	l.nativeAttempt.Swap(nil).close(nil)
	l.conn.release()
	if l.pool != nil {
		l.pool.notifyAccountPoolChanged(l.accountID)
		l.pool.retireReleasedConn(l.conn)
	}
}

// openAIWSWriteGate is the existing pool writer coordinator with cancellable
// acquisition. It replaces, rather than layers on top of, the previous mutex.
// The zero value is usable by connection fixtures as well as production dials.
type openAIWSWriteGate struct {
	once  sync.Once
	token chan struct{}
}

func (g *openAIWSWriteGate) lock(ctx context.Context) error {
	if ctx == nil {
		return native.ErrInvalidWSPreparation
	}
	g.once.Do(func() { g.token = make(chan struct{}, 1); g.token <- struct{}{} })
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-g.token:
		if err := ctx.Err(); err != nil {
			g.unlock()
			return context.Cause(ctx)
		}
		return nil
	}
}
func (g *openAIWSWriteGate) unlock() { g.token <- struct{}{} }

// WriteWS retains the exclusive lease through the physical write. A transport
// without the actual last-writer gate is denied; legacy WriteJSON is no fallback.
func (l *openAIWSConnLease) WriteWS(ctx context.Context, write *native.WSWrite) (model.SendState, error) {
	c, err := l.activeConn()
	if err != nil {
		return model.SendNotSent, err
	}
	if err := c.writeMu.lock(ctx); err != nil {
		return model.SendNotSent, err
	}
	state, err := func() (model.SendState, error) {
		defer c.writeMu.unlock()
		if l.released.Load() || c.isClosed() || !c.isLeased() || c.unusable.Load() {
			return model.SendNotSent, errOpenAIWSConnClosed
		}
		requiredReceipt := l.requiredResponse.Load()
		if requiredReceipt != nil && (!c.matchesResponseContext(requiredReceipt.responseID) ||
			requiredReceipt.writeGeneration != c.writeGeneration.Load()) {
			return model.SendNotSent, errors.Join(ErrOpenAIContinuationUnavailable, errOpenAIWSResponseContextUnavailable)
		}
		if required, ok := ctx.Value(openAIWSResponseRequirementKey{}).(openAIWSResponseRequirement); ok {
			if required.lease != l || !c.matchesResponseContext(required.responseID) ||
				required.generation == 0 || required.generation != c.writeGeneration.Load() {
				return model.SendNotSent, errors.Join(ErrOpenAIContinuationUnavailable, errOpenAIWSResponseContextUnavailable)
			}
		}
		writer, ok := c.ws.(native.WSWriter)
		if !ok {
			return model.SendNotSent, native.ErrInvalidWSPreparation
		}
		state, err := writer.WriteWS(ctx, write)
		if state == model.SendSent || state == model.SendAmbiguous {
			l.responseWriteGeneration.Store(c.writeGeneration.Add(1))
			l.requiredResponse.CompareAndSwap(requiredReceipt, nil)
		}
		if err == nil {
			c.touch()
		}
		// Do not let Release transfer a failed socket before it is retired.
		if err != nil {
			c.unusable.Store(true)
		}
		return state, err
	}()
	if err != nil && c.unusable.Load() {
		// No registry lock is held. Retire only this exclusive connection; the
		// pool's existing tracked close/reader owners perform lifecycle cleanup.
		c.abort()
		if l.pool != nil {
			l.MarkBroken()
		}
		select {
		case <-c.readerDone:
		case <-ctx.Done():
			return state, errors.Join(err, context.Cause(ctx))
		}
		return state, errors.Join(err, c.closeFailure())
	}
	return state, err
}

type openAIWSConn struct {
	id        string
	accountID int64
	ws        openAIWSClientConn

	nativeHandshake        nativeWSHandshake
	handshakeHeaders       http.Header
	handshakeCompatibility openAIWSHandshakeCompatibilityKey
	routingAffinity        string

	leaseCh        chan struct{}
	closedCh       chan struct{}
	closeOnce      sync.Once
	abortOnce      sync.Once
	poolWatchOnce  sync.Once
	closeDone      chan struct{}
	readerDone     chan struct{}
	lifetimeCtx    context.Context
	cancelLifetime context.CancelFunc
	closeErrMu     sync.Mutex
	closeErr       error

	readMu  sync.Mutex
	writeMu openAIWSWriteGate

	// readerLoopResults 非 nil 表示池为该连接常驻了读循环：coder/websocket 只在
	// 阻塞读期间应答上游 ping，空闲连接没有读循环会被上游按保活超时关闭。
	readerLoopResults    chan []byte
	readerLoopErrMu      sync.Mutex
	readerLoopErr        error
	readerLoopPeerClosed atomic.Bool
	// onPeerClosed 由池在建连后设置：上游主动关闭时立刻把连接移出账号池，不等清理周期。
	onPeerClosed atomic.Pointer[func()]
	// unusable 表示空闲期收到数据被判为脏连接：持有令牌不再借出，由池在锁外关闭。
	unusable atomic.Bool

	waiters       atomic.Int32
	createdAtNano atomic.Int64
	lastUsedNano  atomic.Int64
	prewarmed     atomic.Bool
	// Any write can replace the server's most recent response on this socket,
	// including another thread or generate=false. Idle pings do not advance it.
	writeGeneration atomic.Uint64
	latestResponse  atomic.Pointer[openAIWSConnResponse]
}

type openAIWSConnResponse struct {
	responseID      string
	writeGeneration uint64
}

func (c *openAIWSConn) matchesResponseContext(responseID string) bool {
	if c == nil || c.isClosed() || c.unusable.Load() {
		return false
	}
	latest := c.latestResponse.Load()
	return latest != nil && latest.responseID == stringsTrim(responseID) &&
		latest.writeGeneration > 0 && latest.writeGeneration == c.writeGeneration.Load()
}

func newOpenAIWSConn(id string, accountID int64, ws openAIWSClientConn, handshakeHeaders http.Header) *openAIWSConn {
	now := time.Now()
	lifetime, cancel := context.WithCancel(context.Background())
	conn := &openAIWSConn{
		id:        id,
		accountID: accountID,
		closeDone: make(chan struct{}), readerDone: make(chan struct{}),
		lifetimeCtx: lifetime, cancelLifetime: cancel,
		ws:               ws,
		handshakeHeaders: cloneHeader(handshakeHeaders),
		leaseCh:          make(chan struct{}, 1),
		closedCh:         make(chan struct{}),
	}
	conn.leaseCh <- struct{}{}
	conn.createdAtNano.Store(now.UnixNano())
	conn.lastUsedNano.Store(now.UnixNano())
	if capable, ok := ws.(openAIWSReaderLoopCapable); ok && capable.RequiresReaderLoop() {
		conn.readerLoopResults = make(chan []byte, 1)
		go conn.runReaderLoop()
	} else {
		close(conn.readerDone)
	}
	return conn
}

func (c *openAIWSConn) runReaderLoop() {
	defer close(c.readerDone)
	defer close(c.readerLoopResults)
	for {
		payload, err := c.ws.ReadMessage(c.lifetimeCtx)
		if err != nil {
			c.readerLoopErrMu.Lock()
			c.readerLoopErr = err
			c.readerLoopErrMu.Unlock()
			// 本地主动关闭时对端会回 close 帧，同样以读错误结束循环，不算上游事件。
			peerClosed := false
			select {
			case <-c.closedCh:
			default:
				peerClosed = true
				c.readerLoopPeerClosed.Store(true)
				now := time.Now()
				// 空闲连接被上游断开是常态，池已当场出池，只记 info；借出中断开会影响请求，记 warn。
				logClosed := logOpenAIWSModeInfo
				if c.isLeased() {
					logClosed = logOpenAIWSModeWarn
				}
				logClosed(
					"conn_reader_loop_closed conn_id=%s leased=%v idle_ms=%d age_ms=%d upstream_pings=%d cause=%s",
					c.id,
					c.isLeased(),
					c.idleDuration(now).Milliseconds(),
					c.age(now).Milliseconds(),
					c.upstreamPingCount(),
					truncateOpenAIWSLogValue(err.Error(), openAIWSLogValueMaxLen),
				)
			}
			c.close()
			if evict := c.onPeerClosed.Load(); peerClosed && evict != nil {
				(*evict)()
			}
			return
		}
		select {
		case c.readerLoopResults <- payload:
		case <-c.closedCh:
			return
		}
	}
}

func (c *openAIWSConn) hasReaderLoop() bool {
	return c != nil && c.readerLoopResults != nil
}

func (c *openAIWSConn) readerLoopClosedByPeer() bool {
	return c != nil && c.readerLoopPeerClosed.Load()
}

func (c *openAIWSConn) upstreamPingCount() int64 {
	if c == nil || c.ws == nil {
		return 0
	}
	if counter, ok := c.ws.(openAIWSUpstreamPingCounter); ok {
		return counter.UpstreamPingCount()
	}
	return 0
}

// readerLoopPending 报告空闲期是否已有数据消息被读循环缓存。len 不消费消息；
// 缓存满时读循环阻塞在投递上，因此最多只有这一条待接管消息。
func (c *openAIWSConn) readerLoopPending() bool {
	return c.hasReaderLoop() && len(c.readerLoopResults) > 0
}

func (c *openAIWSConn) readerLoopError() error {
	c.readerLoopErrMu.Lock()
	defer c.readerLoopErrMu.Unlock()
	if c.readerLoopErr != nil {
		return c.readerLoopErr
	}
	return errOpenAIWSConnClosed
}

// leaseTokenUsable 在拿到租约令牌后确认连接仍可借出：已关闭的连接退回令牌；
// 空闲期收到过数据消息的连接状态已不可信，直接关闭而不交给借用者。
func (c *openAIWSConn) leaseTokenUsable() bool {
	select {
	case <-c.closedCh:
		c.release()
		return false
	default:
	}
	if c.readerLoopPending() {
		// 只记事件类型，不记报文原文，避免模型输出进日志。
		eventType := ""
		select {
		case payload := <-c.readerLoopResults:
			eventType = effectiveOpenAISSEEventType(payload, "")
		default:
		}
		logOpenAIWSModeWarn(
			"conn_idle_dirty_discard conn_id=%s idle_ms=%d upstream_pings=%d event=%s",
			c.id,
			c.idleDuration(time.Now()).Milliseconds(),
			c.upstreamPingCount(),
			normalizeOpenAIWSLogValue(eventType),
		)
		// 关闭握手可能阻塞到一个 RTT，而 tryAcquire 在池锁内调用，这里只标记，出池后再关闭。
		c.unusable.Store(true)
		return false
	}
	return true
}

func (c *openAIWSConn) isClosed() bool {
	if c == nil {
		return true
	}
	select {
	case <-c.closedCh:
		return true
	default:
		return false
	}
}

func (c *openAIWSConn) isUnusable() bool {
	return c != nil && c.unusable.Load()
}

func (c *openAIWSConn) tryAcquire() bool {
	if c == nil {
		return false
	}
	select {
	case <-c.closedCh:
		return false
	default:
	}
	select {
	case <-c.leaseCh:
		return c.leaseTokenUsable()
	default:
		return false
	}
}

func (c *openAIWSConn) acquire(ctx context.Context) error {
	if c == nil {
		return errOpenAIWSConnClosed
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.closedCh:
			return errOpenAIWSConnClosed
		case <-c.leaseCh:
			// A cancellation and a lease delivery can become ready together. Once
			// the semaphore token has been consumed, check the context again and
			// return it before reporting cancellation so a canceled waiter cannot
			// strand a pooled connection.
			if err := ctx.Err(); err != nil {
				c.release()
				return err
			}
			if !c.leaseTokenUsable() {
				return errOpenAIWSConnClosed
			}
			return nil
		}
	}
}

// acquireOrPoolChanged 与 acquire 相同，但同时监听账号池的变更信号：
// 别的连接释放、连接被剔除或新拨号完成都会触发它，此时返回 errOpenAIWSPoolChanged，
// 调用方应放弃只等这一条连接，回到选择逻辑重新挑选。
func (c *openAIWSConn) acquireOrPoolChanged(ctx context.Context, poolChanged <-chan struct{}) error {
	if c == nil {
		return errOpenAIWSConnClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closedCh:
		return errOpenAIWSConnClosed
	case <-poolChanged:
		return errOpenAIWSPoolChanged
	case <-c.leaseCh:
		if err := ctx.Err(); err != nil {
			c.release()
			return err
		}
		if !c.leaseTokenUsable() {
			return errOpenAIWSConnClosed
		}
		return nil
	}
}

func (c *openAIWSConn) release() {
	if c == nil {
		return
	}
	select {
	case c.leaseCh <- struct{}{}:
	default:
	}
	c.touch()
}

func (c *openAIWSConn) close() {
	c.closeWith(false)
}

// abort 不做关闭握手直接切断。读循环常驻持有读锁，礼貌关闭要等对端回 close 帧，
// 对端已不响应时库会等满 5 秒；读超时这类场景必须立即返回。
func (c *openAIWSConn) abort() {
	c.closeWith(true)
}

func (c *openAIWSConn) closeWith(force bool) {
	if c == nil {
		return
	}
	// Escalate a graceful close already waiting on the peer without waiting
	// behind closeOnce. Pool-owned close tasks join both calls before success.
	if force && c.isClosed() {
		c.cancelLifetime()
		select {
		case <-c.closeDone:
			return
		default:
		}
		if closer, ok := c.ws.(openAIWSForceCloser); ok {
			c.abortOnce.Do(func() { c.recordCloseError(closer.CloseNow()) })
		}
	}
	c.closeOnce.Do(func() {
		defer close(c.closeDone)
		close(c.closedCh)
		defer c.cancelLifetime()
		if force {
			c.cancelLifetime()
		}
		if c.ws != nil {
			if forceCloser, ok := c.ws.(openAIWSForceCloser); ok && force {
				c.abortOnce.Do(func() { c.recordCloseError(forceCloser.CloseNow()) })
			} else {
				c.recordCloseError(c.ws.Close())
			}
		}
		select {
		case c.leaseCh <- struct{}{}:
		default:
		}
	})
}

func (c *openAIWSConn) recordCloseError(err error) {
	if err == nil {
		return
	}
	c.closeErrMu.Lock()
	c.closeErr = errors.Join(c.closeErr, err)
	c.closeErrMu.Unlock()
}

func (c *openAIWSConn) closeFailure() error {
	c.closeErrMu.Lock()
	defer c.closeErrMu.Unlock()
	return c.closeErr
}

func (c *openAIWSConn) writeJSONWithTimeout(parent context.Context, value any, timeout time.Duration, owners ...*openAIWSConnLease) error {
	if c == nil {
		return errOpenAIWSConnClosed
	}
	select {
	case <-c.closedCh:
		return errOpenAIWSConnClosed
	default:
	}

	writeCtx := parent
	if writeCtx == nil {
		writeCtx = context.Background()
	}
	if timeout <= 0 {
		return c.writeJSON(value, writeCtx, owners...)
	}
	var cancel context.CancelFunc
	writeCtx, cancel = context.WithTimeout(writeCtx, timeout)
	defer cancel()
	return c.writeJSON(value, writeCtx, owners...)
}

func (c *openAIWSConn) writeJSON(value any, writeCtx context.Context, owners ...*openAIWSConnLease) error {
	if writeCtx == nil {
		writeCtx = context.Background()
	}
	if err := c.writeMu.lock(writeCtx); err != nil {
		return err
	}
	defer c.writeMu.unlock()
	// A queued control frame belongs to the original exclusive lease. Release
	// and a new lane may have won the gate before this writer resumed.
	for _, owner := range owners {
		if owner == nil || owner.conn != c || owner.released.Load() {
			return errOpenAIWSConnClosed
		}
	}
	select {
	case <-c.closedCh:
		return errOpenAIWSConnClosed
	default:
	}
	if c.ws == nil {
		return errOpenAIWSConnClosed
	}
	if writeCtx == nil {
		writeCtx = context.Background()
	}
	payload, err := json.Marshal(value)
	if raw, ok := value.(json.RawMessage); ok {
		payload = raw
	}
	if err != nil {
		return err
	}
	create, err := nativeWSIsCreate(payload)
	if err != nil {
		return err
	}
	if create {
		return native.ErrInvalidWSPreparation
	}
	c.writeGeneration.Add(1)
	if err := c.ws.WriteJSON(writeCtx, value); err != nil {
		return err
	}
	c.touch()
	return nil
}

func (c *openAIWSConn) readMessageWithTimeout(timeout time.Duration) ([]byte, error) {
	return c.readMessageWithContextTimeout(context.Background(), timeout)
}

func (c *openAIWSConn) readMessageWithContextTimeout(parent context.Context, timeout time.Duration) ([]byte, error) {
	if c == nil {
		return nil, errOpenAIWSConnClosed
	}
	// 有读循环时连接关闭后缓冲里可能还有未取走的消息，交给 readMessage 先排空再报错。
	if c.readerLoopResults == nil {
		select {
		case <-c.closedCh:
			return nil, errOpenAIWSConnClosed
		default:
		}
	}

	if parent == nil {
		parent = context.Background()
	}
	if timeout <= 0 {
		return c.readMessage(parent)
	}
	readCtx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return c.readMessage(readCtx)
}

func (c *openAIWSConn) readMessage(readCtx context.Context) ([]byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if c.ws == nil {
		return nil, errOpenAIWSConnClosed
	}
	if readCtx == nil {
		readCtx = context.Background()
	}
	if c.readerLoopResults == nil {
		payload, err := c.ws.ReadMessage(readCtx)
		if err != nil {
			return nil, err
		}
		c.touch()
		return payload, nil
	}
	select {
	case payload, ok := <-c.readerLoopResults:
		if !ok {
			return nil, c.readerLoopError()
		}
		c.touch()
		return payload, nil
	case <-readCtx.Done():
		// 与库在 ctx 取消时切断连接的语义一致：读超时后消息边界已不可信，且对端多半
		// 已不响应，直接切断而不做关闭握手。
		c.abort()
		return nil, readCtx.Err()
	}
}

func (c *openAIWSConn) pingWithTimeout(timeout time.Duration) error {
	if c == nil {
		return errOpenAIWSConnClosed
	}
	select {
	case <-c.closedCh:
		return errOpenAIWSConnClosed
	default:
	}

	// coder/websocket 除 Reader/Read 外的方法都可并发调用，控制帧由库内 writeFrameMu 串行化，
	// 这里不持有 writeMu，避免等 pong 期间阻塞借用者写请求。
	if c.ws == nil {
		return errOpenAIWSConnClosed
	}
	if timeout <= 0 {
		timeout = openAIWSConnHealthCheckTO
	}
	pingCtx, cancel := context.WithTimeout(c.lifetimeCtx, timeout)
	defer cancel()
	if err := c.ws.Ping(pingCtx); err != nil {
		return err
	}
	return nil
}

func (c *openAIWSConn) supportsIdlePingWithoutReader() bool {
	if c == nil || c.ws == nil {
		return false
	}
	if c.readerLoopResults != nil {
		return true
	}
	capable, ok := c.ws.(openAIWSIdlePingCapable)
	// Test and alternate implementations keep the historical probe behavior
	// unless they explicitly declare it unsafe.
	return !ok || capable.SupportsIdlePingWithoutReader()
}

func (c *openAIWSConn) touch() {
	if c == nil {
		return
	}
	c.lastUsedNano.Store(time.Now().UnixNano())
}

func (c *openAIWSConn) createdAt() time.Time {
	if c == nil {
		return time.Time{}
	}
	nano := c.createdAtNano.Load()
	if nano <= 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

func (c *openAIWSConn) lastUsedAt() time.Time {
	if c == nil {
		return time.Time{}
	}
	nano := c.lastUsedNano.Load()
	if nano <= 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

func (c *openAIWSConn) idleDuration(now time.Time) time.Duration {
	if c == nil {
		return 0
	}
	last := c.lastUsedAt()
	if last.IsZero() {
		return 0
	}
	return now.Sub(last)
}

func (c *openAIWSConn) age(now time.Time) time.Duration {
	if c == nil {
		return 0
	}
	created := c.createdAt()
	if created.IsZero() {
		return 0
	}
	return now.Sub(created)
}

func (c *openAIWSConn) isLeased() bool {
	if c == nil {
		return false
	}
	return len(c.leaseCh) == 0
}

func (c *openAIWSConn) handshakeHeader(name string) string {
	if c == nil || c.handshakeHeaders == nil {
		return ""
	}
	return strings.TrimSpace(c.handshakeHeaders.Get(strings.TrimSpace(name)))
}

func (c *openAIWSConn) matchesHandshakeCompatibility(compatibility openAIWSHandshakeCompatibilityKey) bool {
	return c != nil && c.handshakeCompatibility == compatibility
}

func (c *openAIWSConn) matchesRoutingAffinity(routingAffinity string) bool {
	return c != nil && c.routingAffinity == routingAffinity
}

func (c *openAIWSConn) isPrewarmed() bool {
	if c == nil {
		return false
	}
	return c.prewarmed.Load()
}

func (c *openAIWSConn) markPrewarmed() {
	if c == nil {
		return
	}
	c.prewarmed.Store(true)
}

type openAIWSAccountPool struct {
	mu            sync.Mutex
	conns         map[string]*openAIWSConn
	pinnedConns   map[string]int
	changedCh     chan struct{}
	creating      int
	generation    uint64
	lastCleanupAt time.Time
	lastAcquire   *openAIWSAcquireRequest
	prewarmActive bool
	prewarmUntil  time.Time
	prewarmFails  int
	prewarmFailAt time.Time
}

func (ap *openAIWSAccountPool) changeChannelLocked() chan struct{} {
	if ap.changedCh == nil {
		ap.changedCh = make(chan struct{})
	}
	return ap.changedCh
}

func (ap *openAIWSAccountPool) signalChangedLocked() {
	if ap == nil {
		return
	}
	if ap.changedCh != nil {
		close(ap.changedCh)
	}
	ap.changedCh = make(chan struct{})
}

type OpenAIWSPoolMetricsSnapshot struct {
	AcquireTotal            int64
	AcquireReuseTotal       int64
	AcquireCreateTotal      int64
	AcquireQueueWaitTotal   int64
	AcquireQueueWaitMsTotal int64
	ConnPickTotal           int64
	ConnPickMsTotal         int64
	ScaleUpTotal            int64
	ScaleDownTotal          int64
}

type openAIWSPoolMetrics struct {
	acquireTotal          atomic.Int64
	acquireReuseTotal     atomic.Int64
	acquireCreateTotal    atomic.Int64
	acquireQueueWaitTotal atomic.Int64
	acquireQueueWaitMs    atomic.Int64
	connPickTotal         atomic.Int64
	connPickMs            atomic.Int64
	scaleUpTotal          atomic.Int64
	scaleDownTotal        atomic.Int64
}

type openAIWSConnPool struct {
	cfg *config.Config
	// 通过接口解耦底层 WS 客户端实现，默认使用 coder/websocket。
	clientDialer openAIWSClientDialer

	accounts sync.Map // key: int64(accountID), value: *openAIWSAccountPool
	seq      atomic.Uint64

	metrics openAIWSPoolMetrics

	workerStopCh chan struct{}
	workerWg     sync.WaitGroup
	lifecycle    openAIWSPoolLifecycle
}

func newOpenAIWSConnPool(cfg *config.Config) *openAIWSConnPool {
	pool := &openAIWSConnPool{
		cfg:          cfg,
		clientDialer: newDefaultOpenAIWSClientDialer(),
		workerStopCh: make(chan struct{}),
	}
	pool.startBackgroundWorkers()
	return pool
}

func (p *openAIWSConnPool) SnapshotMetrics() OpenAIWSPoolMetricsSnapshot {
	if p == nil {
		return OpenAIWSPoolMetricsSnapshot{}
	}
	return OpenAIWSPoolMetricsSnapshot{
		AcquireTotal:            p.metrics.acquireTotal.Load(),
		AcquireReuseTotal:       p.metrics.acquireReuseTotal.Load(),
		AcquireCreateTotal:      p.metrics.acquireCreateTotal.Load(),
		AcquireQueueWaitTotal:   p.metrics.acquireQueueWaitTotal.Load(),
		AcquireQueueWaitMsTotal: p.metrics.acquireQueueWaitMs.Load(),
		ConnPickTotal:           p.metrics.connPickTotal.Load(),
		ConnPickMsTotal:         p.metrics.connPickMs.Load(),
		ScaleUpTotal:            p.metrics.scaleUpTotal.Load(),
		ScaleDownTotal:          p.metrics.scaleDownTotal.Load(),
	}
}

func (p *openAIWSConnPool) SnapshotTransportMetrics() OpenAIWSTransportMetricsSnapshot {
	if p == nil {
		return OpenAIWSTransportMetricsSnapshot{}
	}
	if dialer, ok := p.clientDialer.(openAIWSTransportMetricsDialer); ok {
		return dialer.SnapshotTransportMetrics()
	}
	return OpenAIWSTransportMetricsSnapshot{}
}

func (p *openAIWSConnPool) setClientDialerForTest(dialer openAIWSClientDialer) {
	if p == nil || dialer == nil {
		return
	}
	p.clientDialer = dialer
}

// Close is the bounded legacy shutdown entry. Root lifecycle uses Stop/Drain
// so failure to join is returned to the shutdown coordinator.
func (p *openAIWSConnPool) Close() {
	if p == nil {
		return
	}
	p.closeLegacy()
}

func (p *openAIWSConnPool) startBackgroundWorkers() {
	if p == nil || p.workerStopCh == nil {
		return
	}
	p.lifecycle.mu.Lock()
	p.initLifecycleLocked()
	if p.lifecycle.state != openAIWSPoolRunning {
		p.lifecycle.mu.Unlock()
		return
	}
	p.workerWg.Add(2)
	p.lifecycle.mu.Unlock()
	go func() {
		defer p.workerWg.Done()
		p.runBackgroundPingWorker()
	}()
	go func() {
		defer p.workerWg.Done()
		p.runBackgroundCleanupWorker()
	}()
}

type openAIWSIdlePingCandidate struct {
	accountID int64
	conn      *openAIWSConn
}

func (p *openAIWSConnPool) runBackgroundPingWorker() {
	if p == nil {
		return
	}
	ticker := time.NewTicker(openAIWSBackgroundPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.runBackgroundPingSweep()
		case <-p.workerStopCh:
			return
		}
	}
}

func (p *openAIWSConnPool) runBackgroundPingSweep() {
	if p == nil {
		return
	}
	candidates := p.snapshotIdleConnsForPing()
	var g errgroup.Group
	g.SetLimit(10)
	for _, item := range candidates {
		item := item
		if item.conn == nil || item.conn.isLeased() || item.conn.waiters.Load() > 0 || !item.conn.supportsIdlePingWithoutReader() {
			continue
		}
		g.Go(func() error {
			started := time.Now()
			idleMs := item.conn.idleDuration(started).Milliseconds()
			if err := item.conn.pingWithTimeout(openAIWSProbePingTO); err != nil {
				// 只有拿到租约令牌才能剔除：判断与占有必须是同一个原子动作，否则
				// 等 pong 期间刚借出的连接会被从借用者手里关掉。已关闭或已判脏的连接
				// 没有借用者，照常剔除。
				if !item.conn.tryAcquire() && !item.conn.isClosed() && !item.conn.isUnusable() {
					logOpenAIWSModeWarn(
						"conn_background_ping_skip_leased conn_id=%s idle_ms=%d upstream_pings=%d cause=%s",
						item.conn.id,
						idleMs,
						item.conn.upstreamPingCount(),
						truncateOpenAIWSLogValue(err.Error(), openAIWSLogValueMaxLen),
					)
					return nil
				}
				logOpenAIWSModeWarn(
					"conn_background_ping_evict conn_id=%s idle_ms=%d upstream_pings=%d cause=%s",
					item.conn.id,
					idleMs,
					item.conn.upstreamPingCount(),
					truncateOpenAIWSLogValue(err.Error(), openAIWSLogValueMaxLen),
				)
				p.evictConn(item.accountID, item.conn.id)
				return nil
			}
			logOpenAIWSModeDebug(
				"conn_background_ping_ok conn_id=%s idle_ms=%d rtt_ms=%d upstream_pings=%d",
				item.conn.id,
				idleMs,
				time.Since(started).Milliseconds(),
				item.conn.upstreamPingCount(),
			)
			return nil
		})
	}
	_ = g.Wait()
}

func (p *openAIWSConnPool) snapshotIdleConnsForPing() []openAIWSIdlePingCandidate {
	if p == nil {
		return nil
	}
	candidates := make([]openAIWSIdlePingCandidate, 0)
	p.accounts.Range(func(key, value any) bool {
		accountID, ok := key.(int64)
		if !ok || accountID <= 0 {
			return true
		}
		ap, ok := value.(*openAIWSAccountPool)
		if !ok || ap == nil {
			return true
		}
		ap.mu.Lock()
		for _, conn := range ap.conns {
			if conn == nil || conn.isLeased() || conn.waiters.Load() > 0 {
				continue
			}
			candidates = append(candidates, openAIWSIdlePingCandidate{
				accountID: accountID,
				conn:      conn,
			})
		}
		ap.mu.Unlock()
		return true
	})
	return candidates
}

func (p *openAIWSConnPool) runBackgroundCleanupWorker() {
	if p == nil {
		return
	}
	ticker := time.NewTicker(openAIWSBackgroundSweepTicker)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.runBackgroundCleanupSweep(time.Now())
		case <-p.workerStopCh:
			return
		}
	}
}

func (p *openAIWSConnPool) runBackgroundCleanupSweep(now time.Time) {
	if p == nil {
		return
	}
	type cleanupResult struct {
		evicted []*openAIWSConn
	}
	results := make([]cleanupResult, 0)
	p.accounts.Range(func(_ any, value any) bool {
		ap, ok := value.(*openAIWSAccountPool)
		if !ok || ap == nil {
			return true
		}
		maxConns := p.maxConnsHardCap()
		ap.mu.Lock()
		if ap.lastAcquire != nil && ap.lastAcquire.Account != nil {
			maxConns = p.effectiveMaxConnsByAccount(ap.lastAcquire.Account)
		}
		evicted := p.cleanupAccountLocked(ap, now, maxConns)
		ap.lastCleanupAt = now
		ap.mu.Unlock()
		if len(evicted) > 0 {
			results = append(results, cleanupResult{evicted: evicted})
		}
		return true
	})
	for _, result := range results {
		closeOpenAIWSConns(result.evicted)
	}
}

// openAIWSAcquireQueueWait 跨广播重选与递归重试累计一次获取的排队耗时，
// 由 Acquire 在统一出口写入租约与指标，任何成功路径都不会漏记。
type openAIWSAcquireQueueWait struct {
	queued  bool
	rewoken bool
	total   time.Duration
}

func (p *openAIWSConnPool) Acquire(ctx context.Context, req openAIWSAcquireRequest) (*openAIWSConnLease, error) {
	ctx, end, beginErr := p.beginPoolWork(ctx)
	if beginErr != nil {
		return nil, beginErr
	}
	defer end()
	if p != nil {
		p.metrics.acquireTotal.Add(1)
	}
	req = cloneOpenAIWSAcquireRequest(req)
	if req.RequiredResponseID != "" {
		if req.ForceNewConn || req.PreferredConnID == "" {
			return nil, errors.Join(ErrOpenAIContinuationUnavailable, errOpenAIWSResponseContextUnavailable)
		}
		req.ForcePreferredConn = true
	}
	queueWait := &openAIWSAcquireQueueWait{}
	lease, err := p.acquire(ctx, req, 0, queueWait)
	if req.RequiredResponseID != "" {
		if err != nil && errors.Is(err, errOpenAIWSPreferredConnUnavailable) {
			return nil, errors.Join(ErrOpenAIContinuationUnavailable, errOpenAIWSResponseContextUnavailable, err)
		}
		// This covers both immediate and queued acquisition. The socket may
		// advance while waiting, even if its ID and routing scope are unchanged.
		if lease != nil && !lease.HasResponseContext(req.RequiredResponseID) {
			lease.Release()
			return nil, errors.Join(ErrOpenAIContinuationUnavailable, errOpenAIWSResponseContextUnavailable)
		}
		if lease != nil {
			// Retain the actual receipt across later preparation/writer waits.
			// Every physical write on this lease checks it even if a caller
			// does not need the held-multi-turn context binding.
			lease.requiredResponse.Store(lease.conn.latestResponse.Load())
		}
	}
	if err != nil && errors.Is(context.Cause(ctx), errOpenAIWSPoolDraining) {
		return nil, errOpenAIWSPoolDraining
	}
	if lease != nil {
		p.lifecycle.mu.Lock()
		running := p.lifecycle.state == openAIWSPoolRunning
		p.lifecycle.mu.Unlock()
		if !running || ctx.Err() != nil {
			lease.Release()
			if !running {
				return nil, errOpenAIWSPoolDraining
			}
			return nil, context.Cause(ctx)
		}
	}
	if lease != nil && queueWait.rewoken {
		// 广播重选经 tryAcquire 拿令牌，不像排队分支那样在取得令牌后检查取消，
		// 这里补上复查：上下文已取消就归还令牌并按取消返回。
		if ctxErr := ctx.Err(); ctxErr != nil {
			lease.Release()
			return nil, ctxErr
		}
	}
	if lease != nil && queueWait.total > 0 {
		lease.queueWait = queueWait.total
		p.metrics.acquireQueueWaitMs.Add(queueWait.total.Milliseconds())
	}
	if lease != nil && lease.conn != nil {
		now := time.Now()
		lease.idleBefore = lease.conn.idleDuration(now)
		lease.ageBefore = lease.conn.age(now)
	}
	return lease, err
}

func (p *openAIWSConnPool) acquire(ctx context.Context, req openAIWSAcquireRequest, retry int, queueWait *openAIWSAcquireQueueWait) (*openAIWSConnLease, error) {
	if p == nil || req.Account == nil || req.Account.ID <= 0 {
		return nil, errors.New("invalid ws acquire request")
	}
	if stringsTrim(req.WSURL) == "" {
		return nil, errors.New("ws url is empty")
	}
	if queueWait == nil {
		queueWait = &openAIWSAcquireQueueWait{}
	}

retryAcquire:
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	accountID := req.Account.ID
	compatibility := normalizeOpenAIWSAcquireCompatibility(req, req.Headers)
	routingAffinity := normalizeOpenAIWSRoutingAffinity(req.Headers)
	effectiveMaxConns := p.effectiveMaxConnsByAccount(req.Account)
	if effectiveMaxConns <= 0 {
		return nil, errOpenAIWSConnQueueFull
	}
	var evicted []*openAIWSConn
	ap := p.getOrCreateAccountPool(accountID)
	ap.mu.Lock()
	acquireGeneration := ap.generation
	now := time.Now()
	if ap.lastCleanupAt.IsZero() || now.Sub(ap.lastCleanupAt) >= openAIWSAcquireCleanupInterval {
		evicted = p.cleanupAccountLocked(ap, now, effectiveMaxConns)
		ap.lastCleanupAt = now
	}
	pickStartedAt := time.Now()
	allowReuse := !req.ForceNewConn
	preferredConnID := stringsTrim(req.PreferredConnID)
	forcePreferredConn := allowReuse && req.ForcePreferredConn

	if allowReuse {
		if forcePreferredConn {
			if preferredConnID == "" {
				p.recordConnPickDuration(time.Since(pickStartedAt))
				ap.mu.Unlock()
				closeOpenAIWSConns(evicted)
				return nil, errOpenAIWSPreferredConnUnavailable
			}
			preferredConn, ok := ap.conns[preferredConnID]
			if !ok || !preferredConn.matchesHandshakeCompatibility(compatibility) {
				p.recordConnPickDuration(time.Since(pickStartedAt))
				ap.mu.Unlock()
				closeOpenAIWSConns(evicted)
				return nil, errOpenAIWSPreferredConnUnavailable
			}
			if preferredConn.tryAcquire() {
				connPick := time.Since(pickStartedAt)
				p.recordConnPickDuration(connPick)
				ap.mu.Unlock()
				closeOpenAIWSConns(evicted)
				if p.shouldHealthCheckConn(preferredConn) {
					if err := preferredConn.pingWithTimeout(openAIWSConnHealthCheckTO); err != nil {
						preferredConn.close()
						p.evictConn(accountID, preferredConn.id)
						if retry < 1 {
							return p.acquire(ctx, req, retry+1, queueWait)
						}
						return nil, err
					}
				}
				lease := &openAIWSConnLease{
					pool:      p,
					accountID: accountID,
					conn:      preferredConn,
					connPick:  connPick,
					reused:    true,
				}
				p.metrics.acquireReuseTotal.Add(1)
				p.recordLastSuccessfulAcquire(accountID, acquireGeneration, req)
				p.ensureTargetIdleAsync(accountID)
				return lease, nil
			}

			if p.dropDeadConnLocked(ap, preferredConn, &evicted) {
				p.recordConnPickDuration(time.Since(pickStartedAt))
				ap.mu.Unlock()
				closeOpenAIWSConns(evicted)
				return nil, errOpenAIWSPreferredConnUnavailable
			}
			connPick := time.Since(pickStartedAt)
			p.recordConnPickDuration(connPick)
			if int(preferredConn.waiters.Load()) >= p.queueLimitPerConn() {
				ap.mu.Unlock()
				closeOpenAIWSConns(evicted)
				return nil, errOpenAIWSConnQueueFull
			}
			preferredConn.waiters.Add(1)
			ap.mu.Unlock()
			closeOpenAIWSConns(evicted)
			defer preferredConn.waiters.Add(-1)
			waitStart := time.Now()
			p.metrics.acquireQueueWaitTotal.Add(1)

			if err := preferredConn.acquire(ctx); err != nil {
				if errors.Is(err, errOpenAIWSConnClosed) {
					p.evictConn(accountID, preferredConn.id)
					if retry < 1 {
						return p.acquire(ctx, req, retry+1, queueWait)
					}
				}
				return nil, err
			}
			if p.shouldHealthCheckConn(preferredConn) {
				if err := preferredConn.pingWithTimeout(openAIWSConnHealthCheckTO); err != nil {
					preferredConn.release()
					preferredConn.close()
					p.evictConn(accountID, preferredConn.id)
					if retry < 1 {
						return p.acquire(ctx, req, retry+1, queueWait)
					}
					return nil, err
				}
			}

			queueWait := time.Since(waitStart)
			p.metrics.acquireQueueWaitMs.Add(queueWait.Milliseconds())
			lease := &openAIWSConnLease{
				pool:      p,
				accountID: accountID,
				conn:      preferredConn,
				queueWait: queueWait,
				connPick:  connPick,
				reused:    true,
			}
			p.metrics.acquireReuseTotal.Add(1)
			p.recordLastSuccessfulAcquire(accountID, acquireGeneration, req)
			p.ensureTargetIdleAsync(accountID)
			return lease, nil
		}

		if preferredConnID != "" {
			if conn, ok := ap.conns[preferredConnID]; ok && conn.matchesHandshakeCompatibility(compatibility) && conn.tryAcquire() {
				connPick := time.Since(pickStartedAt)
				p.recordConnPickDuration(connPick)
				ap.mu.Unlock()
				closeOpenAIWSConns(evicted)
				if p.shouldHealthCheckConn(conn) {
					if err := conn.pingWithTimeout(openAIWSConnHealthCheckTO); err != nil {
						conn.close()
						p.evictConn(accountID, conn.id)
						if retry < 1 {
							return p.acquire(ctx, req, retry+1, queueWait)
						}
						return nil, err
					}
				}
				lease := &openAIWSConnLease{pool: p, accountID: accountID, conn: conn, connPick: connPick, reused: true}
				p.metrics.acquireReuseTotal.Add(1)
				p.recordLastSuccessfulAcquire(accountID, acquireGeneration, req)
				p.ensureTargetIdleAsync(accountID)
				return lease, nil
			} else if conn, ok := ap.conns[preferredConnID]; ok {
				p.dropDeadConnLocked(ap, conn, &evicted)
			}
		}

		// A routing hint is advisory at WebSocket dial time. Prefer a pooled
		// connection whose handshake used the same hint, but do not make that
		// preference a continuation compatibility requirement.
		best := p.pickLeastBusyConnWithRoutingAffinityLocked(ap, compatibility, routingAffinity)
		if best != nil && best.tryAcquire() {
			connPick := time.Since(pickStartedAt)
			p.recordConnPickDuration(connPick)
			ap.mu.Unlock()
			closeOpenAIWSConns(evicted)
			if p.shouldHealthCheckConn(best) {
				if err := best.pingWithTimeout(openAIWSConnHealthCheckTO); err != nil {
					best.close()
					p.evictConn(accountID, best.id)
					if retry < 1 {
						return p.acquire(ctx, req, retry+1, queueWait)
					}
					return nil, err
				}
			}
			lease := &openAIWSConnLease{pool: p, accountID: accountID, conn: best, connPick: connPick, reused: true}
			p.metrics.acquireReuseTotal.Add(1)
			p.recordLastSuccessfulAcquire(accountID, acquireGeneration, req)
			p.ensureTargetIdleAsync(accountID)
			return lease, nil
		} else if best != nil {
			p.dropDeadConnLocked(ap, best, &evicted)
		}
		if routingAffinity == "" || len(ap.conns)+ap.creating >= effectiveMaxConns {
			for _, conn := range ap.conns {
				if conn == nil || conn == best || !conn.matchesHandshakeCompatibility(compatibility) {
					continue
				}
				if conn.tryAcquire() {
					connPick := time.Since(pickStartedAt)
					p.recordConnPickDuration(connPick)
					ap.mu.Unlock()
					closeOpenAIWSConns(evicted)
					if p.shouldHealthCheckConn(conn) {
						if err := conn.pingWithTimeout(openAIWSConnHealthCheckTO); err != nil {
							conn.close()
							p.evictConn(accountID, conn.id)
							if retry < 1 {
								return p.acquire(ctx, req, retry+1, queueWait)
							}
							return nil, err
						}
					}
					lease := &openAIWSConnLease{pool: p, accountID: accountID, conn: conn, connPick: connPick, reused: true}
					p.metrics.acquireReuseTotal.Add(1)
					p.recordLastSuccessfulAcquire(accountID, acquireGeneration, req)
					p.ensureTargetIdleAsync(accountID)
					return lease, nil
				}
				p.dropDeadConnLocked(ap, conn, &evicted)
			}
		}
	}

	if !req.ForceNewConn && len(ap.conns)+ap.creating >= effectiveMaxConns {
		affine := p.pickLeastBusyConnWithRoutingAffinityLocked(ap, compatibility, routingAffinity)
		if idle := p.pickOldestIdleConnWithoutHandshakeCompatibilityLocked(ap, compatibility); idle != nil {
			delete(ap.conns, idle.id)
			evicted = append(evicted, idle)
			p.metrics.scaleDownTotal.Add(1)
		} else if affine == nil {
			compatible := p.pickLeastBusyConnLocked(ap, "", compatibility)
			if compatible != nil {
				// Capacity is full and every compatible connection is busy. The
				// hint remains soft here: queue on a compatible connection below.
				goto acquireAtCapacity
			}
			hasConnection := false
			for _, conn := range ap.conns {
				if conn != nil {
					hasConnection = true
					break
				}
			}
			if !hasConnection && ap.creating == 0 {
				ap.mu.Unlock()
				closeOpenAIWSConns(evicted)
				return nil, errOpenAIWSConnClosed
			}
			changedCh := ap.changeChannelLocked()
			ap.mu.Unlock()
			closeOpenAIWSConns(evicted)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-changedCh:
				goto retryAcquire
			}
		}
	}

	if req.ForceNewConn && len(ap.conns)+ap.creating >= effectiveMaxConns {
		if idle := p.pickOldestIdleConnLocked(ap); idle != nil {
			delete(ap.conns, idle.id)
			evicted = append(evicted, idle)
			p.metrics.scaleDownTotal.Add(1)
		}
	}

	if len(ap.conns)+ap.creating < effectiveMaxConns {
		connPick := time.Since(pickStartedAt)
		p.recordConnPickDuration(connPick)
		ap.creating++
		ap.mu.Unlock()
		closeOpenAIWSConns(evicted)

		conn, dialErr := p.dialConn(ctx, req)

		ap = p.getOrCreateAccountPool(accountID)
		ap.mu.Lock()
		ap.creating--
		if ap.generation != acquireGeneration {
			ap.signalChangedLocked()
			ap.mu.Unlock()
			if conn != nil {
				conn.close()
			}
			if retry < 1 {
				return p.acquire(ctx, req, retry+1, queueWait)
			}
			return nil, errOpenAIWSConnClosed
		}
		if dialErr != nil {
			ap.prewarmFails++
			ap.prewarmFailAt = time.Now()
			ap.signalChangedLocked()
			ap.mu.Unlock()
			return nil, dialErr
		}
		// Claim the freshly dialed connection before publishing it. Otherwise a
		// topology waiter awakened below can take the free semaphore first and
		// make the caller that paid for the dial queue behind it.
		if !conn.tryAcquire() {
			ap.signalChangedLocked()
			ap.mu.Unlock()
			conn.close()
			return nil, errOpenAIWSConnClosed
		}
		if !p.publishConnLocked(ap, conn) {
			ap.signalChangedLocked()
			ap.mu.Unlock()
			p.closeConnAsync(conn)
			return nil, errOpenAIWSPoolDraining
		}
		ap.prewarmFails = 0
		ap.prewarmFailAt = time.Time{}
		// Wake acquires that observed creating>0 with no compatible connection.
		// Without this signal they can remain asleep until the new lease is
		// released, even though the pool topology already changed.
		ap.signalChangedLocked()
		ap.mu.Unlock()
		p.metrics.acquireCreateTotal.Add(1)
		lease := &openAIWSConnLease{pool: p, accountID: accountID, conn: conn, connPick: connPick}
		p.recordLastSuccessfulAcquire(accountID, acquireGeneration, req)
		p.ensureTargetIdleAsync(accountID)
		return lease, nil
	}

	if req.ForceNewConn {
		p.recordConnPickDuration(time.Since(pickStartedAt))
		ap.mu.Unlock()
		closeOpenAIWSConns(evicted)
		return nil, errOpenAIWSConnQueueFull
	}

acquireAtCapacity:
	target := p.pickLeastBusyConnLocked(ap, req.PreferredConnID, compatibility)
	connPick := time.Since(pickStartedAt)
	p.recordConnPickDuration(connPick)
	if target == nil {
		ap.mu.Unlock()
		closeOpenAIWSConns(evicted)
		return nil, errOpenAIWSConnClosed
	}
	if int(target.waiters.Load()) >= p.queueLimitPerConn() {
		ap.mu.Unlock()
		closeOpenAIWSConns(evicted)
		return nil, errOpenAIWSConnQueueFull
	}
	target.waiters.Add(1)
	// 排队时不只等这一条连接的令牌：账号池任何容量变化（别的连接释放、被剔除、
	// 新拨号完成）都会唤醒等待者回到 retryAcquire 重新选择。变更通道必须在锁内取，
	// 否则会漏掉解锁到开始等待之间的信号。
	changedCh := ap.changeChannelLocked()
	ap.mu.Unlock()
	closeOpenAIWSConns(evicted)
	waitStart := time.Now()
	if !queueWait.queued {
		queueWait.queued = true
		p.metrics.acquireQueueWaitTotal.Add(1)
	}

	waitErr := target.acquireOrPoolChanged(ctx, changedCh)
	target.waiters.Add(-1)
	queueWait.total += time.Since(waitStart)
	if waitErr != nil {
		if errors.Is(waitErr, errOpenAIWSPoolChanged) {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			queueWait.rewoken = true
			goto retryAcquire
		}
		if errors.Is(waitErr, errOpenAIWSConnClosed) {
			p.evictConn(accountID, target.id)
			if retry < 1 {
				return p.acquire(ctx, req, retry+1, queueWait)
			}
		}
		return nil, waitErr
	}
	if p.shouldHealthCheckConn(target) {
		if err := target.pingWithTimeout(openAIWSConnHealthCheckTO); err != nil {
			target.release()
			target.close()
			p.evictConn(accountID, target.id)
			if retry < 1 {
				return p.acquire(ctx, req, retry+1, queueWait)
			}
			return nil, err
		}
	}

	lease := &openAIWSConnLease{pool: p, accountID: accountID, conn: target, connPick: connPick, reused: true}
	p.metrics.acquireReuseTotal.Add(1)
	p.recordLastSuccessfulAcquire(accountID, acquireGeneration, req)
	p.ensureTargetIdleAsync(accountID)
	return lease, nil
}

func (p *openAIWSConnPool) recordConnPickDuration(duration time.Duration) {
	if p == nil {
		return
	}
	if duration < 0 {
		duration = 0
	}
	p.metrics.connPickTotal.Add(1)
	p.metrics.connPickMs.Add(duration.Milliseconds())
}

func (p *openAIWSConnPool) recordLastSuccessfulAcquire(accountID int64, generation uint64, req openAIWSAcquireRequest) {
	if p == nil || accountID <= 0 {
		return
	}
	ap, ok := p.getAccountPool(accountID)
	if !ok || ap == nil {
		return
	}
	ap.mu.Lock()
	if ap.generation != generation {
		ap.mu.Unlock()
		return
	}
	if !p.poolRunning() {
		ap.mu.Unlock()
		return
	}

	ap.lastAcquire = cloneOpenAIWSAcquireRequestPtr(&req)
	ap.mu.Unlock()
}

func (p *openAIWSConnPool) pickOldestIdleConnLocked(ap *openAIWSAccountPool) *openAIWSConn {
	if ap == nil || len(ap.conns) == 0 {
		return nil
	}
	var oldest *openAIWSConn
	for _, conn := range ap.conns {
		if conn == nil || conn.isLeased() || conn.waiters.Load() > 0 || p.isConnPinnedLocked(ap, conn.id) {
			continue
		}
		if oldest == nil || conn.lastUsedAt().Before(oldest.lastUsedAt()) {
			oldest = conn
		}
	}
	return oldest
}

func (p *openAIWSConnPool) pickOldestIdleConnWithoutHandshakeCompatibilityLocked(
	ap *openAIWSAccountPool,
	compatibility openAIWSHandshakeCompatibilityKey,
) *openAIWSConn {
	if ap == nil || len(ap.conns) == 0 {
		return nil
	}
	var oldest *openAIWSConn
	for _, conn := range ap.conns {
		if conn == nil ||
			conn.matchesHandshakeCompatibility(compatibility) ||
			conn.isLeased() || conn.waiters.Load() > 0 || p.isConnPinnedLocked(ap, conn.id) {
			continue
		}
		if oldest == nil || conn.lastUsedAt().Before(oldest.lastUsedAt()) {
			oldest = conn
		}
	}
	return oldest
}

func (p *openAIWSConnPool) getOrCreateAccountPool(accountID int64) *openAIWSAccountPool {
	if p == nil || accountID <= 0 {
		return nil
	}
	if existing, ok := p.accounts.Load(accountID); ok {
		if ap, typed := existing.(*openAIWSAccountPool); typed && ap != nil {
			return ap
		}
	}
	ap := &openAIWSAccountPool{
		conns:       make(map[string]*openAIWSConn),
		pinnedConns: make(map[string]int),
		changedCh:   make(chan struct{}),
	}
	actual, _ := p.accounts.LoadOrStore(accountID, ap)
	if typed, ok := actual.(*openAIWSAccountPool); ok && typed != nil {
		return typed
	}
	return ap
}

// ensureAccountPoolLocked 兼容旧调用。
func (p *openAIWSConnPool) ensureAccountPoolLocked(accountID int64) *openAIWSAccountPool {
	return p.getOrCreateAccountPool(accountID)
}

func (p *openAIWSConnPool) getAccountPool(accountID int64) (*openAIWSAccountPool, bool) {
	if p == nil || accountID <= 0 {
		return nil, false
	}
	value, ok := p.accounts.Load(accountID)
	if !ok || value == nil {
		return nil, false
	}
	ap, typed := value.(*openAIWSAccountPool)
	return ap, typed && ap != nil
}

func (p *openAIWSConnPool) notifyAccountPoolChanged(accountID int64) {
	ap, ok := p.getAccountPool(accountID)
	if !ok || ap == nil {
		return
	}
	ap.mu.Lock()
	ap.signalChangedLocked()
	ap.mu.Unlock()
}

func (p *openAIWSConnPool) isConnPinnedLocked(ap *openAIWSAccountPool, connID string) bool {
	if ap == nil || connID == "" || len(ap.pinnedConns) == 0 {
		return false
	}
	return ap.pinnedConns[connID] > 0
}

func (p *openAIWSConnPool) cleanupAccountLocked(ap *openAIWSAccountPool, now time.Time, maxConns int) []*openAIWSConn {
	if ap == nil {
		return nil
	}
	maxAge := p.maxConnAge()

	evicted := make([]*openAIWSConn, 0)
	for id, conn := range ap.conns {
		if conn == nil {
			delete(ap.conns, id)
			if len(ap.pinnedConns) > 0 {
				delete(ap.pinnedConns, id)
			}
			continue
		}
		if conn.isClosed() || conn.isUnusable() {
			delete(ap.conns, id)
			if len(ap.pinnedConns) > 0 {
				delete(ap.pinnedConns, id)
			}
			evicted = append(evicted, conn)
			continue
		}
		if p.isConnPinnedLocked(ap, id) {
			continue
		}
		if !conn.isLeased() && conn.waiters.Load() == 0 &&
			!conn.supportsIdlePingWithoutReader() &&
			conn.idleDuration(now) >= openAIWSConnIdleRecycleAfter {
			delete(ap.conns, id)
			if len(ap.pinnedConns) > 0 {
				delete(ap.pinnedConns, id)
			}
			evicted = append(evicted, conn)
			p.metrics.scaleDownTotal.Add(1)
			continue
		}
		if maxAge > 0 && !conn.isLeased() && conn.age(now) > maxAge {
			delete(ap.conns, id)
			if len(ap.pinnedConns) > 0 {
				delete(ap.pinnedConns, id)
			}
			evicted = append(evicted, conn)
		}
	}

	if maxConns <= 0 {
		maxConns = p.maxConnsHardCap()
	}
	maxIdle := p.maxIdlePerAccount()
	if maxIdle < 0 || maxIdle > maxConns {
		maxIdle = maxConns
	}
	if maxIdle >= 0 && len(ap.conns) > maxIdle {
		idleConns := make([]*openAIWSConn, 0, len(ap.conns))
		for id, conn := range ap.conns {
			if conn == nil {
				delete(ap.conns, id)
				if len(ap.pinnedConns) > 0 {
					delete(ap.pinnedConns, id)
				}
				continue
			}
			// 有等待者的连接不能在清理阶段被淘汰，否则等待中的 acquire 会收到 closed 错误。
			if conn.isLeased() || conn.waiters.Load() > 0 || p.isConnPinnedLocked(ap, conn.id) {
				continue
			}
			idleConns = append(idleConns, conn)
		}
		sort.SliceStable(idleConns, func(i, j int) bool {
			return idleConns[i].lastUsedAt().Before(idleConns[j].lastUsedAt())
		})
		redundant := len(ap.conns) - maxIdle
		if redundant > len(idleConns) {
			redundant = len(idleConns)
		}
		for i := 0; i < redundant; i++ {
			conn := idleConns[i]
			delete(ap.conns, conn.id)
			if len(ap.pinnedConns) > 0 {
				delete(ap.pinnedConns, conn.id)
			}
			evicted = append(evicted, conn)
		}
		if redundant > 0 {
			p.metrics.scaleDownTotal.Add(int64(redundant))
		}
	}
	if len(evicted) > 0 {
		ap.signalChangedLocked()
	}

	return evicted
}

func (p *openAIWSConnPool) pickLeastBusyConnLocked(
	ap *openAIWSAccountPool,
	preferredConnID string,
	compatibility openAIWSHandshakeCompatibilityKey,
) *openAIWSConn {
	if ap == nil || len(ap.conns) == 0 {
		return nil
	}
	preferredConnID = stringsTrim(preferredConnID)
	if preferredConnID != "" {
		if conn, ok := ap.conns[preferredConnID]; ok && conn.matchesHandshakeCompatibility(compatibility) {
			return conn
		}
	}
	var best *openAIWSConn
	var bestWaiters int32
	var bestLastUsed time.Time
	for _, conn := range ap.conns {
		if conn == nil || !conn.matchesHandshakeCompatibility(compatibility) {
			continue
		}
		waiters := conn.waiters.Load()
		lastUsed := conn.lastUsedAt()
		if best == nil ||
			waiters < bestWaiters ||
			(waiters == bestWaiters && lastUsed.Before(bestLastUsed)) {
			best = conn
			bestWaiters = waiters
			bestLastUsed = lastUsed
		}
	}
	return best
}

func (p *openAIWSConnPool) pickLeastBusyConnWithRoutingAffinityLocked(
	ap *openAIWSAccountPool,
	compatibility openAIWSHandshakeCompatibilityKey,
	routingAffinity string,
) *openAIWSConn {
	if ap == nil || len(ap.conns) == 0 {
		return nil
	}
	var best *openAIWSConn
	var bestWaiters int32
	var bestLastUsed time.Time
	for _, conn := range ap.conns {
		if conn == nil ||
			!conn.matchesHandshakeCompatibility(compatibility) ||
			!conn.matchesRoutingAffinity(routingAffinity) {
			continue
		}
		waiters := conn.waiters.Load()
		lastUsed := conn.lastUsedAt()
		if best == nil ||
			waiters < bestWaiters ||
			(waiters == bestWaiters && lastUsed.Before(bestLastUsed)) {
			best = conn
			bestWaiters = waiters
			bestLastUsed = lastUsed
		}
	}
	return best
}

func accountPoolLoadLocked(ap *openAIWSAccountPool) (inflight int, waiters int) {
	if ap == nil {
		return 0, 0
	}
	for _, conn := range ap.conns {
		if conn == nil {
			continue
		}
		if conn.isLeased() {
			inflight++
		}
		waiters += int(conn.waiters.Load())
	}
	return inflight, waiters
}

// AccountPoolLoad 返回指定账号连接池的并发与排队快照。
func (p *openAIWSConnPool) AccountPoolLoad(accountID int64) (inflight int, waiters int, conns int) {
	if p == nil || accountID <= 0 {
		return 0, 0, 0
	}
	ap, ok := p.getAccountPool(accountID)
	if !ok || ap == nil {
		return 0, 0, 0
	}
	ap.mu.Lock()
	defer ap.mu.Unlock()
	inflight, waiters = accountPoolLoadLocked(ap)
	return inflight, waiters, len(ap.conns)
}

func (p *openAIWSConnPool) ensureTargetIdleAsync(accountID int64) {
	if p == nil || accountID <= 0 {
		return
	}

	var req openAIWSAcquireRequest
	generation := uint64(0)
	need := 0
	ap, ok := p.getAccountPool(accountID)
	if !ok || ap == nil {
		return
	}
	ap.mu.Lock()
	defer ap.mu.Unlock()
	if ap.lastAcquire == nil {
		return
	}
	if ap.prewarmActive {
		return
	}
	now := time.Now()
	if !ap.prewarmUntil.IsZero() && now.Before(ap.prewarmUntil) {
		return
	}
	if p.shouldSuppressPrewarmLocked(ap, now) {
		return
	}
	effectiveMaxConns := p.maxConnsHardCap()
	if ap.lastAcquire != nil && ap.lastAcquire.Account != nil {
		effectiveMaxConns = p.effectiveMaxConnsByAccount(ap.lastAcquire.Account)
	}
	target := p.targetConnCountLocked(ap, effectiveMaxConns)
	current := len(ap.conns) + ap.creating
	if current >= target {
		return
	}
	need = target - current
	if need <= 0 {
		return
	}
	workCtx, end, err := p.beginPoolWork(context.Background())
	if err != nil {
		return
	}
	req = cloneOpenAIWSAcquireRequest(*ap.lastAcquire)
	generation = ap.generation
	ap.prewarmActive = true
	if cooldown := p.prewarmCooldown(); cooldown > 0 {
		ap.prewarmUntil = now.Add(cooldown)
	}
	ap.creating += need
	p.metrics.scaleUpTotal.Add(int64(need))

	go func() { defer end(); p.prewarmConnsOwned(workCtx, accountID, req, need, generation) }()
}

func (p *openAIWSConnPool) targetConnCountLocked(ap *openAIWSAccountPool, maxConns int) int {
	if ap == nil {
		return 0
	}

	if maxConns <= 0 {
		return 0
	}

	minIdle := p.minIdlePerAccount()
	if minIdle < 0 {
		minIdle = 0
	}
	if minIdle > maxConns {
		minIdle = maxConns
	}

	inflight, waiters := accountPoolLoadLocked(ap)
	utilization := p.targetUtilization()
	demand := inflight + waiters
	if demand <= 0 {
		return minIdle
	}

	target := 1
	if demand > 1 {
		target = int(math.Ceil(float64(demand) / utilization))
	}
	if waiters > 0 && target < len(ap.conns)+1 {
		target = len(ap.conns) + 1
	}
	if target < minIdle {
		target = minIdle
	}
	if target > maxConns {
		target = maxConns
	}
	return target
}

func (p *openAIWSConnPool) prewarmConns(accountID int64, req openAIWSAcquireRequest, total int, generations ...uint64) {
	ctx, end, err := p.beginPoolWork(context.Background())
	if err != nil {
		return
	}
	defer end()
	p.prewarmConnsOwned(ctx, accountID, req, total, generations...)
}

func (p *openAIWSConnPool) prewarmConnsOwned(parent context.Context, accountID int64, req openAIWSAcquireRequest, total int, generations ...uint64) {
	generation := uint64(0)
	if len(generations) > 0 {
		generation = generations[0]
	}
	staleTarget := false
	remaining := total
	defer func() {
		if ap, ok := p.getAccountPool(accountID); ok && ap != nil {
			ap.mu.Lock()
			ap.creating = max(0, ap.creating-remaining)
			ap.prewarmActive = false
			ap.signalChangedLocked()
			ap.mu.Unlock()
		}
		if staleTarget {
			// A newer acquire arrived while the old dial was in flight. Re-run
			// target selection only after clearing prewarmActive so the latest
			// beta/hint target can fill the idle budget.
			p.ensureTargetIdleAsync(accountID)
		}
	}()

	for i := 0; i < total; i++ {
		if parent.Err() != nil {
			return
		}
		ctx, cancel := context.WithTimeout(parent, p.dialTimeout()+openAIWSConnPrewarmExtraDelay)
		conn, err := p.dialConn(ctx, req)
		cancel()

		ap, ok := p.getAccountPool(accountID)
		if !ok || ap == nil {
			if conn != nil {
				conn.close()
			}
			return
		}
		ap.mu.Lock()
		if ap.creating > 0 {
			ap.creating--
		}
		remaining--
		if err != nil {
			ap.prewarmFails++
			ap.prewarmFailAt = time.Now()
			ap.signalChangedLocked()
			ap.mu.Unlock()
			continue
		}
		if ap.generation != generation || ap.lastAcquire == nil {
			ap.mu.Unlock()
			conn.close()
			continue
		}
		if !sameOpenAIWSPrewarmTarget(req, *ap.lastAcquire) {
			staleTarget = true
			ap.signalChangedLocked()
			ap.mu.Unlock()
			conn.close()
			continue
		}
		if len(ap.conns) >= p.effectiveMaxConnsByAccount(req.Account) {
			ap.signalChangedLocked()
			ap.mu.Unlock()
			conn.close()
			continue
		}
		if !p.publishConnLocked(ap, conn) {
			ap.signalChangedLocked()
			ap.mu.Unlock()
			p.closeConnAsync(conn)
			return
		}
		ap.prewarmFails = 0
		ap.prewarmFailAt = time.Time{}
		ap.signalChangedLocked()
		ap.mu.Unlock()
	}
}

// ClearAccount closes all pooled connections and discards delayed prewarm
// state for one account. The generation guard prevents an in-flight prewarm
// started before credential recovery from re-entering the pool afterwards.
func (p *openAIWSConnPool) ClearAccount(accountID int64) {
	if p == nil || accountID <= 0 {
		return
	}
	ap, ok := p.getAccountPool(accountID)
	if !ok || ap == nil {
		return
	}
	ap.mu.Lock()
	ap.generation++
	conns := make([]*openAIWSConn, 0, len(ap.conns))
	for id, conn := range ap.conns {
		delete(ap.conns, id)
		delete(ap.pinnedConns, id)
		if conn != nil {
			conns = append(conns, conn)
		}
	}
	ap.lastAcquire = nil
	ap.prewarmUntil = time.Time{}
	ap.prewarmFails = 0
	ap.prewarmFailAt = time.Time{}
	ap.signalChangedLocked()
	ap.mu.Unlock()
	closeOpenAIWSConns(conns)
}

// dropDeadConnLocked 在池锁内把已关闭或已判脏的连接移出账号池并释放容量，
// 连接本身交给调用方在解锁后关闭。
func (p *openAIWSConnPool) dropDeadConnLocked(ap *openAIWSAccountPool, conn *openAIWSConn, evicted *[]*openAIWSConn) bool {
	if ap == nil || conn == nil || (!conn.isClosed() && !conn.isUnusable()) {
		return false
	}
	if _, exists := ap.conns[conn.id]; !exists {
		return false
	}
	delete(ap.conns, conn.id)
	if len(ap.pinnedConns) > 0 {
		delete(ap.pinnedConns, conn.id)
	}
	ap.signalChangedLocked()
	*evicted = append(*evicted, conn)
	return true
}

func (p *openAIWSConnPool) evictConn(accountID int64, connID string) {
	if p == nil || accountID <= 0 || stringsTrim(connID) == "" {
		return
	}
	var conn *openAIWSConn
	ap, ok := p.getAccountPool(accountID)
	if ok && ap != nil {
		ap.mu.Lock()
		if c, exists := ap.conns[connID]; exists {
			conn = c
			delete(ap.conns, connID)
			if len(ap.pinnedConns) > 0 {
				delete(ap.pinnedConns, connID)
			}
			ap.signalChangedLocked()
		}
		ap.mu.Unlock()
	}
	if conn != nil {
		conn.close()
	}
}

func (p *openAIWSConnPool) PinConn(accountID int64, connID string) bool {
	if p == nil || accountID <= 0 {
		return false
	}
	connID = stringsTrim(connID)
	if connID == "" {
		return false
	}
	ap, ok := p.getAccountPool(accountID)
	if !ok || ap == nil {
		return false
	}
	ap.mu.Lock()
	defer ap.mu.Unlock()
	if _, exists := ap.conns[connID]; !exists {
		return false
	}
	if ap.pinnedConns == nil {
		ap.pinnedConns = make(map[string]int)
	}
	ap.pinnedConns[connID]++
	return true
}

func (p *openAIWSConnPool) UnpinConn(accountID int64, connID string) {
	if p == nil || accountID <= 0 {
		return
	}
	connID = stringsTrim(connID)
	if connID == "" {
		return
	}
	ap, ok := p.getAccountPool(accountID)
	if !ok || ap == nil {
		return
	}
	ap.mu.Lock()
	defer ap.mu.Unlock()
	if len(ap.pinnedConns) == 0 {
		return
	}
	count := ap.pinnedConns[connID]
	if count <= 1 {
		delete(ap.pinnedConns, connID)
		ap.signalChangedLocked()
		return
	}
	ap.pinnedConns[connID] = count - 1
	ap.signalChangedLocked()
}

func (p *openAIWSConnPool) dialConn(ctx context.Context, req openAIWSAcquireRequest) (*openAIWSConn, error) {
	if p == nil || p.clientDialer == nil {
		return nil, errors.New("openai ws client dialer is nil")
	}
	ctx, end, beginErr := p.beginPoolWork(ctx)
	if beginErr != nil {
		return nil, beginErr
	}
	defer end()
	headers := cloneHeader(req.Headers)
	var err error
	if req.HeadersFactory != nil {
		headers, err = req.HeadersFactory(ctx, headers)
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	var handshakeFact nativeWSHandshake
	if req.HandshakeFactsFactory != nil {
		handshakeFact, err = req.HandshakeFactsFactory(ctx, headers)
		if err != nil {
			return nil, err
		}
	}
	conn, status, handshakeHeaders, err := p.clientDialer.Dial(ctx, req.WSURL, headers, req.ProxyURL)
	if conn != nil && err != nil {
		orphan := newOpenAIWSConn(p.nextConnID(req.Account.ID), req.Account.ID, conn, handshakeHeaders)
		p.trackConn(orphan)
		p.closeConnAsync(orphan)
	}
	if err != nil {
		var handshakeErr *openAIWSHandshakeError
		var responseBody []byte
		if errors.As(err, &handshakeErr) && handshakeErr != nil {
			responseBody = append([]byte(nil), handshakeErr.Body...)
		}
		return nil, &openAIWSDialError{
			StatusCode:      status,
			ResponseHeaders: cloneHeader(handshakeHeaders),
			ResponseBody:    responseBody,
			Err:             err,
		}
	}
	if conn == nil {
		return nil, &openAIWSDialError{
			StatusCode:      status,
			ResponseHeaders: cloneHeader(handshakeHeaders),
			Err:             errors.New("openai ws dialer returned nil connection"),
		}
	}
	id := p.nextConnID(req.Account.ID)
	pooledConn := newOpenAIWSConn(id, req.Account.ID, conn, handshakeHeaders)
	if req.HandshakeFactsFactory != nil {
		handshakeFact, err = completeNativeWSHandshake(handshakeFact, conn, headers)
		if err != nil {
			p.trackConn(pooledConn)
			p.closeConnAsync(pooledConn)
			return nil, err
		}
		pooledConn.nativeHandshake = handshakeFact
	}
	accountID := req.Account.ID
	evict := func() { p.evictConn(accountID, id) }
	pooledConn.onPeerClosed.Store(&evict)
	pooledConn.handshakeCompatibility = normalizeOpenAIWSAcquireCompatibility(req, req.Headers)
	pooledConn.routingAffinity = normalizeOpenAIWSRoutingAffinity(req.Headers)
	p.trackConn(pooledConn)
	if !p.poolRunning() || ctx.Err() != nil {
		p.closeConnAsync(pooledConn)
		if ctx.Err() != nil {
			return nil, context.Cause(ctx)
		}
		return nil, errOpenAIWSPoolDraining
	}
	return pooledConn, nil
}

func (p *openAIWSConnPool) nextConnID(accountID int64) string {
	seq := p.seq.Add(1)
	buf := make([]byte, 0, 32)
	buf = append(buf, "oa_ws_"...)
	buf = strconv.AppendInt(buf, accountID, 10)
	buf = append(buf, '_')
	buf = strconv.AppendUint(buf, seq, 10)
	return string(buf)
}

func (p *openAIWSConnPool) shouldHealthCheckConn(conn *openAIWSConn) bool {
	if conn == nil || !conn.supportsIdlePingWithoutReader() {
		return false
	}
	// 有读循环的连接能即时感知上游关闭，半开探测已由后台巡检覆盖，借出前不再多付一个往返。
	if conn.hasReaderLoop() {
		return false
	}
	return conn.idleDuration(time.Now()) >= openAIWSConnHealthCheckIdle
}

func (p *openAIWSConnPool) maxConnsHardCap() int {
	if p != nil && p.cfg != nil && p.cfg.Gateway.OpenAIWS.MaxConnsPerAccount > 0 {
		return p.cfg.Gateway.OpenAIWS.MaxConnsPerAccount
	}
	return 8
}

func (p *openAIWSConnPool) dynamicMaxConnsEnabled() bool {
	if p != nil && p.cfg != nil {
		return p.cfg.Gateway.OpenAIWS.DynamicMaxConnsByAccountConcurrencyEnabled
	}
	return false
}

func (p *openAIWSConnPool) modeRouterV2Enabled() bool {
	if p != nil && p.cfg != nil {
		return p.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled
	}
	return false
}

func (p *openAIWSConnPool) maxConnsFactorByAccount(account *Account) float64 {
	if p == nil || p.cfg == nil || account == nil {
		return 1.0
	}
	switch account.Type {
	case AccountTypeOAuth:
		if p.cfg.Gateway.OpenAIWS.OAuthMaxConnsFactor > 0 {
			return p.cfg.Gateway.OpenAIWS.OAuthMaxConnsFactor
		}
	case AccountTypeAPIKey:
		if p.cfg.Gateway.OpenAIWS.APIKeyMaxConnsFactor > 0 {
			return p.cfg.Gateway.OpenAIWS.APIKeyMaxConnsFactor
		}
	}
	return 1.0
}

func (p *openAIWSConnPool) effectiveMaxConnsByAccount(account *Account) int {
	hardCap := p.maxConnsHardCap()
	if hardCap <= 0 {
		return 0
	}
	if p.modeRouterV2Enabled() && account != nil && account.Concurrency <= 0 {
		return 0
	}
	if account == nil || !p.dynamicMaxConnsEnabled() {
		return hardCap
	}
	if account.Concurrency <= 0 {
		// 0/-1 等“无限制”并发场景下，仍由全局硬上限兜底。
		return hardCap
	}
	factor := p.maxConnsFactorByAccount(account)
	if factor <= 0 {
		factor = 1.0
	}
	effective := int(math.Ceil(float64(account.Concurrency) * factor))
	if effective < 1 {
		effective = 1
	}
	if effective > hardCap {
		effective = hardCap
	}
	return effective
}

func (p *openAIWSConnPool) minIdlePerAccount() int {
	if p != nil && p.cfg != nil && p.cfg.Gateway.OpenAIWS.MinIdlePerAccount >= 0 {
		return p.cfg.Gateway.OpenAIWS.MinIdlePerAccount
	}
	return 0
}

func (p *openAIWSConnPool) maxIdlePerAccount() int {
	if p != nil && p.cfg != nil && p.cfg.Gateway.OpenAIWS.MaxIdlePerAccount >= 0 {
		return p.cfg.Gateway.OpenAIWS.MaxIdlePerAccount
	}
	return 4
}

func (p *openAIWSConnPool) maxConnAge() time.Duration {
	return openAIWSConnMaxAge
}

func (p *openAIWSConnPool) queueLimitPerConn() int {
	if p != nil && p.cfg != nil && p.cfg.Gateway.OpenAIWS.QueueLimitPerConn > 0 {
		return p.cfg.Gateway.OpenAIWS.QueueLimitPerConn
	}
	return 256
}

func (p *openAIWSConnPool) targetUtilization() float64 {
	if p != nil && p.cfg != nil {
		ratio := p.cfg.Gateway.OpenAIWS.PoolTargetUtilization
		if ratio > 0 && ratio <= 1 {
			return ratio
		}
	}
	return 0.7
}

func (p *openAIWSConnPool) prewarmCooldown() time.Duration {
	if p != nil && p.cfg != nil && p.cfg.Gateway.OpenAIWS.PrewarmCooldownMS > 0 {
		return time.Duration(p.cfg.Gateway.OpenAIWS.PrewarmCooldownMS) * time.Millisecond
	}
	return 0
}

func (p *openAIWSConnPool) shouldSuppressPrewarmLocked(ap *openAIWSAccountPool, now time.Time) bool {
	if ap == nil {
		return true
	}
	if ap.prewarmFails <= 0 {
		return false
	}
	if ap.prewarmFailAt.IsZero() {
		ap.prewarmFails = 0
		return false
	}
	if now.Sub(ap.prewarmFailAt) > openAIWSPrewarmFailureWindow {
		ap.prewarmFails = 0
		ap.prewarmFailAt = time.Time{}
		return false
	}
	return ap.prewarmFails >= openAIWSPrewarmFailureSuppress
}

func (p *openAIWSConnPool) dialTimeout() time.Duration {
	if p != nil && p.cfg != nil && p.cfg.Gateway.OpenAIWS.DialTimeoutSeconds > 0 {
		return time.Duration(p.cfg.Gateway.OpenAIWS.DialTimeoutSeconds) * time.Second
	}
	return 10 * time.Second
}

func cloneOpenAIWSAcquireRequest(req openAIWSAcquireRequest) openAIWSAcquireRequest {
	copied := req
	copied.Headers = cloneHeader(req.Headers)
	copied.WSURL = stringsTrim(req.WSURL)
	copied.ProxyURL = stringsTrim(req.ProxyURL)
	copied.PreferredConnID = stringsTrim(req.PreferredConnID)
	copied.RequiredResponseID = stringsTrim(req.RequiredResponseID)
	return copied
}

func cloneOpenAIWSAcquireRequestPtr(req *openAIWSAcquireRequest) *openAIWSAcquireRequest {
	if req == nil {
		return nil
	}
	copied := cloneOpenAIWSAcquireRequest(*req)
	return &copied
}

func sameOpenAIWSPrewarmTarget(a, b openAIWSAcquireRequest) bool {
	return stringsTrim(a.WSURL) == stringsTrim(b.WSURL) &&
		stringsTrim(a.ProxyURL) == stringsTrim(b.ProxyURL) &&
		normalizeOpenAIWSAcquireCompatibility(a, a.Headers) == normalizeOpenAIWSAcquireCompatibility(b, b.Headers)
}

func normalizeOpenAIWSBetaFeatures(headers http.Header) string {
	features := make(map[string]struct{})
	for name, values := range headers {
		if !strings.EqualFold(strings.TrimSpace(name), "x-codex-beta-features") {
			continue
		}
		for _, value := range values {
			for _, feature := range strings.Split(value, ",") {
				if feature = strings.TrimSpace(feature); feature != "" {
					features[feature] = struct{}{}
				}
			}
		}
	}
	if len(features) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(features))
	for feature := range features {
		normalized = append(normalized, feature)
	}
	sort.Strings(normalized)
	return strings.Join(normalized, ",")
}

func normalizeOpenAIWSHandshakeCompatibility(account *Account, headers http.Header) openAIWSHandshakeCompatibilityKey {
	key := openAIWSHandshakeCompatibilityKey{
		betaFeatures: normalizeOpenAIWSBetaFeatures(headers),
	}
	mode := activeCodexFingerprintMode(account)
	if mode == codexFingerprintOff {
		return key
	}
	key.codexInstallationID = normalizeOpenAIWSStableIdentityHeader(headers, "x-codex-installation-id")
	if mode == codexFingerprintDevice {
		return key
	}
	key.sessionIDHyphen = normalizeOpenAIWSStableIdentityHeader(headers, "session-id")
	key.sessionIDUnderscore = normalizeOpenAIWSStableIdentityHeader(headers, "session_id")
	key.threadID = normalizeOpenAIWSStableIdentityHeader(headers, "thread-id")
	key.clientRequestID = normalizeOpenAIWSStableIdentityHeader(headers, "x-client-request-id")
	key.codexWindowID = normalizeOpenAIWSStableIdentityHeader(headers, "x-codex-window-id")
	return key
}

func activeCodexFingerprintMode(account *Account) codexFingerprintMode {
	if account == nil || account.GetCodexFingerprintMode() == codexFingerprintOff {
		return codexFingerprintOff
	}
	if _, ok := codexFingerprintSeed(account.Extra); !ok {
		return codexFingerprintOff
	}
	return account.GetCodexFingerprintMode()
}

func normalizeOpenAIWSStableIdentityHeader(headers http.Header, name string) string {
	if headers == nil {
		return ""
	}
	return strings.TrimSpace(headers.Get(name))
}

func normalizeOpenAIWSRoutingAffinity(headers http.Header) string {
	canonicalName := http.CanonicalHeaderKey(openAICodexRoutingHintHeader)
	if values, ok := headers[canonicalName]; ok {
		for _, value := range values {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}

	variantNames := make([]string, 0)
	for name := range headers {
		if name != canonicalName && strings.EqualFold(strings.TrimSpace(name), openAICodexRoutingHintHeader) {
			variantNames = append(variantNames, name)
		}
	}
	sort.Strings(variantNames)
	for _, name := range variantNames {
		for _, value := range headers[name] {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	return ""
}

func cloneHeader(src http.Header) http.Header {
	if src == nil {
		return nil
	}
	dst := make(http.Header, len(src))
	for k, vals := range src {
		if len(vals) == 0 {
			dst[k] = nil
			continue
		}
		copied := make([]string, len(vals))
		copy(copied, vals)
		dst[k] = copied
	}
	return dst
}

func closeOpenAIWSConns(conns []*openAIWSConn) {
	if len(conns) == 0 {
		return
	}
	for _, conn := range conns {
		if conn == nil {
			continue
		}
		conn.close()
	}
}

func stringsTrim(value string) string {
	return strings.TrimSpace(value)
}

func normalizeOpenAIWSAcquireCompatibility(req openAIWSAcquireRequest, headers http.Header) openAIWSHandshakeCompatibilityKey {
	key := normalizeOpenAIWSHandshakeCompatibility(req.Account, headers)
	key.tenantScope = req.TenantScope
	key.targetURL, key.proxyURL = stringsTrim(req.WSURL), stringsTrim(req.ProxyURL)
	key.credentialScope = req.CredentialScope
	if key.credentialScope == ([32]byte{}) && req.Account != nil {
		// Generic pool callers have no shadow credential owner; production
		// native composition supplies the full selected/owner scope above.
		key.credentialScope, _ = nativeWSReuseScope(req.Account, req.Account)
	}
	return key
}
