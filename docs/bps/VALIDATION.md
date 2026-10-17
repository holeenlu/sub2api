# BPS 集成验收（2026-10-12）

本次是共享功能的独立源码移植，来源 `ranxi2001/sub2api@1a74ed0e3cc6cf6923ba4747e8147decf15556b0`，本地 main 基点 `128e28cd7728772bcb7a763450616ec4bdefb023`。范围和取舍见 [需求盘点](REQUIREMENTS.md)、[行为决定](upstream-changes.md)，逐文件来源/散列见 [integration-state.json](integration-state.json)。

**追加复核**：用户要求按 Responses WebSocket 的多轮审核标准再次检查。第二轮发现了入口/调度、取消身份、纠错用量、兼容协议及 compact 限流入账的遗漏，已逐项修复；最新证据与真实环境限制见 [两条 BPS 路径第二轮复核](DUAL_PATH_REVIEW.md)。下列命令、统计与前端结果保留为首轮集成记录，不能替代第二轮证据。

## HTTP/SSE 与 WS 审核结论

| 边界 | 实现与验证 |
| --- | --- |
| 路由/权限 | 全局+账号+映射后模型决定 BPS；保留客户端限制、API Key/分组/模型/并发。Excel 单授权禁止进入原生 Codex。WS 无需开启账号原生 WS 模式；原生与 BPS 模型切换要求重连 |
| 密文/完整历史 | 不再通过删除 reasoning 密文重试 400；完整消息、工具调用/输出和已确认锚点保留。HTTP JSON/SSE 与 WS 错误后下一轮均有回归 |
| 工具 | namespace/custom/function/dynamic tools、catalog 继承、显式 tools:[]、tool_choice、成批终态验证及有界修复。未支持托管工具默认明确拒绝；显式省略必须告知能力缺失 |
| WS 生命周期 | 复用原生 owner，单 reader/串行写；stream_id/event_id 关联、取消 ID 归属、错误后继续、多轮历史、预热无计费、逐事件返回及断连用量都有回归 |
| 首输出/尝试预算 | 响应头前及 created 元数据持续到来的超时；语义 delta 后允许延迟终态；工具修复和图片转原生使用同一尝试预算。keepalive 不延长期限 |
| 错误/计费 | EOF/无终态失败不重放；无真实 ID 返回请求级 error；SSE event 名与 JSON type 一致。保留真实部分用量、compact/修复用量累计及原账单去重 |
| 限流/恢复 | 429 BPS 冷却不污染原生账号配额/分数；模型权限与通用 403 分开；总开关关闭后 BPS 冷却不阻挡原生模型。403 标记、分组动作和恢复 claim 使用事务/CAS |
| 凭据 | 独立加密 grant、身份匹配、过期检查、刷新并发合并、导入胜过旧刷新、旧 401 不破坏新 grant、撤销；无配置不得拿 Codex AT 代用。前端账号切换清空秘密并忽略旧请求结果 |
| 图片/附件 | 原生附件与 HTTPS relay、工具截图、校验/去重/有界容量、正文预算、自动 compact、能力链接脱敏、Images 格式拒绝的受控原生回退 |
| 原生兼容 | 官方原生网关/调度/IP/图片出口/WS 归属/Grok/Fable/品牌代码保留；全量 unit 回归覆盖现有测试。只新增 SQL 272，未改旧 SQL 265/266/268 |

## 验证命令与结果

使用 Go 1.27.2、Node 22.22.0、pnpm 10.34.6；依赖锁文件安装。以下命令分别在 backend/frontend 或仓库根目录运行，不启动应用或连接生产服务。

| 检查 | 结果 |
| --- | --- |
| `go test -tags=unit -timeout 300s ./...` | 通过；最终预算计费调整另经 unit + race 专项回归 |
| `go test -race -tags=unit -timeout 150s ./internal/service ./internal/handler -run 'BPS\|Excel'` | 通过：HTTP/WS/授权/冷却/图片等集成适配 |
| `go test -race -timeout 150s ./internal/service/basispoints ./internal/util/transportdiag` | 通过：完整协议包与传输诊断 |
| `go test -c -tags=integration -o /tmp/bps-repository-integration.test ./internal/repository` | 通过：集成测试编译，未运行数据库用例 |
| `go build -o /tmp/sub2api-bps-server ./cmd/server` | 通过 |
| Wire `check ./cmd/server` | 通过；因仓库未记录 wire CLI 的 subcommands 校验和，使用临时 modfile 安装工具依赖，未改变业务依赖 |
| BPS Vue 组件/composable Vitest | 29 项通过：403 标记、默认模板、账号切换/授权隔离 |
| `pnpm build` | 通过：多语言键完整性、`vue-tsc -b`、Vite；存在构建包体积提示 |
| `python3 scripts/bps/upstream-review.py <source> --ref production` | 通过；本地散列一致，来源新提交识别为 Channel Monitor V3/共享接线候选，未自动纳入 |
| `git diff --check`、共享归属检查、旧迁移散列 | 通过；已发布迁移未改 |
| `test_auto_release_workflow.py` | 4 项通过；保持手动发布 |

扩大竞态范围时，原有 `openai_compat_model_test.go` 的多个并行 Anthropic 用例同时调用 `gin.SetMode`，触发全局变量竞态；这不在本次改动文件内。未修改这些原生测试来掩盖结果；BPS 专项及完整 basispoints/transportdiag 竞态检查分别通过。全量 unit 不等于全仓竞态通过。

业务代码变更 **+14,280 / -93 行（149 文件）**；测试 **+16,989 / -12 行（111 文件）**。另含生成代码/繁体资源 +134/-1、迁移 +7、依赖清单 +3、只读同步脚本 +79。文档和来源清单另计；来源专属协议包与测试保持可核对散列。

## 未经真实环境验证的部分

- Docker daemon 未运行；没有执行 PostgreSQL/Redis 容器集成测试或数据库迁移。SQLMock、内存状态测试与 integration 编译不能替代实际行锁、多实例 Redis 和迁移运行验证。
- 没有使用真实付费 Excel grant 请求 BPS/OpenAI；登录回调、真实模型权限、长时间流、图像视觉内容和外部可访问 relay 仍需测试环境验收。没有把协议夹具当真实上游成功。
- 未发布、部署、运行生产迁移或自动开启 BPS。上线验收沿用 Responses 恢复文档的分阶段观察；本提交不宣称线上性能收益。

## 新接口、配置与执行路径

新增 **7 个 HTTP method/path**：

| 方法 | 路径 | 权限/用途 |
| --- | --- | --- |
| GET/POST/DELETE（3 个） | `/api/v1/admin/openai/accounts/:id/excel-authorization` | `accounts.authorize`；状态/保存/撤销独立 Excel grant；GET 不返回秘密 |
| GET/PUT（2 个） | `/api/v1/admin/settings/excel-bps-defaults` | super admin；仅表单模板，不自动改账号 |
| GET/HEAD（2 个） | `/api/bps-images/:token` | 限时不可猜 capability；仅显式 relay 配置后提供图片，日志脱敏 |

现有 OAuth 授权接口增加 `oauth_profile: "excel"`（不新增 URL），固定 Excel client/redirect 和 PKCE/state。现有账号测试、模型列表、设置和批量编辑接口承载 BPS，未新增平台或模型治理 API。

新增 **2 个 YAML 配置项**：`gateway.excel_bps_state_redis_enabled`（false）；`gateway.excel_bps_stream_data_interval_timeout`（120 秒，0 或 30–300）。首输出和尝试上限复用现有 OpenAI 配置。

新增 **17 个 settings 键**：总开关 1、图片/容量/策略 15、默认模板对象 1；模板内含 10 项表单属性。账号 Extra 白名单 **15 个键**（11 项配置、4 项服务端运行标记）。新增 **1 张表/3 列**：`openai_excel_grants(account_id, ciphertext, updated_at)`；复用现有 `TOTP_ENCRYPTION_KEY`，混合授权要求该密钥持久配置。无需新增独立密钥变量，也不要求开启 TOTP 登录。

按网关主分流统计新增 **3 条执行路径**：BPS Responses HTTP/SSE（含 compact/兼容入口）、客户端 WS 的 BPS bridge、BPS Images（生成/编辑）。支持它们的附件/relay、工具修复、模型发现和凭据刷新属于这些路径内部操作。新增 **1 个后台循环**：显式启用的 403 恢复，每分钟扫描、事务 claim，默认不发起探测。没有自动登录 Worker、Mihomo、自动降智处置或额外 Key 队列。

## 使用与回滚

先在测试环境应用迁移 272，并为普通 Codex 账号在编辑页保存匹配身份的独立 Excel grant，再选择 BPS 模型并打开全局开关；已有 Excel 主授权账号直接复用其主授权。图片支持单独开启；relay 要配置可被上游访问的 HTTPS 地址，多实例要评估粘性路由/共享状态配置。

全局关闭后，普通 Codex 账号恢复原生路由；Excel 主授权账号不能当 Codex 账号使用。新表仅存加密 grant，不复制主凭据。撤销独立授权不删除主凭据；代码回滚保留新增表和历史迁移账本，不自动执行逆向数据库操作。

## #14164 流尾修复复核（2026-10-12）

共享需求，覆盖 KDAN、TapModels、Tokensavy 的同一 BPS 实现。本轮先复现完整终态尾帧被读错误丢弃、失败补帧序号从 1 回退到 0，再修复读取顺序和封装；增加正常 EOF 缺终态与真实读错误的区分，以及 Ops 冻结前的 HTTP 身份/安全状态诊断。详见 [适配决定](upstream-changes.md#流尾与断流诊断修正14164)。

新增 48 个表格用例场景：五类终态和四种行尾、截断 JSON/[DONE]/非终态用量、纠错/compact 读取器，以及 JSON/SSE/WS 的空流、仅元数据、正文后 EOF、原始读错误、未知事件脱敏和无 ID 失败的序号。流中断不重放，不从 HTTP 200 推断生成成功，不丢弃已观察用量或伪造响应 ID。

| 本轮执行 | 结果与证据 |
| --- | --- |
| 新增边界用例（先失败后通过） | `/tmp/bps-eof-14164-parser-red.log`、`/tmp/bps-eof-14164-forward-red.log`、`/tmp/bps-eof-14164-green.log` |
| 完整 basispoints + transportdiag，`-race -tags unit -timeout 180s` | 通过；`/tmp/bps-eof-14164-protocol-race.log` |
| service + handler BPS/Excel 专项，`-race -tags unit -timeout 180s -count=1` | 通过；`/tmp/bps-eof-14164-dual-path-race.log` |
| 新增边界用例 `-race -count=20` | 通过；`/tmp/bps-eof-14164-boundary-race20.log` |
| `go build -o /tmp/sub2api-bps-eof-14164 ./cmd/server` | 通过 |
| gofmt、`git diff --check`、清单散列 | 通过；仅更新本轮修改的五个文件散列 |

本轮相对 `1dc2c39ac`：业务代码 **+80/-33（3 文件）**，测试 **+228/-0（2 文件）**；文档与清单另计。新增 HTTP 接口、配置项及主执行路径均 **0**；内部仅新增一个兼容原 EOF 判定的错误标记，复用既有诊断事件并合并重复日志辅助函数。原生 gateway 的另三个既存改动未纳入本轮统计或修改。

上述为本地夹具、竞态和构建验证；本轮未重跑整个仓库 unit，未调用真实 BPS 或部署。后续提交、推送不代表生产环境已更新。旧线上记录未保存原始末帧，不能证明 #14164 的实际原因正是已复现的解析边界；真断流仍需新增诊断及实际请求 ID 定位。


## ranxi BPS 增量同步验收（2026-10-12）

来源 production 固定 a519cfeb66c253dd39d7728d20d55b7cf9f79897，采用 c59fabb39b88c1e2e1305f9813806a74fc1b2ba3（PR #407）的图片准入修复；本地基于 main 9d3420be4083e5b8b9519ec2e6e114578d3b0ed2。按原始来源到当前来源审查专属和相邻差异，票据/糖果观测、Mihomo 静态节点、渠道监控及账号筛选不属于本轮 BPS 例外。未整分支合并 ranxi。

实际落地：删除选号前的 BPS 读体预算与动态 resize 实现，将预算统一到既有 forwardExcelBPS。普通账号和内部探针不占预算；实际进入 BPS 的 HTTP/SSE、compact、WS 每轮及 Chat/Messages 共用该预算。保留原网关读体/解压上限和 WS 消息/历史边界；该处理预算不覆盖选号前的解析、闲置 WS 历史或进程 RSS。

容量/正文拒绝均发生在上游生成之前，复用既有换号边界，不冷却或惩罚账号。耗尽时 HTTP 保留 503/413、固定代码以及容量 503 的 Retry-After；WS 依原关闭契约分别使用 1013/1008，并记录对应逻辑状态和固定原因。请求级诊断不继承早先尝试的上游状态，也不伪造上游 HTTP 响应。旧模型权限、过载、工具目录、EOF、取消、完整历史与实际用量处理仍保留。

| 本轮验证 | 结果 / 本地证据 |
| --- | --- |
| service、handler、middleware、routes 的 BPS/Excel 专项，race、unit、180s | 全部通过；/tmp/bps-ranxi-sync-20261012/bps-race.log |
| 本轮准入与 WS 取消后续轮回归，race、count=10 | 全部通过；/tmp/bps-ranxi-sync-20261012/admission-race-repeat.log |
| basispoints 完整包及 transportdiag，race | 通过（协议包命中相同源码测试缓存）；/tmp/bps-ranxi-sync-20261012/protocol-race.log |
| Anthropic/Messages handler 既有回归 | 通过；/tmp/bps-ranxi-sync-20261012/compat-handler.log |
| 后端 server 构建 | 通过；/tmp/bps-ranxi-sync-20261012/sub2api |
| 前端四语言键完整性 | 4 项通过；/tmp/bps-ranxi-sync-20261012/i18n-test.log |
| 手动发版工作流约束 | 4 项通过；/tmp/bps-ranxi-sync-20261012/release-test.log |
| gofmt、diff whitespace、改动文件来源及散列 | 通过；integration-state.json 保存逐文件来源和增量记录 |

覆盖普通账号与内部探针绕过、预算/槽位/降额、HTTP/WS 路由别名标记、满额改由原生账号处理、七种客户端格式全池满额、正文超限、keepalive 后逻辑状态，以及完成/EOF/取消/策略拒绝后的释放。WS 取消归属与下一轮测试在单槽容量下执行，避免空闲连接占用整个会话的预算。测试使用本地 HTTP/WS 与内存夹具，未调用真实付费 BPS，也不宣称线上容量或性能已验证。

相对本地固定基点，业务代码 +243/-258（11 文件，含四语言文案），测试 +777/-439（7 文件，旧中间件预算测试迁移至 service 并补本地协议边界）。文档和来源清单另计。新增 HTTP 接口、配置项、数据库迁移、主执行路径均为 0；新增一个 service 内部准入文件承接原中间件预算，因为选号前中间件无法得知最终账号/协议。未增加第二套预算或重试循环。

清单原有 3 项 drift 来自本轮基点已有的模型目录/用量及网关变更，已记录而未当作 ranxi 导入。只刷新本轮修改文件的散列；未触及的目录和用量文件保持原快照，可继续被只读审查脚本识别。

本轮交付方式：按用户要求并入原 BPS 功能提交，保留后续官方合并及 main 向两个品牌分支的普通合并拓扑。重写前备份三个旧分支和完整工作区，逐一核对最终树等于各旧分支加本轮补丁，再以三个远端分支的实际 SHA 验证推送结果。本地验证和代码推送不包含发布、生产部署、真实 BPS 调用或数据库迁移。


## #25587 / #25584 密文与文件归属错误复核（2026-10-13）

基于 main `f4dffdcc54b02f3c204b02976de955c87402a871`。#25587 保留默认密文拒绝与当前任务完整性保护，增加 WS 的旧历史省略/新任务拒绝回归；#25584 本地复现附件被拒绝后继续复用和 HTTP 5xx 返回换号信号的缺口，再实现精确分类和按原缓存 ID 失效。本地夹具不能证明线上这一次 500 的实际诱因。

| 验证 | 结果与证据 |
| --- | --- |
| 新增主路径回归在修复前运行 | 如预期失败，覆盖缓存继续复用、换号信号及缺少明确错误代码；`/tmp/bps-ownership-20261013/red.log` |
| HTTP JSON / SSE / 客户端 WS；网关上传原图、客户端 file_id、纯文本；HTTP 拒绝和流内失败 | 通过；每次显式请求仅一次生成，拒绝后下一次原图请求重新上传，随后复用新引用 |
| 缓存失效边界 | 通过；重复图片、其他凭据范围、预检占位 ID、迟到错误与不同的新 ID、客户端自带引用、空范围及 nil 计划 |
| 自动图片压缩、两类工具纠错 | 通过；包括 SSE 头部有事件类型而 data 无 type；保留失败阶段请求 ID、HTTP 状态及累计用量，失败不继续生成/发布工具 |
| 密文边界 | 既有 HTTP/compact 测试及新增 WS 测试通过；默认拒绝、明确开启旧历史省略、当前新加密任务始终拒绝；无密文外泄和预检失败后的上游调用 |
| 最终 basispoints 全包，`-race -tags unit -count=1 -timeout 180s` | 通过；`/tmp/bps-ownership-20261013/protocol-final-race.log` |
| service + handler BPS / Excel 专项，同上竞态/超时配置 | 通过；`/tmp/bps-ownership-20261013/gateway-final-race.log` |
| transportdiag 全包竞态 | 通过；`/tmp/bps-ownership-20261013/protocol-race.log` |
| `go build -o /tmp/bps-ownership-20261013/sub2api ./cmd/server` | 通过；`/tmp/bps-ownership-20261013/build.log` |
| gofmt、`git diff --check`、修改 Go 文件清单散列 | 通过；仅刷新本轮 10 个 Go 文件的散列 |

使用项目声明的 Go 1.27.2。宿主未预装 go，本轮从官方发行下载至 /tmp 并核验官方 SHA-256 后运行；未变更 go.mod/go.sum 或项目工具链版本。

业务代码 **+95/-8（4 文件）**；测试 **+335/-37（6 文件）**；文档和来源清单另计。新增 HTTP 接口、配置项、数据库迁移及主执行路径均为 **0**，新增固定错误代码 **1**，复用既有错误分类、附件缓存和共用转发流程。没有导入新的 ranxi 代码。

本节记录提交前的本地验证：未执行整个仓库 unit，未调用真实 BPS、变更线上账号开关、发版或部署；后续提交和推送不代表生产环境已更新。该修复让可恢复的网关附件缓存不再长期重复使用被拒绝引用；对外部文件引用或上游自身异常仍需原始图片数据/文件所属授权通道或上游恢复。

## #25645 / #25677 工具搜索历史复核（2026-10-13）

基于 main `ef5dc87a7c0e576c3bbb3d02c99374a762a4af39`，属于三品牌共享协议修正。官方 Tool search 文档规定客户端搜索调用/结果以同一 call_id 关联；server 搜索可使用 null call_id。本次只修复 BPS 目录和历史转换，不增加原生托管搜索能力。来源：https://developers.openai.com/api/docs/guides/tools-tool-search 。

| 验证 | 结果 |
| --- | --- |
| 修复前新增 `TestToolSearch*` 回归 | 如预期失败：配对中的输出被删除而调用残留；真正缺失/未完成结果未被拒绝；未完成发现被加入目录 |
| 完成的 client/server 搜索及普通工具历史 | 通过；同时消费搜索调用/结果，保留发现的工具声明、普通调用和结果；`tool_choice=none` 仍禁止工具 |
| 乱序客户端结果、连续 server/null ID 搜索、旧执行方式省略、独立目录快照 | 通过；缺失、身份冲突、重复调用、非法 ID、未完成结果明确拒绝或不授予目录权限 |
| HTTP JSON / SSE / WS | 通过；已发现的 `collaboration.send_message` 正常下发；未声明时复现 5 工具目录、无可见内容、有工具历史的安全拒绝，单次上游调用，保留真实 HTTP 200 与用量；待补搜索结果在上游调用前拒绝 |
| 续轮与密文安全边界 | 通过；只有搜索历史也不能未知工具再生成，尾随搜索条目不能遮住当前加密任务 |
| basispoints 全包 `go test -race -tags=unit ./internal/service/basispoints -count=1 -timeout=180s` | 通过；Go 1.27.2，最终运行 13.496s |
| service / handler BPS 专项竞态 | 通过；`-run 'Test.*(BPS\|Excel\|ToolSearch\|ToolRepair\|LoadedCatalog\|NewEncrypted\|UnknownTool)' -race -tags=unit -count=1 -timeout=180s`，服务 7.185s、handler 2.424s |
| `go build ./cmd/server` | 通过；产物写入临时验证目录 |

最终协议竞态日志及构建产物保存在 `/var/folders/mz/2z2mk7m12dd2pj_jqsz8gd0h0000gp/T/bps-toolsearch-20261013-bu1u0qcr/`；其他运行结果见本轮工具输出。业务代码 **+76/-4（4 文件）**，测试 **+212/-4（4 文件）**，文档与来源清单另计。新增接口、配置、迁移及主执行路径均为 **0**；沿用现有请求错误代码，仅在现有目录/历史转换中增加配对校验。

本节记录并入原 BPS 提交前的本地验证。没有线上原始请求，故不宣称这两个生产请求已被实际重放验证；#25677 的目录缺项来源仍不能由现有日志确定。没有调用真实 BPS、改动账号设置、发版、部署或迁移；后续提交和推送以三个目标远端的实际 SHA 为准。

## cyber 会话自动屏蔽补齐（2026-10-14）

基于 main 49b29ef158e6319cfd6b1227781307de5f8f870e，归属 KDAN / TapModels / Tokensavy 共享能力。本轮在独立工作树修复，没有改动主目录其他任务的工作内容。

根因：BPS 已将流内 cyber_policy 映射为客户端 400，但没有设置原生 Ops cyber 标记。handler 因此无法执行风控记录及 Redis 会话 TTL 写入。修复前新增回归确认：同一会话连续两次请求都到达 BPS。

- HTTP JSON / SSE / 客户端 WS，以及 Chat / Messages 兼容入口均复用原生标记及屏蔽逻辑；不新增配置、接口、迁移、缓存或主执行路径。现有开关、TTL、用户白名单继续有效，关闭上游正文日志也会触发屏蔽。
- 覆盖 HTTP 拒绝、HTTP 200 中的 error / response.failed、图片历史压缩、已知工具修复和未知工具再生成失败。保留真实上游 HTTP 身份，标记中的凭据和附件 ID 先脱敏，汇总原始生成、修复和压缩已报告的真实用量。
- 只按原生精确错误码规则识别（含大小写及首尾空格兼容），普通错误正文出现 cyber_policy 不触发屏蔽；策略拒绝即便包装为 401/403/429/503，也不应失效凭据、禁用/转组、冷却或换号重放。
- 回归确认同一 WS 连接续轮、重连及去掉会话头后的历史匹配均在上游前阻断；相同 Key 的其他会话、不同 Key 的同名会话不受影响，60 秒测试 TTL 到期后允许继续。关闭开关、用户白名单、非 cyber 错误保留原语义。
- 部分失败已有完整 result 时只走一次正常 RecordUsage；没有可用 result 才走 cyber 用量补记，避免双写/双扣并保留原有缓存、effort 等计费字段。

实际验证（Go 1.27.2，全部离线模拟，无真实 BPS 请求）：

| 验证 | 结果 |
| --- | --- |
| 修复前 TestExcelBPSCyberSessionBlockAcrossClientProtocols/error/sse | 按预期失败；重复会话上游调用数为 2，预期为 1 |
| 新增跨入口会话测试与错误分类、403 状态、工具修复、压缩回归，-race -tags=unit | 通过；最终 service 1.729s，handler 2.464s |
| service / handler / repository，-run 'ExcelBPS\|Cyber\|Basispoints' -race -tags=unit -count=1 -timeout=180s | 通过；10.643s / 5.374s / 2.906s |
| basispoints 全包 -race -count=1 -timeout=180s | 通过；19.221s |
| go build -o /tmp/sub2api-bps-cyber-session-20261014 ./cmd/server | 通过 |

扩展竞态回归首次暴露既有 content_moderation_cyber_test.go 在 worker 启动后写 runtimeCacheTTL 的测试初始化竞争；本轮改为使用默认 TTL 推导过期快照，未修改业务 TTL 行为。BPS handler 模拟服务补齐 DeferredService，覆盖新接入的 cyber 用量记录。

业务代码 +57/-31（4 文件），测试 +283/-9（6 文件），文档及来源清单另计；新增公开接口、配置项、迁移和主执行路径均为 0。逐文件散列及本地适配已写入 integration-state.json，固定 ranxi 来源 SHA 未改变。

日志：/tmp/bps-cyber-red-20261014.log、/tmp/bps-cyber-final-anchors-20261014.log、/tmp/bps-cyber-regression-final-20261014.log、/tmp/bps-cyber-protocol-20261014.log。本节记录本地修复验证；提交和推送不包含发版、部署或数据库迁移，实际远端 SHA 以本轮交付核对结果为准。


## #26470 / #26467 附件复用与工具纠错（2026-10-14）

基于 main 4cff0f483509ff9239fb16bace644fbd476c9a67，属于 KDAN / TapModels / Tokensavy 共享网关适配。修改位于独立工作树；未改主目录、账号设置或线上服务。

### 证据及取舍

- #26470 的实际诊断为 reused=6、fresh=0、unmanaged=0、invalidated=6，确认本次发送的是网关跨请求复用的上传引用。它不能证明 BPS 服务内部究竟发生过期、授权变化还是归属校验故障。上传响应只提供 ID，没有可验证的归属有效期，因此取消本地 30 分钟缓存及跨请求 singleflight；每次显式客户端请求重新上传原图，仅本轮重复图片去重，全局 32 个在途上传限制继续生效。重复历史图片增加上传流量和等待；原请求内的压缩/工具纠错继续使用该请求上传结果。客户端自带 file_id、压缩上下文或新上传也可能被上游拒绝，本轮不掩盖这些情况、不重放生成、不罚账号。
- #26467 没有原始和纠错后的工具载荷，不能断言对应哪一种操作差异。本地复现并修复：完全相同的裸 JSON 参数补外层包装误拒；原样 cmd 改成普通函数包装误拒。另发现缺工具名误入未知工具再生成，以及畸形具名包装可跳过原操作比对；一并修正。
- 保留完整参数值和大整数，仅忽略 JSON 对象键顺序/空白；原始执行文本按字节比较，明确工具目标/命名空间及可验证元数据不可改变。不移除操作保留检查，不向客户端发送部分工具。拒绝时既有 Ops 记录追加固定 reason、tool_index、correction_attempt、executed=false，不记录参数、代码、调用 ID 或目录内容；指向真实纠错请求 ID，并累计两次已报告用量。
- 未增加公开接口、配置、迁移或主执行路径。内部 AttachmentUploader 替代已移除的 AttachmentCache，只保留并发计数；内部结构化错误替代布尔校验结果，承载既有诊断机制所需的拒绝原因。原 reused_upload_references/cache_entries_invalidated 诊断字段保留为 0，兼容旧记录比较。

### 验证

- 修改前新增测试确认两个缺陷：未改参数的补包装得到 response.failed；畸形具名包装改成新操作却得到 response.completed。
- 新增六张不同图片的连续三轮测试，模拟上游拒绝前轮文件引用；JSON/SSE/WS/Chat 流式全部通过，每轮 6 次上传、1 次生成，本轮第七张重复图片不增加上传。既有文件归属失败矩阵保持 HTTP/流内拒绝分类、无换号重放、真实用量及脱敏诊断。
- Chat 流式/非流式、Responses JSON/SSE 和 WS 均验证裸 JSON 参数正确补包装、改参数拒绝、纠错请求身份及用量；覆盖大整数、命名空间 cmd、畸形/冲突具名包装、元数据更改、全批次不提前下发。
- 完整 basispoints 竞态测试：go test -race ./internal/service/basispoints -count=1 -timeout=180s。
- service/handler/repository 专项竞态：go test -race -tags=unit ./internal/service ./internal/handler ./internal/repository -run 'ExcelBPS|Cyber|Basispoints' -count=1 -timeout=180s。
- 服务构建：go build -o /tmp/sub2api-bps-attachments-20261014 ./cmd/server。最终运行结果见本节下方。

业务代码 **+154/-183（7 文件）**，测试 **+301/-157（8 文件）**，协议说明、需求、验证与来源清单另计。固定 ranxi 来源 SHA 不变，实际修改文件更新 local_sha256 并标为 local-adaptation。

所有验证使用离线模拟，未重放这两个生产请求，未调用真实 BPS；不能据此宣称线上错误已消失。本轮不包含提交、推送、发布、部署或迁移。

最终检查（Go 1.27.2）：basispoints 全包竞态通过（13.823s）；service / handler / repository 专项竞态通过（10.755s / 5.523s / 2.986s）；服务构建退出码 0，git diff --check 通过。日志为 /tmp/bps-20261014-attachments-protocol.log、/tmp/bps-20261014-attachments-regression.log；修改过的 16 个已登记来源条目逐一校验 SHA-256。主目录 main 仍为 4cff0f483509ff9239fb16bace644fbd476c9a67，工作区干净。


## #26478 具名独立外部输入（2026-10-14）

继续在同一独立工作树中修复，保留上节 #26470 / #26467 的未提交修改。本错误是本地工具历史检查返回的 400，尚未向 BPS 发起生成；不是上游模型权限或服务端 500。

### 复现及边界

- 对比原生 Responses 后确认兼容差异：原生保留带 name、无 call_id 的 function_call_output 作为 Codex 外部任务输入；BPS 却把它当成必须有前置调用的普通结果。在 input[5] 放入该形态，修改前可稳定复现用户提供的同一错误和索引。
- 用户日志未提供原始 input[5]，因此这仅证明可复现的缺陷，不能断言线上该项一定是这种形态。若原项有非空 call_id、但匹配完整调用及同作用域缓存均不可得，仍应明确拒绝，不能以工具名猜参数、扩大会话/账号缓存作用域、补造调用或丢弃结果。
- 将原生已有分类提取到 internal/pkg/openai，原生入口保留原协议项，BPS 复用该分类并在现有消息归属转换中转成 user 正文。仅说明来源和内容，不断言执行过工具，也不抬升为 system/developer。独立输入不生成回放记录，不扩大本轮目录或启用未知工具再生成。
- 文本/拒绝/HTTPS 图片内容按原顺序保留；非法文本类型明确拒绝，不能转换为空串。内联图片走既有消息附件上传与限制。独立输入为新轮次，不能被当成历史工具迭代或丢入旧历史压缩前缀。新任务密文在省略/上传/生成前拒绝；旧任务仅在既有显式省略选项开启时可用固定标记替换。
- 新增内部共用分类函数是为了避免 service 与其 basispoints 子包分别维护原生兼容规则；该依赖方向不能直接复用 service 私有函数。新增公开 HTTP 接口、配置项、迁移及主执行路径均为 0。

### 验证

- 索引 5 的修改前失败日志：/tmp/bps-26478-red.log；修复后协议定向测试 /tmp/bps-26478-core.log 通过。
- Responses JSON/SSE、客户端 WS：明文独立输入成功；图片上传后以 file_id 转发；正常生成仅一次。带 call_id 的真实孤立结果、当前任务密文均本地拒绝且上传/生成次数为 0，不泄露密文。手动 compact 入口沿用现有消息归属矩阵覆盖。
- 协议层复核：空/缺省 call_id、来源 namespace、消息权限、正文顺序、幂等转换、轮次身份、压缩边界、错误文本不含载荷，以及当前加密任务顶层/内容与尾随目录/压缩条目的边界。原生 standalone 分类的既有回归继续通过。
- 完整 basispoints 竞态、service/handler/repository 的 BPS/Cyber 及原生孤立结果专项竞态，以及服务构建均在最终状态运行；结果追加在下方。

当前工作树累计（含前两项修复）：业务代码 **+269/-210（14 文件）**，测试 **+599/-231（12 文件）**，文档及来源清单另计。本项相对上节增加的改动统计不能用累计数冒充。固定 ranxi 来源 SHA 保持不变。

全部为离线模拟验证，未重放生产用户内容、未调用真实 BPS；不能据此确认生产错误消失。本次未提交、推送、发布、部署或迁移。

最终复核（Go 1.27.2）：

- basispoints 全包竞态通过（12.751s）；service / handler / repository 的 BPS、Cyber 及原生结果清理专项竞态通过（11.072s / 5.552s / 2.985s）。公共 openai 包通过编译，该过滤条件下没有独立测试；共用分类由 service 和 basispoints 上述测试实际覆盖。
- 服务构建 /tmp/sub2api-bps-history-20261014 退出码 0，git diff --check 通过。
- 本工作树修改涉及的 27 个来源清单条目全部通过 SHA-256 复核；其中新增登记已有 native 兼容文件和新共用分类文件，均为 local-only。固定 ranxi 来源 SHA 未变。
- 主目录 main 保持 4cff0f483509ff9239fb16bace644fbd476c9a67，工作区干净；修复留在 codex/bps-attachment-tool-repair-20261014，尚未提交或推送。

最终日志：/tmp/bps-26478-protocol-race.log、/tmp/bps-26478-regression-race.log、/tmp/bps-26478-build.log。HTTP JSON/SSE、WS 以及前两项附件/工具纠错修复均包含在同一次最终状态回归中。


## 上述修复的提交交付（2026-10-14）

用户随后明确要求合入之前 BPS 提交并推送。将 #26470/#26467/#26478 合入最新 BPS 修复 4cff0f483509ff9239fb16bace644fbd476c9a67，保留该提交原有 cyber 会话屏蔽修改，不改写更早的共享或品牌定制提交。上文“尚未提交/推送”记录对应修复验证时点，不再表示本次授权后的交付目标。

归属为三品牌共享网关：main 先形成折叠后的 BPS 提交，再普通 merge 到 TapModels 与 tokensavy。仅 main 此次替换已发布 tip，使用固定旧 SHA 的 force-with-lease；品牌分支保留原历史并核对快进。推送前保存三个分支原 tip 和包含全部未提交内容的检查点，核对折叠前后文件树与品牌覆盖；仅向 holeenlu/sub2api 的同名分支推送。

业务代码与测试文件未在交付阶段另作语义修改，复用上节最终状态的竞态及构建证据；品牌合并后额外验证相关回归和差异保留。发版 workflow 仍为 workflow_dispatch，推送只运行质量检查，不发版、不部署、不执行迁移。实际远端 SHA 以交付后的 ls-remote 结果为准。

## #27195 附件 429 与绑定上下文恢复（2026-10-14）

本轮基于最新 main b1b5790094d1bcf8060bd6568246d002c1b89991，在 codex/bps-attachment-recovery-20261014 独立工作树修复。属于 KDAN / TapModels / Tokensavy 共享网关适配；保留此前 BPS 修复及后续原生上游、ranxi 保留路径同步。

### 证据与实现

- 生产日志确认失败端点是 attachments，HTTP 429、generation_started=false；后续恢复因 context_not_portable / unresolved_tool_context 停止。该阻断器可由 compaction、item_reference、previous_response_id 等不可迁移上下文触发，不能把名字解释成“工具目录缺失”。用户未提供原始请求及 Retry-After，无法判断具体是哪一项上下文、上游限流维度或持续时间。
- 使用带 opaque compaction 和原始内联图片的请求离线复现：修改前 JSON/SSE/WS 均直接在第一次附件 429 停止。现有处理将 429 交给外层账号切换，而外层正确地拒绝转移绑定上下文；缺少的是附件阶段同账号的有界恢复，不应取消归属保护。
- 在既有附件回调中保留请求级一次重试状态，仅对明确 HTTP 429 且原始上下文不可跨账号迁移时生效。固定原账号、令牌、代理和原图；最多补试一个被拒附件，本请求已成功上传的图片继续复用，后续另一图片若再被限流不获得额外机会。生成与 compaction 尚未发送，失败最终标记 RequestNotSent，但不改 account replay / uncertain-execution 判定。
- 有效 Retry-After 优先，否则使用既有 429 fallback（默认 5 秒）；至少等 1 秒，超过 5 秒不自动等候。关闭 fallback 且无有效头时不自动补试。预先确认首输出期限和尝试余量，重试占用共享预算且留一个生成位置；等待和再次上传受原取消/期限约束。重试预留后即使取消，也不退还已占预算。等待期间释放失败 HTTP 响应，保留既有上传容量槽位（最多 32，等待不超过 5 秒）；不会清理或缩短其他请求延长的 BPS 冷却。
- 可迁移请求维持既有换号路径。持续 429、过长等待、预算不足或已取消时停止；401/403/5xx、连接故障、损坏上传响应不进入该恢复分支。不会因附件限流写 Codex 配额、账号全局健康或模型权限，不重放生成，不恢复跨请求附件 ID 缓存。
- 复用现有 Ops 事件增加 retry、retry_delay_ms、retry_same_account；generation_started=false 保留，不记录图片、文件 ID、令牌或正文。恢复成功不设置最终上游错误摘要。
- 原上传函数无跨图片状态，无法单独保证“整个请求只重试一次”，因此由现有 Upload 回调携带一个布尔状态；共享预算原函数无法为后续生成预留位置，因此增加私有 reserved 参数实现，原公开调用保持 reserved=0。未增加公开接口、配置或迁移；新增内部附件恢复分支 1 条。

### 验证

- 修改前复现日志 /tmp/bps-27195-red.log；修改后 JSON/SSE/WS 均为两次附件调用（一次拒绝、一次成功）、仅一次生成，保留 compaction 和图片，重复图片仍去重。恢复与生成共同占用两次预算。
- 专项竞态覆盖：持续 429、较长 Retry-After、预算须预留生成、首输出期限、非 429 不重试；缺失/非法 Retry-After 使用默认等待，客户端取消立即停止；并发延长冷却不被清理；三张不同图片加重复图片只重试被拒部分，且全请求只获得一次机会。现有 fallback 设置、取消释放及附件诊断回归继续纳入。
- 实际 handler 入口 JSON/SSE/WS 回归：持续附件 429 在同一账号最多上传两次，第二个可用账号从不收到绑定上下文，零生成，最终仍为限流；诊断保留 context_not_portable。
- 完整 basispoints 竞态、service/handler/repository 的 BPS/Cyber/首输出/共享预算/恢复专项竞态、服务构建及 git diff --check 的最终结果追加在下方。

本轮业务代码 +81/-12（3 文件），测试 +322/-0（4 文件），文档与来源清单另计。固定 ranxi 来源及 latest_observed_commit 不变；新增改动只登记本地适配散列。

以上均为离线 mock 验证，未重放生产请求、未调用真实 BPS。补齐短暂限流的恢复不代表消除上游配额限制；长时间或持续限流仍需等待后重试。此轮尚未提交、推送、发版或部署，无数据库迁移。

最终检查（Go 1.27.2）：

- 完整协议包：go test -race ./internal/service/basispoints -count=1 -timeout=180s，通过（13.229s）。
- 最终取消检查调整后：go test -race -tags=unit ./internal/service ./internal/handler ./internal/repository -run 'ExcelBPS|Cyber|Basispoints|OpenAITurn|OpenAI.*FirstOutput|OpenAI.*Replay|OpenAI.*Failover' -count=1 -timeout=240s，通过（31.034s / 11.428s / 2.984s）。
- go build -o /tmp/sub2api-bps-attachment-recovery-20261014 ./cmd/server，退出码 0；git diff --check 通过。以上不是全仓库 unit / 前端测试，未改前端。
- 本轮 8 个已登记改动条目的 SHA-256 复核通过；固定来源及 latest_observed_commit 均未改变。主目录 main 仍为 b1b5790094d1bcf8060bd6568246d002c1b89991，工作区干净，所有修复留在上述独立工作树。

最终日志：/tmp/bps-27195-protocol-race.log、/tmp/bps-27195-regression-race.log、/tmp/bps-27195-build.log。

## #27141 工具纠错 raw_payload_changed（2026-10-14）

继续在 codex/bps-attachment-recovery-20261014 独立工作树上处理，保留上一节尚未提交的 #27195 附件恢复；同属三品牌共享网关。未改变 main、生产账号、上游配置或发布状态。

### 日志解释及复现

- 日志定位至 gateway_tool_transport：第一次纠错、工具索引 0、raw_payload_changed、executed=false。HTTP 200 表示上游 SSE 响应已建立，502 是网关工具操作一致性检查拒绝该批次后的逻辑错误，两者并不矛盾。当前证据未包含纠错前后的实际内容，无法判断生产差异是代码、换行还是包装形式，亦不能证明本地复现就是该生产请求的唯一根因。
- 本地发现两类可复现缺口：原始裸载荷在纠错使用 RAW 标记时已会绑定原字节，但换成普通工具包装就没有绑定，导致同一安全恢复策略因包装形式不同而拒绝；显式 RAW 的 JSON 代码含业务 name/arguments 字段时，绑定器误当成工具包装而跳过。新增四个用例在修改前均失败，日志 /tmp/bps-27141-red.log。

### 修改与边界

- 扩展既有 restoreToolOperations，不增设纠错入口或新的模型生成：普通包装仅对当前目录中的 CUSTOM、已支持的 string code 函数及 exec_command/string cmd 函数绑定原始字节。其他函数不猜字段。原始裸 JSON 参数继续逐值比较，含大整数；原始可验证的目标/命名空间及元数据不能改变。无论模型怎样改写代码，客户端收到的必须是原字节。
- 显式 RAW 标记优先于载荷内部的业务 name/arguments；未标记具名 JSON 包装仍按工具信封处理，不当成可替换的任意源码。复核中增加负向案例后还发现：冲突目标可在旧路径中被整体 JSON 重新解释为 CUSTOM 输入；现明确拒绝该路径及不可解析的嵌套具名包装，维持无法证明原操作就不下发的边界。
- 继续整批验证工具目标、schema、顺序/数量、调用身份，使用纠正后的调用 ID；缓存及后续工具结果回放保存最终下发的原操作，不修改模型返回的原对象。中间纠错内容不下发，计入两次真实用量，失败诊断仍脱敏。最大两次纠错、不确定传输不重试及原账号/模型/代理归属均不变。
- 复用当前转换器和既有字段资格判定；未增加公开接口、配置、迁移或独立执行路径，只扩展现有纠错器的包装兼容分支。

### 验证

- 扩展原有协议矩阵：CUSTOM / FUNCTION_CODE / FUNCTION_CMD 的普通包装恢复、显式 RAW JSON 业务字段、原字节与下一轮工具历史一致性、模型返回对象不被修改；保留裸 JSON 参数、大整数、目标/元数据修改拒绝、过长载荷、批次数量/顺序和取消的既有用例。
- 原 unmarked_raw_code 拒绝案例已由普通 CUSTOM 包装的原字节恢复正向矩阵承接；负向矩阵改为“未声明 raw 字段的普通函数”，继续证明不能为不相关函数推断代码字段。冲突具名包装同时验证普通函数及 CUSTOM 纠错均拒绝，没有删掉安全断言来放行改变后的代码。
- Responses JSON/SSE、客户端 WS、Chat 非流式/流式：原始代码字节保留后成功，工作目录等明确元数据变化仍拒绝，未下发工具、未交给账号切换，保留纠错请求 ID 和累计用量。这些风险在既有跨协议测试中扩展，不另建重复流程。
- 最终完整 basispoints 竞态、service/handler/repository 的 BPS/Cyber/首输出/预算/恢复专项竞态（同时覆盖 #27195）、服务构建与 git diff --check 的结果追加在下方。

本项相对上一轮新增业务代码 +62/-10（1 文件），测试 +92/-25（2 文件）；文档与来源散列另计。公开接口、配置、迁移、独立执行路径新增均为 0。固定来源和 latest_observed_commit 保持不变。

仅离线模拟验证，未重放用户生产内容、未调用真实 BPS，不能据此保证任意格式错误或线上该请求都能恢复。修改与 #27195 一起留在独立工作树，尚未提交、推送、发版或部署。

最终检查（Go 1.27.2）：

- 完整 basispoints 包竞态通过（14.629s）：go test -race ./internal/service/basispoints -count=1 -timeout=180s。
- 最终 service / handler / repository 专项竞态通过（31.440s / 11.515s / 3.133s）：go test -race -tags=unit ./internal/service ./internal/handler ./internal/repository -run 'ExcelBPS|Cyber|Basispoints|OpenAITurn|OpenAI.*FirstOutput|OpenAI.*Replay|OpenAI.*Failover' -count=1 -timeout=240s。包含本项和此前未提交的附件恢复。
- go build -o /tmp/sub2api-bps-tool-correction-20261014 ./cmd/server 退出码 0；git diff --check 通过。未把专项回归称为全仓库或前端测试。
- 累计 11 个已登记改动条目的 SHA-256 复核通过；此前 #27195 的 7 个 Go 文件与本轮开始时快照逐字节一致。主目录 main 保持 b1b5790094d1bcf8060bd6568246d002c1b89991 且干净。

最终日志：/tmp/bps-27141-protocol-race.log、/tmp/bps-27141-regression-race.log、/tmp/bps-27141-build.log。上述两项尚未提交的累计业务 diff 为 +143/-22（4 文件），测试 +414/-25（6 文件），文档另计。

## #27195/#27141 的折叠交付（2026-10-14）

用户明确要求将上述两项修改合入之前 BPS 提交并推送。交付范围为三品牌共享网关；累计业务代码 +143/-22（4 文件），测试 +414/-25（6 文件），文档与来源清单另计。未增加公开接口、配置或迁移；附件阶段增加一条有界恢复分支，工具纠错扩展既有路径。

以 3f87d9ef2bcde59d01ea734a4fcd472676863660 为折叠目标。保留其后 5918d39d932d67dcc77758c1fa762918c1e12c98 的 ranxi 保留路径更新，以及 b1b5790094d1bcf8060bd6568246d002c1b89991 的官方上游合并；仅因父提交改变重新建立它们的提交对象，不改业务内容、原提交说明或官方上游第二父提交。折叠前保存三个交付分支 tip 和包含全部未提交内容的完整检查点。

交付门禁：折叠并恢复后续提交后的完整文件树必须与检查点一致；TapModels / tokensavy 普通 merge 后必须保留原品牌差异，且本轮 14 个共享文件与 main 一致。复用上节最终源码的竞态及构建证据，追加品牌入口回归和手动发版 workflow 检查。固定来源不变；本次不执行生产请求、发版、部署或数据库迁移。

推送仅限 holeenlu/sub2api：refs/heads/main:refs/heads/main、refs/heads/TapModels:refs/heads/TapModels、refs/heads/tokensavy:refs/heads/tokensavy。main 的授权历史替换使用固定旧 SHA 的 force-with-lease；品牌分支核对快进。最终完成状态与各分支 SHA 以推送后的 ls-remote 核对及交付回复为准，上文“尚未提交/推送”为实施验证时点记录。

## #28237 工具参数格式识别与一致性检查（2026-10-15）

基于 main 8e7aac151e352011b11f119cef4583581b4d0652，在 codex/bps-tool-payload-20261015 独立工作树复核并修复，归属为三品牌共享网关。包含上一轮 BPS 修复和其后的 New API/测试隔离更新，未变更 main 或品牌分支。

### 日志与本地复现

- 用户日志指向 gateway_tool_transport，第一次纠错、工具索引 0、raw_payload_changed、executed=false。上游事件实际 HTTP 200，客户端 502 来自网关终态工具操作一致性校验。原始模型响应及纠错后的工具载荷、生产运行版本均未提供，不能确认线上已部署前一轮推送，也不能断言此复现是生产请求的唯一根因。
- 现有普通工具转换器已经支持完整 JSON 围栏、短说明前缀、字符串编码层；纠错比较却仅严格解码未包装的 JSON。修改前，参数值不变的上述三种格式均可复现失败，失败原因同为 raw_payload_changed。首批复现日志 /tmp/bps-28237-red.log。
- 同一不一致还影响有 code 参数的函数：原参数包装可能被整体绑定为代码，而纠错模型修改后的工作目录被保留；正向原参数断言和负向元数据变化拒绝断言均在修改前失败。因此只放行错误码或删除操作一致性校验并不能正确修复。
- 扩展测试又发现：即使原始参数是纯 JSON，纠错改用 FUNCTION_CODE/FUNCTION_CMD RAW 标记也会错误绑定整份 JSON。该分支的八个正向格式组合在第一步局部修正后仍失败，日志 /tmp/bps-28237-raw-red.log；最终将参数对象识别移到所有 raw 绑定之前统一处理。

### 实现与边界

- 复用 decodeTransportCode 的现有格式、深度和大小限制；普通函数参数对象按完整值比较，保留 JSON 大整数。相同参数可由普通信封或 RAW code/cmd 运输，不把包装当源码，不忽略工作目录等元数据变化。
- 原始显式 RAW 标记继续区分源码与参数；CUSTOM 输入继续按字节保留。畸形/冲突具名包装、变更目标或真实参数、非法 schema、批次数量/顺序与身份不匹配仍拒绝，整批通过前不下发工具、不提交回放缓存。既有取消、两次纠错上限、用量累计、同账号/模型/代理约束及不重试不确定传输保持不变。
- 在现有失败诊断增加 original_code_shape、corrected_tool_kind，仅包含格式类别/长度/解析错误类别及已验证的工具种类。不增加代码、参数、工具名、调用 ID 或凭据；客户端仍只收到原有安全错误摘要。这是既有 Ops 诊断的两个内部字段，不是新公开 API 或配置。
- 原有恢复函数把普通 JSON 判断放在普通信封分支内，无法覆盖 RAW 结果；现把判断前移并复用同一解码器，删除原来重复的严格解码分支。未新增辅助层、公开接口、配置、迁移或独立执行路径。

### 验证范围

- 扩展已有参数回归矩阵：plain/fenced/prose/encoded 四种格式，普通函数、code 函数信封、RAW code、RAW cmd；不变参数成功，改变工作目录拒绝。验证原参数完整值、大整数、回放缓存一致性、模型响应对象不被修改、失败时无工具事件和无缓存，以及诊断脱敏。
- 同时验证显式 RAW 的带围栏 JSON 仍为源码，CUSTOM 的 JSON 仍为原字节；带围栏的畸形及冲突具名包装继续拒绝。复用原有协议整批验证、两次纠错、原始调用缓存和取消测试。
- 扩展已有 service 矩阵覆盖 Responses JSON/SSE、客户端 WS、Chat JSON/SSE 的格式参数恢复及元数据变化拒绝，检查不改账号调度、纠错请求 ID、累计用量和不下发失败工具。未添加平行入口或重复桥接逻辑。

本轮业务代码 +22/-10（1 文件），测试 +120/-38（2 文件），文档另计。四个已登记改动条目更新本地 SHA-256，固定来源和 latest_observed_commit 均不变。仅离线模拟验证，未重放用户生产内容、未调用真实 BPS；本轮尚未提交、推送、发版、部署或执行迁移。

最终检查（Go 1.27.2 darwin/arm64）：

- 完整 basispoints 竞态通过（12.779s）：go test -race ./internal/service/basispoints -count=1 -timeout=180s。
- service / handler / repository 的 BPS、Cyber、首输出、共享预算和恢复专项竞态通过（31.409s / 11.424s / 3.032s）：go test -race -tags=unit ./internal/service ./internal/handler ./internal/repository -run 'ExcelBPS|Cyber|Basispoints|OpenAITurn|OpenAI.*FirstOutput|OpenAI.*Replay|OpenAI.*Failover' -count=1 -timeout=240s。
- go build -o /tmp/sub2api-bps-tool-payload-20261015 ./cmd/server 退出码 0；gofmt 与 git diff --check 通过。未执行全仓库 unit 或前端测试，不将专项结果表述为全仓验证。
- 四个改动条目 SHA-256 与最终源码一致；来源固定点及 latest_observed_commit 未变。main 保持 8e7aac151e352011b11f119cef4583581b4d0652 且干净，全部修改留在本轮工作树。

最终日志：/tmp/bps-28237-protocol-race.log、/tmp/bps-28237-regression-race.log、/tmp/bps-28237-build.log。


## #28237 的折叠交付（2026-10-15）

用户明确要求将本轮七个文件的修改并入原 BPS 提交并推送。以 d47bb2d177a8add61eab92246ae47f907d10e3f6 为折叠目标；保留其后 ranxi 保留路径更新、官方上游合并、并发测试隔离及 New API Vertex/用量修复的业务内容和提交说明，保留官方上游合并的第二父提交。折叠前备份三个交付分支 tip，并保存包含全部未提交内容的完整检查点。

交付门禁：恢复后续提交后的完整文件树必须与本轮检查点一致；TapModels / tokensavy 通过普通 merge 接收 main，本轮七个共享文件与 main 一致，完整品牌差异与交付前逐字节一致。复用上节最终源码的竞态与构建证据，追加品牌 BPS 入口竞态回归和三个分支的手动发版 workflow 检查。业务代码 +22/-10（1 文件）、测试 +120/-38（2 文件）；公开接口、配置、迁移及独立执行路径新增均为 0。

推送仅限 holeenlu/sub2api 的 refs/heads/main:refs/heads/main、refs/heads/TapModels:refs/heads/TapModels、refs/heads/tokensavy:refs/heads/tokensavy。main 的授权历史替换使用精确旧 SHA 的 force-with-lease，品牌分支核对快进；最终状态以推送后的 ls-remote 和交付回复为准。此前“尚未提交/推送”为实施验证时点记录。本次不发版、不部署、不执行迁移或生产请求。

## 2026-10-15：BPS 429 同账号原生 Codex 回退

用户反馈 BPS 经常限流而未使用原生 Codex。本轮在 `9b793949da5520d2a0436feb9323da4a759e66c3` 的隔离工作区修改，主工作区其他任务的改动未纳入。归属为三品牌共享网关能力；当前验证不代表已提交、推送或部署。

- 原因：HTTP 429 只回到换号循环；BPS 冷却在普通、负载感知、高级调度和出站前复核中排除整个账号；HTTP 200 内的 429 只结束流。新增同账号原生授权回退，并统一所有调度过滤点。
- 无授权、不可迁移历史、未配对工具结果、不确定工具执行、已输出、工具纠错阶段、探针、取消、超时和耗尽预算的处理继续遵守已有边界。附件尚未生成且不可迁移时，保留原有同账号短等待补试。
- 复用现有 Forward/native HTTP bridge、原生令牌刷新、上下文检查、共享预算和用量汇总；新增内部连接归属标记，因为账号保存的 BPS 开关无法表达一次 WS 回退后响应/密文归属。公开接口、数据库、配置项均无新增。
- 测试覆盖两种 native 转发模式下的 HTTP JSON/SSE、WS、Chat/Messages 的 HTTP 429、流内 429 和既有冷却；完整 WS 两轮验证原生密文留在原生、冷却消失后仍不回到 BPS、模型映射不重复。
- 图片用例验证附件、压缩 HTTP/流内 429、压缩成功后生成 429：原生收到原图而非 BPS 附件或密文；已计量压缩用量累加一次。扩展现有工具纠错 429 用例使用双授权，验证不会因新回退重放工具。
- 同时修正原生 OAuth passthrough 把上游强制 stream=true 误当作客户端模式的问题；SSE 聚合 JSON 时显式覆盖 Content-Type，避免 Chat/Messages 非流式调用收到 SSE 或空 JSON。

验证使用 Go 1.27.2，最终结果：

| 检查 | 结果 |
| --- | --- |
| `go test -race -tags=unit ./internal/service ./internal/handler ./internal/repository -run 'ExcelBPS\|Cyber\|Basispoints\|OpenAITurn\|OpenAI.*FirstOutput\|OpenAI.*Replay\|OpenAI.*Failover\|OpenAIWSBPS\|Passthrough\|SSEToJSON' -skip '^TestIsUpstreamModelRestrictedByChannel_PassthroughFlagWithRawChatFallbackMatchesForwardPath$' -count=1 -timeout=240s` | 通过：service 53.482s、handler 13.719s、repository 3.116s；包括上述全部新增用例及原有 Excel-only 换号、不可迁移附件重试边界 |
| `go test -race ./internal/service/basispoints -count=1 -timeout=180s` | 通过：17.406s |
| `go build -o /tmp/sub2api-bps-429-native-20261015 ./cmd/server` | 通过 |
| `git diff --check`，本轮改动的来源清单 SHA-256 | 通过 |

最初的宽匹配 race 回归发现 `TestIsUpstreamModelRestrictedByChannel_PassthroughFlagWithRawChatFallbackMatchesForwardPath` 两个并行子测试共享同一个 Account，读写 `GetModelMapping` 的可变缓存。使用 Go overlay 将所有本轮 Go 改动还原至基点 `9b793949`，对该测试运行 `-race -count=10` 也复现相同竞态。该问题未混入 BPS 修改；最终命令明确排除这一个既有测试，不能描述为无条件全量 race 通过。

代码统计：业务 9 文件 +186/-36 行；测试 5 文件 +306/-11 行；文档/来源清单 3 文件。新增公开接口 0、配置项 0、数据库变更 0；新增恢复执行路径 2 条（HTTP Forward 和 WS HTTP bridge，Chat/Messages 复用 HTTP 路径），内部 WS 连接归属标记 1 个。

本轮使用本地模拟上游，未连接真实 BPS/Codex 或生产服务；没有新的生产 429 载荷，因此不能据此判断上游真实限流额度，也不承诺原生授权一定有可用额度。未执行全仓库/前端测试、提交、推送或部署。


### 2026-10-15：429 回退修复折叠交付

用户授权将本轮改动折叠到已有 BPS 修复提交后推送。交付固定 main 基点 `c03e51c364946ad02fedffa7356f8732c230b456`、原 BPS 修复 `f02568f3d0742e7516c78fbf1303f334a9daac25`；本轮开发基点之后 main 的五个文件更新不与本轮 BPS 改动重叠。折叠时保留后续提交内容、作者及所有上游合并的其他父节点；两个品牌以原 tip 为第一父节点普通 merge 新 main，保留品牌覆盖。

仅向 holeenlu/sub2api 的 main、TapModels、tokensavy 同名分支推送。main 使用精确原 tip 的 force-with-lease，品牌保持快进；先保存三分支 tip、主工作区及本任务的完整检查点，再核对最终文件树与主工作区未提交修改。仅运行质量检查，发版仍为 workflow_dispatch，不部署或执行迁移。此前“尚未提交/推送”为实施时点记录，最终 SHA 以推送后 ls-remote 核验和交付回复为准。

## #29823：附件 429 未补试原因与上下文所有权诊断

本轮基于已交付 main 12085501cb12b7d1b2e93d5ea33a991be5303d6c，在独立 BPS 工作区处理三品牌共享网关诊断。主工作区其他任务的供应商相关未提交内容保持原样。

线上记录证明附件接口返回 429、生成尚未开始、网关因 context_not_portable 停止恢复，attempts=0。没有提供生产运行版本、Retry-After、当前回避设置、共享预算配置或请求结构，因此不能断定具体跳过原因，也不能仅凭 unresolved_tool_context 认定哪一个字段触发限制。

- 原有恢复策略保持：不可迁移上下文不跨账号或切换 Excel/Codex 授权；被明确拒绝的附件在同账号/凭据/代理下最多补试一次，等待 1–5 秒，遵守上游 Retry-After、默认回避开关、首输出期限、尝试预算和并发延长的冷却。成功上传的附件继续保留，生成不重放。
- 扩展既有附件错误对象与 Ops 事件，记录附件实际补试次数、固定停止原因、等待来源/毫秒数和 5 秒上限；区分 short_wait 等待计划与真正发送的补试。等待超限、关闭默认回避、预算留给生成、期限不足、冷却延长和一次补试耗尽可分别定位。
- 原生上下文所有权扫描改为返回固定结构原因，布尔判定复用它，避免复制一套诊断规则；原因包括 encrypted_content、item_reference、previous_response_id、file_id 等，不保存值、密文、ID、工具参数或原始 Retry-After。WS 新 turn 清理诊断，已有工具执行安全判断保持独立。
- 扩展原有测试，先在修改前复现附件诊断缺失，再覆盖长 Retry-After、较长/关闭的配置回避、尝试预算、期限、重复 429、并发延长冷却，以及 HTTP JSON/SSE/WS 的原账号保持、次数与脱敏；原生所有权拒绝/允许及跨 turn 清理同时验证。

验证（Go 1.27.2，本地模拟上游）：

- go test -race -tags=unit ./internal/service ./internal/handler -run 'ExcelBPS|OpenAIWS.*Retry|OpenAIWS.*Replay|OpenAIResponsesAccountReplay|OpenAIRetryStop|OpenAI.*Failover|OpenAI.*Recovery' -count=1 -timeout=240s：通过，service 26.663s、handler 8.900s；本轮命令没有排除匹配用例。
- go build -o /tmp/sub2api-bps-29823 ./cmd/server：退出码 0。
- gofmt、git diff --check、本轮 7 个 Go 文件的清单 SHA-256：通过。日志 /tmp/bps-29823-before.log、/tmp/bps-29823-regression.log、/tmp/bps-29823-build.log。
- 业务代码 3 文件 +107/-30 行，测试 4 文件 +66/-20 行，文档/来源清单 3 文件；新增公开接口、配置项、数据库变更、恢复执行路径均为 0。

本轮补齐诊断，没有消除上游限流，也没有放宽上下文迁移规则。未调用真实 BPS/Codex，未验证生产部署版本或线上恢复，未运行全仓库/前端测试；当前修改未提交、推送或部署。

### #29823 折叠交付

用户授权将本轮 10 文件修改折叠到原 BPS 提交 34037613067dc9d1802b4290206f8d90ddaac4e7 并推送。交付基点 main 为 99087eefebaa369c5f1d7f482f5d928dbd3b1de6；相对开发基点的后续上游合并仅改动 7 个前端文件，本轮 Go 文件及 BPS 文档未变。重写保留后续提交内容、作者、说明和上游合并父节点；两个品牌通过普通 merge 接收，并核对本轮文件之外的全部品牌内容保持不变。

折叠前保存三分支本地/远端 tip、两处工作区的跟踪文件补丁及未跟踪文件。只推送 holeenlu/sub2api 的 main、TapModels、tokensavy 同名完整 refs；main 使用精确旧 SHA 的 force-with-lease，品牌保持快进。最终共享后端树若与已验证版本一致，则复用上节竞态和构建证据；品牌增加定向检查及手动发版 workflow 检查。此前“当前修改未提交/推送”为实施时点记录，完成状态以远端 SHA 核验及交付回复为准。

## 2026-10-16 #29895：WS 附件 429 的冻结错误归因

日志中的 short_wait → retry_exhausted 已证明同账号附件补试执行一次：等待约 5 秒后，第二次上传仍返回 429。原始上下文包含 encrypted_content，故恢复按既有约定停止，不切原生 Codex、不换账号；无生成或客户端语义输出不等于上下文可迁移。本轮不调整次数、等待、冷却或迁移规则，也不声称已消除真实上游限流。

本地复现发现独立缺陷：WS 错误快照保存了附件事件，但其空的上游状态/消息/详情标量覆盖了落库时已从事件解析出的 429。修复复用 HTTP 的最后真实上游尝试解析，输入改为本轮冻结事件；retry_stopped 保留为恢复诊断，不抢占上游归因。最后一次无 HTTP 响应的传输失败清除陈旧状态，account_auth 保留状态 0 及其阶段；无真实尝试时保留快照标量。后续 turn 和快照后事件修改不能污染旧错误。

- 扩展原有 HTTP JSON/SSE/WS 附件测试，保持两次上传、同账号、无生成的断言；WS 经实际 handler、冻结快照及 Ops 入队验证真实 429、消息和重试详情。修改前新增断言因 UpstreamStatusCode 为 nil 失败（/tmp/bps-29895-red.log）。
- 扩展既有多轮 WS 测试，同时覆盖上述归因边界、每轮不同客户端状态、首次错误优先及事件深拷贝；没有新增重试路径或另建归因规则。

验证（Go 1.27.2，本地模拟上游）：

- 定向测试通过：handler 3.066s（/tmp/bps-29895-focused.log）。
- go test -race -tags=unit ./internal/service ./internal/handler -run 'ExcelBPS|Ops|OpenAIWS.*Retry|OpenAIWS.*Replay|OpenAIResponsesAccountReplay|OpenAIRetryStop|OpenAI.*Failover|OpenAI.*Recovery|OpenAI.*Credential' -count=1 -timeout=240s：通过，service 28.385s、handler 9.214s（/tmp/bps-29895-regression.log），未排除匹配用例。
- 最后仅加强每轮客户端状态的测试断言，再次运行两个已修改测试的全部子用例并启用 race：通过，handler 4.177s（/tmp/bps-29895-final-focused.log）；业务代码与上述广泛回归一致。
- go build -o /tmp/sub2api-bps-29895 ./cmd/server：退出码 0（/tmp/bps-29895-build.log）。gofmt、git diff --check、本轮三个 Go 文件清单 SHA-256 核验通过。
- 业务代码 1 文件 +3/-1 行；测试 2 文件 +78/-15 行；文档/来源清单 3 文件。新增公开接口、配置项、数据库变更及恢复执行路径均为 0。归属为共享网关逻辑，未涉及品牌定制。

未调用真实 BPS/Codex，未验证线上部署或限流解除，未运行全仓库/前端测试；当前修改位于独立工作区，未提交、推送或部署。

### #29895 折叠交付

用户授权将本轮六文件修改并入原 BPS 提交 3b7140e7cfce019e1e39325c30c9c1d9f8f7054b 并推送。按 main 最新交付规范先保存三分支 tip、暂存/未暂存补丁与未跟踪文件；仅折叠本轮差异，保留其后提交内容和上游合并父节点。共享改动经 main 普通 merge 到两个品牌，逐一核对实际差异、品牌保留及远端 SHA；main 历史改写只使用精确旧远端 SHA 的 force-with-lease。交付验证以最终 main 为准，未触发发版、生产部署或数据库迁移。前述未提交/推送为实施时点记录，完成状态以交付回复和远端核验为准。


## 2026-10-17 原生 32 MiB 预检激活与诊断（#31015–#31018）

- 代码路径复核：该原文来自本地 NativeImages 计划，发送前返回 400；不能据此认定上游拒绝或压缩已经执行。旧 off 默认跳过策略，能够复现；未读取线上设置或运行版本。
- 统一缺省为 auto_compact，保留已有显式 off/warn；前端明确显示关闭状态及无法靠原样重试消除字节超限。关闭时先返回独立错误码和安全计数诊断，不启动附件上传或计费压缩。
- JSON、SSE、WS、工具截图、下一轮检查点均复用原压缩流程；17+17 MiB、32 MiB 边界、历史/新增各自超限、无可信状态、手动 compact 超限、压缩失败、压缩后仍超限、后段上传429及用量归集已覆盖。不会丢图、解密或多轮强制拆分。
- 通过：basispoints 整包（3.698s）；service/handler BPS、图片、恢复、预算相关回归（44.667s / 51.871s），admin 包编译成功但该筛选无匹配用例；定向 race 覆盖容量/关闭诊断/默认及保存策略（66.581s）；三个相关包 go vet。
- 前端 SettingsView 和语言键共58例通过；vue-tsc -b、主站与供应商构建、修改文件 ESLint、繁体生成校验通过；手动 Workflow 保护4例通过。构建仅有现存包体和浏览器数据提示。
- 未做真实上游、线上配置写入、生产部署或付费压测。已有站点应检查 /admin/settings → Excel / BPS 图片 → 图片上限策略；保存 auto_compact 后才会对可安全分界的历史执行自动压缩。历史或新增单独超过32 MiB仍需缩小/拆分；更新代码不会覆盖管理员明确关闭的选择。


## 2026-10-18 新上传引用仍出现文件归属500（#31490）

- 日志为生成入口真实HTTP500，fresh_upload_references=2、reused/unmanaged=0；引用次数不等于上传数，无法排除未展示的压缩/推理密文依赖，也不能据此断言上游文件服务具体故障。
- 复核当前代码：上传、压缩和生成共用本次取得的Excel token和账号ID，并使用选中账号的代理配置；资格校验包装不替换请求授权。代理本身是否轮换出口及线上运行版本未经验证。
- 补充不同新上传文件数、失败出站的opaque compaction/reasoning项计数和本请求已完成压缩标志。仅故障且原日志开关开启时存储，客户端响应和上游原文不混入本地诊断；不记录文件ID、密文、图片URL或令牌。
- 复用并扩展已有JSON/SSE/WS归属失败矩阵：新图、重复图、客户端file_id、纯文本及含opaque历史；检查同一授权/账号/代理、不重放、不污染健康、流失败用量，以及后续显式请求重新上传。工具参数内的伪引用/密文不计入诊断。
- basispoints整包及相关原生附件、归属、跨轮、压缩回归通过；未调用真实上游，未确认生产根因。本次完善可观测性，恢复策略保持原有边界。


## #31791 工具 JSON 分隔符恢复与原操作核验（2026-10-19）

- 归属三品牌共享 BPS 转换器。用户日志为 Chat SSE，真实上游 HTTP 200，网关在第一次工具纠错后拒绝，结构诊断是 format=json_object、bytes=3502、json_offset=3061、missing_separator、raw_payload_changed。该分类既可来自逗号/冒号缺失，也可来自未转义引号；未取得生产原始/纠正后载荷，不将离线复现当成生产唯一根因。
- 修改前复现了普通函数的同参数纠正误拒绝，以及 code/cmd 路径把畸形参数对象整体绑定成源码、保留已变化工作目录的问题。前者对应报告中的错误类别，后者说明不能简单放行 raw_payload_changed。
- 扩展既有 decodeEnvelopeValue：仅依严格 JSON 解析器给出的上下文，在已有空白分隔的完整 token 之间补逗号/冒号；最多 8 次、结果不超 1 MiB，保留所有原字节及大整数。恢复后检查所有嵌套对象的重复键（包括 Unicode 转义别名），不与非法字符串转义/控制字符修复叠加，不补引号、值或闭合符，不修紧邻 token。该本地分支不调用上游模型。
- 具名且目录/schema 有效的信封直接转换，裸参数仍复用原有最多两次纠错补目标，并在 RAW 绑定前逐值核对。显式 CUSTOM/CODE/CMD 内容保持原字节；无法解码的未标记 JSON 不能作为完整程序发给客户端。原文无法核验采用既有 original_payload_unverifiable 原因，并明确区别于已确认的操作变化。整批验证、回放提交、用量和同账号/模型/出口约束继续保留。
- 扩展既有参数回归矩阵，覆盖普通函数、code 信封、RAW code/cmd 的未变/已变元数据；JSON/SSE/WS/Chat JSON/Chat SSE 覆盖恢复成功和拒绝边界。新增纯合成 3502 字节/3061 偏移样例确认无需模型纠错即可转换、保留原 native 回放及用量；覆盖八次/九次修复、字节上限、重复键、未转义引号、截断与混合错误。未使用真实用户载荷。
- 新增接口、配置、上游调用路径均为 0；在现有解码器增加 1 个有界本地恢复分支，去重键扫描只为该恢复的安全核验，不引入平行传输层。业务代码 +114/-2（2 文件），测试 +138/-2（3 文件），文档另计。来源 SHA 与 latest_observed_commit 不变，仅更新五个本次改动条目的本地指纹。
- 完整 basispoints 整包通过（2.323s）；协议/格式/纠错专项 race 通过（2.870s）。跨传输 service 定向 race 通过（1.407s）；覆盖 TestExcelBPSToolArgumentCorrectionAcrossTransports 与 TestExcelBPSToolCorrectionPreservesRouteAndUsage，启用 unit 标签、count=1、timeout=120s。gofmt 与 git diff --check 通过；无前端改动，未调用真实 BPS、未重放生产请求。当前改动位于原工作区的上述 BPS 文件，未移动分支或暂存其他任务内容；尚未提交、推送、发版、部署或迁移。

日志：/tmp/sub2api-bps-31791-package.log、/tmp/sub2api-bps-31791-race.log、/tmp/sub2api-bps-31791-service-race.log。


## #31847 首输出超时状态与停止诊断（2026-10-19）

- 归属为共享 BPS/网关错误归因。日志中实际 BPS HTTP 状态为 200，约 299893 ms 只观察到两条元数据事件，最后为 response.in_progress，无可交付语义输出；本地首输出计时取消读取。计数来自 BPS 转换后的流，工具增量在整批验证前会被暂存，因此不能据“两条事件”断言原始上游未产生任何 token。共享绝对期限已经耗尽，现行恢复合同不会通过换号或原生回退重新分配期限。不能据此确认上游卡住的具体原因，亦不能将调度摘要的 complete 当成生成成功。
- 复现缺口：Ops 的 SSE 分类未识别 first_output_timeout，显示成 502；BPS 自行生成终态时未接入既有网关来源标记，导致实际 HTTP 200 被逻辑 502 覆盖。Chat SSE 再分类丢失原超时码，Chat JSON 通用错误助手也省略 code。原超时分支没有记录 retry_stopped，无法直接看到整轮期限已耗尽。
- 保留既有本地超时/取消和不重放决定；补齐 first_output_timeout -> 504 分类，BPS 与 handler 共用原有网关合成终态标记，WS 下一轮清除该标记。标记本地错误的真实语义状态，保留上游实际 HTTP 状态与请求 ID；超时终止前记录已有 turn_budget 停止原因及预算快照。Chat JSON 保留 Forward 已生成的安全 code/param，SSE 复用既有安全错误分类器。
- 扩展已有头部等待/仅元数据回归至 Responses JSON/SSE、WS、Chat JSON/SSE，验证一次实际发送、共享期限耗尽、不返回可重试 failover、实际用量、504、真实 HTTP 200 与 WS 下一轮来源隔离。Ops 中间件回归验证已建立流的 wire 200、逻辑 504 和真实上游 HTTP 200 可以同时保留；上游真实失败仍覆盖早前尝试。
- 原恢复文档中“0 关闭首输出计时”的旧说明与当前代码不符，已校正：标准默认 300 秒；high/xhigh/max 的专用值为 0 时继承显式标准值，两项均为 0 时使用 600 秒；默认生成尝试上限为 8。未修改任何配置值或生产设置。
- 本轮业务代码 +34/-6（7 文件），测试 +77/-5（4 文件）；文档另计，未包含上轮 #31791 已推送的工作区副本。无新增对外接口、配置项或上游执行路径；复用已有诊断标记、停止事件和转换入口。最终验证：basispoints 整包 race 通过（26.316s）；service 全部 ExcelBPS 功能测试通过（36.002s）；首输出、取消/WS、流中断、用量及归因专项 race 通过（3.708s）；handler 的 Ops/来源归因专项 race 通过（51.975s）。扩展 service 全 BPS race 在 TestExcelBPSImageByteCompactionFlow 大图组触发 240 秒测试上限，未完整通过，未出现断言失败或数据竞态告警；不以功能测试替代该项未完成的竞态覆盖。
- 未调用真实 BPS、未重放用户请求，不能将诊断修复表述为上游等待问题已消除。当前改动尚未提交、推送、发版、部署或迁移；其他任务的代码、分支和暂存区保持原有现场。

验证日志：/tmp/sub2api-bps-31847-package-race.log、/tmp/sub2api-bps-31847-targeted-race.log、/tmp/sub2api-bps-31847-all-bps.log；扩展竞态超时与 handler 通过结果保留在 /tmp/sub2api-bps-31847-regression-race.log。

## 2026-10-19 BPS 当前版本全链路复核

按此前未完成项补齐授权、协议、缓存、数据和计费复核，新增真实 PostgreSQL/Redis 验收并修复四项可复现缺陷。范围、线上证据、最终测试和部署边界见 [本轮专项报告](FULL_PATH_REVIEW_2026-10-19.md)。不以原生 OpenAI 回归或混合余额聚合替代 BPS 独立上游对账。
