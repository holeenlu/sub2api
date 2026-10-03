## 先准备好 Key 和模型

在 [API 密钥](/keys) 选择要使用的 Key，点击“使用密钥 → Codex”。分组决定开放模型与接入范围，优先复制当前弹窗生成的配置。本文型号仅为示例，不保证当前 Key 可用。已有配置先备份，再合并所需字段。

主模型与审查模型由当前 Key 的配置资料提供；获取模型目录后，可按目录中的可用型号更新选择。不要把文档示例、前端候选项或厂商全部型号视为这把 Key 的授权清单。

本文适用于本机读取 Codex 配置的客户端。云端任务不一定读取这份配置；需要在实际执行主机单独配置。

## CLI：配置与启动

### 在线部署（macOS / Linux）

如果站点管理员已发布安装脚本，可在设置环境变量后直接部署当前用户配置：

```bash
export TOKENSAVY_API_KEY="你的 Tokensavy API Key"
curl -fsSL https://tokensavy.ai/install/codex.sh | bash
```

脚本会备份已有 `config.toml`、写入 Responses Provider，并设置 `600` 权限。执行前应审阅脚本内容；不要把 API Key 写进命令历史或提交到仓库。需要自定义地址时额外设置 `TOKENSAVY_BASE_URL`，模型可用 `TOKENSAVY_MODEL` 覆盖。

下图为 Tokensavy OpenAI 分组的 API key 模式配置器，使用无效示例 Key 和演示地址。根据自己的分组复制实际配置；[控制台配置器](/apps/console) 说明认证模式与操作步骤，截图可点击放大。

![Tokensavy Codex API key 配置器（示例数据）](/docs-assets/client-codex-zh.png)

需要已安装 Codex CLI，可按 [官方 CLI 安装说明](https://developers.openai.com/codex/cli/) 安装，再运行 `codex --version` 确认。

macOS / Linux 终端：

```bash
export TOKENSAVY_API_KEY="你的 Tokensavy API Key"
mkdir -p "${CODEX_HOME:-$HOME/.codex}"
```

Windows PowerShell：

```powershell
$env:TOKENSAVY_API_KEY="你的 Tokensavy API Key"
$codexConfigDir = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $env:USERPROFILE '.codex' }
New-Item -ItemType Directory -Force $codexConfigDir | Out-Null
notepad (Join-Path $codexConfigDir 'config.toml')
```

将以下“路由分组”配置合并到 `~/.codex/config.toml`（设置了 `CODEX_HOME` 时使用该目录）。它适用于 Anthropic、Gemini、Grok 等通过 Responses 接入 Codex 的分组。顶层字段要位于所有 `[表名]` 之前，同名 Provider 表只保留一份。模型和 `base_url` 直接复制当前弹窗，不要凭示例推断。

```toml
model_provider = "tokensavy"
model = "gpt-6-astra"

[model_providers.tokensavy]
name = "Tokensavy"
base_url = "{{API_ROOT}}/v1"
env_key = "TOKENSAVY_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
```

在刚才设置环境变量的同一个终端运行 `codex`。`supports_websockets = false` 是先验证 HTTP/SSE 的配置，不代表本站没有 WebSocket 路由。

OpenAI 分组的配置使用弹窗显示的 Provider ID。该 ID 区分大小写，必须与 model_providers 下的表名一致。API key 模式使用直接令牌；Legacy 模式同时需要对应的 auth.json。模型目录若选本地文件，还必须下载目录 JSON，不能只保存 config.toml。切换配置后完全重启客户端。

## 桌面端：让应用拿到 Key

桌面端和 CLI 可使用同一份配置，但 Dock、开始菜单启动的应用不会自动继承另一个终端刚设置的环境变量。仅执行 `export` 然后点击图标可能得到 401。

先完全退出已有 Codex 进程，再在设置 Key 的终端直接启动实际安装的应用可执行文件。macOS 若安装为 `/Applications/Codex.app`：

```bash
"/Applications/Codex.app/Contents/MacOS/Codex"
```

安装名称或路径不同，请在 Finder 中确认后替换。Windows 在设置 `$env:TOKENSAVY_API_KEY` 的 PowerShell 中运行实际安装的 Codex `.exe` 路径。远程主机、WSL 和容器有各自的环境与用户目录，需要在执行位置配置。

如果必须使用图标启动，可在 OpenAI 分组的“使用密钥”弹窗选择 **API key**，下载其完整 `config.toml`。它会把凭据保存在配置文件中，注意文件权限与备份，勿提交到仓库。智谱分组的 Codex 配置直接把 API Key 写入 `experimental_bearer_token`，复制 `config.toml` 即可使用；其他分组优先使用弹窗生成的 `env_key` 配置。不要自行把两种认证字段并列。

## 当前分组模型目录

若当前 Codex 标签提供模型目录，按弹窗所选方式配置：

- **本地文件**：获取并下载 codex-models.json，保存到生成配置的 model_catalog_json 所指路径。修改文件名或 CODEX_HOME 后须同步修改路径。
- **远程目录**：客户端和分组支持时，可使用弹窗生成的 model_catalog_url，由客户端请求当前 Key 的目录。不要自行改变该字段在配置中的位置。

不支持目录的标签使用该分组开放的精确模型 ID。模型目录、账号可调度状态和请求协议是不同条件；列表可见不保证所有工具与接口可用。配置来源以当前弹窗为准，不把普通模型列表 JSON 当作 Codex 专用目录。

## 验证接入

1. 新建一个任务并发送简单问题。
2. 查看 Tokensavy 用量记录，确认相同时间的请求、模型和 Key。
3. 确认请求成功后再继续已有会话；遇到历史记录不见，进入 [Codex 会话恢复](/apps/session-recovery-codex)。

| 现象 | 优先检查 |
| --- | --- |
| 401 / API_KEY_REQUIRED | 当前进程是否读取到 `env_key` 指定的变量；直接令牌是否属于当前 Provider |
| 模型不存在 | Key 分组、精确模型 ID、目录是否过期 |
| 修改配置未生效 | `CODEX_HOME`、启动 profile、项目配置覆盖、远程执行主机 |
| Speed/Fast 入口不出现 | 模型目录是否声明对应能力；价格倍率不会自动创建客户端入口 |
| 旧任务不可见 | 原项目、归档、Provider 与本地数据目录；先诊断，不删除历史 |

配置由当前“使用密钥”窗口生成。字段说明见 [Codex 配置参考](https://developers.openai.com/codex/config-reference/)。
