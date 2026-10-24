# 06 · 用量排行与页面加载

归档日期：2026-10-07。文件快照：main `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238`；官方比较基线 `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`。

本页登记最终需求与实际文件归属；不把本轮提交整理等同于重新实现或完整安全审计。

## 最终需求

1. /admin/usage 面向站点管理范围，保留用量明细、错误请求、用户排行和 API 密钥排行；API 密钥排行显示所属用户，受成本和导出权限限制。

2. /usage 仅查询当前登录用户自己的 Key 和用量；用量明细/API 密钥排行布局与管理端一致，排行不显示多余的所属用户列。用户端 JSON 不能携带 account_cost 等上游成本。

3. 列表、筛选、汇总、模型分布、排行、导出使用一致的数据归属和计费口径；actual_cost 表示客户实收，不因 account_id 过滤伪装成上游账号成本。用户 API Key 用量范围保留既有 90 天限制。

4. 前端切换筛选、刷新或卸载时取消过期统计/模型/趋势请求，避免旧响应覆盖最新数据和积压请求。管理页延迟图表计时器随刷新/卸载清理。

5. /admin/ops 的核心快照不等待切换率图表，显示设置与数据并行；卸载后迟到设置不能重启刷新。/keys 的列表不再等待慢统计，未知金额显示 — 而非假零；相关代码归入客户端组。

6. 性能优化沿用原接口与数据语义，不添加第二套报表服务、历史清理、估算总数或新的数据库索引。真实慢 SQL 与线上端到端耗时尚需另行测量。

## 入口与配置归属

/admin/usage、/usage、/admin/ops、/keys；API Key breakdown 使用既有 dashboard/usage 服务。

## 验收边界

普通用户不能跨用户查询、无成本字段泄露；管理排行有所属用户；慢请求模拟下列表/核心快照可见，刷新取消旧请求；已有榜单组件和 Usage/Keys/Ops 回归。不得把 UI 解耦宣称为所有 SQL 已加速。

验证状态统一见 [总说明](README.md#验证和未关闭边界)。本页列的是验收要求，不是宣称本轮已重新运行这些检查。

## 对比统计

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 业务代码 | 24 | 1046 | 351 | 0 |
| 测试/夹具 | 12 | 1065 | 38 | 0 |
| 已有文档/许可 | 1 | 2 | 2 | 0 |

统计为最终文件相对固定官方基线的累计差异，并非本次整理新写的代码。对重命名统一展开为删除/新增；跨域文件只设一个主归属。

## 修改文件

| 状态 | 类型 | +行 | −行 | 文件 |
| --- | --- | ---: | ---: | --- |
| M | 业务代码 | 50 | 15 | [backend/internal/handler/admin/dashboard_handler.go](../../../backend/internal/handler/admin/dashboard_handler.go) |
| A | 测试/夹具 | 151 | 0 | [backend/internal/handler/admin/dashboard_handler_api_key_breakdown_test.go](../../../backend/internal/handler/admin/dashboard_handler_api_key_breakdown_test.go) |
| M | 业务代码 | 6 | 0 | [backend/internal/handler/admin/ops_ws_handler.go](../../../backend/internal/handler/admin/ops_ws_handler.go) |
| M | 业务代码 | 4 | 0 | [backend/internal/handler/admin/usage_handler.go](../../../backend/internal/handler/admin/usage_handler.go) |
| M | 测试/夹具 | 24 | 0 | [backend/internal/handler/admin/usage_handler_request_type_test.go](../../../backend/internal/handler/admin/usage_handler_request_type_test.go) |
| M | 业务代码 | 81 | 0 | [backend/internal/handler/usage_handler.go](../../../backend/internal/handler/usage_handler.go) |
| M | 测试/夹具 | 56 | 9 | [backend/internal/handler/usage_handler_request_type_test.go](../../../backend/internal/handler/usage_handler_request_type_test.go) |
| M | 业务代码 | 42 | 1 | [backend/internal/pkg/usagestats/usage_log_types.go](../../../backend/internal/pkg/usagestats/usage_log_types.go) |
| M | 测试/夹具 | 177 | 0 | [backend/internal/repository/usage_log_repo_breakdown_test.go](../../../backend/internal/repository/usage_log_repo_breakdown_test.go) |
| M | 测试/夹具 | 3 | 3 | [backend/internal/repository/usage_log_repo_request_type_test.go](../../../backend/internal/repository/usage_log_repo_request_type_test.go) |
| M | 业务代码 | 3 | 11 | [backend/internal/repository/usage_log_repo_stats.go](../../../backend/internal/repository/usage_log_repo_stats.go) |
| M | 业务代码 | 114 | 29 | [backend/internal/repository/usage_log_repo_trend.go](../../../backend/internal/repository/usage_log_repo_trend.go) |
| M | 业务代码 | 9 | 0 | [backend/internal/service/dashboard_service.go](../../../backend/internal/service/dashboard_service.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/service/ops_scheduled_report_service.go](../../../backend/internal/service/ops_scheduled_report_service.go) |
| M | 业务代码 | 10 | 0 | [backend/internal/service/usage_service.go](../../../backend/internal/service/usage_service.go) |
| M | 测试/夹具 | 3 | 0 | [frontend/src/__tests__/integration/usage-reasoning-effort.spec.ts](../../../frontend/src/__tests__/integration/usage-reasoning-effort.spec.ts) |
| M | 业务代码 | 23 | 4 | [frontend/src/api/admin/dashboard.ts](../../../frontend/src/api/admin/dashboard.ts) |
| M | 业务代码 | 3 | 2 | [frontend/src/api/admin/usage.ts](../../../frontend/src/api/admin/usage.ts) |
| M | 业务代码 | 26 | 6 | [frontend/src/api/usage.ts](../../../frontend/src/api/usage.ts) |
| A | 业务代码 | 87 | 0 | [frontend/src/components/admin/usage/APIKeyTokenRanking.vue](../../../frontend/src/components/admin/usage/APIKeyTokenRanking.vue) |
| A | 业务代码 | 266 | 0 | [frontend/src/components/admin/usage/BreakdownRanking.vue](../../../frontend/src/components/admin/usage/BreakdownRanking.vue) |
| M | 业务代码 | 27 | 5 | [frontend/src/components/admin/usage/UsageFilters.vue](../../../frontend/src/components/admin/usage/UsageFilters.vue) |
| M | 业务代码 | 2 | 2 | [frontend/src/components/admin/usage/UsageTable.vue](../../../frontend/src/components/admin/usage/UsageTable.vue) |
| M | 业务代码 | 38 | 153 | [frontend/src/components/admin/usage/UserTokenRanking.vue](../../../frontend/src/components/admin/usage/UserTokenRanking.vue) |
| A | 测试/夹具 | 172 | 0 | [frontend/src/components/admin/usage/__tests__/APIKeyTokenRanking.spec.ts](../../../frontend/src/components/admin/usage/__tests__/APIKeyTokenRanking.spec.ts) |
| A | 测试/夹具 | 166 | 0 | [frontend/src/components/admin/usage/__tests__/BreakdownRanking.spec.ts](../../../frontend/src/components/admin/usage/__tests__/BreakdownRanking.spec.ts) |
| M | 测试/夹具 | 21 | 0 | [frontend/src/components/admin/usage/__tests__/UsageFilters.spec.ts](../../../frontend/src/components/admin/usage/__tests__/UsageFilters.spec.ts) |
| M | 业务代码 | 10 | 7 | [frontend/src/views/KeyUsageView.vue](../../../frontend/src/views/KeyUsageView.vue) |
| M | 业务代码 | 119 | 66 | [frontend/src/views/admin/UsageView.vue](../../../frontend/src/views/admin/UsageView.vue) |
| M | 测试/夹具 | 200 | 19 | [frontend/src/views/admin/__tests__/UsageView.spec.ts](../../../frontend/src/views/admin/__tests__/UsageView.spec.ts) |
| M | 业务代码 | 22 | 13 | [frontend/src/views/admin/ops/OpsDashboard.vue](../../../frontend/src/views/admin/ops/OpsDashboard.vue) |
| A | 测试/夹具 | 47 | 0 | [frontend/src/views/admin/ops/__tests__/OpsDashboard.spec.ts](../../../frontend/src/views/admin/ops/__tests__/OpsDashboard.spec.ts) |
| M | 业务代码 | 3 | 1 | [frontend/src/views/admin/ops/components/OpsAlertEventsCard.vue](../../../frontend/src/views/admin/ops/components/OpsAlertEventsCard.vue) |
| M | 业务代码 | 5 | 3 | [frontend/src/views/admin/ops/components/OpsDashboardHeader.vue](../../../frontend/src/views/admin/ops/components/OpsDashboardHeader.vue) |
| M | 已有文档/许可 | 2 | 2 | [frontend/src/views/auth/USAGE_EXAMPLES.md](../../../frontend/src/views/auth/USAGE_EXAMPLES.md) |
| M | 业务代码 | 95 | 32 | [frontend/src/views/user/UsageView.vue](../../../frontend/src/views/user/UsageView.vue) |
| M | 测试/夹具 | 45 | 7 | [frontend/src/views/user/__tests__/UsageView.spec.ts](../../../frontend/src/views/user/__tests__/UsageView.spec.ts) |
