# 数据与配置契约

日期：2026-10-25；源码基线 `750d4a16c7adaf558cf2c6ce7101975602832d7b`，已包含当前 Cloud parser 重构。状态：工程设计资料，未实施；**运行验收 `not_run`，真实客户端/线上证据未采集**。

本文负责数据事实和存储决策，不重定义接口：[CONTRACTS.md](CONTRACTS.md) **0.1** 是 CT-01～11 唯一语义来源，[REQUIREMENTS.md](REQUIREMENTS.md) 管理需求，[DECISIONS.md](DECISIONS.md) 的 ADR-05/06/07/08/09 及 OPEN-01～04 管理相关决策/缺口。主要追踪 REQ-04/05/06/08/09/13/14/15/16/18。目标模块/视图名称均为**设计**，不表示对应包已存在。

任务与写集唯一来源为 [work-items.json](work-items.json)，阅读视图见 [WORK_PACKAGES.md](WORK_PACKAGES.md)，协作遵循 [COLLABORATION.md](COLLABORATION.md)。本文供 WP-03～07、WP-11/12、WP-16/17/20/22 消费；切换/删除/回滚见 [MIGRATION.md](MIGRATION.md)。三品牌共享，文档版本化交付遵循现行规则；生产上线/迁移仍按现行独立授权边界。

## 1. 事实层级与存储地图

- **现状**指已阅读的源码、SQL 定义及调用关系，不代表目标数据库实际部署状态。
- **目标**指新模块必须实现的契约；本文提出的统一配置 revision、领域视图和通用 fencing 并非已有成品。
- **待验证**指尚无运行证据、客户端报文或现场配置支持的事项。测试文件存在、文档检查通过，都不等于测试通过。

### 1.1 Ent 与 SQL 的实际归属

手写 schema 位于 [backend/ent/schema](../../backend/ent/schema)。已核查：`user.go/api_key.go/user_allowed_group.go`（主体/准入）；`account.go/group.go/account_group.go/proxy.go`（资源/关联）；`user_subscription.go/user_platform_quota.go/usage_log.go`（资金/记录）；`vendor.go/account_supply_identity_claim.go/vendor_settlement.go`（供给/结算）；`batch_image_job.go/batch_image_item.go/idempotency_record.go`（异步/通用幂等）；`setting.go/security_secret.go`（设置/密钥）。

SQL 是运行时 schema 权威：[ent.go](../../backend/internal/repository/ent.go) 先 `applyMigrationsFS` 再初始化 Ent client。账务/grant/outbox 等手写 SQL 不能因没有对应 Ent 文件而重建。仓库已有 [297_batch_image_owned_money.sql](../../backend/migrations/297_batch_image_owned_money.sql)，不证明现场已执行；实际水位、约束及旧记录属 OPEN-04。

### 1.2 当前 Cloud parser 边界

[openai_cloud_ingress.go](../../backend/internal/service/openai_cloud_ingress.go) 的 `PreserveCloudResponsesInput` 保留 bootstrap/compact 前报文，`parseCloudIngressSource` 克隆并规范化后调用 [openai_cloud_protocol.go](../../backend/internal/service/openai_cloud_protocol.go) 的 `compileCloudProtocolRequest`；`SyncCloudReasoningEffort` 在既有策略修改点失效已编译结果。`cloudIngressState` 是当前进程请求内缓存，不是新的 PG/Redis 数据格式。迁移应移动该职责和有效事实，不能据旧架构描述重新实现一套 parser、再持久化重复正文。

### 1.3 ce58 并发增量（独立于待统一的资料基线）

另以 `git show` / 两提交 `git diff` 只读核查 `ce58a16ee67305575a704cef304b9c2b1959cb75`。此节是该提交的源码事实；首部 3e 基线暂未统一替换。下面标注“ce58 新文件”的链接在该提交存在、在 3e 检出尚不存在，不是虚构的目标包。

| 增量 | 数据/状态与保留要求 | 具体源码符号（ce58） |
| --- | --- | --- |
| WS 多流并发 | 一条物理连接持有多个 stream；同流 FIFO、异流并发。`stream_id` 选流，输出 `response.id` 与控制帧 `response_id` 关联活动轮；取消目标必须属于所选流当前轮。连接、stream、Operation、Billing Root 不共用一个身份 | [openai_ws_multiplex.go](../../backend/internal/service/openai_ws_multiplex.go)（ce58 新文件）：`RunOpenAIWSMultiplexer`、`acceptCreate/completeTurn/prepareMultiplexTurn`；[openai_ws_client_read.go](../../backend/internal/service/openai_ws_client_read.go)：`openAIWSRequestEnvelope` |
| 有界连接内状态 | 常量：活动轮16、命名流32（不含默认流）、每流等待16、整连接等待64，fork response 历史256；请求/历史另受字节预算。lane、队列、history/ready/committed 是进程状态，无跨机迁移承诺；这些上限不代替逐轮用户/Key/资源容量 | 同文件 `openAIWSMaxActiveTurns/openAIWSMaxNamedStreams/openAIWSStreamQueueLimit/openAIWSMaxPendingTurns/openAIWSMaxForkResponses`；`queuedBytes` 包含已接纳、尚未结束的请求 |
| 每流交付与归属 | lane 独立 Gin 状态、timing/trace、错误与当前 response；实际 socket 写串行化。跨流 fork 只允许已成功交付的完整历史，并比较 account/key/group/credentials generation，不借原流活 socket | [openai_gateway_handler.go](../../backend/internal/handler/openai_gateway_handler.go)：`responsesWebSocketStream/refreshOpenAIWSAuthorization`；[openai_gateway_ws_writer.go](../../backend/internal/handler/openai_gateway_ws_writer.go)（ce58 新文件）：`newOpenAIWSLaneWriter`。其 Write/Flush 只记账，不证明网络交付；以 `OpenAIWSDownstream.Write` 结果为准 |
| 能力 metadata | `UpstreamModelMetadataSnapshot.CapabilityScope` 持久化于既有 Account.extra；scope 根据账号、平台、类型、BaseURL 和身份计算摘要。实验工具声明仅在 scope 匹配时有效，按候选账号交集发布；新 live 列表撤回不能从旧快照复活。scope 不是授权 epoch/配置 revision | [upstream_models.go](../../backend/internal/service/upstream_models.go)：`SetUpstreamModelMetadataSnapshot/retainLiveCodexExperimentalMetadata`；[openai_codex_model_metadata.go](../../backend/internal/service/openai_codex_model_metadata.go)：`codexCapabilityScope/restrictCodexExperimentalToolsManifest` |
| 严格 Responses 兼容来源 | `gateway.strict_responses_compatibility` 默认 **false**；绑定 `CODEX_STRICT_RESPONSES_COMPAT`；请求/WS握手头 `X-Codex2API-Strict-Compatibility: true` 可启用。选定原生 Codex 账号后检查显式约束损失；不是全局 JSON Schema 验证，也不代替 Cloud/BPS 自有合同。有效值与来源进入策略/诊断，头不持久化为管理员配置 | [config.go](../../backend/internal/config/config.go)：`GatewayConfig.StrictResponsesCompatibility/setDefaults`；[openai_responses_strict.go](../../backend/internal/service/openai_responses_strict.go)（ce58 新文件）：`strictResponsesError/detectOpenAIResponsesCompatibilityLoss` |

WP-05/07/09/10/22 消费这些增量；现有 CT-01/02/04/05/06/08/10 的“连接不等于 Operation”和逐轮复核必须覆盖并发轮。共享身份失效会关闭物理连接各流，账号/模型/stream 失败按 lane 局部处理。不存在据本增量默认新增表的依据；metadata 仍走现有 JSON，滚动 reader 的 scope 缺省/撤回语义须验证。

## 2. 数据权威、owner、TTL 与恢复

表中 owner 是目标唯一写入责任，括号说明当前实现；TTL 为源码常量或配置默认值，均不是对生产配置的断言。“无自动 TTL”不等于数据永远不会丢失。Redis AOF/RDB、复制、淘汰及备份尚未检查。

### 2.1 PG 与长期事实

| 数据/状态 | 权威与 owner | TTL/删除语义 | 敏感信息 | 失效及恢复边界 |
| --- | --- | --- | --- | --- |
| 用户、Key、Group、资源关联、供应商政策 | PG；access/control（现有 repository 与管理事务） | 业务禁用、到期和软删除不等于缓存过期；不设置架构统一 TTL | Key、IP 规则、身份/客户关系 | 按 CT-05 使用有一致性依据的权威读取、撤销 epoch 或有效快照；硬授权无法证明时拒绝对应请求，非逐次强制 SQL；统计缓存故障不扩大为全站拒绝 |
| Account 主凭据与 Proxy | PG `accounts.credentials/extra`、代理记录；credential/egress owner | Token 到期、刷新、身份代际与账号删除分别处理 | OAuth/API Key、工作区、代理凭据 | 保留现有条件写/CAS；迟到刷新不能覆盖新授权；不把全量账号拷入公共快照 |
| 独立 Excel grant | PG `openai_excel_grants(account_id,ciphertext)`；凭据 owner | 非 Redis TTL；外键随 account 删除；刷新比较旧密文及主身份 | 加密凭据，解密材料仍敏感 | [account_repo_excel_credentials.go](../../backend/internal/repository/account_repo_excel_credentials.go)：`StoreExcelCredentials` 锁账号并比较旧密文；DB 失败不承认新 grant 已持久化 |
| Cloud 原生身份绑定 | PG `openai_cloud_grants`；授权 owner | generation/revision/revoked 由身份变更推进，非闲置 TTL | subject、workspace/user 标识；绑定不复制 AT/RT | [account_repo_cloud_authorization.go](../../backend/internal/repository/account_repo_cloud_authorization.go)：`GetCloudAuthorization`、`StoreCloudAuthorization`；绑定冲突重新读取，不能恢复旧身份 |
| 余额、订阅与平台/Key/账号额度 | PG；billing | 业务窗口重置，不等同 Redis key 到期 | 金额与用户消费 | 通过同一账务事务及幂等事件恢复；缓存只做投影 |
| 热/冷扣费去重 | PG `usage_billing_dedup` 与 `_archive`；billing | 热表归档后冷表仍参与去重；不能随使用记录保留期删除 | request/key 关联及指纹 | 热、冷都查；冲突拒绝，不能换键重扣 |
| `usage_billing_events` | PG 可恢复命令；billing（`usageBillingRepository`） | 恢复领取 30 秒；`applied` 后 payload 清空；源码按 30 天清理 applied tombstone，每批最多 500；不是去重有效期 | 脱去实体指针的用量/价格/归属；仍有业务和消费信息 | DB 成功但 ACK 丢失由旧键判定；未知 payload version 不扣费 |
| Cloud 未解决证据归档 | PG `openai_cloud_settlement_archive`；billing recovery | 无业务外键、无本表自动 TTL；先持久化再清 Redis 对应字节 | 精确原字节，损坏记录可能含非预期敏感字段，必须受限 | `(root_id,payload_sha256)` + 字节一致性；归档不是成功扣款、不是零账单 |
| 批图片冻结资金 | PG `batch_image_money_holds`；billing | v2 `(batch_id,api_key_id)` 记录 held/captured/released；不能按短 TTL 删除 | 用户、任务和金额 | 不从合并的 `frozen_balance` 猜测历史任务归属；旧精度歧义走现有只读回执核验 |
| 供应商账单/快照 | PG `vendor_settlements` 及 line/payment/allocation；billing/control | 独立于原始 usage 保留；账单版本、watermark、snapshot/hash 保留 | 商务结算、上下级和支付信息 | 不以当前资源归属重算历史账单；已付款不是代码回滚可撤销的事实 |
| 调度/鉴权 outbox | PG；control 事务产生，host worker 投递 | 消费、领取租约和重试控制，不是可丢通知 | 鉴权 outbox 保存 Key hash，仍为敏感关联标识 | 通知失败重投；不把 Redis Pub/Sub 当唯一撤销证据 |

独立 Excel 表名以 [272 migration](../../backend/migrations/272_excel_bps_grants.sql) 和当前 repository 为准；旧 BPS 文档中的不同名称不能替代源码。Cloud 原始 [291 migration](../../backend/migrations/291_openai_cloud_authorization.sql) 不等于最终结构：[293 migration](../../backend/migrations/293_remove_openai_cloud_verification.sql) 已移除 `models/protocol_version`；[294](../../backend/migrations/294_openai_cloud_native_authorization.sql)、[295](../../backend/migrations/295_remove_openai_cloud_manual_reconciliation.sql) 延续原生身份 fencing，取消独立登录前提和人工核对准入。历史列/表仍在不表示允许恢复已退役的产品门禁。

### 2.2 Redis 与进程状态

| 状态 / 当前依据 | 权威与目标 owner | TTL 语义（源码默认/常量） | 敏感信息 | 失效/恢复边界 |
| --- | --- | --- | --- | --- |
| 鉴权 L1/L2 | PG 派生；access | L1 15s、L2 300s、negative 30s，抖动；negative 只入 L1 | Key hash、权限/业务属性 | 重订阅 epoch 防旧读取回填；依 CT-05 判断有效授权依据，不要求每发必查 SQL |
| `sched:*` 快照 | PG/outbox 派生；routing | 旧快照宽限 60s、本地复用上限 10s | Account/extra/credentials 须按敏感数据审计 | outbox/PG 重建；bucket version 不等于配置 revision 或发送许可 |
| 普通并发槽 | Redis ZSET；capacity | 构造兜底 15min、正常 cfg 默认 30min；Redis TIME；成员 requestID | user/key/account 关联 | TTL 只表示占用过期，不证明上游停止；现状非通用 owner epoch |
| Live / WS ingress lease | Redis；capacity/host | 均 60s，独立于普通槽 | 主体/会话关联 | ingress 不替代逐轮容量；失效停止新发，释放不能误删新 owner |
| SessionLimit / 窗口成本 | Redis；session/billing projection | idleTimeout 来自调用者；费用缓存另有 TTL | session UUID/费用 | 与 socket、终态、账本分离；到期重新核验 |
| sticky / 历史亲和 | Redis；routing | sticky 默认 3600s；history 独立配置默认 0；写 TTL 来自调用者 | Group/session/account | 普通亲和可失效重选；强续接不得套用此降级 |
| response owner / 原生 WS | owner/account 共享；conn/turn-state 本地；session/host | 调用者 TTL + `normalizeOpenAIWSTTL` | response hash、user/key/连接标识 | v1 与 legacy pair 核对；共享 owner 出错不退本地陈旧许可；socket 不可序列化移交 |
| BPS replay/catalog | 默认内存，可选 Redis；BPS session | idle 2h；Redis读写续期，LRU 可提前淘汰；条目/总量/1024数目预算，总量32MiB | 工具参数、历史/schema、签名 | Redis 开关默认 false；开启且非 transient 后故障不退本地；CAS 随机 revision 不是配置版本 |
| BPS attachment | 进程缓存；adapter | 1h；scope+内容绑定 | 内容摘要/attachment id | 不等于上游保留期；miss 不证明未上传，沿现有附件阶段预算恢复 |
| Cloud actor/socket | 进程；owner/host | owner lease/工具等待/关闭驱动 | 正文、RPC、工具结果/出站凭据 | 进程死亡不允许假接管 socket 或 pending server RPC |
| Cloud session/owner/streams | Redis `openai:{cloud}:*`；session | owner 20s、5s续租；完成闲置30min；reference/command/event 2h；活动记录不自动 TTL 清除 | command/result/event 可含正文/图片；OwnerToken 为控制秘密 | scope+token+sequence fencing；终止记录另设 TTL；此存储不可用 fail closed，不外推到所有缓存 |
| Cloud settlement/claim/marker | Redis 未结算热事实；billing | payload 无自动 TTL；claim≤1min；settled marker 2h，archived/latency marker 24h | usage/价格/归属，异常原字节受限 | marker 到期不表示未扣费；归档查 PG；热事实丢失不能补零重建 |
| 余额/订阅/Key 限速投影 | PG 派生；billing | 余额/订阅5min减去最多30s抖动；限速7天，业务窗口另算 | 金额/订阅状态 | 事务后失效重建；统计缓存失效不是资金事实丢失 |
| 在途费用预留 | Redis 辅助准入；billing | 默认15min；余额活跃时ttl/3续租，handler结束停续租；订阅/Key scope不续租 | requestID/估算金额 | 非 PG batch hold；可选接口未实现时现状 fail-open，不能宣称已有强持久预留 |

当前实现锚点：

- [api_key_auth_cache_impl.go](../../backend/internal/service/api_key_auth_cache_impl.go)：`authCacheKey/getAuthCacheEntry/StartAuthCacheInvalidationSubscriber`；[scheduler_cache.go](../../backend/internal/repository/scheduler_cache.go)：`GetSnapshot/SetSnapshot`；[concurrency_cache.go](../../backend/internal/repository/concurrency_cache.go)、[session_limit_cache.go](../../backend/internal/repository/session_limit_cache.go)。
- [gateway_cache.go](../../backend/internal/repository/gateway_cache.go)、[gateway_cache_http_owner.go](../../backend/internal/repository/gateway_cache_http_owner.go)：`GetHTTPResponseOwner`；[openai_ws_state_store.go](../../backend/internal/service/openai_ws_state_store.go)：`OpenAIWSStateStore`。
- [openai_excel_bps.go](../../backend/internal/service/openai_excel_bps.go)：`forwardExcelBPS` 的 account/key/thread scope 和 transient 隔离；[persistent_state.go](../../backend/internal/service/basispoints/persistent_state.go)：`StateStore/NewPersistentCaches`；[gateway_cache_bps_state.go](../../backend/internal/repository/gateway_cache_bps_state.go)：`LoadBPSState/SaveBPSState`；[attachments.go](../../backend/internal/service/basispoints/attachments.go)：`AttachmentCache`。
- [openai_cloud_session_store.go](../../backend/internal/service/openai_cloud_session_store.go) 的常量/结构；[gateway_cache_cloud.go](../../backend/internal/repository/gateway_cache_cloud.go)：`SaveCloudSession/RenewCloudSession/ClaimCloudSettlement/AckCloudSettlement/ArchiveCloudSettlement`；[openai_cloud_session.go](../../backend/internal/service/openai_cloud_session.go)：`cloudSessionActor.run`。`OpenAICloudSettlementTTL` 不能解释为全部未结算事实24h自动删除。
- [billing_cache.go](../../backend/internal/repository/billing_cache.go)、[billing_inflight_cache.go](../../backend/internal/repository/billing_inflight_cache.go)、[billing_inflight_reservation.go](../../backend/internal/service/billing_inflight_reservation.go)：`InflightReservation`。视频 pending、Live call、Gemini 缓存等操作专属状态由 WP-16/17 随切片补齐，表格不冒充全库键空间穷尽清单。

## 3. 账务与既有幂等键必须保留

### 3.1 PG 事务与恢复顺序（现状）

[usage_billing_repo.go](../../backend/internal/repository/usage_billing_repo.go)：`Apply` 对带 UsageLog 命令先持久化事件；待定价返回 `ErrUsagePricingPending`。`applyUsageBillingTransaction` 原子处理 claim、资金/额度、使用记录和事件 settled。不能把此保证直接套到不带 UsageLog 的专用资金操作。

[usage_billing_events.go](../../backend/internal/repository/usage_billing_events.go)：`RecoverPendingUsageBilling` 用 SKIP LOCKED、30 秒领取，上限 64；[usage_record_worker_pool.go](../../backend/internal/service/usage_record_worker_pool.go) 的 `startBillingRecovery` 当前每 5 秒传入 16。`finishUsageBillingEvent` 在缓存失效后才标 applied、清 payload；settled 不是再次扣款许可。PayloadVersion 当前为 1，未知版本/身份错配不扣费；pending_pricing 首次成功修价持久化后复用。新模块保留这些顺序与单位。

### 3.2 身份兼容清单

| 已有身份 | 保留要求 |
| --- | --- |
| `(request_id,api_key_id)` + `request_fingerprint` | 热、冷去重同时保留；同键不同指纹报冲突；usage_logs 的唯一索引仍存在，不能靠删除日志解除扣费去重 |
| `resolveUsageBillingRequestID` | 保留强制资金事件 ID 优先于客户端/本地 ID，然后 `client:`、`local:`、上游 ID、`generated:` 的既有顺序；不要重新生成已受理请求的键 |
| Cloud root | `openAICloudBillingWorker.settle` 构造 `cloud:<RootRequestID>`；root 与 session/turn/attempt 不混为一物；已受理 root 的归属和价格不随客户端重连变化 |
| 图片批次 hold/capture/release | `batch_image_hold:`、`batch_image_capture:`、`batch_image_release:` + batchID 分别拥有事件键；不能合并为一个“批次已处理”位 |
| 语音/实时/视频/搜索/缓存存储 | 复用 `StableGrokAudioBillingRequestID`、`StableGrokRealtimeBillingRequestID` 和 `isForcedUsageBillingRequestID` 列出的前缀；新 Operation ID 不能压扁多项独立收费事件 |
| 供应商结算 | 保留 platform `(vendor_id,idempotency_key)` 与 supplier `(payer_vendor_id,vendor_id,idempotency_key)` 的部分唯一索引及 input/snapshot hash；不以用户扣费去重代替账单幂等 |
| 通用 HTTP/异步请求幂等 | 保留原 scope、key、request hash 与状态语义；不与扣费键或供应商账单键互相替代 |

依据：[071](../../backend/migrations/071_add_usage_billing_dedup.sql)、[073](../../backend/migrations/073_add_usage_billing_dedup_archive.sql)、[027](../../backend/migrations/027_usage_billing_consistency.sql)、[281](../../backend/migrations/281_usage_billing_events.sql)；[gateway_usage_billing.go](../../backend/internal/service/gateway_usage_billing.go)：`resolveUsageBillingRequestID`、`isForcedUsageBillingRequestID`；[batch_image_billing_hold.go](../../backend/internal/service/batch_image_billing_hold.go)；[vendor_settlement.go](../../backend/ent/schema/vendor_settlement.go)。027 的历史 `billing_usage_entries` 定义本身不能证明它是当前写入主链。

[usage_billing.go](../../backend/internal/service/usage_billing.go) 的 `UsageBillingCommand.Normalize` **先按原金额产生/保留指纹，再统一量化资金到 8 位**；不要调换顺序破坏升级前后重试。`BalanceExempt` 冻结个人余额豁免，不豁免 Key 限额/速率、订阅、平台保护、供应成本。新关联若确需持久化，先证明旧键映射可逆且不重扣，不能默认增加一套账本。

### 3.3 Cloud 用量持久化（现状与目标）

`OpenAICloudUsage == nil` 是未知，测得零/累计水位/确认提前结束分别表达。按 CT-07：Cloud 的 interrupted/failed 经上游确认结束且有实测 usage，才按现规则 **单根幂等结算并标 lower_bound**；该下界仍非完整 Final，不证明健康、探测成功或重放安全。仅断连、结束未确认，即使收到 usage 水位仍为 unknown，不自动计费或补零。冲突迟到事实走现有幂等/归档合同，**不新增独立后补差**。Reasoning output 为 output 子集，累计通知不重复相加。`OpenAICloudBillingSnapshot` 当前有 Version、主体/Group/Account、角色/额度、模型/价格/倍率/时间等白名单事实，见 [openai_cloud_billing.go](../../backend/internal/service/openai_cloud_billing.go)：`CaptureCloudBillingSnapshot`、`FinalizeCloudBillingSnapshot`、`cloudFrozenCost`。

`openAICloudBillingWorker.consume` claim 后走 `settle`；unknown/非法事实走 `archive`：先 [usage_billing_cloud_archive.go](../../backend/internal/repository/usage_billing_cloud_archive.go) 的 `StoreCloudSettlementArchive` 写 PG、核对 SHA256 和原字节，再条件清 Redis。PG 不可用时保留热事实，不标成功。Key 删除仍可按冻结原归属结算，用户不存在等永久异常进入证据归档。probe、已证明推理前拒绝的零费用、客户正常结算分别处理，不能把所有 ACK 都统计成客户成功账单。

目标：账务只接收可信旁路用量事实，协议、actor 和 provider 不另开补扣路径。Unknown 的持久待核对不是恢复已取消的人工准入门禁；不得自动将账号禁用直到“验证通过”。Redis 尚未归档的热事实可能是唯一副本，必须明确运维持久性要求和未完成风险，不能宣称 Redis 清空无损。

## 4. Group / Account 的领域视图（目标）

第一阶段保持现有记录、ID、关系和公开 API；只编译只读领域视图。领域拆分不是数据库拆表指令。所有消费者使用同一编译语义，迁完删除旧字段解释器。

| 管理聚合 → 视图 | 现有输入示例 | 允许消费者/边界 |
| --- | --- | --- |
| Group → AccessPolicy | status/exclusive、用户可用 Group、模型 allowlist、OAuth/privacy/client 限制、图片/live 等能力开关 | access/ingress；负面撤销必须发送前重查，价格快照不可授予权限 |
| Group → ResourceScope | account_groups、account/group priority、model_routing、fallback group、供应商范围 | routing；fallback 是新的授权候选边，不可扩大原主体范围 |
| Group → PricingPlan | rate/peak/image/video/search/audio、model_pricing、channel/mapping、long-context/free-fast | billing；资源集合与计价分组分别引用，不以“切池”隐含换价 |
| Group → SubscriptionPolicy | subscription_type、daily/weekly/monthly limit、有效期及实际 subscription | billing admission；订阅归属与 resource scope 不强绑为同一实体 |
| Account → ResourceIdentity | account/platform/type、parent、vendor、供给身份 claim、proxy | routing/session；Group 不替代资源所有者 |
| Account → CredentialGrant | 主 credentials、Excel grant、Cloud binding、代际/撤销 | 凭据 owner 在发送前取最小秘密；执行核心只拿引用和代际，禁止内核访问完整 credentials |
| Account → CapabilityProfile | 模型映射/限制、授权模式、Native/BPS/Cloud、传输及图片/音视频等能力 | catalog/provider；unknown 不等于 unsupported，目录缺项不自动推翻现行权限 |
| Account → RuntimeHealth | schedulable、error/overload/rate_limit/temp_unschedulable、窗口状态、运行观测 | routing/health；用户取消/撤销不混作供应商故障，不能覆盖管理员禁用 |

额外规则：Vendor 的 `policy_version/contract_version`、Proxy 出口身份及原有安全约束进入相应视图；不能把供应商读写权限只留前端。保存管理聚合时由领域用例统一验证，不让 UI、脚本、provider 各维护一份布尔组合。

## 5. 配置来源与 revision

### 5.1 当前来源不是一个全局优先级链

| 配置域 | 现状来源和解析点 | 目标快照/更新要求 |
| --- | --- | --- |
| 启动与宿主配置 | [config.go](../../backend/internal/config/config.go)：`Load/load/configureConfigSource/setDefaults`。显式 CONFIG_FILE 优先；否则 DATA_DIR、/app/data、当前目录、./config、/etc/kdan；环境键替换 `.`→`_`，注册默认值使 env 可达。TZ 有显式优先处理，可信代理等 env 有独立解析 | 记录有效来源与内容摘要；不可假定修改 settings 或文件就自动更新所有实例；宿主配置与业务 revision 分域 |
| 数据库业务设置 | PG settings key/value/updated_at，[setting_repo.go](../../backend/internal/repository/setting_repo.go)：`Get/Set/SetMultiple`；SettingService 按域读取、默认/缓存和回调 | 当前通用接口没有统一 expectedRevision CAS；updated_at 不是统一强 revision。引入前先定义领域边界和事务 |
| 资源、分组、供应商和权限 | PG 结构字段、JSON、关系与各域版本；账号/分组变更通过 outbox 更新调度与鉴权 | 编译只读视图；原数据库状态继续为权威，缓存版本不代替真实身份代际 |
| 模型与价格 | [model_pricing_resolver.go](../../backend/internal/service/model_pricing_resolver.go)：`ModelPricingResolver.Resolve`；Group/Channel 规则、基础目录/fallback；[pricing_service.go](../../backend/internal/service/pricing_service.go)：`Initialize`、`reloadCustomPricingLayers`、`startUpdateScheduler`，含远程目录缓存与 fallback/override 文件 | revision 必须覆盖参与解析的实际来源/内容，不能只记 Group updated_at；复用现有计价算法，不因统一快照改变计价模式 |
| JWT 与系统密钥 | [security_secret_bootstrap.go](../../backend/internal/repository/security_secret_bootstrap.go)：`ensureBootstrapSecrets`；已有 PG secret 优先于不同的配置值，无值时生成并持久化 | 密钥代际单独处理；不得因发布配置生成每实例不同 secret，也不把秘密写进快照摘要明文 |
| BPS 运行意图 | Group/Account/全局 settings；[setting_excel_bps.go](../../backend/internal/service/setting_excel_bps.go)：`IsExcelBPSEnabled` 当前本地缓存 2 秒；StateStore 开关来自 cfg | 区分当前 2 秒收敛与目标发送前撤销保证；不把可选共享状态默认化；403 条件更新保留 |

BPS 403 写入由 [account_repo_excel_bps.go](../../backend/internal/repository/account_repo_excel_bps.go)：`DisableExcelBPSOn403` 比较主凭据与 opt-in 后修改协议开关/标记，并投递 scheduler outbox；重构不能让迟到 403 禁用已换授权的账号。供应商 Excel grant 的准入事务与 refresh CAS 分别拥有权限，不能用普通设置保存路径绕过。

2026-10-27目标修订以[CT-02故障分类与恢复](CONTRACTS.md#bps-failure-recovery-20261027)为准；下表描述需落地的数据责任，不表示新增字段/迁移已实现，也不改写已应用迁移：

| 状态 | 权威及目标写入责任 | 恢复/失效边界 |
| --- | --- | --- |
| BPS系统自动停用 | PG账号既有开关与服务端故障记录；授权/调度用例经条件事务写入并发布outbox | 保留403标记的实际读者，扩展故障原因/作用域/发生时间/动作身份及授权配置代际，授权失败不得伪装403。管理员显式关闭使旧恢复资格失效，即使开关之前已是false |
| 需重新授权 | Excel凭据权威及关联的系统停用原因；不复制token到调度状态 | 权威确认缺失/撤销/不可刷新才标记；没有下次自动生成probe时间。新授权是新证据，不能由旧probe开启；本地存储/解密/刷新服务故障不证明凭据无效 |
| BPS限流冷却 | 沿现有调度状态存储扩展有证据的scope、not-before及授权关联，供多实例共同消费 | 当前sync.Map按账号的冷却只是现状，不能证明多实例/重启保持。有效Retry-After/重置时间不能被本地fallback最大值截短；普通限流不改BPS开关，不与403恢复标记共用 |
| 恢复领取与结果 | 复用现有PG claim和host生命周期，绑定系统故障身份及相关scope | 网络前领取、成功后条件更新；比较独立Excel grant及相关配置/分组，不能只比较主credentials。状态提交不确定时读回确认；探测消费与用户请求消费分开关联，仍沿既有账务链 |

上述字段含义由同一合同解释；实际schema/DTO在WP-03/04/11/18/22接线时由共享owner确定，不预建第二套账号状态库。普通无关用量更新不得使恢复永久失效，正常同身份token刷新与管理员重新授权/撤销要分别识别。旧403记录的迁移和读取继续保留，不能为统一分类删除历史故障数据。

### 5.2 revision 存储与发布（目标 CT-08 / WP-07/20）

提交/发布语义直接遵循 [CT-08](CONTRACTS.md#ct-08-目录与配置)：一致读取并编译 → 事务复核 expectedRevision 与依赖 → 保存配置/revision/audit/发布意图 → 提交后发布 → 丢通知从权威 revision 恢复。编译失败可维持最后有效配置，但即时撤销通道继续有效。

存储选择尚未定（OPEN-04）。先考察已有域版本，或受行锁保护的现有 JSON 记录；只有**所有 writer** 能保留并比较 revision 才成立。settings 当前无条件 upsert、updated_at 或 Redis snapshot version 不能代替强 CAS。跨行原子版本/审计/发布若无法由现有事务表达，再提议字段/小表，不默认新增全局配置表。

编译输出应带 schema version、来源域/ID、内容 digest、适用范围和规则引用；价格 revision 覆盖实际 Group/Channel/目录文件等输入。账务保存重放所需事实，禁止仅保存会被覆盖的“最新价格指针”。复用适用 outbox，但不能假称现有调度/鉴权 outbox 已覆盖所有域。格式改变走 [迁移决策与版本矩阵](MIGRATION.md)，是否需要 DDL 依据现有表示能否承载不变量决定。

### 5.3 revision、撤销、租约和幂等不是同一个编号

| 标识 | 解决的问题 | 不提供的保证 |
| --- | --- | --- |
| 目标配置 revision/digest | 同轮策略一致、并发保存、可复现 | 不冻结 Key、成员、凭据或供应商许可 |
| grant generation / 撤销事实 | 当前凭据身份及权限仍有效 | 不证明已取得容量或发送从未发生 |
| owner token/epoch + lease | 独占活动状态、拒绝旧 owner 的变更 | 不取消已在上游发生的生成，不证明用量为零 |
| billing key/fingerprint | 本地结算副作用去重与冲突 | 不保证真实上游生成 exactly-once |
| Redis cache revision / snapshot watermark | 缓存发布与失效顺序 | 不等于持久配置提交或即时撤销 |

发送竞态遵循 CT-03/05：复核尽量接近 Open，SendPermit 绑定单一 attempt/身份/lease；网络发送与数据库撤销不能全局原子化。已越过发送点的请求收到撤销后取消并有界排空，不承诺撤回已发送字节。

当前 [184 migration](../../backend/migrations/184_auth_cache_invalidation_outbox.sql) 与 [auth_cache_invalidation_outbox.go](../../backend/internal/service/auth_cache_invalidation_outbox.go) 提供持久 outbox、500ms 轮询、30 秒领取及二次失效延迟，配合 Pub/Sub/L1 epoch；这不是零延迟撤销证明。Cloud `validateCloudSessionOwner` 检查 grant generation/settings digest，本身不等于完整用户/Key/成员复核。

## 6. 工作包交接矩阵

任务状态/依赖只维护在 [work-items.json](work-items.json)；下表是本文件交付输入，不是第二份状态表。共享 SQL/Ent/跨模式字段由集成者或明确合同 owner 单写，遵循 [COLLABORATION.md](COLLABORATION.md)。

| 工作包 / 契约 | 本文提供的输入 | 该包需交回的证据 |
| --- | --- | --- |
| WP-03/04；CT-05/08 | 当前鉴权投影、成员关系、容量和 TTL 差异 | 等待/发送/异步交接撤销、租约丢失及幂等释放 |
| WP-05；CT-06 | owner/token/sequence、共享与本地状态边界 | 版本化读写、迟到命令、强归属与安全恢复 |
| WP-06；CT-07 | PG 事务、旧键、量化、冻结价格、Cloud/批任务费用 | 重复/崩溃/修价/取消对账，事件格式兼容 |
| WP-07/20；CT-08/11 | Group/Account 视图与真实配置来源 | revision 表达是否需迁移、CAS/审计/发布原子性、旧 writer 处置 |
| WP-11/12；CT-03/04/06/07 | BPS 可选状态与授权；当前 Cloud parser、usage/archive | 原合同保真、工具/取消/unknown，不恢复退役门禁 |
| WP-16/17；CT-09 | batch money、实时/异步专属状态需独立盘点 | 补全操作专属 producer/consumer、TTL、资金及产物归属 |
| WP-22/23；CT-10 | worker、socket、持久事实的不同寿命 | 有界 Drain/Stop 和数据库最后关闭顺序 |
| WP-24/25；REQ-16/18 | 历史 reader、兼容退出与未知事实 | 旧路径/数据读者删除证据，按实际层级验收 |

## 7. 证据与未确定事项

证据当前为静态源码核查；运行验收统一 **`not_run`**，真实报文/线上证据统一 **未采集**。复用测试入口见 [MIGRATION.md](MIGRATION.md)，完整验收场景以 [VALIDATION.md](VALIDATION.md) 为准。

| 对齐决策 | 尚未确定 | 未补齐前边界 |
| --- | --- | --- |
| OPEN-01 / WP-00/11/12/25 | 当前真实客户端完整报文、原生/BPS/Cloud 取消与 usage 合同 | 不用 fixture 名称冒充 live 证据，不自行改变协议 |
| OPEN-02 / WP-05/10/25 | Redis 持久性/淘汰/Cluster 行为、活跃旧 owner/版本、跨实例恢复 | 不宣称清 Redis 无损或活 socket 可接管 |
| OPEN-03 / WP-00/25 | 实际负载、状态规模、TTL 覆盖时长、资源/性能阈值 | 不预填容量或性能改进 |
| OPEN-04 / WP-05/06/20 | schema 水位、历史 payload/旧表存量、revision 最终表示及旧 writer 兼容 | 优先保留现有结构；不预造 SQL、不删除旧数据 |

`runtime_blocked`/历史人工核对结构仍可能有存量；当前迁移删除准入要求不证明数据已清空。后续清理代码与处置数据分别交接。已授权且不依赖缺失证据的实现可继续。本轮未改业务代码、未执行迁移。

## 统一项目与品牌配置补充

按REQ-19和[BRANDING](BRANDING.md)，site_name/site_logo/api_base_url等复用现有settings及公共投影；镜像/容器/挂载由部署配置负责，不存进可公开的网站品牌对象。技术项目统一sub2api，不由运营品牌选择代码路径。旧显式设置保留，旧编译fallback先核对实际有效值再一次性迁移；网站改名不得改变账务键、存储namespace、凭据或会话归属。公开API展示地址、管理API、provider上游和更新来源分别管理。
