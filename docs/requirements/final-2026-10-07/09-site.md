# 09 · 品牌界面、文档站与用户体验

归档日期：2026-10-07。文件快照：main `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238`；官方比较基线 `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`。

本页登记最终需求与实际文件归属；不把本轮提交整理等同于重新实现或完整安全审计。

## 最终需求

1. main 为 KDAN 品牌基线；品牌名称/Logo/默认域名/文档/下载/法律与部署差异保留在当前明确文件中，运行时站点设置优先于品牌默认值。TapModels 和 tokensavy 仍是分别部署的网站，不共享用户或站点数据库。

2. 保留首页展示、紧凑首页、文档站导航和代码示例、模型页面、法律说明、登录/注册协议及用户资料的当前实现；删除的宣传资产也按最终树保留删除状态。

3. 可用渠道开关只控制用户 /available-channels 入口和用户查询；管理端渠道管理/定价入口保留。渠道监控开关控制监控运行和用户渠道状态入口；不得将两个功能开关混为一项。

4. 模型广场、Channel Monitor、Passkey 等原生功能继续存在；只有相对固定基线的品牌、授权、安全或展示差异计入本清单，不能把整个原生模块宣称为 fork 新增。

5. 公共内容、Logo/链接处理、图标和弹窗体验保持当前代码；品牌适配不可覆盖共享权限、计费和安全约束。

## 入口与配置归属

首页、/docs、/model-plaza、用户个人区、frontend/src/config/brand.ts、backend/internal/service/brand.go。

## 验收边界

品牌分支与整理前逐文件一致，Logo/品牌常量/域名/下载/服务名未串换；用户可用渠道开关不隐藏后台定价配置；原有 UI 与文档测试证据按实际运行范围记录。

验证状态统一见 [总说明](README.md#验证和未关闭边界)。本页列的是验收要求，不是宣称本轮已重新运行这些检查。

## 对比统计

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 已有文档/许可 | 76 | 4132 | 824 | 0 |
| 图像/下载资源 | 47 | 82 | 22 | 44 |
| 业务代码 | 60 | 2418 | 483 | 0 |
| 测试/夹具 | 17 | 465 | 40 | 0 |
| 生成代码/繁体 | 23 | 1477 | 0 | 0 |

统计为最终文件相对固定官方基线的累计差异，并非本次整理新写的代码。对重命名统一展开为删除/新增；跨域文件只设一个主归属。

## 修改文件

| 状态 | 类型 | +行 | −行 | 文件 |
| --- | --- | ---: | ---: | --- |
| M | 已有文档/许可 | 5 | 5 | [DEV_GUIDE.md](../../../DEV_GUIDE.md) |
| M | 已有文档/许可 | 45 | 274 | [README.md](../../../README.md) |
| M | 已有文档/许可 | 35 | 268 | [README_CN.md](../../../README_CN.md) |
| M | 已有文档/许可 | 33 | 266 | [README_JA.md](../../../README_JA.md) |
| M | 图像/下载资源 | 22 | 11 | [assets/logo.svg](../../../assets/logo.svg) |
| D | 图像/下载资源 | - | - | `assets/partners/logos/RoxyBrowser.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/aigocode.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/aimzoon.jpg`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/apikey-fun.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/apimart.jpg`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/axisnow.jpg`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/bestproxy.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/bmoplus.jpg`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/cctk.jpg`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/codex-everywhere.jpg`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/ctok.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/duckip.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/etok.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/fastaitoken.jpg`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/fennoai.jpg`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/haoai.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/lanox.jpg`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/nagora.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/openmodel.jpg`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/pateway.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/pincc-logo.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/poixe.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/pptoken.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/proxy4free.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/qiniu.jpg`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/rapidproxy.jpg`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/runapi.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/silkapi.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/swiftprox.png`（已删除） |
| D | 图像/下载资源 | - | - | `assets/partners/logos/veilx.png`（已删除） |
| A | 图像/下载资源 | 38 | 0 | [assets/wordmark.svg](../../../assets/wordmark.svg) |
| M | 业务代码 | 3 | 2 | [backend/internal/handler/channel_monitor_v2_handler.go](../../../backend/internal/handler/channel_monitor_v2_handler.go) |
| A | 业务代码 | 32 | 0 | [backend/internal/service/brand.go](../../../backend/internal/service/brand.go) |
| M | 业务代码 | 64 | 7 | [backend/internal/service/model_plaza_service.go](../../../backend/internal/service/model_plaza_service.go) |
| M | 测试/夹具 | 45 | 0 | [backend/internal/service/model_plaza_service_test.go](../../../backend/internal/service/model_plaza_service_test.go) |
| A | 测试/夹具 | 73 | 0 | [backend/internal/setup/bootstrap_security_test.go](../../../backend/internal/setup/bootstrap_security_test.go) |
| M | 业务代码 | 3 | 3 | [backend/internal/setup/cli.go](../../../backend/internal/setup/cli.go) |
| M | 业务代码 | 24 | 4 | [backend/internal/setup/handler.go](../../../backend/internal/setup/handler.go) |
| M | 业务代码 | 4 | 4 | [backend/internal/setup/setup.go](../../../backend/internal/setup/setup.go) |
| M | 测试/夹具 | 22 | 10 | [backend/internal/setup/setup_test.go](../../../backend/internal/setup/setup_test.go) |
| M | 业务代码 | 8 | 1 | [backend/internal/web/embed_on.go](../../../backend/internal/web/embed_on.go) |
| M | 测试/夹具 | 13 | 13 | [backend/internal/web/embed_test.go](../../../backend/internal/web/embed_test.go) |
| A | 已有文档/许可 | 49 | 0 | [docs/MODEL_PLAZA_CATALOG.md](../../../docs/MODEL_PLAZA_CATALOG.md) |
| M | 已有文档/许可 | 5 | 5 | [docs/legal/admin-compliance.en.md](../../../docs/legal/admin-compliance.en.md) |
| A | 已有文档/许可 | 49 | 0 | [docs/legal/admin-compliance.ja.md](../../../docs/legal/admin-compliance.ja.md) |
| A | 生成代码/繁体 | 49 | 0 | [docs/legal/admin-compliance.zh-TW.md](../../../docs/legal/admin-compliance.zh-TW.md) |
| M | 已有文档/许可 | 5 | 5 | [docs/legal/admin-compliance.zh.md](../../../docs/legal/admin-compliance.zh.md) |
| M | 业务代码 | 1 | 1 | [frontend/index.html](../../../frontend/index.html) |
| A | 已有文档/许可 | 13 | 0 | [frontend/public/docs-assets/README.md](../../../frontend/public/docs-assets/README.md) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/client-claude-en.png](../../../frontend/public/docs-assets/client-claude-en.png) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/client-claude-windows.png](../../../frontend/public/docs-assets/client-claude-windows.png) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/client-claude-zh-TW.png](../../../frontend/public/docs-assets/client-claude-zh-TW.png) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/client-claude-zh.png](../../../frontend/public/docs-assets/client-claude-zh.png) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/client-claude.png](../../../frontend/public/docs-assets/client-claude.png) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/client-codex-apikey.png](../../../frontend/public/docs-assets/client-codex-apikey.png) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/client-codex-en.png](../../../frontend/public/docs-assets/client-codex-en.png) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/client-codex-zh-TW.png](../../../frontend/public/docs-assets/client-codex-zh-TW.png) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/client-codex-zh.png](../../../frontend/public/docs-assets/client-codex-zh.png) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/client-codex.png](../../../frontend/public/docs-assets/client-codex.png) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/create-api-key-en.png](../../../frontend/public/docs-assets/create-api-key-en.png) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/create-api-key-zh-TW.png](../../../frontend/public/docs-assets/create-api-key-zh-TW.png) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/create-api-key-zh.png](../../../frontend/public/docs-assets/create-api-key-zh.png) |
| A | 图像/下载资源 | - | - | [frontend/public/docs-assets/create-api-key.png](../../../frontend/public/docs-assets/create-api-key.png) |
| M | 图像/下载资源 | 22 | 11 | [frontend/public/logo.svg](../../../frontend/public/logo.svg) |
| M | 业务代码 | 10 | 1 | [frontend/src/components/AliyunCaptchaWidget.vue](../../../frontend/src/components/AliyunCaptchaWidget.vue) |
| M | 业务代码 | 10 | 1 | [frontend/src/components/TencentCaptchaGate.vue](../../../frontend/src/components/TencentCaptchaGate.vue) |
| M | 业务代码 | 7 | 21 | [frontend/src/components/auth/LoginAgreementPrompt.vue](../../../frontend/src/components/auth/LoginAgreementPrompt.vue) |
| M | 业务代码 | 2 | 8 | [frontend/src/components/auth/WechatOAuthSection.vue](../../../frontend/src/components/auth/WechatOAuthSection.vue) |
| M | 测试/夹具 | 0 | 1 | [frontend/src/components/auth/__tests__/WechatOAuthSection.spec.ts](../../../frontend/src/components/auth/__tests__/WechatOAuthSection.spec.ts) |
| M | 业务代码 | 20 | 13 | [frontend/src/components/common/BaseDialog.vue](../../../frontend/src/components/common/BaseDialog.vue) |
| M | 业务代码 | 2 | 1 | [frontend/src/components/common/DateRangePicker.vue](../../../frontend/src/components/common/DateRangePicker.vue) |
| M | 业务代码 | 1 | 1 | [frontend/src/components/common/PlatformIcon.vue](../../../frontend/src/components/common/PlatformIcon.vue) |
| M | 业务代码 | 1 | 0 | [frontend/src/components/common/PlatformTypeBadge.vue](../../../frontend/src/components/common/PlatformTypeBadge.vue) |
| D | 业务代码 | 0 | 18 | `frontend/src/components/common/ProxyAdBanner.vue`（已删除） |
| M | 业务代码 | 1 | 1 | [frontend/src/components/common/Select.vue](../../../frontend/src/components/common/Select.vue) |
| M | 业务代码 | 3 | 1 | [frontend/src/components/common/Toast.vue](../../../frontend/src/components/common/Toast.vue) |
| M | 测试/夹具 | 3 | 1 | [frontend/src/components/common/__tests__/BaseDialog.ids.spec.ts](../../../frontend/src/components/common/__tests__/BaseDialog.ids.spec.ts) |
| M | 测试/夹具 | 2 | 1 | [frontend/src/components/common/__tests__/BaseDialog.scrollLock.spec.ts](../../../frontend/src/components/common/__tests__/BaseDialog.scrollLock.spec.ts) |
| M | 测试/夹具 | 14 | 1 | [frontend/src/components/common/__tests__/BaseDialog.spec.ts](../../../frontend/src/components/common/__tests__/BaseDialog.spec.ts) |
| A | 业务代码 | 53 | 0 | [frontend/src/components/docs/DocsCodeTabs.vue](../../../frontend/src/components/docs/DocsCodeTabs.vue) |
| A | 业务代码 | 25 | 0 | [frontend/src/components/docs/DocsGroupSelect.vue](../../../frontend/src/components/docs/DocsGroupSelect.vue) |
| A | 业务代码 | 26 | 0 | [frontend/src/components/docs/DocsPricingTable.vue](../../../frontend/src/components/docs/DocsPricingTable.vue) |
| A | 业务代码 | 36 | 0 | [frontend/src/components/docs/DocsSidebar.vue](../../../frontend/src/components/docs/DocsSidebar.vue) |
| A | 测试/夹具 | 31 | 0 | [frontend/src/components/docs/__tests__/DocsPricingTable.spec.ts](../../../frontend/src/components/docs/__tests__/DocsPricingTable.spec.ts) |
| A | 业务代码 | 44 | 0 | [frontend/src/components/home/AgentToolCards.vue](../../../frontend/src/components/home/AgentToolCards.vue) |
| A | 业务代码 | 203 | 0 | [frontend/src/components/home/ArchitectureDiagram.vue](../../../frontend/src/components/home/ArchitectureDiagram.vue) |
| A | 业务代码 | 41 | 0 | [frontend/src/components/home/DiagramNode.vue](../../../frontend/src/components/home/DiagramNode.vue) |
| A | 业务代码 | 160 | 0 | [frontend/src/components/home/GatewayShowcase.vue](../../../frontend/src/components/home/GatewayShowcase.vue) |
| A | 业务代码 | 37 | 0 | [frontend/src/components/home/MechanismList.vue](../../../frontend/src/components/home/MechanismList.vue) |
| A | 业务代码 | 94 | 0 | [frontend/src/components/home/ModelShowcase.vue](../../../frontend/src/components/home/ModelShowcase.vue) |
| A | 测试/夹具 | 41 | 0 | [frontend/src/components/home/__tests__/ArchitectureDiagram.spec.ts](../../../frontend/src/components/home/__tests__/ArchitectureDiagram.spec.ts) |
| A | 测试/夹具 | 86 | 0 | [frontend/src/components/home/__tests__/ModelShowcase.spec.ts](../../../frontend/src/components/home/__tests__/ModelShowcase.spec.ts) |
| A | 业务代码 | 42 | 0 | [frontend/src/components/home/mechanisms.ts](../../../frontend/src/components/home/mechanisms.ts) |
| A | 业务代码 | 49 | 0 | [frontend/src/components/home/officialModels.ts](../../../frontend/src/components/home/officialModels.ts) |
| M | 业务代码 | 7 | 6 | [frontend/src/components/layout/AppHeader.vue](../../../frontend/src/components/layout/AppHeader.vue) |
| M | 业务代码 | 8 | 2 | [frontend/src/components/layout/AppLayout.vue](../../../frontend/src/components/layout/AppLayout.vue) |
| M | 业务代码 | 13 | 3 | [frontend/src/components/layout/AppSidebar.vue](../../../frontend/src/components/layout/AppSidebar.vue) |
| M | 业务代码 | 6 | 3 | [frontend/src/components/layout/AuthLayout.vue](../../../frontend/src/components/layout/AuthLayout.vue) |
| M | 已有文档/许可 | 1 | 1 | [frontend/src/components/layout/README.md](../../../frontend/src/components/layout/README.md) |
| M | 测试/夹具 | 0 | 9 | [frontend/src/components/layout/__tests__/docUrlSanitization.spec.ts](../../../frontend/src/components/layout/__tests__/docUrlSanitization.spec.ts) |
| M | 业务代码 | 1 | 1 | [frontend/src/components/modelPlaza/PlazaNavBar.vue](../../../frontend/src/components/modelPlaza/PlazaNavBar.vue) |
| M | 业务代码 | 2 | 2 | [frontend/src/components/user/profile/ProfileInfoCard.vue](../../../frontend/src/components/user/profile/ProfileInfoCard.vue) |
| M | 测试/夹具 | 0 | 1 | [frontend/src/components/user/profile/__tests__/ProfileIdentityBindingsSection.spec.ts](../../../frontend/src/components/user/profile/__tests__/ProfileIdentityBindingsSection.spec.ts) |
| A | 业务代码 | 38 | 0 | [frontend/src/composables/useModelPlaza.ts](../../../frontend/src/composables/useModelPlaza.ts) |
| A | 业务代码 | 74 | 0 | [frontend/src/config/brand.ts](../../../frontend/src/config/brand.ts) |
| A | 测试/夹具 | 20 | 0 | [frontend/src/content/docs/__tests__/examples.spec.ts](../../../frontend/src/content/docs/__tests__/examples.spec.ts) |
| A | 测试/夹具 | 21 | 0 | [frontend/src/content/docs/__tests__/navigation.spec.ts](../../../frontend/src/content/docs/__tests__/navigation.spec.ts) |
| A | 测试/夹具 | 44 | 0 | [frontend/src/content/docs/__tests__/pricingSnapshot.spec.ts](../../../frontend/src/content/docs/__tests__/pricingSnapshot.spec.ts) |
| A | 已有文档/许可 | 102 | 0 | [frontend/src/content/docs/en/api/chat/anthropic-messages.md](../../../frontend/src/content/docs/en/api/chat/anthropic-messages.md) |
| A | 已有文档/许可 | 64 | 0 | [frontend/src/content/docs/en/api/chat/gemini-native.md](../../../frontend/src/content/docs/en/api/chat/gemini-native.md) |
| A | 已有文档/许可 | 47 | 0 | [frontend/src/content/docs/en/api/chat/grok-native.md](../../../frontend/src/content/docs/en/api/chat/grok-native.md) |
| A | 已有文档/许可 | 105 | 0 | [frontend/src/content/docs/en/api/chat/openai-chat.md](../../../frontend/src/content/docs/en/api/chat/openai-chat.md) |
| A | 已有文档/许可 | 107 | 0 | [frontend/src/content/docs/en/api/chat/openai-responses.md](../../../frontend/src/content/docs/en/api/chat/openai-responses.md) |
| A | 已有文档/许可 | 112 | 0 | [frontend/src/content/docs/en/api/image/openai-image.md](../../../frontend/src/content/docs/en/api/image/openai-image.md) |
| A | 已有文档/许可 | 27 | 0 | [frontend/src/content/docs/en/api/protocols.md](../../../frontend/src/content/docs/en/api/protocols.md) |
| A | 已有文档/许可 | 34 | 0 | [frontend/src/content/docs/en/api/query/embeddings.md](../../../frontend/src/content/docs/en/api/query/embeddings.md) |
| A | 已有文档/许可 | 44 | 0 | [frontend/src/content/docs/en/api/query/models.md](../../../frontend/src/content/docs/en/api/query/models.md) |
| A | 已有文档/许可 | 59 | 0 | [frontend/src/content/docs/en/api/query/token-count.md](../../../frontend/src/content/docs/en/api/query/token-count.md) |
| A | 已有文档/许可 | 113 | 0 | [frontend/src/content/docs/en/apps/codex.md](../../../frontend/src/content/docs/en/apps/codex.md) |
| A | 已有文档/许可 | 70 | 0 | [frontend/src/content/docs/en/apps/console.md](../../../frontend/src/content/docs/en/apps/console.md) |
| A | 已有文档/许可 | 80 | 0 | [frontend/src/content/docs/en/apps/image-skills.md](../../../frontend/src/content/docs/en/apps/image-skills.md) |
| A | 已有文档/许可 | 27 | 0 | [frontend/src/content/docs/en/apps/index.md](../../../frontend/src/content/docs/en/apps/index.md) |
| A | 已有文档/许可 | 58 | 0 | [frontend/src/content/docs/en/apps/session-recovery-claude.md](../../../frontend/src/content/docs/en/apps/session-recovery-claude.md) |
| A | 已有文档/许可 | 76 | 0 | [frontend/src/content/docs/en/apps/session-recovery-codex.md](../../../frontend/src/content/docs/en/apps/session-recovery-codex.md) |
| A | 已有文档/许可 | 21 | 0 | [frontend/src/content/docs/en/authentication.md](../../../frontend/src/content/docs/en/authentication.md) |
| A | 已有文档/许可 | 18 | 0 | [frontend/src/content/docs/en/downloads.md](../../../frontend/src/content/docs/en/downloads.md) |
| A | 已有文档/许可 | 15 | 0 | [frontend/src/content/docs/en/errors.md](../../../frontend/src/content/docs/en/errors.md) |
| A | 已有文档/许可 | 9 | 0 | [frontend/src/content/docs/en/limits.md](../../../frontend/src/content/docs/en/limits.md) |
| A | 已有文档/许可 | 24 | 0 | [frontend/src/content/docs/en/pricing.md](../../../frontend/src/content/docs/en/pricing.md) |
| A | 已有文档/许可 | 20 | 0 | [frontend/src/content/docs/en/quickstart.md](../../../frontend/src/content/docs/en/quickstart.md) |
| A | 业务代码 | 38 | 0 | [frontend/src/content/docs/examples.ts](../../../frontend/src/content/docs/examples.ts) |
| A | 已有文档/许可 | 102 | 0 | [frontend/src/content/docs/ja/api/chat/anthropic-messages.md](../../../frontend/src/content/docs/ja/api/chat/anthropic-messages.md) |
| A | 已有文档/许可 | 64 | 0 | [frontend/src/content/docs/ja/api/chat/gemini-native.md](../../../frontend/src/content/docs/ja/api/chat/gemini-native.md) |
| A | 已有文档/许可 | 47 | 0 | [frontend/src/content/docs/ja/api/chat/grok-native.md](../../../frontend/src/content/docs/ja/api/chat/grok-native.md) |
| A | 已有文档/许可 | 105 | 0 | [frontend/src/content/docs/ja/api/chat/openai-chat.md](../../../frontend/src/content/docs/ja/api/chat/openai-chat.md) |
| A | 已有文档/许可 | 107 | 0 | [frontend/src/content/docs/ja/api/chat/openai-responses.md](../../../frontend/src/content/docs/ja/api/chat/openai-responses.md) |
| A | 已有文档/许可 | 112 | 0 | [frontend/src/content/docs/ja/api/image/openai-image.md](../../../frontend/src/content/docs/ja/api/image/openai-image.md) |
| A | 已有文档/许可 | 27 | 0 | [frontend/src/content/docs/ja/api/protocols.md](../../../frontend/src/content/docs/ja/api/protocols.md) |
| A | 已有文档/许可 | 34 | 0 | [frontend/src/content/docs/ja/api/query/embeddings.md](../../../frontend/src/content/docs/ja/api/query/embeddings.md) |
| A | 已有文档/许可 | 44 | 0 | [frontend/src/content/docs/ja/api/query/models.md](../../../frontend/src/content/docs/ja/api/query/models.md) |
| A | 已有文档/许可 | 59 | 0 | [frontend/src/content/docs/ja/api/query/token-count.md](../../../frontend/src/content/docs/ja/api/query/token-count.md) |
| A | 已有文档/许可 | 113 | 0 | [frontend/src/content/docs/ja/apps/codex.md](../../../frontend/src/content/docs/ja/apps/codex.md) |
| A | 已有文档/许可 | 70 | 0 | [frontend/src/content/docs/ja/apps/console.md](../../../frontend/src/content/docs/ja/apps/console.md) |
| A | 已有文档/许可 | 80 | 0 | [frontend/src/content/docs/ja/apps/image-skills.md](../../../frontend/src/content/docs/ja/apps/image-skills.md) |
| A | 已有文档/许可 | 27 | 0 | [frontend/src/content/docs/ja/apps/index.md](../../../frontend/src/content/docs/ja/apps/index.md) |
| A | 已有文档/许可 | 58 | 0 | [frontend/src/content/docs/ja/apps/session-recovery-claude.md](../../../frontend/src/content/docs/ja/apps/session-recovery-claude.md) |
| A | 已有文档/许可 | 76 | 0 | [frontend/src/content/docs/ja/apps/session-recovery-codex.md](../../../frontend/src/content/docs/ja/apps/session-recovery-codex.md) |
| A | 已有文档/许可 | 21 | 0 | [frontend/src/content/docs/ja/authentication.md](../../../frontend/src/content/docs/ja/authentication.md) |
| A | 已有文档/许可 | 18 | 0 | [frontend/src/content/docs/ja/downloads.md](../../../frontend/src/content/docs/ja/downloads.md) |
| A | 已有文档/许可 | 15 | 0 | [frontend/src/content/docs/ja/errors.md](../../../frontend/src/content/docs/ja/errors.md) |
| A | 已有文档/许可 | 9 | 0 | [frontend/src/content/docs/ja/limits.md](../../../frontend/src/content/docs/ja/limits.md) |
| A | 已有文档/许可 | 24 | 0 | [frontend/src/content/docs/ja/pricing.md](../../../frontend/src/content/docs/ja/pricing.md) |
| A | 已有文档/许可 | 20 | 0 | [frontend/src/content/docs/ja/quickstart.md](../../../frontend/src/content/docs/ja/quickstart.md) |
| A | 业务代码 | 92 | 0 | [frontend/src/content/docs/modelCatalog.ts](../../../frontend/src/content/docs/modelCatalog.ts) |
| A | 业务代码 | 73 | 0 | [frontend/src/content/docs/nav.ts](../../../frontend/src/content/docs/nav.ts) |
| A | 业务代码 | 9 | 0 | [frontend/src/content/docs/pricingSnapshot.ts](../../../frontend/src/content/docs/pricingSnapshot.ts) |
| A | 业务代码 | 64 | 0 | [frontend/src/content/docs/types.ts](../../../frontend/src/content/docs/types.ts) |
| A | 生成代码/繁体 | 127 | 0 | [frontend/src/content/docs/zh-TW/api/chat/anthropic-messages.md](../../../frontend/src/content/docs/zh-TW/api/chat/anthropic-messages.md) |
| A | 生成代码/繁体 | 64 | 0 | [frontend/src/content/docs/zh-TW/api/chat/gemini-native.md](../../../frontend/src/content/docs/zh-TW/api/chat/gemini-native.md) |
| A | 生成代码/繁体 | 47 | 0 | [frontend/src/content/docs/zh-TW/api/chat/grok-native.md](../../../frontend/src/content/docs/zh-TW/api/chat/grok-native.md) |
| A | 生成代码/繁体 | 140 | 0 | [frontend/src/content/docs/zh-TW/api/chat/openai-chat.md](../../../frontend/src/content/docs/zh-TW/api/chat/openai-chat.md) |
| A | 生成代码/繁体 | 150 | 0 | [frontend/src/content/docs/zh-TW/api/chat/openai-responses.md](../../../frontend/src/content/docs/zh-TW/api/chat/openai-responses.md) |
| A | 生成代码/繁体 | 150 | 0 | [frontend/src/content/docs/zh-TW/api/image/openai-image.md](../../../frontend/src/content/docs/zh-TW/api/image/openai-image.md) |
| A | 生成代码/繁体 | 27 | 0 | [frontend/src/content/docs/zh-TW/api/protocols.md](../../../frontend/src/content/docs/zh-TW/api/protocols.md) |
| A | 生成代码/繁体 | 34 | 0 | [frontend/src/content/docs/zh-TW/api/query/embeddings.md](../../../frontend/src/content/docs/zh-TW/api/query/embeddings.md) |
| A | 生成代码/繁体 | 51 | 0 | [frontend/src/content/docs/zh-TW/api/query/models.md](../../../frontend/src/content/docs/zh-TW/api/query/models.md) |
| A | 生成代码/繁体 | 59 | 0 | [frontend/src/content/docs/zh-TW/api/query/token-count.md](../../../frontend/src/content/docs/zh-TW/api/query/token-count.md) |
| A | 生成代码/繁体 | 119 | 0 | [frontend/src/content/docs/zh-TW/apps/codex.md](../../../frontend/src/content/docs/zh-TW/apps/codex.md) |
| A | 生成代码/繁体 | 70 | 0 | [frontend/src/content/docs/zh-TW/apps/console.md](../../../frontend/src/content/docs/zh-TW/apps/console.md) |
| A | 生成代码/繁体 | 87 | 0 | [frontend/src/content/docs/zh-TW/apps/image-skills.md](../../../frontend/src/content/docs/zh-TW/apps/image-skills.md) |
| A | 生成代码/繁体 | 27 | 0 | [frontend/src/content/docs/zh-TW/apps/index.md](../../../frontend/src/content/docs/zh-TW/apps/index.md) |
| A | 生成代码/繁体 | 58 | 0 | [frontend/src/content/docs/zh-TW/apps/session-recovery-claude.md](../../../frontend/src/content/docs/zh-TW/apps/session-recovery-claude.md) |
| A | 生成代码/繁体 | 76 | 0 | [frontend/src/content/docs/zh-TW/apps/session-recovery-codex.md](../../../frontend/src/content/docs/zh-TW/apps/session-recovery-codex.md) |
| A | 生成代码/繁体 | 26 | 0 | [frontend/src/content/docs/zh-TW/authentication.md](../../../frontend/src/content/docs/zh-TW/authentication.md) |
| A | 生成代码/繁体 | 18 | 0 | [frontend/src/content/docs/zh-TW/downloads.md](../../../frontend/src/content/docs/zh-TW/downloads.md) |
| A | 生成代码/繁体 | 21 | 0 | [frontend/src/content/docs/zh-TW/errors.md](../../../frontend/src/content/docs/zh-TW/errors.md) |
| A | 生成代码/繁体 | 19 | 0 | [frontend/src/content/docs/zh-TW/limits.md](../../../frontend/src/content/docs/zh-TW/limits.md) |
| A | 生成代码/繁体 | 24 | 0 | [frontend/src/content/docs/zh-TW/pricing.md](../../../frontend/src/content/docs/zh-TW/pricing.md) |
| A | 生成代码/繁体 | 34 | 0 | [frontend/src/content/docs/zh-TW/quickstart.md](../../../frontend/src/content/docs/zh-TW/quickstart.md) |
| A | 已有文档/许可 | 127 | 0 | [frontend/src/content/docs/zh/api/chat/anthropic-messages.md](../../../frontend/src/content/docs/zh/api/chat/anthropic-messages.md) |
| A | 已有文档/许可 | 64 | 0 | [frontend/src/content/docs/zh/api/chat/gemini-native.md](../../../frontend/src/content/docs/zh/api/chat/gemini-native.md) |
| A | 已有文档/许可 | 47 | 0 | [frontend/src/content/docs/zh/api/chat/grok-native.md](../../../frontend/src/content/docs/zh/api/chat/grok-native.md) |
| A | 已有文档/许可 | 140 | 0 | [frontend/src/content/docs/zh/api/chat/openai-chat.md](../../../frontend/src/content/docs/zh/api/chat/openai-chat.md) |
| A | 已有文档/许可 | 150 | 0 | [frontend/src/content/docs/zh/api/chat/openai-responses.md](../../../frontend/src/content/docs/zh/api/chat/openai-responses.md) |
| A | 已有文档/许可 | 150 | 0 | [frontend/src/content/docs/zh/api/image/openai-image.md](../../../frontend/src/content/docs/zh/api/image/openai-image.md) |
| A | 已有文档/许可 | 27 | 0 | [frontend/src/content/docs/zh/api/protocols.md](../../../frontend/src/content/docs/zh/api/protocols.md) |
| A | 已有文档/许可 | 34 | 0 | [frontend/src/content/docs/zh/api/query/embeddings.md](../../../frontend/src/content/docs/zh/api/query/embeddings.md) |
| A | 已有文档/许可 | 51 | 0 | [frontend/src/content/docs/zh/api/query/models.md](../../../frontend/src/content/docs/zh/api/query/models.md) |
| A | 已有文档/许可 | 59 | 0 | [frontend/src/content/docs/zh/api/query/token-count.md](../../../frontend/src/content/docs/zh/api/query/token-count.md) |
| A | 已有文档/许可 | 119 | 0 | [frontend/src/content/docs/zh/apps/codex.md](../../../frontend/src/content/docs/zh/apps/codex.md) |
| A | 已有文档/许可 | 70 | 0 | [frontend/src/content/docs/zh/apps/console.md](../../../frontend/src/content/docs/zh/apps/console.md) |
| A | 已有文档/许可 | 87 | 0 | [frontend/src/content/docs/zh/apps/image-skills.md](../../../frontend/src/content/docs/zh/apps/image-skills.md) |
| A | 已有文档/许可 | 27 | 0 | [frontend/src/content/docs/zh/apps/index.md](../../../frontend/src/content/docs/zh/apps/index.md) |
| A | 已有文档/许可 | 58 | 0 | [frontend/src/content/docs/zh/apps/session-recovery-claude.md](../../../frontend/src/content/docs/zh/apps/session-recovery-claude.md) |
| A | 已有文档/许可 | 76 | 0 | [frontend/src/content/docs/zh/apps/session-recovery-codex.md](../../../frontend/src/content/docs/zh/apps/session-recovery-codex.md) |
| A | 已有文档/许可 | 26 | 0 | [frontend/src/content/docs/zh/authentication.md](../../../frontend/src/content/docs/zh/authentication.md) |
| A | 已有文档/许可 | 18 | 0 | [frontend/src/content/docs/zh/downloads.md](../../../frontend/src/content/docs/zh/downloads.md) |
| A | 已有文档/许可 | 21 | 0 | [frontend/src/content/docs/zh/errors.md](../../../frontend/src/content/docs/zh/errors.md) |
| A | 已有文档/许可 | 19 | 0 | [frontend/src/content/docs/zh/limits.md](../../../frontend/src/content/docs/zh/limits.md) |
| A | 已有文档/许可 | 24 | 0 | [frontend/src/content/docs/zh/pricing.md](../../../frontend/src/content/docs/zh/pricing.md) |
| A | 已有文档/许可 | 34 | 0 | [frontend/src/content/docs/zh/quickstart.md](../../../frontend/src/content/docs/zh/quickstart.md) |
| M | 业务代码 | 1 | 1 | [frontend/src/features/channel-monitor-v2/RelayPulseMatrix.vue](../../../frontend/src/features/channel-monitor-v2/RelayPulseMatrix.vue) |
| A | 业务代码 | 37 | 0 | [frontend/src/utils/legalDocumentIcon.ts](../../../frontend/src/utils/legalDocumentIcon.ts) |
| M | 业务代码 | 251 | 269 | [frontend/src/views/HomeView.vue](../../../frontend/src/views/HomeView.vue) |
| M | 业务代码 | 5 | 5 | [frontend/src/views/NotFoundView.vue](../../../frontend/src/views/NotFoundView.vue) |
| M | 测试/夹具 | 50 | 3 | [frontend/src/views/__tests__/HomeView.compact.spec.ts](../../../frontend/src/views/__tests__/HomeView.compact.spec.ts) |
| M | 业务代码 | 5 | 3 | [frontend/src/views/auth/EmailVerifyView.vue](../../../frontend/src/views/auth/EmailVerifyView.vue) |
| M | 业务代码 | 3 | 3 | [frontend/src/views/auth/LoginView.vue](../../../frontend/src/views/auth/LoginView.vue) |
| M | 业务代码 | 5 | 3 | [frontend/src/views/auth/RegisterView.vue](../../../frontend/src/views/auth/RegisterView.vue) |
| A | 业务代码 | 202 | 0 | [frontend/src/views/docs/DocsArticleView.vue](../../../frontend/src/views/docs/DocsArticleView.vue) |
| A | 业务代码 | 89 | 0 | [frontend/src/views/docs/DocsHomeView.vue](../../../frontend/src/views/docs/DocsHomeView.vue) |
| A | 业务代码 | 108 | 0 | [frontend/src/views/docs/DocsModelView.vue](../../../frontend/src/views/docs/DocsModelView.vue) |
| A | 业务代码 | 52 | 0 | [frontend/src/views/docs/DocsModelsView.vue](../../../frontend/src/views/docs/DocsModelsView.vue) |
| A | 业务代码 | 166 | 0 | [frontend/src/views/docs/DocsView.vue](../../../frontend/src/views/docs/DocsView.vue) |
| M | 业务代码 | 15 | 17 | [frontend/src/views/public/LegalDocumentView.vue](../../../frontend/src/views/public/LegalDocumentView.vue) |
| M | 业务代码 | 27 | 8 | [frontend/src/views/setup/SetupWizardView.vue](../../../frontend/src/views/setup/SetupWizardView.vue) |
| M | 业务代码 | 20 | 68 | [frontend/src/views/user/BatchImageGuideView.vue](../../../frontend/src/views/user/BatchImageGuideView.vue) |
| M | 业务代码 | 1 | 1 | [frontend/src/views/user/ChannelStatusV2View.vue](../../../frontend/src/views/user/ChannelStatusV2View.vue) |
