# CPR 增量能力整合技术方案

日期：2026-10-28。状态：本轮四项后端增量已落地并完成隔离本地验证；实际任务状态与写集以 work-items.json 为准。仅 codex/sub2api-rebuild；人工验收环境保持冻结。

## 输入与复核方法

用户指定研究来自会话“分析 Codex Proxy RS 移植方案” `01a166ae-3502-7eb0-bcc1-472e5e9b6ed0` 的 turn `01a17c14-399a-7323-8e1a-cedbb54e97d0`。该研究比较 CPR `1db8b0e319431717611e3e30ae1a2d3b371de86a` 与维护版 main `025945a5233d0c9698a5ac63a9e6a79109b25bfa`；不等于当前重构的缺陷清单。复核时本地 CPR 为 `86a27bed9493feb3096baac86ac246463d794411`，只读源代码，不声明远端最新。原始分析及本轮源码输入摘要保存在 `.local/cpr-integration-20261028/`。

每项采用“既有 owner → 实际调用者 → 可复现缺口 → 修改及删除 → 验证”闭环。不移植另一语言运行时，不复制单副本假设，不靠新 wrapper 声称统一完成。

## 采用决策

| 研究建议 | 重构现状及处置 | 落点 |
| --- | --- | --- |
| 请求级执行计划 | 已有 responseExecutionPlan、真实 Operation、封定 PreparedIdentity、价格意图和预算；补每轮计划一致性及发送边界单份实时资源事实，避免同一校验前后读取不同资源 | openai_response_mode、native_dispatch/authority |
| 版本分域 | 明确策略计划、credential、目录刷新、账务身份各自所有者；不新增万能 version | 本文版本矩阵与各原 owner |
| 目录 generation/能力证据 | 在既有 Codex 目录 owner 补迟到写回、scope、认证失效、成功空目录及三态语义的真实缺口 | openai_codex_models_service、catalog |
| 等待 ticket | 核对实际 Redis 等待原子性、取消和共享截止；补完整真实消费者，保留各资源容量池 | concurrency service/repository、routing |
| 观测完整性/查询预算 | 审查当前重构实际实现；补有界查询的连接归还及诊断不完整标记，不新建分析系统 | ops repository、scheduling trace |
| 跨请求 ClientRetryStore | 条件不满足前不实施：客户端 event_id/请求ID/previous_response_id 均不能自行当作可信且唯一的业务轮次。禁止将失败标记当分布式互斥或幂等执行证明 | 保留请求内唯一预算与发送事实 |
| 插件集合排空 | 核对现有进程 owner 的真实生命周期与总预算，只有真实缺口才补；不移植 CPR stdio/表/管理页 | plugin_runtime/manager |
| 额度预测 | 只读预测必须具备窗口、独立 token/费用完整性和采样证据；不能将近似值接入选择、停用或账本 | 复核现有采样消费者后决定 |
| zstd、SSE、Smart、刷新错峰、Settings CAS | 已有有效单一实现，复用并核对，不重复开发 | 现有实现 |

## 请求事实和版本矩阵

| 事实 | 冻结范围 | 实时重验/恢复约束 |
| --- | --- | --- |
| 用户、Key、初始模型、客户端协议、OperationID | 认证入口创建的 nativeDispatchRequest/Operation | 每次准许和实际发送均校验 Key、用户、分组、额度；原始身份不可替换 |
| 候选账号的模式、映射模型、原生传输选择 | 当前轮次实际接纳的 responseExecutionPlan，按账号隔离 | 同轮同候选计划不能静默漂移；新WS轮次重新准入，解析下一帧不能修改前轮 |
| 账号/凭据/代理 | PreparedIdentity、真实 credential digest、wire policy | 每个发送边界重新取权威事实，撤权/换代/租约过期立即拒绝；同一次检查使用同一份资源读取 |
| 参数、canonical/raw/sealed bytes | 分别由 ingress、协议准备和 PreparedAttempt 拥有 | 不把规范化正文冒充原始字节；TLS安全重试不重新发放总预算 |
| 价格与资金来源 | NativeBillingIntent/各模式既有价格快照 | 恢复只补实测事实并走原账本，未知不置零，不从当前设置重算历史价格 |
| 目录 | provider scope、凭据身份、客户端画像及刷新代际 | 未知能力不等于支持；认证失效和临时网络错误分开；旧刷新不能覆盖新目录 |
| 等待 | 首次等待时间、请求原截止、Redis实际票据/租约 | 取消清理，出队重验，重选不重置预算，队列拥塞不伪装上游429 |
| 观测 | 请求/尝试关联、截断/丢失事实 | 不能以空结果证明没有失败；SQL连接真实归还前不提前释放查询容量 |

## 实施与验收

MAIN 单写计划合同/集成和请求计划；目录、等待、观测三个独立写集并行。每个 worker 先证明实际缺口，补既有 owner 及对应测试，不扩大公共API或改变已批准BPS规则。BPS当前执行不转原生；账号级自动停用后的新请求和条件恢复保留。

验证包括：同轮/新轮/模式及映射变化；HTTP/WS与跨传输发送前撤权；目录迟到成功/失败及认证换代；Redis取消/排队/跨实例竞争；查询超时/Close顺序；观测截断。仅运行受影响检查，并追加定向 race/实际本机 Redis/PG 验证。构建测试使用独立资源，不能覆盖用户正在人工验收的镜像/数据库。不得将本切片通过写成整个重构完成。

## 实施结果

### 具体实现

1. **请求计划与实时校验**：在实际 nativeDispatchOperation 中按账号保存第一次接纳的 responseExecutionPlan 值。仅解析下一轮 WS 帧不写入前轮；同一操作同一候选的模式、映射模型或选择的原生传输变化时，返回请求级 response_execution_plan_changed，停止当前请求而不惩罚上游。其他候选独立映射，新认证轮次有新计划。HTTP、持有 WS 及 WS→HTTP 的封定策略检查改为消费权限 owner 本次读取的资源；删除原先独立的重复读取。Admit/Consume 仍分别重新校验，真实容量策略及影子账号父凭据仍保留独立必要读取。WS复用键同时绑定目标、代理及选中账号/凭据owner代际；过期时钟或每次dial临时签名不被当作持久凭据换代，在途连接不被强制杀掉，发送仍走实时授权。这不是一次读取授权后永久缓存。发送前价格模型事实还改为读取同一份实际有界正文并重建 replay reader，删除对调用方 GetBody 返回内容的信任；独占流不被提前消费。Messages/Chat→Responses 两条编码器补上真实 Model/UpstreamModel/BillingModel 的同一意图接线，避免只有测试预置事实才可发送的缺口。
2. **目录 owner**：原缓存唯一写入口改为 generation 条件 publish，替代无条件 set；共享刷新键包括本地代际。按凭据 owner、身份、请求画像隔离缓存，刷新前后校验实际凭据。401/403 清除同凭据目录并短期负缓存，普通临时失败不延长 last-known-good 的上限。成功空数组生效，null/畸形不冒充撤回；BPS独立 fresh-only 缓存复用条件发布但保留原权限语义。API-key能力声明归已有 accountCodexToolCapabilities 统一消费；显式非法布尔归未知，旧身份的声明不进入新目录。本地确实实现的能力仍有自己的证据，不把目录授权混为一谈。
3. **等待 owner**：用户和账号分别保持 Redis FIFO 票据，同级到达序号由 Redis 原子分配；普通快速获取也不得绕过已登记队首。首次等待绝对期限属于逻辑操作，重选账号不重置；取消与成功容量交接互斥，关闭等待票据不释放已转交的容量租约。入口继续保持 SSE 心跳和原资格复核。本地队满与上游限流使用不同错误。票据只保存身份/时间/序号，不保存正文，不把 Cloud 工具等待、业务任务队列或 Live 长连接合并进普通容量队列。
4. **观测 owner**：Ops 仓储的分析/列表/聚合 SQL 按实际连接持有名额，最多4条，数据库池允许时给业务保留两个连接。排队、取连接和 SQL 总时限取30秒及调用方期限中的较短值；归还连接、关闭 rows/事务回滚前不释放名额。统计缺失小时与真实原始数据比对；依赖故障返回错误而非“无数据”。调度诊断增加 decisions_complete 与 incomplete_scheduling，区分本地决策和上游 attempts；截断后已有摘要计数继续累计。取消的运维采集不继续发起数据库查询。

以上只接管已有 owner 的确定缺口，不引入另一个总执行器、账本、目录或观测系统。整个多平台重构的剩余父工作包仍保持原状态。

### 不直接移植的明确依据

- CPR ClientRetryStore 的 record_failure/clear_failure 是失败后的可丢失标记，Redis比较删除防止旧成功清新失败，但没有生成前独占 claim，**不能保证跨实例同一轮只发送一次**。本地 Codex turn_id 可缺失并由指纹准备生成；Responses event_id/previous_response_id 又分别是事件关联和续接身份，不能统一当幂等执行ID。若未来实施，必须先明确客户端主动提供的轮次合同、API Key+会话隔离、报文冲突处理，以及未发送/无副作用边界；本轮不注册一个缺少消费者依据的 RetryStore。
- PluginManager 现有 route 原子发布、artifact/config/binding复核及 pluginRuntime beginRequest/draining/inFlight 已提供单路插件的整组切换与排空。新请求不能接纳旧进程，实际转发继承请求 context。没有多插件链，复制 CPR PreparedSet/stdio runtime 会制造第二体系。保留现有生命周期回归验证。
- 当前账号用量查询的 WindowStats 是窗口聚合，不是可证明连续的配额采样序列；它缺少独立 token/费用完整性及回落分段事实。直接外推会把未知当已知。只读预测暂不进入生产：未来需以账号+凭据代际+上游窗口作为采样scope，累计至少5个百分点、重复读数不增样本、回落/重置断段；token与费用分别完整才分别输出预测，低样本/缺口明确状态。不得反向接入调度、停用或账本。此为有前置条件的后续功能设计，不声称本轮已上线预测。
- 不冻结或复制数据库整份候选账号实体。当前快照/准入/账务 owner 分别负责自己已经落地的值对象；尚未迁完的多平台候选编译仍属于 WP-07/08 的原任务，不能凭一个新大 struct 宣称完成。

### 最终验证（2026-10-28）

所有结果对应同一冻结输入，源码 SHA256：04b36ed10bd01e6cd0ed2802ed6ed480b4341692584d13b02cf963035693a636。当前工作区相关输入与冻结源码逐文件一致。

| 检查 | 实际结果 | 边界 |
| --- | --- | --- |
| 六个后端包扩大回归 | 2061个测试及子测试通过，0失败、0跳过 | 含Native HTTP/WS消费者、目录、并发和Ops；不是全仓或前端753项页面验收 |
| 定向race | 294个测试及子测试通过，0失败、0跳过 | 与上一行重叠，不相加 |
| 独立真实Redis | 13个通过事件，0失败、0跳过 | 双client/32并发、实际Lua、FIFO/取消/状态上限/lease；不是两台完整网关或Redis Cluster验收 |
| 独立真实PostgreSQL | 取消、连接容量保留、归还及再查询通过 | 已计入unit/race；不使用生产数据，不证明真实上游计费 |
| 根Dockerfile | 独立API镜像构建成功，56.901秒 | 锁定原基础镜像，未升级依赖 |
| 隔离API运行 | health、public settings、合成管理员登录、身份读取4项200 | 容器内部HTTP；无上游生成消费，未代接受运营协议 |

候选镜像：sub2api:cpr-review-04b36ed10bd0；image ID：sha256:617cf758a569c5f7015772a04a263e7b2a1449cddfd37bd80e7e82b118e56599。现有人工验收环境未替换，未提交、未推送。

证据位于 .local/cpr-integration-20261028/：verification.json、frozen-source.json、frozen-unit.jsonl.gz、frozen-race.jsonl.gz、frozen-redis.jsonl.gz、docker-smoke.json、resources.json。

扩大验证修复了实际正文/GetBody不一致的定价事实来源、Messages/Chat缺少编码器模型事实、旧凭据/端点仍匹配WS池、取消后的Ops额外查询。过期夹具补齐真实意图/价格端口、独立WS新轮回入口、实际握手兼容键及未发送不虚构账务回执。API-key HTTP仍保留HTTP路由；另有真实本机socket的OAuth HTTP到WS消费者证明。错误托管用量仍保留原始证据并进入有原因的待准备状态，不伪造零账单。

严格FIFO只适用于本版脚本的普通user/account容量参与者；Live独立租约保留，旧二进制混跑不宣称同样FIFO。匿名等待计数仍有真实调度/限额读者。通用priority、多插件集合、跨请求失败抑制及配额预测不冒称已实现。全项目未完成工作包和真正外部上游验收按原清单继续。


资源收尾：保留验证候选镜像186,089,788逻辑字节（约177.5MiB）和约6.33MiB压缩证据/恢复资料；本任务临时服务及卷已清理。共享Go缓存实测28.37GiB，超过项目8GiB上限，因混有其他任务且无法安全归属，缓存清理未完成；未执行全机prune。
