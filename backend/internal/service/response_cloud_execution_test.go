package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/stretchr/testify/require"
)

// This fixture drives the real Redis port contract but is not Redis evidence;
// the repository tests exercise the production atomic scripts separately.
type cloudExecutionTestCache struct {
	GatewayCache
	*cloudLiveRoutingStore
	// Public ingress fixtures may route several sessions through the same
	// execution state, exactly as the shared Redis namespace does.
	sessionState OpenAICloudSessionStore
	execMu       sync.Mutex
	entries      map[string]*cloudExecutionTestEntry
	checks       int
}

func (c *cloudExecutionTestCache) executionSessions() OpenAICloudSessionStore {
	if c.sessionState != nil {
		return c.sessionState
	}
	return c.cloudLiveRoutingStore
}

type cloudExecutionTestEntry struct {
	session, operation, owner, state string
	digest                           [32]byte
	returned                         *execution.OperationReturn
}

func (c *cloudExecutionTestCache) OfferCloudExecution(ctx context.Context, session, command, op string, digest [32]byte) error {
	c.execMu.Lock()
	defer c.execMu.Unlock()
	r, err := c.executionSessions().LoadCloudSession(ctx, session)
	if err != nil || r == nil {
		return execution.ErrDispatchChanged
	}
	if c.entries[command] != nil {
		return execution.ErrOperationConsumed
	}
	c.entries[command] = &cloudExecutionTestEntry{session: session, operation: op, owner: r.OwnerToken, state: "offered", digest: digest}
	return nil
}
func (c *cloudExecutionTestCache) check(ctx context.Context, claim CloudExecutionClaim, take bool) (time.Duration, error) {
	c.execMu.Lock()
	defer c.execMu.Unlock()
	entry := c.entries[claim.CommandID]
	r, err := c.executionSessions().LoadCloudSession(ctx, claim.SessionID)
	if err != nil || r == nil {
		return 0, execution.ErrDispatchChanged
	}
	closed, err := c.executionSessions().CloudRequestClosed(ctx, claim.CommandID)
	if err != nil || closed || entry == nil || entry.session != claim.SessionID || entry.operation != claim.OperationID || entry.digest != claim.TransferDigest || entry.owner != claim.OwnerToken || r.OwnerToken != claim.OwnerToken || !time.Now().Before(r.LeaseUntil) {
		return 0, execution.ErrDispatchChanged
	}
	if take {
		if entry.state != "offered" {
			return 0, execution.ErrOperationConsumed
		}
		entry.state = "active"
	} else if entry.state != "active" {
		return 0, execution.ErrOperationConsumed
	}
	c.checks++
	return time.Until(r.LeaseUntil), nil
}
func (c *cloudExecutionTestCache) ClaimCloudExecution(ctx context.Context, claim CloudExecutionClaim) (time.Duration, error) {
	return c.check(ctx, claim, true)
}
func (c *cloudExecutionTestCache) CheckCloudExecution(ctx context.Context, claim CloudExecutionClaim) (time.Duration, error) {
	return c.check(ctx, claim, false)
}
func (c *cloudExecutionTestCache) SaveCloudExecutionReturn(_ context.Context, claim CloudExecutionClaim, r execution.OperationReturn) error {
	c.execMu.Lock()
	defer c.execMu.Unlock()
	e := c.entries[claim.CommandID]
	if e == nil || e.owner != claim.OwnerToken || e.digest != r.TransferDigest || e.state != "active" {
		return execution.ErrDispatchChanged
	}
	copy := cloudLiveClone(r)
	e.returned = &copy
	e.state = "returned"
	return nil
}
func (c *cloudExecutionTestCache) LoadCloudExecutionReturn(_ context.Context, session, command string, digest [32]byte) (*execution.OperationReturn, error) {
	c.execMu.Lock()
	defer c.execMu.Unlock()
	e := c.entries[command]
	if e == nil || e.session != session || e.digest != digest {
		return nil, execution.ErrDispatchChanged
	}
	if e.returned == nil {
		return nil, nil
	}
	copy := cloudLiveClone(*e.returned)
	return &copy, nil
}
func (c *cloudExecutionTestCache) ClaimCloudExecutionReturn(ctx context.Context, session, command string, transfer, returned [32]byte) error {
	c.execMu.Lock()
	defer c.execMu.Unlock()
	e := c.entries[command]
	closed, _ := c.executionSessions().CloudRequestClosed(ctx, command)
	if closed || e == nil || e.session != session || e.digest != transfer || e.state != "returned" || e.returned == nil || e.returned.Digest() != returned {
		return execution.ErrOperationConsumed
	}
	e.state = "reclaimed"
	return nil
}

type cloudExecutionAccounts struct {
	*nativeAuthorityAccounts
	binding *OpenAICloudAuthorizationBinding
}

func (r *cloudExecutionAccounts) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.err != nil {
		return nil, r.err
	}
	account := r.accounts[id]
	if account == nil {
		return nil, ErrAccountNotFound
	}
	copy := *account
	return &copy, nil
}
func (r *cloudExecutionAccounts) ValidateVendorSupply(context.Context, *Account, *int64) error {
	return r.supplyErr
}

func (r *cloudExecutionAccounts) GetCloudAuthorization(context.Context, int64) (*OpenAICloudAuthorizationBinding, error) {
	copy := *r.binding
	return &copy, nil
}
func (r *cloudExecutionAccounts) StoreCloudAuthorization(context.Context, *Account, string, *OpenAICloudAuthorizationBinding) (bool, error) {
	return false, errors.New("unexpected grant write")
}
func (r *cloudExecutionAccounts) ValidateVendorAuthorizationMode(context.Context, *Account, *int64, string) error {
	return r.modeErr
}

type cloudExecutionCapacity struct {
	ConcurrencyCache
	*nativeWSCapacityStore
}

func (c *cloudExecutionCapacity) AcquireAccountSlot(ctx context.Context, id int64, limit int, token string) (bool, error) {
	return c.nativeWSCapacityStore.AcquireAccountSlot(ctx, id, limit, token)
}
func (c *cloudExecutionCapacity) AcquireUserSlot(ctx context.Context, id int64, limit int, token string) (bool, error) {
	return c.nativeWSCapacityStore.AcquireUserSlot(ctx, id, limit, token)
}
func (c *cloudExecutionCapacity) ReleaseAccountSlot(ctx context.Context, id int64, token string) error {
	return c.nativeWSCapacityStore.ReleaseAccountSlot(ctx, id, token)
}
func (c *cloudExecutionCapacity) ReleaseUserSlot(ctx context.Context, id int64, token string) error {
	return c.nativeWSCapacityStore.ReleaseUserSlot(ctx, id, token)
}

type cloudExecutionFixture struct {
	ctx      context.Context
	a        *cloudSessionActor
	command  OpenAICloudCommand
	source   *cloudResponseSource
	cache    *cloudExecutionTestCache
	keys     *cloudForwardKeys
	repo     *cloudExecutionAccounts
	capacity *nativeWSCapacityStore
	wire     *cloudRPCFakeConn
}

func newCloudExecutionFixture(t *testing.T) *cloudExecutionFixture {
	t.Helper()
	f := newNativeAuthorityFixture(t)
	f.resource.Type = AccountTypeOAuth
	f.resource.Concurrency = 1
	f.resource.Credentials = map[string]any{"chatgpt_account_id": "workspace", "chatgpt_user_id": "user", "model_mapping": map[string]any{"client-model": "gpt-5.1"}}
	f.resource.Extra = map[string]any{OpenAICloudEnabledKey: true}
	f.key.User.Concurrency = 1
	repo := &cloudExecutionAccounts{nativeAuthorityAccounts: f.accounts, binding: &OpenAICloudAuthorizationBinding{Generation: "grant", AccountID: "workspace", UserID: "user", Subject: "subject"}}
	cache := &cloudExecutionTestCache{GatewayCache: &stubGatewayCache{}, cloudLiveRoutingStore: newCloudLiveRoutingStore(), entries: make(map[string]*cloudExecutionTestEntry)}
	capacity := &nativeWSCapacityStore{}
	svc := &OpenAIGatewayService{accountRepo: repo, userRepo: &cloudBillingUserStub{user: f.key.User}, cache: cache, concurrencyService: &ConcurrencyService{cache: &cloudExecutionCapacity{nativeWSCapacityStore: capacity}}}
	svc.cloudService = NewOpenAICloudService(repo, nil, nil)
	keys := &cloudForwardKeys{key: f.key}
	svc.SetCloudBillingQuotaUpdater(&APIKeyService{apiKeyRepo: keys})
	ctx, scope := routing.WithCapacityScope(t.Context())
	for _, r := range []routing.CapacityRequest{{ID: f.key.UserID, User: true, Token: "user-lease", Limit: 1}, {ID: f.resource.ID, Token: "account-lease", Limit: 1}} {
		lease, err := routing.AcquireCapacity(ctx, capacity, r, nil)
		require.NoError(t, err)
		require.True(t, lease.Acquired)
		t.Cleanup(lease.ReleaseFunc)
	}
	leases, err := svc.NativeDispatchCapacity(scope)
	require.NoError(t, err)
	f.gate.leases = leases
	f.gate.accounts = repo
	f.gate.keys = &APIKeyService{apiKeyRepo: keys}
	budget := NewOpenAITurnBudget(nil)
	ctx = WithOpenAITurnBudget(ctx, budget)
	dispatch := &nativeDispatchOperation{request: f.gate.request, authority: f.gate, budget: budget}
	dispatch.dispatcher, err = execution.NewDispatcher(budget.control, f.gate.request.OperationID, budget.attemptBudget, budget.firstOutput, dispatch)
	require.NoError(t, err)
	raw := json.RawMessage(`{"model":"gpt-5.1","input":"hello"}`)
	core, err := execution.NewOperation(dispatch.dispatcher, model.OperationResult{OperationID: f.gate.request.OperationID, UserID: f.key.UserID, APIKeyID: f.key.ID, GroupBound: true, GroupID: *f.key.GroupID, Protocol: model.ProtocolResponses, OriginalWireDigest: sha256.Sum256(raw)})
	require.NoError(t, err)
	require.NoError(t, core.Begin(ctx))
	owner := &nativeHTTPOperation{core: core, dispatch: dispatch}
	ctx = context.WithValue(ctx, nativeHTTPOperationKey{}, owner)
	source, err := svc.bindResponseCloud(ctx)
	require.NoError(t, err)
	cloudOwner := OpenAICloudOwner{UserID: f.key.UserID, APIKeyID: f.key.ID, GroupID: *f.key.GroupID, AccountID: f.resource.ID, GrantGeneration: "grant", Model: "gpt-5.1", SettingsDigest: cloudSettingsDigest(f.resource)}
	record := &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: cloudOwner, SessionID: "session", RootRequestID: "original-root", OwnerInstance: "owner", ExpiresAt: time.Now().Add(time.Minute), Status: "created"}, OwnerToken: "actual-owner-token", LeaseUntil: time.Now().Add(time.Minute), BillingSnapshot: json.RawMessage(`{}`)}
	require.NoError(t, cache.CreateCloudSession(ctx, record))
	wire := newCloudRPCFakeConn()
	rpc := newCloudRPC(ctx, wire)
	cloudTestInitialize(t, ctx, rpc, wire)
	sessions := &OpenAICloudSessions{executionGateway: svc, store: cache, validate: svc.validateCloudSessionOwner}
	a := &cloudSessionActor{service: sessions, record: record, ctx: ctx, rpc: rpc, tools: make(map[string]*cloudPendingTool), completed: make(map[string]*OpenAICloudResult)}
	h, err := source.transfer(ctx, "command", record.AccountID)
	require.NoError(t, err)
	require.NoError(t, cache.OfferCloudExecution(ctx, record.SessionID, "command", h.Execution.Operation.OperationID, h.Execution.Digest()))
	command := OpenAICloudCommand{RequestID: "command", Execution: &h, Request: OpenAICloudRequest{Owner: cloudOwner, Body: raw, RootRequestID: "original-root", BillingSnapshot: json.RawMessage(`{}`)}}
	require.NoError(t, a.beginCloudExecution(command))
	t.Cleanup(func() { a.execution.stopMonitor(); _ = wire.Close(); _ = rpc.Close(); a.children.Wait() })
	return &cloudExecutionFixture{ctx: ctx, a: a, command: command, source: source, cache: cache, keys: keys, repo: repo, capacity: capacity, wire: wire}
}

func TestCloudExecutionActualActorStartAndTerminalUseSameBudgetAndRoot(t *testing.T) {
	f := newCloudExecutionFixture(t)
	done := make(chan error, 1)
	go func() { done <- f.a.handleCommand(f.command) }()
	cloudWireResponse(t, f.ctx, f.wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
	cloudWireResponse(t, f.ctx, f.wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
	require.NoError(t, <-done)
	for _, raw := range []string{`{"method":"thread/tokenUsage/updated","params":{"threadId":"thread","turnId":"turn","tokenUsage":{"total":{"inputTokens":10,"outputTokens":2,"cachedInputTokens":0,"cacheWriteInputTokens":0,"totalTokens":12}}}}`, `{"method":"turn/completed","params":{"threadId":"thread","turn":{"id":"turn","status":"completed"}}}`} {
		var event cloudRPCMessage
		require.NoError(t, json.Unmarshal([]byte(raw), &event))
		require.NoError(t, f.a.handleEvent(event))
	}
	require.NotNil(t, f.a.execution.returned)
	r := *f.a.execution.returned
	require.Equal(t, 1, r.Budget.Attempts)
	require.Equal(t, "original-root", r.Result.Attempts[1].BillingRoot)
	require.Equal(t, model.BillingUsageRecorded, r.Result.Billing.Disposition)
	require.NotEqual(t, model.BillingSettled, r.Result.Billing.Disposition)
	require.NoError(t, f.source.core.CompleteTransfer(r))
	require.Equal(t, 1, f.source.budget.attemptBudget.Snapshot().Attempts)
	require.Zero(t, f.source.core.Snapshot().DelegatedCycles)
}

func TestCloudExecutionRPCWaitRevalidatesKeyGrantLeaseAndCancel(t *testing.T) {
	for _, name := range []string{"key", "grant", "capacity", "owner", "cancel"} {
		t.Run(name, func(t *testing.T) {
			f := newCloudExecutionFixture(t)
			f.a.execution.stopMonitor() // This case isolates the post-write-lock send check.
			require.NoError(t, f.a.rpc.write.Lock(f.ctx))
			done := make(chan error, 1)
			go func() {
				_, err := f.a.rpc.Call(f.ctx, "turn/start", map[string]any{"threadId": "thread", "input": []any{}})
				done <- err
			}()
			require.Eventually(t, func() bool { return f.a.execution.controller.budget.attemptBudget.Snapshot().Attempts == 1 }, time.Second, time.Millisecond)
			switch name {
			case "key":
				f.keys.key.Status = StatusDisabled
			case "grant":
				f.repo.binding.Generation = "replaced"
			case "capacity":
				require.NoError(t, f.capacity.ReleaseAccountSlot(f.ctx, 7, "account-lease"))
			case "owner":
				f.cache.mu.Lock()
				f.cache.record.OwnerToken = "replacement"
				f.cache.mu.Unlock()
			case "cancel":
				require.NoError(t, f.cache.DeleteCloudEvents(f.ctx, "command"))
			}
			f.a.rpc.write.Unlock()
			require.Error(t, <-done)
			require.Equal(t, model.SendNotSent, f.a.execution.controller.Snapshot().Attempts[0].Send)
			select {
			case raw := <-f.wire.writes:
				t.Fatalf("revoked send reached wire: %s", raw)
			default:
			}
		})
	}
}

func TestCloudExecutionToolReplyBatchOneResumeAndOriginalRoot(t *testing.T) {
	f := newCloudExecutionFixture(t)
	frames := []json.RawMessage{json.RawMessage(`{"id":"tool-one","result":{"contentItems":[],"success":true}}`), json.RawMessage(`{"id":"tool-two","result":{"contentItems":[],"success":true}}`)}
	f.a.record.ThreadID, f.a.record.TurnID = "thread", "turn"
	require.NoError(t, f.a.replyCloudBatch(f.ctx, frames))
	require.Equal(t, 1, f.a.execution.controller.budget.attemptBudget.Snapshot().Attempts)
	require.Len(t, f.a.execution.controller.Snapshot().Attempts, 1)
	a := f.a.execution.controller.Snapshot().Attempts[0]
	require.Equal(t, model.AttemptResume, a.Identity.Role)
	require.Equal(t, "original-root", a.Identity.Binding.BillingRoot)
	require.JSONEq(t, string(frames[0]), string(<-f.wire.writes))
	require.JSONEq(t, string(frames[1]), string(<-f.wire.writes))
	f.a.execution.controller.Cancel(context.Canceled)
	require.Error(t, f.a.replyCloudBatch(f.ctx, frames))
}

func TestCloudExecutionOwnerRejectsDuplicateResumeAndMissingGatewayAuthority(t *testing.T) {
	f := newCloudExecutionFixture(t)
	_, _, err := ResumeCloudResponseExecution(f.ctx, *f.command.Execution, f.a.execution.authority)
	require.Error(t, err)
	f.a.execution.stopMonitor()
	f.a.execution.returned = &execution.OperationReturn{}
	copy := f.command
	copy.Execution = nil
	require.ErrorIs(t, f.a.beginCloudExecution(copy), execution.ErrDispatchUnavailable)
	f.a.service.executionGateway.SetCloudBillingQuotaUpdater(&cloudBillingKeyStub{})
	_, err = f.a.service.executionGateway.bindResponseCloud(f.ctx)
	require.ErrorIs(t, err, execution.ErrDispatchUnavailable)
}
