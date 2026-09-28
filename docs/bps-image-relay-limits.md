# Excel / BPS 图片中转参数

后台「设置 → 功能开关 → Excel / BPS 图片支持」分别提供请求接入限制、图片转换与暂存限制。请求体上限不替代图片限制：例如 128 MiB 请求体配合 20 张图片上限，仍会拒绝第 21 张内嵌图片。常见错误的处理方式见本文后面的故障排查。

| 设置字段 | 默认值 | 范围 | 含义 |
| --- | ---: | ---: | --- |
| `excel_bps_image_max_image_mib` | 20 | 1–128 | 单张图片解码后的大小，MiB |
| `excel_bps_image_max_images` | 20 | 1–4096 | 整份请求的内嵌图片项数量 |
| `excel_bps_image_max_total_mib` | 32 | 1–128 | 整份请求图片解码后总大小，MiB |
| `excel_bps_image_storage_mib` | 1024 | 1–16384 | 每进程临时图片磁盘容量，MiB |
| `excel_bps_image_storage_entries` | 512 | 1–65536 | 每进程临时图片数量，含正在写入及读取后待释放的文件 |
| `excel_bps_image_ttl_minutes` | 30 | 1–1440 | 最后一次提交后的链接有效期，分钟 |

参数须为整数。每请求图片总大小须不小于单张上限；进程暂存容量和数量须能容纳一次请求的上限。图片数和图片总大小包括历史消息、工具返回和重复图片项；已有的公网 URL 不计入内嵌图片转换数量。图片格式、64 百万像素和下载并发保护仍然保留。

保存后无需重启，后续转换读取新设置。正在转换的请求使用已读取的单张/请求限额；共享存储在每次预留容量时使用最新设置。降低暂存限制不会立即删除有效文件，容量不足时返回 503，待自然释放或提高配置后可继续。单次转换超限返回 400，并在消息中说明当前本地配置的上限。

修改有效期不会立即改写已有链接的到期时间，再次提交同一图片时使用新的有效期。关闭中转仍会停止转换并禁止下载；实例重启后原进程链接不可用。异常退出目录使用独立的空闲清理宽限期，不随链接有效期延长。

已有数据库缺少新字段时使用上述默认值；旧客户端更新设置时省略这些字段会保留已有值。显式零、负数、超范围或互相冲突的设置会拒绝保存，不会部分写入。部署只增加 settings 键，无 schema 迁移。

多轮截图超过 20 张时，可先将每请求图片数调至 100（默认暂存图片数 512 无需同时修改），再按实际图片总大小调整相应大小限额。请求体仍包含 base64 膨胀和文本，应保留请求接入上限的余量。

## Excel / BPS 常见错误与配置

本文针对 OpenAI OAuth 账号的 Excel / BPS 协议。以下本地图片接入保护不是独立 OpenAI BPS 平台的上游限制。

### 1. TPM / RPM 限流

`Rate limit reached for gpt-6-sol in organization org-… on tokens per min (TPM) … Please try again in 173ms` 是 BPS 上游组织的令牌吞吐窗口满了，不是本地图片大小、推理档位或余额设置。该组织由**所有** BPS 账号共用（2026-09-28 生产数据：5 个账号报的是同一个组织、同一个 40M TPM 上限），切换或冷却单个账号都不能缓解。

上游的表现是 HTTP 200 的 SSE：先发 `response.created`、`response.in_progress`，第三个事件才是 `error`（`code=rate_limit_exceeded`，错误对象里带 `retry-after-ms`，通常 100–400ms）。

处理逻辑：

- **开头限流自动重发**：拿到 2xx 响应后，最多观察 5 秒，直到出现第一个不是生命周期元数据（created / in_progress / queued）的事件。如果这个事件是限流，说明模型还没生成任何内容，就按上游提示在**同一个账号**上重发同一个请求：退避从提示值开始（最少 100ms），每次翻倍并加随机抖动，最多 3 次、总等待不超过 5 秒；单次提示超过 2 秒则不重发。被拒那次的生命周期事件不会发给客户端，也不冷却账号、不切换账号。每次重发在运维后台记一条 `rate_limit_retry` 上游事件，并打一行 `upstream rate limit before output; retrying on the same account` 日志。
- **重发用尽**：返回 HTTP 429，`Retry-After` 为整秒（至少 1），错误体为 `{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"Upstream BPS rate limit reached. Please try again in 1s."}`。保留标准 code 和 `try again in` 提示是因为 OpenAI 客户端（包括 Codex）只在 `rate_limit_exceeded` 时按这段提示计算重试等待。不冷却账号，也不交给调度循环换号。
- **已有输出之后的限流**：桥接层可能已处理文本或工具修复，不重放。非流式返回 HTTP 429；流式保留 HTTP 200，把终止事件统一改写成 `response.failed`（Codex 会忽略顶层 `error` 事件），错误对象同上，保留已有输出和用量。
- **HTTP 429**（非 SSE）：沿用原逻辑，只冷却该账号的 BPS 路由并在预算内换号；冷却时长优先取 `Retry-After`，其次取错误对象里的 `retry-after-ms` / `retry-after` / `try again in …` 提示，向上取整到秒，最多 2 小时，都缺失时沿用既有 429 冷却设置。
- 客户端永远拿不到上游原文（其中含组织 ID）。`gateway.log_upstream_error_body` 开启时（默认开启），运维后台会记录脱敏后的上游原文，便于区分组织级 TPM 和其他限流。

自动重发只能消化瞬时的毫秒级拥塞，不能提高上游 TPM 配额。持续高于配额时，仍需降低并发或缩短重复历史。

### 2. 托管工具不支持

`basispoints_unsupported_tool` 是本地兼容性检查。当前会拒绝要求实时外网搜索（`external_web_access=true`）、高搜索上下文（`search_context_size=high`）或 `image_generation` 的请求。

客户端看到的报错会写明被拒的工具类型（`tool_choice` / `web_search` / `image_generation`），并提示「请管理员为该账号允许省略不支持的工具」，不会出现内部配置键名。

在「账号管理 → 编辑 OpenAI OAuth 账号 → Excel / BPS 协议」可以启用「保持 BPS，省略不支持的托管工具」（`openai_excel_bps_omit_unsupported_tools`）。启用后，请求继续走 BPS，但这些工具被省略，并向模型说明不可用；这不赋予 BPS 联网或生图能力。客户端函数工具不受此开关影响。强制工具选择仍须调整为支持的 `auto` / `none`，不能靠省略开关跳过。

如果任务必须使用这些托管能力，应使用支持该能力的原生转发账号/模型路由，而不是省略工具。此开关默认关闭，修复不会替管理员改变工具策略。

### 3. 图片接入容量 503

`basispoints_image_request_busy`（客户端文案为 `Request capacity is temporarily busy; retry later`）来自本地接入保护，附带 `Retry-After: 1`。它表示当前在途请求数或请求体加权预算耗尽，不等于上游 BPS 宕机。

配置位置：「设置 → 功能开关 → Excel / BPS 图片支持 → 请求接入限制」。

| 设置 | 默认值 | 作用 |
| --- | ---: | --- |
| 请求体上限 | 64 MiB | 单个请求体大小限制 |
| 共享资源预算 | 1024 MiB | 请求体及解析副本的加权接入预算，不是实际 RSS |
| 最大在途请求数 | 128 | 同时持有接入名额的请求数 |

有效预算取「配置预算」和「在途上限 × 8 MiB」的较大值。已知长度的请求在上传前预留 `max(请求体大小, 1 MiB) × 8`：默认预算下，32 MiB 请求最多同时上传 4 个。压缩请求解码前按请求体上限预留，未知长度请求边读边扩。

开启图片支持后，保护覆盖 OpenAI/Composite 分组的 Responses、Chat、Messages HTTP 请求，因为选账号之前就需要读取请求体。请求体由这道保护完整读入后再判断：

- **可能带内嵌图片**（出现 `data:image`，或 Anthropic 格式的 `"base64"` 图片来源）：保留预留和名额，直到请求结束（包括调度等待和流式响应）。压缩和未知长度的请求缩减为实际大小 × 8。
- **纯文本**：读完立即释放预留和名额。纯文本请求只在上传期间占用这道保护，不再占到流式响应结束。

2026-09-28 生产上的 1001 条 503 就是旧行为造成的：当时平均只有 15–35 个在途请求，但 Codex 每个 6–17 MB 的纯文本请求按 × 8 计权并占到流结束，把 1 GiB 预算耗尽。判断是保守的，误判为可能带图只会退回旧行为。图片链接暂存容量不足是另一个限制，不能用提高「进程暂存容量」解决本节的接入 503。

优先降低客户端同时请求数、减少重复截图和历史体积，并按 `Retry-After` 退避且加入抖动。确有内存余量时再提高共享资源预算或在途上限；单独提高请求体上限不增加并发容量。修复没有取消保护、排队大请求或自动提高预算。

### 4. 最多 20 张内嵌图片

`basispoints accepts at most 20 inline images per request` 是旧版本的本地原生附件校验文案，不是已经证实的上游固定 20 张限制。现在的文案为 `BPS accepts at most N inline images per request, counting images in history and tool outputs; remove older images or ask the administrator to raise the limit`（HTTPS 中转模式为 `image relay accepts at most N …`）。对应的后台字段是 `excel_bps_image_max_images`，文案里不出现字段名，因为终端用户改不了它。

同一设置页的「每请求图片数上限」默认 20，可设 1–4096。统计整份请求中的内嵌图片项，包括历史消息、工具截图和重复项；不是只统计本轮新增图片。多轮截图场景可以按实际需要增加，例如 100；也可从客户端压缩历史、减少截图。保存后新请求生效，无需重启。

提高张数不会同时提高字节限制。原生附件模式仍固定单张 20 MiB、每请求图片合计 32 MiB；HTTPS 中转模式使用该页的图片大小与暂存设置。原始 JSON 的文本和 base64 膨胀也受请求体上限及共享预算保护。不会自动删除历史图片或静默丢图以绕过限制。
