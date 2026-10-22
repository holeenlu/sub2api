# 17 · 多语言与繁体生成

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`feat(i18n): preserve translations and generated traditional Chinese`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. 保留 en、zh、zh-TW 及现存 ja 文案。zh-TW 必须通过 tools/zh-tw/gen-locale.mjs 从 zh 生成，不手工修改生成物；后端繁体转换仍由原构建工具控制。

2. 角色、权限、API Key 排行、Setup Token、客户端配置、退款和品牌说明使用最终文案；管理员策略页不得残留必须 TOTP 的过时提示。

3. 站点名称沿用品牌常量与运行时名称解析；语言判断复用 localeUtils。多语言文件同时涉及多个需求，因此按文件统一归属，不把每一段翻译复制到各功能提交。

供应商门户四语切片与生成翻译包由供应商功能组统一收录；其余共用语言文件在本组一次提交。继续使用原繁体生成器，保留原语义、模型名称与品牌差异。没有新建第二套翻译规则。

## 入口与配置归属

frontend/src/i18n、tools/zh-tw；不新增语言、配置键或翻译服务。

## 最终实现方案

1. zh 是繁体生成来源，zh-TW 使用 tools/zh-tw 生成，保留现有 en/ja；不在重组中手改翻译生成物。

2. 共享语言包包含 RBAC、模型、BPS、退款等多项需求，以文件主归属一次提交并记录关联。

3. 品牌词汇和默认名称随品牌覆盖，功能键和授权含义保持一致。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

gen-locale.mjs --check、localeKeyCompleteness、前端 typecheck。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 1 | 122 | 0 | 0 |
| generated | 19 | 9865 | 0 | 0 |
| source | 50 | 11830 | 342 | 0 |
| test | 8 | 388 | 5 | 0 |
| tooling | 15 | 1379 | 0 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 34 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 6 | 4 | 17 | [frontend/src/components/common/LocaleSwitcher.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/LocaleSwitcher.vue) |
| A | test | 6 | 0 | 17 | [frontend/src/i18n/__tests__/hardcodedLocaleUsage.baseline.json](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/__tests__/hardcodedLocaleUsage.baseline.json) |
| A | test | 100 | 0 | 17 | [frontend/src/i18n/__tests__/hardcodedLocaleUsage.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/__tests__/hardcodedLocaleUsage.spec.ts) |
| M | test | 47 | 3 | 17 | [frontend/src/i18n/__tests__/localeKeyCompleteness.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/__tests__/localeKeyCompleteness.spec.ts) |
| M | test | 4 | 0 | 17 | [frontend/src/i18n/__tests__/localesMessageCompile.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/__tests__/localesMessageCompile.spec.ts) |
| M | test | 2 | 2 | 17 | [frontend/src/i18n/__tests__/openaiFastPolicyLocales.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/__tests__/openaiFastPolicyLocales.spec.ts) |
| A | test | 85 | 0 | 17 | [frontend/src/i18n/__tests__/siteNameLinked.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/__tests__/siteNameLinked.spec.ts) |
| A | test | 50 | 0 | 17 | [frontend/src/i18n/__tests__/taiwanLocalization.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/__tests__/taiwanLocalization.spec.ts) |
| A | test | 94 | 0 | 17 | [frontend/src/i18n/__tests__/zhTwLocale.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/__tests__/zhTwLocale.spec.ts) |
| M | source | 56 | 16 | 17 | [frontend/src/i18n/index.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/index.ts) |
| A | source | 44 | 0 | 17 | [frontend/src/i18n/localeUtils.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/localeUtils.ts) |
| M | source | 230 | 9 | 05,07,17 | [frontend/src/i18n/locales/en/admin/accounts.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/admin/accounts.ts) |
| M | source | 1 | 1 | 17 | [frontend/src/i18n/locales/en/admin/channels.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/admin/channels.ts) |
| M | source | 79 | 4 | 05,17 | [frontend/src/i18n/locales/en/admin/overview.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/admin/overview.ts) |
| M | source | 5 | 5 | 17 | [frontend/src/i18n/locales/en/admin/plugins.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/admin/plugins.ts) |
| M | source | 9 | 0 | 17 | [frontend/src/i18n/locales/en/admin/resources.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/admin/resources.ts) |
| M | source | 81 | 14 | 07,17 | [frontend/src/i18n/locales/en/admin/settings.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/admin/settings.ts) |
| A | source | 55 | 0 | 07,17 | [frontend/src/i18n/locales/en/autoBPSOps.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/autoBPSOps.ts) |
| M | source | 6 | 2 | 17 | [frontend/src/i18n/locales/en/common.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/common.ts) |
| M | source | 81 | 32 | 17 | [frontend/src/i18n/locales/en/dashboard.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/dashboard.ts) |
| A | source | 73 | 0 | 17 | [frontend/src/i18n/locales/en/docs.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/docs.ts) |
| M | source | 8 | 0 | 07,12,17 | [frontend/src/i18n/locales/en/index.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/index.ts) |
| M | source | 162 | 88 | 17 | [frontend/src/i18n/locales/en/landing.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/landing.ts) |
| M | source | 11 | 5 | 17 | [frontend/src/i18n/locales/en/misc.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/misc.ts) |
| A | source | 164 | 0 | 17 | [frontend/src/i18n/locales/en/ui.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/ui.ts) |
| A | source | 1991 | 0 | 05,07,17 | [frontend/src/i18n/locales/ja/admin/accounts.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/admin/accounts.ts) |
| A | source | 52 | 0 | 17 | [frontend/src/i18n/locales/ja/admin/audit.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/admin/audit.ts) |
| A | source | 830 | 0 | 17 | [frontend/src/i18n/locales/ja/admin/channels.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/admin/channels.ts) |
| A | source | 21 | 0 | 17 | [frontend/src/i18n/locales/ja/admin/index.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/admin/index.ts) |
| A | source | 1411 | 0 | 05,17 | [frontend/src/i18n/locales/ja/admin/overview.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/admin/overview.ts) |
| A | source | 50 | 0 | 17 | [frontend/src/i18n/locales/ja/admin/plugins.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/admin/plugins.ts) |
| A | source | 100 | 0 | 17 | [frontend/src/i18n/locales/ja/admin/promptAudit.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/admin/promptAudit.ts) |
| A | source | 621 | 0 | 17 | [frontend/src/i18n/locales/ja/admin/resources.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/admin/resources.ts) |
| A | source | 1512 | 0 | 07,17 | [frontend/src/i18n/locales/ja/admin/settings.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/admin/settings.ts) |
| A | source | 1 | 0 | 07,17 | [frontend/src/i18n/locales/ja/autoBPSOps.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/autoBPSOps.ts) |
| A | source | 212 | 0 | 17 | [frontend/src/i18n/locales/ja/batchImage.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/batchImage.ts) |
| A | source | 149 | 0 | 17 | [frontend/src/i18n/locales/ja/channelMonitorV2.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/channelMonitorV2.ts) |
| A | source | 464 | 0 | 17 | [frontend/src/i18n/locales/ja/common.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/common.ts) |
| A | source | 1109 | 0 | 17 | [frontend/src/i18n/locales/ja/dashboard.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/dashboard.ts) |
| A | source | 73 | 0 | 17 | [frontend/src/i18n/locales/ja/docs.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/docs.ts) |
| A | source | 25 | 0 | 07,12,17 | [frontend/src/i18n/locales/ja/index.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/index.ts) |
| A | source | 332 | 0 | 17 | [frontend/src/i18n/locales/ja/landing.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/landing.ts) |
| A | source | 638 | 0 | 17 | [frontend/src/i18n/locales/ja/misc.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/misc.ts) |
| A | source | 164 | 0 | 17 | [frontend/src/i18n/locales/ja/ui.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/ui.ts) |
| A | generated | 1965 | 0 | 05,07,17 | [frontend/src/i18n/locales/zh-TW/admin/accounts.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/admin/accounts.ts) |
| A | generated | 54 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/admin/audit.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/admin/audit.ts) |
| A | generated | 830 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/admin/channels.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/admin/channels.ts) |
| A | generated | 23 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/admin/index.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/admin/index.ts) |
| A | generated | 1412 | 0 | 05,17 | [frontend/src/i18n/locales/zh-TW/admin/overview.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/admin/overview.ts) |
| A | generated | 52 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/admin/plugins.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/admin/plugins.ts) |
| A | generated | 102 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/admin/promptAudit.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/admin/promptAudit.ts) |
| A | generated | 619 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/admin/resources.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/admin/resources.ts) |
| A | generated | 1507 | 0 | 07,17 | [frontend/src/i18n/locales/zh-TW/admin/settings.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/admin/settings.ts) |
| A | generated | 57 | 0 | 07,17 | [frontend/src/i18n/locales/zh-TW/autoBPSOps.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/autoBPSOps.ts) |
| A | generated | 214 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/batchImage.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/batchImage.ts) |
| A | generated | 146 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/channelMonitorV2.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/channelMonitorV2.ts) |
| A | generated | 465 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/common.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/common.ts) |
| A | generated | 1114 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/dashboard.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/dashboard.ts) |
| A | generated | 123 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/docs.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/docs.ts) |
| A | generated | 27 | 0 | 07,12,17 | [frontend/src/i18n/locales/zh-TW/index.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/index.ts) |
| A | generated | 324 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/landing.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/landing.ts) |
| A | generated | 665 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/misc.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/misc.ts) |
| A | generated | 166 | 0 | 17 | [frontend/src/i18n/locales/zh-TW/ui.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/ui.ts) |
| M | source | 230 | 9 | 05,07,17 | [frontend/src/i18n/locales/zh/admin/accounts.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/admin/accounts.ts) |
| M | source | 1 | 1 | 17 | [frontend/src/i18n/locales/zh/admin/channels.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/admin/channels.ts) |
| M | source | 79 | 4 | 05,17 | [frontend/src/i18n/locales/zh/admin/overview.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/admin/overview.ts) |
| M | source | 5 | 5 | 17 | [frontend/src/i18n/locales/zh/admin/plugins.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/admin/plugins.ts) |
| M | source | 9 | 0 | 17 | [frontend/src/i18n/locales/zh/admin/resources.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/admin/resources.ts) |
| M | source | 81 | 14 | 07,17 | [frontend/src/i18n/locales/zh/admin/settings.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/admin/settings.ts) |
| A | source | 55 | 0 | 07,17 | [frontend/src/i18n/locales/zh/autoBPSOps.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/autoBPSOps.ts) |
| M | source | 6 | 2 | 17 | [frontend/src/i18n/locales/zh/common.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/common.ts) |
| M | source | 82 | 34 | 17 | [frontend/src/i18n/locales/zh/dashboard.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/dashboard.ts) |
| A | source | 121 | 0 | 17 | [frontend/src/i18n/locales/zh/docs.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/docs.ts) |
| M | source | 8 | 0 | 07,12,17 | [frontend/src/i18n/locales/zh/index.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/index.ts) |
| M | source | 152 | 88 | 17 | [frontend/src/i18n/locales/zh/landing.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/landing.ts) |
| M | source | 11 | 5 | 17 | [frontend/src/i18n/locales/zh/misc.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/misc.ts) |
| A | source | 164 | 0 | 17 | [frontend/src/i18n/locales/zh/ui.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/ui.ts) |
| A | documentation | 122 | 0 | 17 | [tools/zh-tw/README.md](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/README.md) |
| A | tooling | 71 | 0 | 17 | [tools/zh-tw/audit-locale.mjs](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/audit-locale.mjs) |
| A | tooling | 73 | 0 | 17 | [tools/zh-tw/audit-ui.mjs](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/audit-ui.mjs) |
| A | tooling | 60 | 0 | 17 | [tools/zh-tw/audit.mjs](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/audit.mjs) |
| A | tooling | 131 | 0 | 17 | [tools/zh-tw/audit2.mjs](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/audit2.mjs) |
| A | tooling | 45 | 0 | 17 | [tools/zh-tw/audit3.mjs](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/audit3.mjs) |
| A | tooling | 64 | 0 | 17 | [tools/zh-tw/classify.mjs](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/classify.mjs) |
| A | tooling | 178 | 0 | 17 | [tools/zh-tw/convert-go.mjs](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/convert-go.mjs) |
| A | tooling | 383 | 0 | 17 | [tools/zh-tw/convert.mjs](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/convert.mjs) |
| A | tooling | 103 | 0 | 17 | [tools/zh-tw/gen-locale.mjs](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/gen-locale.mjs) |
| A | tooling | 19 | 0 | 17 | [tools/zh-tw/package-lock.json](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/package-lock.json) |
| A | tooling | 9 | 0 | 17 | [tools/zh-tw/package.json](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/package.json) |
| A | tooling | 26 | 0 | 17 | [tools/zh-tw/remaining.mjs](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/remaining.mjs) |
| A | tooling | 78 | 0 | 17 | [tools/zh-tw/scan-go.mjs](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/scan-go.mjs) |
| A | tooling | 52 | 0 | 17 | [tools/zh-tw/trap-scan.mjs](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/trap-scan.mjs) |
| A | tooling | 87 | 0 | 17 | [tools/zh-tw/verify-matchers.mjs](https://github.com/holeenlu/sub2api/blob/main/tools/zh-tw/verify-matchers.mjs) |
