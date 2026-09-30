// 此檔案由 tools/zh-tw/gen-locale.mjs 依 locales/zh 自動產生，請勿手動修改。
// 詞彙修正請改 tools/zh-tw/convert.mjs（CORRECTIONS / TW_VOCAB），逐句修正請改 gen-locale.mjs 的 OVERRIDES。
export default {
  batchImageGuide: {
    title: '圖片批次生成',
    description: '一次提交多條提示詞，任務完成後可統一下載圖片結果'
  },
  // Home Page
  home: {
    viewDocs: '檢視文件',
    docs: '文件',
    apiDocs: 'API 文件',
    aiApps: 'AI 應用',
    switchToLight: '切換到淺色模式',
    switchToDark: '切換到深色模式',
    dashboard: '控制台',
    login: '登入',
    getStarted: '取得 API Key',
    exploreModels: '探索模型',
    goToDashboard: '進入控制台',
    // 首頁主視覺文案
    heroSubtitle: '選個模型，開始開發。',
    heroDescription: '透過一個 API 使用不同 AI 模型。\n減少串接與管理的負擔，把時間留給產品開發。',
    heroEyebrow: '多模型 AI API 閘道器',
    heroTitle: '選個模型，開始開發。',
    contactIntegration: '接入諮詢',
    quickInstall: { eyebrow: '幾分鐘完成接入', title: '從 API Key 直接開始', description: 'Codex CLI 與 Claude Code 提供安全備份、環境變數和設定檔安裝指引。' },
    terminal: {
      caption: 'API 請求示意',
      routing: '正在轉發至上游…'
    },
    models: {
      title: "模型與費率",
      description: "一個 API，靈活選用前沿模型。能力、價格一目瞭然，為每項任務找到合適的選擇。",
      rateNote: "展示模型廣場前 6 項及標準時段費率，已套用分組倍率（專屬倍率優先）；階梯、分時和按次計價詳情請檢視模型廣場。官網連結供參考。",
      loading: "正在載入通道模型…",
      error: "暫時無法載入通道模型。",
      empty: "目前沒有公開模型。",
      retry: "重試",
      channelUnavailable: "通道名稱暫不可用",
      pricingDetails: "官網參考計價說明",
      officialPricing: "官網費率",
      modelDetails: "模型介紹",
      priceUnit: "USD / 1M tokens · 通道費率",
      input: "輸入",
      output: "輸出",
      cacheRead: "快取讀取",
      copyModel: "複製模型 ID：{model}",
      verifiedAt: "官網價格核對日期：{date}",
      anthropicNote: "採用全球標準 API 價格，1M Token 上下文無額外長上下文加價。快取寫入、快速模式和區域推理另計。",
      openaiNote: "展示輸入不超過 272K Token 的標準價；超過後整次請求的輸入及快取讀取費率為 2 倍、輸出為 1.5 倍。Sol 目前優惠價至少持續至 2026-11-21；快取寫入、快速模式及區域處理另計。",
      introductions: {
        generic: "通過統一 API 接入此模型。具體能力及接入範圍以模型和通道設定為準。",
        fable51: "擅長高難度推理、自主程式設計與多步驟研究，也能處理文件、表格和簡報。支援 1M Token 上下文。",
        fable5: "面向複雜軟體工程、視覺理解與科學研究，適合需要持續推理和自主執行的長週期任務。",
        opus5: "強化深度推理、程式設計與智慧體能力，適合複雜工程和專業分析。支援 1M Token 上下文。",
        opus48: "面向複雜程式設計與智慧體的混合推理模型，兼顧長任務一致性和自主性。支援 1M Token 上下文。",
        sonnet5: "兼顧速度與智慧，適合日常開發和互動式應用。支援自適應思考與 1M Token 上下文。",
        astra: "面向高難度端到端工作，涵蓋推理、程式設計、計算機操作、研究與文件創作。支援 105 萬 Token 上下文。",
        sol: "面向複雜專業工作的旗艦模型，適合深入分析和高難度任務。支援可調推理強度與 105 萬 Token 上下文。",
        terra: "平衡智慧與成本，適合常規開發和業務任務。支援可調推理強度與 105 萬 Token 上下文。",
        luna: "面向成本敏感、高吞吐量的輕量任務。支援可調推理強度與 105 萬 Token 上下文。"
      }
    },
    architecture: {
      title: '@:common.siteName 如何運作',
      description: '從模型與工具接入，到請求排程與用量管理，瞭解各項能力如何配合。',
      indexNote: '編號代表能力項，不代表請求步驟。',
      layers: {
        access: '接入端',
        processing: '請求處理',
        management: '管理層'
      },
      nodes: {
        marketplaceDesc: '可用模型、分組與費率',
        application: '應用程式',
        applicationDesc: '你的服務與後端',
        agentsDesc: 'Claude Code、Codex、OpenCode 等代理工具',
        upstream: '可用上游帳號',
        upstreamDesc: '同模型、同能力的候選帳號池',
        failoverDesc: '條件分支：僅在錯誤符合重試條件時觸發。',
        gatewayDesc: '使用者、分組、API Key、配額與每分鐘請求數限制。',
        billingDesc: '逐筆記錄 API 使用量與費用。'
      },
      edges: {
        selection: '選擇模型',
        request: 'API 請求',
        error: '符合重試條件的錯誤',
        retry: '重試其他可用上游',
        controls: '存取與配額設定',
        usage: '用量與費用資料'
      }
    },
    mechanisms: {
      marketplace: {
        title: '模型廣場',
        description: '檢視可用模型、分組與費率，選擇合適的接入方案。'
      },
      routing: {
        title: '智慧路由',
        description: '我們按可用狀態與排程設定選擇上游帳號，並通過黏性工作階段協助保持對話連續性。'
      },
      cost: {
        title: '成本優先路由',
        description: '在支援的 OpenAI 請求中，我們把同一模型候選帳號的上游費率納入排程評分，與負載等因素共同決定分配。',
        note: '上游費率是排程因子之一，不保證每次採用最低費率，也不代表客戶帳單最低。'
      },
      failover: {
        title: '自動換線重試',
        description: '遇到符合重試條件的上游錯誤時，我們會嘗試切換其他可用上游。'
      },
      billing: {
        title: '用量計費',
        description: '我們逐筆記錄 API 使用量與費用，讓你檢視每次呼叫的消耗。'
      },
      gateway: {
        title: '企業級 AI 閘道器',
        description: '集中管理使用者、分組、API Key、配額與每分鐘請求數限制，並由管理員檢視管理操作記錄。'
      },
      agents: {
        title: 'Agent 路由',
        description: '每把 API Key 綁定分組與模型範圍，Claude Code、Codex、OpenCode 等代理工具的請求據此導向對應的模型與帳號池；黏性工作階段與 Codex 對話續接讓多輪任務保持在同一條線上。'
      }
    },
    management: {
      subtitle: '團隊存取管理',
      description: '通過分組費率、配額與存取設定管理使用，並檢視用量及費用。',
      demoLabel: '示意資料',
      auditNote: '管理操作記錄由平台管理員檢視。',
      usage: {
        title: '逐條用量與費用',
        description: '檢視每次 API 呼叫的模型、用量與費用。'
      },
      keys: {
        title: '使用者、分組與金鑰',
        description: '集中管理使用者、分組與 API Key。'
      },
      quotas: {
        title: '配額與請求限制',
        description: '設定配額及每分鐘請求數限制，管理資源使用。',
        monthlyQuota: '本月配額'
      },
      fields: {
        model: '模型',
        tokens: 'Token 用量',
        cost: '費用',
        key: '金鑰名稱',
        group: '分組',
        quota: '配額',
        rpm: '每分鐘請求數'
      },
      sample: {
        model: '範例模型',
        key: '開發金鑰',
        group: '範例分組'
      }
    },
    agents: {
      title: 'Agent 工具接入',
      setupGuide: '檢視設定指南',
      codexContinuation: '支援 Codex 對話續接',
      codexWebSocket: 'WebSocket 模式',
      dedicatedSetup: '專用接入設定',
      details: '各工具共用同一組接入細節：模型的別名對應到實際上游模型，/v1/models 按 API Key 所屬分組提供模型目錄，並在轉發時保留推理強度設定與 prompt cache key。'
    },
    seo: {
      title: '@:{\'common.siteName\'}｜一個 API，自由選模型',
      description: '透過 @:common.siteName 的一個 API 使用不同 AI 模型，探索可用模型與費率，並檢視逐條用量與費用。'
    },
    cta: {
      title: '從下一個 API 請求開始',
      description: '取得 API Key，或先探索可用模型與費率。'
    },
    footer: {
      allRightsReserved: '保留所有權利。'
    }
  },

  // Key Usage Query Page
  keyUsage: {
    title: 'API Key 用量查詢',
    subtitle: '輸入您的 API Key 以檢視即時消費金額與使用狀態',
    placeholder: 'sk-ant-mirror-xxxxxxxxxxxx',
    query: '查詢',
    querying: '查詢中...',
    privacyNote: '您的 Key 僅在瀏覽器本地處理，不會被儲存',
    dateRange: '統計範圍:',
    dateRangeToday: '今日',
    dateRange7d: '7 天',
    dateRange30d: '30 天',
    dateRange90d: '90 天',
    dateRangeCustom: '自訂',
    apply: '套用',
    used: '已使用',
    detailInfo: '詳細資訊',
    tokenStats: 'Token 統計',
    dailyDetail: '按日明細',
    modelStats: '模型用量統計',
    // Table headers
    date: '日期',
    model: '模型',
    requests: '請求數',
    inputTokens: '輸入 Tokens',
    outputTokens: '輸出 Tokens',
    cacheCreationTokens: '快取建立',
    cacheReadTokens: '快取讀取',
    cacheWriteTokens: '快取寫入',
    totalTokens: '總 Tokens',
    cost: '費用',
    // Status
    quotaMode: 'Key 限額模式',
    walletBalance: '錢包餘額',
    // Ring card titles
    totalQuota: '總額度',
    limit5h: '5 小時限額',
    limitDaily: '日限額',
    limit7d: '7 天限額',
    limitWeekly: '周限額',
    limitMonthly: '月限額',
    // Detail rows
    remainingQuota: '剩餘額度',
    expiresAt: '過期時間',
    todayExpires: '(今日到期)',
    daysLeft: '({days} 天)',
    usedQuota: '已用額度',
    resetNow: '即將重設',
    subscriptionType: '訂閱類型',
    billingType: '計費方式',
    subscriptionExpires: '訂閱到期',
    // Usage stat cells
    todayRequests: '今日請求',
    todayInputTokens: '今日輸入',
    todayOutputTokens: '今日輸出',
    todayTokens: '今日 Tokens',
    todayCacheCreation: '今日快取建立',
    todayCacheRead: '今日快取讀取',
    todayCost: '今日費用',
    rpmTpm: 'RPM / TPM',
    totalRequests: '累計請求',
    totalInputTokens: '累計輸入',
    totalOutputTokens: '累計輸出',
    totalTokensLabel: '累計 Tokens',
    totalCacheCreation: '累計快取建立',
    totalCacheRead: '累計快取讀取',
    totalCost: '累計費用',
    avgDuration: '平均耗時',
    // Messages
    enterApiKey: '請輸入 API Key',
    querySuccess: '查詢成功',
    queryFailed: '查詢失敗',
    queryFailedRetry: '查詢失敗，請稍後重試',
    noDailyUsage: '暫無按日用量資料',
  },

  // Setup Wizard
  setup: {
    bootstrapToken: '安裝授權權杖',
    bootstrapTokenPlaceholder: '輸入伺服器啟動時顯示的權杖',
    bootstrapTokenHint: '請使用伺服器啟動日誌中的權杖，或已設定的 SETUP_BOOTSTRAP_TOKEN。權杖不會儲存在瀏覽器中，安裝完成後即失效。',
    pageTitle: '安裝嚮導',
    title: '@:common.siteName 安裝嚮導',
    description: '設定您的 @:common.siteName 實例',
    database: {
      title: '資料庫設定',
      description: '連線到您的 PostgreSQL 資料庫',
      host: '主機',
      port: '埠',
      username: '使用者名稱',
      password: '密碼',
      databaseName: '資料庫名稱',
      sslMode: 'SSL 模式',
      passwordPlaceholder: '密碼',
      ssl: {
        disable: '停用',
        require: '要求',
        verifyCa: '驗證 CA',
        verifyFull: '完全驗證'
      }
    },
    redis: {
      title: 'Redis 設定',
      description: '連線到您的 Redis 伺服器',
      host: '主機',
      port: '埠',
      username: '使用者名稱（可選）',
      password: '密碼（可選）',
      database: '資料庫',
      usernamePlaceholder: '預設使用者留空',
      passwordPlaceholder: '密碼',
      enableTls: '啟用 TLS',
      enableTlsHint: '連線 Redis 時使用 TLS（公共 CA 憑證）'
    },
    admin: {
      title: '管理員帳戶',
      description: '建立您的管理員帳戶',
      email: '電子郵件',
      password: '密碼',
      confirmPassword: '確認密碼',
      passwordPlaceholder: '至少 8 個字元',
      confirmPasswordPlaceholder: '確認密碼',
      passwordMismatch: '密碼不匹配'
    },
    ready: {
      title: '準備安裝',
      description: '檢查您的設定並完成安裝',
      database: '資料庫',
      redis: 'Redis',
      adminEmail: '管理員電子郵件'
    },
    status: {
      testing: '測試中...',
      success: '連線成功',
      testConnection: '測試連線',
      installing: '安裝中...',
      completeInstallation: '完成安裝',
      completed: '安裝完成！',
      redirecting: '正在導向登入頁面...',
      restarting: '服務正在重啟，請稍候...',
      timeout: '服務重啟時間超出預期，請手動重新整理頁面。'
    }
  },

  // Common
}
