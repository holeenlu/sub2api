# sub2api 前端重构差异复核

当前验证状态：e5源码已冻结；6196完整测试和双build真实输入为63fe，按唯一测试类型补丁的JS/生产等价证明复用；五项static直接绑定e5。d629 lint失败保留为历史，不抹除或改绑。

最终前端源码已停写并冻结为 `e5a6080563419821daef5eb96e29a95b95f8d80c722b2f8af68dca762878277d`（2009文件、18,901,651 bytes）。c2维护35路径的退休、显式模型、metadata与类型修复已应用；完整测试/双构建的真实输入保留63fe，按唯一测试类型补丁的等价证明复用于e5，五项static直接绑定e5。当前catalog为753用例、92页面、51面板、10管理设置tab，业务定义全部not_run；后端及联合Docker/UI/外部验收尚未完成。

复核日期：2026-10-27。原始输入为 `6f48d6ab6fcc29763af84410690530aeaa957403`；当前为 `codex/sub2api-rebuild` 的未提交重构工作区，临时根提交仍是 `673c53c130c11cf1cb55abf0722dd996bf79eded`。不能只用当前 HEAD 代表这些重构代码。

原始比较输入为6f48d6ab6fcc29763af84410690530aeaa957403，维护输入至c2a10f2（远端确认由MAIN完成）。当前最终前端为2009文件、18,901,651 bytes，SHA256 `e5a6080563419821daef5eb96e29a95b95f8d80c722b2f8af68dca762878277d`；精确相对f4fe增9、删11、改62。历史f4fe/5c/5085证据保留各自hash，依据见[最终证据](product-spec/REVIEW-EVIDENCE.md#final-e5-evidence)与[机器核验](product-spec/verification.json)。

## 复核结论

这次重构改变了模块边界、领域所有权、跨领域表单组合、请求生命周期与品牌装配。Vue、TypeScript、Pinia、Vue Router、Vite 及大量有效界面、控件和业务算法继续使用；原始输入与当前的 package.json、pnpm-lock.yaml 没有差异。它没有承诺重新设计所有页面视觉，也没有改变全部服务端 API。

最明显的结构变化是从根目录的 api/stores/types/views/components 等横向集中组织，转为 app 负责装配、14 个 feature 负责业务、shared 负责技术机制。当前这些被替代的根桶没有源文件或转发壳；原本已存在的供应商、支付等局部 feature 也被纳入统一边界，而非声称旧项目完全没有领域组织。

本轮记录 frontend 已有开发验证和未闭环接口依赖；整个项目仍为 `in_progress`，不代 MAIN 更新工作包，也不将局部完成作为整个项目停工或验收完成的依据。历史检查点不代表当前源码收口或全站验收。历史 [TEST_REPORT](TEST_REPORT.md) 已记录旧阶段UI镜像部署及TLS前置验证；这些记录不认证当前e5源码，但后端尚未冻结，运行 API 仍是旧阶段镜像；当前753项前端业务验收定义均为not_run，已按MAIN目录回执重建追踪。

此前“没有待接线的前端实现项”的结论过强，已纠正。本版完成了下列已确认缺口的源码修复；实际 Docker 镜像与逐页验收由主集成会话串行执行，尚不据此关闭整项重构验收。

## 复核发现与本版处置

| 编号 | 原问题或更正 | 本版实现及验证边界 |
| --- | --- | --- |
| FR-01 | 生产 Router 内联供应商判断，与守卫工厂重复 | [实际 Router](../../frontend/src/app/router/index.ts) 直接消费 [createVendorPortalGuard](../../frontend/src/app/compositions/vendor-portal/guard.ts)，删除重复分支；区分 sessionEpoch 与 policy/scope，设置和供应商查询 await 后复核。新增实际 Router 导航回归，不只调用工厂替身；部署导航验收待执行。 |
| FR-02 | 注释引用退役 utils/views/types 路径 | [featureFlags](../../frontend/src/app/navigation/featureFlags.ts) 按实际领域 owner 更新示例；没有借修注释保留旧目录转发壳。 |
| FR-03 | 大 SFC、弱类型与大 chunk | 账号编辑器改为打开时懒加载；批量 patch 为纯模型，图片缓存/缩略图/预览请求各有明确寿命。历史5c3005统计中的生产 TS/Vue 脚本显式 any 为 0；仍有大型模板、未执行的真实页面验收，不能据此宣称全部类型或性能问题消失。 |
| FR-04 | **撤回旧误报**：曾称供应商角色目录粗粒度错误 | 原 route-inventory 的哈希 `6bf9f639e8eb7f3547326546a3efb040848cf7e138b7b859487251308396f2bf` 已包含正确角色：platform 仅超管、profile 三角色、children/child-*/payables 仅一级供应商。本版补角色与 case 关联检查及源码指针，未把原本正确的角色值说成修复。 |
| FR-05 | 优先级快捷编辑卸载 flush，缺少 actor/字段权限边界 | [单元格](../../frontend/src/features/resources/components/AccountPriorityCell.vue) 接收实时 scope/canEdit，父页同时检查 accounts.manage 与 account.update.priority；捕获账号/原值/意图并贯通 signal。卸载、换 scope/账号、撤权取消，不 flush；step-up 取消与 ERR_CANCELED 静默恢复草稿，真实失败仍反馈。 |
| FR-06 | 退款动作与服务端状态/类型条件不一致 | [唯一资格模型](../../frontend/src/features/payments/model/refundEligibility.ts)：用户仅 balance + COMPLETED + 合格 Provider；管理仅 COMPLETED/REFUND_REQUESTED/REFUND_FAILED，REFUND_PENDING 只查，PARTIALLY_REFUNDED 不重退；首次部分金额退款保留。显示、打开、提交均复核；不放宽后端权限。 |
| QA-PRE-001 | 首页常驻介绍在目录关闭时仍请求而报 404 | 保留首页模型介绍和官方参考链接；目录明确区分 unknown/disabled/authentication_required/available，设置就绪且允许才发请求。scope/配置变更取消旧读取，重试 401/403/404 前刷新公共设置；不伪造模型卡或报价。部署页面复测待执行。 |
| FR-07 | Excel 主授权提示遮蔽缺失事实；多阶段授权任务没有逐次复核权限，未知状态也开放独立授权操作 | [ExcelBPSAuthorization](../../frontend/src/features/resources/components/ExcelBPSAuthorization.vue) 优先呈现 authorized=false，主授权回到账号 OAuth 管理；未知状态不开放写入。每个异步交接复核 scope/权限/目标；空会话立即失效并清秘密草稿。JSON 对象校验后沿既有服务端验证。70 项组件与 288 项受影响回归通过；真实 OAuth 验收仍待条件。 |

此表是代码复核结论，不是另一份工作包状态；实施与整站验收状态仍由 [work-items](work-items.json) 和 [TEST_REPORT](TEST_REPORT.md) 管理。

## 持续维护同步

上一轮已复核维护矩阵固定到本地025945；本轮新增c2a10f2的35条路径只完成语义映射，待Arendt/MAIN及tooltip-info worker应用交接。原始 6f→ec 的 38 路径、6f→b22 的 46 路径是历史批次，历史54路径为当时端点差异去重后的集合，不能把重复路径相加。逐路径映射记录在 [源码对照](product-spec/source-review.json)，适配至现有 app/features/shared，不恢复旧目录。

- Ops：紧凑总览、集群节点身份/未知与不可用/30 秒老化、完整诊断分页导出；倒计时到零立即刷新，失败也重置间隔；设置仅保存后更新，Header 按刷新完成的 lastUpdated 更新摘要。页面刷新与告警评估周期分开。
- 账号：管理员和供应商使用 app 组合层同一元数据组件；订阅计划按平台解释，展示授权方式、计费类型和 compact 状态；authorization_modes/auth_mode 只消费后端投影，不变为授权判定。
- Group：image_count_billing 归 pricing 草稿/面板，OpenAI/Composite 且允许图片时可设置；默认 false，关闭图片/不适用平台清除，仍由一次 Group 命令与字段权限控制。
- 供应商：已授权 Composite 组可选择；保留真实脱敏 error_message；超管以实际 owner 的 management 视图分配原生账号组，不扩大平台或父级授权。
- 身份：主动 refresh 复核 session generation、用户、原内存 refresh token 和持久会话快照；同 session 轮换只更新内存/计时器，退出立即停止定时器。渠道状态页 sticky offset 同步维护输入。
- 品牌：供应商静态首屏通过同源 `/api/v1/settings/favicon`；Vite 与运行时共用图片引用校验，拒绝反斜杠、内部 CR/LF/TAB、userinfo 和无 host HTTP(S)。data:image 引用与端点解码分层。主站侧栏版本徽标仅超管显示。
- 角色设置：按类别呈现部分 CRUD 授权，提交前明确复核修改与原因；冲突先重新比较，保留无关权限；离开确认绑定当前会话。维护版新增布局和对应设置消费者均有回归。
- BPS：安全DTO消费、内部scope展示、BPSFailureBadge、明确停止意图、隐藏配置保留和运行态过滤均已进入固定e5前端开发快照；旧私有failure读路径已删除。前端开发验证按最终证据成立，供应商安全DTO、后端stale-edit原子合同及联合runtime业务验收仍未完成。

## 本轮 BPS 实现与接口依赖

Arendt的显式选择/默认模板整链退休已停写交接，MAIN核验34个现存文件hash和10个删除；SettingsWorkspace由11降至10个tab，独立bps tab删除，全站BPS gateway开关移至features并沿原revision CAS保存。此为局部实现交接，不冻结整体source或合并计算历史测试。

本次用户新增显式模型要求（后于c2a10f2的用户修订，非维护同步）：BPS只匹配最终账号映射后的明确勾选模型；缺失、空、null或畸形列表均不表示全选，不自动替历史账号补全。Cloud开启时仍优先并保留已保存BPS列表，关闭Cloud后按该列表恢复选路；具备原生授权的独立新请求可走Native，Excel-only不因此获得Native权限。按主代理交接，Arendt已接模型选择全链、默认模板退休及供应商迁移；主代理已完成安全DTO时间校验与scope展示修复。这些改动已进入最终e5，开发证据按63fe真实输入和类型补丁证明复用，不沿用f4fe计数认证。

- [accountRateLimitScopes](../../frontend/src/features/resources/model/accountRateLimitScopes.ts) 的当前验收要求是区分BPS/Native限制，展示可信实际模型、接口、交集以及账号429/模型权限限制；未知编码仅显示范围待确认。成功测试后的自动恢复保留BPS范围冷却，管理员明确恢复覆盖按独立权限和实际端点语义核对。对应 FE-RES-BPS-PUBLIC-RECOVERY，仍not_run。
- [bpsRecoveryPresentation](../../frontend/src/features/resources/model/bpsRecoveryPresentation.ts) 消费公开 `account.excel_bps_403_recovery`，旧reader和专属测试已删除，无shim。七种state保留unknown；不读私有extra.failure、不由reason推授权。严格日期与字符规则在最终开发验证中覆盖；早期220项局部测试不另加到6196。供应商安全投影与真实API/业务验收仍单独待完成。
- [BPSFailureBadge](../../frontend/src/features/resources/components/BPSFailureBadge.vue) 在f4fe检查点替代并删除 ExcelBPS403Badge.vue 及旧测试路径，panels/AccountsPage 与测试接新名。该检查点测试覆盖新record优先、未知不回落旧403、恢复/实际开启不显示停用、授权缺失或未知不显示probe时间，以及到期不当恢复；最终展示须消费安全DTO后重新验证。仍符合当前组归属的legacy403历史分组动作独立保留。
- 编辑器在f4fe检查点使用 `excelBPSSystemDisabled` 保留模式、模型及隐藏配置，提交过滤failure与五个403运行态标记；这不代表故障状态的真实API读取已接通。“保持关闭并停止本次自动恢复”仅建立未保存表单意图，保存才提交现有 `extra.openai_excel_bps=false`；普通disabled账号改名不产生该false，关闭、换目标、权限/scope/role变化重置意图。按钮点击或保存成功均不能替代服务端旧claim失效证明。
- 安全DTO生产迁移及220项相关测试已由MAIN交接，前端snapshot已冻结为e5，整体重构尚未验收。supplier安全投影仍缺；成功测试自动恢复与管理员明确覆盖必须分开验收；陈旧enabled草稿、明确停止及旧probe竞态要求后端原子合同，缺少时blocked，GET预检不能替代。三条新业务用例均not_run，不能将目录修订或局部单测写成并发缺口闭环。详见[设置与合同](product-spec/SETTINGS_AND_CONTRACTS.md#5-服务端依赖边界)。

当前catalog新增 `FE-RES-BPS-EXPLICIT-MODELS`、`FE-RES-BPS-PUBLIC-RECOVERY`、`FE-RES-BPS-STALE-EDIT`，删除退役默认模板面板两条用例；当前753项全部not_run。本轮仅更新规格/追踪，没有新增Docker/CUA业务验收。

## 保持不变的合同

- 保留主要页面 URL、既有深链、OAuth 与支付回调和有效供应商入口；路由兼容只保留实际入口映射，不保留两套页面实现。完整清单见规格书页面附录。
- 服务端继续决定身份、页面/操作/字段授权、对象归属、模型准入、价格、费用和账务状态；浏览器没有另建调度器或统一六维能力推断。
- 设置 GET/PUT 的 revision 条件写入在原始基线已经存在。重构加强前端草稿/冲突处理，没有把它冒称为新建的后端 CAS 能力，也没有增加通用 revision API。
- 有效 Native、BPS、Cloud、各平台配置与授权字段继续消费现有合同。页面拆分不等于协议执行内核已经完成，也不证明真实上游操作通过。
- 网站品牌仍使用现有公开设置投影。技术项目、部署资源名与网站展示名保持不同责任，不把网站 API 链接用作管理令牌发送目标。

## 已确认的优化

| 方面 | 原始组织或风险 | 当前落点与变化 | 实际收益与边界 |
| --- | --- | --- | --- |
| 领域所有权 | 根 api、stores、types、views 和 components 按技术类别聚集，已有局部 feature 尚未统一 | app 装配；identity、keys、resources、access-policy、catalog、pricing、subscriptions、payments、usage、operations、jobs、supply、site、system 各自拥有 API/DTO/状态/页面/语言贡献 | 减少跨域实现依赖；业务 DTO 没有集中搬到 shared/types |
| 设置工作区 | SettingsView.vue 11,874 行承载多个领域与独立控制 | SettingsWorkspace、transaction、领域投影与设置面板；普通 admin 使用独立委托设置视图 | 聚合草稿一次 revision PUT，委派页面同样支持冲突比较/合并；保存后的公共/私有投影刷新失败单独报告，保留新草稿且不重放 PUT。独立 SMTP、Provider、备份等命令不冒充跨端点事务 |
| Group 编辑 | GroupsView.vue 6,902 行混合列表、准入、定价和订阅 | 各领域提供面板/草稿/payload；app 组合一次 Group 命令 | app 中后续 552 行混合 groupDraft 已进一步缩为 34 行组合；这组 552→34 是重构中间阶段对照，不是原始基线大小 |
| Account 编辑 | CreateAccountModal 7,498 行、EditAccountModal 6,528 行 | 63fe实测并复用于e5的组件 2,225 / 2,115 行，职责模型位于 resources/model，旧路径删除 | 领域、草稿、授权、提交与遥测责任已分开；组件本身仍较大 |
| HTTP 设施 | 通用 API 客户端与应用认证/恢复副作用交织 | shared/http 提供传输机制，app/http 注入认证恢复及合规回调；bootstrap 拥有安装/释放 | 匿名请求不携带私有会话；单次受控恢复，避免换身份重发旧命令 |
| 命令与查询 | 仅检查当前组件或当前目标不足以阻止异步晚到和 step-up 续接 | 捕获 actor/session、目标、payload、请求代际与 signal；取消贯通到 API/HTTP | 旧结果不能写入新目标；Abort 不代表服务器回滚，未知结果保留而非自动重发 |
| 幂等状态 | 旧响应确认可能清掉同载荷的新 key；复制意图可能缺少 actor 归属 | 复制与批量订阅确认核对本次 key/actor/来源；不清除他人的新意图 | 不把本地取消当作未执行；已有不明结果保持可追溯 |
| 导航与语言 | 导航与路由信息分散；业务文案集中在语言根桶 | route contribution 为菜单/标题等提供同源元数据；14 域语言公开贡献由 app 装配 | 保留四语言和既有键值；按语言动态加载，检测重复归属和缺键 |
| 品牌与构建 | 历史品牌默认值、下载工具名及后端嵌入式输出位置耦合 | 中性 sub2api 标识、可配置展示品牌；Vite 输出前端目录；主站/vendor 按角色构建 | 去除无消费者推广和旧升级器链；许可证、来源和有效历史备份读取保留 |
| 数值展示 | 缺失 token 可能被呈现为已知零，完整总量与部分未知混淆 | 区分未知、零、实测下界和结算状态；支付终态停止进入 SDK | 不改变服务端账务权威、TPS 原始 duration 口径，也不把 UI 状态当付款事实 |
| 共享基础件 | 表格、错误处理、焦点与表单机制混有业务细节或清理缺口 | 泛型 DataTable、技术草稿克隆/patch、可取消表格加载、域内支付错误本地化、顶层 Dialog 焦点管理 | 降低重复机制；radio/跨 form/teleported Select 有隔离 Chrome 键盘复核 |
| 启动与退出 | 启动失败、重复挂载可能遗留 Router/预加载/服务/状态 | bootstrap 释放路由预热、HTTP 配置、feature scope 和 Pinia；应用壳、公告铃/弹窗与合规弹窗按实际需要懒加载 | 真实 Vue/Router 回归覆盖匿名、管理员、供应商、退出后迟到加载；支付 SDK 不属于入口静态依赖 |
| 叠加弹窗 | 公告与通用 Dialog 独立改写 body overflow，关闭其中一个可能提前恢复滚动 | 三个真实消费者共用具名滚动锁令牌；最后一个释放时恢复原 overflow 各轴、优先级和 modal class | 多层打开、任意关闭顺序、卸载、预览/非预览均有组件回归；浏览器交互结果单列 |

## 仍需明确保留的限制

- 大型 SFC 仍存在：63fe实测并复用于e5：BatchImageJobsPage 2,409行、BulkEditAccountModal 1,953行、AccountsPage 2,583行；AccountWorkbench为1,857行。预览缓存/解码/请求、批量 patch 和独立编辑面板已有明确 owner，模板继续复用有效交互。行数不直接证明错误，也不代表所有页面均已细粒度重写。
- 63fe按同一AST口径实测并凭生产等价复用于e5，生产 TS/TSX/Vue 脚本内显式 any 从原始 352 处降至 0；65 是上个检查点。统计不计测试、Vue template、隐式 any，也不证明运行时数据已全部校验，不能表述为“所有弱类型已清零”。见 [技术附录](product-spec/REVIEW-EVIDENCE.md)。
- 当前两角色build exit0，真实输入63fe，按作用域复用于e5。入口JS主站370,376/供应商370,142 bytes，静态依赖611,885/611,651，AccountsPage 369,886，xlsx 500,005；router/adminSettings混合导入提示仍在，无默认大chunk提示。磁盘字节不是浏览器流量或性能。
- 已建立隔离 Chrome、固定合成 API 和交替顺序的旧新对照：60 正式样本、6 预热全部有效，严格绑定补漏前的 5085 制品。登录/首页/账号首屏未压缩 JS 分别减少约6.04%/4.86%/34.41%，但账号 ready p95 增加4.39%，首次创建弹窗 median 增加37.30%。共享主机、4/6秒布局时间簇及小样本限制明确，不能承诺稳定提速。历史5c3005的受影响加载补验与这组计时分开记录，见技术证据。
- 后端新的统一能力 DTO、全领域配置版本等并非当前浏览器已消费的普遍接口；规格书分别记录现有合同和服务端依赖，不将目标设计写成已实现功能。

## 验证依据

最终目录 `frontend/.local/frontend-final-c2-fixed-20261027/` 的HANDOFF/integration-receipt确认e5、2009文件、18,901,651 bytes。完整Vitest为600文件6196/6196、0 failed/pending/todo，146.408秒；主站/供应商build均exit0，16.094/11.936秒。完整测试/双build真实输入输出均为63fe，无漂移。

63fe→e5仅OpsNodeMetrics.spec.ts的type-only import与OpsJobHeartbeat[]标注；test-type-fix实测JS一致，production-reuse-proof逐个生产输入相等。后续该测试9/9、五项static全部exit0直接绑定e5，因此按实际作用域复用完整测试/双build，不改写原回执。catalog 6项已包含在6196中，补验及worker计数不相加。本文只核对证据，未运行测试或构建。

当前753业务定义、92页面、51面板、10管理设置tab仍未转为业务passed；电脑工具仅预检旧90c927本机web-build，临时tab关闭、无业务步骤，未重测当前浏览器性能。供应商安全DTO、stale-enabled后端原子合同、后端整体及最终Docker/全站/外部验收仍未闭环。完整边界见[技术附录](product-spec/REVIEW-EVIDENCE.md#final-e5-evidence)。

## 交付文档

全站功能规格见 [product-spec](product-spec/README.md)，可重查数据见 [技术证据](product-spec/REVIEW-EVIDENCE.md) 与 [源码统计](product-spec/source-review.json)。本版同步了维护功能、修复已确认的前端缺口并更新规格；资料完成不等于整个重构或线上验收完成。
