// 此檔案由 tools/zh-tw/gen-locale.mjs 依 locales/zh 自動產生，請勿手動修改。
// 詞彙修正請改 tools/zh-tw/convert.mjs（CORRECTIONS / TW_VOCAB），逐句修正請改 gen-locale.mjs 的 OVERRIDES。
export default {
  "refresh": "重新整理",
  "authorization": {
    "primary": "目前使用帳號的主 Excel 授權，請在帳號 OAuth 設定中管理",
    "title": "Excel 獨立授權",
    "hint": "請授權同一 ChatGPT 使用者和工作區。Excel 憑證獨立加密儲存，不會覆蓋 Codex 憑證。",
    "ready": "已儲存 Excel 授權",
    "required": "需要 Excel 授權",
    "refreshRequired": "下次 BPS 請求會重新整理憑證",
    "authorize": "生成授權連結",
    "openLogin": "開啟官方 Excel 登入頁",
    "callback": "登入後貼上完整回呼網址",
    "save": "儲存 Excel 授權",
    "revoke": "移除 Excel 授權",
    "importHint": "貼上包含 client_id、access_token、refresh_token 和 id_token 的 Excel OAuth 憑證 JSON。不能通過修改 client_id 將 Codex 憑證轉換為 Excel 憑證。",
    "failed": "Excel 授權失敗",
    "invalidCallback": "請貼上 bps.openai.com 的完整回呼網址",
    "import": "匯入 Excel 憑證"
  },
  "globalEnabled": "啟用 BPS 閘道器",
  "globalHint": "全站執行總開關。開啟後，僅帳號中啟用並選定的模型走 BPS；帳號策略在帳號設定中儲存。圖片處理預算仍由功能設定統一管理。"
}
