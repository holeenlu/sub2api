## 支援範圍

本教程適用於具有 **Third-Party Inference** 設定入口的 Claude 桌面版。其閘道器設定獨立於 Claude Code CLI；不要期望 `ANTHROPIC_BASE_URL` 或 `~/.claude/settings.json` 自動改變桌面版連線。

本文按官方桌面版文件與 TapModels Messages 路由整理；尚未完成真實桌面版到線上 Key 的端到端驗證。沒有該入口的用戶端，請使用 [Claude Code](/apps/claude-code)。

## 設定步驟

1. 開啟 Help → Troubleshooting → Enable Developer Mode，按用戶端提示重啟。
2. 開啟 Developer → Configure Third-Party Inference。
3. Inference provider 選擇 **Gateway**，填寫下表。
4. 按用戶端提示儲存或應用，重新開啟本地工作階段，傳送測試問題。

| 欄位 | 填寫值 |
| --- | --- |
| Gateway base URL | `{{API_ROOT}}` |
| Credential kind | Static API key |
| Gateway API key | 目前 TapModels Key |
| Gateway auth scheme | Bearer（專案亦支援 x-api-key） |
| Model | 目前 Key 分組開放、支援 Messages 的模型 ID |

組織下發的設定可能使表單只讀，需聯絡組織管理員；不要用本地腳本繞過它。不要將 TapModels Key 填到官方 OAuth 或 OIDC 登入欄位。

## 驗證與常見問題

先確認模型選擇，再發送一個簡單請求，在 TapModels 用量記錄核對。模型發現與具體權限以 Key 分組為準。閘道器只保證專案已實現的 Messages 能力，並不意味著桌面版所有外掛或雲端功能都可用。

提示 Gateway was unreachable 時檢查網路和根地址；401 檢查 Key；模型未出現時核對分組及用戶端的顯式模型設定。該模式的遠端和雲端功能限制見官方說明。官方帳號雲端歷史和本機閘道器工作階段不保證互相遷移，本站修復包僅適用於 Codex 本地索引。

來源：[Claude 桌面版閘道器設定](https://claude.com/docs/third-party/claude-desktop/gateway)、[各用戶端的閘道器區別](https://code.claude.com/docs/en/llm-gateway-connect#desktop-app)，核對於 2026-09-15。
