# ranxi2001 Excel / BPS 增量同步

## 范围与来源

归属：公共功能。本轮仅创建本地提交，不推送、不发布镜像、不操作生产数据库。

BPS 公共提交基于三个分支已有的共同版本 79ff9cf2757ce5e0fa16cf5a3809f122723d60a2 构建，分别普通 merge 到 holeen/main、KDAN、TapModels，保留相同公共 SHA。公共工作区另一个模型目录提交仍留在其原有分支，不包含在本次 BPS 共享提交中。

本机通过 GitHub SSH 443 拉取 ranxi2001/production，核对到 fe27f9895a75e12562f33eff70f21c660329a684（源提交时间 2026-09-28 00:58:02 +08:00）。上次 Excel/BPS 整合核对到 f80611d6cd7fa7d225b7c8767fcd555b75f8745e，本次审查两者间的增量。

| 来源提交 | 原作者 | 本次采用范围 |
| --- | --- | --- |
| fd7964df2224dca69dbe9a3af4f07a4e8a4d1c60 | danvilig | 协作历史 agent_message 图片的校验、原生附件上传及禁用时忽略处理 |
| dc01c71b758e8c24bd76ea5f7ddb089fc76481d3 | akihitohyh | BPS 429 换号、账号 BPS 专用冷却、错误响应及调度/连接测试适配 |
| a670aacc8a22ddfab4a4217aca5c8a4441350a09 | ranxi2001 | 仅 BPS 独立传输 profile、HTTP/2 故障证据、后续请求 HTTP/1.1 短期回退与测试 |

前两个提交以 cherry-pick --no-commit -x 导入并解决本地冲突；第三个是混合功能提交，按文件和调用点移植传输部分。合并为一次依赖完整的公共提交，记录原作者及源 SHA。来源 SHA 不会因此成为本地提交的祖先。

未引入混合提交中的 Mihomo、订阅/节点管理、IP 预热池及其设置、后台任务和依赖注入；不恢复其他已移除的 fork 功能。sub4api 的独立 openai_bps 平台不属于本次功能更新范围。

## 行为变化与配置

### 协作历史图片

agent_message 与普通消息使用相同的图片路径。原生附件模式下，在历史消息转换前校验、上传并统计图片限额；图片支持关闭且账号允许忽略图片时，替换为明确的不可用提示。保留发送者、接收者和相邻文本。

配置仍为“系统设置 → 功能开关 → Excel / BPS 图片支持”，以及账号的“图片支持关闭时忽略图片输入”；没有新增开关，也不改变已有图片数量配置。

### 429 换号与冷却

- 主生成、无效加密历史的受限重试、原生图片附件上传在尚无业务输出时遇到 HTTP 429，交给原有账号切换流程；不重试同一已限流账号，继续遵守切换预算和客户端取消检查。
- 仅冷却该账号走 Excel/BPS 的模型。以渠道映射后的转发模型、再按账号模型映射判断范围，与本地 Forward 路径一致；保留原有账号优先级、粘性会话和轮次准入检查。原生 Codex 模型不受该冷却影响。
- 冷却优先使用合法 Retry-After，否则使用“系统设置 → 429 默认回避”；默认回避关闭且上游无合法 Retry-After 时，仅本次请求排除账号。冷却限制 1–7200 秒，并发更新不缩短已有的更长冷却。
- 冷却是进程内状态，重启清空，多实例不共享；不写 Codex 配额、全局冷却或账号健康失败率。
- 所有候选失败后返回 429、basispoints_rate_limited 和合法 Retry-After；已输出 SSE 时返回失败事件。仅因池中账号均在 BPS 冷却的新请求也报告限流，本地账号池诊断读取真实的进程内冷却到期时间，继续保留配置不支持、数据库查询失败和其他停调原因的原有分类。按既有规则，池中可用原生账号仍可被选中。
- 工具纠错阶段已经接收有效输出后遇到 429，只记录 BPS 冷却并结束纠错，不换号重放。

### 传输与诊断

BPS Responses 与附件上传使用独立 excel_bps 连接池，与 Codex 和其他长流量隔离。只有 HTTP/HTTPS 代理上已确认协商 HTTP/2 的 EOF、连接重置或 HTTP/2 错误，才标记后续 BPS 请求短暂尝试 HTTP/1.1。下一次使用该代理时开始一分钟回退窗口；未再次使用的标记最多保留一小时，状态容量受限。

当前失败请求不会在传输层重放，也不切换代理或改为直连。TLS 握手失败、客户端取消、截止超时、普通 HTTP 错误、正常流结束不触发该回退。

传输诊断仅记录固定分类、协议、连接/写入阶段、复用状态和散列作用域；不保存原始代理地址、请求头或错误中的凭据。

## 代码位置

| 职责 | 文件 |
| --- | --- |
| 历史图片 | backend/internal/service/basispoints/attachments.go、images.go、history_message_images_test.go |
| 429 与 BPS 冷却 | backend/internal/service/openai_excel_bps_ratelimit.go、openai_excel_bps.go、openai_excel_bps_attachments.go |
| 调度与最终错误 | openai_account_runtime_block_fastpath.go、openai_account_scheduler.go、openai_gateway_scheduling.go、openai_gateway_model_availability.go、backend/internal/handler/openai_gateway_handler.go |
| 独立连接池与回退 | backend/internal/repository/http_upstream.go、http_upstream_bps.go、backend/internal/service/http_upstream_profile.go |
| 脱敏诊断 | backend/internal/util/transportdiag/trace.go、backend/internal/service/openai_excel_bps_transport.go |
| 使用说明 | docs/excel-bps.md |

没有新增或修改数据库迁移、前端设置、依赖锁文件及版本号。原有主库轮次准入、Codex 票据边界、403 策略和图片限额保留。

## 验证

公共改动的本机验证：

- go test -tags=unit ./...：61 个测试包通过。
- BPS、主库轮次准入、账号池可用性与错误分类专项测试通过。
- go test -race -tags=unit ./internal/util/transportdiag ./internal/service/basispoints ./internal/repository ./internal/service ./internal/handler -run 'BPS|Bps|Excel|OpenAITurnAdmission|CodexTicket|Trace|AgentMessage|HistoryMessage' -count=1：通过。
- golangci-lint（unit 标签，以 79ff9cf27 为增量基线）：0 issues；不宣称仓库既有 lint 全部清零。
- go build ./cmd/server：通过；此为本机非 embed 编译检查，不是发布镜像。
- git diff --check、公共归属检查通过；迁移、前端、版本号和依赖锁文件均无差异。

HTTP/2 回归通过本机 TLS 上游和 CONNECT 代理制造首部前/响应体中的连接中断，断言失败 POST 不重放、下一独立请求走 HTTP/1.1、其他长流量 profile 不受影响。429 测试覆盖换号后的完整工具历史、并发冷却不缩短、全池冷却及 Retry-After、客户端取消、纠错后不重放、原生 Codex 隔离和渠道模型映射。

全部测试使用本机模拟数据，没有调用真实 BPS/OpenAI 账号；没有执行生产数据库迁移或真实账号效果验收。
