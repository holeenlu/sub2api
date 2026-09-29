# Excel / BPS 增量：容量上限、配置模板与 OAuth 保护

## 范围与来源

本轮归属共享能力：先落 KDAN 主分支 `main`，再普通 merge 到 `TapModels`，保留来源提交和品牌差异。本轮仅本地同步、验证和提交，不推送、不发版、不运行生产迁移。版本号不变。

固定审查范围：ranxi2001/production `e0b227cc07dfb7094f1c01889c9fdb9f1d391374..df4597682ff66ba1920385a3c2985124d9816010`。PR #204 已于 2026-09-29 13:35:53（UTC+8）合入源仓库，合并提交 `90ed3a52889e90d53aae1d6f2bb19bb9e18a2f4b`；功能提交 `ab65ecba0cc8bef900de61f70ca7e6b8335ddd9e` 已是固定源 tip 的祖先。同步前，本项目仍使用旧的 128 MiB 请求体和 2048 MiB 预算上限，功能确实尚未完整引入。

以下来源作者均为 ranxi2001 `<ranxi169@163.com>`。采用选择性适配，不能把功能等价描述成这些源 SHA 已成为本地祖先。

| 来源 SHA | 本地结果 |
| --- | --- |
| `ab65ecba0cc8bef900de61f70ca7e6b8335ddd9e` | #204 图片参数允许上限：前端输入/保存校验、后端 API/运行时/准入完整接通 |
| `6de5d485e3e8bd3ddd32fb88e32a36d5c8491f1c` | 默认模板与初始配置模式，独立保存模板、管理员手动应用；剥离通用自动配置与代理管理依赖 |
| `83f9ff9e83b0e35bb3ea2c66a93e02f05ce83f80` | 默认关闭的 OAuth HTTP 流式 WS 加速；保留本地主库资格、逐次 RPM 与 BPS 优先路由 |
| `9515861c6f98b588b34abefe71b854390fde3fdd` | Free 套餐禁止 BPS，覆盖管理员写入、运行时、自动规则与本地 403 恢复任务 |
| `4b24362edc0d05a4c998877f6fb9a59505fa809a` | 套餐刷新/Free 降级适配现有重新登录 V2 和 CAS；不恢复旧凭证守护 |

## 优化改进：图片容量上限

入口：**系统设置 → 功能开关 → Excel/BPS 图片支持**，路由 `/admin/settings`（选择功能开关标签）。这是现有参数扩大允许范围，**不改变任何已保存值，也不提高默认值**。只更新允许范围无需配置，部署后自动生效；需要扩大实际容量时由管理员按资源手动保存。

| 后端设置键 | 原允许上限 | 新允许上限 | 保持的默认值 |
| --- | ---: | ---: | ---: |
| `excel_bps_image_body_limit_mib` | 128 MiB | 1024 MiB | 64 MiB |
| `excel_bps_image_budget_mib` | 2048 MiB | 65536 MiB | 1024 MiB |
| `excel_bps_image_max_requests` | 512 | 4096 | 128 |
| `excel_bps_image_max_image_mib` | 128 MiB | 512 MiB | 20 MiB |
| `excel_bps_image_max_total_mib` | 128 MiB | 512 MiB | 32 MiB |
| `excel_bps_image_max_images` | 4096 | 65536 | 20 |
| `excel_bps_image_storage_mib` | 16384 MiB | 262144 MiB | 1024 MiB |
| `excel_bps_image_storage_entries` | 65536 | 1048576 | 512 |
| `excel_bps_image_ttl_minutes` | 1440 分钟 | 10080 分钟 | 30 分钟 |
| `excel_bps_image_warning_remaining` | 4096 | 65536 | 8 |
| `excel_bps_image_compact_reserve` | 4096 | 65536 | 3 |

保留约束：预算至少为请求体的 8 倍；实际准入预算还受每并发请求最低预留影响；缓存容量不得小于单次请求图片容量，缓存条目不得小于单次张数，预警策略仍满足 reserve < warning < limit。上限本身不是预分配大小。图片 relay 缓存按进程计，管理员增大实际预算会提高内存压力；不能把准入预算当作进程 RSS 的绝对上限。

原生附件仍保留单张 20 MiB、单次 32 MiB 和解码像素保护，本次只放宽其可配置张数以及入口准入参数；不改变上游协议的真实限制。`server.max_request_body_size`、`gateway.max_body_size` 和反向代理请求体上限仍可能先行拦截，必须一并核对。

代码：`basispoints/image_capacity.go`、`basispoints/image_relay_limits.go`、`setting_excel_bps_image.go`、`setting_handler_update.go`、`middleware/excel_bps_image_admission.go`；前端统一常量 `utils/excelBPSImageLimits.ts` 与 `SettingsView.vue`。后端相对路径位于 `backend/internal/`；前端位于 `frontend/src/`。

## 新增功能：手动 BPS 模板

模板管理：**智能运维 → 自动 BPS → BPS 默认配置**，`/admin/account-quality#bps-defaults`。后端新增设置键 `excel_bps_defaults`，GET/PUT `/api/v1/admin/settings/excel-bps-defaults`，仅管理员可访问，复用现有 settings 存储，无新迁移。未保存模板时只返回推荐内容，不落库、不改账号。模板读取失败时禁止用空白/默认值覆盖旧配置。

账号使用：**账号管理 → 编辑/批量编辑 → Excel/BPS 协议**，`/admin/accounts`。新增“默认配置”与“初始配置”激活模式；管理员明确点击才写入当前表单，仍须保存账号才生效。打开已有账号、读取模板、保存模板本身都不批量改账号，也不自动配置新账号/导入账号。`accounts.extra.openai_excel_bps_config_mode` 仅记录表单模式；真正的运行时仍使用既有 `openai_excel_bps*` 字段。

推荐模板默认选中忽略历史加密、403 自动关闭、缓存创建按输入计费；其他可选行为保持关闭。选择“初始配置”明确重置本次表单的可选项为关闭，并保留默认模型候选。编辑已有设置不自动重置；403 自动关闭后的配置在未主动选择新模式时保留。模板请求失败、弹窗关闭或切换账号时不把过期结果写入新账号。

本地取舍已按用户“继续”所承接的推荐范围实施：不引入源仓库整个自动配置中心、Mihomo、会话代理默认值或分组规则模板；不恢复自动并发升档。目标分组的实际资格在保存账号时沿用现有校验。

代码：`service/setting_excel_bps_defaults.go`、`handler/admin/setting_handler_excel_bps_defaults.go`；前端 `BPSDefaultsPanel.vue`、`BPSDefaultsCard.vue`、`useExcelBPSDefaults.ts`、两个账号编辑弹窗。

## 新增功能：可选 OAuth WS→SSE 加速

入口：**账号管理 → 编辑普通 OpenAI OAuth 账号 → HTTP 流式 WS 加速**，`/admin/accounts`；字段 `accounts.extra.openai_oauth_ws_sse_acceleration`，新增开关默认关闭，需手动开启。WS mode 与全局 `gateway.openai_ws` 的已有开关、force_http、插件/透传优先级仍有效。只覆盖符合条件的 HTTP 流式 Responses；BPS 生成请求继续优先走 BPS，本开关用于普通 OAuth 路径及适用的原生回退。

只在握手失败且尚未发送请求时允许 HTTP 回退，不因认证/权限/限流错误绕过控制，不在发送后重放；前置元数据立即刷出但不计为首 token。详见 [实现与条件](openai-ws-sse-acceleration.md)。

## Bug 修复：Free 套餐与重新登录

**无需配置，部署后自动生效。** 当凭据中 `plan_type` 明确为 Free（忽略大小写及空白）时，管理员不得启用 BPS，运行时、自动 BPS 和 403 自动恢复均拒绝启用。已有 Free 账号即使历史标记为 true 也不会走 BPS。未知套餐不被擅自当作 Free。

现有 **智能运维 → 凭证运维**（`/admin/token-guard-v2`）的重新登录 V2 回调继续从 ID token 更新套餐；确认降级 Free 时通过既有凭据/extra 原子 CAS 同时关闭 BPS，不覆盖操作员并发暂停/编辑。付费套餐不自动开启 BPS，空套餐信息保留已有套餐。旧凭证守护、第三方默认地址与第三方自动重新登录开关不恢复。

## 明确排除的源增量

| 来源 SHA | 排除理由 |
| --- | --- |
| `ba2340a44ccfaf34b16ca24c73759c476c4679bd` | 通用账号自动配置历史；本地没有引入该自动配置/并发升档子系统 |
| `2556d89857fcdba14d6ad8b558dcf564d445cbfb` | 网页创建凭据加密密钥涉及独立的密钥生命周期；非本轮 BPS 必需，保留现有 V2 固定加密配置与权限 |
| `894c5d7974e45816449d3d73bd1dc80254b39415` | 上项源截图，不是本地验证 |
| `07eb4246967461c42b91a1c471a026035620dacd` | 通用质量规则“所有匹配账号”与查询能力；本地最小自动 BPS 仍按明确账号配置 |
| `deaa581884151b2ffadb071f5267fb1933879b00` | 分组质量规则模板与未来账号自动创建规则、迁移 260；超出最小 BPS 依赖 |
| `8183f68fa5c1f4f05a09070e2937bf244c7296b0` | 已退役旧凭证守护的列表与 2FA 操作 |
| `cdab8bbb4e799bb4c85f8f87f4a2e88accd4256a` | 源版本号 2.9.2，不沿用源发版 |

合并包装提交另用 remerge-diff 审查，`5b619d7476e90ea36b240afa6d87f752914c919c` 的额外解决只涉及源通用自动配置日志与语言块拼接；相关 BPS 内容已保留，不导入无关自动配置日志。未恢复 sub4api 独立 `openai_bps` 平台、请求采集、Mihomo 或打票扩展。

## 验证与交付边界

- Go 全量单元测试（`go test -p 2 -tags=unit ./...`）与 `go vet -p 2 -tags=unit ./...`。
- BPS 图片准入/relay、WS→SSE、重新登录和模板服务的 `go test -race`；最大计数/预算测试使用计数预留，不分配数十 GiB 内存。
- Docker 中 PostgreSQL 18.1 与 Redis 8.4，运行 repository 的 BPS/ExcelBPS/Reauth 集成测试，34 个顶层测试通过：覆盖规则与恢复租约、Free 资格、凭据原子更新/CAS、真实 Redis 图片策略。
- main 前端全量测试 376 文件 / 3030 用例通过，类型检查、ESLint、生产构建、繁体生成一致性通过；中英文更新，zh-TW 从简体生成。
- 品牌/手动发版契约检查；没有工作流、版本、依赖、Ent/Wire 输入或数据库结构变更。
- 未连接真实 OpenAI 账号、未测真实 WS 性能、未进行上限规模内存压力测试或生产部署。大容量设置需管理员结合机器资源逐步调整。

具体通过数量与两个本地交付提交见本轮交付消息。TapModels 通过普通 merge 保留主分支提交；相同 backend 树可复用已通过的 Go 验证，品牌前端独立检查。
