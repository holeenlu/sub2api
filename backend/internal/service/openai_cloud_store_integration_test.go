//go:build unit

package service_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// The actor's unit fixtures accept every state update. These checks run the
// actor against the real Lua-fenced Redis store: miniredis by default, or a
// real Redis (cjson semantics) when SUB2API_CLOUD_TEST_REDIS=host:port is set.
func cloudIntegrationStore(t *testing.T) service.OpenAICloudSessionStore {
	t.Helper()
	var client *redis.Client
	if addr := os.Getenv("SUB2API_CLOUD_TEST_REDIS"); addr != "" {
		client = redis.NewClient(&redis.Options{Addr: addr, DB: 15})
		require.NoError(t, client.FlushDB(context.Background()).Err())
	} else {
		server := miniredis.RunT(t)
		client = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1, ContextTimeoutEnabled: true})
	}
	t.Cleanup(func() { _ = client.Close() })
	store, ok := repository.NewGatewayCache(client).(service.OpenAICloudSessionStore)
	require.True(t, ok, "the gateway cache is the production Cloud session store")
	return store
}

// A billing snapshot with the shapes Lua cjson cannot round-trip exactly: a
// 17-digit float, an empty array and an integer beyond 2^53.
var cloudIntegrationSnapshot = json.RawMessage(`{"version":1,"user_id":1,"api_key_id":2,"account_id":4,"prices":{"gpt-5.1":{"output":7.500000000000001e-05,"input":1e-05}},"inflight":{"request_id":"r","user_id":1,"balance":true,"scopes":[]},"stamp":1234567890123456789}`)

func serveCloudIntegrationWire(ctx context.Context, wire *service.CloudTestWire, scenario string) {
	go func() {
		for {
			msg, err := service.CloudTestNextMessage(ctx, wire)
			if err != nil {
				return
			}
			switch msg.Method {
			case "initialize":
				service.CloudTestSend(wire, map[string]any{"id": msg.ID, "result": map[string]any{"userAgent": "cloud-test"}})
			case "thread/start":
				if scenario == "thread start rejected" {
					service.CloudTestSend(wire, map[string]any{"id": msg.ID, "error": map[string]any{"code": -32602, "message": "unsupported thread configuration"}})
					continue
				}
				service.CloudTestSend(wire, map[string]any{"id": msg.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}})
			case "turn/start":
				service.CloudTestSend(wire, map[string]any{"id": msg.ID, "result": map[string]any{"turn": map[string]any{"id": "turn"}}})
				usage := map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread", "turnId": "turn", "tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 10, "outputTokens": 2, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": 12}}}}
				switch scenario {
				case "late completed", "late interrupted", "late unknown":
					if scenario != "late unknown" {
						service.CloudTestSend(wire, usage)
					}
					status := "completed"
					if scenario == "late interrupted" {
						status = "interrupted"
					}
					service.CloudTestSend(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": status}}})
					late := map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread", "turnId": "turn", "tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 20, "outputTokens": 4, "cachedInputTokens": 0, "totalTokens": 24}}}}
					service.CloudTestSend(wire, late)
					service.CloudTestSend(wire, late)
				case "completed":
					service.CloudTestSend(wire, usage)
					service.CloudTestSend(wire, map[string]any{"method": "item/started", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"id": "answer", "type": "agentMessage"}}})
					service.CloudTestSend(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "answer", "delta": "ok"}})
					service.CloudTestSend(wire, map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"id": "answer", "type": "agentMessage", "text": "ok"}}})
					service.CloudTestSend(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "completed"}}})
				case "interrupted":
					service.CloudTestSend(wire, usage)
					service.CloudTestSend(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "interrupted"}}})
				case "connection lost":
					service.CloudTestSend(wire, usage)
					_ = wire.Close()
					return
				}
			case "thread/archive", "turn/interrupt":
				service.CloudTestSend(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
			}
		}
	}()
}

func TestCloudActorLateUsageReachesRedisJournal(t *testing.T) {
	for _, scenario := range []string{"late completed", "late interrupted", "late unknown"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			store := cloudIntegrationStore(t)
			wire := service.NewCloudTestWire()
			sessions := service.CloudTestSessions(store, wire)
			t.Cleanup(func() { sessions.Close(); _ = wire.Close() })
			serveCloudIntegrationWire(ctx, wire, scenario)
			owner := service.OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "gpt-5.1", SettingsDigest: "settings"}
			_, _ = sessions.Run(ctx, service.OpenAICloudRequest{Owner: owner, Body: json.RawMessage(`{"model":"gpt-5.1","input":"hi"}`), RootRequestID: "late-actor-root", BillingSnapshot: cloudIntegrationSnapshot}, nil)
			var journal *service.OpenAICloudSettlement
			require.Eventually(t, func() bool {
				var err error
				journal, err = store.GetCloudSettlement(ctx, "late-actor-root")
				return err == nil && journal != nil && len(journal.LateUsage) == 1
			}, 5*time.Second, 10*time.Millisecond, "the actual actor must consume post-terminal frames into the real store")
			sessions.Close()
			require.True(t, journal.ReconciliationOnly)
			require.Equal(t, 24, journal.LateUsage[0].Usage.TotalTokens)
			require.Equal(t, string(cloudIntegrationSnapshot), string(journal.BillingSnapshot))
			require.Equal(t, scenario == "late unknown", journal.Unknown)
			if scenario == "late unknown" {
				require.Nil(t, journal.Usage)
			} else {
				require.Equal(t, 12, journal.Usage.TotalTokens, "late facts never overwrite the original settlement")
			}
			var original service.OpenAICloudSettlement
			require.NoError(t, json.Unmarshal(journal.OriginalPayload, &original))
			require.Empty(t, original.LateUsage)
			require.Equal(t, original.Usage, journal.Usage)
			record, err := store.LoadCloudSession(ctx, journal.SessionID)
			require.NoError(t, err)
			require.NotNil(t, record.Result)
			require.Equal(t, original.Usage, record.Result.Usage, "late evidence does not rewrite the public terminal response")
			require.Len(t, record.LateUsage, 1)
		})
	}
}

func TestCloudActorPersistsEveryTerminalStateInTheRealStore(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		status     string
		unknown    bool
		lowerBound bool
		fails      bool
	}{
		{name: "completed", status: "completed"},
		{name: "interrupted", status: "interrupted", lowerBound: true, fails: true},
		{name: "thread start rejected", status: "failed", fails: true},
		{name: "connection lost", status: "unknown", unknown: true, fails: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			store := cloudIntegrationStore(t)
			wire := service.NewCloudTestWire()
			sessions := service.CloudTestSessions(store, wire)
			t.Cleanup(func() { sessions.Close(); _ = wire.Close() })
			serveCloudIntegrationWire(ctx, wire, scenario.name)
			owner := service.OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "gpt-5.1", SettingsDigest: "settings"}
			root := "cloud_root_" + scenario.name
			var events []string
			result, err := sessions.Run(ctx, service.OpenAICloudRequest{Owner: owner, Body: json.RawMessage(`{"model":"gpt-5.1","input":"hi"}`), RootRequestID: root, BillingSnapshot: cloudIntegrationSnapshot}, func(event json.RawMessage) error {
				events = append(events, string(event))
				return nil
			})
			// A terminal failure is delivered either as a Run error (connection
			// lost before any public response) or as a terminal result with an
			// error body; neither may be the actor failing to persist its state.
			if err != nil {
				require.True(t, scenario.fails, "events=%v", events)
				require.NotContains(t, err.Error(), "could not be persisted", "the terminal state must reach Redis; events=%v", events)
			} else {
				require.NotNil(t, result)
				require.True(t, result.Terminal)
				require.Equal(t, scenario.fails, result.ErrorStatus != 0 || scenario.status != "completed", "events=%v", events)
			}
			settlements, err := store.ListCloudSettlements(ctx, 16)
			require.NoError(t, err)
			require.Len(t, settlements, 1, "every terminal state publishes exactly one settlement fact")
			require.Equal(t, root, settlements[0].RootRequestID)
			require.Equal(t, scenario.status, settlements[0].Status)
			require.Equal(t, scenario.unknown, settlements[0].Unknown)
			require.Equal(t, scenario.lowerBound, settlements[0].LowerBound)
			require.JSONEq(t, string(cloudIntegrationSnapshot), string(settlements[0].BillingSnapshot), "frozen billing facts survive storage byte-for-byte in meaning")
			require.Equal(t, string(cloudIntegrationSnapshot), string(settlements[0].BillingSnapshot), "frozen billing facts survive storage byte-for-byte")
			record, err := store.LoadCloudSession(ctx, settlements[0].SessionID)
			require.NoError(t, err)
			require.NotNil(t, record)
			require.Equal(t, scenario.status, record.Status)
			require.Equal(t, string(cloudIntegrationSnapshot), string(record.BillingSnapshot), "the stored record keeps the exact Go bytes; Lua must not re-encode it")
		})
	}
}
