# OpenAI Cloud 授权与 Responses 网关

日期：2026-10-24。KDAN、TapModels、Tokensavy 共享能力。本文描述当前实现；本机真实 Cloud 协议证据与生产适配器、数据库、前端验证分别记录，不能互相替代。SIWC 使用链路已经移除，Cloud 不读取旧 SIWC 授权或注册记录。

## 使用方式

管理员在普通 OpenAI OAuth 账号的编辑弹窗中授权 Cloud、执行完整验证、选择通过验证的模型，然后保存启用设置。Cloud 与 BPS 互斥，启用时账号仅接收 Cloud 白名单内的 `/v1/responses` HTTP JSON/SSE 与客户端 WebSocket 请求。普通用户沿用 Sub2API API Key、分组、余额和订阅，无须接触上游 token。

授权完成或模型出现在 `model/list` 中都不代表可调度。完整验证覆盖文本、客户端工具往返及跨实例续接、图片输入、加密历史、实时搜索、图片生成、可信用量和结算确认。图片生成验证会消耗上游额度。验证证据绑定授权代次、协议版本、实际代理内容、身份和模型映射；重新授权或这些设置变化后重新验证。选择已验证模型和启用开关不使证据失效。`model/list` 按游标读取全部有界分页，Cloud 模型名不套用原生 Codex 别名。

验证在首个失败步骤停止；失败结果中的能力值不代表后续步骤已经执行。`result.json` 的错误显示能力阶段和检查点（请求、结果证据、结算或会话关闭），并保留安全分类后的上游错误码及状态。管理探针不生成客户账单：空快照和 Redis JSON 往返产生的 `null` 等价，仍必须有可信用量、合法归属及终态后才能确认结算；普通客户请求不能借此绕过计费快照校验。

供应商账号、影子账号、PAT、Agent Identity 和独立 Excel 授权不属于 Cloud 授权范围。管理操作复用现有账号可见范围、`accounts.authorize` 权限和操作期间的权限复核。

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

Cloud 产品入口独立，凭据复用原生 Codex OAuth。真实客户端未证明独立 refresh family，因此 Cloud 私有绑定只保存可信身份、授权代次、能力和运行封锁状态，不复制 AT/RT。原生 `OpenAITokenProvider` 是唯一刷新权威；OAuth 回调原子更新父账号凭据和 Cloud 绑定。

新迁移 `291_openai_cloud_authorization.sql` 建立私有绑定、扩展 `usage_logs.authorization_mode` 为 `cloud`，并允许历史 `siwc` 日志原值继续满足存储约束；运行时不保留 SIWC 类型、标签或分支。此前发布的 290 清理迁移在升级时删除退役授权表、安装资料及账号标记。数据库 trigger 覆盖账号创建、导入、批量更新和直接凭据/extra 写入。正常 token 轮换保持绑定，稳定身份替换撤销绑定并停用 Cloud。只新增迁移文件，本次代码交付不执行生产迁移或删除历史数据。

迁移 `292_openai_cloud_gateway_verification.sql` 将已有 Cloud 停用并清除旧能力，协议版本升级为 `2026-10-24.2`，保留授权身份和原生凭据。升级后必须重新执行完整验证再选择启用；账号代理/模型映射变化或同一代理内容变化也由数据库使证据失效。增加核实对账和未决 root 的审计表；不改写已应用的 290/291。

| 设置 | 规则 |
| --- | --- |
| `extra.openai_cloud` | 默认关闭，必须通过授权与完整能力门槛，且 BPS 关闭 |
| `extra.openai_cloud_models` | 必须显式选择验证通过的模型；空列表不表示全部模型 |

Cloud 共六个管理 API，路径省略既有 `/api/v1` 前缀：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/admin/accounts/:id/cloud` | 脱敏状态和已验证模型 |
| POST | `/admin/accounts/:id/cloud/authorize` | 创建 PKCE/state 一次性授权会话 |
| POST | `/admin/accounts/:id/cloud/callback` | 身份、权限及代次 CAS 校验后保存 |
| DELETE | `/admin/accounts/:id/cloud` | 本地断开绑定并停用，不声称远端已撤销 |
| GET | `/admin/accounts/:id/cloud/unsettled` | 超管读取未决请求及已观察用量下限 |
| POST | `/admin/accounts/:id/cloud/reconcile` | 超管提交核实最终用量、证据、原因和确认，进入原账本 |

设置沿用现有账号更新 API，验真沿用账号测试 API 的 `mode=cloud`。关闭或切换编辑账号时清除回调输入，并防止旧异步请求回写新账号。

## Responses 映射与边界

- 支持 HTTP JSON、SSE 和客户端 Responses WebSocket。WS 复用现有入口、串行读写、权限/配额、取消及 SSE→WS 桥接；`response.create` 保留 Cloud previous/call ID，不展开原生历史或改写 Cloud 模型名。同一 Cloud session 的 HTTP/WS 请求按相同归属与账务规则续接。`/responses/compact` 和 `store:true` 仍明确拒绝，失败不会改走原生或 BPS。
- 普通 Responses 调度接受原生 Responses 或既有 Chat Completions 转换候选，Cloud 本身只支持 Responses；已验证 Cloud 续接可通过原有 OAuth HTTP 限制，其他 OAuth 的限制不放宽。
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

资源上限：全局 128 个活会话、每用户 8 个、每账号 16 个、每实例 16 个 actor；15 分钟会话、20 秒 owner lease、5 秒心跳；单帧 16 MiB、RPC 64 个、事件队列 256 条/32 MiB，Redis 单请求事件流 16 MiB/8192 条。结束请求清理事件流并留 tombstone，防迟到写入重建。容量不足时由原 owner 串行回收已完成的空闲会话；不回收生成中或等待客户端工具的连接。15 分钟是最大续接保留期，压力回收后旧 previous ID 明确失效，不重放。明确容量拒绝释放未开始请求的账务预留，Redis 写入结果不确定时仍保留预留。

活会话记录和未 ACK 账单不靠 TTL 丢弃。owner 失联后由 fenced reaper 接管状态；不尝试重放原待回复工具。已完成结果可恢复其同一结算根；不能确认的执行标记 Unknown，保留最后可信用量并封锁该账号新 Cloud 使用。未知客户账封锁跨重新授权保存，不能靠重新登录消除。失败管理员探针不成为客户欠账或永久封锁。

账号编辑提供超管核实对账：逐 root 提交完整最终 token、缓存、搜索、图片用量与终态，附外部核实证据和原因。已观察值只是下限，不能直接当最终消费或自动补零。SQL 保存不可变审计后，Redis 按完整旧快照和无活动账务租约条件原子替换；原 worker 按冻结价格和 `cloud:<root>` 幂等记账。仅账本成功 ACK 且每个持久阻断 root 都有已结算审计，才清运行阻断和旧能力。之后仍须重新验证、手动保存启用。Redis 丢数据不代表已结清；ACK 后 SQL 确认失败在回执有效期内重试，缺少证据继续封锁。未提供自动上游用量推测或删 key 解锁入口。

结算在发送前冻结原用户、Key、分组、订阅、账号、模型价格、倍率、分时和 reasoning 口径。工具等待时不填零账；最终可信用量才进入既有原子账本，`cloud:<root_id>` 保证幂等，成功后才 ACK。Key 后来删除也继续按冻结归属结算原用户；数据库暂时故障继续重试，不误判删除。

主模型 token 费用保留；托管搜索另按已完成且去重的调用次数，图片按真实尺寸和张数，叠加既有分组/默认按次价格与倍率。两项价格在请求前冻结。未得到图片模型、图片 token 或实付金额时不推测；这些是本系统收费策略，不是上游发票。`wham/usage` 百分比不参与计费。

## 错误和清理

握手 HTTP、JSON-RPC 数字码、已知 `codexErrorInfo` 和已验证的内嵌 JSON 错误信封转换为固定公开错误，区分参数/加密错误、权限、限流/额度、容量及协议故障。原始 message、additionalDetails、请求体和凭据不回显。上游 `willRetry:true` 只表示上游正在重试，网关等待真实终态，不再发一个请求。

明确的参数、加密、授权、额度或容量拒绝，且没有任何模型/工具输出或已观察用量时，视为推理前拒绝：公开 usage 仍为空，释放预留且不收按次费用，不制造未知欠账。单纯缺少 usage、网络中断或已经发生工具/模型工作不能使用该规则。

`turn/completed` 仍须检查失败、中断和缺失用量。非流式保留安全错误及对应 HTTP 状态，SSE 保留失败终态，已发头后的传输故障发送安全 error 事件；不能因为收到 completed 事件就报告成功。JSON/SSE/WS 返回客户端模型名。健康反馈区分可信成功、失败、参数拒绝、取消、工具等待、幂等重放和未知消费，不把等待或失败算成成功。SSE 及 WS 每 15 秒保活，Cloud 接入整轮首输出预算；心跳不计模型输出。线程清理只针对本次创建的线程，完成或失败时尝试 archive；清理不证明未知账已补齐。

本机验证使用用户明确授权的官方 Codex 登录。只读取当前 AT，不输出凭据，不创建第二份 refresh token。验证程序为显式 opt-in；未指定授权文件不会发真实请求。生产部署、迁移、发版仍需独立执行，本轮不包含。

验证分为两层：发布前的调度、HTTP 编码、客户端 WS 工具续接、错误、Redis/PostgreSQL、账务与界面回归验证网关适配；账号管理探针验证该账号/模型的真实上游能力。探针通过不等于线上 API Key、反向代理和负载环境已经验收。本轮修复没有部署或运行生产 #40 的验收测试。
