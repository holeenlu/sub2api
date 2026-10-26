<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import VendorManagement from '@/features/vendor/components/VendorManagement.vue'
import AccountWorkbench from '@/features/vendor/components/AccountWorkbench.vue'
import ProxyWorkbench from '@/features/vendor/components/ProxyWorkbench.vue'
import UsageWorkbench from '@/features/vendor/components/UsageWorkbench.vue'
import SettlementWorkbench from '@/features/vendor/components/SettlementWorkbench.vue'
import GroupSupply from '@/features/vendor/components/GroupSupply.vue'
import VendorAudit from '@/features/vendor/components/VendorAudit.vue'
import RequestState from '@/features/vendor/components/RequestState.vue'
import { vendorAPI } from '@/features/vendor/api'
import apiClient from '@/api/client'
import { errorText } from '@/features/vendor/helpers'
import { useAuthStore } from '@/stores/auth'
import type { Query, Vendor, VendorGroup } from '@/features/vendor/types'
const props = defineProps<{ embedded?: boolean }>()
const { t } = useI18n(),
  route = useRoute(),
  router = useRouter(),
  auth = useAuthStore()
const tabs = [
  'suppliers',
  'accounts',
  'proxies',
  'usage',
  'settlements',
  'supply',
  'audit',
]
const tabIcons: Record<string, 'users' | 'grid' | 'globe' | 'chartBar' | 'document' | 'cube' | 'clock'> = {
  suppliers: 'users',
  accounts: 'grid',
  proxies: 'globe',
  usage: 'chartBar',
  settlements: 'document',
  supply: 'cube',
  audit: 'clock',
}
const tab = computed(() =>
  tabs.includes(String(route.query.tab))
    ? String(route.query.tab)
    : 'suppliers',
)
const vendorId = ref<number>(),
  vendor = ref<Vendor>(),
  vendors = ref<Vendor[]>([]),
  search = ref(''),
  page = ref(1),
  total = ref(0),
  loading = ref(false),
  error = ref('')
const vendorsLoading = ref(false),
  vendorsError = ref('')
const vendorOptions = computed(() => [
  { value: null, label: t('vendor.allVendors') },
  ...(vendor.value && !vendors.value.some((v) => v.id === vendor.value?.id)
    ? [{ value: vendor.value.id, label: vendor.value.name }]
    : []),
  ...vendors.value.map((v) => ({
    value: v.id,
    label: `${v.name} · ${t('vendor.' + v.status)}`,
  })),
])
const usageGroups = ref<VendorGroup[]>([]),
  groupsLoading = ref(false),
  groupsError = ref('')
let groupsController: AbortController | undefined
async function loadUsageGroups() {
  groupsController?.abort()
  usageGroups.value = []
  groupsError.value = ''
  groupsLoading.value = false
  if (!auth.isSuperAdmin || !['usage', 'accounts'].includes(tab.value) || vendorId.value) return
  const controller = new AbortController()
  groupsController = controller
  groupsLoading.value = true
  try {
    const { data } = await apiClient.get<VendorGroup[]>('/admin/groups/all', {
      signal: controller.signal,
    })
    if (!controller.signal.aborted) usageGroups.value = data
  } catch (e) {
    if (!controller.signal.aborted) groupsError.value = errorText(e)
  } finally {
    if (!controller.signal.aborted) groupsLoading.value = false
  }
}
watch([tab, vendorId, () => auth.isSuperAdmin], loadUsageGroups, {
  immediate: true,
})
onUnmounted(() => groupsController?.abort())
function retry() {
  if (vendorsError.value) loadVendors()
  if (groupsError.value) loadUsageGroups()
  if (error.value) loadVendor()
}
const draftFilters = ref<Query>()
function draftFromUsage(filters: Query) {
  draftFilters.value = filters
  router.replace({ query: { ...route.query, tab: 'settlements' } })
}
function openVendorTab(
  target: 'accounts' | 'proxies' | 'usage' | 'settlements',
  id: number,
) {
  vendorId.value = id
  router.replace({ query: { ...route.query, tab: target } })
}
let epoch = 0
let searchEpoch = 0
async function loadVendors() {
  const current = ++searchEpoch
  vendorsLoading.value = true
  vendorsError.value = ''
  try {
    const response = await vendorAPI.list({
      page: page.value,
      page_size: 20,
      search: search.value,
    })
    if (current !== searchEpoch) return
    vendors.value = response.items
    total.value = response.total
  } catch (e) {
    if (current === searchEpoch) vendorsError.value = errorText(e)
  } finally {
    if (current === searchEpoch) vendorsLoading.value = false
  }
}
async function loadVendor() {
  const current = ++epoch
  vendor.value = undefined
  if (!vendorId.value) {
    loading.value = false
    error.value = ''
    return
  }
  loading.value = true
  error.value = ''
  try {
    const value = await vendorAPI.detail(vendorId.value)
    if (current === epoch) vendor.value = value
  } catch (e) {
    if (current === epoch) error.value = errorText(e)
  } finally {
    if (current === epoch) loading.value = false
  }
}
watch(vendorId, () => {
  draftFilters.value = undefined
  loadVendor()
})
onMounted(() => {
  if (auth.isSuperAdmin) loadVendors()
})
</script>
<template>
  <component :is="props.embedded ? 'div' : AppLayout"
    ><section v-if="auth.isSuperAdmin" class="space-y-6">
      <nav v-if="!embedded"
        class="flex gap-1 overflow-x-auto rounded-xl border border-gray-200 bg-white p-1 shadow-sm dark:border-dark-700 dark:bg-dark-800"
        :aria-label="t('vendor.title')"
      >
        <button
          v-for="item in tabs"
          :key="item"
          class="inline-flex items-center gap-2 whitespace-nowrap rounded-lg px-3 py-2.5 text-sm transition-colors"
          :class="
            tab === item
              ? 'bg-primary-50 font-semibold text-primary-700 dark:bg-primary-900/25 dark:text-primary-300'
              : 'text-gray-500 hover:bg-gray-50 hover:text-gray-800 dark:text-dark-400 dark:hover:bg-dark-700 dark:hover:text-gray-200'
          "
          :aria-current="tab === item ? 'page' : undefined"
          @click="router.replace({ query: { ...route.query, tab: item } })"
        >
          <Icon :name="tabIcons[item]" size="sm" aria-hidden="true" />
          {{ t('vendor.' + item) }}
        </button>
      </nav>
      <div
        v-if="tab !== 'suppliers'"
        class="space-y-3 rounded-2xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800"
      >
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('vendor.' + tab) }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('vendor.reportFilterHelp') }}</p>
          </div>
          <span class="badge badge-gray">{{ t('vendor.superAdminOnly') }}</span>
        </div>
        <form
          class="flex flex-wrap items-end gap-3 border-t border-gray-100 pt-3 dark:border-dark-700"
          @submit.prevent="
            page = 1
            loadVendors();
          "
        >
          <label
            >{{ t('vendor.search')
            }}<input v-model="search" class="input mt-1" /></label
          ><button class="btn btn-secondary">{{ t('vendor.search') }}</button>
          <div class="min-w-64 flex-1">
            <p class="mb-1 text-sm">{{ t('vendor.vendor') }}</p>
            <Select
              :model-value="vendorId ?? null"
              :options="vendorOptions"
              :aria-label="t('vendor.vendor')"
              @update:model-value="
                vendorId =
                  typeof $event === 'number' && $event > 0 ? $event : undefined
              "
            />
          </div>
        </form>
        <Pagination
          v-if="total > 20"
          :page="page"
          :page-size="20"
          :total="total"
          :show-page-size-selector="false"
          @update:page="
            page = $event
            loadVendors();
          "
        />
      </div>
      <VendorManagement
        v-if="tab === 'suppliers'"
        @navigate="openVendorTab"
        @changed="
          loadVendors()
          loadVendor();
        "
      /><RequestState
        v-else
        :loading="loading || vendorsLoading || groupsLoading"
        :error="vendorsError || error || groupsError"
        @retry="retry"
        ><template v-if="tab === 'accounts' || tab === 'proxies'"
          ><AccountWorkbench
            v-if="tab === 'accounts'"
            :key="vendorId ?? 'all'"
            :scope="vendorId ? { admin: true, vendorId } : { admin: true, vendorId: 0, readOnly: true }"
            :vendor="vendor"
            :groups="usageGroups"
            @select-vendor="vendorId = $event" /><ProxyWorkbench
            v-else
            :key="vendorId ?? 'all'"
            :scope="vendorId ? { admin: true, vendorId } : { admin: true, vendorId: 0, readOnly: true }"
            @select-vendor="vendorId = $event" /></template
        ><UsageWorkbench
          v-else-if="tab === 'usage'"
          :key="'usage-' + vendorId"
          admin
          :vendor-id="vendorId"
          :groups="vendorId ? vendor?.groups : usageGroups"
          @draft="draftFromUsage" /><SettlementWorkbench
          v-else-if="tab === 'settlements'"
          :key="'settlements-' + vendorId"
          admin
          :vendor-id="vendorId"
          :vendor="vendor"
          :initial-draft="draftFilters"
          @draft-consumed="draftFilters = undefined" /><GroupSupply
          v-else-if="tab === 'supply'"
          :vendor-id="vendorId" /><VendorAudit
          v-else-if="tab === 'audit'"
          admin
          :vendor-id="vendorId"
      /></RequestState></section
  ></component>
</template>
