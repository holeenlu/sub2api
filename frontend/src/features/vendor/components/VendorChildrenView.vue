<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Pagination from '@/components/common/Pagination.vue'
import { vendorManagementAPI } from '../api'
import { errorText } from '../helpers'
import type { ResourceScope, Vendor } from '../types'
import AccountWorkbench from './AccountWorkbench.vue'
import ProxyWorkbench from './ProxyWorkbench.vue'
import UsageWorkbench from './UsageWorkbench.vue'
import RequestState from './RequestState.vue'

const props = defineProps<{ parent: Vendor; kind: 'accounts' | 'proxies' | 'usage' }>()
const { t } = useI18n(), route = useRoute(), router = useRouter()
const vendors = ref<Vendor[]>([]), selected = ref<Vendor>(), search = ref('')
const page = ref(1), total = ref(0), loading = ref(false), error = ref('')
const listLoading = ref(false), listError = ref('')
const id = computed(() => Number(route.query.vendor_id) || undefined)
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
let epoch = 0, listEpoch = 0
let listRequest: AbortController | undefined
async function loadList() {
  const current = ++listEpoch
  listRequest?.abort()
  const controller = new AbortController()
  listRequest = controller
  listLoading.value = true
  listError.value = ''
  try {
    const result = await vendorManagementAPI(props.parent).list(
      { search: search.value, page: page.value, page_size: 20 }, controller.signal)
    if (current !== listEpoch) return
    vendors.value = result.items
    total.value = result.total
  } catch (e) {
    if (current === listEpoch) listError.value = errorText(e)
  } finally {
    if (current === listEpoch) listLoading.value = false
  }
}
async function loadSelected() {
  const current = ++epoch
  selected.value = undefined
  error.value = ''
  loading.value = !!id.value
  if (!id.value) return
  try {
    const value = await vendorManagementAPI(props.parent).detail(id.value)
    if (current === epoch) selected.value = value
  } catch (e) { if (current === epoch) error.value = errorText(e) }
  finally { if (current === epoch) loading.value = false }
}
watch(() => props.parent.id, () => { vendors.value = []; total.value = 0; page.value = 1; loadList() }, { immediate: true })
watch(() => [id.value, props.parent.id], loadSelected, { immediate: true })
onUnmounted(() => { epoch++; listEpoch++; listRequest?.abort() })
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
      <template v-if="!id || selected">
        <AccountWorkbench v-if="kind === 'accounts'" :key="id ?? 'children'" :vendor="selected" :groups="parent.groups" :scope="inventoryScope" @select-vendor="selectVendor" />
        <ProxyWorkbench v-else-if="kind === 'proxies'" :key="id ?? 'children'" :scope="inventoryScope" @select-vendor="selectVendor" />
        <UsageWorkbench v-else-if="selected" :key="selected.id" read-only :vendor-id="selected.id" :groups="selected.groups" />
        <p v-else class="p-6 text-center text-gray-500">{{ t('vendor.selectVendor') }}</p>
      </template>
      <p v-else class="p-6 text-center text-gray-500">{{ t('vendor.selectVendor') }}</p>
    </RequestState>
  </section>
</template>
