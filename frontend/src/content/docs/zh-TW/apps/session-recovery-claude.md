此工具用於 Claude Code 的本機 JSONL 工作階段。Codex 請使用 [Codex 恢復工具](/apps/session-recovery-codex)；官方雲端歷史需要回到原帳號或主機。

## 先排除連線問題

用目前 Key 與模型建立新工作階段。401、503、模型不可用或餘額不足屬於連線、權限或伺服器問題，無法靠工作階段掃描解決。若只有舊記錄找不到，再依下列步驟處理。

## 直接恢復原始記錄

先執行 claude --resume 尋找工作階段。已知 ID 時可指定 ID；跨專案或目錄遷移後，也可使用完整 JSONL 路徑，避免選擇器範圍造成找不到記錄：

```bash
claude --resume "/absolute/path/to/SESSION_ID.jsonl"
```


請在要繼續工作的專案目錄執行。路徑必須是實際存在的本機檔案，不能直接使用範例文字。用戶端須支援以絕對檔案路徑恢復；舊版請先更新。

## 下載與掃描

需要 Python 3.10+。[下載 Claude Code 恢復工具](/downloads/tapmodels-claude-session-recovery.zip)，解壓縮後進入 tapmodels-claude-session-recovery 目錄：

```bash
bash find-claude-sessions.sh
```


```powershell
.\Find-ClaudeSessions.ps1
```


掃描預設使用 CLAUDE_CONFIG_DIR 或 ~/.claude。工具核對檔名 UUID 與記錄中的 sessionId，輸出已驗證的絕對路徑及恢復命令，不輸出訊息內文或 Key，不修改記錄，也不會自動啟動 Claude。

## 專案遷移或舊設定目錄

掃描另一份歷史時，以 --claude-home 指定包含 projects 的設定目錄；專案已搬移時，用 --project-dir 指定目前存在的目錄：

```bash
bash find-claude-sessions.sh --claude-home "/path/to/.claude" --project-dir "/path/to/project"
```


```powershell
.\Find-ClaudeSessions.ps1 -ClaudeHome "C:\path\to\.claude" -ProjectDir "C:\path\to\project"
```


PowerShell 使用 -ClaudeHome 與 -ProjectDir。原工作目錄不存在且未指定新目錄時，工具會提示補充目錄，不再產生無法執行的 cd 命令。

產生的命令會設定 CLAUDE_CONFIG_DIR，並以 JSONL 絕對路徑恢復。請先確認掃描目錄中的 settings.json 對應目前要使用的服務商與憑證。若要保留目前的用戶端設定，也可在已設定的終端手動執行 claude --resume 並加上報告中的絕對檔案路徑。

## 執行與驗證

將報告中的命令複製到對應 shell 執行。看到原對話後，再傳送明確的新訊息驗證呼叫；繼續對話會產生 API 用量。PowerShell 命令在切換專案失敗時會停止。

此工具無法還原刪除的檔案，不會下載其他主機或雲端記錄，也不承諾將官方帳號的雲端歷史遷移至閘道。掃描為空時請檢查作業系統使用者、原主機、設定目錄及自己的備份。

[Claude Code CLI --resume](https://code.claude.com/docs/en/cli-reference) · 2026-10-02
