# 代码质量优化落地与验收（2026-10-19）

依据 CODE_QUALITY_REVIEW_2026-10-19.md 的 R1–R8。归属为共享能力；保留工作区已有协议、SSE、WS 并行修改。本轮没有提交、推送、发版、生产部署或生产数据库迁移。

## 落地结果

| 项目 | 实现与验收范围 |
| --- | --- |
| R1 | 10 个入口文件的 12 处渠道计费字段改为提交前快照。回归覆盖 worker 阻塞后 Gin Context 从 model-A 复用于 model-B。 |
| R2 | usage_billing_events 保存版本化、无凭据/无提示词的计费事实。先持久化再扣费；既有去重事务同时更新余额、订阅、Key/账号/平台额度与用量行。DB 失败保留事件；缓存失效失败保持 settled，确认后才清除 payload。覆盖重启、租约过期、重复投递与缺价修复后的恢复。 |
| R3 | 文本请求检查价格存在性，独立于是否启用余额预留；显式零价有效。已执行请求缺价/计价失败时进入 pending_pricing。恢复复用原计价函数，冻结倍率、请求时刻、Fast/长上下文规则和计量；首次成功修复写回后不再定价。CN/OpenCode 的 Claude 名称仍要求显式价。Live 标记 unmetered，保留开关、并发及最大时长，没有虚构收费规则。页面及 CSV/XLSX 可见状态；历史数据保持 unknown。 |
| R4 | 资金任务在请求结束前进行首次结算，不走 drop/sample。同步与恢复任务共享令牌；生产预算为数据库最大连接数的一半（至少 1）。DB 饱和或等待增长时不扩 worker。统计补充同步任务、执行中数量和累计等待时间。 |
| R5 | 复核后选择更简单的替代：平台额度纳入既有结算事务；删除 Redis 脏集合到绝对快照覆盖的 flusher、端口、Lua 和专用测试。管理员重置与增量结算由 PostgreSQL 行锁串行化。旧 flusher 开启配置明确拒绝启动，要求先排空旧写入。 |
| R6 | server.read_body_timeout 默认 300 秒，0 禁用，范围 0–3600。只约束请求体读取，EOF/Close 清除阶段 deadline；没有给 SSE/WS 加全局写超时。真实 socket 测试覆盖慢上传及较长响应。 |
| R7 | 用户/管理员用量页接入游标历史，支持 created_at/model/id 与 asc/desc，以 id 打破同值排序。校验筛选和排序；精确 COUNT 按需请求；旧 page 接口最多跳过 10000 行。非精确总数显示下界。真实 PostgreSQL 验收排序及筛选变化。 |
| R8 | 删除无导入调用的 @lobehub/icons 和无效 @types/dompurify，保留源码 SVG。升级 DOMPurify；SheetJS 使用官方 CDN 0.20.3 tarball，由锁文件固定完整性。pnpm 固定为 Dockerfile 已用的 9.15.9。手动门禁增加 vet、关键 race、隔离集成和依赖审计，仍仅 workflow_dispatch。 |

## 新增结构与不变量

- 闭包不能跨进程恢复，因此增加 1 张事件表；复用原去重事务，不新造收费模型。
- 零金额不能表达未知，因此 usage_logs 增加 billing_status；不猜测历史零费用。
- OFFSET 不能表达下一页边界，因此既有接口增加 cursor 及 next_cursor/has_more/total_exact；新增 HTTP 路由 0 个。
- 新增 1 项请求体超时配置；移除旧 flusher 的 3 项配置字段。
- 新增生产机制 4 类：持久恢复（含缺价修复和缓存确认）、定价预检、游标查询、请求体阶段超时。平台额度替换旧路径，没有新增收费产品。
- 同一 request_id/API Key 只扣费一次；重复投递不覆盖已结算金额。用量行失败时余额、额度、去重一起回滚，事件仍可重放。
- BPS/native 拆分只写原拆分行，不额外写 aggregate，费用分摊守恒。缓存副作用采用失效操作，不重放累加。
- 事件格式 version=1；未知格式不扣款。确认后清除 payload，30 天后分批清理 tombstone；权威幂等由既有 dedup/archive 维护。
- Ent/Wire 重新生成；手写 INSERT/扫描的列数、顺序、占位符有回归。
- 二次复核保留原 user/key/account 锁顺序并将平台配额置后，修正了恢复钩子装配、缺价时 Fast 标准价回退、CN 空候选恢复、拆分行重复写和价格冻结等边界；中间失败未计为通过。

## 升级顺序（生产未执行）

迁移：backend/migrations/281_usage_billing_events.sql。按原部署流程先迁移后启动，新代码不可连接未迁移数据库。

若旧版启用了 database.user_platform_quota_flusher_enabled=true：

1. 停止入口流量及管理员配额修改，保持旧服务/flusher 运行。
2. 等待旧批次完成；确认 billing:upq:dirty 多个完整刷写周期为空且无 flush 错误。SPOP 后集合瞬时为空不代表在途批次已完成。
3. 对账 Redis 活跃快照与 DB 窗口/用量；历史丢标记或窗口不一致先人工核对，禁止用旧快照覆盖管理员重置。
4. 平稳关闭旧实例、移除 flusher 开启项，再按授权部署。禁止混跑新事务与旧绝对快照 flusher。

默认关闭旧 flusher 的部署无需排空该队列。迁移已在隔离 PostgreSQL 测试容器执行，未连接生产库。

## 运维观察

以下 SQL 只读，不触发扣费：

    SELECT status, count(*) AS events, min(created_at) AS oldest,
           max(attempts) AS max_attempts
    FROM usage_billing_events WHERE status <> 'applied' GROUP BY status;

    SELECT billing_status, count(*) AS requests, sum(actual_cost) AS recorded_cost
    FROM usage_logs WHERE created_at >= now() - interval '1 day'
    GROUP BY billing_status;

pending_pricing：修复模型/分组/渠道价格；failed：检查 last_error；settled 长期积压：检查 Redis/API Key 缓存失效及确认写入。不要直接设置 applied 跳过恢复。

## 保证边界

- 持久恢复从事件成功落库开始。首次落库前崩溃或 DB 持续不可写，不能凭内存实现端到端零丢单；首次写入有界重试并明确报错，不声称上游执行与本地 DB 是同一事务。
- 未做生产压测、真实大客户数据 EXPLAIN ANALYZE 或供应商账单对账，不声称费用/延迟降低倍数，未凭猜测加用量查询索引/分区。
- Live 仍未计量，不等于已知免费；原时长/并发限制保留。
- 本机 golangci-lint 的 Go 导出格式兼容问题未通过修改生产逻辑规避；使用 build、vet、unit、race、integration 和前端门禁验收。

## 验收记录

- 全量后端 unit：61 个有测试包通过，退出码 0；随后仓储锁顺序/游标语义调整又经过该包全部 unit + race 复核。
- 隔离 PostgreSQL/Redis integration：17 项通过，退出码 0。真实迁移、租约到期、落库后未扣费/提交后未确认的故障断点模拟、重复投递、价格修复冻结、配额重置和排序均覆盖；没有宣称执行了真实生产进程 SIGKILL。
- 关键 service/handler/repository/server race 检测通过；最后仓储包完整 race 复核通过。
- 后端 build、Go vet、Ent/Wire 生成通过。
- 前端全量：419 个测试文件、3441 项测试通过；最终导出/UI/i18n 专项 153 项通过。
- 前端 typecheck、lint:check、生产 build、语言完整性/繁体生成检查通过。构建仍有既存大 chunk 提示，未将其描述成构建失败或未经测量的性能收益。
- npm 官方端点生产依赖审计：19 → 0 项告警。安装器仍提示部分有效依赖的主版本生命周期（如 vue-i18n/ESLint）；未将有效依赖当死代码删除，也未混入破坏性主版本迁移。
- git diff --check 与相关 Go 文件 gofmt 检查通过。所有完成检查退出码均为 0；中间失败与最终日志分别保存，没有隐藏失败。

### 增删统计

按质量变更相关文件的当前 Git diff 统计，包含此前已验证的 14 个无调用函数清理；排除工作区独立的协议/SSE/WS 和 Grok 配额测试并行改动。不是对整个工作区提交归属的判断；详细文件清单位于本机验收目录 quality-files.json。

| 分类 | 新增行 | 删除行 |
| --- | ---: | ---: |
| 业务代码/数据库定义 | 1223 | 1189 |
| 测试代码 | 551 | 1121 |
| Ent/Wire 生成代码 | 318 | 74 |
| 依赖锁文件 | 189 | 5659 |

新增 HTTP 路由 0；新增命名内部恢复端口 1（装配/恢复两个方法），两种网关各增加一个预检方法；新增配置 1、移除配置字段 3；新增事件表 1、用量状态列 1；新增生产机制 4 类，替换旧配额快照路径。

本机详细证据：/tmp/sub2api-quality-implementation-*；文件清单和起始工作区快照：/var/folders/mz/2z2mk7m12dd2pj_jqsz8gd0h0000gp/T/sub2api-quality-implementation-20261019-isk16z8f。这些日志没有作为生成文件写进仓库。
