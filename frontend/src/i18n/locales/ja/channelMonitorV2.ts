/** Channel Monitor V2 (user + admin passive monitor UI) */
export default {
  channelMonitorV2: {
    title: 'チャネルモニター',
    updating: 'データを更新中',
    updatedTo: '{time} に更新',
    partialCoverage: '履歴データが一部のみ利用可能',
    bootstrap: {
      title: '履歴モニターデータを構築中',
      description:
        '初回有効化時に、バックグラウンドでパッシブ集計が90m、24h、7d、30dの各期間を静かに補完します。完了すると、すべての範囲が完全になります。',
      progress: '{percent}% 完了',
      working: 'バックグラウンドで集計中…',
    },
    timeRange: '時間範囲',
    clearFilters: 'リセット',
    refreshingFilters: 'フィルターが変更されました。マトリックス、トレンド、詳細を更新中…',
    switchingData: 'フィルターデータを切り替え中…',
    summaryAria: '選択範囲の概要',
    loadFailed: 'チャネルモニターの読み込みに失敗しました',
    detailLoadFailed: 'チャネルモニターの詳細の読み込みに失敗しました',
    otherModels: 'その他のモデル',
    ignored: '無視',
    currentUser: '現在のユーザー',
    ranges: { '90m': '90m', '24h': '24h', '7d': '7d', '30d': '30d' },
    filters: {
      platform: 'プラットフォーム', allPlatforms: 'すべて', group: 'グループ', allGroups: 'すべて', model: 'モデル', allModels: 'すべて',
      empty: '選択肢なし', selectedCount: '{count}', labelValue: '{label}: {value}'
    },
    groupBy: {
      label: 'グループ化', platform: 'プラットフォーム', platformGroup: 'プラットフォーム / グループ', platformModel: 'プラットフォーム / モデル', platformGroupModel: 'プラットフォーム / グループ / モデル'
    },
    trendView: { label: 'トレンド表示', pulse: 'パルスマトリックス', line: '折れ線グラフ' },
    healthMode: { label: '健全性表示', overall: '全体', success: 'エラー率', ttft: '初回トークン', cache: 'キャッシュ率' },
    tabs: { aria: '詳細ディメンション', models: 'モデル', errors: 'エラー原因', users: 'ユーザーランキング' },
    metrics: {
      rpm: 'RPM',
      tpm: 'TPM',
      tps: 'トークン/秒',
      rpmDetail: '1分あたりのリクエスト数',
      tpmDetail: '1分あたりのトークン数',
      tpsDetail: 'TPM ÷ 60 から算出',
      errorRate: 'エラー率',
      ttft: '初回トークン',
      ttftP50: '初回トークン P50',
      durationP50: '処理時間 P50',
      cacheRate: 'キャッシュ率',
      cacheDetail: '読み取りキャッシュの割合',
      successRate: '成功率',
      successRateValue: '成功率 {value}',
      errorRateValue: 'エラー率 {value}',
      rpmValue: 'RPM {value}',
      tpmValue: 'TPM {value}',
      tpsValue: 'トークン/秒 {value}',
      ttftValue: '初回トークン {value}',
      durationValue: '処理時間 {value}',
      cacheRateValue: 'キャッシュ率 {value}',
    },
    table: { platformModel: 'プラットフォーム / モデル', rank: '順位', user: 'ユーザー' },
    empty: { title: '表示するデータがありません', description: '時間範囲またはフィルターを変更してください' },
    bucket: { minutes: '{count}分単位', hours: '{count}時間単位', days: '{count}日単位' },
    matrix: {
      title: '可用性トレンド', description: '各行はチャネルディメンション、各ブロックは集計期間を表します。詳細を表示するにはホバーしてください', wheelZoom: 'ブロック上でスクロールすると拡大します（範囲が狭くなり、ブロックが広くなります）', wheelZoomX: 'ブロック上でスクロールすると拡大します（範囲が狭くなり、ブロックが広くなります）', dimension: 'チャネルディメンション', emptyTitle: '選択した期間のマトリックスデータがありません', legendAria: '健全性スコアの凡例', bad: '不良', good: '良好', healthyLegend: '健全（≥80）', warningLegend: '要注意（50–79）', criticalLegend: '重大（<50）', unknownLegend: 'トラフィックなし / サンプル不足', noTraffic: 'この期間のトラフィックはありません', noTrafficAt: '{time} · トラフィックなし', scoreLine: '健全性スコア {score}', resetZoom: 'ズームをリセット'
    },
    chart: {
      title: '可用性トレンド', description: '平滑化トレンド：エラー率 · 初回トークン P50 · キャッシュ率', emptyTitle: '選択した期間のトレンドデータがありません', errorLegend: 'エラー率（左軸 %）', cacheLegend: 'キャッシュ率（左軸 %）', ttftLegend: '初回トークン P50（右軸）', errorDataset: 'エラー率トレンド %', cacheDataset: 'キャッシュ率トレンド %', ttftDataset: '初回トークントレンド P50（ms）', percentAxis: '率 %', resetZoom: 'ズームをリセット'
    },
    errorDetail: { http: 'HTTP {code}', upstream: 'アップストリーム {code}', noMessage: 'エラーメッセージなし', empty: 'カテゴリ別の率のみ（サンプルメッセージは管理者専用）' },
    errorCategories: {
      content_policy: 'コンテンツポリシー', authentication: '認証', context_limit: 'コンテキスト上限', invalid_request: '無効なリクエスト', model_unsupported: 'サポート対象外のモデル', group_access: 'グループアクセス', quota_or_balance: 'クォータまたは残高', account_pool_unavailable: 'アカウントプールを利用できません', rate_or_capacity: 'レートまたは容量', timeout: 'タイムアウト', transport_or_stream: 'トランスポートまたはストリーム', upstream_forbidden: 'アップストリームで拒否', not_found: '見つかりません', client_cancelled: 'クライアントによるキャンセル', upstream_5xx: 'アップストリーム 5xx', internal: '内部エラー', other: 'その他'
    },
    rank: {
      gold: '1位（金）',
      silver: '2位（銀）',
      bronze: '3位（銅）',
      place: '{n}位',
      unranked: 'ランク外',
    },
    settings: {
      title: 'V2データモニター設定',
      description:
        'パッシブ使用量集計のディメンション（プラットフォーム / モデル / グループ）と更新頻度を設定します。ユーザーの /monitor ページに表示される健全性カラーと詳細には、絶対的なリクエスト数ではなく、率、RPM、TPMが表示されます。',
      save: '保存',
      loading: '読み込み中…',
      loadFailed: 'V2設定の読み込みに失敗しました',
      saveSuccess: 'V2モニター設定を保存しました',
      saveFailed: 'V2モニター設定の保存に失敗しました',
      modeBanner:
        '現在のシステムモードは {mode} です。V2の分単位集計は実行されません。この設定は準備しておくことができ、{modeV2} への切り替え後に有効になります。モードは「システム設定」→「機能スイッチ」で変更してください。',
      modeClosed: 'チャネルモニターは無効です',
      modeV1: 'V1アクティブプローブ',
      modeV2: 'V2パッシブ監視',
      enableTitle: 'V2集計を有効化',
      enableHint:
        'システムモードがV2の場合に適用されます。これをオフにしても、この設定の集計のみが停止し、システムモードの切り替えは「機能スイッチ」に残ります。',
      refreshTitle: '集計間隔',
      refreshHint: 'マトリックスの時間粒度と更新頻度に影響します',
      refreshAria: '集計間隔',
      platformsTitle: 'プラットフォームとモデル',
      platformsHint:
        '空欄の場合は実際のモデル名をすべて表示します。入力した場合は、一覧にあるモデルだけが個別の行に表示され、残りは「その他」にまとめられます',
      modelsPlaceholder: '空欄 = 実際の全モデル、または主要モデルを列挙（残り → その他）',
      badgeAllModels: 'すべてのモデル',
      badgeOther: '+ その他',
      groupsTitle: '監視対象グループ',
      groupsSelected: '{count}グループを選択中',
      groupsAll: 'すべてのグループ',
      groupsEmpty: '利用可能なグループがありません',
      errorsTitle: 'エラーカテゴリと無視設定',
      errorsHint:
        '「無視」にチェックしたカテゴリは、エラー率と健全性スコアから除外されますが、エラー内訳にはグレー表示で残ります。一致しないエラーは「その他」にまとめられます。',
      ignoredSummary: '{ignored}カテゴリを無視 · エラー率に{counted}カテゴリをカウント',
      healthTitle: '健全性のしきい値',
      healthHint:
        'ユーザー向けのカラーバンドと総合スコアを制御します。デフォルトは許容範囲が広く、わずかなエラー率や低いキャッシュ率ですぐに不健全とは表示されません。',
      fields: {
        minimumSample: '最小サンプル数',
        warningError: 'エラー率の要注意しきい値 %',
        criticalError: 'エラー率の重大しきい値 %',
        targetTtft: 'TTFT目標 ms',
        warningTtft: 'TTFTの要注意しきい値 ms',
        criticalTtft: 'TTFTの重大しきい値 ms',
        warningCache: 'キャッシュ率の要注意しきい値 %',
        criticalCache: 'キャッシュ率の重大しきい値 %',
      },
      namedModelsEmpty: 'プラットフォームモデル一覧が空です。実際のすべてのモデル名が表示されます（「その他」にはまとめられません）。',
      namedModelsCount: '{count}個の名前付きモデルディメンションを表示中。一覧にないモデルはプラットフォームごとの「その他」にまとめられます。',
      userContractTitle: 'ユーザー向け表示仕様',
      userContract: {
        health: '健全性カラーの重み：エラー率 60% + 初回トークン P50 20% + キャッシュ率 20%（しきい値は上で設定可能）',
        trend: 'トレンドはパルスマトリックスと折れ線グラフを切り替え可能（エラー · キャッシュ · 初回トークン）',
        latency: 'レイテンシにはAVG · P50 · P90が表示されます。絶対的なリクエスト数 / エラー数は表示されません',
        models: 'モデル一覧が空の場合は実際のモデル名を表示し、すべてを「その他」にまとめることはありません',
      },
    },
    admin: {
      descriptionV1:
        'システムモードはV1アクティブプローブです。プローブモニターを管理して今すぐチェックを実行できます。V2集計は実行されません。',
      descriptionV2:
        'システムモードはV2パッシブ監視です。集計ディメンションを設定できます。V1アクティブプローブは実行されません。',
      tabAria: 'モニター管理',
      tabV2: 'V2データモニター設定',
      tabV1Active: 'V1アクティブプローブ',
      tabV1History: 'V1履歴（現在のモードではプローブはアクティブではありません）',
    },
  },
}
