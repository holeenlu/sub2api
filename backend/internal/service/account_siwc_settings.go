package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

func validateSIWCAccountSettings(account *Account) error {
	if account == nil {
		return nil
	}
	raw, exists := account.Extra[OpenAISIWCEnabledKey]
	if !exists {
		return nil
	}
	enabled, ok := raw.(bool)
	if !ok {
		return infraerrors.BadRequest("INVALID_OPENAI_SIWC", "openai_siwc must be a boolean")
	}
	if enabled && !account.IsSIWCEligible() {
		return infraerrors.BadRequest("INVALID_OPENAI_SIWC_ACCOUNT", "SIWC requires an administrator-owned ordinary OpenAI OAuth parent account")
	}
	return nil
}
