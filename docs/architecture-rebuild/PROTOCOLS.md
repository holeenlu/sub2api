# 协议、模式与交付契约

状态：开发交接设计，未实施新架构。核查日期：2026-10-25。源码基线：`750d4a16c7adaf558cf2c6ce7101975602832d7b`，包含已完成的 Cloud parser 重构；源码行为优先于旧分析、旧测试名称和历史说明。已纳入ce58原生Responses与WS多路复用交付，具体行为见第4.2节。

本文承接 [架构总纲](ARCHITECTURE.md)，仅细化协议侧事实、目标合同与验收。共享能力适用于 KDAN、TapModels、Tokensavy，不改变 [根规则](../../AGENTS.md)、[交付政策](../CHANGE_DELIVERY.md) 或品牌归属。本次仅写本文，未修改业务/测试代码，未运行构建、业务测试或真实客户端/上游验证。

## 1. 规范与证据状态

本文对齐 [REQUIREMENTS](REQUIREMENTS.md)、[CONTRACTS 0.1](CONTRACTS.md)、[DECISIONS](DECISIONS.md)、[COLLABORATION](COLLABORATION.md)、[work-items.json](work-items.json) 与 [WORK_PACKAGES](WORK_PACKAGES.md)。REQ、CT、WP 的定义/写集/依赖由这些文件维护；本文不另建规则、类型或任务状态。目标包与 CT 签名均为**设计，尚未落地**；源码链接指本工作树文件或第4.2节明确标注的ce58 Git对象，不指虚构目标包。

- **已实现**：本基线有可达源码路径，非本次运行通过；**明确拒绝/未注册**：有拒绝分支或没有对应公开路由；**待核实**：证据不足，不等于支持或不支持。
- 所有 PC 验收状态统一为 **未运行（not_run）/ 外部证据未采集**。既有 fixture/历史实施记录仅作材料来源，不转录为本轮通过证据。
- 分开判断客户端协议、入口 transport、平台、凭据、mode、上游 transport、操作能力；矩阵不是笛卡尔积。`openai_ws_mode=true` 仅证明客户端 WS 入口。

**能力差异仍由 mode adapter 负责，核心只共享安全生命周期**：授权与撤销、attempt/预算、发送与交付、租约、归属、取消、幂等账务。具体 Excel 图片阶段、Cloud RPC、原生恢复不进入核心平台分支。

## 2. 现状：入口与能力矩阵

### 2.1 入口全集及别名

公开网关入口来源为 [RegisterGatewayRoutes](../../backend/internal/server/routes/gateway.go)。下表区分路由存在与实际能力；管理、用户、供应商控制台由 CT-11 负责，本表不把管理探测接口当作客户端生成入口。

| 入口（含方法） | 客户端协议 / transport | 当前分派与限制 | 状态 |
|---|---|---|---|
| `POST /v1/responses`、`/responses`、`/backend-api/codex/responses` | Responses / HTTP JSON 或 SSE | OpenAI 网关族进入 `OpenAIGatewayHandler.Responses`，其他进入 `GatewayHandler.Responses`；再按账号与模式决定 | 已注册并实现；逐组合见后表 |
| 上述 Responses 前缀的 `POST /*subpath` | Responses 子操作 / HTTP | `guardResponsesSubpath` → `IsForwardableOpenAIResponsesRequestPath`；路径结构闭集校验。`input_tokens` 单独计数，`compact` 有独立语义 | 已实现路径护栏；任意子路径的上游语义 **待核实**，不能因通配注册认定支持 |
| 上述三处 Responses 的 `GET` | Responses 帧 / 客户端 WS | `ResponsesWebSocket`；不是获取 response 资源，非 Upgrade 返回 426；Composite WS 仅允许解析到 OpenAI/Grok | 已实现；不提供全平台 WS |
| `POST /v1/messages` | Anthropic Messages / HTTP JSON、SSE | 按平台进入 `Messages`；原生透传或转换，见平台表 | 已实现；没有根 `/messages` 别名 |
| `POST /v1/messages/count_tokens`、`/messages/count_tokens` | Anthropic 计数 / HTTP JSON | OpenAI 桥接计数、Grok 本地估算、其他沿原计数链；不是所有模式的真实上游 token meter | 已实现，估算与测量须区别 |
| `POST /v1/chat/completions`、`/chat/completions` | Chat Completions / HTTP JSON、SSE | 原生 CC 或协议转换；有 Responses-shaped body 兼容 | 已实现；不等于旧 `/completions` |
| `POST /v1/systemone` | System One / HTTP JSON | `GatewayHandler.SystemOne` 与 TypeSafe 语义，使用纯文本体积限制 | 已实现专用入口 |
| `GET /v1/models`、`/models`；`GET /v1/models/:model`、`/models/:model` | OpenAI 模型列表/单项 / HTTP JSON | 带 `client_version` 的列表切为 Codex manifest；单项不切 manifest。普通模型清单不证明每个工具/传输可用 | 已实现 |
| `GET /backend-api/codex/models` | Codex manifest / HTTP JSON | OpenAI 平台使用 `OpenAIGatewayHandler.CodexModels`，其他经生成目录；BPS/Cloud 覆盖能力受其矩阵约束 | 已实现 |
| `GET /v1/usage`、`/v1/sub2api/billing` | 网关用量/Key 账务查询 / HTTP JSON | 已有读接口；后者注册在图片准入等生成中间件之前，仍有 Key 鉴权 | 已实现，不能当作结算权威事件 |
| `POST /v1/embeddings`、`/embeddings` | Embeddings / HTTP JSON | 路由仅 OpenAI，账号须具备 embeddings 能力且为 API Key | 已实现；OAuth/BPS/Cloud 非此能力 |
| `POST /v1/alpha/search`、`/alpha/search`、`/backend-api/codex/alpha/search` | Codex 搜索 / HTTP | OAuth/API Key 分别构造上游地址，按 endpoint capability 选取 | 已实现本地路径；第三方端点可用性待核实 |
| `POST /v1/images/generations`、`/v1/images/edits` 及去掉 `/v1` 的别名 | Images / JSON 或 multipart，输出依端点支持 JSON/SSE | OpenAI → `Images`；Grok → `GrokImages`；其他平台路由拒绝 | 已实现，模式限制见媒体表 |
| 上述 Images 路径加 `/async`；`GET /v1/images/tasks/:task_id` 及根别名 | 网关异步图片任务 / 提交 202 + 轮询 JSON | `AsyncImageHandler.Submit/Get`；直接检查 Key 分组为 OpenAI/Grok；拒绝流式提交，功能开关控制新建 | 已实现；Composite 即使同步路由可解析，也不能推定异步可用 |
| `/v1/images/batches`：POST/GET；GET `/models`、`/:id`、`/:id/items`、`/:id/items/:custom_id/content`、`/:id/download`；POST `/:id/cancel`；DELETE `/:id`、`/:id/outputs` | 图片批处理 / HTTP JSON、结果下载 | `BatchImageHandler`；Gemini API 与 Vertex 两个 provider，非通用 OpenAI Batch | 已实现；没有对应根路径别名 |
| `POST /v1/videos`、`/videos`、`{prefix}/videos/generations`；POST `.../edits`、`.../extensions` | Grok 视频 / HTTP 异步任务 | 创建 generations 允许 Grok/Composite 路径；edit/extension 要求解析后 Grok | 已实现，有账号媒体资格门槛 |
| GET `{prefix}/videos/:request_id`及`.../:request_id/content`、`.../videos/{generations,edits,extensions}/:request_id`及其`/content`（`prefix` 为 `/v1` 或空） | 视频状态/二进制结果 / HTTP | 无 model 的查询交 Grok handler；允许 Grok/Composite，须找回原任务归属 | 已实现，不重新发起生成 |
| POST `{prefix}/contents/generations/tasks`；GET/DELETE `.../:task_id`（`prefix` 为 `/api/v3`、`/v3`、`/v1`、空） | Seedance task / HTTP | OpenAI/Composite 分组入口；账号要求 OpenAI API Key、自定义 base URL、显式 seedance capability | 已实现专用适配；不把 Seedance 当 OpenAI 官方 mode |
| POST `/v1/tts`、`/v1/stt`、`/v1/custom-voices`；GET `/v1/custom-voices`、`/:voice_id`、`/:voice_id/audio`；PATCH/DELETE `/:voice_id`；均有根别名 | xAI TTS/STT/音色管理 / HTTP | `GrokVoice`，实际 Key 分组须 Grok；音色查询、修改、试听须保持归属 | 已实现，不能推广为 `/audio/*` |
| GET `/v1/realtime`、`/realtime` | Grok 实时语音 / 双向 WS | `GrokRealtime`，Grok Key、用户并发与账号资格；不是 Responses WS | 已实现独立生命周期 |
| POST `/v1/live`、`/backend-api/codex/realtime/calls`；GET `/v1/live/:call_id`、`/backend-api/codex/:call_id` | Codex Live：建会话/SDP + sideband WS | `Live/LiveSideband`，OpenAI/Composite Key、普通原生 OAuth 与 Live 开关；call owner 绑定 | 已实现；BPS/Cloud 非 Live mode |
| POST `/v1/web_search`、`/v1/x_search` 及根别名 | Grok 搜索 / HTTP | Grok 平台专用 `WebSearch/XSearch` | 已实现；不等于 Cloud hosted search |
| GET `/v1beta/models`、`/models/:model`；POST `/v1beta/models/*modelAction` | Gemini 原生 / HTTP JSON、SSE | `GeminiV1BetaListModels/GetModel/Models`；Gemini/允许的 Antigravity 路由，action 仍由具体 handler/forwarder校验 | 已实现，未知 action 不默认支持 |
| POST/GET `/v1beta/cachedContents`；GET/PATCH/DELETE `/:cacheID` | Gemini 显式缓存 / HTTP | 网关保存资源与账号绑定；list/get 是本地读记录 | 已实现，生命周期非普通生成 |
| GET `/antigravity/models`；`/antigravity/v1` 下 POST `/messages`、`/messages/count_tokens`、GET `/models`、`/usage`；`/antigravity/v1beta` 下 GET `/models`、`/models/:model`、POST `/models/*modelAction` | Anthropic/Gemini 外观 / HTTP JSON、SSE | `ForcePlatform`，只用 Antigravity；不能与普通分组任意混调 | 已实现专用路由 |
| GET/HEAD `/api/bps-images/:token` | 受控图片 relay / HTTP 二进制 | `ExcelBPSImage`，不走普通 API Key 链；依 relay token、有效期、图片存储与出口保护 | 已实现；token 必须作为敏感能力引用保护 |
| `/v1/files`、`/files`、通用 `/v1/batches`、`/v1/audio/{speech,transcriptions,translations}`、旧 `/v1/completions`、通用 response GET/DELETE 资源接口 | OpenAI 标准资源协议 | 在 `RegisterGatewayRoutes` 未注册对应公共接口。内部 BPS attachments、Gemini batch files/GCS、媒体 content 不构成公开 Files API | **未注册**；不能用内部文件调用声称对外支持 |

### 2.2 生成平台 × 凭据 × mode × transport

平台清单以 [domain.platformList](../../backend/internal/domain/platforms.go) 为准；“Native”在下表表示该供应商既有直连/转换路径，并不表示所有行使用 OpenAI 原生 Responses。`Composite` 是解析策略，不能作为上游平台或额外 mode。

| 平台 / 凭据 | 可达客户端协议与入口 transport | mode → 上游协议 / transport | 现状与明确边界 |
|---|---|---|---|
| OpenAI / 普通 OAuth、适用 OAuth-like 身份 | Responses HTTP JSON/SSE | Native → Codex Responses HTTP/SSE；账号/配置允许时 HTTP 入口也可转原生 WS | 已实现。compact、passthrough、force-HTTP 等可强制 HTTP；特定身份不能自动获得 Cloud/Live/Excel 授权 |
| OpenAI / API Key | Responses、CC、Messages HTTP JSON/SSE | Native → 端点能力/协议配置决定 Responses、CC 或 Anthropic；Responses 不支持时存在 CC 适配 | 已实现条件分派；HTTP API Key relay 保持 HTTP，不因 WS 可配置而改变该入口合同 |
| OpenAI / 被允许的 OAuth 或 API Key | Responses WS | Native `ctx_pool` → 原生 Responses WS；`passthrough` → v2 relay；`http_bridge` → HTTP/SSE | 三条已实现，受全局/账号 mode、并发、force-HTTP 等约束；`off` 不是第四条转发路径 |
| OpenAI / 原生 OAuth、API Key | Messages、CC HTTP JSON/SSE | Native → 专用兼容 forwarder | 已实现；兼容 forwarder 自己的传输语义不能写成“自动经过与 Responses 完全相同的路径” |
| OpenAI / 独立 Excel-only 或双授权 Excel grant | Responses HTTP JSON/SSE、WS；Messages/CC HTTP JSON/SSE | BPS → HTTP SSE；非流式聚合、WS 桥、Messages/CC 编解码 | 已实现，须 BPS 总开关、账号模型范围和 Excel grant。Excel-only **不能**退回 Native；普通 Codex token 不是 Excel grant |
| OpenAI / 普通原生 OAuth | Responses HTTP JSON/SSE、WS | Cloud → `codex-cloud-backend` WS JSON-RPC，输出投影回 Responses | 已实现；`IsOpenAICloudEligible` 排除供应商、shadow、agent identity、PAT、非原生 client_id。已配置但失效应拒绝，不静默退 BPS/Native |
| OpenAI / Cloud configured | Messages、CC、legacy `/responses/compact`、Images/Embeddings/Live 等 | 无独立 Cloud 对应承载 | 不列为 Cloud 已支持；endpoint capability 仅 Responses。原 Responses 内的 `compaction_trigger` 是已实现例外；未来扩展须单独合同 |
| Anthropic / OAuth、API Key、支持的 service account | Messages HTTP/SSE；CC、Responses 经兼容转换 | Native → Anthropic Messages；Vertex/Bedrock 等走其已有认证/封装 | 已实现入口与转换。各凭据、模型、thinking/cache/beta 组合须复用原专项验证，未做全组合外部验收；无 Responses WS 保证 |
| Gemini / API Key、OAuth、service account | Gemini v1beta；Messages/CC HTTP JSON/SSE | Native → Gemini/Vertex 的已有路径 | 已实现相应桥接。通用 Responses 路由虽存在，当前 `GatewayHandler.Responses` 未看到独立 Gemini 分支，**待核实，不宣称支持**；无 Gemini WS 路由 |
| Antigravity / 其既有凭据 | Messages、CC、Responses、Gemini v1beta HTTP JSON/SSE | Native → Antigravity gateway；受可用模型/混合调度/专用 ForcePlatform 约束 | 已实现专用转换；不推广 BPS/Cloud/Responses WS |
| Grok / OAuth、API Key | Responses、Messages、CC HTTP JSON/SSE；Responses WS 桥 | Native → Grok Responses/CC 及工具转换；WS 强制 HTTP 桥 | 已实现；Grok OAuth CC 只在 `grokChatResponsesBridgeEligibility` 允许时走 Responses，否则 raw CC；不能称原生上游 WS |
| Kimi / API Key，payg/coding | Responses、Messages、CC HTTP JSON/SSE | Native/adaptive 按入站选择；profile 有 Responses/Anthropic/CC 三项 | 本地已实现，实际模型支持/自定义地址待真实上游核实 |
| 智谱 GLM / API Key，payg/coding | 同上三个客户端外观 | Native profile 只有 Anthropic/CC，Responses 外观转换 | 已实现转换；profile **未登记原生 Responses**，不能把外观当直通 |
| DeepSeek / API Key，payg | 同上 | Native profile 三协议；Responses 上游路径覆盖为 `/responses` | 已实现，工具 namespace/图片限制按该适配器；实际模型组合待核实 |
| MiniMax / API Key，payg/coding | 同上 | Native profile 三协议；套餐共享端点，凭据决定套餐 | 已实现本地分派；不从名称假定所有模型支持每个工具 |
| OpenCode / API Key，Go/Zen | 同上 | Native 按模型规则选择 Responses/Anthropic/CC | 已实现；模型禁用及规则优先级保留，不做入站协议的任意全覆盖 |
| Command Code / API Key | 同上 | Native 按模型规则与模型目录 `supported_endpoints` 选择三协议 | 已实现本地路径；目录/规则缺失时的具体候选仍依现有解析器 |
| Cline / API Key | 同上三个客户端外观 | Native profile 只有 CC，其余转换 | 已实现；ClinePass/按量额度语义不是新的 protocol mode |
| TypeSafe / 既有账号 | System One HTTP；其平台登记在 Anthropic gateway family | Native → `GatewayHandler.SystemOne` | 已实现专用语义；`rejectSystemOneOnlyPlatform` 可拒绝普通 CC/Responses，平台 family 不等于 Messages 全兼容 |
| Composite / 用户 Key 分组 | 按 model、入口及 scope 解析到上述候选 | 不是 provider；不增加任何 transport/mode | 已实现解析；OAuth 歧义拒绝；WS、异步图片、语音等仍各有额外限制 |

模式证据：[OpenAIGatewayService.Forward/forwardOpenAIResponses](../../backend/internal/service/openai_gateway_forward.go)、[ForwardAsAnthropic](../../backend/internal/service/openai_gateway_messages.go)、[ForwardAsChatCompletions](../../backend/internal/service/openai_gateway_chat_completions.go)、[forwardOpenAIGrantCompatibility](../../backend/internal/service/openai_excel_bps_compat.go)、[ProviderProfile/providerProfiles](../../backend/internal/service/provider_profile.go)、[resolveUpstreamProtocolFor](../../backend/internal/service/model_protocol_catalog.go)、[SupportsOpenAIEndpointCapability](../../backend/internal/service/account.go)、[IsOpenAICloudEligible/Configured](../../backend/internal/service/account_openai_cloud.go)、[GatewayHandler.Responses](../../backend/internal/handler/gateway_handler_responses.go)、[GatewayHandler.ChatCompletions](../../backend/internal/handler/gateway_handler_chat_completions.go)。BPS、Cloud 只存在于明确支持的 OpenAI 账号路径；ce58多流增量见第4.2节，原matrix不代表完整多流外验；其他平台的这两列均无已实现依据。

### 2.3 媒体、实时与资源能力

| 操作 | 平台/模式与上游 transport | 当前事实 | 迁移必须保留 / 未核实项 |
|---|---|---|---|
| OpenAI 同步 Images | 原生 Images 或 Responses 生图路径；BPS 有独立 Images 适配 | [Images 转换](../../backend/internal/service/openai_excel_bps_images.go) 与 [图片策略](../../backend/internal/service/openai_excel_bps_image_policy.go) 实现支持形状；不等同任意 multipart 都可走 BPS | gpt-image 路由、数量/格式/编辑条件由现有适配判定；Cloud hosted image 不是此接口；复杂 BPS edit 外部行为待核实 |
| Grok 同步图 / 异步视频 | Grok media HTTP | [GrokMediaGenerationEligibility](../../backend/internal/service/account.go)、[ForwardGrokMedia](../../backend/internal/service/grok_media.go)；显式 forbidden/free、有效 override、未观测探测分别处理 | 任务所属账号/用户/Key；轮询、下载不得当新生成；OAuth 探测不充分不等于肯定有媒体能力 |
| 网关异步图片 | OpenAI/Grok 同步执行包在任务生命周期内 | [AsyncImageHandler.Submit/Get](../../backend/internal/handler/image_task_handler.go)、[ImageTaskService](../../backend/internal/service/image_task.go)；先返回 202，后台执行，Redis 保存结果；关闭新建不阻断已有结果查询 | 断开提交 HTTP 不取消已受理作业；worker 接手重验权限与租约；不能承诺现状具备尚未证明的持久队列恢复 |
| 图片批处理 | Gemini API Key → Gemini batch/files；Gemini service account → Vertex/GCS/batch | [NewDefaultBatchImageProviderRegistry](../../backend/internal/service/batch_image_provider.go) 仅注册 Gemini、Vertex；`SupportsAccount` 在各 provider 再校验 | submit/get/cancel/result/cleanup 分开；批次与 item 幂等、文件归属、取消后晚到输出/账务；不是音频 batch 或 OpenAI `/batches` |
| Seedance | OpenAI 平台自定义 API Key endpoint → HTTP task | [SeedanceTasks](../../backend/internal/handler/seedance.go)、[ForwardSeedance](../../backend/internal/service/seedance.go) | POST、GET、DELETE 不共用生成重试；上游任务 ID 不能替代网关 owner |
| TTS/STT/音色 | Grok HTTP | [GrokVoice](../../backend/internal/handler/grok_audio.go)、[grok_audio service](../../backend/internal/service/grok_audio.go) | 字符/音频时长等用量按现有计价语义，不能强转 token；用户并发及音色归属必须保留 |
| 实时语音 | Grok 双向 WS | `GrokRealtime` 与 `grokRealtimeBillingResult` | session 音频输入、输出、终止和断线用量；没有证据支持通用 OpenAI audio/realtime 的全部协议 |
| Codex Live | 普通原生 OAuth；HTTP SDP 建立 + sideband WS | [Live/LiveSideband](../../backend/internal/handler/openai_live.go)、[Live service](../../backend/internal/service/openai_live.go) | attestation、call owner、并发/到期与结算，不与 Responses turn 混用 |
| Files/附件/缓存 | BPS attachment HTTP、Gemini batch file/GCS、Gemini cachedContents、签名图 relay | 各自绑定资源，有内部接口；公开通用 Files 未注册 | 二进制、JSONL、task refs 与业务消息分离；不可从 BPS `file_id` 推导 Cloud 能读取；未知资源先拒绝或走各自显式错误合同 |
| 音频批处理/通用视频批处理 | 无对应公共 batch route/provider 证据 | **未核实且不列为已支持**；现有 batch 是图片批次 | 若后续要新增，单独定义 CT-09 操作与授权，不借文本 mode 自动放开 |

## 3. 目标：协议与执行交接

以 [CT-01～11](CONTRACTS.md) 为唯一接口语义来源，不在本文复制状态机。主要消费面：CT-04 的 `Decode/Convert/Encode/DeliverySink.Write` 只做纯协议与交付事实；CT-03 的 `Prepare/Open/Next/Cancel/Close` 执行一次受 `SendPermit` 约束的尝试；CT-02 唯一决定恢复；CT-05/08 负责实时资格、租约、快照/撤销；CT-06 保管续接；CT-07 处理用量；媒体/实时用 CT-09，宿主/观测用 CT-10，配置展示用 CT-11。均为设计签名。

`Prepare` 不得生成；BPS 图片压缩/工具纠错、Cloud 新 turn/目录交接中会继续生成的动作必须显式登记并取得相应 permit，不能藏在“准备”或 codec 内。原始 wire 不可变，出站为 attempt 副本；保留 buffer 的异步消费者遵守 CT-01 生命周期。

| 事件 | 必须区分的事实与目标行为 |
|---|---|
| 排队、授权探测、附件上传 | 不自动等于生成已发送；上传是有 owner/清理的控制副作用。已完成 compaction 则已有业务及费用，后续失败不能全算 NotSent。 |
| 写入开始后返回错误 | 无响应不证明 NotSent；发送证据不足为 Ambiguous。无业务输出也不能任意换号重放。 |
| SSE 注释、WS ping/pong、桥接 keepalive | 不算业务提交、token 或 usage，不续模型无进展计时；HTTP 头可能已锁定，后续仍须正确流内错误。 |
| response.created、空 item、role 等 metadata | 本地暂存时可替换；实际发出的、客户端依赖的 response/tool 身份可能已不可撤回，不能只看文本为空。 |
| 文本、reasoning 摘要、refusal、工具、图/音频 | `DeliveryReceipt` 以真实 Write/flush 记录；部分写失败保守视为可能已交付。已提交不替换成另一份生成。 |
| 取消、写失败、断线、EOF | 停新 attempt，有界 cancel/drain；EOF/HTTP 200 不造成功。Unknown/Partial/Final 与发送/交付独立，迟到费用可幂等入账。 |

现有锚点：[GuardSSEWrites](../../backend/internal/service/gateway_stream_writer.go)、[openAIFirstOutputStage.CommitTo](../../backend/internal/service/openai_first_output_timeout.go)、[excelBPSResponseWriter](../../backend/internal/service/openai_ws_excel_bps_bridge.go)。同协议尽量保留原文；跨协议转换须说明工具 ID/namespace/schema、raw code/cmd/custom、图片顺序、reasoning/refusal、终态与 usage 的映射/损失。网关不执行客户端 shell/OfficeJS；hosted 工具另计事实。

## 4. Native：当前传输与续接合同

| 路径 | 当前源码事实 | 迁移必须保留 |
|---|---|---|
| HTTP JSON/SSE | [Forward/forwardOpenAIResponses](../../backend/internal/service/openai_gateway_forward.go)；OAuth 可按配置走上游 WS，HTTP API Key relay 保留 HTTP；compact/passthrough 可强制 HTTP | ingress 与 upstream transport 分别记录；兼容 forwarder 不能假称经过同一完整主链 |
| ctx_pool | [ProxyResponsesWebSocketFromClient](../../backend/internal/service/openai_ws_forwarder_ingress.go)、[openAIWSConnLease](../../backend/internal/service/openai_ws_pool.go)；原生上游 WS、租约与保留历史 | 首轮、generate:false warmup、增量工具、store:false、previous 与重连分别判断；强归属非普通 sticky |
| passthrough | [v2 adapter](../../backend/internal/service/openai_ws_v2_passthrough_adapter.go)、[relay](../../backend/internal/service/openai_ws_v2/passthrough_relay.go) | 独立 relay 语义，不是 ctx_pool 别名；不能绕过每轮准入/取消/归属 |
| http_bridge | [proxyOpenAIWSHTTPBridgeTurn/openAIWSToolCallReplayCollector](../../backend/internal/service/openai_ws_http_bridge.go)；WS 请求→HTTP/SSE→完整 WS 事件 | 完整历史与孤立工具结果分别恢复；tools 缺省继承/空数组清空、模型切换重连、取消后下一轮不串状态 |

[defaultOpenAIWSProtocolResolver.Resolve/openAIWSRequestAllowsHTTP](../../backend/internal/service/openai_ws_protocol_resolver.go) 以平台、凭据、开关、并发、mode 决定实际路径；`shared/dedicated` 才是旧 ctx_pool 别名。warmup、agent_task、conversation、显式 previous 等不能通过删除字段强转新 HTTP 生成。`openAIWSFailureBeforeSend` 的具体分类须与实际写证据核对，非任意网络错误都未发送。

SSE 共享 [sse.ScanLines](../../backend/internal/pkg/sse/sse.go) 的 CR/LF/CRLF/BOM；事件解码仍有模式差异：原生有限多 JSON 修复、BPS 严格单 JSON、Cloud 上游 JSON-RPC 不机械合并。资源限额参考 [GATEWAY_PROTOCOL_LIMITS](../GATEWAY_PROTOCOL_LIMITS.md) 当前章节及源码；字节/token/图像/历史条数不可替代，上限不等于真实上游成功容量。

### 4.1 Native不确定发送恢复的既有例外

不能把Ambiguous统一禁恢复，也不能改标NotSent。当前 [openAIWSUncertainExecutionReplaySafe/openAIWSExecutionReplayBlocker](../../backend/internal/service/openai_ws_forwarder_payload.go) 判断副作用：未交付的function/custom尚未被客户端执行；只读hosted搜索可在接受重复搜索用量风险下恢复；未知工具、MCP/computer/code_interpreter/image_generation/shell等及不可解析历史阻断。namespace/发现目录也检查。另一道 `OpenAIResponsesAccountReplayAllowed` 决定**是否可跨账号**：previous/conversation/prompt、密文、file/vector/container等绑定不能迁移；同账号完整历史恢复可保留允许密文。两道判定不可合并。

[WS v2](../../backend/internal/service/openai_ws_forwarder_v2.go) 的 `canReplay` 还要求非synthesizedContinuation、无下游提交/上游输出/已观测token或图、客户端未断；[HTTP bridge](../../backend/internal/service/openai_ws_http_bridge.go) 在首轮或完整replayProven、无提交/断线且副作用安全时才将超时/断流交恢复；[gateway_turn_recovery](../../backend/internal/service/gateway_turn_recovery.go) 继续限制失败原因/预算。其他平台 `NativeUncertainReplaySafe` 是不同谓词，不能直接替换上述OpenAI判定。目标CT-02按这些**模式事实与既有例外**统一决策，不抹掉有效恢复，也不宣称上游exactly-once。

### 4.2 ce58只读增量：严格兼容、多流、模型metadata

增量范围：已用git show/git diff核对 `3e1f5275300450683f7cd434663e07f807c6aa82` → `ce58a16ee67305575a704cef304b9c2b1959cb75`，后者父为 `37067e8e340e9440d173a92b4faa05e8ce58368f`。当前资料基线已统一为ce58。**本小节是ce58已有源码事实，非目标包设计、非本次测试通过**。新增文件与链接已在当前ce58检出核实；本轮没有重写这些运行代码。

| 增量 / 源码符号 | 当前合同及迁移约束 |
|---|---|
| [strictResponsesError/detectOpenAIResponsesCompatibilityLoss](../../backend/internal/service/openai_responses_strict.go) | `gateway.strict_responses_compatibility`默认false，环境变量`CODEX_STRICT_RESPONSES_COMPAT`，请求头`X-Codex2API-Strict-Compatibility: true`可开启，头false不能关全局true。按已选Native Codex及HTTP/WS实际转换检测损失；HTTP400或WS error/status400、code unsupported_feature、details.fields。非通用schema验证/授权，不套到Relay/BPS/Cloud；BPS转Native后适用。 |
| [normalizeOpenAIResponsesNativeAliases](../../backend/internal/service/openai_responses_ingress_compat.go) | 原生reasoning/text.format/tools/tool_choice优先，alias只补缺失；显式null/无效留给原校验，messages保留previous，input/max_output_tokens原生优先，UseNumber保大整数。标准Relay不套Codex受限schema清洗。 |
| [RunOpenAIWSMultiplexer](../../backend/internal/service/openai_ws_multiplex.go) | 单物理reader、串行物理writes；stream_id分流，同流FIFO、不同流并行；response.cancel/interrupt的显式response_id必须匹配该流活动轮。response_id不是跳过stream/owner授权的任意路由键。append/steer返回400 unsupported_feature。 |
| multiplexer容量常量 | 每连接**16活动轮、32命名流、每流16等待create、全连接64等待create**；命名数计已用名字。活动/等待/重建请求合计受client_read_limit_bytes约束，控制帧≤16KiB，取消不被同流create队列挡住。不是新的可调“默认单轮”开关。已收齐帧等待活动名额属准入，不计缺帧读取超时。 |
| [responsesWebSocketStream/refreshOpenAIWSAuthorization](../../backend/internal/handler/openai_gateway_handler.go)、[openAIWSLaneWriter](../../backend/internal/handler/openai_gateway_ws_writer.go) | 每流独立Gin/request timing/trace/输出历史/恢复状态，逐新流与后续轮复核权限、各轮用户/账号容量与账务不放宽。lane writer只保Gin头/状态，业务输出经downstream.Write，不能从其Size证明物理交付。命名流局部失败不伤其他流；缺省流保留旧关闭语义；共享鉴权失效/物理断开/连接租约失效终止连接。 |
| prepareMultiplexTurn /跨流续链 | 仅同连接已成功交付completed/done的完整历史、相同Key/group/account/完整凭据代际可fork；等待终态写成功后提交历史。新流用自己的上游连接重建，不借活socket/opaque turn；失败/取消/部分/未知输出不可作恢复依据。最多256响应，历史总预算≤client_read_limit_bytes，单流collector≤该预算1/16；超限仍可输出但不承诺恢复。 |
| [resolveOpenAIWSExecutionLane](../../backend/internal/service/openai_ws_execution_scope.go)、[WriteFrame](../../backend/internal/service/openai_ws_client.go) | 亲和加入connection+named stream；stream_id仅最终出站边界移除，保留本地取消/历史/身份；无跨连接新增共享缓存。每operation预算不能提升成整连接一个预算。 |
| [codexCapabilityScope/restrictCodexExperimentalToolsManifest](../../backend/internal/service/openai_codex_model_metadata.go)、[UpstreamModelMetadataSnapshot](../../backend/internal/service/upstream_models.go) | experimental_supported_tools有界字符串数组，按可用账号/映射取交集、绑定identity/provider scope、真实撤回清旧声明、重算ETag；同OAuth身份正常刷新不误失效。Cloud ceiling为[]。**未知时不广告实验工具**不等于CT-08禁止未知模型或新增产品准入。 |

此增量以stream为传输隔离单位、每create为operation/turn，不能将物理WS连接当单operation；每流仍运行原mode adapter，16活动槽不替代用户/账号租约。新增本地回归定位见PC-39～41，本次未运行/未采集。

## 5. BPS：当前专属合同与迁移边界

| 能力 | 当前源码锚点与事实 | 目标约束 |
|---|---|---|
| 独立 Excel grant | [forwardExcelBPS/handleExcelBPSUnauthorized](../../backend/internal/service/openai_excel_bps.go)：BPS 用独立 Excel token/client/身份；双授权 Excel 401 不隔离原生主授权，Excel-only 保留单授权处理 | 不通过改 client_id/header/Cookie 伪造授权；复用 CT-01 GrantRef 与 CT-05/08 校验，不恢复自动登录 Worker |
| mode/模型 | [excelBPSHasNativeFallback/excelBPSNativeFallbackAccount](../../backend/internal/service/openai_excel_bps_ratelimit.go)；总开关、模型范围、grant、供应商 mode 共同决定；[模型目录](../../backend/internal/service/openai_excel_bps_models_manifest.go) 使用 Excel access 能力 | Excel-only 不能 Native fallback；真实原生授权、可迁移输入、预算全部满足才有候选边；目录失败不拿另一 mode 补成功 |
| 首轮/工具续接 | [Prepare](../../backend/internal/service/basispoints/request.go)、[catalog](../../backend/internal/service/basispoints/catalog.go)、[catalog_items](../../backend/internal/service/basispoints/catalog_items.go)、[tools](../../backend/internal/service/basispoints/tools.go)、[history_messages](../../backend/internal/service/basispoints/history_messages.go) | function/custom/namespace/additional_tools 与 tool_search 配对；顶层 tools、空数组、缺省继承区别；整批完整验证后发布，保留 call ID、schema、原 code/cmd/custom 字节；无 call_id 独立输入不伪造调用 |
| 回放/纠错 | [persistent_state](../../backend/internal/service/basispoints/persistent_state.go)、[tool_repair](../../backend/internal/service/basispoints/tool_repair.go)、[unknown_tool_repair](../../backend/internal/service/basispoints/unknown_tool_repair.go) | 缓存按可信作用域；有界纠错不得改变业务操作、先执行部分工具或已输出后再生成；涉及生成均走 CT-02/03 attempt 与原费用链 |
| 图片 preflight/上传 | [NativeImages](../../backend/internal/service/basispoints/attachments.go)、[uploadExcelBPSAttachment](../../backend/internal/service/openai_excel_bps_attachments.go)；完整校验后消息图上传为 file_id，工具截图可内联，响应体先关闭；禁止重定向 | 上传非生成但有资源副作用；先校验完整图片/工具配对，不删图降清晰度；保留可信出口与取消传播 |
| 图片计时/容量 | 当前单图120秒、阶段180秒；每请求并行4/进程32；[pauseOpenAITurnFirstOutput](../../backend/internal/service/openai_first_output_timeout.go) 排除有界上传阶段 | attachment_timeout 不错归模型首字；阶段等待仍有界，换号不重置共享预算；历史/续接图片窗口分别校验 |
| ID 缓存/附件429 | 可信会话按账号/Key/线程/凭据命名空间/工作区隔离；[excelBPSAttachmentUpload](../../backend/internal/service/openai_excel_bps_attachments.go) 在条件成立时整请求共享一次同账号429补试，保留成功图片 | TTL 不是上游归属保证；transient 不跨请求复用；自带不透明 file_id 不可凭可见图修复；取消停止补试 |
| file ownership/压缩 | [canRetryExcelBPSAttachmentOwnership](../../backend/internal/service/openai_excel_bps_transport.go)、[forwardExcelBPSAttempt](../../backend/internal/service/openai_excel_bps.go)；原图齐全、引用受控、无输出/usage/不确定工具副作用、未完成 compaction 等条件下同账号重传一次 | 作废本请求已用ID；新上传错误也不能都归缓存；不换号/罚模型。compaction 已发生费用，后续上传失败不能填零 |
| 输出与取消 | [excelBPSResponseWriter](../../backend/internal/service/openai_ws_excel_bps_bridge.go)、[isExcelBPSClientCancellation](../../backend/internal/service/openai_excel_bps_cancellation.go)；JSON聚合/SSE/WS桥，共用工具验证及账务 | keepalive 非业务；provider 子context取消不等于客户端断线；HTTP200/不完整流不造成功；Inputs图、hosted生图、独立Images不是一个能力 |

**显式403恢复**：[disableExcelBPSOn403/moveExcelBPSOn403](../../backend/internal/service/openai_excel_bps.go) 先区分模型权限、文件归属和账号授权，按配置及条件标记处理；[IsExcelBPS403RecoveryPending/probeExcelBPS403Recovery](../../backend/internal/service/openai_excel_bps_recovery.go) 只处理有效停用标记、开启自动停用/恢复且当前合格的账号。[ClaimExcelBPS403Probe/RestoreExcelBPSAfter403](../../backend/internal/repository/account_repo_excel_bps_recovery.go) 先持久化claim，再用真实BPS nonce生成和完整终态验证，最后条件恢复。probe 是独立生成，非keepalive、不回放用户失败请求；并发关闭/换grant/改分组不能被旧probe覆盖。上游401/403经 `excelBPSUpstreamAuthClientStatus` 映射客户端503，避免误报网关Key失效。目标保留功能，把显式probe纳入CT-03/10，不能扩展为总调度或自动登录。

来源与需求引用 [BPS REQUIREMENTS](../bps/REQUIREMENTS.md)、[integration-state](../bps/integration-state.json)；历史条款冲突见第7节。

## 6. Cloud：当前 parser、capability plan 与 owner

现行版本 `2026-10-25.parser.1`，依据 [parser实施记录](../cloud-parser/implementation-20261025.md) 及本基线源码；历史运行结论不算本轮验收。以下都是**现有实现**，迁入目标包的工作仍待开发。

### 6.1 解析和计划的数据流

[PreserveCloudResponsesInput/SyncCloudReasoningEffort](../../backend/internal/service/openai_cloud_ingress.go) 保存 bootstrap/compact 前输入并同步公共策略 → `cloudIngress/validateCloudIngress` 用 UseNumber 和唯一键/深度/节点检查 → [PrepareCloudContinuation](../../backend/internal/service/openai_cloud_gateway.go) 对真实 response/call 引用做 `projectCloudIngressSource` 轻量归属投影 → 确定账号/模型/公共策略后 [compileCloudProtocolRequest](../../backend/internal/service/openai_cloud_protocol.go) 编译同一 `cloudProtocolRequest` → owner/actor。普通文本含 `resp_cloud_` 不是授权。

`source` 保留声明，`body/history/input/outputs` 为有效视图；容器复制、字符串只读共享。 [cloudCapabilities/cloudScopeFields/cloudContinuationProjection/cloudRequestProtocol](../../backend/internal/service/openai_cloud_capabilities.go) 统一规范化、目录、拒绝/诊断与过滤后的 CallIDs/PairedCalls/HistoryRefs/ToolReply。同进程缓存计划，继承后编译结构；跨实例只传公共策略后的JSON及owner/billing事实，不传可信plan，远端本进程重新解码校验。WS逐轮重置plan/binding/root。不得重新套整个Native OAuth出站转换器或新建平行parser。

### 6.2 当前能力差异

| 项目 | 已实现/明确限制 |
|---|---|
| 提示/执行字段 | 无RPC载体的采样、缓存、遥测提示按矩阵drop并记录；接纳不等于生效。错放的执行、工具、密文/schema字段不能当提示删除。 |
| legacy/Lite | 复用alias、角色/文本、空图、schema、压缩标记规则；additional_tools进入目录，标准tools优先，纯增量按顺序合并；不做Native目标Lite反向编码。目录仍 `use_responses_lite=false`。 |
| function/namespace/custom | 映射thread/start动态工具并恢复公开身份；function strict:true仍拒绝；custom grammar仅描述提示，不保证lark/regex约束输出。 |
| tool_choice/发现 | auto/none；parallel=false保留串行约束；client tool_search有映射与受限目录换线程，server执行拒绝；目录仍 `supports_search_tool=false`。 |
| text/reasoning/include | text或strict json_schema→outputSchema；json_object/非strict拒绝。effort/summary映射，reasoning.context无逐轮RPC控制；有效include参与继承/摘要，未知include可drop。 |
| store/tier/background | ephemeral线程；store:true、非默认service_tier等无对应语义拒绝；owner异步运行不等于支持公开background操作。 |
| 图片 | [cloudValidateInlineImage](../../backend/internal/service/openai_cloud_protocol.go) 接内联base64，保留MIME/原字节和auto/high/original；拒绝remote URL/file_id/low。**当前pending工具结果original拒绝**，完整配对的导入历史经inject_items可保留；不套BPS位图格式名单。 |
| 迁移历史 | 明文agent内容降低、不提权；纯密文agent无可读正文按当前策略丢弃；configuration_update不成为当前设置。无密文reasoning保留公开历史但不注入；客户端item ID仅在注入边界处理，call_id/owner不改写。 |
| 压缩/密文/hosted | 原Responses compaction_trigger可用，legacy compact不等价；不透明历史先保留，明确推理前拒绝才两级约减。 [hosted](../../backend/internal/service/openai_cloud_hosted.go) 只支持已映射搜索/图片，非任意shell或独立Images API。 |
| 资源/观测 | 已移除256工具/8192历史独立门槛；pending工具、RPC/队列/结果/retained字节仍受限。diagnostics仅schema路径/动作/矩阵ID及省略量，无正文/源码/密文/token。 |

### 6.3 续接、owner 与费用

| 现有锚点 | 合同及目标迁移约束 |
|---|---|
| [OpenAICloudOwner/Binding/SessionRecord](../../backend/internal/service/openai_cloud_session_store.go) | owner含user/key/group/account/grant generation/model/settings digest，binding关联session/root/thread/turn/instance。OwnerToken给活socket写入fencing；Redis不搬运socket。CT-06/10保留。 |
| [validateCloudSessionOwner](../../backend/internal/service/openai_cloud_gateway.go) | 沿用原生OAuth权威，复核grant/settings；wham/usage仅准入，非token meter。不能凭此宣称所有Key撤销检查点已覆盖；联验CT-05/08。 |
| [cloudRPC](../../backend/internal/service/openai_cloud_transport.go) | 一个reader分离回复与通知/server request；item/tool/call RPC id不同于公开call_id及本地递增id，Reply原样使用；不自动重连重放。 |
| [resolveProtocol/handleCommand/splitToolContinuation](../../backend/internal/service/openai_cloud_session.go) | user/key/group查ref再核owner；孤立过期结果拒绝，完整配对旧历史可新建线程。第一个RPC回复前校验整批结果、发布状态、turn、图片、历史/settings和指纹。 |
| 同上：轮中排队消息 | 已支持工具回传夹带符合规则的user/developer/system消息，先确认thread/inject_items再释放工具；任意不含结果的新用户轮仍不能抢占。不能恢复旧的一律拒绝轮中消息逻辑。 |
| [refreshToolCatalog](../../backend/internal/service/openai_cloud_client_tools.go) | 仅工具相关变化、全部pending结果齐、hosted完成、可信interrupt与usage后归档旧thread，同owner/同root建新thread注入；旧工具不再执行。中断/用量不确定则unknown，不换号重放。 |
| handleCommand /设置与root | 工具续接继承有效设置；独立新轮用新root/snapshot，outputSchema不跨轮继承，include参与摘要；完整历史与现线程须匹配。instructions继承与无状态Responses有差异，应保留说明。 |
| [billing](../../backend/internal/service/openai_cloud_billing.go)、[recovery](../../backend/internal/service/openai_cloud_recovery.go)、[archive](../../backend/internal/repository/usage_billing_cloud_archive.go) | 确认interrupted/failed结束且有实测usage时按既有单根幂等结算并标lower_bound，非完整Final；未确认结束即使有水位仍unknown，不自动收费/补零。迟到冲突事实依既有幂等/归档，不另开补差链；ACK/释放遵守原合同，旧owner不能新发业务。CT-07是唯一账务用例。 |

## 7. 兼容来源与退出条件

| 保留项 / 来源 | 目标所有者 | 删除或收敛条件 |
|---|---|---|
| 公开root/Codex别名：RegisterGatewayRoutes | ingress/CT-04 | 仍有调用方即产品合同；确认客户端迁移并处于授权范围才删除。 |
| shared/dedicated旧mode：WS resolver | CT-08 | 已存配置迁移、旧版本读写/回退成立才去别名；不扩大新别名。 |
| legacy alias、Responses-shaped CC、Lite/迁移历史：当前codec/Cloud plan | CT-04+各mode | 有实际版本/数据契约才保留；新plan替代后同次删重复解析/原字节查ref，不删有效客户端能力。 |
| Native previous/store:false恢复 | Native/CT-06 | 只删除被替代的重复路径；完整历史、孤立工具及硬归属分别验收。 |
| BPS工具封装/缓存： [source-inventory](../bps/source-inventory.json) | BPS codec/CT-06 | 等价替代成立、旧读取方/活跃会话结束后清理；缓存TTL不替代真实归属。 |
| Excel grant、403恢复、图片阶段 | CT-05+BPS/CT-10 | 当前明确保留功能；只删重复调度/授权外壳，不恢复退役登录体系。 |
| Cloud parser版本/摘要/命令 | Cloud/CT-06/08 | 旧pending会话排空或明确过期；不能用新摘要盲续旧RPC，账务不随会话TTL删除。 |
| grammar提示、instructions继承、图片差异 | Cloud adapter | 外部合同改变且有对应证据才能收敛；不得静默宣称原生等价。 |
| 三mode密文约减： [openAIReduceHistoryCiphertext](../../backend/internal/service/openai_encrypted_history_recovery.go) | 纯约减+adapter推理前证明+CT-02预算 | 仅现有两级：去不可读reasoning/回放密文但保留明文；再用提示代替不可读compaction。禁止第三阶段、假装解密、有输出/usage或不确定副作用后盲目重放。 |

**材料冲突已识别**：BPS REQUIREMENTS较早段落称密文拒绝后一律不自动删，现源码记录2026-10-24用户决定后的两级恢复；图片缓存/阈值也有历史更新。以本基线源码及日期明确的当前章节为准，不改其他作者文件。数据处置另核读取方及授权；代码清理不等于删除旧数据。

## 8. 协议契约验收场景

全部PC状态：**未运行（not_run）/ 外部证据未采集**。已有测试仅是复用落点，不保证覆盖每行全部断言；按独立风险补差异。单测/替身、真Redis/PG、多实例、真客户端+替身、真上游、线上验收分别记录。第9节说明真实报文缺口，不从测试名推断实测。

| 场景 / 契约 | 输入→必须观察的结果 | 现有fixture/符号（未运行） |
|---|---|---|
| PC-01 / CT-01、04、08 | 方法/别名、错平台、Composite、危险subpath→鉴权/限制齐全，未注册接口不误生成。 | [gateway_test](../../backend/internal/server/routes/gateway_test.go)：`TestGatewayRoutesResponsesSubpathRejectsNonConformingSubpaths` |
| PC-02 / CT-04 | 完整JSON/WS、扩展/重复键/大整数/工具图片→保真或明确map/drop/reject，不执行客户端载荷。 | [Cloud parser fixtures](../../backend/internal/service/openai_cloud_parser_plan_test.go)：`TestCloudParserPlanNeverDropsMisplacedExecutionFields` |
| PC-03 / CT-02、03、04、07 | Native JSON/SSE首轮、failed/EOF/仅usage→不从HTTP200或usage造成功，保留费用。 | [gateway_http_stream_contract_test](../../backend/internal/service/gateway_http_stream_contract_test.go)：`TestHTTPStreamContractRawUsageIsNotProofOfCompletion` |
| PC-04 / CT-02、05、06 | ctx_pool warmup/store:false/previous/工具重连→强归属、完整历史与孤立结果分别处理。 | [WS ingress session tests](../../backend/internal/service/openai_ws_forwarder_ingress_session_test.go)：`TestOpenAIGatewayService_ProxyResponsesWebSocketFromClient_StoreDisabledFunctionCallOutputAutoAttachPreviousResponseID` |
| PC-05 / CT-03、04、06 | http_bridge/passthrough、大请求/模式切换、tools空/缺省→正确transport/身份/继承，受限切换重连。 | [bridge isolation](../../backend/internal/service/openai_ws_http_bridge_isolation_test.go)：`TestOpenAIGatewayService_ProxyResponsesWebSocketFromClient_HTTPBridgeModeRelaysHTTPStream` |
| PC-06 / CT-02、03、04、10 | keepalive/prelude/空role/首内容/部分写失败→三轴独立，不以无文本否认不可撤回身份交付。 | [HTTP stream tests](../../backend/internal/service/gateway_http_stream_contract_test.go)：`TestHTTPBoundaryRawFirstTokenMustExcludeRoleMetadata` |
| PC-07 / CT-02、05、07 | 排队取消/response.cancel/断线后下一轮→停新attempt、有界drain/释放，不串取消状态、不漏真实费用。 | [WS tests](../../backend/internal/service/openai_ws_forwarder_ingress_session_test.go)：`TestOpenAIWSIngressCancellationAndStaging` |
| PC-08 / CT-02、03 | 确定未发/写后不确定→保留Native副作用安全与跨账号归属两道判定；不一律禁恢复或换号，不伪造NotSent。 | [retry payload](../../backend/internal/service/openai_ws_forwarder_retry_payload_test.go)：`TestOpenAIWSUncertainReplayChecksNamespaceChildren`、`TestOpenAIResponsesAccountReplayPreservesOwnership` |
| PC-09 / CT-01、03、08 | Excel-only/双授权、401/代际漂移→不伪造grant、不伤主授权，Excel-only不走Native。 | [BPS compatibility](../../backend/internal/service/openai_excel_bps_compat_test.go)：`TestExcelBPSCompatibilityRoutingAndFailures` |
| PC-10 / CT-04、06 | BPS目录首轮/增量/乱序工具回传→整批验证，call ID/schema/raw字节保真，无ID外部输入不造调用。 | [BPS repair](../../backend/internal/service/openai_excel_bps_repair_test.go)：`TestExcelBPSToolSearchHistoryAcrossTransports` |
| PC-11 / CT-02、03、04、07 | BPS坏封装/未知工具/纠错取消→有界且不改原操作，已可见不再生成，真实费用累计。 | [BPS repair](../../backend/internal/service/openai_excel_bps_repair_test.go)：`TestExcelBPSMixedToolCorrectionKeepsOriginalOperation` |
| PC-12 / CT-03、05、07 | BPS多图/上传超时/压缩后失败→先preflight；上传不耗模型首字，压缩费用保留。 | [attachment phase](../../backend/internal/service/openai_excel_bps_context_attachment_phase_test.go)：`TestExcelBPSAttachmentPhaseDoesNotConsumeFirstOutputBudget` |
| PC-13 / CT-03、06 | BPS缓存/新ID归属错、自带file_id、429→只按既有条件同账号一次补试，TTL不证明归属。 | ：`TestExcelBPSFileOwnershipFailure` |
| PC-14 / CT-01、03、08、10 | 403停用/并发恢复/不完整probe→持久claim+条件恢复、完整nonce终态；不重放用户请求。 | [recovery](../../backend/internal/service/openai_excel_bps_recovery_test.go)：`TestExcelBPS403RecoveryRestoresOnlyAfterSuccessfulClaimAndProbe` |
| PC-15 / CT-02、04、07 | BPS流内失败/保活/WS取消/provider超时→具体错误/用量/归因准确，保活不掩盖无进展。 | [phase tests](../../backend/internal/service/openai_excel_bps_context_attachment_phase_test.go)：`TestExcelBPSHTTP200ContextLengthFailureAcrossTransports` |
| PC-16 / CT-04、08 | Cloud三种入口、本地/远端owner→同一plan、每进程解码，过滤后归属一致，跨实例重新校验。 | [capabilities tests](../../backend/internal/service/openai_cloud_capabilities_test.go)：`TestCloudProtocolIsDecodedOncePerOwnerProcess` |
| PC-17 / CT-04、08、10 | Cloud能力每项、strict/grammar/Lite/旧数量边界→目录/诊断同源，接纳不代表生效，字节保护保留。 | [parser plan](../../backend/internal/service/openai_cloud_parser_plan_test.go)：`TestCloudCapabilityMatrixFixtures` |
| PC-18 / CT-03、10 | Cloud乱序RPC/迟到回复/半开/队列满→单reader分派，原RPC id回复，无自动重连重放。 | [transport tests](../../backend/internal/service/openai_cloud_transport_test.go)：`TestCloudRPCOutOfOrderRepliesAndServerRequests` |
| PC-19 / CT-04、06 | Cloud批量回传/旧turn/冲突及排队消息→整批校验，先inject确认再释放RPC；保留现有轮中消息能力。 | [migration](../../backend/internal/service/openai_cloud_migration_test.go)：`TestCloudMigratedHistoryToolRepliesCarryQueuedMessages` |
| PC-20 / CT-02、03、06、07 | Cloud发现新目录/pending未齐/未确认中断→合法同owner同root交接，旧工具不重做，费用不丢。 | [migration](../../backend/internal/service/openai_cloud_migration_test.go)：`TestCloudMigratedToolDiscoveryKeepsHistoryAndBilling` |
| PC-21 / CT-06、07、08 | Cloud设置继承/新轮reasoning/include/schema→新root与快照、schema不误继承，历史须匹配。 | [client contract](../../backend/internal/service/openai_cloud_client_contract_test.go)：`TestCloudCompletedTurnChangesReasoningAndDoesNotInheritOutputSchema` |
| PC-22 / CT-01、05、06 | 跨user/key/group、lease/epoch、取消队列→拒绝串归属、旧owner fenced，未知enqueue非未发证明。 | [Redis store tests](../../backend/internal/repository/gateway_cache_cloud_test.go)：`TestCloudStoreReferenceOwnershipAndCollisionAtomicity` |
| PC-23 / CT-02、07、10 | Cloud提前结束/断线/归档故障→确认实测lower_bound单根结算；未确认即使有水位仍unknown，不收费/填零/新开补差链。 | [lower bound](../../backend/internal/service/openai_cloud_lower_bound_test.go)：`TestCloudConfirmedEarlyEndSettlesMeasuredUsageAsLowerBound`、`TestCloudUnconfirmedEndKeepsMeasuredUsageUnknown`；[billing](../../backend/internal/service/openai_cloud_billing_test.go)：`TestCloudBillingArchiveOnlyReleasesAfterDurablePreservation` |
| PC-24 / CT-02、04、06 | 迁移历史/孤立结果/密文/compaction→完整配对可重建、空历史不inject；推理前明确拒绝才两级约减。 | [foreign history](../../backend/internal/service/openai_cloud_foreign_history_test.go)：`TestCloudUnusableHistoryIsBadRequestBeforeInference` |
| PC-25 / CT-03、04、07 | Cloud输入/历史/当前工具original与hosted→分清图片限制，hosted事实同根，不能保持精度则明确拒绝。 | [images](../../backend/internal/service/openai_cloud_images_test.go)：`TestCloudImagePayloadsAreForwardedWithoutLocalCodecPolicy` |
| PC-26 / CT-01、08、11 | models单项/manifest/pinned/Composite→形态权限正确，模式能力覆盖不污染别的候选。 | [catalog history](../../backend/internal/service/openai_cloud_catalog_history_test.go)：`TestCloudCompositeCatalogDoesNotApplyBPSRestrictions` |
| PC-27 / CT-03、04、08 | 每provider实际model/endpoint与SystemOne→按模型/按入站不混，工具/usage/终态和转换损失独立验收。 | [protocol routing](../../backend/internal/service/upstream_protocol_routing_test.go)：`TestResolveUpstreamProtocol` |
| PC-28 / CT-03、06、07、09 | 异步图202后断线/超时/关开关/错owner→受理与完成分开，旧结果可查，轮询不重扣。 | [async handler](../../backend/internal/handler/image_task_handler_test.go)：`TestAsyncImageHandlerSubmitAndPoll` |
| PC-29 / CT-05、07、09 | Gemini/Vertex batch、缺失/重复item、cancel/崩溃→item对齐、冻结价格、文件归属、幂等结算。 | [processor](../../backend/internal/service/batch_image_processor_test.go)：`TestBatchImageResultIndexer_ReconcilesMissingAndUnknownCustomIDs` |
| PC-30 / CT-03、06、07、09 | Grok视频/Seedance查询下载删除、Range/恶意URL→不重生成，任务归属与出口凭据正确。 | [media content](../../backend/internal/service/grok_media_content_test.go)：`TestForwardGrokMediaContentUsesUpstreamCredentialAndStreamsRange` |
| PC-31 / CT-01、05、07、09 | TTS/STT/音色/实时二进制与迟到音频→每引用验owner、用户并发在上游前、费用按实测单位。 | [grok audio](../../backend/internal/service/grok_audio_test.go)：`TestSecurityCustomVoiceOwnerAllReferences` |
| PC-32 / CT-01、05、06、07、09 | Live SDP/attestation/sideband/到期→call绑定、租约终止、幂等结算，非Responses文本终态。 | [Live lifecycle](../../backend/internal/service/openai_live_lifecycle_test.go)：`TestGetLiveCallForIdentityRejectsMismatchedCaller` |
| PC-33 / CT-01、04、06、09 | 公共files/audio/batches负例，内部附件/cache/relay→未注册不可用，资源独立鉴权，不跨mode解引用。 | [gateway routes](../../backend/internal/server/routes/gateway.go) |
| PC-34 / CT-03、04、05、10 | 体积/解压/SSE拼帧/背压/Cloud队列→限额不混、取消可达、超限不丢usage或造成功。 | [SSE](../../backend/internal/pkg/sse/sse_test.go)、[HTTP流](../../backend/internal/service/gateway_http_stream_contract_test.go)：`TestHTTPStreamContractGeminiCompleteFrameParsing` |
| PC-35 / CT-01、02、05、06、08 | 排队/重试/工具/worker发送前撤销→一致性明确的权威读/epoch/有效快照复核，非每次SQL；统计缓存故障不全站拒绝。 | [owner校验](../../backend/internal/service/openai_cloud_gateway.go)：`validateCloudSessionOwner`；[运行时复核](../../backend/internal/service/openai_ws_account_sticky_test.go)；全链覆盖待补核 |
| PC-36 / CT-01、04、06、10 | 可信IP/恶意出口/伪owner/秘密日志→保留安全边界、诊断最小化，不扩大scope。 | [Cloud transport](../../backend/internal/service/openai_cloud_transport_test.go)：`TestCloudDialFixedHandshakeDoesNotExposeCredentials` |
| PC-37 / CT-02、04、06、08、10 | 切片后注册/调用/版本/缓存读取方→唯一副作用路径，旧实现删除或列真实兼容退出依据。 | [架构替换清单](ARCHITECTURE.md)与第7节；新架构尚无运行证据 |
| PC-38 / CT-04、08、11 | 三品牌共享输入配置→协议与安全一致、品牌差异有来源，相同有效输入复用证据。 | [平台](../../backend/internal/domain/platforms.go)：`platformList`；[profiles](../../backend/internal/service/provider_profile.go)：`providerProfiles` |
| PC-39 / CT-03、04、08 | 严格默认/全局true/头true或false、Native/Relay/BPS/Cloud、alias与大整数→只对实际Native有损约束拒绝，原生字段优先；HTTP/WS在发送前一致。 | [strict tests（ce58）](../../backend/internal/service/openai_responses_strict_test.go)：`TestResponsesStrictSelectedAccount`、`TestResponsesStrictHTTPAndWebSocketFailBeforeDispatch`；[alias tests](../../backend/internal/service/openai_responses_ingress_compat_test.go) |
| PC-40 / CT-02、03、04、05、06、07、10 | 两命名流并发/同流FIFO、逐轮权限容量/独立账务、response_id错配取消、16/32/16/64边界、取消越排队、局部/共享失败、跨流fork代际/终态写失败→不串状态不放宽授权；各mode组合分别验证，不能推定全部已外验。 | [multiplex tests（ce58）](../../backend/internal/service/openai_ws_multiplex_test.go)：`TestOpenAIWSMultiplexFIFOAndIndependentFailure`、`TestOpenAIWSMultiplexForkRequiresCommittedOwnedHistory`、`TestOpenAIWSMultiplexCancellationBypassesQueuedCreate`；[handler tests](../../backend/internal/handler/openai_gateway_handler_test.go)：`TestOpenAIResponsesWebSocketMultiplexIndependentBilling` |
| PC-41 / CT-04、08、11 | experimental_supported_tools交集、部分metadata、真实撤回、identity变化/正常刷新、Cloud上限/ETag→不沿用越权旧能力；保守广告不拒未知模型。 | [metadata tests（ce58）](../../backend/internal/service/openai_codex_model_metadata_test.go)：`TestExperimentalToolsIntersectionAndScope`、`TestExperimentalToolsPersistPartialMetadataAndLiveWithdrawal` |

## 9. 现有fixture来源与证据缺口

| 材料 | 文件来源标记与复用边界 |
|---|---|
| [0.159.3](../../backend/internal/service/testdata/cloud_codex_0_159_3.json) | 2026-10-24 CLI、loopback/dummy key；tools/http/ws_warmup/ws_create/scenarios。真实客户端形状脱敏快照，非真实上游回执。 |
| [0.164 desktop](../../backend/internal/service/testdata/cloud_codex_0_164_desktop.json)、[history](../../backend/internal/service/testdata/cloud_codex_0_164_history.json)、[compaction](../../backend/internal/service/testdata/cloud_codex_0_164_compaction.json) | 内置0.164.0-alpha.11.1、loopback与网关目录；压缩回答由接收器构造，正文/metadata替换，不证明线上端到端。 |
| [catalog_shapes](../../backend/internal/service/testdata/cloud_codex_0_164_catalog_shapes.json)、[catalog_history](../../backend/internal/service/testdata/cloud_codex_catalog_history.json) | 原目录Lite与Cloud覆盖standard形状，用于目录消费回归，非所有版本通用能力。 |
| [migrated_history_items](../../backend/internal/service/testdata/cloud_codex_0_164_migrated_history_items.json)、[additional_tools](../../backend/internal/service/testdata/cloud_codex_additional_tools.json) | 历史逐形状脱敏与configuration_update源码补充须分开；additional_tools有capture/requests。抽样不能拼称完整实测会话。 |
| [BPS tests](../../backend/internal/service/basispoints)、[路由golden](../../backend/internal/service/testdata/upstream_protocol_routing_main_golden.json) | 本地工具/图片/历史/失败及路由预期；Real/E2E名称不证明真实外部链。 |
| [cloudlocal](../../backend/internal/service/openai_cloud_local_client_test.go) | `TestCloudLocalCodexClientUsesAdvertisedContract`：实际客户端→生产入口→替身RPC，非真上游。 |
| [cloudlive](../../backend/internal/service/openai_cloud_live_integration_test.go)、[migration live](../../backend/internal/service/openai_cloud_migration_live_test.go) | `newCloudLiveHarness`连接真上游但路由/结算store是内存替身；不证明Redis/PG/生产收费。本次均未运行。 |

**仍需真实外部报文**：PC-02～15的Native/BPS首轮、工具、取消、附件与403链；PC-16～25的Cloud完整目录/回传/RPC/终态；PC-26～33的实际provider模型、媒体、batch文件、语音/Live；PC-34大包、PC-39～41新增严格/多流/metadata客户端消费。PC-22/23/29/35另需真实存储/多实例。未注册负例、纯转换/静态删除本地可证，不凭此宣传外部普遍兼容。

采集复用既有harness：记录客户端版本、目录广告、method/path/语义headers、完整首轮与工具回传、mode/两端transport、终态/usage、取消时序、脱敏范围及真客户端/真上游/真存储标记。未知字段不预先截断，凭据不入fixture。本次未采集任何新报文，不把用户示例作实测，不转录历史通过结论。

## 10. 工作包交接与只读复核

### 10.1 WP消费索引

实际依赖/写集/状态以 [work-items.json](work-items.json) 为准。WP-00冻结输入、WP-01落实合同后按依赖开发；WP-11～17等待WP-10原生完整切片。下列目标模块均为设计，现有业务源码仍为迁移输入。

| 工作包 | 本文交接面 / PC |
|---|---|
| WP-00、WP-01 | 基线/报文来源/CT-01～11版本；所有PC未运行/未采集 |
| WP-02 | 纯协议保真诊断：PC-01/02/16/17/34/39 |
| WP-03、WP-04、WP-05 | 授权、容量、owner：PC-04/07/09/13/22/35/36/40 |
| WP-06、WP-07、WP-22 | 账务、目录快照、host：PC-06/14/21/23/26/29/34/35/41 |
| WP-08、WP-09、WP-10 | 唯一执行与Native切片：PC-01～08/35/36/39/40 |
| WP-11、WP-12、WP-18 | BPS/Cloud与集成：PC-09～25、40/41中模式边界 |
| WP-13、WP-14、WP-15 | Anthropic/TypeSafe、Gemini/Antigravity、其他供给：PC-26/27/33 |
| WP-16、WP-17、WP-19 | 媒体/文件/batch、实时/语音及集成：PC-28～34 |
| WP-20、WP-21、WP-23 | 控制/前端/装配：PC-26/35/36/38/41，服务端能力权威 |
| WP-24、WP-25、WP-26 | 清理、真实验收、品牌交付：PC-37/38及前序风险；本轮不进入编码/发布 |

按 [COLLABORATION](COLLABORATION.md) / [HANDOFF模板](templates/HANDOFF.md) 交接；共享旧入口/Wire由集成者串行处理，不自行扩大写集。需求只引用 [REQUIREMENTS](REQUIREMENTS.md)：矩阵REQ-01/07/14，生命周期REQ-02～06/08/12/15，资源安全REQ-09/13，展示REQ-10/11，兼容来源清理REQ-16～18。

### 10.2 未确定事实

- Gemini分组通用Responses、任意Responses wildcard子操作、自定义provider/model/transport组合仍按第2节待核实；不填满支持矩阵。
- 全链撤销、真实Redis/PG/多实例、滚动版本/迟到usage/媒体恢复均未运行；已有Cloud历史实施记录不是本次通过。历史记录中的Native `BoundedPreludeAndLiveFlush/true` 失败仅作为复核线索，本轮未重跑，不能断言在当前源码仍失败或已修复。
- ce58的严格模式/多流/metadata已纳入资料基线，但本轮没有真实客户端或上游运行证据。

### 10.3 CONTRACTS只读交叉审查

CT-05权威读/撤销epoch/有效快照语义不等于每次SQL，统计缓存故障不全站拒绝；CT-07确认结束实测lower_bound与未确认unknown的区分，与已读源码未发现实质冲突。PC-23/35依此验收。

Native存在不确定发送下的有条件恢复，见第4节；CT-02/03允许按恢复依据决策，ADR-04是默认限制，不能解释为一律禁止。ce58的并发粒度是connection→stream→operation/turn；CT-01/02/09已有连接与operation分离，可承载但不能按整条连接持有唯一operation预算。Cloud确认后的同root目录换线程是受限续接，不等于任意替换已交付生成。

**已解决的封装歧义**：主集成者已在CT-03区分原始输入指纹与Prepare完成后的sealed wire指纹。BPS上传得到file_id后才封定最终出站正文，随后执行者复核/登记/授权Open；Open不可偷偷改正文，变更不重置总预算。该决定对应[forwardExcelBPSAttempt](../../backend/internal/service/openai_excel_bps.go)和[NativeImages](../../backend/internal/service/basispoints/attachments.go)，WP-01落地类型，WP-11/18验证实际行为。

本轮仅进行文档和源码静态核查，运行状态及统一交付结果见[READINESS](READINESS.md)。
