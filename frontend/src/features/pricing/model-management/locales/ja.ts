export default {
  title: 'モデル料金管理',
  readDenied: '料金の参照には有効なセッション、models.read、billing.cost.read 権限が必要です。', unit: '単位：米ドル / 100 万トークン。画像料金は 100 万画像トークン単位です。',
  search: 'モデル ID を検索', refresh: '料金を再取得', sync: '料金を同期', loading: '料金を取得中…', empty: '該当するモデル料金はありません。',
  model: 'モデル', source: '出典', version: '料金バージョン：{version}', synced: '最終同期：{time}', details: '料金の詳細',
  edit: '料金を上書き', cancel: 'キャンセル', save: '料金を保存', reset: '上書きを解除', saved: 'コマンドが完了し、料金を再取得しました。',
  input: '入力', output: '出力', cache_read: 'キャッシュ読み取り', cache_5m: 'キャッシュ書き込み（5 分）', cache_1h: 'キャッシュ書き込み（1 時間）',
  unknown: '不明 / 未設定', unpriced: '料金なし（null）', priced: '料金を設定', inherit: '継承 / 未設定', custom: '個別料金',
  fallback: 'サーバー側の補完（null）', noImage: '個別の画像料金なし（null）',
  tiers: 'コンテキスト段階', above: 'トークン数の下限（超過）', addTier: '段階を追加', remove: '削除', modes: 'サービスモード', image: '画像トークン料金',
  stale: '料金バージョンが変わりました。下書きは保持されています。閉じて再度開き、最新の料金を編集してください。',
  reloadRequired: '次の操作前に料金を再取得してください。下書きは保持されています。サーバーの状態を確認してから再試行してください。',
  working: 'コマンドを実行中…', invalidDraft: '各料金の入力・出力・キャッシュ読み取りは必須で、0〜1e9 の有限数が必要です。キャッシュ書き込みは省略または null を指定できます。段階は最大 32 個で、下限は重複しない正の整数です。',
  errors: {
    conflict: '料金バージョンが競合しました。下書きは保持されています。最新の値を再取得して確認してください。',
    denied: '権限がありません。models.read、billing.cost.read、models.manage、billing.rates.update を確認してください。',
    unavailable: 'モデル料金を現在取得できません。', invalid: '料金バージョンまたは料金データが無効、もしくは欠落しています。',
    request: 'リクエストに失敗しました。結果が不明なため、再試行前に料金を再取得してください。',
    refresh: 'コマンドは受理されましたが、再取得に失敗しました。下書きは保持されています。次の操作前に再取得してください。'
  }
}
