## 先准备好 Key 和模型

在 [API 密钥](/keys) 创建 Key，选定分组，再点击“使用密钥 → Codex”。优先使用控制台当前生成的配置与模型目录。示例模型 `gpt-5.6-sol` 仅用于演示，请替换为该 Key 实际开放的模型。已有配置先备份，合并对应字段，不要覆盖整个文件。

本文适用于本机读取 Codex 配置的客户端。云端任务不一定读取这份配置；需要在实际执行主机单独配置。

## CLI：配置与启动

需要已安装 Codex CLI，可按 [官方 CLI 安装说明](https://developers.openai.com/codex/cli/) 安装，再运行 `codex --version` 确认。

macOS / Linux 终端：

```bash
export TAPMODELS_API_KEY="你的 TapModels API Key"
mkdir -p "${CODEX_HOME:-$HOME/.codex}"
```

Windows PowerShell：

```powershell
$env:TAPMODELS_API_KEY="你的 TapModels API Key"
$codexConfigDir = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $env:USERPROFILE '.codex' }
New-Item -ItemType Directory -Force $codexConfigDir | Out-Null
notepad (Join-Path $codexConfigDir 'config.toml')
```

将以下配置合并到 `~/.codex/config.toml`（设置了 `CODEX_HOME` 时使用该目录）。顶层字段要位于所有 `[表名]` 之前。同名 Provider 表只保留一份。

```toml
model_provider = "tapmodels"
model = "gpt-5.6-sol"

[model_providers.tapmodels]
name = "TapModels"
base_url = "{{API_ROOT}}/v1"
env_key = "TAPMODELS_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
```

在刚才设置环境变量的同一个终端运行 `codex`。`supports_websockets = false` 是先验证 HTTP/SSE 的配置，不代表本站没有 WebSocket 路由。不要修改 `auth.json` 来切换本站 Key。

## 桌面端：让应用拿到 Key

桌面端和 CLI 可使用同一份配置，但 Dock、开始菜单启动的应用不会自动继承另一个终端刚设置的环境变量。仅执行 `export` 然后点击图标可能得到 401。

先完全退出已有 Codex 进程，再在设置 Key 的终端直接启动实际安装的应用可执行文件。macOS 若安装为 `/Applications/Codex.app`：

```bash
"/Applications/Codex.app/Contents/MacOS/Codex"
```

安装名称或路径不同，请在 Finder 中确认后替换。Windows 在设置 `$env:TAPMODELS_API_KEY` 的 PowerShell 中运行实际安装的 Codex `.exe` 路径。远程主机、WSL 和容器有各自的环境与用户目录，需要在执行位置配置。

如果必须使用图标启动，可按控制台“使用密钥”提供的直接令牌模式，将当前 Provider 的 `env_key` **替换为** `experimental_bearer_token = "你的 Key"`；两种方式只选一种。它将凭据保存在配置文件中，注意文件权限与备份，勿提交到仓库。这是官方支持但不推荐的回退方式。

## 获取当前分组模型目录

在“API 密钥 → 使用密钥 → Codex”中获取并下载当前 Key 的模型目录。将 `codex-models.json` 放在固定位置，在 `config.toml` 顶层加入：

```toml
model_catalog_json = "/你的绝对路径/.codex/codex-models.json"
```

Windows TOML 使用单引号路径，例如 `model_catalog_json = 'C:\Users\你的用户名\.codex\codex-models.json'`。目录下载失败时先用最小配置排查。分组或开放模型变化后重新获取目录，不要编辑本地模型名称来绕过权限。

## 验证接入

1. 新建一个任务并发送简单问题。
2. 查看 TapModels 用量记录，确认相同时间的请求、模型和 Key。
3. 确认请求成功后再继续已有会话；遇到历史记录不见，进入 [会话恢复](/docs/apps/session-recovery)。

| 现象 | 优先检查 |
| --- | --- |
| 401 / API_KEY_REQUIRED | 当前进程是否读取到 `env_key` 指定的变量；直接令牌是否属于当前 Provider |
| 模型不存在 | Key 分组、精确模型 ID、目录是否过期 |
| 修改配置未生效 | `CODEX_HOME`、启动 profile、项目配置覆盖、远程执行主机 |
| Speed/Fast 入口不出现 | 模型目录是否声明对应能力；价格倍率不会自动创建客户端入口 |
| 旧任务不可见 | 原项目、归档、Provider 与本地数据目录；先诊断，不删除历史 |

配置基于项目“使用密钥”生成器；字段含义见 [Codex 配置参考](https://developers.openai.com/codex/config-reference/)，核对于 2026-09-15。
