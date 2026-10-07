# 02 · 三级角色、会话与管理授权

归档日期：2026-10-07。文件快照：main `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238`；官方比较基线 `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`。

本页登记最终需求与实际文件归属；不把本轮提交整理等同于重新实现或完整安全审计。

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

## 入口与配置归属

系统设置→管理员权限、用户管理、各管理表单；GET/PUT /api/v1/admin/roles/admin/permissions。人员 CRUD、个人 Key 和登录仍复用已有入口。

## 验收边界

重点用例为三角色对象隔离、财务字段投影、无 TOTP 的策略保存、机器密钥/普通管理员拒绝、409 冲突、权限降级保留独立授权，以及真实数据库并发数修改/回滚。完整设计见 ../../design/2026-10-06-three-tier-roles-permissions.md；旧审计中的强制策略 TOTP 描述已被最终需求覆盖。

验证状态统一见 [总说明](README.md#验证和未关闭边界)。本页列的是验收要求，不是宣称本轮已重新运行这些检查。

## 对比统计

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 业务代码 | 98 | 6062 | 1522 | 0 |
| 测试/夹具 | 53 | 2699 | 133 | 0 |

统计为最终文件相对固定官方基线的累计差异，并非本次整理新写的代码。对重命名统一展开为删除/新增；跨域文件只设一个主归属。

## 修改文件

| 状态 | 类型 | +行 | −行 | 文件 |
| --- | --- | ---: | ---: | --- |
| M | 业务代码 | 1 | 1 | [backend/cmd/jwtgen/main.go](../../../backend/cmd/jwtgen/main.go) |
| A | 业务代码 | 245 | 0 | [backend/internal/authz/fields.go](../../../backend/internal/authz/fields.go) |
| A | 业务代码 | 132 | 0 | [backend/internal/authz/financial_fields.go](../../../backend/internal/authz/financial_fields.go) |
| A | 业务代码 | 53 | 0 | [backend/internal/authz/lifecycle.go](../../../backend/internal/authz/lifecycle.go) |
| A | 业务代码 | 122 | 0 | [backend/internal/authz/mutation.go](../../../backend/internal/authz/mutation.go) |
| A | 业务代码 | 311 | 0 | [backend/internal/authz/policy.go](../../../backend/internal/authz/policy.go) |
| A | 测试/夹具 | 96 | 0 | [backend/internal/authz/policy_test.go](../../../backend/internal/authz/policy_test.go) |
| A | 业务代码 | 237 | 0 | [backend/internal/authz/request_fields.go](../../../backend/internal/authz/request_fields.go) |
| A | 业务代码 | 498 | 0 | [backend/internal/authz/routes.go](../../../backend/internal/authz/routes.go) |
| M | 测试/夹具 | 2 | 0 | [backend/internal/handler/admin/admin_service_stub_test.go](../../../backend/internal/handler/admin/admin_service_stub_test.go) |
| A | 测试/夹具 | 133 | 0 | [backend/internal/handler/admin/auth_lifecycle_security_test.go](../../../backend/internal/handler/admin/auth_lifecycle_security_test.go) |
| M | 业务代码 | 23 | 23 | [backend/internal/handler/admin/channel_handler.go](../../../backend/internal/handler/admin/channel_handler.go) |
| M | 业务代码 | 14 | 14 | [backend/internal/handler/admin/grok_oauth_handler.go](../../../backend/internal/handler/admin/grok_oauth_handler.go) |
| M | 业务代码 | 121 | 121 | [backend/internal/handler/admin/group_handler.go](../../../backend/internal/handler/admin/group_handler.go) |
| M | 业务代码 | 15 | 15 | [backend/internal/handler/admin/openai_oauth_handler.go](../../../backend/internal/handler/admin/openai_oauth_handler.go) |
| A | 业务代码 | 23 | 0 | [backend/internal/handler/admin/request_field_schemas.go](../../../backend/internal/handler/admin/request_field_schemas.go) |
| A | 测试/夹具 | 132 | 0 | [backend/internal/handler/admin/request_field_schemas_test.go](../../../backend/internal/handler/admin/request_field_schemas_test.go) |
| A | 业务代码 | 64 | 0 | [backend/internal/handler/admin/role_handler.go](../../../backend/internal/handler/admin/role_handler.go) |
| M | 业务代码 | 40 | 7 | [backend/internal/handler/admin/user_handler.go](../../../backend/internal/handler/admin/user_handler.go) |
| M | 测试/夹具 | 1 | 1 | [backend/internal/handler/admin/user_handler_role_stepup_test.go](../../../backend/internal/handler/admin/user_handler_role_stepup_test.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/handler/auth_dingtalk_oauth.go](../../../backend/internal/handler/auth_dingtalk_oauth.go) |
| M | 业务代码 | 41 | 14 | [backend/internal/handler/auth_handler.go](../../../backend/internal/handler/auth_handler.go) |
| A | 测试/夹具 | 23 | 0 | [backend/internal/handler/auth_legacy_bind_cookie_test.go](../../../backend/internal/handler/auth_legacy_bind_cookie_test.go) |
| A | 测试/夹具 | 242 | 0 | [backend/internal/handler/auth_lifecycle_security_test.go](../../../backend/internal/handler/auth_lifecycle_security_test.go) |
| M | 业务代码 | 106 | 64 | [backend/internal/handler/auth_linuxdo_oauth.go](../../../backend/internal/handler/auth_linuxdo_oauth.go) |
| M | 测试/夹具 | 6 | 6 | [backend/internal/handler/auth_linuxdo_oauth_test.go](../../../backend/internal/handler/auth_linuxdo_oauth_test.go) |
| M | 业务代码 | 86 | 14 | [backend/internal/handler/auth_oauth_pending_flow.go](../../../backend/internal/handler/auth_oauth_pending_flow.go) |
| M | 测试/夹具 | 17 | 6 | [backend/internal/handler/auth_oauth_pending_flow_test.go](../../../backend/internal/handler/auth_oauth_pending_flow_test.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/handler/auth_oidc_oauth.go](../../../backend/internal/handler/auth_oidc_oauth.go) |
| M | 测试/夹具 | 5 | 4 | [backend/internal/handler/auth_oidc_oauth_test.go](../../../backend/internal/handler/auth_oidc_oauth_test.go) |
| M | 测试/夹具 | 7 | 5 | [backend/internal/handler/auth_session_revocation_test.go](../../../backend/internal/handler/auth_session_revocation_test.go) |
| A | 测试/夹具 | 292 | 0 | [backend/internal/handler/auth_totp_proof_race_test.go](../../../backend/internal/handler/auth_totp_proof_race_test.go) |
| M | 业务代码 | 16 | 11 | [backend/internal/handler/auth_wechat_oauth.go](../../../backend/internal/handler/auth_wechat_oauth.go) |
| M | 测试/夹具 | 11 | 7 | [backend/internal/handler/auth_wechat_oauth_test.go](../../../backend/internal/handler/auth_wechat_oauth_test.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/handler/passkey_handler.go](../../../backend/internal/handler/passkey_handler.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/handler/totp_handler.go](../../../backend/internal/handler/totp_handler.go) |
| M | 测试/夹具 | 1 | 1 | [backend/internal/handler/user_handler_test.go](../../../backend/internal/handler/user_handler_test.go) |
| A | 测试/夹具 | 391 | 0 | [backend/internal/repository/admin_authorization_integration_test.go](../../../backend/internal/repository/admin_authorization_integration_test.go) |
| A | 业务代码 | 180 | 0 | [backend/internal/repository/admin_object_scope.go](../../../backend/internal/repository/admin_object_scope.go) |
| A | 业务代码 | 88 | 0 | [backend/internal/repository/admin_role_policy.go](../../../backend/internal/repository/admin_role_policy.go) |
| M | 业务代码 | 57 | 10 | [backend/internal/repository/audit_log_repo.go](../../../backend/internal/repository/audit_log_repo.go) |
| A | 测试/夹具 | 229 | 0 | [backend/internal/repository/auth_lifecycle_integration_test.go](../../../backend/internal/repository/auth_lifecycle_integration_test.go) |
| A | 测试/夹具 | 225 | 0 | [backend/internal/repository/auth_lifecycle_security_test.go](../../../backend/internal/repository/auth_lifecycle_security_test.go) |
| M | 业务代码 | 22 | 1 | [backend/internal/repository/proxy_repo.go](../../../backend/internal/repository/proxy_repo.go) |
| M | 业务代码 | 3 | 1 | [backend/internal/repository/redeem_code_repo.go](../../../backend/internal/repository/redeem_code_repo.go) |
| M | 业务代码 | 66 | 24 | [backend/internal/repository/refresh_token_cache.go](../../../backend/internal/repository/refresh_token_cache.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/repository/simple_mode_admin_concurrency.go](../../../backend/internal/repository/simple_mode_admin_concurrency.go) |
| M | 业务代码 | 4 | 0 | [backend/internal/repository/user_attribute_repo.go](../../../backend/internal/repository/user_attribute_repo.go) |
| M | 业务代码 | 183 | 108 | [backend/internal/repository/user_group_rate_repo.go](../../../backend/internal/repository/user_group_rate_repo.go) |
| M | 业务代码 | 7 | 0 | [backend/internal/repository/user_platform_quota_repo.go](../../../backend/internal/repository/user_platform_quota_repo.go) |
| M | 业务代码 | 7 | 0 | [backend/internal/repository/user_profile_identity_repo.go](../../../backend/internal/repository/user_profile_identity_repo.go) |
| M | 业务代码 | 208 | 92 | [backend/internal/repository/user_repo.go](../../../backend/internal/repository/user_repo.go) |
| M | 测试/夹具 | 14 | 14 | [backend/internal/repository/user_repo_integration_test.go](../../../backend/internal/repository/user_repo_integration_test.go) |
| M | 业务代码 | 120 | 102 | [backend/internal/repository/user_subscription_repo.go](../../../backend/internal/repository/user_subscription_repo.go) |
| M | 业务代码 | 27 | 8 | [backend/internal/server/middleware/admin_auth.go](../../../backend/internal/server/middleware/admin_auth.go) |
| M | 测试/夹具 | 8 | 8 | [backend/internal/server/middleware/admin_auth_test.go](../../../backend/internal/server/middleware/admin_auth_test.go) |
| A | 业务代码 | 559 | 0 | [backend/internal/server/middleware/admin_authorization.go](../../../backend/internal/server/middleware/admin_authorization.go) |
| A | 测试/夹具 | 214 | 0 | [backend/internal/server/middleware/admin_authorization_test.go](../../../backend/internal/server/middleware/admin_authorization_test.go) |
| D | 业务代码 | 0 | 27 | `backend/internal/server/middleware/admin_only.go`（已删除） |
| A | 业务代码 | 83 | 0 | [backend/internal/server/middleware/admin_response.go](../../../backend/internal/server/middleware/admin_response.go) |
| M | 测试/夹具 | 6 | 4 | [backend/internal/server/middleware/audit_log_test.go](../../../backend/internal/server/middleware/audit_log_test.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/server/middleware/backend_mode_guard.go](../../../backend/internal/server/middleware/backend_mode_guard.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/server/middleware/cors.go](../../../backend/internal/server/middleware/cors.go) |
| M | 业务代码 | 16 | 2 | [backend/internal/server/middleware/jwt_auth.go](../../../backend/internal/server/middleware/jwt_auth.go) |
| M | 测试/夹具 | 1 | 1 | [backend/internal/server/middleware/jwt_auth_test.go](../../../backend/internal/server/middleware/jwt_auth_test.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/server/middleware/panel_rate_limit.go](../../../backend/internal/server/middleware/panel_rate_limit.go) |
| M | 测试/夹具 | 7 | 3 | [backend/internal/server/middleware/panel_rate_limit_test.go](../../../backend/internal/server/middleware/panel_rate_limit_test.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/server/middleware/server_timing.go](../../../backend/internal/server/middleware/server_timing.go) |
| M | 业务代码 | 9 | 4 | [backend/internal/server/middleware/session_binding.go](../../../backend/internal/server/middleware/session_binding.go) |
| M | 测试/夹具 | 3 | 3 | [backend/internal/server/middleware/session_binding_test.go](../../../backend/internal/server/middleware/session_binding_test.go) |
| M | 测试/夹具 | 11 | 0 | [backend/internal/server/middleware/step_up_test.go](../../../backend/internal/server/middleware/step_up_test.go) |
| A | 测试/夹具 | 46 | 0 | [backend/internal/server/routes/admin_authorization_coverage_test.go](../../../backend/internal/server/routes/admin_authorization_coverage_test.go) |
| M | 业务代码 | 1 | 0 | [backend/internal/server/routes/user.go](../../../backend/internal/server/routes/user.go) |
| M | 业务代码 | 4 | 4 | [backend/internal/service/admin_compliance.go](../../../backend/internal/service/admin_compliance.go) |
| M | 业务代码 | 106 | 102 | [backend/internal/service/admin_group.go](../../../backend/internal/service/admin_group.go) |
| A | 业务代码 | 169 | 0 | [backend/internal/service/admin_mutation.go](../../../backend/internal/service/admin_mutation.go) |
| A | 业务代码 | 194 | 0 | [backend/internal/service/admin_object_scope.go](../../../backend/internal/service/admin_object_scope.go) |
| A | 业务代码 | 134 | 0 | [backend/internal/service/admin_role_policy.go](../../../backend/internal/service/admin_role_policy.go) |
| A | 测试/夹具 | 71 | 0 | [backend/internal/service/admin_security_notice_test.go](../../../backend/internal/service/admin_security_notice_test.go) |
| M | 业务代码 | 5 | 0 | [backend/internal/service/admin_service.go](../../../backend/internal/service/admin_service.go) |
| M | 测试/夹具 | 1 | 1 | [backend/internal/service/admin_service_apikey_test.go](../../../backend/internal/service/admin_service_apikey_test.go) |
| M | 测试/夹具 | 26 | 10 | [backend/internal/service/admin_service_delete_test.go](../../../backend/internal/service/admin_service_delete_test.go) |
| M | 测试/夹具 | 2 | 2 | [backend/internal/service/admin_service_email_identity_sync_test.go](../../../backend/internal/service/admin_service_email_identity_sync_test.go) |
| M | 测试/夹具 | 4 | 5 | [backend/internal/service/admin_service_role_test.go](../../../backend/internal/service/admin_service_role_test.go) |
| M | 业务代码 | 348 | 170 | [backend/internal/service/admin_user.go](../../../backend/internal/service/admin_user.go) |
| M | 业务代码 | 9 | 4 | [backend/internal/service/antigravity_oauth_service.go](../../../backend/internal/service/antigravity_oauth_service.go) |
| M | 业务代码 | 28 | 3 | [backend/internal/service/audit_log.go](../../../backend/internal/service/audit_log.go) |
| M | 业务代码 | 22 | 4 | [backend/internal/service/audit_log_service.go](../../../backend/internal/service/audit_log_service.go) |
| A | 业务代码 | 12 | 0 | [backend/internal/service/auth_admin_policy.go](../../../backend/internal/service/auth_admin_policy.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/service/auth_email_binding.go](../../../backend/internal/service/auth_email_binding.go) |
| A | 测试/夹具 | 62 | 0 | [backend/internal/service/auth_lifecycle_security_test.go](../../../backend/internal/service/auth_lifecycle_security_test.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/service/auth_oauth_email_flow.go](../../../backend/internal/service/auth_oauth_email_flow.go) |
| M | 业务代码 | 41 | 2 | [backend/internal/service/auth_pending_identity_service.go](../../../backend/internal/service/auth_pending_identity_service.go) |
| M | 业务代码 | 89 | 49 | [backend/internal/service/auth_service.go](../../../backend/internal/service/auth_service.go) |
| M | 测试/夹具 | 16 | 3 | [backend/internal/service/auth_service_email_bind_test.go](../../../backend/internal/service/auth_service_email_bind_test.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/service/content_moderation.go](../../../backend/internal/service/content_moderation.go) |
| M | 测试/夹具 | 37 | 32 | [backend/internal/service/content_moderation_test.go](../../../backend/internal/service/content_moderation_test.go) |
| M | 业务代码 | 24 | 7 | [backend/internal/service/email_queue_service.go](../../../backend/internal/service/email_queue_service.go) |
| M | 业务代码 | 14 | 4 | [backend/internal/service/email_service.go](../../../backend/internal/service/email_service.go) |
| M | 测试/夹具 | 22 | 1 | [backend/internal/service/email_service_reset_token_test.go](../../../backend/internal/service/email_service_reset_token_test.go) |
| M | 业务代码 | 13 | 8 | [backend/internal/service/gemini_oauth_service.go](../../../backend/internal/service/gemini_oauth_service.go) |
| M | 业务代码 | 5 | 0 | [backend/internal/service/grok_oauth_service.go](../../../backend/internal/service/grok_oauth_service.go) |
| M | 业务代码 | 27 | 0 | [backend/internal/service/notification_email_service.go](../../../backend/internal/service/notification_email_service.go) |
| M | 测试/夹具 | 33 | 0 | [backend/internal/service/notification_email_service_test.go](../../../backend/internal/service/notification_email_service_test.go) |
| M | 业务代码 | 10 | 5 | [backend/internal/service/oauth_service.go](../../../backend/internal/service/oauth_service.go) |
| M | 业务代码 | 11 | 6 | [backend/internal/service/openai_oauth_service.go](../../../backend/internal/service/openai_oauth_service.go) |
| M | 业务代码 | 8 | 7 | [backend/internal/service/refresh_token_cache.go](../../../backend/internal/service/refresh_token_cache.go) |
| M | 业务代码 | 99 | 38 | [backend/internal/service/totp_service.go](../../../backend/internal/service/totp_service.go) |
| M | 业务代码 | 6 | 2 | [backend/internal/service/user.go](../../../backend/internal/service/user.go) |
| M | 业务代码 | 32 | 6 | [backend/internal/service/user_service.go](../../../backend/internal/service/user_service.go) |
| M | 测试/夹具 | 1 | 1 | [backend/internal/service/user_service_test.go](../../../backend/internal/service/user_service_test.go) |
| M | 测试/夹具 | 1 | 0 | [frontend/src/api/__tests__/client.spec.ts](../../../frontend/src/api/__tests__/client.spec.ts) |
| A | 业务代码 | 35 | 0 | [frontend/src/api/admin/roles.ts](../../../frontend/src/api/admin/roles.ts) |
| M | 业务代码 | 7 | 2 | [frontend/src/api/admin/users.ts](../../../frontend/src/api/admin/users.ts) |
| M | 业务代码 | 74 | 9 | [frontend/src/api/client.ts](../../../frontend/src/api/client.ts) |
| M | 业务代码 | 17 | 3 | [frontend/src/components/admin/AdminComplianceDialog.vue](../../../frontend/src/components/admin/AdminComplianceDialog.vue) |
| A | 业务代码 | 156 | 0 | [frontend/src/components/admin/AdminRolePermissions.vue](../../../frontend/src/components/admin/AdminRolePermissions.vue) |
| A | 业务代码 | 75 | 0 | [frontend/src/components/admin/DelegatedSettings.vue](../../../frontend/src/components/admin/DelegatedSettings.vue) |
| A | 测试/夹具 | 105 | 0 | [frontend/src/components/admin/__tests__/AdminRolePermissions.spec.ts](../../../frontend/src/components/admin/__tests__/AdminRolePermissions.spec.ts) |
| M | 业务代码 | 9 | 5 | [frontend/src/components/admin/user/UserCreateModal.vue](../../../frontend/src/components/admin/user/UserCreateModal.vue) |
| M | 业务代码 | 15 | 5 | [frontend/src/components/admin/user/UserEditModal.vue](../../../frontend/src/components/admin/user/UserEditModal.vue) |
| M | 测试/夹具 | 3 | 0 | [frontend/src/components/admin/user/__tests__/UserEditModal.spec.ts](../../../frontend/src/components/admin/user/__tests__/UserEditModal.spec.ts) |
| A | 测试/夹具 | 76 | 0 | [frontend/src/composables/__tests__/useHiddenColumns.spec.ts](../../../frontend/src/composables/__tests__/useHiddenColumns.spec.ts) |
| M | 测试/夹具 | 18 | 0 | [frontend/src/composables/__tests__/useStepUp.spec.ts](../../../frontend/src/composables/__tests__/useStepUp.spec.ts) |
| A | 业务代码 | 74 | 0 | [frontend/src/composables/useHiddenColumns.ts](../../../frontend/src/composables/useHiddenColumns.ts) |
| M | 业务代码 | 18 | 0 | [frontend/src/composables/useStepUp.ts](../../../frontend/src/composables/useStepUp.ts) |
| M | 业务代码 | 6 | 2 | [frontend/src/features/prompt-audit/PromptAuditView.vue](../../../frontend/src/features/prompt-audit/PromptAuditView.vue) |
| M | 测试/夹具 | 2 | 0 | [frontend/src/features/prompt-audit/__tests__/PromptAuditView.spec.ts](../../../frontend/src/features/prompt-audit/__tests__/PromptAuditView.spec.ts) |
| M | 业务代码 | 7 | 6 | [frontend/src/features/prompt-audit/components/EventWorkspace.vue](../../../frontend/src/features/prompt-audit/components/EventWorkspace.vue) |
| M | 测试/夹具 | 2 | 0 | [frontend/src/stores/__tests__/adminSettings.retry.spec.ts](../../../frontend/src/stores/__tests__/adminSettings.retry.spec.ts) |
| A | 测试/夹具 | 48 | 0 | [frontend/src/stores/__tests__/auth.permissions.spec.ts](../../../frontend/src/stores/__tests__/auth.permissions.spec.ts) |
| M | 业务代码 | 40 | 8 | [frontend/src/stores/adminCompliance.ts](../../../frontend/src/stores/adminCompliance.ts) |
| M | 业务代码 | 37 | 2 | [frontend/src/stores/auth.ts](../../../frontend/src/stores/auth.ts) |
| M | 业务代码 | 7 | 4 | [frontend/src/views/admin/AnnouncementsView.vue](../../../frontend/src/views/admin/AnnouncementsView.vue) |
| M | 业务代码 | 5 | 3 | [frontend/src/views/admin/AuditLogView.vue](../../../frontend/src/views/admin/AuditLogView.vue) |
| M | 业务代码 | 16 | 13 | [frontend/src/views/admin/ChannelsView.vue](../../../frontend/src/views/admin/ChannelsView.vue) |
| M | 业务代码 | 103 | 99 | [frontend/src/views/admin/GroupsView.vue](../../../frontend/src/views/admin/GroupsView.vue) |
| M | 业务代码 | 8 | 5 | [frontend/src/views/admin/PromoCodesView.vue](../../../frontend/src/views/admin/PromoCodesView.vue) |
| M | 业务代码 | 55 | 71 | [frontend/src/views/admin/ProxiesView.vue](../../../frontend/src/views/admin/ProxiesView.vue) |
| M | 业务代码 | 8 | 4 | [frontend/src/views/admin/RedeemView.vue](../../../frontend/src/views/admin/RedeemView.vue) |
| M | 业务代码 | 6 | 4 | [frontend/src/views/admin/RiskControlView.vue](../../../frontend/src/views/admin/RiskControlView.vue) |
| M | 业务代码 | 36 | 35 | [frontend/src/views/admin/UsersView.vue](../../../frontend/src/views/admin/UsersView.vue) |
| M | 测试/夹具 | 3 | 0 | [frontend/src/views/admin/__tests__/GroupsView.codexManifest.spec.ts](../../../frontend/src/views/admin/__tests__/GroupsView.codexManifest.spec.ts) |
| M | 测试/夹具 | 1 | 1 | [frontend/src/views/admin/__tests__/GroupsView.columnSettings.spec.ts](../../../frontend/src/views/admin/__tests__/GroupsView.columnSettings.spec.ts) |
| M | 测试/夹具 | 1 | 1 | [frontend/src/views/admin/__tests__/GroupsView.duplicate.spec.ts](../../../frontend/src/views/admin/__tests__/GroupsView.duplicate.spec.ts) |
| M | 测试/夹具 | 14 | 3 | [frontend/src/views/admin/__tests__/ProxiesView.credentials.spec.ts](../../../frontend/src/views/admin/__tests__/ProxiesView.credentials.spec.ts) |
| M | 测试/夹具 | 3 | 0 | [frontend/src/views/admin/__tests__/ProxiesView.filters.spec.ts](../../../frontend/src/views/admin/__tests__/ProxiesView.filters.spec.ts) |
| M | 测试/夹具 | 3 | 0 | [frontend/src/views/admin/__tests__/RedeemView.batchUpdate.spec.ts](../../../frontend/src/views/admin/__tests__/RedeemView.batchUpdate.spec.ts) |
| M | 测试/夹具 | 3 | 0 | [frontend/src/views/admin/__tests__/RiskControlView.spec.ts](../../../frontend/src/views/admin/__tests__/RiskControlView.spec.ts) |
| M | 测试/夹具 | 18 | 10 | [frontend/src/views/admin/__tests__/UsersView.spec.ts](../../../frontend/src/views/admin/__tests__/UsersView.spec.ts) |
| M | 业务代码 | 38 | 146 | [frontend/src/views/admin/settings/EmailTemplateEditor.vue](../../../frontend/src/views/admin/settings/EmailTemplateEditor.vue) |
