package basispoints

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStreamTerminalTailBeforeReadError(t *testing.T) {
	for _, kind := range []string{"response.completed", "response.failed", "response.incomplete", "response.cancelled", "error"} {
		for _, ending := range []string{"", "\n", "\r\n", "\n\n"} {
			t.Run(kind+"/"+strings.ReplaceAll(ending, "\n", "LF"), func(t *testing.T) {
				_, bridge := mustPrepare(t, testSource(), "", nil)
				payload := object{"type": kind, "response": object{"id": "resp_tail", "output": []any{}, "usage": object{"input_tokens": 7}}}
				if kind == "error" {
					payload["error"] = object{"code": "invalid_value"}
				}
				raw, err := json.Marshal(payload)
				require.NoError(t, err)
				reader := io.MultiReader(strings.NewReader("data: "+string(raw)+ending), brokenStream{io.ErrUnexpectedEOF})
				body := bridge.Stream(io.NopCloser(reader))
				defer body.Close()
				out, err := io.ReadAll(body)
				require.NoError(t, err, "a complete terminal already received must win over a late transport error")
				require.Contains(t, string(out), "event: "+kind+"\n")
				if kind != "error" {
					require.Equal(t, json.Number("7"), ObservedStreamUsage(body)["input_tokens"])
				}
			})
		}
	}
}

func TestStreamTruncatedTailKeepsTransportFailure(t *testing.T) {
	for _, tail := range []string{"data: {\"type\":\"response.completed\",\"response\":", "data: [DONE]\n", "data: {\"type\":\"response.in_progress\",\"response\":{\"usage\":{\"input_tokens\":7}}}\n"} {
		_, bridge := mustPrepare(t, testSource(), "", nil)
		body := bridge.Stream(io.NopCloser(io.MultiReader(strings.NewReader(tail), brokenStream{io.ErrUnexpectedEOF})))
		out, err := io.ReadAll(body)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.NotContains(t, string(out), "response.completed")
		require.NotContains(t, string(out), "basispoints_protocol_error", "truncated JSON is a transport failure, not an upstream protocol rejection")
		if strings.Contains(tail, "input_tokens") {
			require.Equal(t, json.Number("7"), ObservedStreamUsage(body)["input_tokens"])
		}
		_ = body.Close()
	}
}

func TestTerminalTailAcrossBPSReaders(t *testing.T) {
	for _, name := range []string{"tool correction", "image compaction"} {
		for _, kind := range []string{"response.completed", "response.failed"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				raw, err := json.Marshal(object{"type": kind, "response": object{
					"id": "resp_tail", "status": strings.TrimPrefix(kind, "response."), "output": []any{},
					"usage": object{"input_tokens": 7}, "error": object{"code": "invalid_value"},
				}})
				require.NoError(t, err)
				reader := io.MultiReader(strings.NewReader("data: "+string(raw)), brokenStream{io.ErrUnexpectedEOF})
				var response map[string]any
				if name == "tool correction" {
					response, err = ReadToolRepairResponse(reader)
				} else {
					response, err = ReadImageCompaction(reader, nil)
				}
				if kind == "response.completed" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					require.NotErrorIs(t, err, io.ErrUnexpectedEOF, "keep the observed terminal rejection")
				}
				require.Equal(t, "resp_tail", response["id"])
			})
		}
	}
}

func TestUpstreamFailureClassificationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		event  object
		status int
		code   string
	}{
		{"flat error", object{"type": "error", "code": "rate_limit_exceeded", "message": "PRIVATE"}, 429, "rate_limit_exceeded"},
		{"nested status", object{"type": "error", "error": object{"status": 401, "code": "PRIVATE", "type": "invalid_request_error"}}, 401, "basispoints_upstream_error"},
		{"explicit first", object{"type": "error", "status": 429, "error": object{"status": 400, "code": "invalid_value"}}, 429, "invalid_value"},
		{"invalid success", object{"type": "error", "status": 200, "error": object{"code": "rate_limit_exceeded"}}, 429, "rate_limit_exceeded"},
		{"invalid range", object{"type": "error", "status": 600}, 502, "basispoints_upstream_error"},
		{"invalid fraction", object{"type": "error", "status": 429.5}, 502, "basispoints_upstream_error"},
		{"invalid string", object{"type": "error", "status": "429 PRIVATE"}, 502, "basispoints_upstream_error"},
		{"type only", object{"type": "response.failed", "response": object{"error": object{"type": "rate_limit_error"}}}, 429, "basispoints_upstream_error"},
		{"BPS overloaded", object{"type": "error", "error": object{"code": "server_is_overloaded", "type": "service_unavailable_error"}}, 503, "server_is_overloaded"},
		{"BPS overloaded code only", object{"type": "error", "error": object{"code": "server_is_overloaded"}}, 503, "server_is_overloaded"},
		{"BPS unavailable type only", object{"type": "response.failed", "response": object{"error": object{"type": "service_unavailable_error"}}}, 503, "basispoints_upstream_error"},
		{"explicit rejection overrides overload", object{"type": "error", "status": 403, "error": object{"code": "server_is_overloaded", "type": "service_unavailable_error"}}, 403, "server_is_overloaded"},
		{"overload prose does not classify", object{"type": "error", "error": object{"code": "PRIVATE", "message": "Our servers are currently overloaded. Please try again later."}}, 502, "basispoints_upstream_error"},
		{"no error", object{"type": "response.cancelled"}, 502, "basispoints_upstream_cancelled"},
		{"not failure", object{"type": "response.completed", "error": object{"code": "rate_limit_exceeded"}}, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.event)
			require.NoError(t, err)
			failure := ParseUpstreamFailure(raw)
			if tc.status == 0 {
				require.Nil(t, failure)
				return
			}
			require.NotNil(t, failure)
			require.Equal(t, tc.status, failure.Status)
			require.Equal(t, tc.code, failure.Code)
			require.NotContains(t, failure.Error(), "PRIVATE")
		})
	}
	require.Nil(t, ParseUpstreamFailure([]byte("invalid JSON")))
}

func TestUpstreamFailureStopsReadersWithoutEOF(t *testing.T) {
	for _, mode := range []string{"bridge", "tool correction", "unknown tool correction", "compaction"} {
		t.Run(mode, func(t *testing.T) {
			_, bridge := mustPrepare(t, testSource(), "", nil)
			reader, writer := io.Pipe()
			t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
			wire := sse(object{"type": "response.cancelled", "response": object{
				"id": "resp_cancelled", "status": "cancelled", "output": []any{},
				"error": object{"code": "rate_limit_exceeded", "message": "PRIVATE"},
				"usage": object{"input_tokens": 7},
			}})
			go func() { _, _ = io.WriteString(writer, wire) }()
			done := make(chan error, 1)
			go func() {
				switch mode {
				case "bridge":
					body := bridge.Stream(reader)
					defer func() { _ = body.Close() }()
					raw, err := io.ReadAll(body)
					if err == nil && (!strings.Contains(string(raw), "rate_limit_exceeded") || strings.Contains(string(raw), "PRIVATE")) {
						err = io.ErrUnexpectedEOF
					}
					done <- err
				case "tool correction":
					_, err := ReadToolRepairResponse(reader)
					done <- err
				case "unknown tool correction":
					_, err := readRepairResponse(reader)
					done <- err
				case "compaction":
					_, err := ReadImageCompaction(reader, nil)
					done <- err
				}
			}()
			select {
			case err := <-done:
				if mode == "bridge" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					require.NotContains(t, err.Error(), "PRIVATE")
					if mode != "compaction" {
						var failure *UpstreamFailure
						require.ErrorAs(t, err, &failure)
						require.Equal(t, 429, failure.Status)
					}
				}
			case <-time.After(3 * time.Second):
				t.Fatal("reader waited for EOF after cancellation terminal")
			}
		})
	}
}

func TestFileOwnershipFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name, code, kind, message string
		status                    int
		ownership                 bool
	}{
		{"exact", "", "server_error", "Unknown error while validating file ownership.", 500, true},
		{"server code", "server_error", "server_error", "Unknown error while validating file ownership.", 500, true},
		{"unavailable status", "", "server_error", "Unknown error while validating file ownership.", 503, true},
		{"permission status", "", "server_error", "Unknown error while validating file ownership.", 403, false},
		{"different type", "", "invalid_request_error", "Unknown error while validating file ownership.", 500, false},
		{"different code", "server_is_overloaded", "server_error", "Unknown error while validating file ownership.", 500, false},
		{"substring", "", "server_error", "PRIVATE: Unknown error while validating file ownership.", 500, false},
		{"unrelated failure", "", "server_error", "PRIVATE", 500, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detail := object{"code": tc.code, "type": tc.kind, "message": tc.message}
			raw, err := json.Marshal(object{"error": detail, "status": 429})
			require.NoError(t, err)
			failure := ParseHTTPFailure(tc.status, raw)
			require.NotNil(t, failure)
			require.Equal(t, tc.status, failure.Status, "HTTP status remains authoritative")
			require.Equal(t, tc.ownership, failure.Code == FileOwnershipFailureCode)
			require.NotContains(t, failure.Message, "PRIVATE")
			for _, kind := range []string{"error", "response.failed", "response.cancelled"} {
				payload := object{"type": kind, "status": tc.status, "error": detail}
				if kind != "error" {
					delete(payload, "error")
					payload["response"] = object{"id": "resp_ownership", "error": detail}
				}
				raw, err = json.Marshal(payload)
				require.NoError(t, err)
				observed := ParseUpstreamFailure(raw)
				require.NotNil(t, observed)
				require.Equal(t, tc.ownership, observed.Code == FileOwnershipFailureCode)
				require.Equal(t, tc.status, observed.Status)
				if tc.ownership {
					var decoded object
					require.NoError(t, decode(raw, &decoded))
					normalized := normalizeFailureEvent(kind, decoded)
					normalized["type"] = kind
					raw, err = json.Marshal(normalized)
					require.NoError(t, err)
					again := ParseUpstreamFailure(raw)
					require.Equal(t, observed.Code, again.Code)
					require.Equal(t, observed.Message, again.Message)
					require.Equal(t, observed.Status, again.Status)
				}
			}
		})
	}
	require.Nil(t, ParseHTTPFailure(200, []byte(`{"error":{"type":"server_error"}}`)))
	require.Nil(t, ParseHTTPFailure(500, []byte("not JSON")))
}

func TestUpstreamFailureWithholdsPendingToolsAndNeverRepairs(t *testing.T) {
	source := testSource()
	source["tools"] = []any{object{"type": "function", "name": "shell"}}
	_, bridge := mustPrepare(t, source, "", nil)
	tool := nativeCall(object{"name": "shell", "arguments": object{"cmd": "PRIVATE_TOOL_ARGUMENTS"}})
	wire := sse(object{"type": "response.output_item.done", "item": tool, "output_index": 0}) +
		sse(object{"type": "response.failed", "response": object{
			"status": "failed", "output": []any{tool}, "error": object{"code": "invalid_value", "message": "PRIVATE"},
		}})
	corrections := 0
	body := bridge.StreamWithRepairs(context.Background(), io.NopCloser(strings.NewReader(wire)),
		func(context.Context, object, error) (object, error) { corrections++; return nil, nil },
		func(context.Context) (io.ReadCloser, error) { corrections++; return nil, nil })
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Zero(t, corrections)
	require.NotContains(t, string(raw), "PRIVATE")
	require.NotContains(t, string(raw), "function_call")
	require.Contains(t, string(raw), "invalid_value")
}

func TestUpstreamFailureDiagnosticSnapshotIsSeparateFromClientEvents(t *testing.T) {
	_, bridge := mustPrepare(t, testSource(), "", nil)
	wire := sse(object{"type": "response.failed", "response": object{
		"id": "resp_failed", "output": []any{object{"type": "message", "content": "PRIVATE_OUTPUT"}},
		"error": object{"code": "server_error", "message": "capacity fault Bearer secret", "input": "PRIVATE_INPUT"},
	}})
	body := bridge.Stream(io.NopCloser(strings.NewReader(wire)))
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	failure := ObservedStreamFailure(body)
	require.NotNil(t, failure)
	require.Contains(t, string(failure.Diagnostic), "capacity fault Bearer secret")
	require.NotContains(t, string(raw), "capacity fault")
	require.NotContains(t, string(failure.Diagnostic), "PRIVATE_")
	encoded, err := json.Marshal(failure)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "capacity fault")
}

func TestCompactedCancellationRetainsWindowAndUsage(t *testing.T) {
	wire := sse(object{"type": "response.cancelled", "response": object{
		"id": "resp_cancelled", "status": "cancelled", "output": []any{},
		"usage": object{"input_tokens": 7, "output_tokens": 2},
		"error": object{"code": "rate_limit_exceeded"},
	}})
	window := []any{object{"type": "compaction", "id": "compaction_test"}}
	body := WithCompactedWindow(context.Background(), io.NopCloser(strings.NewReader(wire)), window, object{"input_tokens": 5, "output_tokens": 1})
	defer func() { _ = body.Close() }()
	events := repairEvents(t, body)
	last := events[len(events)-1]
	require.Equal(t, "response.cancelled", last["type"])
	response := mustTestValue[object](t, last["response"])
	require.Equal(t, window, response["output"])
	usage := mustTestValue[object](t, response["usage"])
	require.Equal(t, json.Number("12"), usage["input_tokens"])
	require.Equal(t, json.Number("3"), usage["output_tokens"])
}
