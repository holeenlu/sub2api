## 支持范围

本教程适用于具有 **Third-Party Inference** 配置入口的 Claude 桌面端。其网关设置独立于 Claude Code CLI；不要期望 `ANTHROPIC_BASE_URL` 或 `~/.claude/settings.json` 自动改变桌面端连接。

本文按官方桌面端文档与 本项目 Messages 路由整理；尚未完成真实桌面端到线上 Key 的端到端验证。没有该入口的客户端，请使用 [Claude Code](/apps/claude-code)。

## 配置步骤

1. 打开 Help → Troubleshooting → Enable Developer Mode，按客户端提示重启。
2. 打开 Developer → Configure Third-Party Inference。
3. Inference provider 选择 **Gateway**，填写下表。
4. 按客户端提示保存或应用，重新开启本地会话，发送测试问题。

| 字段 | 填写值 |
| --- | --- |
| Gateway base URL | `{{API_ROOT}}` |
| Credential kind | Static API key |
| Gateway API key | 当前 本项目 Key |
| Gateway auth scheme | Bearer（项目亦支持 x-api-key） |
| Model | 当前 Key 分组开放、支持 Messages 的模型 ID |

组织下发的配置可能使表单只读，需联系组织管理员；不要用本地脚本绕过它。不要将 本项目 Key 填到官方 OAuth 或 OIDC 登录字段。

## 验证与常见问题

先确认模型选择，再发送一个简单请求，在 本项目 用量记录核对。模型发现与具体权限以 Key 分组为准。网关只保证项目已实现的 Messages 能力，并不意味着桌面端所有插件或云端功能都可用。

提示 Gateway was unreachable 时检查网络和根地址；401 检查 Key；模型未出现时核对分组及客户端的显式模型配置。该模式的远程和云端功能限制见官方说明。官方账号云端历史和本机网关会话不保证互相迁移，本站修复包仅适用于 Codex 本地索引。

来源：[Claude 桌面端网关配置](https://claude.com/docs/third-party/claude-desktop/gateway)、[各客户端的网关区别](https://code.claude.com/docs/en/llm-gateway-connect#desktop-app)，核对于 2026-09-15。
