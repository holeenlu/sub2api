## 选择你正在使用的 Claude

| 客户端 | 配置方式 |
| --- | --- |
| Claude Code CLI | 本页环境变量或用户 `settings.json` |
| VS Code 中的 Claude Code | 编辑器用户配置中的环境变量，见下文 |
| 支持 Third-Party Inference 的 Claude 桌面端 | [Claude 桌面端教程](/apps/claude-desktop) |
| claude.ai 网页聊天 | 本页不会把网页账号聊天切换为 KDAN Key |

先在 [API 密钥](/keys) 为目标分组创建 Key。项目支持 Messages 请求，最终是否允许该客户端、模型及辅助请求由分组策略决定。

## CLI 配置

### 在线部署（macOS / Linux）

```bash
export KDAN_API_KEY="你的 KDAN API Key"
curl -fsSL {{API_ROOT}}/install/claude-code.sh | bash
```

脚本会备份 `~/.claude/settings.json` 后写入 Messages 环境变量并限制权限。执行前请审阅脚本；需要自定义地址时设置 `KDAN_BASE_URL`。

下图来自KDAN“使用密钥 → Claude Code”，使用无效示例 Key 与演示地址。选择与你操作系统一致的标签，并复制自己控制台里的值；[配置器教程](/apps/console) 也提供了 PowerShell 截图。

![KDAN Claude Code 配置器（示例数据）](/docs-assets/client-claude-zh.png)

已安装 Claude Code 后运行 `claude --version`。安装步骤见 [Claude Code 官方说明](https://code.claude.com/docs/en/setup)。

macOS / Linux：

```bash
export ANTHROPIC_BASE_URL="{{API_ROOT}}"
export ANTHROPIC_AUTH_TOKEN="你的 KDAN API Key"
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
claude --model claude-sonnet-5
```

Windows PowerShell：

```powershell
$env:ANTHROPIC_BASE_URL="{{API_ROOT}}"
$env:ANTHROPIC_AUTH_TOKEN="你的 KDAN API Key"
$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1"
claude --model claude-sonnet-5
```

将示例模型替换为当前 Key 开放的 ID。Base URL 填根地址，客户端自行追加 `/v1/messages`。Token 填原始 Key，不加 `Bearer `。清理自己旧配置中冲突的凭据来源前先备份，不必删除已保存的官方登录。

## 持久化与 IDE

需要每次启动生效时，备份并合并 `~/.claude/settings.json` 中的 `env`：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "{{API_ROOT}}",
    "ANTHROPIC_AUTH_TOKEN": "你的 KDAN API Key",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"
  }
}
```

该文件含私密凭据。不要写进共享的项目 `.claude/settings.json`。`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` 是当前控制台生成器用于减少登录、遥测等非必要外连的设置，不会把 Claude 网页、Remote Control 或语音功能改为由 KDAN 提供。GUI 启动的 IDE 不一定继承终端环境。VS Code 扩展可在用户 Settings JSON 中设置：

```json
{
  "claudeCode.environmentVariables": [
    { "name": "ANTHROPIC_BASE_URL", "value": "{{API_ROOT}}" },
    { "name": "ANTHROPIC_AUTH_TOKEN", "value": "你的 KDAN API Key" },
    { "name": "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "value": "1" }
  ]
}
```

## 验证与模型选择

运行 `/status` 检查 Base URL 和凭据来源，再通过 `/model claude-sonnet-5` 选择分组开放的精确 ID。发送一次简单问题，并在 KDAN 用量记录核对。不要假设 `/model` 一定自动列出 `GET /v1/models` 的所有条目。

Claude Code 可能为标题、摘要和 token 计数发送辅助请求。主模型调用正常但辅助功能失败时，需核对分组白名单或模型映射，不能仅换 Key 解决。

## 切换后找回会话

回到原来的项目目录，使用 `claude --resume` 选择会话；`claude --continue` 继续当前目录最近的会话，也可在客户端使用 `/resume`。确认操作系统用户、项目路径和配置目录没有变化。仍找不到时进入 [Claude Code 会话恢复](/apps/session-recovery-claude)，不要使用 Codex 修复脚本处理 Claude 数据。

| 错误 | 处理 |
| --- | --- |
| 401 | 核对 Key、env/settings 的覆盖关系、`/status` 的凭据来源 |
| 404 / 模型错误 | 检查 Base URL 与分组模型，不要重复拼接 `/v1/messages` |
| 429 | 等待限流窗口，核对余额、并发与分组配额 |
| 仍提示账号登录 | 确认启动的是 Claude Code，变量传到当前进程；桌面端使用独立教程 |

来源：[官方网关接入](https://code.claude.com/docs/en/llm-gateway-connect)、[恢复会话](https://code.claude.com/docs/en/common-workflows#resume-previous-conversations)，项目配置生成器核对于 2026-09-15。
