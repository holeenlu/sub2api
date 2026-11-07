# 来源、适配与证据索引

这是现行来源说明，核查日期 2026-11-07。需求和技术设计归属 [工程基线](ENGINEERING.md)。下表是本轮实际冻结版本；选择性来源的冻结版本表示可用性与审查范围，不表示整仓合入，也不把延期主题的水位提升为“完成”。历史清单仍在 Git 和本地 `.release/upstream-sync/` 中保留。

## 1. 本轮五源冻结与取舍

2026-11-07 后续批次从已交付 NewAPI `07bdc79af3858b646779423ebe0da942e28863f7` 开始，以 `6b4b37db43443b5e88e424fe3c69602cc3810dfb` 重建唯一官方父提交；不沿用旧定制父链。冻结版本是观察/审查边界，不代表选择性来源整仓导入，也不把延期项改记为完成。

| 来源/ref | 上次观察 → 本轮冻结 | 提交/变化路径 | 本轮处理 |
| --- | --- | --- | --- |
| Wei-Shaw/sub2api main | `0eee80ba87aa4f3aff7282bf1695aa6dc0471c36` → `6b4b37db43443b5e88e424fe3c69602cc3810dfb` | 52/135 | 采用适用的后端协议、授权、图片、配额和计费修复；请求体/TTFT 与本地更完整合同融合。前端交 owner；广告、旧目录及版本发布身份不导入 |
| ranxi2001/sub2api production | `bde8f910f574ce7e1e5893a9bdcac1c7c455bc49` → `7eb2e271acdc4b36d62f1b542b16b5abb5b1bb09` | 13/32 | 采用普通正文/元数据扫描的快速排除，保留转义键解析；已删除的 encrypted sticky 不恢复。利润运营、AI 运维不引入；前端模型/分组联动继续交接 |
| QuantumNous/new-api main | `8c8940c999c6c38031dacdd0139cf7b0e1c38308` → `a3fd673bb7897691b1041aaf8d78bab6cc15e1d5` | 4/12 | 采用数据库当前状态保护手动停用；多 Key 渠道及 SQLite 不适用。DeepSeek reasoning 占位修复列为实际未覆盖候选，不声称已有等价实现 |
| zyycn/codex-proxy-rs main | `0d25061ef0830b127a6363a5ceaa6b8fb7ab99eb` → `f772bda57b62842e7bfca0d385d9fd6099a168b3` | 19/84 | 采用 Codex OAuth 已拒绝选项清理、Codex picker 已知音频过滤；独立队列/失败换号仍延期，未导入 pending OAuth 存储、迁移合并、自更新或插件/UI 栈 |
| dofastted/vm2api main | `011e0c6d2a81397556c614dc2f6f028aa5490058` → `f173bd5916455035c26393cab1adf4cf677026c5` | 18/54 | 已具备最终 wire 四个缓存断点限制；Key 自助查询、租户统计/权限导航、用户并发 UI、JEV 多端点控制台分别评估并延期/交 owner，VM/Node 产品和可逆脱敏不导入 |

### 行为冲突、落点与未完成边界

| 主题及来源 | 最终处理、实际消费者及验证 |
| --- | --- |
| 请求体超时/诊断（官方 `3425ba36`、`22693fb4`、`f10a45c0`） | 保留 NewAPI `httputil/body.go` + `body_read_diagnostics.go` 的单一读取/计重所有者与统一 413/408/499/503；删除候选中新带入的另一套 `body_read_error.go` 与 middleware failure 分类，不保留平行解释器。官方认证断开分类及非 UTF-8 Key 提前拒绝融合到原 middleware / APIKeyService，保留供应商 Key 禁止网关使用的 defer。HTTP/1 读超时先于 context canceled 的本地优先级不丢失。 |
| 413 透传（官方 `fa6f0651`） | `OpenAIGatewayHandler.handleFailoverExhausted` 在默认 body-limit 413 前执行显式规则；沿原总预算耗尽后收尾，规则不新增重试。单一 `applyOpenAIFailoverPassthroughRule` 替代原内联规则，覆盖 JSON、已启动 SSE、无命中及 skip-monitoring。 |
| Responses 完成态/推理（官方 `2d6b64a1`、`30ec2ecc`） | `apicompat` 对 message 完成态复用同一 wire 编码，保留空 summary、annotations/logprobs/refusal 与工具 call 结构；Chat/Messages 缓冲转换在 summary 为空时读 reasoning_text。保留本地语义 TTFT；官方每轮 WS 用例移入现有 relay suite。 |
| Terra（官方 `bea67f41`、`e73f4c5e`、`64e6c260`） | descriptor/instructions、API effort 校验、`catalog` 归一化、Anthropic 兼容 effort、工具要求 Responses、缓存标识、显式缓存写价与订阅长上下文豁免融合。旧 service 归一化函数不恢复；测试改用现有 catalog owner。Codex descriptor 与 API none 的差别保留，不改变当前默认导出模型。 |
| GPT Image 2.6（官方 `7f2876de`、`89012012`） | 沿既有 images generations/edits、JSON/multipart 原生链路接 complexity、帧、SVG、markup 选项及校验；不默默降级到不支持选项的 Responses。复用原下载出口、body 预算、审核输入、价格与一次结算 owner；账号测试不再发送被上游拒绝的 response_format。 |
| Gemini/Antigravity（官方 `aae31c74`、`0b535b5c`、`c3541803`、`2ce1849b`） | mapped model 传给原 Gemini schema converter，能力支持时工具媒体保留在 functionResponse.parts，其他模型走原回退。responseJsonSchema 与已有 schema 的优先级明确；stream response 只解包一次，删除候选中自动合并出的重复 envelope 分支。图片 INTERNAL 500 只冷却该模型；文本继续原分层惩罚，增加四个环境配置。 |
| 配额/连接测试（官方 `bf4e16b9`、`45575140`、`a8d90a72`、`0ee6b6c5`） | AntigravityQuotaFetcher 增加真实分组 5h/weekly（含独立 Argon），UsageInfo 新增可选 antigravity_quota_groups；展示交 owner。Anthropic setup-token 继续现有持久化 5h/7d/Fable 投影；429 已在入口采样，删除自动合并出的第二次写入。非 Anthropic setup-token 仍拒绝无依据的 usage 估算，测试按该合同验证。连接测试发送管理员原 prompt、默认 hi，并允许 256 输出 token。 |
| Cloudflare Messages/强制模式探针（官方 `21060815`、`c476edbf`） | 统一 URL 构造供正式转发/透传/连接测试消费，Cloudflare REST 不附 beta 查询；强制 Responses/Chat 模式不做额外能力探针，原 adaptive 探针继续。 |
| WS HTTP 桥接指纹（官方 `40d9c096`） | Native 桥接按现有 caller+credential 规则生成一份 IDs，body 与请求头共用；BPS/Cloud 保持自己的 owner。保留现有 turn-ID 账号隔离、规范 session-id 与嵌入 installation_id，不恢复官方旧 session_id/独立安装头格式。现有 bound Native fixture 验证稳定会话与 off 模式。 |
| 手动停用（New API `ada15c05`） | `accountRepository.SetError` 原子条件不覆盖 disabled；`ClearError` 仅清当前 error。删除仅 vendor 受保护而自有账号绕过的分支，缓存照常从数据库重读。测试用真实 Ent PostgreSQL SQL 生成器+sqlmock 验证写入条件；不是实际 PostgreSQL 验收。`433830b4` 的多 Key 渠道无对应数据模型，不复制。 |
| 会话性能（ranxi `bdb619ee`、`fd1f13b0`、`bd9aef40`） | `codex.ParseClientMetadata` 与显式 prompt_cache_key 入口无相关字面键且无 Unicode 转义时跳过解析；转义键、嵌套边界、header 优先级由原解析器/测试保持。来源 session_id 已拆到不同 owner，不复制旧 service 解释器；独立 encrypted sticky 路径已删除，不恢复。 |
| OAuth options（CPR `55526973`） | 复用 map/raw OAuth compatibility 路径；清理已拒绝的 top_p、penalty、metadata 等字段及 include_usage，仅省略空 stop/cache 和 disabled truncation。原生 max_output_tokens 优先，Chat aliases 仅补缺；保留有效截断/停止/缓存意图。raw 只解码选项，不整体解码工具历史，数字使用 UseNumber。同步清理 transform/strict/passthrough 的旧无条件删除列表，实际出站验证有效截断/停止/输出上限仍在，不仅测试辅助函数。API Key、未选定 OAuth 的请求不扩大清理。 |
| Codex picker（CPR `db779a4a`） | 现有 isCodexDedicatedMediaModel 增补已知音频/实时模型，已覆盖图片过滤继续；未知模型与普通 API 列表不改，不凭名称前缀隐藏未知模型。沿现有 alias target 检查，不引入第二套目录。 |
| CPR 其他增量 | `ca5901ba` 的 pending OAuth proxy capacity 针对来源 PostgreSQL document，不直接复制；本地 pending/当前 proxy 资格仍由自己的授权路径负责，未声称新增字段已覆盖。`750dd322` 为尚未引入的排队失败换号增加迁移日志，连同独立排队和绑定迁移整体延期；归并历史迁移、发布渠道/Rust/UI/插件 bridge 改动排除。 |
| New API 其他增量 | `9c2c370a` SQLite WAL/busy_timeout/txlock 不适用于本地 PostgreSQL。`a3fd673b` 在官方 DeepSeek 无状态 Responses、有效 thinking 和工具轮中补空白 reasoning 占位，并移动 reasoning 到前面；本地 normalizeDeepSeekResponsesRequestBody 尚未执行该变换。该项会改变历史结构，须核对 reasoning 保真、最终参数覆盖及恢复契约后整体验证，明确延期；不是“已等效覆盖”。 |
| vm2api 缓存（`0a62a5b8`） | 原 gateway_claude_oauth_body.enforceCacheControlLimit 在最终 system/tools/messages 形状上统一限四个断点（含顶层自动缓存），超限按原优先级裁剪，并统一 TTL 顺序；无 CLI Node persona 旁路可额外补两个。复用现有断点/TTL 测试，无重复补丁。 |
| vm2api 其他增量 | Key 自助 /v1/me/quota/usage 是额外公共协议，现有 /v1/usage 并不等价，延期。tenant 统计/排序、角色导航、用户并发 UI 交 owner 核对本地 vendor/用户权限，VM/guest 自有产品不导入。JEV 多端点（含 keyless 与密钥管理）、可逆脱敏及规则 UI 需要独立合同，仍延期；不把遥测隐私等同正文脱敏。 |
| 官方前端/广告 | 所有 frontend 路径与 `07bdc79a` 的完整树相同；新增配额显示、模型预设/导出配置交前端 owner，未挪回退休组件/types/utils 路径。官方赞助资产及 README sponsor 变更排除。backend VERSION 保留项目手动发布身份，基线升级不等于发版。 |

### 验证与交付限制

- `go build ./internal/...` 已通过；受影响包、服务与 handler/middleware 定向用例通过，race 与最终记录见本轮回执。
- 试跑较宽 Antigravity 用例时，`TestAntigravitySystemIdentityMultipleBlocksForward` 的历史身份期望失败；在未修改的 `07bdc79a` 主目录上单独复现同一失败。没有改写该测试来伪造全绿。历史 repository fixture 的旧接口阻塞仍在，repository 本次只编译当前生产文件和两份现有夹具来运行原子状态保护用例，未宣称全仓测试通过。
- 无 frontend 变更/构建/浏览器结果；本机 Docker socket 不存在，Docker/实际 PostgreSQL/Redis 未执行；无真实收费上游、生产部署、数据迁移或发布。
- 新公共入口 0，新增环境配置 4，数据库迁移 0；既有 images/usage 的字段扩展见完整 Release Note。当前有效 frontend、规则、许可证及迁移继续保留。
- 恢复 bundle、最终树、唯一官方父提交/一笔累计提交、精确旧 SHA lease 与收尾核验统一写入 `.release/upstream-sync/sync-five-20261107-followup/`，不使用已退役同步脚本。

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

## 9. 2026-11-02 五源增量审查

本轮实时冻结：官方 `d851a38477597157e78450d30af2d8504764d085`，ranxi `c760a11d4161f982b1d62776ecaeb1a8d461781f`，QuantumNous `d0abbf6e0d33552a41c402d9b1fadbaff27cf40`，Codex Proxy RS `892cba68aeb2c0599bd03f0b48acaeb619327238`，dofastted/vm2api `6ff6efd2af5a9819dcb9a6116f9ac5c2d68684f5`。

- 采用 CPR `c57af9b0` 的出站追踪字段清理，落点为现有 Go `normalizeCodexClientMetadata`；删除 traceparent/tracestate/sentry-trace/baggage 及 WS metadata 副本，保留业务轮次和账号隔离。
- ranxi 的抽奖、充值奖励、延迟观测和 Anthropic 新开关属于独立产品/前端/迁移；QuantumNous Vertex Gemini Omni 属于插件运行时；CPR Link v2/peer discovery 属于跨节点 Rust 合同；vm2api Fast 图片模型、别名和 OAuth egress 属于 VM 运行时，均未直接移植。
- 本轮仅改动 Codex metadata 投影、对应单测和同步文档；没有新增 API、配置、权限、迁移、依赖或部署入口。Go/gofmt 不可用，定向测试与格式化记为 `blocked`。

## 8. 2026-11-01 五源增量审查

本轮实时冻结：官方 `d851a38477597157e78450d30af2d8504764d085`，ranxi `3c88bd92e569bd697e8ee77b4315f17d4f24e3fb`，QuantumNous `ea343c4a81151bfd537d33b66527c25fa21a1112`，Codex Proxy RS `641f43b3030b1c68961e05d525e58ec6f4264a15`，dofastted/vm2api `eec6301cad8a6fcd3a077cb723fb997ade3b5333`。官方和 QuantumNous 相对上一批次无新增。

- 采用 ranxi `b6b29cabd` 的 usage 证据保护：空对象及非计费字段不标记用量观察，显式 token 零值保留；采用 `ea3ce7ebf` 的当前未完成 passthrough turn 单次收口语义，复用本地 relay 与 `completeOpenAIWSTurn` owner。
- ranxi `f6e0bf9d` 物理写边界准入、`4c17aeac` 响应头超时重放加收、`06e365413` Anthropic 模拟 Claude Code 和 `620d083a` 智能协议/运营扩展延期或排除，原因见 `.release/upstream-sync/sync-five-20261101/upstream-changes.md`。
- CPR `58a1f754..641f43b3` 的账号发现、身份累计、配额恢复和 CPR Link 跨节点共享仍依赖 Rust peer、迁移及新的跨节点权限/账务合同，未直接移植；vm2api `7056c1a4..eec6301` 的 Node 错误策略、官方 CC 开关、OAuth 控制台和页面变更没有同一 Go/Vue 消费者，未移植。
- 本轮仅改动 WS relay、适配器及测试；没有新增 API、配置、权限、数据库迁移、依赖或部署入口。Go/gofmt 不可用，定向测试与格式化记为 `blocked`。

## 7. 2026-10-31 五源增量审查

本轮冻结的官方 `main` 是 `d851a38477597157e78450d30af2d8504764d085`（2026-10-30 10:44:47 +08:00），相对当前 NewAPI `a25905ea` 的唯一父提交没有未合入的官方提交。上一批次的四源审查见 `.release/upstream-sync/sync-five-20261031/upstream-changes.md`；本次跟进冻结的选择性来源为 ranxi `d14ed180`、QuantumNous/new-api `ea343c4a`、Codex Proxy RS `58a1f754`、dofastted/vm2api `7056c1a`，完整增量见 `.release/upstream-sync/sync-five-20261031-followup/upstream-changes.md`。

- ranxi production 的 BPS/凭据 fence、队列/首字、图片计费、Gemini 图片、multipart 安全和 Claude UTF-16 指纹增量中，当前 NewAPI 已有对应的凭据代际与归属、整轮预算、批次幂等账本、图片 32 MiB `Limit+1` 拒绝、以及 `gateway_billing_block.go` 的 UTF-16 指纹实现。来源的新默认图片上限、自动运营和额外策略不改变本地合同。
- QuantumNous/new-api 的 Relay/插件/Bedrock/目录和依赖增量中，精确 `[DONE]`、Bedrock 字段清理、usage/取消收口、能力作用域和 ETag 已由本地 Go owner 覆盖；插件运行时、RelayKit、React/Web 和独立依赖栈不导入。
- Codex Proxy RS 的 Retry-After、WS keepalive、heartbeat/interrupt、连接亲和和目录能力增量已有本地协议/恢复合同；Codex 166/Guardian 与可配置错误保护需要真实客户端/上游和独立产品决定，延期。
- dofastted/vm2api 的 Node/VM/guest、303/310 persona、SeatPlanner、独立遥测和 OAuth 控制台不属于 NewAPI Go 网关；本地保留已确认的遥测隐私、搜索历史和 TTL 边界，不复制平行运行时。

跟进批次采用了 QuantumNous `ea343c4a` 的已交付图片流入账语义，落点为 `backend/internal/handler/openai_images.go`，并补充部分图片流失败测试；没有新增 API、配置、迁移、权限或部署路径。Kling 插件计费、CPR Link 跨节点共享、CPR Rust/UI 变更和 CPR 前端依赖升级分别排除、当前不适用或延期至前端 owner；来源 LICENSE/NOTICE 未出现需要新增的适用材料，既有 vm2api 授权与署名继续保留。
