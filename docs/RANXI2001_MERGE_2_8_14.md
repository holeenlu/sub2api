# ranxi2001 v2.8.14 增量合并记录

日期：2026-09-26。公共归属；目标为 `holeen/main`、KDAN、TapModels。本次仅本地提交与合并，不推送、不发版、不部署、不执行生产数据库迁移。

## 来源与范围

已通过 GitHub `ls-remote` 与 fetch 确认并固定 `production` 为 `594cdf0d6027fe7097ef42fe029c22713b9cc989`：v2.8.14 发布提交之后的 PR #92。来源范围是上次 `3e345632f` 至该提交，目标基线为 `dd3179f55`。

继续沿用 2.8.13 的按功能移植方案，避免整体引入 ranxi 的互斥打票体系。原始提交使用 `cherry-pick -x`，保留作者；PR #91 使用最终合并的净变化，记录完整来源 SHA 和两位作者。公共功能随后通过普通 merge 传播到品牌，保留公共 SHA。

| 来源 | 本地功能提交 | 说明 |
| --- | --- | --- |
| PR #84：`6df263ef2`、`7a97283dd` | `baa3336b9`、`b36b139c5` | BPS 403 自动关闭与并发测试清理 |
| PR #87：`5c1839b28` | `e034742e7` | 代码工具参数传输 |
| PR #89：`19becb835` | `27cd3e16b` | BPS 用量快照、429 冷却 |
| PR #85：`62e6739eb`、`ddebc3559` | `b51b97c46`、`5c210c3aa` | 长上下文徽章开关及接口契约 |
| PR #91：`9129624f7` | `59221d67d` | 图片请求容量设置、预算联动及默认值 |
| PR #92：`0687a03f0` | `4380dbdfe` | 判题只接收参考值和候选值 |

PR #86 的 `1dfbc6f4d` 工具往返探针已包含在上次合并中，探针专属实现与测试逐字节一致，未重复应用。来源 README、收款/社群图片、新人部署教程截图和 fork 版本号不属于公共功能增量，未引入。上次排除的 ranxi 打票体系和 PR #23 轮次准入继续排除；凭证守护的可信地址留空、自动重登默认关闭策略保持不变。

## 新增功能

### Excel / BPS 遇到通用 403 后自动关闭协议

- 新增可选开关，默认关闭。入口：**账号管理 → 编辑 OpenAI OAuth 账号 → Excel / BPS 协议 → 403 自动关闭**（`/admin/accounts`）。账号字段：`extra.openai_excel_bps_auto_disable_on_403`。
- 仅对映射为 `basispoints_upstream_error` 的 HTTP 403 生效，模型访问权限错误不触发；403 本身不等于封号。
- 只关闭该账号的 Excel / BPS 协议，不停用账号、不重放当前请求。后续请求按账号其余通道设置调度。更新时比较凭据及当前开关，防止旧请求覆盖管理员的新设置；事务内写入调度事件，更新调度缓存。
- 代码：`backend/internal/service/openai_excel_bps.go`、`backend/internal/repository/account_repo_excel_bps.go`、`frontend/src/components/account/EditAccountModal.vue`。

### 长上下文计费徽章显示开关

- 新增开关，默认开启。入口：**系统设置 → 网关 → 使用记录 → 展示长上下文计费标识**（`/admin/settings`），后端键：`usage_show_long_context_badge`。
- 关闭后隐藏用量记录费用旁的 x2 徽章，只改变展示，不改变计费。管理员设置、公共设置、页面注入和前端状态一并更新，旧数据缺少此键时保持显示。
- 代码：`backend/internal/service/setting_public.go`、`backend/internal/handler/admin/setting_handler_update.go`、`frontend/src/components/admin/usage/UsageTable.vue`。

## 优化改进

### 图片请求容量上限与预算联动

- **现有设置的扩展**，没有新增另一套字段。入口：**系统设置 → 功能开关 → Excel / BPS 图片中转**（`/admin/settings`）。
- 设置键：`excel_bps_image_relay_enabled`（仍默认关闭）、`excel_bps_image_base_url`、`excel_bps_image_body_limit_mib`、`excel_bps_image_budget_mib`、`excel_bps_image_max_requests`。
- 最大在途请求数范围从 1–128 扩为 1–512；默认值从 32 调为 128，共享预算默认从 512 MiB 调为 1024 MiB；默认请求体上限仍为 64 MiB。
- 有效预算为 `max(设置预算, 在途上限 × 8 MiB)`，所以 512 槽至少对应 4096 MiB 记账预算，可能高于表单内 2048 MiB 的显式预算上限。此数值不是预分配内存或进程 RSS 限额。
- 已保存的数值不被覆盖；但预算联动规则会作用于现有设置。调低上限不中断已有请求，只限制后续准入。保留原有未知长度正文渐进预留、压缩正文读取后缩减预留和超额 503 行为。
- 无需新增配置，部署后规则自动生效；管理员应按实例资源检查现有容量设置。开启图片中转时，这些限制也覆盖 OpenAI/Composite 的 Responses、Chat、Messages 纯文本 HTTP 请求。
- 代码：`backend/internal/server/middleware/excel_bps_image_admission.go`、`backend/internal/service/setting_excel_bps_image.go`。来源压测与边界说明见 [容量说明](excel-bps-capacity-validation.md)，其中的压测数字是原作者报告，不是本仓库实测。

## Bug 修复

### BPS 代码工具双重转义

带明确字符串 `code` 参数的 function 工具改用原生代码字符串传输，其余参数单独编码，服务端再序列化为客户端工具参数；保留嵌套 JSON、引号、反斜杠、Unicode 和调用历史。要求精确匹配工具目录并限制体积，不执行代码、不猜测或修复源文本。**无需配置，部署后自动生效**。代码：`backend/internal/service/basispoints/function_code_transport.go`、`catalog.go`、`tools.go`、`request.go`。

### BPS 用量快照与 429 回避

成功 HTTP 响应到达时便记录共享 Codex 配额，不再依赖流正常结束；收到 429 时同步配额并直接进入冷却，避免被 Codex 的同账号重试窗口延迟。已发出的 BPS 请求不重放，客户端断开不取消账号状态写入；当前请求仍返回原有 429 错误。**无需新增配置，部署后自动生效**。

无明确重置时间时复用已有配置：**系统设置 → 网关 → 429 默认回避**（`/admin/settings`），后端键 `rate_limit_429_cooldown_settings`，包含 `enabled`、`cooldown_seconds`。上游明确重置时间优先。代码：`backend/internal/service/openai_excel_bps.go`、`ratelimit_service.go`。

### 质量运营判题不再下发原题

判题请求仅携带 `reference_answer` 与 `candidate_answer`，避免判题模型自行解题后推翻参考答案。同步四种语言的默认判题提示词，并在服务调用测试中验证原题不发给判题模型。**无需配置，部署后自动生效**；已保存的自定义判题提示词不会被覆盖，如其中自行包含题目，应由管理员检查。

现有配置入口：**智能运营 → 质量运营 → 判题提示词**（`/admin/account-quality`），计划字段 `pelican_config.quality.judge.prompt`（参考值为 `quality.expected_answer`）。代码：`backend/internal/service/quality_judge.go`、`frontend/src/i18n/locales/*/qualityOps.ts`。

## 合并适配与升级须知

- 用户已确认设置冲突按“保留图片容量配置，同时新增徽章开关”合并。
- PR #91 的早期分支使用废弃字段 `excel_bps_image_relay_max_requests`；采用最终合并净差异，继续使用现有 `excel_bps_image_max_requests`，不引入重复设置或退回旧的正文读取逻辑。
- 补齐日文；繁体通过 `tools/zh-tw/gen-locale.mjs` 生成。新增文档加入 `.gitignore` 白名单。
- 本增量没有数据库迁移、依赖版本变化、Ent schema 或 Wire 输入变化，不需要重新生成 Ent/Wire。2.8.13 尚未部署的迁移要求仍见上次记录。
- 本次不修改发版工作流。未来推送 main/KDAN 仍可能触发既有自动发版；本次没有推送。

## 验证

公共整合树验证：

- `go test -tags=unit ./...` 全量通过；新增判题断言单独复测通过。
- `go vet -tags=unit ./...` 通过；`golangci-lint run --new-from-rev=dd3179f55` 为 0 issues。
- `go test -tags=integration -exec /usr/bin/true ./internal/repository` 通过：仅编译新增 PostgreSQL 集成测试，未运行测试二进制。
- 前端 `vue-tsc --noEmit`、ESLint 检查通过；Vitest 385 个文件、2972 个用例全部通过；Vite 生产构建通过（现有大分块体积提示仍存在）。
- `gen-locale.mjs --check` 通过；zh-TW UI 审计 568 个文件，0 条未翻译中文。
- 未执行真实 PostgreSQL/Redis 集成测试、真实 BPS 上游请求、512 并发压测或浏览器端人工验收；原作者的压测报告不替代本仓库验证。

品牌以各自基线普通合并公共提交；最终交付同时核对品牌专属 diff 保留情况，并在交付报告中列出实际品牌回归结果。
