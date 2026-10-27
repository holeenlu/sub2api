# 网关三路径协议与资源限制（原生 / Excel BPS / Codex Cloud）

当前资源契约：2026-10-25。需求与架构见 [工程基线](ENGINEERING.md#design)。**除明确标注的模型/协议约束，以下均为本地资源保护，不是已验证的上游最大容量。** MiB = 1,048,576 字节。token、历史项数、编码字节、解码图片和会话寿命不可互换；实际容量取请求实际经过的所有层的最小值。新默认不覆盖已保存的 YAML、环境变量或后台设置。本次没有真实上游大帧/大图验收。

## 1. 上下文、历史、会话与跨协议恢复

| 项目 | 原生 | BPS | Cloud | 调整依据 |
|---|---|---|---|---|
| token 窗口 | 保留实际模型清单和上游判断 | 最大输入窗口 922,000，更小模型不抬高 | 保留实际模型/host 窗口，不把字节预算冒充 token 容量 | BPS 源码记录 2026-10-22 输入 920,360 成功、约 926k 失败；不是所有模型统一窗口 |
| 自动压缩 | 遵守模型/客户端有效阈值 | 请求默认 **830,000**，与目录共用常量；更低显式阈值保留 | 既有摘要及续接流程保留 | 为估算误差、工具和下一轮留余量 |
| 历史项数 | 不新增本地条数限制 | 不新增总历史条数限制 | 不设独立 input 条数上限；移除旧 8192 项门槛 | 长工具链减少条数先触顶；仍受 token/字节限制 |
| 历史字节 | HTTP 默认 256 MiB、客户端 WS 128 MiB | 同 HTTP/WS；含图正文默认 128 MiB，图片窗口另计 | actor 累计保留 **128 MiB**；新历史/输出计入，重发已知历史不重复累计 | 区分正文、RPC、线程累计占用 |
| 客户端 WS | 三路径共用：首消息读取 **120 秒**；轮间闲置 **900 秒** | 同左 | 同左 | 不是活跃会话总寿命；仍回收闲置连接 |
| 会话/缓存寿命 | 原有池/上游策略保留，不承诺永久续接 | 工具/目录闲置 2 小时保留；附件 ID **60 分钟**、512 项 | 完成后闲置 **30 分钟**；等待工具无活动 1 小时；状态索引 **2 小时** | 缓存过期不等于可解密；连接释放不删除账务事实 |
| 归属/结算 | 保留现有归属检查 | 账号/Key/凭据代际/会话作用域、CAS 保留 | owner lease 20 秒；结算去重 24 小时，未决事实及归档不因状态 TTL 自动删除 | 延长寿命不放宽授权 |

源码：backend/internal/service/openai_excel_bps_models_manifest.go、basispoints/request.go、openai_cloud_protocol.go、openai_cloud_session*.go、basispoints/persistent_state.go、basispoints/attachments.go、backend/internal/config/config.go。

**密文和切换：** 网关没有上游密文解密密钥，不能任意解密/重加密不同账号或签发域的密文。三路径共用明确推理前密文拒绝后的有界恢复：先移除 reasoning 密文，并保留输出项明文、移除回放密文；再将不可读 compaction 换为省略提示。最多两层，已开始推理或提交内容后不盲目重放。本网关 Cloud 摘要信封是可读 Base64URL 摘要，恢复时可展开为摘要消息，不是上游加密。完整可读历史可以重建会话；previous_response_id、待回复工具和附件 ID 仍受原会话/账号归属约束，**不能保证任意切换都无损**。详见 openai_encrypted_history_recovery.go。

## 2. 工具支持及容量

| 项目 | 原生 | BPS | Cloud |
|---|---|---|---|
| 工具类型 | 按原生能力转发 | 既有 function/custom/namespace/dynamic 目录及 FUNCTION/RAW CODE/CMD/CUSTOM 适配 | function/namespace/custom 映射为动态工具；保持名称、调用和结果身份 |
| 工具数量 | 不新增统一上限 | 受目录字节/模型容量 | 顶层及 namespace 展开后不设独立数量上限；同时待回复调用 256 保留 |
| schema/封装 | 上游契约及请求体约束 | 单 schema、封装解码 **4 MiB**；结构化答案 schema 也为 **4 MiB** | schema 受 RPC/请求体及既有语义检查 |
| 目录/回放 | 原生历史契约 | 目录 **8 MiB**、回放单项 **16 MiB**、持久条目 **16.5 MiB**；各进程缓存及共享 Redis 总预算 **32 MiB** | 完整结果 **16 MiB**、封装 **16.5 MiB**；结果重放窗口 128 请求 |
| 纠错/语义 | 既有原生恢复 | 最多两轮；嵌套 128、数字/指数展开防护保留 | function strict:true 仍拒绝；tool_choice 为 auto/none；结构化答案按 host strict schema 语义 |
| 托管工具 | 账号/模型决定 | 搜索/图片按既有适配 | 搜索/生图走既有 host 工具，不凭容量宣称支持任意宿主工具 |

4 MiB 是对应阶段的实际编码字节，不保证 4 MiB 原始源码经多层转义后仍能装下。目录、回放、缓存及 SSE 同步扩容，避免声明接纳而续接丢状态；不增加纠错次数或改变工具身份、并行权限、授权及计费。来源：basispoints 下 tool_schema/envelope/tools/catalog_cache/persistent_state/structured_output/stream.go，repository/gateway_cache_bps_state.go，service/openai_cloud_protocol.go（均相对 backend/internal）。

## 3. 图片全部环节

| 项目 | 原生 | BPS | Cloud |
|---|---|---|---|
| 输入 | Responses 按原生契约透传 | native 附件上传或 HTTPS relay；工具截图按既有路径 | 内联 base64 data URL；拒绝远程 URL/file_id、输入 detail low；工具结果 detail original 仍明确拒绝 |
| 单图字节 | multipart part/回填下载 **32 MiB**；不据此限制 Responses 内联图 | native **32 MiB**；relay 默认 **32 MiB**、可后台调整 | 无独立单图门槛，受整条 RPC 64 MiB 约束 |
| 图片合计 | 请求体及上游限制 | 每个实际 native 窗口解码后 **64 MiB**；relay 默认 **64 MiB** | RPC 64 MiB 是编码后整包；即使全为 Base64 也仅约 48 MiB 原图，实际还需扣除其他字段 |
| 图片数量 | 不新增统一张数限制 | 默认 **20 张/窗口**，后台可调；重复出现计数 | 无独立图片张数限制，受 input 项数/RPC/token 约束 |
| 像素/格式 | 上游决定 | 单图 **64 Mpx** 防护保留 | 输入不解位图、不维护格式白名单；接纳 MIME 不证明模型支持 |
| 正文/准入 | HTTP 256 MiB、WS 128 MiB | 图片正文 **128 MiB**；估算总预算 1024 MiB、128 在途槽保留；实际正文按 8 倍记账 | HTTP 256 MiB/WS 128 MiB；RPC/跨实例命令 64 MiB、actor 保留 128 MiB 更早生效 |
| 上传/缓存 | 出口安全保护保留 | 单图 **120 秒**；阶段 **180 秒**；请求并行 4/进程 32 不增加；附件 ID 60 分钟/512 项，归属失败一次重传 | 图随 RPC，无 BPS 上传阶段 |
| relay 存储 | — | 默认 1024 MiB/512 项、链接 30 分钟保留；调大图预算须同步检查磁盘 | — |
| 输出/独立接口 | 回填下载 32 MiB；HTTP 读体 128 MiB；模型/数量/格式由上游决定 | gpt-image-2；edit 单图沿用接口适配证据，复杂请求继续原生路由 | 托管图仍校验 PNG；完整结果 16 MiB 包括 Base64/JSON，不能承诺 16 MiB 原始图 |

native 自动压缩先验证全部图片及调用配对，再分别校验历史阶段和压缩后续接窗口，每窗 ≤64 MiB；不任意分段绕限、不静默删图/降清晰度。64 MiB 图片经 Base64 约 85.3 MiB，128 MiB 正文给其他字段留空间；32 MiB 单图覆盖较大 original 截图。保留 20 张、64 Mpx、8 倍估算和并发，避免所有维度同时膨胀。来源：service/openai_images*.go、basispoints/attachments.go、basispoints/image_relay.go、setting_excel_bps_image.go、openai_excel_bps_image_policy.go、openai_excel_bps_attachments.go。

## 4. 传输、请求与输出的主要资源限制

| 项目 | 调整后本地值 | 原值/依据 |
|---|---|---|
| HTTP 请求体 | gateway.max_body_size **256 MiB**；纯文本专用路由 **32 MiB** | 保留；HTTP 服务、代理和端点还有限制 |
| JSON 解压 | 按入口传入的规范化正文预算；BOM 额外 3 字节不增加正文预算；超限 413 | 消除固定 64 MiB 拦住已允许大 JSON 的问题；其他通用读体 API 仍保留 64 MiB 解压防护 |
| 原生 SSE 行/完整事件 | gateway.max_line_size **128 MiB**，源码/fallback/YAML 一致 | 源码 500、示例 40 →128；控制异常单帧峰值 |
| 首字前暂存 | **32 MiB** | 8 →32；兼容大工具元数据，保留超时/帧数/提交边界 |
| BPS SSE | 单行/事件、转换后读泵、WS 桥均 **32 MiB** | 16 →32，覆盖转义及聚合 |
| 非流式上游 HTTP 读体 | gateway.upstream_response_read_max_bytes **128 MiB** | 源码 128 保留，示例 8 →128 |
| 客户端 WS 入站 | gateway.openai_ws.client_read_limit_bytes **128 MiB** | 64 →128；不代表上游收帧能力 |
| 原生上游 WS 读帧 | **64 MiB** | 16 →64；原有大请求转 HTTP 阈值保留 |
| Cloud RPC/跨实例命令 | 各自编码后整包 **64 MiB** | 16 →64；命令包含路由/账单快照，本地 owner 不经 Redis 命令 |
| Cloud 结果/封装 | 完整 JSON **16 MiB**、持久/事件封装 **16.5 MiB** | 4/4.5 →16/16.5；包含正文/工具/图，超限 cloud_response_too_large，可信用量仍结算 |
| Cloud 传输事件队列 | **128 MiB 总预算**，每条计原报文字节及 128 字节元数据/节点开销 | 有序 FIFO 替代 256 条固定 channel；历史回显突发不阻塞 RPC 应答，超字节预算仍明确终止且不据此重放 |
| Cloud Redis 事件流 | **64 MiB /8192 条/请求** | 16 →64，条数不变 |
| Cloud 命令积压 | 每会话 **64 MiB /64 条**，取消关闭另有 16 槽/1 MiB 控制预留 | 16 →64；未启动证明、取消与归属保持 |
| Cloud 会话准入 | 复用原生用户/账号并发与排队；不另设 actor、live session、用户/账号会话、待结算及账务预留数量上限 | 删除旧 8/16/128 准入与压力回收；维护分批仅限制单次扫描工作量，不限制记录总数；待回复工具不因数量被淘汰 |
| SSE 拼帧 | 原生/BPS 共用 CR/LF/CRLF/BOM，多行 data 用换行拼合；原生多 JSON 修复最多 16 文档/16 MiB，BPS 严格单 JSON | 保留差异；Cloud 为 WS JSON-RPC |
| Cloud 时间预算 | 复用原生 WS dial/read/write/工具 idle；使用同一首输出与整轮重试预算 | 取消旧独立拨号/初始化30秒、操作60秒与工具等待1小时；初始化取消不终止后续工具连接 |
| zstd/保活 | 原生仅指定 Codex Responses HTTPS POST 出站 zstd；BPS 不主动压缩请求；Cloud WS 压缩关闭；HTTP SSE 心跳默认 10 秒、0 关闭 | 保留；注释心跳不等于客户端有效输出，WS 桥另有保活 |

**容量取舍：** 上限不预分配，但接近上限会产生解码、JSON、输出/缓存副本。16 actor×128 MiB 保留量计数可达 2 GiB，尚未包含队列/编码副本；128 会话同时积压 64 MiB 命令可达约 8 GiB Redis 数据，另有控制预留、事件/结果及 Redis 开销，不是整个进程/Redis 内存保证。较小机器应降低现有并发/正文设置。工具纠错、图数/像素和并发不继续盲目放大。

## 5. 升级与验收

- 示例与默认启动的 SSE、非流式读体、WS 读帧/闲置/首消息参数一致；已有显式 YAML/环境变量须主动核对，升级不重写。
- BPS 后台已保存值保留；采用建议值时正文 128 MiB、relay 单图 32/合计 64 MiB，预算至少正文×8（默认 1024 MiB）。native 32/64 MiB 为代码常量，提高 relay 值不会扩 native。
- 不新增配置键、HTTP 接口、授权或协议路径；调整现有阈值并清理下游瓶颈，不改刷新权威、身份栏栅、取消及结算事实。
- 验证包括边界/超限、解压、图片双阶段、跨实例回放、Cloud 历史/工具/结果及账务；本地替身不能替代真实大图、64 MiB RPC 和端到端长会话验收。
