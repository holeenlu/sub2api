package service

import (
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Compare the existing integer consumer with the strictly typed measurement
// from the same selected hosted field. Coercible legacy values are not proof
// that Native may replace a separately measured main zero.
func TestOpenAIHostedImageProjectionPreservesSourceAndLegacyFallback(t *testing.T) {
	for _, sample := range []struct {
		name, tool string
		measured   *int64
		legacy     int
	}{
		{"positive", "{\"input_tokens_details\":{\"image_tokens\":4},\"output_tokens_details\":{\"image_tokens\":4}}", nativeImageCount(4), 4},
		{"measured_zero", "{\"input_tokens_details\":{\"image_tokens\":0},\"output_tokens_details\":{\"image_tokens\":0}}", nativeImageCount(0), 0},
		{"absent", "{}", nil, 0},
		{"totals_are_not_image_components", "{\"input_tokens\":12,\"output_tokens\":8,\"total_tokens\":20}", nil, 0},
		{"null_field", "{\"input_tokens_details\":{\"image_tokens\":null},\"output_tokens_details\":{\"image_tokens\":null}}", nil, 0},
		{"legacy_string", "{\"input_tokens_details\":{\"image_tokens\":\"4\"},\"output_tokens_details\":{\"image_tokens\":\"4\"}}", nil, 4},
		{"fraction", "{\"input_tokens_details\":{\"image_tokens\":4.5},\"output_tokens_details\":{\"image_tokens\":4.5}}", nil, 4},
		{"negative", "{\"input_tokens_details\":{\"image_tokens\":-4},\"output_tokens_details\":{\"image_tokens\":-4}}", nil, 0},
		{"not_an_object", "4", nil, 0},
		{"null_object", "null", nil, 0},
		{"unknown_extension", "{\"extension\":{\"input_tokens_details\":{\"image_tokens\":99},\"output_tokens_details\":{\"image_tokens\":99}}}", nil, 0},
	} {
		t.Run(sample.name, func(t *testing.T) {
			tool := gjson.Parse(sample.tool)
			projection := projectOpenAIHostedImageUsage(tool)
			for _, field := range []openAIUsageField{projection.input, projection.output} {
				measurement := field.observed()
				if sample.measured == nil {
					require.Nil(t, measurement)
				} else {
					require.NotNil(t, measurement)
					require.Equal(t, *sample.measured, measurement.Value)
					measurement.Value = 99
					require.Equal(t, *sample.measured, field.observed().Value, "returned measurements do not mutate the selected wire field")
				}
			}
			for _, main := range []int{0, 2} {
				body := "{\"usage\":{\"input_tokens\":100,\"output_tokens\":50,\"input_tokens_details\":{\"image_tokens\":" + strconv.Itoa(main) + "},\"output_tokens_details\":{\"image_tokens\":" + strconv.Itoa(main) + "}},\"tool_usage\":{\"image_gen\":" + sample.tool + "}}"
				legacy, ok := extractOpenAIUsageFromJSONBytes([]byte(body))
				require.True(t, ok)
				want := main
				if main == 0 {
					want = sample.legacy
				}
				require.Equal(t, want, legacy.ImageInputTokens)
				require.Equal(t, want, legacy.ImageOutputTokens)
				require.Equal(t, 100, legacy.InputTokens)
				require.Equal(t, 50, legacy.OutputTokens)
				raw := projectNativeUsage(gjson.Get(body, "usage"), gjson.Get(body, "tool_usage.image_gen"))
				require.NotNil(t, raw)
				require.NotNil(t, raw.ImageInput)
				require.NotNil(t, raw.ImageOutput)
				require.EqualValues(t, main, raw.ImageInput.Value, "retain main scope even when the legacy projection uses hosted scope")
				require.EqualValues(t, main, raw.ImageOutput.Value)
				for _, observed := range []*model.TokenMeasurement{raw.HostedImageInput, raw.HostedImageOutput} {
					if sample.measured == nil {
						require.Nil(t, observed)
					} else {
						require.NotNil(t, observed)
						require.Equal(t, *sample.measured, observed.Value)
					}
				}
			}
		})
	}
}
