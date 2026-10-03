package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

func validateOpenAIAPIKeyIdentityExtra(account *Account, extra map[string]any) error {
	raw, exists := extra[OpenAIAPIKeyCodexIdentityKey]
	if !exists {
		return nil
	}
	if _, ok := raw.(bool); !ok {
		return infraerrors.BadRequest("OPENAI_APIKEY_IDENTITY_INVALID", "openai_apikey_codex_identity must be a boolean")
	}
	if account == nil || !account.IsOpenAIApiKey() {
		return infraerrors.BadRequest("OPENAI_APIKEY_IDENTITY_INVALID", "Codex identity requires an OpenAI API-key account")
	}
	return nil
}
