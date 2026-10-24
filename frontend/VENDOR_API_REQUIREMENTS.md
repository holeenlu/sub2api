# Supplier frontend API and integration requirements

> 2026-10-17 V3：以 [统一平台方案](../docs/vendor-console/UNIFIED-PLATFORM-PLAN.md) 和 [实际验收](../docs/vendor-console/UNIFIED-PLATFORM-ACCEPTANCE.md) 为准。独立供应商角色、原子开户、直属下级只读与双边结算现已实现；生产升级另行执行。旧原型不代表当前业务权限。

Requirement `VC-2026-10-15` · Updated 2026-10-17 · Shared by KDAN, TapModels and Tokensavy.

This document records the supplier frontend's API contracts, interaction requirements and acceptance boundaries. Maintain it with the implementation. Current product policy is defined in the [unified platform plan](../docs/vendor-console/UNIFIED-PLATFORM-PLAN.md), the complete method/path inventory in [API routes](../docs/vendor-console/API-ROUTES.md), and executed checks in [implementation status](../docs/vendor-console/IMPLEMENTATION-STATUS.md) and the [requirement review](../docs/vendor-console/REQUIREMENTS-REVIEW.md).

## Confirmed layout and page acceptance

- The final [prototype](../docs/vendor-console/prototype.html) defines supplier page content, layout and interaction order. The latest 2026-10-16 request also requires direct alignment with CCMax; update the prototype alongside the implementation to preserve one visual baseline. Validate each modified page against it and the authenticated CCMax supplier console; an existing Vue implementation does not supersede the confirmed design. Record executed checks and deliberate data/privacy differences in [UI acceptance](../docs/vendor-console/UI-ACCEPTANCE.md).
- Keep the native component/icon/style system. Use a 56px full-width fixed header, 224px resource sidebar, 24px desktop content padding and 16px section gaps, collapsible account overview, dense account/proxy tables, icon actions and an overflow-safe keyboard-accessible action menu. Mobile navigation is a dismissible drawer with focus containment and Escape restoration. Account/proxy tables scroll inside their containers; usage tables retain native mobile cards. Theme and locale controls use the existing project conventions.
- Account overview uses the compact total/status block, five runtime metrics and three quota distributions. Background refresh reuses the existing list endpoint, defaults to 60 seconds, preserves rows and pauses for hidden pages, selections and open edit/confirmation/detail flows. This adds no server job or endpoint.
- Proxy overview belongs beside the heading. Keep filters/actions in a compact toolbar and show ID, endpoint, region, creator, private scope, occupancy, bound IDs, creation, state/latency and icon actions; retain authentication, expiry, quality and reference details in the dialog.
- Account onboarding uses platform/method segments, name/notes, owned egress, authorized group multiselect, OAuth link/result controls and a fixed dialog footer. Proxy standard/quick modes share the existing import parser and preview/commit contract, including busy-state protection.
- Supplier usage starts with account/date filters, four summary metrics, daily trends and account detail tables. Advanced platform/group/timezone filters retain the draft/apply flow. Account usage shows seven metrics, fixed-window tokens including cache, request/Token/cost trends, model/outcome distributions and daily detail.
- All resource names, policy defaults, supported authorization methods, groups, quota windows/reset timestamps and usage come from the existing safe backend DTOs. No prototype fixtures or reference-site data enter the application. Unknown values remain unknown; statistical cost and settlement payable remain distinct.

## Entry points and authority

- The configured supplier HTTPS origin opens `/vendor/accounts` after native sign-in. Supplier identities use explicit vendor_level1 / vendor_level2 roles. Common tabs are accounts, proxies, usage, settlements and avatar profile; audit/security sidebar items are removed. L1 additionally manages direct children, reads their accounts/proxies/usage and manages payables. Super administrators enter /vendor/platform on the same supplier origin.
- `/admin/vendors` contains supplier management, supplier accounts, supplier proxies, usage, settlements, group supply and audit. The live super administrator manages all supplier identities; L1 manages only direct L2 identities through /api/v1/vendor/vendors, with inherited authorization limits. Existing user, account, proxy and batch interfaces must enforce the same boundary.
- Reuse native password, Passkey, MFA, step-up, password reset and security settings. Public `vendor_console_enabled` and `vendor_portal_origin` gate entry. Missing, disabled or mismatched configuration fails closed.
- `build:vendor` uses `VITE_VENDOR_PORTAL=true` and same-origin `/api/v1`. The [reverse-proxy example](../deploy/examples/vendor-portal.conf) forwards same-origin API requests to the single backend allowlist: scoped supplier administration is allowed, customer administration and public registration remain forbidden.
- Compare normalized browser origins: hostname case, the default HTTPS port and an optional trailing slash are equivalent. Reject credentials, other paths, queries, fragments or a different origin. HTTP/HTTPS, hostnames and non-default ports remain distinct origins.

## Resource scope and request contracts

- The frontend [API adapter](src/features/vendor/api.ts) owns scope and optional-query normalization; [types](src/features/vendor/types.ts) describe safe responses. Native administrator account/proxy updates use `PUT`; supplier updates use `PATCH`.
- Administrator account/proxy requests explicitly identify `vendor_id`. Aggregate supplier-only inventory uses zero. Mutations, authorization transfer and detailed workbenches require a selected positive supplier ID. Supplier writes derive direct ownership from the authenticated session. L1 read-only requests may explicitly select a verified direct child with vendor_id; that scope cannot mutate, probe or export credentials.
- Changing supplier clears selections, forms, cached labels, receipts and pending results. Cancel queued work and suppress late responses from the previous scope.
- Omit null, undefined and blank optional query values. Preserve meaningful zero watermarks, aggregate scope and false flags. Mutation bodies retain explicit empty values when clearing a field.
- `GET /api/v1/vendor/me` returns the safe vendor object with its capabilities. Consume the embedded fields directly. Treat backend authorization, policy/version conflicts and step-up requirements as authoritative.
- Create the dedicated login and supplier atomically using required email/password/username, tier and optional parent. No source selector or ordinary-user binding remains. New limits default to 1000/1000, concurrency 15, priority 1; passwords never appear in responses or audits. Editing defaults affects only future accounts; root administrators may adjust existing account concurrency/priority in the scoped account editor.

## Account onboarding, authorization and maintenance

- Expose account name, supplier notes, platform, authorized groups, egress and supported native OAuth/Token inputs. Concurrency, priority and statistical multipliers follow platform policy.
- Use native `GroupSelector`. Allow multiple active, authorized groups for the selected platform; filter initial defaults as well as later platform changes. At least one valid group is required.
- OAuth uses `/api/v1/vendor/oauth/:platform/sessions` and the matching complete/proxy routes; the administrator equivalents are under `/api/v1/admin/vendors/oauth` with explicit supplier scope. Preserve the server's scope, state/PKCE and receipt/version checks.
- Metadata edits preserve the authorization lease. Save metadata after successful authorization through the scoped native update; retry a failed metadata save without repeating a committed token exchange.
- Changing the account's proxy or editing the proxy endpoint/authentication preserves existing account authorization, supplier pause and platform restrictions. Updating an unfinished OAuth session follows the server's atomic egress/version contract.
- Support account detail, usage, edit, pause/resume, reauthorization, authorization refresh, quota refresh, probe, sync and deletion. Capture the submitted account ID before awaiting deletion, and discard results after a scope change.
- Delete archives the account and excludes it from new scheduling, while retaining its list row, identity claim, credentials and all historical usage. Archived records cannot be mutated, reauthorized or scheduled. Historical reports still resolve the account.
- Account details display the safe projection: supplier, verified identity, groups, masked egress and region source, rates, concurrency, success counts/denominators, recent/today/all-time metrics, cooldowns, model restrictions, plan and authorization/subscription expiry.
- Display supplier pause in both scoped and aggregate inventory. List/detail quota windows show usage, reset time/countdown and collection time. Missing provider data stays unknown.

## Authorization import and export

- Use the native `sub2api-data` / `sub2api-bundle` v1 account format, bounded to 1–25 rows per import. File resource IDs, owners and system settings do not grant authority or become bindings.
- Support batch defaults and per-row `mappings` for mixed Claude/OpenAI files. Each row explicitly resolves its authorized same-platform groups, selected proxy or allowed direct egress, and `skip`/`update` duplicate policy.
- Preview shows safe account labels, final groups, egress and row outcomes. Keep credentials in the request input only; do not echo parser failures or render raw authorization/proxy secrets.
- Confirm by server `preview_id`, respecting expiry and stale-policy/egress conflicts. Editing input or mapping invalidates the receipt; supplier changes and unmount clear authorization input. Prevent stale receipts from appearing in another supplier's dialog.
- Export requires explicit selection/scope, `sensitive_confirmed` and native server step-up. Normal list/detail DTOs remain credential-free. New imported accounts remain paused until explicitly resumed.

## Private proxy management

- Both add dialogs share ProxyFormFields and ProxyBatchInput: name, protocol, host, port, optional username/password, expiry presets and expiry fallback. Supplier backup chains must remain within the same direct owner; direct fallback is disabled.


- Share one workbench between the supplier and super administrator: summary, search, protocol/status/region/occupancy/date filters, sorting, pagination, selection and affected-reference details.
- Reuse native protocol parsing and components. Display protocol names as HTTP, HTTPS, SOCKS5 and SOCKS5H; preserve lowercase protocol values and URL schemes in API payloads.
- Provide standard add, quick multiline URL input and file import, with preview, duplicate/error feedback and partial-success results. Inline account-form proxy creation uses the same flows.
- Version edits and distinguish credential preserve/update/clear. Keep stable idempotency keys across unchanged write/import retries. Display detected region before a manual label and load proxy choices through supported fifty-row pages.
- Show safe bound-account and backup-account IDs plus OAuth/in-flight reference counts in details and before deletion. Reference-read failure offers retry; confirmation waits for successful reads. The backend retains authority over occupied-resource deletion and reports each result.
- Manual connection and quality probes use a bounded queue with at most two running requests. Unselected probes collect the complete bounded filtered set after confirmation; cancellation stops dequeuing and retains finished outcomes.
- CSV/JSON inventory export is masked and excludes proxy passwords. Re-import requires explicit authentication input. Probe results and unavailable region/latency fields must not be invented.

## Usage, reports and historical detail

- Reuse the native date picker and draft/apply/cancel/clear filter flow. Support supplier, account, date, group and account/date aggregation; administrator usage additionally supports safe per-request records.
- All-supplier administrator usage loads `/api/v1/admin/groups/all` only in that scope. Scoped reports use the supplier's authorized groups. Cancel stale catalog requests, clear changed group filters and expose retry on failures.
- Keep summary, pages, dimension changes, sorting and full-filter CSV export on the same `snapshot_at`, `watermark` and explicit timezone. Preserve zero watermarks. Sort on the server and distinguish applied filters from draft changes.
- Historical account selection uses `/api/v1/vendor/usage/accounts?all_history=true` or administrator usage with `dimension=account&all_history=true`. Use independent twenty-row paging and exact account-ID search, without report date/group/platform restrictions. The catalog includes attributed deleted accounts and omits accounts without recorded attributed usage.
- Account detail uses `/api/v1/vendor/accounts/:id/usage` or `/api/v1/admin/accounts/:id/usage` with its supplier scope. Aggregate drilldowns carry the row's vendor ID. Daily `page` and model `model_page` are independent; model responses include `model_total`, `model_page` and `model_page_size=100`.
- Show request/cost/Token/cache/success/latency metrics, fixed 5-hour/7-day/30-day snapshots, request/Token/cost trends, daily detail, model distribution and outcome distribution. Fixed windows retain their own meaning when the global date filter changes.
- Render Decimal cost strings without converting them to JavaScript Number. Separate statistical USD, Opus/Fable model consumption and settlement payable amounts. Supplier projections omit consumer identities, API Keys, IPs and request content.

## Settlement and audit

- Support report-to-draft navigation, per-supplier preview outcomes, contract periods, group coefficients, confirmation, void, payment registration, reversal, linked adjustment and paged frozen allocations/payments. Receivable statements are read-only and limited to their own confirmed bills. Tier-1 payables use a separate payer scope; tier-2 cannot access that scope.
- For platform-admin batch preview, omit `contract_version` to resolve each supplier's current contract. Omit `group_coefficients` to inherit each supplier's overrides; an explicit empty object clears overrides for that preview. Scoped contract settings cannot apply to other suppliers.
- Tier-1 payables always use the direct child's current server-side contract. Reject custom historical periods or forged prices/versions; keep platform-admin historical contract functionality.
- Display the resolved contract version. Contract/form changes invalidate preview; saving revalidates the receipt and version. Offer no save when all matching usage is already occupied.
- Freeze the payee, pricing and usage sources. Payment registration records an external payment and performs no transfer. Render inclusive calendar end dates in the statement's timezone; preserve DST behavior and late-usage handling.
- Audit supplier account, proxy, usage and settlement operations through safe supplier projections. Aggregate exports are visible only in the super administrator's aggregate scope. Preserve safe result summaries such as exported row counts.

## Native groups and identity reconciliation

- Supplier accounts supply the existing native groups. API Keys created by super administrators, administrators or ordinary users consume them only under the Key owner's existing group/subscription authorization.
- `/api/v1/admin/vendors/identity-reconciliation` accepts `after_id` and a batch `limit` of 1–25. The management panel sends one batch per click, defaults to cursor zero/limit ten, retains failed rows and supports bounded explicit retry.
- Reconciliation is manual. A completed scan does not mean failed identities were resolved. Match frontend/proxy timeouts to the backend's bounded operation; do not create an automatic provider-login or background reconciliation path.

## Components and verification

- Reuse the project's `Select`, `DataTable`, `StatusBadge`, `PlatformIcon`, `Icon`, `GroupSelector`, dialogs, pagination and date formatting. Preserve native table patterns and responsive layout.
- Localize platform/status/region/quota labels in zh, en, ja and generated zh-TW. Date-picker buttons must not submit their enclosing filter form. Render account actions outside inert native `template` elements.
- Keep interaction coverage for proxy changes preserving authorization, import/export privacy and receipt replay, deleted-account history, mixed-platform groups, quota reset timestamps, reference-protected deletion, stale scopes, Decimal precision, model paging, contract inheritance and audit visibility.
- Run checks using the existing package scripts and installed frontend/`tools/zh-tw` dependencies:

```sh
pnpm --dir frontend typecheck
pnpm --dir frontend lint:check
pnpm --dir frontend exec vitest run src/features/vendor src/api/__tests__/client.spec.ts src/i18n/__tests__/localeKeyCompleteness.spec.ts
node tools/zh-tw/gen-locale.mjs --check
pnpm --dir frontend exec vite build --outDir dist-review
VITE_VENDOR_PORTAL=true pnpm --dir frontend exec vite build --outDir dist-vendor
```

The normal Vite output targets `backend/internal/web/dist`; local verification uses the frontend output directories above. Executed test counts and deployment evidence belong in [implementation status](../docs/vendor-console/IMPLEMENTATION-STATUS.md).

## Deployment acceptance

Production activation requires the documented migration and deployment process, a dedicated HTTPS supplier origin, matching native Passkey RP/origin configuration, real Claude/OpenAI authorization and proxy checks, and capacity/reconciliation verification. Local fixtures and provider substitutes establish only their stated test boundaries. Production credentials, payments and deployment are separate operations.

For local supplier-origin checks, configure a separate HTTPS reverse proxy and the matching `vendor_portal_origin`; preserve the browser Origin and use same-origin `/api/v1`. The dedicated build flag does not bypass the origin or enable gate. Reuse native authentication rather than a prototype role selector.


## 2026-10-17 归档与运营资料补充

- 账号 DTO：`archived`、`archived_at`；现有 DELETE 返回 `archived: true`，默认列表与 `status=archived` 均可查。归档行仅详情/用量，编辑、授权、启停、导入覆盖及导出凭证都由服务端拒绝。
- 报表/历史选择器与账号详情用量：`account_archived` 独立于旧 `account_deleted`。归档不改变汇总金额、历史 Token、模型成本及账单事实。
- 运营表分别展示输入/输出、缓存写入/读取、汇总命中率及统计成本。`opus_cost_usd`、`fable_cost_usd` 复用已有后端值；其他/未分类用精确十进制减法核对，未知不能补零。金额消耗不得命名为配额。
- 后端的供应商名称/ID是归属信息，不能冒充创建者来源。V3 已保存直接 owner 与 root/parent 请求快照；前端不能由邮箱/名称或当前所有者角色推导历史归属。
- account_count/proxy_count 供直接归属列表和当前页汇总；account_tree_count/proxy_tree_count 仅用于一级含下级额度占用。不得把一级含下级数与二级数量再次相加。

## Aggregate resource workbenches (2026-10-17)

- Root supplier accounts/proxies use the complete workbenches by default with explicit `vendor_id=0`; remove the separate compact inventory table.
- Tier-1 child accounts/proxies default to `scope=children`. Supplier selection is optional, and choosing one switches to the existing `vendor_id` scope. The parent comes exclusively from server authentication.
- Both workbenches display a permanent owner column (supplier name + ID) for superior views; clicking the owner narrows the list. Proxy detail also carries the owner name.
- Aggregate details and account usage use the selected row's owner, preserving read-only access for tier-1. Root can select an owner to access its authorized management actions. No cross-owner write requests are formed.
- Counts, pagination, filters and pool statistics come from the scoped server response, never a browser merge of separately fetched child lists. Missing or inaccessible selected suppliers show an error, never a fallback to another resource scope.
