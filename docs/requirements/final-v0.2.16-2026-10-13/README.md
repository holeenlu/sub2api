# v0.2.16 最终需求、实现方案与功能提交重建

本次以用户指定的 Wei-Shaw v0.2.16 发布标签为起点，按当前已修改/提交/推送的最终代码归档并重建本地功能提交。不是逐条重放已被替代的旧补丁。2026-10-14 已把通过验收的 New API 实现按需求归并到对应本地提交，原需求和品牌覆盖继续保留。

## 固定输入

官方 release commit：`01842576b3d5c1e2545b5a99ad555d36e28ed306`；annotated tag object：`c3cb8588ce6a5aef4dc29fa4f71ac91f3c0bdf3d`。Git 远端标签已核对。

| 来源 | 固定提交 | 用途 |
| --- | --- | --- |
| main | 49b29ef158e6319cfd6b1227781307de5f8f870e | 已交付最终代码 |
| TapModels | 87fb1f2613667cb250525af8a4c7c0c12f03c2f6 | 已交付最终代码 |
| tokensavy | f58b507f2c774bb6e6f7f9b8915a84b60a1d3f7e | 已交付最终代码 |
| main 工作区 | acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286 | 包含 AGENTS.md、CHANGE_DELIVERY.md 未提交规则 |

原分支与原工作区继续保留；新分支见 provenance.json。另一个旧 BPS 工作区已单独检查点备份，只含未完成的文档更新，不用它回退当前已交付代码。

## 当前有效需求与历史取舍

- 三级角色、统一管理员策略、会话与财务授权以最终实现为准；策略编辑不要求 TOTP，超管创建/提升仍保留强校验。
- 原生网关、可信客户端 IP、图片出口、WS owner/客户端限制、Grok 语音隔离与 Fable 独立阈值保持。
- BPS 以 2026-10-12 明确恢复范围为准；旧 2026-10-07 归档、BPS_REMOVAL 中的旧退役说明只作历史，不覆盖当前例外。自动登录 Worker、Mihomo、票据/Cookie、额外准入和自动质量运营仍排除。
- Responses 完整密文/工具上下文、恢复/重试/取消/计费与 BPS 双路径共享契约；不恢复删密文盲重试。
- New API 更新吸收以 2026-10-13 规则覆盖整个网关价值与既有缺口；本次已纳入 G01–G11 的实际落地，等效覆盖和不采用项以实施报告为准。
- 官方已有功能保持在基线，不因文件存在就算本地新增；旧 SQL/遗留表保留不等于旧运行能力恢复。

## 功能、需求与提交清单

| 组 | 详细需求与方案 | 路径项 | 提交主题 |
| --- | --- | ---: | --- |
| 00 | [官方发布后的版本标记](00-upstream-version.md) | 1 | chore(upstream): retain v0.2.16 version marker |
| 01 | [数据库升级与历史数据保护](01-migration.md) | 65 | chore(schema): preserve final migrations and entity schemas |
| 02 | [三级角色、会话与管理授权](02-identity.md) | 151 | feat(auth): preserve roles sessions and usage export authorization |
| 03 | [原生网关安全与入站失败](03-gateway-security.md) | 42 | fix(gateway): preserve tenant safety and inbound body failures |
| 04 | [账号、Anthropic 授权、亲和历史与 Fable](04-accounts.md) | 60 | feat(accounts): preserve setup tokens sticky history and Fable thresholds |
| 05 | [账号模型权限、分组候选与目录顺序](05-models.md) | 11 | fix(models): preserve final restrictions catalogs and discovery order |
| 06 | [网关恢复、流终止、失败用量与传输诊断](06-recovery.md) | 89 | fix(gateway): preserve recovery and harden stream failure accounting |
| 07 | [BPS、独立 Excel 授权、工具图片与 WS 桥接](07-bps.md) | 234 | feat(bps): preserve Excel credentials and reliable HTTP WebSocket bridge |
| 08 | [最终模型能力、推理参数与协议语义](08-thinking.md) | 48 | feat(gateway): normalize model capabilities and preserve protocol semantics |
| 09 | [支付、退款与返利完整性](09-billing.md) | 37 | fix(billing): preserve final payment and refund invariants |
| 10 | [用量分析、页面加载与有界快照导出](10-usage.md) | 46 | feat(usage): preserve analytics and add bounded snapshot exports |
| 11 | [Codex 降智检测与定时诊断](11-diagnostics.md) | 32 | feat(diagnostics): preserve metered probes and scheduled diagnostics |
| 12 | [客户端配置、模型目录下载与修复工具](12-clients.md) | 47 | feat(clients): preserve setup catalogs and recovery downloads |
| 13 | [KDAN 品牌、文档站与用户界面](13-site.md) | 225 | feat(site): preserve final KDAN branding docs and presentation |
| 14 | [多语言与繁体生成](14-localization.md) | 95 | feat(i18n): preserve translations and generated traditional Chinese |
| 15 | [构建、手动发布、安装与在线更新](15-delivery.md) | 84 | chore(delivery): preserve manual releases and update tooling |
| 16 | [跨功能配置、路由、类型与依赖装配](16-integration.md) | 93 | refactor(core): preserve final shared wiring and configuration |
| 17 | [需求、方案、来源与工程规范](17-records.md) | 75 | docs(requirements): consolidate final requirements and New API evidence |

多功能共享文件仍仅落一个主提交，related_groups 标记完整关系；分类提交不承诺独立挑选。

## 品牌覆盖

| 品牌 | 需求方案 | 路径项 | 新本地分支 |
| --- | --- | --- | --- |
| TapModels | [查看](BRAND-TapModels.md) | 258 | codex/rebuild-v0.2.16-TapModels-20261013 |
| tokensavy | [查看](BRAND-tokensavy.md) | 285 | codex/rebuild-v0.2.16-tokensavy-20261013 |

两个品牌从新 main 继承同一共享历史，再按运行品牌、客户端工具、展示文档、交付运维、品牌记录五类覆盖最终差异。共享工作区规则更新继承到三者。

## 累计差异与本次新增

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| asset | 56 | 1611 | 23 | 48 |
| documentation | 232 | 19696 | 1131 | 0 |
| generated | 29 | 11045 | 39 | 0 |
| migration | 47 | 1278 | 0 | 0 |
| source | 586 | 52888 | 7587 | 0 |
| test | 409 | 37963 | 2456 | 0 |
| tooling | 76 | 48778 | 1086 | 0 |

相对旧重建引入业务 61 文件 +1616/-316、测试/基准 18 文件 +1025/-45、生成/示例 2 文件 +14/-6。新增 HTTP 入口 2、配置 3、导出流程 1，数据库结构变更 0。归并过程额外业务/测试改写均 0。

## 追溯与验证

- files-main.tsv、files-TapModels.tsv、files-tokensavy.tsv：完整变更路径、mode/blob 和分类。
- source-commits.json：当前原提交全文、作者、日期和旧归档全部 provenance。
- commits-main.tsv：00–17 的 18 个功能/规范提交映射；其后的第 19 个归档提交及当前品牌最终 SHA 见 /Users/luhonglin/Git/sub2api-history-20261014-new-api-fold/result.json；旧审计目录 DELIVERY.md 仅描述 2026-10-13 初始重建。
- provenance.json：固定基线、来源 tip、工作区检查点、备份与新本地分支。
- VALIDATION.md：本轮实际检查结果；历史测试证据保留在原 docs 中，不能冒充本轮运行。

## 交付边界

完整原历史 bundle 已存 `/Users/luhonglin/Git/sub2api-history-20261013-v0216-210726/source-history.bundle`。不删除旧分支/标签，不 reset 原工作区，不覆盖远端已发布历史。本地新提交可审查后再决定远端切换；若要覆盖必须明确授权并使用精确旧 SHA 的 force-with-lease。发布/部署/生产迁移仍属独立范围。

## 2026-10-14 New API 归并

[功能需求、合并理由、旧新提交及边界分析](NEW-API-INTEGRATION-2026-10-14.md)。原 19 提交结构保持，两个品牌各有 5 个覆盖提交；旧工作区仍保留未提交实现，正式分支和远端未替换。原实施报告状态按其验收时点解释。
