# 2026-10-19 主工作区未提交修改复核

## 范围与判定

用户授权逐项复核主工作区：有效但未提交的需求归入原有相关提交并推送；已经发布或无效的修改清理。冻结主工作区与最新三品牌远端，比较基线、工作区完整快照和当前远端的路径、mode 与 blob，不按“未提交”标记或缺失祖先直接判断功能缺失。

| 类别 | 路径数 | 处理 |
| --- | ---: | --- |
| 与最新 main 完全一致 | 55 | 已发布的本地副本，其中 18 个在旧 HEAD 下显示为未跟踪；同步主工作区后恢复正确跟踪状态，不重复提交 |
| 旧副本或无效残留 | 13 | 保留最新远端的完整实现、需求说明和来源记录，清除本地回退与多余空行 |
| 有效但尚未发布 | 25 | 保留并验证；其中 2 个 WS 文件剔除夹带的旧诊断/测试回退，仅纳入有效新逻辑 |

已发布副本主要是客户端遥测、#33681 BPS 恢复归因、AGENTS 清理规则及相关界面/迁移文件。旧副本包括已被完整重构覆盖的 Anthropic 用量服务及界面/多语言片段、WS 首语义/可见输出/关闭码诊断删除、Lite 搜索历史说明删除、BPS 来源 SHA 回退、已交付报告状态回退。它们既不构成新的完整需求，也不能覆盖当前消费者、测试与已发布记录。

## 纳入的有效需求

- 计量后失败：HTTP Responses、Chat/Messages 兼容、ctx_pool、WS HTTP bridge 和 passthrough 将实际观测到的用量交回既有账单入口；保留失败状态，阻止这次已计量尝试被透明重放或重复结算。未观测用量不估算，不新建账本、价格规则或历史补扣。
- 有界断连回收：沿用既有 1200 ms WS usage-drain 时长，将 HTTP 头部等待及正文读取纳入客户端断连后的限时收尾；上游心跳不能无限延长资源占用。结束时释放已有请求上下文与响应体。
- 非流式协议收尾：上游实际返回 SSE 时，读到有效终态即可结束，不必等连接 EOF；保留完整终态状态与实际用量，缺失终态或无效 JSON 不再伪装成成功响应。复用既有 SSE 文档/事件解析器。
- 会话与诊断：Responses 的派生会话种子沿用起始 system/developer 前缀与首条用户输入，后续 developer 消息不改写会话身份；已有缓存诊断继续沿用生成后的会话上下文。
- 保留已发布能力：WS 1012 与结构性进度回归、visible_output_seen、first_semantic_event、upstream_close_code、Lite 搜索保护、当前 Anthropic 用量与 Fable 子集语义、品牌及发布渠道均保留。

原有整块读取助手在读取失败时丢弃局部正文，且没有按 SSE 终态结束的契约；本轮局部读取助手复用现有解析器解决该缺口。新的 HTTP 收尾守卫补齐原有 detached 上游请求的断连边界；不新增上游端点、重试器或计费链路。透传结果结构复用已有公共结果类型，清除重复字段定义。

## 代码与提交归属

计量/断连/终态修复归入原有 billing 主题；会话种子及缓存诊断接续归入原 diagnostics 主题；本报告、恢复契约及 BPS 指纹记录归入原 requirements 主题。原作者和官方上游父链保持，品牌覆盖经三方树核对后接续。

新增对外接口、持久化配置、数据库迁移、上游执行入口均为 0；内部增加有界 HTTP 收尾守卫，复用既有发送、SSE 解析和结算机制。

## 验证

- 定向 race：service 20.424s、handler 4.066s；覆盖计量失败、断连收尾、终态与会话身份。
- WS relay 整包 race：5.111s。
- go vet -tags=unit ./... 与 go build ./... 通过。
- 全部 Workflow 手动触发检查：5 项通过。
- 冻结候选的后端全量单测 go test -json -tags=unit ./... -count=1 -timeout=10m 通过：61 个有测试的包通过；27978 项测试/子用例通过，121 项跳过。service 包耗时 300.549s。跳过项涉及显式关闭的 DingTalk OAuth 路径、外部 TLS 探测及需要专门环境的 PostgreSQL/Redis 等集成场景，不计为真实服务联调通过。

真实上游调用、生产数据库、部署和迁移不在本轮执行范围。测试中的外部服务/数据库跳过项单列，不将其计为实际联调通过。

## 清理与恢复

原 HEAD、暂存区及包含未跟踪文件的完整工作区已保存到本地备份提交。最终仅在发布与远端 SHA 核验完成，并确认工作区自冻结后没有新增修改时，将主 main 对齐已发布树；不执行宽泛 git clean，不清理其他 worktree、忽略文件或已应用迁移。

备份：refs/heads/codex/backup-workspace-audit-20261019-xx9bm27n，快照 1ba0d4a5ecd7fb707c7441d255124d2f27985730。

### 有效代码路径

- `backend/internal/handler/openai_gateway_handler.go`
- `backend/internal/handler/openai_gateway_handler_test.go`
- `backend/internal/service/openai_compact_sse_keepalive.go`
- `backend/internal/service/openai_compat_model_test.go`
- `backend/internal/service/openai_content_session_seed.go`
- `backend/internal/service/openai_content_session_seed_test.go`
- `backend/internal/service/openai_gateway_cc_pipeline.go`
- `backend/internal/service/openai_gateway_chat_completions.go`
- `backend/internal/service/openai_gateway_chat_completions_raw.go`
- `backend/internal/service/openai_gateway_chat_completions_raw_test.go`
- `backend/internal/service/openai_gateway_chat_completions_test.go`
- `backend/internal/service/openai_gateway_forward.go`
- `backend/internal/service/openai_gateway_messages.go`
- `backend/internal/service/openai_gateway_passthrough.go`
- `backend/internal/service/openai_gateway_response_handling.go`
- `backend/internal/service/openai_gateway_scheduling.go`
- `backend/internal/service/openai_gateway_service_test.go`
- `backend/internal/service/openai_oauth_passthrough_test.go`
- `backend/internal/service/openai_response_terminal_usage_compat_test.go`
- `backend/internal/service/openai_stream_read_error.go`
- `backend/internal/service/openai_ws_forwarder_ingress.go`
- `backend/internal/service/openai_ws_forwarder_ingress_session_test.go`
- `backend/internal/service/openai_ws_http_bridge.go`
- `backend/internal/service/openai_ws_v2/passthrough_relay.go`
- `backend/internal/service/openai_ws_v2_passthrough_adapter.go`

业务代码 +463/-166，测试 +361/-30；文档另计。

## 发布期间的并发上游更新

首次原子推送因 Git 数据包解包失败而未更新任何分支；本地新增对象已逐个完整解码并验证 SHA-1。随后审查并保留另一任务已发布的官方 PR #8139（e068c3d02ff8664c1142d5f7c51a05a82f75ad3d）：response.queued 与 response.metadata 仍属于元数据，不解除首输出时限、不提交语义输出。该变化与已计量失败不重放规则同时保留；新增组合另行覆盖元数据/过载、HTTP、WS bridge 和计量失败定向竞态检查。原官方提交及两个官方 merge 的侧父链均保留。
