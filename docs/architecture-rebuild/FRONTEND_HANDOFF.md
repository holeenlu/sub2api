# 前端会话交接边界

日期：2026-10-25。前后端统一分支 codex/sub2api-rebuild；前端由用户另建会话在同一工作树继续开发，以明确写集和证据交接；按最新指令，全部开发与本机验收完成后再整体归并提交。此文件定义交接事实，不代表未验收工作已经完成。

## 当前可用检查点

2026-10-27需求修订（待后端实现与联调）：用户取消同一BPS执行中的原生回退，保留403自动关闭BPS开关及条件自动恢复；停用后的独立新请求可按更新配置使用Native。前端保留现有自动停用/恢复设置和真实开关状态展示，不将其替换成仅临时熔断或新增平行路由意图字段。旧续接/工具回复不能因开关变化自动迁移。合同见[CT-02 BPS路由修订](CONTRACTS.md#bps-routing-20261027)，后端任务WP-11/WP-18，验收TC-EXEC-02/TC-SESSION-04；本轮未改变API/DTO、未修改frontend实现。

同日用户补充账号故障处理，见[CT-02故障分类与恢复](CONTRACTS.md#bps-failure-recovery-20261027)：确认账号403按现有配置关闭BPS并定时恢复；Excel授权有限刷新后确认失效则自动停用，缺失/撤销显示需重新授权而非自动恢复倒计时；429仅展示实际scope及冷却到期，不显示成整个账号被封；请求不支持/池耗尽不关闭开关。恢复任务只恢复仍匹配的系统停用记录，管理员在已自动关闭后再次明确关闭也须阻止旧probe。前端状态与原因/可选下次探测时间以服务端投影为准，不自行推断恢复成功、不自动开启Cloud；新字段/API尚待后端共同schema确定，本段是需求交接而非接口已上线。

2026-10-26用户在MAIN会话明确要求“包括前端我也有在维护、修改，你也要让前端那边同步更新”。MAIN已获人类授权向前端会话01a171ab-53ee-7772-a290-346a110036cd发出继续开发指令：从实际采用基线到最新main逐项吸收有效维护改动，含新的运维布局、集群节点与权限相关合同，适配app/features/shared真实消费者，同步FR-01/04/05/06及QA-PRE-001。最新只读核验origin/main与本地main均cd1d64f8f29dc8837168e8796ab108f8a22d21e8；重构远端仍4e2074d66d1260ea233ffc7372e87c6b4fa09fd1，未推送。旧1948文件冻结只保留为历史证据；后续由前端owner交精确文件集/新hash与验证，MAIN更新受影响Docker并实际逐页验收。共同不提交规则不变。

前端规格文档已接收为文档交接：13文件743275字节，包SHA256为9bccc3c5e86367dcb4248db43ce79f7bbb789633713ee38f0a65960a51bfe81e，MAIN独立重算一致；verification中12条逐文件摘要也核对一致。根README已链接FRONTEND_REVIEW及product-spec/README。文档完成不关闭所列产品缺陷或提升frontend accepted。

2026-10-26前端会话只读复核交接：FR-01（portal guard factory未接实际router）保持open；FR-04原被误报为vendor角色元数据过宽，后续核对同一冻结hash原值正确，已撤回，不能记作本轮修复，前端源码冻结不能解释为accepted。MAIN已核对实际inline guard与factory差异。后续前端owner须删除被替代inline规则、接入factory并覆盖真实router的异步登出/切换session、super_admin/vendor1/vendor2正负例；保留原正确角色值并补实际Router关联验证，禁止将权限重定向计为目标页操作通过。MAIN不修改frontend/**。 后续FR-05优先级快捷编辑卸载flush未统一actor/字段gate、FR-06用户与管理端退款按钮条件和后端状态合同不一致，已由前端会话记录在FRONTEND_REVIEW；当前属于交互/合同缺口，不据此推断实际越权。该会话随后已纠正文档草稿和链接，其先前“只读环境阻塞”自述已撤回，不能当持续阻塞；以最终文件manifest和校验交接审阅，Git仍由MAIN单写。

API已更新至sub2api:rebuild-api-162a697a6711，镜像ID sha256:34f9491e44bce58006425fe7bbf3e55f4c66a7e94a0d75df527186ef895abd00；源工作区hash 162a697a671183de187ef23ae7a3652e0dc1524381b8fe067070840849f864a5。地址127.0.0.1:32180、原测试账号/组和凭据文件不变。分组访问规则与模型白名单/别名解释已迁入access/catalog；外部API、DTO和已有错误格式保持，主机运行复验通过。下方旧镜像/阶段提交仅是历史记录，以本节与environment.json为准。

目前仍等待前端owner给出稳定源码检查点，实际web/vendor构建继续暂停；32181/32182没有真实应用页面验收结果。本轮没有提交或推送，Git索引仍由主集成者单写；最终需前后端全部开发与逐页操作验收通过后一次共同归并。

## 写入和集成

- 前端会话独占 frontend/** 的领域目录、路由、共享组件、类型及品牌读取调整；本后端任务不修改它们。
- backend/**、根Dockerfile与deploy/api/**、根构建和共享合同由本任务整合。跨界变动先给出精确接口/构建产物影响，由各自owner落地。
- 早期隔离工作树的已验证提交已经整合；当前共同工作树按唯一写集协作，不覆盖另一owner的文件，不向main自动合入。
- 本分支后端可以单独构建、运行；完整站点由前端会话产物接入后验收。WP-21的external状态不意味着前端已经完成。

## 当前对外合同

并行文档写集交接（2026-10-26）：前端会话独占新增FRONTEND_REVIEW.md与product-spec/**，只核对源码/资料并编写规格，不改冻结frontend业务和测试。根README、work-items、共享交付规范及Git继续由后端集成者单写。已读取该会话最新人类请求，内容为复核差异和整理规格；当前共同工作树AGENTS/CHANGE_DELIVERY仍明确整体验收后统一根提交，未核到解除本会话明确提交限制的人类指令。本轮不stage/commit/push，也不把其他未验收源码顺带收取；新文档完成后按实际文件/hash/链接交接，不据转述擅改交付节奏。

2026-10-26开发期电脑预检发现QA-PRE-001：独立Chrome配置进入32181/home，模型区显示加载失败。已核实同一运行环境公开设置model_plaza_enabled=false、require_auth=false，GET /api/v1/model-plaza按当前后端合同返回404 Model plaza is not enabled；当前HomePage不受该开关控制地挂载ModelShowcase，其query仍调用该API。前端注释又明确模型区常驻，故不能简单把整区隐藏作为默认修复，也不能为去掉报错放宽目录权限或伪造报价。

由前端owner结合既有产品合同统一处理“首页展示”与“完整模型广场访问”的关系：禁用/需登录时不把预期拒绝当网络故障反复请求；若保留参考内容须明确其参考来源/核对日期，不能冒充当前渠道价。若需要后端公开投影，先确认只含被允许的公开模型/分组/费率，不能绕过private/exclusive或个人scope。对应FE-PAGE-HOME-ACTION-2，在最终API/UI镜像上实际复测。当前只记录handoff，未修改冻结frontend源码，也未把预检计为完整页面通过。

| 边界 | 本轮合同 |
| --- | --- |
| 管理、用户、供应商API | 保持现有路由、JSON、鉴权及错误格式；设置页已有revision条件写入，详见下方核实合同，不新增通用revision API |
| 网关 | HTTP/SSE/WS已有请求、终态和取消合同保持；内部包迁移不改变URL或response字段 |
| 网站品牌 | 继续使用site_name、site_logo、site_subtitle、api_base_url、doc_url、contact_info及现有公开设置投影；后端空白安装fallback中性化 |
| API链接 | api_base_url是用户展示的网关地址，不是携带管理令牌的浏览器请求baseURL；浏览器管理API地址沿用已有配置 |
| 更新 | 独立开发制品不应下载维护品牌的二进制；前端应显示后端返回的更新状态，不从展示品牌推导可信更新源 |
| 构建交付 | 后端API制品独立于Node和frontend/dist；完整UI角色与最终资源路径由集成阶段确认，不能把缺少UI误报为全站可用 |

现有代码锚点：[公开设置DTO](../../backend/internal/handler/dto/settings.go)、[API客户端](../../frontend/src/shared/http/)、[前端品牌入口](../../frontend/src/shared/config/project.ts)、[目标CT-11](CONTRACTS.md)。本轮没有新增面向前端的API字段或路由。

## 后续整合验收

核对公开设置、登录和刷新token、主站/供应商入口、网关地址展示及独立品牌首屏；确认API请求目标与品牌链接分离。若分离部署，先采用同源反向代理复用当前身份/跨域合同；不凭本文件推导任意跨域许可。前端真实构建/浏览器结果由对应会话提供，未提供时保持not_run。

## 2026-10-25 删除旧自更新后的接口变化

用户明确要求新项目不保留无用旧实现，因此旧自更新/回滚写链及专属缓存/socket/发布通道已移除。本节覆盖先前“所有管理路由不变”的第一波说明。前端合入前必须同步删除以下调用和无效按钮/状态/文案，不能仅隐藏界面留下旧调用：

| 接口 | 当前状态 |
| --- | --- |
| POST /api/v1/admin/system/update | 已删除路由、处理器、安装实现及权限目录项 |
| POST /api/v1/admin/system/rollback | 已删除路由、处理器、恢复实现及权限目录项 |
| GET /api/v1/admin/system/rollback-versions | 已删除路由、处理器、版本列表及权限目录项 |
| GET /api/v1/admin/system/version | 保留version、upstream_version、build_commit；升级器专属响应头已删除 |
| GET /api/v1/admin/system/check-updates | 已删除；前端已改为仅getVersion，无活跃消费者 |
| POST /api/v1/admin/system/restart | 保留独立运维用途、原授权、锁及幂等要求 |

更新方式是从本独立分支构建制品并重新部署。后端不提供隐藏的下载/安装开关，UPDATE_CHECK_ENABLED已删除。保留实际被Claude/Codex版本同步和价格数据读取使用的GitHub客户端及代理配置；它们不安装本项目二进制。前端代码未由本任务修改，相关删除验收由前端会话完成后再整合。

旧根assets/logo.svg、wordmark.svg及两张partners/logos推广图片已确认无消费者并删除；没有修改frontend自身Logo或资源。旧vendor-local品牌网络覆盖也退出，同机API和门户使用同一Compose项目default网络；新版门户UI和品牌资源仍由前端会话完成。

## 前端会话反馈（2026-10-25，外部提供）

前端会话告知：用户已要求双方统一在codex/sub2api-rebuild按普通增量提交协作；其实现暂存在隔离工作树/Users/luhonglin/.codex/worktrees/b889/sub2api的detached 6f48基线，不应自动收集其中未提交文件。该会话报告327项定向测试、类型检查、变更lint及主站/门户构建通过，浏览器仅验证合成设置首屏；本后端会话未独立重跑这些检查，尚未将该前端实现合入。

后端完成当前提交后在此登记可集成HEAD和写集边界；前端会话自行核对并仅集成frontend及其实施记录。后端清理已删除三个旧系统更新/回滚接口；门户旧updater代理块随同退休，前端需移除旧socket/updater调用与控件。网站公共设置、认证、网关协议合同本轮保持。维护三品牌分支继续独立。

独立项目启动默认改为/etc/sub2api配置目录、sub2api数据库名和WebAuthn显示名；显式配置优先，不变更RP ID或已保存数据，不再自动发现旧/etc/kdan。

## 已整合前端检查点

后端第一波代码提交：5aaf015b6d62864ee0fe3279967a579261cab51f。前端原始提交737fd177d30679a573995f6ad9b9314467ccb3f4已审查并cherry-pick为18853316238485e373a1113feee782549169bd03；frontend树与已验证d786178e800e85b0dd7e3d36540e25625bf17d89完全相同，没有收取隔离工作树的未提交文件。

当前前端仅消费GET /api/v1/admin/system/version。因此进一步删除无消费者GET /api/v1/admin/system/check-updates、安装状态字段及X-Portal-Backend-Version专属升级器响应头；BuildStatus仅保存三个当前版本值，不留旧更新占位接口。独立restart接口保留。前后端共同分支已有第一波实现，完整领域迁移与真实UI登录联调仍未完成。

## 共同分支持续开发检查点（2026-10-25）

第一波已核验代码HEAD：2d32cd0ea6c7c3defdaf866ef8a58701ee4adc93。前后端是同一个完整项目，当前均在本工作树的codex/sub2api-rebuild继续开发；此前detached前端工作树仅用于隔离，已验证提交已整合。首批成果不构成整体方案完成。

工作包已从原始前端规划72dc16b恢复WP-30至WP-35，并恢复WP-21对全部领域切片的集成依赖。WP-29由前端会话继续实施；其只写frontend/**，后端集成者唯一操作Git索引、提交和推送。未交接、未验证的前端新文件不进入后端阶段提交。完整验收仍包含身份/Key、资源/准入、目录/定价/订阅、支付/用量、观测/作业、供应关系/结算、网站/系统及最终装配。

远端第一波检查点已推送并通过git ls-remote核对：origin/codex/sub2api-rebuild = 2d32cd0ea6c7c3defdaf866ef8a58701ee4adc93。新的identity/keys及shared/http改动正在共同工作树内实施，尚未自动纳入提交；准确文件集/回执由前端owner交接。

旧用户兑换码附带残留已接手：删除Server-Timing中/api/v1/redeem及子路径白名单，改为负例；通用audit action测试改用仍注册的accounts/batch-refresh。Claude reset-credits/redeem独立路由及保护链保持，旧RedeemCode数据仍服务邀请和余额历史读取，不按同名关键词删除。定向结果见IMPLEMENTATION；前端正在开发的HTTP/identity/keys仍待owner验证交接。

第二批后端接线及兑换码timing残留清理已验收：675项唯一测试/子场景、目标核心race、depguard/vet及独立API编译通过。当前API字段/网站设置合同保持；本次提交不包含前端owner正在开发的未验证文件。完整项目仍在领域迁移阶段，不标记implementation_ready或rollout_ready。

## WP-20 / CT-11 当前真实控制面合同（2026-10-25核实）

纠正先前交接中“没有revision”的表述：此能力已存在于实施基线6f48d6a，并非本轮新增。WP-20全领域迁移尚未完成，前端可以消费下列现有合同，不能伪造新字段。

### 设置保存

- GET /api/v1/admin/settings 返回现有设置DTO及64字符的revision。PUT同一路径接受revision加实际修改的现有JSON字段；不是PATCH路由，也不使用If-Match。省略字段通过sentFields/OmittedSettingKeys保留数据库值。false、0、空值和secret清空仍遵守各字段原有规则，不统一转换为null或默认值。
- 设置页继续一次PUT原子保存：system settings、auth source defaults，以及预校验后的payment/Fast值汇总后交同一次CompareAndSetMultiple。现有PG实现事务内锁settings表、复核revision、批量写入；验证或revision失败不产生部分设置写入。不要把拆分面板变成多个不可回滚请求。
- revision缺失/长度不是64：HTTP428，body code=428及message；当前handler的该分支没有reason字段，不能依赖其一定存在。旧revision：HTTP409，reason=SETTINGS_REVISION_CONFLICT，message提示重新读取。返回统一code/message/可选reason/metadata/data封套。成功返回完整更新后的设置及新revision。
- 前端保持一份设置草稿与读取revision，收集脏字段一次提交；409保留草稿并重新读取/对比，不自动用新revision盲重放旧修改。新增六维DTO或统一领域revision接口仍需独立合同，当前没有这种API。

依据：backend/internal/server/routes/admin.go的settings GET/PUT；handler/admin/setting_handler_update.go的UpdateSettingsRequest、sentFields和UpdateSettings；service/setting_update.go；repository/setting_repo.go的CompareAndSetMultiple；handler/admin/setting_handler_auth_source_defaults_test.go的缺失/过期revision、非法payment及原子验证用例。这里的原子承诺限已有设置聚合，不扩大到任意账号/分组/支付操作。

### 六个维度的当前表达

| 设计维度 | 当前真实字段/事实 | 前端边界 |
| --- | --- | --- |
| 客户端协议 | 现有协议入口及页面选项，usage的inbound_endpoint/request_type等既有投影 | 没有新增client_protocol通用DTO，不从账号platform推定所有入口 |
| Provider平台 | 管理Account DTO的platform及现有Group平台 | 继续既有枚举/校验，不将OAuth或WS当平台 |
| 凭据/授权 | Account.type、已脱敏credentials及credentials_status；供应商DTO的method、authorization_modes、authorizations | 供应商与管理DTO不同，不能互相强转或假造credential/grant字段 |
| 执行模式 | 现有extra.openai_cloud、extra.openai_excel_bps及各模式已有设置/授权状态 | 配置不等于资格；Cloud/BPS优先级和Excel-only边界仍由后端规则决定，不在浏览器复制调度器 |
| 传输 | 现有extra.openai_oauth_responses_websockets_v2_mode、openai_apikey_responses_websockets_v2_mode等有效设置 | openai_ws_mode是既有入口/观测事实，不证明上游原生WS；无新增transport支持列表DTO |
| 操作能力 | 现有组开关、模型/manifest字段及其实际模式约束；服务端现有校验 | 没有统一六维CapabilityProfile DTO；缺失/未知不能填true、推断unsupported或伪造capability矩阵 |

资源编辑器、Group/Settings组合继续消费现有已校验字段；同一组合命令保持原子保存边界。通用能力快照/领域控制DTO仍在WP-07/20规划范围，本文不把它们标为已落地。

## 本机Docker真实API联调环境

- 当前API运行未提交授权与目录切片：镜像sub2api:rebuild-api-162a697a6711，ID sha256:34f9491e44bce58006425fe7bbf3e55f4c66a7e94a0d75df527186ef895abd00；工作区源码hash 162a697a671183de187ef23ae7a3652e0dc1524381b8fe067070840849f864a5，Git仍为26cf4313bbd083a50a73645fc9f734f8f9d5e84e。此次更新未新增提交或推送，现有API合同不变。
- API：http://127.0.0.1:32180；Compose项目sub2api-rebuild-ui-20261025-4e2074d6，api/postgres/redis均为本次新建隔离资源。数据库/Redis未映射宿主端口，没有读取生产数据或使用上游凭据。
- 环境清单：.local/rebuild-integration/environment.json。测试账号：ui-alpha@example.invalid、ui-beta@example.invalid，普通用户ID2/3；随机密码只在同目录test-accounts.json（0600），不要写入提交/聊天。两用户经真实注册API创建；为此仅临时开启隔离库registration_enabled，创建后已恢复原值。bootstrap管理员独立随机密码在compose.env（0600），未代替用户接受管理员合规确认。
- 实际API已验证：两用户登录200；A创建Key成功；B读取A的Key404、修改/删除403；A的Key未被改变，B列表仍为空；A删除测试Key成功。创建探针已清理。可用公共测试组rebuild-ui-openai，ID2；没有附加真实上游账号，不能据此宣称推理/计费真实上游验收。
- 按用户最新要求，主站与供应商前端也部署Docker，后续测试更新受影响镜像；同项目的web/vendor模板及代理、构建取消测试已完成，实际前端构建按owner指示暂停。端口与实际镜像验收以environment.json后续补充为准，目前32181/32182尚未在本记录声明就绪。前端每个实际页面需逐项操作验收，测试/构建通过或单一Keys流程不能替代全站验收。

## 台湾词组转换修正与工具调用记录

root tools/zh-tw/convert.mjs的既有CORRECTIONS已增加明确词组返佣→返傭；不改hasSimplified准入、不恢复affiliate业务链。按本次明确委托，在既有frontend/src/i18n/__tests__/taiwanLocalization.spec.ts只新增转换、ASCII标识保持及幂等回归；该前端文件仍由前端最终manifest交接，不随root工具提交自动收取。以Node22.20.0直接执行vitest该测试：1通过、7跳过。请前端owner从zh源重新生成唯一受影响TW文案，不手改生成文件。

本次首次通过环境fallback pnpm调用测试意外触发install检查并改写pnpm-lock；已确认并只恢复到原HEAD锁文件，package.json未变。没有执行approve-builds、删除或重装既有node_modules；axios/vitest链接早于此次调用。后续使用显式/Users/luhonglin/sdk/node/bin/node和既有vitest入口，避免fallback包装器隐式安装。实际依赖/锁变化未纳入提交，前端最终验证应继续使用约定工具链。

前端每页验收的统一记录要求：对应实际route/page及角色、Docker镜像ID和源码快照、进入/刷新/返回、该页有效读写动作、关键校验/失败反馈和结果证据；只执行该页真实存在的动作。涉及外部OAuth/支付/真实上游或人工合规确认而缺少条件时明确blocked/not_run，不能将编译、替身或别页冒烟记作该页通过。WP-21汇总完整页面清单，WP-25按实际依赖核验；业务页面实现与页面验收分开计状态。

设置合同补验已执行：现有handler的省略字段保留、auth defaults校验失败不部分保存、revision缺失/过期、非法payment与有效保存共7个测试/子场景通过；证据级别为既有handler stub，不冒充新增真实PG并发验收。

## 最新交付节奏覆盖（2026-10-25）

用户明确要求前后端都归同一个codex/sub2api-rebuild最终提交：开发阶段不再逐批提交或推送；等待全部开发、本机Docker测试及每页实际操作验收通过后，一起归并提交并推送一次。此前阶段提交先保留，不现在重写。前端manifest仍用于核对写集/证据，但收到阶段manifest也不立即提交。所有owner继续本地协作，Git索引和最终归并仍由主集成者统一操作。

台湾词组回归的单文件ESLint也已通过：使用PATH中明确Node22.20.0和本地node_modules/.bin/eslint，未再调用fallback pnpm或改动依赖/锁。

## 前端构建当前暂停点

2026-10-25最新owner指示：整体类型/构建尚未稳定，暂停web/vendor构建等待明确稳定源码检查点。最后快照的localeKeyCompleteness有1失败、3通过，涉及SettingsWorkspace尚未接齐的workspace.*键；没有生成可宣称验收的UI镜像。API32180和隔离账号继续运行。Docker依赖层及包装器占位文件问题已处理，不绕过现有i18n/type检查；前端提供稳定检查点后再构建并进行逐页验收。


## 已有提交归并及继续开发（用户后续指令）

2026-10-25原6笔阶段提交先归并为ea2faa08c；用户随后要求连同父提交6f48d6ab6一起合并，当前HEAD已改为673c53c130c11cf1cb55abf0722dd996bf79eded，父提交为空，分支可达历史仅1笔，tree仍5cdc27e895292f1554f94231d3d5544b64c4a04e。索引内容和已提交源码树未变，没有checkout/reset/stash，没有收取任何工作区修改，也未推送。旧HEAD及祖先链由本地恢复引用保留，恢复bundle已verify，最新回执为.local/rebuild-history/20261025-201243-root/receipt.json。当前只是独立根开发快照；前后端全部重构、Docker及逐页验收完成后统一替换该根提交，首个验收版本才作为后续开发的父提交。各owner继续只改所属源码，不暂存、提交或推送。前述26cf仍是已运行镜像构建时的真实Git基点，不伪改其来源；后续代码以实际文件指纹验收。

前端最新交接确认：六个分域子任务继续协作，Settings已拆为面板/Workspace并有局部回归；旧监控与资源重授权入口仍在收口，featureServices装配仍有旧import，未给稳定构建检查点。web/vendor构建继续暂停；局部测试不等于逐页Docker验收。全部新增前端由其owner给统一manifest，Git索引和共享文档仍由集成者单写。后续前后端改动全部开发和验收通过后，再一起归并提交。


前端后续状态：整站生产vue-tsc（Node24）退出0，45项文档/custom-page/shared SSE/权限scope检查通过；仍处于各domain owner收尾和根目录归属切换，未发布最终源码fingerprint。docs内容迁至frontend/src/features/site/docs/content，103篇内容和实际路由保留。继续暂停web/vendor镜像，等其稳定检查点；这些属于外部会话报告，不由后端冒充浏览器验收。


根工具已跟随docs迁移：tools/zh-tw/gen-locale.mjs的唯一docs源/目标已改为features/site/docs/content/{zh,zh-TW}，没有旧路径fallback，未生成或修改frontend内容；显式Node语法检查通过。前端最终locale/docs校验应使用更新后的根工具。


后端平台清单引用已同步（2026-10-25）：platform_catalog_test.go的读取路径和失败提示、platform_catalog.go注释均改为frontend/src/features/catalog/model/platformCatalog.builtin.json。与当前根快照原constants文件逐字节一致，SHA256=4b623716857f74de963940450e3bc8e0542f4dcdefefac94cac46da243d3bbc2；未改JSON内容/平台定义，未设置UPDATE_PLATFORM_CATALOG，不做不必要的应用构建。前端报告1431处引用切换、245旧源/旧测试删除，仍未给最终稳定快照；web/vendor继续暂停。


## 2026-10-25 根工具与本机夹具最新交接

- catalog测试读取/失败提示及注释已指向frontend/src/features/catalog/model/platformCatalog.builtin.json；JSON与旧提交源逐字节一致。
- audit-ui只迁移原5个精确合规短语的owner路径到features/identity/model/adminCompliance.ts；实际1079源码文件审计0遗漏，没有扩大忽略范围或改法律短语。
- 下载包generator及五份根Python测试已同步sub2api-*路径和SUB2API_*新环境，gpt-image-*名字保留；任意既有provider配置和历史KDAN备份仍有实际读取/恢复覆盖。MAIN源级50测试通过，随后前端owner报告四包/manifest54测试与docs23测试通过，源/归档归其owner；未把并发改名视为源文件遗失。
- BatchImageJobsPage的skillName仅用于ui.batchAgentInstruction说明文本插值，示例请求体没有该字段；后端Go/SQL没有kdan-batch-image固定名称引用。此处不是后端协议识别符，可由前端owner中性命名并同步说明，API路径保持不变。
- locale唯一生成器已支持i18n及features/*两类zh根；用node tools/zh-tw/gen-locale.mjs --locales-only生成，再加--check只读核对。audit-locale也扫描两类TW根。3项隔离CLI验证通过，未生成真实frontend/docs/legal。冻结来源后可显式加--prune-generated-orphans，仅删除完整生成标记且无来源的普通文件；未确认文件保留并报错。详见.local/rebuild-wave4/locale-root-tools.json。
- 当前角色/资源的非秘密回执：.local/rebuild-integration/role-fixture-report.json。凭据只在同目录role-fixtures.json（0600），不写聊天/提交。管理员4为合成历史确认状态，5为未确认负例（423），6为受限admin（groups403）；vendor用户7/8/9分别对应vendor1/2/empty，父关系1→2、组3/4。登录、scope读取、代理改名/恢复均真实API验证；外部admin403、跨供应商资源404、无MFA导出403 STEP_UP_TOTP_NOT_ENABLED。原用户1/2/3未改承诺/凭据。
- 组3/资源1/公告草稿1/下架套餐1已通过真实API改名或改标题并恢复；套餐关联组5，价格为显式合成测试值，未交易。fixture-markdown已返回200；fixture-embed的静态素材在.local/rebuild-integration/page-fixtures/embed.html，须在真实web制品启动时只读挂到/usr/share/nginx/html/__fixtures/embed.html，未启动占位UI。
- vendor origin通过现有revision校验API配置为https://127.0.0.1:32182；HTTP被生产校验拒绝，未放宽校验。钥匙串存在覆盖localhost/127的公共证书，但两者OS验证均CSSMERR_TP_NOT_TRUSTED；没有mkcert/certutil，未导出私钥、未改系统信任、未跳过浏览器安全。供应商浏览器验收仍需可用受信任证书/密钥或经浏览器owner确认的受控测试信任前置条件。
- 运行API仍为sha256:34f9491e44bce58006425fe7bbf3e55f4c66a7e94a0d75df527186ef895abd00，源码fingerprint162a697a671183de187ef23ae7a3652e0dc1524381b8fe067070840849f864a5；不是当前全部后端工作区的新镜像。尚无浏览器页面通过，不标frontend accepted。
- setup安装向导须在UI冻结后用既有Compose另建一次性空库project/端口验证；禁止重置当前夹具库。完整页面矩阵以.local/frontend-page-acceptance/routes.json为准，TLS、外部授权/支付和真实上游条件分别标记，不用接口夹具冒充页面验收。

安裝入口合同已核實：現有main.go在NeedsSetup且AUTO_SETUP非true時進入runSetupServer；既有Compose預設AUTO_SETUP=true，可用本機覆蓋檔設AUTO_SETUP=false、SKIP_SETUP=false及0600檔提供SETUP_BOOTSTRAP_TOKEN，另建新project/空卷/端口，健康檢查改讀/setup/status。此為現有能力，不需新增初始化平台。待UI來源freeze後再執行；不得重用或清空當前角色fixtures卷，不將已安裝站點的/setup redirect當作向導驗收。

目前接口夾具及工具工作不構成完整重構通過：Sidebar metadata/業務locale最後收口、前端最終fingerprint、真實UI鏡像、受信任本機TLS與逐頁操作仍待完成。後端execution/provider全量222項race通過；HTTP/repository50項早期組合與後續變更分別保留指紋，現有組件/handler/Wire仍按owner完成缺失整合檢查。

## 2026-10-25 完整测试用例准备

按用户新增要求，MAIN已编写[系统测试用例](TEST_CASES.md)和[test-cases.json](test-cases.json)：79项系统场景及13项本机API可运行用例。前端唯一入口已确认是[frontend/tests/acceptance/README.md](../../frontend/tests/acceptance/README.md)，根VALIDATION/README/WP-25均只链接该入口，不复制其case清单。

根Makefile已删除失效的FRONTEND_CRITICAL_VITEST旧路径清单及无人引用的critical目标，make test-frontend沿用lint/typecheck并调用现有test:run；本轮只dry-run该目标，未启动全量前端测试。新增API脚本tools/tests/rebuild_api_acceptance.py只可在源码/镜像冻结后显式运行，校验本机项目/容器/镜像/fixture，既有角色数据和当前Docker未变。前端实际逐页操作仍等待双方开发完成、freeze及供应商TLS前置；不提前写passed、不Git提交推送。

## 2026-10-26 前端用例冻结交接核验

已逐项核对.local/frontend-testcases-handoff.json列出的11份文件，SHA256全部匹配。定义为752项，覆盖92页面、17 redirect、2 alias、52设置面板和24类fixtures；唯一入口仍为frontend/tests/acceptance/README.md。前端owner报告4项实际Router覆盖检查及schema正反校验通过，48项相关实现回归通过；MAIN本轮没有重复执行或把定义校验计为真实页面通过。完整执行继续not_run，等待共同项目源码与镜像冻结。

后端继续修复真实NativeHTTP撤权写入竞态、fresh凭据与租户头一致性、发送事实在恢复链中的保存，并移除重复后台启动链。源码级编译解锁和定向回归不等于全部重构完成。当前不更新运行镜像、不执行全站浏览器验收、不进行Git暂存/提交/推送。

## 2026-10-26 前端继续开发期间的消费合同

此前“冻结”仅对已交接的752项验收定义及局部实现证据成立，不表示全部frontend源码已经冻结。前端会话当前继续五个领域查漏及app/shared最终检查，最终工作区指纹另行交接；旧IMPLEMENTATION/HANDOFF的早期目录快照不能作为当前代码未迁移的证据。

| 消费边界 | 当前确定事实 | 后续依赖 |
| --- | --- | --- |
| 管理/用户/供应商API、认证与权限 | 本轮没有新增/删除路由、DTO字段或面向前端错误码；现有revision条件写入、角色范围、合规423及step-up403继续有效 | 后端完整真实包编译与控制面实际消费者回归仍在收口；不要为联调降低前端权限判断 |
| 公开设置与品牌 | site_name/site_logo/api_base_url等现有字段不变；api_base_url只作网关地址展示，不能切换带管理token的请求目标 | 同制品多品牌首屏和实际页面待最终镜像验证 |
| 网关发送事实 | MAIN将Go内部OpenAITransportAttemptError的Unsent改为model.SendState以保留NotSent/Sent/Ambiguous/Unknown；不新增前端JSON字段 | NativeHTTP实际写入撤权guard正在修复；NativeWS SendPermit、BPS/Cloud统一执行切片仍未完整接管，不应标为全系统完成 |
| 用量与能力投影 | unknown/zero/lower_bound及原始/实际/计费模型继续遵守现有合同；本轮没有新增布尔能力字段或更名现有字段 | 若CT-08/11后续实现需新增对外字段，必须先由root记录准确DTO和示例，不让前端猜新字段 |
| setup及三角色部署 | 既有setup/public settings/auth接口不变；API、main、vendor分别构建 | 全新安装须独立空卷；待前端最终来源摘要后才构建实际UI镜像 |
| 本机联调 | 原API项目/32180端口、0600角色fixture、测试数据保留；运行镜像仍为旧阶段来源，不能代表新工作区 | vendor origin仍为https://127.0.0.1:32182；可信TLS未满足，不能用HTTP或忽略证书顶替 |

当前重构没有新的前端破坏性消费合同；若后续工作改变此事实，必须在此先记录精确路由/字段/权限/错误变化及对应owner。根文档和work-items仍由MAIN单写。完整页面测试、最终验收、Git提交推送继续待双方开发完成及源/镜像冻结。

## 2026-10-26 正式前端冻结与UI镜像完成

已核对1948文件全部SHA，冻结集合为d4d7bfae71c4ccbca8d36302cf9d709c1bd06a1544e8ff95579d6315be7809f5。按该冻结源及必要根输入独立构建主站与供应商镜像，完整上下文SHA为90c927992b545676e5080c936125a3c474598493999877f2615c3e282c10e539；两个镜像exit0、构建期间来源不变、临时源快照已清理。精确image ID/耗时/大小见.local/rebuild-integration/ui-build/receipt.json。FRONTEND_IMPLEMENTATION已用本次最终manifest替换早期root桶仍有消费者的描述；WP21/28–35进入review/code_ready，未标accepted。

全量unit首轮4966/4970＋唯一测试文件6/6有效替换得到4970项通过，未冒称单次全绿。当前只完成镜像构建；后端仍未最终freeze，752前端case与系统用例尚未整套执行。所有Git与发版操作继续暂停到联合验收完成。

## 2026-10-26 UI运行与浏览器HTTPS前置已验证

两个冻结UI镜像已实际运行：主站http://127.0.0.1:32181，供应商https://127.0.0.1:32182，来源均为90c927992b545676e5080c936125a3c474598493999877f2615c3e282c10e539。API/PG/Redis原容器未重建。TLS边缘仅用本任务证书和只读本地覆盖，临时写目录均落tmpfs。

通过Chrome原生UI创建未登录的独立“sub2api 本机验收 20261026”配置（Profile 9），只将任务公共CA导入该配置的用户证书库；实际portal-build.json读取匹配来源，浏览器显示“连接是安全的”。wrong-name.localhost负例被NET::ERR_CERT_COMMON_NAME_INVALID拒绝，未点击绕过；macOS security verify-cert仍拒绝该证书，系统信任未修改，无ignore证书或禁用sandbox参数。精简证据见.local/rebuild-integration/browser-tls-verification.json。原“可信本机HTTPS缺失”阻塞已在该专用测试配置中解除，其他个人浏览器配置没有导入。

这些只是环境前置及来源核验，业务页面实际执行仍为0，752用例not_run；后端仍未最终freeze。iframe合成页原拟挂在主站，但主站X-Frame-Options为DENY，正例需独立fixture origin，不能降低主站安全响应头。最终测试前补齐该fixture。

2026-10-26前端后端合同同步跟进：当前image_count_billing、Ops nodes与supplier composite/platform-admin assignment的后端原来均未接齐，MAIN已据实际源码明确告知前端owner，不作已完成承诺。现分别由Halley（group/schema/专用图片消费者）、Gauss（节点采样/健康投影）、Aristotle（供应商授权/脱敏状态）适配固定cd1，MAIN接共享计费/类型/配置与恢复参数。ops.node_id/node_name已加入配置，运行消费者仍待其owner交付；图片新迁移302保留历史248，301预留Native I2。前端可以按权威维护合同继续开发，最终API镜像验收前各项需真实接通。

## 2026-10-26 规划落地复核与新一轮协作

前端上一轮正式交接为`frontend/.local/maintenance-ec-integration/handoff.json`：1963文件、18,462,742字节，源码摘要`748782a3af429e29765bb6fd90a1c66385f2191623521ee5119ac4e195438ff4`。owner回执为105个测试文件1110项通过，后续Logo定向19项有重叠，不相加；主站与门户构建通过，未部署这份新源码、未执行752项完整页面验收。它取代1948文件快照作为最近交接记录，但不能替代仍在运行的旧镜像来源。

owner已按用户指令恢复开发：设置取消/step-up重放、委派管理员草稿及409冲突、lower_bound token展示、peakRate领域归属、两处失效组件及identity无消费者导出。MAIN只维护共享文档和后端，不修改frontend或owner的FRONTEND_REVIEW/product-spec。当前前端不处于冻结状态，本次对历史manifest重算时87个已有文件发生变化；新增路径未包含在这个数字中。完成后由owner重新提供完整来源和有效验证，再串行构建UI。

本次本地main/origin-main观察为`ce3ecf6a1c9c6bac9d4614db174d1fef0599942c`，尚未独立证明远端新鲜度。已向前端会话`01a171ab-53ee-7772-a290-346a110036cd`发送ec0222→ce3ecf精确增量：UsageTable、AccountInfoCell、HelpTooltip及测试、PlatformTypeBadge、供应商AccountWorkbench及测试、管理员AccountsView及sparkShadow测试；按实际领域消费者适配，不整段覆盖旧目录。

后端admin authorization_modes、supplier auth_mode/extra已存在本地改动及部分admin/repository单测证据；对应service投影与授权测试在最新组合回归中被Cloud panic中断，不能对前端宣称已整体验证。后端Native发送前财务意图/恢复接线仍未完成，Cloud新增生命周期行为仍需适配。两侧继续遵守原公共API及权限合同，发生新的接口变化再逐项记录。

本机旧API/UI镜像及隔离测试卷继续保留用于后续联调，健康状态不等于当前源码验收。此次没有Docker重建、页面验收或Git暂存/提交/推送。


## 2026-10-27 BPS勾选列表严格化

主集成复核确认旧代码仍有“缺失模型列表即全选”的分支，与用户明确的“只走勾选模型”不符，现由WP-18-BPS-EXPLICIT-MODEL-SELECTION-20261027删除。openai_excel_bps_models缺失、空或畸形均不选择BPS；不得在前端自动视作全选、静默补齐全量模型或改历史数据。原生授权有效的独立请求按Native路由，Excel-only不能因此获得Native权限。Cloud仍优先，开关Cloud不擦除BPS选择；最终比较账号映射后的模型。独立Images既有格式回退范围不变。

前端owner已收到同步：检查表单初始化、保存、提示及空/缺失/显式选择/Cloud往返用例。后端已接生产路径及24个HTTP JSON/SSE/WS、两类授权组合案例，目录无选择不探测BPS；本条写入时新组正在等待共享验证锁，不能记为passed。

## 2026-10-27 条件更新合同与联合Docker快照

后端已完成系统停用保护的实际接线及复测：管理员full/Extra/bulk和供应商更新均在原事务中核对停用条件；OAuth凭据与Extra一起提交或回滚，不吞掉409。管理员显式重开条件为`extra.openai_excel_bps_expected_disabled_at`，供应商为`bps_expected_disabled_at`；保持安全DTO的原disabled_at字符串，不读私有failure、不转换时间精度。缺失/过时条件返回409 `OPENAI_EXCEL_BPS_STATE_CONFLICT`；不操作开关时省略，显式false可取消旧恢复，即使已是false。供应商旧`bps_all_models`明确拒绝，选择仅用`bps_models`。具体规则以CONTRACTS为准，PG31项和供应商PG1项、OAuth实际消费者结果见TEST_REPORT。

前端owner的`frontend/.local/frontend-final-ready-20261027/HANDOFF.md`交接2018文件，来源`9f7b1da1938a4e376d227ff027ccf1a4b78f3bbd2fab98cd723aeec8e33073e4`，已包含上述条件更新/409交互。其6401项全量Vitest、五项静态检查及两角色构建通过；主集成逐文件核对后，实际构建并部署同源UI，Docker上下文摘要为`4563951d97961fea0847850f9bdc27fed17505445dfbbceae17fe94353c9f8dd`。后者包含部署等额外输入，不能与frontend manifest摘要直接比较。

| 角色 | 本机地址 | 实际运行镜像ID |
| --- | --- | --- |
| API | http://127.0.0.1:32180 | sha256:de9404189f9712c17ff572250b6425443ed3077885ce70206510fb382e4a6304 |
| 主站 | http://127.0.0.1:32181 | sha256:2d217ec7aeec925dd9c81d69ee8526b9545a5db069cda72f63fb4b2c868276e9 |
| 供应商门户 | https://127.0.0.1:32182 | sha256:8513972fb6b7127659a87cd448ddd35d433943616ac8dee364bbe39d1ce23923 |

API对应独立冻结后端来源`3a0638612e059de48e7f4fcb665ba1b66175e58c61e16a669e26698ac61ed2ea`。本次只替换三应用容器，PG/Redis/TLS和固定合成测试角色保留；角色fixture及环境回执已更新为实际镜像，保持0600。专用Chrome Profile 9既有测试CA与安全验证继续有效，不安装系统证书、不忽略证书错误。完整部署/来源证据见`.local/rebuild-integration/api-unified-20261027/deploy-receipt.json`与`.local/unified-closeout-20261027/ui-runtime.json`。

已部署API的13项权限/脱敏用例通过。主集成另用电脑工具验证合成错误凭据被拒绝、刷新表单清空以及未登录访问管理页重定向；截图`login-rejection.jpg`及AX在`.local/unified-closeout-20261027/`。此冒烟不覆盖整个登录用例，更不替代753个用例/92个页面/51个面板。中文页面出现英文凭据错误已交owner处理；前端当前已恢复修改，须按新交付/受影响验证更新镜像，不能把9f7b1da称为实时源码冻结。

后端桥接会话也仍在继续跨流/跨实例缺口，当前API只包含已验证快照；部署时39个后续变化路径明确排除，待稳定交付再审核。完整页面验收的共享设置、品牌和策略修改由单一执行者串行处理，其他浏览器执行使用隔离测试上下文。两侧保持本地未提交状态，整体验收前不收取索引或推送。


## 2026-10-27 运行验收修复版UI已更新

最新已部署前端owner来源改为`66f4ada428b498fec87e7bf67d072e8dc629f3740221a87641940b895882e5d1`（final-v2，2026文件）；Docker上下文为`1bae0522affeb33825ba0d3d742f444ceeea876c02fcf587e92665ba2c157d2d`。主站ID`sha256:b832ea5d7cb7faf53b569d5e92e74eb5c410de37108a61bb937f07ef39fceaa8`，供应商ID`sha256:4d1d9fba9a4452e2fcd9d4c680808798fad919686d7de85b4207d17437c16c0e`；地址32181/32182不变，两个实际build metadata均200且摘要匹配。API保持3a063861，测试角色及PG/Redis/TLS不变。部署回执`.local/remaining-closeout-20261027/ui-66f4-deploy.json`；owner已获通知重载，继续已修缺陷复验和753项完整清单。

后端控制面Docker10项及双网关追加3项通过，错误信封仍为数字code加reason；前端按现有reason优先处理。没有变更网站全局策略、原测试资源或前端源文件；所有业务验收需继续绑定实际image/source。

## 2026-10-27 18:12 API 新检查点

API 已更新至 `sub2api:rebuild-api-597456181362`（源码597456181362…，image73fe0c5bf788…），端口32180、原本机测试账号和PG/Redis/TLS均保留；web/vendor仍为66f4ada镜像。API13+BPS10及SystemOne隔离HTTP/PG计量通过。SystemOne外部请求/响应不变，后端已退出旧执行与异步账务路径；OpsMetricsCollector参数类型修复已进入运行镜像。构建/部署与源码manifest见 `.local/systemone-closeout-20261027/`，环境和私有fixture已经同步实际container/image，不沿用旧3a回执。

MAIN电脑实际操作运维页刷新/全部服务器弹窗，确认新节点正常上报；仅记录该页面子集，未写frontend用例为全通过。观察到默认前三个卡片均为历史离线节点、活跃节点位于第四项，该呈现已发给owner评估。frontend源码/目录与完整页面验收仍归前端会话，不改变其活动写集。

后续18:36复核：前端owner已交付并部署 `ops-node-7c67dd244109` 两角色镜像，覆盖上述66f4ada运行状态。MAIN核对2068个交接文件全部匹配，实际image分别2aa674fe3a48…/3c11eefba989…；电脑刷新确认活跃节点首位，回执 `.local/systemone-closeout-20261027/frontend-ops-independent.json`。MAIN不修改frontend源码。统一模型管理12条后端API/CAS/运行读者已作为MAIN待实施切片登记，不能归责为前端已完成页面却未验收。


## 2026-10-27 重新初始化人工验收环境

按用户明确要求，MAIN已备份并移除旧 `sub2api-rebuild-ui-20261025-4e2074d6` 的六个容器、三个命名卷及项目网络，另移除带 `codex.task=sub2api-rebuild-contract-check` 标签的合同测试PG及其匿名卷。已核对归属且无容器引用的13个旧重构镜像标签一并清理；共享基础镜像、来源未确认的fixture镜像、其他项目网络及共享编译缓存未动。备份仅保留在0600本地文件，不进入新数据库。

新项目为 `sub2api-rebuild-acceptance-20261027`；主站 http://127.0.0.1:32181 ，API http://127.0.0.1:32180 ，供应商 https://127.0.0.1:32182 。后端从当前冻结来源 `4cb8cac3be34dd4cb40f530865a4705398afe051c1b78dcee6933a08220f79ce` 根Dockerfile构建，image `sha256:6698fe2f6dab9f2b089633b054f7d81370d62cc187edbf4f71f71cf1a9d9e24d`。构建前344项定向测试/子测试通过，构建期间输入未变；此组不覆盖所有新增Cloud逻辑。前端2068文件逐个校验，继续复用owner最新 `7c67dd244109...` 的两角色镜像，实际重新创建容器；未修改frontend源码。

六个新容器healthy；10项部署检查通过，含主站反代登录与身份、主站/门户健康、构建来源、门户测试CA验证。数据库仅有1名初始超级管理员，上游账号/API密钥/用量/供应商均为0。电脑实际验证登录后停在部署与运营确认弹窗，由用户亲自确认；未代为接受协议。未执行真实上游调用。该环境供人工验收，不能标记全功能或整个重构验收通过。

权威运行记录、启动配置、私有登录资料和备份位置：`.local/manual-acceptance-20261027/environment.json`、`smoke-report.json`、`LOGIN.md`、`old-environment-backup/`。旧 `.local/rebuild-integration/environment.json` 已显式标为superseded；旧role fixture仅是历史证据，不得拿它运行新环境测试。用户人工验收期间保持这套运行镜像固定，后续替换需重新记录来源并与人工验收协调；新数据库不沿用此前的合成多角色及确认记录。


## 2026-10-28 CPR后端候选已验证，人工环境冻结

新增既有scheduling_trace detail的version=2、decisions_complete:boolean，以及diagnostics.gaps的incomplete_scheduling。旧记录缺字段不表示完整；dropped_decisions与上游attempts分开。已只读核对OpsErrorDetailModal/errorDetailResponse的kind消费并通知owner后续展示/翻译，无新端点或删字段，frontend由其owner单写。

同轮计划漂移返回503 response_execution_plan_changed，沿原错误信封；BPS禁止当前执行转Native、系统自动停用后新请求分流及取消语义不变。候选 sub2api:cpr-review-04b36ed10bd0 仅在隔离环境验证，未替换32180/32181/32182。后端测试不改变前端整站验收状态。


## 2026-10-28 用户要求前后端合成一个提交

前端owner只读确认当前2070文件由基础7c67dd清单与BPS、采样明细交付逐路径覆盖合成：18项修改、2项新增，全部SHA一致。BPS253与采样91是既有结果，不冒称本轮重跑；最终类型检查通过。完整收取manifest见 .local/rebuild-history/20261028-101354-all-code/frontend-manifest.json（当前文件内容映射摘要 4fae964027134792cfcfaa67fa108be5d4485bdf934fca2772c44e3424cf5192，该摘要算法与owner既有文件元数据清单不同）。后端4521构建输入与CPR04b36冻结一致。本次只整理一个本地独立根提交，保留恢复bundle；未完成页面、接口和矩阵状态继续保留，无推送或Docker更换。

## 2026-10-28 统一模型管理完整接入与本机功能验收

用户随后明确授权继续完整接入并测试。此前只有前端切片、缺少后端消费者的状态已解除：正式入口为 `/admin/models`，模型目录、来源同步、GPT 映射、价格及账号独立引用共12个管理API已注册并接入持久CAS、权限目录、Wire及真实运行消费者。目录禁用、别名解析、账号引用、分组候选和价卡读取均消费同一中央配置；保留实际账号 rewrite、渠道价格优先级、Native/BPS/Cloud原执行所有权及发送前撤销检查。新增306迁移拆分旧identity mapping与模型引用，未修改已应用历史迁移。

已按维护会话“复核并补齐协议桥接方案”的实际修复语义处理：来源同步不重新启用人工禁用模型；新建/编辑账号的新增候选和默认选择排除禁用模型；管理员交互测试供应商账号携带有效管理诊断上下文，不再误走供应商工作台拒绝分支。后台探测仍要求原有供应商作用域，供应商/分组/代理授权未扩大。风险模型选择独立于账号写命令；BPS空集合仍为显式空，Native空引用表示继承。

真实联调暴露模型策略重复读取耗尽64次调度操作预算，已修复阶段内N+1。manifest两字段各1、128、1024条以及128次capability初筛均只读2次策略；下一阶段重读并能拒绝撤销。未扩大或重置预算。最终真实本地HTTP生成已通过，未以单测代替运行证明。

验证结果：前端全量6769项通过；最后价格数字格式与标题结构微调后111项受影响回归通过，两组有重叠、不相加。最终86个变更TS/Vue文件ESLint通过；两角色Docker构建的类型、i18n和Vite检查通过。后端最后专项527个测试及子测试通过，覆盖模型管理、仓储CAS、真实发送消费者、供应商诊断、Native/BPS/Cloud与价格/账务；authz/cmd在该筛选命令中仅编译，不计业务用例。扩展调度套件3个既有父测试失败已在精确HEAD `b572e1f`、撤去本任务全部backend Go变更的只读overlay中复现，完整后端套件不能记为全绿。

本机真实API/PG验收21项全部通过，含匿名401、普通用户403、并发写一胜一409、同步保留禁用、引用持久化/继承、独立映射/价格CAS、别名实际生成、用量落库及禁用别名发送前404。受控本地上游返回input=10/output=2，手工价格input=1/output=2美元每百万token，实际落库费用为0.000014美元；此证据不代表真实外部供应商验收。

电脑实际浏览器验证正式菜单/路由、切换平台、禁用与保存、选源同步后仍禁用、账号独立引用保存与继承、新建账号默认排除禁用、价格编辑回读，以及双页409后保留草稿、读取最新对照及显式合并。手机视口工具未成功应用，未记手机验收通过；四语浏览器完整矩阵及全站93页/762项用例仍未整套执行。WP-20仅此子切片完成本机功能验收，父工作包及整个重构保持未完成状态。

当前同一人工验收项目 `sub2api-rebuild-acceptance-20261027` 已更新API、主站与供应商门户，六服务healthy。API来源 `39fa0d9b8969c46f79e1f064963ef9ea73c4b5b634a59ec9947a96172f010e85`；前端2085文件来源 `62ac19c22afd16822f438e0560fbf18b035b25a4b699c36535c8da7077b29175`，两角色构建上下文 `49480b63196edd9e5674cf1031a9e414bcc93c643c2b51473b4023a6c396842d`。全部4541个API输入和2085个前端输入在交付时逐文件核对一致。权威环境记录已更新 `.local/manual-acceptance-20261027/environment.json`，原初始化smoke报告只作历史证据。

验收临时账号、分组、用户、密钥及别名已按归属移除，原价格覆盖和目录种子已恢复，无全库重置；测试前数据库恢复包0600保留。证据入口 `.local/model-management-integration-20261028/validation-summary.json` 与 `browser-verification.json`，前端截图及全量/定向结果在 `frontend/.local/model-management-integration-20261028/`。本次由主代理与5名子智能体分工实现及复核，改动保留于共同工作树，未新增提交、未推送、未触及生产。


## NewAPI 独立分支

用户已明确将当前独立重构成果发布到 NewAPI 分支。frontend/** 的代码收取范围保持现有 owner manifest 与维护交接的逐文件核对结果；网站运行时及数据库兼容身份继续使用 sub2api，避免改名影响持久化键、账务和接口合同。


## 2026-10-28 活跃节点预览与账号资料布局提案

按用户要求，NewAPI节点预览已改成默认最多3台新鲜上报实例，历史节点不补位；无在线显示空态且保留全部历史入口。四语统计区分当前展示、在线及历史；不改后端30秒/24小时合同、不删除历史。42项节点/Header/i18n测试、局部7文件lint、类型和两角色Docker构建通过；实际浏览器验证0在线/3历史及恢复后1在线/2历史，展开排序与状态正确。证据见.local/ops-active-nodes-20261028/HANDOFF.md及browser.json；当前环境元数据已更新，同一API/PG/Redis/TLS未替换。

账号信息已完成实际授权/刷新/profile/quota消费者分析及三方案交互原型：精简悬浮卡、分组信息卡、详情侧栏。正式AccountInfoCell未改版，待用户确认。字段来源、平台差异、缺失与权限语义见frontend/docs/ACCOUNT_INFO_DESIGN.md；不把设计原型或示例数据记成已开发或真实上游证据。本轮不提交、不推送、不修改线上维护站点。


## 2026-10-28 用户确认双排账号与精简卡实施

按用户附件，AccountInfoCell正式接入第二排套餐、支付/订阅方式、访问凭据有效期三项同行；悬浮卡显示邮箱、套餐与原始rate tier、实际授权方式、支付方式和完整带时区凭据日期。管理员credentials.expires_at与供应商authorization_expires_at分别接线，不混用账号/订阅到期；明确Stripe/Apple等枚举四语本地化、未知不推断。保留现有复制/键盘/固定及有效扩展信息。

168项受影响回归、118项两侧集成通过；先前核心83项与168重叠不相加。局部lint、类型及两角色Docker构建通过，2088文件来源一致；真实浏览器验证三摘要同行、Stripe/Apple/缺失状态和悬浮卡固定/关闭。停用且关闭调度的3个虚构账号已按归属移除，无真实上游请求。未提交/推送，未更新线上维护站点；证据入口.local/account-info-two-row-20261028/HANDOFF.md，当前环境元数据已更新。
