# 三级账号角色与功能授权方案

日期：2026-10-06。版本：v0.3（审计重构版）。状态：2026-10-07 已按复审后的 v0.3 完成隔离工作树实现、功能审计及测试；未提交、推送、发布或执行生产迁移。

研究基线：本地 main `704383a02`，工作树 tokensavy `2f84132c6`；本文引用的角色、认证、二次验证、审计、用户管理、路由与导航实现在两者间一致。不代表远端最新代码。

适用分支：main、TapModels、tokensavy 共享能力。先在 main 实施，再普通 merge 到两个品牌分支。角色策略存放在各站点自己的数据库，代码同步不同步人员或策略。

## 0. v0.3 审计结论（相对 v0.2 的改动）

v0.2 的目标模型（固定三角色 + 一份管理员角色策略 + 超管对象保护）合理，保留。审计发现以下缺陷，已在本版修正：

| # | v0.2 问题 | 证据 | v0.3 处理 |
| --- | --- | --- | --- |
| 1 | 多处写“强制二次验证”，但现有 step-up 受全站开关控制，关闭时直接放行；且要求操作人已启用 TOTP | `middleware/step_up.go` `enforceStepUp` 首行判断 `IsStepUpEnabled`；未启用 TOTP 返回 `STEP_UP_TOTP_NOT_ENABLED` | 明确两档：策略修改、提升超管用 `EnforceStepUpAlways`；其余敏感动作复用现有 step-up 中间件并随开关生效。见 §6.3 |
| 2 | 给 JWT 与 refresh token 加策略版本，每次改策略都强制全体管理员重新登录 | 授权若每次请求都读库，令牌里的版本是冗余的；且带来备份恢复后旧令牌复活等额外问题 | 删除。令牌只证明“是谁”，权限逐请求从数据库读取；改策略无需下线。见 §6.1 |
| 3 | 要求所有关键管理写事务对策略行加共享锁，并与用户/账务锁统一排序 | 牵涉余额、退款、账号等几十个事务，死锁和回归面大，收益只是毫秒级窗口 | 删除。生效语义改为“提交后开始的请求一律按新策略”；只对当前目标身份做事务保护；不读取或锁定角色策略。最后有效超管的维护仅在人员身份事务中串行化。见 §6.2 |
| 4 | 管理员成员管理与普通用户管理共用 `users.*`，超管无法“让管理员服务客户但不能再造管理员” | 创建管理员等于把整份策略复制给新人，是权限扩散动作 | 拆出 `staff.manage`，默认开启（满足“管理员可创建管理员”的要求），超管可统一关闭。见 §3 |
| 5 | “全仓清点旧 IsAdmin”未落到具体位置；有 handler 用角色决定数据范围，机械替换会让超管被当成普通用户 | `channel_monitor_v2_handler.go:33` 同一 handler 挂在用户和管理两组路由上，以 `role == RoleAdmin` 判断范围 | 删除旧谓词使编译失败强制逐处评审，并列出 16 个后端文件的处置。见 §7 |
| 6 | 未估算路由覆盖工作量 | `routes/admin.go` 433 条、`routes/payment.go` 37 条路由，`stepUpAuth` 目前只挂 29 处 | 补充路由权限声明机制和工作量。见 §8、§12 |
| 7 | 策略读取遇未知权限键即判失败，一次改名发版会锁死全部管理员 | 权限目录随代码演进，存量策略随数据库 | 读取时忽略未知键并告警，写入时拒绝；改名靠目录别名。见 §5 |
| 8 | 初始策略内容未定义 | — | 明确初始集合。见 §5 |
| 9 | 文档 525 行，§1/§7/§19 多处重复，“建议/应当/不能”混杂 | — | 按“要求 → 模型 → 执行 → 数据 → 迁移 → 验收”重排，合并重复段落 |

## 1. 已确认需求

- 只有三种角色：超级管理员 `super_admin`、管理员 `admin`、用户 `user`。现有全部 admin 迁为 super_admin。
- 全部管理员共用一份“管理员角色策略”，没有个人权限例外。
- 管理员可以创建管理员和普通用户；新管理员自动使用当前角色策略。
- 管理员之间平级、业务数据互通，可以互相查看、修改、停用、删除；创建人只用于追溯，不形成上下级或数据归属。
- 只有超管能修改管理员角色策略，保存后对现有和未来全部管理员生效。
- 管理员看不到超管操作日志，也不能把自己或他人提升为超管、不能操作超管账号。

由此带来的使用前提：所有管理员权限一致，无法让 A 只做客服、B 只做财务；本方案适用于高度互信的管理团队。若以后要分工，需要另立需求，不在本方案里预留个人覆盖。

## 2. 角色与对象规则

| 维度 | super_admin | admin | user |
| --- | --- | --- | --- |
| 后台权限来源 | 全部（含超管专属） | 管理员角色策略 | 无 |
| 创建 / 维护 admin | 可以 | 需 `staff.manage` | 不可以 |
| 创建 / 维护 user | 可以 | 需 `users.*` | 仅站点允许的自助注册 |
| 改变账号角色 | 三种角色之间均可 | 仅 admin ↔ user，需 `staff.manage` | 不可以 |
| 修改管理员角色策略 | 可以 | 不可以 | 不可以 |
| 操作 super_admin 账号 | 仅其他有效超管 | 一律拒绝 | 不可以 |
| 业务数据 | 全站 | 已授权模块内全站共享 | 仅本人 |
| 操作日志 | 全部 | 有 `audit.read` 时看全部 staff 级日志 | 不可以 |
| `/usage`、`/keys`、个人订单 | 仅本人 | 仅本人 | 仅本人 |

固定保护规则（不可由策略开启）：

1. 不能在后台对自己执行删除、停用、降级；改自己的密码和 MFA 走个人安全入口。
2. 至少保留一个 active 且未删除的 super_admin；admin 数量可以为零。原“最后一个 admin”保护改为“最后一个有效 super_admin”。
3. 普通查询对任何角色都不返回密码、TOTP 种子、refresh token、上游长期 token、代理密码明文；“可编辑凭据”只表示可替换。为与 §3 的超管专属导出及现有功能一致，保留超管显式发起、受现有二次验证保护的上游账号/代理迁移导出；授权向导刚生成的凭据仍可在该流程内返回。受限管理员不能读取已存凭据或调用迁移导出。
4. 管理员不能通过订单、订阅、身份绑定、API Key、返利或批量接口间接改变超管账号或其权益。
5. 批量操作先校验完整目标集合；只要夹带 super_admin 或其他不允许的目标就整体拒绝，不做部分成功。
6. 人员身份事务内重读目标当前角色：A 打开编辑页后 B 被提升为超管，A 的提交必须失败。
7. 禁止管理员给自己调整余额、给本人订单发起后台退款。但这挡不住两名管理员互相操作；要彻底避免，只能由超管统一收回相应财务权限。

## 3. 权限目录

权限键在后端以编译期常量集中定义（目录），包含：键名、分组、显示文案键、依赖、是否可授予 admin、敏感级别、别名。前端只读取服务端返回的目录和有效权限，不自行定义授权规则。

界面上每个模块显示“关闭 / 只读 / 可操作”，敏感动作单列勾选。`*.manage` 依赖同模块的 `*.read`，保存时由后端补全或拒绝。

| 模块 | 可授予 admin 的权限键 | 边界 |
| --- | --- | --- |
| 管理员成员 | `staff.manage`（依赖 `users.read`） | 对 role=admin 账号的创建、编辑、停用、删除、密码重置、身份重绑，以及 admin ↔ user 角色变更；需二次验证；不适用于 super_admin |
| 用户管理 | `users.read`、`users.create`、`users.update`、`users.delete` | 仅 role=user 的资料、状态、并发、RPM、可用分组；不含余额、倍率、角色 |
| 用户安全处理 | `users.security`（依赖 `users.read`、`users.update`） | role=user 的密码重置、身份绑定处理；需二次验证；撤销目标旧会话并通知原联系方式 |
| 仪表盘 | `dashboard.read` | 请求、Token、收费等业务汇总；成本数据另需 `billing.cost.read` |
| API Key | `api_keys.read`、`api_keys.manage` | 仅现有后台能力：脱敏标识、分组调整等；不读取完整密钥，不新增代生成入口 |
| 分组 | `groups.read`、`groups.manage` | 分组、模型许可、路由、Composite；改收费倍率另需 `billing.rates.update` |
| 渠道 | `channels.read`、`channels.manage` | 渠道定义、模型关联；改价格另需 `billing.rates.update` |
| 渠道监控 | `channel_monitor.read`、`channel_monitor.manage` | 监控、模板、任务、结果；修改通知出口不得带出秘密 |
| 上游账号 | `accounts.read`、`accounts.manage` | 状态、优先级、调度开关、分组绑定；凭据只返回脱敏状态 |
| 上游账号授权 | `accounts.authorize` | OAuth 创建、token/Setup Token 导入、重新授权、凭据替换；需二次验证 |
| 账号测试与诊断 | `accounts.diagnostics` | 账号测试、定时测试、Codex 检测等；会产生真实调用成本 |
| IP 管理 | `proxies.read`、`proxies.manage` | `/admin/proxies` 状态、测试、配置；代理凭据读取与导出为超管专属 |
| 用量与排行 | `usage.read`、`usage.export` | 全站明细、错误请求、用户和 API Key 排行；导出与页面同范围、同字段投影 |
| 运营成本 | `billing.cost.read` | 上游成本、成本倍率、利润及可推导字段；无此权限时后端统一剥离（§6.4） |
| 余额与价格 | `billing.balance.adjust`、`billing.rates.update` | 对 admin/user 的余额调整、收费修改；需二次验证；不能改变超管权益 |
| 订阅 | `subscriptions.read`、`subscriptions.manage` | 分配、续期、撤销、恢复、配额重置；有价权益不随 `users.update` 开放 |
| 订单 | `orders.read`、`orders.manage`、`orders.refund` | 查询、取消、重试发货与退款分开；复用原幂等、租约和退款状态机；人工强制裁决为超管专属 |
| 套餐 | `plans.read`、`plans.manage` | 改价格另需 `billing.rates.update` |
| 兑换码 | `redeem_codes.read`、`redeem_codes.manage`、`redeem_codes.export` | 生成有价权益属于财务授权 |
| 优惠码 | `promo_codes.read`、`promo_codes.manage` | 直接影响应收金额 |
| 邀请返利 | `affiliates.read`、`affiliates.manage`、`affiliates.adjust` | 不能改变超管权益 |
| 公告 | `announcements.read`、`announcements.manage` | 沿用原渲染安全规则 |
| 运维监控 | `ops.read`、`ops.manage` | 脱敏运行指标与告警处置；原始系统日志、日志清理为超管专属 |
| 内容风控 | `risk.read`、`risk.manage` | 摘要、解封、规则维护；服务密钥与外部检测出口为超管专属 |
| 提示词审计 | `prompt_audit.read`、`prompt_audit.content.read` | 摘要与正文分开；采集出口配置、删除为超管专属 |
| 操作日志 | `audit.read` | 仅 staff 级日志（§9） |
| 站点信息 | `settings.general.read`、`settings.general.manage` | 站名、Logo、联系方式等白名单字段 |
| 协议与模板 | `settings.content.read`、`settings.content.manage` | 协议文案、邮件模板；不能改认证收件地址或安全策略 |

账号管理指上游模型服务账号，用户管理指本系统登录的人，二者分开授权。具有某模块 `manage` 不自动获得以后新增的敏感能力；新增能力必须登记新权限键。

## 4. 超管专属范围（不出现在可勾选项中）

- 管理员角色策略本身；创建、提升、修改、降级、停用、删除 super_admin。
- 注册、登录、OIDC/OAuth 提供方、SSO、MFA、Passkey、会话策略、step-up 开关。
- 默认赠额、默认订阅、计费默认值（避免与创建用户组合成批量赠额通道）。
- 全站功能开关、后台模式、限流与豁免、可信客户端 IP、出口安全边界、网关全局调度参数。
- 支付服务商密钥、回调验签、收款账户；SMTP 凭据、全局通知出口、S3、对象存储。
- 全局 Admin API Key 的查看、轮换、删除。
- 插件、备份、数据导入与恢复、系统升级、回滚、重启。
- 原始系统日志、操作日志清空与保留期缩短、审计删除。
- 带凭据上游账号的 `base_url`、`proxy_id`、TLS 校验变更，以及被这类账号实际引用的代理的目标地址变更。这些改动能把已保存的凭据发往新出口，等于间接读取凭据。按实际引用关系判定，不另建“已批准出口”状态或审批流。

## 5. 管理员角色策略

### 5.1 存储

复用 `settings` 表新增受保护键 `admin_role_policy`，值为：

~~~json
{
  "version": 3,
  "permissions": ["staff.manage", "users.read", "users.create", "users.update", "users.delete"],
  "updated_by": 1,
  "updated_at": "2026-10-06T00:00:00Z"
}
~~~

- 只保存一份；创建管理员时不复制、不快照，用户表不加个人权限字段。
- 迁移时插入初始记录，保证后续 `SELECT … FOR UPDATE` 一定有行可锁。
- 该键必须从通用设置的批量保存、删除、公开输出、导入和插件配置路径中排除。现有 `setting_repo` 的 `Set`/`SetMultiple` 是无条件 upsert，不能直接复用。

### 5.2 初始内容

迁移后的初始策略只包含需求明确要求的人员管理能力：`staff.manage`、`users.read`、`users.create`、`users.update`、`users.delete`。其余业务权限由超管首次进入角色权限页统一勾选；以后新增的权限键默认关闭。

### 5.3 更新

`PUT /api/v1/admin/roles/admin/permissions` 请求体为 `expected_version`、`permissions`、`reason`：

1. 仅 super_admin，且无条件二次验证（`EnforceStepUpAlways`）；Admin API Key 不可调用。
2. 校验权限键：未知键、超管专属键一律拒绝；按依赖补全或拒绝。
3. 同一事务内 `SELECT … FOR UPDATE` 锁定策略行，校验 `expected_version`，写入新集合并 `version+1`，同事务写入 super_admin 级审计（增删的权限键、新旧版本、原因）。版本不符返回 409。
4. 提交即生效，不批量写用户、不强制下线（§6.1）。

### 5.4 读取与容错

- 每个管理员请求读取已提交策略。管理请求量低，一期直接按主键读一行，不加缓存；以后如要缓存，必须以数据库版本号为键，不能复用现有设置缓存。
- 读取时遇到未知权限键：忽略该键并记录告警，其余照常生效。代码改名在目录里登记别名映射，避免发版后权限静默丢失。
- 策略行缺失或无法解析：拒绝全部 admin 的后台操作（fail-closed），绝不回退到旧 admin 全权；super_admin 不受影响，可进入角色权限页修复。

## 6. 授权执行

### 6.1 认证与授权分离

- 令牌只证明身份。现有 JWT/refresh token 已有 `TokenVersion`（由邮箱、密码指纹、`session_generation` 推导）和 `SessionID`，个人改密、改角色、停用、删除继续递增 `users.session_generation` 撤销会话。
- 权限不进令牌，不新增策略版本字段。管理中间件已经每次从数据库读取用户（`admin_auth.go` `validateJWTForAdmin`），在此之后增加一步：读当前策略，把有效权限放入请求上下文。
- 授权一律使用数据库中的当前角色，不使用 JWT 里的 `Role` claim。注意 `session_binding.go:92` 现在用 `claims.Role` 写审计 actor_role，需改为数据库角色。
- 前端在收到 `403 PERMISSION_DENIED` 时重新拉取 `/auth/me` 并重算菜单；服务端可在管理响应头带 `X-Admin-Policy-Version`，前端发现变化即刷新。改策略不需要任何人重新登录。

### 6.2 生效时点

- 语义：策略事务提交后，开始的请求一律按新策略裁决。提交前已通过鉴权、正在执行的单个请求允许完成；已经返回的数据不能撤回，已提交的账务不因撤权逆转。
- 多步流程（OAuth 授权向导、导入、待执行人工任务、导出产物下载）在每一步的副作用前重新鉴权，不能沿用第一步的结果。
- WebSocket/SSE 长连接在每次推送受保护消息前、以及至少每 30 秒，重查用户状态与权限；失配即关闭。
- 人员身份事务（创建、改角色、停用、删除、重置凭据）内 `SELECT … FOR UPDATE` 目标用户行，重读目标角色和状态后再写。最后一个 super_admin 的保护在同一事务内计数。

### 6.3 二次验证

现有 step-up 依赖两个前提：全站开关 `step_up_enabled` 开启，且操作人已启用 TOTP。本方案不改这套机制，只规定挂载位置：

| 档位 | 动作 | 实现 |
| --- | --- | --- |
| 无条件 | 修改管理员角色策略；创建或提升 super_admin | `EnforceStepUpAlways`，开关关闭也要求 |
| 随开关 | `staff.manage` 的写操作、`users.security`、`accounts.authorize`、`billing.balance.adjust`、`billing.rates.update`、`orders.refund`、`redeem_codes.manage`（生成有价码）、`usage.export` | 现有 `stepUpAuth` 中间件或 `EnforceStepUp` |

角色权限页在 step-up 开关关闭时显示警示：上述“随开关”动作此时不要求二次验证。未启用 TOTP 的管理员执行这些动作会被拒绝并提示先启用 TOTP，这是现有行为。

### 6.4 对象与字段

- 用户创建、编辑、批量更新按字段绑定权限：`role` 需 `staff.manage`（或超管），`balance`/`group_rates` 需对应财务权限。请求含无权字段时整体拒绝并说明字段名，不静默忽略。
- 系统设置按字段归属到 `settings.general` / `settings.content` / 超管专属；保存请求含越权字段整体拒绝。
- 无 `billing.cost.read` 时，成本、成本倍率、利润及可直接推导的字段在 DTO 映射层统一剥离，覆盖汇总、排序、排行、弹窗和导出；按成本列排序的请求直接拒绝。这是本方案字段投影中工作量最大的一项，实施前需先清点所有含成本字段的 DTO。
- 凭据脱敏沿用 `handler/dto/credentials_redact.go`，不另建敏感键清单。
- 订单详情附带的 `payment_audit_logs` 只有 `operator` 文本、没有操作时角色，无法可靠区分超管操作。对 admin 返回订单时不附带这份原始审计，只返回状态、金额、退款进度等业务字段。余额流水、订阅变更、返利记录同理：金额、状态、时间可见，操作人 IP、请求体、安全备注不随业务查询返回。

### 6.5 Admin API Key

- 定位为“超级管理员机器密钥”，仅 super_admin 可查看、轮换、删除。
- `validateAdminAPIKey` 现在绑定 `GetFirstAdmin`（最小 ID 的 active admin）。改为绑定最小 ID 的 active super_admin；找不到则 401。
- 用它产生的日志一律为 super_admin 可见级。step-up 中间件已拒绝该密钥访问需二次验证的接口，保持不变。
- 不提供受限管理员的机器凭据；以后有需要再单独设计带归属、范围和撤销的服务凭据。

## 7. 角色判断的替换

删除 `User.IsAdmin()` 和 `middleware.AdminOnly()`，用编译失败迫使每处调用点重新选择语义：

- `IsSuperAdmin()`：全站权限。
- `IsStaff()`：admin 或 super_admin，仅表示“可进入管理面”。
- `can(permission)` / `canTarget(permission, target)`：动作与对象授权。

现有后端调用点的处置（`grep IsAdmin()|RoleAdmin` 共 16 个文件）：

| 位置 | 现在的作用 | 处置 |
| --- | --- | --- |
| `middleware/admin_auth.go` | 进入管理面 | `IsStaff()`，再加载策略 |
| `middleware/admin_only.go` | 同上 | 删除，调用方改用管理鉴权 + 权限声明 |
| `handler/channel_monitor_v2_handler.go:33` | 共享 handler 按角色决定全站 / 本人范围 | 改为由路由组注入范围标记，不再看角色；否则超管走 `/admin` 也会被当成本人范围 |
| `middleware/panel_rate_limit.go:82` | `ExemptAdmin` 时管理员免限流 | 建议仅 super_admin 豁免；admin 走正常限流，防止批量创建账号绕过频率限制 |
| `handler/auth_handler.go:154`、`handler/passkey_handler.go:235` | 后台模式下只允许管理员登录 | `IsStaff()` |
| `service/totp_service.go:152` | 管理员不可用邮箱验证码替代 TOTP | `IsStaff()` |
| `service/totp_service.go:338` | 管理员关闭 TOTP 需 step-up | `IsStaff()` |
| `service/content_moderation.go:1979` | 自动封禁跳过管理员 | 待决（§13）；建议 `IsStaff()`，避免误封运营账号 |
| `service/admin_user.go` | 最后一个 admin 保护、角色变更 | 改为最后一个有效 super_admin；按 §2 对象规则重写 |
| `handler/admin/user_handler.go` | 角色变更时的 step-up | 按 §6.3 档位 |
| `repository/user_repo.go:1443` | `GetFirstAdmin` | 改为 super_admin（§6.5） |
| `setup/setup.go:425/445` | 安装时检查并创建首个 admin | 计数和创建 super_admin |
| `repository/simple_mode_admin_concurrency.go:35` | 一次性升级 admin 并发 | 一次性迁移，必须排在角色迁移之前执行，或同时匹配 super_admin |
| `domain/constants.go`、`service/domain_constants.go`、`service/user.go` | 常量与谓词 | 增加 `RoleSuperAdmin`，删除 `IsAdmin()` |

前端 23 个文件共 117 处 `isAdmin` / `requiresAdmin` / `role === 'admin'`，同样拆成 `canAccessAdmin`、`isSuperAdmin`、`can(key)`。路由守卫和菜单以 `/auth/me` 返回的有效权限为准；`stores/adminSettings.ts` 不再为渲染导航拉取完整系统和支付配置。

## 8. 路由权限声明

管理面共约 470 条路由（`routes/admin.go` 433、`routes/payment.go` 37），payment 有独立的 `adminGroup` 与 `AdminComplianceGuard`，两处都要覆盖。

- 保留原生 Gin 路由注册行，以 `internal/authz/routes.go` 为唯一 method+path 权限声明表，登记所需权限、超管专属或独立认证范围。这样避免包装注册函数造成大面积上游同步冲突；未声明仍默认拒绝。
- 新增路由覆盖测试：遍历 `gin.Engine.Routes()`，凡 `/api/v1/admin/**` 及 payment 管理组下的路由，没有声明即失败。未声明的路由在运行时默认拒绝。“已经通过 adminAuth”不算声明。
- 按真实业务动作声明，不按 HTTP 方法推断：有的 POST 只是批量读取统计，有的 GET 会触发同步或下载。
- 还要覆盖：WebSocket/SSE 握手（现在通过 `Sec-WebSocket-Protocol` 传 JWT）、OAuth 回调与 token 兑换、批量导入、后台任务结果下载、插件 UI 能力 URL。
- 支付 webhook（签名校验）和用户网关 API Key 不属于三角色体系，保持原样。

## 9. 操作日志隔离

### 9.1 可见级别

`audit_logs` 新增 `visibility`：`staff` / `super_admin`，默认 `super_admin`。满足任一条件即写 `super_admin`：

- 操作人是 super_admin，或认证方式是 Admin API Key；
- 修改管理员角色策略；
- 目标是 super_admin 账号或涉及 super_admin 角色变化；
- 来源或角色无法可靠判断。

只有来源明确的 admin/user 事件写 `staff`。分类在服务端可信上下文中完成，不接受请求体传入的角色或可见级别。后台任务保存发起人身份与分类，之后以 system 身份完成的结果沿用发起时的分类。

存量日志迁移后全部为 `super_admin`：旧 `actor_role=admin` 的语义是全权管理员，不能当作新 admin 的历史；原内容不改写，界面标注为“升级前记录”。超管降级后，其任超管期间的日志仍为 super_admin 级；日志按事件发生时的身份分类。

### 9.2 查询

- `List`、`GetByID`、`Count` 必须接收服务端生成的可见范围；仓储层不再保留无范围的详情方法（现有 `GetByID(ctx, id)` 改签名）。
- 范围条件在 SQL 层先于搜索、计数、排序、分页生效；隐藏记录不计入 total。
- 请求不可见日志详情统一返回 404，不透露是超管日志。
- 有 `audit.read` 不等于能看成本、提示词正文或凭据；日志详情同样做字段投影。
- 原始系统日志没有可靠的身份分类，整体为超管专属。

### 9.3 审计内容与事务

- 策略变更审计、人员身份变更审计与业务写入同事务提交；现有异步日志队列不能作为“权限已改”的唯一证据。
- `extra` 若需放宽现有标量白名单，只接受经目录校验、有数量上限的权限键列表，不放开任意嵌套请求体。
- 管理员 A 重置 B 的密码后以 B 登录，后续日志显示 B。这无法从日志本身区分；依靠“A 的重置事件 + B 旧会话撤销 + 通知 B 原联系方式”三者共同追溯。

## 10. 人员生命周期

**创建管理员**：在 `/admin/users` 选择角色 admin；不出现个人权限勾选，只读显示“使用管理员角色统一权限，后续调整对全体管理员生效”。后端校验操作者 `staff.manage` 与目标角色，请求中的权限、余额、倍率等无权字段整体拒绝。记录创建人、目标 ID、当时策略版本（仅追溯）。新账号不继承创建者的密码、MFA、API Key、余额、订阅或会话。

**同级维护**：资料与状态走 `staff.manage`；改邮箱、重置密码、重绑身份、关闭 MFA 需二次验证，并通知目标原有已验证联系方式和全部超管（不能只通知新邮箱）。角色或凭据变化、停用、删除时递增目标 `session_generation`，同时清除待完成的认证流程；重新启用不恢复旧会话。

**删除**：沿用软删除（`SoftDeleteMixin`）与账务保护，撤销登录和本人 API Key；订单、用量、余额与退款凭证、操作记录保留；共享的上游账号、渠道、公告不级联删除；进行中的计费请求和退款按原状态机结算。删除后同名重建得到新 ID，不继承旧凭据或历史。

**停用 A 不连带 B/C**：创建关系不是权限链。处置离职或账号失窃时，超管应核查 A 创建过的账号和外发凭据。

**限流**：创建账号沿用现有频率限制，admin 不享有超管的限流豁免（§7）。

## 11. 页面

- **人员管理**（复用 `/admin/users`）：超管看三类角色；admin 看 admin/user，按钮按有效权限显示；显示创建人、创建时间、最近登录、状态，仅供追溯。
- **角色权限**（“系统设置 → 角色权限 → 管理员”，仅超管）：按“人员管理 / 服务资源 / 经营财务 / 内容运维 / 站点设置”分组，模块三档加敏感项单列。保存前预览新增和撤销的权限、受影响的管理员人数，提示“立即对全部管理员生效”。step-up 开关关闭时显示警示（§6.3）。
- **菜单**：缺 `dashboard.read` 时进入第一个有权限的页面；没有任何后台权限时进入个人区。
- **缓存**：权限目录可按 `role + version` 缓存；业务数据缓存键必须含用户身份与范围。退出、切换账号、策略版本变化时清空。
- 文案三语言同步，zh-TW 由 `tools/zh-tw/gen-locale.mjs` 生成；三个品牌规则一致。

## 12. 数据、接口与工作量预算

| 项 | 变更 |
| --- | --- |
| 表 | 新增 0 |
| 字段 | `audit_logs.visibility` 与 `scheduled_test_plans.management_authorization` 共 2 个；`users.role` 新增取值 `super_admin` |
| 配置键 | `settings.admin_role_policy` 1 个（受保护） |
| 端点 | `GET`、`PUT /api/v1/admin/roles/admin/permissions`（1 路径 2 方法）；另增 `POST /admin/users/batch-delete` 以支持原子整批删除；`GET /auth/me` 增加 `permissions`、`policy_version`、`admin_pages`、`admin_features` 字段 |
| 令牌 | 不变（v0.2 的 JWT/refresh 策略版本字段已删除） |
| 环境变量、外部服务 | 0 |
| 平行 CRUD | 0；人员仍走现有 `/admin/users` |

GET 同时返回可编辑的权限目录与依赖，不另设目录端点。只为管理员这一个固定角色开放，不提供动态建角色的 API。

主要工作量（实施前按此拆任务）：

| 部分 | 规模 |
| --- | --- |
| 路由权限声明 + 覆盖测试 | 约 470 条路由 |
| 后端角色判断替换 | 16 个文件，§7 逐项 |
| 前端角色判断替换 | 23 个文件、117 处 |
| 成本字段投影 | 待清点所有含成本字段的 DTO 与导出 |
| 日志可见范围 | 仓储 3 个方法 + 写入分类 |

## 13. 实施默认值（2026-10-07 复审采用）

| 问题 | 建议 |
| --- | --- |
| 内容风控自动封禁是否豁免 admin（§7） | 豁免 staff，与现状一致；管理员滥用由超管人工处理 |
| 面板限流 `ExemptAdmin` 是否覆盖 admin | 只豁免 super_admin |
| 是否要求 admin 必须启用 TOTP 才能使用后台 | 一期不强制，靠 §6.3 的动作级拦截；若需要，作为超管专属的全站设置另行加入 |
| 初始策略是否在 §5.2 基础上多开只读模块 | 不多开，由超管首次审阅时勾选 |

## 14. 迁移、上线与回退

上线（每个站点各执行一次）：

1. 维护窗口内停止管理写入，排空管理长连接和待执行任务；新旧语义的实例不能混跑。
2. 数据库迁移（编号迁移文件，只执行一次；不在启动代码里反复把 admin 提升为 super_admin）：
   - 先确认 `simple_mode_admin_concurrency` 升级标记已存在（§7）；
   - `UPDATE users SET role='super_admin' WHERE role='admin'`，保留 disabled 和软删除状态，并递增这些用户的 `session_generation`；
   - 插入 §5.2 初始策略；
   - `audit_logs` 增加 `visibility`，默认值 `super_admin`。
3. 发布新版本。安装流程改为创建首个 super_admin。
4. 如果全局 Admin API Key 曾交给将来会成为受限管理员的人，由超管先轮换。
5. 超管审阅策略后开始创建管理员。

回退（旧二值版本不认识 `super_admin`：旧二进制看到它会判为非管理员，超管将全部失去后台权限）：

1. 停写；把新建的 admin 降为 user 并停用，撤销其会话；
2. `UPDATE users SET role='admin' WHERE role='super_admin'`；
3. 再换回旧二进制。策略记录和审计数据保留，不删除。
4. 不能用恢复旧备份的方式回退，那会丢掉上线后的真实账务。

## 15. 验收

| 风险 | 验收用例 |
| --- | --- |
| 同级创建 | A 创建 B，B 创建 C；三人有效权限相同；创建请求带权限字段、余额或 `role=super_admin` 被拒绝 |
| 统一修改 | 超管撤去 `users.update` 后，A/B/C 的下一次请求均被拒，不需要重新登录；之后新建的 D 也没有该权限；恢复后同时恢复 |
| staff 开关 | 关闭 `staff.manage` 后，admin 仍可管理 user，但不能创建、修改、删除 admin，也不能做 admin ↔ user 角色变更 |
| 同级互管 | A/B 互查、互改、停用、软删除成功；创建人不限制访问 |
| 跨级 | admin 对 super_admin 的直接、批量、子资源（订单、订阅、身份绑定、API Key）操作全部被拒，批量中夹带超管时整体回滚 |
| 角色竞态 | A 编辑 B 时 B 被提升为超管，A 提交失败 |
| 策略并发 | 两位超管基于同一版本保存，只有一个成功，另一个得到 409；策略与审计原子提交 |
| 多步与长连接 | 撤权后 OAuth 下一步、导出下载、待执行任务、WS 推送均被拒；WS 在 30 秒内断开 |
| 策略容错 | 策略行缺失或损坏时 admin 全部被拒、super_admin 正常；存量策略含未知键时其余键照常生效 |
| 二次验证 | 开关关闭时策略修改仍要求 step-up；开关开启时 §6.3 所列动作要求 step-up；在需要二次验证时 Admin API Key 调用这些接口被拒；随开关档位关闭时沿用现有放行语义 |
| 超管日志 | 列表、计数、搜索、详情（404）、导出、订单附带审计中均看不到超管日志；admin 之间日志可见范围一致 |
| 成本与秘密 | 无 `billing.cost.read` 时汇总、JSON、排行、导出中无成本字段，按成本排序被拒；普通查询不返回凭据明文，显式超管迁移导出例外 |
| 财务边界 | 只有人员编辑权限时，提交 balance、group_rates 或退款被拒 |
| 最后超管 | 并发停用、删除、降级不会让有效 super_admin 变为零；admin 为零时超管正常 |
| 个人入口 | 三种角色访问 `/usage` 都只看到本人数据、不显示所属用户；`/admin/usage` 按权限显示全站 |
| 共享 handler | super_admin 与 admin 访问 `/admin` 下的渠道监控得到全站范围，user 访问用户入口得到本人范围 |
| 路由覆盖 | 新增一条未声明权限的管理路由时覆盖测试失败 |
| 迁移回退 | 迁移只执行一次；按 §14 回退后旧版本中不存在全权的新 admin |

测试层次：单元、真实 PostgreSQL 并发、前端行为、浏览器检查、三品牌回归。

## 16. 实施顺序

1. **角色基础**：`RoleSuperAdmin`、删除旧谓词并逐处处置（§7）、迁移脚本、策略存储与更新接口、最后超管保护、Admin API Key 绑定。
2. **授权执行**：路由声明与覆盖测试、字段与对象规则、二次验证挂载、成本投影、日志可见范围、长连接与多步流程重新鉴权。
3. **前端**：权限驱动的路由与菜单、角色权限页、人员页调整、三语文案。
4. **验收**：§15 全部用例通过后，才在任何站点创建第一个受限 admin。

权限改动单独提交，不与其他功能混在一起。代码推送不代表授权生产迁移或发版。

## 17. 参考

设计原则参考 OWASP Authorization、Session Management、Logging 三份指南（服务端逐请求授权、权限变化后的会话处理、日志完整性）。本文中的角色映射、权限目录和迁移步骤是基于本仓库的设计，外部指南不代表已验证这些实现。

~~~text
https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html
https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html
https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html
~~~

## 18. v0.3 实施前复审结论（2026-10-07）

结论：主方案合理，继续实施。鉴权与登录状态分离、共用策略、默认拒绝、当前目标身份保护、原子审计符合本项目运营团队平级协作的目标。未引入白牌上下游余额、租户层级或个人权限覆盖。

为消除内部冲突并使验收可实现，明确以下细节：

- 凭据规则以“普通查询不可读已存秘密、可替换”为基本边界；保留原超管专用迁移导出和新授权流程即时产物，避免把 §3 与 §2 解读成互相矛盾的功能删除。
- `staff.manage` 依赖 `users.read` 以进入共用人员页；人员增删改的授权以目标当前角色决定，普通用户权限不授予管理员成员写权限。财务和子资源能力继续独立检查。
- 创建者有权创建人员不代表能赠送站点默认余额、默认订阅；受限管理员的创建默认零余额且不隐式赋予付费订阅，额外赋额必须有财务权限。
- 定时任务原表没有发起人字段，需要一个 `management_authorization` JSONB 字段；存身份和安全代次，不存权限快照或策略版本。原批量删除由前端循环单条删除，无法保证整批校验和回滚，因此增加一个整批接口，复用既有删除逻辑。
- 日志“升级前记录”在策略现有 JSON 中保存迁移时最大日志 ID，查询时生成既有 `extra.pre_role_upgrade` 展示标记，不整表改写旧审计；变更通知发往原已验证地址及所有具备已验证通知地址的超管。
- 全局二次验证关闭时，随开关动作保持既有行为；策略修改和创建/提升超管始终验证。模块敏感标签、提示语和验收按这两个档位解释。
- §13 按建议值实施：风控豁免 staff，面板限流仅豁免 super_admin，不强制所有后台访问先启用 TOTP，初始策略不额外开放业务只读模块。

业务代码与测试分别统计；验证完成前不提交或推送。本节明确实现取舍，不代表测试已经通过。

实施依赖说明：普通用户安全处理复用原人员编辑入口，因此 `users.security` 依赖 `users.update`；前端敏感项勾选会同步展示其依赖，改为模块只读时撤去依赖写能力。管理员成员安全处理仍独立归属 `staff.manage`。创建人复用原子创建审计的只读 ID，旧注册或审计保留期外的数据显示未知，不新建人员归属关系。

验收记录：`docs/audit/2026-10-07-rbac-v03-implementation-audit.md`。最终实施基线 main `39d4e56`；品牌验收快照 TapModels `0fde25b7`、tokensavy `1cba6468`。


## 19. 独立复核后的实施细化（2026-10-07）

本节收敛字段边界与实现方式，不改变三角色、共享策略或平级管理员协作模型。

- `auth` 类验证码、找回密码等模板为超管专属，不能通过“协议与模板”权限编辑、预览或恢复；内部发信仍按原模板服务渲染。
- 成本响应按明确字段类别投影；客户实收 `actual_cost` 不再因只选账号而换成上游成本。账号自身倍率/额度消耗与嵌套分组客户费率区分处理。
- 修改字段的权限登记在真实请求结构体；新字段不默认继承模块写权。嵌套账号/代理导入在落库前整体校验。无类型的新账号 provider extra 采用显式创建字段声明，未知键默认超管。
- 现有 `/auth/me` 返回从同一声明生成的 `admin_write_fields`，供表单禁用字段和构造请求；它不是客户端授权依据，不加入令牌、不新增接口或独立配置。
- 受限管理员编辑既有账号时只提交基础字段与被授权的新凭据，不回传整份已投影 extra；`extra: {}` 会清空配置，不能当无操作放行。已有账号高级整体配置替换仍由超管执行，重新授权保留这些配置。
- 无兑换码导出权时已有兑换码明文不可见，生成操作可返回本次新码。无用量导出权时在真正分页处理处限制最多 100 条；不承诺阻止逐页读取合法可见数据。
- 路由覆盖直接调用真实 `router.go`，单一权限表与实际注册表保持一一对应；非 GET 必须声明只读或副作用，插件 UI 独立签名能力 URL 明确列为例外，不隐含开放管理能力。
- 模块改为只读时保留 read 及仍满足依赖的独立敏感权限；只有关闭模块或移除真实依赖才级联取消。策略冲突按结构化 reason 判断并重新加载。

最新验收和统计见 `docs/audit/2026-10-07-rbac-v03-review-fixes.md`。未授权提交、推送或生产迁移。
