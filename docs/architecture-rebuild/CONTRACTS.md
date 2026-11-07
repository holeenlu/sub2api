# 模块接口与状态契约

契约版本：0.4。状态：设计基线及逐项实施；已编译并接入的子集见IMPLEMENTATION，未落地合同仍属设计。本文件是跨工作包接口语义的唯一来源。以下签名是设计签名，不是已编译SDK；类型声明由WP-01按实际消费切片落地，后续工作包消费同一版本。既有外部API默认不变。

## 所有权与依赖

| 契约 | 消费者 | 实现责任 | 允许持有的状态 |
| --- | --- | --- | --- |
| CT-01 身份和事实 | 各模块 | model共享类型单写者 | 不可变身份、值对象、稳定标识；无service.Account指针 |
| CT-02 执行 | ingress、作业宿主 | execution | Operation、attempt预算、恢复动作和退出协调 |
| CT-03 单次尝试 | execution | 具体provider/模式adapter | PreparedAttempt、上游stream、模式专属临时状态 |
| CT-04 协议 | ingress、provider | protocol | 无副作用解码、wire和转换诊断 |
| CT-05 调度租约 | execution | access/routing及设施实现 | 资格判定、候选排序、容量租约 |
| CT-06 会话 | execution、provider | session及owner host | 续接绑定、epoch、actor、索引；socket不进入持久化DTO |
| CT-07 账务 | execution、host恢复任务 | billing及存储 | 价格规则快照、幂等事实、结算结果 |
| CT-08 目录配置 | execution、control | catalog/access | 不可变策略revision、能力、动态撤销代际 |
| CT-09 作业实时 | 对应ingress/host | 操作执行器+具体adapter | 作业/轮次标识、状态和取消，不挪用文本结束语义 |
| CT-10 生命周期观测 | 各模块 | host/observability | task句柄、脱敏事件、独立可靠性策略 |
| CT-11 控制前端 | HTTP控制面、UI | 领域用例 | 类型化命令/查询、权限与revision，不直接持有网关运行体 |

Go依赖：model不依赖任何更高模块；protocol只依赖必要的纯数据/编解码库；execution依赖自己消费的端口及model，不依赖具体providers/storage/Gin/Ent/Redis；provider和storage通过composition root接入。保持现有internal/domain/platforms.go作为平台注册事实来源，在明确迁移前不复制第二张平台清单。

接口定义归消费者所在包或真正共享的model；不新建无边界的common/interfaces包。不将整个service实体序列化到context。context仅用于取消、期限和现有追踪关联；业务输入、输出和授权事实显式传参。

## CT-01 身份 数据寿命与错误

| 值对象 | 最小字段语义 | 禁止事项 |
| --- | --- | --- |
| Principal | user/key、角色/供应商scope、经过验证的身份来源 | 不把客户端声称的user_id当权威 |
| OperationRef | request ID、operation ID、客户端协议、接收时间 | 不等同于WS连接、session或billing root |
| ConnectionRef / StreamRef | 物理连接及其命名流身份，由ingress持有；每流可先后提交多个operation | 不把stream_id转发给不支持它的上游；并发流不共享可变请求状态 |
| SessionRef | opaque ID、租户/用户/Key绑定、模式、owner epoch | 不仅按可猜测ID检索后返回内容 |
| ResourceRef | 稳定account/parent-owner/vendor身份与平台 | 不携带可变Account对象或凭据明文 |
| GrantRef | 凭据身份/代际、授权方式与允许用途 | 不是token；Excel-only不得变为原生授权 |
| PolicySnapshot | revision、模型/资源/恢复/定价规则引用 | 不冻结撤销状态或把旧资格当永久可用 |
| ModelIdentity | requested、routed、sent、returned、billed及来源 | 不把计价别名用于选号；returned要有可信来源 |
| UsageFact | provider turn、event identity、计量单位、累计/增量、可信度、结束证据、实测下界标记、指纹 | unknown不填零；禁止不同事实共享幂等身份 |
| FailureFact | cause class、发送事实、上游状态/Retry-After、可验证恢复依据、归因维度 | 不把健康判定或跨账号重试动作直接塞入adapter |

原始wire在Operation生命周期内不可变。出站改写产生本次attempt副本；观察日志仅保存必要摘要。帧由生产者持有至下一次Next或Close，消费者需异步保留时显式复制或取得有界引用；禁止未界定寿命的复用缓冲。大附件通过受权限约束的引用和流式接口交接，不复制到通用上下文。

时间来源用于持续时间和绝对截止，不混用客户端时钟。保留现有金额/配额精度与单位，DTO必须携带单位；不在重构中改货币精度。所有新值对象字段的零值是否有效在WP-01声明，unknown/absent/zero不得混用。

错误分类至少分：InvalidInput、Denied、Revoked、CapacityUnavailable、PreparationFailure、AuthenticationRejected、RateLimited、TransportNotSent、TransportAmbiguous、ProtocolFailure、UpstreamFailure、Canceled、Deadline、UsagePending、SettlementPending。公共HTTP/SSE/WS错误映射由ingress保留现行合同，内部错误不泄露token、地址凭据或用户内容。

## CT-02 执行主接口

设计签名：Execute(ctx, OperationInput, DeliverySink) → OperationResult。

OperationInput包含Principal、原始wire/已解码事实、客户端协议与操作种类、可选continuation、追踪引用。调用方不得注入未验证的ResourceRef或任意billing root绕过归属校验。

OperationResult包含operation/session/turn关联、终态、各attempt的结果摘要、DeliveryState及BillingDisposition；返回成功不等于所有usage已确定，PendingUsage必须显式表达。一次方法返回后不能存在无人负责的生成goroutine。

执行顺序：解析与准入 → 获取快照 → 验证续接硬归属 → 编译候选 → 计费资格/适用预留 → 容量租约 → 准备 → 发出前复核 → 登记attempt → 单次发送与交付 → 恢复或终止 → 持久化结算/待核对 → 释放资源。

| 操作状态 | 可转移到 | 条件 |
| --- | --- | --- |
| Accepted | Authorized、Rejected、Canceled | 每次业务提交重新准入 |
| Authorized | Waiting、Preparing、Rejected、Canceled | 已固定配置/定价策略及续接约束 |
| Waiting | Preparing、Rejected、Canceled、Expired | 等待有界；出队后复核范围与租约 |
| Preparing | Dispatching、RetryPlanning、Failed、Canceled | 准备不能产生未登记业务生成 |
| Dispatching | Receiving、RetryPlanning、Failed、Canceled | 已登记attempt；报告NotSent/Sent/Ambiguous |
| Receiving | Completed、Suspended、RetryPlanning、Failed、Canceled | 交付提交后不启动另一份生成 |
| RetryPlanning | Waiting、Preparing、Failed、Canceled | 中央预算、权限、恢复依据全部满足 |
| Completed/Suspended/Rejected/Failed/Canceled/Expired | 无新的业务发送 | 按实际取得的资源执行有界收尾；已有生成事实仍按CT-07处理，不能凭拒绝状态补造用量/费用；原状态不因费用恢复重新生成 |

工具挂起后Operation可以结束，但Provider Turn/Billing Root按该模式合同保持。下一次客户端提交创建新Operation并重新准入；续接是否沿用同一turn由经验证模式事实决定。

保留ce58交付的Responses WS多路复用：物理连接一个读泵和串行写协调；同流FIFO，不同命名流可并发，每轮各自准入/容量/预算/计费。命名流局部业务错误不自动终止其他健康流；身份失效或物理连接/连接lease失效终止整连接。stream与operation、session分别建模，不以统一引擎把并发能力退回单流。

三轴：SendState=NotSent/Sent/Ambiguous；DeliveryState=Uncommitted/Committed/Closed；UsageConfidence=Unknown/Partial/Final。响应头发送与业务提交分别观察，已发头时不能再输出另一份JSON错误；按当前协议用终态或关闭连接处理。

恢复决策由execution按失败事实、续接硬约束、预算、已交付状态决定。同目标、换目标、刷新grant、模式切换、同turn续接分别是显式动作。禁止adapter新建总预算、重置deadline或暗中发送下一轮生成。

<a id="bps-routing-20261027"></a>

### BPS 回退与自动停用修订（2026-10-27）

用户明确取消BPS受控回退原生，同时确认账号级故障应分类自动停用/恢复、403自动关闭BPS开关影响后续请求是正常业务逻辑。本修订替代此前保留BPS→Native候选边的目标要求；属于已确认需求，生产实现和验收仍待WP-11/WP-18及授权、调度、宿主工作包完成。

- 一次客户端业务提交选定BPS后，本Operation不再转Native。能力不匹配、Excel授权不可用/拒绝、429、上游失败及BPS候选耗尽均不能触发原生兜底；适用于HTTP JSON/SSE和Responses WS，以及进入同一BPS执行链的兼容入口。禁止通过重新读取账号开关、换账号、换传输或新建Operation伪装当前请求的原生回退。
- BPS只匹配最终账号映射后的明确勾选模型；缺失、空或非法模型列表不表示全选。不自动替用户填充历史账号列表；具备原生授权的独立新请求按Native处理，Excel-only不因此获得Native权限。Cloud开启仍优先且保留保存的BPS列表；关闭Cloud后按该列表恢复选路。
- 同模式内已有合法刷新、重试及换候选仍受原授权、续接、发送证据和总预算约束；本修订不扩大重试，不抹除已发生的BPS用量，不改变已交付后禁止另起生成的合同。
- 保留403自动停用的配置条件和并发条件写入；先按下表区分账号拒绝、模型权限、文件归属与请求拒绝。符合条件时真实持久化关闭BPS开关及停用标记，不改成仅临时熔断，也不新增一套“路由意图”状态来阻止该业务效果。确认Excel授权不可用的自动停用按下表处理，不冒用403原因或403专用设置。
- 停用成功后，后续独立新执行按更新后的有效配置、模型与权限选择路径；Cloud未开启且原生授权有效时允许走Native。Excel-only仍不能走Native。普通配置传播沿既有合同，不将本修订解释为新的全局强一致开关机制。
- previous_response_id、待回复工具调用及活动turn的提交先核对原归属和当前授权；新的HTTP请求或WS帧不自动等于独立新执行。BPS停用不能成为把旧续接、opaque引用或取消命令交给Native的依据；无法合法续接时明确拒绝，不丢弃引用重新生成。
- 保留现有显式403恢复probe、持久claim与条件恢复；并发手动关闭、换授权或变更分组不能被旧probe覆盖。恢复后后续新执行重新按有效配置选择，不能重放先前失败的用户请求。

<a id="bps-failure-recovery-20261027"></a>

#### 故障分类、状态写入与恢复条件

失败分类必须消费可信来源、实际端点/阶段、上游结构化原因及凭据状态。HTTP状态码、对客错误码或adapter包装后的笼统“授权不可用”均不能单独证明账号失效。普通生成、图片上传、压缩、repair和恢复probe使用同一分类规则；图片/工具准备已发生消费时，停用和恢复不改变原账务事实。

| 事实及作用域 | 开关/运行状态动作 | 恢复条件 |
| --- | --- | --- |
| 明确账号级BPS 403拒绝，已排除模型权限、文件归属、内容/请求限制 | 按既有自动停用配置关闭该账号BPS，持久化系统停用来源、原因及本次停用身份；保留既有有条件分组动作，不扩展到其他账号 | 开启既有自动恢复且授权仍有效时，按配置间隔到期领取探测，成功后条件开启；仅HTTP 200、连通或不完整响应不算成功 |
| Excel access token被拒绝/过期，但当前grant可刷新 | 原凭据owner在既有总预算内有限刷新并校验身份，不因第一次401就关闭整个账号BPS | 刷新成功后仍按原发送/续接规则继续BPS；刷新成功不是当前请求可重放的独立证明 |
| 权威读取确认Excel grant缺失、明确撤销，或有限刷新后有确定授权拒绝证据 | 自动关闭该账号BPS并记录授权原因；缺失/撤销/无可用刷新授权标记需重新授权，不误伤独立原生凭据，不将该原因记为403 | 需重新授权时不定时向无效凭据发送生成probe；重新授权后重读当前配置/停用身份，由管理员显式开启或在仍获授权的恢复流程中验证，旧probe不得开启新grant。凭据仍有效、仅BPS端点暂时拒绝的情况才可安排端点恢复探测 |
| 429及可信Retry-After/重置时间 | 仅暂停已确认的账号/BPS、模型、端点或共享配额作用域；不因普通429关闭BPS开关。缺少范围证据沿已知BPS范围及既有缺省处理，不扩大到整池或无关授权 | 不早于有效上游时间进行有界恢复尝试，较短的新信号不能缩短已有冷却；无有效时间才用既有fallback。可由受控探测或到期后新请求验证，不能两者并发密集试探；到期不等于已证明恢复 |
| 请求能力不匹配、参数/工具选择/内容拒绝 | 返回对应错误，不关闭账号BPS、不触发账号恢复probe | 后续可支持的请求仍可使用BPS；某模型权限拒绝只影响该账号映射后的对应模型，不外推账号封禁 |
| BPS候选耗尽 | 当前请求失败；不批量关闭账号，不进入Native阶段 | 每个候选仅按自身已确认事实更新状态；池耗尽本身没有恢复probe或账号处罚 |
| 网络超时、上游5xx、本地数据库/缓存/解密服务故障、刷新服务超时、客户取消 | 不当作凭据已撤销或账号403，不据此持久化关闭BPS；保留已有有界临时故障处理及可信归因 | 依原预算与恢复合同处理；授权查询失败不等于查无grant，不能将一个公共依赖故障放大为全池停用 |

429冷却与关闭开关是不同状态：冷却期间开关未变，命中的BPS请求不能借冷却绕回Native；不受该scope限制的模型/能力继续正常选路。只有已确认的共享限流事实才进入既有共享配额规则，不能由单次BPS 429推断Native/Cloud也限流。关闭BPS后新请求可选Native，Cloud仅在其本来已配置且合格时参与选择，恢复任务不得自动开启Cloud。

恢复以系统停用记录和当前授权为依据，复用现有宿主/存储链，不新增自动登录体系：

1. 停用记录至少表达可信原因、作用域、发生时间、系统动作身份、关联授权/配置代际和可选下次尝试时间。需重新授权时下次自动探测时间为空，不能填零后立即重试；具体持久化归属见DATA_AND_CONFIG，不将临时冷却伪装为管理员开关。
2. 到期先取得同一故障作用域的持久claim，再发起有界probe；重启及多实例不能绕过已保存的冷却/领取时间。有效Retry-After不能因本地fallback上限被截短；长时间等待可分批唤醒检查，但不得提前发出上游请求。失败使用同一owner的有界退避，遵守最新Retry-After，不为每次扫描重新建立重试预算。
3. 探测前与恢复写入前复核真实grant、账号/供应商资格、全局开关、Cloud选择、管理员配置和相关分组/模型。绑定独立Excel grant，不能只比较原生credentials；正常token刷新与重新授权/撤销身份分别处理。成功probe只证明被探测scope，不能清除其他模型/端点冷却或扩大模型能力。
4. 只有当前仍匹配的系统自动停用身份允许条件恢复。系统已关闭后管理员再明确关闭也必须阻止旧probe；不能只比较false→false。停用、恢复与发布用现有条件事务/outbox，提交确认不明须读回核对，不宣称写入成功，也不盲目覆盖新状态。
5. probe是独立且有预算、并发、截止和用量归属的实际请求，绝不重放用户失败正文；成功沿既有完整终态/nonce校验，EOF/200不代替成功。取消、Stop/Drain和丢失claim后停止新发送并有界收尾；已发生消费按原账务合同记录，不借后台探测绕过计量或费用归属。

所有退出都遵循资源栈：取消生产者 → 有界排空/必要用量收集 → 持久化可信事实或待核对 → 释放stream/prepared资源与本操作租约。账务恢复使用继承授权关联的有界独立上下文，不因客户端断开完全丢失，也不无限脱离请求运行。

#### 系统停用后的显式编辑条件（2026-10-27接线）

管理员完整更新、Extra更新和批量更新在原事务行锁内校验BPS开启条件。当前为系统停用时，显式true必须携带刚读取的安全恢复投影`disabled_at`，输入字段为`extra.openai_excel_bps_expected_disabled_at`；供应商既有PATCH使用`bps_expected_disabled_at`。缺失或旧条件返回409 / `OPENAI_EXCEL_BPS_STATE_CONFLICT`，不得清除新停用标记、发布成功outbox或自动重试覆盖。条件仅保护这次系统停用，不是账号所有字段的通用revision；请求条件不得持久化。

未操作开关的普通编辑省略该字段，不能把读取到的false变成明确停止恢复。显式false即使当前已false也取消旧probe。供应商list/detail使用与管理员相同的`excel_bps_403_recovery`安全投影，不返回私有failure/grant/policy。旧`bps_all_models`输入明确拒绝，不能通过删除显式模型列表重新表示全选；选择模型必须提供`bps_models`。这些接口的运行验收仍以TEST_REPORT的实际结果为准。

## CT-03 Provider 单次尝试

设计签名：

- Prepare(ctx, AttemptSpec) → PreparedAttempt 或 FailureFact。
- Open(ctx, PreparedAttempt, SendPermit) → AttemptStream 或 FailureFact。
- AttemptStream.Next(ctx) → WireFrame、FactDelta 或终态。
- AttemptStream.Cancel(ctx, cause)；Close()。Cancel、Close重复调用不得造成新生成或重复扣费。

AttemptSpec固定resource与授权身份约束、mode/transport、model身份、原始输入指纹、continuation及能力计划。准备前输入指纹不等于最终wire指纹：BPS上传等准备步骤可以取得attachment/file ID并生成出站副本。PreparedAttempt封定最终安全头、完整目标、传输参数及解析后的grant身份。可重放正文从实际发送字节封存并计算指纹；不可重放流以不可替换的独占body handle封定，BodyDigest明确unknown、禁止自动重放，不能为凑哈希偷读或耗尽流。压缩/编码必须在封定前完成。执行者据此复核、登记attempt并授予permit。PreparedAttempt不是可持久化token包，凭据和连接归具体设施。Open只能发送封定内容；任何语义变化必须重新准备、复核和登记，不能重置operation/owner总预算。

Prepare可做明确有界的凭据刷新、连接预热或必要控制面准备；不得启动生成。会创建上游会话等副作用的准备必须声明其owner、清理和幂等依据。Open是唯一业务发送入口，SendPermit只能消费一次并绑定operation、attempt、sealed identity、当前授权/租约及期限。

execution在登记attempt后授予permit；Open不能更换资源、模式、凭据身份或正文。token轮换若改变被校验的grant代际，重新准备和复核，不能沿用旧permit。实际网络发送与数据库撤销无法全局原子化：确保尽可能接近发送的复核，之后收到撤销/lease失效立即取消，不承诺已经发出的请求可撤销。

Next返回原始帧和来自同一数据的事实，允许二者之一为空。EOF不自动等于成功；适配器必须说明明确终态、断流或unknown。发送事实以传输能证明的状态为准，不能以无响应字节推断NotSent。

协议差异保留在provider/模式内，BPS图片阶段、Cloud thread/turn和原生WS恢复遵循PROTOCOLS。核心不执行客户端工具代码。已有插件通过受控宿主适配此端口，插件权限不因接口改造扩大。

## CT-04 协议与交付

设计签名：Decode(protocol, WireRequest) → RequestFacts；Convert(from,to,WireRequest,ConversionPolicy) → ConversionResult；Encode(clientProtocol,WireFrame,Facts) → ClientFrame；DeliverySink.Write(ctx,ClientFrame) → DeliveryReceipt。

ConversionResult包含输出、明确from/to、转换步骤、unsupported/loss诊断及原文关联。没有网络、配置查询、账务或选号。未知扩展原生透传；跨协议无法表达的字段按已有合同明确拒绝或声明转换，不能默默吞掉。

DeliveryReceipt记录实际写入/flush结果、业务提交变化和终态；执行层不能在Write成功前自行假定交付。Keepalive与可暂存metadata不自动构成业务提交；无法再替换的response/tool身份按协议规则构成提交。部分写失败保守认为可能已交付。缓冲大小、等待与backpressure有界，继承已有限制；消费者退出必须取消生产者。

SSE帧拼接、WS完整消息、非流式聚合分别保留各自合同。工具call id、item索引、reasoning/refusal、usage和finish必须有来源，不凭部分增量造完整终态。

## CT-05 授权 调度与租约

设计签名：Authorize(ctx,Principal,RequestFacts,PolicySnapshot) → AccessScope；Plan(ctx,Scope,Capabilities,Continuation) → CandidatePlan；Acquire(ctx,Candidate,CapacityRequest) → ResourceLease；Revalidate(ctx,Scope,PreparedIdentity,Leases) → ValidatedDispatch；Lease.Renew/Release。

Scope来自当前权威授权。空/关闭/失效资源池不扩大到全局候选。Plan固定策略但不把资格冻结；Acquire后和Open前复核Key、用户、供应商、归档、成员、grant与lease。实时额度/健康事实按已有调度政策使用，不新增全局评分算法。

实时复核不等于每次强制执行一次SQL或全局拒绝所有缓存；沿用有明确一致性依据的权威读取、撤销epoch及有效快照机制。区分硬授权/容量依赖与统计缓存，依赖故障时不能将陈旧信息当作新的许可，也不能把统计缓存故障扩大成全站拒绝。

Lease包含唯一token、owner、resource/用户作用域、容量单位、有效截止和epoch；Release只释放本token，幂等，不能误删新owner的租约。续期失败在有效期内有界处理，过期停止新发送并取消正在持有的生成。亲和记录不代表已取得容量。

失败归因与恢复动作独立，区分客户端、权限、资源、上游、依赖、取消；不能把本地排队或支付拒绝算作上游健康失败。继承现有账号/供应商资格和预算语义。

## CT-06 会话与owner

设计签名：Resolve(ctx,Principal,ContinuationRef) → SessionBinding；AcquireOwner(ctx,Binding) → OwnerLease；AppendFact(ctx,Binding,Epoch,Fact)；RouteCommand(ctx,Binding,Epoch,CommandID,Command)；CloseTurn(ctx,Binding,Epoch,TerminalFact)。

SessionBinding绑定用户/Key/有效范围/resource-owner/grant identity/mode及上游response/thread/turn；价格/设置约束按现有模式合同保留。普通亲和和强续接分别建模。未知/不匹配绑定不能通过客户端输入补成可信记录。

Owner epoch对命令、输出和状态写入防旧owner；command ID用于去重及关联，不能保证上游无幂等能力的副作用exactly-once。活socket留在进程；Redis只承载可定义的定位/消息/状态。租约过期后的重建必须依赖该模式真实恢复合同。终态迟到usage进入CT-07，不能因owner失效一概丢弃。

原生WS同连接跨流续链保留已有条件：仅从成功交付终态的可恢复历史重建，校验同Key/Group/账号及该合同要求的完整凭据代际；不借用另一流活动连接，不把失败/取消/部分输出当恢复依据。保留现有有界缓存和store:false数据失效行为，不据此承诺跨连接共享恢复。

存储敏感历史的TTL、容量、加密/访问控制沿用现有实现并在DATA_AND_CONFIG明确；不从本设计推导无限保留。

## CT-07 账务

设计签名：CheckEligibility(ctx,BillingIntent)；Reserve(ctx,BillingIntent) → Reservation（仅现有策略适用时）；RecordUsage(ctx,BillingIdentity,UsageFact) → RecordResult；Settle(ctx,BillingIdentity,PricingSnapshot) → SettlementResult；Release(ctx,Reservation,Reason)。

BillingIdentity稳定映射现有request/key幂等键、provider turn及供应商结算主体。金额计算只由billing规则执行，适配器不增减余额。保持请求/上游/返回/计价模型区分：冻结价格规则不等于永远按请求模型计费。

Record/Settle/Release重复调用均以已有持久化幂等合同保护；改变旧键需迁移论证。累计和增量UsageFact明确分开，不重复叠加累计值。Unknown与PendingPricing持久化待核对，不能填零后标settled。账务失败需持久化恢复证据，不能仅将closure留在内存队列。

Native发送前意图接线的具体失败合同（2026-10-26补充，组件实施中）：真实解析器已确认Final用量，但尚未取得原始计价准备投影时，技术journal仍保存原Confidence、终态、可选计量字段及已保存的attempt scope，标记`usage_preparation_unavailable`；不保存臆造的SettlementUsage或金额command，不把Final降为Partial/Unknown，不把缺字段写零。缺少原始准备资料不能靠重启读取当前配置修复，因此原worker不领取此原因；真实迟到准备只有与已存Final测量/身份一致时才可补全，转为原有可恢复准备状态。重复的缺准备事实不能抹除已补全投影；不同attempt不共用投影。保存失败不能返回耐久回执，技术事实耐久也不代表已完成金额结算。此合同和组件测试不表示before-send/Finalizer/worker生产装配已接通。

登记attempt后、保存价格scope前失败的窗口（2026-10-27）：完整操作快照必须保留该attempt。只有确证NotSent且无上游ID、用量和终态的观察，才允许没有scope digest；它不能成为selected intent或计价来源，不能以零用量替代。关闭已有技术intent仍要求完整操作失败/取消、全部attempt未发送且未交付、所有已保存scope逐一匹配；较晚的NotSent不能抹去较早Sent/Ambiguous。首次保存失败时没有已确认intent就不制造耐久回执；提交确认丢失须通过原journal核对实际记录，无法确认则保留收尾错误。恢复继续只消费真实保存的价格与事实。

可观察状态：Eligible、Reserved、PendingUsage、PendingPricing、Settled、Released、RecoveryPending。Released只退还适用的预留，不删除真实用量。资金来源、用户余额豁免、Key配额、订阅和供应商成本分别保持原有语义。Cloud ACK与持久化/结算时序由模式有效合同约束。

保留当前已确认提前结束的实测下界合同：Cloud的interrupted/failed经上游确认且有实测usage时，按现有规则单根幂等结算并标lower_bound；它仍不等于完整Final usage，不能用于宣称完整健康/探测/重放安全。仅断连、结束未确认时，即使收到过usage水位仍为unknown，不自动收费或补零。本设计不新增后续独立补差路径；冲突迟到事实按现有幂等/归档合同处理。依据为现有openai_cloud_lower_bound_test.go及相应实现，当前仅阅读未执行该测试。

## CT-08 目录与配置

设计签名：Snapshot(ctx,Scope) → PolicySnapshot；Compile(rawConfig,ExpectedRevision) → ValidatedConfig；Commit(ctx,ValidatedConfig,ExpectedRevision) → NewRevision；CheckRevocation(ctx,IdentityRefs,ExpectedEpochs)。

编译已有平台注册、模型能力、路由/价格规则，不重复平台列表。模型unknown不是unsupported，目录不是新增产品准入。配置提交以expected revision防丢更新，保存配置/revision/audit后发布；通知只加速，各实例能从权威revision收敛。紧急撤销独立于请求快照。

区分模型列表缺项和工具能力声明：前者不能自动扩大为模型权限拒绝，后者未知时不得宣称支持。保留ce58中experimental_supported_tools按可用账号/映射求交集、撤回旧声明和身份作用域校验的行为。原生Codex可选严格兼容配置及请求头也保留其既定范围和默认，不把它扩大成所有模式的全局字段拒绝。

组/账号现有数据先映射为只读领域视图。每个字段有唯一解释模块；跨模块只传已验证值对象。不要在前端、脚本、后端分别实现相同组合规则。

## CT-09 异步作业与实时

JobExecutor.Submit/Poll/Cancel/GetArtifact使用独立JobRef和既有幂等合同；创建、轮询和取文件分别鉴权，后台推进仍受资源/用户scope限制。受控下载出口和产物归属不可丢。作业费用按真实任务证据进入CT-07。

RealtimeExecutor.Accept/StartTurn/Send/Receive/CancelTurn/Close区分连接与业务轮；每轮复核授权、容量、归属与预算。音频双向并发与背压明确，不能以文本EOF代替实时终态。voice id归属和Grok用户并发保留。

批次中子任务标识与账务合并规则依当前实现列明；不默认全批次只计一次，也不将父任务和子任务重复收费。细节以PROTOCOLS及DATA_AND_CONFIG的当前合同为迁移输入。

## CT-10 Host 观测与插件

Host.Start(ctx)建立必要依赖后接收请求；Drain(ctx)停止新准入、让在途有界结束、保存恢复事实；Stop(ctx)按依赖逆序关闭。构造只构造，worker拥有明确父context、错误处理和关闭等待；不遗留匿名goroutine。

EmitObservation(Event)与RecordAudit(AuditFact)分别声明可丢/必须持久化策略。事件关联operation/attempt/session/资源/协议mode/计费状态，不传播token或默认全文内容。日志异常不改业务已提交状态，关键审计失效按既有安全合同处理。保留现有插件进程的信任边界，不新增第三方运行权限。

## CT-11 控制面与前端

现有REST接口和RBAC/step-up为外部合同。实施基线已有GET/PUT /api/v1/admin/settings的revision条件写入：读取64字符revision，保存时附带revision与实际修改字段；省略字段保留，缺失/长度错误返回428，冲突返回409及SETTINGS_REVISION_CONFLICT。该设置聚合一次事务提交，拆前端面板不能改成多次不可回滚请求。此前将revision全列为设计目标的表述已纠正；其他领域尚无通用revision或统一六维能力DTO，新增对外合同仍须先核实调用方并同步前端。领域命令携带经过验证的actor/scope、目标ID、expected revision和类型化payload；查询返回领域视图，不泄漏凭据。共享管理和供应商用例必须在服务器核验对象归属。

前端按能力/配置类型展示，服务端权威校验。revision冲突展示明确错误，不能以最后写覆盖全部设置。网站品牌差异复用现有设置并按BRANDING收敛；不得以品牌分支重编译或分别实现路由/计费规则。

## 统一项目与品牌配置补充合同

按[BRANDING](BRANDING.md)，CT-08/10/11共同承载REQ-19，不增加平行品牌服务。项目身份固定为sub2api并集中维护；部署镜像/容器/挂载由部署配置管理，网站site_name/site_logo/api_base_url等复用现有设置和public投影。公开展示地址不能修改管理API、provider上游或更新信任源。品牌更新不得改变账务/会话/存储身份；同角色同版本制品支持不同品牌配置。旧编译fallback、品牌选择器和广告专属路径随迁移退出；LICENSE/NOTICE和必要来源保留。维护三分支不承接重构交付；独立分支策略见根AGENTS，不是目标程序的品牌分叉合同。

## 契约变更与实现检查

WP-01提交实际Go类型和受影响调用方后形成可编译的contract基线。任何签名/单位/状态语义变化先更新本文件与work-items契约版本，列出所有消费者，由单一契约owner合并；消费者不能另加同名类型和兼容转换绕过依赖。

实现验收同时检查：调用方/实现方签名一致、状态与资源释放完整、失败分类可观察、既有安全账务不变量、无循环依赖。不能仅以编译通过宣称合同已实现。

## 0.3 独立项目清理变更

用户要求删除新项目中的无用旧实现：系统二进制更新、回滚、回滚版本列表和门户updater/socket链退出，三个管理写/列表路由删除，细节见FRONTEND_HANDOFF。保留实际消费的只读构建/手动部署状态和独立重启操作。原版本同步GitHub读取及价格配置不属于软件安装链，继续保留。此版本仅为工程契约，不新增客户端准入版本门禁。

0.4同时整合已制定的FRONTEND_ARCHITECTURE与编码规范1.3，并包含独立项目旧接口退休；未增加运行时协议版本准入。

## 0.4执行切片接线记录

execution.FirstOutput承接既有首输出绝对截止、自动/显式policy选择、有界准备暂停和不可逆交付标记。Configure与attempt准入由调用方串行；Snapshot仅供观察，Watch接收交付通知，Commit只表示入口实际不可撤回输出，不证明上游终态或Final usage。服务层只提取既有配置、context和传输timer，不再持有另一份deadline/committed状态。此切片不等于Execute或SendPermit全合同完成。

会话切片由session.ExecutionIdentity/ExecutionScopeSeed唯一解释线程、请求kind、subagent与命名stream作用域；protocol/codex只解码wire，Gin边界继续使用既有显式session resolver及哈希格式。新类型中的客户端声明不是可信授权，不能替代CT-05复核或CT-06 owner lease。

账务切片由billing/reservation.Handle持有已准入预留的生命周期：构造不启动后台任务，显式StartRenewal；HandlerDone取消并等待续期，但异步计费引用仍保持到完成或原TTL。Snapshot只输出原有恢复身份，Scopes为独立副本；Release是明确回滚/完成收尾，不等于unknown usage可标settled。余额/订阅/Key缓存原子脚本、金额估计、豁免及故障策略沿用原规则，service负责已有配置与日志适配。旧预留struct及其接口已替换，没有保留同名alias。


授权/目录切片追加：access.GroupBinding与Group只表达现有身份/分组事实，HTTP/Google/WS续接继续依既有入口顺序复核，不是可复用SendPermit；authz仍拥有认证/RBAC。catalog.ModelAllowlist唯一解释现有模型白名单，NormalizeModelAllowlist返回纯错误，由控制面边界映射既有400/INVALID_MODEL_ALLOWLIST。模型名/别名解释迁到catalog，但未知模型、传输选择和费用合同未改变。持久化domain类型保留原JSON字段，仓储做显式转换；DTO/cache的Models切片共享规则尚未变更，不将此切片称为完整不可变PolicySnapshot、统一CapabilityProfile或CT-08撤销发布实现。


## 发送许可实现合同（2026-10-25追加，接线中）

model.PreparedIdentity是可比较的封定值，包含operation、已验证principal、实际资源与physical owner、mode/transport及grant/target/wire/body身份。BodyReplayable必须有真实正文摘要；BodyExclusiveStream只有本次独占handle，正文摘要保持unknown且禁止重放。零值无效。此修正来自当前流式body及晚压缩调用链证据，保留已有流式能力，不创造假的完整哈希。

execution.Dispatcher使用同一个AttemptBudget和FirstOutput，根装配提供DispatchAuthority。Admit实时复核后才登记并生成具体SendPermit；Open只接受该具体许可，不接受任意返回nil的授权callback。许可私有共享原子状态保证复制后也只能消费一次；消费前再次校验完整sealed identity、原operation取消、期限和实际授权/lease fingerprint，变化后必须重新准备，不能延长旧许可或重置预算。fingerprint必须来自已核实的scope/owned lease，不是客户端值或虚构代际。零fingerprint fail closed。

SendState零值为Unknown；未调用发送的准备/许可拒绝可证明NotSent，有发送或响应证据才是Sent，证据不足为Ambiguous。它不证明上游生成完成。此核心目前正在接入NativeHTTP；WS/Cloud具有独立准备/owner/恢复合同，未接线前不得声称CT03全平台完成。
