package service

import (
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

// Cache field selection is shared by the existing integer result and the
// presence-aware Native observation. This selects a source; it never prices,
// sums lifetime components, or fills absent counters from request metadata.
type openAIUsageField struct{ source gjson.Result }

func (f openAIUsageField) legacyCount() int { return max(int(f.source.Int()), 0) }
func (f openAIUsageField) observed() *model.TokenMeasurement {
	return observedOpenAIUsageNumber(f.source)
}

func projectOpenAICacheRead(value gjson.Result) openAIUsageField {
	return selectOpenAIUsageField(value,
		[]string{"input_tokens_details.cached_tokens", "prompt_tokens_details.cached_tokens"},
		[]string{"cache_read_input_tokens", "cache_read_tokens", "cached_tokens"})
}

func projectOpenAICacheWrite(value gjson.Result) openAIUsageField {
	return openAIUsageField{source: apicompat.OpenAICacheCreationSourceFromUsage(value)}
}

func projectOpenAIReasoning(value gjson.Result) openAIUsageField {
	// Match the existing first-positive alias order for the legacy result;
	// the Native projection additionally preserves valid measured zero.
	return selectOpenAIUsageField(value, nil,
		[]string{"completion_tokens_details.reasoning_tokens", "output_tokens_details.reasoning_tokens"})
}

// Hosted image measurements belong to tool_usage.image_gen, independently of
// the response's usage. Keep the source so strict observations and the existing
// integer parser select the same field without confusing absence with zero.
type openAIHostedImageUsage struct {
	input, output openAIUsageField
}

func projectOpenAIHostedImageUsage(value gjson.Result) openAIHostedImageUsage {
	if !value.IsObject() {
		return openAIHostedImageUsage{}
	}
	return openAIHostedImageUsage{
		input:  openAIUsageField{source: value.Get("input_tokens_details.image_tokens")},
		output: openAIUsageField{source: value.Get("output_tokens_details.image_tokens")},
	}
}

// This is the existing hosted-image fallback, not a price or an observation.
// Callers retaining raw measurements must keep both scopes before projecting.
// A billing validator may pass only separately validated source measurements.
func projectOpenAIHostedImageCount(primary, hosted int64) int64 {
	if primary == 0 && hosted > 0 {
		return hosted
	}
	return primary
}

func selectOpenAIUsageField(value gjson.Result, canonical, fallback []string) openAIUsageField {
	for _, path := range canonical {
		if field := value.Get(path); field.Exists() {
			return openAIUsageField{field}
		}
	}
	var first, zero gjson.Result
	for _, path := range fallback {
		field := value.Get(path)
		// Preserve the original first-positive compatibility ordering. Native
		// still requires a valid JSON integer before publishing a measurement.
		if field.Int() > 0 {
			return openAIUsageField{field}
		}
		if field.Exists() && !first.Exists() {
			first = field
		}
		if observed := observedOpenAIUsageNumber(field); observed != nil && observed.Value == 0 && !zero.Exists() {
			zero = field
		}
	}
	if zero.Exists() {
		return openAIUsageField{zero}
	}
	return openAIUsageField{first}
}

func observedOpenAIUsageNumber(value gjson.Result) *model.TokenMeasurement {
	if value.Exists() && value.Type == gjson.Number && value.Float() >= 0 && float64(value.Int()) == value.Float() {
		return &model.TokenMeasurement{Value: value.Int()}
	}
	return nil
}

func firstObservedOpenAIUsage(value gjson.Result, paths ...string) *model.TokenMeasurement {
	for _, path := range paths {
		if observed := observedOpenAIUsageNumber(value.Get(path)); observed != nil {
			return observed
		}
	}
	return nil
}

// Raw output and its optional normalization evidence stay separate. The
// existing pricing/mapping boundaries own canonical output; this projection
// never adds reasoning to output or invents a total from other measurements.
func projectNativeUsage(value, imageTool gjson.Result) *model.UsageFact {
	hosted := projectOpenAIHostedImageUsage(imageTool)
	u := &model.UsageFact{
		Input:             firstObservedOpenAIUsage(value, "input_tokens", "prompt_tokens"),
		Output:            firstObservedOpenAIUsage(value, "output_tokens", "completion_tokens"),
		CacheRead:         projectOpenAICacheRead(value).observed(),
		CacheWrite:        projectOpenAICacheWrite(value).observed(),
		ImageInput:        firstObservedOpenAIUsage(value, "input_tokens_details.image_tokens", "prompt_tokens_details.image_tokens"),
		ImageOutput:       firstObservedOpenAIUsage(value, "output_tokens_details.image_tokens", "completion_tokens_details.image_tokens"),
		HostedImageInput:  hosted.input.observed(),
		HostedImageOutput: hosted.output.observed(),
	}
	// Auxiliary proof alone cannot create primary usage or a zero-token bill.
	if u.Input == nil && u.Output == nil && u.CacheRead == nil && u.CacheWrite == nil &&
		u.ImageInput == nil && u.ImageOutput == nil && u.HostedImageInput == nil && u.HostedImageOutput == nil {
		return nil
	}
	u.Total = firstObservedOpenAIUsage(value, "total_tokens")
	u.Reasoning = projectOpenAIReasoning(value).observed()
	return u
}

// An explicit terminal snapshot must cover every previously observed token
// component before those watermarks can become Final. ImageCount has its own
// completed-output proof and is never inferred from token measurements.
func nativeUsageProjectionCovers(previous, current *model.UsageFact) bool {
	if previous == nil {
		return true
	}
	if current == nil {
		current = &model.UsageFact{}
	}
	return (previous.Total == nil || current.Total != nil) &&
		(previous.Reasoning == nil || current.Reasoning != nil) &&
		(previous.Input == nil || current.Input != nil) &&
		(previous.Output == nil || current.Output != nil) &&
		(previous.CacheRead == nil || current.CacheRead != nil) &&
		(previous.CacheWrite == nil || current.CacheWrite != nil) &&
		(previous.ImageInput == nil || current.ImageInput != nil) &&
		(previous.ImageOutput == nil || current.ImageOutput != nil) &&
		(previous.HostedImageInput == nil || current.HostedImageInput != nil) &&
		(previous.HostedImageOutput == nil || current.HostedImageOutput != nil)
}
