# 持久账务与旧配额刷写器升级操作

本固定路径由配置错误消息引用，保留为当前操作附录。需求与架构见 [R12](ENGINEERING.md#req-billing) 和 [R19](ENGINEERING.md#req-data)。本轮仅核对文档，原日期版测试报告不作为新验收。

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

