# Payment DTO ownership

The payment DTO owner is `@/features/payments/api/types`; `@/features/payments` explicitly exports these types, with its local `DashboardStats` exposed as `PaymentDashboardStats`. The retired `frontend/src/types/payment.ts` and root type barrel are not import targets. `SubscriptionPlan` remains owned by `@/features/subscriptions/api/types` and is referenced with `import type`, not redeclared.

- `OrderStatus`
- `PaymentType`
- `OrderType`
- `RechargeBonusTier`
- `PaymentConfig`
- `MethodLimit`
- `MethodLimitsResponse`
- `CheckoutInfoResponse`
- `PaymentOrder`
- `PaymentChannel`
- `ProviderInstance`
- `CreateOrderRequest`
- `CreateOrderResultType`
- `WechatOAuthInfo`
- `WechatJSAPIPayload`
- `CreateOrderResult`
- `CurrencyAmounts`
- `DailyPaymentStats`
- `PaymentMethodStats`
- `TopUserPaymentStats`
- `DashboardStats`

Personal balance history belongs to `@/features/payments/api/balanceHistory`; administrative user balance history and the id-only mutation acknowledgement belong to `@/features/payments/userBalances`. Their different ledger IDs, filters and response fields are not interchangeable.

Ownership and language rules remain in [FRONTEND_ARCHITECTURE](../../../../docs/architecture-rebuild/FRONTEND_ARCHITECTURE.md), [CODING_STANDARDS](../../../../docs/architecture-rebuild/CODING_STANDARDS.md) and [CT-11](../../../../docs/architecture-rebuild/CONTRACTS.md#ct-11-控制面与前端). This ownership record is not a validation receipt: Docker and page-by-page acceptance are `not_run`; full frontend acceptance remains incomplete.
