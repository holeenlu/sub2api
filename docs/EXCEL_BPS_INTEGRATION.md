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

图片开关位于“系统设置 → 功能开关 → Excel / BPS 图片支持”，默认关闭。打开后可选择原生附件或 HTTPS 中转；后者需填写本站公网 HTTPS origin，并将 `/api/bps-images/` 固定到生成链接的实例。临时链接的持有者可在有效期内取图。

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

检测周期、默认选项、探测刷新、凭证页布局与请求上下文修复见 [EXCEL_BPS_SYNC_2026_09_29.md](EXCEL_BPS_SYNC_2026_09_29.md)。独立成本倍率已审查，尚待替换既有 Teams 回本策略的确认。
