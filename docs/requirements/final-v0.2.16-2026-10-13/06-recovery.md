# 06 · 网关恢复、流终止、失败用量与传输诊断

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。


2026-10-14 增量归并：本页已纳入通过验收的 New API 实现，来源和逐文件归属见 [归并分析](NEW-API-INTEGRATION-2026-10-14.md)。原快照 SHA 仅代表 2026-10-13 历史输入。

## 最终需求

1. HTTP/SSE、原生 WS、HTTP bridge 和 passthrough 使用一致的当前轮生命周期、上下文归属、恢复证据与终态优先级，不能因错误丢失本轮历史和真实用量。
2. 保留 reasoning 密文、compaction、成对 call_id 和完整工具历史。删除旧的删密文重试、盲目删除 previous_response_id、跨模式剥离逻辑。只有明确持久化 item 拒绝且输入自包含时才允许有界修复。
3. 区分同 owner 完整重放与跨账号可携带性。私有文件、vector store、container、未解决的持久化状态不跨账号；只读搜索可按既有规则重试，执行型或不透明工具上下文禁止自动重放。输出已提交后禁止重放。
4. 暂存响应身份和元数据，遵守 8 MiB 与身份建立后一秒边界；安全 EOF/1012/连接容量失败不能泄露被舍弃的 response ID。原生 lease 55 分钟轮换时保持合法存储锚点。
5. 一个客户端 reader、串行 writer、有界队列；取消固定到当前轮和当前凭据，无响应身份的拒绝返回请求级错误。错误后后续轮可继续，失败尝试不进入重放历史。WS bridge 只支持单 lane，不把它描述为多路复用。
6. 后续轮重查 Key、账号、分组、模型及有效出口配置。凭据/代理/端点/模式变化只在上下文足够时重建上游；计费用户、账号、模型和请求快照冻结。
7. 尝试预算和首个语义输出预算贯穿准入、调度、退避、响应头和正文；元数据与 keepalive 不重置预算。取消、未发送耗尽不处罚未收到请求的账号。
8. 保留已观察部分用量，取消后的 drain 有界；不估算缺失用量，不重复计费废弃尝试。Ops 按轮确认恢复结果，保留上游状态与客户端状态差异。
9. 只记录有界且脱敏的传输、时序、上下文、工具阻断和预算证据，不记录提示词、密文或输出正文。仅有已证明未发送且 body 可回绕时重试代理 TLS 握手；H2 故障反馈保持有界有效期。
10. Anthropic 在 message_start 或 keepalive 后的 overloaded_error 按真实根因分类，不把 SSE HTTP 200 视为生成成功。补充保留 OAuth web_search 历史修复与新上游的 Responses fallback 语义。

## 入口与配置归属

沿用下列方案和完整文件清单中的现有入口。

## 最终实现方案

1. 在现有 forwarding/WS owner、恢复策略、context lineage、usage 和 Ops 路径收敛规则；不创建平行网关。

2. 正常轮次：校验身份与资格 → 固定尝试快照 → 预算内发送 → 暂存元数据 → 语义输出提交 → 终态/用量收敛。

3. 失败轮次：先判定发送/输出/归属/工具副作用证据，再决定同 owner 或可携带重选；不满足证据即返回完整错误并等待客户端完整历史。

4. 断连、取消与迟到终态保持单 reader 和串行写，不把失败尝试当作下一轮上下文。

## 边界、失败路径与依赖

没有额外文件归属组关联。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。此组与关联组共同形成完整运行行为；本次归并不额外改写已验收的业务源码。

## 验收要求

HTTP/WS recovery、lineage、终态、取消、部分用量和传输诊断；有界重放/首输出预算相关 unit 与 race。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| source | 43 | 1895 | 793 | 0 |
| test | 46 | 2318 | 1384 | 0 |

相对官方 v0.2.16 的累计差异，已包含本批实现；不是本次归并额外编写的代码。

## 来源提交

- `49b29ef158e6319cfd6b1227781307de5f8f870e`
- `4c5aa7b49988f5752b01940475530832c188b7ad`
- `6dc1165380ed5f93a5f63729eb5c3b8421d61126`
- `944fdb1eff8e7ea33ed70ff722b6822f962e9255`
- `c6a92e1501a3ed5bb405c02bd541fcb59366bd28`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 5 | 0 | 06 | [backend/internal/handler/concurrency_error_response.go](../../../backend/internal/handler/concurrency_error_response.go) |
| M | source | 12 | 3 | 06 | [backend/internal/handler/failover_loop.go](../../../backend/internal/handler/failover_loop.go) |
| M | test | 38 | 0 | 06 | [backend/internal/handler/gateway_handler_stream_failover_test.go](../../../backend/internal/handler/gateway_handler_stream_failover_test.go) |
| M | source | 25 | 1 | 06 | [backend/internal/handler/gateway_helper.go](../../../backend/internal/handler/gateway_helper.go) |
| M | test | 2 | 2 | 06 | [backend/internal/handler/openai_gateway_first_output_timeout_test.go](../../../backend/internal/handler/openai_gateway_first_output_timeout_test.go) |
| D | source | 0 | 62 | 06 | [backend/internal/handler/openai_gateway_reasoning_failover.go](../../../backend/internal/handler/openai_gateway_reasoning_failover.go) |
| D | test | 0 | 146 | 06 | [backend/internal/handler/openai_gateway_reasoning_failover_test.go](../../../backend/internal/handler/openai_gateway_reasoning_failover_test.go) |
| M | test | 0 | 28 | 06 | [backend/internal/handler/openai_responses_astra_pro_failover_test.go](../../../backend/internal/handler/openai_responses_astra_pro_failover_test.go) |
| M | test | 99 | 1 | 06 | [backend/internal/handler/openai_responses_failover_cancel_test.go](../../../backend/internal/handler/openai_responses_failover_cancel_test.go) |
| M | test | 5 | 0 | 06 | [backend/internal/handler/openai_ws_v2_passthrough_cyber_test.go](../../../backend/internal/handler/openai_ws_v2_passthrough_cyber_test.go) |
| M | source | 31 | 32 | 06 | [backend/internal/handler/ops_error_logger.go](../../../backend/internal/handler/ops_error_logger.go) |
| M | test | 93 | 2 | 06 | [backend/internal/handler/ops_error_logger_test.go](../../../backend/internal/handler/ops_error_logger_test.go) |
| M | source | 1 | 1 | 06 | [backend/internal/handler/security_audit_errors.go](../../../backend/internal/handler/security_audit_errors.go) |
| M | source | 5 | 0 | 06 | [backend/internal/handler/stream_error_event.go](../../../backend/internal/handler/stream_error_event.go) |
| M | source | 3 | 0 | 06 | [backend/internal/pkg/antigravity/gemini_types.go](../../../backend/internal/pkg/antigravity/gemini_types.go) |
| M | source | 6 | 1 | 06 | [backend/internal/pkg/antigravity/response_transformer.go](../../../backend/internal/pkg/antigravity/response_transformer.go) |
| M | source | 20 | 3 | 06 | [backend/internal/pkg/antigravity/stream_transformer.go](../../../backend/internal/pkg/antigravity/stream_transformer.go) |
| M | source | 16 | 2 | 06 | [backend/internal/pkg/logger/logger.go](../../../backend/internal/pkg/logger/logger.go) |
| M | test | 62 | 0 | 06 | [backend/internal/pkg/logger/stdlog_bridge_test.go](../../../backend/internal/pkg/logger/stdlog_bridge_test.go) |
| A | source | 94 | 0 | 06 | [backend/internal/repository/http_upstream_pre_request_retry.go](../../../backend/internal/repository/http_upstream_pre_request_retry.go) |
| A | test | 312 | 0 | 06 | [backend/internal/repository/http_upstream_pre_request_retry_test.go](../../../backend/internal/repository/http_upstream_pre_request_retry_test.go) |
| M | source | 16 | 2 | 06 | [backend/internal/repository/ops_repo.go](../../../backend/internal/repository/ops_repo.go) |
| A | test | 64 | 0 | 06 | [backend/internal/repository/ops_repo_recovery_test.go](../../../backend/internal/repository/ops_repo_recovery_test.go) |
| M | source | 31 | 0 | 06 | [backend/internal/server/middleware/api_key_auth.go](../../../backend/internal/server/middleware/api_key_auth.go) |
| M | test | 31 | 0 | 06 | [backend/internal/server/middleware/api_key_auth_test.go](../../../backend/internal/server/middleware/api_key_auth_test.go) |
| M | source | 14 | 2 | 06 | [backend/internal/service/antigravity_gateway_claude.go](../../../backend/internal/service/antigravity_gateway_claude.go) |
| M | source | 35 | 10 | 06 | [backend/internal/service/antigravity_gateway_compat_stream.go](../../../backend/internal/service/antigravity_gateway_compat_stream.go) |
| M | source | 15 | 2 | 06 | [backend/internal/service/antigravity_gateway_gemini.go](../../../backend/internal/service/antigravity_gateway_gemini.go) |
| M | test | 30 | 3 | 06 | [backend/internal/service/antigravity_gateway_service_test.go](../../../backend/internal/service/antigravity_gateway_service_test.go) |
| M | source | 50 | 23 | 06 | [backend/internal/service/antigravity_gateway_streaming.go](../../../backend/internal/service/antigravity_gateway_streaming.go) |
| M | source | 1 | 1 | 06 | [backend/internal/service/antigravity_gateway_upstream.go](../../../backend/internal/service/antigravity_gateway_upstream.go) |
| A | test | 114 | 0 | 06 | [backend/internal/service/gateway_body_resource_bench_test.go](../../../backend/internal/service/gateway_body_resource_bench_test.go) |
| M | test | 51 | 36 | 06 | [backend/internal/service/gateway_forward_partial_usage_test.go](../../../backend/internal/service/gateway_forward_partial_usage_test.go) |
| M | test | 7 | 8 | 06 | [backend/internal/service/gemini_native_response_signal_test.go](../../../backend/internal/service/gemini_native_response_signal_test.go) |
| M | test | 2 | 0 | 06 | [backend/internal/service/gemini_sse_comment_compat_test.go](../../../backend/internal/service/gemini_sse_comment_compat_test.go) |
| A | source | 223 | 0 | 06 | [backend/internal/service/gemini_stream_outcome.go](../../../backend/internal/service/gemini_stream_outcome.go) |
| M | test | 39 | 1 | 06 | [backend/internal/service/openai_capacity_shed_test.go](../../../backend/internal/service/openai_capacity_shed_test.go) |
| M | test | 5 | 3 | 06 | [backend/internal/service/openai_compact_model_mapping_test.go](../../../backend/internal/service/openai_compact_model_mapping_test.go) |
| D | source | 0 | 353 | 06 | [backend/internal/service/openai_encrypted_content_lineage.go](../../../backend/internal/service/openai_encrypted_content_lineage.go) |
| D | test | 0 | 216 | 06 | [backend/internal/service/openai_encrypted_content_lineage_test.go](../../../backend/internal/service/openai_encrypted_content_lineage_test.go) |
| M | source | 169 | 3 | 06 | [backend/internal/service/openai_first_output_timeout.go](../../../backend/internal/service/openai_first_output_timeout.go) |
| M | test | 156 | 0 | 06 | [backend/internal/service/openai_first_output_timeout_test.go](../../../backend/internal/service/openai_first_output_timeout_test.go) |
| M | source | 125 | 10 | 06 | [backend/internal/service/openai_gateway_passthrough.go](../../../backend/internal/service/openai_gateway_passthrough.go) |
| M | source | 4 | 118 | 06 | [backend/internal/service/openai_gateway_request_body.go](../../../backend/internal/service/openai_gateway_request_body.go) |
| M | test | 0 | 135 | 06 | [backend/internal/service/openai_gateway_service_hotpath_test.go](../../../backend/internal/service/openai_gateway_service_hotpath_test.go) |
| M | source | 45 | 0 | 06 | [backend/internal/service/openai_gateway_upstream_errors.go](../../../backend/internal/service/openai_gateway_upstream_errors.go) |
| M | test | 36 | 0 | 06 | [backend/internal/service/openai_nonstreaming_terminal_failure_failover_test.go](../../../backend/internal/service/openai_nonstreaming_terminal_failure_failover_test.go) |
| M | test | 8 | 10 | 06 | [backend/internal/service/openai_oauth_passthrough_test.go](../../../backend/internal/service/openai_oauth_passthrough_test.go) |
| M | source | 30 | 10 | 06 | [backend/internal/service/openai_oauth_web_search_history.go](../../../backend/internal/service/openai_oauth_web_search_history.go) |
| M | test | 34 | 6 | 06 | [backend/internal/service/openai_oauth_web_search_history_forward_test.go](../../../backend/internal/service/openai_oauth_web_search_history_forward_test.go) |
| M | test | 22 | 2 | 06 | [backend/internal/service/openai_oauth_web_search_history_test.go](../../../backend/internal/service/openai_oauth_web_search_history_test.go) |
| M | test | 21 | 13 | 06 | [backend/internal/service/openai_passthrough_normalization_test.go](../../../backend/internal/service/openai_passthrough_normalization_test.go) |
| M | source | 5 | 0 | 06 | [backend/internal/service/openai_plugin_transport.go](../../../backend/internal/service/openai_plugin_transport.go) |
| A | test | 56 | 0 | 06 | [backend/internal/service/openai_replay_policy_test.go](../../../backend/internal/service/openai_replay_policy_test.go) |
| M | source | 76 | 0 | 06 | [backend/internal/service/openai_responses_item_id.go](../../../backend/internal/service/openai_responses_item_id.go) |
| M | source | 49 | 1 | 06 | [backend/internal/service/openai_responses_rejected_field_retry.go](../../../backend/internal/service/openai_responses_rejected_field_retry.go) |
| M | source | 20 | 2 | 06 | [backend/internal/service/openai_upstream_transport_error.go](../../../backend/internal/service/openai_upstream_transport_error.go) |
| M | test | 2 | 0 | 06 | [backend/internal/service/openai_visible_ttft_test.go](../../../backend/internal/service/openai_visible_ttft_test.go) |
| M | source | 4 | 2 | 06 | [backend/internal/service/openai_ws_client.go](../../../backend/internal/service/openai_ws_client.go) |
| M | source | 363 | 2 | 06 | [backend/internal/service/openai_ws_client_read.go](../../../backend/internal/service/openai_ws_client_read.go) |
| M | test | 105 | 0 | 06 | [backend/internal/service/openai_ws_client_read_test.go](../../../backend/internal/service/openai_ws_client_read_test.go) |
| M | test | 12 | 0 | 06 | [backend/internal/service/openai_ws_client_test.go](../../../backend/internal/service/openai_ws_client_test.go) |
| M | source | 11 | 5 | 06 | [backend/internal/service/openai_ws_forwarder_logutil.go](../../../backend/internal/service/openai_ws_forwarder_logutil.go) |
| M | source | 262 | 1 | 06 | [backend/internal/service/openai_ws_forwarder_payload.go](../../../backend/internal/service/openai_ws_forwarder_payload.go) |
| M | test | 99 | 0 | 06 | [backend/internal/service/openai_ws_forwarder_retry_payload_test.go](../../../backend/internal/service/openai_ws_forwarder_retry_payload_test.go) |
| M | source | 50 | 0 | 06 | [backend/internal/service/openai_ws_forwarder_support.go](../../../backend/internal/service/openai_ws_forwarder_support.go) |
| M | test | 1 | 0 | 06 | [backend/internal/service/openai_ws_forwarder_test.go](../../../backend/internal/service/openai_ws_forwarder_test.go) |
| M | test | 3 | 4 | 06 | [backend/internal/service/openai_ws_http_bridge_resume_test.go](../../../backend/internal/service/openai_ws_http_bridge_resume_test.go) |
| M | test | 394 | 4 | 06 | [backend/internal/service/openai_ws_http_bridge_test.go](../../../backend/internal/service/openai_ws_http_bridge_test.go) |
| M | test | 68 | 3 | 06 | [backend/internal/service/openai_ws_ingress_capacity_shed_test.go](../../../backend/internal/service/openai_ws_ingress_capacity_shed_test.go) |
| M | test | 31 | 0 | 06 | [backend/internal/service/openai_ws_ops_failure_test.go](../../../backend/internal/service/openai_ws_ops_failure_test.go) |
| M | test | 40 | 751 | 06 | [backend/internal/service/openai_ws_protocol_forward_test.go](../../../backend/internal/service/openai_ws_protocol_forward_test.go) |
| A | test | 135 | 0 | 06 | [backend/internal/service/openai_ws_recovery_contract_test.go](../../../backend/internal/service/openai_ws_recovery_contract_test.go) |
| M | source | 21 | 129 | 06 | [backend/internal/service/openai_ws_state_store.go](../../../backend/internal/service/openai_ws_state_store.go) |
| M | source | 13 | 1 | 06 | [backend/internal/service/openai_ws_v2/passthrough_relay.go](../../../backend/internal/service/openai_ws_v2/passthrough_relay.go) |
| M | test | 20 | 8 | 06 | [backend/internal/service/openai_ws_v2/passthrough_relay_test.go](../../../backend/internal/service/openai_ws_v2/passthrough_relay_test.go) |
| M | test | 24 | 0 | 06 | [backend/internal/service/openai_ws_v2_passthrough_lifecycle_test.go](../../../backend/internal/service/openai_ws_v2_passthrough_lifecycle_test.go) |
| M | source | 6 | 3 | 06 | [backend/internal/service/ops_models.go](../../../backend/internal/service/ops_models.go) |
| M | source | 10 | 1 | 06 | [backend/internal/service/ops_service.go](../../../backend/internal/service/ops_service.go) |
| M | test | 13 | 0 | 06 | [backend/internal/service/ops_service_batch_test.go](../../../backend/internal/service/ops_service_batch_test.go) |
| M | test | 38 | 0 | 06 | [backend/internal/service/ops_service_redaction_test.go](../../../backend/internal/service/ops_service_redaction_test.go) |
| M | source | 1 | 0 | 06 | [frontend/src/api/admin/ops.ts](../../../frontend/src/api/admin/ops.ts) |
| M | source | 16 | 5 | 06 | [frontend/src/views/admin/ops/components/OpsErrorDetailModal.vue](../../../frontend/src/views/admin/ops/components/OpsErrorDetailModal.vue) |
| M | source | 5 | 1 | 06 | [frontend/src/views/admin/ops/components/OpsErrorLogTable.vue](../../../frontend/src/views/admin/ops/components/OpsErrorLogTable.vue) |
| M | test | 5 | 1 | 06 | [frontend/src/views/admin/ops/components/__tests__/OpsAlertEventsCard.pagination.spec.ts](../../../frontend/src/views/admin/ops/components/__tests__/OpsAlertEventsCard.pagination.spec.ts) |
| M | test | 19 | 0 | 06 | [frontend/src/views/admin/ops/components/__tests__/OpsErrorDetailModal.spec.ts](../../../frontend/src/views/admin/ops/components/__tests__/OpsErrorDetailModal.spec.ts) |
| M | test | 9 | 0 | 06 | [frontend/src/views/admin/ops/components/__tests__/OpsErrorLogTable.spec.ts](../../../frontend/src/views/admin/ops/components/__tests__/OpsErrorLogTable.spec.ts) |
| M | test | 13 | 1 | 06 | [frontend/src/views/admin/ops/utils/__tests__/opsFormatters.spec.ts](../../../frontend/src/views/admin/ops/utils/__tests__/opsFormatters.spec.ts) |
| M | source | 7 | 1 | 06 | [frontend/src/views/admin/ops/utils/opsFormatters.ts](../../../frontend/src/views/admin/ops/utils/opsFormatters.ts) |

## 关联实现（由其他组提交）

同一共享文件保持单一主提交，跨组依赖随完整提交集验收。

| 主归属组 | 类别 | 文件 |
| --- | --- | --- |
| 16 | source | [backend/internal/config/config.go](../../../backend/internal/config/config.go) |
| 16 | test | [backend/internal/config/config_test.go](../../../backend/internal/config/config_test.go) |
| 03 | source | [backend/internal/handler/gateway_handler.go](../../../backend/internal/handler/gateway_handler.go) |
| 03 | source | [backend/internal/handler/gateway_handler_chat_completions.go](../../../backend/internal/handler/gateway_handler_chat_completions.go) |
| 03 | source | [backend/internal/handler/gemini_v1beta_handler.go](../../../backend/internal/handler/gemini_v1beta_handler.go) |
| 16 | source | [backend/internal/handler/openai_chat_completions.go](../../../backend/internal/handler/openai_chat_completions.go) |
| 03 | source | [backend/internal/handler/openai_gateway_handler.go](../../../backend/internal/handler/openai_gateway_handler.go) |
| 16 | test | [backend/internal/handler/openai_gateway_handler_test.go](../../../backend/internal/handler/openai_gateway_handler_test.go) |
| 16 | test | [backend/internal/handler/openai_gateway_ws_group_pricing_test.go](../../../backend/internal/handler/openai_gateway_ws_group_pricing_test.go) |
| 07 | source | [backend/internal/handler/openai_images.go](../../../backend/internal/handler/openai_images.go) |
| 16 | source | [backend/internal/repository/http_upstream.go](../../../backend/internal/repository/http_upstream.go) |
| 08 | source | [backend/internal/service/antigravity_gateway_compat.go](../../../backend/internal/service/antigravity_gateway_compat.go) |
| 08 | test | [backend/internal/service/antigravity_gateway_compat_test.go](../../../backend/internal/service/antigravity_gateway_compat_test.go) |
| 16 | source | [backend/internal/service/gateway_forward.go](../../../backend/internal/service/gateway_forward.go) |
| 04 | source | [backend/internal/service/gateway_service.go](../../../backend/internal/service/gateway_service.go) |
| 08 | source | [backend/internal/service/gemini_chat_completions_compat_service.go](../../../backend/internal/service/gemini_chat_completions_compat_service.go) |
| 08 | test | [backend/internal/service/gemini_chat_completions_compat_service_test.go](../../../backend/internal/service/gemini_chat_completions_compat_service_test.go) |
| 08 | source | [backend/internal/service/gemini_messages_compat_service.go](../../../backend/internal/service/gemini_messages_compat_service.go) |
| 08 | test | [backend/internal/service/gemini_messages_compat_service_test.go](../../../backend/internal/service/gemini_messages_compat_service_test.go) |
| 08 | source | [backend/internal/service/gemini_schema_capabilities.go](../../../backend/internal/service/gemini_schema_capabilities.go) |
| 16 | source | [backend/internal/service/http_upstream_profile.go](../../../backend/internal/service/http_upstream_profile.go) |
| 04 | source | [backend/internal/service/openai_account_scheduler.go](../../../backend/internal/service/openai_account_scheduler.go) |
| 16 | source | [backend/internal/service/openai_gateway_forward.go](../../../backend/internal/service/openai_gateway_forward.go) |
| 08 | test | [backend/internal/service/openai_gateway_request_body_reasoning_test.go](../../../backend/internal/service/openai_gateway_request_body_reasoning_test.go) |
| 03 | source | [backend/internal/service/openai_gateway_response_handling.go](../../../backend/internal/service/openai_gateway_response_handling.go) |
| 03 | source | [backend/internal/service/openai_gateway_service.go](../../../backend/internal/service/openai_gateway_service.go) |
| 03 | test | [backend/internal/service/openai_responses_rejected_field_retry_test.go](../../../backend/internal/service/openai_responses_rejected_field_retry_test.go) |
| 16 | source | [backend/internal/service/openai_ws_forwarder.go](../../../backend/internal/service/openai_ws_forwarder.go) |
| 03 | source | [backend/internal/service/openai_ws_forwarder_ingress.go](../../../backend/internal/service/openai_ws_forwarder_ingress.go) |
| 03 | test | [backend/internal/service/openai_ws_forwarder_ingress_session_test.go](../../../backend/internal/service/openai_ws_forwarder_ingress_session_test.go) |
| 03 | test | [backend/internal/service/openai_ws_forwarder_ingress_test.go](../../../backend/internal/service/openai_ws_forwarder_ingress_test.go) |
| 03 | source | [backend/internal/service/openai_ws_forwarder_v2.go](../../../backend/internal/service/openai_ws_forwarder_v2.go) |
| 16 | source | [backend/internal/service/openai_ws_http_bridge.go](../../../backend/internal/service/openai_ws_http_bridge.go) |
| 03 | source | [backend/internal/service/openai_ws_pool.go](../../../backend/internal/service/openai_ws_pool.go) |
| 16 | source | [backend/internal/service/openai_ws_session_preemption.go](../../../backend/internal/service/openai_ws_session_preemption.go) |
| 03 | test | [backend/internal/service/openai_ws_state_store_test.go](../../../backend/internal/service/openai_ws_state_store_test.go) |
| 03 | source | [backend/internal/service/openai_ws_v2_passthrough_adapter.go](../../../backend/internal/service/openai_ws_v2_passthrough_adapter.go) |
| 16 | source | [backend/internal/service/ops_upstream_context.go](../../../backend/internal/service/ops_upstream_context.go) |
| 15 | tooling | [deploy/config.example.yaml](../../../deploy/config.example.yaml) |
| 17 | documentation | [docs/OPENAI_RESPONSES_RECOVERY.md](../../../docs/OPENAI_RESPONSES_RECOVERY.md) |
| 17 | documentation | [docs/openai-transports.md](../../../docs/openai-transports.md) |
| 14 | source | [frontend/src/i18n/locales/en/admin/ops.ts](../../../frontend/src/i18n/locales/en/admin/ops.ts) |
| 14 | source | [frontend/src/i18n/locales/ja/admin/ops.ts](../../../frontend/src/i18n/locales/ja/admin/ops.ts) |
| 14 | generated | [frontend/src/i18n/locales/zh-TW/admin/ops.ts](../../../frontend/src/i18n/locales/zh-TW/admin/ops.ts) |
| 14 | source | [frontend/src/i18n/locales/zh/admin/ops.ts](../../../frontend/src/i18n/locales/zh/admin/ops.ts) |

## 2026-10-14 新增需求与实施边界

Gemini/Antigravity 的 EOF、usage-only、[DONE] 不伪装成功；首个协议错误保持、终止只发送一次。失败携带已观察用量并经原幂等结算；重试只补决策诊断，不改策略。保留 96 格 body 资源基准，不新增缓存层。
