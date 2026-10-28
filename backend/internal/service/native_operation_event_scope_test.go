package service

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/stretchr/testify/require"
)

func TestNativeHTTPReviewUnknownResponsesNamespaceIsNotEvidence(t *testing.T) {
	created := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"real\",\"model\":\"actual-model\"}}\n\n"
	completed := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"real\",\"model\":\"actual-model\",\"usage\":{\"input_tokens\":1}}}\n\n"
	foreign := "\"response_id\":\"foreign\",\"model\":\"foreign-top-model\",\"object\":\"response\",\"status\":\"completed\",\"usage\":{\"output_tokens\":900},\"response\":{\"id\":\"foreign-nested\",\"model\":\"foreign-model\",\"status\":\"completed\",\"usage\":{\"input_tokens\":900}}"
	for _, named := range []bool{false, true} {
		future := "data: {\"type\":\"response.future.telemetry\"," + foreign + "}\n\n"
		if named {
			future = "event: response.future.telemetry\ndata: {" + foreign + "}\n\n"
		}
		for _, position := range []string{"alone", "before_known", "between", "after_terminal"} {
			wire := future
			switch position {
			case "before_known":
				wire = future + created + completed
			case "between":
				wire = created + future + completed
			case "after_terminal":
				wire = created + completed + future
			}
			for _, singleByte := range []bool{false, true} {
				t.Run(fmt.Sprintf("named_%t/%s/one_byte_%t", named, position, singleByte), func(t *testing.T) {
					f, owner, _ := nativeHTTPObservedFixture(t, wire, "text/event-stream")
					if singleByte {
						f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
							return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(iotest.OneByteReader(strings.NewReader(wire)))}, nil
						}
					}
					response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
					require.NoError(t, err)
					actual, err := io.ReadAll(response.Body)
					require.NoError(t, err, "unknown namespaced events must not bind or challenge the operation owner")
					require.Equal(t, wire, string(actual))
					require.NoError(t, response.Body.Close())
					attempt := owner.core.Snapshot().Attempts[0]
					if position == "alone" {
						require.Empty(t, attempt.ResponseID)
						require.Empty(t, attempt.ReturnedModel)
						require.Nil(t, attempt.Usage)
						require.Equal(t, model.TerminalUnknown, attempt.Terminal)
					} else {
						require.Equal(t, "real", attempt.ResponseID)
						require.Equal(t, "actual-model", attempt.ReturnedModel)
						require.EqualValues(t, 1, attempt.Usage.Input.Value)
						require.Nil(t, attempt.Usage.Output)
						require.Equal(t, model.UsageFinal, attempt.Usage.Confidence)
					}
				})
			}
		}
	}
	for _, kind := range []string{"response.future.telemetry", "response.output_text.future", "response.completed.future"} {
		t.Run("ws/"+kind, func(t *testing.T) {
			d := nativeOperationDecoder{sse: true}
			require.Empty(t, d.payload([]byte("{\"type\":\""+kind+"\","+foreign+"}")))
		})
	}
}

func TestNativeHTTPReviewKnownResponsesEventsKeepOwnerChecks(t *testing.T) {
	for _, kind := range []string{
		"response.created", "response.queued", "response.completed", "response.output_text.delta",
		"response.output_item.done", "response.content_part.added", "response.function_call_arguments.delta",
		"response.reasoning_text.delta", "response.image_generation_call.partial_image",
		"response.web_search_call.completed", "response.metadata", "response.interrupt.accepted",
	} {
		t.Run(kind, func(t *testing.T) {
			wire := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"real\"}}\n\n" +
				"data: {\"type\":\"" + kind + "\",\"response_id\":\"foreign\"}\n\n"
			f, owner, _ := nativeHTTPObservedFixture(t, wire, "text/event-stream")
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			_, err = io.ReadAll(response.Body)
			require.ErrorIs(t, err, execution.ErrOperationFacts)
			require.NoError(t, response.Body.Close())
			require.Equal(t, "real", owner.core.Snapshot().Attempts[0].ResponseID)
		})
	}
}
