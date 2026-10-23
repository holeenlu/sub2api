# 07 · BPS、Excel 独立授权、工具、图片与原生回退

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`feat(bps): preserve final tools images recovery and native fallback`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. 保留用户明确恢复的 HTTP/SSE、compact、Messages/Chat 兼容入口及客户端 WS → 原生会话 owner → BPS HTTP/SSE 桥接。新安装的全局 BPS 和图片支持默认开启、图片传输默认 native；初始化不覆盖已有站点或账号开关。
2. 由全局设置、账号资格、映射后模型和独立 Excel grant 共同选路，沿用 Key、分组、客户端、并发与模型准入。Excel 单授权不得借用不存在的 Codex 凭据；缺 grant 明确报错。原生账号 WS 开关不作为 BPS 必要条件。
3. Excel 授权独立加密，校验身份和期限，刷新单飞、导入优先、旧 401 不撤销新授权；删除 grant 不删除主凭据。PKCE/state 及敏感操作权限保留。
4. function/custom/namespace/dynamic 工具、显式空 tools、tool_choice、工具目录继承和完整历史保持。独立命名外部输入不伪造成工具调用；有界传输纠错保留操作和原上下文，未知工具不默认静默丢弃。
5. 附件按每次客户端请求上传，同一请求内去重；不恢复跨请求上传缓存。文件归属错误保留真实 HTTP/逻辑状态，禁止自动换号重放，不污染账号健康或权限。诊断只保留安全计数与阶段，不记录文件 ID、图片 URL 或内容。
6. native 图片历史同时检查张数和解码后总字节。先完整校验请求及工具配对，再上传历史并 compact，之后复验压缩结果与新增图片，再上传新增图片并生成。单图 20 MiB、每阶段累计 32 MiB 等既有上限保留；不能静默丢图、降低分辨率或反复拆分绕过预算。
7. 附件上传 429 在当前 owner 下可按既有条件短等待、有界补试一次；两段上传、压缩和生成共享取消、首输出期限与尝试预算。压缩已完成时不能再声称 RequestNotSent；已报告压缩用量即使后续失败也记录一次。
8. 生成前明确 429、尚无语义输出且原始上下文可迁移时，双授权账号优先同账号原生 Codex 回退；其他可迁移请求交回原选号预算。密文、私有文件、previous_response_id、未配对工具结果或不确定工具执行不能因此跨账号迁移，已经输出不得重放。
9. BPS 冷却不污染原生 Codex 全局配额。有原生授权的账号可继续承接安全请求；Excel 单授权仍受 BPS 冷却过滤。HTTP 每请求判断，WS 成功回退后在该连接/账号固定 native，避免把原生密文送回 BPS。
10. cyber_policy 进入原生风控记录和会话屏蔽，沿用 TTL、白名单和用户/API Key/会话隔离；不因关闭诊断正文日志而失效。策略拒绝不触发换号、BPS 冷却或普通 403 关闭/移组。
11. 普通 HTTP 403、模型拒绝、流内失败、429 与过载分开处理；显式开启的 403 自动关闭、分组动作、恢复扫描、事务 claim/CAS 和探测规则继续保留。探测必须真的验证 BPS，不以原生回退结果宣称恢复。
12. HTTP/SSE/WS 共用完整终态、EOF、取消、部分用量、计费去重和冻结上游归属；补试计数、等待来源与停止原因仅记录有界脱敏证据。密文 assignment 错误包含可定位字段路径和明文恢复建议。
13. 可选 Redis 共享状态与实例内 relay 的可达性边界保留；工具共享不能解决跨实例文件归属。忽略历史密文仍默认关闭，自动登录 Worker、Mihomo、票据/Cookie 绑定及明确退役的运营能力不随历史重建恢复。

## 入口与配置归属

复用既有入口、配置和授权；具体落点见下方逐文件清单与关联功能组。

## 最终实现方案

1. 基于现有 basispoints 协议包、Excel grant 存储和原生 Responses owner 接入；恢复、尝试预算、计费及权限继续由原网关承担，不引入平行调度器。
2. 图片计划将历史和新增输入分阶段处理，并共享请求内去重与资源释放；实际发送证据决定可否原生回退，不能根据错误文案猜测上下文可迁移。
3. 账号开关、全局设置、403 控制与可观测性沿现有 settings 和 Extra 白名单；保留当前默认与已保存配置优先级。
4. 完整落点以 files-main.tsv 的第 07 组及 related_groups 为准。最新行为、来源和边界详见 ../../bps/REQUIREMENTS.md、../../bps/DUAL_PATH_REVIEW.md 和 ../../bps/upstream-changes.md。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

basispoints、Excel 授权、附件/compact、429/403/cyber、HTTP/WS 恢复和计费回归；真实授权、远端 relay 与付费调用不由本地单元测试代替。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 9 | 17114 | 0 | 0 |
| source | 112 | 14194 | 49 | 0 |
| test | 117 | 20972 | 7 | 0 |
| tooling | 1 | 79 | 0 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 76 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| A | source | 71 | 0 | 07 | [backend/internal/handler/admin/openai_excel_oauth_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/openai_excel_oauth_handler.go) |
| A | source | 33 | 0 | 07 | [backend/internal/handler/admin/setting_handler_excel_bps_defaults.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/setting_handler_excel_bps_defaults.go) |
| A | test | 45 | 0 | 07 | [backend/internal/handler/admin/setting_handler_excel_bps_defaults_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/setting_handler_excel_bps_defaults_test.go) |
| A | test | 205 | 0 | 07 | [backend/internal/handler/openai_excel_bps_cyber_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_excel_bps_cyber_test.go) |
| A | test | 210 | 0 | 07 | [backend/internal/handler/openai_excel_bps_error_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_excel_bps_error_test.go) |
| A | test | 705 | 0 | 07 | [backend/internal/handler/openai_excel_bps_failover_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_excel_bps_failover_test.go) |
| A | source | 7 | 0 | 07 | [backend/internal/handler/openai_excel_bps_image.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_excel_bps_image.go) |
| A | test | 270 | 0 | 07 | [backend/internal/handler/openai_excel_bps_image_admission_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_excel_bps_image_admission_test.go) |
| M | source | 8 | 1 | 06,07 | [backend/internal/handler/openai_images.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_images.go) |
| M | source | 43 | 13 | 07 | [backend/internal/pkg/httputil/body.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/httputil/body.go) |
| M | source | 7 | 6 | 07 | [backend/internal/pkg/openai/constants.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/openai/constants.go) |
| A | source | 74 | 0 | 07,12 | [backend/internal/repository/account_repo_excel_bps.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_excel_bps.go) |
| A | test | 107 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_auto_disable_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_excel_bps_auto_disable_test.go) |
| A | source | 129 | 0 | 07,12 | [backend/internal/repository/account_repo_excel_bps_groups.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_excel_bps_groups.go) |
| A | test | 287 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_groups_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_excel_bps_groups_integration_test.go) |
| A | test | 82 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_groups_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_excel_bps_groups_test.go) |
| A | test | 177 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_excel_bps_integration_test.go) |
| A | source | 109 | 0 | 07,12 | [backend/internal/repository/account_repo_excel_bps_recovery.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_excel_bps_recovery.go) |
| A | test | 178 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_recovery_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_excel_bps_recovery_integration_test.go) |
| A | source | 42 | 0 | 07 | [backend/internal/repository/account_repo_excel_bps_settings.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_excel_bps_settings.go) |
| A | source | 74 | 0 | 07,12 | [backend/internal/repository/account_repo_excel_credentials.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_excel_credentials.go) |
| A | test | 57 | 0 | 07 | [backend/internal/repository/account_repo_excel_credentials_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_excel_credentials_test.go) |
| M | test | 10 | 2 | 07,12 | [backend/internal/repository/account_repo_opencode_go_usage_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_opencode_go_usage_test.go) |
| A | source | 38 | 0 | 07 | [backend/internal/repository/excel_bps_image_policy_cache.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/excel_bps_image_policy_cache.go) |
| A | test | 63 | 0 | 07 | [backend/internal/repository/excel_bps_image_policy_cache_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/excel_bps_image_policy_cache_integration_test.go) |
| A | test | 62 | 0 | 07 | [backend/internal/repository/excel_bps_image_policy_cache_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/excel_bps_image_policy_cache_test.go) |
| A | source | 122 | 0 | 07 | [backend/internal/repository/gateway_cache_bps_state.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/gateway_cache_bps_state.go) |
| A | test | 75 | 0 | 07 | [backend/internal/repository/gateway_cache_bps_state_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/gateway_cache_bps_state_test.go) |
| A | source | 100 | 0 | 07 | [backend/internal/repository/http_upstream_bps.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/http_upstream_bps.go) |
| A | test | 153 | 0 | 07 | [backend/internal/repository/http_upstream_bps_eof_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/http_upstream_bps_eof_test.go) |
| A | test | 109 | 0 | 07 | [backend/internal/repository/http_upstream_bps_policy_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/http_upstream_bps_policy_test.go) |
| A | source | 41 | 0 | 07 | [backend/internal/server/middleware/excel_bps_image_admission.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/excel_bps_image_admission.go) |
| A | test | 69 | 0 | 07 | [backend/internal/server/middleware/excel_bps_image_admission_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/excel_bps_image_admission_test.go) |
| M | source | 15 | 1 | 07 | [backend/internal/server/middleware/request_logger.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/middleware/request_logger.go) |
| A | test | 68 | 0 | 07 | [backend/internal/server/routes/excel_bps_image_admission_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/routes/excel_bps_image_admission_test.go) |
| M | source | 8 | 2 | 07 | [backend/internal/server/routes/gateway.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/routes/gateway.go) |
| M | test | 2 | 2 | 07 | [backend/internal/server/routes/gateway_model_allowlist_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/routes/gateway_model_allowlist_test.go) |
| M | source | 137 | 2 | 07,12 | [backend/internal/service/account.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account.go) |
| A | source | 45 | 0 | 07 | [backend/internal/service/account_excel_bps_403_marker.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_excel_bps_403_marker.go) |
| A | test | 112 | 0 | 07 | [backend/internal/service/account_excel_bps_403_marker_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_excel_bps_403_marker_test.go) |
| A | source | 121 | 0 | 07 | [backend/internal/service/account_excel_bps_groups.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_excel_bps_groups.go) |
| A | source | 38 | 0 | 07 | [backend/internal/service/account_excel_bps_recovery_settings.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_excel_bps_recovery_settings.go) |
| A | test | 65 | 0 | 07 | [backend/internal/service/account_excel_bps_recovery_settings_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_excel_bps_recovery_settings_test.go) |
| A | source | 15 | 0 | 07 | [backend/internal/service/account_excel_bps_settings.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_excel_bps_settings.go) |
| A | source | 18 | 0 | 07 | [backend/internal/service/account_excel_bps_tools.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_excel_bps_tools.go) |
| A | source | 35 | 0 | 07 | [backend/internal/service/account_test_bps_evidence.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_test_bps_evidence.go) |
| A | test | 49 | 0 | 07 | [backend/internal/service/account_test_bps_evidence_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_test_bps_evidence_test.go) |
| M | source | 189 | 10 | 07,12,14 | [backend/internal/service/account_test_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_test_service.go) |
| A | source | 250 | 0 | 07 | [backend/internal/service/account_test_service_bps_probe.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_test_service_bps_probe.go) |
| A | test | 238 | 0 | 07 | [backend/internal/service/account_test_service_bps_probe_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_test_service_bps_probe_test.go) |
| A | test | 89 | 0 | 07 | [backend/internal/service/account_test_service_excel_bps_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/account_test_service_excel_bps_test.go) |
| M | source | 56 | 0 | 07 | [backend/internal/service/admin_account.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_account.go) |
| A | documentation | 104 | 0 | 07 | [backend/internal/service/basispoints/NOTICE.md](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/NOTICE.md) |
| A | documentation | 244 | 0 | 07 | [backend/internal/service/basispoints/README.md](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/README.md) |
| A | source | 85 | 0 | 07 | [backend/internal/service/basispoints/agent_assignment.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/agent_assignment.go) |
| A | test | 90 | 0 | 07 | [backend/internal/service/basispoints/agent_assignment_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/agent_assignment_test.go) |
| A | test | 177 | 0 | 07 | [backend/internal/service/basispoints/agent_message_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/agent_message_test.go) |
| A | source | 355 | 0 | 07 | [backend/internal/service/basispoints/attachments.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/attachments.go) |
| A | test | 311 | 0 | 07 | [backend/internal/service/basispoints/attachments_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/attachments_test.go) |
| A | test | 362 | 0 | 07 | [backend/internal/service/basispoints/basispoints_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/basispoints_test.go) |
| A | source | 144 | 0 | 07 | [backend/internal/service/basispoints/catalog.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/catalog.go) |
| A | source | 216 | 0 | 07 | [backend/internal/service/basispoints/catalog_cache.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/catalog_cache.go) |
| A | test | 67 | 0 | 07 | [backend/internal/service/basispoints/catalog_compact_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/catalog_compact_test.go) |
| A | source | 174 | 0 | 07 | [backend/internal/service/basispoints/catalog_items.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/catalog_items.go) |
| A | test | 199 | 0 | 07 | [backend/internal/service/basispoints/catalog_items_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/catalog_items_test.go) |
| A | test | 40 | 0 | 07 | [backend/internal/service/basispoints/catalog_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/catalog_test.go) |
| A | source | 70 | 0 | 07 | [backend/internal/service/basispoints/content.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/content.go) |
| A | test | 77 | 0 | 07 | [backend/internal/service/basispoints/content_diagnostics_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/content_diagnostics_test.go) |
| A | test | 28 | 0 | 07 | [backend/internal/service/basispoints/content_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/content_test.go) |
| A | test | 137 | 0 | 07 | [backend/internal/service/basispoints/custom_history_transport_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/custom_history_transport_test.go) |
| A | source | 31 | 0 | 07 | [backend/internal/service/basispoints/custom_transport.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/custom_transport.go) |
| A | test | 93 | 0 | 07 | [backend/internal/service/basispoints/custom_transport_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/custom_transport_test.go) |
| A | test | 115 | 0 | 07 | [backend/internal/service/basispoints/deep_audit_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/deep_audit_test.go) |
| A | test | 153 | 0 | 07 | [backend/internal/service/basispoints/deep_envelope_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/deep_envelope_test.go) |
| A | test | 169 | 0 | 07 | [backend/internal/service/basispoints/direct_call_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/direct_call_test.go) |
| A | source | 72 | 0 | 07 | [backend/internal/service/basispoints/encrypted_content.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/encrypted_content.go) |
| A | test | 107 | 0 | 07 | [backend/internal/service/basispoints/encrypted_content_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/encrypted_content_test.go) |
| A | source | 300 | 0 | 07 | [backend/internal/service/basispoints/envelope.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/envelope.go) |
| A | test | 189 | 0 | 07 | [backend/internal/service/basispoints/envelope_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/envelope_test.go) |
| A | source | 90 | 0 | 07 | [backend/internal/service/basispoints/function_cmd_transport.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/function_cmd_transport.go) |
| A | test | 370 | 0 | 07 | [backend/internal/service/basispoints/function_cmd_transport_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/function_cmd_transport_test.go) |
| A | source | 75 | 0 | 07 | [backend/internal/service/basispoints/function_code_transport.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/function_code_transport.go) |
| A | test | 264 | 0 | 07 | [backend/internal/service/basispoints/function_code_transport_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/function_code_transport_test.go) |
| A | test | 123 | 0 | 07 | [backend/internal/service/basispoints/history_collision_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/history_collision_test.go) |
| A | test | 83 | 0 | 07 | [backend/internal/service/basispoints/history_message_id_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/history_message_id_test.go) |
| A | test | 105 | 0 | 07 | [backend/internal/service/basispoints/history_message_images_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/history_message_images_test.go) |
| A | source | 146 | 0 | 07 | [backend/internal/service/basispoints/history_messages.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/history_messages.go) |
| A | test | 189 | 0 | 07 | [backend/internal/service/basispoints/history_messages_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/history_messages_test.go) |
| A | test | 250 | 0 | 07 | [backend/internal/service/basispoints/history_recovery_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/history_recovery_test.go) |
| A | source | 17 | 0 | 07 | [backend/internal/service/basispoints/image_capacity.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/image_capacity.go) |
| A | test | 78 | 0 | 07 | [backend/internal/service/basispoints/image_file_reference_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/image_file_reference_test.go) |
| A | source | 269 | 0 | 07 | [backend/internal/service/basispoints/image_policy.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/image_policy.go) |
| A | source | 236 | 0 | 07 | [backend/internal/service/basispoints/image_policy_stream.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/image_policy_stream.go) |
| A | test | 217 | 0 | 07 | [backend/internal/service/basispoints/image_policy_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/image_policy_test.go) |
| A | source | 492 | 0 | 07 | [backend/internal/service/basispoints/image_relay.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/image_relay.go) |
| A | test | 174 | 0 | 07 | [backend/internal/service/basispoints/image_relay_disk_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/image_relay_disk_test.go) |
| A | source | 86 | 0 | 07 | [backend/internal/service/basispoints/image_relay_limits.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/image_relay_limits.go) |
| A | test | 167 | 0 | 07 | [backend/internal/service/basispoints/image_relay_limits_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/image_relay_limits_test.go) |
| A | test | 410 | 0 | 07 | [backend/internal/service/basispoints/image_relay_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/image_relay_test.go) |
| A | source | 64 | 0 | 07 | [backend/internal/service/basispoints/images.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/images.go) |
| A | test | 57 | 0 | 07 | [backend/internal/service/basispoints/images_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/images_test.go) |
| A | test | 138 | 0 | 07 | [backend/internal/service/basispoints/invocation_recovery_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/invocation_recovery_test.go) |
| A | source | 113 | 0 | 07 | [backend/internal/service/basispoints/persistent_state.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/persistent_state.go) |
| A | source | 257 | 0 | 07 | [backend/internal/service/basispoints/plan.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/plan.go) |
| A | test | 222 | 0 | 07 | [backend/internal/service/basispoints/plan_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/plan_test.go) |
| A | source | 103 | 0 | 07 | [backend/internal/service/basispoints/progress_timeout.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/progress_timeout.go) |
| A | test | 93 | 0 | 07 | [backend/internal/service/basispoints/progress_timeout_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/progress_timeout_test.go) |
| A | test | 503 | 0 | 07 | [backend/internal/service/basispoints/protocol_completion_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/protocol_completion_test.go) |
| A | test | 30 | 0 | 07 | [backend/internal/service/basispoints/raw_custom_call_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/raw_custom_call_test.go) |
| A | test | 138 | 0 | 07 | [backend/internal/service/basispoints/replay_cache_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/replay_cache_test.go) |
| A | source | 249 | 0 | 07 | [backend/internal/service/basispoints/request.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/request.go) |
| A | test | 57 | 0 | 07 | [backend/internal/service/basispoints/request_compatibility_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/request_compatibility_test.go) |
| A | source | 47 | 0 | 07 | [backend/internal/service/basispoints/route.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/route.go) |
| A | test | 23 | 0 | 07 | [backend/internal/service/basispoints/route_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/route_test.go) |
| A | source | 446 | 0 | 07 | [backend/internal/service/basispoints/stream.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/stream.go) |
| A | test | 131 | 0 | 07 | [backend/internal/service/basispoints/stream_repair_boundary_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/stream_repair_boundary_test.go) |
| A | source | 199 | 0 | 07 | [backend/internal/service/basispoints/structured_output.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/structured_output.go) |
| A | test | 283 | 0 | 07 | [backend/internal/service/basispoints/structured_output_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/structured_output_test.go) |
| A | test | 68 | 0 | 07 | [backend/internal/service/basispoints/terminal_tools_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/terminal_tools_test.go) |
| A | test | 126 | 0 | 07 | [backend/internal/service/basispoints/tool_duplicates_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/tool_duplicates_test.go) |
| A | source | 75 | 0 | 07 | [backend/internal/service/basispoints/tool_examples.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/tool_examples.go) |
| A | test | 107 | 0 | 07 | [backend/internal/service/basispoints/tool_examples_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/tool_examples_test.go) |
| A | source | 45 | 0 | 07 | [backend/internal/service/basispoints/tool_images.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/tool_images.go) |
| A | test | 229 | 0 | 07 | [backend/internal/service/basispoints/tool_images_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/tool_images_test.go) |
| A | source | 499 | 0 | 07 | [backend/internal/service/basispoints/tool_repair.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/tool_repair.go) |
| A | test | 705 | 0 | 07 | [backend/internal/service/basispoints/tool_repair_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/tool_repair_test.go) |
| A | source | 92 | 0 | 07 | [backend/internal/service/basispoints/tool_schema.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/tool_schema.go) |
| A | source | 799 | 0 | 07 | [backend/internal/service/basispoints/tools.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/tools.go) |
| A | source | 272 | 0 | 07 | [backend/internal/service/basispoints/unknown_tool_repair.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/unknown_tool_repair.go) |
| A | source | 191 | 0 | 07 | [backend/internal/service/basispoints/upstream_failure.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/upstream_failure.go) |
| A | test | 290 | 0 | 07 | [backend/internal/service/basispoints/upstream_failure_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/basispoints/upstream_failure_test.go) |
| M | source | 8 | 1 | 07 | [backend/internal/service/grok_credential_failure.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/grok_credential_failure.go) |
| M | source | 6 | 0 | 07 | [backend/internal/service/openai_account_runtime_block_fastpath.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_account_runtime_block_fastpath.go) |
| M | source | 89 | 1 | 07 | [backend/internal/service/openai_bulk_account_settings.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_bulk_account_settings.go) |
| M | source | 11 | 1 | 07 | [backend/internal/service/openai_codex_model_metadata.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_codex_model_metadata.go) |
| M | source | 4 | 0 | 07 | [backend/internal/service/openai_compact_probe.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_compact_probe.go) |
| M | source | 4 | 0 | 07 | [backend/internal/service/openai_compact_stream_bridge.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_compact_stream_bridge.go) |
| A | source | 1218 | 0 | 07 | [backend/internal/service/openai_excel_bps.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps.go) |
| A | test | 93 | 0 | 07 | [backend/internal/service/openai_excel_bps_agent_assignment_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_agent_assignment_test.go) |
| A | test | 371 | 0 | 07 | [backend/internal/service/openai_excel_bps_attachment_diagnostics_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_attachment_diagnostics_test.go) |
| A | source | 256 | 0 | 07 | [backend/internal/service/openai_excel_bps_attachments.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_attachments.go) |
| A | test | 599 | 0 | 07 | [backend/internal/service/openai_excel_bps_attachments_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_attachments_test.go) |
| A | test | 178 | 0 | 07 | [backend/internal/service/openai_excel_bps_auto_disable_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_auto_disable_test.go) |
| A | test | 165 | 0 | 07 | [backend/internal/service/openai_excel_bps_billing_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_billing_test.go) |
| A | source | 15 | 0 | 07 | [backend/internal/service/openai_excel_bps_cancellation.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_cancellation.go) |
| A | test | 107 | 0 | 07 | [backend/internal/service/openai_excel_bps_cancellation_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_cancellation_test.go) |
| A | source | 196 | 0 | 07 | [backend/internal/service/openai_excel_bps_compat.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_compat.go) |
| A | test | 86 | 0 | 07 | [backend/internal/service/openai_excel_bps_compat_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_compat_test.go) |
| A | test | 125 | 0 | 07 | [backend/internal/service/openai_excel_bps_completion_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_completion_test.go) |
| A | source | 24 | 0 | 07 | [backend/internal/service/openai_excel_bps_encrypted.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_encrypted.go) |
| A | test | 92 | 0 | 07 | [backend/internal/service/openai_excel_bps_encrypted_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_encrypted_test.go) |
| A | test | 531 | 0 | 07 | [backend/internal/service/openai_excel_bps_failure_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_failure_test.go) |
| A | test | 197 | 0 | 07 | [backend/internal/service/openai_excel_bps_groups_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_groups_test.go) |
| A | test | 163 | 0 | 07 | [backend/internal/service/openai_excel_bps_history_messages_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_history_messages_test.go) |
| A | test | 120 | 0 | 07 | [backend/internal/service/openai_excel_bps_ignore_encrypted_content_e2e_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_ignore_encrypted_content_e2e_test.go) |
| A | source | 170 | 0 | 07 | [backend/internal/service/openai_excel_bps_image_admission.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_image_admission.go) |
| A | test | 333 | 0 | 07 | [backend/internal/service/openai_excel_bps_image_admission_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_image_admission_test.go) |
| A | source | 253 | 0 | 07 | [backend/internal/service/openai_excel_bps_image_policy.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_image_policy.go) |
| A | test | 809 | 0 | 07 | [backend/internal/service/openai_excel_bps_image_policy_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_image_policy_test.go) |
| A | test | 142 | 0 | 07 | [backend/internal/service/openai_excel_bps_image_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_image_test.go) |
| A | source | 390 | 0 | 07 | [backend/internal/service/openai_excel_bps_images.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_images.go) |
| A | test | 195 | 0 | 07 | [backend/internal/service/openai_excel_bps_images_e2e_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_images_e2e_test.go) |
| A | test | 464 | 0 | 07 | [backend/internal/service/openai_excel_bps_images_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_images_test.go) |
| A | source | 386 | 0 | 07 | [backend/internal/service/openai_excel_bps_model_access.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_model_access.go) |
| A | test | 178 | 0 | 07 | [backend/internal/service/openai_excel_bps_model_access_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_model_access_test.go) |
| A | source | 91 | 0 | 07 | [backend/internal/service/openai_excel_bps_model_cooldown.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_model_cooldown.go) |
| A | test | 114 | 0 | 07 | [backend/internal/service/openai_excel_bps_model_cooldown_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_model_cooldown_test.go) |
| A | source | 80 | 0 | 07 | [backend/internal/service/openai_excel_bps_models_manifest.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_models_manifest.go) |
| A | test | 133 | 0 | 07 | [backend/internal/service/openai_excel_bps_models_manifest_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_models_manifest_test.go) |
| A | test | 112 | 0 | 07 | [backend/internal/service/openai_excel_bps_models_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_models_test.go) |
| A | source | 20 | 0 | 07 | [backend/internal/service/openai_excel_bps_progress.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_progress.go) |
| A | test | 54 | 0 | 07 | [backend/internal/service/openai_excel_bps_progress_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_progress_test.go) |
| A | source | 191 | 0 | 07 | [backend/internal/service/openai_excel_bps_ratelimit.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_ratelimit.go) |
| A | test | 671 | 0 | 07 | [backend/internal/service/openai_excel_bps_ratelimit_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_ratelimit_test.go) |
| A | source | 294 | 0 | 07 | [backend/internal/service/openai_excel_bps_recovery.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_recovery.go) |
| A | test | 199 | 0 | 07 | [backend/internal/service/openai_excel_bps_recovery_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_recovery_test.go) |
| A | test | 410 | 0 | 07 | [backend/internal/service/openai_excel_bps_reliability_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_reliability_test.go) |
| A | test | 574 | 0 | 07 | [backend/internal/service/openai_excel_bps_repair_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_repair_test.go) |
| A | test | 39 | 0 | 07 | [backend/internal/service/openai_excel_bps_scheduler_health_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_scheduler_health_test.go) |
| A | test | 115 | 0 | 07 | [backend/internal/service/openai_excel_bps_structured_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_structured_test.go) |
| A | test | 302 | 0 | 07 | [backend/internal/service/openai_excel_bps_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_test.go) |
| A | test | 107 | 0 | 07 | [backend/internal/service/openai_excel_bps_tool_images_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_tool_images_test.go) |
| A | test | 141 | 0 | 07 | [backend/internal/service/openai_excel_bps_tools_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_tools_test.go) |
| A | source | 203 | 0 | 07,12 | [backend/internal/service/openai_excel_bps_transport.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_transport.go) |
| A | test | 12 | 0 | 07 | [backend/internal/service/openai_excel_bps_transport_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_transport_test.go) |
| A | source | 45 | 0 | 07 | [backend/internal/service/openai_excel_bps_usage.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_usage.go) |
| A | test | 129 | 0 | 07 | [backend/internal/service/openai_excel_bps_usage_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_bps_usage_test.go) |
| A | source | 402 | 0 | 07 | [backend/internal/service/openai_excel_oauth.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_oauth.go) |
| A | test | 189 | 0 | 07 | [backend/internal/service/openai_excel_oauth_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_excel_oauth_test.go) |
| M | source | 12 | 1 | 07,12 | [backend/internal/service/openai_gateway_chat_completions.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_chat_completions.go) |
| M | source | 13 | 2 | 07,12 | [backend/internal/service/openai_gateway_messages.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_messages.go) |
| M | source | 40 | 2 | 07,12 | [backend/internal/service/openai_gateway_scheduling.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_scheduling.go) |
| M | source | 8 | 3 | 07,12 | [backend/internal/service/openai_gateway_usage.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_usage.go) |
| M | source | 18 | 1 | 07,12 | [backend/internal/service/openai_images_responses.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_images_responses.go) |
| A | source | 157 | 0 | 07 | [backend/internal/service/openai_sse_read_pump.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_sse_read_pump.go) |
| A | test | 114 | 0 | 07 | [backend/internal/service/openai_sse_read_pump_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_sse_read_pump_test.go) |
| A | test | 401 | 0 | 07 | [backend/internal/service/openai_ws_bps_ingress_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_bps_ingress_test.go) |
| A | source | 269 | 0 | 07 | [backend/internal/service/openai_ws_excel_bps_bridge.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_excel_bps_bridge.go) |
| M | test | 15 | 2 | 07 | [backend/internal/service/openai_ws_session_preemption_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_ws_session_preemption_test.go) |
| A | source | 21 | 0 | 07 | [backend/internal/service/setting_excel_bps.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_excel_bps.go) |
| A | source | 101 | 0 | 07 | [backend/internal/service/setting_excel_bps_defaults.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_excel_bps_defaults.go) |
| A | test | 62 | 0 | 07 | [backend/internal/service/setting_excel_bps_defaults_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_excel_bps_defaults_test.go) |
| A | source | 218 | 0 | 07 | [backend/internal/service/setting_excel_bps_image.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_excel_bps_image.go) |
| A | test | 270 | 0 | 07 | [backend/internal/service/setting_excel_bps_image_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_excel_bps_image_test.go) |
| A | source | 67 | 0 | 07 | [backend/internal/util/transportdiag/error.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/util/transportdiag/error.go) |
| A | source | 109 | 0 | 07 | [backend/internal/util/transportdiag/trace.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/util/transportdiag/trace.go) |
| A | test | 72 | 0 | 07 | [backend/internal/util/transportdiag/trace_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/util/transportdiag/trace_test.go) |
| A | source | 17 | 0 | 01,07 | [backend/migrations/245_openai_bps_platform.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/245_openai_bps_platform.sql) |
| A | source | 7 | 0 | 01,07 | [backend/migrations/254_quality_bps_coexist.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/254_quality_bps_coexist.sql) |
| A | source | 25 | 0 | 01,07 | [backend/migrations/265_prepare_bps_provider_constraints.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/265_prepare_bps_provider_constraints.sql) |
| A | source | 119 | 0 | 01,07 | [backend/migrations/265_remove_bps_protocols.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/265_remove_bps_protocols.sql) |
| A | source | 7 | 0 | 01,07 | [backend/migrations/272_excel_bps_grants.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/272_excel_bps_grants.sql) |
| A | test | 22 | 0 | 01,07 | [backend/migrations/openai_bps_platform_migration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/openai_bps_platform_migration_test.go) |
| A | documentation | 131 | 0 | 07,20 | [docs/BPS_REMOVAL.md](https://github.com/holeenlu/sub2api/blob/main/docs/BPS_REMOVAL.md) |
| A | documentation | 147 | 0 | 07 | [docs/bps/DUAL_PATH_REVIEW.md](https://github.com/holeenlu/sub2api/blob/main/docs/bps/DUAL_PATH_REVIEW.md) |
| A | documentation | 524 | 0 | 07 | [docs/bps/REQUIREMENTS.md](https://github.com/holeenlu/sub2api/blob/main/docs/bps/REQUIREMENTS.md) |
| A | documentation | 465 | 0 | 07 | [docs/bps/VALIDATION.md](https://github.com/holeenlu/sub2api/blob/main/docs/bps/VALIDATION.md) |
| A | documentation | 1971 | 0 | 07 | [docs/bps/integration-state.json](https://github.com/holeenlu/sub2api/blob/main/docs/bps/integration-state.json) |
| A | documentation | 13371 | 0 | 07 | [docs/bps/source-inventory.json](https://github.com/holeenlu/sub2api/blob/main/docs/bps/source-inventory.json) |
| A | documentation | 157 | 0 | 07 | [docs/bps/upstream-changes.md](https://github.com/holeenlu/sub2api/blob/main/docs/bps/upstream-changes.md) |
| A | source | 11 | 0 | 07 | [frontend/src/api/admin/excelBPSDefaults.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/admin/excelBPSDefaults.ts) |
| M | source | 24 | 0 | 07 | [frontend/src/components/account/AccountStatusIndicator.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/AccountStatusIndicator.vue) |
| M | source | 10 | 2 | 07 | [frontend/src/components/account/AccountTestModal.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/AccountTestModal.vue) |
| A | source | 60 | 0 | 07 | [frontend/src/components/account/ExcelBPS403Badge.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/ExcelBPS403Badge.vue) |
| A | source | 85 | 0 | 07 | [frontend/src/components/account/ExcelBPSAuthorization.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/ExcelBPSAuthorization.vue) |
| A | source | 37 | 0 | 07 | [frontend/src/components/account/ExcelBPSModeSwitches.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/ExcelBPSModeSwitches.vue) |
| M | test | 9 | 1 | 07 | [frontend/src/components/account/__tests__/EditAccountModal.grokMediaEligibility.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/__tests__/EditAccountModal.grokMediaEligibility.spec.ts) |
| M | test | 4 | 0 | 07 | [frontend/src/components/account/__tests__/EditAccountModal.grokUpstream.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/__tests__/EditAccountModal.grokUpstream.spec.ts) |
| A | test | 161 | 0 | 07 | [frontend/src/components/account/__tests__/ExcelBPS403Badge.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/__tests__/ExcelBPS403Badge.spec.ts) |
| A | test | 48 | 0 | 07 | [frontend/src/components/account/__tests__/ExcelBPSAuthorization.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/__tests__/ExcelBPSAuthorization.spec.ts) |
| A | source | 63 | 0 | 07 | [frontend/src/components/admin/operations/BPSDefaultsCard.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/operations/BPSDefaultsCard.vue) |
| A | source | 51 | 0 | 07 | [frontend/src/components/admin/operations/BPSDefaultsPanel.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/operations/BPSDefaultsPanel.vue) |
| A | test | 114 | 0 | 07 | [frontend/src/composables/__tests__/useExcelBPSDefaults.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/composables/__tests__/useExcelBPSDefaults.spec.ts) |
| A | source | 62 | 0 | 07 | [frontend/src/composables/useExcelBPSDefaults.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/composables/useExcelBPSDefaults.ts) |
| M | source | 2 | 0 | 07 | [frontend/src/constants/account.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/constants/account.ts) |
| A | source | 45 | 0 | 07 | [frontend/src/utils/excelBPSDefaults.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/utils/excelBPSDefaults.ts) |
| A | source | 35 | 0 | 07 | [frontend/src/utils/excelBPSImageLimits.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/utils/excelBPSImageLimits.ts) |
| A | source | 11 | 0 | 07 | [frontend/src/utils/excelBPSRecovery.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/utils/excelBPSRecovery.ts) |
| A | tooling | 79 | 0 | 07 | [scripts/bps/upstream-review.py](https://github.com/holeenlu/sub2api/blob/main/scripts/bps/upstream-review.py) |
