# 03 · 原生网关安全与入站失败

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。


2026-10-14 增量归并：本页已纳入通过验收的 New API 实现，来源和逐文件归属见 [归并分析](NEW-API-INTEGRATION-2026-10-14.md)。原快照 SHA 仅代表 2026-10-13 历史输入。

## 最终需求

1. 网关、Responses HTTP/WS、调度准入和原生并发以固定 Wei-Shaw 基线为主，不重新导入 ranxi/sub4api 的打票、Cookie 绑定、额外排队或独立模型目录治理。

2. 安全客户端 IP 取自可信代理链；转发元数据兼容开关不能改变 ACL、认证限流及会话 IP 的判定。

3. 图片转存保留解析后按公网 IP 连接、逐跳重定向复验、HTTPS 降级限制、文件体积与真实图片类型校验，复用已有公共出口机制。

4. WebSocket 首轮、续轮和透传沿用 Claude/Codex 客户端资格及模型检查；响应引用按已认证用户归属验证，同用户的 Key 可以共享其获准响应，跨用户不允许。连接池兼容键包含已认证租户隔离信息。

5. Grok 语音 HTTP 与实时连接复用原生用户并发；音色列表、增改删、TTS 和 Realtime 引用检查用户/分组/上游账号归属。保留现有账单行为，不新增异步媒体任务系统。

## 入口与配置归属

原有 /v1 网关、Responses WebSocket、Grok 音频入口；没有因本次整理新增公开 API。

## 最终实现方案

1. 继续用原生鉴权、调度、并发与公共出口，不新增准入服务。可信代理链决定安全客户端 IP。

2. 图片逐跳复验公网出口、禁止 HTTPS 降级；WS response owner 和连接兼容键按认证租户隔离。

3. Grok 语音占用原用户并发，音色 CRUD/引用在既有仓储校验归属；BPS 的恢复例外见 07。

## 边界、失败路径与依赖

关联功能组：06, 07, 08。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。此组与关联组共同形成完整运行行为；本次归并不额外改写已验收的业务源码。

## 验收要求

可信代理、图片出口、WS tenant/owner、Grok 音色与用户并发已有回归。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 8 | 491 | 0 | 0 |
| source | 20 | 2744 | 575 | 0 |
| test | 14 | 2035 | 57 | 0 |

相对官方 v0.2.16 的累计差异，已包含本批实现；不是本次归并额外编写的代码。

## 来源提交

- `49b29ef158e6319cfd6b1227781307de5f8f870e`
- `4b2cfde513867c40dbcc663a5ddea30cdd286dd2`
- `4c5aa7b49988f5752b01940475530832c188b7ad`
- `71fc12f8edf4cbb839a72ba5f06c86745a1f2612`
- `944fdb1eff8e7ea33ed70ff722b6822f962e9255`
- `a47851536b749381397de8bb8c10e2812928f08d`
- `c6a92e1501a3ed5bb405c02bd541fcb59366bd28`
- `e37f32177a2b3f31414e8e2801d1f20678983fe5`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 45 | 8 | 03,06,08 | [backend/internal/handler/gateway_handler.go](../../../backend/internal/handler/gateway_handler.go) |
| M | source | 10 | 2 | 03,06,08 | [backend/internal/handler/gateway_handler_chat_completions.go](../../../backend/internal/handler/gateway_handler_chat_completions.go) |
| M | source | 7 | 1 | 03,08 | [backend/internal/handler/gateway_handler_responses.go](../../../backend/internal/handler/gateway_handler_responses.go) |
| M | source | 7 | 5 | 03,06 | [backend/internal/handler/gemini_v1beta_handler.go](../../../backend/internal/handler/gemini_v1beta_handler.go) |
| M | source | 152 | 34 | 03 | [backend/internal/handler/grok_audio.go](../../../backend/internal/handler/grok_audio.go) |
| M | test | 44 | 0 | 03 | [backend/internal/handler/grok_audio_billing_test.go](../../../backend/internal/handler/grok_audio_billing_test.go) |
| M | source | 429 | 90 | 03,06,07 | [backend/internal/handler/openai_gateway_handler.go](../../../backend/internal/handler/openai_gateway_handler.go) |
| M | test | 6 | 9 | 03 | [backend/internal/handler/openai_gateway_ws_model_allowlist_test.go](../../../backend/internal/handler/openai_gateway_ws_model_allowlist_test.go) |
| M | source | 4 | 11 | 03 | [backend/internal/pkg/ip/ip.go](../../../backend/internal/pkg/ip/ip.go) |
| M | test | 7 | 7 | 03 | [backend/internal/pkg/ip/ip_test.go](../../../backend/internal/pkg/ip/ip_test.go) |
| A | source | 48 | 0 | 03 | [backend/internal/repository/model_policy_preflight.go](../../../backend/internal/repository/model_policy_preflight.go) |
| M | source | 16 | 6 | 03 | [backend/internal/server/middleware/audit_log.go](../../../backend/internal/server/middleware/audit_log.go) |
| M | test | 11 | 0 | 03 | [backend/internal/service/gemini_multiplatform_test.go](../../../backend/internal/service/gemini_multiplatform_test.go) |
| M | source | 298 | 4 | 03 | [backend/internal/service/grok_audio.go](../../../backend/internal/service/grok_audio.go) |
| M | test | 151 | 0 | 03 | [backend/internal/service/grok_audio_test.go](../../../backend/internal/service/grok_audio_test.go) |
| M | source | 53 | 20 | 03 | [backend/internal/service/image_storage.go](../../../backend/internal/service/image_storage.go) |
| M | test | 53 | 7 | 03 | [backend/internal/service/image_storage_test.go](../../../backend/internal/service/image_storage_test.go) |
| M | test | 11 | 0 | 03 | [backend/internal/service/openai_cyber_session_block_test.go](../../../backend/internal/service/openai_cyber_session_block_test.go) |
| M | source | 144 | 1 | 03,06 | [backend/internal/service/openai_gateway_response_handling.go](../../../backend/internal/service/openai_gateway_response_handling.go) |
| M | source | 95 | 16 | 03,06,07 | [backend/internal/service/openai_gateway_service.go](../../../backend/internal/service/openai_gateway_service.go) |
| M | test | 11 | 0 | 03 | [backend/internal/service/openai_gateway_service_test.go](../../../backend/internal/service/openai_gateway_service_test.go) |
| M | test | 1 | 1 | 03 | [backend/internal/service/openai_plugin_account_directory_test.go](../../../backend/internal/service/openai_plugin_account_directory_test.go) |
| M | test | 138 | 0 | 03,06 | [backend/internal/service/openai_responses_rejected_field_retry_test.go](../../../backend/internal/service/openai_responses_rejected_field_retry_test.go) |
| M | source | 11 | 0 | 03 | [backend/internal/service/openai_ws_execution_scope.go](../../../backend/internal/service/openai_ws_execution_scope.go) |
| M | source | 999 | 362 | 03,06,07 | [backend/internal/service/openai_ws_forwarder_ingress.go](../../../backend/internal/service/openai_ws_forwarder_ingress.go) |
| M | test | 957 | 32 | 03,06 | [backend/internal/service/openai_ws_forwarder_ingress_session_test.go](../../../backend/internal/service/openai_ws_forwarder_ingress_session_test.go) |
| M | test | 291 | 1 | 03,06,07 | [backend/internal/service/openai_ws_forwarder_ingress_test.go](../../../backend/internal/service/openai_ws_forwarder_ingress_test.go) |
| M | source | 7 | 3 | 03,06 | [backend/internal/service/openai_ws_forwarder_v2.go](../../../backend/internal/service/openai_ws_forwarder_v2.go) |
| M | source | 17 | 7 | 03,06 | [backend/internal/service/openai_ws_pool.go](../../../backend/internal/service/openai_ws_pool.go) |
| M | test | 61 | 0 | 03,06 | [backend/internal/service/openai_ws_state_store_test.go](../../../backend/internal/service/openai_ws_state_store_test.go) |
| M | source | 94 | 5 | 03,06,07 | [backend/internal/service/openai_ws_v2_passthrough_adapter.go](../../../backend/internal/service/openai_ws_v2_passthrough_adapter.go) |
| M | source | 1 | 0 | 03 | [backend/internal/service/setting_gateway_runtime.go](../../../backend/internal/service/setting_gateway_runtime.go) |
| A | source | 307 | 0 | 03 | [backend/internal/util/urlvalidator/public_transport.go](../../../backend/internal/util/urlvalidator/public_transport.go) |
| A | test | 293 | 0 | 03 | [backend/internal/util/urlvalidator/public_transport_test.go](../../../backend/internal/util/urlvalidator/public_transport_test.go) |
| A | documentation | 93 | 0 | 03 | [frontend/src/content/docs/en/apps/claude-code.md](../../../frontend/src/content/docs/en/apps/claude-code.md) |
| A | documentation | 30 | 0 | 03 | [frontend/src/content/docs/en/apps/claude-desktop.md](../../../frontend/src/content/docs/en/apps/claude-desktop.md) |
| A | documentation | 92 | 0 | 03 | [frontend/src/content/docs/ja/apps/claude-code.md](../../../frontend/src/content/docs/ja/apps/claude-code.md) |
| A | documentation | 30 | 0 | 03 | [frontend/src/content/docs/ja/apps/claude-desktop.md](../../../frontend/src/content/docs/ja/apps/claude-desktop.md) |
| A | documentation | 93 | 0 | 03 | [frontend/src/content/docs/zh-TW/apps/claude-code.md](../../../frontend/src/content/docs/zh-TW/apps/claude-code.md) |
| A | documentation | 30 | 0 | 03 | [frontend/src/content/docs/zh-TW/apps/claude-desktop.md](../../../frontend/src/content/docs/zh-TW/apps/claude-desktop.md) |
| A | documentation | 93 | 0 | 03 | [frontend/src/content/docs/zh/apps/claude-code.md](../../../frontend/src/content/docs/zh/apps/claude-code.md) |
| A | documentation | 30 | 0 | 03 | [frontend/src/content/docs/zh/apps/claude-desktop.md](../../../frontend/src/content/docs/zh/apps/claude-desktop.md) |

## 2026-10-14 新增需求与实施边界

保留首次读取的 typed error 和已读前缀，保持 Close 所有权与捕获上限；各原生入口正确记录入站失败和 413，不尝试上游、不处罚账号。

## 关联实现（由其他组提交）

同一共享文件保持单一主提交，跨组依赖随完整提交集验收。

| 主归属组 | 类别 | 文件 |
| --- | --- | --- |
| 02 | test | [backend/internal/server/middleware/audit_log_test.go](../../../backend/internal/server/middleware/audit_log_test.go) |
