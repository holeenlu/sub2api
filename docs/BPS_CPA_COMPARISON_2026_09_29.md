# BPS 方案对比与整合记录（cpa-plugin-oai-basispoints v0.2.8）

本次审查对象为 `/Users/luhonglin/Git/cpa-plugin-oai-basispoints` 的 `main`，固定源提交为 `280e28b0bcfd38431d851e22c853417c690455ff`（标签 `v0.2.8`）。目标代码基线为 Sub2API `main` 的 `e49ac5141`。本次只整合可在 Sub2API 现有 HTTP/SSE BPS 桥接中验证的行为，不引入 CPA 插件 ABI、宿主配置或部署文件。

## 结论

| 来源能力 | Sub2API 原有状态 | 处理 | 原因 |
| --- | --- | --- | --- |
| 按当前客户端工具目录生成中继示例 | 提示中固定写入 `functions.exec`、`functions.apply_patch` | **已整合** | 提示示例现在只使用本次声明且通过 JSON Schema 校验的工具；没有这些工具时不会误导模型调用不存在的名称。 |
| 函数载荷的两层 JSON、转义边界说明 | 已有逐 transport 说明，但缺少来源方案的集中表述 | **已整合到现有提示** | 保留 Sub2API 的 FUNCTION/FUNCTION_CODE/FUNCTION_CMD/CUSTOM 和命名空间约束，补充动态示例，不改变既有解码协议。 |
| 流正文/推理与终态一致性 | 工具终态会完整校验，普通正文增量没有和最终响应逐项比对 | **已整合** | 对同时提供完整索引、item id、part id 的消息和推理事件进行增量核验；终态不匹配时先返回安全协议错误，再执行工具翻译和回放写入。缺少索引的旧网关事件继续兼容透传。 |
| 终态补齐缺失的正文/推理生命周期 | 结构化输出有补全，普通文本主要依赖终态响应 | **暂不整合** | 需要改写现有 SSE 时序，并可能改变客户端已依赖的稀疏事件行为；本轮先落地可独立验证的终态校验。 |
| CPA 到上游 WebSocket 优先、凭据 `websockets` 门槛 | Sub2API 走自身账号调度和 HTTP/SSE BPS | **不整合** | 传输方向和凭据生命周期不同；来源文档也明确 WS 能力不能证明 BPS 上游支持。 |
| `ultra` 原样透传、模型别名和插件 OAuth 虚拟认证 | Sub2API 有自己的推理档位归一化、账号路由和凭据刷新 | **不整合** | 会改变现有模型能力声明和账号调度契约，不能仅按插件行为替换。 |
| 图片、附件、失败分类、受限缓存、工具回放 | 已在本地 BPS 路径中有对应实现 | **保留现有实现** | 已有实现分别结合 Sub2API 的账号状态、图片策略、用量记录和 scoped replay cache，重复移植会扩大风险。 |

## 已整合的行为

`catalog.go` 新增目录驱动的中继示例生成器。它按当前工具的完整命名空间和类型选择 custom、FUNCTION_CODE、FUNCTION_CMD 或普通 FUNCTION 示例，并用已编译的 schema 验证示例参数；最多加入四个示例，避免工具目录膨胀提示。`request.go` 删除固定工具名示例，保留现有工具白名单、`tool_choice`、并行调用限制和自定义 transport 标记。

`stream_progress.go` 记录带有合法 `output_index`、`content_index`/`summary_index`、`item_id` 和生命周期的正文及推理片段。`response.completed` 到达时，比对响应 id、item id、part 类型及已交付文本前缀；如果 delta/done 已完成，则要求终态文本完全一致。校验发生在 `translateCompleted` 之前，因此不一致不会触发工具下发、未知工具重生成后的回放或 replay cache 写入。稀疏事件缺少这些身份字段时不启用严格状态机，保持现有兼容行为。

## 未整合但保留为后续候选

来源方案的完整流交付器还会延后首个有效正文、合成 `response.created`/`in_progress`、补齐缺失生命周期并处理 logprobs。Sub2API 当前需要保持已有首条摘要即时透传、工具事件终态隔离、结构化输出专用补全和客户端断开语义，因此本轮只采用其中的终态一致性原则。若后续要补全生命周期，应先增加真实 Codex 客户端回归和逐事件兼容矩阵。

来源方案的 WebSocket、OAuth 虚拟认证、模型目录别名和 CPA 管理页面属于宿主/插件边界；本项目不复制这些文件，也不修改生产配置、数据库或部署流程。

## 验证

- `/Users/luhonglin/sdk/go1.27/bin/go test ./internal/service/basispoints`：通过。
- 新增目录示例回归：声明的命名空间工具会生成示例，未声明的固定工具名不会出现；不满足 schema 的样例会被跳过。
- 新增流回归：完整索引事件与终态一致时完成；终态正文不一致时返回 `basispoints_protocol_error`，不返回 `response.completed`。
- 本轮未进行真实 BPS 账号调用、CPA 插件加载、WebSocket 验收或生产部署；这些不属于本地代码整合的验证证据。
