# 04 · 账号、Anthropic 授权与调度

归档日期：2026-10-07。文件快照：main `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238`；官方比较基线 `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`。

本页登记最终需求与实际文件归属；不把本轮提交整理等同于重新实现或完整安全审计。

## 最终需求

1. Anthropic 新增账号和重新授权展示 OAuth、OAuth token、Setup Token（长期有效）三种方式。原作者曾标为长期 Setup Token 的授权方式重命名为 OAuth token；新增手动导入 claude setup-token 生成的 OAuth token，不能用新入口覆盖旧授权方式。

2. 手动 Setup Token 支持纯 token 或 export CLAUDE_CODE_OAUTH_TOKEN=... 文本，去除外层引号并检查 sk-ant-oat 前缀。使用 inference 范围；不通过粘贴时间伪造供应商签发时间或承诺从导入起再有效一年。真实到期与撤销由上游凭据决定。

3. Anthropic 长期 sticky history 是调度亲和历史，和 Setup Token 有效期是两个独立需求。短期绑定 miss 后可使用长期历史作为优先候选；仍须经过模型、配额、并发、限流、RPM、窗口及利润门检查。

4. 历史按分组和会话隔离。临时切换备用账号不能无条件覆盖原历史；保留 Redis Lua 的不存在或相同才写入规则。绑定后的 TTL、历史 TTL 上限与利润准入后的绑定调用沿用当前实现；历史不应污染非 Anthropic 平台。

5. Fable 独立阈值同时支持全站 anthropic_fable scope 和账号覆盖，调度缓存保留覆盖字段。仅对 Fable 的专用窗口判断，不用共享 7d 指标冒充 Fable 窗口；读取异常不能清除限制，解除限制不能覆盖并发写入的上游 429。

6. 账号列表会话窗口费用按实际窗口起点批量查询，保留 StandardCost 口径、最多 10 组并发以及查询失败省略费用字段的行为；lite 列表直接生成 lite DTO。

7. 其他平台授权、账号属性、原生模型候选和原生调度规则继续使用当前行为。包含 RBAC 的账号表单调整仅是权限投影与可写字段限制，不增加另一套账号管理系统。

## 入口与配置归属

/admin/accounts；account_scheduling_thresholds.anthropic_fable、credentials.anthropic_fable_scheduling_threshold 与既有 gateway.scheduling sticky 配置。共享 config/gateway 文件的跨域修改在第 12 组登记。

## 验收边界

Setup Token 文本解析、前缀拒绝、创建/重新授权；长期历史 CAS、临时回退和平台隔离；Fable 缓存投影与窗口判断；账号列表 41 账号/2 窗口的测试查询数为 2。该数字是本地合成场景，不是生产性能承诺。

验证状态统一见 [总说明](README.md#验证和未关闭边界)。本页列的是验收要求，不是宣称本轮已重新运行这些检查。

## 对比统计

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 业务代码 | 32 | 1599 | 568 | 0 |
| 测试/夹具 | 28 | 1336 | 14 | 0 |

统计为最终文件相对固定官方基线的累计差异，并非本次整理新写的代码。对重命名统一展开为删除/新增；跨域文件只设一个主归属。

## 修改文件

| 状态 | 类型 | +行 | −行 | 文件 |
| --- | --- | ---: | ---: | --- |
| M | 业务代码 | 17 | 17 | [backend/internal/handler/admin/account_codex_import.go](../../../backend/internal/handler/admin/account_codex_import.go) |
| M | 业务代码 | 20 | 20 | [backend/internal/handler/admin/account_data.go](../../../backend/internal/handler/admin/account_data.go) |
| M | 业务代码 | 120 | 79 | [backend/internal/handler/admin/account_handler.go](../../../backend/internal/handler/admin/account_handler.go) |
| M | 测试/夹具 | 102 | 0 | [backend/internal/handler/admin/account_handler_list_test.go](../../../backend/internal/handler/admin/account_handler_list_test.go) |
| M | 测试/夹具 | 27 | 0 | [backend/internal/handler/admin/account_handler_long_context_billing_test.go](../../../backend/internal/handler/admin/account_handler_long_context_billing_test.go) |
| A | 测试/夹具 | 53 | 0 | [backend/internal/handler/gateway_sticky_bind_context_test.go](../../../backend/internal/handler/gateway_sticky_bind_context_test.go) |
| M | 业务代码 | 6 | 4 | [backend/internal/pkg/antigravity/oauth.go](../../../backend/internal/pkg/antigravity/oauth.go) |
| M | 业务代码 | 7 | 5 | [backend/internal/pkg/geminicli/oauth.go](../../../backend/internal/pkg/geminicli/oauth.go) |
| M | 业务代码 | 7 | 5 | [backend/internal/pkg/oauth/oauth.go](../../../backend/internal/pkg/oauth/oauth.go) |
| M | 业务代码 | 8 | 6 | [backend/internal/pkg/openai/oauth.go](../../../backend/internal/pkg/openai/oauth.go) |
| M | 业务代码 | 10 | 8 | [backend/internal/pkg/xai/oauth.go](../../../backend/internal/pkg/xai/oauth.go) |
| M | 业务代码 | 217 | 41 | [backend/internal/repository/account_repo.go](../../../backend/internal/repository/account_repo.go) |
| M | 测试/夹具 | 87 | 0 | [backend/internal/repository/account_repo_integration_test.go](../../../backend/internal/repository/account_repo_integration_test.go) |
| M | 业务代码 | 62 | 0 | [backend/internal/repository/gateway_cache.go](../../../backend/internal/repository/gateway_cache.go) |
| A | 测试/夹具 | 147 | 0 | [backend/internal/repository/gateway_cache_sticky_history_test.go](../../../backend/internal/repository/gateway_cache_sticky_history_test.go) |
| M | 业务代码 | 3 | 3 | [backend/internal/repository/scheduler_cache.go](../../../backend/internal/repository/scheduler_cache.go) |
| M | 测试/夹具 | 10 | 0 | [backend/internal/repository/scheduler_cache_test.go](../../../backend/internal/repository/scheduler_cache_test.go) |
| M | 业务代码 | 4 | 25 | [backend/internal/service/account_credentials_redact.go](../../../backend/internal/service/account_credentials_redact.go) |
| M | 业务代码 | 155 | 44 | [backend/internal/service/account_scheduling_threshold_eval.go](../../../backend/internal/service/account_scheduling_threshold_eval.go) |
| M | 测试/夹具 | 16 | 1 | [backend/internal/service/account_scheduling_threshold_eval_test.go](../../../backend/internal/service/account_scheduling_threshold_eval_test.go) |
| M | 业务代码 | 8 | 3 | [backend/internal/service/account_scheduling_threshold_reason.go](../../../backend/internal/service/account_scheduling_threshold_reason.go) |
| M | 业务代码 | 4 | 3 | [backend/internal/service/account_service.go](../../../backend/internal/service/account_service.go) |
| M | 业务代码 | 10 | 0 | [backend/internal/service/account_usage_service.go](../../../backend/internal/service/account_usage_service.go) |
| M | 测试/夹具 | 4 | 0 | [backend/internal/service/account_usage_service_batch_test.go](../../../backend/internal/service/account_usage_service_batch_test.go) |
| M | 测试/夹具 | 11 | 0 | [backend/internal/service/gateway_multiplatform_test.go](../../../backend/internal/service/gateway_multiplatform_test.go) |
| M | 业务代码 | 54 | 19 | [backend/internal/service/gateway_scheduling.go](../../../backend/internal/service/gateway_scheduling.go) |
| M | 业务代码 | 199 | 9 | [backend/internal/service/gateway_service.go](../../../backend/internal/service/gateway_service.go) |
| A | 测试/夹具 | 686 | 0 | [backend/internal/service/gateway_sticky_session_ttl_test.go](../../../backend/internal/service/gateway_sticky_session_ttl_test.go) |
| M | 测试/夹具 | 13 | 0 | [backend/internal/service/generate_session_hash_test.go](../../../backend/internal/service/generate_session_hash_test.go) |
| M | 业务代码 | 27 | 0 | [backend/internal/service/model_rate_limit.go](../../../backend/internal/service/model_rate_limit.go) |
| M | 业务代码 | 27 | 0 | [backend/internal/service/openai_account_scheduler.go](../../../backend/internal/service/openai_account_scheduler.go) |
| M | 测试/夹具 | 11 | 0 | [backend/internal/service/openai_account_scheduler_test.go](../../../backend/internal/service/openai_account_scheduler_test.go) |
| M | 业务代码 | 73 | 4 | [backend/internal/service/ratelimit_service.go](../../../backend/internal/service/ratelimit_service.go) |
| M | 测试/夹具 | 25 | 0 | [backend/internal/service/ratelimit_service_scheduling_threshold_test.go](../../../backend/internal/service/ratelimit_service_scheduling_threshold_test.go) |
| M | 业务代码 | 25 | 22 | [frontend/src/components/account/BulkEditAccountModal.vue](../../../frontend/src/components/account/BulkEditAccountModal.vue) |
| M | 业务代码 | 124 | 154 | [frontend/src/components/account/CreateAccountModal.vue](../../../frontend/src/components/account/CreateAccountModal.vue) |
| M | 业务代码 | 169 | 51 | [frontend/src/components/account/EditAccountModal.vue](../../../frontend/src/components/account/EditAccountModal.vue) |
| M | 业务代码 | 54 | 2 | [frontend/src/components/account/OAuthAuthorizationFlow.vue](../../../frontend/src/components/account/OAuthAuthorizationFlow.vue) |
| M | 业务代码 | 55 | 10 | [frontend/src/components/account/ReAuthAccountModal.vue](../../../frontend/src/components/account/ReAuthAccountModal.vue) |
| M | 测试/夹具 | 4 | 3 | [frontend/src/components/account/__tests__/BulkEditAccountModal.spec.ts](../../../frontend/src/components/account/__tests__/BulkEditAccountModal.spec.ts) |
| M | 测试/夹具 | 23 | 0 | [frontend/src/components/account/__tests__/CreateAccountModal.spec.ts](../../../frontend/src/components/account/__tests__/CreateAccountModal.spec.ts) |
| M | 测试/夹具 | 44 | 1 | [frontend/src/components/account/__tests__/EditAccountModal.spec.ts](../../../frontend/src/components/account/__tests__/EditAccountModal.spec.ts) |
| M | 测试/夹具 | 10 | 1 | [frontend/src/components/account/__tests__/OpenAIReferralCell.transport.spec.ts](../../../frontend/src/components/account/__tests__/OpenAIReferralCell.transport.spec.ts) |
| M | 业务代码 | 14 | 10 | [frontend/src/components/admin/account/AccountActionMenu.vue](../../../frontend/src/components/admin/account/AccountActionMenu.vue) |
| M | 业务代码 | 2 | 2 | [frontend/src/components/admin/account/AccountTableActions.vue](../../../frontend/src/components/admin/account/AccountTableActions.vue) |
| M | 业务代码 | 55 | 4 | [frontend/src/components/admin/account/ReAuthAccountModal.vue](../../../frontend/src/components/admin/account/ReAuthAccountModal.vue) |
| M | 测试/夹具 | 3 | 0 | [frontend/src/components/admin/account/__tests__/AccountActionMenu.position.spec.ts](../../../frontend/src/components/admin/account/__tests__/AccountActionMenu.position.spec.ts) |
| M | 测试/夹具 | 3 | 0 | [frontend/src/components/admin/account/__tests__/AccountActionMenu.spark_shadow.spec.ts](../../../frontend/src/components/admin/account/__tests__/AccountActionMenu.spark_shadow.spec.ts) |
| A | 测试/夹具 | 30 | 0 | [frontend/src/composables/__tests__/useAccountOAuth.spec.ts](../../../frontend/src/composables/__tests__/useAccountOAuth.spec.ts) |
| M | 业务代码 | 22 | 1 | [frontend/src/composables/useAccountOAuth.ts](../../../frontend/src/composables/useAccountOAuth.ts) |
| M | 测试/夹具 | 3 | 1 | [frontend/src/utils/__tests__/accountSelection.spec.ts](../../../frontend/src/utils/__tests__/accountSelection.spec.ts) |
| M | 业务代码 | 6 | 1 | [frontend/src/utils/accountSelection.ts](../../../frontend/src/utils/accountSelection.ts) |
| M | 业务代码 | 35 | 16 | [frontend/src/views/admin/AccountsView.vue](../../../frontend/src/views/admin/AccountsView.vue) |
| M | 测试/夹具 | 1 | 1 | [frontend/src/views/admin/__tests__/AccountsView.bulkEdit.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.bulkEdit.spec.ts) |
| M | 测试/夹具 | 18 | 1 | [frontend/src/views/admin/__tests__/AccountsView.lite.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.lite.spec.ts) |
| M | 测试/夹具 | 1 | 1 | [frontend/src/views/admin/__tests__/AccountsView.priorityColumn.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.priorityColumn.spec.ts) |
| M | 测试/夹具 | 1 | 1 | [frontend/src/views/admin/__tests__/AccountsView.schedulerScore.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.schedulerScore.spec.ts) |
| M | 测试/夹具 | 1 | 1 | [frontend/src/views/admin/__tests__/AccountsView.selectAllResults.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.selectAllResults.spec.ts) |
| M | 测试/夹具 | 1 | 1 | [frontend/src/views/admin/__tests__/AccountsView.sparkShadow.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.sparkShadow.spec.ts) |
| M | 测试/夹具 | 1 | 1 | [frontend/src/views/admin/__tests__/AccountsView.usageWindowsHint.spec.ts](../../../frontend/src/views/admin/__tests__/AccountsView.usageWindowsHint.spec.ts) |
