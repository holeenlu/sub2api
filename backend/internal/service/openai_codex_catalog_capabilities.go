package service

import (
	"bytes"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/catalog"
)

// Only current, explicitly declared evidence can supplement a live API-key
// manifest. It does not change model availability or the other platforms.
func codexCatalogScopedCapabilities(account *Account, snapshot *UpstreamModelMetadataSnapshot, capabilities map[string]json.RawMessage) map[string]json.RawMessage {
	if snapshot == nil || snapshot.CapabilityScope != codexCapabilityScope(account) {
		return nil
	}
	result := make(map[string]json.RawMessage, len(capabilities))
	for field, raw := range capabilities {
		result[field] = raw
	}
	normalizeCodexCatalogBooleanEvidence(result)
	return result
}

// Missing evidence can use an implemented local capability. Explicitly invalid
// evidence is unknown, and must block a default true just like explicit null.
func normalizeCodexCatalogBooleanEvidence(fields map[string]json.RawMessage) bool {
	changed := false
	for _, field := range []string{"supports_search_tool", "use_responses_lite", "supports_reasoning_effort_updates"} {
		if raw, present := fields[field]; present && catalog.BooleanCapability(raw) == catalog.CapabilityUnknown && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			fields[field] = json.RawMessage("null")
			changed = true
		}
	}
	return changed
}

// Project API-key discovery without metadata belonging to an old identity.
// Existing capability consumers still own the local bridge/Cloud/BPS facts.
func codexCatalogMetadataAccount(account *Account) *Account {
	if account == nil || !account.IsOpenAIApiKey() {
		return account
	}
	snapshot := account.GetUpstreamModelMetadataSnapshot()
	if snapshot == nil {
		return account
	}
	matched := snapshot.CapabilityScope == codexCapabilityScope(account)
	if matched {
		changed := false
		for id, metadata := range snapshot.Models {
			fields := make(map[string]json.RawMessage, len(metadata.CodexToolCapabilities))
			for field, raw := range metadata.CodexToolCapabilities {
				fields[field] = raw
			}
			if normalizeCodexCatalogBooleanEvidence(fields) {
				metadata.CodexToolCapabilities = fields
				snapshot.Models[id] = metadata
				changed = true
			}
		}
		if !changed {
			return account
		}
	}
	current := *account
	current.Extra = make(map[string]any, len(account.Extra))
	for key, value := range account.Extra {
		if key != UpstreamModelMetadataExtraKey {
			current.Extra[key] = value
		}
	}
	if matched {
		current.Extra[UpstreamModelMetadataExtraKey] = *snapshot
	}
	return &current
}
