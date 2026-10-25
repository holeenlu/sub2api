package service

import (
	"strings"
)

// BuildStatusService exposes the running build and its manual deployment policy.
// Rebuild artifacts are installed by the operator; this service performs no
// release lookup, download, binary replacement, rollback, or host update.
type BuildStatusService struct {
	currentVersion  string
	buildType       string
	upstreamVersion string
	buildCommit     string
}

func NewBuildStatusService(version, buildType string) *BuildStatusService {
	return &BuildStatusService{
		currentVersion: version,
		buildType:      buildType,
	}
}

// WithUpstreamVersion records the upstream version this build is based on.
func (s *BuildStatusService) WithUpstreamVersion(version string) *BuildStatusService {
	if s == nil {
		return nil
	}
	s.upstreamVersion = strings.TrimSpace(version)
	return s
}

// WithBuildCommit records the identity of the running binary, not a cached release.
func (s *BuildStatusService) WithBuildCommit(commit string) *BuildStatusService {
	if s == nil {
		return nil
	}
	s.buildCommit = ""
	commit = strings.TrimSpace(commit)
	if len(commit) < 7 || len(commit) > 40 {
		return s
	}
	for _, c := range commit {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return s
		}
	}
	s.buildCommit = strings.ToLower(commit)
	return s
}

// BuildStatus is the read-only status consumed by the admin UI. LatestVersion
// mirrors CurrentVersion because the application does not check remote releases.
type BuildStatus struct {
	UpdateMethod    string `json:"update_method"`
	BuildCommit     string `json:"build_commit,omitempty"`
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	HasUpdate       bool   `json:"has_update"`
	Cached          bool   `json:"cached"`
	Warning         string `json:"warning,omitempty"`
	BuildType       string `json:"build_type"`
	UpstreamVersion string `json:"upstream_version,omitempty"`
	Disabled        bool   `json:"disabled"`
}

// Status describes the running build without I/O or mutable release state.
func (s *BuildStatusService) Status() BuildStatus {
	return BuildStatus{
		UpdateMethod:    "manual",
		CurrentVersion:  s.currentVersion,
		LatestVersion:   s.currentVersion,
		HasUpdate:       false,
		Cached:          false,
		Warning:         "Automatic updates and rollback are unavailable; deploy independent rebuild artifacts manually.",
		BuildType:       s.buildType,
		UpstreamVersion: s.upstreamVersion,
		BuildCommit:     s.buildCommit,
		Disabled:        true,
	}
}
