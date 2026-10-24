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

func TestCloudStoreGlobalUserAccountCapacity(t *testing.T) {
	for _, scope := range []string{"global", "user", "account"} {
		t.Run(scope, func(t *testing.T) {
			cache, _ := newCloudStoreFixture(t)
			limit := service.OpenAICloudMaxSessions
			if scope == "user" {
				limit = service.OpenAICloudMaxUserSessions
			}
			if scope == "account" {
				limit = service.OpenAICloudMaxAccountSessions
			}
			for i := 0; i <= limit; i++ {
				user, account := int64(i+1), int64(i+1)
				if scope == "user" {
					user = 1
				}
				if scope == "account" {
					account = 1
				}
				err := cache.CreateCloudSession(t.Context(), cloudStoreRecord(fmt.Sprintf("capacity-%d", i), user, account))
				if i < limit {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
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
		"expiry":   func(r *service.OpenAICloudSessionRecord) { r.ExpiresAt = r.ExpiresAt.Add(time.Hour) },
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
	require.NoError(t, cache.cloudAppend(ctx, "event", "byte-limit", payload, 2, len(raw)))
	require.Error(t, cache.cloudAppend(ctx, "event", "byte-limit", payload, 2, len(raw)))
	require.NoError(t, cache.cloudAppend(ctx, "event", "entry-limit", payload, 1, 4096))
	require.Error(t, cache.cloudAppend(ctx, "event", "entry-limit", payload, 1, 4096))
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

func TestCloudStoreIdleCapacityReclamation(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	for i := 0; i < 12; i++ {
		r := cloudStoreRecord(fmt.Sprintf("sequential-%d", i), 1, 1)
		err := cache.CreateCloudSession(ctx, r)
		if i >= service.OpenAICloudMaxUserSessions {
			require.ErrorIs(t, err, service.ErrOpenAICloudCapacity)
			idle, err := cache.FindCloudIdleSession(ctx, 1, 1)
			require.NoError(t, err)
			require.NotNil(t, idle)
			require.NoError(t, cache.ReleaseCloudSession(ctx, idle.SessionID, idle.OwnerToken))
			require.NoError(t, cache.CreateCloudSession(ctx, r))
		} else {
			require.NoError(t, err)
		}
		r.Sequence++
		r.Status = "completed"
		require.NoError(t, cache.SaveCloudSession(ctx, r, nil, nil))
	}
	cache2, _ := newCloudStoreFixture(t)
	for i := 0; i < service.OpenAICloudMaxUserSessions; i++ {
		r := cloudStoreRecord(fmt.Sprintf("busy-%d", i), 1, 1)
		r.Status = "awaiting_tool_outputs"
		require.NoError(t, cache2.CreateCloudSession(ctx, r))
	}
	idle, err := cache2.FindCloudIdleSession(ctx, 1, 1)
	require.NoError(t, err)
	require.Nil(t, idle, "tool wait must never be reclaimed")
}

func TestCloudStoreUnknownFactsDoNotBlockAccountOrBillingQueue(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	r := cloudStoreRecord("unknown", 1, 1)
	require.NoError(t, cache.CreateCloudSession(ctx, r))
	r.Status = "unknown"
	r.Sequence++
	bill := cloudStoreSettlement(r, true)
	require.NoError(t, cache.SaveCloudSession(ctx, r, nil, bill))
	stored, err := cache.GetCloudSettlement(ctx, bill.RootRequestID)
	require.NoError(t, err)
	before, err := cache.rdb.Get(ctx, cloudKey("settlement", bill.RootRequestID)).Bytes()
	require.NoError(t, err)
	stale := *stored
	stale.Sequence++
	require.Error(t, cache.RetainCloudUnknownSettlement(ctx, &stale), "retention must not discard concurrent evidence")
	claimed, err := cache.ClaimCloudSettlement(ctx, bill.RootRequestID, "worker", time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Error(t, cache.AckCloudSettlement(ctx, bill.RootRequestID, "worker"), "unknown facts are never a successful debit")
	require.Error(t, cache.RetainCloudUnknownSettlement(ctx, stored), "an active billing claim remains fenced")
	require.NoError(t, cache.rdb.Del(ctx, cloudKey("settlement_claim", bill.RootRequestID)).Err())
	require.NoError(t, cache.RetainCloudUnknownSettlement(ctx, stored))
	require.NoError(t, cache.RetainCloudUnknownSettlement(ctx, stored), "retention is idempotent")
	after, err := cache.rdb.Get(ctx, cloudKey("settlement", bill.RootRequestID)).Bytes()
	require.NoError(t, err)
	require.Equal(t, before, after, "unknown facts are preserved byte-for-byte")
	pending, err := cache.ListCloudSettlements(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, pending)
	acked, err := cache.CloudSettlementAcknowledged(ctx, bill.RootRequestID)
	require.NoError(t, err)
	require.False(t, acked)
	require.Equal(t, int64(1), cache.rdb.ZCard(ctx, cloudCachePrefix+"unknown_settlements").Val())
	// A stale account index written by an older version is no longer read.
	require.NoError(t, cache.rdb.SAdd(ctx, cloudKey("account_unsettled", "1"), bill.RootRequestID).Err())
	next := cloudStoreRecord("same-account", 1, 1)
	next.GrantGeneration = "new-grant"
	require.NoError(t, cache.CreateCloudSession(ctx, next))
}
