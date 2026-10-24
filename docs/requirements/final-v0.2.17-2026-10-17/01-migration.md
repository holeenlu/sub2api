# 01 · 数据库升级、实体结构与历史数据保护

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`chore(schema): preserve final migrations and entity contracts`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. 保留既有已发布 SQL 文件、迁移顺序与校验字节；不因整理 Git 历史删除数据库中的旧表、列、索引或账务数据。新增角色迁移沿用 270_three_tier_roles.sql，旧 admin 升级为 super_admin。

2. 保留 265_prepare_bps_provider_constraints 对旧 BPS 数据的升级准备，及 267/268/269 对 Fable 阈值的暂存、旧运行时清理和恢复流程；不得重新开启已被停用的账号。

3. 保留会话撤销、支付约束、诊断记录、操作日志可见级别及定时任务发起身份的原迁移。Ent 生成代码与 schema 配套保留。

4. 历史表结构存在不代表对应业务仍运行。旧打票、退役 BPS 历史结构（当前恢复范围见第 07 组）、账号质量运营、请求抓取、独立模型目录等迁移作为升级遗留登记，不能写进当前功能宣传清单。

供应商实体、归属占用与结算的迁移 273/274 和配套 Ent 生成物归入第 12 组；旧迁移 265/267–270/272 的字节及历史兼容路径全部保留。重建不运行迁移，也不重置数据库。数据库已执行记录、旧表存在与当前运行功能分开描述。

## 入口与配置归属

backend/migrations、backend/ent；应用启动沿用原迁移流程，无新管理页面。

## 最终实现方案

1. 沿用现有 migrations 启动器、历史账本和 Ent schema/生成物；不调整已发布 SQL 的执行顺序与校验字节。

2. 265 准备迁移处理历史 BPS 约束表达式；267/268/269 保留 Fable 转存/退役清理/恢复；270 角色、271 最终平台约束、272 Excel grant 与最终树一起保留。

3. 只保留历史表/列不恢复运行消费者；账号明确停用状态不由历史整理重新激活。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

对所有迁移/Ent 逐 blob 和 file mode 比较；go test -tags=unit ./migrations ./ent/...；真实数据库升级另行验收。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| generated | 8 | 42528 | 33078 | 0 |
| source | 44 | 1105 | 0 | 0 |
| test | 5 | 103 | 2 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 10 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | generated | 352 | 37 | 01,12 | [backend/ent/migrate/schema.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/migrate/schema.go) |
| M | generated | 41736 | 32970 | 01,12 | [backend/ent/mutation.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/mutation.go) |
| M | generated | 234 | 70 | 01,12 | [backend/ent/runtime/runtime.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/runtime/runtime.go) |
| M | source | 1 | 0 | 01 | [backend/ent/schema/pending_auth_session.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/schema/pending_auth_session.go) |
| M | source | 1 | 0 | 01 | [backend/ent/schema/user.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/schema/user.go) |
| M | generated | 12 | 1 | 01 | [backend/ent/user.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/user.go) |
| M | generated | 10 | 0 | 01 | [backend/ent/user/user.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/user/user.go) |
| M | generated | 45 | 0 | 01 | [backend/ent/user/where.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/user/where.go) |
| M | generated | 85 | 0 | 01 | [backend/ent/user_create.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/user_create.go) |
| M | generated | 54 | 0 | 01 | [backend/ent/user_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/user_update.go) |
| M | test | 26 | 0 | 01 | [backend/internal/repository/platform_list_db_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/platform_list_db_integration_test.go) |
| A | source | 7 | 0 | 01 | [backend/migrations/234_ops_job_heartbeats_add_expected_interval.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/234_ops_job_heartbeats_add_expected_interval.sql) |
| A | source | 9 | 0 | 01 | [backend/migrations/235_group_no_account_fallback.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/235_group_no_account_fallback.sql) |
| A | source | 15 | 0 | 01 | [backend/migrations/236_ops_metric_thresholds_ttft_default.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/236_ops_metric_thresholds_ttft_default.sql) |
| A | source | 5 | 0 | 01 | [backend/migrations/237_add_api_key_concurrency_limit.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/237_add_api_key_concurrency_limit.sql) |
| A | source | 52 | 0 | 01 | [backend/migrations/239_codex_ticket_attempts.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/239_codex_ticket_attempts.sql) |
| A | source | 5 | 0 | 01 | [backend/migrations/241_add_account_group_rate_multiplier.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/241_add_account_group_rate_multiplier.sql) |
| A | source | 55 | 0 | 01 | [backend/migrations/241_codex_ticket_lifecycle.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/241_codex_ticket_lifecycle.sql) |
| A | source | 39 | 0 | 01 | [backend/migrations/242_prune_openai_request_timezones.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/242_prune_openai_request_timezones.sql) |
| A | source | 10 | 0 | 01 | [backend/migrations/242_request_timing_details.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/242_request_timing_details.sql) |
| A | source | 6 | 0 | 01 | [backend/migrations/243_codex_ticket_oailb_invalidation.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/243_codex_ticket_oailb_invalidation.sql) |
| A | source | 4 | 0 | 01 | [backend/migrations/243_pelican_scheduled_tests.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/243_pelican_scheduled_tests.sql) |
| A | source | 7 | 0 | 01 | [backend/migrations/244_account_group_allowed_models.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/244_account_group_allowed_models.sql) |
| A | source | 7 | 0 | 01 | [backend/migrations/244_disable_codex_ticket_harvesting.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/244_disable_codex_ticket_harvesting.sql) |
| A | source | 7 | 0 | 01 | [backend/migrations/245_user_group_denied_models.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/245_user_group_denied_models.sql) |
| A | source | 6 | 0 | 01 | [backend/migrations/246_group_stream_only.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/246_group_stream_only.sql) |
| A | source | 8 | 0 | 01 | [backend/migrations/247_account_quality_ops.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/247_account_quality_ops.sql) |
| A | source | 2 | 0 | 01 | [backend/migrations/248_account_quality_judgment.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/248_account_quality_judgment.sql) |
| A | source | 19 | 0 | 01 | [backend/migrations/249_account_ops_alerts.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/249_account_ops_alerts.sql) |
| A | source | 23 | 0 | 01 | [backend/migrations/249_pelican_showcase_items.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/249_pelican_showcase_items.sql) |
| A | source | 37 | 0 | 01 | [backend/migrations/250_account_token_guard.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/250_account_token_guard.sql) |
| A | source | 18 | 0 | 01 | [backend/migrations/251_request_captures.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/251_request_captures.sql) |
| A | source | 1 | 0 | 01 | [backend/migrations/252_user_observer_groups.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/252_user_observer_groups.sql) |
| A | source | 66 | 0 | 01 | [backend/migrations/253_retire_fork_features.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/253_retire_fork_features.sql) |
| A | source | 45 | 0 | 01 | [backend/migrations/254_openai_oauth_reauth.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/254_openai_oauth_reauth.sql) |
| A | source | 31 | 0 | 01 | [backend/migrations/254_retire_standalone_bps.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/254_retire_standalone_bps.sql) |
| A | source | 61 | 0 | 01 | [backend/migrations/255_account_token_guard_v2.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/255_account_token_guard_v2.sql) |
| A | source | 3 | 0 | 01 | [backend/migrations/256_openai_oauth_reauth_proxy_override.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/256_openai_oauth_reauth_proxy_override.sql) |
| A | source | 22 | 0 | 01 | [backend/migrations/257_openai_oauth_reauth_proxy_source.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/257_openai_oauth_reauth_proxy_source.sql) |
| A | source | 8 | 0 | 01 | [backend/migrations/258_security_auth_lifecycle.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/258_security_auth_lifecycle.sql) |
| A | source | 23 | 0 | 01 | [backend/migrations/259_security_gateway_media.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/259_security_gateway_media.sql) |
| A | source | 78 | 0 | 01 | [backend/migrations/260_security_payment_invariants.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/260_security_payment_invariants.sql) |
| A | source | 76 | 0 | 01 | [backend/migrations/261_model_catalog_registry.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/261_model_catalog_registry.sql) |
| A | source | 3 | 0 | 01 | [backend/migrations/262_model_catalog_global_sync_jobs.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/262_model_catalog_global_sync_jobs.sql) |
| A | source | 91 | 0 | 01 | [backend/migrations/263_model_catalog_candidates_only.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/263_model_catalog_candidates_only.sql) |
| A | source | 36 | 0 | 01 | [backend/migrations/264_codex_diagnostic_monitor.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/264_codex_diagnostic_monitor.sql) |
| A | source | 32 | 0 | 01 | [backend/migrations/266_remove_companion_account_extensions.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/266_remove_companion_account_extensions.sql) |
| A | source | 20 | 0 | 01 | [backend/migrations/267_preserve_fable_threshold.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/267_preserve_fable_threshold.sql) |
| A | source | 34 | 0 | 01 | [backend/migrations/267_unify_diagnostic_scheduled_tests.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/267_unify_diagnostic_scheduled_tests.sql) |
| A | source | 60 | 0 | 01 | [backend/migrations/268_retire_fork_gateway_runtime.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/268_retire_fork_gateway_runtime.sql) |
| A | source | 20 | 0 | 01 | [backend/migrations/269_restore_fable_threshold.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/269_restore_fable_threshold.sql) |
| A | source | 43 | 0 | 01 | [backend/migrations/270_three_tier_roles.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/270_three_tier_roles.sql) |
| A | source | 9 | 0 | 01 | [backend/migrations/271_finalize_platform_catalog_constraints.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/271_finalize_platform_catalog_constraints.sql) |
| A | test | 22 | 0 | 01 | [backend/migrations/codex_ticket_oailb_invalidation_migration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/codex_ticket_oailb_invalidation_migration_test.go) |
| A | test | 21 | 0 | 01 | [backend/migrations/group_no_account_fallback_migration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/group_no_account_fallback_migration_test.go) |
| M | test | 7 | 2 | 01 | [backend/migrations/platform_check_constraints_drop_migration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/platform_check_constraints_drop_migration_test.go) |
| A | test | 27 | 0 | 01 | [backend/migrations/prune_openai_request_timezones_migration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/prune_openai_request_timezones_migration_test.go) |
