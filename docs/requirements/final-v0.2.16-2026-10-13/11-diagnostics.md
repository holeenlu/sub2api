# 11 · Codex 降智检测与定时诊断

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。

## 最终需求

1. 保留 Codex 单账号/多模型检测、可计费 Key 选择、定时检测、取消、最近记录、模板编辑和指纹刷新。统一使用 codex-diagnostic 命名，不恢复旧打票 URL/字段别名。

2. 检测通过原生 /v1/responses 发送真实计费请求，复用原 ScheduledTestService/Runner 和结果表。指定账号不可用时不换号代测；发送前复核权限及资格，异常中断不自动重放。

3. ModelTrace 只分析响应和随机挑战，不采集或绑定 Cookie/票据，不自动修改账号状态或业务模型路由；保留现有 80% 判定门槛和多实例互斥机制。

4. 保留 ModelTrace 许可证、指纹数据与历史结果；settings 中的 openai_codex_diagnostic_prompt_template 是唯一运行时模板键。指纹分析是统计信号，不是上游模型身份认证。

## 入口与配置归属

/admin/accounts 的降智检测与定时测试；复用现有管理接口和计划/结果表，不新增票据服务或 worker。

## 最终实现方案

1. ScheduledTestService/Runner、既有计划/结果表和网关实际计费请求承担诊断；指定账号不可用直接失败。

2. ModelTrace 仅分析响应/挑战，保留指纹来源与许可证；模板唯一键、80%门槛、任务互斥和取消授权复核不变。

3. 不采集 Cookie、不恢复打票，不因诊断结果自动改账号或业务路由。

## 边界、失败路径与依赖

没有额外文件归属组关联。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。上面描述现有最终行为，本次不重新实现业务。

## 验收要求

诊断任务取消、撤权、指定账号与模板/指纹统计已有回归；不发送真实计费探测。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | --- | --- | --- | --- |
| asset | 1 | 7 | 0 | 0 |
| documentation | 1 | 21 | 0 | 0 |
| source | 18 | 2725 | 67 | 0 |
| test | 11 | 1466 | 0 | 0 |
| tooling | 1 | 29460 | 0 | 0 |

这是相对 v0.2.16 的累计定制差异，不是本次新写代码。删除/新增重命名分别计数；生成代码与测试单列。

## 来源提交

- `0d5ef648ec39915cfa09d560ae3c9fe43f866826`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | --- | --- | --- | --- |
| A | source | 177 | 0 | 11 | [backend/internal/handler/admin/codex_diagnostic_handler.go](../../../backend/internal/handler/admin/codex_diagnostic_handler.go) |
| A | test | 93 | 0 | 11 | [backend/internal/handler/admin/codex_diagnostic_handler_test.go](../../../backend/internal/handler/admin/codex_diagnostic_handler_test.go) |
| A | test | 348 | 0 | 11 | [backend/internal/repository/scheduled_test_diagnostic_integration_test.go](../../../backend/internal/repository/scheduled_test_diagnostic_integration_test.go) |
| A | source | 311 | 0 | 11 | [backend/internal/repository/scheduled_test_diagnostic_repo.go](../../../backend/internal/repository/scheduled_test_diagnostic_repo.go) |
| M | source | 73 | 41 | 11 | [backend/internal/repository/scheduled_test_repo.go](../../../backend/internal/repository/scheduled_test_repo.go) |
| A | test | 260 | 0 | 11 | [backend/internal/service/codex_diagnostic_gateway_policy_test.go](../../../backend/internal/service/codex_diagnostic_gateway_policy_test.go) |
| A | source | 210 | 0 | 11 | [backend/internal/service/codex_diagnostic_probe.go](../../../backend/internal/service/codex_diagnostic_probe.go) |
| A | source | 353 | 0 | 11 | [backend/internal/service/codex_fingerprint.go](../../../backend/internal/service/codex_fingerprint.go) |
| A | test | 43 | 0 | 11 | [backend/internal/service/codex_fingerprint_test.go](../../../backend/internal/service/codex_fingerprint_test.go) |
| A | source | 114 | 0 | 11 | [backend/internal/service/codex_fingerprint_update.go](../../../backend/internal/service/codex_fingerprint_update.go) |
| A | test | 152 | 0 | 11 | [backend/internal/service/codex_probe_gateway_test.go](../../../backend/internal/service/codex_probe_gateway_test.go) |
| A | source | 370 | 0 | 11 | [backend/internal/service/codex_probe_template.go](../../../backend/internal/service/codex_probe_template.go) |
| A | asset | 7 | 0 | 11 | [backend/internal/service/codex_probe_template.jsonl](../../../backend/internal/service/codex_probe_template.jsonl) |
| A | test | 216 | 0 | 11 | [backend/internal/service/codex_probe_template_test.go](../../../backend/internal/service/codex_probe_template_test.go) |
| A | documentation | 21 | 0 | 11 | [backend/internal/service/modeltrace_LICENSE](../../../backend/internal/service/modeltrace_LICENSE) |
| A | source | 79 | 0 | 11 | [backend/internal/service/modeltrace_challenge.go](../../../backend/internal/service/modeltrace_challenge.go) |
| A | test | 75 | 0 | 11 | [backend/internal/service/modeltrace_challenge_test.go](../../../backend/internal/service/modeltrace_challenge_test.go) |
| A | tooling | 29460 | 0 | 11 | [backend/internal/service/modeltrace_unified_bank.json](../../../backend/internal/service/modeltrace_unified_bank.json) |
| A | source | 45 | 0 | 11 | [backend/internal/service/scheduled_test_authorization.go](../../../backend/internal/service/scheduled_test_authorization.go) |
| A | source | 437 | 0 | 11 | [backend/internal/service/scheduled_test_diagnostic.go](../../../backend/internal/service/scheduled_test_diagnostic.go) |
| M | source | 19 | 12 | 11 | [backend/internal/service/scheduled_test_port.go](../../../backend/internal/service/scheduled_test_port.go) |
| M | source | 25 | 5 | 11 | [backend/internal/service/scheduled_test_runner_service.go](../../../backend/internal/service/scheduled_test_runner_service.go) |
| M | source | 30 | 9 | 11 | [backend/internal/service/scheduled_test_service.go](../../../backend/internal/service/scheduled_test_service.go) |
| A | source | 50 | 0 | 11 | [backend/internal/service/setting_codex_probe_template.go](../../../backend/internal/service/setting_codex_probe_template.go) |
| A | test | 65 | 0 | 11 | [backend/internal/service/testdata/modeltrace_challenges.json](../../../backend/internal/service/testdata/modeltrace_challenges.json) |
| A | test | 1 | 0 | 11 | [backend/internal/service/testdata/modeltrace_gpt_reference.json](../../../backend/internal/service/testdata/modeltrace_gpt_reference.json) |
| A | source | 100 | 0 | 11 | [frontend/src/api/admin/codexDiagnostics.ts](../../../frontend/src/api/admin/codexDiagnostics.ts) |
| A | source | 24 | 0 | 11 | [frontend/src/components/admin/account/CodexDiagnosticBadge.vue](../../../frontend/src/components/admin/account/CodexDiagnosticBadge.vue) |
| A | source | 257 | 0 | 11 | [frontend/src/components/admin/account/CodexDiagnosticModal.vue](../../../frontend/src/components/admin/account/CodexDiagnosticModal.vue) |
| A | source | 51 | 0 | 11 | [frontend/src/components/admin/account/CodexDiagnosticModelPicker.vue](../../../frontend/src/components/admin/account/CodexDiagnosticModelPicker.vue) |
| A | test | 167 | 0 | 11 | [frontend/src/components/admin/account/__tests__/CodexDiagnosticModal.spec.ts](../../../frontend/src/components/admin/account/__tests__/CodexDiagnosticModal.spec.ts) |
| A | test | 46 | 0 | 11 | [frontend/src/components/admin/account/__tests__/CodexDiagnosticModelPicker.spec.ts](../../../frontend/src/components/admin/account/__tests__/CodexDiagnosticModelPicker.spec.ts) |
