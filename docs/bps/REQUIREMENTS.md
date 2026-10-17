# ranxi2001 BPS 最终需求与独立集成方案

核查日期：2026-10-11 至 2026-10-12（北京时间）。本文保留固定来源快照的完整需求盘点。核心功能及用户追加的 WS 桥接已在本项目实现；本地最终行为以[适配决定](upstream-changes.md)和[验收记录](VALIDATION.md)为准。来源行为不等于本地默认设置或交付范围。

2026-10-12 增量同步：已审查至 ranxi production 的 a519cfeb66c253dd39d7728d20d55b7cf9f79897。BPS-08 的图片准入以本轮更新为准：仅实际转发到 BPS 的客户请求占用处理预算，覆盖本地 Responses HTTP/SSE、WS、Chat 和 Messages；原生账号不再受此预算影响。其余原始盘点及排除范围保留，详见 [增量适配决定](upstream-changes.md#ranxi-增量同步productiona519cfeb62026-10-12)。

## 1. 基准与结论

| 项目 | 固定基准 |
| --- | --- |
| 来源 | `ranxi2001/sub2api` 的 `production` |
| 来源 SHA | `1a74ed0e3cc6cf6923ba4747e8147decf15556b0` |
| 本地来源目录 | `/Users/luhonglin/Git/sub2api_ ranxi2001` |
| 目标仓库基点 | `holeenlu/sub2api`：`128e28cd7728772bcb7a763450616ec4bdefb023` |
| 核实方式 | 本地干净检出；`git ls-remote origin refs/heads/production` 与来源 HEAD 一致；阅读当前源码、测试、文档及 Git 历史 |
| 需求归属 | KDAN、TapModels、Tokensavy 共享能力；实现先落 main，再按现行交付规范普通 merge |

“最终”指上述 SHA 的最终有效行为，不代表未来不会变化。来源仓库以 production 为维护分支，不能用其 upstream/main 或某个早期 BPS 提交代替。[来源仓库](https://github.com/ranxi2001/sub2api)

BPS 在这里指 **Excel / Basispoints 协议适配**：复用 OpenAI OAuth 账号、原有 API Key 鉴权/分组/计费，将请求转到 BPS 接口。它不是旧 `openai_bps` 独立平台，也不是一个 CPA 插件，更不等于“保证不降智”。截至该基准，完整相关能力包括协议、授权、模型发现、工具、图片、错误恢复、会话状态、管理界面，以及可选的质量自动化与代理池。

**推荐交付方式：按固定源码快照进行有限范围移植，在本项目形成一个完整 BPS 功能提交，同时携带来源清单、适配说明和验收结果。** 不建议整分支合并 ranxi，也不能仅复制 `basispoints/` 就称完成。源码已经穿过账号、凭据、网关、调度、用量、缓存、前端和生命周期接线。

当前项目曾明确删除 BPS 及相关扩展。本次用户明确提出重新整理 BPS，构成对 BPS 的新需求；不据此恢复打票/Cookie 绑定、Prism、独立模型目录、额外 Key 排队或全套运营系统。自动授权、质量处置、Mihomo 的宿主依赖见第 8 节：需求完整列出，实际纳入范围需在实现提交中明确，不能隐式夹带。

## 2. 文档中已过时的说法

以下结论优先使用当前调用路径及测试；旧函数仍存在不等于主链仍调用它。

| 旧说法或容易误读的描述 | 该 SHA 的实际行为/集成要求 | 核查位置 |
| --- | --- | --- |
| 高上下文搜索/生图工具声明会从文本 BPS 自动退回 Codex | 文本 Forward 对命中 BPS 的模型直接进入 BPS；托管工具省略并提示能力缺失，强制工具选择拒绝；不能恢复旧静默回退 | [文本分流][forward]、[协议请求][request]；`446f1f36e` |
| BPS 全部能力一律不回退 | **图片生成/编辑另有例外**：不支持的参数及指定格式拒绝码可退回 Codex。2026-10-17 本地明确 429 采用 BPS 账号池优先、耗尽后原生回退，见 BPS-11；Excel 单授权账号仍禁止使用 Codex | [图片入口][image-entry]、[图片转发][images] |
| 图片默认关闭，默认 HTTPS relay | 全局 BPS 默认开启；图片支持缺省开启，传输缺省 `native`；账号 BPS 开关仍默认关闭；已存明确设置不因缺省值改变 | [图片设置][image-settings]、`setting_parse.go`、`8519ded90`、`0e1cad458` |
| 图片关闭时可以开启“忽略图片”继续旧会话 | 该开关和剥离图片路径已移除；旧 `openai_excel_bps_ignore_images=true` 不再生效；内联图片明确拒绝，HTTPS 图片仍按协议处理 | `openai_excel_bps_images_e2e_test.go`、`0e1cad458` |
| 请求体上限只能设到 128 MiB，图片数固定 20 | 容量上限已提升；默认值与最大值不同；原生附件的字节限制仍有独立上限 | [容量规则][capacity]；第 4.8 节 |
| 工具目录与回放只能存在进程内 | 新增可选 Redis 共享，默认关闭；开启后的故障不能悄悄切回本地历史 | [共享状态][persistent]、[Redis 实现][redis] |
| 401 直接删除独立 Excel grant，重新登录 | 最新实现只标记匹配的 AT 失效、保留 RT；后续读取刷新，真正 invalid_grant 才条件删除；并发替换优先 | [Excel 授权][oauth]；`6084c9ab0`、`b04dff8dd` |
| 开启 BPS 后，没有 Excel grant 的账号立即走 BPS 并报 pending | 普通 Codex 账号保存启用意图及 pending；准备期间保留原生路由，成功后事务激活；BPS 专用测试/发现仍可报告授权不可用 | [账号判定][account]、[授权准备][oauth]；`375a0c4c1` |
| 自动登录只有一个隔离 CLI | 已接入双授权表、重登任务 profile、自动排队及管理界面；早期 `bps-2fa-worker.md` 不能概括最终产品 | [双授权迁移][dual-sql]、[Excel 授权][oauth] |
| 加密消息全部都可选择忽略 | 历史可明确有损省略；**当前新子代理任务正文加密必须拒绝**，不能把标题当完整任务 | [任务完整性][agent] |
| WS 加速或 HTTP bridge 选项能让客户端 WS 进入 BPS | BPS 使用 HTTP/SSE；WS 入口在选择 HTTP bridge 之前拒绝 BPS 模型，现有 bridge 也不调用 BPS Forward。WS 加速是相邻的原生 OAuth 功能 | [WS 入口][ws]、`openai_ws_http_bridge.go` |

另有两项源码范围差异需要保留在验收中：文本模型权限 403 的专用冷却不能自动推断也覆盖 Images 403；Images 路径当前会调用通用 403 关闭/转组处理。`omit_unsupported_tools` 仍在配置及专用探针判断中使用，但不能把它解释为重新启用文本主链的原生回退。

## 3. 产品使用流程

1. 管理员开启全局 BPS，在账号创建、编辑或批量编辑中选择 BPS 模型与策略。
2. 准备授权：可创建 Excel OAuth 单授权账号，或给普通 Codex OAuth 账号准备独立 Excel grant。两类账号能力不同，界面必须说明。
3. 模型列表从实际路由对应的权限源读取；选择模型只决定协议，不授予权限。
4. 客户端使用现有 API Key、模型与 HTTP 接口；原有鉴权、模型白名单、分组、额度和并发继续有效。
5. 管理员通过账号状态、凭据诊断、连接/工具往返测试及用量记录核查结果。HTTP 200、目录 allowed、模型响应名称均不等于完整质量证明。
6. 403/429、凭据失效、图片超限和流中断采用不同处置；自动开关及有损兼容需要管理员选择。

## 4. 最终功能需求

### BPS-01：账号资格、协议开关与路由

- 仅普通 `openai/oauth` 母账号可启用；Free（大小写/空白归一）、shadow、Agent Identity、PAT 不可使用该功能。未知套餐不擅自当 Free。
- 创建、更新、批量更新与运行时都校验资格，不能靠导入旧 JSON 绕过。协议开关不自动恢复 disabled/error 账号。
- `extra.openai_excel_bps` 为启用意图；`openai_excel_bps_models` 按**账号映射后的模型**匹配。未配置该键是历史全模型语义；显式空列表或畸形值不是全模型。
- 部分模型未命中时，普通 Codex 账号维持原生路径；Excel 单授权账号对未命中、全局关闭、账号关闭等情况必须拒绝原生调度/转发。
- 复用现有 Responses、compact 及最终转到 Responses 的兼容入口；BPS 优先于该账号自动透传、WS mode 和打票注入。上游固定 HTTP/SSE，不携带 Codex Cookie/票据或插件身份。
- 选中 BPS 模型的账号参与 BPS 优先排序；清理妨碍新策略的旧原生粘性绑定。BPS 忙/冷却时，原生候选可作为调度容量使用，这与“已选定 BPS 后因工具声明静默改协议”不同。
- 保留本项目现有客户端限制、账号资格复核、用户/账号并发和模型授权。不得为接入 BPS 恢复额外 Key 租约调度。
- WS：当前来源客户端 WS 入口在处理显式 HTTP bridge 之前就拒绝 BPS 模型，关闭原因 `Excel BPS models require HTTP/SSE`（policy violation，1008）；现有 bridge 直接构建原生上游请求，不调用 `forwardExcelBPS`，所以开启该选项也不能接入 BPS。若需要“客户端 WS → 网关 → BPS HTTP/SSE”，属于新增桥接需求，须验证多轮历史、工具往返、取消、计费及响应归属，不应宣称来源已经支持。

证据：[账号判定][account]、[文本分流][forward]、`openai_gateway_scheduling.go`、`openai_excel_oauth_routing.go`、[WS 入口][ws]。

### BPS-02：Excel OAuth 与独立凭据生命周期

- 新建账号提供“BPS OAuth（官方 Excel 登录）”；授权链接、PKCE/state、固定官方回调、code 兑换绑定到同一会话。允许粘贴完整回调 URL，不在日志回显 code/state。
- 继续创建 `openai/oauth`，不新建平台。保存同次会话的 AT/RT/ID Token、client ID、工作区和有效期；重新授权及手动 RT 路径继续选对客户端。
- 会话 JSON 导入支持顶层及 tokens/credentials/session_info 容器，保留来源 client ID；客户端/工作区去重不能覆盖原 Codex 账号。
- Excel 使用其注册 client 与 `/oauth/token?unified=true` 刷新；不附带 Codex scope/客户端头；未返回新 RT 时保留原配对。
- 普通 Codex 账号的主凭据不动；独立 Excel grant 加密保存于 `openai_excel_oauth_credentials`，普通账号 API/导出不暴露密文或 Token。
- Excel AT 到期前独立刷新；进程锁、Redis 锁及密文 CAS 防并发 RT 轮转覆盖；保存失败不能把未落库的新凭据伪装为成功。
- BPS 401：独立 grant 仅标记实际请求所用 AT，保留 RT；迟到的 401 不破坏新 grant；下一次请求刷新。网络失败保留凭据，invalid_grant 才条件清理并准备重新授权，不能重放当前已发生成。
- Excel 单授权账号保留既有主凭据刷新/认证失败处置；不能把“双授权不伤害 Codex”的保证套到所有账号。
- 必须校验 client、邮箱/用户/工作区；Excel ID Token 的 sid 不可当工作区 ID。
- 单纯修改 client ID、请求头、Cookie 或给 Codex RT 换 client 不会转换授权。只拥有旧 Codex Token 的情况需重新完成官方授权。

证据：[Excel 授权][oauth]、[双授权迁移][dual-sql]、`openai_excel_oauth_repo.go`、`openai_oauth_service.go`、`backend/internal/pkg/openai/`。

### BPS-03：自动准备授权及安全状态展示（依赖 Worker）

- 创建/编辑/批量开启 BPS 时保存意图；缺 grant 的普通 Codex 账号进入 `authorization_pending`，准备期间继续原生路由。
- 使用已加密保存的密码/TOTP 登录材料及既有本地 Worker；每分钟检查缺少 grant 的账号。无材料时显示需配置，不伪造授权；材料补齐后继续。
- 同账号 Codex 登录、Excel 登录和 2FA 更换互斥；Excel 任务固定本地 Worker；不更改 Codex 的其他引擎选择。
- 一个任务只处理一个 `oauth_profile`；失败至少 30 分钟冷却；固定配置代理，不自动轮换出口处理登录挑战。额外安全校验明确阻塞。
- 成功写入 grant、移除 pending、刷新调度缓存同一事务；用户已关闭 BPS、修改授权/账号状态时，旧回调不得重新启用。
- 质量规则触发也进入相同准备链，记录 `bps_authorizing`，不另造自动登录框架。
- 凭据诊断只返回受控状态、过期/观察时间、固定错误码和 `requires_manual_resume`；状态包括 unknown、pending、not_expired、expired、auth_failed、revoked。Token、密文、哈希和任意上游错误不得返回。
- “not_expired”只表示时效；原本暂停的账号仍需管理员恢复，不能等同可调用。

证据：[Excel 授权][oauth]、`excel_bps_credential_state.go`、`ExcelBPSCredentialStatus.vue`、`openai_oauth_reauth*`、`tools/openai_excel_*`。本项目缺该宿主运行链，见第 8 节。

### BPS-04：模型发现与能力声明

- 使用 Excel grant 请求 `GET /basispoints/api/responses/access?include_models=true`，读取 allowed、restricted_models、availability/policy、名称与 efforts；不以静态名单冒充实时权限。
- 路由到 BPS 的模型使用 BPS 目录，原生模型使用原生目录。全模型 BPS 不依赖 Codex 目录成功；混合目录中 BPS 获取失败必须明确失败，不能用 Codex 列表补足。
- 缓存按账号、bearer 摘要及代理配置隔离；60 秒新鲜期，到期同步刷新、不返回旧权限；singleflight、5 秒请求上限、1 MiB 响应上限。
- 测试模型选择器、管理端同步、实时列表和 Codex manifest 复用发现结果。仍叠加账号映射、分组限制；管理员显式配置的分组目录不被自动覆盖。
- 仅发布有效模型与 low/medium/high/xhigh 档位；未知新 GPT 模型不需改固定名单，但仍须满足解析规则和上游权限。
- BPS 路由的 manifest 去除加密多代理 v2 声明并重算 ETag；不能让 pinned/fetched 目录绕过此限制。
- 目录 HTTP 失败不改变业务账号健康。allowed=true 不保证某次生成实际使用所请求模型。

证据：[模型发现][models]、`openai_excel_bps_models_manifest.go`、`openai_models*.go`、`openai_codex_models*.go`。

### BPS-05：Responses 转换、文本流与 compact

- 保持请求模型或显式映射，发送 `model_selection=explicit`、`stream=true`、`store=false`；构造隔离的 task/turn/cache 标识，不跨账号/API Key/线程混用。
- 客户端非流式仍读取上游 SSE 并组装最终响应；普通文本实时增量输出，不能全部缓冲到结束。
- effort：缺省/medium→medium；low/high 原值；none/minimal→low；xhigh 及其别名、max、ultra→xhigh。记录请求档位与实际发送档位，不把 max 宣称为独立上游档位。
- 缺省 compaction threshold 为 920000，客户端显式 `context_management` 可覆盖；这不是所有模型的上下文能力保证。
- compact、工具纠正、reasoning 恢复与图片压缩均保留真实用量和响应身份；终态 completed 才可判成功。
- 无进展超时默认 120 秒，可设 0 禁用或 30–300 秒。只计算上游读取等待；注释/空行、发给客户端的 keepalive 不续期，本地验证及下游背压不消耗该预算。
- 超时、取消、failed、cancelled 或不完整流及时关闭响应体并释放资源；不以 HTTP 200 推断业务成功，不自动重放已接受的生成。

证据：[协议请求][request]、[文本转发][bps]、`basispoints/stream.go`、`progress_timeout.go`、`openai_excel_bps_progress.go`。

### BPS-06：客户端工具、动态目录与历史回放

- 支持 function、custom、namespace 和 additional_tools/tool_search_output 中的目录声明；网关只转换，不执行 OfficeJS、shell 或客户端代码。
- 已完成的 `tool_search_call` / `tool_search_output` 历史先校验配对，再将发现的工具合入既有目录，并同时移除搜索调用和结果，不能只删除结果而向 BPS 留下孤立调用。客户端搜索按 call_id 配对；托管搜索支持 server/null ID 的顺序配对。缺少结果、未完成或身份冲突的调用在上游请求前明确拒绝；只含结果的目录快照继续兼容，未完成结果不授予工具权限。
- 工具目录转换为 developer 提示，使用 run_officejs 信封、显式 CUSTOM/FUNCTION_CODE/FUNCTION_CMD transport；保留工具原名、namespace、raw custom/代码文本及 JSON 大整数。
- 原生调用与各类封装共用目录解析：完整名称精确匹配优先；未命中时只允许去掉一层宿主显示前缀 `functions.` 后再次精确匹配，不补命名空间、不做后缀或模糊匹配。
- 顶层 tools 为本轮权威声明；只含历史增量时同名后声明覆盖前声明；省略 tools 时才继承会话缓存，显式空数组清空。`tool_choice=none` 禁本轮工具，不错误清掉长期目录；仅支持 auto/none，required/强制选择明确拒绝。
- 托管搜索/生图等无法执行的工具声明省略并加入能力不可用说明，不能让模型声称已执行。
- 文本工具目录可紧凑表示 schema，但验证保留原始 schema；复杂/不能无损压缩的结构保留完整 JSON。
- 所有工具等待完整终态后整批验证，成功后才提交回放记录并向客户端下发。拒绝未知目标、重复 call_id、无完整身份及禁止并行时的多调用。
- 工具历史包括已转换的搜索调用/结果，不能因为移除了搜索元数据而误判为首次请求并启动未知工具再生成。提示词中的历史名称及执行器内部函数不扩大本轮可调用目录。
- JSON function 完整检查 schema；禁外部引用，schema≤1 MiB，嵌套≤128，数字文本≤4096 字符，指数绝对值≤4096，并限制累计展开预算。不能因紧凑提示词省略真实校验。
- 历史按 call_id 配对，可多个完整工具/乱序结果；保留原生 ID/summary/references。只有结果、无完整调用或可信缓存时明确报错，不猜调用、不丢结果。
- 与原生 Responses 共用具名、无 call_id 的 function_call_output 分类：这是客户端提供的独立外部输入，不是待配对的工具结果。BPS 将其转为带来源说明的 user 消息，保留 namespace/name、正文及图片顺序，不虚构调用或执行状态，不授予新工具权限。内联图片走既有消息附件上传；新输入重置轮次并保留在压缩尾部。带非空 call_id 的真实结果仍须完整配对或可信缓存。
- 子线程优先 thread-id/x-codex-turn-metadata；没有稳定会话时只在本请求去重。并行子代理不加全账号串行锁，仍遵守账号并发。
- author/recipient 转成正文归属说明；agent_message 保留来源、内容与图片顺序，不能抬升为 system/developer 权限；工具业务字段同名不改写。

证据：[协议目录][protocol]、`catalog*.go`、`tools.go`、`history_messages.go`、`tool_schema.go`、相关 `*_test.go`。

### BPS-07：有界纠错、密文兼容与结构化输出

**纠错必须按原因区分：**

| 情况 | 允许行为 | 禁止行为 |
| --- | --- | --- |
| 已知工具封装可识别但格式错误 | 最多 2 次模型纠正；同账号/模型/出口/历史，错误回传标记 executed=false | 变更原操作、顺序、数量或原始代码字节；提前执行部分工具 |
| 首次交互末尾只有一个未知 run_officejs 目标 | 满足无工具历史、未下发工具等条件时最多再生成 1 次 | 流式已交付非空文本/推理摘要/refusal 后继续再生成；schema 错误借此重试 |
| HTTP 400 明确拒绝 opaque reasoning | 仅可删可恢复的 opaque reasoning，固定同一上下文重试 1 次 | 删除密文消息/工具结果/compaction window，或借此重试普通 400/网络失败 |
| 结构化答案不合要求 | 返回验证错误 | 自动修 JSON、去围栏、编造数据、追加模型重试 |

- 2026-10-14 工具纠错表示统一：原始裸载荷在纠错采用 RAW 标记或普通函数包装时都须按原字节绑定，仅限目录声明的 CUSTOM、string code 函数及既有 exec_command/string cmd 规则；不猜测其他函数的原始字段。显式 RAW 标记下的 JSON name/arguments 是载荷内容，不误认成外层工具目标；未标记的畸形/冲突具名包装不得转成 CUSTOM 输入掩盖无法验证的操作。保留目标、明确元数据、schema、批次数量/顺序、身份与回放一致性检查，不增加模型纠错次数。
- 缺少目标名的 JSON 包装属于格式错误，不能进入未知工具再生成。裸 JSON 参数补上包装须保持所有参数值（含大整数），仅对象键顺序和空白不影响比较。2026-10-15 复核：复用转换器现有有界解码器识别完整 JSON 围栏、说明前缀及字符串编码层，再逐值比较；无论纠错返回普通信封或 RAW code/cmd 标记，都先识别原始参数对象，不能将整个 JSON 包装绑定到代码字段。显式 RAW 源码及 CUSTOM 输入继续按原字节处理。原始 cmd/code 字节、明确目标及可验证元数据保持不变；畸形具名包装不能授权任意新参数。操作不一致时仅在既有脱敏诊断中记录原因、工具序号、纠正次数、executed=false、原始代码形状及纠正后工具种类，不记录参数、代码、工具名或调用 ID。
- 2026-10-19（#31791）：现有工具 JSON 解码器允许在严格解析器确定的位置，为有空白分隔的完整 JSON token 补至多 8 个逗号/冒号；最终仍限 1 MiB，键和值及字符串字节不变，拒绝任意层重复键。该恢复不与非法转义/控制字符修复叠加，不补引号、值或闭合符，不修正紧邻的破损 token；RAW code/cmd/CUSTOM 内容不进入此恢复。具名调用可直接转换，裸参数仍需既有模型纠正提供目录目标并逐值校验。未标记且无法解码的 JSON 不得整体绑定为源码；以 original_payload_unverifiable 区分无法核验原文与确已改变参数，保留原次数、用量及批次边界。missing_separator 也可能来自未转义引号，不能据结构诊断保证所有同码错误都可恢复。
- 纠正前关闭前一次响应体，防并发为 1 时死锁；取消立即结束；所有已报告用量在成功或失败时均累计。
- `ignore_encrypted_content` 默认关闭；开启仅对旧历史中消息/工具结果的加密部分原位放省略提示，保留相邻明文和工具配对。
- **新任务末尾 agent_message 或具名无 call_id 的独立外部输入，其顶层或 content/output 密文必须在压缩/上传/生成前拒绝**，即便开启忽略；尾随目录条目或 compaction trigger 不能藏住新任务。
- 此本地拒绝在 HTTP 和 WS 中返回具体 `error.param`，提示刷新模型目录、移除显式 multi-agent v2 覆盖，并在新会话重发原始明文任务；忽略旧历史和图片压缩均不能恢复当前密文任务。不自动换号或跨授权转发该密文，亦不根据可读标题猜测任务正文。
- 协作工具 `message.encrypted` 只在明文目录展示中移除；原 schema、校验、缓存不变，返回明文工具参数元数据；不开放 v2 manifest。
- json_object/json_schema 以 developer 指令表达、最终本地校验，不提供上游约束解码。结构化文本在最终验证前扣住，验证后重建事件；工具续接/refusal/失败按协议保留。
- 结构化 schema≤1 MiB，答案≤16 MiB，仍受 SSE 事件上限约束；不读本地文件或请求远程 schema。

证据：[协议说明][protocol]、[新任务完整性][agent]、`tool_repair.go`、`unknown_tool_repair.go`、`structured_output.go`、`openai_excel_bps_encrypted.go`。

### BPS-08：图片输入、附件与资源限制

图片理解输入与图片生成是两套功能，不能混为一个开关。

- 图片输入支持 PNG/JPEG/GIF/WebP、真实格式/MIME/base64/尺寸校验，64 Mi 像素保护；先验证完整请求，避免无效请求先上传。
- native：用户消息内联图片 multipart 上传固定 BPS attachments，改写为 file_id；工具截图经校验保留 data URL。附件引用只发送 type/file_id；工具截图缺省/null detail→auto，显式 low/high/original 保留。
- native 上传跟随请求取消、禁止重定向，单实例最多 32 实际上传、每次最多 60 秒；先释放上传响应再生成。同请求重复图片仅上传一次但每次出现都计容量。
- 本地可靠性适配（2026-10-14）：原生消息附件不跨客户端请求缓存或共享上传 ID；每次显式请求均从该请求提供的原图上传，同一请求内重复图片只上传一次，全局上传并发仍≤32。替代来源实现的 512 项/30 分钟缓存，避免把本地 TTL 当成上游文件归属仍有效的证明；代价是重复图片增加上传流量和等待。工具截图仍走既有内联方式。
- 本地附件 429 恢复（2026-10-14）：若原始上下文不可跨账号迁移，仅在附件 HTTP 明确返回 429、尚未启动生成时，同账号/凭据/代理/原图最多补试一个被拒附件；整次请求共享此一次机会，不重传已成功图片。优先有效 Retry-After，否则复用既有 429 回避时长；等待至少 1 秒、最多 5 秒，较长限流直接交回原 429 路径。受客户端取消、首输出绝对期限及共享尝试预算限制，启用次数预算时预留后续生成的一次机会；等待中保留 BPS 专属冷却，不清理并发延长的冷却。可重放请求先按 BPS-11 遍历可用 BPS 账号，耗尽后进入原生 Codex 阶段；429 恢复不将带未解决资源引用的上下文改为可迁移，不恢复跨请求文件缓存，不重放生成。
- 附件 429 恢复诊断（#29823）：沿用上述边界，记录实际附件补试次数、跳过/停止原因、等待来源与毫秒数、5 秒上限，以及原始上下文不可迁移的固定字段类别。上下文原因复用原生所有权判定，不解析或记录密文、工具参数、文件/会话 ID；每个 WS turn 清理。区分等待计划、实际补试及共享尝试预算，不能把 attempts=0 解读为上游未收到第一次附件上传，也不能仅凭 unresolved_tool_context 判定具体请求字段。
- 上游明确返回文件归属校验错误时，按固定错误结构识别为 `basispoints_file_ownership_error`，保留真实 HTTP/流内逻辑状态，不换号重放、不改账号健康或模型权限。不重放已发送的生成；后续显式请求携带原图时重新上传，无跨请求附件缓存可失效。客户端自带 file_id 不可由网关恢复，新上传仍失败也不推定为缓存问题。生成、自动图片压缩、两类工具纠错共用此规则，HTTP/SSE/WS 一致。
- 文件归属失败启用既有上游诊断记录时，追加本地 file_ownership_context：失败阶段（生成、图片压缩或两类工具纠错）、该次实际请求中的新上传/复用/非本次计划管理的文件引用数、inline/远程图片引用数，以及成功清理的缓存条目数。引用数按出现次数计；移除跨请求缓存后，reused_upload_references 与 cache_entries_invalidated 为兼容既有诊断固定为 0。非本次计划管理的引用可来自客户端或压缩结果，不据此推定错误责任；零引用也不能排除不透明压缩上下文中的上游文件依赖。诊断不保存文件 ID、图片 URL、内容或凭据，与原始脱敏上游响应分开；不改变日志开关、长度限制或恢复策略。
- 2026-10-18 文件归属诊断补齐（#31490）：fresh_upload_references 是失败请求中引用次数，不能据此判断不同图片或上传请求数；新增 fresh_upload_files 统计实际引用的新上传 ID 去重数。opaque_compaction_items、encrypted_reasoning_items 仅计失败出站 input 的非空密文状态项，compaction_completed 标明当前请求是否已完成网关压缩。可见文件引用均为本次新上传，不排除不透明历史中的上游资源依赖；密文存在也不证明依赖或账号归属。保留真实上游失败、不重放、不记录密文或文件 ID；这些诊断不将未知上游原因误报为已修复。
- relay：base64 流式写私有磁盘，产生免登录 capability GET/HEAD URL；按账号/Key/线程隔离，token 不可猜，访问不续期，相同图片再次提交可续期。
- relay 使用 DATA_DIR 下专属目录（0700）及文件（0600），先预留磁盘/条数，失败回滚；满额/存储失败 503，不逐出有效图片。链接禁 CDN 缓存，日志脱敏；下载并发≤32。
- 切回 native 或关闭支持后旧 relay 链接停止提供。多实例 relay 下载须到创建实例；Redis 工具共享不能解决文件归属。
- 图片支持关闭时，内联图片明确拒绝并提示启用图片支持；外部 HTTPS 图片保持协议处理。旧 `openai_excel_bps_ignore_images` 开关及其文本替换路径已删除，即便历史数据仍为 true 也不能剥离图片。旧文档描述的“忽略图片继续会话”不属于本次最终需求。
- 2026-10-12 增量修正：启用图片支持时，在选定 BPS 账号之后、BPS 改写及图片转换之前预留处理预算。覆盖实际走 BPS 的 Responses HTTP/SSE、WS 每轮、Chat、Messages 及别名/compact（包括纯文本）；原生账号、内部账号测试和探针不占用该预算。
- 请求预算按 BPS 转发时正文约 8 倍、最小 1 MiB 正文估算；正文已通过网关原有读体/解压限制。预留持有到该次 BPS 转发结束，WS 空闲连接不占用。无预算或超限时，在任何上游生成/客户端内容发出前沿既有预算换号；耗尽为 503 + Retry-After:1 或 413。拒绝不产生用量、不冷却或惩罚账号；不增加等待队列，不宣称该预算限制选号前内存或进程 RSS。

| 可配置项目 | 缺省值 | 源码允许最大值 |
| --- | ---: | ---: |
| 请求体 MiB | 64 | 1024 |
| 准入预算 MiB | 1024 | 65536 |
| 在途请求数 | 128 | 4096 |
| relay 单图 MiB | 20 | 512 |
| 每请求图片数，两模式共用 | 20 | 65536 |
| relay 每请求图片总 MiB | 32 | 512 |
| relay 磁盘 MiB | 1024 | 262144 |
| relay 已存图片数 | 512 | 1048576 |
| relay 链接分钟数 | 30 | 10080 |

native 单图仍≤20 MiB、每个有效处理阶段的解码后内联图片总量仍≤32 MiB，包含历史、工具截图与重复项；自动压缩按 BPS-09 分别校验历史输入和压缩后续接输入，提高 relay 数值不改变这些上限。准入预算须至少覆盖正文上限×8，有效预算还受每在途槽最低 8 MiB 的估算约束；磁盘须覆盖一请求的总量/数量。原网关、HTTP 服务和反向代理更小上限仍生效。容量参数不是 RSS、吞吐或“200 人大图不卡”的承诺。

证据：[图片设置][image-settings]、[容量规则][capacity]、`basispoints/attachments.go`、`image_relay.go`、`image_capacity.go`、`openai_excel_bps_attachments.go`、`excel_bps_image_admission.go`。

### BPS-09：图片上限预警与自动 compact

- 策略 auto_compact（新安装/缺失设置默认）、off、warn；默认 warning remaining=8、compact reserve=3。已有显式 off/warn 不自动迁移。
- 自动模式仅在“旧历史+本轮新增”超限但两部分各自未超限、能可靠识别边界时执行。每请求最多压缩一次；固定账号/模型/出口，压缩禁客户端工具，再用真实结果继续。
- 本地容量修正（2026-10-16，#29923）：native 的 auto_compact 同时检查张数与解码后累计字节；超过 32 MiB 也触发边界判断，而非先被整份图片计划拒绝。任何上传前完整校验原请求格式、所有图片及工具调用配对，历史与新增各自遵守单图 20 MiB、累计 32 MiB 和配置张数。先上传历史消息图片并执行历史压缩，再以真实压缩窗口与原始新增输入重新预检，成功后才上传新增消息图片并生成；工具截图继续内联，同请求两阶段共享去重。历史或新增单独超限、边界不可信、压缩后仍超限明确拒绝，不多轮拆分、不静默删图或降低分辨率。
- 两次上传共用原请求取消、首输出期限、准入资源和附件补试预算；压缩后的附件 429 标记 compaction_completed，不能再声明 RequestNotSent。继续沿用原始上下文的恢复条件：可迁移时依 BPS-11 先 BPS 换号、耗尽转原生，不可迁移时仅可同账号补试被拒附件一次；不重复已成功上传或压缩，耗尽返回 429。已报告压缩用量在后续预检、上传或生成失败时仍保留一次。
- 不丢本轮文字/图片/完整工具批次；失败明确返回；两阶段已报告用量累计，生成失败也记压缩成本。
- 客户端必须原样带回网关返回的 compaction window。Redis 保存位置/摘要/预警标记而非完整对话/图片；按账号/Key/线程/模型/策略隔离，闲置 2 小时过期，CAS 防并发冲突。
- 最多 16 次检查点衔接；只移除校验过的已压缩前缀。native 的已验证旧前缀可在活动窗口计数前移除，不再因原始历史累计超过 32 MiB 而否定已完成的压缩；原始客户端上传仍受正文上限和单图校验，剩余活动窗口仍严格预检。缺窗口/换身份/过期可能需手动 compact。
- warn 要求 `1≤reserve<warning<总上限`；20/8/3 时 0–11 正常、12–17 首次警告后可重试、18–20 阻断普通请求但允许 compact 使用预留量；超 20 仍拒绝。
- warn 的原生字节超限会明确提示累计 MiB、自动压缩或缩小图片的处理方式，不能靠直接重试绕过 32 MiB；手动 compact 的输入本身也必须符合原生容量。auto_compact 的预检日志仅记录计数与字节，先记录决策再上传；不在正式响应前插入会提交下游内容的预警事件。已有策略及账号设置均不自动更改。
- 本地激活/诊断修正（2026-10-17，#31015–#31018）：旧 off 默认使自动压缩未进入判断，最终落入附件计划的笼统 32 MiB 错误。统一初始化、读取、保存缺省与前端初始值为 auto_compact；显式关闭仍不产生压缩费用。off 超字节预算在上传前返回 basispoints_image_auto_compact_disabled，明确指出 excel_bps_image_limit_policy=off、开启位置及直接重试无效。image_preflight 诊断记录实际策略、图片数、解码后字节、安全身份及未开始压缩/生成；不记录图片、会话或令牌，不伪造上游状态。缺少缓存/身份的手动 compact 超限也在此阶段明确拒绝。线上旧版本和实际保存策略须另行核对，代码发布不等于部署或修改站点配置。
- 成功 compact 或回到阈值以下重置周期；Redis 状态失败普通路径明确拒绝，手动 compact 保留恢复入口。

证据：[协议说明][protocol] 的 image policy 部分、`openai_excel_bps_image_policy.go`、`excel_bps_image_policy_cache.go`。

### BPS-10：BPS 图片生成与单图编辑

- 复用既有 `/v1/images/generations` 与 `/v1/images/edits`；上游对应 `/basispoints/api/images/generations`、`/basispoints/api/images/edits`。
- 仅明确选入 BPS 列表的 `gpt-image-2`；历史“所有模型”不会自动改变 Images 路由。其他模型保持原有路径，不能宣称所有 gpt-image 变体可用。
- 当前为非流式：生成字段 model/prompt/size/quality/n/background/output_format；只支持 png，background 空/auto/opaque，moderation 空/auto 且出站省略该字段。
- stream、partial_images、output_compression、style、input_fidelity 等走不支持判断。编辑限恰好一张本地上传/data URL，无 mask、远程 URL 或多图。
- 普通 Codex 账号可在本地不支持或上游 400/404/405/413/422 格式拒绝时转原生；Excel 单授权账号在原生出站前拒绝。网络不确定、已接受生成或流中断不得重放。
- 429 复用 BPS 冷却/账号切换；401 复用凭据处理；Images 的普通 HTTP 403 目前会调用自动关闭/转组。账单继续使用现有图片计量与实际上游端点。

证据：[图片入口][image-entry]、[图片转发][images]。这是对当前实现的描述，不能用文本“强制 BPS”口号覆盖它。

### BPS-11：401、403、429 与失败隔离

| 失败类型 | 来源最终行为 |
| --- | --- |
| 本地协议/结构/schema/图片校验失败 | 本地明确拒绝，不处罚账号，不切换账号掩盖输入问题 |
| 独立 Excel grant 的 401 | 保留 RT，标记匹配 AT；后续刷新；不伤害 Codex 主凭据/健康 |
| 文本模型权限 403：model_not_allowed / basispoints_model_access_changed | 按账号+实际上游模型冷却 30 分钟，别名共享；其他 BPS 模型/原生路由不受影响，不自动关闭整个 BPS/移组 |
| 普通真实 HTTP 403 | 依据账号设置独立执行关闭 BPS、移组；不重试当前失败请求 |
| HTTP 429，尚未交付内容 | 本地适配（2026-10-17）：使用 BPS 的请求先尝试其他可用 BPS 账号；BPS 候选耗尽后，原生阶段按现有调度优先级选择有 Codex 授权的账号，包括先前仅 BPS 被限流的双授权账号和未开启 BPS 的账号。原生阶段不再返回 BPS。不能完整重放的上下文仍按 BPS-08 同账号短等待补试被拒附件一次；不因 BPS 写 Codex 全局限流状态 |
| 流内 rate-limit/403/failed/cancelled | 2026-10-15 本地适配：明确 429 的 error/response.failed，在首轮生成、尚无语义输出且无不确定工具执行时，可进入相同恢复流程。已输出内容、工具纠错/再生成阶段不重放；仍冷却 BPS、保留用量。流内 403 不执行普通 HTTP 403 关闭/移组 |
| 网络/流中断/取消 | 释放响应体/槽位，记录安全分类；没有充分未发送证据就不重放 |
| 共享首输出期限耗尽 | first_output_timeout / 504；记录 retry_stopped / turn_budget，不因换号或协议切换重置期限。只有元数据不算语义输出；Ops 保留实际 HTTP 状态（流已建立时为 200），不把本地合成失败写成上游 502 |

- 账号池恢复覆盖 Responses HTTP JSON/SSE、客户端 WS→HTTP bridge、Chat/Messages 转 Responses、附件上传及图片历史压缩的明确 429。BPS 和原生阶段共用既有换号次数、总尝试预算及首输出期限；同一次选号中的 BPS 预筛与原生兜底还共用 64 次主动 SQL/Redis 执行额度和 3 秒期限，依赖错误不冒充候选耗尽；明确 BPS 429 不误计入“首输出超时最多换号一次”或原生 OAuth 429 风暴次数。只有 BPS 429 排除的账号可在原生阶段重新候选，已失败的原生尝试不因此复活；BPS 单授权账号不进入原生阶段。复用原生凭据获取和转发，不将 Excel token 发给 Codex，不永久修改账号 BPS 开关、不增加配置项。
- 检查客户端原始上下文：previous_response_id、conversation/prompt、file/container/vector-store 引用、未配对工具结果和不确定托管工具执行仍阻止迁移。2026-10-17 修正“含密文即绑定账号”的过宽判定：附件上传或生成入口明确 HTTP 429、或本地 BPS 冷却，且当前请求尚未生成/压缩时，完整 input 中独立的 reasoning/compaction 密文可原样重放；必须继续扫描后续项及 tools，拒绝新加密代理任务、消息内嵌密文、重复歧义类型、重复 input/call_id 和其他未解决引用。密文不解密、不删除、不伪造明文；上游仍可拒绝无效密文，不能保证任意密文跨授权兼容。所有候选拿到原始请求及图片，各自重新上传，不复用其他账号的附件 ID；探针、已输出内容、取消、期限和总预算继续限制恢复。
- BPS 冷却时，有原生授权的账号仍可进入恢复流程；BPS 阶段跳过冷却路由，候选耗尽才进入原生阶段，Excel 单授权不能借此走 Codex。原生失败交回现有失败处理，不循环切回 BPS。HTTP 每个请求独立判断，不新增跨请求协议归属缓存；WS 已交付原生响应或成功回退后，在当前连接、当前账号固定原生，避免冷却结束后把原生密文送回 BPS；新连接重新按配置判断。
- BPS 已上报的生成/压缩用量累计一次；最终端点反映实际原生转发，仅 BPS 用量应用 BPS 缓存写入计价策略。JSON 客户端保持 JSON，Codex 上游强制 SSE 不得改变下游格式。
- 429 冷却优先有效 Retry-After，否则用现有 429 默认回避；范围 1–7200 秒，关闭该默认且无有效值时只排除当前请求；并发更新不缩短更长冷却。
- 429 与模型权限冷却目前进程内存保存；Redis 工具状态开关不会共享它们。模型权限条目上限 4096。
- BPS 本地代理获取失败和 BPS 限流不能污染原生账号失败评分；诊断保留实际 HTTP 状态与语义失败状态的区别。
- WS 失败落库使用失败当时冻结的本轮事件，复用 HTTP 的最后真实上游尝试归因；retry_stopped 只保留停止诊断，不能覆盖附件 429 的状态、消息和详情，也不能读取后续 turn 的失败。最后一次无 HTTP 响应的传输失败须清除旧状态，account_auth 保持独立归因；不改变重试次数、等待上限或上下文迁移边界。
- 客户端只接收安全错误码/消息/字段位置；完整上游文本若按现有运维开关记录也须脱敏。

证据：[文本转发][bps]、`openai_excel_bps_ratelimit.go`、`openai_excel_bps_model_cooldown.go`、`basispoints/upstream_failure.go`、[Excel 授权][oauth]。

### BPS-12：403 自动关闭、分组动作与恢复

- 自动关闭与自动移组为两个独立显式选项，缺省关闭；只对符合入口的真实 HTTP 403 触发。关闭协议不等于禁用账号。
- 转组需显式合法 OpenAI/Composite 分组，简易模式仅 OpenAI；0 表示退出所有分组，缺失/非法不能等同 0。
- 替换全部现有分组；已有目标成员关系保留优先级和模型限制。事务中复核凭据、开关、成员、目标有效性，配置变更或目标无效时跳过；更新与 outbox 同事务。
- 并发 403 不重复有效处置；保留服务器管理的关闭/移组时间标记，账号列表显示原因而非断言账号已被封。
- 自动恢复依赖自动关闭及真实关闭标记；账号仍须 active/schedulable、符合套餐资格。手动关闭账号不自动启用。
- 恢复间隔默认 60 分钟，整数 1–10080；以关闭/上次尝试较晚时间算到期。每分钟扫描、单实例≤3 探测、每次≤45 秒；先持久化尝试时间，多实例条件抢占。
- 使用选定 BPS 范围第一个非空模型（历史全部模型用默认测试模型）及原授权/代理发随机文本回声。完整终态且内容匹配才恢复；不回退原生。
- 成功只开回 BPS、清理恢复标记、刷新缓存；不还原 403 已调整的分组。在途旧探测不得覆盖用户新配置/凭据。

证据：`account_repo_excel_bps*.go`、`account_excel_bps_403_marker.go`、`openai_excel_bps_recovery.go`。

### BPS-13：用量、计费与可观测性

- 复用原账单与配额结算，记录客户端模型、映射上游模型、实际响应模型/冲突、请求与发送 effort、实际上游端点。
- `cache_creation_as_input` 显式开启时，普通输入吸收 cache creation；下游写入/创建及 5m/1h 字段归零，不重复增加总 input/total；cache read/output/reasoning 保留。
- 覆盖 stream/non-stream、Responses/compact、顶层及 response.usage；本地先读取原始用量再分类，不关闭上游缓存。
- 纠正/压缩/有界恢复的已报告用量统一累计，失败或取消仍记录已有用量；未报告消耗不能凭空推算。
- 错误区分本地校验、代理获取、传输、HTTP 拒绝、流内失败、取消；禁止记录 Token、密码、TOTP、附件 ID、capability token、原始会话和任意工具参数。
- 管理员主动账号测试可显示 upstream_response：真实状态、受控模型名、request ID、错误码，不增加普通流量的正文采集。

证据：`openai_gateway_usage.go`、`openai_excel_bps_usage.go`、`account_test_bps_evidence.go`、`openai_excel_bps_transport.go`。

### BPS-14：工具状态持久化与多实例

- 默认进程缓存：回放≤1024 项/16 MiB，单项≤1 MiB；目录≤512 会话/16 MiB，单目录≤1 MiB；闲置 2 小时按访问清理。
- `gateway.excel_bps_state_redis_enabled=false` 默认不变；开启且有稳定会话时，回放/目录共享 Redis，目录 CAS，工具下发前完成状态写入。
- Redis 统一预算：≤1024 项/16 MiB，单项≤1 MiB，TTL 2 小时并按访问续期，单次操作超时 2 秒；命名空间 `{sub2api:bps-state:v1}:`，键为作用域摘要。
- Redis 故障、超预算或持久化失败明确报错，不降级成本地历史；无稳定会话仍请求级隔离。
- Redis **确实保存工具回放载荷和目录**，与图片策略“只存摘要”不同；不能把它描述为无会话内容的缓存。部署应沿用现有 Redis 访问保护。
- 该开关不持久化附件 ID、图片文件、429/模型冷却、Mihomo 绑定或完整对话。重启/跨实例能力需分别验收。

证据：[共享状态][persistent]、[Redis 实现][redis]、`basispoints/catalog_cache.go`、`tools.go`。

### BPS-15：传输稳定性与可选代理池

**独立 HTTP profile 为核心；Mihomo/IP 管理为可选宿主能力。**

- BPS 独立连接池，隔离 Codex 和其他长流；禁止将 bearer 通过重定向泄露到其他目标。
- 已确认 H2 连接发生异常 EOF/reset/H2 错误后，**后续**相同 HTTP/HTTPS 代理 BPS 请求使用独立 H1 池；再次使用起 60 秒后试 H2，待使用状态最多 1 小时。普通 EOF/TLS 未建连/取消/应用拒绝不触发。
- 当前失败请求不因 H2 降级重放。传输诊断只记录固定 phase、布尔值、时间；body_read 不证明上游收到，缺写回调也不证明没发送。
- 会话代理默认关闭，关闭使用账号原代理；开启可选 mihomo / ip_pool，空池快速 503，不直连兜底。
- 会话按账号/Key/线程隔离；健康既有绑定保出口，在途不迁移。无会话为临时绑定；最多 4096 绑定；显式普通会话闲置 30 分钟回收，动态出口有 20 分钟本地评估窗口。
- 后台 warm pool 依据已启用可调度 BPS 账号并发汇总目标，每池≤4096；订阅优先、动态作为备用，静态 IP 管理单独池。请求只取就绪出口，不临时探冷节点。
- 后台每池串行补充、网络最多 4 路、每轮 8 秒、单节点 4 秒；固定无凭据 Responses POST 仅 JSON 401 挑战视为通过，就绪有效 2 分钟，到期前刷新；明确拒绝至少冷却 5 分钟。
- 评分优先实际业务完成率，其次连通率并考虑负载；普通流故障 5–30 分钟冷却，动态 30–120 秒。4xx/取消不当流故障。停用/删除来源后不再分配，旧引用释放后换绑。
- 唯一可换出口重试场景为充分追踪证明请求未发：已开始建连、未拿可写连接、未尝试写头/读体、无响应/取消；最多额外一次。附件已上传或可能发送后不换出口重放。
- 无凭据预热只验证路径，不证明账号权限、模型质量、长流稳定或公网 IP 恒定。不得恢复旧的用户请求内 HEAD 冷探测策略。

证据：`repository/http_upstream_bps*.go`、`openai_excel_bps_transport.go`、`openai_excel_bps_warm.go`、`backend/internal/mihomo/bps*.go`。

### BPS-16：账号默认模板、批量管理与入口

- 创建/编辑/批量编辑统一暴露资格、模型、默认/初始模式及更多选项；展示 BPS 状态、403 动作和安全凭据状态，关闭后不丢恢复所需隐藏设置。
- “使用默认配置”读取管理员模板填当前表单；“使用初始配置”使用初始候选和全关闭附加项；切换时重填，保存账号才生效。
- 推荐模板候选为 gpt-6-astra、gpt-5.6-sol、gpt-5.6-terra；推荐勾选历史密文忽略、403 自动关闭、缓存创建转输入。原始初始模式这些仍关闭。
- 保存模板不覆盖已有账号；新建/导入/同步不能因模板存在而自动打开 BPS。`config_mode` 只记录表单来源，不另设运行路由。
- 读取模板失败保留当前输入；切换账号/关闭弹窗丢弃旧异步结果；观察员不读取管理员模板。批量编辑只修改明确勾选字段。
- 来源模板混有 `ws_sse_acceleration` 与 `auto_enable_on_degradation`；它们对应原生 WS 和质量规则，不能作为核心 BPS 的必需开关复制。
- 全局 BPS 开关及图片设置在管理 API、公开配置和前端一致；服务端仍是最终校验来源。

证据：`ExcelBPSModeSwitches.vue`、`useExcelBPSDefaults.ts`、`excelBPSDefaults.ts`、`oauth_auto_config_bps.go`、`BPSDefaultsCard.vue`、`setting_public.go`。

### BPS-17：质量触发自动 BPS（可选，依赖质量运维）

- 状态探针检测原生账号能力，达到连续降智次数或 5h/7d 用量阈值时启用 BPS；可选任一/同时满足。不能用已经走 BPS 的正常回复误判原生恢复。
- failure threshold 0–100、usage 0–100%，至少一个非零；开启 auto_restore 时连续健康次数 1–100（旧零值按 1），可按高用量暂缓关闭。
- 账号弹窗只是既有计划的快捷入口；当前新规则缺省每 2 分钟、探测模型 gpt-6-astra，前端策略缺省连续降智 2 次/健康 2 次。已有规则保留自己的 cron/参数，不照旧文档重置为 30 分钟。
- 规则写入其 BPS 选项并保存恢复快照；只恢复本规则有权恢复且未被管理员改过的字段，不自动把已移走分组加回来。
- 关闭“降智自动开启”只暂停规则，保留规则/历史/当前 BPS 设置；不能顺带关闭已启用 BPS。
- 同账号观察规则、分组处置规则、自动 BPS 规则各可一条；暂停仍占同类名额，同账号检测串行，不创建第二套调度器。
- 可选 BPS 糖果题 observe_only：只测已启用模型、不回退/启用协议，不改调度/分组/403 状态；批量转换旧规则需清理旧处置权，防在途旧结果恢复账号。
- 质量检测通过只证明该次测试，不能当模型真实性证明；不恢复来源中与 BPS 无关的质量运营模块。

证据：`account_quality_bps.go`、`account_quality_observation*`、`accountAutoBPS.ts`、`qualityRulePatch.ts`、历史迁移 254/258。

### BPS-18：测试入口与验收证据

- 保留普通连接/compact 测试，新增 BPS 工具往返模式：固定账号/模型，最多三次真实请求（nonce 文本→echo 工具→回传结果验证），每步必须完整终态且内容匹配。
- 单实例最多 3 个探针，同账号最多 1；不自动定时执行，不写持久“能力已认证”标记。
- 模型列表失败显示明确原因、重载及凭据运维入口；不能用默认模型掩盖 BPS 发现失败。
- 普通连接成功、工具往返成功、真实视觉正确、多代理任务完整及生产容量是不同验收项，不能互相替代。
- 来源文档的真实 OAuth 对照曾出现“请求 Astra、响应声明 Luna”，证明测试账号准入恢复，未证明 Astra 质量。本文未执行任何真实登录、生成或付费探测。

## 5. 配置与状态清单

精确关键词枚举和来源文件/行号见 [source-inventory.json](source-inventory.json)。下表是语义分类，不把所有运行状态当成用户配置。

| 归属 | 字段/接口 | 缺省/边界 |
| --- | --- | --- |
| 全局 | `excel_bps_enabled` | 来源缺省 true；目标可采用首次集成关闭策略，但必须记录为本地差异 |
| 账号 | `openai_excel_bps`、`openai_excel_bps_models` | 开关缺省 false；列表缺失/空值语义不同 |
| 账号表单 | `openai_excel_bps_config_mode` | defaults / initial；不改变协议判断 |
| 工具/历史 | `openai_excel_bps_omit_unsupported_tools`、`openai_excel_bps_ignore_encrypted_content` | 独立选项；密文省略是有损继续；旧 ignore_images 不再提供 |
| 403 | `openai_excel_bps_auto_disable_on_403`、`openai_excel_bps_auto_move_on_403`、`openai_excel_bps_403_target_group_id` | 默认不开动作；0 显式退出全部分组 |
| 恢复 | `openai_excel_bps_auto_recover_on_403`、`openai_excel_bps_403_recovery_interval_minutes` | 默认 false / 60；1–10080 分钟 |
| 代理 | `openai_excel_bps_mihomo`、`openai_excel_bps_proxy_source` | 默认 false；mihomo / ip_pool |
| 计量 | `openai_excel_bps_cache_creation_as_input` | 底层缺省 false；默认模板推荐 true |
| 服务端状态 | `openai_excel_bps_authorization_pending`、`openai_excel_bps_credential_state`、403 disabled/moved/probe 时间与来源标记 | 服务端控制，不允许客户端伪造处置或授权成功 |
| 图片 | `excel_bps_image_relay_enabled`、`excel_bps_image_mode`、`excel_bps_image_base_url` | 来源缺省 true/native；relay 才要求公网 HTTPS origin |
| 图片容量 | `excel_bps_image_body_limit_mib`、`budget_mib`、`max_requests`、`max_image_mib`、`max_images`、`max_total_mib`、`storage_mib`、`storage_entries`、`ttl_minutes` | 后八项同样以 `excel_bps_image_` 为前缀；数值见 BPS-08 |
| 图片策略 | `excel_bps_image_limit_policy`、`excel_bps_image_warning_remaining`、`excel_bps_image_compact_reserve` | auto_compact / 8 / 3（保留已有显式策略） |
| 运行配置 | `gateway.excel_bps_stream_data_interval_timeout` | 120 秒；0 或 30–300；环境变量 `GATEWAY_EXCEL_BPS_STREAM_DATA_INTERVAL_TIMEOUT` |
| 运行配置 | `gateway.excel_bps_state_redis_enabled` | false；启用影响缓存故障语义及数据存储范围 |
| 质量规则 | `quality.action=enable_bps`、`quality.bps.*`，观察模式 `test_channel=bps` + `observe_only` | 复用现有计划，参数见 BPS-17 |
| 授权任务 | `oauth_profile=codex/excel`；独立 grant 表 | 旧任务默认 codex，凭据加密保存 |

对外 HTTP 增量需区分：

- 文本和图片生成复用现有业务入口，不新增 `/v1/bps` 或独立平台。
- relay 新增 `GET/HEAD /api/bps-images/:token` 两个注册；没有匿名上传 API。
- 创建/编辑/批量/设置/账号测试/模型发现主要扩展既有请求与响应字段。
- 来源默认模板在 `GET/PUT /api/v1/admin/account-ops/auto-config` 的 excel_bps 子对象；不能整块带入模型收费、自动并发、OAuth 首配映射。
- 双授权扩展 `POST /api/v1/admin/accounts/:id/openai-reauth` 的 oauth_profile，但本项目已没有该运行入口；若纳入 Worker，相关状态/配置/内部领取回调 API 必须单独计数，不能报为“接口零新增”。

## 6. 目标项目的冲突与推荐取舍

| 范围 | 本项目现状 | 来源行为 | 推荐集成办法 |
| --- | --- | --- | --- |
| BPS 写入 | `account_repo.go` 拒绝所有 openai_excel_bps/openai_bps 字段 | 接受账号 BPS 及服务端状态 | 在既有仓储校验中仅放行经确认的新 BPS 字段；旧独立平台仍拒绝；不删整个退役字段校验 |
| 历史迁移 | 265 清除 BPS、停用特定账号；266 清理附带扩展；268 清理退役网关 | 有自己的 254/258/271 等历史 | 所有已发布 SQL/校验和原样保留；必要时新迁移；禁止复制同号 SQL 覆盖本项目历史 |
| 已清数据 | 旧配置已清、部分账号已暂停/软删除 | 可保存新协议开关 | 不复活软删除平台、不自动恢复停用账号、不猜回已删除 Token/配置；管理员显式重新配置 |
| 网关 | Wei-Shaw 原生 + 当前安全检查 | 大量 ranxi 其他分流和运维调用 | 在本地现有 Forward/选路/计费处接 BPS；不整文件覆盖、不恢复原生路径的其他 fork 行为 |
| 模型目录 | 官方目录链；禁止独立目录治理回流 | BPS access + 许多共享治理能力 | 仅接入路由所需 BPS 权限源和 manifest 限制，不导入独立目录任务/权限系统 |
| 质量检测 | 既有诊断/ModelTrace/Fable 保留 | 完整质量规则及自动 BPS | 核心保留当前检测；若接自动 BPS，扩展现有计划/runner 的最小动作及归属校验；不复活整套运维 |
| 自动授权 | 相关重登运行服务已退役，存在历史表不等于可用 | Worker/加密/双授权服务为宿主 | 手动 Excel 单授权可独立成立；双授权刷新有独立价值；全自动登录需明确纳入最小 Worker 闭环及部署依赖 |
| 代理池 | Mihomo/独立出口管理不在当前保留边界 | 托管代理、IP 管理、warm pool | 默认复用账号原代理；完整代理池作为可选扩展，单列依赖/资源/新增路径，不附带票据采集 |
| 图片准入 | 已退役额外准入；保留原生图片出口安全 | 读体前 BPS 图片预算影响更广 HTTP 请求 | 如采用完整图片能力，必须明确这是 BPS 资源保护例外，并验证对纯文本/非 BPS 请求影响；避免另加 Key 队列 |
| UI/默认模板 | 原生账号页和设置页 | 自动配置整页混有计价/映射/并发等 | 用现有页面承载 BPS 部分，保持品牌与多语言；不复制整页的非 BPS 产品规则 |
| 发布 | 仅本仓库三渠道，人工发布 | ranxi 自己的发布/镜像/版本 | 保留本项目所有渠道、Workflow 与品牌规则 |

必须保留：可信客户端 IP、图片下载 SSRF/重定向出口保护、WS 客户端限制和响应归属、Grok 语音并发/音色归属、Fable 独立阈值、现有账单/检测/迁移历史。新功能不成为绕过它们的例外。

### 实现范围的选择

建议独立提交包含 BPS-01/02/04–14/16/18 的核心与管理员手动入口；BPS-03、BPS-15 的代理池、BPS-17 是明确的可选运营扩展。

这是一项**集成建议，不是已经决定删掉需求**。若用户要“与 ranxi 全部 BPS 行为一致”，上述三类也必须纳入并处理宿主依赖，才能宣称完整复刻；若选择核心版本，交付说明须逐项列明未实现的自动授权/代理池/质量自动动作。不要将未实现的项目藏在关闭的 UI 后面。

来源默认图片开启会影响广泛 HTTP 准入；为本项目首次接入建议全局 BPS/图片初始关闭、管理员逐步启用，但这与来源缺省不同，实施前需作为明确产品取舍记录。本文没有改动运行默认值或退役边界文件。

## 7. 文件与依赖地图

完整候选路径、来源 Git blob、命中行、本地对应文件是否存在，以及关键词提交列表均保存在 [source-inventory.json](source-inventory.json)：共 457 个候选文件、163 个非 merge 关键词提交，含 190 个测试候选文件。**该表是排查索引，不是整文件复制白名单或已证明闭合的依赖图。** 含 BPS 的大文件常同时承载无关功能；不含关键词的公共库也可能是依赖。关键词字段集合还包含诊断代码，不能把其中 44 个名字都算成新增配置项。

| 功能层 | 来源核心位置 | 本地处理 |
| --- | --- | --- |
| 协议内核 | `backend/internal/service/basispoints/` | 保持来源布局与 NOTICE，尽量少改以便 diff |
| 网关适配 | `openai_excel_bps*.go`、`openai_gateway_forward/scheduling/usage.go` | 专属文件移植；共享文件按调用点适配 |
| 账号资格/状态 | `account.go`、`account_excel_bps*.go`、admin_service、account_repo、DTO/缓存 | 统一规则来源；状态变更事务/outbox |
| 授权 | `openai_excel_oauth*.go`、`openai_oauth_service.go`、OAuth pkg/client/import/refresh | 保留普通 Codex；双授权按最小闭环接线 |
| 运行接线 | `backend/cmd/server/wire*.go`、服务构造/停止、repository providers | 必须带构造参数、定时任务关闭、缓存实现；生成文件重生成 |
| 图片输入 | attachments/image_relay/image_policy、handler、middleware、routes、setting | relay 两入口及预算接线；不能只复制转换器 |
| Images | `openai_excel_bps_images.go`、`openai_images_responses.go` | 保留现有安全出站与单授权隔离 |
| 模型发现 | BPS model_access/manifest、models service、测试选择器 | 仅新数据源和能力投影 |
| 传输/共享缓存 | `http_upstream_bps*.go`、gateway_cache_bps_state、image_policy_cache | 独立连接池与可选 Redis，不改其他上游健康 |
| 前端 | Account create/edit/bulk、BPS 状态组件、utils/composables、types/API、四语言 | 就地接入；不恢复无关运营页面 |
| 可选依赖 | mihomo、reauth Worker/加密、quality ops、auto-config | 先定范围，再提取最小依赖；不可直接整目录导入 |
| 构建依赖 | `backend/go.mod` 中 jsonschema/v6 及相关锁文件；Worker Python/Node runtime（若纳入） | 仅引入实际用到的依赖，核查目标已有版本 |

许可证来源以 `basispoints/NOTICE.md` 和仓库 LICENSE 为准；保留已有作者/来源说明。不要把其“独立行为实现、未复制无许可证参考项目代码”的说明改成另一个来源的直接代码授权。

## 8. 独立提交与后续同步方案

### 首次引入

1. 实施开始重新核对本地 main 和来源 production；若 SHA 变化，先更新本清单中的行为差异，不能默默换基准。
2. 从 main 建 `codex/bps-integration` 工作分支/隔离 worktree；保留其他任务现场。当前这份文档不创建运行功能分支。
3. 冻结纳入的 BPS-ID，写明自动授权/质量/代理池和默认值取舍。对共享文件列采用上游/保留本地/融合的具体原因。
4. 按最终快照移植协议和必要调用点；清理被替换的旧路径；不把历史已删除的所有代码整批 revert 回来。
5. 新建迁移只做必要 schema/数据适配，编号以实施时本项目最新迁移为准；不删除迁移账本、不改已发布 SQL、不自动启用历史账号。
6. 完成第 9 节验收后，将业务代码、测试、必要迁移、文档、依赖和来源记录一起形成 **一个 BPS 功能提交**。开发中可本地分段验证，最终独立提交不混入品牌/部署/其他功能。
7. 提交说明建议 `feat(bps): integrate Excel Basispoints from ranxi production`，正文列纳入/排除 BPS-ID、实际验证及已知限制；记录 `BPS-Upstream-Repository`、`BPS-Upstream-Ref`、`BPS-Upstream-Commit`、`BPS-Local-Base`。这些是拟议来源字段，不声称已经存在工具自动识别它们。
8. 这是多提交聚合移植，不能伪造一个 `cherry-pick -x` 来源涵盖全部。日后若实际逐提交移植，用 `cherry-pick -x`；本次快照聚合保留完整来源清单与关键历史。
9. 按现行 `docs/CHANGE_DELIVERY.md`，共享提交 main→TapModels/tokensavy 普通 merge；只有获授权发布代码时才向本仓库三个完整 refspec 推送。Release/部署/生产迁移仍独立。

### 每轮跟随 ranxi

1. 读来源清单的上次基准 A，fetch 来源 production 得到新基准 B；先核实它确实来自 ranxi 仓库。
2. 同时检查 `A..B` 的：协议专属目录、已纳入的共享接入文件、配置/迁移/Worker、测试，以及新增/删除/重命名文件。提交关键词搜索只能辅助，不能漏掉没有 BPS 标题的共享修复。
3. 用三方对照：来源 A、来源 B、当前本地实现。给每项新行为标记采用/已等价/不属于范围/暂缓，并说明理由；新安全修复与依赖提交一并审查。
4. 如果提交纯 BPS 且依赖闭合，优先 `cherry-pick -x`；若混有 Prism/打票/品牌/大范围重构，做精确 hunk 移植，记录真实源 SHA 和偏离。不得为了保留祖先关系整分支 merge。
5. 本轮相关风险通过后再更新基准。**基准更新不等于 A..B 所有行为都已采用**：另存逐项决定和未纳入清单，未解决依赖不能宣称同步完成。
6. 每轮单独一个 `fix(bps)` 或 `feat(bps)` 提交，普通 merge 到品牌；保留历次基准和决策，不重写已交付的首个功能提交。

建议在实际实施时新增 `integration-state.json`：包含 implemented_requirement_ids、implementation_commit、last_reviewed_source_commit、last_integrated_source_commit、source-to-local path mapping、accepted/skipped/pending commit decisions、验证日志位置。当前 source-inventory 中 implementation_commit 为 null，防止把需求审计误当功能落地。

“一次独立提交”解决回滚/审查边界；持续可同步还需要固定基准、最小共享改动、来源记录和行为回归，不能承诺以后全部无冲突。

### 关键历史锚点（不是可直接执行的 cherry-pick 顺序）

| 来源提交 | 对最终需求的意义 |
| --- | --- |
| `d133e7c47` | 最初 OpenAI OAuth BPS 适配 |
| `ad3070b7c` | 模型级路由 |
| `1c3dd267d` / `be285e992` / `49bb7b049` | 结构化输出、缓存创建计量及下游一致性 |
| `604390100` / `6de1ed3a5` / `f6666ab43` | 原生附件、工具截图、file_id 合同 |
| `446f1f36e` | 文本强制 BPS，覆盖此前托管工具回退策略 |
| `dc01c71b7` / `59b1f1ad7` | 429 专属冷却和切号、模型权限 403 专属冷却 |
| `85c220bc6` / `b20518a32` | 403 恢复及自定义间隔 |
| `b570f412c` / `ab65ecba0` | 图片策略/历史衔接、容量上限 |
| `b6617bf62` | 流内失败和取消分类 |
| `bc56155e8` / `b17411605` | Images 生成及单图编辑范围 |
| `0e1cad458` / `8519ded90` | 图片默认 native/开启 |
| `46b4bc50c` / `5aa74e688` / `4222db08a` | Excel OAuth 入口、双授权、单授权路由隔离 |
| `afb7e8ec1` / `3bc1f9a55` / `385a364fc` / `9f0142f1d` / `9d5bb002b` | 动态目录、新任务完整性、超时、实时权限、提示词压缩 |
| `375a0c4c1` | 授权准备 pending 与成功自动激活 |
| `98fb0e534` / `5eb86b425` | Redis 工具状态及写入错误传播 |
| `5756934a1` / `0ab0c9498` | 凭据诊断和真实上游测试证据 |
| `6084c9ab0` / `b04dff8dd` | 401 保留 RT、并发刷新/替换恢复 |

完整关键词候选 SHA/日期/标题在 JSON 中；没有关键词的普通提交不因此排除。

## 9. 验收矩阵

以下是覆盖目标；实际运行结果和未验证边界见 [VALIDATION.md](VALIDATION.md)。优先移植/扩展来源现有测试及目标现有安全回归，不重复造一套同义测试。

| 验收域 | 最少需要验证的独立风险 | 来源测试入口示例 |
| --- | --- | --- |
| 资格与路由 | Free/PAT/shadow 拒绝；映射后命中；pending；全局关闭；Excel 单授权永不发 Codex；BPS 容量溢出 | `openai_excel_bps_models_test.go`、`openai_excel_oauth_routing_test.go`、`openai_bps_scheduler_fallback_test.go` |
| 授权/并发 | client/state/workspace；RT 未轮转；401 与 refresh 竞态；迟到 401；invalid_grant 与新 grant；CAS 失败 | `openai_excel_oauth*_test.go`、repository Excel 集成测试 |
| 工具 | 原始文本/大整数；多工具原子校验；动态目录优先级；父子线程隔离；缓存丢失；不执行 OfficeJS | `basispoints/*tool*test.go`、`catalog*test.go`、`history*test.go` |
| 纠错与任务 | 精确纠错次数；已输出流不再生；代码字节不变；新加密任务拒绝；旧历史省略 | `openai_excel_bps_repair_test.go`、`agent_assignment_test.go`、`encrypted_content_test.go` |
| 结构/流 | JSON/schema 不合格拒绝；失败/取消/断流；心跳不延长超时；背压不误超时 | `structured_output_test.go`、`progress_timeout_test.go`、`upstream_failure_test.go` |
| 图片输入 | malformed 先拒绝不上传；native/relay 隔离；file_id/detail；缓存与凭据轮换；预算释放/磁盘回滚/取消 | `openai_excel_bps_attachments_test.go`、`images_e2e_test.go`、image admission/relay 测试 |
| 图片策略 | 新旧边界、原样窗口、16 次上限、CAS 冲突、预警周期、压缩失败也计费 | `image_policy_test.go`、`excel_bps_image_policy_cache_integration_test.go` |
| 图片生成 | 明确选中模型；生成/编辑限制；单授权禁止回落；不同 HTTP 状态；不确定发送不重放 | `openai_excel_bps_images_test.go`、Images 入口测试 |
| 错误与恢复 | 文本模型权限 403 与普通 403/流内 403 区分；429 切号预算；多实例 403 抢占；不覆盖管理员修改 | model cooldown/ratelimit/auto-disable/recovery/groups 测试 |
| 用量 | cache creation 不重复计；compact/纠错/失败累计；流式非流式一致 | `openai_excel_bps_billing_test.go`、usage 测试 |
| 多实例 | 工具共享隔离/TTL/CAS/预算/故障拒绝；不把其他状态误当共享 | `gateway_cache_bps_state_test.go`、真实临时 Redis 验证 |
| 可选宿主 | Worker profile/挑战/任务互斥；规则处置权；warm pool 零请求探测/不直连/租约 | reauth/quality/mihomo 测试；仅纳入部分执行 |
| 目标安全 | 原有 IP、图片出口、WS 租户归属、Grok、Fable、鉴权及账单回归 | 目标项目既有对应测试 |
| 数据升级 | 从已执行 265/266/268 的库升级；新库；重跑；保留账本/账单/检测/停用状态 | 临时 PostgreSQL，禁止以来源迁移编号直接覆盖 |
| 前端/品牌 | 创建/编辑/批量/模板/错误/刷新；四语言；品牌差异；无关开关未带入 | 来源现有 BPS 组件测试、目标品牌测试、类型与构建 |

真实上游验收需单独使用获准测试账号，至少区分：Excel 登录+刷新、一次最小文本、工具往返、实际图片视觉、compact、流取消及所需真实客户端流程。离线 mock 不证明上游准入、模型质量或生产容量；本次整理不执行这些付费/登录动作。

## 10. 本地交付与后续同步

2026-10-14 本地风控接线修正：BPS 的 HTTP 拒绝和 SSE 流内 `cyber_policy` 必须进入现有 cyber 标记、风控记录及会话屏蔽链路，覆盖客户端 HTTP/SSE、WS、Chat/Messages、图片历史压缩与工具修复。复用风控中心的“cyber 会话自动屏蔽”、原 TTL、用户白名单和 API Key/会话隔离；关闭诊断正文日志不影响屏蔽。策略拒绝不触发换号重放、BPS 冷却、401 凭据失效或 403 关闭/转组；保留脱敏诊断和实际用量，且每次只记账一次。后续同步必须保留该本地适配，无新增开关。验收见 [VALIDATION.md](VALIDATION.md)。

本地已纳入 BPS-01/02/04–14/16/18 核心和 BPS-15 的普通代理传输稳定性，并实现客户端 WS → 原生会话所有者 → BPS HTTP/SSE → WS。BPS-03 自动登录 Worker、BPS-15 Mihomo 托管代理池、BPS-17 自动质量运营未纳入；票据/Cookie、额外准入、独立目录治理继续退役。

2026-10-16 用户确认：新安装的全局 BPS 和图片支持默认开启，传输方式仍默认原生；初始化不覆盖已有站点保存的开关、账号 BPS 开关或容量值。图片参数页展示有效预算与槽位预算下限，原生仅使用图片张数，中转的容量关系在选择中转时校验；容量上限及实际准入算法不变。

本地与来源仍有明确差异：忽略历史加密消息默认关闭；缺少独立 Excel grant 时明确报授权缺失；不恢复自动登录 pending 路由；不自动删除 reasoning 密文重放；不默认丢弃不支持的托管工具；HTTP 与 WS 共享首输出和尝试预算。详见 [upstream-changes.md](upstream-changes.md)。

独立提交包含业务、现有路径适配、测试、迁移 272、依赖、许可证和文档。旧迁移 265/266/268 保持原字节。新增接口、配置、执行路径与实际验证见 [VALIDATION.md](VALIDATION.md)。

每轮同步先在本地来源仓库显式 fetch，再运行只读检查：

```bash
python3 scripts/bps/upstream-review.py "/Users/luhonglin/Git/sub2api_ ranxi2001" --ref origin/production
```

脚本比较 `integration-state.json` 中的固定来源 SHA 与目标 SHA，报告已纳入文件、相邻候选、其他变化、涉及提交和本地后续改动，不 fetch、修改或覆盖任何仓库。新文件还会按内容识别 BPS 线索；发现清单不是依赖完整性的证明。共享文件必须比较实际行为后局部适配，禁止整分支合并或按清单自动覆盖。

更新时保留 `basispoints` 的来源 NOTICE，更新逐项行为决策、源/本地文件散列和验收证据。当前需求或安全语义变化时重新审查；不能仅移动来源 SHA 宣称完成同步。首次有限移植并非来源提交的整体 cherry-pick，使用提交 trailer 和文件清单记录来源，不伪造 `cherry-pick -x`。

[account]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/account.go
[forward]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/openai_gateway_forward.go
[bps]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/openai_excel_bps.go
[request]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/basispoints/request.go
[protocol]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/basispoints/README.md
[oauth]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/openai_excel_oauth.go
[dual-sql]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/migrations/271_openai_excel_dual_oauth.sql
[models]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/openai_excel_bps_model_access.go
[agent]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/basispoints/agent_assignment.go
[images]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/openai_excel_bps_images.go
[image-entry]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/openai_images_responses.go
[image-settings]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/setting_excel_bps_image.go
[capacity]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/basispoints/image_relay_limits.go
[persistent]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/basispoints/persistent_state.go
[redis]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/repository/gateway_cache_bps_state.go
[ws]: https://github.com/ranxi2001/sub2api/blob/1a74ed0e3cc6cf6923ba4747e8147decf15556b0/backend/internal/service/openai_ws_forwarder_ingress.go
