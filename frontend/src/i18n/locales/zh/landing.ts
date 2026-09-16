export default {
  batchImageGuide: {
    title: '图片批量生成',
    description: '一次提交多条提示词，任务完成后可统一下载图片结果'
  },
  // Home Page
  home: {
    viewDocs: '查看文档',
    docs: '文档',
    apiDocs: 'API 文档',
    aiApps: 'AI 应用',
    switchToLight: '切换到浅色模式',
    switchToDark: '切换到深色模式',
    dashboard: '控制台',
    login: '登录',
    getStarted: '获取 API Key',
    exploreModels: '探索模型',
    goToDashboard: '进入控制台',
    // 首页主视觉文案
    heroSubtitle: '选个模型，开始开发。',
    heroDescription: '通过一个 API 使用不同 AI 模型。\n减少接入与管理的负担，把时间留给产品开发。',
    heroEyebrow: '多模型 AI API 网关',
    heroTitle: '选个模型，开始开发。',
    contactIntegration: '接入咨询',
    quickInstall: { eyebrow: '几分钟完成接入', title: '从 API Key 直接开始', description: 'Codex CLI 与 Claude Code 提供安全备份、环境变量和配置文件安装指引。' },
    terminal: {
      caption: 'API 请求示意',
      routing: '正在转发至上游…'
    },
    models: {
      title: "模型与费率",
      description: "一个 API，灵活选用前沿模型。能力、价格一目了然，为每项任务找到合适的选择。",
      rateNote: "展示模型广场前 6 项及标准时段费率，已应用分组倍率（专属倍率优先）；阶梯、分时和按次计价详情请查看模型广场。官网链接供参考。",
      loading: "正在加载渠道模型…",
      error: "暂时无法加载渠道模型。",
      empty: "当前没有公开模型。",
      retry: "重试",
      channelUnavailable: "渠道名称暂不可用",
      pricingDetails: "官网参考计价说明",
      officialPricing: "官网费率",
      modelDetails: "模型介绍",
      priceUnit: "USD / 1M tokens · 渠道费率",
      input: "输入",
      output: "输出",
      cacheRead: "缓存读取",
      copyModel: "复制模型 ID：{model}",
      verifiedAt: "官网价格核对日期：{date}",
      anthropicNote: "采用全球标准 API 价格，1M Token 上下文无额外长上下文加价。缓存写入、快速模式和区域推理另计。",
      openaiNote: "展示输入不超过 272K Token 的标准价；超过后整次请求的输入及缓存读取费率为 2 倍、输出为 1.5 倍。Sol 当前优惠价至少持续至 2026-11-21；缓存写入、快速模式及区域处理另计。",
      introductions: {
        generic: "通过统一 API 接入此模型。具体能力及接入范围以模型和渠道配置为准。",
        fable51: "擅长高难度推理、自主编程与多步骤研究，也能处理文档、表格和演示文稿。支持 1M Token 上下文。",
        fable5: "面向复杂软件工程、视觉理解与科学研究，适合需要持续推理和自主执行的长周期任务。",
        opus5: "强化深度推理、编程与智能体能力，适合复杂工程和专业分析。支持 1M Token 上下文。",
        opus48: "面向复杂编程与智能体的混合推理模型，兼顾长任务一致性和自主性。支持 1M Token 上下文。",
        sonnet5: "兼顾速度与智能，适合日常开发和交互式应用。支持自适应思考与 1M Token 上下文。",
        astra: "面向高难度端到端工作，涵盖推理、编程、计算机操作、研究与文档创作。支持 105 万 Token 上下文。",
        sol: "面向复杂专业工作的旗舰模型，适合深入分析和高难度任务。支持可调推理强度与 105 万 Token 上下文。",
        terra: "平衡智能与成本，适合常规开发和业务任务。支持可调推理强度与 105 万 Token 上下文。",
        luna: "面向成本敏感、高吞吐量的轻量任务。支持可调推理强度与 105 万 Token 上下文。"
      }
    },
    architecture: {
      title: '@:common.siteName 如何运作',
      description: '从模型与工具接入，到请求调度与用量管理，了解各项能力如何配合。',
      indexNote: '编号代表能力项，不代表请求步骤。',
      layers: {
        access: '接入端',
        processing: '请求处理',
        management: '管理层'
      },
      nodes: {
        marketplaceDesc: '可用模型、分组与费率',
        application: '应用程序',
        applicationDesc: '你的服务与后端',
        agentsDesc: 'Claude Code、Codex、OpenCode 等代理工具',
        upstream: '可用上游账号',
        upstreamDesc: '同模型、同能力的候选账号池',
        failoverDesc: '条件分支：仅在错误符合重试条件时触发。',
        gatewayDesc: '用户、分组、API Key、配额与每分钟请求数限制。',
        billingDesc: '逐条记录 API 使用量与费用。'
      },
      edges: {
        selection: '选择模型',
        request: 'API 请求',
        error: '符合重试条件的错误',
        retry: '重试其他可用上游',
        controls: '访问与配额设置',
        usage: '用量与费用数据'
      }
    },
    mechanisms: {
      marketplace: {
        title: '模型广场',
        description: '查看可用模型、分组与费率，选择合适的接入方案。'
      },
      routing: {
        title: '智能路由',
        description: '我们按可用状态与调度设置选择上游账号，并通过粘性会话协助保持对话连续性。'
      },
      cost: {
        title: '成本优先路由',
        description: '在支持的 OpenAI 请求中，我们把同一模型候选账号的上游费率纳入调度评分，与负载等因素共同决定分配。',
        note: '上游费率是调度因子之一，不保证每次采用最低费率，也不代表客户账单最低。'
      },
      failover: {
        title: '自动换线重试',
        description: '遇到符合重试条件的上游错误时，我们会尝试切换其他可用上游。'
      },
      billing: {
        title: '用量计费',
        description: '我们逐条记录 API 使用量与费用，让你查看每次调用的消耗。'
      },
      gateway: {
        title: '企业级 AI 网关',
        description: '集中管理用户、分组、API Key、配额与每分钟请求数限制，并由管理员查看管理操作记录。'
      },
      agents: {
        title: 'Agent 路由',
        description: '每把 API Key 绑定分组与模型范围，Claude Code、Codex、OpenCode 等代理工具的请求据此导向对应的模型与账号池；粘性会话与 Codex 对话续接让多轮任务保持在同一条线上。'
      }
    },
    management: {
      subtitle: '团队访问管理',
      description: '通过分组费率、配额与访问设置管理使用，并查看用量及费用。',
      demoLabel: '示意数据',
      auditNote: '管理操作记录由平台管理员查看。',
      usage: {
        title: '逐条用量与费用',
        description: '查看每次 API 调用的模型、用量与费用。'
      },
      keys: {
        title: '用户、分组与密钥',
        description: '集中管理用户、分组与 API Key。'
      },
      quotas: {
        title: '配额与请求限制',
        description: '设置配额及每分钟请求数限制，管理资源使用。',
        monthlyQuota: '本月配额'
      },
      fields: {
        model: '模型',
        tokens: 'Token 用量',
        cost: '费用',
        key: '密钥名称',
        group: '分组',
        quota: '配额',
        rpm: '每分钟请求数'
      },
      sample: {
        model: '示例模型',
        key: '开发密钥',
        group: '示例分组'
      }
    },
    agents: {
      title: 'Agent 工具接入',
      setupGuide: '查看配置指南',
      codexContinuation: '支持 Codex 对话续接',
      codexWebSocket: 'WebSocket 模式',
      dedicatedSetup: '专用接入配置',
      details: '各工具共用同一组接入细节：模型的别名映射到实际上游模型，/v1/models 按 API Key 所属分组提供模型目录，并在转发时保留推理强度设置与 prompt cache key。'
    },
    seo: {
      title: '@:{\'common.siteName\'}｜一个 API，自由选模型',
      description: '通过 @:common.siteName 的一个 API 使用不同 AI 模型，探索可用模型与费率，并查看逐条用量与费用。'
    },
    cta: {
      title: '从下一个 API 请求开始',
      description: '获取 API Key，或先探索可用模型与费率。'
    },
    footer: {
      allRightsReserved: '保留所有权利。'
    }
  },

  // Key Usage Query Page
  keyUsage: {
    title: 'API Key 用量查询',
    subtitle: '输入您的 API Key 以查看实时消费金额与使用状态',
    placeholder: 'sk-ant-mirror-xxxxxxxxxxxx',
    query: '查询',
    querying: '查询中...',
    privacyNote: '您的 Key 仅在浏览器本地处理，不会被存储',
    dateRange: '统计范围:',
    dateRangeToday: '今日',
    dateRange7d: '7 天',
    dateRange30d: '30 天',
    dateRange90d: '90 天',
    dateRangeCustom: '自定义',
    apply: '应用',
    used: '已使用',
    detailInfo: '详细信息',
    tokenStats: 'Token 统计',
    dailyDetail: '按日明细',
    modelStats: '模型用量统计',
    // Table headers
    date: '日期',
    model: '模型',
    requests: '请求数',
    inputTokens: '输入 Tokens',
    outputTokens: '输出 Tokens',
    cacheCreationTokens: '缓存创建',
    cacheReadTokens: '缓存读取',
    cacheWriteTokens: '缓存写入',
    totalTokens: '总 Tokens',
    cost: '费用',
    // Status
    quotaMode: 'Key 限额模式',
    walletBalance: '钱包余额',
    // Ring card titles
    totalQuota: '总额度',
    limit5h: '5 小时限额',
    limitDaily: '日限额',
    limit7d: '7 天限额',
    limitWeekly: '周限额',
    limitMonthly: '月限额',
    // Detail rows
    remainingQuota: '剩余额度',
    expiresAt: '过期时间',
    todayExpires: '(今日到期)',
    daysLeft: '({days} 天)',
    usedQuota: '已用额度',
    resetNow: '即将重置',
    subscriptionType: '订阅类型',
    billingType: '计费方式',
    subscriptionExpires: '订阅到期',
    // Usage stat cells
    todayRequests: '今日请求',
    todayInputTokens: '今日输入',
    todayOutputTokens: '今日输出',
    todayTokens: '今日 Tokens',
    todayCacheCreation: '今日缓存创建',
    todayCacheRead: '今日缓存读取',
    todayCost: '今日费用',
    rpmTpm: 'RPM / TPM',
    totalRequests: '累计请求',
    totalInputTokens: '累计输入',
    totalOutputTokens: '累计输出',
    totalTokensLabel: '累计 Tokens',
    totalCacheCreation: '累计缓存创建',
    totalCacheRead: '累计缓存读取',
    totalCost: '累计费用',
    avgDuration: '平均耗时',
    // Messages
    enterApiKey: '请输入 API Key',
    querySuccess: '查询成功',
    queryFailed: '查询失败',
    queryFailedRetry: '查询失败，请稍后重试',
    noDailyUsage: '暂无按日用量数据',
  },

  // Setup Wizard
  setup: {
    pageTitle: '安装向导',
    title: '@:common.siteName 安装向导',
    description: '配置您的 @:common.siteName 实例',
    database: {
      title: '数据库配置',
      description: '连接到您的 PostgreSQL 数据库',
      host: '主机',
      port: '端口',
      username: '用户名',
      password: '密码',
      databaseName: '数据库名称',
      sslMode: 'SSL 模式',
      passwordPlaceholder: '密码',
      ssl: {
        disable: '禁用',
        require: '要求',
        verifyCa: '验证 CA',
        verifyFull: '完全验证'
      }
    },
    redis: {
      title: 'Redis 配置',
      description: '连接到您的 Redis 服务器',
      host: '主机',
      port: '端口',
      username: '用户名（可选）',
      password: '密码（可选）',
      database: '数据库',
      usernamePlaceholder: '默认用户留空',
      passwordPlaceholder: '密码',
      enableTls: '启用 TLS',
      enableTlsHint: '连接 Redis 时使用 TLS（公共 CA 证书）'
    },
    admin: {
      title: '管理员账户',
      description: '创建您的管理员账户',
      email: '邮箱',
      password: '密码',
      confirmPassword: '确认密码',
      passwordPlaceholder: '至少 8 个字符',
      confirmPasswordPlaceholder: '确认密码',
      passwordMismatch: '密码不匹配'
    },
    ready: {
      title: '准备安装',
      description: '检查您的配置并完成安装',
      database: '数据库',
      redis: 'Redis',
      adminEmail: '管理员邮箱'
    },
    status: {
      testing: '测试中...',
      success: '连接成功',
      testConnection: '测试连接',
      installing: '安装中...',
      completeInstallation: '完成安装',
      completed: '安装完成！',
      redirecting: '正在跳转到登录页面...',
      restarting: '服务正在重启，请稍候...',
      timeout: '服务重启时间超出预期，请手动刷新页面。'
    }
  },

  // Common
}
