## 先準備好 Key 和模型

在 [API 金鑰](/keys) 選擇要使用的 Key，點選“使用金鑰 → Codex”。分組決定開放模型與接入範圍，優先複製目前彈出視窗生成的設定。本文型號僅為範例，不保證目前 Key 可用。已有設定先備份，再合併所需欄位。

主模型與審查模型由目前 Key 的設定資料提供；取得模型目錄後，可按目錄中的可用型號更新選擇。不要把文件範例、前端候選項或廠商全部型號視為這把 Key 的授權清單。

本文適用於本機讀取 Codex 設定的用戶端。雲端任務不一定讀取這份設定；需要在實際執行主機單獨設定。

## CLI：設定與啟動

### 線上部署（macOS / Linux）

如果站點管理員已釋出安裝腳本，可在設定環境變數後直接部署目前使用者設定：

```bash
export TOKENSAVY_API_KEY="你的 Tokensavy API Key"
curl -fsSL https://tokensavy.ai/install/codex.sh | bash
```

腳本會備份已有 `config.toml`、寫入 Responses Provider，並設定 `600` 權限。執行前應審閱腳本內容；不要把 API Key 寫進命令歷史或提交到儲存庫。需要自訂地址時額外設定 `TOKENSAVY_BASE_URL`，模型可用 `TOKENSAVY_MODEL` 覆蓋。

下圖為 Tokensavy OpenAI 分組的 API key 模式設定器，使用無效範例 Key 和演示地址。根據自己的分組複製實際設定；[控制台設定器](/apps/console) 說明驗證模式與操作步驟，截圖可點選放大。

![Tokensavy Codex API key 設定器（範例資料）](/docs-assets/client-codex-zh-TW.png)

需要已安裝 Codex CLI，可按 [官方 CLI 安裝說明](https://developers.openai.com/codex/cli/) 安裝，再執行 `codex --version` 確認。

macOS / Linux 終端機：

```bash
export TOKENSAVY_API_KEY="你的 Tokensavy API Key"
mkdir -p "${CODEX_HOME:-$HOME/.codex}"
```

Windows PowerShell：

```powershell
$env:TOKENSAVY_API_KEY="你的 Tokensavy API Key"
$codexConfigDir = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $env:USERPROFILE '.codex' }
New-Item -ItemType Directory -Force $codexConfigDir | Out-Null
notepad (Join-Path $codexConfigDir 'config.toml')
```

將以下“路由分組”設定合併到 `~/.codex/config.toml`（設定了 `CODEX_HOME` 時使用該目錄）。它適用於 Anthropic、Gemini、Grok 等透過 Responses 接入 Codex 的分組。頂層欄位要位於所有 `[表名]` 之前，同名 Provider 表只保留一份。模型和 `base_url` 直接複製目前彈出視窗，不要憑範例推斷。

```toml
model_provider = "tokensavy"
model = "gpt-6-astra"

[model_providers.tokensavy]
name = "Tokensavy"
base_url = "{{API_ROOT}}/v1"
env_key = "TOKENSAVY_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
```

在剛才設定環境變數的同一個終端機執行 `codex`。`supports_websockets = false` 是先驗證 HTTP/SSE 的設定，不代表本站沒有 WebSocket 路由。

OpenAI 分組的設定使用彈出視窗顯示的 Provider ID。該 ID 區分大小寫，必須與 model_providers 下的表名一致。API key 模式使用直接權杖；Legacy 模式同時需要對應的 auth.json。模型目錄若選本地檔案，還必須下載目錄 JSON，不能只儲存 config.toml。切換設定後完全重啟用戶端。

## 桌面版：讓應用拿到 Key

桌面版和 CLI 可使用同一份設定，但 Dock、開始選單啟動的應用不會自動繼承另一個終端機剛設定的環境變數。僅執行 `export` 然後點選圖示可能得到 401。

先完全退出已有 Codex 程序，再在設定 Key 的終端機直接啟動實際安裝的應用執行檔。macOS 若安裝為 `/Applications/Codex.app`：

```bash
"/Applications/Codex.app/Contents/MacOS/Codex"
```

安裝名稱或路徑不同，請在 Finder 中確認後替換。Windows 在設定 `$env:TOKENSAVY_API_KEY` 的 PowerShell 中執行實際安裝的 Codex `.exe` 路徑。遠端主機、WSL 和容器有各自的環境與使用者目錄，需要在執行位置設定。

如果必須使用圖示啟動，可在 OpenAI 分組的“使用金鑰”彈出視窗選擇 **API key**，下載其完整 `config.toml`。它會把認證資訊儲存在設定檔中，注意檔案權限與備份，勿提交到儲存庫。智譜分組的 Codex 設定直接把 API Key 寫入 `experimental_bearer_token`，複製 `config.toml` 即可使用；其他分組優先使用彈出視窗生成的 `env_key` 設定。不要自行把兩種驗證欄位並列。

## 目前分組模型目錄

若目前 Codex 標籤提供模型目錄，按彈出視窗所選方式設定：

- **本地檔案**：取得並下載 codex-models.json，儲存到生成設定的 model_catalog_json 所指路徑。修改檔名或 CODEX_HOME 後須同步修改路徑。
- **遠端目錄**：用戶端和分組支援時，可使用彈出視窗生成的 model_catalog_url，由用戶端請求目前 Key 的目錄。不要自行改變該欄位在設定中的位置。

不支援目錄的標籤使用該分組開放的精確模型 ID。模型目錄、帳號可排程狀態和請求協議是不同條件；列表可見不保證所有工具與介面可用。設定來源以目前彈出視窗為準，不把普通模型列表 JSON 當作 Codex 專用目錄。

## OpenAI 分組的網頁搜尋

OpenAI 分組的 Codex 設定啟用原生獨立搜尋：用戶端透過 `web.run` 呼叫本站 `/v1/alpha/search`，由閘道器既有的帳號選擇、驗證與計費流程處理。模型對話繼續使用原有的 Responses HTTP 或 WebSocket 連線。

既有設定需在對應的 Provider 表及 `[features]` 表分別合併以下欄位，不要重複建立表：

```toml
[model_providers.OpenAI]
supports_standalone_web_search = true

[features]
standalone_web_search = true
```

已使用 Codex CLI 0.160.1 驗證。該用戶端將獨立搜尋標記為開發中功能，啟動提示不代表搜尋失敗。儲存後完全退出並重新啟動用戶端，再重新開啟任務。明確設定 `web_search = "disabled"` 仍會關閉搜尋。其他平台分組不自動啟用此 OpenAI 專用介面。

若出現 `Hosted tool 'web_search' requires authorization and metering ... rustponsesapi`，先檢查兩個開關是否由實際執行的程序載入；不要藉由移除搜尋工具後重試來隱藏錯誤。搜尋權限與計費仍由上游帳號決定，不會由這兩個用戶端開關繞過。

## 驗證接入

1. 新建一個任務並行送簡單問題。
2. 檢視 Tokensavy 用量記錄，確認相同時間的請求、模型和 Key。
3. 確認請求成功後再繼續已有工作階段；遇到歷史記錄不見，進入 [Codex 工作階段恢復](/apps/session-recovery-codex)。

| 現象 | 優先檢查 |
| --- | --- |
| 401 / API_KEY_REQUIRED | 目前程序是否讀取到 `env_key` 指定的變數；直接權杖是否屬於目前 Provider |
| 模型不存在 | Key 分組、精確模型 ID、目錄是否過期 |
| 修改設定未生效 | `CODEX_HOME`、啟動 profile、專案設定覆蓋、遠端執行主機 |
| Speed/Fast 入口不出現 | 模型目錄是否宣告對應能力；價格倍率不會自動建立用戶端入口 |
| 舊任務不可見 | 原專案、封存、Provider 與本地資料目錄；先診斷，不刪除歷史 |

設定由目前“使用金鑰”視窗生成。欄位說明見 [Codex 設定參考](https://developers.openai.com/codex/config-reference/)。
