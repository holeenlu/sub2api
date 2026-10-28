<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Select from '@/shared/ui/Select.vue'
import Pagination from '@/shared/ui/Pagination.vue'
import { useSupplyServices } from '@/features/supply/services'
import { useSupplyRequest } from '@/features/supply/useSupplyRequest'
import { vendorManagementAPI } from '@/features/supply/api'
import { errorText } from '@/features/supply'
import type { ResourceScope, Vendor } from '@/features/supply/types'
import AccountWorkbench from './AccountWorkbench.vue'
import ProxyWorkbench from './ProxyWorkbench.vue'
import UsageWorkbench from './UsageWorkbench.vue'
import { RequestState } from '@/features/supply/panels'

const props = defineProps<{ parent: Vendor; kind: 'accounts' | 'proxies' | 'usage' }>()
const services = useSupplyServices()
const childContext = computed(() => services.scopeKey.value === null ? null : JSON.stringify([services.scopeKey.value, props.parent.id, props.parent.policy_version]))
const requests = useSupplyRequest(childContext)
const { t } = useI18n(), route = useRoute(), router = useRouter()
const vendors = ref<Vendor[]>([]), selected = ref<Vendor>(), search = ref('')
const page = ref(1), total = ref(0), loading = ref(false), error = ref('')
const listLoading = ref(false), listError = ref('')
const selection = computed<{ kind: 'aggregate' } | { kind: 'child'; id: number } | { kind: 'invalid' }>(() => {
  const raw = route.query.vendor_id
  if (raw === undefined) return { kind: 'aggregate' }
  if (typeof raw !== 'string' || !/^\+?\d+$/.test(raw)) return { kind: 'invalid' }
  const value = Number(raw)
  return Number.isSafeInteger(value) && value > 0 ? { kind: 'child', id: value } : { kind: 'invalid' }
})
const id = computed(() => selection.value.kind === 'child' ? selection.value.id : undefined)
const invalidSelection = computed(() => selection.value.kind === 'invalid')
const inventoryScope = computed<ResourceScope>(() => id.value
  ? { admin: false, vendorId: id.value, readOnly: true }
  : { admin: false, children: true, readOnly: true })
function selectVendor(value: unknown) {
  router.replace({ query: { ...route.query, vendor_id: value ? String(value) : undefined } })
}
const options = computed(() => [
  { value: null, label: t(props.kind === 'usage' ? 'vendor.selectVendor' : 'vendor.allChildren') },
  ...(selected.value && !vendors.value.some(v => v.id === selected.value?.id) ? [{ value: selected.value.id, label: selected.value.name }] : []),
  ...vendors.value.map(v => ({ value: v.id, label: v.name + ' #' + v.id }))
])
async function loadList() {
  const request = requests.begin('children-list')
  if (!request.isCurrent()) return
  listLoading.value = true
  listError.value = ''
  try {
    const result = await request.wait(vendorManagementAPI(props.parent).list(
      { search: search.value, page: page.value, page_size: 20 }, request.signal))
    vendors.value = result.items
    total.value = result.total
  } catch (failure) {
    if (request.isCurrent()) listError.value = errorText(failure)
  } finally { if (request.isCurrent()) listLoading.value = false }
}
async function loadSelected() {
  const request = requests.begin('selected-child')
  selected.value = undefined
  error.value = invalidSelection.value ? t('vendor.unavailable') : ''
  loading.value = false
  if (invalidSelection.value || !id.value || !request.isCurrent()) return
  loading.value = true
  try {
    selected.value = await request.wait(vendorManagementAPI(props.parent).detail(id.value, { signal: request.signal }))
  } catch (failure) {
    if (request.isCurrent()) error.value = errorText(failure)
  } finally { if (request.isCurrent()) loading.value = false }
}
watch(childContext, () => {
  vendors.value = []; selected.value = undefined; total.value = 0; page.value = 1
  loading.value = false; listLoading.value = false; error.value = ''; listError.value = ''
  if (childContext.value !== null) { void loadList(); void loadSelected() }
}, { immediate: true, flush: 'sync' })
watch(() => [route.query.vendor_id, props.kind], loadSelected, { flush: 'sync' })
</script>
<template>
  <section class="space-y-4">
    <header><h1 class="text-2xl font-semibold">{{ t(kind === 'accounts' ? 'vendor.childAccounts' : kind === 'proxies' ? 'vendor.childProxies' : 'vendor.usage') }}</h1><p class="mt-2 text-sm text-gray-500 dark:text-dark-400">{{ t('vendor.childReadOnly') }}</p></header>
    <form class="flex flex-wrap items-end gap-3 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800" @submit.prevent="page = 1; loadList()">
      <label>{{ t('vendor.search') }}<input v-model="search" class="input mt-1" /></label>
      <button class="btn btn-secondary" :disabled="listLoading">{{ t('vendor.search') }}</button>
      <label class="min-w-64 flex-1">{{ t('vendor.childVendors') }}<Select :model-value="id ?? null" :options="options" :aria-label="t('vendor.childVendors')" class="mt-1" @update:model-value="selectVendor" /></label>
    </form>
    <p v-if="listError" role="alert" class="text-sm text-red-600">{{ listError }}</p>
    <Pagination v-if="total > 20" :page="page" :page-size="20" :total="total" :show-page-size-selector="false" @update:page="page = $event; loadList()" />
    <RequestState :loading="loading" :error="error" @retry="loadSelected">
      <template v-if="!invalidSelection && (!id || selected)">
        <AccountWorkbench v-if="kind === 'accounts'" :key="id ?? 'children'" :vendor="selected" :groups="parent.groups" :scope="inventoryScope" @select-vendor="selectVendor" />
        <ProxyWorkbench v-else-if="kind === 'proxies'" :key="id ?? 'children'" :scope="inventoryScope" @select-vendor="selectVendor" />
        <UsageWorkbench v-else-if="selected" :key="selected.id" read-only :vendor-id="selected.id" :groups="selected.groups" />
        <p v-else class="p-6 text-center text-gray-500">{{ t('vendor.selectVendor') }}</p>
      </template>
      <p v-else class="p-6 text-center text-gray-500">{{ t('vendor.selectVendor') }}</p>
    </RequestState>
  </section>
</template>
