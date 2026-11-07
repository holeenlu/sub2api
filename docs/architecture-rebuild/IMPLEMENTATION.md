# 独立重构实施记录

日期：2026-10-31。当前维护分支 `NewAPI`；起点6f48d6ab6fcc29763af84410690530aeaa957403。原维护main、TapModels、tokensavy不在写入或推送范围，仅保留历史参考。工程契约0.5；前端实现由其隔离会话按普通增量接入NewAPI。

## 第一波完成范围（历史检查点）

| 切片 | 实际结果 | 尚未代表完成的部分 |
| --- | --- | --- |
| WP-01-BUDGET | ResourceOwner/AttemptTarget纯值；execution独占总尝试、physical owner、目标、准备次数和累计等待计数；已有Native/BPS/Cloud消费方接入 | 全Operation状态机、SendPermit及完整执行引擎 |
| WP-02-SSE | 纯SSE分行器与测试迁至protocol/sse，服务/BPS接入，旧pkg/sse删除 | 其余协议、provider及会话迁移 |
| WP-27-API | 唯一根Dockerfile，deploy/api独立PG/Redis/API模板、镜像/项目/可选容器名配置 | 完整UI角色集成及真实上游验收 |
| WP-27-IDENTITY | projectinfo集中技术名；站点/日志默认sub2api；显式网站/日志设置保留 | 前端品牌与资产治理、历史部署迁移 |
| WP-24-UPDATE | 删除旧下载/校验/解压/二进制替换/备份回滚、Compose socket、缓存及四个失效API；删除旧UpdateService文件，保留纯BuildStatus和独立重启 | 新的制品发布自动化没有开发或宣称完成 |
| WP-24-PACKAGING | 删除旧品牌发版矩阵、GoReleaser、安装器、多套旧主站Compose；删除门户updater代理和品牌网络覆盖 | 门户完整新UI由前端会话验收 |
| WP-24-DOCS | 替换旧项目入口/交付规则、清理无引用旧Logo及推广图片、消除维护品牌推送选择器与当前资料断链 | 历史输入资料仍保留作研究/许可证据，不当作当前运行清单 |

本轮未新增网关生成入口或持久化schema。预算保留原8次上限/配置收紧、owner最多3次、目标1或2次、准备2次、累计等待10秒及正向抖动；权限、取消、首输出deadline和模式恢复仍按既有真实调用链处理。没有第二套计数实现。

## 已删除与有依据保留

旧自更新不是放在死开关后：releaseSourceTrusted、下载/替换/回滚方法、update_check_cache、UPDATE_CHECK_ENABLED及专属路由/权限项一并移除。只读构建状态只保留当前version/upstream_version/build_commit三个值，无I/O、force/context或不可能的错误分支。前端VersionBadge仅消费getVersion；check-updates和升级器专属响应头也删除，不留无消费者安装状态字段。接口清单见FRONTEND_HANDOFF。

GitHub latest/recent读取继续供Claude Code和Codex版本同步；同一代理配置也供远程价格读取。保留这些实际消费者、鉴权/重定向/出口保护，不把它们按update名称误删。供应商门户的静态站点/同源代理、许可、SDK、法律文档、历史SQL和未被接管的业务模块各有当前用途。

门户的旧updater/socket专属代理已删除；Nginx真实配置检查及两个纯代理替身测试通过（不是前端应用构建或真实UI联调）；其独立Dockerfile补充自己的构建上下文，避免根API-only忽略规则破坏有效门户构建。根assets的旧Logo、wordmark和两张partners推广图片无消费者，已删除；frontend目录未由本任务修改。

## 验证证据

- Go 1.27.3新核心-race通过；预算/SSE源码后续未改变，621个服务测试/子测试的有效回归结果复用。基线中唯一8 MiB陈旧断言改为真实32 MiB上限及零交付断言后，定向复测通过，没有修改运行上限。
- 站点显式配置、项目配置/日志、BPS SSE消费、Go vet、依赖边界和本机独立二进制已验证。完整golangci-lint因本机工具不能解码Go 1.27.3 export data v5而未通过；未伪称全部lint完成。
- 清理后版本同步、GitHub读取鉴权/代理、构建状态、权限目录和中间件284个测试/子测试通过。处理器/路由编译发现的一个无用import已删除，两个受影响包定向复测通过。
- Wire通过真实生成器重新生成，使用任务临时modfile承载工具依赖，应用go.mod/go.sum未修改；不以手改生成代码掩盖类型问题。
- 交付工具19项测试通过；API部署权限/密钥/卷隔离/命名/门户接线8项测试通过，资源与Caddy检查通过。部署worker只完成部分清理，剩余测试及代理移除由主代理接手完成，未把未运行检查记为worker成功。
- 第一波Linux arm64镜像实际构建、UID1000、PG客户端、隔离初始化、健康、公开设置、登录和重启持久化已通过。清理后唯一根Dockerfile已重新构建，独立新数据库初始化、健康、公开设置、登录、重启持久化通过；原三个安装/回滚接口实测404；前端接入后第四个无消费者check-updates读接口也已删除并补相应回归。最后默认值调整的镜像/原生编译结果继续按当前源码记录。
- 管理员check-updates实际路由受既有423合规确认保护；没有代用户确认。纯只读状态由服务/处理器测试证明。真实上游、生产、Linux amd64和完整UI均未验证。

## 并行与交接

第一波由3个实现worker和1个只读审查worker协作；追加清理由2个worker分离写集，主代理整合共享配置、权限、Wire、文档及未完成部署验证。所有worker均已结束，Git索引和发布由主代理单写。

前端提交737fd177已审查并作为188533162整合。核对其365项测试、类型/lint与web/vendor构建回执，并校验frontend树d786178e与提交一致；相关backend JSON与法律输入未改变，复用该会话有效证据。没有复制未提交文件，也没有宣称真实UI登录或上游联调完成。

## 后续开发

继续补齐access/session/billing/catalog端口与统一执行引擎，再完成原生完整切片及BPS/Cloud/其余平台迁移。每个切片同步删除替代代码、旧配置和测试；不把无用旧项目文件保留到最后。当前仅上述切片完成，36项父工作包未全部验收，implementation_ready及rollout_ready仍为false。

## 第一波整合核验（历史检查点）

已审查并整合前端提交，frontend源码树保持其365项已通过验证输入不变；相同backend JSON、法律输入和依赖清单核对一致。集成后删除最后一个无消费者check-updates读接口、升级器响应头和安装状态字段，服务/处理器/路由定向回归通过。最终源码镜像重新构建并实际启动，健康/中性公开设置通过，四个退役接口均实测404；测试容器、卷、网络和镜像均已按本任务归属移除。

后端代码检查点5aaf015b6，前端整合检查点188533162；后续集成和上游同步只收敛到NewAPI，不移动原维护分支。详细SHA、最终资源实测与保留证据以最终交接回执为准。

## 第二批后端领域接线

共同分支继续实施完整36项计划，未将第一波当作整体交付。前端owner及其两个worker正在共同工作树内处理identity/keys和shared HTTP；其未交接差异未纳入后端提交。

- WP-08-FIRST-OUTPUT：首输出绝对截止、自动/显式policy、准备暂停、交付标记及唤醒归execution.FirstOutput独占；HTTP/WS/BPS/Cloud、调度重试与观测消费者接入，旧六项状态字段删除。完整Execute、SendPermit及跨端口编排仍未完成。
- WP-05-EXECUTION-SCOPE：protocol/codex只解码，session域解释thread/kind/subagent/connection-stream身份；Gin边界继续使用现有显式session解析与哈希。保持线程优先、memory/guardian隔离、多路复用与原nil边界，不将客户端声明当权限。
- WP-06-RESERVATION-LIFETIME：billing/reservation拥有预留引用、一次释放、显式续期、取消和join；服务/Cloud恢复/Redis消费者接入，旧struct和三个旧缓存接口删除。四段Redis Lua和估价部分逐字保持，不新增资金策略、schema或兼容alias。
- WP-24-REDEEM-TIMING：删除已退役用户兑换码路径的Server-Timing许可，添加root/子路径负例；audit测试改用有效accounts/batch-refresh。Claude reset-credits独立兑换保护测试通过；邀请和余额历史仍读取旧数据，保留相关模型/历史迁移。

验证采用实际影响范围：execution race通过；session三个测试及41个子场景通过；reservation 11个测试及2个子用例race通过。主代理跨service/handler/repository/middleware/routes整合回归，修复遗漏的WS旧字段引用与测试时钟夹具后，累计675个唯一测试/子场景均通过。测试时钟修复只影响夹具与基准断言，没有更改运行超时策略；有效通过项复用，修正后定向覆盖所有受影响用例。新领域depguard及Go vet通过，当前源码CGO_ENABLED=0独立API二进制编译通过（37.072秒）；临时二进制已删除。Redis相关本轮为现有miniredis测试，不冒充真实Redis/上游验收。

本轮没有新增对外API、运行配置或平行执行路径。父WP-05/06/08仍in_progress；全量状态机、发送前授权/租约合同、完整账务与provider迁移、前端全领域和真实UI/上游验收继续保留为未完成。完整golangci工具的Go export-data兼容限制仍未解除，不能将depguard/vet通过表述为全量lint通过。

后端两个领域worker和主代理分离写集；资源worker仅处理第一波确证归属的临时文件/缓存。共享锁被另一维护任务占用时按既有有界等待处理，未移除锁或打断其进程。第一波测试容器/卷/网络/镜像已删除，SDK与有效共享依赖保留；Docker构建缓存仍超5GiB，已知取消构建记录不可回收，安全清理未达容量目标。最终实测与精简本地证据登记在.local/rebuild-wave1、.local/rebuild-wave2，未提交凭据或完整日志。


## 第三批授权与本机部署

WP-03-DISPATCH-REVALIDATION已接入：access拥有供应商/模式发送前复核的纯规则与窄函数端口；现有repository继续作为唯一实时SQL权威。服务边界负责账号、group及上下文转换，BPS与native传输显式携带mode，不再从可伪造的context标记决定模式。已删除生产vendorAuthorizationModeKey；测试仅保留旧键作为伪造负例。服务层ErrVendorSupplyUnavailable保留同一错误身份，因为现有真实消费者仍按该错误判定；不新增第二个授权实现。用户/Key完整scope、撤销epoch、租约与全部异步交接合同仍未完成，父WP-03保持in_progress。

纯access四个顶层测试（含子场景27项）race通过；主代理集成服务侧16个顶层测试、共78项测试/子场景通过。初次整合失败来自新测试以string比较GatewayFailureReason，已修正为既有具名类型；没有改错误语义来适配测试。depguard通过，当前API镜像实际构建并复用原隔离库更新，健康和两用户Keys隔离重新通过：A创建成功，B读404、改/删403，原对象未变，探针已删除。没有执行真实上游推理。

最新API制品sub2api:rebuild-api-471bc2fa7ccf，镜像sha256:2f99f40b4aad53930bb2dd52acd49231a599f8b2d34d9878a2f702c970066635；源工作区hash为471bc2fa7ccf859592223f6f81aae34769a61955e35abf30292e57779d7586b7，Git仍为26cf4313，不是新的提交。构建42.852秒。源码/镜像及运行证据在忽略的.local/rebuild-wave3，持续联调配置和凭据在.local/rebuild-integration，不进入Git。

部署已统一到deploy/web/Dockerfile的main/vendor两个target；旧供应商独立Dockerfile及重复忽略规则删除。默认API/web/vendor端口8080/8082/8081，持续联调覆盖为32180/32181/32182。主站同源代理覆盖真实API、网关、setup及已有尾斜杠重定向路径；门户保留原有入口隔离。请求访问日志移除query并对BPS图片token脱敏，保留upstream状态和耗时；server级原始request error日志关闭，启动配置诊断仍保留。没有增加API端点、跨域许可或隐藏网关绕过路径。

部署纯配置/权限10项通过；主站7项与门户3项真实NGINX代理夹具测试通过，含成功/失败日志脱敏、大上传、SSE首帧和WS双向流。夹具只含最小HTML/JS，不是前端应用镜像或页面验收；夹具镜像、网络、容器和临时上下文已删除。源码快照取消采用自有子进程组，TERM有界等待后必要时KILL，写cancelled并清理临时上下文；8项真实小进程测试通过，覆盖重复信号、后代进程、无关进程保护和已完成镜像记录保留。SIGKILL无法保证清理或终态写入，不声称可处理。

前端owner仍在领域迁移；最新完整UI构建因workspace.*语言键未接齐失败，按owner要求暂停实际web/vendor构建。API32180保留可用；32181/32182尚未提供真实应用验收。等待稳定源码检查点后再构建、部署和逐页操作。全部前后端开发及本机验收完成前不再提交或推送；已存在阶段提交暂不改写。


## 分组授权与模型目录规则接线

WP-03-GROUP-ACCESS由Copernicus实现、主集成者复核：access统一拥有分组绑定、可用性和Key分组资格规则，实际HTTP/Google/WS续接与Key创建/更新、可绑定分组列表均已消费。旧service与middleware中的规则删除，只保留服务实体转换和原协议错误映射，并在源码注明直接接收access值对象后的退出条件。nil GroupID与显式零、缺失/删除/停用分组、公开/专属/受限公开分组、供应商限制和订阅例外均保持。没有把本切片声称为完整身份授权、撤销代际或租约。

该worker使用Go1.27.3完成38项纯access race用例，三个分组函数覆盖100%；53个顶层service/middleware测试、含子项135项通过。主代理核对六个改动文件SHA-256与其回执完全一致。135项结果来自worker实际工具输出，已按来源登记为worker handoff而非伪称主代理重跑；整个catalog源码指纹未由该worker保存，所以这批结果不充当catalog行为验收。

WP-07-MODEL-ALLOWLIST由主集成者实现：原service白名单类型、归一化、匹配和列表投影迁到catalog，所有DTO/cache、service、handler、repository与中间件实际消费者已改用catalog.ModelAllowlist；旧group_model_allowlist.go及两份service/domain转换函数删除，无service类型alias兜底。Ent/domain结构继续承担持久化表示，仓储边界显式类型转换，数据库schema、JSON字段及nil/omitempty语义不变。

白名单需要的Codex模型名、effort/日期后缀、图片模型分类和已有别名表也由catalog单独拥有；原转换、价格/限流、诊断、影子模型及模型列表消费者改用该唯一解释。不是另建一份模型列表，也没有扩大unknown为supported/unsupported。模型别名仍是已有客户端/上游合同，不是旧部署品牌兼容层。原spark别名包装删除，catalog返回新切片而不暴露内部map。group_model_catalog.go只保留仍依赖Account/Group实体的发现补全和字段投影，不复制白名单判断。

主代理的静态逐函数核对确认13个模型解释函数在符号迁移后函数体一致、两份别名表逐字一致；Zeno独立只读审查另核对19个迁移函数及两表，未发现新增语义偏差。归一化拥有新切片，但现有DTO/cache副本仍可能共享Models底层数组，不能将此结果描述为已实现不可变PolicySnapshot。

实际验证：catalog race 48项通过；消费者定向回归合计401个唯一测试/子场景通过、0失败。初次编译出现遗漏私有调用和无用import，均已修正；原已通过包复用输入未变的结果，失败包分别重编译和定向复测通过。1项TestScopedAdminGroupModelsPostgresScopeAndAtomicWrite因未提供独立SCOPED_ADMIN_TEST_DSN跳过，不能记作真实PG事务测试通过。当前catalog/access depguard及相关Go vet通过；全量golangci工具兼容限制仍单列，不声称全部lint通过。

当前独立API镜像sub2api:rebuild-api-162a697a6711，ID sha256:34f9491e44bce58006425fe7bbf3e55f4c66a7e94a0d75df527186ef895abd00；源工作区hash 162a697a671183de187ef23ae7a3652e0dc1524381b8fe067070840849f864a5，Git仍26cf4313。实际Linux arm64镜像构建37.256秒，前后源码指纹一致。更新原隔离Compose项目后，健康、既有两用户登录、可用分组完整JSON与上版一致、Key创建/越权GET404与PUT/DELETE403、本人对象未变、Key计费自省200均通过。零余额/v1/models按原规则403 INSUFFICIENT_BALANCE；这不等于成功模型列表或真实上游推理验收。没有发送生成请求，探针Key已删除，原用户/组/卷保留。

当前未提交后端改动相对HEAD的累计统计（含新增access/catalog文件）：业务及配置53文件，新增969行、删除763行、净增206行；测试44文件，新增1190行、删除559行、净增631行。新增外部接口、后端运行配置及并行执行路径均为0。统计只覆盖backend，不把另一会话未完成frontend内容算作本轮交付。完整工作包仍未全部完成，implementation_ready/rollout_ready保持false；本轮未暂存、提交或推送。

已删除本任务过期的rebuild-ui-26cf4313镜像，保留当前API和上一成功471bc镜像供持续联调/回退（各约182.6MB）。保留API数据364KiB、PG数据79720KiB、Redis1132KiB；本轮完整测试/构建日志和临时编辑helper已在汇总精简证据后删除，wave3回执约1.2MiB。共享Docker BuildKit实测12.6GB仍高于5GiB目标；无法按现有证据安全归属全部共享记录，未执行全机prune。worker报告的共享Go缓存超限也未据此宣称已清理。资源容量收尾尚未完全达标，持续联调资源按用户要求保留。

## Native HTTP 真实调度接线（2026-10-25，进行中）

已将Responses、Messages和ChatCompletions的HTTP入口在排队前绑定原始Key/User、客户端模型和安全IP；用户/账号实际AcquireResult按同一operation索引，发送时按新鲜并发策略Verify，并仅绑定本次选中的两份租约。Native bearer路径依次经过实际凭据解析、最终编码与wire封定、现有Dispatcher.Admit、单次provider.Open；不再额外调用旧TakeOpenAITurnAttempt。TLS-before-HTTP恢复由operation显式至多重试一次，重新准备同一封定内容并再次消费原预算。旧端点、代理或身份头配置与新凭据混用时拒绝发送；本地授权/容量失败不记作上游健康故障。

这不是WP-10或整体重构完成证明。首轮组合执行198项通过、1个旧容量fixture失败；该失败已由其owner复现并以实际token/期限fake修正，7项定向race通过。随后显式重定向/许可/预算/provider/真实repository组合50项race全部通过，输入指纹稳定，见.local/rebuild-wave4/native-redirect-tests.json。后续修改仍需按其实际影响验证。原生WS、AgentIdentity、插件/其他provider模式、完整Execute状态收敛及真实Docker验收仍须继续完成。重定向采用operation内标准HTTP控制器，每跳重新Prepare/Admit/Open；实际响应允许的transport continuation不放开普通业务重试，仍受原总预算、物理owner和目标预算约束；跨主机敏感头剥离、重定向禁用及供应商代理目标规则继续执行。前端尚未发布稳定源码fingerprint，web/vendor镜像继续暂停。当前HEAD仍是无父提交的临时根，所有新增工作均未暂存、提交或推送。


## WP-03 authority 与真实 handler consumer 交接（Copernicus，2026-10-25）

当前切片仍未验收。Key/User纯规则已被HTTP、Google和WS续接消费；NativeDispatchAuthority使用fresh Key/User/Group及账号仓储、实际TokenProvider bearer、完整持久凭据摘要和实际routing容量proof。已证明unlimited可使用零附加期限，但缺失/零fingerprint仍拒绝，期限不以now+TTL伪造。SimpleMode保持既有full-pool调度，同时保留fresh Key/Group/model及供应商模式复核。没有重复调用账务准入或RPM写入。

PAT当前代码支持持久auth_mode/openai_auth_mode分类的无expires_at凭据；仍核对实际bearer、完整凭据及physical owner，已保存期限则严格校验。已补无expiry、分类变化、TokenProvider期间分类变化、token替换、owner变化及真实expiry测试。早先服务包实际通过包含这些用例；本轮service重编译因下述外部测试接口未迁移而阻塞，不能把旧通过称为当前共享树复测通过。AgentIdentity仍沿MAIN保留的既有模式；新的签名grant证明另需持久owner/完整凭据、签名键身份、runtime/task身份及实际准备的签名assertion，不以时间戳充当代际。

handler测试夹具已接入具体NativeHTTPUpstream（含ValidateNativeRedirect）、fresh Key/User/Account存储、实际TokenProvider缓存、Redis-backed routing有限/unlimited收据及显式生命周期。旧Do/DoWithTLS端口在夹具中拒绝，正常用例必须成功到Prepare→Admit→Open并返回实际内容。补充GotConn/WroteHeaders/WroteRequest事件，连接后调用真实permit复核；三入口撤销测试覆盖Admit、Consume及取得连接后的等待边界，并检查没有向脚本上游发送。原取消/520事件、403切换、Astra Pro/effort及账务断言保留。Usage测试改用host.UsageWorker及真实配置factory，Start显式、Stop有界且检查错误；夹具goroutine在资源关闭前join。以上handler运行结果尚未取得，未加入production测试旁路。

本轮实际命令均在backend下使用SDK /Users/luhonglin/sdk/go1.27/bin/go、GOTOOLCHAIN=auto、Go1.27.3、共享sync-upstream.lock有界45秒及独立GOTMPDIR。go test -tags=unit -p=1 -count=1 -json ./internal/handler -run '^$'运行27.148秒、exit1、0测试：当前生产admin/codex_diagnostic_handler.go:25的scheduledRunner.Start()缺context。另一次authority+middleware定向命令运行12.436秒、整体exit1：middleware两个顶层、含子项36项通过；service因usage_billing_recovery_test.go:79仍调用Stop()以及usage_record_worker_pool_test.go:39引用已删NewUsageRecordWorkerPool而未编译，authority/PAT没有执行。这三个外部写集阻塞未由本worker修改，未重复执行已知失败编译。

精确argv/选择regex、错误、12项authority/middleware/go.mod/go.sum输入SHA-256、11项handler当前待验源码SHA-256及后续命令见忽略目录.local/rebuild-wave4/wp03-handler-consumers.json。前述12项输入在本轮前后相同，不代表并发共享依赖全部冻结；handler后续夹具修改尚待成功编译及定向/race验证。原始完整go-test日志未保留，该文件为实际工具输出整理的精简回执。另一次共享锁45秒超时未启动编译。所有本任务临时目录已移除；共享Go缓存末次7504532KiB，小于8388608KiB上限，未清理他人缓存。无Docker/前端构建，无暂存、提交或推送；WP-03/CT-05及整个重构均不据此标完成。

## 2026-10-26 实际 Native Finalizer 及本机前端制品

本阶段不是完整重构完成。原生HTTP执行、帧事实和真实写出/flush已有独立回执；MAIN已接入实际 RecordNativeOperationUsage，复用原 prepareOpenAIUsage、buildUsageBillingCommand 与同一个 Journal/PG 结算，不调用第二次 Apply。原 RecordUsage 仍使用同一准备结果；成功定价与待定价都捕获原 openAIPricingRequest，原始计数与账务去缓存桶分开。真实事务结果继续触发既有 quota/cache/通知；Unknown/Partial只保留真实证据，不制造零token日志。简单模式保留原非计费用量行为。

实际service包定向race：280个测试/子项通过，0失败/跳过，未使用overlay；此次MAIN相关输入未变化，精确命令及范围见.local/rebuild-wave4/native-billing-adapter.json。账务owner此前真实PG用例证明Journal内部批量、幂等/回放和缓存计数映射；实际应用adapter到PG的新增串联验证正在补，不能把两个独立回执自动当成端到端已通过。

已核对输入SHA并接入WS共享purpose、root注册和handler首轮/后续轮ctx补丁。共享Dispatcher/SendPermit定向race23项通过；WS owner既有31项消费者回执仍只证明对应源快照，剩余统一事实/终态及旧恢复分支继续收口。保持每个客户端轮次原budget，内部generate:false只使用已核准的准备目的。

前端1948文件冻结集合d4d7bfae71c4ccbca8d36302cf9d709c1bd06a1544e8ff95579d6315be7809f5已逐项核对。主站、供应商UI镜像构建并运行，实际来源哈希90c927992b545676e5080c936125a3c474598493999877f2615c3e282c10e539。独立Chrome Profile 9只信任本任务公共CA，正确HTTPS来源可读且显示安全，错误主机名被拒绝；macOS系统信任仍拒绝该CA。当前API镜像仍为早期162a697a6711来源，不代表最新后端。

本机iframe fixture已改到独立32183端口，只允许32181作为frame ancestor；主站DENY安全头未放宽。此为合成数据准备，不是页面用例执行。752项前端完整验收和整个系统验收仍not_run。未暂存、提交、推送、发版或生产迁移。

### 同阶段生产编译边界

实际Wire、server production build和全部136后端包编译均exit0；覆盖2176输入文件。但并行6个WS源文件变化导致整体回执compiled_but_inputs_drifted/exit1，不能记为最终冻结编译通过，也不为相同已知漂移反复重跑。精确回执.local/rebuild-host-owner/production-wire-20261026-081000/receipt.json。共享Go缓存实测9,373,972 KiB，Docker构建缓存12.7GB，超过8GiB/5GiB约束；未证明独占归属前未全局清理，资源收尾仍未完成。专用测试环境/镜像/浏览器配置与必要证据继续保留。

### Native应用到账务PG串联补证

公开OpenAIGatewayService.RecordNativeOperationUsage经原builder及真实UsageBillingRepository进入隔离PG：2个顶层/4个含子项race通过，0失败/跳过，Go1.27.3，31.278s。核验raw1000/cache100+200→normalized700、四并发首次调用及重放只有一次余额影响/usage/dedup，原request/key和V1保留；Unknown/Partial无金额影响且不生成零token日志。见.local/rebuild-billing-owner/native-public-adapter-pg.json。相关13个账务输入不变，但并行4个WS文件变化；不能据此认定整个后端freeze。临时PG/凭据/目录已清理。
