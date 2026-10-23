# 03 · 网关安全、隔离、准入与敏感信息保护

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`fix(gateway): preserve tenant isolation and request safety`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. 网关、Responses HTTP/WS、调度准入和原生并发以固定 Wei-Shaw 基线为主，不重新导入 ranxi/sub4api 的打票、Cookie 绑定、额外排队或独立模型目录治理。

2. 安全客户端 IP 取自可信代理链；转发元数据兼容开关不能改变 ACL、认证限流及会话 IP 的判定。

3. 图片转存保留解析后按公网 IP 连接、逐跳重定向复验、HTTPS 降级限制、文件体积与真实图片类型校验，复用已有公共出口机制。

4. WebSocket 首轮、续轮和透传沿用 Claude/Codex 客户端资格及模型检查；响应引用按已认证用户归属验证，同用户的 Key 可以共享其获准响应，跨用户不允许。连接池兼容键包含已认证租户隔离信息。

5. Grok 语音 HTTP 与实时连接复用原生用户并发；音色列表、增改删、TTS 和 Realtime 引用检查用户/分组/上游账号归属。保留现有账单行为，不新增异步媒体任务系统。

保留客户端正文读取失败的分阶段诊断：记录已读取字节、内容长度、读取阶段和安全错误分类，覆盖 Responses、Messages、Chat、图片、Grok、Embeddings、Alpha Search 和 composite 路由。正文超限、截断和取消仍按现有错误路径返回，不记录正文内容或凭据；解析前预算及请求取消约束继续由原入口承担。

2026-10-22 审查修复：拒答与设备记忆写入 Redis 失败时，保留受原容量、原 TTL 限制的待重试记录；共享快照刷新不能覆盖这些记录。管理员清空在 Redis 原子推进清空代际，其他实例的旧记录不得通过延迟重试恢复；清空后的新拒答正常生效。沿用原检查、记忆和清空入口，无新增产品开关；Redis 增加内部代际键，不执行生产数据迁移。

## 入口与配置归属

原有 /v1 网关、Responses WebSocket、Grok 音频入口；没有因本次整理新增公开 API。

## 最终实现方案

1. 继续用原生鉴权、调度、并发与公共出口，不新增准入服务。可信代理链决定安全客户端 IP。

2. 图片逐跳复验公网出口、禁止 HTTPS 降级；WS response owner 和连接兼容键按认证租户隔离。

3. Grok 语音占用原用户并发，音色 CRUD/引用在既有仓储校验归属；BPS 的恢复例外见 07。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

可信代理、图片出口、WS tenant/owner、Grok 音色与用户并发已有回归。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| source | 35 | 3600 | 680 | 0 |
| test | 20 | 2412 | 103 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 50 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 45 | 8 | 03,06,08 | [backend/internal/handler/gateway_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/gateway_handler.go) |
| M | source | 10 | 2 | 03,06,08 | [backend/internal/handler/gateway_handler_chat_completions.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/gateway_handler_chat_completions.go) |
| M | source | 7 | 1 | 03,08 | [backend/internal/handler/gateway_handler_responses.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/gateway_handler_responses.go) |
| M | source | 7 | 5 | 03,06 | [backend/internal/handler/gemini_v1beta_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/gemini_v1beta_handler.go) |
| M | source | 152 | 34 | 03 | [backend/internal/handler/grok_audio.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/grok_audio.go) |
| M | test | 44 | 0 | 03 | [backend/internal/handler/grok_audio_billing_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/grok_audio_billing_test.go) |
| M | source | 1 | 0 | 03 | [backend/internal/handler/grok_media.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/grok_media.go) |
| M | source | 1 | 0 | 03 | [backend/internal/handler/image_task_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/image_task_handler.go) |
| M | test | 55 | 0 | 03 | [backend/internal/handler/no_account_error_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/no_account_error_test.go) |
| M | source | 1 | 0 | 03 | [backend/internal/handler/openai_alpha_search.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_alpha_search.go) |
| M | source | 1 | 0 | 03 | [backend/internal/handler/openai_embeddings.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_embeddings.go) |
| M | source | 441 | 103 | 03,06,07 | [backend/internal/handler/openai_gateway_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_gateway_handler.go) |
| M | test | 21 | 24 | 03 | [backend/internal/handler/openai_gateway_ws_model_allowlist_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_gateway_ws_model_allowlist_test.go) |
| M | source | 2 | 69 | 03 | [backend/internal/handler/request_body_read_log.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/request_body_read_log.go) |
| M | test | 9 | 12 | 03 | [backend/internal/handler/request_body_read_log_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/request_body_read_log_test.go) |
| A | source | 212 | 0 | 03 | [backend/internal/pkg/httputil/body_read_diagnostics.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/httputil/body_read_diagnostics.go) |
| A | test | 129 | 0 | 03 | [backend/internal/pkg/httputil/body_read_diagnostics_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/httputil/body_read_diagnostics_test.go) |
| M | source | 4 | 11 | 03 | [backend/internal/pkg/ip/ip.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/ip/ip.go) |
| M | test | 7 | 7 | 03 | [backend/internal/pkg/ip/ip_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/ip/ip_test.go) |
| A | source | 48 | 0 | 03 | [backend/internal/repository/model_policy_preflight.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/model_policy_preflight.go) |
| M | source | 16 | 6 | 03 | [backend/internal/server/middleware/audit_log.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/audit_log.go) |
| M | source | 2 | 0 | 03 | [backend/internal/server/middleware/group_model_allowlist.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/group_model_allowlist.go) |
| M | test | 47 | 0 | 03 | [backend/internal/server/middleware/group_model_allowlist_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/group_model_allowlist_test.go) |
| M | test | 26 | 0 | 03 | [backend/internal/server/routes/composite_platform_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/routes/composite_platform_test.go) |
| M | test | 1 | 2 | 03 | [backend/internal/service/content_moderation_cyber_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/content_moderation_cyber_test.go) |
| M | test | 11 | 0 | 03 | [backend/internal/service/gemini_multiplatform_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gemini_multiplatform_test.go) |
| M | source | 298 | 4 | 03 | [backend/internal/service/grok_audio.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/grok_audio.go) |
| M | test | 151 | 0 | 03 | [backend/internal/service/grok_audio_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/grok_audio_test.go) |
| M | source | 53 | 20 | 03 | [backend/internal/service/image_storage.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/image_storage.go) |
| M | test | 53 | 7 | 03 | [backend/internal/service/image_storage_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/image_storage_test.go) |
| M | test | 11 | 0 | 03 | [backend/internal/service/openai_cyber_session_block_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_cyber_session_block_test.go) |
| M | source | 147 | 1 | 03,06 | [backend/internal/service/openai_gateway_response_handling.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_response_handling.go) |
| M | source | 95 | 16 | 03,06,07 | [backend/internal/service/openai_gateway_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_service.go) |
| M | test | 13 | 2 | 03 | [backend/internal/service/openai_gateway_service_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_service_test.go) |
| M | test | 6 | 2 | 03,12 | [backend/internal/service/openai_plugin_account_directory_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_plugin_account_directory_test.go) |
| M | test | 138 | 0 | 03,06 | [backend/internal/service/openai_responses_rejected_field_retry_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_responses_rejected_field_retry_test.go) |
| M | source | 11 | 0 | 03 | [backend/internal/service/openai_ws_execution_scope.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_execution_scope.go) |
| M | source | 1053 | 365 | 03,06,07,12 | [backend/internal/service/openai_ws_forwarder_ingress.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_forwarder_ingress.go) |
| M | test | 1045 | 46 | 03,06 | [backend/internal/service/openai_ws_forwarder_ingress_session_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_forwarder_ingress_session_test.go) |
| M | test | 291 | 1 | 03,06,07 | [backend/internal/service/openai_ws_forwarder_ingress_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_forwarder_ingress_test.go) |
| M | source | 13 | 3 | 03,06,12 | [backend/internal/service/openai_ws_forwarder_v2.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_forwarder_v2.go) |
| M | source | 17 | 7 | 03,06 | [backend/internal/service/openai_ws_pool.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_pool.go) |
| M | test | 61 | 0 | 03,06 | [backend/internal/service/openai_ws_state_store_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_state_store_test.go) |
| M | source | 201 | 25 | 03,06,07,12 | [backend/internal/service/openai_ws_v2_passthrough_adapter.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_v2_passthrough_adapter.go) |
| M | source | 1 | 0 | 03 | [backend/internal/service/setting_gateway_runtime.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_gateway_runtime.go) |
| A | source | 270 | 0 | 03,12 | [backend/internal/util/urlvalidator/public_transport.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/util/urlvalidator/public_transport.go) |
| A | test | 293 | 0 | 03 | [backend/internal/util/urlvalidator/public_transport_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/util/urlvalidator/public_transport_test.go) |
| A | source | 93 | 0 | 03 | [frontend/src/content/docs/en/apps/claude-code.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/apps/claude-code.md) |
| A | source | 30 | 0 | 03 | [frontend/src/content/docs/en/apps/claude-desktop.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/en/apps/claude-desktop.md) |
| A | source | 92 | 0 | 03 | [frontend/src/content/docs/ja/apps/claude-code.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/apps/claude-code.md) |
| A | source | 30 | 0 | 03 | [frontend/src/content/docs/ja/apps/claude-desktop.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/ja/apps/claude-desktop.md) |
| A | source | 93 | 0 | 03 | [frontend/src/content/docs/zh-TW/apps/claude-code.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/apps/claude-code.md) |
| A | source | 30 | 0 | 03 | [frontend/src/content/docs/zh-TW/apps/claude-desktop.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh-TW/apps/claude-desktop.md) |
| A | source | 93 | 0 | 03 | [frontend/src/content/docs/zh/apps/claude-code.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/apps/claude-code.md) |
| A | source | 30 | 0 | 03 | [frontend/src/content/docs/zh/apps/claude-desktop.md](https://github.com/holeenlu/sub2api/blob/main/frontend/src/content/docs/zh/apps/claude-desktop.md) |
