# 04 · 账号授权、调度、粘性会话与 Fable 阈值

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`feat(accounts): preserve authorization scheduling and Fable controls`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 2026-10-17 号池调度补充修订

统一号池调度已实施：OpenAI 兼容入口固定使用 openai_unified_v2；资格、实际可用容量和模型健康先于排名，同健康层内按账号优先级严格分层，再处理订阅偏好及容量加权随机。全池参与有界候选选择，占位和最终复核有预算；通用平台保留原生语义并改为一次排序。亲和不绕过供应商资格、配额及协议归属，账号列表评分只代表静态估算。

本次经用户授权归入既有相关提交；完整需求、独立风险、迁移与回滚边界及实测结果见[号池调度审计第 11 节](../../new-api-account-pool-scheduling-review-2026-10-17.md#11-第二阶段实施与大账号池审计2026-10-17)。下方行数、来源和 TSV/blob 仍是原始重建快照，保留用于历史追溯，不冒充本次最终源码摘要。

## 2026-10-17 最终调度修订

优先级仍先于成本；开启低倍率优先后按 10% 成本档硬排序，档内按同层剩余容量加权。不限并发账号借用同完整业务层的最大有限容量，成本和健康快照在本次选择中固定。候选保留后续层，SQL/Redis 逻辑执行共用 64 次预算及 3 秒截止时间；高层最多消耗可用预算的 75%，资源释放另计且始终执行。原生入口改按实际槽位判断可用性，保留原排名；会话只读批量预筛后最多四次原子确认。窗口费用缓存分批预热并预留终检预算，RPM 使用多键读取。

实现、复核与验证边界见[审计报告第 15 节](../../new-api-account-pool-scheduling-review-2026-10-17.md#15-最终优化实施与复核2026-10-17)。用户要求归并既有相关提交；不执行发版、部署或生产迁移。

## 最终需求

1. Anthropic 新增账号和重新授权展示 OAuth、OAuth token、Setup Token（长期有效）三种方式。原作者曾标为长期 Setup Token 的授权方式重命名为 OAuth token；新增手动导入 claude setup-token 生成的 OAuth token，不能用新入口覆盖旧授权方式。

2. 手动 Setup Token 支持纯 token 或 export CLAUDE_CODE_OAUTH_TOKEN=... 文本，去除外层引号并检查 sk-ant-oat 前缀。使用 inference 范围；不通过粘贴时间伪造供应商签发时间或承诺从导入起再有效一年。真实到期与撤销由上游凭据决定。

3. Anthropic 长期 sticky history 是调度亲和历史，和 Setup Token 有效期是两个独立需求。短期绑定 miss 后可使用长期历史作为优先候选；仍须经过模型、配额、并发、限流、RPM、窗口及利润门检查。

4. 历史按分组和会话隔离。临时切换备用账号不能无条件覆盖原历史；保留 Redis Lua 的不存在或相同才写入规则。绑定后的 TTL、历史 TTL 上限与利润准入后的绑定调用沿用当前实现；历史不应污染非 Anthropic 平台。

5. Fable 独立阈值同时支持全站 anthropic_fable scope 和账号覆盖，调度缓存保留覆盖字段。仅对 Fable 的专用窗口判断，不用共享 7d 指标冒充 Fable 窗口；读取异常不能清除限制，解除限制不能覆盖并发写入的上游 429。

6. 账号列表会话窗口费用按实际窗口起点批量查询，保留 StandardCost 口径、最多 10 组并发以及查询失败省略费用字段的行为；lite 列表直接生成 lite DTO。

7. 其他平台授权、账号属性、原生模型候选和原生调度规则继续使用当前行为。包含 RBAC 的账号表单调整仅是权限投影与可写字段限制，不增加另一套账号管理系统。

保留 Claude 配额重置额度的查询、兑换、错误反馈与账号更新通知。5h、7d 与 Fable 7d_oi 快照按各自窗口更新；只收到 5h 重置不能清空周窗口，缺少 status header 也可接收有效使用率采样。调度快照的有界 pipeline、generation fencing、缺少 metadata 时 miss 和 LRU 新鲜值继续保留。归档供应商账号停止调度并禁止普通账号管理写入，原凭据和历史仍保存。

2026-10-22 审查修复：Anthropic 阈值和用量展示逐字段核对当前凭据身份，不从旧身份投影恢复周窗口；同身份令牌刷新保留有效采样。调度 metadata 使用 v4 惰性重建，旧缓存不作为新判定依据。Fable 本地阈值通过仓储原子条件写入，不能覆盖并发到达的有效上游 429；HTTP 请求走 OAuth WS 上游时，选号与上报使用同一实际传输健康分组，compact 与强制 HTTP 保持原分组。历史错误停调缺少身份依据，未自动批量清除，需要按实际账号核实处理。

## 入口与配置归属

/admin/accounts；account_scheduling_thresholds.anthropic_fable、credentials.anthropic_fable_scheduling_threshold 与既有 gateway.scheduling sticky 配置。共享 config/gateway 文件的跨域修改在第 19 组登记。

## 最终实现方案

1. Setup Token 解析复用账号创建/重新授权，不覆盖既有 OAuth token 方式，不伪造供应商签发或有效期。

2. 长期 sticky history 使用原 Redis CAS 与资格门，短绑定 miss 后作为候选；备用账号不无条件覆写历史。

3. Fable 全局与账号覆盖复用阈值执行器与缓存投影，读取失败不清限流；窗口费用按相同起点批量查询，失败省略费用而非填零。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

Setup Token 解析、sticky CAS、Fable scope/读取失败/限流保留和账号列表批量费用已有回归。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| source | 37 | 2774 | 747 | 0 |
| test | 34 | 1973 | 34 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 62 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 17 | 17 | 04 | [backend/internal/handler/admin/account_codex_import.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/account_codex_import.go) |
| M | source | 27 | 20 | 04,12 | [backend/internal/handler/admin/account_data.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/account_data.go) |
| M | source | 120 | 79 | 04 | [backend/internal/handler/admin/account_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/account_handler.go) |
| M | test | 102 | 0 | 04 | [backend/internal/handler/admin/account_handler_list_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/account_handler_list_test.go) |
| M | test | 27 | 0 | 04 | [backend/internal/handler/admin/account_handler_long_context_billing_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/account_handler_long_context_billing_test.go) |
| A | test | 53 | 0 | 04 | [backend/internal/handler/gateway_sticky_bind_context_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/gateway_sticky_bind_context_test.go) |
| M | source | 6 | 4 | 04 | [backend/internal/pkg/antigravity/oauth.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/antigravity/oauth.go) |
| M | source | 7 | 5 | 04 | [backend/internal/pkg/geminicli/oauth.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/geminicli/oauth.go) |
| M | source | 7 | 5 | 04 | [backend/internal/pkg/oauth/oauth.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/oauth/oauth.go) |
| M | source | 26 | 6 | 04,07 | [backend/internal/pkg/openai/oauth.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/openai/oauth.go) |
| M | source | 10 | 8 | 04 | [backend/internal/pkg/xai/oauth.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/xai/oauth.go) |
| M | source | 658 | 130 | 04,07,12 | [backend/internal/repository/account_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo.go) |
| M | test | 87 | 0 | 04 | [backend/internal/repository/account_repo_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_integration_test.go) |
| M | source | 62 | 0 | 04 | [backend/internal/repository/gateway_cache.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/gateway_cache.go) |
| A | test | 147 | 0 | 04 | [backend/internal/repository/gateway_cache_sticky_history_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/gateway_cache_sticky_history_test.go) |
| M | source | 43 | 12 | 04,07 | [backend/internal/repository/scheduler_cache.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/scheduler_cache.go) |
| M | test | 10 | 0 | 04 | [backend/internal/repository/scheduler_cache_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/scheduler_cache_test.go) |
| A | test | 113 | 0 | 04 | [backend/internal/repository/scheduler_snapshot_pipeline_unit_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/scheduler_snapshot_pipeline_unit_test.go) |
| M | source | 4 | 25 | 04 | [backend/internal/service/account_credentials_redact.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_credentials_redact.go) |
| M | source | 155 | 44 | 04 | [backend/internal/service/account_scheduling_threshold_eval.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_scheduling_threshold_eval.go) |
| M | test | 16 | 1 | 04 | [backend/internal/service/account_scheduling_threshold_eval_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_scheduling_threshold_eval_test.go) |
| M | source | 8 | 3 | 04 | [backend/internal/service/account_scheduling_threshold_reason.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_scheduling_threshold_reason.go) |
| M | source | 4 | 3 | 04 | [backend/internal/service/account_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_service.go) |
| M | source | 31 | 17 | 04 | [backend/internal/service/account_usage_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_usage_service.go) |
| M | test | 4 | 0 | 04 | [backend/internal/service/account_usage_service_batch_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_usage_service_batch_test.go) |
| M | test | 104 | 0 | 04 | [backend/internal/service/account_usage_service_fable_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_usage_service_fable_test.go) |
| M | source | 31 | 2 | 04 | [backend/internal/service/claude_reset_credits.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/claude_reset_credits.go) |
| M | test | 74 | 1 | 04 | [backend/internal/service/claude_reset_credits_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/claude_reset_credits_test.go) |
| M | source | 4 | 0 | 04 | [backend/internal/service/claude_reset_redeem.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/claude_reset_redeem.go) |
| M | test | 28 | 0 | 04 | [backend/internal/service/claude_reset_redeem_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/claude_reset_redeem_test.go) |
| M | test | 11 | 0 | 04 | [backend/internal/service/gateway_multiplatform_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_multiplatform_test.go) |
| M | source | 57 | 19 | 04,12 | [backend/internal/service/gateway_scheduling.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_scheduling.go) |
| M | source | 204 | 10 | 04,06,07,14 | [backend/internal/service/gateway_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_service.go) |
| A | test | 686 | 0 | 04 | [backend/internal/service/gateway_sticky_session_ttl_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_sticky_session_ttl_test.go) |
| M | test | 13 | 0 | 04 | [backend/internal/service/generate_session_hash_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/generate_session_hash_test.go) |
| M | source | 12 | 1 | 04 | [backend/internal/service/model_rate_limit.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/model_rate_limit.go) |
| M | source | 72 | 16 | 04,06,07 | [backend/internal/service/openai_account_scheduler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_account_scheduler.go) |
| M | test | 33 | 9 | 04,07 | [backend/internal/service/openai_account_scheduler_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_account_scheduler_test.go) |
| M | source | 85 | 17 | 04 | [backend/internal/service/ratelimit_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ratelimit_service.go) |
| M | test | 25 | 0 | 04 | [backend/internal/service/ratelimit_service_scheduling_threshold_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ratelimit_service_scheduling_threshold_test.go) |
| M | test | 39 | 6 | 04 | [backend/internal/service/ratelimit_session_window_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ratelimit_session_window_test.go) |
| M | source | 1 | 0 | 04 | [frontend/src/api/admin/claudeResetCredits.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/admin/claudeResetCredits.ts) |
| M | source | 1 | 1 | 04 | [frontend/src/components/account/AccountUsageCell.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/AccountUsageCell.vue) |
| M | source | 244 | 22 | 04,05,07 | [frontend/src/components/account/BulkEditAccountModal.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/BulkEditAccountModal.vue) |
| M | source | 64 | 9 | 04 | [frontend/src/components/account/ClaudeResetCreditsCell.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/ClaudeResetCreditsCell.vue) |
| M | source | 148 | 168 | 04,05 | [frontend/src/components/account/CreateAccountModal.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/CreateAccountModal.vue) |
| M | source | 421 | 58 | 04,05,07 | [frontend/src/components/account/EditAccountModal.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/EditAccountModal.vue) |
| M | source | 54 | 2 | 04 | [frontend/src/components/account/OAuthAuthorizationFlow.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/OAuthAuthorizationFlow.vue) |
| M | source | 55 | 10 | 04 | [frontend/src/components/account/ReAuthAccountModal.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/ReAuthAccountModal.vue) |
| M | test | 57 | 4 | 04,05,07 | [frontend/src/components/account/__tests__/BulkEditAccountModal.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/__tests__/BulkEditAccountModal.spec.ts) |
| M | test | 59 | 0 | 04 | [frontend/src/components/account/__tests__/ClaudeResetCreditsCell.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/__tests__/ClaudeResetCreditsCell.spec.ts) |
| M | test | 101 | 1 | 04,05 | [frontend/src/components/account/__tests__/CreateAccountModal.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/__tests__/CreateAccountModal.spec.ts) |
| M | test | 111 | 3 | 04,05,07 | [frontend/src/components/account/__tests__/EditAccountModal.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/__tests__/EditAccountModal.spec.ts) |
| M | test | 10 | 1 | 04 | [frontend/src/components/account/__tests__/OpenAIReferralCell.transport.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/__tests__/OpenAIReferralCell.transport.spec.ts) |
| M | source | 14 | 10 | 04 | [frontend/src/components/admin/account/AccountActionMenu.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/account/AccountActionMenu.vue) |
| M | source | 2 | 2 | 04 | [frontend/src/components/admin/account/AccountTableActions.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/account/AccountTableActions.vue) |
| M | source | 55 | 4 | 04 | [frontend/src/components/admin/account/ReAuthAccountModal.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/account/ReAuthAccountModal.vue) |
| M | test | 3 | 0 | 04 | [frontend/src/components/admin/account/__tests__/AccountActionMenu.position.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/account/__tests__/AccountActionMenu.position.spec.ts) |
| M | test | 3 | 0 | 04 | [frontend/src/components/admin/account/__tests__/AccountActionMenu.spark_shadow.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/account/__tests__/AccountActionMenu.spark_shadow.spec.ts) |
| A | test | 30 | 0 | 04 | [frontend/src/composables/__tests__/useAccountOAuth.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/composables/__tests__/useAccountOAuth.spec.ts) |
| M | source | 22 | 1 | 04 | [frontend/src/composables/useAccountOAuth.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/composables/useAccountOAuth.ts) |
| M | test | 3 | 1 | 04 | [frontend/src/utils/__tests__/accountSelection.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/utils/__tests__/accountSelection.spec.ts) |
| M | source | 6 | 1 | 04 | [frontend/src/utils/accountSelection.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/utils/accountSelection.ts) |
| M | source | 37 | 16 | 04,07 | [frontend/src/views/admin/AccountsView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/AccountsView.vue) |
| M | test | 1 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.bulkEdit.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/AccountsView.bulkEdit.spec.ts) |
| M | test | 18 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.lite.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/AccountsView.lite.spec.ts) |
| M | test | 1 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.priorityColumn.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/AccountsView.priorityColumn.spec.ts) |
| M | test | 1 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.schedulerScore.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/AccountsView.schedulerScore.spec.ts) |
| M | test | 1 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.selectAllResults.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/AccountsView.selectAllResults.spec.ts) |
| M | test | 1 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.sparkShadow.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/AccountsView.sparkShadow.spec.ts) |
| M | test | 1 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.usageWindowsHint.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/AccountsView.usageWindowsHint.spec.ts) |
