# 供应商平台已接入接口

> **2026-10-17 V3 当前规范**：[统一平台方案](UNIFIED-PLATFORM-PLAN.md)和[本轮真实验收](UNIFIED-PLATFORM-ACCEPTANCE.md)已替代下面涉及普通用户身份、邀请/无密码开户、独立审计/安全菜单和单边结算的旧决定。以下保留历史原型与历次验收证据；不代表 V3 尚未实现，也不代表已经生产部署。

> **2026-10-17 最新补充**：一级可只读直属二级账号/代理，超级管理员可查看全部；账号「删除」改为仅归档，保留数据库、账号池列表、授权关联与历史。运营表补所属供应商/真实创建来源、Token/缓存和模型成本核对。详见 [V2 业务与数据规则](HIERARCHY-DESIGN.md#6-查询运营报表与历史保留)。下文旧「删除移出列表/清空凭证」为历史实现记录，已由本规则替代。


> V3 专用层级角色和双边结算现已落地；以下为当前接口，历史日期说明只记录对应阶段的增量。

以 `backend/internal/server/routes/vendor.go` 为准；供应商范围由服务端会话派生，后台所有新增路径均需实时超级管理员权限。

累计供应商相关 method/path 共 **86** 项（供应商 52，超级管理员 34）。V3 层级结算新增 14 项，本次 OpenAI 并行授权再增加 2 项。原有 `/admin/accounts`、`/admin/proxies` 路径的供应商范围分派另计为复用，不计新增。

2026-10-17 OpenAI 并行授权增量：供应商策略增加 `openai_authorization_modes`；账号/OAuth 输入增加 `authorization_mode=native|bps` 和 `authorization_action=create|attach|reauthorize`。刷新沿用原 POST，增加 `authorization_mode` 查询参数；账号 PATCH 接收 `native_enabled`、`bps_enabled`、`bps_models` / `bps_all_models`。双授权导入导出的原行格式增加可选 `bps_credentials`，仍受归属与导出二次验证限制。

账号列表和用量查询接受 `authorization_mode` 过滤，缺省为全部。请求行返回真实 `native` / `bps` 或历史 `unknown`，聚合行可能为 `mixed`；不从当前授权状态回填历史。按模式解除授权与账号归档是两个不同操作。详情见 [授权设计](OPENAI-BPS-AUTH-DESIGN.md) 及 [实施验收](OPENAI-BPS-AUTH-ACCEPTANCE.md)。

2026-10-17 V3 新增接口（全部在现有认证、限流与实时供应商权限中）：

| 方法 | 路径 | 访问方及业务 |
| --- | --- | --- |
| GET / POST | `/api/v1/vendor/vendors` | 一级列出或原子创建直属二级 |
| GET / PATCH | `/api/v1/vendor/vendors/:id` | 一级查看或调整直属二级策略和合同；父级不可改 |
| GET / POST | `/api/v1/vendor/payables` | 一级应付列表或从有效预览创建草稿 |
| POST | `/api/v1/vendor/payables/preview` | 读取直属二级当前合同，签名预览 |
| GET | `/api/v1/vendor/payables/:id` | 本人付款边账单详情 |
| GET | `/api/v1/vendor/payables/:id/export` | 本人付款边账单导出 |
| POST | `/api/v1/vendor/payables/:id/confirm` | 确认并锁定快照 |
| POST | `/api/v1/vendor/payables/:id/void` | 作废符合条件的账单 |
| POST | `/api/v1/vendor/payables/:id/payments` | 登记外部付款，不执行转账 |
| POST | `/api/v1/vendor/payables/:id/payments/:payment_id/reverse` | 冲正该边付款 |
| POST | `/api/v1/vendor/payables/:id/adjustments` | 同一付款/收款边的调整单 |

开户 POST 必须提供 email/password/username/tier；不再接受 user_id 绑定客户。合同版本由后端生成；默认限额1000/1000、并发15、优先级1。根级仍复用 `/admin/vendors`，二级创建需明确父级。Vendor DTO 的 account_count/proxy_count 表示直接归属数量；account_tree_count/proxy_tree_count 表示含下级额度占用，避免在列表汇总中重复计数。这两个只读派生字段不新增数据库列。

一级默认以 `GET /api/v1/vendor/accounts?scope=children`、`GET /api/v1/vendor/proxies?scope=children` 查询全部直属二级资源；该范围仅允许一级供应商的列表 GET，由已认证父级决定，不能同时指定 vendor_id。账号概览、配额、实时并发和历史用量按同一直属范围计算。单个供应商筛选及资源详情继续使用 vendor_id。

超级管理员默认复用完整账号池/代理工作台，通过原 `/admin/accounts?vendor_id=0`、`/admin/proxies?vendor_id=0` 查询全部供应商；选定归属后保留原管理权限。账号和代理均返回 `vendor_id`、`vendor_name`，列表常驻“归属供应商”列。跨供应商列表不执行混合归属批量写入。

一级经资源 GET 的 vendor_id 选择直属二级。账号、代理与用量均裁掉凭证、消费者身份和上级采购合同；同级、跨根、越级或写请求拒绝。二级看不到 payables，也不能创建下级。

2026-10-17 归档语义（沿用现有路径）：

- `DELETE /vendor/accounts/:id` 及原后台对应分派仅执行归档，响应 `{"archived": true}`。重复归档幂等，首次归档时间不变；默认列表继续返回该账号，`status=archived` 可筛选。
- 账号安全 DTO 增加 `archived:boolean`、`archived_at:string|null`，原 `status` 保留健康状态。用量行、历史账号选择器和账号用量详情增加 `account_archived:boolean`；`account_deleted` 继续表示旧 tombstone/缺失账号，不混为新归档。
- 归档保留凭证、extra、分组、代理/备用出口、身份占用及历史；同归属影子账号一并停止供给，计划停用但不删除历史。归档后所有管理变更和敏感导出拒绝，锁内复核阻止在途写回；读详情/用量保留。
- 存储复用 `accounts.extra.vendor_archived_at` 与原暂停/调度标志，无新增迁移、环境配置或 HTTP 路由。V3 复用资源 GET 路径的 vendor_id 参数提供直属下级只读查询；写入、凭证导出、探测、编辑和删除均在服务端拒绝。

2026-10-16 运营报表优化沿用上述接口，不新增路径：

- `/admin/vendors/usage` 的 `dimension` 增加 `request`，只有超级管理员可查询逐请求安全记录；原五个聚合维度继续支持。
- 用量查询支持 `sort_by=cost_usd|opus_cost_usd|fable_cost_usd|requests|total_tokens|date|created_at` 和 `order=asc|desc`；`date` 只用于日期/账号日期维度，`created_at` 只用于请求维度。非法值、空值或重复排序参数返回 422。
- 聚合指标增加 Decimal 字符串 `opus_cost_usd`、`fable_cost_usd`；账号行增加 `account_status`。请求行沿用安全指标，另含 `usage_log_id`、`created_at`、`platform`、账号/供应商/分组与模型，不含消费者身份、API Key、IP 或内容。
- 非日期聚合默认成本降序，日期默认升序，请求默认 `created_at desc, usage_log_id desc`。翻页、换维度与导出透传同一个 `snapshot_at`/`watermark`，保留 0 水位语义。金额显示不能经 JavaScript Number 转换丢失精度。

2026-10-16 需求复核补充（仍无新增路径）：

- 账号用量接口增加可选 `model_page`（默认 1，1～100000），独立于日明细的 `page`。响应包含已有 `model_total` 及 `model_page`、`model_page_size=100`；分页共享原快照和权限。空值、非正整数或重复分页参数返回 422。
- 结算预览省略 `contract_version` 时，按各供应商当前合同解析后冻结；显式版本仍要求匹配。`group_coefficients` 省略表示继承各家合同，显式空对象表示清除本次预览的分组覆盖；草稿保存继续校验签名和版本，合同变化须重新预览。

| 方法 | 路径 | 访问方 |
|---|---|---|
| GET | `/api/v1/vendor/me` | 供应商 |
| GET | `/api/v1/vendor/accounts` | 供应商 |
| POST | `/api/v1/vendor/accounts` | 供应商 |
| POST | `/api/v1/vendor/accounts/import/preview` | 供应商 |
| POST | `/api/v1/vendor/accounts/import` | 供应商 |
| POST | `/api/v1/vendor/accounts/export` | 供应商 |
| GET | `/api/v1/vendor/accounts/:id` | 供应商 |
| PATCH | `/api/v1/vendor/accounts/:id` | 供应商 |
| DELETE | `/api/v1/vendor/accounts/:id` | 供应商 |
| DELETE | `/api/v1/vendor/accounts/:id/authorizations/:mode` | 资源所有者；一级不能操作二级 |
| POST | `/api/v1/vendor/accounts/:id/pause` | 供应商 |
| POST | `/api/v1/vendor/accounts/:id/resume` | 供应商 |
| POST | `/api/v1/vendor/accounts/:id/refresh` | 供应商 |
| POST | `/api/v1/vendor/accounts/:id/usage-refresh` | 供应商 |
| POST | `/api/v1/vendor/accounts/:id/probe` | 供应商 |
| POST | `/api/v1/vendor/accounts/:id/sync` | 供应商 |
| GET | `/api/v1/vendor/accounts/:id/usage` | 供应商 |
| GET | `/api/v1/vendor/proxies` | 供应商 |
| POST | `/api/v1/vendor/proxies` | 供应商 |
| POST | `/api/v1/vendor/proxies/import/preview` | 供应商 |
| POST | `/api/v1/vendor/proxies/import` | 供应商 |
| POST | `/api/v1/vendor/proxies/export` | 供应商 |
| POST | `/api/v1/vendor/proxies/batch-delete` | 供应商 |
| GET | `/api/v1/vendor/proxies/:id` | 供应商 |
| PATCH | `/api/v1/vendor/proxies/:id` | 供应商 |
| DELETE | `/api/v1/vendor/proxies/:id` | 供应商 |
| POST | `/api/v1/vendor/proxies/:id/test` | 供应商 |
| POST | `/api/v1/vendor/proxies/:id/quality-check` | 供应商 |
| GET | `/api/v1/vendor/usage/summary` | 供应商 |
| GET | `/api/v1/vendor/usage/trend` | 供应商 |
| GET | `/api/v1/vendor/usage/accounts` | 供应商 |
| GET | `/api/v1/vendor/audit-logs` | 供应商 |
| GET | `/api/v1/vendor/settlements` | 供应商 |
| GET | `/api/v1/vendor/settlements/:id` | 供应商 |
| GET | `/api/v1/vendor/settlements/:id/export` | 供应商 |
| POST | `/api/v1/vendor/oauth/:platform/sessions` | 供应商 |
| POST | `/api/v1/vendor/oauth/:platform/sessions/:session_id/complete` | 供应商 |
| PATCH | `/api/v1/vendor/oauth/:platform/sessions/:session_id/proxy` | 供应商 |
| GET | `/api/v1/admin/vendors` | 超级管理员 |
| POST | `/api/v1/admin/vendors` | 超级管理员 |
| GET | `/api/v1/admin/vendors/group-supply` | 超级管理员 |
| GET | `/api/v1/admin/vendors/audit-logs` | 超级管理员 |
| GET | `/api/v1/admin/vendors/usage` | 超级管理员 |
| POST | `/api/v1/admin/vendors/identity-reconciliation` | 超级管理员 |
| GET | `/api/v1/admin/vendors/usage/export` | 超级管理员 |
| GET | `/api/v1/admin/vendors/settlements` | 超级管理员 |
| POST | `/api/v1/admin/vendors/settlements/preview` | 超级管理员 |
| POST | `/api/v1/admin/vendors/settlements` | 超级管理员 |
| GET | `/api/v1/admin/vendors/settlements/:id` | 超级管理员 |
| POST | `/api/v1/admin/vendors/settlements/:id/confirm` | 超级管理员 |
| POST | `/api/v1/admin/vendors/settlements/:id/void` | 超级管理员 |
| POST | `/api/v1/admin/vendors/settlements/:id/payments` | 超级管理员 |
| POST | `/api/v1/admin/vendors/settlements/:id/payments/:payment_id/reverse` | 超级管理员 |
| POST | `/api/v1/admin/vendors/settlements/:id/adjustments` | 超级管理员 |
| GET | `/api/v1/admin/vendors/settlements/:id/export` | 超级管理员 |
| GET | `/api/v1/admin/vendors/:id` | 超级管理员 |
| PATCH | `/api/v1/admin/vendors/:id` | 超级管理员 |
| POST | `/api/v1/admin/accounts/import/preview` | 超级管理员 |
| POST | `/api/v1/admin/accounts/import` | 超级管理员 |
| POST | `/api/v1/admin/accounts/export` | 超级管理员 |
| DELETE | `/api/v1/admin/accounts/:id/authorizations/:mode` | 超级管理员指定供应商范围 |
| POST | `/api/v1/admin/accounts/:id/pause` | 超级管理员 |
| POST | `/api/v1/admin/accounts/:id/resume` | 超级管理员 |
| POST | `/api/v1/admin/accounts/:id/usage-refresh` | 超级管理员 |
| POST | `/api/v1/admin/accounts/:id/probe` | 超级管理员 |
| POST | `/api/v1/admin/accounts/:id/sync` | 超级管理员 |
| POST | `/api/v1/admin/proxies/import/preview` | 超级管理员 |
| POST | `/api/v1/admin/proxies/import` | 超级管理员 |
| POST | `/api/v1/admin/proxies/export` | 超级管理员 |
| POST | `/api/v1/admin/vendors/oauth/:platform/sessions` | 超级管理员 |
| POST | `/api/v1/admin/vendors/oauth/:platform/sessions/:session_id/complete` | 超级管理员 |
| PATCH | `/api/v1/admin/vendors/oauth/:platform/sessions/:session_id/proxy` | 超级管理员 |
