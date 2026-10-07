# 最终定制需求、文件归属与提交重组（2026-10-07）

## 归档依据

本次把已交付的最终源码按功能与文件主归属重新组织，并补齐可审查的需求说明。只整理本地 Git 历史和新增本目录文档，**不新增或回退业务行为**。历史方案中的“待实现”、测试结果和用户后来的要求不能混在同一份完成声明中。

固定官方基线：Wei-Shaw/sub2api `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`（VERSION 0.2.14，本仓库此前已合入的版本）。本次没有拉取或升级新的上游功能，也不把这个固定版本称作互联网最新版本。

| 原分支 | 整理前最终提交 |
| --- | --- |
| main | `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238` |
| TapModels | `a2d3feb5fe7c4e79ec280c04f20714aed78fa68f` |
| tokensavy | `fcc675b664360da27b3fccbafce32c7de55647b2` |

这三个原提交包含此前已推送的 RBAC 修复、取消策略编辑 TOTP、用户并发数保存修复和页面加载优化。当前用户端“可用渠道”开关与管理端“渠道管理”入口是两项功能；本轮不因此改变导航规则。

## 需求优先级和排除项

- 三级角色最终规则以第 02 组和最新 v0.3 设计为准。统一管理员策略编辑无需 TOTP；其他敏感动作按原规则执行。
- Anthropic 长期 sticky history 与 claude setup-token 凭据导入分别定义，不能用“一年 token”解释 Redis 亲和历史，也不能承诺导入后重置供应商的凭据有效期。
- /usage 只显示本人数据，API 密钥排行不显示所属用户；/admin/usage 在获准的管理范围显示所属用户及全站业务数据。
- 先前撤销的白牌上游余额镜像、跨站充值池和“下游充值不能超过上游余额”设计，不在实现范围内。
- 原生 Passkey/Step-up、Model Plaza、模型远程目录、用量/Ops/Channel Monitor、支付提供方等不是仅因出现在页面里就算 fork 新增；这里只记录真实差异。
- 已退役 BPS、票据/Cookie 绑定、额外 Key 准入队列、独立模型目录治理、账号质量运营和请求抓取不重新导入。迁移/历史说明存在不等于运行入口存在。

## 功能分组和提交边界

各编号对应一个文件归属组和一个本地分类提交。主归属按文件的职责确定；如账号 handler 同时包含 RBAC、Setup Token 和列表优化，完整最终文件只在一个主归属提交中落地，需求页注明关联。

这些分类提交共同构成完整交付集；共享路由、SettingsView、DTO、装配、语言包和生成物存在跨组依赖。它们用于审查与追溯，**不能把“按功能分类”理解成任意单条提交可独立 cherry-pick 或部署**。要移植单一能力需审查依赖，完整安装以最终分支树为准。本次不为了制造独立补丁拆出新的运行时层。

| 编号 | 功能与详细需求 | 文件路径项 | 提交主题 |
| --- | --- | ---: | --- |
| 01 | [数据库升级与历史兼容](01-migration.md) | 61 | `chore(schema): consolidate preserved migrations and entity schemas` |
| 02 | [三级角色、会话与管理授权](02-identity.md) | 151 | `feat(auth): consolidate three-tier roles and session protections` |
| 03 | [原生网关与多媒体安全](03-gateway.md) | 41 | `fix(gateway): consolidate native forwarding and tenant isolation` |
| 04 | [账号、Anthropic 授权与调度](04-anthropic.md) | 60 | `feat(accounts): consolidate setup tokens sticky history and Fable thresholds` |
| 05 | [支付、退款与返利完整性](05-billing.md) | 37 | `fix(billing): consolidate payment and refund invariants` |
| 06 | [用量排行与页面加载](06-usage.md) | 37 | `feat(usage): consolidate rankings and page loading improvements` |
| 07 | [Codex 降智检测与定时诊断](07-diagnostics.md) | 32 | `feat(diagnostics): consolidate Codex probes and scheduled tests` |
| 08 | [API Key 客户端配置与下载工具](08-clients.md) | 47 | `feat(clients): consolidate Codex setup catalogs and downloads` |
| 09 | [品牌界面、文档站与用户体验](09-site.md) | 223 | `feat(site): consolidate KDAN presentation and documentation portal` |
| 10 | [多语言与繁体生成](10-localization.md) | 89 | `feat(i18n): consolidate translations and generated traditional Chinese` |
| 11 | [构建、手动发布与在线更新](11-delivery.md) | 84 | `chore(delivery): consolidate manual release and update tooling` |
| 12 | [跨功能设置、路由与依赖装配](12-integration.md) | 82 | `refactor(core): consolidate shared settings routes and dependency wiring` |
| 13 | [需求、历史说明与工程规范](13-records.md) | 32 | `docs(requirements): record final feature requirements and file inventories` |

## 文件清单如何使用

- [main 全量差异](files-main.tsv)：以固定官方基线比较最终 main，列明功能组、文件类型、A/M/D、增删行和路径。
- [TapModels 全量品牌差异](files-TapModels.tsv)、[tokensavy 全量品牌差异](files-tokensavy.tsv)：分别与整理前 main 比较。差异不一定只有常量，也包括部署、文档、资源、测试和历史品牌工具适配，全部保留。
- [来源与旧提交清单](provenance.json)：原提交/树、父提交、作者、主题、分组及本次零运行时变更声明。官方祖先和原许可证保留；该文件不将原作者修改冒称为本轮新开发。

普通 Git rename 展示中 main 差异为 975 files changed, 99389 insertions(+), 8093 deletions(-)。清单为可机械核对的 `--no-renames` 口径，共 976 个路径项：重命名源/目标各一项，不是额外改动。两个品牌相对 main 分别有 257、285 项。各分组页使用同一口径，避免将两个品牌与 main 的共同代码重复计算。

## 本次增删与既有累计差异

本次业务代码 **+0/−0**、测试 **+0/−0**；新增 HTTP 接口 **0**、运行时配置项 **0**、执行路径 **0**、迁移 **0**。新增内容仅本目录的需求/来源/清单文档。以下是整理前代码相对官方基线的累计差异，不能写成本次实现量。

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 工具/构建/配置 | 70 | 3516 | 1089 | 0 |
| 已有文档/许可 | 139 | 7500 | 1120 | 0 |
| 测试/夹具 | 212 | 12835 | 686 | 0 |
| 图像/下载资源 | 51 | 82 | 22 | 48 |
| 业务代码 | 402 | 32294 | 5169 | 0 |
| 生成代码/繁体 | 55 | 12458 | 32 | 0 |
| 运行时指纹/模板数据 | 2 | 29467 | 0 | 0 |
| SQL 迁移 | 45 | 1262 | 0 | 0 |

统计将 Ent/装配/繁体生成物、测试、SQL、构建工具、下载/图片资源和指纹数据单列。二进制没有文本行数。RBAC 等旧审计的单轮增删统计有各自基线，不与本表直接相加。

## 三品牌重组方式

最终 main 保留官方祖先，在其后按上表形成一组分类提交。两个品牌各以新 main 为直接父提交，增加一个品牌适配提交；适配内容来自各自原最终树，无品牌替换、统一名称或额外功能移植。这样两条品牌分支采用对称结构，不再层叠旧的同功能提交和重复 merge 记录。

这是用户本次明确要求的**本地重新提交**，不是日常同步方式变更。后续仍遵守 [需求交付规范](../../CHANGE_DELIVERY.md)：共享修改先 main，普通 merge 至品牌。远端本轮未改写；后续若要求推送，应核对远端实际旧 SHA，再采用精确 force-with-lease。发布标签和仍附着的旧工作分支未删除，所以“所有分支/标签”视图仍可能显示旧图，这不等于新交付分支依赖旧图。

完整本机恢复包位于忽略目录 `docs/audit/history-reorganization-20261007/before.bundle`；它不随代码提交。来源 JSON 已记录原公开提交，恢复包验证通过后才移动本地引用。重组后逐文件核对三个代码树；仅允许本目录新增说明文档。

## 验证和未关闭边界

**本轮验证的是整理的完整性，不重新声称完成所有功能的安全审计。**核对项为：源 tip 未变化、每个路径恰有一个归属、无漏掉删除/二进制文件、品牌最终差异原样保留、原迁移/许可证/生成物/运行配置对象一致，以及最终 main 为两个新品牌分支共同祖先。结果记录在本机交付回执，实际新 SHA 由 Git 日志给出。

此前同一代码内容已有以下验证记录：

| 范围 | 已有证据 | 限制 |
| --- | --- | --- |
| 页面性能优化 | 前端全套 355 文件/2726 测试；类型、相关 ESLint；后端构建/vet、账号列表定向测试及 race | 当时快照；不是本轮重跑或生产压测 |
| 用户并发数保存 | 修复前真实库超时，修复后 super_admin/admin 均可改为 0/8，外事务回滚；RBAC/redeem 集成套件通过 | 本地临时 PostgreSQL/Redis，没有修改生产用户 |
| 取消权限保存 TOTP | 后端允许超管/拒绝 admin、user、机器 Key/保留冲突；前端 9 项及类型/lint/繁体检查 | 只覆盖该修改，不宣布全站 MFA 审计通过 |
| 两个品牌上次同步 | 后端构建、权限/账号列表定向回归；前端类型、各 70 项测试；繁体、下载包、手动发版约束 | 代码树分别为本页原快照，不是线上端到端测试 |
| 三级角色原审计 | [历史修复报告](../../audit/2026-10-07-rbac-v03-review-fixes.md) 与 [设计](../../design/2026-10-06-three-tier-roles-permissions.md) | 原报告时间/提交状态属历史；其中策略 TOTP 要求已被后续明确取消 |

已知边界和待跟进事项：

- 未采集本轮线上 HAR、生产慢 SQL/EXPLAIN 或真实支付/上游请求；不能承诺页面每次加载耗时或所有资金流程已经实网验收。
- 历史审计提出的 TOTP 防重放与原子限速、外部指纹库固定 commit、退款/返利/日限额其他边界、HTTP 重复键、孤儿音色和幽灵迁移需要各自复核关闭；本轮仅归档，不把建议改写为“已完成”。
- 既有内存阈值测试曾有负载相关偶发失败；未放宽断言或借重组删除测试。
- 旧模型目录、BPS、Key 并发、账号守护等设计文件可能描述已经退役方案。当前边界优先看本需求集和 [原生能力恢复说明](../../FORK_FEATURE_RETIREMENT.md)，旧文档保留仅作历史追溯。
- 原生多角色迁移不能由新旧二值角色实例混跑；生产升级与迁移始终独立授权。本轮不执行发版、部署、迁移或删除数据库对象。
