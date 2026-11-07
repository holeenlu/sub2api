package basispoints

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func collectStreamEvents(t *testing.T, bridge *Bridge, wire string, window []any, usage object) (*StreamConversion, []object, error) {
	t.Helper()
	var events []object
	conversion := bridge.NewStreamConversion(func(kind string, raw []byte) error {
		var event object
		require.NoError(t, decode(raw, &event))
		require.Equal(t, kind, event["type"])
		events = append(events, event)
		return nil
	}, RepairPolicy{})
	conversion.SetCompactedWindow(window, usage)
	demand, err := conversion.Convert(strings.NewReader(wire))
	if err == nil {
		require.Nil(t, demand)
		err = conversion.Deliver(bridge.CommitReplays(conversion.Replays()))
	}
	return conversion, events, err
}

func TestStreamConversionDirectEventsPreserveExtensionsAndOwnership(t *testing.T) {
	const extension = `{"large":90071992547409931234567890,"decimal":1.2300,"nested":{"null":null,"value":"data: literal\n\n<value>"}}`
	wire := "event: provider.extension\r\ndata: {\"extension\":" + extension + "}\r\n\r\n" +
		"data: {\"type\":\"response.completed\",\"extension\":" + extension + ",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"extension\":" + extension + ",\"output\":[]}}\n\n"
	var events [][]byte
	conversion := (&Bridge{}).NewStreamConversion(func(kind string, raw []byte) error {
		require.True(t, json.Valid(raw), "semantic output must be JSON, not SSE")
		require.Equal(t, kind, gjson.GetBytes(raw, "type").String())
		events = append(events, raw) // retaining an event must not alias the next one
		return nil
	}, RepairPolicy{})
	demand, err := conversion.Convert(strings.NewReader(wire))
	require.NoError(t, err)
	require.Nil(t, demand)
	require.Len(t, events, 1, "terminal still awaits the replay commit boundary")
	first := bytes.Clone(events[0])
	require.NoError(t, conversion.Deliver(nil))
	require.Len(t, events, 2)
	require.Equal(t, first, events[0])
	require.Equal(t, "provider.extension", gjson.GetBytes(events[0], "type").String())
	for i, raw := range events {
		require.EqualValues(t, i, gjson.GetBytes(raw, "sequence_number").Int())
		require.Equal(t, "90071992547409931234567890", gjson.GetBytes(raw, "extension.large").Raw)
		require.Equal(t, "1.2300", gjson.GetBytes(raw, "extension.decimal").Raw)
		require.Equal(t, "data: literal\n\n<value>", gjson.GetBytes(raw, "extension.nested.value").String())
	}
	require.Equal(t, "90071992547409931234567890", gjson.GetBytes(events[1], "response.extension.large").Raw)
	require.False(t, gjson.GetBytes(events[1], "response.usage").Exists())
	require.Empty(t, conversion.Usage())
}

func TestStreamConversionCompactWindowProjectsToolsWithoutChangingFacts(t *testing.T) {
	_, bridge := repairBridge(t, nil)
	window := []any{object{"type": "compaction", "id": "cmp", "encrypted_content": "opaque"}}
	compactUsage := object{"input_tokens": json.Number("7"), "input_tokens_details": object{"cached_tokens": json.Number("3")}}
	original := repairResponse("original", 10, 2, repairCall("tool", "codex2api.custom/functions.exec", "text(42);"))
	original["extension"] = object{"large": json.Number("90071992547409931234567890")}
	original["usage"].(object)["input_tokens_details"] = object{"cached_tokens": json.Number("1"), "extension": "retained"}
	conversion, events, err := collectStreamEvents(t, bridge, sse(object{"type": "response.completed", "response": original}), window, compactUsage)
	require.NoError(t, err)
	require.Len(t, events, 7)
	for i, event := range events {
		require.Equal(t, strconv.Itoa(i), mustTestValue[json.Number](t, event["sequence_number"]).String())
		if i >= 2 && i < 6 {
			require.Equal(t, json.Number("1"), event["output_index"], "generated tool indices are ints before wire encoding")
		}
	}
	response := mustTestValue[object](t, events[6]["response"])
	require.Len(t, response["output"], 2)
	projected := mustTestValue[object](t, response["usage"])
	require.Equal(t, json.Number("17"), projected["input_tokens"])
	details := mustTestValue[object](t, projected["input_tokens_details"])
	require.Equal(t, json.Number("4"), details["cached_tokens"])
	require.Equal(t, "retained", details["extension"])
	require.Equal(t, json.Number("10"), conversion.Usage()["input_tokens"])
	require.Equal(t, json.Number("1"), conversion.Usage()["input_tokens_details"].(object)["cached_tokens"])
	require.Equal(t, json.Number("3"), compactUsage["input_tokens_details"].(object)["cached_tokens"])
	require.Equal(t, json.Number("90071992547409931234567890"), response["extension"].(object)["large"])
}

func TestStreamConversionCompactWindowPreservesUnknownAndExplicitZeroUsage(t *testing.T) {
	for _, reported := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "explicit_zero"}[reported], func(t *testing.T) {
			response := object{"id": "r", "status": "completed", "output": []any{}}
			if reported {
				response["usage"] = object{"input_tokens": 0}
			}
			conversion, events, err := collectStreamEvents(t, &Bridge{}, sse(object{"type": "response.completed", "response": response}), []any{object{"type": "compaction", "id": "c"}}, nil)
			require.NoError(t, err)
			final := events[len(events)-1]["response"].(object)
			if reported {
				require.Equal(t, json.Number("0"), conversion.Usage()["input_tokens"])
				require.Equal(t, json.Number("0"), final["usage"].(object)["input_tokens"])
				require.NotContains(t, final["usage"], "output_tokens")
			} else {
				require.Empty(t, conversion.Usage())
				require.NotContains(t, final, "usage")
			}
		})
	}
}

func TestStreamConversionDirectEventsBoundProjectedPayload(t *testing.T) {
	delivered := 0
	conversion := (&Bridge{}).NewStreamConversion(func(string, []byte) error {
		delivered++
		return nil
	}, RepairPolicy{})
	conversion.SetCompactedWindow([]any{object{"type": "compaction", "encrypted_content": strings.Repeat("x", MaxSSEEventBytes)}}, nil)
	demand, err := conversion.Convert(strings.NewReader(`data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[],"usage":{"input_tokens":7}}}` + "\n\n"))
	require.NoError(t, err)
	require.Nil(t, demand)
	require.ErrorContains(t, conversion.Deliver(nil), "converted event exceeds")
	require.Zero(t, delivered, "the removed SSE read pump's payload bound must survive direct delivery")
	require.Equal(t, json.Number("7"), conversion.Usage()["input_tokens"], "oversized projection cannot erase observed generation usage")
}

type conversionSafeCause struct{ cause error }

func (e *conversionSafeCause) Error() string { return "safe repair failure" }
func (e *conversionSafeCause) Unwrap() error { return e.cause }

func TestStreamConversionRetainsRepairCauseOutsideWire(t *testing.T) {
	for _, cause := range []error{errors.New("PRIVATE_CAUSE"), context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
			_, bridge := repairBridge(t, nil)
			original := repairResponse("original", 10, 2, repairCall("bad", "Run", "text(42);"))
			upstream := io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": original})))
			stream := driveRepairFixture(t.Context(), bridge, upstream, func(context.Context, object, error) (object, error) { return nil, &conversionSafeCause{cause} }, nil)
			output, err := io.ReadAll(stream)
			if errors.Is(cause, context.Canceled) {
				require.ErrorIs(t, err, cause)
			} else {
				require.NoError(t, err)
			}
			_ = stream.Close()
			observed := ObservedStreamCause(stream)
			require.ErrorIs(t, observed, cause)
			var typed *conversionSafeCause
			require.ErrorAs(t, observed, &typed)
			require.NotContains(t, string(output), "PRIVATE_CAUSE")
			require.NotContains(t, string(output), "response.output_item.added")
			require.Equal(t, json.Number("10"), ObservedStreamUsage(stream)["input_tokens"])
		})
	}
}

type conversionReplayStore struct {
	reads, writes int
	err           error
}

func (s *conversionReplayStore) LoadBPSState(context.Context, string, string) ([]byte, uint64, error) {
	s.reads++
	return nil, 0, s.err
}

func (s *conversionReplayStore) SaveBPSState(context.Context, string, string, *uint64, []byte) (bool, error) {
	s.writes++
	return s.err == nil, s.err
}

func TestStreamConversionYieldsRepairAndReplayFactsWithoutEffects(t *testing.T) {
	for _, persistFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "persistence_failure"}[persistFailure], func(t *testing.T) {
			_, bridge := repairBridge(t, nil)
			store := &conversionReplayStore{}
			if persistFailure {
				store.err = errors.New("unavailable")
			}
			bridge.replay, _ = NewPersistentCaches(t.Context(), store)
			var output bytes.Buffer
			conversion := bridge.NewStreamConversion(writeSSEFixture(&output), RepairPolicy{ToolTransport: true})
			original := repairResponse("original", 10, 2, repairCall("bad", "Run", "text(42);"))
			demand, err := conversion.Convert(strings.NewReader(sse(object{"type": "response.completed", "response": original})))
			require.NoError(t, err)
			require.NotNil(t, demand)
			require.Equal(t, RepairToolTransport, demand.Kind)
			require.NotNil(t, demand.Validation)
			require.Equal(t, "original", demand.FailedResponse["id"])
			require.Empty(t, conversion.Replays())
			require.Zero(t, output.Len())
			require.Zero(t, store.reads+store.writes, "a converter must not access the replay store")

			fixed := repairResponse("fixed", 7, 1, repairCall("fixed", "codex2api.custom/functions.exec", "text(42);"))
			demand, err = conversion.ResolveRepair(fixed, nil)
			require.NoError(t, err)
			require.Nil(t, demand)
			require.Zero(t, output.Len(), "valid tools still wait for the application's replay commit")
			require.Zero(t, store.reads+store.writes)
			facts := conversion.Replays()
			require.Len(t, facts, 1)
			require.Equal(t, "fixed", facts[0].CallID)
			require.Equal(t, "text(42);", facts[0].Client["input"])
			require.Equal(t, json.Number("17"), conversion.Usage()["input_tokens"])
			persistErr := bridge.CommitReplays(facts)
			require.Equal(t, 1, store.writes)
			require.Zero(t, store.reads)
			require.NoError(t, conversion.Deliver(persistErr))
			if persistFailure {
				require.ErrorIs(t, persistErr, ErrStateUnavailable)
				require.Equal(t, 503, conversion.Failure().Status)
				require.ErrorIs(t, conversion.Failure(), ErrStateUnavailable)
				require.True(t, conversion.Failure().RequestScoped())
				require.Contains(t, output.String(), "response.failed")
				require.NotContains(t, output.String(), "response.output_item.added")
			} else {
				require.NoError(t, persistErr)
				require.Contains(t, output.String(), "response.completed")
				require.Contains(t, output.String(), "custom_tool_call")
			}
			before := output.String()
			_, err = conversion.ResolveRepair(fixed, nil)
			require.Error(t, err)
			require.NoError(t, conversion.Deliver(nil))
			require.Equal(t, before, output.String(), "terminal resolution cannot duplicate tool delivery")
		})
	}
}

func TestStreamConversionProviderCannotSpoofLocalStateFailure(t *testing.T) {
	_, bridge := repairBridge(t, nil)
	var output bytes.Buffer
	conversion := bridge.NewStreamConversion(writeSSEFixture(&output), RepairPolicy{})
	demand, err := conversion.Convert(strings.NewReader(sse(object{
		"type": "response.failed", "response": object{
			"id": "provider", "status": "failed", "output": []any{},
			"error": object{"code": "basispoints_state_unavailable", "status": 503, "message": "provider text"},
		},
	})))
	require.NoError(t, err)
	require.Nil(t, demand)
	require.NotNil(t, conversion.Failure())
	require.False(t, conversion.Failure().Local)
	require.False(t, conversion.Failure().RequestScoped())
	require.NotErrorIs(t, conversion.Failure(), ErrStateUnavailable)
}

func TestStreamConversionUnknownUsageRemainsAbsent(t *testing.T) {
	_, bridge := repairBridge(t, nil)
	var output bytes.Buffer
	conversion := bridge.NewStreamConversion(writeSSEFixture(&output), RepairPolicy{ToolTransport: true})
	original := object{"id": "original", "status": "completed", "output": []any{repairCall("bad", "Run", "text(42);")}}
	demand, err := conversion.Convert(strings.NewReader(sse(object{"type": "response.completed", "response": original})))
	require.NoError(t, err)
	require.NotNil(t, demand)
	_, err = conversion.ResolveRepair(nil, context.Canceled)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, conversion.Usage())
	require.Empty(t, conversion.Replays())
	require.NotContains(t, output.String(), "response.output_item.added")
}
