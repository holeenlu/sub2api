# 账号遥测：生成、生命周期与退役范围

2026-10-20 用户最终要求：移除 Claude Code / Claude 客户端上传遥测及样本保存；移除账号页“从已保存遥测选择”；创建账号始终自动生成，账号页始终可手工重新生成；独立的定时发送开关默认关闭，仅开启时按 vm2api 规则向上游发送。本文件替代此前按客户端上传触发的方案。

## 生成来源

- 对照 vm2api `819ee2a7c40b49fe81ebc3823e20373350a20545` 的 `worker/internal/telemetry/event.go`、`run.go`、`client.go` 与 `src/lib/identity/telemetry-env.mjs`、`worker-telemetry.mjs`。本地 vm2api `3019d75b` 的这些生成文件未变化。
- 新建 Anthropic OAuth / Setup Token 账号在原账号创建事务中写入服务端管理的 `extra.account_telemetry` 待生成状态。普通 API Key 和其他平台不进入此生命周期，与 vm2api 的 OAuth 遥测条件一致。
- 官方 CLI 在账号隔离目录运行一次 hello；禁用工具、插件/自定义配置、MCP、会话持久化和 CLI 自带遥测。没有常驻 CLI、自动登录流程或第二个 refresh-token 写入器。
- CLI 只获得账号 access token；初始化请求通过临时 loopback 入口进入原账号的代理、自定义中继和 TLS 发送路径，支持原有 SOCKS 出口。loopback 只允许认证后的 messages/count_tokens，至多一次 hello 推理和两次 count_tokens。它不是公网路由。
- 仅读取官方 CLI 实际写出的 `.claude.json` 安装 `userID`（64 位十六进制）及 `machineID`。缺少身份、CLI 不可用或 hello 失败时，记录错误并暂停发送；不拿原网关随机指纹冒充官方安装身份。初始化失败五分钟后可重试，管理员也可手工生成。
- 观察 CLI 运行环境，保存发行版、内核、shell、包管理器、运行时、安装完成时间及可读取的 CPU、内存、根盘、MAC、DMI 字段。不可观察的硬件不捏造；这些值来自本部署的实际环境，不等于 vm2api 某台 VM 的具体硬件值。
- 官方身份、安装环境及稳定会话持久化；账号生成结果保存于原 accounts.extra，不新增数据库表或迁移。手工重新生成保留已有有效安装身份，刷新生成资料，不复制客户端事件。
- 生成结果带 revision、账号身份摘要、状态、错误码和生成时间。写入同时校验 revision 与账号凭据快照，防止初始化完成后覆盖重新导入或手工重建。普通编辑、批量编辑与 extra 更新不能覆盖运行态资料；使用现有 scheduler outbox 发布快照。

## 上游数据契约

事件使用 `events[] → event_type: ClaudeCodeInternalEvent → event_data`。

| 部分 | 规则 |
| --- | --- |
| 初始化 | `tengu_init`、新 UUID v4、UTC 毫秒时间戳、`is_interactive:false`，不带 process |
| 后续事件 | `tengu_api_success`、新 UUID v4 和时间戳、`is_interactive:true`，每次生成新的 process |
| 固定属性 | `entrypoint:cli`、`client_type:cli`、`user_type:external` |
| 身份 | 官方 device ID；已知实际账号的 UUID、organization UUID、email；真实出站会话优先 |
| 环境 | 完整默认字段和空字段；version_base 在第三个点前截断；env 不加入 locale/timezone；有事实时才放 build_time 和硬件字段 |
| 进程 | JSON 再标准 Base64；rss [300M,500M)、heapTotal [40M,80M)、heapUsed [100M,200M)、external [1M,3M)、arrayBuffers [10k,50k)；CPU user [50k,500k)、system [15k,150k)、cpuPercent [0.5,5)；constrainedMemory 为 0 |
| uptime | 账号遥测 worker 的运行时长，不使用全网关进程启动时长 |
| GrowthBook | 独立 POST `/api/eval/sdk-zAZezfDKGoZuXXKe`；attributes、forcedVariations:{}、forcedFeatures:[]、url:""；可选账号/订阅属性仅在有值时输出 |
| 请求头 | 事件/eval 使用 `claude-code/<version>`；环境白名单、实际会话和账号 Bearer；不带 Anthropic-Beta 或客户端认证数据 |

process 是 vm2api 规则下合成的指标，并非真实 CLI 进程测量。`tengu_api_success` 是该生成器的周期事件，也不是客户请求成功、计费或 SLA 的证据。客户上传的事件名、时间、ID、错误、用量和其他属性均不再进入发送内容。

## 生命周期

- “运维监控设置 → 高级设置 → 账号遥测定时发送”默认关闭，复用 `ops_advanced_settings.account_telemetry_sending_enabled` 持久化和现有设置接口；旧配置缺少该字段时按关闭处理，不读取已退役的客户端采集开关。自动初始化（含官方 CLI hello）、身份更新后的重新初始化、失败重试和手工生成始终执行，不受发送开关影响。
- 仅发送开关开启时，启动、从关闭切换为开启、生成成功或实际模型 dispatch 激活十分钟窗口；无活动超过十分钟时停止事件与 eval。旧的“只有客户端上传才发送”已按最新要求撤销。
- 窗口内初始化事件一次，后续事件约十秒一次，GrowthBook 首次及此后六小时一次。空闲期间不补发积压；再次活动时根据当前时钟判断是否到期。
- 关闭开关不删除安装身份、账号资料或发送时间戳，停止窗口激活及事件/eval 调度，并取消正在进行的发送；不取消初始化。重新开启恢复固定周期，不补发停用期间的事件，也不会重置六小时 GrowthBook 时钟。已抵达上游的请求无法撤回。
- 当前实例保存后更新内存设置，发送前复核开关，调度器每秒检查并取消发送。其他实例沿用现有 Ops 设置后台刷新（30 秒、20% 抖动），因此跨实例生效有同步延迟；无需重启。
- 多实例以 Redis 选举活动 worker，并使用账号级有期限锁。领导权接续重新启动账号 worker；并发实例不同时初始化或发送同一账号。发送任务受并发和超时限制；高负载时十秒是目标间隔，不保证硬实时。
- init 4xx 停止该轮 worker，五分钟后再启动；后续事件/eval 的 4xx 分别停止对应发送项；其他错误留待下一轮。发送失败不会换号、给账号加限流处罚或产生客户账务。
- 发送前复核账号状态、有效期和原生供应权限。账号身份改变时停止使用旧资料并重新生成；无法确认属于该账号的客户端 metadata 不进入活动会话，继续使用账号安装会话。环境与请求头始终来自账号资料。

## 部署与界面

Docker 构建新增官方 Claude Code 2.1.296 runtime 阶段，AMD64 / ARM64 musl 包固定版本和 SHA-512 完整性值，构建时下载，运行时不下载。镜像会增加约 250 MB 的官方 CLI 可执行文件。首次 hello 使用账号自身的额度和出口。

```yaml
gateway:
  account_telemetry_cli_path: /usr/local/bin/claude
  account_telemetry_home_dir: /app/data/account-telemetry
```

以上为默认值。非 Docker 部署需安装官方 CLI，并通过配置或 `GATEWAY_ACCOUNT_TELEMETRY_CLI_PATH`、`GATEWAY_ACCOUNT_TELEMETRY_HOME_DIR` 提供绝对路径。私有目录为 0700，环境资料为 0600；根目录应位于服务的数据卷。安装资料与数据库生成结果均需纳入原有备份范围。

超级管理员在账号编辑页查看待生成、已生成或错误状态，点击“手工生成 / 重新生成”。唯一新增公网管理入口为 `POST /api/v1/admin/accounts/:id/telemetry/generate`，读取状态复用原账号详情接口。创建账号只显示自动生成说明；没有样本选择、上传或手工输入环境字段。

已有账号不会把旧样本副本自动升级成官方资料；可在账号页手工生成。新建及导入的新账号自动进入生成队列。

## 清理与历史数据

删除客户端采集 handler/service/repository、OTLP HTTP 解码与专用依赖、样本查询 API 和权限条目、样本页/选择器、运维采集开关、去重/计数/清理任务、客户端会话归属历史缓存及对应失效测试。原事件入口恢复原生空 200，不读取、保存或转发正文；`/v1/logs` 和 `/v1/metrics` 不再是采集入口。

历史迁移 282/283 保持不变。数据库旧样本表、旧 accounts.extra.client_telemetry、旧设置 JSON 和 Redis 历史键不作为新功能输入；历史表和资料未删除，旧 Redis 有期限键自然过期。本次不执行生产数据库清理或迁移。API 响应继续屏蔽已退役的私密副本字段，防止旧数据随普通账号资料暴露。

## 验证边界

`testdata/account_telemetry_vm2api.json` 由固定来源的 JS 环境构造函数及 Go 事件/eval 构造函数实际运行生成。回归测试对比完整 JSON，动态 UUID 与 process 单独检查格式、刷新、字段及范围。

官方 CLI 离线联调使用真实可执行文件、临时私有 HOME 和内存上游响应，不使用真实账号凭据或外部模型请求。它验证安装身份产生和接入链路；不证明真实 Anthropic 接收、风控效果、生产额度或代理网络状况与 vm2api 相同。生产部署、发版和历史数据删除不是本次提交推送的范围。
