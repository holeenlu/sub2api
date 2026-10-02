package service

import (
	"context"
	"fmt"
	"net/http"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// maxCodexModelsManifestAccounts bounds the number of accounts that may be
// used as a deterministic Codex manifest source for one group.
const maxCodexModelsManifestAccounts = 10

// normalizeCodexModelsManifestConfig keeps the persisted selection stable
// while making the runtime contract explicit: only OpenAI groups can enable
// this source. IDs are deduplicated in selection order so the first account
// still wins when manifests contain the same slug.
func normalizeCodexModelsManifestConfig(platform string, cfg GroupCodexModelsManifestConfig) GroupCodexModelsManifestConfig {
	if platform != PlatformOpenAI {
		return GroupCodexModelsManifestConfig{}
	}
	out := GroupCodexModelsManifestConfig{
		Enabled:             cfg.Enabled,
		FallbackToScheduler: cfg.FallbackToScheduler,
	}
	if len(cfg.AccountIDs) == 0 {
		return out
	}
	seen := make(map[int64]struct{}, len(cfg.AccountIDs))
	out.AccountIDs = make([]int64, 0, len(cfg.AccountIDs))
	for _, id := range cfg.AccountIDs {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out.AccountIDs = append(out.AccountIDs, id)
	}
	if len(out.AccountIDs) == 0 {
		out.AccountIDs = nil
	}
	return out
}

// validateCodexModelsManifestConfig checks enabled selections against the
// current group membership. Disabled configurations keep their selected IDs
// so turning the source back on does not silently erase the saved choice.
func (s *adminServiceImpl) validateCodexModelsManifestConfig(ctx context.Context, groupID int64, cfg GroupCodexModelsManifestConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if len(cfg.AccountIDs) == 0 {
		return infraerrors.New(http.StatusBadRequest, "INVALID_CODEX_MODELS_MANIFEST_CONFIG", "codex models manifest config requires at least one account")
	}
	if len(cfg.AccountIDs) > maxCodexModelsManifestAccounts {
		return infraerrors.Newf(http.StatusBadRequest, "INVALID_CODEX_MODELS_MANIFEST_CONFIG", "codex models manifest config allows at most %d accounts, got %d", maxCodexModelsManifestAccounts, len(cfg.AccountIDs))
	}
	if s.accountRepo == nil {
		return infraerrors.New(http.StatusInternalServerError, "CODEX_MODELS_MANIFEST_VALIDATION_UNAVAILABLE", "account repository is unavailable")
	}
	accounts, err := s.accountRepo.ListByGroup(ctx, groupID)
	if err != nil {
		return fmt.Errorf("load group accounts for codex models manifest config: %w", err)
	}
	members := make(map[int64]struct{}, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		if account.IsActive() && account.Platform == PlatformOpenAI {
			members[account.ID] = struct{}{}
		}
	}
	invalid := make([]int64, 0)
	for _, id := range cfg.AccountIDs {
		if _, ok := members[id]; !ok {
			invalid = append(invalid, id)
		}
	}
	if len(invalid) > 0 {
		return infraerrors.Newf(http.StatusBadRequest, "INVALID_CODEX_MODELS_MANIFEST_CONFIG", "codex models manifest config contains accounts not in this group or not active openai accounts: %v", invalid)
	}
	return nil
}
