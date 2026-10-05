package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestIsOpenAIWSClientDisconnectError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "io_eof", err: io.EOF, want: true},
		{name: "net_closed", err: net.ErrClosed, want: true},
		{name: "context_canceled", err: context.Canceled, want: true},
		{name: "ws_normal_closure", err: coderws.CloseError{Code: coderws.StatusNormalClosure}, want: true},
		{name: "ws_going_away", err: coderws.CloseError{Code: coderws.StatusGoingAway}, want: true},
		{name: "ws_no_status", err: coderws.CloseError{Code: coderws.StatusNoStatusRcvd}, want: true},
		{name: "ws_abnormal_1006", err: coderws.CloseError{Code: coderws.StatusAbnormalClosure}, want: true},
		{name: "ws_policy_violation", err: coderws.CloseError{Code: coderws.StatusPolicyViolation}, want: false},
		{name: "wrapped_eof_message", err: errors.New("failed to get reader: failed to read frame header: EOF"), want: true},
		{name: "connection_reset_by_peer", err: errors.New("failed to read frame header: read tcp 127.0.0.1:1234->127.0.0.1:5678: read: connection reset by peer"), want: true},
		{name: "windows_connection_reset", err: errors.New("failed to get reader: failed to read frame header: read tcp 127.0.0.1:1234->127.0.0.1:5678: wsarecv: An existing connection was forcibly closed by the remote host."), want: true},
		{name: "broken_pipe", err: errors.New("write tcp 127.0.0.1:1234->127.0.0.1:5678: write: broken pipe"), want: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isOpenAIWSClientDisconnectError(tt.err))
		})
	}
}

func TestIsOpenAIWSIngressPreviousResponseNotFound(t *testing.T) {
	t.Parallel()

	require.False(t, isOpenAIWSIngressPreviousResponseNotFound(nil))
	require.False(t, isOpenAIWSIngressPreviousResponseNotFound(errors.New("plain error")))
	require.False(t, isOpenAIWSIngressPreviousResponseNotFound(
		wrapOpenAIWSIngressTurnError("read_upstream", errors.New("upstream read failed"), false),
	))
	require.False(t, isOpenAIWSIngressPreviousResponseNotFound(
		wrapOpenAIWSIngressTurnError(openAIWSIngressStagePreviousResponseNotFound, errors.New("previous response not found"), true),
	))
	require.True(t, isOpenAIWSIngressPreviousResponseNotFound(
		wrapOpenAIWSIngressTurnError(openAIWSIngressStagePreviousResponseNotFound, errors.New("previous response not found"), false),
	))
}

func TestOpenAIWSIngressPreviousResponseRecoveryEnabled(t *testing.T) {
	t.Parallel()

	var nilService *OpenAIGatewayService
	require.True(t, nilService.openAIWSIngressPreviousResponseRecoveryEnabled(), "nil service should default to enabled")

	svcWithNilCfg := &OpenAIGatewayService{}
	require.True(t, svcWithNilCfg.openAIWSIngressPreviousResponseRecoveryEnabled(), "nil config should default to enabled")

	svc := &OpenAIGatewayService{
		cfg: &config.Config{},
	}
	require.False(t, svc.openAIWSIngressPreviousResponseRecoveryEnabled(), "explicit config default should be false")

	svc.cfg.Gateway.OpenAIWS.IngressPreviousResponseRecoveryEnabled = true
	require.True(t, svc.openAIWSIngressPreviousResponseRecoveryEnabled())
}

func TestDropPreviousResponseIDFromRawPayload(t *testing.T) {
	t.Parallel()

	t.Run("empty_payload", func(t *testing.T) {
		updated, removed, err := dropPreviousResponseIDFromRawPayload(nil)
		require.NoError(t, err)
		require.False(t, removed)
		require.Empty(t, updated)
	})

	t.Run("payload_without_previous_response_id", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","model":"gpt-5.1"}`)
		updated, removed, err := dropPreviousResponseIDFromRawPayload(payload)
		require.NoError(t, err)
		require.False(t, removed)
		require.Equal(t, string(payload), string(updated))
	})

	t.Run("normal_delete_success", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"resp_abc"}`)
		updated, removed, err := dropPreviousResponseIDFromRawPayload(payload)
		require.NoError(t, err)
		require.True(t, removed)
		require.False(t, gjson.GetBytes(updated, "previous_response_id").Exists())
	})

	t.Run("duplicate_keys_are_removed", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","previous_response_id":"resp_a","input":[],"previous_response_id":"resp_b"}`)
		updated, removed, err := dropPreviousResponseIDFromRawPayload(payload)
		require.NoError(t, err)
		require.True(t, removed)
		require.False(t, gjson.GetBytes(updated, "previous_response_id").Exists())
	})

	t.Run("nil_delete_fn_uses_default_delete_logic", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"resp_abc"}`)
		updated, removed, err := dropPreviousResponseIDFromRawPayloadWithDeleteFn(payload, nil)
		require.NoError(t, err)
		require.True(t, removed)
		require.False(t, gjson.GetBytes(updated, "previous_response_id").Exists())
	})

	t.Run("delete_error", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"resp_abc"}`)
		updated, removed, err := dropPreviousResponseIDFromRawPayloadWithDeleteFn(payload, func(_ []byte, _ string) ([]byte, error) {
			return nil, errors.New("delete failed")
		})
		require.Error(t, err)
		require.False(t, removed)
		require.Equal(t, string(payload), string(updated))
	})

	t.Run("malformed_json_is_still_best_effort_deleted", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","previous_response_id":"resp_abc"`)
		require.True(t, gjson.GetBytes(payload, "previous_response_id").Exists())

		updated, removed, err := dropPreviousResponseIDFromRawPayload(payload)
		require.NoError(t, err)
		require.True(t, removed)
		require.False(t, gjson.GetBytes(updated, "previous_response_id").Exists())
	})
}

func TestNormalizeOpenAIWSContextWindowBoundary(t *testing.T) {
	t.Parallel()

	t.Run("same_window_keeps_previous_response_id", func(t *testing.T) {
		payload := []byte("{\"type\":\"response.create\",\"previous_response_id\":\"resp_old\",\"client_metadata\":{\"x-codex-window-id\":\"window-a\"}}")
		updated, boundary, err := normalizeOpenAIWSContextWindowBoundary(payload, "window-a")
		require.NoError(t, err)
		require.False(t, boundary.Changed)
		require.False(t, boundary.PreviousResponseIDRemoved)
		require.Equal(t, "window-a", boundary.WindowID)
		require.Equal(t, "resp_old", gjson.GetBytes(updated, "previous_response_id").String())
	})

	t.Run("new_window_drops_previous_response_id", func(t *testing.T) {
		payload := []byte("{\"type\":\"response.create\",\"previous_response_id\":\"resp_old\",\"client_metadata\":{\"x-codex-window-id\":\"window-b\"}}")
		updated, boundary, err := normalizeOpenAIWSContextWindowBoundary(payload, "window-a")
		require.NoError(t, err)
		require.True(t, boundary.Changed)
		require.True(t, boundary.PreviousResponseIDRemoved)
		require.Equal(t, "window-b", boundary.WindowID)
		require.False(t, gjson.GetBytes(updated, "previous_response_id").Exists())
	})

	t.Run("new_window_without_previous_response_id_still_marks_boundary", func(t *testing.T) {
		payload := []byte("{\"type\":\"response.create\",\"client_metadata\":{\"x-codex-window-id\":\"window-b\"}}")
		updated, boundary, err := normalizeOpenAIWSContextWindowBoundary(payload, "window-a")
		require.NoError(t, err)
		require.True(t, boundary.Changed)
		require.False(t, boundary.PreviousResponseIDRemoved)
		require.Equal(t, string(payload), string(updated))
	})

	t.Run("embedded_turn_metadata_is_fallback", func(t *testing.T) {
		payload := []byte("{\"type\":\"response.create\",\"previous_response_id\":\"resp_old\",\"client_metadata\":{\"x-codex-turn-metadata\":\"{\\\"window_id\\\":\\\"window-b\\\"}\"}}")
		updated, boundary, err := normalizeOpenAIWSContextWindowBoundary(payload, "window-a")
		require.NoError(t, err)
		require.True(t, boundary.Changed)
		require.True(t, boundary.PreviousResponseIDRemoved)
		require.Equal(t, "window-b", boundary.WindowID)
		require.False(t, gjson.GetBytes(updated, "previous_response_id").Exists())
	})
}

func TestStripCodexSparkImageGenerationToolFromRawPayload(t *testing.T) {
	t.Run("strips_image_generation_for_spark", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","model":"gpt-5.3-codex-spark","tools":[{"type":"function","name":"shell"},{"type":"image_generation","output_format":"png"}]}`)
		updated, changed, err := stripCodexSparkImageGenerationToolFromRawPayload(payload, "gpt-5.3-codex-spark")
		require.NoError(t, err)
		require.True(t, changed)
		require.False(t, gjson.GetBytes(updated, `tools.#(type=="image_generation")`).Exists())
		require.True(t, gjson.GetBytes(updated, `tools.#(type=="function")`).Exists())
	})

	t.Run("strips_namespace_tools_for_spark", func(t *testing.T) {
		payload := []byte(`{
			"type":"response.create",
			"model":"gpt-5.3-codex-spark",
			"input":[
				{"type":"message","role":"user","content":"hello"},
				{"type":"additional_tools","tools":[{"type":"namespace","name":"image_gen"}]}
			],
			"tool_choice":{"type":"namespace","name":"image_gen"}
		}`)
		updated, changed, err := stripCodexSparkImageGenerationToolFromRawPayload(payload, "gpt-5.3-codex-spark")
		require.NoError(t, err)
		require.True(t, changed)
		require.False(t, IsImageGenerationIntent(openAIResponsesEndpoint, "gpt-5.3-codex-spark", updated))
		require.Equal(t, "hello", gjson.GetBytes(updated, "input.0.content").String())
		require.False(t, gjson.GetBytes(updated, "tool_choice").Exists())
	})

	t.Run("keeps_image_generation_for_non_spark", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","model":"gpt-5.3-codex","tools":[{"type":"image_generation","output_format":"png"}]}`)
		updated, changed, err := stripCodexSparkImageGenerationToolFromRawPayload(payload, "gpt-5.3-codex")
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, string(payload), string(updated))
	})

	t.Run("noop_when_no_image_tool", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","model":"gpt-5.3-codex-spark","tools":[{"type":"function","name":"shell"}]}`)
		updated, changed, err := stripCodexSparkImageGenerationToolFromRawPayload(payload, "gpt-5.3-codex-spark")
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, string(payload), string(updated))
	})
}

func TestStripOpenAIImageGenerationToolsFromRawPayload(t *testing.T) {
	t.Run("flat image tool", func(t *testing.T) {
		payload := []byte(`{
			"type":"response.create",
			"model":"gpt-5.4",
			"tools":[
				{"type":"function","name":"shell"},
				{"type":"image_generation","output_format":"png"}
			],
			"tool_choice":{"type":"image_generation"}
		}`)

		updated, changed, err := stripOpenAIImageGenerationToolsFromRawPayload(payload)

		require.NoError(t, err)
		require.True(t, changed)
		require.False(t, gjson.GetBytes(updated, `tools.#(type=="image_generation")`).Exists())
		require.True(t, gjson.GetBytes(updated, `tools.#(type=="function")`).Exists())
		require.False(t, gjson.GetBytes(updated, "tool_choice").Exists())
	})

	t.Run("namespace and Responses Lite tools", func(t *testing.T) {
		payload := []byte(`{
			"type":"response.create",
			"model":"gpt-5.5",
			"tools":[
				{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]},
				{"type":"namespace","name":"code_tools","tools":[{"type":"function","name":"run"}]}
			],
			"input":[
				{"type":"message","role":"user","content":"hello"},
				{"type":"additional_tools","tools":[{"type":"namespace","name":"image_gen"}]}
			],
			"tool_choice":{"type":"namespace","name":"image_gen"}
		}`)

		updated, changed, err := stripOpenAIImageGenerationToolsFromRawPayload(payload)

		require.NoError(t, err)
		require.True(t, changed)
		require.False(t, IsImageGenerationIntent(openAIResponsesEndpoint, "gpt-5.5", updated))
		require.True(t, gjson.GetBytes(updated, `tools.#(name=="code_tools")`).Exists())
		require.Equal(t, "hello", gjson.GetBytes(updated, "input.0.content").String())
		require.False(t, gjson.GetBytes(updated, "tool_choice").Exists())
	})

	t.Run("non-image namespace is unchanged", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","model":"gpt-5.5","tools":[{"type":"namespace","name":"code_tools"}]}`)

		updated, changed, err := stripOpenAIImageGenerationToolsFromRawPayload(payload)

		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, payload, updated)
	})
}

func TestAlignStoreDisabledPreviousResponseID(t *testing.T) {
	t.Parallel()

	t.Run("empty_payload", func(t *testing.T) {
		updated, changed, err := alignStoreDisabledPreviousResponseID(nil, "resp_target")
		require.NoError(t, err)
		require.False(t, changed)
		require.Empty(t, updated)
	})

	t.Run("empty_expected", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","previous_response_id":"resp_old"}`)
		updated, changed, err := alignStoreDisabledPreviousResponseID(payload, "")
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, string(payload), string(updated))
	})

	t.Run("missing_previous_response_id", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","model":"gpt-5.1"}`)
		updated, changed, err := alignStoreDisabledPreviousResponseID(payload, "resp_target")
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, string(payload), string(updated))
	})

	t.Run("already_aligned", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","previous_response_id":"resp_target"}`)
		updated, changed, err := alignStoreDisabledPreviousResponseID(payload, "resp_target")
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, "resp_target", gjson.GetBytes(updated, "previous_response_id").String())
	})

	t.Run("mismatch_rewrites_to_expected", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","previous_response_id":"resp_old","input":[]}`)
		updated, changed, err := alignStoreDisabledPreviousResponseID(payload, "resp_target")
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, "resp_target", gjson.GetBytes(updated, "previous_response_id").String())
	})

	t.Run("duplicate_keys_rewrites_to_single_expected", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","previous_response_id":"resp_old_1","input":[],"previous_response_id":"resp_old_2"}`)
		updated, changed, err := alignStoreDisabledPreviousResponseID(payload, "resp_target")
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, "resp_target", gjson.GetBytes(updated, "previous_response_id").String())
	})
}

func TestSetPreviousResponseIDToRawPayload(t *testing.T) {
	t.Parallel()

	t.Run("empty_payload", func(t *testing.T) {
		updated, err := setPreviousResponseIDToRawPayload(nil, "resp_target")
		require.NoError(t, err)
		require.Empty(t, updated)
	})

	t.Run("empty_previous_response_id", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","model":"gpt-5.1"}`)
		updated, err := setPreviousResponseIDToRawPayload(payload, "")
		require.NoError(t, err)
		require.Equal(t, string(payload), string(updated))
	})

	t.Run("set_previous_response_id_when_missing", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","model":"gpt-5.1"}`)
		updated, err := setPreviousResponseIDToRawPayload(payload, "resp_target")
		require.NoError(t, err)
		require.Equal(t, "resp_target", gjson.GetBytes(updated, "previous_response_id").String())
		require.Equal(t, "gpt-5.1", gjson.GetBytes(updated, "model").String())
	})

	t.Run("overwrite_existing_previous_response_id", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"resp_old"}`)
		updated, err := setPreviousResponseIDToRawPayload(payload, "resp_new")
		require.NoError(t, err)
		require.Equal(t, "resp_new", gjson.GetBytes(updated, "previous_response_id").String())
	})
}

func TestShouldInferIngressFunctionCallOutputPreviousResponseID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                    string
		storeDisabled           bool
		turn                    int
		signals                 ToolContinuationSignals
		currentPreviousResponse string
		expectedPrevious        string
		want                    bool
	}{
		{
			name:             "infer_when_all_conditions_match",
			storeDisabled:    true,
			turn:             2,
			signals:          ToolContinuationSignals{HasFunctionCallOutput: true},
			expectedPrevious: "resp_1",
			want:             true,
		},
		{
			name:             "skip_when_store_enabled",
			storeDisabled:    false,
			turn:             2,
			signals:          ToolContinuationSignals{HasFunctionCallOutput: true},
			expectedPrevious: "resp_1",
			want:             false,
		},
		{
			name:             "skip_on_first_turn",
			storeDisabled:    true,
			turn:             1,
			signals:          ToolContinuationSignals{HasFunctionCallOutput: true},
			expectedPrevious: "resp_1",
			want:             false,
		},
		{
			name:             "skip_without_function_call_output",
			storeDisabled:    true,
			turn:             2,
			signals:          ToolContinuationSignals{},
			expectedPrevious: "resp_1",
			want:             false,
		},
		{
			name:                    "skip_when_request_already_has_previous_response_id",
			storeDisabled:           true,
			turn:                    2,
			signals:                 ToolContinuationSignals{HasFunctionCallOutput: true},
			currentPreviousResponse: "resp_client",
			expectedPrevious:        "resp_1",
			want:                    false,
		},
		{
			name:             "skip_when_last_turn_response_id_missing",
			storeDisabled:    true,
			turn:             2,
			signals:          ToolContinuationSignals{HasFunctionCallOutput: true},
			expectedPrevious: "",
			want:             false,
		},
		{
			name:             "trim_whitespace_before_judgement",
			storeDisabled:    true,
			turn:             2,
			signals:          ToolContinuationSignals{HasFunctionCallOutput: true},
			expectedPrevious: "   resp_2   ",
			want:             true,
		},
		{
			name:             "skip_when_tool_call_context_already_present",
			storeDisabled:    true,
			turn:             2,
			signals:          ToolContinuationSignals{HasFunctionCallOutput: true, HasToolCallContext: true},
			expectedPrevious: "resp_2",
			want:             false,
		},
		{
			name:             "infer_when_only_item_reference_covers_call_ids",
			storeDisabled:    true,
			turn:             2,
			signals:          ToolContinuationSignals{HasFunctionCallOutput: true, HasItemReferenceForAllCallIDs: true},
			expectedPrevious: "resp_2",
			want:             true,
		},
		{
			name:             "skip_when_function_call_output_missing_call_id",
			storeDisabled:    true,
			turn:             2,
			signals:          ToolContinuationSignals{HasFunctionCallOutput: true, HasFunctionCallOutputMissingCallID: true},
			expectedPrevious: "resp_2",
			want:             false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := shouldInferIngressFunctionCallOutputPreviousResponseID(
				tt.storeDisabled,
				tt.turn,
				tt.signals,
				tt.currentPreviousResponse,
				tt.expectedPrevious,
			)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestOpenAIWSInputIsPrefixExtended(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		previous  []byte
		current   []byte
		want      bool
		expectErr bool
	}{
		{
			name:     "both_missing_input",
			previous: []byte(`{"type":"response.create","model":"gpt-5.1"}`),
			current:  []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"resp_1"}`),
			want:     true,
		},
		{
			name:     "previous_missing_current_empty_array",
			previous: []byte(`{"type":"response.create","model":"gpt-5.1"}`),
			current:  []byte(`{"type":"response.create","model":"gpt-5.1","input":[]}`),
			want:     true,
		},
		{
			name:     "previous_missing_current_non_empty_array",
			previous: []byte(`{"type":"response.create","model":"gpt-5.1"}`),
			current:  []byte(`{"type":"response.create","model":"gpt-5.1","input":[{"type":"input_text","text":"hello"}]}`),
			want:     false,
		},
		{
			name:     "array_prefix_match",
			previous: []byte(`{"input":[{"type":"input_text","text":"hello"}]}`),
			current:  []byte(`{"input":[{"text":"hello","type":"input_text"},{"type":"input_text","text":"world"}]}`),
			want:     true,
		},
		{
			name:     "array_prefix_mismatch",
			previous: []byte(`{"input":[{"type":"input_text","text":"hello"}]}`),
			current:  []byte(`{"input":[{"type":"input_text","text":"different"}]}`),
			want:     false,
		},
		{
			name:     "current_shorter_than_previous",
			previous: []byte(`{"input":[{"type":"input_text","text":"a"},{"type":"input_text","text":"b"}]}`),
			current:  []byte(`{"input":[{"type":"input_text","text":"a"}]}`),
			want:     false,
		},
		{
			name:     "previous_has_input_current_missing",
			previous: []byte(`{"input":[{"type":"input_text","text":"a"}]}`),
			current:  []byte(`{"model":"gpt-5.1"}`),
			want:     false,
		},
		{
			name:     "input_string_treated_as_single_item",
			previous: []byte(`{"input":"hello"}`),
			current:  []byte(`{"input":"hello"}`),
			want:     true,
		},
		{
			name:      "current_invalid_input_json",
			previous:  []byte(`{"input":[]}`),
			current:   []byte(`{"input":[}`),
			expectErr: true,
		},
		{
			name:      "invalid_input_json",
			previous:  []byte(`{"input":[}`),
			current:   []byte(`{"input":[]}`),
			expectErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := openAIWSInputIsPrefixExtended(tt.previous, tt.current)
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeOpenAIWSJSONForCompare(t *testing.T) {
	t.Parallel()

	normalized, err := normalizeOpenAIWSJSONForCompare([]byte(`{"b":2,"a":1}`))
	require.NoError(t, err)
	require.Equal(t, `{"a":1,"b":2}`, string(normalized))

	_, err = normalizeOpenAIWSJSONForCompare([]byte("   "))
	require.Error(t, err)

	_, err = normalizeOpenAIWSJSONForCompare([]byte(`{"a":`))
	require.Error(t, err)
}

func TestNormalizeOpenAIWSJSONForCompareOrRaw(t *testing.T) {
	t.Parallel()

	require.Equal(t, `{"a":1,"b":2}`, string(normalizeOpenAIWSJSONForCompareOrRaw([]byte(`{"b":2,"a":1}`))))
	require.Equal(t, `{"a":`, string(normalizeOpenAIWSJSONForCompareOrRaw([]byte(`{"a":`))))
}

func TestNormalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(t *testing.T) {
	t.Parallel()

	normalized, err := normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(
		[]byte(`{"model":"gpt-5.1","input":[1],"previous_response_id":"resp_x","client_metadata":{"request_start_ms":"1"},"stream_options":{"include_usage":true},"generate":false,"metadata":{"b":2,"a":1}}`),
	)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(normalized, "input").Exists())
	require.False(t, gjson.GetBytes(normalized, "previous_response_id").Exists())
	require.False(t, gjson.GetBytes(normalized, "client_metadata").Exists())
	require.False(t, gjson.GetBytes(normalized, "stream_options").Exists())
	require.False(t, gjson.GetBytes(normalized, "generate").Exists())
	require.Equal(t, float64(1), gjson.GetBytes(normalized, "metadata.a").Float())

	normalized, err = normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(
		[]byte(`{"model":"gpt-5.1","generate":true}`),
	)
	require.NoError(t, err)
	require.True(t, gjson.GetBytes(normalized, "generate").Bool())

	_, err = normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(nil)
	require.Error(t, err)

	_, err = normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID([]byte(`[]`))
	require.Error(t, err)
}

func TestOpenAIWSExtractNormalizedInputSequence(t *testing.T) {
	t.Parallel()

	t.Run("empty_payload", func(t *testing.T) {
		items, exists, err := openAIWSExtractNormalizedInputSequence(nil)
		require.NoError(t, err)
		require.False(t, exists)
		require.Nil(t, items)
	})

	t.Run("input_missing", func(t *testing.T) {
		items, exists, err := openAIWSExtractNormalizedInputSequence([]byte(`{"type":"response.create"}`))
		require.NoError(t, err)
		require.False(t, exists)
		require.Nil(t, items)
	})

	t.Run("input_array", func(t *testing.T) {
		items, exists, err := openAIWSExtractNormalizedInputSequence([]byte(`{"input":[{"type":"input_text","text":"hello"}]}`))
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 1)
	})

	t.Run("input_object", func(t *testing.T) {
		items, exists, err := openAIWSExtractNormalizedInputSequence([]byte(`{"input":{"type":"input_text","text":"hello"}}`))
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 1)
	})

	t.Run("input_string", func(t *testing.T) {
		items, exists, err := openAIWSExtractNormalizedInputSequence([]byte(`{"input":"hello"}`))
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 1)
		require.Equal(t, `"hello"`, string(items[0]))
	})

	t.Run("input_number", func(t *testing.T) {
		items, exists, err := openAIWSExtractNormalizedInputSequence([]byte(`{"input":42}`))
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 1)
		require.Equal(t, "42", string(items[0]))
	})

	t.Run("input_bool", func(t *testing.T) {
		items, exists, err := openAIWSExtractNormalizedInputSequence([]byte(`{"input":true}`))
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 1)
		require.Equal(t, "true", string(items[0]))
	})

	t.Run("input_null", func(t *testing.T) {
		items, exists, err := openAIWSExtractNormalizedInputSequence([]byte(`{"input":null}`))
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 1)
		require.Equal(t, "null", string(items[0]))
	})

	t.Run("input_invalid_array_json", func(t *testing.T) {
		items, exists, err := openAIWSExtractNormalizedInputSequence([]byte(`{"input":[}`))
		require.Error(t, err)
		require.True(t, exists)
		require.Nil(t, items)
	})
}

func TestShouldKeepIngressPreviousResponseID(t *testing.T) {
	t.Parallel()

	previousPayload := []byte(`{
		"type":"response.create",
		"model":"gpt-5.1",
		"store":false,
		"tools":[{"type":"function","name":"tool_a"}],
		"input":[{"type":"input_text","text":"hello"}]
	}`)
	currentStrictPayload := []byte(`{
		"type":"response.create",
		"model":"gpt-5.1",
		"store":false,
		"tools":[{"name":"tool_a","type":"function"}],
		"previous_response_id":"resp_turn_1",
		"input":[{"text":"hello","type":"input_text"},{"type":"input_text","text":"world"}]
	}`)

	t.Run("strict_incremental_keep", func(t *testing.T) {
		keep, reason, err := shouldKeepIngressPreviousResponseID(previousPayload, currentStrictPayload, "resp_turn_1", false)
		require.NoError(t, err)
		require.True(t, keep)
		require.Equal(t, "strict_incremental_ok", reason)
	})

	t.Run("codex_prewarm_to_business_keep", func(t *testing.T) {
		prewarmPayload := []byte(`{
			"type":"response.create",
			"model":"gpt-5.1",
			"store":false,
			"generate":false,
			"client_metadata":{"x-codex-ws-stream-request-start-ms":"100"},
			"stream_options":{"include_usage":true},
			"input":[{"type":"input_text","text":"hello"}]
		}`)
		businessPayload := []byte(`{
			"type":"response.create",
			"model":"gpt-5.1",
			"store":false,
			"client_metadata":{"x-codex-ws-stream-request-start-ms":"200"},
			"previous_response_id":"resp_prewarm",
			"input":[{"type":"input_text","text":"hello"}]
		}`)

		keep, reason, err := shouldKeepIngressPreviousResponseID(
			prewarmPayload,
			businessPayload,
			"resp_prewarm",
			false,
		)
		require.NoError(t, err)
		require.True(t, keep)
		require.Equal(t, "strict_incremental_ok", reason)
	})

	t.Run("missing_previous_response_id", func(t *testing.T) {
		payload := []byte(`{"type":"response.create","model":"gpt-5.1","input":[]}`)
		keep, reason, err := shouldKeepIngressPreviousResponseID(previousPayload, payload, "resp_turn_1", false)
		require.NoError(t, err)
		require.False(t, keep)
		require.Equal(t, "missing_previous_response_id", reason)
	})

	t.Run("missing_last_turn_response_id", func(t *testing.T) {
		keep, reason, err := shouldKeepIngressPreviousResponseID(previousPayload, currentStrictPayload, "", false)
		require.NoError(t, err)
		require.False(t, keep)
		require.Equal(t, "missing_last_turn_response_id", reason)
	})

	t.Run("previous_response_id_mismatch", func(t *testing.T) {
		keep, reason, err := shouldKeepIngressPreviousResponseID(previousPayload, currentStrictPayload, "resp_turn_other", false)
		require.NoError(t, err)
		require.False(t, keep)
		require.Equal(t, "previous_response_id_mismatch", reason)
	})

	t.Run("missing_previous_turn_payload", func(t *testing.T) {
		keep, reason, err := shouldKeepIngressPreviousResponseID(nil, currentStrictPayload, "resp_turn_1", false)
		require.NoError(t, err)
		require.False(t, keep)
		require.Equal(t, "missing_previous_turn_payload", reason)
	})

	t.Run("non_input_changed", func(t *testing.T) {
		payload := []byte(`{
			"type":"response.create",
			"model":"gpt-5.1-mini",
			"store":false,
			"tools":[{"type":"function","name":"tool_a"}],
			"previous_response_id":"resp_turn_1",
			"input":[{"type":"input_text","text":"hello"},{"type":"input_text","text":"world"}]
		}`)
		keep, reason, err := shouldKeepIngressPreviousResponseID(previousPayload, payload, "resp_turn_1", false)
		require.NoError(t, err)
		require.False(t, keep)
		require.Equal(t, "non_input_changed", reason)
	})

	t.Run("delta_input_keeps_previous_response_id", func(t *testing.T) {
		payload := []byte(`{
			"type":"response.create",
			"model":"gpt-5.1",
			"store":false,
			"tools":[{"type":"function","name":"tool_a"}],
			"previous_response_id":"resp_turn_1",
			"input":[{"type":"input_text","text":"different"}]
		}`)
		keep, reason, err := shouldKeepIngressPreviousResponseID(previousPayload, payload, "resp_turn_1", false)
		require.NoError(t, err)
		require.True(t, keep)
		require.Equal(t, "strict_incremental_ok", reason)
	})

	t.Run("function_call_output_keeps_previous_response_id", func(t *testing.T) {
		payload := []byte(`{
			"type":"response.create",
			"model":"gpt-5.1",
			"store":false,
			"previous_response_id":"resp_external",
			"input":[{"type":"function_call_output","call_id":"call_1","output":"ok"}]
		}`)
		keep, reason, err := shouldKeepIngressPreviousResponseID(previousPayload, payload, "resp_turn_1", true)
		require.NoError(t, err)
		require.True(t, keep)
		require.Equal(t, "has_function_call_output", reason)
	})

	t.Run("non_input_compare_error", func(t *testing.T) {
		keep, reason, err := shouldKeepIngressPreviousResponseID([]byte(`[]`), currentStrictPayload, "resp_turn_1", false)
		require.Error(t, err)
		require.False(t, keep)
		require.Equal(t, "non_input_compare_error", reason)
	})

	t.Run("current_payload_compare_error", func(t *testing.T) {
		keep, reason, err := shouldKeepIngressPreviousResponseID(previousPayload, []byte(`{"previous_response_id":"resp_turn_1","input":[}`), "resp_turn_1", false)
		require.Error(t, err)
		require.False(t, keep)
		require.Equal(t, "non_input_compare_error", reason)
	})
}

func TestBuildOpenAIWSReplayInputSequence(t *testing.T) {
	t.Parallel()

	lastFull := []json.RawMessage{
		json.RawMessage(`{"type":"input_text","text":"hello"}`),
	}

	t.Run("no_previous_response_id_use_current", func(t *testing.T) {
		items, exists, err := buildOpenAIWSReplayInputSequence(
			lastFull,
			true,
			[]byte(`{"input":[{"type":"input_text","text":"new"}]}`),
			false,
		)
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 1)
		require.Equal(t, "new", gjson.GetBytes(items[0], "text").String())
	})

	t.Run("no_previous_response_id_custom_tool_history_does_not_accumulate", func(t *testing.T) {
		previousFull := []json.RawMessage{
			json.RawMessage(`{"type":"input_text","text":"stale"}`),
			json.RawMessage(`{"type":"custom_tool_call","id":"stale_item","call_id":"stale_call","name":"exec","input":"stale"}`),
		}
		currentPayload := []byte(`{"input":[
			{"type":"custom_tool_call","id":"item_1","call_id":"call_1","name":"exec","input":"pwd"},
			{"type":"custom_tool_call_output","call_id":"call_1","output":"/tmp"},
			{"type":"input_text","text":"continue"}
		]}`)

		for range 3 {
			items, exists, err := buildOpenAIWSReplayInputSequence(
				previousFull,
				true,
				currentPayload,
				false,
			)
			require.NoError(t, err)
			require.True(t, exists)
			require.Len(t, items, 3)
			require.Equal(t, "custom_tool_call", gjson.GetBytes(items[0], "type").String())
			require.Equal(t, "call_1", gjson.GetBytes(items[0], "call_id").String())
			require.Equal(t, "custom_tool_call_output", gjson.GetBytes(items[1], "type").String())
			require.Equal(t, "call_1", gjson.GetBytes(items[1], "call_id").String())
			previousFull = append(items, json.RawMessage(`{"type":"custom_tool_call","id":"replayed_item","call_id":"replayed_call","name":"exec","input":"ignored"}`))
		}
	})

	t.Run("previous_response_id_delta_append", func(t *testing.T) {
		items, exists, err := buildOpenAIWSReplayInputSequence(
			lastFull,
			true,
			[]byte(`{"previous_response_id":"resp_1","input":[{"type":"input_text","text":"world"}]}`),
			true,
		)
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 2)
		require.Equal(t, "hello", gjson.GetBytes(items[0], "text").String())
		require.Equal(t, "world", gjson.GetBytes(items[1], "text").String())
	})

	t.Run("previous_response_id_filters_orphan_historical_custom_tool_call", func(t *testing.T) {
		previousFull := []json.RawMessage{
			json.RawMessage(`{"type":"input_text","text":"hello"}`),
			json.RawMessage(`{"type":"custom_tool_call","id":"item_orphan","call_id":"call_orphan","name":"exec","input":"pwd"}`),
		}
		items, exists, err := buildOpenAIWSReplayInputSequence(
			previousFull,
			true,
			[]byte(`{"previous_response_id":"resp_1","input":[{"role":"user","content":"continue"}]}`),
			true,
		)
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 2)
		require.Equal(t, "hello", gjson.GetBytes(items[0], "text").String())
		require.Equal(t, "user", gjson.GetBytes(items[1], "role").String())
	})

	t.Run("previous_response_id_preserves_paired_historical_function_call", func(t *testing.T) {
		previousFull := []json.RawMessage{
			json.RawMessage(`{"type":"function_call","id":"item_1","call_id":"call_1","name":"lookup","arguments":"{}"}`),
			json.RawMessage(`{"type":"function_call_output","call_id":"call_1","output":"ok"}`),
		}
		items, exists, err := buildOpenAIWSReplayInputSequence(
			previousFull,
			true,
			[]byte(`{"previous_response_id":"resp_1","input":[{"role":"user","content":"continue"}]}`),
			true,
		)
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 3)
		require.Equal(t, "function_call", gjson.GetBytes(items[0], "type").String())
		require.Equal(t, "function_call_output", gjson.GetBytes(items[1], "type").String())
	})

	t.Run("previous_response_id_preserves_paired_historical_custom_tool_call", func(t *testing.T) {
		previousFull := []json.RawMessage{
			json.RawMessage(`{"type":"custom_tool_call","id":"item_1","call_id":"call_1","name":"exec","input":"pwd"}`),
			json.RawMessage(`{"type":"custom_tool_call_output","call_id":"call_1","output":"/tmp"}`),
		}
		items, exists, err := buildOpenAIWSReplayInputSequence(
			previousFull,
			true,
			[]byte(`{"previous_response_id":"resp_1","input":[{"role":"user","content":"continue"}]}`),
			true,
		)
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 3)
		require.Equal(t, "custom_tool_call", gjson.GetBytes(items[0], "type").String())
		require.Equal(t, "custom_tool_call_output", gjson.GetBytes(items[1], "type").String())
	})

	t.Run("item_reference_does_not_complete_historical_call", func(t *testing.T) {
		previousFull := []json.RawMessage{
			json.RawMessage(`{"type":"custom_tool_call","id":"item_1","call_id":"call_1","name":"exec","input":"pwd"}`),
		}
		items, exists, err := buildOpenAIWSReplayInputSequence(
			previousFull,
			true,
			[]byte(`{"previous_response_id":"resp_1","input":[{"type":"item_reference","id":"call_1"},{"role":"user","content":"continue"}]}`),
			true,
		)
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 2)
		require.Equal(t, "item_reference", gjson.GetBytes(items[0], "type").String())
		require.Equal(t, "user", gjson.GetBytes(items[1], "role").String())
	})

	t.Run("previous_response_id_preserves_current_orphan_custom_tool_call", func(t *testing.T) {
		items, exists, err := buildOpenAIWSReplayInputSequence(
			lastFull,
			true,
			[]byte(`{"previous_response_id":"resp_1","input":[{"type":"custom_tool_call","id":"item_live","call_id":"call_live","name":"exec","input":"pwd"}]}`),
			true,
		)
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 2)
		require.Equal(t, "custom_tool_call", gjson.GetBytes(items[1], "type").String())
		require.Equal(t, "call_live", gjson.GetBytes(items[1], "call_id").String())
	})

	t.Run("previous_response_id_full_input_replace", func(t *testing.T) {
		items, exists, err := buildOpenAIWSReplayInputSequence(
			lastFull,
			true,
			[]byte(`{"previous_response_id":"resp_1","input":[{"type":"input_text","text":"hello"},{"type":"input_text","text":"world"}]}`),
			true,
		)
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 2)
		require.Equal(t, "hello", gjson.GetBytes(items[0], "text").String())
		require.Equal(t, "world", gjson.GetBytes(items[1], "text").String())
	})
}

func TestOpenAIWSRawPayloadHasToolCallOutput(t *testing.T) {
	t.Parallel()

	for _, typ := range []string{
		"function_call_output",
		"tool_search_output",
		"custom_tool_call_output",
		"mcp_tool_call_output",
	} {
		typ := typ
		t.Run(typ, func(t *testing.T) {
			t.Parallel()
			payload := []byte(`{"input":[{"type":"` + typ + `","call_id":"call_1","output":"ok"}]}`)
			require.True(t, openAIWSRawPayloadHasToolCallOutput(payload))
		})
	}

	t.Run("object_input", func(t *testing.T) {
		t.Parallel()
		payload := []byte(`{"input":{"type":"tool_search_output","call_id":"call_1","output":"ok"}}`)
		require.True(t, openAIWSRawPayloadHasToolCallOutput(payload))
	})

	t.Run("non_tool_output", func(t *testing.T) {
		t.Parallel()
		payload := []byte(`{"input":[{"type":"input_text","text":"hello"}]}`)
		require.False(t, openAIWSRawPayloadHasToolCallOutput(payload))
	})
}

func TestSetOpenAIWSPayloadInputSequence(t *testing.T) {
	t.Parallel()

	t.Run("set_items", func(t *testing.T) {
		original := []byte(`{"type":"response.create","previous_response_id":"resp_1"}`)
		items := []json.RawMessage{
			json.RawMessage(`{"type":"input_text","text":"hello"}`),
			json.RawMessage(`{"type":"input_text","text":"world"}`),
		}
		updated, err := setOpenAIWSPayloadInputSequence(original, items, true)
		require.NoError(t, err)
		require.Equal(t, "hello", gjson.GetBytes(updated, "input.0.text").String())
		require.Equal(t, "world", gjson.GetBytes(updated, "input.1.text").String())
	})

	t.Run("preserve_empty_array_not_null", func(t *testing.T) {
		original := []byte(`{"type":"response.create","previous_response_id":"resp_1"}`)
		updated, err := setOpenAIWSPayloadInputSequence(original, nil, true)
		require.NoError(t, err)
		require.True(t, gjson.GetBytes(updated, "input").IsArray())
		require.Len(t, gjson.GetBytes(updated, "input").Array(), 0)
		require.False(t, gjson.GetBytes(updated, "input").Type == gjson.Null)
	})
}

func TestCombineOpenAIWSReplayItems(t *testing.T) {
	t.Parallel()

	t.Run("empty_delta_returns_history", func(t *testing.T) {
		history := []json.RawMessage{json.RawMessage(`{"a":1}`)}
		require.Nil(t, combineOpenAIWSReplayItems(nil, nil))
		combined := combineOpenAIWSReplayItems(history, nil)
		require.Len(t, combined, 1)
	})

	t.Run("new_header_shares_bodies", func(t *testing.T) {
		history := []json.RawMessage{json.RawMessage(`{"a":1}`)}
		delta := []json.RawMessage{json.RawMessage(`{"b":2}`)}
		combined := combineOpenAIWSReplayItems(history, delta)
		require.Len(t, combined, 2)
		// 头数组必须是新建的：对 combined 追加不影响 history。
		require.NotSame(t, &history[0], &combined[0])
		// 正文共享：不发生字节级深拷贝。
		require.Same(t, &history[0][0], &combined[0][0])
		require.Same(t, &delta[0][0], &combined[1][0])
	})
}

func TestOpenAIWSReplaySequenceSharesBodies(t *testing.T) {
	t.Parallel()

	t.Run("extract_shares_payload_backing_array", func(t *testing.T) {
		payload := []byte(`{"input":[{"type":"input_text","text":"hello"},{"type":"input_text","text":"world"}]}`)
		items, exists, err := openAIWSExtractNormalizedInputSequence(payload)
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 2)
		for _, item := range items {
			start := bytes.Index(payload, []byte(item))
			require.GreaterOrEqual(t, start, 0)
			require.Same(t, &payload[start], &item[0], "extract 应零拷贝共享 payload 底层数组")
		}
	})

	t.Run("build_transfers_current_items_ownership", func(t *testing.T) {
		payload := []byte(`{"input":[{"type":"input_text","text":"hello"}]}`)
		items, exists, err := buildOpenAIWSReplayInputSequence(nil, false, payload, false)
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 1)
		start := bytes.Index(payload, []byte(items[0]))
		require.GreaterOrEqual(t, start, 0)
		require.Same(t, &payload[start], &items[0][0])
	})

	t.Run("build_merge_shares_history_bodies", func(t *testing.T) {
		history := []json.RawMessage{json.RawMessage(`{"type":"input_text","text":"hello"}`)}
		items, exists, err := buildOpenAIWSReplayInputSequence(
			history,
			true,
			[]byte(`{"previous_response_id":"resp_1","input":[{"type":"input_text","text":"world"}]}`),
			true,
		)
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 2)
		require.Same(t, &history[0][0], &items[0][0], "历史正文应共享而非深拷贝")
	})

	t.Run("build_prefix_hit_transfers_current_items", func(t *testing.T) {
		history := []json.RawMessage{json.RawMessage(`{"type":"input_text","text":"hello"}`)}
		payload := []byte(`{"previous_response_id":"resp_1","input":[{"type":"input_text","text":"hello"},{"type":"input_text","text":"world"}]}`)
		items, exists, err := buildOpenAIWSReplayInputSequence(history, true, payload, true)
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, 2)
		start := bytes.Index(payload, []byte(items[1]))
		require.GreaterOrEqual(t, start, 0)
		require.Same(t, &payload[start], &items[1][0], "prefix 命中应转移当前 items 所有权并共享 payload 底层数组")
	})
}

const codexCLIOnlyTestOfficialUA = "codex_cli_rs/0.141.0 (x) (codex_cli_rs; 0.141.0)"

// codexCLIOnlyWSTestSetup builds one codex_cli_only account per ingress path:
// native ctx_pool and passthrough upstream sockets.
type codexCLIOnlyWSTestSetup struct {
	svc     *OpenAIGatewayService
	account *Account
	native  *stagedPassthroughConn
	http    *httpUpstreamRecorder
}

func newCodexCLIOnlyWSTestSetup(t *testing.T, mode string) codexCLIOnlyWSTestSetup {
	t.Helper()

	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	upstream := newStagedPassthroughConn()
	svc := newPassthroughLifecycleService(cfg, upstream)
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &nativeCodexStagedConn{upstream}})
	svc.openaiWSPool = pool
	account := nativeWSTestAccount()
	account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
	account.Extra["openai_oauth_responses_websockets_v2_enabled"] = true
	account.Extra["codex_cli_only"] = true
	return codexCLIOnlyWSTestSetup{svc: svc, account: account, native: upstream, http: svc.httpUpstream.(*httpUpstreamRecorder)}
}

// completeTurn answers one forwarded turn and reads it through to its terminal.
func (s codexCLIOnlyWSTestSetup) completeTurn(t *testing.T, client *coderws.Conn, responseID string) {
	t.Helper()
	if s.native != nil {
		requirePassthroughUpstreamWrite(t, s.native, time.Second)
		s.native.Send(`{"type":"response.completed","response":{"id":"` + responseID + `","model":"gpt-6-astra","usage":{"input_tokens":2,"output_tokens":1}}}`)
	}
	events := readNativeWSTestTurn(t, client)
	require.Equal(t, "response.completed", gjson.GetBytes(events[len(events)-1], "type").String())
}

// requireRejected expects the 403 event the client must see, then the policy
// close, with no request reaching either upstream.
func (s codexCLIOnlyWSTestSetup) requireRejected(t *testing.T, client *coderws.Conn, serverErr <-chan error, upstreamRequests int) {
	t.Helper()
	event, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "error", gjson.GetBytes(event, "type").String())
	require.Equal(t, int64(http.StatusForbidden), gjson.GetBytes(event, "status").Int())
	require.Equal(t, "forbidden_error", gjson.GetBytes(event, "error.type").String())
	require.Equal(t, CodexOfficialClientsOnlyMessage, gjson.GetBytes(event, "error.message").String())
	// net.Pipe writes are synchronous: keep reading so closing frames cannot block.
	go func() {
		for {
			if _, _, err := client.Read(context.Background()); err != nil {
				return
			}
		}
	}()
	select {
	case err := <-serverErr:
		var closeErr *OpenAIWSClientCloseError
		require.ErrorAs(t, err, &closeErr)
		require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
		require.ErrorIs(t, err, ErrOpenAIWSCodexClientRestricted)
	case <-time.After(3 * time.Second):
		t.Fatal("WS ingress did not stop at the codex_cli_only rejection")
	}
	require.Len(t, s.http.requests, upstreamRequests)
	if s.native != nil {
		select {
		case payload := <-s.native.writes:
			t.Fatalf("a rejected turn must not reach the native upstream: %s", payload)
		default:
		}
	}
}

func TestOpenAIWSIngressCodexCLIOnlyRejectsNonCodexClient(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			setup := newCodexCLIOnlyWSTestSetup(t, mode)
			client, serverErr := startOpenAIWSMemorySession(t, setup.svc, setup.account, http.Header{"User-Agent": {"curl/8.0"}},
				`{"type":"response.create","model":"gpt-6-astra","input":"hello"}`, nil)
			defer client.CloseNow()
			setup.requireRejected(t, client, serverErr, 0)
		})
	}
}

func TestOpenAIWSIngressCodexCLIOnlyAllowsOfficialClient(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			setup := newCodexCLIOnlyWSTestSetup(t, mode)
			header := http.Header{"User-Agent": {codexCLIOnlyTestOfficialUA}, "Originator": {"codex_cli_rs"}, "X-Codex-Window-Id": {"window-1"}}
			client, serverErr := startOpenAIWSMemorySession(t, setup.svc, setup.account, header,
				`{"type":"response.create","model":"gpt-6-astra","input":"hello"}`, nil)
			defer client.CloseNow()
			setup.completeTurn(t, client, "resp_official")
			requireNativeWSTestServerExit(t, client, serverErr)
		})
	}
}

// HTTP checks every request, so a WS connection re-checks every turn: the
// handshake fixes the identity headers, but body fingerprint signals vary.
func TestOpenAIWSIngressCodexCLIOnlyRechecksEachTurn(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			setup := newCodexCLIOnlyWSTestSetup(t, mode)
			setup.svc.settingService = NewSettingService(&nativeWSSettingsRepo{values: map[string]string{
				SettingKeyCodexCLIOnlyEngineFingerprintSignals: `[{"type":"body_path","match":["client_metadata.x-codex-installation-id"],"required":true}]`,
			}}, setup.svc.cfg)
			header := http.Header{"User-Agent": {codexCLIOnlyTestOfficialUA}, "Originator": {"codex_cli_rs"}}
			client, serverErr := startOpenAIWSMemorySession(t, setup.svc, setup.account, header,
				`{"type":"response.create","model":"gpt-6-astra","input":"first","client_metadata":{"x-codex-installation-id":"install-1"}}`, nil)
			defer client.CloseNow()
			setup.completeTurn(t, client, "resp_first")
			firstTurnRequests := len(setup.http.requests)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-astra","input":"second"}`)))
			setup.requireRejected(t, client, serverErr, firstTurnRequests)
		})
	}
}

type nativeWSTestTurn struct {
	result *OpenAIForwardResult
	err    error
}

// Exercise a real HTTP upgrade and WS framing over net.Pipe. No TCP ports or
// external services are required, including in restricted test environments.
type nativeWSMemoryListener struct {
	conn     net.Conn
	done     chan struct{}
	once     sync.Once
	accepted bool
}

func (l *nativeWSMemoryListener) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		return l.conn, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *nativeWSMemoryListener) Close() error { l.once.Do(func() { close(l.done) }); return nil }
func (l *nativeWSMemoryListener) Addr() net.Addr {
	return &net.UnixAddr{Name: "native-ws-test", Net: "memory"}
}

func startOpenAIWSMemorySession(t *testing.T, svc *OpenAIGatewayService, account *Account, header http.Header, payload string, hooks *OpenAIWSIngressHooks) (*coderws.Conn, <-chan error) {
	t.Helper()
	clientPipe, serverPipe := net.Pipe()
	listener := &nativeWSMemoryListener{conn: serverPipe, done: make(chan struct{})}
	serverErr := make(chan error, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.CloseNow()
		ctx := context.Background()
		_, first, err := ReadOpenAIWSClientMessage(ctx, conn, 3*time.Second, coderws.StatusPolicyViolation, "missing first request")
		if err != nil {
			serverErr <- err
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r.Clone(ctx)
		serverErr <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, account, "test-token", first, hooks)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = clientPipe.Close(); _ = serverPipe.Close(); _ = server.Close() })
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return clientPipe, nil }}
	t.Cleanup(transport.CloseIdleConnections)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws://native-ws-test/v1/responses", &coderws.DialOptions{HTTPClient: &http.Client{Transport: transport}, HTTPHeader: header})
	require.NoError(t, err)
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(payload)))
	return client, serverErr
}
func readNativeWSTestTurn(t *testing.T, conn *coderws.Conn) []json.RawMessage {
	t.Helper()
	var events []json.RawMessage
	for {
		payload, err := readPassthroughLifecycleFrame(t, conn, 3*time.Second)
		require.NoError(t, err)
		require.True(t, json.Valid(payload), "a WS frame must contain JSON, not SSE lines")
		events = append(events, payload)
		kind := gjson.GetBytes(payload, "type").String()
		if isOpenAIWSTerminalEvent(kind) || kind == "error" {
			return events
		}
	}
}
func requireNativeWSTestServerExit(t *testing.T, conn *coderws.Conn, serverErr <-chan error) {
	t.Helper()
	_ = conn.CloseNow()
	select {
	case err := <-serverErr:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Native WS ingress did not exit after client disconnect")
	}
}
func startNativeWSMemorySession(t *testing.T, svc *OpenAIGatewayService, account *Account, payload string, hooks *OpenAIWSIngressHooks) (*coderws.Conn, <-chan error) {
	t.Helper()
	return startOpenAIWSMemorySession(t, svc, account, nil, payload, hooks)
}

type nativeCodexStagedConn struct{ *stagedPassthroughConn }

func (c *nativeCodexStagedConn) WriteJSON(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.WriteFrame(ctx, coderws.MessageText, payload)
}

func nativeWSTestAccount() *Account {
	return &Account{ID: 300, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 10,
		Credentials: map[string]any{"access_token": "test-token", "chatgpt_account_id": "test-account"}, Extra: map[string]any{"openai_passthrough": true}}
}

type nativeWSSettingsRepo struct {
	SettingRepository
	mu     sync.Mutex
	values map[string]string
	err    error
}

func (r *nativeWSSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		values[key] = r.values[key]
	}
	return values, r.err
}

func (r *nativeWSSettingsRepo) GetAll(_ context.Context) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, r.err
}

func (r *nativeWSSettingsRepo) GetValue(ctx context.Context, key string) (string, error) {
	values, err := r.GetMultiple(ctx, []string{key})
	return values[key], err
}

func (r *nativeWSSettingsRepo) SetMultiple(_ context.Context, values map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.values == nil {
		r.values = make(map[string]string)
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}
