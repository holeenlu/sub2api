# API Key 并发限制与等待队列

本地适配来源：ranxi2001/sub2api PR #263，`cbb8ef3ed36dc4d7b4b5e8355e59d2de666660c8` 及其并发统计、升级验证和测试修复增量。共享能力先落 main，再普通 merge 到 TapModels。

## 配置入口

用户侧「API 密钥」(`/keys`) →「创建 / 编辑」→「并发上限」，以及批量编辑。字段 `concurrency_limit` 为非负整数：默认 `0` 不增加 Key 级限制，用户、账号及图片容量限制继续生效。设为正数后限制同一 Key 的同时请求数。页面独立展示活跃数、等待数、全局等待策略；统计失败展示未知或过期状态，不伪装成零。

队列策略只在进程启动时从环境变量或配置文件读取，无后台写入入口：

| 配置键 | 环境变量 | 默认值 | 行为 |
| --- | --- | --- | --- |
| `gateway.api_key_queue.max_waiting` | `GATEWAY_API_KEY_QUEUE_MAX_WAITING` | `5` | 每个受限 Key 的额外等待容量，`0` 关闭排队但保留并发限制 |
| `gateway.api_key_queue.timeout_seconds` | `GATEWAY_API_KEY_QUEUE_TIMEOUT_SECONDS` | `30` | 单次等待上限，必须为正整数 |

示例见 `deploy/config.example.yaml`、`deploy/.env.example` 和 Compose 配置。修改 `.env` 后重建容器使环境变量进入进程。扩大等待时间时应同步核对客户端、反向代理首字节超时。队列不保证 FIFO。

## 请求及运维行为

- Key 队列覆盖 HTTP/SSE、OpenAI Responses WebSocket 每轮请求、Live 创建，以及模型查询、计数等上游入口。Key 排队期间不占用户/账号槽，也不发送 SSE 心跳；用户/账号等待沿用各自策略。
- WebSocket 后续轮次先等 Key，再有界等待绑定账号，最后取得用户槽。等待时同一连接 reader 继续观察断连、待发送取消与重叠请求；全部准入后才冻结本轮利润计价时刻；模型目录的路由与价格快照仍按同一轮模型映射保真，避免等待或重试时映射与账单错配。已经发送或输出的请求不会因 Key 容量错误重放。
- 排队及 WebSocket 轮次通过现有鉴权缓存复核 Key 禁用/删除/过期/额度、用户、IP、分组、模型及实际使用的能力。绑定、平台或计费模式改变返回可重试的 `503 / API_KEY_GROUP_CHANGED`，不会把旧路由和新授权混用。鉴权缓存版本提升到 `28`，旧快照自动失效。
- Key 槽由 Redis 原子脚本分配，有请求 ID、续租和失租停上游机制。先停止并等待上游工作结束，再释放容量，避免断连释放过早导致超限。Live 原有 Key/用户/账号槽转入联合租约，避免重复计数；进程停机取消等待任务。
- 达上限且关闭排队：`429 / gateway_concurrency_limit`；队列满：`429 / api_key_queue_full`；超时：`429 / api_key_queue_timeout`。WebSocket 鉴权/权限错误关闭码 `1008`，容量或临时故障为 `1013`。Redis 不可用的受限 Key 拒绝准入；Key 级本地容量错误保留诊断，不视为上游账号故障。
- 同组模型目录发布/定价校验、实际选号分组粘连、已有容量重试预算、原生请求主库资格检查和逐次 RPM保持。没有恢复独立 BPS、Prism、Mihomo、请求采集、用户禁用模型、自动 BPS/优先调度/凭证运营。

## 升级与兼容

新增 `backend/migrations/237_add_api_key_concurrency_limit.sql`，为 `api_keys` 加 `BIGINT NOT NULL DEFAULT 0` 字段及非负约束。迁移按完整文件名记录，可与已有 `237_add_minimax_platform.sql` 并存；历史迁移不改动。应用升级时迁移框架自动执行；本次同步不执行生产迁移。旧 Key 无需配置，部署后保持原限制；要启用新限制需主动设正数。回退二进制不会自动删除新列。

没有修改依赖版本、锁文件或发版触发器；仅使用现有锁文件准备前端验证依赖。共享代码变更仍需要正常滚动部署并确认多实例使用同一 Redis；Key 限制只对已升级进程生效，须完成全实例升级后启用。默认值不同的实例不应混用不同排队策略。

代码入口：`api_key_queue.go`、`api_key_slot_lease.go`、`api_key_admission_owner.go`、`concurrency_cache.go`、`gateway_helper.go`、`openai_gateway_handler.go`、`openai_ws_forwarder_ingress.go`、`openai_live.go`、`KeysView.vue`。本轮验证结果见 项目对应提交的验证记录。
