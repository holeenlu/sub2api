# 后端网关设计与实现复核

2026-10-26，按用户要求以四个维护会话为切入点，继续审查当前整个后端网关。状态：复核、修复与回归进行中，不是完整重构验收结论。来源和具体修复见[维护修复对照](MAINTENANCE_FIX_AUDIT.md)；任务状态只由work-items.json维护，实际测试进入TEST_REPORT。

## 当前结论与问题分类

存在实质性遗漏。原因包括维护分支修复没有持续逐项对照、旧消费者没有完整迁入，以及新执行/事实/账务链改造后没有验证所有消费端。目录分层和接口编译通过不能证明这些语义已经贯通。

严格区分三类：维护分支已发布但重构缺失的行为；工作包计划内尚未实施的能力；新重构链引入或本轮查实的缺陷。未实施的工作包不能冒充已覆盖，也不能只因旧文件还在就断言存在新的安全漏洞。问题须给出触发输入、真实调用链、错误副作用、处理及执行证据。

## 真实执行覆盖

| 操作/模式 | 当前实际路径 | 尚不能声称完成的部分 |
| --- | --- | --- |
| 原生Responses、Chat、Messages HTTP | 三个handler根调用RunNativeHTTPOperation；BindNativeHTTPDispatch/BindNativeHTTPOperation；同一dispatcher授予permit；原journal终结 | 所有变体、失败/取消/恢复和最新维护修复的组合验收仍在进行 |
| 原生客户端WS | 每个lane/turn绑定独立scope、budget与core；同步Finalizer、Finish后ReleaseTurn | 最新真实consumer批次仍有诊断覆盖和HTTP bridge结果问题；跨流/断线/续接组合未全通过 |
| HTTP→原生WS | 沿HTTP的原core/Execute，不能重新Begin或把OpenAIWSMode当成委托账务 | synthetic-parent恢复及真实socket lineage的维护修复仍在接入 |
| BPS | 纯转换产事实/repair demand，应用driver执行；既有授权与整轮预算保留；新continuation已有服务消费者 | 完整SendPermit、HTTP/WS跨池恢复正文及共享入口接线未全部完成；WP11/18不作通过结论 |
| Cloud RPC | actor/thread/turn/工具等待与原账务归档仍是实际执行者；分批/期限/owner等维护修复分段适配 | 终态公共policy、四向历史/目录及新核心垂直集成未全完成；WP12/18不作通过结论 |
| Embeddings、alpha/search、媒体、实时及其他平台 | 仍有既有handler/service路径；alpha/search共享turn预算不等于新Operation已接入 | WP13–19仍有planned项；前三个HTTP入口的回执不能推广为全平台已迁移 |
| 生命周期与停机 | 已迁移host owner显式Start/Drain/Join；超时后保留可再次join句柄 | 各consumer和真实容器TERM仍须结合最终装配验证；构造可编译不等于所有生产者可排空 |

## 按完整请求检查的场景与处理

| 边界 | 必须覆盖的组合 | 处理归属与判断依据 |
| --- | --- | --- |
| 输入/规范化 | 缺省/null/空数组；原生input与非法messages并存；未知扩展；重复字段；大整数；密文；图片；namespace | 原生字段优先，转换在明确目标边界执行；不以白名单默删未知内容。原文、有效正文和最终wire的指纹用途不同 |
| 路由与权限 | user/key/group/model改变；Composite映射；模式撤回；OAuth/PAT/Excel-only；排队期间变更；DB/Redis不可用 | 准入与发送前复核使用现有权威；明确拒绝与依赖故障分开；缓存快照和上游同名code不能授予许可 |
| 续接归属 | 原生/BPS/Cloud四向；HTTP↔WS；显式previous/合成增量/全量历史；账号失效；store=false旧socket；未决工具 | 已知owner失效不退普通选号；producer scope不能从当前store猜。只有完整可证明历史可解除旧锚点，活动工具RPC留原owner |
| 准备/发送 | 附件、catalog快照、warmup、repair、compaction；redirect；TLS前失败；GotConn后撤权 | 准备副作用与生成attempt分开；最终编码在seal前；原Operation拥有总预算与一次性permit，provider不隐藏重试 |
| 恢复 | 明确推理前拒绝；发送后接受未知；同frame错误后有usage/文本；已输出；未决工具；预算耗尽 | 使用完整发送/消费/交付事实，不能只看错误码。共享原owner/target/等待预算，禁止清零预算或删历史制造成功 |
| 输出/发布 | JSON短写、SSE flush或WS失败；终态未交付；output_item.done后EOF；下一轮已排队 | 输出项结束不等于响应成功；continuation仅在成功交付后发布；旧轮回调不能覆盖新轮terminal/active状态 |
| 用量/费用 | cache-only、image-only、显式零、缺字段、partial、确认failed/interrupted、迟到usage、重复ACK、无provider ID多流 | 测量存在性与响应成功独立，未知不补零，原journal单根幂等。连接关联ID不能去重不同lane，内部money-ID不成为续接ID |
| 副作用/错误来源 | 同一失败经过actor/writer/bridge/handler；本地队列/存储故障；provider自报本地同名code | 可信本地cause、请求拒绝、provider失败分开；健康、凭据处罚、SLA、投影和账务事实不能互相覆盖或重复执行 |
| 取消/资源 | 上传/排队/握手/读写/repair/RPC/工具等待取消；owner失效；停机；迟到返回；join超时 | 停新副作用，Cancel+Join已有任务；消费按原journal/reservation/归档合同处理，不能把取消当作无消费证明 |
| 大小/性能 | wire/解码/缓存/规范化、zstd窗口、单RPC/累计历史、事件结构开销、长SSE分片、多流背压 | 预算在预读前确定，分批不切原子语义、不重放成功前缀；不增固定槽位或独立条数门槛替代真实资源预算；基准注明实现/单位/样本 |

场景复用CONTRACTS、VALIDATION及test-cases.json，不构成新平台或产品开关。结论必须到达真实调用点，只增helper、字段或测试文件不算闭环。

## 本轮已发现的新链断点

| 编号 | 触发与影响 | 处理及当前证据 |
| --- | --- | --- |
| GW-01 | WS独立result账务ID被HTTP resolver用连接级client/local ID覆盖，多lane可能错误去重 | clientWS保留resultID，无结果时用真实OperationID冻结；独立lane与重复callback测试已加入，最新service组该测试通过；bridge完整链另有GW-04失败 |
| GW-02 | 新事实链未传image input/output或完成图片计数，只改HasTokens仍会丢消费 | 已扩展既有UsageFact/clone/decoder、core校验与NativeEvidence，仍用原builder计价；139项core-race通过，图片/adapter回归进入77项service通过组 |
| GW-03 | 拒因保留回归失败，但夹具没有发送真正触发persisted-item修复的错误 | 已改为该真实错误形态，保留“不安全修复被拒、只发一次”断言并通过。属于夹具触发偏差；不能将最初通用错误直接算成生产覆盖缺陷 |
| GW-04 | WS→HTTP bridge把客户端transport与封定上游transport硬等比较；实际在发送前拒绝，sends=0/Attempts为空 | 已确认生产断点。只允许同Operation登记、实际准备过的HTTP identity进入同authority复核，不改不可变root/fingerprint；组合补丁已落，正负例待复测 |
| GW-05 | 测试夹具漏host admission，首轮前断连；新增维护回归仍用旧领域类型/Stop签名 | admission/严格journal与两处旧测试接口已修；入口/Ops82项通过。后续handler39项通过，仅并发未执行review测试文件漂移，不代表全树冻结 |
| GW-06 | principal撤权与resource/代理/wire policy变化共用cause；handler把后者也当客户端403并可能关连接 | 已增加由真实authority标记的私有provenance，资源故障不误撤客户端权限；35项authority定向回归通过，同批一个未执行WS测试文件变更，不能据此宣称全树冻结 |
| GW-07 | Native已有Final测量，但原builder前置依赖失败时在recorder.Record前返回，事实尚未持久化 | 已定位调用顺序。不能简单把Final＋空command写成UsageRecorded；须保留原事实、冻结结算意图及未决状态，再证明原journal可恢复，不降级事实或造回执。设计/回归处理中 |
| GW-08 | simple usage不产金额command，原始测量映射校验只在有command时执行，可接受前一cycle的过时结果 | 已让现有raw mapping校验同时消费非计费映射，不造金额command；含当前/旧cycle不同计数负例的13项adapter回归通过，输入稳定 |
| GW-09 | 新decoder一次Read只返回最后FactDelta，响应归属校验结果可能随网络分块变化 | 私有有界队列已依逻辑事件观察，归属检查先于该批wire，单reader保持；one_read/one_byte_reads等原失败场景后续回归已关，仍需最终整合来源复验 |
| GW-10 | 未知扩展中同名usage或response.*命名空间会污染核心事实，终态尾部还可覆盖已选快照 | 已由实际encoder固定上游family，已知协议事件才提供事实，未知扩展原字节透传；namespace负例先失败后修复。不能按前缀/普通key授予计量权威 |
| GW-11 | 只在业务Completed时设Final，却又能把此前水位在缺usage的Completed上提升Final | 已区分终态与终态当帧最终快照；三个Forward消费者的非成功Final、裸error pending和cache-only回归通过。非成功仍失败，不能据Final允许重放 |
| GW-12 | Cloud terminal后迟到usage直接返回，或Redis已有100后新的200始终保存失败而缺少独立耐久事实 | 已定位既有actor/归档消费缺口，按原owner/turn/root进入原journal/事实归档；不重算已结算金额、不授权自动补差或第二账本。计划实现与故障回归待完成 |
| GW-13 | Operation结束只改状态，仍持有活control和已颁permit，可能再派发并把终态改成Preparing | 已在唯一Dispatcher加入私有退休gate；Finish同时禁新Admit及旧Consume/Revalidate，不用取消客户端表示正常完成。issued/consumed/authority等待三组在内的36项core-race通过 |
| GW-14 | 原prepareOpenAIUsage还写调度cache观测、重置403计数；发送前用假零值捕获，或历史恢复重放，会增加错误副作用 | 已从可重复prepare中移出；Native真实Final完成边界只观察一次，Cloud原live terminal owner按当前grant归属处理，worker重试不清新403。22项定向及后续203项组合回归覆盖；不能以此声称整个价格/intent完成 |
| GW-15 | 新main再次修正Cloud/BPS开关与独立操作能力关系；不持续检查快照会再次遗漏 | 已观察c5af并审查delta，account能力及search/image/live测试已接；模式开关不当所有endpoint总能力，也不扩大Cloud客户端协议范围 |
| GW-16 | BPS repair driver把预算耗尽/发送前拒绝的typed cause包装成连接/协议错；普通repair HTTP拒绝诊断还可能沿首个成功请求身份 | 旧main也有相同路径，属于扩大审查查出的现存问题，不冒称漏抄或重构新引入。保持cause、区分repair status/request ID与货币根，独占BPS切片实现/回归中 |
| GW-17 | request/local故障在健康统计前虽被跳过，却可能先以错误cause结束原probe并加cooldown | scheduler统一早期可信分类，Report/Observe共用；保留provider同名code负例、实际原probe释放与下一owner隔离。共享归因切片回归待执行 |
| GW-18 | 单请求大小有界，不等于多请求、continuation解码、repair派生正文的总内存已受同一owner约束 | 已定位BPS恢复在admission前解码/拼接、派生body未重计容量；属于WP08/11资源准入尚未闭合，不宣称已实测OOM。后续使用同byte owner覆盖真实分配/取消释放，不能逐端点新开一套限流 |
| GW-19 | Composite中间件改写working model后，Native fresh复核用上游名称代替原public alias；有效alias误拒，alias撤回后仅wire名称获准时仍可200 | 实际三个HTTP入口先复现6子场景失败，再从可信route decision读取原public identity授权。最终7项handler回归通过，仍发送正确上游模型；不改真实路由或加白名单 |
| GW-20 | 真实APIKey.GetByID只预载User/Group，漏掉User.AllowedGroups；新authority对专属/受限公开组错误拒绝，假仓储手填grants掩盖 | 真实PG两类授权先失败，再让同一GetByID加载canonical grant IDs；有效grant与之后撤回的3项PG回归通过，不引新查询入口或放宽授权 |
| GW-21 | root阶段才hash当前Body，中间件已preread/decode/改写时却标成OriginalWireDigest | 最早route限额旁单读capture，root删旧wrapper并拒未知；同时修内层n+EOF被外层限额拒绝仍known的错误。45项actualrouter/单读证明回归通过；到最终journal完整证明仍待集成 |
| GW-22 | 原builder用total_tokens证明独立reasoning后把raw output2归一化为5，Native证据直接与5比较发生冲突且journal前失败 | 四个实际Forward/encoder/permit/Finalizer路径已复现。保留raw Output并增加可选Total/Reasoning证明，复用原xai函数验证canonical映射；public字段/映射已接，producer及组合回归进行中，不再算一次reasoning费用 |

组件通过不证明BPS/Cloud、所有平台、最终Docker或752项前端用例已验收。raw/normalized、cache alias、图片tool scope等测量问题继续逐producer核对；独立Voice/alpha-search/Grok计量尚未由本Native核心接管，不能仅凭核心缺字段断言这些旧计费已丢失。

GW-23（新确认）：影子账号已选定后父账号共享凭据冷却或到期，HTTP实际Consume原来仍可发出；先复现2个叶子失败，再在共同loadResource复用IsCredentialUsableForShadow，HTTP deadline并入父账号期限。203项组合覆盖Consume/heldWS/原授权负例，父手动调度和global429保持独立；真实PG另验证父状态/成员投影及普通Supply检查边界。

GW-24（新确认）：Native simple/log_only原以Final无command标usage_recorded，再尽力插usage_logs，回执不能证明日志落库。现用原UsageLogOnly零资金command进入同journal/dedup/log事务；原倍率统计保留，余额/配额金额全零，SQL失败不旁路写log。22项adapter定向通过，真实PG重复调用只1日志/1dedup、余额与Key计数不变；不泛化清理旧usage_recorded历史。

GW-22状态更新：HTTP producer的62项定向race已通过；203项组合又覆盖ctx_pool/passthrough/http_bridge的实际WS消费者共9叶子，raw2/total10/reasoning3与canonical5分开保存，已包含不重复加、仅辅助字段不结算。R6主图片显式0与hosted工具正值的不同计量scope仍在复现，不用此通过数关闭另一问题。

## 2026-10-27 BPS故障与恢复规格复核

以下是[CT-02新确认需求](CONTRACTS.md#bps-failure-recovery-20261027)对当前工作树的源码核查，均待实现/运行验证；不是本轮已修复或已复现的测试缺陷。不能因为保留了403 worker就声称账号故障处理完整。

| 已核实的实现差距 | 消费点及验收要求 |
| --- | --- |
| Excel刷新失败原因合并 | `openai_excel_oauth.go:excelAccessToken`把刷新错误合并为OPENAI_EXCEL_AUTH_REFRESH_FAILED；`getExcelBPSAccessToken`缺少provider依赖也可能返回AUTH_REQUIRED。需由凭据owner保留可信原因，区分权威缺失/撤销、可刷新拒绝与本地依赖/网络故障，不能据统一错误批量停用；TC-ACCESS-04 |
| 429 scope与持久性不完整 | `openai_excel_bps_ratelimit.go:coolDownExcelBPS`按account.ID写进程sync.Map；附件429也可进入该路径，尚不等于按实际模型/端点/配额scope暂停。`isExcelBPSRateLimitBlocking`仍受原生fallback能力影响，须随取消回退一起修改；TC-EXEC-02、TC-CONTROL-04 |
| 有效上游等待时间被截短 | 同函数将有效Retry-After也截到maxRateLimit429CooldownSeconds（当前7200秒）。需将fallback范围与真实not-before分开，覆盖长Retry-After、HTTP日期、重置时间、短信号不能覆盖长冷却及重启；TC-EXEC-02、TC-CONTROL-04 |
| 恢复SQL的身份条件尚不足以证明新合同 | `account_repo_excel_bps_recovery.go:excelBPSRecoveryGuardSQL`比较主credentials、proxy及BPS extra，没有直接比较独立Excel grant或账号分组关系；需在实际探测前/恢复写入边界证明这些变化不能被旧结果覆盖。现有测试保护部分设置与主凭据，不能推广为全部身份覆盖；TC-CONTROL-04 |
| 管理员再次关闭及恢复失败类型需实测 | `MergeExcelBPS403Marker`会保留false状态的旧服务端标记，必须结合真实管理更新消费者覆盖“系统已关闭后管理员再次明确关闭”。probe目前将非200合并为rejected，需保留429时间、授权失效与临时故障原因，防止密集探测；TC-CONTROL-04、TC-HOST-06 |

新合同保留403自动关闭开关及条件恢复，不把它改成只暂停路由；增加账号授权失败停用、局部限流和需重新授权的明确分类。旧BPS→Native回退的删除、状态分类及后续请求选路须作为同一WP-11/WP-18切片验收，当前任务状态不因文档更新提升。

## 已核对并排除的误报

| 线索 | 实际链路与结论 |
| --- | --- |
| Native核心没有Voice/alpha-search/Grok搜索字段 | 这些专用操作仍有原producer、原计价和原journal消费者，当前未被Native Finalizer接管；是完整迁移范围，不是仅凭字段缺失就能证明当前漏扣 |
| 裸error带实测usage却没有最终usage_logs | 新链已耐久保存Partial/PendingUsage且不重放；不能为满足旧日志假设提升Final或制造零账。两条新回归已纠正预期，并继续断言数量/独立root/零Apply |
| 资源采样使用context.Background | collectSystemStats内部实际调用maintenanceTaskOwner.attach，I/O仍合入真实owner取消/Join。不能只按一行Background认定Stop失效；后续重构仍须保留这条真实消费者 |
| continuation提交失败后保持pending | 保守不发布未确认历史是正确边界；问题在最终提交结果和恢复职责不可观察，不能以“恢复成功”为由发布未确认快照或重新生成 |
| 复用客户端X-Client-Request-ID就能让普通HTTP免账 | 实际gateway middleware不采信该header为内部金额关联ID，而逐HTTP生成UUID；不能从resolver文字直接认定可攻击。WS同连接继承关联ID是不同已证场景，仍需独立turn与晚绑定 |
| GetByID的IP编译字段或软删除仅凭实体字段判断失效 | 实际APIKeyService.GetByID重新编译IP规则，Ent soft-delete Traverse默认过滤删除实体；这两条不能因仓储局部未赋值便报漏洞。grants漏预载则是GW-20实际PG证明的不同问题 |

## 修复顺序与收口

先修已复现的身份、测量、投影和结果丢失；并行补维护遗漏，再完成BPS/Cloud及其他工作包的真实消费者迁移。核对新入口、执行路径、持久字段和临时兼容的实际读取者，替代旧路径同次删除，不增长期双核心。

每轮保留失败→根因→改动→受影响复测证据。开发完成后以同一源码/镜像执行完整Docker、页面和集成验收；外部条件不足明确记录，不用通过数量掩盖缺口。整个重构验收完成前不stage/commit/push，不改维护分支。

## 2026-10-27 独立二次复核：Native新执行的发送前意图

用户要求本会话落实最新BPS规则并在“分析统一协议桥接方案”（01a17902-4afb-7ce1-925b-78c2a6f0211c）交付后独立复核。该会话当前在同一工作树拥有BPS路由、授权停用/恢复、429及对应测试写集；本集成者不重叠修改，独立复核与Native阻塞修复登记于work-items.json的WP-18-BPS-INDEPENDENT-REVIEW-20261027及其关联切片。以下为源码已证断点，尚不代表运行修复通过：

- Native WS handler的bindNativeTurn只有Dispatch/Operation/Publish，缺少BindNativeIntentPricing；实际WS/HTTP桥的preserveNativeIntent均拒绝缺少intent，故BPS停用后的独立Native WS新请求也被阻断。必须从逻辑turn的已认证key/group、映射和同一pricingAt绑定，重试不重新冻结。
- sendNativeWSFrame构造nativeWSPreparedAttempt时漏pricingFacts，真实capture要求非空Model/UpstreamModel；修handler仍不足以发送。适配器必须消费实际封定wire与producer已有billing/image决策，不能凭slug造计价规则。
- handler在Native Finalizer前给缺上游RequestID的实测usage补随机openai-ws-usage-ID，与技术intent/货币身份分域不符。Native应保留缺失canonical的pending事实；现有delegated模式的日志规则独立处理。
- 旧Native服务/handler测试仍存在旧回执接口、未保存before-send scope、以及只拦截旧RecordNativeUsageEvidence的fixture。正在迁移实际端口，并保留释放/取消/幂等断言；内存fixture不是PG事务证据。

本轮早期两次service定向执行均停在整包编译阶段（BPS活动切片旧测试引用、未使用import和Native图片回执接口），业务命中0，不能记录为通过。回执.local/rebuild-wave4/bps-review-native-fixtures*.json/log；第三次在共享编译锁后执行。待对方交付稳定写集及真实测试结果后再执行BPS状态/续接/多实例/存储独立复核，未将整个WP-18标accepted。


### 2026-10-27 补查：BPS隐式全模型兼容残留

独立核对用户“只走勾选模型”与实际Account/目录/probe消费者，发现缺失openai_excel_bps_models仍被isExcelBPSAllModelsEnabled和isExcelBPSUpstreamModelEnabled解释为全选，恢复probe也默选DefaultTestModel。此前取消单次Native回退和403恢复的定向验证未覆盖这项旧行为，不能视作完整统一方案通过。

主集成已删除该旧分支及唯一消费者，普通/图片选路只匹配显式列表；无选择时双授权目录直接读取Native，Excel-only目录为空，恢复任务不发送未勾选模型。没有新兼容开关、自动数据迁移或静默填充所有模型。已同步前端owner，加入实际HTTP JSON/SSE/WS双授权/Excel-only发送目标、目录网络调用和恢复空scope用例，等待共享锁后执行。原独立Images格式回退仍保留。

对应WP-18-BPS-EXPLICIT-MODEL-SELECTION-20261027与TC-EXEC-02；完整执行决策/所有适配器退出旧路径、Docker与逐页验收尚未完成。

## 2026-10-27 收尾复核与实际修复

本轮主集成已把HTTP Forward、WS逐轮payload与bridge、续接和Responses候选筛选接到`responseExecutionPlan`。候选规划不授予发送权；旧执行预算继续约束BPS pin，真实grant/owner/lease在发送边界复核。WS已经映射的body不得二次账号映射，发送前取消检查改为直接消费本轮plan。旧`responseModeForAccount`分流退出。

实际新增发现及处置：

- 本地输入拒绝后service读下一帧会复用firstPlan/原准入。ctx_pool/http_bridge/passthrough改为返回request/not-sent/stop失败，原handler结束本轮并重新认证、映射、绑定预算/价格与slots。passthrough新组暴露的出站账号映射遗漏及次轮channel alias选BPS问题已修复：出站统一使用原模型映射器，次轮消费同一执行计划和预算准入。最后12项与37项定向race组关闭这些已发现问题，原失败记录保留。
- 系统BPS停用可能被旧true草稿覆盖。full/Extra/bulk与供应商更新已接入原事务行锁条件；安全DTO与请求条件见CONTRACTS，真实PG竞争、回滚和后续probe失配通过。旧bps_all_models行为不再清空勾选列表，改为明确拒绝。
- OAuth应用入口原吞掉Extra冲突。现凭据与Extra经专用原子用例复用原UpdateAccount/UpdateAccountExtra事务、权限与supply identity检查；冲突整体回滚。成功/提交确认不明的token缓存失效和清错副作用分别处理，真实handler/实际service加事务替身测试通过；不把sqlmock称为PG提交证据。
- Cloud整数工具参数原保持7500.0导致严格整数客户端拒绝。投影使用精确十进制运算，短指数仅允许在int64/uint64可消费范围增长，custom input和fraction保持；完整工具publication在发身份/参数前核对整个result和event envelope。16MiB边界、已有输出/usage/metadata/转义、工具回复/重放与单根账务测试通过。
- Native HTTP桥SSE内部401/403不能证明生成未发生，现保留终态/用量及账号观察，不转成自动换号重放；流前HTTP401刷新继续走原有预算。原先漏出的UpstreamEndpoint已沿既有Responses端点记录补齐。
- BPS合法长Retry-After已持久保存，但对客通用七天限制造成响应头丢失。BPS失败现使用同一绝对截止解析器复核输出，其他模式保持原边界，相关正负例通过。

源码和精确测试回执见TEST_REPORT的closeout各组；436pass为受影响网关组合的稳定源码结果，之后passthrough改动由12/37组补验证，不能把重复测试事件相加为唯一覆盖。最终37组4428个后端输入与本机已部署3a0638612e05快照逐文件相同，13项隔离Docker API验收通过；这仍不是全站753用例或真实上游验收。未stage/commit/push，未改维护分支或真实上游。

### “分析统一协议桥接方案”最新稳定交付的独立审核

已读取`.local/cloud-retention-20261027/changes.patch`、`verification.json`、`green-v4.json`及实际生产消费者，而非仅接受会话文字结论。审核时七个交付文件SHA全部等于当时源，也全部存在于本集成者`closeout-gateway-final`冻结输入中；后续并发修改不能自动继承这份结果。

复核确认：新root/缓存替换和驱逐只释放实际编码回放副本的字节，消息、hosted输出和tool history继续单独记账；活动压缩在中断前检查工具结果/提示词大小，中断排空后的迟到输出再检查提示词空间。第二次检查失败时保留已确认的实测lower_bound，不对已结束turn再次interrupt，也不启动summary。未增加新的上游执行路径、配置或公开接口。

交付方green-v4在相同来源上有132pass/0fail，race通过，含`TestCloudCompactionLateHistoryLimitPreservesConfirmedUsage`；主集成436pass组合独立覆盖相关工具/回放/续接/取消和缓存生命周期，源输入一致，最终37组也独立执行迟到压缩用量场景。有效证据按实际输入复用，不再次复制或编译同一切片。本次稳定交付的审核与受影响复测闭环，重复轮询此交付的定时跟进已删除。

### 已部署快照与下一轮审核边界

API/main/vendor已部署本机隔离Docker，实际ID和来源见`.local/rebuild-integration/api-unified-20261027/deploy-receipt.json`；数据库/Redis/TLS和测试数据保留。API13项认证/权限/脱敏测试及电脑登录拒绝、未登录路由重定向冒烟通过。中文登录页英文错误提示已交前端owner修复，不能将冒烟计为完整页面用例完成。

另一会话此后仍在写入跨流续接、缺失模式索引和Cloud跨实例预算/owner转交。部署回执记录了当时相对API快照的39个变化路径，它们没有包含在当前镜像，也没有由本轮测试整体验收。新的稳定切片须重新比对真实消费者、模式归属、取消/账务和共享存储边界。完整架构、协议场景与753项页面验收仍保持未完成，不能以本节已闭环的问题替代后续审核。


## 2026-10-27 实际控制面并发与后端剩余迁移

两个独立Docker API进程共用隔离PG/Redis，管理员与供应商同停用条件并发重开均只有一方成功，另一方409且metadata回滚；跨进程读取可见。13项基础/双实例控制面用例通过，精确来源、清理见`.local/remaining-closeout-20261027/bps-two-gateway-control.json`和`replica-runtime.json`。这是控制面CAS的实际M级子集，不是后台恢复probe、Cloud所有权交还或真实上游证明。

新增独立生命周期审查的两处遗漏已修复：余额成功变更后即登记有界缓存失效，覆盖随后读失败；支付成功通知由maintenanceTaskOwner保留至排空，在支付扫描之后、订阅与存储之前关闭。实际Wire生成、36项race组通过；独立支付审查未发现阻断缺陷，说明见`payment-independent-review.md`。通知仍沿既有best-effort政策，join不等于保证投递成功或恰好一次邮件，不改变已经完成的资金事务。当前API3a尚不含该新切片，不能称Docker已验收。

其他provider实际审计见`provider-migration-audit.md`：Anthropic/Gemini/兼容供给、jobs/realtime的旧控制路径仍有真实消费者，不因目录缺失判业务不存在，也不因共用Recovery/容量组件判迁移完成。当前推进TypeSafe SystemOne专属完整切片；新增真实provider/执行需与原持久journal和唯一恢复callback接通，随后退出旧handler/service链，尚未验收。

## 2026-10-27 SystemOne 共享 finalizer 实际接管

已纠正早期临时接线：仓储不再另写 SystemOne 事务或仅因保存了价格 intent 就返回 usage durable。新方法构造实际 attempt observations 后调用共享 FinalizeNativeUsageIntent，锁内比较完整 scope、协议与各 attempt digest，绑定 canonical 后使用既有 ledger/dedup。恢复仍由同一 usage worker 按 protocol 分派，unknown/缺原价/缺 ID 分开保留。历史可选字段 omitempty；Final 后迟到未知观察保持原投影。首次 race 抓到解码浅拷贝写竞争，已修复，六组实 PG/race 全通过。独立 reviewer 对最新消费者、scope、Claim 和幂等链没有发现新的阻断项；实际 Docker HTTP known/unknown 用量账本核对通过，替身与真实上游边界见 TEST_REPORT。

旧 `ForwardSystemOne`、`SystemOneForwardResult`、`recordSystemOneUsage`、handler 私有重试与 `SystemOneExecutionReady` 已删除；原有效 HTTP 错误/URL 策略/内容类型/usage 断言迁入新服务消费者。WP-13 中的 Anthropic 仍未因此完成，WP-14至17/19等范围也未获提前验收。

本轮另修复 non-frozen 混合图片目录token计价与 Ops metrics SQL 参数类型问题，来源为实际失败而非静态猜测。Cloud 最新归档修复已完成独立源核对与消费者/实 PG+Redis 补验，详见 `.local/systemone-closeout-20261027/cloud-archive-independent-review.json`。公开模式消费者历史失败继续单独复核，不以已通过的控制面、SystemOne 或归档测试覆盖它们。

### 剩余范围重新核对

当前仍有实际生产消费者等待迁移：Anthropic Messages及兼容入口，Gemini/Antigravity生成和cachedContents，非OpenAI文本平台，Embeddings与Alpha Search，sync/async/batch图片与产物，Grok/Seedance视频，Grok Voice/Realtime及Codex Live。它们分别保有handler/service控制或后台模拟请求；共用Recovery/容量并不等于执行所有权已切换。已将具体范围写入work-items各父项的latest_verified_remaining_scope，不让用户逐项重新确认。

统一模型管理是MAIN的后端依赖：前端已交 `frontend/docs/UNIFIED_MODEL_MANAGEMENT_HANDOFF.md`，但12条新API、独立CAS和运行时目录/价格/allowlist读者尚未接通，不能标成单纯“等待前端”。同角色同制品两个品牌profile的实际验证、全部页面与最终Host/旧路径收口仍保留开放状态。下一较窄的执行切片为Embeddings；此记录不是其实施完成证明。


## 2026-10-28 CPR增量整合与同源复核

用户指定会话最新深挖研究已按当前重构实际消费者复核；采用/不采用理由和版本矩阵见 [CPR_INTEGRATION](CPR_INTEGRATION.md)。落地范围：已接纳请求计划一致性及发送边界资源读取；目录scope/generation和三态证据；Redis普通用户/账号FIFO ticket、取消及共享等待期限；Ops连接预算、统计缺口与诊断完整度。未引入另一套执行器、目录、插件Runtime或账本。

扩大验证另修真实桥接：Messages/Chat向原Native意图传入原始/上游/计价模型；模型事实读取实际持有正文而非不可信GetBody；WS复用绑定目标、代理和凭据owner代际；取消采集不继续SQL查询。显式group_id=0仍拒绝，权限未放宽；旧夹具已按当前合同修正。

冻结来源 04b36ed10bd01e6cd0ed2802ed6ed480b4341692584d13b02cf963035693a636，六包2061个测试/子测试通过，定向race294通过，真实Redis13通过事件，均0失败/0跳过。真实PostgreSQL取消和连接池复用计入unit/race，覆盖重叠不相加。Root Dockerfile构建56.901秒，候选 sub2api:cpr-review-04b36ed10bd0，image sha256:617cf758a569c5f7015772a04a263e7b2a1449cddfd37bd80e7e82b118e56599。隔离容器内health/public settings/login/auth me四项200；没有真实上游生成，未接受运营协议，不代表全站页面或整个重构验收。

回执 .local/cpr-integration-20261028/verification.json 与 docker-smoke.json；命令、输入、失败修复记录、压缩日志同目录。临时API/网络/Redis/PG及测试卷清理，候选镜像与精简验证/恢复资料保留，见 resources.json。共享Go缓存超限且有其他任务，未不安全全机清除。

人工验收项目 sub2api-rebuild-acceptance-20261027 未替换，仍由 .local/manual-acceptance-20261027/environment.json 指向。新增诊断字段已交接frontend owner，MAIN未修改frontend，无Git暂存/提交/推送。
