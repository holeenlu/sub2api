package service

import (
	"io"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

func TestNativeHTTPHostedImageTerminalCannotCertifyEarlierToolScope(t *testing.T) {
	for _, family := range []nativeUpstreamFamily{nativeUpstreamResponses, nativeUpstreamChat} {
		for _, sample := range []struct {
			name, terminalTool string
			confidence         model.UsageConfidence
			hostedInput        int64
			hostedOutput       int64
		}{
			{"omitted", "", model.UsagePartial, 4, 8},
			{"invalid", ",\"tool_usage\":{\"image_gen\":{\"input_tokens_details\":{\"image_tokens\":\"4\"},\"output_tokens_details\":{\"image_tokens\":\"8\"}}}", model.UsagePartial, 4, 8},
			{"same_snapshot_positive", ",\"tool_usage\":{\"image_gen\":{\"input_tokens_details\":{\"image_tokens\":4},\"output_tokens_details\":{\"image_tokens\":8}}}", model.UsageFinal, 4, 8},
			{"same_snapshot_measured_zero", ",\"tool_usage\":{\"image_gen\":{\"input_tokens_details\":{\"image_tokens\":0},\"output_tokens_details\":{\"image_tokens\":0}}}", model.UsageFinal, 0, 0},
		} {
			t.Run(string(family)+"/"+sample.name, func(t *testing.T) {
				usage := "\"usage\":{\"input_tokens\":100,\"output_tokens\":50,\"input_tokens_details\":{\"image_tokens\":0},\"output_tokens_details\":{\"image_tokens\":0}}"
				tool := ",\"tool_usage\":{\"image_gen\":{\"input_tokens_details\":{\"image_tokens\":4},\"output_tokens_details\":{\"image_tokens\":8}}}"
				first := "data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"image-scope\",\"object\":\"response\"," + usage + tool + "}}\n\n"
				last := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"image-scope\",\"object\":\"response\",\"status\":\"completed\"," + usage + sample.terminalTool + "}}\n\n"
				if family == nativeUpstreamChat {
					first = "data: {\"id\":\"image-scope\",\"object\":\"chat.completion.chunk\",\"choices\":[]," + usage + tool + "}\n\n"
					last = "data: {\"id\":\"image-scope\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]," + usage + sample.terminalTool + "}\n\ndata: [DONE]\n\n"
				}
				wire := first + last
				f, owner, _ := nativeHTTPObservedFixture(t, wire, "text/event-stream", family)
				response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
				require.NoError(t, err)
				got, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				require.Equal(t, wire, string(got))
				attempt := owner.core.Snapshot().Attempts[0]
				require.Equal(t, model.TerminalCompleted, attempt.Terminal)
				require.NotNil(t, attempt.Usage)
				require.True(t, attempt.Usage.TerminalConfirmed)
				require.EqualValues(t, 0, attempt.Usage.ImageInput.Value)
				require.EqualValues(t, 0, attempt.Usage.ImageOutput.Value)
				require.NotNil(t, attempt.Usage.HostedImageInput)
				require.NotNil(t, attempt.Usage.HostedImageOutput)
				require.Equal(t, sample.hostedInput, attempt.Usage.HostedImageInput.Value)
				require.Equal(t, sample.hostedOutput, attempt.Usage.HostedImageOutput.Value)
				require.Equal(t, sample.confidence, attempt.Usage.Confidence, "terminal main counters do not certify an earlier hosted scope")
			})
		}
	}
}

func TestNativeHTTPHostedImageExtensionsCannotSupplyMeasurements(t *testing.T) {
	tool := "\"tool_usage\":{\"image_gen\":{\"input_tokens_details\":{\"image_tokens\":99},\"output_tokens_details\":{\"image_tokens\":99}}}"
	terminal := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"image-scope\",\"object\":\"response\",\"model\":\"returned-main\",\"status\":\"completed\",\"usage\":{\"input_tokens\":100,\"output_tokens\":50,\"input_tokens_details\":{\"image_tokens\":0},\"output_tokens_details\":{\"image_tokens\":0}}}}\n\n"
	for _, sample := range []struct {
		name, event string
	}{
		{"future_namespace", "{\"type\":\"response.future_telemetry\",\"response\":{\"id\":\"foreign\",\"model\":\"foreign-model\"," + tool + "}}"},
		{"foreign_protocol", "{\"object\":\"chat.completion.chunk\",\"id\":\"foreign\",\"model\":\"foreign-model\",\"choices\":[]," + tool + "}"},
		{"known_output_event_is_not_usage", "{\"type\":\"response.output_item.added\",\"response_id\":\"image-scope\",\"item\":{\"type\":\"message\",\"id\":\"item-1\"}," + tool + "}"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			event := "data: " + sample.event + "\n\n"
			wire := event + terminal + event
			f, owner, _ := nativeHTTPObservedFixture(t, wire, "text/event-stream", nativeUpstreamResponses)
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			got, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, wire, string(got))
			attempt := owner.core.Snapshot().Attempts[0]
			require.Equal(t, "image-scope", attempt.ResponseID)
			require.Equal(t, "returned-main", attempt.ReturnedModel)
			require.Equal(t, model.TerminalCompleted, attempt.Terminal)
			require.NotNil(t, attempt.Usage)
			require.Equal(t, model.UsageFinal, attempt.Usage.Confidence)
			require.EqualValues(t, 0, attempt.Usage.ImageInput.Value)
			require.EqualValues(t, 0, attempt.Usage.ImageOutput.Value)
			require.Nil(t, attempt.Usage.HostedImageInput)
			require.Nil(t, attempt.Usage.HostedImageOutput)
		})
	}
}
