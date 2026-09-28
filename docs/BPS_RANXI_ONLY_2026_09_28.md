# BPS 范围调整与 ranxi2001 增量审查

日期：2026-09-28。归属：公共能力；按需求交付规范先落公共，再普通 merge 到 KDAN、TapModels。本轮只做本地代码与验证，不推送、不发布镜像、不操作生产数据库。

## 范围与行为

用户要求移除之前同步的 sub4api BPS 功能，仅保留 ranxi2001 相关能力。本次删除独立 `openai_bps` 平台的转发/凭据/JWT 诊断、Redis 会话/压缩/工具状态、专属模型目录，以及账号/分组/Composite 路由/配额/监测/UI 支持。保留原有其他公共与品牌功能。

唯一保留的 BPS 模式：`platform=openai,type=oauth`，由 `extra.openai_excel_bps` 及映射后模型范围选择。入口为 **账号管理（/admin/accounts）→ 编辑 OpenAI OAuth 账号 → Excel / BPS 协议**；批量编辑继续支持。文本/SSE、工具历史与回放、结构化输出、图片中转和原生附件、403 处理、独立 BPS 冷却、工具往返探测、OAuth RPM 控制均保留。图片配置仍在 **系统设置 → 功能开关 → Excel / BPS 图片支持**。原有配置默认值不改变，无需重新开启现有 OAuth BPS 开关。

保留 `openai_excel_bps*.go`、`account_excel_bps*.go`、`basispoints/` 及实际必需的共享依赖。打票与 BPS 的隔离、当前原生 OAuth 通道保持原逻辑。本次没有合入工作目录中原有的 13 个原生回退草稿文件；以独立补丁保留，并另行验证与本次改动兼容。

## 升级与旧数据

新增 `backend/migrations/254_retire_standalone_bps.sql`：停用旧平台账号和分组并写入调度 outbox；关闭相应监测、Composite 路由和定时测试；清除旧平台测试计划的下一次/占用时间。只针对 `openai_bps`，不更改 `openai` OAuth 的 BPS、RPM 或票据状态。历史 SQL 不改写，账号凭据、历史记录与原始诊断不删除。

运行时仍保留一个旧平台识别标记，用于阻止旧快照/旧 API Key 调度到其他协议。旧账号不能创建、恢复、刷新或转发，旧分组不能重新启用或原地改成其他平台；网关对旧平台返回 410 `platform_retired`（鉴权/分组校验先拒绝时保持其原错误）。支持的平台列表中不再包含它。这些退役保护与历史数据库枚举不属于功能保留。

迁移在后续部署启动时由已有迁移机制应用。旧独立平台用户须管理员新增/选择有效的 OpenAI OAuth 账号，配置 Excel/BPS，并将 API Key 绑定可用 OpenAI/Composite 分组。独立 Access Token 不自动转换为 OAuth。旧 Redis 专属键不再读写，按原 TTL 自行过期。

## ranxi2001 最新提交审查

已定向 fetch `ranxi2001/production` 并通过 Git 远端查询确认，最终固定审查 tip 为 `6d7a6964b1da85c62f44bb4fc4473d4f164e4432`，合并时间为 2026-09-28 16:01:30 +08:00。初次检查至 `d65b8d6849ae01160a93fd64e88d40cdabe4ba1a`，结束前再次查询发现远端前进后已补查。上次审查 tip 为 `3dafea660a0a9c1a607c914a0391ddf916673811`。本轮完整增量为 6 个非 merge 提交、90 个文件，其中直接 BPS 功能为下面 2 个提交；merge 包装不重复计算。

| 源提交 | 时间（+08:00） | 实际能力与依赖 | 本轮处理 |
| --- | --- | --- | --- |
| `b3e494dbdb90db789f7c06a1453b642892badbe2` | 09-28 13:24:57 | 质量规则按连续降智次数、5 小时/7 天用量阈值启用 BPS；支持任一/全部条件。连续满血达到次数后恢复原设置，可在用量仍高时暂缓恢复。账号弹窗新增自动 BPS 设置。 | 仅审查，未导入 |
| `211202d4a8abfe7d1b8d51f5edc8cda19ecdf50f` | 09-28 14:47:16 | 恢复时把省略 false 与默认代理来源视为等价，仍保护人工改动；规则改为别的动作后继续完成待恢复探测；阻止已被其他质量规则（包括暂停规则）占用的账号重复建立自动 BPS 规则。 | 仅审查，未导入 |

追加审查、未导入的 4 个提交：

- `906883c38ecdcfe23cbfd5f0b6bf2890b31106de`：鹈鹕测智改为按分组测试，通过既有网关调度器选择账号，新增分组测试表与智能运维入口。与 BPS 的联系是可测试到 BPS 账号，不修改底层协议；依赖已删除的鹈鹕/运维子系统，不属于本轮恢复范围。
- `4414b9364b876922dce6fd868f85f8d019f2e9e4`：成功 2FA 登录后把邮箱/密码/2FA 保存到凭证守护，串行化配置读写；属于已删除的凭证守护，不是现有 OAuth 刷新或 BPS 协议修复。
- `a6192bbe45211fa8f6d51364c1b9d049710f27c3`：账号运维和凭证守护页面改为全宽；与 BPS 协议无直接关系。
- `13bd676e2836096bfb4d53031699f92823d991ab`：上述布局的前后截图文档，无运行时代码。

这些是 **BPS 自动启停/恢复的质量运维功能**，没有改动 `basispoints/` 或 `openai_excel_bps*.go` 的底层协议实现。依赖本地此前已删除的 `account_quality` 状态事务、鹈鹕定时计划、质量管理 UI；选项还包括 Mihomo/IP 池会话代理。不能整块套入当前项目。按本轮“检查更新”的范围记录候选，不恢复这些子系统。后续若需要自动 BPS，应另行适配现有调度与探测基础，并排除 Mihomo/IP 池依赖。

核对代码：`backend/internal/service/account_quality_bps.go`、`backend/internal/repository/account_quality_bps.go`、`scheduled_test_repo.go`、`openai_codex_state_probe_scheduled.go`、`frontend/src/composables/useAccountAutoBPS.ts`。源提交归属 ranxi2001/akayedi 上游作者；本轮没有导入其代码，不能把源 SHA 写成本地实现来源。

## 后续同步规则

已更新项目同步说明与安装的 `sync-upstream/SKILL.md`、`references/excel-bps.md`：官方 upstream 仍普通 merge；BPS 只审查 ranxi2001 OpenAI OAuth 方案与严格必要依赖；不得重新引入 sub4api 独立平台。最新审查 tip 不等于已合入 tip，质量规则增量继续标为未导入。

## 验证

- PostgreSQL 18.1 / Redis 8.4 临时测试容器：254 迁移执行两次，验证仅旧平台停用、outbox 幂等、旧凭据/历史保留、OAuth BPS/RPM/打票状态不变；历史 253 迁移 ledger 和 OAuth BPS 仓储相关测试通过。未触达生产数据库。
- 前端：364 文件、2918 用例通过；类型检查、ESLint、生产构建通过。
- 公共后端：全量 unit 测试与 vet 通过；BPS、OAuth RPM、旧平台保护的 race 检查通过；带 embed 的本机二进制构建通过。
- 新增测试覆盖旧账号拒绝调度/创建/转发、旧分组禁止重启用/转换、Composite 不回退到原生协议，以及实际注册的普通/别名路由本地拒绝。
- 繁体生成检查通过；历史迁移、保留的 basispoints 协议包和原有 13 个草稿文件在提交前完成差异/完整性核对。品牌传播与工作现场兼容验证日志保存在本机 `.release/bps-ranxi-only-20260928/`。
- 未用真实 BPS/OpenAI 账号做上游效果验收；没有声称源质量规则已经导入或实测。
