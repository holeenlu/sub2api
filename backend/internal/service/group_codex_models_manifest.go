package service

// Retained only for old persisted rows and auth-cache payloads. No runtime path
// reads or fetches pinned sources; discovery aggregates every group member.
func RetiredCodexModelsManifestConfig(cfg GroupCodexModelsManifestConfig) GroupCodexModelsManifestConfig {
	cfg.Enabled = false
	cfg.FallbackToScheduler = false
	return cfg
}
