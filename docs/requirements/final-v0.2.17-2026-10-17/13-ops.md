# 13 · 运维健康、SLA 分类、监控与运行指标

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`fix(ops): preserve health scoring SLA and monitoring corrections`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 2026-10-17 号池调度补充修订

请求诊断复用原错误日志，加入最多 32 事件 / 16 KiB 的调度轨迹；保留失败轨迹，成功采样为 1%。账号模型健康信号与运维 SLA 分开归属：客户端取消、本地输入及明确凭据故障不误罚账号，真实 BPS 推理失败仍反馈调度。模型健康有样本、新鲜度和容量上限，不引入多实例协调器或主动探测。

本次经用户授权归入既有相关提交；完整需求、独立风险、迁移与回滚边界及实测结果见[号池调度审计第 11 节](../../new-api-account-pool-scheduling-review-2026-10-17.md#11-第二阶段实施与大账号池审计2026-10-17)。下方行数、来源和 TSV/blob 仍是原始重建快照，保留用于历史追溯，不冒充本次最终源码摘要。

## 2026-10-17 最终调度修订

调度轨迹增加不随 32 条事件淘汰的固定摘要，汇总逻辑访问、Redis 命令、候选尝试和停止原因。固定枚举指标记录跨层、逃逸、缓存 token 与健康样本拒收；普通/逃逸聚合并非同会话的因果对照。现有 Ops 资源采样统一为每实例 5 秒；CPU 或内存连续三次达到 90% 时拒绝新请求，连续两次均低于 85% 恢复，样本缺失或超过 30 秒不拒绝。HTTP 返回 503/Retry-After，WS 新 turn 关闭码 1013，已开始的流继续。

实现、复核与验证边界见[审计报告第 15 节](../../new-api-account-pool-scheduling-review-2026-10-17.md#15-最终优化实施与复核2026-10-17)。用户要求归并既有相关提交；不执行发版、部署或生产迁移。

## 最终需求

1. 健康分继续由请求 70% 与基础设施 30% 构成；请求可靠性 80%、TTFT 20%，不按生成全程时长扣分。请求/上游错误重叠时取最大归一压力，避免重复扣分。
2. TTFT、请求错误率和上游错误率采用已配置阈值；保留已有自定义值及显式零。后台任务按 expected_interval_seconds 判断过期，未知/未运行不冒充成功。数据库及严重资源故障限制最高健康分。
3. 诊断与颜色阈值共用口径；按窗口请求数识别空闲，严重异常优先；无 SLA 样本不显示 0%，已有具体诊断不再重复派生健康分警告。
4. 有效状态为 400/413/422 且明确 request payload is too large 的请求归入 client/request/context_limit 并排除 SLA 异常。并非所有 4xx 都排除；真实上游状态和流内逻辑失败优先于包装状态。
5. 平台路由账号池耗尽计入 SLA；历史 platform/routing 429/503 的旧业务标记不再错误排除，明确本地 model_not_found、余额、额度和用户并发仍按既有规则排除。
6. 统一日志写入、SQL 查询、趋势/仪表盘、请求明细与后续聚合口径。历史原始行不改写，已有小时/日汇总不自动全量重算；渠道实际成功率也不把 SLA 排除项算成成功。
7. 原生 Channel Monitor V2 查询排除软删除分组、保留 group_id=0 平台汇总，并与原用户 AllowedGroupIDs 相交；不删除历史指标，不导入 fork 独立“近 18 次样本卡片”模式。
8. 保留运行指标采集内存修正、健康分/设置加载竞态修复和有界错误详情。Grok 上游诊断复用原 opt-in、脱敏及大小限制，不能默认记录完整敏感正文。

## 入口与配置归属

复用既有入口、配置和授权；具体落点见下方逐文件清单与关联功能组。

## 最终实现方案

1. ops_sla.go 统一日志与 SQL 规则，repository 复用；ops_health_score.go 统一健康判定，任务响应复用既有周期字段供前端展示。
2. Channel Monitor 查询复用原 where builder；采集器按当前修正实现保留有界内存读取，不新增监控系统、DTO 样本协议或额外后台作业。
3. 完整规则见 ../../OPS_HEALTH_SCORING.md、../../OPS_SLA_CLASSIFICATION.md 和 ../../channel-monitor-v2-safe-defaults.md。用户已批准的统计口径修正与生产历史重算分开。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

健康分、SLA 状态优先级、历史 SQL 条件、采集器内存、软删除分组与前端设置/诊断回归；本轮不执行服务器重算或修改线上阈值。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 4 | 101 | 0 | 0 |
| generated | 1 | 852 | 0 | 0 |
| source | 37 | 1625 | 252 | 0 |
| test | 23 | 1178 | 67 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 50 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 6 | 0 | 10,13 | [backend/internal/handler/admin/ops_ws_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/ops_ws_handler.go) |
| M | source | 3 | 2 | 13,16 | [backend/internal/handler/channel_monitor_v2_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/channel_monitor_v2_handler.go) |
| M | source | 38 | 35 | 06,13 | [backend/internal/handler/ops_error_logger.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/ops_error_logger.go) |
| M | test | 177 | 23 | 06,13 | [backend/internal/handler/ops_error_logger_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/ops_error_logger_test.go) |
| M | source | 1 | 1 | 13 | [backend/internal/repository/channel_monitor_v2_aggregation.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/channel_monitor_v2_aggregation.go) |
| M | source | 2 | 0 | 13 | [backend/internal/repository/channel_monitor_v2_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/channel_monitor_v2_repo.go) |
| M | test | 4 | 0 | 13 | [backend/internal/repository/channel_monitor_v2_repo_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/channel_monitor_v2_repo_test.go) |
| M | test | 13 | 13 | 13 | [backend/internal/repository/ops_error_where_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/ops_error_where_test.go) |
| M | source | 30 | 16 | 06,13 | [backend/internal/repository/ops_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/ops_repo.go) |
| M | source | 5 | 5 | 13 | [backend/internal/repository/ops_repo_dashboard.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/ops_repo_dashboard.go) |
| M | source | 3 | 1 | 13 | [backend/internal/repository/ops_repo_metrics.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/ops_repo_metrics.go) |
| M | test | 20 | 0 | 13 | [backend/internal/repository/ops_repo_metrics_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/ops_repo_metrics_test.go) |
| M | source | 3 | 1 | 13 | [backend/internal/repository/ops_repo_preagg.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/ops_repo_preagg.go) |
| A | test | 64 | 0 | 06,13 | [backend/internal/repository/ops_repo_recovery_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/ops_repo_recovery_test.go) |
| M | source | 1 | 1 | 13 | [backend/internal/repository/ops_repo_request_details.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/ops_repo_request_details.go) |
| M | source | 7 | 7 | 13 | [backend/internal/repository/ops_repo_trends.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/ops_repo_trends.go) |
| A | test | 159 | 0 | 13 | [backend/internal/repository/ops_sla_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/ops_sla_integration_test.go) |
| M | source | 1 | 1 | 13 | [backend/internal/service/channel_monitor_v2_error_taxonomy.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/channel_monitor_v2_error_taxonomy.go) |
| M | source | 1 | 0 | 13 | [backend/internal/service/openai_gateway_chat_completions_raw.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_chat_completions_raw.go) |
| A | test | 192 | 0 | 13 | [backend/internal/service/openai_gateway_grok_ops_detail_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_grok_ops_detail_test.go) |
| M | source | 11 | 1 | 13 | [backend/internal/service/ops_dashboard.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_dashboard.go) |
| M | source | 87 | 39 | 13 | [backend/internal/service/ops_health_score.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_health_score.go) |
| M | test | 112 | 20 | 13 | [backend/internal/service/ops_health_score_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_health_score_test.go) |
| M | source | 50 | 58 | 13 | [backend/internal/service/ops_metrics_collector.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_metrics_collector.go) |
| M | test | 74 | 0 | 13 | [backend/internal/service/ops_metrics_collector_memory_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_metrics_collector_memory_test.go) |
| M | source | 6 | 3 | 06,13 | [backend/internal/service/ops_models.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_models.go) |
| M | source | 3 | 1 | 13 | [backend/internal/service/ops_port.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_port.go) |
| M | source | 1 | 1 | 10,13 | [backend/internal/service/ops_scheduled_report_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_scheduled_report_service.go) |
| M | source | 12 | 1 | 06,13 | [backend/internal/service/ops_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_service.go) |
| M | test | 13 | 0 | 06,13 | [backend/internal/service/ops_service_batch_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_service_batch_test.go) |
| M | test | 38 | 0 | 06,13 | [backend/internal/service/ops_service_redaction_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_service_redaction_test.go) |
| M | source | 17 | 4 | 13 | [backend/internal/service/ops_settings.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_settings.go) |
| A | source | 93 | 0 | 13 | [backend/internal/service/ops_sla.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_sla.go) |
| A | test | 85 | 0 | 13 | [backend/internal/service/ops_sla_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_sla_test.go) |
| M | test | 21 | 8 | 13 | [backend/internal/service/ops_system_log_sink_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_system_log_sink_test.go) |
| M | source | 217 | 9 | 06,07,08,13,19 | [backend/internal/service/ops_upstream_context.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/ops_upstream_context.go) |
| M | source | 47 | 3 | 13 | [backend/internal/util/logredact/redact.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/util/logredact/redact.go) |
| M | test | 30 | 0 | 13 | [backend/internal/util/logredact/redact_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/util/logredact/redact_test.go) |
| A | test | 18 | 0 | 01,13 | [backend/migrations/ops_job_heartbeats_expected_interval_migration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/ops_job_heartbeats_expected_interval_migration_test.go) |
| A | test | 20 | 0 | 01,13 | [backend/migrations/ops_metric_thresholds_ttft_default_migration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/ops_metric_thresholds_ttft_default_migration_test.go) |
| A | documentation | 15 | 0 | 13,20 | [docs/OPS_FEATURE_RETIREMENT_2026_09_29.md](https://github.com/holeenlu/sub2api/blob/main/docs/OPS_FEATURE_RETIREMENT_2026_09_29.md) |
| A | documentation | 35 | 0 | 13 | [docs/OPS_HEALTH_SCORING.md](https://github.com/holeenlu/sub2api/blob/main/docs/OPS_HEALTH_SCORING.md) |
| A | documentation | 45 | 0 | 13 | [docs/OPS_SLA_CLASSIFICATION.md](https://github.com/holeenlu/sub2api/blob/main/docs/OPS_SLA_CLASSIFICATION.md) |
| M | documentation | 6 | 0 | 13 | [docs/channel-monitor-v2-safe-defaults.md](https://github.com/holeenlu/sub2api/blob/main/docs/channel-monitor-v2-safe-defaults.md) |
| M | source | 3 | 0 | 06,13 | [frontend/src/api/admin/ops.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/admin/ops.ts) |
| M | source | 1 | 1 | 13,16 | [frontend/src/features/channel-monitor-v2/RelayPulseMatrix.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/channel-monitor-v2/RelayPulseMatrix.vue) |
| M | source | 14 | 8 | 06,13,17 | [frontend/src/i18n/locales/en/admin/ops.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/admin/ops.ts) |
| A | source | 849 | 0 | 06,13,17 | [frontend/src/i18n/locales/ja/admin/ops.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/admin/ops.ts) |
| A | generated | 852 | 0 | 06,13,17 | [frontend/src/i18n/locales/zh-TW/admin/ops.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/admin/ops.ts) |
| M | source | 14 | 8 | 06,13,17 | [frontend/src/i18n/locales/zh/admin/ops.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/admin/ops.ts) |
| M | source | 22 | 13 | 10,13 | [frontend/src/views/admin/ops/OpsDashboard.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/OpsDashboard.vue) |
| A | test | 47 | 0 | 10,13 | [frontend/src/views/admin/ops/__tests__/OpsDashboard.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/__tests__/OpsDashboard.spec.ts) |
| M | source | 3 | 1 | 10,13 | [frontend/src/views/admin/ops/components/OpsAlertEventsCard.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/components/OpsAlertEventsCard.vue) |
| M | source | 43 | 21 | 10,13 | [frontend/src/views/admin/ops/components/OpsDashboardHeader.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/components/OpsDashboardHeader.vue) |
| M | source | 16 | 5 | 06,13 | [frontend/src/views/admin/ops/components/OpsErrorDetailModal.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/components/OpsErrorDetailModal.vue) |
| M | source | 5 | 1 | 06,13 | [frontend/src/views/admin/ops/components/OpsErrorLogTable.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/components/OpsErrorLogTable.vue) |
| M | source | 1 | 1 | 13 | [frontend/src/views/admin/ops/components/OpsRuntimeSettingsCard.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/components/OpsRuntimeSettingsCard.vue) |
| M | source | 2 | 2 | 13 | [frontend/src/views/admin/ops/components/OpsSettingsDialog.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/components/OpsSettingsDialog.vue) |
| M | test | 5 | 1 | 06,13 | [frontend/src/views/admin/ops/components/__tests__/OpsAlertEventsCard.pagination.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/components/__tests__/OpsAlertEventsCard.pagination.spec.ts) |
| M | test | 44 | 0 | 13 | [frontend/src/views/admin/ops/components/__tests__/OpsDashboardHeader.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/components/__tests__/OpsDashboardHeader.spec.ts) |
| M | test | 19 | 0 | 06,13 | [frontend/src/views/admin/ops/components/__tests__/OpsErrorDetailModal.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/components/__tests__/OpsErrorDetailModal.spec.ts) |
| M | test | 9 | 0 | 06,13 | [frontend/src/views/admin/ops/components/__tests__/OpsErrorLogTable.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/components/__tests__/OpsErrorLogTable.spec.ts) |
| M | test | 1 | 1 | 13 | [frontend/src/views/admin/ops/components/__tests__/OpsSettingsDialog.loading.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/components/__tests__/OpsSettingsDialog.loading.spec.ts) |
| M | test | 13 | 1 | 06,13 | [frontend/src/views/admin/ops/utils/__tests__/opsFormatters.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/utils/__tests__/opsFormatters.spec.ts) |
| M | source | 7 | 1 | 06,13 | [frontend/src/views/admin/ops/utils/opsFormatters.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ops/utils/opsFormatters.ts) |
