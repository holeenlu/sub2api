恢复现有本机会话，不等于修复网络或恢复已删除内容。Claude Code 请使用 [对应工具](/apps/session-recovery-claude)。

## 准备与选择路径

先在 [API 密钥](/keys) 复制当前分组的有效配置，确认新会话能使用。401、503、余额不足和上游错误需要排查配置或服务端，历史工具不能解决。请保留原始会话目录。

需要 Python 3.11+ 和已安装的 Codex CLI。[下载恢复工具](/downloads/tapmodels-codex-session-repair.zip)，解压后进入 tapmodels-codex-session-repair 目录。工具支持 macOS、Linux 和 Windows。

## 找回会话并生成恢复命令

先运行只读扫描。它直接检查 sessions 与 archived_sessions 中的记录，不依赖 SQLite 索引；不会输出消息正文或 Key。报告包含会话 ID、本机路径和恢复命令，分享前请检查。

```bash
bash repair-sessions.sh --list
```


```powershell
.\Repair-TapModelsSessions.ps1 -List
```


指定报告中的 ID；项目已搬家时提供当前路径。以下命令只预览，不启动客户端：

```bash
bash repair-sessions.sh --resume "SESSION_ID" --project-dir "/path/to/project"
```


```powershell
.\Repair-TapModelsSessions.ps1 -Resume "SESSION_ID" -ProjectDir "C:\path\to\project"
```


预览使用当前 config.toml（包括所选 profile）中的 Provider 和模型，以单次 CLI 参数覆盖旧会话配置。可用 --provider 指定已配置的 Provider ID、--model 指定当前 Key 可用的模型；ID 大小写必须一致。若配置本身缺失或无效，先修正配置。

确认命令中的 Provider、模型和目录正确后，加 --run 启动该会话；工具不会自动发送提示词。继续对话会产生正常 API 用量。这是 CLI 恢复路径，不会批量改写桌面端 Provider、历史或归档状态。

## 索引路径失效时再修复

若本地 JSONL 还在，但客户端索引指向旧目录，运行以下诊断。默认读取 CODEX_HOME 或 ~/.codex；用 --codex-home 指定其他目录。存在多个 state_*.sqlite 时，用 --database 明确指定当前客户端的数据库，不按文件名猜测。

```bash
bash repair-sessions.sh --dry-run
```


确认 repairable_rollout_paths 中的候选正确，完全退出使用该目录的 Codex 桌面端、CLI 和 IDE，再执行：

```bash
bash repair-sessions.sh --apply --client-closed
```


```powershell
.\Repair-TapModelsSessions.ps1 -Apply -ClientClosed
```


工具按 JSONL 中的会话 ID 验证唯一候选，支持文件重命名和跨系统迁移后的路径修复。写入前生成一致 SQLite 备份，只修改 rollout_path；存在重复候选、符号链接、未知表结构或数据库损坏时拒绝猜测。

## 验证与回滚

检查 repaired_rollout_paths；0 表示没有修改，不表示会话已经恢复。重新运行诊断并在客户端打开会话验证。回滚先预览，再退出客户端并执行：

```bash
bash repair-sessions.sh --rollback "/path/from/backup/report"
bash repair-sessions.sh --rollback "/path/from/backup/report" --apply --client-closed
```


PowerShell 对应 -Database、-CodexHome、-Rollback、-Apply、-ClientClosed。恢复预览对应 -Resume、-Provider、-Model、-ProjectDir、-Run。回滚仅撤销本次写入且仍匹配的路径，保留后续其他修改。

无法找回已删除、仅在另一台主机或官方云端保存的对话；需要原主机或自己的备份。新版本数据库结构未知时仅报告，不强行修改。

[Codex CLI resume](https://developers.openai.com/codex/cli/reference/) · 2026-10-02
