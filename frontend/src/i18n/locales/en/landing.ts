export default {
  batchImageGuide: {
    title: 'Batch Image Generation',
    description: 'Submit multiple prompts in one job and download the generated images when complete'
  },
  // Home Page
  home: {
    viewDocs: 'View Documentation',
    docs: 'Docs',
    apiDocs: 'API Docs',
    aiApps: 'AI Apps',
    switchToLight: 'Switch to Light Mode',
    switchToDark: 'Switch to Dark Mode',
    dashboard: 'Dashboard',
    login: 'Login',
    getStarted: 'Get an API Key',
    exploreModels: 'Explore Models',
    goToDashboard: 'Go to Dashboard',
    // Hero copy
    heroSubtitle: 'Pick a model. Start building.',
    heroDescription:
      'Access multiple AI models through one unified API.\nSpend less time managing integrations and more time building.',
    heroEyebrow: 'A multi-model AI API gateway',
    heroTitle: 'Pick a model. Start building.',
    contactIntegration: 'Discuss your integration',
    quickInstall: { eyebrow: 'Ready in minutes', title: 'Start with one API key', description: 'Codex CLI and Claude Code guides include backups, environment variables, and configuration files.' },
    terminal: {
      caption: 'API request illustration',
      routing: 'Forwarding to upstream…'
    },
    models: {
      title: "Models & pricing",
      description: "One API, leading models. Compare capabilities and prices to find the right fit for every task.",
      rateNote: "The first 6 model catalog entries at standard-period rates, including group multipliers (personal rates take precedence). See the catalog for tiers, time-based and per-request pricing. Official links are for reference.",
      loading: "Loading channel models…",
      error: "Channel models could not be loaded.",
      empty: "No public models are currently available.",
      retry: "Retry",
      channelUnavailable: "Channel name unavailable",
      pricingDetails: "Official reference pricing details",
      officialPricing: "Official pricing",
      modelDetails: "About this model",
      priceUnit: "USD / 1M tokens · Channel rates",
      input: "Input",
      output: "Output",
      cacheRead: "Cache read",
      copyModel: "Copy model ID: {model}",
      verifiedAt: "Official prices verified: {date}",
      anthropicNote: "Global standard API pricing includes the full 1M-token context window. Cache writes, fast mode, and regional inference are priced separately.",
      openaiNote: "Standard prices for up to 272K input tokens. Above that threshold, the entire request uses 2× input and cache read rates and 1.5× output rates. Sol promotional pricing lasts at least through 2026-11-21. Cache writes, fast mode, and regional processing are priced separately.",
      introductions: {
        generic: "Access this model through a unified API. Capabilities and availability depend on the model and channel configuration.",
        fable51: "Demanding reasoning, autonomous coding, multistep research, and work with documents, spreadsheets, and slides. 1M-token context.",
        fable5: "Complex software engineering, visual understanding, and scientific research with sustained reasoning for long autonomous tasks.",
        opus5: "Deep reasoning, coding, and capable agents for complex engineering and professional analysis. 1M-token context.",
        opus48: "Hybrid reasoning for serious coding and agents, with consistent, autonomous execution on long tasks. 1M-token context.",
        sonnet5: "A balance of speed and intelligence for everyday development and interactive apps. Adaptive thinking and a 1M-token context.",
        astra: "Demanding end-to-end reasoning, coding, computer use, research, and document creation. 1.05M-token context.",
        sol: "Flagship intelligence for complex professional work and in-depth analysis. Adjustable reasoning and a 1.05M-token context.",
        terra: "Balanced intelligence and cost for everyday development and business tasks. Adjustable reasoning and a 1.05M-token context.",
        luna: "Efficient processing for cost-sensitive, high-volume, lightweight tasks. Adjustable reasoning and a 1.05M-token context."
      }
    },
    architecture: {
      title: 'How @:common.siteName works',
      description:
        'See how model and tool access, request scheduling, and usage management work together.',
      indexNote: 'Numbers identify capabilities, not request steps.',
      layers: {
        access: 'Access',
        processing: 'Request processing',
        management: 'Management'
      },
      nodes: {
        marketplaceDesc: 'Available models, groups, and rates',
        application: 'Application',
        applicationDesc: 'Your services and backend',
        agentsDesc: 'Agent tools such as Claude Code, Codex, and OpenCode',
        upstream: 'Available upstream accounts',
        upstreamDesc: 'Candidate account pool for the same model and capabilities',
        failoverDesc: 'Conditional branch: triggered only when an error is eligible for retry.',
        gatewayDesc: 'Users, groups, API keys, quotas, and requests-per-minute limits.',
        billingDesc: 'Request-level records of API usage and charges.'
      },
      edges: {
        selection: 'Model selection',
        request: 'API request',
        error: 'Retry-eligible error',
        retry: 'Retry another available upstream',
        controls: 'Access & quota settings',
        usage: 'Usage & charge data'
      }
    },
    mechanisms: {
      marketplace: {
        title: 'Model marketplace',
        description: 'Browse available models, groups, and rates to choose your access options.'
      },
      routing: {
        title: 'Smart routing',
        description:
          'We select upstream accounts based on availability and scheduling settings, with sticky sessions to help preserve conversation continuity.'
      },
      cost: {
        title: 'Cost-prioritized routing',
        description:
          'For supported OpenAI requests, we factor upstream rates for accounts serving the same model into scheduling scores alongside load and other factors.',
        note: 'Upstream rates are one scheduling factor; this does not guarantee the lowest-rate route or the lowest customer bill.'
      },
      failover: {
        title: 'Failover',
        description: 'When an upstream error is eligible for retry, we attempt another available upstream.'
      },
      billing: {
        title: 'Usage billing',
        description:
          'We record API usage and charges for individual requests so you can review consumption for each call.'
      },
      gateway: {
        title: 'Enterprise AI Gateway',
        description:
          'Centrally manage users, groups, API keys, quotas, and requests-per-minute limits, with management-operation logs available to administrators.'
      },
      agents: {
        title: 'Agent routing',
        description:
          'Each API key is bound to a group and model scope, so requests from agent tools such as Claude Code, Codex, and OpenCode are routed to the matching models and account pool. Sticky sessions and Codex conversation continuation keep multi-turn tasks on the same line.'
      }
    },
    management: {
      subtitle: 'Team access management',
      description:
        'Manage usage with group rates, quotas, and access settings, and review usage and charges.',
      demoLabel: 'Illustrative data',
      auditNote: 'Management-operation logs are available to platform administrators.',
      usage: {
        title: 'Request-level usage & charges',
        description: 'Review the model, usage, and charges for each API request.'
      },
      keys: {
        title: 'Users, groups & keys',
        description: 'Centrally manage users, groups, and API keys.'
      },
      quotas: {
        title: 'Quotas & request limits',
        description: 'Set quotas and requests-per-minute limits to manage resource usage.',
        monthlyQuota: 'Monthly quota'
      },
      fields: {
        model: 'Model',
        tokens: 'Token usage',
        cost: 'Charge',
        key: 'Key name',
        group: 'Group',
        quota: 'Quota',
        rpm: 'Requests per minute'
      },
      sample: {
        model: 'Example model',
        key: 'Development key',
        group: 'Example group'
      }
    },
    agents: {
      title: 'Agent & tool integrations',
      setupGuide: 'View setup guide',
      codexContinuation: 'Supports Codex conversation continuation',
      codexWebSocket: 'WebSocket mode',
      dedicatedSetup: 'Dedicated connection settings',
      details:
        'All tools share the same connection details: model aliases map to the actual upstream models, /v1/models returns the model catalog for the API key’s group, and reasoning effort settings and the prompt cache key are preserved when forwarding.'
    },
    seo: {
      title: '@:common.siteName | Pick a model. Start building.',
      description:
        'Access multiple AI models through one @:common.siteName API. Explore available models and rates, and review request-level usage and charges.'
    },
    cta: {
      title: 'Start with your next API request',
      description: 'Get an API key, or explore available models and rates first.'
    },
    footer: {
      allRightsReserved: 'All rights reserved.'
    }
  },

  // Key Usage Query Page
  keyUsage: {
    title: 'API Key Usage',
    subtitle: 'Enter your API Key to view real-time spending and usage status',
    placeholder: 'sk-ant-mirror-xxxxxxxxxxxx',
    query: 'Query',
    querying: 'Querying...',
    privacyNote: 'Your Key is processed locally in the browser and will not be stored',
    dateRange: 'Date Range:',
    dateRangeToday: 'Today',
    dateRange7d: '7 Days',
    dateRange30d: '30 Days',
    dateRange90d: '90 Days',
    dateRangeCustom: 'Custom',
    apply: 'Apply',
    used: 'Used',
    detailInfo: 'Detail Information',
    tokenStats: 'Token Statistics',
    dailyDetail: 'Daily Detail',
    modelStats: 'Model Usage Statistics',
    // Table headers
    date: 'Date',
    model: 'Model',
    requests: 'Requests',
    inputTokens: 'Input Tokens',
    outputTokens: 'Output Tokens',
    cacheCreationTokens: 'Cache Creation',
    cacheReadTokens: 'Cache Read',
    cacheWriteTokens: 'Cache Write',
    totalTokens: 'Total Tokens',
    cost: 'Cost',
    // Status
    quotaMode: 'Key Quota Mode',
    walletBalance: 'Wallet Balance',
    // Ring card titles
    totalQuota: 'Total Quota',
    limit5h: '5-Hour Limit',
    limitDaily: 'Daily Limit',
    limit7d: '7-Day Limit',
    limitWeekly: 'Weekly Limit',
    limitMonthly: 'Monthly Limit',
    // Detail rows
    remainingQuota: 'Remaining Quota',
    expiresAt: 'Expires At',
    todayExpires: '(expires today)',
    daysLeft: '({days} days)',
    usedQuota: 'Used Quota',
    resetNow: 'Resetting soon',
    subscriptionType: 'Subscription Type',
    billingType: 'Billing Type',
    subscriptionExpires: 'Subscription Expires',
    // Usage stat cells
    todayRequests: 'Today Requests',
    todayInputTokens: 'Today Input',
    todayOutputTokens: 'Today Output',
    todayTokens: 'Today Tokens',
    todayCacheCreation: 'Today Cache Creation',
    todayCacheRead: 'Today Cache Read',
    todayCost: 'Today Cost',
    rpmTpm: 'RPM / TPM',
    totalRequests: 'Total Requests',
    totalInputTokens: 'Total Input',
    totalOutputTokens: 'Total Output',
    totalTokensLabel: 'Total Tokens',
    totalCacheCreation: 'Total Cache Creation',
    totalCacheRead: 'Total Cache Read',
    totalCost: 'Total Cost',
    avgDuration: 'Avg Duration',
    // Messages
    enterApiKey: 'Please enter an API Key',
    querySuccess: 'Query successful',
    queryFailed: 'Query failed',
    queryFailedRetry: 'Query failed, please try again later',
    noDailyUsage: 'No daily usage data',
  },

  // Setup Wizard
  setup: {
    bootstrapToken: 'Setup authorization token',
    bootstrapTokenPlaceholder: 'Enter the token shown when the server started',
    bootstrapTokenHint: 'Use the token from the server startup log, or the configured SETUP_BOOTSTRAP_TOKEN. It is not saved in this browser and stops working after installation.',
    pageTitle: 'Setup',
    title: '@:common.siteName Setup',
    description: 'Configure your @:common.siteName instance',
    database: {
      title: 'Database Configuration',
      description: 'Connect to your PostgreSQL database',
      host: 'Host',
      port: 'Port',
      username: 'Username',
      password: 'Password',
      databaseName: 'Database Name',
      sslMode: 'SSL Mode',
      passwordPlaceholder: 'Password',
      ssl: {
        disable: 'Disable',
        require: 'Require',
        verifyCa: 'Verify CA',
        verifyFull: 'Verify Full'
      }
    },
    redis: {
      title: 'Redis Configuration',
      description: 'Connect to your Redis server',
      host: 'Host',
      port: 'Port',
      username: 'Username (optional)',
      password: 'Password (optional)',
      database: 'Database',
      usernamePlaceholder: 'Leave empty for default user',
      passwordPlaceholder: 'Password',
      enableTls: 'Enable TLS',
      enableTlsHint: 'Use TLS when connecting to Redis (public CA certs)'
    },
    admin: {
      title: 'Admin Account',
      description: 'Create your administrator account',
      email: 'Email',
      password: 'Password',
      confirmPassword: 'Confirm Password',
      passwordPlaceholder: 'Min 8 characters',
      confirmPasswordPlaceholder: 'Confirm password',
      passwordMismatch: 'Passwords do not match'
    },
    ready: {
      title: 'Ready to Install',
      description: 'Review your configuration and complete setup',
      database: 'Database',
      redis: 'Redis',
      adminEmail: 'Admin Email'
    },
    status: {
      testing: 'Testing...',
      success: 'Connection Successful',
      testConnection: 'Test Connection',
      installing: 'Installing...',
      completeInstallation: 'Complete Installation',
      completed: 'Installation completed!',
      redirecting: 'Redirecting to login page...',
      restarting: 'Service is restarting, please wait...',
      timeout: 'Service restart is taking longer than expected. Please refresh the page manually.'
    }
  },

  // Common
}
