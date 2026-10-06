// 此檔案由 tools/zh-tw/gen-locale.mjs 依 locales/zh 自動產生，請勿手動修改。
// 詞彙修正請改 tools/zh-tw/convert.mjs（CORRECTIONS / TW_VOCAB），逐句修正請改 gen-locale.mjs 的 OVERRIDES。
export default {
  audit: {
    title: '操作日誌',
    description: '記錄管理員與使用者的管理面操作，請求標頭憑證僅保留首尾、請求體已去識別化。日誌無法單筆刪除，完整清理需二次驗證。',
    clearAll: '全部清理',
    empty: '暫無操作日誌',
    loadFailed: '載入操作日誌失敗',
    filters: {
      all: '全部',
      q: '關鍵字',
      qPlaceholder: '路徑 / 動作 / 操作者電子郵件',
      actorEmail: '操作者電子郵件',
      action: '動作',
      clientIp: '用戶端 IP',
      method: '請求方法',
      authMethod: '驗證方式',
      result: '結果',
      resultSuccess: '成功',
      resultFailure: '失敗',
      startTime: '開始時間',
      endTime: '結束時間'
    },
    columns: {
      time: '時間',
      actor: '操作者',
      action: '動作',
      method: '方法',
      result: '結果',
      clientIp: '用戶端 IP',
      detail: '詳情'
    },
    detail: {
      title: '操作日誌詳情',
      actorRole: '角色',
      methodPath: '方法 / 路徑',
      latency: '耗時',
      requestId: '請求 ID',
      credential: '憑證（掩碼）',
      userAgent: 'User-Agent',
      requestBody: '請求體（已去識別化）',
      extra: '附加資訊'
    },
    clearConfirm: {
      title: '清理全部操作日誌',
      message: '此操作將永久刪除所有操作日誌，且不可恢復。清理動作本身會被留痕記錄。確定繼續嗎？',
      totpTitle: '輸入二次驗證碼',
      totpHint: '清理操作日誌需要現場驗證 TOTP 驗證碼。',
      success: '已清理 {count} 筆操作日誌',
      failed: '清理操作日誌失敗'
    }
  }
}
