package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAICacheWriteAliasReviewPreservesRawSource(t *testing.T) {
	// Maintenance baseline: 79eacfa489444a3f657d5eaa684665ed6c378b77.
	// Counts follow that baseline; Native additionally retains the selected raw
	// number's presence and validity instead of manufacturing a measurement.
	for _, tt := range []struct {
		name, usage, source string
		count               int
		observed            bool
	}{
		{"responses_alias", `{"input_tokens_details":{"cache_creation_input_tokens":7}}`, "input_tokens_details.cache_creation_input_tokens", 7, true},
		{"chat_alias", `{"prompt_tokens_details":{"cache_creation_input_tokens":7}}`, "prompt_tokens_details.cache_creation_input_tokens", 7, true},
		{"larger_prompt_alias", `{"input_tokens_details":{"cache_creation_input_tokens":3},"prompt_tokens_details":{"cache_creation_input_tokens":9}}`, "prompt_tokens_details.cache_creation_input_tokens", 9, true},
		{"alias_overrides_canonical_zero", `{"input_tokens_details":{"cache_write_tokens":0,"cache_creation_input_tokens":7}}`, "input_tokens_details.cache_creation_input_tokens", 7, true},
		{"canonical_max_with_alias", `{"input_tokens_details":{"cache_write_tokens":3,"cache_creation_tokens":11,"cache_creation_input_tokens":7}}`, "input_tokens_details.cache_creation_tokens", 11, true},
		{"other_details_max_with_alias", `{"input_tokens_details":{"cache_write_tokens":3,"cache_creation_input_tokens":7},"prompt_tokens_details":{"cache_creation_tokens":11}}`, "prompt_tokens_details.cache_creation_tokens", 11, true},
		{"duplicates_keep_canonical_source", `{"input_tokens_details":{"cache_write_tokens":7,"cache_creation_tokens":7,"cache_creation_input_tokens":7},"prompt_tokens_details":{"cache_write_tokens":7,"cache_creation_tokens":7,"cache_creation_input_tokens":7},"cache_write_tokens":7,"cache_creation_input_tokens":7,"cache_write_input_tokens":7,"cache_creation_tokens":7}`, "input_tokens_details.cache_write_tokens", 7, true},
		{"alias_duplicates_keep_input_source", `{"input_tokens_details":{"cache_creation_input_tokens":7},"prompt_tokens_details":{"cache_creation_input_tokens":7}}`, "input_tokens_details.cache_creation_input_tokens", 7, true},
		{"top_level_is_not_extra_max_with_canonical", `{"input_tokens_details":{"cache_write_tokens":3,"cache_creation_input_tokens":7},"cache_write_tokens":19}`, "input_tokens_details.cache_creation_input_tokens", 7, true},
		{"fallback_larger_than_alias", `{"cache_creation_input_tokens":19,"prompt_tokens_details":{"cache_creation_input_tokens":7}}`, "cache_creation_input_tokens", 19, true},
		{"alias_larger_than_fallback", `{"cache_write_tokens":3,"prompt_tokens_details":{"cache_creation_input_tokens":7}}`, "prompt_tokens_details.cache_creation_input_tokens", 7, true},
		{"first_positive_fallback_with_alias", `{"cache_write_tokens":3,"cache_creation_tokens":19,"prompt_tokens_details":{"cache_creation_input_tokens":7}}`, "prompt_tokens_details.cache_creation_input_tokens", 7, true},
		{"no_alias_canonical_zero", `{"input_tokens_details":{"cache_write_tokens":0,"cache_creation_tokens":11},"cache_creation_input_tokens":19}`, "input_tokens_details.cache_write_tokens", 0, true},
		{"no_alias_prompt_write_precedes_input_creation", `{"input_tokens_details":{"cache_creation_tokens":11},"prompt_tokens_details":{"cache_write_tokens":3}}`, "prompt_tokens_details.cache_write_tokens", 3, true},
		{"no_alias_creation_zero", `{"input_tokens_details":{"cache_creation_tokens":0},"cache_write_tokens":19}`, "input_tokens_details.cache_creation_tokens", 0, true},
		{"no_alias_prompt_creation_zero", `{"prompt_tokens_details":{"cache_creation_tokens":0},"cache_write_tokens":19}`, "prompt_tokens_details.cache_creation_tokens", 0, true},
		{"zero_alias_does_not_enable_max", `{"input_tokens_details":{"cache_write_tokens":3,"cache_creation_tokens":11,"cache_creation_input_tokens":0}}`, "input_tokens_details.cache_write_tokens", 3, true},
		{"negative_alias_does_not_enable_max", `{"input_tokens_details":{"cache_write_tokens":0,"cache_creation_tokens":11,"cache_creation_input_tokens":-1}}`, "input_tokens_details.cache_write_tokens", 0, true},
		{"no_alias_first_positive_fallback", `{"cache_write_tokens":0,"cache_creation_input_tokens":3,"cache_creation_tokens":19}`, "cache_creation_input_tokens", 3, true},
		{"fallback_measured_zero_over_invalid", `{"cache_write_tokens":null,"cache_creation_input_tokens":-1,"cache_creation_tokens":0}`, "cache_creation_tokens", 0, true},
		{"alias_measured_zero", `{"input_tokens_details":{"cache_creation_input_tokens":0}}`, "input_tokens_details.cache_creation_input_tokens", 0, true},
		{"prompt_alias_measured_zero", `{"prompt_tokens_details":{"cache_creation_input_tokens":0}}`, "prompt_tokens_details.cache_creation_input_tokens", 0, true},
		{"raw_number_spelling", `{"input_tokens_details":{"cache_creation_input_tokens":7e0}}`, "input_tokens_details.cache_creation_input_tokens", 7, true},
		{"unknown", `{}`, "", 0, false},
		{"alias_null", `{"input_tokens_details":{"cache_creation_input_tokens":null}}`, "input_tokens_details.cache_creation_input_tokens", 0, false},
		{"alias_negative", `{"input_tokens_details":{"cache_creation_input_tokens":-1}}`, "input_tokens_details.cache_creation_input_tokens", 0, false},
		{"alias_string_zero", `{"input_tokens_details":{"cache_creation_input_tokens":"0"}}`, "input_tokens_details.cache_creation_input_tokens", 0, false},
		{"alias_string_max_stays_invalid", `{"input_tokens_details":{"cache_write_tokens":3,"cache_creation_input_tokens":"7"}}`, "input_tokens_details.cache_creation_input_tokens", 7, false},
		{"alias_fraction_max_stays_invalid", `{"input_tokens_details":{"cache_write_tokens":3,"cache_creation_input_tokens":7.5}}`, "input_tokens_details.cache_creation_input_tokens", 7, false},
		{"other_max_string_stays_invalid", `{"input_tokens_details":{"cache_write_tokens":3,"cache_creation_tokens":"11","cache_creation_input_tokens":7}}`, "input_tokens_details.cache_creation_tokens", 11, false},
		{"other_max_fraction_stays_invalid", `{"input_tokens_details":{"cache_write_tokens":3,"cache_creation_tokens":11.5,"cache_creation_input_tokens":7}}`, "input_tokens_details.cache_creation_tokens", 11, false},
		{"canonical_invalid_tie_keeps_source", `{"input_tokens_details":{"cache_write_tokens":"7","cache_creation_input_tokens":7}}`, "input_tokens_details.cache_write_tokens", 7, false},
		{"canonical_null_presence", `{"input_tokens_details":{"cache_write_tokens":null},"cache_write_tokens":19}`, "input_tokens_details.cache_write_tokens", 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			usage := gjson.Parse(tt.usage)
			field := projectOpenAICacheWrite(usage)
			var source gjson.Result
			if tt.source != "" {
				source = usage.Get(tt.source)
				require.True(t, source.Exists())
			}
			require.Equal(t, source, field.source, "retain raw type, spelling and position")
			require.Equal(t, source, apicompat.OpenAICacheCreationSourceFromUsage(usage))
			require.Equal(t, tt.count, apicompat.OpenAICacheCreationTokensFromUsage(usage))
			require.Equal(t, tt.count, field.legacyCount())
			legacy, ok := openAIUsageFromGJSON(usage)
			require.True(t, ok)
			require.Equal(t, tt.count, legacy.CacheCreationInputTokens)

			native := projectNativeUsage(usage, gjson.Result{})
			if !tt.observed {
				require.Nil(t, field.observed())
				require.Nil(t, native, "unknown/invalid cache usage cannot manufacture a zero fact")
				return
			}
			require.NotNil(t, field.observed())
			require.EqualValues(t, tt.count, field.observed().Value)
			require.NotNil(t, native)
			require.Equal(t, field.observed(), native.CacheWrite)
			require.Nil(t, native.Input, "cache reports must not invent prompt or total counts")
			require.Nil(t, native.Total)
		})
	}
}
