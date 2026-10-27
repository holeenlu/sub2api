# 供应商平台接口参考

当前接口从 backend/internal/server/routes/vendor.go 的注册及分组读取。需求、层级、权限、状态与结算统一见 [R13](../ENGINEERING.md#req-vendor)。本文是接口导航，不再保存 V1/V2/V3 中间方案。

供应商入口使用 JWT、面板限流和 Vendor.Scope；管理入口使用超管/管理范围中间件。导出、归档、授权解除及合同动作仍执行 handler/service 的独立校验。注册存在不代表任意角色可以调用。

| 方法 | 完整路径 | Handler / 关键中间件 | 源码行 |
|---|---|---|---|
| GET | `/api/v1/vendor/me` | `h.Vendor.Me` | 14 |
| GET | `/api/v1/vendor/vendors` | `h.Vendor.List` | 16 |
| POST | `/api/v1/vendor/vendors` | `h.Vendor.Create` | 17 |
| GET | `/api/v1/vendor/vendors/:id` | `h.Vendor.Get` | 18 |
| PATCH | `/api/v1/vendor/vendors/:id` | `h.Vendor.Update` | 19 |
| GET | `/api/v1/vendor/accounts` | `h.VendorAccount.List` | 21 |
| POST | `/api/v1/vendor/accounts` | `h.VendorAccount.Create` | 22 |
| POST | `/api/v1/vendor/accounts/import/preview` | `h.VendorAccount.ImportPreview` | 23 |
| POST | `/api/v1/vendor/accounts/import` | `h.VendorAccount.Import` | 24 |
| POST | `/api/v1/vendor/accounts/export` | `h.Vendor.ExportStepUp, h.VendorAccount.Export` | 25 |
| GET | `/api/v1/vendor/accounts/:id` | `h.VendorAccount.Get` | 26 |
| PATCH | `/api/v1/vendor/accounts/:id` | `h.VendorAccount.Update` | 27 |
| DELETE | `/api/v1/vendor/accounts/:id` | `h.VendorAccount.Delete` | 28 |
| DELETE | `/api/v1/vendor/accounts/:id/authorizations/:mode` | `h.VendorAccount.RevokeAuthorization` | 29 |
| POST | `/api/v1/vendor/accounts/:id/pause` | `h.VendorAccount.Pause` | 30 |
| POST | `/api/v1/vendor/accounts/:id/resume` | `h.VendorAccount.Resume` | 31 |
| POST | `/api/v1/vendor/accounts/:id/refresh` | `h.VendorAccount.Refresh` | 32 |
| POST | `/api/v1/vendor/accounts/:id/usage-refresh` | `h.VendorAccount.UsageRefresh` | 33 |
| POST | `/api/v1/vendor/accounts/:id/probe` | `h.VendorAccount.Probe` | 34 |
| POST | `/api/v1/vendor/accounts/:id/sync` | `h.VendorAccount.Sync` | 35 |
| GET | `/api/v1/vendor/accounts/:id/usage` | `h.VendorUsage.AccountUsage` | 36 |
| GET | `/api/v1/vendor/proxies` | `h.VendorProxy.List` | 39 |
| POST | `/api/v1/vendor/proxies` | `h.VendorProxy.Create` | 40 |
| POST | `/api/v1/vendor/proxies/import/preview` | `h.VendorProxy.ImportPreview` | 41 |
| POST | `/api/v1/vendor/proxies/import` | `h.VendorProxy.Import` | 42 |
| POST | `/api/v1/vendor/proxies/export` | `h.VendorProxy.Export` | 43 |
| POST | `/api/v1/vendor/proxies/batch-delete` | `h.VendorProxy.BatchDelete` | 44 |
| GET | `/api/v1/vendor/proxies/:id` | `h.VendorProxy.Get` | 45 |
| PATCH | `/api/v1/vendor/proxies/:id` | `h.VendorProxy.Update` | 46 |
| DELETE | `/api/v1/vendor/proxies/:id` | `h.VendorProxy.Delete` | 47 |
| POST | `/api/v1/vendor/proxies/:id/test` | `h.VendorProxy.Test` | 48 |
| POST | `/api/v1/vendor/proxies/:id/quality-check` | `h.VendorProxy.QualityCheck` | 49 |
| GET | `/api/v1/vendor/usage/summary` | `h.VendorUsage.Summary` | 50 |
| GET | `/api/v1/vendor/usage/trend` | `h.VendorUsage.Trend` | 51 |
| GET | `/api/v1/vendor/usage/accounts` | `h.VendorUsage.Accounts` | 52 |
| GET | `/api/v1/vendor/audit-logs` | `h.Vendor.Audit` | 53 |
| GET | `/api/v1/vendor/payables` | `h.VendorSettlement.PayableList` | 54 |
| GET | `/api/v1/vendor/payables/:id` | `h.VendorSettlement.PayableGet` | 55 |
| GET | `/api/v1/vendor/payables/:id/export` | `h.VendorSettlement.PayableExport` | 56 |
| GET | `/api/v1/vendor/settlements` | `h.VendorSettlement.List` | 57 |
| POST | `/api/v1/vendor/payables/preview` | `h.VendorSettlement.Preview` | 58 |
| POST | `/api/v1/vendor/payables` | `h.VendorSettlement.Create` | 59 |
| POST | `/api/v1/vendor/payables/:id/confirm` | `h.VendorSettlement.Confirm` | 60 |
| POST | `/api/v1/vendor/payables/:id/void` | `h.VendorSettlement.Void` | 61 |
| POST | `/api/v1/vendor/payables/:id/payments` | `h.VendorSettlement.Payment` | 62 |
| POST | `/api/v1/vendor/payables/:id/payments/:payment_id/reverse` | `h.VendorSettlement.Reverse` | 63 |
| POST | `/api/v1/vendor/payables/:id/adjustments` | `h.VendorSettlement.Adjustment` | 64 |
| GET | `/api/v1/vendor/settlements/:id` | `h.VendorSettlement.Get` | 65 |
| GET | `/api/v1/vendor/settlements/:id/export` | `h.VendorSettlement.Export` | 66 |
| POST | `/api/v1/vendor/oauth/:platform/sessions` | `h.VendorAccount.StartOAuth` | 69 |
| POST | `/api/v1/admin/vendors/oauth/:platform/sessions` | `h.VendorAccount.StartOAuth` | 69 |
| POST | `/api/v1/vendor/oauth/:platform/sessions/:session_id/complete` | `h.VendorAccount.CompleteOAuth` | 70 |
| POST | `/api/v1/admin/vendors/oauth/:platform/sessions/:session_id/complete` | `h.VendorAccount.CompleteOAuth` | 70 |
| PATCH | `/api/v1/vendor/oauth/:platform/sessions/:session_id/proxy` | `h.VendorAccount.RebindOAuthProxy` | 71 |
| PATCH | `/api/v1/admin/vendors/oauth/:platform/sessions/:session_id/proxy` | `h.VendorAccount.RebindOAuthProxy` | 71 |
| GET | `/api/v1/admin/vendors` | `h.Vendor.List` | 78 |
| POST | `/api/v1/admin/vendors` | `h.Vendor.Create` | 79 |
| GET | `/api/v1/admin/vendors/group-supply` | `h.Vendor.GroupSupply` | 80 |
| GET | `/api/v1/admin/vendors/audit-logs` | `h.Vendor.Audit` | 81 |
| GET | `/api/v1/admin/vendors/usage` | `h.VendorUsage.AdminUsage` | 82 |
| POST | `/api/v1/admin/vendors/identity-reconciliation` | `h.VendorAccount.ReconcileIdentity` | 83 |
| GET | `/api/v1/admin/vendors/usage/export` | `h.VendorUsage.AdminExport` | 84 |
| GET | `/api/v1/admin/vendors/settlements` | `h.VendorSettlement.AdminList` | 85 |
| POST | `/api/v1/admin/vendors/settlements/preview` | `h.VendorSettlement.Preview` | 86 |
| POST | `/api/v1/admin/vendors/settlements` | `h.VendorSettlement.Create` | 87 |
| GET | `/api/v1/admin/vendors/settlements/:id` | `h.VendorSettlement.AdminGet` | 88 |
| POST | `/api/v1/admin/vendors/settlements/:id/confirm` | `h.VendorSettlement.Confirm` | 89 |
| POST | `/api/v1/admin/vendors/settlements/:id/void` | `h.VendorSettlement.Void` | 90 |
| POST | `/api/v1/admin/vendors/settlements/:id/payments` | `h.VendorSettlement.Payment` | 91 |
| POST | `/api/v1/admin/vendors/settlements/:id/payments/:payment_id/reverse` | `h.VendorSettlement.Reverse` | 92 |
| POST | `/api/v1/admin/vendors/settlements/:id/adjustments` | `h.VendorSettlement.Adjustment` | 93 |
| GET | `/api/v1/admin/vendors/settlements/:id/export` | `h.VendorSettlement.AdminExport` | 94 |
| GET | `/api/v1/admin/vendors/:id` | `h.Vendor.Get` | 95 |
| PATCH | `/api/v1/admin/vendors/:id` | `h.Vendor.Update` | 96 |
| POST | `/api/v1/admin/accounts/import/preview` | `h.VendorAccount.ImportPreview` | 100 |
| POST | `/api/v1/admin/accounts/import` | `h.VendorAccount.Import` | 101 |
| POST | `/api/v1/admin/accounts/export` | `h.Vendor.ExportStepUp, h.VendorAccount.Export` | 102 |
| DELETE | `/api/v1/admin/accounts/:id/authorizations/:mode` | `h.VendorAccount.RevokeAuthorization` | 103 |
| POST | `/api/v1/admin/accounts/:id/pause` | `h.VendorAccount.Pause` | 104 |
| POST | `/api/v1/admin/accounts/:id/resume` | `h.VendorAccount.Resume` | 105 |
| POST | `/api/v1/admin/accounts/:id/usage-refresh` | `h.VendorAccount.UsageRefresh` | 106 |
| POST | `/api/v1/admin/accounts/:id/probe` | `h.VendorAccount.Probe` | 107 |
| POST | `/api/v1/admin/accounts/:id/sync` | `h.VendorAccount.Sync` | 108 |
| POST | `/api/v1/admin/proxies/import/preview` | `h.VendorProxy.ImportPreview` | 110 |
| POST | `/api/v1/admin/proxies/import` | `h.VendorProxy.Import` | 111 |
| POST | `/api/v1/admin/proxies/export` | `h.Vendor.SuperOnly, h.Vendor.AdminReadScope, h.VendorProxy.Export` | 112 |

## 请求与数据约束

- 开户、账号和代理输入以当前 handler/service DTO 为准；不继续使用旧文档的独立 username 必填或固定 1000/1000 限额假设。账号/名称/额度默认值由当前创建参数与供应商策略决定。
- 平台→一级、一级→二级合同和付款边独立；付款登记/冲正不是在线自动转账。
- scope=children 的直属只读聚合和单供应商选择由已认证身份决定；详情/批量写入仍检查对象范围。
- 原生/BPS 授权模式、导入/导出、暂停与归档是不同操作；历史用量模式不根据当前账号授权补写。
- 完整模型、供应商凭据、原始上游错误和其他供应商资料不可因列表或导出而泄露。
- 本表只记录本文件的供应商专用注册；复用的 /admin/accounts、/admin/proxies 原生管理行为还须查看 admin.go 和相应 handler。

本轮只核对静态注册与文档，没有启动服务器、执行真实数据库或生产接口验收。
