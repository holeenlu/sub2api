package handler

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/host"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Use the already-present zap core: zaptest.NewLogger imports clock, which is
// not in this branch's readonly module graph. Keep diagnostics test-local and
// immediate without replacing the global logger or changing module files.
type wsNativeFixtureLogWriter struct{ t *testing.T }

func (w wsNativeFixtureLogWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Log(strings.TrimSuffix(string(p), "\n"))
	return len(p), nil
}

func newWSNativeFixtureLogger(t *testing.T) *zap.Logger {
	t.Helper()
	return zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
		zapcore.Lock(zapcore.AddSync(wsNativeFixtureLogWriter{t: t})),
		zap.DebugLevel,
	))
}

// Explicit in-memory substitute for the native intent ports. Receipts confirm
// only local fixture state, never PostgreSQL durability. Technical scopes/facts
// are separate from canonical records; only real log-only commands reach the
// existing log sink. Monetary commands stay pending without fabricating Apply.
type wsNativeFixtureJournal struct {
	service.UsageBillingRepository
	mu                sync.Mutex
	records           map[billing.Identity]*service.NativeUsageEvidenceRecord
	intents           map[billing.NativeIntentKey]*service.NativeUsageIntentRecord
	finalizations     map[billing.NativeIntentKey]*service.NativeIntentFinalizationRecord
	closures          map[billing.NativeIntentKey]*service.NativeIntentNotSentRecord
	prepareRecovery   func(context.Context, *service.NativeIntentRecoveryInput) (*service.NativeIntentFinalizationInput, error)
	calls, applyCalls int
	logSink           service.UsageLogRepository
}

var _ service.NativeUsageIntentRepository = (*wsNativeFixtureJournal)(nil)
var _ service.NativeIntentFinalizationRepository = (*wsNativeFixtureJournal)(nil)

func (r *wsNativeFixtureJournal) Apply(context.Context, *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.mu.Lock()
	r.applyCalls++
	r.mu.Unlock()
	return nil, errors.New("WS fixture forbids a second/legacy monetary Apply")
}

// Caller holds mu and has preserved/validated the technical finalization first.
func (r *wsNativeFixtureJournal) recordCanonicalLocked(ctx context.Context, input *service.NativeUsageEvidenceInput, mode service.UsageBillingMode) (service.NativeUsageRecordResult, error) {
	var empty service.NativeUsageRecordResult
	if input.Evidence.Identity.RequestID == "" {
		return empty, billing.ErrNativeUsageEvidence
	}
	prepared, err := service.PrepareNativeUsageEvidence(*input)
	if err != nil {
		return empty, err
	}
	if r.records == nil {
		r.records = make(map[billing.Identity]*service.NativeUsageEvidenceRecord)
	}
	id := prepared.Evidence.Identity
	prior := r.records[id]
	record, err := service.MergeNativeUsageRecord(prior, prepared)
	if err != nil {
		return empty, err
	}
	disposition := billing.SettlementPending
	if prepared.Settlement != nil && prepared.Settlement.NeedsPricing() {
		disposition = billing.NativePendingPricing
	}
	if prior != nil && prior.Receipt.Disposition == billing.NativeSettled {
		disposition = billing.NativeSettled
	}
	record.Receipt = billing.NativeUsageReceipt{Identity: id, EvidenceFingerprint: record.Evidence.Fingerprint(), Disposition: disposition}
	if !record.Receipt.Durable() || record.Receipt.EvidenceFingerprint != prepared.Evidence.Fingerprint() {
		return empty, billing.ErrNativeUsageConflict
	}
	// Binding is distinct from the log write, just as the technical receipt is
	// distinct from this canonical receipt. A failed sink must remain pending.
	r.records[id] = record
	result := service.NativeUsageRecordResult{Receipt: record.Receipt}
	command := prepared.Settlement
	if command == nil || disposition == billing.NativeSettled || command.NeedsPricing() || mode != service.UsageBillingModeLogOnly || r.logSink == nil {
		return result, nil
	}
	logOnly := command.Platform == "" && command.SubscriptionID == nil && !command.BalanceExempt
	for _, amount := range []float64{command.BalanceCost, command.SubscriptionCost, command.APIKeyQuotaCost, command.APIKeyRateLimitCost, command.AccountQuotaCost} {
		logOnly = logOnly && amount == 0
	}
	if !logOnly {
		return result, billing.ErrNativeIntentConflict
	}
	logs := command.UsageLogs
	if len(logs) == 0 {
		logs = []*service.UsageLog{command.UsageLog}
	}
	for _, log := range logs {
		if _, err := r.logSink.Create(ctx, log); err != nil {
			return result, err
		}
	}
	record.Receipt.Disposition = billing.NativeSettled
	result.Receipt = record.Receipt
	return result, nil
}

func (r *wsNativeFixtureJournal) LoadNativeUsageEvidence(ctx context.Context, id billing.Identity) (*service.NativeUsageEvidenceRecord, error) {
	if err := wsNativeFixtureContext(ctx, false); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.records[id]
	if record == nil {
		return nil, billing.ErrNativeUsageEvidence
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	var copy service.NativeUsageEvidenceRecord
	if err := json.Unmarshal(raw, &copy); err != nil {
		return nil, err
	}
	copy.Receipt = record.Receipt
	return &copy, nil
}

func (r *wsNativeFixtureJournal) assertNoLegacyApply(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Zero(t, r.applyCalls, "native journal must never be followed by legacy Apply")
}

func (r *wsNativeFixtureJournal) counts() (int, int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Keep the existing canonical-row / finalization-write / legacy-Apply
	// readers. Preserved scopes and technical-only facts are not money rows.
	return len(r.records), r.calls, r.applyCalls
}

func (r *wsNativeFixtureJournal) identities() []billing.Identity {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]billing.Identity, 0, len(r.records))
	for id := range r.records {
		ids = append(ids, id)
	}
	return ids
}

func (r *wsNativeFixtureJournal) assertUsageLogs(t *testing.T, logs []*service.UsageLog) {
	t.Helper()
	r.assertNoLegacyApply(t)
	for _, log := range logs {
		id := billing.Identity{RequestID: log.RequestID, APIKeyID: log.APIKeyID}
		record, err := r.LoadNativeUsageEvidence(context.Background(), id)
		require.NoError(t, err, "usage row must refer to the same committed native journal identity")
		require.Equal(t, id, record.Receipt.Identity)
		require.Equal(t, record.Evidence.Fingerprint(), record.Receipt.EvidenceFingerprint)
		require.True(t, record.Receipt.Durable())
		require.Equal(t, billing.NativeUsageFinal, record.Evidence.Confidence)
		require.True(t, record.Evidence.TerminalConfirmed)
		require.Equal(t, log.UserID, record.Evidence.UserID)
		require.Equal(t, log.AccountID, record.Evidence.Prepared.ResourceID)
		require.NotNil(t, record.Intent, "canonical logs must bind an actual before-send intent")
		recovery, err := r.LoadNativeIntentRecovery(context.Background(), record.Intent.Key)
		require.NoError(t, err)
		require.Equal(t, *record.Intent, recovery.Finalization.Intent)
		require.Equal(t, record.Evidence.Fingerprint(), recovery.Finalization.Evidence.Evidence.Fingerprint())
		require.False(t, recovery.Finalization.Evidence.Receipt.Durable(), "technical facts are not a canonical receipt")
		if log.GroupID != nil {
			require.Equal(t, *log.GroupID, record.Evidence.GroupID)
		}
	}
}

// The application request owner normally injects this technical lifetime port
// (cmd/server/application.go). A handler-only httptest server bypasses that
// wrapper, so it must provide an explicit owner too. This grants no capacity,
// authorization, dispatch permit or execution budget.
type wsNativeFixtureAdmission struct {
	mu                       sync.Mutex
	enabled                  bool
	accepted, released, live int
}

var _ host.TurnAdmission = (*wsNativeFixtureAdmission)(nil)

func (a *wsNativeFixtureAdmission) EnableTurnTracking() {
	a.mu.Lock()
	a.enabled = true
	a.mu.Unlock()
}

func (a *wsNativeFixtureAdmission) AdmitTurn() (func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.enabled {
		return nil, errors.New("fixture turn tracking was not enabled by ingress")
	}
	a.accepted++
	a.live++
	// Deliberately non-idempotent: the production admission owns exactly-once
	// release, including discarded queued frames and failed first-frame binding.
	return func() { a.mu.Lock(); a.released++; a.live--; a.mu.Unlock() }, nil
}

func (a *wsNativeFixtureAdmission) snapshot() (bool, int, int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.enabled, a.accepted, a.released, a.live
}

func (a *wsNativeFixtureAdmission) assertClosed(t *testing.T, expectAdmission bool) {
	t.Helper()
	enabled, accepted, released, live := a.snapshot()
	require.True(t, enabled, "physical ingress must enable turn tracking")
	if expectAdmission {
		require.Positive(t, accepted, "fixture must reach real turn admission")
	}
	require.Equal(t, accepted, released, "every admitted/queued create must release exactly once")
	require.Zero(t, live, "handler joined with a live admission")
}

// The WS fixture uses the same authoritative record for cached ingress auth
// and uncached dispatch checks, returning independent snapshots on both paths.
func (s *wsGroupPricingAPIKeyRepoStub) GetByID(ctx context.Context, id int64) (*service.APIKey, error) {
	if s.beforeDispatchRead != nil {
		s.beforeDispatchRead(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	if id != s.apiKey.ID {
		return nil, service.ErrAPIKeyNotFound
	}
	return s.snapshot(), nil
}

// Caller holds mu. Never expose maps/slices to concurrent lane consumers.
func (s *wsGroupPricingAPIKeyRepoStub) snapshot() *service.APIKey {
	key := s.apiKey
	key.IPWhitelist = slices.Clone(key.IPWhitelist)
	key.IPBlacklist = slices.Clone(key.IPBlacklist)
	if key.User != nil {
		user := *key.User
		user.AllowedGroups = slices.Clone(user.AllowedGroups)
		key.User = &user
	}
	if key.GroupID != nil {
		id := *key.GroupID
		key.GroupID = &id
		group := s.group
		group.ModelAllowlist.Models = slices.Clone(group.ModelAllowlist.Models)
		key.Group = &group
	}
	return &key
}

type wsNativeFixtureUsers struct {
	service.UserRepository
	keys  *service.APIKeyService
	keyID int64
}

type wsNativeFixtureSubscriptions struct {
	service.UserSubscriptionRepository
}

func (*wsNativeFixtureSubscriptions) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*service.UserSubscription, error) {
	return nil, errors.New("WS fixture has no provisioned subscription")
}

func (r *wsNativeFixtureUsers) GetByID(ctx context.Context, id int64) (*service.User, error) {
	key, err := r.keys.GetByID(ctx, r.keyID)
	if err != nil {
		return nil, err
	}
	if key == nil || key.User == nil || key.User.ID != id {
		return nil, service.ErrUserNotFound
	}
	return key.User, nil
}

func cloneWSNativeFixtureAccount(account service.Account) service.Account {
	account.Credentials = maps.Clone(account.Credentials)
	account.Extra = maps.Clone(account.Extra)
	account.GroupIDs = slices.Clone(account.GroupIDs)
	return account
}

func (s *openAIWSUsageHandlerAccountRepoStub) ValidateVendorSupply(ctx context.Context, account *service.Account, groupID *int64) error {
	if account == nil {
		return service.ErrAccountNotFound
	}
	current, err := s.GetByID(ctx, account.ID)
	if err != nil {
		return err
	}
	// This fixture provisions ordinary owned accounts, never supplier grants.
	if current == nil || current.VendorID != nil || account.VendorID != nil || !current.IsActive() || !current.Schedulable || current.Platform != account.Platform {
		return service.ErrVendorSupplyUnavailable
	}
	if groupID != nil && !slices.Contains(current.GroupIDs, *groupID) {
		return service.ErrGroupNotAllowed
	}
	return nil
}

func (s *openAIWSUsageHandlerAccountRepoStub) ValidateVendorAuthorizationMode(ctx context.Context, account *service.Account, groupID *int64, mode string) error {
	if mode != service.VendorOpenAIModeNative {
		return service.ErrVendorSupplyUnavailable
	}
	return s.ValidateVendorSupply(ctx, account, groupID)
}

// Delegate token storage and expiry to the existing Redis implementation.
// Legacy case callbacks remain admission predicates, never lease evidence.
type wsNativeFixtureCapacity struct {
	service.ConcurrencyCache
	routing.CapacityLeaseStore
	service.APIKeyConcurrencyCache
	routing.IngressLeaseStore
	overrides     *concurrencyCacheMock
	beforeAcquire func(context.Context, bool)
}

func newWSNativeFixtureCapacity(t *testing.T, overrides *concurrencyCacheMock, before func(context.Context, bool)) *wsNativeFixtureCapacity {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	cache := repository.NewConcurrencyCache(rdb, 5, 30)
	return &wsNativeFixtureCapacity{ConcurrencyCache: cache, CapacityLeaseStore: cache.(routing.CapacityLeaseStore),
		APIKeyConcurrencyCache: cache.(service.APIKeyConcurrencyCache), IngressLeaseStore: cache.(routing.IngressLeaseStore),
		overrides: overrides, beforeAcquire: before}
}

func (s *wsNativeFixtureCapacity) AcquireUserSlot(ctx context.Context, id int64, limit int, token string) (bool, error) {
	if s.beforeAcquire != nil {
		s.beforeAcquire(ctx, true)
	}
	if s.overrides != nil && s.overrides.acquireUserSlotFn != nil {
		if ok, err := s.overrides.acquireUserSlotFn(ctx, id, limit, token); err != nil || !ok {
			return ok, err
		}
	}
	return s.ConcurrencyCache.AcquireUserSlot(ctx, id, limit, token)
}

func (s *wsNativeFixtureCapacity) AcquireAccountSlot(ctx context.Context, id int64, limit int, token string) (bool, error) {
	if s.beforeAcquire != nil {
		s.beforeAcquire(ctx, false)
	}
	if s.overrides != nil && s.overrides.acquireAccountSlotFn != nil {
		if ok, err := s.overrides.acquireAccountSlotFn(ctx, id, limit, token); err != nil || !ok {
			return ok, err
		}
	}
	return s.ConcurrencyCache.AcquireAccountSlot(ctx, id, limit, token)
}

func (s *wsNativeFixtureCapacity) ReleaseUserSlot(ctx context.Context, id int64, token string) error {
	err := s.ConcurrencyCache.ReleaseUserSlot(ctx, id, token)
	if s.overrides != nil {
		err = errors.Join(err, s.overrides.ReleaseUserSlot(ctx, id, token))
	}
	return err
}

func (s *wsNativeFixtureCapacity) ReleaseAccountSlot(ctx context.Context, id int64, token string) error {
	err := s.ConcurrencyCache.ReleaseAccountSlot(ctx, id, token)
	if s.overrides != nil {
		err = errors.Join(err, s.overrides.ReleaseAccountSlot(ctx, id, token))
	}
	return err
}

func waitWSHandlerFixture(t *testing.T, done <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
