# 账号遥测：设备包导入、事件生成与发送

2026-10-20 最终需求：只接收管理员导入的设备安装资料。移除服务器自行初始化、手工生成、客户端遥测上传采集和样本库。工具包仅供本地设备使用，不在网关网站提供下载、安装或生成入口。

## 唯一资料来源

本地独立工具在 Windows / Linux / macOS 上读取官方 Claude Code 真实安装 ID、机器 ID、CLI 版本和设备环境，导出 version 2 JSON。不要求 Node.js 或账号登录；没有官方原生 CLI 时，工具从官方 latest 渠道下载、校验并安装。缺少安装身份时，仅在导出设备的独立配置目录内免登录启动 CLI 获取官方实际写出的 ID。

包不包含账号、访问令牌、刷新令牌、会话、任意事件或请求头。工具的安装和初始化只发生在用户运行工具的设备上。使用说明随本地工具包提供，仓库维护说明见 [本地设备导出](ACCOUNT_TELEMETRY_DEVICE_EXPORT.md)。

新建 Anthropic OAuth / Setup Token 账号显示“等待导入”；未导入账号不生成、不开启遥测活动、不向上游发送。普通 API Key 和其他平台不参与。

超级管理员通过账号编辑页导入或替换 JSON，唯一写入入口为 POST /api/v1/admin/accounts/:id/telemetry/import，最大 1 MiB。后端拒绝未知字段、账号/令牌/会话字段、任意事件、非法环境及旧包版本。普通账号创建/编辑/批量更新不能写入遥测运行状态。

## 保存及账号绑定

accounts.extra.account_telemetry 只保存：

- status：waiting_import / ready。
- revision：导入或重新绑定版本，供并发保护使用。
- device_installation：唯一一份原始 version 2 设备资料。
- owner、session_id：网关维护的账号身份摘要及稳定会话。
- imported_at：本次管理员导入时间。

不再保存完整事件副本、profile、payload_json、请求头、初始化错误或重试状态。账号 UUID、组织、邮箱、订阅每次从当前网关账号获取；动态事件及请求头按需构造。设备包可在多个账号间复用，账号和会话独立。

导入立即绑定并保存，无需服务端 CLI 或模型请求。账号身份发生变化时，保留设备资料并分配新的会话和 revision；这个纯本地绑定不受发送开关影响。更新使用凭据快照与旧 revision 比较并交换，复用 scheduler outbox 发布账号快照；过期任务不能覆盖新导入或向上游发送旧资料。

推理请求指纹和提示词环境也直接使用导入设备事实；没有包时维持原生网关指纹行为，不因此伪造或发送账号遥测。

## vm2api 上游事件契约

对照 vm2api 819ee2a7c40b49fe81ebc3823e20373350a20545 的事件、进程、环境及周期生成逻辑。设备事实来自导出设备，账号来自网关；这两者替代原版 slot/官方登录环境的来源，事件生成结构沿用原规则。

事件使用 events[] → event_type: ClaudeCodeInternalEvent → event_data。

| 部分 | 规则 |
| --- | --- |
| 初始化事件 | tengu_init、新 UUID v4、UTC 毫秒时间戳、is_interactive:false，不带 process |
| 后续事件 | tengu_api_success、新 UUID v4/时间戳、is_interactive:true，每次新 process |
| 固定属性 | entrypoint:cli、client_type:cli、user_type:external |
| 身份 | 导入的官方 device_id、当前账号 UUID/organization/email、真实出站会话优先，否则账号绑定会话 |
| 环境 | 完整默认字段和空字段；version_base 在第三个点前截断；不加入 locale/timezone；有事实才带安装时间和硬件；Linux deployment_environment 为 unknown-linux，其余为空 |
| 进程 | JSON 后标准 Base64；rss [300M,500M)、heapTotal [40M,80M)、heapUsed [100M,200M)、external [1M,3M)、arrayBuffers [10k,50k)；CPU user [50k,500k)、system [15k,150k)、cpuPercent [0.5,5)，constrainedMemory=0 |
| uptime | 账号遥测 worker 运行时长 |
| GrowthBook | 独立 POST /api/eval/sdk-zAZezfDKGoZuXXKe，attributes、forcedVariations:{}、forcedFeatures:[]、url:""；可选账号属性有值才输出 |
| 请求头 | claude-code/<导入CLI版本>、设备环境白名单、实际会话；不带 Anthropic-Beta |
| 授权 | 发送时读取当前账号 access token，仅放 Authorization: Bearer，不写入包或事件正文 |

process 是 vm2api 规则下的合成指标，不是实时测量本机 CLI。周期 tengu_api_success 不是客户实际请求成功、账务或 SLA 的证据。首次 tengu_init 是本地构造的上游事件，不会启动 Claude Code 或发起 hello 推理。

## 发送生命周期

- “运维监控设置 → 高级设置 → 账号遥测定时发送”默认关闭，沿用 ops_advanced_settings.account_telemetry_sending_enabled。
- 开启后，启动、开关从关闭到开启、导入/重新绑定成功、实际模型 dispatch 激活十分钟窗口。只有有效已导入账号进入窗口。
- 窗口内初始化事件一次、后续事件约十秒一次、GrowthBook 首次及之后六小时一次；无活动超过十分钟停止，不补发积压。
- 关闭时取消发送任务，保留设备、会话和发送时间戳；再开启不补停用期间事件、不重置六小时 eval 时钟。已抵达上游的请求不能撤回。
- Redis 负责 leader 选举、账号锁和发送时钟；并发/超时限制继续保留。领导者切换按现有 worker 启动规则处理，高负载时十秒是目标间隔。
- init 4xx 停止本轮，五分钟后可重启；后续 batch/eval 4xx 分别停止对应项；其他错误等待下一轮。发送失败不换号、不惩罚账号、不产生客户账务。
- 保留账号资格、供应权限、revision/owner 复核以及账号原生代理、中继、TLS 出口。令牌获取使用现有账号凭据机制。
- 配置保存后当前实例更新；其他实例沿用 Ops 设置刷新（30秒及20%抖动），存在同步延迟。

## 已移除范围与历史数据

删除服务器 CLI bootstrap、临时 loopback relay、服务器环境采集、私有初始化目录及文件读写、初始化错误码/五分钟重试、手工生成 API/权限/按钮及轮询、CLI 路径/home 配置、镜像 CLI 安装阶段与发布上下文、专属测试。服务端镜像不再因账号遥测安装官方 Claude Code。独立设备工具保留自动安装能力。

旧客户端采集 handler/service/repository、OTLP /v1/logs /v1/metrics、样本查询及选择器、去重计数/清理任务和客户端归属历史缓存继续保持移除。原事件入口只返回空200，不读写正文。

已保存的合法 version 2 device_installation 继续使用；首次后台扫描为缺少绑定的旧设备包补入网关会话，并以新状态覆盖该账号遥测字段，不读取旧事件 profile。只有服务器 profile、旧 version 1 或客户端副本的账号等待重新导入；不再将它们转为有效来源。没有保留 profile-only 的执行回退或旧包转换。

旧数据库 JSONB 中未访问/未替换的废弃字段、历史样本表及服务器磁盘安装目录可能仍存在，但不参与此链路。API 响应只投影当前状态，旧客户端私密字段继续过滤。历史迁移282/283不改写；本次不执行生产数据删除、数据库迁移或服务器目录清理。旧 Redis 初始化字段不再读取，过期或下一次运行状态写入后消失。

## 验证边界

固定 vm2api 实际运行生成的 oracle 比对完整 init/success/eval/env JSON；动态 UUID、process 字段和范围另行检查。测试覆盖实际本地导出包的导入、三平台、账号切换、并发 revision、发送开关/周期和已移除采集路径。

隔离测试使用内存上游与测试凭据，不证明真实 Anthropic 接收或风控效果。生产部署、发版、数据库迁移及历史数据删除不由代码提交推送触发。
