package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/catalog"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The legacy protocol recorders are local peers, not dispatch authorities.
// Reuse the real Native bind/prepare/permit/pricing path and acquired capacity
// receipts before invoking their single-response port. The in-memory journal
// validates actual scopes and final facts; it is not a PostgreSQL receipt.
type nativeHTTPBridgeFixture struct {
	service   *OpenAIGatewayService
	key       *APIKey
	keys      *APIKeyService
	capacity  *nativeWSCapacityStore
	transport *nativeOperationTransport
	sequence  atomic.Uint64
}

func newNativeHTTPBridgeFixture(t *testing.T, svc *OpenAIGatewayService, accounts ...*Account) *nativeHTTPBridgeFixture {
	t.Helper()
	a := newNativeAuthorityFixture(t)
	a.key.Group.ModelAllowlist = catalog.ModelAllowlist{}
	a.accounts.accounts = make(map[int64]*Account, len(accounts))
	for _, account := range accounts {
		account.Status, account.Schedulable = StatusActive, true
		account.GroupIDs = []int64{*a.key.GroupID}
		account.Credentials = maps.Clone(account.Credentials)
		if account.Credentials == nil {
			account.Credentials = make(map[string]any)
		}
		if account.Type == AccountTypeOAuth {
			if account.GetOpenAIAccessToken() == "" {
				account.Credentials["access_token"] = "test-token"
			}
			account.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
		} else if account.GetOpenAIProtocolAPIKey() == "" {
			account.Credentials["api_key"] = "test-token"
		}
		a.accounts.accounts[account.ID] = account
	}
	if svc.cfg == nil {
		svc.cfg = &config.Config{}
	}
	if svc.cfg.Gateway.MaxBodySize <= 0 {
		svc.cfg.Gateway.MaxBodySize = ResolveOpenAIWSClientReadLimitBytes(svc.cfg)
	}
	svc.accountRepo = &nativeWSConcurrentAccounts{nativeAuthorityAccounts: a.accounts}
	svc.userRepo = &nativeOperationUsers{user: a.key.User}
	svc.userSubRepo = &nativeOperationSubscriptions{}
	svc.openAITokenProvider = NewOpenAITokenProvider(svc.accountRepo, nil, nil)
	svc.billingService = NewBillingService(svc.cfg, nil)
	journal := &nativeApplicationJournal{t: t}
	svc.usageBillingRepo = journal
	peer := svc.httpUpstream
	require.NotNil(t, peer)
	transport := &nativeOperationTransport{}
	transport.reply = func(req *http.Request, options native.TransportOptions) (*http.Response, error) {
		owner := nativeHTTPFromContext(req.Context())
		if owner == nil || owner.intent == nil || owner.intent.record == nil {
			return nil, errors.New("bridge reached the peer without preserved Native intent")
		}
		sequence := owner.active.Load()
		found := false
		for _, scope := range owner.intent.record.Attempts {
			if scope.Sequence == sequence {
				found = scope.Prepared.ResourceID == options.AccountID && scope.Prepared.Transport == "http" &&
					scope.Prepared.OperationID == owner.dispatch.request.OperationID
				break
			}
		}
		if !found {
			return nil, errors.New("bridge reached the peer without its actual prepared pricing scope")
		}
		if options.TLSProfile != nil {
			return peer.DoWithTLS(req, options.ProxyURL, options.AccountID, options.AccountConcurrency, options.TLSProfile)
		}
		return peer.Do(req, options.ProxyURL, options.AccountID, options.AccountConcurrency)
	}
	svc.httpUpstream = transport
	f := &nativeHTTPBridgeFixture{service: svc, key: a.key,
		keys:     &APIKeyService{apiKeyRepo: &nativeWSConcurrentKeys{nativeAuthorityKeys: a.keys}},
		capacity: &nativeWSCapacityStore{}, transport: transport}
	t.Cleanup(func() { require.Zero(t, transport.legacy.Load(), "Native must use the permitted single-send port") })
	return f
}

func (f *nativeHTTPBridgeFixture) bind(parent context.Context, c *gin.Context, account *Account, payload []byte) (context.Context, func(), error) {
	return f.bindTransport(parent, c, account, payload, OpenAIClientTransportWS)
}

func (f *nativeHTTPBridgeFixture) bindTransport(parent context.Context, c *gin.Context, account *Account, payload []byte, transport OpenAIClientTransport) (context.Context, func(), error) {
	request := NativeDispatchRequest{OperationID: fmt.Sprintf("bridge-fixture-%d", f.sequence.Add(1)),
		UserID: f.key.UserID, APIKeyID: f.key.ID, APIKeyDigest: sha256.Sum256([]byte(f.key.Key)),
		GroupID: *f.key.GroupID, GroupBound: true, RequestedModel: gjson.GetBytes(payload, "model").String(),
		ClientIP: "192.0.2.8", Transport: string(transport)}
	ctx := EnsureOpenAITurnBudget(parent, f.service.cfg)
	var err error
	if transport == OpenAIClientTransportWS {
		ctx, err = f.service.BindNativeWSDispatch(ctx, f.keys, request)
	} else {
		ctx, err = f.service.BindNativeHTTPDispatch(ctx, f.keys, request)
	}
	if err != nil {
		return nil, func() {}, err
	}
	if transport == OpenAIClientTransportWS {
		ctx, err = f.service.BindNativeWSOperation(ctx, payload)
	} else {
		ctx, err = f.service.BindNativeHTTPOperation(ctx, model.ProtocolResponses, sha256.Sum256(payload))
	}
	if err != nil {
		return nil, func() {}, err
	}
	err = f.service.BindNativeIntentPricing(ctx, NativeIntentPricingInput{APIKey: f.key, User: f.key.User,
		APIKeyService: f.keys, PricingAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC),
		ChannelUsageFields: ChannelUsageFields{OriginalModel: request.RequestedModel}})
	if err != nil {
		return nil, func() {}, err
	}
	// This principal has the explicit unlimited policy. Account limits use
	// actual owned tokens and routing's normal receipt/lease revalidation.
	user, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: f.key.UserID, User: true, Limit: 0}, nil)
	if err != nil {
		return nil, func() {}, err
	}
	slot, err := routing.AcquireCapacity(ctx, f.capacity, routing.CapacityRequest{ID: account.ID,
		Token: request.OperationID, Limit: nativeSchedulingConcurrency(ctx, account, f.service.settingService)}, nil)
	if err != nil || !slot.Acquired {
		user.ReleaseFunc()
		return nil, func() {}, errors.Join(err, errors.New("bridge fixture account capacity unavailable"))
	}
	release := func() { slot.ReleaseFunc(); user.ReleaseFunc() }
	c.Request = c.Request.WithContext(ctx)
	c.Set("api_key", f.key)
	SetOpenAIClientTransport(c, transport)
	return ctx, release, nil
}

// Captured WS-to-HTTP clients start a new HTTP turn on the same authenticated
// principal. Keep the HTTP leg on its actual owner as well as the WS legs.
func (f *nativeHTTPBridgeFixture) forwardHTTP(parent context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
	ctx, release, err := f.bindTransport(parent, c, account, body, OpenAIClientTransportHTTP)
	if err != nil {
		return nil, err
	}
	defer release()
	var result *OpenAIForwardResult
	err = RunNativeHTTPOperation(ctx, c, func() NativeOperationStep {
		var forwardErr error
		result, forwardErr = TrackNativeHTTPForward(ctx, func() (*OpenAIForwardResult, error) {
			return f.service.Forward(ctx, c, account, body)
		})
		handled, finalizeErr := f.service.FinalizeNativeHTTPUsage(ctx, &OpenAIRecordUsageInput{
			Result: result, APIKey: f.key, User: f.key.User, Account: account, APIKeyService: f.keys,
			RequestPayloadHash: HashUsageRequestPayload(body),
		})
		if !handled {
			finalizeErr = errors.Join(finalizeErr, ErrNativeOperationBillingUnavailable)
		}
		return NativeOperationStep{Err: errors.Join(forwardErr, finalizeErr)}
	})
	return result, err
}

// These peers exercise the provider's real trace-based send classification.
// In particular, a dial failure must not read the request body: the legacy
// recorder did so even on errors, which is evidence of a possible send.
type nativeHTTPBridgeFailurePeer struct {
	state         model.SendState
	cause         error
	waitForCancel bool
	calls         atomic.Int32
}

func (p *nativeHTTPBridgeFailurePeer) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	p.calls.Add(1)
	trace := httptrace.ContextClientTrace(req.Context())
	if trace != nil {
		switch p.state {
		case model.SendNotSent:
			trace.GetConn(req.URL.Host)
		case model.SendSent:
			trace.WroteHeaders()
			trace.WroteRequest(httptrace.WroteRequestInfo{})
		case model.SendAmbiguous:
			trace.WroteHeaderField("Content-Type", []string{"application/json"})
		}
	}
	if p.waitForCancel {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	return nil, p.cause
}

func (p *nativeHTTPBridgeFailurePeer) DoWithTLS(req *http.Request, proxy string, account int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return p.Do(req, proxy, account, concurrency)
}

func (f *nativeHTTPBridgeFixture) turn(t *testing.T, parent context.Context, c *gin.Context, account *Account, payload []byte) (context.Context, *gin.Context, string) {
	t.Helper()
	if c == nil {
		c, _ = gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	}
	ctx, release, err := f.bind(parent, c, account, payload)
	require.NoError(t, err)
	t.Cleanup(release)
	token, _, err := f.service.GetAccessToken(ctx, account)
	require.NoError(t, err)
	return ctx, c, token
}

// Mirror the handler's per-turn bind, publication and synchronous finalizer.
// Re-entry for account failover consumes the existing downstream/root/budget.
func (f *nativeHTTPBridgeFixture) proxy(parent context.Context, c *gin.Context, conn OpenAIWSDownstreamConn, account *Account, first []byte, hooks *OpenAIWSIngressHooks) error {
	ds, managed := conn.(*OpenAIWSDownstream)
	if !managed {
		if cached, ok := c.Get("openai_ws_downstream_owner"); ok {
			ds, _ = cached.(*OpenAIWSDownstream)
		}
		if ds == nil {
			ds = NewOpenAIWSDownstream(conn, ResolveOpenAIWSClientReadLimitBytes(f.service.cfg), parent)
			c.Set("openai_ws_downstream_owner", ds)
		}
	}
	var current context.Context
	release := func() {}
	defer func() { release() }()
	bind := func(body []byte) error {
		release()
		var err error
		current, release, err = f.bind(ds.TurnBudgetContext(parent, f.service.cfg, body), c, account, body)
		if err != nil {
			return err
		}
		return ds.PublishNativeWSTurn(parent, current)
	}
	if err := bind(first); err != nil {
		return err
	}
	combined := OpenAIWSIngressHooks{}
	if hooks != nil {
		combined = *hooks
	}
	beforeRequest, afterTurn, releaseTurn := combined.BeforeRequest, combined.AfterTurn, combined.ReleaseTurn
	combined.BeforeRequest = func(turn int, body []byte, model string) error {
		if beforeRequest != nil {
			if err := beforeRequest(turn, body, model); err != nil {
				return err
			}
		}
		return bind(body)
	}
	var finalizeErr error
	combined.AfterTurn = func(turn int, result *OpenAIForwardResult, cause error) {
		_, err := f.service.FinalizeNativeWSUsage(current, &OpenAIRecordUsageInput{
			Result: result, APIKey: f.key, User: f.key.User, Account: account, APIKeyService: f.keys}, cause)
		finalizeErr = errors.Join(finalizeErr, err)
		if afterTurn != nil {
			afterTurn(turn, result, errors.Join(cause, err))
		}
	}
	combined.ReleaseTurn = func(turn int) {
		release()
		if releaseTurn != nil {
			releaseTurn(turn)
		}
	}
	token, _, err := f.service.GetAccessToken(current, account)
	if err != nil {
		return err
	}
	err = f.service.ProxyResponsesWebSocketFromClient(current, c, conn, account, token, first, &combined)
	return errors.Join(err, finalizeErr)
}

// httptest.Server.Close does not join hijacked WebSockets. Close the client
// before cleanup, then join even if a frame/assertion failed, so the next test
// cannot mutate Gin's global mode while this handler creates its context.
func nativeHTTPBridgeTestHandler(t *testing.T, handler http.HandlerFunc) http.Handler {
	t.Helper()
	var active sync.WaitGroup
	t.Cleanup(func() {
		joined := make(chan struct{})
		go func() { active.Wait(); close(joined) }()
		select {
		case <-joined:
		case <-time.After(3 * time.Second):
			t.Error("Native HTTP bridge handler did not join")
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Done()
		handler(w, r)
	})
}

func startNativeHTTPBridgeMemorySession(t *testing.T, f *nativeHTTPBridgeFixture, account *Account, payload string, hooks *OpenAIWSIngressHooks) (*coderws.Conn, <-chan error) {
	t.Helper()
	svc := f.service
	clientPipe, serverPipe := net.Pipe()
	listener := &nativeWSMemoryListener{conn: serverPipe, done: make(chan struct{})}
	serverErr, joined := make(chan error, 1), make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(joined)
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.CloseNow()
		parent := openAIWSTestAdmissionContext(r.Context())
		conn.SetReadLimit(ResolveOpenAIWSClientReadLimitBytes(svc.cfg))
		_, first, err := ReadOpenAIWSClientMessage(parent, conn, 3*time.Second, coderws.StatusPolicyViolation, "missing first request")
		if err != nil {
			serverErr <- err
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r.Clone(parent)
		serverErr <- f.proxy(parent, c, conn, account, first, hooks)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = clientPipe.Close()
		_ = serverPipe.Close()
		_ = server.Close()
		select {
		case <-joined:
		case <-time.After(3 * time.Second):
			t.Error("Native HTTP bridge handler did not join")
		}
	})
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return clientPipe, nil }}
	t.Cleanup(transport.CloseIdleConnections)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws://native-ws-test/v1/responses", &coderws.DialOptions{HTTPClient: &http.Client{Transport: transport}})
	require.NoError(t, err)
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(payload)))
	return client, serverErr
}
