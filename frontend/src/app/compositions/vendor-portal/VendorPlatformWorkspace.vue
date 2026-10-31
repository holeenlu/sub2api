<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Pagination from '@/shared/ui/Pagination.vue'
import Select from '@/shared/ui/Select.vue'
import { VendorManagement, SettlementWorkbench, GroupSupply, VendorAudit, RequestState } from '@/features/supply/panels'
import AccountWorkbench from './components/AccountWorkbench.vue'
import ProxyWorkbench from './components/ProxyWorkbench.vue'
import UsageWorkbench from './components/UsageWorkbench.vue'
import { vendorAPI, type Query, type Vendor, type VendorGroup } from '@/features/supply'
import { errorText } from '@/features/supply'
import { useSupplyServices } from '@/features/supply/services'
import { useSupplyRequest } from '@/features/supply/useSupplyRequest'
import { useAuthStore } from '@/features/identity'

const { t } = useI18n(), route = useRoute(), router = useRouter(), auth = useAuthStore()
const services = useSupplyServices()
const context = computed(() => auth.isSuperAdmin ? services.scopeKey.value : null)
const requests = useSupplyRequest(context)
const tabs = ['suppliers', 'accounts', 'proxies', 'usage', 'settlements', 'supply', 'audit']
const tab = computed(() => tabs.includes(String(route.query.tab)) ? String(route.query.tab) : 'suppliers')
const vendorId = ref<number>(), vendor = ref<Vendor>(), vendors = ref<Vendor[]>([]), search = ref('')
const page = ref(1), total = ref(0), loading = ref(false), error = ref('')
const vendorsLoading = ref(false), vendorsError = ref('')
const usageGroups = ref<VendorGroup[]>([]), groupsLoading = ref(false), groupsError = ref('')
const draftFilters = ref<Query>()
const vendorOptions = computed(() => [
  { value: null, label: t('vendor.allVendors') },
  ...(vendor.value && !vendors.value.some(value => value.id === vendor.value?.id)
    ? [{ value: vendor.value.id, label: vendor.value.name }] : []),
  ...vendors.value.map(value => ({ value: value.id, label: value.name + ' · ' + t('vendor.' + value.status) })),
])

async function loadUsageGroups() {
  const request = requests.begin('usage-groups')
  usageGroups.value = []; groupsError.value = ''; groupsLoading.value = false
  if (!request.isCurrent() || !['usage', 'accounts'].includes(tab.value) || vendorId.value) return
  groupsLoading.value = true
  try { usageGroups.value = await request.wait(services.getAssignableGroups({ signal: request.signal })) }
  catch (failure) { if (request.isCurrent()) groupsError.value = errorText(failure) }
  finally { if (request.isCurrent()) groupsLoading.value = false }
}
async function loadVendors() {
  const request = requests.begin('vendor-catalog')
  if (!request.isCurrent()) return
  vendorsLoading.value = true; vendorsError.value = ''
  try {
    const response = await request.wait(vendorAPI.list({ page: page.value, page_size: 20, search: search.value }, request.signal))
    vendors.value = response.items; total.value = response.total
  } catch (failure) { if (request.isCurrent()) vendorsError.value = errorText(failure) }
  finally { if (request.isCurrent()) vendorsLoading.value = false }
}
async function loadVendor() {
  const request = requests.begin('vendor-detail')
  vendor.value = undefined; loading.value = false; error.value = ''
  if (!vendorId.value || !request.isCurrent()) return
  loading.value = true
  try { vendor.value = await request.wait(vendorAPI.detail(vendorId.value, { signal: request.signal })) }
  catch (failure) { if (request.isCurrent()) error.value = errorText(failure) }
  finally { if (request.isCurrent()) loading.value = false }
}
function retry() {
  if (vendorsError.value) void loadVendors()
  if (groupsError.value) void loadUsageGroups()
  if (error.value) void loadVendor()
}
function draftFromUsage(filters: Query) {
  draftFilters.value = { ...filters }
  void router.replace({ query: { ...route.query, tab: 'settlements' } })
}
function openVendorTab(target: 'accounts' | 'proxies' | 'usage' | 'settlements', id: number) {
  vendorId.value = id
  void router.replace({ query: { ...route.query, tab: target } })
}
watch([tab, vendorId, () => auth.isSuperAdmin], loadUsageGroups, { immediate: true, flush: 'sync' })
watch(vendorId, () => { draftFilters.value = undefined; void loadVendor() }, { flush: 'sync' })
watch(context, () => {
  vendorId.value = undefined; vendor.value = undefined; vendors.value = []; usageGroups.value = []
  page.value = 1; total.value = 0; search.value = ''; draftFilters.value = undefined
  loading.value = false; vendorsLoading.value = false; groupsLoading.value = false
  error.value = ''; vendorsError.value = ''; groupsError.value = ''
  if (context.value !== null) { void loadVendors(); void loadUsageGroups() }
}, { flush: 'sync' })
onMounted(() => { void loadVendors() })
</script>
<template>
  <div
    ><section v-if="auth.isSuperAdmin" class="space-y-6">

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
  ></div>
</template>
