# 旧实现迁移与退出登记

这是完整迁移的退出清单；逐项状态以work-items.json和本页实际退出记录为准，不能把计划当作已删除。WP-24核验调用方、配置、文档和测试是否同时收敛。生产数据清理与代码删除分别处理。

| ID | 当前功能与参考路径 | 唯一目标责任 | 集成与退出责任 | 完成证据 |
| --- | --- | --- | --- | --- |
| CLN-01 | handler/openai_gateway_handler.go的执行循环 | execution + ingress | WP-10 | HTTP/SSE/WS及工具后续轮走一个执行者，旧注册/循环/键删除 |
| CLN-02 | handler/failover_loop.go、service/gateway_turn_recovery.go | execution | WP-10/18/24 | 各模式预算和恢复动作统一，每次生成attempt可查，无隐藏第二计数器 |
| CLN-03 | OpenAITurnBudget中的账号/BPS/健康对象耦合 | 预算/session/模式状态/归因分别持有 | WP-10/18 | 被替代字段、context键、helper和失效测试清除，配置预算语义不变 |
| CLN-04 | openai_gateway_forward.go的模式/平台路由分支 | catalog计划 + providers | WP-10/18/19 | 真实转发能力迁入，模式边受授权/恢复合同限制，无service第二路由器 |
| CLN-05 | BPS历史/工具/图片与共享控制混合 | protocol + BPS adapter | WP-18 | Excel-only、403、图片阶段与工具身份保留，重复授权/调度/账务外壳清理 |
| CLN-06 | Cloud parser/actor/transport与控制交叉 | 最新parser适配 + session + host | WP-18/23 | 不恢复旧parser，thread/turn/owner/usage ACK单一权威，旧控制链删除 |
| CLN-07 | 各provider handler/service局部执行链 | 对应adapter + operation端口 | WP-19 | 清单中每个平台和媒体/实时操作有唯一处理者，旧生命周期循环删除 |
| CLN-08 | Group/Account多处能力/价格/范围解释 | access/catalog/billing领域视图 | WP-20/23 | 字段解释者唯一；外部API与持久化读者仍有追踪，临时编译wrapper有退出点 |
| CLN-09 | 多条使用记录/补扣与worker包装 | billing用例 + host恢复 | WP-10/18/23 | 保留PG幂等事务和首次执行保证，内存恢复不成为唯一账本 |
| CLN-10 | 构造器隐式Start和重复provider sets | host显式生命周期 | WP-23 | 顺序启动关闭、有界drain、错误和资源归属明确，废弃装配删除 |
| CLN-11 | 巨型设置/账号页旧状态、文案、API | frontend领域features | WP-21/24 | 服务端权威校验，旧props/store/locale/路由/API清理，品牌差异保留 |
| CLN-12 | 仅服务旧路径的配置、导入、历史提示和专属测试 | 对应领域或删除 | WP-24 | 新旧名、动态注册、读写、未跟踪源码一起查；保留项有具体调用方和退出条件 |
| CLN-13 | 品牌编译fallback、镜像/容器硬编码、上游广告/赞助及其专属实现 | 中性项目身份 + 部署配置 + 现有网站设置 | WP-20/21/23/24/27 | 同角色制品跨品牌配置；旧有效设置/挂载不丢，广告路径和资产退出，许可证/必要署名保留 |

## 每条退休记录的必填项

当前入口与调用链、替代入口、外部合同/REQ、数据字段与读者、旧文件/符号/配置/文案、删除或保留原因、验证命令与实际结果、尚未清理的数据、回滚条件。

不能仅隐藏页面、停一次调用、换名称或加兼容开关认定退出完成；共享能力仍被其他功能使用则保留其共享部分。不能为了删除旧文件而回退其他任务的改动。

真实业务只允许一个写路径；纯计划/协议转换可离线比较。旧路径只在明确的切片迁移窗口内暂存，随该切片集成验收删除，不在全部重构结束后统一拖欠。

## 2026-10-25 用户明确的清理要求

独立重构项目不保留无用旧文件和代码。第一波已删除旧SSE包和service重复预算字段；追加WP-24-UPDATE、WP-24-PACKAGING、WP-24-DOCS移除旧自更新/回滚、品牌打包/发版/安装及过时项目入口。删除以实际diff为准，未完成前不标为全量清理通过。

仍服务现有版本同步的GitHub读取设施、远程价格配置、供应商门户、法律文档、SDK、历史迁移以及尚未被新领域替代的业务模块各有真实消费者；保留的原因是当前用途，不是未来假设。前端无用页面/资产/依赖的清理由其独立会话负责，合入时按同一退出条件审查。

旧API镜像中的Go字串构建时改写链已退出，连同convert-go、Go扫描器及9个仅服务该链的脚本删除；前端实际使用的convert/gen-locale/audit-locale/audit-ui和依赖继续保留。API文字按源码提供，不再存在ZH_TW镜像分叉。

## 2026-10-27 SystemOne 与重复构建入口退出

- **SystemOne：**`GatewayHandler.SystemOne` 仅建立可信入口/容量并调用 `ExecuteSystemOne`；删除 handler 内独立 failover loop、`recordSystemOneUsage` 的异步闭包提交、`SystemOneExecutionReady` 旧链回退与 `service/gateway_systemone.go`（含 `ForwardSystemOne`、`SystemOneForwardResult`）。HTTP状态政策函数由实际新dispatch消费，`pkg/typesafe` 解码器继续供审核和账号探测使用，未误删这些消费者。原有效测试改测新执行链，344冻结源码测试事件、六组PG/race及本机实际HTTP/PG known/unknown用量验证见TEST_REPORT。没有删除历史数据库或缓存数据。
- **重复API构建：**删除没有工程消费者的 `backend/Dockerfile` 及其 `.dockerignore`。README、deploy/README与Compose均指向根Dockerfile，backend/Makefile没有Docker构建调用；Go本地build及resolve-version.sh仍有消费者，继续保留。部署静态7项通过，根Dockerfile与实际运行制品不受这次删除影响。此为删除未使用入口，未重建同一应用制品。
- **BPS范围说明：**修正批量设置helper中“nil代表全部模型”的旧注释；运行中的missing/empty scope仍仅表示没有选中BPS模型，不恢复旧隐式全选语义。

以上是具体CLN-07/09/12切片，不代表CLN-01至13全量已退休；其他平台实际旧执行者仍按工作包逐项接管。


## 2026-10-28 CPR确定替代路径

- 删除Native发送前独立wire-policy账号重复读取；政策消费同次authority读取，保留每次发送及容量/父凭据必要重验。
- 无条件cache.set由唯一generation条件publish接管，Native/BPS真实目录消费者完成接线，不增第二目录。
- 等待循环每次新token抢槽由Redis ticket接管；删除无调用者AcquireAccountSlotWithWait、waitForSlotWithPing。匿名计数保留实际policy guard、调度load、active-index读者。
- 调度截断与affinity状态分离；trace.escaped运行读者全部迁移，未丢失亲和逃逸计量。
- 计费模型事实不再信任GetBody；实际正文有界读取并生成独立replay reader。WS复用不再只按header/tenant匹配。

无历史数据库清理、已应用迁移改写或长期双执行器。证据见CPR_INTEGRATION。
