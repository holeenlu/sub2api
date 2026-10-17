# v0.2.17 最终需求、方案与提交重建

日期：2026-10-17。官方基线为 [Wei-Shaw/sub2api v0.2.17](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.17)，提交 `060d0160417782cd962a0c7f460f6cd4565d4916`。

按用户要求，将已修改、提交及推送后的最终有效内容按功能重新组织。main 代表 KDAN；共享代码和 KDAN 当前定制形成 20 笔提交，TapModels、tokensavy 各在新 main 上保留一笔最终品牌覆盖。旧修补、合并和反复适配过程不再作为这三个交付分支的祖先。

## 输入与完整性

| 分支 / 品牌 | 重建前最终 SHA | 最终文件树 |
| --- | --- | --- |
| main | `e6b3b332dc1fba704db6e07157c4b4674bed2437` | `1589b40aa70756ec5d8506ec67643fd7a686a8cb` |
| TapModels | `bf2c1d8580f5a00b3369f43e5edd0f2a5cde2ad4` | `6bfbb53cc548a635e4f75225a6437c4143fd5011` |
| tokensavy | `9cbb381ccb6d5871cf56540711791776fa1c2dc9` | `403194e70e64132f67b9f09e55e3f3e359692672` |

本次固定三品牌的最终已推送快照，整理其相对官方基线的全部 1874 个 main 路径项，以及 TapModels 的 263、tokensavy 的 290 个品牌差异路径项。统计关闭重命名识别，因此同一重命名可占两个路径项。

所有既有业务文件逐路径、blob 和 mode 保留；唯一既有文件修正是两个过期 SQL mock 断言，每个 +1 / −1，与已经存在的供应商归档写入保护一致。业务代码本次 +0 / −0，测试 +2 / −2，新增业务接口、配置项、执行路径均为 0。新增本目录为需求与证据归档。精确允许差异见 [test-corrections.json](test-corrections.json)。

159 个原可达来源提交的作者、正文与父子关系保存在 [source-commits.json](source-commits.json)。更早重建的来源仍由原归档和 TSV 的 historical_source_commits 保留；不虚构当前 Git 中已不存在的原始对象。官方基线已包含的功能直接继承，不重复当作本地新增。

旧 BPS 调查 worktree 中 4 个未提交文档属于较早调查现场，原字节、权限及补丁另行备份并保留，不覆盖最终已交付文件。完整 Git bundle 和现场证据位于 `/Users/luhonglin/Git/sub2api-rebuild-v0217-20261017-113657`；该备份不建立本地 Git 引用。

## 功能与需求提交目录

| 组 | 功能与需求 | 原差异路径项 | 新提交主题 |
| --- | --- | ---: | --- |
| 01 | [数据库升级、实体结构与历史数据保护](01-migration.md) | 57 | `chore(schema): preserve final migrations and entity contracts` |
| 02 | [角色、会话、管理授权与敏感操作](02-identity.md) | 151 | `feat(auth): preserve roles sessions and management authorization` |
| 03 | [网关安全、隔离、准入与敏感信息保护](03-security.md) | 55 | `fix(gateway): preserve tenant isolation and request safety` |
| 04 | [账号授权、调度、粘性会话与 Fable 阈值](04-accounts.md) | 71 | `feat(accounts): preserve authorization scheduling and Fable controls` |
| 05 | [模型权限、分组候选与目录顺序](05-models.md) | 11 | `fix(models): preserve restrictions catalogs and group selection` |
| 06 | [Responses 恢复、流终止、计费与 WS 所有权](06-recovery.md) | 91 | `fix(gateway): preserve recovery streaming and response ownership` |
| 07 | [BPS、Excel 独立授权、工具、图片与原生回退](07-bps.md) | 239 | `feat(bps): preserve final tools images recovery and native fallback` |
| 08 | [模型能力、推理参数与多协议兼容](08-protocols.md) | 57 | `feat(gateway): preserve model capabilities and protocol semantics` |
| 09 | [支付、退款、余额与返利完整性](09-billing.md) | 44 | `fix(billing): preserve payment refund and balance invariants` |
| 10 | [用量分析、统计快照与有界导出](10-usage.md) | 40 | `feat(usage): preserve analytics snapshots and bounded exports` |
| 11 | [降智检测、计费探测与定时诊断](11-diagnostics.md) | 32 | `feat(diagnostics): preserve metered probes and scheduled checks` |
| 12 | [供应商门户、资源运营、归档与结算](12-vendor.md) | 266 | `feat(vendor): preserve supplier portal operations and settlement` |
| 13 | [运维健康、SLA 分类、监控与运行指标](13-ops.md) | 65 | `fix(ops): preserve health scoring SLA and monitoring corrections` |
| 14 | [Claude Linux 指纹、CCH 与请求身份一致性](14-claude.md) | 31 | `fix(claude): preserve verified Linux fingerprint and CCH wire format` |
| 15 | [客户端配置、下载包与恢复工具](15-clients.md) | 47 | `feat(clients): preserve setup downloads and recovery tools` |
| 16 | [KDAN 品牌、文档站与公共界面](16-site.md) | 222 | `feat(site): preserve final KDAN branding and presentation` |
| 17 | [多语言与繁体生成](17-localization.md) | 93 | `feat(i18n): preserve translations and generated traditional Chinese` |
| 18 | [手动发布、渠道、构建、安装与在线更新](18-delivery.md) | 89 | `chore(delivery): preserve manual workflows and release channels` |
| 19 | [共享配置、路由、类型、依赖与装配](19-integration.md) | 92 | `refactor(core): preserve shared routes configuration and wiring` |
| 20 | [需求、方案、来源与逐文件核验归档](20-records.md) | 121 | `docs(requirements): document final v0.2.17 requirements and provenance` |

每组包含当前需求、实现方案、边界、验收要求和全部文件落点。共用文件只提交一次，TSV 的 related_groups 指明依赖。完整最终组合已经验证，不保证每个中间提交独立构建或任意 cherry-pick。

本目录的原始输入 SHA、文件树、TSV/blob 与累计体量保留重建时快照。2026-10-17 后续调度/恢复和供应商修订已更新对应功能组；当前行为及验证以各组链接的最新实施、验收章节和实际代码为准，不能仅凭本页原始快照重放旧补丁。

前 19 笔具体 SHA 见 [commits-main.tsv](commits-main.tsv)；第 20 笔包含该索引，避免在自身内容写入自身 SHA。最终三分支 SHA 由交付报告及远端引用核验。

## 品牌覆盖

- [TapModels 最终需求与文件](BRAND-TapModels.md)
- [Tokensavy 最终需求与文件](BRAND-tokensavy.md)

品牌覆盖只对应各自最终分支，不能用文件名中的品牌词替代实际差异核对。原历史品牌说明保留为来源；当前有效规范以 main 的 CHANGE_DELIVERY.md 和各分支实际配置为准。

## 当前需求的关键边界

1. BPS 新安装的全局与图片默认开启、默认 native，已有配置优先；保留工具、Excel 独立授权、分阶段图片预算、429 有界补试和安全同账号原生回退。密文、私有引用、已输出和不确定执行状态不允许盲目迁移重放。
2. 供应商两级角色、层级鉴权、双边合同/结算及直属资源只读聚合已在后续 V3 修订中实现；账号归档、私有出口、历史用量和冻结快照继续保留。当前范围及本地验证见 [第 12 组](12-vendor.md) 与 [统一平台验收](../../vendor-console/UNIFIED-PLATFORM-ACCEPTANCE.md)，不据此宣称已生产部署。
3. Claude 固定已验证的 Linux 指纹/CCH；Ops 维持当前健康分、SLA 和软删除分组规则。历史数据与原始日志不因重建改写。
4. 全部 GitHub Workflows 仅手动 workflow_dispatch，含质量检查。此次提交/推送不发版、不部署、不执行迁移；有效已发布标签、Release 与附件必须保留，避免影响在线更新。
5. 退役的票据、Cookie 绑定、自动登录 Worker、Mihomo 及额外运营体系不因历史整理恢复。数据库升级遗留和历史文档继续保存。

## 相对官方基线的累计体量

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| asset | 47 | 83 | 22 | 43 |
| documentation | 184 | 47682 | 1069 | 0 |
| generated | 122 | 84698 | 33127 | 4 |
| source | 918 | 86625 | 9009 | 0 |
| test | 530 | 54024 | 3226 | 0 |
| tooling | 73 | 34482 | 1082 | 0 |

source 含手写业务与 SQL；test、generated、tooling、asset、documentation 分列。以上是累计定制差异，包含已有删除，不是此次新增工作量；本次新增归档不计入本表。

## 可复核证据

- [全部 main 差异、最终 blob / mode、功能归属和来源](files-main.tsv)
- [TapModels 逐文件覆盖](files-TapModels.tsv)
- [Tokensavy 逐文件覆盖](files-tokensavy.tsv)
- [输入基点、目标树、草稿与完整性规则](provenance.json)
- [本轮验证、真实命令、初次失败处理与限制](VALIDATION.md)

正式引用切换前，必须从新提交树剔除本目录，再与 provenance.json 中各品牌 expected_trees_without_this_archive 做完整字典比较；最后逐一核对远端三个 SHA。旧本地 worktree HEAD 迁移到对应最终版本且仍 detached，避免旧提交继续出现在“所有分支”，同时不占用品牌分支检出。

官方基线的祖先历史仍保留；远端已发布版本标签保留原提交，以维持版本追溯与在线更新。它们不是无用标签，不删除或改指。原提交正文、来源 SHA 和外部 bundle 是证据，不是活动分支。
