# 10 · 用量分析、统计快照与有界导出

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`feat(usage): preserve analytics snapshots and bounded exports`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. /admin/usage 面向站点管理范围，保留用量明细、错误请求、用户排行和 API 密钥排行；API 密钥排行显示所属用户，受成本和导出权限限制。

2. /usage 仅查询当前登录用户自己的 Key 和用量；用量明细/API 密钥排行布局与管理端一致，排行不显示多余的所属用户列。用户端 JSON 不能携带 account_cost 等上游成本。

3. 列表、筛选、汇总、模型分布、排行、导出使用一致的数据归属和计费口径；actual_cost 表示客户实收，不因 account_id 过滤伪装成上游账号成本。用户 API Key 用量范围保留既有 90 天限制。

4. 前端切换筛选、刷新或卸载时取消过期统计/模型/趋势请求，避免旧响应覆盖最新数据和积压请求。管理页延迟图表计时器随刷新/卸载清理。

5. /admin/ops 的核心快照不等待切换率图表，显示设置与数据并行；卸载后迟到设置不能重启刷新。/keys 的列表不再等待慢统计，未知金额显示 — 而非假零；相关代码归入客户端组。

6. 性能优化沿用原接口与数据语义，不添加第二套报表服务、历史清理、估算总数或新的数据库索引。真实慢 SQL 与线上端到端耗时尚需另行测量。

保留基于水位和统计快照的有界导出、并发额度、取消和字段授权；失败、客户端中断以及空结果的行为沿当前接口执行。供应商用量快照、固定窗口、账号/模型/日维度分页与精确结算使用同一原生用量数据，第 12 组记录其供给范围。Ops 健康与 SLA 统一放在第 13 组。

## 入口与配置归属

/admin/usage、/usage、/admin/ops、/keys；API Key breakdown 使用既有 dashboard/usage 服务。

## 最终实现方案

1. 使用原 dashboard/usage/Ops 查询和原费用定义；用户与管理范围分开，API Key 排行沿既有 breakdown。

2. 前端把列表/核心快照与慢统计解耦，取消过期请求，卸载后不恢复计时器；未知金额使用缺失态。

3. 不增加估算总数、重复报表服务或索引，也不将 UI 改善宣称为未经测量的 SQL 性能收益。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

用户/管理用量隔离、取消旧请求、慢统计/页面加载测试；不宣称生产耗时变化。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| source | 26 | 1571 | 463 | 0 |
| test | 14 | 1362 | 63 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 13 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| A | source | 37 | 0 | 10 | [backend/internal/config/usage_export.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/config/usage_export.go) |
| M | source | 50 | 15 | 10 | [backend/internal/handler/admin/dashboard_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/dashboard_handler.go) |
| A | test | 151 | 0 | 10 | [backend/internal/handler/admin/dashboard_handler_api_key_breakdown_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/dashboard_handler_api_key_breakdown_test.go) |
| M | source | 20 | 2 | 10 | [backend/internal/handler/admin/usage_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/usage_handler.go) |
| M | test | 24 | 0 | 10 | [backend/internal/handler/admin/usage_handler_request_type_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/usage_handler_request_type_test.go) |
| M | source | 90 | 1 | 10 | [backend/internal/handler/usage_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/usage_handler.go) |
| M | test | 91 | 9 | 10 | [backend/internal/handler/usage_handler_request_type_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/usage_handler_request_type_test.go) |
| A | source | 86 | 0 | 10 | [backend/internal/handler/usageexport/stream.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/usageexport/stream.go) |
| M | source | 42 | 1 | 10 | [backend/internal/pkg/usagestats/usage_log_types.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/usagestats/usage_log_types.go) |
| A | source | 76 | 0 | 10 | [backend/internal/repository/usage_log_export.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/usage_log_export.go) |
| A | test | 74 | 0 | 10 | [backend/internal/repository/usage_log_export_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/usage_log_export_integration_test.go) |
| M | test | 177 | 0 | 10 | [backend/internal/repository/usage_log_repo_breakdown_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/usage_log_repo_breakdown_test.go) |
| M | source | 74 | 43 | 10,12 | [backend/internal/repository/usage_log_repo_query.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/usage_log_repo_query.go) |
| M | test | 27 | 3 | 10,12 | [backend/internal/repository/usage_log_repo_request_type_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/usage_log_repo_request_type_test.go) |
| M | source | 8 | 16 | 10,12 | [backend/internal/repository/usage_log_repo_stats.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/usage_log_repo_stats.go) |
| M | source | 117 | 31 | 10,12 | [backend/internal/repository/usage_log_repo_trend.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/usage_log_repo_trend.go) |
| M | source | 9 | 0 | 10 | [backend/internal/service/dashboard_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/dashboard_service.go) |
| A | source | 89 | 0 | 10 | [backend/internal/service/usage_export.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/usage_export.go) |
| A | test | 91 | 0 | 10 | [backend/internal/service/usage_export_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/usage_export_test.go) |
| M | source | 32 | 0 | 10,12 | [backend/internal/service/usage_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/usage_service.go) |
| M | test | 3 | 0 | 10 | [frontend/src/__tests__/integration/usage-reasoning-effort.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/__tests__/integration/usage-reasoning-effort.spec.ts) |
| A | test | 66 | 0 | 10 | [frontend/src/api/__tests__/usageExport.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/__tests__/usageExport.spec.ts) |
| M | source | 23 | 4 | 10 | [frontend/src/api/admin/dashboard.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/admin/dashboard.ts) |
| M | source | 3 | 2 | 10 | [frontend/src/api/admin/usage.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/admin/usage.ts) |
| M | source | 26 | 6 | 10 | [frontend/src/api/usage.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/usage.ts) |
| A | source | 84 | 0 | 10 | [frontend/src/api/usageExport.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/usageExport.ts) |
| A | source | 87 | 0 | 10 | [frontend/src/components/admin/usage/APIKeyTokenRanking.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/usage/APIKeyTokenRanking.vue) |
| A | source | 266 | 0 | 10 | [frontend/src/components/admin/usage/BreakdownRanking.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/usage/BreakdownRanking.vue) |
| M | source | 27 | 5 | 10 | [frontend/src/components/admin/usage/UsageFilters.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/usage/UsageFilters.vue) |
| M | source | 2 | 2 | 10 | [frontend/src/components/admin/usage/UsageTable.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/usage/UsageTable.vue) |
| M | source | 38 | 153 | 10 | [frontend/src/components/admin/usage/UserTokenRanking.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/usage/UserTokenRanking.vue) |
| A | test | 172 | 0 | 10 | [frontend/src/components/admin/usage/__tests__/APIKeyTokenRanking.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/usage/__tests__/APIKeyTokenRanking.spec.ts) |
| A | test | 166 | 0 | 10 | [frontend/src/components/admin/usage/__tests__/BreakdownRanking.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/usage/__tests__/BreakdownRanking.spec.ts) |
| M | test | 21 | 0 | 10 | [frontend/src/components/admin/usage/__tests__/UsageFilters.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/usage/__tests__/UsageFilters.spec.ts) |
| M | source | 10 | 7 | 10 | [frontend/src/views/KeyUsageView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/KeyUsageView.vue) |
| M | source | 125 | 77 | 10 | [frontend/src/views/admin/UsageView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/UsageView.vue) |
| M | test | 211 | 28 | 10 | [frontend/src/views/admin/__tests__/UsageView.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/UsageView.spec.ts) |
| M | source | 2 | 2 | 10 | [frontend/src/views/auth/USAGE_EXAMPLES.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/auth/USAGE_EXAMPLES.md) |
| M | source | 148 | 96 | 10 | [frontend/src/views/user/UsageView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/user/UsageView.vue) |
| M | test | 88 | 23 | 10 | [frontend/src/views/user/__tests__/UsageView.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/user/__tests__/UsageView.spec.ts) |
