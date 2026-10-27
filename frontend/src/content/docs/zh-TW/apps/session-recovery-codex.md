恢復現有本機工作階段，不等於修復網路或恢復已刪除內容。Claude Code 請使用 [對應工具](/apps/session-recovery-claude)。

## 準備與選擇路徑

先在 [API 金鑰](/keys) 複製目前分組的有效設定，確認新工作階段能使用。401、503、餘額不足和上游錯誤需要檢查設定或伺服器端，歷史工具不能解決。請保留原始工作階段目錄。

需要 Python 3.11+ 和已安裝的 Codex CLI。[下載恢復工具](/downloads/tapmodels-codex-session-repair.zip)，解壓後進入 tapmodels-codex-session-repair 目錄。工具支援 macOS、Linux 和 Windows。

## 找回工作階段並生成恢復命令

先執行只讀掃描。它直接檢查 sessions 與 archived_sessions 中的記錄，不依賴 SQLite 索引；不會輸出訊息正文或 Key。報告包含工作階段 ID、本機路徑和恢復命令，分享前請檢查。

```bash
bash repair-sessions.sh --list
```


```powershell
.\Repair-TapModelsSessions.ps1 -List
```


指定報告中的 ID；專案已搬家時提供目前路徑。以下命令只預覽，不啟動用戶端：

```bash
bash repair-sessions.sh --resume "SESSION_ID" --project-dir "/path/to/project"
```


```powershell
.\Repair-TapModelsSessions.ps1 -Resume "SESSION_ID" -ProjectDir "C:\path\to\project"
```


預覽使用目前 config.toml（包括所選 profile）中的 Provider 和模型，以單次 CLI 參數覆蓋舊工作階段設定。可用 --provider 指定已設定的 Provider ID、--model 指定目前 Key 可用的模型；ID 大小寫必須一致。若設定本身缺失或無效，先修正設定。

確認命令中的 Provider、模型和目錄正確後，加 --run 啟動該工作階段；工具不會自動傳送提示詞。繼續對話會產生正常 API 用量。這是 CLI 恢復路徑，不會批次改寫桌面版 Provider、歷史或封存狀態。

## 索引路徑失效時再修復

若本地 JSONL 還在，但用戶端索引指向舊目錄，執行以下診斷。預設讀取 CODEX_HOME 或 ~/.codex；用 --codex-home 指定其他目錄。存在多個 state_*.sqlite 時，用 --database 明確指定目前用戶端的資料庫，不按檔名猜測。

```bash
bash repair-sessions.sh --dry-run
```


確認 repairable_rollout_paths 中的候選正確，完全退出使用該目錄的 Codex 桌面版、CLI 和 IDE，再執行：

```bash
bash repair-sessions.sh --apply --client-closed
```


```powershell
.\Repair-TapModelsSessions.ps1 -Apply -ClientClosed
```


工具按 JSONL 中的工作階段 ID 驗證唯一候選，支援檔案重新命名和跨系統遷移後的路徑修復。寫入前生成一致 SQLite 備份，只修改 rollout_path；存在重複候選、符號連結、未知表結構或資料庫損壞時拒絕猜測。

## 驗證與回滾

檢查 repaired_rollout_paths；0 表示沒有修改，不表示工作階段已經恢復。重新執行診斷並在用戶端開啟工作階段驗證。回滾先預覽，再退出用戶端並執行：

```bash
bash repair-sessions.sh --rollback "/path/from/backup/report"
bash repair-sessions.sh --rollback "/path/from/backup/report" --apply --client-closed
```


PowerShell 對應 -Database、-CodexHome、-Rollback、-Apply、-ClientClosed。恢復預覽對應 -Resume、-Provider、-Model、-ProjectDir、-Run。回滾僅撤銷本次寫入且仍匹配的路徑，保留後續其他修改。

無法找回已刪除、僅在另一台主機或官方雲端儲存的對話；需要原主機或自己的備份。新版本資料庫結構未知時僅報告，不強行修改。

[Codex CLI resume](https://developers.openai.com/codex/cli/reference/) · 2026-10-02
