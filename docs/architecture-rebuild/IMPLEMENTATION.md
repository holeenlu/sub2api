# 独立重构实施记录

日期：2026-10-25。分支codex/sub2api-rebuild；起点6f48d6ab6fcc29763af84410690530aeaa957403。原维护main、TapModels、tokensavy不在写入或推送范围。工程契约0.4；前端实现由其隔离会话按普通增量接入共同重构分支。

## 当前完成范围

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

## 最终整合核验

已审查并整合前端提交，frontend源码树保持其365项已通过验证输入不变；相同backend JSON、法律输入和依赖清单核对一致。集成后删除最后一个无消费者check-updates读接口、升级器响应头和安装状态字段，服务/处理器/路由定向回归通过。最终源码镜像重新构建并实际启动，健康/中性公开设置通过，四个退役接口均实测404；测试容器、卷、网络和镜像均已按本任务归属移除。

后端代码检查点5aaf015b6，前端整合检查点188533162；后续集成收敛提交采用普通历史，不移动维护三品牌。详细SHA、最终资源实测与保留证据以最终交接回执为准。

## 第二批后端领域接线

共同分支继续实施完整36项计划，未将第一波当作整体交付。前端owner及其两个worker正在共同工作树内处理identity/keys和shared HTTP；其未交接差异未纳入后端提交。

- WP-08-FIRST-OUTPUT：首输出绝对截止、自动/显式policy、准备暂停、交付标记及唤醒归execution.FirstOutput独占；HTTP/WS/BPS/Cloud、调度重试与观测消费者接入，旧六项状态字段删除。完整Execute、SendPermit及跨端口编排仍未完成。
- WP-05-EXECUTION-SCOPE：protocol/codex只解码，session域解释thread/kind/subagent/connection-stream身份；Gin边界继续使用现有显式session解析与哈希。保持线程优先、memory/guardian隔离、多路复用与原nil边界，不将客户端声明当权限。
- WP-06-RESERVATION-LIFETIME：billing/reservation拥有预留引用、一次释放、显式续期、取消和join；服务/Cloud恢复/Redis消费者接入，旧struct和三个旧缓存接口删除。四段Redis Lua和估价部分逐字保持，不新增资金策略、schema或兼容alias。
- WP-24-REDEEM-TIMING：删除已退役用户兑换码路径的Server-Timing许可，添加root/子路径负例；audit测试改用有效accounts/batch-refresh。Claude reset-credits独立兑换保护测试通过；邀请和余额历史仍读取旧数据，保留相关模型/历史迁移。

验证采用实际影响范围：execution race通过；session三个测试及41个子场景通过；reservation 11个测试及2个子用例race通过。主代理跨service/handler/repository/middleware/routes整合回归，修复遗漏的WS旧字段引用与测试时钟夹具后，累计675个唯一测试/子场景均通过。测试时钟修复只影响夹具与基准断言，没有更改运行超时策略；有效通过项复用，修正后定向覆盖所有受影响用例。新领域depguard及Go vet通过，当前源码CGO_ENABLED=0独立API二进制编译通过（37.072秒）；临时二进制已删除。Redis相关本轮为现有miniredis测试，不冒充真实Redis/上游验收。

本轮没有新增对外API、运行配置或平行执行路径。父WP-05/06/08仍in_progress；全量状态机、发送前授权/租约合同、完整账务与provider迁移、前端全领域和真实UI/上游验收继续保留为未完成。完整golangci工具的Go export-data兼容限制仍未解除，不能将depguard/vet通过表述为全量lint通过。

后端两个领域worker和主代理分离写集；资源worker仅处理第一波确证归属的临时文件/缓存。共享锁被另一维护任务占用时按既有有界等待处理，未移除锁或打断其进程。第一波测试容器/卷/网络/镜像已删除，SDK与有效共享依赖保留；Docker构建缓存仍超5GiB，已知取消构建记录不可回收，安全清理未达容量目标。最终实测与精简本地证据登记在.local/rebuild-wave1、.local/rebuild-wave2，未提交凭据或完整日志。
