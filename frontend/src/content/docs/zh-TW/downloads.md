## 用戶端工具

| 檔案 | 用途 | 預設行為 |
| --- | --- | --- |
| [TapModels Codex 工作階段修復包](/downloads/tapmodels-codex-session-repair.zip) | macOS、Linux、Windows 工作階段診斷與修復 | 只讀診斷，顯式 Apply 才修改 |
| [GPT Image 2.5 Flare Skill](/downloads/gpt-image-flare.zip) | Codex 圖片生成與編輯 | 固定 Flare 模型 |
| [GPT Image 2.5 Sunburst Skill](/downloads/gpt-image-sunburst.zip) | Codex 圖片生成與編輯 | 固定 Sunburst 模型 |

## 下載後檢查

壓縮包屬於目前網站的靜態釋出資源。安裝前解壓並檢查頂層目錄中的 `README.md` 或 `SKILL.md`；腳本不應包含你的 API Key。工作階段修復包僅操作本機 Codex 資料，圖片 Skill 會使用目前 Codex Provider 傳送一次明確的計費請求。

解壓前，請使用 [SHA256SUMS.txt](/downloads/SHA256SUMS.txt) 驗證下載檔案。

不要從聊天記錄、網盤或非本站映像安裝同名腳本。升級包前保留原 Skill 目錄與 `~/.codex` 備份。
