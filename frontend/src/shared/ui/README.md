# Shared UI

This directory owns technical Vue controls, not account/group/pricing/authorization rules. Shared controls receive props/events or a narrow technical injection and do not read application or domain stores. Import the concrete component or documented technical type module; the retired `@/components/common` barrel is not an import target.

## Current component contracts

| Component | Public boundary |
| --- | --- |
| `DataTable.vue` | `columns`, `data`, loading/sorting, row identity and optional selection/windowing; column types belong to `tableTypes.ts`. Named `cell-{key}` slots receive the row/value; `empty` replaces the empty state. Sorting, row-click and selected-key events remain component outputs. |
| `Pagination.vue` | `total`, `totalExact`, one-based `page`, `pageSize` and optional selectors/jump controls; emits `update:page` and `update:pageSize`. Page-size options come from `shared/lib/tablePreferences` rather than a hard-coded README list. |
| `BaseDialog.vue` | `show`, `title` and `width`: `narrow`, `normal`, `wide`, `extra-wide` or `full`. Default width is `normal`; Escape closes by default, outside-click closing defaults to false. Emits `close` and accepts default and footer slots. |
| `ConfirmDialog.vue` | Uses `BaseDialog`; accepts show/title/message and optional confirm/cancel text and danger state; emits `confirm` or `cancel`. Default labels use i18n. |
| `LoadingSpinner.vue` | Sizes `sm/md/lg/xl` and colors `primary/secondary/white/gray`; the status role and label describe loading. |
| `EmptyState.vue` | Optional title/message, description, icon and action configuration; supports icon/action slots and emits `action`. |
| `TablePageLayout.vue` | Technical table-page layout; domain filters, columns and commands remain supplied by consumers. |
| `icons/Icon.vue` | Shared technical icon presentation. |

The old README's `Modal.vue` and `StatCard.vue` examples did not correspond to files in this directory and have been removed. The implementation interfaces remain authoritative; this overview does not claim that every legacy row type has already been narrowed or that all accessibility scenarios have been validated.

## Feedback and domain presentation

`feedback.ts` declares `UiFeedback`, `uiFeedbackKey` and `useUiFeedback()`. The application provides success/error callbacks once in its effect scope. This is a technical feedback boundary, not a replacement domain service locator. Business components continue to consume their domain's declared services.

The toast host is `app/components/Toast.vue`, with application state in `app/model/app.ts`. It is not a shared control that imports a root app store. No `@/stores/app` or global-store example belongs here.

Business presentation has its own owner: Group badges/selectors are published by `@/features/access-policy/presentation`; resource proxy fields by `@/features/resources/proxyFields`; resource pickers/commands by `@/features/resources/managementPanels`. Cross-domain consumers receive these components through app-installed ports. Shared UI does not import the implementations back from those domains.

`app/compositions/panelLoadingServices.ts` handles selected async panel presentation. Loading/failure views do not forward business listeners to placeholder DOM, and a closed panel stays visually closed. The actual domain component is loaded; no empty replacement implements business behavior.

## Import example

~~~vue
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/shared/ui/BaseDialog.vue'

defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
</script>

<template>
  <BaseDialog :show="show" :title="t('common.confirm')" width="narrow" @close="emit('close')">
    <slot />
  </BaseDialog>
</template>
~~~

Keyboard navigation, focus restoration, ARIA feedback, narrow-screen behavior and large-table performance still require the relevant runtime/browser verification. Source presence alone is not an accessibility or performance acceptance result.

## Source of rules and acceptance

The authoritative rules are [FRONTEND_ARCHITECTURE](../../../../docs/architecture-rebuild/FRONTEND_ARCHITECTURE.md), [CODING_STANDARDS](../../../../docs/architecture-rebuild/CODING_STANDARDS.md) and [CT-11](../../../../docs/architecture-rebuild/CONTRACTS.md#ct-11-控制面与前端). This document describes current source ownership; it is not a second coding standard or a test receipt. Docker and page-by-page acceptance are `not_run`; full frontend acceptance remains incomplete.
