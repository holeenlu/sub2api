# Operations and jobs integration

## Public ownership

`@/features/operations/api` and `@/features/operations` explicitly publish the Ops, audit, administrative/user channel-monitor and monitor APIs and their wire types. The aggregate has no default export and does not flatten conflicting list/get functions. Direct endpoint modules retain their named/default interfaces. `@/features/jobs/api` publishes the existing batch-image functions, errors and DTOs; jobs owns the corresponding page/query behavior.

Paths below are relative to `frontend/src`.

| Old source | New sole implementation |
| --- | --- |
| api/admin/ops.ts | features/operations/api/ops.ts |
| api/admin/audit.ts (actual filename) | features/operations/api/audit.ts |
| api/admin/channelMonitor.ts | features/operations/api/adminChannelMonitor.ts |
| api/channelMonitor.ts | features/operations/api/userChannelMonitor.ts |
| api/channelMonitorV2.ts | features/operations/api/monitor.ts |
| api/__tests__/channelMonitorV2.spec.ts | features/operations/api/__tests__/monitor.spec.ts |
| views/admin/ops/** | features/operations/dashboard/** |
| views/admin/AuditLogView.vue | features/operations/pages/AuditLogPage.vue |
| features/prompt-audit/** | features/operations/promptAudit/** |
| features/channel-monitor-v2/** | features/operations/monitor/** |
| api/batchImage.ts | features/jobs/api/batchImage.ts |
| views/user/BatchImageGuideView.vue | features/jobs/pages/BatchImageJobsPage.vue |
| composables/useBatchImageAccess.ts | features/jobs/queries/useBatchImageAccess.ts (also exported by features/jobs) |
| api/__tests__/admin.channelMonitor.duplicate.spec.ts | features/operations/api/__tests__/adminChannelMonitor.duplicate.spec.ts |
| composables/__tests__/useBatchImageAccess.retry.spec.ts | features/jobs/__tests__/useBatchImageAccess.spec.ts |

The legacy root buckets and old feature locations are absent. `operations/routes.ts` and `jobs/routes.ts` contribute lazy routes, with application shell and guards assembled by app. Existing locale ownership is a separate migration concern; moved runtime modules do not prove that every planned locale move is complete.

## Installed application contracts

`app/compositions/operationsServices.ts` installs `operationsServicesKey` and the monitor service contract. Operations receives logical session/portal/business scope; permissions including super-admin, operations management and prompt-content visibility; monitoring settings/reload/disable callbacks; catalog projections; public monitor settings; group/account-name readers with signal; TOTP status, notifications and clipboard. These are existing owner projections, not another settings writer or catalog implementation.

`app/compositions/jobsServices.ts` installs `jobsServicesKey`. It provides scope, the displayed API base URL, personal-Key reads and feedback/copy actions. The displayed URL does not select the authenticated management HTTP destination. Sidebars and quick actions consume the public `useBatchImageAccess` query contract.

`@/features/operations/panelLoaders` exposes the real error table/detail components for app-bound usage panels; component props/events and permissions are retained. `opsAPI.listErrorLogs(params, {signal})` keeps the existing /admin/ops/errors endpoint and filters.

## Commands, state and transport

Monitor duplication uses `useMonitorDuplicate().duplicateMonitor(id)`. Its explicit idempotency key is retained for an ambiguous operation in the same logical scope and is not reused for another scope. Existing historical browser data is not indiscriminately deleted.

Management requests use `@/shared/http`. Batch generation keeps its distinct explicit API-Key transport with cancellation, omitted credentials and redirect rejection. It does not inherit the management JWT, retry ambiguously submitted generation or treat a display URL as an authorized external target. Cancelling local waiting does not establish server job cancellation or final cost. Parent/child status and unknown costs/counts remain server facts.

Query and command owners capture scope/resource identity, handle stale errors/finalizers and dispose owned timers/reads. Preview-cache identities use scope and Key id, not raw Key material.

## Public specifier mapping

| Old import | Public import |
| --- | --- |
| @/api/admin/ops | @/features/operations/api/ops |
| @/api/admin/audit | @/features/operations/api/audit |
| @/api/admin/channelMonitor | @/features/operations/api/adminChannelMonitor |
| @/api/channelMonitor | @/features/operations/api/userChannelMonitor |
| @/api/batchImage | @/features/jobs/api |
| @/api/channelMonitorV2 | @/features/operations/api/monitor |
## Standards and acceptance

[FRONTEND_ARCHITECTURE](../../../../docs/architecture-rebuild/FRONTEND_ARCHITECTURE.md), [CODING_STANDARDS](../../../../docs/architecture-rebuild/CODING_STANDARDS.md) and [CT-11](../../../../docs/architecture-rebuild/CONTRACTS.md#ct-11-控制面与前端) remain the rule sources. Use the existing Vitest, TypeScript and ESLint configuration for code changes; this document does not establish a separate validation platform. This documentation pass runs no business tests or builds. Docker and page-by-page acceptance are `not_run`; full frontend acceptance remains incomplete. Source migration alone does not establish runtime acceptance.
