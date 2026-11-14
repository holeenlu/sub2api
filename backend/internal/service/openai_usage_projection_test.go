package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIUsageProjectionCacheAliasPrecedenceAndPresence(t *testing.T) {
	for _, sample := range []struct {
		name, usage string
		write       bool
		legacy      int
		observed    *int64
	}{
		{"read_canonical_zero", "{\"input_tokens_details\":{\"cached_tokens\":0},\"cached_tokens\":9}", false, 0, nativeImageCount(0)},
		{"read_prompt_precedes_top", "{\"prompt_tokens_details\":{\"cached_tokens\":2},\"cache_read_input_tokens\":9}", false, 2, nativeImageCount(2)},
		{"read_first_positive", "{\"cache_read_input_tokens\":0,\"cache_read_tokens\":3,\"cached_tokens\":9}", false, 3, nativeImageCount(3)},
		{"read_measured_zero", "{\"cache_read_tokens\":0}", false, 0, nativeImageCount(0)},
		{"write_prompt_before_creation", "{\"prompt_tokens_details\":{\"cache_write_tokens\":2},\"input_tokens_details\":{\"cache_creation_tokens\":9}}", true, 2, nativeImageCount(2)},
		{"write_canonical_zero", "{\"input_tokens_details\":{\"cache_creation_tokens\":0},\"cache_write_tokens\":9}", true, 0, nativeImageCount(0)},
		{"write_first_positive", "{\"cache_write_tokens\":0,\"cache_creation_input_tokens\":3,\"cache_creation_tokens\":9}", true, 3, nativeImageCount(3)},
		{"write_measured_zero", "{\"cache_creation_tokens\":0}", true, 0, nativeImageCount(0)},
		{"absent", "{}", false, 0, nil},
		{"invalid_negative", "{\"cached_tokens\":-1}", false, 0, nil},
		{"invalid_fraction", "{\"cached_tokens\":1.5}", false, 1, nil},
		{"legacy_string_is_not_measurement", "{\"cached_tokens\":\"3\"}", false, 3, nil},
	} {
		t.Run(sample.name, func(t *testing.T) {
			u := gjson.Parse(sample.usage)
			field, legacy := projectOpenAICacheRead(u), openAICacheReadTokensFromUsage(u)
			if sample.write {
				field, legacy = projectOpenAICacheWrite(u), openAICacheCreationTokensFromUsage(u)
			}
			require.Equal(t, sample.legacy, legacy)
			if sample.observed == nil {
				require.Nil(t, field.observed())
			} else {
				require.NotNil(t, field.observed())
				require.Equal(t, *sample.observed, field.observed().Value)
			}
		})
	}
}

func TestOpenAIUsageProjectionReasoningAliasesAndPresence(t *testing.T) {
	for _, sample := range []struct {
		name, fields     string
		legacy           int
		reasoning, total *int64
	}{
		{"output_alias", "\"output_tokens_details\":{\"reasoning_tokens\":3},\"total_tokens\":10", 3, nativeImageCount(3), nativeImageCount(10)},
		{"completion_precedes_output", "\"completion_tokens_details\":{\"reasoning_tokens\":2},\"output_tokens_details\":{\"reasoning_tokens\":3}", 2, nativeImageCount(2), nil},
		{"first_positive_skips_zero", "\"completion_tokens_details\":{\"reasoning_tokens\":0},\"output_tokens_details\":{\"reasoning_tokens\":3}", 3, nativeImageCount(3), nil},
		{"measured_zero", "\"completion_tokens_details\":{\"reasoning_tokens\":0},\"total_tokens\":0", 0, nativeImageCount(0), nativeImageCount(0)},
		{"absent", "\"extension\":true", 0, nil, nil},
		{"null", "\"output_tokens_details\":{\"reasoning_tokens\":null},\"total_tokens\":null", 0, nil, nil},
		{"legacy_string", "\"output_tokens_details\":{\"reasoning_tokens\":\"3\"},\"total_tokens\":\"10\"", 3, nil, nil},
		{"fraction", "\"output_tokens_details\":{\"reasoning_tokens\":1.5},\"total_tokens\":10.5", 1, nil, nil},
		{"negative", "\"output_tokens_details\":{\"reasoning_tokens\":-1},\"total_tokens\":-1", 0, nil, nil},
	} {
		t.Run(sample.name, func(t *testing.T) {
			raw := gjson.Parse("{\"input_tokens\":5,\"output_tokens\":2," + sample.fields + "}")
			require.Equal(t, sample.legacy, projectOpenAIReasoning(raw).legacyCount())
			u := projectNativeUsage(raw, gjson.Result{})
			require.NotNil(t, u)
			require.EqualValues(t, 2, u.Output.Value, "projection never folds reasoning into raw output")
			for _, pair := range []struct {
				want *int64
				got  *model.TokenMeasurement
			}{{sample.reasoning, u.Reasoning}, {sample.total, u.Total}} {
				if pair.want == nil {
					require.Nil(t, pair.got)
				} else {
					require.NotNil(t, pair.got)
					require.Equal(t, *pair.want, pair.got.Value)
				}
			}
		})
	}
	for _, auxiliary := range []string{
		"{\"total_tokens\":10}", "{\"output_tokens_details\":{\"reasoning_tokens\":3}}",
		"{\"total_tokens\":10,\"output_tokens_details\":{\"reasoning_tokens\":3}}",
	} {
		require.Nil(t, projectNativeUsage(gjson.Parse(auxiliary), gjson.Result{}), "auxiliary-only observation cannot create primary usage")
	}
}
