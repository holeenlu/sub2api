# 08 · 模型能力、推理参数与多协议兼容

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`feat(gateway): preserve model capabilities and protocol semantics`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. 根据映射后、实际发往上游的模型能力规范化 thinking mode、effort、budget；账号回退必须保留原始请求意图，不能在第一尝试就不可逆修改共享请求。
2. Claude 原生 Messages、OpenAI 兼容转换、Bedrock 使用同一能力规则；保留有签名历史，省略的字段仍表达供应商默认，不为了统一而强行补值。
3. Fable 对不支持的 disabled 保持严格拒绝；Sonnet 的 between-tools 转换明确执行；Opus 5 历史 effort 和 Sonnet 5.5 省略 effort 采用当前已通过验收的规则。
4. ingress effort policy 只执行一次；手工模型的低预算、输出上限错误保持文档中的显式限制，不能自动改变用户模型来掩盖参数不支持。
5. Gemini 在本协议内归一 thinking，不将 Claude 参数语义机械套用；兼容路径保留原重试、计费、归属和 BPS 恢复契约。
6. New API 只作为固定来源研究与语义适配依据。2026-10-13 更新吸收规则覆盖整个网关价值与既有缺口；本次已纳入 G03–G06/G08 的对应实现，其他落点在关联功能组登记。

保留 New API 的 Vertex endpoint 语义适配：global 与区域 host 统一由现有函数决定，模型目录、生成与图片请求共用规则，Express 鉴权保持。仅 cache read/write 有用量、input/output 为零时，终态与 EOF、流式与缓冲路径仍记录实际消耗，不把缓存消耗归零。Gemini 思考档位与摘要、Argon、最小输出 token 等官方 v0.2.17 已实现部分直接继承基线。

## 入口与配置归属

沿用下列方案和完整文件清单中的现有入口。

## 最终实现方案

1. 保留既有 Claude capability/effort 规则模块和各协议最后转换位置；入口只保留意图，最终模型处进行归一。

2. Claude/Gemini 各自沿协议输出，fallback 使用请求副本，签名历史和未指定默认值不被结构性重写。

3. 研究、采用方案、实际验证分开保存；New API 整仓参考不等于整分支导入，不恢复退役能力。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

Claude/Gemini thinking、最终模型、fallback 副本与计价标识；历史省略/disabled/budget 回归。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 3 | 1114 | 0 | 0 |
| source | 34 | 1497 | 586 | 0 |
| test | 20 | 1139 | 211 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 45 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 6 | 0 | 08 | [backend/internal/handler/composite_platform.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/composite_platform.go) |
| M | test | 72 | 0 | 08 | [backend/internal/handler/composite_platform_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/composite_platform_test.go) |
| M | source | 1 | 0 | 08 | [backend/internal/handler/gemini_cached_contents_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/gemini_cached_contents_handler.go) |
| M | test | 58 | 2 | 08 | [backend/internal/pkg/apicompat/anthropic_responses_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/apicompat/anthropic_responses_test.go) |
| M | source | 9 | 8 | 08 | [backend/internal/pkg/apicompat/chatcompletions_to_responses.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/apicompat/chatcompletions_to_responses.go) |
| A | source | 22 | 0 | 08 | [backend/internal/pkg/apicompat/conversion_diagnostics.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/apicompat/conversion_diagnostics.go) |
| A | source | 106 | 0 | 08 | [backend/internal/pkg/apicompat/responses_request_fidelity.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/apicompat/responses_request_fidelity.go) |
| M | test | 33 | 0 | 08 | [backend/internal/pkg/apicompat/responses_to_anthropic_cc_chain_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/apicompat/responses_to_anthropic_cc_chain_test.go) |
| M | source | 109 | 62 | 08 | [backend/internal/pkg/apicompat/responses_to_anthropic_request.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/apicompat/responses_to_anthropic_request.go) |
| M | source | 44 | 34 | 08 | [backend/internal/pkg/apicompat/types.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/apicompat/types.go) |
| M | source | 69 | 33 | 08 | [backend/internal/pkg/claude/effort_catalog.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/claude/effort_catalog.go) |
| M | test | 16 | 0 | 08 | [backend/internal/pkg/claude/effort_catalog_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/claude/effort_catalog_test.go) |
| A | source | 150 | 0 | 08 | [backend/internal/pkg/claude/thinking.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/claude/thinking.go) |
| A | test | 94 | 0 | 08 | [backend/internal/pkg/claude/thinking_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/claude/thinking_test.go) |
| M | source | 29 | 6 | 06,08 | [backend/internal/service/antigravity_gateway_compat.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/antigravity_gateway_compat.go) |
| M | test | 117 | 6 | 06,08 | [backend/internal/service/antigravity_gateway_compat_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/antigravity_gateway_compat_test.go) |
| M | source | 1 | 5 | 08 | [backend/internal/service/batch_image_provider_vertex.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/batch_image_provider_vertex.go) |
| M | source | 19 | 48 | 08 | [backend/internal/service/bedrock_request.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/bedrock_request.go) |
| M | test | 11 | 51 | 08 | [backend/internal/service/bedrock_request_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/bedrock_request_test.go) |
| M | source | 1 | 1 | 08 | [backend/internal/service/claude_code_validator.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/claude_code_validator.go) |
| M | test | 11 | 13 | 08 | [backend/internal/service/claude_opus5_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/claude_opus5_test.go) |
| A | source | 32 | 0 | 08 | [backend/internal/service/conversion_diagnostics.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/conversion_diagnostics.go) |
| M | test | 12 | 3 | 08,14 | [backend/internal/service/gateway_anthropic_apikey_passthrough_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_anthropic_apikey_passthrough_test.go) |
| M | source | 10 | 4 | 08,12 | [backend/internal/service/gateway_anthropic_passthrough.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_anthropic_passthrough.go) |
| M | source | 8 | 4 | 08,12 | [backend/internal/service/gateway_bedrock.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_bedrock.go) |
| M | test | 3 | 1 | 08 | [backend/internal/service/gateway_compat_reasoning_pricing_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_compat_reasoning_pricing_test.go) |
| M | source | 26 | 24 | 08,12,14 | [backend/internal/service/gateway_count_tokens.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_count_tokens.go) |
| M | source | 11 | 5 | 08,12 | [backend/internal/service/gateway_forward_as_chat_completions.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_forward_as_chat_completions.go) |
| M | source | 12 | 6 | 08,12 | [backend/internal/service/gateway_forward_as_responses.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_forward_as_responses.go) |
| M | test | 31 | 4 | 08 | [backend/internal/service/gateway_forward_as_responses_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_forward_as_responses_test.go) |
| M | source | 13 | 4 | 08 | [backend/internal/service/gateway_request.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_request.go) |
| A | test | 218 | 0 | 08 | [backend/internal/service/gateway_thinking_normalization_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_thinking_normalization_test.go) |
| M | test | 51 | 1 | 08 | [backend/internal/service/gateway_tool_changes_beta_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_tool_changes_beta_test.go) |
| M | source | 66 | 39 | 08,14 | [backend/internal/service/gateway_upstream_request.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_upstream_request.go) |
| M | source | 2 | 9 | 08 | [backend/internal/service/gemini_cached_content.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gemini_cached_content.go) |
| M | source | 67 | 11 | 06,08 | [backend/internal/service/gemini_chat_completions_compat_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gemini_chat_completions_compat_service.go) |
| M | test | 117 | 0 | 06,08 | [backend/internal/service/gemini_chat_completions_compat_service_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gemini_chat_completions_compat_service_test.go) |
| M | source | 14 | 194 | 08 | [backend/internal/service/gemini_compat_thinking.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gemini_compat_thinking.go) |
| M | test | 20 | 74 | 08 | [backend/internal/service/gemini_compat_thinking_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gemini_compat_thinking_test.go) |
| M | source | 152 | 55 | 06,08 | [backend/internal/service/gemini_messages_compat_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gemini_messages_compat_service.go) |
| M | test | 116 | 0 | 06,08 | [backend/internal/service/gemini_messages_compat_service_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gemini_messages_compat_service_test.go) |
| M | test | 43 | 0 | 08 | [backend/internal/service/gemini_native_reasoning_pricing_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gemini_native_reasoning_pricing_test.go) |
| A | source | 124 | 0 | 06,08 | [backend/internal/service/gemini_schema_capabilities.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gemini_schema_capabilities.go) |
| A | source | 188 | 0 | 08 | [backend/internal/service/gemini_thinking.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gemini_thinking.go) |
| A | test | 75 | 0 | 08 | [backend/internal/service/gemini_thinking_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gemini_thinking_test.go) |
| M | source | 44 | 3 | 08,14 | [backend/internal/service/header_util.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/header_util.go) |
| M | source | 9 | 1 | 08,12 | [backend/internal/service/openai_gateway_chat_completions_anthropic_native.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_chat_completions_anthropic_native.go) |
| M | source | 14 | 13 | 08,12 | [backend/internal/service/openai_gateway_messages_anthropic_native.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_messages_anthropic_native.go) |
| M | test | 8 | 56 | 06,08 | [backend/internal/service/openai_gateway_request_body_reasoning_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_request_body_reasoning_test.go) |
| M | source | 9 | 1 | 08,12 | [backend/internal/service/openai_gateway_responses_anthropic_native.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_responses_anthropic_native.go) |
| M | source | 116 | 1 | 08 | [backend/internal/service/openai_reasoning_effort_policy.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_reasoning_effort_policy.go) |
| M | source | 1 | 5 | 08 | [backend/internal/service/upstream_models.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/upstream_models.go) |
| M | source | 13 | 10 | 08 | [backend/internal/service/vertex_service_account.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vertex_service_account.go) |
| M | test | 33 | 0 | 08 | [backend/internal/service/vertex_service_account_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vertex_service_account_test.go) |
| A | documentation | 289 | 0 | 08 | [docs/new-api-adoption-plan-2026-10-12.md](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-adoption-plan-2026-10-12.md) |
| A | documentation | 160 | 0 | 08 | [docs/new-api-adoption-validation-2026-10-13.md](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-adoption-validation-2026-10-13.md) |
| A | documentation | 665 | 0 | 08 | [docs/new-api-relay-research-2026-10-12.md](https://github.com/holeenlu/sub2api/blob/main/docs/new-api-relay-research-2026-10-12.md) |
