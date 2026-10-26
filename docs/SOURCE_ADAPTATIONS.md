# 来源、适配与证据索引

这是现行来源说明，核查日期 2026-10-26。需求和技术设计唯一归属 [工程基线](ENGINEERING.md)；本页记录来源、已采用语义和历史证据，不再保存多套按日期分叉的实施计划。本轮在2026-10-26重新获取五源所需分支并固定下列SHA；官方与New API无新增，选择性补齐浏览器会话刷新、菜单字符校验、移动端监控布局和错误等待提示。

## 1. 已记录的最近五源基点

下表为本轮联网固定的输入，仅表示所述分支和主题核查范围，不表示五个仓库的全部历史均已审查。

| 来源 | 已记录分支 | 固定 SHA | 在本工程中的角色 |
|---|---|---|---|
| Wei-Shaw/sub2api | main | `ba81c7fd758e425d67f856d601147ef0c5263d3f` | 完整官方父基线 |
| ranxi2001/sub2api | production | `973d4bbd0064121a5533e0d3232ece6043f0d5fc` | 选择性适配，BPS 等已批准范围 |
| QuantumNous/new-api | main | `34003e9c2eed2c20e617f583fdfe54bf1bf42237` | 网关语义/可靠性等按本地架构采用 |
| zyycn/codex-proxy-rs | main | `1db8b0e319431717611e3e30ae1a2d3b371de86a` | 协议、恢复与观测机制参考 |
| dofastted/vm2api | main | `6925be7c4790048b5b5109cdf425cf16ae47742b` | 已批准提示词、遥测、缓存/搜索等机制 |

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

**上轮官方与选择性修复。** 官方`ba81c7fd`完整引入默认关闭的分组`image_count_billing`、图片端点标记及响应实际张数计费。New API `9823ebc7e`的nested缓存写入别名由共享apicompat owner融合，HTTP/SSE/WS及Chat/Responses/Anthropic转换取最大值去重，保留旧零值优先语义；`bc795cc11`在本地实际Chat→Anthropic入口补齐stop序列。`4d962e464`对应数字原文已由RawMessage覆盖并增加回归；`0165fef98`的Gemini入站→Chat请求入口在本地不存在，不将现有反向响应测试当作该入口已移植。来源的动态表达式UI、自动质量对账、IP黑名单产品及CPR插件UI未导入。

**此前已记录的选择性修复。** vm2api `2f9a62fec5cae134364692910d580eceb25f3de7` 提示缓存创建总量在 message_delta 增长但明细未重发的情形；本地沿已有总量保护补 TTL override，未照搬扩大矛盾明细的算法。`4732e71d` 的搜索历史启发被条件化为“未声明原生 web_search 时转换已知搜索块”；保留可读文本，不承诺保留原加密引用。ranxi 批量资金持有方案在固定 `cc9e57a933d3aa08b6de62fc4672435c5aaa2d2d` 范围内按本地幂等账本实现；普通额度双投影、延迟重置截止和新增 TypeSafe 产品策略未被等效宣称。

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

本轮按实际影响验证图片计费/分组缓存/权限、New API协议、隔离PostgreSQL迁移与ORM列读写、现有批量资金隔离和前端分组组件/语言键/类型；稳定计划与真实回执保存在本次交付证据目录。未执行生产迁移、真实收费上游、线上验收或部署，不把替身或历史报告描述成真实上游通过。

## 6. 核查期间新增的已发布能力

Responses / WS 多路复用行为对照 james-6-23/codex2api PR #788，贡献者 @dundunge，固定 head c2d7b2ee7f6e8362500c5b337f213baa71a84055。原交付记录说明源树未发现根许可证，采用用户指定协议行为和本地架构独立编写，未复制来源代码；这不是将该仓库新增为默认同步来源。其本地/race/替身证据属于原实现阶段，未证明真实收费上游或生产耐久。本轮将已发布 35 个变化路径保留，并将 [请求/多流合同](ENGINEERING.md#responses-compat) 纳入统一工程基线。

原生续接缺口修复参考 Zyy 的 CPR 本地只读快照 `e7e5b659cd4a8c8a7be2028ed52f27cbf7a526a9`（Apache-2.0）的续接作用域、精确连接锚点和客户端完整历史恢复机制，沿本地 Go 调度/连接池/整轮预算独立实现；未复制 Rust 执行引擎，未更新 CPR 检出，也未推进整仓来源核查水位。本地保留已确认完整历史的有限恢复，并额外修复网关合成增量的 missing-parent 全量恢复；BPS 快照、Cloud 生命周期和账务语义保持各自现行归属。

Codex 出站 zstd 保留此前参考 CPR e7e5b659cd4a8c8a7be2028ed52f27cbf7a526a9 client_sse.rs 的请求内同步、无固定并发槽等待语义。2026-10-25 根据本地 Go 实测，改为 sync.Pool 复用独占编码器：空闲复用、未命中立即创建，不恢复四槽队列或单个共享编码器内部排队；保留线路范围、取消、正文所有权和重放。Go level 3 为相近级别映射，未复刻 Rust 压缩字节；该生命周期调整是 Go 本地资源优化，不能据此宣称与 CPR 性能相同。入站预算及窗口保护按本地路由合同和 RFC 9659 独立实现，详见 [技术方案](ENGINEERING.md#request-compression)。本轮未访问或修改 CPR 检出、未推进来源核查水位。

2026-10-26 分别使用既有有效授权实测 BPS HTTP、原生 WS 与 Cloud WS，结论及共享编码器边界见 [请求压缩](ENGINEERING.md#request-compression)。这是本项目协议能力核验与回归补强，未移植新的 CPR 代码、未访问其检出，也未推进来源水位。公开 OpenAI Responses API 文档仅作为 zstd 与 DEFLATE 格式边界的参考，不作为私有 BPS/Cloud 能力证明。

2026-10-26 Cloud 生命周期补强参考 Zyy 的 CPR `1db8b0e319431717611e3e30ae1a2d3b371de86a`（Apache-2.0）OpenAI `transport/websocket/pump.rs` 的有序数据/终止通知，以及 `gateway-plugin/runtime/src/rpc/calls.rs` 的副作用撤销原则；插件子模块核对 `a02c7eacd173119165105f6e5d52220155bd4cb3`。公开快照未发现等价的 Codex Cloud App Server；宿主/插件 stdio RPC 不直接移植为 Cloud WS 协议。本地独立实现关闭排空、已确认回包保留、工具交接栅栏与 ID 语义去重；保留账务/owner/无重放边界。原 CPR 检出未修改，不推进整仓 reviewed 水位；仅记录上述局部核查，详见 [复核](cloud-parser/review-20261026.md#cpr-生命周期对照补强)。

## 7. 2026-10-26 五源增量

- ranxi `3612ec450` / `76b40f3e5`（danvilig）：主动刷新完成后只采纳当前会话已由 coordinator 持久化的 token pair；本地增加 generation/user/token 一致性保护，保留原 sid 请求隔离与迟到注销保护。`c094663df` 的 Unicode 校验只用于已有菜单 label；`6ca0627d0`（Keegan）修复手机端监控标题粘顶。新增多语言菜单字段尚待决定；本地已无 MonitorStatusCards，不恢复该来源卡片。
- CPR `a7f95c3a783ca514810bb6e051ff90cf398973fa`（Zyy，Apache-2.0）：安全保留投影到 SSE / WS 错误里的 Retry-After，局部错误优先、仅传验证后的等待值，不复制其他上游头；重复大小写头仍按本地策略拒绝。其私有区域/账号路由配置、插件生命周期和栈依赖未导入。目录缺失模型警告仍需按本地多平台、自定义映射及部分目录明确范围，未宣称等效覆盖。
- vm2api 固定 SHA 仅表示可用来源。303 提示词、调度状态、组织禁用、窗口统计和遥测适配与其他工作树的未交付任务重叠，本轮保护其现场、不列为已发布。后续 Codex park 清理对应本地不存在的 `codex_limited_until`；本地清除错误入口已清账号/模型/临时冷却和运行阻断。kin-egress、VM 池、host kernel、ARM64 安装修复不导入本地 Go 网关。旧条件搜索/TTL/遥测契约不变。
- 五源未变部分复用上轮具体证据；LICENSE/NOTICE 无增量，vm2api 沿用已有授权。范围、未采用/待决策与验证证据保存在 `.release/upstream-sync/sync-five-20261026/`，不把本表当作全仓历史已采用水位。
