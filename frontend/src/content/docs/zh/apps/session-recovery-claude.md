本工具用于 Claude Code 的本地 JSONL 会话。Codex 请使用 [Codex 恢复工具](/apps/session-recovery-codex)；官方云端历史需要回到原账号或主机。

## 先排除连接问题

用当前 Key 和模型新建一个会话。401、503、模型不可用或余额不足属于连接、权限或服务端问题，不能靠会话扫描解决。若只有旧会话找不到，再按下面步骤处理。

## 直接恢复原始记录

先运行 claude --resume 查找会话。已知 ID 时可指定 ID；跨项目或目录迁移后，也可使用完整 JSONL 路径，避免选择器范围导致找不到记录：

```bash
claude --resume "/absolute/path/to/SESSION_ID.jsonl"
```


请在要继续工作的项目目录运行命令。这里的路径必须是实际存在的本地文件，不是示例文字。客户端需要支持按绝对文件路径恢复；旧版本请先更新。

## 下载与扫描

需要 Python 3.10+。[下载 Claude Code 恢复工具](/downloads/tokensavy-claude-session-recovery.zip)，解压后进入 tokensavy-claude-session-recovery 目录：

```bash
bash find-claude-sessions.sh
```


```powershell
.\Find-ClaudeSessions.ps1
```


扫描默认使用 CLAUDE_CONFIG_DIR 或 ~/.claude。工具核对文件名 UUID 与记录内的 sessionId，输出已验证的文件绝对路径及恢复命令，不输出消息正文或 Key，不修改记录，也不会自动启动 Claude。

## 项目迁移或旧配置目录

扫描另一份历史时，--claude-home 指定包含 projects 的配置目录；项目已搬家时，用 --project-dir 指定当前存在的目录：

```bash
bash find-claude-sessions.sh --claude-home "/path/to/.claude" --project-dir "/path/to/project"
```


```powershell
.\Find-ClaudeSessions.ps1 -ClaudeHome "C:\path\to\.claude" -ProjectDir "C:\path\to\project"
```


PowerShell 使用 -ClaudeHome 和 -ProjectDir。原工作目录不存在且没有指定新目录时，工具会提示补充目录，不再生成无法执行的 cd 命令。

生成的命令会设置 CLAUDE_CONFIG_DIR，并使用 JSONL 绝对路径恢复。因此请先确认扫描目录里的 settings.json 对应你现在要使用的服务商和凭据。若希望保留当前客户端配置，也可在已配置好的终端手动执行 claude --resume 加报告中的绝对文件路径。

## 运行与验证

复制报告中的命令到对应 shell 执行。看到原对话后，再发送一条明确的新消息验证调用；继续会话会产生 API 用量。PowerShell 命令在切换项目失败时会停止。

此工具不会恢复删除的文件，不会下载其他主机或云端记录，也不承诺把官方账号的云端历史迁移到网关。扫描为空时检查操作系统用户、原主机、配置目录和自己的备份。

[Claude Code CLI --resume](https://code.claude.com/docs/en/cli-reference) · 2026-10-02
