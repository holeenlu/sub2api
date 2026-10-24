# 10 · 多语言与繁体生成

归档日期：2026-10-07。文件快照：main `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238`；官方比较基线 `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`。

本页登记最终需求与实际文件归属；不把本轮提交整理等同于重新实现或完整安全审计。

## 最终需求

1. 保留 en、zh、zh-TW 及现存 ja 文案。zh-TW 必须通过 tools/zh-tw/gen-locale.mjs 从 zh 生成，不手工修改生成物；后端繁体转换仍由原构建工具控制。

2. 角色、权限、API Key 排行、Setup Token、客户端配置、退款和品牌说明使用最终文案；管理员策略页不得残留必须 TOTP 的过时提示。

3. 站点名称沿用品牌常量与运行时名称解析；语言判断复用 localeUtils。多语言文件同时涉及多个需求，因此按文件统一归属，不把每一段翻译复制到各功能提交。

## 入口与配置归属

frontend/src/i18n、tools/zh-tw；不新增语言、配置键或翻译服务。

## 验收边界

gen-locale.mjs --check、类型与翻译键回归；品牌生成词汇以各品牌原最终树为准。新增需求文档为中文工程说明，不属于前端 locale 生成物。

验证状态统一见 [总说明](README.md#验证和未关闭边界)。本页列的是验收要求，不是宣称本轮已重新运行这些检查。

## 对比统计

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 业务代码 | 46 | 12255 | 338 | 0 |
| 测试/夹具 | 8 | 388 | 5 | 0 |
| 生成代码/繁体 | 19 | 10525 | 0 | 0 |
| 已有文档/许可 | 1 | 122 | 0 | 0 |
| 工具/构建/配置 | 15 | 1379 | 0 | 0 |

统计为最终文件相对固定官方基线的累计差异，并非本次整理新写的代码。对重命名统一展开为删除/新增；跨域文件只设一个主归属。

## 修改文件

| 状态 | 类型 | +行 | −行 | 文件 |
| --- | --- | ---: | ---: | --- |
| M | 业务代码 | 6 | 4 | [frontend/src/components/common/LocaleSwitcher.vue](../../../frontend/src/components/common/LocaleSwitcher.vue) |
| A | 测试/夹具 | 6 | 0 | [frontend/src/i18n/__tests__/hardcodedLocaleUsage.baseline.json](../../../frontend/src/i18n/__tests__/hardcodedLocaleUsage.baseline.json) |
| A | 测试/夹具 | 100 | 0 | [frontend/src/i18n/__tests__/hardcodedLocaleUsage.spec.ts](../../../frontend/src/i18n/__tests__/hardcodedLocaleUsage.spec.ts) |
| M | 测试/夹具 | 47 | 3 | [frontend/src/i18n/__tests__/localeKeyCompleteness.spec.ts](../../../frontend/src/i18n/__tests__/localeKeyCompleteness.spec.ts) |
| M | 测试/夹具 | 4 | 0 | [frontend/src/i18n/__tests__/localesMessageCompile.spec.ts](../../../frontend/src/i18n/__tests__/localesMessageCompile.spec.ts) |
| M | 测试/夹具 | 2 | 2 | [frontend/src/i18n/__tests__/openaiFastPolicyLocales.spec.ts](../../../frontend/src/i18n/__tests__/openaiFastPolicyLocales.spec.ts) |
| A | 测试/夹具 | 85 | 0 | [frontend/src/i18n/__tests__/siteNameLinked.spec.ts](../../../frontend/src/i18n/__tests__/siteNameLinked.spec.ts) |
| A | 测试/夹具 | 50 | 0 | [frontend/src/i18n/__tests__/taiwanLocalization.spec.ts](../../../frontend/src/i18n/__tests__/taiwanLocalization.spec.ts) |
| A | 测试/夹具 | 94 | 0 | [frontend/src/i18n/__tests__/zhTwLocale.spec.ts](../../../frontend/src/i18n/__tests__/zhTwLocale.spec.ts) |
| M | 业务代码 | 56 | 16 | [frontend/src/i18n/index.ts](../../../frontend/src/i18n/index.ts) |
| A | 业务代码 | 44 | 0 | [frontend/src/i18n/localeUtils.ts](../../../frontend/src/i18n/localeUtils.ts) |
| M | 业务代码 | 193 | 9 | [frontend/src/i18n/locales/en/admin/accounts.ts](../../../frontend/src/i18n/locales/en/admin/accounts.ts) |
| M | 业务代码 | 78 | 3 | [frontend/src/i18n/locales/en/admin/overview.ts](../../../frontend/src/i18n/locales/en/admin/overview.ts) |
| M | 业务代码 | 5 | 5 | [frontend/src/i18n/locales/en/admin/plugins.ts](../../../frontend/src/i18n/locales/en/admin/plugins.ts) |
| M | 业务代码 | 9 | 0 | [frontend/src/i18n/locales/en/admin/resources.ts](../../../frontend/src/i18n/locales/en/admin/resources.ts) |
| M | 业务代码 | 33 | 14 | [frontend/src/i18n/locales/en/admin/settings.ts](../../../frontend/src/i18n/locales/en/admin/settings.ts) |
| M | 业务代码 | 6 | 2 | [frontend/src/i18n/locales/en/common.ts](../../../frontend/src/i18n/locales/en/common.ts) |
| M | 业务代码 | 81 | 32 | [frontend/src/i18n/locales/en/dashboard.ts](../../../frontend/src/i18n/locales/en/dashboard.ts) |
| A | 业务代码 | 73 | 0 | [frontend/src/i18n/locales/en/docs.ts](../../../frontend/src/i18n/locales/en/docs.ts) |
| M | 业务代码 | 4 | 0 | [frontend/src/i18n/locales/en/index.ts](../../../frontend/src/i18n/locales/en/index.ts) |
| M | 业务代码 | 162 | 88 | [frontend/src/i18n/locales/en/landing.ts](../../../frontend/src/i18n/locales/en/landing.ts) |
| M | 业务代码 | 11 | 5 | [frontend/src/i18n/locales/en/misc.ts](../../../frontend/src/i18n/locales/en/misc.ts) |
| A | 业务代码 | 164 | 0 | [frontend/src/i18n/locales/en/ui.ts](../../../frontend/src/i18n/locales/en/ui.ts) |
| A | 业务代码 | 1933 | 0 | [frontend/src/i18n/locales/ja/admin/accounts.ts](../../../frontend/src/i18n/locales/ja/admin/accounts.ts) |
| A | 业务代码 | 52 | 0 | [frontend/src/i18n/locales/ja/admin/audit.ts](../../../frontend/src/i18n/locales/ja/admin/audit.ts) |
| A | 业务代码 | 830 | 0 | [frontend/src/i18n/locales/ja/admin/channels.ts](../../../frontend/src/i18n/locales/ja/admin/channels.ts) |
| A | 业务代码 | 21 | 0 | [frontend/src/i18n/locales/ja/admin/index.ts](../../../frontend/src/i18n/locales/ja/admin/index.ts) |
| A | 业务代码 | 839 | 0 | [frontend/src/i18n/locales/ja/admin/ops.ts](../../../frontend/src/i18n/locales/ja/admin/ops.ts) |
| A | 业务代码 | 1405 | 0 | [frontend/src/i18n/locales/ja/admin/overview.ts](../../../frontend/src/i18n/locales/ja/admin/overview.ts) |
| A | 业务代码 | 50 | 0 | [frontend/src/i18n/locales/ja/admin/plugins.ts](../../../frontend/src/i18n/locales/ja/admin/plugins.ts) |
| A | 业务代码 | 100 | 0 | [frontend/src/i18n/locales/ja/admin/promptAudit.ts](../../../frontend/src/i18n/locales/ja/admin/promptAudit.ts) |
| A | 业务代码 | 619 | 0 | [frontend/src/i18n/locales/ja/admin/resources.ts](../../../frontend/src/i18n/locales/ja/admin/resources.ts) |
| A | 业务代码 | 1465 | 0 | [frontend/src/i18n/locales/ja/admin/settings.ts](../../../frontend/src/i18n/locales/ja/admin/settings.ts) |
| A | 业务代码 | 212 | 0 | [frontend/src/i18n/locales/ja/batchImage.ts](../../../frontend/src/i18n/locales/ja/batchImage.ts) |
| A | 业务代码 | 147 | 0 | [frontend/src/i18n/locales/ja/channelMonitorV2.ts](../../../frontend/src/i18n/locales/ja/channelMonitorV2.ts) |
| A | 业务代码 | 464 | 0 | [frontend/src/i18n/locales/ja/common.ts](../../../frontend/src/i18n/locales/ja/common.ts) |
| A | 业务代码 | 1107 | 0 | [frontend/src/i18n/locales/ja/dashboard.ts](../../../frontend/src/i18n/locales/ja/dashboard.ts) |
| A | 业务代码 | 73 | 0 | [frontend/src/i18n/locales/ja/docs.ts](../../../frontend/src/i18n/locales/ja/docs.ts) |
| A | 业务代码 | 21 | 0 | [frontend/src/i18n/locales/ja/index.ts](../../../frontend/src/i18n/locales/ja/index.ts) |
| A | 业务代码 | 332 | 0 | [frontend/src/i18n/locales/ja/landing.ts](../../../frontend/src/i18n/locales/ja/landing.ts) |
| A | 业务代码 | 638 | 0 | [frontend/src/i18n/locales/ja/misc.ts](../../../frontend/src/i18n/locales/ja/misc.ts) |
| A | 业务代码 | 164 | 0 | [frontend/src/i18n/locales/ja/ui.ts](../../../frontend/src/i18n/locales/ja/ui.ts) |
| A | 生成代码/繁体 | 1904 | 0 | [frontend/src/i18n/locales/zh-TW/admin/accounts.ts](../../../frontend/src/i18n/locales/zh-TW/admin/accounts.ts) |
| A | 生成代码/繁体 | 54 | 0 | [frontend/src/i18n/locales/zh-TW/admin/audit.ts](../../../frontend/src/i18n/locales/zh-TW/admin/audit.ts) |
| A | 生成代码/繁体 | 830 | 0 | [frontend/src/i18n/locales/zh-TW/admin/channels.ts](../../../frontend/src/i18n/locales/zh-TW/admin/channels.ts) |
| A | 生成代码/繁体 | 23 | 0 | [frontend/src/i18n/locales/zh-TW/admin/index.ts](../../../frontend/src/i18n/locales/zh-TW/admin/index.ts) |
| A | 生成代码/繁体 | 842 | 0 | [frontend/src/i18n/locales/zh-TW/admin/ops.ts](../../../frontend/src/i18n/locales/zh-TW/admin/ops.ts) |
| A | 生成代码/繁体 | 1406 | 0 | [frontend/src/i18n/locales/zh-TW/admin/overview.ts](../../../frontend/src/i18n/locales/zh-TW/admin/overview.ts) |
| A | 生成代码/繁体 | 52 | 0 | [frontend/src/i18n/locales/zh-TW/admin/plugins.ts](../../../frontend/src/i18n/locales/zh-TW/admin/plugins.ts) |
| A | 生成代码/繁体 | 102 | 0 | [frontend/src/i18n/locales/zh-TW/admin/promptAudit.ts](../../../frontend/src/i18n/locales/zh-TW/admin/promptAudit.ts) |
| A | 生成代码/繁体 | 617 | 0 | [frontend/src/i18n/locales/zh-TW/admin/resources.ts](../../../frontend/src/i18n/locales/zh-TW/admin/resources.ts) |
| A | 生成代码/繁体 | 1459 | 0 | [frontend/src/i18n/locales/zh-TW/admin/settings.ts](../../../frontend/src/i18n/locales/zh-TW/admin/settings.ts) |
| A | 生成代码/繁体 | 214 | 0 | [frontend/src/i18n/locales/zh-TW/batchImage.ts](../../../frontend/src/i18n/locales/zh-TW/batchImage.ts) |
| A | 生成代码/繁体 | 144 | 0 | [frontend/src/i18n/locales/zh-TW/channelMonitorV2.ts](../../../frontend/src/i18n/locales/zh-TW/channelMonitorV2.ts) |
| A | 生成代码/繁体 | 465 | 0 | [frontend/src/i18n/locales/zh-TW/common.ts](../../../frontend/src/i18n/locales/zh-TW/common.ts) |
| A | 生成代码/繁体 | 1112 | 0 | [frontend/src/i18n/locales/zh-TW/dashboard.ts](../../../frontend/src/i18n/locales/zh-TW/dashboard.ts) |
| A | 生成代码/繁体 | 123 | 0 | [frontend/src/i18n/locales/zh-TW/docs.ts](../../../frontend/src/i18n/locales/zh-TW/docs.ts) |
| A | 生成代码/繁体 | 23 | 0 | [frontend/src/i18n/locales/zh-TW/index.ts](../../../frontend/src/i18n/locales/zh-TW/index.ts) |
| A | 生成代码/繁体 | 324 | 0 | [frontend/src/i18n/locales/zh-TW/landing.ts](../../../frontend/src/i18n/locales/zh-TW/landing.ts) |
| A | 生成代码/繁体 | 665 | 0 | [frontend/src/i18n/locales/zh-TW/misc.ts](../../../frontend/src/i18n/locales/zh-TW/misc.ts) |
| A | 生成代码/繁体 | 166 | 0 | [frontend/src/i18n/locales/zh-TW/ui.ts](../../../frontend/src/i18n/locales/zh-TW/ui.ts) |
| M | 业务代码 | 193 | 9 | [frontend/src/i18n/locales/zh/admin/accounts.ts](../../../frontend/src/i18n/locales/zh/admin/accounts.ts) |
| M | 业务代码 | 78 | 3 | [frontend/src/i18n/locales/zh/admin/overview.ts](../../../frontend/src/i18n/locales/zh/admin/overview.ts) |
| M | 业务代码 | 5 | 5 | [frontend/src/i18n/locales/zh/admin/plugins.ts](../../../frontend/src/i18n/locales/zh/admin/plugins.ts) |
| M | 业务代码 | 9 | 0 | [frontend/src/i18n/locales/zh/admin/resources.ts](../../../frontend/src/i18n/locales/zh/admin/resources.ts) |
| M | 业务代码 | 33 | 14 | [frontend/src/i18n/locales/zh/admin/settings.ts](../../../frontend/src/i18n/locales/zh/admin/settings.ts) |
| M | 业务代码 | 6 | 2 | [frontend/src/i18n/locales/zh/common.ts](../../../frontend/src/i18n/locales/zh/common.ts) |
| M | 业务代码 | 82 | 34 | [frontend/src/i18n/locales/zh/dashboard.ts](../../../frontend/src/i18n/locales/zh/dashboard.ts) |
| A | 业务代码 | 121 | 0 | [frontend/src/i18n/locales/zh/docs.ts](../../../frontend/src/i18n/locales/zh/docs.ts) |
| M | 业务代码 | 4 | 0 | [frontend/src/i18n/locales/zh/index.ts](../../../frontend/src/i18n/locales/zh/index.ts) |
| M | 业务代码 | 152 | 88 | [frontend/src/i18n/locales/zh/landing.ts](../../../frontend/src/i18n/locales/zh/landing.ts) |
| M | 业务代码 | 11 | 5 | [frontend/src/i18n/locales/zh/misc.ts](../../../frontend/src/i18n/locales/zh/misc.ts) |
| A | 业务代码 | 164 | 0 | [frontend/src/i18n/locales/zh/ui.ts](../../../frontend/src/i18n/locales/zh/ui.ts) |
| A | 已有文档/许可 | 122 | 0 | [tools/zh-tw/README.md](../../../tools/zh-tw/README.md) |
| A | 工具/构建/配置 | 71 | 0 | [tools/zh-tw/audit-locale.mjs](../../../tools/zh-tw/audit-locale.mjs) |
| A | 工具/构建/配置 | 73 | 0 | [tools/zh-tw/audit-ui.mjs](../../../tools/zh-tw/audit-ui.mjs) |
| A | 工具/构建/配置 | 60 | 0 | [tools/zh-tw/audit.mjs](../../../tools/zh-tw/audit.mjs) |
| A | 工具/构建/配置 | 131 | 0 | [tools/zh-tw/audit2.mjs](../../../tools/zh-tw/audit2.mjs) |
| A | 工具/构建/配置 | 45 | 0 | [tools/zh-tw/audit3.mjs](../../../tools/zh-tw/audit3.mjs) |
| A | 工具/构建/配置 | 64 | 0 | [tools/zh-tw/classify.mjs](../../../tools/zh-tw/classify.mjs) |
| A | 工具/构建/配置 | 178 | 0 | [tools/zh-tw/convert-go.mjs](../../../tools/zh-tw/convert-go.mjs) |
| A | 工具/构建/配置 | 383 | 0 | [tools/zh-tw/convert.mjs](../../../tools/zh-tw/convert.mjs) |
| A | 工具/构建/配置 | 103 | 0 | [tools/zh-tw/gen-locale.mjs](../../../tools/zh-tw/gen-locale.mjs) |
| A | 工具/构建/配置 | 19 | 0 | [tools/zh-tw/package-lock.json](../../../tools/zh-tw/package-lock.json) |
| A | 工具/构建/配置 | 9 | 0 | [tools/zh-tw/package.json](../../../tools/zh-tw/package.json) |
| A | 工具/构建/配置 | 26 | 0 | [tools/zh-tw/remaining.mjs](../../../tools/zh-tw/remaining.mjs) |
| A | 工具/构建/配置 | 78 | 0 | [tools/zh-tw/scan-go.mjs](../../../tools/zh-tw/scan-go.mjs) |
| A | 工具/构建/配置 | 52 | 0 | [tools/zh-tw/trap-scan.mjs](../../../tools/zh-tw/trap-scan.mjs) |
| A | 工具/构建/配置 | 87 | 0 | [tools/zh-tw/verify-matchers.mjs](../../../tools/zh-tw/verify-matchers.mjs) |
