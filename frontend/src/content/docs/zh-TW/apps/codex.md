## 先準備好 Key 和模型

在 [API 金鑰](/keys) 建立 Key，選定分組，再點選“使用金鑰 → Codex”。優先使用控制台目前生成的設定與模型目錄。範例模型 `gpt-5.6-sol` 僅用於演示，請替換為該 Key 實際開放的模型。已有設定先備份，合併對應欄位，不要覆蓋整個檔案。

本文適用於本機讀取 Codex 設定的用戶端。雲端任務不一定讀取這份設定；需要在實際執行主機單獨設定。

## CLI：設定與啟動

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

將以下設定合併到 `~/.codex/config.toml`（設定了 `CODEX_HOME` 時使用該目錄）。頂層欄位要位於所有 `[表名]` 之前。同名 Provider 表只保留一份。

```toml
model_provider = "tapmodels"
model = "gpt-5.6-sol"

[model_providers.tapmodels]
name = "TapModels"
base_url = "{{API_ROOT}}/v1"
env_key = "TAPMODELS_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
```

在剛才設定環境變數的同一個終端機執行 `codex`。`supports_websockets = false` 是先驗證 HTTP/SSE 的設定，不代表本站沒有 WebSocket 路由。不要修改 `auth.json` 來切換本站 Key。

## 桌面版：讓應用拿到 Key

桌面版和 CLI 可使用同一份設定，但 Dock、開始選單啟動的應用不會自動繼承另一個終端機剛設定的環境變數。僅執行 `export` 然後點選圖示可能得到 401。

先完全退出已有 Codex 程序，再在設定 Key 的終端機直接啟動實際安裝的應用執行檔。macOS 若安裝為 `/Applications/Codex.app`：

```bash
"/Applications/Codex.app/Contents/MacOS/Codex"
```

安裝名稱或路徑不同，請在 Finder 中確認後替換。Windows 在設定 `$env:TAPMODELS_API_KEY` 的 PowerShell 中執行實際安裝的 Codex `.exe` 路徑。遠端主機、WSL 和容器有各自的環境與使用者目錄，需要在執行位置設定。

如果必須使用圖示啟動，可按控制台“使用金鑰”提供的直接權杖模式，將目前 Provider 的 `env_key` **替換為** `experimental_bearer_token = "你的 Key"`；兩種方式只選一種。它將認證資訊儲存在設定檔中，注意檔案權限與備份，勿提交到儲存庫。這是官方支援但不推薦的回退方式。

## 取得目前分組模型目錄

在“API 金鑰 → 使用金鑰 → Codex”中取得並下載目前 Key 的模型目錄。將 `codex-models.json` 放在固定位置，在 `config.toml` 頂層加入：

```toml
model_catalog_json = "/你的絕對路徑/.codex/codex-models.json"
```

Windows TOML 使用單引號路徑，例如 `model_catalog_json = 'C:\Users\你的使用者名稱\.codex\codex-models.json'`。目錄下載失敗時先用最小設定檢查。分組或開放模型變化後重新取得目錄，不要編輯本地模型名稱來繞過權限。

## 驗證接入

1. 新建一個任務並行送簡單問題。
2. 檢視 TapModels 用量記錄，確認相同時間的請求、模型和 Key。
3. 確認請求成功後再繼續已有工作階段；遇到歷史記錄不見，進入 [工作階段恢復](/docs/apps/session-recovery)。

| 現象 | 優先檢查 |
| --- | --- |
| 401 / API_KEY_REQUIRED | 目前程序是否讀取到 `env_key` 指定的變數；直接權杖是否屬於目前 Provider |
| 模型不存在 | Key 分組、精確模型 ID、目錄是否過期 |
| 修改設定未生效 | `CODEX_HOME`、啟動 profile、專案設定覆蓋、遠端執行主機 |
| Speed/Fast 入口不出現 | 模型目錄是否宣告對應能力；價格倍率不會自動建立用戶端入口 |
| 舊任務不可見 | 原專案、封存、Provider 與本地資料目錄；先診斷，不刪除歷史 |

設定基於專案“使用金鑰”生成器；欄位含義見 [Codex 設定參考](https://developers.openai.com/codex/config-reference/)，核對於 2026-09-15。
