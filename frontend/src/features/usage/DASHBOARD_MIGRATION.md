# Usage, dashboard and balance-history ownership

The dashboard is an app composition because it combines usage, identity, Keys, subscriptions, jobs and balances. Usage owns read/query/format/export behavior; it does not import those domains' stores or implement payment mutations.

## API owners

- The retired `api/admin/dashboard.ts` maps to `@/features/usage/adminDashboard`. The default `dashboardAPI` and original named functions/types remain; the domain index also exposes `adminDashboardAPI`. Existing query methods accept cancellation options.
- The dashboard API retains stats/realtime/trend/model/group/snapshot queries, per-Key/per-user trends, spending ranking, batch user/Key usage and the named user/Key breakdown queries.
- Former `api/user.ts` usage reads live in `api/userRead.ts`, exported by `@/features/usage`. Platform-quota configuration DTOs have the single access-policy owner at `@/features/access-policy/api/platformQuotaTypes`.
- Former personal balance-history reads live in `@/features/payments` / `api/balanceHistory.ts`, preserving signed values, string ledger IDs, source/direction/category filters, pagination and total_recharged. Administrative balance history is a different projection in `@/features/payments/userBalances`.
- `AdminDashboardStats` is declared in `api/dashboardTypes.ts` and published by usage/adminDashboard and usage. The retired root DashboardStats declaration/barrel is not a compatibility import target. See [DTO ownership](DTO_MIGRATION.md).

## Retired pages and components

Paths are relative to `frontend/src`.

| Old | New |
| --- | --- |
| views/KeyUsageView.vue | features/usage/pages/PublicKeyUsagePage.vue |
| views/admin/DashboardView.vue | app/compositions/dashboard/pages/AdminDashboardPage.vue |
| views/user/DashboardView.vue | app/compositions/dashboard/pages/UserDashboardPage.vue |
| components/user/dashboard/UserDashboardStats.vue | app/compositions/dashboard/components/UserDashboardStats.vue; its platform fold is features/usage/model/dashboardPlatforms.ts |
| components/user/dashboard/UserDashboardQuickActions.vue | app/compositions/dashboard/components/UserDashboardQuickActions.vue |
| components/user/dashboard/UserDashboardCharts.vue | features/usage/components/UserDashboardCharts.vue -> usage/panels |
| components/user/dashboard/UserDashboardRecentUsage.vue | features/usage/components/UserDashboardRecentUsage.vue -> usage/panels |
| components/charts/EndpointDistributionChart.vue | features/usage/components/EndpointDistributionChart.vue -> usage/panels |
| components/charts/GroupDistributionChart.vue | features/usage/components/GroupDistributionChart.vue -> usage/panels |
| components/charts/ModelDistributionChart.vue | features/usage/components/ModelDistributionChart.vue -> usage/panels |
| components/charts/UserBreakdownSubTable.vue | features/usage/components/UserBreakdownSubTable.vue (only the three migrated charts consume it) |
| components/user/BalanceHistoryModal.vue | features/payments/components/BalanceHistoryModal.vue -> payments/panels |

`app/compositions/dashboard/context.ts` composes existing public identity/jobs APIs with the usage service contract. The user and admin dashboards have separate query owners; tables and charts receive their data or use an injected usage query port. Display-only trend charts and technical formatting remain shared rather than copied into the domain.

## Installed providers and credential boundaries

`app/compositions/usageServices.ts` installs `usageServicesKey` and `publicKeyUsageServicesKey`. It supplies scope, permissions, public settings, directory projections and the cross-domain history/error panels. `app/compositions/resourceServices.ts` supplies usage statistics charts via the public usage loaders. The three distribution components still have their usage owner; shared chart primitives do not own administrative breakdown requests.

`publicKeyUsageServicesKey` carries read-only appearance/settings loading and notifications, with no management-login scope. `getPublicKeyUsage` uses the shared apiClient and `buildGatewayUrl('/v1/usage')` with mandatory anonymous policy and an explicit x-api-key. Key material does not enter URL parameters or persistent storage, and an API-Key 401 does not refresh or clear the management session.

`app/compositions/paymentsServices.ts` provides `balanceHistoryAccountReaderKey` through the cancellable identity profile read. Balance/history query owners separately reject stale profile and ledger responses. This provider is already present; the earlier request to add it and extend getProfile is obsolete.

The public Key page separates credential-scoped reads, presentation, animation and detail panels. Admin reads separate snapshot, trend and ranking requests. TPS remains based on raw recorded duration, independently of TTFT; absent costs/durations remain unknown. Existing aliases and route metadata are held by app/router and domain routes rather than legacy wrappers.

## Standards and acceptance

[FRONTEND_ARCHITECTURE](../../../../docs/architecture-rebuild/FRONTEND_ARCHITECTURE.md), [CODING_STANDARDS](../../../../docs/architecture-rebuild/CODING_STANDARDS.md) and [CT-11](../../../../docs/architecture-rebuild/CONTRACTS.md#ct-11-控制面与前端) remain the rule sources. Use the existing Vitest, TypeScript and ESLint configuration for code changes; this document does not establish a separate validation platform. This documentation pass runs no business tests or builds. Docker and page-by-page acceptance are `not_run`; full frontend acceptance remains incomplete. Source migration alone does not establish runtime acceptance.
