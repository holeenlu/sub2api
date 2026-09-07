export default {
  batchImageGuide: {
    title: '图片批量生成',
    description: '一次提交多条提示词，任务完成后可统一下载图片结果'
  },
  // Home Page
  home: {
    viewDocs: '查看文档',
    docs: '文档',
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
    terminal: {
      caption: 'API 请求示意',
      routing: '正在转发至上游…'
    },
    models: {
      title: '模型与费率',
      description: '查看可用模型与公开费率，按需选择分组。',
      rateNote: '此处显示公开费率；账号适用费率请登录后查看。',
      loading: '正在加载模型…',
      error: '暂时无法加载模型，请稍后重试。',
      empty: '当前没有公开模型。',
      retry: '重试',
      sampleGroup: '{provider} 标准分组',
      sampleNote: '示意数据，正式费率以模型广场为准。',
      inputPer1M: '输入 / 1M tokens',
      outputPer1M: '输出 / 1M tokens'
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
