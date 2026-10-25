# 来源、适配与证据索引

这是现行来源说明，核查日期 2026-10-25。需求和技术设计唯一归属 [工程基线](ENGINEERING.md)；本页记录来源、已采用语义和历史证据，不再保存多套按日期分叉的实施计划。本轮为文档整理，未抓取或整合新的外部来源。

## 1. 已记录的最近五源基点

下表来自文档整理输入中已提交的 2026-10-25 来源核查。它表示当时固定版本，不表示本轮重新联网确认了这些项目的最新 HEAD，也不表示五个仓库的全部历史均已审查。

| 来源 | 已记录分支 | 固定 SHA | 在本工程中的角色 |
|---|---|---|---|
| Wei-Shaw/sub2api | main | `37067e8e340e9440d173a92b4faa05e8ce58368f` | 完整官方父基线 |
| ranxi2001/sub2api | production | `9fcfe25d600a331abcc28a1788e597e5542d4cd3` | 选择性适配，BPS 等已批准范围 |
| QuantumNous/new-api | main | `e835b1bfc43b565d625537830dbfb19328925134` | 网关语义/可靠性等按本地架构采用 |
| zyycn/codex-proxy-rs | main | `84c0db26135dae694a5f5ac608ed6c04df049234` | 协议、恢复与观测机制参考 |
| dofastted/vm2api | main | `6f188e94e10ff6ce9239b4c39f964da4c98916d7` | 已批准提示词、遥测、缓存/搜索等机制 |

MACOS-DO/sub4api 不是默认增量同步来源；已有 ModelTrace/诊断及署名继续保留。openai/codex 用于明确记录的协议/压缩提示词参考。新增来源不授权改动独立 vm2api 仓库、引入 Rust/Node/VM/guest 平行网关或运行来源项目发布流程。

## 2. 当前采用范围与未采用边界

| 主题 | 已采用/现行归属 | 明确未采用或限制 |
|---|---|---|
| 官方基线 | 原生平台、网关、调度、计费、受限管理员 Key 等；本地角色/范围在 authz 融合 | 已退役优惠码等不因官方合并恢复 |
| BPS | 独立 Excel grant、工具/图片、HTTP/SSE/客户端 WS、有限恢复；[R09](ENGINEERING.md#req-bps) | 自动登录、票据/Cookie、Mihomo、自动质量运营不恢复 |
| New API | 请求体/错误、SSE、工具 Schema、用量与缓存、调度比较、Gemini imageConfig 等 | 不复制独立产品默认值、React/Bun 或第二套网关 |
| CPR | 请求准备、恢复/凭据、终态、观测及前端语言/设置机制按本地 owner 吸收 | Rust 插件/预算/组合中间件、UI 产品删除不直接照搬 |
| vm2api | 既有 Claude 提示词/六项能力、导入式账号遥测、可配置上传间隔、TTL 余量与条件搜索历史转换 | 完整 303 身份/模板、旧图主动删除/前缀写锚、第三方生图白名单和原值 turn ID 仍未采用 |
| Cloud | 独立 native-auth + Cloud RPC 实现；Codex 协议和本地压缩提示词参考 | 不继承 SIWC grant，不承诺与原生全语义/压缩等同 |
| 诊断 | 已吸收 ModelTrace 和原生计费探测 | 统计分类不是上游身份保证 |

**最后已记录的选择性修复。** vm2api `2f9a62fec5cae134364692910d580eceb25f3de7` 提示缓存创建总量在 message_delta 增长但明细未重发的情形；本地沿已有总量保护补 TTL override，未照搬扩大矛盾明细的算法。`4732e71d` 的搜索历史启发被条件化为“未声明原生 web_search 时转换已知搜索块”；保留可读文本，不承诺保留原加密引用。ranxi 批量资金持有方案在固定 `cc9e57a933d3aa08b6de62fc4672435c5aaa2d2d` 范围内按本地幂等账本实现；普通额度双投影、延迟重置截止和新增 TypeSafe 产品策略未被等效宣称。

这些来源事实由原核查正文和源代码注释支持；当前执行合同以工程基线和代码为准。来源项目后续删除某能力，不自动授权删除本地明确保留的功能。

## 3. 许可与署名材料

| 材料 | 保留用途 |
|---|---|
| [仓库 LICENSE](../LICENSE) | 当前仓库许可原文 |
| [BPS NOTICE](../backend/internal/service/basispoints/NOTICE.md) | ranxi/BPS 来源及适用声明 |
| [vm2api LICENSE](../backend/internal/pkg/claude/prompts/LICENSE.vm2api) / [NOTICE](../backend/internal/pkg/claude/prompts/NOTICE) | 原始限制、来源及适配声明 |
| [ModelTrace license](../backend/internal/service/modeltrace_LICENSE) | 诊断来源声明 |
| [Inter license](../frontend/public/fonts/inter/LICENSE.txt) | 字体许可原文 |
| [提示词审计源冻结清单](../openspec/changes/add-openai-compatible-prompt-audit/source-freeze/MANIFEST.md) | 原 patch、未跟踪源码归档及校验依据 |
| [历史作者/提交](customization/source-commits.json) | 单一定制历史整理之前的来源追踪 |

既有已提交来源记录记载：用户已确认 vm2api 书面授权覆盖本项目适配、三品牌分发和商用。本轮没有重新审核授权文件，不扩大授权范围，也不删除原 LICENSE/NOTICE；新增范围仍依实际授权和当前交付规范处理。改写语言不改变来源许可责任。

Cloud 压缩提示词注明 openai/codex `rust-v0.164.0-alpha.11` 的 `codex-rs/prompts/templates/compact/`；对应代码注释和原来源保留。历史 SHA 短写仅按原记录引用，不补造未核实的完整 SHA。

## 4. 原文和逐文件证据如何找

- [文档去向表](customization/documentation-audit.tsv) 记录原路径、原 blob、核查输入、保留/合并去向和审查范围。
- [原文来源提取表](customization/documentation-sources.tsv) 保存原方案中的完整 SHA 与 GitHub 来源 URL。它是文本提取索引：包含本地、上游、测试和历史 SHA，不能将每个值都当作“已采用上游提交”。
- [原始 provenance](customization/provenance.json)、日期版目录内保留的 source-commits/provenance/TSV、测试摘要/日志和冻结 patch 仍是其当时的不可变记录。里面的旧文档路径按去向表定位；本轮不重写历史作者、结果或旧测试数字。
- 被合并的 Markdown 原文与删除前树保存在文档核查输入及本次外部 Git bundle；交付记录包含 bundle 路径和校验。文件回退与生产数据回滚分别处理。

## 5. 证据使用限制

本地函数测试、真实 Redis/PostgreSQL、真实客户端到替身、真实上游探测和指定生产版本验收属于不同层级。比如 Cloud 历史真实 host 探测使用内存会话/结算存储时，不能证明生产 SQL 扣款和线上部署；遥测 oracle 对比不能证明 Anthropic 实际接收或风控收益。

本次仅重新核对源码、文档、静态注册、链接、矩阵与文件树。未执行业务全量、构建、收费上游探测、部署、迁移或生产清理。后续按受影响输入复用有效证据，不把旧报告复制成新一轮验收。

## 6. 核查期间新增的已发布能力

Responses / WS 多路复用行为对照 james-6-23/codex2api PR #788，贡献者 @dundunge，固定 head c2d7b2ee7f6e8362500c5b337f213baa71a84055。原交付记录说明源树未发现根许可证，采用用户指定协议行为和本地架构独立编写，未复制来源代码；这不是将该仓库新增为默认同步来源。其本地/race/替身证据属于原实现阶段，未证明真实收费上游或生产耐久。本轮将已发布 35 个变化路径保留，并将 [请求/多流合同](ENGINEERING.md#responses-compat) 纳入统一工程基线。
