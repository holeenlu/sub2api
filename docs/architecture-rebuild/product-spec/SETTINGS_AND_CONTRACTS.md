# 设置面板与公共交互合同

最终前端源码已停写并冻结为 `e5a6080563419821daef5eb96e29a95b95f8d80c722b2f8af68dca762878277d`（2009文件、18,901,651 bytes）。c2维护35路径的退休、显式模型、metadata与类型修复已应用；完整测试/双构建的真实输入保留63fe，按唯一测试类型补丁的等价证明复用于e5，五项static直接绑定e5。当前catalog为753用例、92页面、51面板、10管理设置tab，业务定义全部not_run；后端及联合Docker/UI/外部验收尚未完成。

本附录依据当前catalog列出51个面板/子视图，已删除退役BPSDefaultsPanel及其两条用例引用，区分聚合草稿、独立命令和混合面板。精确字段/校验由领域投影与实际API DTO负责；表中输入范围不构成一套新schema。

## 1 工作区与保存语义

完整SettingsWorkspace仅供super_admin；普通admin走DelegatedSettings，受页面、操作及字段白名单约束。当前管理设置为10个tab、51个面板，独立bps tab和默认模板已删除；全站BPS gateway开关移到features、位于图片预算前，仍通过原settings revision CAS一次保存，不新增独立默认模板命令。供应商门户的11个tab是另一目录。最终前端source已交接；后端与联合业务验收仍待完成。

- 聚合：GET /api/v1/admin/settings读取合法revision；字段级草稿经各域校验后，一次PUT携带原revision与实际修改字段。省略字段保留，false/0/null/空串按字段合同处理。
- 独立：使用相应面板的API和按钮，不能由全局保存重复执行，也不承诺与设置PUT组成事务。
- 委托：DelegatedSettings使用现有设置revision写入，只发送被授权字段；复用事务和冲突审阅控件，但只显示可读冲突字段、只合并可写字段。分区保存保留未提交/提交后草稿；保存成功后的公共配置刷新失败单独反馈，不重放PUT。超管保存后的管理员设置/支付配置刷新也显式传播失败；后台导航读取仍可安全失败，投影重试不重新保存或覆盖新草稿。
- 混合：Providers的全局方式字段进入聚合PUT，实例CRUD、启停和排序独立；SMTP设置字段进入聚合PUT，连接检查/测试邮件独立；WebSearch等控制也以实际独立API为准。

来源：[SettingsWorkspace](../../../frontend/src/app/compositions/settings/SettingsWorkspace.vue)、[preparePatch](../../../frontend/src/app/compositions/settings/preparePatch.ts)、[writableFields](../../../frontend/src/app/compositions/settings/writableFields.ts)、[transaction](../../../frontend/src/app/compositions/settings/transaction.ts)。

## 2 面板清单（当前catalog：51个）

| 面板 | 主要输入和功能范围 | 保存边界 | 实现 | 验收引用 |
| --- | --- | --- | --- | --- |
| AdminRolePermissions | 管理员页面/操作/字段策略和策略版本 | 独立命令 | [源码](../../../frontend/src/features/identity/panels/AdminRolePermissions.vue) | FE-SETTING-ADMINROLEPERMISSIONS-FIELDS, FE-SETTING-ADMINROLEPERMISSIONS-WRITE |
| ConsoleSettingsPanel | 控制台运行模式、前端入口、供应商门户及表格偏好 | 聚合草稿 | [源码](../../../frontend/src/features/system/settings/ConsoleSettingsPanel.vue) | FE-SETTING-CONSOLESETTINGSPANEL-FIELDS, FE-SETTING-CONSOLESETTINGSPANEL-WRITE |
| AppearanceSettingsPanel | 站点名称、Logo、文案、API展示/联系/文档链接和首页内容 | 聚合草稿 | [源码](../../../frontend/src/features/site/settings/AppearanceSettingsPanel.vue) | FE-SETTING-APPEARANCESETTINGSPANEL-FIELDS, FE-SETTING-APPEARANCESETTINGSPANEL-WRITE |
| NavigationSettingsPanel | 自定义菜单与公开端点说明 | 聚合草稿 | [源码](../../../frontend/src/features/site/settings/NavigationSettingsPanel.vue) | FE-SETTING-NAVIGATIONSETTINGSPANEL-FIELDS, FE-SETTING-NAVIGATIONSETTINGSPANEL-WRITE |
| AgreementsSettingsPanel | 登录协议文档、展示方式和版本 | 聚合草稿 | [源码](../../../frontend/src/features/site/settings/AgreementsSettingsPanel.vue) | FE-SETTING-AGREEMENTSSETTINGSPANEL-FIELDS, FE-SETTING-AGREEMENTSSETTINGSPANEL-WRITE |
| AdminKeysSettingsPanel | 历史单一管理Key的存在状态、轮换与删除 | 独立命令；历史Key不存在时不展示轮换/删除 | [源码](../../../frontend/src/features/system/settings/AdminKeysSettingsPanel.vue) | FE-SETTING-ADMINKEYSSETTINGSPANEL-FIELDS, FE-SETTING-ADMINKEYSSETTINGSPANEL-WRITE |
| RegistrationSettingsPanel | 注册、验证、找回密码与邮箱域约束 | 聚合草稿 | [源码](../../../frontend/src/features/identity/settings/RegistrationSettingsPanel.vue) | FE-SETTING-REGISTRATIONSETTINGSPANEL-FIELDS, FE-SETTING-REGISTRATIONSETTINGSPANEL-WRITE |
| ClientIpSettingsPanel | 可信代理与客户端IP规则 | 聚合草稿 | [源码](../../../frontend/src/features/access-policy/settings/ClientIpSettingsPanel.vue) | FE-SETTING-CLIENTIPSETTINGSPANEL-FIELDS, FE-SETTING-CLIENTIPSETTINGSPANEL-WRITE |
| PanelRateLimitSettingsPanel | 控制台请求速率与独立限流规则 | 独立命令 | [源码](../../../frontend/src/features/access-policy/settings/PanelRateLimitSettingsPanel.vue) | FE-SETTING-PANELRATELIMITSETTINGSPANEL-FIELDS, FE-SETTING-PANELRATELIMITSETTINGSPANEL-WRITE |
| CaptchaSettingsPanel | Turnstile、阿里云、腾讯等验证码设置 | 聚合草稿 | [源码](../../../frontend/src/features/identity/settings/CaptchaSettingsPanel.vue) | FE-SETTING-CAPTCHASETTINGSPANEL-FIELDS, FE-SETTING-CAPTCHASETTINGSPANEL-WRITE |
| LinuxDoSettingsPanel | LinuxDo登录源配置 | 聚合草稿 | [源码](../../../frontend/src/features/identity/settings/LinuxDoSettingsPanel.vue) | FE-SETTING-LINUXDOSETTINGSPANEL-FIELDS, FE-SETTING-LINUXDOSETTINGSPANEL-WRITE |
| EmailOAuthSettingsPanel | 邮箱/相关OAuth认证源配置 | 聚合草稿 | [源码](../../../frontend/src/features/identity/settings/EmailOAuthSettingsPanel.vue) | FE-SETTING-EMAILOAUTHSETTINGSPANEL-FIELDS, FE-SETTING-EMAILOAUTHSETTINGSPANEL-WRITE |
| WeChatSettingsPanel | 微信登录源配置 | 聚合草稿 | [源码](../../../frontend/src/features/identity/settings/WeChatSettingsPanel.vue) | FE-SETTING-WECHATSETTINGSPANEL-FIELDS, FE-SETTING-WECHATSETTINGSPANEL-WRITE |
| DingTalkSettingsPanel | 钉钉登录源配置 | 聚合草稿 | [源码](../../../frontend/src/features/identity/settings/DingTalkSettingsPanel.vue) | FE-SETTING-DINGTALKSETTINGSPANEL-FIELDS, FE-SETTING-DINGTALKSETTINGSPANEL-WRITE |
| OidcSettingsPanel | OIDC发现/客户端/回调及声明映射配置 | 聚合草稿 | [源码](../../../frontend/src/features/identity/settings/OidcSettingsPanel.vue) | FE-SETTING-OIDCSETTINGSPANEL-FIELDS, FE-SETTING-OIDCSETTINGSPANEL-WRITE |
| AdminAccessKeysSettings | 管理访问Key的范围、创建、轮换、吊销及记录 | 独立命令 | [源码](../../../frontend/src/features/identity/settings/AdminAccessKeysSettings.vue) | FE-SETTING-ADMINACCESSKEYSSETTINGS-FIELDS, FE-SETTING-ADMINACCESSKEYSSETTINGS-WRITE |
| UserDefaultsSettingsPanel | 用户默认余额、并发和速率 | 聚合草稿 | [源码](../../../frontend/src/features/identity/settings/UserDefaultsSettingsPanel.vue) | FE-SETTING-USERDEFAULTSSETTINGSPANEL-FIELDS, FE-SETTING-USERDEFAULTSSETTINGSPANEL-WRITE |
| DefaultQuotaSettingsPanel | 默认平台配额 | 聚合草稿 | [源码](../../../frontend/src/features/access-policy/settings/DefaultQuotaSettingsPanel.vue) | FE-SETTING-DEFAULTQUOTASETTINGSPANEL-FIELDS, FE-SETTING-DEFAULTQUOTASETTINGSPANEL-WRITE |
| DefaultSubscriptionsSettingsPanel | 默认订阅分配 | 聚合草稿 | [源码](../../../frontend/src/features/subscriptions/settings/DefaultSubscriptionsSettingsPanel.vue) | FE-SETTING-DEFAULTSUBSCRIPTIONSSETTINGSPANEL-FIELDS, FE-SETTING-DEFAULTSUBSCRIPTIONSSETTINGSPANEL-WRITE |
| AuthDefaultsSettingsPanel | 各认证来源默认授予与首次绑定/注册条件 | 聚合草稿 | [源码](../../../frontend/src/features/identity/settings/AuthDefaultsSettingsPanel.vue) | FE-SETTING-AUTHDEFAULTSSETTINGSPANEL-FIELDS, FE-SETTING-AUTHDEFAULTSSETTINGSPANEL-WRITE |
| OverloadCooldownSettingsPanel | 过载后的账号冷却规则 | 独立命令 | [源码](../../../frontend/src/features/resources/settings/OverloadCooldownSettingsPanel.vue) | FE-SETTING-OVERLOADCOOLDOWNSETTINGSPANEL-FIELDS, FE-SETTING-OVERLOADCOOLDOWNSETTINGSPANEL-WRITE |
| RateLimitCooldownSettingsPanel | 上游限流后的冷却规则 | 独立命令 | [源码](../../../frontend/src/features/resources/settings/RateLimitCooldownSettingsPanel.vue) | FE-SETTING-RATELIMITCOOLDOWNSETTINGSPANEL-FIELDS, FE-SETTING-RATELIMITCOOLDOWNSETTINGSPANEL-WRITE |
| StreamTimeoutSettingsPanel | 流式超时相关规则 | 独立命令 | [源码](../../../frontend/src/features/resources/settings/StreamTimeoutSettingsPanel.vue) | FE-SETTING-STREAMTIMEOUTSETTINGSPANEL-FIELDS, FE-SETTING-STREAMTIMEOUTSETTINGSPANEL-WRITE |
| RectifierSettingsPanel | 请求修复/纠正规则 | 独立命令 | [源码](../../../frontend/src/features/resources/settings/RectifierSettingsPanel.vue) | FE-SETTING-RECTIFIERSETTINGSPANEL-FIELDS, FE-SETTING-RECTIFIERSETTINGSPANEL-WRITE |
| BetaPolicySettingsPanel | Beta请求头策略 | 独立命令 | [源码](../../../frontend/src/features/access-policy/settings/BetaPolicySettingsPanel.vue) | FE-SETTING-BETAPOLICYSETTINGSPANEL-FIELDS, FE-SETTING-BETAPOLICYSETTINGSPANEL-WRITE |
| FastPolicySettingsPanel | Fast请求/计费相关配置 | 聚合草稿 | [源码](../../../frontend/src/features/access-policy/settings/FastPolicySettingsPanel.vue) | FE-SETTING-FASTPOLICYSETTINGSPANEL-FIELDS, FE-SETTING-FASTPOLICYSETTINGSPANEL-WRITE |
| ClaudeVersionSettingsPanel | Claude客户端版本同步相关配置 | 聚合草稿 | [源码](../../../frontend/src/features/resources/settings/ClaudeVersionSettingsPanel.vue) | FE-SETTING-CLAUDEVERSIONSETTINGSPANEL-FIELDS, FE-SETTING-CLAUDEVERSIONSETTINGSPANEL-WRITE |
| CodexClientsSettingsPanel | Codex客户端识别与兼容配置 | 聚合草稿 | [源码](../../../frontend/src/features/access-policy/settings/CodexClientsSettingsPanel.vue) | FE-SETTING-CODEXCLIENTSSETTINGSPANEL-FIELDS, FE-SETTING-CODEXCLIENTSSETTINGSPANEL-WRITE |
| BillingProbeSettingsPanel | 上游计费倍率探测配置与独立动作 | 独立命令 | [源码](../../../frontend/src/features/resources/settings/BillingProbeSettingsPanel.vue) | FE-SETTING-BILLINGPROBESETTINGSPANEL-FIELDS, FE-SETTING-BILLINGPROBESETTINGSPANEL-WRITE |
| OllamaUsageSettingsPanel | Ollama用量相关配置 | 独立命令 | [源码](../../../frontend/src/features/resources/settings/OllamaUsageSettingsPanel.vue) | FE-SETTING-OLLAMAUSAGESETTINGSPANEL-FIELDS, FE-SETTING-OLLAMAUSAGESETTINGSPANEL-WRITE |
| OpenCodeUsageSettingsPanel | OpenCode用量相关配置 | 独立命令 | [源码](../../../frontend/src/features/resources/settings/OpenCodeUsageSettingsPanel.vue) | FE-SETTING-OPENCODEUSAGESETTINGSPANEL-FIELDS, FE-SETTING-OPENCODEUSAGESETTINGSPANEL-WRITE |
| SchedulingSettingsPanel | 调度、客户端或会话相关配置 | 聚合草稿 | [源码](../../../frontend/src/features/resources/settings/SchedulingSettingsPanel.vue) | FE-SETTING-SCHEDULINGSETTINGSPANEL-FIELDS, FE-SETTING-SCHEDULINGSETTINGSPANEL-WRITE |
| WebSearchSettingsPanel | 搜索提供者配置、检查及用量重置 | 独立命令 | [源码](../../../frontend/src/features/resources/settings/WebSearchSettingsPanel.vue) | FE-SETTING-WEBSEARCHSETTINGSPANEL-FIELDS, FE-SETTING-WEBSEARCHSETTINGSPANEL-WRITE |
| ErrorVisibilitySettingsPanel | 错误输出/日志显示范围 | 聚合草稿 | [源码](../../../frontend/src/features/operations/settings/ErrorVisibilitySettingsPanel.vue) | FE-SETTING-ERRORVISIBILITYSETTINGSPANEL-FIELDS, FE-SETTING-ERRORVISIBILITYSETTINGSPANEL-WRITE |
| BpsImagesSettingsPanel | BPS图片路径相关配置 | 聚合草稿 | [源码](../../../frontend/src/features/resources/settings/BpsImagesSettingsPanel.vue) | FE-SETTING-BPSIMAGESSETTINGSPANEL-FIELDS, FE-SETTING-BPSIMAGESSETTINGSPANEL-WRITE |
| ChannelMonitorSettingsPanel | 渠道监控展示和刷新设置 | 聚合草稿 | [源码](../../../frontend/src/features/operations/settings/ChannelMonitorSettingsPanel.vue) | FE-SETTING-CHANNELMONITORSETTINGSPANEL-FIELDS, FE-SETTING-CHANNELMONITORSETTINGSPANEL-WRITE |
| AvailableChannelsSettingsPanel | 可用渠道功能开关 | 聚合草稿 | [源码](../../../frontend/src/features/catalog/settings/AvailableChannelsSettingsPanel.vue) | FE-SETTING-AVAILABLECHANNELSSETTINGSPANEL-FIELDS, FE-SETTING-AVAILABLECHANNELSSETTINGSPANEL-WRITE |
| ModelPlazaSettingsPanel | 模型广场功能与访问要求 | 聚合草稿 | [源码](../../../frontend/src/features/catalog/settings/ModelPlazaSettingsPanel.vue) | FE-SETTING-MODELPLAZASETTINGSPANEL-FIELDS, FE-SETTING-MODELPLAZASETTINGSPANEL-WRITE |
| BillingModeSettingsPanel | 站点计费展示模式 | 聚合草稿 | [源码](../../../frontend/src/features/pricing/settings/BillingModeSettingsPanel.vue) | FE-SETTING-BILLINGMODESETTINGSPANEL-FIELDS, FE-SETTING-BILLINGMODESETTINGSPANEL-WRITE |
| PluginFeatureSettingsPanel | 插件管理入口功能开关 | 聚合草稿 | [源码](../../../frontend/src/features/system/settings/PluginFeatureSettingsPanel.vue) | FE-SETTING-PLUGINFEATURESETTINGSPANEL-FIELDS, FE-SETTING-PLUGINFEATURESETTINGSPANEL-WRITE |
| RiskControlSettingsPanel | 风控功能和规则相关开关 | 聚合草稿 | [源码](../../../frontend/src/features/access-policy/settings/RiskControlSettingsPanel.vue) | FE-SETTING-RISKCONTROLSETTINGSPANEL-FIELDS, FE-SETTING-RISKCONTROLSETTINGSPANEL-WRITE |
| PaymentSettingsPanel | 支付可用性、订单/充值条件及公共说明 | 聚合草稿 | [源码](../../../frontend/src/features/payments/settings/PaymentSettingsPanel.vue) | FE-SETTING-PAYMENTSETTINGSPANEL-FIELDS, FE-SETTING-PAYMENTSETTINGSPANEL-WRITE |
| RechargeBonusSettingsPanel | 充值赠送规则 | 聚合草稿 | [源码](../../../frontend/src/features/payments/settings/RechargeBonusSettingsPanel.vue) | FE-SETTING-RECHARGEBONUSSETTINGSPANEL-FIELDS, FE-SETTING-RECHARGEBONUSSETTINGSPANEL-WRITE |
| ProvidersSettingsPanel | 全局支付方式草稿及独立Provider实例命令 | 聚合字段＋独立操作 | [源码](../../../frontend/src/features/payments/settings/ProvidersSettingsPanel.vue) | FE-SETTING-PROVIDERSSETTINGSPANEL-FIELDS, FE-SETTING-PROVIDERSSETTINGSPANEL-WRITE |
| SmtpSettingsPanel | SMTP主机/端口/认证/发件人/TLS；独立测试动作 | 聚合字段＋独立操作 | [源码](../../../frontend/src/features/system/settings/SmtpSettingsPanel.vue) | FE-SETTING-SMTPSETTINGSPANEL-FIELDS, FE-SETTING-SMTPSETTINGSPANEL-WRITE |
| ExpiryNotificationSettingsPanel | 订阅到期通知 | 聚合草稿 | [源码](../../../frontend/src/features/subscriptions/settings/ExpiryNotificationSettingsPanel.vue) | FE-SETTING-EXPIRYNOTIFICATIONSETTINGSPANEL-FIELDS, FE-SETTING-EXPIRYNOTIFICATIONSETTINGSPANEL-WRITE |
| BalanceNotificationSettingsPanel | 余额通知条件 | 聚合草稿 | [源码](../../../frontend/src/features/payments/settings/BalanceNotificationSettingsPanel.vue) | FE-SETTING-BALANCENOTIFICATIONSETTINGSPANEL-FIELDS, FE-SETTING-BALANCENOTIFICATIONSETTINGSPANEL-WRITE |
| QuotaNotificationSettingsPanel | 资源配额通知 | 聚合草稿 | [源码](../../../frontend/src/features/resources/settings/QuotaNotificationSettingsPanel.vue) | FE-SETTING-QUOTANOTIFICATIONSETTINGSPANEL-FIELDS, FE-SETTING-QUOTANOTIFICATIONSETTINGSPANEL-WRITE |
| EmailTemplateEditor | 事件/语言邮件模板、预览和恢复官方模板 | 独立命令 | [源码](../../../frontend/src/features/system/settings/EmailTemplateEditor.vue) | FE-SETTING-EMAILTEMPLATEEDITOR-FIELDS, FE-SETTING-EMAILTEMPLATEEDITOR-WRITE |
| BackupPage | 备份配置、创建、下载和受控恢复 | 独立命令 | [源码](../../../frontend/src/features/system/pages/BackupPage.vue) | FE-SETTING-BACKUPPAGE-FIELDS, FE-SETTING-BACKUPPAGE-WRITE |
| DelegatedSettings | 普通管理员获授权字段的revision条件保存 | 委托字段的revision PUT | [源码](../../../frontend/src/app/compositions/settings/DelegatedSettings.vue) | FE-SETTING-DELEGATEDSETTINGS-FIELDS, FE-SETTING-DELEGATEDSETTINGS-WRITE |

表内FIELDS/WRITE用例引用描述覆盖义务，不表示已执行。外部SMTP/支付/对象存储/上游动作另需真实隔离前置条件。

## 3 品牌与地址合同

| 设置 | 展示或消费位置 | 不得推导的权限/副作用 |
| --- | --- | --- |
| site_name、site_logo、site_subtitle | 首页、壳、登录、标题、favicon等使用当前公开设置；空值按中性项目默认 | 不改变技术ID、数据库归属、协议或计费 |
| api_base_url、custom_endpoints | 用户复制的网关链接、文档和客户端示例 | 不替换携带管理令牌的浏览器API目标 |
| doc_url、contact_info、home_content | 帮助、联系与首页内容 | 不变成更新源、上游代理目标或可信任脚本 |
| vendor_console_enabled、vendor_portal_origin | 独立门户可用性及精确origin准入 | 构建标记不授予角色；普通供应商不能进入平台管理 |
| 镜像引用、Compose项目、容器/网络/卷 | 部署输入；规则归BRANDING和部署模板 | 网站设置不修改Docker资源或镜像仓库凭据 |

来源：[公开设置DTO](../../../frontend/src/features/site/api/publicSettingsTypes.ts)、[项目身份](../../../frontend/src/shared/config/project.ts)、[HTTP地址](../../../frontend/src/shared/http/url.ts)、[品牌合同](../BRANDING.md)。

图片引用合同由 [前端纯函数](../../../frontend/src/shared/lib/imageReference.mjs) 与 [表驱动样本](../../../frontend/src/shared/lib/__tests__/fixtures/imageReferences.json) 明确：trim，拒绝内部 CR/LF/TAB/反斜杠、协议相对 URL、HTTP(S) userinfo/空 hostname；允许单斜杠根路径、有效 HTTP(S)、大小写不敏感的 data:image/。返回 trim 后原值，由消费者按 HTML 属性转义；不执行脚本、不在后端抓取远端图片。data reference 接受不等于载荷合法，favicon 接口独立解码，缺逗号/空载荷/坏 base64/坏转义返回无内容，保留 no-store/nosniff/CSP。

新增维护投影：资源 `authorization_modes?: ('native'|'bps')[]`、供应商 `auth_mode?: string` 只作展示；供应商 Extra 只接受服务端明确的隐私/compact 白名单。Group `image_count_billing` 属 pricing 草稿并参与一次原子 Group 命令；后端字段持久化、账务与授权不由前端推断。

## 4 需要统一解释的输入与输出

| 项目 | 规格 |
| --- | --- |
| Secret字段 | 展示配置状态/掩码和一次性返回值必须区分；空串、省略、清空操作按具体字段，不用掩码写回秘密 |
| 数字与单位 | 正常0与未知区分；金额、百分比、配额和ms/s沿现有单位，校验有限值/范围，不让空串转换0替代业务判断 |
| ID与范围 | 用户、Key、账号、组、订单、供应商和作业ID不得混用；复制/批量/导出在确认时冻结目标 |
| JSON/规则编辑 | 使用对应字段语法及领域校验；不能把unknown能力默认成支持；原子Group/Settings组合维持原服务端边界 |
| URL/内容 | 管理API目标与公开链接隔离；Markdown/HTML/iframe遵守既有净化与origin边界，不能跨源传管理凭据 |
| 反馈 | 正常、加载、空、失败、取消、未知结果、权限不足分别表达；业务成功来自服务端结果，禁止静默盲重放 |

## 5 服务端依赖边界

目前没有全域统一的六维CapabilityProfile或通用expectedRevision命令接口。前端展示实际Account/Group/manifest/usage字段，配置不等于实际可用资格；缺失能力或账务事实保持未知。新增此类合同需要接口owner先提供可用字段及消费者，不能让规格书替后端宣布实现。

### BPS 安全DTO迁移与未闭环事项（2026-10-27）

管理API已公开 `Account.excel_bps_403_recovery` 并隐藏私有failure；f4fe私有读取的6005项开发测试不覆盖新source。MAIN现确认生产consumer全部迁至[安全恢复reader](../../../frontend/src/features/resources/model/bpsRecoveryPresentation.ts)，旧reader及专属测试删除，无shim；新状态/严格时间parser的220项相关测试由MAIN报告通过，本资料owner未重跑或独立核验测试回执，最终e5已交接，真实业务验收仍待完成。

| 边界 | 已知事实 | 未闭环条件 |
| --- | --- | --- |
| 管理安全DTO消费 | MAIN确认生产迁移完成，旧reader及专属测试删除无shim；220相关tests通过 | history/reauthorization_required/paused/pending/failed/probing/unknown；不读私有extra.failure、不从reason推授权；严格日期/24:00校验。最终前端e5已交接，联合业务验收尚未完成 |
| supplier投影 | supplier安全BPS状态投影尚未新增 | 后端提供符合供应商权限的安全投影及实际消费者证据；不能透传私有extra或复用管理权限 |
| BPS冷却的恢复权限 | 成功测试后的自动恢复须保留BPS范围冷却；管理员明确覆盖是另一独立权限/端点语义 | 按 FE-RES-BPS-PUBLIC-RECOVERY 分别核对，不能要求所有recover都保留、也不能把成功测试视为获准清除BPS；真实API验收仍not_run |
| 陈旧enabled草稿与原子并发 | 无关旧草稿不能重新开启后台已自动关闭的BPS；明确停止才发送既有false | 按 FE-RES-BPS-STALE-EDIT 要求后端原子更新或等效并发合同及旧claim取消；缺少合同标blocked，GET预检不等价，当前未闭环 |
| 显式停止与最终合同 | 普通已关闭账号编辑不产生false停止意图；关闭表单、目标/会话/权限变化须清除未保存意图 | 旧probe不得恢复已明确取消的状态；按 FE-RES-BPS-STALE-EDIT 验证服务端原子语义，不能以按钮或保存成功替代证明；前端snapshot已固定为e5，后端原子合同及联合runtime验收仍待完成 |

f4fe回归只属于历史检查点。当前验收分别引用 FE-RES-BPS-EXPLICIT-MODELS、FE-RES-BPS-PUBLIC-RECOVERY、FE-RES-BPS-STALE-EDIT；三条均not_run。当前catalog为753用例，定义更新不等于运行通过；既有组合用例仍保留其有效范围。

### BPS 显式模型选择（本次用户修订）

BPS只匹配最终账号映射后的明确勾选模型；缺失、空、null或畸形列表均不表示全选，不自动替历史账号补全。Cloud开启时仍优先并保留已保存BPS列表，关闭Cloud后按该列表恢复选路；具备原生授权的独立新请求可走Native，Excel-only不因此获得Native权限。创建、编辑、批量配置及供应商授权迁移都不得用“全选兼容”或“缺失=全模型”替代该要求。来源是[CONTRACTS第82行](../CONTRACTS.md#bps-routing-20261027)与 `WP-18-BPS-EXPLICIT-MODEL-SELECTION-20261027`，是c2a10f2之后的用户修订，不是该维护输入已实现语义。按主代理交接，Arendt已接模型选择全链/默认模板退休/供应商迁移；主代理已完成安全DTO时间校验与scope展示修复，最终e5与作用域复用证据已交接，业务验收未完成。该条不授权自动补全旧账号；前端e5已固定，不代表后端与联合runtime已冻结或验收。

CT-11、当前DTO和领域章节共同描述真实合同；本附录不增加新开关、不改变接口方法/端点，也不代替完整外部验收。
