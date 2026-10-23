# New API 全网关更新吸收：实施与二次复核报告

## 2026-10-22 后续：Responses完成与取消边界

固定 `7d8b91268dab69d7150f345c448b5e250163146a..0c2af9780e45f6f9073528fb0fcbdcd64b5d9495`（Calcium-Ion，PR #7956）。保留前轮全网关评估证据，只复核受影响的生命周期。

本地已在完整终态后停止等待EOF，使用有界写入器传递Write/Flush错误，并先保留真实用量；未重复移植来源scanner或计费层。补验确认：normalized和passthrough都可能在最终成功Flush内收到取消后，仍将完整成功响应标为断连。

采用来源语义，融入现有`openAIHTTPUsageDrain`：仅完整`response.completed`/`response.done`边界已成功写入刷新、无失败终态和实际写错误时结束drain归因，后到取消不覆盖该结果。刷新前取消、FlushError、流中断及失败终态不变；用量继续保留，无新增重试、生成请求或收费。局部flush成功不等于客户端receipt/display。

扩展既有`gateway_http_stream_contract_test.go`，分别覆盖两条路径、两个成功终态别名、刷新前取消、刷新失败、流中断和失败终态；原实现成功终态取消4项失败已实测。无需前端设置、API、迁移或新执行入口，部署新程序后生效。本轮只提交/推送，不部署；最终验证见 `.release/upstream-sync/lifecycle-20261022/`。

## 2026-10-22 增量核查

固定 `206e0fa5c600127a314a97f88592c067f784f4a6..7d8b91268dab69d7150f345c448b5e250163146a`，沿用此前全网关及历史缺口评估，不重复重建未变化的功能。

- `eea05e010c7f212e1a617638fe27164f24d9478d`：将实际用于请求压缩和上游响应解压的 `github.com/klauspost/compress` 从本地1.18.2升至1.18.7；复用既有 `pkg/httputil` 和 `repository/http_upstream`，验证zstd/gzip及全后端单测。没有新压缩开关或额外执行路径。[依赖发布说明](https://github.com/klauspost/compress/releases/tag/v1.18.7)。
- `7d8b91268dab69d7150f345c448b5e250163146a`：来源升级Go1.26.10；本地模块/构建镜像已经要求Go1.27.3，覆盖对应修复，不降级。开发者调用本地SDK时仍须遵守模块工具链选择，不能把外层go启动器版本当作实际编译版本。
- `4d36f2c52`：来源React项目Vitest跨主版本升级与本地Vue测试栈不对应，不机械复制依赖主版本。前端文件树本轮不变。

本轮适配纳入完整定制提交，来源为 `QuantumNous/new-api`。无生产操作、数据库变更或新增配置；当前门禁证据位于 `.release/upstream-sync/selective-20261022/`。

## 2026-10-20 增量核查

固定来源 `058c8231b68c51ee02abee15befcd720fee9f4a2..603a5ceb55908cc24b509aca2caa6fc956c9958c`，本轮 3 个提交、33 文件；沿用既有全网关与历史缺口审查证据，不把增量核查称为重审全部历史。

- `25372e30b1f94255b1f818aeaaf006f042572580`：Claude 请求准备、首包等待与重试期间保活。复用本地原有互斥串行的 SSE 心跳实现，导出既有入口供 Messages handler 使用，在用户/账号队列交接及安全重试时续接。心跳与真实输出分开计数，业务头/写入前同步停拍，失败仍按真实状态在 Ops 记录；已提交 HTTP 200 时发送 Claude SSE error。沿用 `gateway.stream_keepalive_interval`，没有第二套定时器策略或新设置。原始 Anthropic 身份、计费和不安全重放禁令不变。
- `603a5ceb55908cc24b509aca2caa6fc956c9958c`：缓存 token 展示。后端 DashboardStats 的累计/今日 cache creation/read、聚合查询及用户首页已具备；管理员首页缺少展示，本轮直接在原统计项补缓存合计，使用原字段及 `dashboard.cache` 文案。不新增表、API、统计查询或缓存数推算。
- `32e587406c6ebe891f18ab9aeb2f4a1f6b7b9e14`：Resend 为新邮件供应商通道。本地 SMTP 已有连接/IO 超时；没有现行供应商切换需求或发送失败证据，本轮不新增凭据设置、不调用邮件服务。

适配归入已有恢复、用量及需求文档主题；最终组合门禁、竞态测试、实际生效树和三品牌 SHA 见 `.release/upstream-sync/20261020T175441-a1067638/upstream-changes.md`。本轮不发版、不部署，不将 mock 上游/本地 race 通过表述为真实生产性能收益。

## 2026-10-19 增量核查

固定 `6461f084ffcd1c16e3b31a26678584c888ac7f56..058c8231b68c51ee02abee15befcd720fee9f4a2`（3 提交、42 路径）。本轮没有新增 New API 移植代码：

- `058c8231b` 审计排序复合索引已由本地迁移 180 的 `(created_at DESC, id DESC)` 与 `audit_log_repo.go` 相同排序覆盖，无新迁移。
- `55bb6b903c63fcddbf71740d7267b653fe1dbd47` 修复通知小时桶及原子 TTL。本地无该小时桶；余额通知按阈值穿越，邮件按事件/来源/收件人/提醒去重，Ops 有自身冷却与 leader。未引入重复计数器，也不将此等同于全部通知并发幂等已审计。
- `6cf0d90b5ff27f901697dddd61e0c28aba8efefc` 的 DeepSeek V4.1/Flash 基础映射和 max 能力已有覆盖。新增 Search/Fetch 独立端点、工具估价和 `@modifier` 涉及新的产品及收费语义，未整体导入；不推算不存在的上游费用。相关协议可靠性仍纳入审查，DeepSeek 正文工具恢复按用户本轮选择保持 Wei-Shaw 行为。

任务开始：2026-10-13；验收完成：2026-10-14（Asia/Shanghai）。状态：实施及二次复核完成，目标验证通过；未暂存、未提交、未推送。

## 2026-10-17/18 增量：图片细节与首屏主题

固定 QuantumNous/new-api `09109ec315a5086366f94bcce00f177ec831dbe1..6461f084ffcd1c16e3b31a26678584c888ac7f56`。既有全网关缺口以 10 月 17 日号池审计第 15、17 节的最终调度/恢复实现为准；本报告下文旧“不采用”记录不覆盖后续已实施需求。

- `ff3684658fc1ab5747bda9a142910a617df8eb10`：扩展原 `ResponsesContentPart` 的可选 detail，Chat 图片保留 low/high/auto/original，缺失或未知值归 auto；Anthropic 普通图片及工具结果显式 auto。沿用原协议转换器及原测试，不新增转换层。
- `b0a98ac49ca138e6274dbc16dc99365a2f639929`：将原模块入口主题初始化移到同源 head 静态脚本，删除 main.ts 旧初始化；主站/供应商共用现有 theme 设置与系统偏好。外部脚本适配现有 CSP self，不放宽 CSP；禁止存储时仍能启动。这一个启动路径是为了在模块加载、首次绘制之前生效，原模块入口无法保证该时机。
- 通知增量已比较站点/用户开关与余额阈值穿越的现有实现；不宣称新增渠道订阅及语言选择全部等效。渠道拖拽、近样本卡片、任务价占位及 provider 图标分别与本地账号优先级/健康/成本、窗口统计、零价语义和平台身份比较，未照搬其产品数据模型或展示规则。详细逐项依据见本轮报告。
- 全量前端验证额外发现已有 Ops 调度诊断拆分后不再过滤空 `[]`；复用原 `meaningfulPayload` 消除空块，既有错误详情测试覆盖，不增加第二套过滤规则。此项是本地验收修复，不标为上游新增。

新增公共 HTTP 接口、配置项、数据库迁移均为 0；新增静态主题启动路径 1，网关主执行路径 0。变化归入原协议、站点、Ops 等功能提交。本轮证据位于 `.release/upstream-sync/20261017T232427-246ec216/`；不发版、不部署，不用单测代替浏览器、真实数据库或上游验证。

## 2026-10-16 增量：推理项必填摘要

参考 QuantumNous/new-api `ca2cae676db87333a583b6e104d0f1f7366f3258`（#7833，`relaykit/dto/openai_response.go`）。本地流式宣告已由 `responsesItemWire` 和 `TestWire_ReasoningItemSummaryAlwaysArray` 等效覆盖；非流式 Anthropic 密文推理仍会因 `omitempty` 丢失空摘要。在既有 `ResponsesOutput.MarshalJSON` 补 `summary: []`，保留密文、非空摘要和非推理项原有编码，扩展已有 wire/密文往返测试。不新增接口、配置或序列化层。此段是后续共享同步增量，下文状态及数据仍描述原验收时点；本轮门禁和交付证据见 `.release/upstream-sync/20261016T135907-95cc1d6a/`。

## 1. 范围与固定基线

2026-10-16 后续参考 `09109ec315a5086366f94bcce00f177ec831dbe1`（#7317）：在原 `OpsMetricsCollector` 中扣除 cgroup 非活跃文件缓存，CPU 分母取配额与进程可调度核心数的较小者。v1/v2、无限制/缺失/损坏数据及缓存瞬时超出用量由原指标测试扩展验证，数据不完整时整体回退宿主机，仍不混合容器使用量与宿主机总量。CPU/memory stat 复用一个解析函数，清理旧重复解析；未移植其额外采样服务、全局503哨兵、连续计数及配置。新增HTTP接口/配置/执行流程为0，既有Ops页面、评分和告警消费修正后的新采样，历史样本不重写。

同轮 `d032204733db31208f10bac118b6e683aab3bf29` 的 AWS Mantle 已比较原 Bedrock InvokeModel 签名/事件流与 Anthropic API Key 的 base_url/显式模型映射。IAM Mantle 的新签名服务/传输模式尚未纳入，不能直接替换原 AWS 账号；现有 Anthropic API Key 路径仅构造行为接近，未实测真实 Mantle。不添加未经需求确认的模式字段或双网关，也不声称已有完整支持。本轮选择性吸收与三品牌验收见 `.release/upstream-sync/20261016T223509-14c4f039/`，下文保留原验收时点事实。

- 实施计划：[new-api-gateway-adoption-plan-2026-10-13.md](new-api-gateway-adoption-plan-2026-10-13.md)。本报告记录实施结果，计划第 6 节仍保留当时的研究事实。
- New API 参考源码固定为 `ef60ef27f36e418143fa4abf0c9025ba6f17fba7`；不是整仓合并，不引入第二套网关、调度或计费系统。
- 本地基线/当前 HEAD：`49b29ef158e6319cfd6b1227781307de5f8f870e`；工作分支 `codex/new-api-gateway-hardening-20261013`。
- 归属：KDAN / TapModels / Tokensavy 共享能力。按本次指令仅保留工作区改动，不传播品牌分支、不发版、不部署、不运行生产迁移。
- 开始时已有的 `AGENTS.md` 和 `docs/CHANGE_DELIVERY.md` 改动保留，不计入本批业务/测试统计。

## 2. 逐项落地

| 项目 | 实施结果与主要落点 |
| --- | --- |
| G01 首次 body 失败 | `middleware/audit_log.go` 恢复已读取前缀并保留首次 typed error；后续读取不把截断伪装为成功 EOF；Close 仍由原 body 所有者负责，捕获上限不变。 |
| G02 入站错误 | Messages、CountTokens、OpenAI Messages/Chat/Responses、Gemini、Images 的失败/413 早返回统一调用既有安全分类日志；不进入上游、重试或账号处罚。 |
| G03 Schema | 依据实际传输选择方言：AI Studio v1beta / Vertex 服务账号用 raw JSON Schema；带 project 的 Code Assist OAuth / Antigravity 保留旧 typed-schema 适配。工具与输出共用能力判定；保留数值边界修正和 64 层深度限制。 |
| G04 结构化输出 | Chat response_format → Responses text.format → Anthropic 中间 output_config.format → 最终 Gemini/Claude 出站保持约束。原生 Claude 去除 OpenAI envelope 元数据；已知不支持的目标明确 400 并记录诊断；未知模型由上游决定。 |
| G05 beta | 全链聚合多行及大小写不同的 anthropic-beta header，拆分、去空白、去重；保留 override/mimic、Vertex/Bedrock 过滤、CountTokens 和 BPS 原策略。 |
| G06 assistant 尾部 | 仅源为 Responses、目标明确拒绝 prefill、尾部状态 completed 且内容为纯 output_text 时处理；工具/思考/签名/未知扩展、in_progress 及原生 Chat 来源不删除。基于最终映射模型，每次尝试重算，不改原输入。 |
| G07 流终止 | 共用 Gemini 终止观察器覆盖 Chat、Messages、Native、聚合及 Antigravity；usage-only、空 EOF、[DONE] 不充当 Gemini 完成标志。保留首个协议错误，处理末行无换行、n+err、完成后取消和生成中取消。 |
| G08 诊断/重试 | 结构性 code/path 诊断最多 32 项，单项 code≤64、path≤128，不记录 schema、工具参数、签名；成功日志和实际失败 attempt 均可按 request ID 追踪。原 FailoverState 增加实际决策原因/动作/计数，策略与预算不变。 |
| G09 查询/导出 | 新增用户/管理员两个入口，共用 ExportUsage、SQL 快照及 NDJSON writer。500 行批次、稳定 keyset、只读 repeatable-read、硬期限、单语句超时、跨节点每用户 Redis 租约；复用权限、成本脱敏、step-up 与审计。前端移除旧分页全量数组循环，取消/失败不生成成功文件。 |
| G10 故障用量 | 失败携带已观察到的 token/image/model/request ID，经既有 RecordUsage 和 DB 幂等结算；不按分片累加，不因图片模型名在失败时捏造图片。用量来源日志区分 upstream_reported 与 unknown，包括上游明确零；不引入估算、搜索新收费或新价格规则。 |
| G11 请求体资源 | 已完成固定条件 96 组前后测量。保留现有内存快路径，不新增磁盘缓存层或配置。测量说明和原始结果见第 6 节。 |

### 2.1 入口与转换归属

| 入站/目标 | 最终约束落点 |
| --- | --- |
| Chat → Anthropic | ChatCompletionsToResponses 设置内部来源标记；ResponsesToAnthropicRequest → 原有 Claude build；Chat 历史不因借道 Responses 被裁剪。 |
| Responses → Anthropic | 按最终模型处理安全 completed 尾部；保留格式到 native output_config.format；beta/body 同步继续由现有 build 处理。 |
| Chat/Messages → Gemini API Key/Vertex | 先保留中间原始 schema，再生成 parametersJsonSchema / responseJsonSchema，互斥旧字段不同时发送。 |
| Chat/Messages → Code Assist / Antigravity Gemini | 使用既有 typed-schema 清理并产生结构性诊断；不误把私有 API 当公共 raw-schema 协议。 |
| Responses → Antigravity | 根据映射目标决定尾部策略；Claude 私有传输不能兑现的 output format 明确拒绝；Gemini 走既有转换。 |
| 原生 Gemini/SSE、Gemini 经跨协议或非流聚合 | 统一验证终止；失败只返回已知事实，不能自动补成功尾帧；HTTP/SSE 错误保持目标协议。 |

### 2.2 已等效覆盖，保留原实现

Gorilla 1.5.3 / coder WebSocket 1.8.14 已满足本次版本对照，不做无效依赖升级。WS 正常退出既有 Close frame + CloseNow，异常清理保留原生实现；本次回归包含 BPS/WS/403 恢复与现有结算相关测试。保留可信 IP、图片出口、TLS、响应归属、客户端限制、品牌、迁移保护；未导入退役的 ranxi 运营/票据路径。

既有同帧思考+正文、正文+工具、custom/namespace、工具图片/签名、原生字段、模型差异和价格档位展示依靠现有跨包/前端回归验收，不重复造新实现。没有将“有 helper”当作链路完成依据：beta、schema、output format 另有最终 HTTP 请求断言。

## 3. 明确的行为变化与边界

1. 日志筛选列表、对应过滤统计和导出共用窗口：默认最近 90 天；显式旧日期可分窗口查询，不删除历史数据。超过配置窗口返回业务 400，不截断查询为伪完整结果。
2. 导出固定 `created_at DESC,id DESC` 顺序，保证快照游标稳定；列表自身排序仍可用。最大默认 100,000 行，超过后不发 complete；客户端拒绝保存半成品。
3. 配置 `usage_export.max_range_days=90`、`max_rows=100000`、`timeout_seconds=120`。租约为硬期限 +30 秒，持有者比较删除；进程消失后 Redis TTL 释放。DB 每条查询 10 秒超时。
4. 管理导出需要 `usage.read` + `usage.export`，复用敏感操作确认与审计；成本权限中途撤销会在下个批次前停止。用户接口固定自身 user_id。
5. 服务端内存仅持有当前批次；浏览器仍需要组装最终 CSV/XLSX 文件，内存并非常数。客户端设 8 MiB 行/批次解析限制、256 MiB 总传输限制，同时受服务端行数/时间限制。没有宣称流式 XLSX 编码。
6. 快照稳定的是 usage_logs 记录；关联用户/账号展示名称沿用既有 hydration，在导出中途改名可能反映新名称。不承诺关联元数据也来自同一数据库快照。
7. 私有 Gemini 方言仍可能损失其不能表达的约束，诊断会标明 legacy conversion；不宣称全部目标都支持 raw JSON Schema。
8. 图片计数保留现有每帧最大值策略，避免累计式媒体分片重复扣费；没有唯一图片身份的增量多图仍可能保守少计。本批不改变该既有计费口径。
9. 之前 Fable/Opus 的显式 thinking.disabled 与 adaptive-only 冲突没有被静默改为 low；这涉及用户关闭思考的语义和费用，并非本轮可靠性修复的授权取舍。

## 4. 二次复核发现与修复

最终 diff 从入站 → 请求重建 → 下游已提交边界 → 异步计费再走一次，补齐了以下边界：

- 流式错误 writer 标记 committed，避免 handler 再补第二个错误帧；取消后的不可写连接也不追加错误。Antigravity 原生 Gemini/Claude 收尾错误改为各协议的对象型错误结构，不再发送 error 字符串。
- Antigravity Responses 失败使用带 id/model/created_at/sequence_number 的 response.failed，禁止随后 Finalize 产生 response.completed。
- Antigravity Gemini 聚合失败实际写出 502 Google 错误体，避免只有 Go error、客户端得到空 200；仍带回已知 usage。
- Antigravity 转换器延后成功尾帧到通过流结束校验后。上游先报错再出现 STOP 不能把失败改成成功；末尾正常完成后传输取消仍保留已完成语义。
- 聚合路径逐事件观察媒体和返回模型，避免只看最后一个文本分片丢掉前面的已确认图片。
- completed assistant 带未知顶层扩展/签名时保留原输入，避免安全裁剪过度。
- 上游报零与未报告用量使用独立原子观察状态；扫描 goroutine 不持有或写回可复用的 Gin Context，日志仅含来源和数值事实。
- 管理导出每批仍检查权限；DTO 投影固定后，成本读权限撤销不能继续传出敏感列。

旧测试中把无 STOP/取消当作成功的样例已按新契约更新；API 合约的 2025 年历史样例改为显式日期窗口；CSV/XLSX 组件测试改为 NDJSON 批次回调并保持筛选/文件名快照、多语言列名及价格字段断言。没有删除失败断言来掩盖业务回归。

## 5. 验证记录

验证状态：最终后端完整 unit 全部通过（60 个有测试的包，service 231.163 秒）；build/vet、定向 race、真实测试容器集成和前端验证通过。增量 lint 0；取消报告限额后的全仓问题 194 条，与固定基线完全相同。

- Go 1.27.2；golangci-lint 2.14.0；Node 24.19.0。仅使用本机工具及测试容器。
- 后端最终全量单测：`go test -tags=unit ./... -count=1`。
- 构建与静态检查：`go build ./...`、`go vet -tags=unit ./...`、gofmt、`git diff --check`。
- 定向 race：service / handler / middleware / apicompat / antigravity 五包，包含新增终止、失败用量、导出、body、诊断和既有 Failover、BPS/WS 回归。不是全仓 race。
- PostgreSQL/Redis 真实测试容器：`go test -tags=integration ./internal/repository -run 'TestUsageExport|TestUsageBilling' -count=1`，通过。含 501 条同时间戳、导出中插入回溯时间记录、稳定 ID 顺序、超限/取消、跨服务租约及旧 owner 不能解锁、既有余额/订阅/账号用量事务去重。
- 增量 lint：`golangci-lint run --new-from-rev=HEAD`；默认报告限额下各显示 53 条；取消限额后工作区和固定基线各 194 条，按路径/诊断及重复数量比较完全一致；不将既有问题报告为全仓 lint 通过。
- 前端 8 文件 / 95 测试通过：NDJSON 消费器、用户/管理 UsageView、UsageTable、价格档位/思考档位、localeKeyCompleteness；typecheck、改动生产文件 ESLint、Vite build 通过。Vite 仍有既有大 chunk 提示。
- Wire 由 `go generate ./cmd/server` 实际重新生成，未只手改生成产物。

最终日志已归档在 [验证目录](new-api-gateway-validation-2026-10-13/README.md)，机器可读结果见 [validation-summary.json](new-api-gateway-validation-2026-10-13/validation-summary.json)。

本轮没有真实供应商付费调用、线上流量回放、生产部署或生产数据库操作；没有运行所有后端 integration 或全前端测试集合。不能据本地通过宣称线上镜像已更新。

## 6. 资源测量与采用决定

[96 组 CSV](new-api-gateway-validation-2026-10-13/body-resource-matrix.csv)、[JSON](new-api-gateway-validation-2026-10-13/body-resource-matrix.json)、[执行脚本](new-api-gateway-validation-2026-10-13/run-body-matrix.py)。基准源码：`backend/internal/service/gateway_body_resource_bench_test.go`。

固定矩阵：baseline/current × passthrough/conversion × text/image × 1/8/32 MiB × 并发 1/16 × 重试 0/1。每组新进程、GOMAXPROCS=16、`/usr/bin/time -l`、`-test.benchtime=1x`；B/op、allocs/op 为整个并发批次，requests/op 给出折算分母。

| 路径 | 基线峰值 RSS 上界 | 本批峰值 RSS 上界 | FD / goroutine 回落 |
| --- | ---: | ---: | --- |
| 透传 | 1120.9 MiB | 1107.2 MiB | 48 组两版本的 delta 均为 0 |
| 转换 | 3319.3 MiB | 2964.9 MiB | 48 组两版本的 delta 均为 0 |

local_first_frame_p50/p95 是进程内读取、重放、转换直至可写首帧的耗时，**不是含网络和模型生成的线上 TTFT**。每格单次测量、无置信区间；不能把这组 RSS 差异当作稳定性能提升百分比。测量版本的后续变动仅涉及流结果、导出和保真边界，未改基准所测的简单文本/图片 body 快路径。

决定：现有重放直接复用已读字节，未证明再引入磁盘缓存能给这组场景带来净收益，故不新增缓存层。完整 JSON 转换在 32 MiB×16 并发仍有明显内存峰值；本轮不以微基准替代线上容量评估，也不降低既有安全限制。导出资源边界是明确落地的优化；body 路径保留测量结果与可复跑基准。

## 7. 改动规模、接口与回退

| 类别 | 文件数 | 新增行 | 删除行 |
| --- | ---: | ---: | ---: |
| 业务代码（含集中配置类型） | 61 | 1616 | 316 |
| 测试与基准 | 18 | 1025 | 45 |
| 生成 DI / 部署配置示例 | 2 | 14 | 6 |

统计排除原有两份文档改动；包含本轮新增未跟踪源码/测试，避免仅 git diff 遗漏新增文件。详细 [文件摘要](new-api-gateway-validation-2026-10-13/changed-files.json) / [交付摘要](new-api-gateway-validation-2026-10-13/delivery-summary.json)。固定文件清单摘要：`fa60b9e4f64d261141a87fafa29e6764db186d3d77728397cb2d3c8aae4a86fe`。

- 新增业务 HTTP 入口 **2**：`GET /api/v1/usage/export`、`GET /api/v1/admin/usage/export`；共用 **1 条导出业务流程**。既有模型转发入口新增 **0**，新调度器/资金结算路径/磁盘缓存层均 **0**。
- 新增集中配置 **3** 项，见第 3 节；数据库表/列/迁移 **0**。
- 现有协议/诊断结构新增或补齐 **7 个顶层字段**：AnthropicRequest.Diagnostics、AnthropicOutputConfig.Format、ResponsesRequest.FromChatCompletions、ResponsesRequest.TargetRejectsAssistantPrefill、ResponsesInputItem.Status、GeminiResponse.PromptFeedback、OpsUpstreamErrorEvent.ConversionDiagnostics。其中内部控制标记不序列化到上游；诊断 code/path、promptFeedback.blockReason 为嵌套字段。其余为私有读写/服务状态，不新增用户配置。
- 新小模块用于共同的 schema 能力判断、流终止观察、有界诊断和导出生命周期；既有中间结构无法携带格式、普通分页无法控制跨页快照与跨节点租约，所以在原模块内补接口/字段，而不是并行造一套流程。
- 删除用户/管理员旧的全量分页导出执行循环；已有普通分页 API 继续用于列表，不删除列表查询能力。

回退可分三块：入站/beta；转换/流/诊断；导出/配置/前端。导出前端必须与端点一起回退，不留下旧全量分页替代路径作为双轨。当前无新增提交，因此报告仅提供逻辑回退边界，不伪造可回退 SHA。

本报告、计划和证据目录受现有 `docs/*` ignore 规则影响，仍是本地文件。将来用户授权提交时必须明确纳入所需文档；本轮不修改忽略规则、不强制暂存。

## 9. 2026-10-15 选择性同步补充

来源固定 `QuantumNous/new-api@718a4e7f9751c5bf1958747cd0c2c895e2b937d4`，比较上轮已审查的 `7de10f2c602b50122017556e8a09cb7c488c273c`；不是整分支合并。本地实施基点 `4a5c815bd7618973b73f22a1a996e3905a74c80b`。

- 吸收 `718a4e7f9` 的 Vertex `us/eu` 域名规则：把原 `geminiCachedContentVertexHost` 移为 `vertex_service_account.go` 内的 `vertexAPIHost`，现有 Gemini/Anthropic 生成、模型查询、显式缓存及批量图片共用；删除其他路径的重复拼接。保留验证、转义、Express 全局地址、普通区域及批量自定义 baseURL 优先级。Google 官方依据：[多区域端点](https://docs.cloud.google.com/gemini-enterprise-agent-platform/resources/locations)。这不保证所有模型或 API 在每一区域可用。
- 对照 `1b63c28dc` 保留 Claude 真零值与纯缓存计费目标，补齐本地 Anthropic → Responses 缓冲 JSON 的纯缓存增量同步：复用 `hasObservedTokens`，保持服务器聚合记录与客户端用量一致。上游缺失用量估算不采用，继续只结算实际计量。既有 `RecordUsage` 无普通 input/output 为零即丢弃缓存费用的门禁，无须新增计费判断。
- `a0bf6a8a4` 的 API Key project / Express 分流在本地既有 `buildVertexGeminiAccountURL`、账号 `project_id` 及 `setVertexGeminiAuth` 已覆盖，不复制字段；原生转发测试随本轮验证。
- 未引入 moejs 视频插件、独立任务计费、来源敏感词默认值或动态价格排序交互；分别属于缺少对应产品依赖、已有管理员安全策略及额外前端需求。此前 G01-G11 与未采用项的证据继续适用，不宣称重审全历史。
- 复现：新增到原测试文件的两项回归在修复前失败，表现为 `us/eu` 错误域名及纯缓存增量 200 在缓冲 JSON 中仍为 100；修复后定向回归通过。完整验证和三品牌远端 SHA 记于本轮 `.release/upstream-sync/20261015T004208-f8ed21f4/upstream-changes.md`。
- 用户路径：管理员 → 账号管理 → 创建/编辑 Vertex 账号 → 区域，使用既有 `credentials.location` / `credentials.vertex_model_locations`；批量图片使用既有 `batch_image.vertex_location` / `batch_image.vertex_batch_prediction_base_url`。纯缓存修复无需配置。部署新代码后生效，本次仅提交、推送。
- 规则归属和数量：移动并扩展一个现有 host 函数，清理旧名称和重复实现；新增业务接口、配置项、网关主执行路径、数据库迁移均为 0。

## 10. 2026-10-15 错误 URL 凭据脱敏

固定参考 `QuantumNous/new-api@19d435e7fdfdaa2b9a734a76393f112cf980b815`，从上次 `718a4e7f9751c5bf1958747cd0c2c895e2b937d4` 比较。本地没有来源旧版“所有域名打码”的行为，公共文档链接、点号模型 ID 和管理员域名/地址归属诊断继续保留；不新增渠道主机注册表或平行错误系统。

独立缺口是 URL userinfo、未知查询键、OAuth fragment 和 JSON 消息中的 URL 凭据。复用现有 `internal/util/logredact`，由一个内部导出 helper `RedactURLCredentials` 统一处理结构化字符串与普通文本，使用 net/url 解析；损坏 URL 安全隐藏，原引号和尾随标点保留。原 `sanitizeUpstreamErrorMessage` 复用该 helper 并保留无完整 URL 的敏感查询片段规则，避免直接使用授权日志的默认 `code` 脱敏而破坏业务错误码。

测试扩展原 logredact 文件，覆盖 HTTP/WS、JSON、损坏地址、userinfo、任意查询键、fragment、文档路径、点号模型、邮件和业务错误码；未引入新 HTTP 入口、配置或调度/结算流程。官方合并与该适配共同进行一次最终门禁，证据位于 `.release/upstream-sync/20261015T165315-d9d993be/`。旧 G01-G11 及既有缺口记录继续有效，本轮未穷尽重审全历史。

## 11. 2026-10-22 缓存明细与工具 Schema

来源范围 `QuantumNous/new-api`：`0c2af9780e45f6f9073528fb0fcbdcd64b5d9495..d0b840e75ff36d731b04ff981ee361819acd593f`。作者 CaIon；按行为参考在现有 Sub2API 实现中补齐，不整分支合并，不新增 RelayKit 或 JS 任务插件层。

- **吸收 `94cc3ff09`。** 经用户确认，`normalizeCacheCreationBreakdown` 对明确正总量的明细缺口按 5m 补齐，保留原超量比例截断和负数钳制；`computeCacheCreationCost` 删除重复零明细兜底。总量缺失仍只按实际明细，不新增用量估算，不重算历史记录。原计费测试扩展部分 TTL、整数边界及 Priority/长上下文倍率。
- **部分吸收 `d0b840e75`。** 本地 `apicompat/types.go` 已使用 RawMessage 携带工具参数、input_schema 和 response_format。本轮修复 `responses_to_anthropic_tool_schema.go` 根联合展开的 map 排序缺口：保留根和分支首次出现顺序、嵌套 RawMessage、特殊属性名及已有同名约束/required 合并。其余客户端工具 map 重写链路未在本轮穷尽审计，不声称整个来源提交完全等效。
- **`7e6596728` 不替换现有实现。** 来源按事件字节数累计写入预算；本地 `gateway_stream_writer.go` 以 32KiB 分块、每块刷新 30 秒写期限并传播取消/FlushError。两者不等价；当前选择限制单次阻塞而非按大帧累积数分钟期限，避免延长已停滞连接占用。随全量单测运行既有真实 HTTP/1、HTTP/2 慢消费者回归，但不宣称完成生产带宽或吞吐基准。
- **四项任务插件变更 `880211326/db80ac8e0/dbe97beb9/556517fa5` 当前不适用。** 来源属于模型抢占、JS 驱动返回计费 facts、原生插件 multipart 路由；本地插件是已选 OpenAI OAuth 账号的 gRPC 传输替换，入口为 `doOpenAIUpstream` → `RoundTripOpenAIOAuth`，响应解析和计费仍由核心承担。原生图片 multipart 已由 `openai_images.go` 解析并经过统一鉴权、调度，不存在对应 JS 任务路由或 task-expression 计费对象。不把客户端全部 header 转交给现有插件，也不移除本地上传限制。
- **`eb169e74d` 暂不引入多币种价格导入产品。** 来源增加逐模型的站点汇率乘除选择及表达式重写。本地模型价格使用现有定价服务；供应商采购的 CNY/USDT 账单换算是独立契约，不能用于转换模型价表。此轮没有改写价格币种或现有管理员设置。
- **`d8e65613c` 和 `e2321a5a9` 不移植。** 前者为来源使用记录的模型复制交互，后者为来源语言文件重排；非网关可靠性缺口，本轮不扩展前端功能或制造翻译重排。不是声称本地使用记录已有相同复制按钮。

用户路径：两项修复均无新页面、参数或配置，部署后沿原计费和协议转换入口生效。新增业务接口、配置、独立执行路径和迁移均为 0。测试证据与实际三品牌交付状态见 `.release/upstream-sync/selective-review-20261022/`，未部署、未执行生产数据变更。已有未评估缺口继续保留，不以本轮增量分类冒充全仓覆盖。
