# Request Lifecycle

本文描述网关请求的职责顺序，用于判断新功能应接入哪里。

## HTTP / SSE 主路径

```text
路由和请求解析
  → API Key 认证
  → User / Group / Subscription 上下文
  → 安全与内容策略前置检查
  → 用户并发槽位
  → 计费资格与配额门禁
  → Group / Channel 策略解析
  → Account 调度和账号并发槽位
  → 上游请求与重试
  → 权威 usage 解析
  → 统一结算和 UsageLog
  → 释放 Account / Key / User 槽位
```

具体协议可能在顺序细节上有所不同，但不能各自拥有独立的认证、配额或结算语义。

## 状态边界

| 阶段 | 允许拥有的状态 | 不应拥有的状态 |
| --- | --- | --- |
| Handler | 请求参数、协议流状态、响应是否开始 | 余额账本、持久配额算法 |
| Auth middleware/service | 已认证 User/API Key/Group 快照 | 上游 Account 选择 |
| Gateway scheduler | 候选账号、排除集合、会话粘性 | 用户登录权限 |
| Billing service/repository | 预占、结算、退款、去重 | 前端展示状态 |
| Usage repository | 只追加消费事实和查询 | 再次扣费 |
| Concurrency service | 活跃槽位和释放句柄 | 持久余额 |

## API Key 的共享接入点

API Key 独立主体能力优先接入：

1. 认证完成后的统一主体解析；
2. `ConcurrencyHelper` 一类共享槽位帮助器；
3. 统一 billing/usage 提交路径；
4. 统一 usage 查询过滤和 DTO；
5. 用户和管理员 API Key service。

禁止把同一 Key 权限或快照算法分别复制到 Chat、Responses、Gemini、Images 和 WebSocket Handler。

## WebSocket

OpenAI WS ingress 的“活跃连接”与每一轮实际请求的用户/Account 并发是两个概念。空闲连接可以保留 ingress lease，但不应长期占用每轮请求槽位。每轮请求仍必须执行认证后的权限、资金、计费和归属合同。

## 异步任务

异步任务必须持久化请求所有者 `user_id + api_key_id`，并明确提交、上游接受、完成、失败和退款的唯一状态所有者。请求 Context 结束不能成为丢失最终结算或退款的理由。

## 修改本生命周期的要求

涉及阶段顺序、重试、预占、结算、退款或槽位所有权的修改属于 L 级，必须：

- 更新或新增 ADR；
- 提供失败路径和取消路径；
- 证明无双扣、双退和槽位泄漏；
- 对所有相关协议执行合同测试。
