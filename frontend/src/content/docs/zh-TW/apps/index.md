## 從 API Key 到應用

KDAN 提供 OpenAI Responses、Chat Completions、Anthropic Messages 與影像相容介面。第三方應用的欄位名稱不同，但都需要三項真實設定：控制台顯示的 API 基礎網址、KDAN API Key、目前 Key 分組開放的模型 ID。

| 應用 | 協議 | 基礎網址 | 驗證方式 |
| --- | --- | --- | --- |
| Codex 桌面版 / CLI | Responses | 控制台 API 地址，保留 `/v1` | 按設定器選擇 Legacy、API key 或路由分組環境變數 |
| Claude Code | Messages | 控制台 API 根地址，不追加 `/v1/messages` | `ANTHROPIC_AUTH_TOKEN` |
| OpenAI SDK | OpenAI 相容 | 控制台 API 地址，保留 `/v1` | Bearer API Key |

先看 [控制台設定器](/apps/console) 瞭解目前 Key 分組可生成哪些用戶端設定，再進入 [Codex](/apps/codex)、[Claude Code](/apps/claude-code) 或 [Claude Desktop](/apps/claude-desktop) 的完整教程。圖片能力使用獨立的 [圖片 Skills](/apps/image-skills)。

## 建立專用 Key

在 [API 金鑰](/keys) 點選建立，選擇你實際要使用的分組。下面為KDAN控制台截圖，選項以目前版本為準。

![KDAN 建立 API 金鑰：選擇分組與限額](/docs-assets/create-api-key.png)

## 推薦接入順序

1. 在控制台建立 Key，並記下它綁定的分組。
2. 使用同一把 Key 請求 `GET /v1/models`，複製回傳的精確模型 ID。
3. 按應用教程設定 Base URL、Key 和模型。
4. 完全退出並重啟應用，傳送一條新工作階段測試。
5. 在控制台用量記錄中確認請求、模型與費用。

切換後列表為空可能與專案、封存、Provider 篩選、資料目錄或索引路徑有關。按用戶端進入 [Codex 工作階段恢復](/apps/session-recovery-codex) 或 [Claude Code 工作階段恢復](/apps/session-recovery-claude)，先執行只讀診斷，再決定是否應用修復。所有可下載檔案與驗證值集中在 [下載](/apps/downloads)。
