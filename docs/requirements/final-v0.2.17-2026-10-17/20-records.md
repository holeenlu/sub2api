# 20 · 需求、方案、来源与逐文件核验归档

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`docs(requirements): document final v0.2.17 requirements and provenance`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. 对固定官方 v0.2.17 与三个正式分支最终快照建立完整文件清单，覆盖新增、修改、删除、二进制、可执行位和生成物。
2. 每个本地差异路径只指定一个主功能提交，跨功能文件记录 related_groups；原提交、历史需求与最终 blob 均可追溯，不重放已废弃中间补丁。
3. 历史归档按原文件保留并明确日期；新默认、供应商已实现/待实现、BPS 例外及只手动 Actions 规则以本次和当前权威文档为准。
4. 除两处已说明的测试预期修正及本目录新归档外，三个分支所有原有路径 mode/blob 完全一致。四份旧 BPS 未提交文档单独备份，不并入正式需求实现。
5. 报告当前实际测试结果、未执行项、初次环境错误及修复方式；历史验收不冒充本轮重跑。发布与生产操作继续独立。

## 入口与配置归属

复用既有入口、配置和授权；具体落点见下方逐文件清单与关联功能组。

## 最终实现方案

1. 机器清单记录 base/final mode、blob、状态、行数、种类、主归属、关联组和来源提交；README 作为人工索引，20 个分组页列出具体需求与实现落点。
2. 保留此前 0.2.16 和更早来源归档，新 source-commits.json 固定本轮可达来源；提交中保留固定来源与需求链接。
3. 提交 SHA 索引只记录前置已生成提交，避免把文档提交自己的 SHA 写进自己形成循环依赖；完整最终三品牌 SHA 写入仓库外交付报告。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

全路径集合、mode/blob、主归属唯一性、目标文件树、分支祖先关系、旧历史可达性与远端精确 SHA 联合核验。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 121 | 17997 | 44 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 84 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| A | documentation | 37 | 0 | 07,20 | [AGENTS.md](https://github.com/holeenlu/sub2api/blob/main/AGENTS.md) |
| M | documentation | 8 | 8 | 20 | [docs/ADMIN_PAYMENT_INTEGRATION_API.md](https://github.com/holeenlu/sub2api/blob/main/docs/ADMIN_PAYMENT_INTEGRATION_API.md) |
| A | documentation | 33 | 0 | 20 | [docs/API_KEY_CONCURRENCY.md](https://github.com/holeenlu/sub2api/blob/main/docs/API_KEY_CONCURRENCY.md) |
| M | documentation | 9 | 9 | 20 | [docs/BATCH_IMAGE_MVP.md](https://github.com/holeenlu/sub2api/blob/main/docs/BATCH_IMAGE_MVP.md) |
| A | documentation | 42 | 0 | 20 | [docs/BRANCH_STRUCTURE.md](https://github.com/holeenlu/sub2api/blob/main/docs/BRANCH_STRUCTURE.md) |
| A | documentation | 29 | 0 | 20 | [docs/BRAND_TOOLING_PARITY.md](https://github.com/holeenlu/sub2api/blob/main/docs/BRAND_TOOLING_PARITY.md) |
| A | documentation | 114 | 0 | 20 | [docs/CHANGE_DELIVERY.md](https://github.com/holeenlu/sub2api/blob/main/docs/CHANGE_DELIVERY.md) |
| A | documentation | 49 | 0 | 20 | [docs/CODEX_DIAGNOSTIC_MONITOR.md](https://github.com/holeenlu/sub2api/blob/main/docs/CODEX_DIAGNOSTIC_MONITOR.md) |
| A | documentation | 14 | 0 | 20 | [docs/CODEX_MODEL_CATALOG_REFRESH.md](https://github.com/holeenlu/sub2api/blob/main/docs/CODEX_MODEL_CATALOG_REFRESH.md) |
| A | documentation | 73 | 0 | 20 | [docs/CODEX_PROBE_TEMPLATE.md](https://github.com/holeenlu/sub2api/blob/main/docs/CODEX_PROBE_TEMPLATE.md) |
| A | documentation | 176 | 0 | 07,12,20 | [docs/FORK_FEATURE_RETIREMENT.md](https://github.com/holeenlu/sub2api/blob/main/docs/FORK_FEATURE_RETIREMENT.md) |
| A | documentation | 75 | 0 | 20 | [docs/I18N_COMPLETENESS_AUDIT.md](https://github.com/holeenlu/sub2api/blob/main/docs/I18N_COMPLETENESS_AUDIT.md) |
| A | documentation | 491 | 0 | 20 | [docs/KEYS_PAGE_AND_BRANDING_PLAN.md](https://github.com/holeenlu/sub2api/blob/main/docs/KEYS_PAGE_AND_BRANDING_PLAN.md) |
| A | documentation | 55 | 0 | 20 | [docs/MODEL_CATALOG.md](https://github.com/holeenlu/sub2api/blob/main/docs/MODEL_CATALOG.md) |
| A | documentation | 33 | 0 | 20 | [docs/MODEL_CATALOG_CASCADING.md](https://github.com/holeenlu/sub2api/blob/main/docs/MODEL_CATALOG_CASCADING.md) |
| A | documentation | 80 | 0 | 20 | [docs/MODEL_CATALOG_INCIDENT_2026-10-01.md](https://github.com/holeenlu/sub2api/blob/main/docs/MODEL_CATALOG_INCIDENT_2026-10-01.md) |
| A | documentation | 48 | 0 | 06,07,20 | [docs/OPENAI_RESPONSES_RECOVERY.md](https://github.com/holeenlu/sub2api/blob/main/docs/OPENAI_RESPONSES_RECOVERY.md) |
| M | documentation | 8 | 8 | 20 | [docs/PAYMENT.md](https://github.com/holeenlu/sub2api/blob/main/docs/PAYMENT.md) |
| M | documentation | 8 | 8 | 20 | [docs/PAYMENT_CN.md](https://github.com/holeenlu/sub2api/blob/main/docs/PAYMENT_CN.md) |
| M | documentation | 11 | 11 | 20 | [docs/PLUGIN_DEVELOPMENT.md](https://github.com/holeenlu/sub2api/blob/main/docs/PLUGIN_DEVELOPMENT.md) |
| A | documentation | 98 | 0 | 20 | [docs/PRIVATE_REPOSITORY_SYNC.md](https://github.com/holeenlu/sub2api/blob/main/docs/PRIVATE_REPOSITORY_SYNC.md) |
| A | documentation | 40 | 0 | 20 | [docs/account-token-guard-v2.md](https://github.com/holeenlu/sub2api/blob/main/docs/account-token-guard-v2.md) |
| A | documentation | 100 | 0 | 20 | [docs/audit/2026-10-07-rbac-v03-review-fixes.md](https://github.com/holeenlu/sub2api/blob/main/docs/audit/2026-10-07-rbac-v03-review-fixes.md) |
| A | documentation | 414 | 0 | 20 | [docs/design/2026-10-06-three-tier-roles-permissions.md](https://github.com/holeenlu/sub2api/blob/main/docs/design/2026-10-06-three-tier-roles-permissions.md) |
| A | documentation | 36 | 0 | 20 | [docs/kdan-ci.md](https://github.com/holeenlu/sub2api/blob/main/docs/kdan-ci.md) |
| A | documentation | 256 | 0 | 20 | [docs/new-api-gateway-adoption-plan-2026-10-13.md](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-adoption-plan-2026-10-13.md) |
| A | documentation | 151 | 0 | 13,20 | [docs/new-api-gateway-implementation-report-2026-10-13.md](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-implementation-report-2026-10-13.md) |
| A | documentation | 18 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/README.md](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/README.md) |
| A | documentation | 97 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/body-resource-matrix.csv](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/body-resource-matrix.csv) |
| A | documentation | 1634 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/body-resource-matrix.json](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/body-resource-matrix.json) |
| A | documentation | 0 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/build.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/build.log) |
| A | documentation | 569 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/changed-files.json](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/changed-files.json) |
| A | documentation | 1 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/db-integration.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/db-integration.log) |
| A | documentation | 25 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/delivery-summary.json](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/delivery-summary.json) |
| A | documentation | 313 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/frontend-build.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/frontend-build.log) |
| A | documentation | 0 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/frontend-eslint.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/frontend-eslint.log) |
| A | documentation | 19 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/frontend-tests.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/frontend-tests.log) |
| A | documentation | 0 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/frontend-typecheck.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/frontend-typecheck.log) |
| A | documentation | 164 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/lint-baseline-full.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/lint-baseline-full.log) |
| A | documentation | 587 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/lint-baseline-uncapped.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/lint-baseline-uncapped.log) |
| A | documentation | 164 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/lint-current-full.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/lint-current-full.log) |
| A | documentation | 587 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/lint-current-uncapped.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/lint-current-uncapped.log) |
| A | documentation | 1 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/lint-new.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/lint-new.log) |
| A | documentation | 5 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/race.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/race.log) |
| A | documentation | 23 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/run-body-matrix.py](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/run-body-matrix.py) |
| A | documentation | 113 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/unit.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/unit.log) |
| A | documentation | 19 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/validation-summary.json](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/validation-summary.json) |
| A | documentation | 0 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/vet.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/vet.log) |
| A | documentation | 2 | 0 | 20 | [docs/new-api-gateway-validation-2026-10-13/wire.log](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-gateway-validation-2026-10-13/wire.log) |
| A | documentation | 146 | 0 | 20 | [docs/new-api-update-review-2026-10-13.md](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-update-review-2026-10-13.md) |
| A | documentation | 81 | 0 | 06,20 | [docs/openai-transports.md](https://github.com/holeenlu/sub2api/blob/main/docs/openai-transports.md) |
| A | documentation | 15 | 0 | 20 | [docs/openai-ws-sse-acceleration.md](https://github.com/holeenlu/sub2api/blob/main/docs/openai-ws-sse-acceleration.md) |
| A | documentation | 101 | 0 | 20 | [docs/requirements/final-2026-10-07/01-migration.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/01-migration.md) |
| A | documentation | 200 | 0 | 20 | [docs/requirements/final-2026-10-07/02-identity.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/02-identity.md) |
| A | documentation | 84 | 0 | 20 | [docs/requirements/final-2026-10-07/03-gateway.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/03-gateway.md) |
| A | documentation | 105 | 0 | 20 | [docs/requirements/final-2026-10-07/04-anthropic.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/04-anthropic.md) |
| A | documentation | 76 | 0 | 20 | [docs/requirements/final-2026-10-07/05-billing.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/05-billing.md) |
| A | documentation | 81 | 0 | 20 | [docs/requirements/final-2026-10-07/06-usage.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/06-usage.md) |
| A | documentation | 73 | 0 | 20 | [docs/requirements/final-2026-10-07/07-diagnostics.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/07-diagnostics.md) |
| A | documentation | 91 | 0 | 20 | [docs/requirements/final-2026-10-07/08-clients.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/08-clients.md) |
| A | documentation | 267 | 0 | 20 | [docs/requirements/final-2026-10-07/09-site.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/09-site.md) |
| A | documentation | 129 | 0 | 20 | [docs/requirements/final-2026-10-07/10-localization.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/10-localization.md) |
| A | documentation | 127 | 0 | 20 | [docs/requirements/final-2026-10-07/11-delivery.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/11-delivery.md) |
| A | documentation | 124 | 0 | 20 | [docs/requirements/final-2026-10-07/12-integration.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/12-integration.md) |
| A | documentation | 70 | 0 | 20 | [docs/requirements/final-2026-10-07/13-records.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/13-records.md) |
| A | documentation | 101 | 0 | 20 | [docs/requirements/final-2026-10-07/README.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/README.md) |
| A | documentation | 258 | 0 | 20 | [docs/requirements/final-2026-10-07/files-TapModels.tsv](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/files-TapModels.tsv) |
| A | documentation | 977 | 0 | 20 | [docs/requirements/final-2026-10-07/files-main.tsv](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/files-main.tsv) |
| A | documentation | 286 | 0 | 20 | [docs/requirements/final-2026-10-07/files-tokensavy.tsv](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/files-tokensavy.tsv) |
| A | documentation | 506 | 0 | 20 | [docs/requirements/final-2026-10-07/provenance.json](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-2026-10-07/provenance.json) |
| A | documentation | 49 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/00-upstream-version.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/00-upstream-version.md) |
| A | documentation | 123 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/01-migration.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/01-migration.md) |
| A | documentation | 227 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/02-identity.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/02-identity.md) |
| A | documentation | 120 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/03-gateway-security.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/03-gateway-security.md) |
| A | documentation | 125 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/04-accounts.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/04-accounts.md) |
| A | documentation | 94 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/05-models.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/05-models.md) |
| A | documentation | 210 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/06-recovery.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/06-recovery.md) |
| A | documentation | 385 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/07-bps.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/07-bps.md) |
| A | documentation | 124 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/08-thinking.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/08-thinking.md) |
| A | documentation | 91 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/09-billing.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/09-billing.md) |
| A | documentation | 133 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/10-usage.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/10-usage.md) |
| A | documentation | 88 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/11-diagnostics.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/11-diagnostics.md) |
| A | documentation | 106 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/12-clients.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/12-clients.md) |
| A | documentation | 288 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/13-site.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/13-site.md) |
| A | documentation | 157 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/14-localization.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/14-localization.md) |
| A | documentation | 155 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/15-delivery.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/15-delivery.md) |
| A | documentation | 175 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/16-integration.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/16-integration.md) |
| A | documentation | 161 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/17-records.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/17-records.md) |
| A | documentation | 31 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/BRAND-TapModels.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/BRAND-TapModels.md) |
| A | documentation | 31 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/BRAND-tokensavy.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/BRAND-tokensavy.md) |
| A | documentation | 67 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/NEW-API-INTEGRATION-2026-10-14.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/NEW-API-INTEGRATION-2026-10-14.md) |
| A | documentation | 89 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/README.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/README.md) |
| A | documentation | 64 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/VALIDATION.md](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/VALIDATION.md) |
| A | documentation | 19 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/commits-main.tsv](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/commits-main.tsv) |
| A | documentation | 259 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/files-TapModels.tsv](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/files-TapModels.tsv) |
| A | documentation | 1436 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/files-main.tsv](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/files-main.tsv) |
| A | documentation | 286 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/files-tokensavy.tsv](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/files-tokensavy.tsv) |
| A | documentation | 82 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-files.tsv](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-files.tsv) |
| A | documentation | 0 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/TapModels-build.log](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/TapModels-build.log) |
| A | documentation | 2 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/TapModels-unit.log](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/TapModels-unit.log) |
| A | documentation | 41 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/backend-summary.json](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/backend-summary.json) |
| A | documentation | 56 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/brands-summary.json](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/brands-summary.json) |
| A | documentation | 0 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/frontend-eslint.log](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/frontend-eslint.log) |
| A | documentation | 42 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/frontend-summary.json](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/frontend-summary.json) |
| A | documentation | 19 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/frontend-tests.log](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/frontend-tests.log) |
| A | documentation | 0 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/frontend-typecheck.log](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/frontend-typecheck.log) |
| A | documentation | 1 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/lint-incremental.log](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/lint-incremental.log) |
| A | documentation | 10 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/lint-summary.json](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/lint-summary.json) |
| A | documentation | 0 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/main-build.log](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/main-build.log) |
| A | documentation | 113 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/main-unit.log](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/main-unit.log) |
| A | documentation | 0 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/main-vet.log](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/main-vet.log) |
| A | documentation | 0 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/tokensavy-build.log](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/tokensavy-build.log) |
| A | documentation | 2 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/tokensavy-unit.log](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/tokensavy-unit.log) |
| A | documentation | 8 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/tree-audit.json](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/tree-audit.json) |
| A | documentation | 11 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/unchanged-groups.json](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/new-api-fold-validation/unchanged-groups.json) |
| A | documentation | 344 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/provenance.json](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/provenance.json) |
| A | documentation | 1338 | 0 | 20 | [docs/requirements/final-v0.2.16-2026-10-13/source-commits.json](https://github.com/holeenlu/sub2api/blob/main/docs/requirements/final-v0.2.16-2026-10-13/source-commits.json) |
| A | documentation | 37 | 0 | 20 | [openspec/changes/unify-model-catalog-and-pricing/design.md](https://github.com/holeenlu/sub2api/blob/main/openspec/changes/unify-model-catalog-and-pricing/design.md) |
| A | documentation | 22 | 0 | 20 | [openspec/changes/unify-model-catalog-and-pricing/proposal.md](https://github.com/holeenlu/sub2api/blob/main/openspec/changes/unify-model-catalog-and-pricing/proposal.md) |
| A | documentation | 17 | 0 | 20 | [openspec/changes/unify-model-catalog-and-pricing/tasks.md](https://github.com/holeenlu/sub2api/blob/main/openspec/changes/unify-model-catalog-and-pricing/tasks.md) |
| A | documentation | 32 | 0 | 20 | [openspec/changes/unify-model-catalog-and-pricing/verification.md](https://github.com/holeenlu/sub2api/blob/main/openspec/changes/unify-model-catalog-and-pricing/verification.md) |
