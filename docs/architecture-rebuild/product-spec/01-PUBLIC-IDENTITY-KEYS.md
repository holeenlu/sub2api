# 01 公共站点、身份与 API Key 功能规格

c2维护35路径的应用和前端开发证据已交接，当前e5源码已冻结；详见[最终证据](REVIEW-EVIDENCE.md#final-e5-evidence)。当前catalog为51面板/753业务定义，均not_run；开发测试不替代实际业务验收。

## 功能索引

| 功能编号 | 功能名 | 实际入口 | 角色 |
| --- | --- | --- | --- |
| FS-SITE-001 | 首页、品牌与公共导航 | /home、应用壳 | 访客、已登录用户 |
| FS-SITE-002 | 文档导航、搜索、分组和语言 | /docs、/apps 及注册子页 | 访客、已登录用户 |
| FS-SITE-003 | 文档阅读、代码复制与下载链接 | API/Apps 文章页 | 访客、已登录用户 |
| FS-SITE-004 | 文档模型列表与详情入口 | /docs/models、/docs/models/:modelId | 访客、已登录用户 |
| FS-SITE-005 | 登录协议与管理员合规文档 | /legal/:documentId | 访客、已登录用户 |
| FS-SITE-006 | 自定义 Markdown/iframe 页面 | /custom/:id | 当前菜单允许的已登录用户 |
| FS-SITE-007 | 公告铃铛、弹窗和已读 | 应用壳公告入口 | 当前身份的公告接收者 |
| FS-SITE-008 | 公告编辑、发布、删除和已读名单 | /admin/announcements | 获服务端页面及命令授权的管理员 |
| FS-SITE-009 | 外观、导航、协议配置 | /admin/settings 对应面板 | 获字段写权限的管理员 |
| FS-ID-001 | 会话恢复、刷新、跨 tab 与退出 | 登录流程及应用壳 | 当前登录主体 |
| FS-ID-002 | 密码登录与入口限制 | /login | 未完成登录者 |
| FS-ID-003 | 注册及注册校验 | /register | 注册开关允许的访客 |
| FS-ID-004 | 邮箱验证与重发 | /email-verify | 有有效注册上下文者 |
| FS-ID-005 | 第三方登录发起 | 登录/注册页 provider 按钮 | provider 开关允许的访客 |
| FS-ID-006 | OAuth 回调与兼容 alias | /auth/callback 及 provider 回调 | 持合法回调上下文者 |
| FS-ID-007 | pending OAuth 新建、绑定与资料采用 | 回调补全页、钉钉邮箱补全页 | 有有效 pending 会话者 |
| FS-ID-008 | 忘记与重置密码 | /forgot-password、/reset-password | 有效邮箱/重置上下文者 |
| FS-ID-009 | 登录第二因素 | 登录及绑定登录 TOTP 弹窗 | 被服务端要求二次验证者 |
| FS-ID-010 | 管理敏感操作 step-up | 管理命令验证弹窗 | 当前获权管理员 |
| FS-ID-011 | 个人资料、头像及资料来源 | /profile | 当前登录用户 |
| FS-ID-012 | 认证绑定、邮箱绑定与解绑 | 个人资料绑定卡 | 当前登录用户 |
| FS-ID-013 | 修改密码 | 个人资料/供应商安全面板 | 当前登录用户 |
| FS-ID-014 | TOTP 状态、设置与关闭 | 个人资料/供应商安全面板 | 功能开关允许的当前用户 |
| FS-ID-015 | Passkey 列表、注册、改名与撤销 | 登录页、个人资料安全卡 | 功能和浏览器能力允许者 |
| FS-ID-016 | 余额提醒与通知邮箱 | 个人资料通知卡 | 当前登录用户 |
| FS-ID-017 | 用户目录查询与表格操作 | /admin/users | 获页面/读取权限的管理员 |
| FS-ID-018 | 新建与编辑用户 | 用户目录编辑弹窗 | 获命令及字段权限的管理员 |
| FS-ID-019 | 状态、并发、批量限制与删除 | 用户目录行操作和批量操作 | 获对应命令权限的管理员 |
| FS-ID-020 | 自定义用户属性和值 | 用户属性配置及编辑器 | 获属性命令/字段权限的管理员 |
| FS-ID-021 | 用户跨域管理面板 | 用户目录各操作入口 | 获目标对象及命令权限的管理员 |
| FS-ID-022 | 管理员角色权限策略 | 管理设置角色权限面板 | super_admin |
| FS-ID-023 | 管理访问 Key 控制面 | 管理设置 Admin Access Keys 面板 | 获对应权限并满足 step-up 的管理员 |
| FS-ID-024 | 管理员合规确认 | 管理页 guard/受保护命令 | 需确认当前合规版本的管理员 |
| FS-ID-025 | 注册和验证码设置 | /admin/settings 注册、安全面板 | 获对应字段权限的管理员 |
| FS-ID-026 | OAuth provider 配置与校验 | /admin/settings 认证面板 | 获对应字段权限的管理员 |
| FS-ID-027 | 用户默认值与认证来源授予 | /admin/settings 默认值面板 | 获对应字段权限的管理员 |
| FS-ID-028 | 已存在的旧管理 API Key 维护入口 | /admin/settings 折叠旧 Key 面板 | 获对应系统控制权限的管理员 |
| FS-KEY-001 | 我的 Key 查询、列和分页 | /keys | 当前登录用户 |
| FS-KEY-002 | 创建与编辑 Key | Key 编辑弹窗 | 服务端允许管理该 Key 者 |
| FS-KEY-003 | Key 状态和删除 | Key 行操作 | 服务端允许管理该 Key 者 |
| FS-KEY-004 | 批量 Key 修改 | Key 批量编辑弹窗 | 服务端允许管理所选 Key 者 |
| FS-KEY-005 | Key 导入与秘密导出 | Key 导入/导出对话框 | 服务端允许对应命令者 |
| FS-KEY-006 | 客户端配置与使用指南 | Use Key 对话框 | 当前持有可使用 Key 者 |
| FS-KEY-007 | Key/端点复制与显示 | Key 表格、端点浮层 | 当前可见该 Key 者 |

## 口径与证据边界

- 原始比较基线为6f48d6ab6fcc29763af84410690530aeaa957403，维护输入固定c2a10f2；当前前端e5已冻结。完整6196测试/双build真实输入63fe，按唯一测试类型补丁和生产等价证明复用于e5；五项static直接绑定e5。旧f4fe、5c3005及5085记录保留原hash，详见[最终证据](REVIEW-EVIDENCE.md#final-e5-evidence)。开发验证不等于业务验收。
- 路由依据 [应用路由](../../../frontend/src/app/router/index.ts)、[site](../../../frontend/src/features/site/routes.ts)、[identity](../../../frontend/src/features/identity/routes.ts)、[keys](../../../frontend/src/features/keys/routes.ts) 与 [route-inventory](../../../frontend/tests/acceptance/route-inventory.json)。动态参数使用已准备 fixture，不能把 AST 变量表达式当 URL。
- 下文 case ID 均引用 [既有 cases.json](../../../frontend/tests/acceptance/cases.json)，是需求追踪，不是通过回执；本章不另建执行状态；本版实际开发检查在技术证据中单列。工作包及验收事实仍归 [work-items](../work-items.json) 与 [TEST_REPORT](../TEST_REPORT.md)。
- 正常 origin、backend-only 和 supplier origin 的 guard 不同；公开页也受部署模式限制。管理员页面须同时满足服务端 admin_pages、命令 permissions、admin_write_fields 与目标归属；前端按钮不代替服务端鉴权。
- 模型广场、模型可用性与价格权威归第 2 章；Key 公开用量查询 /key-usage、余额/订阅/支付规则归第 3 章。本章只描述入口、调用者权限与组合关系，不重定义资金或分组规则。
- 所有涉及账号、Key、管理目标的异步操作遵守现有 session/scope 和取消合同；Abort 不证明服务器已回滚。权限/金额命令结果未知须读取真实状态并由操作者决定下一步，不能盲重放。

## 公共站点与内容

### FS-SITE-001 首页、品牌与公共导航
- 图片引用：Vite 首屏、运行时 Logo/favicon 使用同一规则；trim 后拒绝内部 CR/LF/TAB、反斜杠、//host、userinfo 和无 hostname 的 HTTP(S)。允许单斜杠根路径、有效 HTTP(S)、大小写不敏感 data:image/；引用通过不表示 data 内容可解码。供应商初始 favicon 使用同源 `/api/v1/settings/favicon`。
- 目录展示：首页模型区与官方参考链接常驻；实际目录查询由公共设置决定 unknown/disabled/authentication_required/available，未知、关闭、需登录时不请求受限目录，不生成虚构报价。设置/身份变化取消旧读取；401/403/404 后重试先刷新设置。
- 前置与入口：/home 面向访客及已登录者；登录按钮与控制台入口按当前身份变化，供应商 origin 的可访问范围由应用 guard 决定。
- 输入与校验：读取 site_name、site_logo、site_subtitle、doc_url、contact_info、home_content、compact_home_enabled；链接经 URL 清理，Logo 允许既有相对地址/data URL。未配置或危险 doc_url 不生成可点击的外部文档链接。
- 正常流程：默认首页展示品牌、机制/模型介绍和登录、API 文档、Apps 入口；紧凑首页提供精简入口；home_content 为有效 URL 时嵌入沙箱 iframe，否则以净化后的自定义 HTML 展示。主题与语言使用现有应用偏好。
- 异常与边界：公共设置未就绪时显示占位/中性展示，不从旧账号或旧部署取品牌；文档链接与展示用 api_base_url 不改变管理 HTTP 的运行 baseURL。首页模型介绍不等于可用模型或配额承诺。
- 权威与追踪：[HomePage](../../../frontend/src/features/site/pages/HomePage.vue)、[公共设置 GET /settings/public](../../../frontend/src/features/site/api.ts)；case：FE-PAGE-HOME-ACTION-1、FE-PAGE-HOME-ACTION-2、FE-BRAND-001、FE-SEC-001、FE-SEC-002。

### FS-SITE-002 文档导航、搜索、分组和语言
- 前置与入口：API 文档及 Apps 共用 DocsView；真实路径见本章末的完整路径表，/docs/models/:modelId 的参数取目录中的真实 ID 或明确的不存在 fixture。
- 输入与校验：搜索针对当前语言的页面标题/说明及已有模型名称，不是全文联网搜索；结果最多 20 条。选择组使用当前查询返回的组 ID，不能通过任意参数扩展访问范围。
- 正常流程：顶栏切 API/Apps，侧栏选页，移动端展开/关闭抽屉；搜索按钮或 Ctrl/⌘+K 打开搜索、选结果跳转，Escape 关闭搜索及抽屉；切组、切语言、点击目录锚点、刷新/前进/返回保持对应路由内容。
- 异常与取消：无搜索结果展示空态；组/模型数据失败显示失败状态，不能伪造空目录为加载成功；卸载移除键盘监听和内容观察器。语言包失败恢复由既有 i18n loader 承担。
- 权威与追踪：[DocsView](../../../frontend/src/features/site/docs/pages/DocsView.vue)、[注册导航](../../../frontend/src/features/site/docs/content/nav.ts)；case：FE-PAGE-DOCS-ACTION-1、FE-PAGE-DOCS-ACTION-2、FE-PAGE-APPS-ACTION-1、FE-PAGE-APPS-ACTION-2；各实际子页另有同名路由对应的 ENTRY/REFRESH/BACK 与 ACTION 用例。

### FS-SITE-003 文档阅读、代码复制与下载链接
- 前置与入口：各 API/Apps 文章页；文档内容来自仓库内 en/zh/zh-TW/ja 内容模块，不是后台文章 CRUD。文章类别包含认证、协议、价格、错误、限制、聊天/图片/查询 API 及客户端使用和会话恢复说明。
- 输入与校验：路由映射到已登记 articleId，代码示例使用当前展示 API 地址及示例凭据；Markdown/HTML 与外链沿已有净化规则。代码语言切换仅修改展示内容。
- 正常流程：加载当前语言文章，生成标题目录；用户切换示例标签、复制代码、点击安全链接、阅读价格表或下载说明。Apps 的安装/恢复说明和下载链接不代表已在本机安装或执行了恢复。
- 异常与外部依赖：文章/数据缺失显示现有空态/错误；复制失败有反馈。真实外链下载、安装器运行、第三方客户端和上游请求须另行准备获准环境，未执行时不能标成功。
- 权威与追踪：[DocsArticleView](../../../frontend/src/features/site/docs/pages/DocsArticleView.vue)、[DocsCodeTabs](../../../frontend/src/features/site/docs/components/DocsCodeTabs.vue)、[示例生成](../../../frontend/src/features/site/docs/content/examples.ts)；case：FE-PAGE-DOCS-QUICKSTART-ACTION-3、FE-PAGE-APPS-CODEX-ACTION-3、FE-PAGE-APPS-DOWNLOADS-EXTERNAL-NETWORK、FE-BRAND-003、FE-SEC-003。

### FS-SITE-004 文档模型列表与详情入口
- 前置与入口：/docs/models 与 /docs/models/:modelId；共享文档壳的组选择，模型存在性和价格来自应用注入的模型查询及本地说明目录。
- 输入与校验：modelId 编码后进入详情；已知模型、服务端存在但本地说明未知的模型、不存在模型是不同情况，不因本地说明缺失虚构下架/免费。
- 正常流程：按组浏览、点击模型、展示相应说明和协议示例；必要时根据模型所在组调整当前组。模型广场、价格计算和可用性规则引用第 2 章，本章仅拥有文档呈现和导航。
- 异常：保留加载失败和缺失数据差别；没有当前模型数据时不显示编造的可用额度。此入口本身不调用生成、付款或开通服务。
- 权威与追踪：[DocsModelsView](../../../frontend/src/features/site/docs/pages/DocsModelsView.vue)、[DocsModelView](../../../frontend/src/features/site/docs/pages/DocsModelView.vue)；case：FE-PAGE-DOCS-MODELS-ACTION-3、FE-PAGE-DOCS-MODELS-MODELID-ACTION-3。

### FS-SITE-005 登录协议与管理员合规文档
- 前置与入口：/legal/:documentId；普通协议取公共设置 login_agreement_documents，admin-compliance 是单独的内置法律文档入口。
- 输入与校验：documentId 按已配置 ID 匹配；普通协议显示 login_agreement_updated_at，管理员合规文档按当前语言选内置文本，不能伪造用户接受记录。
- 正常流程：显示标题、文档类型、更新时间及净化 Markdown；可返回首页/登录、点击安全链接。正文为空、文档不存在与设置读取失败分别显示不同状态。
- 边界：阅读 /legal/admin-compliance 不等于接受合规；接受必须走 FS-ID-024 的人工确认。登录协议 modal/checkbox 的门禁属于 FS-ID-002/003。
- 权威与追踪：[LegalDocumentPage](../../../frontend/src/features/site/pages/LegalDocumentPage.vue)、[公共设置类型](../../../frontend/src/features/site/api/publicSettingsTypes.ts)；case：FE-PAGE-LEGAL-DOCUMENTID-ACTION-1、FE-PAGE-LEGAL-DOCUMENTID-ACTION-2。

### FS-SITE-006 自定义 Markdown/iframe 页面
- 前置与入口：已登录者访问 /custom/:id；可见菜单合并公共设置和管理员允许的私有项，visibility 决定用户/管理展示，路径 ID 不能绕过菜单授权。
- 输入与校验：按 page_slug 或 md: 前缀识别 Markdown 模式，slug 经编码后请求 /pages/:slug；iframe 只接受实际校验允许的 HTTP(S) 地址。嵌入 URL 保留 user_id/theme/lang/ui_mode/src_host/src_url 的展示上下文，移除 token，来源 URL 去掉 query/fragment，不能把这些参数当登录凭证。
- 正常流程：Markdown 模式加载正文、生成目录、折叠/展开目录并滚动到标题；iframe 模式展示页面，按 hide_open_button 显示新窗口按钮，按钮可拖动并区分拖动与点击；切路由/主题/语言后刷新当前内容。
- 异常与外部依赖：菜单项不存在、内容读取失败、iframe 被远端拒绝嵌入分别处理；切目标或离开后旧 Markdown 不得覆盖新页。外部 iframe/新窗口真实加载归外部依赖验收，不由本地占位页面替代。
- 权威与追踪：[CustomPage](../../../frontend/src/app/compositions/custom-page/CustomPage.vue)、[自定义正文 API](../../../frontend/src/features/site/customPages.ts)；case：FE-PAGE-CUSTOM-ID-ACTION-1、FE-PAGE-CUSTOM-ID-ACTION-2、FE-PAGE-CUSTOM-ID-EXTERNAL-NETWORK、FE-SEC-003。

### FS-SITE-007 公告铃铛、弹窗和已读
- 前置与入口：应用壳中的 AnnouncementBell/AnnouncementPopup，作用域为当前登录会话；只消费服务端返回的可见公告，不在客户端重新判定订阅/余额受众资格。
- 输入与流程：读取 GET /announcements，铃铛显示未读数、列表和详情；用户可标记单条或全部已读。popup 模式的未读公告按队列展示，关闭弹窗会发起该条已读请求；管理预览的关闭不写已读。
- 结果：POST /announcements/:id/read 成功后才更新该条 read_at；全部已读逐条调用并保留实际部分结果，不使用不存在的批量端点。同一条并发已读请求去重。
- 异常与取消：单条失败不伪造已读；全部已读部分失败不得报告全成功；离开身份作用域清空队列、延时任务和在途请求。外部真实投递不是该站内通知功能。
- 权威与追踪：[公告状态流程](../../../frontend/src/features/site/queries/useAnnouncements.ts)、[铃铛](../../../frontend/src/features/site/components/announcements/AnnouncementBell.vue)、[公告 API](../../../frontend/src/features/site/api/announcements.ts)；case：FE-AUTH-003、FE-SEC-003。既有目录未给铃铛逐条/全部已读独立 case ID，不能把管理公告 ACTION 用例当作该操作已覆盖。

### FS-SITE-008 公告编辑、发布、删除和已读名单
- 前置与入口：/admin/announcements，写动作需 announcements.manage；搜索、状态筛选、排序、分页、刷新、预览和查看 read-status 为明确独立操作。
- 输入与校验：标题/正文必填；状态 draft/active/archived，通知 silent/popup；开始/结束时间按本地输入转为既有时间戳。受众为 any_of 条件组，组内 all_of，最多各 50 项；订阅条件选择已有组，余额条件使用 gt/gte/lt/lte/eq 与数值，资格判定仍由服务器执行。
- 正常流程：新建 POST /admin/announcements；编辑 PUT /:id 只发送相对读取基线发生变化的字段，清空已设置时间用既有 0 语义；发布/归档通过状态字段完成。删除须确认后 DELETE /:id；已读名单 GET /:id/read-status 支持搜索/分页/排序，显示 eligible/read_at 等服务端字段。
- 异常与取消：取消编辑/删除不发送命令；订阅选项加载失败不清空既有 targeting；服务器验证错误保留草稿；切身份/目标使旧查询和写命令失效。公告预览不能当成已发布或用户已读。
- 权威与追踪：[AnnouncementsPage](../../../frontend/src/features/site/pages/AnnouncementsPage.vue)、[草稿差量](../../../frontend/src/features/site/model/announcementDraft.ts)、[受众编辑器](../../../frontend/src/features/site/components/announcements/AnnouncementTargetingEditor.vue)、[管理 API](../../../frontend/src/features/site/api/adminAnnouncements.ts)；case：FE-PAGE-ADMIN-ANNOUNCEMENTS-ACTION-1、FE-PAGE-ADMIN-ANNOUNCEMENTS-ACTION-2。

### FS-SITE-009 外观、导航和协议配置
- 前置与入口：/admin/settings 对应三个面板，须具备服务端字段写权限；设置工作区负责读取 revision、dirty/sentFields 和一次原子 PUT /admin/settings，本章不建立另一个保存入口。
- 输入：外观包括名称/副标题、Logo、展示 API 地址、custom_endpoints、联系方式、doc_url、自定义首页、紧凑模式；导航项包括 id/label/icon_svg/url/visibility/sort_order/hide_open_button；协议包括 enabled、modal/checkbox、更新时间和文档列表。
- 校验与流程：URL 使用各字段自己的相对地址/data URL 规则；协议 ID 规范化为小写安全 slug 且不重复，标题不得为空，启用协议至少有一份文档。新增/删除/调整菜单和协议条目形成草稿，再由工作区统一提交，成功后刷新公共展示。
- 异常：409 保留草稿并要求重新读取/审阅冲突，不能用新 revision 自动覆盖；敏感/无权字段不进入 sentFields，失败不能当作各面板已经部分保存。运行 API baseURL 与展示地址继续分离。
- 权威与追踪：[site 设置字段与校验](../../../frontend/src/features/site/settings/preparePatch.ts)、[设置工作区](../../../frontend/src/app/compositions/settings/SettingsWorkspace.vue)；case：FE-SETTING-APPEARANCESETTINGSPANEL-FIELDS、FE-SETTING-APPEARANCESETTINGSPANEL-WRITE、FE-SETTING-NAVIGATIONSETTINGSPANEL-FIELDS、FE-SETTING-NAVIGATIONSETTINGSPANEL-WRITE、FE-SETTING-AGREEMENTSSETTINGSPANEL-FIELDS、FE-SETTING-AGREEMENTSSETTINGSPANEL-WRITE。

## 身份、个人安全与管理控制面

### FS-ID-001 会话恢复、刷新、跨 tab 与退出
- 前置：应用启动恢复已有认证，服务端 /auth/me 是当前用户、角色、policy_version、权限和运行模式的权威；缓存用户信息不能代替 Bearer 主体校验。
- 正常流程：登录/刷新沿既有 access/refresh token 协议；普通 token 轮换保留同一逻辑 session，跨 tab 协调刷新；退出调用 /auth/logout 并清理本地身份和身份作用域。全局消费者由 app 监听身份变化清理。
- 异常：暂时刷新失败不冒充退出成功；终态失效按真实状态清会话。A→B、退出、同用户新 session、角色/策略变化后，旧查询、toast、finally、秘密及 step-up continuation 不得回写当前页面。
- 边界：/auth/revoke-all-sessions 是已存在 API，不据此虚构个人资料页的“管理全部设备”按钮；用户目录的供应商角色展示也不产生供应商管理能力。
- 权威与追踪：[auth store](../../../frontend/src/features/identity/model/auth.ts)、[刷新协调](../../../frontend/src/shared/http/tokenRefresh.ts)、[认证 API](../../../frontend/src/features/identity/api/auth.ts)；case：FE-AUTH-002、FE-AUTH-003、FE-DATA-002。

### FS-ID-002 密码登录与入口限制
- 入口/前置：/login；先强制读取公共设置以确定注册、重置、OAuth、Passkey、CAPTCHA 与协议入口。已登录进入登录页按 app guard 返回允许的落点，backend-only 非管理身份例外单独处理。
- 输入/校验：邮箱必填且格式合法，密码必填且当前页面校验至少 6 字符；可切密码显隐。协议启用时须按 modal/checkbox 明确同意；验证码使用实际启用的 Turnstile/Tencent/Aliyun proof 字段，过期 proof 不复用。
- 流程/结果：POST /auth/login；正常响应写入已确认会话再导航；需要 TOTP 时保留临时登录 token 并打开 FS-ID-009，不当成已登录。错误密码、禁用账号和配置加载失败显示真实错误，验证码依现有流程重置。
- 跳转边界：普通 origin 使用当前 router 的 redirect/管理员允许落点，supplier origin 使用 safePortalRedirect；不把前端可见登录按钮当跨门户授权，危险 redirect 的真实 guard 结果需执行既有安全用例确认。
- 权威与追踪：[LoginView](../../../frontend/src/features/identity/pages/LoginView.vue)、[认证 API](../../../frontend/src/features/identity/api/auth.ts)、[协议门禁](../../../frontend/src/features/identity/composables/useLoginAgreement.ts)；case：FE-PAGE-LOGIN-ACTION-1、FE-PAGE-LOGIN-ACTION-2、FE-AUTH-001、FE-AUTH-LOGIN-REDIRECT。

### FS-ID-003 注册及注册校验
- 入口/前置：/register；registration_enabled 控制可注册状态，邮箱后缀白名单、域配额模式、验证码和协议来自最新公共设置。
- 输入/校验：邮箱格式与后缀，密码至少 6 字符、两次一致，协议同意及有效 CAPTCHA；域配额错误来自服务端，不把前端后缀提示视为名额保证。
- 流程/结果：无邮箱验证时提交 POST /auth/register；启用验证时暂存既有 register_data 注册上下文并进入 /email-verify，由后续验证码流程完成注册。第三方按钮只发起启用的 provider，不绕过其补全规则。
- 异常/取消：已有邮箱、后缀不允许、注册关闭、CAPTCHA 失败保留明确反馈；未完成邮箱流程不能声称账号已创建。注册上下文含敏感输入，清理和重入沿既有实现，不输出到日志或规格示例。
- 权威与追踪：[RegisterView](../../../frontend/src/features/identity/pages/RegisterView.vue)、[注册请求 DTO](../../../frontend/src/features/identity/api/types.ts)；case：FE-PAGE-REGISTER-ACTION-1、FE-PAGE-REGISTER-ACTION-2、FE-PAGE-REGISTER-EXTERNAL-CAPTCHA、FE-AUTH-REGISTER-REDIRECT。

### FS-ID-004 邮箱验证与重发
- 前置：/email-verify 有合法 register_data 或 pending OAuth 注册上下文；缺失/损坏上下文返回可恢复入口，不凭 URL 中邮箱直接创建账号。
- 输入/校验：6 位验证码，重发受响应返回的冷却信息和当前 CAPTCHA 门禁约束；普通注册与 pending OAuth 分别使用各自的发送/完成端点。
- 正常流程：发送/重发、输入验证码、提交注册；成功后清理 register_data 并接受最终认证结果。pending OAuth 可继续 FS-ID-007 的资料采用选择；返回上一步保留/清理何种草稿按现有路径处理。
- 异常：错误、过期、已使用验证码和发送失败分别提示；定时器在卸载时清理；本地邮件捕获只证明本地流程，真实邮件投递或 CAPTCHA 验证仍属外部依赖。
- 权威与追踪：[EmailVerifyView](../../../frontend/src/features/identity/pages/EmailVerifyView.vue)、[/auth/send-verify-code 与 pending 发送 API](../../../frontend/src/features/identity/api/auth.ts)；case：FE-PAGE-EMAIL-VERIFY-ACTION-1、FE-PAGE-EMAIL-VERIFY-ACTION-2、FE-PAGE-EMAIL-VERIFY-ACTION-3、FE-PAGE-EMAIL-VERIFY-EXTERNAL-CAPTCHA。

### FS-ID-005 第三方登录发起
- 前置：登录/注册页只呈现当前公共设置启用的 LinuxDo、微信、钉钉、OIDC、GitHub、Google 入口及该页面实际具备的按钮；不因设置存在而补造未渲染入口。
- 输入/校验：沿 provider start URL 和当前 redirect；有 action CAPTCHA 时先取得 proof 再请求 start，普通模式使用既有跳转方式。微信 open/mp/mobile 能力由明确配置决定，非法组合/未启用模式不能猜测回退。
- 正常流程：进入提供商认证，后端处理真实 code/state，再跳回已登记前端 callback；provider 展示名、图标和微信入口提示由当前配置决定。
- 异常/外部依赖：未配置、取消、proof 缺失或 provider 拒绝时不创建本地成功会话；真实 OAuth 往返必须有获准应用、回调配置和测试账号，未具备时标外部依赖阻塞。
- 权威与追踪：[OAuth start/微信模式解析](../../../frontend/src/features/identity/api/auth.ts)、[provider 按钮](../../../frontend/src/features/identity/components/EmailOAuthButtons.vue)；case：FE-PAGE-LOGIN-EXTERNAL-OAUTH、FE-PAGE-REGISTER-EXTERNAL-OAUTH、FE-PAGE-LOGIN-EXTERNAL-CAPTCHA。

### FS-ID-006 OAuth 回调与兼容 alias
- 入口：/auth/callback；/auth/linuxdo/callback、/auth/wechat/callback、/auth/dingtalk/callback、/auth/oidc/callback；/auth/oauth/callback 是前者的 alias，保留 alias URL，不能当新增页面或无条件 redirect。
- 输入/流程：读取当前 provider 的 code/state、错误与完成上下文；通用 callback 可显示 code/state/fullUrl 复制入口，并按已有 alias 分支转到后端 provider callback；完成结果区分最终登录、pending 新建/绑定及 MFA。
- 结果：只有最终认证确认后才采用 token/user；pending 中间 token 不触发错误的整页重挂载。provider 绑定完成回到个人资料，待邮箱补全进入专用流程。
- 异常：缺参数、provider error、过期 pending、重复刷新/返回不能重复消费一次性 code；backend-only 回调 allowlist 不包含通用 alias 的事实须保留，不把 alias 与规范路径的 guard 结果写成完全等价。
- 权威与追踪：[OAuthCallbackView](../../../frontend/src/features/identity/pages/OAuthCallbackView.vue)、[identity 路由](../../../frontend/src/features/identity/routes.ts)、[应用 guard](../../../frontend/src/app/router/index.ts)；case：FE-AUTH-006、FE-PAGE-AUTH-CALLBACK-ACTION-1、FE-PAGE-AUTH-CALLBACK-ACTION-2、FE-PAGE-AUTH-CALLBACK-ACTION-3、FE-ALIAS-AUTH-OAUTH-CALLBACK；各 provider 的准确用例列于末尾。

### FS-ID-007 pending OAuth 新建、绑定与资料采用
- 前置/入口：有效 pending 会话；provider 回调补全组件、/email-verify 或 /auth/dingtalk/email-completion。pending 不是已授权业务会话。
- 输入/校验：按服务端 completion kind 选择新账号或绑定已有账号；需要邮箱/密码/验证码时沿当前页面验证。存在 suggested profile 时明确选择是否采用用户名/头像，保留 adoption 决策，不默认覆盖已有用户资料。
- 正常流程：POST /auth/oauth/pending/exchange 识别上下文；create-account 或 bind-login 完成相应动作；绑定登录若要求 MFA，先完成二次验证再完成 pending；钉钉支持从新建切回绑定已有账号。
- 异常：pending 缺失、过期、已消费、邮箱冲突或取消只能返回明确可恢复步骤，不拼造账号、不将暂存身份当登录成功；重复提交由现有单次上下文和服务器约束处理。
- 权威与追踪：[pending API](../../../frontend/src/features/identity/api/pendingOAuth.ts)、[补全表单](../../../frontend/src/features/identity/components/PendingOAuthCreateAccountForm.vue)、[钉钉邮箱补全](../../../frontend/src/features/identity/pages/DingTalkEmailCompletionView.vue)；case：FE-AUTH-006、FE-PAGE-EMAIL-VERIFY-ACTION-3、FE-PAGE-AUTH-DINGTALK-EMAIL-COMPLETION-ACTION-1、FE-PAGE-AUTH-DINGTALK-EMAIL-COMPLETION-ACTION-2。

### FS-ID-008 忘记与重置密码
- 前置/入口：password_reset_enabled 控制登录页入口；/forgot-password 接受邮箱，/reset-password 要求既有 email/token 参数。
- 输入/校验：邮箱合法及有效验证码 proof；重置新密码至少 6 字符且确认一致。参数缺失显示无效链接，不向服务器提交伪造 token。
- 正常流程：POST /auth/forgot-password 后显示请求已提交；使用合法邮件重置链接提交 /auth/reset-password，成功展示结果并返回登录。是否存在该邮箱以服务端响应合同为准，不额外枚举用户。
- 异常/外部依赖：错误/过期/重复 token、邮件或 CAPTCHA 失败显示对应错误，不改变当前账号密码；真实送达及新旧密码登录结果需实际执行，不能由本地表单通过推定。
- 权威与追踪：[ForgotPasswordView](../../../frontend/src/features/identity/pages/ForgotPasswordView.vue)、[ResetPasswordView](../../../frontend/src/features/identity/pages/ResetPasswordView.vue)；case：FE-PAGE-FORGOT-PASSWORD-ACTION-1、FE-PAGE-FORGOT-PASSWORD-ACTION-2、FE-PAGE-RESET-PASSWORD-ACTION-1、FE-PAGE-RESET-PASSWORD-ACTION-2、FE-PAGE-FORGOT-PASSWORD-EXTERNAL-CAPTCHA。

### FS-ID-009 登录第二因素
- 前置：密码登录或绑定登录响应明确需要 TOTP，返回临时 token 与遮罩邮箱；不能以账号缓存的 totp_enabled 自行跳过验证。
- 输入/流程：用户在 TotpLoginModal 输入 6 位数字验证码，支持既有逐格输入/粘贴，提交 POST /auth/login/2fa；成功接收最终会话并继续原登录/pending 流程。
- 取消/异常：关闭弹窗回到未完成登录状态；错误验证码保留重试提示，临时会话过期需要重新登录；不得先进入私有页面再补验证。
- 权威与追踪：[TotpLoginModal](../../../frontend/src/features/identity/components/TotpLoginModal.vue)、[认证 API](../../../frontend/src/features/identity/api/auth.ts)；case：FE-PAGE-LOGIN-ACTION-2、FE-PAGE-AUTH-LINUXDO-CALLBACK-ACTION-2、FE-AUTH-006。

### FS-ID-010 管理敏感操作 step-up
- 前置：服务器返回 STEP_UP_REQUIRED；未启用 TOTP 或使用不允许的管理员 API Key 返回明确阻断原因，前端不能把其变成普通成功。
- 输入/正常流程：TOTP 验证弹窗提交 POST /user/totp/step-up；验证成功后最多恢复原操作一次，保持原目标、payload、actor 和 signal。对话框自身不是业务命令成功证明。
- 取消/未知结果：取消、关闭业务面板、切目标、换 session 或卸载使对应 continuation 失效；已经 abort 的写请求在真实 HTTP 恢复链也不能再次 dispatch。命令若已发送后断网，不能把本地取消解释为服务端回滚。
- 权威与追踪：[useStepUp](../../../frontend/src/features/identity/composables/useStepUp.ts)、[HTTP 恢复](../../../frontend/src/app/http/install.ts)、[写命令恢复边界回归源码](../../../frontend/src/app/compositions/user-directory/__tests__/stepUpDispatch.spec.ts)；case：FE-AUTH-005、FE-AUTH-003。回归源码不是本次浏览器验收记录。

### FS-ID-011 个人资料、头像及资料来源
- 前置/入口：/profile 读取当前用户，显示身份/账号信息、已绑定来源及当前用户名/头像；资料来源说明取服务器字段，不从头像外链推断身份。
- 输入/流程：用户名去除空白后不得为空，PUT /user 只更新提交的资料；轮询取得的新值不能覆盖正在编辑的不同草稿。头像选择 image/* 文件，预览后显式保存，删除以 avatar_url 空字符串提交。
- 头像校验：目标大小为 20 KiB；GIF 超过该值直接拒绝，较大非 GIF 按现有尺寸/质量步骤压缩；读取/压缩失败和压缩后仍过大分别提示，不声称已上传。头像回写采用服务器返回值。
- 异常：资料读取失败不将旧账号投影当当前资料；修改失败保留可恢复提示，空用户名、未选头像、无可删除头像有各自校验。该页面的余额/订阅只作跨章展示入口。
- 权威与追踪：[ProfilePage](../../../frontend/src/features/identity/pages/ProfilePage.vue)、[ProfileEditForm](../../../frontend/src/features/identity/components/profile/ProfileEditForm.vue)、[ProfileAvatarCard](../../../frontend/src/features/identity/components/profile/ProfileAvatarCard.vue)、[profile API](../../../frontend/src/features/identity/api/profile.ts)；case：FE-PAGE-PROFILE-ACTION-1、FE-PAGE-PROFILE-ACTION-3。

### FS-ID-012 认证绑定、邮箱绑定与解绑
- 前置：ProfileIdentityBindingsSection 读取当前用户的 provider 绑定状态、可绑/可解绑条件、遮罩标识、显示名及提示；未配置 provider 不出现可执行绑定动作。
- 输入/流程：邮箱绑定或换绑填写邮箱，按当前分支要求发送/输入验证码及密码；首次设置登录密码的既有门槛为至少 6 字符。调用 /user/account-bindings/email/send-code、POST /user/account-bindings/email；成功接收新的用户投影。
- OAuth 绑定：从当前已登录主体发起带绑定意图的 provider start，保留回到当前资料页的 redirect；解绑调用 DELETE /user/account-bindings/:provider。用户的实际 bound_count 和能否移除最后一种登录方式由服务端决定。
- 异常/外部依赖：验证码错误、账号冲突、最后登录方式不可解绑等错误不能靠隐藏提示绕过；后台刷新不覆盖进行中的同用户邮箱验证草稿。真实第三方绑定/解绑回程须另行外部验收。
- 权威与追踪：[绑定组件](../../../frontend/src/features/identity/components/profile/ProfileIdentityBindingsSection.vue)、[绑定 API](../../../frontend/src/features/identity/api/profile.ts)；case：FE-PAGE-PROFILE-ACTION-3、FE-PAGE-PROFILE-EXTERNAL-OAUTH、FE-AUTH-006。

### FS-ID-013 修改密码
- 前置/入口：当前已登录用户的 ProfilePasswordForm；供应商安全面板复用同一组件，不生成供应商专用密码 API。
- 输入/校验：旧密码、新密码及确认密码；本组件新密码至少 8 字符且确认一致。此限制与注册/找回页面的 6 字符校验不同，规格保留现状，不统一改写。
- 正常流程：捕获本次输入并 PUT /user/password；等待期间阻止重复提交，成功后清空密码并提示。服务端仍验证旧密码及会话资格。
- 取消/异常：作用域变化/卸载清空敏感输入、abort 请求并丢弃晚到成功/错误；密码错误保留明确反馈。Abort 不撤销已经被服务器接受的改密操作，需用真实登录结果确认未知结果。
- 权威与追踪：[ProfilePasswordForm](../../../frontend/src/features/identity/components/profile/ProfilePasswordForm.vue)、[profile API](../../../frontend/src/features/identity/api/profile.ts)；case：FE-PAGE-PROFILE-ACTION-2、FE-AUTH-003。

### FS-ID-014 TOTP 状态、设置与关闭
- 前置：公共安全开关允许且当前用户可操作；读取 /user/totp/status，设置/关闭前从 /user/totp/verification-method 获取需验证邮箱还是密码。
- 输入/流程：邮箱分支发送 /user/totp/send-code 并按 60 秒冷却重发，密码分支提交当前密码；/user/totp/setup 返回 secret/二维码及 setup 上下文，用户扫描或复制后输入 6 位 TOTP 调用 /enable。
- 关闭：通过独立关闭确认框提交 /user/totp/disable 所需的邮箱/密码验证字段；成功刷新状态，不把打开设置框当作已启用或已关闭。
- 异常/取消：错误验证码、密码、失效设置上下文均显示实际错误；关闭和卸载释放倒计时，秘密不进入持久化偏好。不能用本地验证码 fixture 冒充真实 authenticator 绑定成功。
- 权威与追踪：[TOTP API](../../../frontend/src/features/identity/api/totp.ts)、[设置弹窗](../../../frontend/src/features/identity/components/profile/TotpSetupModal.vue)、[关闭弹窗](../../../frontend/src/features/identity/components/profile/TotpDisableDialog.vue)；case：FE-PAGE-PROFILE-ACTION-2、FE-AUTH-005。

### FS-ID-015 Passkey 列表、注册、改名、撤销与登录
- 前置：passkey_enabled 与浏览器 PublicKeyCredential/credentials 能力；登录页还受协议/CAPTCHA 门禁，个人安全卡显示不支持浏览器或功能关闭提示。
- 注册/管理：GET /user/passkeys；填写名称和非空当前密码，register/begin → 浏览器 credentials.create → register/finish；改名经 prompt 输入非空且变化的名称 PATCH /user/passkeys/:id；删除弹窗必须填写密码后 DELETE 同一路径。
- 登录：login/begin → credentials.get → login/finish，二进制字段按现有 WebAuthn JSON 编解码；CAPTCHA proof 只随 begin，不复制到 finish。正常同-session token 轮换不等于新账号。
- 异常/取消：用户取消 authenticator、配置关闭、换 session/卸载及每个异步交接均不得让旧注册结果进入当前卡片；新增/改名/删除失败不虚构成功或改动本地凭据。真实浏览器设备注册/登录/撤销仍需外部 authenticator 验收。
- 权威与追踪：[Passkey API](../../../frontend/src/features/identity/api/passkey.ts)、[ProfilePasskeyCard](../../../frontend/src/features/identity/components/profile/ProfilePasskeyCard.vue)；case：FE-PAGE-PROFILE-ACTION-3、FE-PAGE-PROFILE-EXTERNAL-WEBAUTHN、FE-PAGE-LOGIN-EXTERNAL-WEBAUTHN。

### FS-ID-016 余额提醒与通知邮箱
- 前置：个人资料通知卡读取当前启用状态、个人阈值、系统默认阈值和已保存通知邮箱；本功能只设通知，不改变余额、扣费或充值规则。
- 输入/流程：切换 balance_notify_enabled；保存正数个人阈值，否则按现有 0 语义恢复默认；最多 3 个已保存和待添加邮箱，地址不能非法/重复。新邮箱发送验证码、输入 6 位码并确认后成为已验证条目。
- 独立操作：已有未验证邮箱可重发/验证，条目可禁用/启用或删除；发送/验证/删除/toggle 使用 /user/notify-email 系列端点，阈值及启用开关走 PUT /user。
- 异常：启用开关保存失败回退 UI；邮箱验证失败不标已验证；每个 60 秒倒计时在卸载时释放。真实邮件送达与提醒是否触发需使用已有邮件 fixture/获准邮箱另行验证。
- 权威与追踪：[ProfileBalanceNotifyCard](../../../frontend/src/features/identity/components/profile/ProfileBalanceNotifyCard.vue)、[profile API](../../../frontend/src/features/identity/api/profile.ts)；case：FE-PAGE-PROFILE-ACTION-1；独立通知邮箱的细分操作当前无专用 case ID，须在该操作用例及 profile-security fixture 中明确记录覆盖范围。

### FS-ID-017 用户目录查询与表格操作
- 前置：/admin/users 的精确页面权限；普通管理员和 super_admin 看到的可管理对象与操作不同。simple 模式导航隐藏不等同于服务端授权。
- 输入：搜索、角色/状态、组名称、Key 所属组、自定义属性过滤；排序白名单含 id/email/username/role/balance/concurrency/status/last_used_at/last_active_at/created_at；另有用量表头的今日/近30天与降序/升序/关闭菜单。分页、每页数量、列显隐及既有列/过滤偏好可操作，切换筛选不能沿旧目标选择误提交。
- 正常流程：GET /admin/users；返回行数据后按可见列及权限加载属性、订阅和用量等附属投影。usage.read 才取用量；缺失数据与真实 0 分开，不能借名称或 user_id 自动扩展 peer 对象范围。
- 异常/取消：刷新、快速筛选/切页、同用户新 session/角色变化时旧查询失效；单项附属投影失败不能抹掉正确的用户主列表或改写其他域事实。
- 权威与追踪：[UsersPage](../../../frontend/src/features/identity/pages/UsersPage.vue)、[目录查询](../../../frontend/src/features/identity/queries/useUserDirectory.ts)、[users API](../../../frontend/src/features/identity/api/users.ts)；case：FE-PAGE-ADMIN-USERS-ACTION-1、FE-AUTH-004、FE-AUTH-003。

### FS-ID-018 新建与编辑用户
- 入口/权限：新建按钮要求 users.create 或 super_admin；普通管理者编辑对象受 users.update 和目标角色约束。新建 user/admin/super_admin 按当前角色选项开放，供应商角色只展示既有状态，不经此表单创建/转换供应商层级。
- 输入：邮箱、密码、用户名、备注、角色、并发、RPM；密码编辑留空不修改，管理密码输入沿 6–72 长度约束，可生成随机密码，编辑器提供显式复制；并发须非负整数、0 保持既有限制语义。新建初始 balance 仅在 billing.balance.adjust 且非 super_admin 目标时呈现，空值省略，资金规则归第 3 章。
- 安全字段：不能在此重设自身安全字段；修改他人的邮箱/密码/重置 TOTP 要求 super_admin，或目标为普通 user 且有 users.security；super_admin 的 peer 操作仍由后端逐对象鉴权。编辑角色选项不允许跨供应商角色转换。
- 流程/异常：POST /admin/users 或 PUT /admin/users/:id；敏感变更按 step-up 恢复，取消不报告成功。编辑器随后单独保存自定义属性，用户更新与属性更新不是同一事务；后一步失败须读取真实状态，不能声称整次回滚。
- 权威与追踪：[UserCreateModal](../../../frontend/src/features/identity/components/users/UserCreateModal.vue)、[UserEditModal](../../../frontend/src/features/identity/components/users/UserEditModal.vue)、[payload 构造](../../../frontend/src/features/identity/model/userDrafts.ts)；case：FE-PAGE-ADMIN-USERS-ACTION-2、FE-AUTH-004、FE-AUTH-005。

### FS-ID-019 状态、并发、批量限制与删除
- 前置：按当前 can(users.update/users.delete) 与对象角色呈现行/批量动作；停用/删除不允许以可见列表绕过自身保护或服务端 peer 语义。
- 正常操作：状态在 active/disabled 间切换；并发单元格只显示当前/上限，修改入口是编辑用户或批量限制。批量限制只提交明确勾选的 concurrency/rpm_limit，使用最多 500 个选中 ID 的快照、all:false，经确认调用 /admin/users/batch-limits，并展示返回 affected。
- 删除：单个 DELETE /admin/users/:id 或批量 POST /admin/users/batch-delete；确认框取消不提交，完成后刷新列表/清理相应选择。批改空字段、无选择、非法数值或超过组件选择上限时禁止提交。
- 异常：状态响应不含列表专属订阅/实时并发字段时，只更新状态相关字段；与列表读取竞态时重新读取。权限拒绝、未知结果不得自动整批再发，用户须确认真实对象状态。
- 权威与追踪：[UsersPage 命令](../../../frontend/src/features/identity/pages/UsersPage.vue)、[BulkEditUserModal](../../../frontend/src/features/identity/components/users/BulkEditUserModal.vue)、[并发单元格](../../../frontend/src/features/identity/components/users/UserConcurrencyCell.vue)；case：FE-PAGE-ADMIN-USERS-ACTION-2、FE-AUTH-004。

### FS-ID-020 自定义用户属性和值
- 前置：用户目录属性配置弹窗及 UserAttributeForm；属性定义、筛选条件和值分开读取，不把列显示偏好当作定义变更。
- 输入/校验：定义 key 以字母开始、后续字母/数字/下划线，name 必填；类型 text/textarea/number/email/url/date/select/multi_select；select/multi_select 至少有选项，保留选项 value/label、required、enabled、placeholder、描述及服务端验证约束。
- 正常流程：新增/编辑/删除定义、启用状态、排序；字段按实际类型渲染并保存当前用户属性值，目录支持属性列与属性过滤。已有定义附加字段不能因编辑器投影被随意丢弃。
- 异常：定义读取失败、正则/范围/选项验证错误按当前表单/服务器反馈；删除与关闭不误提交，用户属性值的保存失败不能报告与用户主信息一起原子失败或原子成功。
- 权威与追踪：[定义弹窗](../../../frontend/src/features/identity/components/users/UserAttributesConfigModal.vue)、[值表单](../../../frontend/src/features/identity/components/users/UserAttributeForm.vue)、[/admin/user-attributes 与用户值 API](../../../frontend/src/features/identity/api/attributes.ts)；case：FE-PAGE-ADMIN-USERS-ACTION-1、FE-PAGE-ADMIN-USERS-ACTION-2。目录未提供每种属性类型的独立用例，执行时须在 user-directory fixture 逐类型记录。

### FS-ID-021 用户跨域管理面板
- 前置：由用户目录传入明确 user/target，app composition 组合领域公开 API；资金、组准入、订阅、用量的权威与计费规则分别归其章节，不能凭入口扩权。
- 组权限/替换：组授权入口对 super_admin 或具 users.update 的普通 user 目标开放；可选额外组、公开组限制开关、专属倍率，清空既有倍率按 null 删除；PUT /admin/users/:id。替换要求选择另一个有效独占标准组，POST /:id/replace-group 一次提交 old_group_id/new_group_id，结果取 migrated_keys，不逐 Key 伪造原子操作。
- 用户 Key：api_keys.read 才显示查看入口，GET /admin/users/:id/api-keys；改组 PUT /admin/api-keys/:keyId，解绑提交 group_id:0，服务端 auto_granted_group_access/组名决定成功提示。写权限/自动授予仍由服务端判定，不以列表读取权推导写权。
- 平台配额：读取 /admin/users/:id/platform-quotas 后编辑日/周/月配置或确认重置窗口；未成功读取不能保存，负数/非有限数拒绝，清空需二次确认；配额完整替换和 reset 端点归 access-policy，额度意义不在身份域重定义。
- 余额/订阅：目录可打开余额历史、加/减余额；加减余额要求 billing.balance.adjust、非自身且目标非 super_admin。订阅列展示已有订阅及组标记，当前 UsersPage 没有订阅分配弹窗；分配操作由第 3 章的订阅管理入口承担。历史查询不是资金变更，金额结果未知不自动重发。
- 取消与追踪：四个组合面板同时捕获 actor/target，关闭/切目标/卸载的 writes.signal 传至底层 HTTP，step-up 后不能 dispatch 旧命令；[组合目录](../../../frontend/src/app/compositions/user-directory)、[userAccess API](../../../frontend/src/features/access-policy/userAccess.ts)、[Key 管理写 API](../../../frontend/src/features/keys/admin.ts)；case：FE-PAGE-ADMIN-USERS-ACTION-3、FE-AUTH-003、FE-AUTH-005。

### FS-ID-022 管理员角色权限策略
- 前置：AdminRolePermissions 仅向 super_admin 呈现；读取 /admin/roles/admin/permissions 的 policy、catalogue、affected_admin_count、step_up_enabled，不能使用客户端自建权限全集。
- 输入/流程：按人员、资源、财务、运维和设置分类展示服务端目录，模块基础权限可选 off/read/manage；部分 CRUD 授权显示 custom，不伪装为完整管理权限。展开明细后逐项勾选，增加权限带上服务端声明的依赖，移除时清掉依赖不满足项。编辑后先审阅新增/移除项和影响人数，再输入非空、最多512字符的 reason，发送 expected_version、permissions、reason；不再描述不存在的全局全选/清空入口。
- 结果/冲突：成功采用返回的新版本并清除本次草稿；ADMIN_POLICY_CONFLICT 保留本地修改和理由、暂停保存，不自动 GET/PUT。用户显式协调时才读取最新策略，在新基线上重放本地差异，保留无关并发授权，再次展示审阅；仍需手动保存。协调失败可重试，version=0 展示既有恢复提示，不当作无限权限。
- 取消/异常：审阅、保存、协调和离开确认期间阻止冲突编辑；切换设置 tab、离开路由或丢弃草稿均核对未保存状态，异步确认仍绑定原会话。角色草稿与聚合网站设置草稿分别管理。失去 super_admin、换 session、卸载会清空策略与草稿并取消请求，旧确认/冲突不能作用于新会话；其他失败保留输入并反馈原因。
- 权威与追踪：[角色权限面板](../../../frontend/src/features/identity/panels/AdminRolePermissions.vue)、[roles API](../../../frontend/src/features/identity/api/roles.ts)；case：FE-SETTING-ADMINROLEPERMISSIONS-FIELDS、FE-SETTING-ADMINROLEPERMISSIONS-WRITE、FE-AUTH-004。

### FS-ID-023 管理访问 Key 控制面
- 前置：管理设置的独立 AdminAccessKeysSettings；这不是 /keys 的模型调用 Key。读取 /admin/settings/admin-api-keys 返回管理 Key 列表和 permission catalogue，权限与 step-up 仍由服务端控制。
- 输入：name 非空、description、enabled、expires_at、allowed_ips；策略含 global_permissions 与 rules，各规则含 permissions、group_ids/all_groups/include_ungrouped、account_types、platforms。到期时间须可解析，不能把读回的 key_prefix 当秘密。
- 操作：新建、编辑、开关、确认轮换 /:id/rotate、确认撤销 DELETE /:id；只有已撤销记录可 DELETE /:id/record。新建/轮换后显示一次性 secret，可复制或显式关闭，普通历史列表只显示 prefix/状态/审计字段。
- 异常/取消：step-up 取消不伪造成功，复制失败有反馈；换 session/关闭所属生命周期清除一次性秘密和旧请求。独立命令不进入全局 settings revision 保存，结果未知不得自动重复创建/轮换。
- 权威与追踪：[AdminAccessKeysSettings](../../../frontend/src/features/identity/settings/AdminAccessKeysSettings.vue)、[管理 Key API/DTO](../../../frontend/src/features/identity/api/adminAccessKeys.ts)、[秘密生命周期](../../../frontend/src/features/identity/queries/useAdminAccessKeys.ts)；case：FE-SETTING-ADMINACCESSKEYSSETTINGS-FIELDS、FE-SETTING-ADMINACCESSKEYSSETTINGS-WRITE、FE-AUTH-005。

### FS-ID-024 管理员合规确认
- 前置：进入管理页或服务器返回 ADMIN_COMPLIANCE_ACK_REQUIRED；GET /admin/compliance 给出 required、当前版本、文档与确认短语，app 通过 typed requireAdminCompliance 回调交给身份 store。
- 流程：对话框展示当前语言文档、版本及安全外链，用户阅读后手动键入与 expectedPhrase 一致的短语，POST /admin/compliance/accept 提交 phrase/language；返回 acknowledgement 才是接受凭据。
- 退出/异常：不能 Escape/点遮罩关闭阻断框；用户可退出登录。请求失败或会话变更后不沿用旧接受状态；读取文档、打开对话框、自动填框均不等于人工承诺。
- 验收约束：真实接受由获授权测试操作者明确执行，自动化不得代替用户接受；缺少该人工动作时保留既有未执行/阻塞事实，不改变全站用例状态。
- 权威与追踪：[AdminComplianceDialog](../../../frontend/src/features/identity/components/AdminComplianceDialog.vue)、[compliance API](../../../frontend/src/features/identity/api/compliance.ts)、[HTTP 装配](../../../frontend/src/app/http/install.ts)；case：FE-PAGE-ADMIN-USERS-MANUAL-COMPLIANCE、FE-PAGE-ADMIN-SETTINGS-MANUAL-COMPLIANCE。

### FS-ID-025 注册、安全开关与 CAPTCHA 设置
- 前置：设置工作区及字段白名单；Registration/Captcha 面板只编辑自己的字段，统一 revision 保存仍归设置工作区。
- 注册/安全输入：注册、邮箱验证、邮箱域配额、后缀白名单、找回密码、frontend_url、TOTP、Passkey、step-up、session binding、审计保留天数；Passkey RP ID/origins/configured 是部署状态展示，未配置时开关禁用，不能虚构此 UI 能修改部署 RP。
- CAPTCHA 输入：Turnstile site/secret，Tencent region/app id/app secret/cloud凭据，Aliyun region/prefix/scene/access凭据及各自开关；configured 指示已有秘密，不将遮罩回填为新秘密。实际 provider proof 仍由登录/注册/找回动作获取。
- 流程/异常：编辑后一次 settings CAS；保持未改字段和 false/0 的既有含义，冲突/未知结果回到工作区审阅。真实验证码服务及邮件不可由本地成功 toast 代替验收。
- 权威与追踪：[RegistrationSettingsPanel](../../../frontend/src/features/identity/settings/RegistrationSettingsPanel.vue)、[CaptchaSettingsPanel](../../../frontend/src/features/identity/settings/CaptchaSettingsPanel.vue)；case：FE-SETTING-REGISTRATIONSETTINGSPANEL-FIELDS、FE-SETTING-REGISTRATIONSETTINGSPANEL-WRITE、FE-SETTING-CAPTCHASETTINGSPANEL-FIELDS、FE-SETTING-CAPTCHASETTINGSPANEL-WRITE、FE-PAGE-ADMIN-SETTINGS-EXTERNAL-CAPTCHA。

### FS-ID-026 OAuth provider 配置与回调辅助
- 前置：LinuxDo、GitHub/Google、微信、钉钉、OIDC 对应设置面板；敏感字段遵守 configured/留空保持合同，经全局 CAS 保存，面板不存在的“测试登录成功”按钮不得补造。
- 字段与小操作：填写 client/app ID、secret、开关和已有回调字段；LinuxDo、Email OAuth、微信、OIDC 可按当前实际 HTTP base 生成/设置并复制后端 callback URL。展示 api_base_url 不能改变认证请求目的地址。
- 微信：open/mp/mobile 各能力与凭据、模式/scopes、前后端 redirect；mp 与 mobile 同时启用被既有 prepareIdentitySettings 拒绝。钉钉：企业限制策略、绕过注册配置、显示名/企业邮箱/部门的属性映射；不把这些开关解释为前端自行授予登录资格。
- OIDC：issuer/discovery/authorize/token/userinfo/JWKS、scopes、token auth method、clock skew、signing algorithms、PKCE、ID token 校验、verified email 及 userinfo 路径；仅说明配置与保存，不伪造 discovery/令牌验证结果。
- 权威与追踪：[认证设置模块](../../../frontend/src/features/identity/settings)、[回调 URL 构造](../../../frontend/src/features/identity/settings/callbackUrl.ts)；case：FE-SETTING-LINUXDOSETTINGSPANEL-FIELDS、FE-SETTING-LINUXDOSETTINGSPANEL-WRITE、FE-SETTING-EMAILOAUTHSETTINGSPANEL-FIELDS、FE-SETTING-EMAILOAUTHSETTINGSPANEL-WRITE、FE-SETTING-WECHATSETTINGSPANEL-FIELDS、FE-SETTING-WECHATSETTINGSPANEL-WRITE、FE-SETTING-DINGTALKSETTINGSPANEL-FIELDS、FE-SETTING-DINGTALKSETTINGSPANEL-WRITE、FE-SETTING-OIDCSETTINGSPANEL-FIELDS、FE-SETTING-OIDCSETTINGSPANEL-WRITE、FE-PAGE-ADMIN-SETTINGS-EXTERNAL-OAUTH。

### FS-ID-027 用户默认值与认证来源授予
- 前置：UserDefaults/AuthDefaults 面板及对应字段权限；默认值影响未来服务端注册/首次绑定规则，不代表当前列表用户已批量变更。
- 输入：default_balance/default_concurrency/default_user_rpm_limit；email、LinuxDo、OIDC、微信、钉钉、GitHub、Google 各来源的默认 balance/concurrency/subscriptions/platform_quotas，以及 grant_on_signup/grant_on_first_bind；force_email_on_third_party_signup 控制第三方注册是否补邮箱。
- 流程：编辑来源配置、选择注入的可分配订阅及既有期限/平台额度值，提交设置工作区统一 CAS；授予是否发生、是否曾授予及最终资金/订阅事实由服务端决定。
- 异常：缺少可分配选项不生成替代组，空值、0、false 不能机械视为未修改；409/未知结果不重放授予。具体订阅分配、平台限额和余额含义引用第 2/3 章及现有后端合同。
- 权威与追踪：[UserDefaultsSettingsPanel](../../../frontend/src/features/identity/settings/UserDefaultsSettingsPanel.vue)、[AuthDefaultsSettingsPanel](../../../frontend/src/features/identity/settings/AuthDefaultsSettingsPanel.vue)、[来源授予投影](../../../frontend/src/features/identity/settings/authSourceGrants.ts)；case：FE-SETTING-USERDEFAULTSSETTINGSPANEL-FIELDS、FE-SETTING-USERDEFAULTSSETTINGSPANEL-WRITE、FE-SETTING-AUTHDEFAULTSSETTINGSPANEL-FIELDS、FE-SETTING-AUTHDEFAULTSSETTINGSPANEL-WRITE。

### FS-ID-028 已存在的旧管理 API Key 维护入口
- 前置：AdminKeysSettingsPanel 仅当 GET /admin/settings/admin-api-key 返回 exists 时显示折叠维护区；没有旧 Key 时不开放首次生成入口。它与 FS-ID-023 的可授权多 Key 列表、普通用户 Key 均不混用。
- 流程：查看 masked_key，确认重新生成后 POST /admin/settings/admin-api-key/regenerate；返回的新秘密可复制。确认删除时 DELETE /admin/settings/admin-api-key，成功后清状态并隐藏此维护区。
- 异常/取消：仍要求现有权限/step-up；失败不假装已换 Key，作用域失效清秘密并取消请求；此独立命令不通过全局 settings CAS 提交。
- 跨章权威：生产 owner 是第 4 章的 system settings，本章说明安全操作边界；[维护面板](../../../frontend/src/features/system/settings/AdminKeysSettingsPanel.vue)、[系统控制 API](../../../frontend/src/features/system/api/settings.ts)；case：FE-SETTING-ADMINKEYSSETTINGSPANEL-FIELDS、FE-SETTING-ADMINKEYSSETTINGSPANEL-WRITE。

## 用户 API Key 与客户端配置

### FS-KEY-001 我的 Key 查询、列和分页
- 前置：/keys 的用户壳，角色/会话已确认；GET /keys 读取当前主体拥有或后端授权的 Key，不与管理访问 Key 混用。
- 输入/流程：按名称/文本搜索，状态 active/inactive/quota_exhausted/expired、组或明确的“无组”(group_id=0)筛选；排序、分页/每页数量、列显隐、行选择/清空选择与手动刷新。筛选/排序/分页变化清理不适用的选择。
- 结果：展示名称、Key 遮罩、状态、组、配额/已用、滚动窗口及时间字段；用户可显式复制完整 Key，当前表格没有明文显隐切换按钮。用量数据由注入的 usage 查询补充，展示含义归第 3 章，缺失用量不能写成 0。
- 异常：列表失败保留明确 error；组/倍率读取失败不捏造可选组或免费价格；换身份清理列表、选择、指南和草稿，旧结果不能回填。
- 权威与追踪：[KeysPage](../../../frontend/src/features/keys/pages/KeysPage.vue)、[列表查询](../../../frontend/src/features/keys/queries/useKeyList.ts)、[Key API](../../../frontend/src/features/keys/api/keys.ts)；case：FE-PAGE-KEYS-ACTION-1、FE-AUTH-003。

### FS-KEY-002 创建与编辑 Key
- 前置/输入：编辑器绑定当前目标；名称和可用组、状态、可选自定义 Key、IP 白/黑名单、quota、5h/1d/7d 限额、过期设置。创建自定义 Key 必填且至少 16 字符，只允许字母/数字/下划线/短横线；编辑不替换既有秘密。
- 校验：组不得为 null；数值不得为负数或非有限数；IP 文本按行 trim/去空再提交，合法地址/网段由现有后端校验，不能声称前端已做完整 CIDR 验证。开启过期时要求现有日期解析有效。
- 正常流程：创建 POST /keys，编辑 PUT /keys/:id；创建到期转为至少 1 天的 expires_in_days，编辑使用 ISO expires_at，取消过期发空字符串。quota/滚动限额保持本 Key API 的 0/空=不限制合同，不能套用用户平台配额的 0 意义。
- 小操作/异常：支持日期预设与自定义日期、IP/限额开关、组选择的供应商分类；关闭不保存。quota_exhausted/expired 的编辑投影不应随普通保存自动覆写服务端状态，只有明确激活选择才改变；请求失败保留错误，换目标/身份丢弃旧结果。
- 权威与追踪：[编辑器](../../../frontend/src/features/keys/components/KeyEditorDialog.vue)、[草稿及序列化](../../../frontend/src/features/keys/model/keyDraft.ts)、[组分类](../../../frontend/src/features/keys/model/keyGroupProviders.ts)；case：FE-PAGE-KEYS-ACTION-2、FE-AUTH-003。

### FS-KEY-003 状态、改组、用量重置和删除
- 前置：当前可管理的 Key 行/编辑器；每个写操作捕获具体 id 并以服务器返回结果为准，不因读取了 Key 文本自动取得管理权限。
- 正常流程：行内启用/停用与改组经 PUT /keys/:id；删除确认后 DELETE /keys/:id。行内滚动限额重置、编辑器内配额/滚动限额重置分别经确认后 PUT reset_rate_limit_usage:true 或 reset_quota:true。
- 结果：重置成功用响应更新基线、用量/状态显示并刷新列表；编辑器未提交的其他草稿不能因重置被错误整体保存。重置 Key 计量不等于退款、余额调整或删除使用历史。
- 取消/未知：取消确认框不写入；在途重复点击受 busy 限制；切身份/卸载取消/失效请求，未知结果先读取 Key 当前状态。当前 /keys 没有独立的“批量删除 Key”入口，不能从单删或导入能力推导该功能。
- 权威与追踪：[页面命令](../../../frontend/src/features/keys/pages/KeysPage.vue)、[编辑器独立重置命令](../../../frontend/src/features/keys/components/KeyEditorDialog.vue)；case：FE-PAGE-KEYS-ACTION-2、FE-AUTH-003。

### FS-KEY-004 批量 Key 修改
- 前置：已有选择；打开对话框时复制 ID/name 目标清单。只修改明确启用的组、状态、quota、5h/1d/7d 限额、到期、IP 白/黑名单字段，不设不存在的后端批改原子事务。
- 校验：至少选一个字段；组必须来自当前可选组；启用的数值不能为空且必须有限/非负，到期可选永不过期或可解析日期。未勾选字段完全省略，显式 0/空数组/空到期按原合同清除限制。
- 正常流程：复用逐 Key PUT，最多 5 个并发一批；结果逐 ID 汇总。成功项从待处理/页面选择中移除，失败项保留名称和消息，可由用户审阅后再次提交失败目标。
- 异常：部分成功明确报告成功/失败数，不以一个 toast 声称全成功；处理中不主动关闭，作用域退出使后续批次停止。已完成的远端更新不因剩余请求取消而回滚。
- 权威与追踪：[批改模型](../../../frontend/src/features/keys/model/useBulkKeyEdit.ts)、[bulkUpdate](../../../frontend/src/features/keys/api/keys.ts)；case：FE-PAGE-KEYS-ACTION-2、FE-AUTH-003。现有验收 ID 为综合操作，不表示每种部分失败排列已执行。

### FS-KEY-005 Key 导入与秘密导出
- 前置：用户显式打开对应对话框；导出确认告知文件含完整秘密，范围为选中 IDs，未选择时为当前筛选；选中对象/筛选改变后旧确认失效，须重新确认。
- 导入校验：本地 JSON 文件不超过 5 MiB；允许 UTF-8 BOM，type 必须 sub2api-keys、version=1、keys 为 1–1000 项数组；逐行业务字段由服务器继续校验，不把 envelope 合法当作全部 Key 可导入。
- 正常流程：GET /keys/export 返回既有 v1 格式并下载；POST /keys/import 提交 {data}，显示 created/skipped/failed 与逐项 index/message；有真实创建项才通知列表刷新。导入保持原分组标识、额度/已用和窗口元数据合同，不本地重新算账。
- 异常/取消：空导出、文件解析错误、过大文件、服务端部分失败分别提示；关闭/退出清文件文本和结果。换会话、目标改变或卸载后禁止旧秘密下载；已发送导入结果未知不自动再导入。
- 权威与追踪：[传输流程](../../../frontend/src/features/keys/queries/useKeyTransfer.ts)、[文件包校验](../../../frontend/src/features/keys/model/keyTransfer.ts)、[导入导出 DTO/API](../../../frontend/src/features/keys/api/keys.ts)；case：FE-PAGE-KEYS-ACTION-3、FE-AUTH-003、FE-SEC-004。

### FS-KEY-006 客户端配置与使用指南
- 前置：UseKeyModal 接收明确 apiKey/baseUrl/platform、claudeCodeOnly 和 allowMessagesDispatch；客户端 tab 由实际平台能力决定，不为未知平台编造兼容协议。
- 操作：选择当前支持的 Claude Code、Codex、Codex WS、Gemini CLI、Grok CLI、OpenCode、System One 等 tab；选择 macOS/Linux、Windows CMD/PowerShell 或 Windows 文件模式；切 Codex legacy/api-key 认证配置，复制生成片段或下载实际提供的 config.toml/auth.json 等文件。生成配置不自动安装/覆盖用户本机文件。
- 模型目录：按当前 Key、平台和展示 base 查询注入的 codexModels 服务；有缓存时显示当前 Key 的目录并按既有 60 秒刷新策略更新。可选远程/本地目录，超过 1 MiB 强制本地模式；可重试获取并下载 codex-models.json，首次目录加载中禁止复制/下载依赖其模型的占位文件。
- 异常与秘密：切 Key/身份不共享旧目录或复制状态；目录失败与空目录有区别，复制失败有提示；生成文件包含用户秘密，需要显式下载，不写持久全局缓存或日志。第三方客户端能否真实连接、WS/图片扩展/会话恢复是否成功必须另行验收，规格不从配置文本推断上游能力。
- 权威与追踪：[UseKeyModal](../../../frontend/src/features/keys/components/UseKeyModal.vue)、[生成文件](../../../frontend/src/features/keys/model/guide/useKeyGuideFiles.ts)、[目录生命周期](../../../frontend/src/features/keys/queries/useKeyGuideCatalog.ts)；case：FE-PAGE-KEYS-ACTION-3、FE-BRAND-002、FE-BRAND-003、FE-AUTH-003。模型权威及平台兼容规则另见第 2 章。

### FS-KEY-007 Key/端点复制与显示
- 入口：Key 表格的遮罩/复制和 EndpointPopover；展示地址来自公共 api_base_url 与 custom_endpoints，不替换应用内部传输地址。
- 流程：表格保持 Key 遮罩，点击复制完整值并通过注入的 clipboard 反馈结果；端点浮层列出配置名称、地址及说明，按项复制，移动端/鼠标交互沿组件实际显示方式。
- 异常/边界：复制返回 false 或抛错提示失败，不能显示复制成功；空配置不补硬编码厂商地址。Key 明文仅在获权页面及明确复制/导出/配置操作中使用；/key-usage 的匿名公开查询与此页面分属不同权限入口，见第 3 章。
- 权威与追踪：[KeysTable](../../../frontend/src/features/keys/components/KeysTable.vue)、[EndpointPopover](../../../frontend/src/features/keys/components/EndpointPopover.vue)、[KeysServices](../../../frontend/src/features/keys/services.ts)；case：FE-PAGE-KEYS-ACTION-3、FE-SEC-001、FE-SEC-004。

## 路由、差异与验收追踪补充

### 实际文档路径与操作追踪

下表每一路径均由 docsStaticItems 实际注册，使用 FS-SITE-002/003 的共享导航、刷新/返回、搜索、目录与复制流程；模型行同时引用 FS-SITE-004 和第 2 章。ENTRY/REFRESH/BACK 仍以原目录中的同一路由 case 为准，表中列的是既有主要动作 ID。

| 真实路径 | 内容/分支 | 既有动作 case ID |
| --- | --- | --- |
| /docs | 文档首页 | FE-PAGE-DOCS-ACTION-1、FE-PAGE-DOCS-ACTION-2 |
| /docs/quickstart | API 文章 | FE-PAGE-DOCS-QUICKSTART-ACTION-1、FE-PAGE-DOCS-QUICKSTART-ACTION-2、FE-PAGE-DOCS-QUICKSTART-ACTION-3 |
| /docs/authentication | API 文章 | FE-PAGE-DOCS-AUTHENTICATION-ACTION-1、FE-PAGE-DOCS-AUTHENTICATION-ACTION-2、FE-PAGE-DOCS-AUTHENTICATION-ACTION-3 |
| /docs/api/protocols | API 文章 | FE-PAGE-DOCS-API-PROTOCOLS-ACTION-1、FE-PAGE-DOCS-API-PROTOCOLS-ACTION-2、FE-PAGE-DOCS-API-PROTOCOLS-ACTION-3 |
| /docs/pricing | API 文章 | FE-PAGE-DOCS-PRICING-ACTION-1、FE-PAGE-DOCS-PRICING-ACTION-2、FE-PAGE-DOCS-PRICING-ACTION-3 |
| /docs/errors | API 文章 | FE-PAGE-DOCS-ERRORS-ACTION-1、FE-PAGE-DOCS-ERRORS-ACTION-2、FE-PAGE-DOCS-ERRORS-ACTION-3 |
| /docs/limits | API 文章 | FE-PAGE-DOCS-LIMITS-ACTION-1、FE-PAGE-DOCS-LIMITS-ACTION-2、FE-PAGE-DOCS-LIMITS-ACTION-3 |
| /docs/api/chat/openai-chat | API 文章 | FE-PAGE-DOCS-API-CHAT-OPENAI-CHAT-ACTION-1、FE-PAGE-DOCS-API-CHAT-OPENAI-CHAT-ACTION-2、FE-PAGE-DOCS-API-CHAT-OPENAI-CHAT-ACTION-3 |
| /docs/api/chat/openai-responses | API 文章 | FE-PAGE-DOCS-API-CHAT-OPENAI-RESPONSES-ACTION-1、FE-PAGE-DOCS-API-CHAT-OPENAI-RESPONSES-ACTION-2、FE-PAGE-DOCS-API-CHAT-OPENAI-RESPONSES-ACTION-3 |
| /docs/api/chat/anthropic-messages | API 文章 | FE-PAGE-DOCS-API-CHAT-ANTHROPIC-MESSAGES-ACTION-1、FE-PAGE-DOCS-API-CHAT-ANTHROPIC-MESSAGES-ACTION-2、FE-PAGE-DOCS-API-CHAT-ANTHROPIC-MESSAGES-ACTION-3 |
| /docs/api/chat/gemini-native | API 文章 | FE-PAGE-DOCS-API-CHAT-GEMINI-NATIVE-ACTION-1、FE-PAGE-DOCS-API-CHAT-GEMINI-NATIVE-ACTION-2、FE-PAGE-DOCS-API-CHAT-GEMINI-NATIVE-ACTION-3 |
| /docs/api/chat/grok-native | API 文章 | FE-PAGE-DOCS-API-CHAT-GROK-NATIVE-ACTION-1、FE-PAGE-DOCS-API-CHAT-GROK-NATIVE-ACTION-2、FE-PAGE-DOCS-API-CHAT-GROK-NATIVE-ACTION-3 |
| /docs/api/image/openai-image | API 文章 | FE-PAGE-DOCS-API-IMAGE-OPENAI-IMAGE-ACTION-1、FE-PAGE-DOCS-API-IMAGE-OPENAI-IMAGE-ACTION-2、FE-PAGE-DOCS-API-IMAGE-OPENAI-IMAGE-ACTION-3 |
| /docs/models | 模型列表/详情 | FE-PAGE-DOCS-MODELS-ACTION-1、FE-PAGE-DOCS-MODELS-ACTION-2、FE-PAGE-DOCS-MODELS-ACTION-3 |
| /docs/api/query/models | API 文章 | FE-PAGE-DOCS-API-QUERY-MODELS-ACTION-1、FE-PAGE-DOCS-API-QUERY-MODELS-ACTION-2、FE-PAGE-DOCS-API-QUERY-MODELS-ACTION-3 |
| /docs/api/query/token-count | API 文章 | FE-PAGE-DOCS-API-QUERY-TOKEN-COUNT-ACTION-1、FE-PAGE-DOCS-API-QUERY-TOKEN-COUNT-ACTION-2、FE-PAGE-DOCS-API-QUERY-TOKEN-COUNT-ACTION-3 |
| /docs/api/query/embeddings | API 文章 | FE-PAGE-DOCS-API-QUERY-EMBEDDINGS-ACTION-1、FE-PAGE-DOCS-API-QUERY-EMBEDDINGS-ACTION-2、FE-PAGE-DOCS-API-QUERY-EMBEDDINGS-ACTION-3 |
| /apps | Apps 文章 | FE-PAGE-APPS-ACTION-1、FE-PAGE-APPS-ACTION-2、FE-PAGE-APPS-ACTION-3 |
| /apps/console | Apps 文章 | FE-PAGE-APPS-CONSOLE-ACTION-1、FE-PAGE-APPS-CONSOLE-ACTION-2、FE-PAGE-APPS-CONSOLE-ACTION-3 |
| /apps/codex | Apps 文章 | FE-PAGE-APPS-CODEX-ACTION-1、FE-PAGE-APPS-CODEX-ACTION-2、FE-PAGE-APPS-CODEX-ACTION-3 |
| /apps/claude-code | Apps 文章 | FE-PAGE-APPS-CLAUDE-CODE-ACTION-1、FE-PAGE-APPS-CLAUDE-CODE-ACTION-2、FE-PAGE-APPS-CLAUDE-CODE-ACTION-3 |
| /apps/claude-desktop | Apps 文章 | FE-PAGE-APPS-CLAUDE-DESKTOP-ACTION-1、FE-PAGE-APPS-CLAUDE-DESKTOP-ACTION-2、FE-PAGE-APPS-CLAUDE-DESKTOP-ACTION-3 |
| /apps/session-recovery-codex | Apps 文章 | FE-PAGE-APPS-SESSION-RECOVERY-CODEX-ACTION-1、FE-PAGE-APPS-SESSION-RECOVERY-CODEX-ACTION-2、FE-PAGE-APPS-SESSION-RECOVERY-CODEX-ACTION-3 |
| /apps/session-recovery-claude | Apps 文章 | FE-PAGE-APPS-SESSION-RECOVERY-CLAUDE-ACTION-1、FE-PAGE-APPS-SESSION-RECOVERY-CLAUDE-ACTION-2、FE-PAGE-APPS-SESSION-RECOVERY-CLAUDE-ACTION-3 |
| /apps/image-skills | Apps 文章 | FE-PAGE-APPS-IMAGE-SKILLS-ACTION-1、FE-PAGE-APPS-IMAGE-SKILLS-ACTION-2、FE-PAGE-APPS-IMAGE-SKILLS-ACTION-3 |
| /apps/downloads | Apps 文章 | FE-PAGE-APPS-DOWNLOADS-ACTION-1、FE-PAGE-APPS-DOWNLOADS-ACTION-2、FE-PAGE-APPS-DOWNLOADS-ACTION-3 |
| /docs/models/:modelId | 模型列表/详情 | FE-PAGE-DOCS-MODELS-MODELID-ACTION-1、FE-PAGE-DOCS-MODELS-MODELID-ACTION-2、FE-PAGE-DOCS-MODELS-MODELID-ACTION-3 |

### 既有文档 redirect 与 OAuth callback 参数

文档重定向保留公开书签；这些是 redirect，不是新增功能页。/docs/apps/console 没有在当前 legacyAppRedirects 中注册，不能凭规则猜出该路径。

| 源路径 | 已登记目标 |
| --- | --- |
| /apps/session-recovery | /apps/session-recovery-codex |
| /docs/apps/session-recovery | /apps/session-recovery-codex |
| /docs/apps | /apps |
| /docs/apps/codex | /apps/codex |
| /docs/apps/claude-code | /apps/claude-code |
| /docs/apps/claude-desktop | /apps/claude-desktop |
| /docs/apps/session-recovery-codex | /apps/session-recovery-codex |
| /docs/apps/session-recovery-claude | /apps/session-recovery-claude |
| /docs/apps/image-skills | /apps/image-skills |
| /docs/downloads | /apps/downloads |

OAuth 回调入口依各 provider 的既有分支处理；以下只记录参数名称，不保留实际 code/token。回调重复进入、刷新/返回和真实 provider 往返分别属于 ACTION/EXTERNAL 用例。

| 回调路径 | query / fragment 字段 | 既有分支/真实往返 case ID |
| --- | --- | --- |
| /auth/callback | query: code, state, error, error_description；fragment: access_token, refresh_token, expires_in, token_type, error, error_description, error_message, redirect | FE-PAGE-AUTH-CALLBACK-ACTION-2、FE-PAGE-AUTH-CALLBACK-EXTERNAL-OAUTH |
| /auth/linuxdo/callback | query: redirect；fragment: error, error_description, error_message, redirect, access_token, refresh_token, expires_in, token_type | FE-PAGE-AUTH-LINUXDO-CALLBACK-ACTION-2、FE-PAGE-AUTH-LINUXDO-CALLBACK-EXTERNAL-OAUTH |
| /auth/wechat/callback | query: mode, redirect, email, wechat_bind_existing；fragment: error, error_description, error_message, redirect, access_token, refresh_token, expires_in, token_type | FE-PAGE-AUTH-WECHAT-CALLBACK-ACTION-2、FE-PAGE-AUTH-WECHAT-CALLBACK-EXTERNAL-OAUTH |
| /auth/dingtalk/callback | query: redirect, bind, email；fragment: error, error_description, error_message, redirect, access_token, refresh_token, expires_in, token_type | FE-PAGE-AUTH-DINGTALK-CALLBACK-ACTION-2、FE-PAGE-AUTH-DINGTALK-CALLBACK-EXTERNAL-OAUTH |
| /auth/dingtalk/email-completion | query: email, redirect；fragment: access_token, refresh_token, expires_in, token_type, error, error_description, error_message, redirect | FE-PAGE-AUTH-DINGTALK-EMAIL-COMPLETION-ACTION-2、FE-PAGE-AUTH-DINGTALK-EMAIL-COMPLETION-EXTERNAL-OAUTH |
| /auth/oidc/callback | query: redirect；fragment: error, error_description, error_message, redirect, access_token, refresh_token, expires_in, token_type | FE-PAGE-AUTH-OIDC-CALLBACK-ACTION-2、FE-PAGE-AUTH-OIDC-CALLBACK-EXTERNAL-OAUTH |

### 重构前路径与当前唯一归属

下表保留原始实现与本版的路径/端点归属对照；本版逐路径维护映射见 source-review.json，原始比较输入仍为 6f。路径迁移不等于业务已全部验收。

| 旧 frontend/src 路径 | 当前实现/公开归属 | 保留的行为与本章边界 |
| --- | --- | --- |
| views/HomeView.vue、views/docs/* | [features/site/pages](../../../frontend/src/features/site/pages)、[site/docs](../../../frontend/src/features/site/docs) | 品牌展示、公开文档/Apps；路由仍由真实导航注册，模型数据权威跨章引用 |
| views/user/CustomPageView.vue | [app/compositions/custom-page](../../../frontend/src/app/compositions/custom-page) + [site/customPages](../../../frontend/src/features/site/customPages.ts) | 菜单/actor 是 app 组合，Markdown 读取归 site；外链不携带 token |
| stores/announcements.ts、api/announcements.ts、api/admin/announcements.ts、views/admin/AnnouncementsView.vue | [features/site/announcements](../../../frontend/src/features/site/announcements.ts)、[site/api](../../../frontend/src/features/site/api)、[公告页](../../../frontend/src/features/site/pages/AnnouncementsPage.vue) | 读/已读/受众/管理命令保持原端点；队列和异步失效绑定身份 |
| stores/auth.ts、api/auth.ts、views/auth/* | [features/identity/index](../../../frontend/src/features/identity/index.ts)、[identity/model/auth](../../../frontend/src/features/identity/model/auth.ts)、[identity/pages](../../../frontend/src/features/identity/pages) | 登录、协议、pending/adoption、权限和跨 tab session 保留；公共设置唯一 owner 转至 site |
| composables/useStepUp.ts | [identity/composables/useStepUp](../../../frontend/src/features/identity/composables/useStepUp.ts) | 取消/会话替换后不恢复旧命令；共享 HTTP 收取调用方 signal |
| api/user.ts、api/totp.ts、api/passkey.ts、views/user/ProfileView.vue、components/user/profile/* | [identity/api](../../../frontend/src/features/identity/api)、[identity/profile](../../../frontend/src/features/identity/components/profile) | profile/绑定/安全归身份；原 user.ts 中余额历史、平台配额不被吞进身份 API |
| api/admin/users.ts、views/admin/UsersView.vue、components/admin/user/* | [identity/api/users](../../../frontend/src/features/identity/api/users.ts)、[UsersPage](../../../frontend/src/features/identity/pages/UsersPage.vue)、[user-directory 组合](../../../frontend/src/app/compositions/user-directory) | 身份命令独立；金额、组准入、用户 Key、用量历史分别调用实际 owner |
| api/admin/adminAccessKeys.ts、api/admin/compliance.ts、stores/adminCompliance.ts | [管理访问 Key](../../../frontend/src/features/identity/api/adminAccessKeys.ts)、[compliance](../../../frontend/src/features/identity/api/compliance.ts)、[合规 store](../../../frontend/src/features/identity/model/adminCompliance.ts) | 一次性秘密、手动承诺、step-up 保留；旧单 Key 维护明确留在 system，不宣称退役 |
| api/keys.ts、views/user/KeysView.vue | [features/keys/api/keys](../../../frontend/src/features/keys/api/keys.ts)、[KeysPage](../../../frontend/src/features/keys/pages/KeysPage.vue)、[keys/components](../../../frontend/src/features/keys/components) | /keys、导入/导出 v1 和编辑 DTO 保留；查询、草稿、限制、配置生成分开拥有 |
| api/admin/apiKeys.ts | [features/keys/admin](../../../frontend/src/features/keys/admin.ts) | 普通用户 Key 的管理改组，不是管理访问 Key；/admin/api-keys/:id payload 不变 |

当前前后差异中可直接确认的运行变化是更明确的会话/目标取消链、秘密寿命和独立 owner；本规格不把这些修复描述成新增后端 CRUD/支付能力。当前原 schema/DTO/URL 是合同基线，不能为了规格表齐全添加操作。

### 本地 fixture 与外部前置

fixture 的实际准备步骤、字段和清理方式引用 [FIXTURES](../../../frontend/tests/acceptance/FIXTURES.md)，下表仅分配本章功能使用既有 fixture，不复制新状态库。

| 适用功能 | 必备既有 fixture | 必须准备的分支 |
| --- | --- | --- |
| 全章角色/作用域 | local-runtime、session-roles | anonymous、普通用户 A/B、同用户新 session、super_admin、获页面/字段权限的 admin、无权/只读 admin、supplier origin |
| FS-SITE-001 至 FS-SITE-006/009 | site-content、catalog-groups、settings-revision（设置动作） | 空/自定义品牌、四语言、合法/危险 URL、存在/缺失正文/模型、可见/不可见菜单、版本冲突 |
| FS-SITE-007/008 | site-content、admin-compliance | draft/active/archived、silent/popup、读/未读、全部/条件受众、合法和不可销毁公告目标 |
| FS-ID-002 至 FS-ID-010 | auth-local、registration-mail、pending-oauth | 正确/错误/禁用账号、协议/CAPTCHA 开关、邮箱码错误/过期/冷却、各 provider pending/绑定/MFA/adoption/重复完成 |
| FS-ID-011 至 FS-ID-016 | profile-security | 可编辑资料、绑定/解绑限制、图片大小/类型、TOTP 启用/关闭、Passkey 不支持/取消、通知邮箱验证分支 |
| FS-ID-017 至 FS-ID-021 | user-directory | user/admin/super_admin/供应商显示样本、只读字段、可销毁目标、自定义属性、组/Key/配额与资金历史；跨域金额/订阅操作沿对应章节 fixture |
| FS-ID-022 至 FS-ID-028、FS-SITE-009 | settings-revision、admin-compliance、session-roles | policy_version/revision 冲突、未配置秘密、管理 Key 轮换/撤销、旧单 Key exists/不存在、需 step-up 及取消 |
| FS-KEY-001 至 FS-KEY-007 | keys、catalog-groups | active/inactive/expired/quota_exhausted、无组/可分配组、未提供/为零用量、导入合法/非法/部分失败、目录大/空/失败、A/B 作用域 |

- 每个实际页面均要执行现有 ENTRY、REFRESH、BACK 分支；对话框还要执行关闭/取消、失败、重新打开及目标变化，不能以打开页面截图代替命令验收。
- OAuth/绑定需获准 provider 应用、真实回调与测试身份；CAPTCHA 和邮件需相应 sandbox/收信能力；Passkey 真实路径需要 HTTPS/正确 RP origin 和获准 authenticator；外链/iframe/下载需获准出口和外部资源。
- 条件不足时按现有 result schema 记录 blocked 及具体原因；未执行保持 not_run。没有新增 blocked_by_external_dependency 状态，本章只把“外部依赖不足”作为阻塞原因说明。
- 真实管理员合规接受必须人工授权执行；资金/支付/真实上游动作不在本章自动执行。所有证据使用 fixture 引用，不能存放 password、token、Key 明文、真实 callback URL 中的秘密。

### 已发现的规格/用例粒度差异与未验收项

1. FE-PAGE-ADMIN-USERS-ACTION-3 描述含“订阅面板”，当前 UsersPage 只有订阅列和跨域用户面板，未接订阅分配弹窗；本文按源码写明界限，原 cases.json 留给其 owner 校正或细化。
2. 个人密码组件至少 8 字符，注册/登录/找回页面至少 6 字符；当前前端不是统一密码长度策略。服务端仍权威，本章不改代码或擅改规则。
3. 并发单元格只读、Key 表格没有明文切换、Key 批改没有批量删除入口；不能从 API 方法存在或笼统 CRUD 描述推导可执行 UI。
4. 公告用户端细分已读操作、头像、各属性类型、管理 Key 的各策略维度及客户端配置组合多映射到综合 case；准确引用父 case 不代表这些子分支已经逐一验收，也不在本文件发明 case ID。
5. 原 Key/用户/角色 API 依然以服务器角色、字段及目标权限为准；路由进入、前端隐藏按钮、测试 fixture 成功均不能证明后端权限完成全部验收。
6. 本文交付是源码规格和文档路径/case 引用核对，不新增通过回执；完整开发测试、Docker、逐页和外部验收继续以根工作包/TEST_REPORT 及 [执行规范](../../../frontend/tests/acceptance/EXECUTION.md) 为唯一记录。
