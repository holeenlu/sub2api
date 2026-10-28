# 04 观测、异步作业、供应关系与系统控制

c2维护35路径的应用和前端开发证据已交接，当前e5源码已冻结；详见[最终证据](REVIEW-EVIDENCE.md#final-e5-evidence)。当前catalog为51面板/753业务定义，均not_run；开发测试不替代实际业务验收。

## 功能索引

| 功能编号 | 功能名 | 入口 | 角色 |
| --- | --- | --- | --- |
| FS-OPS-01 | 运维总览与联动筛选 | /admin/ops | 获准管理员；写操作另验能力 |
| FS-OPS-02 | 请求、错误详情与诊断导出 | /admin/ops | 获准管理员；写操作另验能力 |
| FS-OPS-03 | 系统日志、运行日志级别与清理 | /admin/ops | 获准管理员；写操作另验能力 |
| FS-OPS-04 | 告警规则编辑与删除 | /admin/ops | 获准管理员；写操作另验能力 |
| FS-OPS-05 | 告警事件、历史、静默与人工处理 | /admin/ops | 获准管理员；写操作另验能力 |
| FS-OPS-06 | 运维配置与通知 | /admin/ops → 设置 | super_admin |
| FS-OPS-07 | 管理审计查询与整库清理 | /admin/audit-logs | 获准管理员；整库清理仅超管 |
| FS-OPS-08 | 提示审计运行状态、策略与端点池 | /admin/prompt-audit | 获准管理员读；配置/runtime/探测仅超管 |
| FS-OPS-09 | 提示事件详情与精确删除 | /admin/prompt-audit | 删除仅超管；内容另验权限 |
| FS-OPS-10 | 渠道监控项、复制与手动执行 | /admin/channels/monitor → legacy | 获准管理员 |
| FS-OPS-11 | 监控模板、高级请求与应用确认 | /admin/channels/monitor → 模板 | 获准管理员 |
| FS-OPS-12 | 用户渠道状态与 V1/V2 展示 | /monitor | 主站已登录用户、功能 gate |
| FS-OPS-13 | 账号计划测试与诊断边界 | /admin/accounts → 账号测试/计划 | 获准账号操作角色 |
| FS-OPS-14 | 流量观测 V2 独立配置 | /admin/channels/monitor → v2 | 获准管理员 |
| FS-JOB-01 | 访问资格与任务列表 | /batch-image；alias /docs/batch-image | 本人 active Gemini Key 且批图能力开启 |
| FS-JOB-02 | 创建批图任务 | /batch-image；alias /docs/batch-image | 本人 active Gemini Key 且批图能力开启 |
| FS-JOB-03 | 详情、明细与图片预览 | /batch-image；alias /docs/batch-image | 本人 active Gemini Key 且批图能力开启 |
| FS-JOB-04 | 取消与失败项重试 | /batch-image；alias /docs/batch-image | 本人 active Gemini Key 且批图能力开启 |
| FS-JOB-05 | ZIP 下载、批量下载与记录删除 | /batch-image；alias /docs/batch-image | 本人 active Gemini Key 且批图能力开启 |
| FS-JOB-06 | 使用指南与提示词复制 | /batch-image 的指南/提示词浮层 | 已进入该页面的当前主体；不因复制授予生成能力 |
| FS-SUPPLY-01 | 门户 origin、11 tab 与不可用页 | /vendor/:tab；/vendor-unavailable | 真实 role 分支，见 11 tab 矩阵 |
| FS-SUPPLY-02 | 平台工作区七个 query tab | /vendor/platform?tab=... | super_admin |
| FS-SUPPLY-03 | 供应商开通、策略编辑与直属下级 | platform/suppliers；/vendor/children | super_admin；直属父 vendor_level1 |
| FS-SUPPLY-04 | 账号清单、详情、编辑与批操作 | accounts；platform/accounts | vendor_level1/2；平台超管 |
| FS-SUPPLY-05 | 账号授权、导入导出与敏感确认 | 账号授权/导入/导出对话框 | 可写 owner，非聚合 |
| FS-SUPPLY-06 | 代理配置、导入导出与诊断 | proxies；platform/proxies | vendor_level1/2；平台超管 |
| FS-SUPPLY-07 | 用量筛选、账号下钻和平台导出 | usage；child-usage；platform/usage | 按 owner 只读；平台导出仅超管 |
| FS-SUPPLY-08 | 子账号、子代理、子用量只读下钻 | child-accounts/child-proxies/child-usage | vendor_level1 |
| FS-SUPPLY-09 | 应收/应付账单列表、详情和导出 | settlements；payables；platform/settlements | 按收款方/付款方区分 |
| FS-SUPPLY-10 | 结算预览、合同期间与草稿落单 | payables；platform/settlements | vendor_level1 付款方；平台超管 |
| FS-SUPPLY-11 | 确认、作废、付款登记、冲正与调整 | 结算详情动作对话框 | 当前账单付款方 |
| FS-SUPPLY-12 | 门户资料与安全入口 | /vendor/profile | vendor_level1/2、super_admin |
| FS-SUPPLY-13 | 组供给观察、供应商审计与身份对账 | platform → supply/audit/suppliers | super_admin |
| FS-SYS-01 | 设置工作区和领域边界 | /admin/settings | 超管工作区；获准 admin 委派视图 |
| FS-SYS-02 | revision、稀疏保存与冲突审阅 | /admin/settings → 全局保存/冲突 | super_admin |
| FS-SYS-03 | 委派设置与角色权限入口 | /admin/settings → 委派；roles | 按 general/content/角色权限 |
| FS-SYS-04 | 控制台、供应商入口与插件开关 | general/features | super_admin |
| FS-SYS-05 | SMTP 草稿、连接测试与测试邮件 | email → SMTP | super_admin |
| FS-SYS-06 | 邮件模板选择、预览、保存与恢复 | email；委派内容设置 | 超管；content.read/manage |
| FS-SYS-07 | 管理访问密钥与历史单 Key | security；general legacy | super_admin |
| FS-SYS-08 | 备份存储、图像存储和计划/归档 | backup → 存储/计划 | super_admin |
| FS-SYS-09 | 手动备份、观察、下载、恢复与删除 | backup → 备份记录 | super_admin，敏感动作 step-up |
| FS-SYS-10 | 插件上传、兼容性、启停、灰度和测试 | /admin/plugins | 获准管理员，写动作另验权限 |
| FS-SYS-11 | 插件配置 iframe 与消息桥 | 插件配置 iframe | 当前合法插件会话/动作权限 |
| FS-SYS-12 | 只读构建版本 | 主站/门户 VersionBadge | 主站/门户徽标均仅超管 |
| FS-SYS-13 | 初始化向导 | /setup | needs_setup=true、bootstrap token |

## 范围、证据与共同前置

- 原始比较基线为6f48d6ab6fcc29763af84410690530aeaa957403，维护输入固定c2a10f2；当前前端e5已冻结。完整6196测试/双build真实输入63fe，按唯一测试类型补丁和生产等价证明复用于e5；五项static直接绑定e5。旧f4fe、5c3005及5085记录保留原hash，详见[最终证据](REVIEW-EVIDENCE.md#final-e5-evidence)。开发验证不等于业务验收。
- [路由清单](../../../frontend/tests/acceptance/route-inventory.json)和[验收用例](../../../frontend/tests/acceptance/cases.json)是规格追踪来源，不是通过证明。vendor 是 **1 个参数路由、11 个 tab**：accounts/proxies/usage/settlements/profile/platform/children/child-accounts/child-proxies/child-usage/payables；因此 82 个 rendered records 展开为 92 个验收 entries，增加 10。
- [根 TEST_REPORT](../TEST_REPORT.md)已记录 UI 镜像构建/部署及来源核对、TLS 专用浏览器前置通过；当前753个业务用例均为not_run，目录追踪已按回执重建，API 镜像仍属旧阶段、须更新。上述阶段不能相互替代。本章不另建结果状态系统；工作包和结果继续由根 work-items/TEST_REPORT 管理。
- API 路径记法：本章 `/admin/...`、`/vendor/...` 是 shared/http 的相对路径，通常落在 `/api/v1` 前缀；批图 `/v1/images/batches` 和 setup 根路径按各自网关 URL helper 拼接，不混用模型 Key 与管理 JWT。
- “获准管理员”指超管或持有**精确页面** admin_pages 的管理员。页面可见不授予写操作；permissions、admin_write_fields、policy_version、供应商层级/合同版本均以服务端为准。[路由 guard](../../../frontend/src/app/router/index.ts)、[身份权限](../../../frontend/src/features/identity/model/auth.ts)。
- 主站页面与供应商 origin 分开。供应商业务要求 vendor_console_enabled，且 origin 的 scheme/host/port 精确匹配；不得修改 store、query 或角色投影绕过 guard。
- 功能条目默认以 local-runtime、session-roles 及其指定业务 fixture 为前置；测试须正常登录对应角色，使用 fixture 已知 ID。管理合规人工确认不得由工具代替实际管理员接受。
- 换 actor/目标、卸载和取消 step-up 后，旧响应不得回写新界面或自动重放命令；Abort 不代表后端回滚。结果未知需读权威状态。[HTTP](../../../frontend/src/shared/http/client.ts)、[应用 step-up](../../../frontend/src/app/http/install.ts)、[生命周期规范](../FRONTEND_ARCHITECTURE.md)。
- 章节边界：网站具体品牌、公告、公开文档属第 1 章；资源编辑、目录/分组的细节引用相应所属章；支付 provider、商户凭据和用户支付订单属第 3 章。51设置面板总表由 main 的[设置与合同附录](SETTINGS_AND_CONTRACTS.md)维护，本章不复制其余领域业务字段。

## FS-OPS：观测、审计、告警和诊断

### FS-OPS-01 运维总览与联动筛选
- 节点与刷新：每个节点保留身份、观察时间及独立 unknown/不可用状态，不把缺项补零；默认前三项、可展开全部，按服务端 age_seconds 加本地经过时间判定超过 30 秒过期。自动倒计时先递减到零即触发，成功/失败均重置间隔；设置关闭不重复请求，保存后更新，Header 摘要按完成的 lastUpdated 更新。页面刷新周期不等于告警评估周期。
- **入口/前置**：`/admin/ops`，获准管理员；准备 operations、admin-compliance fixture，运维开关关闭不得伪造零指标或成功快照。
- **输入/流程**：选择时间范围/自定义起止、平台、组和查询模式，刷新、全屏、退出全屏；平台/组联动到吞吐、切换、并发/可用账号、延迟、错误趋势/分布、OpenAI token 统计，图表可下钻到对应请求条件。
- **结果/异常**：以 overview/snapshot-v2/trend 等响应显示 current/stale/空/失败及覆盖范围；刷新不把缺失用量变成 0，离页或换过滤条件停止旧请求、轮询；关闭配置导致的 404 不等于正常无流量。
- **权威来源**：`GET /api/v1/admin/ops/dashboard/{overview,snapshot-v2,throughput-trend,latency-histogram,error-trend,error-distribution,openai-token-stats}`，并发/可用性独立查询；[Dashboard](../../../frontend/src/features/operations/dashboard/OpsDashboard.vue)、[API](../../../frontend/src/features/operations/api/ops.ts)。
- **既有 case**：`FE-PAGE-ADMIN-OPS-ACTION-1`；真实上游关联另用 `FE-PAGE-ADMIN-OPS-EXTERNAL-UPSTREAM`，当前不能据本地观测认定上游已验收。

### FS-OPS-02 请求、错误详情与诊断导出
- 诊断完整性：导出包含详情与全部关联 upstream errors，使用取消信号、分页去重/总数核对；总数漂移或不完整时报告失败，不把截断列表导出为完整诊断。切换目标/卸载取消旧任务，保留后台脱敏错误正文而不以 failure_category 覆盖。
- **入口/前置**：运维图表/请求列表/错误列表的详情弹窗；沿当前时间、平台、组、请求及账号目标，敏感字段只展示服务端允许返回的内容。
- **输入/流程**：筛选分页成功/错误请求；分别查看 request error、upstream error 及关联上游错误；展开请求、响应、调度诊断和阶段信息，复制字段、导出诊断 JSON，并返回原列表条件；按当前动作标记处理/未处理。
- **结果/异常**：错误记录保留原状态和 unknown；详情加载失败不显示前一目标。IP 信息仅主动查询允许的公开 IP，private/reserved 不外发。冻结错误详情未提供“重新执行原模型请求”的命令，不把诊断导出解释为请求重放。
- **权威来源**：`/admin/ops/requests`、`/errors`、`/request-errors`、`/upstream-errors` 的 GET、详情及 PUT `/:id/resolve`；[详情](../../../frontend/src/features/operations/dashboard/components/OpsErrorDetailModal.vue)、[请求列表](../../../frontend/src/features/operations/dashboard/components/OpsRequestDetailsModal.vue)、[错误列表](../../../frontend/src/features/operations/dashboard/components/OpsErrorLogTable.vue)、[API](../../../frontend/src/features/operations/api/ops.ts)。
- **既有 case**：`FE-PAGE-ADMIN-OPS-ACTION-2`；外部 Geo 查询为 `FE-PAGE-ADMIN-OPS-EXTERNAL-NETWORK`，不得使用真实用户敏感 IP 作随意探测。

### FS-OPS-03 系统日志、运行日志级别与清理
- **入口/前置**：运维页系统日志区域/运行日志配置；使用脱敏 operations fixture，写操作接受服务端权限校验。
- **输入/流程**：按界面日志条件、级别、时间筛选分页/刷新，展开详情和查看 sink health；修改运行日志配置/采样参数或恢复默认；清理先确认当前范围，采样数量输入须符合表单整数边界。
- **结果/异常**：更新只改变相应运行配置；清理展示返回 deleted 数，不宣称清理了其他审计或提示事件。读取/保存失败、权限不足单独反馈，取消确认不请求清理；切 scope 丢弃旧结果。
- **权威来源**：`GET /admin/ops/system-logs`、`/system-logs/health`，`POST /system-logs/cleanup`，`GET/PUT /admin/ops/runtime/logging`、`POST .../reset`；[系统日志组件](../../../frontend/src/features/operations/dashboard/components/OpsSystemLogTable.vue)、[API](../../../frontend/src/features/operations/api/ops.ts)。
- **既有 case**：归入 `FE-PAGE-ADMIN-OPS-ACTION-3` 的实际设置/权限操作；目前无独立“日志清理”case ID，执行该父 case 时须列明本子功能证据，不能把打开设置当作清理通过。

### FS-OPS-04 告警规则编辑与删除
- **入口/前置**：运维页告警规则弹窗；页面入口权限与后端规则写权限分别校验，不能由可见按钮推断授权。
- **输入/流程**：新增/编辑规则的名称、描述、指标、比较符、阈值、严重级别、平台/组范围、窗口、持续时间、冷却、启用及邮件通知；窗口只允许 1/5/60 分钟，组指标必须有正 group_id，阈值须有限数；持续分钟为 1–1440，冷却为 0–1440，其他范围依实际 metric 校验；删除需确认具体规则。
- **结果/异常**：创建 POST、编辑 PUT 成功后重新读取规则；非法指标/阈值不提交，失败保留编辑内容并反馈，删除取消不调用接口；新旧 actor 的列表和保存结果隔离。
- **权威来源**：`GET/POST /admin/ops/alert-rules`，`PUT/DELETE .../:id`；[规则编辑器](../../../frontend/src/features/operations/dashboard/components/OpsAlertRulesCard.vue)、[规则类型](../../../frontend/src/features/operations/dashboard/types.ts)。
- **既有 case**：`FE-PAGE-ADMIN-OPS-ACTION-3`；本地规则保存不等于已触发真实告警或邮件投递。

### FS-OPS-05 告警事件、历史、静默与人工处理
- **入口/前置**：运维页告警事件卡/详情；静默和人工解决入口要求 ops.manage；准备 firing、resolved、manual_resolved 及不同 scope 的事件。
- **输入/流程**：按现有条件加载更多、打开事件及规则历史；对当前事件设置静默（界面含 1h/24h/7d 期限）或人工解决，静默请求带规则/平台/组范围及 until/reason。
- **结果/异常**：静默不删除事件；人工处理提交 `manual_resolved`，展示服务端时间和状态。关闭详情或切换事件使旧命令结果失效，403/失败不改写为已解决，失败不自动重发。
- **权威来源**：`GET /admin/ops/alert-events`、`/:id`，`PUT /:id/status`，`POST /admin/ops/alert-silences`；[事件组件](../../../frontend/src/features/operations/dashboard/components/OpsAlertEventsCard.vue)、[API](../../../frontend/src/features/operations/api/ops.ts)。
- **既有 case**：`FE-PAGE-ADMIN-OPS-ACTION-3`，告警子动作共享父 case，须分别核对操作前后事实。

### FS-OPS-06 运维配置与通知
- **入口/前置**：运维设置弹窗由超管入口展示；使用 operations 和本地邮件配置，不借 UI 权限扩大普通管理员能力。
- **输入/流程**：读取运行告警、邮件通知、advanced settings、指标阈值；维护邮件地址、告警运行参数、保留期、观测上传间隔及实际阈值/配额保护选项。邮箱、整数/百分比范围分别校验；观测上传间隔为 60–86400 秒。
- **结果/异常**：保存调用多个独立配置端点，不是一个 CAS 事务；某项失败可能已有其他项成功，应重读各配置核对，不把 Promise 汇总失败当成整体回滚。取消/卸载不向新页面报旧成功。
- **权威来源**：`/admin/ops/runtime/alert`、`/email-notification/config`、`/advanced-settings`、`/settings/metric-thresholds` 的 GET/PUT；通知、告警及阈值配置由实际消费这些 API 的[配置弹窗](../../../frontend/src/features/operations/dashboard/components/OpsSettingsDialog.vue)负责。
- **既有 case**：`FE-PAGE-ADMIN-OPS-ACTION-3`。SMTP 连接/投递独立条件见 FS-SYS-05，不能由“保存通知配置”证明送达。

### FS-OPS-07 管理审计查询与整库清理
- **入口/前置**：`/admin/audit-logs`；获准管理员可查询，清理按钮/提交仅超管；使用脱敏、可销毁审计 fixture。
- **输入/流程**：按时间、操作者、动作、认证方式、方法、IP、成功/失败及文本筛选、重置和分页；打开单条脱敏请求体/凭据遮罩。清理走“整库清理确认 → 6 位 TOTP → 提交”，不是当前筛选结果或逐条删除。
- **结果/异常**：返回 deleted 数后重读；取消任一确认不删除，TOTP 错误清空验证码并反馈；已退出/换身份的清理结果不污染新列表。
- **权威来源**：`GET /admin/audit-logs`、`/:id`，`POST /admin/audit-logs/clear {totp_code}`；[审计页](../../../frontend/src/features/operations/pages/AuditLogPage.vue)、[API 及 DTO](../../../frontend/src/features/operations/api/audit.ts)。
- **既有 case**：`FE-PAGE-ADMIN-AUDIT-LOGS-ACTION-1`、`FE-PAGE-ADMIN-AUDIT-LOGS-ACTION-2`；人工合规前置为 `FE-PAGE-ADMIN-AUDIT-LOGS-MANUAL-COMPLIANCE`。

### FS-OPS-08 提示审计运行状态、策略与端点池
- **入口/前置**：`/admin/prompt-audit`，risk_control_enabled 显式关闭时按 guard 跳转；准备 operations fixture；配置、runtime 加载、端点探测与删除动作仅超管，事件内容另需 prompt_audit.content.read；真实探测须另备获准审核端点。
- **输入/流程**：查看进程、队列、guard 指标及 expected/active config_version；编辑 off/async_audit/blocking 对应开关、最新轮次范围、通过事件存储、scanner、组范围、worker/queue 和优先级端点池。端点包含名称、协议、base_url、model、timeout_ms、input_limit、enabled；token 留空保留既有值，clear_token 是显式清除。
- **结果/异常**：启用 blocking 先确认；保存携带 expected_config_version，不能覆盖冲突的新版本；新建/编辑/移除、启停端点只是配置草稿，最终由整体配置 PUT 提交。逐端点 probe 返回延迟、状态、token_applied，不代表全部业务请求已获准或已成功。
- **权威来源**：`/admin/prompt-audit/config` GET/PUT、`/runtime` GET、`/endpoints/probe` POST；[页面](../../../frontend/src/features/operations/promptAudit/PromptAuditView.vue)、[端点池](../../../frontend/src/features/operations/promptAudit/components/EndpointPool.vue)、[类型](../../../frontend/src/features/operations/promptAudit/types.ts)、[API](../../../frontend/src/features/operations/promptAudit/api.ts)。
- **既有 case**：`FE-PAGE-ADMIN-PROMPT-AUDIT-ACTION-1`、`FE-PAGE-ADMIN-PROMPT-AUDIT-ACTION-2`；真实审核为 `FE-PAGE-ADMIN-PROMPT-AUDIT-EXTERNAL-UPSTREAM`。

### FS-OPS-09 提示事件详情与精确删除
- **入口/前置**：提示审计事件工作区；删除仅超管，内容查看需 prompt_audit.content.read；事件数据必须是当前 scope 内的脱敏/合成样本。
- **输入/流程**：条件筛选、分页、勾选、单条详情；支持单条删除、选中 IDs 批量删除、按筛选预览/确认删除。确认可复用当前有效预览，或在该次确认动作内先取得新预览再提交；请求绑定 filter、snapshot_max_id、filter_hash、confirmation_token，不使用旧条件的 token。
- **结果/异常**：显示服务端删除结果并重读，不能把某次列表展示当成完整删除范围；失败不清空其他成功记录，取消确认不发请求；旧详情及预览不能在新 actor/筛选下生效。
- **权威来源**：`/admin/prompt-audit/events` GET，`/:id` GET/DELETE，`/batch-delete`、`/delete-preview`、`/delete-by-filter` POST；[事件工作区](../../../frontend/src/features/operations/promptAudit/components/EventWorkspace.vue)、[删除对话框](../../../frontend/src/features/operations/promptAudit/components/FilterDeleteDialog.vue)、[API](../../../frontend/src/features/operations/promptAudit/api.ts)。
- **既有 case**：`FE-PAGE-ADMIN-PROMPT-AUDIT-ACTION-3`；它不授权清理真实用户提示词历史。

### FS-OPS-10 渠道监控项、复制与手动执行
- **入口/前置**：`/admin/channels/monitor`，获准管理员、监控开关；准备 operations、已有账号/Key 与模板 fixture。
- **输入/流程**：按监控模式/条件查询分页；新建、编辑、启停、删除、复制、立即执行和查看历史/运行结果。编辑 name、provider、api_mode、check_mode、账号关联、endpoint、主/额外模型、group_name、间隔和 jitter；名称必填，quota 模式须账号，probe 部分须主模型；interval-jitter 不得低于 15 秒。
- **结果/异常**：切 provider 清空临时 Key/模板；编辑未填写新 Key 时省略 api_key；probe 更新解绑账号发送 account_id=0，移除模板显式 clear_template=true，不能用 null 猜测清空。复制沿原 Idempotency-Key 合同；未知结果不自动重复复制；run 是有副作用的真实探测，不能把创建配置当作执行成功。
- **权威来源**：`/admin/channel-monitors` GET/POST，`/:id` GET/PUT/DELETE，`/:id/duplicate`、`/:id/run` POST 及 history GET；[监控页](../../../frontend/src/features/operations/monitor/pages/ChannelMonitorPage.vue)、[表单](../../../frontend/src/features/operations/monitor/admin/MonitorFormDialog.vue)、[API](../../../frontend/src/features/operations/api/adminChannelMonitor.ts)。
- **既有 case**：`FE-PAGE-ADMIN-CHANNELS-MONITOR-ACTION-1`、`FE-PAGE-ADMIN-CHANNELS-MONITOR-ACTION-2`；实际 run 另用 `FE-PAGE-ADMIN-CHANNELS-MONITOR-EXTERNAL-UPSTREAM`。

### FS-OPS-11 监控模板、高级请求与应用确认
- **入口/前置**：管理监控页的模板管理/应用选择器、Key 选择器、高级请求设置；模板归属 provider，不跨平台复用不兼容凭据；应用是复制快照，模板后续修改不自动同步到监控项。
- **输入/流程**：管理页面实际提供的模板，从该模板已关联监控项中选择非空子集后应用；编辑额外 headers、body_override_mode 和 JSON body_override，按所选模板覆盖相应草稿字段；Key 选择受当前用户可用 Key/组范围约束。
- **结果/异常**：删除/应用确认绑定当前模板和目标；预览草稿不立即发送探测，敏感 header/Key 不进入持久化偏好；关闭/换 actor 丢弃旧选择器结果；应用显示服务端 affected 数，不按选择数量推断全部成功。
- **权威来源**：[模板管理](../../../frontend/src/features/operations/monitor/admin/MonitorTemplateManagerDialog.vue)、[应用选择器](../../../frontend/src/features/operations/monitor/admin/MonitorTemplateApplyPickerDialog.vue)、[高级请求](../../../frontend/src/features/operations/monitor/admin/MonitorAdvancedRequestConfig.vue)、[监控公开端口](../../../frontend/src/features/operations/monitor/ports.ts)、[模板 API](../../../frontend/src/features/operations/monitorTemplates.ts)：`/admin/channel-monitor-templates` GET/POST、`/:id` GET/PUT/DELETE、`/:id/monitors` GET、`/:id/apply` POST。
- **既有 case**：`FE-PAGE-ADMIN-CHANNELS-MONITOR-ACTION-2`；模板子动作尚未单列 ID，不以打开对话框替代保存/应用核对。

### FS-OPS-12 用户渠道状态与 V1/V2 展示
- **入口/前置**：`/monitor`，主站已登录且符合后端模式/功能 gate；不把用户身份用于匿名公共倍率查询。
- **输入/流程**：按既有 resolveChannelMonitorMode 选择展示，缺省/未知 mode 沿 V2；V1 查看监控卡/时间线/详情/配额；V2 选择 range（90m/24h/7d/30d）、平台/组/模型和矩阵分组，查看模型、错误及获准用户统计。
- **结果/异常**：展示覆盖率、当前/过期/unknown，不推测缺失配额；吞吐、配额、用户排行遵守 public display settings。刷新/返回不重复开启轮询，换身份不留前一用户数据。
- **权威来源**：[入口 resolve](../../../frontend/src/features/operations/monitor/pages/ChannelStatusPage.vue)、[V1](../../../frontend/src/features/operations/monitor/pages/ChannelStatusV1Page.vue)、[V2](../../../frontend/src/features/operations/monitor/pages/ChannelStatusV2Page.vue)、[观测 API](../../../frontend/src/features/operations/api/monitor.ts)、[V1 API](../../../frontend/src/features/operations/api/userChannelMonitor.ts)。
- **既有 case**：`FE-PAGE-MONITOR-ACTION-1`、`FE-PAGE-MONITOR-ACTION-2`。

### FS-OPS-13 账号计划测试与诊断边界
- **入口/前置**：资源账号详情中的 ScheduledTestsPanel/AccountTestModal，由资源工作台承载，不是新增独立路由；必须有当前账号的测试/写能力和可用模型。
- **输入/流程**：计划输入 model_id、cron_expression、enabled、max_results、auto_recover；创建、编辑、启停、删除确认及结果查看/刷新；模型与 cron 必填；其他已核实字段为 enabled、max_results（保留结果数）、auto_recover，不添加未定义的重试/超时字段。手动账号测试使用其真实请求/流结果，不把“运行中”或配置保存当成测试通过。
- **结果/异常**：按 plan ID 读取结果；切账号/权限、关闭面板后旧测试结果不得归到新账号。真实账号探测要有外部凭据/预算，本地失败记录只能验证展示与取消。
- **权威来源**：`GET /admin/accounts/:id/scheduled-test-plans`，`POST /admin/scheduled-test-plans`、`PUT/DELETE /:id`、`GET /:id/results`；[计划面板](../../../frontend/src/features/resources/components/ScheduledTestsPanel.vue)、[测试对话框](../../../frontend/src/features/resources/components/AccountTestModal.vue)、[API](../../../frontend/src/features/resources/api/scheduledTests.ts)。
- **既有 case**：当前可关联 `FE-PAGE-ADMIN-ACCOUNTS-ACTION-3` 的诊断面板及该页外部探测条件；清单未单列计划 CRUD case ID，不能宣称已有计划逐项验收。账号编辑字段归资源章。

### FS-OPS-14 流量观测 V2 独立配置
- **入口/前置**：管理渠道监控页的 v2 tab，legacy tab 保留原监控项工作台；全站模式/开关与本独立配置是不同输入，当前不是 V2 时显示模式提示及设置入口。
- **输入/流程**：配置 enabled、60/300 秒刷新、已有目录平台/组范围、忽略错误类别、健康阈值；控件范围标注样本量 1–10000、TTFT 正数；错误/缓存百分比限制 0–100 并按 API 比率转换，最终合法性仍由服务端校验。显式空 ignored_error_categories 保留，不擅自填回默认。
- **结果/异常**：仅 dirty 时保存，成功更新当前配置基线；保存 V2 配置不自动切换全站 channel_monitor_mode。加载失败/取消保留正确空或错误态，旧 scope 结果不写新页面。
- **权威来源**：`GET/PUT /admin/channel-monitor-v2/config`；[独立设置](../../../frontend/src/features/operations/monitor/MonitorSettingsPanel.vue)、[V2 API](../../../frontend/src/features/operations/api/monitor.ts)。
- **既有 case**：`FE-PAGE-ADMIN-CHANNELS-MONITOR-ACTION-1`、`FE-PAGE-ADMIN-CHANNELS-MONITOR-ACTION-2`；不把 legacy 创建监控项视为 V2 设置验收。

## FS-JOB：批量图片与异步作业

### FS-JOB-01 访问资格与任务列表
- **入口/前置**：`/batch-image`，`/docs/batch-image` 为同一页面 alias；登录后仅当存在 active、Gemini 分组且 allow_batch_image_generation=true 的本人 Key 才开放能力。准备 jobs、keys fixture；资格加载失败单列 error。
- **输入/流程**：加载本人可用 Key/模型；按任务名、状态、时间、下载条件筛选，刷新、清空过滤、游标分页、多选，展开父子重试任务；任务所属 api_key_id 不能由当前另选 Key 覆盖。
- **结果/异常**：空、部分加载失败、拒绝访问与终态分开；切用户/Key 取消旧查询并清除私有详情。供应商身份不能借主站角色投影进入。
- **权威来源**：本人 Key API 由 JobsServices 注入；`GET /v1/images/batches`、`/models`；[访问判定](../../../frontend/src/features/jobs/queries/useBatchImageAccess.ts)、[页面](../../../frontend/src/features/jobs/pages/BatchImageJobsPage.vue)、[API](../../../frontend/src/features/jobs/api/batchImage.ts)。
- **既有 case**：`FE-PAGE-BATCH-IMAGE-ENTRY`、`FE-PAGE-BATCH-IMAGE-ACTION-1`。

### FS-JOB-02 创建批图任务
- **入口/前置**：工作台“创建”弹窗；获准 Key、该 Key 返回的模型列表、jobs 合成输入；真实创建需外部模型权限和预算。
- **输入/流程**：选择 Key、模型、任务名、输出 MIME，添加/移除 prompt 行和参考图片，调整每项输出数量；每项最多 4 张、单任务预计总输出最多 200，参考图上限取所选模型。当前提交 image_size 固定 `1K`；不能为空 prompt/模型/Key，越界不提交。
- **结果/异常**：POST 带独立 Idempotency-Key，返回 job 后进入详情、加载 items 并轮询；在途禁重复提交。失败/取消保留需要的草稿，结果未知不自动再 POST；新手动提交不是原请求已完成的证明。
- **权威来源**：`POST /v1/images/batches`；API 使用所选 Key 的 Bearer、credentials=omit、redirect=error，不使用管理 JWT；[校验与提交](../../../frontend/src/features/jobs/pages/BatchImageJobsPage.vue)、[请求 DTO](../../../frontend/src/features/jobs/api/batchImage.ts)。
- **既有 case**：`FE-PAGE-BATCH-IMAGE-ACTION-2`；真实生成/账务关联用 `FE-PAGE-BATCH-IMAGE-EXTERNAL-UPSTREAM`，不能由本地 payload 校验替代。

### FS-JOB-03 详情、明细与图片预览
- **入口/前置**：选定当前用户/Key 所属任务；准备父子任务、失败项及本地小图片文件。
- **输入/流程**：查看进度、成功/失败计数与子任务，按明细状态读取 items；逐图主动加载预览、重新刷新详情，并展示失败信息。缩略图/Blob URL 按现有缓存与销毁规则管理，不转成公共跨用户图片缓存。
- **结果/异常**：失败图片不伪造预览；已换任务或卸载的响应不能更改当前明细，取消等待不宣称后台任务停止；终态以服务端 job/item 字段为准。
- **权威来源**：`GET /v1/images/batches/:id`、`/:id/items`、`/:id/items/:custom_id/content?image_index=...`；[详情实现](../../../frontend/src/features/jobs/pages/BatchImageJobsPage.vue)、[scope owner](../../../frontend/src/features/jobs/queries/useJobsRequest.ts)。
- **既有 case**：`FE-PAGE-BATCH-IMAGE-ACTION-1`、`FE-PAGE-BATCH-IMAGE-ACTION-2`。

### FS-JOB-04 取消与失败项重试
- **入口/前置**：非终态任务允许取消；终态且 fail_count>0 才允许失败重试，必须仍能取得任务原 Key 与失败项 prompt。
- **输入/流程**：取消提交 `/:id/cancel` 后读服务端状态；重试读取失败项，用其 prompt_preview 构造新 custom_id，沿原 model/provider、根 parent_batch_id 创建新关联批次，明确使用新的幂等标识。
- **结果/异常**：失败项没有可用 prompt 则拒绝重试；这不是“原任务恢复”或完整原始请求重放，不能声称参考图/全部原输入被无损重建。新批次可能产生新上游执行/账务；网络失败不得自动重复创建。
- **权威来源**：[canCancel/canRetry/retryFailedJob](../../../frontend/src/features/jobs/pages/BatchImageJobsPage.vue)、[取消/提交 API](../../../frontend/src/features/jobs/api/batchImage.ts)。
- **既有 case**：`FE-PAGE-BATCH-IMAGE-ACTION-1` 与 `FE-PAGE-BATCH-IMAGE-EXTERNAL-UPSTREAM`；现清单无独立“失败项重试”ID，应在父子任务外部场景明确记录，不能仅用查看列表结案。

### FS-JOB-05 ZIP 下载、批量下载与记录删除
- **入口/前置**：下载仅 completed 且 success_count>0；删除仅终态。多选后仅处理符合条件的任务；使用已有本地结果，不为下载测试启动真实任务。
- **输入/流程**：按任务所属 Key 下载 ZIP，或按当前选择批量下载；删除需确认所选记录，批量操作逐项处理。Blob 下载生成并释放临时 object URL。
- **结果/异常**：部分失败逐项反馈，不把某一成功折算成整批成功；删除记录不等价于取消正在运行的模型生成或退款。换 scope 停止接纳下载和删除旧结果。
- **权威来源**：`GET /v1/images/batches/:id/download`、`DELETE /v1/images/batches/:id`；[工作台动作](../../../frontend/src/features/jobs/pages/BatchImageJobsPage.vue)、[API](../../../frontend/src/features/jobs/api/batchImage.ts)。
- **既有 case**：`FE-PAGE-BATCH-IMAGE-ACTION-3`。

### FS-JOB-06 使用指南与提示词复制
- **入口/流程**：页面“使用指南”打开说明弹窗，可关闭或复制 agent 使用说明；说明使用当前展示 endpoint、项目 skill 名称及已有 models/submit/status/items/download/cancel URL，示例中的模型、prompt 等为占位内容。
- **提示词浮层**：任务明细中的 prompt_preview 可由悬停、点击或键盘聚焦展开，浮层复制其当前显示文本；空文本不发送复制动作，不能把预览值宣称为完整原始输入或附带参考图。
- **结果/边界**：两种复制只使用现有剪贴板服务，不创建任务、不发起上游生成、不获取额外授权；复制成功不代表第三方 agent 或生成 API 已联通。剪贴板失败沿原反馈处理。
- **权威来源**：[指南、agentInstruction、copyInstruction 与 copyPromptPopover](../../../frontend/src/features/jobs/pages/BatchImageJobsPage.vue)、[技术 skill 标识](../../../frontend/src/shared/config/project.ts)。
- **追踪**：现有 `FE-PAGE-BATCH-IMAGE-ACTION-1`、`FE-PAGE-BATCH-IMAGE-ACTION-2` 为组合场景，不是这两个复制动作的独立执行证明；逐页验收须在相应 variant/evidence 明确记录打开、关闭、复制成功和复制失败。

## FS-SUPPLY：供应关系、门户与结算

### FS-SUPPLY-01 门户 origin、11 tab 与不可用页
- **入口/前置**：配置供应商 origin + vendor_console_enabled；准备 vendor-scope、一级/二级/同级/空 scope fixture。匿名去登录并使用安全 redirect；普通用户/普通管理员、错误 origin 或关闭门户进入 `/vendor-unavailable`。
- **输入/流程**：读取 `/vendor/me` 和当前身份后进入表中 tab；一级才可管理直属下级。超管进入 platform，旧 accounts/proxies/usage/settlements 入口按 guard 转为 platform query tab；不可用页提供重试入口和退出，不提供提权。
- **结果/异常（验收合同）**：父/子/同级和空 scope 不互通；role、origin、目标变化应销毁旧页请求和选择。`vendor_id=0` 仅明确的安全聚合；scope 最后写入请求，不能被旧 query 覆盖。
- **实际接线**：root Router 直接调用 createVendorPortalGuard，重复内联规则已移除。设置等待前捕获 sessionEpoch；同会话刷新身份后接受新的权限投影，供应商读取期间再核对 policy/scope 与 session。真实 Router 的旧身份/迟到响应负例已加入开发回归，部署入口验收仍待执行。
- **权威来源**：[实际 router guard](../../../frontend/src/app/router/index.ts)、[门户装配](../../../frontend/src/app/compositions/vendor-portal/VendorPortalPage.vue)、[scope](../../../frontend/src/features/supply/scope.ts)、[不可用页](../../../frontend/src/app/compositions/vendor-portal/VendorUnavailablePage.vue)。
- **既有 case**：`FE-SEC-005`、`FE-PAGE-VENDOR-UNAVAILABLE-ACTION-1`、`FE-PAGE-VENDOR-UNAVAILABLE-ACTION-2`；各 tab 的精确入口见下表。

以下矩阵是**真实 role 与 router/component 分支**，不是 role 被 scope 替代；“直达”仍须满足正确 origin、开关、登录及供应商读取前置。普通 user/admin 均走 unavailable；super_admin 不调用供应商 me 来冒充 vendor。
| tab | vendor_level1 | vendor_level2 | super_admin | 既有入口 case |
| --- | --- | --- | --- | --- |
| accounts | 直达，本供应商 | 直达，本供应商 | → platform?tab=accounts | FE-PAGE-VENDOR-ACCOUNTS-ENTRY |
| proxies | 直达，本供应商 | 直达，本供应商 | → platform?tab=proxies | FE-PAGE-VENDOR-PROXIES-ENTRY |
| usage | 直达，本供应商 | 直达，本供应商 | → platform?tab=usage | FE-PAGE-VENDOR-USAGE-ENTRY |
| settlements | 直达，应收查看 | 直达，应收查看 | → platform?tab=settlements | FE-PAGE-VENDOR-SETTLEMENTS-ENTRY |
| profile | 直达 | 直达 | 直达，无供应商冒充 | FE-PAGE-VENDOR-PROFILE-ENTRY |
| platform | → /vendor/accounts | → /vendor/accounts | 直达七个 query tab | FE-PAGE-VENDOR-PLATFORM-ENTRY |
| children | 直达，管理直属二级 | → /vendor/accounts | → /vendor/platform | FE-PAGE-VENDOR-CHILDREN-ENTRY |
| child-accounts | 直达，全部/具体直属子账号只读 | → /vendor/accounts | → /vendor/platform | FE-PAGE-VENDOR-CHILD-ACCOUNTS-ENTRY |
| child-proxies | 直达，全部/具体直属子代理只读 | → /vendor/accounts | → /vendor/platform | FE-PAGE-VENDOR-CHILD-PROXIES-ENTRY |
| child-usage | 直达，须选择具体直属子节点 | → /vendor/accounts | → /vendor/platform | FE-PAGE-VENDOR-CHILD-USAGE-ENTRY |
| payables | 直达，直属二级的付款方视角 | → /vendor/accounts | → /vendor/platform | FE-PAGE-VENDOR-PAYABLES-ENTRY |

### FS-SUPPLY-02 平台工作区七个 query tab
- **入口/前置**：`/vendor/platform?tab=...` 仅超管；query tab 为 suppliers/accounts/proxies/usage/settlements/supply/audit，不是再增加七条路由。
- **输入/流程**：选择全体或单一供应商，搜索/切换七个工作面并从列表下钻；accounts/proxies 全体范围使用 `vendorId:0, readOnly:true`，选定实际 owner 后才提供对应写入口；usage 可把当前条件带入结算草稿。
- **结果/异常**：未知 tab 回 suppliers，缺失/错误/越界 owner 不按任意供应商默认；切目标清除旧详情、草稿过滤和晚到结果。platform 不是“冒充供应商登录”。
- **权威来源**：[平台工作区](../../../frontend/src/app/compositions/vendor-portal/VendorPlatformWorkspace.vue)、[供应 API](../../../frontend/src/features/supply/api.ts)、[scope 合同](../../../frontend/src/features/supply/scope.ts)。
- **既有 case**：`FE-PAGE-VENDOR-PLATFORM-ACTION-1`、`FE-PAGE-VENDOR-PLATFORM-ACTION-2`、`FE-PAGE-VENDOR-PLATFORM-ACTION-3`。

### FS-SUPPLY-03 供应商开通、策略编辑与直属下级
- Composite 组：父级或本供应商已授权的 Composite 组可在供应商编辑及账号创建选择；平台名单与父级授权仍由服务端限制，Composite 不意味着可选任意组/平台。
- **入口/前置**：platform/suppliers 或一级的 `/vendor/children`；父节点必须可管理，二级没有 children 创建入口。使用 vendor-scope 的 disposable 账号。
- **输入/流程**：创建需邮箱（≤254 字符）、6–72 字符密码、名称；提供显式随机密码按钮。选择层级/父级、active 等状态、平台、组、OpenAI native/bps 授权模式、账号/代理上限（0–10000，0 不限）、直连、联系人/结算邮箱/Binance 标识、默认/分组系数和汇率；子策略不得超父能力。系数字符串最多 8 位小数，汇率输入最多 10 位，不擅自 Number 重算合同价格。
- **结果/异常**：创建把身份与供应商信息作为一次请求；编辑带 policy_version，不夹带 contract_version 冒充合同变更。actor 更换清除凭据并关闭旧编辑器；目标/版本更换重建草稿；失败保留当前输入，取消 step-up 不报业务失败、不自动重放。
- **权威来源**：平台 `GET/POST /admin/vendors`、`GET/PATCH /admin/vendors/:id`；直属下级对应 `/vendor/vendors`；[编辑器](../../../frontend/src/features/supply/components/VendorEditor.vue)、[管理列表](../../../frontend/src/features/supply/components/VendorManagement.vue)、[API](../../../frontend/src/features/supply/api.ts)。
- **既有 case**：`FE-PAGE-VENDOR-PLATFORM-ACTION-3`、`FE-PAGE-VENDOR-CHILDREN-ACTION-1`、`FE-PAGE-VENDOR-CHILDREN-ACTION-2`。

### FS-SUPPLY-04 账号清单、详情、编辑与批操作
- 管理视角与展示：超管以实际 vendor owner 和现有 `view=management` 获取原生账号组分配能力，聚合仍只读；共用 app 层 PlatformTypeBadge 显示授权/计费/订阅/compact 元数据。显示标签不是权限，已脱敏 error_message 优先保留。
- **入口/前置**：本供应商 accounts 或超管选定供应商的 accounts 工作区；聚合/children readOnly 不可写，archived 等账号状态另限制动作。
- **输入/流程**：按实际名称、平台、类型、状态、时间等条件筛选排序/分页/列选择；查看账号、用量及授权详情；打开所属资源编辑器，执行页面提供的暂停/恢复、refresh、usage-refresh、probe、sync 和删除；批量编辑入口只在 scope.admin 的平台管理视角出现，不能据供应商 case 标题假设普通 vendor 同样拥有该按钮。
- **结果/异常**：账号/凭据方式/Native-BPS-Cloud 语义归[资源章](02-RESOURCES-ACCESS-CATALOG.md)，不能压成普通用户 DTO。管理端 `Account.excel_bps_403_recovery` 已存在不表示supplier已获得安全投影；supplier边界仍未提供，不得透传私有extra或把未知状态显示为正常。每个命令必须绑定实际 owner、账号 ID 与当前权限；不能向全体聚合发写请求，失败/取消不对新目标补发。
- **权威来源**：`/vendor/accounts` 与超管 `/admin/accounts`；创建 POST，供应商更新 **PATCH**、管理员更新 **PUT**，动作使用 `/:id/:action`；[账号工作台](../../../frontend/src/app/compositions/vendor-portal/components/AccountWorkbench.vue)、[角色 API 映射](../../../frontend/src/features/supply/api.ts)。
- **既有 case**：`FE-PAGE-VENDOR-ACCOUNTS-ACTION-1`、`FE-PAGE-VENDOR-ACCOUNTS-ACTION-2`；真实探测用 `FE-PAGE-VENDOR-ACCOUNTS-EXTERNAL-UPSTREAM`。

### FS-SUPPLY-05 账号授权、导入导出与敏感确认
- **入口/前置**：可写账号工作区；供应商策略和账号授权投影共同决定 native/bps 可操作性，历史 usage 的 cloud 标签不新增供应商授权能力。
- **输入/流程**：已有授权可重新授权、启停、刷新/撤销，BPS仅使用明确勾选模型，缺失/null/空/畸形不代表全选，不能自动迁为全部模型；Cloud优先且保留已保存列表，关闭Cloud后按列表恢复；该要求是本次用户修订，非025945维护内容；OAuth 建立 session、按租约更换代理并提交 code 完成。导入先解析/预览、处理映射和逐条结果；导出确认敏感内容后提交选中 IDs。
- **结果/异常**：授权 code/密码/导出凭据不进入普通偏好或证据；临时租约、代理版本、scope 过期不能完成旧授权。导入部分失败按实际成功项刷新，不将失败整批标绿；导出带 sensitive_confirmed，不是无权限下载通道。
- **权威来源**：`/vendor/oauth/:platform/sessions`、`/:session/complete`、PATCH `/:session/proxy`（超管用 `/admin/vendors/oauth`）；账号 `/import/preview`、`/import`、`/export`；[授权展示](../../../frontend/src/features/supply/components/AccountAuthorizations.vue)、[账号导入](../../../frontend/src/app/compositions/vendor-portal/components/AccountImport.vue)、[API](../../../frontend/src/features/supply/api.ts)。
- **既有 case**：`FE-PAGE-VENDOR-ACCOUNTS-ACTION-2`、`FE-PAGE-VENDOR-ACCOUNTS-EXTERNAL-OAUTH`；外部授权与本地导入/展示分开留证。

### FS-SUPPLY-06 代理配置、导入导出与诊断
- **入口/前置**：本供应商 proxies 或超管单一 owner；全体/子资源聚合只读。准备本地无外网出口代理 fixture，真实网络检查另获授权。
- **输入/流程**：按协议/状态/名称筛选分页、详情/复制地址；创建/编辑协议、host、1–65535 整数 port、凭据意图、到期/备用设置；导入预览后提交，导出、勾选批量删除需确认；测试和质量检查提交当前 config_version。
- **结果/异常**：编辑遵守 credential_action，空口令不擅自覆盖既有凭据；Idempotency-Key 绑定该次创建/编辑/导入意图。批量及并发测试逐项显示结果；旧配置版本的探测结果不能覆盖新配置，换 scope 停止等待且不重发写命令。
- **权威来源**：`/{admin|vendor}/proxies`、`/:id`、`/batch-delete`、`/import[/preview]`、`/export`、`/:id/{test,quality-check}`；[工作台](../../../frontend/src/app/compositions/vendor-portal/components/ProxyWorkbench.vue)、[表单](../../../frontend/src/app/compositions/vendor-portal/components/ProxyForm.vue)、[API](../../../frontend/src/features/supply/api.ts)。
- **既有 case**：`FE-PAGE-VENDOR-PROXIES-ACTION-1`、`FE-PAGE-VENDOR-PROXIES-ACTION-2`、`FE-PAGE-VENDOR-PROXIES-EXTERNAL-NETWORK`。

### FS-SUPPLY-07 用量筛选、账号下钻和平台导出
- **入口/前置**：`/vendor/usage`、child-usage、platform/usage 或账号用量详情；准备 usage-observations、vendor-scope，scope 不由可编辑 query 推导授权。
- **输入/流程**：草稿条件明确 apply/cancel/reset，按日期、组、账号和授权模式查看 summary/trend/accounts、排序分页；平台管理员可导出 CSV，并从用量条件建立结算草稿；普通供应商视角没有该“导出全部”按钮。
- **结果/异常**：区分业务量、成本、不同授权模式及缺失观测，不将 unknown/lower_bound 补零。用量筛选不改账本；取消草稿条件不发写命令，切目标不保留旧账号/旧图表。
- **权威来源**：`GET /vendor/usage/{summary,trend,accounts}`、`/{admin|vendor}/accounts/:id/usage`；平台 `/admin/vendors/usage`、`/usage/export`；[用量工作台](../../../frontend/src/app/compositions/vendor-portal/components/UsageWorkbench.vue)、[API](../../../frontend/src/features/supply/api.ts)。
- **既有 case**：`FE-PAGE-VENDOR-USAGE-ACTION-1`、`FE-PAGE-VENDOR-USAGE-ACTION-2`、`FE-PAGE-VENDOR-PLATFORM-ACTION-1`。case 的“导出”须按角色实际按钮执行，不虚构普通供应商导出接口。

### FS-SUPPLY-08 子账号、子代理、子用量只读下钻
- **入口/前置**：一级的 child-accounts、child-proxies、child-usage；账号/代理可选全部直属子节点或指定 vendor_id，子用量必须选择具体直属子节点；二级/同级/跨父目标必须被拒绝。
- **输入/流程**：从 children 列表导航并带 vendor_id，或在子资源选择器更换目标；过滤、分页、刷新、查看所属供应商和详情，按正确 owner 构造只读资源范围。
- **结果/异常**：全部子节点不变成普通 vendor_id=0 写通道；只读禁止导入、修改、删除、授权等命令。child-usage 未选择时显示选择提示而不装载 UsageWorkbench；选项加载失败/空列表不自动选一个未知 supplier；旧选择和晚到数据在切换时清除。
- **权威来源**：[子资源装配](../../../frontend/src/app/compositions/vendor-portal/components/VendorChildrenView.vue)、[scopeQuery/mutationScope](../../../frontend/src/features/supply/scope.ts)、[API](../../../frontend/src/features/supply/api.ts)。
- **既有 case**：`FE-PAGE-VENDOR-CHILD-ACCOUNTS-ACTION-1`、`FE-PAGE-VENDOR-CHILD-ACCOUNTS-ACTION-2`、`FE-PAGE-VENDOR-CHILD-PROXIES-ACTION-1`、`FE-PAGE-VENDOR-CHILD-PROXIES-ACTION-2`、`FE-PAGE-VENDOR-CHILD-USAGE-ACTION-1`、`FE-PAGE-VENDOR-CHILD-USAGE-ACTION-2`。

### FS-SUPPLY-09 应收/应付账单列表、详情和导出
- **入口/前置**：供应商 settlements 是应收查看；一级 payables 是对直属二级的付款方视角；超管 platform/settlements 管理平台对一级的账单。不同付款主体不互换。
- **输入/流程**：按状态/期间/供应商筛选、分页、打开账单，读取行项目、结算单价、汇率、期间、晚到用量、付款/冲正、调整及分配页；按所属路径导出单据。
- **结果/异常**：普通应收视角仅展示 pending_payment/partially_paid/paid 等已公布状态；管理视角另有 draft/void。金额与币种/系数以服务端字符串/DTO 为准，前端不重算权威账本；失败不把缺数据显示为已结清。
- **权威来源**：`/vendor/settlements`、`/vendor/payables`、`/admin/vendors/settlements` 的列表/`/:id`/`/:id/export`；[结算工作台](../../../frontend/src/features/supply/components/SettlementWorkbench.vue)、[结算 DTO](../../../frontend/src/features/supply/types.ts)、[API](../../../frontend/src/features/supply/api.ts)。
- **既有 case**：`FE-PAGE-VENDOR-SETTLEMENTS-ACTION-1`、`FE-PAGE-VENDOR-SETTLEMENTS-ACTION-2`、`FE-PAGE-VENDOR-PAYABLES-ACTION-1`。应收角色不得按 case 的宽泛“草稿/确认”文字获得管理按钮。

### FS-SUPPLY-10 结算预览、合同期间与草稿落单
- **入口/前置**：canDraft 仅平台付款方或合法一级付款方；平台收款目标限无父一级，下级应付目标限当前一级的直属二级；准备可销毁结算和合同版本数据。
- **输入/流程**：选择收款方、起止/时区、汇率/来源、调整和界面允许的默认/组系数、合同期间，先请求预览；平台允许的合同覆盖与一级付款方字段不同，不能跨主体借用。修改预览输入使旧 preview_token 失效。
- **结果/异常**：保存只发送待落单 preview_token 与绑定的 idempotency_key；逐收款方展示成功/失败，保留已成功项，失败不能重新自动创建已成功草稿。重试必须沿既有幂等/预览版本合同，不靠前端估算金额。
- **权威来源**：`POST /admin/vendors/settlements/preview`、`POST /admin/vendors/settlements`；付款方对应 `/vendor/payables/preview`、`/vendor/payables`；[预览/草稿逻辑](../../../frontend/src/features/supply/components/SettlementWorkbench.vue)、[API](../../../frontend/src/features/supply/api.ts)。
- **既有 case**：`FE-PAGE-VENDOR-PLATFORM-ACTION-3`、`FE-PAGE-VENDOR-PAYABLES-ACTION-2`；普通 `/vendor/settlements` 不拥有本命令。

### FS-SUPPLY-11 确认、作废、付款登记、冲正与调整
- **入口/前置**：当前付款方可管理的账单；draft 结束时间未到时不允许确认，付款/作废/调整依实际状态按钮；冲正必须选定已有付款记录。
- **输入/流程**：动作携带 statement.version；付款输入正金额、paid_at、channel、reference 并显式勾选确认；冲正/调整/作废提交相应金额或原因，付款/冲正/调整使用独立 idempotency_key；成功重读账单和列表。
- **结果/异常**：这是供应商结算账本的**付款登记**，不是调用第 3 章支付 provider 扣款/自动转账。版本冲突、部分结果和网络未知保留操作上下文并读权威事实；换付款方/目标、取消不重发旧账务命令。
- **权威来源**：`POST /{admin/vendors/settlements|vendor/payables}/:id/{confirm,void,payments,adjustments}`、`/payments/:payment_id/reverse`；[动作表单](../../../frontend/src/features/supply/components/SettlementWorkbench.vue)、[API](../../../frontend/src/features/supply/api.ts)。
- **既有 case**：`FE-PAGE-VENDOR-PAYABLES-ACTION-2`、`FE-PAGE-VENDOR-PLATFORM-ACTION-3`；现有宽粒度 case 要分别记录每种账务动作，不把一次预览计为全部通过。

### FS-SUPPLY-12 门户资料与安全入口
- **入口/前置**：`/vendor/profile`；当前供应商或门户超管，正常登录，准备 profile-security fixture。
- **输入/流程**：info/password 两个页面 tab；显示当前用户、角色及供应商相关资料，进入已有密码、TOTP、Passkey 安全面板；密钥、设备与身份绑定的详细规则归[第 1 章](01-PUBLIC-IDENTITY-KEYS.md)，不另造供应商登录身份。
- **结果/异常**：安全挑战可取消；旧角色的私有资料/验证码/秘密不残留。真实 WebAuthn 需要获准 authenticator，不以本地空设备列表作为外部成功。
- **权威来源**：[门户账号设置](../../../frontend/src/app/compositions/vendor-portal/components/VendorAccountSettings.vue)、[安全面板装配](../../../frontend/src/app/compositions/vendor-portal/panels.ts)、[供应商 store](../../../frontend/src/features/supply/store.ts)。
- **既有 case**：`FE-PAGE-VENDOR-PROFILE-ACTION-1`、`FE-PAGE-VENDOR-PROFILE-ACTION-2`、`FE-PAGE-VENDOR-PROFILE-EXTERNAL-WEBAUTHN`。

### FS-SUPPLY-13 组供给观察、供应商审计与身份对账
- **入口/前置**：超管 platform 的 supply/audit/suppliers 工作面；组供给是只读表，显示组、平台、供应商、account_count、available_count，支持搜索和分页。
- **输入/流程**：审计按本组件条件查询；身份对账仅超管手动指定 after_id（非负安全整数）与 limit（1–25），按服务端 rows、next_after_id、done 显示 claimed/失败，并按页推进或重试实际入口。它不是浏览器执行数据库迁移。
- **结果/异常**：供给数据没有编辑/保存按钮；对账失败按账号行保留，不凭一次响应宣称全部身份已归并；换 scope/卸载忽略旧进度。归属和审计事件依服务端事实。
- **权威来源**：`GET /admin/vendors/group-supply`、`/admin/vendors/audit-logs`，`POST /admin/vendors/identity-reconciliation`；[供给表](../../../frontend/src/features/supply/components/GroupSupply.vue)、[审计](../../../frontend/src/features/supply/components/VendorAudit.vue)、[对账](../../../frontend/src/features/supply/components/IdentityReconciliation.vue)。
- **既有 case**：`FE-PAGE-VENDOR-PLATFORM-ACTION-1`、`FE-PAGE-VENDOR-PLATFORM-ACTION-3`。后者标题含“编辑组供给”，但本版源码只读；应按实际可执行范围记录，不能虚构组供给写 API。身份对账无单列 case ID，须由 main 处理覆盖缺口。

## FS-SYS：设置工作区与独立系统控制

### FS-SYS-01 设置工作区和领域边界
- **入口/前置**：`/admin/settings`。超管使用SettingsWorkspace的10个tab，有限管理员进入DelegatedSettings；当前catalog有51个面板。独立bps tab和默认模板已删除，全站BPS gateway开关移至features、图片预算前，仍走原revision CAS；不改变字段权限、一次聚合保存及冲突处理。
- **输入/流程**：各面板接收只读 projection，修改本地 draft 后发字段 patch；工作区显示脏字段数和校验状态，全局保存只处理聚合字段。backup、roles 及标注 independent 的按钮属于独立命令，不因切 tab/点全局保存自动重复执行。
- **结果/异常**：加载失败可重试；saving/loading 禁重复保存，离页确认和 actor 更换清除原上下文。网站/协议/公告交第 1 章；身份、安全、角色定义交第 1 章；资源、access-policy、catalog 交[第 2 章](02-RESOURCES-ACCESS-CATALOG.md)；pricing/subscriptions/payments 交第 3 章；operations 的监控/错误展示配置归本章观测边界。
- **权威来源**：[SettingsWorkspace](../../../frontend/src/app/compositions/settings/SettingsWorkspace.vue)、[preparePatch](../../../frontend/src/app/compositions/settings/preparePatch.ts)、[域服务绑定](../../../frontend/src/app/compositions/settings/installServices.ts)。当前51面板名称、字段及保存边界归[设置与合同附录](SETTINGS_AND_CONTRACTS.md)，本节不把所有面板视为一次跨 API 事务。
- **既有 case**：`FE-PAGE-ADMIN-SETTINGS-ACTION-1`、`FE-PAGE-ADMIN-SETTINGS-ACTION-3`、各既有 FE-SETTING 条目；独立命令须执行自己的按钮。

### FS-SYS-02 revision、稀疏保存与冲突审阅
- **入口/前置**：超管聚合草稿，settings-revision fixture 的两名编辑者；revision/稀疏更新沿基线既有合同，本次改变前端组合与审阅责任；GET 必须给出有效 64 字符 revision。
- **输入/流程**：只向 `PUT /admin/settings` 发送 revision 和实际修改字段；sentFields/OmittedSettingKeys 是字段存在性合同，不凭空增加前端请求字段。保留省略、false、0、空集合、null 与各秘密字段的既有语义；配置过的秘密空输入不统一解释为清除。
- **结果/异常**：成功更新已提交字段的基线，保留未提交及提交后编辑的草稿；409 SETTINGS_REVISION_CONFLICT 保留原 baseline/draft/revision，读取服务器差异，用户逐字段选择保留后显式 merge，再手动保存；428 不依赖 reason 才识别。断网/5xx 等结果不明进入 uncertain，禁止自动更新 revision 重放旧写入。
- **权威来源**：[事务 owner](../../../frontend/src/app/compositions/settings/transaction.ts)、[控制 API/DTO](../../../frontend/src/features/system/api/settings.ts)、[当前 CT11 交接](../FRONTEND_HANDOFF.md)。
- **既有 case**：`FE-SET-001`、`FE-SET-002`、`FE-SET-003`、`FE-PAGE-ADMIN-SETTINGS-ACTION-2`。保存某个独立面板成功不能折算为 workspace CAS 成功。

### FS-SYS-03 委派设置与角色权限入口
- **入口/前置**：有限管理员须具有精确 settings 页面及 settings.general/content 的 read/manage 能力；roles tab 的实际权限编辑由 identity 角色面板负责。
- **输入/流程**：委派界面只展示允许的通用字段、协议内容和邮件模板只读/编辑入口；通用/内容分区共享事务基线，各自仅提交获授权的脏字段及 revision，邮件模板保留独立端点。角色/字段白名单不由浏览器扩大。
- **结果/异常**：无 manage 能力没有对应提交；后端拒绝保留当前输入并反馈。DelegatedSettings 复用事务及 SettingsConflictReview，只呈现可读字段并只合并可写字段；显式审阅后手动保存，不扩展为超管权限。确认保存后刷新公共投影，刷新失败单独反馈，不重放PUT或清除其他分区/提交后草稿；失效scope不发旧反馈。
- **权威来源**：[委派设置](../../../frontend/src/app/compositions/settings/DelegatedSettings.vue)、[角色权限面板](../../../frontend/src/features/identity/panels/AdminRolePermissions.vue)、[权限判定](../../../frontend/src/features/identity/model/auth.ts)。
- **既有 case**：`FE-SETTING-DELEGATEDSETTINGS-FIELDS`、`FE-SETTING-DELEGATEDSETTINGS-WRITE`、`FE-SETTING-ADMINROLEPERMISSIONS-FIELDS`、`FE-SETTING-ADMINROLEPERMISSIONS-WRITE`、`FE-AUTH-004`。本章只说明装配边界，权限字段规格交第 1 章。

### FS-SYS-04 控制台、供应商入口与插件开关
- **入口/前置**：超管 general/features；这些是 workspace draft 字段，尚未全局保存前不等于已生效。
- **输入/流程**：配置 backend_mode、供应商控制台/独立 origin、表格默认页长和候选页长、插件管理功能开关。vendor origin 要求 HTTPS、完整纯 origin、无凭据、不同于当前主站；页长整数 5–1000，候选按逗号解析、去重排序且不能为空。
- **结果/异常**：无效 origin 或页长阻止聚合保存，错误定位所属字段；站点 api_base_url 展示配置不改变管理 JWT 的 HTTP 目标。关闭入口不是卸载插件或删除供应商数据。
- **权威来源**：[ConsoleSettingsPanel](../../../frontend/src/features/system/settings/ConsoleSettingsPanel.vue)、[校验/patch](../../../frontend/src/features/system/settings/preparePatch.ts)、[插件开关面板](../../../frontend/src/features/system/settings/PluginFeatureSettingsPanel.vue)。
- **既有 case**：`FE-SETTING-CONSOLESETTINGSPANEL-FIELDS`、`FE-SETTING-CONSOLESETTINGSPANEL-WRITE`、`FE-SETTING-PLUGINFEATURESETTINGSPANEL-WRITE`。

### FS-SYS-05 SMTP 草稿、连接测试与测试邮件
- **入口/前置**：email tab；当前面板按 email_verify_enabled 展示。草稿保存用 settings-revision，本地投递测试用 registration-mail 捕获器；真实邮件另备获准地址。
- **输入/流程**：host/port/username/password、from_email/from_name/TLS 属聚合草稿；“连接测试”和“发送测试邮件”分别独立 POST。测试邮件须有收件人；未手动改密码时测试请求发送既有保留空值语义，不泄露已配置秘密。
- **结果/异常**：连接响应成功不等于真实收件箱送达；修改连接/收件目标、换 actor/卸载取消旧等待，错误/取消不重发邮件；输入新基线不产生虚假的脏 patch，秘密不进入日志/持久偏好。
- **权威来源**：`POST /admin/settings/test-smtp`、`/send-test-email`；[SMTP 面板](../../../frontend/src/features/system/settings/SmtpSettingsPanel.vue)、[API](../../../frontend/src/features/system/api/settings.ts)。
- **既有 case**：`FE-SETTING-SMTPSETTINGSPANEL-FIELDS`、`FE-SETTING-SMTPSETTINGSPANEL-WRITE`、`FE-SMTP-CONNECTION`、`FE-SMTP-DELIVERY`；外部条件另见 `FE-PAGE-ADMIN-SETTINGS-EXTERNAL-CAPTCHA`。

### FS-SYS-06 邮件模板选择、预览、保存与恢复
- **入口/前置**：email tab 或具备 settings.content.read 的委派界面；readonly 时无保存/恢复权限。
- **输入/流程**：读取 event/locale 列表，选择模板，编辑非空 subject/html、查看/复制 placeholder、生成预览；保存 PUT 指定 event/locale，恢复官方模板需确认，是独立 POST，不随 workspace 全局保存。
- **结果/异常**：切 event/locale/readOnly、换 actor、关闭后旧读取/预览/保存结果不覆盖当前草稿；保存期间又编辑的新草稿保留。失败保留草稿，不自动再写；预览是模板渲染，不是邮件发送证明。
- **权威来源**：`GET /admin/settings/email-templates`、`GET/PUT .../:event/:locale`，`POST .../:event/:locale/restore-official`、`POST /admin/settings/email-template-preview`；[模板编辑器](../../../frontend/src/features/system/settings/EmailTemplateEditor.vue)、[API](../../../frontend/src/features/system/api/settings.ts)。
- **既有 case**：`FE-SETTING-EMAILTEMPLATEEDITOR-FIELDS`、`FE-SETTING-EMAILTEMPLATEEDITOR-WRITE`。

### FS-SYS-07 管理访问密钥与历史单 Key
- **入口/前置**：security 的 AdminAccessKeysSettings 与 general 的历史 AdminKeysSettingsPanel；超管和有效 step-up，profile-security fixture。新型管理 Key 与用户推理 Key、管理 JWT 是不同凭据。
- **输入/流程**：新型 Key 输入非空名称、描述、enabled、可选有效期、allowed_ips、服务器给出的 global/account/group 权限和范围；支持新增、编辑、启停、轮换、撤销，以及只对 revoked 记录的物理删除。新建/轮换明文只在本次局部区域显示/复制/关闭，列表保留 prefix。
- **历史/异常**：legacy status 不存在时整个历史面板隐藏且不能新建；存在时仅确认再生成/删除并显示一次性新值。取消验证不变更，换 actor/卸载清除秘密并阻止旧续发；IP/策略合法性仍由服务端校验，不能将 UI 勾选视为实际授权。
- **权威来源**：新型 `/admin/settings/admin-api-keys` GET/POST、`/:id` PUT/DELETE、`/:id/rotate` POST、`/:id/record` DELETE；历史 `/admin/settings/admin-api-key` GET/DELETE、`/regenerate` POST；[新型面板](../../../frontend/src/features/identity/settings/AdminAccessKeysSettings.vue)、[新型 DTO/API](../../../frontend/src/features/identity/api/adminAccessKeys.ts)、[历史面板](../../../frontend/src/features/system/settings/AdminKeysSettingsPanel.vue)。
- **既有 case**：`FE-ADMIN-KEY-ONCE`、`FE-LEGACY-KEY-PRESENCE`、`FE-SETTING-ADMINACCESSKEYSSETTINGS-WRITE`、`FE-SETTING-ADMINKEYSSETTINGSPANEL-WRITE`。第 1 章管理身份/授权定义与本节交叉引用，不能重复计算一次性秘密验收。

### FS-SYS-08 备份存储、图像存储和计划/归档
- **入口/前置**：`/admin/settings` 的 backup section（仅超管，委派界面不含此面板）；使用隔离数据库及可销毁对象存储。它不是独立注册的 `/admin/backups` 前端页面。
- **输入/流程**：S3 endpoint/region/bucket/prefix/access key/secret/路径样式；图像存储可复用 backup S3 或使用其配置，包含公开地址、预签名期限、下载大小限制；分别保存和连接测试，并可展开/关闭 Cloudflare R2 配置指南（不代填真实凭据）。计划配置 enabled、cron、retain_days/retain_count；月归档选择日期/月末和保留份数，0 的永久保留语义保留。
- **结果/异常**：关闭月归档只提交关闭意图，不用隐藏参数覆盖历史设置；非法日期/保留值阻止保存；secret 空/省略沿现有语义，测试成功不证明备份恢复链成功。多处独立 API 不伪装 workspace 原子事务，取消/旧 actor 结果无 toast/草稿回写。
- **权威来源**：`/admin/backups/s3-config`、`/image-storage`、`/schedule` GET/PUT，前两者各自 `/test` POST；[备份页](../../../frontend/src/features/system/pages/BackupPage.vue)、[月归档面板](../../../frontend/src/features/system/components/BackupArchiveSettings.vue)、[API](../../../frontend/src/features/system/api/backup.ts)。
- **既有 case**：`FE-SETTING-BACKUPPAGE-FIELDS`、`FE-SETTING-BACKUPPAGE-WRITE`；真实对象存储为 `FE-PAGE-ADMIN-SETTINGS-EXTERNAL-STORAGE`。

### FS-SYS-09 手动备份、观察、下载、恢复与删除
- **入口/前置**：同一超管 backup section，使用一次性可恢复数据；敏感创建/下载/恢复按现有 step-up；恢复还需明确确认及密码。
- **输入/流程**：创建发送 expire_days，读取服务端 record 后轮询；观察 progress/status/restore_status，区分 completed/failed/running。下载沿单 URL 或分卷清单，显示索引/大小/可用校验信息；恢复绑定备份 ID；删除归档另确认并显式 delete_archived=true。
- **结果/异常**：409 表示已有进行中操作，不能当成新建成功；到达轮询上限提示仍运行而非失败/完成。页面隐藏、卸载、换 actor 停止等待和旧下载导航；结果未知不得重新自动创建/恢复；删除取消不发请求，恢复启动不等于恢复完成。
- **权威来源**：`GET/POST /admin/backups`、`GET/DELETE /admin/backups/:id`、`GET /:id/download-url`、`POST /:id/restore {password}`；[命令与轮询](../../../frontend/src/features/system/pages/BackupPage.vue)、[API/分卷 DTO](../../../frontend/src/features/system/api/backup.ts)。
- **既有 case**：`FE-SETTING-BACKUPPAGE-WRITE`、`FE-PAGE-ADMIN-SETTINGS-EXTERNAL-STORAGE`；必须记录真实读回/文件/恢复终态，不把生成 URL 或接口接受当作完成。

### FS-SYS-10 插件上传、兼容性、启停、灰度和测试
- **入口/前置**：`/admin/plugins`，精确页面权限及 plugin management 开关；准备合法/非法 synthetic .s2plugin、本地无副作用测试插件及 admin-compliance 状态。
- **输入/流程**：上传仅接受 .s2plugin 扩展名，服务端负责完整包/签名校验；列表展示版本、作者、signature、兼容/tested 信息和 runtime health。可启用、停用、卸载、调整 UI 1–100 灰度并测试；未 tested 启用、停用、卸载均按实际确认交互。
- **结果/异常**：敏感动作沿 step-up，读取实际安装/运行结果，不把 uploaded、starting 或测试失败当 enabled/healthy。操作在途禁重复，取消/卸载页面不继续旧授权回调；新列表示例不是安装任意外部二进制的授权。
- **权威来源**：`GET /admin/plugins`、`POST /upload`、`POST /:id/{enable,disable,test}`、`DELETE /:id`；[插件页](../../../frontend/src/features/system/pages/PluginsPage.vue)、[DTO/API](../../../frontend/src/features/system/api/plugins.ts)。
- **既有 case**：`FE-PAGE-ADMIN-PLUGINS-ACTION-1`、`FE-PAGE-ADMIN-PLUGINS-ACTION-2`、`FE-PAGE-ADMIN-PLUGINS-MANUAL-COMPLIANCE`。

### FS-SYS-11 插件配置 iframe 与消息桥
- **入口/前置**：已安装插件的“配置”；先申请当前 plugin ID 的 ui-session，UI bridge token 只用于该会话，不能作为管理 JWT。
- **输入/流程**：验证 iframe source、origin、消息来源、bridge_token、request_id；处理 ready、config.load/save/test、plugin.status、ui.resize/notify。配置保存是原始对象的一次 PUT；runtime status 只读，不借用 config.test 造成副作用。
- **结果/异常**：重复/过期请求 ID、旧页面/旧 iframe、错误 origin/token 不执行命令或回传敏感配置；关闭、重开、iframe 导航、换 actor 会使旧响应失效。取消 step-up 可回告当前 iframe 取消，不能对新插件保存旧配置。
- **权威来源**：`POST /admin/plugins/:id/ui-session`、`GET/PUT /:id/config`、`GET /:id/status`；[桥验证与命令 owner](../../../frontend/src/features/system/pages/PluginsPage.vue)、[API](../../../frontend/src/features/system/api/plugins.ts)。
- **既有 case**：`FE-PAGE-ADMIN-PLUGINS-ACTION-1`、`FE-PAGE-ADMIN-PLUGINS-ACTION-2`；消息来源/会话负例没有独立 case ID，需随本配置流程记录，不能仅以 iframe 显示计通过。

### FS-SYS-12 只读构建版本
- **入口/前置**：主站侧栏仅 super_admin 渲染 VersionBadge，降权即移除；供应商侧栏同样仅超管渲染徽标。只有超管可展开详情、强制刷新和复制版本，非超管不能继承私有构建详情。
- **输入/流程**：读取当前 version、可选 upstream_version/build_commit；供应商前端制品版本与后端版本分开显示，缺失项留缺失，复制仅含允许的构建信息。
- **结果/异常**：读取失败提示不可用，不能用后端版本冒充供应商 UI 制品来源。本前端系统接口只保留 getVersion；update/rollback/check-updates/portal-updater 已退役，不提供浏览器升级/回滚承诺。
- **权威来源**：`GET /admin/system/version`；[只读 API](../../../frontend/src/features/system/api/version.ts)、[VersionBadge](../../../frontend/src/app/components/VersionBadge.vue)、[门户版本源](../../../frontend/src/app/compositions/vendor-portal/version.ts)、[退休合同](../CONTRACTS.md)。
- **既有 case**：`FE-BOOT-001`、`FE-DEPLOY-001`、`FE-ARCH-001` 是现有来源/退出路径追踪；没有专用版本详情操作 case，需单列实际证据，不改造为更新功能。

### FS-SYS-13 初始化向导
- **入口/前置**：`/setup`，needs_setup=true 的一次性本地实例及有效 bootstrap token；needs_setup=false 时按匿名/user/admin 分支转登录或相应 dashboard。非初始化实例不能靠前端表单重新安装。
- **输入/流程**：数据库 host/port/user/password/dbname/sslmode，Redis host/port/username/password/db/enable_tls，管理员 email、至少 8 位且确认一致的密码，以及 server 配置；先完成数据库、Redis 测试，逐步前进/返回，再显式安装。
- **结果/异常**：测试/安装用 X-Setup-Token，不使用普通模型 Key；服务端决定连接/安装资格。成功清除本地秘密并等待服务恢复：初始 3 秒后最多 60 次状态观察，ready 后转登录；超时不重复 install。卸载停止等待并擦除密码/token，安装错误保留明确失败状态，不声称数据库已回滚。
- **权威来源**：网关根路径 `GET /setup/status`、`POST /setup/test-db`、`/setup/test-redis`、`/setup/install`，不是 `/api/v1/setup`；[向导](../../../frontend/src/features/system/pages/SetupWizardPage.vue)、[API/配置 DTO](../../../frontend/src/features/system/api/setup.ts)。
- **既有 case**：`FE-PAGE-SETUP-ACTION-1`、`FE-PAGE-SETUP-ACTION-2`、`FE-PAGE-SETUP-ACTION-3`、`FE-DEPLOY-002`；只在隔离一次性实例验收，不是生产安装或数据迁移授权。


## 追踪与验收边界

- 本章编号是功能规格定位，不是新的测试结果状态。所有 FE case ID 均指现有 [cases.json](../../../frontend/tests/acceptance/cases.json)；宽粒度 case 下的多个实际子动作须分别留证，不用页面数、按钮出现或组件测试通过替代业务验收。
- 已核对的清单/实现差异：组供给只读、普通供应商不展示平台用量导出、应收账单不提供付款方命令、child-usage 必须选具体子节点；已有 case 的宽泛标题按实际角色能力执行，原 catalog 本轮未修改。
- 计划测试细项、身份对账、版本详情、插件桥负例等未单列 case ID；本章给出父 case/现有追踪与实际来源，不虚构新 case 或已完成覆盖。main 可在根验收体系处理细粒度补充。
- 真实 Router 已接唯一 guard；角色矩阵以实际 guard + VendorPortalPage 为准。FR-04 旧误报撤回：原 route-inventory 已正确区分 platform/profile/一级专属页面，本版补 case 关联回归而非修改原本正确的角色。
- 环境与执行阶段以共享 [TEST_REPORT](../TEST_REPORT.md) 为结果入口，本任务未改该文件。本轮电脑工具仅预检旧90c927本机web-build，未执行业务步骤；前端e5已固定，753业务用例仍not_run，后端/API/角色镜像与联合runtime尚待完成。真实OAuth、模型执行/账务、邮件、对象存储、WebAuthn及人工条件分别准备。
