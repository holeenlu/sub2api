# 全站页面与权限附录

本附录记录当前实际入口与访问条件。管理SettingsWorkspace现为10个tab（51面板），独立bps tab已删除；供应商门户的11个tab另行计数，不受本次设置页调整影响。92 个页面验收入口对应 82 条渲染记录，其中供应商参数路由展开为 11 个 tab。页面定义、redirect 和 alias 分开列示；页面可见不代表服务端操作授权。

主站、backend-mode、供应商 origin、待完成认证及功能开关还会影响最终入口。角色字段沿服务器真实投影；精确前置条件和参数变体见现有 [route-inventory.json](../../../frontend/tests/acceptance/route-inventory.json)。

FR-04 更正：此前将供应商目录称为粗粒度角色误配属于误报。原始目录已列 platform=super_admin、profile=三角色、children/child-*/payables=vendor_level1，本版仅更新源码追踪并补 case 角色一致性检查；以下角色没有作为权限扩展。

## 1 页面入口

| 编号 | 路径 | 页面 | 角色与附加条件 | 源码 | 已有用例数量 |
| --- | --- | --- | --- | --- | --- |
| page-0 | `/login` | 登录 | 匿名；已登录；已登录时转向合法 landing；backend-mode 的非管理员例外按守卫处理 | [实现](../../../frontend/src/features/identity/pages/LoginView.vue) | 10 |
| page-1 | `/register` | 创建账户 | 匿名；已登录；已登录时转向合法 landing；backend-mode 的非管理员例外按守卫处理 | [实现](../../../frontend/src/features/identity/pages/RegisterView.vue) | 8 |
| page-2 | `/email-verify` | 验证您的邮箱 | 匿名；已登录 | [实现](../../../frontend/src/features/identity/pages/EmailVerifyView.vue) | 7 |
| page-3 | `/auth/callback` | OAuth 回调 | 匿名；已登录 | [实现](../../../frontend/src/features/identity/pages/OAuthCallbackView.vue) | 7 |
| page-4 | `/auth/linuxdo/callback` | LinuxDo 登录回调 | 匿名；已登录 | [实现](../../../frontend/src/features/identity/pages/LinuxDoCallbackView.vue) | 7 |
| page-5 | `/auth/wechat/callback` | 微信登录回调 | 匿名；已登录 | [实现](../../../frontend/src/features/identity/pages/WechatCallbackView.vue) | 7 |
| page-6 | `/auth/dingtalk/callback` | 钉钉登录回调 | 匿名；已登录 | [实现](../../../frontend/src/features/identity/pages/DingTalkCallbackView.vue) | 7 |
| page-7 | `/auth/dingtalk/email-completion` | 创建钉钉账户 | 匿名；已登录 | [实现](../../../frontend/src/features/identity/pages/DingTalkEmailCompletionView.vue) | 6 |
| page-8 | `/auth/oidc/callback` | OIDC 登录回调 | 匿名；已登录 | [实现](../../../frontend/src/features/identity/pages/OidcCallbackView.vue) | 7 |
| page-9 | `/forgot-password` | 重置密码 | 匿名；已登录 | [实现](../../../frontend/src/features/identity/pages/ForgotPasswordView.vue) | 6 |
| page-10 | `/reset-password` | 重置密码 | 匿名；已登录 | [实现](../../../frontend/src/features/identity/pages/ResetPasswordView.vue) | 5 |
| page-11 | `/profile` | 个人设置 | 经主站入口守卫允许的已登录主体 | [实现](../../../frontend/src/features/identity/pages/ProfilePage.vue) | 8 |
| page-12 | `/admin/users` | 用户管理 | super_admin；admin且页面在admin_pages；控件权限：users.update、users.delete、users.create、billing.balance.adjust | [实现](../../../frontend/src/features/identity/pages/UsersPage.vue) | 7 |
| page-13 | `/admin/risk-control` | 风控中心 | super_admin；admin且页面在admin_pages；风控明确关闭时转向可访问设置/仪表盘 | [实现](../../../frontend/src/features/access-policy/components/RiskControlPage.vue) | 8 |
| page-14 | `/keys` | API 密钥 | 经主站入口守卫允许的已登录主体 | [实现](../../../frontend/src/features/keys/pages/KeysPage.vue) | 6 |
| page-15 | `/model-plaza` | 模型广场 | 匿名；已登录；模型广场开关/登录要求生效；embedded=1且已登录使用用户布局 | [实现](../../../frontend/src/features/catalog/pages/ModelPlazaPage.vue) | 5 |
| page-16 | `/available-channels` | 可用渠道 | 经主站入口守卫允许的已登录主体 | [实现](../../../frontend/src/features/catalog/pages/AvailableChannelsPage.vue) | 5 |
| page-18 | `/admin/channels/pricing` | 渠道管理 | super_admin；admin且页面在admin_pages；控件权限：channels.manage | [实现](../../../frontend/src/features/pricing/pages/ChannelsPage.vue) | 6 |
| page-19 | `/subscriptions` | 我的订阅 | 经主站入口守卫允许的已登录主体；订阅明确关闭时拒绝该入口；简易模式转向可访问仪表盘 | [实现](../../../frontend/src/features/subscriptions/pages/UserSubscriptionsPage.vue) | 5 |
| page-20 | `/admin/subscriptions` | 订阅管理 | super_admin；admin且页面在admin_pages；简易模式转向可访问仪表盘 | [实现](../../../frontend/src/features/subscriptions/pages/AdminSubscriptionsPage.vue) | 6 |
| page-21 | `/purchase` | 充值/订阅 | 经主站入口守卫允许的已登录主体；设置已加载且支付明确关闭时转合法仪表盘；加载失败保持未知 | [实现](../../../frontend/src/features/payments/pages/CheckoutPage.vue) | 7 |
| page-22 | `/auth/wechat/payment/callback` | 微信支付回调 | 匿名；已登录 | [实现](../../../frontend/src/features/payments/pages/WechatPaymentCallbackPage.vue) | 6 |
| page-23 | `/orders` | 我的订单 | 经主站入口守卫允许的已登录主体；设置已加载且支付明确关闭时转合法仪表盘；加载失败保持未知 | [实现](../../../frontend/src/features/payments/pages/UserOrdersPage.vue) | 6 |
| page-24 | `/payment/qrcode` | 请扫码支付 | 经主站入口守卫允许的已登录主体；设置已加载且支付明确关闭时转合法仪表盘；加载失败保持未知 | [实现](../../../frontend/src/features/payments/pages/PaymentQRCodePage.vue) | 6 |
| page-25 | `/payment/result` | 支付成功 | 匿名；已登录 | [实现](../../../frontend/src/features/payments/pages/PaymentResultPage.vue) | 6 |
| page-26 | `/payment/stripe` | 立即支付 | 匿名；已登录；method选择弹窗布局；其他情况由App选择用户布局 | [实现](../../../frontend/src/features/payments/pages/StripePaymentPage.vue) | 6 |
| page-27 | `/payment/airwallex` | Airwallex 支付 | 匿名；已登录 | [实现](../../../frontend/src/features/payments/pages/AirwallexPaymentPage.vue) | 6 |
| page-28 | `/payment/stripe-popup` | 立即支付 | 匿名；已登录 | [实现](../../../frontend/src/features/payments/pages/StripePopupPage.vue) | 6 |
| page-29 | `/admin/orders/dashboard` | 支付概览 | super_admin；admin且页面在admin_pages；设置已加载且支付明确关闭时转合法仪表盘；加载失败保持未知 | [实现](../../../frontend/src/features/payments/pages/AdminPaymentDashboardPage.vue) | 6 |
| page-30 | `/admin/orders` | 订单管理 | super_admin；admin且页面在admin_pages；设置已加载且支付明确关闭时转合法仪表盘；加载失败保持未知 | [实现](../../../frontend/src/features/payments/pages/AdminOrdersPage.vue) | 7 |
| page-31 | `/admin/orders/plans` | 订阅套餐 | super_admin；admin且页面在admin_pages；设置已加载且支付明确关闭时转合法仪表盘；加载失败保持未知 | [实现](../../../frontend/src/features/payments/pages/AdminPaymentPlansPage.vue) | 6 |
| page-32 | `/usage` | 使用记录 | 经主站入口守卫允许的已登录主体 | [实现](../../../frontend/src/features/usage/pages/UserUsagePage.vue) | 7 |
| page-33 | `/admin/usage` | 使用记录 | super_admin；admin且页面在admin_pages | [实现](../../../frontend/src/features/usage/pages/AdminUsagePage.vue) | 8 |
| page-34 | `/key-usage` | API Key 用量查询 | 匿名；已登录 | [实现](../../../frontend/src/features/usage/pages/PublicKeyUsagePage.vue) | 5 |
| page-35 | `/admin/ops` | 运维监控 | super_admin；admin且页面在admin_pages | [实现](../../../frontend/src/features/operations/dashboard/OpsDashboard.vue) | 9 |
| page-36 | `/admin/audit-logs` | 操作日志 | super_admin；admin且页面在admin_pages | [实现](../../../frontend/src/features/operations/pages/AuditLogPage.vue) | 6 |
| page-37 | `/admin/prompt-audit` | 提示词审计 | super_admin；admin且页面在admin_pages；风控明确关闭时转向可访问设置/仪表盘 | [实现](../../../frontend/src/features/operations/promptAudit/PromptAuditView.vue) | 8 |
| page-38 | `/admin/channels/monitor` | 渠道监控 | super_admin；admin且页面在admin_pages | [实现](../../../frontend/src/features/operations/monitor/pages/ChannelMonitorPage.vue) | 7 |
| page-39 | `/monitor` | 渠道状态 | 经主站入口守卫允许的已登录主体 | [实现](../../../frontend/src/features/operations/monitor/pages/ChannelStatusPage.vue) | 5 |
| page-40 | `/batch-image` | 图片批量生成 | 经主站入口守卫允许的已登录主体 | [实现](../../../frontend/src/features/jobs/pages/BatchImageJobsPage.vue) | 7 |
| page-41 | `/home` | @:{'common.siteName'}｜一个 API，自由选模型 | 匿名；已登录 | [实现](../../../frontend/src/features/site/pages/HomePage.vue) | 5 |
| page-42 | `/legal/:documentId` | 法律文件 | 匿名；已登录 | [实现](../../../frontend/src/features/site/pages/LegalDocumentPage.vue) | 5 |
| page-43 | `/admin/announcements` | 公告管理 | super_admin；admin且页面在admin_pages | [实现](../../../frontend/src/features/site/pages/AnnouncementsPage.vue) | 6 |
| page-54 | `/docs` | @:common.siteName API 文档 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 5 |
| page-55 | `/docs/quickstart` | 快速开始 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-56 | `/docs/authentication` | 鉴权与接入地址 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-57 | `/docs/api/protocols` | 协议与接口总览 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-58 | `/docs/pricing` | 价格与计费 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-59 | `/docs/errors` | 错误与重试 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-60 | `/docs/limits` | 限额与超时 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-61 | `/docs/api/chat/openai-chat` | OpenAI Chat Completions | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-62 | `/docs/api/chat/openai-responses` | OpenAI Responses | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-63 | `/docs/api/chat/anthropic-messages` | Anthropic Messages | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-64 | `/docs/api/chat/gemini-native` | Gemini 原生协议 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-65 | `/docs/api/chat/grok-native` | Grok 专属接口 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-66 | `/docs/api/image/openai-image` | OpenAI 图像生成与编辑 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-67 | `/docs/models` | 开放模型 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-68 | `/docs/api/query/models` | 查询模型列表 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-69 | `/docs/api/query/token-count` | Token 统计 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-70 | `/docs/api/query/embeddings` | 文本向量 Embeddings | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-71 | `/apps` | AI 应用 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-72 | `/apps/console` | 控制台与客户端配置 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-73 | `/apps/codex` | Codex 桌面端与 CLI | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-74 | `/apps/claude-code` | Claude Code | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-75 | `/apps/claude-desktop` | Claude 桌面端 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-76 | `/apps/session-recovery-codex` | Codex 会话恢复 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-77 | `/apps/session-recovery-claude` | Claude Code 会话恢复 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-78 | `/apps/image-skills` | Codex 图片 Skills | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-79 | `/apps/downloads` | 下载中心 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 7 |
| page-80 | `/docs/models/:modelId` | 开放模型 | 匿名；已登录 | [实现](../../../frontend/src/features/site/docs/pages/DocsView.vue) | 6 |
| page-81 | `/setup` | 安装向导 | 匿名；已登录；已安装时转登录或相应仪表盘 | [实现](../../../frontend/src/features/system/pages/SetupWizardPage.vue) | 6 |
| page-82 | `/admin/plugins` | 插件管理 | super_admin；admin且页面在admin_pages | [实现](../../../frontend/src/features/system/pages/PluginsPage.vue) | 6 |
| page-84-accounts | `/vendor/accounts` | 供应商控制台 | vendor_level1 / vendor_level2；super_admin 转 platform?tab=accounts；配置origin精确匹配且vendor_console_enabled=true | [实现](../../../frontend/src/app/compositions/vendor-portal/VendorPortalPage.vue) | 7 |
| page-84-proxies | `/vendor/proxies` | 供应商控制台 | vendor_level1 / vendor_level2；super_admin 转 platform?tab=proxies；配置origin精确匹配且vendor_console_enabled=true | [实现](../../../frontend/src/app/compositions/vendor-portal/VendorPortalPage.vue) | 6 |
| page-84-usage | `/vendor/usage` | 供应商控制台 | vendor_level1 / vendor_level2；super_admin 转 platform?tab=usage；配置origin精确匹配且vendor_console_enabled=true | [实现](../../../frontend/src/app/compositions/vendor-portal/VendorPortalPage.vue) | 5 |
| page-84-settlements | `/vendor/settlements` | 供应商控制台 | vendor_level1 / vendor_level2；super_admin 转 platform?tab=settlements；配置origin精确匹配且vendor_console_enabled=true | [实现](../../../frontend/src/app/compositions/vendor-portal/VendorPortalPage.vue) | 5 |
| page-84-profile | `/vendor/profile` | 供应商控制台 | super_admin / vendor_level1 / vendor_level2；配置origin精确匹配且vendor_console_enabled=true | [实现](../../../frontend/src/app/compositions/vendor-portal/VendorPortalPage.vue) | 6 |
| page-84-platform | `/vendor/platform` | 供应商控制台 | super_admin；其他角色回到各自允许入口；配置origin精确匹配且vendor_console_enabled=true | [实现](../../../frontend/src/app/compositions/vendor-portal/VendorPortalPage.vue) | 7 |
| page-84-children | `/vendor/children` | 供应商控制台 | vendor_level1；vendor_level2 转 accounts；super_admin 转 platform；配置origin精确匹配且vendor_console_enabled=true | [实现](../../../frontend/src/app/compositions/vendor-portal/VendorPortalPage.vue) | 5 |
| page-84-child-accounts | `/vendor/child-accounts` | 供应商控制台 | vendor_level1；vendor_level2 转 accounts；super_admin 转 platform；配置origin精确匹配且vendor_console_enabled=true | [实现](../../../frontend/src/app/compositions/vendor-portal/VendorPortalPage.vue) | 5 |
| page-84-child-proxies | `/vendor/child-proxies` | 供应商控制台 | vendor_level1；vendor_level2 转 accounts；super_admin 转 platform；配置origin精确匹配且vendor_console_enabled=true | [实现](../../../frontend/src/app/compositions/vendor-portal/VendorPortalPage.vue) | 5 |
| page-84-child-usage | `/vendor/child-usage` | 供应商控制台 | vendor_level1；vendor_level2 转 accounts；super_admin 转 platform；配置origin精确匹配且vendor_console_enabled=true | [实现](../../../frontend/src/app/compositions/vendor-portal/VendorPortalPage.vue) | 5 |
| page-84-payables | `/vendor/payables` | 供应商控制台 | vendor_level1；vendor_level2 转 accounts；super_admin 转 platform；配置origin精确匹配且vendor_console_enabled=true | [实现](../../../frontend/src/app/compositions/vendor-portal/VendorPortalPage.vue) | 5 |
| page-87 | `/vendor-unavailable` | 供应商控制台 | 匿名；已登录 | [实现](../../../frontend/src/app/compositions/vendor-portal/VendorUnavailablePage.vue) | 5 |
| page-90 | `/dashboard` | 仪表盘 | 经主站入口守卫允许的已登录主体 | [实现](../../../frontend/src/app/compositions/dashboard/pages/UserDashboardPage.vue) | 5 |
| page-91 | `/custom/:id` | 自定义页面 | 经主站入口守卫允许的已登录主体 | [实现](../../../frontend/src/app/compositions/custom-page/CustomPage.vue) | 6 |
| page-93 | `/admin/dashboard` | 管理控制台 | super_admin；admin且页面在admin_pages | [实现](../../../frontend/src/app/compositions/dashboard/pages/AdminDashboardPage.vue) | 6 |
| page-94 | `/admin/groups` | 分组管理 | super_admin；admin且页面在admin_pages；控件权限：groups.manage、billing.rates.update | [实现](../../../frontend/src/app/compositions/group-editor/GroupsPage.vue) | 6 |
| page-95 | `/admin/accounts` | 账号管理 | super_admin；admin且页面在admin_pages；控件权限：accounts.authorize、accounts.manage、billing.rates.update、billing.cost.read、accounts.diagnostics | [实现](../../../frontend/src/app/compositions/resource-workspace/AccountsPage.vue) | 9 |
| page-96 | `/admin/proxies` | IP管理 | super_admin；admin且页面在admin_pages；控件权限：proxies.manage、accounts.read | [实现](../../../frontend/src/app/compositions/resource-workspace/ProxiesPage.vue) | 8 |
| page-97 | `/admin/settings` | 系统设置 | super_admin；admin且页面在admin_pages | [实现](../../../frontend/src/app/compositions/settings/SettingsWorkspace.vue) | 128 |
| page-98 | `/:pathMatch(.*)*` | 页面不存在 | 经主站入口守卫允许的已登录主体 | [实现](../../../frontend/src/app/pages/NotFoundPage.vue) | 5 |

供应商的 platform/children/child-* 等入口还有页面内权限与关系校验；上表列出的供应商角色不能理解为所有 tab 均可操作。普通 admin 对完整设置面板的可见性与委托字段编辑能力也不能混用。供应商的有效角色在本表按实际 Router/页面分支补充，原验收 inventory 的通用 allowedRoles 不足以描述 platform、profile 和下级 tab；追踪文件同时保留原值与有效值，源测试定义没有被擅改。

## 2 重定向入口

| 原入口 | 注册目标 | 说明 |
| --- | --- | --- |
| `/admin/channels` | `/admin/channels/pricing` | 目标仍经过当前角色、origin和功能守卫 |
| `/apps/session-recovery` | `/apps/session-recovery-codex` | 目标仍经过当前角色、origin和功能守卫 |
| `/docs/apps/session-recovery` | `/apps/session-recovery-codex` | 目标仍经过当前角色、origin和功能守卫 |
| `/docs/apps` | `/apps` | 目标仍经过当前角色、origin和功能守卫 |
| `/docs/apps/codex` | `/apps/codex` | 目标仍经过当前角色、origin和功能守卫 |
| `/docs/apps/claude-code` | `/apps/claude-code` | 目标仍经过当前角色、origin和功能守卫 |
| `/docs/apps/claude-desktop` | `/apps/claude-desktop` | 目标仍经过当前角色、origin和功能守卫 |
| `/docs/apps/session-recovery-codex` | `/apps/session-recovery-codex` | 目标仍经过当前角色、origin和功能守卫 |
| `/docs/apps/session-recovery-claude` | `/apps/session-recovery-claude` | 目标仍经过当前角色、origin和功能守卫 |
| `/docs/apps/image-skills` | `/apps/image-skills` | 目标仍经过当前角色、origin和功能守卫 |
| `/docs/downloads` | `/apps/downloads` | 目标仍经过当前角色、origin和功能守卫 |
| `/vendor` | `/vendor/accounts` | 目标仍经过当前角色、origin和功能守卫 |
| `/vendor/security` | `/vendor/profile` | 目标仍经过当前角色、origin和功能守卫 |
| `/vendor/audit` | `/vendor/accounts` | 目标仍经过当前角色、origin和功能守卫 |
| `/admin/vendors` | `/admin/dashboard` | 目标仍经过当前角色、origin和功能守卫 |
| `/` | `/home` | 目标仍经过当前角色、origin和功能守卫 |
| `/admin` | `/admin/dashboard` | 目标仍经过当前角色、origin和功能守卫 |

## 3 路由别名

| 别名 | 规范记录 | 语义 |
| --- | --- | --- |
| `/auth/oauth/callback` | `/auth/callback` | OAuthCallbackView branches on this alias path; backend-mode anonymous callback allowlist does not include alias. |
| `/docs/batch-image` | `/batch-image` | Resolve the same route record/component while retaining alias URL; callback-specific path branches and guards must still be tested. |

alias 保留其 URL 并匹配同一记录，不等同 redirect。OAuth callback 内部仍可能按实际路径分支，不能把所有别名当作完全相同的匿名 allowlist。

## 4 与原始输入的对应

对原始路由常量、26 个文档导航条目及其应用文档 redirect 的组合结果进行核对：规范 URL 集合仍为 99 条、alias 仍为 2 条，没有以目录迁移为理由新增或删除页面入口。该结论针对入口集合，不代表每个入口的所有运行行为已验收。

核对输入是原始 Git 对象和当前 Router 覆盖清单；当前清单另由现有 acceptanceCatalog.spec.ts 对生产装配 Router 校验。该检查用于文档一致性，不折算为当前753项业务验收通过。
