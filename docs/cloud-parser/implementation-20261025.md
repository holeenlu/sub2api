# Cloud 解析器重构实施记录

日期：2026-10-25。依照 [已批准方案](../ENGINEERING.md#req-cloud) 的 D1–D8 实施。研究快照与附录保留原 SHA；本记录说明实际替代的代码和验收范围，不将历史设计快照改写为当前实现。

## 实现与职责

公共鉴权、模型权限、账号选择、原生 OAuth/wham 准备、并发配额、价格快照和账务机制继续沿用。Cloud 仍是账号级优先开关，不新增授权、模型列表、产品验证门槛、接口、配置或数据库迁移。composite 目录继续只覆盖已有 Cloud 模型能力及 ETag。

Cloud 处理链为：保存改写前 Responses 输入 → 公共策略求值 → 每进程一次解码 → 公共内容规范化 → Cloud 执行计划 → 现有 owner/actor → Responses 事件投影 → JSON/SSE/WS。原有 cloudProtocolRequest 演进为同一载体，没有并行解析器或新状态平台。

- L0：共用 JSON 唯一键与结构保护；数字使用 UseNumber。重复键、深度、节点耗尽分别报告；保留入口、RPC、队列、结果与 retained 字节保护。
- L1：复用 legacy alias/Chat 消息转换、角色/文本、空图片、tool schema 修正、裸压缩标记归位与去重；标准 tools 优先，纯 Lite 按增量顺序合并，命名空间保留未变化成员。legacy 适配不删除 Cloud previous_response_id。
- L2：矩阵决定非执行提示的 drop 与执行内容的 map/reject。未知非执行键使用通配 schema 路径留痕；错放的已知工具/输出/密文字段不能被当作提示悄悄删除。
- L3：只映射 thread/start、thread/inject_items、turn/start 和 dynamic tool RPC。禁止调用整个原生 OAuth 出站转换器、原生目标 Lite 反向编码或修改账号身份。

Resolve、账务强度求值、Run、actor 使用同一计划投影。继承设置后仅编译已解码的结构，不 marshal 后重新解码。跨实例命令传送应用公共策略后的 JSON 字节及 owner 身份；不序列化可信执行计划，远端 owner 在本进程解码一次，并继续独立校验归属、版本、取消和副作用状态。JSON 结构扫描、摘要，以及独立 RPC/工具 arguments 载荷的解码不等于第二份客户端请求语义解析。

## D1–D8 与已知差异

| 决定 | 实施结果 |
|---|---|
| D1 | client_metadata、prompt_cache_key、本地标识及无 RPC 载体的采样/缓存/遥测提示按既有资源/类型保护消费并留痕；reasoning.context 与 text.format.description 明确 drop。不承诺这些参数仍影响上游生成。 |
| D2 | 单次解码、只读投影、同进程计划缓存；WS 缓存按请求重置。旧字节引用提取、重复入口转换和原始 body 账务解析已移除。 |
| D3 | 私有 Go 矩阵派生目录覆盖、字段规范化、拒绝与诊断 ID；矩阵夹具、自检及下表一致性测试共同约束。 |
| D4 | 保留字段级继承与现有设置变化处理。工具续接继承执行设置；新用户轮省略继承线程值，outputSchema 按轮清除。与无状态 Responses 顶层 instructions 不自动继承的行为有差异。include 纳入继承和摘要，摘要只包含有效执行/输出设置。 |
| D5 | 有效 custom grammar 接纳并写入描述。上游不强制 lark/regex 合规，客户端仍须处理不合规输出；这是部分支持。 |
| D6 | 仅原有两级历史约减，表驱动、有决策记录，同账号新线程、共用两次预算。推理已产生输出/用量、pending 工具、未知副作用或归属失效时不重放。第 3 阶段不执行。触发条件含注入阶段的上游丢弃线程及 JSON-RPC 参数拒绝（`-32602`/`-32600`，2026-10-25 生产故障后补充）；阶段数不变。 |
| D7 | 移除工具 256、历史 8192 两种计数门槛；保留 OpenAICloudMaxRetainedBytes（128 MiB）及既有传输保护，不代表上游无限容量。 |
| D8 | 失败通过现有 ops 事件导出 kind=cloud_adaptation；成功非平凡决策写带请求上下文的 WARN。最多 64 条、16 KiB，标注省略计数；只含路径/动作/矩阵 ID，不含正文、密文、图片、参数值或凭据。完整成功持久化不在本轮范围。 |

非默认 service_tier、store:true、强制 tool_choice、json_object 仍明确拒绝；非 strict json_schema 已改为本轮最佳努力 JSON 提示（schema 不丢弃、不强制转 strict，仍非严格保证）。client tool_search 按已发布旧会话修复映射，server 执行模式仍拒绝，目录仍保守声明 supports_search_tool=false。图片保持 MIME 与原字节，不引入 BPS 格式名单；远程 URL、file_id、detail:low 没有对应承载。用户图片/历史可保留 original，当前动态工具图片回复复用已确认中断后的同根历史交接保留 original，不静默降级；必须收齐已发布工具结果并确认前段用量，超大原子条目在中断前预检，未发布调用由确认中断取消。

不透明 reasoning/compaction 密文保留原文。纯密文 agent_message 无可读正文时整条丢弃；多段明文以换行连接。无密文 reasoning 留在公开历史供匹配，注入副本省略该项；其他客户端 item ID 仅在注入边界删除，call_id/owner 身份不改写。无 call_id 具名投递在两个入口均转为原 output 用户消息。

## 缺陷与入口收敛

| 项目 | 实际处理 |
|---|---|
| F01/F02 | 消除 Run/actor/继承后的整份重复解析；早期路由和执行使用相同过滤后的历史/工具回复视图。 |
| F03/F04 | handler 保存 bootstrap 前输入；HTTP/WS 注入 JSON 字节一致。无密文 reasoning 不注入，不触发客户端 rs_ 引用查找。 |
| F05 | include 进入继承和有效设置摘要；移除未消费的 includeEncryptedReasoning 标记，密文返回仍按已有投影。 |
| F06/R1–R3 | Cloud 不再执行原生目标 Lite 包装；共用压缩标记语义；HTTP/WS 均在编译前执行现有账号图片工具剥离策略。 |
| F07/F11 | legacy 完整转换后的采样提示、access_programs 等按矩阵消费，不再由封闭顶层名单误拒绝。 |
| F08 | 移除数量门槛，区分 JSON 资源/重复键错误，既有字节保护仍生效。 |
| F09 | 本地/远端 owner 的结构化拒绝保留 code、param 和决策；引用及请求级错误作为 WS 当轮错误，健康连接可继续使用。畸形传输 envelope 仍执行原生关闭策略。 |
| F10 | namespace 响应还原、custom 参数编码、JSON 检查和 schema/压缩公共规则按语义共享；历史增量工具合并不替换为原生同批声明冲突算法。Cloud custom inputSchema 保留自己的 additionalProperties:false；公共 schema 含 input 描述且未关闭额外属性，两者语义不同，不作机械去重。 |
| R4 | 真正 legacy /responses/compact 仍遵守原边界；stream:false 压缩信号从原 Responses 输入编译，避免被 handler 路径改写误拒绝。 |

实测追加修复：过滤或约减后注入历史为空时跳过 thread/inject_items。不能把 null/空历史发给上游，再将其协议错误当成模型/密文能力不足；请求与账务根、后续副作用检查均保留。

## 矩阵与文档契约

以下前三列由实现矩阵核对；每项须有夹具命中。矩阵只说明 Cloud 协议适配，不替代公共鉴权、资源、限流和上游错误分类。

| ID | 动作 | 分类 |
|---|---|---|
| CLOUD-REQ | drop | policy |
| CLOUD-TOOL | map | protocol-fact |
| CLOUD-CHOICE | map | policy |
| CLOUD-STORE | default-only | policy |
| CLOUD-FORMAT | map | protocol-fact |
| CLOUD-TIER | default-only | policy |
| CLOUD-SEARCH | map | observed |
| CLOUD-LITE | map | protocol-fact |
| CLOUD-CONFIG | drop | policy |
| CLOUD-AGENT | map | policy |
| CLOUD-CODE | reject | policy |
| CLOUD-HISTORY | map | policy |
| CLOUD-CIPHER | map | observed |
| CLOUD-IMAGE | map | observed |
| CLOUD-PART | map | policy |
| CLOUD-META | drop | policy |
| CLOUD-REASONING | map | protocol-fact |
| CLOUD-INCLUDE | map | policy |
| CLOUD-CUSTOM | map | policy |
| CLOUD-JSON | reject | policy |
| CLOUD-RECOVERY | map | observed |

## 验证与证据

- 先运行失败回归：非执行字段、旧数量边界、过滤后 owner 路由、无密文 reasoning、include 摘要、JSON 深度、schema 与原生差分、handler 非流式压缩、错放执行字段、空历史注入。失败原因与修复对应，不以修改旧断言代替独立回归。
- 本地协议回归覆盖 JSON/SSE/WS 九种跨请求续接组合、混合 namespace、增量目录、取消、失败、幂等、历史防篡改、用量与单根结算。原生/BPS 作为自身既有契约对照，不把 BPS 当作 Cloud 能力标准。
- 本机 Codex CLI 0.159.3 与桌面内置 0.164.0-alpha.11.1 运行实际客户端到网关：均一次请求成功，16 个动态工具到达 thread/start。0.159 使用生产目录字节的本地文件覆盖，0.164 实际读取网关目录 URL；上游 RPC 为本地替身。完整固定样本另覆盖 106 工具及角色/图片/历史形态。
- 使用本机官方登录账号，经生产 Sessions.Run 连接真实 codex-cloud-backend.chatgpt.com，gpt-6.1-sol 已验证标准/Lite 完整目录请求、跨 manager 工具续接、串行工具与输出 schema、上传 PNG、工具返回 PNG、托管搜索、图片生成及无效密文的约减恢复。验证用内存路由/结算存储，真实用量与一根结算已检查；不等于生产 SQL 扣款或线上部署验收。
- 密文跨线程回放测试观察到上游拒绝后执行约减并完成。它证明可观测的安全恢复，不证明原密文在新线程被解密接受。图片首轮有一次归档未确认，独立重跑同项后归档与释放通过；不隐瞒这次上游清理波动。
- 广泛对照曾发现原生 API-key WS 的 BoundedPreludeAndLiveFlush/true 断言失败；在未改动基线 d1e6e924d 单独复现相同 request/空 scope 差异，已在验收记录中列为既有问题，不借本轮修改其实现或夹具。
- 最终定向测试、race、基准和发布映射由本任务现有 validation receipt/交付记录保存。源码验证不因品牌提交 SHA 改写而重复；不在生产双发请求。

## 升级与未实施项

本次会话协议版本为 2026-10-25.parser.1，因为设置摘要/内部命令契约变化。部署应让活跃工具会话自然排空；旧会话不能按新摘要盲目续接，客户端可按明确的过期/归属错误补发完整历史。账号 OAuth、Cloud 开关和目录权限保留，不恢复能力验证准入，不清空用户历史或账务事实。本任务不执行发版、部署或数据库迁移。

继续开放：service_tier 计费映射、D8 完整成功持久化、仅文档候选的第 3 级恢复、孤立工具输出与输出 schema 关键字规范化两个 L1 候选。grammar 强制、Cloud 无逐轮 reasoning.context 控制等差异保留说明，不能称为全部语义与原生等同。

## 合并旧会话修复

本次以已验收并推送的 main d1e6e924d（TapModels da00ff139、tokensavy 557c7ec8d）为组合基线。保留其轮中 user/developer/system 消息、client tool_search、可信中断后的同根工具目录换线程、精确数字历史比较和安全 RPC 诊断。上述能力的具体前置条件见主方案“旧会话接入实施增量”；该增量优先于历史研究表中的一律拒绝描述。

整合时移除该增量内部整份历史 marshal/decode 的副本构造，改用计划的结构副本；原始定义在 source 保留，执行/继承使用同一有效视图。工具发现的早期 owner 投影包含 tool_search call/output，不能因后续降低为 function 而失去归属。有效摘要另有不含工具目录的投影，只有工具相关变化才可进入已验证的换线程路径，include/生成设置变化仍有约束；该投影不成为另一套请求解析器。矩阵登记 client 映射与 server 拒绝，并保留旧会话全部回归。

此次组合不恢复旧的轮中消息拒绝、原始字节引用提取或死的工具搜索错误分支。完整 D8 错误留痕覆盖编译前 owner 查找失败，已有 drop/map 决策不因提前返回或 WS context 替换而丢失。


## 四向历史切换修复（2026-10-25）

覆盖原生→Cloud、BPS→Cloud、Cloud→原生、Cloud→BPS。公共入口只解析续接身份和调用配对，不能因为旧 Cloud ID 就对整份请求执行 Cloud 目标能力规则；该规则仍在最终 Cloud 编译时执行。完整调用/结果对可以作为已结束历史迁移，不要求虚构新用户消息。只有活动 owner、显式 previous、孤立工具结果仍需要原执行归属；本轮选中 Cloud 后保留最后工具结果的幂等缓存，不将它变成跨协议强制路由。

具名独立工具上下文的 input_text/text/output_text/refusal 由公共小函数规范化，保留文字、图片顺序和额外字段供目标校验。删除 BPS 重复文本分支，不引入其工具包装、图片限制或授权规则。源历史保持不变，目标注入副本单独构建。

Cloud WS 复用已有输出采集器，分别处理实际发往 Cloud 的 previous/delta 与本 socket 累计的完整迁移历史。显式账号协议变化复用原生配置刷新和完整请求重建：store:true 也不能携带外协议 previous；迁移检查覆盖原请求及拼接历史中的 Cloud 引用，活动 RPC 不转移，拒绝轮不污染成功历史。重新进入目标协议时清除旧 Cloud 归属/账务上下文，使用现有准入及当前凭据，不新增自动降级或产品开关。

验证以先失败的文本形态、历史路由、WS 输出累计和四向配置刷新回归为起点；源协议的真实适配器输出进入目标适配器的 JSON/SSE/WS 夹具与同 socket 切换另行覆盖。执行结果以本轮验证回执为准，本地替身、真实 Cloud 和生产验收分别报告，不以模拟成功宣称四个真实上游方向全部验收。


本轮参照已发布的 BPS 重连快照（62184ca51），将客户端工具目录随公共 WS 已确认历史保存；Cloud/BPS 桥接在完整同锚点证明下补齐省略目录；原生 WS 只在其既有工具结果续接条件成立时继承，普通新用户轮的省略保持原生语义。显式空数组不被旧目录覆盖。只保存目录的 RawMessage，不重复保存图片请求。现有 BPS Redis 快照恢复及成功交付提交规则已保留；本轮没有把 BPS 的容量计数或专属开关引入 Cloud，也没有把待回复 Cloud RPC 当作可恢复的历史快照。

## 公共政策收敛及生产迁移问题（2026-10-26）

- 错误：原生和Cloud共用终态账号政策，HTTP/SSE/WS共用展示规则。owner的后台终态同样处理，root/thread/turn标记防止重复；旧owner的前台fallback必须重新核对授权代际，不能处罚替换后的身份。展示规则只修改交付副本，原始状态继续决定恢复、健康与持久账务。本地状态依赖失败不作为账号健康样本；HTTP/SSE与WS均保留真实上游状态和对客状态的区别，本地请求拒绝与网关服务失败分别归因。
- 账务：原生/Cloud使用共同三态政策；Cloud log_only使用既有幂等账本事务记录日志，失败不ACK，重复ACK不重记、不扣费。v2读兼容（含完成前轮的reasoning继承读取）已准备，writer仍v1；部署读兼容版本并确认旧消费者退出后，下一交付再切换写版本。v1按旧解释处理，不能宣称本次写入已采用新simple政策。没有新增SQL、账本、产品配置或授权流程。
- 输入：既有legacy、文本/角色、空图、schema、namespace共用路径保留；没有把不同语义的Lite历史合并强塞进原生打包规则。wham请求和响应解析抽到现有quota service；Cloud不查询界面reset-credit详情。
- 62384：生产只有authorization_changed停止标签和可信部分用量，不能据此断言真实撤销或JWKS故障。已建立owner改查持久事实，不重取token/签名密钥；新连接仍走完整凭据检查。依赖失败保持fail-closed并单独分类，真实撤销/关闭/代际或设置变化仍及时停止。
- 62378：生产注入上游报告11,715,065字节超过4,194,304字节，属于消息解码拒绝，不是工具字段或模型上下文权限。真实探针单次6,291,979字节请求报同样4 MiB边界；同一线程三次约2 MiB追加全部成功，累计历史6,291,919字节，响应均2字节。未启动turn/start、未执行工具、未发送用户历史；没有远端完整历史逐字节回读。
- 分批：按完整RPC编码计算大小，2 MiB目标、4 MiB观测边界减封装预留。全部item先检查再发送，逐批检查取消；切片视图保留内容与顺序，单条超过批次目标仍可独立发送。单条无法放入上游消息时明确413。部分成功后的失败不重发原前缀，不引入默认摘要压缩；大小错误不走密文约减。
- 诊断：保留真实握手HTTP状态、RPC方法/代码、gRPC大小拒绝的数值；注入语义错误映射不再丢弃底层RPC事实。仅安全枚举/数值进入导出，不保留provider正文或凭据。

官方App Server文档将thread/inject_items定义为追加原始历史且不启动用户轮次，应答为空对象：[接口文档](https://learn.chatgpt.com/docs/app-server)。4 MiB为上述真实Cloud路径的实测边界，不宣称是所有App Server部署的固定限制，也不是整个线程的历史总量上限。

兼容解码只服务仍有读取方的v1会话/未结算或归档事实；不得按天数盲删。未启用第三阶段历史约减、能力验证门禁、独立模型名单、调度计数限制或自动协议fallback。真实上游注入探针、本地完整链路测试与生产验收分别报告，不能用其中一种代替另一种。
