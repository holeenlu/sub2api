# 02 资源、准入、目录、定价与订阅功能规格

c2维护35路径的应用和前端开发证据已交接，当前e5源码已冻结；详见[最终证据](REVIEW-EVIDENCE.md#final-e5-evidence)。当前catalog为51面板/753业务定义，均not_run；开发测试不替代实际业务验收。

## 功能索引

| 功能编号 | 功能名 | 实际入口 | 角色 |
| --- | --- | --- | --- |
| FS-RES-001 | 账号查询、选择、刷新与观测列 | /admin/accounts | 获准管理员 |
| FS-RES-002 | 新建、编辑与复制账号 | 账号编辑对话框 | 有账号授权/管理及字段权限者 |
| FS-RES-003 | 平台凭据与资源授权 | 账号授权、重授权对话框 | 有 accounts.authorize 者 |
| FS-RES-004 | 传输、能力、模型及资源约束 | 账号编辑各领域面板 | 有相应字段权限者 |
| FS-RES-005 | 账号状态、恢复及独立命令 | 账号行菜单与状态列 | 有命令权限者 |
| FS-RES-006 | 账号批量修改与批量命令 | 账号选择工具栏 | 有命令及字段权限者 |
| FS-RES-007 | 账号导入、导出和 CRS 同步 | 账号工具菜单 | 授权管理员；秘密导出限超管 |
| FS-RES-008 | 错误透传、TLS 与诊断关联入口 | 账号工具/行菜单 | 超管或对应诊断权限者 |
| FS-RES-009 | 代理查询、创建和编辑 | /admin/proxies | 获准管理员 |
| FS-RES-010 | 代理批量、导入导出和秘密复制 | 代理工具栏及 URL 菜单 | 管理员；秘密导出限超管 |
| FS-RES-011 | 代理关联账号、连通性与质量 | 代理行/批量操作 | 有对应读取/管理权限者 |
| FS-RES-012 | 资源聚合设置与配额通知 | /admin/settings 的 gateway/features/notifications | super_admin |
| FS-RES-013 | 冷却、探测、用量与 WebSearch 独立设置 | /admin/settings 的 gateway | super_admin |
| FS-RES-014 | 账号优先级快捷编辑 | 账号表格 priority 列 | accounts.manage + priority 字段许可；服务端最终鉴权 |
| FS-ACCESS-001 | 分组查询、新建、复制和删除 | /admin/groups | 获准管理员 |
| FS-ACCESS-002 | Group 跨域草稿及原子保存 | GroupEditorDialog | 有 groups.manage 及字段权限者 |
| FS-ACCESS-003 | 准入、资源范围、路由与模型清单 | Group 准入面板 | 有对应字段权限者 |
| FS-ACCESS-004 | 复合路由、用户覆盖及排序 | Group 独立对话框 | 有命令及字段权限者 |
| FS-ACCESS-005 | 风控配置、规则和运行状态 | /admin/risk-control | 获准管理员；写权限另验 |
| FS-ACCESS-006 | 风控日志、详情、解封与 hash 清理 | 风控运行面板 | 有对应权限者 |
| FS-ACCESS-007 | 默认配额、可信 IP、客户端与 Fast 策略 | /admin/settings 对应面板 | super_admin |
| FS-ACCESS-008 | 面板限流与 Beta 策略独立设置 | /admin/settings 的 security/gateway | super_admin |
| FS-CAT-001 | 模型广场入口与目录过滤 | /model-plaza | 按站点开关允许的访客/用户 |
| FS-CAT-002 | 模型能力、报价与分组展示 | 模型广场及公开目录面板 | 当前入口允许的读者 |
| FS-CAT-003 | 用户可用渠道 | /available-channels | 主站已登录用户 |
| FS-CAT-004 | 平台选项、映射候选与清单选择 | 资源/Group 编辑器 | 当前编辑器获权操作者 |
| FS-CAT-005 | 目录可见性和说明设置 | /admin/settings 的 features | super_admin |
| FS-PRICE-001 | 渠道查询、状态和删除 | /admin/channels/pricing | 获准管理员；写需 channels.manage |
| FS-PRICE-002 | 渠道基本信息、组绑定和模型映射 | 渠道编辑对话框 | 有命令及字段权限者 |
| FS-PRICE-003 | 模型单价、区间、推理和时段价格 | 渠道模型定价卡 | 有相应价格字段权限者 |
| FS-PRICE-004 | 成本统计规则与模型同步 | 渠道编辑对话框 | 有对应成本/编辑权限者 |
| FS-PRICE-005 | Group 倍率、峰时及利润配置 | Group 定价面板 | 有相应价格字段权限者 |
| FS-PRICE-006 | Group 图像、视频和批图价格 | Group 媒体定价面板 | 有相应价格字段权限者 |
| FS-PRICE-007 | 站点计费模式配置 | /admin/settings 的 features | super_admin |
| FS-SUB-001 | Group 订阅策略及额度 | Group 订阅策略面板 | 有对应 Group 字段权限者 |
| FS-SUB-002 | 我的订阅、额度与续费入口 | /subscriptions | 主站已登录用户 |
| FS-SUB-003 | 管理订阅查询和用户选择 | /admin/subscriptions | 获准管理员 |
| FS-SUB-004 | 单人/多人分配订阅 | 分配订阅对话框 | 有 subscriptions.manage 者 |
| FS-SUB-005 | 延期、重置、撤销与恢复 | 订阅操作对话框 | 有 subscriptions.manage 者 |
| FS-SUB-006 | 批量订阅操作与结果未知恢复 | 订阅批量操作对话框 | 有 subscriptions.manage 者 |
| FS-SUB-007 | 默认订阅与到期提醒设置 | /admin/settings 的 users/notifications | super_admin |

## 口径与跨章节边界

- 原始比较基线为6f48d6ab6fcc29763af84410690530aeaa957403，维护输入固定c2a10f2；当前前端e5已冻结。完整6196测试/双build真实输入63fe，按唯一测试类型补丁和生产等价证明复用于e5；五项static直接绑定e5。旧f4fe、5c3005及5085记录保留原hash，详见[最终证据](REVIEW-EVIDENCE.md#final-e5-evidence)。开发验证不等于业务验收。
- 路由与用例依据 [route-inventory](../../../frontend/tests/acceptance/route-inventory.json)、[cases.json](../../../frontend/tests/acceptance/cases.json)。下列 case ID 是已有验收定义的关联，不是通过回执；753条当前定义、页面数量及源码存在均不能证明真实业务验收成功。工作状态与结果继续归 [work-items](../work-items.json)、[TEST_REPORT](../TEST_REPORT.md)。
- “获准管理员”指主站 super_admin，或精确页面被服务端 admin_pages 允许的 admin；还须分别满足命令 permissions、admin_write_fields 和资源归属。有限管理员缺少 policy_version 时先刷新当前资料；前端不扩展页面前缀权限，最终鉴权在服务端。[路由 guard](../../../frontend/src/app/router/index.ts)、[身份模型](../../../frontend/src/features/identity/model/auth.ts)。
- 管理编辑需要隔离的合成账号/组/代理/用户，列表与空态需要对应 fixture。真实授权需受控上游账号；真实出口/探测需测试代理及外部服务。执行前读取 [FIXTURES](../../../frontend/tests/acceptance/FIXTURES.md)，不得拿真实客户对象试写。
- 第一章负责登录身份、API Key 和文档容器；本章负责资源、组、目录、价格事实及订阅授予。套餐售卖配置实际位于 payments 的 `/admin/orders/plans`，创建订单、支付、退款及订阅购买属于第三章；本章不把订阅管理当成收款或退款入口。[套餐路由](../../../frontend/src/features/payments/routes.ts)、[套餐页面](../../../frontend/src/features/payments/pages/AdminPaymentPlansPage.vue)。
- 第四章负责供应关系、作业与运行诊断；`/vendor/accounts`、`/vendor/proxies` 复用资源能力，但受供应商 origin、层级、合同和目标 scope 约束；child-accounts/child-proxies 不能被描述为普通管理员全量资源编辑。[第四章](04-OPERATIONS-SUPPLY-SYSTEM.md)。
- 下述所有失败边界区分“请求失败”和“服务端确定未执行”。离开页面、换 actor/scope/目标须取消查询或拒绝迟到结果；Abort 不证明服务端回滚。没有接口幂等保证的写操作结果未知时应先重读权威对象，不能自动重放。金额、额度、调度资格、能力及 revision 不由前端猜造。

## 资源账号与代理

### FS-RES-001 账号查询、选择、刷新与观测列
- 元数据展示：管理员与供应商账号工作台共用 app 组合层 PlatformTypeBadge；按平台区分 Claude Pro/Max5x/Max20x 与 OpenAI 计划，展示授权方式、计费类型、native/bps 投影与 compact 状态。`authorization_modes` 和 `auth_mode` 是后端只读元数据，不自行推断、授予或扩大可执行授权。 Meitner已接入名称直接复制、独立详情按钮、surface tooltip与可选择的结构化元数据；删除列表中重复proxy摘要，复用PlatformTypeBadge slot。局部190项组件验证与联合类型修复已交接，真实浏览器交互仍待验；最终构建按63fe→e5证据复用。
- 前置/输入：获准进入 `/admin/accounts`；按名称搜索、platform/type/status/privacy_mode/group 筛选，`ungrouped` 是未分组哨兵；可排序、分页、调整列和自动刷新间隔。所选行、当前页和全部匹配结果是不同选择范围。
- 流程/结果：读取列表及必要的批量用量/统计，展示状态、可调度开关、容量、优先级、组、代理、费用和平台配额；新增/删除导致的列表变化有待同步提示。全选匹配结果使用真实查询，不以当前页冒充全部；成本列受 billing.cost.read 约束。
- 异常/取消：列表失败与无结果分别呈现；actor/scope 改变关闭旧目标弹窗并清选择、失效在途动作；用量加载失败不补零或推断账号失效。自动刷新不是上游主动探测承诺。
- 权威/源码：GET `/admin/accounts`、POST `/admin/accounts/usage/batch`、`/today-stats/batch`；[AccountsPage](../../../frontend/src/app/compositions/resource-workspace/AccountsPage.vue)、[列表与观测 API](../../../frontend/src/features/resources/api/accounts.ts)、[筛选合同](../../../frontend/src/features/resources/model/accountTableFilters.ts)。用例：`FE-PAGE-ADMIN-ACCOUNTS-ENTRY`、`FE-PAGE-ADMIN-ACCOUNTS-REFRESH`、`FE-PAGE-ADMIN-ACCOUNTS-BACK`、`FE-PAGE-ADMIN-ACCOUNTS-ACTION-1`。

### FS-RES-002 新建、编辑与复制账号
- 前置/输入：新建/复制需 accounts.authorize，编辑需 accounts.manage，并按字段授权开放传输、价格和凭据。输入名称、备注、平台、凭据类型、并发、优先级、负载因子、到期、组和代理；默认并发/优先级读取 `/admin/accounts/defaults`。普通名称不得空；apikey/upstream 要求其密钥，upstream 还要求 Base URL；Bedrock 按所选 API Key 或 AK/SK 模式校验，Vertex 按凭据模式校验项目/位置。
- 流程/结果：创建 POST `/admin/accounts`，编辑 PUT `/admin/accounts/:id`；成功后更新列表。复制仅非影子 apikey/upstream/bedrock/service_account，调用 `/admin/accounts/:id/duplicate` 保存列表没有承载的配置，并复用未确定操作的幂等键。
- 异常/取消：`409 mixed_channel_warning` 是需确认的具体混合组风险，不是泛化 revision 冲突；确认只重发被拒绝的原目标/原 payload，换目标/权限/关闭后旧确认失效。后续模型元数据同步或成本探测失败可提示警告，不能把已经创建的账号说成未创建。没有为普通更新虚构 CAS revision。
- 权威/源码：[创建面板](../../../frontend/src/features/resources/components/CreateAccountModal.vue)、[创建提交](../../../frontend/src/features/resources/model/createSubmission.ts)、[编辑提交](../../../frontend/src/features/resources/model/editSubmission.ts)、[账号 API](../../../frontend/src/features/resources/api/accounts.ts)。用例：`FE-PAGE-ADMIN-ACCOUNTS-ACTION-2`。

### FS-RES-003 平台凭据与资源授权
- Excel授权状态：先表达现有API的authorized事实；source=primary且缺少授权时明确要求在账号OAuth中重新授权，不误显示为已就绪，也不开放独立grant导入/删除。状态未知或读取失败时不推定为独立授权。生成、交换、保存及重读之间复核actor/scope、目标与accounts.authorize；失效后清理秘密草稿，禁止继续下一步请求或回写旧结果。该要求使用已有DTO，与下节管理安全DTO迁移及未闭环并发条件分开。
- 前置/输入：授权是上游资源授权，不是站点登录。各平台只显示实际支持的凭据方式：Anthropic OAuth/setup-token/手填长效 token；OpenAI OAuth、refresh/session 导入、Codex 会话文件/PAT 等现有入口；Gemini 的 google_one/code_assist/ai_studio、API Key/Vertex；Antigravity OAuth；Grok SSO/API Key。不得推断任意平台支持所有方式。
- 流程/结果：生成授权 URL、外部登录、回填 code/state/session 或导入凭据，再创建或向现有账号应用授权。重授权保留目标账号；OpenAI Excel/BPS 独立授权可查询、发起、回填或导入 JSON、撤销，`source=primary` 不显示独立撤销。相关 credential/grant 数据不从普通列表恢复秘密。
- 异常/取消：上游拒绝、过期 code、错误 JSON、代理失败分别保留错误；关闭/换账号清授权 URL、回调与本地秘密。影子账号不持凭据，不开放重授权/刷新 token。真实 OAuth 往返必须用受控上游环境，单元替身不能证明成功。
- 权威/源码：各平台 `/admin/{platform}/…` 授权 API、POST `/admin/accounts/:id/apply-oauth-credentials`、`/admin/accounts/import/codex-session`、`/admin/openai/create-from-codex-pat`；[授权流程](../../../frontend/src/features/resources/components/OAuthAuthorizationFlow.vue)、[重授权](../../../frontend/src/features/resources/components/ReAuthAccountModal.vue)、[OpenAI 创建授权](../../../frontend/src/features/resources/model/createOpenAIAuthorization.ts)、[BPS 授权](../../../frontend/src/features/resources/components/ExcelBPSAuthorization.vue)、[授权 API](../../../frontend/src/features/resources/api/editorOperations.ts)。用例：`FE-PAGE-ADMIN-ACCOUNTS-ACTION-2`、`FE-PAGE-ADMIN-ACCOUNTS-EXTERNAL-OAUTH`。

### FS-RES-004 传输、能力、模型及资源约束
- 前置/输入：编辑器按平台、凭据方式、Native/BPS/Cloud、传输和操作能力的实际组合显示字段；这些不是同一开关。可编辑 Base URL/协议、自定义请求头、模型白名单/映射、Responses/Compact、WebSocket 模式、透传、命名空间工具、图像响应、WebSearch、长上下文、缓存/重试/错误码、TLS、会话/RPM/窗口及总量/日/周额度等适用字段。
- 流程/结果：字段由 resources 自有草稿组装 credentials/extra 及顶层字段；组/代理是关联选择。模型候选及同步预览读取后端；headers 校验名称和值，429/529 特殊错误策略有确认；Cloud 资格、BPS 403 恢复间隔/目标组按现有规则验证。编辑未填的新密钥不等于清空已有秘密。
- BPS 开关与恢复：按[2026-10-27执行合同](../CONTRACTS.md#bps-routing-20261027)保留符合条件403后自动持久化停用、条件恢复及其配置，页面展示服务端实际开关与恢复状态。取消的是同一次 BPS 执行中的 Native 回退；后续独立新执行仍可按有效配置和原生授权走 Native，Excel-only 不可。旧 previous_response_id、工具回复和取消命令不随开关变化迁移；浏览器不新增路由意图字段或以临时熔断替换真实开关。后端执行链与真实联调证据另验。
- 显式模型选择（本次用户修订）：账号级单一开关不拉取模板、不自动填充历史缺失模型、不重置已选模型和选项。BPS只匹配最终账号映射后的明确勾选模型；缺失、空、null或畸形列表均不表示全选，不从预设或旧all_models补全历史账号；提交模型仅保留trim、去重后的非空字符串，供应商仅传bps_models、不发送bps_all_models。Cloud开启时仍优先并保留已保存BPS列表，关闭Cloud后按该列表恢复选路；具备原生授权的独立新请求可走Native，Excel-only不因此获得Native权限。权威为[2026-10-27执行合同](../CONTRACTS.md#bps-routing-20261027)第82行及 `WP-18-BPS-EXPLICIT-MODEL-SELECTION-20261027`；后于c2a10f2，不属于该维护输入已实现语义。模型选择全链、默认模板退休及供应商迁移按主代理交接已接入，且源码已停写，最终e5与作用域复用证据已交接，业务验收待执行，不作验收完成声明。
- 故障安全投影：生产consumer已按MAIN确认全部迁至 `readBPSRecoveryPresentation(account.excel_bps_403_recovery)`，旧bpsFailurePresentation及专属测试删除且无shim；不读取私有extra.failure。支持 history/reauthorization_required/paused/pending/failed/probing/unknown；缺失投影保持缺失，畸形或未来state为unknown，reason仅是展示分类，不推断授权。完整账号与lite列表均按正式DTO读取；真实开关开启或history不显示活动故障，仅pending/failed/probing接纳有效next_probe_at；last_result仅消费status（restored/failed/unknown）、reason与completed_at。严格时间parser拒绝非法实际日期与24:00；到期不代表恢复。MAIN报告220相关tests通过，未与旧6005或最终snapshot绑定。supplier安全投影仍未提供；按[故障分类合同](../CONTRACTS.md#bps-failure-recovery-20261027)保留权限边界。
- 冷却与恢复权限：页面区分BPS与Native限制，并展示可信的实际模型、接口或交集；未知编码只显示范围待确认，不把429等同账号关闭。成功测试触发的自动恢复保留BPS范围冷却；管理员明确覆盖按独立权限及真实端点语义核对，不能混写成所有恢复均保留或均清空。对应 FE-RES-BPS-PUBLIC-RECOVERY，尚未执行。
- 陈旧编辑原子边界：先以BPS已开启打开表单，后台停用后仅改名保存，旧草稿不得回写旧true重新开启。后端须提供原子更新或等效并发合同；缺少时 FE-RES-BPS-STALE-EDIT 标blocked，GET预检不能替代。普通已关闭账号编辑不产生false停止意图，明确停止才发送既有false并原子取消旧claim；旧probe不能覆盖停止结果，关闭/换目标/会话/撤权清除未提交意图。
- 异常/取消：不满足组合时禁用或清理相应草稿，不推断未知平台能力；字段权限不足时不发送受限字段。调度阈值、quota、fallback 等是配置输入，运行资格和实际扣费仍由服务端决定。Excel/BPS 授权及 Grok media eligibility 等独立接口不能误写成所有账号字段均同一事务。
- 权威/源码：[资源草稿与面板入口](../../../frontend/src/features/resources/panels.ts)、[凭据构造](../../../frontend/src/features/resources/model/credentialsBuilder.ts)、[编辑凭据投影](../../../frontend/src/features/resources/model/editCredentialPayload.ts)、[BPS 恢复](../../../frontend/src/features/resources/model/excelBPSRecovery.ts)、[能力与授权合同](../../../frontend/src/features/resources/api/accountCredentialTypes.ts)、[显式模型解析](../../../frontend/src/features/resources/model/excelBPSModels.ts)、[安全恢复模型](../../../frontend/src/features/resources/model/bpsRecoveryPresentation.ts)、[故障徽章](../../../frontend/src/features/resources/components/BPSFailureBadge.vue)、[运行态过滤](../../../frontend/src/features/resources/model/editOpenAITransportPayload.ts)。用例：`FE-PAGE-ADMIN-ACCOUNTS-ACTION-2`。
- 独立验收追踪：`FE-RES-BPS-EXPLICIT-MODELS`、`FE-RES-BPS-PUBLIC-RECOVERY`、`FE-RES-BPS-STALE-EDIT`，全部not_run；定义来自当前catalog，不计入历史6005测试。实际源码集合：[editModels](../../../frontend/src/features/resources/model/editModels.ts)、[editHydration](../../../frontend/src/features/resources/model/editHydration.ts)、[editOpenAITransportPayload](../../../frontend/src/features/resources/model/editOpenAITransportPayload.ts)、[AccountAuthorizations](../../../frontend/src/features/supply/components/AccountAuthorizations.vue)、[BPSFailureBadge](../../../frontend/src/features/resources/components/BPSFailureBadge.vue)、[AccountStatusIndicator](../../../frontend/src/features/resources/components/AccountStatusIndicator.vue)、[bpsRecoveryPresentation](../../../frontend/src/features/resources/model/bpsRecoveryPresentation.ts)、[accountRateLimitScopes](../../../frontend/src/features/resources/model/accountRateLimitScopes.ts)、[EditAccountModal](../../../frontend/src/features/resources/components/EditAccountModal.vue)、[editTransport](../../../frontend/src/features/resources/model/editTransport.ts)。

### FS-RES-005 账号状态、恢复及独立命令
- 前置/输入：按账号类型及权限显示命令；accounts.manage 控制可调度、恢复、quota 重置、隐私等，accounts.authorize 控制复制、重授权和 Spark shadow 创建；accounts.diagnostics 控制测试/诊断。OpenAI OAuth 非影子父账号才显示 shadow 创建，隐私操作限实际支持的非影子账号。
- 流程/结果：独立调用schedulable、recover-state、reset-quota、refresh、set-privacy、revert-proxy-fallback、shadow或删除端点。成功测试后的自动恢复与管理员明确恢复覆盖不是同一命令：前者保留BPS范围冷却，后者核对独立权限、当前目标及实际端点覆盖语义。不能仅由按钮显示推定已获准清除BPS；操作后重读权威状态，本地开关不保证可调度。
- 平台附属动作：OpenAI 配额查询/重置、referral 列表及发邀请；Claude reset credits 查询/兑换；CN 余额/配额探测；Ollama Cloud、OpenCode Go 用量刷新；telemetry JSON 导入/复制。邀请须新查询确认 should_show、剩余数>0、有效邮箱及所需显式确认，网络丢失显示发送结果未知；Claude 先查可兑换 credit 再确认消耗，单纯查询不扣次数。这些结果取服务端返回，不以页面计算替代。完整诊断过程归第四章。
- 账号用量配置：Ollama Cloud 独立会话输入需后端 encryption_key_configured，支持保存、确认删除、即时刷新及自动刷新开关；OpenCode Go 支持对应自动刷新/刷新。接口分别在 `/admin/accounts/:id/ollama-cloud-usage`、`/opencode-go-usage` 下，不把保存账号主表当作这些命令的提交；[Ollama 会话面板](../../../frontend/src/features/resources/components/OllamaCloudUsageSettings.vue)。
- 异常/取消：动作以 actor+目标绑定；同一待执行动作防重复，换身份后迟到结果不污染新行或新选择。受控外部账号不足时保留未验收，不把列表 fixture 当真实兑换/邀请成功。
- 权威/源码：[账号命令](../../../frontend/src/features/resources/api/accounts.ts)、[行菜单](../../../frontend/src/features/resources/components/AccountActionMenu.vue)、[OpenAI 邀请](../../../frontend/src/features/resources/components/OpenAIReferralCell.vue)、[Claude credits](../../../frontend/src/features/resources/components/ClaudeResetCreditsCell.vue)、[telemetry](../../../frontend/src/features/resources/components/AccountTelemetryPanel.vue)、[内部scope隔离](../../../frontend/src/features/resources/model/accountRateLimitScopes.ts)。用例：`FE-PAGE-ADMIN-ACCOUNTS-ACTION-2`、`FE-PAGE-ADMIN-ACCOUNTS-EXTERNAL-UPSTREAM`；这些是组合用例，未声称每个小动作已有独立 case。 本项BPS混合scope联调同时关联 `FE-PAGE-ADMIN-ACCOUNTS-ACTION-2`（仍not_run）。
- 独立验收追踪：`FE-RES-BPS-PUBLIC-RECOVERY`，全部not_run；定义来自当前catalog，不计入历史6005测试。实际源码集合：[BPSFailureBadge](../../../frontend/src/features/resources/components/BPSFailureBadge.vue)、[AccountStatusIndicator](../../../frontend/src/features/resources/components/AccountStatusIndicator.vue)、[bpsRecoveryPresentation](../../../frontend/src/features/resources/model/bpsRecoveryPresentation.ts)、[accountRateLimitScopes](../../../frontend/src/features/resources/model/accountRateLimitScopes.ts)。

### FS-RES-006 账号批量修改与批量命令
- 前置/输入：明确选定 ID 或筛选结果，并选择要应用的字段；无目标、无字段不提交。基础批改及参考账号高级编辑沿同一 bulk API；只修改勾选字段，平台/类型不适用组合受限，Cloud 开启前检查目标资格，BPS 恢复与 Header 等保持单账号校验。
- 流程/结果：POST `/admin/accounts/bulk-update`；批量删除、清错、刷新 token 分别使用 batch-delete/batch-clear-error/batch-refresh；可批量启停调度、探测成本，成本探测最多 20 个 ID。报告成功/失败/继承项，刷新失败可保留失败目标供操作者处理；不宣称批量事务全部回滚。
- 异常/取消：混合组 409 确认冻结原 IDs、writer、payload；关闭、参考目标变化或权限变化使旧确认失效。部分成功后只按返回结果处理，旧请求不能清掉后来新选择；成本同步冲突、长上下文父账号要求有明确错误。
- 权威/源码：[批量编辑](../../../frontend/src/features/resources/components/BulkEditAccountModal.vue)、[批量工具栏](../../../frontend/src/features/resources/components/AccountBulkActionsBar.vue)、[页面命令 owner](../../../frontend/src/app/compositions/resource-workspace/AccountsPage.vue)、[bulk API](../../../frontend/src/features/resources/api/accounts.ts)。用例：`FE-PAGE-ADMIN-ACCOUNTS-ACTION-1`、`FE-PAGE-ADMIN-ACCOUNTS-ACTION-2`。

### FS-RES-007 账号导入、导出和 CRS 同步
- 前置/输入：导入和 CRS 同步需 accounts.authorize，含秘密数据导出限超管并沿 step-up。本地导入支持多文件；逐文件接受缺省/空 type 或 sub2api-data/sub2api-bundle，version 缺省或 0/1/2，必须含 accounts/proxies 数组，解析错误标明文件。
- 流程/结果：合并后 POST `/admin/accounts/data`；GET 同路径按实际选择/筛选导出。bundle 可携带账号 credentials、独立 bps_credentials、组描述、代理关联；skipped_shadows 显示不持秘密影子账号的跳过数。导入报告 proxy_created/reused/failed、account_created/failed 和逐项错误，已有成功导入后关闭仍通知外层刷新。
- CRS：输入 base_url、username、password → `/sync/crs/preview` 查看并选账号 → `/sync/crs`，按返回 created/updated/skipped/failed 呈现；这不是任意站点通用迁移。真实 CRS 需要可访问测试实例。
- 异常/取消：坏文件不混入成功 payload；部分成功不显示全成功；导出取消 step-up 不下载秘密。导入/同步超时不能断言服务器未写入，先核对对象再重试。
- 权威/源码：[导入](../../../frontend/src/features/resources/components/AccountImportDataModal.vue)、[bundle DTO](../../../frontend/src/features/resources/api/accountImportTypes.ts)、[CRS](../../../frontend/src/features/resources/components/SyncFromCrsModal.vue)、[data/sync API](../../../frontend/src/features/resources/api/accounts.ts)。用例：`FE-PAGE-ADMIN-ACCOUNTS-ACTION-3`。

### FS-RES-008 错误透传、TLS 与诊断关联入口
- 前置/输入：工具菜单的错误透传规则/TLS profiles 限超管。错误规则有名称、优先级、平台/状态码/关键词条件、匹配与响应处理；名称及条件必填。TLS profile 有名称、GREASE、cipher suites、curves/point formats 等配置，支持粘贴 YAML 解析后编辑。
- 流程/结果：两弹窗都可新增、编辑、确认删除；错误规则另可启停。各自保存后重读；TLS 解析失败不提交。对话框关闭/换 scope 清理本地状态；错误不会被展示成成功规则。
- 关联入口：账号行测试、统计、计划测试、Codex 诊断、临时不可调度详情和成本探测，以及Cloud、用量设置，按真实权限显示。旧BPS defaults工具链按c2退休，不再作为当前功能要求；相关两条用例已从当前catalog删除，剩余实现收尾由Arendt交接。计划执行/诊断记录和外部结果详见第四章，不在这里新增诊断协议或后台作业。
- 权威/源码：`/admin/error-passthrough-rules[/:id]`、`/admin/tls-fingerprint-profiles[/:id]`；[错误规则](../../../frontend/src/features/resources/components/ErrorPassthroughRulesModal.vue)、[TLS](../../../frontend/src/features/resources/components/TLSFingerprintProfilesModal.vue)、[账号测试](../../../frontend/src/features/resources/components/AccountTestModal.vue)、[计划测试](../../../frontend/src/features/resources/components/ScheduledTestsPanel.vue)。用例：`FE-PAGE-ADMIN-ACCOUNTS-ACTION-3`、`FE-PAGE-ADMIN-ACCOUNTS-EXTERNAL-UPSTREAM`。

### FS-RES-009 代理查询、创建和编辑
- 前置/输入：`/admin/proxies` 按 search/protocol/status 筛选、排序分页；创建/编辑需 proxies.manage。名称与 host 不得空，端口 1–65535；输入协议、认证、到期时间、到期预警天数及 fallback_mode=none/proxy/direct，proxy 模式选择 backup_proxy_id。
- 流程/结果：GET/POST `/admin/proxies`、PUT `/admin/proxies/:id`；到期日期转换为 Unix 秒或 null，非 proxy fallback 清空 backup_proxy_id。编辑过期记录以 inactive 草稿呈现；未改密码省略 password，明确改为空才发送清空，避免脱敏响应擦掉原秘密。
- 异常/取消：失败保留编辑并提示服务端错误，取消不提交；删除独立确认。秘密不保存在普通列表/编辑回填中，前端 host 校验不替代服务端出口与地址校验。
- 权威/源码：[代理页面](../../../frontend/src/app/compositions/resource-workspace/ProxiesPage.vue)、[草稿投影](../../../frontend/src/app/compositions/resource-workspace/model/proxyDraft.ts)、[代理 API](../../../frontend/src/features/resources/api/proxies.ts)。用例：`FE-PAGE-ADMIN-PROXIES-ENTRY`、`FE-PAGE-ADMIN-PROXIES-REFRESH`、`FE-PAGE-ADMIN-PROXIES-BACK`、`FE-PAGE-ADMIN-PROXIES-ACTION-1`、`FE-PAGE-ADMIN-PROXIES-ACTION-2`。

### FS-RES-010 代理批量、导入导出和秘密复制
- 前置/输入：管理权限开放标准/批量新建、JSON 导入和删除；批量文本先解析显示有效/无效项。导出按钮限超管；URL/authority 复制先显式拉取秘密 export，地址形式只复制 host:port，不把脱敏占位符当密码。
- 流程/结果：POST `/admin/proxies/batch` 返回创建/跳过结果；batch-delete 返回各项成功/失败。GET/POST `/admin/proxies/data` 导出/导入；导入显示 created/reused/failed，失败项仍可检查。URL 中用户名/密码进行编码，右键菜单保留 URL/authority/address 实际格式。
- 异常/取消：坏 JSON 提示解析失败；部分导入后的关闭刷新外层；删除取消不发请求；秘密导出/复制 step-up 取消不当作成功，也不从缓存伪造凭据。批量选择范围由页面选择决定，不承诺全库操作。
- 权威/源码：[批量输入](../../../frontend/src/features/resources/components/ProxyBatchInput.vue)、[导入](../../../frontend/src/features/resources/components/ProxyImportDataModal.vue)、[页面复制/导出](../../../frontend/src/app/compositions/resource-workspace/ProxiesPage.vue)、[代理 API](../../../frontend/src/features/resources/api/proxies.ts)。用例：`FE-PAGE-ADMIN-PROXIES-ACTION-1`、`FE-PAGE-ADMIN-PROXIES-ACTION-2`。

### FS-RES-011 代理关联账号、连通性与质量
- 前置/输入：关联账号需 accounts.read；测试/质量操作按 proxies.manage 显示，可单条或批量，批量无选择时的实际范围按页面当前筛选取得。准备可控本地代理；Geo/公网出口和真实质量测试另需外部前置。
- 流程/结果：GET `/admin/proxies/:id/accounts` 展示关联摘要；POST `/test` 返回 success、latency_ms 与出口地理信息；POST `/quality-check` 展示真实质量报告。刷新不会被描述为修复代理、解除账号限制或保证出口可用。
- 异常/取消：单项失败与批量部分失败保留区别；质量报告只展示当前目标结果；退出/换 scope 取消探测并拒绝旧结果。外网失败不能被“本机可连”覆盖。
- 权威/源码：[代理页](../../../frontend/src/app/compositions/resource-workspace/ProxiesPage.vue)、[质量报告](../../../frontend/src/features/resources/components/ProxyQualityReport.vue)、[测试 API](../../../frontend/src/features/resources/api/proxies.ts)。用例：`FE-PAGE-ADMIN-PROXIES-ACTION-3`、`FE-PAGE-ADMIN-PROXIES-EXTERNAL-NETWORK`。

## 分组、准入与风控

### FS-ACCESS-001 分组查询、新建、复制和删除
- 前置/输入：`/admin/groups` 获准管理员；平台、状态、独享和搜索筛选，排序分页；管理动作需 groups.manage。名称为必填，可设置 description、多语言 i18n、platform；simple mode 只开放其现有精简字段。
- 流程/结果：列表 GET `/admin/groups`；新建/编辑进入组合编辑器。复制 POST `/admin/groups/:id/duplicate` 由服务端复制完整对象，不以列表摘要拼新 Group；删除 DELETE `/admin/groups/:id` 前确认，成功后刷新。复制按 actor+source 保存幂等意图，未知结果重试保留原键。
- 异常/取消：查询失败不等于没有组；删除取消不写；复制超时不改成创建另一个组。私有数据不跨 scope 复用，重复按钮不能释放另一条新命令的 pending 状态。
- 权威/源码：[GroupsPage](../../../frontend/src/app/compositions/group-editor/GroupsPage.vue)、[groups API](../../../frontend/src/features/access-policy/api/groups.ts)、[身份草稿](../../../frontend/src/features/access-policy/model/groupIdentityDraft.ts)。用例：`FE-PAGE-ADMIN-GROUPS-ENTRY`、`FE-PAGE-ADMIN-GROUPS-REFRESH`、`FE-PAGE-ADMIN-GROUPS-BACK`、`FE-PAGE-ADMIN-GROUPS-ACTION-1`。

### FS-ACCESS-002 Group 跨域草稿及原子保存
- 前置/输入：access-policy 拥有身份、准入、路由/清单；pricing 拥有价格与利润草稿；subscriptions 拥有订阅类型及周期额度。只读面板输入产生本域 patch，不直接改原 DTO；app 持有最终草稿、跨域联动和一次保存命令。
- 流程/结果：校验名称、推理策略、利润参数、价格倍率、已启用 allowlist 的非空选择和 OpenAI manifest 的账号选择，然后合并三域 payload，**一次** POST `/admin/groups` 或 PUT `/admin/groups/:id`。受限管理员再经 `editableAdminFields('group.create'/'group.update', payload)` 白名单；无权字段不能因面板存在就提交。此保存不拆成准入/价格/订阅三次 HTTP。
- 字段合同：simple create 只发 name/description/i18n/platform，simple update 只发前三项；普通 create 不发 status/default_mapped_model，manifest 固定禁用；更新未选 fallback 以 0 表示清除。额度、媒体价格的 null/0/-1 语义分别见 FS-SUB-001、FS-PRICE-006；不增加通用 revision，也不把 Settings 的 CAS 套到 Group。
- 联动/异常：订阅选择协调独享/fallback 与峰时价格，媒体准入协调批图及折扣默认值，换平台调用各域重置投影；这些跨域联系仍在 app。pending 期间禁用关闭/重复保存；换目标、scope、管理/字段权限使草稿失效；保存错误保留当前草稿，晚到结果不能关闭新弹窗。
- 权威/源码：[组合编辑器](../../../frontend/src/app/compositions/group-editor/GroupEditorDialog.vue)、[原子组合](../../../frontend/src/app/compositions/group-editor/model/groupDraft.ts)、[准入公开 facet](../../../frontend/src/features/access-policy/groupEditor.ts)、[价格 facet](../../../frontend/src/features/pricing/groupEditor.ts)、[订阅 facet](../../../frontend/src/features/subscriptions/groupEditor.ts)、[Group DTO](../../../frontend/src/features/access-policy/api/groupTypes.ts)。用例：`FE-PAGE-ADMIN-GROUPS-ACTION-1`、`FE-PAGE-ADMIN-GROUPS-ACTION-2`。

### FS-ACCESS-003 准入、资源范围、路由与模型清单
- 前置/输入：按平台和字段权限编辑独享、状态、RPM、复制账号关联、Claude Code only、fallback/invalid-request fallback、OAuth/privacy 要求、supported_model_scopes、MCP XML、Messages dispatch、推理 effort 映射/上限及超限处理。模型 routing 使用 pattern→account IDs；有效规则需非空 pattern 和正 ID。
- 流程/结果：allowlist 从 `/admin/groups/:id/model-allowlist-candidates` 读候选，新建使用 id=0 与 platform；可选择/自定义现有允许模型，启用但空集合阻止保存。OpenAI 编辑可配置 Codex models manifest 指定账号及 scheduler fallback；新建仍按既有 disabled 合同。媒体准入与价格分开，不从无报价反推无权限。
- Live：开启前 GET `/admin/groups/live-capability`。明确 supported=true 可开；false 展示原因并允许操作者确认；缺失/请求失败显示错误，不假造 supported。确认设置只代表保存配置，不能证明运行环境已支持 Live。
- 异常/取消：候选与账号搜索绑定当前组/平台/scope；旧请求不回写新草稿。平台切换仅按既有规则清理不适用字段；未知 capability、模型或配置不能扩展成前端调度权威。
- 权威/源码：[准入面板](../../../frontend/src/features/access-policy/panels/GroupAdmissionFields.vue)、[routing](../../../frontend/src/features/access-policy/panels/GroupRoutingFields.vue)、[allowlist](../../../frontend/src/features/access-policy/panels/GroupModelAllowlistPanel.vue)、[manifest](../../../frontend/src/features/access-policy/components/CodexManifestAccountsField.vue)、[Live](../../../frontend/src/features/access-policy/components/GroupLiveToggle.vue)、[payload](../../../frontend/src/features/access-policy/model/groupPolicyDraft.ts)。用例：`FE-PAGE-ADMIN-GROUPS-ACTION-1`、`FE-PAGE-ADMIN-GROUPS-ACTION-2`。

### FS-ACCESS-004 复合路由、用户覆盖及排序
- 前置/输入：复合组路由弹窗编辑 public_model、match_type、target_platform、upstream_model、endpoint、priority、enabled、notes；public_model 不得空，无有效 priority 使用既有 100 默认。预览输入 model/endpoint 等请求条件，结果由服务端决策返回。
- 流程/结果：`/admin/groups/:id/composite-routes[/:routeId]` 独立增改删，`/preview` 读取匹配决定；不是 GroupEditor 三域保存的一部分。组排序拖动后 PUT `/admin/groups/sort-order` 提交 id/sort_order。
- 用户覆盖：倍率弹窗需 billing.rates.update，正数倍率可新增/改值/按因子批调/移除/清空草稿后保存；RPM 弹窗需 groups.manage，输入整数且 ≥0。倍率“清空”只改本地、等待保存；**RPM“清空”立即 DELETE rpm-overrides 并更新基线，没有额外确认**，后续取消不能恢复已清空的服务端记录。两接口各自维护列，不互相覆盖。用户搜索真实装配为 `usersAPI.list(1,10,{search:keyword},{signal})`，不宣称全量或历史用户 search 接口。
- 异常/取消：未加载当前组基线不能保存；本地取消恢复草稿，清空/删除按对应弹窗确认与提交方式执行。复合路由失败留错误，预览不产生真实模型调用；换组或权限后拒绝旧保存/搜索结果。
- 权威/源码：[复合路由](../../../frontend/src/features/access-policy/components/CompositeRoutesDialog.vue)、[倍率](../../../frontend/src/features/access-policy/components/GroupRateMultipliersModal.vue)、[RPM](../../../frontend/src/features/access-policy/components/GroupRPMOverridesModal.vue)、[排序](../../../frontend/src/features/access-policy/components/GroupSortDialog.vue)、[users 绑定](../../../frontend/src/app/compositions/accessPolicyServices.ts)、[API](../../../frontend/src/features/access-policy/api/groups.ts)。用例：`FE-PAGE-ADMIN-GROUPS-ACTION-2`。

### FS-ACCESS-005 风控配置、规则和运行状态
- 前置/输入：`/admin/risk-control` 按页面权限进入；已加载 risk_control_enabled 明确 false 时转可访问的设置/仪表板。**配置读取/编辑入口与保存限 super_admin**。引擎 openai/typesafe；mode=off/observe/pre_block；可设置 URL/model/proxy、超时/重试/抽样、全组或组集合、队列/worker、拦截响应、通知/自动封禁/窗口与留存，以及关键词/模型筛选、类别阈值、distillation、regex、refusal/device guards 和 fail_open/fail_closed。
- 密钥/校验：密钥按行去重，append/replace、按 hash 删除已存密钥或 clear；replace 不能无输入。模型 include/exclude 不能空；阈值表单百分比转换为 API 比例；proxy_id=0 清除代理，非命中留存约束 1–3 天。切引擎保留各引擎独立草稿，不拿掩码当原密钥。
- 流程/结果：GET/PUT `/admin/risk-control/config`，GET `/status`；普通保存只保存配置。“保存并启用”先保存配置，再通过 app 注入的全局设置命令启用；第二步失败明确提示已保存但未启用并重读状态，**不是跨端点原子事务**。
- 外部测试/异常：可测试输入或已存密钥，附文本及最多 1 张、≤8 MiB 图片，查看逐键 unknown/ok/error/frozen、延迟/HTTP 与审核结果；需真实测试审核服务。取消设置不提交草稿；网络失败不得声称审核通过或全局已启用。
- 权威/源码：[风控页](../../../frontend/src/features/access-policy/components/RiskControlPage.vue)、[配置模型](../../../frontend/src/features/access-policy/model/riskConfigDraft.ts)、[DTO/API](../../../frontend/src/features/access-policy/api/riskControl.ts)、[全局设置端口装配](../../../frontend/src/app/compositions/accessPolicyServices.ts)。用例：`FE-PAGE-ADMIN-RISK-CONTROL-ENTRY`、`FE-PAGE-ADMIN-RISK-CONTROL-REFRESH`、`FE-PAGE-ADMIN-RISK-CONTROL-BACK`、`FE-PAGE-ADMIN-RISK-CONTROL-ACTION-2`、`FE-PAGE-ADMIN-RISK-CONTROL-EXTERNAL-UPSTREAM`。

### FS-ACCESS-006 风控日志、详情、解封与 hash 清理
- 前置/输入：按页面可见权限读日志，筛选 result/group_id/endpoint/search/from/to、分页；时间转换为 ISO。解封按钮针对日志中 auto_banned 且 user_status=disabled 的用户，是否准许执行以服务端为准，不因按钮可见推导任意用户管理权。
- 流程/结果：GET `/admin/risk-control/logs` 展示命中、分类/分数、输入片段/错误和详情；详情含实际 engine_meta/rules_version/skipped_images 等返回字段。POST `/users/:id/unban` 后更新同用户日志状态；hash 单条删除报告 deleted/not-found，全清前确认并显示删除数量，随后重读运行状态。
- 异常/取消：hash 输入 trim 后须 64 位十六进制；取消全清不发 DELETE，失败不伪造清零。日志详情不额外推断请求已拦截或已扣费；scope 改变后旧日志和命令结果不能写回。
- 权威/源码：[运行/详情交互](../../../frontend/src/features/access-policy/components/RiskControlPage.vue)、[运行展示](../../../frontend/src/features/access-policy/components/RiskRuntimePanel.vue)、[日志/hash API](../../../frontend/src/features/access-policy/api/riskControl.ts)。用例：`FE-PAGE-ADMIN-RISK-CONTROL-ACTION-1`、`FE-PAGE-ADMIN-RISK-CONTROL-ACTION-3`。

## 目录与报价展示

### FS-CAT-001 模型广场入口与目录过滤
- 查询前置：设置未知/关闭/需登录/可用是独立状态；只有设置明确允许且身份满足才请求，scope/配置变化丢弃旧结果。读取被 401/403/404 拒绝后的重试先刷新公共设置；不可用提示不伪装为空目录，也不自动放宽权限。
- 前置/输入：`/model-plaza` 是公共入口，但已加载设置明确 model_plaza_enabled=false 时转首页/仪表板；require_auth=true 的访客转登录并保留 redirect；backend mode 仍限制非管理员。已登录 `embedded=1` 使用应用 user shell，独立页有导航栏。
- 流程/结果：GET `/model-plaza` 返回目录、说明及分组；按平台、组、有效倍率、模型关键字联动过滤，清搜索恢复可见项，不可组合的筛选项禁用；倍率优先 user_rate_multiplier 再 group rate。说明经 Markdown 安全呈现，空数据、无匹配和加载失败有不同提示。
- 异常/取消：公共设置尚未知不等于明确禁用；服务端关闭可返回 404。登录/退出或 scope 变化清旧目录并取消请求，销毁后的 retained reload 不再发请求；不能把上一用户的专属倍率用于匿名展示。
- 权威/源码：[路由](../../../frontend/src/features/catalog/routes.ts)、[查询](../../../frontend/src/features/catalog/queries/modelPlaza.ts)、[过滤内容](../../../frontend/src/features/catalog/components/ModelPlazaContent.vue)、[API](../../../frontend/src/features/catalog/api/modelPlaza.ts)。用例：`FE-PAGE-MODEL-PLAZA-ENTRY`、`FE-PAGE-MODEL-PLAZA-REFRESH`、`FE-PAGE-MODEL-PLAZA-BACK`、`FE-PAGE-MODEL-PLAZA-ACTION-1`、`FE-PAGE-MODEL-PLAZA-ACTION-2`。

### FS-CAT-002 模型能力、报价与分组展示
- 前置/输入：读取服务器返回的 group/models/pricing，不允许读者编辑价格或凭空添加能力。组区块展示名称、描述、平台、独享/订阅标记、组/个人倍率、峰时说明；模型表与模型 chip 展示已有模型价格事实。
- 流程/结果：对照展示实际配置报价与数据源中的官方参考价；token 使用每百万单位，非 token 区分按次/图片/视频；支持区间、长上下文 basis、fast/flex、推理 effort、cache write/read、时段、图片/视频独立倍率。effort 顺序和计费枚举由 pricing 经 app 的 readonly projection 注入，目录不重复定义价格 taxonomy。
- 异常/取消：缺价显示未知/未配置而非免费，显式 0 保留；区间缺失字段沿既有报价 fallback，不合成权威账单。展示倍率与时段注释不是实际费用或最终准入证明；模型未在目录中不能推导所有端点都不支持。
- 权威/源码：[DTO](../../../frontend/src/features/catalog/api/types.ts)、[组区块](../../../frontend/src/features/catalog/components/PlazaGroupSection.vue)、[报价表](../../../frontend/src/features/catalog/components/PlazaModelPricingTable.vue)、[模型浮层](../../../frontend/src/features/catalog/components/SupportedModelChip.vue)、[区间展示](../../../frontend/src/features/catalog/model/intervalQuote.ts)、[只读 ports](../../../frontend/src/features/catalog/ports.ts)。用例：`FE-PAGE-MODEL-PLAZA-ACTION-1`、`FE-PAGE-AVAILABLE-CHANNELS-ACTION-1`；文档中嵌入的同一组面板由第一章描述容器。

### FS-CAT-003 用户可用渠道
- 前置/输入：主站登录后 `/available-channels`；available_channels_enabled 是菜单 opt-in，simple mode 隐藏该菜单。当前 route 没有单独 requiresAvailableChannels guard，不能把“菜单隐藏”写成前端禁止直达；直达结果仍取接口。
- 流程/结果：GET `/channels/available` 与 app 注入的个人组倍率查询并行；显示渠道名称/描述、平台区块、可见组和模型报价。搜索先匹配渠道名称/描述，否则保留匹配平台名、组名或模型名的平台区块；当前页面是搜索过滤，**没有独立平台下拉**。手动刷新重新查询。
- 异常/取消：列表请求失败提示错误；可选个人倍率查询失败保留默认组倍率 fallback，不宣称已取得个性化价格。空渠道、无模型、无价格分别显示；scope 改变清列表/倍率/搜索并拒绝迟到结果。
- 权威/源码：[页面](../../../frontend/src/features/catalog/pages/AvailableChannelsPage.vue)、[搜索规则](../../../frontend/src/features/catalog/model/availableChannels.ts)、[查询/fallback](../../../frontend/src/features/catalog/queries/availableChannels.ts)、[API](../../../frontend/src/features/catalog/api/availableChannels.ts)。用例：`FE-PAGE-AVAILABLE-CHANNELS-ENTRY`、`FE-PAGE-AVAILABLE-CHANNELS-REFRESH`、`FE-PAGE-AVAILABLE-CHANNELS-BACK`、`FE-PAGE-AVAILABLE-CHANNELS-ACTION-1`、`FE-PAGE-AVAILABLE-CHANNELS-ACTION-2`。

### FS-CAT-004 平台选项、映射候选与清单选择
- 前置/输入：资源/Group/渠道表单复用 catalog 公开平台目录、平台选项及模型映射预设；当前平台枚举和顺序从 owner 导出，经 app 组合，不向 shared 塞业务常量。搜索到的未知平台名称保留显示，不猜凭据方式或工具能力。
- 流程/结果：编辑器的模型候选、上游同步与 Group allowlist 仍取各自权威 API；选择账号组成 Codex manifest 是 FS-ACCESS-003 的准入配置。API Key 使用指南的 manifest 获取/下载入口由第一章 FS-KEY-006 负责；catalog helper 只按配置的客户端 Base URL 请求清单、校验 models 数组并生成 JSON 内容。
- 异常/取消：请求错误/非清单 JSON 不作为有效下载；未知目录项不扩展能力。用户 API Key 发往所配置客户端 endpoint，不能将其混为管理 API 的会话令牌。
- 权威/源码：[平台目录](../../../frontend/src/features/catalog/model/platformCatalog.ts)、[平台选项](../../../frontend/src/features/catalog/model/platformOptions.ts)、[映射预设](../../../frontend/src/features/catalog/model/modelMappingPresets.ts)、[manifest helper](../../../frontend/src/features/catalog/codexManifest.ts)、[第一章](01-PUBLIC-IDENTITY-KEYS.md)。用例：`FE-PAGE-ADMIN-ACCOUNTS-ACTION-2`、`FE-PAGE-ADMIN-GROUPS-ACTION-1`、`FE-PAGE-ADMIN-CHANNELS-PRICING-ACTION-1`。

## 渠道与 Group 定价

### FS-PRICE-001 渠道查询、状态和删除
- 前置/输入：`/admin/channels/pricing` 为定价入口，`/admin/channels` 重定向至此；`/admin/channels/monitor` 是第四章监控页面。按名称/状态搜索、排序分页和刷新；新建/编辑/删除需 channels.manage，状态切换还需 channel.update 的 status 字段许可。
- 流程/结果：GET `/admin/channels`；状态 PUT `/admin/channels/:id`，枚举 active/disabled；删除 DELETE 同路径，确认前冻结目标，成功后重新读列表。不存在“删除渠道自动退订/退款”的前端流程。
- 异常/取消：每个目标独立 pending，互不取消其他行的命令；同目标重复点击受阻，删除 pending 不可换确认目标。scope 变化拒绝旧结果，失败不伪造状态成功，确认取消不写。
- 权威/源码：[页面](../../../frontend/src/features/pricing/pages/ChannelsPage.vue)、[API](../../../frontend/src/features/pricing/api/channels.ts)、[路由](../../../frontend/src/features/pricing/routes.ts)。用例：`FE-PAGE-ADMIN-CHANNELS-PRICING-ENTRY`、`FE-PAGE-ADMIN-CHANNELS-PRICING-REFRESH`、`FE-PAGE-ADMIN-CHANNELS-PRICING-BACK`、`FE-PAGE-ADMIN-CHANNELS-PRICING-ACTION-2`、`FE-REDIRECT-ADMIN-CHANNELS`。

### FS-PRICE-002 渠道基本信息、组绑定和模型映射
- 前置/输入：名称非空；描述、restrict_models、billing_model_source、平台 section、group_ids、model_mapping、features_config。计价模型来源保留 requested/upstream/channel_mapped/response_model；仅启用的平台参与 payload，每个启用平台必须绑定组。
- 流程/结果：加载组及完整渠道冲突信息；组已绑定别的渠道时提示/限制，composite 组按既有平台投影展示。每个平台可增删映射与定价行；定价模型列表不能为空，模型/映射源的同名及尾部 `*` 前缀重叠不允许。模型标签支持 Enter/Tab、删除、粘贴逗号/分号/换行分隔文本及去重。
- 平台特性：实际平台下显示 WebSearch emulation、OpenAI image bridge、Anthropic Bedrock CC compatibility；app 传入事实，保存到原 features_config。POST/PUT 渠道前经过 channel.create/update 字段白名单；不因隐藏成本面板自动获得写成本权限。
- 异常/取消：打开前查询失败不冒充完整冲突名单；校验失败留草稿并定位平台。保存 pending 禁止关闭/重入；目标、scope 或 channels.manage 改变后整个编辑器失效；失败保持当前草稿。
- 权威/源码：[渠道编辑器](../../../frontend/src/features/pricing/components/ChannelEditorDialog.vue)、[payload 转换](../../../frontend/src/features/pricing/model/channelDraft.ts)、[标签输入](../../../frontend/src/features/pricing/components/ModelTagInput.vue)、[DTO](../../../frontend/src/features/pricing/api/types.ts)。用例：`FE-PAGE-ADMIN-CHANNELS-PRICING-ACTION-1`。

### FS-PRICE-003 模型单价、区间、推理和时段价格
- 前置/输入：每条模型定价选择现有 billing_mode=token/per_request/image/video；输入 token 输入/输出、缓存写入/1h/读取、图像 token、per_request_price、fast/flex、reasoning_effort_multipliers、intervals 与 time_pricing。界面 $/MTok 与 API per-token 只做单位投影；空值保留 null，0 不作空值。
- 校验：per_request/image 没有区间时必须填按次价；fast/flex 与非空推理倍率须有限正数，effort 限 none/minimal/low/medium/high/xhigh/max。区间 min≥0，非空 max>min 且 >0，价格不得负；token 区间不重叠且无限上界在末尾，非 token 档位不套 token 重叠规则。
- 时段流程：增删时段，指定有效 IANA timezone、weekdays_only、HH:mm:ss 起止和最多两位小数正倍率；旧 HH:mm 回填秒。结束 00:00:00 表示日末，起止不得相等/倒置/重叠；无时段发送 null。校验通过随渠道一次保存，不在浏览器定时改服务端价格。
- 异常/取消：默认价格读取失败留明确错误，不补“免费”；输入错误定位对应模型/平台；取消不保存。报价预览不是完整计费引擎。
- 权威/源码：[定价卡](../../../frontend/src/features/pricing/components/PricingEntryCard.vue)、[时段](../../../frontend/src/features/pricing/components/TimePricingSection.vue)、[表单校验/转换](../../../frontend/src/features/pricing/model/pricingForm.ts)、[枚举](../../../frontend/src/features/pricing/model/channelConstants.ts)。用例：`FE-PAGE-ADMIN-CHANNELS-PRICING-ACTION-1`。

### FS-PRICE-004 成本统计规则与模型同步
- 前置/输入：billing.cost.read 才显示独立账号统计定价面板，写还需 account_stats_pricing_rules 等字段许可。规则有名称、所选组/账号和规则定价，支持搜索账号、添加/移除规则及价格行；apply_pricing_to_account_stats 是独立字段。
- 流程/结果：规则随渠道保存；账号搜索通过 app 的资源 port，不从 pricing 直接调用资源实现。模型默认价 GET `/admin/channels/model-pricing?model=…`；支持平台的“同步模型” GET `/admin/channels/pricing/sync-models?platform=…`，把返回新模型加入草稿，再由保存落库，不自动升级依赖或修改资源账号。
- 异常/取消：无新增模型有单独提示；同步/查询失败留旧草稿，不宣称价格源已更新。独立成本规则不会被描述为用户真实扣费规则；上游价格目录可用性仍需执行验证。
- 权威/源码：[统计规则](../../../frontend/src/features/pricing/components/AccountStatsPricingRulesPanel.vue)、[编辑同步](../../../frontend/src/features/pricing/components/ChannelEditorDialog.vue)、[查询 API](../../../frontend/src/features/pricing/api/channels.ts)。用例：`FE-PAGE-ADMIN-CHANNELS-PRICING-ACTION-1`。

### FS-PRICE-005 Group 倍率、峰时及利润配置
- 前置/输入：Group 定价面板按字段许可编辑 rate_multiplier、峰时开关/起止/倍率、free_openai_fast、模型定价、长上下文及利润控制；force_openai_fast 由准入面板持有。OpenAI 另有 web_search_price_per_call；Grok 有 search_price_per_1k、audio_realtime_price_per_min、audio_tts_price_per_million_chars、audio_stt_price_per_hour，分别按次/千次/分钟/百万字符/小时显示，不混用 token 单位。峰时说明使用服务端时区；利润仅既有支持平台且启用时校验，不推导新平台支持。
- 流程/结果：利润 margin/buffer 以百分比输入，投影成原四位小数比例，各自 ≥0 且 <1、总和 <1，按**转换后**边界验证。Group 模型定价沿旧路径：token intervals 序列化为空，time_pricing 为 null，不能套用渠道编辑器全部能力。价格 payload 交 FS-ACCESS-002 一次保存。
- 异常/取消：利润边界错误阻止提交；订阅/平台切换触发既有重置，不从本地配置计算最终账号资格。字段未授权不提交；未知失败保留草稿和目标。
- 权威/源码：[价格草稿](../../../frontend/src/features/pricing/model/groupPricingDraft.ts)、[利润转换](../../../frontend/src/features/pricing/model/groupsProfitControl.ts)、[Group 价格面板](../../../frontend/src/features/pricing/panels/GroupPricingPanel.vue)、[峰时面板](../../../frontend/src/features/pricing/panels/GroupPeakPricingPanel.vue)。用例：`FE-PAGE-ADMIN-GROUPS-ACTION-1`、`FE-PAGE-ADMIN-GROUPS-ACTION-2`。

### FS-PRICE-006 Group 图像、视频和批图价格
- 按张计费：`image_count_billing` 由 pricing 拥有，默认 false，仅 OpenAI/Composite 且 allow_image_generation 时可设；关闭图片/切换不适用平台清除，更新发送明确 false，经字段许可后纳入一次 Group 命令。此字段控制专用图片计费，不能据 UI 开关改写混合 Chat/Responses/WS 的 token 账务合同。
- 前置/输入：图片单价 1K/2K/4K 与独立倍率按实际支持平台显示；视频面板为 Grok 的 480p/720p/1080p 每秒价、family/resolution 价格及独立倍率。媒体开关由 access-policy 提供槽位，价格面板不持有准入逻辑。
- 流程/结果：create 空媒体价发送 null；update 空价以 **-1 清除哨兵**发送，显式 0 保留。媒体/峰时倍率空或非法值沿现有默认 1，显式 0 保留。视频 family/resolution trim+小写，保留既有未知 family，有限非负价可序列化；create 空 map 省略，update 空 map 用于清除。
- 批图/异常：仅 Gemini 且图像准入和批图准入同时开启时有效；关闭时 discount/hold 重置为 0.5/0.6。面板 placeholder 是当前默认预览提示，不宣称永远等于官方实时价；保存仍走一个 Group 命令。任务创建/扣额/取消详见第四章批图工作台。
- 权威/源码：[媒体面板](../../../frontend/src/features/pricing/panels/GroupMediaPricingPanel.vue)、[payload](../../../frontend/src/features/pricing/model/groupPricingDraft.ts)、[视频 family](../../../frontend/src/features/pricing/model/groupsVideoModelPricing.ts)、[媒体 DTO](../../../frontend/src/features/pricing/api/mediaPricingTypes.ts)。用例：`FE-PAGE-ADMIN-GROUPS-ACTION-1`、`FE-PAGE-ADMIN-GROUPS-ACTION-2`。

## 订阅策略、授予和额度

### FS-SUB-001 Group 订阅策略及额度
- 前置/输入：Group 编辑器中选择 subscription_type=standard/subscription，配置 daily_limit_usd/weekly_limit_usd/monthly_limit_usd。此领域草稿仅拥有这四个字段；独享、fallback、价格、峰时不混入订阅内部草稿。
- 流程/结果：有效且大于 0 的有限额度写入原单位 USD；空、null、0、负值或非法值投影为 null（未设置额度上限），不是扣费 0。策略随 FS-ACCESS-002 原子 Group 命令保存；它不会直接给用户创建订阅，也不创建售卖套餐或收款订单。
- 异常/取消：策略切换只经 app 协调其他领域既有联动，取消丢弃草稿；字段无权不提交。日/周/月额度消耗、窗口和有效性事实从订阅 API 读取，不由表单运行调度/扣款。
- 权威/源码：[订阅策略面板](../../../frontend/src/features/subscriptions/panels/GroupSubscriptionPolicyPanel.vue)、[草稿与 payload](../../../frontend/src/features/subscriptions/model/groupSubscriptionDraft.ts)、[公开 facet](../../../frontend/src/features/subscriptions/groupEditor.ts)。用例：`FE-PAGE-ADMIN-GROUPS-ACTION-1`。

### FS-SUB-002 我的订阅、额度与续费入口
- 前置/输入：主站已登录访问 `/subscriptions`；已加载设置 subscription_enabled 明确 false 时转可访问仪表板，simple mode 同样限制；未知设置不当 false。页面读自身订阅，没有按任意 user_id 查询的输入。
- 流程/结果：GET `/subscriptions` 展示实际 active/expired/revoked 等状态、组/平台/描述、倍率/峰时、到期与日/周/月用量；空到期显示无到期。缺失用量显示“未知”，有额度才显示对应进度；≤24 小时订阅的日额度提示有效期结束，不冒称另有完整日重置窗口。
- 导航/异常：active 订阅续费跳 `/purchase?tab=subscription&group={group_id}`。没有本页付款/自动续费/退订或 Key 管理按钮；整页刷新重新读取，当前模板没有专门手动刷新按钮。失败提示不能证明无订阅；换 scope 清旧数据并丢弃迟到结果。
- 权威/源码：[用户页](../../../frontend/src/features/subscriptions/pages/UserSubscriptionsPage.vue)、[日期/额度展示](../../../frontend/src/features/subscriptions/model/subscriptionQuota.ts)、[user API](../../../frontend/src/features/subscriptions/api/user.ts)、[导航装配](../../../frontend/src/app/compositions/catalogServices.ts)。用例：`FE-PAGE-SUBSCRIPTIONS-ENTRY`、`FE-PAGE-SUBSCRIPTIONS-REFRESH`、`FE-PAGE-SUBSCRIPTIONS-BACK`、`FE-PAGE-SUBSCRIPTIONS-ACTION-1`、`FE-PAGE-SUBSCRIPTIONS-ACTION-2`。

### FS-SUB-003 管理订阅查询和用户选择
- 前置/输入：`/admin/subscriptions` 获准管理员，simple mode 禁止；菜单随 subscription 开关，当前管理 route 没有 requiresSubscription，不能从菜单隐藏推断相同直达 guard。按 user/group/platform/status、排序分页；界面状态筛选是 active/expired/revoked，不能因 API 类型还支持 suspended 就声称已有该选项。
- 流程/结果：GET `/admin/subscriptions` 展示用户、组、状态、期限、额度；支持列显示偏好、邮箱/用户名显示方式、全选本页及用户用量链接 `/admin/usage?user_id=…`，指南弹窗可跳分组配置。查询筛选用户通过历史用量搜索包含历史对象；分配用的用户选择另走当前用户列表。
- 异常/取消：筛选/排序/换页清选择，批操作目标必须仍在当前可见结果；换 actor/scope 清筛选用户和弹窗。读取列表不要求 subscriptions.manage，但所有写命令单独验该权限；读取错误不编造额度。
- 权威/源码：[管理页](../../../frontend/src/features/subscriptions/pages/AdminSubscriptionsPage.vue)、[指南](../../../frontend/src/features/subscriptions/components/SubscriptionGuideDialog.vue)、[directory 绑定](../../../frontend/src/app/compositions/catalogServices.ts)、[admin API](../../../frontend/src/features/subscriptions/api/admin.ts)。用例：`FE-PAGE-ADMIN-SUBSCRIPTIONS-ENTRY`、`FE-PAGE-ADMIN-SUBSCRIPTIONS-REFRESH`、`FE-PAGE-ADMIN-SUBSCRIPTIONS-BACK`、`FE-PAGE-ADMIN-SUBSCRIPTIONS-ACTION-1`。

### FS-SUB-004 单人/多人分配订阅
- 前置/输入：subscriptions.manage；选择当前用户和 active 且 subscription 类型的组；有效天数默认 30，必须整数 1–36500；多人模式去重、最多 100 用户。当前用户搜索实参为 `usersAPI.list(1,30,{search,sort_by:'email',sort_order:'asc'},{signal})`，不同于管理列表的历史搜索。
- 流程/结果：单人 POST `/admin/subscriptions/assign`；多人 POST `/bulk-assign`，提交 user_id(s)、group_id、validity_days。多人结果按 created/reused/failed 计数及实际返回订阅处理，移除成功用户、保留失败用户，避免把部分成功当全失败再次授予。
- 异常/取消：缺用户/组或天数错误阻止提交；pending 防重复，scope/权限变化关闭旧命令上下文；失败留错误和可处理用户。多人分配与下一节 bulk-action 是不同端点，不声称前者也有后者的 Idempotency-Key 恢复协议。
- 权威/源码：[分配对话框](../../../frontend/src/features/subscriptions/components/AssignSubscriptionDialog.vue)、[分配 API](../../../frontend/src/features/subscriptions/api/admin.ts)、[app directory](../../../frontend/src/app/compositions/catalogServices.ts)。用例：`FE-PAGE-ADMIN-SUBSCRIPTIONS-ACTION-2`。

### FS-SUB-005 延期、重置、撤销与恢复
- 前置/输入：subscriptions.manage；active/expired 可调有效期，active 可重置/撤销，revoked 可恢复。调整 days 可缩短或延长；已有 expires_at 时前端检查调整后仍在未来，最终有效期由服务端决定。
- 流程/结果：分别 POST `/admin/subscriptions/:id/extend`、`/reset-quota`、`/revoke`、`/restore`；重置、撤销、恢复各自有确认。**单条重置同时发送 daily/weekly/monthly=true**，不是自选周期；成功后关闭相应弹窗并重读列表。
- 异常/取消：取消确认不发送；失败保留错误，不能把撤销当退款或删除历史。权限/scope 改变使在途结果失效；单条操作没有本章 bulk-action 的持久幂等重试承诺，未知结果先查当前订阅。
- 权威/源码：[单条操作](../../../frontend/src/features/subscriptions/components/SubscriptionActionsDialog.vue)、[行入口状态限制](../../../frontend/src/features/subscriptions/pages/AdminSubscriptionsPage.vue)、[API](../../../frontend/src/features/subscriptions/api/admin.ts)。用例：`FE-PAGE-ADMIN-SUBSCRIPTIONS-ACTION-2`。

### FS-SUB-006 批量订阅操作与结果未知恢复
- 前置/输入：选择 1–100 条符合动作状态的订阅；extend 允许 active/expired，reset_quota/revoke 仅 active，restore 仅 revoked。days 必须非零整数且绝对值≤36500；批量 reset_quota 至少选 daily/weekly/monthly 一项。
- 流程/结果：冻结 action/IDs/参数，排序去重后生成与 scope/载荷对应的 operation key；POST `/admin/subscriptions/bulk-action` 携 `Idempotency-Key`。展示 success_count/failed_count 和逐条结果；批量结果不是跨订阅事务全成功保证。
- 异常/取消：超时、5xx、408、409 或无法确定结果时锁定原参数并保留 key，重试同一意图；仅确定拒绝的其他 4xx 可释放尚未处于未知状态的 key。完成仅删除自己的 key，迟到旧响应不能清后来新意图；关闭弹窗/退出不能抹掉未决结果记录。
- 权威/源码：[批量对话框](../../../frontend/src/features/subscriptions/components/BulkSubscriptionActionDialog.vue)、[幂等意图 owner](../../../frontend/src/features/subscriptions/components/bulkSubscriptionOperation.ts)、[bulk API](../../../frontend/src/features/subscriptions/api/admin.ts)。用例：`FE-PAGE-ADMIN-SUBSCRIPTIONS-ACTION-2`。这是源码确认的恢复合同，完整断网/重登/真实服务端重放仍须执行验收。

## 本章所属设置能力

以下入口均在 `/admin/settings`。完整 tab 工作区只给 super_admin；普通 admin 走 DelegatedSettings 的服务端共享策略及字段白名单，不能按面板 FIELDS 用例角色推断可见全部 tab。聚合面板产生字段 patch，由 app 携**真实 revision**一次 PUT `/admin/settings`；409 保留基线/服务器/草稿对比，由操作者选保留字段，结果未知先重读，不自动覆盖。独立面板自行 GET/PUT 与保存反馈，**不属于聚合 revision PUT，也没有由前端发明的 CAS**。总流程、委派界面、敏感认证及执行记录归第四章；此处列本章领域内容。[SettingsWorkspace](../../../frontend/src/app/compositions/settings/SettingsWorkspace.vue)、[transaction](../../../frontend/src/app/compositions/settings/transaction.ts)、[settings API](../../../frontend/src/features/system/api/settings.ts)。下表来源链接即具体面板，FIELDS/WRITE 都是定义而非通过记录。

### FS-RES-012 资源聚合设置与配额通知
- 输入/流程：下列面板先改本地草稿，统一保存后以服务端回读为基线；离页放弃未保存草稿不触发领域额外写命令。非法值定位字段；不把“调度设置保存成功”当作真实上游调度/恢复验证。

| 面板与关键输入/校验 | 权威字段/流程 | 既有 case ID |
| --- | --- | --- |
| [ClaudeVersion](../../../frontend/src/features/resources/settings/ClaudeVersionSettingsPanel.vue)：最小/最大 Claude Code 版本 | min_claude_code_version、max_claude_code_version 聚合保存 | `FE-SETTING-CLAUDEVERSIONSETTINGSPANEL-FIELDS`、`FE-SETTING-CLAUDEVERSIONSETTINGSPANEL-WRITE` |
| [Scheduling](../../../frontend/src/features/resources/settings/SchedulingSettingsPanel.vue)：未分组 Key、平台调度阈值、OpenAI 订阅/低成本优先、OAuth 调度倍率、TTFT、Grok 默认模型/跨客户端映射/URL 模式、指纹/metadata/CCH、Claude system prompt/cache、客户端 dateline、UA/版本自动同步 | openai_oauth_scheduling_rate_multiplier 空→null，有限非负值含 0 保留；prompt blocks 按已有结构校验；只提交脏字段，运行选择归服务端 | `FE-SETTING-SCHEDULINGSETTINGSPANEL-FIELDS`、`FE-SETTING-SCHEDULINGSETTINGSPANEL-WRITE` |
| [BpsImages](../../../frontend/src/features/resources/settings/BpsImagesSettingsPanel.vue)：BPS/relay 开关、模式、relay origin、警告/预留、body/budget、请求/图片/存储/TTL 上限 | relay 要求 HTTPS 纯 origin，无凭据/path/query/hash；整数及 budget、预留、存储之间约束由 [preparePatch](../../../frontend/src/features/resources/settings/preparePatch.ts) 校验 | `FE-SETTING-BPSIMAGESSETTINGSPANEL-FIELDS`、`FE-SETTING-BPSIMAGESSETTINGSPANEL-WRITE` |
| [QuotaNotification](../../../frontend/src/features/resources/settings/QuotaNotificationSettingsPanel.vue)：账号额度提醒开关、接收邮箱增删 | account_quota_notify_enabled/emails；邮箱组件校验，保存不等于已发邮件 | `FE-SETTING-QUOTANOTIFICATIONSETTINGSPANEL-FIELDS`、`FE-SETTING-QUOTANOTIFICATIONSETTINGSPANEL-WRITE` |

### FS-RES-013 冷却、探测、用量与 WebSearch 独立设置
- 前置/流程：各面板分别加载，加载失败/未 ready 禁止以默认空稿覆盖；自己的保存按钮提交，失败留本地错误，可重读。其成功不清掉尚未提交的全局草稿；关闭聚合工作区也不回滚已经成功的独立命令。

| 面板与实际动作 | 权威 API/边界 | 既有 case ID |
| --- | --- | --- |
| [OverloadCooldown](../../../frontend/src/features/resources/settings/OverloadCooldownSettingsPanel.vue)：enabled、cooldown_minutes | GET/PUT `/admin/settings/overload-cooldown`，界面分钟范围 1–120 | `FE-SETTING-OVERLOADCOOLDOWNSETTINGSPANEL-FIELDS`、`FE-SETTING-OVERLOADCOOLDOWNSETTINGSPANEL-WRITE` |
| [RateLimitCooldown](../../../frontend/src/features/resources/settings/RateLimitCooldownSettingsPanel.vue)：enabled、cooldown_seconds | GET/PUT `/admin/settings/rate-limit-429-cooldown`，界面秒范围 1–7200 | `FE-SETTING-RATELIMITCOOLDOWNSETTINGSPANEL-FIELDS`、`FE-SETTING-RATELIMITCOOLDOWNSETTINGSPANEL-WRITE` |
| [StreamTimeout](../../../frontend/src/features/resources/settings/StreamTimeoutSettingsPanel.vue)：开关、窗口/次数阈值、action、暂不可调度分钟 | GET/PUT `/admin/settings/stream-timeout`；保存策略不代表已经中断/恢复在途流 | `FE-SETTING-STREAMTIMEOUTSETTINGSPANEL-FIELDS`、`FE-SETTING-STREAMTIMEOUTSETTINGSPANEL-WRITE` |
| [Rectifier](../../../frontend/src/features/resources/settings/RectifierSettingsPanel.vue)：总开关、thinking signature/budget、apikey signature 与 patterns | GET/PUT `/admin/settings/rectifier`；按现有字段写，真实修复路径见第四章运行证据 | `FE-SETTING-RECTIFIERSETTINGSPANEL-FIELDS`、`FE-SETTING-RECTIFIERSETTINGSPANEL-WRITE` |
| [BillingProbe](../../../frontend/src/features/resources/settings/BillingProbeSettingsPanel.vue)：启用及 interval_minutes | GET/PUT `/admin/accounts/upstream-billing-probe/settings`，查询/保存失败分别反馈 | `FE-SETTING-BILLINGPROBESETTINGSPANEL-FIELDS`、`FE-SETTING-BILLINGPROBESETTINGSPANEL-WRITE` |
| [OllamaUsage](../../../frontend/src/features/resources/settings/OllamaUsageSettingsPanel.vue)：启用、间隔、防抖 | GET/PUT `/admin/accounts/ollama-cloud-usage/settings`；不是保存上游密码的通用入口 | `FE-SETTING-OLLAMAUSAGESETTINGSPANEL-FIELDS`、`FE-SETTING-OLLAMAUSAGESETTINGSPANEL-WRITE` |
| [OpenCodeUsage](../../../frontend/src/features/resources/settings/OpenCodeUsageSettingsPanel.vue)：启用、间隔、防抖 | GET/PUT `/admin/accounts/opencode-go-usage/settings`；实际查询仍取各账号状态 | `FE-SETTING-OPENCODEUSAGESETTINGSPANEL-FIELDS`、`FE-SETTING-OPENCODEUSAGESETTINGSPANEL-WRITE` |
| [WebSearch](../../../frontend/src/features/resources/settings/WebSearchSettingsPanel.vue)：启用、provider 增删/展开、key 显隐/复制、quota、proxy、订阅/到期日期，确认重置用量，测试查询弹窗 | GET/PUT `/admin/settings/web-search-emulation`；POST `/reset-usage` 只重置选中 provider_type，`/test` 取真实服务结果；404 未配置可建立草稿，代理列表失败只使候选为空；无 key 不复制掩码 | `FE-SETTING-WEBSEARCHSETTINGSPANEL-FIELDS`、`FE-SETTING-WEBSEARCHSETTINGSPANEL-WRITE` |

### FS-ACCESS-007 默认配额、可信 IP、客户端与 Fast 策略
- 输入/流程：下表均为聚合 patch；校验失败留草稿，保存需真实 settings revision。可信 IP/客户端策略是服务端输入，不是浏览器自行认证；默认配额不是批量改写现有用户，风控全局开关也不替代独立引擎配置。

| 面板与关键输入/结果 | 字段/异常边界 | 既有 case ID |
| --- | --- | --- |
| [ClientIp](../../../frontend/src/features/access-policy/settings/ClientIpSettingsPanel.vue)：信任转发 IP、header 列表 | api_key_acl_trust_forwarded_ip、forwarded_client_ip_headers；沿已配置可信来源处理，不从任意请求头授信 | `FE-SETTING-CLIENTIPSETTINGSPANEL-FIELDS`、`FE-SETTING-CLIENTIPSETTINGSPANEL-WRITE` |
| [DefaultQuota](../../../frontend/src/features/access-policy/settings/DefaultQuotaSettingsPanel.vue)：每平台日/周/月额度 | default_platform_quotas；空/负/非有限→null，**0 保留为显式禁用**，与 Group 订阅额度 0→null 不同；[转换源](../../../frontend/src/features/access-policy/settings/platformQuotas.ts) | `FE-SETTING-DEFAULTQUOTASETTINGSPANEL-FIELDS`、`FE-SETTING-DEFAULTQUOTASETTINGSPANEL-WRITE` |
| [FastPolicy](../../../frontend/src/features/access-policy/settings/FastPolicySettingsPanel.vue)：规则增删、tier/scope、用户选择、模型白名单、action、fallback/error 文案 | pass/filter/force_priority/block；清空无效模型项，非 block 不带 block 文案，无模型限制不带 fallback；命令解释由服务端 | `FE-SETTING-FASTPOLICYSETTINGSPANEL-FIELDS`、`FE-SETTING-FASTPOLICYSETTINGSPANEL-WRITE` |
| [CodexClients](../../../frontend/src/features/access-policy/settings/CodexClientsSettingsPanel.vue)：诊断模板、版本上下限、App Server 许可、黑白名单及指纹信号 | 保留 min/max/CLI 策略现有字段；不把诊断模板当实际调用结果，真实诊断见第四章 | `FE-SETTING-CODEXCLIENTSSETTINGSPANEL-FIELDS`、`FE-SETTING-CODEXCLIENTSSETTINGSPANEL-WRITE` |
| [RiskControl](../../../frontend/src/features/access-policy/settings/RiskControlSettingsPanel.vue)：总开关、cyber session block/TTL、用户 allowlist | risk_control_enabled 与 cyber_*；引擎密钥/规则在 FS-ACCESS-005 独立保存，不能声称一起提交 | `FE-SETTING-RISKCONTROLSETTINGSPANEL-FIELDS`、`FE-SETTING-RISKCONTROLSETTINGSPANEL-WRITE` |

### FS-ACCESS-008 面板限流与 Beta 策略独立设置
- 前置/输入：super_admin 的 security/gateway 面板。PanelRateLimit 编辑 enabled、exempt_admin、user_rpm、heavy_rpm、public_ip_rpm；BetaPolicy 对 beta_token 编辑 action=pass/filter/block、scope、模型白名单、错误文案、fallback action/message，可应用预设及快捷模型 pattern。保存清除空 pattern，无白名单不带 fallback；匹配及执行按服务端合同，不由前端尝试真实网关请求来验证。
- 流程/结果：各自 GET/PUT `/admin/settings/panel-rate-limit`、`/admin/settings/beta-policy`，独立保存并反馈；不是 Group 或全局 aggregate payload 的子事务。加载失败需重试，未加载不提交，取消本地编辑不回滚过去成功保存。
- 权威/源码：[PanelRateLimit](../../../frontend/src/features/access-policy/settings/PanelRateLimitSettingsPanel.vue)、[BetaPolicy](../../../frontend/src/features/access-policy/settings/BetaPolicySettingsPanel.vue)、[settings API](../../../frontend/src/features/system/api/settings.ts)。用例：`FE-SETTING-PANELRATELIMITSETTINGSPANEL-FIELDS`、`FE-SETTING-PANELRATELIMITSETTINGSPANEL-WRITE`、`FE-SETTING-BETAPOLICYSETTINGSPANEL-FIELDS`、`FE-SETTING-BETAPOLICYSETTINGSPANEL-WRITE`。

### FS-CAT-005 目录可见性和说明设置
- 前置/输入：features tab 的 AvailableChannels/ModelPlaza；编辑 available_channels_enabled、model_plaza_enabled、model_plaza_require_auth、model_plaza_description。描述有安全 Markdown 展示，开启模型广场不意味着匿名自动有私有组或专属价格。
- 流程/结果：面板 patch 进入 aggregate revision PUT，回读后菜单/访问 gate 消费公共设置。现有 opt-in 菜单和显式 false guard 差异见 FS-CAT-001/003；取消/409 不自动覆盖他人的开关。
- 权威/源码：[可用渠道设置](../../../frontend/src/features/catalog/settings/AvailableChannelsSettingsPanel.vue)、[模型广场设置](../../../frontend/src/features/catalog/settings/ModelPlazaSettingsPanel.vue)。用例：`FE-SETTING-AVAILABLECHANNELSSETTINGSPANEL-FIELDS`、`FE-SETTING-AVAILABLECHANNELSSETTINGSPANEL-WRITE`、`FE-SETTING-MODELPLAZASETTINGSPANEL-FIELDS`、`FE-SETTING-MODELPLAZASETTINGSPANEL-WRITE`。

### FS-PRICE-007 站点计费模式配置
- 前置/输入：features tab，选择 recharge_and_subscription、recharge_only、subscription_only；这是站点售卖模式，不是渠道 token/image 计价模式。
- 流程/结果：经 app 投影成 subscription_enabled/payment_balance_disabled 两个既有字段，分别为 true/false、false/false、true/true；同 aggregate revision 保存。旧数据两项同时禁用时展示规则优先 recharge_only，不伪造新枚举或后台收费规则。
- 异常/取消：冲突/未知结果按 Settings 共同合同；选择不产生付款或给用户授予订阅。第三章负责购买 UI、provider、订单与实际支付。
- 权威/源码：[BillingMode 面板](../../../frontend/src/features/pricing/settings/BillingModeSettingsPanel.vue)、[合同](../../../frontend/src/features/pricing/settings/contracts.ts)、[app 投影](../../../frontend/src/app/navigation/siteBillingMode.ts)。用例：`FE-SETTING-BILLINGMODESETTINGSPANEL-FIELDS`、`FE-SETTING-BILLINGMODESETTINGSPANEL-WRITE`。

### FS-SUB-007 默认订阅与到期提醒设置
- 前置/输入：users tab 的默认订阅只从 active subscription 组取候选，增删 group_id/validity_days，天数归一为 1–36500 整数，同组重复阻止提交；notifications tab 编辑 subscription_expiry_notify_enabled。
- 流程/结果：default_subscriptions/expiry 开关同 aggregate revision PUT；默认配置不等于即时批量授予已有用户，到期提醒开关也不等于已发送邮件。认证来源的默认授予组合由第一章说明，手动分配见 FS-SUB-004。
- 异常/取消：组查询失败不生成虚假可选组；校验/冲突留草稿，取消不授予，不发通知。真实发信和真实注册后授予仍需各自 fixture/执行记录。
- 权威/源码：[默认订阅面板](../../../frontend/src/features/subscriptions/settings/DefaultSubscriptionsSettingsPanel.vue)、[归一/去重](../../../frontend/src/features/subscriptions/settings/defaultSubscriptionSettings.ts)、[提醒面板](../../../frontend/src/features/subscriptions/settings/ExpiryNotificationSettingsPanel.vue)。用例：`FE-SETTING-DEFAULTSUBSCRIPTIONSSETTINGSPANEL-FIELDS`、`FE-SETTING-DEFAULTSUBSCRIPTIONSSETTINGSPANEL-WRITE`、`FE-SETTING-EXPIRYNOTIFICATIONSETTINGSPANEL-FIELDS`、`FE-SETTING-EXPIRYNOTIFICATIONSETTINGSPANEL-WRITE`。

### FS-RES-014 账号优先级快捷编辑
- 前置/输入：账号表格 priority 列可见时有减/加按钮及输入；整数夹到 1–9999，连续点按防抖 450ms；输入提交保存，Escape 取消输入草稿。父页面同时读取 accounts.manage 与 account.update.priority 字段权限，组件消费实时 canEdit/scopeKey；列可见不授予写权限。
- 流程/结果：PUT `/admin/accounts/:id` 只提交 priority；成功 emit updated，失败回退原 priority 并报错。这是独立快捷命令，不等待账号编辑弹窗的保存。
- 生命周期：捕获原 session/scope、账号和 priority，450ms 后及发送前复核；换身份/目标、撤权或卸载取消定时器和 signal，不再卸载 flush。迟到结果不能覆盖新目标；STEP_UP_CANCELLED/ERR_CANCELED 不报业务失败，仍释放 saving 和恢复草稿。Abort 不代表服务端回滚。
- 权威/源码：[单元格](../../../frontend/src/features/resources/components/AccountPriorityCell.vue)、[实际父列](../../../frontend/src/app/compositions/resource-workspace/AccountsPage.vue)。关联用例：`FE-PAGE-ADMIN-ACCOUNTS-ACTION-1`、`FE-PAGE-ADMIN-ACCOUNTS-ACTION-2`；尚不能以这两个组合定义替代该入口权限变化/离页的执行证据。

## 重构差异与验收边界

历史侧路径及规模依据主复核的 [FRONTEND_REVIEW](../FRONTEND_REVIEW.md)，当前 owner/调用链由本章所链本版源码核对；本次不执行 Git，也不把中间阶段文件快照冒充原始 `6f48d6ab…`。保留 URL/DTO/有效行为与移动文件是不同事实，以下列明运行职责变化。

| 原组织 | 当前唯一 owner/运行组合 | 保留与变化的边界 |
| --- | --- | --- |
| views/admin/AccountsView、ProxiesView，components/account，api/admin/accounts/proxies | app/resource-workspace + resources 的 API、凭据/授权/限制/提交模型及面板 | 继续原接口与有效 Native/BPS/Cloud；新增职责隔离和 actor/目标生命周期约束，不表示已经验证上游全部组合 |
| views/admin/GroupsView 及 Group helper | app/group-editor 调用 access-policy/pricing/subscriptions 公开 facets | 6,902 行旧混合页拆为领域草稿/面板，单次原子 Group 命令保留；552→34 是重构中间 groupDraft 对照，不是原始基线大小 |
| views/admin/RiskControlView、api/admin/riskControl | access-policy 配置/运行组件及 API | 独立配置与全局启用仍是两个命令，真实失败语义保留 |
| constants/platformCatalog、ModelPlaza/AvailableChannels 页面、api/channels | catalog 目录、查询与展示；app 注入倍率/价格枚举投影 | 公共/私有 scope 分离，保留原 endpoint 和默认倍率 fallback；不在 catalog 新建计费权威 |
| views/admin/ChannelsView、api/admin/channels | pricing 渠道页、表单模型、API | 保留模型/区间/时段及成本规则，明确空值/0，状态写按字段授权 |
| admin/user SubscriptionsView、stores/subscriptions、两套订阅 API | subscriptions 页面、独立对话框、scope store/API | 授予/有效性与付款分开；批量未知结果保留幂等意图，页面销毁停止本域查询/轮询 |

- 页面进入/刷新/历史返回已有定义，以上按钮和子面板关联现有 ACTION/SETTING case；部分 ACTION 覆盖多个小功能，不能以“引用了 case”声称每个小功能已独立执行。若执行中需细分证据，在既有结果 schema 的 variant/fixture/evidence 里记录，不在本章另造验收状态。
- 管理合规确认关联现有 `FE-PAGE-ADMIN-ACCOUNTS-MANUAL-COMPLIANCE`、`FE-PAGE-ADMIN-PROXIES-MANUAL-COMPLIANCE`、`FE-PAGE-ADMIN-GROUPS-MANUAL-COMPLIANCE`、`FE-PAGE-ADMIN-RISK-CONTROL-MANUAL-COMPLIANCE`、`FE-PAGE-ADMIN-CHANNELS-PRICING-MANUAL-COMPLIANCE`、`FE-PAGE-ADMIN-SUBSCRIPTIONS-MANUAL-COMPLIANCE`；必须由有权测试操作者确认，文档不代为接受。
- 真实 OAuth/CRS/上游余额与额度/代理出口/审核与 WebSearch、SMTP 通知、售卖和支付仍有外部数据与服务依赖。本章完成是源码事实和可执行规格整理，不是 Docker、逐页、真实外部服务或完整业务验收通过。
