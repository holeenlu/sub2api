# New API 方案实施与二次复核记录

日期：2026-10-13（Asia/Shanghai）。范围：KDAN / TapModels / Tokensavy 共享网关能力。状态：实现、二次代码复核及用户验收意见修订完成；当前候选完整 unit、定向 race、vet/build 均通过，独立 lint 新增问题为 0、全仓仍有 48 条基线问题。保留首轮候选记录用于区分复核对象，当前结果见末节。

交付说明：用户随后于 2026-10-13 授权提交、推送及清理旧分支。依共享能力交付规范，代码与研究、计划、验收文档一并纳入 main，再普通合并至 TapModels、tokensavy，目标仅为 holeenlu/sub2api 的三个对应 origin 分支。下文“未暂存、提交、推送”和“docs 忽略范围”描述验收完成时的快照；此次显式纳入这三份文档，不修改 .gitignore。生产部署、发版与迁移不在本次授权内。

## 工作区与授权

- 工作目录：/Users/luhonglin/Git/sub2api。
- 基线：main@c575e66799775e649e08e45053034f90a8fcd96a。
- 工作分支：codex/new-api-adoption-20261013。
- 用户要求完成后先不要提交、推送，且必须二次复核；本轮未暂存、未提交、未推送、未发布、未部署、未执行数据库迁移。
- 依据：同目录 new-api-adoption-plan-2026-10-12.md、new-api-relay-research-2026-10-12.md。New API 研究固定为 633b4c5b190dc1af99a2814391354d86948e7943；采用行为方案并复用本地代码，不引入整个仓库或新运行依赖。

## 实施结果

| 阶段 | 实际实现或验证 |
| --- | --- |
| S0 | 追踪原生 Messages、Chat/Responses 转 Messages、API Key 透传、OAuth、Bedrock、Vertex 最末构建钩点；以可控 HTTP 上游截获最终请求。原生请求按账号尝试克隆，避免前次规范化污染原始意图。 |
| S1 | 扩展现有 claude/effort_catalog.go：同一能力表供候选目录和请求适配使用。区分未知模型与明确无 effort 的老模型；准确匹配日期、云平台、点号版本等既有模型标识，不推测未来小版本。 |
| S2 | 新增纯局部 JSON 适配入口，按最终映射模型调整模式、档位与预算。三种入口使用共同规则；补齐兼容入口策略、最终合法档位与策略上限交集、原始/发送档位区分。Bedrock 保留合法 effort 与 Opus 4.5 beta，计价读取真正发送值。 |
| S3 | Gemini generateContent 原生、Messages、Chat 各自按实际模型处理 budget/level、不能关闭、冲突及别名；预算不推断计费档位。Antigravity 使用中间表示转换，避免继承 Claude API 模式限制。OpenAI/Codex、GLM/Kimi/DeepSeek/Qwen 复用原有方言与回归，不新建供应商接入。 |
| S4 | 复核并回归已有重放证明、同 turn 预算、终态、取消、已观察用量、WS 归属和 BPS 显式恢复契约。未另建重试器或扩大可重放条件；本地模式/策略拒绝不进入上游重试。 |
| S5 | 复用现有 Usage/计费及 Ops 诊断。新增有界 thinking 调整日志，仅记录枚举、最终模型、预算和策略信息；测试不泄漏对话、签名、工具、凭据且幂等检查不重复记录。补充最终代码复核与完整验证记录。 |

### 原生现场错误的实际处理

| 样例 | 本轮结果 | 明确边界 |
| --- | --- | --- |
| #25177 Sonnet 5.5，旧模式 enabled + budget | 转 adaptive，明确 effort 优先；仅有预算时近似映射到档位，去掉不支持的预算。 | 合成请求可完成；effort 不是精确 token 预算。原始现场完整请求体未提供，不能确认当时具体模式。 |
| #25177 Sonnet 5.5，旧模式 disabled | 转 between_tools；原生省略 effort 保持省略（默认 high），显式 xhigh/max 收敛为 high；清除该模式不允许的 budget_tokens/display/block_binding。 | 仅关闭前置思考，工具间仍可能有思考。保留 forced-tool、采样及每消息限制；跨协议显式 none 的既有映射仍为 between_tools + low。 |
| #25176 Fable 5.1，disabled | 在本地返回明确 400，提示省略 thinking 或用 adaptive + 合法 effort；零上游请求。 | 严格关闭不能实现，本次没有将 disabled 静默改成 adaptive+low。客户端仍须调整请求；不能声称该输入已变成成功调用。 |

原始用户要求没有授权将“关闭思考”改为“最低思考”，沿用已确定的严格口径。客户端省略 thinking 时按模型既有默认执行；合法的摘要显示、独立 effort、扩展字段和签名历史分别处理。已存在的原生显式手动预算校验/修复契约继续保留；网关派生预算不得抬高客户端明确的 max_tokens。

### 主要代码与新增接口的必要性

- internal/pkg/claude/effort_catalog.go：已有档位目录无法表达 adaptive/manual/disabled/between_tools 联合约束，新增最小能力结构，避免服务层复制模型判断。
- internal/pkg/claude/thinking.go：一处纯函数承载局部调整及派生预算，不整对象重写历史。
- internal/pkg/apicompat/responses_to_anthropic_request.go：保留最终 Messages 转换；新增中间表示入口以区分 Gemini/Antigravity 的真实出站协议，签名封装解码不等同于套用模式规则。
- internal/service/openai_reasoning_effort_policy.go：已有 pending policy 无法区分“等待桥接后应用”与“入口已应用”，新增 applied 状态绑定以避免链式映射和默认值再次被处理；原始显式预算转换为 effort 后仍受有效上限约束。
- internal/service/gemini_thinking.go：Gemini 的字段、预算范围及关闭能力与 Claude 不同，必须由自己的协议路径处理。
- 现有 handler、原生/兼容请求构建、计费结果读取就地接线。删除旧通用 effort 映射/预算 helper 与已不再调用的 Bedrock Opus 版本判断；不保留两份 Claude 模型规则。

## 二次复核

执行方式：完成首轮实现后，由主代理再次从最终差异、入口到实发链路、签名/策略/计价和回归证据逐项复核，并修正发现的问题。没有调用独立子代理，也不把自动测试冒充第二位审阅者。

| 复核发现 | 修正与证据 |
| --- | --- |
| Bedrock 仍按旧规则删除非 Sonnet 5.5 的 effort，且计价可能使用被删前值 | 按能力保留；Opus 4.5 自动补必要 beta，同时尊重管理员 beta 过滤；ForwardResult 与 handler fallback 读取最终 InvokeModel body。TestClaudeBedrockFinalWireEffort、TestBedrockEffortBetaFilterRespectsPolicy。 |
| Anthropic 组的 Chat/Responses handler 缺入口策略；桥接新绑定策略又可能重复运行映射 | 两入口在选号前校验；区分已应用与待应用策略。TestAnthropicCompatibleIngressEffortDeniedBeforeScheduling、TestAppliedIngressEffortPolicyDoesNotRunAgainInBridge。 |
| 兼容转换写入的默认 effort 可能被误认为客户端显式请求 | 绑定最终上限时保留原始“未指定”语义；显式预算近似转换则仍检查。TestClaudeThinkingCeilingDistinguishesIntentFromBridgeDefaults。 |
| 原生请求切到兜底分组时，可能沿用前一分组的映射和上限 | 保存初始输入，兜底分组按原始意图重新应用策略并绑定自身上限。TestFallbackGroupEffortPolicyReevaluatesOriginalIntent，结合实际切组调用点复核。 |
| 把 Messages 作为中间表示时，新协议隔离可能连带丢掉已编码的签名历史 | 独立保留受支持模型的桥接签名封装，不把 OpenAI 原生密文伪装成 Anthropic 签名。TestResponsesIntermediateKeepsSignedThinkingWithoutClaudeModeRestrictions。 |
| 扩展默认思考模型后，可能意外将 5.5 的整段无效历史清理行为扩展到 Fable | 分开默认思考与原 5.5 清理边界；Fable 的其他有效签名保持。TestFableDefaultThinkingKeepsOtherValidSignatures。 |
| Gemini 重建 thinkingConfig 会丢掉摘要可见性/扩展字段；未知目标可能丢掉既有通用转换 | 改为局部字段修改；未知目标保留既有中间转换预算。TestClaudeToGeminiEffortPreservesVisibilityAndExtensions，加上原有协议转换回归。 |

本次不修改 BPS manifest 覆盖的文件（已核对交集为空），不改固定来源 SHA；BPS 运行契约通过共享 handler 策略回归及已有 HTTP/SSE/WS 测试校验。

### 用户独立验收后的修订

- Opus 5 的请求级 disabled effort 上限，与 Haiku 5.5 的历史一致性要求分离。新增一个实际使用的内部能力字段 DisableFixedEffort，避免把 DisableHighOnly 同时解释为两种规则；不新增配置或远程接口。
- 原生 Sonnet 5.5 disabled 且省略 effort 不再注入 low；历史 high 可通过，显式 low 与历史 high 的冲突仍拒绝。历史消息按原样保留，规范化保持幂等。跨协议 none 在现有转换器设置 low，维持此前通过验收的契约。
- 保留并明确记载两项有感变化：跨协议 low 对手动模型开启 1024 预算；派生手动预算时明确输出上限为 1–1024 则本地拒绝，不抬高用户上限、不改为关闭。none/省略的区别见计划第 15 节及 TestResponsesManualThinkingIntentAndLimit。
- QF1003 对应分支改为 tagged switch。永久回归加入 TestNormalizeThinking、TestClaudeThinkingLocalRejectionAndCeiling；最终发送体用所需 beta 验证历史保留，首轮夹具遗漏 beta 导致的断言失败已通过修正夹具解决，未修改现有 beta 净化规则。

## 验收矩阵

以下列出代表性证据，完整用例结果见自动化日志；不是仅凭函数命名判断通过。

| 编号 | 验证证据 |
| --- | --- |
| T01/T02 | TestEffortLevelsForModel、能力别名/未知模型测试；TestClaudeThinkingNativeWireAndAttemptIsolation、TestGatewayAnthropicCompatReasoningPricingUsesForwardedEffort。 |
| T03/T04 | TestNormalizeThinking、TestSonnet55ResponsesThinkingAndSampling、既有 Opus/Sonnet 5.5 forced-tool 与采样回归、原生本地拒绝。 |
| T05 | TestThinkingBudgetBoundaries；整数溢出、1023/1024、max_tokens 下限与预算夹取。原生明确预算仍沿用既有契约。 |
| T06 | TestNormalizeThinkingPreservesHistoryAndExtensions、现有 5.5 signed thinking round-trip、新增中间转换签名与 Fable 签名回归。 |
| T07 | 新增入口拒绝、mapping 仅一次、模型能力与 ceiling 交集、显式/省略区分和兜底分组原始意图用例；既有 policy deny/mapping tests。 |
| T08 | 原生 API Key 透传/标准 × stream/buffered；OAuth 与 count_tokens 实发；Chat/Responses 的流式/非流式计价；Bedrock 实发。Vertex 复用共享构建入口和现有回归，无真实云端调用。 |
| T09 | TestClaudeThinkingNativeWireAndAttemptIsolation、既有跨账号 bounded retry 与共享预算用例。 |
| T10 | TestRecordUsage_ReasoningPricingUsesForwardedEffort、转换/原生计价、Gemini native pricing、Bedrock 最终 body 与结果档位一致。 |
| T11 | TestOpenAIForwardReplaySafetyFollowsFinalToolPolicy、MissingTerminalBefore/AfterOutput、裸 error/部分输出后禁止 failover、写入后不确定执行恢复等既有用例。 |
| T12 | TestOpenAIWSAccountRefreshRespectsCredentialOwnership、TestOpenAIWSBridgeEncryptedRejectionRetainsUsageWithoutAccountPenalty、BPS403Recovery、WSBPS 系列。 |
| T13 | TestOpenAISSEReadPumpCancelAndBoundedQueue、FirstOutputDeadlineNotExtendedByComments、ExcelBPSClientCancellation、现有 usage/并发回收回归及 race。 |
| T14 | TestNormalizeGeminiThinking、TestGeminiThinkingUsesMappedProtocolAndRejectsLocally、既有 OpenAI/国产兼容/BPS 用例；无新增独立供应商入口。 |
| T15 | TestClaudeThinkingDiagnosticsAreBoundedAndDoNotIncludeContent、TestOpenAIRetryStopCapturesBoundedProgressWithoutContent；废弃 helper 清理与静态检查。 |

## 首轮候选自动化检查（历史记录）

工具链：Go 1.27.2 darwin/arm64。只使用测试替身/本地资源，不调用付费上游或生产服务。全部命令在 backend 目录执行；下面的 unit 与 make test-unit 具有同一包/标签范围，额外使用 JSON 输出保存证据。

| 检查 | 命令 | 最终结果 |
| --- | --- | --- |
| 后端完整 unit | go test -tags=unit -json ./... | 退出码 0；24,711 条测试通过事件（含子测试；12,355 个顶层测试），60 个测试包通过，52 个包无测试，18 项依既有条件跳过。 |
| 定向竞态 | 四个包的 -race -tags=unit 定向回归；筛选式见下文 | 退出码 0；185 条测试通过事件（含子测试；45 个顶层测试），4 个包通过，无 race 告警。 |
| 静态检查 | go vet -tags=unit ./... | 退出码 0，无输出。 |
| 构建 | CGO_ENABLED=0 go build -trimpath -o /tmp/sub2api-new-api-server-20261013 ./cmd/server | 退出码 0。 |
| 差异/格式 | git diff --check；对全部变更和新 Go 文件运行 gofmt | 通过。 |
| 独立 lint | golangci-lint run ./... | 未执行；无本地工具/缓存，模块元数据获取遇到 proxy.golang.org:443 i/o timeout。 |

竞态筛选完整命令：

~~~sh
go test -race -tags=unit -json ./internal/pkg/claude ./internal/pkg/apicompat ./internal/service ./internal/handler -run 'Test(ClaudeThinking|ClaudeBedrockFinalWire|FableDefaultThinking|ResponsesIntermediate|NormalizeThinking|ThinkingBudget|NormalizeGeminiThinking|ClaudeToGemini|AppliedIngressEffort|AnthropicCompatibleIngressEffort|FallbackGroupEffort|OpenAIWSAccountRefreshRespectsCredentialOwnership|OpenAIRetryStopCapturesBoundedProgressWithoutContent|OpenAISSEReadPump|OpenAIWSBPS|ExcelBPS403Recovery|OpenAIForwardReplaySafety)'
~~~

日志位于 /tmp/sub2api-new-api-verified-unit-20261013.jsonl、/tmp/sub2api-new-api-verified-race-20261013.jsonl、/tmp/sub2api-new-api-verified-vet-20261013.log、/tmp/sub2api-new-api-verified-build-20261013.log，unit/race 各有同名前缀 .stderr 文件（为空）。构建产物只在 /tmp。

18 项跳过包括显式网络/付费 API 门控、Linux 抓包样本、未配置的 Prompt Audit Redis/PostgreSQL、历史 NULL PostgreSQL 用例、插件进程集成条件和一项已有 sentinel；没有把跳过记为通过。本次新增回归没有跳过。先前迭代中的旧预期失败与编译诊断均已修正，最终记录不以早期失败日志充当通过证明。

首轮候选 35 个变更/新 Go 文件按路径排序，以 path + NUL + 原始文件字节串联计算的 SHA-256：8ee11a2f46e563986f562d3266fcac2a838f8a4559c9808964214728b7105ecb。这是用户独立复核时的对象；后续修正改变了源码，当前候选摘要见末节，不沿用此摘要表示最终状态。

## 变更数量与边界

当前差异：业务代码 24 个文件，+755 / -165 行；测试代码 11 个文件，+603 / -74 行。共 35 个 Go 文件（含 5 个未跟踪新文件），排除说明文档；统计包含未跟踪文件，不仅使用 git diff --stat。本轮验收修订触及其中 3 个业务文件和 3 个测试文件，净增加业务 11 行、测试 66 行。新增公开 HTTP/WS 接口 0、用户配置项 0、数据库迁移 0、顶层转发/重试/后台 worker 路径 0；在既有执行路径接入 Claude/Gemini 两套协议内适配。

新增 6 个内部包导出函数：ThinkingCapabilitiesForModel、NormalizeThinking、ThinkingBudgetForEffort、DerivedThinkingBudget、ResponsesToAnthropicIntermediateRequest、WithAppliedReasoningEffortPolicy；能力结构 ThinkingCapabilities 1 个。不是新增远程接口。

## 验证限制

- 未执行真实 Anthropic/AWS/Vertex/Gemini/BPS 请求，也未获取错误 #25177/#25176 的生产完整请求或部署 revision。不能据本地合成测试承诺所有线上 400 消失。
- 未修改存储/仓储/数据库 schema，未启动生产数据库、未运行真实供应商端到端验收；本轮使用 -tags=unit 的完整后端回归，不宣称等价于独立 integration/e2e 标签测试。
- 首轮因工具缺失/网络超时未做独立 lint；用户安装后，本轮实际调用 /tmp/gobin/golangci-lint v2.14.0 补跑。全仓仍有 48 条基线问题，新增问题为 0，不能表述为全仓 lint 无问题。
- 前端未改，未运行无关前端构建。
- docs/* 在现有忽略范围内；未修改 .gitignore，也未强制暂存文档。后续如获准提交，需显式审查并纳入本轮研究、计划与验收文档。

## 供应商规则来源

核对日期 2026-10-13。模型能力是已知型号的明确事实，不用“版本更大”猜测未来模型。

- Anthropic effort：https://platform.claude.com/docs/en/build-with-claude/effort
- Anthropic thinking 排错矩阵：https://platform.claude.com/docs/en/build-with-claude/thinking-troubleshooting
- Sonnet 5.5：https://platform.claude.com/docs/zh-CN/models/sonnet-5-5/whats-new-sonnet-5-5
- Fable 5.1：https://platform.claude.com/docs/en/models/fable-5-1/whats-new-fable-5-1
- AWS adaptive thinking：https://docs.aws.amazon.com/bedrock/latest/userguide/claude-messages-adaptive-thinking.html
- AWS InvokeModel 请求及 effort beta：https://docs.aws.amazon.com/bedrock/latest/userguide/model-parameters-anthropic-claude-messages-request-response.html
- Gemini generateContent thinking：https://ai.google.dev/gemini-api/docs/generate-content/thinking

Gemini 使用 generateContent 文档，不以新版 Interactions API 的字段矩阵代替；Effort→budget 是已有网关语义的协议近似，未新增“预算折算为计费档位”的规则。

## 用户验收修订后的当前验证

检查均针对上述未提交候选，由主代理实际执行；未调用独立子代理。再次复核本轮 6 个修改文件及原生 Forward、最终构建、策略上限和 beta/body 对齐调用点。定向测试先发现历史夹具缺少必要 beta，补齐夹具后通过；保留生产过滤规则。

| 检查 | 当前候选结果 |
| --- | --- |
| 新增及相关定向回归 | claude、apicompat、service 三包通过，退出码 0。 |
| 完整后端 unit | go test -tags=unit -json ./... 退出码 0；24,734 条通过事件（含子测试），12,356 个顶层测试，60 个测试包通过、52 个包无测试，18 项按既有条件跳过；新增回归无跳过。 |
| 定向 race | 退出码 0；209 条通过事件（含子测试），47 个顶层测试，4 个包通过，无跳过或 race 告警。 |
| go vet -tags=unit ./... | 退出码 0，无输出。 |
| CGO_ENABLED=0 go build -trimpath ./cmd/server | 退出码 0；产物 /tmp/sub2api-new-api-acceptance-server-20261013。 |
| golangci-lint v2.14.0 全仓 | 退出码 1；48 条：depguard 1、errcheck 28、staticcheck 11、unused 8；全部位于本次 35 个 Go 文件之外，报告的源码行均在 HEAD 中存在。 |
| golangci-lint --new-from-rev=HEAD | 退出码 0；0 issues，QF1003 已消除。 |
| gofmt / git diff --check | 通过。 |

本轮日志统一使用 /tmp/sub2api-new-api-acceptance- 前缀和 -20261013 后缀：focused.log、unit.jsonl/.stderr、race.jsonl/.stderr、vet.log、build.log、lint.log/.json、lint-new.log/.json、lint-status.json、source.json、verification.json（实际文件名将类型放在日期之前，如 /tmp/sub2api-new-api-acceptance-unit-20261013.jsonl）。unit/race 的 stderr 均为空，vet/build 无诊断输出。

race 命令沿用首轮筛选，并加入 ResponsesManualThinkingIntentAndLimit、Sonnet55ResponsesThinkingAndSampling。lint 命令为 /tmp/gobin/golangci-lint run --timeout=5m --output.json.path=<本轮日志路径> ./...；第二次增加 --new-from-rev=HEAD；使用仓库 .golangci.yml 默认标签。

当前 35 个 Go 文件 SHA-256：c9b7cf94ae51657a0a087fab1c40d9caad6639cbcf9ec9580fa279c598e644b9。算法与首轮相同。HEAD 保持 c575e66799775e649e08e45053034f90a8fcd96a，分支 codex/new-api-adoption-20261013，暂存区为空；未提交、推送、发版、部署或迁移。研究、计划与报告仍在 docs/* 忽略范围内。
