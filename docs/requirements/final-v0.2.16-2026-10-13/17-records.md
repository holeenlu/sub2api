# 17 · 需求、方案、来源与工程规范

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。


2026-10-14 增量归并：本页已纳入通过验收的 New API 实现，来源和逐文件归属见 [归并分析](NEW-API-INTEGRATION-2026-10-14.md)。原快照 SHA 仅代表 2026-10-13 历史输入。

## 最终需求

1. 以固定官方基线和三个最终已交付快照为唯一归档输入，记录需求、涉及文件、继承/定制/历史遗留边界及验证证据。历史设计/审计内容作为当时记录保留，不能覆盖后来的明确需求。

2. 旧 v0.2/v0.3 的讨论以最终三级角色实现和取消权限编辑 TOTP 为准；已撤销的白牌上游余额池/充值上限方案不纳入实施要求。

3. 本轮只新增需求说明、完整文件清单和来源记录，再重组本地提交；业务、测试、生成物、资源、迁移和运行配置的最终内容不改。

4. 不物理删除 Git 对象、不清理发布标签、不影响仍附着的旧工作树。为可恢复性保存本机完整 bundle；远端仍保留原历史，若后续要求推送需精确 SHA 的 lease 并再次核对差异。

## 入口与配置归属

本目录 README、01–13 需求文件、files-main.tsv、files-TapModels.tsv、files-tokensavy.tsv 和 provenance.json。

## 最终实现方案

1. 保存旧需求/设计/审计作为历史证据，新目录明确最终有效规则及其优先级；BPS 例外覆盖过时退役说法。

2. 新增本次逐文件 mode/blob 清单、源提交正文与旧归档 provenance；每个相对官方基线变更路径恰好一次主归属。

3. 纳入当前 main 未提交的 AGENTS.md、CHANGE_DELIVERY.md 两项规则更新；分别备份另一个旧 BPS 工作区，不把其未完成文档混作已交付最终代码。

4. 原 tip、工作区检查点、完整 bundle 和新旧对象树校验支持恢复；不删除旧引用/标签，不执行远端历史覆盖。

## 边界、失败路径与依赖

关联功能组：06, 07。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。此组与关联组共同形成完整运行行为；本次归并不额外改写已验收的业务源码。

## 验收要求

文件清单覆盖/不重叠、历史来源存在、三个最终树扣除新文档后一致；原工作区/远端未被本任务重写。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| asset | 3 | 1521 | 0 | 0 |
| documentation | 71 | 8787 | 44 | 0 |
| tooling | 1 | 506 | 0 | 0 |

相对官方 v0.2.16 的累计差异，已包含本批实现；不是本次归并额外编写的代码。

## 来源提交

- `0d5ef648ec39915cfa09d560ae3c9fe43f866826`
- `1c56f12c4c90d9435e7959cdc25ff39106062a97`
- `230ddc10f4926e88f62de3caa805a5bfa0598bf0`
- `2e83e47bc12d60c125917a986706c205ee6a3e91`
- `4b2cfde513867c40dbcc663a5ddea30cdd286dd2`
- `4c5aa7b49988f5752b01940475530832c188b7ad`
- `64fa31719baa9fa11ffc36842b49608955d3720e`
- `6902e3b21a59df794601ff8fc13c2ac8f5824106`
- `880b96f67612767ed5955efa8b91b56fa2681daf`
- `a47851536b749381397de8bb8c10e2812928f08d`
- `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`
- `bb7c4d0a169b5ba3dbd5aa3cec957d52170d0265`
- `cba51ec0f6af3ded9bddfd41a3e8f5292181935b`
- `d75e372d49d61ba00e2afb6f7a77f090eda0d694`
- `e4bf78f132acb2d09d3f604a4cde873f5ebd2d29`
- `f9daed217b4d198a26178c1610ca02a8d9d903f9`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| A | documentation | 37 | 0 | 07,17 | [AGENTS.md](../../../AGENTS.md) |
| M | documentation | 8 | 8 | 17 | [docs/ADMIN_PAYMENT_INTEGRATION_API.md](../../../docs/ADMIN_PAYMENT_INTEGRATION_API.md) |
| A | documentation | 33 | 0 | 17 | [docs/API_KEY_CONCURRENCY.md](../../../docs/API_KEY_CONCURRENCY.md) |
| M | documentation | 9 | 9 | 17 | [docs/BATCH_IMAGE_MVP.md](../../../docs/BATCH_IMAGE_MVP.md) |
| A | documentation | 42 | 0 | 17 | [docs/BRANCH_STRUCTURE.md](../../../docs/BRANCH_STRUCTURE.md) |
| A | documentation | 29 | 0 | 17 | [docs/BRAND_TOOLING_PARITY.md](../../../docs/BRAND_TOOLING_PARITY.md) |
| A | documentation | 112 | 0 | 17 | [docs/CHANGE_DELIVERY.md](../../../docs/CHANGE_DELIVERY.md) |
| A | documentation | 49 | 0 | 17 | [docs/CODEX_DIAGNOSTIC_MONITOR.md](../../../docs/CODEX_DIAGNOSTIC_MONITOR.md) |
| A | documentation | 14 | 0 | 17 | [docs/CODEX_MODEL_CATALOG_REFRESH.md](../../../docs/CODEX_MODEL_CATALOG_REFRESH.md) |
| A | documentation | 73 | 0 | 17 | [docs/CODEX_PROBE_TEMPLATE.md](../../../docs/CODEX_PROBE_TEMPLATE.md) |
| A | documentation | 172 | 0 | 07,17 | [docs/FORK_FEATURE_RETIREMENT.md](../../../docs/FORK_FEATURE_RETIREMENT.md) |
| A | documentation | 75 | 0 | 17 | [docs/I18N_COMPLETENESS_AUDIT.md](../../../docs/I18N_COMPLETENESS_AUDIT.md) |
| A | documentation | 491 | 0 | 17 | [docs/KEYS_PAGE_AND_BRANDING_PLAN.md](../../../docs/KEYS_PAGE_AND_BRANDING_PLAN.md) |
| A | documentation | 55 | 0 | 17 | [docs/MODEL_CATALOG.md](../../../docs/MODEL_CATALOG.md) |
| A | documentation | 33 | 0 | 17 | [docs/MODEL_CATALOG_CASCADING.md](../../../docs/MODEL_CATALOG_CASCADING.md) |
| A | documentation | 80 | 0 | 17 | [docs/MODEL_CATALOG_INCIDENT_2026-10-01.md](../../../docs/MODEL_CATALOG_INCIDENT_2026-10-01.md) |
| A | documentation | 48 | 0 | 06,07,17 | [docs/OPENAI_RESPONSES_RECOVERY.md](../../../docs/OPENAI_RESPONSES_RECOVERY.md) |
| A | documentation | 15 | 0 | 17 | [docs/OPS_FEATURE_RETIREMENT_2026_09_29.md](../../../docs/OPS_FEATURE_RETIREMENT_2026_09_29.md) |
| M | documentation | 8 | 8 | 17 | [docs/PAYMENT.md](../../../docs/PAYMENT.md) |
| M | documentation | 8 | 8 | 17 | [docs/PAYMENT_CN.md](../../../docs/PAYMENT_CN.md) |
| M | documentation | 11 | 11 | 17 | [docs/PLUGIN_DEVELOPMENT.md](../../../docs/PLUGIN_DEVELOPMENT.md) |
| A | documentation | 96 | 0 | 17 | [docs/PRIVATE_REPOSITORY_SYNC.md](../../../docs/PRIVATE_REPOSITORY_SYNC.md) |
| A | documentation | 40 | 0 | 17 | [docs/account-token-guard-v2.md](../../../docs/account-token-guard-v2.md) |
| A | documentation | 100 | 0 | 17 | [docs/audit/2026-10-07-rbac-v03-review-fixes.md](../../../docs/audit/2026-10-07-rbac-v03-review-fixes.md) |
| A | documentation | 414 | 0 | 17 | [docs/design/2026-10-06-three-tier-roles-permissions.md](../../../docs/design/2026-10-06-three-tier-roles-permissions.md) |
| A | documentation | 36 | 0 | 17 | [docs/kdan-ci.md](../../../docs/kdan-ci.md) |
| A | documentation | 256 | 0 | 17 | [docs/new-api-gateway-adoption-plan-2026-10-13.md](../../../docs/new-api-gateway-adoption-plan-2026-10-13.md) |
| A | documentation | 123 | 0 | 17 | [docs/new-api-gateway-implementation-report-2026-10-13.md](../../../docs/new-api-gateway-implementation-report-2026-10-13.md) |
| A | documentation | 18 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/README.md](../../../docs/new-api-gateway-validation-2026-10-13/README.md) |
| A | documentation | 97 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/body-resource-matrix.csv](../../../docs/new-api-gateway-validation-2026-10-13/body-resource-matrix.csv) |
| A | documentation | 1634 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/body-resource-matrix.json](../../../docs/new-api-gateway-validation-2026-10-13/body-resource-matrix.json) |
| A | documentation | 0 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/build.log](../../../docs/new-api-gateway-validation-2026-10-13/build.log) |
| A | documentation | 569 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/changed-files.json](../../../docs/new-api-gateway-validation-2026-10-13/changed-files.json) |
| A | documentation | 1 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/db-integration.log](../../../docs/new-api-gateway-validation-2026-10-13/db-integration.log) |
| A | documentation | 25 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/delivery-summary.json](../../../docs/new-api-gateway-validation-2026-10-13/delivery-summary.json) |
| A | documentation | 313 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/frontend-build.log](../../../docs/new-api-gateway-validation-2026-10-13/frontend-build.log) |
| A | documentation | 0 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/frontend-eslint.log](../../../docs/new-api-gateway-validation-2026-10-13/frontend-eslint.log) |
| A | documentation | 19 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/frontend-tests.log](../../../docs/new-api-gateway-validation-2026-10-13/frontend-tests.log) |
| A | documentation | 0 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/frontend-typecheck.log](../../../docs/new-api-gateway-validation-2026-10-13/frontend-typecheck.log) |
| A | documentation | 164 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/lint-baseline-full.log](../../../docs/new-api-gateway-validation-2026-10-13/lint-baseline-full.log) |
| A | documentation | 587 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/lint-baseline-uncapped.log](../../../docs/new-api-gateway-validation-2026-10-13/lint-baseline-uncapped.log) |
| A | documentation | 164 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/lint-current-full.log](../../../docs/new-api-gateway-validation-2026-10-13/lint-current-full.log) |
| A | documentation | 587 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/lint-current-uncapped.log](../../../docs/new-api-gateway-validation-2026-10-13/lint-current-uncapped.log) |
| A | documentation | 1 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/lint-new.log](../../../docs/new-api-gateway-validation-2026-10-13/lint-new.log) |
| A | documentation | 5 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/race.log](../../../docs/new-api-gateway-validation-2026-10-13/race.log) |
| A | documentation | 23 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/run-body-matrix.py](../../../docs/new-api-gateway-validation-2026-10-13/run-body-matrix.py) |
| A | documentation | 113 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/unit.log](../../../docs/new-api-gateway-validation-2026-10-13/unit.log) |
| A | documentation | 19 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/validation-summary.json](../../../docs/new-api-gateway-validation-2026-10-13/validation-summary.json) |
| A | documentation | 0 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/vet.log](../../../docs/new-api-gateway-validation-2026-10-13/vet.log) |
| A | documentation | 2 | 0 | 17 | [docs/new-api-gateway-validation-2026-10-13/wire.log](../../../docs/new-api-gateway-validation-2026-10-13/wire.log) |
| A | documentation | 146 | 0 | 17 | [docs/new-api-update-review-2026-10-13.md](../../../docs/new-api-update-review-2026-10-13.md) |
| A | documentation | 81 | 0 | 06,17 | [docs/openai-transports.md](../../../docs/openai-transports.md) |
| A | documentation | 15 | 0 | 17 | [docs/openai-ws-sse-acceleration.md](../../../docs/openai-ws-sse-acceleration.md) |
| A | documentation | 101 | 0 | 17 | [docs/requirements/final-2026-10-07/01-migration.md](../../../docs/requirements/final-2026-10-07/01-migration.md) |
| A | documentation | 200 | 0 | 17 | [docs/requirements/final-2026-10-07/02-identity.md](../../../docs/requirements/final-2026-10-07/02-identity.md) |
| A | documentation | 84 | 0 | 17 | [docs/requirements/final-2026-10-07/03-gateway.md](../../../docs/requirements/final-2026-10-07/03-gateway.md) |
| A | documentation | 105 | 0 | 17 | [docs/requirements/final-2026-10-07/04-anthropic.md](../../../docs/requirements/final-2026-10-07/04-anthropic.md) |
| A | documentation | 76 | 0 | 17 | [docs/requirements/final-2026-10-07/05-billing.md](../../../docs/requirements/final-2026-10-07/05-billing.md) |
| A | documentation | 81 | 0 | 17 | [docs/requirements/final-2026-10-07/06-usage.md](../../../docs/requirements/final-2026-10-07/06-usage.md) |
| A | documentation | 73 | 0 | 17 | [docs/requirements/final-2026-10-07/07-diagnostics.md](../../../docs/requirements/final-2026-10-07/07-diagnostics.md) |
| A | documentation | 91 | 0 | 17 | [docs/requirements/final-2026-10-07/08-clients.md](../../../docs/requirements/final-2026-10-07/08-clients.md) |
| A | documentation | 267 | 0 | 17 | [docs/requirements/final-2026-10-07/09-site.md](../../../docs/requirements/final-2026-10-07/09-site.md) |
| A | documentation | 129 | 0 | 17 | [docs/requirements/final-2026-10-07/10-localization.md](../../../docs/requirements/final-2026-10-07/10-localization.md) |
| A | documentation | 127 | 0 | 17 | [docs/requirements/final-2026-10-07/11-delivery.md](../../../docs/requirements/final-2026-10-07/11-delivery.md) |
| A | documentation | 124 | 0 | 17 | [docs/requirements/final-2026-10-07/12-integration.md](../../../docs/requirements/final-2026-10-07/12-integration.md) |
| A | documentation | 70 | 0 | 17 | [docs/requirements/final-2026-10-07/13-records.md](../../../docs/requirements/final-2026-10-07/13-records.md) |
| A | documentation | 101 | 0 | 17 | [docs/requirements/final-2026-10-07/README.md](../../../docs/requirements/final-2026-10-07/README.md) |
| A | asset | 258 | 0 | 17 | [docs/requirements/final-2026-10-07/files-TapModels.tsv](../../../docs/requirements/final-2026-10-07/files-TapModels.tsv) |
| A | asset | 977 | 0 | 17 | [docs/requirements/final-2026-10-07/files-main.tsv](../../../docs/requirements/final-2026-10-07/files-main.tsv) |
| A | asset | 286 | 0 | 17 | [docs/requirements/final-2026-10-07/files-tokensavy.tsv](../../../docs/requirements/final-2026-10-07/files-tokensavy.tsv) |
| A | tooling | 506 | 0 | 17 | [docs/requirements/final-2026-10-07/provenance.json](../../../docs/requirements/final-2026-10-07/provenance.json) |
| A | documentation | 37 | 0 | 17 | [openspec/changes/unify-model-catalog-and-pricing/design.md](../../../openspec/changes/unify-model-catalog-and-pricing/design.md) |
| A | documentation | 22 | 0 | 17 | [openspec/changes/unify-model-catalog-and-pricing/proposal.md](../../../openspec/changes/unify-model-catalog-and-pricing/proposal.md) |
| A | documentation | 17 | 0 | 17 | [openspec/changes/unify-model-catalog-and-pricing/tasks.md](../../../openspec/changes/unify-model-catalog-and-pricing/tasks.md) |
| A | documentation | 32 | 0 | 17 | [openspec/changes/unify-model-catalog-and-pricing/verification.md](../../../openspec/changes/unify-model-catalog-and-pricing/verification.md) |

## 关联实现（由其他组提交）

同一共享文件保持单一主提交，跨组依赖随完整提交集验收。

| 主归属组 | 类别 | 文件 |
| --- | --- | --- |
| 07 | documentation | [docs/BPS_REMOVAL.md](../../../docs/BPS_REMOVAL.md) |

## 2026-10-14 新增需求与实施边界

把已实施的 New API 计划、验收、日志、资源矩阵与本轮归并分析纳入版本控制；历史报告里的未提交状态只描述实施阶段。
