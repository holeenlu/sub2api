# 流内 `event: error` 的 usage 入账：现状说明

> 基线：`main` @ `9c56e60ef` + 本地未提交的 SSE 改动（后者只改事件解析，不改本文涉及的计费链路）。
> 本文是**静态阅读结论**：逐处读了错误传播与 usage 提交路径，未发真实请求、未跑集成测试、未核对线上账单数据。
> 结论用词区分「代码上会发生」与「线上已发生」：本文只能支持前者。

## 1. 结论摘要

上游返回 HTTP 200，随后在 SSE 流体内下发 `event: error` 时，**本次请求已经从 `message_start` 观测到的 input / cache token 不会进入 usage 记录**。

出流前与出流后两种情形的后果不同：

| 情形 | failover | 本次观测到的 usage | 净效果 |
|---|---|---|---|
| 错误帧到达时**一个字节都没写给客户端** | 会重试其它账号 | 丢弃 | 部分补偿：重试成功后按重试那次计费，但**第一个账号已消耗的额度没有任何记录** |
| 错误帧到达时**已经写出过内容** | 被 handler 守卫禁止 | 丢弃 | **纯损失**：既不重试、也不记账 |

第二种是更明确的问题：没有任何后续请求去补这笔账。

## 2. 完整链路

```
上游 SSE:  event: error\ndata: {...}
  ↓ gateway_upstream_response.go:878   processSSEEvent 返回 &sseStreamErrorEventError
  ↓ gateway_upstream_response.go:1066  caller: return nil, err      ← 累积的 usage 局部变量在此被丢弃
  ↓ gateway_forward.go:824             errors.As(err, &sseErr)
  ↓ gateway_forward.go:874             return nil, &UpstreamFailoverError{...}
  ↓ gateway_handler.go:1102            errors.As(err, &failoverErr)
      ├ 已出流 → :1105 handleFailoverExhausted(..., true); return
      └ 未出流 → FailoverContinue / Exhausted / Canceled
                                        ← 两条分支都在 :1151 之前 return
```

末尾的 `gateway_handler.go:1151`：

```go
if result != nil {
    submitForwardUsage(result)
    upstreamServedSession = true
}
```

failover 分支从不到达这里。

## 3. 三层拦截，彼此独立

即便只改其中一层也不会生效：

1. **service 层丢弃。** `handleStreamingResponse` 在 `gateway_upstream_response.go:1066` 直接 `return nil, err`，函数内累积的 `usage` 不随错误返回。
2. **forward 层绕过。** `gateway_forward.go:824` 的 `sseErr` 分支在 `:874` 直接返回 `UpstreamFailoverError`，**根本没有调用** `partialStreamUsageResult`（它在同函数 `:881` 的下一分支才被调用）。
3. **不变量兜底拦截。** 即使前两层都改了，`partialStreamUsageResult`（`gateway_upstream_response.go:685`）里有显式拦截：

   ```go
   var failoverErr *UpstreamFailoverError
   if errors.As(err, &failoverErr) {
       return nil
   }
   ```

   注释写明理由：**「UpstreamFailoverError 必须保持 result=nil——failover 重试成功后按成功请求计费，若同时返回部分 usage 会造成双重计费，此处显式拦截兜底。」**

这条不变量是为「未出流 → 重试 → 重试成功」这一情形设计的，而且在那个情形下它是对的。问题在于它**同时覆盖了「已出流、不会重试」的情形**，而那里不存在双计风险。

## 4. 对照：其它中断原因为什么记得上

同一个 `handleStreamingResponse`，其它中断路径返回的是**普通 error**（不是 `UpstreamFailoverError`）：

- 缺 terminal 事件：`fmt.Errorf("stream usage incomplete: missing terminal event")`
- 读错误（已出流）：`fmt.Errorf("stream read error: %w", ...)`
- 数据间隔超时：`fmt.Errorf("stream data interval timeout")`

这些会走 `gateway_forward.go:881` 的 `partialStreamUsageResult`，拿到非 nil 的 `ForwardResult` 一起返回，handler 在 `:1151` 照常入账（issue #5148 就是为此修的）。

**所以流内 `event: error` 是唯一一个「上游已计量但本地不记账」的中断原因。** 这是不对称，不是设计意图——至少现有注释里没有为它单独说明理由。

## 5. 一处缓解（但不能替代记账）

`handleStreamingResponse` 开头调用了 `s.rateLimitService.UpdateSessionWindow(ctx, account, resp.Header)`，从上游响应头刷新账号的 5h / 7d 窗口状态。因此**账号级限流窗口不依赖本地 usage 记录**，不会因为这笔漏记而失准。

受影响的是依赖 `usage_logs` 的部分：账号用量统计、成本归集、按用量的调度权重。具体影响面需要按实际查询口径确认，本文不做量化。

## 6. 可选方案

| 方案 | 做法 | 双计风险 | 影响面 |
|---|---|---|---|
| **A 只放开「已出流」分支** | service 层把部分 result 与 sseErr 一起返回；forward 层在 `c.Writer.Written()` 为真时用 `partialStreamUsageResult` 的结果；`partialStreamUsageResult` 的 failover 拦截改为「failover 且未出流才拦截」 | **无**。已出流时 handler 必定走 `handleFailoverExhausted` 且不重试，不存在第二次计费 | 最小。只改一个条件判断的粒度，不动不变量本身的意图 |
| B 全面放开 | 未出流时也记部分 usage | **有**。重试成功后会按成功请求再记一次，同一次上游消耗被记两笔 | 需要在 handler 侧引入「已记过部分 usage」的去重标记，跨 failover 迭代传递 |
| C 不改 | 维持现状 | 无 | 已出流的 SSE 错误持续漏记 |

倾向 A：它把现有不变量从「failover 错误一律 result=nil」收窄成「**会重试的** failover 错误才 result=nil」，与注释里陈述的理由（「重试成功后按成功请求计费」）完全一致——换句话说，现有实现比它自己的注释管得更宽。

A 需要确认的一点：handler 的出流判断用的是 `c.Writer.Size() != writerSizeBeforeForward`（`gateway_handler.go:1104`），而 service 层能看到的是 `c.Writer.Written()`。两者在「写过 0 字节的 header」这类边界上可能不一致，实现时必须用同一个判据，否则会出现「service 认为已出流而记账、handler 认为未出流而重试」的双计。

## 7. 验收要求

无论选哪个方案，验收必须覆盖：

1. 未出流 + SSE error + 重试成功 → usage **恰好一笔**（重试那笔），不得出现两笔。
2. 已出流 + SSE error → usage **恰好一笔**（本次观测到的部分），不重试。
3. 未观测到任何 token 的 SSE error → **零笔**，不得凭空造记录。
4. 既有的三条中断路径（缺 terminal / 读错误 / 超时）行为不变，仍恰好一笔。
5. 客户端断开 + SSE error 的组合不重复计费。
6. service 层与 handler 层的「是否已出流」判据一致。
7. 覆盖普通转发与 passthrough 两条链路。

## 8. 核验边界

- 全部结论来自阅读源码，行号可逐条复核。
- **未**验证线上是否真的发生过这类漏记，也未估算规模——没有查过 `usage_logs` 或账单数据。
- 未验证「上游下发 `message_start` 后再发 `event: error`」在真实流量中的发生频率。
- `UpdateSessionWindow` 的缓解作用来自代码调用位置，未实测其在该错误路径下的实际刷新结果。
