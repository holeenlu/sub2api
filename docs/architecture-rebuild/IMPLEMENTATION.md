# 独立重构实施记录

日期：2026-10-25。分支codex/sub2api-rebuild；起点6f48d6ab6fcc29763af84410690530aeaa957403。原维护main、TapModels、tokensavy不在写入或推送范围。工程契约0.3；前端实现由其隔离会话按普通增量接入共同重构分支。

## 当前完成范围

| 切片 | 实际结果 | 尚未代表完成的部分 |
| --- | --- | --- |
| WP-01-BUDGET | ResourceOwner/AttemptTarget纯值；execution独占总尝试、physical owner、目标、准备次数和累计等待计数；已有Native/BPS/Cloud消费方接入 | 全Operation状态机、SendPermit及完整执行引擎 |
| WP-02-SSE | 纯SSE分行器与测试迁至protocol/sse，服务/BPS接入，旧pkg/sse删除 | 其余协议、provider及会话迁移 |
| WP-27-API | 唯一根Dockerfile，deploy/api独立PG/Redis/API模板、镜像/项目/可选容器名配置 | 完整UI角色集成及真实上游验收 |
| WP-27-IDENTITY | projectinfo集中技术名；站点/日志默认sub2api；显式网站/日志设置保留 | 前端品牌与资产治理、历史部署迁移 |
| WP-24-UPDATE | 删除旧下载/校验/解压/二进制替换/备份回滚、Compose socket、缓存及三个失效API；删除旧UpdateService文件，保留纯BuildStatus和独立重启 | 新的制品发布自动化没有开发或宣称完成 |
| WP-24-PACKAGING | 删除旧品牌发版矩阵、GoReleaser、安装器、多套旧主站Compose；删除门户updater代理和品牌网络覆盖 | 门户完整新UI由前端会话验收 |
| WP-24-DOCS | 替换旧项目入口/交付规则、清理无引用旧Logo及推广图片、消除维护品牌推送选择器与当前资料断链 | 历史输入资料仍保留作研究/许可证据，不当作当前运行清单 |

本轮未新增网关生成入口或持久化schema。预算保留原8次上限/配置收紧、owner最多3次、目标1或2次、准备2次、累计等待10秒及正向抖动；权限、取消、首输出deadline和模式恢复仍按既有真实调用链处理。没有第二套计数实现。

## 已删除与有依据保留

旧自更新不是放在死开关后：releaseSourceTrusted、下载/替换/回滚方法、update_check_cache、UPDATE_CHECK_ENABLED及专属路由/权限项一并移除。只读构建状态使用无I/O的Status值返回，不保留无用force/context参数或不可能的错误分支。HTTP读合同供当前前端消费者显示版本和手动部署方式；写接口删除清单见FRONTEND_HANDOFF。

GitHub latest/recent读取继续供Claude Code和Codex版本同步；同一代理配置也供远程价格读取。保留这些实际消费者、鉴权/重定向/出口保护，不把它们按update名称误删。供应商门户的静态站点/同源代理、许可、SDK、法律文档、历史SQL和未被接管的业务模块各有当前用途。

门户的旧updater/socket专属代理已删除；Nginx真实配置检查及两个纯代理替身测试通过（不是前端应用构建或真实UI联调）；其独立Dockerfile补充自己的构建上下文，避免根API-only忽略规则破坏有效门户构建。根assets的旧Logo、wordmark和两张partners推广图片无消费者，已删除；frontend目录未由本任务修改。

## 验证证据

- Go 1.27.3新核心-race通过；预算/SSE源码后续未改变，621个服务测试/子测试的有效回归结果复用。基线中唯一8 MiB陈旧断言改为真实32 MiB上限及零交付断言后，定向复测通过，没有修改运行上限。
- 站点显式配置、项目配置/日志、BPS SSE消费、Go vet、依赖边界和本机独立二进制已验证。完整golangci-lint因本机工具不能解码Go 1.27.3 export data v5而未通过；未伪称全部lint完成。
- 清理后版本同步、GitHub读取鉴权/代理、构建状态、权限目录和中间件284个测试/子测试通过。处理器/路由编译发现的一个无用import已删除，两个受影响包定向复测通过。
- Wire通过真实生成器重新生成，使用任务临时modfile承载工具依赖，应用go.mod/go.sum未修改；不以手改生成代码掩盖类型问题。
- 交付工具19项测试通过；API部署权限/密钥/卷隔离/命名/门户接线8项测试通过，资源与Caddy检查通过。部署worker只完成部分清理，剩余测试及代理移除由主代理接手完成，未把未运行检查记为worker成功。
- 第一波Linux arm64镜像实际构建、UID1000、PG客户端、隔离初始化、健康、公开设置、登录和重启持久化已通过。清理后唯一根Dockerfile已重新构建，独立新数据库初始化、健康、公开设置、登录、重启持久化通过；三个退役接口实测404。最后默认值调整的镜像/原生编译结果继续按当前源码记录。
- 管理员check-updates实际路由受既有423合规确认保护；没有代用户确认。纯只读状态由服务/处理器测试证明。真实上游、生产、Linux amd64和完整UI均未验证。

## 并行与交接

第一波由3个实现worker和1个只读审查worker协作；追加清理由2个worker分离写集，主代理整合共享配置、权限、Wire、文档及未完成部署验证。所有worker均已结束，Git索引和发布由主代理单写。

前端会话报告327项定向测试、类型/lint和主站/vendor构建通过，但仍在独立工作树，未由后端自动收集或合入。后端提交后在FRONTEND_HANDOFF登记准确HEAD，供其自行核对增量。此处不宣称前后端完整联调。

## 后续开发

继续补齐access/session/billing/catalog端口与统一执行引擎，再完成原生完整切片及BPS/Cloud/其余平台迁移。每个切片同步删除替代代码、旧配置和测试；不把无用旧项目文件保留到最后。当前仅上述切片完成，28项父工作包未全部验收，implementation_ready及rollout_ready仍为false。
