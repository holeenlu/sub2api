package apicompat

import "github.com/tidwall/gjson"

func OpenAICacheCreationSourceFromUsage(value gjson.Result) gjson.Result {
	var base, reportedMax gjson.Result
	for _, path := range []string{
		"input_tokens_details.cache_write_tokens", "prompt_tokens_details.cache_write_tokens",
		"input_tokens_details.cache_creation_tokens", "prompt_tokens_details.cache_creation_tokens",
	} {
		field := value.Get(path)
		if !base.Exists() && field.Exists() {
			base = field
		}
		if field.Int() > reportedMax.Int() {
			reportedMax = field
		}
	}
	inputAlias := value.Get("input_tokens_details.cache_creation_input_tokens")
	promptAlias := value.Get("prompt_tokens_details.cache_creation_input_tokens")
	alias := inputAlias
	if promptAlias.Int() > alias.Int() {
		alias = promptAlias
	}
	if !base.Exists() {
		var first, zero gjson.Result
		for _, field := range []gjson.Result{
			value.Get("cache_write_tokens"), value.Get("cache_creation_input_tokens"),
			value.Get("cache_write_input_tokens"), value.Get("cache_creation_tokens"),
			inputAlias, promptAlias,
		} {
			if field.Int() > 0 {
				base = field
				break
			}
			if field.Exists() && !first.Exists() {
				first = field
			}
			// A coerced string, null, negative or fraction is not measured zero.
			if field.Type == gjson.Number && field.Float() == 0 && !zero.Exists() {
				zero = field
			}
		}
		if !base.Exists() {
			base = first
			if zero.Exists() {
				base = zero
			}
		}
	}
	return selectCacheCreationWithInputAlias(base, alias, gjson.Result.Int, reportedMax)
}

// OpenAICacheCreationTokensFromUsage retains the legacy nonnegative integer
// projection. Raw consumers must keep OpenAICacheCreationSourceFromUsage instead
// so an absent or invalid report cannot become a measured zero.
func selectCacheCreationWithInputAlias[T any](base, alias T, count func(T) int64, other ...T) T {
	if count(alias) <= 0 {
		return base
	}
	if count(alias) > count(base) {
		base = alias
	}
	for _, field := range other {
		if count(field) > count(base) {
			base = field
		}
	}
	return base
}
