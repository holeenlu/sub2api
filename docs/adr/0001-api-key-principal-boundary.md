# ADR 0001: API Key 独立主体复用现有聚合根

Status: Accepted
Date: 2026-07-24

## Context

产品希望每个 API Key 能独立管理权限、配额、并发、用量和日志，同时项目需要长期跟进官方 `Wei-Shaw/sub2api`。

现有 APIKey 已拥有身份、User、Group、状态、IP ACL、总配额、累计用量、过期和周期额度。UsageLog 已强制关联 `api_key_id`，并存在按 Key 的统计和查询基础。创建通用 `custom_api_key_policies` 会减少部分 Ent 文本冲突，但会把同一个 API Key 拆成两个状态源。

## Decision

APIKey 继续作为 Key 独立主体的聚合根。P0 优先产品化已有能力：

- `quota=0` 继续作为数据库和运行时的无限配额权威语义；
- API/UI 增加派生的 `unlimited_quota` 表达，不新增配额模式账本；
- Key 用量继续以 UsageLog 的 `api_key_id` 为权威归属；
- 补齐现有用户和管理员 Key 工作流，不创建平行页面或 API 体系；
- 历史名称快照在统一 usage 写入点设计，不放入泛化策略表；
- P1 Key 并发通过共享并发服务接入；
- 只有凭证轮换等确有独立生命周期的状态才新增子对象。

## Alternatives considered

### 通用 `custom_api_key_policies`

不采用。它会引入热路径 Join、双写、一致性恢复和两个配置来源。只有未来权限策略获得独立版本、审批或生命周期时重新评估。

### 把 API Key 建模为虚拟 User

否决。它会复制登录、钱包、订阅、角色和所有权，破坏现有资金模型。

### 一次性给 APIKey 增加所有未来字段

不采用。模型、端点、并发和轮换分阶段引入，避免为未验证需求形成策略袋或大量生成文件变更。

## Consequences

优点：

- 维持一个 API Key 状态所有者；
- 最大限度复用现有计费和统计；
- Feature Flag 关闭时自然回到官方行为；
- 减少认证热路径复杂度。

代价：

- 少量 APIKey schema/service/DTO 修改可能与官方更新发生文本冲突；
- 增加字段前必须判断是否可派生；
- 历史名称快照仍需单独性能和事务评审。

## Migration and rollback

P0 的无限配额表达不需要数据库迁移。关闭新 UI/API 字段后，官方 `quota=0` 语义继续工作。后续新增字段或子对象必须提供独立迁移和 Feature Flag。

## Revisit triggers

- 官方提供原生 Token/API Key policy；
- APIKey 权限出现版本、审批和独立生命周期；
- 核心表职责明显膨胀；
- 性能测试证明当前聚合方式无法满足要求；
- 需要跨多个逻辑 Key 共享同一凭证策略。
