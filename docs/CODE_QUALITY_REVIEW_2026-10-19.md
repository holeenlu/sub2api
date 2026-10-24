# Sub2API 代码质量复核与死代码清理（2026-10-19）

## 范围与结论

审查基线：本地 main `10a7baf5022eef2d6accc53fbf36422df17510be`；对照仓库已纳入的官方基线 `b0a02a330`，不代表重新获取或审计了官方最新主干。重点检查记账任务生命周期、持久性、资源边界、数据查询、依赖和遗留实现。本次不重复把已经修复的协议/缓存问题列为新问题。

“整个项目写得很烂”不能作为工程结论。当前代码已有事务去重、有界队列、响应大小限制、导出流控、备份文件上传、OAuth 日志脱敏等有效保护；问题主要集中在不同生命周期之间的边界和故障闭环。下面严格区分已复现错误、代码可确认的缺口、依赖配置的风险和需要压测的优化。

本次用户追加授权清理过期、无用、无效代码，因此实际删除了已确认无调用的实现。下列涉及计费规则、持久化架构、传输超时或依赖升级的行为变更仅给出方案，未混入死代码清理。

## 按优先级整理的发现

### R1 / P1：异步记账闭包仍读取原始 Gin Context（已定向复现）

- 代码：`backend/internal/handler/gateway_handler.go:636-654,1007-1024`；`openai_gateway_handler.go:1516-1531`；`content_moderation_helper.go:26-38`。
- 请求信息大多在入队前拍成快照，但 `ChannelUsageFields` 仍在 worker 执行时调用 `clientRequestedUsageFields(c, ...)`。它继续读取 `c.Request.Context()`；请求结束后原 Gin Context 可能归还池并用于别的请求。
- 复现使用生产 `UsageRecordWorkerPool`、生产提交包装器和相同取值函数：阻塞唯一 worker，将 A 的任务排队，再将该 Context 指向 B 的请求后释放 worker。断言期望 `model-A`，实际得到 `model-B`。探针通过 Go overlay 运行，没有修改项目测试文件。
- 已证明模型归属可串请求；当计费模型来源选择 requested 时，也可能污染计价模型。没有证据表明该路径会直接将 A 的扣款写到 B 的用户 ID，不能扩大结论。
- 改进：入队前生成不可变计费快照，包含 ChannelUsageFields；闭包不得保留 gin.Context、可变请求体或下一轮可重用的状态。增加“worker 延迟 + Context 复用”的入口级回归。

### R2 / P1：普通文本记账缺少持久投递与补偿闭环（代码路径确认）

- 代码：`service/usage_record_worker_pool.go:155-193,327-340`；`service/gateway_usage_billing.go:354-389,616-645,890-910`；`repository/usage_billing_repo.go:45-62`。
- 当前入队的是进程内函数闭包；数据库扣费失败时向调用方返回错误，handler 记录错误日志后结束。事务已提交而 usage_log 尚未写入之间也有独立故障窗口。
- 不是“完全没保护”：扣费有事务与请求 ID 去重，usage_log 有 best-effort 失败后的同步兜底，默认溢出策略为 sync。上述措施仍不能恢复进程被杀时尚未执行的闭包或持续数据库错误。
- 失败计费会保留 ActualCost=0 的用量记录，已有 `TestGatewayServiceRecordUsage_BillingErrorWritesUnsettledUsageLog` 明确验证此语义；本次在普通文本链未找到持久重试/对账消费者。批量图片已有独立的 billing retry，不能因此宣称普通文本也有。
- 改进：保留现有去重事务，增加可持久重放的计费事件及 pending/applied/failed 状态；将账务变更与日志/outbox 的产生关联到同一事务。资金事件不得沿用遥测的 drop/sample 策略。故障测试覆盖 worker 崩溃、DB 短断、提交结果不确定及重复投递。

### R3 / P1-P2：零费用混合了缺价、失败和明确免费的语义

- 缺价/计价错误：`service/gateway_usage_billing.go:1105-1122,1252-1266` 在所有价格兜底都失败后仍返回零成本。应区分“明确配置免费”与“价格不可解析”，在发往收费上游前确认计价资格；请求已执行则保存待定价状态，不能把未知自动解释为免费。
- Live：`service/openai_live.go:815-881` 只写零费用日志，绕过统一结算；`handler/openai_live.go:37-39,84-98` 又会做开关与计费准入。仅在允许 Live 时触发，不影响所有普通文本请求。
- Live 是否收费是产品规则，不能擅自按时长编一个价格；免费能力也应有明确标识和成本预算。源码已写明这项 TODO，不能声称“作者完全不知道”。

### R4 / P2：数据库变慢时，扩 worker 和同步兜底可能放大拥塞

- 代码：`service/usage_record_worker_pool.go:18-26,178-187,252-296`；`config/config.go` 的 database.max_open_conns 默认值。
- 默认 worker 128、最多 512，数据库连接默认 256；扩容依据主要是队列水位，未在该决策中考虑数据库等待或事务延迟。队列满后的 sync 在请求线程执行，越过池的并发边界。
- 这是可确认的退化机制，尚未测得本部署的瓶颈规模，不能声称当前线上已发生雪崩。
- 改进：现有池与同步兜底共享明确的 DB 工作预算；扩容决策纳入 DB 等待、任务年龄和事务 p95，保留必须结算的任务。先做降速数据库故障压测，再决定参数，不盲目增大队列或 worker。

### R5 / P2：可选配额 flusher 的领取/确认与版本边界仍有缺口

- 代码：`repository/billing_cache.go:575-594` 用 SPOP 先移除脏 key；`service/user_platform_quota_flusher.go:125-208` 再读快照并写 DB。
- SPOP 后进程崩溃、且该 key 后续不再更新时，dirty 标记无法通过下一次流量自行恢复；Redis TTL 过期前未完成持久化也需要处理。
- 同文件 178-188 行已说明旧快照覆盖管理员重置的竞态。配置默认关闭，因此这是启用该优化后的风险，不能泛化到默认部署。
- 改进：带租约的 pending/processing/ack 领取过程，以及配额 epoch/version 的条件更新。不能只加失败 re-add，因为进程崩溃时不会运行失败处理；也不能把 Redis 当前值当作永久审计账本。

### R6 / P2：请求体大小有界，但读取时间缺少应用层边界

- 代码：`server/http.go:112-133` 设置 ReadHeaderTimeout、IdleTimeout 和 MaxBytesHandler，明确没有设置 ReadTimeout。
- 大小限制不等于时间限制；服务直连，或前置代理没有有效限制慢请求体时，连接/请求资源可能长时间占用。本次未读取生产反代配置，不断言线上已可利用。
- 改进：分别设置请求头、请求体、上游首输出和流内空闲预算；上传体限制完成后清除该阶段 deadline。不能直接加一个短的全局 WriteTimeout 而破坏合法 SSE/WS 长连接。

### R7 / P2：用量列表的大页码与精确总数仍有优化空间

- 代码：`repository/usage_log_repo_query.go:98-163`。
- 已有 fast pagination 会少取一部分精确总数，但两条路径都使用 OFFSET；用户/API Key/账号强过滤时仍执行 COUNT(*)。大客户的筛选结果未必小。
- 改进：按实际排序支持游标分页、限制任意深翻页，精确总数按需计算；基于真实高基数数据跑 EXPLAIN ANALYZE，再决定索引、分区或聚合。不能仅凭 SQL 形态给出未经测量的加速倍数。

### R8 / P2：依赖审计和质量门禁需要与实际调用路径对应

- 对当前 frontend 锁文件执行 `pnpm audit --prod --registry=https://registry.npmjs.org --json`：19 项告警，2 high、13 moderate、4 low，无 critical。默认 npmmirror 审计端点不可用，改为本次命令级 registry 参数，没有修改仓库或用户配置。
- 锁定依赖包括 xlsx 0.18.5、mermaid 11.14.0、DOMPurify 3.4.14。版本告警不是已验证可利用漏洞。
- 特别复核 xlsx：`frontend/src/views/admin/UsageView.vue:618-656` 目前是写出 Excel；没有发现该依赖用于读取用户上传表格。SheetJS 原型污染公告明确说明纯导出流程不受该项影响，因此不把这条 high 告警描述成已证实攻击入口。
- 改进：为直接与传递依赖记录生产可达路径、维护来源和升级验证，优先处理实际可达项；不使用强制自动修复把破坏性升级混进清理。
- 现行手动 Quality gate 主要跑 build/unit，数据库/Redis integration 使用独立 build tag；这些不能替代故障恢复、竞争、慢客户端和组合入口测试。保留用户要求的 workflow_dispatch，补本地/手动完整门禁即可。
- 本机 golangci-lint 2.13.1 的 unused 扫描未成功：其导出数据读取器不支持当前 Go 1.27.2 的 export data v5。这是工具链兼容性限制，不是仓库编译失败。本轮改用 Go AST 候选扫描、全仓引用核验和编译/测试验收，未把失败扫描写成通过。

## 已完成的清理

以下 14 个生产函数均核对了全仓符号引用与实际替代路径；删除了无生产入口的 3 个假成功服务方法，以及已被替代/未接入的私有辅助函数。没有删除注册处理器、第三方接口实现、迁移文件或仍有调用的兼容路径。

| 删除项 | 归属 | 删除理由 |
| --- | --- | --- |
| AccountService.TestCredentials | account_service.go | 无调用，平台分支仅返回 nil，实际探测由 AccountTestService 承担 |
| ProxyService.TestConnection | proxy_service.go | 无调用，没有网络探测，只返回 nil；实际代理检测已有独立路径 |
| adminServiceImpl.RefreshAccountCredentials | admin_account.go | 无调用，只读取账号后原样返回；连同 AdminService 接口声明和测试 stub 删除 |
| openAIWSInputIsPrefixExtended | openai_ws_forwarder_payload.go | 仅被自己的孤立单测调用；生产增量/恢复使用现有实际调用链，删除对应孤立测试 |
| stringSetting | setting_handler_audit.go | 没有引用的设置辅助函数 |
| decompressRequestBody | pkg/httputil/body.go | 没有引用的旧包装，保留有实际调用的限额解压路径 |
| schedulerCache.mgetChunked | scheduler_cache.go | 没有引用的旧读取实现，保留现行读取/管线实现 |
| hasPermissionField | middleware/admin_authorization.go | 没有引用；其余字段归一化与授权检查保留 |
| prioritizeOpenAICompactAccounts | openai_gateway_scheduling.go | 没有引用的旧排序辅助，保留实际统一调度 |
| markStickySessionHit | openai_gateway_scheduling.go | 没有引用的旧标记包装 |
| openAIWSRawItemsHaveToolCallContextForOutputs | openai_ws_forwarder_payload.go | 没有引用的工具上下文判断；实际恢复判断保留 |
| normalizeOptionalPositiveIntString | setting_parse.go | 没有引用的重复解析包装 |
| normalizeOptionalNonNegativeFloatString | setting_parse.go | 没有引用的重复解析包装 |
| strconvVendorID | vendor_oauth.go | 没有引用的单行包装，连同失效的 strconv import 删除 |

Go AST 第一轮额外产生 130 个“名称没有其他标识符引用”的候选：其中 10 个上述私有函数经过确认后删除；余下导出项不等于 120 个死函数，其中包括由外部接口调用的 WebAuthn 方法、MarshalLogObject、handler/CLI 与测试工厂。本轮没有机械删除这些项。该扫描也不等于完整可达性证明。

业务代码 +0/-225 行；测试 +0/-92 行；合计 14 个既有文件删除 317 行。另新增本报告及 .gitignore 的文档白名单。新增外部接口、配置项、生产执行路径均为 0；移除 1 个无调用内部接口方法。

## 验收与剩余工作

- 全后端 unit：`go test -tags unit ./... -count=1 -timeout=15m`，61 个有测试包全部通过。
- Go 1.27.2：`go build ./...`、`go vet -tags unit ./...` 均通过。
- git diff --check 与修改文件 gofmt 检查：通过；Go AST 候选扫描复查未留下本轮定义的私有单声明候选。
- 本次没有执行真实 PostgreSQL/Redis integration、生产流量或故障压测；不得将上述 unit 结果等同于这些覆盖。
- R1 的隔离 overlay 复现：按“必须保持 model-A”的契约失败，实际 model-B；这是未修复问题的证据，不属于清理测试通过结果。
- 本次没有实施 R1-R8 的业务行为调整，没有运行生产请求、数据库迁移、提交、推送或部署。保留所有发布渠道与品牌配置。

建议实施顺序：先修 R1 的快照边界，随后处理 R2/R3 的结算状态，再做 R4/R5 的故障恢复；R6/R7 以实际部署和负载验证；依赖升级与工具链门禁单独提交并验收。

## 规范依据

- Gin 的 goroutine/context 生命周期文档：`https://gin-gonic.com/en/docs/middleware/goroutines-inside-a-middleware/`。
- Go net/http.Server 的请求读取超时语义：`https://pkg.go.dev/net/http#Server`。
- PostgreSQL 关于大 OFFSET 仍需计算跳过行的说明：`https://www.postgresql.org/docs/18/queries-limit.html`。
- Redis reliable queue 的领取、处理中与确认模式：`https://redis.io/docs/latest/commands/lmove/`。
- SheetJS 原型污染公告及纯导出例外：`https://github.com/advisories/GHSA-4r6h-8v6p-xvw6`。
