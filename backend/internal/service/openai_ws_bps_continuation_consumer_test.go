package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Only the first coordinator read misses. The existing shared state store owns
// the committed snapshot; the provider's later restore can publish its real
// effective body through ContinuationReplayBody without another generation.
type bpsWSFirstSnapshotMissCache struct {
	*bpsContinuationIntegrationCache
	kind   string
	missed atomic.Bool
}

func (c *bpsWSFirstSnapshotMissCache) LoadBPSState(ctx context.Context, scope, kind string) ([]byte, uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if kind == c.kind && c.missed.CompareAndSwap(false, true) {
		return nil, 0, nil
	}
	return c.bpsContinuationIntegrationCache.LoadBPSState(ctx, scope, kind)
}

func TestOpenAIWSBPSLateRestoreCarriesEffectiveBodyAndTools(t *testing.T) {
	cache := &bpsContinuationIntegrationCache{}
	key, account := newBPSContinuationIntegrationKey(), excelAccount()
	producer := newBPSContinuationIntegrationService(&httpUpstreamRecorder{resp: bpsCompletionResponse(200, excelBPSRepairWire(t, "late_restore_tool", "codex2api.custom/functions.exec", "text(42);"))}, cache)
	seed := []byte("{\"model\":\"gpt-6-astra\",\"input\":\"late restore original question\",\"tools\":[{\"type\":\"namespace\",\"name\":\"functions\",\"tools\":[{\"type\":\"custom\",\"name\":\"exec\"}]}]}")
	c, recorder := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(seed)), key)
	_, err := producer.Forward(t.Context(), c, account, seed)
	require.NoError(t, err)
	id := gjson.Get(recorder.Body.String(), "id").String()
	call := gjson.Get(recorder.Body.String(), "output.0.call_id").String()
	require.NotEmpty(t, id)
	require.NotEmpty(t, call)
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusTooManyRequests, "{\"error\":{\"code\":\"rate_limit_exceeded\",\"type\":\"rate_limit_error\",\"message\":\"BPS capacity\"}}")}
	consumer := newBPSContinuationIntegrationService(upstream, cache)
	late := &bpsWSFirstSnapshotMissCache{bpsContinuationIntegrationCache: cache, kind: excelBPSContinuationKind(id)}
	consumer.cache = late
	payload := fmt.Sprintf("{\"type\":\"response.create\",\"event_id\":\"restored-event\",\"model\":\"gpt-6-astra\",\"previous_response_id\":%q,\"input\":[{\"type\":\"custom_tool_call_output\",\"call_id\":%q,\"output\":\"late result 42\"}]}", id, call)
	client, done, _ := startBPSContinuationIntegrationSession(t, consumer, account, key, payload)
	defer client.CloseNow()
	select {
	case err = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("late restore did not return the typed failover")
	}
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, http.StatusTooManyRequests, failure.StatusCode)
	require.True(t, failure.RejectedBeforeGeneration)
	require.True(t, late.missed.Load())
	require.Len(t, upstream.requests, 1, "restoration is preparation, not permission to replay generation")
	require.Equal(t, "/basispoints/api/responses", upstream.lastReq.URL.Path)
	require.NotEmpty(t, failure.ContinuationReplayBody)
	require.False(t, gjson.GetBytes(failure.ContinuationReplayBody, "previous_response_id").Exists())
	replay, current := OpenAIWSCurrentTurnRetryPayload(err)
	require.True(t, current, "the first socket turn must consume the attempt's restored-body fact")
	require.Equal(t, "response.create", gjson.GetBytes(replay, "type").String())
	require.Equal(t, "restored-event", gjson.GetBytes(replay, "event_id").String())
	require.False(t, gjson.GetBytes(replay, "previous_response_id").Exists())
	require.JSONEq(t, gjson.GetBytes(seed, "tools").Raw, gjson.GetBytes(replay, "tools").Raw)
	for _, value := range []string{"late restore original question", call, "late result 42"} {
		require.Contains(t, string(replay), value)
	}
}

func TestOpenAIWSReplaySourceWithToolsPreservesExplicitCatalog(t *testing.T) {
	catalog := json.RawMessage("[{\"type\":\"function\",\"name\":\"inspect\",\"parameters\":{\"type\":\"object\",\"properties\":{\"n\":{\"const\":9007199254740993}}}}]")
	for _, tc := range []struct {
		name, payload, tools string
		inherit              bool
	}{
		{"proven_omission", "{\"input\":\"hello\",\"exact\":9007199254740993}", string(catalog), true},
		{"unproven_omission", "{\"input\":\"hello\"}", "", false},
		{"empty_replaces", "{\"input\":\"hello\",\"tools\":[]}", "[]", true},
		{"null_replaces", "{\"input\":\"hello\",\"tools\":null}", "null", true},
		{"legacy_functions", "{\"input\":\"hello\",\"functions\":[]}", "", true},
		{"lite_carrier", "{\"input\":[{\"type\":\"additional_tools\",\"tools\":[]}]}", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := []byte(tc.payload)
			result, tools, err := openAIWSReplaySourceWithTools(payload, catalog, tc.inherit)
			require.NoError(t, err)
			require.Equal(t, tc.payload, string(payload), "caller-owned source must remain unchanged")
			require.Equal(t, tc.tools, string(tools))
			require.Equal(t, tc.tools, gjson.GetBytes(result, "tools").Raw)
			if tc.name == "proven_omission" {
				require.Equal(t, "9007199254740993", gjson.GetBytes(result, "exact").Raw)
				require.Equal(t, "9007199254740993", gjson.GetBytes(result, "tools.0.parameters.properties.n.const").Raw)
			} else {
				require.Equal(t, tc.payload, string(result))
			}
		})
	}
}
