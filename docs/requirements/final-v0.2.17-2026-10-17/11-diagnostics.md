# 11 · 降智检测、计费探测与定时诊断

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`feat(diagnostics): preserve metered probes and scheduled checks`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. 保留 Codex 单账号/多模型检测、可计费 Key 选择、定时检测、取消、最近记录、模板编辑和指纹刷新。统一使用 codex-diagnostic 命名，不恢复旧打票 URL/字段别名。

2. 检测通过原生 /v1/responses 发送真实计费请求，复用原 ScheduledTestService/Runner 和结果表。指定账号不可用时不换号代测；发送前复核权限及资格，异常中断不自动重放。

3. ModelTrace 只分析响应和随机挑战，不采集或绑定 Cookie/票据，不自动修改账号状态或业务模型路由；保留现有 80% 判定门槛和多实例互斥机制。

4. 保留 ModelTrace 许可证、指纹数据与历史结果；settings 中的 openai_codex_diagnostic_prompt_template 是唯一运行时模板键。指纹分析是统计信号，不是上游模型身份认证。

检测请求属于真实可计费流量；本次只运行本地测试，不执行真实提供方探测。检测结果、模板、取消与任务发起者权限完整保留，当前源码中的默认值及功能边界高于早期退役记录。

## 入口与配置归属

/admin/accounts 的降智检测与定时测试；复用现有管理接口和计划/结果表，不新增票据服务或 worker。

## 最终实现方案

1. ScheduledTestService/Runner、既有计划/结果表和网关实际计费请求承担诊断；指定账号不可用直接失败。

2. ModelTrace 仅分析响应/挑战，保留指纹来源与许可证；模板唯一键、80%门槛、任务互斥和取消授权复核不变。

3. 不采集 Cookie、不恢复打票，不因诊断结果自动改账号或业务路由。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

诊断任务取消、撤权、指定账号与模板/指纹统计已有回归；不发送真实计费探测。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| source | 20 | 2753 | 67 | 0 |
| test | 9 | 1403 | 0 | 0 |
| tooling | 3 | 29526 | 0 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 8 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| A | source | 177 | 0 | 11 | [backend/internal/handler/admin/codex_diagnostic_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/codex_diagnostic_handler.go) |
| A | test | 93 | 0 | 11 | [backend/internal/handler/admin/codex_diagnostic_handler_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/codex_diagnostic_handler_test.go) |
| A | test | 348 | 0 | 11 | [backend/internal/repository/scheduled_test_diagnostic_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/scheduled_test_diagnostic_integration_test.go) |
| A | source | 311 | 0 | 11 | [backend/internal/repository/scheduled_test_diagnostic_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/scheduled_test_diagnostic_repo.go) |
| M | source | 73 | 41 | 11 | [backend/internal/repository/scheduled_test_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/scheduled_test_repo.go) |
| A | test | 263 | 0 | 11 | [backend/internal/service/codex_diagnostic_gateway_policy_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/codex_diagnostic_gateway_policy_test.go) |
| A | source | 210 | 0 | 11 | [backend/internal/service/codex_diagnostic_probe.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/codex_diagnostic_probe.go) |
| A | source | 353 | 0 | 11 | [backend/internal/service/codex_fingerprint.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/codex_fingerprint.go) |
| A | test | 43 | 0 | 11 | [backend/internal/service/codex_fingerprint_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/codex_fingerprint_test.go) |
| A | source | 114 | 0 | 11 | [backend/internal/service/codex_fingerprint_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/codex_fingerprint_update.go) |
| A | test | 152 | 0 | 11 | [backend/internal/service/codex_probe_gateway_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/codex_probe_gateway_test.go) |
| A | source | 370 | 0 | 11 | [backend/internal/service/codex_probe_template.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/codex_probe_template.go) |
| A | source | 7 | 0 | 11 | [backend/internal/service/codex_probe_template.jsonl](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/codex_probe_template.jsonl) |
| A | test | 216 | 0 | 11 | [backend/internal/service/codex_probe_template_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/codex_probe_template_test.go) |
| A | source | 21 | 0 | 11 | [backend/internal/service/modeltrace_LICENSE](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/modeltrace_LICENSE) |
| A | source | 79 | 0 | 11 | [backend/internal/service/modeltrace_challenge.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/modeltrace_challenge.go) |
| A | test | 75 | 0 | 11 | [backend/internal/service/modeltrace_challenge_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/modeltrace_challenge_test.go) |
| A | tooling | 29460 | 0 | 11 | [backend/internal/service/modeltrace_unified_bank.json](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/modeltrace_unified_bank.json) |
| A | source | 45 | 0 | 11 | [backend/internal/service/scheduled_test_authorization.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/scheduled_test_authorization.go) |
| A | source | 437 | 0 | 11 | [backend/internal/service/scheduled_test_diagnostic.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/scheduled_test_diagnostic.go) |
| M | source | 19 | 12 | 11 | [backend/internal/service/scheduled_test_port.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/scheduled_test_port.go) |
| M | source | 25 | 5 | 11 | [backend/internal/service/scheduled_test_runner_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/scheduled_test_runner_service.go) |
| M | source | 30 | 9 | 11 | [backend/internal/service/scheduled_test_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/scheduled_test_service.go) |
| A | source | 50 | 0 | 11 | [backend/internal/service/setting_codex_probe_template.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/setting_codex_probe_template.go) |
| A | tooling | 65 | 0 | 11 | [backend/internal/service/testdata/modeltrace_challenges.json](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/testdata/modeltrace_challenges.json) |
| A | tooling | 1 | 0 | 11 | [backend/internal/service/testdata/modeltrace_gpt_reference.json](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/testdata/modeltrace_gpt_reference.json) |
| A | source | 100 | 0 | 11 | [frontend/src/api/admin/codexDiagnostics.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/admin/codexDiagnostics.ts) |
| A | source | 24 | 0 | 11 | [frontend/src/components/admin/account/CodexDiagnosticBadge.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/account/CodexDiagnosticBadge.vue) |
| A | source | 257 | 0 | 11 | [frontend/src/components/admin/account/CodexDiagnosticModal.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/account/CodexDiagnosticModal.vue) |
| A | source | 51 | 0 | 11 | [frontend/src/components/admin/account/CodexDiagnosticModelPicker.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/account/CodexDiagnosticModelPicker.vue) |
| A | test | 167 | 0 | 11 | [frontend/src/components/admin/account/__tests__/CodexDiagnosticModal.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/account/__tests__/CodexDiagnosticModal.spec.ts) |
| A | test | 46 | 0 | 11 | [frontend/src/components/admin/account/__tests__/CodexDiagnosticModelPicker.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/admin/account/__tests__/CodexDiagnosticModelPicker.spec.ts) |
