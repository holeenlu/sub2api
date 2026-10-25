package service

import (
	"strings"
)

// BuildStatusService exposes the running build metadata.
// Rebuild artifacts are installed by the operator; this service performs no
// release lookup, download, binary replacement, rollback, or host update.
type BuildStatusService struct {
	currentVersion  string
	upstreamVersion string
	buildCommit     string
}

func NewBuildStatusService(version string) *BuildStatusService {
	return &BuildStatusService{
		currentVersion: version,
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

// BuildStatus contains only the running build metadata consumed by the UI.
type BuildStatus struct {
	CurrentVersion  string
	BuildCommit     string
	UpstreamVersion string
}

// Status describes the running build without I/O or mutable release state.
func (s *BuildStatusService) Status() BuildStatus {
	return BuildStatus{
		CurrentVersion:  s.currentVersion,
		UpstreamVersion: s.upstreamVersion,
		BuildCommit:     s.buildCommit,
	}
}
