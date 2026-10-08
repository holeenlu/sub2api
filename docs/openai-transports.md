# OpenAI 客户端与上游协议

普通 OpenAI 请求默认跟随客户端的实际入站协议。账号开启 WS 能力不会把客户端 HTTP 请求升级为上游 WS；管理员可以显式选择 `http_bridge` 模式。

| 客户端 | 实际生成通道 | 中转站连接上游 | 返回客户端 |
| --- | --- | --- | --- |
| HTTP Responses | 原生 OpenAI | HTTP | SSE 或非流式 JSON |
| WebSocket Responses | 原生 OpenAI | WebSocket | WebSocket 事件 |
| WebSocket Responses，手动选择 `http_bridge` | 原生 OpenAI | HTTP / SSE | 转换为 WebSocket 事件 |

原生 WS 仍受全局开关、账号 WS 能力、权限和调度约束。不满足 WS 条件时返回明确错误，不自动改用普通 HTTP。绑定仅支持 HTTP 的 OAuth 传输插件的账号要求客户端使用 HTTP，或显式选择手动 HTTP 桥接；不能绕过插件改走原生 WS。Grok 等 HTTP 上游的兼容适配继续保留。

## 手动桥接与续聊

账号创建、编辑、批量设置以及历史配置中的 `http_bridge` 模式继续有效。它仍受 `mode_router_v2_enabled` 开关控制，不改为 `ctx_pool`。客户端 WS 请求经 HTTP Responses 转发，SSE 事件转换成 WS 帧；工具续聊、previous_response_id 历史重建、原生账号资格和 RPM 校验保持。

手动桥接的 `generate=false` 预热在桥接器本地应答；原生 WS 的预热保留 `generate=false` 并发往上游。取消和连接结束仍执行已有的用量收尾，避免把正常客户端退出归为账号故障。

## 已移除的转换

- 2026-10-04：移除账号“HTTP 流式 WS 加速”和按 WS 首包大小自动改走 HTTP 的策略。旧 `openai_oauth_ws_sse_acceleration` 不再影响路由；旧 `http_bridge_enabled` / `http_bridge_threshold_bytes` 配置按未使用字段忽略。
- 2026-10-05：移除 Excel/BPS 和旧独立平台的全部运行路径，参见 [升级说明](BPS_REMOVAL.md)。保留 Codex 降智检测、原生工具与图片能力；打票、Cookie/票据绑定及额外轮次准入也已随原生恢复移除。

通用 WS 帧大小限制、`client_first_message_timeout_seconds`、上游读写超时和客户端重试各自生效。移除旧转换不会回退首包超时设置，也不改变模型的实际生成速度。

## 来源边界

手动模式沿用官方方案的 WS mode、选择器和显式桥接逻辑。2026-10-04 的源码对照基准为官方 `b8dece9000c68815a5b867ca5a1e6f236e173905` 与 ranxi `0ae36e501952000c5c910a2e616c6e0861f66a49`；来源与历史比较可从对应 Git 提交查阅。本轮恢复后的转发、连接池、预热、续聊、工具处理与用量收尾使用固定官方版本；检测及品牌/安全等保留边界见 [原生恢复说明](FORK_FEATURE_RETIREMENT.md)。

## 2026-10-09 恢复与运维边界

2026-10-09。共享能力，适用于 KDAN、TapModels、tokensavy。实施基线为 `cd42f2e0d`，对应跨项目审计修订方案的 P1 和 P2 中代码可验证的恢复、生命周期、传输反馈部分。

### 当前行为

- 原生 WS 暂存 Codex 元数据、`response.created` 和 `response.in_progress`。1 秒窗口从本轮首次 created/in_progress 起算，Codex 元数据不提前消耗窗口；重试不延长已开始的窗口，暂存仍受首输出预算及 8 MiB 上限约束。首次内容、其他需要公开的事件、终态或窗口到期会按序公开。窗口到期不会关流；公开响应身份后禁止透明重放。`output_item` 等可能带完整结果的事件按保守交付边界处理，不另建首 token 分类表。
- 首次实际输出前的 EOF / 1012，可在执行风险允许时同账号换连接一次。`store=false` 续聊必须重建完整历史，并移除旧连接的 `previous_response_id`；同账号可保留必要密文与文件引用，跨账号拒绝不透明资源、密文和孤立工具输出。含托管工具或未知工具的执行结果不确定时不自动重发。
- 裸 `error` 通常是暂定状态。尚无 response ID、明确指出非 input 参数的 unknown/unsupported/missing_required_parameter 拒绝，在字段修复无法处理后即时结束本轮；无参数定位的错误码和 persisted-item lookup 消息仍等待权威终态，避免截断实际可恢复的流。恢复内容后清理 pending payload 和 error idle deadline；最终失败才记录 Ops 失败标记，成功终态保留原始错误遥测但不计失败 SLA。安全拒绝的既有风控采集仍在原入口执行，不因延后 SLA 定案而绕过。
- 每条下游连接有一个物理 reader，普通帧队列满时背压，字节或控制帧安全上限被触发时发送明确关闭原因；所有业务写入与关闭经过连接对象协调。取消绑定接收时的 turn；模式确定前的取消保留，passthrough 按原帧透传；旧轮 ID 不取消当前请求，也不触发暂存身份公开。已知上游 response ID 时先发送取消，短时间等待终态；无法取得终态则淘汰上游连接并结束本轮。未知 ID 不生成虚假响应 ID。取消后历史完整时允许新连接续聊；存在尚未完成的输出项时不得把部分历史标记为完整。关闭握手最多等 250ms，随后关闭底层连接并回收 reader。
- reader 观察到客户端断开后停止重试，也不执行已排队的新 create；用量收尾有 1.2 秒绝对期限，不被持续流量刷新。此值是保守初始实现，真实生产分布仍需验证。已收到的权威终态优先保留；未取得终态时保留已观察到的 usage，标记 ClientDisconnect 和未知终态，跳过账号健康结算。没有 usage 的缺口仅记录诊断，不估算补扣。原生、bridge 和 passthrough 均覆盖该规则，租约/抢占通知仍有序关闭。
- 后续 turn 复用 HTTP API Key 状态、用户、分组、过期/额度和 IP 规则；保留 Simple Mode 的现行豁免。nil 分组保持 HTTP 的允许语义；标准模式每个新 turn 复查余额、订阅限额和 RPM，同轮重试不重复执行这次检查。认证或计费准入失效不处罚上游账号。改分组或刷新失败时停止新 turn，不能继续依赖旧授权快照。
- 弃用 attempt 不产生可计费结果；必要的 error/cleanup hook 仍释放槽位。handler 回归覆盖旧 response ID 不进入 `RecordUsage` 的用量记录，winner 只记录一次。取消、输出上限、安全 incomplete 与真正的上游失败分开处理账号健康。

### HTTP 与代理

普通 OpenAI HTTP/2 错误反馈覆盖响应体读取，支持 HTTP、HTTPS、SOCKS5、SOCKS5H 代理。状态按出口、上游 origin 和账号隔离，回退使用独立 HTTP/1.1 池；不会关闭其他活动流或重放已交付内容的请求。响应头/普通 EOF 不代表成功完成，协议终态或完整非流式 JSON 才清理失败累计；queued/in_progress JSON、无终态 SSE 和损坏 JSON 不算完成。回退状态在新条目插入时惰性清理过期项，最多保留 4096 项；日志记录出口和路由的脱敏 hash。代理 URL 验证复用现有 `proxyurl`，无效代理不回退直连。

所有 OpenAI HTTP 尝试（包括直连、HTTP 地址和不可重读 Body）均收集连接获取与发送证据；已观察到连接获取但未交给 HTTP 的拨号/DNS/TLS 失败允许外层换号。发送前 TLS 内部重试仍仅对代理 HTTPS 生效，在 httptrace 证明暂态握手失败、尚未交给 HTTP 且请求体可重读时执行一次。没有 trace、已经写入、TLS 证书错误、取消均不进入该内部重试。适配来源为 ranxi2001/sub2api `11be589504b482d77827ab383b4b53f777241238`；没有恢复其票据、BPS 或额外准入业务。

一次 HTTP transport 返回错误不一定意味着未执行。错误保留发送证据；当可能已发送且请求带托管工具时，不能因没有首 token 就换号。插件原有 RequestSent 约束继续生效。

### 逻辑 turn 预算

新增配置：

```yaml
gateway:
  openai_turn_max_attempts: 0
```

环境变量为 `GATEWAY_OPENAI_TURN_MAX_ATTEMPTS`，允许 0–100。默认 0 不增加新的合计次数限制；现有 `max_account_switches` 默认 10 保持。灰度设置 3 后，有效上限取 `min(3, max_account_switches + 1)`，同时保留既有切换剩余次数与单类重试上限。

拨号/生成尝试、同账号重连、字段修复后重发和内部 TLS 重试共用预算；同一 turn 换账号不重置，新 create 才重置。T=0 时 HTTP 首内容时限保留每次 Forward 的起点，换号不会继承已耗尽的 deadline；T>0 时跨尝试共用逻辑请求起点。原生 WS 维持既有的 turn 级时限。发送前预算耗尽不记为未收到请求账号的故障。为用量排空而 detached 的 context 不能授权取消后的新生成。

T>0 时，同一 turn 在两个不同账号遇到 `response protection is unavailable` 后，停止继续遍历账号；后续请求仍可以探测恢复。T=0 同时关闭这项新增的相关错误抑制，保留原有换号行为；不创建全局永久故障状态。

### 模式与验证边界

原生 ctx_pool 是完整上下文恢复路径。HTTP bridge 在外层证明历史完整后可对后续轮次的安全失败生成当前轮重试输入；失联或取消不再继续换号。passthrough 保留独立协议透传，增加共用连接/预算与暂定错误结算保护，但不声称它拥有 ctx_pool 的全部上下文重建能力。

当前实现一条客户端连接只承载一个响应 lane，改变 stream_id 会明确拒绝，以防共享历史串线。修订方案 P3 的 16 在途响应 / 32 命名 lane 多路复用与 steering 属于后续专项，本次没有实现或启用。也未重新引入 HTTP→WS 加速、H2 多连接分片、预扣退款系统或新的计费估算政策。

生产发布、代理切换、真实 60 分钟耐久、付费 OpenAI 调用和灰度性能对比不包含在本次本地实现中。部署时先小范围设置预算，观察最终成功率、每 turn 尝试数、首内容 P95/P99、连接数与账单一致性；代理维护先排空连接。1 秒暂存到期与 1.2 秒断连排空是不同的时钟，不能互换。

测试扩展在已有 WS session、handler usage、配置、鉴权和 relay 测试中；仅 TLS/响应体 transport 回归使用一个新的测试文件。测试覆盖取消/终态竞态、真正 handler 用量入口、旧 ID 隔离、strict-affinity 实际重放 JSON、预算、TLS 发送前证据和 H2 响应体反馈。新实现没有新增 HTTP endpoint 或数据库迁移。

### 2026-10-09 实施复核处理

H-1/H-2 修复分别由真实 net/http trace、真实 handler 换号以及 nil 分组多轮测试验证。M-2 至 M-6 覆盖延迟 created、三种 WS 模式的部分用量、真实 handler 计费、队列背压、默认预算兼容和客户端断连优先级。

M-1 没有按“未收到 created 即可重放托管工具”的建议放宽。上游可以先执行再返回首帧，EOF/1012 不能反证未执行；发送前拨号失败已恢复换号，发送后的未知执行仍须满足重放安全条件。L-9 的 bridge 标记不是死代码，HTTP transport 错误处理函数会读取它，故保留。完整逐项处理记录随本轮交付说明保存。
