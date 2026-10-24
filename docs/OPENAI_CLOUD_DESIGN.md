# OpenAI Cloud 授权与 Responses 网关

日期：2026-10-24。KDAN、TapModels、Tokensavy 共享能力。本文描述当前实现；本机真实 Cloud 协议证据与生产适配器、数据库、前端验证分别记录，不能互相替代。SIWC 使用链路已经移除，Cloud 不读取旧 SIWC 授权或注册记录。

## 使用方式

管理员在普通 OpenAI OAuth 账号编辑页直接开启或关闭 Cloud 网关，批量编辑与完整参数编辑也支持只修改选中的 Cloud 开关。无需单独登录、回调或授权 Cloud。原生与 Cloud 共享 Codex OAuth 刷新权威，Excel 授权独立保存；原生与 BPS 授权保持独立，Cloud 开关不更换任何凭据。普通用户沿用 Sub2API API Key、分组、余额和订阅，无须接触上游 token。

Cloud 不再要求完整验证后启用，不维护独立模型白名单；模型权限统一由分组与账号普通模型白名单/映射决定。原生账号符合条件且没有未决消费阻断即可选择 Cloud；模型是否提供具体上游能力由实际请求结果决定。升级、普通代理或模型配置变化不清空授权、不要求重新测试。

“Cloud 兼容性”仅保留在“测试账号连接”的测试模式下，由管理员主动运行。测试覆盖文本、客户端工具往返与跨实例续接、图片输入、加密历史、实时搜索、生图、可信用量和结算确认，会消耗上游额度。测试不保存能力准入状态，也不改变账号启用、模型权限或授权；失败结果中的能力值不代表后续步骤已经执行。`result.json` 保留安全错误码、状态、阶段和检查点，`answer.txt` 保留答案。管理测试不生成客户账单，普通客户请求仍必须满足计费快照和归属校验。

### 多授权下的协议选择

| 现有账号开关 | 新请求使用路径 |
| --- | --- |
| Cloud 与 BPS 均关闭 | 原生 Codex OAuth 路径；HTTP/SSE/WS 遵循原生传输设置 |
| Cloud 关闭、Excel/BPS 开启 | 系统 BPS 开启且匹配既有账号 BPS 模型/模式规则时走 Excel grant；未匹配的请求保持原有原生回退规则 |
| Cloud 网关开启（BPS 设置保留） | 通过普通分组/账号模型权限的 Responses 请求走 Cloud；不按测试结果选模型，也不自动回退原生/BPS |

沿用已有 `openai_cloud` 与 `openai_excel_bps` 开关，不新增三协议选择器或额外模型路由表。Cloud 为账号级优先：开启时压住 BPS，保留 BPS 授权、开关与模型配置；关闭 Cloud 后原生默认，仅当 BPS 全局开关、账号开关均开启且映射后的上游模型精确命中既有 BPS 模型列表时走 BPS。历史账号未配置 BPS 列表时保留原有全模型语义，显式空列表不选任何 BPS 模型。Cloud 失效或请求失败不改变此账号的协议选择；独立请求的推理前恢复遵循下文已有账号调度规则。已建立的 response/tool 会话绑定原账号、授权代次和协议执行状态；不能将 Cloud response ID 或待回复工具迁移到原生/BPS。切换后旧 Cloud 续接明确拒绝，客户端需发起新会话。

供应商账号、影子账号、PAT、Agent Identity 和独立 Excel 授权不属于 Cloud 授权范围。管理操作复用现有账号可见范围、既有账号参数编辑权限和操作期间的权限复核。

迁移 294 取消独立 Cloud 绑定的启用前置条件，保留原生账号资格、身份变化失效与未决账阻断。旧执行绑定及账务资料继续保留。退役 OAuth 会话 `oauth:openai:cloud:*` 无读取入口，按原 30 分钟 TTL 过期；不新增清理服务。

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

Cloud 产品入口独立，凭据复用原生 Codex OAuth。真实客户端未证明独立 refresh family，因此 Cloud 私有绑定只保存可信身份、授权代次和运行封锁状态，不复制 AT/RT。原生 `OpenAITokenProvider` 是唯一刷新权威；首次实际使用时校验原生签名令牌并以 CAS 自动建立执行身份绑定；绑定仅用于会话归属及未决账，不保存或替换原生 AT/RT。并发首次使用读取同一 CAS 胜出绑定。

新迁移 `291_openai_cloud_authorization.sql` 建立私有绑定、扩展 `usage_logs.authorization_mode` 为 `cloud`，并允许历史 `siwc` 日志原值继续满足存储约束；运行时不保留 SIWC 类型、标签或分支。此前发布的 290 清理迁移在升级时删除退役授权表、安装资料及账号标记。数据库 trigger 覆盖账号创建、导入、批量更新和直接凭据/extra 写入。正常 token 轮换保持绑定，稳定身份替换撤销绑定并停用 Cloud。只新增迁移文件，本次代码交付不执行生产迁移或删除历史数据。

历史迁移 291/292/293 保持原字节，避免破坏已应用迁移账本。迁移 293 撤销旧能力准入和代理变化清证据触发器，移除旧能力存储列及 `extra.openai_cloud_models` 字段，保留当前授权、启用选择和未决消费账。此后不再有“升级使能力证据失效、须重新验证”的启用机制；已在旧版本被停用的账号不会被擅自自动启用，可直接在账号编辑启用 Cloud，无需先测试。

| 设置 | 规则 |
| --- | --- |
| `extra.openai_cloud` | 默认关闭；复用原生授权且没有未决消费阻断时可启用，优先于保留的 BPS 设置 |
| 分组模型限制、账号 `credentials.model_mapping` | 沿用原生模型权限及映射；没有独立 Cloud 模型列表 |

Cloud 共三个管理 API，路径省略既有 `/api/v1` 前缀：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/admin/accounts/:id/cloud` | 启用和未决消费状态（不触发登录或远程测试） |
| GET | `/admin/accounts/:id/cloud/unsettled` | 超管读取未决请求及已观察用量下限 |
| POST | `/admin/accounts/:id/cloud/reconcile` | 超管提交核实最终用量、证据、原因和确认，进入原账本 |

设置沿用现有账号更新 API；手工测试沿用账号测试 API 的 `mode=cloud`。账号编辑页没有验证按钮、验证模型列表或测试通过门槛；切换编辑账号时防止旧状态请求回写新账号。界面采用说明文字加右侧开关的单行样式，没有授权卡片。

## Responses 映射与边界

- 支持 HTTP JSON、SSE 和客户端 Responses WebSocket。WS 复用现有入口、串行读写、权限/配额、取消及 SSE→WS 桥接；`response.create` 保留 Cloud previous/call ID，不展开原生历史或改写 Cloud 模型名。同一 Cloud session 的 HTTP/WS 请求按相同归属与账务规则续接。本地 `generate:false` 预热 ID 只从当前连接已缓存的完整输入续接，真实 Cloud ID 仍回原 owner，不展开重放。`/responses/compact` 和 `store:true` 仍明确拒绝，失败不会改走原生或 BPS。
- 接受 Codex HTTP/WS 的 `client_metadata` 字符串映射（最多 64 项、总编码 64 KiB、键最长 128 字节），作为入口客户端附带信息；不外发为 Cloud 身份、认证、工具配置或生成参数，逐轮变化不破坏续接。应用 `metadata` 仍独立保留并回传；错误类型和重复键继续拒绝。
- `prompt_cache_key` 沿用公共入口的缓存亲和用途，接受字符串或 null；不映射为 Cloud thread/owner、工具配置或生成参数，变化不影响工具结果幂等及跨 HTTP/WS 续接。Cloud 没有对应 RPC 缓存键字段，不承诺与原生具有相同缓存命中率。
- HTTP/SSE 与 WS 桥接在同一 Cloud 入口复用原生 legacy `prompt`/`messages` 转换和工具 schema 空 type 修正；转换前检查重复键和请求大小，真实 Cloud `previous_response_id` 始终保留。旧格式工具回传在选号前复用同一转换按 Cloud call ID 固定原 owner，不要求额外携带 previous ID。旧模型的 `reasoning.mode:pro` 沿用原生转换为 max effort，GPT-6 独立 mode 不被静默删去。
- 常用 `reasoning.summary` 映射为 `turn/start.summary`（auto/concise/detailed/none），`text.verbosity` 映射为 `thread/start.config.model_verbosity`（low/medium/high）。沿原有生成设置摘要固定同一会话设置，省略时继承，变化时明确拒绝，不把未生效参数当作成功。映射核对官方 Codex 源码 `7d592f6dd9988e349e3164feaa21d0fc14bbac8a` 的 client.rs、v2/turn.rs 和 Config.model_verbosity；本轮使用协议夹具核验出站字段，未声称生产 Cloud 版本或模型已实测。
- 普通 Responses 调度接受原生 Responses 或既有 Chat Completions 转换候选，Cloud 本身只支持 Responses；已绑定原 owner 的 Cloud 续接可通过原有 OAuth HTTP 限制，其他 OAuth 的限制不放宽。
- 角色历史经 `thread/inject_items` 原生注入，保留 system/developer/user/assistant、assistant phase 和函数历史，不压成一段提示词。
- function/namespace 工具转换为固定私有 `sub2api_client` namespace 的 dynamicTools，并通过 `features.code_mode.direct_only_tool_namespaces` 保持直接函数调用。实测默认 exec 包装无法正确呈现客户端图片，关闭 code_mode 本身也不能改变该行为；不保留有损兜底。私有 namespace 不进入下游名称或公开历史。等待客户端工具时返回 function_call，usage 为 null；收到 function_call_output 后向原 RPC ID 回传结果。文本与经过校验的 PNG 工具截图均保留原类型。
- `reasoning.encrypted_content` 可用于无 `previous_response_id` 的历史回传，密文原样保留，不解密或改写；`store:false` 的输出保留上游密文，接受 legacy `include`。只发布 summary 和密文，不发布 raw reasoning content、内部 metadata 或注入的系统消息。
- 用户图片输入支持已验证的 PNG data URL（省略或 auto detail），同一路径支持客户端工具返回的 PNG。每张解码不超过 3 MiB，每请求最多 8 张、合计 16 MP、单边不超过 8192，仍受 4 MiB 请求限制。远程 URL、`file_id`、其他图片格式和 detail 语义未在此适配器验真，明确拒绝，避免绕过既有图片出口及文件归属保护。
- `{type:"web_search"}` 显式启用实时搜索，返回 `web_search_call` 生命周期、action 和可选 `web_search_call.action.sources`；原始助手引用通过白名单 `url_citation` 输出。
- `{type:"image_generation"}` 显式启用上游托管图片生成，完成必须有通过完整 PNG 解码、尾部和像素上限检查的真实图片与尺寸，转换为 `image_generation_call`。不把成功文本或空 result 当成生成成功。
- Cloud 托管工具的内部 raw custom tool 可能名为 `exec`。它属于上游实现，绝不作为下游待执行的 function/custom call 返回。托管工具与客户端工具可交错完成：上游完成结果在 HTTP 工具等待间隙有界缓冲，下一段响应补齐完整公开生命周期，整轮按上游 ID 去重计费。托管工具继续对话使用 `previous_response_id`，保留原线程上下文；不能仅凭公开 action/revised_prompt 重建内部执行历史。当前拒绝脱离会话的托管工具历史重放。
- 仅接受已验证参数；未验证的工具选项、强制 tool_choice、strict grammar/schema、音频/视频、结构化输出等明确报错，不静默丢弃。`tool_choice:none` 关闭全部工具。

加密历史失效保留 `invalid_encrypted_content` 错误；不删除密文并重放。托管工具与函数执行后均不自动换账号、改授权方式或重放。

## 会话、恢复与结算

每个活跃会话由一个进程持有原始 WebSocket。Redis 保存隔离到 user/API Key/group/account/grant/model/settings 的归属、命令与事件流。跨实例请求通过命令流回到原 owner；Redis 记录不是可以转移的 socket。工具调用 ID 和 response ID 不能跨 API Key 使用。

重复工具结果按规范化内容幂等；内容冲突拒绝。JSON 重试返回同一结果，SSE 重试重建完整公开生命周期，但不执行第二次工具或写第二笔账。同一完成线程的新用户 turn 使用新计费根，并在发起前冻结实际继承的 reasoning 设置。

资源上限：全局 128 个活会话、每用户 8 个、每账号 16 个、每实例 16 个 actor；15 分钟会话、20 秒 owner lease、5 秒心跳；单帧 16 MiB、RPC 64 个、事件队列 256 条/32 MiB，Redis 单请求事件流 16 MiB/8192 条。结束请求清理事件流并留 tombstone，防迟到写入重建。取消先写 tombstone 再排队中断；owner 在连接准备后、创建线程、注入历史、启动 turn 和回传工具前检查请求截止时间与 tombstone，会话 socket 不依赖单次 HTTP context。容量不足时由原 owner 串行回收已完成的空闲会话；不回收生成中或等待客户端工具的连接。15 分钟是最大续接保留期，压力回收后旧 previous ID 明确失效，不重放。明确容量或 owner 派发前拒绝带结构化未启动证明，立即释放该新根的账务预留；Redis 写入结果不确定时仍保留预留。

公开请求/结果仍限制 4 MiB；Redis 完整记录、命令与事件封装统一限制 4.5 MiB，为账务快照和元数据留空间。终态正文（包含 JSON 转义和名称还原）超限时保存精简的失败响应及已确认用量，保留原 root 的可信结算事实。

活会话记录和未 ACK 账单不靠 TTL 丢弃。owner 失联后由 fenced reaper 接管状态；不尝试重放原待回复工具。已完成结果可恢复其同一结算根；不能确认的执行标记 Unknown，保留最后可信用量并封锁该账号新 Cloud 使用。未知客户账的 SQL root、当前授权的运行阻断和停用状态按账号在同一事务保存，不受旧授权代次限制；读取授权和后续授权写入也检查未结清 root，不能靠重新登录或 Redis 状态缺失消除。失败管理员探针不成为客户欠账或永久封锁。

账号编辑提供超管核实对账：逐 root 提交完整最终 token、缓存、搜索、图片用量与终态，附外部核实证据和原因。已观察值只是下限，不能直接当最终消费或自动补零。SQL 保存不可变审计后，Redis 按完整旧快照和无活动账务租约条件原子替换；原 worker 按冻结价格和 `cloud:<root>` 幂等记账。仅账本成功 ACK 且每个持久阻断 root 都有已结算审计，才清运行阻断。之后可手动保存启用，不要求重新测试。Redis 丢数据不代表已结清；ACK 后 SQL 确认失败在回执有效期内重试，缺少证据继续封锁。未提供自动上游用量推测或删 key 解锁入口。

结算在发送前冻结原用户、Key、分组、订阅、账号、模型价格、倍率、分时和 reasoning 口径。工具等待时不填零账；最终可信用量才进入既有原子账本，`cloud:<root_id>` 保证幂等，成功后才 ACK。Key 后来删除也继续按冻结归属结算原用户；数据库暂时故障继续重试，不误判删除。

主模型 token 费用保留；托管搜索另按已完成且去重的调用次数，图片按真实尺寸和张数，叠加既有分组/默认按次价格与倍率。两项价格在请求前冻结。未得到图片模型、图片 token 或实付金额时不推测；这些是本系统收费策略，不是上游发票。`wham/usage` 百分比不参与计费。

## 错误和清理

握手 HTTP、JSON-RPC 数字码、已知 `codexErrorInfo` 和已验证的内嵌 JSON 错误信封转换为固定公开错误，区分参数/加密错误、权限、限流/额度、容量及协议故障。原始 message、additionalDetails、请求体和凭据不回显。上游 `willRetry:true` 只表示上游正在重试，网关等待真实终态，不再发一个请求。

独立新请求的恢复复用现有 `UpstreamFailoverError` 和账号调度循环。授权准备失败、连接/线程准备失败，以及上游明确推理前限流或容量拒绝，可在无已提交输出、无 Cloud 续接归属、输入完整可重放时重试/换号。临时 5xx 遵守账号原有重试开关，最多同号再试一次；额度/授权拒绝直接换号。A 被排除后，B 按自己的 Cloud/BPS/原生设置与普通资格、优先级、容量选出，没有全池协议固定顺序，也不关闭 A 的 Cloud 开关。换号不重置全轮最多 8 次、同 owner 3 次、同目标 2 次及累计重试等待 10 秒等原有上限；授权准备仍受单独的小预算约束。

`response.created` / `response.in_progress` 在真实输出前暂存，`response.failed` 等待 owner 的持久终态证明再决定发送或恢复；正常响应保持事件顺序。内部结果新增 `rejected_before_inference`，沿现有 Redis 结果保存，不进入公开响应、不建表；旧记录缺失该字段视为无证明。只有 owner 明确的未推理事实、可信零用量、对应计费 root 和终态同时成立才允许释放本地预留，原持久结算继续保留。单纯没有 token、断线、超时或上游说将重试都不构成证明。已开始模型/工具工作、输出身份已提交、消费未知及参数/安全策略错误保持终止；Cloud 响应 ID、工具续接继续绑定原账号。失败反馈和本轮排除使用 Cloud 模式，不污染原生 HTTP/WS 或启动 BPS 恢复阶段。


明确的参数、加密、授权、额度或容量拒绝，且没有任何模型/工具输出或已观察用量时，视为推理前拒绝：公开 usage 仍为空，释放预留且不收按次费用，不制造未知欠账。单纯缺少 usage、网络中断或已经发生工具/模型工作不能使用该规则。

`turn/completed` 仍须检查失败、中断和缺失用量。非流式保留安全错误及对应 HTTP 状态，SSE 保留失败终态，已发头后的传输故障发送安全 error 事件；不能因为收到 completed 事件就报告成功。JSON/SSE/WS 返回客户端模型名。健康反馈区分可信成功、失败、参数拒绝、取消、工具等待、幂等重放和未知消费，不把等待或失败算成成功。SSE 及 WS 每 15 秒保活，Cloud 接入整轮首输出预算；心跳和空消息开始事件不计模型输出，复用现有语义输出判定；工具进展和有效输出按原规则结束首输出等待。线程清理只针对本次创建的线程，完成或失败时尝试 archive；清理不证明未知账已补齐。

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
| 延迟账 | 可选安全错误码随原结算保存，cyber请求在对账后仍按既有类型记录；不改变原收费、未决账、零消费证明或幂等语义 |
| 原生专属优化 | 原生 Responses WS池、HTTP zstd及自动后缀裁剪不是Cloud JSON-RPC接口；Cloud沿用原owner长连接和显式续接，不套用原生删ID重放或另造连接池 |

能力边界仍须如实保留：`service_tier` 尚未完成 Cloud 实际档位与收费口径映射；`reasoning.context`、独立 `reasoning.mode`、特殊 `stream_options`、强制串行工具等未支持的执行语义仍明确拒绝。入口保护和桥接复用不意味着 Cloud 的每个模型参数、工具或性能都与原生/BPS相同。所有本轮回归为本地夹具与协议测试，生产 #40 未部署或验收。


## 全过程复盘与再次复查（2026-10-24）

责任在实施与验证方法：此前的多次“已修复/回归通过”描述有给出本地限制，但整体完成表述仍容易让人理解为完整客户端已经可用；服务层小请求和手写工具夹具没有充分证明这件事。不能把用户上线后发现基本入口错误解释成正常验收成本。

| 阶段 | 原问题及事实依据 | 本次纠正的方法 |
| --- | --- | --- |
| 需求理解 | 从原生/BPS参考链路发展出重复Cloud OAuth、验证准入和独立模型列表，后续用户逐项要求取消；撤销SIWC曾需再次明确完整清理范围 | 以最新用户需求确定唯一有效契约，区分工程验证与产品开关；撤销按功能及后续演变清理 |
| 方案设计 | 先证明登录→usage→Cloud RPC最小流程，再补Responses适配；没有先列客户端默认字段、完整工具目录及公共入口的生命周期契约 | 先取得实际客户端请求，再设计适配；原生/BPS按调用链复用，不复制一套入口策略 |
| 开发 | 严格白名单把client_metadata、prompt_cache_key当成不支持语义；另有公共取消、写入保护、错误状态和后续轮设置继承漏接 | 区分客户端附带信息、会话归属、执行控制；每项映射有依据，关键公共机制以实际调用点验证 |
| 复核/测试 | 反复扩充局部测试，却曾缺少跨传输和“完成一轮后再发下一轮”；上一轮summary映射新增了未覆盖的继承交互 | 先复现失败，再测真实请求形态和跨轮生命周期；现有测试通过不是新增交互已覆盖的证据 |
| 交付/上线 | 已推送与已部署、已授权与真实客户端可用未形成同一条可追踪证据；没有线上#40成功验收记录 | 交付说明列源码/本地/真实客户端/上游/线上五层证据，不宣称未执行层通过；部署仍独立授权 |

本轮在不更改用户provider或登录配置的情况下，用本机 `codex-cli 0.159.3` 的 ephemeral、ignore-user-config 与临时loopback provider取得实际HTTP/SSE请求、WS `generate:false`预热及正式 `response.create`。实际请求包含106个function/namespace工具、developer/user多段内容、summary、cache key和client metadata；使用固定模拟响应让客户端退出，未调用真实Cloud、未执行工具。初次WS接收器以HTTP/1.0回复导致客户端fallback，已改为HTTP/1.1并确认实际捕获WS帧；这也验证了必须检查实际传输而非只看配置。脱敏夹具只保留字段/工具schema形态，正文、元数据值和工具描述已去除或替换，不含凭据和请求头。

新增问题回归在修改前实际失败：完成后的第二轮省略reasoning被误拒、取消后额外发502、Flush失败未被识别。修复沿用原有actor全对象继承、WS取消终态归属以及 `GuardSSEWrites`；继承effort只用于账务和首输出期限，不再修改请求成半个reasoning对象。客户端写入失败归因下游、终止派发且不自动重放；已经发生的消费仍由原持久结算处理。

| 再次复查项 | 当前结论与证据边界 |
| --- | --- |
| 原生授权/usage/OAuth刷新/Cloud初始化 | 沿原生TokenProvider和Cloud固定握手；真实上游能力与账号权益仍需线上观察，不以本地替身证明 |
| JSON、SSE、WS入口与预热 | 实际CLI报文用于服务/桥接回归，保留工具规模和多段内容；既有真实WS连接测试仍覆盖入口生命周期，尚非部署环境的完整鉴权路由验收 |
| 函数/namespace、加密历史、PNG、搜索/生图 | 沿现有协议与保护回归；原生未验证参数、非PNG/远程图片、custom grammar工具不伪装支持 |
| 工具等待、完成后新轮、HTTP↔WS | 原owner/Key/group/account绑定、幂等工具结果与新轮单独账根；新增完成后完整reasoning继承回归 |
| 取消、客户端断线、慢写、Flush错误 | HTTP复用原生有界写出；WS复用公共控制与drain；取消不抢发502终态，写失败不成为上游健康故障，不能证明最终用量时仍保留未决账 |
| 首输出、心跳、重试、错误码 | 已接公共预算及非语义心跳计数；推理前证明才允许恢复，输出后和已绑定续接不换号重放；错误保留安全状态/码 |
| 账务、风控、限额与配置变动 | 沿冻结价格与原子账本；未知消费、身份代次和最新客户端限制继续保持；不通过重新授权或测试清除未知账 |
| 原生传输性能优化 | Cloud是独立JSON-RPC长连接，不直接套原生Responses池、HTTP zstd或自动后缀裁剪；已有owner续接可复用连接，但没有生产性能对比证据 |
| 延迟观测 | 原生/BPS已有timing v2。Cloud目前有请求耗时/首文字局部值，但未完整接入同一attempt时间轴和延迟账指标，不宣称观测等价；本轮不扩建新的计时系统 |

当前还不能宣称Cloud与原生/BPS“所有能力完全等价”：`service_tier`、compact、部分工具/图片形态仍是明确边界，生产#40指定版本与原客户端的完整验收尚未执行。工程方法在[交付规范](CHANGE_DELIVERY.md#协议适配的需求与验收证据2026-10-24)中统一维护；不会据复盘重新增加账号验证准入或额外授权。
