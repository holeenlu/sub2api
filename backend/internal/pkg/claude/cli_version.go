package claude

import (
	"log/slog"
	"os"
	"strings"

	"golang.org/x/mod/semver"
)

// CLIVersionEnv 是身份跟踪/兼容路径的可选运维版本覆盖。
//
// 存在的理由：Anthropic 会对新模型设客户端版本下限（例如 claude-fable-5-1 要求
// claude-cli >= 2.1.251），命中时上游直接返回
// `Claude Code X.Y.Z does not support this model; version A.B.C or newer is required`。
// 此覆盖不升级生成请求的 mimic 协议模板；新模型若要求更高版本，仍须先
// 采样并升级已验证模板，不能仅换 UA 而继续使用旧 CCH/SDK/runtime。
const CLIVersionEnv = "SUB2API_CLAUDE_CLI_VERSION"

// resolvedCLIVersion 在包初始化时解析一次。
//
// ⚠️ 故意不做成"每次调用读一次环境变量"：伪装身份必须在一个进程的生命周期内保持恒定。
// User-Agent 头与请求体 billing attribution 块里的 cc_version 由不同代码路径写入，
// 若两次读到不同的值（例如进程运行中有人改了环境变量），同一个请求就会自相矛盾，
// 被上游判为非正版客户端。
var resolvedCLIVersion = resolveCLIVersion(os.Getenv(CLIVersionEnv))

// CLIVersion 返回身份跟踪/兼容路径的配置版本号（三段 semver）。
// 生成的 mimic 请求固定使用 CLICurrentVersion 的已验证 CCH/SDK 模板，
// 不跟随版本字符串覆盖；升级该模板需要同时重新验证协议。
func CLIVersion() string {
	return resolvedCLIVersion
}

// IsSupportedCLIVersion 判断运维给的覆盖值是否可用。
//
// 判据有两条，缺一不可：
//  1. 严格三段纯数字（"2.1.251"）。带 -local / -dev / +build 等后缀的版本号会被
//     identity_service 的 fingerprintUserAgentPattern 拒绝，一旦漏进去，该账号的
//     持久指纹会被写成一个不存在的客户端版本，此后所有上游请求都声称这个版本，
//     被判非正版并持续 429——而系统内没有指纹重置入口。
//  2. 不低于内置基线 CLICurrentVersion。向下覆盖没有任何使用场景，
//     却会让 identity_service 的主版本超前检查基准跟着一起降。
func IsSupportedCLIVersion(version string) bool {
	version = strings.TrimSpace(version)
	if version == "" {
		return false
	}
	// semver 允许 "v1.2" 与预发布/构建元数据，这里都不接受：
	// Canonical 相等可排除省略段，再显式排除预发布与构建元数据。
	canonical := "v" + version
	if !semver.IsValid(canonical) || semver.Canonical(canonical) != canonical {
		return false
	}
	if semver.Prerelease(canonical) != "" || semver.Build(canonical) != "" {
		return false
	}
	return semver.Compare(canonical, "v"+CLICurrentVersion) >= 0
}

// resolveCLIVersion 把环境变量的原始值解析成可用的版本号。
// 空值静默回落（未配置是正常状态）；非空但不合法则回落并告警——
// 静默忽略一个显式配置会让运维以为已经生效。
func resolveCLIVersion(raw string) string {
	version := strings.TrimSpace(raw)
	if version == "" {
		return CLICurrentVersion
	}
	if !IsSupportedCLIVersion(version) {
		slog.Warn("ignoring invalid Claude CLI version override; falling back to the built-in pin",
			"env", CLIVersionEnv,
			"value", version,
			"builtin", CLICurrentVersion,
			"requirement", "strict three-part semver, not older than the built-in pin")
		return CLICurrentVersion
	}
	return version
}
