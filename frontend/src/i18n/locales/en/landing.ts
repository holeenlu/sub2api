export default {
  batchImageGuide: {
    title: 'Batch Image Generation',
    description: 'Submit multiple prompts in one job and download the generated images when complete'
  },
  // Home Page
  home: {
    viewDocs: 'View Documentation',
    docs: 'Docs',
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
    terminal: {
      caption: 'API request illustration',
      routing: 'Forwarding to upstream…'
    },
    models: {
      title: 'Models & pricing',
      description: 'Browse available models and public rates to choose a group for your needs.',
      rateNote: 'Public rates are shown here; sign in to view rates applicable to your account.',
      loading: 'Loading models…',
      error: 'Models could not be loaded. Please try again later.',
      empty: 'No public models are currently available.',
      retry: 'Retry',
      sampleGroup: '{provider} standard group',
      sampleNote: 'Illustrative data; see the model catalog for actual rates.',
      inputPer1M: 'Input / 1M tokens',
      outputPer1M: 'Output / 1M tokens'
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
