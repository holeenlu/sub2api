//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Cloud also uses sets and streams, which the general integration prefix hook
// does not handle. Keep both independent clients inside the same test namespace.
type cloudIntegrationPrefixHook struct{ prefixHook }

func (h cloudIntegrationPrefixHook) prefixCloudCmd(cmd redis.Cmder) {
	h.prefixCmd(cmd)
	args := cmd.Args()
	prefix := func(i int) {
		key, ok := args[i].(string)
		if ok && !strings.HasPrefix(key, h.prefix) {
			args[i] = h.prefix + key
		}
	}
	switch strings.ToLower(cmd.Name()) {
	case "scard":
		prefix(1)
	case "xread":
		for i := 1; i < len(args); i++ {
			if strings.EqualFold(fmt.Sprint(args[i]), "streams") {
				keyCount := (len(args) - i - 1) / 2
				for j := 1; j <= keyCount; j++ {
					prefix(i + j)
				}
				break
			}
		}
	}
}

func (h cloudIntegrationPrefixHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		h.prefixCloudCmd(cmd)
		return next(ctx, cmd)
	}
}

func (h cloudIntegrationPrefixHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			h.prefixCloudCmd(cmd)
		}
		return next(ctx, cmds)
	}
}

func newCloudRealRedisStores(t *testing.T) (*gatewayCache, *gatewayCache) {
	t.Helper()
	prefix := fmt.Sprintf("it:%s:%d:%d:", sanitizeRedisNamespace(t.Name()), time.Now().UnixNano(), atomic.AddUint64(&redisNamespaceSeq, 1))
	options := *integrationRedis.Options()
	a, b := redis.NewClient(&options), redis.NewClient(&options)
	for _, client := range []*redis.Client{a, b} {
		client.AddHook(cloudIntegrationPrefixHook{prefixHook{prefix: prefix}})
	}
	t.Cleanup(func() {
		defer a.Close()
		defer b.Close()
		ctx := context.Background()
		var cursor uint64
		for {
			keys, next, err := integrationRedis.Scan(ctx, cursor, prefix+"*", 500).Result()
			require.NoError(t, err)
			if len(keys) > 0 {
				require.NoError(t, integrationRedis.Unlink(ctx, keys...).Err())
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	})
	return &gatewayCache{rdb: a}, &gatewayCache{rdb: b}
}

func TestCloudStoreRealRedisCrossClientToolsAndSettlement(t *testing.T) {
	a, b := newCloudRealRedisStores(t)
	ctx := t.Context()
	r := cloudStoreRecord("tools", 1, 1)
	require.NoError(t, a.CreateCloudSession(ctx, r))
	ttl, err := b.rdb.PTTL(ctx, cloudKey("session", r.SessionID)).Result()
	require.NoError(t, err)
	require.Equal(t, time.Duration(-1), ttl, "active work must survive loss of its owner lease")
	r.Sequence++
	r.Status = "awaiting_tool_outputs"
	require.NoError(t, a.SaveCloudSession(ctx, r, []string{"resp_tools", "call_tools"}, nil))
	for _, ref := range []string{"resp_tools", "call_tools"} {
		found, err := b.FindCloudSession(ctx, r.UserID, r.APIKeyID, r.GroupID, ref)
		require.NoError(t, err)
		require.NotNil(t, found)
		require.Equal(t, r.SessionID, found.SessionID)
	}
	for _, scope := range [][3]int64{{2, 10, 20}, {1, 11, 20}, {1, 10, 21}} {
		found, err := b.FindCloudSession(ctx, scope[0], scope[1], scope[2], "call_tools")
		require.NoError(t, err)
		require.Nil(t, found)
	}
	command := service.OpenAICloudCommand{RequestID: "tool-output", Request: service.OpenAICloudRequest{
		Owner: r.OpenAICloudOwner, RootRequestID: r.RootRequestID,
		Body: json.RawMessage(`{"input":[{"type":"function_call_output","call_id":"call_tools","output":"42"}]}`),
	}}
	require.NoError(t, b.EnqueueCloudCommand(ctx, r.SessionID, command))
	wrongOwner := command
	wrongOwner.Request.Owner.UserID++
	require.Error(t, b.EnqueueCloudCommand(ctx, r.SessionID, wrongOwner))
	messages, err := a.ReadCloudCommands(ctx, r.SessionID, "")
	require.NoError(t, err)
	require.Len(t, messages, 1)
	var received service.OpenAICloudCommand
	require.NoError(t, json.Unmarshal(messages[0].Payload, &received))
	require.Equal(t, command, received)
	for _, delta := range []string{"4", "2"} {
		event := service.OpenAICloudStreamEvent{Event: json.RawMessage(fmt.Sprintf(`{"type":"response.output_text.delta","delta":%q}`, delta))}
		require.NoError(t, a.PublishCloudEvent(ctx, command.RequestID, event))
	}
	messages, err = b.ReadCloudEvents(ctx, command.RequestID, "")
	require.NoError(t, err)
	require.Len(t, messages, 2)
	for i, delta := range []string{"4", "2"} {
		var event service.OpenAICloudStreamEvent
		require.NoError(t, json.Unmarshal(messages[i].Payload, &event))
		require.JSONEq(t, fmt.Sprintf(`{"type":"response.output_text.delta","delta":%q}`, delta), string(event.Event))
	}
	remaining, err := b.ReadCloudEvents(ctx, command.RequestID, messages[0].ID)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, messages[1].ID, remaining[0].ID)

	r.Sequence++
	r.Status = "completed"
	r.Usage = &service.OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}
	firstRoot := r.RootRequestID
	require.NoError(t, a.SaveCloudSession(ctx, r, nil, cloudStoreSettlement(r, false)))
	firstBill, err := b.GetCloudSettlement(ctx, firstRoot)
	require.NoError(t, err)
	require.NotNil(t, firstBill)
	require.Equal(t, r.Usage, firstBill.Usage)

	r.Sequence++
	r.Status = "running"
	r.RootRequestID = "next-user-turn"
	r.Usage = nil
	r.BillingSnapshot = json.RawMessage(`{"request_id":"next-user-turn","owner":1}`)
	require.NoError(t, a.SaveCloudSession(ctx, r, nil, nil))
	preserved, err := b.GetCloudSettlement(ctx, firstRoot)
	require.NoError(t, err)
	require.Equal(t, firstBill, preserved, "starting the next turn must preserve the first bill")
	claimed, err := b.ClaimCloudSettlement(ctx, firstRoot, "worker-b", time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = a.ClaimCloudSettlement(ctx, firstRoot, "worker-a", time.Second)
	require.NoError(t, err)
	require.False(t, claimed)
	require.Error(t, a.AckCloudSettlement(ctx, firstRoot, "worker-a"))
	require.NoError(t, b.AckCloudSettlement(ctx, firstRoot, "worker-b"))
	acked, err := a.CloudSettlementAcknowledged(ctx, firstRoot)
	require.NoError(t, err)
	require.True(t, acked)
	preserved, err = a.GetCloudSettlement(ctx, firstRoot)
	require.NoError(t, err)
	require.Nil(t, preserved)
	current, err := b.LoadCloudSession(ctx, r.SessionID)
	require.NoError(t, err)
	require.Equal(t, r.RootRequestID, current.RootRequestID)
	require.Equal(t, "running", current.Status)
}

func TestCloudStoreRealRedisAdminProbeBillingWorker(t *testing.T) {
	for _, tc := range []struct {
		name     string
		snapshot json.RawMessage
		unknown  bool
		failed   bool
	}{
		{name: "absent snapshot survives Redis round trip"},
		{name: "unexpected snapshot fails", snapshot: json.RawMessage(`{"api_key_id":10}`), failed: true},
		{name: "unknown usage fails", unknown: true, failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := newCloudRealRedisStores(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			r := cloudStoreRecord("admin-probe", 1, 1)
			r.Probe, r.APIKeyID, r.GroupID = true, 0, 0
			r.BillingSnapshot = tc.snapshot
			require.NoError(t, a.CreateCloudSession(ctx, r))
			r.Sequence++
			r.Status = "completed"
			r.Usage = &service.OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}
			if tc.unknown {
				r.Status, r.Usage = "unknown", nil
			}
			require.NoError(t, a.SaveCloudSession(ctx, r, nil, cloudStoreSettlement(r, tc.unknown)))
			persisted, err := b.GetCloudSettlement(ctx, r.RootRequestID)
			require.NoError(t, err)
			require.NotNil(t, persisted)
			if tc.snapshot == nil {
				require.Equal(t, json.RawMessage(`null`), persisted.BillingSnapshot,
					"Redis JSON decoding represents an absent probe snapshot as literal null")
			} else {
				require.JSONEq(t, string(tc.snapshot), string(persisted.BillingSnapshot))
			}
			require.Equal(t, r.Usage, persisted.Usage)

			gateway := &service.OpenAIGatewayService{}
			gateway.StartCloudBilling(b)
			t.Cleanup(gateway.StopCloudBilling)
			err = gateway.WaitCloudProbeSettlement(ctx, r.RootRequestID)
			if tc.failed {
				require.ErrorIs(t, err, service.ErrOpenAICloudProbeSettlementFailed)
			} else {
				require.NoError(t, err)
				require.NoError(t, a.ReleaseCloudSession(ctx, r.SessionID, r.OwnerToken))
				closed, err := b.LoadCloudSession(ctx, r.SessionID)
				require.NoError(t, err)
				require.NotNil(t, closed)
				require.Equal(t, "closed", closed.Status)
			}
			acknowledged, err := a.CloudSettlementAcknowledged(ctx, r.RootRequestID)
			if tc.failed {
				require.ErrorIs(t, err, service.ErrOpenAICloudProbeSettlementFailed)
				require.False(t, acknowledged)
			} else {
				require.NoError(t, err)
				require.True(t, acknowledged, "session release must preserve the settlement receipt")
			}
			pending, err := a.GetCloudSettlement(ctx, r.RootRequestID)
			require.NoError(t, err)
			require.Nil(t, pending, "the billing worker must consume the probe settlement")
			blocked, err := a.CloudAccountBlocked(ctx, r.AccountID)
			require.NoError(t, err)
			require.False(t, blocked, "an admin probe must not create customer billing liability")
		})
	}
}

func TestCloudStoreRealRedisExpiredOwnerRetainsUnknownUsage(t *testing.T) {
	a, b := newCloudRealRedisStores(t)
	ctx := t.Context()
	r := cloudStoreRecord("lost-owner", 1, 1)
	r.Status = "awaiting_tool_outputs"
	require.NoError(t, a.CreateCloudSession(ctx, r))
	require.NoError(t, a.rdb.PExpire(ctx, cloudKey("owner", r.SessionID), time.Millisecond).Err())
	require.NoError(t, a.rdb.ZAdd(ctx, cloudCachePrefix+"leases", redis.Z{Score: float64(time.Now().Add(-time.Second).UnixMilli()), Member: r.SessionID}).Err())
	require.Eventually(t, func() bool {
		return b.rdb.Exists(ctx, cloudKey("owner", r.SessionID)).Val() == 0
	}, time.Second, time.Millisecond)
	expired, err := b.ListExpiredCloudSessions(ctx, 10)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	require.Equal(t, r.SessionID, expired[0].SessionID)
	recovered, claimed, err := b.ClaimCloudSession(ctx, r.SessionID, "reaper-b", service.OpenAICloudOwnerLease)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, "reaper-b", recovered.OwnerToken)
	_, claimed, err = a.ClaimCloudSession(ctx, r.SessionID, "reaper-a", service.OpenAICloudOwnerLease)
	require.NoError(t, err)
	require.False(t, claimed)
	r.Sequence++
	require.Error(t, a.SaveCloudSession(ctx, r, nil, nil), "late original owner must be fenced")
	recovered.Sequence++
	recovered.Status = "unknown"
	recovered.Failure = "owner lost while awaiting tool output"
	require.NoError(t, b.SaveCloudSession(ctx, recovered, nil, cloudStoreSettlement(recovered, true)))
	for _, key := range []string{cloudKey("settlement", r.RootRequestID), cloudKey("account_unsettled", "1")} {
		ttl, err := a.rdb.PTTL(ctx, key).Result()
		require.NoError(t, err)
		require.Equal(t, time.Duration(-1), ttl, "unresolved usage must not expire")
	}
	// Terminal session retention can end without erasing the billing liability.
	require.NoError(t, b.rdb.PExpire(ctx, cloudKey("session", r.SessionID), time.Millisecond).Err())
	require.Eventually(t, func() bool {
		return a.rdb.Exists(ctx, cloudKey("session", r.SessionID)).Val() == 0
	}, time.Second, time.Millisecond)
	settlement, err := a.GetCloudSettlement(ctx, r.RootRequestID)
	require.NoError(t, err)
	require.NotNil(t, settlement)
	require.True(t, settlement.Unknown)
	require.Nil(t, settlement.Usage, "missing usage must not become measured zero")
	blocked, err := a.CloudAccountBlocked(ctx, r.AccountID)
	require.NoError(t, err)
	require.True(t, blocked)
	next := cloudStoreRecord("new-generation", 1, 1)
	next.GrantGeneration = "new-grant"
	require.Error(t, b.CreateCloudSession(ctx, next))
	require.NoError(t, b.CreateCloudSession(ctx, cloudStoreRecord("unrelated-account", 2, 2)))
	claimed, err = a.ClaimCloudSettlement(ctx, r.RootRequestID, "billing-a", time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Error(t, a.AckCloudSettlement(ctx, r.RootRequestID, "billing-a"))
	acked, err := b.CloudSettlementAcknowledged(ctx, r.RootRequestID)
	require.NoError(t, err)
	require.False(t, acked)
	settlements, err := b.ListCloudSettlements(ctx, 10)
	require.NoError(t, err)
	require.Len(t, settlements, 1)
	require.Equal(t, r.RootRequestID, settlements[0].RootRequestID)
}

func TestCloudStoreRealRedisConcurrentCapacityAdmission(t *testing.T) {
	a, b := newCloudRealRedisStores(t)
	ctx := t.Context()
	for i := 0; i < service.OpenAICloudMaxUserSessions-1; i++ {
		require.NoError(t, a.CreateCloudSession(ctx, cloudStoreRecord(fmt.Sprintf("seed-%d", i), 1, 1)))
	}
	type admission struct {
		id  string
		err error
	}
	start := make(chan struct{})
	results := make(chan admission, 2)
	for i, cache := range []*gatewayCache{a, b} {
		go func() {
			<-start
			id := fmt.Sprintf("contender-%d", i)
			results <- admission{id, cache.CreateCloudSession(ctx, cloudStoreRecord(id, 1, 1))}
		}()
	}
	close(start)
	winners := 0
	for i := 0; i < 2; i++ {
		result := <-results
		stored, err := b.LoadCloudSession(ctx, result.id)
		require.NoError(t, err)
		if result.err == nil {
			winners++
			require.NotNil(t, stored)
		} else {
			require.Nil(t, stored, "rejected admission must not leave a partial session")
		}
	}
	require.Equal(t, 1, winners, "the final slot must have exactly one winner across clients")
	count, err := a.rdb.ZCard(ctx, cloudKey("user", "1")).Result()
	require.NoError(t, err)
	require.EqualValues(t, service.OpenAICloudMaxUserSessions, count)
	require.NoError(t, b.CreateCloudSession(ctx, cloudStoreRecord("other-user", 2, 2)))
}
