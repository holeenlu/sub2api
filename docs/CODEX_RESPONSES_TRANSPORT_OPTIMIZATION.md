# Codex Responses 传输与增量续接优化方案

日期：2026-10-21（Asia/Shanghai），第 4 版。性能判断以第 16 节为依据，最新本地实施和验收以第 17 节为准；第 2、3、5.5、11–15 节保留注明日期的历史证据。受控 HTTP→WS、连接池和精确端点 zstd 已有实现，本轮接入跨传输 timing v2、普通 HTTP 的被动增量评估及离线汇总。线上样本、配对性能验证和 P4 自动裁增量尚未完成。测试平台 S0–S4 继续保持撤销状态。

## 1. 目标与边界

减少可验证的请求上传字节、连接建立开销和多轮工具调用延迟，同时保持租户隔离、响应归属、上下文完整性、原生恢复与用量记账。收益以最终出站请求及客户端可见结果衡量，不以“模拟官方”“无降智”或“无封号”为验收标准。

本方案属于 KDAN / TapModels / Tokensavy 共享网关能力。2026-10-19 的实现及归并交付已完成；2026-10-21 本轮范围是复核截图、当前方案与代码并完善后续优化设计，不据此前交付授权继续推送新改动。未读取生产配置、连接生产数据库、发出真实供应商请求或改变部署；保留工作区其他任务的调度改动。

第 12 节是已撤销测试平台的历史设计，不是当前待实施清单。本轮沿用已有观测、转发与测试入口；客户端兼容画像、真实设备标识及遥测分别处理，不把对外合成遥测或虚构仓库活动作为延迟优化方法。

## 2. 方案制定时基线（2026-10-18 历史快照）

以下“当前”均指当时快照；HTTP→WS、请求压缩等后续已实现，现状见第 4、16 节。

开始检查时 HEAD 为 e80eb7ca0；工作区另有 SSE、metadata 编码器及会话身份相关未提交改动。本方案不把这些未提交改动当成已经验收的能力，也不重复实施或覆盖它们。

- 当前已有 Responses WS 客户端、连接池、previous_response_id、严格增量判断与恢复历史。需按实际调用路径区分 WS ingress、池化 WS forwarder、WS passthrough 和 HTTP/compact，不能概括为 Sub2API 永远全量、永远 SSE。
- openai_ws_client.go 启用了 CompressionContextTakeover 协商；是否实际协商成功和节省多少字节需要测量。
- HTTP transport 的 zstd 依赖与解码逻辑属于响应解压，不能据此声称请求上传已使用 zstd。
- 当前普通 HTTP 入站仍经过 resolveOpenAIWSDecisionByClientTransport 保持 HTTP 上游；HTTP→WS 属于新的传输策略候选，不是单纯修一个字段。
- 当前 HTTP 入站的请求体处理：客户端发来的 zstd/gzip/deflate 请求体在 pkg/httputil/body.go 解码后，以未作 Content-Encoding 压缩的 JSON 重发上游；这不表示关闭 HTTPS/TLS。两跳 WS 均配置 CompressionContextTakeover 协商 permessage-deflate，实际是否采用及字节收益仍需观察。
- 当前 WS ingress 默认 ctx_pool 模式：正常连续轮次复用所持有的上游连接，lease 55 分钟轮换；连接故障、归属或有效配置变化等仍可能触发更早重建。保留客户端 previous_response_id/增量语义不等于字节原样透传，现有身份/协议规范化仍会执行；恢复历史属于现有进程内会话状态。
- 官方源码基线固定为 5.5 的提交，本机另经命令确认 codex-cli 0.159.3；主干静态核验不能自动证明该安装版本逐行相同。源码中的 cached_websocket_session 是会话级缓存，跨轮次保留；前缀比较有六类未命中：no_previous_request、no_previous_response、no_previous_response_id、非 input 字段变更、input_shortened、input_mismatch，连接/认证重置还有独立原因。**实际激活 WS→HTTP fallback 后，同一客户端会话后续轮次继续使用 HTTP；一次 WS 故障不等于必然激活 fallback。** 所核验客户端的 HTTP 路径发送当前完整逻辑请求；不能推广为所有 HTTP 客户端都不使用 previous_response_id。

## 3. 初版截图：仅作历史描述性统计

本节是 2026-10-18 旧两图；2026-10-21 新三图见 16.1，不混用样本。原始数据来自用户当时两张截图，由人工逐行读取。图片没有提供相同任务配对、模型/effort、账号、并发、代理、时间窗或 TTFT 的精确定义。不得把这组统计当成 WS 或任一补丁的因果效果。

| 指标 | 优化前截图 | 优化后截图 |
| --- | ---: | ---: |
| 请求数 | 5 | 8 |
| 首字延迟中位数 | 22.81 s | 3.45 s |
| 首字延迟均值 | 25.06 s | 4.82 s |
| 首字延迟范围 | 12.38–43.06 s | 2.34–14.88 s |
| 总耗时中位数 | 28.11 s | 5.40 s |
| 总耗时均值 | 28.97 s | 7.49 s |

截图首字中位数之比约 6.61，描述性下降约 84.9%；这是不配对、不同样本的比值，不是已验证的加速倍数。优化后 14.88 s 的慢样本必须保留。两图输入、输出及缓存量不同，缓存 token 数也不等于实际上传的历史字节数。

逐行首字/总耗时（秒）：

- 前：(12.38,16.20)、(26.53,28.11)、(20.50,24.58)、(22.81,30.95)、(43.06,45.03)。
- 后：(2.34,3.03)、(2.91,4.13)、(3.49,4.19)、(3.84,4.83)、(3.41,5.96)、(3.04,9.06)、(14.88,21.57)、(4.63,7.13)。

## 4. 能力现状（2026-10-21 复核）

已提交基线为 `c5d1b1a64b0282df936c53ee38c6287eafde570a`；另有未提交调度改动，不视为已上线。源码存在也不证明截图环境已部署或启用。

| 路径/能力 | 实际状态与入口 | 后续工作 |
| --- | --- | --- |
| 原生 WS ingress | `ProxyResponsesWebSocketFromClient` 保留客户端 previous_response_id、恢复历史、线程/凭据归属与串行轮次；合法增量已支持 | 度量状态/连接复用与恢复，不重复裁剪原生增量 |
| HTTP→WS | `Forward` 已允许普通 OAuth-like Responses HTTP 请求走已有 WS forwarder；API-key HTTP、compact、passthrough 保持 HTTP，还受全局/账号模式控制 | 核验部署 SHA、有效模式与每次 attempt 实际传输，入站“流式”不代表上游 SSE |
| WS 复用 | `forwardOpenAIWSV2` 经 response/session 绑定选连接；池已有空闲预建、reader-loop 保活和失效处理 | 查 reused、排队、握手、conn age、关闭原因，不另造连接池 |
| 请求压缩 | `compressCodexResponsesRequest` 对 HTTPS POST chatgpt.com/backend-api/codex/responses 的可回放 JSON 做 zstd；WS 已配置压缩协商 | 度量实际协商/压缩字节和 CPU；不把逻辑 JSON 长度当 wire bytes |
| 全量→自动后缀 | 未发现通用生产路径；`openAIWSRawItemsHasPrefix` 用于恢复材料合并，旧文中的 `openAIWSInputIsPrefixExtended` 已不存在 | P2 被动评估后，P4 接已有最终发送点；不按缓存率/token数裁历史 |
| generate=false | `performOpenAIWSGeneratePrewarm` 默认关闭；开启后在当前请求关键路径等预热完成才发正式请求 | 先测额外上传/等待；不把开关当无条件加速，区分连接预建与上下文预热 |
| 计时/TPS | HTTP TTFT 可选 semantic/visible；WS 按 token-event 判定；页面 TPS=output_tokens/duration | P0 统一时间轴与明确指标版本，不靠换分母或首字定义制造提速 |
| 观测落点 | HTTP 已有 `openAIForwardTiming`；WS 已记录 conn pick/queue/reused；`request_timing_details` 只有迁移未见运行读写方；`servertiming` 针对管理/用户 UI 请求 | 复用 attempt/Ops 和关联 ID 补齐查询聚合，不能因有表或 UI timing 就称模型请求闭环已完成 |

源码落点：backend/internal/service/ 下的 openai_gateway_forward.go、openai_ws_protocol_resolver.go、openai_ws_forwarder_v2.go、openai_ws_pool.go、openai_ws_forwarder_payload.go、openai_ws_state_store.go、openai_forward_timing.go；另见 backend/internal/repository/http_upstream.go、frontend/src/utils/latencyHealth.ts。

## 5. 核心设计：只对可证明相同的历史做增量

### 5.1 区分三种输入

1. 客户端已经提供合法 previous_response_id 与增量 input：校验归属与上下文能力后沿用现有路径；不再重复裁剪，也不为了省流量改掉客户端指定的父响应。
2. 客户端发全量 input，网关能够证明其前缀等于本会话上一成功轮的完整上下文，且该轮的上游连接状态仍可用：允许构造 previous_response_id + 新增后缀。
3. 无可靠父响应、历史不完整、前缀不一致或参数改变：保持自包含全量请求。客户端只发增量但恢复材料不足时，返回现有可恢复错误并要求完整上下文；不能只删 ID 再把剩余后缀发送。

第二种的逻辑可表示为：

```text
上一轮已确认上下文 H = 上一轮完整 input + 上一轮完整 output items
本轮全量 input C = H + Δ
只有 C 与 H 的逐项前缀关系已验证，且连接/归属/设置兼容：
  上游发送 previous_response_id = 上一轮真实 response ID，input = Δ
否则维持全量，或按原生恢复契约返回上下文不足。
```

不能只比较上一轮用户输入而漏掉 assistant 输出、reasoning、tool call/output、compaction 或多模态项；不能按 token 数、字符串字符前缀、相同模型/工具、缓存键或“看起来是同一会话”猜测可裁剪。

### 5.2 允许合成增量的条件

- 复用现有已认证租户、响应归属、credential owner、execution scope/thread 和连接实例边界。prompt_cache_key 不是会话或租户身份，不能单独充当状态键。
- 父响应是本轮允许引用的真实响应 ID；首版优化仅从已确认 response.completed 且输出 collector 完整的轮次建立合成锚点。其他原生合法续接保持既有语义，不因新增优化而被全局禁止。
- store=false 时，父响应所在连接的状态必须仍可用；不能仅因同账号另有一条连接就假定它拥有该状态。
- input 逐项匹配完整已确认历史，使用现有保精度的比较/RawMessage 机制。复用已有畸形及重复字段检查，不把有歧义的 JSON 归一化成可裁剪请求。
- 逐项比较采用固定版本 Codex 的 response_items_equal_ignoring_internal_metadata 语义，不另定宽松豁免：整项相等即通过；否则工具结果元数据必须相同，然后只清除内部 chat message metadata passthrough 再比较。它比较的是反序列化后的 ResponseItem，Go 的 RawMessage 哈希不能直接等同于 Rust 类型相等：须覆盖字段默认值、缺失/null、数字精度与已知 item 类型，并用同一组夹具核验结果；未知结构保守标为不可判定，不据此删除原始出站字段。
- call id 规范化是确定性的，但并非全部 ID 都走 sha256：normalizeCodexCallIDForItemType 先处理类型前缀，超长才调用 compactCodexCallIDForItemType 的 sha256("sub2api:codex-call-id:v1:" + id) 压缩。比较阶段还须同时覆盖短 ID、超长 ID、工具类型与输入/输出配对，不能仅验证长 ID 分支。
- 模型、instructions、tools、工具选择、reasoning、输出格式、服务档位等语义设置兼容；保守沿用现有非 input 比较，只忽略已确认纯传输字段，不不断扩张豁免列表。
- 顶层 instructions 和当前轮必要选项仍显式发送；previous_response_id 不等于所有请求选项自动继承。
- 同连接当前实现仍串行处理回合，不把不同 thread 或并发子代理合到一个 last_response_id。
- body、平铺 metadata、内嵌 metadata 与会话头使用同一作用域；不二次派生，不牺牲 TenantScope 或跨用户响应归属。

### 5.3 复用现有状态，不另建网关

**P2 旁路计算优先放在普通 HTTP 入口。** 官方 Codex 已在 WS 路径做前缀裁剪，因此网关对同一正常链路再次裁剪的边际收益预计较小；这是待测假设，不能由六类比较失败直接推出命中率必为零。六类并非“前四类加连接重建”，而且网关与客户端保存的历史可能不同。HTTP 路径更值得先测全量请求的候选比例，其中包含已激活 fallback 的 Codex；其他客户端的 WS 全量请求或已有 HTTP 显式续接分别分组，不混入推断。

旁路计算复用 Responses handler 到 Forward 的 HTTP 生命周期，在最终账号/配置与逻辑请求变换确定后观察请求，并在既有响应 collector 确认完成且输出完整后提交候选历史。状态键必须同时包含已有租户/API-key 归属、最终账号/凭据代际、明确客户端会话/线程及 window 边界，并绑定上游配置快照。复用现有身份解析和 execution scope；不能直接拿调度粘性 hash 当隔离键，因为 GenerateSessionHash 允许 prompt_cache_key 和内容回退。缺少可靠会话身份的请求记为 unassessable，不按同一提示词拼会话。

有界状态保存非 input 语义设置摘要、上一轮 input 与完整 output 的逐项比较摘要、必要长度/类型、完整性和顺序代际；只有确有需要才保留 response ID，不能凭 HTTP 响应 ID 推定 store=false 的 WS 状态存在。摘要生成必须满足 5.2 的类型相等和工具结果元数据约束；单个“清掉 metadata 后的哈希”不够。优先使用既有摘要机制并保护低熵内容，摘要本身不进入普通日志。只保存哈希/长度，不新增正文历史仓库；TTL、条目数、每条 item 数和总内存均有界，实例本地、不进 Redis。

同会话并发只对已完成且顺序明确的父轮评估；无法确认顺序时标记 concurrent/unassessable，不能让迟到响应覆盖新一代历史。失败、取消或输出 collector 不完整的轮次不建立完整锚点，隔离变化与淘汰不沿用旧状态。未命中区分 Codex 比较原因与网关观测原因（缺少身份、换实例、被淘汰、并发、未知类型、输出不完整），不把它们伪装成客户端六类原因。

记录三个不同结果：前缀/设置是否匹配、估算可移除的未压缩 item 数与字节、真实发送所需的连接/归属/重放条件是否已验证。P2 只有前两项的候选观测；HTTP 阶段没有 WS 连接时第三项为 unknown，不宣称已经可发送 Δ。字节估算用当前请求中拟移除前缀的实际序列化长度，并注明尚未扣除续接封装成本、未体现压缩；本阶段实际节省发送字节为零。实际仍发原请求，不多发请求、不改变出站字节，且不改变调用方时序/取消契约。

只在同一语义阶段比较：请求在模型/工具兼容变换与 call id 规范化之后、压缩与编码之前计算摘要；上游输出映射到同一比较表示，保证输入/输出 ID 和元数据处理一致。必要的观测入口复用现有变换与 collector，不能在 handler 的变换前请求上计算，再与 Forward 后的历史比较。P2 证明值得后，OAuth HTTP→WS 的裁剪发送随 P4 实现，并核验可用连接状态；API-key HTTP 原生续接保持现有契约。WS ingress 若另有真实全量样本，再独立测量，不把 P2 尚未采集的 WS 数据写成已观测事实。

现有 WS 路径继续保留有界完整恢复材料，P2 新增的是旁路摘要状态，不能用哈希替代恢复正文。P4 若需要跨 HTTP 请求保留完整材料，必须先说明现有 execution-scope / state-store 不足之处，再确定容量、生命周期、实例路由和失败行为；P2 不提前建设该正文状态层。

### 5.4 恢复不能为了加速放宽

| 情形 | 行为约束 |
| --- | --- |
| previous_response_not_found、连接状态丢失 | 仅在完整恢复材料和现有重放资格成立时，走有界全量恢复；不得删 reasoning 密文或私有引用来凑请求 |
| 模型/工具/instructions 改变、compact/new_context 窗口改变 | 不套用旧前缀；按现有窗口与压缩契约重新建链。保留完整 compaction 输出，不自行摘要或裁掉必要项目 |
| 切账号、凭据、代理、endpoint 或模式 | 重新判断归属、私有状态和可携带性；不拿旧账号响应 ID 继续另一账号 |
| 写入失败/断连且可能执行过工具 | “未见首字”不是重放许可；先走现有 uncertain-execution 与工具副作用判定 |
| 已向客户端提交输出 | 不重生成、不拼接新流；保留终态错误和实际部分用量 |
| 历史不足或恢复预算耗尽 | 返回现有清晰错误，让客户端携完整输入重连，不悄悄丢上下文 |

所有尝试继续共用原有整轮预算、首语义输出预算、取消与有界 drain。失败尝试不得成为下一轮有效历史。模型、账号和计费快照保持既有冻结语义。

### 5.5 固定官方客户端版本，避免照抄旧忽略规则

本轮直接读取 openai/codex 的 client.rs，并将核验固定到该文件最近一次提交 1cc7dc8f2acc63ce6c1cb72271e6b90a76136c2b（提交时间 2026-10-17 23:17:51 UTC，北京时间 2026-10-18 07:17:51）。这是源码比较基线，不代表当前用户安装的 Codex 版本。

- 330–341 行的 WebsocketSession 保存连接、认证 owner generation、connection key、上次请求和上次响应接收器；不能只移植 last_response_id 而省略其归属。
- 345–370 行仍有六类 NonIncrementalReason；1496–1560 行验证非 input 设置以及“上次 input + 上次完整响应 items”的逐项前缀，成功后取余下 items。六类比较失败并未覆盖全部连接/认证重置原因。
- 458–477 行的比较器先检查 has_same_tool_result_metadata，再清除 internal_chat_message_metadata_passthrough 后比较。迟到的工具结果更新已经发送过的 output 时，单纯增量无法传递更新，必须拒绝该前缀命中。不得据此实现全局删除任意 metadata、id、status 的 JSON 比较器。
- 请求属性比较有明确的字段取舍，部分授权相关内容仍按每个 response 独立处理；对网关尚不认识的字段保持保守，不能照搬“比较时忽略”来省略实际发送或授权。
- 1734–1743 行的 HTTP 请求压缩由开关、uses_codex_backend 和 OpenAI provider 三个条件共同控制。官方存在 zstd 逻辑不等于所有 API-key/relay/自定义 endpoint 都支持。
- 2153–2159 行支持 generate=false 预热；预热与普通模型生成分开计数，不能把预热当成已产生可计费输出，也不默认向真实上游新增探测请求。

外部 Preloop PR 可参考其有界会话注册表、逐请求传输/负载观测和小样本验证过程；其宽泛“增量错误后全量重发”不能覆盖本项目的副作用与归属契约。引用该 PR 的忽略字段列表时，必须复核以上固定版本官方类型/比较器的实际语义，而不是将旧示例扩展为通用删除规则。

## 6. 传输与压缩：分别验证

### 6.1 原生 WS 路径：先保证不回退、不断链

先在原生 WS 入口证明合法增量不被扩成全量、连接不被无故重建、会话不被无故断链。激活客户端会话级 fallback 的故障会影响后续多轮，因此 P1 优先于新增裁剪发送；“收益最高”仍需 P0 的频率与耗时证据支持。用户提到 2026-09-30 的慢回退案例，本次未取得其原始 trace，作为待复现线索，不作为已核验的量化依据。应减少非必要故障与恢复等待，同时保留及时 HTTP 回退、合法重连与 55 分钟轮换；不以阻止回退牺牲可用性，也不关闭 ForceNewConn、strict 或线程隔离换漂亮数字。

### 6.2 已落地的 HTTP→WS：先查实际命中

受控 HTTP→WS 已在第 13 节落地，不再新建桥接层。它受已有全局/OAuth/WSv2 开关、账号类型/模式及 force_http 控制；API-key HTTP、compact 和 passthrough 按当前合同保持 HTTP。

`mode_router_v2_enabled=false` 时核验账号 WSv2 开关；开启路由器时核验 ctx_pool / passthrough / http_bridge / off 的实际解析结果，不能只看示例 enabled=true；默认值不代表线上保存值。

P0 保留 request→attempt 序列、入口/上游传输、决策/回退原因及 final outcome，区分从未进 WS、握手失败后的安全回退和客户端自行改 HTTP。P4 只在已有 WS 发送点加入可证明的后缀优化，不复制网关，不关闭必要恢复来提高 WS 占比。

### 6.3 压缩种类不能混用

- WS 当前使用 permessage-deflate 类型的协商能力；应记录实际扩展协商和 frame 压缩情况。开关存在不等于本轮成功压缩。
- HTTP zstd 是 Content-Encoding 请求 body 压缩，与 WS frame 压缩是两件事。只针对经证据确认支持的准确 scheme/host/path/method；不能因当前会解压 zstd 响应就给所有请求加 zstd。
- 压缩发生在最终逻辑 body、身份和审计快照确定之后；每次尝试只压一次，Content-Length/GetBody 一致，重试可以从原始逻辑 body 重建，不重复压缩同一内容。
- 不支持编码时，仅在明确请求未执行且现有预算允许时回退；超时/断连不等于安全重发。
- 测量 CPU、分配、压缩后字节与上传时间，避免为很小的增量增加更大的压缩开销。不预设“压缩一定更快”。

## 7. 观测与对照实验

### 7.1 统一首字定义

截至 2026-10-21，HTTP 的 openai_ttft_mode 默认 semantic，可选 visible；后者也接受工具参数/图片，不等于纯文字。WS 的 isOpenAIWSTokenEvent 包含多种 delta，不能默认与 HTTP 同口径。页面 TPS 为输出 token 除以完整转发耗时，包含首字等待，输出还可能包含推理 token；不是纯文本解码速度。

沿用现有 Ops/传输诊断，分别标记：请求进入、准入/排队、连接获取/复用、TLS/WS 握手、请求写完、首上游事件、首语义输出、首可展示文本、首本地下游 flush、终态。heartbeat 与 response.created 不算可展示文本；本地 flush 不冒称客户端已收到。需要客户端 trace 才能测最后一段网络。

字节/状态指标至少包括：原始逻辑请求大小、发送前逻辑 body 大小、增量 item 数、删除历史字节、实际压缩后请求/帧字节、连接复用、增量判定原因、完整恢复次数与字节、previous_response_not_found、上下文缺失、重试/重复执行、实际 input/cached/output usage。不能把 JSON 长度当成线上压缩字节；若尚无可用计数器，明确标成待测。

指标用低基数标签与既有脱敏 trace；不把用户提示词、密文、token、原始 session ID 或完整工具参数写入性能日志。

现有 usage_log.go 的 RequestType/OpenAIWSMode 是兼容历史展示的字段，不能独立回答每次尝试实际使用什么上游传输、是否裁剪了历史。新增观测先落既有 attempt/Ops 路径，只有确认现有结构不能持久化所需摘要时，再提出最小 schema 扩展；不能只改 Go struct 就声称已经落库。

P0 必须先得到 **带 Codex 标识流量的 HTTP 请求与会话占比**。originator / User-Agent / turn-metadata 是分类线索，不是客户端真实性、租户授权或发生 fallback 的证明；缺失或冲突单列 unknown，不擅自归零。请求分母是相同时间窗/识别口径下 HTTP 生成请求与 WS response.create 生成请求，排除握手、心跳和 generate=false 预热；网关内部重试不重复计作新客户端请求。

会话按已认证租户与可靠显式会话身份聚合，报告 HTTP-only、WS-only、mixed 与身份不足的 unknown；HTTP 触达会话占比为 (HTTP-only + mixed) / 已识别会话数，并同时报告身份覆盖率。观察到同会话先 WS 后 HTTP 只能说明传输转换；只有客户端明确的 fallback 事件/trace 才标记 confirmed_fallback，不能从全部 HTTP 流量反推 WS 故障。跨实例关联不足要保留未知，不为了度量新增正文共享状态。

P2 同时报告可判定覆盖率（可判定 / 候选全量 HTTP 请求）与条件命中率（匹配 / 可判定），单列原生显式续接、淘汰、输出不完整和并发等样本。请求/会话 HTTP 占比决定样本来源，前缀候选与估算字节决定 P4 是否值得研究，均不能代替实际 WS 状态可复用性与端到端性能验收。

建议记录两组独立语义：上游传输为 HTTP/SSE、HTTP/JSON 或 Responses WS；负载形态为完整自包含输入、客户端原生增量、网关证明后裁剪的增量、恢复全量或未知。仅有 previous_response_id 不足以判定本轮一定减小了输入；实际节省由裁剪前后字节证明。失败尝试保留耗时、字节和原因，账单摘要绑定最终归属；混合尝试不能伪装成一次成功 WS 请求。

“跑一周”可作为观察窗口，不能作为性能结论的充分条件。固定模型、effort、输入/输出规模、缓存状态、账号与出口，按会话分组避免不同试验共享父响应和连接状态；记录样本数、p50/p95、错误率、恢复成本及完整任务耗时。未达到事先定义的样本/稳定性条件时继续测量，不根据单张截图晋级。

### 7.2 把传输、压缩、增量的效果拆开

| 对照组 | 上游传输 | input | 请求/帧压缩 | 回答的问题 |
| --- | --- | --- | --- | --- |
| A | HTTP，正常连接复用 | 全量 | 不额外启用请求压缩 | 当前基线 |
| B | HTTP | 全量 | 仅端点确认支持的 zstd | HTTP 请求压缩收益 |
| C | WS | 全量 | 关闭 WS frame 压缩 | 单独改变传输的收益 |
| D | WS | 全量 | 协商 frame 压缩 | WS 压缩收益 |
| E | WS | 增量 | 关闭 WS frame 压缩 | 在相同传输下减少历史的收益 |
| F | WS | 增量 | 协商 frame 压缩 | 组合收益和 CPU 代价 |

这是实验设计，不是六组都已可运行；不支持的端点/协商单独记录，不伪造成功。对照固定模型/effort/工具、账号权限、服务档位、代理出口和并发，交错运行配对工作负载；冷连接、热连接、冷缓存、热缓存分层。输出长度不同须分层或归一化，不能直接以总耗时排序。

覆盖首轮、10–20 次工具往返的长链、长历史、并发子代理、重连、压缩窗口、大小图片/文件等不同负载。公开 p50/p95、样本量与失败/回退率，保留慢样本；不把截图的 6.61 倍写成目标或保证。样本量与真实请求预算在执行实验前确定；本轮没有运行真实付费对照。

### 7.3 费用与上下文不能混淆

增量减少的是 wire 上传和部分续接开销，不是凭空把模型历史上下文或计费输入删除。OpenAI 公共 Responses 文档明确 previous_response_id 链仍计量历史输入；最终按上游真实 usage 和当前缓存计费规则记账，不用 Δ 大小替代 input_tokens。

同样，保持 prompt_cache_key 不变只能保证本地键连续性，不能保证命中率零波动。会话身份方案不得顺带改变现有缓存键的派生分支；新旧 wire key 需要逐字节断言。

## 8. 分阶段实施顺序与完成标准

| 阶段 | 工作 | 必须看到的结果 |
| --- | --- | --- |
| P0 运行证据与统一度量 | 复用已有 HTTP timing、WS pool/relay、Ops、调度 trace，取得部署/生效模式及 request→attempt 证据 | 明确起点、首事件/语义/内容/文字/flush，分传输/负载报告，未知保持 unknown |
| P1 连接和等待 | 核查现有线程/账号粘性、连接归属、保活、重建/回退及排队；对接在研调度任务 | 减少有证据的多余等待，不另造调度/连接池，不放宽重放边界 |
| P2 被动前缀评估 | 最终变换后按固定 Codex 语义比较 H/C 和非 input 设置，仅保存有界摘要/必要锚点 | 取得覆盖率、命中率、不可判定原因及字节估算；出站不变，不多发请求 |
| P3 已有压缩验证 | 测量现有 HTTP zstd / WS 压缩的真实编码、字节、耗时及重试一致性 | 字节收益与 CPU/TTFT 代价可量化，不再次实现压缩 |
| P4 受限自动增量 | P2 证实值得后，在现有 HTTP→WS 发送点使用真实父响应与精确 Δ；条件不满足沿用原请求/恢复 | 实际 wire 变小，语义/用量等价，失败/换号/重连/compact/并发安全 |

P0/P1 与 P2 设计可以并行；实际顺序由排队、上传、上游准备和首文字各阶段耗时决定。P2 有计算和内存开销；原生客户端已发增量时只保护其语义，P4 优先处理真实全量 HTTP 请求。第 16 节对应的上一轮只完成复核与方案更新；第 17 节记录本次实现，不把本地实现当作已经上线。

性能门槛在试验前约定；正确性门槛固定为：零跨租户泄露、零自动重复执行副作用、零静默上下文丢失、零以缺失终态伪装成功，预算/取消/部分 usage 保持既有要求。单纯 TTFT 变小而恢复成本、错误率或重复执行增加，不算通过。

## 9. 测试落点与边界用例

优先扩展已有 openai_ws_forwarder_ingress_test、retry_payload、execution_scope、state_store、pool、http_bridge_resume、passthrough 和 recovery/ownership 测试。输入/输出改写只在一个现有归属处决定，不给每个入口复制一套判断。

- 原生增量：首轮完整、第二轮仅用户新项、工具结果、parallel tool calls、客户端自带不同合法父响应；不得擅自替换客户端指定父响应。
- 自动裁剪：完整历史逐项相同；某个历史消息被编辑；相同文本不同角色；工具参数大整数；相同 key 不同请求/租户；未知或重复字段。
- 旁路计算：命中与六类比较失败；网关自身缺身份/淘汰/换实例/并发等原因另测；不同租户/账号的相同会话值不能合并；模型/工具等非 input 变化、窗口变化必须失配；同会话并发和迟到完成不能覆盖新代际；不完整输出不能建立锚点。
- 比较等价性：同一组夹具对照固定版本官方类型比较与本地摘要比较；覆盖缺失/null、数字精度、未知类型、仅内部 passthrough 不同、迟到工具结果元数据不同，以及短/长 call id 的成对转换；不自定扩大忽略规则。
- 度量与成本：TTL、条目/item/总内存限制及 CPU 开销；重试/预热不污染请求分母；mixed 会话与 confirmed_fallback 分开；字节估算不冒称已省流量；不产生第二份上游请求，出站字节逐字节不变。
- 完整输出：thinking/reasoning 密文、tool call/output 配对、compaction、多模态及特殊 output items；collector 不完整时不宣告可裁剪。
- 连接与归属：同租户连续回合、不同用户相同原始 ID、独立 thread/子代理、凭据轮换、代理/端点/模式改变、跨实例和连接过期。
- 恢复：明确 previous_response_not_found、已写部分帧、未见首字但可能已执行工具、丢失终态、下游取消、当前轮预算耗尽；完整历史足够与不足分别断言。
- 压缩：未协商、协商成功、HTTP 不支持编码、body 可回绕、Content-Length、重试不重复压缩、不支持的 endpoint 完全不受影响。
- 费用与日志：使用上游 usage，不以裁剪长度估算历史收费；部分用量只提交一次；无正文/凭据进入性能日志。

先在本地替身和实际 socket 上做非计费的契约/故障注入测试；这些通过也不能代替真实上游支持与性能验证。本机 Go 启动器在 ~/sdk/go1.27/bin/go，不在默认 PATH；backend 模块实际自动选用 go1.27.2 darwin/arm64。2026-10-18 的既有验证见 11.1；2026-10-21 本轮定向复核见 16.6，不能视为本节全部候选场景已经覆盖。

## 10. 对作者其他说法的判断

1. 截图支持“作者展示的后组请求更快”，不支持“因为 SSE 被故意限速”或“所有请求会快 6 倍”。输入规模、缓存、排队、模型、effort、出口与首字定义都是混杂因素。
2. WS、HTTP 请求压缩、增量续接、prompt cache 是不同层。SSE 是响应事件载体；它本身不要求每轮上传全量历史。全量自包含输入也是状态不可用时的合法恢复方式，不能把它等同 DDoS。
3. “唯一开源 WS”与当前仓库已有原生 WS 代码不符；“1:1、无封号、无降智”没有由这些截图或源码获得证明。
4. 不复制他人的真实 device/client ID、Cookie 或 token；Windows/Node/SDK 版本等通用兼容画像并不是独有设备标识。保留本项目已核验的真实样本与身份隔离，不能随机改几项版本号或频繁换 ID 来“去特征”。
5. Claude 的可选使用遥测、错误上报和组织自建 OpenTelemetry 是不同通道。官方允许 DISABLE_TELEMETRY / DISABLE_ERROR_REPORTING 等选择，部分开关会影响 feature flags/Remote Control；因此“没遥测必然异常或封号”没有得到官方资料支持。
6. 地区与仓库活动可作自有系统测试输入，但第 12 节平台后来已撤销，不借性能复核重新启动。作者声称模拟地域/维护大型项目未提供可核验的性能因果证据，不进入当前生产优化方案。

## 11. 一手资料与验证状态

本轮累计查阅的资料（2026-10-18）：

- OpenAI WebSocket Mode：`https://developers.openai.com/api/docs/guides/websocket-mode`。用于核验持久连接、增量 input、连接内状态及恢复机制；不把公共 API 最新能力直接套到内部 ChatGPT OAuth。
- openai/codex 源码 `codex-rs/core/src/client.rs`（2026-10-18 经 GitHub API 取主干版本；本机 codex-cli 0.159.3）。核验点：会话级 cached_websocket_session；get_incremental_items 严格前缀判定与 NonIncrementalReason 六类；response_items_equal_ignoring_internal_metadata 比较规则；WebsocketSession.reset 后全量重发；WS 回退 HTTP 为会话级。增量续接逻辑对 ChatGPT 与 API provider 共用。
- OpenAI Conversation state：`https://developers.openai.com/api/docs/guides/conversation-state`。用于区分传输历史与模型上下文/计量。
- OpenAI Compaction：`https://developers.openai.com/api/docs/guides/compaction`。用于窗口切换与保留压缩产物。
- OpenAI Reasoning models：`https://developers.openai.com/api/docs/guides/reasoning`。用于 reasoning 与工具上下文完整性。
- Claude Code Data usage：`https://code.claude.com/docs/en/data-usage`。用于可选遥测、错误上报与默认行为。
- Claude Code Monitoring：`https://code.claude.com/docs/en/monitoring-usage`。用于区分组织 OpenTelemetry 与供应商使用遥测。
- OpenAI 工程博客：`https://openai.com/index/speeding-up-agentic-workflows-with-websockets/`。报道 agent 工作流的收益与多层优化，不证明某个网关的 SSE 被有意限速，也不承诺本项目同等收益。
- OpenAI Codex 固定源码：`https://github.com/openai/codex/blob/1cc7dc8f2acc63ce6c1cb72271e6b90a76136c2b/codex-rs/core/src/client.rs`。通过 GitHub API 确认该文件提交并读取同 SHA 原文；具体比较、预热和压缩条件见 5.5。
- Preloop PR #1457：`https://github.com/preloop/preloop/pull/1457`。维护者实现与其自述验证，仅作工程参考；不把该项目的生产测试视为本项目已验收。
- kstack issue #141：`https://github.com/kstackhq/kstack/issues/141`。需求/方案讨论，不是已完成的生产验证。

本次还读取同一固定 SHA 的 codex-rs/protocol/src/models.rs：ResponseItem 的派生 PartialEq、serde 默认值及内部 metadata 清理方法，是 5.2 比较等价性要求的依据，不能仅摘取 client.rs 的比较函数就假定 RawMessage 哈希等价。

用户提供的 LingTai 页面此前抓取失败，未据其内容作结论。此前 Python urllib 初次访问 GitHub 因本机证书链失败，改用系统 curl 在保留证书验证的情况下读取成功；未降低 TLS 校验。

当前本地权威恢复要求见 `docs/requirements/final-v0.2.17-2026-10-17/06-recovery.md` 及其最新链接，优先于外部示例中的简化“删 previous_response_id 重试”。同 owner 恢复、跨账号可携带性、工具副作用和已提交输出的边界不能合并为一个“允许回退”开关。

此前完成两图统计、调用链和工作区差异核查、官方资料核验及第二版复核，并实际确认 Go 1.27.0 和 codex-cli 0.159.3。本轮依据用户澄清，将地区、环境、遥测和仓库活动模拟作为测试需求，扩展第 12 节并同步第 1/10 节范围说明；先保存 /tmp 备份，再检查示例配置、文档一致性及未涉及章节/源码。未执行：测试框架实现、Go 测试、仓库活动或遥测发送、真实付费探测、线上 A/B、推送、发布、部署或迁移。

上段记录的是设计复核阶段，随后开发的测试平台已按用户要求全部撤销；原有设计文档作为历史设计保留。当前生产协议修复的实现与验收见 11.1，不再以测试平台作为主交付。

### 11.1 当前实施状态：P0 正确性第一批

| 项目 | 当前状态 |
| --- | --- |
| Claude SSE 事件与终态 | 已修改现有解析/转发：多行 data；无 event 的 type:error；终态后立即返回；不把空白行当分隔；保留累计而非重复相加的 usage |
| SSE 错误后的用量与重试 | 已打通真实 Messages handler → Forward → usage worker → RecordUsage 验收。已输出、已观察到 usage 或客户端断开后不再转成丢弃结果的 failover；原始错误只发送一次、部分用量只提交一次；无输出无用量的安全重试保留 |
| Codex 出站身份 | 补齐官方头白名单、缺 namespace 的旧隔离降级、原始值回退、compact metadata 及 WS 首帧投影。详见 [身份迁移实施更新](CODEX_SESSION_IDENTITY_MIGRATION.md#8-2026-10-18第一阶段实现与验收) |
| 原生语义修正 | 固定 Codex 源码允许显式 session-id affinity override 与 metadata session 不同；保留合法差异，不将其误判为不一致而覆盖 |
| P0 度量 / P1 全部可靠性 / P2–P4 | 未完成；不把上述单元/链路回归写成真实流量 HTTP 占比、压缩收益或增量性能实验 |

本批不新增生产 HTTP 接口、运行开关、数据库表或独立转发路径；只修改已有协议/错误
处理并扩展现有回归。保留旧下划线头；未启用 HTTP→WS，未裁剪任何 input，未启用
新的上传压缩，未改变真实上游请求的计费口径。未发真实供应商探测、未部署。

2026-10-18 验证记录（Go 1.27.2）：

- 整包 unit：internal/service（复跑 256.310s）、internal/handler（90.416s）、internal/repository（11.852s）、internal/pkg/openai（1.151s）、internal/pkg/claude（0.046s）、internal/pkg/tlsfingerprint（0.087s）通过。首轮 service 暴露两项旧身份预期，核对本次明确的 compact 隔离/缓存与 thread 关联规则后更新断言，再整包复跑通过。
- 身份、WS ingress/passthrough/recovery、SSE 及真实 handler 部分用量链路的定向 race：service 27.341s、handler 2.409s，通过。
- cmd/server 正常构建通过，临时二进制已删除；git diff --check 通过。未运行真实付费 A/B，也未测得线上延迟改善。

## 12. 地区、环境、协议与活动数据测试设计（历史，已撤销）

本节保留原设计供追溯；其中的实施语句不构成当前待办，S0–S4 平台不会因本次性能复核恢复。

按用户本轮澄清，本节目标是建设可控的测试模拟能力：为同一系统组合不同地区、客户端环境、会话身份、遥测行为与仓库活动，生成可复现的正常/异常输入，并核对系统是否按预期处理。测试中的虚拟地区、模拟事件和模拟开发者操作是正常测试数据，不直接视为欺骗行为。交付包含场景模型、数据契约、执行流程、故障注入和验收标准；本轮仅设计，未实现执行器。

### 12.1 将 1:1 拆成可以检验的契约

| 层次 | 要检验的内容 | 真实来源和边界 |
| --- | --- | --- |
| 应用协议 | 路径/方法、JSON 字段、流式事件、错误与终态、工具调用配对、取消 | 固定版本官方客户端和公开契约；以模型实际收到的语义及下游恢复行为验收 |
| SDK 兼容元数据 | 客户端版本、平台、功能开关与请求能力是否自洽 | 只使用已核验、与实际支持能力一致的数据；未知字段保留或明确拒绝，不猜造官方版本/能力 |
| 会话与身份 | tenant/account/session/window、header/body 对齐、重连与父响应归属 | 真实入站身份加既有隔离规则；不得复制另一设备的标识或跨账号复用 |
| 传输 | 实际连接复用、协议协商、WS 扩展、请求编码、超时和背压 | 观察实际协商与字节，不将修改 User-Agent 等同于 TLS/HTTP 全栈相同 |
| 遥测 | 客户端可选使用指标、错误上报、组织 OpenTelemetry、网关诊断 | 分开记录控制权、目的地和隐私选择；不由网关补造客户端使用事实 |

网关终结客户端连接后重新连接上游，不天然保留原客户端整个网络栈；方案不承诺跨操作系统/运行时/网络路径的全部可观测字节相同。兼容元数据差异应逐条解释，不能以“删得越少/越多越像官方”作为统一规则。

### 12.2 复用现有实现，先修正确性差异

- Codex：身份 B 继续复用现有 scopeCodexAccountIdentityValue，以同一原始值、同一作用域、同一派生种类生成最终会话身份，再让 header/body 引用同一结果。先验证 namespace 缺失仍隔离、只派生一次、四条实际出站路径一致，再清理下划线别名；不得先删 session_id 再期待另一路自动补齐。
- prompt_cache_key：独立保留原始输入与派生依据。现有实现会比较原始 session 与 cache key；身份迁移不等于缓存字段天然不变，必须断言迁移前后的最终出站 key 和归属。不得为了 header 对齐修改实际用户上下文或 cache 语义。
- Claude：复用现有 OAuth body、metadata_userid、identity_service 与 fingerprint 归属；测试真实客户端请求经网关后，工具、thinking、cache_control、system 及流事件的业务语义是否完整。现有工作树改动来自其他工作，不在本方案中直接覆盖。
- SSE：沿用完整事件解析与已提交输出边界；多行 data 按事件组合，合规 EOF 与缺失分隔/终态分别处理。流内 error 保留真实错误与部分 usage，不能因为 HTTP 200 或模型已输出一些文字就当成功，也不能拼接另一轮重试流。
- 测试执行器复用现有 handler/service 测试入口、临时 HTTP/WS 服务与身份规则；新增的是场景夹具和测试编排，不新增生产常驻 worker、自动下载公共仓库任务或第二套生产身份配置。

### 12.3 本地差分验证夹具

夹具与故障注入的完整框架设计见 [测试数据与环境模拟框架](TEST_DATA_AND_ENVIRONMENT_SIMULATION.md)；本节只说明协议一致性这一类用途，不重复其结构。

链路：固定版本客户端样本/合成场景 → 脱敏与来源标记 → 现有网关测试入口 → 本地、CI 或自有预发接收器 → 结构/事件/时序差异报告。原生客户端如支持指定测试端点，可将测试请求导向自有接收器；没有该能力的部分采用夹具验证，并把“原生客户端实测”与“模拟器测试”分别报告。

每份夹具记录版本、来源、采集条件与已知不确定项。鉴权材料不落盘；涉及 session/工具 ID 的脱敏采用一致映射以保留关联关系，不能随机替换后丢失身份错误。原始会话正文不进入共享仓库或普通性能日志。

比较器分三类：必须一致的业务语义；允许变化但必须满足关联约束的 request/session/call ID 和时序；协议明确不敏感的编码/序列化差异。忽略规则按字段且有理由，不能全局删除 client_metadata、所有 ID 或所有未知字段后宣告 1:1。

首批覆盖：普通文本、工具往返与并行工具、reasoning/thinking、缓存、图片/文件、compact/window 切换、重连、取消、部分输出后错误。复用已有测试套件和解析器；夹具仅增加缺少的独立风险覆盖。

验收报告列出匹配项、可解释差异、真实缺陷和未核验项。以事件完整、身份隔离、正确终态、正确计费及不重复执行为通过条件；不把“未被封号”“显示某地区”“被识别为大型维护者”写成质量指标。

### 12.4 测试范围、执行模式与场景矩阵

提供三种执行模式，共用一种场景描述：synthetic 生成可控事件，fixture_actions 在临时目录执行真实文件/Git 动作并采集结果，sanitized_replay 回放经授权且脱敏的历史样本。模拟器不声称实际运行了原生客户端；fixture_actions 也只代表本地测试动作，不代表公共 GitHub 活动。

| 维度 | 正常场景 | 异常或边界场景 | 主要断言 |
| --- | --- | --- | --- |
| 地区与本地化 | SG/US/其他地区标签、不同语言和时区 | 地区/语言/时区不一致、未知地区、缺失值、运行中改变 | 系统按测试配置解析，不把缺失信息自动补成确定位置 |
| 环境与客户端画像 | Windows/Linux/macOS 协议夹具、已记录版本、终端/CI 环境 | 版本字段缺失、未知能力、环境声明与实际 runner 不一致 | 声明环境与真实执行环境分别记录，兼容路径有明确结果 |
| 身份与会话 | 同一会话稳定身份、多个独立测试租户 | 不同租户使用相同原始 session、换账号、重启、窗口切换 | 既有作用域隔离生效，不串状态/日志/用量 |
| 遥测生命周期 | 启用、禁用、批处理、正常退出 | 禁用后仍有待发送批次、丢失/重复/乱序、离线恢复、时钟偏移 | 遵守 opt-out；按事件身份去重；错误可归因，不阻塞主请求 |
| 仓库活动 | 打开、检索、读取、编辑、测试、提交本地仓库 | 空库、多语言、大量小文件、大文件、只读、测试失败、分支切换 | 活动关联正确，资源有界，失败不被记录为成功 |
| 传输与故障 | HTTP/SSE/WS、连接复用、正常取消 | 批次超时/429/5xx、连接断开、半包、异常终态 | 重试受预算约束，不重复输出/计费，不丢已观察终态 |

地区测试拆分 client_region_hint、locale/timezone、模拟 GeoIP 查询结果和实际出口观测，禁止把它们合成一个含糊“所在地”。synthetic 模式向自有解析器注入查询结果；需要真实跨地区网络测试时，使用自有测试节点，记录实际测量条件，不能把注入标签当成真实网络位置。

环境测试同样区分 declared_profile 与 observed_runtime。模拟 Windows 字段只验证数据处理；验证真实 Windows 运行行为须在相应 runner 执行。时区通过测试配置或子进程环境设置，不修改开发者机器的全局时间/系统环境。

GitHub 相关数据采用两类夹具：本地版本化 Git 仓库用于真实动作；自有 mock 服务提供仓库/分支/commit/issue/PR 元数据用于接口场景。按固定 seed 从夹具目录选取不同项目规模与语言，记录 fixture 版本或摘要；默认不联网随机克隆项目，不向公共仓库 push、发 PR 或制造活动记录。

### 12.5 执行架构与数据契约

执行链：场景加载器 → profile/fixture 选择 → 场景执行器与可控时钟 → 请求/遥测/仓库适配器 → 自有被测系统与接收器 → 断言与差异报告。

复用现有 Go handler/service 测试和临时 HTTP/WS 服务：协议解析、身份映射、事件聚合只保留原有实现，测试适配器负责提供输入和观察结果。独立编排只承载跨阶段脚本、可复现随机和故障调度，这些职责不属于生产转发器，因此不放入生产请求链。首版不新建数据库表、管理端页面、生产 API 或常驻后台任务。

拟定三类测试对象；名称与字段是设计契约，不是已经存在的生产配置：

| 对象 | 必要信息 | 用途 |
| --- | --- | --- |
| Scenario | schema_version、scenario_id、seed、mode、target、profile、fixture、steps、faults、limits、expect | 描述一次可复现运行、注册的自有目标及通过条件 |
| TestEvent | run_id、case_id、event_id、sequence、attempt_id、event_kind、logical_time、observed_at、origin、payload | 区分生成顺序、投递尝试和实际采集时间；origin 标为 synthetic、fixture_observed 或 sanitized_replay |
| RunReport | 场景/夹具版本、实际 runner、有效配置摘要、计划/生成/投递/接收/去重计数、断言结果、资源使用、失败证据 | 明确“预期发生”与“确实发生”，支持重放与差异分析 |

同一逻辑事件重投时 event_id 不变，attempt_id 改变；故意复制事件的测试显式复用 event_id。run_id 隔离每次执行，语义序列由 seed、夹具版本和虚拟时钟确定；跨次比较排除 run_id 与真实墙钟，但不忽略会话/工具关联差异。超时和资源预算使用单调时间，不受被注入的时钟偏移影响。

synthetic/origin 标记属于测试 envelope、接收器及报告，不擅自塞进要求原始协议形状的模型请求或遥测 payload。这样既能测试精确 wire 格式，又能追溯测试来源。采集器与业务事件格式分开；敏感字段使用测试值或稳定脱敏映射。

正常契约样本、缺失字段样本和畸形 wire 样本分开保存，异常样本附 expected_error 与 expected_stage。场景描述本身必须校验通过，不能因为故意生成无效 payload 而跳过对目的地、资源预算和夹具来源的校验。

### 12.6 示例场景与单次执行流程

以下为拟定配置示例；数字是测试预算示例，profile/fixture 名称待实现时注册，并非现有可执行配置。

~~~json
{
  "schema_version": 1,
  "scenario_id": "sg-locale-repository-telemetry-retry",
  "seed": 271828,
  "mode": "fixture_actions",
  "target": "owned_mock",
  "profile": {
    "client_fixture": "recorded-client-profile-a",
    "client_region_hint": "SG",
    "locale": "en-SG",
    "timezone": "Asia/Singapore",
    "telemetry_enabled": true
  },
  "fixture": {
    "source": "generated",
    "profile": "small-typescript-repository-v1"
  },
  "steps": [
    {"op": "session.start"},
    {"op": "repository.open"},
    {"op": "repository.search"},
    {"op": "repository.edit_fixture_file"},
    {"op": "repository.run_fixture_test"},
    {"op": "repository.commit_local"},
    {"op": "session.end"},
    {"op": "telemetry.flush"}
  ],
  "faults": [
    {"at": "telemetry.flush", "type": "first_attempt_http_status", "value": 429},
    {"at": "telemetry.flush", "type": "duplicate_delivery", "count": 1}
  ],
  "limits": {
    "max_events": 1000,
    "max_duration_ms": 60000,
    "max_concurrency": 2,
    "max_memory_mb": 256,
    "max_subprocesses": 2,
    "max_workspace_bytes": 10485760
  },
  "expect": {
    "repository_actions_observed": true,
    "business_events_deduplicated": true,
    "session_identity_stable": true,
    "cross_tenant_events": 0,
    "external_production_requests": 0
  }
}
~~~

单次运行：加载并验证场景与预算；创建独立临时目录、测试身份和自有接收器；记录实际 runner 与配置摘要；按步骤执行动作并记录原始结果；在指定事件边界注入故障；收集输出直到终态或预算耗尽；核对事件、会话、仓库和资源断言；生成报告，清理临时进程/目录。失败仍输出报告，不把未执行步骤记作已完成。

fixture_actions 的文件、测试命令和本地 commit 只作用于生成或复制的测试仓库，不作用于当前开发工作树。重放动作只允许预定义操作与受控夹具命令，不能把事件中的任意文本作为 shell 命令执行。取消或预算耗尽后终止子进程，防止残留任务继续产生日志。

故障计划明确先后顺序与作用层：例如上述 429 作用于第一次批次投递，重复投递使用同一逻辑 event_id；随后报告分别统计尝试次数与去重后的事件数量。消息乱序、截断、离线与时间漂移分别作为独立场景，组合测试也记录组合顺序，便于定位失败原因。

### 12.7 环境、隔离与验收

local 使用进程内或 loopback 服务，CI 使用一次性测试网络与固定夹具，自有预发使用显式目标清单、独立测试租户和资源预算。目标在启动时解析到具体接收器；不允许重定向悄悄越过目标范围。第三方公开提供的测试环境如需集成，另用其测试凭据和契约，不能默认切到其生产收集端。各 runner 声明可强制执行的资源限制；不支持某项硬限制时拒绝需要该限制的场景，不将仅有监控冒称已限额。

控制测试影响：数据标识与生产统计分开，默认不使用生产凭据，不向真实用户发送事件；客户端 opt-out 场景验证本系统确实停止对应采集/发送，测试执行器自己的结果报告另行保留。真实用户遥测与组织 OTel 的现有选择继续生效，不由模拟器改变。

验收以可观察事实为准：

- 相同 seed/夹具/虚拟时钟生成同一输入序列与故障计划；报告可以准确重放失败步骤。真实网络和系统耗时仍作为实测结果，不要求跨机器性能数值完全一致。
- 地区、时区、语言与环境的正常/缺失/冲突输入得到预期处理，实际 runner 信息不被模拟 profile 覆盖。
- 开启、禁用、重复、乱序、离线恢复均有断言；去重作用于业务事件，投递尝试仍可审计，错误不造成重复计费。
- 多租户相同原始 session 不串数据；重启、换凭据和窗口切换遵守既有身份与恢复规则。
- 仓库行为与实际文件/Git 结果一致；模拟 API 元数据标为 synthetic，失败动作不被报告为成功活动。
- 事件数、并发、持续时间、内存和子进程数量均受预算限制；取消/退出后无残留测试进程和未完成投递。
- 接收器收到的事件与 RunReport 计数对得上；统计明确区分预期、生成、投递、接收、去重和丢弃。

按 fixture 单元测试、实际 socket 契约测试、跨环境 runner 三层验证；模拟成功不等于真实操作系统、网络出口或原生客户端验证成功。采用最小风险场景与按对覆盖组合，再对身份隔离、opt-out、重复投递、半流错误等关键组合定向增加覆盖，避免穷举所有维度导致测试失控。

### 12.8 实施顺序与交付物

T1 定义 Scenario/TestEvent/RunReport、fixture 清单与正常/异常断言，先复用已有 Go 测试入口跑纯本地场景。T2 增加仓库动作、可控时钟和遥测故障计划，完成多租户、opt-out、重复和乱序验证。T3 纳入 CI 与自有预发，增加真实 OS runner/网络条件并输出分层报告。

测试能力可独立于 P2/P4 推进，产生的测试流量需从线上性能 A/B 样本中排除；HTTP/WS 生产优化仍按第 8 节依赖实施。实现优先扩展现有夹具与测试，不给每个客户端或环境复制一套解析、身份或重试规则。

拟交付物为测试契约、版本化场景与夹具、统一执行入口、自有接收器适配、断言与报告。当前只完成方案与示例配置检查，没有实现模拟器、执行仓库活动、启动遥测发送器或修改生产代码。


## 13. 指定来源协议优化落地（2026-10-18 至 2026-10-19）

来源固定为 `huoji120/sub4api@a3eaf63afc672ffedf7844e6cfc5cb6750d52a3e`。这是对公开快照的行为适配，不是整分支合并；本地起点为 `e80eb7ca0e3330ac7a67600838004d4b2aabcd8e` 加已存在的未提交 SSE/身份改动。归属为三品牌共享网关；当前只实施、验证工作树，未提交、推送、发版、部署或迁移数据库。

### 13.1 本轮补齐

| 领域 | 实际实现与入口 |
| --- | --- |
| Claude SSE | 普通/透传共享完整事件解析，多行 data、id/retry/注释与原始 error 帧保留；缺分隔的 EOF 不伪造完整事件，部分 usage 随错误交回记账，已输出不重新生成。 |
| Claude 请求正确性 | 四处 JSON 重建采用 UseNumber；仅使用本请求工具映射回译；已验证 CC 的 max_tokens=1 流输出 # 与 max_tokens 终态。 |
| Claude 身份 | Redis 原子获取 masking seed；每个原 session 和 parent 使用同一账号/seed 映射，保持父子对应及不同会话基数。旧固定 session 缓存不作为新 seed；连续活动续期，空闲 15 分钟后轮换。 |
| Claude billing/count_tokens | isMeta/is_meta 不作用户文本采样；迁移 system 前捕获原文；真实 CC 缺块补 billing、客户端 UA 回退、count_tokens mimic 与 Messages 对齐；Vertex 服务账号使用 count-tokens:rawPredict 与 Bearer。保留本地固定 Linux profile、版本门控真实 CCH，不退回来源的 00000。 |
| Claude 控制头 | 六个控制头显式空值优先；缓存只接受真实上游响应（含错误响应）反馈，只为 OAuth mimic 缺项补值；不保存客户端头作账号反馈。 |
| Codex 头迁移 | 先完成账号/caller 作用域及官方 session-id，再删除下划线 ID、installation 旧头和 accept-language；namespace 不可用时保留旧隔离降级。明确 session affinity 可以不同于 body 的真实 session/thread。 |
| Codex metadata | 统一默认身份/轮次/window 字段，保留未知项和大整数；tool_namespaces_info 仅从 header metadata 裁剪，body 保留；同账号重试复用最终 body 中的 turn UUID/time。 |
| Codex WS 兼容 | parent-thread、subagent、memgen、timing 等真实调用头；parent 作用域化；可选收敛纳入 caller，保留已作用域化的独立 thread 与 window ordinal；首帧与后续轮接入同一归一化。 |
| Codex JSON/工具 | 重建后恢复原顶层字段次序，新字段采用声明顺序；保留字段值与精度。python__custom 采用请求拥有的双向映射，处理名称冲突，不单侧改名。 |
| HTTP→WS | OpenAI OAuth 类账号允许普通 Responses HTTP 入口进入现有 forwardOpenAIWSV2；复用账号模式、连接池、租户/响应归属、整轮预算及原生安全恢复。API-key HTTP、透传和 compact 保持 HTTP。 |
| HTTP zstd | 仅 HTTPS POST chatgpt.com/backend-api/codex/responses、JSON、有可重放 body、未已有其他编码的请求压缩；语义 body 与 GetBody 重试源保留，wire body/ContentLength/GetBody 一致。不压公共 API、旧 compact 或 WS frame。OpenAI transport 不再自动添加 gzip 请求头。 |
| compact 兼容 | 不将所有公开/自定义 compact 请求强行升级。仅 OAuth 旧 compact 路由明确返回 404/405、不是模型错误、尚未输出且预算允许时，原账号一次改用 Responses v2 compaction_trigger；客户端仍收到原来的 unary 合同。状态不跨账号 failover。 |
| 可观测性 | 真实 HTTP attempt 的连接、写请求、首响应字节、响应头、首语义、首下游 flush；心跳不计语义 flush。记录语义请求字节和 zstd wire 字节，WS→HTTP 降级同步实际传输上下文。日志不包含 token、代理凭据、正文。 |

### 13.2 配置与上线影响

- 没有新 HTTP 接口、管理页、配置项、后台 worker 或测试平台。HTTP→WS 使用已有 `gateway.openai_ws` 的 `enabled`、`oauth_enabled`、`responses_websockets_v2`、`force_http` 与账号 WS 模式；强制 HTTP、关闭 WS、`off` / `http_bridge` 模式继续生效。透传账号不会因普通 WS 开关被隐式切换。
- zstd 和正确性修复随新二进制生效；没有数据库 schema 迁移。masking 启用账号的新 seed 命名空间会在首次请求建立，父子会话标识因此有一次切换；未启用账号的普通映射保持。
- 保留 B 方案的既有 UUIDv4 与 cache key 派生；不为与来源外观相同而全局切换 UUIDv7。显式 thread/parent 语义、认证归属、BPS 独立授权与安全恢复优先于复制外观。
- 部署后可用已有账号开关对照 HTTP/WS，结合实际 input 字节、压缩后字节、首语义/flush、失败率和 usage 观察。当前没有真实供应商流量 A/B，单测或本地压缩比不证明线上 TTFT 改善，更不证明“不会降智/封号”。

### 13.3 覆盖边界和验收

- 本轮补齐公开快照中适用的协议/传输缺口，并保留已验证的本地差异。请求自动全量→严格后缀增量仍是 P2/P4 后续能力；公开来源本身没有此生产裁剪调用，不能把保留客户端 previous_response_id 续接说成网关自动裁历史。
- 保持现有客户端原生增量及完整恢复历史；没有新增 stream_id 多 lane 或跨 socket 内容缓存，没有伪造地区、GitHub 活动或对外遥测程序。
- 验收完成：Go 1.27.2 全后端 unit（61 个有测试包）通过；重点 service/handler/repository race、WS 多轮与重连 race、vet、server build、4 项 Workflow 约束检查通过。测试复用已有夹具，不访问真实供应商，不启用已撤销的测试平台。
- HTTP→WS 补齐 partial usage/no-replay 及 HTTP response owner；同用户后续轮通过、其他租户被拒绝，无身份头两轮续接复用原连接。WS header-only 首轮与当前轮重连 metadata、compact probe 均通过实际发送捕获验证。
- 业务代码 +1771/-464 行；测试 +2375/-282 行（相对任务开始的脏工作树，不是累计 HEAD diff）。新增外部接口 0、配置项 0、数据库迁移 0、独立生产入口 0；新增可达转发/恢复分支 3：Vertex count_tokens、受控 HTTP→WS、旧 compact 路由拒绝后的一次 v2 恢复。现有 IdentityCache 内部接口 +3/-2 方法，无新增服务。
- 交付审计与最终日志：[upstream-changes](../.release/upstream-sync/2026-10-19-claude-codex-coverage/upstream-changes.md)、[validation.json](../.release/upstream-sync/2026-10-19-claude-codex-coverage/validation.json)。本轮验证在 2026-10-19 完成，代码仍在当前工作树，未提交、推送或部署。

- 并发边界：最终通过结果对应审计文件记录的本轮代码快照。00:50 后共享文件又出现其他任务写入，已保留并单独记录，未计入本轮增删，不将此前测试结果用于认证后续任意修改。

- 并发补查当前状态：handler race 通过；service 新增 TestApplyToolNameRewriteToBody_Spans 的工具别名断言失败，详见 concurrent-check-retry.log。未覆盖其他任务修改；整个最新工作树尚未取得全量通过结论。

### 13.4 后续工作区组合验收（2026-10-19）

用户随后授权将工作区全部修改归入既有相关提交并推送三个品牌分支。本节记录后续完整组合的验证，取代上一节并发补查时点“最新工作树尚未全量通过”的状态；保留此前结果作为过程记录。

- Go 1.27.2 执行 `go test -tags unit ./... -count=1 -timeout=15m`，61 个有测试包全部通过；另有全后端 `go vet -tags unit ./...` 及 repository 全包 race 通过。
- 主站与供应商门户构建、多语言键完整性、繁体生成器 `--check`、离线缓存分析器通过。繁体文案按生成器重新生成，未保留手工改写。
- 三品牌 Workflow 手动触发与发布渠道检查通过；Tokensavy 有 1 项既有安装器保护测试按其适用条件跳过。没有执行真实供应商调用、生产数据库迁移、发版或部署。
- 历史整理按候选文件树验证；官方 Wei-Shaw 提交保持原 SHA，各品牌由实际共同基点三方合并生成预期树后重新接续。新旧主题 SHA 对照、完整备份、命令日志和最终远端核验保存在本次交付记录中。

## 14. 六项复核缺口修复（2026-10-19）

第 13 节“适用缺口已覆盖”的范围以本次复核与修复记录为准；此前未覆盖的兼容出口已补齐，不把旧的全绿结果等同于全路径正确。

1. **Claude 工具回译**：在 Anthropic 响应/事件转换之前按本请求映射恢复工具 name；Chat/Responses 的 JSON/SSE 四条回程统一，参数、文本、ID 不替换，失败账号的旧映射不带入下一尝试。
2. **Claude 兼容 SSE**：四条回程共用既有完整事件解析与大小限制；接受多行 data、注释、id、CRLF。只有完整 message_stop 才成功，缺终态/读取错误/流内 error 返回部分 usage 与错误。终态无需等待 EOF，断连后仅进行有界用量回收。Responses handler 与 Chat 一样保留 partial result 的一次记账，不重放已执行请求。
3. **Claude 控制头反馈**：两个兼容发送点接入现有 observer；真实成功响应、HTTP 错误响应及伴随 transport error 的响应都可更新既有缓存。
4. **Codex header-only metadata**：在相同作用域阶段先合并真实 header/body，body 优先，再生成缺省字段；真实身份捕获与已投影快照分开。session/full 模式保留真实 thread、turn、时间与 window ordinal；HTTP 重试稳定，WS 后续轮不复用首帧头。B 方案 v4/cache 派生不变。
5. **Responses HTTP 保活**：普通/透传上游等待响应头期间使用已有心跳器，跨阶段/重试累计注释字节，心跳不算语义输出。已暂存但未可见的半帧不阻止独立心跳；已可见半帧中不插入心跳。心跳提交后错误使用 response.failed，不能拼接裸 JSON；已发心跳也不误挡合法恢复。沿用本地断连后回收实际 usage 的规则。
6. **WS server_err**：短拼写与 server_error 进入同一暂态分类、现有重试与账号/模型策略；已输出、已计量或不透明工具执行保护保持。

没有新增用户配置、HTTP 接口、数据库迁移或独立供应商转发入口。新增内部共享读取器替换重复循环，HTTP 等待包装器复用既有心跳与错误输出；元数据合并复用单一规则。

测试将上轮失败复现落入正式回归：工具名四路径、严格终态/多行/部分 usage、真实 handler 只记账一次、两种 HTTP 发送路径实际 socket 的首头保活/后续错误/连续恢复/取消，以及 Codex 真实 Forward 的 header/body、模式、caller、重试和 WS 重连矩阵。旧 EOF 成功断言改为部分 usage + 失败；旧保活断言改为只允许独立注释、不能泄漏暂存半帧，未放宽终态条件或关闭 race。

实现及来源取舍见 [.release 实施记录](../.release/upstream-sync/2026-10-19-protocol-gap-fixes/upstream-changes.md)，最终命令、输入哈希与验证边界见 [validation.json](../.release/upstream-sync/2026-10-19-protocol-gap-fixes/validation.json)。共享工作树同时存在其他计费/Ent/清理任务，本轮统计与独立组合验收不包含那些改动；当前工作树的协议定向集成另有记录。无提交、推送、发版、部署或实际供应商请求。

自动全历史→严格增量裁剪、真实流量性能 A/B，以及上次仅静态发现、未复现的 WS 透传 turn-state 来源接线，不包含在本轮六项完成范围内。

本轮最终验收：独立组合快照按包汇总61个测试包通过（60包初次通过，service修正旧测试后完整重跑通过）；协议/handler专项race、vet及生产构建通过，当前工作树重点集成也通过。业务+529/-560、测试+1051/-147；明细及各次真实退出码保留在validation.json，未将并发工作区整体宣称为本轮全量验收结果。


## 15. 复核发现落地与旧路径清理（2026-10-19）

用户要求按当前 AGENTS 的完整功能清理规则落实 F1–F5。归属为三品牌共享网关；实施验收完成本地合回后，用户授权归入原有相关主题提交并推送 main、TapModels、tokensavy。发布、部署和生产数据库处理不在本次交付范围。来源默认值对照继续固定 `huoji120/sub4api@a3eaf63afc672ffedf7844e6cfc5cb6750d52a3e`；本轮不是重新整分支同步。

- **F1 用量编译缺口与旧详情**：最初冻结的未提交工作树只有一部分删除，残留观测模块仍引用已删除字段。合回前另一任务已将工作区对齐 `05ef8e4c`，完整实现当前紧凑用量与账号状态需求，并清除退役详情、自动 Profile 查询、无消费者字段及相关文案/测试。本轮按最新调用链复核后保留该实现，不把旧快照的局部删除当作整项用量功能撤销。5h/7d/Fable、当前观测与凭据隔离、Sonnet 渠道监控、scope 校验和独立手动重置继续有效；需求沿革、实际消费者及兼容退出条件见 [Anthropic 用量与账号状态](anthropic-usage-observations.md)。
- **F2 Codex 默认请求字段**：在既有 OAuth 转换末尾一次补齐缺省 reasoning、tool_choice、parallel_tool_calls 和 encrypted reasoning include，工具判断基于转换后的 tools 及 additional_tools。保留显式请求值和既有 Lite/compact 规则，删除前置重复 include 调用。无工具时的默认 auto/none 不再误触发工具续链或跳过 WS 预热；真实工具、additional_tools、服务端调用历史和引用继续保留保护，并删除重复工具存在性判断。include helper 仍由当前 WS→HTTP bridge 使用；桥接以后统一执行完整转换时再删除该独立调用，不为已存在的有效调用复制第二套规则。
- **F3 WS turn-state**：透传 metadata 仅在成功发送给客户端之后写入已有凭据来源记录，复用 API Key、分组、session、具体 blob 与真实凭据归属；失败写和放弃的帧不登记。没有增加缓存或票据生命周期。
- **F4 Claude 合成 session**：Messages、count_tokens 和协议兼容入口共享缺省 session 推导，统一调用方作用域和改写前的首条用户文本，删除账号指纹作为客户端区分因子的分叉逻辑。已有显式 metadata 仍按原身份映射处理；无显式 session 的旧请求因修正默认推导可能发生一次标识变化，不保留旧错误算法并行运行。
- **F5 原生 Anthropic 直通 SSE**：CN 原生 Messages 路径复用有界整事件读取，替代单行 data/单独 event 判终态；保持完整原帧的非 data 字段、按请求工具回译、CN 用量归一化、部分用量与断连回收。仅带完整有效 data 的 message_stop 成功，截断/无终态不再被当作成功，不重放已输出请求。尚未写出的半帧不阻止独立心跳；断连与写失败后不再写心跳。

历史数据与代码清理分开：当前用量/状态投影仍消费活动的 `anthropic_observation_*` 字段及原生窗口快照，已退役字段由最新实现的统一白名单排除，不恢复旧详情展示。历史 JSONB 键可能仍在；本轮没有连接生产库核查或删除数据、改写迁移，也不改动当前 scope 与手动重置契约。WS 的 `visible_output_seen`、首语义事件/项目和关闭码仍有当前诊断消费者，保留生产实现及原有严格测试。

测试平台 S0–S4 继续保持撤销状态。修复 `backend/Makefile` 的 e2e 入口，直接运行既有测试包；既有文档及本次用户明确引用的 `test-e2e-local` 只作为同一 recipe 的别名，调用方迁移后移除。既有质量/发布工作流仍只允许手动触发。

本轮逐文件增删、残留核验及最终真实验证结果见 [.release 实施记录](../.release/upstream-sync/2026-10-19-review-fixes-212039/reconciled/upstream-changes.md) 与 [验证清单](../.release/upstream-sync/2026-10-19-review-fixes-212039/reconciled/validation.json)。统计只计相对最新 `05ef8e4c` 的本轮改动；旧候选的删除量和通过记录不替代最终组合验收。


## 16. 三图性能复核与后续方案（2026-10-21）

### 16.1 等待差距与生成速度

图一/二是他方样本，图三是用户当前样本。图二只统计六条完整行，底部 481ms 行因 TPS 不完整不纳入同表。截图不配对；图二/三未显示模型、effort、账号、上游模式或计时定义，不能认定条件相同。

| 指标 | 图一（9 行） | 图二（6 完整行） | 图三（12 行） |
| --- | ---: | ---: | ---: |
| 首字中位数 | 2.66s | 0.5075s | 9.61s |
| 首字均值 | 2.983s | 0.536s | 11.47s |
| 首字范围 | 2.20–4.31s | 0.484–0.657s | 3.85–23.04s |
| 展示 TPS 中位数 | 31.0 | 32.9 | 31.9 |
| 展示 TPS 范围 | 27.9–35.7 | 22.4–35.7 | 14.2–73.4 |

**首先应查首输出之前的等待；这组图不能证明整体吐字速度显著落后。** 当前页面分母包含首字等待，生成阶段还可能含 reasoning、工具和网络等待。不能用 output_tokens/(duration-first_token_ms) 冒充纯文本解码 TPS：要有对应 token 类别和生成区间，缺失就不报告。

图三最大缓存读约 352.3K，图一约 117.8K；高缓存命中与长上下文仍可能有排队/推理/网络等待。缓存 token 不等于上传字节，previous_response_id 不表示历史退出上下文或计费。两条分钟格式总耗时已经舍入，不用显示值反算精确性能。保留慢样本，不将不配对中位数比值称为加速倍数。

### 16.2 仍需处理的缺口

1. **运行实况未知。** 源码支持 WS/zstd，但未取得截图对应的部署 SHA、生效模式、单轮 attempt trace 和客户端首文字时间。端点 /v1/responses、流式标签、低 TTFT、高缓存率都不是实际上游 WS 的证明。
2. **指标不统一。** HTTP semantic 早于内容，visible 也包括工具参数/图片；WS 另用 token-event 判定。HTTP `Forward` 的 startTime 在已选中账号后设置，用量 DurationMs 来自该次 ForwardResult.Duration；不能覆盖入口全部鉴权、用户/账号排队及此前失败账号等待。WS relay 另有每轮起点。保留旧字段历史语义，新增清楚的观测版本/名称；不猜测回填，不为美化指标改恢复门槛。
3. **观测未闭环。** 已有 HTTP 分段日志和 WS conn pick/queue/reused；仍缺成功/失败 attempt 的统一关联、首文字、完整排队和实际压缩帧字节。request_timing_details 仅有迁移，无当前运行读写；UI Server-Timing 不覆盖模型网关请求。先利用既有日志/Ops，确实不够再扩展已存在结构，不新建同用途表/服务。
4. **通用自动增量尚未实现。** 客户端原生增量、response/conn 绑定与恢复材料都已存在；本轮未发现普通全量 HTTP input 自动变后缀的生产路径。不能以 helper 或 keepalive 代替该能力的验收。
5. **预热成本未量化。** 当前 generate=false 默认关闭；开启后在关键路径同步等预热，再发正式 payload。helper 将预热 response 绑定进 state store，但不自动将正式请求改成后缀。优先量化并复用已有连接池预建，不盲开开关、不增加无目标账号探测。
6. **调度工作另在进行。** 开始复核时有 51 个修改/新增路径，涉及负载批量读取、等待、亲和与恢复；属于其他任务，未覆盖也未计为已部署。其完成后应接入同一时间轴验证排队收益，不能用微基准直接解释截图 9.61s 的原因。

### 16.3 官方协议与作者主张

本轮读取官方 WebSocket、Streaming、Prompt Caching、Latency 文档；Codex client.rs 固定到 `f29d55ff0697a88226e8a9811b3c007e5f719cba`（2026-10-20 15:43:19 UTC）。这是源码核验版本，不证明截图客户端/网关使用该版本。

- 官方仍以“上轮 input + 完整 output items”作前缀，非 input 参数变化、历史缩短/修改、缺少父响应等拒绝增量。工具结果 metadata 须相等，只忽略指定的内部 message metadata。实现须固定类型语义和共用夹具，不扩张忽略字段。
- 官方持久 WS + previous_response_id + 新 input 可减少多轮开销；公开文档的约 40% 是长工具链端到端观察，不是首字 500ms 或 ChatGPT OAuth 私有端点保证。公共 API stream_id 多路能力不能直接套到当前 OAuth，也不是本轮优先项。
- 官方同时支持 SSE；未找到“SSE 故意限速中转站”的证据。SSE 是响应载体，HTTP 也支持 previous_response_id；WS 压缩、HTTP zstd、逻辑增量、KV cache 是四个不同维度。
- Codex 有只建连接、不发送 prompt 的预连接，也有请求预热。HTTP zstd 受 enable_request_compression、uses_codex_backend、OpenAI provider 限定；不推广为任意端点，也不以 Δ 大小替代 usage。
- Claude 官方支持 DISABLE_TELEMETRY、DO_NOT_TRACK、DISABLE_ERROR_REPORTING；不能据缺少遥测断言会封号。复制设备指纹、虚构地域或仓库活动没有给出可验证的延迟收益；“1:1、不降智、不封号”不作验收承诺。

一手来源：[WebSocket Mode](https://developers.openai.com/api/docs/guides/websocket-mode)、[Streaming](https://developers.openai.com/api/docs/guides/streaming-responses)、[Prompt Caching](https://developers.openai.com/api/docs/guides/prompt-caching)、[Latency](https://developers.openai.com/api/docs/guides/latency-optimization)、[固定 Codex 源码](https://github.com/openai/codex/blob/f29d55ff0697a88226e8a9811b3c007e5f719cba/codex-rs/core/src/client.rs)、[Claude 环境变量](https://code.claude.com/docs/en/env-vars)。历史指定来源仍为 huoji120/sub4api a3eaf63a；宣传文案不等于另一份已核验源码。

### 16.4 下一批实施顺序

**A：统一观测并确认实际 WS 路径（P0，最高优先）。** 复用现有 handler/Forward/collector/relay，事件定义跨 HTTP、HTTP→WS、WS ingress、passthrough、HTTP bridge 共用，不逐路径复制比较器。

- 时间轴：request accepted → auth done → user slot → account selected/account slot → connection ready → request write complete → first event → first semantic → first nonempty content → first nonempty output_text → corresponding local flush → terminal。客户端 receipt/display 单独测量；本地 flush 不冒称客户端已收到。
- 使用单调时钟偏移与 request/turn/attempt 关联，保留失败账号、恢复和排队；generate=false、握手、心跳不计作新生成请求。重叠 span 不能简单相加成总时长。
- 按 transport/mode、conn reuse/age、queue、模型/effort、请求和实际 service tier、逻辑/压缩字节、负载形态、usage 与结果分组。上游未返回的 reasoning/usage 字段保持 unknown；不记录正文、凭据、密文、原始 session ID。
- 先验证现有日志关联与抽样汇总；需要落库时评估已有结构并补读写/保留期，不因存在迁移就认为已可用。历史兼容 TTFT 只作历史展示，性能报告使用固定定义。

**B：按最慢阶段消除等待（P1/P3）。** 排队长时与在研调度工作合并核查容量/资格/等待；建连长时查线程/凭据/代理隔离下的复用和已有预建；上传长时量化 zstd/deflate；写完至首文字长时按模型、effort、上下文、档位、账号/出口、工具阶段分层。不得把推理等待归因于 SSE，不通过降低 effort、删上下文或改模型伪装协议提速。

**C：被动评估后再发送严格增量（P2→P4）。** 优先处理有可靠租户/线程身份的全量 HTTP→WS 请求。H=上轮 input+完整 output，C=当前全量 input；只有 C=H+Δ、设置相等、真实父 response、credential generation、connection ID、窗口与顺序均可信才发 Δ。P2 分别记录 prefix_match 与 state_ready，前缀命中不代表状态可用。

复用 execution scope、state store、conn lease 和 response collector，确认不足后再加最小有界状态；不新建 Redis 全量对话库。当前 HTTP 请求已带完整 C，候选未命中可以继续发送 C；只带原生增量的请求沿用原恢复规则，哈希不能恢复正文。失败/取消/collector 不完整不更新锚点；换窗口、模型/工具改变、并发/迟到完成、连接丢失使候选失效。已输出、已计量或工具执行不确定时不自动重发。

**D：配对验证后决定推广。** 固定任务、实际模型、effort、工具、档位、账号能力、出口和并发，交错比较原生客户端直连、当前网关 HTTP、当前网关 WS，以及条件成立时的 WS+增量；压缩独立分组。各组独立建立会话状态，冷/热连接与冷/热缓存分层，不让一组替另一组预热却不计成本。

首批诊断建议每个可行条件至少 30 个完整配对任务，覆盖首轮、10–20 次工具往返、100K/300K 长历史、并发子线程、重连和 compact；这只是采样起点，不能保证统计充分。真实付费试验先确定预算再执行。报告端到端首文字/任务完成时间 p50/p95、样本量、置信区间或波动、错误/回退率、恢复代价、wire 字节和 CPU/内存。500ms 仅是他方观察，不能无条件写成 SLA。

### 16.5 清理与验收

- 统一观测时收敛重复计时，保留计费/恢复规则；指标按版本/名称区分，不静默重解释旧数据。兼容字段仅因实际展示消费者保留，并写明迁移后退出条件。
- 增量只改现有最终 payload 决策，不复制 bridge、连接池、调度器、压缩器、身份映射或测试平台；新增状态必须说明现有结构不足和容量上限。
- 扩展现有 transport、pool、execution_scope、recovery、SSE、usage metric 用例，校验实际发送、后续轮、工具/计费一次、跨租户拒绝、窗口切换、取消和完整终态。只测分类器或只看到小 payload 不算通过。
- 低延迟必须保持上下文、模型/effort、工具和安全语义；禁止提前填 first_token_ms、把 heartbeat 当输出、删慢样本、关闭必要恢复或隐藏失败来改善数字。代码清理与历史数据处理分开。

### 16.6 上一轮复核证据与边界

本轮只更新本方案和本地复核记录，业务代码、测试、接口、配置、独立执行路径和迁移均无新增/删除。未推送、部署、访问生产数据库或调用真实供应商。截图对应的实际传输和延迟根因仍需运行证据，不能宣称已解决线上首字延迟。

截图逐行数据、冻结源码、官方源码副本、实际回归命令/结果见 [本轮复核记录](../.release/upstream-sync/2026-10-21-latency-review-095216/review.md)。验证在干净的已提交 HEAD 导出上运行：service 定向用例/子用例 71 项、repository 请求压缩 10 项、前端指标 57 项全部通过；包括相同流在 semantic/visible 下记录不同首字时刻的既有回归。没有执行全量测试、race 或真实付费性能 A/B；不把其他任务的在研调度修改带入结论。


## 17. 复核后首批实施（2026-10-21，本地未部署）

### 17.1 实现边界

本次先落实 A 的可运行观测基础与 C 的 P2 被动评估，并提供离线汇总；不把缺少真实运行数据的 P1/P3 调优、P4 裁历史或 D 配对 A/B 写成完成。P4 仍以第 16.4 节的收益和状态证明为门槛，没有预埋长期关闭的另一套发送逻辑，没有改变模型、effort、上下文、计费、超时、故障恢复或默认预热策略。

- 复用 `openai_forward_timing.go`，HTTP、HTTP→WS、WS ctx_pool、passthrough、WS→HTTP bridge 共同使用 timing_version=2 的 request/turn/attempt 时间轴。attempt 是网关实际调用边界；HTTP 客户端内部透明连接重试不伪装成新的客户端请求。失败 attempt 和换传输均保留。
- HTTP 起点为 Ops 中间件收到请求；WS 起点为接受该轮完整请求帧，不能把升级连接后的用户空闲计成首字。后续 turn 的 timing root 单独更新，同时保留原连接的取消、准入和计费 context；不会由旧 ctx 覆盖新轮次。预热只记录耗时，不新增 generation 样本。
- 明确区分首完整事件、首语义事件、首非空内容、首非空 output_text、对应成功本地写出/flush 与终态。reasoning、工具、图片、心跳和空 delta 不能充当首文字。HTTP SSE 依现有事件边界提交观测；丢弃未发送缓冲时同时丢弃 flush 标记。缓冲 JSON 单列 buffered_response_complete/written，不反推首流式文字。
- actual transport、reason、连接复用/获取/等待、模型/effort、请求及上游返回档位、输入形态、实际 usage 与结果用于分组。缺失 usage/reasoning/档位保持缺失。HTTP 记录编码前语义字节及编码后 body 字节；这不是含 TLS/HTTP 帧的总流量。WS 压缩帧字节和未被接口提供的连接年龄保持 unknown。历史 `first_token_ms`、账单展示 TPS 和费用字段保持原义。
- 诊断不写数据库、不记录正文、凭据、原始 session 或 P2 哈希；既有 request ID、账号 ID 用于关联。未接入 session 级传输占比；离线工具明确返回 unknown，不能由请求占比推断会话比例。

### 17.2 被动增量观察

普通 HTTP 最终请求在全部出站变换之后、压缩之前开始观察；支持原生 HTTP 和 HTTP→WS，复用已有 state store 的私有有界摘要及原响应 collector。插件 RPC 的实际供应商传输不可见，单列 plugin，消费并作废旧锚点，不计作原生 HTTP/WS 增量候选。非流式客户端收到缓冲 SSE 时未取得逐 item 完整性时间线，锚点保持 output_incomplete。原生 WS 客户端和 WS→HTTP bridge 本次不新增 P2 样本，避免与官方客户端已有增量混算。真实 HTTP/SSE 多轮接线回归检查最终请求字节完全一致、每轮只有原有一次发送。

比较采用更窄的 canonical JSON 严格相等子集，并非完整 Rust typed comparator 的等价实现。H 必须为上轮完整 input + 完整 output，C 为当前全量 input；所有非 input 设置（包括 metadata）保守参与比较。未知结构、重复 JSON、歧义 null、不完整输出和不支持的 item 明确不可判定，不猜测缺失默认值或剔除客户端元数据来提高命中率。

摘要按认证 key/user/group、原始 execution scope、实际账号/credential owner、有效凭据、端点、窗口和代理/传输设置隔离。仅明确完成且未取消/失败的轮次能发布锚点；并发、迟到完成、换窗口/账号/凭据、淘汰及 compact 使旧锚点失效。保留实际 response/connection 锚点，内容仅存进程随机 HMAC 与长度。没有另建 Redis 正文库或后台 goroutine。

限制为 5 分钟 TTL、1024 scope、每段历史最多 2048 items、单次 JSON 最大 **2MiB**、摘要状态预算 **16MiB**（含在飞摘要），昂贵快照解析另设 **8MiB 在飞原始字节预算**，忙时立即返回 observation_busy。解析预算不是 Go 峰值堆保证；临时 JSON 分配仍有放大，应观察真实并发与 GC。起初 8MiB 基准每次需约 56–78ms、64–80MiB 累计分配，故首批主动收紧上限；最终接近 2MiB 的本机基准为 begin 19–23ms、终态解析约 15.6–16.0ms，累计临时分配约 15.4–19.4MiB（非峰值堆）；超限只丢失观测覆盖，不阻断或改变网关请求。

报告严格区分 prefix_match 和 state_ready。只有持有健康 WS lease、明确窗口、原有父响应映射与同一连接同时匹配，才记录 begin 时刻 state_ready=true；它不是后续发送授权。HTTP 没有 WS 状态时始终 unknown。候选字节是当前 item 序列化长度之和，不含分隔符/封装/压缩，**本节首批 P2 实施不改变实际发送字节**；当前 SIWC WS 实际增量见第 17.5 节。

交付时并发收到已发布的 SIWC 独立授权改动，保留其 HTTP/SSE、WS bridge、grant 归属和禁止接受后重放契约。计时和 P2 汇总按 authorization_mode 单独分组；SIWC 同样参与 HTTP 完整历史的被动候选比较，复用实际请求携带的原子 grant 快照隔离授权，换授权或有效凭据使旧锚点失效，不另查凭据或发送探测。首批交付时 SIWC 上游为 HTTP/SSE，因此其 P2 state_ready 保持 unknown，不裁历史、不注入 previous_response_id。官方 SIWC 限制针对 HTTP；同一已认证 WebSocket 连接内允许响应续接，不能据当前 HTTP 桥接实现推断 SIWC 协议不支持增量。后续已独立接入 SIWC 原生上游 WS 和同连接增量，当前能力及统计见第 17.5 节；普通 HTTP 的 P2 仍不裁历史。依据：[SIWC Preview limitations](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)（2026-10-21 复核）。

### 17.3 使用与后续门槛

沿用现有工具，无新服务入口：

```sh
python3 tools/analyze_gateway_cache.py --latency gateway.jsonl > latency-summary.json
```

输出按协议、计时范围、客户端类别、入口/上游传输、请求/实际模型、effort、请求/实际档位、请求形态分组；汇总 p50/p95、有效观测样本量、失败/重试、可判定覆盖率与条件命中率。缺失时刻不填零，重试不重复计算客户端 generation，混合传输请求单列。原有缓存分析命令行为不变。汇总不是配对实验，不能宣称加速倍数；本地 flush 不代表客户端 receipt/display。

下一步应在获准的部署环境取得同窗口日志：先看排队、连接、写请求至首文字及真实 HTTP/WS 占比，再决定 P1/P3 的具体变更。P2 覆盖率/收益和 WS 状态证明达标后才实施 P4。真实供应商配对试验需要另定预算，当前未调用。

### 17.4 清理、保留与验收

已替换旧 HTTP-only timing 主体，清除原流式路径重复 semantic 标记、WS 重复耗时/首字日志字段及无调用方的 `openAIWSFirstTokenMsForLog`；不保留并行计时服务。原有 `FirstTokenMs/first_token_ms` 由用量落库、前端账单和历史报表实际消费，保持兼容；退出条件是这些消费者与历史展示明确迁到版本化新指标。既有完整恢复材料仍由真实恢复路径消费，摘要不能替代；只有恢复契约和所有调用方一并变更后才可移除。

本次不撤销传输功能，不删除历史迁移或生产数据；旧测试平台继续撤销。新增对外 HTTP/WS 路由、配置项、依赖和数据库迁移均为 0；没有新生成出口。内部新增 1 条被动观测路径及现有离线 CLI 的 1 个分析模式，relay 使用只读回调，不复制网关/调度器/压缩器/身份系统。

精确增删、最终验证及隔离组装证据见 [实施记录](../.release/upstream-sync/latency-implementation-20261021-ub2xu9vu/implementation.md)。该记录对应首批本地实施时点；随后已归并推送，不代表发版、部署、数据库迁移或付费上游试验；不能据本地通过宣称线上首字已达 500ms。


### 17.5 当前 SIWC WS 与观测口径（2026-10-22）

SIWC 客户端 WS 已能按既有全局/账号配置走原生上游 WS。复用同连接的授权、完整历史与轮次协调器；只有同授权、同连接、完整 input+output 前缀及设置一致才发送 previous_response_id + 增量。连接、凭据或设置改变时不能沿用旧锚点，写出后不以 HTTP 回退或重连重放。SIWC HTTP 及显式选择 HTTP 的 WS bridge 继续发送完整历史。详情见 [SIWC 当前实现](SIWC_UI.md) 和 [来源及实现状态](siwc/upstream-changes.md)。

成功写出 SIWC WS 帧后，在既有 timing_version=2 attempt_summary 的 incremental_payload 记录 full_json_bytes 与 sent_json_bytes。两者使用相同 WS JSON 封装及编码规则，包含父响应字段的开销，比较全量上下文与实际发送的 JSON；不是压缩帧、TLS 或客户端接收字节。失败写出不发布测量，不额外发送探测。原非结构化 ingress_ws_siwc_write 字节日志已由此统一替换，避免重复口径。

离线分析按有测量的 attempt 汇总 actual_payload_json_bytes_saved；可为负数（增量引用开销），无测量为 null；actual_payload_measured_attempts 给出覆盖数量。P2 candidate_semantic_bytes 继续单列。actual_wire_bytes_saved 保持 null，不把未知帧压缩效果计作零。原固定零的 actual_incremental_bytes_saved 已移除，全仓仅旧分析测试消费该字段，无需保留兼容别名。

Claude 原生流记录请求模型与实际响应模型，分析分组保留 protocol、timing_scope 和 actual_model；未知保持 unknown，不按别的模型推断。其 response_stream 时钟从进入响应流处理开始，不包含此前建连/上传，不能混入 Responses 的 upstream_attempt 或完整 request 时间线；原账单 FirstTokenMs/TPS 继续使用既有口径。相同请求的嵌套流观察不算新的重试，预热仍不算客户端 generation。

本次修正观察与汇总，不改变传输选择、增量条件、请求内容、恢复、计费或增加配置。线上首字/TPS 收益仍须同条件运行数据验证。
