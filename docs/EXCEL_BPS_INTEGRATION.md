# Excel / BPS 整合记录（仅 ranxi2001 模式）

当前范围更新：2026-09-28；初次整合日期：2026-09-27。归属：主线能力，先提交到 `main`（KDAN 品牌主分支），验证后普通 merge 传播至 TapModels。

当前只保留 ranxi2001 的 OpenAI OAuth Excel/BPS 协议及必要依赖。sub4api 的独立 OpenAI BPS 平台已移除。移除、升级迁移和最新源提交审查见 [2026-09-28 范围调整](BPS_RANXI_ONLY_2026_09_28.md)。

后续协议增量见 [2026-09-28 ranxi2001 同步记录](EXCEL_BPS_SYNC_2026_09_28.md)。

## 接入入口

| 项目 | 保留的 OpenAI OAuth Excel / BPS 协议 |
| --- | --- |
| 配置路径 | 账号管理 → 编辑 OpenAI OAuth 账号 → Excel / BPS 协议；支持批量编辑 |
| 凭据 | 复用 OpenAI OAuth 凭据及刷新逻辑 |
| 平台与分组 | `openai`，沿用现有分组、优先级与粘性会话 |
| 协议 | 选定模型使用 HTTP/SSE BPS；范围外模型沿用现有 Codex 路径 |
| 工具与图片 | function/custom/namespace、完整工具历史、结构化输出、HTTPS 图片及原生附件 |
| 状态 | 进程内工具目录、回放和附件元数据；完整客户端历史可重建 |
| 使用说明 | [Excel / BPS](excel-bps.md) |

这是网关对 BPS 协议的适配，不包含 Microsoft Excel 桌面插件安装或工作簿编辑功能。

## 当前保留的能力

- Excel/BPS 账号开关及模型范围；作用于映射后的上游模型。未提供模型范围表示全部模型，空数组表示不选任何模型；范围外模型仍可走现有 Codex 通道。
- 文本与 SSE、compact、function/custom/namespace 工具、JSON Schema 校验、完整工具结果回放、线程隔离、上下文目录增量合并，以及受限的工具封装修复和无效加密 reasoning 重试。
- 缓存创建用量转普通输入的可选计费规则；BPS 限流独立处理，客户端取消保留真实耗时并从 SLA 错误中排除。
- 通用 BPS 403 时可选自动关闭协议、调整分组或退出所有分组；执行前重新检查凭据与策略，事务内写入调度通知，并发失败仅做一次有效修改。
- 管理员可主动运行“BPS 工具往返”探测，核对随机文本、echo 工具和回传结果；不自动定时调用上游。
- 可选择省略不支持的托管工具、图片关闭时忽略图片、忽略历史加密消息。均默认关闭，省略行为向模型明确说明，不宣称具备被省略的能力。
- 图片容量控制、原生附件流式上传及作用域缓存、临时 HTTPS 图片中转与过期回收、请求体预算和并发准入；繁体、简体、英文、日文界面已对齐。

## 与当前项目的适配

1. 保留现有账号调度优先级及会话绑定。未采用上游“同组存在 BPS 账号就优先抢占原生 Codex 粘性账号”的策略。
2. 必须依赖原生能力的声明（live 搜索、高搜索上下文、图片生成、强制指定这些工具）默认由同一账号改走原生 Codex 通道，响应头 `X-Codex2API-Upstream: codex`；管理员开启省略选项后才留在 BPS 省略。Codex 默认的 cached 搜索始终留在 BPS 省略。2026-09-28 恢复了上游 c23e18642 的同账号回退，取消整合时加的 400。
3. BPS 主请求、重试、工具纠错和原生附件上传前均重新检查主库账号状态。账号停用、移出分组、凭据/代理/BPS 模型范围改变或主库不可读时停止发送；这不是跨 SQL 与网络的原子事务。
4. BPS 路径不注入 Codex 票据、Cookie 或指纹头，选中的 BPS 模型也不参与 Codex 打票。未选中的原生模型继续使用本项目的打票、出口/会话绑定与验票逻辑。BPS 配置变更会使旧 WebSocket 绑定失效。
5. 保留现有登录协议编辑器、账号调度阈值、平台模型白名单与品牌配置。移除源补丁中夹带的 Mihomo、鹈鹕/质量运营和请求采集代码，不恢复其他已退役 fork 功能。
6. 补齐工具省略选项的后端批量校验，并在批量关闭协议时清理图片/加密历史/工具省略选项，避免再次开启时意外沿用旧行为。
7. 使用本项目模块路径及当前仓库接口；ent 和 Wire 重新生成后与整合代码一致。

## 默认值与升级

新增 `254_retire_standalone_bps.sql`，只停用旧独立平台的账号、分组、路由、监测和定时测试，保留凭据与历史。历史迁移不修改；普通 OpenAI OAuth 的 Excel/BPS 设置不变。

旧独立平台不可重新启用或自动改成 OAuth 账号。管理员如需继续使用，须新增或选择可用的 OpenAI OAuth 账号，在「Excel / BPS 协议」中配置，并把客户端 API Key 绑定到可用 OpenAI/Composite 分组。本次未操作生产数据库；上线启动时会执行尚未应用的迁移。

图片开关位于“系统设置 → 功能开关 → Excel / BPS 图片支持”，新安装或历史上未保存该键的配置默认开启并使用原生附件；显式保存的关闭值继续保持关闭。打开后可选择原生附件或 HTTPS 中转；后者需填写本站公网 HTTPS origin，并将 `/api/bps-images/` 固定到生成链接的实例。临时链接的持有者可在有效期内取图。账号级图片忽略兼容开关仍保留，只有明确配置时才生效。

原生上传的单图和合计大小固定为 20 MiB / 32 MiB，图片数可配置（默认 20）；附件缓存最多 512 条、30 分钟。HTTPS 中转的大小、数量、磁盘配额和有效期可配置，见 [图片中转参数](bps-image-relay-limits.md)。默认 HTTP 请求体 64 MiB、估算预算 1024 MiB、在途上限 128；实际预算取配置值与在途上限 × 8 MiB 的较大值，详见 [容量联动](excel-bps-capacity-validation.md)。这不是进程 RSS 上限或真实上游吞吐保证。

## 代码位置

| 职责 | 文件/目录 |
| --- | --- |
| Excel/BPS 转发、计费、诊断、探测 | `backend/internal/service/openai_excel_bps*.go`、`account_test_service_bps_probe.go` |
| 工具/历史/结构化输出/图片协议 | `backend/internal/service/basispoints/`，协议出处和许可证见该目录 `NOTICE.md` |
| 403 原子处理和分组 | `backend/internal/repository/account_repo_excel_bps*.go` |
| 图片设置和入口准入 | `backend/internal/service/setting_excel_bps_image.go`、`backend/internal/server/middleware/excel_bps_image_admission.go` |
| 打票与准入边界 | `backend/internal/service/openai_codex_ticket*.go`、`openai_turn_admission.go` |
| 账号界面 | `frontend/src/components/account/` 的创建、编辑、批量编辑、连接测试和 OAuth RPM 状态组件 |
| 系统设置、使用说明和语言包 | `frontend/src/views/admin/SettingsView.vue`、`components/keys/UseKeyModal.vue`、`i18n/locales/` |

## 历史验证（2026-09-27 初次整合）

以下为初次整合记录，不代表当前移除版本的验证结果；当前结果见范围调整记录。

当时主线分支本机验证：

- `go test -tags=unit ./...`：60 个测试包通过。
- `go test -tags=integration -exec=/usr/bin/true ./...`：全仓库集成测试编译通过；该命令只编译，不执行测试。
- 临时 PostgreSQL 18.1 / Redis 8.4：BPS、退役状态及轮次准入筛选的 17 个顶层测试通过；另行通过 `TestAccountRepoSuite/TestBulkUpdate_ExcelBPSModelScope` 与 `TestOpsClientCancellationMetricsAndDuration`。覆盖并发 403、分组优先级保留、配置撤回、旧凭据、调度通知、批量关闭清理及取消耗时/SLA SQL。
- `go test -race -tags=unit ./internal/service ./internal/repository ./internal/server/middleware -run 'BPS|Bps|Excel|OpenAITurnAdmission|CodexTicket' -count=1` 通过；`go test -race ./internal/service/basispoints -count=1` 通过。
- golangci-lint 的本次增量检查为 0 问题（unit、integration 标签）；全量检查仍有仓库基线告警，不能写成全仓库 lint 零告警。
- 前端 Vitest：364 个文件、2895 个用例通过；类型检查、ESLint、生产构建及繁体生成检查通过。
- `go generate ./ent ./cmd/server` 后无额外差异；`go build -tags=embed ./cmd/server` 通过；历史 SQL 无 diff。

没有向真实 BPS/OpenAI 账号发送请求；实际账号权限、上游模型与工具/图片效果仍未验收。没有手工发布镜像或操作生产数据库。

## 历史移植与出处（保留可追溯性）

基线 `aaf4cf9a35dd36a5d047ea768fbe44dce5abe78d` 已移除两个 fork 的非打票功能。直接合并完整 fork 或反转退役提交会恢复本次范围外的功能，因此采用带 `-x` 来源记录的选择性补丁适配，并把相互依赖的补丁整合提交。下表保留来源 SHA 与作者；不声称这些来源 SHA 成为本次提交的祖先。公共提交向品牌分支使用普通 merge，保留新的公共 SHA。

核对的源分支：sub4api/main `ca30f2141bc9ced4cbcfa2ef336f795a94c133e2`（v1.1.5），ranxi2001/production `f80611d6cd7fa7d225b7c8767fcd555b75f8745e`。复用了之前适配树 `3e9da9822` 中 BPS 必需的 SSE 读取泵；只保留现有转发实际使用的读取生命周期逻辑。

| 来源提交 | 原作者 | 功能/修复 |
| --- | --- | --- |
| `200de9c989b40c0c63cf89ae0cf38a211c14d158` | Jobs | 独立平台，2026-09-28 已移除 |
| `775744a66771550aecf64757a177e2e4fb780344` | Jobs | 独立平台凭据诊断，2026-09-28 已移除 |
| `5bb245bcc743fcbf0488b439864ac68b78796379` | ranxi2001 | feat(openai): add Excel / BPS protocol for OpenAI OAuth accounts |
| `baa3336b9cd371467e90a5ab2b701d61fc942351` | akihito | feat: 支持 BPS 403 时自动关闭协议 |
| `b36b139c5795d3a4b5dea3918df9738d7f730e44` | akihito | test: 清理 BPS 并发测试的调度事件 |
| `e034742e773273dac51aad51b98edcb412cc9ea8` | akihito | fix: 修复 BPS 代码工具的嵌套转义问题 |
| `27cd3e16b9d6dd86a5854a38fe2828f35dbb78a8` | akihito | fix: 补齐 BPS 用量快照和 429 冷却 |
| `59221d67d178a6894c82d51b988b5e865aeb2339` | 周涛 | feat(bps): configure image admission capacity using merged PR #91 |
| `46437b69cd351d3e47812c2b291b9fe5bd20422c` | ranxi2001 | feat(bps): configure image relay limits in admin settings |
| `e450f312ecd58082e0adf508cbaec5ecc0dcd42f` | akihito | fix(bps): 对齐工具历史回放和图片参数校验 |
| `5b1d7ec3e93998267e9ea5626e7dd8088e1133f5` | akihito | fix(bps): 在工具下发前纠正封装错误 |
| `0cae2484731dbd4ded282ec7eb7f4dbbe4d98a3b` | akihito | fix(bps): preserve raw payloads during transport correction |
| `f0061aac7799c918cfcf3fabd1e9a36d3d5fdf4c` | ranxi2001 | feat(bps): add native attachments and validated tool recovery |
| `c755a0d638b7d7e5e87148317c600fbe66cda9b2` | ranxi2001 | fix: isolate BPS rate limits and expire bounded replay state |
| `3b0c0276cfba25b0f036ff74bd15781220ed1eed` | ranxi2001 | docs(bps): distinguish unknown-target and formatting corrections |
| `33872d3f2b89c872221dbeecbec6cd1d71478643` | ranxi2001 | feat: add opt-in native BPS image attachments |
| `b021b8fb60d3182f1ab9121f99285e94deaf16c9` | loserzero-7 | feat(bps): raise default compaction threshold to 920k |
| `49ffa974bb019afab5951f82f02864d61833b482` | loserzero-7 | fix(bps): relay declared exec_command cmd as raw text |
| `afa5debb584f201667823e924dd6e602163b6025` | loserzero-7 | fix(bps): accept redundant equal cmd metadata |
| `ce2a47a60e3e87ad159dfbe4ba78ae8d41b14c24` | loserzero-7 | fix(bps): pass reconstructed cmd transport args through |
| `030b395030e9a42c77ea1d8277e902137e49f43c` | akihito | feat(bps): add automatic group action on 403 |
| `3f9b398632a7c54aa3e96e49f3ccfe22b47d0ae2` | ranxi2001 | test(bps): check command transport fixture type assertions |
| `7ac4c8c975642b79254eac92161504dae3e335df` | ranxi2001 | test(bps): supply missing repair fixture payload |
| `57125f2236f7b3af391edab4daf5cbe97d652c26` | akihito | feat: configure Excel BPS images per request |
| `fef8108236ee4f3845e2536fd150c1cfd323aab5` | akihito | test: align Excel BPS image settings coverage |
| `af390f6729091ac332e9682d28873d32464c6cfd` | ranxi2001 | fix: apply authentication policy to Excel BPS 401 responses |
| `ba3156c874d58bfacba8ebe010d5b38d88248996` | akihito | feat: align Excel BPS account edit options |
| `caefbb6c5689f0cfd1f09b10f8f92c217baea6cc` | holeen | fix(bps): preserve final upstream protocol merge resolutions |
| `b976c9f1628627b87e34775f17193ccd3bee755d` | ranxi2001 | fix(bps): retain sanitized transport and stream diagnostics |
| `3bfce058bf917d52639a67b0f02a4b5987eafc33` | ranxi2001 | fix(bps): recover OAuth reasoning and constrain encrypted agent capabilities |
| `026b62f7a7261de8f99725e185a857cc1c277d25` | akihitohyh | feat(bps): BPS 403 自动关闭协议后在账号列表标记疑似被封 |
| `494f0be52297cd5ac58c2a1c90c104132a57551e` | psyche314 | fix(bps): 支持账号在图片功能关闭时忽略图片输入 |
| `6de1ed3a5d4dac8ab6a5aa7606b598a7c49272cd` | ranxi2001 | fix(bps): preserve inline tool screenshots in native attachment mode |
| `dbf403e9b982c4730f82d5bd944e97b66fe8d03f` | psyche314 | fix(bps): 明确图片忽略开关并补充真实入口 E2E |
| `4f195e9197675c994dd740e5ae709f0f93486693` | ranxi2001 | test(bps): check native screenshot fixture type assertions |
| `4373ac322586868169d51ab48187364c631c2943` | ranxi2001 | fix(bps): retain validated operations during mixed tool batch repair |
| `a70f3edb1e28562ae935fb25fe09eff48b643801` | akihitohyh | feat(bps): BPS 403 自动调整分组后也在账号列表标记疑似被封 |
| `0923cf4bbbb064734d2850006d9ccd99a98d5599` | ranxi2001 | fix: contain scheduled BPS test panics and initialize request headers |
| `c23e18642559cac71a91068d7272070243df184c` | ranxi2001 | fix(bps): make hosted-tool fallback configurable and traceable |
| `8c3776a45277c8bcc9711f57c9eb7d06f861d84a` | 圣 | fix(bps): preserve tool image references and compatible catalogs |
| `446f1f36e319590a40e0b3d9f359b656ece46138` | root | fix(bps): force enabled accounts through basispoints |
| `4e8c894fe3003ee3b3de108e51b047436f0c6cbe` | root | fix(bps): align fallback tests with forced routing |
| `8a9c2a4e7d5172ea66397739d354f226bce21738` | Terry | fix(bps): expose encrypted history validation path |
| `207f31e4e34d7dd9ffed4e543ad75ae46a0a6160` | 圣 | fix(bps): 修复历史消息 author 字段导致的上游 400 |
| `c83e1eec7ad05734056033eb28e2fb879935d20b` | ranxi2001 | fix(ops): record BPS cancellation and error duration |
| `b31d070a66ff4fda1ba01b91c0262ebac1b8ea02` | psyche314 | fix(bps): 明确提示图片输入不可用并避免重复读图 |
| `8b339717302f47fec8de0dc748eb9dd9d74df823` | akihitohyh | fix(bps): 增加忽略历史加密消息的账号选项，避免旧多代理会话每轮报错 |
| `fdd3532b822b8b8fb0a244e89c7fb218c6e589f7` | ranxi2001 | fix(bps): satisfy history message staticcheck |
| `f593eeb76ddd741fc70e294364b99ba8856d021c` | ranxi2001 | test(bps): check compact failure payload type assertions |

## 本次完整审计与运维增量

详见 [BPS_AUDIT_AND_INCREMENT_2026_09_28.md](BPS_AUDIT_AND_INCREMENT_2026_09_28.md)：固定源、最新 tip 差异、图片策略、最小自动 BPS、优先调度、凭证运营、迁移、测试和未验证边界。仅本地提交与品牌普通合并，不推送或发版。

## 2026-09-29 增量

检测周期、默认选项、探测刷新、凭证页布局、请求上下文修复与独立成本倍率见 [EXCEL_BPS_SYNC_2026_09_29.md](EXCEL_BPS_SYNC_2026_09_29.md)。已按用户确认以独立成本倍率替换 Teams 回本策略，默认 0.1，仅影响优先调度的利润估算。

### 后续：自动 BPS 规则删除

来源审查推进至 `e0b227cc07dfb7094f1c01889c9fdb9f1d391374`。单条/批量规则删除及失败重试已适配到本地自动 BPS 页面；自动并发升档计数修复仍排除。行为、接口、兼容与验证见 [本轮记录](EXCEL_BPS_SYNC_2026_09_29_RULE_DELETE.md)。

### 后续：#204 容量上限与 OAuth 保护

来源审查推进至 `ac29d58ee7e8d49ab1c82e637f4a20086c6d0cf5`。#204 的允许上限已贯通前后端，默认值和已有配置保持不变；同时适配手动 BPS 模板、默认关闭的 WS→SSE、Free 套餐保护及本地重新登录 V2。最新一轮只适配 ranxi2001 的原生图片优先、未设置时默认启用图片和账号列表 BPS 徽章；保留本地账号级图片忽略兼容开关，不恢复源项目已退役的自动质量、优先调度、凭证运营和重新登录 worker。全部来源、排除项、入口、参数上限与验证边界见 [本轮容量与 OAuth 同步记录](EXCEL_BPS_SYNC_2026_09_29_CAPACITY.md)。

### 后续：图片端点与成本跟随

图片端点、安全重选和工具流修复已在 `3e157e83e` 适配，审查至 `30a06848bc5365ce414825bcc2c9464aaaef5d95`，见 [图片增量记录](EXCEL_BPS_SYNC_2026_09_29_IMAGES.md)。

本轮继续审查至 `faf58e440b1bddb07429f74ed63b570c11d1c0f8`，适配会影响 BPS/原生混合池优先调度的成本持久化与手动覆盖保护。按用户选择，“跟随上游”默认关闭，已有成本和扣费保持原样。来源、调用链、配置与验证见 [成本同步记录](EXCEL_BPS_SYNC_2026_09_29_COST_SYNC.md)。

### 2026-09-29：余额来源诊断与 OAuth 初始模型映射

本轮同步了 ranxi2001 的余额错误来源诊断提交 `c92647ed6889a04048f611299052aba260ddb5fa`，保留原作者并用 `cherry-pick -x` 记录来源。后端统一用户余额错误文案，运维错误分类与前端明细页依据明确的 `error_owner`、`error_source`、`upstream_status_code` 和账号标识区分“用户余额不足”“上游余额/响应”及未知来源；未知情况保留原始诊断，不通过用户 ID 或单独 HTTP 状态码猜测归属。

同时按 ranxi2001 PR #219（合并提交 `b35f3d15e31ab82021279370002ff5346ca7a4dc`，变更头 `8ea71d7c7f1d0f6e54aace2f7d1cbda70a7e7c48`）适配 OAuth 初始模型映射。源 PR 依赖本项目已退役的整套通用自动账号配置，因此本地只引入其模型规则、校验、显式规则优先和凭据隔离语义，没有恢复自动并发、质量规则、分组调度或账号运维子系统。

管理员入口位于“系统设置 → Excel / BPS”的“新建 OpenAI OAuth 模型映射”卡片，对应 `GET/PUT /api/v1/admin/settings/oauth-initial-model-mappings`，设置键为 `oauth_initial_model_mappings`。开关默认关闭；打开后只作用于新建 OpenAI OAuth 账号，API Key、重新认证和既有账号不受影响。已有自定义 `model_mapping` 项优先；同名直通项（例如 `gpt-5.4 → gpt-5.4`）会被模板替换，已与模板相同的项保持不变。规则上限 100 条，源模型最多允许末尾通配符。删除全部规则表示不自动添加映射。当前未把本地 CRS 同步改造成源 PR 的自动配置入口，避免恢复已退役的通用自动配置依赖。

### 2026-09-29：#220–#222 BPS 链路增量

继续同步 ranxi2001 的三个 BPS 修复：

- PR #220，合并提交 `31d4cabc1fab5cf4cbe8eec05e1b5663d7ca049f`：在输入校验和历史翻译完成后，用户消息、历史附件及从工具输出移入的 `file_id` 图片只向 BPS 发送 `type` 与 `file_id`；HTTPS 图片和工具截图仍保留原有字段。不会在校验前清理字段，也不会修改调用方持有的历史对象。
- PR #221，合并提交 `f364254c35a3c1b2a3001644a8bac3d2781cbaa9`：新增 BPS 流内失败分类，按鉴权、权限、限流、请求参数、上游服务和取消状态生成安全错误码；显式上游状态优先于错误码推断，运维记录保留真实 HTTP 状态，已接受的生成不会因分类而重放。
- PR #222，合并提交 `8d19508a6deff99f2ef8c58c7c6393d318c5a5e3`：保留本地代理池错误的 typed sentinel 和调度健康隔离，使未来的托管代理获取失败不处罚账号。源 PR 中依赖 Mihomo 池的获取、切换和重试路径继续排除，符合本项目“不引入 Mihomo”的既定范围；当前普通账号代理路径不会伪造该错误。

上述适配均保留源作者和 `cherry-pick -x` 记录；图片路径、流内分类和调度错误隔离分别覆盖 `backend/internal/service/basispoints`、`openai_excel_bps.go` 与 `openai_account_scheduler.go`，未恢复源项目的 Mihomo 管理页面或代理子系统。

### 2026-09-29：运维入口退役

自动 BPS `/admin/account-quality`、独立优先调度 `/admin/priority-scheduling` 和凭证运营 `/admin/token-guard-v2` 已整体移除，包括对应页面、API、后台任务、质量计划调度和自动重新登录 worker。手动 BPS 默认参数与 OAuth 初始模型映射保留，统一从“系统设置 → Excel / BPS”进入。历史迁移、旧字段和数据保留；详细范围见 [运维入口退役说明](OPS_FEATURE_RETIREMENT_2026_09_29.md)。

### 2026-09-30：ranxi2001 最新提交审查

本轮固定核对 `ranxi2001/production` `690758d22be0ed65b38b4d5261981d117d0d570f`。官方 `upstream/main` 在本轮无新提交。与当前 Excel/BPS 边界有关且本地尚未等价实现的 `a3c007b9fb2f228e7f293d6a521023ef364a8a5` 已适配：OAuth 初始模型模板现在会替换同源同目标的身份直通映射，同时保留已有自定义目标、已匹配模板的映射和非字符串异常值，并继续克隆凭据避免修改调用方对象。该修复保留在本地独立的 `oauth_initial_model_mappings` 设置中，没有恢复源项目已退役的通用自动配置、重新登录 worker、优先调度或质量运营入口。

此前已适配的 #220（原生附件字段清理）、#221（流内错误分类）和 #222（BPS 托管代理获取错误与账号健康隔离）在最新源提交中没有新的等价差异，本轮不重复导入。源分支中的 reauth worker、账号分类优先级、自动计费映射、发布准备和其他运维增量均按既定范围排除。本轮无新增迁移、版本号或发布动作。

随后源分支推进到 `8736dac80ccd5e02f147ee711fc98af6040f67fe`。本轮继续选择性适配以下 BPS 增量：

- `58c5e2401eebd669c2a544890beaa39ce7a904c`：根据实际可调用工具目录生成 `run_officejs` 协议示例，避免提示词固定教导未声明的工具；保留本地已有的目录说明示例并叠加新校验过的原生参数示例。
- `bf71b049fcbdea81c38733cf5534109c0391e8c4`：只移除兼容层产生的 `item_` 加 24 位十六进制消息 ID，保留原生、未知及工具调用身份字段，并验证输入不变和处理幂等。
- `8cb9b31dcdcc21915187f3b2f0556a05f4bd7086`：确认工具示例构造器的 `strings.Builder` 写入错误为不可发生错误，显式处理返回值以通过静态检查。
- `3526888148ff6261643e01370b4c729fa2f46753`：图片数量超限提示改为明确指出是本地网关配置，同时保留历史与工具输出计数、容量上限和管理员调高配置的指引。

`8736dac80` 中的 Kubernetes 多副本运行时、Pod 排空、并发缓存和部署清单不属于 Excel/BPS 协议范围，未合并；本轮无新增迁移、版本号或发布动作。

### 2026-09-30：ranxi2001 `7124114c2` 增量

源分支随后推进到 `7124114c22c7cb786a62d5e3ee64713ca87ebdfc`。本轮适配三项与 OpenAI OAuth/BPS 直接相关的修复：

- `3ef21baad59fd276a42f0b95d0d4ff3b68a3400c`：对明确的 OpenAI “Your IP is not authorized to make this request.” 401 响应按账号建立进程内五秒窗口，要求连续两次才进入 OAuth 临时停调；重复 request ID 不重复计数，正常响应和成功调度会清除 streak。不会把通用 401、token 撤销或缺少 refresh token 改成宽松处理。
- `d0fdc590e9c7d2ba861ba7f06729a999bde08672`：在 OpenAI 网关上下文窗口、容量削峰等早退响应前先清除 IP 401 streak，避免一次正常错误响应被遗漏而延长去抖窗口。
- `bbee31f6e3f8d11c1edc20c510a1be6c7d391c81`：附件上传失败按准备、传输、HTTP、响应解析和容量阶段记录脱敏诊断，保留真实上游状态码、失败原因和“generation 未开始”标记；客户端错误和已接受生成的重放策略保持不变。源补丁中的 Mihomo 代理池获取、固定出口租约和相关测试未导入，附件继续使用本项目已有账号代理路径。

初始 BPS/OAuth 适配范围排除 `50b488de7` 的 Pelican 测试计费及迁移、`a1e688f84`/`97946b5ef` 的 Claude/通用网关取消链路，以及合并包装提交 `550a1d93f`、`769080ebc`、`a6121b970`；Claude/通用网关链路随后按用户确认单独合入，见下节。无新增数据库迁移、版本号或发布动作；当前 BPS 配置入口、附件容量设置和 OAuth 初始模型映射入口不变。

### 2026-09-30：Claude/通用网关取消链路确认合入

按用户确认，补充合入 ranxi2001 的 `a1e688f84acd51a36817e37dbea3f710bb8bdbec` 和后续 `97946b5ef81df104ee59eba226a96ebdfdd08f3c`。客户端流断开、写入或刷新失败时立即取消上游读取与重试，关闭响应体，保留断开前已经观察到的用量；已收到终态时正常结束，未收到终态时返回不完整错误，不再为计费继续读取静默上游。`FlushGatewayResponse` 通过响应 writer 的受保护委托链报告刷新错误，避免绕过生命周期租约；`message_start` 的输出 token 与后续累计 `message_delta` 合并时不重复计数。覆盖 Claude 流、通用 gateway、handler writer 和资源清理测试，不改变 BPS 的“不因客户端断开重放已接受生成”规则。

### 2026-09-30：官方兼容入口与 BPS 分类保护衔接

官方上游固定到 `a0f41f95a07ee6ca0b1300d3c96ce4b62e24724b`，普通 merge 保留 `b31b435091709920c87c2b5d4d66bf9e8d25134b`：启用“仅 Claude Code”且配置降级分组时，`/v1/chat/completions` 和 `/v1/responses` 继续使用现有降级选号逻辑；未配置降级分组仍返回 403。既有配置入口为管理员 → 分组管理 → 编辑分组，对应字段 `claude_code_only`、`fallback_group_id`；无需新增配置，部署后现有配置自动生效。

全量验证同时修复此前 #221 分类与本地 TPM 保护的衔接：只有明确的 RPM/TPM 错误且尚无输出或输出计量时，才在同一账号上做原有的有界启动重试；已接受的生成不重放，流内常规限流不误冷却单个账号。缺少错误码的明确 TPM 错误仍生成标准限流码；错误参数/显式状态优先。脱敏后的错误只保留数值化、至多两小时的重试提示；兼容 compact 已提交的 SSE 响应，并分别记录真实上游 HTTP 状态与语义错误状态。目录回归测试按已声明元数据验证，保留厂商未公开创建时间的零值，不伪造时间戳。

上述兼容修复无需配置，部署后自动生效；无新增迁移、环境变量或依赖。

### 2026-09-30：ranxi2001 `8faa54e0b` 别名范围与错误率衰减

本轮固定审查 `7124114c22c7cb786a62d5e3ee64713ca87ebdfc` → `8faa54e0b024e80ceb28872e9ff56c334f53fe08`（#230）。源码主要属于此前已退役的优先调度，因此按实际调用链拆出以下两项共享 OpenAI OAuth/BPS 改动，不整合完整分支：

- 来源 `6bc3e3954d03bfed11d326c9610f5826dcd28915`：新增明确的 `credentials.model_mapping_mode`，值为 `aliases` 或 `whitelist`；未填写、空值和 `whitelist` 都保留旧的白名单语义。新建 OpenAI OAuth 账号若启用既有初始映射模板、且原本没有任何映射，则新增规则作为别名，保留其他原生模型；已有非空映射和明确导入范围不会自动扩大。映射改写、模型目录、调度候选缓存、单号与批量保存均按同一规则处理，保留外部厂商模型准入、分组白名单、影子账号和逐次主库发送保护。
- 来源 `826b097367d816a18cf82a1d320163e886f53478`：共享调度统计的错误率按两分钟半衰期随时间降低；新的失败仍按原有 EWMA 权重计入，迟到报告不会倒退观察时间。TTFT 不因空闲而虚构改善。保留现有普通/高级调度顺序与粘性策略，不导入容量优先排序、组争用权重、利润排序或强制粘性重分配。

**配置入口与兼容：**

- 管理员 → 账号管理（`/admin/accounts`）→ 编辑普通 OpenAI OAuth 账号 → “模型映射仅作别名，允许其它原生模型”；对应 `PUT /api/v1/admin/accounts/:id` 的 `credentials.model_mapping_mode`。默认不勾选旧账号；单纯保存旧默认范围不会添加额外策略键或打断未改变的 WebSocket 绑定。真正切换范围仍改变准入指纹，已有绑定可能需要完整上下文重连。
- 管理员 → 账号管理 → 批量编辑 → “修改模型映射范围”；对应 `POST /api/v1/admin/accounts/bulk-update`。未勾选修改开关时不改范围；只修改范围时保留已有映射和 BPS 选项。旧客户端批量改写映射但未提供范围时，仍按明确白名单编辑处理。
- 系统设置 → Excel / BPS（`/admin/settings`）→ 新建 OpenAI OAuth 模型映射，继续使用 `GET/PUT /api/v1/admin/settings/oauth-initial-model-mappings` 与 `oauth_initial_model_mappings` 设置键，开关仍默认关闭；该既有设置的行为修复只作用于后续新账号。
- 错误率衰减无需配置，部署后现有 OpenAI 调度统计自动生效；常量位于 `openai_account_scheduler.go`，没有新调度页面或配置开关。

没有恢复 `priority_scheduling*` 服务/API/UI、自动账号配置、账户质量规则、凭证运营、Mihomo 或 sub4api 独立 BPS。来源中的 `b5ede92391a19fd7fe64535956823903e124bd93` 运维页面说明，以及合并包装 `002633d26`、`4ec93aa0a`、`74b2d9386`、`8faa54e0b` 不重复导入。迁移、Ent/Wire 输入、依赖和版本号均未改变；真实上游账号未实测，验证记录以本轮测试日志与最终交付报告为准。

本轮 main 验证：全量 `go test -tags unit -timeout 10m ./...`、`go vet -tags unit ./...` 通过；别名与衰减的 service/repository 定向 race 通过。前端类型检查、371 文件/3027 用例、生产构建与繁体生成检查通过。另补齐两个旧页面测试对直接 store 导入的模拟路径，保留原有断言。未执行真实上游账号或生产数据库测试。

推送前审查继续推进到 `fdd376481856182ee3142fd013c4593ea85640d7`；`8faa54e0b` 之后只有 `fdd376481` 将源 fork 的 VERSION 改为 `2.9.5`，未包含新的功能代码。本轮不导入 fork 版本号、不修改本项目发布版本；功能适配范围仍以 #230 的上述两项为准。

### 2026-09-30：官方 96f4c115 与 fork 范围复核

官方上游从 a0f41f95a07ee6ca0b1300d3c96ce4b62e24724b 普通合并至 96f4c115c9749078f90cbf210a01d39baf3f53b6。按用户确认，将余额在途预留、Claude 手动兑换重置、API Key 创建限制及模型目录更新与已有安全修复组合，不重写分支历史。

- API Keys → 使用 → Codex（/keys）：目录默认仍为本地文件、立即生成配置、手动获取；新增可选远程目录，仅作用于现有 OpenAI/Composite 范围。远程配置使用鉴权的 /v1/models；手动下载仍使用 /backend-api/codex/models。模式切换不自动请求，API Key/分组变化会取消旧请求并恢复本地模式；超出远程大小上限时回退本地。保留品牌、Windows 路径和精简配置。
- 新版 Sol 6.1 使用独立 Codex 元数据和指令。Codex 目录的默认/最大上下文为上游描述中的 272000/872000，与 API/OpenCode 原有上下文及计费口径分别保留；不把客户端默认窗口当作价格阈值。真实账号元数据仍优先；BPS 不宣称原生 Responses Lite 或加密 multi-agent 能力。Astra Ultrafast 遵循账号能力，而非仅凭订阅类型放行。
- 账号管理 → Anthropic 账号 → 重置次数旁的重置按钮（/admin/accounts）新增二次确认。POST /api/v1/admin/accounts/:id/claude/reset-credits/redeem 由服务端选取 grant，使用幂等和租约；结果不确定时阻止重复兑换，不自动消耗次数。
- 余额预留仅新增后端 billing.inflight_reservation.* 配置，默认启用；默认 Redis 故障或无法估价时沿用旧准入，非绝对透支保证。保留语音归属隔离、模型白名单、异步媒体持久计费与安全校验，预留不替代真实扣费。
- API Key 数量/创建频次限制采用后端 api_key_create.max_active_per_user=200、max_per_user_per_hour=60；0 可关闭对应限制，删除 Key 不返还创建次数。配置示例位于 deploy/config.example.yaml，无新增前端管理开关。

ranxi2001/production 审计推进到 a53a7ff163d9337a094e7537df3aac2b31f308b7。相对 fdd376481856182ee3142fd013c4593ea85640d7，唯一非 merge 提交 c28a9f3c6bff6f25f31f86cee64c4799c7aaf6e3 新增已退役的重新登录引擎、TokenGuardV2 设置与并行 worker，并非当前 BPS 请求的必要依赖，因此整项排除，包括其迁移。未恢复 Mihomo、凭证运营或 sub4api。

官方增量无新迁移、依赖及发版 Workflow 变更；本地此前未推送的安全修复包含 258/259/260 三个迁移，本轮保留但不在生产执行。实际验证和逐远端 SHA 见 .release/upstream-sync/20260930-isolated-96f4c115/；本次仅提交/推送，不发版、不部署。

### 2026-10-01：ranxi2001 `2d55b424e` 最新提交复核

本轮从 `ac29d58ee7e8d49ab1c82e637f4a20086c6d0cf5` 审查至 `2d55b424e`。官方 `upstream/main` 本轮无新增提交。

- `978c8a91b` / PR #249 新增 BPS 默认模板的 WS→SSE 加速和“降级后自动开启 BPS”两个勾选字段。前者在源提交中只加入表单和 JSON 字段，没有把默认值接入账号创建/编辑保存链路；后者依赖已退役的自动 BPS 质量规则与 `/admin/account-quality`，违反本项目已确认的运维范围。两项均不导入，避免保存无效设置或恢复已删除的自动运营依赖。
- `4806330e0` / PR #248 是凭证运营和重新登录运行时控制，继续排除；`245bc672a`、`e51159cf2` 是凭证运行时/部署升级，排除。
- `2ecd5b7d5`、`04e76d18d`、`aa95c14f9`、`2d55b424e` 是后台功能搜索和合并包装，不属于 Excel/BPS 协议；`e52bf01b1` 仅绕过 SPA 的裸 API 别名，也不属于本次范围。

本轮无可安全落地的 BPS 代码增量、迁移、版本号或发布动作；保留上一轮已合入的原生图片默认、图片容量、附件字段清理、流内错误分类、BPS 代理错误隔离和账号 BPS 徽章。该审查记录随本次提交推送，不恢复自动 BPS、凭证运营、重新登录 worker、Mihomo 或独立 sub4api BPS。


### 2026-10-01：ranxi2001 `c2e3e098f` Serverless 增量复核

固定来源：`ranxi2001/production@c2e3e098f7a8cb6a88666574ed050d58be922aa3`；定向 fetch 与 `git ls-remote` 核对一致。审查区间为此前已记录的 `2d55b424e` → 此 SHA，包含五个非合并提交与 PR #251 包装提交。本轮仅同步 fork 审查进度，不进行官方 upstream 整合。

| 来源提交 | 实际行为和本轮处理 |
| --- | --- |
| `520d74e10eb93eb9e4c5a8542cff1a97a0015f19` | 新增 Serverless 地区到 gateway Pod 路由、注册/心跳、API Key 实例绑定、签名转发和图片归属下载。属于新分布式运行架构，排除。 |
| `b86adf3e18cb516502ee97e5a4fe79dec7cdb543` | 说明上述 Pod 入口、地区查询、会话排空和运维边界；未引入该系统，不复制其运行说明。 |
| `f56180dd6591f424a12fdf808e06dd78b906000a` | 修正上述转发的可信 IP、会话绑定和路由前模型准入；只服务新 Serverless 链，排除。 |
| `f17a8d5c43aa62f91dfe786ae4653eb6af78ff28` | Serverless 设置页面截图及清单，排除。 |
| `4f651425a9f530290f662d5ab77e8ca6e10bf28e` | 通用前端 Axios 依赖由 1.18.1 更新到 1.20.0；未更改 BPS 后端传输或专属管理协议，也不是此次必需依赖，留待独立依赖维护审核，不导入。提交标题中的安全声明不作为本项目已确认的漏洞结论。 |
| `c2e3e098f7a8cb6a88666574ed050d58be922aa3` | PR #251 合并包装；remerge-diff 无额外内容，不整体合入 fork。 |

**BPS 调用链复核：** `basispoints/image_relay.go` 的 `SetURLDecorator` 与 `openai_excel_bps.go` 的装饰器接入，仅调用 `Serverless.ImageOwnerURL`，为图片能力 URL 添加 `sl_node` / `sl_boot` / `sl_proof`；下载由新 `ImageRoute` 检查签名、实例与注册信息后转发原 Pod。这不是独立的图片容量、上传协议、RPM、重试或调度修复；脱离该系统导入会形成没有接收端的设置/链接，因此两处 BPS 文件的变化亦排除。

**交付与配置：** 本轮只追加本文审查记录，无新增或修改的功能、配置入口、环境变量、迁移、依赖或运行行为；现有设置和默认值保持。没有部署后新增生效事项，无需管理员操作。保留原生图片默认、图片容量、逐次 RPM/资格保护、附件清理、流内错误分类和 BPS 代理错误隔离；不恢复已退役运维系统、Mihomo 或独立 sub4api BPS。

按共享审查文档落地 `main`，普通 merge 到 TapModels，保持来源 SHA 和品牌文件；推送目标为 `origin/main`、`origin/TapModels`、`erwinlin/main`。检查文档差异、固定来源范围、两品牌运行代码树未变化及手动发版 Workflow 回归；文档改动不重复全量业务测试。仅提交/推送，不发版、不部署，不混入当前模型目录任务的未提交代码。

实际验证：`git diff --check` 与仅文档范围检查通过；逐文件确认两品牌发版 Workflow 保持仅 `workflow_dispatch`。`test_auto_release_workflow.py` 因现有 Python 环境缺少 PyYAML 未能启动，未安装依赖；未重复业务测试，也未使用真实 BPS 账号、Redis 或生产数据库。

## 2026-10-01：无 Key 的 WS 测试与 API Key 并发/排队（#259、#263）

审查范围从 `c2e3e098f7a8cb6a88666574ed050d58be922aa3` 到固定来源 `ranxi2001/production@a7263faa247b74edd9e2bc8671a17ad96b3a1237`，交付前再次查询来源远端仍为此 SHA。普通 ranxi 同步继续只覆盖 OAuth Excel/BPS 与必要依赖；本轮用户另行明确批准扩大到全协议 API Key 并发与排队。没有整体合入 fork，Merge 包装不重复导入。

### 新增功能

- API Key 的 `concurrency_limit`（默认 `0` 无额外限制）及 Redis 原子准入、续租、失租终止、取消清理；队列覆盖 HTTP/SSE、Responses WebSocket 每轮和 Live 等入口。默认每个受限 Key 额外等待容量 `5`、单次 `30` 秒，不保证 FIFO。用户/账号等原有额度仍生效，Key 排队不占用户或账号槽。
- 用户「API 密钥」(`/keys`) → 创建/编辑、批量编辑 →「并发上限」；页面展示活跃与等待统计。只读接口 `GET /api/v1/keys/concurrency?ids=...` 先核对 Key 所有权，统计不可用时显示未知/过期，不显示虚假的零。繁体由生成器输出，日语补齐。
- 全局队列仅有进程启动配置：`gateway.api_key_queue.max_waiting` / `GATEWAY_API_KEY_QUEUE_MAX_WAITING` 默认 `5`（`0` 关闭排队但仍限并发）；`gateway.api_key_queue.timeout_seconds` / `GATEWAY_API_KEY_QUEUE_TIMEOUT_SECONDS` 默认 `30`、必须为正整数。配置与升级说明见 [API Key 并发限制与等待队列](API_KEY_CONCURRENCY.md)。

### 优化改进

- 排队/WS 轮次复核现有 Key、用户、IP、模型和当前请求所用能力；换组/平台/计费模式时返回 `503 / API_KEY_GROUP_CHANGED`，不混用新权限与旧路由。队列满 `429 / api_key_queue_full`，超时 `429 / api_key_queue_timeout`；WS 权限失败 `1008`，容量/临时错误 `1013`。Key 级本地容量错误仍有诊断，不当作上游账号故障。
- WebSocket 继续只用一个 reader，在准入等待时处理断连/待发送取消/重叠请求。槽位随实际请求工作结束释放；失租取消上游，先等待工作停止再释放，Live 转移准确的 Key/用户/账号成员避免重复占用。
- 统计查询改用 Redis Pipeline；鉴权快照版本由本地 `27` 提升到 `28`，包含并发字段，使旧缓存回源重建。未恢复已退役字段。

### Bug 修复

- #259：没有 API Key 的内部账号测试不再被 WS 回调按分组 `0` 误拒绝；账号存在/启用/平台、主库资格与 RPM 等保护仍在。**无需配置，部署后自动生效**。账号测试入口为管理后台 → 账号管理 (`/admin/accounts`) → 测试。
- 本地组合适配保留实际选号分组的粘连和准入命名空间、当前轮次模型、逐次 RPM、已有容量重试预算及 BPS 保护。WS 后续轮次按 Key → 有界等待绑定账号 → 用户准入，账号等待不占用户槽；准入后冻结利润计价时刻。模型目录路由/价格仍保留同轮映射快照。已发送/输出的请求不因新容量错误重放。
- 保留 Grok 自定义语音归属和双向退出后统计用量；新 Key 限额补到本地新增的两个 Grok 上游入口。修正源测试构造器与本地没有 `proxyRepo` 的差异。为执行集成验证，修复两处既有测试编译问题：退役投影测试残留 import、模型目录测试重名变量；没有删除测试用例。

### 来源与本地提交

| 源提交 | 行为 | 本地提交 |
| --- | --- | --- |
| `f98a370825602a4512a7c201da925b325b7d0bdb` | #259 无 Key WS 测试 | `e862b6a22` |
| `cbb8ef3ed36dc4d7b4b5e8355e59d2de666660c8` | #263 API Key 并发/排队、前后端和迁移，经本地组合适配 | `9ac3bf7c5` |
| `467f5497354bafbcbd1b328052fa22d70171de81` | 批量统计与旧版本升级验证 | `bcfd5ba5c` |
| `95fcb3995a31a45f4d53f6a889e516ebc804327d` | Redis 断言与 benchmark 清理错误处理 | `32a0daec6` |
| `75481943141ffdb342fdef619b1f7f60ff669341` | 鉴权缓存测试 stub 接口 | `8f1a706d3` |
| `15bf3cd838bbad243e548212c4dee44d04b54ae4` | 轮次准入测试夹具 | `f799d8b83` |
| 上述 #263 本地兼容修正 | 构造器、Grok 入口、绑定账号等待回归及集成夹具修复 | `d52c9b9cd` |

六份源提交保留原作者和 `cherry-pick -x` 来源：akihitohyh、InCerry。冲突按用户确认方案逐块组合：没有套用整文件 ours/theirs，没有导入 fork README 中品牌/采集/运维方向。

排除：`67266c9cbade43a4f73534cc6fdabab7190badae` 的 Grok CLI `1.0.44`（本地官方同步已有 `1.0.46`，不降级）；`c885caa4f77e650f88b0efa8c9a647f6376ba7e8` 鹈鹕 HTTP 预览与 BPS 无关。Prism 新平台/浏览器桥、票据和文档均排除，涉及 `cd1aa7db8`、`4482c64f8`、`6709d529a`、`27b20d536`、`bfb2cc57b`、`8ff9b9a6f`、`8d053f79e`、`648ac02af`、`47dcb323b`、`01ccb05d9`；其所谓 OAuth 生命周期/账号设置修改实际依赖 Prism，不抽取缺少消费者的代码。共享冲突夹带的 Cyber 新身份口径、请求采集和用户禁用模型未恢复；Mihomo、独立 sub4api BPS、自动 BPS/优先调度/凭证运营仍退役。

### 迁移、兼容与验证

新增 `237_add_api_key_concurrency_limit.sql`：`api_keys.concurrency_limit BIGINT NOT NULL DEFAULT 0` 及非负约束，保留源 SQL；按完整文件名记录，与 `237_add_minimax_platform.sql` 并存，不修改历史迁移。已有 Key **无需配置，部署后保留原并发限制**；启用 Key 级上限需主动设正数。设置修改使鉴权缓存失效。需完成全实例升级后启用，统一 Redis 和队列配置；扩大等待时间需核对客户端/代理首字节超时。二进制回退不自动删除新列。本次未执行生产迁移。

未修改依赖版本、Go 模块/前端锁文件或发版 Workflow。用户确认后按 pnpm 9.15.9 与既有锁文件准备前端依赖；Ent、Wire 重新生成没有差异。仅代码推送，不发版、不构建镜像、不部署。

main 验证：后端 `go test -tags=unit ./...` 全量、`go vet -tags=unit ./...` 和 Key/轮次/租约的四包 `-race` 定向检查通过；新增绑定账号等待与断连回收回归通过。前端类型检查、377 文件/3177 用例全量、ESLint、生产构建、四语完整性与繁体生成检查通过。手动发版 Workflow 的三个回归用例通过。

真实本机 Docker PostgreSQL 18.1 与 Redis 8.4：`TestAPIKeyConcurrencyLimitMigration` 和 `TestAPIKeyAdmissionDockerCompetition` 通过，验证新列默认/约束、多客户端原子限额、Live 共享上限及失租停止上游后再释放。使用全量历史 SQL 在临时库应用迁移；不接触应用或生产数据库。另外在独立临时 PostgreSQL 上运行 `TestAPIKeyConcurrencyUpgradePaths`：首次升级旧 Key 默认为 `0`、已部署源 PR 的手填 `9` 在重复迁移后保留、文件名/校验和并存、负数拒绝和新建默认值两条路径均通过。临时容器测试后已清理。TapModels 执行同套分支检查后才推送，具体分支 SHA 与结果以交付报告为准。未使用真实 BPS、OAuth 或其他上游账号验证。


## 2026-10-02：WebSocket 客户端桥接 Excel BPS

问题：分组内全部账号对某模型开启 BPS 后，Codex 的 WebSocket 请求要么被调度排除，要么首帧命中 BPS 后被 1008 `Excel BPS models require HTTP/SSE` 关闭；客户端一直“思考中”并反复重试，直至退回 HTTP。`ranxi2001/production`（至 `0994fe0f1`）仍是同样的拒绝，本功能为本地实现，不改变 BPS 上游只用 HTTP/SSE 的约束。

- 调度：客户端 WebSocket 入口把 BPS 账号视为可用；原生上游 WebSocket 仍不可用。首帧（经渠道与账号映射后）为 BPS 模型时，整条连接走 HTTP bridge，与账号的 WS mode 设置无关。
- 每轮复用 `forwardExcelBPS`：把 `response.create` 转为 BPS HTTP 请求，SSE 逐条转回 WebSocket；图片、托管工具回退、403/429、工具修复与用量逻辑与 HTTP 入口一致。前置拒绝以带真实状态码的 `error` 事件返回，输出前 429 仍走换号。
- 续写：`previous_response_id` 在连接内重建完整历史（与通用桥共用全量输出回放和 store=false 规则）；省略的工具声明沿用本连接上一份并按本轮权限复检，`tools: []` 清除；`generate=false` 预热由通用桥本地应答、不计费。
- 保活：桥接回合 15 秒无输出即发 `{"type":"keepalive"}`，避免 BPS 迟迟不出首事件时被 Codex 的 5 分钟空闲断开。
- 模型切换：首轮走原生 WebSocket 的连接若后续切到 BPS 模型，在准入前以 1008 `model switch requires reconnect` 关闭（不计账号故障），Codex 重连后新连接直接桥接。
- 用量：失败或未完成回合中已返回的 token 用量照常入账，与 HTTP 入口一致。
- 客户端限制：账号开启「仅允许 Codex 官方客户端」（`codex_cli_only`）时，WebSocket 与 HTTP 一样按同一规则逐轮校验（握手头固定身份，请求体指纹与全局策略每轮读取）；拒绝时先发 403 `error` 事件再以 1008 关闭，不发上游请求、不计用量、不计账号故障。此前 WebSocket 入口（含原生 ctx_pool/passthrough）从未执行该限制。

实现最初由 Codex 线程完成，审查后与上述通用桥回放、预热修复合并，补充保活、模型切换与回放规则。说明见 [Excel / BPS 协议](excel-bps.md)。

## 2026-10-02：Codex 目录兼容与 WebSocket 轮次写入状态

固定审查区间：`ranxi2001/production@a7263faa247b74edd9e2bc8671a17ad96b3a1237` → `bf9405e4ab58c1be4fc8ec2101371753a016908e`。共 17 个非合并提交；5 个合并包装的 remerge-diff 无独立修改。没有整体合入 fork。本轮共享后端修复在隔离工作目录适配，先普通 merge 到 main，再普通 merge 到 TapModels；主目录其他任务未提交的 BPS/WS 桥接修改保留，不计入本轮交付。

### Bug 修复

- Codex 生成目录的 `service_tiers` 始终是数组：上游显式 `null`、混合账号声明不一致或部分账号缺少声明时输出 `[]`，避免客户端解码失败或展示只有部分账号支持的档位。已有按实际模型路由选择账号的本地目录逻辑保留。
- OpenAI API Key 账号的 GPT-6.1 Sol 及 `openai/`、`-max` 别名使用完整 Responses；即使没有上游快照或快照宣称 Lite，生成目录也固定 `use_responses_lite=false`。OAuth 原生账号继续按其原生能力声明；BPS 仍禁用 Lite 及原生加密多代理能力，不关闭普通明文工具。
- 每个 WebSocket `response.create` 拥有独立下游写入状态。上一轮 terminal 已被客户端收到、旧写回调尚未结束时，新一轮不会被旧写入标成已输出；本地下一轮输出前限流能正确走原有重连/错误路径。连接整体诊断仍保留曾输出的信息；已有输出的请求不重放，也不把后续轮次改为自动整段重试。

### 来源与本地提交

| 源提交 | 本地提交 | 处理 |
| --- | --- | --- |
| `1c018400ba6d7962d881d05dc28428e14498b82a` | `2d80836a3` | Codex 目录数组、API Key Sol 及别名的完整 Responses 修复，含 OAuth/混合账号回归 |
| `11e2c024d035deccecaf7a8aee9256a5e9336303` | `e883a709c` | 上述能力交集回落改用 switch，行为一致 |
| `1fd3c958f325cc7e41be7673501420942f820aea` | `f013b5389` | WS 各轮次写入状态隔离，含前轮写回延迟与下一轮输出前错误竞态回归 |

三份源提交保留 ranxi2001 原作者和 `cherry-pick -x` 记录，均无 Git 冲突；保留本地 BPS 保护与模型目录路由、提供商上下文配置。来源 PR #267 的标题虽为 Prism，其内 `1fd3c958f` 实际只修改现有共享 WS relay 和测试，可独立导入。

### 已有等价功能与排除范围

下列 7 个源补丁的原始官方提交已是 main 祖先，且复核现有调用点、配置和测试确认行为仍在，不重复应用：

| ranxi 源提交 | 原始官方提交 | 已有行为证据 |
| --- | --- | --- |
| `59d80e919c33ef7bae6f4c88e5485dab348a2631` | `9688571a83775b87db85917398c628b7cdfe8276` | GPT-6.1 Sol 常量、模型映射、原生与兼容协议、定价和 Codex 目录测试已存在 |
| `6d0ff27b5271db5354f8c499d4838b518b67fbc3` | `b5efbe3f4c6c026d94a1c9e2a804d23d04e91b75` | UseKeyModal 的远程/本地目录入口、catalog URL 与测试已存在，保留本地智谱强制文件目录适配 |
| `c6ee0f51c6b46caeb2594384daf969cccc9b68f6` | `c91bb6124a71920e8f9ef91103518556076c7e8b` | planType、credentialsBuilder、PlatformTypeBadge 与 PAT 测试已识别新订阅 SKU |
| `a3f89b49513606e90365362f431ab0358699b165` | `b31b435091709920c87c2b5d4d66bf9e8d25134b` | 两个 OpenAI 兼容 handler 只在 ClaudeCodeOnly 且无 FallbackGroupID 时直接拒绝 |
| `df5b9721572cbb658eb0cac45d5f4f5a03bd6bb3` | `e6d191a83f0b37df3cb5b183e9cfd42e47b47a1d` | Astra Ultrafast 账号能力与模型专属 6 倍计价、服务档位降级测试已存在 |
| `d00a5babd2d397c71de9fa152b3470bec3fee27b` | `9ecb3408223bbfc6a2fde9f60b7ddd24d622a8ab` | Key 创建总量/频率限制、独立创建计数及回归已存在 |
| `32a7b4a91e36cd201849b8a51e6bf9efd59fcd97` | `017bbcb901c6f030992a84fffad2f309d20bb7a6` | `api_key_create.max_per_user_per_hour` 默认仍为 60，活跃 Key 默认 200；本轮不改默认值 |

排除 `c36c79f6cc3ea40b0c12362c0c57695768d87da7` 源项目截图，避免复制不对应本项目品牌和 UI 的验证图。Prism 会话缓存、浏览器路由、CI 和错误样式排除：`bb3014258bf4c694c5e543fc105f138ea2b43511`、`a52305a169ec9829a98a26c2bc37716ff07e11e5`、`af96148c22c9a3d6719d6c5ec6df51b47179d306`、`849577e62d2c64d7fdc919066ea7292cf6c1b7a1`。鹈鹕结果 API/缓存修复排除：`31b9dcedea064763e84a47e3669887c6d3aef7d7`、`f87f224c3bac74d2eea4ad4a12d7658c5264fdb7`。未恢复 Prism、Mihomo、独立 sub4api BPS 或已退役运维系统。

### 配置、运维和验证

本轮无新增设置、环境变量、迁移、依赖或工作流，**无需配置，部署后自动生效**。受影响的既有入口为用户「API 密钥」(`/keys`) →「使用密钥」→ Codex 模型目录获取/下载，以及客户端的远程目录；目录接口为 `/backend-api/codex/models` 和现有 Codex 格式的 `/v1/models`。需要更新已有离线目录的客户端重新获取并保存目录文件；远程模式沿用其正常刷新行为。重启/部署仍按项目既有运维流程，本轮仅提交与推送，不发版、不部署、不执行生产迁移。

隔离 main 代码：后端 `go test -tags=unit ./...` 全量、`go vet -tags=unit ./...` 和整个 `openai_ws_v2` 包的 `-race` 检查通过，包含新目录兼容与轮次写回竞态回归。手动发版 Workflow 的三个回归通过，使用本机已有 PyYAML 导入路径，未安装依赖。无前端代码、构造器、DTO、Wire/Ent 输入变化，不重复前端测试/构建或生成；本轮未使用真实上游账号、Redis、PostgreSQL，也未执行生产迁移。TapModels 普通合并后单独验证后端和品牌差异，再推送三个远端；最终 SHA、分支检查及推送结果见交付报告。
