# 16 · 跨功能配置、路由、类型与依赖装配

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。


2026-10-14 增量归并：本页已纳入通过验收的 New API 实现，来源和逐文件归属见 [归并分析](NEW-API-INTEGRATION-2026-10-14.md)。原快照 SHA 仅代表 2026-10-13 历史输入。

## 最终需求

1. 共用配置、settings 读写与公开投影、HTTP DTO、服务装配、路由注册、前端 API 类型及 store 统一保留最终版本。跨功能文件只提交一次，文件归属不等于该文件只实现一个需求。

2. 后端策略声明与真实注册路由匹配；新管理路由缺少声明应由覆盖测试发现，前端导航依据同一授权快照。公共功能开关仍由既有 feature flag/store 链路消费。

3. 前后端权限、模型、计费和配置校验复用现有实现；不因整理新建兼容层、独立规则表、接口别名或双写路径。SettingsView 等大型共享文件不作机械拆分，以免改变行为。

4. 保留已退休字段的拒绝写入和缓存代次；保留后端依赖/生成装配与前端 package/lock 文件匹配。插件 SDK 的原有协议与品牌示例依照实际文件差异归档，不宣称增加插件能力。

## 入口与配置归属

backend/internal/config、handler/dto、server/routes、service/setting*、wire；frontend SettingsView/router/stores/types/api。

## 最终实现方案

1. 共享 settings、config、DTO、wire、路由、前端 API/store/types 和大型设置页只写最终版本，防止按功能复制时覆盖彼此。

2. 路由/字段声明校验、缓存代次、退役字段拒绝和依赖锁文件与最终功能一起收敛。

3. 这一组与其他组有真实编译/装配依赖；完整提交集才是运行/部署单位，不承诺任意单个分类提交可独立 cherry-pick。

## 边界、失败路径与依赖

关联功能组：05, 06, 07, 08。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。此组与关联组共同形成完整运行行为；本次归并不额外改写已验收的业务源码。

## 验收要求

最终后端 unit/构建、前端 typecheck/相关回归；完整最终树装配正确且源码对象一致。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 8 | 20 | 20 | 0 |
| generated | 1 | 26 | 21 | 0 |
| source | 61 | 2591 | 691 | 0 |
| test | 18 | 1057 | 150 | 0 |
| tooling | 5 | 41 | 23 | 0 |

相对官方 v0.2.16 的累计差异，已包含本批实现；不是本次归并额外编写的代码。

## 来源提交

- `3bd5cd406dd37b332f01a446718caca3a8a4222f`
- `49b29ef158e6319cfd6b1227781307de5f8f870e`
- `4b2cfde513867c40dbcc663a5ddea30cdd286dd2`
- `4c5aa7b49988f5752b01940475530832c188b7ad`
- `6dc1165380ed5f93a5f63729eb5c3b8421d61126`
- `71fc12f8edf4cbb839a72ba5f06c86745a1f2612`
- `944fdb1eff8e7ea33ed70ff722b6822f962e9255`
- `a2988a14ca54b99b09a6f64024166d52fa8db132`
- `bb7c4d0a169b5ba3dbd5aa3cec957d52170d0265`
- `be0749922538fe576b5e7329609e4f1d7716699f`
- `c6a92e1501a3ed5bb405c02bd541fcb59366bd28`
- `e37f32177a2b3f31414e8e2801d1f20678983fe5`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 63 | 5 | 16 | [backend/cmd/server/main.go](../../../backend/cmd/server/main.go) |
| A | test | 25 | 0 | 16 | [backend/cmd/server/upstream_version_test.go](../../../backend/cmd/server/upstream_version_test.go) |
| M | source | 6 | 2 | 07,16 | [backend/cmd/server/wire.go](../../../backend/cmd/server/wire.go) |
| M | generated | 26 | 21 | 07,10,16 | [backend/cmd/server/wire_gen.go](../../../backend/cmd/server/wire_gen.go) |
| M | tooling | 2 | 1 | 07,16 | [backend/go.mod](../../../backend/go.mod) |
| M | tooling | 14 | 20 | 07,16 | [backend/go.sum](../../../backend/go.sum) |
| M | source | 81 | 9 | 06,07,10,16 | [backend/internal/config/config.go](../../../backend/internal/config/config.go) |
| M | test | 63 | 1 | 06,16 | [backend/internal/config/config_test.go](../../../backend/internal/config/config_test.go) |
| M | source | 3 | 2 | 16 | [backend/internal/domain/constants.go](../../../backend/internal/domain/constants.go) |
| A | source | 29 | 0 | 16 | [backend/internal/domain/credential_keys.go](../../../backend/internal/domain/credential_keys.go) |
| M | test | 53 | 53 | 16 | [backend/internal/handler/admin/admin_basic_handlers_test.go](../../../backend/internal/handler/admin/admin_basic_handlers_test.go) |
| M | source | 20 | 3 | 07,16 | [backend/internal/handler/admin/setting_handler.go](../../../backend/internal/handler/admin/setting_handler.go) |
| M | source | 0 | 3 | 16 | [backend/internal/handler/admin/setting_handler_audit.go](../../../backend/internal/handler/admin/setting_handler_audit.go) |
| M | source | 26 | 0 | 16 | [backend/internal/handler/admin/setting_handler_email.go](../../../backend/internal/handler/admin/setting_handler_email.go) |
| M | source | 4 | 0 | 16 | [backend/internal/handler/admin/setting_handler_runtime.go](../../../backend/internal/handler/admin/setting_handler_runtime.go) |
| M | test | 71 | 0 | 16 | [backend/internal/handler/admin/setting_handler_stepup_switch_test.go](../../../backend/internal/handler/admin/setting_handler_stepup_switch_test.go) |
| M | source | 190 | 11 | 07,16 | [backend/internal/handler/admin/setting_handler_update.go](../../../backend/internal/handler/admin/setting_handler_update.go) |
| M | source | 22 | 5 | 16 | [backend/internal/handler/admin/system_handler.go](../../../backend/internal/handler/admin/system_handler.go) |
| M | source | 7 | 4 | 16 | [backend/internal/handler/dto/mappers.go](../../../backend/internal/handler/dto/mappers.go) |
| M | source | 40 | 24 | 07,16 | [backend/internal/handler/dto/settings.go](../../../backend/internal/handler/dto/settings.go) |
| M | source | 7 | 2 | 16 | [backend/internal/handler/dto/types.go](../../../backend/internal/handler/dto/types.go) |
| M | source | 4 | 2 | 16 | [backend/internal/handler/handler.go](../../../backend/internal/handler/handler.go) |
| M | source | 18 | 4 | 06,07,16 | [backend/internal/handler/openai_chat_completions.go](../../../backend/internal/handler/openai_chat_completions.go) |
| M | test | 343 | 17 | 06,07,16 | [backend/internal/handler/openai_gateway_handler_test.go](../../../backend/internal/handler/openai_gateway_handler_test.go) |
| M | test | 75 | 18 | 06,07,16 | [backend/internal/handler/openai_gateway_ws_group_pricing_test.go](../../../backend/internal/handler/openai_gateway_ws_group_pricing_test.go) |
| M | source | 1 | 1 | 16 | [backend/internal/handler/page_handler.go](../../../backend/internal/handler/page_handler.go) |
| M | source | 0 | 1 | 16 | [backend/internal/handler/setting_handler.go](../../../backend/internal/handler/setting_handler.go) |
| M | source | 3 | 0 | 16 | [backend/internal/handler/wire.go](../../../backend/internal/handler/wire.go) |
| M | source | 30 | 3 | 16 | [backend/internal/pkg/response/response.go](../../../backend/internal/pkg/response/response.go) |
| M | source | 134 | 24 | 06,07,16 | [backend/internal/repository/http_upstream.go](../../../backend/internal/repository/http_upstream.go) |
| M | test | 5 | 3 | 16 | [backend/internal/repository/integration_harness_test.go](../../../backend/internal/repository/integration_harness_test.go) |
| M | test | 61 | 0 | 16 | [backend/internal/repository/migrations_schema_integration_test.go](../../../backend/internal/repository/migrations_schema_integration_test.go) |
| M | source | 10 | 0 | 16 | [backend/internal/repository/setting_repo.go](../../../backend/internal/repository/setting_repo.go) |
| M | test | 95 | 8 | 07,10,16 | [backend/internal/server/api_contract_test.go](../../../backend/internal/server/api_contract_test.go) |
| A | test | 43 | 0 | 16 | [backend/internal/server/management_route_coverage_test.go](../../../backend/internal/server/management_route_coverage_test.go) |
| M | source | 3 | 1 | 16 | [backend/internal/server/router.go](../../../backend/internal/server/router.go) |
| M | source | 17 | 1 | 07,10,16 | [backend/internal/server/routes/admin.go](../../../backend/internal/server/routes/admin.go) |
| M | source | 3 | 0 | 16 | [backend/internal/server/routes/payment.go](../../../backend/internal/server/routes/payment.go) |
| M | source | 1 | 1 | 16 | [backend/internal/service/api_key_auth_cache_impl.go](../../../backend/internal/service/api_key_auth_cache_impl.go) |
| M | source | 9 | 0 | 16 | [backend/internal/service/api_key_auth_cache_invalidate.go](../../../backend/internal/service/api_key_auth_cache_invalidate.go) |
| M | source | 17 | 3 | 16 | [backend/internal/service/domain_constants.go](../../../backend/internal/service/domain_constants.go) |
| M | source | 19 | 12 | 06,08,16 | [backend/internal/service/gateway_forward.go](../../../backend/internal/service/gateway_forward.go) |
| M | source | 15 | 2 | 06,07,16 | [backend/internal/service/http_upstream_profile.go](../../../backend/internal/service/http_upstream_profile.go) |
| M | source | 35 | 17 | 05,07,16 | [backend/internal/service/openai_codex_models_service.go](../../../backend/internal/service/openai_codex_models_service.go) |
| M | source | 30 | 134 | 06,07,16 | [backend/internal/service/openai_gateway_forward.go](../../../backend/internal/service/openai_gateway_forward.go) |
| M | source | 28 | 6 | 05,07,16 | [backend/internal/service/openai_models_list.go](../../../backend/internal/service/openai_models_list.go) |
| M | source | 175 | 1 | 06,07,16 | [backend/internal/service/openai_ws_forwarder.go](../../../backend/internal/service/openai_ws_forwarder.go) |
| M | source | 387 | 54 | 06,07,16 | [backend/internal/service/openai_ws_http_bridge.go](../../../backend/internal/service/openai_ws_http_bridge.go) |
| M | source | 15 | 1 | 06,07,16 | [backend/internal/service/openai_ws_session_preemption.go](../../../backend/internal/service/openai_ws_session_preemption.go) |
| M | source | 210 | 9 | 06,07,08,16 | [backend/internal/service/ops_upstream_context.go](../../../backend/internal/service/ops_upstream_context.go) |
| M | source | 39 | 23 | 16 | [backend/internal/service/setting_features.go](../../../backend/internal/service/setting_features.go) |
| M | source | 57 | 5 | 07,16 | [backend/internal/service/setting_parse.go](../../../backend/internal/service/setting_parse.go) |
| M | source | 0 | 4 | 16 | [backend/internal/service/setting_public.go](../../../backend/internal/service/setting_public.go) |
| M | source | 5 | 7 | 16 | [backend/internal/service/setting_service.go](../../../backend/internal/service/setting_service.go) |
| M | test | 15 | 13 | 16 | [backend/internal/service/setting_service_platform_threshold_test.go](../../../backend/internal/service/setting_service_platform_threshold_test.go) |
| M | test | 3 | 4 | 16 | [backend/internal/service/setting_service_update_test.go](../../../backend/internal/service/setting_service_update_test.go) |
| M | source | 80 | 6 | 07,16 | [backend/internal/service/setting_update.go](../../../backend/internal/service/setting_update.go) |
| M | source | 18 | 3 | 07,16 | [backend/internal/service/settings_view.go](../../../backend/internal/service/settings_view.go) |
| M | source | 23 | 9 | 07,10,16 | [backend/internal/service/wire.go](../../../backend/internal/service/wire.go) |
| M | source | 9 | 0 | 16 | [backend/internal/testutil/stubs.go](../../../backend/internal/testutil/stubs.go) |
| A | source | 13 | 0 | 16 | [backend/internal/testutil/transport.go](../../../backend/internal/testutil/transport.go) |
| M | documentation | 9 | 9 | 16 | [backend/pkg/pluginapi/README.md](../../../backend/pkg/pluginapi/README.md) |
| M | documentation | 1 | 1 | 16 | [backend/pkg/pluginapi/docs/development.md](../../../backend/pkg/pluginapi/docs/development.md) |
| M | documentation | 1 | 1 | 16 | [backend/pkg/pluginapi/docs/package-format.md](../../../backend/pkg/pluginapi/docs/package-format.md) |
| M | documentation | 2 | 2 | 16 | [backend/pkg/pluginapi/docs/security.md](../../../backend/pkg/pluginapi/docs/security.md) |
| M | tooling | 1 | 1 | 16 | [backend/pkg/pluginapi/v1/manifest.schema.json](../../../backend/pkg/pluginapi/v1/manifest.schema.json) |
| M | tooling | 4 | 1 | 16 | [frontend/package.json](../../../frontend/package.json) |
| M | tooling | 20 | 0 | 16 | [frontend/pnpm-lock.yaml](../../../frontend/pnpm-lock.yaml) |
| M | source | 8 | 2 | 16 | [frontend/src/App.vue](../../../frontend/src/App.vue) |
| M | test | 1 | 0 | 16 | [frontend/src/api/__tests__/settings.authSourceDefaults.spec.ts](../../../frontend/src/api/__tests__/settings.authSourceDefaults.spec.ts) |
| M | test | 6 | 12 | 16 | [frontend/src/api/__tests__/settings.paymentVisibleMethods.spec.ts](../../../frontend/src/api/__tests__/settings.paymentVisibleMethods.spec.ts) |
| M | source | 64 | 26 | 07,16 | [frontend/src/api/admin/settings.ts](../../../frontend/src/api/admin/settings.ts) |
| M | source | 16 | 2 | 16 | [frontend/src/api/admin/system.ts](../../../frontend/src/api/admin/system.ts) |
| M | source | 12 | 6 | 16 | [frontend/src/api/setup.ts](../../../frontend/src/api/setup.ts) |
| M | source | 2 | 2 | 16 | [frontend/src/composables/useOnboardingTour.ts](../../../frontend/src/composables/useOnboardingTour.ts) |
| M | source | 3 | 2 | 16 | [frontend/src/main.ts](../../../frontend/src/main.ts) |
| M | documentation | 1 | 1 | 16 | [frontend/src/router/README.md](../../../frontend/src/router/README.md) |
| M | test | 16 | 0 | 16 | [frontend/src/router/__tests__/feature-access.spec.ts](../../../frontend/src/router/__tests__/feature-access.spec.ts) |
| M | test | 105 | 12 | 16 | [frontend/src/router/__tests__/title.spec.ts](../../../frontend/src/router/__tests__/title.spec.ts) |
| M | source | 61 | 13 | 16 | [frontend/src/router/index.ts](../../../frontend/src/router/index.ts) |
| M | source | 12 | 0 | 16 | [frontend/src/router/meta.d.ts](../../../frontend/src/router/meta.d.ts) |
| M | source | 43 | 4 | 16 | [frontend/src/router/title.ts](../../../frontend/src/router/title.ts) |
| M | documentation | 1 | 1 | 16 | [frontend/src/stores/README.md](../../../frontend/src/stores/README.md) |
| M | test | 0 | 2 | 16 | [frontend/src/stores/__tests__/app.spec.ts](../../../frontend/src/stores/__tests__/app.spec.ts) |
| M | source | 15 | 0 | 16 | [frontend/src/stores/adminSettings.ts](../../../frontend/src/stores/adminSettings.ts) |
| M | source | 38 | 5 | 16 | [frontend/src/stores/app.ts](../../../frontend/src/stores/app.ts) |
| M | source | 31 | 4 | 16 | [frontend/src/types/index.ts](../../../frontend/src/types/index.ts) |
| M | source | 368 | 220 | 07,16 | [frontend/src/views/admin/SettingsView.vue](../../../frontend/src/views/admin/SettingsView.vue) |
| M | test | 77 | 7 | 16 | [frontend/src/views/admin/__tests__/SettingsView.spec.ts](../../../frontend/src/views/admin/__tests__/SettingsView.spec.ts) |
| M | documentation | 1 | 1 | 16 | [frontend/src/views/auth/README.md](../../../frontend/src/views/auth/README.md) |
| M | documentation | 4 | 4 | 16 | [frontend/src/views/auth/VISUAL_GUIDE.md](../../../frontend/src/views/auth/VISUAL_GUIDE.md) |
| M | source | 7 | 1 | 16 | [frontend/vite.config.ts](../../../frontend/vite.config.ts) |
| M | source | 5 | 0 | 16 | [frontend/vitest.config.ts](../../../frontend/vitest.config.ts) |

## 关联实现（由其他组提交）

同一共享文件保持单一主提交，跨组依赖随完整提交集验收。

| 主归属组 | 类别 | 文件 |
| --- | --- | --- |
| 05 | source | [frontend/src/composables/useModelWhitelist.ts](../../../frontend/src/composables/useModelWhitelist.ts) |

## 2026-10-14 新增需求与实施边界

统一接入导出配置、路由、Wire 装配、协议返回和 Ops 诊断。没有数据库迁移、第二套调度器或新的资金结算路径。
