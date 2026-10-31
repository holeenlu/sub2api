package service

import "context"

// GitHubReleaseClient provides release metadata for Claude Code and Codex
// version synchronization. It does not install or download application updates.
type GitHubReleaseClient interface {
	FetchLatestRelease(ctx context.Context, repo string) (*GitHubRelease, error)
	FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*GitHubRelease, error)
}

// GitHubRelease contains only the metadata used to select stable CLI versions.
type GitHubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}
