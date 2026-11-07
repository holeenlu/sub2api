package service

import (
	"encoding/json"
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const (
	OpenAICloudEnabledKey      = "openai_cloud"
	OpenAICloudProtocolVersion = "2026-10-25.parser.1"
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

func validateOpenAICloudAccountSettings(account *Account) error {
	if account.IsOpenAICloudConfigured() && (!account.IsOpenAICloudEligible() || account.GetChatGPTAccountID() == "" || account.GetChatGPTUserID() == "") {
		return infraerrors.BadRequest("OPENAI_CLOUD_ACCOUNT_SETTINGS_INVALID", fmt.Sprintf("Cloud cannot be enabled for account #%d: an ordinary native OpenAI OAuth identity is required", account.ID))
	}
	return nil
}

// Only an actual Cloud switch change may bypass revalidation of saved BPS
// settings. Compare the entire identity/credentials and every BPS field so a
// simultaneous authorization, model mapping or BPS edit keeps its old checks.
func cloudSwitchValidationState(account *Account) string {
	bps := make(map[string]any)
	for key, value := range account.Extra {
		if strings.HasPrefix(key, "openai_excel_bps") {
			bps[key] = value
		}
	}
	raw, err := json.Marshal(map[string]any{
		"platform": account.Platform, "type": account.Type,
		"parent_account_id": account.ParentAccountID, "vendor_id": account.VendorID,
		"credentials": account.Credentials, "bps": bps,
	})
	if err != nil {
		return ""
	}
	return string(raw)
}
