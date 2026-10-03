本工具用於 Claude Code 的本地 JSONL 工作階段。Codex 請使用 [Codex 恢復工具](/apps/session-recovery-codex)；官方雲端歷史需要回到原帳號或主機。

## 先排除連線問題

用目前 Key 和模型新建一個工作階段。401、503、模型不可用或餘額不足屬於連線、權限或伺服器端問題，不能靠工作階段掃描解決。若只有舊工作階段找不到，再按下面步驟處理。

## 直接恢復原始記錄

先執行 claude --resume 查詢工作階段。已知 ID 時可指定 ID；跨專案或目錄遷移後，也可使用完整 JSONL 路徑，避免選擇器範圍導致找不到記錄：

```bash
claude --resume "/absolute/path/to/SESSION_ID.jsonl"
```


請在要繼續工作的專案目錄執行命令。這裡的路徑必須是實際存在的本地檔案，不是範例文字。用戶端需要支援按絕對檔案路徑恢復；舊版本請先更新。

## 下載與掃描

需要 Python 3.10+。[下載 Claude Code 恢復工具](/downloads/tokensavy-claude-session-recovery.zip)，解壓後進入 tokensavy-claude-session-recovery 目錄：

```bash
bash find-claude-sessions.sh
```


```powershell
.\Find-ClaudeSessions.ps1
```


掃描預設使用 CLAUDE_CONFIG_DIR 或 ~/.claude。工具核對檔名 UUID 與記錄內的 sessionId，輸出已驗證的檔案絕對路徑及恢復命令，不輸出訊息正文或 Key，不修改記錄，也不會自動啟動 Claude。

## 專案遷移或舊設定目錄

掃描另一份歷史時，--claude-home 指定包含 projects 的設定目錄；專案已搬家時，用 --project-dir 指定目前存在的目錄：

```bash
bash find-claude-sessions.sh --claude-home "/path/to/.claude" --project-dir "/path/to/project"
```


```powershell
.\Find-ClaudeSessions.ps1 -ClaudeHome "C:\path\to\.claude" -ProjectDir "C:\path\to\project"
```


PowerShell 使用 -ClaudeHome 和 -ProjectDir。原工作目錄不存在且沒有指定新目錄時，工具會提示補充目錄，不再生成無法執行的 cd 命令。

生成的命令會設定 CLAUDE_CONFIG_DIR，並使用 JSONL 絕對路徑恢復。因此請先確認掃描目錄裡的 settings.json 對應你現在要使用的服務商和認證資訊。若希望保留目前用戶端設定，也可在已設定好的終端機手動執行 claude --resume 加報告中的絕對檔案路徑。

## 執行與驗證

複製報告中的命令到對應 shell 執行。看到原對話後，再發送一條明確的新訊息驗證呼叫；繼續工作階段會產生 API 用量。PowerShell 命令在切換專案失敗時會停止。

此工具不會恢復刪除的檔案，不會下載其他主機或雲端記錄，也不承諾把官方帳號的雲端歷史遷移到閘道器。掃描為空時檢查作業系統使用者、原主機、設定目錄和自己的備份。

[Claude Code CLI --resume](https://code.claude.com/docs/en/cli-reference) · 2026-10-02
