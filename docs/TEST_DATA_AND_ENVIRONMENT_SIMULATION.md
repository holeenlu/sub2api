# 测试数据与环境模拟框架设计

日期：2026-10-18（Asia/Shanghai），同日第 2 版：按仓库测试基建实测结果修订落点。状态：历史设计；后续测试平台已按用户要求撤销，不作为当前实施计划。配套文档：[CODEX_RESPONSES_TRANSPORT_OPTIMIZATION.md](CODEX_RESPONSES_TRANSPORT_OPTIMIZATION.md) 第 12 节。

## 1. 目标

给本系统自己灌数据、造环境、注故障，让以下几类验证不依赖真实付费请求和线上事故：

- **恢复与容错**：06-recovery.md 的每一条契约都要有可重放的触发场景，而不是等线上碰到。
- **计费与用量**：定价、倍率、订阅、配额边界、统计聚合、清理任务，需要大批量且分布可控的 usage 行。
- **前后端展示**：账单页、Ops 面板、图表分页与时区分桶，需要稳定可预期的数据集。
- **调度与并发**：账号池、粘性、限额、降级，需要可复现的并发压力而不是随机抽样。
- **传输与协议**：HTTP/SSE、原生 WS、passthrough、HTTP bridge 四条路径的事件序列和故障分支。
- **国际化与环境**：时区、语言、货币对日期分桶、配额重置、文案渲染的影响。

不做的事：这套框架的数据出口是本地接收器和本地数据库，不向任何供应商发送合成记录。它是测试替身，指向真实上游就不再是测试替身，结论也不成立。所以“向供应商发伪造遥测/活跃度”不在本设计内，不是因为难做，而是它会让这套工具失去测试意义。

## 2. 设计时盘点与后续核实

| 需要的能力 | 现状 |
| --- | --- |
| 声明式场景 | 近乎无。最接近的是 `internal/handler/openai_gateway_handler_test.go:1954` 的 `openAIResponsesWSUsageLogCase`，约 35 个开关含多个故障旋钮，但只服务 4 个文件 |
| 可复现的随机性 | 无统一 seed。生产 15+ 文件用未播种的 `math/rand/v2`；全仓只有两处确定性种子，一处是 `gateway_tool_rewrite.go:75` 的 fnv64a，一处是某个测试内的常量 |
| 可注入时钟 | 零散。全仓只有 `internal/securityaudit/prompt_types.go:257` 一个 Clock 接口；通行做法是结构体上的非导出 `now func() time.Time` 字段，已有约 14 处；唯一导出的是 `openai_ws_v2/passthrough_relay.go:90`。网关与计费热路径直接 `time.Now()` |
| 批量用量数据 | 无生成器，`internal/testutil` 里也没有 UsageLog 工厂。现有播种是三处一次性函数（`user_repo_sort_integration_test.go:13`、`api_key_repo_last_used_unit_test.go:63`、`usage_log_session_id_unit_test.go:15`） |
| 合成数据的标识与清理 | 无 synthetic 标记。`UsageCleanupFilters` 只支持时间、user/api_key/account/group、model、request_type、stream、billing_type |
| 离线重放生产逻辑 | 已有先例：`cmd/profit-preview` 读 JSON，复用线上 U/D 判定做五平台离线预演 |
| 组合爆炸的合成数据 | **已有成熟先例且应作为模板**：`internal/service/upstream_protocol_routing_matrix_test.go` 把协议 × 模式 × 账号类型 × 探测 × 地址形态 × 规则 × ingress × 模型叉乘成 63,600 个用例，用 `httpUpstreamRecorder` 驱动真实 `OpenAIGatewayService`，结果去重存 golden，并用 `case_keys_sha256` 摘要防漂移，由 Python recorder 对着固定 git 版本重录 |
| 共享夹具包 | **已存在且几乎没人用**：`internal/testutil` 导出 `NewTestUser` / `NewTestAccount` / `NewTestAPIKey` / `NewTestGroup` / `NewRedisGatewayCache` / `NewGinTestContext` / `NewRealHTTPUpstream`，是可导入的正式包，全仓却只有 2 个文件导入它 |
| 真实库的集成环境 | 已有：`internal/repository/integration_harness_test.go` 用 testcontainers 起 PG 18.1 + Redis 8.4，跑 `ApplyMigrations`，强制 UTC，Redis 按测试命名空间隔离，非 CI 环境缺少 Docker 时跳过，CI 缺少 Docker 时失败 |
| CI 实际覆盖 | 2026-10-19 核实：仅手动触发，包含全后端 unit、定向 race、定向 repository integration；没有全量 integration 或 e2e job |
| 参数化请求上下文 | 已有先例：`internal/service/codex_probe_template.jsonl` 带 `{{TIMEZONE}}` / `{{CURRENT_DATE}}` / `{{MODEL}}` / `{{MODELTRACE_PROMPT}}` 占位符与白名单校验 |
| 周期性真实检测 | 已有 ScheduledTestService / ScheduledTestRunnerService / scheduled_test_plans / scheduled_test_results，但发的是真实计费请求 |

当时的设计选择是收敛已有替身并补场景、时钟和批量数据；后续平台已撤销，下面保留设计细节供历史追溯，不表示重新启动这些批次。

## 3. 四层结构

```text
scenario.json  ──►  internal/testutil/*  ──►  被测系统
                      ├─ scenario  场景解析与确定性种子
                      ├─ upstream  上游替身（HTTP/SSE + WS，可注入故障）
                      ├─ client    客户端驱动（多轮、工具循环、历史增长）
                      └─ seed      数据灌注（直写 usage/ops 表，跳过网关）
```

**不新建并列的测试库，扩展已有的 `internal/testutil`。** 该包已经是可导入的正式共享夹具包，只是长期无人使用（2 个导入方）；在它旁边再造一个 `internal/testsim` 正是 AGENTS.md 要求避免的重复铺设。新增以子包形式挂在下面：

| 子包 | 职责 |
| --- | --- |
| `internal/testutil/scenario` | 场景 schema、解析、种子、run 目录、expect 比较器 |
| `internal/testutil/upstream` | HTTP/SSE 与 WS 替身、故障注入 |
| `internal/testutil/client` | 客户端画像与驱动 |
| `internal/testutil/seed` | 批量数据灌注 |

现有 `fixtures.go` 继续作为单对象工厂层，补一个缺失的 `NewTestUsageLog`。注意现有 `fixtures.go` / `httptest.go` / `stubs.go` 带 `//go:build unit`，而新子包要在 unit、integration、e2e 三种标签下都能用，因此新增文件不加构建标签。

禁止生产代码导入这些子包用已启用的 depguard 落实：`.golangci.yml` 里已有 `service-no-repository` 规则，照同一形态加一条即可，不新增工具链。CLI 放 `backend/cmd/testsim/`，沿用 `cmd/profit-preview` 的 `-input xxx.json` 形态。场景文件放 `backend/testdata/scenarios/`。

### 3.1 场景描述层

一个场景是一个 JSON 文件，声明环境、参与者、流量和故障。不写 Go 代码就能加一个用例。

```jsonc
{
  "schema_version": 1,
  "scenario_id": "ws-lease-expiry-midturn",
  "seed": 20261018,
  "clock": { "start": "2026-10-18T09:00:00+08:00", "mode": "virtual" },
  "env": { "timezone": "Asia/Shanghai", "locale": "zh", "currency": "CNY" },
  "fixtures": {
    "users": [{ "ref": "u1", "quota": "subscription:pro" }],
    "groups": [{ "ref": "g1", "platform": "openai", "rate_multiplier": 1.3 }],
    "accounts": [{ "ref": "a1", "group": "g1", "type": "oauth", "model_mapping": ["gpt-5.5-codex"] }]
  },
  "client": { "kind": "codex_cli", "version": "0.159.3", "transport": "ws_native" },
  "turns": [
    { "input": "full", "items": 12, "tools": 2 },
    { "input": "incremental", "items": 3, "tool_outputs": 2 }
  ],
  "faults": [
    { "at": "turn=2,after_first_token=false", "kind": "ws_lease_expiry" }
  ],
  "expect": { "recovery": "full_replay_once", "client_visible_error": null, "billed_turns": 2 }
}
```

`expect` 是断言而非描述：跑完后由比较器判定，不满足即失败。`clock.mode` 为 `virtual` 时所有时间戳来自场景时钟，`wall` 时用真实时间（仅压测用）。

这一层不是凭空设计：`internal/handler/openai_gateway_handler_test.go:1954` 的 `openAIResponsesWSUsageLogCase` 已经是一个约 35 个开关的声明式 WS 场景结构，其中 `firstTurnHTTPError`、`failFirstBeforeContent`、`bareErrorBeforeCompleted`、`disconnectAfterUsage`、`closeStatus` 等正是需要的故障旋钮，runner 在 `:2932`。做法是把它泛化成场景文件并让原有 4 个调用点迁移过来，而不是另起炉灶。

### 3.2 上游替身层

一个能同时扮演 OpenAI 与 Anthropic 的本地服务端，按场景产出字节级可控的事件序列。

- **传输**：HTTP/SSE、HTTP/JSON、WebSocket（含 permessage-deflate 协商与拒绝协商两种）。
- **事件脚本**：可以逐帧指定 `response.created` / `output_item.added` / 各类 `.delta` / `.done` / `usage` / `[DONE]`，也可以用模板按 token 数自动展开。
- **故障目录**（与 06-recovery.md 的契约一一对应）：

| 类别 | 可注入项 |
| --- | --- |
| 连接 | 拒绝连接、TLS 失败、WS 握手 4xx、拒绝 deflate、慢握手 |
| HTTP | 401/403/429/500/502/503、Retry-After、慢响应头、慢 body、连接中途断开 |
| SSE | 缺终止事件、事件中途截断、多行 data、流内 error、`[DONE]` 无 usage、重复 usage、CRLF/BOM |
| WS | 1000/1008/1012/1013 在各时点关闭、corrupt deflate 帧、`previous_response_not_found`、55 分钟 lease 到期、ping 超时 |
| 内容 | 工具调用无输出、call_id 不配对、reasoning 缺 encrypted_content、item_reference、超大帧、图片/文件项 |
| 时序 | 首字延迟、事件间隔、总时长超预算 |
| 账号 | 轮次中切号、凭据轮换、账号被禁用 |

每个故障点用 `at` 表达式定位（轮次、是否已出首字、是否已提交输出），因为恢复契约对这几个边界的判定不同。

**收敛目标（这是本层的主要工作量）**：51 个各自手写的 `…Upstream` 结构、约 54 个在测试里直接拼 `data: ` 帧的文件（其中只有 3 个用了 `http.Flusher`）、约 25 段几乎相同的 `coderws.Accept` 块（全部挤在 5736 行的 `openai_ws_forwarder_ingress_session_test.go` 里）、9 处复制粘贴的 `Hijack()` 断流。

**两个已是事实标准的替身要先提升可见性**：`httpUpstreamRecorder`（`openai_oauth_passthrough_test.go:30`，138 个文件在用）与 `openAIWSCaptureConn`（`openai_ws_forwarder_success_test.go:1952`，14 个文件在用）。两者都是 `internal/service` 包私有且定义在 `_test.go` 里，迁到 `internal/testutil/upstream` 需要先脱离测试文件；这一步单独提交，不与行为改动混在一起。

**必须继承的约束**：`internal/repository/inprocess_transport_test.go:57` 的 `newLocalTestServer` 带 `localListenerAvailable()` 跳过保护，受限环境下不监听端口就跳过；同文件 `:19` 的 `newInProcessTransport` 能在完全不开 socket 的情况下把 `http.HandlerFunc` 适配成 RoundTripper。新替身默认走不开 socket 的路径，需要真实监听时必须带同样的跳过保护，否则在沙箱里会直接失败。

### 3.3 客户端驱动层

按真实客户端的行为发请求，而不是发一个最小 body。

- **客户端画像**：`codex_cli`（会话级 WS 缓存、严格前缀增量、WS 失败后会话级回退 HTTP）、`claude_code`、第三方 OpenAI SDK、裸 curl。画像决定 header 形态、是否带 `previous_response_id`、历史增长方式。
- **历史增长**：按轮次累积 input items 与 output items，模拟工具循环把上下文推到压缩阈值，触发 compaction 与 window 切换。
- **负载档位**：单请求冒烟；N 路并发会话；20 轮工具往返长链；长历史；并发子代理；大小图片/文件。
- **客户端识别**：复用 `openai.IsCodexOfficialClientByHeaders(userAgent, originator)`，保证驱动发出的画像能被网关按既有逻辑识别，而不是另造一套判定。

### 3.4 数据灌注层

绕过网关直接写库，用于聚合、分页、清理、面板、配额这类不需要跑请求的验证。

- **生成维度**：时间分布（均匀/尖峰/跨时区边界/跨月）、模型组合、平台、request_type、stream/ws、缓存读写比例、图片与视频行、失败行与部分用量行、订阅与余额两种计费类型。
- **规模档位**：1e4 快速、1e6 面板与分页、1e7 清理任务与索引验证。
- **写入方式**：批量 COPY 或多值 INSERT，不逐行走业务层；但字段取值必须由业务层的构造函数产出，避免造出生产不可能出现的组合。
- **运行环境**：复用 `internal/repository/integration_harness_test.go` 的 testcontainers 环境（PG 18.1 + Redis 8.4、`ApplyMigrations`、强制 UTC、Redis 命名空间隔离、非 CI 缺 Docker 时跳过、CI 缺 Docker 时失败、`SUB2API_TEST_POSTGRES_IMAGE` 可换版本）。它目前定义在 `_test.go` 里因而不可被其他包导入，灌注器要用就得先把启动部分提到可导入位置，这是 S3 的前置条件。
- **配额校准数据**：Anthropic 与 Codex 的周配额换算常数已有记录，灌注器按这些常数生成用量，使配额面板的百分比可被独立验算。

## 4. 环境维度矩阵

“各种情况和环境”展开成可枚举的维度，场景文件每次取一个组合：

| 维度 | 取值 |
| --- | --- |
| 客户端 | codex_cli / claude_code / 第三方 SDK / curl |
| 上游传输 | HTTP-SSE / HTTP-JSON / WS 原生 / WS passthrough / HTTP bridge |
| 平台 | openai / anthropic / 其余已接入平台 |
| 鉴权 | OAuth / API key |
| 时区 | Asia/Shanghai、Asia/Singapore、UTC、America/Los_Angeles（跨日界与配额重置） |
| 语言 | en / zh / zh-TW（zh-TW 为生成物，渲染断言须用生成后的产物） |
| 货币与计费 | 余额 / 订阅；不同倍率与长上下文计费 |
| 规模 | 单请求 / 并发 / 长链 / 批量行 |
| 故障 | 第 3.2 节目录 |

维度叉乘不自己实现：复用 `upstream_protocol_routing_matrix_test.go` 的生成器形态（叉乘 → 驱动真实 service → 结构化观测 → 去重 golden → `case_keys_sha256` 防漂移 → 对固定版本重录）。那套已经在 63,600 个用例上跑通，差别只是把它的维度换成本节的维度。

注入点是实测的，不是假设的：

- **时区**：进程级来自配置（`config.go:105`，默认 Asia/Shanghai，`TZ` 环境变量优先，并注入 Postgres DSN）；请求级来自 **query 参数** `c.Query("timezone")`，不是 header，也不在用户记录里。调用点在 `usage_handler.go:198,331,791`、`admin/dashboard_handler.go:37`、`admin/affiliate_handler.go:292`。
- **语言**：来自 `Accept-Language` 请求头，由约 25 个 handler 手工传进 DTO 的 `.Localized(...)`。
- **货币**：只存在于支付层，没有用户或地区级的货币记录。

时区与语言在这里是被测系统自己的输入维度：验证日期分桶、配额重置边界、文案渲染。它不涉及对外声明用户位于何处。zh-TW 是生成物，任何文案断言必须跑在 `tools/zh-tw/gen-locale.mjs` 的产物上。

## 5. 确定性

失败必须能精确重放，否则故障注入没有意义。

- 单一种子：`seed` 决定所有随机量，用 `math/rand/v2` 的 PCG，不用全局 rand。
- 稳定 ID：所有生成的 request_id / session_id / call_id 由 `hash(scenario_id, seed, kind, index)` 派生，与生产里 `sha256("sub2api:codex-call-id:v1:" + id)` 同样是确定性映射，便于跨运行比对。
- 可注入时钟：**沿用仓库已有的约定，不引入第二套抽象**。现成做法是结构体上的非导出 `now func() time.Time` 字段（已有约 14 处，如 `subscription_service.go:60`、`channel_monitor_v2.go:381`、`dashboard_aggregation_repo.go:19`），按同一形态扩展到需要断言时间行为的路径：计费快照、lease 轮换、配额重置、清理任务边界。不全仓替换 `time.Now()`，每扩展一处说明为什么这条路径需要。
- 运行产物：每次运行输出一个 run 目录，含场景快照、seed、实际事件序列、断言结果，可直接作为缺陷附件重放。

## 6. 合成数据的标识、归属与清理

现有 `UsageCleanupFilters` 没有 synthetic 字段，而删 usage_logs 必须走 usage_cleanup_tasks。因此不新增字段，改用归属隔离：

- 灌注器只写入专用的 sim 租户：固定前缀 `sim-` 的 user、api_key、group、account，由灌注器创建并登记在 run 目录里。
- 清理时按 user_id / account_id / group_id 加时间范围建 usage_cleanup_tasks，命中集合与灌注集合完全一致，不碰真实数据。
- sim 租户不得与真实租户共用 group 或 account，避免统计口径互相污染。
- 生产库默认拒绝灌注：灌注器启动时校验目标库的标识（库名、一个显式的 sim 开关行），不匹配直接退出。
- 默认目标是 testcontainers 起的临时库，整库随容器销毁；连真实库是显式选项，只用于面板与清理的真机验证。

## 7. 隔离边界

- 替身监听 loopback 或受控测试网络；驱动的目的地白名单只含替身地址，不跟随跳到外部的重定向。
- 不持有任何供应商凭据；场景里的 token 字段是显式的假值。
- 合成事件带 `synthetic=true` 与 `scenario_id`，任何一条都能追溯到本地用例。
- 这套白名单是框架的定义，不提供一个开关把它切换成对外发送。

## 8. 复用与新增

优先复用，新增只补缺口：

| 已有 | 状态 | 如何用 |
| --- | --- | --- |
| `internal/testutil`（7 个导出助手，2 个导入方） | 可导入、几乎未用 | 新代码的落点。补 `NewTestUsageLog`，新增四个子包 |
| `internal/repository/integration_harness_test.go` | 全仓最完整，但 `_test.go` 作用域 | 灌注层的运行环境；需先把启动部分提到可导入位置 |
| `internal/repository/fixtures_integration_test.go` 的 `mustCreate*` | 可复用，限 `internal/repository` | sim 租户的创建参考 |
| `httpUpstreamRecorder`（138 文件）、`openAIWSCaptureConn`（14 文件） | 事实标准，但包私有且在 `_test.go` | 替身层的基础；先脱离测试文件再迁移 |
| `newInProcessTransport` / `newLocalTestServer` | 可复用，限 `internal/repository` | 不开 socket 的默认路径与沙箱跳过保护 |
| `upstream_protocol_routing_matrix_test.go` + golden + Python recorder | 可复用的成熟模式 | 维度矩阵的生成器模板 |
| `openAIResponsesWSUsageLogCase` + runner（约 35 旋钮） | 一次性，但最佳先例 | 场景结构的泛化起点 |
| `cmd/profit-preview` | 可复用形态 | CLI 形态：JSON 输入 + 复用生产判定逻辑 |
| `codex_probe_template.jsonl` 与占位符白名单 | 可复用 | 请求上下文模板语法，不另造一套 |
| ScheduledTest 系列与 scheduled_test_plans/results | 可复用 | 周期编排与结果存储，不新建调度器 |
| `openai.IsCodexOfficialClientByHeaders` | 可复用 | 客户端画像的识别标准 |
| 三处 `testdata/` 目录 | 可复用 | 继续承载各自包的夹具 |
| usage_cleanup_tasks | 可复用 | 合成数据的唯一清理入口 |
| 51 个手写 Upstream、54 处内联 SSE、25 段重复 `coderws.Accept`、9 处 `Hijack()` | 重复，待收敛 | 替换目标，不保留两套 |
| 前端：只有 `setup.ts`，无任何数据层 | 缺口 | 见 8.1 |
| 统一场景时钟、种子、负载与批量 UsageLog 生成器 | 未形成统一平台；已有 14 处时间函数注入及 Clock 接口 | 历史设计建议，不另增重复时钟抽象 |

新增仅三项：`internal/testutil` 下的四个子包、`cmd/testsim` CLI、场景目录 `backend/testdata/scenarios/`。不新增生产接口、配置项、数据库表或常驻 worker。

### 8.1 前端数据层

前端 418 个 spec 没有 MSW、没有 faker、没有共享夹具。`src/__tests__/setup.ts` 只做 localStorage、matchMedia、IntersectionObserver 等 polyfill，不含任何数据。账单与面板的数据靠每个 spec 自己写行构造闭包，`BreakdownRanking.spec.ts:28` 与 `TokenUsageTrend.spec.ts:21` 是典型。

因此框架要同时产出一份前端数据层：灌注层的行生成器同时导出 JSON 夹具，前端加一个共享的行/序列工厂消费它。这样同一组数据既能驱动后端聚合断言，也能驱动前端渲染断言，口径不会分叉。zh-TW 的断言跑在生成产物上。

## 9. 实施顺序

| 批次 | 内容 | 完成标准 |
| --- | --- | --- |
| S0 | 场景 schema、seed、run 目录；把 `httpUpstreamRecorder` 提升为可导入并迁进 `internal/testutil/upstream` | 一个现有恢复用例改写为场景文件后行为不变；138 个现有调用点不受影响 |
| S1 | 故障目录的 SSE 与 HTTP 部分；比较器与 expect 断言 | 06-recovery.md 的 SSE 相关契约逐条有场景覆盖 |
| S2 | WS 替身与 WS 故障；泛化 `openAIResponsesWSUsageLogCase`；客户端画像（codex_cli 优先） | WS lease 到期、1013、previous_response_not_found 可稳定重放；原 4 个调用点迁移完成 |
| S3 | 把 testcontainers 启动提到可导入位置；数据灌注层与 sim 租户；清理闭环；前端夹具导出 | 1e6 行灌注后面板与聚合可测，清理任务精确回收，前后端用同一份数据 |
| S4 | 并发与长链负载档位；可注入时钟铺到计费与配额路径 | 配额重置与 lease 轮换的时间边界可断言 |

S0 到 S2 不碰生产代码，只改测试与新增库；S3 需要 sim 租户的创建路径，S4 的 `now func()` 字段会触及生产结构体，单独审核。

**2026-10-19 复核修订：** 当前 quality-gate 仅由 `workflow_dispatch` 手动触发，包含全后端 unit、定向 race 和定向 repository integration；没有全量 integration 或 e2e job。下面是历史设计的约束，不能视为已恢复 S0–S4 或建立自动 push/PR 门禁：

- S0 到 S2 的场景必须能在 `-tags=unit` 下跑完，默认走不开 socket、不依赖 Docker 的替身路径，这样才真的进 CI 门禁。
- S3 平台已撤销；现有手动 integration 命令只覆盖其明确选择的仓储用例，不能作为本设计的数据灌注验收。

2026-10-19 已修复测试入口：`make test-e2e` 直接执行现有 `internal/integration` 的 e2e 标签测试，不再调用缺失脚本。`test-e2e-local` 作为既有文档和本次用户明确引用的命令别名复用同一 recipe，待调用方迁移到 `test-e2e` 后移除。测试仍遵守各用例的本地环境与凭据要求，命令可运行不等于所有外部联调用例已执行。

## 10. 验收标准

- 每个场景在同一 seed 下结果逐字节可复现。
- 断言失败时 run 目录足以独立重放，不需要原始环境。
- 合成数据不进入真实统计：抽查聚合口径在灌注前后对真实租户无变化。
- 收敛替身后，重复的上游替身实现数量下降；不出现 testsim 与旧替身长期并存。
- 框架自身有测试：故障注入确实注入了、时钟确实被用上、清理确实只删 sim 行。

## 11. 与传输优化方案的衔接

传输优化的 P0 到 P4 需要的证据，由本框架提供：P0 的统一计时口径与字节计数在替身侧可精确给出；P1 的「合法增量不被扩成全量」用 codex_cli 画像加 WS 故障注入验证；P2 的旁路计算命中率用可控的历史增长场景验证规则正确，再用线上只读数据验证真实命中率；P3 的压缩对比在替身上先验证协商与回退分支，再测真实字节；P4 的会话桥接用并发与断连场景验证资源边界。
