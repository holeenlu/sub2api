# Sub2API Architecture Overview

状态：当前架构事实基线。代码和测试变化时同步更新。

## 系统职责

Sub2API 是面向多上游平台的 API 网关。它负责客户端认证、用户资金和订阅校验、分组与账号调度、协议转换、上游调用、计费结算、用量归属、并发控制和运营审计。

核心身份关系：

```text
User
  └─ owns API Key
       └─ selects Group
            ├─ is associated with Channel policy
            └─ schedules Account credentials
```

## 状态所有权

| 状态 | 权威所有者 | 说明 |
| --- | --- | --- |
| 登录、角色、余额、用户并发 | User / Subscription | User 是资金和权限上界 |
| 调用身份、Key 状态、Key 配额、周期额度 | APIKey | Key 是独立工作负载和统计主体 |
| 服务等级、平台和账号池 | Group | Key 选择 Group，Group 组织可调度账号 |
| 模型映射、渠道定价、功能策略 | Channel | Channel 是策略层，不保存客户端身份 |
| 上游凭证、代理、账号并发、上游额度 | Account | Account 是实际执行资源 |
| 已发生请求的消费事实 | UsageLog / billing ledger | 只追加；不能由聚合统计替代 |
| 当前用户和账号并发槽位 | Redis concurrency ledger | 数据库配置提供上限，Redis 管理活跃槽位 |
| 安全、错误和运营事件 | 对应审计/ops 事件存储 | 不应替代消费账本 |

## 后端模块边界

```text
server/routes + handler
  = HTTP/WS 参数、认证上下文、响应协议

service
  = 领域规则、调度、计费、并发和生命周期

repository
  = PostgreSQL/Redis 持久化和原子操作

ent/schema + migrations
  = 数据模型与迁移事实

frontend
  = 用户和管理员交互，不拥有结算规则
```

Handler 不应成为计费、权限或重试状态的唯一所有者。跨协议规则必须进入共享 service/repository 接入点。

## API Key 当前事实

API Key 已经包含：

- `user_id`、不可变 ID、Key、名称、状态；
- Group、IP 白名单/黑名单；
- `quota`、`quota_used`，其中 `quota=0` 表示无限；
- 过期时间；
- 5 小时、1 天、7 天费用窗口；
- 最后使用时间。

`usage_logs.api_key_id` 为必填，并具有按 Key 与时间查询的索引。用户用量接口已经支持 `api_key_id` 过滤和所有权约束。普通请求的 API Key 并发当前只做实时统计，不做独立门禁；OpenAI WS ingress 另有每 Key 连接数限制。

## Channel 与 Account

Channel 绑定 Group，并提供模型映射、定价、模型限制和协议功能策略。Account 保存真正的上游凭证并承担账号级并发、RPM、额度和健康状态。

一条用量记录应同时回答：

```text
谁调用：user_id + api_key_id
使用什么服务：group_id + requested/upstream model
由谁执行：account_id + channel_id
如何结算：billing type + costs + subscription/user funding
```

## 架构变更原则

优先扩展现有聚合根和共享流程。仅当新状态拥有独立生命周期、权限、事务边界或明显不同的读写模型时，才建立旁路表或新模块。减少上游文本冲突不能成为制造第二状态源的理由。
