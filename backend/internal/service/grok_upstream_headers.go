package service

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// defaultGrokUpstreamUserAgent 使用固定版本的官方交互式 CLI UA。
// Grok upstream must not forward Claude Code / Codex / browser client UAs.
func defaultGrokUpstreamUserAgent() string {
	return xai.CLIUserAgent(xai.ResolveCLIVersion())
}
