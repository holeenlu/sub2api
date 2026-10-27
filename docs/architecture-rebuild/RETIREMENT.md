# 旧实现迁移与退出登记

这是开发前的退休计划，全部状态为planned；不能据此宣称代码已删除。逐条实施证据记录到本表指定条目或实际retirement-record.md，由WP-24核验。生产数据清理与代码删除分别处理。

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
| CLN-11 | 巨型设置/账号页旧状态、文案、API | FRONTEND_ARCHITECTURE三层及14领域 | WP-28至35/21/24 | 服务端权威校验；全局桶/重复缓存/旧props/store/locale/页面/API收敛，正式URL与品牌设置保留 |
| CLN-12 | 仅服务旧路径的配置、导入、历史提示和专属测试 | 对应领域或删除 | WP-24 | 新旧名、动态注册、读写、未跟踪源码一起查；保留项有具体调用方和退出条件 |
| CLN-13 | 品牌编译fallback、镜像/容器硬编码、上游广告/赞助及其专属实现 | 中性项目身份 + 部署配置 + 现有网站设置 | WP-20/21/23/24/27 | 同角色制品跨品牌配置；旧有效设置/挂载不丢，广告路径和资产退出，许可证/必要署名保留 |

## 每条退休记录的必填项

当前入口与调用链、替代入口、外部合同/REQ、数据字段与读者、旧文件/符号/配置/文案、删除或保留原因、验证命令与实际结果、尚未清理的数据、回滚条件。

不能仅隐藏页面、停一次调用、换名称或加兼容开关认定退出完成；共享能力仍被其他功能使用则保留其共享部分。不能为了删除旧文件而回退其他任务的改动。

真实业务只允许一个写路径；纯计划/协议转换可离线比较。旧路径只在明确的切片迁移窗口内暂存，随该切片集成验收删除，不在全部重构结束后统一拖欠。
