# 05 · 支付、退款与返利完整性

归档日期：2026-10-07。文件快照：main `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238`；官方比较基线 `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`。

本页登记最终需求与实际文件归属；不把本轮提交整理等同于重新实现或完整安全审计。

## 最终需求

1. 沿用原订单、支付供应商、订阅及返利体系；补充商户/交易身份、回调、订单履约和退款一致性检查，不将上游已有的 Stripe/Airwallex 等提供方列作本次独立新增。

2. 退款先预留余额或订阅权益。网络超时或未知供应商结果不能被视为明确失败，不能立即释放预留并重发退款；保留 REFUND_PENDING 和现有查询/对账流程。

3. 没有自动 QueryRefund 能力的供应商使用明确的人工裁决入口，并要求原因、审计和一次性结束预留。人工裁决与查询共用退款租约判断，供应商请求仍在有效租约内时不得提前判失败而造成重复退款。

4. 支付恢复、日限额和返利回收遵循当前已提交代码；管理员操作还须满足 RBAC 的余额、定价、退款或返利调整权限。旧审计未关闭的账务边界不能因提交整理自动宣布修复。

## 入口与配置归属

原支付、订单、订阅和返利接口，以及已存在的退款人工裁决处理；未新建充值池、白牌余额镜像或跨站结算接口。

## 验收边界

退款 reservation 原子性、并发、未知结果与租约、商户快照/回调校验是独立风险。禁止用本次文件树检查代替真实供应商退款验收。

验证状态统一见 [总说明](README.md#验证和未关闭边界)。本页列的是验收要求，不是宣称本轮已重新运行这些检查。

## 对比统计

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 业务代码 | 22 | 1126 | 460 | 0 |
| 测试/夹具 | 15 | 1073 | 57 | 0 |

统计为最终文件相对固定官方基线的累计差异，并非本次整理新写的代码。对重命名统一展开为删除/新增；跨域文件只设一个主归属。

## 修改文件

| 状态 | 类型 | +行 | −行 | 文件 |
| --- | --- | ---: | ---: | --- |
| M | 业务代码 | 26 | 0 | [backend/internal/handler/admin/payment_handler.go](../../../backend/internal/handler/admin/payment_handler.go) |
| M | 业务代码 | 81 | 16 | [backend/internal/payment/provider/easypay.go](../../../backend/internal/payment/provider/easypay.go) |
| M | 测试/夹具 | 12 | 0 | [backend/internal/payment/provider/easypay_notify_security_test.go](../../../backend/internal/payment/provider/easypay_notify_security_test.go) |
| M | 测试/夹具 | 35 | 0 | [backend/internal/payment/provider/easypay_refund_test.go](../../../backend/internal/payment/provider/easypay_refund_test.go) |
| A | 测试/夹具 | 92 | 0 | [backend/internal/payment/provider/easypay_security_test.go](../../../backend/internal/payment/provider/easypay_security_test.go) |
| A | 业务代码 | 125 | 0 | [backend/internal/repository/affiliate_refund.go](../../../backend/internal/repository/affiliate_refund.go) |
| M | 业务代码 | 85 | 35 | [backend/internal/repository/affiliate_repo.go](../../../backend/internal/repository/affiliate_repo.go) |
| A | 测试/夹具 | 225 | 0 | [backend/internal/repository/affiliate_security_integration_test.go](../../../backend/internal/repository/affiliate_security_integration_test.go) |
| M | 测试/夹具 | 1 | 0 | [backend/internal/server/routes/payment_public_rate_limit_test.go](../../../backend/internal/server/routes/payment_public_rate_limit_test.go) |
| M | 业务代码 | 26 | 13 | [backend/internal/service/affiliate_service.go](../../../backend/internal/service/affiliate_service.go) |
| A | 业务代码 | 135 | 0 | [backend/internal/service/payment_allowance.go](../../../backend/internal/service/payment_allowance.go) |
| A | 测试/夹具 | 191 | 0 | [backend/internal/service/payment_allowance_test.go](../../../backend/internal/service/payment_allowance_test.go) |
| M | 业务代码 | 24 | 24 | [backend/internal/service/payment_config_service.go](../../../backend/internal/service/payment_config_service.go) |
| M | 业务代码 | 82 | 42 | [backend/internal/service/payment_fulfillment.go](../../../backend/internal/service/payment_fulfillment.go) |
| M | 业务代码 | 15 | 24 | [backend/internal/service/payment_order.go](../../../backend/internal/service/payment_order.go) |
| M | 业务代码 | 7 | 1 | [backend/internal/service/payment_order_expiry_service.go](../../../backend/internal/service/payment_order_expiry_service.go) |
| M | 业务代码 | 4 | 1 | [backend/internal/service/payment_order_lifecycle.go](../../../backend/internal/service/payment_order_lifecycle.go) |
| M | 测试/夹具 | 3 | 2 | [backend/internal/service/payment_order_result_test.go](../../../backend/internal/service/payment_order_result_test.go) |
| M | 业务代码 | 152 | 271 | [backend/internal/service/payment_refund.go](../../../backend/internal/service/payment_refund.go) |
| A | 业务代码 | 292 | 0 | [backend/internal/service/payment_refund_reservation.go](../../../backend/internal/service/payment_refund_reservation.go) |
| M | 测试/夹具 | 65 | 44 | [backend/internal/service/payment_refund_test.go](../../../backend/internal/service/payment_refund_test.go) |
| M | 业务代码 | 5 | 2 | [backend/internal/service/payment_resume_service.go](../../../backend/internal/service/payment_resume_service.go) |
| M | 测试/夹具 | 2 | 2 | [backend/internal/service/payment_resume_service_test.go](../../../backend/internal/service/payment_resume_service_test.go) |
| A | 测试/夹具 | 145 | 0 | [backend/internal/service/payment_security_postgres_test.go](../../../backend/internal/service/payment_security_postgres_test.go) |
| A | 测试/夹具 | 286 | 0 | [backend/internal/service/payment_security_test.go](../../../backend/internal/service/payment_security_test.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/service/payment_service.go](../../../backend/internal/service/payment_service.go) |
| M | 业务代码 | 9 | 1 | [backend/internal/service/subscription_service.go](../../../backend/internal/service/subscription_service.go) |
| M | 测试/夹具 | 9 | 9 | [frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts](../../../frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts) |
| M | 业务代码 | 11 | 8 | [frontend/src/views/admin/SubscriptionsView.vue](../../../frontend/src/views/admin/SubscriptionsView.vue) |
| M | 测试/夹具 | 3 | 0 | [frontend/src/views/admin/__tests__/SubscriptionsView.bulkActions.spec.ts](../../../frontend/src/views/admin/__tests__/SubscriptionsView.bulkActions.spec.ts) |
| M | 测试/夹具 | 3 | 0 | [frontend/src/views/admin/__tests__/SubscriptionsView.userUsageLink.spec.ts](../../../frontend/src/views/admin/__tests__/SubscriptionsView.userUsageLink.spec.ts) |
| M | 业务代码 | 4 | 2 | [frontend/src/views/admin/affiliates/AdminAffiliateRecordsTable.vue](../../../frontend/src/views/admin/affiliates/AdminAffiliateRecordsTable.vue) |
| M | 测试/夹具 | 1 | 0 | [frontend/src/views/admin/affiliates/__tests__/AdminAffiliateRecordsTable.spec.ts](../../../frontend/src/views/admin/affiliates/__tests__/AdminAffiliateRecordsTable.spec.ts) |
| M | 业务代码 | 9 | 6 | [frontend/src/views/admin/orders/AdminOrdersView.vue](../../../frontend/src/views/admin/orders/AdminOrdersView.vue) |
| M | 业务代码 | 9 | 5 | [frontend/src/views/admin/orders/AdminPaymentPlansView.vue](../../../frontend/src/views/admin/orders/AdminPaymentPlansView.vue) |
| M | 业务代码 | 10 | 7 | [frontend/src/views/admin/orders/PlanEditDialog.vue](../../../frontend/src/views/admin/orders/PlanEditDialog.vue) |
| M | 业务代码 | 14 | 1 | [frontend/src/views/user/AirwallexPaymentView.vue](../../../frontend/src/views/user/AirwallexPaymentView.vue) |
