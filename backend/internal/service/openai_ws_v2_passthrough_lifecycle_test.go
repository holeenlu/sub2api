package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type stagedPassthroughFrame struct {
	messageType coderws.MessageType
	payload     []byte
	err         error
}

type stagedPassthroughConn struct {
	frames    chan stagedPassthroughFrame
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newStagedPassthroughConn() *stagedPassthroughConn {
	return &stagedPassthroughConn{
		frames: make(chan stagedPassthroughFrame, 4),
		writes: make(chan []byte, 4),
		closed: make(chan struct{}),
	}
}

func (c *stagedPassthroughConn) Send(payload string) {
	c.frames <- stagedPassthroughFrame{messageType: coderws.MessageText, payload: []byte(payload)}
}

func (c *stagedPassthroughConn) Fail(err error) {
	c.frames <- stagedPassthroughFrame{err: err}
}

func (c *stagedPassthroughConn) WriteJSON(context.Context, any) error { return nil }

func (c *stagedPassthroughConn) ReadMessage(ctx context.Context) ([]byte, error) {
	_, payload, err := c.ReadFrame(ctx)
	return payload, err
}

func (c *stagedPassthroughConn) Ping(context.Context) error { return nil }

func (c *stagedPassthroughConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return coderws.MessageText, nil, ctx.Err()
	case <-c.closed:
		return coderws.MessageText, nil, errOpenAIWSConnClosed
	case frame := <-c.frames:
		return frame.messageType, append([]byte(nil), frame.payload...), frame.err
	}
}

func (c *stagedPassthroughConn) WriteFrame(ctx context.Context, _ coderws.MessageType, payload []byte) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return errOpenAIWSConnClosed
	default:
	}
	var parsed any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return err
	}
	select {
	case c.writes <- append([]byte(nil), payload...):
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return errOpenAIWSConnClosed
	}
	return nil
}

func (c *stagedPassthroughConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

type stagedPassthroughDialer struct {
	conn openAIWSClientConn
}

func TestOpenAIWSPassthroughBareErrorAllowsDelayedTerminal(t *testing.T) {
	for _, terminal := range []string{"response.failed", "response.completed"} {
		t.Run(terminal, func(t *testing.T) {
			inner := newStagedPassthroughConn()
			defer inner.Close()
			conn := &openAIWSPassthroughFirstOutputFrameConn{inner: inner, activeReadTimeout: time.Second}
			conn.armActiveReadDeadline()
			inner.Send(`{"type":"error","error":{"type":"invalid_request_error","message":"rejected"}}`)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, event, err := conn.ReadFrame(ctx)
			require.NoError(t, err)
			require.Equal(t, "error", gjson.GetBytes(event, "type").String())
			time.Sleep(500 * time.Millisecond)
			inner.Send(`{"type":"` + terminal + `","response":{"usage":{"input_tokens":8,"output_tokens":4}}}`)
			_, event, err = conn.ReadFrame(ctx)
			require.NoError(t, err)
			require.Equal(t, terminal, gjson.GetBytes(event, "type").String())
			require.Equal(t, int64(8), gjson.GetBytes(event, "response.usage.input_tokens").Int())
			require.False(t, conn.deadlineState().armed)
		})
	}
}

func (d *stagedPassthroughDialer) Dial(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
	return d.conn, http.StatusSwitchingProtocols, http.Header{}, nil
}

func newPassthroughLifecycleService(cfg *config.Config, upstream *stagedPassthroughConn) *OpenAIGatewayService {
	return &OpenAIGatewayService{
		cfg:                       cfg,
		httpUpstream:              &httpUpstreamRecorder{},
		cache:                     &stubGatewayCache{},
		openaiWSResolver:          NewOpenAIWSProtocolResolver(cfg),
		toolCorrector:             NewCodexToolCorrector(),
		openaiWSPassthroughDialer: &stagedPassthroughDialer{conn: upstream},
	}
}

func passthroughLifecycleConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModeCtxPool
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 1
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 1
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	return cfg
}

func passthroughLifecycleAccount() *Account {
	return &Account{
		ID:          901,
		Name:        "passthrough-lifecycle",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test"},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough,
		},
	}
}

func startPassthroughLifecycleServer(
	t *testing.T,
	controlCtx context.Context,
	svc *OpenAIGatewayService,
	account *Account,
) (*httptest.Server, <-chan error) {
	return startPassthroughLifecycleServerWithHooks(t, controlCtx, svc, account, nil)
}

func startPassthroughLifecycleServerWithHooks(
	t *testing.T,
	controlCtx context.Context,
	svc *OpenAIGatewayService,
	account *Account,
	hooksFactory func(*gin.Context) *OpenAIWSIngressHooks,
	wrapClient ...func(OpenAIWSDownstreamConn) OpenAIWSDownstreamConn,
) (*httptest.Server, <-chan error) {
	t.Helper()
	serverErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			serverErr <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()

		msgType, firstMessage, err := ReadOpenAIWSClientMessage(
			controlCtx,
			conn,
			3*time.Second,
			coderws.StatusPolicyViolation,
			"missing first response.create message",
		)
		if err != nil {
			serverErr <- err
			return
		}
		if msgType != coderws.MessageText {
			serverErr <- errors.New("first message was not text")
			return
		}

		recorder := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(recorder)
		req := r.Clone(controlCtx)
		req.Header = req.Header.Clone()
		ginCtx.Request = req
		var hooks *OpenAIWSIngressHooks
		if hooksFactory != nil {
			hooks = hooksFactory(ginCtx)
		}
		var client OpenAIWSDownstreamConn = conn
		if len(wrapClient) > 0 {
			client = wrapClient[0](client)
		}
		serverErr <- svc.ProxyResponsesWebSocketFromClient(controlCtx, ginCtx, client, account, "sk-test", firstMessage, hooks)
	}))
	return server, serverErr
}

func TestPassthroughLifecycle_DeliveredTurnStateProvenance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)
	store := NewOpenAIWSStateStore(nil)
	groupID := int64(37)
	parent := passthroughLifecycleAccount()
	parent.Type = AccountTypeOAuth
	parent.Credentials = map[string]any{"access_token": "test-token", "chatgpt_account_id": "credential-A"}
	parent.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}
	shadow := *parent
	shadow.ID = parent.ID + 1
	shadow.ParentAccountID = &parent.ID
	shadow.Credentials = nil
	other := *parent
	other.ID = parent.ID + 2
	other.Credentials = map[string]any{"access_token": "other-token", "chatgpt_account_id": "credential-B"}
	type session struct {
		client   *coderws.Conn
		upstream *stagedPassthroughConn
		source   *Account
		err      <-chan error
	}
	var sessions []session
	for _, account := range []*Account{&shadow, &other} {
		upstream := newStagedPassthroughConn()
		cfg := passthroughLifecycleConfig()
		cfg.Gateway.OpenAIWS.OAuthEnabled = true
		cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 5
		svc := newPassthroughLifecycleService(cfg, upstream)
		svc.cache = nil
		svc.openaiWSStateStore = store
		svc.accountRepo = stubOpenAIAccountRepo{accounts: []Account{*parent, shadow, other}}
		server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, svc, account, func(c *gin.Context) *OpenAIWSIngressHooks {
			c.Set("api_key", &APIKey{ID: 77, GroupID: &groupID})
			c.Request.Header.Set("session_id", "shared-client-session")
			return nil
		})
		defer server.Close()
		client := dialPassthroughLifecycleClient(t, server)
		defer client.CloseNow()
		requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
		source := account
		if account.IsShadow() {
			source = parent
		}
		sessions = append(sessions, session{client, upstream, source, serverErr})
	}

	// Both sockets stay active in the same tenant/session. Each credential and
	// each subsequent turn issues a distinct blob; later writes cannot replace
	// the provenance of an earlier blob from the other socket.
	for turn := 0; turn < 2; turn++ {
		for i, session := range sessions {
			if turn > 0 {
				writeCtx, cancelWrite := context.WithTimeout(ctx, 3*time.Second)
				err := session.client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"next"}`))
				cancelWrite()
				require.NoError(t, err)
				requirePassthroughUpstreamWrite(t, session.upstream, 3*time.Second)
			}
			session.upstream.Send(fmt.Sprintf(`{"type":"codex.response.metadata","headers":{"x-codex-turn-state":"state-%d-%d"}}`, i, turn))
			session.upstream.Send(fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_%d_%d","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`, i, turn))
		}
		for i, session := range sessions {
			metadata, err := readPassthroughLifecycleFrame(t, session.client, 3*time.Second)
			require.NoError(t, err)
			require.Equal(t, fmt.Sprintf("state-%d-%d", i, turn), gjson.GetBytes(metadata, "headers.x-codex-turn-state").String())
			completed, err := readPassthroughLifecycleFrame(t, session.client, 3*time.Second)
			require.NoError(t, err)
			require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())
		}
	}
	for _, session := range sessions {
		require.NoError(t, session.client.Close(coderws.StatusNormalClosure, "done"))
		select {
		case err := <-session.err:
			require.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("passthrough did not exit")
		}
	}

	c, _ := newTurnStateTestContext(t, 77, "shared-client-session")
	c.Set("api_key", &APIKey{ID: 77, GroupID: &groupID})
	reader := &OpenAIGatewayService{openaiWSStateStore: store}
	for i, session := range sessions {
		for turn := 0; turn < 2; turn++ {
			state := fmt.Sprintf("state-%d-%d", i, turn)
			owner, err := store.GetResponseAccount(ctx, groupID, openAICodexTurnStateKey(c, state))
			require.NoError(t, err)
			require.Equal(t, openAICodexTurnStateOwner(session.source), owner, "record the resolved credential, not the shadow row")
			for _, target := range []*Account{parent, &other} {
				headers, _, err := reader.buildOpenAIWSHeaders(ctx, c, target, "test-token", OpenAIWSProtocolDecision{}, true, state, "", "", "gpt-5.1", "", nil)
				require.NoError(t, err)
				require.Equal(t, target == session.source, headers.Get(openAICodexTurnStateHeader) != "", "%s on credential %d", state, target.ID)
			}
		}
	}
	// Identical state text in another API key or group is still unknown there.
	for _, scope := range []struct{ key, group int64 }{{78, groupID}, {77, groupID + 1}} {
		otherContext, _ := newTurnStateTestContext(t, scope.key, "shared-client-session")
		otherContext.Set("api_key", &APIKey{ID: scope.key, GroupID: &scope.group})
		headers, _, err := reader.buildOpenAIWSHeaders(ctx, otherContext, &other, "test-token", OpenAIWSProtocolDecision{}, true, "state-0-0", "", "", "gpt-5.1", "", nil)
		require.NoError(t, err)
		require.Equal(t, "state-0-0", headers.Get(openAICodexTurnStateHeader))
	}
}

type passthroughMetadataFailConn struct {
	OpenAIWSDownstreamConn
	writes atomic.Int32
}

func (c *passthroughMetadataFailConn) Write(ctx context.Context, kind coderws.MessageType, payload []byte) error {
	if gjson.GetBytes(payload, "type").String() == "codex.response.metadata" {
		c.writes.Add(1)
		return io.ErrClosedPipe
	}
	return c.OpenAIWSDownstreamConn.Write(ctx, kind, payload)
}

func TestPassthroughLifecycle_UndeliveredTurnStateDoesNotRecordProvenance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)
	upstream := newStagedPassthroughConn()
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	account := passthroughLifecycleAccount()
	account.Type = AccountTypeOAuth
	account.Credentials = map[string]any{"access_token": "test-token", "chatgpt_account_id": "failed-credential"}
	account.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}
	svc := newPassthroughLifecycleService(cfg, upstream)
	svc.cache = nil
	c, _ := newTurnStateTestContext(t, 77, "failed-session")
	prior := &Account{ID: 42}
	svc.noteOpenAICodexTurnStateProvenance(c, prior, "known-state")
	failed := &passthroughMetadataFailConn{}
	server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, svc, account, func(c *gin.Context) *OpenAIWSIngressHooks {
		c.Set("api_key", &APIKey{ID: 77})
		c.Request.Header.Set("session_id", "failed-session")
		return nil
	}, func(conn OpenAIWSDownstreamConn) OpenAIWSDownstreamConn {
		failed.OpenAIWSDownstreamConn = conn
		return failed
	})
	defer server.Close()
	client := dialPassthroughLifecycleClient(t, server)
	defer client.CloseNow()
	requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
	upstream.Send(`{"type":"codex.response.metadata","headers":{"x-codex-turn-state":"known-state"}}`)
	upstream.Send(`{"type":"codex.response.metadata","headers":{"x-codex-turn-state":"abandoned-state"}}`)
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_failed_write"}}`)
	_, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.Error(t, err)
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("failed passthrough did not exit")
	}
	require.Equal(t, int32(1), failed.writes.Load(), "exercise the metadata write failure before the attempt is abandoned")
	for state, want := range map[string]int64{"known-state": prior.ID, "abandoned-state": 0} {
		owner, err := svc.getOpenAIWSStateStore().GetResponseAccount(ctx, 0, openAICodexTurnStateKey(c, state))
		require.NoError(t, err)
		require.Equal(t, want, owner, "%s must not be bound to the failed attempt", state)
	}
}

func TestPassthroughLifecycle_LaterTurnPreOutputRateLimitRequestsReconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	upstream := newStagedPassthroughConn()
	account := passthroughLifecycleAccount()
	repo := &openAIWSRateLimitSignalRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{*account}}}
	svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
	svc.accountRepo = repo
	svc.rateLimitService = &RateLimitService{accountRepo: repo}

	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, svc, account)
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()
	firstRequest := requirePassthroughUpstreamWrite(t, upstream, time.Second)
	require.Equal(t, "response.create", gjson.GetBytes(firstRequest, "type").String())

	upstream.Send(`{"type":"response.completed","response":{"id":"resp_first","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	completed, err := readPassthroughLifecycleFrame(t, clientConn, time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"hello","stream":false}`))
	cancelWrite()
	require.NoError(t, err)
	secondRequest := requirePassthroughUpstreamWrite(t, upstream, time.Second)
	require.Equal(t, "response.create", gjson.GetBytes(secondRequest, "type").String())

	resetAt := time.Now().Add(90 * time.Minute).Unix()
	upstream.Send(fmt.Sprintf(`{"type":"error","error":{"code":"rate_limit_exceeded","type":"usage_limit_reached","message":"The usage limit has been reached","resets_at":%d}}`, resetAt))
	_, err = readPassthroughLifecycleFrame(t, clientConn, time.Second)
	var websocketCloseErr coderws.CloseError
	require.ErrorAs(t, err, &websocketCloseErr)
	require.Equal(t, coderws.StatusTryAgainLater, websocketCloseErr.Code)
	require.Equal(t, "upstream rate limit exceeded; please reconnect", websocketCloseErr.Reason)
	require.Len(t, repo.rateLimitCalls, 1)
	require.WithinDuration(t, time.Now().Add(defaultRateLimit429CooldownSeconds*time.Second), repo.rateLimitCalls[0], 2*time.Second)

	select {
	case err := <-serverErr:
		var closeErr *OpenAIWSClientCloseError
		require.ErrorAs(t, err, &closeErr)
		require.Equal(t, coderws.StatusTryAgainLater, closeErr.StatusCode())
	case <-time.After(time.Second):
		t.Fatal("later-turn rate limit did not terminate passthrough")
	}
	select {
	case replay := <-upstream.writes:
		t.Fatalf("later-turn reconnect must not replay the retained first request: %s", replay)
	default:
	}
}

func TestPassthroughLifecycle_CyberTerminalEventsMarkBeforeAfterTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		events      []string
		wantBody    string
		wantMessage string
		wantInput   int
		wantOutput  int
	}{
		{
			name: "error",
			events: []string{
				`{"type":"error","error":{"code":"cyber_policy","message":"blocked by error event"},"usage":{"input_tokens":5,"output_tokens":1}}`,
				`{"type":"response.failed","response":{"id":"resp_error","error":{"code":"cyber_policy","message":"blocked by paired failed event"},"usage":{"input_tokens":9,"output_tokens":2}}}`,
			},
			wantBody:    `"type":"error"`,
			wantMessage: "blocked by error event",
			wantInput:   5,
			wantOutput:  1,
		},
		{
			name: "response_failed",
			events: []string{
				`{"type":"response.failed","response":{"id":"resp_failed","error":{"code":"cyber_policy","message":"blocked by failed event"},"usage":{"input_tokens":9,"output_tokens":2}}}`,
			},
			wantBody:    `"type":"response.failed"`,
			wantMessage: "blocked by failed event",
			wantInput:   9,
			wantOutput:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controlCtx, cancelControl := context.WithCancelCause(context.Background())
			defer cancelControl(context.Canceled)
			upstream := newStagedPassthroughConn()
			for _, event := range tt.events {
				upstream.Send(event)
			}

			markSeen := make(chan CyberPolicyMark, 1)
			afterTurnCalls := atomic.Int32{}
			server, serverErr := startPassthroughLifecycleServerWithHooks(
				t,
				controlCtx,
				newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream),
				passthroughLifecycleAccount(),
				func(c *gin.Context) *OpenAIWSIngressHooks {
					return &OpenAIWSIngressHooks{AfterTurn: func(_ int, _ *OpenAIForwardResult, _ error) {
						afterTurnCalls.Add(1)
						if mark := GetOpsCyberPolicy(c); mark != nil {
							select {
							case markSeen <- *mark:
							default:
							}
						}
					}}
				},
			)
			defer server.Close()
			clientConn := dialPassthroughLifecycleClient(t, server)
			defer func() { _ = clientConn.CloseNow() }()

			for range tt.events {
				_, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
				require.NoError(t, err)
			}

			select {
			case mark := <-markSeen:
				require.Equal(t, "cyber_policy", mark.Code)
				require.Equal(t, tt.wantMessage, mark.Message)
				require.Contains(t, mark.Body, tt.wantBody)
				require.Equal(t, http.StatusOK, mark.UpstreamStatus)
				require.Equal(t, tt.wantInput, mark.UpstreamInTok)
				require.Equal(t, tt.wantOutput, mark.UpstreamOutTok)
			case <-time.After(3 * time.Second):
				t.Fatal("cyber mark was not visible to AfterTurn")
			}
			require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))
			select {
			case <-serverErr:
			case <-time.After(3 * time.Second):
				t.Fatal("cyber passthrough test did not exit")
			}
			require.Equal(t, int32(1), afterTurnCalls.Load(), "error/response.failed pair must complete and record once")
		})
	}
}

func TestPassthroughLifecycle_NonCyberFailureKeepsAccountSideEffects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.failed","response":{"id":"resp_non_cyber","error":{"type":"authentication_error","code":"invalid_api_key","status_code":401,"message":"credential rejected"},"usage":{"input_tokens":3,"output_tokens":1}}}`)
	repo := &openAIStream403AccountRepo{}
	svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
	svc.rateLimitService = NewRateLimitService(repo, nil, svc.cfg, nil, nil)
	account := passthroughLifecycleAccount()

	markSeen := make(chan *CyberPolicyMark, 1)
	server, serverErr := startPassthroughLifecycleServerWithHooks(
		t,
		controlCtx,
		svc,
		account,
		func(c *gin.Context) *OpenAIWSIngressHooks {
			return &OpenAIWSIngressHooks{AfterTurn: func(_ int, _ *OpenAIForwardResult, _ error) {
				markSeen <- GetOpsCyberPolicy(c)
			}}
		},
	)
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	event, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.failed", gjson.GetBytes(event, "type").String())
	select {
	case mark := <-markSeen:
		require.Nil(t, mark)
	case <-time.After(3 * time.Second):
		t.Fatal("non-cyber terminal event did not complete its turn")
	}
	require.Equal(t, 1, repo.setErrorCalls, "non-cyber credential failure must retain account failure side effects")
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("non-cyber passthrough test did not exit")
	}
}

func TestPassthroughLifecycle_CyberSkipsFailureAccountSideEffects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.failed","response":{"id":"resp_cyber_auth","error":{"type":"authentication_error","code":"cyber_policy","status_code":401,"message":"request blocked"}}}`)
	repo := &openAIStream403AccountRepo{}
	svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
	svc.rateLimitService = NewRateLimitService(repo, nil, svc.cfg, nil, nil)
	account := passthroughLifecycleAccount()

	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, svc, account)
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	event, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.failed", gjson.GetBytes(event, "type").String())
	require.Zero(t, repo.setErrorCalls, "cyber_policy is request-scoped and must not cool down the account")
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))

	require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("cyber side-effect test did not exit")
	}
}

func TestPassthroughLifecycle_CloseReasonTruncationPreservesUTF8(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	upstream := newStagedPassthroughConn()
	originalReason := strings.Repeat("a", 119) + "界"
	upstream.Fail(NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, originalReason, errors.New("policy rejected")))

	server, serverErr := startPassthroughLifecycleServer(
		t,
		controlCtx,
		newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream),
		passthroughLifecycleAccount(),
	)
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	_, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	var closeErr coderws.CloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, closeErr.Code)
	require.True(t, utf8.ValidString(closeErr.Reason))
	require.LessOrEqual(t, len(closeErr.Reason), 120)
	require.Equal(t, strings.Repeat("a", 119), closeErr.Reason)

	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("passthrough close reason test did not exit")
	}
}

func dialPassthroughLifecycleClient(t *testing.T, server *httptest.Server) *coderws.Conn {
	t.Helper()
	return dialPassthroughLifecycleClientWithPayload(t, server, `{"type":"response.create","model":"gpt-5.1","input":"hello","stream":false}`)
}

func dialPassthroughLifecycleClientWithPayload(t *testing.T, server *httptest.Server, payload string) *coderws.Conn {
	t.Helper()
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	cancelDial()
	require.NoError(t, err)
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(payload))
	cancelWrite()
	require.NoError(t, err)
	return clientConn
}

func readPassthroughLifecycleFrame(t *testing.T, clientConn *coderws.Conn, timeout time.Duration) ([]byte, error) {
	t.Helper()
	readCtx, cancelRead := context.WithTimeout(context.Background(), timeout)
	_, payload, err := clientConn.Read(readCtx)
	cancelRead()
	return payload, err
}

func requirePassthroughUpstreamWrite(t *testing.T, upstream *stagedPassthroughConn, timeout time.Duration) []byte {
	t.Helper()
	select {
	case payload := <-upstream.writes:
		return payload
	case <-time.After(timeout):
		t.Fatal("passthrough request was not forwarded upstream")
		return nil
	}
}

func TestPassthroughLifecycle_ResponsesLiteFramesPinParallelToolCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		t.Run(accountType, func(t *testing.T) {
			controlCtx, cancelControl := context.WithCancelCause(context.Background())
			defer cancelControl(context.Canceled)
			upstream := newStagedPassthroughConn()
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			account := passthroughLifecycleAccount()
			if accountType == AccountTypeOAuth {
				account.Type = accountType
				account.Credentials = map[string]any{"access_token": "test-token", "chatgpt_account_id": "lite-account"}
				account.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}
			}
			server, serverErr := startPassthroughLifecycleServer(t, controlCtx, newPassthroughLifecycleService(cfg, upstream), account)
			defer server.Close()
			requests := []string{
				`{"type":"response.create","model":"gpt-5.1","input":"hello","stream":false,
				"parallel_tool_calls":true,"reasoning":{"context":"current_turn"},
				"tool_choice":"none","include":["reasoning.encrypted_content"],
				"client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true"}}`,
				`{"type":"response.create","model":"gpt-5.1","input":"next",
				"tool_choice":"none","include":["reasoning.encrypted_content"],
				"client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true"}}`,
			}
			clientConn := dialPassthroughLifecycleClientWithPayload(t, server, requests[0])
			defer clientConn.CloseNow()
			for turn, request := range requests {
				if turn > 0 {
					writeCtx, cancelWrite := context.WithTimeout(controlCtx, 3*time.Second)
					err := clientConn.Write(writeCtx, coderws.MessageText, []byte(request))
					cancelWrite()
					require.NoError(t, err)
				}
				upstreamBody := requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
				require.Equal(t, gjson.False, gjson.GetBytes(upstreamBody, "parallel_tool_calls").Type, string(upstreamBody))
				if accountType == AccountTypeOAuth {
					require.Equal(t, "all_turns", gjson.GetBytes(upstreamBody, "reasoning.context").String())
				} else {
					require.Equal(t, gjson.Get(request, "reasoning").Raw, gjson.GetBytes(upstreamBody, "reasoning").Raw, "API-key Lite only pins parallel calls")
				}
				require.Equal(t, "none", gjson.GetBytes(upstreamBody, "tool_choice").String())
				require.JSONEq(t, `["reasoning.encrypted_content"]`, gjson.GetBytes(upstreamBody, "include").Raw)
				upstream.Send(fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_lite_%d","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`, turn))
				event, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
				require.NoError(t, err)
				require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
			}
			require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))
			select {
			case err := <-serverErr:
				require.NoError(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("Lite passthrough did not exit")
			}
		})
	}
}

func TestOpenAIWSPassthroughTurnLifecycle_SerializesTerminalCommitAndNextTurn(t *testing.T) {
	clientFrameConn := &openAIWSClientFrameConn{interTurnStarted: make(chan struct{}, 1)}
	clientFrameConn.markTurnCompleted()
	lifecycle := newOpenAIWSPassthroughTurnLifecycle(true)
	lifecycle.beginTerminalWrite()

	admitted := make(chan bool, 1)
	go func() {
		admitted <- lifecycle.beginResponseCreate(clientFrameConn.markTurnStarted)
	}()
	select {
	case <-admitted:
		t.Fatal("next response.create was admitted before terminal commit completed")
	case <-time.After(50 * time.Millisecond):
	}

	lifecycle.finishTerminalWrite(true, clientFrameConn.markTurnCompleted)
	select {
	case ok := <-admitted:
		require.True(t, ok)
	case <-time.After(time.Second):
		t.Fatal("next response.create remained blocked after terminal commit")
	}
	require.False(t, clientFrameConn.waitingForNextTurn.Load(), "accepted next turn must win over terminal idle state")

	lifecycle = newOpenAIWSPassthroughTurnLifecycle(true)
	lifecycle.beginTerminalWrite()
	admitted = make(chan bool, 1)
	go func() {
		admitted <- lifecycle.beginResponseCreate(nil)
	}()
	lifecycle.finishTerminalWrite(false, func() {
		t.Error("failed terminal write must not commit idle state")
	})
	require.False(t, <-admitted, "failed terminal write must keep the current turn in flight")
}

func TestPassthroughLifecycle_LeaseLossSendsRetryClose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.created","response":{"id":"resp_lease","model":"gpt-5.1"}}`)
	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), passthroughLifecycleAccount())
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	event, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.created", gjson.GetBytes(event, "type").String())
	cancelControl(ErrOpenAIWSIngressLeaseLost)

	_, err = readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	var closeErr coderws.CloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusTryAgainLater, closeErr.Code)
	require.Equal(t, "websocket ingress capacity lease lost; please reconnect", closeErr.Reason)
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("passthrough lease-loss reader did not exit")
	}
}

func TestPassthroughLifecycle_CompletedTurnStartsInterTurnIdle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_idle","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), passthroughLifecycleAccount())
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	event, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
	_, err = readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	var closeErr coderws.CloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusNormalClosure, closeErr.Code)
	require.Equal(t, "websocket idle timeout", closeErr.Reason)
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("passthrough idle reader did not exit")
	}
}

func TestPassthroughLifecycle_ActiveTurnInactivityUsesReadTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.output_text.delta","response_id":"resp_active","delta":"hello"}`)
	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), passthroughLifecycleAccount())
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	delta, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.output_text.delta", gjson.GetBytes(delta, "type").String())
	_, err = readPassthroughLifecycleFrame(t, clientConn, 2500*time.Millisecond)
	var websocketCloseErr coderws.CloseError
	require.ErrorAs(t, err, &websocketCloseErr)
	require.Equal(t, coderws.StatusGoingAway, websocketCloseErr.Code)
	require.Equal(t, "upstream websocket read timeout; please reconnect", websocketCloseErr.Reason)
	select {
	case err := <-serverErr:
		var closeErr *OpenAIWSClientCloseError
		require.ErrorAs(t, err, &closeErr)
		require.Equal(t, coderws.StatusGoingAway, closeErr.StatusCode())
		require.Equal(t, "upstream websocket read timeout; please reconnect", closeErr.Reason())
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("passthrough active turn remained unbounded after upstream activity stopped")
	}
}

func TestPassthroughLifecycle_PreambleAllowsPromptClientCancel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 3
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.created","response":{"id":"resp_cancel","model":"gpt-5.1"}}`)
	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, newPassthroughLifecycleService(cfg, upstream), passthroughLifecycleAccount())
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()
	require.Equal(t, "response.create", gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream, time.Second), "type").String())

	created, err := readPassthroughLifecycleFrame(t, clientConn, time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.created", gjson.GetBytes(created, "type").String())
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.cancel","response_id":"resp_cancel"}`))
	cancelWrite()
	require.NoError(t, err)
	cancelFrame := requirePassthroughUpstreamWrite(t, upstream, 500*time.Millisecond)
	require.Equal(t, "response.cancel", gjson.GetBytes(cancelFrame, "type").String())

	require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("passthrough cancel test did not exit")
	}
}

func TestPassthroughLifecycle_RejectsOverlappingResponseCreate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 3
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.created","response":{"id":"resp_overlap_first","model":"gpt-5.1"}}`)
	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, newPassthroughLifecycleService(cfg, upstream), passthroughLifecycleAccount())
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()
	require.Equal(t, "response.create", gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream, time.Second), "type").String())

	created, err := readPassthroughLifecycleFrame(t, clientConn, time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.created", gjson.GetBytes(created, "type").String())
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"hello"}`))
	cancelWrite()
	require.NoError(t, err)

	_, err = readPassthroughLifecycleFrame(t, clientConn, time.Second)
	var websocketCloseErr coderws.CloseError
	require.ErrorAs(t, err, &websocketCloseErr)
	require.Equal(t, coderws.StatusPolicyViolation, websocketCloseErr.Code)
	require.Equal(t, "overlapping response.create is not supported", websocketCloseErr.Reason)
	select {
	case err := <-serverErr:
		var closeErr *OpenAIWSClientCloseError
		require.ErrorAs(t, err, &closeErr)
		require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
		require.Equal(t, "overlapping response.create is not supported", closeErr.Reason())
	case <-time.After(3 * time.Second):
		t.Fatal("overlapping response.create did not terminate passthrough")
	}
}

func TestPassthroughLifecycle_ActiveTurnActivityRefreshesReadTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.output_text.delta","response_id":"resp_active_refresh","delta":"one"}`)
	go func() {
		for _, event := range []string{
			`{"type":"response.output_text.delta","response_id":"resp_active_refresh","delta":"two"}`,
			`{"type":"response.output_text.delta","response_id":"resp_active_refresh","delta":"three"}`,
			`{"type":"response.completed","response":{"id":"resp_active_refresh","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":3}}}`,
		} {
			timer := time.NewTimer(600 * time.Millisecond)
			<-timer.C
			timer.Stop()
			upstream.Send(event)
		}
	}()
	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), passthroughLifecycleAccount())
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	for _, wantType := range []string{
		"response.output_text.delta",
		"response.output_text.delta",
		"response.output_text.delta",
		"response.completed",
	} {
		frame, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
		require.NoError(t, err)
		require.Equal(t, wantType, gjson.GetBytes(frame, "type").String())
	}
	require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("passthrough active-turn refresh test did not exit")
	}
}

func TestPassthroughLifecycle_TerminalSwitchesToInterTurnIdleTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 1
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 2
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_idle_first","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, newPassthroughLifecycleService(cfg, upstream), passthroughLifecycleAccount())
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()
	require.Equal(t, "response.create", gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream, 3*time.Second), "type").String())

	completed, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "resp_idle_first", gjson.GetBytes(completed, "response.id").String())
	time.Sleep(1300 * time.Millisecond)
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"resp_idle_first"}`))
	cancelWrite()
	require.NoError(t, err)
	require.Equal(t, "response.create", gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream, 3*time.Second), "type").String())
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_idle_second","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	completed, err = readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "resp_idle_second", gjson.GetBytes(completed, "response.id").String())
	_, err = readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	var websocketCloseErr coderws.CloseError
	require.ErrorAs(t, err, &websocketCloseErr)
	require.Equal(t, coderws.StatusNormalClosure, websocketCloseErr.Code)
	require.Equal(t, "websocket idle timeout", websocketCloseErr.Reason)

	select {
	case err := <-serverErr:
		var closeErr *OpenAIWSClientCloseError
		require.ErrorAs(t, err, &closeErr)
		require.Equal(t, coderws.StatusNormalClosure, closeErr.StatusCode())
		require.Equal(t, "websocket idle timeout", closeErr.Reason())
	case <-time.After(3 * time.Second):
		t.Fatal("passthrough terminal turn did not use inter-turn idle timeout")
	}
}

func TestPassthroughLifecycle_FirstOutputTimeoutRemainsBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	upstream := newStagedPassthroughConn()
	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), passthroughLifecycleAccount())
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	select {
	case err := <-serverErr:
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
		require.Contains(t, string(failoverErr.ResponseBody), "first_output_timeout")
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("passthrough first output was left unbounded")
	}
}

func TestPassthroughLifecycle_ResponseCreatedTimeoutClosesWithoutFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.created","response":{"id":"resp_preamble","model":"gpt-5.1"}}`)
	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), passthroughLifecycleAccount())
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	created, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.created", gjson.GetBytes(created, "type").String())
	_, err = readPassthroughLifecycleFrame(t, clientConn, 2500*time.Millisecond)
	var websocketCloseErr coderws.CloseError
	require.ErrorAs(t, err, &websocketCloseErr)
	require.Equal(t, coderws.StatusGoingAway, websocketCloseErr.Code)
	require.Equal(t, "upstream produced no semantic output; please reconnect", websocketCloseErr.Reason)
	select {
	case err := <-serverErr:
		var failoverErr *UpstreamFailoverError
		require.NotErrorAs(t, err, &failoverErr)
		var closeErr *OpenAIWSClientCloseError
		require.ErrorAs(t, err, &closeErr)
		require.Equal(t, coderws.StatusGoingAway, closeErr.StatusCode())
		require.Equal(t, "upstream produced no semantic output; please reconnect", closeErr.Reason())
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("response.created timeout did not close the passthrough connection")
	}
}

func TestPassthroughLifecycle_SecondTurnTimeoutIsNotFailoverSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_first","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), passthroughLifecycleAccount())
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	completed, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"resp_first"}`))
	cancelWrite()
	require.NoError(t, err)
	upstream.Send(`{"type":"response.created","response":{"id":"resp_second","model":"gpt-5.1"}}`)

	created, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.created", gjson.GetBytes(created, "type").String())
	_, err = readPassthroughLifecycleFrame(t, clientConn, 2500*time.Millisecond)
	var websocketCloseErr coderws.CloseError
	require.ErrorAs(t, err, &websocketCloseErr)
	require.Equal(t, coderws.StatusGoingAway, websocketCloseErr.Code)
	require.Equal(t, "upstream produced no semantic output; please reconnect", websocketCloseErr.Reason)
	select {
	case err := <-serverErr:
		var failoverErr *UpstreamFailoverError
		require.NotErrorAs(t, err, &failoverErr, "handler must not replay the initial request on another account for a later-turn timeout")
		var closeErr *OpenAIWSClientCloseError
		require.ErrorAs(t, err, &closeErr)
		require.Equal(t, coderws.StatusGoingAway, closeErr.StatusCode())
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("second turn first semantic output was left unbounded")
	}
}

// Hold the second local Write completion while permitting a real upstream reply.
// This catches summaries that close on the reader before the sender returns.
type latencyDelayedWriteConn struct {
	*stagedPassthroughConn
	calls   atomic.Int32
	release chan struct{}
}

func (c *latencyDelayedWriteConn) WriteFrame(ctx context.Context, typ coderws.MessageType, payload []byte) error {
	if err := c.stagedPassthroughConn.WriteFrame(ctx, typ, payload); err != nil {
		return err
	}
	if c.calls.Add(1) == 2 {
		select {
		case <-c.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func TestPassthroughLifecycle_TransportTimingWaitsForSendCompletion(t *testing.T) {
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 5
	upstream := newStagedPassthroughConn()
	delayed := &latencyDelayedWriteConn{stagedPassthroughConn: upstream, release: make(chan struct{})}
	svc := newPassthroughLifecycleService(cfg, upstream)
	svc.openaiWSPassthroughDialer = &stagedPassthroughDialer{conn: delayed}
	core, logs := observer.New(zap.InfoLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, svc, passthroughLifecycleAccount(), func(c *gin.Context) *OpenAIWSIngressHooks {
		c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), zap.New(core)))
		return nil
	})
	defer server.Close()
	client := dialPassthroughLifecycleClient(t, server)
	defer client.CloseNow()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(delayed.release) }) }
	defer release()
	for turn := 1; turn <= 2; turn++ {
		if turn == 2 {
			wctx, done := context.WithTimeout(ctx, time.Second)
			err := client.Write(wctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"second"}`))
			done()
			require.NoError(t, err)
		}
		requirePassthroughUpstreamWrite(t, upstream, time.Second)
		id := fmt.Sprintf("resp_send_%d", turn)
		upstream.Send(`{"type":"response.output_text.delta","response_id":"` + id + `","delta":"ok"}`)
		upstream.Send(`{"type":"response.completed","response":{"id":"` + id + `","status":"completed"}}`)
		readNativeWSTestTurn(t, client)
		if turn == 2 {
			require.Len(t, wsLatencySummaries(logs), 1)
			release()
		}
		require.Eventually(t, func() bool { return len(wsLatencySummaries(logs)) == turn }, time.Second, time.Millisecond)
	}
	summaries := wsLatencySummaries(logs)
	stages := summaries[1]["stages_ms"].(map[string]int64)
	require.Contains(t, stages, "wrote_request")
	require.Contains(t, stages, "terminal")
	require.Contains(t, stages, "first_text_flush")
	require.GreaterOrEqual(t, stages["wrote_request"], stages["terminal"])
	requireNativeWSTestServerExit(t, client, serverErr)
}

func TestPassthroughLifecycle_TransportTimingWarmupDoesNotGenerateAttempt(t *testing.T) {
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 5
	upstream := newStagedPassthroughConn()
	svc := newPassthroughLifecycleService(cfg, upstream)
	core, logs := observer.New(zap.InfoLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, svc, passthroughLifecycleAccount(), func(c *gin.Context) *OpenAIWSIngressHooks {
		c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), zap.New(core)))
		BeginOpenAIRequestTiming(c)
		openAIRequestTimingFromContext(c.Request.Context()).started = time.Now().Add(-2 * time.Minute)
		return nil
	})
	defer server.Close()
	client := dialPassthroughLifecycleClientWithPayload(t, server, `{"type":"response.create","model":"gpt-5.1","generate":false,"input":"warm"}`)
	defer client.CloseNow()
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	// An ID-less warmup does not advance the relay's billed-turn counter.
	upstream.Send(`{"type":"response.completed","response":{"status":"completed","output":[]}}`)
	readNativeWSTestTurn(t, client)
	require.Empty(t, wsLatencySummaries(logs))
	wctx, done := context.WithTimeout(ctx, time.Second)
	err := client.Write(wctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"hello"}`))
	done()
	require.NoError(t, err)
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	upstream.Send(`{"type":"response.completed","response":{"id":"real","status":"completed","output":[]}}`)
	readNativeWSTestTurn(t, client)
	require.Eventually(t, func() bool { return len(wsLatencySummaries(logs)) == 1 }, time.Second, time.Millisecond)
	require.Equal(t, "completed", wsLatencySummaries(logs)[0]["outcome"])
	require.Less(t, wsLatencySummaries(logs)[0]["request_elapsed_ms"].(int64), int64(30000), "first generation must not reuse the old prewarm middleware root")
	prewarms := 0
	for _, entry := range logs.All() {
		if entry.ContextMap()["stage"] == "prewarm" {
			prewarms++
		}
	}
	require.Equal(t, 1, prewarms)
	requireNativeWSTestServerExit(t, client, serverErr)
}

func TestPassthroughLifecycle_MeteredRateLimitDoesNotFailover(t *testing.T) {
	for _, usage := range []string{`{"input_tokens":5,"output_tokens":1}`, `{"input_tokens":0,"output_tokens":0,"input_tokens_details":{"cached_tokens":3}}`} {
		t.Run(usage, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(context.Canceled)
			upstream := newStagedPassthroughConn()
			results := make(chan *OpenAIForwardResult, 4)
			server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), passthroughLifecycleAccount(), func(*gin.Context) *OpenAIWSIngressHooks {
				return &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, _ error) {
					if result != nil {
						results <- result
					}
				}}
			})
			defer server.Close()
			client := dialPassthroughLifecycleClient(t, server)
			defer client.CloseNow()
			requirePassthroughUpstreamWrite(t, upstream, time.Second)
			upstream.Send(`{"type":"error","error":{"code":"rate_limit_exceeded","type":"usage_limit_reached","message":"capacity exhausted after metering"},"usage":` + usage + `}`)
			payload, err := readPassthroughLifecycleFrame(t, client, time.Second)
			require.NoError(t, err, "metered failure must be delivered instead of returned for replay")
			require.Equal(t, "rate_limit_exceeded", gjson.GetBytes(payload, "error.code").String())
			upstream.Fail(io.EOF)
			select {
			case result := <-results:
				require.Equal(t, int(gjson.Get(usage, "input_tokens").Int()), result.Usage.InputTokens)
				require.Equal(t, int(gjson.Get(usage, "input_tokens_details.cached_tokens").Int()), result.Usage.CacheReadInputTokens)
				require.True(t, openAIUsageHasTokens(&result.Usage))
			case <-time.After(2 * time.Second):
				t.Fatal("metered error lost its usage")
			}
			// Complete the client side of the normal close handshake before
			// waiting for the server's relay cleanup to join its reader.
			_ = client.CloseNow()
			select {
			case err := <-serverErr:
				var failover *UpstreamFailoverError
				require.False(t, errors.As(err, &failover), "metered turn must never authorize an account replay")
			case <-time.After(2 * time.Second):
				t.Fatal("passthrough did not finish")
			}
			require.Empty(t, results, "one observed usage result per turn")
			require.Empty(t, upstream.writes, "the initial create is sent only once")
		})
	}
}
