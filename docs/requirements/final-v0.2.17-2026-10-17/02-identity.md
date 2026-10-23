# 02 · 角色、会话、管理授权与敏感操作

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`feat(auth): preserve roles sessions and management authorization`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. 固定 super_admin / admin / user 三级角色。super_admin 保有全站权限和超管专属操作；初始化可以只有超级管理员，由其直接建用户和个人 API Key，不依赖先建受限管理员。

2. 全部管理员共用 admin_role_policy；不存在个人特批、上下级或创建人数据隔离。管理员是否能创建/修改其他管理员由 staff.manage 控制；获准的管理员共享同一策略，超管修改后对现有和以后创建的管理员生效。

3. 管理员只能操作获准模块中的业务对象，不能读取或修改超管对象及超管操作日志。超管查看全部范围；创建人、关联分配人等响应也遵循保护规则。

4. 策略编辑仅限超级管理员登录会话。最终需求已移除该动作的 TOTP 二次验证，无论全站开关是否开启都不要求；机器 Admin API Key 仍不可调用。保留权限目录校验、expected_version 冲突 409、变更原因和策略/审计原子提交。

5. 创建或提升超级管理员仍采用原强制二次验证；其余敏感操作按现有全站开关和权限控制。关闭 TOTP 限制的范围仅是统一管理员策略编辑，不是全站取消 MFA。

6. 权限归属由后端 authz 目录、路由表和真实请求结构体字段声明统一决定。前端消费授权快照和 admin_write_fields，不维护第二套财务授权规则；新未声明字段不能默认放行给受限管理员。

7. 成本、计价、余额调整、退款、账号授权、导出和提示词原文分别授权。无导出权的兑换码列表/详情隐藏明文；认证类邮件模板仅超管可编辑；模型上游预览需 accounts.authorize。

8. 改密码/身份等会话生命周期保护、refresh family 撤销、OAuth pending 一次性消费和管理事务继续有效。用户并发数可以从正数改为 0 或另一正数，调整记录必须使用同一请求事务，避免外键跨连接等待；失败回滚时用户和记录一起回滚。

9. 长连接、多步授权和定时任务继续按既有机制复核授权；最后有效超管保护、管理员操作通知、整批用户删除全有或全无保持。内容风控自动封禁豁免 staff，面板限流仅豁免超管。

供应商门户继续复用原用户会话、密码、TOTP、Passkey 和后端对象授权；当前已实现的供应商身份与原站点角色共存。早期 V2 的“仅设计”状态已由后续 V3 实现替代：现已提供 vendor_level1 / vendor_level2 两级独立角色、层级鉴权与双边结算。当前供给范围和敏感导入导出规则见[第 12 组](12-vendor.md)及[统一平台验收](../../vendor-console/UNIFIED-PLATFORM-ACCEPTANCE.md)。

## 2026-10-22 超级管理员协作与 Key 计费补充

- 超级管理员之间没有创建人数据隔离，可通过现有管理入口查看、修改、删除对方创建的业务数据。个人 API Key 页面也显示其他超级管理员所属的 Key，并允许查看、编辑、删除；普通管理员/用户的个人 Key 范围不扩张。
- 可删除其他超级管理员；删除、禁用、降级仍使用现有事务锁、有效身份复核和最后有效超级管理员保护。自删保护、强认证、整批操作原子性和审计保留。删除某个超级管理员只删除其本人所属 Key，不能把协作列表当成删除范围。
- 超级管理员所属 Key 的调用不扣个人余额，零余额或负余额不因余额被拒绝；Key 总额度、5h/1d/7d 限速用量、有效期、IP、并发、分组权限、订阅及平台配额仍按现行规则校验和累计。普通管理员与普通用户继续按原规则扣余额。适用于现有标准计费模式，不改写简易模式已有的显式绕过契约。
- 免扣资格以请求鉴权时 Key 所属用户的角色为准，不因 Key 被交给他人使用而转移；角色变更沿用现有认证缓存失效。当前余额免扣资格与计价修复策略一同固化到原计费事件，重试/缺价恢复不得补扣个人余额，Key 用量和平台用量仍幂等结算。历史事件按其原冻结策略处理，不追溯退款。
- 沿用已有 Key 列表、详情、编辑、删除接口及统一权限查询，不新增 HTTP 路由或配置开关。原计费命令把个人余额扣款额同时用作平台用量；新增内部 BalanceExempt 快照仅用于分离这两者，避免免扣余额意外取消平台额度。没有新增数据表或迁移。

## 入口与配置归属

系统设置→管理员权限、用户管理、各管理表单；GET/PUT /api/v1/admin/roles/admin/permissions。人员 CRUD、个人 Key 和登录仍复用已有入口。

## 最终实现方案

1. 后端 authz 目录、真实路由与 request 字段声明是单一授权来源；前端使用授权快照和 admin_write_fields。

2. 策略保存走版本检查、原因和审计事务；机器 Admin API Key 拒绝，保存统一策略不做 TOTP，创建/提升超管仍沿用强校验。

3. 会话撤销、一次性 pending、最后超管保护和批量删除事务在已有服务/仓储中完成；并发数变更记录沿同一事务连接。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

authz、middleware、server 路由/字段契约；角色、会话、策略版本与 TOTP 边界测试。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| source | 98 | 6587 | 1589 | 0 |
| test | 53 | 2842 | 133 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 18 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 1 | 1 | 02 | [backend/cmd/jwtgen/main.go](https://github.com/holeenlu/sub2api/blob/main/backend/cmd/jwtgen/main.go) |
| A | source | 245 | 0 | 02 | [backend/internal/authz/fields.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/authz/fields.go) |
| A | source | 132 | 0 | 02 | [backend/internal/authz/financial_fields.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/authz/financial_fields.go) |
| A | source | 69 | 0 | 02,12 | [backend/internal/authz/lifecycle.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/authz/lifecycle.go) |
| A | source | 138 | 0 | 02,12 | [backend/internal/authz/mutation.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/authz/mutation.go) |
| A | source | 311 | 0 | 02 | [backend/internal/authz/policy.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/authz/policy.go) |
| A | test | 96 | 0 | 02 | [backend/internal/authz/policy_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/authz/policy_test.go) |
| A | source | 237 | 0 | 02 | [backend/internal/authz/request_fields.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/authz/request_fields.go) |
| A | source | 537 | 0 | 02,07,10,12 | [backend/internal/authz/routes.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/authz/routes.go) |
| M | test | 2 | 0 | 02 | [backend/internal/handler/admin/admin_service_stub_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/admin_service_stub_test.go) |
| A | test | 133 | 0 | 02 | [backend/internal/handler/admin/auth_lifecycle_security_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/auth_lifecycle_security_test.go) |
| M | source | 23 | 23 | 02 | [backend/internal/handler/admin/channel_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/channel_handler.go) |
| M | source | 14 | 14 | 02 | [backend/internal/handler/admin/grok_oauth_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/grok_oauth_handler.go) |
| M | source | 129 | 126 | 02,05 | [backend/internal/handler/admin/group_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/group_handler.go) |
| M | source | 19 | 17 | 02,07 | [backend/internal/handler/admin/openai_oauth_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/openai_oauth_handler.go) |
| A | source | 23 | 0 | 02 | [backend/internal/handler/admin/request_field_schemas.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/request_field_schemas.go) |
| A | test | 139 | 0 | 02 | [backend/internal/handler/admin/request_field_schemas_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/request_field_schemas_test.go) |
| A | source | 64 | 0 | 02 | [backend/internal/handler/admin/role_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/role_handler.go) |
| M | source | 40 | 7 | 02 | [backend/internal/handler/admin/user_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/user_handler.go) |
| M | test | 1 | 1 | 02 | [backend/internal/handler/admin/user_handler_role_stepup_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/user_handler_role_stepup_test.go) |
| M | source | 1 | 1 | 02 | [backend/internal/handler/auth_dingtalk_oauth.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_dingtalk_oauth.go) |
| M | source | 41 | 14 | 02 | [backend/internal/handler/auth_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_handler.go) |
| A | test | 23 | 0 | 02 | [backend/internal/handler/auth_legacy_bind_cookie_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_legacy_bind_cookie_test.go) |
| A | test | 242 | 0 | 02 | [backend/internal/handler/auth_lifecycle_security_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_lifecycle_security_test.go) |
| M | source | 106 | 64 | 02 | [backend/internal/handler/auth_linuxdo_oauth.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_linuxdo_oauth.go) |
| M | test | 6 | 6 | 02 | [backend/internal/handler/auth_linuxdo_oauth_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_linuxdo_oauth_test.go) |
| M | source | 86 | 14 | 02 | [backend/internal/handler/auth_oauth_pending_flow.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_oauth_pending_flow.go) |
| M | test | 17 | 6 | 02 | [backend/internal/handler/auth_oauth_pending_flow_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_oauth_pending_flow_test.go) |
| M | source | 1 | 1 | 02 | [backend/internal/handler/auth_oidc_oauth.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_oidc_oauth.go) |
| M | test | 5 | 4 | 02 | [backend/internal/handler/auth_oidc_oauth_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_oidc_oauth_test.go) |
| M | test | 7 | 5 | 02 | [backend/internal/handler/auth_session_revocation_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_session_revocation_test.go) |
| A | test | 292 | 0 | 02 | [backend/internal/handler/auth_totp_proof_race_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_totp_proof_race_test.go) |
| M | source | 16 | 11 | 02 | [backend/internal/handler/auth_wechat_oauth.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_wechat_oauth.go) |
| M | test | 11 | 7 | 02 | [backend/internal/handler/auth_wechat_oauth_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/auth_wechat_oauth_test.go) |
| M | source | 1 | 1 | 02 | [backend/internal/handler/passkey_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/passkey_handler.go) |
| M | source | 1 | 1 | 02 | [backend/internal/handler/totp_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/totp_handler.go) |
| M | test | 1 | 1 | 02 | [backend/internal/handler/user_handler_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/user_handler_test.go) |
| A | test | 391 | 0 | 02 | [backend/internal/repository/admin_authorization_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/admin_authorization_integration_test.go) |
| A | source | 180 | 0 | 02 | [backend/internal/repository/admin_object_scope.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/admin_object_scope.go) |
| A | source | 88 | 0 | 02 | [backend/internal/repository/admin_role_policy.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/admin_role_policy.go) |
| M | source | 57 | 10 | 02 | [backend/internal/repository/audit_log_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/audit_log_repo.go) |
| A | test | 229 | 0 | 02 | [backend/internal/repository/auth_lifecycle_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/auth_lifecycle_integration_test.go) |
| A | test | 225 | 0 | 02 | [backend/internal/repository/auth_lifecycle_security_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/auth_lifecycle_security_test.go) |
| M | source | 207 | 17 | 02,12 | [backend/internal/repository/proxy_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/proxy_repo.go) |
| M | source | 3 | 1 | 02 | [backend/internal/repository/redeem_code_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/redeem_code_repo.go) |
| M | source | 66 | 24 | 02 | [backend/internal/repository/refresh_token_cache.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/refresh_token_cache.go) |
| M | source | 1 | 1 | 02 | [backend/internal/repository/simple_mode_admin_concurrency.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/simple_mode_admin_concurrency.go) |
| M | source | 4 | 0 | 02 | [backend/internal/repository/user_attribute_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/user_attribute_repo.go) |
| M | source | 183 | 108 | 02 | [backend/internal/repository/user_group_rate_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/user_group_rate_repo.go) |
| M | source | 7 | 0 | 02 | [backend/internal/repository/user_platform_quota_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/user_platform_quota_repo.go) |
| M | source | 7 | 0 | 02 | [backend/internal/repository/user_profile_identity_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/user_profile_identity_repo.go) |
| M | source | 230 | 98 | 02,12 | [backend/internal/repository/user_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/user_repo.go) |
| M | test | 14 | 14 | 02 | [backend/internal/repository/user_repo_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/user_repo_integration_test.go) |
| M | source | 120 | 102 | 02 | [backend/internal/repository/user_subscription_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/user_subscription_repo.go) |
| M | source | 27 | 8 | 02 | [backend/internal/server/middleware/admin_auth.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/admin_auth.go) |
| M | test | 8 | 8 | 02 | [backend/internal/server/middleware/admin_auth_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/admin_auth_test.go) |
| A | source | 564 | 0 | 02,10 | [backend/internal/server/middleware/admin_authorization.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/admin_authorization.go) |
| A | test | 238 | 0 | 02,10 | [backend/internal/server/middleware/admin_authorization_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/admin_authorization_test.go) |
| D | source | 0 | 27 | 02 | `backend/internal/server/middleware/admin_only.go` |
| A | source | 87 | 0 | 02,10 | [backend/internal/server/middleware/admin_response.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/admin_response.go) |
| M | test | 98 | 4 | 02,03 | [backend/internal/server/middleware/audit_log_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/audit_log_test.go) |
| M | source | 1 | 1 | 02 | [backend/internal/server/middleware/backend_mode_guard.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/backend_mode_guard.go) |
| M | source | 1 | 1 | 02 | [backend/internal/server/middleware/cors.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/cors.go) |
| M | source | 16 | 2 | 02 | [backend/internal/server/middleware/jwt_auth.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/jwt_auth.go) |
| M | test | 1 | 1 | 02 | [backend/internal/server/middleware/jwt_auth_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/jwt_auth_test.go) |
| M | source | 1 | 1 | 02 | [backend/internal/server/middleware/panel_rate_limit.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/panel_rate_limit.go) |
| M | test | 7 | 3 | 02 | [backend/internal/server/middleware/panel_rate_limit_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/panel_rate_limit_test.go) |
| M | source | 1 | 1 | 02 | [backend/internal/server/middleware/server_timing.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/server_timing.go) |
| M | source | 9 | 4 | 02 | [backend/internal/server/middleware/session_binding.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/session_binding.go) |
| M | test | 3 | 3 | 02 | [backend/internal/server/middleware/session_binding_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/session_binding_test.go) |
| M | test | 11 | 0 | 02 | [backend/internal/server/middleware/step_up_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/step_up_test.go) |
| A | test | 46 | 0 | 02 | [backend/internal/server/routes/admin_authorization_coverage_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/routes/admin_authorization_coverage_test.go) |
| M | source | 2 | 1 | 02,10 | [backend/internal/server/routes/user.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/routes/user.go) |
| M | source | 4 | 4 | 02 | [backend/internal/service/admin_compliance.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_compliance.go) |
| M | source | 211 | 125 | 02,05 | [backend/internal/service/admin_group.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_group.go) |
| A | source | 169 | 0 | 02 | [backend/internal/service/admin_mutation.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_mutation.go) |
| A | source | 194 | 0 | 02 | [backend/internal/service/admin_object_scope.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_object_scope.go) |
| A | source | 134 | 0 | 02 | [backend/internal/service/admin_role_policy.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_role_policy.go) |
| A | test | 71 | 0 | 02 | [backend/internal/service/admin_security_notice_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_security_notice_test.go) |
| M | source | 5 | 0 | 02 | [backend/internal/service/admin_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_service.go) |
| M | test | 1 | 1 | 02 | [backend/internal/service/admin_service_apikey_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_service_apikey_test.go) |
| M | test | 26 | 10 | 02 | [backend/internal/service/admin_service_delete_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_service_delete_test.go) |
| M | test | 2 | 2 | 02 | [backend/internal/service/admin_service_email_identity_sync_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_service_email_identity_sync_test.go) |
| M | test | 4 | 5 | 02 | [backend/internal/service/admin_service_role_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_service_role_test.go) |
| M | source | 348 | 170 | 02 | [backend/internal/service/admin_user.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_user.go) |
| M | source | 9 | 4 | 02 | [backend/internal/service/antigravity_oauth_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/antigravity_oauth_service.go) |
| M | source | 28 | 3 | 02 | [backend/internal/service/audit_log.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/audit_log.go) |
| M | source | 22 | 4 | 02 | [backend/internal/service/audit_log_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/audit_log_service.go) |
| A | source | 12 | 0 | 02 | [backend/internal/service/auth_admin_policy.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/auth_admin_policy.go) |
| M | source | 1 | 1 | 02 | [backend/internal/service/auth_email_binding.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/auth_email_binding.go) |
| A | test | 62 | 0 | 02 | [backend/internal/service/auth_lifecycle_security_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/auth_lifecycle_security_test.go) |
| M | source | 1 | 1 | 02 | [backend/internal/service/auth_oauth_email_flow.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/auth_oauth_email_flow.go) |
| M | source | 41 | 2 | 02 | [backend/internal/service/auth_pending_identity_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/auth_pending_identity_service.go) |
| M | source | 89 | 49 | 02 | [backend/internal/service/auth_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/auth_service.go) |
| M | test | 16 | 3 | 02 | [backend/internal/service/auth_service_email_bind_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/auth_service_email_bind_test.go) |
| M | source | 1 | 1 | 02 | [backend/internal/service/content_moderation.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/content_moderation.go) |
| M | test | 37 | 32 | 02 | [backend/internal/service/content_moderation_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/content_moderation_test.go) |
| M | source | 24 | 7 | 02 | [backend/internal/service/email_queue_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/email_queue_service.go) |
| M | source | 14 | 4 | 02 | [backend/internal/service/email_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/email_service.go) |
| M | test | 22 | 1 | 02 | [backend/internal/service/email_service_reset_token_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/email_service_reset_token_test.go) |
| M | source | 13 | 8 | 02 | [backend/internal/service/gemini_oauth_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gemini_oauth_service.go) |
| M | source | 5 | 0 | 02 | [backend/internal/service/grok_oauth_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/grok_oauth_service.go) |
| M | source | 27 | 0 | 02 | [backend/internal/service/notification_email_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/notification_email_service.go) |
| M | test | 33 | 0 | 02 | [backend/internal/service/notification_email_service_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/notification_email_service_test.go) |
| M | source | 59 | 7 | 02,12 | [backend/internal/service/oauth_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/oauth_service.go) |
| M | source | 77 | 13 | 02,07,12 | [backend/internal/service/openai_oauth_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_oauth_service.go) |
| M | source | 8 | 7 | 02 | [backend/internal/service/refresh_token_cache.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/refresh_token_cache.go) |
| M | source | 99 | 38 | 02 | [backend/internal/service/totp_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/totp_service.go) |
| M | source | 6 | 2 | 02 | [backend/internal/service/user.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/user.go) |
| M | source | 32 | 6 | 02 | [backend/internal/service/user_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/user_service.go) |
| M | test | 1 | 1 | 02 | [backend/internal/service/user_service_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/user_service_test.go) |
| M | test | 21 | 0 | 02,12 | [frontend/src/api/__tests__/client.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/__tests__/client.spec.ts) |
| A | source | 35 | 0 | 02 | [frontend/src/api/admin/roles.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/admin/roles.ts) |
| M | source | 7 | 2 | 02 | [frontend/src/api/admin/users.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/admin/users.ts) |
| M | source | 75 | 10 | 02,12 | [frontend/src/api/client.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/client.ts) |
| M | source | 17 | 3 | 02 | [frontend/src/components/admin/AdminComplianceDialog.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/AdminComplianceDialog.vue) |
| A | source | 156 | 0 | 02 | [frontend/src/components/admin/AdminRolePermissions.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/AdminRolePermissions.vue) |
| A | source | 75 | 0 | 02 | [frontend/src/components/admin/DelegatedSettings.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/DelegatedSettings.vue) |
| A | test | 105 | 0 | 02 | [frontend/src/components/admin/__tests__/AdminRolePermissions.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/__tests__/AdminRolePermissions.spec.ts) |
| M | source | 9 | 5 | 02 | [frontend/src/components/admin/user/UserCreateModal.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/user/UserCreateModal.vue) |
| M | source | 15 | 5 | 02 | [frontend/src/components/admin/user/UserEditModal.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/user/UserEditModal.vue) |
| M | test | 3 | 0 | 02 | [frontend/src/components/admin/user/__tests__/UserEditModal.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/user/__tests__/UserEditModal.spec.ts) |
| A | test | 76 | 0 | 02 | [frontend/src/composables/__tests__/useHiddenColumns.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/composables/__tests__/useHiddenColumns.spec.ts) |
| M | test | 18 | 0 | 02 | [frontend/src/composables/__tests__/useStepUp.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/composables/__tests__/useStepUp.spec.ts) |
| A | source | 74 | 0 | 02 | [frontend/src/composables/useHiddenColumns.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/composables/useHiddenColumns.ts) |
| M | source | 18 | 0 | 02 | [frontend/src/composables/useStepUp.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/composables/useStepUp.ts) |
| M | source | 6 | 2 | 02 | [frontend/src/features/prompt-audit/PromptAuditView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/prompt-audit/PromptAuditView.vue) |
| M | test | 2 | 0 | 02 | [frontend/src/features/prompt-audit/__tests__/PromptAuditView.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/prompt-audit/__tests__/PromptAuditView.spec.ts) |
| M | source | 7 | 6 | 02 | [frontend/src/features/prompt-audit/components/EventWorkspace.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/prompt-audit/components/EventWorkspace.vue) |
| M | test | 2 | 0 | 02 | [frontend/src/stores/__tests__/adminSettings.retry.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/stores/__tests__/adminSettings.retry.spec.ts) |
| A | test | 48 | 0 | 02 | [frontend/src/stores/__tests__/auth.permissions.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/stores/__tests__/auth.permissions.spec.ts) |
| M | source | 40 | 8 | 02 | [frontend/src/stores/adminCompliance.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/stores/adminCompliance.ts) |
| M | source | 37 | 2 | 02 | [frontend/src/stores/auth.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/stores/auth.ts) |
| M | source | 7 | 4 | 02 | [frontend/src/views/admin/AnnouncementsView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/AnnouncementsView.vue) |
| M | source | 5 | 3 | 02 | [frontend/src/views/admin/AuditLogView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/AuditLogView.vue) |
| M | source | 20 | 17 | 02 | [frontend/src/views/admin/ChannelsView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ChannelsView.vue) |
| M | source | 103 | 99 | 02 | [frontend/src/views/admin/GroupsView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/GroupsView.vue) |
| M | source | 8 | 5 | 02 | [frontend/src/views/admin/PromoCodesView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/PromoCodesView.vue) |
| M | source | 55 | 71 | 02 | [frontend/src/views/admin/ProxiesView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/ProxiesView.vue) |
| M | source | 8 | 4 | 02 | [frontend/src/views/admin/RedeemView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/RedeemView.vue) |
| M | source | 6 | 4 | 02 | [frontend/src/views/admin/RiskControlView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/RiskControlView.vue) |
| M | source | 36 | 35 | 02 | [frontend/src/views/admin/UsersView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/UsersView.vue) |
| M | test | 3 | 0 | 02 | [frontend/src/views/admin/__tests__/GroupsView.codexManifest.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/GroupsView.codexManifest.spec.ts) |
| M | test | 1 | 1 | 02 | [frontend/src/views/admin/__tests__/GroupsView.columnSettings.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/GroupsView.columnSettings.spec.ts) |
| M | test | 1 | 1 | 02 | [frontend/src/views/admin/__tests__/GroupsView.duplicate.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/GroupsView.duplicate.spec.ts) |
| M | test | 14 | 3 | 02 | [frontend/src/views/admin/__tests__/ProxiesView.credentials.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/ProxiesView.credentials.spec.ts) |
| M | test | 3 | 0 | 02 | [frontend/src/views/admin/__tests__/ProxiesView.filters.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/ProxiesView.filters.spec.ts) |
| M | test | 3 | 0 | 02 | [frontend/src/views/admin/__tests__/RedeemView.batchUpdate.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/RedeemView.batchUpdate.spec.ts) |
| M | test | 3 | 0 | 02 | [frontend/src/views/admin/__tests__/RiskControlView.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/RiskControlView.spec.ts) |
| M | test | 18 | 10 | 02 | [frontend/src/views/admin/__tests__/UsersView.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/UsersView.spec.ts) |
| M | source | 38 | 146 | 02 | [frontend/src/views/admin/settings/EmailTemplateEditor.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/settings/EmailTemplateEditor.vue) |
