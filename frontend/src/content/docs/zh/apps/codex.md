## 先准备好 Key 和模型

在 [API 密钥](/keys) 创建 Key，选定分组，再点击“使用密钥 → Codex”。优先使用控制台当前生成的配置；模型目录仅对 OpenAI/Composite 分组开放。示例模型 `gpt-5.6-sol` 仅用于演示，请替换为该 Key 实际开放的模型。已有配置先备份，合并对应字段，不要覆盖整个文件。控制台的普通 HTTP/SSE 与 WebSocket 配置是不同标签；先用普通配置验证。

本文适用于本机读取 Codex 配置的客户端。云端任务不一定读取这份配置；需要在实际执行主机单独配置。

## CLI：配置与启动

### 在线部署（macOS / Linux）

如果站点管理员已发布安装脚本，可在设置环境变量后直接部署当前用户配置：

```bash
export KDAN_API_KEY="你的 KDAN API Key"
curl -fsSL {{API_ROOT}}/install/codex.sh | bash
```

脚本会备份已有 `config.toml`、写入 Responses Provider，并设置 `600` 权限。执行前应审阅脚本内容；不要把 API Key 写进命令历史或提交到仓库。需要自定义地址时额外设置 `KDAN_BASE_URL`，模型可用 `MODEL_ID` 覆盖。

下图为KDAN OpenAI 分组的 API key 模式配置器，使用无效示例 Key 和演示地址。根据自己的分组复制实际配置；[控制台配置器](/apps/console) 展示了另一种 Legacy 模式及操作步骤，截图可点击放大。

![KDAN Codex API key 配置器（示例数据）](/docs-assets/client-codex-zh.png)

需要已安装 Codex CLI，可按 [官方 CLI 安装说明](https://developers.openai.com/codex/cli/) 安装，再运行 `codex --version` 确认。

macOS / Linux 终端：

```bash
export KDAN_API_KEY="你的 KDAN API Key"
mkdir -p "${CODEX_HOME:-$HOME/.codex}"
```

Windows PowerShell：

```powershell
$env:KDAN_API_KEY="你的 KDAN API Key"
$codexConfigDir = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $env:USERPROFILE '.codex' }
New-Item -ItemType Directory -Force $codexConfigDir | Out-Null
notepad (Join-Path $codexConfigDir 'config.toml')
```

将以下“路由分组”配置合并到 `~/.codex/config.toml`（设置了 `CODEX_HOME` 时使用该目录）。它适用于 Anthropic、Gemini、Grok 等通过 Responses 接入 Codex 的分组。顶层字段要位于所有 `[表名]` 之前，同名 Provider 表只保留一份。模型和 `base_url` 直接复制当前弹窗，不要凭示例推断。

```toml
model_provider = "gateway"
model = "gpt-5.6-sol"

[model_providers.gateway]
name = "KDAN"
base_url = "{{API_ROOT}}/v1"
env_key = "KDAN_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
```

在刚才设置环境变量的同一个终端运行 `codex`。`supports_websockets = false` 是先验证 HTTP/SSE 的配置，不代表本站没有 WebSocket 路由。

OpenAI 分组由弹窗生成另一种配置：Provider ID 是 `OpenAI`，包含 `disable_response_storage`、`network_access`、模型目录和 `[features]`。默认 **Legacy** 模式同时下载 `config.toml` 和 `auth.json`，其中 `requires_openai_auth = true`；**API key** 模式改用 `requires_openai_auth = false` 与 `experimental_bearer_token`，修改后必须完全重启 Codex。两种模式不要混合，也不要把路由分组的 `gateway` 表和 OpenAI 分组的 `OpenAI` 表拼成一个 Provider。

## 桌面端：让应用拿到 Key

桌面端和 CLI 可使用同一份配置，但 Dock、开始菜单启动的应用不会自动继承另一个终端刚设置的环境变量。仅执行 `export` 然后点击图标可能得到 401。

先完全退出已有 Codex 进程，再在设置 Key 的终端直接启动实际安装的应用可执行文件。macOS 若安装为 `/Applications/Codex.app`：

```bash
"/Applications/Codex.app/Contents/MacOS/Codex"
```

安装名称或路径不同，请在 Finder 中确认后替换。Windows 在设置 `$env:KDAN_API_KEY` 的 PowerShell 中运行实际安装的 Codex `.exe` 路径。远程主机、WSL 和容器有各自的环境与用户目录，需要在执行位置配置。

如果必须使用图标启动，可在 OpenAI 分组的“使用密钥”弹窗选择 **API key**，下载其完整 `config.toml`。它会把凭据保存在配置文件中，注意文件权限与备份，勿提交到仓库。对于其他分组，优先使用弹窗生成的 `env_key` 配置；不要自行把两种认证字段并列。

## 获取当前分组模型目录

本节只适用于 OpenAI/Composite 分组。其他路由分组不支持专用目录下载，也不应添加 `model_catalog_json`。用普通 `GET /v1/models` 查询精确 ID 后填入 `model`；不要把该列表响应保存为 Codex manifest。

在“API 密钥 → 使用密钥 → Codex”中获取并下载当前 Key 的模型目录。将 `codex-models.json` 放在固定位置，在 `config.toml` 顶层加入：

```toml
model_catalog_json = "/你的绝对路径/.codex/codex-models.json"
```

Windows TOML 使用单引号路径，例如 `model_catalog_json = 'C:\Users\你的用户名\.codex\codex-models.json'`。目录下载失败时先用最小配置排查。分组或开放模型变化后重新获取目录，不要编辑本地模型名称来绕过权限。

## 验证接入

1. 新建一个任务并发送简单问题。
2. 查看 KDAN 用量记录，确认相同时间的请求、模型和 Key。
3. 确认请求成功后再继续已有会话；遇到历史记录不见，进入 [Codex 会话恢复](/apps/session-recovery-codex)。

| 现象 | 优先检查 |
| --- | --- |
| 401 / KDAN_API_KEY_REQUIRED | 当前进程是否读取到 `env_key` 指定的变量；直接令牌是否属于当前 Provider |
| 模型不存在 | Key 分组、精确模型 ID、目录是否过期 |
| 修改配置未生效 | `CODEX_HOME`、启动 profile、项目配置覆盖、远程执行主机 |
| Speed/Fast 入口不出现 | 模型目录是否声明对应能力；价格倍率不会自动创建客户端入口 |
| 旧任务不可见 | 原项目、归档、Provider 与本地数据目录；先诊断，不删除历史 |

配置基于项目“使用密钥”生成器；字段含义见 [Codex 配置参考](https://developers.openai.com/codex/config-reference/)，核对于 2026-09-15。
