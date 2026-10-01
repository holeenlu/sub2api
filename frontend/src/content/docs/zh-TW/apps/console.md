## 控制台設定器

控制台的 **API 金鑰 → 使用金鑰** 是應用設定生成器。它根據 Key 的分組平台輸出可複製的終端機命令、設定檔，OpenAI/Composite 分組還可取得 Codex 模型目錄；輸出中的地址、環境變數名和模型範例應以目前彈出視窗為準。

## 建立 Key 並選擇分組

1. 開啟 [API 金鑰](/keys)，建立一把專用 Key。
2. 在分組選擇器中選擇實際要呼叫的分組。分組決定可見模型、路由協議、餘額和速率限制；只複製 Key 而不確認分組，容易得到 401 或模型不存在。
3. 建立後點擊 **使用金鑰**，選擇用戶端標籤。不要把同一段設定同時貼上到多個用戶端。
4. 使用 **複製** 或 **下載** 儲存彈出視窗顯示的原文。已有設定先備份，再合併欄位。

## 目前生成器覆蓋

| 分組平台 | 可生成的用戶端 | 主要設定 |
| --- | --- | --- |
| OpenAI | Codex CLI、Codex WebSocket、Claude Code（啟用 Messages 時）、OpenCode | `config.toml`、`auth.json` 或 `experimental_bearer_token`、Anthropic 環境變數、`opencode.json` |
| Anthropic | Claude Code、Codex（路由）、OpenCode | `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN`、Codex Responses provider、OpenCode provider |
| Gemini | Gemini CLI、Codex（路由）、OpenCode | `GOOGLE_GEMINI_BASE_URL`、`GEMINI_API_KEY`、`GEMINI_MODEL` |
| Antigravity | Claude Code、Gemini CLI、Codex（路由）、OpenCode | 地址自動追加 `/antigravity`，Gemini 使用 `/v1beta` |
| Grok | Grok CLI、Claude Code、Codex、OpenCode | `GROK_MODELS_BASE_URL`、`XAI_API_KEY` 或對應用戶端設定 |
| DeepSeek、MiniMax、Composite、Kimi、Zhipu、OpenCode | Claude Code、Codex（路由）、OpenCode | 以彈出視窗生成的分組地址為準；Codex 目錄僅 Composite 提供 |

## Codex 驗證模式

OpenAI 分組的 Codex 標籤提供兩種模式，開啟“使用金鑰”時預設選中 **Codex CLI (WebSocket)**、**API key** 和 **macOS / Linux**：

- **Legacy**：`config.toml` 使用 `requires_openai_auth = true`，並下載 `auth.json`。只在 Codex 版本需要該登入形態時使用。
- **API key**：`requires_openai_auth = false`，把 Key 寫入 `experimental_bearer_token`，並附加本地圖片擴充套件所需的請求標頭。該模式會把金鑰儲存在磁碟，限制檔案權限且不要提交到儲存庫。

其他分組的 Codex 路由預設使用 `env_key = "TAPMODELS_API_KEY"`、`wire_api = "responses"` 和 `supports_websockets = false`；智譜分組把 API Key 寫入生成的 `experimental_bearer_token`，因此單獨複製 `config.toml` 也不會依賴 `TAPMODELS_API_KEY`。WebSocket 標籤只對 OpenAI Responses WebSocket 路徑啟用。

## 模型目錄

OpenAI/Composite 分組和智譜 API Key 分組支援 Codex 專用模型目錄。其他分組不生成 `model_catalog_url` 或 `model_catalog_json`，請查詢普通 `GET /v1/models` 後手動填寫模型 ID。

支援的 Codex 標籤生成的 `config.toml` 在根級包含 `model_catalog_json = "~/.codex/codex-models.json"`。在 **取得模型目錄及下載** 區域取得目前 Key 的目錄並下載到該路徑，重啟 Codex 後即可顯示模型列表。OpenAI/Composite 可切換遠端目錄；智譜始終使用本地檔案，`GLM-5.3` 系列目錄與 `config.toml` 宣告 1,000,000 token 上下文；`GLM-4.7` 保留上游的 200,000 token 限制。不要手工改模型 slug 繞過分組策略。

## 本專案介面操作

以下截圖來自目前專案的“使用金鑰”元件，使用無效範例 Key 和 `api.example.com` 演示地址。實際接入請複製自己控制台生成的值，不要抄錄圖片中的地址或 Key。

1. OpenAI 分組預設即為 **Codex CLI (WebSocket)** + API key 驗證；網路不支援 WebSocket 時改選 **Codex CLI**，需要 `auth.json` 登入形態時改選 Legacy。兩種驗證模式對應的檔案不同。

![本專案 Codex Legacy 設定器（範例資料）](/docs-assets/client-codex-zh-TW.png)

2. API key 模式會把 Key 寫入設定檔；下載後限制檔案權限，並完全重啟用戶端。

![本專案 Codex API key 設定器（範例資料）](/docs-assets/client-codex-zh-TW.png)

3. Anthropic 分組選擇 **Claude Code**，根據作業系統切換命令。複製目前標籤下完整命令，在同一個終端機執行用戶端。

![本專案 Claude Code macOS / Linux 設定器（範例資料）](/docs-assets/client-claude-zh-TW.png)

![本專案 Claude Code PowerShell 設定器（範例資料）](/docs-assets/client-claude-zh-TW.png)

## 驗證與檢查

在實際用戶端傳送一條簡單訊息，然後在控制台用量中核對時間、模型、Key 和分組。401 先檢查程序是否繼承了彈出視窗生成的環境變數；404/模型錯誤檢查地址是否重複追加 `/v1`；429 檢查分組限額和並行。桌面版從 Dock 或開始選單啟動時不會繼承另一終端機的 `export`，需要在同一終端機啟動應用或使用用戶端自己的環境設定。

用戶端連結：

- [Codex](/apps/codex)
- [Claude Code](/apps/claude-code)
- [Claude Desktop](/apps/claude-desktop)
- [圖片 Skills](/apps/image-skills)
- [Codex 工作階段恢復](/apps/session-recovery-codex)
- [Claude Code 工作階段恢復](/apps/session-recovery-claude)
