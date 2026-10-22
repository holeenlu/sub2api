# 13 · KDAN 品牌、文档站与用户界面

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。

## 最终需求

1. main 为 KDAN 品牌基线；品牌名称/Logo/默认域名/文档/下载/法律与部署差异保留在当前明确文件中，运行时站点设置优先于品牌默认值。TapModels 和 tokensavy 仍是分别部署的网站，不共享用户或站点数据库。

2. 保留首页展示、紧凑首页、文档站导航和代码示例、模型页面、法律说明、登录/注册协议及用户资料的当前实现；删除的宣传资产也按最终树保留删除状态。

3. 可用渠道开关只控制用户 /available-channels 入口和用户查询；管理端渠道管理/定价入口保留。渠道监控开关控制监控运行和用户渠道状态入口；不得将两个功能开关混为一项。

4. 模型广场、Channel Monitor、Passkey 等原生功能继续存在；只有相对固定基线的品牌、授权、安全或展示差异计入本清单，不能把整个原生模块宣称为 fork 新增。

5. 公共内容、Logo/链接处理、图标和弹窗体验保持当前代码；品牌适配不可覆盖共享权限、计费和安全约束。

## 入口与配置归属

首页、/docs、/model-plaza、用户个人区、frontend/src/config/brand.ts、backend/internal/service/brand.go。

## 最终实现方案

1. 公共组件、文档导航与 brand.go/brand.ts、运行时站点设置承载现有体验；运行时配置优先。

2. 用户可用渠道开关与管理端渠道管理/定价入口分开；保留官方已有 Model Plaza、监控和认证功能。

3. 品牌差异以当前三棵最终树为准，后续品牌提交只覆盖清单中的名称、资源、域名、商业/法律和部署差异。

## 边界、失败路径与依赖

没有额外文件归属组关联。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。上面描述现有最终行为，本次不重新实现业务。

## 验收要求

品牌常量/资源、文档例子、可用渠道与导航相关现有测试；重组不改变已发布页面内容。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | --- | --- | --- | --- |
| asset | 47 | 82 | 22 | 44 |
| documentation | 113 | 6642 | 824 | 0 |
| source | 46 | 1385 | 483 | 0 |
| test | 19 | 470 | 42 | 0 |

这是相对 v0.2.16 的累计定制差异，不是本次新写代码。删除/新增重命名分别计数；生成代码与测试单列。

## 来源提交

- `49b29ef158e6319cfd6b1227781307de5f8f870e`
- `6dc1165380ed5f93a5f63729eb5c3b8421d61126`
- `944fdb1eff8e7ea33ed70ff722b6822f962e9255`
- `a2988a14ca54b99b09a6f64024166d52fa8db132`
- `c6a92e1501a3ed5bb405c02bd541fcb59366bd28`
- `e37f32177a2b3f31414e8e2801d1f20678983fe5`
- `e4bf78f132acb2d09d3f604a4cde873f5ebd2d29`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | --- | --- | --- | --- |
| M | documentation | 5 | 5 | 13 | [DEV_GUIDE.md](../../../DEV_GUIDE.md) |
| M | documentation | 45 | 274 | 13 | [README.md](../../../README.md) |
| M | documentation | 35 | 268 | 13 | [README_CN.md](../../../README_CN.md) |
| M | documentation | 33 | 266 | 13 | [README_JA.md](../../../README_JA.md) |
| M | asset | 22 | 11 | 13 | [assets/logo.svg](../../../assets/logo.svg) |
| D | asset | - | - | 13 | [assets/partners/logos/RoxyBrowser.png](../../../assets/partners/logos/RoxyBrowser.png) |
| D | asset | - | - | 13 | [assets/partners/logos/aigocode.png](../../../assets/partners/logos/aigocode.png) |
| D | asset | - | - | 13 | [assets/partners/logos/aimzoon.jpg](../../../assets/partners/logos/aimzoon.jpg) |
| D | asset | - | - | 13 | [assets/partners/logos/apikey-fun.png](../../../assets/partners/logos/apikey-fun.png) |
| D | asset | - | - | 13 | [assets/partners/logos/apimart.jpg](../../../assets/partners/logos/apimart.jpg) |
| D | asset | - | - | 13 | [assets/partners/logos/axisnow.jpg](../../../assets/partners/logos/axisnow.jpg) |
| D | asset | - | - | 13 | [assets/partners/logos/bestproxy.png](../../../assets/partners/logos/bestproxy.png) |
| D | asset | - | - | 13 | [assets/partners/logos/bmoplus.jpg](../../../assets/partners/logos/bmoplus.jpg) |
| D | asset | - | - | 13 | [assets/partners/logos/cctk.jpg](../../../assets/partners/logos/cctk.jpg) |
| D | asset | - | - | 13 | [assets/partners/logos/codex-everywhere.jpg](../../../assets/partners/logos/codex-everywhere.jpg) |
| D | asset | - | - | 13 | [assets/partners/logos/ctok.png](../../../assets/partners/logos/ctok.png) |
| D | asset | - | - | 13 | [assets/partners/logos/duckip.png](../../../assets/partners/logos/duckip.png) |
| D | asset | - | - | 13 | [assets/partners/logos/etok.png](../../../assets/partners/logos/etok.png) |
| D | asset | - | - | 13 | [assets/partners/logos/fastaitoken.jpg](../../../assets/partners/logos/fastaitoken.jpg) |
| D | asset | - | - | 13 | [assets/partners/logos/fennoai.jpg](../../../assets/partners/logos/fennoai.jpg) |
| D | asset | - | - | 13 | [assets/partners/logos/haoai.png](../../../assets/partners/logos/haoai.png) |
| D | asset | - | - | 13 | [assets/partners/logos/lanox.jpg](../../../assets/partners/logos/lanox.jpg) |
| D | asset | - | - | 13 | [assets/partners/logos/nagora.png](../../../assets/partners/logos/nagora.png) |
| D | asset | - | - | 13 | [assets/partners/logos/openmodel.jpg](../../../assets/partners/logos/openmodel.jpg) |
| D | asset | - | - | 13 | [assets/partners/logos/pateway.png](../../../assets/partners/logos/pateway.png) |
| D | asset | - | - | 13 | [assets/partners/logos/pincc-logo.png](../../../assets/partners/logos/pincc-logo.png) |
| D | asset | - | - | 13 | [assets/partners/logos/poixe.png](../../../assets/partners/logos/poixe.png) |
| D | asset | - | - | 13 | [assets/partners/logos/pptoken.png](../../../assets/partners/logos/pptoken.png) |
| D | asset | - | - | 13 | [assets/partners/logos/proxy4free.png](../../../assets/partners/logos/proxy4free.png) |
| D | asset | - | - | 13 | [assets/partners/logos/qiniu.jpg](../../../assets/partners/logos/qiniu.jpg) |
| D | asset | - | - | 13 | [assets/partners/logos/rapidproxy.jpg](../../../assets/partners/logos/rapidproxy.jpg) |
| D | asset | - | - | 13 | [assets/partners/logos/runapi.png](../../../assets/partners/logos/runapi.png) |
| D | asset | - | - | 13 | [assets/partners/logos/silkapi.png](../../../assets/partners/logos/silkapi.png) |
| D | asset | - | - | 13 | [assets/partners/logos/swiftprox.png](../../../assets/partners/logos/swiftprox.png) |
| D | asset | - | - | 13 | [assets/partners/logos/veilx.png](../../../assets/partners/logos/veilx.png) |
| A | asset | 38 | 0 | 13 | [assets/wordmark.svg](../../../assets/wordmark.svg) |
| M | source | 3 | 2 | 13 | [backend/internal/handler/channel_monitor_v2_handler.go](../../../backend/internal/handler/channel_monitor_v2_handler.go) |
| A | source | 32 | 0 | 13 | [backend/internal/service/brand.go](../../../backend/internal/service/brand.go) |
| M | source | 64 | 7 | 13 | [backend/internal/service/model_plaza_service.go](../../../backend/internal/service/model_plaza_service.go) |
| M | test | 45 | 0 | 13 | [backend/internal/service/model_plaza_service_test.go](../../../backend/internal/service/model_plaza_service_test.go) |
| A | test | 73 | 0 | 13 | [backend/internal/setup/bootstrap_security_test.go](../../../backend/internal/setup/bootstrap_security_test.go) |
| M | source | 3 | 3 | 13 | [backend/internal/setup/cli.go](../../../backend/internal/setup/cli.go) |
| M | source | 24 | 4 | 13 | [backend/internal/setup/handler.go](../../../backend/internal/setup/handler.go) |
| M | source | 4 | 4 | 13 | [backend/internal/setup/setup.go](../../../backend/internal/setup/setup.go) |
| M | test | 22 | 10 | 13 | [backend/internal/setup/setup_test.go](../../../backend/internal/setup/setup_test.go) |
| M | source | 8 | 1 | 13 | [backend/internal/web/embed_on.go](../../../backend/internal/web/embed_on.go) |
| M | test | 9 | 9 | 13 | [backend/internal/web/embed_test.go](../../../backend/internal/web/embed_test.go) |
| A | documentation | 49 | 0 | 13 | [docs/MODEL_PLAZA_CATALOG.md](../../../docs/MODEL_PLAZA_CATALOG.md) |
| M | documentation | 5 | 5 | 13 | [docs/legal/admin-compliance.en.md](../../../docs/legal/admin-compliance.en.md) |
| A | documentation | 49 | 0 | 13 | [docs/legal/admin-compliance.ja.md](../../../docs/legal/admin-compliance.ja.md) |
| A | documentation | 49 | 0 | 13 | [docs/legal/admin-compliance.zh-TW.md](../../../docs/legal/admin-compliance.zh-TW.md) |
| M | documentation | 5 | 5 | 13 | [docs/legal/admin-compliance.zh.md](../../../docs/legal/admin-compliance.zh.md) |
| M | source | 1 | 1 | 13 | [frontend/index.html](../../../frontend/index.html) |
| A | documentation | 13 | 0 | 13 | [frontend/public/docs-assets/README.md](../../../frontend/public/docs-assets/README.md) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/client-claude-en.png](../../../frontend/public/docs-assets/client-claude-en.png) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/client-claude-windows.png](../../../frontend/public/docs-assets/client-claude-windows.png) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/client-claude-zh-TW.png](../../../frontend/public/docs-assets/client-claude-zh-TW.png) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/client-claude-zh.png](../../../frontend/public/docs-assets/client-claude-zh.png) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/client-claude.png](../../../frontend/public/docs-assets/client-claude.png) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/client-codex-apikey.png](../../../frontend/public/docs-assets/client-codex-apikey.png) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/client-codex-en.png](../../../frontend/public/docs-assets/client-codex-en.png) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/client-codex-zh-TW.png](../../../frontend/public/docs-assets/client-codex-zh-TW.png) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/client-codex-zh.png](../../../frontend/public/docs-assets/client-codex-zh.png) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/client-codex.png](../../../frontend/public/docs-assets/client-codex.png) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/create-api-key-en.png](../../../frontend/public/docs-assets/create-api-key-en.png) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/create-api-key-zh-TW.png](../../../frontend/public/docs-assets/create-api-key-zh-TW.png) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/create-api-key-zh.png](../../../frontend/public/docs-assets/create-api-key-zh.png) |
| A | asset | - | - | 13 | [frontend/public/docs-assets/create-api-key.png](../../../frontend/public/docs-assets/create-api-key.png) |
| M | asset | 22 | 11 | 13 | [frontend/public/logo.svg](../../../frontend/public/logo.svg) |
| M | source | 10 | 1 | 13 | [frontend/src/components/AliyunCaptchaWidget.vue](../../../frontend/src/components/AliyunCaptchaWidget.vue) |
| M | source | 10 | 1 | 13 | [frontend/src/components/TencentCaptchaGate.vue](../../../frontend/src/components/TencentCaptchaGate.vue) |
| M | test | 4 | 2 | 13 | [frontend/src/components/admin/group/__tests__/GroupI18nField.spec.ts](../../../frontend/src/components/admin/group/__tests__/GroupI18nField.spec.ts) |
| M | source | 7 | 21 | 13 | [frontend/src/components/auth/LoginAgreementPrompt.vue](../../../frontend/src/components/auth/LoginAgreementPrompt.vue) |
| M | source | 2 | 8 | 13 | [frontend/src/components/auth/WechatOAuthSection.vue](../../../frontend/src/components/auth/WechatOAuthSection.vue) |
| M | test | 0 | 1 | 13 | [frontend/src/components/auth/__tests__/WechatOAuthSection.spec.ts](../../../frontend/src/components/auth/__tests__/WechatOAuthSection.spec.ts) |
| M | source | 20 | 13 | 13 | [frontend/src/components/common/BaseDialog.vue](../../../frontend/src/components/common/BaseDialog.vue) |
| M | source | 2 | 1 | 13 | [frontend/src/components/common/DateRangePicker.vue](../../../frontend/src/components/common/DateRangePicker.vue) |
| M | source | 1 | 1 | 13 | [frontend/src/components/common/PlatformIcon.vue](../../../frontend/src/components/common/PlatformIcon.vue) |
| M | source | 1 | 0 | 13 | [frontend/src/components/common/PlatformTypeBadge.vue](../../../frontend/src/components/common/PlatformTypeBadge.vue) |
| D | source | 0 | 18 | 13 | [frontend/src/components/common/ProxyAdBanner.vue](../../../frontend/src/components/common/ProxyAdBanner.vue) |
| M | source | 1 | 1 | 13 | [frontend/src/components/common/Select.vue](../../../frontend/src/components/common/Select.vue) |
| M | source | 3 | 1 | 13 | [frontend/src/components/common/Toast.vue](../../../frontend/src/components/common/Toast.vue) |
| M | test | 5 | 4 | 13 | [frontend/src/components/common/__tests__/BaseDialog.escape.spec.ts](../../../frontend/src/components/common/__tests__/BaseDialog.escape.spec.ts) |
| M | test | 3 | 1 | 13 | [frontend/src/components/common/__tests__/BaseDialog.ids.spec.ts](../../../frontend/src/components/common/__tests__/BaseDialog.ids.spec.ts) |
| M | test | 2 | 1 | 13 | [frontend/src/components/common/__tests__/BaseDialog.scrollLock.spec.ts](../../../frontend/src/components/common/__tests__/BaseDialog.scrollLock.spec.ts) |
| M | test | 14 | 1 | 13 | [frontend/src/components/common/__tests__/BaseDialog.spec.ts](../../../frontend/src/components/common/__tests__/BaseDialog.spec.ts) |
| A | documentation | 53 | 0 | 13 | [frontend/src/components/docs/DocsCodeTabs.vue](../../../frontend/src/components/docs/DocsCodeTabs.vue) |
| A | documentation | 25 | 0 | 13 | [frontend/src/components/docs/DocsGroupSelect.vue](../../../frontend/src/components/docs/DocsGroupSelect.vue) |
| A | documentation | 26 | 0 | 13 | [frontend/src/components/docs/DocsPricingTable.vue](../../../frontend/src/components/docs/DocsPricingTable.vue) |
| A | documentation | 36 | 0 | 13 | [frontend/src/components/docs/DocsSidebar.vue](../../../frontend/src/components/docs/DocsSidebar.vue) |
| A | test | 31 | 0 | 13 | [frontend/src/components/docs/__tests__/DocsPricingTable.spec.ts](../../../frontend/src/components/docs/__tests__/DocsPricingTable.spec.ts) |
| A | source | 44 | 0 | 13 | [frontend/src/components/home/AgentToolCards.vue](../../../frontend/src/components/home/AgentToolCards.vue) |
| A | source | 203 | 0 | 13 | [frontend/src/components/home/ArchitectureDiagram.vue](../../../frontend/src/components/home/ArchitectureDiagram.vue) |
| A | source | 41 | 0 | 13 | [frontend/src/components/home/DiagramNode.vue](../../../frontend/src/components/home/DiagramNode.vue) |
| A | source | 160 | 0 | 13 | [frontend/src/components/home/GatewayShowcase.vue](../../../frontend/src/components/home/GatewayShowcase.vue) |
| A | source | 37 | 0 | 13 | [frontend/src/components/home/MechanismList.vue](../../../frontend/src/components/home/MechanismList.vue) |
| A | source | 94 | 0 | 13 | [frontend/src/components/home/ModelShowcase.vue](../../../frontend/src/components/home/ModelShowcase.vue) |
| A | test | 41 | 0 | 13 | [frontend/src/components/home/__tests__/ArchitectureDiagram.spec.ts](../../../frontend/src/components/home/__tests__/ArchitectureDiagram.spec.ts) |
| A | test | 86 | 0 | 13 | [frontend/src/components/home/__tests__/ModelShowcase.spec.ts](../../../frontend/src/components/home/__tests__/ModelShowcase.spec.ts) |
| A | source | 42 | 0 | 13 | [frontend/src/components/home/mechanisms.ts](../../../frontend/src/components/home/mechanisms.ts) |
| A | source | 49 | 0 | 13 | [frontend/src/components/home/officialModels.ts](../../../frontend/src/components/home/officialModels.ts) |
| M | source | 7 | 6 | 13 | [frontend/src/components/layout/AppHeader.vue](../../../frontend/src/components/layout/AppHeader.vue) |
| M | source | 8 | 2 | 13 | [frontend/src/components/layout/AppLayout.vue](../../../frontend/src/components/layout/AppLayout.vue) |
| M | source | 13 | 3 | 13 | [frontend/src/components/layout/AppSidebar.vue](../../../frontend/src/components/layout/AppSidebar.vue) |
| M | source | 6 | 3 | 13 | [frontend/src/components/layout/AuthLayout.vue](../../../frontend/src/components/layout/AuthLayout.vue) |
| M | documentation | 1 | 1 | 13 | [frontend/src/components/layout/README.md](../../../frontend/src/components/layout/README.md) |
| M | test | 0 | 9 | 13 | [frontend/src/components/layout/__tests__/docUrlSanitization.spec.ts](../../../frontend/src/components/layout/__tests__/docUrlSanitization.spec.ts) |
| M | source | 1 | 1 | 13 | [frontend/src/components/modelPlaza/PlazaNavBar.vue](../../../frontend/src/components/modelPlaza/PlazaNavBar.vue) |
| M | source | 2 | 2 | 13 | [frontend/src/components/user/profile/ProfileInfoCard.vue](../../../frontend/src/components/user/profile/ProfileInfoCard.vue) |
| M | test | 0 | 1 | 13 | [frontend/src/components/user/profile/__tests__/ProfileIdentityBindingsSection.spec.ts](../../../frontend/src/components/user/profile/__tests__/ProfileIdentityBindingsSection.spec.ts) |
| A | source | 38 | 0 | 13 | [frontend/src/composables/useModelPlaza.ts](../../../frontend/src/composables/useModelPlaza.ts) |
| A | source | 74 | 0 | 13 | [frontend/src/config/brand.ts](../../../frontend/src/config/brand.ts) |
| A | test | 20 | 0 | 13 | [frontend/src/content/docs/__tests__/examples.spec.ts](../../../frontend/src/content/docs/__tests__/examples.spec.ts) |
| A | test | 21 | 0 | 13 | [frontend/src/content/docs/__tests__/navigation.spec.ts](../../../frontend/src/content/docs/__tests__/navigation.spec.ts) |
| A | test | 44 | 0 | 13 | [frontend/src/content/docs/__tests__/pricingSnapshot.spec.ts](../../../frontend/src/content/docs/__tests__/pricingSnapshot.spec.ts) |
| A | documentation | 102 | 0 | 13 | [frontend/src/content/docs/en/api/chat/anthropic-messages.md](../../../frontend/src/content/docs/en/api/chat/anthropic-messages.md) |
| A | documentation | 64 | 0 | 13 | [frontend/src/content/docs/en/api/chat/gemini-native.md](../../../frontend/src/content/docs/en/api/chat/gemini-native.md) |
| A | documentation | 47 | 0 | 13 | [frontend/src/content/docs/en/api/chat/grok-native.md](../../../frontend/src/content/docs/en/api/chat/grok-native.md) |
| A | documentation | 105 | 0 | 13 | [frontend/src/content/docs/en/api/chat/openai-chat.md](../../../frontend/src/content/docs/en/api/chat/openai-chat.md) |
| A | documentation | 107 | 0 | 13 | [frontend/src/content/docs/en/api/chat/openai-responses.md](../../../frontend/src/content/docs/en/api/chat/openai-responses.md) |
| A | documentation | 112 | 0 | 13 | [frontend/src/content/docs/en/api/image/openai-image.md](../../../frontend/src/content/docs/en/api/image/openai-image.md) |
| A | documentation | 27 | 0 | 13 | [frontend/src/content/docs/en/api/protocols.md](../../../frontend/src/content/docs/en/api/protocols.md) |
| A | documentation | 34 | 0 | 13 | [frontend/src/content/docs/en/api/query/embeddings.md](../../../frontend/src/content/docs/en/api/query/embeddings.md) |
| A | documentation | 44 | 0 | 13 | [frontend/src/content/docs/en/api/query/models.md](../../../frontend/src/content/docs/en/api/query/models.md) |
| A | documentation | 59 | 0 | 13 | [frontend/src/content/docs/en/api/query/token-count.md](../../../frontend/src/content/docs/en/api/query/token-count.md) |
| A | documentation | 113 | 0 | 13 | [frontend/src/content/docs/en/apps/codex.md](../../../frontend/src/content/docs/en/apps/codex.md) |
| A | documentation | 70 | 0 | 13 | [frontend/src/content/docs/en/apps/console.md](../../../frontend/src/content/docs/en/apps/console.md) |
| A | documentation | 80 | 0 | 13 | [frontend/src/content/docs/en/apps/image-skills.md](../../../frontend/src/content/docs/en/apps/image-skills.md) |
| A | documentation | 27 | 0 | 13 | [frontend/src/content/docs/en/apps/index.md](../../../frontend/src/content/docs/en/apps/index.md) |
| A | documentation | 58 | 0 | 13 | [frontend/src/content/docs/en/apps/session-recovery-claude.md](../../../frontend/src/content/docs/en/apps/session-recovery-claude.md) |
| A | documentation | 76 | 0 | 13 | [frontend/src/content/docs/en/apps/session-recovery-codex.md](../../../frontend/src/content/docs/en/apps/session-recovery-codex.md) |
| A | documentation | 21 | 0 | 13 | [frontend/src/content/docs/en/authentication.md](../../../frontend/src/content/docs/en/authentication.md) |
| A | documentation | 18 | 0 | 13 | [frontend/src/content/docs/en/downloads.md](../../../frontend/src/content/docs/en/downloads.md) |
| A | documentation | 15 | 0 | 13 | [frontend/src/content/docs/en/errors.md](../../../frontend/src/content/docs/en/errors.md) |
| A | documentation | 9 | 0 | 13 | [frontend/src/content/docs/en/limits.md](../../../frontend/src/content/docs/en/limits.md) |
| A | documentation | 24 | 0 | 13 | [frontend/src/content/docs/en/pricing.md](../../../frontend/src/content/docs/en/pricing.md) |
| A | documentation | 20 | 0 | 13 | [frontend/src/content/docs/en/quickstart.md](../../../frontend/src/content/docs/en/quickstart.md) |
| A | documentation | 38 | 0 | 13 | [frontend/src/content/docs/examples.ts](../../../frontend/src/content/docs/examples.ts) |
| A | documentation | 102 | 0 | 13 | [frontend/src/content/docs/ja/api/chat/anthropic-messages.md](../../../frontend/src/content/docs/ja/api/chat/anthropic-messages.md) |
| A | documentation | 64 | 0 | 13 | [frontend/src/content/docs/ja/api/chat/gemini-native.md](../../../frontend/src/content/docs/ja/api/chat/gemini-native.md) |
| A | documentation | 47 | 0 | 13 | [frontend/src/content/docs/ja/api/chat/grok-native.md](../../../frontend/src/content/docs/ja/api/chat/grok-native.md) |
| A | documentation | 105 | 0 | 13 | [frontend/src/content/docs/ja/api/chat/openai-chat.md](../../../frontend/src/content/docs/ja/api/chat/openai-chat.md) |
| A | documentation | 107 | 0 | 13 | [frontend/src/content/docs/ja/api/chat/openai-responses.md](../../../frontend/src/content/docs/ja/api/chat/openai-responses.md) |
| A | documentation | 112 | 0 | 13 | [frontend/src/content/docs/ja/api/image/openai-image.md](../../../frontend/src/content/docs/ja/api/image/openai-image.md) |
| A | documentation | 27 | 0 | 13 | [frontend/src/content/docs/ja/api/protocols.md](../../../frontend/src/content/docs/ja/api/protocols.md) |
| A | documentation | 34 | 0 | 13 | [frontend/src/content/docs/ja/api/query/embeddings.md](../../../frontend/src/content/docs/ja/api/query/embeddings.md) |
| A | documentation | 44 | 0 | 13 | [frontend/src/content/docs/ja/api/query/models.md](../../../frontend/src/content/docs/ja/api/query/models.md) |
| A | documentation | 59 | 0 | 13 | [frontend/src/content/docs/ja/api/query/token-count.md](../../../frontend/src/content/docs/ja/api/query/token-count.md) |
| A | documentation | 113 | 0 | 13 | [frontend/src/content/docs/ja/apps/codex.md](../../../frontend/src/content/docs/ja/apps/codex.md) |
| A | documentation | 70 | 0 | 13 | [frontend/src/content/docs/ja/apps/console.md](../../../frontend/src/content/docs/ja/apps/console.md) |
| A | documentation | 80 | 0 | 13 | [frontend/src/content/docs/ja/apps/image-skills.md](../../../frontend/src/content/docs/ja/apps/image-skills.md) |
| A | documentation | 27 | 0 | 13 | [frontend/src/content/docs/ja/apps/index.md](../../../frontend/src/content/docs/ja/apps/index.md) |
| A | documentation | 58 | 0 | 13 | [frontend/src/content/docs/ja/apps/session-recovery-claude.md](../../../frontend/src/content/docs/ja/apps/session-recovery-claude.md) |
| A | documentation | 76 | 0 | 13 | [frontend/src/content/docs/ja/apps/session-recovery-codex.md](../../../frontend/src/content/docs/ja/apps/session-recovery-codex.md) |
| A | documentation | 21 | 0 | 13 | [frontend/src/content/docs/ja/authentication.md](../../../frontend/src/content/docs/ja/authentication.md) |
| A | documentation | 18 | 0 | 13 | [frontend/src/content/docs/ja/downloads.md](../../../frontend/src/content/docs/ja/downloads.md) |
| A | documentation | 15 | 0 | 13 | [frontend/src/content/docs/ja/errors.md](../../../frontend/src/content/docs/ja/errors.md) |
| A | documentation | 9 | 0 | 13 | [frontend/src/content/docs/ja/limits.md](../../../frontend/src/content/docs/ja/limits.md) |
| A | documentation | 24 | 0 | 13 | [frontend/src/content/docs/ja/pricing.md](../../../frontend/src/content/docs/ja/pricing.md) |
| A | documentation | 20 | 0 | 13 | [frontend/src/content/docs/ja/quickstart.md](../../../frontend/src/content/docs/ja/quickstart.md) |
| A | documentation | 92 | 0 | 13 | [frontend/src/content/docs/modelCatalog.ts](../../../frontend/src/content/docs/modelCatalog.ts) |
| A | documentation | 73 | 0 | 13 | [frontend/src/content/docs/nav.ts](../../../frontend/src/content/docs/nav.ts) |
| A | documentation | 9 | 0 | 13 | [frontend/src/content/docs/pricingSnapshot.ts](../../../frontend/src/content/docs/pricingSnapshot.ts) |
| A | documentation | 64 | 0 | 13 | [frontend/src/content/docs/types.ts](../../../frontend/src/content/docs/types.ts) |
| A | documentation | 127 | 0 | 13 | [frontend/src/content/docs/zh-TW/api/chat/anthropic-messages.md](../../../frontend/src/content/docs/zh-TW/api/chat/anthropic-messages.md) |
| A | documentation | 64 | 0 | 13 | [frontend/src/content/docs/zh-TW/api/chat/gemini-native.md](../../../frontend/src/content/docs/zh-TW/api/chat/gemini-native.md) |
| A | documentation | 47 | 0 | 13 | [frontend/src/content/docs/zh-TW/api/chat/grok-native.md](../../../frontend/src/content/docs/zh-TW/api/chat/grok-native.md) |
| A | documentation | 140 | 0 | 13 | [frontend/src/content/docs/zh-TW/api/chat/openai-chat.md](../../../frontend/src/content/docs/zh-TW/api/chat/openai-chat.md) |
| A | documentation | 150 | 0 | 13 | [frontend/src/content/docs/zh-TW/api/chat/openai-responses.md](../../../frontend/src/content/docs/zh-TW/api/chat/openai-responses.md) |
| A | documentation | 150 | 0 | 13 | [frontend/src/content/docs/zh-TW/api/image/openai-image.md](../../../frontend/src/content/docs/zh-TW/api/image/openai-image.md) |
| A | documentation | 27 | 0 | 13 | [frontend/src/content/docs/zh-TW/api/protocols.md](../../../frontend/src/content/docs/zh-TW/api/protocols.md) |
| A | documentation | 34 | 0 | 13 | [frontend/src/content/docs/zh-TW/api/query/embeddings.md](../../../frontend/src/content/docs/zh-TW/api/query/embeddings.md) |
| A | documentation | 51 | 0 | 13 | [frontend/src/content/docs/zh-TW/api/query/models.md](../../../frontend/src/content/docs/zh-TW/api/query/models.md) |
| A | documentation | 59 | 0 | 13 | [frontend/src/content/docs/zh-TW/api/query/token-count.md](../../../frontend/src/content/docs/zh-TW/api/query/token-count.md) |
| A | documentation | 119 | 0 | 13 | [frontend/src/content/docs/zh-TW/apps/codex.md](../../../frontend/src/content/docs/zh-TW/apps/codex.md) |
| A | documentation | 70 | 0 | 13 | [frontend/src/content/docs/zh-TW/apps/console.md](../../../frontend/src/content/docs/zh-TW/apps/console.md) |
| A | documentation | 87 | 0 | 13 | [frontend/src/content/docs/zh-TW/apps/image-skills.md](../../../frontend/src/content/docs/zh-TW/apps/image-skills.md) |
| A | documentation | 27 | 0 | 13 | [frontend/src/content/docs/zh-TW/apps/index.md](../../../frontend/src/content/docs/zh-TW/apps/index.md) |
| A | documentation | 58 | 0 | 13 | [frontend/src/content/docs/zh-TW/apps/session-recovery-claude.md](../../../frontend/src/content/docs/zh-TW/apps/session-recovery-claude.md) |
| A | documentation | 76 | 0 | 13 | [frontend/src/content/docs/zh-TW/apps/session-recovery-codex.md](../../../frontend/src/content/docs/zh-TW/apps/session-recovery-codex.md) |
| A | documentation | 26 | 0 | 13 | [frontend/src/content/docs/zh-TW/authentication.md](../../../frontend/src/content/docs/zh-TW/authentication.md) |
| A | documentation | 18 | 0 | 13 | [frontend/src/content/docs/zh-TW/downloads.md](../../../frontend/src/content/docs/zh-TW/downloads.md) |
| A | documentation | 21 | 0 | 13 | [frontend/src/content/docs/zh-TW/errors.md](../../../frontend/src/content/docs/zh-TW/errors.md) |
| A | documentation | 19 | 0 | 13 | [frontend/src/content/docs/zh-TW/limits.md](../../../frontend/src/content/docs/zh-TW/limits.md) |
| A | documentation | 24 | 0 | 13 | [frontend/src/content/docs/zh-TW/pricing.md](../../../frontend/src/content/docs/zh-TW/pricing.md) |
| A | documentation | 34 | 0 | 13 | [frontend/src/content/docs/zh-TW/quickstart.md](../../../frontend/src/content/docs/zh-TW/quickstart.md) |
| A | documentation | 127 | 0 | 13 | [frontend/src/content/docs/zh/api/chat/anthropic-messages.md](../../../frontend/src/content/docs/zh/api/chat/anthropic-messages.md) |
| A | documentation | 64 | 0 | 13 | [frontend/src/content/docs/zh/api/chat/gemini-native.md](../../../frontend/src/content/docs/zh/api/chat/gemini-native.md) |
| A | documentation | 47 | 0 | 13 | [frontend/src/content/docs/zh/api/chat/grok-native.md](../../../frontend/src/content/docs/zh/api/chat/grok-native.md) |
| A | documentation | 140 | 0 | 13 | [frontend/src/content/docs/zh/api/chat/openai-chat.md](../../../frontend/src/content/docs/zh/api/chat/openai-chat.md) |
| A | documentation | 150 | 0 | 13 | [frontend/src/content/docs/zh/api/chat/openai-responses.md](../../../frontend/src/content/docs/zh/api/chat/openai-responses.md) |
| A | documentation | 150 | 0 | 13 | [frontend/src/content/docs/zh/api/image/openai-image.md](../../../frontend/src/content/docs/zh/api/image/openai-image.md) |
| A | documentation | 27 | 0 | 13 | [frontend/src/content/docs/zh/api/protocols.md](../../../frontend/src/content/docs/zh/api/protocols.md) |
| A | documentation | 34 | 0 | 13 | [frontend/src/content/docs/zh/api/query/embeddings.md](../../../frontend/src/content/docs/zh/api/query/embeddings.md) |
| A | documentation | 51 | 0 | 13 | [frontend/src/content/docs/zh/api/query/models.md](../../../frontend/src/content/docs/zh/api/query/models.md) |
| A | documentation | 59 | 0 | 13 | [frontend/src/content/docs/zh/api/query/token-count.md](../../../frontend/src/content/docs/zh/api/query/token-count.md) |
| A | documentation | 119 | 0 | 13 | [frontend/src/content/docs/zh/apps/codex.md](../../../frontend/src/content/docs/zh/apps/codex.md) |
| A | documentation | 70 | 0 | 13 | [frontend/src/content/docs/zh/apps/console.md](../../../frontend/src/content/docs/zh/apps/console.md) |
| A | documentation | 87 | 0 | 13 | [frontend/src/content/docs/zh/apps/image-skills.md](../../../frontend/src/content/docs/zh/apps/image-skills.md) |
| A | documentation | 27 | 0 | 13 | [frontend/src/content/docs/zh/apps/index.md](../../../frontend/src/content/docs/zh/apps/index.md) |
| A | documentation | 58 | 0 | 13 | [frontend/src/content/docs/zh/apps/session-recovery-claude.md](../../../frontend/src/content/docs/zh/apps/session-recovery-claude.md) |
| A | documentation | 76 | 0 | 13 | [frontend/src/content/docs/zh/apps/session-recovery-codex.md](../../../frontend/src/content/docs/zh/apps/session-recovery-codex.md) |
| A | documentation | 26 | 0 | 13 | [frontend/src/content/docs/zh/authentication.md](../../../frontend/src/content/docs/zh/authentication.md) |
| A | documentation | 18 | 0 | 13 | [frontend/src/content/docs/zh/downloads.md](../../../frontend/src/content/docs/zh/downloads.md) |
| A | documentation | 21 | 0 | 13 | [frontend/src/content/docs/zh/errors.md](../../../frontend/src/content/docs/zh/errors.md) |
| A | documentation | 19 | 0 | 13 | [frontend/src/content/docs/zh/limits.md](../../../frontend/src/content/docs/zh/limits.md) |
| A | documentation | 24 | 0 | 13 | [frontend/src/content/docs/zh/pricing.md](../../../frontend/src/content/docs/zh/pricing.md) |
| A | documentation | 34 | 0 | 13 | [frontend/src/content/docs/zh/quickstart.md](../../../frontend/src/content/docs/zh/quickstart.md) |
| M | source | 1 | 1 | 13 | [frontend/src/features/channel-monitor-v2/RelayPulseMatrix.vue](../../../frontend/src/features/channel-monitor-v2/RelayPulseMatrix.vue) |
| A | source | 37 | 0 | 13 | [frontend/src/utils/legalDocumentIcon.ts](../../../frontend/src/utils/legalDocumentIcon.ts) |
| M | source | 251 | 269 | 13 | [frontend/src/views/HomeView.vue](../../../frontend/src/views/HomeView.vue) |
| M | source | 5 | 5 | 13 | [frontend/src/views/NotFoundView.vue](../../../frontend/src/views/NotFoundView.vue) |
| M | test | 50 | 3 | 13 | [frontend/src/views/__tests__/HomeView.compact.spec.ts](../../../frontend/src/views/__tests__/HomeView.compact.spec.ts) |
| M | source | 5 | 3 | 13 | [frontend/src/views/auth/EmailVerifyView.vue](../../../frontend/src/views/auth/EmailVerifyView.vue) |
| M | source | 3 | 3 | 13 | [frontend/src/views/auth/LoginView.vue](../../../frontend/src/views/auth/LoginView.vue) |
| M | source | 5 | 3 | 13 | [frontend/src/views/auth/RegisterView.vue](../../../frontend/src/views/auth/RegisterView.vue) |
| A | documentation | 202 | 0 | 13 | [frontend/src/views/docs/DocsArticleView.vue](../../../frontend/src/views/docs/DocsArticleView.vue) |
| A | documentation | 89 | 0 | 13 | [frontend/src/views/docs/DocsHomeView.vue](../../../frontend/src/views/docs/DocsHomeView.vue) |
| A | documentation | 108 | 0 | 13 | [frontend/src/views/docs/DocsModelView.vue](../../../frontend/src/views/docs/DocsModelView.vue) |
| A | documentation | 52 | 0 | 13 | [frontend/src/views/docs/DocsModelsView.vue](../../../frontend/src/views/docs/DocsModelsView.vue) |
| A | documentation | 166 | 0 | 13 | [frontend/src/views/docs/DocsView.vue](../../../frontend/src/views/docs/DocsView.vue) |
| M | source | 15 | 17 | 13 | [frontend/src/views/public/LegalDocumentView.vue](../../../frontend/src/views/public/LegalDocumentView.vue) |
| M | source | 27 | 8 | 13 | [frontend/src/views/setup/SetupWizardView.vue](../../../frontend/src/views/setup/SetupWizardView.vue) |
| M | source | 20 | 68 | 13 | [frontend/src/views/user/BatchImageGuideView.vue](../../../frontend/src/views/user/BatchImageGuideView.vue) |
| M | source | 1 | 1 | 13 | [frontend/src/views/user/ChannelStatusV2View.vue](../../../frontend/src/views/user/ChannelStatusV2View.vue) |
