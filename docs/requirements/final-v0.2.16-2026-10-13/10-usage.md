# 10 · 用量分析、页面加载与有界快照导出

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。


2026-10-14 增量归并：本页已纳入通过验收的 New API 实现，来源和逐文件归属见 [归并分析](NEW-API-INTEGRATION-2026-10-14.md)。原快照 SHA 仅代表 2026-10-13 历史输入。

## 最终需求

1. /admin/usage 面向站点管理范围，保留用量明细、错误请求、用户排行和 API 密钥排行；API 密钥排行显示所属用户，受成本和导出权限限制。

2. /usage 仅查询当前登录用户自己的 Key 和用量；用量明细/API 密钥排行布局与管理端一致，排行不显示多余的所属用户列。用户端 JSON 不能携带 account_cost 等上游成本。

3. 列表、筛选、汇总、模型分布、排行、导出使用一致的数据归属和计费口径；actual_cost 表示客户实收，不因 account_id 过滤伪装成上游账号成本。用户 API Key 用量范围保留既有 90 天限制。

4. 前端切换筛选、刷新或卸载时取消过期统计/模型/趋势请求，避免旧响应覆盖最新数据和积压请求。管理页延迟图表计时器随刷新/卸载清理。

5. /admin/ops 的核心快照不等待切换率图表，显示设置与数据并行；卸载后迟到设置不能重启刷新。/keys 的列表不再等待慢统计，未知金额显示 — 而非假零；相关代码归入客户端组。

6. 性能优化沿用原接口与数据语义，不添加第二套报表服务、历史清理、估算总数或新的数据库索引。真实慢 SQL 与线上端到端耗时尚需另行测量。

## 入口与配置归属

/admin/usage、/usage、/admin/ops、/keys；API Key breakdown 使用既有 dashboard/usage 服务。

## 最终实现方案

1. 使用原 dashboard/usage/Ops 查询和原费用定义；用户与管理范围分开，API Key 排行沿既有 breakdown。

2. 前端把列表/核心快照与慢统计解耦，取消过期请求，卸载后不恢复计时器；未知金额使用缺失态。

3. 不增加估算总数、重复报表服务或索引，也不将 UI 改善宣称为未经测量的 SQL 性能收益。

## 边界、失败路径与依赖

没有额外文件归属组关联。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。此组与关联组共同形成完整运行行为；本次归并不额外改写已验收的业务源码。

## 验收要求

用户/管理用量隔离、取消旧请求、慢统计/页面加载测试；不宣称生产耗时变化。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 1 | 2 | 2 | 0 |
| source | 30 | 1565 | 471 | 0 |
| test | 15 | 1385 | 63 | 0 |

相对官方 v0.2.16 的累计差异，已包含本批实现；不是本次归并额外编写的代码。

## 来源提交

- `64fa31719baa9fa11ffc36842b49608955d3720e`
- `6dc1165380ed5f93a5f63729eb5c3b8421d61126`
- `a2988a14ca54b99b09a6f64024166d52fa8db132`
- `c6a92e1501a3ed5bb405c02bd541fcb59366bd28`
- `e37f32177a2b3f31414e8e2801d1f20678983fe5`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| A | source | 37 | 0 | 10 | [backend/internal/config/usage_export.go](../../../backend/internal/config/usage_export.go) |
| M | source | 50 | 15 | 10 | [backend/internal/handler/admin/dashboard_handler.go](../../../backend/internal/handler/admin/dashboard_handler.go) |
| A | test | 151 | 0 | 10 | [backend/internal/handler/admin/dashboard_handler_api_key_breakdown_test.go](../../../backend/internal/handler/admin/dashboard_handler_api_key_breakdown_test.go) |
| M | source | 6 | 0 | 10 | [backend/internal/handler/admin/ops_ws_handler.go](../../../backend/internal/handler/admin/ops_ws_handler.go) |
| M | source | 20 | 2 | 10 | [backend/internal/handler/admin/usage_handler.go](../../../backend/internal/handler/admin/usage_handler.go) |
| M | test | 24 | 0 | 10 | [backend/internal/handler/admin/usage_handler_request_type_test.go](../../../backend/internal/handler/admin/usage_handler_request_type_test.go) |
| M | source | 90 | 1 | 10 | [backend/internal/handler/usage_handler.go](../../../backend/internal/handler/usage_handler.go) |
| M | test | 91 | 9 | 10 | [backend/internal/handler/usage_handler_request_type_test.go](../../../backend/internal/handler/usage_handler_request_type_test.go) |
| A | source | 86 | 0 | 10 | [backend/internal/handler/usageexport/stream.go](../../../backend/internal/handler/usageexport/stream.go) |
| M | source | 42 | 1 | 10 | [backend/internal/pkg/usagestats/usage_log_types.go](../../../backend/internal/pkg/usagestats/usage_log_types.go) |
| A | source | 76 | 0 | 10 | [backend/internal/repository/usage_log_export.go](../../../backend/internal/repository/usage_log_export.go) |
| A | test | 74 | 0 | 10 | [backend/internal/repository/usage_log_export_integration_test.go](../../../backend/internal/repository/usage_log_export_integration_test.go) |
| M | test | 177 | 0 | 10 | [backend/internal/repository/usage_log_repo_breakdown_test.go](../../../backend/internal/repository/usage_log_repo_breakdown_test.go) |
| M | source | 48 | 42 | 10 | [backend/internal/repository/usage_log_repo_query.go](../../../backend/internal/repository/usage_log_repo_query.go) |
| M | test | 3 | 3 | 10 | [backend/internal/repository/usage_log_repo_request_type_test.go](../../../backend/internal/repository/usage_log_repo_request_type_test.go) |
| M | source | 3 | 11 | 10 | [backend/internal/repository/usage_log_repo_stats.go](../../../backend/internal/repository/usage_log_repo_stats.go) |
| M | source | 114 | 29 | 10 | [backend/internal/repository/usage_log_repo_trend.go](../../../backend/internal/repository/usage_log_repo_trend.go) |
| M | source | 9 | 0 | 10 | [backend/internal/service/dashboard_service.go](../../../backend/internal/service/dashboard_service.go) |
| M | source | 1 | 1 | 10 | [backend/internal/service/ops_scheduled_report_service.go](../../../backend/internal/service/ops_scheduled_report_service.go) |
| A | source | 89 | 0 | 10 | [backend/internal/service/usage_export.go](../../../backend/internal/service/usage_export.go) |
| A | test | 91 | 0 | 10 | [backend/internal/service/usage_export_test.go](../../../backend/internal/service/usage_export_test.go) |
| M | source | 25 | 0 | 10 | [backend/internal/service/usage_service.go](../../../backend/internal/service/usage_service.go) |
| M | test | 3 | 0 | 10 | [frontend/src/__tests__/integration/usage-reasoning-effort.spec.ts](../../../frontend/src/__tests__/integration/usage-reasoning-effort.spec.ts) |
| A | test | 66 | 0 | 10 | [frontend/src/api/__tests__/usageExport.spec.ts](../../../frontend/src/api/__tests__/usageExport.spec.ts) |
| M | source | 23 | 4 | 10 | [frontend/src/api/admin/dashboard.ts](../../../frontend/src/api/admin/dashboard.ts) |
| M | source | 3 | 2 | 10 | [frontend/src/api/admin/usage.ts](../../../frontend/src/api/admin/usage.ts) |
| M | source | 26 | 6 | 10 | [frontend/src/api/usage.ts](../../../frontend/src/api/usage.ts) |
| A | source | 84 | 0 | 10 | [frontend/src/api/usageExport.ts](../../../frontend/src/api/usageExport.ts) |
| A | source | 87 | 0 | 10 | [frontend/src/components/admin/usage/APIKeyTokenRanking.vue](../../../frontend/src/components/admin/usage/APIKeyTokenRanking.vue) |
| A | source | 266 | 0 | 10 | [frontend/src/components/admin/usage/BreakdownRanking.vue](../../../frontend/src/components/admin/usage/BreakdownRanking.vue) |
| M | source | 27 | 5 | 10 | [frontend/src/components/admin/usage/UsageFilters.vue](../../../frontend/src/components/admin/usage/UsageFilters.vue) |
| M | source | 2 | 2 | 10 | [frontend/src/components/admin/usage/UsageTable.vue](../../../frontend/src/components/admin/usage/UsageTable.vue) |
| M | source | 38 | 153 | 10 | [frontend/src/components/admin/usage/UserTokenRanking.vue](../../../frontend/src/components/admin/usage/UserTokenRanking.vue) |
| A | test | 172 | 0 | 10 | [frontend/src/components/admin/usage/__tests__/APIKeyTokenRanking.spec.ts](../../../frontend/src/components/admin/usage/__tests__/APIKeyTokenRanking.spec.ts) |
| A | test | 166 | 0 | 10 | [frontend/src/components/admin/usage/__tests__/BreakdownRanking.spec.ts](../../../frontend/src/components/admin/usage/__tests__/BreakdownRanking.spec.ts) |
| M | test | 21 | 0 | 10 | [frontend/src/components/admin/usage/__tests__/UsageFilters.spec.ts](../../../frontend/src/components/admin/usage/__tests__/UsageFilters.spec.ts) |
| M | source | 10 | 7 | 10 | [frontend/src/views/KeyUsageView.vue](../../../frontend/src/views/KeyUsageView.vue) |
| M | source | 125 | 77 | 10 | [frontend/src/views/admin/UsageView.vue](../../../frontend/src/views/admin/UsageView.vue) |
| M | test | 211 | 28 | 10 | [frontend/src/views/admin/__tests__/UsageView.spec.ts](../../../frontend/src/views/admin/__tests__/UsageView.spec.ts) |
| M | source | 22 | 13 | 10 | [frontend/src/views/admin/ops/OpsDashboard.vue](../../../frontend/src/views/admin/ops/OpsDashboard.vue) |
| A | test | 47 | 0 | 10 | [frontend/src/views/admin/ops/__tests__/OpsDashboard.spec.ts](../../../frontend/src/views/admin/ops/__tests__/OpsDashboard.spec.ts) |
| M | source | 3 | 1 | 10 | [frontend/src/views/admin/ops/components/OpsAlertEventsCard.vue](../../../frontend/src/views/admin/ops/components/OpsAlertEventsCard.vue) |
| M | source | 5 | 3 | 10 | [frontend/src/views/admin/ops/components/OpsDashboardHeader.vue](../../../frontend/src/views/admin/ops/components/OpsDashboardHeader.vue) |
| M | documentation | 2 | 2 | 10 | [frontend/src/views/auth/USAGE_EXAMPLES.md](../../../frontend/src/views/auth/USAGE_EXAMPLES.md) |
| M | source | 148 | 96 | 10 | [frontend/src/views/user/UsageView.vue](../../../frontend/src/views/user/UsageView.vue) |
| M | test | 88 | 23 | 10 | [frontend/src/views/user/__tests__/UsageView.spec.ts](../../../frontend/src/views/user/__tests__/UsageView.spec.ts) |

## 2026-10-14 新增需求与实施边界

用量列表/筛选统计/导出使用配置窗口，默认最近 90 天；新增用户和管理两个 GET export 入口，共用快照、稳定 keyset、Redis 租约、取消/超时和 NDJSON 完成标记。前端验证完整性后生成 CSV/XLSX，不再全量分页累积记录。旧日期仍可按窗口查询。

## 关联实现（由其他组提交）

同一共享文件保持单一主提交，跨组依赖随完整提交集验收。

| 主归属组 | 类别 | 文件 |
| --- | --- | --- |
| 16 | generated | [backend/cmd/server/wire_gen.go](../../../backend/cmd/server/wire_gen.go) |
| 02 | source | [backend/internal/authz/routes.go](../../../backend/internal/authz/routes.go) |
| 16 | source | [backend/internal/config/config.go](../../../backend/internal/config/config.go) |
| 16 | test | [backend/internal/server/api_contract_test.go](../../../backend/internal/server/api_contract_test.go) |
| 02 | source | [backend/internal/server/middleware/admin_authorization.go](../../../backend/internal/server/middleware/admin_authorization.go) |
| 02 | test | [backend/internal/server/middleware/admin_authorization_test.go](../../../backend/internal/server/middleware/admin_authorization_test.go) |
| 02 | source | [backend/internal/server/middleware/admin_response.go](../../../backend/internal/server/middleware/admin_response.go) |
| 16 | source | [backend/internal/server/routes/admin.go](../../../backend/internal/server/routes/admin.go) |
| 02 | source | [backend/internal/server/routes/user.go](../../../backend/internal/server/routes/user.go) |
| 16 | source | [backend/internal/service/wire.go](../../../backend/internal/service/wire.go) |
| 15 | tooling | [deploy/config.example.yaml](../../../deploy/config.example.yaml) |
