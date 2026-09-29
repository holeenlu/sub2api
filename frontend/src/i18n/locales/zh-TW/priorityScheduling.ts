// 此檔案由 tools/zh-tw/gen-locale.mjs 依 locales/zh 自動產生，請勿手動修改。
// 詞彙修正請改 tools/zh-tw/convert.mjs（CORRECTIONS / TW_VOCAB），逐句修正請改 gen-locale.mjs 的 OVERRIDES。
export default {

  profit: '近期利潤 / 利潤率',
  economics: { usage: '使用記錄', rate: '倍率估算', unknown: '利潤樣本不足' },
  priority: '優先順序',
  batch: {
    typeOrder: '帳號類型順序', typeOrderHint: '拖動或使用箭頭調整 Teams、Pro、Plus、API 的順序，自動生成優先順序；API 內按倍率從低到高。套用後寫入帳號，再次載入按已有優先順序還原類型順序。', moveUp: '上移 {type}', moveDown: '下移 {type}', resetOrder: '恢復 Teams → Pro → Plus → API',

    title: '按帳號類別快捷設定', hint: '批次寫入真實帳號現有欄位（不含影子帳號），優先排程關閉時也會影響帳號排程。載入後可檢視命中帳號，按 Teams、Pro、Plus 和 API 帳號倍率分檔設定；不修改計費倍率。',
    group: '限定分組 ID（可留空）', load: '載入帳號並生成排序', category: '帳號類別 / 倍率', accounts: '命中帳號', current: '目前優先順序 / 並行 / 負載因子', priority: '新優先順序',
    priorityHint: '優先順序數值越小越先使用；同一體驗檔內先比較優先順序，再比較動態得分。可手動填寫相同優先順序，讓多類帳號一起參與動態競爭。',
    concurrency: '同時設定並行數', loadFactor: '同時設定負載因子', loadHint: '並行數是實際同時請求上限；提高負載因子可提高排程頻率。即使並行數為 100、負載因子為 10000，擁堵判斷和並行槽仍按實際並行上限執行。未勾選的欄位保留原值。',
    preview: '將更新 {count} 個帳號的優先順序；並行數：{concurrency}；負載因子：{loadFactor}。', keep: '保持原值', apply: '套用到選中帳號', empty: '沒有待更新的帳號。', otherOAuth: 'OAuth · 其他方案',
    loadError: '載入帳號失敗或超過 10000 個，請限定分組後重試。', result: '已更新 {count} 個帳號。', partial: '部分帳號未完成更新，已保留在列表中，可重試。'
  },

  title: '優先排程',
  description: '先滿足體驗目標，再平衡品質、延遲、並行與成本。',
  enabled: '啟用優先排程',
  scopeNote: '適用於 OpenAI 文字請求的自由選路。工作階段綁定、協議優先順序、模型權限、速率限制與利潤門檻繼續生效；圖片、影片及其他平台沿用原排程。',
  modes: { experience: '體驗優先', balanced: '均衡', profit: '利潤優先', custom: '自訂' },
  strategy: '排程策略',
  weights: '評分權重',
  quality_weight: '品質', latency_weight: '延遲', load_weight: '並行餘量', cost_weight: '利潤 / 成本',
  thresholds: '體驗目標與統計視窗',
  target_ttft_ms: '首 token P90 目標（毫秒）', max_load_percent: '並行佔用上限（%）', min_quality_percent: '品質通過率目標（%）',
  window_minutes: '使用記錄視窗（分鐘）', min_samples: '延遲 / 利潤最少樣本數', quality_max_age_hours: '品質結果有效期（小時）',
  groupIds: '生效分組 ID', groupHint: '逗號分隔；留空表示所有分組。',
  models: '生效模型', modelHint: '每行一個請求模型名，精確匹配；留空表示所有模型。',
  rule: '排序順序：體驗達標 → 資料不足 → 體驗未達標；同檔先按優先順序（越小越先），再按策略評分。擁堵或品質不佳時，低倍率不會越過體驗分檔。',
  costHint: '利潤＝使用者扣費−估算成本；估算成本＝同分組、同模型的（帳號統計定價或基礎費用）合計 × 帳號目前成本倍率。成本倍率在帳號編輯中設定，預設 0.1，獨立於帳號計費和使用者扣費；修改後重估近期利潤，不改歷史帳單。利潤率＝利潤÷使用者扣費。',
  save: '儲存設定', saving: '儲存中…', saved: '設定已儲存', loading: '載入中…', retry: '重試', error: '讀取或儲存失敗，請重試', invalid: '請檢查分組 ID 和參數範圍',
  recent: '最近一次候選評分', refresh: '重新整理評分', empty: '暫無評分。啟用後，符合範圍且需要自由選路的請求會生成評分。',
  historyPending: '歷史統計尚未就緒，目前使用原有評分；後台每 30 秒重新整理統計。',
  snapshotHint: '僅展示目前服務實例最近一次候選池，最多 100 個帳號。分數用於同一協議與訂閱優先池內排序，不代表最終選中；即時並行可能繼續變化。',
  model: '模型', group: '分組', account: '帳號', score: '得分', tier: '狀態', latency: 'P90 首 token', load: '並行佔用', rate: '成本倍率', quality: '品質通過', samples: '條樣本', unknown: '未知',
  tiers: { eligible: '體驗達標', insufficient: '資料不足', degraded: '體驗未達標' },
  reasons: { quality_below_target: '品質低於目標', quality_unknown: '無有效品質結果', latency_above_target: '延遲超過目標', latency_insufficient: '延遲樣本不足', busy: '並行偏高或有排隊', load_unknown: '並行資料未知', cost_unknown: '成本未知', recent_errors: '近期錯誤偏多', historical_loss: '近期使用者扣費低於理論成本', profit_insufficient: '利潤樣本不足' }
}
