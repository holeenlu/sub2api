# Codex 出站会话身份：迁移前现状说明

> **2026-10-18 实施更新：** 第 1–7 节保留迁移前的历史分析，不代表当前代码；本轮已补齐官方头的入口与出站落点，最新行为、原生语义修正和验收范围见第 8 节。尚未删除旧头，也未部署。

> 基线：`main` @ `9c56e60ef`（工作区另有未提交的 SSE / metadata.user_id / turn-metadata 编码器改动，均不触及本文涉及的身份落点）。
> 本文是**静态阅读结论**：逐处读了出站请求构造代码，没有发真实上游请求、没有抓包、没有跑集成测试。
> 凡标注「官方」的行为均来自 sub4api 代码注释对 `codex-rs` 的转述（它给了文件与行号），**本仓库没有独立核验过 codex-rs 源码**。

## 1. 为什么要先做迁移，而不是先删头

把「官方不发的头」删掉是最终目标，但不能先做。今天承载**多租户会话隔离**的字段，恰好就是那几个要删的下划线头：删完等于隔离回退，而不是特征收敛。

因此顺序必须是：

```
身份承载字段迁移（新字段上线 + 隔离验收）  →  旧字段冗余  →  删除旧字段
```

## 2. 出站身份落点全表

### 2.1 写入点

| 路径 | 代码位置 | 写入的头 | 值的来源 | 形状 |
|---|---|---|---|---|
| 普通 Responses | `openai_gateway_forward.go:1434` | `session_id`、`conversation_id`¹ | `isolate(promptCacheKey)` | **16 位 hex** |
| `/responses/compact` | `openai_gateway_forward.go:1428` | `session_id` | `isolate(resolveOpenAICompactSessionID)` | **16 位 hex** |
| HTTP passthrough | `openai_gateway_passthrough.go:758/761` | `session_id`、`conversation_id` | `isolate(客户端 session_id → compact → prompt_cache_key)` | **16 位 hex** |
| WS 握手 | `openai_ws_forwarder_payload.go:128/131` | `session_id`、`conversation_id` | `isolate(sessionResolution)` | **16 位 hex** |
| Chat Completions bridge | `openai_gateway_chat_completions.go:369` | `session_id` | `generateSessionUUID(isolate(...))` | **UUID** |
| Messages bridge | `openai_gateway_messages.go:391/393` | `session_id`、`conversation_id`² | `generateSessionUUID(isolate(...))` | **UUID** |
| Alpha search | `openai_alpha_search.go:283` | `Session_ID`、`Conversation_ID` | `isolate(body.id)` | **16 位 hex** |
| 指纹收敛（非 off） | `openai_codex_fingerprint.go:363-378` | `x-codex-installation-id`、`x-codex-window-id`、`x-client-request-id`、**`session-id`**、`session_id`、**`thread-id`** | 账号种子派生 | UUIDv4 / v7 |
| 账号身份作用域 | `openai_codex_account_identity.go:252` | 对**已存在**的字段逐个改写 | `scopeCodexAccountIdentityValue` | **UUIDv4** |

¹ `conversation_id` 仅在 `!compatMessagesBridge || clientConversationID != ""` 时写。
² `conversation_id` 仅在该头已存在时覆盖。

### 2.2 三种派生函数并存

| 函数 | 位置 | 输出 | 用在哪 |
|---|---|---|---|
| `isolateOpenAIUpstreamSessionID` | `openai_codex_account_identity.go:80` | `sha256[:8]` → **16 位 hex** | 四条主路径的 header |
| `generateSessionUUID` | `gateway_claude_oauth_body.go:516` | sha256 → **UUIDv4 形状** | 两条 bridge 的 header |
| `scopeCodexAccountIdentityValue` | `openai_codex_account_identity.go:93` | `deriveStableUUIDv4` → **UUIDv4 形状** | body 的 `client_metadata`、以及已存在的 header |

**结论：同一次请求里，header 的 session 身份与 body `client_metadata.session_id` 既不同值、也不同形状。** 官方两处发的是同一个 `Uuid::now_v7()`（据 sub4api 注释转述）。这一点在「复用已隔离的最终值」时必须先定清楚用哪一个，否则迁移只会把不一致搬个位置。

### 2.3 白名单：官方连字符名根本进不来

`openaiAllowedHeaders` / `openaiPassthroughAllowedHeaders`（`openai_gateway_service.go:77` / `95`）两张表里**都没有** `session-id`、`thread-id`、`x-client-request-id`。客户端即使按官方形态发送，也会在 `openai_gateway_forward.go:1398` 的过滤处被丢掉。

与之对称的是：两张表里**有** `session_id`、`conversation_id`、`x-codex-installation-id`、`accept-language`——据 sub4api 转述，这四个恰好是官方**从不发送**的名字（下划线形式在 codex-rs `fix: drop underscured id headers #22193` 中已删除；installation id 官方只放在 `client_metadata`；`accept-language` reqwest 不写）。

也就是说，今天出站的头集合相对官方是**反的**：该发的被丢掉，不该发的被补上。

### 2.4 `applyCodexAccountIdentityHeaders` 只改不补

`openai_codex_account_identity.go:252` 遍历 `codexAccountIdentityFields`（该表里**已经包含** `session-id` / `thread-id` / `x-client-request-id`），但：

- 只对 `headers.Get(name) != ""` 的字段改写，**不会凭空补出** `session-id`；
- 显式 `continue` 跳过 `session_id`（注释：由各请求构造器按 prompt cache key 另行重建）。

## 3. 危险分支

同时满足以下三条时，出站请求**没有任何官方形态的会话标识**：

1. 客户端没带 `session-id`（或带了但被白名单丢掉）；
2. 会话值回退自 `prompt_cache_key` / compact session；
3. 账号未开启指纹收敛（`GetCodexFingerprintMode()` 默认 `off`）。

此时只有 16-hex 的 `session_id` / `conversation_id` 出站。passthrough 路径更窄一些：`openai_gateway_passthrough.go:734` 读客户端原始值时也只读下划线形式。

## 4. 不能推导出的结论

**不能**由上述现状推导出「本地已经发生跨用户串用」。WS 连接池有独立的 `TenantScope`（`openai_ws_pool.go:73`，注释明确「authenticated tenant, independent of client-controlled headers」），连接复用不依赖这些 header。本文描述的是**出站会话身份退化 / 可判别特征**风险，不是本地隔离失效。

同样不能推导的：上游是否真的用这些字段做风控、以及改成官方形态后风控结果会不会变。没有证据。

## 5. 形状选择：已定为 B

| 方案 | 做法 | 评价 |
|---|---|---|
| A 直接搬运 | 把现有 16-hex 写到 `session-id` | 修掉「该发的没发」，但新造一个「`session-id` 不是 UUID」，且保留两套派生结果，日后仍要再迁一次。sub4api 即此做法（其 isolate 函数与本仓库逐字节相同） |
| **B 对齐 body**（已选） | 让 header 复用 body 已经在用的 `scopeCodexAccountIdentityValue` 结果 | header 与 body 首次使用同一个会话身份，顺带消掉 2.2 的不一致 |
| C 不迁移 | 维持现状 | 第 6 节第 4 步（删头）永远做不了 |

**B 能承诺的是「统一 UUID 表示、消除内部不一致」，不能宣称「已符合官方 UUIDv7 时序」。**
`deriveStableUUIDv4`（`openai_codex_fingerprint.go:214`）把 version nibble 置为 `4`，产出的是确定性 UUIDv4 形状；据转述官方用的是 `Uuid::now_v7()`。本轮不改这个 helper。

### 5.1 B 的四条约束

1. **复用同一个最终身份值，而不只是调用同一个函数。**
   header、body 平铺 `client_metadata`、内嵌 turn-metadata 三处必须来自同一个原始 session、同一 API Key、同一凭据命名空间、同一 `kind="session"`。已作用域化的结果不得再送进 `scopeCodexAccountIdentityValue` 二次派生，旧的 16-hex 更不行。
   Forward 当前刻意在 `openai_gateway_forward.go:559` 先捕获 `clientPromptCacheKey`（原始值），再于 `:565` 调用 `applyCodexAccountIdentityClientMetadataMap` 作用域化 body——header builder 拿到的是原始键，正是为了避免重复派生。迁移不得破坏这个顺序。

2. **namespace 缺失分支必须单独处理，不能机械替换。**
   两个函数在 `codexAccountIdentityNamespace(account) == ""` 时行为不同：
   - `scopeCodexAccountIdentityValue`（`openai_codex_account_identity.go:93`）**原样返回 raw**；
   - `isolateOpenAIUpstreamSessionID`（`:80`）退回 `isolateOpenAISessionID(apiKeyID, raw)`，**仍按 API Key 隔离**。

   直接替换会让这个分支退化成「把客户端原始 session 原样发给上游」。没有稳定 namespace 的账号本轮**不进入**新字段迁移，保留旧隔离行为；或先由既有凭据解析流程补齐 `chatgpt_account_id` / 指纹种子再迁。任何情况下都不得原样输出客户端 session。

3. **最终出站的 `prompt_cache_key` 必须逐字节不变。**
   它和 body session 存在明确耦合（`openai_codex_account_identity.go:182-186`，raw 路径同逻辑在 `:235-239`）：
   ```
   kind := "prompt-cache"
   if originalBodySessionID != "" && raw == originalBodySessionID { kind = "session" }
   ```
   `originalBodySessionID` 取自**作用域化之前**的 `client_metadata.session_id`。迁移若先补写 body session，会改变这个相等判断，进而改变派生出的缓存键。
   验收必须用**最终请求**断言迁移前后缓存键逐字节一致，覆盖三种情况：缓存键与 body session **相等** / **不等** / body session **缺失**。

4. **本轮不改 `scopeCodexAccountIdentityValue` 本身。**
   改它会同时改变 body 身份与依赖它派生的缓存键，把两件事绑在一起失败。

### 5.2 关于缓存，不承诺零影响

本次迁移保持现有 body 身份派生与最终 `prompt_cache_key` 不变，**不主动改变缓存键连续性**；但缓存命中率是否受会话路由变化影响需要实测，不承诺零影响。

即使缓存键不变也不等于必然命中：公开文档明确缓存还受前缀匹配、路由与容量影响，key 本身不保证命中；而且该文档描述的是公共 API，不能当作 ChatGPT 内部 OAuth endpoint 的零影响保证。

一次性身份变化可以接受，但**应在部署排空或新连接边界切换**，避免活跃 WS 回合中途改变身份。不得为了「重新开始」主动清空 `previous_response_id` 或 `x-codex-turn-state`。

## 6. 建议的迁移与验收顺序

1. **补齐，不删除**：白名单放行 `session-id` / `thread-id` / `x-client-request-id`；在四条主路径按选定方案写入这三个头。此阶段旧的下划线头**保持原样**，新旧并存。
2. **验收隔离**（`applyCodexAccountIdentityHeaders` 这类 helper 单测不够，必须测到最终出站头与 body）：
   - 同一原始 session，不同 API Key → 不同值；
   - 同一原始 session，不同上游账号 → 不同值；
   - 同一会话连续多轮 → 稳定不变；
   - header 与 body `client_metadata` / 内嵌 turn-metadata 三处**同值**；
   - **最终 `prompt_cache_key` 与迁移前逐字节一致**，覆盖 5.1 约束 3 的三种情况；
   - namespace 缺失的账号走旧路径，输出不得等于客户端原始 session；
   - 覆盖四条主路径，以及 builder 返回**之后**仍会覆盖下划线头的两条 bridge（Chat / Messages），不能只测 builder。
3. **验收形态**：`thread-id` 保留独立语义，不得把所有 thread 强行等同于 session。
   切换安排在部署排空或新连接边界，不在活跃 WS 回合中途改变身份。
4. 确认 1–3 稳定后，再评估删除 `session_id` / `conversation_id` / `x-codex-installation-id` / `accept-language`，且**范围限定在已确认的 Codex endpoint**：Alpha search、API-key relay、其它 beta 协商路径不顺带推广。

## 7. 本文档的核验边界

- 全部结论来自阅读 `main @ 9c56e60ef` 的源码，列出的行号可逐条复核。
- 「官方行为」一律是转述，未独立核验 codex-rs。
- 未执行：真实账号请求、抓包、集成测试、性能测量。
- 未对上游风控规则做任何推断。

## 8. 2026-10-18：第一阶段实现与验收

本轮按“先补齐、验收，再考虑删除”的顺序修改现有转发链路。未更改
scopeCodexAccountIdentityValue、命名空间或 prompt_cache_key 派生算法；未恢复测试平台。

### 8.1 已实现

- HTTP 普通/透传白名单接收 session-id、thread-id、x-client-request-id。OAuth 出站继续按当前 API Key 与凭据 namespace 作用域化；API-key relay 保留客户端值。
- namespace 不存在时不外发三个未隔离的新头；缺少旧 session_id 时把原 session-id 按既有 API-key 隔离规则写入旧头。不会把 raw session 或已隔离的 16-hex 当成 UUID 再派生。
- 有已经作用域化的 client_metadata 时，从平铺或嵌入 turn-metadata 读取最终值。字段必须为字符串，不能把未参与作用域化的数字/布尔值转换成出站身份。
- 缺少 body 身份时，先读已作用域化的 turn-metadata；最后才以原始 session/缓存线索调用现有派生函数补齐 session-id。透传的缓存线索在 body 作用域化之前捕获，不从已改写的 cache key 二次派生。不凭缓存键补 thread-id。
- 普通 compact 原先跳过整个 body 身份处理，现在仅作用域化其 client_metadata，保留该路径既有 prompt_cache_key 字节；透传 compact 继续沿用原有缓存键作用域化。
- WS ctx_pool、passthrough、池化 V2 和重连头构造现在接收已规范化、已作用域化的首帧/当前帧身份。无需假定“握手构造器拿不到 body”，也不靠缓存键覆盖帧内身份。
- V2 重新嵌入原始 turn-metadata 后单独作用域化嵌入字段；写入前复制 metadata map，防止修改重试源请求。已作用域化的平铺字段不重复处理。
- Chat/Messages 兼容桥保留原有 post-build 下划线头覆盖；新增官方头保持隔离值。可选指纹模式仍按既有显式配置最后应用。

### 8.2 对“header/body 必须始终相等”的修正

独立读取固定官方源码
[client.rs @ 1cc7dc8f2acc](https://github.com/openai/codex/blob/1cc7dc8f2acc63ce6c1cb72271e6b90a76136c2b/codex-rs/core/src/client.rs)
后确认：responses_session_id() 对根 agent 可使用 prompt-cache affinity override，
而 turn metadata 保留实际 session；非根 agent 则保留自身 session。
因此 **两个字段对应同一原始身份时必须同值，明确的原生 affinity override 不应被强行抹平**。

实现优先级为：已作用域化的显式官方头 → 已作用域化的 body → 已作用域化的
turn-metadata 头 → 原始线索的有界回退。thread-id 保持独立；存在可靠 thread-id
时 x-client-request-id 与其一致。B 仍是统一 UUID/隔离语义的选择，不把“消除内部
误配”扩大为“禁止原生协议的合法差异”，也不承诺 UUIDv7 时序或缓存命中率。

### 8.3 验证与剩余边界

现有测试增加最终 HTTP/compact/透传构造器和真实 Forward 捕获断言：两个 API Key、
两份账号 namespace、同/不同/缺少 body session 的缓存分支、嵌入 metadata、显式
affinity override、缺 namespace 降级、API-key relay，以及 Chat/Messages post-build。
WS 校验首帧身份优先于缓存回退、头/帧同源、无 thread 时不伪造，以及重试源 map 不变。
这些请求仅到已有内存上游替身，不发真实供应商请求。

第一阶段仍保留 session_id、conversation_id、x-codex-installation-id、accept-language；
删头不在本批执行。生产上游识别、真实缓存命中率、跨版本客户端兼容性和延迟收益尚未
实测。部署应排空旧连接或在新连接边界生效，不改活跃 socket 的身份。


## 9. 2026-10-19：出站投影与完整调用链补齐

第一阶段保留旧头的说明仅描述当时状态。本轮已在可信账号 namespace 下、官方 session-id 已完成作用域化后，移除 session_id、conversation_id、installation 旧头和 accept-language；HTTP 过滤 WS/旧 Responses beta，保留独立功能 beta；WS 保留其协商头。namespace 不可用时继续保留既有隔离降级，不从未作用域化 body 伪造官方身份。

四条 Responses 路径及 Chat/Messages 兼容桥、native compact probe 已统一最终投影。仍采用 B 的 UUIDv4 派生与既有 prompt_cache_key 规则；显式 session affinity 不强行改成 body session，真实 thread/parent/window ordinal 独立保存。metadata 缺项补齐属于网关默认值，非声称客户端曾发送；tool_namespaces_info 仅在 header 裁剪，body 不丢。

HTTP 同账号重试复用已完成的 metadata body，默认 turn UUID/time 不重复生成。WS 首帧 header-only turn 在作用域化前合入；后续轮及重连使用当前 payload 的 metadata，不沿用首轮 turn。HTTP→WS 成功结果登记原有 HTTP response owner，允许本用户续接并拒绝其他租户；无身份头两轮续接验证使用同一上游连接。

验收：Go 1.27.2 全后端 unit、重点 race、vet、server build 通过；详细配置、差异与未实施的自动裁历史能力见 [传输方案第 13 节](CODEX_RESPONSES_TRANSPORT_OPTIMIZATION.md#13-指定来源协议优化落地2026-10-18-至-2026-10-19)。本文档原有 ignore 状态未调整。
