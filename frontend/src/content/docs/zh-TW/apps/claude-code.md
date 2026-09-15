## 選擇你正在使用的 Claude

| 用戶端 | 設定方式 |
| --- | --- |
| Claude Code CLI | 本頁環境變數或使用者 `settings.json` |
| VS Code 中的 Claude Code | 編輯器使用者設定中的環境變數，見下文 |
| 支援 Third-Party Inference 的 Claude 桌面版 | [Claude 桌面版教程](/docs/apps/claude-desktop) |
| claude.ai 網頁聊天 | 本頁不會把網頁帳號聊天切換為 TapModels Key |

先在 [API 金鑰](/keys) 為目標分組建立 Key。專案支援 Messages 請求，最終是否允許該用戶端、模型及輔助請求由分組策略決定。

## CLI 設定

已安裝 Claude Code 後執行 `claude --version`。安裝步驟見 [Claude Code 官方說明](https://code.claude.com/docs/en/setup)。

macOS / Linux：

```bash
export ANTHROPIC_BASE_URL="{{API_ROOT}}"
export ANTHROPIC_AUTH_TOKEN="你的 TapModels API Key"
claude --model claude-sonnet-5
```

Windows PowerShell：

```powershell
$env:ANTHROPIC_BASE_URL="{{API_ROOT}}"
$env:ANTHROPIC_AUTH_TOKEN="你的 TapModels API Key"
claude --model claude-sonnet-5
```

將範例模型替換為目前 Key 開放的 ID。Base URL 填根地址，用戶端自行追加 `/v1/messages`。Token 填原始 Key，不加 `Bearer `。清理自己舊設定中衝突的認證資訊來源前先備份，不必刪除已儲存的官方登入。

## 持久化與 IDE

需要每次啟動生效時，備份併合並 `~/.claude/settings.json` 中的 `env`：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "{{API_ROOT}}",
    "ANTHROPIC_AUTH_TOKEN": "你的 TapModels API Key"
  }
}
```

該文件含私密認證資訊。不要寫進共享的專案 `.claude/settings.json`。GUI 啟動的 IDE 不一定繼承終端機環境。VS Code 擴充套件可在使用者 Settings JSON 中設定：

```json
{
  "claudeCode.environmentVariables": [
    { "name": "ANTHROPIC_BASE_URL", "value": "{{API_ROOT}}" },
    { "name": "ANTHROPIC_AUTH_TOKEN", "value": "你的 TapModels API Key" }
  ]
}
```

## 驗證與模型選擇

執行 `/status` 檢查 Base URL 和認證資訊來源，再透過 `/model claude-sonnet-5` 選擇分組開放的精確 ID。傳送一次簡單問題，並在 TapModels 用量記錄核對。不要假設 `/model` 一定自動列出 `GET /v1/models` 的所有項目。

Claude Code 可能為標題、摘要和 token 計數傳送輔助請求。主模型呼叫正常但輔助功能失敗時，需核對分組白名單或模型對應，不能僅換 Key 解決。

## 切換後找回工作階段

回到原來的專案目錄，使用 `claude --resume` 選擇工作階段；`claude --continue` 繼續目前目錄最近的工作階段，也可在用戶端使用 `/resume`。確認作業系統使用者、專案路徑和設定目錄沒有變化。本站 Codex 恢復腳本不適用於 Claude 資料。

| 錯誤 | 處理 |
| --- | --- |
| 401 | 核對 Key、env/settings 的覆蓋關係、`/status` 的認證資訊來源 |
| 404 / 模型錯誤 | 檢查 Base URL 與分組模型，不要重複拼接 `/v1/messages` |
| 429 | 等待速率限制視窗，核對餘額、並行與分組配額 |
| 仍提示帳號登入 | 確認啟動的是 Claude Code，變數傳到目前程序；桌面版使用獨立教程 |

來源：[官方閘道器接入](https://code.claude.com/docs/en/llm-gateway-connect)、[恢復工作階段](https://code.claude.com/docs/en/common-workflows#resume-previous-conversations)，專案設定生成器核對於 2026-09-15。
