# 07 · Codex 降智检测与定时诊断

归档日期：2026-10-07。文件快照：main `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238`；官方比较基线 `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`。

本页登记最终需求与实际文件归属；不把本轮提交整理等同于重新实现或完整安全审计。

## 最终需求

1. 保留 Codex 单账号/多模型检测、可计费 Key 选择、定时检测、取消、最近记录、模板编辑和指纹刷新。统一使用 codex-diagnostic 命名，不恢复旧打票 URL/字段别名。

2. 检测通过原生 /v1/responses 发送真实计费请求，复用原 ScheduledTestService/Runner 和结果表。指定账号不可用时不换号代测；发送前复核权限及资格，异常中断不自动重放。

3. ModelTrace 只分析响应和随机挑战，不采集或绑定 Cookie/票据，不自动修改账号状态或业务模型路由；保留现有 80% 判定门槛和多实例互斥机制。

4. 保留 ModelTrace 许可证、指纹数据与历史结果；settings 中的 openai_codex_diagnostic_prompt_template 是唯一运行时模板键。指纹分析是统计信号，不是上游模型身份认证。

## 入口与配置归属

/admin/accounts 的降智检测与定时测试；复用现有管理接口和计划/结果表，不新增票据服务或 worker。

## 验收边界

网关资格/真实扣费边界、指定账号失败不回退、任务取消/撤权、模板渲染、指纹分类和仓储生命周期；外部指纹源固定提交仍须独立跟踪，不能把历史审计建议写成已实现。

验证状态统一见 [总说明](README.md#验证和未关闭边界)。本页列的是验收要求，不是宣称本轮已重新运行这些检查。

## 对比统计

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 业务代码 | 18 | 2725 | 67 | 0 |
| 测试/夹具 | 11 | 1466 | 0 | 0 |
| 运行时指纹/模板数据 | 2 | 29467 | 0 | 0 |
| 已有文档/许可 | 1 | 21 | 0 | 0 |

统计为最终文件相对固定官方基线的累计差异，并非本次整理新写的代码。对重命名统一展开为删除/新增；跨域文件只设一个主归属。

## 修改文件

| 状态 | 类型 | +行 | −行 | 文件 |
| --- | --- | ---: | ---: | --- |
| A | 业务代码 | 177 | 0 | [backend/internal/handler/admin/codex_diagnostic_handler.go](../../../backend/internal/handler/admin/codex_diagnostic_handler.go) |
| A | 测试/夹具 | 93 | 0 | [backend/internal/handler/admin/codex_diagnostic_handler_test.go](../../../backend/internal/handler/admin/codex_diagnostic_handler_test.go) |
| A | 测试/夹具 | 348 | 0 | [backend/internal/repository/scheduled_test_diagnostic_integration_test.go](../../../backend/internal/repository/scheduled_test_diagnostic_integration_test.go) |
| A | 业务代码 | 311 | 0 | [backend/internal/repository/scheduled_test_diagnostic_repo.go](../../../backend/internal/repository/scheduled_test_diagnostic_repo.go) |
| M | 业务代码 | 73 | 41 | [backend/internal/repository/scheduled_test_repo.go](../../../backend/internal/repository/scheduled_test_repo.go) |
| A | 测试/夹具 | 260 | 0 | [backend/internal/service/codex_diagnostic_gateway_policy_test.go](../../../backend/internal/service/codex_diagnostic_gateway_policy_test.go) |
| A | 业务代码 | 210 | 0 | [backend/internal/service/codex_diagnostic_probe.go](../../../backend/internal/service/codex_diagnostic_probe.go) |
| A | 业务代码 | 353 | 0 | [backend/internal/service/codex_fingerprint.go](../../../backend/internal/service/codex_fingerprint.go) |
| A | 测试/夹具 | 43 | 0 | [backend/internal/service/codex_fingerprint_test.go](../../../backend/internal/service/codex_fingerprint_test.go) |
| A | 业务代码 | 114 | 0 | [backend/internal/service/codex_fingerprint_update.go](../../../backend/internal/service/codex_fingerprint_update.go) |
| A | 测试/夹具 | 152 | 0 | [backend/internal/service/codex_probe_gateway_test.go](../../../backend/internal/service/codex_probe_gateway_test.go) |
| A | 业务代码 | 370 | 0 | [backend/internal/service/codex_probe_template.go](../../../backend/internal/service/codex_probe_template.go) |
| A | 运行时指纹/模板数据 | 7 | 0 | [backend/internal/service/codex_probe_template.jsonl](../../../backend/internal/service/codex_probe_template.jsonl) |
| A | 测试/夹具 | 216 | 0 | [backend/internal/service/codex_probe_template_test.go](../../../backend/internal/service/codex_probe_template_test.go) |
| A | 已有文档/许可 | 21 | 0 | [backend/internal/service/modeltrace_LICENSE](../../../backend/internal/service/modeltrace_LICENSE) |
| A | 业务代码 | 79 | 0 | [backend/internal/service/modeltrace_challenge.go](../../../backend/internal/service/modeltrace_challenge.go) |
| A | 测试/夹具 | 75 | 0 | [backend/internal/service/modeltrace_challenge_test.go](../../../backend/internal/service/modeltrace_challenge_test.go) |
| A | 运行时指纹/模板数据 | 29460 | 0 | [backend/internal/service/modeltrace_unified_bank.json](../../../backend/internal/service/modeltrace_unified_bank.json) |
| A | 业务代码 | 45 | 0 | [backend/internal/service/scheduled_test_authorization.go](../../../backend/internal/service/scheduled_test_authorization.go) |
| A | 业务代码 | 437 | 0 | [backend/internal/service/scheduled_test_diagnostic.go](../../../backend/internal/service/scheduled_test_diagnostic.go) |
| M | 业务代码 | 19 | 12 | [backend/internal/service/scheduled_test_port.go](../../../backend/internal/service/scheduled_test_port.go) |
| M | 业务代码 | 25 | 5 | [backend/internal/service/scheduled_test_runner_service.go](../../../backend/internal/service/scheduled_test_runner_service.go) |
| M | 业务代码 | 30 | 9 | [backend/internal/service/scheduled_test_service.go](../../../backend/internal/service/scheduled_test_service.go) |
| A | 业务代码 | 50 | 0 | [backend/internal/service/setting_codex_probe_template.go](../../../backend/internal/service/setting_codex_probe_template.go) |
| A | 测试/夹具 | 65 | 0 | [backend/internal/service/testdata/modeltrace_challenges.json](../../../backend/internal/service/testdata/modeltrace_challenges.json) |
| A | 测试/夹具 | 1 | 0 | [backend/internal/service/testdata/modeltrace_gpt_reference.json](../../../backend/internal/service/testdata/modeltrace_gpt_reference.json) |
| A | 业务代码 | 100 | 0 | [frontend/src/api/admin/codexDiagnostics.ts](../../../frontend/src/api/admin/codexDiagnostics.ts) |
| A | 业务代码 | 24 | 0 | [frontend/src/components/admin/account/CodexDiagnosticBadge.vue](../../../frontend/src/components/admin/account/CodexDiagnosticBadge.vue) |
| A | 业务代码 | 257 | 0 | [frontend/src/components/admin/account/CodexDiagnosticModal.vue](../../../frontend/src/components/admin/account/CodexDiagnosticModal.vue) |
| A | 业务代码 | 51 | 0 | [frontend/src/components/admin/account/CodexDiagnosticModelPicker.vue](../../../frontend/src/components/admin/account/CodexDiagnosticModelPicker.vue) |
| A | 测试/夹具 | 167 | 0 | [frontend/src/components/admin/account/__tests__/CodexDiagnosticModal.spec.ts](../../../frontend/src/components/admin/account/__tests__/CodexDiagnosticModal.spec.ts) |
| A | 测试/夹具 | 46 | 0 | [frontend/src/components/admin/account/__tests__/CodexDiagnosticModelPicker.spec.ts](../../../frontend/src/components/admin/account/__tests__/CodexDiagnosticModelPicker.spec.ts) |
