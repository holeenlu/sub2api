package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
)

type cloudBillingKeyStub struct {
	APIKeyQuotaUpdater
	key *APIKey
	err error
}

func (s *cloudBillingKeyStub) GetByID(context.Context, int64) (*APIKey, error) { return s.key, s.err }

type cloudBillingUserStub struct {
	UserRepository
	user *User
	err  error
}

func (s *cloudBillingUserStub) GetByID(context.Context, int64) (*User, error) { return s.user, s.err }

type cloudBillingStoreStub struct {
	OpenAICloudSessionStore
	pending      []*OpenAICloudSettlement
	claims, acks int
	acked        map[string]bool
	archived     map[string]bool
	failed       map[string]bool
	retained     int
	retainError  error
	ackError     error
}

func (s *cloudBillingStoreStub) ArchiveCloudSettlement(_ context.Context, id, worker string, raw []byte) error {
	if s.retainError != nil {
		return s.retainError
	}
	s.retained++
	if s.archived == nil {
		s.archived = map[string]bool{}
	}
	s.archived[id] = true
	return nil
}
func (s *cloudBillingStoreStub) CloudSettlementArchived(_ context.Context, id string) (bool, error) {
	return s.archived[id], nil
}

type cloudBillingArchiveRepoStub struct {
	*openAIRecordUsageBillingRepoStub
	archives   map[string][]byte
	reasons    map[string]string
	archiveErr error
}

func (r *cloudBillingArchiveRepoStub) StoreCloudSettlementArchive(_ context.Context, root, reason string, payload []byte) error {
	if r.archiveErr != nil {
		return r.archiveErr
	}
	if r.archives == nil {
		r.archives = map[string][]byte{}
		r.reasons = map[string]string{}
	}
	r.archives[root] = append([]byte(nil), payload...)
	r.reasons[root] = reason
	return nil
}
func (r *cloudBillingArchiveRepoStub) HasCloudSettlementArchive(_ context.Context, root string) (bool, error) {
	_, ok := r.archives[root]
	return ok, r.archiveErr
}

func (s *cloudBillingStoreStub) GetCloudSettlement(_ context.Context, id string) (*OpenAICloudSettlement, error) {
	for _, settlement := range s.pending {
		if settlement.RootRequestID == id {
			return settlement, nil
		}
	}
	return nil, nil
}

func (s *cloudBillingStoreStub) ListCloudSettlements(context.Context, int) ([]*OpenAICloudSettlement, error) {
	return s.pending, nil
}
func (s *cloudBillingStoreStub) ClaimCloudSettlement(_ context.Context, id, worker string, ttl time.Duration) (bool, error) {
	s.claims++
	return !s.acked[id] && !s.failed[id] && !s.archived[id], nil
}
func (s *cloudBillingStoreStub) AckCloudSettlement(_ context.Context, id, worker string, payload []byte) error {
	s.acks++
	if s.ackError != nil {
		return s.ackError
	}
	if s.acked == nil {
		s.acked = make(map[string]bool)
	}
	s.acked[id] = true
	return nil
}
func (s *cloudBillingStoreStub) FailCloudProbeSettlement(_ context.Context, id, worker string) error {
	if s.failed == nil {
		s.failed = make(map[string]bool)
	}
	s.failed[id] = true
	return nil
}
func (s *cloudBillingStoreStub) CloudSettlementAcknowledged(_ context.Context, id string) (bool, error) {
	if s.failed[id] {
		return false, ErrOpenAICloudProbeSettlementFailed
	}
	return s.acked[id], nil
}

func TestCloudLateUsageConsumerArchivesWithoutRebilling(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown_%t", unknown), func(t *testing.T) {
			svc, _, billing := cloudBillingFixture(t)
			archive := svc.usageBillingRepo.(*cloudBillingArchiveRepoStub)
			settlement := &OpenAICloudSettlement{
				OpenAICloudBinding: OpenAICloudBinding{RootRequestID: "late-root", SessionID: "session", ThreadID: "thread", TurnID: "turn"},
				Status:             "completed", Usage: &OpenAICloudUsage{InputTokens: 10, TotalTokens: 10}, Unknown: unknown,
				ReconciliationOnly: true, OriginalPayload: []byte(`{"original":"unchanged"}`),
				LateUsage: []OpenAICloudLateUsage{{Total: &OpenAICloudUsage{InputTokens: 20, TotalTokens: 20}, Conflict: true, ObservedAt: time.Now()}},
			}
			if unknown {
				settlement.Status, settlement.Usage = "unknown", nil
			}
			payload, err := json.Marshal(settlement)
			require.NoError(t, err)
			settlement.ArchivePayload = payload
			store := &cloudBillingStoreStub{pending: []*OpenAICloudSettlement{settlement}}
			worker := &openAICloudBillingWorker{service: svc, store: store, id: "late-worker"}
			require.Error(t, worker.settle(t.Context(), settlement), "even a direct caller cannot turn reconciliation facts into a bill")
			archive.archiveErr = errors.New("PG unavailable")
			worker.consume(t.Context())
			require.Zero(t, billing.calls)
			require.Zero(t, store.acks)
			require.Zero(t, store.retained, "Redis facts survive a failed durable archive")
			archive.archiveErr = nil
			worker.consume(t.Context())
			require.Equal(t, payload, archive.archives["late-root"])
			require.Equal(t, 1, store.retained)
			require.Zero(t, store.acks)
			require.Zero(t, billing.calls, "no second ledger command or revised amount")
			require.Equal(t, unknown, settlement.Unknown)
		})
	}
}

func cloudBillingFixture(t *testing.T) (*OpenAIGatewayService, *OpenAIRecordUsageInput, *openAIRecordUsageBillingRepoStub) {
	t.Helper()
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	user := &User{ID: 11, Role: "user", PasswordHash: "must-not-persist-password", TotpSecretEncrypted: strPtr("must-not-persist-totp")}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &cloudBillingUserStub{user: user}, &openAIRecordUsageSubRepoStub{}, nil)
	repo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: false}}
	svc.usageBillingRepo = &cloudBillingArchiveRepoStub{openAIRecordUsageBillingRepoStub: repo}
	inputPrice, outputPrice, readPrice := 2e-6, 4e-6, 1e-6
	group := &Group{ID: 22, Name: "original-group", Platform: PlatformOpenAI, RateMultiplier: 2,
		ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-5.1"}, BillingMode: BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice, CacheReadPrice: &readPrice}}}
	key := &APIKey{ID: 33, UserID: user.ID, User: user, Key: "must-not-persist-api-key", GroupID: &group.ID, Group: group, Quota: 100, RateLimit5h: 50}
	account := &Account{ID: 44, Name: "original-account", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "must-not-persist-access", "refresh_token": "must-not-persist-refresh"}}
	svc.SetCloudBillingQuotaUpdater(&cloudBillingKeyStub{key: key})
	in := &OpenAIRecordUsageInput{APIKey: key, User: user, Account: account, PricingAt: time.Date(2026, 10, 23, 4, 0, 0, 0, time.UTC),
		Result:             &OpenAIForwardResult{Model: "gpt-5.1", UpstreamModel: "gpt-5.1", Stream: true},
		ChannelUsageFields: ChannelUsageFields{OriginalModel: "gpt-5.1", ChannelMappedModel: "gpt-5.1", BillingModelSource: BillingModelSourceRequested}, QuotaPlatform: PlatformOpenAI}
	return svc, in, repo
}

func TestCloudBillingSnapshotWhitelistAndRestartPricing(t *testing.T) {
	svc, input, repo := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(context.Background(), input)
	require.NoError(t, err)
	ctx, err = svc.FinalizeCloudBillingSnapshot(ctx, "gpt-5.1", "default", "low")
	require.NoError(t, err)
	// Explicitly exercise recovery of a pre-stage v3 root.
	legacy := *cloudBillingContext(ctx)
	legacy.Version = 3
	encoded, err := json.Marshal(&legacy)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "must-not-persist")
	for _, forbidden := range []string{"access_token", "refresh_token", "PasswordHash", "TotpSecret", "credentials", "\"Key\""} {
		require.NotContains(t, string(encoded), forbidden)
	}
	// All later mutable configuration and role changes must not re-price the old turn.
	*input.APIKey.Group.ModelPricing[0].InputPrice = 99
	input.APIKey.Group.RateMultiplier = 99
	input.User.Role = RoleSuperAdmin
	input.APIKey.GroupID = nil
	svc.cfg.Default.RateMultiplier = 99
	settlement := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "turn-root"},
		BillingSnapshot: encoded, Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 1_000_000, TotalTokens: 1_000_000}, ObservedAt: input.PricingAt.Add(time.Minute)}
	worker := &openAICloudBillingWorker{service: svc}
	require.NoError(t, worker.settle(context.Background(), settlement))
	require.Equal(t, "cloud:turn-root", repo.lastCmd.RequestID)
	require.InDelta(t, 4, repo.lastCmd.BalanceCost, 1e-9)
	require.Equal(t, int64(11), repo.lastCmd.UserID)
	require.Equal(t, int64(33), repo.lastCmd.APIKeyID)
	require.Equal(t, "cloud", repo.lastCmd.UsageLog.AuthorizationMode)
	require.NotEqual(t, RequestTypeCyberBlocked, repo.lastCmd.UsageLog.RequestType)
	require.Equal(t, int64(22), *repo.lastCmd.UsageLog.GroupID)
	require.Equal(t, "original-account", *repo.lastCmd.UsageLog.AccountNameSnapshot)
	require.False(t, repo.lastCmd.BalanceExempt, "a later admin-role change cannot exempt an accepted old request")
	require.NoError(t, worker.settle(context.Background(), settlement))
	require.Equal(t, "cloud:turn-root", repo.lastCmd.RequestID, "retries use the same existing-ledger idempotency key")
}

func TestCloudBillingConsumerRetainsUnknownAndFailedDebits(t *testing.T) {
	svc, input, repo := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(context.Background(), input)
	require.NoError(t, err)
	store := &cloudBillingStoreStub{}
	worker := &openAICloudBillingWorker{service: svc, store: store, id: "worker", holds: make(map[string]openAICloudHeldReservation)}
	settlement := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "root"}, BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "failed", ErrorCode: "cyber_policy", ObservedAt: time.Now()}
	raw, err := json.Marshal(settlement)
	require.NoError(t, err)
	var persisted OpenAICloudSettlement
	require.NoError(t, json.Unmarshal(raw, &persisted))
	settlement = &persisted
	store.pending = []*OpenAICloudSettlement{settlement}
	worker.consume(context.Background())
	require.Zero(t, store.acks)
	require.Zero(t, repo.calls, "missing usage is never synthesized as zero")
	require.Equal(t, 1, store.retained)
	settlement.RootRequestID = "unknown-root"
	settlement.Usage = &OpenAICloudUsage{InputTokens: 10, TotalTokens: 10}
	settlement.Unknown = true
	released := 0
	worker.holds[settlement.RootRequestID] = openAICloudHeldReservation{done: func() { released++ }, expires: time.Now().Add(time.Minute)}
	worker.consume(context.Background())
	require.Zero(t, store.acks)
	require.Zero(t, repo.calls)
	require.Equal(t, 2, store.retained)
	require.Equal(t, 1, released, "unknown terminal facts release execution holds without a debit or ACK")
	require.True(t, settlement.Unknown)
	require.Equal(t, 10, settlement.Usage.TotalTokens, "observed usage is preserved without pretending it is final")
	settlement.Unknown = false
	settlement.RootRequestID = "credible-root"
	repo.err = errors.New("database unavailable")
	worker.consume(context.Background())
	require.Zero(t, store.acks)
	require.Equal(t, 1, repo.calls)
	repo.err = nil
	worker.consume(context.Background())
	require.Equal(t, 1, store.acks)
	require.Equal(t, RequestTypeCyberBlocked, repo.lastCmd.UsageLog.RequestType)
	require.Equal(t, "cloud:credible-root", repo.lastCmd.RequestID)
	require.InDelta(t, 0.00004, repo.lastCmd.BalanceCost, 1e-12, "policy classification does not alter confirmed consumption pricing")
	require.True(t, store.acked["credible-root"])
	worker.consume(context.Background())
	require.Equal(t, 2, repo.calls, "already acked roots are not submitted again")
}

func TestCloudProbeSettlementUsesClaimAckAndCannotImpersonateUserBilling(t *testing.T) {
	svc := &OpenAIGatewayService{}
	store := &cloudBillingStoreStub{}
	worker := &openAICloudBillingWorker{service: svc, store: store, id: "worker", holds: make(map[string]openAICloudHeldReservation)}
	settlement := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{Probe: true, UserID: 10, AccountID: 2}, RootRequestID: "probe-root"}, Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}}
	encoded, err := json.Marshal(settlement)
	require.NoError(t, err)
	var persisted OpenAICloudSettlement
	require.NoError(t, json.Unmarshal(encoded, &persisted))
	require.Equal(t, json.RawMessage(`null`), persisted.BillingSnapshot)
	settlement = &persisted
	store.pending = []*OpenAICloudSettlement{settlement}
	store.ackError = errors.New("ack unavailable")
	worker.consume(context.Background())
	require.False(t, store.acked["probe-root"])
	store.ackError = nil
	worker.consume(context.Background())
	require.True(t, store.acked["probe-root"])
	require.Equal(t, 2, store.claims)
	for _, tc := range []struct {
		name     string
		snapshot json.RawMessage
		probe    bool
		wantAck  bool
	}{
		{name: "absent", probe: true, wantAck: true},
		{name: "null", snapshot: json.RawMessage(`null`), probe: true, wantAck: true},
		{name: "whitespace null", snapshot: json.RawMessage(" \n null\t "), probe: true, wantAck: true},
		{name: "empty object", snapshot: json.RawMessage(`{}`), probe: true},
		{name: "customer snapshot", snapshot: json.RawMessage(`{"api_key_id":7}`), probe: true},
		{name: "customer null", snapshot: json.RawMessage(`null`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := *settlement
			candidate.RootRequestID = tc.name
			candidate.BillingSnapshot = tc.snapshot
			candidate.Probe = tc.probe
			if !tc.probe {
				candidate.APIKeyID = 7
			}
			candidateStore := &cloudBillingStoreStub{pending: []*OpenAICloudSettlement{&candidate}}
			candidateWorker := &openAICloudBillingWorker{service: svc, store: candidateStore, id: "worker"}
			candidateWorker.consume(context.Background())
			require.Equal(t, 1, candidateStore.claims)
			require.Equal(t, tc.wantAck, candidateStore.acked[tc.name])
			require.Equal(t, tc.probe && !tc.wantAck, candidateStore.failed[tc.name])
		})
	}
	settlement.RootRequestID = "failed-probe-root"
	settlement.Unknown = true
	settlement.Usage = nil
	worker.consume(context.Background())
	require.True(t, store.failed["failed-probe-root"], "an unmetered admin probe is a failed receipt, not unpaid customer usage")
	require.False(t, store.acked["failed-probe-root"])
	svc.cloudBillingWorker = worker
	require.ErrorIs(t, svc.WaitCloudProbeSettlement(context.Background(), "failed-probe-root"), ErrOpenAICloudProbeSettlementFailed)
	settlement.Unknown = false
	settlement.Usage = &OpenAICloudUsage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}
	settlement.APIKeyID = 7
	require.Error(t, worker.settle(context.Background(), settlement))
	settlement.APIKeyID = 0
	settlement.BillingSnapshot = json.RawMessage(`{"api_key_id":7}`)
	require.Error(t, worker.settle(context.Background(), settlement))
}

func TestCloudManagedBillingSkipsSynchronousZeroUsage(t *testing.T) {
	svc := &OpenAIGatewayService{}
	require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{CloudManagedBilling: true}}))
	require.False(t, cloudBillingUsageValid(nil))
	require.False(t, cloudBillingUsageValid(&OpenAICloudUsage{InputTokens: 2, CachedInputTokens: 3, TotalTokens: 2}))
	require.False(t, cloudBillingUsageValid(&OpenAICloudUsage{InputTokens: 2, OutputTokens: 3, TotalTokens: 4}))
	require.True(t, cloudBillingUsageValid(&OpenAICloudUsage{}), "a present measured zero is different from missing usage")
}

func TestCloudFinalizeRejectsUnfrozenModelAndInvalidOwner(t *testing.T) {
	svc, input, _ := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(context.Background(), input)
	require.NoError(t, err)
	_, err = svc.FinalizeCloudBillingSnapshot(ctx, "another-model", "", "")
	require.Error(t, err)
	input.Account.Credentials["sensitive_unrelated"] = strings.Repeat("secret", 100)
	input.APIKey.UserID++
	_, err = svc.CaptureCloudBillingSnapshot(context.Background(), input)
	require.Error(t, err)
}

func TestCloudBillingDeletedKeyStillSettlesOriginalOwner(t *testing.T) {
	for _, test := range []struct {
		name        string
		lookupError error
		wantDebit   bool
	}{
		{name: "confirmed_deleted_key", lookupError: fmt.Errorf("get api key: %w", ErrAPIKeyNotFound), wantDebit: true},
		{name: "temporary_database_failure", lookupError: errors.New("database unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, input, repo := cloudBillingFixture(t)
			ctx, err := svc.CaptureCloudBillingSnapshot(context.Background(), input)
			require.NoError(t, err)
			snapshot := CloudBillingSnapshotFromContext(ctx)
			svc.SetCloudBillingQuotaUpdater(&cloudBillingKeyStub{err: test.lookupError})
			settlement := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "deleted-key-root"},
				BillingSnapshot: snapshot, Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 1_000_000, TotalTokens: 1_000_000}, ObservedAt: input.PricingAt.Add(time.Minute)}
			worker := &openAICloudBillingWorker{service: svc}
			err = worker.settle(context.Background(), settlement)
			if !test.wantDebit {
				require.ErrorIs(t, err, test.lookupError)
				require.Zero(t, repo.calls)
				return
			}
			require.NoError(t, err)
			require.Equal(t, int64(11), repo.lastCmd.UserID)
			require.Equal(t, int64(33), repo.lastCmd.APIKeyID)
			require.Equal(t, "cloud:deleted-key-root", repo.lastCmd.RequestID)
			require.InDelta(t, 4, repo.lastCmd.BalanceCost, 1e-9)
			require.InDelta(t, 4, repo.lastCmd.APIKeyQuotaCost, 1e-9, "preserve the original idempotency fingerprint; the ledger handles deleted-key counters")
			require.Equal(t, int64(22), *repo.lastCmd.UsageLog.GroupID)
		})
	}
}

func TestCloudHostedBillingFreezesAdditivePricesAndMeasuredImages(t *testing.T) {
	svc, input, repo := cloudBillingFixture(t)
	group := input.APIKey.Group
	search, small, medium, large := 0.03, 0.2, 0.4, 0.8
	group.WebSearchPricePerCall = &search
	group.ImagePrice1K, group.ImagePrice2K, group.ImagePrice4K = &small, &medium, &large
	group.ImageRateIndependent, group.ImageRateMultiplier = true, 3
	ctx, err := svc.CaptureCloudBillingSnapshot(context.Background(), input)
	require.NoError(t, err)
	encoded := CloudBillingSnapshotFromContext(ctx)
	// Repricing, a changed image multiplier and a changed default rate after
	// dispatch cannot alter the accepted root, including after worker restart.
	search, small, medium, large = 99, 99, 99, 99
	group.ImageRateMultiplier, group.RateMultiplier, svc.cfg.Default.RateMultiplier = 99, 99, 99
	*group.ModelPricing[0].InputPrice = 99
	settlement := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "hosted-root"},
		BillingSnapshot: encoded, Status: "completed", ObservedAt: input.PricingAt.Add(time.Minute),
		Usage: &OpenAICloudUsage{InputTokens: 1_000_000, TotalTokens: 1_000_000, WebSearchCalls: 2, ImageCount: 3,
			ImageSizes: map[string]int{"1K": 1, "2K": 1, "4K": 1}, ImageOutputSizes: []string{"512x512", "1536x1024", "3072x2048"}}}
	store := &cloudBillingStoreStub{pending: []*OpenAICloudSettlement{settlement}, ackError: errors.New("ack failed after ledger submission")}
	worker := &openAICloudBillingWorker{service: svc, store: store, id: "worker", holds: make(map[string]openAICloudHeldReservation)}
	worker.consume(context.Background())
	require.Equal(t, 1, repo.calls)
	require.False(t, store.acked["hosted-root"])
	require.Equal(t, "cloud:hosted-root", repo.lastCmd.RequestID)
	log := repo.lastCmd.UsageLog
	require.InDelta(t, 2+0.06+1.4, log.TotalCost, 1e-9, "tool fees add to the full main-model token charge")
	require.InDelta(t, 4+0.12+4.2, repo.lastCmd.BalanceCost, 1e-9, "search uses base rate; images use the frozen independent rate")
	require.InDelta(t, 1.4, log.ImageOutputCost, 1e-9)
	require.Equal(t, 3, log.ImageCount)
	require.Equal(t, map[string]int{"1K": 1, "2K": 1, "4K": 1}, log.ImageSizeBreakdown)
	require.Equal(t, "512x512", *log.ImageOutputSize)
	require.Equal(t, ImageBillingSize4K, *log.ImageSize)
	require.Equal(t, ImageSizeSourceOutput, *log.ImageSizeSource)
	require.Equal(t, string(BillingModeToken), *log.BillingMode)
	require.Equal(t, "gpt-5.1", *log.UpstreamModel)
	require.Zero(t, log.ImageInputTokens, "Cloud does not report a trustworthy image token split")
	require.Zero(t, log.ImageOutputTokens)
	fingerprint := repo.lastCmd.RequestFingerprint
	require.NotEmpty(t, fingerprint)
	store.ackError = nil
	worker.consume(context.Background())
	require.Equal(t, 2, repo.calls)
	require.Equal(t, "cloud:hosted-root", repo.lastCmd.RequestID)
	require.Equal(t, fingerprint, repo.lastCmd.RequestFingerprint, "lost ACK retries the same atomic-ledger command")
	require.True(t, store.acked["hosted-root"])
	worker.consume(context.Background())
	require.Equal(t, 2, repo.calls, "an acknowledged tool root is not charged again")
}

func TestCloudHostedBillingUsesExistingGenericDefaultsAndExplicitFreePrices(t *testing.T) {
	for _, free := range []bool{false, true} {
		t.Run(fmt.Sprintf("free=%t", free), func(t *testing.T) {
			svc, input, _ := cloudBillingFixture(t)
			if free {
				zero := 0.0
				input.APIKey.Group.WebSearchPricePerCall = &zero
				input.APIKey.Group.ImagePrice1K, input.APIKey.Group.ImagePrice2K, input.APIKey.Group.ImagePrice4K = &zero, &zero, &zero
			}
			ctx, err := svc.CaptureCloudBillingSnapshot(context.Background(), input)
			require.NoError(t, err)
			snapshot := cloudBillingContext(ctx)
			result := &OpenAIForwardResult{WebSearchCalls: 2, ImageCount: 2,
				ImageSizeBreakdown: map[string]int{"1K": 1, "4K": 1}, ImageOutputSizes: []string{"1024x1024", "3072x2048"}}
			cost, err := svc.cloudFrozenHostedCost(snapshot, result)
			require.NoError(t, err)
			if free {
				require.Zero(t, cost.TotalCost)
				require.Zero(t, cost.ActualCost)
			} else {
				require.InDelta(t, defaultWebSearchPricePerCall, snapshot.HostedTools.WebSearchPerCall, 1e-12)
				require.InDelta(t, defaultImageGenerationPrice*1.5, snapshot.HostedTools.ImagePrices[ImageBillingSize2K], 1e-12)
				require.InDelta(t, 2*defaultWebSearchPricePerCall+3*defaultImageGenerationPrice, cost.TotalCost, 1e-12)
				require.InDelta(t, 2*cost.TotalCost, cost.ActualCost, 1e-12)
			}
		})
	}
}

func TestCloudHostedAccountStatisticsAddFrozenPolicyOnce(t *testing.T) {
	svc, input, _ := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(context.Background(), input)
	require.NoError(t, err)
	snapshot := cloudBillingContext(ctx)
	result := &OpenAIForwardResult{Model: "gpt-5.1", UpstreamModel: "gpt-5.1", WebSearchCalls: 2, ImageCount: 1,
		ImageSizeBreakdown: map[string]int{"1K": 1}, ImageOutputSizes: []string{"1024x1024"}}
	tokens := UsageTokens{InputTokens: 1_000_000}
	hosted := 2*defaultWebSearchPricePerCall + defaultImageGenerationPrice
	customPrice := 10e-6
	for _, test := range []struct {
		name   string
		stats  openAICloudStatsSnapshot
		expect float64
	}{
		{"custom_agent_price", openAICloudStatsSnapshot{CustomPricing: &ChannelModelPricing{BillingMode: BillingModeToken, InputPrice: &customPrice}}, 10 + hosted},
		{"customer_price", openAICloudStatsSnapshot{ApplyCustomerPrice: true}, 2 + hosted},
		{"default_agent_price", openAICloudStatsSnapshot{DefaultPrice: snapshot.Prices["gpt-5.1"]}, 2 + hosted},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot.Stats = &test.stats
			cost, err := svc.cloudFrozenStatsCost(ctx, snapshot, result, tokens, 2+hosted)
			require.NoError(t, err)
			require.NotNil(t, cost)
			require.InDelta(t, test.expect, *cost, 1e-9)
		})
	}
}

func TestCloudHostedUsageRequiresMeasuredDimensionsAndFrozenPrices(t *testing.T) {
	for _, usage := range []*OpenAICloudUsage{
		{WebSearchCalls: -1}, {WebSearchCalls: 1025}, {ImageCount: 1},
		{ImageCount: 1, ImageSizes: map[string]int{"1K": 1}, ImageOutputSizes: []string{"1K"}},
		{ImageCount: 1, ImageSizes: map[string]int{"1K": 1}, ImageOutputSizes: []string{"3072x2048"}},
		{ImageCount: 2, ImageSizes: map[string]int{"1K": 1}, ImageOutputSizes: []string{"512x512", "512x512"}},
		{ImageCount: 1, ImageSizes: map[string]int{"unknown": 1}, ImageOutputSizes: []string{"512x512"}},
		{ImageCount: 1, ImageSizes: map[string]int{"1K": -1}, ImageOutputSizes: []string{"512x512"}},
		{ImageCount: 1, ImageSizes: map[string]int{"1K": 1, "2K": 0}, ImageOutputSizes: []string{"512x512"}},
	} {
		require.False(t, cloudBillingUsageValid(usage))
	}
	svc, input, _ := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(context.Background(), input)
	require.NoError(t, err)
	snapshot := cloudBillingContext(ctx)
	result := &OpenAIForwardResult{WebSearchCalls: 1}
	snapshot.HostedTools = nil
	_, err = svc.cloudFrozenHostedCost(snapshot, result)
	require.Error(t, err, "a new hosted fee cannot use a missing/defaulted old snapshot")
	input.APIKey.Group.ImagePrice1K = float64Ptr(math.NaN())
	_, err = svc.CaptureCloudBillingSnapshot(context.Background(), input)
	require.Error(t, err, "invalid prices are rejected before dispatch")
}

func TestCloudRejectedBeforeInferenceSkipsPerRequestCharge(t *testing.T) {
	svc, input, ledger := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	snapshot := cloudBillingContext(ctx)
	price := snapshot.Prices["gpt-5.1"]
	price.Resolved.Mode = BillingModePerRequest
	snapshot.Prices["gpt-5.1"] = price
	settlement := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "rejected"}, BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "failed", Usage: &OpenAICloudUsage{}, ObservedAt: time.Now()}
	worker := &openAICloudBillingWorker{service: svc}
	require.NoError(t, worker.settle(ctx, settlement))
	require.Zero(t, ledger.calls)
	settlement.Unknown = true
	require.Error(t, worker.settle(ctx, settlement), "an absent usage fact is not a proven rejection")
}

func TestCloudBillingArchiveOnlyReleasesAfterDurablePreservation(t *testing.T) {
	for _, scenario := range []string{"deleted_user", "database_outage", "archive_outage", "malformed_record"} {
		t.Run(scenario, func(t *testing.T) {
			svc, input, ledger := cloudBillingFixture(t)
			ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
			require.NoError(t, err)
			settlement := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "root"}, BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 10, TotalTokens: 10}, ObservedAt: time.Now()}
			archive := svc.usageBillingRepo.(*cloudBillingArchiveRepoStub)
			users := svc.userRepo.(*cloudBillingUserStub)
			switch scenario {
			case "deleted_user":
				users.err = ErrUserNotFound
			case "database_outage":
				users.err = errors.New("database unavailable")
			case "archive_outage":
				settlement.Unknown = true
				archive.archiveErr = errors.New("archive unavailable")
			case "malformed_record":
				settlement.ArchiveReason, settlement.ArchivePayload = "invalid_record", []byte("{broken")
			}
			store := &cloudBillingStoreStub{pending: []*OpenAICloudSettlement{settlement}}
			released := 0
			worker := &openAICloudBillingWorker{service: svc, store: store, id: "worker", holds: map[string]openAICloudHeldReservation{"root": {done: func() { released++ }, expires: time.Now().Add(time.Hour)}}}
			worker.consume(ctx)
			require.Zero(t, store.acks)
			require.Zero(t, ledger.calls)
			if scenario == "database_outage" || scenario == "archive_outage" {
				require.Zero(t, store.retained)
				require.Zero(t, released)
				return
			}
			require.Equal(t, 1, store.retained)
			require.Equal(t, 1, released)
			if scenario == "malformed_record" {
				require.Equal(t, settlement.ArchivePayload, archive.archives["root"])
			} else {
				require.Equal(t, "owner_missing", archive.reasons["root"])
			}
			// Another instance can release its hold without treating archival as ACK.
			worker.holds["root"] = openAICloudHeldReservation{done: func() { released++ }, expires: time.Now().Add(time.Hour)}
			worker.releaseAcknowledgedHolds(ctx)
			require.Equal(t, 2, released)
			require.False(t, store.acked["root"])
		})
	}
}

func TestCloudBillingRetainsObservedTimingAndTransport(t *testing.T) {
	svc, input, ledger := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	first := 42
	metrics := &UsageLatencyMetrics{Version: 1, DurationMs: 1200}
	settlement := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "observed"}, BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 10, TotalTokens: 10}, ObservedAt: input.PricingAt.Add(1200 * time.Millisecond), Telemetry: &OpenAICloudTelemetry{FirstTokenMs: &first, LatencyMetrics: metrics, OpenAIWSMode: true, WaitingToolsMs: 500}}
	raw, err := json.Marshal(settlement)
	require.NoError(t, err)
	var persisted OpenAICloudSettlement
	require.NoError(t, json.Unmarshal(raw, &persisted))
	require.NoError(t, (&openAICloudBillingWorker{service: svc}).settle(ctx, &persisted))
	require.Equal(t, &first, ledger.lastCmd.UsageLog.FirstTokenMs)
	require.True(t, ledger.lastCmd.UsageLog.OpenAIWSMode)
	require.Equal(t, metrics, ledger.lastCmd.UsageLog.LatencyMetrics)
	require.Nil(t, ledger.lastCmd.UsageLog.LatencyMetrics.FirstTextMs, "unobserved downstream flush time is not invented")
	// The legacy duration remains a full turn wall duration, including tool wait.
	require.Equal(t, 1200, *ledger.lastCmd.UsageLog.DurationMs)
}

// The gateway measures client-side latency only after it writes the response,
// later than the owner persists the settlement. It is recorded per billed response
// and joined at settlement; nothing is invented when it was not measured.
func TestCloudLatencyMeasuredByGatewayReachesSettlement(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			go func() {
				cloudWireStart(t, ctx, wire)
				cloudWireComplete(wire)
			}()
			requestCtx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			c.Set("api_key", in.APIKey)
			SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
			result, err := svc.forwardOpenAICloud(requestCtx, c, in.Account, []byte(fmt.Sprintf(`{"model":"client-model","input":"hi","stream":%v}`, stream)))
			require.NoError(t, err)
			require.NotNil(t, result.LatencyMetrics)
			recorded, err := store.LoadCloudLatency(ctx, result.RequestID+"/"+result.ResponseID)
			require.NoError(t, err)
			require.Equal(t, result.LatencyMetrics, recorded)
			settlement := store.snapshotSettlement(result.RequestID)
			require.NotNil(t, settlement)
			require.Nil(t, settlement.Telemetry.LatencyMetrics, "the owner persisted before the gateway measured")
			svc2, input2, ledger := cloudBillingFixture(t)
			_ = input2
			worker := &openAICloudBillingWorker{service: svc2, store: store}
			require.NoError(t, worker.settle(ctx, settlement))
			require.Equal(t, *ledger.lastCmd.UsageLog.DurationMs, ledger.lastCmd.UsageLog.LatencyMetrics.DurationMs)
			require.Equal(t, recorded.FirstTextMs, ledger.lastCmd.UsageLog.LatencyMetrics.FirstTextMs)
			require.Equal(t, recorded.FirstEventMs, ledger.lastCmd.UsageLog.LatencyMetrics.FirstEventMs)
		})
	}
}

// Without a gateway measurement the settlement keeps no latency metrics; a
// young settlement is deferred briefly so the gateway can record it first.
func TestCloudLatencyGraceDefersOnlyYoungCustomerSettlements(t *testing.T) {
	store := newCloudLiveRoutingStore()
	worker := &openAICloudBillingWorker{store: store}
	young := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{RootRequestID: "r"}, ObservedAt: time.Now(), Status: "completed"}
	require.True(t, worker.awaitingLatency(t.Context(), young))
	require.NoError(t, store.RecordCloudLatency(t.Context(), "r", &UsageLatencyMetrics{Version: 1}))
	require.False(t, worker.awaitingLatency(t.Context(), young))
	old := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{RootRequestID: "o"}, ObservedAt: time.Now().Add(-time.Minute), Status: "completed"}
	require.False(t, worker.awaitingLatency(t.Context(), old))
	probe := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{Probe: true}, RootRequestID: "p"}, ObservedAt: time.Now()}
	require.False(t, worker.awaitingLatency(t.Context(), probe))
	require.False(t, (&openAICloudBillingWorker{store: &cloudActorTestStore{}}).awaitingLatency(t.Context(), young), "stores without latency never delay billing")
}

// Background billing failures are logged by stage with a safe class, rate
// limited per stage, and never with raw error text.
func TestCloudBillingFailureLogClassAndRateLimit(t *testing.T) {
	require.Equal(t, "timeout", cloudBillingErrorClass(context.DeadlineExceeded))
	require.Equal(t, "canceled", cloudBillingErrorClass(context.Canceled))
	require.Equal(t, "permanent:unknown_usage", cloudBillingErrorClass(cloudPermanentSettlementError("unknown_usage")))
	require.Equal(t, "conflict", cloudBillingErrorClass(ErrUsageBillingRequestConflict))
	require.Equal(t, "error", cloudBillingErrorClass(errors.New("pq: value secret-token violates constraint")))
	worker := &openAICloudBillingWorker{}
	now := time.Now()
	require.True(t, worker.shouldLog("list", now))
	require.False(t, worker.shouldLog("list", now.Add(time.Second)))
	require.True(t, worker.shouldLog("claim", now.Add(time.Second)))
	require.True(t, worker.shouldLog("list", now.Add(cloudBillingLogInterval+time.Second)))
}

// Persisted per-sampling facts must drive nonlinear prices, not the root sum.
func TestCloudBillingSamplingContextBoundary(t *testing.T) {
	svc, input, ledger := cloudBillingFixture(t)
	input.APIKey.Group.ModelPricing = nil
	input.APIKey.Group.RateMultiplier = 1
	input.APIKey.Group.LongContextPricingEnabled = true
	input.Account.Extra = map[string]any{"openai_long_context_billing_enabled": true}
	input.Result.Model, input.Result.UpstreamModel = "gpt-6-astra", "gpt-6-astra"
	input.ChannelUsageFields = ChannelUsageFields{OriginalModel: "gpt-6-astra", ChannelMappedModel: "gpt-6-astra", BillingModelSource: BillingModelSourceRequested}
	ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	for _, lower := range []bool{false, true} {
		settlement := OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-6-astra"}, RootRequestID: fmt.Sprint("sampling-", lower)}, BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 400000, OutputTokens: 2000, TotalTokens: 402000}, ObservedAt: input.PricingAt.Add(time.Minute)}
		if lower {
			settlement.Status, settlement.LowerBound = "failed", true
		}
		data, err := json.Marshal(settlement)
		require.NoError(t, err)
		var wire map[string]any
		require.NoError(t, json.Unmarshal(data, &wire))
		wire["sampling_usage"] = []OpenAICloudUsage{{InputTokens: 200000, OutputTokens: 1000, TotalTokens: 201000}, {InputTokens: 200000, OutputTokens: 1000, TotalTokens: 201000}}
		data, err = json.Marshal(wire)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(data, &settlement))
		require.NoError(t, (&openAICloudBillingWorker{service: svc}).settle(ctx, &settlement))
		require.InDelta(t, 4.10, ledger.lastCmd.BalanceCost, 1e-9)
		require.False(t, ledger.lastCmd.UsageLog.LongContextBillingApplied)
	}
}

func TestCloudBillingSamplingFailureArchivesWithoutDebit(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing_%t", missing), func(t *testing.T) {
			svc, input, ledger := cloudBillingFixture(t)
			input.APIKey.Group.ModelPricing = nil
			input.APIKey.Group.LongContextPricingEnabled = true
			input.Account.Extra = map[string]any{"openai_long_context_billing_enabled": true}
			input.Result.Model, input.Result.UpstreamModel = "gpt-6-astra", "gpt-6-astra"
			input.ChannelUsageFields = ChannelUsageFields{OriginalModel: "gpt-6-astra", ChannelMappedModel: "gpt-6-astra", BillingModelSource: BillingModelSourceRequested}
			ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
			require.NoError(t, err)
			settlement := &OpenAICloudSettlement{
				OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-6-astra"}, RootRequestID: "sampling-archive"},
				BillingSnapshot:    CloudBillingSnapshotFromContext(ctx),
				Status:             "completed",
				Usage:              &OpenAICloudUsage{InputTokens: 400000, OutputTokens: 2000, TotalTokens: 402000},
				ObservedAt:         input.PricingAt.Add(time.Minute),
			}
			wantFailure, wantArchive := "sampling_usage_missing", "unknown_usage"
			if !missing {
				settlement.SamplingUsage = []OpenAICloudUsage{{InputTokens: 200000, OutputTokens: 1000, TotalTokens: 201000}}
				wantFailure, wantArchive = "invalid_sampling_usage", "invalid_settlement"
			}
			payload, err := json.Marshal(settlement)
			require.NoError(t, err)
			settlement.ArchivePayload = payload
			store := &cloudBillingStoreStub{pending: []*OpenAICloudSettlement{settlement}}
			worker := &openAICloudBillingWorker{service: svc, store: store, id: "sampling-worker"}
			require.ErrorIs(t, worker.settle(ctx, settlement), cloudPermanentSettlementError(wantFailure))
			archive := svc.usageBillingRepo.(*cloudBillingArchiveRepoStub)
			archive.archiveErr = errors.New("archive unavailable")
			worker.consume(ctx)
			require.Zero(t, store.retained, "no removal before durable preservation")
			archive.archiveErr = nil
			worker.consume(ctx)
			require.Equal(t, wantArchive, archive.reasons[settlement.RootRequestID], "reason must satisfy the existing SQL archive constraint")
			require.Equal(t, payload, archive.archives[settlement.RootRequestID])
			require.Equal(t, 1, store.retained)
			require.Zero(t, store.acks)
			require.Zero(t, ledger.calls, "ambiguous sampling must not be charged")
			worker.consume(ctx)
			require.Equal(t, 1, store.retained, "an archived root leaves the retry queue")
		})
	}
}

func TestCloudBillingRootDurationMatchesAggregatedUsage(t *testing.T) {
	svc, input, ledger := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	first := 8820
	metrics := &UsageLatencyMetrics{Version: 1, DurationMs: 14774, FirstTextMs: &first}
	settlement := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "root-duration"}, BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 10, TotalTokens: 10}, ObservedAt: input.PricingAt.Add(851551 * time.Millisecond), Telemetry: &OpenAICloudTelemetry{FirstTokenMs: &first, LatencyMetrics: metrics}}
	require.NoError(t, (&openAICloudBillingWorker{service: svc}).settle(ctx, settlement))
	require.Equal(t, 851551, ledger.lastCmd.UsageLog.LatencyMetrics.DurationMs)
	require.Equal(t, 851551, *ledger.lastCmd.UsageLog.DurationMs)
	require.Equal(t, &first, ledger.lastCmd.UsageLog.LatencyMetrics.FirstTextMs)
	require.Equal(t, 14774, metrics.DurationMs, "do not mutate the per-request measurement")
}

func TestCloudSamplingEvidenceValidationAndLegacyPricing(t *testing.T) {
	total := &OpenAICloudUsage{InputTokens: 400000, OutputTokens: 2000, TotalTokens: 402000}
	samples := []OpenAICloudUsage{{InputTokens: 200000, OutputTokens: 1000, TotalTokens: 201000}, {InputTokens: 200000, OutputTokens: 1000, TotalTokens: 201000}}
	require.True(t, cloudSamplingUsageValid(total, samples))
	require.False(t, cloudSamplingUsageValid(total, samples[:1]))
	require.False(t, cloudSamplingUsageValid(total, append(samples, samples[0])))
	require.True(t, cloudSamplingUsageValid(total, nil), "old records remain distinguishable")
	svc, _, _ := cloudBillingFixture(t)
	resolver := NewModelPricingResolver(nil, svc.billingService)
	enabled := true
	input := CostInput{Ctx: t.Context(), Model: "gpt-6-astra", Tokens: cloudSamplingTokens(*total), RequestCount: 1, RateMultiplier: 1, Resolver: resolver, Resolved: resolver.Resolve(t.Context(), PricingInput{Model: "gpt-6-astra"}), LongContextBillingEnabled: &enabled}
	_, err := svc.cloudSamplingCost(input, nil)
	require.ErrorIs(t, err, cloudPermanentSettlementError("sampling_usage_missing"), "do not guess long-context surcharges for legacy roots")
	input.Tokens = UsageTokens{InputTokens: 1000}
	_, err = svc.cloudSamplingCost(input, nil)
	require.NoError(t, err, "unambiguous flat legacy pricing stays supported")
	samples[0] = OpenAICloudUsage{InputTokens: 300000, OutputTokens: 1000, TotalTokens: 301000}
	cost, err := svc.cloudSamplingCost(input, samples)
	require.NoError(t, err)
	require.True(t, cost.LongContextBillingApplied)
	require.InDelta(t, 8.125, cost.ActualCost, 1e-9, "only the actual 300k sampling operation receives the long-context price")
	// Customer and account statistics use the same sampling boundaries.
	group := int64(1)
	price, err := cloudPriceSnapshot(input.Resolved)
	require.NoError(t, err)
	snapshot := &OpenAICloudBillingSnapshot{GroupID: &group, LongContextEnabled: true, UpstreamModel: "gpt-6-astra", Stats: &openAICloudStatsSnapshot{DefaultPrice: price}}
	value, err := svc.cloudFrozenStatsCost(t.Context(), snapshot, &OpenAIForwardResult{UpstreamModel: "gpt-6-astra", cloudSamplingUsage: samples}, input.Tokens, cost.TotalCost)
	require.NoError(t, err)
	require.InDelta(t, cost.TotalCost, *value, 1e-9)
}

func TestCloudSamplingEvidencePreservesMeasuredBuckets(t *testing.T) {
	total := &OpenAICloudUsage{InputTokens: 20, OutputTokens: 4, TotalTokens: 24, CachedInputTokens: 10, CacheWriteInputTokens: 2, ReasoningOutputTokens: 2, WebSearchCalls: 1}
	first := OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12, CachedInputTokens: 4, CacheWriteInputTokens: 1, ReasoningOutputTokens: 1}
	second := OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12, CachedInputTokens: 6, CacheWriteInputTokens: 1, ReasoningOutputTokens: 1}
	for _, tc := range []struct {
		name   string
		change func(*OpenAICloudUsage)
		valid  bool
	}{
		{name: "exact token buckets and root-only hosted usage", valid: true},
		{name: "missing cached input", change: func(u *OpenAICloudUsage) { u.CachedInputTokens-- }},
		{name: "missing cache write", change: func(u *OpenAICloudUsage) { u.CacheWriteInputTokens-- }},
		{name: "missing reasoning", change: func(u *OpenAICloudUsage) { u.ReasoningOutputTokens-- }},
		{name: "negative reasoning", change: func(u *OpenAICloudUsage) { u.ReasoningOutputTokens = -1 }},
		{name: "reasoning exceeds output", change: func(u *OpenAICloudUsage) { u.ReasoningOutputTokens = 3 }},
		{name: "hosted usage duplicated in sample", change: func(u *OpenAICloudUsage) { u.WebSearchCalls = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sample := first
			if tc.change != nil {
				tc.change(&sample)
			}
			require.Equal(t, tc.valid, cloudSamplingUsageValid(total, []OpenAICloudUsage{sample, second}))
		})
	}
	for _, samples := range [][]OpenAICloudUsage{nil, {}} {
		raw, err := json.Marshal(&OpenAICloudSettlement{SamplingUsage: samples})
		require.NoError(t, err)
		var decoded OpenAICloudSettlement
		require.NoError(t, json.Unmarshal(raw, &decoded))
		require.Equal(t, samples, decoded.SamplingUsage, "unknown legacy and measured zero must remain distinct on the wire")
	}
	require.False(t, cloudSamplingUsageValid(total, []OpenAICloudUsage{}), "empty samples cannot erase measured usage")
	require.False(t, cloudSamplingUsageValid(nil, []OpenAICloudUsage{}), "no root measurement is not measured zero")
	require.True(t, cloudSamplingUsageValid(&OpenAICloudUsage{}, []OpenAICloudUsage{}))
}

type cloudBillingReservationReleases map[string]int

func (r cloudBillingReservationReleases) ReleaseInflightBalance(_ context.Context, _ int64, root string) error {
	r[root]++
	return nil
}

func TestCloudBillingReservationsExceedFormerLimitAndKeepReferencesUntilAckOrTTL(t *testing.T) {
	store := &cloudBillingStoreStub{acked: map[string]bool{}, archived: map[string]bool{}}
	worker := &openAICloudBillingWorker{store: store, holds: make(map[string]openAICloudHeldReservation)}
	svc := &OpenAIGatewayService{cloudBillingWorker: worker}
	released := cloudBillingReservationReleases{}
	const count = 260 // Beyond both the retired hold cap and one maintenance batch.
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("root-%d", i)
		reservation := newInflightReservation(released, 1, id, 1, time.Minute)
		t.Cleanup(reservation.Release)
		ctx := WithInflightReservation(t.Context(), reservation)
		require.NoError(t, svc.HoldCloudBillingReservation(ctx, id))
		require.NoError(t, svc.HoldCloudBillingReservation(ctx, id), "same-root holds remain idempotent")
		reservation.HandlerDone()
	}
	require.Len(t, worker.holds, count)
	require.Empty(t, released, "returning the handler reference must retain the billing reference")
	worker.releaseAcknowledgedHolds(t.Context())
	require.Len(t, worker.holds, count, "unacknowledged, unexpired holds remain owned regardless of count")
	require.Empty(t, released)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("root-%d", i)
		switch i % 4 {
		case 0:
			store.acked[id] = true
		case 1:
			store.archived[id] = true
		case 2:
			hold := worker.holds[id]
			hold.expires = time.Now().Add(-time.Second)
			worker.holds[id] = hold
		}
	}
	worker.releaseAcknowledgedHolds(t.Context())
	require.Len(t, worker.holds, count/4)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("root-%d", i)
		if i%4 == 3 {
			require.Zero(t, released[id], "an unrelated ACK or expiry must not release this root")
			store.acked[id] = true
		} else {
			require.Equal(t, 1, released[id], "ACK, durable archive or TTL releases exactly one held reference")
		}
	}
	worker.releaseAcknowledgedHolds(t.Context())
	worker.releaseAcknowledgedHolds(t.Context())
	require.Empty(t, worker.holds)
	for i := 0; i < count; i++ {
		require.Equal(t, 1, released[fmt.Sprintf("root-%d", i)])
	}
}

func TestCloudBillingPersistsSamplingBreakdownWithoutExtraDebits(t *testing.T) {
	svc, input, ledger := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	samples := []OpenAICloudUsage{{InputTokens: 100, CachedInputTokens: 80, OutputTokens: 5, TotalTokens: 105}, {InputTokens: 120, CachedInputTokens: 100, OutputTokens: 7, TotalTokens: 127}}
	settlement := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "root-sampling-details"}, BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 220, CachedInputTokens: 180, OutputTokens: 12, TotalTokens: 232}, SamplingUsage: samples, ObservedAt: input.PricingAt.Add(time.Minute)}
	require.NoError(t, (&openAICloudBillingWorker{service: svc}).settle(ctx, settlement))
	require.Equal(t, 1, ledger.calls)
	raw, err := json.Marshal(ledger.lastCmd.UsageLog)
	require.NoError(t, err)
	var wire map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &wire))
	require.Contains(t, wire, "sampling_details", "the durable usage row must retain its priced samples, not only a Redis session")
	var detail struct {
		Samples []struct {
			Input      int     `json:"input_tokens"`
			Cached     int     `json:"cache_read_tokens"`
			Output     int     `json:"output_tokens"`
			TotalCost  float64 `json:"total_cost"`
			ActualCost float64 `json:"actual_cost"`
		} `json:"samples"`
	}
	require.NoError(t, json.Unmarshal(wire["sampling_details"], &detail))
	require.Len(t, detail.Samples, 2)
	require.Equal(t, 20, detail.Samples[0].Input)
	require.Equal(t, 80, detail.Samples[0].Cached)
	require.InDelta(t, ledger.lastCmd.UsageLog.TotalCost, detail.Samples[0].TotalCost+detail.Samples[1].TotalCost, 1e-10)
	require.InDelta(t, ledger.lastCmd.UsageLog.ActualCost, detail.Samples[0].ActualCost+detail.Samples[1].ActualCost, 1e-10)
}

func TestCloudSamplingProductionTotalsKeepPerStepContextPricing(t *testing.T) {
	raw, err := os.ReadFile("testdata/cloud_sampling_billing_20261027.json")
	require.NoError(t, err)
	var cases []struct {
		Name     string             `json:"name"`
		Samples  []OpenAICloudUsage `json:"samples"`
		Usage    *OpenAICloudUsage  `json:"usage"`
		Expected float64            `json:"expected_cost"`
	}
	require.NoError(t, json.Unmarshal(raw, &cases))
	svc, _, _ := cloudBillingFixture(t)
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			require.True(t, cloudSamplingUsageValid(tc.Usage, tc.Samples))
			enabled := true
			details := &UsageSamplingDetails{}
			input := CostInput{Ctx: t.Context(), Model: "gpt-6-astra", Tokens: cloudSamplingTokens(*tc.Usage), RequestCount: 1, RateMultiplier: 1, Resolver: NewModelPricingResolver(nil, svc.billingService), LongContextBillingEnabled: &enabled, Resolved: &ResolvedPricing{Mode: BillingModeToken, BasePricing: &ModelPricing{InputPricePerToken: 0.00001, OutputPricePerToken: 0.00005, CacheReadPricePerToken: 0.000001, LongContextInputThreshold: 272000, LongContextInputMultiplier: 2, LongContextOutputMultiplier: 1.5}}}
			cost, err := svc.cloudSamplingCostDetails(input, tc.Samples, details)
			require.NoError(t, err)
			require.InDelta(t, tc.Expected, cost.ActualCost, 1e-9)
			require.False(t, cost.LongContextBillingApplied, "the turn total must not trigger a single-call context tier")
			var sum float64
			for _, segment := range details.Samples {
				require.NotNil(t, segment.ActualCost)
				sum += *segment.ActualCost
			}
			require.InDelta(t, tc.Expected, sum, 1e-9)
		})
	}
}

func TestCloudBillingSamplingDetailsPreservesFreeFastPrices(t *testing.T) {
	svc, input, ledger := cloudBillingFixture(t)
	input.APIKey.Group.FreeOpenAIFast = true
	tier := "priority"
	input.Result.ServiceTier = &tier
	ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	samples := []OpenAICloudUsage{{InputTokens: 100, CachedInputTokens: 80, OutputTokens: 5, TotalTokens: 105}, {InputTokens: 120, CachedInputTokens: 100, OutputTokens: 7, TotalTokens: 127}}
	settlement := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "root-sampling-details"}, BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 220, CachedInputTokens: 180, OutputTokens: 12, TotalTokens: 232}, SamplingUsage: samples, ObservedAt: input.PricingAt.Add(time.Minute)}
	require.NoError(t, (&openAICloudBillingWorker{service: svc}).settle(ctx, settlement))
	require.Equal(t, 1, ledger.calls)
	raw, err := json.Marshal(ledger.lastCmd.UsageLog)
	require.NoError(t, err)
	var wire map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &wire))
	require.Contains(t, wire, "sampling_details", "the durable usage row must retain its priced samples, not only a Redis session")
	var detail struct {
		Samples []struct {
			Input      int     `json:"input_tokens"`
			Cached     int     `json:"cache_read_tokens"`
			Output     int     `json:"output_tokens"`
			TotalCost  float64 `json:"total_cost"`
			ActualCost float64 `json:"actual_cost"`
		} `json:"samples"`
	}
	require.NoError(t, json.Unmarshal(wire["sampling_details"], &detail))
	require.Len(t, detail.Samples, 2)
	require.Equal(t, 20, detail.Samples[0].Input)
	require.Equal(t, 80, detail.Samples[0].Cached)
	require.InDelta(t, ledger.lastCmd.UsageLog.TotalCost, detail.Samples[0].TotalCost+detail.Samples[1].TotalCost, 1e-10)
	require.InDelta(t, ledger.lastCmd.UsageLog.ActualCost, detail.Samples[0].ActualCost+detail.Samples[1].ActualCost, 1e-10)
}
