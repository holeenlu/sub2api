# Supply and supplier portal integration

## Current public interfaces

- `@/features/supply` publishes supplier APIs, DTOs, scope helpers, safe origin/redirect policy and `useSupplyStore`/`useSupplyPage`. `@/features/supply/panels` contains the supply-owned relationship, authorization, statement and usage panels.
- `@/features/supply/api` uses the shared apiClient and preserves existing URLs, methods, exact decimal strings, authorizations, statement versions and idempotency contracts. It does not invent a general revision protocol.
- `VendorAccount` and `VendorProxy` are supply projections, not management Account/Proxy DTOs. Cross-owner read-only leaves are explicit; missing fields are not manufactured to force those types to match.
- `app/compositions/vendor-portal` owns the mixed supplier workbench, guard, version projection and panel contracts. `installSupplyServices` binds session scope before supplier refresh and returns a disposer. `featureServices.ts` invokes it within app lifecycle and provides real identity/resource panels through public loaders.
- `createVendorPortalGuard` preserves its handled/redirect distinction and rejects stale-session admission. Scope, origin, role and public settings are app-supplied facts. `usePortalVersion` keeps portal-manifest and backend build facts separate; both reads now receive an AbortSignal, and generation checks suppress late results.

`SupplyServices` retains the nullable scope, read-only identity, platform-name function and assignable-group reader. It does not import app stores or other domains' runtime implementations. Notifications, clipboard and mixed-domain interaction remain app composition responsibilities.

## Retired implementation mapping

Paths are relative to `frontend/src`.

| Old path | New path |
| --- | --- |
| features/vendor/{api,types,helpers}.ts | features/supply/{api,types,helpers}.ts |
| features/vendor/store.ts useVendorStore | features/supply/store.ts useSupplyStore |
| features/vendor/useVendorPage.ts | features/supply/useSupplyPage.ts |
| features/vendor/version.ts | app/compositions/vendor-portal/version.ts |
| features/vendor/components/{VendorEditor,VendorManagement,SettlementWorkbench,GroupSupply,VendorAudit,IdentityReconciliation,AccountAuthorizations,UsageMetrics,UsageTable,UsageTrend,RequestState,ActionMenu,DisplayModeSwitcher}.vue | features/supply/components/ with the same component filenames |
| features/vendor/components/{AccountWorkbench,ProxyWorkbench,UsageWorkbench,AccountForm,AccountImport,ProxyForm,ProxyImport,VendorChildrenView,VendorLayout,VendorAccountSettings,AccountColumnChooser,AccountCreatedRangePicker,UsageAccountPicker}.vue | app/compositions/vendor-portal/components/ with the same component filenames |
| views/vendor/VendorPortalView.vue | app/compositions/vendor-portal/VendorPortalPage.vue |
| views/vendor/VendorUnavailableView.vue | app/compositions/vendor-portal/VendorUnavailablePage.vue |
| views/admin/VendorsView.vue (follow-up assigned scope) | app/compositions/vendor-portal/VendorPlatformWorkspace.vue |
| features/vendor/__tests__/** | supply/__tests__ or app/compositions/vendor-portal/__tests__, following the component owner |

The old `features/vendor` and `views/vendor` implementation directories are absent. App/router retains the real supplier tab URLs, `/vendor-unavailable` and existing redirect behavior. The platform workspace remains an application composition; a stable URL is not a legacy implementation wrapper.

## Scope, money and privacy contracts

- Scope helpers discard stale query scope/vendor_id values before applying the effective scope. The existing vendor_id=0 aggregate is read-only; children remain read-only and do not grant arbitrary-descendant write access.
- Native/BPS supplier authorization and historical Native/BPS/Cloud usage modes have distinct meanings. Explicit empty authorization lists remain empty; missing historical fields retain their documented interpretation.
- Bilateral statement/payable flows retain payer/payee roles, direct-child limits, versions, preview tokens, idempotency, allocations, reversals/adjustments and exact decimal strings. Supplier money is not converted into a user balance.
- Requests and editors bind session/resource/policy scope, abort owned work and reject stale results. Scope changes release selections, credentials and queued proxy work; the backend remains authoritative for object access.
- Origin checks preserve scheme/port and reject credentials/path/query/hash. Safe redirects accept the established local supplier paths. Display settings do not choose the management HTTP target.
- Supplier usage rows retain their own cost/statement projections; they are not cast to ordinary user UsageLog/UsageStatsResponse. Technical tables and controls are shared, while business queries retain their owner.

## Standards and acceptance

[FRONTEND_ARCHITECTURE](../../../../docs/architecture-rebuild/FRONTEND_ARCHITECTURE.md), [CODING_STANDARDS](../../../../docs/architecture-rebuild/CODING_STANDARDS.md) and [CT-11](../../../../docs/architecture-rebuild/CONTRACTS.md#ct-11-控制面与前端) remain the rule sources. Use the existing Vitest, TypeScript and ESLint configuration for code changes; this document does not establish a separate validation platform. This documentation pass runs no business tests or builds. Docker and page-by-page acceptance are `not_run`; full frontend acceptance remains incomplete. Source migration alone does not establish runtime acceptance.
