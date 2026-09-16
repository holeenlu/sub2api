## 用戶端工具

下載前先閱讀 [控制台設定器](/apps/console) 與對應的 [應用教程](/apps)，確認分組和用戶端支援範圍。

| 檔案 | 用途 | 預設行為 |
| --- | --- | --- |
| [Codex 工作階段修復包](/downloads/session-repair.zip) | macOS、Linux、Windows Codex 工作階段診斷與修復 | 唯讀診斷，顯式 Apply 才修改 |
| [Claude Code 工作階段恢復包](/downloads/claude-session-recovery.zip) | 尋找本機 Claude 工作階段並產生精確恢復命令 | 唯讀掃描，不啟動 Claude、不修改工作階段 |
| [GPT Image 2.5 Flare Skill](/downloads/gpt-image-flare.zip) | Codex 圖片生成與編輯 | 固定 Flare 模型 |
| [GPT Image 2.5 Sunburst Skill](/downloads/gpt-image-sunburst.zip) | Codex 圖片生成與編輯 | 固定 Sunburst 模型 |

## 下載後檢查

壓縮包屬於目前網站的靜態釋出資源。安裝前解壓並檢查頂層目錄中的 `README.md` 或 `SKILL.md`；腳本不應包含你的 API Key。Codex 修復包僅操作本機 Codex 資料；Claude 恢復包唯讀掃描本機 Claude 工作階段並輸出恢復命令；圖片 Skill 會使用目前 Codex Provider 傳送一次明確的計費請求。

解壓前，請使用 [SHA256SUMS.txt](/downloads/SHA256SUMS.txt) 驗證下載檔案。

不要從聊天記錄、網盤或非本站映像安裝同名腳本。升級包前保留原 Skill 目錄與 `~/.codex` 備份。
