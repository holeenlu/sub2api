# 16 · KDAN 品牌、文档站与公共界面

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`feat(site): preserve final KDAN branding and presentation`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. main 为 KDAN 品牌基线；品牌名称/Logo/默认域名/文档/下载/法律与部署差异保留在当前明确文件中，运行时站点设置优先于品牌默认值。TapModels 和 tokensavy 仍是分别部署的网站，不共享用户或站点数据库。

2. 保留首页展示、紧凑首页、文档站导航和代码示例、模型页面、法律说明、登录/注册协议及用户资料的当前实现；删除的宣传资产也按最终树保留删除状态。

3. 可用渠道开关只控制用户 /available-channels 入口和用户查询；管理端渠道管理/定价入口保留。渠道监控开关控制监控运行和用户渠道状态入口；不得将两个功能开关混为一项。

4. 模型广场、Channel Monitor、Passkey 等原生功能继续存在；只有相对固定基线的品牌、授权、安全或展示差异计入本清单，不能把整个原生模块宣称为 fork 新增。

5. 公共内容、Logo/链接处理、图标和弹窗体验保持当前代码；品牌适配不可覆盖共享权限、计费和安全约束。

此次 main 仍代表 KDAN，另外两个站点保持独立品牌、部署与数据。文档站、法律页、默认链接、模型展示、主题及原组件行为按当前最终文件保留；未提交的旧调查文档不覆盖这些已交付文件。

## 入口与配置归属

首页、/docs、/model-plaza、用户个人区、frontend/src/config/brand.ts、backend/internal/service/brand.go。

## 最终实现方案

1. 公共组件、文档导航与 brand.go/brand.ts、运行时站点设置承载现有体验；运行时配置优先。

2. 用户可用渠道开关与管理端渠道管理/定价入口分开；保留官方已有 Model Plaza、监控和认证功能。

3. 品牌差异以当前三棵最终树为准，后续品牌提交只覆盖清单中的名称、资源、域名、商业/法律和部署差异。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

品牌常量/资源、文档例子、可用渠道与导航相关现有测试；重组不改变已发布页面内容。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| asset | 46 | 82 | 22 | 43 |
| documentation | 10 | 278 | 771 | 0 |
| source | 147 | 7759 | 485 | 0 |
| test | 19 | 470 | 42 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 13 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | documentation | 5 | 5 | 16 | [DEV_GUIDE.md](https://github.com/holeenlu/sub2api/blob/main/DEV_GUIDE.md) |
| M | documentation | 47 | 255 | 12,16 | [README.md](https://github.com/holeenlu/sub2api/blob/main/README.md) |
| M | documentation | 35 | 251 | 16 | [README_CN.md](https://github.com/holeenlu/sub2api/blob/main/README_CN.md) |
| M | documentation | 33 | 249 | 16 | [README_JA.md](https://github.com/holeenlu/sub2api/blob/main/README_JA.md) |
| M | asset | 22 | 11 | 16 | [assets/logo.svg](https://github.com/holeenlu/sub2api/blob/main/assets/logo.svg) |
| D | asset | - | - | 16 | `assets/partners/logos/RoxyBrowser.png` |
| D | asset | - | - | 16 | `assets/partners/logos/aigocode.png` |
| D | asset | - | - | 16 | `assets/partners/logos/apikey-fun.png` |
| D | asset | - | - | 16 | `assets/partners/logos/apimart.jpg` |
| D | asset | - | - | 16 | `assets/partners/logos/axisnow.jpg` |
| D | asset | - | - | 16 | `assets/partners/logos/bestproxy.png` |
| D | asset | - | - | 16 | `assets/partners/logos/bmoplus.jpg` |
| D | asset | - | - | 16 | `assets/partners/logos/cctk.jpg` |
| D | asset | - | - | 16 | `assets/partners/logos/codex-everywhere.jpg` |
| D | asset | - | - | 16 | `assets/partners/logos/ctok.png` |
| D | asset | - | - | 16 | `assets/partners/logos/duckip.png` |
| D | asset | - | - | 16 | `assets/partners/logos/etok.png` |
| D | asset | - | - | 16 | `assets/partners/logos/fastaitoken.jpg` |
| D | asset | - | - | 16 | `assets/partners/logos/fennoai.jpg` |
| D | asset | - | - | 16 | `assets/partners/logos/haoai.png` |
| D | asset | - | - | 16 | `assets/partners/logos/lanox.jpg` |
| D | asset | - | - | 16 | `assets/partners/logos/modelflare.png` |
| D | asset | - | - | 16 | `assets/partners/logos/nagora.png` |
| D | asset | - | - | 16 | `assets/partners/logos/openmodel.jpg` |
| D | asset | - | - | 16 | `assets/partners/logos/pateway.png` |
| D | asset | - | - | 16 | `assets/partners/logos/pincc-logo.png` |
| D | asset | - | - | 16 | `assets/partners/logos/poixe.png` |
| D | asset | - | - | 16 | `assets/partners/logos/pptoken.png` |
| D | asset | - | - | 16 | `assets/partners/logos/qiniu.jpg` |
| D | asset | - | - | 16 | `assets/partners/logos/rapidproxy.jpg` |
| D | asset | - | - | 16 | `assets/partners/logos/runapi.png` |
| D | asset | - | - | 16 | `assets/partners/logos/silkapi.png` |
| D | asset | - | - | 16 | `assets/partners/logos/swiftprox.png` |
| D | asset | - | - | 16 | `assets/partners/logos/veilx.png` |
| A | asset | 38 | 0 | 16 | [assets/wordmark.svg](https://github.com/holeenlu/sub2api/blob/main/assets/wordmark.svg) |
| A | source | 32 | 0 | 16 | [backend/internal/service/brand.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/brand.go) |
| M | source | 64 | 7 | 16 | [backend/internal/service/model_plaza_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/model_plaza_service.go) |
| M | test | 45 | 0 | 16 | [backend/internal/service/model_plaza_service_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/model_plaza_service_test.go) |
| A | test | 73 | 0 | 16 | [backend/internal/setup/bootstrap_security_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/setup/bootstrap_security_test.go) |
| M | source | 3 | 3 | 16 | [backend/internal/setup/cli.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/setup/cli.go) |
| M | source | 24 | 4 | 16 | [backend/internal/setup/handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/setup/handler.go) |
| M | source | 4 | 4 | 16 | [backend/internal/setup/setup.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/setup/setup.go) |
| M | test | 22 | 10 | 16 | [backend/internal/setup/setup_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/setup/setup_test.go) |
| M | source | 8 | 1 | 16 | [backend/internal/web/embed_on.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/web/embed_on.go) |
| M | test | 9 | 9 | 16 | [backend/internal/web/embed_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/web/embed_test.go) |
| A | documentation | 49 | 0 | 16 | [docs/MODEL_PLAZA_CATALOG.md](https://github.com/holeenlu/sub2api/blob/main/docs/MODEL_PLAZA_CATALOG.md) |
| M | documentation | 5 | 5 | 16 | [docs/legal/admin-compliance.en.md](https://github.com/holeenlu/sub2api/blob/main/docs/legal/admin-compliance.en.md) |
| A | documentation | 49 | 0 | 16 | [docs/legal/admin-compliance.ja.md](https://github.com/holeenlu/sub2api/blob/main/docs/legal/admin-compliance.ja.md) |
| A | documentation | 49 | 0 | 16 | [docs/legal/admin-compliance.zh-TW.md](https://github.com/holeenlu/sub2api/blob/main/docs/legal/admin-compliance.zh-TW.md) |
| M | documentation | 5 | 5 | 16 | [docs/legal/admin-compliance.zh.md](https://github.com/holeenlu/sub2api/blob/main/docs/legal/admin-compliance.zh.md) |
| M | documentation | 1 | 1 | 16 | [frontend/index.html](https://github.com/holeenlu/sub2api/blob/main/frontend/index.html) |
| A | source | 13 | 0 | 16 | [frontend/public/docs-assets/README.md](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/README.md) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/client-claude-en.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/client-claude-en.png) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/client-claude-windows.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/client-claude-windows.png) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/client-claude-zh-TW.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/client-claude-zh-TW.png) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/client-claude-zh.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/client-claude-zh.png) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/client-claude.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/client-claude.png) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/client-codex-apikey.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/client-codex-apikey.png) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/client-codex-en.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/client-codex-en.png) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/client-codex-zh-TW.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/client-codex-zh-TW.png) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/client-codex-zh.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/client-codex-zh.png) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/client-codex.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/client-codex.png) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/create-api-key-en.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/create-api-key-en.png) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/create-api-key-zh-TW.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/create-api-key-zh-TW.png) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/create-api-key-zh.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/create-api-key-zh.png) |
| A | asset | - | - | 16 | [frontend/public/docs-assets/create-api-key.png](https://github.com/holeenlu/sub2api/blob/main/frontend/public/docs-assets/create-api-key.png) |
| M | asset | 22 | 11 | 16 | [frontend/public/logo.svg](https://github.com/holeenlu/sub2api/blob/main/frontend/public/logo.svg) |
| M | source | 10 | 1 | 16 | [frontend/src/components/AliyunCaptchaWidget.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/AliyunCaptchaWidget.vue) |
| M | source | 10 | 1 | 16 | [frontend/src/components/TencentCaptchaGate.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/TencentCaptchaGate.vue) |
| M | test | 4 | 2 | 16 | [frontend/src/components/admin/group/__tests__/GroupI18nField.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/group/__tests__/GroupI18nField.spec.ts) |
| M | source | 7 | 21 | 16 | [frontend/src/components/auth/LoginAgreementPrompt.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/auth/LoginAgreementPrompt.vue) |
| M | source | 2 | 8 | 16 | [frontend/src/components/auth/WechatOAuthSection.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/auth/WechatOAuthSection.vue) |
| M | test | 0 | 1 | 16 | [frontend/src/components/auth/__tests__/WechatOAuthSection.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/auth/__tests__/WechatOAuthSection.spec.ts) |
| M | source | 20 | 13 | 16 | [frontend/src/components/common/BaseDialog.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/BaseDialog.vue) |
| M | source | 4 | 3 | 12,16 | [frontend/src/components/common/DateRangePicker.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/DateRangePicker.vue) |
| M | source | 1 | 1 | 16 | [frontend/src/components/common/PlatformIcon.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/PlatformIcon.vue) |
| M | source | 1 | 0 | 16 | [frontend/src/components/common/PlatformTypeBadge.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/PlatformTypeBadge.vue) |
| D | source | 0 | 18 | 16 | `frontend/src/components/common/ProxyAdBanner.vue` |
| M | source | 4 | 1 | 12,16 | [frontend/src/components/common/Select.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/Select.vue) |
| M | source | 3 | 1 | 16 | [frontend/src/components/common/Toast.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/Toast.vue) |
| M | test | 5 | 4 | 16 | [frontend/src/components/common/__tests__/BaseDialog.escape.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/__tests__/BaseDialog.escape.spec.ts) |
| M | test | 3 | 1 | 16 | [frontend/src/components/common/__tests__/BaseDialog.ids.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/__tests__/BaseDialog.ids.spec.ts) |
| M | test | 2 | 1 | 16 | [frontend/src/components/common/__tests__/BaseDialog.scrollLock.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/__tests__/BaseDialog.scrollLock.spec.ts) |
| M | test | 14 | 1 | 16 | [frontend/src/components/common/__tests__/BaseDialog.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/__tests__/BaseDialog.spec.ts) |
| A | source | 53 | 0 | 16 | [frontend/src/components/docs/DocsCodeTabs.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/docs/DocsCodeTabs.vue) |
| A | source | 25 | 0 | 16 | [frontend/src/components/docs/DocsGroupSelect.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/docs/DocsGroupSelect.vue) |
| A | source | 26 | 0 | 16 | [frontend/src/components/docs/DocsPricingTable.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/docs/DocsPricingTable.vue) |
| A | source | 36 | 0 | 16 | [frontend/src/components/docs/DocsSidebar.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/docs/DocsSidebar.vue) |
| A | test | 31 | 0 | 16 | [frontend/src/components/docs/__tests__/DocsPricingTable.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/docs/__tests__/DocsPricingTable.spec.ts) |
| A | source | 44 | 0 | 16 | [frontend/src/components/home/AgentToolCards.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/home/AgentToolCards.vue) |
| A | source | 203 | 0 | 16 | [frontend/src/components/home/ArchitectureDiagram.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/home/ArchitectureDiagram.vue) |
| A | source | 41 | 0 | 16 | [frontend/src/components/home/DiagramNode.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/home/DiagramNode.vue) |
| A | source | 160 | 0 | 16 | [frontend/src/components/home/GatewayShowcase.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/home/GatewayShowcase.vue) |
| A | source | 37 | 0 | 16 | [frontend/src/components/home/MechanismList.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/home/MechanismList.vue) |
| A | source | 94 | 0 | 16 | [frontend/src/components/home/ModelShowcase.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/home/ModelShowcase.vue) |
| A | test | 41 | 0 | 16 | [frontend/src/components/home/__tests__/ArchitectureDiagram.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/home/__tests__/ArchitectureDiagram.spec.ts) |
| A | test | 86 | 0 | 16 | [frontend/src/components/home/__tests__/ModelShowcase.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/home/__tests__/ModelShowcase.spec.ts) |
| A | source | 42 | 0 | 16 | [frontend/src/components/home/mechanisms.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/home/mechanisms.ts) |
| A | source | 49 | 0 | 16 | [frontend/src/components/home/officialModels.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/home/officialModels.ts) |
| M | source | 7 | 6 | 16 | [frontend/src/components/layout/AppHeader.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/layout/AppHeader.vue) |
| M | source | 8 | 2 | 16 | [frontend/src/components/layout/AppLayout.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/layout/AppLayout.vue) |
| M | source | 14 | 3 | 12,16 | [frontend/src/components/layout/AppSidebar.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/layout/AppSidebar.vue) |
| M | source | 6 | 3 | 16 | [frontend/src/components/layout/AuthLayout.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/layout/AuthLayout.vue) |
| M | source | 1 | 1 | 16 | [frontend/src/components/layout/README.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/layout/README.md) |
| M | test | 0 | 9 | 16 | [frontend/src/components/layout/__tests__/docUrlSanitization.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/layout/__tests__/docUrlSanitization.spec.ts) |
| M | source | 1 | 1 | 16 | [frontend/src/components/modelPlaza/PlazaNavBar.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/modelPlaza/PlazaNavBar.vue) |
| M | source | 2 | 2 | 16 | [frontend/src/components/user/profile/ProfileInfoCard.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/user/profile/ProfileInfoCard.vue) |
| M | test | 0 | 1 | 16 | [frontend/src/components/user/profile/__tests__/ProfileIdentityBindingsSection.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/user/profile/__tests__/ProfileIdentityBindingsSection.spec.ts) |
| A | source | 38 | 0 | 16 | [frontend/src/composables/useModelPlaza.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/composables/useModelPlaza.ts) |
| A | source | 74 | 0 | 16 | [frontend/src/config/brand.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/config/brand.ts) |
| A | test | 20 | 0 | 16 | [frontend/src/content/docs/__tests__/examples.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/__tests__/examples.spec.ts) |
| A | test | 21 | 0 | 16 | [frontend/src/content/docs/__tests__/navigation.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/__tests__/navigation.spec.ts) |
| A | test | 44 | 0 | 16 | [frontend/src/content/docs/__tests__/pricingSnapshot.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/__tests__/pricingSnapshot.spec.ts) |
| A | source | 102 | 0 | 16 | [frontend/src/content/docs/en/api/chat/anthropic-messages.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/api/chat/anthropic-messages.md) |
| A | source | 64 | 0 | 16 | [frontend/src/content/docs/en/api/chat/gemini-native.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/api/chat/gemini-native.md) |
| A | source | 47 | 0 | 16 | [frontend/src/content/docs/en/api/chat/grok-native.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/api/chat/grok-native.md) |
| A | source | 105 | 0 | 16 | [frontend/src/content/docs/en/api/chat/openai-chat.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/api/chat/openai-chat.md) |
| A | source | 107 | 0 | 16 | [frontend/src/content/docs/en/api/chat/openai-responses.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/api/chat/openai-responses.md) |
| A | source | 112 | 0 | 16 | [frontend/src/content/docs/en/api/image/openai-image.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/api/image/openai-image.md) |
| A | source | 27 | 0 | 16 | [frontend/src/content/docs/en/api/protocols.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/api/protocols.md) |
| A | source | 34 | 0 | 16 | [frontend/src/content/docs/en/api/query/embeddings.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/api/query/embeddings.md) |
| A | source | 44 | 0 | 16 | [frontend/src/content/docs/en/api/query/models.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/api/query/models.md) |
| A | source | 59 | 0 | 16 | [frontend/src/content/docs/en/api/query/token-count.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/api/query/token-count.md) |
| A | source | 113 | 0 | 16 | [frontend/src/content/docs/en/apps/codex.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/apps/codex.md) |
| A | source | 70 | 0 | 16 | [frontend/src/content/docs/en/apps/console.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/apps/console.md) |
| A | source | 80 | 0 | 16 | [frontend/src/content/docs/en/apps/image-skills.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/apps/image-skills.md) |
| A | source | 27 | 0 | 16 | [frontend/src/content/docs/en/apps/index.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/apps/index.md) |
| A | source | 58 | 0 | 16 | [frontend/src/content/docs/en/apps/session-recovery-claude.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/apps/session-recovery-claude.md) |
| A | source | 76 | 0 | 16 | [frontend/src/content/docs/en/apps/session-recovery-codex.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/apps/session-recovery-codex.md) |
| A | source | 21 | 0 | 16 | [frontend/src/content/docs/en/authentication.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/authentication.md) |
| A | source | 18 | 0 | 16 | [frontend/src/content/docs/en/downloads.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/downloads.md) |
| A | source | 15 | 0 | 16 | [frontend/src/content/docs/en/errors.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/errors.md) |
| A | source | 9 | 0 | 16 | [frontend/src/content/docs/en/limits.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/limits.md) |
| A | source | 24 | 0 | 16 | [frontend/src/content/docs/en/pricing.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/pricing.md) |
| A | source | 20 | 0 | 16 | [frontend/src/content/docs/en/quickstart.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/quickstart.md) |
| A | source | 38 | 0 | 16 | [frontend/src/content/docs/examples.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/examples.ts) |
| A | source | 102 | 0 | 16 | [frontend/src/content/docs/ja/api/chat/anthropic-messages.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/api/chat/anthropic-messages.md) |
| A | source | 64 | 0 | 16 | [frontend/src/content/docs/ja/api/chat/gemini-native.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/api/chat/gemini-native.md) |
| A | source | 47 | 0 | 16 | [frontend/src/content/docs/ja/api/chat/grok-native.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/api/chat/grok-native.md) |
| A | source | 105 | 0 | 16 | [frontend/src/content/docs/ja/api/chat/openai-chat.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/api/chat/openai-chat.md) |
| A | source | 107 | 0 | 16 | [frontend/src/content/docs/ja/api/chat/openai-responses.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/api/chat/openai-responses.md) |
| A | source | 112 | 0 | 16 | [frontend/src/content/docs/ja/api/image/openai-image.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/api/image/openai-image.md) |
| A | source | 27 | 0 | 16 | [frontend/src/content/docs/ja/api/protocols.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/api/protocols.md) |
| A | source | 34 | 0 | 16 | [frontend/src/content/docs/ja/api/query/embeddings.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/api/query/embeddings.md) |
| A | source | 44 | 0 | 16 | [frontend/src/content/docs/ja/api/query/models.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/api/query/models.md) |
| A | source | 59 | 0 | 16 | [frontend/src/content/docs/ja/api/query/token-count.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/api/query/token-count.md) |
| A | source | 113 | 0 | 16 | [frontend/src/content/docs/ja/apps/codex.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/apps/codex.md) |
| A | source | 70 | 0 | 16 | [frontend/src/content/docs/ja/apps/console.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/apps/console.md) |
| A | source | 80 | 0 | 16 | [frontend/src/content/docs/ja/apps/image-skills.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/apps/image-skills.md) |
| A | source | 27 | 0 | 16 | [frontend/src/content/docs/ja/apps/index.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/apps/index.md) |
| A | source | 58 | 0 | 16 | [frontend/src/content/docs/ja/apps/session-recovery-claude.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/apps/session-recovery-claude.md) |
| A | source | 76 | 0 | 16 | [frontend/src/content/docs/ja/apps/session-recovery-codex.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/apps/session-recovery-codex.md) |
| A | source | 21 | 0 | 16 | [frontend/src/content/docs/ja/authentication.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/authentication.md) |
| A | source | 18 | 0 | 16 | [frontend/src/content/docs/ja/downloads.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/downloads.md) |
| A | source | 15 | 0 | 16 | [frontend/src/content/docs/ja/errors.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/errors.md) |
| A | source | 9 | 0 | 16 | [frontend/src/content/docs/ja/limits.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/limits.md) |
| A | source | 24 | 0 | 16 | [frontend/src/content/docs/ja/pricing.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/pricing.md) |
| A | source | 20 | 0 | 16 | [frontend/src/content/docs/ja/quickstart.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/quickstart.md) |
| A | source | 92 | 0 | 16 | [frontend/src/content/docs/modelCatalog.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/modelCatalog.ts) |
| A | source | 73 | 0 | 16 | [frontend/src/content/docs/nav.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/nav.ts) |
| A | source | 9 | 0 | 16 | [frontend/src/content/docs/pricingSnapshot.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/pricingSnapshot.ts) |
| A | source | 64 | 0 | 16 | [frontend/src/content/docs/types.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/types.ts) |
| A | source | 127 | 0 | 16 | [frontend/src/content/docs/zh-TW/api/chat/anthropic-messages.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/api/chat/anthropic-messages.md) |
| A | source | 64 | 0 | 16 | [frontend/src/content/docs/zh-TW/api/chat/gemini-native.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/api/chat/gemini-native.md) |
| A | source | 47 | 0 | 16 | [frontend/src/content/docs/zh-TW/api/chat/grok-native.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/api/chat/grok-native.md) |
| A | source | 140 | 0 | 16 | [frontend/src/content/docs/zh-TW/api/chat/openai-chat.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/api/chat/openai-chat.md) |
| A | source | 150 | 0 | 16 | [frontend/src/content/docs/zh-TW/api/chat/openai-responses.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/api/chat/openai-responses.md) |
| A | source | 150 | 0 | 16 | [frontend/src/content/docs/zh-TW/api/image/openai-image.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/api/image/openai-image.md) |
| A | source | 27 | 0 | 16 | [frontend/src/content/docs/zh-TW/api/protocols.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/api/protocols.md) |
| A | source | 34 | 0 | 16 | [frontend/src/content/docs/zh-TW/api/query/embeddings.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/api/query/embeddings.md) |
| A | source | 51 | 0 | 16 | [frontend/src/content/docs/zh-TW/api/query/models.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/api/query/models.md) |
| A | source | 59 | 0 | 16 | [frontend/src/content/docs/zh-TW/api/query/token-count.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/api/query/token-count.md) |
| A | source | 119 | 0 | 16 | [frontend/src/content/docs/zh-TW/apps/codex.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/apps/codex.md) |
| A | source | 70 | 0 | 16 | [frontend/src/content/docs/zh-TW/apps/console.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/apps/console.md) |
| A | source | 87 | 0 | 16 | [frontend/src/content/docs/zh-TW/apps/image-skills.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/apps/image-skills.md) |
| A | source | 27 | 0 | 16 | [frontend/src/content/docs/zh-TW/apps/index.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/apps/index.md) |
| A | source | 58 | 0 | 16 | [frontend/src/content/docs/zh-TW/apps/session-recovery-claude.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/apps/session-recovery-claude.md) |
| A | source | 76 | 0 | 16 | [frontend/src/content/docs/zh-TW/apps/session-recovery-codex.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/apps/session-recovery-codex.md) |
| A | source | 26 | 0 | 16 | [frontend/src/content/docs/zh-TW/authentication.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/authentication.md) |
| A | source | 18 | 0 | 16 | [frontend/src/content/docs/zh-TW/downloads.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/downloads.md) |
| A | source | 21 | 0 | 16 | [frontend/src/content/docs/zh-TW/errors.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/errors.md) |
| A | source | 19 | 0 | 16 | [frontend/src/content/docs/zh-TW/limits.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/limits.md) |
| A | source | 24 | 0 | 16 | [frontend/src/content/docs/zh-TW/pricing.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/pricing.md) |
| A | source | 34 | 0 | 16 | [frontend/src/content/docs/zh-TW/quickstart.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/quickstart.md) |
| A | source | 127 | 0 | 16 | [frontend/src/content/docs/zh/api/chat/anthropic-messages.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/api/chat/anthropic-messages.md) |
| A | source | 64 | 0 | 16 | [frontend/src/content/docs/zh/api/chat/gemini-native.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/api/chat/gemini-native.md) |
| A | source | 47 | 0 | 16 | [frontend/src/content/docs/zh/api/chat/grok-native.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/api/chat/grok-native.md) |
| A | source | 140 | 0 | 16 | [frontend/src/content/docs/zh/api/chat/openai-chat.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/api/chat/openai-chat.md) |
| A | source | 150 | 0 | 16 | [frontend/src/content/docs/zh/api/chat/openai-responses.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/api/chat/openai-responses.md) |
| A | source | 150 | 0 | 16 | [frontend/src/content/docs/zh/api/image/openai-image.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/api/image/openai-image.md) |
| A | source | 27 | 0 | 16 | [frontend/src/content/docs/zh/api/protocols.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/api/protocols.md) |
| A | source | 34 | 0 | 16 | [frontend/src/content/docs/zh/api/query/embeddings.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/api/query/embeddings.md) |
| A | source | 51 | 0 | 16 | [frontend/src/content/docs/zh/api/query/models.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/api/query/models.md) |
| A | source | 59 | 0 | 16 | [frontend/src/content/docs/zh/api/query/token-count.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/api/query/token-count.md) |
| A | source | 119 | 0 | 16 | [frontend/src/content/docs/zh/apps/codex.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/apps/codex.md) |
| A | source | 70 | 0 | 16 | [frontend/src/content/docs/zh/apps/console.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/apps/console.md) |
| A | source | 87 | 0 | 16 | [frontend/src/content/docs/zh/apps/image-skills.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/apps/image-skills.md) |
| A | source | 27 | 0 | 16 | [frontend/src/content/docs/zh/apps/index.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/apps/index.md) |
| A | source | 58 | 0 | 16 | [frontend/src/content/docs/zh/apps/session-recovery-claude.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/apps/session-recovery-claude.md) |
| A | source | 76 | 0 | 16 | [frontend/src/content/docs/zh/apps/session-recovery-codex.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/apps/session-recovery-codex.md) |
| A | source | 26 | 0 | 16 | [frontend/src/content/docs/zh/authentication.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/authentication.md) |
| A | source | 18 | 0 | 16 | [frontend/src/content/docs/zh/downloads.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/downloads.md) |
| A | source | 21 | 0 | 16 | [frontend/src/content/docs/zh/errors.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/errors.md) |
| A | source | 19 | 0 | 16 | [frontend/src/content/docs/zh/limits.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/limits.md) |
| A | source | 24 | 0 | 16 | [frontend/src/content/docs/zh/pricing.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/pricing.md) |
| A | source | 34 | 0 | 16 | [frontend/src/content/docs/zh/quickstart.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/quickstart.md) |
| A | source | 37 | 0 | 16 | [frontend/src/utils/legalDocumentIcon.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/utils/legalDocumentIcon.ts) |
| M | source | 251 | 269 | 16 | [frontend/src/views/HomeView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/HomeView.vue) |
| M | source | 5 | 5 | 16 | [frontend/src/views/NotFoundView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/NotFoundView.vue) |
| M | test | 50 | 3 | 16 | [frontend/src/views/__tests__/HomeView.compact.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/__tests__/HomeView.compact.spec.ts) |
| M | source | 5 | 3 | 16 | [frontend/src/views/auth/EmailVerifyView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/auth/EmailVerifyView.vue) |
| M | source | 9 | 6 | 12,16 | [frontend/src/views/auth/LoginView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/auth/LoginView.vue) |
| M | source | 5 | 3 | 16 | [frontend/src/views/auth/RegisterView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/auth/RegisterView.vue) |
| A | source | 202 | 0 | 16 | [frontend/src/views/docs/DocsArticleView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/docs/DocsArticleView.vue) |
| A | source | 89 | 0 | 16 | [frontend/src/views/docs/DocsHomeView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/docs/DocsHomeView.vue) |
| A | source | 108 | 0 | 16 | [frontend/src/views/docs/DocsModelView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/docs/DocsModelView.vue) |
| A | source | 52 | 0 | 16 | [frontend/src/views/docs/DocsModelsView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/docs/DocsModelsView.vue) |
| A | source | 166 | 0 | 16 | [frontend/src/views/docs/DocsView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/docs/DocsView.vue) |
| M | source | 15 | 17 | 16 | [frontend/src/views/public/LegalDocumentView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/public/LegalDocumentView.vue) |
| M | source | 27 | 8 | 16 | [frontend/src/views/setup/SetupWizardView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/setup/SetupWizardView.vue) |
| M | source | 20 | 68 | 16 | [frontend/src/views/user/BatchImageGuideView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/user/BatchImageGuideView.vue) |
| M | source | 1 | 1 | 16 | [frontend/src/views/user/ChannelStatusV2View.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/user/ChannelStatusV2View.vue) |
