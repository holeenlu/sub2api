# 03 · 原生网关与多媒体安全

归档日期：2026-10-07。文件快照：main `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238`；官方比较基线 `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`。

本页登记最终需求与实际文件归属；不把本轮提交整理等同于重新实现或完整安全审计。

## 最终需求

1. 网关、Responses HTTP/WS、调度准入和原生并发以固定 Wei-Shaw 基线为主，不重新导入 ranxi/sub4api 的打票、BPS、Cookie 绑定、额外排队或独立模型目录治理。

2. 安全客户端 IP 取自可信代理链；转发元数据兼容开关不能改变 ACL、认证限流及会话 IP 的判定。

3. 图片转存保留解析后按公网 IP 连接、逐跳重定向复验、HTTPS 降级限制、文件体积与真实图片类型校验，复用已有公共出口机制。

4. WebSocket 首轮、续轮和透传沿用 Claude/Codex 客户端资格及模型检查；响应引用按已认证用户归属验证，同用户的 Key 可以共享其获准响应，跨用户不允许。连接池兼容键包含已认证租户隔离信息。

5. Grok 语音 HTTP 与实时连接复用原生用户并发；音色列表、增改删、TTS 和 Realtime 引用检查用户/分组/上游账号归属。保留现有账单行为，不新增异步媒体任务系统。

## 入口与配置归属

原有 /v1 网关、Responses WebSocket、Grok 音频入口；没有因本次整理新增公开 API。

## 验收边界

代码与当前已交付快照一致；校验重点为伪造 IP、私网跳转、跨用户响应引用、WS 不合格客户端、Grok 音色越权。仅建表的 gateway_media_jobs 不列为已实现功能。

验证状态统一见 [总说明](README.md#验证和未关闭边界)。本页列的是验收要求，不是宣称本轮已重新运行这些检查。

## 对比统计

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 业务代码 | 19 | 1121 | 88 | 0 |
| 测试/夹具 | 14 | 921 | 34 | 0 |
| 已有文档/许可 | 6 | 368 | 0 | 0 |
| 生成代码/繁体 | 2 | 123 | 0 | 0 |

统计为最终文件相对固定官方基线的累计差异，并非本次整理新写的代码。对重命名统一展开为删除/新增；跨域文件只设一个主归属。

## 修改文件

| 状态 | 类型 | +行 | −行 | 文件 |
| --- | --- | ---: | ---: | --- |
| M | 业务代码 | 23 | 3 | [backend/internal/handler/gateway_handler.go](../../../backend/internal/handler/gateway_handler.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/handler/gateway_handler_chat_completions.go](../../../backend/internal/handler/gateway_handler_chat_completions.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/handler/gateway_handler_responses.go](../../../backend/internal/handler/gateway_handler_responses.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/handler/gemini_v1beta_handler.go](../../../backend/internal/handler/gemini_v1beta_handler.go) |
| M | 业务代码 | 152 | 34 | [backend/internal/handler/grok_audio.go](../../../backend/internal/handler/grok_audio.go) |
| M | 测试/夹具 | 44 | 0 | [backend/internal/handler/grok_audio_billing_test.go](../../../backend/internal/handler/grok_audio_billing_test.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/handler/openai_gateway_handler.go](../../../backend/internal/handler/openai_gateway_handler.go) |
| M | 测试/夹具 | 6 | 9 | [backend/internal/handler/openai_gateway_ws_model_allowlist_test.go](../../../backend/internal/handler/openai_gateway_ws_model_allowlist_test.go) |
| M | 业务代码 | 4 | 11 | [backend/internal/pkg/ip/ip.go](../../../backend/internal/pkg/ip/ip.go) |
| M | 测试/夹具 | 7 | 7 | [backend/internal/pkg/ip/ip_test.go](../../../backend/internal/pkg/ip/ip_test.go) |
| A | 业务代码 | 48 | 0 | [backend/internal/repository/model_policy_preflight.go](../../../backend/internal/repository/model_policy_preflight.go) |
| M | 测试/夹具 | 11 | 0 | [backend/internal/service/gemini_multiplatform_test.go](../../../backend/internal/service/gemini_multiplatform_test.go) |
| M | 业务代码 | 298 | 4 | [backend/internal/service/grok_audio.go](../../../backend/internal/service/grok_audio.go) |
| M | 测试/夹具 | 151 | 0 | [backend/internal/service/grok_audio_test.go](../../../backend/internal/service/grok_audio_test.go) |
| M | 业务代码 | 53 | 20 | [backend/internal/service/image_storage.go](../../../backend/internal/service/image_storage.go) |
| M | 测试/夹具 | 53 | 7 | [backend/internal/service/image_storage_test.go](../../../backend/internal/service/image_storage_test.go) |
| M | 测试/夹具 | 11 | 0 | [backend/internal/service/openai_cyber_session_block_test.go](../../../backend/internal/service/openai_cyber_session_block_test.go) |
| M | 业务代码 | 120 | 0 | [backend/internal/service/openai_gateway_response_handling.go](../../../backend/internal/service/openai_gateway_response_handling.go) |
| M | 业务代码 | 40 | 0 | [backend/internal/service/openai_gateway_service.go](../../../backend/internal/service/openai_gateway_service.go) |
| M | 测试/夹具 | 11 | 0 | [backend/internal/service/openai_gateway_service_test.go](../../../backend/internal/service/openai_gateway_service_test.go) |
| M | 测试/夹具 | 1 | 1 | [backend/internal/service/openai_plugin_account_directory_test.go](../../../backend/internal/service/openai_plugin_account_directory_test.go) |
| M | 测试/夹具 | 5 | 0 | [backend/internal/service/openai_responses_rejected_field_retry_test.go](../../../backend/internal/service/openai_responses_rejected_field_retry_test.go) |
| M | 业务代码 | 11 | 0 | [backend/internal/service/openai_ws_execution_scope.go](../../../backend/internal/service/openai_ws_execution_scope.go) |
| M | 业务代码 | 27 | 3 | [backend/internal/service/openai_ws_forwarder_ingress.go](../../../backend/internal/service/openai_ws_forwarder_ingress.go) |
| M | 测试/夹具 | 12 | 10 | [backend/internal/service/openai_ws_forwarder_ingress_session_test.go](../../../backend/internal/service/openai_ws_forwarder_ingress_session_test.go) |
| M | 测试/夹具 | 283 | 0 | [backend/internal/service/openai_ws_forwarder_ingress_test.go](../../../backend/internal/service/openai_ws_forwarder_ingress_test.go) |
| M | 业务代码 | 4 | 3 | [backend/internal/service/openai_ws_forwarder_v2.go](../../../backend/internal/service/openai_ws_forwarder_v2.go) |
| M | 业务代码 | 14 | 6 | [backend/internal/service/openai_ws_pool.go](../../../backend/internal/service/openai_ws_pool.go) |
| M | 测试/夹具 | 33 | 0 | [backend/internal/service/openai_ws_state_store_test.go](../../../backend/internal/service/openai_ws_state_store_test.go) |
| M | 业务代码 | 15 | 0 | [backend/internal/service/openai_ws_v2_passthrough_adapter.go](../../../backend/internal/service/openai_ws_v2_passthrough_adapter.go) |
| M | 业务代码 | 1 | 0 | [backend/internal/service/setting_gateway_runtime.go](../../../backend/internal/service/setting_gateway_runtime.go) |
| A | 业务代码 | 307 | 0 | [backend/internal/util/urlvalidator/public_transport.go](../../../backend/internal/util/urlvalidator/public_transport.go) |
| A | 测试/夹具 | 293 | 0 | [backend/internal/util/urlvalidator/public_transport_test.go](../../../backend/internal/util/urlvalidator/public_transport_test.go) |
| A | 已有文档/许可 | 93 | 0 | [frontend/src/content/docs/en/apps/claude-code.md](../../../frontend/src/content/docs/en/apps/claude-code.md) |
| A | 已有文档/许可 | 30 | 0 | [frontend/src/content/docs/en/apps/claude-desktop.md](../../../frontend/src/content/docs/en/apps/claude-desktop.md) |
| A | 已有文档/许可 | 92 | 0 | [frontend/src/content/docs/ja/apps/claude-code.md](../../../frontend/src/content/docs/ja/apps/claude-code.md) |
| A | 已有文档/许可 | 30 | 0 | [frontend/src/content/docs/ja/apps/claude-desktop.md](../../../frontend/src/content/docs/ja/apps/claude-desktop.md) |
| A | 生成代码/繁体 | 93 | 0 | [frontend/src/content/docs/zh-TW/apps/claude-code.md](../../../frontend/src/content/docs/zh-TW/apps/claude-code.md) |
| A | 生成代码/繁体 | 30 | 0 | [frontend/src/content/docs/zh-TW/apps/claude-desktop.md](../../../frontend/src/content/docs/zh-TW/apps/claude-desktop.md) |
| A | 已有文档/许可 | 93 | 0 | [frontend/src/content/docs/zh/apps/claude-code.md](../../../frontend/src/content/docs/zh/apps/claude-code.md) |
| A | 已有文档/许可 | 30 | 0 | [frontend/src/content/docs/zh/apps/claude-desktop.md](../../../frontend/src/content/docs/zh/apps/claude-desktop.md) |
