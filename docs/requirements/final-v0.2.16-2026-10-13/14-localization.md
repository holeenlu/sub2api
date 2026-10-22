# 14 · 多语言与繁体生成

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。

## 最终需求

1. 保留 en、zh、zh-TW 及现存 ja 文案。zh-TW 必须通过 tools/zh-tw/gen-locale.mjs 从 zh 生成，不手工修改生成物；后端繁体转换仍由原构建工具控制。

2. 角色、权限、API Key 排行、Setup Token、客户端配置、退款和品牌说明使用最终文案；管理员策略页不得残留必须 TOTP 的过时提示。

3. 站点名称沿用品牌常量与运行时名称解析；语言判断复用 localeUtils。多语言文件同时涉及多个需求，因此按文件统一归属，不把每一段翻译复制到各功能提交。

## 入口与配置归属

frontend/src/i18n、tools/zh-tw；不新增语言、配置键或翻译服务。

## 最终实现方案

1. zh 是繁体生成来源，zh-TW 使用 tools/zh-tw 生成，保留现有 en/ja；不在重组中手改翻译生成物。

2. 共享语言包包含 RBAC、模型、BPS、退款等多项需求，以文件主归属一次提交并记录关联。

3. 品牌词汇和默认名称随品牌覆盖，功能键和授权含义保持一致。

## 边界、失败路径与依赖

关联功能组：05, 06, 07。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。上面描述现有最终行为，本次不重新实现业务。

## 验收要求

gen-locale.mjs --check、localeKeyCompleteness、前端 typecheck。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | --- | --- | --- | --- |
| documentation | 1 | 122 | 0 | 0 |
| generated | 20 | 10704 | 0 | 0 |
| source | 51 | 12646 | 340 | 0 |
| test | 8 | 388 | 5 | 0 |
| tooling | 15 | 1379 | 0 | 0 |

这是相对 v0.2.16 的累计定制差异，不是本次新写代码。删除/新增重命名分别计数；生成代码与测试单列。

## 来源提交

- `3bd5cd406dd37b332f01a446718caca3a8a4222f`
- `49b29ef158e6319cfd6b1227781307de5f8f870e`
- `4b2cfde513867c40dbcc663a5ddea30cdd286dd2`
- `4c5aa7b49988f5752b01940475530832c188b7ad`
- `880b96f67612767ed5955efa8b91b56fa2681daf`
- `944fdb1eff8e7ea33ed70ff722b6822f962e9255`
- `a2988a14ca54b99b09a6f64024166d52fa8db132`
- `c6a92e1501a3ed5bb405c02bd541fcb59366bd28`
- `e37f32177a2b3f31414e8e2801d1f20678983fe5`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | --- | --- | --- | --- |
| M | source | 6 | 4 | 14 | [frontend/src/components/common/LocaleSwitcher.vue](../../../frontend/src/components/common/LocaleSwitcher.vue) |
| A | test | 6 | 0 | 14 | [frontend/src/i18n/__tests__/hardcodedLocaleUsage.baseline.json](../../../frontend/src/i18n/__tests__/hardcodedLocaleUsage.baseline.json) |
| A | test | 100 | 0 | 14 | [frontend/src/i18n/__tests__/hardcodedLocaleUsage.spec.ts](../../../frontend/src/i18n/__tests__/hardcodedLocaleUsage.spec.ts) |
| M | test | 47 | 3 | 14 | [frontend/src/i18n/__tests__/localeKeyCompleteness.spec.ts](../../../frontend/src/i18n/__tests__/localeKeyCompleteness.spec.ts) |
| M | test | 4 | 0 | 14 | [frontend/src/i18n/__tests__/localesMessageCompile.spec.ts](../../../frontend/src/i18n/__tests__/localesMessageCompile.spec.ts) |
| M | test | 2 | 2 | 14 | [frontend/src/i18n/__tests__/openaiFastPolicyLocales.spec.ts](../../../frontend/src/i18n/__tests__/openaiFastPolicyLocales.spec.ts) |
| A | test | 85 | 0 | 14 | [frontend/src/i18n/__tests__/siteNameLinked.spec.ts](../../../frontend/src/i18n/__tests__/siteNameLinked.spec.ts) |
| A | test | 50 | 0 | 14 | [frontend/src/i18n/__tests__/taiwanLocalization.spec.ts](../../../frontend/src/i18n/__tests__/taiwanLocalization.spec.ts) |
| A | test | 94 | 0 | 14 | [frontend/src/i18n/__tests__/zhTwLocale.spec.ts](../../../frontend/src/i18n/__tests__/zhTwLocale.spec.ts) |
| M | source | 56 | 16 | 14 | [frontend/src/i18n/index.ts](../../../frontend/src/i18n/index.ts) |
| A | source | 44 | 0 | 14 | [frontend/src/i18n/localeUtils.ts](../../../frontend/src/i18n/localeUtils.ts) |
| M | source | 229 | 9 | 05,07,14 | [frontend/src/i18n/locales/en/admin/accounts.ts](../../../frontend/src/i18n/locales/en/admin/accounts.ts) |
| M | source | 2 | 0 | 06,14 | [frontend/src/i18n/locales/en/admin/ops.ts](../../../frontend/src/i18n/locales/en/admin/ops.ts) |
| M | source | 79 | 4 | 05,14 | [frontend/src/i18n/locales/en/admin/overview.ts](../../../frontend/src/i18n/locales/en/admin/overview.ts) |
| M | source | 5 | 5 | 14 | [frontend/src/i18n/locales/en/admin/plugins.ts](../../../frontend/src/i18n/locales/en/admin/plugins.ts) |
| M | source | 9 | 0 | 14 | [frontend/src/i18n/locales/en/admin/resources.ts](../../../frontend/src/i18n/locales/en/admin/resources.ts) |
| M | source | 73 | 14 | 07,14 | [frontend/src/i18n/locales/en/admin/settings.ts](../../../frontend/src/i18n/locales/en/admin/settings.ts) |
| A | source | 55 | 0 | 07,14 | [frontend/src/i18n/locales/en/autoBPSOps.ts](../../../frontend/src/i18n/locales/en/autoBPSOps.ts) |
| M | source | 6 | 2 | 14 | [frontend/src/i18n/locales/en/common.ts](../../../frontend/src/i18n/locales/en/common.ts) |
| M | source | 81 | 32 | 14 | [frontend/src/i18n/locales/en/dashboard.ts](../../../frontend/src/i18n/locales/en/dashboard.ts) |
| A | source | 73 | 0 | 14 | [frontend/src/i18n/locales/en/docs.ts](../../../frontend/src/i18n/locales/en/docs.ts) |
| M | source | 6 | 0 | 07,14 | [frontend/src/i18n/locales/en/index.ts](../../../frontend/src/i18n/locales/en/index.ts) |
| M | source | 162 | 88 | 14 | [frontend/src/i18n/locales/en/landing.ts](../../../frontend/src/i18n/locales/en/landing.ts) |
| M | source | 11 | 5 | 14 | [frontend/src/i18n/locales/en/misc.ts](../../../frontend/src/i18n/locales/en/misc.ts) |
| A | source | 164 | 0 | 14 | [frontend/src/i18n/locales/en/ui.ts](../../../frontend/src/i18n/locales/en/ui.ts) |
| A | source | 1992 | 0 | 05,07,14 | [frontend/src/i18n/locales/ja/admin/accounts.ts](../../../frontend/src/i18n/locales/ja/admin/accounts.ts) |
| A | source | 52 | 0 | 14 | [frontend/src/i18n/locales/ja/admin/audit.ts](../../../frontend/src/i18n/locales/ja/admin/audit.ts) |
| A | source | 830 | 0 | 14 | [frontend/src/i18n/locales/ja/admin/channels.ts](../../../frontend/src/i18n/locales/ja/admin/channels.ts) |
| A | source | 21 | 0 | 14 | [frontend/src/i18n/locales/ja/admin/index.ts](../../../frontend/src/i18n/locales/ja/admin/index.ts) |
| A | source | 845 | 0 | 06,14 | [frontend/src/i18n/locales/ja/admin/ops.ts](../../../frontend/src/i18n/locales/ja/admin/ops.ts) |
| A | source | 1411 | 0 | 05,14 | [frontend/src/i18n/locales/ja/admin/overview.ts](../../../frontend/src/i18n/locales/ja/admin/overview.ts) |
| A | source | 50 | 0 | 14 | [frontend/src/i18n/locales/ja/admin/plugins.ts](../../../frontend/src/i18n/locales/ja/admin/plugins.ts) |
| A | source | 100 | 0 | 14 | [frontend/src/i18n/locales/ja/admin/promptAudit.ts](../../../frontend/src/i18n/locales/ja/admin/promptAudit.ts) |
| A | source | 621 | 0 | 14 | [frontend/src/i18n/locales/ja/admin/resources.ts](../../../frontend/src/i18n/locales/ja/admin/resources.ts) |
| A | source | 1504 | 0 | 07,14 | [frontend/src/i18n/locales/ja/admin/settings.ts](../../../frontend/src/i18n/locales/ja/admin/settings.ts) |
| A | source | 1 | 0 | 07,14 | [frontend/src/i18n/locales/ja/autoBPSOps.ts](../../../frontend/src/i18n/locales/ja/autoBPSOps.ts) |
| A | source | 212 | 0 | 14 | [frontend/src/i18n/locales/ja/batchImage.ts](../../../frontend/src/i18n/locales/ja/batchImage.ts) |
| A | source | 149 | 0 | 14 | [frontend/src/i18n/locales/ja/channelMonitorV2.ts](../../../frontend/src/i18n/locales/ja/channelMonitorV2.ts) |
| A | source | 464 | 0 | 14 | [frontend/src/i18n/locales/ja/common.ts](../../../frontend/src/i18n/locales/ja/common.ts) |
| A | source | 1109 | 0 | 14 | [frontend/src/i18n/locales/ja/dashboard.ts](../../../frontend/src/i18n/locales/ja/dashboard.ts) |
| A | source | 73 | 0 | 14 | [frontend/src/i18n/locales/ja/docs.ts](../../../frontend/src/i18n/locales/ja/docs.ts) |
| A | source | 23 | 0 | 07,14 | [frontend/src/i18n/locales/ja/index.ts](../../../frontend/src/i18n/locales/ja/index.ts) |
| A | source | 332 | 0 | 14 | [frontend/src/i18n/locales/ja/landing.ts](../../../frontend/src/i18n/locales/ja/landing.ts) |
| A | source | 638 | 0 | 14 | [frontend/src/i18n/locales/ja/misc.ts](../../../frontend/src/i18n/locales/ja/misc.ts) |
| A | source | 164 | 0 | 14 | [frontend/src/i18n/locales/ja/ui.ts](../../../frontend/src/i18n/locales/ja/ui.ts) |
| A | generated | 1966 | 0 | 05,07,14 | [frontend/src/i18n/locales/zh-TW/admin/accounts.ts](../../../frontend/src/i18n/locales/zh-TW/admin/accounts.ts) |
| A | generated | 54 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/admin/audit.ts](../../../frontend/src/i18n/locales/zh-TW/admin/audit.ts) |
| A | generated | 830 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/admin/channels.ts](../../../frontend/src/i18n/locales/zh-TW/admin/channels.ts) |
| A | generated | 23 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/admin/index.ts](../../../frontend/src/i18n/locales/zh-TW/admin/index.ts) |
| A | generated | 848 | 0 | 06,14 | [frontend/src/i18n/locales/zh-TW/admin/ops.ts](../../../frontend/src/i18n/locales/zh-TW/admin/ops.ts) |
| A | generated | 1412 | 0 | 05,14 | [frontend/src/i18n/locales/zh-TW/admin/overview.ts](../../../frontend/src/i18n/locales/zh-TW/admin/overview.ts) |
| A | generated | 52 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/admin/plugins.ts](../../../frontend/src/i18n/locales/zh-TW/admin/plugins.ts) |
| A | generated | 102 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/admin/promptAudit.ts](../../../frontend/src/i18n/locales/zh-TW/admin/promptAudit.ts) |
| A | generated | 619 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/admin/resources.ts](../../../frontend/src/i18n/locales/zh-TW/admin/resources.ts) |
| A | generated | 1499 | 0 | 07,14 | [frontend/src/i18n/locales/zh-TW/admin/settings.ts](../../../frontend/src/i18n/locales/zh-TW/admin/settings.ts) |
| A | generated | 57 | 0 | 07,14 | [frontend/src/i18n/locales/zh-TW/autoBPSOps.ts](../../../frontend/src/i18n/locales/zh-TW/autoBPSOps.ts) |
| A | generated | 214 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/batchImage.ts](../../../frontend/src/i18n/locales/zh-TW/batchImage.ts) |
| A | generated | 146 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/channelMonitorV2.ts](../../../frontend/src/i18n/locales/zh-TW/channelMonitorV2.ts) |
| A | generated | 465 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/common.ts](../../../frontend/src/i18n/locales/zh-TW/common.ts) |
| A | generated | 1114 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/dashboard.ts](../../../frontend/src/i18n/locales/zh-TW/dashboard.ts) |
| A | generated | 123 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/docs.ts](../../../frontend/src/i18n/locales/zh-TW/docs.ts) |
| A | generated | 25 | 0 | 07,14 | [frontend/src/i18n/locales/zh-TW/index.ts](../../../frontend/src/i18n/locales/zh-TW/index.ts) |
| A | generated | 324 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/landing.ts](../../../frontend/src/i18n/locales/zh-TW/landing.ts) |
| A | generated | 665 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/misc.ts](../../../frontend/src/i18n/locales/zh-TW/misc.ts) |
| A | generated | 166 | 0 | 14 | [frontend/src/i18n/locales/zh-TW/ui.ts](../../../frontend/src/i18n/locales/zh-TW/ui.ts) |
| M | source | 229 | 9 | 05,07,14 | [frontend/src/i18n/locales/zh/admin/accounts.ts](../../../frontend/src/i18n/locales/zh/admin/accounts.ts) |
| M | source | 2 | 0 | 06,14 | [frontend/src/i18n/locales/zh/admin/ops.ts](../../../frontend/src/i18n/locales/zh/admin/ops.ts) |
| M | source | 79 | 4 | 05,14 | [frontend/src/i18n/locales/zh/admin/overview.ts](../../../frontend/src/i18n/locales/zh/admin/overview.ts) |
| M | source | 5 | 5 | 14 | [frontend/src/i18n/locales/zh/admin/plugins.ts](../../../frontend/src/i18n/locales/zh/admin/plugins.ts) |
| M | source | 9 | 0 | 14 | [frontend/src/i18n/locales/zh/admin/resources.ts](../../../frontend/src/i18n/locales/zh/admin/resources.ts) |
| M | source | 73 | 14 | 07,14 | [frontend/src/i18n/locales/zh/admin/settings.ts](../../../frontend/src/i18n/locales/zh/admin/settings.ts) |
| A | source | 55 | 0 | 07,14 | [frontend/src/i18n/locales/zh/autoBPSOps.ts](../../../frontend/src/i18n/locales/zh/autoBPSOps.ts) |
| M | source | 6 | 2 | 14 | [frontend/src/i18n/locales/zh/common.ts](../../../frontend/src/i18n/locales/zh/common.ts) |
| M | source | 82 | 34 | 14 | [frontend/src/i18n/locales/zh/dashboard.ts](../../../frontend/src/i18n/locales/zh/dashboard.ts) |
| A | source | 121 | 0 | 14 | [frontend/src/i18n/locales/zh/docs.ts](../../../frontend/src/i18n/locales/zh/docs.ts) |
| M | source | 6 | 0 | 07,14 | [frontend/src/i18n/locales/zh/index.ts](../../../frontend/src/i18n/locales/zh/index.ts) |
| M | source | 152 | 88 | 14 | [frontend/src/i18n/locales/zh/landing.ts](../../../frontend/src/i18n/locales/zh/landing.ts) |
| M | source | 11 | 5 | 14 | [frontend/src/i18n/locales/zh/misc.ts](../../../frontend/src/i18n/locales/zh/misc.ts) |
| A | source | 164 | 0 | 14 | [frontend/src/i18n/locales/zh/ui.ts](../../../frontend/src/i18n/locales/zh/ui.ts) |
| A | documentation | 122 | 0 | 14 | [tools/zh-tw/README.md](../../../tools/zh-tw/README.md) |
| A | tooling | 71 | 0 | 14 | [tools/zh-tw/audit-locale.mjs](../../../tools/zh-tw/audit-locale.mjs) |
| A | tooling | 73 | 0 | 14 | [tools/zh-tw/audit-ui.mjs](../../../tools/zh-tw/audit-ui.mjs) |
| A | tooling | 60 | 0 | 14 | [tools/zh-tw/audit.mjs](../../../tools/zh-tw/audit.mjs) |
| A | tooling | 131 | 0 | 14 | [tools/zh-tw/audit2.mjs](../../../tools/zh-tw/audit2.mjs) |
| A | tooling | 45 | 0 | 14 | [tools/zh-tw/audit3.mjs](../../../tools/zh-tw/audit3.mjs) |
| A | tooling | 64 | 0 | 14 | [tools/zh-tw/classify.mjs](../../../tools/zh-tw/classify.mjs) |
| A | tooling | 178 | 0 | 14 | [tools/zh-tw/convert-go.mjs](../../../tools/zh-tw/convert-go.mjs) |
| A | tooling | 383 | 0 | 14 | [tools/zh-tw/convert.mjs](../../../tools/zh-tw/convert.mjs) |
| A | tooling | 103 | 0 | 14 | [tools/zh-tw/gen-locale.mjs](../../../tools/zh-tw/gen-locale.mjs) |
| A | tooling | 19 | 0 | 14 | [tools/zh-tw/package-lock.json](../../../tools/zh-tw/package-lock.json) |
| A | tooling | 9 | 0 | 14 | [tools/zh-tw/package.json](../../../tools/zh-tw/package.json) |
| A | tooling | 26 | 0 | 14 | [tools/zh-tw/remaining.mjs](../../../tools/zh-tw/remaining.mjs) |
| A | tooling | 78 | 0 | 14 | [tools/zh-tw/scan-go.mjs](../../../tools/zh-tw/scan-go.mjs) |
| A | tooling | 52 | 0 | 14 | [tools/zh-tw/trap-scan.mjs](../../../tools/zh-tw/trap-scan.mjs) |
| A | tooling | 87 | 0 | 14 | [tools/zh-tw/verify-matchers.mjs](../../../tools/zh-tw/verify-matchers.mjs) |
