package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newCloudStoreFixture(t *testing.T) (*gatewayCache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1, ContextTimeoutEnabled: true})
	t.Cleanup(func() { _ = client.Close() })
	return &gatewayCache{rdb: client}, server
}

func cloudStoreRecord(id string, user, account int64) *service.OpenAICloudSessionRecord {
	now := time.Now().UTC()
	return &service.OpenAICloudSessionRecord{OpenAICloudBinding: service.OpenAICloudBinding{OpenAICloudOwner: service.OpenAICloudOwner{UserID: user, APIKeyID: 10, GroupID: 20, AccountID: account, GrantGeneration: "grant", Model: "model", SettingsDigest: "settings"}, SessionID: id, RootRequestID: "root-" + id, OwnerInstance: "instance", ExpiresAt: now.Add(service.OpenAICloudSessionTTL), Status: "created"}, OwnerToken: "owner-" + id, LeaseUntil: now.Add(service.OpenAICloudOwnerLease), BillingSnapshot: json.RawMessage(`{"request_id":"snapshot","owner":1}`)}
}

func cloneCloudStoreRecord(t *testing.T, r *service.OpenAICloudSessionRecord) *service.OpenAICloudSessionRecord {
	t.Helper()
	raw, err := json.Marshal(r)
	require.NoError(t, err)
	var copy service.OpenAICloudSessionRecord
	require.NoError(t, json.Unmarshal(raw, &copy))
	return &copy
}

func cloudStoreSettlement(r *service.OpenAICloudSessionRecord, unknown bool) *service.OpenAICloudSettlement {
	return &service.OpenAICloudSettlement{OpenAICloudBinding: r.OpenAICloudBinding, BillingSnapshot: r.BillingSnapshot, Status: r.Status, Usage: r.Usage, Sequence: r.Sequence, ObservedAt: time.Now(), Unknown: unknown}
}

func TestCloudStoreAdmitsBeyondFormerSessionCeilings(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	const count = 130 // Exceeds the retired global, user and account count limits.
	for i := range count {
		record := cloudStoreRecord(fmt.Sprintf("admitted-%d", i), 1, 1)
		record.Status = "awaiting_tools"
		require.NoError(t, cache.CreateCloudSession(ctx, record), "shared gateway admission must not acquire a second Cloud count limit")
	}
	for i := range count {
		record, err := cache.LoadCloudSession(ctx, fmt.Sprintf("admitted-%d", i))
		require.NoError(t, err)
		require.NotNil(t, record)
		require.Equal(t, "awaiting_tools", record.Status, "new sessions must not evict a pending tool owner")
	}
	live, err := cache.rdb.ZCard(ctx, cloudCachePrefix+"live").Result()
	require.NoError(t, err)
	require.EqualValues(t, count, live, "the live index tracks ownership rather than rejecting admission")
}

func TestCloudStoreLegacyCapacityIndexesAreCleanupOnly(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprintf("terminal_%t", terminal), func(t *testing.T) {
			cache, _ := newCloudStoreFixture(t)
			ctx := t.Context()
			r := cloudStoreRecord("legacy-owner", 1, 1)
			r.Status = "completed"
			require.NoError(t, cache.CreateCloudSession(ctx, r))
			legacyKeys := []string{cloudKey("user", "1"), cloudKey("account", "1")}
			for _, key := range legacyKeys {
				require.Zero(t, cache.rdb.Exists(ctx, key).Val(), "new records never create shadow admission indexes")
				require.NoError(t, cache.rdb.ZAdd(ctx, key, redis.Z{Score: 1, Member: r.SessionID}, redis.Z{Score: 1, Member: "other-live-owner"}).Err())
			}
			if terminal {
				r.Status = "failed"
				r.Sequence++
				require.NoError(t, cache.SaveCloudSession(ctx, r, nil, nil))
			} else {
				require.NoError(t, cache.ReleaseCloudSession(ctx, r.SessionID, r.OwnerToken))
			}
			for _, key := range legacyKeys {
				members, err := cache.rdb.ZRange(ctx, key, 0, -1).Result()
				require.NoError(t, err)
				require.Equal(t, []string{"other-live-owner"}, members, "normal owner cleanup must preserve every unrelated legacy membership")
			}
		})
	}
}

func TestCloudStoreSettlementBacklogDoesNotBlockAdmissionOrNewTurn(t *testing.T) {
	for _, newRoot := range []bool{false, true} {
		t.Run(fmt.Sprintf("new_root_%t", newRoot), func(t *testing.T) {
			cache, _ := newCloudStoreFixture(t)
			ctx := t.Context()
			r := cloudStoreRecord("active-owner", 1, 1)
			if newRoot {
				r.Status = "completed"
				require.NoError(t, cache.CreateCloudSession(ctx, r))
			}
			const pending = 130
			previous := make(map[string][]byte, pending)
			for i := range pending {
				old := cloudStoreRecord(fmt.Sprintf("unsettled-%d", i), 1, 1)
				old.Status = "completed"
				bill := cloudStoreSettlement(old, false)
				raw, err := json.Marshal(bill)
				require.NoError(t, err)
				previous[bill.RootRequestID] = raw
				require.NoError(t, cache.rdb.Set(ctx, cloudKey("settlement", bill.RootRequestID), raw, 0).Err())
				require.NoError(t, cache.rdb.ZAdd(ctx, cloudCachePrefix+"settlements", redis.Z{Score: float64(i), Member: bill.RootRequestID}).Err())
			}
			if newRoot {
				r.RootRequestID = "next-root"
				r.Status = "running"
				r.Sequence++
				r.BillingSnapshot = json.RawMessage("{\"root\":\"next-root\"}")
				require.NoError(t, cache.SaveCloudSession(ctx, r, nil, nil), "an unsettled backlog must not block a new root on its completed owner")
			} else {
				require.NoError(t, cache.CreateCloudSession(ctx, r), "an unsettled backlog must not block native-admitted requests")
			}
			r.Status = "completed"
			r.Sequence++
			require.NoError(t, cache.SaveCloudSession(ctx, r, nil, cloudStoreSettlement(r, false)))
			require.EqualValues(t, pending+1, cache.rdb.ZCard(ctx, cloudCachePrefix+"settlements").Val())
			for root, raw := range previous {
				stored, err := cache.rdb.Get(ctx, cloudKey("settlement", root)).Bytes()
				require.NoError(t, err)
				require.Equal(t, raw, stored, "removing backlog admission must preserve all prior accounting facts byte-for-byte")
			}
			stored, err := cache.LoadCloudSession(ctx, r.SessionID)
			require.NoError(t, err)
			require.NotNil(t, stored)
			require.Equal(t, r.RootRequestID, stored.RootRequestID)
		})
	}
}

func TestCloudStoreReferenceOwnershipAndCollisionAtomicity(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	r := cloudStoreRecord("first", 1, 1)
	require.NoError(t, cache.CreateCloudSession(ctx, r))
	r.Sequence++
	r.Status = "running"
	require.NoError(t, cache.SaveCloudSession(ctx, r, []string{"resp_shared", "call_shared"}, nil))
	for _, scope := range [][3]int64{{2, 10, 20}, {1, 11, 20}, {1, 10, 21}} {
		found, err := cache.FindCloudSession(ctx, scope[0], scope[1], scope[2], "resp_shared")
		require.NoError(t, err)
		require.Nil(t, found)
	}
	found, err := cache.FindCloudSession(ctx, 1, 10, 20, "call_shared")
	require.NoError(t, err)
	require.Equal(t, r.SessionID, found.SessionID)
	other := cloudStoreRecord("second", 1, 1)
	require.NoError(t, cache.CreateCloudSession(ctx, other))
	other.Sequence++
	require.Error(t, cache.SaveCloudSession(ctx, other, []string{"new_ref", "resp_shared"}, nil))
	stored, err := cache.LoadCloudSession(ctx, other.SessionID)
	require.NoError(t, err)
	require.Zero(t, stored.Sequence, "failed reference binding must not publish a partial record")
	found, err = cache.FindCloudSession(ctx, 1, 10, 20, "new_ref")
	require.NoError(t, err)
	require.Nil(t, found, "failed batch must not publish an earlier noncolliding ref")
	other.UserID = 2
	other.Sequence = 0
	other.SessionID = "third"
	other.RootRequestID = "root-third"
	other.OwnerToken = "owner-third"
	require.NoError(t, cache.CreateCloudSession(ctx, other))
	other.Sequence++
	require.NoError(t, cache.SaveCloudSession(ctx, other, []string{"resp_shared"}, nil), "same external reference remains isolated by owner scope")
}

func TestCloudStoreLeaseFencesOldOwnerAndStaleSequence(t *testing.T) {
	cache, server := newCloudStoreFixture(t)
	ctx := t.Context()
	r := cloudStoreRecord("lease", 1, 1)
	require.NoError(t, cache.CreateCloudSession(ctx, r))
	r.Sequence++
	r.Status = "running"
	require.NoError(t, cache.SaveCloudSession(ctx, r, nil, nil))
	stale := cloneCloudStoreRecord(t, r)
	stale.Status = "awaiting_tool_outputs"
	require.Error(t, cache.SaveCloudSession(ctx, stale, nil, nil), "same sequence cannot overwrite a later snapshot")
	ok, err := cache.RenewCloudSession(ctx, r.SessionID, "not-owner", time.Now().Add(service.OpenAICloudOwnerLease))
	require.NoError(t, err)
	require.False(t, ok)
	server.FastForward(service.OpenAICloudOwnerLease + time.Second)
	claimed, ok, err := cache.ClaimCloudSession(ctx, r.SessionID, "new-owner", service.OpenAICloudOwnerLease)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "new-owner", claimed.OwnerToken)
	r.Sequence++
	require.Error(t, cache.SaveCloudSession(ctx, r, nil, nil), "expired socket must never publish after recovery ownership changes")
	claimed.Sequence++
	require.NoError(t, cache.SaveCloudSession(ctx, claimed, nil, nil))
}

func TestCloudStoreImmutableBillingAndOwnershipMetadata(t *testing.T) {
	for name, change := range map[string]func(*service.OpenAICloudSessionRecord){
		"probe":    func(r *service.OpenAICloudSessionRecord) { r.Probe = true },
		"user":     func(r *service.OpenAICloudSessionRecord) { r.UserID++ },
		"key":      func(r *service.OpenAICloudSessionRecord) { r.APIKeyID++ },
		"group":    func(r *service.OpenAICloudSessionRecord) { r.GroupID++ },
		"account":  func(r *service.OpenAICloudSessionRecord) { r.AccountID++ },
		"grant":    func(r *service.OpenAICloudSessionRecord) { r.GrantGeneration = "new" },
		"model":    func(r *service.OpenAICloudSessionRecord) { r.Model = "other" },
		"settings": func(r *service.OpenAICloudSessionRecord) { r.SettingsDigest = "other" },
		"billing":  func(r *service.OpenAICloudSessionRecord) { r.BillingSnapshot = json.RawMessage(`{"owner":999}`) },
		"instance": func(r *service.OpenAICloudSessionRecord) { r.OwnerInstance = "forged" },
	} {
		t.Run(name, func(t *testing.T) {
			cache, _ := newCloudStoreFixture(t)
			r := cloudStoreRecord("immutable", 1, 1)
			r.Status = "running"
			require.NoError(t, cache.CreateCloudSession(t.Context(), r))
			changed := cloneCloudStoreRecord(t, r)
			changed.Sequence++
			change(changed)
			require.Error(t, cache.SaveCloudSession(t.Context(), changed, nil, nil))
			stored, err := cache.LoadCloudSession(t.Context(), r.SessionID)
			require.NoError(t, err)
			require.Equal(t, r.OpenAICloudOwner, stored.OpenAICloudOwner)
			require.JSONEq(t, string(r.BillingSnapshot), string(stored.BillingSnapshot))
		})
	}
}

func TestCloudStoreStreamsOrderedBoundedAndCancelled(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	r := cloudStoreRecord("streams", 1, 1)
	require.NoError(t, cache.CreateCloudSession(ctx, r))
	for i := 0; i < 3; i++ {
		require.NoError(t, cache.EnqueueCloudCommand(ctx, r.SessionID, service.OpenAICloudCommand{RequestID: fmt.Sprintf("request-%d", i), Request: service.OpenAICloudRequest{Owner: r.OpenAICloudOwner}}))
	}
	messages, err := cache.ReadCloudCommands(ctx, r.SessionID, "")
	require.NoError(t, err)
	require.Len(t, messages, 3)
	for i, m := range messages {
		var cmd service.OpenAICloudCommand
		require.NoError(t, json.Unmarshal(m.Payload, &cmd))
		require.Equal(t, fmt.Sprintf("request-%d", i), cmd.RequestID)
	}
	messages, err = cache.ReadCloudCommands(ctx, r.SessionID, messages[0].ID)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	for i := 3; i < service.OpenAICloudMaxCommands; i++ {
		require.NoError(t, cache.EnqueueCloudCommand(ctx, r.SessionID, service.OpenAICloudCommand{RequestID: fmt.Sprintf("request-%d", i), Request: service.OpenAICloudRequest{Owner: r.OpenAICloudOwner}}))
	}
	require.Error(t, cache.EnqueueCloudCommand(ctx, r.SessionID, service.OpenAICloudCommand{RequestID: "overflow", Request: service.OpenAICloudRequest{Owner: r.OpenAICloudOwner}}))
	// Use the same bounded primitive with small limits to exercise exact byte
	// and event count edges without allocating a production-sized event stream.
	payload := service.OpenAICloudStreamEvent{Error: "bounded"}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, cache.cloudAppend(ctx, "event", "byte-limit", raw, nil, 2, len(raw)))
	require.Error(t, cache.cloudAppend(ctx, "event", "byte-limit", raw, nil, 2, len(raw)))
	require.NoError(t, cache.cloudAppend(ctx, "event", "entry-limit", raw, nil, 1, 4096))
	require.Error(t, cache.cloudAppend(ctx, "event", "entry-limit", raw, nil, 1, 4096))
	require.Error(t, cache.PublishCloudEvent(ctx, "oversized", service.OpenAICloudStreamEvent{Error: strings.Repeat("x", service.OpenAICloudMaxEnvelopeBytes)}))
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = cache.ReadCloudEvents(cancelled, "cancelled", "0-0")
	require.ErrorIs(t, err, context.Canceled)
	require.Error(t, cache.EnqueueCloudCommand(ctx, "missing-session", service.OpenAICloudCommand{RequestID: "no-owner"}))
}

func TestCloudStoreSettlementClaimsAckAndReceipt(t *testing.T) {
	cache, server := newCloudStoreFixture(t)
	ctx := t.Context()
	r := cloudStoreRecord("billing", 1, 1)
	require.NoError(t, cache.CreateCloudSession(ctx, r))
	r.Status = "completed"
	r.Sequence++
	r.Usage = &service.OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}
	require.NoError(t, cache.SaveCloudSession(ctx, r, nil, cloudStoreSettlement(r, false)))
	rows, err := cache.ListCloudSettlements(ctx, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	ok, err := cache.ClaimCloudSettlement(ctx, r.RootRequestID, "worker-a", time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = cache.ClaimCloudSettlement(ctx, r.RootRequestID, "worker-b", time.Second)
	require.NoError(t, err)
	require.False(t, ok)
	require.Error(t, cache.AckCloudSettlement(ctx, r.RootRequestID, "worker-b"))
	server.FastForward(2 * time.Second)
	ok, err = cache.ClaimCloudSettlement(ctx, r.RootRequestID, "worker-b", time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	require.Error(t, cache.AckCloudSettlement(ctx, r.RootRequestID, "worker-a"))
	require.NoError(t, cache.AckCloudSettlement(ctx, r.RootRequestID, "worker-b"))
	acked, err := cache.CloudSettlementAcknowledged(ctx, r.RootRequestID)
	require.NoError(t, err)
	require.True(t, acked)
	rows, err = cache.ListCloudSettlements(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, rows)
	ok, err = cache.ClaimCloudSettlement(ctx, r.RootRequestID, "worker-a", time.Second)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestCloudStoreUnknownSettlementSurvivesRetention(t *testing.T) {
	cache, server := newCloudStoreFixture(t)
	ctx := t.Context()
	r := cloudStoreRecord("unknown", 1, 1)
	require.NoError(t, cache.CreateCloudSession(ctx, r))
	r.Status = "unknown"
	r.Sequence++
	require.NoError(t, cache.SaveCloudSession(ctx, r, nil, cloudStoreSettlement(r, true)))
	server.FastForward(service.OpenAICloudSettlementTTL + time.Hour)
	rows, err := cache.ListCloudSettlements(ctx, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Unknown)
	require.Nil(t, rows[0].Usage)
}

func TestCloudStoreSettlementCannotReplaceBillingFacts(t *testing.T) {
	for name, change := range map[string]func(*service.OpenAICloudSettlement){
		"billing snapshot": func(s *service.OpenAICloudSettlement) { s.BillingSnapshot = json.RawMessage(`{"owner":999}`) },
		"sequence":         func(s *service.OpenAICloudSettlement) { s.Sequence++ },
		"usage": func(s *service.OpenAICloudSettlement) {
			s.Usage = &service.OpenAICloudUsage{InputTokens: 999, TotalTokens: 999}
		},
	} {
		t.Run(name, func(t *testing.T) {
			cache, _ := newCloudStoreFixture(t)
			r := cloudStoreRecord("settlement-facts", 1, 1)
			require.NoError(t, cache.CreateCloudSession(t.Context(), r))
			r.Sequence++
			r.Status = "completed"
			r.Usage = &service.OpenAICloudUsage{InputTokens: 3, OutputTokens: 1, TotalTokens: 4}
			s := cloudStoreSettlement(r, false)
			change(s)
			require.Error(t, cache.SaveCloudSession(t.Context(), r, nil, s))
			stored, err := cache.LoadCloudSession(t.Context(), r.SessionID)
			require.NoError(t, err)
			require.Equal(t, "created", stored.Status)
			rows, err := cache.ListCloudSettlements(t.Context(), 10)
			require.NoError(t, err)
			require.Empty(t, rows)
		})
	}
}

func TestCloudStoreSettlementRootCollisionCannotOverwriteOwner(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	first := cloudStoreRecord("root-collision-first", 1, 1)
	require.NoError(t, cache.CreateCloudSession(ctx, first))
	first.Status = "completed"
	first.Sequence++
	first.Usage = &service.OpenAICloudUsage{InputTokens: 1, TotalTokens: 1}
	require.NoError(t, cache.SaveCloudSession(ctx, first, nil, cloudStoreSettlement(first, false)))
	second := cloudStoreRecord("root-collision-second", 2, 2)
	second.RootRequestID = first.RootRequestID
	err := cache.CreateCloudSession(ctx, second)
	if err == nil {
		second.Status = "completed"
		second.Sequence++
		second.Usage = &service.OpenAICloudUsage{InputTokens: 999, TotalTokens: 999}
		require.Error(t, cache.SaveCloudSession(ctx, second, nil, cloudStoreSettlement(second, false)))
	}
	rows, err := cache.ListCloudSettlements(ctx, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, first.UserID, rows[0].UserID)
	require.Equal(t, 1, rows[0].Usage.InputTokens)
}

func TestCloudStoreNewTurnPreservesPreviousSettlement(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	r := cloudStoreRecord("turns", 1, 1)
	require.NoError(t, cache.CreateCloudSession(ctx, r))
	r.Sequence++
	r.Status = "running"
	require.NoError(t, cache.SaveCloudSession(ctx, r, nil, nil))
	changed := cloneCloudStoreRecord(t, r)
	changed.RootRequestID = "forged-active-root"
	changed.Sequence++
	require.Error(t, cache.SaveCloudSession(ctx, changed, nil, nil))
	r.Sequence++
	r.Status = "completed"
	r.Usage = &service.OpenAICloudUsage{InputTokens: 3, OutputTokens: 1, TotalTokens: 4}
	oldRoot := r.RootRequestID
	require.NoError(t, cache.SaveCloudSession(ctx, r, nil, cloudStoreSettlement(r, false)))
	r.Sequence++
	r.RootRequestID = "next-user-turn"
	r.Status = "running"
	r.Usage = nil
	r.BillingSnapshot = json.RawMessage(`{"request_id":"next-user-turn","owner":1}`)
	require.NoError(t, cache.SaveCloudSession(ctx, r, nil, nil), "a completed thread may accept the next user turn under its live owner")
	rows, err := cache.ListCloudSettlements(ctx, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, oldRoot, rows[0].RootRequestID)
	r.Sequence++
	r.Status = "completed"
	r.Usage = &service.OpenAICloudUsage{InputTokens: 4, OutputTokens: 2, TotalTokens: 6}
	require.NoError(t, cache.SaveCloudSession(ctx, r, nil, cloudStoreSettlement(r, false)))
	rows, err = cache.ListCloudSettlements(ctx, 10)
	require.NoError(t, err)
	require.Len(t, rows, 2, "a subsequent turn must not replace the previous bill")
}

func TestCloudStoreUnavailableFailsClosed(t *testing.T) {
	for _, cache := range []*gatewayCache{nil, {}} {
		r := cloudStoreRecord("unavailable", 1, 1)
		calls := []func() error{
			func() error { return cache.CreateCloudSession(t.Context(), r) },
			func() error { _, err := cache.LoadCloudSession(t.Context(), r.SessionID); return err },
			func() error { _, err := cache.FindCloudSession(t.Context(), 1, 10, 20, "ref"); return err },
			func() error { return cache.SaveCloudSession(t.Context(), r, nil, nil) },
			func() error {
				_, err := cache.RenewCloudSession(t.Context(), r.SessionID, r.OwnerToken, time.Now().Add(time.Second))
				return err
			},
			func() error { return cache.EnqueueCloudCommand(t.Context(), r.SessionID, service.OpenAICloudCommand{}) },
			func() error { _, err := cache.ReadCloudCommands(t.Context(), r.SessionID, ""); return err },
			func() error { return cache.PublishCloudEvent(t.Context(), "request", service.OpenAICloudStreamEvent{}) },
			func() error { _, err := cache.ReadCloudEvents(t.Context(), "request", ""); return err },
			func() error { _, err := cache.ListCloudSettlements(t.Context(), 1); return err },
			func() error {
				_, err := cache.ClaimCloudSettlement(t.Context(), r.RootRequestID, "worker", time.Second)
				return err
			},
			func() error { return cache.AckCloudSettlement(t.Context(), r.RootRequestID, "worker") },
			func() error { _, err := cache.CloudSettlementAcknowledged(t.Context(), r.RootRequestID); return err },
			func() error { _, err := cache.ListExpiredCloudSessions(t.Context(), 1); return err },
			func() error {
				_, _, err := cache.ClaimCloudSession(t.Context(), r.SessionID, "worker", time.Second)
				return err
			},
		}
		for i, call := range calls {
			t.Run(fmt.Sprintf("nil=%t/method=%d", cache == nil, i), func(t *testing.T) { require.NotPanics(t, func() { require.Error(t, call()) }) })
		}
	}
}

func TestCloudStoreArchivedFactsAreFencedAndNeverAcknowledged(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	r := cloudStoreRecord("unknown", 1, 1)
	require.NoError(t, cache.CreateCloudSession(ctx, r))
	r.Status = "unknown"
	r.Sequence++
	bill := cloudStoreSettlement(r, true)
	require.NoError(t, cache.SaveCloudSession(ctx, r, nil, bill))
	before, err := cache.rdb.Get(ctx, cloudKey("settlement", bill.RootRequestID)).Bytes()
	require.NoError(t, err)
	claimed, err := cache.ClaimCloudSettlement(ctx, bill.RootRequestID, "worker", time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Error(t, cache.AckCloudSettlement(ctx, bill.RootRequestID, "worker"))
	require.Error(t, cache.ArchiveCloudSettlement(ctx, bill.RootRequestID, "other", before))
	require.Error(t, cache.ArchiveCloudSettlement(ctx, bill.RootRequestID, "worker", append(append([]byte{}, before...), ' ')))
	require.NoError(t, cache.ArchiveCloudSettlement(ctx, bill.RootRequestID, "worker", before))
	require.NoError(t, cache.ArchiveCloudSettlement(ctx, bill.RootRequestID, "worker", before))
	pending, err := cache.ListCloudSettlements(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, pending)
	archived, err := cache.CloudSettlementArchived(ctx, bill.RootRequestID)
	require.NoError(t, err)
	require.True(t, archived)
	acked, err := cache.CloudSettlementAcknowledged(ctx, bill.RootRequestID)
	require.NoError(t, err)
	require.False(t, acked)
	require.Zero(t, cache.rdb.Exists(ctx, cloudKey("settlement", bill.RootRequestID)).Val(), "raw facts move to durable SQL, not permanent Redis storage")
	require.NoError(t, cache.CreateCloudSession(ctx, cloudStoreRecord("same-account", 1, 1)))
}

func TestCloudStoreListsMalformedAndLegacyUnknownWithoutBlockingValidBills(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	for i, id := range []string{"bad", "good", "legacy"} {
		r := cloudStoreRecord(id, int64(i+1), int64(i+1))
		require.NoError(t, cache.CreateCloudSession(ctx, r))
		r.Status = "unknown"
		r.Sequence++
		require.NoError(t, cache.SaveCloudSession(ctx, r, nil, cloudStoreSettlement(r, true)))
	}
	raw := []byte("{broken-json")
	require.NoError(t, cache.rdb.Set(ctx, cloudKey("settlement", "root-bad"), raw, 0).Err())
	require.NoError(t, cache.rdb.ZRem(ctx, cloudCachePrefix+"settlements", "root-legacy").Err())
	require.NoError(t, cache.rdb.ZAdd(ctx, cloudCachePrefix+"unknown_settlements", redis.Z{Score: 1, Member: "root-legacy"}).Err())
	bills, err := cache.ListCloudSettlements(ctx, 10)
	require.NoError(t, err)
	require.Len(t, bills, 3)
	byID := map[string]*service.OpenAICloudSettlement{}
	for _, bill := range bills {
		byID[bill.RootRequestID] = bill
	}
	require.Equal(t, "invalid_record", byID["root-bad"].ArchiveReason)
	require.Equal(t, raw, byID["root-bad"].ArchivePayload)
	require.Empty(t, byID["root-good"].ArchiveReason)
	require.True(t, byID["root-legacy"].Unknown)
}

func TestCloudStoreRenewalExtendsAdmissionWithoutAllowingSaveToForgeExpiry(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	r := cloudStoreRecord("active", 1, 1)
	r.ExpiresAt = time.Now().Add(time.Second)
	initialExpiry := r.ExpiresAt
	require.NoError(t, cache.CreateCloudSession(ctx, r))
	ok, err := cache.RenewCloudSession(ctx, r.SessionID, r.OwnerToken, time.Now().Add(service.OpenAICloudOwnerLease))
	require.NoError(t, err)
	require.True(t, ok)
	current, err := cache.LoadCloudSession(ctx, r.SessionID)
	require.NoError(t, err)
	require.True(t, current.ExpiresAt.After(r.ExpiresAt.Add(10*time.Minute)))
	// A stale in-memory actor may save safely; a forged expiry cannot extend it.
	r.ExpiresAt = time.Now().Add(24 * time.Hour)
	r.Sequence++
	require.NoError(t, cache.SaveCloudSession(ctx, r, nil, nil))
	after, err := cache.LoadCloudSession(ctx, r.SessionID)
	require.NoError(t, err)
	require.Equal(t, current.ExpiresAt, after.ExpiresAt)
	for _, key := range []string{cloudCachePrefix + "live"} {
		require.Equal(t, float64(initialExpiry.UnixMilli()), cache.rdb.ZScore(ctx, key, r.SessionID).Val(), "lifecycle membership does not need a score rewrite on every heartbeat")
		require.EqualValues(t, 1, cache.rdb.ZCard(ctx, key).Val(), "renewal must preserve live ownership")
	}
	require.Equal(t, float64(current.LeaseUntil.UnixMilli()), cache.rdb.ZScore(ctx, cloudCachePrefix+"leases", r.SessionID).Val())
}

// A long session sends one command per tool reply. Consumed commands must not
// keep counting against the bounded stream, or the session stops accepting work.
func TestCloudStoreConsumedCommandsAreReclaimed(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	r := cloudStoreRecord("reclaim", 1, 1)
	require.NoError(t, cache.CreateCloudSession(ctx, r))
	enqueue := func(id string) error {
		return cache.EnqueueCloudCommand(ctx, r.SessionID, service.OpenAICloudCommand{RequestID: id, Request: service.OpenAICloudRequest{Owner: r.OpenAICloudOwner}})
	}
	cursor := ""
	for round := 0; round < 3; round++ {
		for i := 0; i < service.OpenAICloudMaxCommands; i++ {
			require.NoError(t, enqueue(fmt.Sprintf("round-%d-%d", round, i)), "round %d command %d", round, i)
		}
		messages, err := cache.ReadCloudCommands(ctx, r.SessionID, cursor)
		require.NoError(t, err)
		require.Len(t, messages, service.OpenAICloudMaxCommands)
		cursor = messages[len(messages)-1].ID
		require.Error(t, cache.ReclaimCloudCommands(ctx, r.SessionID, "not-the-owner", cursor), "only the owner may delete its queue")
		require.NoError(t, cache.ReclaimCloudCommands(ctx, r.SessionID, r.OwnerToken, cursor))
	}
	messages, err := cache.ReadCloudCommands(ctx, r.SessionID, "0-0")
	require.NoError(t, err)
	require.Empty(t, messages, "reclaimed commands are removed")
}

// Cancellation shares the session queue but must still be deliverable when
// business commands have filled it.
func TestCloudStoreCancelCommandHasReservedCapacity(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	r := cloudStoreRecord("cancel-reserve", 1, 1)
	require.NoError(t, cache.CreateCloudSession(ctx, r))
	for i := 0; i < service.OpenAICloudMaxCommands; i++ {
		require.NoError(t, cache.EnqueueCloudCommand(ctx, r.SessionID, service.OpenAICloudCommand{RequestID: fmt.Sprintf("work-%d", i), Request: service.OpenAICloudRequest{Owner: r.OpenAICloudOwner}}))
	}
	require.Error(t, cache.EnqueueCloudCommand(ctx, r.SessionID, service.OpenAICloudCommand{RequestID: "work-overflow", Request: service.OpenAICloudRequest{Owner: r.OpenAICloudOwner}}))
	require.NoError(t, cache.EnqueueCloudCommand(ctx, r.SessionID, service.OpenAICloudCommand{RequestID: "work-0", Request: service.OpenAICloudRequest{Owner: r.OpenAICloudOwner}, Cancel: true}))
	require.NoError(t, cache.EnqueueCloudCommand(ctx, r.SessionID, service.OpenAICloudCommand{RequestID: "close", Request: service.OpenAICloudRequest{Owner: r.OpenAICloudOwner}, CloseCompleted: true}))
}

func TestCloudStoreLatencyFirstMeasurementWins(t *testing.T) {
	cache, server := newCloudStoreFixture(t)
	ctx := t.Context()
	missing, err := cache.LoadCloudLatency(ctx, "root")
	require.NoError(t, err)
	require.Nil(t, missing)
	first := 12
	require.NoError(t, cache.RecordCloudLatency(ctx, "root", &service.UsageLatencyMetrics{Version: 1, FirstTextMs: &first, DurationMs: 40}))
	require.NoError(t, cache.RecordCloudLatency(ctx, "root", &service.UsageLatencyMetrics{Version: 1, DurationMs: 999}))
	loaded, err := cache.LoadCloudLatency(ctx, "root")
	require.NoError(t, err)
	require.Equal(t, 40, loaded.DurationMs)
	require.Equal(t, 12, *loaded.FirstTextMs)
	require.Greater(t, server.TTL(cloudKey("latency", "root")), time.Duration(0))
}

// Redis cjson keeps 14 significant digits, encodes [] as {} and loses integers
// beyond 2^53. A record re-encoded by Lua therefore no longer equals the
// actor's own bytes, and the owner fence rejects every later save. Renewal,
// recovery claim and release must leave the stored bytes untouched.
func TestCloudStoreKeepsRecordBytesExactAcrossRenewClaimRelease(t *testing.T) {
	cache, server := newCloudStoreFixture(t)
	ctx := t.Context()
	r := cloudStoreRecord("exact", 1, 1)
	r.BillingSnapshot = json.RawMessage(`{"price":7.500000000000001e-05,"scopes":[],"stamp":1234567890123456789,"ratio":0.1}`)
	require.NoError(t, cache.CreateCloudSession(ctx, r))
	stored, err := cache.LoadCloudSession(ctx, r.SessionID)
	require.NoError(t, err)
	require.Equal(t, string(r.BillingSnapshot), string(stored.BillingSnapshot))

	ok, err := cache.RenewCloudSession(ctx, r.SessionID, r.OwnerToken, time.Now().Add(service.OpenAICloudOwnerLease))
	require.NoError(t, err)
	require.True(t, ok)
	renewed, err := cache.LoadCloudSession(ctx, r.SessionID)
	require.NoError(t, err)
	require.Equal(t, string(r.BillingSnapshot), string(renewed.BillingSnapshot), "a heartbeat must not rewrite the record")
	require.True(t, renewed.LeaseUntil.After(r.LeaseUntil.Add(-time.Second)))
	require.True(t, renewed.ExpiresAt.After(r.ExpiresAt.Add(-time.Second)))
	raw, err := cache.rdb.Get(ctx, cloudKey("session", r.SessionID)).Bytes()
	require.NoError(t, err)
	expected, err := json.Marshal(r)
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(raw), "the stored record is the exact Go encoding")

	// The actor's own exact bytes still pass the fence after the heartbeat.
	r.Sequence++
	r.Status = "running"
	r.ThreadID = "thread"
	require.NoError(t, cache.SaveCloudSession(ctx, r, nil, nil))
	r.Sequence++
	r.Status = "completed"
	require.NoError(t, cache.SaveCloudSession(ctx, r, []string{"resp_cloud_exact"}, nil))

	server.FastForward(service.OpenAICloudOwnerLease + time.Second)
	claimed, ok, err := cache.ClaimCloudSession(ctx, r.SessionID, "recovery", service.OpenAICloudOwnerLease)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "recovery", claimed.OwnerToken)
	require.Equal(t, "recovery", claimed.OwnerInstance)
	require.Equal(t, string(r.BillingSnapshot), string(claimed.BillingSnapshot), "recovery reads the exact bytes")
	require.Equal(t, renewed.ExpiresAt, claimed.ExpiresAt, "the renewed expiry is kept across the claim")
	loaded, err := cache.LoadCloudSession(ctx, r.SessionID)
	require.NoError(t, err)
	require.Equal(t, "recovery", loaded.OwnerToken)
	require.Equal(t, string(r.BillingSnapshot), string(loaded.BillingSnapshot))

	claimed.Sequence++
	require.NoError(t, cache.SaveCloudSession(ctx, claimed, nil, nil), "the recovered owner saves exact bytes under its own token")
	require.NoError(t, cache.ReleaseCloudSession(ctx, r.SessionID, "recovery"))
	closed, err := cache.LoadCloudSession(ctx, r.SessionID)
	require.NoError(t, err)
	require.Equal(t, "closed", closed.Status)
	require.Equal(t, string(r.BillingSnapshot), string(closed.BillingSnapshot), "release must not re-encode the record either")
	require.Equal(t, "recovery", closed.OwnerToken)
}

// A cross-instance command may carry a request body well above the record
// envelope, is stored byte-exact without HTML escaping, is still fenced to the
// session owner, and is refused above OpenAICloudMaxCommandBytes.
func TestCloudStoreCommandCarriesLargeBodyWithoutEscaping(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	r := cloudStoreRecord("large-command", 1, 1)
	require.NoError(t, cache.CreateCloudSession(ctx, r))
	body := json.RawMessage(`{"model":"model","input":"` + strings.Repeat("<img>&", (24<<20)/6) + `"}`)
	command := service.OpenAICloudCommand{RequestID: "large", Request: service.OpenAICloudRequest{Owner: r.OpenAICloudOwner, Body: body, RootRequestID: "root", BillingSnapshot: r.BillingSnapshot}}
	require.NoError(t, cache.EnqueueCloudCommand(ctx, r.SessionID, command))
	messages, err := cache.ReadCloudCommands(ctx, r.SessionID, "")
	require.NoError(t, err)
	require.Len(t, messages, 1)
	expected, err := service.OpenAICloudEncodeCommand(command)
	require.NoError(t, err)
	require.Equal(t, string(expected), string(messages[0].Payload))
	require.Greater(t, len(messages[0].Payload), service.OpenAICloudMaxEnvelopeBytes)
	require.Contains(t, string(messages[0].Payload), `<img>&`, "stored without HTML escaping")
	require.NotContains(t, string(messages[0].Payload), `\u003c`)
	var decoded service.OpenAICloudCommand
	require.NoError(t, json.Unmarshal(messages[0].Payload, &decoded))
	require.Equal(t, string(body), string(decoded.Request.Body))
	require.NoError(t, cache.ReclaimCloudCommands(ctx, r.SessionID, r.OwnerToken, messages[0].ID))

	other := command
	other.Request.Owner.APIKeyID++
	require.ErrorIs(t, cache.EnqueueCloudCommand(ctx, r.SessionID, other), service.ErrOpenAICloudCommandRejected, "the owner check still fences the stream")
	oversized := command
	oversized.Request.Body = json.RawMessage(`{"model":"model","input":"` + strings.Repeat("x", service.OpenAICloudMaxCommandBytes) + `"}`)
	err = cache.EnqueueCloudCommand(ctx, r.SessionID, oversized)
	require.Error(t, err)
	require.NotErrorIs(t, err, service.ErrOpenAICloudCommandRejected)
	messages, err = cache.ReadCloudCommands(ctx, r.SessionID, "")
	require.NoError(t, err)
	require.Empty(t, messages)
}
