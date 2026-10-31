# API Key management

## Public boundaries and assembly

- `@/features/keys` exports `keysAPI`, its DTOs, `keysServicesKey` and `KeysServices`. `@/features/keys/routes` contributes `/keys` with the user shell; `KeysPage.vue` does not wrap itself in an app shell.
- `@/features/keys/panels` exports `UseKeyModal` and `EndpointPopover`. `@/features/keys/userKeys` owns administrative user-Key queries; `@/features/keys/admin` owns administrative Key group binding.
- `app/compositions/featureServices.ts` installs the real services: nullable logical session/business scope; read-only public settings; group/rate/usage requests with cancellation; notifications, clipboard and successful-create onboarding; the public Codex model-manifest adapter; and app-provided Group presentation components. No second auth store or HTTP client is created.
- Requests use `@/shared/http`. Existing endpoints, custom-key behavior, quota/rate/expiry/IP fields, group eligibility and the transfer-v1 JSON `key/format/data` protocol remain explicit contracts. Display labels are not technical provider IDs or credential-storage keys.

## Implementation mapping

Paths are relative to `frontend/src`; retired source names identify the migration, not an import compatibility layer.

| Removed source | New owner/public exit |
| --- | --- |
| api/keys.ts | features/keys/api/keys.ts, api/types.ts; public features/keys/index.ts |
| Key DTO declarations formerly in types/index.ts | features/keys/api/types.ts; public @/features/keys export; old root type barrel removed |
| views/user/KeysView.vue | features/keys/pages/KeysPage.vue; lazy features/keys/routes.ts |
| KeysView list/filter/page/selection/usage requests | features/keys/queries/useKeyList.ts |
| KeysView paired available-group/rate refresh | features/keys/queries/useKeyGroups.ts |
| KeysView edit/create draft and payload conversion | features/keys/model/keyDraft.ts, components/KeyEditorDialog.vue |
| KeysView identity/group/custom key, restrictions, expiry fields | components/KeyIdentityFields.vue, KeyRestrictionFields.vue, KeyExpirationFields.vue under the feature |
| KeysView table, inline group selector, copy/reset countdown | features/keys/components/KeysTable.vue; column preferences in model/useKeyColumns.ts |
| components/keys/BulkEditKeysModal.vue | features/keys/components/BulkEditKeysModal.vue + model/useBulkKeyEdit.ts |
| components/keys/KeyDataTransfer.vue | features/keys/components/KeyDataTransfer.vue + queries/useKeyTransfer.ts + model/keyTransfer.ts |
| components/keys/EndpointPopover.vue | features/keys/components/EndpointPopover.vue; public panels export |
| components/keys/UseKeyModal.vue | features/keys/components/UseKeyModal.vue, queries/useKeyGuideCatalog.ts, model/guide/{types,useKeyGuideFiles,openCodeConfig}.ts; public panels export |
| api/__tests__/keys.bulkUpdate.spec.ts | features/keys/__tests__/keys.bulkUpdate.spec.ts |
| components/keys/__tests__/*.spec.ts | features/keys/components/__tests__/*.spec.ts |
| views/user/__tests__/KeysView.spec.ts | features/keys/__tests__/KeysPage.spec.ts; old page test entry removed |

## Query, draft and presentation ownership

`queries/useKeyList.ts` owns the list/filter/page/selection/usage reads; `useKeyGroups.ts` owns paired eligibility/rate reads. Detached drafts and payload projection are in `model/keyDraft.ts` and the editor field components. Transfers and guide generation have their own models/query owners rather than a page-sized catch-all composable.

Requests capture scope and sequence, reject stale success/error/finalizer writes and stop owned reads on disposal. Commands retain the submitted target/payload; invalidation stops further batch scheduling without claiming rollback of a server mutation. Rejected commands preserve editable drafts. Quota-reset results do not overwrite an unsaved editor. Private guide/import buffers and copied feedback are cleared with their owning session.

Missing/null/non-finite observations display as unknown, while recorded zero remains zero. Existing column configuration applies nowrap and a minimum current-concurrency width; actual viewport/accessibility validation remains separate. The wire/export DTO is not widened into a display-only fallback shape.

Technical controls are in `@/shared/ui`; formatting, clipboard, masking and table preferences are in `@/shared/lib`. Group presentation, catalog configuration and other business facts stay in their owning domains and reach Keys through `KeysServices` or explicit read-only type dependencies. Old `components`, `utils`, `composables` and root API/store/type barrels are retired.

## Standards and acceptance

[FRONTEND_ARCHITECTURE](../../../../docs/architecture-rebuild/FRONTEND_ARCHITECTURE.md), [CODING_STANDARDS](../../../../docs/architecture-rebuild/CODING_STANDARDS.md) and [CT-11](../../../../docs/architecture-rebuild/CONTRACTS.md#ct-11-控制面与前端) remain the rule sources. Use the existing Vitest, TypeScript and ESLint configuration for code changes; this document does not establish a separate validation platform. This documentation pass runs no business tests or builds. Docker and page-by-page acceptance are `not_run`; full frontend acceptance remains incomplete. Source migration alone does not establish runtime acceptance.
