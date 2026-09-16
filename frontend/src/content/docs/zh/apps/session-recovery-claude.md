Codex 用户请使用 [Codex 会话恢复](/apps/session-recovery-codex)。Claude Code 会话不是 Codex SQLite 数据，不能用 Codex 修复工具处理。

## 先判断是哪一种“丢失”

| 现象 | 优先处理 |
| --- | --- |
| 当前项目的选择器为空 | 回到原项目运行 `claude --resume`；按 `Ctrl+W` 或 `Ctrl+A` 扩大范围 |
| 换操作系统用户、主机或 `CLAUDE_CONFIG_DIR` 后不见 | 找回原来的 Claude 配置目录；本机会话不会自动跨主机同步 |
| 项目或 worktree 路径改变 | 恢复原工作目录，再按会话 ID 恢复 |
| 切换 TapModels Key 或登录账号后不见 | Key 不会搬走本地文件；检查实际 OS 用户、配置目录和项目路径 |
| 文件已删除、过期或只存在云端 | 本地扫描工具无法重建，需使用原主机、原客户端或备份 |

Claude Code CLI 会把会话保存在本机 `~/.claude/projects/<项目>/<会话ID>.jsonl`；设置 `CLAUDE_CONFIG_DIR` 后则保存在对应目录。默认情况下，超过 30 天的本地记录可能被清理。

## 先用 Claude Code 找回

回到原项目目录运行：

```bash
claude --resume
```

`claude --continue` 会恢复当前目录最近一次会话；会话内可用 `/resume`。选择器默认显示当前项目，按 `Ctrl+W` 查看同一仓库的其他 worktree，按 `Ctrl+A` 查看本机全部项目。知道 ID 时使用：

```bash
claude --resume <会话ID>
```

## 下载只读恢复工具

[下载 TapModels Claude Code 会话恢复包](/downloads/tapmodels-claude-session-recovery.zip)。macOS / Linux：

```bash
curl -fsSLO https://tapmodels.ai/downloads/tapmodels-claude-session-recovery.zip
unzip tapmodels-claude-session-recovery.zip
cd tapmodels-claude-session-recovery
bash find-claude-sessions.sh
```

Windows PowerShell：

```powershell
Invoke-WebRequest https://tapmodels.ai/downloads/tapmodels-claude-session-recovery.zip -OutFile tapmodels-claude-session-recovery.zip
Expand-Archive .\tapmodels-claude-session-recovery.zip -DestinationPath . -Force
Set-Location .\tapmodels-claude-session-recovery
.\Find-ClaudeSessions.ps1
```

脚本只列出文件名 UUID 与文件内 `sessionId` 一致的记录，并输出带原工作目录的 `claude --resume <会话ID>` 命令。它不会启动 Claude、读取或输出消息正文，也不会修改 JSONL、账号、Key 或设置。报告包含本机路径与会话 ID，分享前检查隐私。

## 扫描其他目录与恢复

扫描旧配置目录：

```bash
bash find-claude-sessions.sh --claude-home "/原来的/.claude"
```

PowerShell 使用 `-ClaudeHome`。若报告提示原工作目录不存在，先恢复原目录或把项目放回该路径，再手动运行生成的命令。打开会话本身不会发送消息；继续输入并发送后会产生模型请求。

工具不能重建已删除、已过期、另一台主机或云端专属会话，也不会改变 Claude 的保留周期或账号归属。

依据：[Claude Code 会话管理](https://code.claude.com/docs/en/sessions)、[Claude 本地数据目录](https://code.claude.com/docs/en/claude-directory)。工具经过临时夹具测试，没有修改本机真实 Claude 会话文件。
