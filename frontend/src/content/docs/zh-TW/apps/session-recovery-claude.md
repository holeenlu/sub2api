Codex 使用者請使用 [Codex 工作階段恢復](/apps/session-recovery-codex)。Claude Code 工作階段不是 Codex SQLite 資料，不能用 Codex 修復工具處理。

## 先判斷是哪一種“丟失”

| 現象 | 優先處理 |
| --- | --- |
| 目前專案的選擇器為空 | 回到原專案執行 `claude --resume`；按 `Ctrl+W` 或 `Ctrl+A` 擴大範圍 |
| 換作業系統使用者、主機或 `CLAUDE_CONFIG_DIR` 後不見 | 找回原來的 Claude 設定目錄；本機工作階段不會自動跨主機同步 |
| 專案或 worktree 路徑改變 | 恢復原工作目錄，再按工作階段 ID 恢復 |
| 切換 TapModels Key 或登入帳號後不見 | Key 不會搬走本地檔案；檢查實際 OS 使用者、設定目錄和專案路徑 |
| 檔案已刪除、過期或只存在雲端 | 本地掃描工具無法重建，需使用原主機、原用戶端或備份 |

Claude Code CLI 會把工作階段儲存在本機 `~/.claude/projects/<專案>/<工作階段ID>.jsonl`；設定 `CLAUDE_CONFIG_DIR` 後則儲存在對應目錄。預設情況下，超過 30 天的本地記錄可能被清理。

## 先用 Claude Code 找回

回到原專案目錄執行：

```bash
claude --resume
```

`claude --continue` 會恢復目前目錄最近一次工作階段；工作階段內可用 `/resume`。選擇器預設顯示目前專案，按 `Ctrl+W` 查看同一儲存庫的其他 worktree，按 `Ctrl+A` 查看本機全部專案。知道 ID 時使用：

```bash
claude --resume <工作階段ID>
```

## 下載唯讀恢復工具

[下載 TapModels Claude Code 工作階段恢復包](/downloads/tapmodels-claude-session-recovery.zip)。macOS / Linux：

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

腳本只列出檔名 UUID 與檔案內 `sessionId` 一致的記錄，並輸出帶原工作目錄的 `claude --resume <工作階段ID>` 命令。它不會啟動 Claude、讀取或輸出訊息正文，也不會修改 JSONL、帳號、Key 或設定。報告包含本機路徑與工作階段 ID，分享前檢查隱私。

## 掃描其他目錄與恢復

掃描舊設定目錄：

```bash
bash find-claude-sessions.sh --claude-home "/原來的/.claude"
```

PowerShell 使用 `-ClaudeHome`。若報告提示原工作目錄不存在，先恢復原目錄或把專案放回該路徑，再手動執行產生的命令。開啟工作階段本身不會傳送訊息；繼續輸入並傳送後會產生模型請求。

工具不能重建已刪除、已過期、另一台主機或雲端專屬工作階段，也不會改變 Claude 的保留週期或帳號歸屬。

依據：[Claude Code 工作階段管理](https://code.claude.com/docs/en/sessions)、[Claude 本機資料目錄](https://code.claude.com/docs/en/claude-directory)。工具經過臨時夾具測試，沒有修改本機真實 Claude 工作階段檔案。
