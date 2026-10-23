# 01 · 数据库升级与历史数据保护

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。

## 最终需求

1. 保留既有已发布 SQL 文件、迁移顺序与校验字节；不因整理 Git 历史删除数据库中的旧表、列、索引或账务数据。新增角色迁移沿用 270_three_tier_roles.sql，旧 admin 升级为 super_admin。

2. 保留 265_prepare_bps_provider_constraints 对旧 BPS 数据的升级准备，及 267/268/269 对 Fable 阈值的暂存、旧运行时清理和恢复流程；不得重新开启已被停用的账号。

3. 保留会话撤销、支付约束、诊断记录、操作日志可见级别及定时任务发起身份的原迁移。Ent 生成代码与 schema 配套保留。

4. 历史表结构存在不代表对应业务仍运行。旧打票、退役 BPS 历史结构（当前恢复范围见第 07 组）、账号质量运营、请求抓取、独立模型目录等迁移作为升级遗留登记，不能写进当前功能宣传清单。

## 入口与配置归属

backend/migrations、backend/ent；应用启动沿用原迁移流程，无新管理页面。

## 最终实现方案

1. 沿用现有 migrations 启动器、历史账本和 Ent schema/生成物；不调整已发布 SQL 的执行顺序与校验字节。

2. 265 准备迁移处理历史 BPS 约束表达式；267/268/269 保留 Fable 转存/退役清理/恢复；270 角色、271 最终平台约束、272 Excel grant 与最终树一起保留。

3. 只保留历史表/列不恢复运行消费者；账号明确停用状态不由历史整理重新激活。

## 边界、失败路径与依赖

关联功能组：07。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。上面描述现有最终行为，本次不重新实现业务。

## 验收要求

对所有迁移/Ent 逐 blob 和 file mode 比较；go test -tags=unit ./migrations ./ent/...；真实数据库升级另行验收。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | --- | --- | --- | --- |
| generated | 8 | 315 | 18 | 0 |
| migration | 47 | 1278 | 0 | 0 |
| source | 2 | 2 | 0 | 0 |
| test | 8 | 163 | 2 | 0 |

这是相对 v0.2.16 的累计定制差异，不是本次新写代码。删除/新增重命名分别计数；生成代码与测试单列。

## 来源提交

- `4b2cfde513867c40dbcc663a5ddea30cdd286dd2`
- `944fdb1eff8e7ea33ed70ff722b6822f962e9255`
- `a2988a14ca54b99b09a6f64024166d52fa8db132`
- `cba51ec0f6af3ded9bddfd41a3e8f5292181935b`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | --- | --- | --- | --- |
| M | generated | 2 | 1 | 01 | [backend/ent/migrate/schema.go](../../../backend/ent/migrate/schema.go) |
| M | generated | 88 | 1 | 01 | [backend/ent/mutation.go](../../../backend/ent/mutation.go) |
| M | generated | 19 | 15 | 01 | [backend/ent/runtime/runtime.go](../../../backend/ent/runtime/runtime.go) |
| M | source | 1 | 0 | 01 | [backend/ent/schema/pending_auth_session.go](../../../backend/ent/schema/pending_auth_session.go) |
| M | source | 1 | 0 | 01 | [backend/ent/schema/user.go](../../../backend/ent/schema/user.go) |
| M | generated | 12 | 1 | 01 | [backend/ent/user.go](../../../backend/ent/user.go) |
| M | generated | 10 | 0 | 01 | [backend/ent/user/user.go](../../../backend/ent/user/user.go) |
| M | generated | 45 | 0 | 01 | [backend/ent/user/where.go](../../../backend/ent/user/where.go) |
| M | generated | 85 | 0 | 01 | [backend/ent/user_create.go](../../../backend/ent/user_create.go) |
| M | generated | 54 | 0 | 01 | [backend/ent/user_update.go](../../../backend/ent/user_update.go) |
| M | test | 26 | 0 | 01 | [backend/internal/repository/platform_list_db_integration_test.go](../../../backend/internal/repository/platform_list_db_integration_test.go) |
| A | migration | 7 | 0 | 01 | [backend/migrations/234_ops_job_heartbeats_add_expected_interval.sql](../../../backend/migrations/234_ops_job_heartbeats_add_expected_interval.sql) |
| A | migration | 9 | 0 | 01 | [backend/migrations/235_group_no_account_fallback.sql](../../../backend/migrations/235_group_no_account_fallback.sql) |
| A | migration | 15 | 0 | 01 | [backend/migrations/236_ops_metric_thresholds_ttft_default.sql](../../../backend/migrations/236_ops_metric_thresholds_ttft_default.sql) |
| A | migration | 5 | 0 | 01 | [backend/migrations/237_add_api_key_concurrency_limit.sql](../../../backend/migrations/237_add_api_key_concurrency_limit.sql) |
| A | migration | 52 | 0 | 01 | [backend/migrations/239_codex_ticket_attempts.sql](../../../backend/migrations/239_codex_ticket_attempts.sql) |
| A | migration | 5 | 0 | 01 | [backend/migrations/241_add_account_group_rate_multiplier.sql](../../../backend/migrations/241_add_account_group_rate_multiplier.sql) |
| A | migration | 55 | 0 | 01 | [backend/migrations/241_codex_ticket_lifecycle.sql](../../../backend/migrations/241_codex_ticket_lifecycle.sql) |
| A | migration | 39 | 0 | 01 | [backend/migrations/242_prune_openai_request_timezones.sql](../../../backend/migrations/242_prune_openai_request_timezones.sql) |
| A | migration | 10 | 0 | 01 | [backend/migrations/242_request_timing_details.sql](../../../backend/migrations/242_request_timing_details.sql) |
| A | migration | 6 | 0 | 01 | [backend/migrations/243_codex_ticket_oailb_invalidation.sql](../../../backend/migrations/243_codex_ticket_oailb_invalidation.sql) |
| A | migration | 4 | 0 | 01 | [backend/migrations/243_pelican_scheduled_tests.sql](../../../backend/migrations/243_pelican_scheduled_tests.sql) |
| A | migration | 7 | 0 | 01 | [backend/migrations/244_account_group_allowed_models.sql](../../../backend/migrations/244_account_group_allowed_models.sql) |
| A | migration | 7 | 0 | 01 | [backend/migrations/244_disable_codex_ticket_harvesting.sql](../../../backend/migrations/244_disable_codex_ticket_harvesting.sql) |
| A | migration | 17 | 0 | 01 | [backend/migrations/245_openai_bps_platform.sql](../../../backend/migrations/245_openai_bps_platform.sql) |
| A | migration | 7 | 0 | 01 | [backend/migrations/245_user_group_denied_models.sql](../../../backend/migrations/245_user_group_denied_models.sql) |
| A | migration | 6 | 0 | 01 | [backend/migrations/246_group_stream_only.sql](../../../backend/migrations/246_group_stream_only.sql) |
| A | migration | 8 | 0 | 01 | [backend/migrations/247_account_quality_ops.sql](../../../backend/migrations/247_account_quality_ops.sql) |
| A | migration | 2 | 0 | 01 | [backend/migrations/248_account_quality_judgment.sql](../../../backend/migrations/248_account_quality_judgment.sql) |
| A | migration | 19 | 0 | 01 | [backend/migrations/249_account_ops_alerts.sql](../../../backend/migrations/249_account_ops_alerts.sql) |
| A | migration | 23 | 0 | 01 | [backend/migrations/249_pelican_showcase_items.sql](../../../backend/migrations/249_pelican_showcase_items.sql) |
| A | migration | 37 | 0 | 01 | [backend/migrations/250_account_token_guard.sql](../../../backend/migrations/250_account_token_guard.sql) |
| A | migration | 18 | 0 | 01 | [backend/migrations/251_request_captures.sql](../../../backend/migrations/251_request_captures.sql) |
| A | migration | 1 | 0 | 01 | [backend/migrations/252_user_observer_groups.sql](../../../backend/migrations/252_user_observer_groups.sql) |
| A | migration | 66 | 0 | 01 | [backend/migrations/253_retire_fork_features.sql](../../../backend/migrations/253_retire_fork_features.sql) |
| A | migration | 45 | 0 | 01 | [backend/migrations/254_openai_oauth_reauth.sql](../../../backend/migrations/254_openai_oauth_reauth.sql) |
| A | migration | 7 | 0 | 01 | [backend/migrations/254_quality_bps_coexist.sql](../../../backend/migrations/254_quality_bps_coexist.sql) |
| A | migration | 31 | 0 | 01 | [backend/migrations/254_retire_standalone_bps.sql](../../../backend/migrations/254_retire_standalone_bps.sql) |
| A | migration | 61 | 0 | 01 | [backend/migrations/255_account_token_guard_v2.sql](../../../backend/migrations/255_account_token_guard_v2.sql) |
| A | migration | 3 | 0 | 01 | [backend/migrations/256_openai_oauth_reauth_proxy_override.sql](../../../backend/migrations/256_openai_oauth_reauth_proxy_override.sql) |
| A | migration | 22 | 0 | 01 | [backend/migrations/257_openai_oauth_reauth_proxy_source.sql](../../../backend/migrations/257_openai_oauth_reauth_proxy_source.sql) |
| A | migration | 8 | 0 | 01 | [backend/migrations/258_security_auth_lifecycle.sql](../../../backend/migrations/258_security_auth_lifecycle.sql) |
| A | migration | 23 | 0 | 01 | [backend/migrations/259_security_gateway_media.sql](../../../backend/migrations/259_security_gateway_media.sql) |
| A | migration | 78 | 0 | 01 | [backend/migrations/260_security_payment_invariants.sql](../../../backend/migrations/260_security_payment_invariants.sql) |
| A | migration | 76 | 0 | 01 | [backend/migrations/261_model_catalog_registry.sql](../../../backend/migrations/261_model_catalog_registry.sql) |
| A | migration | 3 | 0 | 01 | [backend/migrations/262_model_catalog_global_sync_jobs.sql](../../../backend/migrations/262_model_catalog_global_sync_jobs.sql) |
| A | migration | 91 | 0 | 01 | [backend/migrations/263_model_catalog_candidates_only.sql](../../../backend/migrations/263_model_catalog_candidates_only.sql) |
| A | migration | 36 | 0 | 01 | [backend/migrations/264_codex_diagnostic_monitor.sql](../../../backend/migrations/264_codex_diagnostic_monitor.sql) |
| A | migration | 25 | 0 | 01 | [backend/migrations/265_prepare_bps_provider_constraints.sql](../../../backend/migrations/265_prepare_bps_provider_constraints.sql) |
| A | migration | 119 | 0 | 01 | [backend/migrations/265_remove_bps_protocols.sql](../../../backend/migrations/265_remove_bps_protocols.sql) |
| A | migration | 32 | 0 | 01 | [backend/migrations/266_remove_companion_account_extensions.sql](../../../backend/migrations/266_remove_companion_account_extensions.sql) |
| A | migration | 20 | 0 | 01 | [backend/migrations/267_preserve_fable_threshold.sql](../../../backend/migrations/267_preserve_fable_threshold.sql) |
| A | migration | 34 | 0 | 01 | [backend/migrations/267_unify_diagnostic_scheduled_tests.sql](../../../backend/migrations/267_unify_diagnostic_scheduled_tests.sql) |
| A | migration | 60 | 0 | 01 | [backend/migrations/268_retire_fork_gateway_runtime.sql](../../../backend/migrations/268_retire_fork_gateway_runtime.sql) |
| A | migration | 20 | 0 | 01 | [backend/migrations/269_restore_fable_threshold.sql](../../../backend/migrations/269_restore_fable_threshold.sql) |
| A | migration | 43 | 0 | 01 | [backend/migrations/270_three_tier_roles.sql](../../../backend/migrations/270_three_tier_roles.sql) |
| A | migration | 9 | 0 | 01 | [backend/migrations/271_finalize_platform_catalog_constraints.sql](../../../backend/migrations/271_finalize_platform_catalog_constraints.sql) |
| A | migration | 7 | 0 | 01,07 | [backend/migrations/272_excel_bps_grants.sql](../../../backend/migrations/272_excel_bps_grants.sql) |
| A | test | 22 | 0 | 01 | [backend/migrations/codex_ticket_oailb_invalidation_migration_test.go](../../../backend/migrations/codex_ticket_oailb_invalidation_migration_test.go) |
| A | test | 21 | 0 | 01 | [backend/migrations/group_no_account_fallback_migration_test.go](../../../backend/migrations/group_no_account_fallback_migration_test.go) |
| A | test | 22 | 0 | 01 | [backend/migrations/openai_bps_platform_migration_test.go](../../../backend/migrations/openai_bps_platform_migration_test.go) |
| A | test | 18 | 0 | 01 | [backend/migrations/ops_job_heartbeats_expected_interval_migration_test.go](../../../backend/migrations/ops_job_heartbeats_expected_interval_migration_test.go) |
| A | test | 20 | 0 | 01 | [backend/migrations/ops_metric_thresholds_ttft_default_migration_test.go](../../../backend/migrations/ops_metric_thresholds_ttft_default_migration_test.go) |
| M | test | 7 | 2 | 01 | [backend/migrations/platform_check_constraints_drop_migration_test.go](../../../backend/migrations/platform_check_constraints_drop_migration_test.go) |
| A | test | 27 | 0 | 01 | [backend/migrations/prune_openai_request_timezones_migration_test.go](../../../backend/migrations/prune_openai_request_timezones_migration_test.go) |
