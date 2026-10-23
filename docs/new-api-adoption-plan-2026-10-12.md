# New API 研究成果落地实施计划

制定日期：2026-10-12（Asia/Shanghai）；实施验证更新：2026-10-13。状态：计划内代码与二次复核完成，未提交/推送；最终 unit/race/go vet/build 通过，独立 golangci-lint 的环境限制见验收报告。

## 1. 目标、基线与实施范围

- 依据：[源码研究报告](new-api-relay-research-2026-10-12.md)，New API 固定提交 633b4c5b190dc1af99a2814391354d86948e7943。
- 本地基线：main@c575e66799775e649e08e45053034f90a8fcd96a；开始实施时重新核对 HEAD、工作区及相关调用链，变化只复核受影响部分。
- 需求归属：KDAN / TapModels / Tokensavy 共享网关能力。
- 核心目标：按最终上游模型支持的能力发送思考参数；已有分组权限、请求恢复、计价及签名语义保持成立。
- 工程方法：扩展现有能力目录与转换/发送函数；按独立风险分批提交，替换时删除旧规则，避免重复实现。
- 约束来源：[交付规范](CHANGE_DELIVERY.md)、[原生与保留能力边界](FORK_FEATURE_RETIREMENT.md)、[Responses 恢复合同](OPENAI_RESPONSES_RECOVERY.md)、[BPS 当前适配决定](bps/upstream-changes.md)和[验收记录](bps/VALIDATION.md)。以当前本地行为及已确认需求为准，不把 BPS 来源仓库的默认值当本地设置。
- 继续保留现有降智检测、Fable 独立阈值、可信 IP、图片出口、WS 响应归属、Grok 语音并发与音色归属保护；不整分支合入 New API/ranxi，不恢复退役运营扩展，不新增 RelayKit 运行依赖。
- 计划制定后，用户已授权落地实施并要求最终代码二次复核，明确暂不提交、推送；本轮按此范围完成代码与本地验证。供应商付费验收和生产发布仍属独立范围。

## 2. 总体批次与依赖

| 阶段 | 优先级 | 主要结果 | 前置条件 | 完成门槛 |
| --- | --- | --- | --- | --- |
| S0 最终出站路径与基线 | P0 | 路径清单、行为对照、最小回归夹具 | 当前源码与研究报告 | 能截获真实发送前参数；分清已有覆盖与缺口 |
| S1 Claude 能力规则收敛 | P0 | 现有目录同时服务候选展示和请求适配 | S0 | 模型身份、合法档位与模式测试通过 |
| S2 Claude 出站闭环 | P0 | 转换、原生受控调整、分组策略、预算、计价一致 | S1 | 核心验收矩阵通过；第一批可交付 |
| S3 其他现有协议的同类问题 | P1 | Gemini / OpenAI 及现有兼容供应商的定点补齐 | S0；共用接口依赖 S1/S2 | 每种协议独立验收，不新增供应商产品入口 |
| S4 重试与流式终态边界 | P0 风险核查；按证据修复 | 一次请求是否可重放、是否真正完成可验证 | S0；可与 S1/S2 的非重叠任务并行 | 故障注入证明无盲目重放、终态与资源正确 |
| S5 诊断、资源、计费收口与交付 | P1 | 失败可解释、资源回收、账务回归及交付记录 | 对应功能批次完成 | 定向/集成检查通过，剩余风险明确 |

建议里程碑：M1=S0+S1+S2；M2=S3；M3=S4+S5。S4 若发现本地可复现的重复生成、重复工具或计费问题，立即提为 M1 发布阻断项，不等待后续里程碑。

各阶段按证据和验收结果推进，不预设缺少测量依据的日历工期。S0 完成后再根据真实缺口估算工作量。

## 3. 先冻结的行为约定

下表是建议实施口径。与当前明确需求不冲突的等价整理可直接执行；标明为语义变化的项目先形成输入/输出对照，采用推荐口径也不能隐式覆盖已有产品要求。

| 问题 | 推荐处理 | 边界 / 原因 |
| --- | --- | --- |
| 按哪个模型查能力 | 使用最终账号/渠道映射后的实际出站模型 | 客户端别名仅用于请求身份和权限，不能代替上游能力 |
| Claude 已支持的 effort | 保持原值 | 不把所有 xhigh 一律改 max |
| Claude 已知但不支持的 effort | 跨协议或现有受控改写路径按能力映射；记录请求值和发送值 | Opus 4.5 xhigh/max→high；4.6 xhigh→max；支持 xhigh 的族保留；须满足分组限制 |
| 原生 Claude 的合法参数 | 保留；只修改已决定的目标字段 | 不重建整个请求 DTO 丢弃未知字段、output_config.format、每消息配置和签名块 |
| 原生明确非法档位 | 第一批建议只对已知模型、已知档位启用同一适配；未知型号/枚举保持当前校验或透传行为 | 这是有感行为变化，S0 固化示例并列入审查；不能用全文清洗代替局部适配 |
| 未知模型 / 云平台命名 | 沿用现有名称归一化；能力未知时不猜测支持项 | 能力未知和“明确不支持 effort”必须区分，nil 不能同时表达两者 |
| Opus 5.5 的 none | 保留本地既有转换入口的明确拒绝 | 不直接采用 New API 的 adaptive+low，避免将关闭请求改成继续推理 |
| Sonnet 5.5 的 none | 保留本地已建立的 between_tools+low 语义 | 工具续接、签名块、关闭与独立 output effort 的关系需要单独测试 |
| 手动预算与输出上限 | 默认不提高客户端明确给出的 max_tokens；派生预算只能在合法区间内取值 | 明确预算冲突沿用当前入口的拒绝/修复契约；缺省上限使用已有默认，不复制比例常量到多处 |
| thinking、effort、摘要 | 分开表示；仅改其中一项不覆盖其他项 | 不引入裸 -thinking 清掉现有预算/强度的行为 |
| 新模型后缀或 @ 语法 | 不作为本次新增能力 | 现有真实模型名保留；新语法涉及权限、价格、路由，需独立需求 |
| 采样及 forced tool_choice | 复用当前模型/协议校验；已知不可组合才处理 | 不把 New API 的删除规则直接覆盖本地 5.5 拒绝规则 |
| 管理员覆盖后的请求 | 最终发送前仍受已确定的权限、能力和安全边界约束 | 不新增“管理员可绕过全部检查”路径 |
| 重试状态码 | 保留本地现有恢复契约；按原因与发送阶段判定 | 不复制 New API 504/524 固定排除表，也不把所有 400/401 视作同一种错误 |
| BPS 及 WS | 保留专用模型权限、effort 方言、响应归属、显式恢复与一次生成边界 | 模型名称像 Claude 不代表它使用 Anthropic 出站协议 |

优先级原则：原始输入留存 → 现有权限与 deny 检查 → 现有分组映射/上限规则 → 目标能力适配 → 最终策略与结构校验 → 发送 → 从真实发送结果记录用量/档位。具体函数顺序需保持当前合法覆盖语义；上游能力适配不能把原本拒绝的请求变成允许。

特别用例：请求 xhigh、目标仅支持 high/max、分组上限 xhigh 时，不能先转 max 再越过上限。按现有 deny/downgrade 语义处理模型支持集合与策略允许集合的交集；不能静默提升强度。

## 4. S0：形成可验证的出站基线

任务：

1. 对原生 Messages、Chat→Messages、Responses→Messages 逐条记录路由、账号选择、模型映射、策略改写、序列化、HTTP 发送、恢复重试与计价入口。
2. 为每条路径标注目标协议和凭据类型：API Key/OAuth；当前实际存在的云平台/兼容服务。BPS 使用自身协议能力，单列为不受 Anthropic Renderer 直接作用的路径。
3. 使用现有 httptest / Transport 替身截获最终请求体与头部，保留请求输入、映射结果、发送体、请求/实发 effort、上游尝试次数。只用合成数据。
4. 复用已有测试确定：目录展示是否接入出站、预算是否在下游修复、原生未知字段是否保留、已输出后的错误是否真正触发重放。
5. 将差异分为已满足、真实缺口、语义待定、未覆盖；不把研究中的 New API 风险直接标为本地漏洞。

交付：路径/行为对照表，以及覆盖独立缺口的基线测试。已有测试能证明的行为直接引用，不为每个函数重复增加同类测试。

验收：原生、Chat、Responses 三种入口均有最终发送体证据；模型映射/分组策略/计价位置明确；BPS、WS 和签名保护路径已标出。

## 5. S1：统一 Claude 模型能力来源

主要位置：

- [effort_catalog.go](../backend/internal/pkg/claude/effort_catalog.go) 与 [effort_catalog_test.go](../backend/internal/pkg/claude/effort_catalog_test.go)。
- [openai_codex_models_service.go](../backend/internal/service/openai_codex_models_service.go)：使用同一档位目录生成候选信息。
- [responses_to_anthropic_request.go](../backend/internal/pkg/apicompat/responses_to_anthropic_request.go)：删除统一 xhigh→max 的分散规则。

任务：

1. 保留现有模型身份归一化和 EffortLevelsForModel 调用约定；同一模型族只维护一处能力事实。
2. 现有目录只能表示档位，无法表示 adaptive/manual、disable/between_tools、预算与采样约束时，在同一包增加最小能力描述；每个新增字段必须有实际调用方和测试，不预建全供应商大框架。
3. 区分已知支持、已知不支持、未知能力；最具体的型号先匹配，避免 5 系列吞掉 5.5 规则。
4. 基于本地明确需求及供应商证据逐族纳入。New API 表中的 Haiku/Fable/Mythos、云平台命名或特殊 manual 支持先核实实际本地入口，不以第三方前缀判断直接扩大本地承诺。
5. 采用一个纯计算入口表达能力调整结果；仅当现有函数无法承载模式/预算等联合约束时增加，返回值复用现有结构，避免包循环依赖。

验收：模型别名、区域/供应商前缀、日期、点号版本、真实 thinking/max 名称均有覆盖；4.5/4.6/4.7/4.8/5.5 与未知模型行为明确；对合法输入重复规范化不继续改变结果。

## 6. S2：Claude 请求、策略和计价闭环

主要位置：现有 apicompat 转换文件、gateway_request.go、gateway_service.go、openai_reasoning_effort_policy.go，以及现有 reasoning pricing 测试。实际最末发送钩点以 S0 追踪结果为准。

任务：

1. 先完成 Responses→Messages 和已有 Chat→Messages 转换中的能力选择；使用最终映射模型，保持已有工具、消息和缓存转换。
2. 在原生入口仅处理授权范围内的 effort/thinking/预算字段，复用现有局部 JSON 修改方式；未知字段、output_config.format、block_binding、签名 thinking、每消息配置不被整对象覆盖。
3. 对已知不能发送 effort 的旧式模型，仅在跨协议转换时以现有预算表达；明确 none 和 absent 的差异，不能把关闭意图当低档开启。
4. manual 预算必须满足相应协议/型号约束；用边界测试覆盖 0/-1、1023/1024、等于或超过 max_tokens、默认输出上限、整数溢出。其他协议的 0/-1 语义不得直接送入 Anthropic。
5. 接入分组 deny、映射和上限；记录原请求值后再改写，并验证模型适配不会越过策略允许集合。
6. 对最终序列化体做一致性检查；重试换账号或模型映射变化时重新依据原始意图计算，不在上次已经调整的请求上层层降档。
7. 复用 requested_reasoning_effort、现有发送档位和用量日志；thinking=disabled 但独立 output effort 存在时保留现有计价定义，不仅凭开关覆盖计价档位。
8. 推理参数由网关提前拒绝时不得进入账号惩罚或上游重试；若之前已经预留资源则按当前流程释放。
9. 替换旧的档位映射和预算派生路径，删除无人调用的旧 helper，不保留两份活动规则。

M1 验收：已知模型出站参数合法、合法输入不无谓改写；三种入口与流式/非流式都通过；计价反映实际出站规则；现有 5.5、分组 deny、签名和 BPS 回归通过。

## 7. S3：其他协议按现有入口补齐

| 子项 | 修改原则 | 验收重点 |
| --- | --- | --- |
| Gemini | 在现有 Gemini 原生发送与 gemini_messages_compat_service.go / gemini_chat_completions_compat_service.go 中补能力处理；gemini_reasoning_effort.go 继续专职读取计价档位 | 型号合法字段、预算/档位冲突、不能关闭、原生保留、thoughtSignature 不伪造；不能从 budget 擅自推导新的计价档位 |
| OpenAI / Codex | 复用现有模型判断、request body 清洗与策略；将 token 字段、developer、采样限制分开 | 仅对有证据的目标型号施加约束；未知模型不继承猜测规则；WS 与 HTTP 行为对应 |
| OpenRouter / DeepSeek / Qwen 等兼容目标 | S0 先确认已有实际路径与用户需求，再在现有兼容入口修复 | 各上游方言正确、真实模型名不误裁剪、原生字段不丢失 |
| BPS effort | 作为现有方言的回归对象，使用 BPS 能力/权限来源 | requested 与 forwarded 区分；不套用 Claude 档位；未命中路由和独立 Excel 凭据限制不变 |

交付要求：按目标协议分提交；没有实际入口或明确需求的第三方功能列为不适用，不为模仿 New API 新建供应商接入、工具执行器或全协议注册中心。

## 8. S4：重试、流与恢复契约

本阶段先用现有请求生命周期状态核查实际行为；只有缺少可表达状态时才增加最小内部字段。不会套入 New API 的渠道错误优先或默认状态码规则。

复用位置：[failover_loop.go](../backend/internal/handler/failover_loop.go)、[openai_gateway_upstream_errors.go](../backend/internal/service/openai_gateway_upstream_errors.go)、[openai_responses_rejected_field_retry.go](../backend/internal/service/openai_responses_rejected_field_retry.go)、[openai_ws_http_bridge.go](../backend/internal/service/openai_ws_http_bridge.go)、[openai_ws_excel_bps_bridge.go](../backend/internal/service/openai_ws_excel_bps_bridge.go)、[ops_upstream_context.go](../backend/internal/service/ops_upstream_context.go)。实际已有的预算、重放证明和 retry_stopped 诊断优先复用。

必须保留恢复合同细节：同一 turn 的修复/重试/换账号共用已启用预算；原生密文历史不通过删除修复；BPS 仅适用自身明确允许的窄恢复例外，不能反向扩大原生规则；跨账号必须证明上下文与工具执行可迁移；失败 response ID 不进入成功历史或成功用量；无上游 ID 时不捏造可续聊 ID；未知用量不估算。

| 请求阶段 / 原因 | 计划要求 |
| --- | --- |
| 本地请求校验或分组拒绝 | 原协议错误返回；不重试、不处罚账号 |
| 明确尚未发送到上游的连接失败 | 按现有预算、可用候选、粘性和取消规则决定是否尝试 |
| 请求已发送但结果未知 | 进入现有不可盲目重放边界；不能仅因没有下游 chunk 就认为安全 |
| 上游明确拒绝、协议特定恢复 | 保留既有有界恢复；检查错误码、上游是否接受生成、同上下文/账号、次数和资源释放条件 |
| 已输出正文、思考、工具或上游已确认生成 | 禁止自动重新生成；按照现有 Responses/BPS 契约处理允许的恢复，不能扩大范围 |
| HTTP 200 后缺少终态 / 错误事件 | 区分传输关闭与业务完成；保留真实失败及已报告 usage |
| SSE keepalive 或头部已发 | 区分响应是否提交、是否交付业务内容与上游是否接受；不能只看 Writer.Written 或只看业务 chunk |
| 客户端取消 / 超时 | 取消下游等待和上游读取，及时释放并发、连接与占用；不当成切号理由 |
| WS / BPS 多轮 | 维持用户/账号/响应归属、每轮鉴权和资源预算，错误不影响其他会话 |

测试使用可控上游替身及确定性时间控制，统计真实请求次数、工具下发次数、终态数、资源释放和 usage。覆盖部分输出后 error、EOF 无终态、请求写入后断链、429/显式恢复、取消与重连；不只测重试函数返回值。

不默认新增全局退避/随机抖动/新重试开关。如果现有路径出现可复现的重试风暴，再在既有预算/取消/Retry-After 框架内作独立改动，避免一次请求层层叠加重试。

## 9. S5：可观察性、计量与资源收口

1. 使用现有结构化日志记录转换/重试原因；只新增必要的内部诊断字段，不默认新增数据库列、控制台页面或运营系统。
2. 诊断至少能还原入口协议、最终模型、请求档位、策略处理、发送模式/档位/预算、调整原因；不记录凭据、签名正文、原始对话、工具名称或参数。重复原因去重并设上限，数值依据现有日志规范。
3. 优先复用现有 Usage/计费流程：一次业务请求多次尝试、所有已报告用量、成功结算、失败清理、取消、恢复生成分别核验。不照搬 New API 的 token 估算常量或新建 BillingSession 替代本地资金规则。
4. 对请求体重放、响应体关闭、goroutine 退出、并发槽归还做现有测试扩展。只有当前存储存在实际尺寸/内存问题才评估落盘，不提前引入新的 BodyStorage 层。
5. 工具转换损失、签名和 continuation 不能表达时遵循现有明确拒绝/有损策略；不提供虚假的 thoughtSignature 修复或新增客户端工具执行路径。

验收：故障路径能解释原因；无凭据/正文泄漏；重试及取消不重复结算、漏释放；已报告用量不因失败丢弃；现有安全和 BPS 恢复回归通过。

## 10. 可执行验收清单

- [x] T01：同一个请求经不同最终模型映射，发送对应合法档位；目录候选与发送规则一致。
- [x] T02：4.5 xhigh/max→high；4.6 xhigh→max；4.7/4.8 等支持 xhigh 时保留；未知能力不猜测。
- [x] T03：none、未指定、独立 effort、manual budget、adaptive、between_tools、仅隐藏摘要分别验证。
- [x] T04：Opus 5.5 none 仍按现有入口拒绝；Sonnet 5.5 none 保持当前语义；forced tool_choice 与采样限制不退化。
- [x] T05：manual 预算边界和输出上限冲突行为明确；客户端明确的输出上限不被悄悄抬高。
- [x] T06：原生扩展字段、output_config.format、block_binding、每消息设置和签名工具续接保留。
- [x] T07：分组 deny/映射/上限不能被档位适配或模型别名绕过；重点测试 xhigh→max 遇到 ceiling=xhigh。
- [x] T08：API Key/OAuth、HTTP 流式/非流式、Chat/Responses/原生入口覆盖真实可用组合。
- [x] T09：重试更换账号后从原始输入重新推导，不复用前次模型的规范化结果。
- [x] T10：请求/发送/计价含义一致；disabled+独立 effort、失败用量、缓存用量不混淆。
- [x] T11：部分输出、工具可能执行、写入后断链不盲目重放；EOF 无终态不误报 completed。
- [x] T12：显式 Responses/BPS 恢复仍满足原契约；401/403/400 特例不扩展为普遍重试。
- [x] T13：取消、超时、解析失败均关闭资源；终态和账务无重复。
- [x] T14：Gemini 和其他实际纳入的协议独立完成能力/方言矩阵，BPS 不继承 Anthropic 出站规则。
- [x] T15：诊断可解释、受控且不泄密；变更后的旧规则已清理。

## 11. 交付、回退和完成定义

### 11.1 可复用测试与执行顺序

| 阶段 | 现有测试入口 | 必须补的证据 |
| --- | --- | --- |
| S1 | effort_catalog_test.go；TestEffortLevelsForModel、TestIsOpus55OpenRouterExactAlias、TestIsSonnet55 | 模型身份、档位与未知能力分离 |
| S2 转换 | apicompat 现有 Responses/Claude/Chat 测试、chatcompletions_anthropic_reasoning_passback_test.go | 最终模型映射、合法档位、预算、签名与工具续接 |
| S2 策略/计价 | openai_reasoning_effort_policy_test.go；gateway_compat_reasoning_pricing_test.go；gateway_reasoning_pricing_test.go | 原始值留存、deny 不绕过、最终请求与计价一致；后两者需要 unit 标签 |
| S3 | gemini_native_reasoning_pricing_test.go；openai_gateway_converted_reasoning_pricing_test.go；各协议兼容测试 | Gemini 不从预算估算档位；各出站方言不互相污染 |
| S4 | openai_ws_recovery_contract_test.go；openai_responses_rejected_field_retry_test.go；openai_replay_policy_test.go | 同 turn 预算、完整历史、错误归类、不可重放原因 |
| S4/S5 | openai_sse_read_pump_test.go；openai_ws_bps_ingress_test.go；openai_excel_bps_recovery_test.go | 无终态、取消、事件归属、BPS 显式恢复及实际用量 |

下列命令保留计划制定时的执行清单；实际执行命令、结果及独立 lint 限制见 [实施验收记录](new-api-adoption-validation-2026-10-13.md)。使用 backend/go.mod 要求的 Go 1.27.2 或可满足该要求的工具链；必须包含 unit 标签，否则部分关键计价回归不会被执行。

~~~sh
cd /Users/luhonglin/Git/sub2api/backend
go version

# M1：能力与转换包；然后策略、发送档位、计价和 5.5 回归
go test ./internal/pkg/claude ./internal/pkg/apicompat
go test -tags=unit ./internal/service -run "Test.*(Reasoning|Effort|Sonnet55|Opus55)"

# M2/M3：按实际修改范围选取；不是用此正则代替最终回归
go test -tags=unit ./internal/service ./internal/handler -run "Test.*(Replay|Recovery|Retry|BPS|SSEReadPump|Terminal|Failover)"

# 涉及流/并发状态的批次，针对现有合同与本轮新增用例运行 race
go test -race -tags=unit ./internal/service -run "Test(OpenAIWSAccountRefreshRespectsCredentialOwnership|OpenAIRetryStopCapturesBoundedProgressWithoutContent|OpenAISSEReadPump|OpenAIWSBPS)"

# 发布候选的后端回归与构建；遵循当前 CI 配置
make test-unit
golangci-lint run ./...
make build
~~~

新增用例可能不匹配现有正则，执行者须在测试清单中逐一确认实际运行。若修改计费持久化、资源生命周期或仓储交互，使用测试专用数据库/缓存及现有 integration 入口完成相关用例；不使用生产资源。只改后端无需全量重跑无关前端，候选目录/UI 发生变化才补对应前端测试与类型检查。

每批保存测试名称/命令、工具链、退出码、失败项与用例日志；竞态和故障注入必须统计真实尝试/账务效果。测试环境缺依赖就标明未完成，不以编译通过或上一轮 New API 的 616 项记录替代。

### 11.2 批次交付与回退

建议提交粒度：基线测试 → Claude 能力规则 → Claude 出站与计价 → 各协议适配 → 重试/终态的独立修复 → 诊断及资源收口。测试随对应风险提交，不集中到最后。每批都必须可单独验证；独立缺口已解决时可提前交付。

不默认新增运行时 feature flag。先在隔离测试环境验证；需要线上观测/小流量验收时另按部署授权推进，并复用已有路由或测试账号能力。任何发布或生产操作均不由本计划自动触发。

回退按批次使用普通 revert 并检查后续依赖，不 reset/force-push，不回退用户其他工作或已修复安全边界；无新增数据库迁移时优先保持旧版本可运行。出现错误计费、权限绕过、重复生成/工具、签名续接破坏或资源泄漏时停止该批交付，保留证据并修复。

未来获授权提交/推送时：重新读取 main 最新 CHANGE_DELIVERY.md；共享改动 main 验证后普通 merge 到 TapModels/tokensavy，保留品牌。只允许 holeenlu/sub2api 的 main→origin/main、TapModels→origin/TapModels、tokensavy→origin/tokensavy，使用完整 refspec，并逐一核对远端 SHA。提交、推送不发版、不部署、不迁移。

每批交付必须报告：实际文件、业务代码增删、测试代码增删、新增接口/配置/执行路径数量、被删除旧规则、运行过的检查、未执行验证与剩余问题。目标是新增公开接口 0、配置项 0、迁移 0；若确有需要，先说明原有结构无法承载的原因。

若修改了 BPS 来源清单覆盖的文件，记录本次本地适配和与原始来源的差异，必要时更新现有清单/验收记录；不得覆盖固定来源 SHA 或把本地修改标成未经验证的 source-equivalent。

最终完成定义：本次纳入范围的 T01–T15 全部有验证结果或明确的“不适用”依据；最终出站参数与策略/计费一致；恢复、安全及品牌约束保持；所有已授权交付目标均已验证。

## 12. 计划编制验证与状态（历史记录）

本轮已复核本地基线、关键 Claude 能力与转换逻辑、分组策略及现行交付/BPS 要求。仅新增本计划文档，未修改业务实现，未执行本轮实现测试，未提交、推送或部署。上一轮 New API 测试结果只作为研究证据，不充当本地实施的验收结果。

本文件位于现有 docs/* 忽略范围内；未修改 .gitignore。需要纳入后续交付时，只显式纳入已审查的计划和研究资料。

## 13. 现场错误对照与第一批验收补充（2026-10-13）

用户提供了 2026-10-12 23:18:30 / 23:18:49 的两条原生 /v1/messages 流式错误。本轮仅对照用户日志、本地 main@c575e66799775e649e08e45053034f90a8fcd96a 和供应商文档；未读取生产完整请求体或运行镜像 revision，未重放用户请求。

### 13.1 两条错误不只是 effort 档位问题

| 案例 | 已知证据 | 定位与限制 |
| --- | --- | --- |
| #25177，claude-sonnet-5-5 | 请求 400、无上游状态；错误文本与 validateClaude55Request 完全相同 | 当前源码仅在 thinking.type 为 enabled 或 disabled 时返回该消息；Forward 在发送前执行校验。日志符合本地前置拒绝，不能仅由此判断原请求是哪一种模式 |
| #25176，claude-fable-5-1 | 上游明确返回 400，指出 thinking.type.disabled 不支持 | 能确认上游看到不支持的 disabled；缺少原始体与各阶段发送快照，不能断言该字段一定由客户端设置 |

本地证据：[gateway_request.go](../backend/internal/service/gateway_request.go) 的 validateClaude55Request；[gateway_forward.go](../backend/internal/service/gateway_forward.go) 的前置校验。现有 effort 目录只说明档位，不等于已有模式适配。

### 13.2 必须明确的处理口径

| 原生输入 | 可行的兼容处理 | 语义与验收边界 |
| --- | --- | --- |
| Sonnet 5.5：disabled | 改为 between_tools；同时处理该模式的 effort 和 thinking 子字段约束 | 仅关闭前置思考，工具间仍可有 thinking；effort 只能 low/medium/high；不能残留 budget_tokens/display/block_binding |
| Sonnet 5.5：enabled+budget_tokens | 改为 adaptive；将强度意图映射到合法 effort，去掉手动预算 | 档位不能精确兑现原 token 预算，必须记录调整；仍受分组策略和输出上限约束 |
| Fable 5.1：disabled，要求严格关闭 | 本地明确拒绝；提示目标模型不支持该要求 | 可避免无谓上游请求，但客户端仍然失败，不能声称已“解决成成功请求” |
| Fable 5.1：disabled，允许最低思考兼容 | 使用 adaptive+low；受合法的独立 effort、分组策略和已确定优先级约束 | 这是将关闭请求改为最低强度推理，不是关闭思考；可能改变用量/延迟，需要明确采用该产品语义 |

截至本次补充，尚未把 Fable 的 disabled→adaptive+low 设为批准的默认行为；现有计划“不把关闭意图直接当低档开启”的约定仍有效。若目标是让这两类请求自动成功，必须将该兼容口径一并明确；只加模型能力校验、只改 effort、或把上游 400 提前为本地 400 都不足以达到目标。

### 13.3 对 M1 的具体补充

1. S0 将两例列为原生入口的合成回归夹具；Sonnet 同时覆盖 enabled 和 disabled，不能猜测实际输入。真实请求如需进一步追溯，只获取必要脱敏字段和部署版本。
2. S1 显式核实 Sonnet 5.5 和 Fable 5.1 的 adaptive/manual/disable/between_tools 能力；继续使用同一目录。
3. S2 必须覆盖原生 /v1/messages，并在最终模型解析后、现有模式拒绝校验前执行被批准的兼容调整；保留后置合法性校验。只修改 Responses→Anthropic 转换器不能修复这里的原生入口。
4. T03/T04/T06 扩展到模式转换、独立 effort、非法子字段、历史 thinking 签名和工具续接；T07/T10 确认策略与计价；T09 确认恢复/重试不会再次生成不支持的模式。
5. 对允许兼容的案例，验收必须看到最终合法发送体且请求正常完成；严格拒绝案例验收为本地清晰错误、零上游尝试，不计为“成功调用修复”。不以隐藏日志、增加重试或换账号作为通过标准。

官方核对（2026-10-13）：[Sonnet 5.5 模式及字段限制](https://platform.claude.com/docs/zh-CN/models/sonnet-5-5/whats-new-sonnet-5-5)、[Fable 5.1 始终开启 adaptive thinking](https://platform.claude.com/docs/en/models/fable-5-1/whats-new-fable-5-1)、[thinking 模式排错矩阵](https://platform.claude.com/docs/en/build-with-claude/thinking-troubleshooting)。本节追加时仅完成错误对照；随后实施结果见验收记录，未修改生产设置。



## 14. 实施完成记录（2026-10-13）

S0–S5 已完成本次纳入范围的实现、现有契约复核与本地回归；T01–T15 对应证据见 [实施与二次复核报告](new-api-adoption-validation-2026-10-13.md)。最终候选完整 unit 24,711 条通过事件、定向 race 185 条通过事件（均含子测试），go vet/build/diff 检查通过；18 项已有测试依原有条件跳过，独立 golangci-lint 因工具/网络限制未执行。未进行真实供应商调用、生产部署或迁移。

二次复核修复了 Bedrock 实发/计价、兼容入口策略漏检及重复映射、兜底分组原始意图重算、未指定 effort 默认值、签名历史及 Gemini 摘要保留问题。Sonnet 5.5 的两种旧模式有成功的合成请求验证；Fable 5.1 disabled 仍为严格本地拒绝，不把本次工作表述为这类输入已自动成功。

工作分支 codex/new-api-adoption-20261013，HEAD 仍为 c575e66799775e649e08e45053034f90a8fcd96a；未暂存、提交、推送。当前变更归属共享能力，后续交付仍须遵守用户授权及最新交付规范。

## 15. 用户验收意见修订（2026-10-13）

第 14 节记录首轮候选快照；以下修订对应用户独立复核后继续修正的未提交代码。最终摘要和重新执行的检查以验收报告为准。

1. 区分请求级 effort 上限与历史每消息 effort 一致性。Opus 5 disabled 只保留当前请求 high 上限，不拒绝合法的历史档位变化；Haiku 5.5 disabled 和 Sonnet 5.5 between_tools 保留历史一致性限制。现有 DisableHighOnly 无法表达这一区别，因此在同一能力结构新增内部 DisableFixedEffort 标记，仅 Haiku 5.5 启用；不新增用户配置或转发入口。
2. 原生 Sonnet 5.5 disabled 转 between_tools 时，省略 effort 保持省略，采用供应商默认 high 验证历史；不再注入 low。显式 low/medium/high 保留，xhigh/max 仍按既定方案收敛 high，历史配置不重写。
3. 跨协议显式 reasoning.effort=none 的既有 Sonnet 5.5 契约保持 between_tools + low，在转换器明确设置；这与原生 disabled 且省略 output_config.effort 的输入不同。Fable disabled 仍严格本地 400。
4. 明确保留本次方案相对旧代码的行为变化：跨协议显式 low 表示低强度思考，落到只支持手动模式的 Haiku 4.5、Sonnet 4.5、Opus 4.1 等型号时使用 enabled + budget_tokens=1024；none 表示关闭，省略表示保持默认。不把 low 继续等同于关闭；该变化可能影响用量和延迟，不能当成无感修复。
5. 网关派生手动预算必须至少 1024 且小于 max_tokens。Opus 4.5 等手动模型从 adaptive/跨协议 reasoning 转换时，客户端明确 max_tokens（或 max_output_tokens）为 1–1024 则本地 400；1025 起可夹取为 max_tokens-1。既不抬高显式输出上限，也不悄悄关闭思考。原生客户端明确手动预算仍走已有校验/修复契约。
6. 新增永久回归覆盖 Opus/Haiku 历史差异、Sonnet 省略默认与历史 high、显式冲突仍拒绝、别名、幂等、历史不变、跨协议 none 与 low/absent 区别，以及 1024/1025 边界。最终发送体验证显式带所需的 mid-conversation-output-config beta，尊重现有 beta/body 对齐规则。
7. 修复 QF1003，并使用已安装的 golangci-lint v2.14.0 重新检查全仓与相对 HEAD 的新增问题；现有基线 lint 单独报告。复跑完整后端 unit、定向 race、vet、build 后再次审查最终代码。

以上第 4、5 项是本次未提交候选的明确实现选择与兼容性说明，不将用户的“方向合理”记成额外发布授权。没有新增远程接口、配置项、数据库迁移或顶层重试/转发路径。

完成记录：两处边界与 QF1003 均已修正。当前候选完整 unit 24,734 条通过事件（含子测试，18 项既有跳过）、定向 race 209 条通过事件，vet/build/格式/差异检查通过；golangci-lint 新增 0 条，全仓剩余 48 条均在本次改动文件之外。最终源码摘要为 c9b7cf94ae51657a0a087fab1c40d9caad6639cbcf9ec9580fa279c598e644b9；未暂存、提交、推送。详细命令、计数、限制和二次复核说明见验收报告末节。
