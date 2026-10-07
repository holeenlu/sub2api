# 01 · 数据库升级与历史兼容

归档日期：2026-10-07。文件快照：main `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238`；官方比较基线 `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`。

本页登记最终需求与实际文件归属；不把本轮提交整理等同于重新实现或完整安全审计。

## 最终需求

1. 保留既有已发布 SQL 文件、迁移顺序与校验字节；不因整理 Git 历史删除数据库中的旧表、列、索引或账务数据。新增角色迁移沿用 270_three_tier_roles.sql，旧 admin 升级为 super_admin。

2. 保留 265_prepare_bps_provider_constraints 对旧 BPS 数据的升级准备，及 267/268/269 对 Fable 阈值的暂存、旧运行时清理和恢复流程；不得重新开启已被停用的账号。

3. 保留会话撤销、支付约束、诊断记录、操作日志可见级别及定时任务发起身份的原迁移。Ent 生成代码与 schema 配套保留。

4. 历史表结构存在不代表对应业务仍运行。旧打票、BPS、账号质量运营、请求抓取、独立模型目录等迁移作为升级遗留登记，不能写进当前功能宣传清单。

## 入口与配置归属

backend/migrations、backend/ent；应用启动沿用原迁移流程，无新管理页面。

## 验收边界

重组前后所有既有 migrations 与 Ent 文件对象一致；没有新增迁移、DROP、线上迁移或数据回填。真实 PostgreSQL/Redis 升级测试的历史结果仅适用于原记录的版本，不能外推到未测试的生产库。

验证状态统一见 [总说明](README.md#验证和未关闭边界)。本页列的是验收要求，不是宣称本轮已重新运行这些检查。

## 对比统计

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 生成代码/繁体 | 10 | 317 | 18 | 0 |
| SQL 迁移 | 45 | 1262 | 0 | 0 |
| 测试/夹具 | 6 | 130 | 0 | 0 |

统计为最终文件相对固定官方基线的累计差异，并非本次整理新写的代码。对重命名统一展开为删除/新增；跨域文件只设一个主归属。

## 修改文件

| 状态 | 类型 | +行 | −行 | 文件 |
| --- | --- | ---: | ---: | --- |
| M | 生成代码/繁体 | 2 | 1 | [backend/ent/migrate/schema.go](../../../backend/ent/migrate/schema.go) |
| M | 生成代码/繁体 | 88 | 1 | [backend/ent/mutation.go](../../../backend/ent/mutation.go) |
| M | 生成代码/繁体 | 19 | 15 | [backend/ent/runtime/runtime.go](../../../backend/ent/runtime/runtime.go) |
| M | 生成代码/繁体 | 1 | 0 | [backend/ent/schema/pending_auth_session.go](../../../backend/ent/schema/pending_auth_session.go) |
| M | 生成代码/繁体 | 1 | 0 | [backend/ent/schema/user.go](../../../backend/ent/schema/user.go) |
| M | 生成代码/繁体 | 12 | 1 | [backend/ent/user.go](../../../backend/ent/user.go) |
| M | 生成代码/繁体 | 10 | 0 | [backend/ent/user/user.go](../../../backend/ent/user/user.go) |
| M | 生成代码/繁体 | 45 | 0 | [backend/ent/user/where.go](../../../backend/ent/user/where.go) |
| M | 生成代码/繁体 | 85 | 0 | [backend/ent/user_create.go](../../../backend/ent/user_create.go) |
| M | 生成代码/繁体 | 54 | 0 | [backend/ent/user_update.go](../../../backend/ent/user_update.go) |
| A | SQL 迁移 | 7 | 0 | [backend/migrations/234_ops_job_heartbeats_add_expected_interval.sql](../../../backend/migrations/234_ops_job_heartbeats_add_expected_interval.sql) |
| A | SQL 迁移 | 9 | 0 | [backend/migrations/235_group_no_account_fallback.sql](../../../backend/migrations/235_group_no_account_fallback.sql) |
| A | SQL 迁移 | 15 | 0 | [backend/migrations/236_ops_metric_thresholds_ttft_default.sql](../../../backend/migrations/236_ops_metric_thresholds_ttft_default.sql) |
| A | SQL 迁移 | 5 | 0 | [backend/migrations/237_add_api_key_concurrency_limit.sql](../../../backend/migrations/237_add_api_key_concurrency_limit.sql) |
| A | SQL 迁移 | 52 | 0 | [backend/migrations/239_codex_ticket_attempts.sql](../../../backend/migrations/239_codex_ticket_attempts.sql) |
| A | SQL 迁移 | 5 | 0 | [backend/migrations/241_add_account_group_rate_multiplier.sql](../../../backend/migrations/241_add_account_group_rate_multiplier.sql) |
| A | SQL 迁移 | 55 | 0 | [backend/migrations/241_codex_ticket_lifecycle.sql](../../../backend/migrations/241_codex_ticket_lifecycle.sql) |
| A | SQL 迁移 | 39 | 0 | [backend/migrations/242_prune_openai_request_timezones.sql](../../../backend/migrations/242_prune_openai_request_timezones.sql) |
| A | SQL 迁移 | 10 | 0 | [backend/migrations/242_request_timing_details.sql](../../../backend/migrations/242_request_timing_details.sql) |
| A | SQL 迁移 | 6 | 0 | [backend/migrations/243_codex_ticket_oailb_invalidation.sql](../../../backend/migrations/243_codex_ticket_oailb_invalidation.sql) |
| A | SQL 迁移 | 4 | 0 | [backend/migrations/243_pelican_scheduled_tests.sql](../../../backend/migrations/243_pelican_scheduled_tests.sql) |
| A | SQL 迁移 | 7 | 0 | [backend/migrations/244_account_group_allowed_models.sql](../../../backend/migrations/244_account_group_allowed_models.sql) |
| A | SQL 迁移 | 7 | 0 | [backend/migrations/244_disable_codex_ticket_harvesting.sql](../../../backend/migrations/244_disable_codex_ticket_harvesting.sql) |
| A | SQL 迁移 | 17 | 0 | [backend/migrations/245_openai_bps_platform.sql](../../../backend/migrations/245_openai_bps_platform.sql) |
| A | SQL 迁移 | 7 | 0 | [backend/migrations/245_user_group_denied_models.sql](../../../backend/migrations/245_user_group_denied_models.sql) |
| A | SQL 迁移 | 6 | 0 | [backend/migrations/246_group_stream_only.sql](../../../backend/migrations/246_group_stream_only.sql) |
| A | SQL 迁移 | 8 | 0 | [backend/migrations/247_account_quality_ops.sql](../../../backend/migrations/247_account_quality_ops.sql) |
| A | SQL 迁移 | 2 | 0 | [backend/migrations/248_account_quality_judgment.sql](../../../backend/migrations/248_account_quality_judgment.sql) |
| A | SQL 迁移 | 19 | 0 | [backend/migrations/249_account_ops_alerts.sql](../../../backend/migrations/249_account_ops_alerts.sql) |
| A | SQL 迁移 | 23 | 0 | [backend/migrations/249_pelican_showcase_items.sql](../../../backend/migrations/249_pelican_showcase_items.sql) |
| A | SQL 迁移 | 37 | 0 | [backend/migrations/250_account_token_guard.sql](../../../backend/migrations/250_account_token_guard.sql) |
| A | SQL 迁移 | 18 | 0 | [backend/migrations/251_request_captures.sql](../../../backend/migrations/251_request_captures.sql) |
| A | SQL 迁移 | 1 | 0 | [backend/migrations/252_user_observer_groups.sql](../../../backend/migrations/252_user_observer_groups.sql) |
| A | SQL 迁移 | 66 | 0 | [backend/migrations/253_retire_fork_features.sql](../../../backend/migrations/253_retire_fork_features.sql) |
| A | SQL 迁移 | 45 | 0 | [backend/migrations/254_openai_oauth_reauth.sql](../../../backend/migrations/254_openai_oauth_reauth.sql) |
| A | SQL 迁移 | 7 | 0 | [backend/migrations/254_quality_bps_coexist.sql](../../../backend/migrations/254_quality_bps_coexist.sql) |
| A | SQL 迁移 | 31 | 0 | [backend/migrations/254_retire_standalone_bps.sql](../../../backend/migrations/254_retire_standalone_bps.sql) |
| A | SQL 迁移 | 61 | 0 | [backend/migrations/255_account_token_guard_v2.sql](../../../backend/migrations/255_account_token_guard_v2.sql) |
| A | SQL 迁移 | 3 | 0 | [backend/migrations/256_openai_oauth_reauth_proxy_override.sql](../../../backend/migrations/256_openai_oauth_reauth_proxy_override.sql) |
| A | SQL 迁移 | 22 | 0 | [backend/migrations/257_openai_oauth_reauth_proxy_source.sql](../../../backend/migrations/257_openai_oauth_reauth_proxy_source.sql) |
| A | SQL 迁移 | 8 | 0 | [backend/migrations/258_security_auth_lifecycle.sql](../../../backend/migrations/258_security_auth_lifecycle.sql) |
| A | SQL 迁移 | 23 | 0 | [backend/migrations/259_security_gateway_media.sql](../../../backend/migrations/259_security_gateway_media.sql) |
| A | SQL 迁移 | 78 | 0 | [backend/migrations/260_security_payment_invariants.sql](../../../backend/migrations/260_security_payment_invariants.sql) |
| A | SQL 迁移 | 76 | 0 | [backend/migrations/261_model_catalog_registry.sql](../../../backend/migrations/261_model_catalog_registry.sql) |
| A | SQL 迁移 | 3 | 0 | [backend/migrations/262_model_catalog_global_sync_jobs.sql](../../../backend/migrations/262_model_catalog_global_sync_jobs.sql) |
| A | SQL 迁移 | 91 | 0 | [backend/migrations/263_model_catalog_candidates_only.sql](../../../backend/migrations/263_model_catalog_candidates_only.sql) |
| A | SQL 迁移 | 36 | 0 | [backend/migrations/264_codex_diagnostic_monitor.sql](../../../backend/migrations/264_codex_diagnostic_monitor.sql) |
| A | SQL 迁移 | 25 | 0 | [backend/migrations/265_prepare_bps_provider_constraints.sql](../../../backend/migrations/265_prepare_bps_provider_constraints.sql) |
| A | SQL 迁移 | 119 | 0 | [backend/migrations/265_remove_bps_protocols.sql](../../../backend/migrations/265_remove_bps_protocols.sql) |
| A | SQL 迁移 | 32 | 0 | [backend/migrations/266_remove_companion_account_extensions.sql](../../../backend/migrations/266_remove_companion_account_extensions.sql) |
| A | SQL 迁移 | 20 | 0 | [backend/migrations/267_preserve_fable_threshold.sql](../../../backend/migrations/267_preserve_fable_threshold.sql) |
| A | SQL 迁移 | 34 | 0 | [backend/migrations/267_unify_diagnostic_scheduled_tests.sql](../../../backend/migrations/267_unify_diagnostic_scheduled_tests.sql) |
| A | SQL 迁移 | 60 | 0 | [backend/migrations/268_retire_fork_gateway_runtime.sql](../../../backend/migrations/268_retire_fork_gateway_runtime.sql) |
| A | SQL 迁移 | 20 | 0 | [backend/migrations/269_restore_fable_threshold.sql](../../../backend/migrations/269_restore_fable_threshold.sql) |
| A | SQL 迁移 | 43 | 0 | [backend/migrations/270_three_tier_roles.sql](../../../backend/migrations/270_three_tier_roles.sql) |
| A | 测试/夹具 | 22 | 0 | [backend/migrations/codex_ticket_oailb_invalidation_migration_test.go](../../../backend/migrations/codex_ticket_oailb_invalidation_migration_test.go) |
| A | 测试/夹具 | 21 | 0 | [backend/migrations/group_no_account_fallback_migration_test.go](../../../backend/migrations/group_no_account_fallback_migration_test.go) |
| A | 测试/夹具 | 22 | 0 | [backend/migrations/openai_bps_platform_migration_test.go](../../../backend/migrations/openai_bps_platform_migration_test.go) |
| A | 测试/夹具 | 18 | 0 | [backend/migrations/ops_job_heartbeats_expected_interval_migration_test.go](../../../backend/migrations/ops_job_heartbeats_expected_interval_migration_test.go) |
| A | 测试/夹具 | 20 | 0 | [backend/migrations/ops_metric_thresholds_ttft_default_migration_test.go](../../../backend/migrations/ops_metric_thresholds_ttft_default_migration_test.go) |
| A | 测试/夹具 | 27 | 0 | [backend/migrations/prune_openai_request_timezones_migration_test.go](../../../backend/migrations/prune_openai_request_timezones_migration_test.go) |
