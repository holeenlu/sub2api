<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { platformDisplayName } from '@/constants/platformCatalog'
import { platformBadgeLightClass } from '@/utils/platformColors'
import Pagination from '@/components/common/Pagination.vue'
import { get } from '../api'
import { useVendorPage } from '../useVendorPage'
import type { Page } from '../types'
import RequestState from './RequestState.vue'
interface SupplyRow {
  group_id: number
  group_name: string
  platform: string
  vendor_id?: number | null
  vendor_name?: string
  account_count: number
  available_count: number
}
const props = defineProps<{ vendorId?: number }>(),
  { t } = useI18n(),
  search = ref('')
const list = useVendorPage<SupplyRow>((q, s) =>
    get<Page<SupplyRow>>(
      '/admin/vendors/group-supply',
      { ...q, vendor_id: props.vendorId },
      s,
    ),
  ),
  { items, total, page, pageSize, loading, error } = list
function load(reset = false) {
  return list.load({ search: search.value }, reset)
}
watch(
  () => props.vendorId,
  () => load(true),
  { immediate: true },
)
</script>
<template>
  <section class="space-y-4">
    <p class="rounded-xl border p-4 text-sm text-gray-500">
      {{ t('vendor.supplyHelp') }}
    </p>
    <form
      class="flex flex-wrap gap-2 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800"
      @submit.prevent="load(true)"
    >
      <input
        v-model="search"
        class="input max-w-sm"
        :aria-label="t('vendor.search')"
        :placeholder="t('vendor.search')"
      /><button class="btn btn-secondary">{{ t('vendor.search') }}</button>
    </form>
    <RequestState
      :loading="loading"
      :error="error"
      :empty="!items.length"
      @retry="load()"
      ><div class="table-container">
        <table class="table">
          <thead>
            <tr>
              <th
                v-for="k in [
                  'group',
                  'platform',
                  'vendor',
                  'accounts',
                  'active',
                ]"
                :key="k"
                class="p-3"
              >
                {{ t('vendor.' + k) }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in items" :key="i" class="border-t">
              <td class="p-3">{{ row.group_name }} #{{ row.group_id }}</td>
              <td class="p-3">
                <span
                  class="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium"
                  :class="platformBadgeLightClass(row.platform)"
                  ><PlatformIcon :platform="row.platform" size="xs" />{{
                    platformDisplayName(row.platform)
                  }}</span
                >
              </td>
              <td class="p-3">
                {{
                  row.vendor_id == null
                    ? t('vendor.platformOwned')
                    : row.vendor_name || '#' + row.vendor_id
                }}
              </td>
              <td class="p-3">{{ row.account_count }}</td>
              <td class="p-3">{{ row.available_count }}</td>
            </tr>
          </tbody>
        </table>
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
