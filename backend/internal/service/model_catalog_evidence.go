package service

import (
	"encoding/json"
	"time"
)

func catalogFieldEvidence(m UpstreamModelMetadata, now time.Time, ttl time.Duration) map[string]CatalogFieldEvidence {
	raw, _ := json.Marshal(m)
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &fields)
	out := map[string]CatalogFieldEvidence{}
	for _, key := range []string{"reasoning", "supported_reasoning_levels", "default_reasoning_level", "input_modalities", "output_modalities", "context_window", "max_output_tokens", "endpoints", "media_capabilities", "shutdown_date"} {
		value, ok := fields[key]
		state := "known"
		if !ok {
			state = "missing"
		}
		source := m.FieldSources[key]
		if source == "" && ok {
			source = "supplemental"
		}
		out[key] = CatalogFieldEvidence{Value: value, State: state, Source: source, Revision: modelCatalogHash(value), ObservedAt: now, ExpiresAt: now.Add(ttl)}
	}
	return out
}

func catalogRevisionModels(models []ModelCatalogEntry) []ModelCatalogEntry {
	out := append([]ModelCatalogEntry(nil), models...)
	for i := range out {
		fields := map[string]CatalogFieldEvidence{}
		for key, value := range out[i].Fields {
			value.ObservedAt = time.Time{}
			value.ExpiresAt = time.Time{}
			fields[key] = value
		}
		out[i].Fields = fields
	}
	return out
}
