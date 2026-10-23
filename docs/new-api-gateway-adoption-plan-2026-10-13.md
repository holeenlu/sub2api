# New API 全网关更新吸收审查与实施计划

实施状态：已于 2026-10-14 完成落地和二次复核，详见 [实施与验收报告](new-api-gateway-implementation-report-2026-10-13.md)。改动未提交；本计划下方研究记录保留其原始时间口径。

日期：2026-10-13（Asia/Shanghai）。归属：KDAN / TapModels / Tokensavy 共享网关能力。

## 1. 基线、范围与结论口径

- New API 固定源码：`ef60ef27f36e418143fa4abf0c9025ba6f17fba7`；提交时间 2026-10-13 19:48:01（北京时间）。本轮通过 GitHub API 确认 main 头部。
- 最新发布标签：`v1.0.0-rc.44`，发布时间 2026-10-12 21:42:01；发布标签不等同于上述 main 头部。
- Sub2API 固定对照：`49b29ef158e6319cfd6b1227781307de5f8f870e`。研究期间有其他任务同步官方上游，实施前必须重新对照发生变化的文件。
- 审查范围：rc.37–rc.44 发布记录，main 最近 160 条提交的标题筛查、候选项 diff 与调用链核查，rc.44 后四个提交，以及此前研究中值得吸收但未纳入 thinking 专项的请求、转换、流、重试、结算、WS 与资源管理设计。不是对 160 条提交逐行全审，更不是对 New API 全部历史提交做穷尽审计。
- 本轮产物是审查与实施计划，不表示已实现修复或完成线上验证。旧专项计划、旧验证报告不能替代本轮证据。
- 采用方式：参考语义、测试场景与生命周期设计，融入本地现有实现；不将 New API 整个分支 merge 到 Sub2API，不引入第二个网关或第二套计费规则。
- 分类：**应吸收**、**部分覆盖需补齐**、**已等效覆盖**、**当前不适用**；“已有函数”不等于整条调用链已覆盖。尚需验证的风险单独标注，不能算已证实故障。
- 遵循现行 `AGENTS.md`、`docs/CHANGE_DELIVERY.md`、原生能力边界和 BPS 恢复契约。

## 2. 吸收事项、证据与实现边界

### G01 请求体首次失败必须保留

New API `d10eafb5` / `ef60ef27` 保留请求体读取根因、贯通多次读取与 413。Sub2API 已有大小限制、压缩解码和错误包装，但 `backend/internal/server/middleware/audit_log.go` 在预读失败时不恢复已消费内容或错误。此前隔离 overlay 探针用真实 HTTP 截断体复现：直接读取得到 `unexpected EOF`，经过审计中间件后下游得到空体和 nil。该中间件主要是控制面链路，不能据此声称所有模型入口都受影响。

实施：修复现有预读/恢复过程，保留原始 typed error、前缀、Close 所有权、脱敏和捕获上限；不新增第二套请求体缓存。测试截断、超限、取消、前缀伴随错误、成功和重复读取。

### G02 请求体错误诊断覆盖所有实际入口

`backend/internal/handler/request_body_read_log.go` 已有无载荷的分类日志，但生产调用集中在 Responses 和 Chat，且 413 早返回分支绕过日志。Claude Messages、OpenAI 平台 Messages、Gemini、Images 等入口存在通用错误响应但诊断未统一覆盖。

实施：复用该 helper 及现有各协议错误 writer，补齐入站错误分类与根因记录。超限 413，其他格式/读取错误按实际协议处理；入站失败不尝试上游、不触发 failover、不处罚账号。公共响应只用安全说明，不直接暴露底层 URL、密钥或 payload。

### G03 Gemini 工具 Schema 与结构化输出保真

New API 对无法由 Gemini typed Schema 表达的内容使用 `parametersJsonSchema` / `responseJsonSchema`，对旧 API 能力作区分并记录损失。Sub2API `gemini_messages_compat_service.go` 的 `convertClaudeToolsToGeminiTools` 仍统一调用 `cleanToolSchema`：移除 `$ref/$defs`，折叠 type 数组并改写或删除 enum；Chat 兼容链路也经过此路径。`convertClaudeGenerationConfig` 仅映射 token/采样/停止词，需把结构化输出从源协议到最终请求的全链补齐。

实施：按实际端点与传输能力选择 Schema 字段，保留复杂约束；直接 Gemini、Vertex、Code Assist、Antigravity 分别核实，不能仅按模型名称统一开启 raw JSON Schema。兼容性清理限于明确受限目标，记录字段路径和损失，不记录 Schema 内容。保留本地已有数值边界修复，避免复刻上游仍有损的简化分支。

### G04 补齐结构化输出经过中间协议时的传递

隔离测试确认：Responses 的 `text.format.json_schema` 经过 `ResponsesToAnthropicRequest` 后消失；本地 `AnthropicOutputConfig` 目前只有 effort。Chat → Responses 已保存 `response_format`，但 Chat → Gemini 再经过 Anthropic 中间结构时，约束会提前丢失。只修改最终 Gemini Schema writer 不能解决这个缺口。

实施：在现有 `apicompat/types.go`、`responses_to_anthropic_request.go` 和 Gemini 兼容链中保留格式信息，再按实际目标 API 输出；工具 Schema 与输出 Schema 共享同一目标能力判定。原生 Claude 的格式映射作为本地补齐项单独验证目标能力及 beta，不宣称 New API 的上述提交已经解决了所有 Claude 结构化输出问题。目标不支持时给出可观察诊断，不把“已丢弃约束”当作完整支持。

### G05 多行 anthropic-beta 与 body 能力一致性

New API `767a82b8` 合并多行 beta，保留 `thinking.block_binding`。本地已具备 block_binding、message 级 output_config、工具变化 beta、Vertex/Bedrock 过滤和 body/header 同步，但 `getHeaderRaw` 及部分 `c.GetHeader` 只读取首值。隔离测试确认第二行 `thinking-binding-controls` 在计算最终 beta 时丢失，随后 body 清理可能错误删除受其保护的字段。

实施：增加仅针对 token-list header 的多值归并入口，稳定去重、去空白；beta 策略判定、最终头构建、CountTokens、直通和 Bedrock 使用同一入口。不能修改通用 `getHeaderRaw` 为无差别逗号拼接，也不能绕过 OAuth mimic 白名单、账号覆盖和目标平台过滤。沿用“最终 header → body sanitize → 签名 → 发出”的顺序。未知字段和签名历史按既有约束保留。

### G06 Responses 尾部 assistant 历史的协议语义

New API `e0483122` 处理 Responses 重试时末尾已完成的纯 assistant 文本被 Claude/Gemini 当作 prefill 的差异，并输出 `trailing_assistant_omitted`。本地隔离转换测试确认这类输入最终仍以 assistant 结尾；是否产生供应商 400 本轮没有真实调用验证。

实施：在能保留源协议身份的转换边界处理，并依据目标端点/模型的 prefill 能力启用。只处理已识别的纯文本尾部历史，保留 tool_call/result、签名 thinking、尚未完成的调用和正常中间轮。记录结构性诊断。**不能在 `convertResponsesInputToAnthropic` 无条件全局裁剪**：Chat → Gemini 也借用该函数，必须保留 Chat/Messages 原生 prefill 语义；不添加伪造 user 消息。若不能证明某尾部块符合条件，保留或明确报不兼容，不能猜测删除。

### G07 区分传输结束、生成完成和客户端取消

New API `StreamStatus` 分开记录 EndReason 与 ResponseOutcome；`3f926c3e` 在流确已完成后仍计入正确的性能样本。本地 Claude 与 OpenAI raw CC 已有部分终止检查，但 Gemini → Chat/Claude 兼容流在 EOF 后会直接产生成功尾帧。隔离测试复现：只有 `partial` 文本、没有 finishReason 的上游流，得到 `finish_reason=stop`、`[DONE]` 和 nil 错误。

实施：先修 Gemini 两条兼容链，复用现有流错误、Ops 上下文和请求尝试边界；随后核对 Gemini 原生、聚合非流式与 Antigravity 的同类路径。按协议识别 STOP/MAX_TOKENS/安全阻断/显式 error/缺失终止；安全拒绝不等于传输故障，不能一律重试。客户端已见输出后以协议内失败结束并记录原因，不再透明重放整轮或补成功尾帧。未输出时是否切号仍由现有 failover 判断。

不要把 CC 的“usage 帧可视为终止信号”直接套到 Gemini；其 usage 可能在多个生成帧出现。已完成后取消与生成中取消分开统计；保留已读取用量与首 token 时间。

### G08 转换损失与重试决策统一可观察

New API `conversion_diagnostics.go` 限制并去重转换告警；`request_policy.go` / `relay_error.go` 记录 action/reason/source。本地已有 `OpsUpstreamErrorEvent.Kind/Stage/Scope/Reason`、脱敏、数量/字节预算与重试状态，不能宣称没有重试诊断。缺口是跨协议字段损失缺少统一反馈，现有原因字段在凭证阶段之外还需逐调用点检查。

实施：在现有转换结果/请求上下文中增加一个有界诊断承载，优先记录 schema 降级、结构化输出丢失、尾部历史调整、unsupported hosted tool、推理强度调整；只记代码、字段路径、源/目标协议及最终模型，禁止内容、签名、凭证。先接结构化日志，再复用现有运维 JSON 明细展示；成功请求的诊断不能依赖只保存失败的错误日志。需要新的持久字段时另列 schema 变更，不能假称现有表已能承载。

重试决策融入 `FailoverState`、现有错误策略与 attempt 事件，补齐停止/继续/同账号/换账号的原因与执行结果；不另建平行策略引擎，不直接照搬 New API HTTP 状态码重试表。判定保留错误来源、响应是否已提交、会话绑定、取消、预算和账号健康作用域。

### G09 日志查询和导出的资源边界

New API `633b4c5b` 在查询与导出层限制时间范围，并控制同用户导出并发。Sub2API 已有 CSV、筛选、权限和 CSV 公式注入防护；用户页 `exportToCSV` 按 UI total 循环分页并把全部行保存在浏览器，其他标签页/节点不共享 `exporting` 状态。部分排行已有 90 天限制，但普通日志筛选没有同一个范围上限。

实施：在现有 usage 服务/查询仓库中复用权限与筛选，提供受限服务端导出，采用固定查询截止点与稳定 `(created_at,id)` 游标，批量读、流式写、可取消、DB 超时和每用户并发租约。复用现有权限；用户只能导出自身，管理端仍按现行 usage.export 权限。保留脱敏和 CSV 注入防护，去掉对应旧全量内存分页路径。

这是本轮唯一建议新增业务端点的功能块：普通分页 API 无法在一次服务端操作中控制全量导出的快照、生命周期和跨标签页并发。建议用户/管理导出共用一个实现、各自挂到既有权限路由；实施时精确列出端点数量。查询窗口、最大导出量和租约时长集中配置，不能将 New API 默认 365 天无说明套成我们的数据保留政策；查询范围限制不删除历史数据。

### G10 故障后的用量与结算一致性

New API `0a03088e`、`78bd5b1c`、`55a6cd2a` 涉及转换流失败计费、Gemini 累计用量和断流估算。本地 Gemini 已按最新 usage 覆盖而非逐帧累加；`usage_billing_repo.go` 在事务中按 `(request_id,api_key_id)` 去重，已有 in-flight 预留、图片 hold/recovery 和价格快照。因此不替换资金模型，也不宣称发现了重复扣费。

实施：随 G07 检查每个失败返回是否带回已知用量、最终模型、完成的图片事实与结算身份；补上 HTTP/SSE/WS 转换链从读取事实到最终账单的测试。中间失败尝试不重复预扣，终止/失败只结算一次；累计 usage 不按帧数累加，完成的子任务不因主流随后失败丢失已确认事实。

**范围约束：不默认引入 New API 的断流估算收费或搜索收费规则。** 缺少可信 usage 时记录 unknown/estimate 来源供诊断；若需改变对客收费或定价，单独列决策与迁移影响。独立于该决定，已知 usage 传播和幂等修复可直接开展。

### G11 大请求与转换路径性能：先测量，再选择实现

New API `BodyStorage.NewReader` 和可选磁盘缓存可降低某些重试/透传场景的重复读取成本。本地已有 `PrereadBody.Bytes()` 快路径、分块读取和解压上限，因此不能直接断言磁盘缓存更快或内存必定更少。需要完整 JSON 转换的链路最终仍会物化字节。

实施前以 1/8/32 MiB 合成文本与图片请求、并发 1/16、零/一次重试，记录 allocs/op、B/op、峰值 RSS、TTFT 和 FD/goroutine 回落。先减少已证明的多次 marshal/unmarshal/克隆；确有透传/大文件收益时，才把受限临时文件复用放到现有请求体工具。必须有总量/大小限制、磁盘耗尽处理、0600 权限、取消清理和 reader/owner 分离。无显著收益就不增加缓存配置或新存储层；记录基准作为本项完成结果。

## 3. 全量候选决策矩阵

下表是本轮审查范围内的候选，不代表覆盖全部 New API 历史。P0 表示应先修的正确性问题，P1 表示兼容与容错补齐，P2 表示资源和运营优化；不是已确认安全漏洞分级。

| 项目 | 来源 | 本地结论 | 决策与落点 |
| --- | --- | --- | --- |
| 首次 body 失败、多读者根因 | S01 | 审计预读会吞掉失败，已复现 | 部分覆盖需补齐，P0，G01 |
| 413、错误分类和诊断贯通 | S01 | 已有 413/helper，入口和早返回覆盖不足 | 部分覆盖需补齐，P0，G02 |
| Gemini 复杂工具 Schema | S02 | `$ref/$defs` 等被清理，已复现 | 应吸收，P1，G03 |
| 结构化输出穿过中间协议 | S02/S06 + 本地追踪 | Responses 格式在 Anthropic 中间结构丢失，已复现 | 应吸收并补本地链路，P1，G04 |
| 多值 beta + block_binding | S03 | body 保护已有，多行 header 漏后续值，已复现 | 部分覆盖需补齐，P0，G05 |
| Responses 尾部 assistant | S04 | 转换仍产生末尾 assistant，已复现；供应商拒绝未实测 | 应吸收目标语义，P1，G06 |
| EOF/完成/取消/失败分离 | S05/S06 | Claude/CC 部分已有；Gemini 兼容流伪成功，已复现 | 部分覆盖需补齐，P0，G07 |
| 转换损失诊断 | S06 | thinking/错误日志已有，字段损失未统一 | 应吸收有界设计，P1，G08 |
| 重试原因与账号健康作用域 | S13 | 现有 FailoverState、Reason、预算等应保留 | 部分覆盖需补齐调用点与测试，P1，G08 |
| 日志导出与统一资源限制 | S07 | CSV 已有，批次全量累积/并发边界待补 | 部分覆盖需补齐，P2，G09 |
| 失败流结算、累计 usage | S11/S14 | 有事务去重和覆盖式累计；跨失败链需验收 | 部分覆盖需补齐验证，P1，G10；不先认定重复扣费 |
| 大 body 可重放与磁盘缓存 | S15 | 内存快路径已有，磁盘收益待基准 | 条件吸收，P2，G11；不先引入新层 |
| 推理与正文同 chunk | S08 | 独立 if 分别处理，新增隔离对照通过 | 已等效覆盖本次场景，保留回归 |
| assistant 文本 + tool_calls | S08 | `chatcompletions_anthropic_bridge.go` 合并到同一消息 | 已覆盖设计，随工具矩阵回归，不重复移植 |
| Responses 图片 object 格式、工具结果图片 | S09 | 已有媒体提取与带 URL 的对象；apicompat 与 Gemini 定向测试通过 | 已覆盖本轮相关场景；维持图片出口保护 |
| custom/namespace 工具与调用 ID | S10 | 现有 Chat 桥、Responses→Claude 转换与专门测试 | 不整体重写；S3 补跨 Gemini/Antigravity 全链矩阵 |
| WS 每轮鉴权、配额、恢复 | S12 | `BeforeRequest/BeforeTurn`、会话归属与 BPS 契约已有 | 保留原生架构，S4 验证断线/Close/失败结算；不照搬 WS 中转器 |
| WS 正常 Close frame | S12 | `closeOpenAIClientWS` 已先 Close 再 CloseNow，另有 CloseNow 分支 | 部分覆盖：审计退出路径；不能因有 helper 就宣布全覆盖 |
| Gorilla 1.5.0 → 1.5.3 | S16 | 实际解析 Gorilla 1.5.3；模型 WS 主要是 coder 1.8.14 | 已覆盖，无需重复升版，不据版本推断线上已部署 |
| Sub2API 余额查询支持 | S16 | 是 New API 消费我们的 Usage API | 无本地功能缺失；保留有限/无限套餐、负余额契约测试 |
| 页面 chunk 加载失败重载 | S17 | router 已有失败重载及 10 秒抑制 | 已有相关机制；只在前端阶段检查慢失败重载循环/缓存，不先照搬 |
| 日志价格档位/模型差异显示 | S18 | 有实际成本、模型观察和图片价格快照；全 UI 等价性未证实 | 纳入 G10 展示一致性验证，发现缺口再修；不引入表达式计费引擎 |
| 搜索工具编码/按供应商次数计费 | S19 | 已区分显式 hosted search 与普通同名 function，有现有搜索费逻辑 | 先核对已支持供应商的实际次数解析；编码保真可并入 G03/G08，定价变化单列 |
| 无 Redis 429 文案一致 | rc.44 #7721 | 本轮未证明同类缺陷 | 增加现有限流正常/故障分支一致性验证，不新引入无 Redis 运行模式 |
| 按渠道跳过上游 TLS 验证 | S20 | 会改变连接安全边界 | 当前不采用；确有自签场景应先考虑可信 CA 配置 |
| moejs/视频任务插件/插件 utils.fetch | rc.37–rc.43 | 属于另一套执行与任务架构 | 当前不整体导入；2xx/边界/权限/资源的测试思想用于现有原生任务 |
| 定价组排序、游乐场存储、图标等 UI | rc.44/后续 b4be2a78 | 不是本轮已证实网关缺口 | 当前不作为网关核心改动；不否定日后独立 UI 需求 |

## 4. 分阶段实施顺序

### S0 固定实施基点与建立失败样例

- 从实施当时最新 main 开始，复核本计划涉及文件相对 `49b29ef15` 的差异；已有等效修复则删掉重复工作。
- 把第 6 节六项失败样例转为既有测试文件中的回归，不把临时 overlay 文件直接原样拷入。
- 绘制入口 → 转换 → 最终模型 → build → stream → usage 的小范围路由表，特别标出 Chat 借道 Responses/Anthropic 和各 Gemini 传输。
- 产物：基点、实际修改清单、预期行为表。依赖：无。复杂度：小。

### S1 入站错误与 Claude beta 修复（G01/G02/G05）

- 修改现有审计、body 错误 helper、相关 handler、beta 构建和平台过滤调用点；不改公共错误格式。
- 原因始终可追溯；所有实际启用的入口覆盖超限/截断/编码失败；不会触发上游请求或账号处罚。
- 多行 beta 测试覆盖首行/后续行、空白/重复、大小写 key、账号 override、OAuth mimic、Vertex、Bedrock、CountTokens；策略过滤仍有效。
- 验收：G01/G05 回归由失败转通过，413 和已回写错误不被二次覆盖；相关既有测试通过。依赖：S0。复杂度：中。

### S2 流终止与失败结算（G07/G10）

- 优先修已复现 Gemini → Chat 的 EOF 假成功，再扩展 Claude 兼容和聚合路径；抽取必要的共同 outcome 判断，避免各路径独立扩大规则。
- 先保留事实：终止类型、已输出边界、已知 usage、完成的子任务，再让既有错误/计费/统计消费者使用。
- 验收：空 200、半截 JSON、无终止 EOF、显式失败、有效 STOP、MAX_TOKENS、安全阻断、完成后取消、生成中取消分别得到正确行为；中途失败不发成功尾帧、不透明重放；去重账单与预留释放符合既有规则。
- 依赖：S1 的错误归因；可先实施纯流终止测试。复杂度：较高。

### S3 协议与 Agent 内容保真（G03/G04/G06 + G08 最小诊断）

- 先让格式元数据穿过现有中间结构，再做目标 Schema 渲染，最后处理带源协议身份的末尾历史。先建立有界诊断，避免新增清理行为无痕发生。
- Schema 验收覆盖 `$ref/$defs`、const、oneOf、type 联合/nullable、数值/布尔 enum、空对象、additionalProperties、嵌套上限，以及 v1/v1beta/Vertex/Code Assist/Antigravity 能力差异；互斥字段不能同时发送。
- 内容验收覆盖 custom/namespace/tool_search、并行及延迟工具名、增量 JSON、工具图片、同帧思考+正文、正文+工具、签名 thinking、原生未知字段。只扩展尚未覆盖的独立风险。
- 验收：G03/G04/G06 失败样例转通过；源请求不被就地污染，各次账号尝试重新按最终目标能力构建；原生 Chat/Messages prefill 不受影响。
- 依赖：S0；交付顺序在 S2 后。复杂度：较高。

### S4 重试、WS 与诊断闭环（G08/G10）

- 把已有 decision/attempt 原因补齐到实际调用点；日志展示原始上游状态、本地响应状态和错误阶段，不用合成状态伪装上游返回。
- WS 检查握手、每轮重新鉴权/限流、显式 403 恢复、客户端限制、响应归属、正常 Close 与异常退出；保留 BPS、工具和图片恢复行为。
- 验收：本地参数错误不换号；账号鉴权失败与请求内容错误不混同；429/503 遵循已有 Retry-After、预算、会话与健康作用域；输出后不重放；多次尝试共享正确结算身份；取消后资源回落。
- G08 告警在成功及失败请求都能按 request ID 检索，数量/字节有上限，脱敏测试通过。依赖：S1–S3。复杂度：中。

### S5 查询导出与性能优化（G09/G11）

- G09 服务端流式导出、稳定游标、租约/取消/超时，前端替换旧全量数组；列表/统计/导出遵守同一授权和明确窗口规则。
- 验收：跨用户越权被拒、跨标签页/节点并发受限、取消或进程故障后租约最终释放、查询期间新增记录不重不漏、CSV 防注入、多语言列名和时间边界正确。
- G11 在固定载荷与并发下做前后基准；只有测得瓶颈才提交优化。不得用“引入缓存/升级依赖”代替效果证明。
- 依赖：可与 S3/S4 在独立文件范围并行；不阻塞 S1/S2 紧急修复验收。复杂度：G09 中至高，G11 探索性。

### S6 二次复核与交付准备

- 第一遍按样例和路由验收；第二遍从最终 diff 重走正常/失败/重试/取消/结算链路，检查实现是否意外扩大作用域。
- 跑受影响包完整单测、跨包回归、build/vet、定向 race、gofmt、diff --check、增量 lint；记录全仓 lint 基线。前端变更跑 typecheck、相关 Vitest、build。需要 PostgreSQL/Redis 的去重与导出测试在测试环境执行，不以 mock 冒充。
- 复核请求体与 schema 资源上限、原始请求不可变、goroutine/FD/租约释放、所有新诊断的脱敏及限长。
- 按实际 diff 报告业务代码与测试增删、新增端点/配置/字段/执行路径数量及删除的旧路径。仅 S5 导出预期新增业务端点；其他阶段优先复用入口。
- 按现行授权形成可审查结果；收到覆盖本批实施的提交/推送指令后，共享改动先 main，再普通 merge 至 TapModels/tokensavy，并逐一验证 origin SHA。部署、发版、数据库迁移仍是独立范围。
- 依赖：本批纳入的阶段完成。复杂度：中。

建议按三批验收：**A = S1+S2（正确性）→ B = S3+S4（保真与可观察）→ C = S5（资源优化）**。每批都执行对应 S6 检查，避免必须等资源优化完才交付紧急修复。

## 5. 可复核的完成标准与回退

| 验收维度 | 必须有的证据 |
| --- | --- |
| 请求体 | 同一首次错误在预读/正式处理链路保持；errors.Is/As 可用；安全响应与根因日志对应 |
| 兼容 | 对固定输入断言实际最终出站 body/header；不能只测中间 helper |
| SSE | 记录协议终止与传输原因；区分已输出前后，错误帧/成功帧互斥；假上游验证断开与取消 |
| 重试 | 断言实际尝试次数、账号序列、预算、等待中取消、原始 body 不变与无二次回写 |
| 计费 | DB 去重、重试预留、失败已知 usage、完成图片、每轮 WS；输出零用量必须区分真零和未知 |
| 安全 | 原有 TLS、可信 IP、图片出口、WS 归属与 BPS 授权不回退；诊断不泄露数据 |
| 资源 | 导出内存不随全部行数增长；取消后资源释放；优化前后同环境同载荷对比 |
| 交付 | 固定 SHA、来源、采用/替换理由、实际测试日志、未做的验证和可回退提交 |

回退以批次为单位，优先小而独立的修复提交，避免同时改变资金规则和转换行为。新增导出入口可单独回退前端接入；已有查询接口保持兼容。若诊断需要新持久字段，先明确向后兼容和回退读取策略再设计迁移。监测假成功/转换 400/重试耗尽/结算差异/资源指标；部署灰度需另有授权，不在本轮直接进行。

## 6. 本轮实际验证与限制

### 6.1 隔离探针：六个失败场景，不是修复完成

所有探针通过 Go `-overlay` 从 `/tmp` 注入，未在仓库添加 Go 文件。失败断言表达期望契约，用于证明当前缺口；对应命令退出 1 是预期的研究结果，不是通过验收。

| 探针 | 当前结果 | 对照/范围 |
| --- | --- | --- |
| AuditReadFailure/with_audit | 截断体错误被消费，下游空体 + nil | without_audit 得到 7 字节 + unexpected EOF，通过 |
| MultiValueBeta | 最终 beta 仅首行 other-beta | 后续 binding token 缺失 |
| GeminiSchema | 引用属性变成空对象，`$ref/$defs` 消失 | 工具转换函数级复现 |
| StructuredOutput | json_schema 不在转换后的请求中 | 验证中间结构丢失，未发供应商 |
| TrailingAssistant | 最终 role=assistant | 证明与 New API 处理差异，不宣称线上必报 400 |
| GeminiStreamTerminal/missing_terminal | 自动补 stop/[DONE]，err=nil | valid_STOP 对照通过 |

另外：同一 chunk 的 reasoning + answer 对照通过。主探针共五项失败、两个通过对照；审计探针一项失败、一个通过对照。

### 6.2 既有验证

- `go test -tags=unit ./internal/pkg/apicompat ./internal/pkg/httputil -count=1`：通过（不包含上述 overlay）。
- service 定向集合通过：Gemini 工具/Schema、Chat 兼容转发、工具图片和签名、工具块闭合、usage 提取、beta/Vertex 过滤、图片 hold 恢复；本轮未运行整个 service 包。
- `go list -m` 确认 Gorilla 1.5.3 / coder 1.8.14；不是线上镜像版本验证。
- 本轮未做真实供应商请求、线上流量回放、全后端测试、race、lint、性能基准或 DB 集成测试。它们按 S6 和相关阶段安排，不能引用旧专项报告冒充本轮结果。

本机研究材料：`/tmp/new-api-full-review-20261013/` 内的 `head.json`、`head-final.json`、`releases.json`、`source-commits.txt`、`overlay.json`、两个 probe 源文件、`probes.log`、`audit-probe.log`、`existing-package-tests.log`、`existing-service-tests.log`。临时目录不是永久 CI 证据；实施时将必要样例转入既有测试并归档结果。审计 overlay 源位于 `/tmp/new-api-update-review-20261013/`。

收尾复核确认 New API 头部仍为 `ef60ef27`、本地 HEAD 仍为 `49b29ef15`；计划无占位项，涉及的显式文件路径存在，工作区没有探针 Go 文件，`git diff --check` 通过。本轮业务代码增删均为 0，仓库测试文件增删均为 0，新增业务接口/配置/执行路径均为 0。本计划是新增本地文档，受 `.gitignore:139` 的 `docs/*` 规则排除；以后提交本批成果时须显式纳入，不能仅凭 `git status` 判断其已交付。开始时已有的 `AGENTS.md` / `docs/CHANGE_DELIVERY.md` 改动保留；本轮未暂存、提交、推送、发版或部署。

### 6.3 与此前 Claude disabled 报错的关系

本计划的 beta/历史/Schema/流修复不能承诺消除所有 thinking 参数错误。`claude-fable-5-1` / `claude-opus-5-5` 的显式 disabled 与仅支持 adaptive 的冲突，仍涉及“用户要求关闭思考，是否允许改成仍会思考的 low”的语义。此前计划已经建立能力判断；本轮不把静默改变思考/费用含义混进可靠性修复。可在现有配置策略中单独记录该取舍，不阻塞本计划其他项。

## 7. 可追溯来源

以下提交/文件是审查证据；实施时以固定源码为准，不能仅按 release 文案复制。所有文件链接固定在本轮源码 SHA。

- S01：[保留首次读取失败 ef60ef27](https://github.com/QuantumNous/new-api/commit/ef60ef27f36e418143fa4abf0c9025ba6f17fba7)、[暴露安全根因 d10eafb5](https://github.com/QuantumNous/new-api/commit/d10eafb57e289e971453eb68e882addaa29f8cc9)。
- S02：[Gemini JSON Schema c10780e0](https://github.com/QuantumNous/new-api/commit/c10780e054e6e518c9d0d8a903526663c9b57d75)、[schema.go](https://github.com/QuantumNous/new-api/blob/ef60ef27f36e418143fa4abf0c9025ba6f17fba7/relaykit/relayconvert/internal/shared/gemini/schema.go)。
- S03：[多值 beta 与 block_binding 767a82b8](https://github.com/QuantumNous/new-api/commit/767a82b8764778b847462ebecdc231eacd1e282c)。
- S04：[尾部 assistant e0483122](https://github.com/QuantumNous/new-api/commit/e0483122c7d3693629f0e84473380a8c9506c56a)、[Responses input helpers](https://github.com/QuantumNous/new-api/blob/ef60ef27f36e418143fa4abf0c9025ba6f17fba7/relaykit/relayconvert/internal/oai_responses/req_helpers.go)。
- S05：[完成后取消的指标 3f926c3e](https://github.com/QuantumNous/new-api/commit/3f926c3ef49745be273cd8f753fb691abdd7b836)、[StreamStatus](https://github.com/QuantumNous/new-api/blob/ef60ef27f36e418143fa4abf0c9025ba6f17fba7/relay/common/stream_status.go)。
- S06：[转换保真与失败计费 0a03088e](https://github.com/QuantumNous/new-api/commit/0a03088e0e737e99bc7ca936d3d2276e8d636fb8)、[转换诊断](https://github.com/QuantumNous/new-api/blob/ef60ef27f36e418143fa4abf0c9025ba6f17fba7/relay/common/conversion_diagnostics.go)。
- S07：[日志导出 633b4c5b](https://github.com/QuantumNous/new-api/commit/633b4c5b190dc1af99a2814391354d86948e7943)、[导出并发租约](https://github.com/QuantumNous/new-api/blob/ef60ef27f36e418143fa4abf0c9025ba6f17fba7/service/log_export.go)。
- S08：[同 chunk 正文 5ac75628](https://github.com/QuantumNous/new-api/commit/5ac75628409de7e3adde7d36771700714af50853)、[文本与工具共存 ba0e478e](https://github.com/QuantumNous/new-api/commit/ba0e478e061ee0c824922d89c05620de878103cf)。
- S09：[图片对象形式 13c8e90b](https://github.com/QuantumNous/new-api/commit/13c8e90b8f4b929aa88c2e9616efc462e0068d72)；工具结果媒体见 rc.40/rc.42、`6e9de44a`、`4eb3b916`、`78bd5b1c`。
- S10：[Chat custom tools 335ebcff](https://github.com/QuantumNous/new-api/commit/335ebcff71b843655bf2c64f7a9a33ca21b792ea)、[Claude/Gemini custom tools 1a4166d8](https://github.com/QuantumNous/new-api/commit/1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5)。
- S11：[Gemini 用量与媒体 78bd5b1c](https://github.com/QuantumNous/new-api/commit/78bd5b1cbf7bffc462a515a6c6e27567ace7a4d8)、[断流估算 55a6cd2a](https://github.com/QuantumNous/new-api/commit/55a6cd2a44eaf189bbd565dd2cdfa591d19b06d6)。后者仅作为待取舍方案，不自动采用收费规则。
- S12：[WS Close 801456a0](https://github.com/QuantumNous/new-api/commit/801456a061fb9988957341c4eb5aa8ec20c40de1)、[Responses WS](https://github.com/QuantumNous/new-api/blob/ef60ef27f36e418143fa4abf0c9025ba6f17fba7/relay/responses_websocket.go)。
- S13：[重试判定](https://github.com/QuantumNous/new-api/blob/ef60ef27f36e418143fa4abf0c9025ba6f17fba7/service/relay_error.go)、[策略事件](https://github.com/QuantumNous/new-api/blob/ef60ef27f36e418143fa4abf0c9025ba6f17fba7/service/request_policy.go)。
- S14：[BillingSession](https://github.com/QuantumNous/new-api/blob/ef60ef27f36e418143fa4abf0c9025ba6f17fba7/service/billing_session.go)。
- S15：[BodyStorage](https://github.com/QuantumNous/new-api/blob/ef60ef27f36e418143fa4abf0c9025ba6f17fba7/common/body_storage.go)。
- S16：[Gorilla 3fe394cd](https://github.com/QuantumNous/new-api/commit/3fe394cdba280c00bc566de493b5fc8399b0a7d1)、[Sub2API 余额 63cecab8](https://github.com/QuantumNous/new-api/commit/63cecab824bad1a421d19950004c309bb2054c09)。
- S17：[rc.44](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.44)，页面恢复 #7634、限流 #7721 等。
- S18：rc.44 #7763 / #7446，模型名差异 `972aed19`；本地仅对已读的实际成本/模型观察/图片快照作结论，完整展示矩阵留给 G10。
- S19：[搜索编码/计费 acb347a1](https://github.com/QuantumNous/new-api/commit/acb347a197ff0e22f651be9813b4a1a867342941)。
- S20：[跳过 TLS 验证 c6741c36](https://github.com/QuantumNous/new-api/commit/c6741c36a9a1a729554ad94ee99f21602a214a23)。

相关本地文档：[原始研究](new-api-relay-research-2026-10-12.md)、[此前 thinking 专项](new-api-adoption-plan-2026-10-12.md)、[10 月 13 日增量核查](new-api-update-review-2026-10-13.md)、[需求交付规范](CHANGE_DELIVERY.md)。本计划扩展前两者的范围，不重写此前已验证的历史结论。
