# 09 · 支付、退款、余额与返利完整性

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`fix(billing): preserve payment refund and balance invariants`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. 沿用原订单、支付供应商、订阅及返利体系；补充商户/交易身份、回调、订单履约和退款一致性检查，不将上游已有的 Stripe/Airwallex 等提供方列作本次独立新增。

2. 退款先预留余额或订阅权益。网络超时或未知供应商结果不能被视为明确失败，不能立即释放预留并重发退款；保留 REFUND_PENDING 和现有查询/对账流程。

3. 没有自动 QueryRefund 能力的供应商使用明确的人工裁决入口，并要求原因、审计和一次性结束预留。人工裁决与查询共用退款租约判断，供应商请求仍在有效租约内时不得提前判失败而造成重复退款。

4. 支付恢复、日限额和返利回收遵循当前已提交代码；管理员操作还须满足 RBAC 的余额、定价、退款或返利调整权限。旧审计未关闭的账务边界不能因提交整理自动宣布修复。

官方 v0.2.17 的易支付查询、支付宝退款查询、Stripe client_secret 与退款 pending-hold 修复直接继承；相对基线仍保留本地商户校验、权限、审计与账务约束。原 inflight 估算的有界内存测试隔离到子进程，避免其他测试的后台分配污染 HeapAlloc，业务估算规则不变。

## 入口与配置归属

原支付、订单、订阅和返利接口，以及已存在的退款人工裁决处理；未新建充值池、白牌余额镜像或跨站结算接口。

## 最终实现方案

1. 在原 payment provider、订单和退款服务内校验商户/交易快照、回调与权益预留；不建立跨站余额池。

2. 未知退款结果保留 REFUND_PENDING 和预留；查询/人工裁决共用租约判断，理由审计与释放一次性提交。

3. 余额、定价、退款及返利操作同时遵循 02 的权限和对象投影。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

退款未知结果、租约、权益预留和商户回调已有测试；不调用实际支付供应商。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| source | 24 | 1076 | 646 | 0 |
| test | 20 | 1150 | 415 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 16 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 26 | 0 | 09 | [backend/internal/handler/admin/payment_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/payment_handler.go) |
| M | source | 6 | 11 | 09 | [backend/internal/payment/provider/alipay.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/payment/provider/alipay.go) |
| M | test | 15 | 15 | 09 | [backend/internal/payment/provider/alipay_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/payment/provider/alipay_test.go) |
| M | source | 40 | 19 | 09 | [backend/internal/payment/provider/easypay.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/payment/provider/easypay.go) |
| M | test | 18 | 0 | 09 | [backend/internal/payment/provider/easypay_confirm_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/payment/provider/easypay_confirm_test.go) |
| M | test | 12 | 0 | 09 | [backend/internal/payment/provider/easypay_notify_security_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/payment/provider/easypay_notify_security_test.go) |
| M | test | 7 | 1 | 09 | [backend/internal/payment/provider/easypay_query_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/payment/provider/easypay_query_test.go) |
| M | test | 35 | 0 | 09 | [backend/internal/payment/provider/easypay_refund_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/payment/provider/easypay_refund_test.go) |
| A | test | 96 | 0 | 09 | [backend/internal/payment/provider/easypay_security_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/payment/provider/easypay_security_test.go) |
| M | source | 1 | 6 | 09 | [backend/internal/payment/types.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/payment/types.go) |
| A | source | 125 | 0 | 09 | [backend/internal/repository/affiliate_refund.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/affiliate_refund.go) |
| M | source | 85 | 35 | 09 | [backend/internal/repository/affiliate_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/affiliate_repo.go) |
| A | test | 225 | 0 | 09 | [backend/internal/repository/affiliate_security_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/affiliate_security_integration_test.go) |
| M | test | 1 | 0 | 09 | [backend/internal/server/routes/payment_public_rate_limit_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/routes/payment_public_rate_limit_test.go) |
| M | source | 26 | 13 | 09 | [backend/internal/service/affiliate_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/affiliate_service.go) |
| M | test | 12 | 0 | 09 | [backend/internal/service/billing_inflight_reservation_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/billing_inflight_reservation_test.go) |
| A | source | 135 | 0 | 09 | [backend/internal/service/payment_allowance.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_allowance.go) |
| A | test | 191 | 0 | 09 | [backend/internal/service/payment_allowance_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_allowance_test.go) |
| M | source | 24 | 24 | 09 | [backend/internal/service/payment_config_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_config_service.go) |
| M | source | 73 | 40 | 09 | [backend/internal/service/payment_fulfillment.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_fulfillment.go) |
| M | source | 12 | 24 | 09 | [backend/internal/service/payment_order.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_order.go) |
| M | source | 7 | 1 | 09 | [backend/internal/service/payment_order_expiry_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_order_expiry_service.go) |
| M | source | 4 | 1 | 09 | [backend/internal/service/payment_order_lifecycle.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_order_lifecycle.go) |
| M | test | 3 | 2 | 09 | [backend/internal/service/payment_order_result_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_order_result_test.go) |
| M | source | 148 | 437 | 09 | [backend/internal/service/payment_refund.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_refund.go) |
| D | test | 0 | 312 | 09 | `backend/internal/service/payment_refund_pending_hold_test.go` |
| A | source | 292 | 0 | 09 | [backend/internal/service/payment_refund_reservation.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_refund_reservation.go) |
| M | test | 84 | 74 | 09 | [backend/internal/service/payment_refund_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_refund_test.go) |
| M | source | 5 | 2 | 09 | [backend/internal/service/payment_resume_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_resume_service.go) |
| M | test | 2 | 2 | 09 | [backend/internal/service/payment_resume_service_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_resume_service_test.go) |
| A | test | 145 | 0 | 09 | [backend/internal/service/payment_security_postgres_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_security_postgres_test.go) |
| A | test | 288 | 0 | 09 | [backend/internal/service/payment_security_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_security_test.go) |
| M | source | 1 | 3 | 09 | [backend/internal/service/payment_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/payment_service.go) |
| M | source | 9 | 1 | 09 | [backend/internal/service/subscription_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/subscription_service.go) |
| M | test | 9 | 9 | 09 | [frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts) |
| M | source | 11 | 8 | 09 | [frontend/src/views/admin/SubscriptionsView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/SubscriptionsView.vue) |
| M | test | 3 | 0 | 09 | [frontend/src/views/admin/__tests__/SubscriptionsView.bulkActions.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/SubscriptionsView.bulkActions.spec.ts) |
| M | test | 3 | 0 | 09 | [frontend/src/views/admin/__tests__/SubscriptionsView.userUsageLink.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/SubscriptionsView.userUsageLink.spec.ts) |
| M | source | 4 | 2 | 09 | [frontend/src/views/admin/affiliates/AdminAffiliateRecordsTable.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/affiliates/AdminAffiliateRecordsTable.vue) |
| M | test | 1 | 0 | 09 | [frontend/src/views/admin/affiliates/__tests__/AdminAffiliateRecordsTable.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/affiliates/__tests__/AdminAffiliateRecordsTable.spec.ts) |
| M | source | 9 | 6 | 09 | [frontend/src/views/admin/orders/AdminOrdersView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/orders/AdminOrdersView.vue) |
| M | source | 9 | 5 | 09 | [frontend/src/views/admin/orders/AdminPaymentPlansView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/orders/AdminPaymentPlansView.vue) |
| M | source | 10 | 7 | 09 | [frontend/src/views/admin/orders/PlanEditDialog.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/orders/PlanEditDialog.vue) |
| M | source | 14 | 1 | 09 | [frontend/src/views/user/AirwallexPaymentView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/user/AirwallexPaymentView.vue) |
