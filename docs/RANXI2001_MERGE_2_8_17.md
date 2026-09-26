# ranxi2001 v2.8.17 增量合并记录

日期：2026-09-26。公共功能，先进入 `holeen/main`，再普通 merge 到 KDAN、TapModels。发行版本为 **0.2.8.3**，不修改 fork 的上游版本文件，不执行生产部署。

## 来源与适配边界

- 官方 `upstream/main` 固定为 `a3eb7ef302961cba716dc78b39b93b60c467db0e`，已由 `sync-upstream` 流程确认三个分支均包含，没有新增官方提交。
- ranxi 来源为 `594cdf0d6027fe7097ef42fe029c22713b9cc989..26b324b44c80929e5f86aeb36e09996423c4a5c0`（production v2.8.17）。公共基线 `19872cc1e69f57a5111c714a2ec583bb8cffbf04`。
- 按用户明确批准，移植全部可适配增量，保留本地弹窗、品牌、繁体及现有票据能力，暂不引入 Mihomo 代理池和 ranxi 打票子系统。由于该边界无法通过整体 merge 保持，功能提交用 `cherry-pick -x` 保留原作者和来源；公共到品牌仍用普通 merge 保留公共 SHA。
- 并行上游 PR 中的工具目录、原生附件和 FUNCTION_CMD 交叠部分按固定来源的最终合并结果复核；`basispoints` 协议目录与最终来源一致。没有依据早期补丁覆盖最终协议行为。
- Mihomo 系列 `e53407656`、`00c0ecc69`、`210def996`、`3ce508c8c`、`e9ccb7ee1` 不引入；`868c55770`、`20983e357` 仅保留独立的脱敏连接/断流诊断，不引入代理池获取、重试或健康排名。打票 `b4b4be949`、`93fe2b544` 不引入。来源宣传/部署截图 `f671a8d30` 及 fork 版本提交不引入。
- 既有凭证守护仍不设第三方默认地址，自动重新登录默认关闭。长上下文徽章与 BPS 图片容量配置同时保留。

## 新增功能

| 功能与实际行为 | 配置入口与键 | 代码位置 |
| --- | --- | --- |
| BPS 原生图片附件上传：校验整批请求后，用所选账号认证和业务代理上传图片，再发送生成请求；无公网图片地址要求，不自动切换已有部署 | **系统设置 → 功能开关 → Excel / BPS 图片支持**（`/admin/settings`）：已有 `excel_bps_image_relay_enabled` 开关；新增 `excel_bps_image_mode=native`，默认 `relay` | `backend/internal/service/openai_excel_bps_attachments.go`、`basispoints/attachments.go`、`frontend/src/views/admin/SettingsView.vue` |
| 图片中转的大小、数量、缓存容量、条目数与 TTL 可配置；保存后影响新请求，原有请求容量和预算仍保留 | 同上：新增 `excel_bps_image_max_image_mib`、`excel_bps_image_max_images`、`excel_bps_image_max_total_mib`、`excel_bps_image_storage_mib`、`excel_bps_image_storage_entries`、`excel_bps_image_ttl_minutes`；已有 body/budget/max_requests 配置继续生效 | `setting_excel_bps_image.go`、`basispoints/image_relay_limits.go`；详细范围见 `docs/bps-image-relay-limits.md` |
| 通用 BPS 403 可自动移组，目标 0 表示退出全部分组；与既有“403 自动关闭 BPS”独立，可同时开启；不重放当前请求 | **账号管理 → 编辑/批量编辑 OpenAI OAuth → Excel / BPS**（`/admin/accounts`）：新增账号 `extra.openai_excel_bps_auto_move_on_403`、`extra.openai_excel_bps_403_target_group_id`，默认关闭 | `account_excel_bps_groups.go`、`repository/account_repo_excel_bps_groups.go` |
| 观察员角色：只管理授权分组关联账号，空授权不授予账号访问；服务端在路由、批量引用、查询分页/导出及分组绑定处限制范围 | **用户管理 → 新建/编辑 → 角色：观察员 → 可管理分组**（`/admin/users`）：`role=observer`、`observer_group_ids`，与 API 消费的 allowed_groups 独立 | `service/observer_scope.go`、`middleware/observer_routes.go`、`handler/admin/observer_account_access.go`、`repository/account_repo.go` |

**观察员不是只读角色**：可查看/导出范围内账号凭据，创建、编辑、删除、刷新和测试账号；共享账号只要关联任一授权分组即属于管理范围，修改凭据等账号本体会影响其全部使用者。修改分组时保留无权管理的既有绑定。全局设置、用户、代理变更及本地打票操作仍禁止。仅向可信账号管理员授予此角色。

## 优化改进

- BPS 工具目录按账号、API Key 和可信会话隔离；省略 tools 继承目录，显式空数组清空，additional_tools 校验后合并。无可信标识不跨请求缓存。目录与工具回放有条数/字节预算及 2 小时空闲淘汰；重启或跨实例后仍需完整客户端历史。**无需配置，部署后自动生效**。代码：`basispoints/catalog_cache.go`、`tools.go`。
- 工具 Schema 校验、完整调用批次原子校验、原生工具回放、明确 FUNCTION_CMD 原文传输和受限纠错；不会执行解析出的代码。首次目录外工具纠错至多一次，既有格式纠错至多两次，不能据此保证真实工具工作流全部可用。**无需配置，部署后自动生效**。代码：`basispoints/tool_schema.go`、`function_cmd_transport.go`、`tool_repair.go`、`unknown_tool_repair.go`。
- BPS 默认压缩阈值提高到 920k；账号 BPS 默认模型列表对齐来源，支持 Astra、5.6 Sol、5.6 Terra，保留“仅 Astra”快捷选择。入口仍为 `/admin/accounts` 的 Excel / BPS 模型选择，既有键 `extra.openai_excel_bps_models`。已保存的明确选择不被自动覆盖。
- 鹈鹕展示列表缩略图适应画框，支持完整作品预览；保留本地 BaseDialog 标题插槽、国际化及已有弹窗行为。**无需配置，部署后自动生效**。入口：鹈鹕展示页 `/pelican-showcase`；代码：`components/user/pelican/PelicanArtworkPreview.vue`。

## Bug 修复

- Grok 内建图片/视频模型回退同时遵循账号媒体可用性；首次 OpenAI 请求去掉不兼容 reasoning status。**无需配置，部署后自动生效**。代码：`service/account.go`、`openai_gateway_request_body.go`。
- BPS 429 与真实 Codex 冷却隔离：429 原样失败，不写 Codex 用量、全局冷却或运行时禁用；成功响应的用量更新保留。该行为取代上一批增量的 BPS 429 冷却策略。401 走既有认证错误处理，不把上游未授权当作普通工具错误。**无需新增配置，部署后自动生效**。代码：`openai_excel_bps.go`。
- 连接失败与不完整 SSE 记录可定位、已脱敏的错误分类；不回显认证头、代理密码、查询凭据和附件 ID。管理员启用既有请求采集后，可在 **请求采集**（`/admin/request-captures`）查看 HTTP 诊断。未新增采集开关或默认开启采集。代码：`requestcapture/http_diagnostics.go`、`util/transportdiag/error.go`、`openai_excel_bps_transport.go`。
- 补齐日文新增键，繁体由 `tools/zh-tw/gen-locale.mjs` 生成；设置页保留本地登录条款翻译，不引入来源内联双语文案。

## 迁移、运行影响与验证

新增 `backend/migrations/252_user_observer_groups.sql`，为 `users` 添加默认空数组的 JSONB `observer_group_ids`。应用启动沿用现有迁移机制；本次只在本机临时数据库执行，未访问生产数据库。Ent 已重新生成并与纳入的生成代码一致；Wire 输入未变。`go.sum` 仅补齐 Ent 生成器的既有依赖校验值，未升级依赖版本。

原生图片仍有固定图片安全限制、32 个并发上传槽和 60 秒上传超时；附件 ID 缓存不代表上游文件保留期限。中转模式容量调高会增加实例磁盘/内存占用；切换图片模式会使旧本地中转链接失效。详见 `docs/excel-bps.md`。真实 OAuth 图片识别、工具往返及实际吞吐没有在线账号验收。

公共集成验证：

- 后端 `go test -tags=unit ./...`、`go vet -tags=unit ./...` 通过；观察员路由改动另行复测。
- 本机 PostgreSQL 18.1 / Redis 8.4：完整迁移初始化、BPS 403 移组（包括并发与缓存）集成测试、观察员分页/导出隔离与共享绑定保护测试通过。未运行整个集成测试库。
- 前端 387 个测试文件全部通过，共 3025 个用例（386 个文件全量通过后，修正导入测试的国际化 mock，单独复测剩余文件 6 例）；类型检查、ESLint、生产构建通过。现有构建大分块提示仍存在。
- `golangci-lint run --new-from-rev=19872cc1e`：0 issues；繁体生成检查通过。
- 品牌传播后另外核对品牌专属 diff 并运行品牌回归；实际远端 SHA 和本机发行验证在最终交付中报告。
