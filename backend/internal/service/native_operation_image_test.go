package service

import (
	"io"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

func TestNativeHTTPImageFactsUseActualWireMeasurements(t *testing.T) {
	for _, sample := range []struct {
		name, body                string
		input, output             *int64
		hostedInput, hostedOutput *int64
	}{
		{"image_only", "{\"object\":\"response\",\"status\":\"completed\",\"usage\":{\"input_tokens_details\":{\"image_tokens\":9},\"output_tokens_details\":{\"image_tokens\":5}}}", nativeImageCount(9), nativeImageCount(5), nil, nil},
		{"measured_zero", "{\"object\":\"response\",\"status\":\"completed\",\"usage\":{\"output_tokens_details\":{\"image_tokens\":0}}}", nil, nativeImageCount(0), nil, nil},
		{"chat_aliases", "{\"object\":\"chat.completion\",\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens_details\":{\"image_tokens\":7},\"completion_tokens_details\":{\"image_tokens\":0}}}", nativeImageCount(7), nativeImageCount(0), nil, nil},
		{"absent", "{\"object\":\"response\",\"status\":\"completed\",\"usage\":{}}", nil, nil, nil, nil},
		{"invalid", "{\"object\":\"response\",\"status\":\"completed\",\"usage\":{\"input_tokens_details\":{\"image_tokens\":-1},\"output_tokens_details\":{\"image_tokens\":1.5}}}", nil, nil, nil, nil},
		{"hosted_only", "{\"object\":\"response\",\"status\":\"completed\",\"tool_usage\":{\"image_gen\":{\"input_tokens_details\":{\"image_tokens\":4},\"output_tokens_details\":{\"image_tokens\":8}}}}", nil, nil, nativeImageCount(4), nativeImageCount(8)},
		{"hosted_measured_zero", "{\"object\":\"response\",\"status\":\"completed\",\"tool_usage\":{\"image_gen\":{\"output_tokens_details\":{\"image_tokens\":0}}}}", nil, nil, nil, nativeImageCount(0)},
		{"primary_zero", "{\"object\":\"response\",\"status\":\"completed\",\"usage\":{\"output_tokens_details\":{\"image_tokens\":0}},\"tool_usage\":{\"image_gen\":{\"output_tokens_details\":{\"image_tokens\":8}}}}", nil, nativeImageCount(0), nil, nativeImageCount(8)},
	} {
		t.Run(sample.name, func(t *testing.T) {
			family := nativeUpstreamResponses
			if sample.name == "chat_aliases" {
				family = nativeUpstreamChat
			}
			f, owner, _ := nativeHTTPObservedFixture(t, sample.body, "application/json", family)
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			wire, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, sample.body, string(wire))
			require.NoError(t, response.Body.Close())
			usage := owner.core.Snapshot().Attempts[0].Usage
			if sample.input == nil && sample.output == nil && sample.hostedInput == nil && sample.hostedOutput == nil {
				require.Nil(t, usage)
				return
			}
			require.NotNil(t, usage)
			for _, component := range []struct {
				want   *int64
				actual *model.TokenMeasurement
			}{
				{sample.input, usage.ImageInput}, {sample.output, usage.ImageOutput},
				{sample.hostedInput, usage.HostedImageInput}, {sample.hostedOutput, usage.HostedImageOutput},
			} {
				if component.want == nil {
					require.Nil(t, component.actual)
				} else {
					require.NotNil(t, component.actual)
					require.Equal(t, *component.want, component.actual.Value)
				}
			}
			require.Nil(t, usage.Input, "image input is not total input")
			require.Nil(t, usage.Output, "image output is not total output")
			require.Nil(t, usage.ImageCount, "image tokens do not imply completed image count")
			require.Equal(t, model.UsageFinal, usage.Confidence)
			require.True(t, usage.TerminalConfirmed)
		})
	}
}

func nativeImageCount(n int64) *int64 { return &n }

func TestNativeHTTPImageCounterUsesCompletedOutputsOnly(t *testing.T) {
	item := "{\"type\":\"image_generation_call\",\"id\":\"img_done\",\"result\":\"actual-image\"}"
	partial := "{\"type\":\"image_generation_call\",\"id\":\"img_partial\",\"result\":\"preview\",\"partial_image_index\":0}"
	for _, sample := range []struct {
		name, wire, contentType string
		count                   *int64
		confidence              model.UsageConfidence
	}{
		{"json_deduplicates", "{\"object\":\"response\",\"status\":\"completed\",\"output\":[" + item + "," + item + "," + partial + "]}", "application/json", nativeImageCount(1), model.UsageFinal},
		{"named_sse_done_and_terminal", "event: response.output_item.done\ndata: {\"item\":" + item + "}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[" + item + "," + partial + "]}}\n\n", "text/event-stream", nativeImageCount(1), model.UsageFinal},
		{"completed_item_before_eof", "data: {\"type\":\"response.output_item.done\",\"item\":" + item + "}\n\n", "text/event-stream", nativeImageCount(1), model.UsagePartial},
		{"only_partial_image", "data: {\"type\":\"response.output_item.done\",\"item\":" + partial + "}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n", "text/event-stream", nil, model.UsageUnknown},
	} {
		t.Run(sample.name, func(t *testing.T) {
			f, owner, _ := nativeHTTPObservedFixture(t, sample.wire, sample.contentType)
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			wire, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, sample.wire, string(wire), "observing event-named SSE must not rewrite wire")
			require.NoError(t, response.Body.Close())
			usage := owner.core.Snapshot().Attempts[0].Usage
			if sample.count == nil {
				require.Nil(t, usage)
				return
			}
			require.NotNil(t, usage)
			require.NotNil(t, usage.ImageCount)
			require.Equal(t, *sample.count, *usage.ImageCount)
			require.Nil(t, usage.Input)
			require.Nil(t, usage.Output)
			require.Nil(t, usage.ImageInput)
			require.Nil(t, usage.ImageOutput)
			require.Equal(t, sample.confidence, usage.Confidence)
			require.Equal(t, sample.confidence == model.UsageFinal, usage.TerminalConfirmed)
		})
	}
}

func TestNativeHTTPImageUsageMergePreservesMeasurements(t *testing.T) {
	d := nativeOperationDecoder{sse: true}
	first := d.payload([]byte("{\"type\":\"response.in_progress\",\"response\":{\"usage\":{\"input_tokens_details\":{\"image_tokens\":7}}}}"))
	require.Equal(t, model.UsagePartial, first.Usage.Confidence)
	require.False(t, first.Usage.TerminalConfirmed)
	first.Usage.ImageInput.Value = 99
	final := d.payload([]byte("{\"type\":\"response.completed\",\"response\":{\"usage\":{\"output_tokens_details\":{\"image_tokens\":0}}}}"))
	require.EqualValues(t, 7, final.Usage.ImageInput.Value)
	require.EqualValues(t, 0, final.Usage.ImageOutput.Value)
	require.Nil(t, final.Usage.Input)
	require.Nil(t, final.Usage.Output)
	require.Equal(t, model.UsagePartial, final.Usage.Confidence, "earlier image input was not included in the terminal snapshot")
	require.True(t, final.Usage.TerminalConfirmed)
	failed := nativeOperationDecoder{sse: true}
	observation := failed.payload([]byte("{\"type\":\"response.failed\",\"response\":{\"usage\":{\"output_tokens_details\":{\"image_tokens\":2}}}}"))
	require.Equal(t, model.UsageFinal, observation.Usage.Confidence)
	require.True(t, observation.Usage.TerminalConfirmed)
	require.EqualValues(t, 2, observation.Usage.ImageOutput.Value)
}
