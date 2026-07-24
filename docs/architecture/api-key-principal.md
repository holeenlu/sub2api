# API Key Principal Architecture Gate

状态：通过，采用分阶段复用现有聚合根方案。
关联 ADR：`docs/adr/0001-api-key-principal-boundary.md`

## 产品判断

需求应实现，但“每个 API Key 是独立用户”只表示独立调用身份、权限收窄、配额、并发和统计，不创建新的登录、钱包、订阅或渠道所有权。

结论：**合并到现有 API Key 和 Usage 工作流，先产品化已有能力；仅为无法由现有模型表达的状态增加字段或新对象。**

## 当前能力

现有 API Key 已拥有：

- 所属 User、名称、状态和不可变 ID；
- Group 和 IP ACL；
- 总配额、累计用量、过期；
- 5 小时、1 天、7 天窗口额度；
- 最后使用时间；
- 按 Key 用量、日趋势、仪表盘聚合；
- 实时并发统计。

现有 UsageLog 强制保存 `api_key_id`，并支持按 Key/时间查询。管理员用量页已经展示 Key 名称，但管理员 API Key 资源仍不完整。

## 真正缺口

1. `quota=0` 已表示无限，但 API/UI 缺少明确且一致的无限配额表达。
2. 管理员 API Key 仅有局部 Group 更新，不是完整资源。
3. Key 用量能力分散在现有接口，缺少统一详情工作流。
4. 用量日志通过实时关联显示 Key 名称，改名/删除后的历史展示不稳定。
5. 普通 API Key 并发只统计，不限制。
6. 独立模型和端点权限尚无明确状态所有者。
7. 历史删除审计结构存在明文 Key 风险，需要专项治理。

## 方案比较

| 方案 | 优点 | 风险 | 决策 |
| --- | --- | --- | --- |
| 复用现有 APIKey 聚合根 | 概念一致、无双写、认证路径简单 | 少量官方文件可能冲突 | P0 首选 |
| 向核心 APIKey 增加必要字段 | 查询和事务直接 | Ent 生成文件和迁移冲突增加 | 仅在状态无法派生时使用 |
| `custom_api_key_policies` 旁路表 | 文本冲突较少、易删除 | 双状态源、热路径 Join、双写漂移 | 当前不采用 |
| 新建“虚拟用户” | 表面符合独立用户 | 钱包、角色、登录和归属重复 | 否决 |

## 状态所有权决策

| 能力 | 所有者 | 实现策略 |
| --- | --- | --- |
| 无限/有限配额 | APIKey | 继续以 `quota=0` 为权威；API/UI 暴露派生 `unlimited_quota` |
| 已用额度 | APIKey `quota_used` | 沿用现有原子累计和计费路径 |
| User 资金 | User/Subscription | 不复制到 Key |
| Key 用量 | UsageLog `api_key_id` | 复用现有查询，统一详情 API |
| Key 历史展示 | UsageLog 或独立历史快照 | 在写入成本和查询性能评审后决定，不创建策略表 |
| Key 独立并发 | APIKey 配置 + Redis 槽位 | P1，经共享 ConcurrencyHelper 接入 |
| 模型/端点权限 | APIKey | P1；优先少量核心字段或明确子对象，不用泛化 JSON 策略袋 |
| 轮换凭证版本 | 独立 credential 对象 | P1；确有独立生命周期时新增表 |

## 第一阶段

P0-1 不新增 `custom_api_key_policies`，执行：

1. 在现有 Key DTO 和表单中增加派生 `unlimited_quota`；
2. 保持数据库和官方运行时的 `quota=0` 兼容语义；
3. 补齐管理员 API Key CRUD、状态、配额和审计；
4. 统一用户 Key 详情中的统计和日志入口；
5. 审查并治理明文 Key 返回和删除审计；
6. 为历史名称快照做写入/查询基准测试后再确定存储方式。

P0 不改变普通 Key 并发门禁，不增加模型/端点权限，避免一次修改过多热路径。

## 统一接入点

- `APIKeyService`：生命周期、派生配额语义和所有权；
- API Key auth middleware：只读取统一主体，不承载 UI 规则；
- usage billing/repository：用量归属和必要快照；
- usage handler/service：统一 Key 过滤、统计和权限；
- `ConcurrencyHelper`：P1 Key 槽位；
- 现有 `/keys` 页面和 admin 路由：不创建平行 workflow。

## Change amplification 预算

单一业务规则原则上只允许一个领域实现和一个前端表达：

- 无限配额：`APIKey` 领域方法 + DTO 映射 +表单；
- 所有权：`APIKeyService`/usage service 统一验证；
- Key 并发：`ConcurrencyHelper` 统一接入；
- 历史快照：统一 usage 写入点。

如必须修改三个以上协议 Handler，停止实现并重新评审共享接入点。

## 重新评审触发条件

- 官方引入原生 API Key policy/token principal；
- APIKey 核心表字段增长导致职责不清；
- 权限策略具有独立版本、审批或生命周期；
- 热路径无法在不引入复杂 Join 的情况下满足性能；
- P1 权限模型需要表达组合策略而非简单收窄。
