export default {
  title: '統一模型價格',
  readDenied: '讀取價格需要有效工作階段、models.read 和 billing.cost.read 權限。', unit: '單位：美元 / 百萬 token。圖片價格按百萬圖片 token 計。',
  search: '搜尋模型 ID', refresh: '重新讀取價格', sync: '同步價格', loading: '正在讀取價格…', empty: '沒有符合的模型價格。',
  model: '模型', source: '來源', version: '價格版本：{version}', synced: '最近同步：{time}', details: '完整價格明細',
  edit: '覆寫價格', cancel: '取消', save: '儲存價格', reset: '移除覆寫', saved: '命令完成，已重新讀取價格。',
  input: '輸入', output: '輸出', cache_read: '快取讀取', cache_5m: '快取寫入（5 分鐘）', cache_1h: '快取寫入（1 小時）',
  unknown: '未知 / 缺失', unpriced: '未定價（null）', priced: '設定價格', inherit: '繼承 / 缺失', custom: '自訂價格',
  fallback: '伺服器端補值（null）', noImage: '無獨立圖片價格（null）',
  tiers: '上下文階梯', above: '超過 token 數', addTier: '新增階梯', remove: '刪除', modes: '服務模式', image: '圖片 token 價格',
  stale: '價格版本已變更。草稿已保留，請關閉後重新開啟模型，以最新版本編輯。',
  reloadRequired: '再次操作前請重新讀取價格。草稿已保留，請核對伺服器端狀態後重試。',
  working: '正在執行命令…', invalidDraft: '每組已定價基礎欄位的輸入、輸出、快取讀取均必填，且為 0 到 1e9 的有限數；選填快取寫入可缺失或為 null。階梯最多 32 個，門檻為互不重複的正整數。',
  errors: {
    conflict: '價格版本衝突。草稿已保留，請重新讀取並核對最新值。',
    denied: '權限不足，請核對 models.read、billing.cost.read、models.manage 和 billing.rates.update。',
    unavailable: '暫時無法取得模型價格。', invalid: '價格版本或價格資料無效或缺失。',
    request: '請求失敗，執行結果可能未知；重試前請重新讀取價格。',
    refresh: '命令已接受，但重新讀取失敗。草稿已保留，再次操作前請重新讀取。'
  }
}
