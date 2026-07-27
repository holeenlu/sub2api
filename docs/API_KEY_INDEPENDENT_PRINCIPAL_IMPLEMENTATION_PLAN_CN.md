# API Key 独立主体开发实施计划

状态：开发规划稿
关联需求：[API Key 独立主体需求规格](API_KEY_INDEPENDENT_PRINCIPAL_REQUIREMENTS_CN.md)
目标：在保持官方上游可持续同步的前提下，把 API Key 做成独立的权限、配额、并发、统计和日志主体。

## 1. 总体实施策略

采用“先建立架构治理基线，再产品化现有 APIKey 聚合根，最后通过统一接入点补齐缺口”的方式。第一阶段不创建泛化旁路策略表，不重写官方认证、调度和计费核心，不在每个协议 Handler 中复制逻辑。

```text
官方 API Key 认证
  → API Key 主体语义解析
    → 用户权限 ∩ Key 权限
      → 用户/Key 配额门禁
        → 官方网关调度与计费
          → 统一用量事件
            → Key 统计、快照和审计扩展
```

核心边界：

- User 仍是登录、所有权和资金主体；
- API Key 是独立工作负载、权限、配额、并发和统计主体；
- Account/Channel 仍是上游执行资源和路由策略；
- Key 无限配额不绕过用户余额或订阅；
- 所有新增能力都必须能够关闭并恢复官方基线行为。

## 2. 依赖关系和阶段总览

| 阶段 | 目标 | 依赖 | 交付结果 |
| --- | --- | --- | --- |
| Phase 0 | 上游基线和工作区隔离 | Git remote 已配置 | 可重复同步的开发基线 |
| Phase 1 | 架构基线与现有模型契约 | Phase 0 | 架构文档、ADR、派生语义、Feature Flag |
| Phase 2 | P0 配额与权限闭环 | Phase 1 | 显式无限配额、所有权隔离、审计 |
| Phase 3 | P0 统计和日志闭环 | Phase 1、2 | Key 详情、明细、错误、管理查询 |
| Phase 4 | P0 计费与全入口验证 | Phase 2、3 | 并发安全、对账一致、入口覆盖 |
| Phase 5 | P1 独立主体增强 | P0 稳定 | Key 独立并发、模型/端点限制、轮换 |
| Phase 6 | 灰度、同步和退役机制 | P0/P1 | 可持续跟随官方的发布流程 |

P0 完成后才适合正式启用 API Key 独立配额和统计；P1 可以在生产稳定运行后继续迭代。

## 3. Phase 0：上游基线与工作区准备

### 3.1 仓库规则

目标远端：

```text
upstream = https://github.com/Wei-Shaw/sub2api.git  # 只拉取
origin   = https://github.com/holeenlu/sub2api.git  # 拉取和推送
```

当前 `main` 跟踪 `upstream/main`，默认推送已配置为 `origin`。正式开发前应：

1. 获取 `upstream/main` 最新提交；
2. 记录当前提交、分支和工作区状态；
3. 确认部署相关本地改动不被清理；
4. 从稳定基线创建 `custom/api-key-principal` 或按子功能创建短分支；
5. 不在 `main` 直接开发业务功能。

### 3.2 本地已有改动处理

当前工作区已有部署相关改动：

- `deploy/.env.example`；
- `deploy/README.md`；
- `deploy/docker-compose.local.yml`；
- `deploy/local-deploy.sh`；
- 需求文档和 `.gitignore`。

这些改动属于既有工作，不能通过 reset、checkout 或清理命令丢弃。实施时应先建立变更清单，并将部署改动与 API Key 业务改动分成不同提交。

### 3.3 Phase 0 完成标准

- 能够执行 `git fetch upstream`；
- `git push` 默认不会指向官方仓库；
- 当前本地改动有备份或明确提交归属；
- 能在临时同步分支合并官方最新基线；
- 未修改业务代码。

## 4. Phase 1：架构基线和现有模型契约

### 4.1 Architecture Gate

开发前完成并维护：

```text
docs/architecture/overview.md
docs/architecture/invariants.md
docs/architecture/request-lifecycle.md
docs/architecture/api-key-principal.md
docs/adr/0001-api-key-principal-boundary.md
docs/architecture-health.md
```

决策是复用现有 APIKey 聚合根，不新增 `custom_api_key_policies`。已有状态继续由现有字段拥有：

```text
quota=0       = 无限配额
quota>0       = 有限配额
quota_used    = Key 已用额度
usage_log     = api_key_id 用量事实
Redis         = 活跃 Key 并发统计
```

### 4.2 领域语义收口

优先在现有 `APIKey` / `APIKeyService` 中收口：

- 派生 `unlimited_quota`；
- 统一剩余额度和状态转换；
- 复用现有所有权、Group、IP 和周期限额；
- 生成掩码 Key、前缀和指纹；
- 为未来权限交集保留清晰的共享接入点。

推荐 Context 内容：

```text
user_id
api_key_id
api_key_name_snapshot
api_key_prefix
unlimited_quota
```

P0 不引入通用 policy service。只有 P1 出现多个稳定权限维度时，才评估是否需要独立的 `APIKeyPrincipalService`。

### 4.3 Feature Flag

新增配置建议：

```text
api_key_principal.enabled
api_key_principal.unlimited_quota_ui_enabled
api_key_principal.usage_snapshot_enabled
api_key_principal.key_concurrency_enabled
api_key_principal.admin_management_enabled
```

默认策略：

- 展示和读取功能可以逐步开启；
- 新增安全限制默认 fail-closed；
- `key_concurrency_enabled` 在 P1 灰度前保持关闭；
- 旧 Key 始终继续按官方 `quota=0` 规则运行。

### 4.4 Phase 1 完成标准

- 架构文档、ADR 和健康清单建立；
- 旧 Key 无迁移即可正常认证；
- 无限配额派生语义只存在一个领域实现；
- 无需修改 Ent 生成文件；
- Feature Flag 关闭后官方行为保持一致。

## 5. Phase 2：P0 配额、权限和生命周期

### 5.1 显式无限/有限配额

实现顺序：

1. 回填旧 Key：`quota <= 0 → unlimited`，`quota > 0 → limited`；
2. 创建/编辑接口增加 `quota_mode`；
3. 无限模式同步保持官方 `quota=0`；
4. 有限模式沿用官方金额账本；
5. 增加模式切换和重置审计；
6. 统一处理 `quota_exhausted` 状态恢复。

必须先解决计费生命周期，再开放管理员批量修改。有限配额的最终目标是：

```text
预占 → 上游执行 → 权威结算 → 成功提交或精确退款
```

无限 Key 只绕过 Key 总配额门禁，仍必须执行用户余额/订阅检查和实际用量结算。

### 5.2 权限交集

P0 实现：

- 用户所有权验证；
- Key 状态、过期和 IP 规则；
- Key 所属 Group 的安全校验；
- Key 名称、状态、额度和审计字段；
- 管理员范围隔离。

P1 再实现模型列表、端点列表和并发限制，避免在 P0 同时修改过多官方热路径。

### 5.3 生命周期接口

用户和管理员接口统一使用服务层，不让用户接口和管理员接口各写一套业务规则：

```text
创建、查询、更新、启用、停用、删除
切换配额模式、修改额度、重置用量
```

轮换、批量操作和高权限查看放到 P1，先建立掩码和审计基础。

### 5.4 Phase 2 完成标准

- 无限 Key 会累计 Key 用量但不会耗尽 Key 总配额；
- 用户余额不足仍返回资金不足；
- 有限 Key 并发修改不会超卖；
- 用户不能访问其他用户的 Key；
- 改名、禁用、删除、额度变更均有脱敏审计；
- 所有普通 DTO 都不会返回完整 Key。

## 6. Phase 3：P0 统计、用量和日志

### 6.1 用量快照

先比较两种历史展示方案，再在统一用量接入点实现：

1. `usage_logs` 直接保存名称/前缀快照；
2. 独立的 Key 名称/凭证时间历史，并在查询时按事件时间解析。

最终选择必须通过写入延迟、失败补偿、事务边界、删除保留和历史查询压测后确定。禁止在每个协议 Handler 中分别追加快照代码。

### 6.2 Key 统计接口

P0 先复用现有 usage 聚合查询，增加统一 Key 过滤和所有权约束：

```text
GET /api/v1/user/api-keys/:id/stats
GET /api/v1/user/api-keys/:id/usage
GET /api/v1/user/api-keys/:id/errors
GET /api/v1/admin/api-keys/:id/stats
GET /api/v1/admin/api-keys/:id/usage
GET /api/v1/admin/api-keys/:id/errors
```

查询必须使用 `api_key_id`，名称只负责解析筛选条件和展示。Key 详情至少支持请求数、成功率、Token、实际成本、模型分布、时间趋势和最后使用时间。

### 6.3 错误和审计

优先复用已有运维/安全审计事件。如果现有事件无法满足 Key 独立查询，再新增具有明确生命周期的 Key 错误事件对象，字段至少包括：

```text
api_key_id, user_id, request_id, phase,
model, channel_id, account_id, status_code,
error_code, sanitized_message, created_at
```

审计事件必须覆盖创建、编辑、启停、删除、模式切换、重置和轮换，不保存 Key 明文。

### 6.4 Phase 3 完成标准

- Key 改名后历史日志仍显示旧快照；
- 删除 Key 后历史日志仍可查询；
- 用户和管理员的 Key 过滤均有权限隔离；
- Key 费用统计与用户费用统计可对账；
- 错误、拒绝和成功请求均可按 Key 追踪；
- 导出文件没有完整 Key 或上游凭证。

## 7. Phase 4：P0 计费、并发和全入口验证

### 7.1 统一计费合同

以现有统一 usage billing 路径为主接入点，逐步证明以下入口都能得到正确 `api_key_id`：

- Chat Completions；
- Responses HTTP；
- Responses WebSocket；
- Embeddings；
- Images；
- Gemini 兼容入口；
- 异步图片/视频任务；
- 批处理和其他计费入口。

不在每个入口实现自己的配额和日志算法，只补缺失的统一事件调用。

### 7.2 并发策略

P0 保持现有用户级和 Account 级并发行为，只保证 API Key 并发可以正确统计。P1 才开启 Key 独立并发门禁：

```text
用户槽位 → Key 槽位 → Account 槽位
```

Key 槽位必须使用 Redis 原子获取/释放，不能采用先查询当前数量再写入的方式。

### 7.3 关键测试

必须包含：

- 无限 Key 累计用量但不耗尽；
- 无限 Key 仍受余额限制；
- 有限额度并发预占不超卖；
- 重试不重复扣费；
- 失败不重复退款；
- 正向结算不丢失；
- 所有入口都记录同一 Key；
- Redis 缓存失效后状态仍正确；
- Key 改名/删除不改变历史日志；
- 用户跨 Key 聚合与 Key 明细对账。

### 7.4 变异式验证

针对关键合同故意注入以下错误，测试必须能够失败：

- 删除无限 Key 的用量累计；
- 让无限 Key 跳过用户余额；
- 将原子预占改成先读后写；
- 删除某个入口的 `APIKeyID`；
- 去重只按 `request_id`；
- 重复调用退款；
- 日志改用实时名称而不是快照；
- 删除用户所有权条件。

## 8. Phase 5：P1 独立主体增强

按以下顺序实施：

### P1.1 API Key 独立并发

新增 `max_concurrency`，有效值为：

```text
min(user.concurrency, api_key.max_concurrency)
```

增加 Key 等待队列、实时使用量、超时、槽位 TTL 和 Redis 故障测试。普通 API Key 与 OpenAI WS ingress 连接数分开计量。

### P1.2 模型和端点权限

新增允许模型和允许端点列表，采用用户/Group 与 Key 交集。Key 只能收窄权限，不能扩大用户权限。

### P1.3 RPM/TPM 和周期限制

在已有用户、Account、Key 总配额的基础上增加 Key 独立 RPM/TPM，但必须明确它们与并发、5 小时/1 天/7 天费用窗口的差异。

### P1.4 凭证轮换

新增 `custom_api_key_credentials`，支持版本、撤销时间和可选宽限期。逻辑 `api_key_id` 不变，统计连续，原始 Key 只在创建/轮换时显示一次。

### P1.5 管理和导出增强

- 管理员全局 Key 管理页；
- Key 概览、权限、统计、用量、错误、审计六个页签；
- 批量启停/删除的全量授权校验和事务原子性；
- 异步 CSV 导出；
- 按用户、Key、模型、渠道、Account 和时间查询。

## 9. Phase 6：灰度发布和长期同步

### 9.1 灰度顺序

```text
只读统计 → 日志快照 → 用户 Key 详情
  → 管理员查询 → 显式配额
    → P1 Key 并发 → 模型/端点权限 → 轮换
```

每一步都支持按用户、Key 或配置开关灰度，不直接全局启用。

### 9.2 监控指标

上线后必须监控：

- Key 主体语义解析失败率；
- Key 详情和历史快照查询延迟；
- 快照写入失败和积压；
- Key 统计与用户统计对账差异；
- 配额预占失败；
- 重复结算和退款异常；
- Redis Key 槽位泄漏；
- 上游同步分支冲突和官方测试回归。

### 9.3 官方同步节奏

每次官方发布后：

1. 在 `sync/upstream-YYYYMMDD` 获取并合并 `upstream/main`；
2. 先运行迁移和编译检查；
3. 再运行 API Key、计费、并发和安全合同测试；
4. 检查官方是否新增同类 Key/Token 能力；
5. 如有重复能力，建立兼容 ADR 和本地扩展退役计划；
6. 通过 PR 合并到 `origin/main`。

## 10. 建议任务清单

### M0：基线

- [ ] 记录官方基线提交和本地工作区变更；
- [ ] 创建 API Key 功能分支；
- [ ] 添加同步分支和 CI 合并检查模板；
- [ ] 编写扩展对象命名和迁移规范。

### M1：架构基线与现有模型

- [x] 建立架构文档、ADR 和健康清单；
- [x] 确认复用现有 APIKey 聚合根；
- [ ] 在现有 APIKey 领域模型中收口无限配额派生语义；
- [ ] 新增 Feature Flag 和配置校验；
- [ ] 新增兼容 DTO 字段和 API 合同；
- [ ] 完成旧 Key 零迁移兼容测试。

### M2：P0 配额与权限

- [ ] 显式无限/有限配额创建和编辑；
- [ ] 配额模式切换、重置和审计；
- [ ] 用户所有权和管理员权限矩阵；
- [ ] 掩码 Key、指纹和敏感字段检查；
- [ ] 认证缓存失效和数据库权威回源。

### M3：P0 统计与日志

- [ ] Key 历史快照方案 ADR、基准测试和统一接入点；
- [ ] Key stats/usage/errors/audit API；
- [ ] 用户 `/keys` 详情页；
- [ ] 管理员 Key 管理页；
- [ ] 用量表 Key 筛选、渠道维度和导出。

### M4：P0 计费和验证

- [ ] 全入口 API Key 归属合同测试；
- [ ] 有限/无限配额并发测试；
- [ ] 退款、防双扣和权威结算测试；
- [ ] Key 与用户费用对账；
- [ ] 变异式测试和 Redis 故障测试。

### M5：P1

- [ ] Key 独立并发门禁；
- [ ] 模型/端点权限；
- [ ] RPM/TPM；
- [ ] 凭证版本轮换；
- [ ] 批量管理和异步导出；
- [ ] 官方同类能力检测和退役 ADR。

## 11. 完成定义

本项目不以“接口能返回”和“普通测试通过”作为完整交付标准。每个阶段必须同时满足：

1. 功能实现完成；
2. 数据迁移可回滚或可补偿；
3. 用户/管理员权限隔离通过；
4. 计费和统计可对账；
5. 关键不变量有集成测试和变异式证明；
6. 官方上游能合并，且新增触点在清单中有记录；
7. Feature Flag 关闭后能够回到官方基线；
8. 文档、监控、发布和退役路径同步完成。

## 12. 第一轮开发建议

第一轮只做 M0 和 M1，不创建策略扩展表，不立即修改所有网关入口，也不立即开启 Key 独立并发。先完成：

```text
上游基线确认
  → 架构事实和不变量
    → API Key 聚合根决策
      → 无限配额派生语义
        → 旧 Key 兼容和 Feature Flag
```

第一轮验收通过后，再进入 P0 配额、统计和日志闭环。只有现有模型确实无法表达新状态时，才决定增加核心字段或独立子对象。
