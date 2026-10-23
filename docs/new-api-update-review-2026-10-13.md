# New API 增量更新核查（2026-10-13）

## 结论与范围

本轮检查上次研究基线之后的已合入更新，逐项对照当前 Sub2API；没有合并 New API、修改业务代码或发布。共享能力建议适用于 KDAN、TapModels、Tokensavy。

- New API 上次研究基线：`633b4c5b190dc1af99a2814391354d86948e7943`。
- 本轮固定主分支：`ef60ef27f36e418143fa4abf0c9025ba6f17fba7`，提交时间 2026-10-13 19:48:01（Asia/Shanghai）。
- 最新发布标签仍为 `v1.0.0-rc.44`，发布于 2026-10-12 21:42:01（Asia/Shanghai），对应旧基线；本轮新增内容尚不在该发布标签内。GitHub API 的 prerelease 标志为 false，不改变标签本身为 rc.44 的事实。
- 两个 New API SHA 之间共 4 个提交、13 个文件，净 diff 为 +378/-23。
- 本地对照基点：`95615f25d495961c321381a272d1483b7555bdd3`。核查期间工作区出现其他任务的 BPS 等改动，本轮未修改这些文件。

本轮已确认应吸收请求体错误保留与各入口诊断覆盖，并按整条处理链验收。已检查入口的 413 处理可复用，但不能由局部覆盖推断全链路完成。此次没有 Claude thinking/effort 更新，不能靠这 4 个提交修复用户报告的 disabled 400；这不影响这些更新对整个网关的独立价值。后续范围以本文末尾的用户明确原则和项目交付规范为准。

## 新增提交逐项判断

| 提交 | 实际更新 | 本地对照与建议 |
| --- | --- | --- |
| `b4be2a7` / #7782 | New API 定价编辑器新增分组插到列表顶部，并补交互测试 | P3 可选体验。我们已有分组排序与拖动排序，UI 结构不同；不直接移植其 React 数组改动，也不改用户既有排序。 |
| `63cecab` / #7749 | New API 的 Sub2API 渠道可 GET `/v1/usage` 查询余额；优先 remaining，缺省回退 balance；区分负钱包余额与无限订阅 | 对方接入我们已有接口，非我们缺少余额 API。本地 `GatewayHandler.Usage` 已返回余额/订阅信息；建议将有限订阅、无限订阅、负钱包余额纳入对接验收，不新增接口。 |
| `d10eafb` | 请求体读取或解析失败不再被统一的任务协议错误掩盖，返回/记录真实原因，超限返回 413 | P1 借鉴诊断思路。本地已存在有界、无载荷的错误分类函数，但只接入部分 OpenAI 入口；扩展现有函数即可，无需照搬对方错误字符串。 |
| `ef60ef2` | 首次读取失败后缓存错误，后续读取保留原始原因；更多路径统一 413；消除重复 Invalid request 前缀 | P1 借鉴错误保留原则。本地主要网关前置校验读取失败会 Abort，未证明具有对方相同的 closed Body 路径；但审计中间件发现并复现了同类原始错误丢失。 |

来源：

- [最新发布记录](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.44)
- [新增分组置顶](https://github.com/QuantumNous/new-api/commit/b4be2a7)
- [Sub2API 余额查询](https://github.com/QuantumNous/new-api/commit/63cecab)
- [保留请求体失败原因](https://github.com/QuantumNous/new-api/commit/d10eafb)
- [后续读取保留首次失败](https://github.com/QuantumNous/new-api/commit/ef60ef27f36e418143fa4abf0c9025ba6f17fba7)

## 可落地修复一：审计预读保留原始错误

位置：`backend/internal/server/middleware/audit_log.go:197`。

该中间件预读请求体用于脱敏审计，仅在 `io.ReadAll` 成功时恢复 Body；失败时仍执行 `c.Next()`。管理面、用户面和认证路由中的已接入审计路径受此逻辑影响；不能据此认定模型网关转发也受同一中间件影响。

本轮用 `http.ReadRequest` 构造 Content-Length=100、实际只有 7 字节的截断上传，分别通过无审计与有审计的 Gin 路由，使用相同后续读取处理：

```text
without_audit: handler body length=7 error=unexpected EOF
with_audit:    handler body length=0 error=<nil>
```

“后续仍应看到 unexpected EOF”的断言在有审计情况下失败。它证明原始上传错误被预读消费，不证明任何生产请求已被错误写入，也不构成真实供应商调用验证。

建议直接修复现有预读/回填逻辑：失败后向后续读取者保留原始错误，继续由原 handler 生成其协议错误；不重新读底层 Body，不将已读取的半段内容当成功请求。成功路径、审计载荷上限、脱敏、凭据路由整体省略和关闭责任保持现有约束。

验收：截断上传仍报告原始失败；超限仍能用 errors.As 识别 MaxBytesError 并返回 413；正常 JSON/multipart 回填不变；敏感请求体不进入日志。优先扩展已有 audit_log_test.go。

## 可落地修复二：补齐请求体错误分类日志

已有实现：`backend/internal/handler/request_body_read_log.go:19`，区分截断上传、客户端断开、解压失败、超限等类别，仅记录 error_kind/content_encoding/content_length，避免把载荷和任意原始错误串写入日志。

当前生产调用点检索结果是：

- `backend/internal/handler/openai_gateway_handler.go:434`。
- `backend/internal/handler/openai_chat_completions.go:58`。

而原生 Claude 入口 `backend/internal/handler/gateway_handler.go:163` 等路径对普通读取失败仍只返回 `Failed to read request body`，未调用该分类函数。建议复用现有函数补齐相关入口，保持客户端稳定错误结构；不把 New API 的原始错误直出方式当作唯一正确实现。各入口须确认只记录一次。

验收：Claude/OpenAI/Gemini 入口遇到截断、断连、非法压缩时能定位失败类别；超限仍为 413、其余保留各入口适当的 400；不触发上游尝试、账号处罚或无效重试；日志不出现 body/token/cookie。

## 已具备或不需要直接搬入

1. **请求体超限 413**：本地 `request_body_limit.go`、handler 错误提取及分组模型前置校验已经覆盖，定向测试通过。不能把 New API 新增的 413 能力说成本地尚无。
2. **首次读取原始错误**：本地 httputil 读取器保留 UnexpectedEOF 等原始错误，现有测试通过；本轮缺口发生在审计预读与后续读取之间。
3. **Sub2API 余额接口**：现有 `/v1/usage` 在钱包模式返回 remaining/balance，订阅模式返回剩余与限额信息；无需新建同名 API 或新增出站余额轮询。未进行对真实账号的联调。
4. **定价分组 UI**：本地分组默认 sort_order/asc 且支持拖动，New API 的新增行置顶只是可选交互思路。

## Claude disabled 错误结论

新旧 SHA 的 `relaykit/`、`relay/`、`dto/` 差异为空。本次新增提交不包含 Claude 模式、effort、重试或错误自动降级修改。

已再次读取当前固定版本的 `relaykit/relayconvert/internal/shared/claude/reasoning.go`：原生 Claude 请求在没有宿主意图/模型修饰符时，直接保留原生 controls，并不强制调用能力渲染器。因此对方没有在此次更新中提供“所有原生 disabled 自动改 adaptive”的修复。

本地 Fable 5.1 / Opus 5.5 的 disabled 本地 400 仍是既有策略，需要独立处理先前讨论的语义调整；补日志、413、增加重试或导入这 4 个提交均不能让它自动成功。先前的关闭转低档产品选择不因本轮研究而改变。

[本轮固定版本的原生 Claude 分支](https://github.com/QuantumNous/new-api/blob/ef60ef27f36e418143fa4abf0c9025ba6f17fba7/relaykit/relayconvert/internal/shared/claude/reasoning.go#L32-L53)

## 验证与落地顺序

已执行：

```sh
go test ./internal/pkg/httputil -run 'TestReadRequestBodyChunksPreserves|TestReadRequestBodyWithPrealloc' -count=1
go test -tags=unit ./internal/handler ./internal/server/middleware -run '^(TestRequestBodyLimitTooLarge|TestLogRequestBodyReadFailureClassifiesWithoutPayload|TestRequestBodyReadErrorKind|TestGroupModelAllowlistRequestBodyTooLargePassesThrough413|TestUsageUnrestrictedIncludesWeeklyWindowStart)$' -count=1
```

以上现有定向测试通过。另通过 Go `-overlay` 注入隔离复现测试：无审计对照通过、有审计错误保留断言失败（预期复现结果）。没有把临时 Go 测试写进仓库；源码、overlay 和日志保存在 `/tmp/new-api-update-review-20261013/`。未跑全后端、lint/race 或真实供应商调用。

建议顺序：先修审计错误保留并固化回归，再补各入口分类日志，最后进行 New API 到现有 `/v1/usage` 的联调。分组置顶列为低优先级体验项；Claude 模式兼容单独评估。前两项应复用现有中间件和日志 helper，预期不新增 HTTP 接口、持久化配置或数据库迁移。

本轮实际交付仅此核查文档：业务代码 +0/-0、测试代码 +0/-0；新增接口、配置项及生产执行路径均为 0。未提交、推送、发版或部署。

## 补充复核：网关升级价值与 WebSocket 依赖

用户随后补充三个具体提交，要求从网关整体升级与优化判断价值。之前只列 rc.44 之后的增量，并重点说明其与 Claude disabled 的关系，范围不足以代表全部可吸收的网关改进。后续应同时覆盖新提交、已有版本中尚未采用的方案和实际依赖版本。

### 两个 relay 修复属于可靠性升级

`ef60ef2` 与 `d10eafb` 应合并评估为“请求体读取失败在整条处理链中保持一致”的修复范围，不以补一条日志或已有局部 413 判定整体完成：

1. 首次失败后的后续读取须保留原始原因，不能变为空 body、closed Body 或其他覆盖性错误。已在本地审计预读链路复现缺口；其他网关预读链路仍须逐入口验证，不把审计复现扩大为所有网关都已有同一故障。
2. 包装后的超限错误仍能通过 errors.As/errors.Is 分类，维持适当的 413；截断、解压和非法 JSON 等错误分别保留可诊断信息，并遵守各协议响应格式。
3. 本地读取失败在上游发送前结束，不进入无效重试或账号惩罚；这作为落地验收要求，不意味着本轮已发现现有调度错误。
4. 内部诊断覆盖原生 Claude、OpenAI 的 Messages 兼容入口、Gemini、图片等实际入口；只补已有统一 helper 的调用，不建立另一套错误分类。
5. 验收还需覆盖重复读取、multipart、压缩体、关闭责任以及日志不包含载荷/密钥；沿用现有成功路径和安全边界。

本轮再次核对：`gateway_handler.go:163`、`openai_gateway_handler.go:1235`、`gemini_v1beta_handler.go:328`、`openai_images.go:51` 均有读取失败路径；普通错误尚未调用分类日志 helper。已有两个 OpenAI 调用点位于 MaxBytesError 提前返回之后，因此“helper 能识别 max_bytes”也不能代替超限分支实际日志覆盖的验收。

### Gorilla WebSocket 1.5.0 → 1.5.3

New API 提交 `3fe394cdba280c00bc566de493b5fc8399b0a7d1` 只修改 go.mod/go.sum，属于依赖升级。直接读取上次研究基线 `633b4c5:go.mod` 已确认 Gorilla 为 1.5.3，且本轮两个 SHA 之间 go.mod/go.sum 没有变化，因此该升级结果未列入前面的 4 个增量提交；但仍应明确评估，不能仅因属于已有版本而省略采纳状态。参考仓库是浅克隆，不用本地祖先检查推断该独立提交的完整历史关系。

当前本地 `backend/go.mod` 及 `go list -m` 的实际选择一致：

```text
github.com/gorilla/websocket v1.5.3
github.com/coder/websocket v1.8.14
```

本地生产代码的 Gorilla 直接导入位于 `backend/internal/handler/admin/ops_ws_handler.go:23`；模型转发、OpenAI WS 池、BPS WS 桥接及 Grok 音频等路径使用 coder/websocket。Gorilla 还被部分测试用于构造测试服务端。

结论：此项是“仓库实际依赖版本已经覆盖”，不需要再改 go.mod，也不能把同版本重复写入说成新的性能或稳定性提升。若评估模型 WS 优化，应针对实际使用的 coder/websocket 和连接池、读循环、关闭与恢复逻辑核查；本轮未宣称完成其完整依赖审计。以上仅确认源码依赖，不确认线上二进制版本。

Gorilla 1.5.3 发布说明含重要回退说明，不能把 1.5.1/1.5.2 发布列表中的所有项目直接叠加，声称它们都成为 1.5.3 的新增收益。具体收益须按最终版本 diff 核实，本轮没有作性能提升量化或漏洞修复断言。

来源：[New API 依赖升级](https://github.com/QuantumNous/new-api/commit/3fe394cdba280c00bc566de493b5fc8399b0a7d1)、[Gorilla 1.5.3 发布说明](https://github.com/gorilla/websocket/releases/tag/v1.5.3)。

### 修订后的建议

- 优先落地两个 relay 提交对应的错误保留、状态码与诊断覆盖，按上述完整链路验收。
- Gorilla 版本项标记为已覆盖，保留依赖与调用链证据，不制造无效升级。
- “不能修复 Claude disabled”只描述与那组特定错误的关系，不作为排除网关升级价值的理由。
- 后续网关更新评估按“应采纳 / 部分覆盖需补齐 / 实际版本已覆盖 / 当前路径不适用”逐项记录证据，不能只看提交标题或是否引入新功能。

此次补充只更新报告，执行了模块实际版本查询和源码调用点检查，未改生产代码、依赖文件或运行新的全量测试。

## 用户明确的后续吸收原则（2026-10-13）

用户明确要求以 New API 更新对整个网关服务能力、容错处理等方面的价值决定吸收范围，不能局限于已经整合过的模块。现已写入项目 AGENTS.md，并以 [CHANGE_DELIVERY.md 的 New API 更新吸收](CHANGE_DELIVERY.md#new-api-更新吸收) 为详细规则的唯一来源。

本轮状态按此原则记录：两个 relay 修复为“应吸收/部分覆盖需补齐”；Gorilla 1.5.3 为“实际依赖版本已覆盖”，有版本与调用点证据。此前报告的 4 个增量提交只是已完成核查的范围，不代表整个 New API 中所有有价值能力都已检查或吸收。后续应继续识别已有版本中的相关缺口，按本地架构实现并验证，再纳入共享交付。

本次规则更新不代表上述业务补丁已经实施。实际仅更新 AGENTS.md、交付规范和本报告；业务及测试代码增删均为 0，未提交、推送、发版或部署。
