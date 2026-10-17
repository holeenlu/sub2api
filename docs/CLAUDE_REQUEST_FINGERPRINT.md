# Claude 请求指纹：Linux / arm64

## 固定采样基线

目标是 Linux 服务器上的官方原生 Claude Code CLI，默认平台保持 **Linux / arm64**。
2026-10-16 下载官方 npm 包 `@anthropic-ai/claude-code-linux-arm64@2.1.301`，
核验 registry 的 SHA-512 后，在 Linux/arm64 容器中运行原始二进制：

```text
sha512-vMA8QVkLBqO8fZLaCecONZhatIRtoieIwCZ8npn8b0UvinZz40i5PhBIQ52fvhnZYdX16wFdTzhij3iL1RRL7w==
```

采样使用隔离 HOME、虚拟 API Key/OAuth token、本地 TLS 接收器和只读运行环境。
官方域名通过两条路径连接到本地接收器：HTTP CONNECT 和无外网的隔离网络直连。
接收器不向 Anthropic 生产服务转发，证书只在采样进程内受信任。
另采集自定义 endpoint，确认其不生成 CCH。没有使用真实账号凭据。

命令参数：`-p --model claude-sonnet-4-6 --tools "" --setting-sources ""
--no-session-persistence --max-turns 1`。测试向量再添加
`--system-prompt 'Synthetic Linux request fixture.'`，避免收录完整默认提示词。
该非交互入口的 UA entrypoint 是 `sdk-cli`；网关生成的 CLI 身份仍使用 `cli`。
这是固定版本和平台的协议样本，不把它声明为所有未来版本的通用模板。

| 字段 | 抓包 / 内置基线 |
| --- | --- |
| CLI | 2.1.301 |
| X-Stainless-OS / Arch | Linux / arm64 |
| X-Stainless-Package-Version | 0.128.0 |
| X-Stainless-Lang / Runtime | js / node |
| X-Stainless-Runtime-Version | v26.3.0 |
| Accept | application/json，包括 SSE |
| Accept-Encoding | gzip, deflate, br, zstd |
| X-Stainless-Retry-Count / Timeout | 0 / 600 |
| x-app | cli |

服务器**不需要安装或调用 Claude Code CLI**。运行期由 Go 代码构造请求；CLI
仅用于开发阶段的隔离采样。平台标签也不读取网关宿主机的 macOS/Windows 信息。
显式账号头覆写仍生效，可信真实 CLI 的账号 profile 仍用于身份跟踪和非 mimic 转发。

### 版本漂移策略

生成的 OAuth mimic 请求和账号连接检测固定使用 **2.1.301 / SDK 0.128.0 /
node v26.3.0 / Linux / arm64**。UA、billing 和 CCH 从同一已验证基线生成，
不会只升级版本字符串而保留旧算法或混用较新的账号 SDK/runtime。

自动版本同步仍可发现/记录新版本，真实客户端账号缓存仍按原有规则只升不降；
手动设置及 `SUB2API_CLAUDE_CLI_VERSION` 也继续参与身份跟踪和兼容路径。
这三类来源均不升级 mimic 模板。新模型若要求高于该基线的 CLI，须重新采样并
升级模板；仅改设置或环境变量不能让 mimic 自称未经验证的新版本。
回归分别覆盖运行期解析器、进程启动环境变量、较新账号 UA/SDK/runtime 三条漂移路径。

## TLS：按 Linux 实测修正

Linux 样本与此前 macOS 样本不同。直连与 HTTP CONNECT 的 Linux ClientHello
稳定字段一致：

```text
ciphers: 4865,4866,4867,49195,49199,49196,49200,52393,52392,49161,49171,49162,49172,156,157,47,53
extensions: 0,23,65281,10,11,35,16,5,13,18,51,45,43
groups: 4588,29,23,24
key_share: X25519MLKEM768 (1216 bytes), X25519 (32 bytes)
signature_algorithms: 0403,0804,0401,0503,0805,0501,0806,0601,0201
versions: TLS 1.3, TLS 1.2
ALPN: http/1.1
JA3: 1523504b38f0fae0d881d4b6554aac1b
```

不是只比较 JA3：测试把真实原文的全部 ClientHello 稳定字节作为 golden，
仅遮盖随机数、session ID 内容和临时公钥内容，保留其长度、位置和所有其他字段。
归一化握手 SHA-256：
`8845ac2401a951ffc4acef2824c3422124c7883e0c9bc4b5f90d3c6be05da2f9`。
样本位于 `backend/internal/pkg/tlsfingerprint/testdata/claude_code_2_1_301_linux_hello.hex`。

现有直连、HTTP CONNECT、HTTPS CONNECT、SOCKS5 路径复用同一内置模板。
HTTPS 代理先建立并校验到代理的外层 TLS，再做 CONNECT 和到目标的指纹 TLS，
不会把代理认证发成明文，也不会绕过代理。CONNECT 等待期间响应 context 取消。
显式 TLS 模板仍有效；自定义曲线不含 ML-KEM 时不强加该 key share。

## CCH：已复现并接入

此前未命中的根因已确认：2.1.301 更换 XXH64 seed，且哈希输入排除部分字段。
旧实现的 `0x6E52736AC806831E` 加“整个 body 直接哈希”已不适用。

当前原生算法为：

1. billing 文本中的五位 CCH 暂置为 `00000`。
2. 对最终序列化请求字节执行原生扫描规则：跳过紧凑形式 `"model":"..."`
   的 value 字节；跳过 `fallbacks` 数组、`fallback_credit_token` 字符串、
   数字 `max_tokens` 属性和相邻的一个逗号。
3. 对剩余字节用 `XXH64(seed=0x4d659218e32a3268)`，取低 20 位，
   输出补零的五位小写十六进制，写回原 billing CCH 位置。

这不是 JSON 规范化：空白、属性顺序、Unicode 编码等都属于哈希输入。
排除字段仅影响哈希视图，发送的 model/max_tokens 等字段不因此被删除。
新实现复用已有 xxhash 依赖，没有引入新依赖。
原生二进制研究共验证 12 份官方请求（10 份 Linux、2 份 macOS）及 88 份
合成边界输入。后者通过执行官方 macOS 原生扫描/哈希代码生成预期值；
Linux 的交叉证据是实际 Linux 请求抓包，未将 macOS 边界执行冒称为 Linux 执行。

88 份边界输入现随仓库保存于
`backend/internal/service/testdata/claude_code_2_1_301_cch_edges.json`。
其中 75 份原生签名样本逐字节比较扫描后的 hash input，并核对五位 CCH；
另 13 份保留原生 eligibility 不签名结果作为研究证据，不宣称网关复现这些原生门控。
文件同时保留输入、原生输出、路径、门控条件及 hash-input SHA-256。
生成预期值所用官方 macOS 二进制 SHA-256 为
`1f04c69e1c24e4d0a716a863af0aad8fc64d49a499334c29701f073700276b99`。
日常单元测试直接读取 fixture，不执行官方二进制。另加入 scanner fuzz 入口，
包含已报告的畸形输入 `"model":"max_tokens":00`，验证扫描不会 panic 或修改输入。

六份原始 Linux 请求已保存为 golden：
`backend/internal/service/testdata/claude_code_2_1_301_linux_cch.json`。
涵盖 OAuth/API Key、直连、中文/emoji、长文本，以及用户消息内含
`cch=00000;` / `cch=12345;` 的碰撞场景；重签后完整 body 与原生抓包逐字节一致。

网关在 billing 版本对齐、metadata/session 重写、beta 净化和 count_tokens 字段清理后，
紧接着构建 `http.Request` 之前完成 CCH，保证 Content-Length、Body 和 GetBody
重放内容一致。Messages、SSE、count_tokens 均接入该函数。账号连接检测此前
虽调用该函数却没有 billing 块；现在先插入实际 billing，再签名，并通过实际探测
请求的 Body/GetBody 回归确认生效。Vertex/Bedrock 的独立探测路径不改变。
仅修改真正的 system billing text，不全局替换用户文本、工具结果或历史消息。
原生客户端在首个 `"system":[` 后 300 字节中寻找首个占位符，并不识别 billing；
网关有意避免这一误替换行为。异常结构或较远 billing 块的定位规则并非照抄该缺陷。

签名范围明确：仅 `https://api.anthropic.com` 的 Messages/count_tokens、已有
billing block、`cc_version=2.1.301`；finalizer 自身不创建 system prompt。
同一官方端点的重写转发路径遇到其他版本时，删除 billing 中不可验证的旧 CCH，
保留版本、其他 billing 字段和用户文本，不对未知版本套用旧 seed。该路径即使
本次未实际修改正文也剥离未知 CCH，因为网关不能确认它与最终正文相符。
API-key 透明转发路径不调用该 finalizer；自定义端点不注入/清理 CCH。
旧设置键 `enable_cch_signing` 的历史
兼容语义不变；正确签署这一已验证协议字段无需新增配置开关。

## HTTP/1.1：按真实字节适配

仅设置 Go Header map 无法复现原生发送顺序。现在在已完成 TLS 握手的连接上，
复用现有 `http.Transport`，增加一个有界 header 适配器：

- 原生大小写：`Accept`、`Authorization`、`Content-Type`、`User-Agent`、
  `X-Claude-Code-Session-Id` 和 `X-Stainless-*`；Anthropic/x-app/请求提示头保持小写。
- 普通请求头按实际大小写的字节序排列。
- 尾部顺序固定为 `Connection`、`Host`、`Accept-Encoding`、`Content-Length`。
- 保持原值、CRLF、请求行和整个 body 原文；不会重新序列化 JSON 或破坏 CCH。
- 支持分片写入、header/body 合并写入、同连接多次请求和并发读取。
- 只适配有明确 Content-Length 的 Claude CLI Messages/count_tokens 请求；
  chunked、upgrade、异常或超过 64 KiB 的头部保留原传输行为。
- 短写/写错误关闭连接，防止重排后的半个头部被错误重试。

真实 OAuth/API Key header golden 用例逐字节比较，另覆盖连接复用、取消、
100-continue、字节边界、正文内出现 HTTP 样式文本以及压缩响应。
连接池、限流、代理准入、响应解析和取消仍归原有 HTTP 上游层负责。
没有新增独立 RoundTripper 管线。

## 保持的产品行为与准确性边界

### 相对 2.1.301 原生样本仍有的语义差异

| 项目 | 原生样本与当前网关的差异 / 决策 |
| --- | --- |
| OAuth mimic beta | 原生含 `thinking-token-count-2026-05-13`、`cache-diagnosis-2026-04-07`，当前固定集合不默认开启这两个功能。 |
| output-config beta | 网关保留 `mid-conversation-output-config-2026-07-01` 以维持已有会话输出配置兼容性，原生样本没有此项；列表顺序也不同。已有模型/工具过滤和显式客户端兼容 beta 仍生效。 |
| 账号检测 beta | 检测仍使用既有 `DefaultBetaHeader` / `APIKeyBetaHeader`，不是普通 OAuth mimic 的完整集合。补 billing/CCH 不改变检测功能开关。 |
| prompt/request-class 头 | OAuth mimic 现在也发送 `x-claude-code-prompt-id` 和 `x-claude-code-request-class`。保留客户端合法 UUID 和 class hint；缺少合法 prompt ID 时生成 UUID，class 缺省为 `main`。同一下游请求重试复用 prompt ID。 |
| billing 轮次信息 | 网关生成的块仍只有 `cc_version; cc_entrypoint; cch`。原生另有 `cc_prompt_id; cc_turn_origin; cc_prompt_index; cc_turn_index`；网关缺少完整 agent 轮次状态，暂不捏造这四项，也不把新生成的 prompt 头补写成原生 billing 状态。已有 billing 其他字段不会因 CCH 重签被删除。 |
| entrypoint / cache TTL | 本次非交互原生样本 entrypoint 为 `sdk-cli`，网关生成的是 `cli`；网关已有缺省缓存 TTL 为 `5m`，保留客户端显式 TTL。 |

这些差异是当前兼容性边界，不应把 HTTP 大小写/排序适配描述为“完整请求与官方
逐字节相同”。header golden 验证的是给定原生头集合的编码规则；builder 回归
另验证网关的字段策略。beta 涉及功能语义，未为消除抓包差异改动现有功能集合。

### 有意保持的行为变化

OAuth 的 mimic 和非 mimic 路径都会从最终 metadata.user_id 中可解析的 session_id 同步
`X-Claude-Code-Session-Id`，避免账号会话重写后头/体身份不同；metadata 不含
可解析 session 时不凭空补 session。每次重新生成 `x-client-request-id` 只发生
在 mimic（含账号探测）路径，表示独立上游尝试；非 mimic 不主动生成该头。
这与同一 prompt 跨重试保持不变是两个不同维度，已分别加入回归。

- 默认平台始终为 Linux/arm64；不是采集部署服务器的 MAC 地址、硬件序列号或浏览器指纹。
- 账号 ClientID、会话掩码、真实 CLI hint 透传、API-key 透明转发及显式头覆写保留既有语义。
- 指纹 TLS/HTTP 适配沿用账号的现有 TLS 指纹开关。关闭时仍是普通 Go transport；
  自定义 TLS 模板或覆盖值不承诺等同内置模板。
- beta、thinking、工具、缓存 TTL、诊断和轮次字段按真实请求及既有产品策略处理；
  不为模仿一次样本而额外开启功能或捏造完整 CLI agent 状态。
- CCH、HTTP 头和稳定 ClientHello 已有确定性验证；随机 UUID、TLS 密钥、TLS 密文、
  TCP 分段和网络时序不会与另一条真实请求完全相同。
- 未使用生产账号做上游识别验收。这里验证协议字节的一致性，不承诺服务端分类结果。
  升级 CLI/SDK/runtime 基线时须重新采样，尤其需要重新核验 CCH seed/排除规则。

## 验证命令

在 `backend` 下执行：

```sh
go test -tags=unit ./internal/pkg/claude ./internal/pkg/tlsfingerprint ./internal/service ./internal/repository
go test -race -tags=unit ./internal/pkg/tlsfingerprint
go test -race -tags=unit ./internal/service ./internal/repository -run 'Test(BuildUpstreamTransportClaudeHTTP1Wire|FinalizeClaudeCCH|ClaudeCCH|OAuthFinalCCH|BuildOAuthMimic|OAuthRequestsKeepVerified|OAuthMimicKeeps|OAuthFutureCLI|ClaudeAccountProbe)'
go test -tags=unit ./internal/service -run '^$' -fuzz '^FuzzClaudeCCHHashInput$' -fuzztime=5s -parallel=2
go test -tags=integration ./internal/pkg/tlsfingerprint -run '^TestBuildClientHelloSpecNewFields$'
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./internal/pkg/claude ./internal/pkg/tlsfingerprint ./internal/service ./internal/repository
```

本地测试不依赖生产账号；外部 TLS 检测服务仍需显式开启。回归还覆盖请求头与 billing
版本一致、账号 profile 迁移、ClientID 稳定、会话与 metadata 对齐、四种压缩解码及
现有代理安全规则。

2026-10-16 复核修正后再次验证：四包整包单元测试通过（service 265.676s、repository
11.106s）；CCH/版本漂移/提示头/账号探测、HTTP 适配器及端到端四种连接路径的
定向 race 检查通过。scanner fuzz 以两个 worker 运行 5 秒、16,576 次输入通过。
Linux/arm64 交叉编译、本地 TLS 模板集成测试和 `git diff --check` 通过。
整包 `go vet` 仍报告仓库 HEAD 已有的
`vendor_account_operations.go:317` 复制含 Mutex 的 AccountTestService 问题；
该文件未在本任务修改。其他三包 vet 通过，service 排除此既有 copylocks 项后的
其余检查通过。


## 2026-10-18 公开协议补齐

按 `huoji120/sub4api@a3eaf63afc672ffedf7844e6cfc5cb6750d52a3e` 适配真实控制头反馈、原子 masking seed/父子会话映射、原始用户文本采样与 meta 排除、缺失 billing 补齐、count_tokens mimic/Vertex 端点、SSE 完整事件、数值精度与工具映射守卫。实际入口和配置影响见 [传输方案第 13 节](CODEX_RESPONSES_TRANSPORT_OPTIMIZATION.md#13-指定来源协议优化落地2026-10-18-至-2026-10-19)。

本地 Linux/arm64 固定样本、CLI/SDK/runtime 版本与真实 CCH 算法保持；未导入作者 Windows 指纹或占位 CCH。反馈缓存只接收实际上游头，显式空客户端值不会被缓存补回；不生成地区声明、项目活动或供应商遥测事实。masking 新缓存与旧固定 session 键隔离，空闲 15 分钟后轮换；普通未启用 masking 的映射不变。


## 客户端遥测接收与账号转发（2026-10-20）

2026-10-20 最新需求：客户端真实上传时，优先使用会话实际账号保存的遥测副本替换身份与环境；没有副本时，按账号初始化身份、环境及过程数据，再使用该账号凭据转发。此规则替代此前“缺副本只保存”的行为。匿名空 200、合法上传落库后空 200、关闭与隐私退出继续有效。

- POST /api/event_logging/batch 和 /api/event_logging/v2/batch 复用原认证、并发、大小和存储限制。匿名不落库、不生成、不转发；带无效 Key 仍拒绝。开关仍为 forward / disabled，默认关闭。
- 原始事件全部校验并提交数据库后才转发。每批最多 200 事件、正文 1 MiB、环境头 16 KiB、并发 16、落库超时 3 秒、发送预算 10 秒。重复收件按原规则更新计数，不覆盖首份样本；这不是上游幂等队列。
- Messages、API Key 透传、Chat、Responses 转 Anthropic 均在实际上游 HTTP 响应后记录账号。Redis 独立 v2 命名空间保存最多 64 个账号/出站身份变化区间、空闲 TTL 1 小时；用户/Key/分组/原 session 隔离。新键不读取旧标量绑定，旧键按原 TTL 自然到期。
- 带时间的事件按服务器记录的历史区间选择账号；缺时间且历史发生换号/身份变化时仍跳过，不能把延迟事件一律交给最新账号。客户端提供的 account_id 不参与归属。绑定故障不阻断推理。
- 发送前重新验证账号状态、期限、分组和供应资格。有副本时使用共享解析器，支持 JSON 对象/字符串形式的 user_attributes；管理端不再接受永远不能发送的空对象等无效 Claude 副本。已有非法副本不会被静默覆盖，发送只记错误和跳过。
- 无副本时，复用现有身份缓存原子初始化稳定设备 ID，读取账号 OAuth 身份及现有 CLI/runtime 默认配置，构造 vm2api 风格的 env 和 base64 process；仅上传到来时生成。没有运行官方 CLI、创建 VM 或执行登录。模拟 process 指标是配置风格的数据，不是机器真实监控；事件名称、时间、ID、成功/失败及用量仍取当前真实上传。
- 已保存副本的设备 ID 同时用于 OAuth 推理身份。遥测优先使用实际模型出站的 session、device 和经过白名单筛选的环境头，避免复制旧 sample session。UUID/org/email 以已知实际账号凭据为准；不以客户端认证头或 API Key 透传的客户端指纹作为账号环境。
- 已知 additional_metadata、base URL、gateway、hostname 等环境扩展会清理；与身份无关的事件扩展及大整数保留。session/service/language 环境头已支持。按账号和实际 session 拆分发送，避免一个 batch 的头/body 属于不同会话。
- 保留当前账号的上游、代理、TLS、凭据刷新和供应权限检查。最多 16 个分派组，替换后的批次仍受 1 MiB 限制。上游失败只记日志，不自动重试、换号、冷却账号或触发推理账务；不增设定时事件、独立 Growthbook eval 或后台补发。
- 空 200 是本地保存回执，不能证明上游已收到；同步发送可能消耗上述 10 秒预算。无可靠归属、账号无权限、非法副本或初始化缓存失败时仍可“已保存但未转发”。

具体实现与本轮验收见 [vm2api 后续完善](vm2api-improvements-2026-10-20.md)。不修改生产数据，不改写历史迁移。

## 客户端遥测样本留存（2026-10-19）

在两个已鉴权的 Claude 接收入口保存实际收到的合法事件。留存按事件拆分，原始事件 JSON 使用文本字段保存，保留设备、会话、时间、事件 ID、未知扩展及大整数。普通系统日志会截断正文且可能采样丢弃，不能承载完整样本，因此复用 Ops 仓储、权限和清理任务，使用 `ops_client_telemetry_logs` 表（迁移 282），不复用请求计费表或恢复已撤销的账号 CLI 资料编辑器。

- **去重口径**：按网关用户隔离，同一用户跨 API Key 共享去重范围。`event_data.device_id` 相同只保留第一份事件样本；没有设备标识时仅按事件正文摘要去重，不能用 User-Agent、平台或会话 ID 冒充设备身份。正文摘要使用保留 JSON 数字精度的解析结果，消除字段顺序及空白差异；事件 ID、时间、会话等内容不被删去。重复事件只累计 `received_count` 并更新 `last_seen_at`，不覆盖原始样本或首次 API Key。
- **并发和失败**：批次全部校验通过后，在同一事务内入库；用户级事务锁及数据库唯一索引防止多实例并发重复保存。数据库不可用返回 503，整批回滚。原始保存完成后，转发阶段再查已建立的账号归属。
- **数据范围**：只保存白名单环境头。匿名、非法 JSON / 事件类型、超过 200 条或正文超过 1 MiB 的批次不留存。批次级外壳不作为事件样本保存。
- **查看**：超级管理员在 `/admin/ops` 的“客户端遥测样本”展开查询，支持用户、API Key、首次会话及最近接收时间筛选，详情可查看或复制原始事件和环境头。对应只读接口为 `GET /api/v1/admin/ops/client-telemetry` 及 `GET /api/v1/admin/ops/client-telemetry/:id`；列表不返回正文，所有响应禁用缓存。已经移除账号筛选及转发结果展示。
- **旧字段清理**：新增迁移 283 删除仅供转发使用的 `account_id`、`status`、`upstream_status`、`error_code` 列，保留原始正文、环境头、去重键、计数和接收时间。原迁移 282 不改写，避免已应用迁移的校验值变化。
- **保留与清理**：复用运维日志保留天数（默认 30 天）和已启用的 Ops 清理计划，以最后接收时间清除不活跃样本。清理后再次上传可重新建立样本；这不是永久去重。关闭 Ops 或清理计划不会停止入口留存，但会停止自动清理，需要管理员管理容量。本次仅提供迁移文件，未执行生产迁移或部署。

## 将已保存遥测复制到账号（2026-10-19）

超级管理员可在账号新增、编辑页的“账号遥测”中选择已保存的用户样本，读取原始事件正文及环境头并复制到 `accounts.extra.client_telemetry`。该对象只含 `client`、`payload_json`、`headers`，不保存样本 ID、用户 ID、来源账号 ID、外键或引用，不查询来源账号做身份匹配；正文内部原有的设备、账号、会话及事件字段保持原样。新增账号的 OAuth、API Key 和 Codex 导入流程复用同一提交逻辑；编辑页支持预览、复制正文、替换和清除。

这是账号的独立数据副本，原样本被清理后仍可查看和导出，账号修改也不回写样本库。样本选择复用现有运维查询接口（`time_range=all` 可列出所有仍保留的样本），不新增查询或关联接口、不新增数据库表和迁移。账号保存边界共用发送解析器，校验副本事件类型、设备、嵌套环境结构、正文和环境头大小，禁止认证头；不依赖样本库；普通管理员不能读取副本或通过账号配置写入副本。

账号副本继续支持选择、新增、编辑、保存、查看和清除。开启遥测后，它参与当前会话事件的出站身份及环境替换；选择或保存副本本身不会发送历史事件。入库样本仍展示客户端原文；空 200 只确认本地接收，是否转发成功以系统日志为准。
