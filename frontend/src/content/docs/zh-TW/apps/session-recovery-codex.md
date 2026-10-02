恢復既有本機工作階段，不等於修復網路或還原已刪除內容。Claude Code 請使用 [對應工具](/apps/session-recovery-claude)。

## 準備與選擇方式

先在 [API 金鑰](/keys) 複製目前群組的有效設定，確認新工作階段可用。401、503、餘額不足與上游錯誤需要排查設定或伺服器，歷史工具無法解決。請保留原始工作階段目錄。

需要 Python 3.11+ 與已安裝的 Codex CLI。[下載恢復工具](/downloads/kdan-codex-session-repair.zip)，解壓縮後進入 kdan-codex-session-repair 目錄。支援 macOS、Linux 與 Windows。

## 找回工作階段並產生恢復命令

先執行唯讀掃描。工具直接檢查 sessions 與 archived_sessions 的記錄，不依賴 SQLite 索引；不會輸出訊息內文或 Key。報告包含工作階段 ID、本機路徑及恢復命令，分享前請先檢查。

```bash
bash repair-sessions.sh --list
```


```powershell
.\RepairSessions.ps1 -List
```


指定報告中的 ID；專案已搬移時提供目前路徑。以下命令僅預覽，不啟動用戶端：

```bash
bash repair-sessions.sh --resume "SESSION_ID" --project-dir "/path/to/project"
```


```powershell
.\RepairSessions.ps1 -Resume "SESSION_ID" -ProjectDir "C:\path\to\project"
```


預覽使用目前 config.toml（含選取的 profile）中的 Provider 與模型，以單次 CLI 參數覆寫舊工作階段設定。可用 --provider 指定已設定的 Provider ID、--model 指定目前 Key 可用的模型；ID 大小寫必須相符。若設定缺漏或無效，請先修正設定。

確認命令中的 Provider、模型及目錄正確後，加上 --run 啟動該工作階段；工具不會自動傳送提示詞。繼續對話會產生正常 API 用量。這是 CLI 恢復方式，不會批次改寫桌面端 Provider、歷史或封存狀態。

## 索引路徑失效時再修復

若本機 JSONL 仍在，但用戶端索引指向舊目錄，執行以下診斷。預設使用 CODEX_HOME 或 ~/.codex；用 --codex-home 指定其他目錄。有多個 state_*.sqlite 時，用 --database 明確指定目前用戶端的資料庫，不依檔名猜測。

```bash
bash repair-sessions.sh --dry-run
```


確認 repairable_rollout_paths 中的候選正確，完全退出使用該目錄的 Codex 桌面端、CLI 與 IDE，再執行：

```bash
bash repair-sessions.sh --apply --client-closed
```


```powershell
.\RepairSessions.ps1 -Apply -ClientClosed
```


工具依 JSONL 中的工作階段 ID 驗證唯一候選，支援重新命名與跨系統遷移後的路徑修復。寫入前建立一致的 SQLite 備份，只修改 rollout_path；有重複候選、符號連結、未知資料表結構或資料庫損壞時不會猜測。

## 驗證與回復

檢查 repaired_rollout_paths；0 表示沒有修改，不表示工作階段已恢復。重新執行診斷並在用戶端開啟工作階段驗證。回復先預覽，再退出用戶端並執行：

```bash
bash repair-sessions.sh --rollback "/path/from/backup/report"
bash repair-sessions.sh --rollback "/path/from/backup/report" --apply --client-closed
```


PowerShell 對應 -Database、-CodexHome、-Rollback、-Apply、-ClientClosed。恢復預覽對應 -Resume、-Provider、-Model、-ProjectDir、-Run。回復只撤銷本次寫入且仍相符的路徑，保留後續其他修改。

無法找回已刪除、僅在另一台主機或官方雲端保存的對話；需要原主機或自己的備份。新版資料庫結構未知時僅回報，不強行修改。

[Codex CLI resume](https://developers.openai.com/codex/cli/reference/) · 2026-10-02
