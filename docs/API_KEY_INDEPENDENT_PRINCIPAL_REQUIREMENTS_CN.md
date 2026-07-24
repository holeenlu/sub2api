# API Key 独立主体需求规格

状态：需求评审稿
适用项目：Sub2API
参考项目：New-API
目标模块：`/keys`、用户用量、管理员用量日志、错误日志、审计日志、渠道统计

## 1. 背景与目标

当前系统以用户作为登录、归属和资金主体，以 API Key 作为调用凭证。Sub2API 已经能够在大多数成功计费请求中记录 `user_id` 与 `api_key_id`，API Key 也已有名称、状态、分组、IP 规则、总配额、周期限额和用量累计。

本需求要把 API Key 提升为正式的“独立工作负载主体”：每个 Key 都可以独立授权、停用、限额、统计、查询用量和追踪日志。产品体验上可以把每个 API Key 理解为一个独立用户，但它不是新的登录账户，也不默认拥有独立钱包。

目标如下：

1. API Key 可以明确选择“无限配额”或“有限配额”，无限配额仍完整统计用量。
2. 每个 API Key 都有独立的用量概览、明细、错误和审计记录。
3. 用户和管理员均可按 Key 名称、Key ID 或安全的 Key 标识查询。
4. Key 的改名、轮换、禁用和删除不破坏历史归属。
5. Key 的权限只能缩小所属用户的权限，不能绕过用户余额、订阅、角色或分组限制。
6. API Key 与上游渠道保持为两个不同维度，能够回答“谁调用”和“由哪个上游执行”。
7. 个性化实现必须能够长期跟进官方 `Wei-Shaw/sub2api`，并尽量缩小每次上游合并的冲突面和行为回归面。

## 2. 核心模型与边界

系统中的三个主体定义如下：

```text
User（用户）
  = 登录、所有权、角色和资金主体

API Key（密钥）
  = 独立的调用身份、权限、配额、速率和统计主体

Channel / Account（渠道 / 上游账号）
  = 请求路由和实际执行所使用的上游资源
```

一次请求必须能够同时关联：

```text
user_id + api_key_id + account_id/channel_id + request_id
```

“API Key 等同独立用户”包含：

- 独立名称和不可变 ID；
- 独立启用、停用、过期和配额耗尽状态；
- 独立无限/有限配额；
- 独立分组、模型、端点、IP、并发和周期限额；
- 独立请求量、Token、费用、性能和错误统计；
- 独立用量明细、错误记录和管理审计。

“API Key 等同独立用户”不包含：

- 独立登录账号或密码；
- 默认拥有独立钱包、充值或订阅；
- 获得超出所属用户的模型、分组、角色或数据权限；
- 直接拥有或管理上游渠道；
- “无限配额”等同免费调用。

有效权限必须采用交集或更严格限制：

```text
有效模型范围 = 用户/分组允许模型 ∩ API Key 允许模型
有效端点范围 = 用户/分组允许端点 ∩ API Key 允许端点
有效并发数   = min(用户并发上限, API Key 并发上限)
有效资金能力 = 用户余额/订阅可用 ∩ API Key 配额可用
```

## 3. Sub2API 现状与差距

| 能力 | 当前状态 | 本需求处理 |
| --- | --- | --- |
| Key 名称、所属用户、状态、分组 | 已有 | 保留并成为管理和筛选主维度 |
| Key 总配额和累计用量 | 已有，`quota=0` 隐式表示无限 | 增加显式配额模式，消除歧义 |
| 5 小时/1 天/7 天限额 | 已有 | 保留，并在 Key 详情统一展示 |
| Key 过期和最后使用时间 | 已有 | 保留并增加筛选 |
| Key IP 白名单/黑名单 | 已有 | 保留并纳入权限页和审计 |
| 用量日志关联 Key | 已有，`api_key_id` 为必填 | 补历史名称/前缀快照和全入口覆盖证明 |
| 用户按 Key 查询日用量 | 已有基础接口 | 扩展完整概览、明细、错误和导出 |
| 管理员用量表展示 Key 名称 | 部分已有 | 增加一等筛选、详情跳转和稳定历史展示 |
| 管理员全局 Key 管理 | 不完整 | 新增完整资源、权限和批量操作 |
| Key 改名/删除后的历史显示 | 依赖实时关联，语义不稳定 | 日志写入名称和掩码前缀快照 |
| Key 轮换 | 缺少完整生命周期 | 增加轮换和可选凭证版本模型 |
| Key 独立模型/端点/并发限制 | 不完整 | 分阶段补齐 |

现有 `quota=0` 已具有无限配额的运行行为，并且 `quota_used` 会继续累计。本需求不是重新发明该能力，而是将其改成显式、可管理、可审计且不易误配置的产品语义。

## 4. 功能需求

### 4.1 API Key 生命周期

#### KEY-001 创建 Key（P0）

用户可在 `/keys` 创建 Key，至少配置：

- 名称，必填，同一用户下建议唯一；
- 所属分组；
- 配额模式，默认由系统配置决定，推荐默认为无限；
- 有限配额金额；
- 过期时间；
- IP 白名单和黑名单；
- 5 小时、1 天、7 天周期限额；
- 状态。

创建成功只返回一次完整 Key。后续列表和普通详情只能返回掩码值，例如 `sk-abcd****wxyz`。

#### KEY-002 编辑与状态（P0）

用户可以修改自己 Key 的名称、权限、限额和过期时间，并可启用或停用。管理员可在权限范围内执行相同操作。

状态至少包括：

- `active`：可使用；
- `disabled`：主动停用；
- `expired`：已过期，可由时间动态判断或持久化；
- `quota_exhausted`：有限配额已耗尽；
- `deleted`：软删除，不再认证。

手动停用状态的优先级高于自动状态。增加配额、重置已用量或切换无限后，可以按规则从 `quota_exhausted` 恢复为 `active`，但不能自动覆盖手动 `disabled`。

#### KEY-003 删除（P0）

删除采用软删除。删除后：

- 立即失去认证能力并清理认证缓存；
- Key 行及不可变 ID 保留，以维持历史外键和审计链；
- 原始 Key 不得写入删除审计、应用日志或错误信息；
- 历史用量继续显示当时的 Key 名称和安全前缀；
- 用户默认列表不显示，管理员可按“已删除”筛选查看元数据。

#### KEY-004 轮换（P1）

用户或管理员可轮换 Key。新 Key 只显示一次；旧 Key 可以选择立即失效或在受控宽限期后失效。轮换不改变逻辑 `api_key_id`，因此统计连续。

如果后续需要同时支持多个短期有效凭证，应引入 `api_key_credentials`，使用 `credential_id/version` 区分凭证版本；不能通过复制 API Key 业务行模拟轮换。

#### KEY-005 批量操作（P1）

管理员可以批量启用、停用和删除 Key。系统必须先校验整批对象和权限，再在事务内一次提交；任一对象不合法时整批不产生部分修改。

### 4.2 无限与有限配额

#### QUOTA-001 显式配额模式（P0）

数据模型增加显式 `quota_mode`：

```text
unlimited = Key 自身不设置总配额上限
limited   = Key 受 quota 限制
```

也可以使用 `unlimited_quota boolean` 实现，但对外 API 和数据库必须只有一个权威语义。推荐使用枚举，便于未来扩展周期预算等模式。

迁移规则：已有 `quota <= 0` 的 Key 映射为 `unlimited`；已有 `quota > 0` 的 Key 映射为 `limited`。

#### QUOTA-002 无限配额行为（P0）

无限配额仅绕过 Key 自身的总配额可用性检查：

- 每次请求仍累计 `quota_used`；
- 每次请求仍产生用量和计费日志；
- 用户余额或订阅不足时仍必须拒绝；
- 仍受 Key 周期限额、过期、状态、IP、模型、并发等规则约束；
- 仍按正常规则向所属用户余额或订阅结算；
- 不得因为累计值增加而自动进入 `quota_exhausted`。

因此，用户曾遇到的 `INSUFFICIENT_BALANCE` 不能通过把 Key 设置为无限配额来规避。

#### QUOTA-003 有限配额行为（P0）

有限配额使用累计生命周期用量判断：

```text
remaining = max(quota - quota_used, 0)
```

在请求进入上游前，应根据估算费用进行原子预占，防止多个并发请求同时通过检查造成超卖。上游已经产生的权威用量必须完成结算，即使最终使有限 Key 出现少量负余量；后续请求应失败关闭。

#### QUOTA-004 模式切换（P0）

- `limited -> unlimited`：不重置 `quota_used`，解除 Key 总配额门禁；若当前仅因配额耗尽而停用，则恢复为 `active`。
- `unlimited -> limited`：不重置 `quota_used`；新 `quota` 与历史累计比较。若 `quota <= quota_used`，立即进入 `quota_exhausted`。
- 修改配额和重置用量是两个独立操作，不得在普通编辑中隐式清零。
- `quota`、`quota_used` 和结算金额禁止负数、NaN 和无穷值。

#### QUOTA-005 重置已用量（P0）

重置必须使用独立接口和二次确认。操作写入审计事件，包含操作者、Key ID、重置前值、重置后值、理由、时间和请求 ID，但不记录原始 Key。

重置只改变 Key 配额计数器，不删除历史用量日志，也不改变用户已消费金额。

#### QUOTA-006 周期限额（P1）

现有 5 小时、1 天、7 天限额继续独立生效。详情页同时显示周期、额度、已用、剩余和窗口重置时间。无限总配额不代表周期限额无限。

窗口计数需要跨实例原子一致；Redis 可以做加速，但数据库或可恢复的权威账本必须能够防止缓存丢失后放大额度。

### 4.3 独立权限

#### PERM-001 所有权与隔离（P0）

普通用户只能读取和修改自己拥有的 Key，只能查询自己 Key 的统计和日志。所有用户接口必须同时验证：

```text
api_key.user_id = 当前登录 user_id
```

禁止只凭客户端传入的 `api_key_id` 查询。跨用户请求返回 404 或空集，不能泄露 Key 是否存在。

#### PERM-002 分组和模型（P1）

每个 Key 可单独选择分组和模型允许列表。Key 未配置模型限制时继承用户/分组权限；配置后取交集。Key 不能通过指定更高权限分组扩大所属用户能力。

#### PERM-003 端点和协议（P1）

支持按 Key 控制允许的入口，例如：

- Chat Completions；
- Responses；
- Embeddings；
- Images；
- Gemini 兼容入口；
- WebSocket / Realtime；
- 异步任务和批处理。

默认继承现有行为，升级后不能意外阻断历史 Key。

#### PERM-004 IP、并发和速率（P1）

保留 IP 白名单/黑名单，并增加 Key 独立并发、RPM、TPM（如产品启用）。最终限制取用户和 Key 中更严格者。权限拒绝应产生安全/错误事件，但不得包含请求密钥明文。

### 4.4 独立统计

#### STAT-001 Key 概览（P0）

每个 Key 至少提供今日、本月、所选时间和累计统计：

- 请求总数、成功数、失败数、成功率；
- 输入、输出、缓存创建、缓存读取 Token；
- 图片、视频等非文本用量；
- 标价成本 `total_cost` 和实际扣费 `actual_cost`；
- 当前总配额模式、已用和剩余；
- 5 小时、1 天、7 天窗口用量；
- 最后使用时间。

#### STAT-002 分布和性能（P1）

Key 详情提供：

- 按模型、请求端点、渠道/账号、状态码、IP 的分布；
- RPM、TPM 趋势；
- 请求耗时和首 Token 耗时的 P50/P95；
- 错误类型和错误率趋势。

API Key 维度与渠道维度必须同时保留。Key 表示调用方，渠道表示上游执行资源。

#### STAT-003 对账一致性（P0）

在相同口径和时间范围内：

```text
某用户所有 Key 的 actual_cost 之和 = 该用户 Key 调用产生的 actual_cost
```

聚合 Key 统计不能再次扣费，也不能将同一请求重复计入。计费去重键继续至少包含 `(request_id, api_key_id)`。

#### STAT-004 导出（P1）

用户可导出自己 Key 的明细，管理员可按权限导出全局结果。导出使用当前筛选条件，支持异步生成和进度提示。导出文件不得包含原始 Key、上游密钥、访问令牌或请求敏感正文。

### 4.5 用量、错误与审计日志

#### LOG-001 用量明细（P0）

每条可计费用量记录必须包含：

- `request_id`；
- `user_id`；
- `api_key_id`；
- `api_key_name_snapshot`；
- `api_key_prefix_snapshot`；
- `account_id` 和可用时的 `channel_id`；
- 请求模型、实际上游模型、模型映射链；
- 输入/输出/缓存/图片/视频用量；
- 标价成本、实际成本、倍率和计费类型；
- 请求类型、入口、流式标记；
- 耗时、首 Token 耗时、IP、User-Agent；
- 创建时间。

快照在事件产生时写入，Key 后续改名、轮换或删除不能改写历史含义。所有关联和权限判断仍使用不可变 `api_key_id`，名称只用于展示。

#### LOG-002 错误日志（P0）

认证后发生的网关错误至少关联 `user_id` 和 `api_key_id`。认证失败但能够通过安全指纹识别已知 Key 时，可以关联 Key ID；无法安全识别时记录为匿名入口错误。

错误详情包含请求 ID、Key 快照、模型、入口、阶段、错误分类、HTTP 状态、上游渠道（如已选择）、时间和脱敏消息。不得保存完整 Key 或上游响应中的秘密。

#### LOG-003 拒绝与零费用请求（P1）

以下情况即使没有产生计费用量，也应进入独立的安全/运营事件流：

- Key 停用、过期或配额耗尽；
- 用户余额/订阅不足；
- IP、模型、端点、并发、RPM/TPM 或周期额度拒绝；
- 内容策略拒绝；
- 上游选择前失败。

这些事件不得伪造成成功用量，也不得影响消费对账。

#### LOG-004 管理审计（P0）

记录创建、改名、权限修改、启停、配额模式切换、额度修改、用量重置、轮换和删除。审计值采用结构化差异并屏蔽敏感字段。

#### LOG-005 查询条件（P0）

用户和管理员用量日志至少支持：

- Key ID；
- Key 名称；
- 安全掩码前缀；
- 粘贴完整 Key 后的精确指纹匹配；
- 所属用户（仅管理员）；
- 请求 ID；
- 模型；
- 渠道/账号；
- 请求类型和入口；
- 成功/错误状态；
- 起止时间。

Key 名称允许精确或模糊搜索。完整 Key 搜索必须先在服务端计算指纹并精确匹配，禁止对原始 Key 做 SQL `LIKE`。

### 4.6 管理端 Key 管理

#### ADMIN-001 全局列表（P0）

新增管理员 Key 管理页，表格列建议为：

```text
名称 | 所属用户 | 状态 | 掩码 Key | 配额模式 | 已用/剩余
分组 | 模型限制 | IP 规则 | 创建时间 | 最后使用 | 操作
```

筛选支持名称、Key ID、安全 Key 标识、用户、状态、分组、模型、有限/无限、创建/最后使用时间和已删除状态。

#### ADMIN-002 Key 详情（P0）

详情页包含六个页签：

1. 概览；
2. 权限与限制；
3. 用量趋势和分布；
4. 用量明细；
5. 错误日志；
6. 审计事件。

从管理员用量日志中的 Key 名称可以跳转到该详情；删除的 Key 仍可打开只读历史详情。

#### ADMIN-003 权限边界（P0）

管理员管理范围必须由现有角色体系明确决定。若沿用 New-API 的特权令牌边界，则管理员/Root 只能管理被授权范围内的特权 Key，普通用户 Key 不能因“全局列表”被默认暴露完整凭证。

无论角色如何配置，列表和普通详情只返回掩码 Key。若产品坚持支持再次查看完整 Key，必须使用单独的高权限接口、二次认证、审计和限速；安全上更推荐只在创建/轮换时显示一次。

## 5. 数据模型设计

本项目需要长期合并官方更新，但“降低文本冲突”不能以制造第二状态源为代价。Architecture Gate 已决定继续以现有 `APIKey` 作为独立主体聚合根：能从现有字段派生的语义不新增存储；确实具有独立生命周期的状态才新增子对象；只有经过性能和事务评审后才采用旁路扩展表。

推荐优先级：

```text
复用现有字段和共享 service
  > 为无法派生的稳定状态增加少量核心字段
    > 为独立生命周期增加子对象
      > 泛化旁路策略表
```

### 5.1 API Key 聚合根策略

P0 不新增 `custom_api_key_policies`，继续使用现有 `api_keys`：

```text
quota = 0   -> unlimited
quota > 0   -> limited
```

API 和 UI 暴露派生字段 `unlimited_quota`，但不新增第二个配额模式账本。`quota_used`、状态、过期和周期限额继续由现有 APIKey 和计费路径拥有。

P1 的模型、端点和并发权限只有在产品语义稳定后才引入。简单且稳定的 Key 自有状态可以增加少量核心字段；凭证轮换等具有独立生命周期的状态使用子对象。不得把所有未来能力提前塞入泛化 JSON 策略袋。

约束：

- `quota >= 0`、`quota_used >= 0`；
- 指纹使用带服务端 pepper 的 HMAC-SHA-256 或等价方案；
- 原始 Key 如因兼容性暂时保存在数据库，必须限制读取路径，并规划迁移为不可逆摘要认证；
- 禁止任何新的删除审计表保存原始 Key 明文。

### 5.2 用量日志快照策略

历史快照有两种候选实现，Architecture Gate 暂不预选：

```sql
方案 A：在 usage_logs 增加 api_key_name_snapshot / api_key_prefix_snapshot
方案 B：使用按 api_key_id + 生效时间维护的名称/凭证历史子对象
```

最终方案必须比较写入放大、历史查询性能、删除保留、事务一致性和官方合并成本。无论采用哪种方式，都只能在统一 usage 写入/查询接入点实现，不能在每个 Chat/Responses/Images handler 分别复制逻辑。

若采用方案 A，增加：

```sql
api_key_name_snapshot   varchar(100) null
api_key_prefix_snapshot varchar(24) null
```

若采用方案 B，历史对象必须有唯一所有者并覆盖改名、轮换和删除；不能成为第二份 Key 权限或配额状态。

旧记录可以从仍存在的 `api_keys` 回填一次；无法可靠恢复时显示“历史 Key #ID”，不能伪造当前名称。新记录必须写入快照。

建议补充索引：

```sql
(api_key_id, created_at desc, id desc)
(user_id, api_key_id, created_at desc)
```

名称快照不是主要查询键，不建议为高基数历史名称盲目建立普通索引；按名称查询先解析 Key ID 集合，再按 ID 查询日志。

### 5.3 `custom_api_key_credentials`（P1，可选）

如果实现带宽限期轮换：

```text
id, api_key_id, version, fingerprint, prefix,
status, valid_from, valid_until, created_at, revoked_at
```

用量日志可选增加 `api_key_credential_id`，以区分轮换版本，但业务统计仍聚合到 `api_key_id`。

### 5.4 审计事件（复用优先）

使用现有审计基础设施或新增统一资源事件，至少包含：

```text
event_id, action, actor_user_id, target_api_key_id,
before_redacted, after_redacted, reason, request_id, ip, created_at
```

## 6. 后端 API 建议

### 6.1 用户接口

保留现有 `/api/v1/user/api-keys` CRUD，并扩展：

```text
POST /api/v1/user/api-keys/:id/enable
POST /api/v1/user/api-keys/:id/disable
POST /api/v1/user/api-keys/:id/rotate
POST /api/v1/user/api-keys/:id/reset-usage
GET  /api/v1/user/api-keys/:id/stats
GET  /api/v1/user/api-keys/:id/usage
GET  /api/v1/user/api-keys/:id/errors
GET  /api/v1/user/api-keys/:id/audit-events
POST /api/v1/user/api-keys/:id/usage/export
```

现有日用量接口可保留兼容，但最终由统一 stats/usage 能力覆盖。

### 6.2 管理员接口

```text
GET    /api/v1/admin/api-keys
POST   /api/v1/admin/api-keys
GET    /api/v1/admin/api-keys/:id
PUT    /api/v1/admin/api-keys/:id
DELETE /api/v1/admin/api-keys/:id
POST   /api/v1/admin/api-keys/:id/enable
POST   /api/v1/admin/api-keys/:id/disable
POST   /api/v1/admin/api-keys/:id/rotate
POST   /api/v1/admin/api-keys/:id/reset-usage
GET    /api/v1/admin/api-keys/:id/stats
GET    /api/v1/admin/api-keys/:id/usage
GET    /api/v1/admin/api-keys/:id/errors
GET    /api/v1/admin/api-keys/:id/audit-events
POST   /api/v1/admin/api-keys/batch-enable
POST   /api/v1/admin/api-keys/batch-disable
POST   /api/v1/admin/api-keys/batch-delete
```

### 6.3 API 响应约定

Key DTO 至少返回：

```json
{
  "id": 123,
  "name": "生产环境",
  "masked_key": "sk-abcd****wxyz",
  "key_prefix": "sk-abcd",
  "status": "active",
  "quota_mode": "unlimited",
  "quota": null,
  "quota_used": 25.38,
  "quota_remaining": null,
  "is_unlimited": true
}
```

`null` 表示不适用或无限，不能混用 `0`、`-1` 和空字符串表达同一含义。金额序列化精度必须与数据库和现有计费口径一致。

## 7. 前端需求

### 7.1 用户 `/keys`

- 创建/编辑表单提供清晰的“无限配额”开关；打开后隐藏或禁用总配额输入，但继续展示累计用量。
- 列表区分“无限”和具体剩余额度，不把无限显示为 `$0`。
- 展示今日、累计实际费用、周期窗口、过期时间和最后使用时间。
- 名称可点击进入 Key 详情。
- 完整 Key 只在创建或轮换完成弹窗显示一次，要求用户确认已复制。
- 配额重置、轮换和删除使用独立确认流程。

### 7.2 用户 Key 详情

提供概览、权限、用量明细、错误和审计页签。页面所有请求使用 Key ID，不使用名称作为身份。日志表支持日期、模型、请求 ID、渠道、入口和结果筛选。

### 7.3 管理员用量日志

- 用户和 API Key 是两列、两个筛选维度；
- Key 列展示事件快照名称及掩码前缀；
- 同时展示渠道/账号列，不与 Key 混淆；
- 可从用户、Key、渠道分别跳转对应详情；
- 删除 Key 后不出现空白名称。

## 8. 认证与计费不变量

以下是不允许通过实现细节弱化的核心合同：

1. 数据库是 Key 状态、过期、配额和资金可用性的权威来源；缓存只能加速，缓存缺失或过期必须安全回源。
2. Key 配额账本与用户钱包/订阅账本相互独立；两者都通过才允许请求进入上游。
3. 无限 Key 绕过 Key 总额度门禁，但绝不绕过用户资金门禁。
4. 有限 Key 的预占必须为数据库原子条件更新，避免并发超卖。
5. 上游已实际产生的正向用量必须记录，不能因最终费用超过估算而丢失。
6. 失败尝试最多退款一次；已结算请求不得重复扣费。
7. Key 配额预占失败时，用户资金预占不得残留；用户资金预占失败时，Key 预占必须回滚。
8. Redis 故障不能使有限 Key 获得额外额度，也不能令已停用 Key 被长期接受。
9. 每个网关入口必须在统一结算路径写入同一个 `api_key_id`。
10. 统计查询是只读聚合，不产生任何二次扣费。

推荐的请求顺序：

```text
认证 Key（DB 权威状态）
  -> 校验用户与 Key 权限交集
  -> 原子预占 Key 有限配额（无限则只建立会话）
  -> 原子预占用户余额/订阅
  -> 调用上游
  -> 按权威用量统一结算
  -> 写入 usage_log + Key 快照
  -> 更新聚合与缓存
```

## 9. 安全与隐私

- 原始 Key 只用于认证，不得进入访问日志、用量日志、错误日志、审计差异、埋点、导出和告警。
- 应用日志只允许记录 `api_key_id`、名称快照和短掩码前缀。
- 列表和普通详情不得返回数据库中的 `key` 字段。
- 粘贴完整 Key 搜索时，前端不得在 URL query 中传递，应使用 HTTPS POST 请求体；服务端立即计算指纹，不记录请求体。
- 创建/轮换的完整 Key 响应设置 `Cache-Control: no-store`。
- Key 权限修改和敏感查看需要 CSRF、防重放、限速和审计。
- CSV 注入字符需要转义；导出遵守数据保留和管理员权限边界。
- 对历史 `deleted_api_key_audits` 等结构做专项检查，禁止继续写入明文 Key，并制定安全清理或加密迁移方案。

## 10. 迁移与兼容

### 阶段 A：数据与兼容双写（P0）

1. 增加 `quota_mode`、Key 指纹/前缀和日志快照字段。
2. 按 `quota <= 0` 回填无限模式，按 `quota > 0` 回填有限模式。
3. 为现有 Key 生成指纹和前缀；迁移过程不得把 Key 输出到命令日志。
4. 从仍存在的 Key 回填历史日志快照，无法恢复时保留空值并由 UI 显示 `历史 Key #ID`。
5. 新代码先双写旧字段和新字段，认证行为保持兼容。

### 阶段 B：一等统计与 UI（P0）

1. 上线用户 Key 详情、筛选和导出基础能力。
2. 上线管理员全局 Key 管理及日志筛选。
3. 对所有网关入口做 API Key 归属覆盖审计。

### 阶段 C：权限与轮换增强（P1）

1. 模型、端点、并发、RPM/TPM 独立限制。
2. 凭证版本化轮换。
3. 高级统计、错误分布和异步导出。

迁移必须可滚动发布：旧实例看见新记录时仍能按 `quota` 工作，新实例在回填完成前可从旧 `quota` 推导模式。完成一致性验证后，`quota_mode` 才成为唯一权威字段。

## 11. 验收标准

### 11.1 配额和计费

- AC-Q01：无限 Key 连续调用后 `quota_used`、明细和用户扣费均增加，Key 不进入 `quota_exhausted`。
- AC-Q02：无限 Key 所属用户余额不足时返回资金不足，且不调用上游。
- AC-Q03：有限 Key 在高并发下的成功预占总额不超过可用额度；不得通过先读后写实现。
- AC-Q04：上游已产生的权威用量高于估算时，完整用量仍被记录，Key 后续失败关闭。
- AC-Q05：有限切无限不清零历史用量；无限切有限且额度不高于已用量时立即耗尽。
- AC-Q06：重置 Key 用量不删除日志、不返还用户余额，并产生脱敏审计事件。
- AC-Q07：同一请求重试、回调或重复结算不会双扣，失败尝试不会双退。

### 11.2 日志和统计

- AC-L01：Key 改名后，旧日志显示旧名称快照，新日志显示新名称。
- AC-L02：Key 删除后历史日志仍显示快照和 Key ID，且 Key 不能认证。
- AC-L03：按 Key ID 查询只返回该 Key；普通用户传入他人 Key ID 不获得任何数据。
- AC-L04：用户各 Key 费用之和与用户 Key 调用费用一致，无重复统计。
- AC-L05：日志可同时按 Key 与渠道筛选，两者统计维度互不替代。
- AC-L06：完整 Key、数据库明文 Key和上游密钥不出现在 API 普通响应、日志、审计和导出中。
- AC-L07：成功请求、上游错误、权限拒绝和余额不足都能在对应日志流中按 Key 查询。

### 11.3 入口覆盖

以下入口必须逐一验证 Key 归属、配额门禁、结算、错误和快照：

- Chat Completions；
- Responses HTTP；
- Responses WebSocket / Realtime；
- Embeddings；
- Images；
- Gemini 兼容入口；
- 异步图片/视频任务；
- 批处理和其他已有计费入口。

任何新增网关入口必须通过同一合同测试后才能上线。

### 11.4 安全和权限

- AC-S01：列表、普通详情和日志响应只有掩码 Key。
- AC-S02：完整 Key 搜索仅做指纹精确匹配，数据库查询和应用日志不出现原始值。
- AC-S03：批量操作在任一对象越权或无效时全部回滚。
- AC-S04：Key 的模型、端点、并发权限不能超过所属用户。
- AC-S05：Redis 清空或不可用后，停用、过期和有限配额仍正确生效。

## 12. 测试策略

除常规单元、集成和端到端测试外，计费关键合同需要“变异式证明”，即测试必须能杀死以下故意错误：

- 删除无限 Key 的 `quota_used` 累计；
- 让无限 Key 绕过用户余额检查；
- 将有限配额原子条件更新改成先查后写；
- 删除某一个网关入口的 `APIKeyID` 赋值；
- 将 `(request_id, api_key_id)` 去重改成只按 `request_id`；
- 退款路径执行两次；
- Key 改名后日志改用实时名称；
- 移除普通用户查询中的所有权条件；
- 将掩码字段误替换为原始 `key`；
- 把 Key 和渠道 ID 混作同一统计维度。

建议测试层次：

1. 领域测试：配额模式、状态转换、权限交集。
2. PostgreSQL 集成测试：并发预占、结算、去重、事务回滚和索引查询。
3. 路由合同测试：所有入口均注入和记录 Key ID。
4. API 权限测试：用户隔离、管理员范围和批量原子性。
5. 前端测试：无限显示、筛选、删除历史展示和敏感字段不渲染。
6. 对账测试：Key 聚合、用户聚合与账本在相同口径下相等。

## 13. 非功能要求

- Key 明细默认按 `(created_at desc, id desc)` 游标或稳定分页，不能因同时间戳丢行或重复。
- 常用 Key 时间范围查询在目标数据规模下 P95 不高于现有用户日志查询的 1.2 倍。
- 聚合查询避免逐 Key N+1；批量仪表盘继续使用批量聚合接口。
- 用量记录保持只追加；修正通过补偿事件或受控后台任务完成。
- 所有时间以数据库 `timestamptz` 保存，API 使用 ISO 8601，前端按用户时区展示。
- 金额计算沿用项目权威精度，禁止在关键账本中使用前端浮点值作为结算依据。
- 日志保留、归档和清理必须保留 Key ID 和快照的一致性。

## 14. 不在本期范围

- 为每个 Key 创建独立登录账号；
- 为每个 Key 单独充值、支付或购买订阅；
- 允许 Key 自主管理渠道；
- 将 Key 名称当成唯一、不可变身份；
- 用 Key 的无限配额覆盖用户资金限制；
- 在管理列表默认暴露完整 Key。

## 15. 建议交付拆分

### P0：可完整使用和对账

- 显式无限/有限配额和迁移；
- 配额状态转换、独立重置及审计；
- 用量日志 Key 名称/前缀快照；
- 用户 Key 用量明细和完整筛选；
- 管理员全局 Key 列表、详情和筛选；
- 错误日志按 Key 查询；
- 全网关入口归属和计费合同测试；
- 敏感 Key 脱敏和明文审计治理。

### P1：完整独立工作负载主体

- Key 独立模型、端点、并发、RPM/TPM；
- 轮换与凭证版本；
- 高级性能/错误统计；
- 批量管理和异步导出；
- 周期限额权威账本加固。

## 16. 参考依据

New-API：

- `SOURCE_CHANGES_FOR_AUTHOR.md`：特权令牌管理、完整 Key 权限边界、批量删除原子性和凭证脱敏。
- `docs/adr/0003-unified-relay-billing-attempt-loop.md`：令牌配额与用户资金分账、有限令牌原子预占、无限令牌仍计量、已发生用量必须结算、单次退款和防双扣。
- `model/token.go`：`UnlimitedQuota`、`RemainQuota`、`UsedQuota`、模型/IP/分组等令牌属性。
- `model/log.go`：`TokenId`、`TokenName` 作为日志和筛选维度。
- `web/src/features/keys`：无限配额、掩码 Key、剩余/已用额度和权限编辑交互。
- `web/src/features/usage-logs`：Token、用户、模型、渠道、请求 ID 和时间筛选。
- `model/channel.go` 与 `web/src/features/channels`：渠道作为独立上游资源维度。

Sub2API：

- `backend/ent/schema/api_key.go`：现有 Key 身份、分组、IP、配额、周期限额和过期模型。
- `backend/ent/schema/usage_log.go`：现有 `user_id`、`api_key_id`、`account_id/channel_id` 和详细计费字段。
- `backend/internal/server/middleware/api_key_auth.go`：Key 认证与用户/分组上下文。
- `backend/internal/service/gateway_usage_billing.go`：请求结算时的 Key 用量累计和日志归属。
- `backend/internal/repository/usage_billing_repo.go`：计费去重和 Key 配额原子累计。
- `backend/internal/handler/usage_handler.go`：已有用户 Key 所有权校验、日用量和批量统计基础。

## 17. 官方上游同步与低冲突约束

### 17.1 仓库关系

长期仓库关系固定为：

```text
upstream = https://github.com/Wei-Shaw/sub2api.git
           只用于获取官方更新

origin   = https://github.com/holeenlu/sub2api.git
           用于推送个性化代码和发布分支
```

当前本地仓库在本需求评审时只有 `origin`，且 fetch/push 都指向官方仓库。实施前应把官方仓库改名为 `upstream`，再把自有仓库配置为 `origin`；同时把 `upstream` 的 push URL 设置为不可推送地址或通过权限策略阻止误推。

### 17.2 分支与同步流程

推荐分支模型：

```text
upstream/main
  = 官方只读基线

origin/main
  = 已合并官方更新并包含个性化功能的稳定分支

custom/<feature>
  = 单项个性化需求分支

sync/upstream-YYYYMMDD
  = 每次吸收官方更新的临时集成分支
```

每次同步流程：

1. `fetch upstream` 获取官方提交，不直接修改 `upstream/main`。
2. 从 `origin/main` 创建 `sync/upstream-YYYYMMDD`。
3. 将 `upstream/main` 合并到同步分支；已公开的个性化提交默认使用 merge，不重写历史。
4. 解决冲突后执行迁移、后端、前端、API 合同、计费和安全测试。
5. 通过 PR 合并到 `origin/main`，保留官方 commit 关系和冲突解决记录。
6. 个性化功能继续从最新 `origin/main` 创建短生命周期功能分支。

禁止长期维护一个与官方完全脱离的大型重写分支，也禁止复制官方核心模块形成第二套平行实现。

### 17.3 低冲突代码原则

#### SYNC-001 新增优于改写（P0）

优先复用现有聚合根和 service，并通过独立文件、handler 和前端 feature 目录补齐能力。只有新状态具有独立生命周期时才新增扩展表。对官方文件的修改尽量缩减为注册、依赖注入或一次统一 Hook。

#### SYNC-002 统一接入点（P0）

API Key 独立主体功能最多在以下共享接入层形成窄改动：

- API Key 认证完成后的统一权限解析；
- 用户与管理员路由注册；
- 统一用量提交/结算事件；
- 统一并发槽位帮助器；
- 前端路由和导航注册。

禁止在 Chat、Responses、Gemini、Images、WebSocket 等每个 handler 复制 Key 权限、快照、并发或审计逻辑。入口覆盖由共享合同测试证明。

#### SYNC-003 不修改生成文件（P0）

除非官方数据模型必须增加字段，否则不直接编辑 Ent 生成文件、OpenAPI 生成文件或其他机械生成物。需要扩展官方实体时，优先使用独立 SQL migration 和扩展 repository。

#### SYNC-004 兼容默认关闭（P0）

新增能力使用 Feature Flag 和向后兼容默认值。未启用时应保持官方行为：

- 原有 Key 不需要立即补配置；
- `quota=0` 继续被官方代码理解为无限；
- 未配置 Key 独立并发时继续只受用户并发限制；
- 可选扩展数据不存在或未回填期间不得错误放宽权限。

安全相关回退必须 fail-closed；展示性统计扩展可以降级为不显示，不能阻断核心网关。

#### SYNC-005 数据库对象命名空间（P0）

个性化新增表、索引、触发器、设置键和 Feature Flag 使用稳定前缀，例如 `custom_` 或项目最终确定的命名空间，避免未来与官方新增对象重名。每个迁移只做单一职责并保持幂等或可检测状态。

#### SYNC-006 小型原子提交（P0）

个性化修改按“迁移、领域模型、接口、前端、测试”拆成可审查的原子提交。不得把格式化整个官方文件、无关重命名和需求实现混在同一提交中。

#### SYNC-007 冲突预算（P0）

每个个性化功能在设计评审时提交“官方触点清单”，至少说明：

```text
修改了哪些官方文件
每个修改点为什么不能旁路实现
是否位于高频更新的认证/调度/计费热路径
官方合并时的回归测试
未来官方提供同类功能时如何退役本地实现
```

原则上一个子功能不应修改多个协议 handler；如必须修改三个以上官方热路径文件，需要单独 ADR 批准。

#### SYNC-008 可退役性（P1）

本地扩展必须有清晰边界。当官方未来实现相同能力时，可以通过关闭 Feature Flag、迁移数据和删除独立包来退役，不能让本地业务规则不可逆地散落在官方核心逻辑中。

### 17.4 API Key 功能的低冲突落点

Architecture Gate 已决定 P0 不预建通用 `custom/apikeyprincipal` 包。优先沿用现有 `APIKeyService`、usage service/repository 和 handler 组织，只把确有独立职责的新增能力放入独立文件：

```text
backend/internal/service/
  api_key*.go

backend/internal/handler/
  api_key*.go

backend/internal/handler/admin/
  apikey*.go

frontend/src/features/api-key-principal/
  api/
  components/
  views/
  types/

backend/internal/repository/
  usage_log_repo_*.go
```

与官方代码的预期最小触点：

| 官方接入点 | 允许的最小修改 | 避免的修改 |
| --- | --- | --- |
| API Key 认证 | 复用 APIKeyService 的统一领域语义 | 重写认证中间件 |
| 并发帮助器 | 在用户槽位后获取可选 Key 槽位 | 每个协议单独加 Key 并发 |
| 用量提交 | 统一发布 Key 快照/审计事件 | 每个 handler 单独写快照 |
| 路由 | 注册独立用户/管理员路由组 | 扩大现有 handler 职责 |
| 前端 | 独立 feature 目录，路由和导航单点接入 | 在大型页面复制整套逻辑 |

### 17.5 上游兼容验收标准

- AC-U01：全新官方版本可在同步分支完成合并，个性化扩展不要求复制或替换官方核心模块。
- AC-U02：关闭全部个性化 Feature Flag 后，关键 API 行为与对应官方基线一致。
- AC-U03：Key 独立权限、并发、日志快照均由共享接入点覆盖，新增协议入口不需要复制完整逻辑。
- AC-U04：扩展迁移不会与官方表、索引和设置键重名。
- AC-U05：CI 能在临时分支合并最新 `upstream/main` 并报告文本冲突、编译失败、迁移冲突和合同测试回归。
- AC-U06：每次同步官方版本后，有限/无限 Key、用户余额、并发、计费去重、退款和日志归属测试全部通过。
- AC-U07：官方提供同类能力时，存在已记录的数据迁移和本地扩展退役路径。
