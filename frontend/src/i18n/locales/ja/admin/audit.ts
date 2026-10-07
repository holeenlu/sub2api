export default {
  audit: {
    title: '監査ログ',
    description: '管理者とユーザーによる管理プレーン操作を記録します。ヘッダーの認証情報は先頭と末尾の文字のみ保持し、リクエスト本文はマスキングされます。エントリを個別に削除することはできません。すべてクリアするには二要素認証が必要です。',
    clearAll: 'すべてクリア',
    empty: '監査ログはまだありません',
    loadFailed: '監査ログの読み込みに失敗しました',
    filters: {
      all: 'すべて',
      q: 'キーワード',
      qPlaceholder: 'パス / アクション / 実行者のメールアドレス',
      actorEmail: '実行者のメールアドレス',
      action: 'アクション',
      clientIp: 'クライアントIP',
      method: 'メソッド',
      authMethod: '認証方式',
      result: '結果',
      resultSuccess: '成功',
      resultFailure: '失敗',
      startTime: '開始時刻',
      endTime: '終了時刻'
    },
    columns: {
      time: '時刻',
      actor: '実行者',
      action: 'アクション',
      method: 'メソッド',
      result: '結果',
      clientIp: 'クライアントIP',
      detail: '詳細'
    },
    detail: {
      title: '監査ログの詳細',
      actorRole: 'ロール',
      methodPath: 'メソッド / パス',
      latency: 'レイテンシ',
      requestId: 'リクエストID',
      credential: '認証情報（マスキング済み）',
      userAgent: 'User-Agent',
      requestBody: 'リクエスト本文（マスキング済み）',
      extra: '追加情報'
    },
    clearConfirm: {
      title: 'すべての監査ログをクリア',
      message: 'すべての監査ログを完全に削除します。この操作は元に戻せません。クリア操作自体は記録されます。続行しますか？',
      totpTitle: '二要素認証コードを入力',
      totpHint: '監査ログをクリアするには、新しいTOTP検証が必要です。',
      success: '監査ログを{count}件クリアしました',
      failed: '監査ログのクリアに失敗しました'
    }
  }
}
