package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const (
	OpenAICloudEnabledKey      = "openai_cloud"
	OpenAICloudModelsKey       = "openai_cloud_models"
	OpenAICloudProtocolVersion = "2026-10-23.1"
)

// Settings are admitted once, at the account persistence boundary (migration
// 290). These readers also fail closed on stale scheduler snapshots.
func (a *Account) IsOpenAICloudEligible() bool {
	return a != nil && a.IsOpenAIOAuth() && a.VendorID == nil && !a.IsShadow() &&
		!a.IsOpenAIAgentIdentity() && !a.IsOpenAIPersonalAccessToken() &&
		(a.GetCredential("client_id") == "" || a.GetCredential("client_id") == openai.ClientID)
}

func (a *Account) IsOpenAICloudEnabled() bool {
	if !a.IsOpenAICloudEligible() {
		return false
	}
	enabled, _ := a.Extra[OpenAICloudEnabledKey].(bool)
	bps, _ := a.Extra["openai_excel_bps"].(bool)
	return enabled && !bps
}

// IsOpenAICloudConfigured detects the operator's selected route even when the
// snapshot is no longer eligible. Callers must reject it rather than fall back.
func (a *Account) IsOpenAICloudConfigured() bool {
	if a == nil {
		return false
	}
	enabled, _ := a.Extra[OpenAICloudEnabledKey].(bool)
	return enabled
}

func (a *Account) OpenAICloudModels() []string {
	if a == nil {
		return nil
	}
	var result []string
	switch values := a.Extra[OpenAICloudModelsKey].(type) {
	case []string:
		result = append(result, values...)
	case []any:
		for _, value := range values {
			model, ok := value.(string)
			if !ok {
				return nil
			}
			result = append(result, model)
		}
	}
	for _, model := range result {
		if model == "" || strings.TrimSpace(model) != model {
			return nil
		}
	}
	return result
}

func (a *Account) IsOpenAICloudEnabledForModel(requestedModel string) bool {
	if !a.IsOpenAICloudEnabled() {
		return false
	}
	model := a.GetMappedModel(requestedModel)
	for _, allowed := range a.OpenAICloudModels() {
		if allowed == model {
			return true
		}
	}
	return false
}
