package service

import (
	"slices"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// supplementUnmappedOpenAIModels preserves discovery for unmapped/passthrough accounts.
func supplementUnmappedOpenAIModels(accounts []Account, models []string) []string {
	if len(models) == 0 {
		return models
	}
	for i := range accounts {
		account := &accounts[i]
		if account.Platform == PlatformOpenAI && (account.IsOpenAIPassthroughEnabled() || len(account.GetModelMapping()) == 0) {
			return dedupeAndSortModelIDs(slices.Concat(models, openai.DefaultModelIDs()))
		}
	}
	return models
}

// ModelAllowlistEnabled reports the group policy; nil is disabled.
func (g *Group) ModelAllowlistEnabled() bool {
	return g != nil && g.ModelAllowlist.Enabled
}
