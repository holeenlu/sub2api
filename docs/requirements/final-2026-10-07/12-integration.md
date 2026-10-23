# 12 · 跨功能设置、路由与依赖装配

归档日期：2026-10-07。文件快照：main `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238`；官方比较基线 `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`。

本页登记最终需求与实际文件归属；不把本轮提交整理等同于重新实现或完整安全审计。

## 最终需求

1. 共用配置、settings 读写与公开投影、HTTP DTO、服务装配、路由注册、前端 API 类型及 store 统一保留最终版本。跨功能文件只提交一次，文件归属不等于该文件只实现一个需求。

2. 后端策略声明与真实注册路由匹配；新管理路由缺少声明应由覆盖测试发现，前端导航依据同一授权快照。公共功能开关仍由既有 feature flag/store 链路消费。

3. 前后端权限、模型、计费和配置校验复用现有实现；不因整理新建兼容层、独立规则表、接口别名或双写路径。SettingsView 等大型共享文件不作机械拆分，以免改变行为。

4. 保留已退休字段的拒绝写入和缓存代次；保留后端依赖/生成装配与前端 package/lock 文件匹配。插件 SDK 的原有协议与品牌示例依照实际文件差异归档，不宣称增加插件能力。

## 入口与配置归属

backend/internal/config、handler/dto、server/routes、service/setting*、wire；frontend SettingsView/router/stores/types/api。

## 验收边界

最终全树内容一致、路由/字段覆盖、前端类型、构建装配及既有回归。该组与功能组共同组成完整交付集，单条分类提交不承诺可独立挑选、编译或部署。

验证状态统一见 [总说明](README.md#验证和未关闭边界)。本页列的是验收要求，不是宣称本轮已重新运行这些检查。

## 对比统计

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 业务代码 | 53 | 939 | 418 | 0 |
| 测试/夹具 | 16 | 607 | 118 | 0 |
| 生成代码/繁体 | 1 | 16 | 14 | 0 |
| 工具/构建/配置 | 4 | 62 | 27 | 0 |
| 已有文档/许可 | 8 | 20 | 20 | 0 |

统计为最终文件相对固定官方基线的累计差异，并非本次整理新写的代码。对重命名统一展开为删除/新增；跨域文件只设一个主归属。

## 修改文件

| 状态 | 类型 | +行 | −行 | 文件 |
| --- | --- | ---: | ---: | --- |
| M | 业务代码 | 63 | 5 | [backend/cmd/server/main.go](../../../backend/cmd/server/main.go) |
| A | 测试/夹具 | 25 | 0 | [backend/cmd/server/upstream_version_test.go](../../../backend/cmd/server/upstream_version_test.go) |
| M | 业务代码 | 4 | 2 | [backend/cmd/server/wire.go](../../../backend/cmd/server/wire.go) |
| M | 生成代码/繁体 | 16 | 14 | [backend/cmd/server/wire_gen.go](../../../backend/cmd/server/wire_gen.go) |
| M | 工具/构建/配置 | 1 | 1 | [backend/go.mod](../../../backend/go.mod) |
| M | 工具/构建/配置 | 37 | 25 | [backend/go.sum](../../../backend/go.sum) |
| M | 业务代码 | 58 | 7 | [backend/internal/config/config.go](../../../backend/internal/config/config.go) |
| M | 测试/夹具 | 58 | 1 | [backend/internal/config/config_test.go](../../../backend/internal/config/config_test.go) |
| M | 业务代码 | 3 | 2 | [backend/internal/domain/constants.go](../../../backend/internal/domain/constants.go) |
| A | 业务代码 | 29 | 0 | [backend/internal/domain/credential_keys.go](../../../backend/internal/domain/credential_keys.go) |
| M | 测试/夹具 | 53 | 53 | [backend/internal/handler/admin/admin_basic_handlers_test.go](../../../backend/internal/handler/admin/admin_basic_handlers_test.go) |
| M | 业务代码 | 2 | 1 | [backend/internal/handler/admin/setting_handler.go](../../../backend/internal/handler/admin/setting_handler.go) |
| M | 业务代码 | 0 | 3 | [backend/internal/handler/admin/setting_handler_audit.go](../../../backend/internal/handler/admin/setting_handler_audit.go) |
| M | 业务代码 | 26 | 0 | [backend/internal/handler/admin/setting_handler_email.go](../../../backend/internal/handler/admin/setting_handler_email.go) |
| M | 业务代码 | 4 | 0 | [backend/internal/handler/admin/setting_handler_runtime.go](../../../backend/internal/handler/admin/setting_handler_runtime.go) |
| M | 测试/夹具 | 71 | 0 | [backend/internal/handler/admin/setting_handler_stepup_switch_test.go](../../../backend/internal/handler/admin/setting_handler_stepup_switch_test.go) |
| M | 业务代码 | 10 | 4 | [backend/internal/handler/admin/setting_handler_update.go](../../../backend/internal/handler/admin/setting_handler_update.go) |
| M | 业务代码 | 22 | 5 | [backend/internal/handler/admin/system_handler.go](../../../backend/internal/handler/admin/system_handler.go) |
| M | 业务代码 | 7 | 4 | [backend/internal/handler/dto/mappers.go](../../../backend/internal/handler/dto/mappers.go) |
| M | 业务代码 | 23 | 23 | [backend/internal/handler/dto/settings.go](../../../backend/internal/handler/dto/settings.go) |
| M | 业务代码 | 7 | 2 | [backend/internal/handler/dto/types.go](../../../backend/internal/handler/dto/types.go) |
| M | 业务代码 | 4 | 2 | [backend/internal/handler/handler.go](../../../backend/internal/handler/handler.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/handler/page_handler.go](../../../backend/internal/handler/page_handler.go) |
| M | 业务代码 | 0 | 1 | [backend/internal/handler/setting_handler.go](../../../backend/internal/handler/setting_handler.go) |
| M | 业务代码 | 3 | 0 | [backend/internal/handler/wire.go](../../../backend/internal/handler/wire.go) |
| M | 业务代码 | 30 | 3 | [backend/internal/pkg/response/response.go](../../../backend/internal/pkg/response/response.go) |
| M | 测试/夹具 | 5 | 3 | [backend/internal/repository/integration_harness_test.go](../../../backend/internal/repository/integration_harness_test.go) |
| M | 测试/夹具 | 61 | 0 | [backend/internal/repository/migrations_schema_integration_test.go](../../../backend/internal/repository/migrations_schema_integration_test.go) |
| M | 业务代码 | 10 | 0 | [backend/internal/repository/setting_repo.go](../../../backend/internal/repository/setting_repo.go) |
| M | 测试/夹具 | 62 | 7 | [backend/internal/server/api_contract_test.go](../../../backend/internal/server/api_contract_test.go) |
| A | 测试/夹具 | 43 | 0 | [backend/internal/server/management_route_coverage_test.go](../../../backend/internal/server/management_route_coverage_test.go) |
| M | 业务代码 | 3 | 1 | [backend/internal/server/router.go](../../../backend/internal/server/router.go) |
| M | 业务代码 | 11 | 0 | [backend/internal/server/routes/admin.go](../../../backend/internal/server/routes/admin.go) |
| M | 业务代码 | 3 | 0 | [backend/internal/server/routes/payment.go](../../../backend/internal/server/routes/payment.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/service/api_key_auth_cache_impl.go](../../../backend/internal/service/api_key_auth_cache_impl.go) |
| M | 业务代码 | 9 | 0 | [backend/internal/service/api_key_auth_cache_invalidate.go](../../../backend/internal/service/api_key_auth_cache_invalidate.go) |
| M | 业务代码 | 17 | 3 | [backend/internal/service/domain_constants.go](../../../backend/internal/service/domain_constants.go) |
| M | 业务代码 | 39 | 23 | [backend/internal/service/setting_features.go](../../../backend/internal/service/setting_features.go) |
| M | 业务代码 | 5 | 5 | [backend/internal/service/setting_parse.go](../../../backend/internal/service/setting_parse.go) |
| M | 业务代码 | 0 | 4 | [backend/internal/service/setting_public.go](../../../backend/internal/service/setting_public.go) |
| M | 业务代码 | 5 | 7 | [backend/internal/service/setting_service.go](../../../backend/internal/service/setting_service.go) |
| M | 测试/夹具 | 15 | 13 | [backend/internal/service/setting_service_platform_threshold_test.go](../../../backend/internal/service/setting_service_platform_threshold_test.go) |
| M | 测试/夹具 | 3 | 4 | [backend/internal/service/setting_service_update_test.go](../../../backend/internal/service/setting_service_update_test.go) |
| M | 业务代码 | 14 | 6 | [backend/internal/service/setting_update.go](../../../backend/internal/service/setting_update.go) |
| M | 业务代码 | 1 | 2 | [backend/internal/service/settings_view.go](../../../backend/internal/service/settings_view.go) |
| M | 业务代码 | 17 | 8 | [backend/internal/service/wire.go](../../../backend/internal/service/wire.go) |
| M | 业务代码 | 9 | 0 | [backend/internal/testutil/stubs.go](../../../backend/internal/testutil/stubs.go) |
| A | 业务代码 | 13 | 0 | [backend/internal/testutil/transport.go](../../../backend/internal/testutil/transport.go) |
| M | 已有文档/许可 | 9 | 9 | [backend/pkg/pluginapi/README.md](../../../backend/pkg/pluginapi/README.md) |
| M | 已有文档/许可 | 1 | 1 | [backend/pkg/pluginapi/docs/development.md](../../../backend/pkg/pluginapi/docs/development.md) |
| M | 已有文档/许可 | 1 | 1 | [backend/pkg/pluginapi/docs/package-format.md](../../../backend/pkg/pluginapi/docs/package-format.md) |
| M | 已有文档/许可 | 2 | 2 | [backend/pkg/pluginapi/docs/security.md](../../../backend/pkg/pluginapi/docs/security.md) |
| M | 业务代码 | 1 | 1 | [backend/pkg/pluginapi/v1/manifest.schema.json](../../../backend/pkg/pluginapi/v1/manifest.schema.json) |
| M | 工具/构建/配置 | 4 | 1 | [frontend/package.json](../../../frontend/package.json) |
| M | 工具/构建/配置 | 20 | 0 | [frontend/pnpm-lock.yaml](../../../frontend/pnpm-lock.yaml) |
| M | 业务代码 | 8 | 2 | [frontend/src/App.vue](../../../frontend/src/App.vue) |
| M | 测试/夹具 | 7 | 4 | [frontend/src/api/__tests__/settings.authSourceDefaults.spec.ts](../../../frontend/src/api/__tests__/settings.authSourceDefaults.spec.ts) |
| M | 测试/夹具 | 6 | 12 | [frontend/src/api/__tests__/settings.paymentVisibleMethods.spec.ts](../../../frontend/src/api/__tests__/settings.paymentVisibleMethods.spec.ts) |
| M | 业务代码 | 32 | 26 | [frontend/src/api/admin/settings.ts](../../../frontend/src/api/admin/settings.ts) |
| M | 业务代码 | 16 | 2 | [frontend/src/api/admin/system.ts](../../../frontend/src/api/admin/system.ts) |
| M | 业务代码 | 12 | 6 | [frontend/src/api/setup.ts](../../../frontend/src/api/setup.ts) |
| M | 业务代码 | 8 | 6 | [frontend/src/composables/useModelWhitelist.ts](../../../frontend/src/composables/useModelWhitelist.ts) |
| M | 业务代码 | 2 | 2 | [frontend/src/composables/useOnboardingTour.ts](../../../frontend/src/composables/useOnboardingTour.ts) |
| A | 业务代码 | 21 | 0 | [frontend/src/config/openaiModels.ts](../../../frontend/src/config/openaiModels.ts) |
| M | 业务代码 | 3 | 2 | [frontend/src/main.ts](../../../frontend/src/main.ts) |
| M | 已有文档/许可 | 1 | 1 | [frontend/src/router/README.md](../../../frontend/src/router/README.md) |
| M | 测试/夹具 | 16 | 0 | [frontend/src/router/__tests__/feature-access.spec.ts](../../../frontend/src/router/__tests__/feature-access.spec.ts) |
| M | 测试/夹具 | 105 | 12 | [frontend/src/router/__tests__/title.spec.ts](../../../frontend/src/router/__tests__/title.spec.ts) |
| M | 业务代码 | 61 | 13 | [frontend/src/router/index.ts](../../../frontend/src/router/index.ts) |
| M | 业务代码 | 12 | 0 | [frontend/src/router/meta.d.ts](../../../frontend/src/router/meta.d.ts) |
| M | 业务代码 | 43 | 4 | [frontend/src/router/title.ts](../../../frontend/src/router/title.ts) |
| M | 已有文档/许可 | 1 | 1 | [frontend/src/stores/README.md](../../../frontend/src/stores/README.md) |
| M | 测试/夹具 | 0 | 2 | [frontend/src/stores/__tests__/app.spec.ts](../../../frontend/src/stores/__tests__/app.spec.ts) |
| M | 业务代码 | 15 | 0 | [frontend/src/stores/adminSettings.ts](../../../frontend/src/stores/adminSettings.ts) |
| M | 业务代码 | 38 | 5 | [frontend/src/stores/app.ts](../../../frontend/src/stores/app.ts) |
| M | 业务代码 | 31 | 4 | [frontend/src/types/index.ts](../../../frontend/src/types/index.ts) |
| M | 业务代码 | 171 | 219 | [frontend/src/views/admin/SettingsView.vue](../../../frontend/src/views/admin/SettingsView.vue) |
| M | 测试/夹具 | 77 | 7 | [frontend/src/views/admin/__tests__/SettingsView.spec.ts](../../../frontend/src/views/admin/__tests__/SettingsView.spec.ts) |
| M | 已有文档/许可 | 1 | 1 | [frontend/src/views/auth/README.md](../../../frontend/src/views/auth/README.md) |
| M | 已有文档/许可 | 4 | 4 | [frontend/src/views/auth/VISUAL_GUIDE.md](../../../frontend/src/views/auth/VISUAL_GUIDE.md) |
| M | 业务代码 | 7 | 1 | [frontend/vite.config.ts](../../../frontend/vite.config.ts) |
| M | 业务代码 | 5 | 0 | [frontend/vitest.config.ts](../../../frontend/vitest.config.ts) |
