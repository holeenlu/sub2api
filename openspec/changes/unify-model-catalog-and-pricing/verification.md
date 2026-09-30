# 实现与验证记录

日期：2026-09-30。工作分支 `codex/unify-model-catalog`；实现基线 `c612f97b3`，已普通合并 `main` 的 `94a7ad031`，保留模型别名和安全修复。品牌公共能力，无品牌名称、域名或部署配置替换。

## 实现对应

| 设计范围 | 实现入口 |
| --- | --- |
| 持久快照、原子指针、共享租约、版本恢复 | `backend/migrations/261_model_catalog_registry.sql`、`internal/repository/model_catalog_*.go` |
| 分页、能力及媒体部分事实 | `internal/service/upstream_models.go`、`model_catalog_discovery.go`、`model_catalog_evidence.go` |
| 自动同步、失败退避、来源范围、配置热更新 | `model_catalog_service.go`、`model_catalog_retry.go`、`model_catalog_oauth_scope.go` |
| 原生列表与图片未列出/历史调用证据区分 | `model_catalog_media.go`、账号目录中的 `access`、`observations` 和 `endpoints` |
| 账号和分组固定/跟随/排除、兼容旧规则 | `model_catalog_policy.go`、`group_model_allowlist.go`、`group_model_catalog.go` |
| 五页面、账号测试/批量、票据候选 | 管理 handler、`ModelWhitelistSelector.vue`、`ModelCatalogView.vue`、`CodexTicketDashboard.vue` |
| 普通目录、Codex、Key 默认配置 | `model_catalog_codex.go`、`api_key_handler.go`、`UseKeyModal.vue` |
| 原始参考价格、销售覆盖、计费冻结、未知维度待核算 | `model_catalog_prices.go`、`model_catalog_request_pricing.go`、`model_catalog_usage_readiness.go`、现有价格解析器 |
| 解释/旧新差异及部署开关 | `/admin/model-catalog/explain`、`model_catalog_explain.go`、分组 `model_allowlist.mode` |
| 本机目录更新 | `frontend/public/install/update-codex-models.py`、`docs/MODEL_CATALOG.md` |

直接读取 PostgreSQL 发布指针保证实例间一致；请求内复用读取结果，账号策略与渠道变化继续使用原 outbox/缓存失效机制。没有引入第二套独立 Redis 目录缓存。目录、价格数据均可更新而无需构建程序。

## 协议及硬编码审查

旧平台数组退出账号选择器、批量填充和创建账号自动勾选路径。`useModelWhitelist.ts` 的历史资料仍保留给兼容代码，不能作为失败回退或“最新模型”。模型价格只参与报价，不产生账号支持资格。

新模式保留模型原始 ID（含大小写），允许已支持协议中的新 ID 通过数据发布。模型类型、模态和推理字段取原生/补充资料；Codex 只输出合同完整的会话模型，媒体单独处理。原生合同/指令在实际路线一致时保留；缺少专用指令时使用兼容编码指令，不冒充旧 GPT 型号。

名称别名、已知型号的价格/协议特殊规则仍保留作旧数据兼容，不作为未知新型号的通用猜测。新模式不借相似型号的价格；固定模式不自动扩权。`RestrictModels` 作为现有权限限制保留。对同等推荐优先级，Key setup profile 保留该组已保存的默认选择；新数据明确推荐更高优先级时更新。

公开模型发现取可服务账号并集；Codex 能力取实际候选路线交集。请求准入固定可选路线与价格版本，调度过滤不匹配账号。WebSocket 每轮重新准入，轮内重试保留价卡。未知响应模型或实际使用了未定价维度时写待核算证据，不回放生成，也不记录成免费结算。

## 自动化证据

- 全量前端：3036 项通过，涵盖四语言键、台湾本地化、账号/分组/密钥及票据页面。类型检查和 Vite 生产构建通过。构建仍有仓库现有的大 chunk 提示。
- 全量服务包回归通过，输出包含 16985 个通过事件（含子测试）；其余受影响仓储、handler、admin、middleware、routes、server 包通过。
- 新增和后续边界修改再跑定向回归；目录、报价和持久仓储的定向 race 检查通过。
- 使用仅绑定 `127.0.0.1` 的临时 PostgreSQL 18 验证迁移重复执行、两个独立连接抢租约、版本可重读、失败保留/退避、空目录撤销、凭据隔离、回滚防复权、媒体证据和跨实例任务读取。没有连接业务数据库。
- Python 更新器：6 项测试通过，包括当前模型撤销、错误内容、无权限更换、原子替换失败保留文件、读取现有 API Key 方式和不修改配置/认证。

主要场景测试：

| 场景 | 证据 |
| --- | --- |
| 无编译枚举的新型号 | `TestModelCatalogNovelModelDiscoveryPublicationForwardAndSettlement` 在运行时生成型号，验证发现、目录、配置、流式/非流式出站与结算 |
| 数据变化不中途重定价 | 同一测试保留进行中请求旧价，后续请求使用新价 |
| 新 ID、图片部分事实、停服、分页、合法空列表、重启 | `model_catalog_service_test.go` |
| 跟随仍保留别名、真凭据切换失效、OAuth 例行刷新不误清空 | `model_catalog_policy_regression_test.go` |
| 供给方范围、显式 retired 热更新、零价与价格缺项 | `model_catalog_policy_regression_test.go` |
| 跨实例发布与回滚 | `TestModelCatalogPostgresPublication` |
| 新候选/停服不依赖前端构建、刷新不改选择、账号切换竞态 | `ModelWhitelistSelector.catalog.spec.ts` 及现有组件测试 |
| 个人/媒体倍率、阶梯、分时与多路由 | 保留并通过 `model_quote_test.go`、`billing_context_schedule_test.go` 和分组目录/计费回归 |

## 尚需外部环境完成

生产数据库迁移、真实内部组/品牌组推广、实际订阅账号权限和付费调用未执行。本机 mock 及临时数据库测试不能证明上游账号已获得某模型权限。

更新器提供 macOS/Linux/Windows 命令，当前执行的是 macOS Python 自动化测试。原生 Windows/Linux 和指定 Codex Desktop/CLI 版本的完整实机验收保留在 tasks.md，不将缺少该环境写成已验证。新增协议/认证/计量单位仍需协议适配。
