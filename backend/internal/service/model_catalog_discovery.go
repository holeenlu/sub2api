package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Cursor traversal never follows an upstream URL: credentials stay at the
// validated endpoint. Partial traversal is an error, never an authoritative list.
func (s *AccountTestService) fetchPaginatedModelCatalog(ctx context.Context, a *Account, req *http.Request) ([]string, []byte, error) {
	entries := []json.RawMessage{}
	seen := map[string]bool{}
	total := 0
	for page := 0; page < 100; page++ {
		_, body, err := s.fetchSingleUpstreamModelPage(ctx, a, req)
		if err != nil {
			return nil, nil, err
		}
		total += len(body)
		if int64(total) > resolveModelsListReadLimit(s.cfg) {
			return nil, nil, fmt.Errorf("combined model catalog exceeds configured limit")
		}
		current, err := extractUpstreamModelRawEntries(body)
		if err != nil {
			return nil, nil, newUpstreamModelSyncUpstreamError("Invalid model list page", err)
		}
		entries = append(entries, current...)
		var envelope struct {
			HasMore       bool   `json:"has_more"`
			LastID        string `json:"last_id"`
			NextPageToken string `json:"nextPageToken"`
		}
		_ = json.Unmarshal(body, &envelope)
		cursor := envelope.NextPageToken
		param := "pageToken"
		if cursor == "" && envelope.HasMore {
			cursor = envelope.LastID
			param = "after_id"
			if cursor == "" {
				return nil, nil, fmt.Errorf("model page has_more without cursor")
			}
		}
		if cursor == "" {
			merged, err := json.Marshal(map[string]any{"data": entries})
			if err != nil {
				return nil, nil, err
			}
			ids, err := extractUpstreamModelIDs(merged)
			if a.IsGrok() {
				ids, err = extractGrokUpstreamModelIDs(merged)
			}
			return ids, merged, err
		}
		if seen[param+cursor] {
			return nil, nil, fmt.Errorf("repeated model catalog cursor")
		}
		seen[param+cursor] = true
		next := req.Clone(ctx)
		u := *req.URL
		q := u.Query()
		q.Set(param, cursor)
		u.RawQuery = q.Encode()
		next.URL = &u
		req = next
	}
	return nil, nil, fmt.Errorf("model catalog exceeded 100 pages")
}

func enrichNativeCatalogMetadata(m UpstreamModelMetadata, e upstreamModelCapabilityEntry) UpstreamModelMetadata {
	if e.SupportedReasoningLevels != nil && len(e.SupportedReasoningLevels) == 0 && m.Reasoning == nil {
		disabled := false
		m.Reasoning = &disabled
	}
	m.OutputModalities = e.OutputModalities
	if len(m.OutputModalities) == 0 {
		m.OutputModalities = e.Modalities.Output
	}
	m.ModelKind = e.ModelKind
	if m.ModelKind == "" {
		switch e.Mode {
		case "image_generation":
			m.ModelKind = "image"
		case "embedding":
			m.ModelKind = "embedding"
		case "chat":
			m.ModelKind = "chat"
		}
	}
	m.Endpoints = e.Endpoints
	m.ShutdownDate = e.ShutdownDate
	m.Deprecated = e.Deprecated
	m.RecommendedPriority = e.Priority
	if m.ContextWindow <= 0 {
		m.ContextWindow = e.MaxInputTokens
	}
	if m.ContextWindow <= 0 {
		m.ContextWindow = e.InputTokenLimit
	}
	if m.MaxOutputTokens <= 0 {
		m.MaxOutputTokens = e.NativeMaxTokens
	}
	if m.MaxOutputTokens <= 0 {
		m.MaxOutputTokens = e.OutputTokenLimit
	}
	if len(e.GenerationMethods) > 0 {
		for _, method := range e.GenerationMethods {
			if method == "generateContent" {
				m.Endpoints = append(m.Endpoints, "generateContent")
				m.ModelKind = "chat"
			}
			if method == "embedContent" {
				m.ModelKind = "embedding"
				m.Endpoints = append(m.Endpoints, method)
			}
		}
	}
	if len(e.Capabilities) > 0 {
		m.MediaCapabilities = e.Capabilities
		var image struct {
			Supported bool `json:"supported"`
		}
		if json.Unmarshal(e.Capabilities["image_input"], &image) == nil {
			m.InputModalities = []string{"text"}
			if image.Supported {
				m.InputModalities = append(m.InputModalities, "image")
			}
		}
		var effort map[string]json.RawMessage
		if json.Unmarshal(e.Capabilities["effort"], &effort) == nil {
			var supported bool
			if json.Unmarshal(effort["supported"], &supported) == nil {
				m.Reasoning = &supported
			}
			for _, level := range []string{"low", "medium", "high", "xhigh", "max"} {
				var cap struct {
					Supported bool `json:"supported"`
				}
				if json.Unmarshal(effort[level], &cap) == nil && cap.Supported {
					m.SupportedReasoningLevels = append(m.SupportedReasoningLevels, level)
				}
			}
		}
	}
	m.FieldSources = map[string]string{}
	for key, present := range map[string]bool{"reasoning": m.Reasoning != nil, "input_modalities": len(m.InputModalities) > 0, "output_modalities": len(m.OutputModalities) > 0, "context_window": m.ContextWindow > 0, "supported_reasoning_levels": len(m.SupportedReasoningLevels) > 0, "shutdown_date": m.ShutdownDate != ""} {
		if present {
			m.FieldSources[key] = "upstream"
		}
	}
	if len(m.FieldSources) == 0 {
		m.FieldSources = nil
	}
	return m
}

// Preserve only the documented Codex model contract, not arbitrary source fields.
func catalogCodexDescriptors(body []byte) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	entries, err := extractUpstreamModelRawEntries(body)
	if err != nil {
		return out
	}
	defaults, _ := json.Marshal(newConfiguredCodexModelDescriptor("catalog-placeholder"))
	var allowed map[string]json.RawMessage
	_ = json.Unmarshal(defaults, &allowed)
	allowed["default_reasoning_level"] = nil
	allowed["multi_agent_reasoning_effort"] = nil
	allowed["model_purpose"] = nil
	for _, raw := range entries {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			continue
		}
		var slug string
		if json.Unmarshal(fields["slug"], &slug) != nil || strings.TrimSpace(slug) == "" {
			continue
		}
		filtered := map[string]json.RawMessage{}
		for field, value := range fields {
			if _, ok := allowed[field]; ok {
				filtered[field] = value
			}
		}
		if encoded, err := json.Marshal(filtered); err == nil && len(encoded) <= 1<<18 {
			out[slug] = encoded
		}
	}
	return out
}
