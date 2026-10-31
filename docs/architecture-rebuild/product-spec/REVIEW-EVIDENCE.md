# 前端差异复核技术证据

最终前端源码已停写并冻结为 `e5a6080563419821daef5eb96e29a95b95f8d80c722b2f8af68dca762878277d`（2009文件、18,901,651 bytes）。c2维护35路径的退休、显式模型、metadata与类型修复已应用；完整测试/双构建的真实输入保留63fe，按唯一测试类型补丁的等价证明复用于e5，五项static直接绑定e5。当前catalog为753用例、92页面、51面板、10管理设置tab，业务定义全部not_run；后端及联合Docker/UI/外部验收尚未完成。

本附录记录 2026-10-27 原始 `6f48d6ab6fcc29763af84410690530aeaa957403` 与重构源码的差异、固定本地维护输入 `025945a5233d0c9698a5ac63a9e6a79109b25bfa` 的适配及本轮实际验证；没有重新查询远端。临时 Git 根 `673c53c13` 不包含未提交重构；历史 d4d7bfae、748782a3、f9141d4e 检查点不代表当前源。

历史f4fe检查点为2011文件、18,873,504 bytes，旧6005测试等原始回执保存在docs-f4fe-*副本中；仅作历史，不改绑当前e5。最终source、63fe实际测试/构建输入及复用证明见下节。

历史5c3005清单为2,006文件、18,826,234 bytes，保留在 `frontend/.local/frontend-completion-20261027/source-manifest.json`。Excel补漏前5085清单、制品计量与构建回执保留在build-before-excel-auth；两检查点的7个变化文件见delta-after-5085.json。旧性能和Docker记录继续绑定其原始hash，不改绑f4fe或后续源码。

此前相对 ec 冻结集合的 137 修改、29 新增、3 删除记录保留在 `frontend/.local/frontend-rule-closure/delta-from-ec-freeze.json`。它只描述该历史批次，不是本轮与原始项目的业务增删数量，目录迁移不能当作新增功能。

<a id="final-e5-evidence"></a>
## 最终前端证据

最终SHA256为 `e5a6080563419821daef5eb96e29a95b95f8d80c722b2f8af68dca762878277d`，2009文件、18,901,651 bytes。权威交接为 [HANDOFF](../../../frontend/.local/frontend-final-c2-fixed-20261027/HANDOFF.md) 和 [integration-receipt](../../../frontend/.local/frontend-final-c2-fixed-20261027/integration-receipt.json)。本文只核对既有证据与源码引用，没有执行应用测试/构建或写入frontend。

| 验证 | 实际结果与输入 | 对最终e5的适用范围 |
| --- | --- | --- |
| 完整Vitest | 600文件、6196/6196，0 failed/pending/todo，146.408秒；真实输入/输出63fe672756a98e4bd842d36dc1494181e6fb3338186872769efadb97e04972f8，无漂移 | 按类型补丁证明复用，不称在e5重跑 |
| 五项static | 生产types、构建配置types、变更测试types、ESLint、生成器均exit0；输入/输出e5，无漂移 | 直接验证最终输入 |
| 主站/供应商build | 均exit0，16.094/11.936秒；输入/输出63fe，无漂移 | 生产输入相等，按作用域复用，不改写build回执 |
| 测试类型补验 | OpsNodeMetrics.spec.ts 9/9，0失败/待运行 | type-only import和OpsJobHeartbeat[]标注后执行，不与6196相加 |
| Catalog回归 | 路由/panel/tab覆盖6/6，包含在6196中 | 753业务定义仍全部not_run |

63fe→e5仅 `frontend/src/features/operations/dashboard/components/__tests__/OpsNodeMetrics.spec.ts` 一文件变化。`test-type-fix.json`实测编译JS逐字节一致；`production-reuse-proof.json`逐个生产输入相等，package/config及其他文件未变。生产输入哈希比较为1316文件；生产TS/TSX/Vue AST口径另为1197，不混用两类计数。原始full-tests-run、builds、source-metrics输入hash仍为63fe。

`source-metrics.json`测于63fe：frontend/src 1942文件、生产脚本1197、显式AnyKeyword 0，package/lock与原始6f字节相同。按生产等价复用于e5，不称重新执行AST；不计测试/template/字符串/注释或隐式any。EditAccountModal 2115行、BulkEditAccountModal 1953行、AccountWorkbench 1857行均为未变的生产文件。

当前制品来自63fe并复用于e5，完整文件sha和制品hash见`build-summary.json`：

| 磁盘字节 | 主站 | 供应商 |
| --- | --- | --- |
| 入口JS | 370,376 | 370,142 |
| 入口静态依赖JS合计 | 611,885 | 611,651 |
| AccountsPage chunk | 369,886 | 369,886 |
| xlsx chunk | 500,005 | 500,005 |
| 制品文件/总字节 | 489 / 10,500,961 | 489 / 10,500,721 |

两角色仍有router/adminSettings两类静态/动态混合导入提示，无默认大chunk提示，也没有调高阈值。静态字节不是完整页面流量或浏览器性能；本轮未重测当前源码的浏览器性能。历史5085/5c3005性能及旧Docker继续绑定原hash。

`delta-final-from-f4fe.json`精确绑定e5：9新增、11删除、62修改；早先`delta-from-f4fe.json`仍绑定63fe。模板worker删除10文件，其中一个API测试在该worker阶段先新建后删除，因此不叠加各阶段删除数，也不宣称相对f4fe删除12个。

本轮电脑工具只预检本机`/web-build.json`，看到旧90c927992b545676e5080c936125a3c474598493999877f2615c3e282c10e539构建，临时tab已关闭、未执行业务动作。供应商安全恢复DTO、stale-enabled草稿后端原子合同、后端整体及最终Docker/全站/真实外部验收仍未完成。成功测试自动恢复保留BPS scope，管理员明确恢复覆盖是独立权限/端点合同，二者分开验收。

## 历史d629失败证据

d629仅为中间检查点：static为4项通过，changed-lint因accountRateLimitScopes.ts的no-control-regex失败。MAIN改为显式charCode范围校验，没有禁lint或改变验证语义。原失败目录与指标保留；后续最终证据见前述e5/63fe分层，不将中间状态写成最终通过。

## 1 比较与统计口径

| 指标 | 原始6f | 63fe实测，生产不变复用于e5 | 限制 |
| --- | --- | --- | --- |
| frontend/src 文件 | 1,215 | 1,942 | 包含测试、语言、内容和实现文件 |
| 生产 TS/TSX/Vue 文件 | 679 | 1,197 | 不包含测试 |
| 脚本显式 AnyKeyword | 352 | 0 | TypeScript AST；不统计测试、Vue template 或隐式 any，不证明运行时校验完整 |
| package.json / 锁文件 | 固定原始字节 | 相同 | 未升级依赖 |
| 规范路由 / alias | 99 / 2 | 99 / 2 | 路径集合不等于业务验收 |

当前源码按 app、14 个 feature 和 shared 组织。被替代的 api/stores/types/views/components/utils/composables/constants/router 根桶没有源文件或兼容转发壳；依赖扫描包含静态、动态、路由及类型边，不豁免页面导入。指标见 [source-review.json](source-review.json)，路由与需求追踪见 [traceability.json](traceability.json)。

<a id="c2a10f2-维护输入与待应用映射"></a>
## c2a10f2 维护输入与应用映射

MAIN已通过实时ls-remote确认c2a10f2aae9597ca2fe8c94813e21b00b6bde53a；本资料owner只读取本地025945/c2 Git对象和差异，没有重复网络查询、应用源码或运行测试。相对025945共35条前端路径：27修改、7删除、1新增，607行增加、708行删除；行数是该维护差异，不是重构当前业务增删。逐路径SHA256、语义、目标候选和owner见[source-review](source-review.json)的maintenance_sync.pending_wave，证据位于frontend/.local/bps-frontend-integration-20261027/docs-c2a10f2-semantic-mapping.json。

| 维护变化 | 重构目标与owner | 当前验证边界 |
| --- | --- | --- |
| 默认模板与mode链退休 | Arendt：删除旧Card/Panel/API/hook/utils/mode及注册、独有测试/文案；Settings移除旧bps tab，保留features内全局开关与图片控制 | Arendt已停写交接，MAIN核验34个现存hash、10个删除；当前catalog已删除两条模板用例；其余owner及最终组合验收仍待完成 |
| 安全恢复DTO与历史状态 | MAIN：复用现有BPSFailureBadge与新的bpsRecoveryPresentation；公开DTO取代私有failure，保留未知与严格时间规则 | MAIN已交接生产reader迁移及220相关tests；不等于全部c2 UI语义或最终snapshot完成 |
| 账号信息与tooltip | Meitner已交接：复制与详情分开、结构化可选中文本、去重复proxy显示、metadata slot；tooltip hover间隙、点击固定、选择与生命周期清理 | 9文件hash与d629对应；190项组件回归及1197生产+4变更spec联合类型检查exit0，原184项类型错误已修复；真实浏览器与最终构建仍未验收 |
| 显式模型选择 | 更晚用户修订，由Arendt处理创建/编辑/批量/供应商；Cloud优先且保留列表 | c2本身仍有all-models兼容，不能把新要求列为维护已实现 |

冲突证据属于不可变c2源码：EditAccountModal.vue第4612行仍由缺失openai_excel_bps_models推导all-models，第6228行仍有all-models分支；BulkEditAccountModal.vue第2223–2224行仍有null删除scope及all-models载荷语义。当前要求是仅明确勾选，缺失/空/null/畸形都不全选，不自动补全历史账号。不可将旧兼容、c2内置候选或历史测试当成这条新要求的实现证据。

此前52面板/752用例及355显式引用/397章节关联仅属旧目录审查快照。MAIN新catalog删除两条默认模板用例、新增三条BPS用例，当前为51面板/753用例，均not_run；当前追踪重新计算，不把旧数量当作固定目标。旧f4fe的6005测试/597文件、5检查、2构建及旧性能/Docker均保留原hash；当前最终前端source为e5；后端及联合runtime验收仍未完成。

### c2逐路径应用与局部验证

35条维护路径已逐行更新为实际应用或退休状态，原c2源码hash不变；应用依据是worker语义交接、MAIN确认及各文件与d629中间清单的对应。Arendt为34现存文件/10删除，报告13文件390项局部测试；[Meitner交接](../../../frontend/.local/frontend-maintenance-c2-20261027/handoff.md)覆盖9文件，后续[类型修复](../../../frontend/.local/frontend-maintenance-c2-20261027/type-repair/handoff.md)明确1197生产文件+全部4个变更spec联合检查exit0、8文件190项测试通过，原184项错误不再作为豁免。MAIN安全DTO的220项与catalog 6/6另列，各集合不相加。

应用及最终前端开发证据已完成，753业务用例仍not_run。d629 lint失败保留历史；当前五项static在e5通过，全套测试/双build按63fe证据复用。表中1942/1197/AnyKeyword0及行数来自63fe测量；AnyKeyword口径排除测试、template、字符串/注释及隐式any，不能声称所有测试弱类型清零。package/lock在该检查点与6f字节一致。

## 2 历史检查点已记录实现

- 生产 Router 消费唯一供应商守卫；优先级快捷编辑捕获 actor、字段权限、目标和意图，卸载取消而非 flush。退款资格与首页目录查询开关按已有合同收口。FR-04 旧误报继续撤回，原角色目录值已正确。
- 设置工作区与委派设置共用保存结果处理。确认 PUT 成功才更新基线；公共配置刷新先失效缓存代际，私有管理员/支付配置刷新可显式报告失败。刷新不重放 PUT、不抹掉其他分区或提交后的草稿，旧 owner 不接纳迟到反馈。
- Account 创建/编辑/批量弹窗按打开意图懒加载。批量 patch 由纯模型组合，保留 JSONB merge、false/0/空值哨兵；撤销的隐藏草稿不再联动清空其他字段。供应商缺少自己的 writer 时拒绝，不能落回 admin API。
- 作业预览的 IDB、缩略图解码和请求/URL 生命周期各有 owner。保留数据库名、scope key、3 天 TTL、120 条/48 MiB、360px/质量 .72。取消等待、迟到 bitmap/图片错误、坏缓存重试和 IDB 拒绝都有回归；页面旧内联实现和重复卸载清理退出。
- 主动认证刷新适配固定 b22 的 generation、用户、原 refresh token 与持久快照复核；采用已持久化到期时间，不因迟到响应延长会话。渠道页 sticky offset 同步维护输入。
- 三份繁体文档按既有生成器重建；测试夹具修复真实 symbol 注入类型，设置 fixture 使用完整类型核对，不再强转缺失字段对象为完整合同。
- 主应用壳、公告铃/弹窗、管理员合规弹窗根据实际显示意图加载；没有通过导出公告铃的静态 barrel 把弹窗依赖重新带回匿名入口。退出后的迟到模块不挂载私有界面。
- 公告铃、弹窗与 BaseDialog 共用滚动锁所有权；关闭一个叠加层不会解锁其他仍显示的对话框，最后释放时恢复已有 overflow 值、轴和优先级。新增共享设施有三个真实消费者，不是空架构。
- 历史5c3005已记录系统停用后隐藏配置保留、四语言执行边界和Excel授权修复。后续f4fe加入冷却隔离、BPSFailureBadge、运行态过滤和显式停止本地意图；但私有failure字段被管理mapper隐藏，不能将其单测视为真实投影已接通。新管理安全DTO消费迁移、supplier安全投影、混合scope清理、旧true窗口及最终合同边界见[设置与合同](SETTINGS_AND_CONTRACTS.md#5-服务端依赖边界)。
- Excel 主授权 authorized=false 明确显示需重新授权，并指引账号 OAuth 管理；未知或未读到状态不开放独立 grant 写入。生成链接、交换、保存、删除和重读均复核捕获的 scope/权限/目标；空scope即使权限投影尚为true也不操作，恢复有效scope后重新查询。秘密草稿在失效时清除，导入JSON必须是对象，服务端继续验证凭据内容。

63fe实测并复用于e5的大型模板：AccountsPage 2,583行、BulkEditAccountModal 1,953行、BatchImageJobsPage 2,409行、CreateAccountModal 2,225行、EditAccountModal 2,115行、AccountWorkbench 1,857行；原测量输入63fe不改写为e5。拆分减少了职责混杂，不能把行数下降当作业务删除或性能证明。

## 3 历史f4fe开发检查点

以下均读取 `frontend/.local/bps-frontend-integration-20261027/` 的f4fe现有证据；五份原始JSON逐字节保存为同目录 `docs-f4fe-{source-manifest,full-tests-run,full-tests,static-checks,builds}.json`，防止后续新回执覆盖历史归属。本次没有在本次文档审计中重新执行应用测试、类型检查、lint、构建或浏览器。所有列出的run记录输入和输出均为f4fe、source_drift为空，只证明该检查点；该历史测试不直接认证后续安全DTO迁移。

| 检查 | 已记录结果 | 证据 |
| --- | --- | --- |
| 完整Vitest | 597文件、6005项通过；0失败/待运行/todo，109.392秒 | full-tests.json与full-tests-run.json；1458是Vitest suite数，不是文件数 |
| 生产vue-tsc | exit0，18.638秒 | static-checks.json / production-types.log |
| 构建配置tsc | exit0，0.982秒 | static-checks.json / node-types.log |
| 变更测试类型 | exit0，18.933秒 | static-checks.json / changed-types.log；不声称所有历史测试均完成静态检查 |
| 变更文件ESLint | exit0，2.432秒 | static-checks.json / changed-lint.log |
| zh-TW生成检查 | exit0，0.542秒 | static-checks.json / generated-i18n.log |
| 主站/供应商Vite | 均exit0，12.719 / 11.430秒 | builds.json；未部署Docker |

payload-handoff、failure-handoff、cooldown-validation及stop-tests-run是开发过程记录，不能与全量6005项叠加。旧命名与失败尝试保留原始证据；BPSFailureBadge的新名及当时删除清单以f4fe记录为准。f4fe不是当前最终冻结：管理API已公开 `Account.excel_bps_403_recovery`，MAIN随后确认生产consumer已迁至安全reader，旧reader/专属测试删除无shim；七状态及严格时间parser相关220项测试按MAIN交接另记，不并入6005；最终前端snapshot为e5，后端与业务验收仍待完成。历史752用例只属旧目录；当前753定义均not_run，旧性能/旧Docker不能改绑此hash。

本次用户另修订BPS为显式勾选模型，缺失/空/畸形不表示全选，Cloud优先且保留列表。此要求不来自025945维护输入；新模型选择、默认模板/供应商迁移、安全DTO时间校验与scope真实展示的最终e5与开发验证证据已交接，真实业务验收待完成，f4fe回执不覆盖这些后续改动。

### 安全DTO局部交接（历史作用域）

MAIN确认生产consumer均调用readBPSRecoveryPresentation(account.excel_bps_403_recovery)，旧reader和专属测试删除无shim；本文只检查新reader形状/路径与旧路径删除，不重复全消费者审查。七种state明确保留unknown，reason不推授权；严格时间解析拒绝非法实际日期和24:00。220相关tests通过来自MAIN本轮交接，该局部交接当时未含最终manifest，最终证据现已单列；不能写成本文新跑或旧6005覆盖新source。

### 3.1 历史ff45、5085及5c3005验证

所有命令均使用已安装 Node 和现有依赖，无依赖安装或升级。下列历史回执根为 `frontend/.local/frontend-completion-20261027/`；先前全量 lint、控件类型检查等保留在 frontend-rule-closure，未修改的输入可按实际差异复用，不能将旧回执改绑成新执行。

| 检查 | 实际结果 | 回执及范围 |
| --- | --- | --- |
| 完整 Vitest | 594 文件、5,815 项通过，0 失败/跳过；96.967 秒 | full-tests.json / full-tests-run.json；ff45d068 输入输出一致 |
| 最终四语言/合同增量 | 17 文件、199 项通过；10.788 秒 | final-copy-tests.json；5085f298 检查点，与全套重叠，不相加 |
| 生产 vue-tsc | 退出 0；17.480 秒 | static-checks.json；生产 tsconfig 排除测试 |
| 本轮变更测试类型 | 退出 0；17.039 秒 | static-checks.json；不宣称所有历史测试均已静态检查 |
| 构建配置 tsc | 退出 0；0.611 秒 | static-checks.json |
| 变更文件 ESLint | 退出 0；1.648 秒 | static-checks.json；最后文案另见 bps-contract-handoff.json |
| zh-TW 生成检查 | 退出 0 | static-checks.json；后续资源文案由 worker 生成并核对 |
| 主站 / 供应商 Vite（5085检查点） | 均退出 0；9.777 / 10.042 秒 | build-before-excel-auth/builds.json；输入输出一致，未部署 Docker |
| Excel 主授权和任务交接修复 | 同组测试先42失败/19通过，修复后61/61通过；关联279/279通过 | excel-auth-handoff.json；其中已有被替换的3项旧组件测试，不将61项全记新增 |
| 空scope二次审查修复 | 新增9项先失败，修复后组件70/70、关联288/288通过 | excel-auth-empty-scope-handoff.json；10个受影响测试文件，无跳过 |
| 最终生产/测试类型、lint、生成检查 | 全部退出0；生产类型14.696秒 | excel-auth-empty-scope-validation-summary.json；明确记录其作用域输入摘要 |
| 最终目录及语言检查 | 15文件、105项通过；8.547秒 | final-catalog-locale-tests-run.json；绑定5c3005 |
| 最终主站 / 供应商 Vite | 均退出0；14.688 / 12.621秒 | builds.json；输入输出均5c3005，source_drift为空 |

完整套件运行期间源码没有漂移。全套之后的四语言 BPS/WS 文案、BPS合同测试和验收定义变化由199项检查覆盖，Excel补漏由288项回归和最终105项目录/语言检查覆盖，并重新构建两角色。集合相互重叠，不能合计为通过数量；未宣称最终5c3005再次跑过完整套件。最初角色测试 `.get(...).exists()` 的真实类型错误已修为 `.find(...).exists()`，首次失败记录在 static-first-attempt。Excel的失败前后使用同组回归；本地生成调用首次失败及修正后的成功也保留，未安装依赖。所有历史失败回执保留，不能将开发中失败隐藏为“一次全绿”。

## 4 历史制品与性能边界

最终5c3005按磁盘 UTF-8 字节计量，主站入口 **370,865 bytes**、供应商入口 **370,631 bytes**，AccountsPage **367,532 bytes**，xlsx 独立 chunk **500,005 bytes**。入口静态 JavaScript 依赖合计分别为 **612,245 / 612,011 bytes**；实际首屏还会按页面加载其余资源，不能将这两个数当完整网络流量。此次没有默认大 chunk 警告，也没有调高阈值；仍保留 router/adminSettings 同时静态/动态导入警告。`build-summary.json` 保留最终测量方法、manifest及各制品摘要；入口字节数与5085一致不代表文件内容/摘要相同。

浏览器旧新同条件对照由独立 worker 执行。基线为原始6f，重构计时输入为补漏前5085：隔离Chrome 156、1440×900、en-US、每次新profile/禁用缓存、固定合成API、未压缩静态资源，旧新交替。60个正式样本与6个预热全部有效，未剔除异常值；每版本/场景n=10，p95即该组最大值。完整结果在 `frontend/.local/performance-closure-20261026/compare-final-20261027-03-results.json` 和同名前缀summary/run回执，耗时683.102秒。

| 场景 | 原始→5085 JS响应体 bytes | 变化 | ready median ms 原始→5085 | ready p95 ms 原始→5085 |
| --- | --- | --- | --- | --- |
| 登录 | 1,249,044→1,173,621 | -6.04% | 5,999.65→4,030.55 | 6,025.90→6,042.10 |
| 首页 | 1,264,768→1,203,252 | -4.86% | 5,990.40→5,968.40 | 6,018.90→6,049.20 |
| 账号列表 | 3,283,482→2,153,648 | -34.41% | 6,056.30→5,531.85 | 6,118.00→6,386.50 |
| 创建弹窗（增量） | 0→380,181 | 延后加载 | 46.25→63.50 | 137.30→111.70 |

账号含弹窗总JS为3,283,482→2,533,829 bytes（-22.83%）。延后加载增加首次打开成本和请求数量：登录/首页JS请求14→25，账号49→71。两版都出现约4/6秒的布局与就绪时间簇，来源尚未确认；共享主机负载及小样本限制下，不能宣称稳定延迟改善、生产SLO通过或内存收益。账号ready p95增加4.39%、弹窗median增加37.30%也保留，不只报有利结果。

5085的30个非支付正式样本均无支付SDK请求/执行；原始版30次本地SDK及60次外部尝试均被记录并阻断。两项付款加载检查分别证明缺参不加载SDK，以及合成PENDING解析成功后才加载本地SDK；所有远端请求被阻断，预期隔离/SDK错误另列，不证明实际支付成功。供应商在此组只检查构建指纹。失败01、中断02均保留且不进入统计。最终5c3005仅按实际变化补验加载边界，与这组5085计时独立记录，不将新摘要套到旧样本。

最终5c3005的补验ID为 `post-excel-fix-20261027-01`：页面3/3、支付2/2通过，两个命令均exit0，合计51.80秒。每场景只测一次，用于受影响资源/SDK边界，**不生成最终版median/p95**。实际数据如下：

| 最终场景 | 本地JS请求数 | 未压缩响应体 bytes |
| --- | ---: | ---: |
| 登录启动 | 25 | 1,173,836 |
| 首页启动 | 25 | 1,203,467 |
| 账号列表启动 | 71 | 2,153,863 |
| 创建弹窗增量 | 5 | 380,181 |
| 支付缺参数 | 37 | 1,267,438 |
| 支付合成PENDING | 38 | 1,309,400 |

最终账号含弹窗为76请求、2,534,044 bytes。五个启动场景均较5085增加215 bytes，来自最后文案增量；非支付及缺参场景仍没有加载/执行Airwallex。PENDING响应后才加载本地SDK，9次外部尝试全部阻断，23条预期隔离/加载错误保留；未知API、页面异常、非预期console错误均为0。源码及主站/供应商dist测后指纹稳定；这不是供应商浏览器验收，也未操作真实Excel OAuth。两阶段细节见 `frontend/.local/performance-closure-20261026/final-handoff.json`、`pre-excel-fix-handoff.json` 和 `post-excel-fix-20261027-01-analysis.json`。这一组浏览器验证对752项真实Docker验收的贡献为0。

### 当前验收目录交接（定义更新，未执行）

MAIN原始增删用例回执 `frontend/.local/frontend-acceptance-c2-20261027/catalog-update.json` 对应当时cases.json SHA256 `0c2e81bad955aa74d36c165f8f859d76f397305284aeff07fd71c0df37590fb5`。独立统计为753唯一case、92页面、51设置面板；683 local_e2e、48 external_e2e、19 manual、3 static；P0 461/P1 292，全部not_run。退役默认模板两条用例不再进入当前面板、页面case集合或反向映射；三条新BPS用例分别验证显式选择、安全公共恢复投影及陈旧编辑原子并发。成功测试自动恢复保留BPS与管理员明确恢复覆盖须分开；缺少后端原子合同则陈旧编辑用例blocked，GET预检不替代。该0c2e81目录哈希属于先前增删用例阶段，10-tab后续修订已另有新哈希与回执；两者都不是最终source hash，本资料未运行应用测试或修改catalog。

### Settings 10-tab 后续修订与退休交接

[Arendt退休交接](../../../frontend/.local/bps-explicit-models-20261027/retirement-c2/HANDOFF.md)确认默认模板/API/hook/mode整链移除、保留有效模型解析与账号级设置；MAIN已核验34个现存hash一致、10个文件删除。管理SettingsWorkspace为10个tab，独立bps tab删除；全站BPS gateway开关移至features、图片预算前，仍沿原revision CAS一次保存。供应商门户仍为11个tab，不混改。

MAIN随后修订route-inventory、cases/CASES/FIXTURES的设置tab定义并新增真实tabs漂移回归。当前cases.json SHA256为 `f583bef1ab284f6cb20530917bcf415d6f1b0c5887b9a8eabd1f0f0aa5fa5c98`，route-inventory为 `3388e57dc64ce633335f4916473d24e72043540581c754bdcffde20c0863d390`；原catalog-update回执仍绑定0c2e81ba检查点，不改写旧回执。753用例/92页面/51面板及not_run状态未变。HANDOFF中的13文件390测试及当时acceptanceCatalog 5/5，仅属于该worker检查点。MAIN后续的catalog-tabs-update.json及catalog-tabs-tests.json已记录路由、panel、tab真实覆盖6/6通过，exit0，1.353秒；本资料已读取核对回执，未重跑。新6/6单列，不并入worker的5/5、390或f4fe/6005；753业务定义仍not_run，当前组合source为e5，联合业务验收仍未完成。

## 5 规格和验收范围

历史快照是162项功能、13项共同要求、92页面、52面板和752用例，显式关联355、仅章节关联397；这些数字不用于固定当前目录。当前已按catalog-update.json重建51面板/753用例及正反向关联，功能节数量按实际标题与内容重新统计。旧BPSDefaultsPanel只是面板附录中的一行，并无独立FS功能节；不为维持旧计数保留面板行或旧用例引用。历史6f→025945维护矩阵54路径与c2增量35路径继续分开记录。

当前753条业务定义全部not_run；旧752记录保留为历史，不转换成passed。source、联合镜像及全站业务验收仍待完成；浏览器性能替身、历史Vitest和目录静态校验均不能替代业务执行。

[verification.json](verification.json) 记录文档链接、计数与当前输入；[TEST_REPORT](../TEST_REPORT.md) 和 [前端验收入口](../../../frontend/tests/acceptance/README.md) 管理实际联合验收。未 stage、commit 或 push。当前前端e5已冻结，最终manifest及开发回执已交接，后端与业务验收仍待完成；本次资料修正不代表Docker/页面或整个重构accepted。
