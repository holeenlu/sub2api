# New API 中转与模型推理参数处理研究

研究日期：2026-10-12（Asia/Shanghai）。

## 研究范围与证据基线

- 对象：`QuantumNous/new-api`，默认分支 `main`。
- 本次固定源码：`633b4c5b190dc1af99a2814391354d86948e7943`，提交日期 2026-10-12，提交标题 `feat(logs): export usage logs from the common log page (#7774)`。所有源码结论以该提交为准，不把主分支等同于已发布版本。
- 本地对照：`holeenlu/sub2api` 的 `main@c575e66799775e649e08e45053034f90a8fcd96a`。
- 需求归属：共享网关技术研究，适用于 KDAN、TapModels、Tokensavy；本次交付研究文档，不修改业务行为，不提交、推送或发布。
- 研究方法：固定公开仓库源码，沿路由、请求适配、发送、流转换、错误策略、重试、结算追踪调用关系；重点核对 Claude 推理能力矩阵、优先级、转换例外及现有测试。
- 下文明确区分“源码实际行为”“设计价值”“限制/建议”；模型能力表是 New API 的实现表，不自动视为供应商对所有平台的承诺。

## 核心发现

New API 将四种文本协议的转换和推理参数归一化集中在独立的 `relaykit` Go 模块。Claude 的处理不是简单透传 `reasoning_effort`，而是将输入解析为统一 `Intent`，按最终目标模型能力渲染 `thinking`、`output_config.effort`、预算和采样参数，并返回转换诊断。原生请求和跨协议转换路径存在重要差异，需要分别检查。

## 一、优先值得学习的部分

以下排序是对 sub2api 的工程建议，不是已经批准实施的需求。

| 优先级 | 值得学习的方案 | 解决的问题 | 借鉴边界 |
| --- | --- | --- | --- |
| P0 | 用目标模型能力渲染请求 | 同一个 effort 在不同模型上合法值不同，避免上游 400 | 复用本地已有能力目录，不再维护第二套规则 |
| P0 | 分开表示模式、强度、预算、可见性 | 隐藏思考被误作关闭思考；多跳转换丢失精确预算 | 统一档位不代表供应商保证的固定算力或 token 数 |
| P0 | 区分原生透传与跨协议适配 | 新参数、兼容服务私有字段被统一清洗损坏 | 本地安全检查和用户明确的分组策略仍须执行 |
| P0 | 集中重试决策并记录原因 | 无法解释换渠道、停止或账号处罚 | HTTP 流中途错误的重试边界不能照搬 |
| P0 | 一次请求共享预扣与最终结算会话 | 重试多次预扣、重复退款、结算后误退款 | 本地已有计费体系继续作为唯一资金规则来源 |
| P1 | 转换路径、质量与字段损失可观察 | “兼容”掩盖工具、签名和续写语义丢失 | 默认允许有损转换不适合所有 Agent 场景 |
| P1 | 分开记录传输结束与业务完成 | HTTP 200、EOF 被误当模型成功 | 还要核实各消费方是否使用终止事件要求 |
| P1 | 请求级工具调用映射状态 | namespace、自定义工具、索引、增量 JSON 无法还原 | 不把各平台托管工具视作完全等价 |
| P1 | 模型别名、映射、出站模型分离 | 误删真实模型名中的 max/thinking | 以实际映射结果判定能力，保留真实 ID 豁免 |
| P1 | 可重放请求体及大请求磁盘缓存 | 重试时请求体已读尽、Transport 过早关闭缓存 | 字节可重放不等于业务可以安全重放 |
| P1 | WS 每轮复用普通鉴权、限流与计费 | 只在握手检查，后续绕过权限变化 | 本地 Responses 恢复、租户归属和 BPS 契约优先 |

证据入口：[主调用链][R01]、[转换注册表][R02]、[统一推理意图][R03]、[重试策略][R04]、[计费会话][R05]、[WS 每轮中间件][R06]。

## 二、完整中转流程与责任划分

### 2.1 HTTP 文本主链

下图概括公共链路；图片、音频、异步任务有独立 helper，不能推断为全部走同一文本实现。

~~~mermaid
flowchart TD
    A[客户端请求] --> B[鉴权 / 分组与模型权限 / 限流 / 初次选渠道]
    B --> C[解析校验 / 建立 RelayInfo]
    C --> D[估算用量 / 定价快照 / 一次预扣]
    D --> E[本次渠道与分组 / 恢复请求体]
    E --> F[请求拷贝 / 模型映射 / 后缀与参数意图]
    F --> G{原文透传?}
    G -- 是 --> H[原始 BodyStorage]
    G -- 否 --> I[协议适配 / 字段过滤 / 参数覆盖]
    H --> J[HTTP 发送与供应商认证头]
    I --> J
    J --> K{响应成功?}
    K -- 是 --> L[响应或 SSE 转换 / Usage / 终止状态]
    L --> M[结算与日志]
    K -- 否 --> N[错误归一化 / 状态映射 / 重试决策 / 渠道健康]
    N -- 重试 --> E
    N -- 停止 --> O[退款 / 按客户端协议返回错误]
~~~

controller.Relay 管一次业务请求的生命周期；ClaudeHelper 等 helper 管一次渠道尝试；供应商 adaptor 负责 URL、认证头和供应商处理；RelayKit 管协议数据转换。主控制器先 PrepareRequestBilling，再循环尝试，最终失败才进入退款逻辑。[R01][R07][R08]

### 2.2 路由与渠道选择

- ChannelConstraints 收敛候选约束；固定渠道、会话亲和和随机选择都必须满足约束。固定渠道优先，亲和通常仅用于首次尝试，再进入候选选择。
- 先按完整模型名匹配，再按标准化路由名匹配，保留精确专用渠道的优先级。
- 分组内优先级从高到低；同级按权重抽样。全部权重为零时改为等权。
- 重试索引超过优先级数量时继续使用最低优先级，**不保证每个渠道恰好尝试一次，也不保证下次一定换渠道**。
- auto 分组跨组重试会重置计数；RetryTimes=N 不能简单解释为整个业务请求绝对只有 N+1 次发送。
- 亲和有 inherit/prefer/strict 和旧规则兼容；strict 失败停止跨渠道重试，适合上游状态不可迁移的会话。

值得借鉴的是规则集中和决策可解释；具体选择算法不等于 sub2api 的账号调度，应比较实际行为后决定。[R09][R10][R11]

### 2.3 请求变换顺序

Claude 路径：拷贝 DTO → 链式模型映射 → 模型修饰符 → adaptor 转换 → 序列化 → 移除禁用字段 → **最后应用渠道参数覆盖** → 发送。

链式模型映射用 visited 集合发现 A→B→A；A→A 视为终点。OriginModelName 与 UpstreamModelName 分离，避免把客户端别名和最终模型混用。[R07][R12]

最后的参数覆盖仍可能重新写入不支持的参数。因此“先修正能力”不等于“最终发送必定合法”。迁移设计应在最终出站边界核验，或明确管理员覆盖优先的语义。

### 2.4 四种协议的转换矩阵

| 源 → 目标 | Chat | Responses | Claude Messages | Gemini |
| --- | --- | --- | --- | --- |
| Chat | 同协议 | good | fair | fair |
| Responses | good | 同协议 | fair | fair |
| Claude Messages | fair | fair | 同协议 | discouraged |
| Gemini | fair | fair | discouraged | 同协议 |

这些是转换路径质量，不是模型输出质量。Claude↔Gemini 通过 Chat 两跳；部分 Responses 路径的请求与响应使用不同的直达/中转路线。结果携带 Steps、Quality、Diagnostics，应读取实际路径。[R02][R13]

工具定义、托管工具、签名思考块、续写、结构化输出和缓存控制不总能一一对应。默认 allow 继续转换并报告损失；safe/strict 是请求阶段可选拒绝策略。响应和流式转换即使设 strict 也不会因此拒绝，已有测试固定。[R14]

宿主按 code/path/severity/from/to 对转换诊断去重，每请求最多保留 32 个不同条目，超限只追加一次截断告警；转换结果的可观察性也有明确内存边界。[R39][R49]

## 三、Claude“按模型支持的档位发送思考强度”的完整方案

### 3.1 生效范围

**确实有按模型支持档位发送 effort 的方案，但不是所有原生 Claude 请求都自动修正。**

| 入口情况 | 是否渲染模型能力 | 实际行为 |
| --- | --- | --- |
| Chat / Responses / Gemini → Claude | 是 | 解析源意图，再按目标模型生成参数 |
| 原生 Claude + 有效思考后缀或 @ 修饰符 | 是 | 宿主提供意图后重新渲染 thinking/effort |
| 原生 Claude，无宿主思考修饰符 | 否 | 保留 thinking、output_config，仅提取元数据 |
| 全局/渠道原始请求体透传 | 不走通常的重写 | 发送原始 body，不裁掉后缀 |
| 关闭旧式后缀开关 | 旧式后缀不触发 | -thinking/-high 等保留；显式 @ 语法不受此开关控制 |
| 命中真实模型名豁免 | 不解析该名字中的后缀/@ | 避免误伤真实 ID；不等于关闭全部跨协议转换 |

原生提前返回位于 internal/shared/claude/reasoning.go:31–53。本次用相同 xhigh 原生请求与 Chat→Claude 请求实际对照，确认该边界。[R15][R16]

### 3.2 四个维度不能混淆

| Intent 字段 | 语义 | 关键区别 |
| --- | --- | --- |
| Mode | 未指定、enabled、adaptive、disabled | 未指定不等于关闭；模型可能默认思考 |
| Effort | none/minimal/low/medium/high/xhigh/max | 统一词汇不是每个供应商都支持的枚举 |
| BudgetTokens | 精确预算，含 0、-1 特殊语义 | effort 不是固定 token 预算 |
| IncludeThoughts | 是否返回思考摘要 | 隐藏摘要不能擅自关闭推理 |
| Source/BudgetSource | 原生、显式字段、后缀、中间态来源 | 多跳后不能从简化档位反推原始预算 |

ReasoningConversionState 在进程内传递这些信息，不作为额外 JSON 发给供应商。Claude output_config.effort 能独立于 thinking 存在；官方也说明 effort 影响正文和工具调用，不能把它等同于思考开关。[R03][R17][A01]

### 3.3 接受哪些输入

- Chat：顶层 reasoning_effort，以及 OpenRouter 风格 reasoning.enabled/effort/max_tokens/exclude。顶层 effort 在同层冲突时优先，数字预算和可见性可独立保留。
- Responses：reasoning.effort、reasoning.summary，以及进程内精确预算状态。
- Claude：thinking.type/budget_tokens/display、output_config.effort。
- Gemini：generationConfig.thinkingConfig 的 budget、level、includeThoughts。
- 旧式模型后缀：-thinking、-nothinking、-thinking-<整数>、-minimal/-low/-medium/-high/-xhigh/-max/-none；受 Claude/Gemini 各自适配开关控制。
- 显式修饰符：@thinking:on|adaptive|off|整数、@effort:<档位>、@temperature:<有限数字>、@topp:<有限数字>。thinking 数字允许 -1、0，不接受小于 -1。

只识别连续尾部 @key:value；模型名中的其他 @ 保留。重复同名修饰符取最右侧，并报 duplicate_model_modifier。真实 gpt-5.1-codex-max、kimi-k2-thinking 等有豁免，不做盲目全局后缀删除。[R03][R16][R18]

### 3.4 优先级和冲突解决

1. 映射后模型的显式修饰符优先于客户端模型的修饰符。
2. 模型名明确指定的启停/档位/预算优先于请求字段；项目理由是模型名同时参与路由与计价。
3. 要区分纯转换函数和宿主入口：MergeExplicitAndSuffix 对裸开启意图可保留已有强度；但实际 ApplyReasoningModelSuffix → applyModelControls 链会清掉原生 Claude 的 thinking 和 output_config.effort，再按模型修饰符重建。因此宿主的裸 -thinking / @thinking:on 也可能把已有 low+手动预算变成默认 high+adaptive，不能只凭纯函数推断最终请求。[R03][R16][R48]
4. 原生与通用字段合并时，更具体的原生表达优先；摘要显示意图可独立保留。
5. 普通结构化意图：预算 0 关闭；非零预算与关闭冲突时优先预算；小于 -1 调为 -1 并告警。显式 @thinking 对小于 -1 直接拒绝，两者不能混写。
6. Claude 特别允许 thinking 关闭 + 独立 effort，Renderer 单独处理。
7. 渠道参数覆盖在转换之后执行，仍可覆盖前述结果。

冲突通常成为 warning；未知 effort/mode 或无效 JSON 才是错误。宿主把已标记的推理输入错误转为 400 + SkipRetry，避免无谓换渠道和误处罚。[R03][R07][R16][R19]

### 3.5 固定版本的完整模型能力矩阵

下表描述 claudeCapabilitiesFor 的实现；按前缀家族匹配。不能把前缀命中扩张解释成供应商对所有版本、所有云平台的承诺。

| Claude 家族 | output effort 合法集合 | 推断默认强度 | 默认思考 | 模式及关闭处理 |
| --- | --- | --- | --- | --- |
| Sonnet/Haiku 4.5、旧 Opus/Sonnet 4.x、3.7 Sonnet 等无 effort 能力项 | 不发送 output effort | 启用时 high | 否 | manual budget；可 disabled |
| Opus 4.5 | low/medium/high | high | 否 | manual budget；可 disabled |
| Opus 4.6、Sonnet 4.6 | low/medium/high/max | high | 否 | 通常 adaptive；明确预算且非 adaptive 时 manual；可 disabled |
| Mythos Preview | low/medium/high/max | high | 是 | adaptive；源码仍允许明确预算走 manual；关闭改 adaptive+low |
| Opus 4.7、4.8 | low/medium/high/xhigh/max | high | 否 | adaptive-only；可 disabled |
| Opus 5（5.5 先由下一项匹配） | low/medium/high/xhigh/max | high | 是 | adaptive；disabled 时 effort 最多 high |
| Opus 5.5 | low/medium/high/xhigh/max | medium | 是 | adaptive；不能关闭，转 adaptive+low |
| Sonnet 5（5.5 先单独匹配） | low/medium/high/xhigh/max | high | 是 | adaptive；可 disabled，源码未设 off-effort 上限 |
| Sonnet 5.5 | low/medium/high/xhigh/max | high | 是 | 关闭转 between_tools；此模式 effort 最多 high |
| Haiku 5 前缀（含 5.5） | low/medium/high/xhigh/max | medium | 是 | adaptive；disabled 时 effort 最多 high |
| Fable 5 / Mythos 5 前缀（含 5.1） | low/medium/high/xhigh/max | high | 是 | adaptive；不能关闭，转 adaptive+low |

Mythos Preview 的 manual 分支和 Haiku 5 前缀范围均是源码事实，不能替代供应商端验证。[R20]

### 3.6 effort 映射全表

适用于真正进入能力 Renderer 的请求：

| 输入 | 不支持 output effort | 仅 low/medium/high | 支持 max、不支持 xhigh | 同时支持 xhigh/max |
| --- | --- | --- | --- | --- |
| minimal | 用预算表达 | low | low | low |
| low/medium/high | 用预算表达 | 原值 | 原值 | 原值 |
| xhigh | 95% 预算 | high | max | xhigh |
| max | 95% 预算 | high | max | max |
| none | 关闭模式判断 | 关闭模式判断 | 关闭模式判断 | 关闭模式判断 |

未知模型不会被凭空增加一个受支持的 output effort；未知 Claude 兼容模型的跨协议 adaptive 保守改 manual。原生未知模型仍保留原生控制。能力查找主要按裸 Claude 前缀匹配，不能假设已覆盖所有 Bedrock/OpenRouter 命名规范。[R15][R20]

xhigh→max 是项目选择的适配，不是数学意义上“只降级”。跨厂商档位无严格共用量尺，应明确记录请求值与发送值。

### 3.7 thinking 模式转换

- adaptive-only 模型收到手动预算：预算近似为档位，转 adaptive，报 claude_budget_to_adaptive。
- 4.6 同时支持两种模式：明确预算且未指定 adaptive 时保留 manual。
- 旧式模型收到 adaptive：改 manual，必要时默认 high，报 claude_adaptive_to_manual。
- 不能关闭的模型收到 none/disabled：改 adaptive+low，报 claude_thinking_disable_unsupported；这并未满足“完全关闭”的原意。
- Sonnet 5.5 关闭：between_tools，不带其他 thinking 子字段；若还给了 xhigh/max，改 high。
- Opus 5、Haiku 5 家族 disabled+xhigh/max：disabled+high。
- 原生仅给 effort、不设置 thinking：Renderer 尽量保留二者独立，不无条件注入 adaptive。

官方文档确认 Opus 5.5 无法关闭、Sonnet 5.5 between_tools 的强度限制。原生直通仍可由上游返回 400，不能声称网关已兜底所有情况。[R20][A01][A02]

### 3.8 预算、max_tokens 和成本边界

手动 thinking 的不变量：1024 ≤ budget_tokens < max_tokens。

| 意图 | 手动预算 |
| --- | --- |
| minimal | max_tokens 的 5% |
| low | 20% |
| medium | 50% |
| high | 80% |
| xhigh/max | 95% |
| 仅开启 | 配置比例；无效/未设回退 80%，上限低于 100% |

预算再限制到不变量内。max_tokens 缺失或 ≤1024 时，Renderer 可提高到至少 1280；明确预算可以推动到 budget+1。宿主还参考配置默认值，有整数溢出与派生上限检查。若原 max_tokens 已大于 1024 但小于预算，通常缩预算到 max_tokens−1，不无限提高输出上限。[R15][R20]

预算→档位的桶：0→none；负数动态→high；1–1024→low；1025–8192→medium；>8192→high。转换不可逆，不能视为各模型算力等价。[R03]

自动提高 max_tokens 改变客户端输出上限；请求预扣发生在转换之前。这需要和本地价格、额度边界一起评审，不能只以“避免 400”为理由直接复制。

### 3.9 采样、摘要和其他原生字段

- strictSampling 家族在能力渲染路径删除 temperature/top_p/top_k，包括 Opus 4.7/4.8、5 系列、Fable/Mythos 等。
- 其他受限 thinking 路径删除 temperature/top_k，只保留 0.95–1 的 top_p；原生直通例外仍适用。
- IncludeThoughts=true/false 对应 thinking.display=summarized/omitted；隐藏摘要不关闭模型推理。
- 修改 effort 时合并 output_config 的其他键，例如结构化输出配置，不整对象覆盖。
- 重渲染后仍为 adaptive 时，adaptor 补回原生 thinking.block_binding；无修饰符的原生 thinking 还保留未知字段。
- 多个 anthropic-beta header 值合并转发；原生每消息 output_config 有保留测试。

可借鉴原则是只改明确需要改的字段，同时保留独立控制和会话元数据。[R15][R21]

### 3.10 本次实际执行的例子

均为本地运行固定源码函数，未调用付费模型。手动预算例子的 max_tokens=8192。

| 输入/入口 | 实际输出 |
| --- | --- |
| Chat→Opus 4.5，xhigh | effort=high；enabled；budget=7782 |
| Chat→Opus 4.6，xhigh | effort=max；adaptive；有 effort-adjusted 诊断 |
| Chat→Opus 4.8，xhigh | effort=xhigh；adaptive |
| 原生 Opus 4.5/4.6，无修饰符，xhigh | 保留 xhigh 和原 thinking，未校正档位 |
| Opus 5.5，none，进入 Renderer | adaptive+low；无法关闭诊断 |
| Sonnet 5.5，none，进入 Renderer | between_tools；不凭空增加 output effort；元数据 none |
| Sonnet 5.5，disabled+max | between_tools+high |
| Opus 5 / Haiku 5.5，disabled+max | disabled+high |
| 旧式模型，max_tokens=1000、budget=2000 | max_tokens=2001、budget=2000 |
| 宿主原生 Opus 4.6，无修饰符，enabled+budget=2048+effort=low | 保留原手动预算与 low |
| 同一请求加 -thinking，经宿主 helper→adaptor | 改 adaptive+high；原 budget 被清除；保留 output_config.format；有覆盖诊断 |
| 同一请求加 @thinking:on，经宿主 helper→adaptor | 同样改 adaptive+high；有覆盖诊断 |

三个实际观察到的限制：

1. Chat→Opus 4.5 的 xhigh→high **没有返回** claude_effort_adjusted；manual 分支的诊断覆盖不如 adaptive 完整。不能说每次降档都被记录。
2. Sonnet 5.5 的原生 between_tools 转 Responses，被 FromClaude 当作未知类型按 enabled 处理，输出 reasoning.effort=low、summary=detailed，同时报 claude_thinking_type_coerced。目标协议未必能表达该模式，但这证明“能够接受/生成该字段”不等于“跨协议语义完整”。[R03][R20]
3. 宿主模型修饰符的覆盖粒度比纯 Intent 合并函数更粗：裸开启也能改变已有强度和预算。已用实际 helper→adaptor 调用验证上述三组对照，不是仅按注释推断；如移植，应先明确“开启”是否授权覆盖具体强度。[R16][R48]

### 3.11 “有效 effort”不总等于线上的 output effort

EffectiveEffort 是转换/计价元数据，不保证与出站 output_config.effort 一致：

- disabled+max 可实际发送 output effort=high，而元数据是 none。
- Sonnet 4.5 的 xhigh 实际只发 budget=7782、没有 output effort，元数据仍可能是 xhigh。
- Opus 5.5 关闭请求转 adaptive+low，则元数据确实是 low。

本地借鉴时应能区分**请求档位、出站 thinking/effort/预算、计价档位**，不能把单一元数据称为实际模型算力。[R20]

## 四、类似的其他模型与协议处理

### 4.1 Gemini：按型号选择预算制或档位制

Gemini 有相似的 RenderGemini / NormalizeGeminiThinkingConfig，但同样要区分入口：**普通原生 Gemini、无宿主修饰符时提前返回，保留原生配置；跨协议或修饰符路径才做下面的归一化**。[R22][R23]

| 目标模型族 | 出站字段 | 约束与适配 |
| --- | --- | --- |
| Gemini 2.5 Pro | thinkingBudget | 正常预算 128–32768；不能关闭，0/none 改为 128；支持 -1 动态 |
| Gemini 2.5 Flash-Lite | thinkingBudget | 启用预算 512–24576；允许 0 关闭和 -1 动态 |
| 其他常规 Gemini 2.5 Flash | thinkingBudget | 上限 24576；允许关闭和动态预算 |
| 特定 2.5 Flash native-audio | thinkingBudget | 特判允许预算，上限 24576；不是所有音频模型都可配 |
| Gemini 3 Pro | thinkingLevel | low/high；minimal→low，medium→high，xhigh/max→high |
| Gemini 3.1 Pro / gemini-pro-latest | thinkingLevel | low/medium/high；minimal→low，xhigh/max→high |
| 常规 Gemini 3 Flash / 3.5 Flash / 3.6 Flash | thinkingLevel | minimal/low/medium/high；xhigh/max→high |
| Gemini 3.1 Flash Image / Flash-Lite Image | thinkingLevel | minimal/high；low→minimal，medium 及以上→high |
| Gemini 3 Pro Image / nano-banana-pro | 仅可保留 includeThoughts | 删除强度和预算控制并诊断 |
| 2.5 Flash Image、TTS 及其他被特判的 live/native-audio | 不可配置推理 | 去掉不支持的配置并诊断 |
| 未知型号 | 不凭空合成强度字段 | 保留原生表达；跨协议无法表达的强度报 unknown-capability |

源码按顺序匹配，音频特判先于一般不可配置规则；表中的家族名不是对供应商全部产品的承诺。[R22]

档位→2.5 预算：minimal/low=1024、medium=8192、high=24576、xhigh/max=该模型最大预算。不是所有 high 都用最大预算，例如 Pro 的 high=24576、max=32768。

3.x 档位模型不能用 none 真正关闭时，取该型号最低合法档位并告警。已知预算型号同时出现 budget+level 时保留 budget；已知档位型号保留 level。未知型号无法判断时，两者同时出现会报冲突错误。

跨协议时显式恢复源模型默认值：2.5 Flash-Lite 默认关闭，常规 2.5 保留动态 -1；3.5/3.6 Flash 和 flash-latest 默认 medium，Flash-Lite 部分型号默认 minimal，Pro 和旧 3 Flash 默认 high。默认值来自该提交的能力表，latest 别名变化后需重新核验。

本次实测：2.5 Pro 的 none→budget 128；3 Pro 的 medium→high，均有调整诊断。[R22]

其他 Gemini 兼容：支持函数调用 thoughtSignature 的可配置补充和 function-response ID 处理。缺签名时某些转换路径使用固定占位值；这不是恢复了真实签名或获得跨供应商续写能力，不能当作通用签名修复方案直接移植。[R23][R44]

### 4.2 OpenAI：独立能力开关，而不是所有 GPT 统一删除参数

GetOpenAIChatCapabilities 将规则拆成 max_completion_tokens、developer 角色、temperature、top_p、logprobs 能力，按**映射后的模型与已解析 effort**判断。[R24][R25]

| 类型 | 固定源码的处理 |
| --- | --- |
| o 系列 | 转 max_completion_tokens；移除 temperature；除 o1-mini/o1-preview 外使用 developer 角色；该分支并非把 top_p/logprobs 全删 |
| GPT-5 家族 | 使用 max_completion_tokens 和 developer；大部分推理情形不支持采样相关参数 |
| 明确的 gpt-5.1/5.2/5.4 标准型号及日期快照 | effort 为空/none 时允许采样；pro/codex/chat-latest 等命名变体不自动继承例外 |
| gpt-6-sol / gpt-6-luna 及日期快照 | 该提交中仅 effort 为空/none 时保留采样 |
| gpt-6-astra | 该提交中不保留上述采样控制 |
| 未识别的新型号 | 不自动继承已知型号限制 |

这是 New API 的实现表，不是额外开展的 OpenAI 全型号能力认证。角色替换代码实际处理首条 system 消息，不能泛化成清洗了所有历史角色。

原生 Chat 顶层 reasoning_effort 无转换/修饰符时尽量保留；嵌套 reasoning 方言、后缀或跨协议转换才触发统一投影。对 OpenAI/Azure 目标，投影后移除不属于其常规方言的嵌套 reasoning。**源码中并不存在覆盖所有 OpenAI 型号的完整 effort 合法档位夹取表**，不要把 Claude 的完整矩阵推断到 OpenAI。[R25]

### 4.3 OpenRouter：保留本平台的方言

- 接受顶层 reasoning_effort、嵌套 reasoning，以及部分 Anthropic 旧式 thinking 输入。
- 出站归一为 reasoning 对象，清空顶层 reasoning_effort。
- 禁用时 enabled=false，去掉 effort/max_tokens；有数字预算时 max_tokens 优先且去掉 effort；否则写 effort。
- IncludeThoughts 映射为 exclude 的反值，与是否启用推理分开。
- 合并已有 reasoning 对象，保留其他键；未指定 usage 时补 include=true，便于计量。

这说明统一 Intent 的作用是保存语义，再按目标方言编码，不是要求所有上游接受同一种字段。[R25]

### 4.4 DeepSeek、火山引擎、阿里/Qwen

| 适配 | 实际处理 | 不应过度推断 |
| --- | --- | --- |
| DeepSeek V4 -none | 去后缀；Chat 发 thinking.type=disabled；Claude 兼容入口发 disabled；Responses 发 reasoning.effort=none | 仅匹配 deepseek-v4- 前缀及专用尾缀 |
| DeepSeek V4 -max | 去后缀；Chat 发 enabled+reasoning_effort=max；Claude 发 enabled+output_config.effort=max；Responses 发 effort=max | 没有由此实现全系列七档转换 |
| 火山上的 deepseek*-thinking | 去 -thinking，发 thinking.type=enabled，尊重名字豁免 | 不是所有火山模型都强制 thinking |
| 阿里兼容入口 | 只有 Qwen/QwQ 目标保留 thinking_budget，其他模型移除该字段 | 不是把 enable_thinking 一律改 true/false；相关旧代码仅为注释 |
| 阿里 top_p | 显式 ≥1 改 0.99；≤0 改 0.01；未设置不注入 | 同时处理边界和部分模型只接受两位小数的要求 |

DeepSeek 的 Claude 后缀适配会直接重建 output_config；与通用 Claude 合并保留其他键的做法并不相同。这是移植时需要额外检查的字段保留差异。[R26][R27][R28]

### 4.5 适配开关和默认值

该提交宿主默认：Claude thinking_adapter_enabled=true、默认 max_tokens=8192、预算比例 0.8；Gemini thinking_adapter_enabled=false、预算比例 0.6。开关主要控制旧式模型尾缀，不是“是否允许推理”的总开关；显式 @ 修饰符和跨协议转换另有路径。[R43][R44]

模型修饰符还参与路由/计价身份的归一化。若借鉴这类语法，必须同时核对路由权限、模型价格、日志身份，不能只改发出的 model 字符串。[R16][R18]

## 五、错误、重试和流式中断

### 5.1 错误归一化

NewAPIError 统一携带 HTTP 状态、错误类别/代码、跳过重试标记和展示控制。上游错误解析尝试提取 OpenAI/Anthropic/Gemini 风格错误；非 JSON、空消息有单独诊断路径。对外依据入口包装为 OpenAI 或 Claude 错误，并带 request ID。[R01][R35][R46]

优势：本地请求错误、渠道错误、传输错误、上游错误可区别处理；展示信息和本地调试信息可分开；错误日志有敏感信息遮蔽。

限制：渠道状态码映射发生在通用重试决策前，可能同时改变重试和健康判断。要核验“映射只为了兼容客户端”还是“也要改变内部策略”；不能默认两者等价。[R07][R35]

### 5.2 重试判定的真实顺序

DecideRelayRetry 按以下顺序判断：

| 顺序 | 条件 | 决策/原因 |
| --- | --- | --- |
| 1 | 无错误 | stop / request_completed |
| 2 | strict 会话亲和 | stop / strict_session |
| 3 | 固定渠道禁止重试 | stop / pinned_channel |
| 4 | 被标记为渠道错误 | retry / channel_error |
| 5 | SkipRetry | stop / non_retryable_error |
| 6 | 普通错误无剩余次数 | stop / attempt_budget_exhausted |
| 7 | 2xx | stop / system_retry_exclusion |
| 8 | 状态值不在 100–599 | retry / unrecognized_status |
| 9 | 固定排除错误码/状态码 | stop / system_retry_exclusion |
| 10 | 命中管理员重试状态范围 | retry / retry_status_matched |
| 11 | 其他 | stop / status_not_retryable |

**渠道错误判断早于预算和 SkipRetry 判断**，已有单元测试覆盖“剩余 0 时渠道错误仍返回 retry”。外层循环仍有限制；不能据此宣称无限重试，也不能把决策函数说成严格全局次数裁决器。[R04]

### 5.3 默认状态码规则

以下适用于经过前置会话/渠道约束后的普通 HTTP 错误，配置可改变默认范围。

| 状态 | 默认是否重试 |
| --- | --- |
| 100–199 | 是 |
| 200–299 | 否 |
| 300–399 | 是（HTTP client 不自动跟随上游重定向） |
| 400、408 | 否 |
| 401–407、409–499（包括 429） | 是 |
| 500–503、505–523、525–599 | 是 |
| 504、524 | 固定排除 |
| BadResponseBody 错误码 | 固定排除 |

所以“400 一律换号”不是该项目的默认行为；“所有 5xx 都重试”也不正确。固定 504/524 排除优先于普通管理员状态范围，但显式 ChannelError 是前面单独的分支。[R34]

检查的 HTTP 控制器、统一重试函数和请求发送路径中，未见应用层指数退避、抖动或读取 Retry-After 后等待再重试的统一机制；这里主要是即时重新选渠道。底层 Go Transport 的透明重试、其他具体供应商的逻辑不能据此宣称不存在。[R01][R04][R30]

### 5.4 重试与渠道处罚分开

自动停用同时受全局开关和渠道 AutoBan 控制。普通 SkipRetry 错误不处罚；被标记 ChannelError、命中禁用状态码/关键字才可能停用。默认禁用状态范围仅 401，不是 429/5xx 全部停用。多 Key 渠道针对当前 Key 更新状态；停用整个渠道才进一步关活动 WS。自动恢复也受独立开关约束。[R36]

RequestPolicyState 记录 attempt、channel、group、error_source、status、decision.reason、elapsed 和 health，最多 512 个事件。日志因此能说明重试和健康动作的原因，而不必事后从散乱错误文本推断。[R37]

### 5.5 SSE 处理的长处

- Scanner 有大小上限和滑动空闲超时；每次读到行会重置超时。
- ping 与数据写共用互斥锁，防止并发写交叉；写前延长 deadline。
- scanner、handler、ping 退出有 once/WaitGroup 管理，避免 Gin context 回池后仍被 goroutine 使用。
- 客户端在扫描阶段断开会关闭上游响应体，解除阻塞，避免继续无谓生成。
- StreamStatus 将 done、timeout、client_gone、scanner_error、EOF 等传输结束原因与 completed/failed/incomplete/cancelled 业务状态分开；错误条目有上限。
- 转换状态保存工具增量、usage 和终止状态；终结步骤可补正确结束事件和 usage，Responses 可产生失败事件与 sequence_number。
- stop_reason 映射集中维护：tool_use→tool_calls，max_tokens→length，refusal→content_filter；pause_turn 不直接当普通成功 stop。

这些是值得复用的设计，但不是端到端“绝不重复/绝不误报成功”的证明。[R31][R32][R40][R47]

### 5.6 HTTP 已输出后重试：静态追踪确认的风险

通用 DecideRelayRetry 没有直接检查 Writer.Written、已经发出的业务 chunk 或上游请求是否已被接受。

已沿源码串起这条路径：Claude 流先输出若干事件 → 后续收到合法 Claude error envelope → HandleStreamResponseData 包装成 HTTP 500 的 NewAPIError → ClaudeStreamHandler 返回 → 主 Relay 按普通 500 决策可能再次尝试。该错误未自动标记 SkipRetry，而主重试函数也没有统一的“已开始输出”屏障。[R01][R04][R33]

**结论级别：源码可达性风险，尚未以完整 HTTP 客户端/上游替身复现整条链路。** 不能说已观察到线上重复回答，但也不能把这套重试实现直接当作安全模板。移植时应把以下情况作为禁止盲目重放的边界：已输出业务数据、工具动作可能发生、发送成功但结果未知、会话绑定不可迁移。

另外，IsNormalEnd 将 EOF/handler_stop 视作传输正常；MarkRequestPolicySuccess 没直接使用 expectsTerminal，并接受未记录业务终态的情况。部分转换终结器能补终态，但不能据此认定所有 EOF 都已被严格识别为成功或失败。原生流异常完成还需逐入口验证。[R32][R37]

### 5.7 WS 的重试边界更明确

Responses WS 每条 response.create 重新执行普通 TokenAuth、请求限流及计费；连接内一次只处理一个生成请求。初次连接上游的 dial 失败可以套公共重试策略；payload 写入失败标记 SkipRetry。建立后锁定模型、渠道、Key、分组，后续复核渠道、凭据、代理和路由是否仍有效。对旧 response 的重复终止、控制事件归属另作判断。[R06][R38]

值得学习的是“每轮重新准入”和“已写入后不盲目重放”。它不证明满足本地完整 Responses 恢复契约，更不构成替换当前原生 WS/BPS 恢复链路的依据。

## 六、请求体、计量与结算

### 6.1 请求重放的基础设施

BodyStorage 可内存或按阈值落临时磁盘；出站 ReplayableBody 暴露 Size/NewReader，补齐 ContentLength/GetBody。存储所有者保留关闭权，避免 http.Transport 关闭一次发送就毁掉其他重读所需数据。任务发送代码还明确避免把“同一个已读 reader”作为 GetBody 返回。[R29][R30]

底层 HTTP client 复用 Transport/连接池，用浅拷贝修改 CheckRedirect，避免污染共享 client；上游 3xx 保留给 relay 决策。header 通配/正则透传排除 Authorization、各供应商 API Key、Cookie、hop-by-hop、WS 握手等字段；明确配置的 header override 另有较高优先级。[R30]

### 6.2 Usage 与缓存

统一 Usage 外还保存原始供应商 BillingUsage 和来源协议，避免把客户端看到的转换后 usage 当作计费真相。Claude 缓存命中与 5 分钟/1 小时写入拆开；流式 usage 按事件语义合并，非零更新避免后续空字段冲掉已有计数。[R41]

Claude 流缺末尾 usage 时可估算缺失部分，同时保留已经拿到的 cache 信息；源码包含每个 tool_use 约 40 个额外输出 token 的经验补偿。**这是估算策略，不是供应商精确用量保证**。如果本地要采用，应保留 estimated 的可见性，不能伪装成上游报告值。[R33]

### 6.3 结算状态机

BillingSession 把预扣、补充预留、资金结算、Token 配额调整、退款放进一个会话，带锁和 settled/refunded/fundingSettled 标记。资金已提交但 Token 调整失败时，避免再次退款资金；退款调用也防重复。跨渠道尝试共享会话和定价信息。[R05][R08]

价值是减少业务重试与账务重试相互干扰；这些是会话级幂等保护，不应扩张为已证明跨进程、所有存储失败情况下 exactly-once 的完整分布式账务保证。

## 七、与当前 sub2api 的针对性对照

这里只比较已读取的关键实现，不以一次关键词搜索宣称整个本地项目缺少某能力。

| 本地已存在的基础 | 本次确认的事实 | 有价值的下一步 |
| --- | --- | --- |
| claude/effort_catalog.go | 已有 Claude 家族合法档位，且对 provider 前缀、区域、日期、点号版本等做归一化 | 复用该目录作为发送规则依据；不要复制一份 New API 能力表 |
| openai_codex_models_service.go | EffortLevelsForModel 的主要运行引用用于候选档位展示 | 检查“展示支持”与“实际转发使用”是否一致 |
| ResponsesToAnthropicRequest | 5.5 有专项；其他路径仍有统一 xhigh→max 和固定手动预算 | 针对 4.5/4.6/4.7/4.8/Fable 等接上模型能力；先验证最终发送体 |
| 同一转换函数的默认值 | 默认 max_tokens=8192，而非 5.5 的 high 预算为 10240、max 为 32768 | 函数输出可能不满足预算约束；不能据此断言生产必错，须追踪后续是否修正 |
| gateway_request.go | 已有 5.5 thinking/tool_choice 校验、签名敏感块与预算恢复逻辑 | 复用原路径，避免新适配层与原恢复规则互相打架 |
| 分组 effort policy | 已有最高档位、映射、deny 等策略 | 用户策略、模型能力与计价含义要分开；禁止能力降档绕过 deny |
| 请求/实发 effort 计价测试 | 已有相关测试与日志字段 | 验证最终发送参数与计价档位一致，而不只验证请求输入值 |

本地证据：[能力目录][L01]、[候选档位调用][L02]、[Responses→Anthropic][L03]、[请求校验与恢复][L04]、[分组策略][L05]、[计价验证][L06]。

本地 5.5 处理并非空白：Sonnet 5.5 的 none 会转 between_tools+low；Opus 5.5 的 none 在该转换入口选择拒绝，而 New API Renderer 选择 adaptive+low。这是**请求语义策略的差异**，不是简单认为哪一边更新或更强；是否允许把“关闭”改成“最低思考”需有明确产品语义。

### 7.1 建议实施顺序（本次未实施）

1. **先核对最终出站体**：覆盖原生 Messages、Chat/Responses 转 Messages、API Key/OAuth、云平台/兼容服务、映射模型；找出现有清洗、分组策略和错误恢复的交叉点。
2. **统一现有模型能力来源**：扩展已有目录承载需要的 thinking 模式、输出 effort、采样约束；只有现有逻辑确实无法表达才新增最小字段/助手函数。
3. **明确策略**：原生未知字段如何保留；非法档位拒绝还是映射；不能关闭时拒绝还是最低档；是否允许提高 max_tokens；管理员覆盖是否绕过能力检查。
4. **补最终边界和诊断**：在现有出站路径确保档位、预算、参数和计价一致；利用现有日志承载请求值/发送值/修正原因，先不新增独立运营系统。
5. **检查重放边界**：复用现有 Responses/BPS 恢复契约，重点验证已经输出、工具副作用、发送后结果未知三类请求不盲目重试。
6. **按独立风险补测试**：在已有测试中加入模型能力、透传、预算和流终止矩阵，再决定是否提交实现。

共享网关改动若未来获准交付，应按现行规范先 main，再普通 merge 到 TapModels/tokensavy。本次研究不引入第三方整分支，不恢复退役能力，不触发提交、推送、发版、部署或迁移。

### 7.2 建议验收矩阵

| 风险 | 应验证的输入/结果 |
| --- | --- |
| 模型映射时序 | 客户端别名映射到不同 Claude 家族后，使用最终目标能力 |
| 合法档位 | 每个能力族 × none/minimal/low/medium/high/xhigh/max；未知值不随机降档 |
| 开关不等于强度 | 仅 output effort、thinking disabled+effort、只隐藏摘要 |
| 预算边界 | 0、-1、1023、1024、等于 max_tokens、超出 max_tokens、整数溢出 |
| 原生兼容 | 未知 thinking/output_config 字段、每消息 effort、block_binding、签名块 |
| 模型名完整性 | -thinking 真实 ID、codex-max、@ 在真实名字中、日期/云平台命名 |
| 多跳转换 | Gemini→Chat→Claude 精确预算与可见性不无意丢失 |
| 计价 | 请求值、调整后参数、计价档位逐项对照 |
| 参数覆盖 | 管理员 override 后的最终发送仍满足已决定的策略 |
| 流恢复 | 已输出后 error、EOF 无终态、客户端断开、重复终态、工具调用中断 |
| 策略优先级 | 用户 deny、分组最高档位、固定账号、会话亲和都不能被能力适配绕过 |

## 八、本次验证、范围与交付

### 8.1 实际执行结果

| 项目 | 结果 | 说明 |
| --- | --- | --- |
| 固定公开源码 | 633b4c5b190dc1af99a2814391354d86948e7943 | 2026-10-12 获取的 main 快照 |
| RelayKit 全模块测试 | 9 个有测试包通过；196 个顶层测试、含子测试共 530 个通过 | 另外 8 个包无测试文件，不算通过测试数 |
| 宿主定向测试 | 7 个包通过；50 个顶层测试、含子测试共 86 个通过 | 另外 DeepSeek 包无测试文件；不是根模块全量测试 |
| 本地转换探针 | 125 次原函数调用完成、无调用错误 | 98 个 Claude 模型×档位组合，4 个关闭+max，1 个预算边界，6 个原生/转换对照，15 个 Gemini 组合，1 个 between_tools 跨协议 |
| 宿主后缀集成探针 | 3 次 helper→adaptor 调用完成、无调用错误 | 同一原生 Opus 4.6 请求：无后缀、-thinking、@thinking:on；确认裸开启覆盖原强度/预算 |
| 真实供应商调用 | 未执行 | 未产生付费模型调用；能力合法性仍不能仅由本地函数结果证明 |
| 完整端到端 HTTP/WS 与故障注入 | 未执行 | HTTP 已输出后重试标为源码风险，不虚报已复现生产事故 |
| sub2api 业务修改 | 无 | 仅研究文档 |

Go 实际使用 /Users/luhonglin/sdk/go1.27/bin/go，版本 go1.27.0 darwin/arm64；最初 PATH 中无 go，随后找到现有 SDK 完成测试，未安装或升级系统工具链。初稿探针有一次字符串转义编译错误，修正后完成上述 125 次调用；不将首次失败算作成功验证。

执行命令（从相应源码目录运行）：

~~~sh
# relaykit 子模块
/Users/luhonglin/sdk/go1.27/bin/go test -json ./...

# new-api 根模块：只运行所列名称匹配的测试
/Users/luhonglin/sdk/go1.27/bin/go test -json   ./service ./relay/helper ./relay/channel/claude ./relay/channel/openai   ./relay/channel/deepseek ./relay/channel/ali   ./setting/operation_setting ./setting/reasoning   -run 'Test(DecideRelayRetry|ShouldRetryRelayError|ResetStatusCode|RelayErrorHandler|ApplyReasoningModelSuffix|ConvertClaudeRequest|SetupRequestHeaderForwardsEveryAnthropicBetaValue|ConvertOpenAIRequest|ShouldRetryByStatusCode|IsAlwaysSkipRetryStatusCode|ParseDeepSeek)'
~~~

### 8.2 本地证据位置

- 研究源码：/tmp/new-api-research-20261012.rnXsIf。
- RelayKit 测试原始 JSON：/tmp/new-api-relaykit-20261012-tests.jsonl。
- 宿主定向测试原始 JSON：/tmp/new-api-host-20261012-tests.jsonl。
- 探针源码：/tmp/new-api-reasoning-20261012-probe.go。
- 探针输出：/tmp/new-api-reasoning-20261012-probe.jsonl。
- 宿主后缀探针源码/输出：/tmp/new-api-host-thinking-20261012-probe.go、/tmp/new-api-host-thinking-20261012-probe.jsonl。

临时目录可能被系统清理；关键结果已固化在本文，源码证据使用不可变提交链接。本文不是对 New API 全仓全部供应商、插件及生产部署的安全审计。

### 8.3 变更统计与授权边界

项目业务代码 +0/−0；项目测试代码 +0/−0；新增接口 0、配置项 0、业务执行路径 0。交付一份本地研究文档。仓库现有 .gitignore 对 docs/* 的规则忽略本文件；未修改忽略规则、未暂存或提交，Git 工作区业务文件保持不变。

## 九、固定源码与官方文档索引

所有 R 编号均指向本次固定提交。A 编号是研究当天读取的供应商官方文档；L 编号是本地对照文件。每条结论以上文指定函数和入口为边界。
### 9.1 New API 固定源码

| 编号 | 文件 | 重点 |
| --- | --- | --- |
| R01 | [controller/relay.go][R01] | HTTP 主控制器：预扣、渠道尝试、错误与退款 |
| R02 | [relaykit/relayconvert/request_registry.go][R02] | 请求转换注册表、直达与多跳路径 |
| R03 | [relaykit/relayconvert/reasoning/intent.go][R03] | 统一 Intent、解析、合并、预算到档位 |
| R04 | [service/relay_error.go][R04] | DecideRelayRetry 统一决策顺序 |
| R05 | [service/billing_session.go][R05] | 预扣、补充预留、结算、退款状态 |
| R06 | [controller/responses_websocket.go][R06] | WS 每轮鉴权、限流入口 |
| R07 | [relay/claude_handler.go][R07] | Claude 单次尝试与参数覆盖顺序 |
| R08 | [relay/request_billing.go][R08] | 请求级预扣、共享会话与计价快照 |
| R09 | [service/channel_select.go][R09] | 候选约束、亲和、跨组与重试选择 |
| R10 | [model/channel_cache.go][R10] | 模型候选、优先级与权重 |
| R11 | [service/request_policy.go][R11] | 会话策略与成功标记 |
| R12 | [relay/helper/model_mapped.go][R12] | 链式模型映射及循环检查 |
| R13 | [relaykit/README.md][R13] | 模块边界与协议转换质量矩阵 |
| R14 | [relaykit/relayconvert/tool_loss_policy_test.go][R14] | 工具损失策略的请求/响应边界测试 |
| R15 | [relaykit/relayconvert/internal/shared/claude/reasoning.go][R15] | Claude 原生旁路、参数应用及字段保留 |
| R16 | [relay/helper/reasoning_suffix.go][R16] | 宿主后缀与修饰符优先级 |
| R17 | [relaykit/dto/reasoning_state.go][R17] | 仅进程内传递的推理状态 |
| R18 | [relaykit/relayconvert/reasoning/suffix.go][R18] | 通用旧式后缀解析与豁免 |
| R19 | [relay/convert_request_error.go][R19] | 转换客户端错误 400 / SkipRetry |
| R20 | [relaykit/relayconvert/reasoning/claude.go][R20] | 完整 Claude 能力、档位、预算 Renderer |
| R21 | [relay/channel/claude/adaptor.go][R21] | Claude 认证头、原生字段与适配入口 |
| R22 | [relaykit/relayconvert/reasoning/gemini.go][R22] | 完整 Gemini 能力与预算/档位 Renderer |
| R23 | [relaykit/relayconvert/internal/shared/gemini/request.go][R23] | Gemini 原生旁路、默认值和签名适配 |
| R24 | [relaykit/dto/openai_request.go][R24] | OpenAI 型号能力与请求参数约束 |
| R25 | [relay/channel/openai/adaptor.go][R25] | OpenAI / Azure / OpenRouter 出站投影 |
| R26 | [relay/channel/deepseek/adaptor.go][R26] | DeepSeek V4 none / max 后缀 |
| R27 | [relay/channel/ali/text.go][R27] | Qwen 预算字段与 top_p 边界 |
| R28 | [relay/channel/volcengine/adaptor.go][R28] | 火山 DeepSeek thinking 后缀 |
| R29 | [common/body_storage.go][R29] | 内存/磁盘请求体存储与所有权 |
| R30 | [relay/channel/api_request.go][R30] | 可重放请求体、HTTP 客户端、重定向和头部 |
| R31 | [relay/helper/stream_scanner.go][R31] | SSE 扫描、超时、ping、断连与清理 |
| R32 | [relay/common/stream_status.go][R32] | 传输结束与业务终态分离 |
| R33 | [relay/channel/claude/relay-claude.go][R33] | Claude 流错误、usage、缺失用量估计 |
| R34 | [setting/operation_setting/status_code_ranges.go][R34] | 默认重试范围和固定排除项 |
| R35 | [service/error.go][R35] | 上游错误解析、展示与状态码映射 |
| R36 | [service/channel.go][R36] | 渠道自动停用、恢复与多 Key 状态 |
| R37 | [service/request_policy.go][R37] | 可解释的决策事件与成功判定 |
| R38 | [relay/responses_websocket.go][R38] | WS dial 重试、发送后边界和会话锁定 |
| R39 | [relay/common/conversion_diagnostics.go][R39] | 转换诊断收集与上限 |
| R40 | [relaykit/relayconvert/response_registry.go][R40] | 响应、流式转换与终结器注册 |
| R41 | [relaykit/dto/billing_usage.go][R41] | 原始供应商计费用量语义 |
| R42 | [relay/request_clone_test.go][R42] | 渠道尝试中的请求拷贝测试 |
| R43 | [setting/model_setting/claude.go][R43] | Claude 宿主默认设置 |
| R44 | [setting/model_setting/gemini.go][R44] | Gemini 宿主默认设置 |
| R45 | [relay/channel/claude/adaptor_test.go][R45] | 原生字段与 Claude 适配保留测试 |
| R46 | [relaykit/types/error.go][R46] | 统一错误结构及跳过重试标记 |
| R47 | [relaykit/reasonmap/reasonmap.go][R47] | 结束原因跨协议映射 |
| R48 | [relay/helper/model_modifier.go][R48] | @ 语法和宿主参数覆盖/清理 |
| R49 | [relaykit/relayconvert/convmeta/options.go][R49] | allow / safe / strict 默认与选项 |
| R50 | [setting/reasoning/suffix.go][R50] | 宿主后缀解析开关与计价/路由名称 |

### 9.2 官方语义核对

- [A01：Anthropic effort 文档][A01]：effort 与 thinking 独立，支持档位与不能关闭的模型。
- [A02：Sonnet 5.5 迁移文档][A02]：between_tools 与 effort 上限。

这些页面可能继续更新；本文能力矩阵仍以固定 New API 提交为准。

### 9.3 本地实现对照

| 编号 | 文件 | 重点 |
| --- | --- | --- |
| L01 | [backend/internal/pkg/claude/effort_catalog.go][L01] | 现有 Claude 模型档位目录 |
| L02 | [backend/internal/service/openai_codex_models_service.go][L02] | 向客户端展示候选档位 |
| L03 | [backend/internal/pkg/apicompat/responses_to_anthropic_request.go][L03] | Responses 到 Anthropic 转换 |
| L04 | [backend/internal/service/gateway_request.go][L04] | 原生请求校验和恢复 |
| L05 | [backend/internal/service/openai_reasoning_effort_policy.go][L05] | 分组 effort 限制、映射与拒绝 |
| L06 | [backend/internal/service/gateway_compat_reasoning_pricing_test.go][L06] | 请求/实发 effort 计价验证 |

[R01]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/controller/relay.go
[R02]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/relayconvert/request_registry.go
[R03]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/relayconvert/reasoning/intent.go
[R04]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/service/relay_error.go
[R05]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/service/billing_session.go
[R06]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/controller/responses_websocket.go
[R07]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/claude_handler.go
[R08]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/request_billing.go
[R09]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/service/channel_select.go
[R10]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/model/channel_cache.go
[R11]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/service/request_policy.go
[R12]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/helper/model_mapped.go
[R13]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/README.md
[R14]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/relayconvert/tool_loss_policy_test.go
[R15]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/relayconvert/internal/shared/claude/reasoning.go
[R16]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/helper/reasoning_suffix.go
[R17]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/dto/reasoning_state.go
[R18]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/relayconvert/reasoning/suffix.go
[R19]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/convert_request_error.go
[R20]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/relayconvert/reasoning/claude.go
[R21]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/channel/claude/adaptor.go
[R22]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/relayconvert/reasoning/gemini.go
[R23]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/relayconvert/internal/shared/gemini/request.go
[R24]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/dto/openai_request.go
[R25]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/channel/openai/adaptor.go
[R26]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/channel/deepseek/adaptor.go
[R27]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/channel/ali/text.go
[R28]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/channel/volcengine/adaptor.go
[R29]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/common/body_storage.go
[R30]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/channel/api_request.go
[R31]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/helper/stream_scanner.go
[R32]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/common/stream_status.go
[R33]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/channel/claude/relay-claude.go
[R34]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/setting/operation_setting/status_code_ranges.go
[R35]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/service/error.go
[R36]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/service/channel.go
[R37]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/service/request_policy.go
[R38]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/responses_websocket.go
[R39]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/common/conversion_diagnostics.go
[R40]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/relayconvert/response_registry.go
[R41]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/dto/billing_usage.go
[R42]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/request_clone_test.go
[R43]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/setting/model_setting/claude.go
[R44]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/setting/model_setting/gemini.go
[R45]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/channel/claude/adaptor_test.go
[R46]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/types/error.go
[R47]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/reasonmap/reasonmap.go
[R48]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relay/helper/model_modifier.go
[R49]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/relaykit/relayconvert/convmeta/options.go
[R50]: https://github.com/QuantumNous/new-api/blob/633b4c5b190dc1af99a2814391354d86948e7943/setting/reasoning/suffix.go
[A01]: https://platform.claude.com/docs/en/build-with-claude/effort
[A02]: https://platform.claude.com/docs/en/models/sonnet-5-5/migration-guide
[L01]: /Users/luhonglin/Git/sub2api/backend/internal/pkg/claude/effort_catalog.go
[L02]: /Users/luhonglin/Git/sub2api/backend/internal/service/openai_codex_models_service.go
[L03]: /Users/luhonglin/Git/sub2api/backend/internal/pkg/apicompat/responses_to_anthropic_request.go
[L04]: /Users/luhonglin/Git/sub2api/backend/internal/service/gateway_request.go
[L05]: /Users/luhonglin/Git/sub2api/backend/internal/service/openai_reasoning_effort_policy.go
[L06]: /Users/luhonglin/Git/sub2api/backend/internal/service/gateway_compat_reasoning_pricing_test.go
