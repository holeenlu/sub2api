export default {
  batchImageGuide: {
    title: '画像の一括生成',
    description: '複数のプロンプトを1つのジョブで送信し、完了後に生成画像をダウンロードします'
  },
  // Home Page
  home: {
    viewDocs: 'ドキュメントを見る',
    docs: 'ドキュメント',
    apiDocs: 'APIドキュメント',
    aiApps: 'AIアプリ',
    switchToLight: 'ライトモードに切り替え',
    switchToDark: 'ダークモードに切り替え',
    dashboard: 'ダッシュボード',
    login: 'ログイン',
    getStarted: 'APIキーを取得',
    exploreModels: 'モデルを探す',
    goToDashboard: 'ダッシュボードへ移動',
    // Hero copy
    heroSubtitle: 'モデルを選んで、開発を始めましょう。',
    heroDescription:
      '統合APIから複数のAIモデルにアクセスできます。\n統合管理にかかる時間を減らし、開発に集中できます。',
    heroEyebrow: 'マルチモデルAI APIゲートウェイ',
    heroTitle: 'モデルを選んで、開発を始めましょう。',
    contactIntegration: '統合について相談',
    quickInstall: { eyebrow: '数分で利用開始', title: '1つのAPIキーで開始', description: 'Codex CLIとClaude Codeのガイドには、バックアップ、環境変数、設定ファイルが含まれています。' },
    terminal: {
      caption: 'APIリクエストのイラスト',
      routing: 'アップストリームに転送中…'
    },
    models: {
      title: "モデルと料金",
      description: "1つのAPIで主要モデルを利用できます。機能と料金を比較して、タスクに最適なモデルを見つけましょう。",
      rateNote: "標準期間の料金に基づくモデルカタログの先頭6件（グループ倍率を含む。個人料金が優先されます）。段階制、時間制、リクエスト単位の料金についてはカタログをご覧ください。公式リンクは参考情報です。",
      loading: "チャネルモデルを読み込み中…",
      error: "チャネルモデルを読み込めませんでした。",
      empty: "現在利用可能な公開モデルはありません。",
      retry: "再試行",
      channelUnavailable: "チャネル名を利用できません",
      pricingDetails: "公式参考料金の詳細",
      officialPricing: "公式料金",
      modelDetails: "このモデルについて",
      priceUnit: "USD / 1Mトークン · チャネル料金",
      input: "入力",
      output: "出力",
      cacheRead: "キャッシュ読み取り",
      copyModel: "モデルIDをコピー: {model}",
      verifiedAt: "公式料金の確認日: {date}",
      anthropicNote: "グローバル標準API料金には、1Mトークンのコンテキストウィンドウ全体が含まれます。キャッシュ書き込み、ファストモード、リージョン推論は別料金です。",
      openaiNote: "入力トークン272Kまでの標準料金です。このしきい値を超える場合、リクエスト全体に入力料金とキャッシュ読み取り料金の2倍、出力料金の1.5倍が適用されます。Solのプロモーション料金は少なくとも2026-11-21まで継続します。キャッシュ書き込み、ファストモード、リージョン処理は別料金です。",
      introductions: {
        generic: "統合APIからこのモデルにアクセスできます。機能と可用性はモデルおよびチャネル設定によって異なります。",
        fable51: "高度な推論、自律的なコーディング、多段階の調査、ドキュメント・スプレッドシート・スライドの操作に対応。1Mトークンのコンテキスト。",
        fable5: "複雑なソフトウェアエンジニアリング、視覚理解、長時間の自律タスクにおける継続的な推論を伴う科学研究に対応。",
        opus5: "複雑なエンジニアリングや専門的な分析に対応する高度な推論、コーディング、エージェント機能。1Mトークンのコンテキスト。",
        opus48: "本格的なコーディングとエージェント向けのハイブリッド推論。長時間のタスクを安定して自律実行できます。1Mトークンのコンテキスト。",
        sonnet5: "日常的な開発やインタラクティブアプリに適した速度と知性のバランス。適応型思考と1Mトークンのコンテキスト。",
        astra: "エンドツーエンドの高度な推論、コーディング、コンピューター操作、調査、ドキュメント作成に対応。1.05Mトークンのコンテキスト。",
        sol: "複雑な専門業務や詳細な分析に対応するフラッグシップ級の知性。推論レベルを調整でき、1.05Mトークンのコンテキストに対応。",
        terra: "日常的な開発や業務タスクに適した、知性とコストのバランス。推論レベルを調整でき、1.05Mトークンのコンテキストに対応。",
        luna: "コスト重視の大量処理や軽量タスクに適した効率的な処理。推論レベルを調整でき、1.05Mトークンのコンテキストに対応。"
      }
    },
    architecture: {
      title: '@:{\'common.siteName\'} の仕組み',
      description:
        'モデルとツールへのアクセス、リクエストのスケジューリング、使用量管理がどのように連携するかを確認できます。',
      indexNote: '番号は機能を示すもので、リクエスト手順ではありません。',
      layers: {
        access: 'アクセス',
        processing: 'リクエスト処理',
        management: '管理'
      },
      nodes: {
        marketplaceDesc: '利用可能なモデル、グループ、料金',
        application: 'アプリケーション',
        applicationDesc: 'サービスとバックエンド',
        agentsDesc: 'Claude Code、Codex、OpenCodeなどのエージェントツール',
        upstream: '利用可能なアップストリームアカウント',
        upstreamDesc: '同じモデルと機能に対応する候補アカウントプール',
        failoverDesc: '条件分岐: 再試行可能なエラーの場合のみ実行されます。',
        gatewayDesc: 'ユーザー、グループ、APIキー、クォータ、1分あたりのリクエスト制限。',
        billingDesc: 'API使用量と料金のリクエスト単位の記録。'
      },
      edges: {
        selection: 'モデル選択',
        request: 'APIリクエスト',
        error: '再試行可能なエラー',
        retry: '別の利用可能なアップストリームで再試行',
        controls: 'アクセスとクォータの設定',
        usage: '使用量と料金のデータ'
      }
    },
    mechanisms: {
      marketplace: {
        title: 'モデルマーケットプレイス',
        description: '利用可能なモデル、グループ、料金を確認し、アクセス方法を選択できます。'
      },
      routing: {
        title: 'スマートルーティング',
        description:
          '可用性とスケジューリング設定に基づいてアップストリームアカウントを選択し、スティッキーセッションによって会話の継続性を維持します。'
      },
      cost: {
        title: 'コスト優先ルーティング',
        description:
          'サポート対象のOpenAIリクエストでは、同じモデルを提供するアカウントのアップストリーム料金を、負荷などの要素とともにスケジューリングスコアへ反映します。',
        note: 'アップストリーム料金はスケジューリング要素の1つです。最低料金のルートや最低請求額が保証されるわけではありません。'
      },
      failover: {
        title: 'フェイルオーバー',
        description: 'アップストリームエラーが再試行可能な場合、別の利用可能なアップストリームを試行します。'
      },
      billing: {
        title: '使用量請求',
        description:
          '個々のリクエストのAPI使用量と料金を記録し、各呼び出しの消費量を確認できます。'
      },
      gateway: {
        title: 'エンタープライズAIゲートウェイ',
        description:
          'ユーザー、グループ、APIキー、クォータ、1分あたりのリクエスト制限を一元管理できます。管理操作のログは管理者が確認できます。'
      },
      agents: {
        title: 'エージェントルーティング',
        description:
          '各APIキーはグループとモデルスコープに関連付けられます。Claude Code、Codex、OpenCodeなどのエージェントツールからのリクエストは、対応するモデルとアカウントプールへルーティングされます。スティッキーセッションとCodexの会話継続により、マルチターンタスクを同じ経路で処理できます。'
      }
    },
    management: {
      subtitle: 'チームアクセス管理',
      description:
        'グループ料金、クォータ、アクセス設定で使用量を管理し、使用量と料金を確認できます。',
      demoLabel: '説明用データ',
      auditNote: '管理操作のログはプラットフォーム管理者が確認できます。',
      usage: {
        title: 'リクエスト単位の使用量と料金',
        description: '各APIリクエストのモデル、使用量、料金を確認できます。'
      },
      keys: {
        title: 'ユーザー、グループ、キー',
        description: 'ユーザー、グループ、APIキーを一元管理できます。'
      },
      quotas: {
        title: 'クォータとリクエスト制限',
        description: 'クォータと1分あたりのリクエスト制限を設定して、リソース使用量を管理できます。',
        monthlyQuota: '月間クォータ'
      },
      fields: {
        model: 'モデル',
        tokens: 'トークン使用量',
        cost: '料金',
        key: 'キー名',
        group: 'グループ',
        quota: 'クォータ',
        rpm: '1分あたりのリクエスト数'
      },
      sample: {
        model: 'モデル例',
        key: '開発用キー',
        group: 'グループ例'
      }
    },
    agents: {
      title: 'エージェントとツールの統合',
      setupGuide: 'セットアップガイドを見る',
      codexContinuation: 'Codexの会話継続に対応',
      codexWebSocket: 'WebSocketモード',
      dedicatedSetup: '専用接続設定',
      details:
        'すべてのツールで同じ接続情報を使用します。モデルエイリアスは実際のアップストリームモデルにマッピングされ、/v1/modelsはAPIキーのグループのモデルカタログを返します。転送時には推論レベル設定とプロンプトキャッシュキーが保持されます。'
    },
    seo: {
      title: '@:{\'common.siteName\'} | モデルを選んで、開発を始めましょう。',
      description:
        '1つの@:{\'common.siteName\'} APIから複数のAIモデルにアクセスできます。利用可能なモデルと料金を確認し、リクエスト単位の使用量と料金を確認できます。'
    },
    cta: {
      title: '次のAPIリクエストを始める',
      description: 'APIキーを取得するか、先に利用可能なモデルと料金を確認してください。'
    },
    footer: {
      allRightsReserved: '著作権はすべて留保されています。'
    }
  },

  // Key Usage Query Page
  keyUsage: {
    title: 'APIキー使用量',
    subtitle: 'APIキーを入力すると、リアルタイムの支出と使用状況を確認できます',
    placeholder: 'sk-ant-mirror-xxxxxxxxxxxx',
    query: '照会',
    querying: '照会中...',
    privacyNote: 'キーはブラウザ上でローカル処理され、保存されません',
    dateRange: '期間:',
    dateRangeToday: '今日',
    dateRange7d: '7日間',
    dateRange30d: '30日間',
    dateRange90d: '90日間',
    dateRangeCustom: 'カスタム',
    apply: '適用',
    used: '使用済み',
    detailInfo: '詳細情報',
    tokenStats: 'トークン統計',
    dailyDetail: '日別詳細',
    modelStats: 'モデル使用量統計',
    // Table headers
    date: '日付',
    model: 'モデル',
    requests: 'リクエスト数',
    inputTokens: '入力トークン',
    outputTokens: '出力トークン',
    cacheCreationTokens: 'キャッシュ作成',
    cacheReadTokens: 'キャッシュ読み取り',
    cacheWriteTokens: 'キャッシュ書き込み',
    totalTokens: '合計トークン',
    cost: 'コスト',
    // Status
    quotaMode: 'キーのクォータモード',
    walletBalance: 'ウォレット残高',
    // Ring card titles
    totalQuota: '合計クォータ',
    limit5h: '5時間制限',
    limitDaily: '日次制限',
    limit7d: '7日間制限',
    limitWeekly: '週間制限',
    limitMonthly: '月間制限',
    // Detail rows
    remainingQuota: '残りクォータ',
    expiresAt: '有効期限',
    todayExpires: '（本日期限切れ）',
    daysLeft: '（{days}日）',
    usedQuota: '使用済みクォータ',
    resetNow: 'まもなくリセット',
    subscriptionType: 'サブスクリプション種別',
    billingType: '請求種別',
    subscriptionExpires: 'サブスクリプションの有効期限',
    // Usage stat cells
    todayRequests: '本日のリクエスト数',
    todayInputTokens: '本日の入力',
    todayOutputTokens: '本日の出力',
    todayTokens: '本日のトークン',
    todayCacheCreation: '本日のキャッシュ作成',
    todayCacheRead: '本日のキャッシュ読み取り',
    todayCost: '本日のコスト',
    rpmTpm: 'RPM / TPM',
    totalRequests: '合計リクエスト数',
    totalInputTokens: '合計入力',
    totalOutputTokens: '合計出力',
    totalTokensLabel: '合計トークン',
    totalCacheCreation: '合計キャッシュ作成',
    totalCacheRead: '合計キャッシュ読み取り',
    totalCost: '合計コスト',
    avgDuration: '平均所要時間',
    // Messages
    enterApiKey: 'APIキーを入力してください',
    querySuccess: 'クエリに成功しました',
    queryFailed: 'クエリに失敗しました',
    queryFailedRetry: 'クエリに失敗しました。後でもう一度お試しください',
    noDailyUsage: '日次の使用量データはありません',
  },

  // Setup Wizard
  setup: {
    bootstrapToken: 'セットアップ認証トークン',
    bootstrapTokenPlaceholder: 'サーバー起動時に表示されたトークンを入力',
    bootstrapTokenHint: '起動ログのトークン、または設定済みの SETUP_BOOTSTRAP_TOKEN を使用してください。ブラウザーには保存されず、インストール完了後は無効になります。',
    pageTitle: 'セットアップ',
    title: '@:{\'common.siteName\'} セットアップ',
    description: '@:{\'common.siteName\'} インスタンスを設定します',
    database: {
      title: 'データベース設定',
      description: 'PostgreSQLデータベースに接続',
      host: 'ホスト',
      port: 'ポート',
      username: 'ユーザー名',
      password: 'パスワード',
      databaseName: 'データベース名',
      sslMode: 'SSLモード',
      passwordPlaceholder: 'パスワード',
      ssl: {
        disable: '無効',
        require: '必須',
        verifyCa: 'CAを検証',
        verifyFull: '完全検証'
      }
    },
    redis: {
      title: 'Redis設定',
      description: 'Redisサーバーに接続',
      host: 'ホスト',
      port: 'ポート',
      username: 'ユーザー名（任意）',
      password: 'パスワード（任意）',
      database: 'データベース',
      usernamePlaceholder: 'デフォルトユーザーを使用する場合は空欄のままにしてください',
      passwordPlaceholder: 'パスワード',
      enableTls: 'TLSを有効化',
      enableTlsHint: 'Redisへの接続時にTLSを使用（パブリックCA証明書）'
    },
    admin: {
      title: '管理者アカウント',
      description: '管理者アカウントを作成',
      email: 'メールアドレス',
      password: 'パスワード',
      confirmPassword: 'パスワード（確認）',
      passwordPlaceholder: '8文字以上',
      confirmPasswordPlaceholder: 'パスワードを確認',
      passwordMismatch: 'パスワードが一致しません'
    },
    ready: {
      title: 'インストールの準備完了',
      description: '設定を確認してセットアップを完了してください',
      database: 'データベース',
      redis: 'Redis',
      adminEmail: '管理者メールアドレス'
    },
    status: {
      testing: 'テスト中…',
      success: '接続に成功しました',
      testConnection: '接続をテスト',
      installing: 'インストール中…',
      completeInstallation: 'インストールを完了',
      completed: 'インストールが完了しました！',
      redirecting: 'ログインページにリダイレクトしています…',
      restarting: 'サービスを再起動しています。しばらくお待ちください…',
      timeout: 'サービスの再起動に時間がかかっています。手動でページを更新してください。'
    }
  },

  // Common
}
