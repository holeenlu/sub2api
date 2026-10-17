# 19 · 共享配置、路由、类型、依赖与装配

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`refactor(core): preserve shared routes configuration and wiring`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. 共用配置、settings 读写与公开投影、HTTP DTO、服务装配、路由注册、前端 API 类型及 store 统一保留最终版本。跨功能文件只提交一次，文件归属不等于该文件只实现一个需求。

2. 后端策略声明与真实注册路由匹配；新管理路由缺少声明应由覆盖测试发现，前端导航依据同一授权快照。公共功能开关仍由既有 feature flag/store 链路消费。

3. 前后端权限、模型、计费和配置校验复用现有实现；不因整理新建兼容层、独立规则表、接口别名或双写路径。SettingsView 等大型共享文件不作机械拆分，以免改变行为。

4. 保留已退休字段的拒绝写入和缓存代次；保留后端依赖/生成装配与前端 package/lock 文件匹配。插件 SDK 的原有协议与品牌示例依照实际文件差异归档，不宣称增加插件能力。

供应商路由、身份投影、后端生成装配、代理连接版本和前端门户构建已经在当前配置中接通。整个提交序列共用最终 config、DTO、settings、Wire、package/lock 和路由文件；一个文件只由一个组提交，其他组通过 related_groups 关联。分类用于审阅与追溯，不承诺每笔可独立 cherry-pick 或每个中间提交单独编译。

## 入口与配置归属

backend/internal/config、handler/dto、server/routes、service/setting*、wire；frontend SettingsView/router/stores/types/api。

## 最终实现方案

1. 共享 settings、config、DTO、wire、路由、前端 API/store/types 和大型设置页只写最终版本，防止按功能复制时覆盖彼此。

2. 路由/字段声明校验、缓存代次、退役字段拒绝和依赖锁文件与最终功能一起收敛。

3. 这一组与其他组有真实编译/装配依赖；完整提交集才是运行/部署单位，不承诺任意单个分类提交可独立 cherry-pick。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

最终后端 unit/构建、前端 typecheck/相关回归；完整最终树装配正确且源码对象一致。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 4 | 13 | 13 | 0 |
| generated | 1 | 20 | 0 | 0 |
| source | 65 | 2935 | 845 | 0 |
| test | 18 | 1186 | 170 | 0 |
| tooling | 4 | 22 | 23 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 49 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 63 | 5 | 19 | [backend/cmd/server/main.go](https://github.com/holeenlu/sub2api/blob/main/backend/cmd/server/main.go) |
| A | test | 25 | 0 | 19 | [backend/cmd/server/upstream_version_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/cmd/server/upstream_version_test.go) |
| M | source | 6 | 2 | 07,19 | [backend/cmd/server/wire.go](https://github.com/holeenlu/sub2api/blob/main/backend/cmd/server/wire.go) |
| M | source | 102 | 92 | 07,10,12,19 | [backend/cmd/server/wire_gen.go](https://github.com/holeenlu/sub2api/blob/main/backend/cmd/server/wire_gen.go) |
| M | tooling | 2 | 1 | 07,19 | [backend/go.mod](https://github.com/holeenlu/sub2api/blob/main/backend/go.mod) |
| M | tooling | 14 | 20 | 07,19 | [backend/go.sum](https://github.com/holeenlu/sub2api/blob/main/backend/go.sum) |
| M | source | 81 | 9 | 06,07,10,19 | [backend/internal/config/config.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/config/config.go) |
| M | test | 63 | 1 | 06,19 | [backend/internal/config/config_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/config/config_test.go) |
| M | source | 3 | 2 | 19 | [backend/internal/domain/constants.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/domain/constants.go) |
| A | source | 29 | 0 | 19 | [backend/internal/domain/credential_keys.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/domain/credential_keys.go) |
| M | test | 53 | 53 | 19 | [backend/internal/handler/admin/admin_basic_handlers_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/admin_basic_handlers_test.go) |
| M | source | 22 | 3 | 07,12,19 | [backend/internal/handler/admin/setting_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/setting_handler.go) |
| M | source | 0 | 3 | 19 | [backend/internal/handler/admin/setting_handler_audit.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/setting_handler_audit.go) |
| M | source | 26 | 0 | 19 | [backend/internal/handler/admin/setting_handler_email.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/setting_handler_email.go) |
| M | source | 4 | 0 | 19 | [backend/internal/handler/admin/setting_handler_runtime.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/setting_handler_runtime.go) |
| M | test | 71 | 0 | 19 | [backend/internal/handler/admin/setting_handler_stepup_switch_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/setting_handler_stepup_switch_test.go) |
| M | source | 211 | 14 | 07,12,19 | [backend/internal/handler/admin/setting_handler_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/setting_handler_update.go) |
| M | source | 22 | 5 | 19 | [backend/internal/handler/admin/system_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/system_handler.go) |
| M | source | 9 | 4 | 12,19 | [backend/internal/handler/dto/mappers.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/dto/mappers.go) |
| M | source | 44 | 24 | 07,12,19 | [backend/internal/handler/dto/settings.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/dto/settings.go) |
| M | source | 15 | 2 | 12,19 | [backend/internal/handler/dto/types.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/dto/types.go) |
| M | source | 9 | 2 | 12,19 | [backend/internal/handler/handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/handler.go) |
| M | source | 19 | 5 | 06,07,19 | [backend/internal/handler/openai_chat_completions.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_chat_completions.go) |
| M | test | 359 | 33 | 06,07,19 | [backend/internal/handler/openai_gateway_handler_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_gateway_handler_test.go) |
| M | test | 77 | 20 | 06,07,19 | [backend/internal/handler/openai_gateway_ws_group_pricing_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_gateway_ws_group_pricing_test.go) |
| M | source | 1 | 1 | 19 | [backend/internal/handler/page_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/page_handler.go) |
| M | source | 2 | 1 | 12,19 | [backend/internal/handler/setting_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/setting_handler.go) |
| M | source | 13 | 0 | 12,19 | [backend/internal/handler/wire.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/wire.go) |
| M | source | 30 | 3 | 19 | [backend/internal/pkg/response/response.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/response/response.go) |
| M | source | 179 | 35 | 06,07,12,14,19 | [backend/internal/repository/http_upstream.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/http_upstream.go) |
| M | test | 5 | 3 | 19 | [backend/internal/repository/integration_harness_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/integration_harness_test.go) |
| M | test | 61 | 0 | 19 | [backend/internal/repository/migrations_schema_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/migrations_schema_integration_test.go) |
| M | source | 10 | 0 | 19 | [backend/internal/repository/setting_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/setting_repo.go) |
| M | test | 101 | 10 | 07,10,12,19 | [backend/internal/server/api_contract_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/api_contract_test.go) |
| A | test | 43 | 0 | 12,19 | [backend/internal/server/management_route_coverage_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/management_route_coverage_test.go) |
| M | source | 4 | 1 | 12,19 | [backend/internal/server/router.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/router.go) |
| M | source | 34 | 16 | 07,10,12,19 | [backend/internal/server/routes/admin.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/routes/admin.go) |
| M | source | 3 | 0 | 19 | [backend/internal/server/routes/payment.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/routes/payment.go) |
| M | source | 1 | 1 | 19 | [backend/internal/service/api_key_auth_cache_impl.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/api_key_auth_cache_impl.go) |
| M | source | 9 | 0 | 19 | [backend/internal/service/api_key_auth_cache_invalidate.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/api_key_auth_cache_invalidate.go) |
| M | source | 17 | 3 | 19 | [backend/internal/service/domain_constants.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/domain_constants.go) |
| M | source | 26 | 16 | 06,08,12,19 | [backend/internal/service/gateway_forward.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_forward.go) |
| M | source | 15 | 2 | 06,07,19 | [backend/internal/service/http_upstream_profile.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/http_upstream_profile.go) |
| M | source | 31 | 17 | 05,07,19 | [backend/internal/service/openai_codex_models_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_codex_models_service.go) |
| M | source | 66 | 146 | 06,07,12,19 | [backend/internal/service/openai_gateway_forward.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_forward.go) |
| M | source | 28 | 6 | 05,07,19 | [backend/internal/service/openai_models_list.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_models_list.go) |
| M | source | 178 | 2 | 06,07,19 | [backend/internal/service/openai_ws_forwarder.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_forwarder.go) |
| M | source | 444 | 59 | 06,07,12,19 | [backend/internal/service/openai_ws_http_bridge.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_http_bridge.go) |
| M | source | 15 | 1 | 06,07,19 | [backend/internal/service/openai_ws_session_preemption.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_session_preemption.go) |
| M | source | 39 | 23 | 19 | [backend/internal/service/setting_features.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_features.go) |
| M | source | 59 | 5 | 07,12,19 | [backend/internal/service/setting_parse.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_parse.go) |
| M | source | 7 | 4 | 12,19 | [backend/internal/service/setting_public.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_public.go) |
| M | source | 5 | 7 | 19 | [backend/internal/service/setting_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_service.go) |
| M | test | 15 | 13 | 19 | [backend/internal/service/setting_service_platform_threshold_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_service_platform_threshold_test.go) |
| M | test | 36 | 4 | 19 | [backend/internal/service/setting_service_update_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_service_update_test.go) |
| M | source | 172 | 6 | 07,12,19 | [backend/internal/service/setting_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_update.go) |
| M | source | 22 | 3 | 07,12,19 | [backend/internal/service/settings_view.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/settings_view.go) |
| M | source | 47 | 11 | 07,10,12,19 | [backend/internal/service/wire.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/wire.go) |
| M | source | 9 | 0 | 19 | [backend/internal/testutil/stubs.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/testutil/stubs.go) |
| A | source | 13 | 0 | 19 | [backend/internal/testutil/transport.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/testutil/transport.go) |
| M | documentation | 9 | 9 | 19 | [backend/pkg/pluginapi/README.md](https://github.com/holeenlu/sub2api/blob/main/backend/pkg/pluginapi/README.md) |
| M | documentation | 1 | 1 | 19 | [backend/pkg/pluginapi/docs/development.md](https://github.com/holeenlu/sub2api/blob/main/backend/pkg/pluginapi/docs/development.md) |
| M | documentation | 1 | 1 | 19 | [backend/pkg/pluginapi/docs/package-format.md](https://github.com/holeenlu/sub2api/blob/main/backend/pkg/pluginapi/docs/package-format.md) |
| M | documentation | 2 | 2 | 19 | [backend/pkg/pluginapi/docs/security.md](https://github.com/holeenlu/sub2api/blob/main/backend/pkg/pluginapi/docs/security.md) |
| M | tooling | 1 | 1 | 19 | [backend/pkg/pluginapi/v1/manifest.schema.json](https://github.com/holeenlu/sub2api/blob/main/backend/pkg/pluginapi/v1/manifest.schema.json) |
| M | tooling | 5 | 1 | 12,19 | [frontend/package.json](https://github.com/holeenlu/sub2api/blob/main/frontend/package.json) |
| M | generated | 20 | 0 | 19 | [frontend/pnpm-lock.yaml](https://github.com/holeenlu/sub2api/blob/main/frontend/pnpm-lock.yaml) |
| M | source | 21 | 10 | 12,19 | [frontend/src/App.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/App.vue) |
| M | test | 1 | 0 | 19 | [frontend/src/api/__tests__/settings.authSourceDefaults.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/__tests__/settings.authSourceDefaults.spec.ts) |
| M | test | 6 | 12 | 19 | [frontend/src/api/__tests__/settings.paymentVisibleMethods.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/__tests__/settings.paymentVisibleMethods.spec.ts) |
| M | source | 68 | 26 | 07,12,19 | [frontend/src/api/admin/settings.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/admin/settings.ts) |
| M | source | 16 | 2 | 19 | [frontend/src/api/admin/system.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/admin/system.ts) |
| M | source | 12 | 6 | 19 | [frontend/src/api/setup.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/setup.ts) |
| M | source | 2 | 2 | 19 | [frontend/src/composables/useOnboardingTour.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/composables/useOnboardingTour.ts) |
| M | source | 4 | 3 | 12,19 | [frontend/src/main.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/main.ts) |
| M | source | 1 | 1 | 19 | [frontend/src/router/README.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/router/README.md) |
| M | test | 16 | 0 | 19 | [frontend/src/router/__tests__/feature-access.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/router/__tests__/feature-access.spec.ts) |
| M | test | 105 | 12 | 19 | [frontend/src/router/__tests__/title.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/router/__tests__/title.spec.ts) |
| M | source | 94 | 13 | 12,19 | [frontend/src/router/index.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/router/index.ts) |
| M | source | 12 | 0 | 19 | [frontend/src/router/meta.d.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/router/meta.d.ts) |
| M | source | 43 | 4 | 19 | [frontend/src/router/title.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/router/title.ts) |
| M | source | 1 | 1 | 19 | [frontend/src/stores/README.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/stores/README.md) |
| M | test | 0 | 2 | 19 | [frontend/src/stores/__tests__/app.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/stores/__tests__/app.spec.ts) |
| M | source | 15 | 0 | 19 | [frontend/src/stores/adminSettings.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/stores/adminSettings.ts) |
| M | source | 38 | 5 | 19 | [frontend/src/stores/app.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/stores/app.ts) |
| M | source | 33 | 4 | 12,19 | [frontend/src/types/index.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/types/index.ts) |
| M | source | 414 | 221 | 07,12,19 | [frontend/src/views/admin/SettingsView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/SettingsView.vue) |
| M | test | 149 | 7 | 19 | [frontend/src/views/admin/__tests__/SettingsView.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/SettingsView.spec.ts) |
| M | source | 1 | 1 | 19 | [frontend/src/views/auth/README.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/auth/README.md) |
| M | source | 4 | 4 | 19 | [frontend/src/views/auth/VISUAL_GUIDE.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/auth/VISUAL_GUIDE.md) |
| M | source | 7 | 1 | 19 | [frontend/vite.config.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/vite.config.ts) |
| M | source | 5 | 0 | 19 | [frontend/vitest.config.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/vitest.config.ts) |
