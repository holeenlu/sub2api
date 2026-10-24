# 13 · 需求、历史说明与工程规范

归档日期：2026-10-07。文件快照：main `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238`；官方比较基线 `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`。

本页登记最终需求与实际文件归属；不把本轮提交整理等同于重新实现或完整安全审计。

## 最终需求

1. 以固定官方基线和三个最终已交付快照为唯一归档输入，记录需求、涉及文件、继承/定制/历史遗留边界及验证证据。历史设计/审计内容作为当时记录保留，不能覆盖后来的明确需求。

2. 旧 v0.2/v0.3 的讨论以最终三级角色实现和取消权限编辑 TOTP 为准；已撤销的白牌上游余额池/充值上限方案不纳入实施要求。

3. 本轮只新增需求说明、完整文件清单和来源记录，再重组本地提交；业务、测试、生成物、资源、迁移和运行配置的最终内容不改。

4. 不物理删除 Git 对象、不清理发布标签、不影响仍附着的旧工作树。为可恢复性保存本机完整 bundle；远端仍保留原历史，若后续要求推送需精确 SHA 的 lease 并再次核对差异。

## 入口与配置归属

本目录 README、01–13 需求文件、files-main.tsv、files-TapModels.tsv、files-tokensavy.tsv 和 provenance.json。

## 验收边界

每个相对基线的增删改路径恰好归入一组；两个品牌清单覆盖相对 main 的全部差异；新旧代码逐对象核对，不只比对文件数或 patch-id。

验证状态统一见 [总说明](README.md#验证和未关闭边界)。本页列的是验收要求，不是宣称本轮已重新运行这些检查。

## 对比统计

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 已有文档/许可 | 32 | 2280 | 44 | 0 |

统计为最终文件相对固定官方基线的累计差异，并非本次整理新写的代码。对重命名统一展开为删除/新增；跨域文件只设一个主归属。

## 修改文件

| 状态 | 类型 | +行 | −行 | 文件 |
| --- | --- | ---: | ---: | --- |
| A | 已有文档/许可 | 27 | 0 | [AGENTS.md](../../../AGENTS.md) |
| M | 已有文档/许可 | 8 | 8 | [docs/ADMIN_PAYMENT_INTEGRATION_API.md](../../../docs/ADMIN_PAYMENT_INTEGRATION_API.md) |
| A | 已有文档/许可 | 33 | 0 | [docs/API_KEY_CONCURRENCY.md](../../../docs/API_KEY_CONCURRENCY.md) |
| M | 已有文档/许可 | 9 | 9 | [docs/BATCH_IMAGE_MVP.md](../../../docs/BATCH_IMAGE_MVP.md) |
| A | 已有文档/许可 | 131 | 0 | [docs/BPS_REMOVAL.md](../../../docs/BPS_REMOVAL.md) |
| A | 已有文档/许可 | 42 | 0 | [docs/BRANCH_STRUCTURE.md](../../../docs/BRANCH_STRUCTURE.md) |
| A | 已有文档/许可 | 29 | 0 | [docs/BRAND_TOOLING_PARITY.md](../../../docs/BRAND_TOOLING_PARITY.md) |
| A | 已有文档/许可 | 86 | 0 | [docs/CHANGE_DELIVERY.md](../../../docs/CHANGE_DELIVERY.md) |
| A | 已有文档/许可 | 49 | 0 | [docs/CODEX_DIAGNOSTIC_MONITOR.md](../../../docs/CODEX_DIAGNOSTIC_MONITOR.md) |
| A | 已有文档/许可 | 14 | 0 | [docs/CODEX_MODEL_CATALOG_REFRESH.md](../../../docs/CODEX_MODEL_CATALOG_REFRESH.md) |
| A | 已有文档/许可 | 73 | 0 | [docs/CODEX_PROBE_TEMPLATE.md](../../../docs/CODEX_PROBE_TEMPLATE.md) |
| A | 已有文档/许可 | 166 | 0 | [docs/FORK_FEATURE_RETIREMENT.md](../../../docs/FORK_FEATURE_RETIREMENT.md) |
| A | 已有文档/许可 | 75 | 0 | [docs/I18N_COMPLETENESS_AUDIT.md](../../../docs/I18N_COMPLETENESS_AUDIT.md) |
| A | 已有文档/许可 | 491 | 0 | [docs/KEYS_PAGE_AND_BRANDING_PLAN.md](../../../docs/KEYS_PAGE_AND_BRANDING_PLAN.md) |
| A | 已有文档/许可 | 55 | 0 | [docs/MODEL_CATALOG.md](../../../docs/MODEL_CATALOG.md) |
| A | 已有文档/许可 | 33 | 0 | [docs/MODEL_CATALOG_CASCADING.md](../../../docs/MODEL_CATALOG_CASCADING.md) |
| A | 已有文档/许可 | 80 | 0 | [docs/MODEL_CATALOG_INCIDENT_2026-10-01.md](../../../docs/MODEL_CATALOG_INCIDENT_2026-10-01.md) |
| A | 已有文档/许可 | 15 | 0 | [docs/OPS_FEATURE_RETIREMENT_2026_09_29.md](../../../docs/OPS_FEATURE_RETIREMENT_2026_09_29.md) |
| M | 已有文档/许可 | 8 | 8 | [docs/PAYMENT.md](../../../docs/PAYMENT.md) |
| M | 已有文档/许可 | 8 | 8 | [docs/PAYMENT_CN.md](../../../docs/PAYMENT_CN.md) |
| M | 已有文档/许可 | 11 | 11 | [docs/PLUGIN_DEVELOPMENT.md](../../../docs/PLUGIN_DEVELOPMENT.md) |
| A | 已有文档/许可 | 96 | 0 | [docs/PRIVATE_REPOSITORY_SYNC.md](../../../docs/PRIVATE_REPOSITORY_SYNC.md) |
| A | 已有文档/许可 | 40 | 0 | [docs/account-token-guard-v2.md](../../../docs/account-token-guard-v2.md) |
| A | 已有文档/许可 | 100 | 0 | [docs/audit/2026-10-07-rbac-v03-review-fixes.md](../../../docs/audit/2026-10-07-rbac-v03-review-fixes.md) |
| A | 已有文档/许可 | 414 | 0 | [docs/design/2026-10-06-three-tier-roles-permissions.md](../../../docs/design/2026-10-06-three-tier-roles-permissions.md) |
| A | 已有文档/许可 | 36 | 0 | [docs/kdan-ci.md](../../../docs/kdan-ci.md) |
| A | 已有文档/许可 | 28 | 0 | [docs/openai-transports.md](../../../docs/openai-transports.md) |
| A | 已有文档/许可 | 15 | 0 | [docs/openai-ws-sse-acceleration.md](../../../docs/openai-ws-sse-acceleration.md) |
| A | 已有文档/许可 | 37 | 0 | [openspec/changes/unify-model-catalog-and-pricing/design.md](../../../openspec/changes/unify-model-catalog-and-pricing/design.md) |
| A | 已有文档/许可 | 22 | 0 | [openspec/changes/unify-model-catalog-and-pricing/proposal.md](../../../openspec/changes/unify-model-catalog-and-pricing/proposal.md) |
| A | 已有文档/许可 | 17 | 0 | [openspec/changes/unify-model-catalog-and-pricing/tasks.md](../../../openspec/changes/unify-model-catalog-and-pricing/tasks.md) |
| A | 已有文档/许可 | 32 | 0 | [openspec/changes/unify-model-catalog-and-pricing/verification.md](../../../openspec/changes/unify-model-catalog-and-pricing/verification.md) |
