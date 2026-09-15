## 先判断是哪一种“丢失”

| 现象 | 优先处理 |
| --- | --- |
| 切换项目后列表空了 | 返回原项目或使用 `codex resume --all`，查看归档列表 |
| 换操作系统用户、主机或设置 CODEX_HOME 后不见 | 找回原来的数据目录；远程和本地历史分别保存 |
| 改了 Provider 标识后旧任务不显示 | 核对旧配置与当前配置，恢复原 Provider 标识再加载；见下文 |
| 能找到 JSONL，但索引路径指向旧目录 | 用本站工具诊断，满足条件时可修复路径 |
| 官方账号云端任务或文件被删除 | 本地索引修复包无法恢复；回到原账号或使用自己的备份 |

切换 Key 与更换客户端账号、Provider 名称、数据目录是不同操作。不要通过删除 `auth.json`、SQLite 或整个 `.codex` 目录排障。

## 先用客户端找回

Codex CLI 可在原项目目录运行 `codex resume`，或运行 `codex resume --all` 查找其他目录的会话；知道 ID 时使用 `codex resume <会话ID>`。桌面端检查原项目和已归档任务。不要把继续任务的命令放进自动批处理，因为打开后发送新消息会发起模型请求。

Claude Code 用 `claude --resume`、`claude --continue` 或 `/resume`；其数据不是 Codex 数据，不能用下面脚本处理。

## Provider 切换的处理

若只是把 `model_provider = "旧名称"` 改成新名称，先在旧配置备份中核对旧 Provider 的 ID、模型、地址及凭据来源。在同一个旧 Provider 配置项内更新正确的 TapModels 地址/Key，并恢复顶层原 ID，再重启尝试打开旧会话。不要为了显示历史把新的 Key 发往旧服务商地址。

不同客户端版本对账号/Provider 的过滤不同，此步骤不是通用保证。本站工具只报告 Provider 分布，不批量改写 Provider，也不把一个官方账号的数据归给另一个账号。仍不可见时保留报告联系支持。

## 下载与只读诊断

要求 Python 3.10+（图像 Skill 需要 3.11+）。[下载恢复工具](/downloads/tapmodels-codex-session-repair.zip)，解压后进入 `tapmodels-codex-session-repair` 目录。

macOS / Linux：

```bash
bash repair-sessions.sh --dry-run
```

Windows PowerShell：

```powershell
.\Repair-TapModelsSessions.ps1 -DryRun
```

脚本读取实际 `CODEX_HOME`，默认 `~/.codex`。如需要指定，使用 `--codex-home "/实际目录"`，PowerShell 对应 `-CodexHome`。它自动发现唯一的 `state_*.sqlite`，存在多个时不猜测；确认当前客户端使用的文件后通过 `--database state_5.sqlite`（示例文件名）或 `-Database` 指定。

报告包含数据库完整性、结构、会话/归档数量、Provider 分布及缺失路径，不输出会话正文或 API Key。报告仍含本机路径与会话 ID，分享前检查隐私。

## 修复已验证的索引路径

先阅读报告。完全退出使用该目录的 Codex 桌面端、CLI、IDE 进程后执行：

```bash
bash repair-sessions.sh --apply --client-closed
```

```powershell
.\Repair-TapModelsSessions.ps1 -Apply -ClientClosed
```

工具仅在会话目录内找到唯一候选，且 JSONL 首条 `session_meta.payload.id` 与索引会话 ID 完全一致时修改 `rollout_path`。它先取得写锁，再完成最终扫描；锁定期间通过另一只只读连接生成一致 SQLite 备份，并在提交前校验数据库全部表、列及数据。数据库、会话与备份路径均拒绝 `..`、任一路径组件中的符号链接，以及操作中被替换的文件。任何校验失败都会回滚事务。原始对话、Key、配置、Provider 和归档标记均不修改。

没有匹配文件、结构未知、数据库损坏、候选重复或路径含符号链接时不会替你猜测。它不能从缺失的文件重建消息，也不修复任意版本的账户筛选逻辑。

## 检查结果与回滚

成功后查看 `repaired_rollout_paths`，重新执行只读诊断，再打开客户端确认历史内容。备份位于报告的 `backup` 路径。先只预览回滚，不修改数据：

```bash
bash repair-sessions.sh --rollback "/报告中的备份路径"
```

核对预览并再次完全退出所有客户端后，显式执行：

```bash
bash repair-sessions.sh --rollback "/报告中的备份路径" --apply --client-closed
```

PowerShell 对应 `-Rollback "路径" -Apply -ClientClosed`。回滚前工具会先备份当前数据库，然后使用 compare-and-set 条件，仅撤销该次修复实际写入且当前值仍匹配的路径。若路径后来已改变、备份文件被替换，或备份不属于当前 Codex home/数据库，整次回滚都会拒绝执行；新会话、Provider 变更及其他数据不会被覆盖。

依据：[Codex resume](https://developers.openai.com/codex/cli/reference/)、[Claude Code 会话恢复](https://code.claude.com/docs/en/common-workflows#resume-previous-conversations)。工具经过临时数据库测试；没有修改本机真实会话数据库。
