# Application router

`index.ts` assembles the domain route contributions and application composition pages. `meta.d.ts` declares the supported route metadata; `title.ts` resolves document titles and descriptions. Runtime authorization remains server-owned.

## Current route ownership

The router imports `identityRoutes`, `accessPolicyRoutes`, `keysRoutes`, `catalogRoutes`, `pricingRoutes`, `subscriptionsRoutes`, `paymentsRoutes`, `usageRoutes`, `operationsRoutes`, `jobsRoutes`, `siteRoutes` and `systemRoutes` from their domains' `routes.ts` files. Those contributions use lazy imports. Cross-domain pages stay in `app/compositions`.

These are implementation anchors, not another authorization or navigation registry:

| Stable URL | Current implementation |
| --- | --- |
| `/` | Redirect to `/home` |
| `/dashboard` | `app/compositions/dashboard/pages/UserDashboardPage.vue` |
| `/keys` | `features/keys/pages/KeysPage.vue` |
| `/usage`, `/admin/usage`, `/key-usage` | `features/usage/routes.ts`; the public Key page remains anonymous |
| `/profile`, `/admin/users` | `features/identity/routes.ts` |
| `/admin/dashboard` | `app/compositions/dashboard/pages/AdminDashboardPage.vue` |
| `/admin/accounts`, `/admin/proxies` | `app/compositions/resource-workspace/AccountsPage.vue` and `ProxiesPage.vue` |
| `/admin/groups` | `app/compositions/group-editor/GroupsPage.vue` |
| `/admin/settings` | `app/compositions/settings/SettingsWorkspace.vue` |
| `/vendor/:tab` | `app/compositions/vendor-portal/VendorPortalPage.vue`; accepted tab values are declared by the route |
| `/vendor-unavailable` | `app/compositions/vendor-portal/VendorUnavailablePage.vue` |

OAuth aliases, pending-auth exceptions, public payment callbacks, setup redirects and supplier-origin rules are retained in their actual route contributions/guards. Do not replace this logic with a generic admin-versus-user path test. A stable URL or alias does not preserve an obsolete page implementation.

## Authentication, metadata and shells

- Identity is consumed through `@/features/identity`; the old `@/stores/auth` import is retired. Supplier scope comes from `@/features/supply` and its app portal composition.
- `requiresAuth` defaults to protected access unless explicitly false. Admin access also depends on the current role/permissions and route-specific feature conditions; frontend guards do not authorize backend actions.
- Current metadata includes `shell`, `requiresAuth`, `requiresAdmin`, `requiresPayment`, `requiresRiskControl`, `requiresSubscription`, title/description keys and standalone SEO-title flags. Read `meta.d.ts` and the route contribution rather than copying a truncated interface.
- `app/App.vue` selects the application shell and keys private routed views by `identityScopeKey`. Public callbacks retain their distinct scope behavior; domain pages do not add a second app shell.
- Navigation uses the shared resolver in `title.ts`. `App.vue` also reacts to route, locale and site-settings changes; bootstrap sets the initial brand title. The i18n module does not import the router or application stores to update titles.
- Application bootstrap owns installation and mount ordering. Do not add a second auth initialization, token refresher or lifecycle registry in a route page.

## Loading and lifecycle

Route imports and `app/compositions/panelLoadingServices.ts` are different boundaries: the former loads pages, while the latter binds selected heavy public domain panels on demand. Domain `panelLoaders.ts` modules import concrete Vue components, preserving their actual contracts. Dynamic imports alone do not imply instance caching or prove that a component left the initial bundle; inspect the generated entry/import graph.

The existing `useRoutePrefetch.ts`, navigation loading state, scroll behavior and chunk-recovery handling remain in this router directory. Runtime acceptance must exercise stable deep links, role/scope changes, pending OAuth/payment paths, locale/title changes and browser history. The presence of guard tests or source mappings does not establish that these browser scenarios passed.

## Source of rules and acceptance

The authoritative rules are [FRONTEND_ARCHITECTURE](../../../../docs/architecture-rebuild/FRONTEND_ARCHITECTURE.md), [CODING_STANDARDS](../../../../docs/architecture-rebuild/CODING_STANDARDS.md) and [CT-11](../../../../docs/architecture-rebuild/CONTRACTS.md#ct-11-控制面与前端). This document describes current source ownership; it is not a second coding standard or a test receipt. Docker and page-by-page acceptance are `not_run`; full frontend acceptance remains incomplete.
