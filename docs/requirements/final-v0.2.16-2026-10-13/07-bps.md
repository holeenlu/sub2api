# 07 · BPS、独立 Excel 授权、工具图片与 WS 桥接

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。


2026-10-14 增量归并：本页已纳入通过验收的 New API 实现，来源和逐文件归属见 [归并分析](NEW-API-INTEGRATION-2026-10-14.md)。原快照 SHA 仅代表 2026-10-13 历史输入。

## 最终需求

1. 保留用户 2026-10-12 明确恢复的 BPS 范围：HTTP/SSE、compact、Messages/Chat 兼容及客户端 WS → 原生会话 owner → BPS HTTP/SSE → WS。全局 BPS 和图片支持默认关闭。
2. 路由由全局开关、账号配置与映射后模型共同决定，保留原 Key/分组/模型/客户端/并发检查。Excel 单授权不得落入原生 Codex；BPS 不以账号原生 WS 开关作为必要条件。
3. Excel grant 独立加密保存，校验身份与到期，合并并发刷新；导入优先于旧刷新，旧 401 不撤销新授权，删除独立授权不删除主凭据。未配置时明确报错，不借用 Codex AT。
4. 保留 function/custom/namespace/dynamic 工具、工具目录继承、显式 tools:[]、tool_choice、参数与历史回放。未知或不支持工具必须明确处理，有界纠错不能丢失密文或突破尝试预算。
5. 处理图片输入、附件、工具截图、受限 HTTPS relay、容量与正文预算、自动 compact、图片生成和单图编辑。图片链接脱敏，原生回退遵守原权限及同一预算。
6. 工具修复、compact、取消、EOF、失败切换保留实际部分用量与去重；真实响应 ID 与计费 ID 分离。流尾完整终态优先，缺终态、截断 JSON 与真实读错误分别处理，失败事件序号不倒退。
7. WS 复用第 06 组的 owner、reader/writer、取消和恢复契约；首轮/续轮都校验路由、stream_id/event_id 关联。原生/BPS 模型切换需要重连，预热不推理不计费。
8. 区分模型拒绝、通用 403、429 和过载；仅在既有安全条件下原生换号。BPS 模型冷却不污染原生配额/健康分，关闭总开关后不阻挡普通原生账号。
9. 显式 403 自动关闭、分组动作、事务/CAS claim 与恢复保持；每分钟恢复扫描默认不发起探测。前端保存模板仅供表单使用，不自动重写账号。
10. Redis 共享状态可选；实例内状态、relay 可达性与多实例部署限制必须明示。只读来源核查工具记录固定来源、后续本地改动和候选，不自动覆盖或合并 ranxi 分支。
11. BPS-01/02/04–14/16/18 纳入；BPS-15 仅普通代理传输稳定性。BPS-03 自动登录 Worker、Mihomo 托管池、BPS-17 自动质量运营、票据/Cookie、额外准入与独立目录治理继续排除。
12. 保留 migration 272 和原始 SQL 字节、NOTICE 与来源散列；当前需求/行为决定优先于旧 BPS_REMOVAL 或 2026-10-07 归档的退役描述。

## 入口与配置归属

沿用下列方案和完整文件清单中的现有入口。

## 最终实现方案

1. BPS 协议包是供应商特定转换能力；现有 Responses owner、原生网关、调度器、计费去重承担共享规则，避免第二套恢复契约。

2. 授权通过既有 OAuth PKCE/state 加 excel profile 与独立加密 grant 表；已有主凭据结构无法同时表达独立 Excel grant，因此保留已实现的新表及三个状态接口。

3. 图片、工具、compact 的内部修复复用当前预算与用量记录；基于真实发送阶段/输出证据决定是否换号，修复错误归于实际请求 ID。

4. 显式 403 恢复只在用户启用时运行，事务 claim/CAS 防止并发复活；管理模板与运行配置分离。

5. 接口/设置数量引用现有 BPS 验收记录：7 个 HTTP method/path、2 个 YAML 字段、17 个 settings 键、15 个账号 Extra 白名单键、3 个网关主分流和1个可选恢复循环；这是继承功能，本次重组新增均为0。

## 边界、失败路径与依赖

关联功能组：17。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。此组与关联组共同形成完整运行行为；本次归并不额外改写已验收的业务源码。

## 验收要求

basispoints/transportdiag、service/handler BPS/Excel 专项，前端授权/默认模板；真实付费 grant、外部 relay 和 PostgreSQL/Redis 不由 unit 代替。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 7 | 1368 | 0 | 0 |
| source | 107 | 13408 | 31 | 0 |
| test | 117 | 19233 | 15 | 0 |
| tooling | 3 | 15274 | 0 | 0 |

相对官方 v0.2.16 的累计差异，已包含本批实现；不是本次归并额外编写的代码。

## 来源提交

- `2e83e47bc12d60c125917a986706c205ee6a3e91`
- `49b29ef158e6319cfd6b1227781307de5f8f870e`
- `4b2cfde513867c40dbcc663a5ddea30cdd286dd2`
- `944fdb1eff8e7ea33ed70ff722b6822f962e9255`
- `be0749922538fe576b5e7329609e4f1d7716699f`
- `c6a92e1501a3ed5bb405c02bd541fcb59366bd28`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| A | source | 71 | 0 | 07 | [backend/internal/handler/admin/openai_excel_oauth_handler.go](../../../backend/internal/handler/admin/openai_excel_oauth_handler.go) |
| A | source | 33 | 0 | 07 | [backend/internal/handler/admin/setting_handler_excel_bps_defaults.go](../../../backend/internal/handler/admin/setting_handler_excel_bps_defaults.go) |
| A | test | 45 | 0 | 07 | [backend/internal/handler/admin/setting_handler_excel_bps_defaults_test.go](../../../backend/internal/handler/admin/setting_handler_excel_bps_defaults_test.go) |
| A | test | 210 | 0 | 07 | [backend/internal/handler/openai_excel_bps_error_test.go](../../../backend/internal/handler/openai_excel_bps_error_test.go) |
| A | test | 619 | 0 | 07 | [backend/internal/handler/openai_excel_bps_failover_test.go](../../../backend/internal/handler/openai_excel_bps_failover_test.go) |
| A | source | 7 | 0 | 07 | [backend/internal/handler/openai_excel_bps_image.go](../../../backend/internal/handler/openai_excel_bps_image.go) |
| A | test | 270 | 0 | 07 | [backend/internal/handler/openai_excel_bps_image_admission_test.go](../../../backend/internal/handler/openai_excel_bps_image_admission_test.go) |
| M | source | 8 | 1 | 06,07 | [backend/internal/handler/openai_images.go](../../../backend/internal/handler/openai_images.go) |
| M | source | 30 | 4 | 07 | [backend/internal/pkg/httputil/body.go](../../../backend/internal/pkg/httputil/body.go) |
| M | source | 7 | 6 | 07 | [backend/internal/pkg/openai/constants.go](../../../backend/internal/pkg/openai/constants.go) |
| A | source | 73 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps.go](../../../backend/internal/repository/account_repo_excel_bps.go) |
| A | test | 107 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_auto_disable_test.go](../../../backend/internal/repository/account_repo_excel_bps_auto_disable_test.go) |
| A | source | 128 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_groups.go](../../../backend/internal/repository/account_repo_excel_bps_groups.go) |
| A | test | 287 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_groups_integration_test.go](../../../backend/internal/repository/account_repo_excel_bps_groups_integration_test.go) |
| A | test | 82 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_groups_test.go](../../../backend/internal/repository/account_repo_excel_bps_groups_test.go) |
| A | test | 177 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_integration_test.go](../../../backend/internal/repository/account_repo_excel_bps_integration_test.go) |
| A | source | 108 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_recovery.go](../../../backend/internal/repository/account_repo_excel_bps_recovery.go) |
| A | test | 178 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_recovery_integration_test.go](../../../backend/internal/repository/account_repo_excel_bps_recovery_integration_test.go) |
| A | source | 42 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_settings.go](../../../backend/internal/repository/account_repo_excel_bps_settings.go) |
| A | test | 99 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_test.go](../../../backend/internal/repository/account_repo_excel_bps_test.go) |
| A | source | 74 | 0 | 07 | [backend/internal/repository/account_repo_excel_credentials.go](../../../backend/internal/repository/account_repo_excel_credentials.go) |
| A | test | 57 | 0 | 07 | [backend/internal/repository/account_repo_excel_credentials_test.go](../../../backend/internal/repository/account_repo_excel_credentials_test.go) |
| M | test | 2 | 2 | 07 | [backend/internal/repository/account_repo_opencode_go_usage_test.go](../../../backend/internal/repository/account_repo_opencode_go_usage_test.go) |
| M | test | 8 | 8 | 07 | [backend/internal/repository/account_repo_upstream_billing_probe_update_test.go](../../../backend/internal/repository/account_repo_upstream_billing_probe_update_test.go) |
| A | source | 38 | 0 | 07 | [backend/internal/repository/excel_bps_image_policy_cache.go](../../../backend/internal/repository/excel_bps_image_policy_cache.go) |
| A | test | 63 | 0 | 07 | [backend/internal/repository/excel_bps_image_policy_cache_integration_test.go](../../../backend/internal/repository/excel_bps_image_policy_cache_integration_test.go) |
| A | test | 62 | 0 | 07 | [backend/internal/repository/excel_bps_image_policy_cache_test.go](../../../backend/internal/repository/excel_bps_image_policy_cache_test.go) |
| A | source | 122 | 0 | 07 | [backend/internal/repository/gateway_cache_bps_state.go](../../../backend/internal/repository/gateway_cache_bps_state.go) |
| A | test | 75 | 0 | 07 | [backend/internal/repository/gateway_cache_bps_state_test.go](../../../backend/internal/repository/gateway_cache_bps_state_test.go) |
| A | source | 100 | 0 | 07 | [backend/internal/repository/http_upstream_bps.go](../../../backend/internal/repository/http_upstream_bps.go) |
| A | test | 153 | 0 | 07 | [backend/internal/repository/http_upstream_bps_eof_test.go](../../../backend/internal/repository/http_upstream_bps_eof_test.go) |
| A | test | 109 | 0 | 07 | [backend/internal/repository/http_upstream_bps_policy_test.go](../../../backend/internal/repository/http_upstream_bps_policy_test.go) |
| A | source | 41 | 0 | 07 | [backend/internal/server/middleware/excel_bps_image_admission.go](../../../backend/internal/server/middleware/excel_bps_image_admission.go) |
| A | test | 69 | 0 | 07 | [backend/internal/server/middleware/excel_bps_image_admission_test.go](../../../backend/internal/server/middleware/excel_bps_image_admission_test.go) |
| M | source | 8 | 1 | 07 | [backend/internal/server/middleware/request_logger.go](../../../backend/internal/server/middleware/request_logger.go) |
| A | test | 68 | 0 | 07 | [backend/internal/server/routes/excel_bps_image_admission_test.go](../../../backend/internal/server/routes/excel_bps_image_admission_test.go) |
| M | source | 6 | 2 | 07 | [backend/internal/server/routes/gateway.go](../../../backend/internal/server/routes/gateway.go) |
| M | test | 2 | 2 | 07 | [backend/internal/server/routes/gateway_model_allowlist_test.go](../../../backend/internal/server/routes/gateway_model_allowlist_test.go) |
| M | source | 112 | 0 | 07 | [backend/internal/service/account.go](../../../backend/internal/service/account.go) |
| A | source | 45 | 0 | 07 | [backend/internal/service/account_excel_bps_403_marker.go](../../../backend/internal/service/account_excel_bps_403_marker.go) |
| A | test | 112 | 0 | 07 | [backend/internal/service/account_excel_bps_403_marker_test.go](../../../backend/internal/service/account_excel_bps_403_marker_test.go) |
| A | source | 121 | 0 | 07 | [backend/internal/service/account_excel_bps_groups.go](../../../backend/internal/service/account_excel_bps_groups.go) |
| A | source | 38 | 0 | 07 | [backend/internal/service/account_excel_bps_recovery_settings.go](../../../backend/internal/service/account_excel_bps_recovery_settings.go) |
| A | test | 65 | 0 | 07 | [backend/internal/service/account_excel_bps_recovery_settings_test.go](../../../backend/internal/service/account_excel_bps_recovery_settings_test.go) |
| A | source | 15 | 0 | 07 | [backend/internal/service/account_excel_bps_settings.go](../../../backend/internal/service/account_excel_bps_settings.go) |
| A | source | 18 | 0 | 07 | [backend/internal/service/account_excel_bps_tools.go](../../../backend/internal/service/account_excel_bps_tools.go) |
| A | source | 35 | 0 | 07 | [backend/internal/service/account_test_bps_evidence.go](../../../backend/internal/service/account_test_bps_evidence.go) |
| A | test | 49 | 0 | 07 | [backend/internal/service/account_test_bps_evidence_test.go](../../../backend/internal/service/account_test_bps_evidence_test.go) |
| M | source | 167 | 6 | 07 | [backend/internal/service/account_test_service.go](../../../backend/internal/service/account_test_service.go) |
| A | source | 250 | 0 | 07 | [backend/internal/service/account_test_service_bps_probe.go](../../../backend/internal/service/account_test_service_bps_probe.go) |
| A | test | 238 | 0 | 07 | [backend/internal/service/account_test_service_bps_probe_test.go](../../../backend/internal/service/account_test_service_bps_probe_test.go) |
| A | test | 89 | 0 | 07 | [backend/internal/service/account_test_service_excel_bps_test.go](../../../backend/internal/service/account_test_service_excel_bps_test.go) |
| M | source | 56 | 0 | 07 | [backend/internal/service/admin_account.go](../../../backend/internal/service/admin_account.go) |
| A | documentation | 104 | 0 | 07 | [backend/internal/service/basispoints/NOTICE.md](../../../backend/internal/service/basispoints/NOTICE.md) |
| A | documentation | 202 | 0 | 07 | [backend/internal/service/basispoints/README.md](../../../backend/internal/service/basispoints/README.md) |
| A | source | 76 | 0 | 07 | [backend/internal/service/basispoints/agent_assignment.go](../../../backend/internal/service/basispoints/agent_assignment.go) |
| A | test | 75 | 0 | 07 | [backend/internal/service/basispoints/agent_assignment_test.go](../../../backend/internal/service/basispoints/agent_assignment_test.go) |
| A | test | 177 | 0 | 07 | [backend/internal/service/basispoints/agent_message_test.go](../../../backend/internal/service/basispoints/agent_message_test.go) |
| A | source | 415 | 0 | 07 | [backend/internal/service/basispoints/attachments.go](../../../backend/internal/service/basispoints/attachments.go) |
| A | test | 384 | 0 | 07 | [backend/internal/service/basispoints/attachments_test.go](../../../backend/internal/service/basispoints/attachments_test.go) |
| A | test | 362 | 0 | 07 | [backend/internal/service/basispoints/basispoints_test.go](../../../backend/internal/service/basispoints/basispoints_test.go) |
| A | source | 144 | 0 | 07 | [backend/internal/service/basispoints/catalog.go](../../../backend/internal/service/basispoints/catalog.go) |
| A | source | 216 | 0 | 07 | [backend/internal/service/basispoints/catalog_cache.go](../../../backend/internal/service/basispoints/catalog_cache.go) |
| A | test | 67 | 0 | 07 | [backend/internal/service/basispoints/catalog_compact_test.go](../../../backend/internal/service/basispoints/catalog_compact_test.go) |
| A | source | 174 | 0 | 07 | [backend/internal/service/basispoints/catalog_items.go](../../../backend/internal/service/basispoints/catalog_items.go) |
| A | test | 199 | 0 | 07 | [backend/internal/service/basispoints/catalog_items_test.go](../../../backend/internal/service/basispoints/catalog_items_test.go) |
| A | test | 40 | 0 | 07 | [backend/internal/service/basispoints/catalog_test.go](../../../backend/internal/service/basispoints/catalog_test.go) |
| A | source | 70 | 0 | 07 | [backend/internal/service/basispoints/content.go](../../../backend/internal/service/basispoints/content.go) |
| A | test | 77 | 0 | 07 | [backend/internal/service/basispoints/content_diagnostics_test.go](../../../backend/internal/service/basispoints/content_diagnostics_test.go) |
| A | test | 28 | 0 | 07 | [backend/internal/service/basispoints/content_test.go](../../../backend/internal/service/basispoints/content_test.go) |
| A | test | 137 | 0 | 07 | [backend/internal/service/basispoints/custom_history_transport_test.go](../../../backend/internal/service/basispoints/custom_history_transport_test.go) |
| A | source | 31 | 0 | 07 | [backend/internal/service/basispoints/custom_transport.go](../../../backend/internal/service/basispoints/custom_transport.go) |
| A | test | 93 | 0 | 07 | [backend/internal/service/basispoints/custom_transport_test.go](../../../backend/internal/service/basispoints/custom_transport_test.go) |
| A | test | 115 | 0 | 07 | [backend/internal/service/basispoints/deep_audit_test.go](../../../backend/internal/service/basispoints/deep_audit_test.go) |
| A | test | 153 | 0 | 07 | [backend/internal/service/basispoints/deep_envelope_test.go](../../../backend/internal/service/basispoints/deep_envelope_test.go) |
| A | test | 169 | 0 | 07 | [backend/internal/service/basispoints/direct_call_test.go](../../../backend/internal/service/basispoints/direct_call_test.go) |
| A | source | 70 | 0 | 07 | [backend/internal/service/basispoints/encrypted_content.go](../../../backend/internal/service/basispoints/encrypted_content.go) |
| A | test | 107 | 0 | 07 | [backend/internal/service/basispoints/encrypted_content_test.go](../../../backend/internal/service/basispoints/encrypted_content_test.go) |
| A | source | 300 | 0 | 07 | [backend/internal/service/basispoints/envelope.go](../../../backend/internal/service/basispoints/envelope.go) |
| A | test | 189 | 0 | 07 | [backend/internal/service/basispoints/envelope_test.go](../../../backend/internal/service/basispoints/envelope_test.go) |
| A | source | 90 | 0 | 07 | [backend/internal/service/basispoints/function_cmd_transport.go](../../../backend/internal/service/basispoints/function_cmd_transport.go) |
| A | test | 370 | 0 | 07 | [backend/internal/service/basispoints/function_cmd_transport_test.go](../../../backend/internal/service/basispoints/function_cmd_transport_test.go) |
| A | source | 75 | 0 | 07 | [backend/internal/service/basispoints/function_code_transport.go](../../../backend/internal/service/basispoints/function_code_transport.go) |
| A | test | 264 | 0 | 07 | [backend/internal/service/basispoints/function_code_transport_test.go](../../../backend/internal/service/basispoints/function_code_transport_test.go) |
| A | test | 123 | 0 | 07 | [backend/internal/service/basispoints/history_collision_test.go](../../../backend/internal/service/basispoints/history_collision_test.go) |
| A | test | 83 | 0 | 07 | [backend/internal/service/basispoints/history_message_id_test.go](../../../backend/internal/service/basispoints/history_message_id_test.go) |
| A | test | 90 | 0 | 07 | [backend/internal/service/basispoints/history_message_images_test.go](../../../backend/internal/service/basispoints/history_message_images_test.go) |
| A | source | 98 | 0 | 07 | [backend/internal/service/basispoints/history_messages.go](../../../backend/internal/service/basispoints/history_messages.go) |
| A | test | 189 | 0 | 07 | [backend/internal/service/basispoints/history_messages_test.go](../../../backend/internal/service/basispoints/history_messages_test.go) |
| A | test | 172 | 0 | 07 | [backend/internal/service/basispoints/history_recovery_test.go](../../../backend/internal/service/basispoints/history_recovery_test.go) |
| A | source | 17 | 0 | 07 | [backend/internal/service/basispoints/image_capacity.go](../../../backend/internal/service/basispoints/image_capacity.go) |
| A | test | 78 | 0 | 07 | [backend/internal/service/basispoints/image_file_reference_test.go](../../../backend/internal/service/basispoints/image_file_reference_test.go) |
| A | source | 257 | 0 | 07 | [backend/internal/service/basispoints/image_policy.go](../../../backend/internal/service/basispoints/image_policy.go) |
| A | source | 234 | 0 | 07 | [backend/internal/service/basispoints/image_policy_stream.go](../../../backend/internal/service/basispoints/image_policy_stream.go) |
| A | test | 205 | 0 | 07 | [backend/internal/service/basispoints/image_policy_test.go](../../../backend/internal/service/basispoints/image_policy_test.go) |
| A | source | 492 | 0 | 07 | [backend/internal/service/basispoints/image_relay.go](../../../backend/internal/service/basispoints/image_relay.go) |
| A | test | 174 | 0 | 07 | [backend/internal/service/basispoints/image_relay_disk_test.go](../../../backend/internal/service/basispoints/image_relay_disk_test.go) |
| A | source | 78 | 0 | 07 | [backend/internal/service/basispoints/image_relay_limits.go](../../../backend/internal/service/basispoints/image_relay_limits.go) |
| A | test | 167 | 0 | 07 | [backend/internal/service/basispoints/image_relay_limits_test.go](../../../backend/internal/service/basispoints/image_relay_limits_test.go) |
| A | test | 410 | 0 | 07 | [backend/internal/service/basispoints/image_relay_test.go](../../../backend/internal/service/basispoints/image_relay_test.go) |
| A | source | 64 | 0 | 07 | [backend/internal/service/basispoints/images.go](../../../backend/internal/service/basispoints/images.go) |
| A | test | 57 | 0 | 07 | [backend/internal/service/basispoints/images_test.go](../../../backend/internal/service/basispoints/images_test.go) |
| A | test | 138 | 0 | 07 | [backend/internal/service/basispoints/invocation_recovery_test.go](../../../backend/internal/service/basispoints/invocation_recovery_test.go) |
| A | source | 113 | 0 | 07 | [backend/internal/service/basispoints/persistent_state.go](../../../backend/internal/service/basispoints/persistent_state.go) |
| A | source | 257 | 0 | 07 | [backend/internal/service/basispoints/plan.go](../../../backend/internal/service/basispoints/plan.go) |
| A | test | 222 | 0 | 07 | [backend/internal/service/basispoints/plan_test.go](../../../backend/internal/service/basispoints/plan_test.go) |
| A | source | 103 | 0 | 07 | [backend/internal/service/basispoints/progress_timeout.go](../../../backend/internal/service/basispoints/progress_timeout.go) |
| A | test | 93 | 0 | 07 | [backend/internal/service/basispoints/progress_timeout_test.go](../../../backend/internal/service/basispoints/progress_timeout_test.go) |
| A | test | 503 | 0 | 07 | [backend/internal/service/basispoints/protocol_completion_test.go](../../../backend/internal/service/basispoints/protocol_completion_test.go) |
| A | test | 30 | 0 | 07 | [backend/internal/service/basispoints/raw_custom_call_test.go](../../../backend/internal/service/basispoints/raw_custom_call_test.go) |
| A | test | 138 | 0 | 07 | [backend/internal/service/basispoints/replay_cache_test.go](../../../backend/internal/service/basispoints/replay_cache_test.go) |
| A | source | 247 | 0 | 07 | [backend/internal/service/basispoints/request.go](../../../backend/internal/service/basispoints/request.go) |
| A | test | 57 | 0 | 07 | [backend/internal/service/basispoints/request_compatibility_test.go](../../../backend/internal/service/basispoints/request_compatibility_test.go) |
| A | source | 47 | 0 | 07 | [backend/internal/service/basispoints/route.go](../../../backend/internal/service/basispoints/route.go) |
| A | test | 23 | 0 | 07 | [backend/internal/service/basispoints/route_test.go](../../../backend/internal/service/basispoints/route_test.go) |
| A | source | 441 | 0 | 07 | [backend/internal/service/basispoints/stream.go](../../../backend/internal/service/basispoints/stream.go) |
| A | test | 131 | 0 | 07 | [backend/internal/service/basispoints/stream_repair_boundary_test.go](../../../backend/internal/service/basispoints/stream_repair_boundary_test.go) |
| A | source | 199 | 0 | 07 | [backend/internal/service/basispoints/structured_output.go](../../../backend/internal/service/basispoints/structured_output.go) |
| A | test | 283 | 0 | 07 | [backend/internal/service/basispoints/structured_output_test.go](../../../backend/internal/service/basispoints/structured_output_test.go) |
| A | test | 68 | 0 | 07 | [backend/internal/service/basispoints/terminal_tools_test.go](../../../backend/internal/service/basispoints/terminal_tools_test.go) |
| A | test | 126 | 0 | 07 | [backend/internal/service/basispoints/tool_duplicates_test.go](../../../backend/internal/service/basispoints/tool_duplicates_test.go) |
| A | source | 75 | 0 | 07 | [backend/internal/service/basispoints/tool_examples.go](../../../backend/internal/service/basispoints/tool_examples.go) |
| A | test | 107 | 0 | 07 | [backend/internal/service/basispoints/tool_examples_test.go](../../../backend/internal/service/basispoints/tool_examples_test.go) |
| A | source | 45 | 0 | 07 | [backend/internal/service/basispoints/tool_images.go](../../../backend/internal/service/basispoints/tool_images.go) |
| A | test | 229 | 0 | 07 | [backend/internal/service/basispoints/tool_images_test.go](../../../backend/internal/service/basispoints/tool_images_test.go) |
| A | source | 362 | 0 | 07 | [backend/internal/service/basispoints/tool_repair.go](../../../backend/internal/service/basispoints/tool_repair.go) |
| A | test | 495 | 0 | 07 | [backend/internal/service/basispoints/tool_repair_test.go](../../../backend/internal/service/basispoints/tool_repair_test.go) |
| A | source | 92 | 0 | 07 | [backend/internal/service/basispoints/tool_schema.go](../../../backend/internal/service/basispoints/tool_schema.go) |
| A | source | 785 | 0 | 07 | [backend/internal/service/basispoints/tools.go](../../../backend/internal/service/basispoints/tools.go) |
| A | source | 272 | 0 | 07 | [backend/internal/service/basispoints/unknown_tool_repair.go](../../../backend/internal/service/basispoints/unknown_tool_repair.go) |
| A | source | 191 | 0 | 07 | [backend/internal/service/basispoints/upstream_failure.go](../../../backend/internal/service/basispoints/upstream_failure.go) |
| A | test | 290 | 0 | 07 | [backend/internal/service/basispoints/upstream_failure_test.go](../../../backend/internal/service/basispoints/upstream_failure_test.go) |
| M | source | 8 | 1 | 07 | [backend/internal/service/grok_credential_failure.go](../../../backend/internal/service/grok_credential_failure.go) |
| M | source | 6 | 0 | 07 | [backend/internal/service/openai_account_runtime_block_fastpath.go](../../../backend/internal/service/openai_account_runtime_block_fastpath.go) |
| M | source | 89 | 1 | 07 | [backend/internal/service/openai_bulk_account_settings.go](../../../backend/internal/service/openai_bulk_account_settings.go) |
| M | source | 11 | 1 | 07 | [backend/internal/service/openai_codex_model_metadata.go](../../../backend/internal/service/openai_codex_model_metadata.go) |
| M | source | 4 | 0 | 07 | [backend/internal/service/openai_compact_probe.go](../../../backend/internal/service/openai_compact_probe.go) |
| M | source | 4 | 0 | 07 | [backend/internal/service/openai_compact_stream_bridge.go](../../../backend/internal/service/openai_compact_stream_bridge.go) |
| A | source | 1134 | 0 | 07 | [backend/internal/service/openai_excel_bps.go](../../../backend/internal/service/openai_excel_bps.go) |
| A | test | 80 | 0 | 07 | [backend/internal/service/openai_excel_bps_agent_assignment_test.go](../../../backend/internal/service/openai_excel_bps_agent_assignment_test.go) |
| A | test | 137 | 0 | 07 | [backend/internal/service/openai_excel_bps_attachment_diagnostics_test.go](../../../backend/internal/service/openai_excel_bps_attachment_diagnostics_test.go) |
| A | source | 132 | 0 | 07 | [backend/internal/service/openai_excel_bps_attachments.go](../../../backend/internal/service/openai_excel_bps_attachments.go) |
| A | test | 481 | 0 | 07 | [backend/internal/service/openai_excel_bps_attachments_test.go](../../../backend/internal/service/openai_excel_bps_attachments_test.go) |
| A | test | 160 | 0 | 07 | [backend/internal/service/openai_excel_bps_auto_disable_test.go](../../../backend/internal/service/openai_excel_bps_auto_disable_test.go) |
| A | test | 165 | 0 | 07 | [backend/internal/service/openai_excel_bps_billing_test.go](../../../backend/internal/service/openai_excel_bps_billing_test.go) |
| A | source | 15 | 0 | 07 | [backend/internal/service/openai_excel_bps_cancellation.go](../../../backend/internal/service/openai_excel_bps_cancellation.go) |
| A | test | 107 | 0 | 07 | [backend/internal/service/openai_excel_bps_cancellation_test.go](../../../backend/internal/service/openai_excel_bps_cancellation_test.go) |
| A | source | 196 | 0 | 07 | [backend/internal/service/openai_excel_bps_compat.go](../../../backend/internal/service/openai_excel_bps_compat.go) |
| A | test | 86 | 0 | 07 | [backend/internal/service/openai_excel_bps_compat_test.go](../../../backend/internal/service/openai_excel_bps_compat_test.go) |
| A | test | 125 | 0 | 07 | [backend/internal/service/openai_excel_bps_completion_test.go](../../../backend/internal/service/openai_excel_bps_completion_test.go) |
| A | source | 24 | 0 | 07 | [backend/internal/service/openai_excel_bps_encrypted.go](../../../backend/internal/service/openai_excel_bps_encrypted.go) |
| A | test | 92 | 0 | 07 | [backend/internal/service/openai_excel_bps_encrypted_test.go](../../../backend/internal/service/openai_excel_bps_encrypted_test.go) |
| A | test | 488 | 0 | 07 | [backend/internal/service/openai_excel_bps_failure_test.go](../../../backend/internal/service/openai_excel_bps_failure_test.go) |
| A | test | 197 | 0 | 07 | [backend/internal/service/openai_excel_bps_groups_test.go](../../../backend/internal/service/openai_excel_bps_groups_test.go) |
| A | test | 59 | 0 | 07 | [backend/internal/service/openai_excel_bps_history_messages_test.go](../../../backend/internal/service/openai_excel_bps_history_messages_test.go) |
| A | test | 120 | 0 | 07 | [backend/internal/service/openai_excel_bps_ignore_encrypted_content_e2e_test.go](../../../backend/internal/service/openai_excel_bps_ignore_encrypted_content_e2e_test.go) |
| A | source | 170 | 0 | 07 | [backend/internal/service/openai_excel_bps_image_admission.go](../../../backend/internal/service/openai_excel_bps_image_admission.go) |
| A | test | 333 | 0 | 07 | [backend/internal/service/openai_excel_bps_image_admission_test.go](../../../backend/internal/service/openai_excel_bps_image_admission_test.go) |
| A | source | 217 | 0 | 07 | [backend/internal/service/openai_excel_bps_image_policy.go](../../../backend/internal/service/openai_excel_bps_image_policy.go) |
| A | test | 446 | 0 | 07 | [backend/internal/service/openai_excel_bps_image_policy_test.go](../../../backend/internal/service/openai_excel_bps_image_policy_test.go) |
| A | test | 142 | 0 | 07 | [backend/internal/service/openai_excel_bps_image_test.go](../../../backend/internal/service/openai_excel_bps_image_test.go) |
| A | source | 390 | 0 | 07 | [backend/internal/service/openai_excel_bps_images.go](../../../backend/internal/service/openai_excel_bps_images.go) |
| A | test | 195 | 0 | 07 | [backend/internal/service/openai_excel_bps_images_e2e_test.go](../../../backend/internal/service/openai_excel_bps_images_e2e_test.go) |
| A | test | 464 | 0 | 07 | [backend/internal/service/openai_excel_bps_images_test.go](../../../backend/internal/service/openai_excel_bps_images_test.go) |
| A | source | 386 | 0 | 07 | [backend/internal/service/openai_excel_bps_model_access.go](../../../backend/internal/service/openai_excel_bps_model_access.go) |
| A | test | 178 | 0 | 07 | [backend/internal/service/openai_excel_bps_model_access_test.go](../../../backend/internal/service/openai_excel_bps_model_access_test.go) |
| A | source | 91 | 0 | 07 | [backend/internal/service/openai_excel_bps_model_cooldown.go](../../../backend/internal/service/openai_excel_bps_model_cooldown.go) |
| A | test | 114 | 0 | 07 | [backend/internal/service/openai_excel_bps_model_cooldown_test.go](../../../backend/internal/service/openai_excel_bps_model_cooldown_test.go) |
| A | source | 80 | 0 | 07 | [backend/internal/service/openai_excel_bps_models_manifest.go](../../../backend/internal/service/openai_excel_bps_models_manifest.go) |
| A | test | 133 | 0 | 07 | [backend/internal/service/openai_excel_bps_models_manifest_test.go](../../../backend/internal/service/openai_excel_bps_models_manifest_test.go) |
| A | test | 105 | 0 | 07 | [backend/internal/service/openai_excel_bps_models_test.go](../../../backend/internal/service/openai_excel_bps_models_test.go) |
| A | source | 20 | 0 | 07 | [backend/internal/service/openai_excel_bps_progress.go](../../../backend/internal/service/openai_excel_bps_progress.go) |
| A | test | 54 | 0 | 07 | [backend/internal/service/openai_excel_bps_progress_test.go](../../../backend/internal/service/openai_excel_bps_progress_test.go) |
| A | source | 122 | 0 | 07 | [backend/internal/service/openai_excel_bps_ratelimit.go](../../../backend/internal/service/openai_excel_bps_ratelimit.go) |
| A | test | 498 | 0 | 07 | [backend/internal/service/openai_excel_bps_ratelimit_test.go](../../../backend/internal/service/openai_excel_bps_ratelimit_test.go) |
| A | source | 294 | 0 | 07 | [backend/internal/service/openai_excel_bps_recovery.go](../../../backend/internal/service/openai_excel_bps_recovery.go) |
| A | test | 199 | 0 | 07 | [backend/internal/service/openai_excel_bps_recovery_test.go](../../../backend/internal/service/openai_excel_bps_recovery_test.go) |
| A | test | 410 | 0 | 07 | [backend/internal/service/openai_excel_bps_reliability_test.go](../../../backend/internal/service/openai_excel_bps_reliability_test.go) |
| A | test | 455 | 0 | 07 | [backend/internal/service/openai_excel_bps_repair_test.go](../../../backend/internal/service/openai_excel_bps_repair_test.go) |
| A | test | 39 | 0 | 07 | [backend/internal/service/openai_excel_bps_scheduler_health_test.go](../../../backend/internal/service/openai_excel_bps_scheduler_health_test.go) |
| A | test | 115 | 0 | 07 | [backend/internal/service/openai_excel_bps_structured_test.go](../../../backend/internal/service/openai_excel_bps_structured_test.go) |
| A | test | 302 | 0 | 07 | [backend/internal/service/openai_excel_bps_test.go](../../../backend/internal/service/openai_excel_bps_test.go) |
| A | test | 107 | 0 | 07 | [backend/internal/service/openai_excel_bps_tool_images_test.go](../../../backend/internal/service/openai_excel_bps_tool_images_test.go) |
| A | test | 141 | 0 | 07 | [backend/internal/service/openai_excel_bps_tools_test.go](../../../backend/internal/service/openai_excel_bps_tools_test.go) |
| A | source | 197 | 0 | 07 | [backend/internal/service/openai_excel_bps_transport.go](../../../backend/internal/service/openai_excel_bps_transport.go) |
| A | test | 12 | 0 | 07 | [backend/internal/service/openai_excel_bps_transport_test.go](../../../backend/internal/service/openai_excel_bps_transport_test.go) |
| A | source | 45 | 0 | 07 | [backend/internal/service/openai_excel_bps_usage.go](../../../backend/internal/service/openai_excel_bps_usage.go) |
| A | test | 129 | 0 | 07 | [backend/internal/service/openai_excel_bps_usage_test.go](../../../backend/internal/service/openai_excel_bps_usage_test.go) |
| A | source | 402 | 0 | 07 | [backend/internal/service/openai_excel_oauth.go](../../../backend/internal/service/openai_excel_oauth.go) |
| A | test | 189 | 0 | 07 | [backend/internal/service/openai_excel_oauth_test.go](../../../backend/internal/service/openai_excel_oauth_test.go) |
| M | source | 11 | 0 | 07 | [backend/internal/service/openai_gateway_chat_completions.go](../../../backend/internal/service/openai_gateway_chat_completions.go) |
| M | source | 12 | 1 | 07 | [backend/internal/service/openai_gateway_messages.go](../../../backend/internal/service/openai_gateway_messages.go) |
| M | source | 34 | 2 | 07 | [backend/internal/service/openai_gateway_scheduling.go](../../../backend/internal/service/openai_gateway_scheduling.go) |
| M | source | 7 | 3 | 07 | [backend/internal/service/openai_gateway_usage.go](../../../backend/internal/service/openai_gateway_usage.go) |
| M | source | 17 | 0 | 07 | [backend/internal/service/openai_images_responses.go](../../../backend/internal/service/openai_images_responses.go) |
| A | source | 157 | 0 | 07 | [backend/internal/service/openai_sse_read_pump.go](../../../backend/internal/service/openai_sse_read_pump.go) |
| A | test | 114 | 0 | 07 | [backend/internal/service/openai_sse_read_pump_test.go](../../../backend/internal/service/openai_sse_read_pump_test.go) |
| A | test | 360 | 0 | 07 | [backend/internal/service/openai_ws_bps_ingress_test.go](../../../backend/internal/service/openai_ws_bps_ingress_test.go) |
| A | source | 269 | 0 | 07 | [backend/internal/service/openai_ws_excel_bps_bridge.go](../../../backend/internal/service/openai_ws_excel_bps_bridge.go) |
| M | test | 15 | 2 | 07 | [backend/internal/service/openai_ws_session_preemption_test.go](../../../backend/internal/service/openai_ws_session_preemption_test.go) |
| A | source | 21 | 0 | 07 | [backend/internal/service/setting_excel_bps.go](../../../backend/internal/service/setting_excel_bps.go) |
| A | source | 101 | 0 | 07 | [backend/internal/service/setting_excel_bps_defaults.go](../../../backend/internal/service/setting_excel_bps_defaults.go) |
| A | test | 62 | 0 | 07 | [backend/internal/service/setting_excel_bps_defaults_test.go](../../../backend/internal/service/setting_excel_bps_defaults_test.go) |
| A | source | 208 | 0 | 07 | [backend/internal/service/setting_excel_bps_image.go](../../../backend/internal/service/setting_excel_bps_image.go) |
| A | test | 235 | 0 | 07 | [backend/internal/service/setting_excel_bps_image_test.go](../../../backend/internal/service/setting_excel_bps_image_test.go) |
| A | source | 67 | 0 | 07 | [backend/internal/util/transportdiag/error.go](../../../backend/internal/util/transportdiag/error.go) |
| A | source | 109 | 0 | 07 | [backend/internal/util/transportdiag/trace.go](../../../backend/internal/util/transportdiag/trace.go) |
| A | test | 72 | 0 | 07 | [backend/internal/util/transportdiag/trace_test.go](../../../backend/internal/util/transportdiag/trace_test.go) |
| A | documentation | 131 | 0 | 07,17 | [docs/BPS_REMOVAL.md](../../../docs/BPS_REMOVAL.md) |
| A | documentation | 147 | 0 | 07 | [docs/bps/DUAL_PATH_REVIEW.md](../../../docs/bps/DUAL_PATH_REVIEW.md) |
| A | documentation | 506 | 0 | 07 | [docs/bps/REQUIREMENTS.md](../../../docs/bps/REQUIREMENTS.md) |
| A | documentation | 162 | 0 | 07 | [docs/bps/VALIDATION.md](../../../docs/bps/VALIDATION.md) |
| A | tooling | 1824 | 0 | 07 | [docs/bps/integration-state.json](../../../docs/bps/integration-state.json) |
| A | tooling | 13371 | 0 | 07 | [docs/bps/source-inventory.json](../../../docs/bps/source-inventory.json) |
| A | documentation | 116 | 0 | 07 | [docs/bps/upstream-changes.md](../../../docs/bps/upstream-changes.md) |
| A | source | 11 | 0 | 07 | [frontend/src/api/admin/excelBPSDefaults.ts](../../../frontend/src/api/admin/excelBPSDefaults.ts) |
| M | source | 24 | 0 | 07 | [frontend/src/components/account/AccountStatusIndicator.vue](../../../frontend/src/components/account/AccountStatusIndicator.vue) |
| M | source | 10 | 2 | 07 | [frontend/src/components/account/AccountTestModal.vue](../../../frontend/src/components/account/AccountTestModal.vue) |
| A | source | 60 | 0 | 07 | [frontend/src/components/account/ExcelBPS403Badge.vue](../../../frontend/src/components/account/ExcelBPS403Badge.vue) |
| A | source | 85 | 0 | 07 | [frontend/src/components/account/ExcelBPSAuthorization.vue](../../../frontend/src/components/account/ExcelBPSAuthorization.vue) |
| A | source | 37 | 0 | 07 | [frontend/src/components/account/ExcelBPSModeSwitches.vue](../../../frontend/src/components/account/ExcelBPSModeSwitches.vue) |
| M | test | 9 | 1 | 07 | [frontend/src/components/account/__tests__/EditAccountModal.grokMediaEligibility.spec.ts](../../../frontend/src/components/account/__tests__/EditAccountModal.grokMediaEligibility.spec.ts) |
| M | test | 4 | 0 | 07 | [frontend/src/components/account/__tests__/EditAccountModal.grokUpstream.spec.ts](../../../frontend/src/components/account/__tests__/EditAccountModal.grokUpstream.spec.ts) |
| A | test | 161 | 0 | 07 | [frontend/src/components/account/__tests__/ExcelBPS403Badge.spec.ts](../../../frontend/src/components/account/__tests__/ExcelBPS403Badge.spec.ts) |
| A | test | 48 | 0 | 07 | [frontend/src/components/account/__tests__/ExcelBPSAuthorization.spec.ts](../../../frontend/src/components/account/__tests__/ExcelBPSAuthorization.spec.ts) |
| A | source | 63 | 0 | 07 | [frontend/src/components/admin/operations/BPSDefaultsCard.vue](../../../frontend/src/components/admin/operations/BPSDefaultsCard.vue) |
| A | source | 51 | 0 | 07 | [frontend/src/components/admin/operations/BPSDefaultsPanel.vue](../../../frontend/src/components/admin/operations/BPSDefaultsPanel.vue) |
| A | test | 114 | 0 | 07 | [frontend/src/composables/__tests__/useExcelBPSDefaults.spec.ts](../../../frontend/src/composables/__tests__/useExcelBPSDefaults.spec.ts) |
| A | source | 62 | 0 | 07 | [frontend/src/composables/useExcelBPSDefaults.ts](../../../frontend/src/composables/useExcelBPSDefaults.ts) |
| M | source | 2 | 0 | 07 | [frontend/src/constants/account.ts](../../../frontend/src/constants/account.ts) |
| A | source | 45 | 0 | 07 | [frontend/src/utils/excelBPSDefaults.ts](../../../frontend/src/utils/excelBPSDefaults.ts) |
| A | source | 14 | 0 | 07 | [frontend/src/utils/excelBPSImageLimits.ts](../../../frontend/src/utils/excelBPSImageLimits.ts) |
| A | source | 11 | 0 | 07 | [frontend/src/utils/excelBPSRecovery.ts](../../../frontend/src/utils/excelBPSRecovery.ts) |
| A | tooling | 79 | 0 | 07 | [scripts/bps/upstream-review.py](../../../scripts/bps/upstream-review.py) |

## 关联实现（由其他组提交）

同一共享文件保持单一主提交，跨组依赖随完整提交集验收。

| 主归属组 | 类别 | 文件 |
| --- | --- | --- |
| 15 | tooling | [.gitignore](../../../.gitignore) |
| 17 | documentation | [AGENTS.md](../../../AGENTS.md) |
| 16 | source | [backend/cmd/server/wire.go](../../../backend/cmd/server/wire.go) |
| 16 | generated | [backend/cmd/server/wire_gen.go](../../../backend/cmd/server/wire_gen.go) |
| 16 | tooling | [backend/go.mod](../../../backend/go.mod) |
| 16 | tooling | [backend/go.sum](../../../backend/go.sum) |
| 02 | source | [backend/internal/authz/routes.go](../../../backend/internal/authz/routes.go) |
| 16 | source | [backend/internal/config/config.go](../../../backend/internal/config/config.go) |
| 02 | source | [backend/internal/handler/admin/openai_oauth_handler.go](../../../backend/internal/handler/admin/openai_oauth_handler.go) |
| 16 | source | [backend/internal/handler/admin/setting_handler.go](../../../backend/internal/handler/admin/setting_handler.go) |
| 16 | source | [backend/internal/handler/admin/setting_handler_update.go](../../../backend/internal/handler/admin/setting_handler_update.go) |
| 16 | source | [backend/internal/handler/dto/settings.go](../../../backend/internal/handler/dto/settings.go) |
| 16 | source | [backend/internal/handler/openai_chat_completions.go](../../../backend/internal/handler/openai_chat_completions.go) |
| 03 | source | [backend/internal/handler/openai_gateway_handler.go](../../../backend/internal/handler/openai_gateway_handler.go) |
| 16 | test | [backend/internal/handler/openai_gateway_handler_test.go](../../../backend/internal/handler/openai_gateway_handler_test.go) |
| 16 | test | [backend/internal/handler/openai_gateway_ws_group_pricing_test.go](../../../backend/internal/handler/openai_gateway_ws_group_pricing_test.go) |
| 04 | source | [backend/internal/pkg/openai/oauth.go](../../../backend/internal/pkg/openai/oauth.go) |
| 04 | source | [backend/internal/repository/account_repo.go](../../../backend/internal/repository/account_repo.go) |
| 16 | source | [backend/internal/repository/http_upstream.go](../../../backend/internal/repository/http_upstream.go) |
| 04 | source | [backend/internal/repository/scheduler_cache.go](../../../backend/internal/repository/scheduler_cache.go) |
| 16 | test | [backend/internal/server/api_contract_test.go](../../../backend/internal/server/api_contract_test.go) |
| 16 | source | [backend/internal/server/routes/admin.go](../../../backend/internal/server/routes/admin.go) |
| 04 | source | [backend/internal/service/gateway_service.go](../../../backend/internal/service/gateway_service.go) |
| 16 | source | [backend/internal/service/http_upstream_profile.go](../../../backend/internal/service/http_upstream_profile.go) |
| 04 | source | [backend/internal/service/openai_account_scheduler.go](../../../backend/internal/service/openai_account_scheduler.go) |
| 04 | test | [backend/internal/service/openai_account_scheduler_test.go](../../../backend/internal/service/openai_account_scheduler_test.go) |
| 16 | source | [backend/internal/service/openai_codex_models_service.go](../../../backend/internal/service/openai_codex_models_service.go) |
| 16 | source | [backend/internal/service/openai_gateway_forward.go](../../../backend/internal/service/openai_gateway_forward.go) |
| 03 | source | [backend/internal/service/openai_gateway_service.go](../../../backend/internal/service/openai_gateway_service.go) |
| 16 | source | [backend/internal/service/openai_models_list.go](../../../backend/internal/service/openai_models_list.go) |
| 02 | source | [backend/internal/service/openai_oauth_service.go](../../../backend/internal/service/openai_oauth_service.go) |
| 16 | source | [backend/internal/service/openai_ws_forwarder.go](../../../backend/internal/service/openai_ws_forwarder.go) |
| 03 | source | [backend/internal/service/openai_ws_forwarder_ingress.go](../../../backend/internal/service/openai_ws_forwarder_ingress.go) |
| 03 | test | [backend/internal/service/openai_ws_forwarder_ingress_test.go](../../../backend/internal/service/openai_ws_forwarder_ingress_test.go) |
| 16 | source | [backend/internal/service/openai_ws_http_bridge.go](../../../backend/internal/service/openai_ws_http_bridge.go) |
| 16 | source | [backend/internal/service/openai_ws_session_preemption.go](../../../backend/internal/service/openai_ws_session_preemption.go) |
| 03 | source | [backend/internal/service/openai_ws_v2_passthrough_adapter.go](../../../backend/internal/service/openai_ws_v2_passthrough_adapter.go) |
| 16 | source | [backend/internal/service/ops_upstream_context.go](../../../backend/internal/service/ops_upstream_context.go) |
| 16 | source | [backend/internal/service/setting_parse.go](../../../backend/internal/service/setting_parse.go) |
| 16 | source | [backend/internal/service/setting_update.go](../../../backend/internal/service/setting_update.go) |
| 16 | source | [backend/internal/service/settings_view.go](../../../backend/internal/service/settings_view.go) |
| 16 | source | [backend/internal/service/wire.go](../../../backend/internal/service/wire.go) |
| 01 | migration | [backend/migrations/272_excel_bps_grants.sql](../../../backend/migrations/272_excel_bps_grants.sql) |
| 17 | documentation | [docs/FORK_FEATURE_RETIREMENT.md](../../../docs/FORK_FEATURE_RETIREMENT.md) |
| 17 | documentation | [docs/OPENAI_RESPONSES_RECOVERY.md](../../../docs/OPENAI_RESPONSES_RECOVERY.md) |
| 16 | source | [frontend/src/api/admin/settings.ts](../../../frontend/src/api/admin/settings.ts) |
| 04 | source | [frontend/src/components/account/BulkEditAccountModal.vue](../../../frontend/src/components/account/BulkEditAccountModal.vue) |
| 04 | source | [frontend/src/components/account/EditAccountModal.vue](../../../frontend/src/components/account/EditAccountModal.vue) |
| 04 | test | [frontend/src/components/account/__tests__/BulkEditAccountModal.spec.ts](../../../frontend/src/components/account/__tests__/BulkEditAccountModal.spec.ts) |
| 04 | test | [frontend/src/components/account/__tests__/EditAccountModal.spec.ts](../../../frontend/src/components/account/__tests__/EditAccountModal.spec.ts) |
| 14 | source | [frontend/src/i18n/locales/en/admin/accounts.ts](../../../frontend/src/i18n/locales/en/admin/accounts.ts) |
| 14 | source | [frontend/src/i18n/locales/en/admin/settings.ts](../../../frontend/src/i18n/locales/en/admin/settings.ts) |
| 14 | source | [frontend/src/i18n/locales/en/autoBPSOps.ts](../../../frontend/src/i18n/locales/en/autoBPSOps.ts) |
| 14 | source | [frontend/src/i18n/locales/en/index.ts](../../../frontend/src/i18n/locales/en/index.ts) |
| 14 | source | [frontend/src/i18n/locales/ja/admin/accounts.ts](../../../frontend/src/i18n/locales/ja/admin/accounts.ts) |
| 14 | source | [frontend/src/i18n/locales/ja/admin/settings.ts](../../../frontend/src/i18n/locales/ja/admin/settings.ts) |
| 14 | source | [frontend/src/i18n/locales/ja/autoBPSOps.ts](../../../frontend/src/i18n/locales/ja/autoBPSOps.ts) |
| 14 | source | [frontend/src/i18n/locales/ja/index.ts](../../../frontend/src/i18n/locales/ja/index.ts) |
| 14 | generated | [frontend/src/i18n/locales/zh-TW/admin/accounts.ts](../../../frontend/src/i18n/locales/zh-TW/admin/accounts.ts) |
| 14 | generated | [frontend/src/i18n/locales/zh-TW/admin/settings.ts](../../../frontend/src/i18n/locales/zh-TW/admin/settings.ts) |
| 14 | generated | [frontend/src/i18n/locales/zh-TW/autoBPSOps.ts](../../../frontend/src/i18n/locales/zh-TW/autoBPSOps.ts) |
| 14 | generated | [frontend/src/i18n/locales/zh-TW/index.ts](../../../frontend/src/i18n/locales/zh-TW/index.ts) |
| 14 | source | [frontend/src/i18n/locales/zh/admin/accounts.ts](../../../frontend/src/i18n/locales/zh/admin/accounts.ts) |
| 14 | source | [frontend/src/i18n/locales/zh/admin/settings.ts](../../../frontend/src/i18n/locales/zh/admin/settings.ts) |
| 14 | source | [frontend/src/i18n/locales/zh/autoBPSOps.ts](../../../frontend/src/i18n/locales/zh/autoBPSOps.ts) |
| 14 | source | [frontend/src/i18n/locales/zh/index.ts](../../../frontend/src/i18n/locales/zh/index.ts) |
| 04 | source | [frontend/src/views/admin/AccountsView.vue](../../../frontend/src/views/admin/AccountsView.vue) |
| 16 | source | [frontend/src/views/admin/SettingsView.vue](../../../frontend/src/views/admin/SettingsView.vue) |

## 2026-10-14 新增需求与实施边界

BPS 合并多值 anthropic-beta，Images 和 OpenAI Messages 使用既有入站诊断；独立 Excel 凭据、HTTP/WS 桥接、显式 403 恢复边界保持。
