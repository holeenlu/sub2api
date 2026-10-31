# Payments integration

## Current public owners

`@/features/payments` publishes `paymentAPI`, `adminPaymentAPI`, `usePaymentConfig`, service keys, DTOs and explicit pure helpers. API AxiosResponse envelopes remain intact. `@/features/payments/panels` exports actual domain components, while `routes.ts` contributes lazy pages. Payment DTO ownership is recorded in [DTO_MIGRATION](DTO_MIGRATION.md); SubscriptionPlan is owned by subscriptions.

Personal balance history is `api/balanceHistory.ts`. Administrative balance changes/history are `@/features/payments/userBalances`. Its restricted-actor acknowledgement may contain only id; it is not a complete AdminUser or a balance snapshot. `app/compositions/user-directory/UserBalanceModal.vue` requests the parent list refresh without manufacturing those fields. The personal and administrative ledgers retain distinct IDs, filters and DTO shapes.

## Installed app providers

`app/compositions/paymentsServices.ts` installs `PaymentsServices`, `PaymentPlansServices` and `balanceHistoryAccountReaderKey`. The reader delegates to identity `getProfile({signal})` and projects only id/email/username/balance. The service contracts keep nullable identity scope, current-user projection, public UTC offset, subscription projections, refresh actions and notifications. Order management/refund permissions retain `orders.manage` and `orders.refund`.

Plan management retains `plans.manage`, `billing.rates.update`, super-admin config visibility and the existing plan.create/plan.update writable-field sets. The app supplies the group directory and GroupBadge component; plan sale management does not own subscription policy or pricing algorithms.

`resolvePublicOrder` delegates to the unique `paymentAPI.resolveOrderPublicByResumeToken` or `verifyOrderPublic` wrapper and returns its data. Both wrappers require `sessionPolicy: 'anonymous'` on the shared apiClient. Public proof/credential state and login/order state have separate ownership; there is no second HTTP instance or copied endpoint.

## Retired implementations

Paths below are relative to `frontend/src`. These mappings describe existing owners, not patches for an integrator to apply.

| Old path | New owner/public mapping |
| --- | --- |
| api/payment.ts | features/payments/api/payment.ts -> @/features/payments paymentAPI |
| api/admin/payment.ts | features/payments/api/adminPayment.ts -> @/features/payments adminPaymentAPI |
| stores/payment.ts | usePaymentConfig + useOrderMonitor + useCheckoutOrder; no replacement global store |
| views/user/PaymentView.vue | features/payments/pages/CheckoutPage.vue |
| views/auth/WechatPaymentCallbackView.vue | features/payments/pages/WechatPaymentCallbackPage.vue |
| components/payment/*.vue | features/payments/components/*.vue -> @/features/payments/panels |
| components/payment/{currency,providerConfig,paymentFlow,orderUtils,validity,alipayDeepLink}.ts | features/payments/model/* -> explicit @/features/payments exports |
| api/usage.ts | features/usage/api/usage.ts -> usageAPI + named queries |
| api/admin/usage.ts | features/usage/api/adminUsage.ts -> adminUsageAPI |
| api/usageExport.ts | features/usage/api/usageExport.ts -> consumeUsageExport / escapeUsageCSV |
| utils/usageMetrics.ts | features/usage/model/metrics.ts -> explicit usage metric exports |
| views/user/UsageView.vue | features/usage/pages/UserUsagePage.vue |
| views/admin/UsageView.vue | features/usage/pages/AdminUsagePage.vue |
| components/admin/usage/* | features/usage/components/* -> @/features/usage/panels |
| components/user/UserErrorRequestsTable.vue | features/usage/components/UserErrorRequestsTable.vue -> usage/panels |
| components/user/UserErrorDetailModal.vue | features/usage/components/UserErrorDetailModal.vue -> usage/panels |

PaymentQRDialog and StripePaymentInline had no runtime callers and are absent. The routed QR page uses PaymentStatusPanel and one polling owner. Dedicated regressions now sit with the domain implementation.

| Actual old path | Current component | Current route/public entry |
| --- | --- | --- |
| views/user/UserOrdersView.vue | features/payments/pages/UserOrdersPage.vue | /orders |
| views/user/PaymentResultView.vue | features/payments/pages/PaymentResultPage.vue | /payment/result |
| views/user/PaymentQRCodeView.vue | features/payments/pages/PaymentQRCodePage.vue | /payment/qrcode |
| views/user/StripePaymentView.vue | features/payments/pages/StripePaymentPage.vue | /payment/stripe |
| views/user/AirwallexPaymentView.vue | features/payments/pages/AirwallexPaymentPage.vue | /payment/airwallex |
| views/user/StripePopupView.vue | features/payments/pages/StripePopupPage.vue | /payment/stripe-popup |
| views/admin/orders/AdminOrdersView.vue | features/payments/pages/AdminOrdersPage.vue | /admin/orders |
| views/admin/orders/AdminPaymentDashboardView.vue | features/payments/pages/AdminPaymentDashboardPage.vue | /admin/orders/dashboard |
| views/admin/orders/AdminPaymentPlansView.vue | features/payments/pages/AdminPaymentPlansPage.vue | /admin/orders/plans |
| views/admin/orders/PlanEditDialog.vue | features/payments/components/PaymentPlanEditDialog.vue | payments/panels.PaymentPlanEditDialog |
| views/user/paymentUx.ts | features/payments/model/paymentUx.ts | internal helper |
| views/user/paymentWechatResume.ts | features/payments/model/paymentWechatResume.ts | internal helper |
| components/admin/payment/{AdminRefundDialog,OrderStatsCards,DailyRevenueChart}.vue | features/payments/components/* -> payments/panels | domain panels |

`payments/routes.ts` and `app/App.vue` hold the current shell rules. Airwallex retains its user shell for anonymous and authenticated visitors while remaining public; Stripe's shell depends on the existing method query convention. Result, popup and WeChat callback routes retain their standalone behavior. Domain pages do not import AppLayout.

## Preserved command and usage contracts

REST URLs, DTO/CSV/XLSX/JSON formats, currency precision and explicit refund-force confirmation are retained. URL returns and SDK callbacks start server confirmation; they do not establish a paid order. Unknown creation/refund/SDK results are not automatically resent. The existing explicit WeChat merchant-rejection desktop-QR fallback is bounded; ambiguous gateway/SDK failures do not create another order. UI cancellation stops local work and late presentation, not backend/provider execution.

Usage queries, metrics and exports belong to usage. Its app provider retains `usage.export`/`billing.cost.read`, historical user reads with include_deleted, account filtering at 1/20, group filtering at 1/1000 and Key projections limited to id/name. TPS uses raw output_tokens/duration_ms; TTFT presentation remains separate. Unknown/lower-bound/zero distinctions are preserved.

## Standards and acceptance

[FRONTEND_ARCHITECTURE](../../../../docs/architecture-rebuild/FRONTEND_ARCHITECTURE.md), [CODING_STANDARDS](../../../../docs/architecture-rebuild/CODING_STANDARDS.md) and [CT-11](../../../../docs/architecture-rebuild/CONTRACTS.md#ct-11-控制面与前端) remain the rule sources. Use the existing Vitest, TypeScript and ESLint configuration for code changes; this document does not establish a separate validation platform. This documentation pass runs no business tests or builds. Docker and page-by-page acceptance are `not_run`; full frontend acceptance remains incomplete. Source migration alone does not establish runtime acceptance.
