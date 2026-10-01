## 先準備好 Key 和模型

在 [API 金鑰](/keys) 建立 Key，選定分組，再點選“使用金鑰 → Codex”。優先使用控制台目前生成的設定；模型目錄僅對 OpenAI/Composite 分組開放。範例模型 `gpt-6-astra` 僅用於演示，請替換為該 Key 實際開放的模型。已有設定先備份，合併對應欄位，不要覆蓋整個檔案。控制台的普通 HTTP/SSE 與 WebSocket 設定是不同標籤；先用普萬用字元置驗證。

OpenAI/Composite 分組開啟“使用金鑰”後會立即生成預設設定，按本地“模型限制（可選）”候選列表中的模型檔位與版本，選擇適用於 Codex 的最高檔位型號（目前為 `gpt-6-astra`），同時填入 `model` 和 `review_model`。該列表是表單的內建選項，不代表目前帳號已勾選的限制或該 Key 實際可用的模型。開啟或切換金鑰不會自動請求模型目錄，不進行後台重新整理，也不持久儲存目錄快取。

本文適用於本機讀取 Codex 設定的用戶端。雲端任務不一定讀取這份設定；需要在實際執行主機單獨設定。

## CLI：設定與啟動

### 線上部署（macOS / Linux）

如果站點管理員已釋出安裝腳本，可在設定環境變數後直接部署目前使用者設定：

```bash
export TAPMODELS_API_KEY="你的 TapModels API Key"
curl -fsSL https://tapmodels.ai/install/codex.sh | bash
```

腳本會備份已有 `config.toml`、寫入 Responses Provider，並設定 `600` 權限。執行前應審閱腳本內容；不要把 API Key 寫進命令歷史或提交到儲存庫。需要自訂地址時額外設定 `TAPMODELS_BASE_URL`，模型可用 `TAPMODELS_MODEL` 覆蓋。

下圖為本專案 OpenAI 分組的 API key 模式設定器，使用無效範例 Key 和演示地址。根據自己的分組複製實際設定；[控制台設定器](/apps/console) 展示了另一種 Legacy 模式及操作步驟，截圖可點選放大。

![本專案 Codex API key 設定器（範例資料）](/docs-assets/client-codex-zh-TW.png)

需要已安裝 Codex CLI，可按 [官方 CLI 安裝說明](https://developers.openai.com/codex/cli/) 安裝，再執行 `codex --version` 確認。

macOS / Linux 終端機：

```bash
export TAPMODELS_API_KEY="你的 TapModels API Key"
mkdir -p "${CODEX_HOME:-$HOME/.codex}"
```

Windows PowerShell：

```powershell
$env:TAPMODELS_API_KEY="你的 TapModels API Key"
$codexConfigDir = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $env:USERPROFILE '.codex' }
New-Item -ItemType Directory -Force $codexConfigDir | Out-Null
notepad (Join-Path $codexConfigDir 'config.toml')
```

將以下“路由分組”設定合併到 `~/.codex/config.toml`（設定了 `CODEX_HOME` 時使用該目錄）。它適用於 Anthropic、Gemini、Grok 等透過 Responses 接入 Codex 的分組。頂層欄位要位於所有 `[表名]` 之前，同名 Provider 表只保留一份。模型和 `base_url` 直接複製目前彈出視窗，不要憑範例推斷。

```toml
model_provider = "tapmodels"
model = "gpt-6-astra"

[model_providers.tapmodels]
name = "TapModels"
base_url = "{{API_ROOT}}/v1"
env_key = "TAPMODELS_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
```

在剛才設定環境變數的同一個終端機執行 `codex`。`supports_websockets = false` 是先驗證 HTTP/SSE 的設定，不代表本站沒有 WebSocket 路由。

OpenAI 分組由彈出視窗生成另一種設定：Provider ID 是 `OpenAI`，包含主模型、審查模型、`[features]` 和本地目錄路徑 `model_catalog_json`，複製即可使用。彈出視窗預設選中 **Codex CLI (WebSocket)** 與 **API key** 模式：`requires_openai_auth = false` 並寫入 `experimental_bearer_token`，只需下載 `config.toml`；**Legacy** 模式改為 `requires_openai_auth = true` 並同時下載 `auth.json`。切換後必須完全重啟 Codex。兩種模式不要混合，也不要把路由分組的 `tapmodels` 表和 OpenAI 分組的 `OpenAI` 表拼成一個 Provider。

## 桌面版：讓應用拿到 Key

桌面版和 CLI 可使用同一份設定，但 Dock、開始選單啟動的應用不會自動繼承另一個終端機剛設定的環境變數。僅執行 `export` 然後點選圖示可能得到 401。

先完全退出已有 Codex 程序，再在設定 Key 的終端機直接啟動實際安裝的應用執行檔。macOS 若安裝為 `/Applications/Codex.app`：

```bash
"/Applications/Codex.app/Contents/MacOS/Codex"
```

安裝名稱或路徑不同，請在 Finder 中確認後替換。Windows 在設定 `$env:TAPMODELS_API_KEY` 的 PowerShell 中執行實際安裝的 Codex `.exe` 路徑。遠端主機、WSL 和容器有各自的環境與使用者目錄，需要在執行位置設定。

如果必須使用圖示啟動，可在 OpenAI 分組的“使用金鑰”彈出視窗選擇 **API key**，下載其完整 `config.toml`。它會把認證資訊儲存在設定檔中，注意檔案權限與備份，勿提交到儲存庫。智譜分組的 Codex 設定直接把 API Key 寫入 `experimental_bearer_token`，複製 `config.toml` 即可使用；其他分組優先使用彈出視窗生成的 `env_key` 設定。不要自行把兩種驗證欄位並列。

## 目前分組模型目錄

本節適用於 OpenAI/Composite 分組和智譜 API Key 分組。其他路由分組不提供專用目錄，也不應新增 `model_catalog_url` 或 `model_catalog_json`。用普通 `GET /v1/models` 查詢精確 ID 後填入 `model`；不要把該列表回應當作 Codex manifest。

彈出視窗生成的 `config.toml` 在根級包含：

```toml
model_catalog_json = "~/.codex/codex-models.json"
```

在“取得模型目錄及下載”區域點選取得，下載 `codex-models.json` 並儲存到上述路徑，重啟 Codex 後模型列表即來自本站為該 Key 計算的目錄。OpenAI/Composite 可切換遠端目錄；智譜始終使用本地檔案，`GLM-5.3` 系列目錄與 `config.toml` 宣告 1,000,000 token 上下文；`GLM-4.7` 保留上游的 200,000 token 限制。不要編輯模型名稱來繞過權限。

## 驗證接入

1. 新建一個任務並行送簡單問題。
2. 檢視 TapModels 用量記錄，確認相同時間的請求、模型和 Key。
3. 確認請求成功後再繼續已有工作階段；遇到歷史記錄不見，進入 [Codex 工作階段恢復](/apps/session-recovery-codex)。

| 現象 | 優先檢查 |
| --- | --- |
| 401 / API_KEY_REQUIRED | 目前程序是否讀取到 `env_key` 指定的變數；直接權杖是否屬於目前 Provider |
| 模型不存在 | Key 分組、精確模型 ID、目錄是否過期 |
| 修改設定未生效 | `CODEX_HOME`、啟動 profile、專案設定覆蓋、遠端執行主機 |
| Speed/Fast 入口不出現 | 模型目錄是否宣告對應能力；價格倍率不會自動建立用戶端入口 |
| 舊任務不可見 | 原專案、封存、Provider 與本地資料目錄；先診斷，不刪除歷史 |

設定基於專案“使用金鑰”生成器；欄位含義見 [Codex 設定參考](https://developers.openai.com/codex/config-reference/)，核對於 2026-09-15。
