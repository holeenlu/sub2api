## 控制台配置器

控制台的 **API 密钥 → 使用密钥** 是应用配置生成器。它根据 Key 的分组平台输出可复制的终端命令、配置文件，OpenAI/Composite 分组还可获取 Codex 模型目录；输出中的地址、环境变量名和模型示例应以当前弹窗为准。

## 创建 Key 并选择分组

1. 打开 [API 密钥](/keys)，创建一把专用 Key。
2. 在分组选择器中选择实际要调用的分组。分组决定可见模型、路由协议、余额和限流；只复制 Key 而不确认分组，容易得到 401 或模型不存在。
3. 创建后点击 **使用密钥**，选择客户端标签。不要把同一段配置同时粘贴到多个客户端。
4. 使用 **复制** 或 **下载** 保存弹窗显示的原文。已有配置先备份，再合并字段。

## 当前生成器覆盖

| 分组平台 | 可生成的客户端 | 主要配置 |
| --- | --- | --- |
| OpenAI | Codex CLI、Codex WebSocket、Claude Code（启用 Messages 时）、OpenCode | `config.toml`、`auth.json` 或 `experimental_bearer_token`、Anthropic 环境变量、`opencode.json` |
| Anthropic | Claude Code、Codex（路由）、OpenCode | `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN`、Codex Responses provider、OpenCode provider |
| Gemini | Gemini CLI、Codex（路由）、OpenCode | `GOOGLE_GEMINI_BASE_URL`、`GEMINI_API_KEY`、`GEMINI_MODEL` |
| Antigravity | Claude Code、Gemini CLI、Codex（路由）、OpenCode | 地址自动追加 `/antigravity`，Gemini 使用 `/v1beta` |
| Grok | Grok CLI、Claude Code、Codex、OpenCode | `GROK_MODELS_BASE_URL`、`XAI_API_KEY` 或对应客户端配置 |
| DeepSeek、MiniMax、Composite、Kimi、Zhipu、OpenCode | Claude Code、Codex（路由）、OpenCode | 以弹窗生成的分组地址为准；Codex 目录仅 Composite 提供 |

## Codex 认证模式

OpenAI 分组的 Codex 标签提供两种模式，打开“使用密钥”时默认选中 **Codex CLI (WebSocket)**、**API key** 和 **macOS / Linux**：

- **Legacy**：`config.toml` 使用 `requires_openai_auth = true`，并下载 `auth.json`。只在 Codex 版本需要该登录形态时使用。
- **API key**：`requires_openai_auth = false`，把 Key 写入 `experimental_bearer_token`，并附加本地图片扩展所需的请求头。该模式会把密钥保存在磁盘，限制文件权限且不要提交到仓库。

其他分组的 Codex 路由默认使用 `env_key = "TAPMODELS_API_KEY"`、`wire_api = "responses"` 和 `supports_websockets = false`。WebSocket 标签只对 OpenAI Responses WebSocket 路径启用。

## 模型目录

仅 OpenAI/Composite 分组支持 Codex 专用模型目录。其他分组不生成 `model_catalog_url`，请查询普通 `GET /v1/models` 后手动填写模型 ID。

支持的 Codex 标签生成的 `config.toml` 在 Provider 中包含 `model_catalog_url`，Codex 0.156.0 及以上版本启动时直接读取本站为该 Key 计算的目录，无需下载文件。**检查连接 / 查看此 Key 可用模型** 只用于核对，不是使用前置步骤。本站已停止支持本地目录文件：旧配置中的 `model_catalog_json` 请删除后重新复制配置。不要手工改模型 slug 绕过分组策略。

## 本项目界面操作

以下截图来自当前项目的“使用密钥”组件，使用无效示例 Key 和 `api.example.com` 演示地址。实际接入请复制自己控制台生成的值，不要抄录图片中的地址或 Key。

1. OpenAI 分组默认即为 **Codex CLI (WebSocket)** + API key 认证；网络不支持 WebSocket 时改选 **Codex CLI**，需要 `auth.json` 登录形态时改选 Legacy。两种认证模式对应的文件不同。

![本项目 Codex Legacy 配置器（示例数据）](/docs-assets/client-codex-zh.png)

2. API key 模式会把 Key 写入配置文件；下载后限制文件权限，并完全重启客户端。

![本项目 Codex API key 配置器（示例数据）](/docs-assets/client-codex-zh.png)

3. Anthropic 分组选择 **Claude Code**，根据操作系统切换命令。复制当前标签下完整命令，在同一个终端运行客户端。

![本项目 Claude Code macOS / Linux 配置器（示例数据）](/docs-assets/client-claude-zh.png)

![本项目 Claude Code PowerShell 配置器（示例数据）](/docs-assets/client-claude-zh.png)

## 验证与排查

在实际客户端发送一条简单消息，然后在控制台用量中核对时间、模型、Key 和分组。401 先检查进程是否继承了弹窗生成的环境变量；404/模型错误检查地址是否重复追加 `/v1`；429 检查分组限额和并发。桌面端从 Dock 或开始菜单启动时不会继承另一终端的 `export`，需要在同一终端启动应用或使用客户端自己的环境配置。

客户端链接：

- [Codex](/apps/codex)
- [Claude Code](/apps/claude-code)
- [Claude Desktop](/apps/claude-desktop)
- [图片 Skills](/apps/image-skills)
- [Codex 会话恢复](/apps/session-recovery-codex)
- [Claude Code 会话恢复](/apps/session-recovery-claude)
