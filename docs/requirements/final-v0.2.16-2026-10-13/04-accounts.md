# 04 · 账号、Anthropic 授权、亲和历史与 Fable

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。

## 最终需求

1. Anthropic 新增账号和重新授权展示 OAuth、OAuth token、Setup Token（长期有效）三种方式。原作者曾标为长期 Setup Token 的授权方式重命名为 OAuth token；新增手动导入 claude setup-token 生成的 OAuth token，不能用新入口覆盖旧授权方式。

2. 手动 Setup Token 支持纯 token 或 export CLAUDE_CODE_OAUTH_TOKEN=... 文本，去除外层引号并检查 sk-ant-oat 前缀。使用 inference 范围；不通过粘贴时间伪造供应商签发时间或承诺从导入起再有效一年。真实到期与撤销由上游凭据决定。

3. Anthropic 长期 sticky history 是调度亲和历史，和 Setup Token 有效期是两个独立需求。短期绑定 miss 后可使用长期历史作为优先候选；仍须经过模型、配额、并发、限流、RPM、窗口及利润门检查。

4. 历史按分组和会话隔离。临时切换备用账号不能无条件覆盖原历史；保留 Redis Lua 的不存在或相同才写入规则。绑定后的 TTL、历史 TTL 上限与利润准入后的绑定调用沿用当前实现；历史不应污染非 Anthropic 平台。

5. Fable 独立阈值同时支持全站 anthropic_fable scope 和账号覆盖，调度缓存保留覆盖字段。仅对 Fable 的专用窗口判断，不用共享 7d 指标冒充 Fable 窗口；读取异常不能清除限制，解除限制不能覆盖并发写入的上游 429。

6. 账号列表会话窗口费用按实际窗口起点批量查询，保留 StandardCost 口径、最多 10 组并发以及查询失败省略费用字段的行为；lite 列表直接生成 lite DTO。

7. 其他平台授权、账号属性、原生模型候选和原生调度规则继续使用当前行为。包含 RBAC 的账号表单调整仅是权限投影与可写字段限制，不增加另一套账号管理系统。

## 入口与配置归属

/admin/accounts；account_scheduling_thresholds.anthropic_fable、credentials.anthropic_fable_scheduling_threshold 与既有 gateway.scheduling sticky 配置。共享 config/gateway 文件的跨域修改在第 16 组登记。

## 最终实现方案

1. Setup Token 解析复用账号创建/重新授权，不覆盖既有 OAuth token 方式，不伪造供应商签发或有效期。

2. 长期 sticky history 使用原 Redis CAS 与资格门，短绑定 miss 后作为候选；备用账号不无条件覆写历史。

3. Fable 全局与账号覆盖复用阈值执行器与缓存投影，读取失败不清限流；窗口费用按相同起点批量查询，失败省略费用而非填零。

## 边界、失败路径与依赖

关联功能组：05, 06, 07。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。上面描述现有最终行为，本次不重新实现业务。

## 验收要求

Setup Token 解析、sticky CAS、Fable scope/读取失败/限流保留和账号列表批量费用已有回归。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | --- | --- | --- | --- |
| source | 32 | 2158 | 609 | 0 |
| test | 28 | 1543 | 27 | 0 |

这是相对 v0.2.16 的累计定制差异，不是本次新写代码。删除/新增重命名分别计数；生成代码与测试单列。

## 来源提交

- `1c56f12c4c90d9435e7959cdc25ff39106062a97`
- `3bd5cd406dd37b332f01a446718caca3a8a4222f`
- `49b29ef158e6319cfd6b1227781307de5f8f870e`
- `4b2cfde513867c40dbcc663a5ddea30cdd286dd2`
- `4c5aa7b49988f5752b01940475530832c188b7ad`
- `944fdb1eff8e7ea33ed70ff722b6822f962e9255`
- `c6a92e1501a3ed5bb405c02bd541fcb59366bd28`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | --- | --- | --- | --- |
| M | source | 17 | 17 | 04 | [backend/internal/handler/admin/account_codex_import.go](../../../backend/internal/handler/admin/account_codex_import.go) |
| M | source | 20 | 20 | 04 | [backend/internal/handler/admin/account_data.go](../../../backend/internal/handler/admin/account_data.go) |
| M | source | 120 | 79 | 04 | [backend/internal/handler/admin/account_handler.go](../../../backend/internal/handler/admin/account_handler.go) |
| M | test | 102 | 0 | 04 | [backend/internal/handler/admin/account_handler_list_test.go](../../../backend/internal/handler/admin/account_handler_list_test.go) |
| M | test | 27 | 0 | 04 | [backend/internal/handler/admin/account_handler_long_context_billing_test.go](../../../backend/internal/handler/admin/account_handler_long_context_billing_test.go) |
| A | test | 53 | 0 | 04 | [backend/internal/handler/gateway_sticky_bind_context_test.go](../../../backend/internal/handler/gateway_sticky_bind_context_test.go) |
| M | source | 6 | 4 | 04 | [backend/internal/pkg/antigravity/oauth.go](../../../backend/internal/pkg/antigravity/oauth.go) |
| M | source | 7 | 5 | 04 | [backend/internal/pkg/geminicli/oauth.go](../../../backend/internal/pkg/geminicli/oauth.go) |
| M | source | 7 | 5 | 04 | [backend/internal/pkg/oauth/oauth.go](../../../backend/internal/pkg/oauth/oauth.go) |
| M | source | 26 | 6 | 04,07 | [backend/internal/pkg/openai/oauth.go](../../../backend/internal/pkg/openai/oauth.go) |
| M | source | 10 | 8 | 04 | [backend/internal/pkg/xai/oauth.go](../../../backend/internal/pkg/xai/oauth.go) |
| M | source | 230 | 43 | 04,07 | [backend/internal/repository/account_repo.go](../../../backend/internal/repository/account_repo.go) |
| M | test | 87 | 0 | 04 | [backend/internal/repository/account_repo_integration_test.go](../../../backend/internal/repository/account_repo_integration_test.go) |
| M | source | 62 | 0 | 04 | [backend/internal/repository/gateway_cache.go](../../../backend/internal/repository/gateway_cache.go) |
| A | test | 147 | 0 | 04 | [backend/internal/repository/gateway_cache_sticky_history_test.go](../../../backend/internal/repository/gateway_cache_sticky_history_test.go) |
| M | source | 5 | 3 | 04,07 | [backend/internal/repository/scheduler_cache.go](../../../backend/internal/repository/scheduler_cache.go) |
| M | test | 10 | 0 | 04 | [backend/internal/repository/scheduler_cache_test.go](../../../backend/internal/repository/scheduler_cache_test.go) |
| M | source | 4 | 25 | 04 | [backend/internal/service/account_credentials_redact.go](../../../backend/internal/service/account_credentials_redact.go) |
| M | source | 155 | 44 | 04 | [backend/internal/service/account_scheduling_threshold_eval.go](../../../backend/internal/service/account_scheduling_threshold_eval.go) |
| M | test | 16 | 1 | 04 | [backend/internal/service/account_scheduling_threshold_eval_test.go](../../../backend/internal/service/account_scheduling_threshold_eval_test.go) |
| M | source | 8 | 3 | 04 | [backend/internal/service/account_scheduling_threshold_reason.go](../../../backend/internal/service/account_scheduling_threshold_reason.go) |
| M | source | 4 | 3 | 04 | [backend/internal/service/account_service.go](../../../backend/internal/service/account_service.go) |
| M | source | 10 | 0 | 04 | [backend/internal/service/account_usage_service.go](../../../backend/internal/service/account_usage_service.go) |
| M | test | 4 | 0 | 04 | [backend/internal/service/account_usage_service_batch_test.go](../../../backend/internal/service/account_usage_service_batch_test.go) |
| M | test | 11 | 0 | 04 | [backend/internal/service/gateway_multiplatform_test.go](../../../backend/internal/service/gateway_multiplatform_test.go) |
| M | source | 54 | 19 | 04 | [backend/internal/service/gateway_scheduling.go](../../../backend/internal/service/gateway_scheduling.go) |
| M | source | 201 | 10 | 04,06,07 | [backend/internal/service/gateway_service.go](../../../backend/internal/service/gateway_service.go) |
| A | test | 686 | 0 | 04 | [backend/internal/service/gateway_sticky_session_ttl_test.go](../../../backend/internal/service/gateway_sticky_session_ttl_test.go) |
| M | test | 13 | 0 | 04 | [backend/internal/service/generate_session_hash_test.go](../../../backend/internal/service/generate_session_hash_test.go) |
| M | source | 12 | 1 | 04 | [backend/internal/service/model_rate_limit.go](../../../backend/internal/service/model_rate_limit.go) |
| M | source | 69 | 16 | 04,06,07 | [backend/internal/service/openai_account_scheduler.go](../../../backend/internal/service/openai_account_scheduler.go) |
| M | test | 20 | 9 | 04,07 | [backend/internal/service/openai_account_scheduler_test.go](../../../backend/internal/service/openai_account_scheduler_test.go) |
| M | source | 73 | 4 | 04 | [backend/internal/service/ratelimit_service.go](../../../backend/internal/service/ratelimit_service.go) |
| M | test | 25 | 0 | 04 | [backend/internal/service/ratelimit_service_scheduling_threshold_test.go](../../../backend/internal/service/ratelimit_service_scheduling_threshold_test.go) |
| M | source | 244 | 22 | 04,05,07 | [frontend/src/components/account/BulkEditAccountModal.vue](../../../frontend/src/components/account/BulkEditAccountModal.vue) |
| M | source | 148 | 168 | 04,05 | [frontend/src/components/account/CreateAccountModal.vue](../../../frontend/src/components/account/CreateAccountModal.vue) |
| M | source | 421 | 58 | 04,05,07 | [frontend/src/components/account/EditAccountModal.vue](../../../frontend/src/components/account/EditAccountModal.vue) |
| M | source | 54 | 2 | 04 | [frontend/src/components/account/OAuthAuthorizationFlow.vue](../../../frontend/src/components/account/OAuthAuthorizationFlow.vue) |
| M | source | 55 | 10 | 04 | [frontend/src/components/account/ReAuthAccountModal.vue](../../../frontend/src/components/account/ReAuthAccountModal.vue) |
| M | test | 57 | 4 | 04,05,07 | [frontend/src/components/account/__tests__/BulkEditAccountModal.spec.ts](../../../frontend/src/components/account/__tests__/BulkEditAccountModal.spec.ts) |
| M | test | 101 | 1 | 04,05 | [frontend/src/components/account/__tests__/CreateAccountModal.spec.ts](../../../frontend/src/components/account/__tests__/CreateAccountModal.spec.ts) |
| M | test | 111 | 3 | 04,05,07 | [frontend/src/components/account/__tests__/EditAccountModal.spec.ts](../../../frontend/src/components/account/__tests__/EditAccountModal.spec.ts) |
| M | test | 10 | 1 | 04 | [frontend/src/components/account/__tests__/OpenAIReferralCell.transport.spec.ts](../../../frontend/src/components/account/__tests__/OpenAIReferralCell.transport.spec.ts) |
| M | source | 14 | 10 | 04 | [frontend/src/components/admin/account/AccountActionMenu.vue](../../../frontend/src/components/admin/account/AccountActionMenu.vue) |
| M | source | 2 | 2 | 04 | [frontend/src/components/admin/account/AccountTableActions.vue](../../../frontend/src/components/admin/account/AccountTableActions.vue) |
| M | source | 55 | 4 | 04 | [frontend/src/components/admin/account/ReAuthAccountModal.vue](../../../frontend/src/components/admin/account/ReAuthAccountModal.vue) |
| M | test | 3 | 0 | 04 | [frontend/src/components/admin/account/__tests__/AccountActionMenu.position.spec.ts](../../../frontend/src/components/admin/account/__tests__/AccountActionMenu.position.spec.ts) |
| M | test | 3 | 0 | 04 | [frontend/src/components/admin/account/__tests__/AccountActionMenu.spark_shadow.spec.ts](../../../frontend/src/components/admin/account/__tests__/AccountActionMenu.spark_shadow.spec.ts) |
| A | test | 30 | 0 | 04 | [frontend/src/composables/__tests__/useAccountOAuth.spec.ts](../../../frontend/src/composables/__tests__/useAccountOAuth.spec.ts) |
| M | source | 22 | 1 | 04 | [frontend/src/composables/useAccountOAuth.ts](../../../frontend/src/composables/useAccountOAuth.ts) |
| M | test | 3 | 1 | 04 | [frontend/src/utils/__tests__/accountSelection.spec.ts](../../../frontend/src/utils/__tests__/accountSelection.spec.ts) |
| M | source | 6 | 1 | 04 | [frontend/src/utils/accountSelection.ts](../../../frontend/src/utils/accountSelection.ts) |
| M | source | 37 | 16 | 04,07 | [frontend/src/views/admin/AccountsView.vue](../../../frontend/src/views/admin/AccountsView.vue) |
| M | test | 1 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.bulkEdit.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.bulkEdit.spec.ts) |
| M | test | 18 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.lite.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.lite.spec.ts) |
| M | test | 1 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.priorityColumn.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.priorityColumn.spec.ts) |
| M | test | 1 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.schedulerScore.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.schedulerScore.spec.ts) |
| M | test | 1 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.selectAllResults.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.selectAllResults.spec.ts) |
| M | test | 1 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.sparkShadow.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.sparkShadow.spec.ts) |
| M | test | 1 | 1 | 04 | [frontend/src/views/admin/__tests__/AccountsView.usageWindowsHint.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.usageWindowsHint.spec.ts) |
