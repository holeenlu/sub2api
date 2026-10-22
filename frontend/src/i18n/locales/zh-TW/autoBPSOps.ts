// 此檔案由 tools/zh-tw/gen-locale.mjs 依 locales/zh 自動產生，請勿手動修改。
// 詞彙修正請改 tools/zh-tw/convert.mjs（CORRECTIONS / TW_VOCAB），逐句修正請改 gen-locale.mjs 的 OVERRIDES。
export default {
  "defaults": {
    "title": "Excel / BPS 預設設定",
    "subtitle": "在帳號裡選擇“使用預設設定”開啟 BPS 時，填入這裡儲存的模型和選項。",
    "scopeHint": "這裡只儲存預設範本。帳號內提供“使用預設設定”和“使用初始設定”兩種開啟方式；新建、匯入和同步帳號不會自動開啟 BPS。",
    "models": "預設使用的模型",
    "options": "預設勾選的選項",
    "reset": "恢復推薦選項",
    "advanced": "更多選項",
    "advancedHint": "工具與 403 恢復",
    "ignore_encrypted_contentHint": "跳過 BPS 無法讀取的歷史加密內容，保留明文與工具呼叫關係。",
    "auto_disable_on_403Hint": "收到符合條件的上游 403 時關閉 BPS，帳號仍可使用原 Codex 通道。",
    "cache_creation_as_inputHint": "快取建立 token 按普通輸入單價計費，上游快取行為不變。",
    "omit_unsupported_toolsHint": "省略 BPS 不支援的託管工具；即時搜尋和圖片生成將不可用。",
    "auto_recover_on_403Hint": "需要開啟 403 自動關閉。按間隔傳送文字探測，成功後重新開啟 BPS，會消耗少量額度。",
    "auto_move_on_403Hint": "遇到符合條件的 403 時移入目標分組；可選擇退出全部分組。",
    "modelsRequired": "請至少選擇一個預設 BPS 模型，或啟用全部模型。",
    "applying": "讀取預設設定…",
    "loadFailed": "預設設定讀取失敗，原設定未改動。請重試。",
    "manage": "管理預設設定",
    "mode": {
      "defaults": "使用預設設定開啟",
      "defaultsHint": "開啟時填入已儲存的預設模型和勾選項，可繼續調整。",
      "initial": "使用初始設定開啟",
      "initialHint": "開啟時使用原始模型選項，附加功能不勾選，再自行設定。",
      "defaultsApplied": "已填入預設設定，儲存帳號後生效。",
      "initialApplied": "已填入初始設定，可自行勾選，儲存帳號後生效。"
    },
    "modeHint": "兩種方式互斥；切換會重新填入模型和選項，關閉目前開關即關閉 BPS。",
    "adminOnly": "預設範本僅管理員可用，仍可使用初始設定並手動調整。",
    "save": "儲存範本",
    "saved": "BPS 範本已儲存，現有帳號未被修改。",
    "saveFailed": "範本儲存失敗，請重試。"
  },
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
  "globalHint": "預設關閉。開啟後，只有在帳號中設定的模型會透過 BPS；WS 用戶端使用 HTTP/SSE 橋接。"
}
