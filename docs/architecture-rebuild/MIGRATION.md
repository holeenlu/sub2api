# 架构切换与数据迁移方案

日期：2026-10-25；源码基线 `750d4a16c7adaf558cf2c6ce7101975602832d7b`（含当前 Cloud parser 重构）；合同版本 **0.2**。状态：方案未实施，**运行验收 `not_run`，真实客户端/线上证据未采集**。

本文是 [REQUIREMENTS.md](REQUIREMENTS.md) REQ-02～08/12～18 和 [CONTRACTS.md](CONTRACTS.md) CT-01～11 的切换落实方案；依 [DECISIONS.md](DECISIONS.md) ADR-05～11/13/14，不另定义状态机/接口。现状存储/TTL/幂等键见 [DATA_AND_CONFIG.md](DATA_AND_CONFIG.md)。目标模块均为设计，不是假定已经存在的包。

任务 ID、依赖、写集、状态以 [work-items.json](work-items.json) 为唯一来源，[WORK_PACKAGES.md](WORK_PACKAGES.md) 仅为阅读视图；共享集成遵循 [COLLABORATION.md](COLLABORATION.md)。文档版本化与独立重构分支交付遵循根AGENTS当前范围；外部上线、生产迁移与数据处置仍按既有独立授权边界，已授权动作直接执行，不增加固定审批流程。

## 1. 切换单位与副作用边界

按完整垂直切片切换：入口、授权、执行/attempt、单次 provider、输出/取消、账务恢复、关闭全部纳入。一个 Operation 只有一个有副作用的执行者，纯计划/转换可对比，真实上游不双发、账务不双记。发送/交付/usage 三轴、撤销检查、强会话归属和迟到费用直接沿用 CT-02～07。注册接入与旧路径删除属于同次集成，不通过永久开关保留双核心。

## 2. 迁移输入与证据缺口

**已静态确认**：3e 的 parser、PG/Redis/账务合同及另行只读核查的 `ce58a16ee67305575a704cef304b9c2b1959cb75` 原生 Responses/WS/metadata 增量，详见 [DATA_AND_CONFIG.md](DATA_AND_CONFIG.md)。资料首部基线仍待主集成者统一；ce58 新文件按该提交对象核验，不要求 reset 或把不存在的目标包当现状。

**尚未采集**：具体部署/二进制版本，实际 schema/索引/触发器/校验和，旧 payload 与活跃 owner 存量，Redis 持久性/淘汰/备份，真实客户端与上游取消合同，对账和性能基线；对应 OPEN-01～04。源码默认值不是现场配置，测试文件不是执行回执。WP-00 冻结实施输入；只阻断依赖缺失事实的行为切换，不阻断独立准备。

## 3. 是否需要数据迁移：逐项决策

默认判断是**先保留现有表、ID、事务和格式，用端口与只读视图承载新边界**。不是每个新领域对象都需要独立表。

| 变化 | 首选承载 | 何时才需要迁移提案 | 必须提供的证据 |
| --- | --- | --- | --- |
| 目录/包移动、唯一执行编排 | 现有表与 repository 端口 | 不应仅因此新增表 | 公共 API/数据格式与账务树等价、旧入口删除 |
| Group/Account 领域拆分 | 从原记录编译 AccessPolicy/ResourceScope/PricingPlan 等 | 现有表示无法表达约束，或有经测量的查询/事务需要 | 源字段→视图映射、唯一校验 owner、公开 API 兼容；不能只画新 ER 图 |
| 配置 revision | 复用已有域版本，或在可完整控制的现有记录中保存版本 | 跨行原子发布/审计/冲突检测不能在现有事务内表达；旧 writer 会抹除版本 | expectedRevision 竞态、更新事务、发布重放和旧 writer 处置 |
| Operation/Attempt/Billing Root 关联 | 先保留旧 key 与已有 root/usage 关联，必要时仅观测投影 | 恢复跨进程确需持久关联，且现有事件/状态无法承载 | 与旧键一对一/一对多映射规则、未知用量生命周期、无二次扣费证明 |
| Redis envelope/version/fence | 在现有状态 owner 内版本化编码/键空间 | 不必然需要 PG DDL；若新增必须持久化的审计事实再论证 | 新旧 decoder、键碰撞/TTL、CAS、跨版本迟到消息、活 socket 处理 |
| 账务 payload 扩展 | 尽量保留当前 PayloadVersion=1 与合法字段 | 新事实无法被旧命令解析/审计，或约束确需关系字段 | 旧 worker 读能力、冻结价格/指纹、待处理事件的兼容/排空策略 |
| 索引优化 | 现有 SQL migration runner | 有查询计划、锁/容量证据需要新索引 | 实际计划与规模、创建/失败恢复、旧版本仍可读；仅 `_notx` 支持并发索引 |
| 删除旧 JSON 字段/表/列 | 先删失效写入者与运行入口，保留必要读兼容 | 所有实际 reader、恢复任务、备份导入和回滚窗口已关闭 | 消费者证据、数据存量与处置授权；不能为“干净”改历史迁移 |

迁移提案最小内容：目的与不变量、为什么原表示不足、完整文件/字段范围、owner、schema/数据格式版本、旧/新 reader/writer 矩阵、回填幂等键与检查点、对账、上线前条件、回滚止损点。未预定必须新增表；本轮未执行迁移。

如确需变更结构，采用扩展 → 兼容读取/按需回填 → 单写切换 → 验证 → 删除的顺序。回填由稳定主键分批、可重入、记录进度，不触发上游操作、不重跑扣款；不能替历史记录生成一个新的“未结算”事件。回填/删列按现行数据处置与生产迁移授权范围执行，不因本方案新增审批节点。

## 4. 垂直切片与工作包交接

下表只说明数据/切换交接点，不复制 JSON 任务状态和全依赖。所有包仍按 [work-items.json](work-items.json) 领取，运行证据 `not_run`。

| 实现工作包 → 集成点 | 本文要求的完整切换范围 | 接入与删除证据 |
| --- | --- | --- |
| WP-00 → WP-01 | 冻结当前源码/数据 reader/报文来源；形成 CT-01～11 实际类型 | unknown/zero、单位、旧键、格式寿命明确；设计签名不能当已编译 SDK |
| WP-02～09 → WP-10 | 原生 Responses HTTP/SSE、WS 多流并发、逐轮首轮/续接/工具/取消/账务；严格兼容 | 同流 FIFO/异流并发、response 路由、独立交付、逐轮撤销/容量成立；删除旧控制循环，不把整连接迁成单 Operation |
| WP-11/12 → WP-18 | BPS/Cloud 使用已验收原生主链；保留工具/图片、Excel-only、403 恢复、Cloud parser/owner/usage | 不能独立换总调度/账本；ACK、unknown、跨实例和取消完整，旧包装同期删除 |
| WP-13～17 + WP-18 → WP-19 | 其他平台与异步/实时窄端口接入 | remote job ID、逐轮授权、产物/voice 归属与资金正确；不重发已提交任务 |
| WP-03/06/07 → WP-20 → WP-21 | 领域配置/revision、旧 API 和前端保存冲突 | Group/Account 视图可追溯，旧 writer/解释器与表单状态清理；三品牌不分裂业务链 |
| WP-19/20/22 → WP-23 | 控制与 host 启动、Drain/Stop、共享路由/Wire 装配 | 生命周期和持久恢复一起完成；删除旧隐式启动及失效注册 |
| WP-19/21/23 → WP-24 → WP-25 → WP-26 | 全功能旧 reader 收敛、独立风险验收、独立分支交付 | 代码与数据回滚分别证明；WP-26 的交付不包含发版/上线/生产迁移 |

WP-01 后可按各自依赖并行准备；WP-11～17 的正式模式实现以 WP-10 为前提，不能提前将其登记为已切换。共享 SQL/Ent、跨模式 Redis、现有 handler/service、注册/Wire 只能由指定单写者处理。Domain worker 交提取实现和精确旧文件替换建议，集成者在同一切片接入并删除。跨机器交接用 [templates/HANDOFF.md](templates/HANDOFF.md) 记录实际 SHA、合同、diff、证据与资源，不使用本机绝对路径或另建状态平台。

## 5. 读兼容、有限双读与副作用禁区

### 5.1 有限双读的退出合同

| 规则 | 必填约束 |
| --- | --- |
| 来源 | 一个权威来源、一个旧格式来源；只在新记录不存在且旧记录合法时回退 |
| 冲突 | 新记录损坏/未知版本/权限不符不回退；同时存在须核对身份/指纹，不拼字段或取最后时间戳 |
| 副作用 | 读兼容不得新发生成、重建账务根或补扣；纯元数据升级也由唯一 owner 幂等执行 |
| 有界 | 实际消费者、数据合同、owner、启用版本、最长保留/退出判据明确，复用现有观测 |
| 删除 | 所有活跃 reader/恢复 worker 兼容后停旧写，再停旧读；有持久旧数据不能盲等 TTL 删 |

**现有例子**：[gateway_cache_http_owner.go](../../backend/internal/repository/gateway_cache_http_owner.go) 的 `SetHTTPResponseOwner/GetHTTPResponseOwner` 原子写 v1 与 legacy 整数 pair，新 reader 校验一致性。这是同一事实的格式兼容发布，不能据此允许双业务 writer；删除 legacy 写前必须核对旧 reader 和 response owner TTL。BPS 本地历史、CAS revision 与 Cloud owner 各有语义，不能改 namespace 后当请求从未发生。

### 5.2 对比与重试允许做什么

允许：用同一脱敏输入比较纯路由/转换计划；同一冻结 usage/价格离线计算；把同一持久事件重投同一幂等用例；读取旧 owner 已确认事实。禁止：双发生成/工具/附件/建任务；两套扣款后对冲；重投时换 requestID；失联后重放 turn/start；用可变双写掩盖数据 owner 未统一。

## 6. 滚动版本、会话与 lease fencing

### 6.1 必填版本兼容矩阵

发布前以具体旧/新二进制填写，不能只写“JSON 向后兼容”：

| 持久/共享对象 | 旧 reader → 新 writer | 新 reader → 旧 writer | 不兼容时动作 |
| --- | --- | --- | --- |
| Group/Account/settings | 不识别字段是否忽略，旧表单回写是否丢 revision；多行事务语义 | 默认/显式空/未知配置如何解释 | 限制旧控制面 writer；或先发布兼容 reader；仍不行则暂停相应管理写入并 drain |
| usage_billing_events | 当前 worker 只接受 PayloadVersion=1；新版本不应写旧 worker 无法安全恢复的事件 | 旧事件价格/指纹/BalanceExempt 保持 | 先保证 reader；隔离不兼容 worker 的领取资格或暂停生产者并处理存量；不能让其循环 claim 未知事件 |
| Cloud session/command/event/settlement | 原字节、大整数/价格精度、sequence、owner、快照不被旧 codec 破坏 | 缺新字段不自动证明 NotSent 或零用量 | 保留旧 owner 到终态/事实持久化；无法支持则 drain，不宣称 socket 可迁移 |
| BPS replay/catalog | 工具 ID/signature、namespace/schema、CAS 和 scope | 当前历史是否完整，新增字段是否可安全忽略 | 原 owner 完成会话或明确终止；按现有合同要求完整历史，不能猜测 |
| WS multiplex / lane / fork | 旧版是否理解 stream envelope、response 路由与并发交付，不能默认可读 | 单流旧客户端、命名流及 fork 历史的身份/完整性 | 不透明搬迁队列/socket；原连接 drain 或明确结束，新的连接再路由，不重放已发送轮 |
| 实验工具 metadata / strict 配置 | 旧 writer 是否抹除 CapabilityScope，旧版是否支持 strict/header | 缺 scope 不当有效工具声明；live 撤回不可从旧快照复活 | 保留单写与保守能力解释；不把 strict 头当新增持久授权，也不因回退无声放宽显式约束 |
| capacity/session lease | key/scope、时间单位、续期/释放方式、owner token 是否一致 | 是否识别所有旧 lease 并拒绝迟到释放 | 同一命名空间的原子协议先兼容；不兼容时暂停该容量域新请求 |
| 异步 job 与 batch money | provider job ID、snapshot version、held/captured/released | 旧任务金额/终态能否可证地恢复 | 已提交任务保留恢复者；不换版本重新创建远端 job，不按混合冻结余额猜归属 |

Cloud 当前 `OpenAICloudSessionRecord` 有 token/sequence，`OpenAICloudBillingSnapshot` 有 Version；没有据此证明全链已存在统一滚动升级 envelope。新增统一 schema version 是目标设计，是否必要、如何与旧 reader 兼容必须先落实矩阵。

### 6.2 fencing 必须检查的位置（目标 CT-05/06/09/10）

- 领取：以 scope + 不可复用 owner token/epoch + 到期时间获得资格；容量与会话归属分别验证。不得把请求 ID 当万能锁所有权。
- 续租：只续有效且仍属于自己的 lease，不能复活已过期/已替换 owner。时间源遵循存储协议，避免跨节点本地时钟直接判定所有权。
- 写入：session 保存、命令接受、输出发布、终态更新、异步回调和释放均比对 owner/sequence；迟到释放不得删除新 owner 的资源。
- 发送：按 CT-05 用有一致性依据的权威读取、撤销 epoch 和有效快照复核；不要求每次发送强制 SQL，不因统计缓存失败全站拒绝。硬授权/lease 无有效依据时停止受影响派发；已发送结果不明走 Sent/Ambiguous、有界取消和事实保存。
- 失联：只有既有合同支持时恢复**同一生成的事实/终态**；不能因为拿到新 lease 就重新执行业务。Cloud `ClaimCloudSession` 的恢复 owner 不能重放 turn/start 或假定 pending server RPC 存活。
- 迟到用量：失效 owner 不能继续生成，可信事实进入原 root。按 CT-07，Cloud 已确认 interrupted/failed 的实测 usage 可单根结算并标 lower_bound，仍非完整 Final；未确认结束即使有水位仍为 unknown，不自动计费/补零。冲突走现有幂等/归档，不新增独立后补差。

原生 `response→conn` 只在进程内有效；Cloud command stream 路由到 owner 也不迁移 socket。运行节点退出前须让可接受的新请求避开 drain 节点，同时按原路由保留必要控制/取消/恢复通道。只改负载均衡不等于已封住全部 WS lane 的后续轮、队列领取或异步提交。ce58 的多流关闭包含 worker 等待，不能据此假定已满足目标有界 Drain；WP-22/23 需逐轮固化事实后释放连接资源。

## 7. 暂停新请求与有界 drain

### 7.1 进入条件

跨版本 reader/writer 或 lease 无法证明兼容、无法保留强会话归属、需要改变资金表示、旧 flusher/worker 尚可能写同一状态时，使用暂停与有界 drain。它是一次切片变更的运行步骤，不建立长期“双引擎模式”或新运维平台。

在获授权的环境执行前，把总截止时间、单操作取消截止时间、usage/归档截止时间及退出条件写入当次交接记录；根据实际存量时长和现有配置决定具体值。不能把 owner 20 秒、session 闲置 30 分钟或 HTTP Shutdown 5 秒直接当整个 drain 期限。若期限未确定，禁止声称方案已经可无损上线。

### 7.2 有序关闭协议（目标）

| 顺序 | 动作 | 完成判据 / 超时后处理 |
| --- | --- | --- |
| 1 固定范围与水位 | 记录旧/新版本、切片范围、PG 事件与 Redis root/owner 基线；冻结本次路由决策 | 其他切片仍有明确 owner；范围不因重试悄悄扩大 |
| 2 暂停新业务 | 拒绝/暂缓新 Operation、所有 WS lane 的新 create/排队派发、异步新提交/领取；控制/取消/状态查询继续 | 观测证实不再增加该片新 attempt；不能只关 HTTP 新连接 |
| 3 分类在途状态 | NotSent、Sent/Ambiguous；交付 Committed/Uncommitted；usage Unknown/Partial/Final 及已确认 lower_bound 分开 | 未发送可按合同结束并释放；已发送不得重新选号或新发 |
| 4 排空或取消 | 有界等待当前生成；到界发送原合同 cancel/interrupt；工具等待不得无限等客户端 | cancellation/终态证据明确；按 lane/response_id 取消；WS 关闭不等于所有上游结束，逐轮保存 usage/未知事实 |
| 5 固化结算事实 | 保持 PG/Redis 和账务恢复器可用，提交可结算事件；Cloud unknown/非法事实先 PG 归档 | 每个已受理 root 有已结算、待定价/恢复或不可变未知证据；无法持久化时 drain 不算完成 |
| 6 释放与移交 | 带 owner proof 释放容量；停 owner 续租/命令；已提交异步任务保留单一恢复者 | 旧进程不能再次发送/释放新 lease；远端 job 不重新创建，存量有明确后续负责人 |
| 7 停 worker 与连接 | 已无未记录副作用后停旧恢复/投影 worker、socket/pool，最后关 DB/Redis | Stop/Drain 不无限等待；超时列出 root/job、状态、持久化位置与恢复责任，不能宣称零残留 |
| 8 兼容读检查后开放 | 新进程读取所需旧状态/事件，核对 fencing 与账务基线，再启用单一路由 | 新请求只进新执行者；存量兼容期明确有终止条件 |

已提交的异步任务可能长于发布窗口；允许保持可恢复监控，不要求“所有远端 job 完成”才能结束进程，但必须证明新恢复者可读该版本、不会再次提交/重复 hold/capture/release。实时双向会话没有统一“最后一个 HTTP 响应”；其关闭、分钟/字符/音频用量与账户/用户并发应按 CT-09 单独验收。

**中止条件**：账务/Cloud 热事实不能持久化、lease 无法判定且旧 owner 仍可发送、出现指纹/归属冲突或金额对不上。维持停止新请求，保留证据与可工作的恢复链；不得通过清 Redis、杀完所有进程或重置 schema_migrations 假造 drain 成功。已经停止业务的节点是否强制退出属于当次运维决策，并不改变“费用事实未保护”的状态。

## 8. 账务对账与恢复验证

### 8.1 对账必须有基线和范围

按切片、时间/提交水位、user/key/account/group/供应商、授权模式、billing root、旧请求键、币种与货币精度固定范围；变更前已有 backlog 单列，不能将其归因于新架构，也不能把全局数字变化全部当新错误。按 root 去重统计请求，按独立资金事件统计扣款，按 supply 分摊统计使用记录；三者数量不应机械相等。

| 检查对象 | 需要建立的关系 | 异常处理 |
| --- | --- | --- |
| 热/冷 dedup | 以 `(request_id,api_key_id)` 合并去重，指纹一致；同键存在于热/冷都不重复累计金额 | 指纹不同停止自动恢复，保存证据；不能删一份/换键绕过 |
| 持久事件 | pending/pending_pricing/failed/settled/applied 分桶；settled/applied 不应有第二次资金效果；applied payload 清空符合当前实现 | DB commit 不明按同键重试；未知 version/损坏 payload 保留并报告，不当已结算 |
| usage_logs | 与事件/去重及冻结主体/模型/价格/授权模式一致；支持既有分摊记录、lower-bound 状态和独立收费事件 | 不为了“一请求一行”删除合法行；日志保留期与扣费去重保留分离 |
| 用户余额 | 以同窗口已知充值/退款/调账/冻结/释放及 BalanceCost 解释期末余额 | 不能仅用总实际费用推断余额差额；余额豁免与订阅资金来源分别列示 |
| 订阅/Key/账号/平台额度 | 根据原命令各 Cost 字段和窗口边界核对；Key/账号已删除时遵循既有跳过自身计数规则 | 不要求每个维度与余额扣费相等；窗口重置和删除单列 |
| Cloud root | 每个 root 对应完整结算、已确认提前结束的 lower_bound 单根结算、待处理/未知归档或可证 probe/推理前拒绝 | marker 仅短期证据；lower_bound 非 Final，unknown 有水位也不自动收费；不建独立补差 |
| 批图片资金 | 每个 v2 batch/key 的 hold→capture/release 合法且终态单一；与 job snapshot version 和旧三类请求键一致 | v1 高精度歧义只读核验，不能推定属于某任务后扣款/退款 |
| 供应商结算 | 冻结 vendor/payer/contract/policy、watermark、snapshot_hash 和实际分摊；付款事实独立 | 不以当前 vendor 归属重做历史账单，不用用户余额回滚冲抵供应商付款 |

采用现有 `UsageBillingMonetaryScale=8` 和指纹计算先于资金量化的顺序；供应商账单自身货币精度另按现有 schema，不擅自统一成一个浮点误差阈值。缺完整账变流水时明确“无法精确归因”的范围，不伪称仅靠 usage 总和即可证明所有历史余额。

### 8.2 故障窗口的验收输出

至少覆盖并记录：事件持久化前失败；持久化后进程死；幂等 claim 后事务回滚；事务提交但响应丢失；settled 后缓存失效失败；修价成功并发恢复；Cloud PG 归档前/后 Redis 条件删除失败；claim 到期被其他 worker 领取；Key/账号删除与迟到费用；取消后终态/unknown；v2 batch 已 capture 但 job 状态未写成功。每次输出应给出“同一 root/键发生了几次资金效果、剩余事实在哪里”，不能只写 HTTP 状态码或测试通过数。

恢复不需要第二账务服务：复用 `UsageBillingRepository`、`UsageBillingRecoveryRepository`、Cloud settlement/archive 和现有批任务恢复；任何新恢复步骤只能补齐事实读取/编排，不能直接绕过幂等事务更新余额。

## 9. 回滚边界与恢复次序

回滚目标是具体能读取当前事实/恢复队列的版本，不是任意“上一个 commit”。代码版本回退与数据恢复是两件事。

| 已推进到的状态 | 可做的回退 | 不可假设 |
| --- | --- | --- |
| 仅纯编译/计划对比，无业务发送 | 停止新代码路径，回到旧单一路由 | 不需要制造账务补偿 |
| NotSent 且未交付 | 明确结束该 attempt；按原合同选择旧执行者或返回可重试错误，保持 ID/预算 | 不能把准备上传/远端建会话一概当无副作用 |
| Sent/Ambiguous 或已交付 | 原 owner 有界完成/取消、保留用量，随后只回滚未来请求 | 不能重新发旧引擎取得“更正确答案”，也不能撤回已交付内容 |
| 账务事务已提交 | 保留原扣费/额度事实；旧兼容 reader 可继续同键恢复后处理 | 不恢复整库旧快照抹掉已发生费用、不再用旧价格重算 |
| 新版待恢复事件旧版不可读 | 先前向提供兼容 reader，或暂停生产者并由兼容恢复者处理；再考虑回退 | 不能直接让旧 worker 领取/丢弃未知 payload；不得清 pending 队列 |
| 新 session/lease 协议旧版不兼容 | drain 相应范围并保护事实；保留兼容恢复职责 | 不能把 Redis namespace 切回旧名就声称会话已回滚 |
| 只做兼容扩展 DDL | 旧版经验证可读时保留扩展结构，回退业务代码 | 没有默认 down，也不为回退删已保存新事实 |
| 已删列/改不可逆数据或已外部支付 | 按现有授权范围的数据恢复/补偿方案，通常前向修复 | 代码回退不恢复丢失的数据，不撤销上游生成或已付款事实 |

建议次序：停止新准入 → 固定基线/记录异常 → 分类并 drain 在途 → 固化/归档事实 → 对账 → 验证目标 reader/worker 与持久版本 → 切回未来请求 → 确认只有一个 writer → 处理剩余持久待办。严禁用“Redis ACK 全了”“队列变空”“进程退出”作为金额及 owner 正确的替代证据。

## 10. 旧路径删除证据（REQ-18）

每片由对应集成者同次删除，WP-24 最终核对；下表均为待执行，不宣称已删除。

| 集成点 / 旧锚点 | 删除对象 | 必须保留/证明 |
| --- | --- | --- |
| WP-10：OpenAIGatewayHandler、[openai_gateway_forward.go](../../backend/internal/service/openai_gateway_forward.go) 的 `OpenAIGatewayService.Forward` | 重复执行/恢复循环、context 控制字段、旧路由/装配 | 原生首轮/续接/WS/取消/费用已走唯一主链 |
| WP-10/18：[openai_first_output_timeout.go](../../backend/internal/service/openai_first_output_timeout.go) 的 `OpenAITurnBudget` | 已被新 owner 接管的模式/账号/健康字段与 helper | 原有效预算、恢复语义与健康归因不改变 |
| WP-18：Cloud parser/actor、BPS forward/state | 旧总调度/账务包装和多份规则 | 保留已完成 parser、BPS Excel/图片/工具、Cloud owner/用量；不恢复退役门禁 |
| WP-19：其他平台/异步/实时入口 | 旧有副作用执行者与过渡注册 | remote job/voice/下载、父子费用和实时容量合同 |
| WP-20/21/23：Group/Account/Settings、前端与 host | 重复解释器、旧表单/store/API/locale、隐式 worker 启动 | 所有写入口/导入、供应商权限、revision、恢复职责有新 owner |
| WP-24：legacy owner pair、旧 Redis/SQL reader | 无消费者的兼容读写与弃用字段/配置 | 活跃版本和持久旧记录已核实；历史 SQL 不删，数据处置另授权 |

证据必须同时包含：旧/新名称与符号搜索（含跟踪和未跟踪源文件）；路由、DI/Wire、动态注册；JSON/SQL/Redis 的 producer/consumer；相关测试的真实结果/未运行项；最终 diff 和受保护并发文件。`rg` 无命中不能证明动态路径或历史数据已消失。

保留兼容须记录实际消费者、格式、唯一 owner、原因和退出条件；公开有效协议不因名称含 compat 删除。代码清理与数据清除分别报告；不能改历史迁移伪装生产已清理。

## 11. 既有验证入口与证据状态

统一验收定义见 [VALIDATION.md](VALIDATION.md)。下面仅定位可复用资产，**全部 `not_run`**；真实报文/线上证据**未采集**，不新建测试平台。

| 风险 / 责任 WP | 已有文件与代表符号 | 层级限制 |
| --- | --- | --- |
| PG 重复/崩溃/修价 / WP-06/25 | [usage_billing_events_integration_test.go](../../backend/internal/repository/usage_billing_events_integration_test.go)：`TestUsageBillingRecoveryCrashAndDuplicate`、`TestUsageBillingRecoveryFreezesRepairedPrice` | 后续真实 PG harness；[unit 事务测试](../../backend/internal/repository/usage_billing_events_unit_test.go) 使用 sqlmock，不能替代 |
| Cloud actor/Lua / WP-05/12/25 | [openai_cloud_store_integration_test.go](../../backend/internal/service/openai_cloud_store_integration_test.go)：`TestCloudActorPersistsEveryTerminalStateInTheRealStore` | unit tag，默认 miniredis；SUB2API_CLOUD_TEST_REDIS 才真 Redis，harness 会 FlushDB(15)，只允许独占测试实例 |
| unknown/归档/取消 / WP-06/12 | [openai_cloud_billing_test.go](../../backend/internal/service/openai_cloud_billing_test.go)：`TestCloudBillingArchiveOnlyReleasesAfterDurablePreservation`；[openai_cloud_lower_bound_test.go](../../backend/internal/service/openai_cloud_lower_bound_test.go) | 本地替身/状态证明，不代表真实上游 usage 合同 |
| parser/历史 / WP-02/12 | [openai_cloud_client_contract_test.go](../../backend/internal/service/openai_cloud_client_contract_test.go)：`TestCloudCapturedCodexClientEnvelopes`；[openai_cloud_migration_test.go](../../backend/internal/service/openai_cloud_migration_test.go) | 文件名 Captured 不是当前真实采集回执 |
| 真实 Cloud / WP-12/25 | [openai_cloud_migration_live_test.go](../../backend/internal/service/openai_cloud_migration_live_test.go)：`TestCloudLiveMigratedHistoryQueuedMessages` | cloudlive、真实授权/网络，未运行；不重放生产请求 |
| BPS/HTTP owner 兼容 / WP-05/11 | [gateway_cache_bps_state_test.go](../../backend/internal/repository/gateway_cache_bps_state_test.go)：`TestBPSSharedStateCrossInstanceCASIsolationExpiry`；[gateway_cache_http_owner_test.go](../../backend/internal/repository/gateway_cache_http_owner_test.go) | BPS 双 client + miniredis 不等于真多进程；HTTP 文件有独立 real Redis 场景 |
| 撤销与容量 / WP-03/04 | [auth_cache_resubscribe_epoch_test.go](../../backend/internal/service/auth_cache_resubscribe_epoch_test.go)：`TestAuthCacheOldReadCannotRefillNewEpoch`；[auth outbox integration](../../backend/internal/repository/auth_cache_invalidation_outbox_integration_test.go)；[inflight tests](../../backend/internal/service/billing_inflight_reservation_test.go) | epoch/触发器/辅助预留分别验证；仍需发送/交接竞态与持久 hold 证据 |
| schema/runner / WP-05/06/20/25 | [checksum](../../backend/internal/repository/migrations_runner_checksum_test.go)、[notx](../../backend/internal/repository/migrations_runner_notx_test.go)、[schema integration](../../backend/internal/repository/migrations_schema_integration_test.go) | 按实际数据变化选择验证；当前 not_run |

BPS 继续参考 [需求](../bps/REQUIREMENTS.md)、[验证资产](../ENGINEERING.md#req-bps)、[固定来源](../bps/integration-state.json)，旧表名/来源行为/退役门禁以当前源码和有效需求复核。成功证据复用条件遵守现行交付政策，不能因阶段/品牌机械重复全量。

### 11.1 ce58 增量的必要场景（全部 not_run）

责任：WP-03/04/05/07/09/10/22/25。当前配置常量与来源直接引用 DATA_AND_CONFIG 第1.3节，不新增另一套配置值。

| 场景 | 必须观察的结果 | 既有 ce58 资产 |
| --- | --- | --- |
| 同流 FIFO、异流并发，按 stream/response 交付与取消 | 只结束指定活动轮，取消越过排队 create；局部错误不伤邻流，共享授权撤销结束各流 | `TestOpenAIWSMultiplexFIFOAndIndependentFailure`、`TestOpenAIWSMultiplexCancellationBypassesQueuedCreate`、`TestOpenAIWSMultiplexSharedAuthorizationCloseStopsEveryStream` |
| 活动/命名/等待/字节预算，逐轮权限容量 | 保持16/32/16/64边界与字节限制；连接内额度不替代每轮真实容量；排队后撤销不得发出 | `TestOpenAIWSMultiplexActiveAndNamedLimits`、`TestOpenAIWSMultiplexQueueAndUnsupportedControls`，目标复核场景再由 WP-03/04 补证 |
| fork 的所有权与网络交付失败 | 仅已提交完整历史可 fork；账号/Key/Group/凭据变化拒绝；终态写失败不能标 replay committed | `TestOpenAIWSMultiplexForkRequiresCommittedOwnedHistory`、`TestOpenAIWSMultiplexFailedTerminalUnblocksForkWithoutCommit` |
| 严格原生输入 | 配置默认关闭、env/header 启用；选定 native 的有损约束发送前失败；HTTP/WS 模式差异、Cloud/BPS 自有合同保留 | `TestResponsesStrictSelectedAccount`、`TestResponsesStrictHTTPAndWebSocketFailBeforeDispatch`、`TestLoadResponsesStrictCompatibility` |
| 实验工具 metadata | scope 更换/缺失、账号交集、live 撤回/缺项均不能复活旧声明；不冒充授权或全局配置 revision | `TestExperimentalToolsIntersectionAndScope`、`TestExperimentalToolsPersistPartialMetadataAndLiveWithdrawal` |

源码资产：[openai_ws_multiplex_test.go](../../backend/internal/service/openai_ws_multiplex_test.go)、[openai_responses_strict_test.go](../../backend/internal/service/openai_responses_strict_test.go) 是 **ce58 新文件**，已纳入当前资料基线；[config_test.go](../../backend/internal/config/config_test.go)、[openai_codex_model_metadata_test.go](../../backend/internal/service/openai_codex_model_metadata_test.go) 按 ce58 版本定位上述符号。只读核查不等于运行过测试；独立 lane writer 的状态变化不替代实际 WS 写入/flush 证据。

## 12. 历史迁移与交付退出条件

### 12.1 历史 SQL 保护

[Migration README](../../backend/migrations/README.md) 中概述和示例需要结合 runner 解释；runner 按 SQL 全文执行，不能在同一文件加入所谓 Down 段，也不能假设存在通用 migrate-down 行为。已应用迁移不得改名/删除/改 checksum；已有明确历史 checksum 兼容规则原样保留，不能为了通过本次改动扩大白名单。新变化使用新的前向迁移，执行仍遵循现行授权边界。

当前启动会自动运行迁移，因此携带新 SQL 的服务启动本身可能执行 DDL/DML；“只启动新版本”不能绕开生产迁移授权。PG advisory lock 不能替代旧 writer 兼容性证明、流量暂停或数据库备份。

### 12.2 退出条件与未知项

WP-25 按实际层级记录结果，WP-26 在其依赖满足后按现行政策交付；不得将 not_run 当作 passed。OPEN-01/02 决定真实合同/滚动兼容，OPEN-03 决定性能/资源与 drain 阈值，OPEN-04 决定是否需要 schema 变化；缺口只阻断直接受影响切换。

未来切片完成条件：唯一执行/账务路径；旧身份/幂等/价格/撤销不变量；每个受理 root 的费用可解释；版本矩阵有证据；旧路径同次清理；保留兼容有退出条件。交接按 [templates/HANDOFF.md](templates/HANDOFF.md) 记录实际业务/测试增删及新接口/配置/路径数量。

本轮未改业务代码、未执行迁移。运行验收 not_run，真实报文/线上证据未采集；工程资料可交接不等于实现或运行 ready。

## 统一项目和品牌的迁移补充

新增WP-27在WP-23集成前完成部署参数与旧品牌变量迁移，随后WP-24清理、WP-25验收、WP-26交付；不能到发布阶段才改模板。见[BRANDING](BRANDING.md)及AT-38至43。保留旧有效设置、镜像版本和数据挂载，删除被替代的编译品牌fallback、品牌重编译选择器及广告专属路径。许可证/来源、有效业务与持久身份不按品牌词清除。当前三分支/渠道继续维护，重构只交付独立分支，目标同角色制品跨品牌配置；本轮不发布镜像、改线上参数或执行数据迁移。
