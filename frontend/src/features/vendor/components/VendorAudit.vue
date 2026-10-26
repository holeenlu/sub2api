<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Pagination from '@/components/common/Pagination.vue'
import DataTable from '@/components/common/DataTable.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import { formatDateTime } from '@/utils/format'
import { vendorAPI } from '../api'
import { useVendorPage } from '../useVendorPage'
import type { AuditEvent } from '../types'
import RequestState from './RequestState.vue'
const props = withDefaults(
    defineProps<{ admin?: boolean; vendorId?: number }>(),
    { admin: false },
  ),
  { t } = useI18n()
const filters = reactive({ action: '', start_date: '', end_date: '' }),
  applied = ref({ ...filters })
const columns = computed(() => [
  { key: 'created_at', label: t('vendor.date') },
  { key: 'actor', label: t('vendor.actor') },
  { key: 'action', label: t('vendor.actions') },
  { key: 'result', label: t('vendor.results') },
  { key: 'vendor_id', label: t('vendor.vendor') },
  { key: 'object_id', label: t('vendor.object') },
])
const list = useVendorPage<AuditEvent>((q, s) =>
    vendorAPI.audit(
      props.admin,
      { ...q, ...(props.vendorId ? { vendor_id: props.vendorId } : {}) },
      s,
    ),
  ),
  { items, total, page, pageSize, loading, error } = list
function load(reset = false) {
  if (reset) applied.value = { ...filters }
  return list.load(applied.value, reset)
}
watch(
  () => [props.admin, props.vendorId],
  () => load(true),
  { immediate: true },
)
</script>
<template>
  <section class="space-y-4">
    <form
      class="card flex flex-wrap items-end gap-3 p-4"
      @submit.prevent="load(true)"
    >
      <label
        >{{ t('vendor.actions')
        }}<input v-model="filters.action" class="input mt-1" /></label
      ><label
        >{{ t('vendor.startDate')
        }}<input
          v-model="filters.start_date"
          type="date"
          class="input mt-1" /></label
      ><label
        >{{ t('vendor.endDate')
        }}<input
          v-model="filters.end_date"
          type="date"
          class="input mt-1" /></label
      ><button class="btn btn-primary">{{ t('vendor.apply') }}</button>
    </form>
    <RequestState
      :loading="loading"
      :error="error"
      :empty="!items.length"
      @retry="load()"
      ><div class="card overflow-hidden">
        <DataTable :data="items" :columns="columns" row-key="id">
          <template #cell-created_at="{ row }"
            ><span class="whitespace-nowrap">{{
              formatDateTime(row.created_at)
            }}</span></template
          >
          <template #cell-actor="{ row }"
            >{{ row.actor_user_id || '—' }} · {{ row.actor_role }}</template
          >
          <template #cell-action="{ row }"
            ><span class="font-mono text-xs">{{ row.action }}</span></template
          >
          <template #cell-result="{ row }"
            ><StatusBadge
              :status="
                row.result === 'failed' ? 'error' : row.result || 'unknown'
              "
              :label="
                ['success', 'failed'].includes(row.result)
                  ? t('vendor.' + row.result)
                  : row.result || t('vendor.unknown')
              "
          /></template>
          <template #cell-object_id="{ row }">{{
            row.object_id || '—'
          }}</template>
        </DataTable>
      </div></RequestState
    ><Pagination
      v-if="total"
      :page="page"
      :page-size="pageSize"
      :total="total"
      @update:page="
        page = $event
        load();
      "
      @update:page-size="
        pageSize = $event
        page = 1
        load();
      "
    />
  </section>
</template>
