# OpenAI Cloud 独立授权与 Responses 网关设计

日期：2026-10-23。适用范围：KDAN、TapModels、Tokensavy 共享能力。

**本轮交付仅包含设计，未实现 Cloud 授权、界面、迁移或网关，也未发起真实 Cloud 请求。** 本文的 API、字段、状态和代码文件均为实施方案，不能视为已经可用的能力。SIWC 使用链路的移除独立执行；Cloud 不继承 SIWC 的授权、注册记录或运行状态。

## 1. 目标与证据边界

在现有账号编辑中提供 Cloud 独立授权和设置，Sub2API 用户继续调用 `/v1/responses`，由服务端处理 Cloud 会话和 Responses 转换。复用现有鉴权、额度、调度、代理和计费；新增一个 Cloud 上游协议适配路径，不建立第二套网关或任务产品。

拟采用用户指定流程：

```text
登录检查
 -> wham/usage
 -> OAuth 刷新（必要时，随后重新检查 usage）
 -> WebSocket codex-cloud-backend.chatgpt.com（完整 URL 待核实）
 -> initialize
 -> initialized
 -> thread/start（或已验证的会话恢复）
 -> turn/start
 -> 事件流（可能等待客户端工具结果）
 -> turn/completed
 -> Responses 结果 / 可选 result.json 与 answer.txt
```

已核对的[官方 App Server 文档](https://learn.chatgpt.com/docs/app-server)描述了连接初始化、线程与回合生命周期；动态工具属于实验性接口；回合完成事件还可能表示失败或中断。**该文档说明 App Server 协议，不证明 `codex-cloud-backend.chatgpt.com` 开放这些方法，也不证明可用 OAuth 客户端、scope、模型、动态工具或用量契约。**

以下事项仍需真实客户端实现及受控请求证据：完整 WSS 路径、握手头/子协议、授权签发方式、实际开放的方法、线程恢复、工具结果回传、用量和模型身份。不能把本地 App Server 测试成功写成 Cloud 验真成功，不能仅改原生或 SIWC 的上游 URL 宣称完成适配。

## 2. 账号编辑与使用策略

在现有账号编辑弹窗增加 Cloud 卡片，包含独立授权、重新授权、断开授权、状态、启用开关、模型白名单和调用测试。首期面向管理员管理的普通 OpenAI OAuth 父账号；供应商账号、影子账号及特殊原生授权类型不自动获得 Cloud 能力。后续扩展必须补足对应权限和身份规则。

授权操作与普通账号设置保存分开：授权完成只保存凭据；启用和模型范围通过现有账号更新流程保存。关闭弹窗应清除回调输入等前端敏感临时状态。

最少持久设置如下，不增加全局 Cloud 配置页面：

| 设置 | 默认值及规则 |
| --- | --- |
| `extra.openai_cloud` | `false`；仅满足授权和能力门槛才能启用 |
| `extra.openai_cloud_models` | 空列表；启用时必须非空，只接受经过验证的模型；空列表不表示所有模型 |

Cloud 开启后，**Cloud 独占该账号的执行方式**：只承接白名单内的 Responses 请求，其他模型和端点不选该账号。与 BPS 同时启用时，账号保存直接报冲突；保留 BPS 已保存的配置和授权，方便明确切换。关闭 Cloud 不删除独立授权；断开授权则关闭 Cloud 并使现有 Cloud 会话失效。

调度、发送前复核、会话续接和用量归属共用同一个执行方式判断。不能出现调度认定 Cloud、发送阶段却取原生或 BPS token 的情况。Cloud 失败不能自动使用该账号的原生或 BPS 授权重放；Cloud 已关闭后的旧 Cloud 续接也不能落入原生路径。

模型选择沿用已有映射规则，但管理员看到的选择必须能对应真实验证过的上游模型。模型白名单不能覆盖上游实际能力；模型变更、协议变化或重新授权后，相应探测证据需要失效或重新检查。

状态至少区分：

| 状态 | 证明什么 |
| --- | --- |
| 未授权 / 需重新授权 | 没有当前可用的 Cloud grant |
| 凭据有效 | 身份、有效期和必要权限检查通过 |
| 连接已验证 | Cloud 握手及初始化实际通过 |
| 调用已验证 | 指定模型及所需能力真实调用通过 |
| 已启用 / 已停用 | 管理员是否允许参与调度 |
| 限流 / 权限拒绝 / 暂时故障 | Cloud 自身运行状态，不等于整账号失效 |

`wham/usage` 成功不能单独把状态提升为“调用已验证”。状态接口只返回必要身份、能力和错误摘要，不返回 token、原始握手头或完整上游错误体。

## 3. 授权与私有存储

借鉴 BPS 独立 Excel grant 的私有加密存储、身份校验和刷新流程；借鉴已移除 SIWC 的授权代次与 CAS 思路。**不复用 SIWC 旧表、动态客户端注册、installation/host ID、direct scope 或授权工具。**

未来实现时新增一项前向迁移，为 Cloud 建立独立私有 grant 存储，建议表名 `openai_cloud_grants`。不修改已应用的历史迁移，不把旧 SIWC grant 改名当作 Cloud grant，也不在本轮执行任何数据迁移或清库。

同一前向迁移还需扩展现有 `usage_logs.authorization_mode` 的检查约束以接受 `cloud`，保留 `native`、`bps` 与历史 `siwc`；同时更新现有用量类型、筛选及展示，不能只在网关结果里加字符串而让数据库拒绝写入。无需新增第二套用量账本。

建议只新增一张凭据表：以 `account_id` 关联账号，保存加密 payload、用于 CAS 的版本及更新时间。payload 包含签发方、subject、client ID、scope、workspace/user、token、过期时间、授权代次和拒绝标记。凭据不进入通用账号 `extra`、账号导出、调度快照或浏览器可读 DTO。

“独立授权”是产品和存储目标，**尚未证明 Cloud 支持可独立签发的 OAuth client/scope，也未证明重新登录所得 Refresh Token 与原生 token 属于独立的 refresh family。** 必须验证签发、轮换和撤销语义后才能确认设计成立。禁止简单复制原生 Refresh Token 到 Cloud 表，也不能承诺重新登录必然隔离。如果上游实际共用 refresh family，必须重新确定单一刷新权威及凭据引用方式，不能让两个写入者分别轮换同一 token 家族。

授权要求：

- 使用已证实的 OAuth 入口、client ID、scope 和 redirect，不编造 Cloud 专属值；采用 PKCE、state、一次性授权会话和过期限制。
- 开始和完成授权均检查账号管理权限；回调重新验证授权 lease、账号归属和当前身份，不能只依赖开始授权时的权限。
- 校验 token 签名/签发来源、audience/client、有效期和可信身份信息；绑定父账号已验证的 workspace/user，email 仅辅助展示。只解码 JWT 不能作为新导入身份的真实性证明。
- 原生 token 正常轮换不影响 Cloud 绑定；原生账号身份替换使旧 Cloud 绑定失效。Cloud 重新授权更新 `grant_generation`，普通刷新只更新 `token_revision`。
- 已有持久加密能力不可用时拒绝保存授权，不改用明文存储。

刷新复用现有锁和 token HTTP 客户端基础能力，但隔离 Cloud 锁键和状态：本地互斥、带 owner 的分布式租约、锁内重读、数据库 CAS。刷新、回调替换、断开和 401 拒绝标记必须防止相互覆盖。迟到的旧 token 401 不得标坏已经刷新的 token；锁失效或 CAS 失败不能用旧快照盲写。

远端撤销仅在确切契约已验证时执行。删除接口始终如实区分“本地已断开”与“远端撤销已确认”，不把前者报告为后者。

## 4. 管理 API 与复用入口

以下路径省略现有 `/api/v1` 挂载前缀。新增 **4 个管理 API**，授权相关写操作使用既有 `accounts.authorize` 权限；状态读取检查相应账号可见权限。

| 方法与路径 | 行为 |
| --- | --- |
| `GET /admin/accounts/:id/cloud` | 脱敏身份、授权状态、能力检查与最后错误 |
| `POST /admin/accounts/:id/cloud/authorize` | 创建一次性授权会话，返回已验证的授权 URL |
| `POST /admin/accounts/:id/cloud/callback` | 校验 session/state/回调及权限，保存 grant |
| `DELETE /admin/accounts/:id/cloud` | 断开授权、禁用 Cloud、失效其会话并返回撤销确认状态 |

两个账号设置通过既有账号更新接口保存，不另建 Cloud 设置 API。调用检查复用现有 `POST /admin/accounts/:id/test`，增加 `mode=cloud`，使用当前 `model_id` 和 `prompt` 参数；检查代码必须走同一 Cloud 适配器，不能使用仅供测试的另一条成功路径。

下游公开接口不新增，仍为 `/v1/responses`。HTTP SSE 与非流式 JSON 共享事件解析器；如果现有客户端使用 Responses WebSocket 入口，也应复用其鉴权与桥接层，能力验收通过前明确拒绝该传输，不能悄悄回到原生连接池。

## 5. 网关与额度检查

请求先通过既有 API Key、分组、预算和并发检查，再选取允许 Cloud 的账号。发送前重读执行方式、授权代次与能力，避免旧调度快照使用已停用或已重新绑定的账号。

Cloud 凭据执行 `wham/usage`，必要时刷新后重查；已知过期的 token 不进入 WS 握手。额度结果按身份及 token revision 短暂缓存，并与 Cloud 权限状态区分。401 最多进行一次适当刷新恢复；403 区分权限/账号限制，429 按已知重置时间暂停，网络故障不当作撤销授权。

已有 `OpenAIQuotaService.QueryUsage(accountID)` 通过原生 token provider 获取凭据并处理原生状态，不能直接作为 Cloud 检查调用。应仅复用 HTTP 构建和 usage 数据解析，显式传入 Cloud 凭据、workspace 与代理，Cloud 权限/错误状态单独记录。原生和 Cloud 是否共用上游额度窗口也必须依据证据，不假设分开存储就代表上游额度独立。

Cloud 适配器管理固定且经过验证的 WSS 地址、RPC 请求 ID、初始化、线程、回合及事件。复用现有代理、TLS、超时、消息上限和连接清理能力，不开放任意账号上游 URL，不跨用户借用带历史的原生/BPS连接。

Responses 参数依据已验真的能力逐项转换：角色和历史顺序、model、推理配置、工具、输出格式及输入类型。无法保持语义的字段应在发送上游前明确拒绝，不能默默丢弃。Cloud 自带工具和环境能力必须可受控，防止客户端声明的本地工具被云端擅自执行。

## 6. 跨请求工具状态与会话恢复

复用已有 response 归属记录，再增加 Cloud 所需的最小状态，不新建通用会话平台。每次续接绑定：

```text
用户 / API Key / 分组
账号 / Cloud 授权代次 / 实际模型 / 有效设置摘要
response_id / thread_id / turn_id
连接所属实例 / 租约 / 事件游标或去重信息
待回复工具 / 已提交结果摘要 / 用量水位 / 终态
```

`previous_response_id` 只查本地受控绑定；客户端不能凭 Cloud 原始 thread ID 访问任意会话。同时覆盖完整历史输入、没有 `previous_response_id` 但带 `function_call_output` 的客户端：工具 call ID 和归属证明必须仍能定位原回合，不能只看最近一条文本。

计划中的工具闭环如下，是否能实现取决于 Cloud 实际能力：

1. 将兼容的 Responses 工具声明转换为 Cloud 动态工具。
2. 收到 Cloud 待回复工具请求，生成稳定 `call_id` 和 Responses `function_call`，保存其对应 RPC、线程与回合。
3. 将当前下游 Response 以工具输出项结束，服务端状态标记为 `awaiting_tool_outputs`；这不代表上游回合已经完成。
4. 下一次 HTTP 请求带 `function_call_output` 时，核验用户/Key/分组/账号/授权代次及 call ID，回复原来的 RPC，再把后续事件映射成新的下游 Response。
5. 同批多个工具允许符合协议的分批结果；对重复结果按摘要幂等处理，冲突、未知或跨归属结果明确拒绝。不能把工具结果拼成普通用户文本或无条件再发 `turn/start`。
6. 等待状态有 TTL、数量和字节上限；释放普通 HTTP 请求资源，保留受限的活会话资源。超时、停用、撤销或身份变更时终止等待并清理，不能伪造工具成功。

已有 state store 的连接绑定仅在进程内。共享 Redis 记录不能替代活 WebSocket。多实例场景必须证明待工具 RPC 可在新连接恢复，或以已有可行的实例归属路由把请求交回活连接实例；两者都做不到时，这项兼容门槛不通过，不能声称跨请求工具已支持。

恢复规则固定为：

| 失败时点 | 允许行为 |
| --- | --- |
| `turn/start` 尚未发送 | 在 Cloud 方式内按原调度策略重试 |
| 已发送但是否接受未知 | 查询/恢复原回合，不能自动重发 |
| 已有输出或待工具结果 | 固定账号、授权代次和回合，不换授权重放 |
| 客户端取消 | 使用已验证的中断方法；分别记录请求接受和实际终止 |
| 无法取得最终状态 | 标记结果未知/失败并保存可信用量，不宣称成功 |

事件归属至少校验 thread、turn、item，并对恢复后的重复事件去重。只有合法终态且输出完整才能生成最终成功结果；收到名为 `turn/completed` 的事件本身不是成功证明。

## 7. 用量、计费与结果文件

复用 `OpenAIForwardResult`、现有用量账本和预算扣减，未来新增实际执行来源 `authorization_mode=cloud`。取执行时的来源事实，不能从账号当前开关反推历史。SIWC 历史账单仅保留读取兼容，不产生新的 SIWC 执行记录，不伪造或改写历史来源。

Cloud 必须给出可信的本次执行用量或可验证的累计差值，包含实际模型及缓存口径。`wham/usage` 百分比不能推算 token；缺失值不能当零。重复事件、恢复和重复工具结果不能重复计费。

一个 Cloud 回合可能跨多个下游 Responses 请求。实施前必须验证能否获得每段请求的实际用量；若只能拿整回合累计值，要固定账本归属、预算预留/释放和一次性结算规则，再验证不会漏扣、重复扣费或把多个模型混算成单次模型请求。**真实用量与跨请求结算未通过时，不开放普通用户付费调度。**

正常调用直接返回 SSE/JSON，不默认永久落盘。账号测试完成后，前端可从同一脱敏最终对象生成下载文件，避免新增文件存储、下载路由和保留策略：

- `result.json`：响应 ID、完成状态、实际模型、结构化输出、可信用量、错误和时间；未知用量明确为空或缺失。
- `answer.txt`：仅最终面向用户的回答；工具等待、失败或没有答案时不伪造文本。

文件不包含 token、回调 code、握手头、私有推理或其他用户线程。失败可以导出标记失败的 `result.json`，不能输出看似成功的结果。

## 8. 代码落点与最小变更量

已核对的现有承载点：

| 职责 | 现有代码 |
| --- | --- |
| 账号编辑和独立授权交互 | [EditAccountModal.vue](../frontend/src/components/account/EditAccountModal.vue)、[ExcelBPSAuthorization.vue](../frontend/src/components/account/ExcelBPSAuthorization.vue) |
| BPS 独立授权与私有存储 | [openai_excel_oauth.go](../backend/internal/service/openai_excel_oauth.go)、[account_repo_excel_credentials.go](../backend/internal/repository/account_repo_excel_credentials.go) |
| 管理路由与账号测试 | [admin.go](../backend/internal/server/routes/admin.go)、[account_handler.go](../backend/internal/handler/admin/account_handler.go)、[account_test_service.go](../backend/internal/service/account_test_service.go) |
| 调度与网关发送 | [openai_account_scheduler.go](../backend/internal/service/openai_account_scheduler.go)、[openai_gateway_forward.go](../backend/internal/service/openai_gateway_forward.go) |
| usage 查询与 token 基础设施 | [openai_quota_service.go](../backend/internal/service/openai_quota_service.go)、[openai_token_provider.go](../backend/internal/service/openai_token_provider.go) |
| Responses 归属与状态 | [openai_ws_state_store.go](../backend/internal/service/openai_ws_state_store.go) |
| 账单来源和执行结果 | [usage_authorization_mode.go](../backend/internal/service/usage_authorization_mode.go)、[openai_gateway_service.go](../backend/internal/service/openai_gateway_service.go) |

新增代码只承载现有逻辑无法表达的 Cloud 独立授权和有状态 RPC 传输：拟建 `openai_cloud_oauth.go`、`account_repo_cloud_credentials.go`、管理 handler、账号设置校验、`openai_cloud_gateway.go`、`openai_cloud_transport.go` 及必要会话状态文件；前端一个 Cloud 设置组件及对应 API 模块。文件划分按实际体量调整，不预先抽象通用多协议框架。

规划数量为 **4 个管理 API、2 个账号配置项、1 个 Cloud 网关执行路径、1 张私有 grant 表**；复用现有账号测试、账号更新和下游 Responses 入口。能力快照、错误及会话状态属于内部运行状态，不新增管理员配置开关。本轮这些运行代码、接口、字段和迁移的实际新增数均为 **0**。

## 9. 逐阶段验收与上线门槛

每阶段只在前阶段证据有效时继续，不用大量占位代码代替协议核实。协议证据应脱敏保存，不保存真实 token 和用户内容。

| 阶段 | 工作 | 通过条件 |
| --- | --- | --- |
| 0：固定协议和授权事实 | 核实 WSS 完整 URL、握手、OAuth client/scope/refresh family、RPC 与能力 | 有受控真实请求记录；独立授权语义可成立；不存在复制原生 RT 的方案 |
| 1：单轮最小链路 | usage、必要刷新、初始化、指定模型、文本事件、终态及用量 | 实际模型和结果可确认；失败/中断不会算成功；`result.json`/`answer.txt` 来自同一结果 |
| 2：授权与账号编辑 | 私有存储、回调、刷新、断开、开关与模型白名单 | 权限变化、账号重绑定和并发刷新安全；默认关闭；Cloud/BPS 互斥；旧状态不能复活 |
| 3：Responses 与工具 | SSE/JSON、历史、续接、动态工具、跨 HTTP 结果 | 现有客户端完成工具闭环；不支持参数明确拒绝；Cloud 自带能力受控；跨用户访问拒绝 |
| 4：恢复与真实计费 | 发送后断线、重复事件、工具超时、多实例、取消、用量结算 | 不重放未知执行；不重复计费；待工具会话可恢复或正确失败；预算归属可解释 |
| 5：接入正式调度 | 复用既有网关与测试，验收已声明的客户端传输 | 普通用户通过原 `/v1/responses` 使用完整已声明能力；native/BPS 行为及历史账单不受影响 |

必要验证覆盖真实协议轨迹和有独立风险的本地测试：刷新并发/CAS、回调权限撤销、身份替换、Cloud/BPS 互斥与模型筛选、事件归属、工具跨请求、断线接受状态、重复事件计费、资源释放及结果脱敏。优先扩展既有测试，不为文件划分重复铺设测试。

若 Cloud 不能开放动态工具、控制云端执行、恢复待回复请求或提供可信用量，应该明确记录对应能力未通过，不能把仅能返回一段文本的包装称为完整 Responses 网关。阶段验收后的代码交付仍遵守项目当前分支、最小充分验证和发布授权规则；本文不授权发版、部署或生产迁移。
