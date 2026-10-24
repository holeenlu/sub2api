# OpenAI Cloud 授权与 Responses 网关

日期：2026-10-24。KDAN、TapModels、Tokensavy 共享能力。本文描述当前实现；本机真实 Cloud 协议证据与生产适配器、数据库、前端验证分别记录，不能互相替代。SIWC 使用链路已经移除，Cloud 不读取旧 SIWC 授权或注册记录。

## 使用方式

管理员在普通 OpenAI OAuth 账号编辑页直接开启或关闭 Cloud 网关，批量编辑与完整参数编辑也支持只修改选中的 Cloud 开关。无需单独登录、回调或授权 Cloud。原生与 Cloud 共享 Codex OAuth 刷新权威，Excel 授权独立保存；原生与 BPS 授权保持独立，Cloud 开关不更换任何凭据。普通用户沿用 Sub2API API Key、分组、余额和订阅，无须接触上游 token。

Cloud 不再要求完整验证后启用，不维护独立模型白名单；模型权限统一由分组与账号普通模型白名单/映射决定。原生账号符合条件即可选择 Cloud；模型是否提供具体上游能力由实际请求结果决定。升级、普通代理或模型配置变化不清空授权、不要求重新测试。

“Cloud 兼容性”仅保留在“测试账号连接”的测试模式下，由管理员主动运行。测试覆盖文本、客户端工具往返与跨实例续接、图片输入、加密历史、实时搜索、生图、可信用量和结算确认，会消耗上游额度。测试不保存能力准入状态，也不改变账号启用、模型权限或授权；失败结果中的能力值不代表后续步骤已经执行。`result.json` 保留安全错误码、状态、阶段和检查点，`answer.txt` 保留答案。管理测试不生成客户账单，普通客户请求仍必须满足计费快照和归属校验。

### 多授权下的协议选择

| 现有账号开关 | 新请求使用路径 |
| --- | --- |
| Cloud 与 BPS 均关闭 | 原生 Codex OAuth 路径；HTTP/SSE/WS 遵循原生传输设置 |
| Cloud 关闭、Excel/BPS 开启 | 系统 BPS 开启且匹配既有账号 BPS 模型/模式规则时走 Excel grant；未匹配的请求保持原有原生回退规则 |
| Cloud 网关开启（BPS 设置保留） | 通过普通分组/账号模型权限的 Responses 请求走 Cloud；不按测试结果选模型，也不自动回退原生/BPS |

沿用已有 `openai_cloud` 与 `openai_excel_bps` 开关，不新增三协议选择器或额外模型路由表。Cloud 为账号级优先：开启时压住 BPS，保留 BPS 授权、开关与模型配置；关闭 Cloud 后恢复既有原生/BPS 分流：BPS 全局与账号开关、映射后的上游模型及已有列表/模式规则共同决定。本轮不修改 BPS 的缺省列表语义、授权或界面。Cloud 失效或请求失败不改变此账号的协议选择；独立请求的推理前恢复遵循下文已有账号调度规则。已建立的 response/tool 会话绑定原账号、授权代次和协议执行状态；不能将 Cloud response ID 或待回复工具迁移到原生/BPS。切换后，指向 Cloud 响应的 `previous_response_id` 返回原生同码 `previous_response_not_found`，增量工具回传返回 `cloud_tool_call_expired`；携带旧 Cloud 调用 ID 的完整历史只是普通历史，按当前协议选择正常调度。

供应商账号、影子账号、PAT、Agent Identity 和独立 Excel 授权不属于 Cloud 授权范围。管理操作复用现有账号可见范围、既有账号参数编辑权限和操作期间的权限复核。

迁移 294 取消独立 Cloud 绑定的启用前置条件；迁移 295 进一步移除人工用量核对及未决消费的账号启用阻断，保留原生账号资格、身份变化失效和账务事实。旧执行绑定及历史账务资料继续保留。退役 OAuth 会话 `oauth:openai:cloud:*` 无读取入口，按原 30 分钟 TTL 过期；不新增清理服务。

## 已核实的上游链路

```text
检查当前原生 Codex OAuth 身份
 → GET https://chatgpt.com/backend-api/wham/usage
 → 必要时经原生 TokenProvider 刷新，再检查 usage
 → wss://codex-cloud-backend.chatgpt.com/
 → initialize → initialized
 → thread/start（experimentalRawEvents=true）
 → 必要时 thread/inject_items
 → turn/start
 → 文本、加密 reasoning、托管工具或客户端工具事件
 → turn/completed（检查 status、error、可信 token usage）
 → Responses JSON/SSE 或客户端 WS 事件；管理测试可下载 result.json / answer.txt
```

WebSocket 使用 `codex-app-server`、`codex-client.desktop`、`openai-bearer.<AT>` 子协议及账号头。客户端自述为 Sub2API，不依赖伪造桌面应用身份。固定目标禁止重定向，复用账号代理；凭据和完整错误体不进入日志。

真实请求已证明根路径握手、初始化、模型查询、文本和动态函数调用、原生角色历史注入、PNG 输入、加密 reasoning 跨线程回传、实时搜索和图片生成。`thread/tokenUsage/updated.total` 是线程累计值，数值可能为 `3415.0`；接受精确非负整数，拒绝小数、溢出和回退。每个新 turn 以先前累计值为基线取差，客户端工具续接继续使用同一个计费根。

同账号其他线程的事件可能进入连接。适配器在解析具体 payload 前按当前 thread/turn 过滤；不输出他人事件，也不回复他人的待处理工具 RPC。仅处理本次授权、模型和请求的事件。

参考：[官方 App Server](https://learn.chatgpt.com/docs/app-server)、[Responses reasoning](https://developers.openai.com/api/docs/guides/reasoning)、[Web Search](https://developers.openai.com/api/docs/guides/tools-web-search)。文档与本机客户端 schema 仅用于定位协议；Cloud host 的实际行为以受控请求为证据。CLI schema 中的 `account/usage/read` 在 Cloud 返回 -32601，不作为补账接口。

## 授权与设置

Cloud 产品入口独立，凭据复用原生 Codex OAuth。真实客户端未证明独立 refresh family，因此 Cloud 私有绑定只保存可信身份和授权代次，不复制 AT/RT。原生 `OpenAITokenProvider` 是唯一刷新权威；首次实际使用时校验原生签名令牌并以 CAS 自动建立执行身份绑定；绑定仅用于会话归属及未决账，不保存或替换原生 AT/RT。并发首次使用读取同一 CAS 胜出绑定。

新迁移 `291_openai_cloud_authorization.sql` 建立私有绑定、扩展 `usage_logs.authorization_mode` 为 `cloud`，并允许历史 `siwc` 日志原值继续满足存储约束；运行时不保留 SIWC 类型、标签或分支。此前发布的 290 清理迁移在升级时删除退役授权表、安装资料及账号标记。数据库 trigger 覆盖账号创建、导入、批量更新和直接凭据/extra 写入。正常 token 轮换保持绑定，稳定身份替换撤销绑定并停用 Cloud。只新增迁移文件，本次代码交付不执行生产迁移或删除历史数据。

历史迁移 291/292/293 保持原字节，避免破坏已应用迁移账本。迁移 293 撤销旧能力准入和代理变化清证据触发器，移除旧能力存储列及 `extra.openai_cloud_models` 字段，保留当前授权、启用选择和未决消费账。此后不再有“升级使能力证据失效、须重新验证”的启用机制；已在旧版本被停用的账号不会被擅自自动启用，可直接在账号编辑启用 Cloud，无需先测试。

| 设置 | 规则 |
| --- | --- |
| `extra.openai_cloud` | 默认关闭；符合原生 OAuth 账号资格即可启用，优先于保留的 BPS 设置 |
| 分组模型限制、账号 `credentials.model_mapping` | 沿用原生模型权限及映射；没有独立 Cloud 模型列表 |

单账号和批量 Cloud 开关只检查普通原生 OAuth 资格，不由未改动的历史 BPS 配置阻断。单账号完整表单若同时修改凭据、资格或 BPS 字段，仍执行原有相应校验。

Cloud 不增加独立管理 API。设置沿用现有账号更新 API；手工测试沿用账号测试 API 的 `mode=cloud`。账号编辑页没有验证按钮、验证模型列表或测试通过门槛；切换编辑账号时防止旧状态请求回写新账号。界面采用说明文字加右侧开关的单行样式，没有授权卡片。

## 对外契约与实现边界

Cloud 以原生网关的对外行为和业务规则为基准，复用公共鉴权、模型权限、调度、配额、取消、错误处理、账务和日志。专属实现只适配上游 App Server RPC、工具调用所需会话，以及异步最终用量的持久衔接。BPS 不作为 Cloud 语义或功能边界的来源；既有 BPS 回归用于确认共享入口修改没有破坏独立协议。

## Responses 映射与边界

- 支持 HTTP JSON、SSE 和客户端 Responses WebSocket。WS 复用现有入口、串行读写、权限/配额、取消及 SSE→WS 桥接；`response.create` 保留 Cloud previous/call ID，不展开原生历史或改写 Cloud 模型名。同一 Cloud session 的 HTTP/WS 请求按相同归属与账务规则续接。本地 `generate:false` 预热 ID 只从当前连接已缓存的完整输入续接，真实 Cloud ID 仍回原 owner，不展开重放。Codex 远程压缩（输入末尾的 `compaction_trigger`）由 Cloud 摘要轮承接，见“客户端触发的压缩”；旧版一元 `/responses/compact` 和 `store:true` 仍明确拒绝，失败不会改走原生或 BPS。
- 接受 Codex HTTP/WS 的 `client_metadata` 字符串映射（最多 64 项、总编码 64 KiB、键最长 128 字节），作为入口客户端附带信息；不外发为 Cloud 身份、认证、工具配置或生成参数，逐轮变化不破坏续接。应用 `metadata` 仍独立保留并回传；错误类型和重复键继续拒绝。
- `prompt_cache_key` 沿用公共入口的缓存亲和用途，接受字符串或 null；不映射为 Cloud thread/owner、工具配置或生成参数，变化不影响工具结果幂等及跨 HTTP/WS 续接。Cloud 没有对应 RPC 缓存键字段，不承诺与原生具有相同缓存命中率。
- HTTP/SSE 与 WS 桥接在同一 Cloud 入口复用原生 legacy `prompt`/`messages` 转换和工具 schema 空 type 修正；转换前检查重复键和请求大小，真实 Cloud `previous_response_id` 始终保留。旧格式工具回传在选号前复用同一转换按 Cloud call ID 固定原 owner，不要求额外携带 previous ID。旧模型的 `reasoning.mode:pro` 沿用原生转换为 max effort，GPT-6 独立 mode 不被静默删去。
- 常用 `reasoning.summary` 映射为 `turn/start.summary`（auto/concise/detailed/none），`text.verbosity` 映射为 `thread/start.config.model_verbosity`（low/medium/high）。同一未完成 turn 的设置固定，省略时继承；完成后新用户 turn 可改变 effort/summary、串行工具策略及输出 schema，verbosity 等线程级参数仍固定。映射核对官方 Codex 源码 `7d592f6dd9988e349e3164feaa21d0fc14bbac8a` 的 client.rs、v2/turn.rs 和 Config.model_verbosity；参数映射有协议回归；真实上游证据与线上验收分别记录。
- 普通 Responses 调度接受原生 Responses 或既有 Chat Completions 转换候选，Cloud 本身只支持 Responses；已绑定原 owner 的 Cloud 续接可通过原有 OAuth HTTP 限制，其他 OAuth 的限制不放宽。
- 角色历史经 `thread/inject_items` 原生注入，保留 system/developer/user/assistant、assistant phase 和函数历史，不压成一段提示词。
- function/namespace 工具转换为固定私有 `sub2api_client` namespace 的 dynamicTools，并通过 `features.code_mode.direct_only_tool_namespaces` 保持直接函数调用。实测默认 exec 包装无法正确呈现客户端图片，关闭 code_mode 本身也不能改变该行为；不保留有损兜底。私有 namespace 不进入下游名称或公开历史。等待客户端工具时返回 function_call，usage 为自上一响应以来确认的那次采样，没有则为 null（见“响应用量与客户端上下文估算”）；收到 function_call_output 后向原 RPC ID 回传结果。文本与内联工具图片均保留原类型、MIME 与字节；动态工具回复没有 detail 字段，只能承载默认 High。
- `parallel_tool_calls:false` 在公开 Responses 层逐个交付客户端工具：每次响应最多一个待执行调用，其余已收到 RPC 有界保留；收到该结果后才交付下一工具。上游 App Server 未提供此规划参数，因此这是网关串行执行保证，不保证模型不提前规划多个调用。工具等待间隙的文本、reasoning 和托管工具事件有界缓冲，接纳工具结果后按历史顺序发布。
- `reasoning.encrypted_content` 可用于无 `previous_response_id` 的历史回传，密文原样保留，不解密或改写；`store:false` 以 `thread/start.ephemeral:true` 建立临时线程，输出保留上游密文，接受 legacy `include`。只发布 summary 和密文，不发布 raw reasoning content、内部 metadata 或注入的系统消息。
- 用户与工具图片采用原生 Responses 网关的透传原则，不复用 BPS 附件/中转格式白名单，也不在 Cloud 维护解码器列表。保留原 data URL、MIME、图片字节和可表达的 detail；网关只检查 `image/*` base64 传输封装、编码、每张 3 MiB 与每请求 8 张，仍受 4 MiB 请求体和会话保留量上限。流式校验 base64，不生成第二份完整图片或位图。图片内容、格式、动画、方向与尺寸由 Cloud 上游处理；网关接纳 SVG/AVIF/TIFF 等 MIME 不代表上游模型已支持。原输入路径的 PNG 完整解码、单边/像素上限已删除，因为网关不再解码输入图片；托管生图输出仍须严格 PNG/尺寸校验后计费。
- 用户图片接受省略及 `auto/high/original`，历史图片保留原 detail；历史工具图片通过原始 `inject_items` 保留 `original`；后续完整历史按 call_id 和原始结果核对，不会把旧图片当作当前待回复工具。待回复的动态工具图片只能表达默认 High，因此 `original` 工具续接明确拒绝；`low` 被当前上游图像准备逻辑拒绝，不擅自改成 High。远程 URL 被 Cloud app-server 明确禁止，`file_id` 缺少客户端文件归属映射，继续返回具体错误。本机 Codex 的 SVG 文件上传/view_image 会在客户端失败；自行发送 SVG data URL 时本网关原样转交 Cloud 判断，不新增 SVG 渲染、远程下载或文件托管路径。
- `{type:"web_search"}` 显式启用实时搜索，返回 `web_search_call` 生命周期、action 和可选 `web_search_call.action.sources`；原始助手引用通过白名单 `url_citation` 输出，并与保存历史一致；客户端原样带回公开输出不再误判篡改，缺失/null/空 annotations 等价，非空引用仍严格比对。
- `{type:"image_generation"}` 显式启用上游托管图片生成，完成必须有通过完整 PNG 解码、尾部和像素上限检查的真实图片与尺寸，转换为 `image_generation_call`。不把成功文本或空 result 当成生成成功。
- Cloud 托管工具的内部 raw custom tool 可能名为 `exec`。它属于上游实现，绝不作为下游待执行的 function/custom call 返回。托管工具与客户端工具可交错完成：上游完成结果在 HTTP 工具等待间隙有界缓冲，下一段响应补齐完整公开生命周期，整轮按上游 ID 去重计费。托管工具继续对话使用 `previous_response_id`，保留原线程上下文；不能仅凭公开 action/revised_prompt 重建内部执行历史。当前拒绝脱离会话的托管工具历史重放。
- `text.format=json_schema` 的严格子集映射为每轮 `turn/start.outputSchema`：要求 `strict:true`、名称和非空 schema 对象；公开响应保留客户端描述，App Server 内部使用固定 schema 名。新 turn 未指定 schema 时恢复文本输出，不继承上轮约束。`description`、非 strict、json_object 等未确认无损映射的形态明确拒绝。
- 仅接受可表达参数；未支持的工具选项、强制 tool_choice、strict grammar 工具、音频/视频等明确报错，不静默丢弃。`tool_choice:none` 关闭全部工具。

turn 已开始后上游报告的加密历史失效保留 `invalid_encrypted_content` 错误，不删除密文并重放；注入阶段（尚未推理）线程被丢弃时，按 2026-10-24 决定去掉 Cloud 读不了的密文后在新线程重试，见“迁移会话与 Codex 回放形态”。原生 HTTP/WS 与 BPS 对推理前的同类拒绝采用同一组去除步骤，见 `docs/openai-transports.md`。托管工具与函数执行后均不自动换账号、改授权方式或重放。

## 会话、恢复与结算

每个活跃会话由一个进程持有原始 WebSocket。Redis 保存隔离到 user/API Key/group/account/grant/model/settings 的归属、命令与事件流。跨实例请求通过命令流回到原 owner；Redis 记录不是可以转移的 socket。工具调用 ID 和 response ID 不能跨 API Key 使用。

重复工具结果按规范化内容幂等；内容冲突拒绝。JSON 重试返回同一结果，SSE 重试重建完整公开生命周期，但不执行第二次工具或写第二笔账。同一完成线程的新用户 turn 使用新计费根，并在发起前冻结实际继承的 reasoning 设置；重放只在同一计费根内进行，新 turn 开始时释放早先轮次的重放结果与工具登记。早先轮次的工具回传（完整历史或增量）由线程历史与响应状态拒绝，返回 400 `cloud_invalid_request`，不重放，也不会拿到当前待执行的调用。

资源上限：全局 128 个活会话、每用户 8 个、每账号 16 个、每实例 16 个 actor；完成后空闲 15 分钟、等待工具结果无有效客户端活动 1 小时、20 秒 owner lease、5 秒心跳；单帧 16 MiB、未消费命令 64 条（本实例命令进 actor 进程内收件箱，跨实例命令进 Redis 命令流并由 owner 读取后按租约令牌回收；取消/关闭另有 16 条预留）、事件队列 256 条/32 MiB，Redis 单请求事件流 16 MiB/8192 条；每个 actor 的会话保留量 16 MiB，只计线程实际保留的数据：新线程计入请求中的历史与输入，续写只计入新消息、已提交的工具结果与工具参数及已发布响应，完整历史请求每轮重发的历史和逐轮替换的账务快照不累计，早先轮次的响应与工具结果仍在线程历史中而照计；每轮最多缓存 128 个响应、登记 256 次客户端工具调用；上游连接每 30 秒 ping，15 秒无应答按连接丢失处理。结束请求清理事件流并留 tombstone，防迟到写入重建。取消先写 tombstone 再排队中断；owner 在连接准备后、创建线程、注入历史、启动 turn 和回传工具前检查请求截止时间与 tombstone，会话 socket 不依赖单次 HTTP context。容量不足时由原 owner 串行回收已完成的空闲会话；不回收生成中或等待客户端工具的连接。活跃生成沿公共请求预算，不按会话创建时间强制终止；工具等待从实际进入等待开始计时，续接更新活动窗口。压力回收后旧 previous ID 返回 `previous_response_not_found`，不重放；完整历史请求另开线程注入历史。明确容量或 owner 派发前拒绝带结构化未启动证明，立即释放该新根的账务预留；Redis 写入结果不确定时仍保留预留。

请求体与原生一样只受 `gateway.max_body_size`：owner 在本实例时请求不经 Redis；跨实例命令以不做 HTML 转义的紧凑 JSON 存入 Redis，上限 16 MiB（`OpenAICloudMaxCommandBytes`），见“请求体上限对齐原生”。公开结果仍限制 4 MiB；Redis 完整记录与事件封装统一限制 4.5 MiB，为账务快照和元数据留空间。终态正文（包含 JSON 转义和名称还原）超限时保存精简的失败响应及已确认用量，保留原 root 的可信结算事实。

活会话记录和未 ACK 账单不靠 TTL 丢弃。owner 失联后由 fenced reaper 接管状态；不尝试重放原待回复工具。已完成结果可恢复其同一结算根；不能确认的执行标记 Unknown，保留最后已观察用量，不推测完整用量或记零账。Unknown、损坏记录及明确永久失败的账务事实先写入迁移 296 的内部 SQL 归档表，再以原始 payload CAS 移出 Redis 正常/旧 unknown 队列。单条坏记录不阻断整批；SQL 故障保留 Redis 重试。归档不写 ACK、不扣款；执行预留可在持久归档后释放，不代表该账已结清。恢复时核对持久归档，避免 Redis 标记过期后重建并扣款。不会停用账号或阻止其他请求。失败管理员探针不成为客户欠账。

已撤销人工用量核对：账号编辑、状态/未决列表/核对接口、SQL 审计写入、Redis 人工结算改写及运行阻断均无活动入口。迁移 295 替换启用校验函数，因此单账号和批量开启均不再受历史阻断字段影响。历史迁移、核对表及阻断记录保留原状，不执行数据清理；旧字段不再参与运行准入。原先因异常被关闭的账号不会自动开启，管理员可用现有开关自行开启。未知账务记录由内部 SQL 持久归档保存原始字节与原因，不复用人工核对表，不新增审核或补单产品入口。归档不自动推断最终用量。

结算在发送前冻结原用户、Key、分组、订阅、账号、模型价格、倍率、分时和 reasoning 口径。工具等待时不填零账；最终可信用量才进入既有原子账本，`cloud:<root_id>` 保证幂等，成功后才 ACK。上游以 `turn/completed` 确认中断或失败的轮次，其累计用量覆盖全部已完成采样，按原生对 `response.incomplete`/`response.failed` 用量块的口径照常结算，用量记录 `billing_status` 记为 `lower_bound`；被丢弃的进行中采样不估算。Key 后来删除也继续按冻结归属结算原用户；数据库暂时故障继续重试，不误判删除。

主模型 token 费用保留；托管搜索另按已完成且去重的调用次数，图片按真实尺寸和张数，叠加既有分组/默认按次价格与倍率。两项价格在请求前冻结。未得到图片模型、图片 token 或实付金额时不推测；这些是本系统收费策略，不是上游发票。`wham/usage` 百分比不参与计费。

## 错误和清理

握手 HTTP、JSON-RPC 数字码、已知 `codexErrorInfo` 和已验证的内嵌 JSON 错误信封转换为固定公开错误，区分参数/加密错误、权限、限流/额度、容量及协议故障。原始 message、additionalDetails、请求体和凭据不回显。实测临时线程对部分无效历史会在 inject 阶段返回 `-32600 unknown thread: <UUID>`；仅严格识别该错误自身的完整签名。该签名出现在注入阶段时尚未推理，按迁移会话规则在新线程重试，仍被拒返回 `400 cloud_history_rejected`；续写已有线程时出现则脱敏为 `502 cloud_thread_unavailable`，不伪装为 `invalid_encrypted_content`、不自动重放。上游 `willRetry:true` 只表示上游正在重试，网关等待真实终态，不再发一个请求。

独立新请求的恢复复用现有 `UpstreamFailoverError` 和账号调度循环。授权准备失败、连接/线程准备失败，以及上游明确推理前限流或容量拒绝，可在无已提交输出、无 Cloud 续接归属、输入完整可重放时重试/换号。临时 5xx 遵守账号原有重试开关，最多同号再试一次；额度/授权拒绝直接换号。A 被排除后，B 按自己的 Cloud/BPS/原生设置与普通资格、优先级、容量选出，没有全池协议固定顺序，也不关闭 A 的 Cloud 开关。换号不重置全轮最多 8 次、同 owner 3 次、同目标 2 次及累计重试等待 10 秒等原有上限；授权准备仍受单独的小预算约束。

`response.created` / `response.in_progress` 在真实输出前暂存，`response.failed` 等待 owner 的持久终态证明再决定发送或恢复；正常响应保持事件顺序。内部结果新增 `rejected_before_inference`，沿现有 Redis 结果保存，不进入公开响应、不建表；旧记录缺失该字段视为无证明。只有 owner 明确的未推理事实、可信零用量、对应计费 root 和终态同时成立才允许释放本地预留，原持久结算继续保留。单纯没有 token、断线、超时或上游说将重试都不构成证明。已开始模型/工具工作、输出身份已提交、消费未知及参数/安全策略错误保持终止；Cloud 响应 ID、工具续接继续绑定原账号。失败反馈和本轮排除使用 Cloud 模式，不污染原生 HTTP/WS 或启动 BPS 恢复阶段。


明确的参数、加密、授权、额度或容量拒绝，且没有任何模型/工具输出或已观察用量时，视为推理前拒绝：公开 usage 仍为空，释放预留且不收按次费用，不制造未知欠账。单纯缺少 usage、网络中断或已经发生工具/模型工作不能使用该规则。

`turn/completed` 仍须检查失败、中断和缺失用量：已测到用量的中断/失败轮次按下界结算，没有任何用量事件的仍为未知。非流式保留安全错误及对应 HTTP 状态，SSE 保留失败终态，已发头后的传输故障发送安全 error 事件；不能因为收到 completed 事件就报告成功。JSON/SSE/WS 返回客户端模型名。健康反馈区分可信成功、失败、参数拒绝、取消、工具等待、幂等重放和未知消费，不把等待或失败算成成功。SSE 及 WS 每 15 秒保活，Cloud 接入整轮首输出预算；心跳和空消息开始事件不计模型输出，复用现有语义输出判定；工具进展和有效输出按原规则结束首输出等待。线程清理只针对本次创建的线程，完成或失败时尝试 archive；清理不证明未知账已补齐。

本机验证使用用户明确授权的官方 Codex 登录。只读取当前 AT，不输出凭据，不创建第二份 refresh token。验证程序为显式 opt-in；未指定授权文件不会发真实请求。生产部署、迁移、发版仍需独立执行，本轮不包含。

验证分为两层：发布前的调度、HTTP 编码、客户端 WS 工具续接、错误、Redis/PostgreSQL、账务与界面回归验证网关适配；“测试账号连接”中的手工测试检查该账号/模型的实际返回，不参与调度准入。密文创建先使用简单算术；仅在已完成、结算确认且线程清理成功但没有 ciphertext 时，追加一次独立的高推理强度递推题。总计最多两次，不重试请求错误、错误答案、账务或清理失败，不用文本答案替代密文证据；取得真实密文后仍必须在新线程回传验证。两次仍缺失时返回 `cloud_encrypted_context_missing`，不能据此断言整个模型不可用。`client_tool/close_session` 表示工具轮已完成、后续清理失败；归档超时返回 `cloud_archive_timeout`，其他归档失败保留安全上游状态/码。探针通过不等于线上 API Key、反向代理和负载环境已经验收。本轮修复没有部署或运行生产 #40 的验收测试。


## HTTP/SSE、WS 与桥接复核（2026-10-24）

| 原生/BPS 已有能力 | Cloud 处理及本轮补齐 |
| --- | --- |
| 客户端入口兼容 | 复用上述原生转换；接受缓存亲和与客户端元数据，生成参数有对应 RPC 才映射 |
| HTTP JSON/SSE ↔ WS 续接 | 共用 Cloud owner actor 和响应/工具归属；混合入口回归覆盖 WS→WS、WS→JSON/SSE、JSON/SSE→WS，保留工具 RPC 与一次结算根 |
| 预算、准入及清理 | 复用公共逐轮并发与预算；补齐 Cloud WS 准备失败的 drain 清理和本地错误归因；预算错误保留 cause 与原生状态码；SSE 心跳沿用原生非语义字节计数，不妨碍安全的推理前恢复 |
| 最新账号限制 | 读取最新原生身份后重新判断 Codex-only，长连接不得沿用旧限制快照；仍按现有分组/账号模型规则 |
| 失败与风控 | 流式200后的失败写入原有 Ops 语义错误；可恢复尝试不提前记最终失败，本地事件交付故障不伪造上游状态；安全策略拒绝接入既有会话风控与日志 |
| 延迟账 | 可选安全错误码随原结算保存，cyber请求在延迟结算时仍按既有类型记录；不改变原收费、零消费证明或幂等语义 |
| 原生专属优化 | 原生 Responses WS池、HTTP zstd及自动后缀裁剪不是Cloud JSON-RPC接口；Cloud沿用原owner长连接和显式续接，不套用原生删ID重放或另造连接池 |

能力边界仍须如实保留：非缺省 `service_tier` 尚未完成 Cloud 实际档位与收费口径映射（`auto`/`default` 与省略等价，已接纳）；`reasoning.context`、独立 `reasoning.mode`、除 `include_obfuscation` 外的 `stream_options`、其他未支持的执行语义仍明确拒绝。入口保护和桥接复用不意味着 Cloud 的每个模型参数、工具或性能都与原生/BPS相同。所有本轮回归为本地夹具与协议测试，生产 #40 未部署或验收。


## 全过程复盘与再次复查（2026-10-24）

责任在实施与验证方法：此前的多次“已修复/回归通过”描述有给出本地限制，但整体完成表述仍容易让人理解为完整客户端已经可用；服务层小请求和手写工具夹具没有充分证明这件事。不能把用户上线后发现基本入口错误解释成正常验收成本。

| 阶段 | 原问题及事实依据 | 本次纠正的方法 |
| --- | --- | --- |
| 需求理解 | 从原生/BPS参考链路发展出重复Cloud OAuth、验证准入和独立模型列表，后续用户逐项要求取消；撤销SIWC曾需再次明确完整清理范围 | 以最新用户需求确定唯一有效契约，区分工程验证与产品开关；撤销按功能及后续演变清理 |
| 方案设计 | 先证明登录→usage→Cloud RPC最小流程，再补Responses适配；没有先列客户端默认字段、完整工具目录及公共入口的生命周期契约 | 先取得实际客户端请求，再设计适配；原生/BPS按调用链复用，不复制一套入口策略 |
| 开发 | 严格白名单把client_metadata、prompt_cache_key当成不支持语义；另有公共取消、写入保护、错误状态和后续轮设置继承漏接 | 区分客户端附带信息、会话归属、执行控制；每项映射有依据，关键公共机制以实际调用点验证 |
| 复核/测试 | 反复扩充局部测试，却曾缺少跨传输和“完成一轮后再发下一轮”；上一轮summary映射新增了未覆盖的继承交互 | 先复现失败，再测真实请求形态和跨轮生命周期；现有测试通过不是新增交互已覆盖的证据 |
| 交付/上线 | 已推送与已部署、已授权与真实客户端可用未形成同一条可追踪证据；没有线上#40成功验收记录 | 交付说明列源码/本地/真实客户端/上游/线上五层证据，不宣称未执行层通过；部署仍独立授权 |

首轮复盘在不更改用户provider或登录配置的情况下，用本机 `codex-cli 0.159.3` 的 ephemeral、ignore-user-config 与临时loopback provider取得实际HTTP/SSE请求、WS `generate:false`预热及正式 `response.create`。实际请求包含106个function/namespace工具、developer/user多段内容、summary、cache key和client metadata；使用固定模拟响应让客户端退出，未调用真实Cloud、未执行工具。初次WS接收器以HTTP/1.0回复导致客户端fallback，已改为HTTP/1.1并确认实际捕获WS帧；这也验证了必须检查实际传输而非只看配置。脱敏夹具只保留字段/工具schema形态，正文、元数据值和工具描述已去除或替换，不含凭据和请求头。

新增问题回归在修改前实际失败：完成后的第二轮省略reasoning被误拒、取消后额外发502、Flush失败未被识别。修复沿用原有actor全对象继承、WS取消终态归属以及 `GuardSSEWrites`；继承effort只用于账务和首输出期限，不再修改请求成半个reasoning对象。客户端写入失败归因下游、终止派发且不自动重放；已经发生的消费仍由原持久结算处理。

| 再次复查项 | 当前结论与证据边界 |
| --- | --- |
| 原生授权/usage/OAuth刷新/Cloud初始化 | 沿原生TokenProvider和Cloud固定握手；真实上游能力与账号权益仍需线上观察，不以本地替身证明 |
| JSON、SSE、WS入口与预热 | 实际CLI报文用于服务/桥接回归，保留工具规模和多段内容；既有真实WS连接测试仍覆盖入口生命周期，尚非部署环境的完整鉴权路由验收 |
| 函数/namespace、加密历史、图片、搜索/生图 | 沿现有协议与保护回归；原生未验证参数、远程图片、custom grammar工具不伪装支持 |
| 工具等待、完成后新轮、HTTP↔WS | 原owner/Key/group/account绑定、幂等工具结果与新轮单独账根；新增完成后完整reasoning继承回归 |
| 取消、客户端断线、慢写、Flush错误 | HTTP复用原生有界写出；WS复用公共控制与drain；取消不抢发502终态，写失败不成为上游健康故障，不能证明最终用量时仍保留未决账 |
| 首输出、心跳、重试、错误码 | 已接公共预算及非语义心跳计数；推理前证明才允许恢复，输出后和已绑定续接不换号重放；错误保留安全状态/码 |
| 账务、风控、限额与配置变动 | 沿冻结价格与原子账本；未知消费、身份代次和最新客户端限制继续保持；不通过重新授权或测试清除未知账 |
| 原生传输性能优化 | Cloud是独立JSON-RPC长连接，不直接套原生Responses池、HTTP zstd或自动后缀裁剪；已有owner续接可复用连接，但没有生产性能对比证据 |
| 延迟观测 | Cloud 入口接入既有 timing v2，按真实事件及下行 Write/Flush 记录；整轮开始、首文字、工具等待及WS入口事实随延迟账保留。未测量的阶段保持缺失，不把上游事件时间伪装为客户端 flush；不宣称性能等价 |

当前还不能宣称Cloud与原生/BPS“所有能力完全等价”：`service_tier`、旧版 `/responses/compact`、部分工具/图片形态仍是明确边界，生产#40指定版本与原客户端的完整验收尚未执行。工程方法在[交付规范](CHANGE_DELIVERY.md#协议适配的需求与验收证据2026-10-24)中统一维护；不会据复盘重新增加账号验证准入或额外授权。


### 真实客户端序列回放补充（2026-10-24）

在首轮报文基础上，再使用本机 `codex-cli 0.159.3` 对临时 loopback 接收器采集九种场景：HTTP/WS 双 `exec_command`、HTTP/WS PNG 与 `view_image`、WS 断开后 HTTP fallback、HTTP 429、部分输出后失败、HTTP/WS SIGINT。两次 exec 仅运行固定 printf；图片由本任务生成。未更改登录/provider配置，没有调用真实上游。106个工具目录完全相同，复用原夹具的单份目录；正文、描述、路径、元数据标识脱敏。场景摘要保留客户端实际退出状态与传输记录。

真实请求暴露并先回归复现了 Cloud `detail:high` 拒绝。依据本机官方 Codex 源码 `7d592f6dd9988e349e3164feaa21d0fc14bbac8a` 的 `protocol/src/models.rs`、`dynamic_tools.rs` 和 `app-server-protocol/.../turn.rs`：用户图片支持detail；动态工具图片没有detail字段，转换为Responses时使用默认High。首次修复接纳auto/high；本轮用户输入补齐original，动态工具回复仍不能表示original。输入图片采用原生透传机制，保留编码、字节和请求总量检查；远程图片与low仍明确拒绝。

对照时还复现了BPS账号模型映射后HTTP/JSON、SSE返回上游模型名的差异。复用原生 `replaceOpenAISSEModelData` 只还原协议模型字段；`UpstreamResponseModel`仍保留上游值，工具参数与生成文本不改。

| 本地验证路径 | 真实请求与断言 |
| --- | --- |
| 原生HTTP→HTTP JSON/SSE；WS→HTTP；WS切HTTP | 完整106工具、双调用/工具结果、PNG和view_image；返回工具名/参数、模型和用量；每轮仅一次生成 |
| 原生HTTP→WS JSON/SSE；WS→WS ctx_pool/passthrough | 确认实际调用WS替身、没有HTTP兜底；预热、完整工具历史与图片增量续接、返回用量 |
| BPS HTTP JSON/SSE和WS→HTTP | 模拟真实officejs工具封装及附件上传，验证公共工具还原、图片续接、客户端模型与上游诊断模型区分；没有虚构BPS上游WS |
| Cloud HTTP JSON/SSE、WS桥接及双向切换 | 七种图片工具续接组合，完整目录、历史注入、turn/start图片High与工具inputImage；首个等待段usage为空，最终一笔结算；双工具actor使用实际CLI参数/结果检验间隙到达、幂等与归属 |
| 失败/取消 | 真实请求进入原生/BPS HTTP与WS桥接429和部分输出后失败；Cloud已接收失败不重放；既有Cloud/BPS/HTTP→WS取消、Flush失败测试替换为完整CLI请求；保留既有持久账与未知消费回归 |

WS切HTTP的双工具场景为实际采集序列；图片切换为分别采集的WS首轮与HTTP完整历史组合。不能把WS增量直接当HTTP fallback：BPS HTTP要求展开历史，实际CLI fallback携带完整历史。Cloud图片测试直接经过服务及WS桥接函数，真实WS连接生命周期另由既有入口测试覆盖。原生/BPS回放使用内存WS入口（读取上限对齐生产路由），上游仍是替身。

部分输出后的失败在采集时触发了CLI自身的HTTP fallback；这和网关重放是两件事。网关回归分别检查已输出失败不得内部重放、429由外层调度恢复。预热为零生成/零用量；Cloud最终结算按一个模型turn，原生/BPS两个工具交互响应各自记录上游用量，不能按HTTP请求数套用同一种结算方式。

这些结果不证明托管实时搜索、图片生成或加密上下文在真实Cloud上游成功；这三项本轮沿用现有协议/用量/错误回归，不产生新的真实上游证据。未执行线上#40账号验收、真实数据库原子扣款或生产部署；没有恢复产品验证准入、单独授权或独立模型名单。

### 图片形态补充核验（2026-10-24）

使用同版本 CLI 向 loopback 接收器新增十个 HTTP/WS 采集场景。JPEG/WebP 上传及 `view_image` 保留原 MIME 与 high；GIF 两条路径均由客户端转成 PNG。SVG 没有发出 `input_image`，工具返回 `unable to process image: invalid or unsupported image data`；接收器提供最终文本导致 CLI 退出 0，不代表 SVG 视觉可用。夹具为 `testdata/codex_0_159_3_images.json`，原始凭据及个人路径未保存。

`original` 场景使用 gpt-5.5，工具图片实际带 PNG/original，其真实目录只有 13 项且含 custom grammar/tool_search，与普通场景 106 项目录不同。保留独立目录作为证据；回归仅提取真实图片 part 检验 detail，不能据此宣称 Cloud 支持整份 gpt-5.5 请求。用户 original 及带新用户输入的原始历史可无损表达，待回复动态工具 original 返回可操作错误，不降级。依据固定本机 Codex 源码的 `core/src/image_preparation.rs`、`protocol/src/dynamic_tools.rs` 与 `app-server/src/image_url.rs` 核对上述差异。

既有原生/BPS HTTP、SSE、WS及桥接回放扩入 JPEG/WebP/GIF 采集；Cloud 四种来源图片 × 七种续接组合检验原始字节/格式、同一 owner、首个等待段空 usage 和最终一笔结算。另覆盖 SVG/AVIF/TIFF/未知 image MIME 的原样转发、编码与字节/张数边界；不以本地解码器判断内容、动画或格式。HTTP JSON/SSE 与 WS 的明显封装/精度错误在上游 RPC、会话与计费预留前拒绝；已有会话的工具精度由 owner 按实际 pending call_id 判断，错误保留具体 400 且不提交工具、不重复结算。完整历史变更拒绝，正确历史重试继续原会话。BPS 业务代码保持原样，未共享其格式限制。

本节为真实客户端本地采集、固定源码核对与本地替身测试的证据范围；没有新增真实 Cloud/BPS 上游或线上 #40 验收。新增格式在真实 Cloud 的模型可用性仍须线上确认；没有改变授权、模型权限、账号开关或验证准入。

### 当前流程与原生差异复核（2026-10-24）

账号使用流程为：原生 OAuth 登录 → 单账号/批量编辑 Cloud 开关 → 公共分组/账号模型权限与映射 → 原生 TokenProvider → wham/usage（401 时沿原刷新权威重试）→ Cloud WebSocket → initialize/initialized → thread/start → 必要的历史注入 → turn/start → 事件流 → turn/completed。JSON/SSE/客户端 WS 共用同一 actor；手工连接测试另外导出 result.json/answer.txt，不是生产请求的准入步骤。没有第二次 OAuth、完整验证门槛或人工用量审核。

| 环节 | 与原生/BPS的关系及必要差异 |
| --- | --- |
| 授权、开关、模型权限 | 原生凭据和刷新权威；普通分组/账号限制与映射；Cloud 开启账号级优先，关闭恢复既有 BPS/原生规则。BPS 维持既有路由规则，Cloud 修复不调整其模型语义。 |
| HTTP/SSE、WS、桥接 | 复用公共入口、策略与传输生命周期；HTTP/SSE 补齐映射后的 Fast 策略，WS避免二次执行；HTTP与WS心跳沿原生 stream_keepalive_interval，0关闭，不引用BPS专用间隔；Cloud 上游始终是 app-server RPC WebSocket，不能把它视为原生 Responses WebSocket 直通。 |
| 图片与工具 | 内联图片原始 MIME/字节透传，数量上限按单条消息/工具输出计；function/namespace/custom 调用身份和工具结果保留。动态工具不支持 original detail，low/远程图片/file_id仍是上游或文件归属边界；历史工具图片按 call_id 与保存值核对。 |
| 续接、取消和失败 | 同 actor/owner 保持 pending RPC；完整历史和增量输入可续接，拒绝篡改历史；只在明确推理前允许统一恢复。已输出、工具副作用或未知执行不重放，取消和下行写失败沿原生命周期处理。不能从旧 response 分支或在 owner 丢失后重放待回复工具；完整历史在原线程已结束或分叉时另开线程注入。 |
| 用量与结算 | 可信最终用量沿原价格及原子账本，整轮一次结算；公开 usage 按采样报告，工具等待响应报上一次采样、最终响应报最后一次采样。上游确认结束的中断/失败轮次按已测用量结算并标记 `lower_bound`；连接或路由丢失、未确认结束的未知用量保留事实且不扣款，不再阻断账号或等待人工审核；不能宣称与原生每次 Responses 请求的计量时点相同。 |
| 能力范围 | 目前仅 Responses；旧版 /responses/compact、store:true、强制工具选择、动态工具 strict 语义、tool_search、非严格/带额外描述的输出 schema、非缺省 service_tier 等仍无法完整映射；严格 JSON Schema 子集已映射 outputSchema，custom 工具映射为单字符串输入的动态工具。请求/图片/会话资源限额继续存在。 |

这份复核不构成“与原生完全等价”的声明。真实 CLI 本地采集、替身回放、数据库/Redis回归与真实上游验收分别记录；本轮使用本机当前账号运行真实 Cloud 图片、加密历史、托管搜索与生图；未部署或验收线上 #40 账号。

### 续接、请求形态与可观测性修复（2026-10-24 第二轮）

依据：同一提交 `bc9ea213c` 的两轮源码复核、一次独立代码审查，以及 ChatGPT 桌面版内置 `codex-cli 0.164.0-alpha.11.1` 的真实请求。每项先写失败回归再修改。真实请求在临时 `CODEX_HOME`、回环接收器、假密钥下采集，脱敏后存为 `testdata/cloud_codex_0_164_desktop.json`；没有调用真实 Cloud 上游，没有部署或验收线上 #40 账号。

用户在桌面版开启 Cloud 后看到 `Cloud does not support the supplied stream_options semantics`。真实请求显示，问题不止这一处：

| 真实客户端形态 | 原表现 | 现行行为 |
| --- | --- | --- |
| `reasoning.context: auto / current_turn / all_turns`（Codex Desktop 0.163 起声明既往推理的渲染方式） | 整单 400 | 接纳三个官方取值。Cloud 线程由 App Server 按模型默认渲染既往推理，与 `auto` 相同，没有逐轮开关可转发；其他取值仍返回明确 400 |
| `stream_options.reasoning_summary_delivery: "sequential_cutoff"`（并发推理摘要特性） | 整单 400 | 接纳。该模式只接收完整摘要，Cloud 本就不发增量；推理项完成时按原生顺序补发 `reasoning_summary_part.added/text.delta/text.done/part.done`，否则此模式下界面看不到摘要 |
| `include: ["output.encrypted_content"]`（输出 token 回放） | 整单 400 | 接纳。Cloud 不产生输出密文，按可选字段缺省，与原生没有密文时一致；历史里助手消息与调用上的 `encrypted_content` 原样透传 |
| 消息与推理项上的 `internal_chat_message_metadata_passthrough` | 整单 400 | 归属元数据，接纳并校验大小，但不注入 Cloud 线程（2026-10-24 生产：新会话开场消息带它时 Cloud 在注入阶段丢弃线程）；最后一条用户消息无对应 `turn/start` 字段；历史比对忽略 |
| 助手 `output_text` 上回放的 `logprobs` | 整单 400 | 接纳 null 或数组，作为输出遥测丢弃，不注入；历史比对不受影响；其他类型或用户消息上仍拒绝 |
| `tool_search`（客户端执行，一轮中途加载工具） | 整单 400 | App Server 动态工具只在 `thread/start` 注册，无追加方法，无法忠实映射。改为从目录侧解决：Cloud 账号承载的模型在 Codex 目录中声明 `supports_search_tool:false`，客户端随之不发 `tool_search`，原本延迟的多智能体、MCP 等工具直接以 function/namespace 下发 |
| Responses Lite（`additional_tools` 输入项、`reasoning.context`） | 整单 400 | 同上，目录声明 `use_responses_lite:false`、`tool_mode:null`，客户端使用标准 Responses 形态 |
| 多智能体加密消息、服务档位 | 不可用 | 目录声明 `multi_agent_version`/`multi_agent_reasoning_effort` 为 null（与 BPS 一致）、`service_tiers:[]` |
| `web_search` 的 `external_web_access`/`indexed_web_access`/`search_content_types` | 整单 400 | 映射为线程配置 `web_search` 的 cached/indexed/live；此前网关固定写 live，放行即会把 cached 悄悄放宽为实时搜索。`filters`、`user_location`、`search_context_size` 继续明确拒绝 |
| `custom` 工具（apply_patch 语法工具） | 整单 400 | 映射为只有字符串 `input` 的动态函数，语法写入描述（上游不能强制语法），对外仍为 `custom_tool_call` 与 `custom_tool_call_input.*` |

目录声明在同组多账号聚合时，不一致字段按既有规则回退为 `false`/`null`，所以混合 Cloud 与原生账号的分组也拿到 Cloud 可承载的形态；原生账号对直接下发的工具同样可用。客户端会缓存目录，切换 Cloud 后需刷新目录（重启客户端）才会改变请求形态。用本网关为 Cloud 账号生成的 gpt-6-astra 目录驱动桌面版 Codex，抓到的请求已不含 `tool_search`、`additional_tools` 与托管 `web_search`；该请求在 HTTP JSON、SSE、客户端 WS 下完成工具往返并一次结算。

| 问题 | 现行行为 |
| --- | --- |
| WS 帧重复键 | Cloud 账号的 WS 帧与 HTTP 一样，在任何字段读取前拒绝重复键。顶层 `model`/`type`/`previous_response_id` 原已被通用安全校验拦截；`reasoning` 及嵌套字段此前可令计费值（首个）与执行值（末个）不同 |
| 续接路由 | 只有 Cloud `previous_response_id`、仍在运行或等待中的工具调用、以及以工具结果结尾的回放固定会话；终态会话或过期引用一律视为历史。previous 失效返回 400 `previous_response_not_found`（WS 为单轮 error 事件并保留连接）；会话已不存在的工具回传返回 `cloud_tool_call_expired` |
| 完整历史复用 | `store:false` 客户端的完整历史按最近的 `call_cloud_`/`msg_cloud_`/`ws_cloud_`/`ig_cloud_` 找同一归属、租约有效的已完成线程；历史须与线程**完全一致**（编辑、重新生成都另开线程），线程设置与推理参数一致才续写，否则 owner 证明未开始后以同一结算根另开线程并注入完整历史。复用不继承旧线程设置。只有存储明确拒绝入队才视为未开始，传输层错误不作为证明 |
| 托管输出回填 | 网关发布的 `web_search_call`/`image_generation_call` 可作为下一轮历史：原线程存活时按已发布内容核对（可省略、可与消息交错，按集合比较），另开线程时作为已完成项注入；生图结果先按发布时的 PNG 校验；不重新执行，不重复计费；非本网关发布的搜索/生图记录（原生/BPS 时期）不注入、从线程历史省略；冒用本网关 ID 但内容不符、未完成或带私有字段的项仍拒绝 |
| 命令队列 | 上限只约束未消费命令；取消/关闭命令有独立余量，长工具轮次不再在第 65 条命令后失败 |
| 取消传播 | 除「已交付结果」或「owner 证明未开始」外，所有退出路径都向 owner 发取消；对已结束请求为空操作 |
| 中性请求字段 | 接纳并丢弃 `user`、`safety_identifier`、`stream_options.include_obfuscation`、`service_tier: auto/default`、`truncation: disabled`、`background: false`、`top_logprobs: 0`；`reasoning.effort` 增加 `none`/`minimal` |
| 工具结果后的追加消息 | 本轮待回复工具的输出之后出现的 user/developer/system 消息返回 `cloud_mid_turn_input_unsupported`；助手消息和早先已提交工具后的消息仍是正常历史；`turn/steer` 映射未经 Cloud host 实测，未启用 |
| 图片数量 | 上限按单条消息/工具输出计，完整历史重发旧图不再耗尽 |
| 连接与服务端请求 | 不带 `threadId` 的服务端请求属于本连接，立即以 -32601 拒绝且不结束本轮；他人线程的请求仍不应答；上游连接每 30 秒 ping，15 秒无应答按连接丢失处理 |
| 用量字段 | 按 App Server `TokenUsageBreakdown`：缺失的 `cacheWriteInputTokens` 依协议默认为 0，显式 null 仍拒绝；解析 `reasoningOutputTokens` 并输出 `output_tokens_details.reasoning_tokens`，不单独计费 |
| 延迟账 | 网关在开启结算根的请求写完后按根记录入口计时（首个测量为准），worker 结算时合并；新结算最多宽限 2 秒等待该测量，未测得时保持缺失 |
| 结算日志 | 恢复扫描、读队列、领取、结算、归档、ACK 失败按阶段与安全类别限频记录，只写类别和 Go 错误类型，不写原始错误文本 |

### Redis 会话记录只保存 Go 原始字节（2026-10-24 生产故障）

上线 `861021ace` 后，生产每一轮 Cloud 请求都在第一次状态写入之后失败，客户端收到 `stream disconnected before completion: Upstream request failed`，服务端只记到 `Cloud terminal state could not be persisted`，Redis 中 11 条会话全部由恢复流程改为 `unknown`、无线程 ID。根因在存储层：保存、续租、恢复认领和释放脚本都用 Lua `cjson` 解码后重新编码整条会话记录。Redis 的 cjson 只保留 14 位有效数字、把空数组 `[]` 编成 `{}`、把超过 2^53 的整数写成浮点，生产 Redis 8.10 实测 `7.500000000000001e-05 → 7.5e-05`、`[] → {}`。第一次保存把记录改写后，账务快照与 actor 自身的精确字节不再相等，owner 栏栅把之后每一次保存（线程绑定、终态、结算）都判成 `-2`，于是终态写不进去、账务事实只能靠恢复归档为未知。单测用的 miniredis 以 Go 实现 cjson、不丢精度，所以没有暴露。

现在记录只以 Go 产生的原始字节写入；heartbeat 续租、恢复认领会改动的 `owner_token`、`owner_instance`、`lease_until`、`expires_at` 放在旁路哈希 `meta:<session>`，读取时覆盖到记录上，释放把已完成记录在 Go 侧改为 `closed` 后原样写回；Lua 只解码比较，不再编码任何记录。旧格式记录没有旁路哈希时沿用内嵌字段。回归分三层：仓储层核对续租、认领、释放后的字节逐一相等并且 actor 自身字节仍能通过栏栅；`service_test` 外部测试包用真实仓储实现驱动 actor，覆盖完成、中断、thread/start 拒绝、连接丢失四种终态都能落库结算；两者都可通过 `SUB2API_CLOUD_TEST_REDIS=host:port` 指向真实 Redis（库 15，会被清空）运行，本地 `redis:8-alpine` 已复现故障并验证修复。

### 迁移会话与 Codex 回放形态（2026-10-24 第三次生产故障）

`7aa1d21c8` 上线后仍无一轮 Cloud 成功，生产错误分三类：账号 43 一个在 BPS 上跑了 5,932 次请求的 HTTP 会话切到 Cloud 后，6 次全部在 `thread/inject_items` 收到 `-32600 unknown thread`（线程已建、turn 未启动，零消耗）；账号 44 的 Codex 0.164 WS 会话 2 次报 `input.tools`（历史里有原生时期的 `tool_search_output`）；账号 4 有 1 次 `tool_search`，发生在部署后 35 秒，属于客户端模型目录缓存（Codex 目录缓存 300 秒）未刷新。

为避免再逐个暴露，本轮用 ChatGPT 桌面版内置 Codex 0.164 对本机假服务端（返回网关为 Cloud 账号生成的目录）跑了两轮真实会话和一轮工具调用，抓到实际回放报文（脱敏后存为 `testdata/cloud_codex_0_164_history.json`）。结果显示还有一个更普遍的问题：Codex 回放推理项时带 `"content": null`（上游 issue #47733），每个回放项都带 `internal_chat_message_metadata_passthrough`，apply_patch 以 `custom_tool_call` 回放；Cloud 解析器对这三点整单拒绝，等于任何 HTTP 会话的第二轮都必然失败，只是生产上还没有一轮走到第二轮。

| 情况 | 原行为 | 现行为 |
| --- | --- | --- |
| 推理项 `content: null` 或 `[]` | 400 `encrypted history.content` | 接纳并去掉该空字段；含原始推理文本仍拒绝 |
| 回放项上的归属元数据（搜索、生图、custom 工具调用及结果） | 400 | 校验大小后接纳，不注入 |
| `custom_tool_call` 的 `namespace`、`custom_tool_call_output` 的 `name` | 400 | 接纳，随降级后的 function 形式校验 |
| `tool_search_call` / `tool_search_output` 历史 | 400 `input.tools` | 从 Cloud 线程历史省略（同 BPS）；当前请求的工具已直接声明 |
| 非 Cloud 执行的 `web_search_call` / `image_generation_call`（原生/BPS 时期） | 400 | 从线程历史省略，不冒充 Cloud 自身结果；其后的回答仍在历史中 |
| 注入阶段线程被丢弃（尚未推理） | 502 `cloud_thread_unavailable`，会话永久不可用 | 第一次重试去掉推理密文（推理项整体省略，其他项去掉 `encrypted_content` 保留明文）；仍被拒则把压缩摘要换成一条说明“前文摘要已省略”的 developer 消息再试；仍被拒返回 400 `cloud_history_rejected`。每次重试新建线程，最多三次 `thread/start`，零消耗结算 |

去除规则与原生 HTTP/WS、BPS 共用同一实现（`openai_encrypted_history_recovery.go`）。重试只在注入阶段发生，此时 turn 未启动，不存在重复执行或重复计费；线程记录保留客户端原样历史，所以后续完整历史请求仍与该线程完全一致、直接续写，不会每轮重新注入。去掉的内容模型看不到（推理与压缩摘要本就不是客户端可见内容），这是用户 2026-10-24 在“按原生规则报错、旧会话必须新开”和“推理前去掉读不了的密文继续”之间选择的后者。日志只记录历史形态（各类型条数、带密文条数）、根和账号，不记录内容或密文。

### 新会话开场注入被丢弃（2026-10-24 第四次生产故障）

`df71abdd5` 上线后又出现两类报错。账号 43 的 Codex Desktop 0.163 回放助手文本时带 `logprobs`，被当作不支持的语义整单 400。账号 4 的 Codex Desktop 0.164 是一个**刚新建的会话**（本机归档记录显示首轮只有开场的 developer/user 消息），9 次请求都在第一次注入时被丢弃线程，会话记录序号停在 3（只建过一次线程），说明历史里没有任何可去除的密文，最终返回 `cloud_history_rejected`。这些开场消息与以前在真实 Cloud 上验证过的注入相比，唯一的新增是 0.164 给每条消息附带的归属元数据（`turn_id` 指向客户端自己的轮次）。本机 `~/.codex/auth.json` 的账号对 Cloud 握手返回 401，无法做真实注入探测，所以这一结论是依据生产形态与既有实测的推断，尚未在真实 Cloud 上复现。

修复：归属元数据接纳但不注入任何条目；助手文本的 `logprobs` 接纳后丢弃。注入被拒的两条日志改为 WARN 级（生产日志级别会过滤 INFO，之前这两条看不到），并列出每种条目的字段组合（只有类型和字段名，不含内容、ID 和密文），下次若仍被拒可以直接看出是哪一类条目。

### 中断与失败轮次的结算（2026-10-24 决定）

原生网关对上游上报的用量照收：客户端中断得到的 `response.incomplete`、上游失败的 `response.failed` 都带本次响应的完整用量；只有流在终态前断掉、拿不到用量时才整次免掉，不估算。Cloud 一轮可含多次采样，`thread/tokenUsage/updated` 是每次采样完成后的累计值；中断时被丢弃的只是进行中的那次采样，之前每次完成采样的用量都是上游实际上报的数字。原规则把整轮中断记为未知并一分不收，一轮已完成十次工具往返后最后一刻中断，前面十次的消耗也全免，比原生少收且可被利用。

| 情形 | 结算 | 标记 |
| --- | --- | --- |
| `turn/completed` 状态为 `interrupted`/`failed` 且已收到用量事件 | 按已测累计用量沿冻结价格与原子账本结算一次 | 结算事实 `lower_bound: true`；用量记录 `billing_status = lower_bound`（零费用时同样保留该标记，不记为 `free`） |
| `turn/completed` 为 `interrupted`/`failed` 但没有任何用量事件 | 未知，不扣费 | `unknown` |
| 推理前明确拒绝（参数、授权、额度、容量）且无任何输出 | 零用量，释放预留 | 既有规则不变 |
| 连接或路由丢失、owner 失联、中断未得到 `turn/completed` 确认 | 未知，不扣费，保留最后水位 | `unknown`；恢复流程不据水位补扣 |

下界结算只影响账务：`UsageCredible` 仍为假，健康反馈、探针、完整结果重放和恢复流程继续把该轮当作未确认，不把中断算成账号失败或成功样本。`lower_bound` 与 `completed` 互斥，worker 遇到矛盾事实归档为 `invalid_settlement`，不扣费。编辑或重新生成另开线程，属于新的一轮，与被中断轮次的用量不重叠。

### 客户端触发的压缩（2026-10-24）

Codex 按自己统计的上下文用量（最近一次响应的 `total_tokens` 加之后新增项的估算）在接近目录阈值时发起压缩；网关为 Cloud 账号生成的 gpt-6-astra 目录声明 105 万上下文，阈值即 90%。此前 Cloud 对这一步直接返回 400，很长的会话因此停在压缩处。实际报错是 `cloud_unsupported_parameter`（“input item type”），并非文档原写的 `cloud_unsupported_endpoint`：旧检查读取顶层 `compaction_trigger` 字段，从未匹配真实请求。

**取证。** 用 ChatGPT 桌面版内置 `codex-cli 0.164.0-alpha.11.1`，在临时 `CODEX_HOME`、provider 名为 `OpenAI`、`model_catalog_url` 返回网关 Cloud 目录、`model_auto_compact_token_limit = 1000` 的条件下，对本机回环接收器采集 HTTP 与 WebSocket 的轮前、轮中压缩，以及同一 `codex app-server` 进程内两轮形成的热 WebSocket 会话。脱敏夹具为 `testdata/cloud_codex_0_164_compaction.json`，未调用真实上游。对照官方源码 `rust-v0.164.0-alpha.11`（`core/src/compact_remote_v2.rs`、`model-provider/src/capabilities.rs`）：名为 OpenAI 的 provider 在 0.159.3 与 0.164 中都只走远程压缩 v2，不再发旧版 `/responses/compact` 请求（0.150 仍有该路径）。

| 形态 | 客户端实际请求 | 客户端要求的响应 / 后续请求 |
| --- | --- | --- |
| 轮前，HTTP | 完整历史 + 末尾 `{"type":"compaction_trigger"}`，`stream:true` | 恰好一个 `compaction` 输出项及 `response.completed`；下一请求为保留的用户消息、重新生成的环境上下文、该 `compaction` 项与新用户消息 |
| 轮前，热 WebSocket | `previous_response_id` 为上一响应，输入只有触发项 | 同上 |
| 轮中，HTTP | 完整历史含待回复的 `function_call` 及其 `function_call_output`，再加触发项 | 同上；随后以 `compaction` 结尾的压缩历史续写本轮，无新用户消息 |
| 轮中，WebSocket | `previous_response_id` 为发布 `function_call` 的响应，输入为工具结果 + 触发项 | 同上 |

客户端回放 `compaction` 项时只保留 `type`、`id`、`encrypted_content`。

**方案取舍。**

| 方案 | 结论 |
| --- | --- |
| 映射为 App Server `thread/compact/start` | 未采用。压缩结果只留在 App Server 线程内，`ContextCompaction` 事件只有 id，拿不到可交给客户端的内容；Cloud 线程空闲 15 分钟释放，之后压缩后的历史无从恢复；对待回复工具的线程会先中止本轮；Cloud host 上未经实测 |
| 调整目录自动压缩阈值，让线程自管 | 未采用。客户端每次请求仍发送完整历史，会话越长越会撞上 4 MiB 请求和会话保留上限；线程丢失后重注入的历史会超出上下文；手动 `/compact` 仍会失败 |
| 在 Cloud 线程上运行摘要轮（采用） | 与 Codex 对非 OpenAI provider 的本地压缩、以及上游已有的 Grok 压缩模拟一致：用 Codex 原文压缩提示词请模型写交接摘要，摘要放进 `compaction` 项，之后由网关以 Codex 本地压缩安装摘要的同一条用户消息（摘要前缀、换行、摘要）注入新线程。客户端历史保持有界；线程释放、换实例都不影响后续续接；只用已验证的 thread/start、inject_items、turn/start、工具回复与 turn/interrupt |

代价：摘要是模型按提示词写出的交接说明，不等于 OpenAI 服务端压缩的加密状态，细节保真度相应下降；保留的原文用户消息由客户端自行挑选（约 64k token 预算）。

**现行行为。**

| 情形 | 处理 |
| --- | --- |
| 触发项 | 只接受输入末尾一个只有 `type` 的触发项；位置不对、重复、带其他字段或前面没有任何历史（且无 previous）均返回明确 400 |
| 轮前，线程存活且历史完全一致（HTTP），或 previous 指向已完成轮（WS） | 在原线程上开一个新用户轮，输入为 Codex 压缩提示词；沿用原线程缓存，按普通新轮新开结算根 |
| 轮前，线程已释放或历史不一致 | 按完整历史另开线程注入后运行同样的摘要轮 |
| 轮中（线程在等待客户端工具） | App Server 没有经验证的方式给等待工具的轮次追加输入。网关不回复待处理工具 RPC，先 `turn/interrupt` 并等到该轮 `turn/completed`，再在同一线程开摘要轮，客户端给出的工具结果连同调用名与参数作为输入；结算根不变，用量从原轮基线累计，两个上游轮次只结算一次 |
| 摘要轮中的工具调用 | Codex 本地压缩不提供工具；摘要轮的客户端工具调用由网关以失败结果回复并提示只写摘要，超过 3 次即结束请求；托管搜索等仍按实际结果计费 |
| 响应 | 摘要轮的推理与消息照常发布，最后追加一个 `compaction` 项（`cmp_cloud_` 前缀，`encrypted_content` 为 `sub2api.cloud.summary.v1.` 加 base64url 摘要）再发 `response.completed`；客户端只安装该项。HTTP JSON/SSE、客户端 WS 桥接一致 |
| 摘要为空 | 返回 `cloud_compaction_failed`（502），已测用量仍结算 |
| 之后的请求 | 本网关签发的 `compaction` 项注入为上述用户消息，线程记录保留客户端原样项，后续完整历史请求完全一致、直接续写；位于末尾时（轮中压缩后的续写）摘要即本轮输入。其他来源的 `compaction` 项仍作为不透明密文处理 |
| 旧版 `/responses/compact` | 仍返回 `cloud_unsupported_endpoint`：没有在用的 Codex 版本可供采集其服务端保留语义 |

摘要以用户权限注入，伪造信封不比直接发送用户消息多出任何权限，因此信封不加密、不签名。HTTP 入口识别出的远程压缩在 Cloud 用量记录上同样标记 `native_compaction_v2`，与原生一致；原生 WS 本来不标记，Cloud WS 保持相同口径。

**证据层级。**

- 源码核对：Codex `rust-v0.164.0-alpha.11` 的压缩选择、v2 收集与安装逻辑，以及 App Server `thread/compact/start` 实现。
- 本地服务回归：真实采集请求驱动轮前 HTTP 与热 WS、轮中 HTTP 与 WS、拒绝工具调用、空摘要、触发项形态、SSE 与客户端 WS 桥接只出一个压缩项、用量标记；修改前全部失败；`-race` 通过。
- 真实客户端对本地网关：桌面版 Codex 0.164 经 HTTP/SSE 调用以网关服务代码运行的本地服务，后接脚本化 App Server。轮前压缩连续两次、轮中压缩（实际执行 printf、网关中止等待轮、带工具结果摘要、同根结算）后均打印 `context compacted` 并继续对话，客户端退出码 0。该层未经过 handler 鉴权与 WS 入口。
- 未执行：真实 Cloud host 上的摘要轮、等待工具时的 `turn/interrupt` 加同线程新轮，以及线上 #40 账号验收。

### 响应用量与客户端上下文估算（2026-10-24 决定）

Codex 把每个响应的 usage 当作一次采样：`total_tokens` 是它判断当前上下文、决定何时压缩的依据（最近一次响应的值加之后新增项的估算，达到目录窗口的 90% 或 95% 硬上限即压缩），各响应之和是它显示的已用 token。原生一次请求就是一次采样，两种读法一致。Cloud 一个结算根下的一轮含多次采样；此前工具等待响应 usage 为 null，最终响应给出整轮之和，而整轮之和随采样次数近乎平方增长，客户端因此远早于原生压缩，多付摘要轮并丢失细节。

App Server（`rust-v0.164.0-alpha.11`）在采样输出工具调用时立即派发 `item/tool/call`，该次采样的 `thread/tokenUsage/updated` 却要等这次采样的全部客户端工具返回才发出，所以网关发布工具等待响应时，产生该调用的采样用量还不可知，最早在下一段报告。`rawResponse/completed` 带单次精确用量，但属内部通知且同样晚于派发，真实 Cloud host 是否发送未验证；`last` 字段在压缩后变成只有总数的估算。网关只用已验证的累计 `total`：相邻两次不同的累计值之差就是一次采样。

| 响应 | 公开 `usage` |
| --- | --- |
| 工具等待响应 | 自上一响应发布以来确认的最近一次采样；没有新采样时为 null（如参考事件顺序下一轮的首个等待段、同一采样拆开的串行工具段） |
| 最终响应（完成、失败或中断） | 本轮最后一次采样；本轮 App Server 自行压缩过线程（出现 `contextCompaction`）时保留整轮用量，让客户端随后压缩自己仍完整的历史 |
| 推理前明确拒绝 | null（不变） |

账务不变：结算根仍按整轮可信用量一次结算，`lower_bound`/`unknown` 规则、用量记录，以及健康反馈、探针和恢复所用的内部结果仍是整轮用量；幂等重放返回当时发布的同一响应。按响应累加 usage 的客户端每个工具轮比结算少一次采样（最终响应前那次）。结算丢失时的恢复校验相应改为要求公开用量合法且不超过结算用量，否则多采样轮次会被判为未知而不扣费。

| 方案 | 结论 |
| --- | --- |
| 最终响应只报最后一次采样，等待响应仍为 null | 未采用：轮中客户端看不到上下文增长，客户端累计只有结算的约一成 |
| 按段分摊且各段之和等于结算 | 未采用：最终响应约为两次采样，真实上下文过半即提前压缩 |
| 等 `rawResponse/completed` 精确逐段归属 | 暂不采用：依赖未经真实 host 验证的内部通知，需持段等待并超时回退 |
| 调高目录阈值 | 不可行：阈值不超过窗口的 90%，95% 硬上限照样生效，而偏差随采样次数成倍增长 |

**证据。** 桌面版 Codex 0.164 经 HTTP/SSE 调用以网关服务代码运行的本地服务，后接按参考事件顺序编写的脚本化 App Server（起始上下文 60,000、每次采样输出 600、每个工具结果 2,000，目录窗口 1,050,000）。修改前，一轮 12 次工具往返报 991,120（真实上下文 91,840），下一轮开始即压缩；从约 6,700 起 27 次工具往返报 1,186,640（真实 77,480），再次压缩；同场景的原生替身不压缩。修改后四轮均不压缩：客户端上下文估算在轮中落后一次采样、轮末与真实一致，已用 token 比原生少约 5%–9%，四个结算根仍各按整轮用量结算一次。本地回归覆盖滞后一段、重复累计值、串行拆分、跨轮基线、线程自压缩、轮中压缩、WS/JSON/SSE 往返与恢复校验，修改前失败。真实 Cloud host 与线上账号未验证。


### 请求体上限对齐原生：去掉 Redis 命令封装的天花板（2026-10-24）

用户要求 Cloud 的图片尺寸、单图大小与请求体大小与原生 Codex 路径一致。此前一轮（main `93b06143`）已去掉网关自设的单图 3 MiB、每消息 8 张和解析层 4 MiB 检查，但每个 Cloud 请求（包括新会话的第一个）都以 Redis 流命令送达会话 owner，命令含完整请求体与账单快照，封装上限 4 MiB + 512 KiB，且 `json.Marshal` 对 `RawMessage` 做 HTML 转义，`<`、`>`、`&` 各放大 6 倍；`cloudCommandFits` 预检超出即返回 413。官方 Codex `store:false` 每轮重发全部历史图片（每图最长边 2048 px），两张截图加文本就可能越过 4 MiB，Cloud 因此明显低于原生（原生只受 `gateway.max_body_size` 256 MiB）。

| 方案 | 做法 | 结论 |
| --- | --- | --- |
| A：命令单独放大上限 | 给命令独立常量（受 16 MiB 出站单帧与 Redis 内存约束），重算命令队列预算，关闭 HTML 转义或改存 base64 | 未单独采用：单实例生产每个请求仍要把整份请求体写入 Redis 再由同一进程读回（两次拷贝、一次 Lua 执行），上限只是从 4.5 MiB 抬到 16 MiB，仍不是“只受入站上限”；最坏内存 128 会话 × 16 MiB 不变，但放大后 Lua `cjson.decode` 整条命令会阻塞 Redis 数百毫秒，必须同时改脚本 |
| B：本实例 owner 进程内直达 | owner actor 在本进程时，请求交给 actor 的有界收件箱，不经 Redis；只有跨实例才写 Redis 命令 | 采用：新会话的 owner 一定是本实例，所以新会话请求与原生一样只受 `gateway.max_body_size`；续接在 owner 实例时同样如此。生产为单实例，覆盖全部请求 |

最终实现为 B，加上跨实例路径的有界放大（A 中与 Redis 路径相关的部分）：

- **进程内收件箱。** `cloudSessionActor.inbox` 容量 64 + 16 条，与 Redis 命令流同一上限：业务命令满 64 条拒绝，取消/关闭保留 16 条；actor 退出后拒绝而不阻塞。owner 的 Redis 命令读取协程把跨实例命令喂进同一收件箱，处理顺序仍是一条序列。收件箱只持有请求体引用（请求体本就在 HTTP/WS 处理协程内存中），不复制。`OpenAICloudSessions.enqueue` 先查本实例 actor 表，查不到才写 store；取消与关闭走同一入口，`evictLocalIdle` 的关闭因此也不再经 Redis。
- **语义不变。** owner 判定以本进程 actor 表为准（它就是持有 socket 的进程），actor 仍按记录比较命令归属、检查 `Deadline` 与取消 tombstone（`CloudRequestClosed`），取消仍先写 tombstone 再排队中断；`Run` 只在确认可送达之后才置 `mayHaveStarted`，拒绝仍是带未启动证明的 413，不换号、不重放；新会话预留发生在本地 actor 创建时，不存在“预留后发现送不出”的情况。本地与 Redis 两条路径的 `ErrOpenAICloudCommandRejected` 都只表示命令确定未入队（owner 消失、队列满或无法存储）。
- **跨实例命令上限 16 MiB。** `OpenAICloudMaxCommandBytes` = 每会话未消费命令预算（`OpenAICloudMaxEventStreamBytes`，16 MiB）= owner 的单个出站 WS 帧上限。命令以 `OpenAICloudEncodeCommand`（紧凑 JSON、不做 HTML 转义）编码，预检与存储同一编码，所以编码后不大于请求体加快照加少量固定字段，只在接近上限时精确测量。Lua 脚本改为只解码单独传入的 owner 参数做归属比较，不再解码整条命令。读取侧对命令流按 16 MiB、对事件流仍按 4.5 MiB 校验。
- **预算与最坏内存。** 每会话未消费命令仍为 16 MiB + 1 MiB 控制预留、每请求事件流仍为 16 MiB / 8192 条、会话记录与结算仍 4.5 MiB，128 个活会话的最坏 Redis 占用与改动前相同（约 2.1 GiB 未消费命令，前提是所有 owner 同时停读）；变化只是单条命令可用满该预算。本实例 actor 上限 16 个，每个收件箱最多 80 条引用。
- **预检位置。** `previous_response_id`/待回复工具续接且 owner 在另一实例、命令超出 16 MiB 时，在派发前返回 413 `request_too_large`；完整历史命中另一实例的已完成线程而命令送不过去时，不再尝试复用，直接按“历史分叉”同样的路径在本实例新开线程注入完整历史。owner 在本实例时不预检。

改动后 Cloud 请求体的实际天花板：本实例 owner 下为 `gateway.max_body_size`；仍保留的两项 Cloud 自身限制是每个 actor 16 MiB 的会话保留量（新线程计入请求中的历史与输入，续写只计新消息、工具结果与已发布响应）和 16 MiB 的单个上游 WS 帧（`thread/inject_items` 整段历史一帧、`turn/start` 一帧，`send` 超限报 `Cloud RPC payload exceeded its limit`）。真实 codex-cloud-backend 对大帧的接受上限无法从源码确认，本轮没有真实上游请求，这一点未验证；本机 Codex 登录没有 Cloud 权限。

**证据。** 本地替身回归：8 MiB 以上内联图片的新会话请求经完整 HTTP 入口到达本实例 actor，`turn/start` 帧原样携带图片且会话命令流为空；owner 在另一实例时 8 MiB 续接经 Redis 命令送达并完成、超过 16 MiB 的续接在派发前返回未启动 413 且 owner 没有收到任何上游工作、同一会话回到 owner 实例后继续；本地收件箱 64 + 16 条边界；仓储层 6 MiB 含 `<`/`&` 的命令在 miniredis 中字节一致写读、归属栏栅仍生效、超过 16 MiB 拒绝。

仍待决定或验收：

- **逐段精确用量。** 公开 usage 目前滞后一段；若真实 Cloud host 确认发送 `rawResponse/completed`，可改为与原生逐响应一致，需另行验证与决定。
- **压缩摘要跨协议。** 关闭 Cloud 或换到原生/BPS 账号后，原生与 BPS 上游读不了本网关的摘要信封，和其他跨协议密文一样需要新开会话；是否在原生/BPS 路径把它展开为用户消息尚未决定。
- **结算归档保留策略。** 归档既是账务事实又是防重复扣款依据，未加 TTL 或压缩；保留期限和到期留存字段待定。
- **推理强度枚举。** 桌面版可选的 `ultra`/`persistent` 等强度不在 Cloud 接纳范围，选择后会得到明确 400；是否在目录中收窄可选强度待定。
- **未经实测的上游能力。** cached/indexed 搜索模式、托管项注入新线程、custom 工具映射、ping 保活均依据官方协议源码与本地替身，尚无真实 Cloud host 证据；线上 #40 账号仍未验收。
- **既有无关失败。** `TestCodexDirectImagesRouting` 在改动前的 HEAD 上同样失败，不属于本轮范围。
- **会话保留量与上游单帧。** 请求体不再受 Redis 命令封装限制后，Cloud 剩余的 16 MiB 会话保留量与 16 MiB 单个上游帧仍低于原生 256 MiB；是否放大保留量（每实例 16 个 actor 的内存上限随之增加）或分批注入历史，需另行决定并在真实 Cloud host 上验证大帧接受情况。
