# 09 · 支付、退款与返利完整性

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。

## 最终需求

1. 沿用原订单、支付供应商、订阅及返利体系；补充商户/交易身份、回调、订单履约和退款一致性检查，不将上游已有的 Stripe/Airwallex 等提供方列作本次独立新增。

2. 退款先预留余额或订阅权益。网络超时或未知供应商结果不能被视为明确失败，不能立即释放预留并重发退款；保留 REFUND_PENDING 和现有查询/对账流程。

3. 没有自动 QueryRefund 能力的供应商使用明确的人工裁决入口，并要求原因、审计和一次性结束预留。人工裁决与查询共用退款租约判断，供应商请求仍在有效租约内时不得提前判失败而造成重复退款。

4. 支付恢复、日限额和返利回收遵循当前已提交代码；管理员操作还须满足 RBAC 的余额、定价、退款或返利调整权限。旧审计未关闭的账务边界不能因提交整理自动宣布修复。

## 入口与配置归属

原支付、订单、订阅和返利接口，以及已存在的退款人工裁决处理；未新建充值池、白牌余额镜像或跨站结算接口。

## 最终实现方案

1. 在原 payment provider、订单和退款服务内校验商户/交易快照、回调与权益预留；不建立跨站余额池。

2. 未知退款结果保留 REFUND_PENDING 和预留；查询/人工裁决共用租约判断，理由审计与释放一次性提交。

3. 余额、定价、退款及返利操作同时遵循 02 的权限和对象投影。

## 边界、失败路径与依赖

没有额外文件归属组关联。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。上面描述现有最终行为，本次不重新实现业务。

## 验收要求

退款未知结果、租约、权益预留和商户回调已有测试；不调用实际支付供应商。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | --- | --- | --- | --- |
| source | 22 | 1126 | 460 | 0 |
| test | 15 | 1073 | 57 | 0 |

这是相对 v0.2.16 的累计定制差异，不是本次新写代码。删除/新增重命名分别计数；生成代码与测试单列。

## 来源提交

- `6902e3b21a59df794601ff8fc13c2ac8f5824106`
- `c6a92e1501a3ed5bb405c02bd541fcb59366bd28`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | --- | --- | --- | --- |
| M | source | 26 | 0 | 09 | [backend/internal/handler/admin/payment_handler.go](../../../backend/internal/handler/admin/payment_handler.go) |
| M | source | 81 | 16 | 09 | [backend/internal/payment/provider/easypay.go](../../../backend/internal/payment/provider/easypay.go) |
| M | test | 12 | 0 | 09 | [backend/internal/payment/provider/easypay_notify_security_test.go](../../../backend/internal/payment/provider/easypay_notify_security_test.go) |
| M | test | 35 | 0 | 09 | [backend/internal/payment/provider/easypay_refund_test.go](../../../backend/internal/payment/provider/easypay_refund_test.go) |
| A | test | 92 | 0 | 09 | [backend/internal/payment/provider/easypay_security_test.go](../../../backend/internal/payment/provider/easypay_security_test.go) |
| A | source | 125 | 0 | 09 | [backend/internal/repository/affiliate_refund.go](../../../backend/internal/repository/affiliate_refund.go) |
| M | source | 85 | 35 | 09 | [backend/internal/repository/affiliate_repo.go](../../../backend/internal/repository/affiliate_repo.go) |
| A | test | 225 | 0 | 09 | [backend/internal/repository/affiliate_security_integration_test.go](../../../backend/internal/repository/affiliate_security_integration_test.go) |
| M | test | 1 | 0 | 09 | [backend/internal/server/routes/payment_public_rate_limit_test.go](../../../backend/internal/server/routes/payment_public_rate_limit_test.go) |
| M | source | 26 | 13 | 09 | [backend/internal/service/affiliate_service.go](../../../backend/internal/service/affiliate_service.go) |
| A | source | 135 | 0 | 09 | [backend/internal/service/payment_allowance.go](../../../backend/internal/service/payment_allowance.go) |
| A | test | 191 | 0 | 09 | [backend/internal/service/payment_allowance_test.go](../../../backend/internal/service/payment_allowance_test.go) |
| M | source | 24 | 24 | 09 | [backend/internal/service/payment_config_service.go](../../../backend/internal/service/payment_config_service.go) |
| M | source | 82 | 42 | 09 | [backend/internal/service/payment_fulfillment.go](../../../backend/internal/service/payment_fulfillment.go) |
| M | source | 15 | 24 | 09 | [backend/internal/service/payment_order.go](../../../backend/internal/service/payment_order.go) |
| M | source | 7 | 1 | 09 | [backend/internal/service/payment_order_expiry_service.go](../../../backend/internal/service/payment_order_expiry_service.go) |
| M | source | 4 | 1 | 09 | [backend/internal/service/payment_order_lifecycle.go](../../../backend/internal/service/payment_order_lifecycle.go) |
| M | test | 3 | 2 | 09 | [backend/internal/service/payment_order_result_test.go](../../../backend/internal/service/payment_order_result_test.go) |
| M | source | 152 | 271 | 09 | [backend/internal/service/payment_refund.go](../../../backend/internal/service/payment_refund.go) |
| A | source | 292 | 0 | 09 | [backend/internal/service/payment_refund_reservation.go](../../../backend/internal/service/payment_refund_reservation.go) |
| M | test | 65 | 44 | 09 | [backend/internal/service/payment_refund_test.go](../../../backend/internal/service/payment_refund_test.go) |
| M | source | 5 | 2 | 09 | [backend/internal/service/payment_resume_service.go](../../../backend/internal/service/payment_resume_service.go) |
| M | test | 2 | 2 | 09 | [backend/internal/service/payment_resume_service_test.go](../../../backend/internal/service/payment_resume_service_test.go) |
| A | test | 145 | 0 | 09 | [backend/internal/service/payment_security_postgres_test.go](../../../backend/internal/service/payment_security_postgres_test.go) |
| A | test | 286 | 0 | 09 | [backend/internal/service/payment_security_test.go](../../../backend/internal/service/payment_security_test.go) |
| M | source | 1 | 1 | 09 | [backend/internal/service/payment_service.go](../../../backend/internal/service/payment_service.go) |
| M | source | 9 | 1 | 09 | [backend/internal/service/subscription_service.go](../../../backend/internal/service/subscription_service.go) |
| M | test | 9 | 9 | 09 | [frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts](../../../frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts) |
| M | source | 11 | 8 | 09 | [frontend/src/views/admin/SubscriptionsView.vue](../../../frontend/src/views/admin/SubscriptionsView.vue) |
| M | test | 3 | 0 | 09 | [frontend/src/views/admin/__tests__/SubscriptionsView.bulkActions.spec.ts](../../../frontend/src/views/admin/__tests__/SubscriptionsView.bulkActions.spec.ts) |
| M | test | 3 | 0 | 09 | [frontend/src/views/admin/__tests__/SubscriptionsView.userUsageLink.spec.ts](../../../frontend/src/views/admin/__tests__/SubscriptionsView.userUsageLink.spec.ts) |
| M | source | 4 | 2 | 09 | [frontend/src/views/admin/affiliates/AdminAffiliateRecordsTable.vue](../../../frontend/src/views/admin/affiliates/AdminAffiliateRecordsTable.vue) |
| M | test | 1 | 0 | 09 | [frontend/src/views/admin/affiliates/__tests__/AdminAffiliateRecordsTable.spec.ts](../../../frontend/src/views/admin/affiliates/__tests__/AdminAffiliateRecordsTable.spec.ts) |
| M | source | 9 | 6 | 09 | [frontend/src/views/admin/orders/AdminOrdersView.vue](../../../frontend/src/views/admin/orders/AdminOrdersView.vue) |
| M | source | 9 | 5 | 09 | [frontend/src/views/admin/orders/AdminPaymentPlansView.vue](../../../frontend/src/views/admin/orders/AdminPaymentPlansView.vue) |
| M | source | 10 | 7 | 09 | [frontend/src/views/admin/orders/PlanEditDialog.vue](../../../frontend/src/views/admin/orders/PlanEditDialog.vue) |
| M | source | 14 | 1 | 09 | [frontend/src/views/user/AirwallexPaymentView.vue](../../../frontend/src/views/user/AirwallexPaymentView.vue) |
