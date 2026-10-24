package service

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const (
	OpenAICloudEnabledKey      = "openai_cloud"
	OpenAICloudProtocolVersion = "2026-10-24.2"
)

// Authorization and protocol selection are admitted at the account persistence
// boundary. Cloud takes precedence while preserving the saved BPS configuration.
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
	return enabled
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
