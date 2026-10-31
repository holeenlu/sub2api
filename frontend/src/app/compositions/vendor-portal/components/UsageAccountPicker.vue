<script setup lang="ts">
import { computed as supplyComputed, watch as watchSupplyScope } from 'vue'
import { useSupplyRequest } from '@/features/supply/useSupplyRequest'
import { useSupplyServices } from '@/features/supply/services'
const services = useSupplyServices()
const supplierRequestScope = supplyComputed(() => services.scopeKey.value === null ? null : JSON.stringify([services.scopeKey.value, props.vendorId, props.admin]))

import { computed, onUnmounted, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/shared/ui/Select.vue'
import { vendorAPI } from '@/features/supply/api'
import { errorText } from '@/features/supply'
import type { Query, UsageRow } from '@/features/supply/types'

const props = withDefaults(
  defineProps<{
    modelValue: string
    admin?: boolean
    vendorId?: number
  }>(),
  { admin: false }
)
const supplyRequests = useSupplyRequest(supplierRequestScope)

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useI18n()
const pageSize = 20
const page = ref(1),
  total = ref(0),
  search = ref(''),
  error = ref('')
const loading = ref(false),
  invalidSearch = ref(false)
const rows = shallowRef<UsageRow[]>([])
const known = shallowRef(new Map<number, UsageRow>())
let snapshot: Query = {}
let controller: AbortController | undefined
let generation = 0
let loaded = false
const scopeKey = computed(() => `${props.admin}:${props.vendorId ?? 'all'}`)
const pages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

function label(row: UsageRow) {
  return [
    row.account_name,
    '#' + row.account_id,
    props.admin && !props.vendorId
      ? t('vendor.usageSupplier') + ' ' + (row.vendor_id ? '#' + row.vendor_id : t('vendor.unknown'))
      : '',
    row.account_archived ? t('vendor.archived') : '',
    row.account_deleted ? t('vendor.deleted') : ''
  ]
    .filter(Boolean)
    .join(' · ')
}
const options = computed(() =>
  rows.value.map((row) => ({
    value: String(row.account_id),
    label: label(row)
  }))
)
const selectedLabel = computed(() => {
  const row = known.value.get(Number(props.modelValue))
  return row ? label(row) : props.modelValue ? '#' + props.modelValue : t('vendor.all')
})
function select(value: string | number | boolean | null) {
  if (value === null || value === '') emit('update:modelValue', '')
  else if (options.value.some((option) => option.value === value))
    emit('update:modelValue', String(value))
}
function cancel() {
  generation++
  controller?.abort()
  loading.value = false
}
async function load(nextPage = page.value) {
  const request = supplyRequests.begin('load')
  if (!request.isCurrent()) return

  cancel()
  rows.value = []
  error.value = ''
  if (invalidSearch.value) {
    error.value = t('vendor.accountIdSearchInvalid')
    return
  }
  if (
    props.vendorId !== undefined &&
    (!Number.isSafeInteger(props.vendorId) || props.vendorId <= 0)
  ) {
    error.value = t('vendor.selectVendor')
    return
  }
  controller = new AbortController()
  const signal = controller.signal,
    current = generation
  // Deliberately independent of the report's date, group, platform and snapshot.
  const query: Query = {
    all_history: true,
    dimension: 'account',
    page: nextPage,
    page_size: pageSize,
    ...snapshot,
    ...(search.value ? { account_id: Number(search.value) } : {}),
    ...(props.admin && props.vendorId ? { vendor_id: props.vendorId } : {})
  }
  loading.value = true
  try {
    const result = props.admin
      ? await request.wait(vendorAPI.adminUsage(query, signal))
      : await request.wait(vendorAPI.usage('accounts', query, signal))
    if (current !== generation || signal.aborted) return
    const unique = new Map<number, UsageRow>()
    for (const row of result.items) {
      if (!row.account_id || !Number.isSafeInteger(row.account_id) || row.account_id <= 0) continue
      if (props.vendorId && row.vendor_id !== props.vendorId) continue
      unique.set(row.account_id, row)
    }
    rows.value = [...unique.values()]
    known.value = new Map([...known.value, ...unique])
    page.value = result.page || nextPage
    total.value = result.total
    loaded = true
    snapshot = { snapshot_at: result.snapshot_at, watermark: result.watermark }
  } catch (cause) {
    if (!request.isCurrent()) return

    if (current === generation && !signal.aborted) error.value = errorText(cause)
  } finally {
    if (request.isCurrent()) {

      if (current === generation) loading.value = false

    }
  }

}
function find(query: string) {
  search.value = query.trim()
  invalidSearch.value =
    !!search.value &&
    (!/^[1-9]\d*$/.test(search.value) || !Number.isSafeInteger(Number(search.value)))
  page.value = 1
  total.value = 0
  snapshot = {}
  load()
}
function open() {
  // Native Select clears its search input on close. Reopen the full catalog too.
  if (search.value) find('')
  else if (!loaded && !loading.value) load()
}
watch(
  () => [props.admin, props.vendorId],
  (_, previous) => {
    cancel()
    known.value = new Map()
    search.value = ''
    invalidSearch.value = false
    page.value = 1
    total.value = 0
    snapshot = {}
    loaded = false
    rows.value = []
    error.value = ''
    if (previous) emit('update:modelValue', '')
  },
  { immediate: true }
)
onUnmounted(cancel)

watchSupplyScope(supplierRequestScope, () => {
  search.value = ''
  error.value = ''
  loading.value = false
  rows.value = []
}, { flush: 'sync' })
</script>

<template>
  <div class="min-w-0">
    <p class="mb-1 text-xs text-gray-500">{{ t('vendor.account') }}</p>
    <Select
      :key="scopeKey"
      :model-value="modelValue"
      :options="options"
      :aria-label="t('vendor.account')"
      :placeholder="t('vendor.all')"
      :search-placeholder="t('vendor.accountIdSearch')"
      :empty-text="t('vendor.empty')"
      :loading="loading"
      searchable
      remote
      clearable
      @update:model-value="select"
      @search="find"
      @open="open"
    >
      <template #selected>{{ selectedLabel }}</template>
      <template #footer>
        <div
          class="space-y-2 border-t border-gray-200 p-3 text-xs dark:border-dark-600"
          @keydown.enter.stop
          @keydown.space.stop
          @keydown.tab.stop
        >
          <p class="text-gray-500">{{ t('vendor.historicalAccountsHelp') }}</p>
          <div class="flex items-center justify-between gap-2">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="loading || !!error || page <= 1"
              @click.stop="load(page - 1)"
            >
              {{ t('pagination.previous') }}
            </button>
            <span aria-live="polite">{{ t('pagination.pageOf', { page, total: pages }) }}</span>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="loading || !!error || page >= pages"
              @click.stop="load(page + 1)"
            >
              {{ t('pagination.next') }}
            </button>
          </div>
        </div>
      </template>
    </Select>
    <p v-if="loading" role="status" class="mt-1 text-xs text-gray-500">
      {{ t('common.loading') }}
    </p>
    <p v-if="error" role="alert" class="mt-1 text-xs text-red-600">
      {{ error }}
      <button v-if="!invalidSearch" type="button" class="underline" @click="load()">
        {{ t('vendor.retry') }}
      </button>
    </p>
  </div>
</template>
