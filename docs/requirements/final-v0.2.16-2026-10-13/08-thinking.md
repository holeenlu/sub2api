# 08 · 最终模型能力、推理参数与协议语义

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。


2026-10-14 增量归并：本页已纳入通过验收的 New API 实现，来源和逐文件归属见 [归并分析](NEW-API-INTEGRATION-2026-10-14.md)。原快照 SHA 仅代表 2026-10-13 历史输入。

## 最终需求

1. 根据映射后、实际发往上游的模型能力规范化 thinking mode、effort、budget；账号回退必须保留原始请求意图，不能在第一尝试就不可逆修改共享请求。
2. Claude 原生 Messages、OpenAI 兼容转换、Bedrock 使用同一能力规则；保留有签名历史，省略的字段仍表达供应商默认，不为了统一而强行补值。
3. Fable 对不支持的 disabled 保持严格拒绝；Sonnet 的 between-tools 转换明确执行；Opus 5 历史 effort 和 Sonnet 5.5 省略 effort 采用当前已通过验收的规则。
4. ingress effort policy 只执行一次；手工模型的低预算、输出上限错误保持文档中的显式限制，不能自动改变用户模型来掩盖参数不支持。
5. Gemini 在本协议内归一 thinking，不将 Claude 参数语义机械套用；兼容路径保留原重试、计费、归属和 BPS 恢复契约。
6. New API 只作为固定来源研究与语义适配依据。2026-10-13 更新吸收规则覆盖整个网关价值与既有缺口；本次已纳入 G03–G06/G08 的对应实现，其他落点在关联功能组登记。

## 入口与配置归属

沿用下列方案和完整文件清单中的现有入口。

## 最终实现方案

1. 保留既有 Claude capability/effort 规则模块和各协议最后转换位置；入口只保留意图，最终模型处进行归一。

2. Claude/Gemini 各自沿协议输出，fallback 使用请求副本，签名历史和未指定默认值不被结构性重写。

3. 研究、采用方案、实际验证分开保存；New API 整仓参考不等于整分支导入，不恢复退役能力。

## 边界、失败路径与依赖

关联功能组：06。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。此组与关联组共同形成完整运行行为；本次归并不额外改写已验收的业务源码。

## 验收要求

Claude/Gemini thinking、最终模型、fallback 副本与计价标识；历史省略/disabled/budget 回归。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 3 | 1114 | 0 | 0 |
| source | 28 | 1360 | 290 | 0 |
| test | 17 | 1045 | 137 | 0 |

相对官方 v0.2.16 的累计差异，已包含本批实现；不是本次归并额外编写的代码。

## 来源提交

- `49b29ef158e6319cfd6b1227781307de5f8f870e`
- `4c5aa7b49988f5752b01940475530832c188b7ad`
- `71fc12f8edf4cbb839a72ba5f06c86745a1f2612`
- `944fdb1eff8e7ea33ed70ff722b6822f962e9255`
- `c6a92e1501a3ed5bb405c02bd541fcb59366bd28`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 6 | 0 | 08 | [backend/internal/handler/composite_platform.go](../../../backend/internal/handler/composite_platform.go) |
| M | test | 72 | 0 | 08 | [backend/internal/handler/composite_platform_test.go](../../../backend/internal/handler/composite_platform_test.go) |
| M | test | 55 | 2 | 08 | [backend/internal/pkg/apicompat/anthropic_responses_test.go](../../../backend/internal/pkg/apicompat/anthropic_responses_test.go) |
| M | source | 9 | 8 | 08 | [backend/internal/pkg/apicompat/chatcompletions_to_responses.go](../../../backend/internal/pkg/apicompat/chatcompletions_to_responses.go) |
| A | source | 22 | 0 | 08 | [backend/internal/pkg/apicompat/conversion_diagnostics.go](../../../backend/internal/pkg/apicompat/conversion_diagnostics.go) |
| A | source | 106 | 0 | 08 | [backend/internal/pkg/apicompat/responses_request_fidelity.go](../../../backend/internal/pkg/apicompat/responses_request_fidelity.go) |
| M | test | 33 | 0 | 08 | [backend/internal/pkg/apicompat/responses_to_anthropic_cc_chain_test.go](../../../backend/internal/pkg/apicompat/responses_to_anthropic_cc_chain_test.go) |
| M | source | 109 | 62 | 08 | [backend/internal/pkg/apicompat/responses_to_anthropic_request.go](../../../backend/internal/pkg/apicompat/responses_to_anthropic_request.go) |
| M | source | 36 | 30 | 08 | [backend/internal/pkg/apicompat/types.go](../../../backend/internal/pkg/apicompat/types.go) |
| M | source | 69 | 33 | 08 | [backend/internal/pkg/claude/effort_catalog.go](../../../backend/internal/pkg/claude/effort_catalog.go) |
| M | test | 16 | 0 | 08 | [backend/internal/pkg/claude/effort_catalog_test.go](../../../backend/internal/pkg/claude/effort_catalog_test.go) |
| A | source | 150 | 0 | 08 | [backend/internal/pkg/claude/thinking.go](../../../backend/internal/pkg/claude/thinking.go) |
| A | test | 94 | 0 | 08 | [backend/internal/pkg/claude/thinking_test.go](../../../backend/internal/pkg/claude/thinking_test.go) |
| M | source | 29 | 6 | 06,08 | [backend/internal/service/antigravity_gateway_compat.go](../../../backend/internal/service/antigravity_gateway_compat.go) |
| M | test | 117 | 6 | 06,08 | [backend/internal/service/antigravity_gateway_compat_test.go](../../../backend/internal/service/antigravity_gateway_compat_test.go) |
| M | source | 19 | 48 | 08 | [backend/internal/service/bedrock_request.go](../../../backend/internal/service/bedrock_request.go) |
| M | test | 11 | 51 | 08 | [backend/internal/service/bedrock_request_test.go](../../../backend/internal/service/bedrock_request_test.go) |
| M | source | 1 | 1 | 08 | [backend/internal/service/claude_code_validator.go](../../../backend/internal/service/claude_code_validator.go) |
| M | test | 11 | 13 | 08 | [backend/internal/service/claude_opus5_test.go](../../../backend/internal/service/claude_opus5_test.go) |
| A | source | 32 | 0 | 08 | [backend/internal/service/conversion_diagnostics.go](../../../backend/internal/service/conversion_diagnostics.go) |
| M | test | 1 | 3 | 08 | [backend/internal/service/gateway_anthropic_apikey_passthrough_test.go](../../../backend/internal/service/gateway_anthropic_apikey_passthrough_test.go) |
| M | source | 9 | 3 | 08 | [backend/internal/service/gateway_anthropic_passthrough.go](../../../backend/internal/service/gateway_anthropic_passthrough.go) |
| M | source | 7 | 3 | 08 | [backend/internal/service/gateway_bedrock.go](../../../backend/internal/service/gateway_bedrock.go) |
| M | test | 3 | 1 | 08 | [backend/internal/service/gateway_compat_reasoning_pricing_test.go](../../../backend/internal/service/gateway_compat_reasoning_pricing_test.go) |
| M | source | 9 | 6 | 08 | [backend/internal/service/gateway_count_tokens.go](../../../backend/internal/service/gateway_count_tokens.go) |
| M | source | 10 | 4 | 08 | [backend/internal/service/gateway_forward_as_chat_completions.go](../../../backend/internal/service/gateway_forward_as_chat_completions.go) |
| M | source | 10 | 4 | 08 | [backend/internal/service/gateway_forward_as_responses.go](../../../backend/internal/service/gateway_forward_as_responses.go) |
| M | source | 13 | 4 | 08 | [backend/internal/service/gateway_request.go](../../../backend/internal/service/gateway_request.go) |
| A | test | 218 | 0 | 08 | [backend/internal/service/gateway_thinking_normalization_test.go](../../../backend/internal/service/gateway_thinking_normalization_test.go) |
| M | test | 51 | 1 | 08 | [backend/internal/service/gateway_tool_changes_beta_test.go](../../../backend/internal/service/gateway_tool_changes_beta_test.go) |
| M | source | 11 | 6 | 08 | [backend/internal/service/gateway_upstream_request.go](../../../backend/internal/service/gateway_upstream_request.go) |
| M | source | 66 | 10 | 06,08 | [backend/internal/service/gemini_chat_completions_compat_service.go](../../../backend/internal/service/gemini_chat_completions_compat_service.go) |
| M | test | 117 | 0 | 06,08 | [backend/internal/service/gemini_chat_completions_compat_service_test.go](../../../backend/internal/service/gemini_chat_completions_compat_service_test.go) |
| M | source | 144 | 49 | 06,08 | [backend/internal/service/gemini_messages_compat_service.go](../../../backend/internal/service/gemini_messages_compat_service.go) |
| M | test | 116 | 0 | 06,08 | [backend/internal/service/gemini_messages_compat_service_test.go](../../../backend/internal/service/gemini_messages_compat_service_test.go) |
| M | test | 47 | 4 | 08 | [backend/internal/service/gemini_native_reasoning_pricing_test.go](../../../backend/internal/service/gemini_native_reasoning_pricing_test.go) |
| A | source | 124 | 0 | 06,08 | [backend/internal/service/gemini_schema_capabilities.go](../../../backend/internal/service/gemini_schema_capabilities.go) |
| A | source | 186 | 0 | 08 | [backend/internal/service/gemini_thinking.go](../../../backend/internal/service/gemini_thinking.go) |
| A | test | 75 | 0 | 08 | [backend/internal/service/gemini_thinking_test.go](../../../backend/internal/service/gemini_thinking_test.go) |
| M | source | 38 | 0 | 08 | [backend/internal/service/header_util.go](../../../backend/internal/service/header_util.go) |
| M | source | 8 | 0 | 08 | [backend/internal/service/openai_gateway_chat_completions_anthropic_native.go](../../../backend/internal/service/openai_gateway_chat_completions_anthropic_native.go) |
| M | source | 13 | 12 | 08 | [backend/internal/service/openai_gateway_messages_anthropic_native.go](../../../backend/internal/service/openai_gateway_messages_anthropic_native.go) |
| M | test | 8 | 56 | 06,08 | [backend/internal/service/openai_gateway_request_body_reasoning_test.go](../../../backend/internal/service/openai_gateway_request_body_reasoning_test.go) |
| M | source | 8 | 0 | 08 | [backend/internal/service/openai_gateway_responses_anthropic_native.go](../../../backend/internal/service/openai_gateway_responses_anthropic_native.go) |
| M | source | 116 | 1 | 08 | [backend/internal/service/openai_reasoning_effort_policy.go](../../../backend/internal/service/openai_reasoning_effort_policy.go) |
| A | documentation | 289 | 0 | 08 | [docs/new-api-adoption-plan-2026-10-12.md](../../../docs/new-api-adoption-plan-2026-10-12.md) |
| A | documentation | 160 | 0 | 08 | [docs/new-api-adoption-validation-2026-10-13.md](../../../docs/new-api-adoption-validation-2026-10-13.md) |
| A | documentation | 665 | 0 | 08 | [docs/new-api-relay-research-2026-10-12.md](../../../docs/new-api-relay-research-2026-10-12.md) |

## 关联实现（由其他组提交）

同一共享文件保持单一主提交，跨组依赖随完整提交集验收。

| 主归属组 | 类别 | 文件 |
| --- | --- | --- |
| 03 | source | [backend/internal/handler/gateway_handler.go](../../../backend/internal/handler/gateway_handler.go) |
| 03 | source | [backend/internal/handler/gateway_handler_chat_completions.go](../../../backend/internal/handler/gateway_handler_chat_completions.go) |
| 03 | source | [backend/internal/handler/gateway_handler_responses.go](../../../backend/internal/handler/gateway_handler_responses.go) |
| 16 | source | [backend/internal/service/gateway_forward.go](../../../backend/internal/service/gateway_forward.go) |
| 16 | source | [backend/internal/service/ops_upstream_context.go](../../../backend/internal/service/ops_upstream_context.go) |

## 2026-10-14 新增需求与实施边界

在最终模型/传输处选择 schema 方言，保持结构化输出；仅安全的 Responses completed 纯文本尾部可适配，Chat 来源与工具/思考/签名不裁剪；beta 多值聚合。转换诊断有界且不记录载荷。原有 thinking.disabled 严格语义保持。
