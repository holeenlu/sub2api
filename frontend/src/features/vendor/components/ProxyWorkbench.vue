<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import ActionMenu from './ActionMenu.vue'
import Select from '@/components/common/Select.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import { formatDateTime } from '@/utils/format'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import { resourceAPI } from '../api'
import { useClipboard } from '@/composables/useClipboard'
import {
  boundedQueue,
  display,
  download,
  errorText,
  resourceOwnerScope,
  proxyAvailable,
  proxyProbeOutcome,
  selectionForPage
} from '../helpers'
import { useVendorPage } from '../useVendorPage'
import type { ImportResult, ProxyPage, ResourceScope, VendorProxy, ProxyQuality } from '../types'
import RequestState from './RequestState.vue'
import ProxyForm from './ProxyForm.vue'
import ProxyImport from './ProxyImport.vue'
import ProxyQualityReport from '@/components/common/ProxyQualityReport.vue'
import { proxyQualityOverallStatus, proxyQualityStatusClass, proxyQualityStatusKey } from '@/utils/proxyQuality'
const props = defineProps<{ scope: ResourceScope }>()
const emit = defineEmits<{ selectVendor: [vendorId: number] }>()
const readOnly = computed(() => props.scope.readOnly === true)
const showOwner = computed(() => props.scope.admin || readOnly.value)
const { t } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
const { copyToClipboard: copy } = useClipboard()
const capabilities = ref<ProxyPage['capabilities']>()
const filters = reactive({
    search: '',
    protocol: '',
    status: '',
    country: '',
    occupancy: '',
    start_date: '',
    end_date: '',
    sort_by: 'created_at',
    sort_order: 'desc'
  }),
  applied = ref({ ...filters })
const confirmTest = ref<{ ids: number[]; filter: typeof filters; count: number }>()
const qualityReport = ref<{ name: string; report: ProxyQuality }>()
const summary = ref<ProxyPage['summary']>(),
  snapshot = ref('')
const list = useVendorPage<VendorProxy>((q, s) => resourceAPI(props.scope).proxies(q, s))
const { items, page, pageSize, total, loading, error, selected } = list
const editing = ref<VendorProxy>(),
  showForm = ref(false),
  importing = ref(false),
  detail = ref<VendorProxy>(),
  confirmDelete = ref(false),
  deleteResult = ref<ImportResult>(),
  actionError = ref(''),
  busy = ref(false),
  showResults = ref(false),
  testing = ref(false)
const testQuality = ref(false),
  testScope = ref('')
const runningTests = reactive(new Set<number>())
const connectionControllers = new Map<number, AbortController>()
const probeConcurrency = computed(() =>
  Math.max(1, Math.min(2, capabilities.value?.probe_concurrency || 2))
)
const probing = computed(() => testing.value || runningTests.size > 0)
const batchScope = computed(() =>
  t(selected.value.size ? 'vendor.testSelected' : 'vendor.testFiltered') +
  ` (${selected.value.size || total.value})`
)
const testResults = ref<
  {
    id: number
    name: string
    status: string
    detail?: string
    report?: ProxyQuality
  }[]
>([])
const testCounts = computed(() =>
  testResults.value.reduce<Record<string, number>>((counts, result) => {
    counts[result.status] = (counts[result.status] || 0) + 1
    return counts
  }, {})
)
const testCompleted = computed(() => testResults.value.filter(row => !['queued', 'running'].includes(row.status)).length)
const testSummary = computed(() =>
  t('vendor.proxyTestSummary', {
    scope: testScope.value,
    valid: testCounts.value.success || 0,
    invalid: testCounts.value.failed || 0,
    skipped: testCounts.value.skipped || 0
  })
)
const rowCache = new Map<number, VendorProxy>()
const deletePreview = ref<{ id: number; proxy?: VendorProxy; error?: string }[]>([])
const deleteLoading = ref(false)
let deleteController: AbortController | undefined
let testController: AbortController | undefined,
  scopeEpoch = 0
const canDelete = computed(
  () =>
    !readOnly.value &&
    confirmDelete.value &&
    !deleteLoading.value &&
    deletePreview.value.length > 0 &&
    deletePreview.value.every((row) => !!row.proxy?.references && !row.error)
)
const allSelected = computed(
  () => items.value.length > 0 && items.value.every((p) => selected.value.has(p.id))
)
async function load(reset = false, preserveRows = false) {
  const epoch = scopeEpoch
  if (filters.start_date && filters.end_date && filters.start_date > filters.end_date) {
    error.value = t('vendor.invalidDates')
    return
  }
  if (reset) {
    applied.value = { ...filters }
    rowCache.clear()
  }
  const response = (await list.load(applied.value, reset, preserveRows)) as ProxyPage | undefined
  if (response && epoch === scopeEpoch) {
    capabilities.value = response.capabilities
    summary.value = response.summary
    snapshot.value = response.snapshot_at || ''
    for (const p of response.items) rowCache.set(p.id, p)
  }
}
function cancelTests() {
  testing.value = false
  testController?.abort()
  for (const controller of connectionControllers.values()) controller.abort()
  connectionControllers.clear()
  runningTests.clear()
  for (const result of testResults.value)
    if (result.status === 'queued' || result.status === 'running') result.status = 'cancelled'
}
function closeDelete() {
  deleteController?.abort()
  confirmDelete.value = false
  deleteLoading.value = false
  deletePreview.value = []
}
function referenceIDs(ids?: number[]) {
  return ids
    ? ids.length
      ? ids.map((id) => '#' + id).join(', ')
      : t('common.none')
    : t('vendor.unknown')
}
async function loadDeleteReferences() {
  if (readOnly.value) return
  deleteController?.abort()
  const controller = new AbortController(),
    epoch = scopeEpoch,
    api = resourceAPI(props.scope)
  deleteController = controller
  deleteLoading.value = true
  const rows = deletePreview.value.map(
    ({ id }) => ({ id }) as { id: number; proxy?: VendorProxy; error?: string }
  )
  deletePreview.value = rows
  await boundedQueue(rows, controller.signal, async (row) => {
    try {
      const proxy = await api.proxy(row.id)
      if (!proxy.references) throw new Error(t('vendor.referencesUnavailable'))
      if (!controller.signal.aborted && epoch === scopeEpoch) row.proxy = proxy
    } catch (e) {
      if (!controller.signal.aborted && epoch === scopeEpoch) row.error = errorText(e)
    }
  })
  if (!controller.signal.aborted && epoch === scopeEpoch) {
    // Replace the array so the plain objects updated by the queue are reactive.
    deletePreview.value = [...rows]
    deleteLoading.value = false
  }
}
function deleteProxy(id: number) {
  if (readOnly.value) return
  selected.value = new Set([id])
  openDelete()
}
function openDelete() {
  if (readOnly.value || !selected.value.size || busy.value) return
  if (selected.value.size > (capabilities.value?.max_batch_size ?? 1000)) {
    actionError.value = t('vendor.scopeTooLarge')
    return
  }
  actionError.value = ''
  deletePreview.value = [...selected.value].map((id) => ({ id }))
  confirmDelete.value = true
  void loadDeleteReferences()
}
watch(
  () => [props.scope.admin, props.scope.vendorId, props.scope.readOnly, props.scope.children],
  () => {
    scopeEpoch++
    cancelTests()
    closeDelete()
    testing.value = false
    selected.value = new Set()
    rowCache.clear()
    detail.value = undefined
    editing.value = undefined
    showForm.value = false
    importing.value = false
    busy.value = false
    actionError.value = ''
    showResults.value = false
    confirmTest.value = undefined
    qualityReport.value = undefined
    testResults.value = []
    testQuality.value = false
    testScope.value = ''
    deleteResult.value = undefined
    summary.value = undefined
    capabilities.value = undefined
    snapshot.value = ''
    total.value = 0
    // Reload the applied query so invalid draft dates cannot retain another scope's rows.
    Object.assign(filters, applied.value)
    load(true)
  },
  { immediate: true, flush: 'sync' }
)
function openImport() {
  if (readOnly.value) return
  importing.value = true
}
async function edit(proxy?: VendorProxy) {
  if (readOnly.value) return
  busy.value = true
  actionError.value = ''
  const epoch = scopeEpoch
  try {
    const value = proxy ? await resourceAPI(props.scope).proxy(proxy.id) : undefined
    if (epoch === scopeEpoch) {
      editing.value = value
      showForm.value = true
    }
  } catch (e) {
    if (epoch === scopeEpoch) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch) busy.value = false
  }
}
async function showDetail(proxy: VendorProxy) {
  busy.value = true
  actionError.value = ''
  const epoch = scopeEpoch
  try {
    const result = await resourceAPI(resourceOwnerScope(props.scope, proxy.vendor_id)).proxy(proxy.id)
    if (epoch === scopeEpoch) detail.value = result
  } catch (e) {
    if (epoch === scopeEpoch) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch) busy.value = false
  }
}
async function remove() {
  if (readOnly.value || busy.value || !canDelete.value) return
  const ids = deletePreview.value.map((row) => row.id),
    epoch = scopeEpoch
  busy.value = true
  actionError.value = ''
  try {
    const result = await resourceAPI(props.scope).deleteProxies(ids)
    if (epoch !== scopeEpoch) return
    deleteResult.value = result
    closeDelete()
    selected.value = new Set()
    await load()
  } catch (e) {
    if (epoch === scopeEpoch) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch) busy.value = false
  }
}
async function exportRows(format: 'csv' | 'json') {
  if (readOnly.value) return
  const epoch = scopeEpoch
  busy.value = true
  actionError.value = ''
  try {
    const body = {
      format,
      ...(selected.value.size ? { ids: [...selected.value] } : { filter: applied.value })
    }
    const data = await resourceAPI(props.scope).proxyExport(body)
    if (epoch !== scopeEpoch || readOnly.value) return
    download(data, 'supplier-proxies.' + format)
  } catch (e) {
    if (epoch === scopeEpoch) actionError.value = errorText(e)
  } finally {
    if (epoch === scopeEpoch) busy.value = false
  }
}
async function testConnection(proxy: VendorProxy) {
  if (readOnly.value || testing.value || runningTests.has(proxy.id) || runningTests.size >= probeConcurrency.value) return
  if (!proxyAvailable(proxy)) {
    app.showInfo(t('vendor.proxyTestSkipped', { name: proxy.name }))
    return
  }
  const controller = new AbortController(),
    epoch = scopeEpoch,
    api = resourceAPI(props.scope)
  connectionControllers.set(proxy.id, controller)
  runningTests.add(proxy.id)
  try {
    const response = await api.testProxy(proxy.id, proxy.config_version, false, controller.signal)
    if (controller.signal.aborted || epoch !== scopeEpoch) return
    const outcome = proxyProbeOutcome(response)
    if (outcome.status === 'success') {
      const latency = (response.test as { latency_ms?: number } | undefined)?.latency_ms
      app.showSuccess(
        t('vendor.proxyTestAvailable', {
          name: proxy.name,
          latency: latency == null ? t('vendor.unknown') : `${latency} ms`
        })
      )
    } else if (outcome.status === 'skipped') {
      app.showInfo(t('vendor.proxyTestSkipped', { name: proxy.name }))
    } else {
      app.showError(
        t('vendor.proxyTestFailed', { name: proxy.name, reason: outcome.detail || t('vendor.unknown') })
      )
    }
    await load(false, true)
  } catch (e) {
    if (!controller.signal.aborted && epoch === scopeEpoch)
      app.showError(t('vendor.proxyTestFailed', { name: proxy.name, reason: errorText(e) }))
  } finally {
    if (connectionControllers.get(proxy.id) === controller) {
      connectionControllers.delete(proxy.id)
      runningTests.delete(proxy.id)
    }
  }
}
function openQuality(ids = [...selected.value]) {
  if (readOnly.value || probing.value) return
  if (ids.length === 1) { void tests(true, ids); return }
  confirmTest.value = { ids, filter: { ...applied.value }, count: ids.length || total.value }
}
function closeQualityResults() {
  if (testing.value) cancelTests()
  showResults.value = false
}
function viewQuality(proxy: VendorProxy) {
  if (proxy.quality) qualityReport.value = { name: proxy.name, report: proxy.quality }
}
async function tests(quality: boolean, ids = [...selected.value], filter = { ...applied.value }) {
  if (readOnly.value || probing.value) return
  confirmTest.value = undefined
  testController = new AbortController()
  const controller = testController,
    epoch = scopeEpoch,
    api = resourceAPI(props.scope)
  showResults.value = quality
  testQuality.value = quality
  testScope.value =
    t(ids.length ? 'vendor.testSelected' : 'vendor.testFiltered') + ` (${ids.length || total.value})`
  testing.value = true
  testResults.value = []
  actionError.value = ''
  // Freeze the clicked scope and its rows; filters and selection remain usable while probing.
  const rows = new Map(rowCache)
  const maxScope = capabilities.value?.max_scope_rows ?? 10000
  try {
    if (ids.length > Math.min(maxScope, capabilities.value?.max_batch_size ?? 1000))
      throw new Error(t('vendor.scopeTooLarge'))
    if (!ids.length) {
      let nextPage = 1,
        expected = 0
      const found = new Map<number, VendorProxy>()
      do {
        const response = await api.proxies(
          { ...filter, page: nextPage++, page_size: 50 },
          controller.signal
        )
        if (controller.signal.aborted || epoch !== scopeEpoch) return
        if (nextPage === 2) expected = response.total
        if (response.total !== expected) throw new Error(t('vendor.scopeTooLarge'))
        if (expected > Math.min(maxScope, response.capabilities?.max_scope_rows ?? 10000))
          throw new Error(t('vendor.scopeTooLarge'))
        for (const proxy of response.items) {
          found.set(proxy.id, proxy)
          rows.set(proxy.id, proxy)
        }
        if (!response.items.length) break
      } while (found.size < expected && nextPage <= 200)
      if (found.size !== expected) throw new Error(t('vendor.scopeTooLarge'))
      ids = [...found.keys()]
      testScope.value = t('vendor.testFiltered') + ` (${ids.length})`
    }
    testResults.value = ids.map((id) => ({
      id,
      name: rows.get(id)?.name || '#' + id,
      status: 'queued'
    }))
    await boundedQueue(
      testResults.value,
      controller.signal,
      async (result) => {
        if (readOnly.value || controller.signal.aborted || epoch !== scopeEpoch) return
        const proxy = rows.get(result.id)
        if (!proxy || !proxyAvailable(proxy)) {
          result.status = 'skipped'
          return
        }
        result.status = 'running'
        runningTests.add(result.id)
        try {
          const response = await api.testProxy(
            proxy.id,
            proxy.config_version,
            quality,
            controller.signal
          )
          if (controller.signal.aborted || epoch !== scopeEpoch) return
          Object.assign(result, proxyProbeOutcome(response))
          if (result.report) {
            proxy.quality = result.report
            const visible = items.value.find(item => item.id === proxy.id)
            if (visible) visible.quality = result.report
          }
        } catch (e) {
          if (!controller.signal.aborted && epoch === scopeEpoch) {
            result.status = 'failed'
            result.detail = errorText(e)
          }
        } finally {
          if (!controller.signal.aborted && epoch === scopeEpoch) runningTests.delete(result.id)
        }
      },
      probeConcurrency.value
    )
    if (controller.signal.aborted || epoch !== scopeEpoch) return
    if (!quality) {
      if (testResults.value.some((r) => r.status === 'failed')) app.showWarning(testSummary.value)
      else app.showSuccess(testSummary.value)
    }
    await load(false, true)
  } catch (e) {
    if (!controller.signal.aborted && epoch === scopeEpoch) {
      actionError.value = errorText(e)
      app.showError(actionError.value)
    }
  } finally {
    if (epoch === scopeEpoch && testController === controller) testing.value = false
  }
}
function proxyEndpoint(proxy: VendorProxy) {
  const value = proxy.endpoint || proxy.protocol + '://' + proxy.host + ':' + proxy.port
  if (!readOnly.value) return value
  try {
    const endpoint = new URL(value)
    if (['http:', 'https:', 'socks5:', 'socks5h:'].includes(endpoint.protocol) && endpoint.hostname)
      return endpoint.protocol + '//' + endpoint.host
  } catch {
    // Do not display malformed URLs that might contain credentials.
  }
  return t('vendor.unknown')
}
function copyEndpoint(proxy: VendorProxy) {
  if (readOnly.value) return
  void copy(proxyEndpoint(proxy))
}
onUnmounted(() => {
  scopeEpoch++
  cancelTests()
  closeDelete()
})
</script>
<template>
  <section class="space-y-4">
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h1 class="text-2xl font-semibold">{{ t('vendor.proxies') }}</h1>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('vendor.proxyDescription') }}</p>
      </div>
      <div class="flex flex-wrap gap-5 text-xs">
        <span
          v-for="[key, label] in [
            ['total', 'total'],
            ['occupied', 'occupied'],
            ['occupied_unavailable', 'occupiedUnavailable'],
            ['unoccupied', 'unoccupied']
          ] as const"
          :key="key"
          class="flex items-center gap-2"
          ><i
            class="h-3 w-0.5 rounded-full"
            :class="key === 'occupied_unavailable' ? 'bg-red-500' : 'bg-primary-500'"
          /><span class="text-gray-500 dark:text-gray-400">{{ t('vendor.' + label) }}</span
          ><strong class="text-base tabular-nums">{{ display(summary?.[key]) }}</strong></span
        >
      </div>
    </div>
    <div class="card overflow-hidden">
      <form
        class="vendor-filter-bar flex flex-wrap items-center gap-2 p-3"
        @submit.prevent="load(true)"
      >
        <input
          v-model="filters.search"
          class="input !w-44"
          :aria-label="t('vendor.search')"
          :placeholder="t('vendor.proxySearch')"
        />
        <Select
          v-model="filters.protocol"
          class="w-28"
          :options="[
            { value: '', label: t('vendor.allProtocols') },
            ...['http', 'https', 'socks5', 'socks5h'].map((value) => ({
              value,
              label: value.toUpperCase()
            }))
          ]"
          :aria-label="t('vendor.protocol')"
          @update:model-value="load(true)"
        />
        <Select
          v-model="filters.status"
          class="w-28"
          :options="[
            { value: '', label: t('vendor.allStatuses') },
            ...['active', 'inactive', 'expired'].map((value) => ({
              value,
              label: t('vendor.' + value)
            }))
          ]"
          :aria-label="t('vendor.status')"
          @update:model-value="load(true)"
        />
        <input
          v-model="filters.country"
          class="input !w-20"
          maxlength="2"
          :aria-label="t('vendor.country')"
          :placeholder="t('vendor.region')"
        />
        <Select
          v-model="filters.occupancy"
          class="w-28"
          :options="[
            { value: '', label: t('vendor.occupancy') },
            ...['occupied', 'unoccupied'].map((value) => ({ value, label: t('vendor.' + value) }))
          ]"
          :aria-label="t('vendor.occupancy')"
          @update:model-value="load(true)"
        />
        <details class="max-w-full">
          <summary class="cursor-pointer px-1 py-2 text-xs text-gray-500 dark:text-gray-400">
            {{ t('vendor.advancedFilters') }}
          </summary>
          <div class="flex flex-wrap gap-3 pt-2">
            <label
              >{{ t('vendor.startDate')
              }}<input v-model="filters.start_date" type="date" class="input mt-1"
            /></label>
            <label
              >{{ t('vendor.endDate')
              }}<input v-model="filters.end_date" type="date" class="input mt-1"
            /></label>
            <label
              >{{ t('vendor.sort')
              }}<Select
                v-model="filters.sort_by"
                :options="
                  [
                    ['name', 'name'],
                    ['protocol', 'protocol'],
                    ['account_count', 'accounts'],
                    ['latency', 'latency'],
                    ['expires_at', 'expiresAt'],
                    ['created_at', 'createdAt']
                  ].map(([value, label]) => ({ value, label: t('vendor.' + label) }))
                "
                :aria-label="t('vendor.sort')"
                class="min-w-0"
            /></label>
            <label
              >{{ t('vendor.sortOrder')
              }}<Select
                v-model="filters.sort_order"
                :options="[
                  { value: 'asc', label: t('vendor.ascending') },
                  { value: 'desc', label: t('vendor.descending') }
                ]"
                :aria-label="t('vendor.sortOrder')"
                class="min-w-0"
            /></label>
          </div>
        </details>
        <button
          class="btn btn-secondary btn-sm"
          :aria-label="t('vendor.apply')"
          :title="t('vendor.apply')"
        >
          <Icon name="search" size="sm" />
        </button>
        <button
          type="button"
          class="btn btn-ghost btn-sm"
          @click="
            () => {
              Object.assign(filters, {
                search: '',
                protocol: '',
                status: '',
                country: '',
                occupancy: '',
                start_date: '',
                end_date: ''
              })
              load(true)
            }
          "
        >
          {{ t('vendor.clear') }}
        </button>
        <div class="ml-auto flex flex-wrap items-center gap-2">
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="loading"
            :aria-label="t('vendor.refresh')"
            :title="t('vendor.refresh')"
            @click="load()"
          >
            <Icon name="refresh" size="sm" aria-hidden="true" />
          </button>
          <button
            v-if="!readOnly"
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="!(selected.size || total) || probing"
            :aria-busy="testing && !testQuality"
            :aria-label="testing && !testQuality ? testScope : batchScope"
            :title="testing && !testQuality ? testScope : batchScope"
            @click="tests(false)"
          >
            <LoadingSpinner v-if="testing && !testQuality" size="sm" />
            <Icon v-else name="play" size="sm" aria-hidden="true" />
            {{ testing && !testQuality ? testScope : batchScope }}
          </button>
          <button
            v-if="!readOnly"
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="!(selected.size || total) || probing"
            :aria-busy="testing && testQuality"
            :aria-label="t(selected.size ? 'vendor.qualitySelected' : 'vendor.qualityFiltered')"
            :title="t(selected.size ? 'vendor.qualitySelected' : 'vendor.qualityFiltered')"
            @click="openQuality()"
          >
            <LoadingSpinner v-if="testing && testQuality" size="sm" />
            <Icon v-else name="shield" size="sm" aria-hidden="true" />{{ t('admin.proxies.qualityCheck') }}
          </button>
          <button
            v-if="!readOnly && testing"
            type="button"
            class="btn btn-secondary btn-sm"
            @click="cancelTests"
          >
            {{ t('vendor.cancelQueue') }}
          </button>
          <button
            v-if="!readOnly && selected.size"
            type="button"
            class="btn btn-secondary btn-sm text-red-600"
            :disabled="busy"
            @click="openDelete"
          >
            <Icon name="trash" size="sm" />{{ t('vendor.delete') }}
          </button>
          <button v-if="!readOnly" type="button" class="btn btn-secondary btn-sm" @click="openImport">
            {{ t('vendor.import') }}
          </button>
          <ActionMenu v-if="!readOnly" :label="t('vendor.export')"
            ><button role="menuitem" type="button" :disabled="busy" @click="exportRows('csv')">
              CSV</button
            ><button role="menuitem" type="button" :disabled="busy" @click="exportRows('json')">
              JSON
            </button></ActionMenu
          >
          <button v-if="!readOnly" type="button" class="btn btn-primary btn-sm" @click="edit()">
            <Icon name="plus" size="sm" />{{ t('vendor.addProxy') }}
          </button>
        </div>
      </form>
      <p v-if="!readOnly && selected.size" class="px-3 pb-2 text-xs text-primary-600 dark:text-primary-400">
        {{ t('vendor.selected', { count: selected.size }) }}
      </p>
      <p v-if="!testQuality && testResults.length" role="status" class="px-3 pb-2 text-xs text-gray-600 dark:text-gray-300">
        {{ testSummary }}
        <template v-for="state in ['running', 'queued', 'cancelled']" :key="state">
          <span v-if="testCounts[state]"> · {{ t('vendor.' + state) }} {{ testCounts[state] }}</span>
        </template>
      </p>
      <p v-if="actionError" role="alert" class="px-3 pb-2 text-red-600 dark:text-red-400">
        {{ actionError }}
      </p>
      <p v-if="error && items.length" role="alert" class="px-3 pb-2 text-red-600 dark:text-red-400">
        {{ error }}
        <button class="btn btn-secondary btn-sm" @click="load(false, true)">{{ t('vendor.retry') }}</button>
      </p>
      <RequestState :loading="loading && !items.length" :error="items.length ? '' : error" :empty="!items.length" @retry="load()"
        ><div class="overflow-x-auto">
          <table class="table vendor-proxies-table" :aria-label="t('vendor.proxies')">
            <thead class="bg-gray-50 dark:bg-dark-800">
              <tr>
                <th v-if="!readOnly" class="p-3">
                  <input
                    type="checkbox"
                    :aria-label="t('vendor.selectPage')"
                    :checked="allSelected"
                    @change="
                      selected = selectionForPage(
                        selected,
                        items.map((p) => p.id),
                        ($event.target as HTMLInputElement).checked
                      )
                    "
                  />
                </th>
                <th
                  v-for="key in [
                    'resourceId',
                    'endpoint',
                    ...(showOwner ? ['resourceOwner'] : []),
                    'region',
                    'createdBy',
                    'sharingScope',
                    'occupancy',
                    'boundAccounts',
                    'createdAt',
                    'status',
                    ...(!readOnly ? ['actions'] : [])
                  ]"
                  :key="key"
                  class="whitespace-nowrap p-3"
                  :class="{ 'proxy-actions-cell': key === 'actions' }"
                >
                  {{ t('vendor.' + key) }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="p in items" :key="p.id" class="border-t border-gray-100 dark:border-dark-700">
                <td v-if="!readOnly" class="p-3">
                  <input
                    type="checkbox"
                    :checked="selected.has(p.id)"
                    :aria-label="t('vendor.select') + ' ' + p.name"
                    @change="
                      selected = selectionForPage(
                        selected,
                        [p.id],
                        ($event.target as HTMLInputElement).checked
                      )
                    "
                  />
                </td>
                <td class="p-3 text-gray-500 tabular-nums">{{ p.id }}</td>
                <td class="p-3">
                  <div class="flex items-center gap-2">
                    <span class="badge badge-primary text-[10px]">{{
                      p.protocol.toUpperCase()
                    }}</span>
                    <button
                      class="max-w-32 truncate font-medium"
                      :title="p.name"
                      @click="showDetail(p)"
                    >
                      {{ p.name }}
                    </button>
                    <span class="font-mono text-[11px]">{{
                      proxyEndpoint(p)
                    }}</span>
                    <Icon
                      v-if="!readOnly && (p.has_password || p.has_username)"
                      name="lock"
                      size="xs"
                      :title="t('vendor.configured')"
                    />
                    <button
                      v-if="!readOnly"
                      type="button"
                      class="text-gray-500 hover:text-primary-600 dark:text-gray-400 dark:hover:text-primary-400"
                      :aria-label="t('vendor.copy') + ' ' + p.name"
                      :title="t('vendor.copy')"
                      @click="copyEndpoint(p)"
                    >
                      <Icon name="copy" size="xs" aria-hidden="true" />
                    </button>
                  </div>
                </td>
                <td v-if="showOwner" class="p-3" data-resource-owner>
                  <button v-if="p.vendor_id" class="max-w-44 text-left hover:text-primary-600" @click="emit('selectVendor', p.vendor_id)">
                    <span class="block truncate" :title="p.vendor_name">{{ p.vendor_name || t('vendor.unknown') }}</span>
                    <span class="text-xs text-gray-400">#{{ p.vendor_id }}</span>
                  </button>
                  <span v-else>{{ t('vendor.unknown') }}</span>
                </td>
                <td
                  class="p-3"
                  :title="
                    t(
                      'vendor.regionSources.' +
                        (['label', 'detected'].includes(p.region_source || '')
                          ? p.region_source
                          : 'unknown')
                    )
                  "
                >
                  {{
                    [p.country_code || p.region_label_country_code, p.city || p.region_label_city]
                      .filter(Boolean)
                      .join(' · ') || '—'
                  }}
                </td>
                <td class="p-3">
                  {{
                    p.created_by
                      ? p.created_by === auth.user?.id
                        ? t('vendor.me')
                        : '#' + p.created_by
                      : '—'
                  }}
                </td>
                <td class="p-3 text-gray-500" :title="t('vendor.privateProxyScopeHelp')">
                  {{ t('vendor.privateProxyScope') }}
                </td>
                <td class="p-3">
                  <button
                    class="font-medium tabular-nums hover:text-primary-600"
                    @click="showDetail(p)"
                  >
                    {{ display(p.account_count) }}
                  </button>
                </td>
                <td class="p-3">
                  <button
                    v-if="p.account_ids?.length"
                    class="max-w-40 truncate text-primary-600"
                    :title="p.account_ids.map((id) => '#' + id).join(', ')"
                    @click="showDetail(p)"
                  >
                    {{ p.account_ids.map((id) => '#' + id).join(', ') }}
                  </button>
                  <span v-else-if="p.account_count === 0" class="text-gray-400">{{
                    t('vendor.unoccupied')
                  }}</span>
                  <button v-else class="text-primary-600" @click="showDetail(p)">
                    {{ t('vendor.details') }}
                  </button>
                </td>
                <td class="p-3 text-[11px] text-gray-500">
                  {{ formatDateTime(p.created_at) || '—' }}
                </td>
                <td class="p-3">
                  <div class="flex items-center gap-2">
                    <StatusBadge
                      :status="
                        (p.effective_status || p.status) === 'failed'
                          ? 'error'
                          : (p.effective_status || p.status) === 'expired'
                            ? 'warning'
                            : p.effective_status || p.status
                      "
                      :label="t('vendor.' + (p.effective_status || p.status))"
                    />
                    <span
                      v-if="p.latency_ms != null"
                      class="whitespace-nowrap text-[11px] tabular-nums"
                      :class="p.latency_ms > 1000 ? 'text-amber-700 dark:text-amber-400' : 'text-emerald-700 dark:text-emerald-400'"
                      >{{ p.latency_ms }} ms</span
                    >
                  </div>
                  <button v-if="p.quality" type="button" class="mt-2 flex items-center gap-1.5 text-xs text-gray-500 hover:text-primary-600 dark:text-gray-400 dark:hover:text-primary-400"
                    data-quality-report-trigger :aria-label="t('admin.proxies.qualityReportTitle') + ' ' + p.name"
                    :title="formatDateTime(new Date(p.quality.checked_at * 1000))" @click="viewQuality(p)">
                    <span>{{ t('admin.proxies.qualityInline', { grade: p.quality.grade, score: p.quality.score }) }}</span>
                    <span class="badge" :class="proxyQualityStatusClass(proxyQualityOverallStatus(p.quality))">{{ t(proxyQualityStatusKey(proxyQualityOverallStatus(p.quality))) }}</span>
                  </button>
                </td>
                <td v-if="!readOnly" class="proxy-actions-cell p-3">
                  <div v-if="!readOnly" class="flex items-center gap-1">
                    <button
                      type="button"
                      class="proxy-action"
                      :aria-label="t('vendor.test') + ' ' + p.name"
                      :title="t(runningTests.has(p.id) ? 'vendor.running' : 'vendor.test')"
                      :aria-busy="runningTests.has(p.id)"
                      :disabled="testing || runningTests.has(p.id) || runningTests.size >= probeConcurrency"
                      @click="testConnection(p)"
                    >
                      <LoadingSpinner v-if="runningTests.has(p.id) && !(testing && testQuality)" size="sm" />
                      <Icon v-else name="play" size="sm" aria-hidden="true" /><span>{{
                        t('vendor.test')
                      }}</span>
                    </button>
                    <button
                      type="button"
                      class="proxy-action"
                      :aria-label="t('admin.proxies.qualityCheck') + ' ' + p.name"
                      :title="t('admin.proxies.qualityCheck')"
                      :aria-busy="testing && testQuality && runningTests.has(p.id)"
                      :disabled="probing"
                      @click="openQuality([p.id])"
                    >
                      <LoadingSpinner v-if="testing && testQuality && runningTests.has(p.id)" size="sm" />
                      <Icon v-else name="shield" size="sm" aria-hidden="true" /><span>{{ t('admin.proxies.qualityCheck') }}</span>
                    </button>
                    <button
                      type="button"
                      class="proxy-action"
                      :aria-label="t('vendor.edit') + ' ' + p.name"
                      :title="t('vendor.edit')"
                      @click="edit(p)"
                    >
                      <Icon name="edit" size="sm" aria-hidden="true" /><span>{{
                        t('vendor.edit')
                      }}</span>
                    </button>
                    <button
                      type="button"
                      class="proxy-action hover:text-red-600"
                      :aria-label="t('vendor.delete') + ' ' + p.name"
                      :title="t('vendor.delete')"
                      :disabled="busy"
                      @click="deleteProxy(p.id)"
                    >
                      <Icon name="trash" size="sm" aria-hidden="true" /><span>{{
                        t('vendor.delete')
                      }}</span>
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div></RequestState
      >
      <Pagination
        v-if="total"
        :page="page"
        :page-size="pageSize"
        :page-size-options="[10, 20, 50]"
        :total="total"
        @update:page="
          ($event) => {
            page = $event
            load()
          }
        "
        @update:page-size="
          ($event) => {
            pageSize = $event
            page = 1
            load()
          }
        "
      />
    </div>
    <div v-if="deleteResult" class="rounded-xl border p-4">
      <h3>{{ t('vendor.results') }}</h3>
      <p v-for="(row, i) in deleteResult.items || deleteResult.rows" :key="i">
        {{ row.id ?? row.index ?? row.line ?? i + 1 }} ·
        {{
          t(
            'vendor.' +
              (row.status === 'deleted'
                ? 'success'
                : row.status === 'failed'
                  ? 'failed'
                  : 'unknown')
          )
        }}
        ·
        {{ row.code }}
      </p>
    </div>
  </section>
  <BaseDialog
    v-if="!readOnly && confirmTest"
    show
    :title="t('admin.proxies.qualityCheck')"
    @close="confirmTest = undefined"
    ><p>
      {{
        t('vendor.probeScope', {
          count: confirmTest.count
        })
      }}
    </p>
    <template #footer>
      <div class="flex justify-end gap-2">
        <button class="btn btn-secondary" @click="confirmTest = undefined">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" :disabled="probing" @click="tests(true, confirmTest.ids, confirmTest.filter)">{{ t('vendor.confirm') }}</button>
      </div>
    </template>
  </BaseDialog>
  <ProxyForm
    v-if="!readOnly && showForm"
    :scope="scope"
    :proxy="editing"
    @close="showForm = false"
    @saved="
      () => {
        showForm = false
        load()
      }
    "
  />
  <ProxyImport v-if="!readOnly && importing" :scope="scope" @close="importing = false" @saved="load()" />
  <BaseDialog
    v-if="!readOnly && confirmDelete"
    show
    :title="t('vendor.delete')"
    width="wide"
    :close-on-escape="!busy"
    :show-close-button="!busy"
    @close="closeDelete"
    ><p>{{ t('vendor.deleteProxyWarning') }}</p>
    <p class="my-3">
      {{ t('vendor.selected', { count: deletePreview.length }) }}
    </p>
    <p v-if="deleteLoading" role="status">{{ t('vendor.loading') }}</p>
    <div class="table-container my-3" data-delete-references>
      <table class="table">
        <thead>
          <tr>
            <th>{{ t('vendor.proxy') }}</th>
            <th>{{ t('vendor.accounts') }}</th>
            <th>{{ t('vendor.backups') }}</th>
            <th>{{ t('vendor.oauth') }}</th>
            <th>{{ t('vendor.inflight') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in deletePreview" :key="row.id">
            <td>
              {{ row.proxy?.name || rowCache.get(row.id)?.name }} · #{{ row.id }}
              <p v-if="row.error" role="alert" class="text-sm text-red-600">
                {{ row.error }}
              </p>
            </td>
            <td>{{ referenceIDs(row.proxy?.references?.accounts) }}</td>
            <td>{{ referenceIDs(row.proxy?.references?.backups) }}</td>
            <td>{{ row.proxy?.references?.oauth ?? t('vendor.unknown') }}</td>
            <td>
              {{ row.proxy?.references?.inflight ?? t('vendor.unknown') }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="actionError" role="alert" class="mb-3 text-red-600">
      {{ actionError }}
    </p>
    <button
      v-if="!deleteLoading && !canDelete"
      class="btn btn-secondary mr-2"
      :disabled="busy"
      @click="loadDeleteReferences"
    >
      {{ t('vendor.retry') }}
    </button>
    <button class="btn btn-danger" :disabled="busy || !canDelete" @click="remove">
      {{ t('vendor.confirm') }}
    </button></BaseDialog
  >
  <BaseDialog
    v-if="!readOnly && showResults" show :title="t('admin.proxies.qualityReportTitle')" width="wide"
    @close="closeQualityResults"
  >
    <div v-if="testing || testResults.length !== 1 || !testResults[0]?.report" class="mb-5 space-y-3 rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800" role="status" aria-live="polite">
      <div class="flex items-center justify-between gap-3">
        <p class="flex items-center gap-2 text-sm font-medium"><LoadingSpinner v-if="testing" size="sm" />{{ t('vendor.qualityProgress', { done: testCompleted, total: testResults.length || total }) }}</p>
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ testScope }}</span>
      </div>
      <div class="h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600" role="progressbar" :aria-label="t('admin.proxies.qualityCheck')" :aria-valuenow="testCompleted" :aria-valuemin="0" :aria-valuemax="testResults.length || 1">
        <div class="h-full bg-primary-500 transition-all" :style="{ width: (testResults.length ? testCompleted / testResults.length * 100 : 0) + '%' }" />
      </div>
      <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('vendor.qualityNetworkHelp') }}</p>
    </div>
    <p v-if="actionError" role="alert" class="mb-4 text-sm text-red-600 dark:text-red-400">{{ actionError }}</p>
    <div class="space-y-4">
      <section v-for="r in testResults" :key="r.id" :data-quality-result="r.id">
        <ProxyQualityReport v-if="r.report && testResults.length === 1" :name="r.name" :report="r.report" />
        <details v-else-if="r.report" class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
          <summary class="cursor-pointer text-sm font-medium">
            {{ r.name }} · {{ t('admin.proxies.qualityInline', { grade: r.report.grade, score: r.report.score }) }}
            <span class="badge ml-2" :class="proxyQualityStatusClass(proxyQualityOverallStatus(r.report))">{{ t(proxyQualityStatusKey(proxyQualityOverallStatus(r.report))) }}</span>
          </summary>
          <ProxyQualityReport class="mt-4" :name="r.name" :report="r.report" />
        </details>
        <div v-else class="flex flex-wrap items-center gap-3 rounded-xl border border-gray-200 p-4 text-sm dark:border-dark-600">
          <span class="min-w-0 flex-1 break-words font-medium">{{ r.name }}</span>
          <LoadingSpinner v-if="r.status === 'running'" size="sm" />
          <span :class="r.status === 'failed' ? 'text-red-600 dark:text-red-400' : 'text-gray-500 dark:text-gray-400'">{{ t('vendor.' + r.status) }}</span>
          <p v-if="r.detail" class="w-full break-words text-xs text-red-600 dark:text-red-400">{{ r.detail }}</p>
        </div>
      </section>
    </div>
    <template #footer>
      <div class="flex items-center justify-end gap-2">
        <button v-if="testing" class="btn btn-secondary" @click="cancelTests">{{ t('vendor.cancelQueue') }}</button>
        <button class="btn btn-secondary" @click="closeQualityResults">{{ t('common.close') }}</button>
      </div>
    </template>
  </BaseDialog>
  <BaseDialog v-if="qualityReport" show :title="t('admin.proxies.qualityReportTitle')" width="wide" @close="qualityReport = undefined">
    <ProxyQualityReport :report="qualityReport.report" :name="qualityReport.name" />
    <template #footer><div class="flex justify-end"><button class="btn btn-secondary" @click="qualityReport = undefined">{{ t('common.close') }}</button></div></template>
  </BaseDialog>
  <BaseDialog v-if="detail" show :title="detail.name" width="wide" @close="detail = undefined"
    ><dl class="grid grid-cols-2 gap-3">
      <template
        v-for="[key, value] in [
          ['proxy', proxyEndpoint(detail)],
          ['resourceOwner', (detail.vendor_name || t('vendor.unknown')) + (detail.vendor_id ? ' · #' + detail.vendor_id : '')],
          ['status', t('vendor.' + (detail.effective_status || detail.status))],
          ['protocol', detail.protocol.toUpperCase()],
          [
            'credentialAction',
            detail.has_username || detail.has_password
              ? t('vendor.configured')
              : detail.has_username === false && detail.has_password === false
                ? t('vendor.no')
                : t('vendor.unknown')
          ],
          ['expiresAt', formatDateTime(detail.expires_at)],
          [
            'region',
            [
              detail.country_code || detail.region_label_country_code,
              detail.city || detail.region_label_city
            ]
              .filter(Boolean)
              .join(' · ')
          ],
          ['sharingScope', t('vendor.privateProxyScope')],
          ['notes', detail.vendor_notes],
          ['createdAt', formatDateTime(detail.created_at)],
          ['actor', detail.created_by],
          ['version', detail.config_version],
          ['accounts', referenceIDs(detail.references?.accounts)],
          ['backups', referenceIDs(detail.references?.backups)],
          ['oauth', detail.references?.oauth],
          ['inflight', detail.references?.inflight],
          ['latency', detail.latency_ms],
          ['collectedAt', formatDateTime(detail.tested_at)]
        ].filter(([key]) => !readOnly || key !== 'credentialAction')"
        :key="String(key)"
        ><dt class="text-gray-500">
          {{ t('vendor.' + key) }}
        </dt>
        <dd class="break-words">{{ display(value) }}</dd></template
      >
    </dl>
    <ProxyQualityReport v-if="detail.quality" class="mt-5" :report="detail.quality" :name="detail.name" /></BaseDialog
  >
</template>

<style scoped>
.vendor-proxies-table {
  @apply bg-white text-gray-700 dark:bg-dark-900 dark:text-gray-200;
  min-width: 1100px;
  font-size: 12px;
}
.vendor-proxies-table :is(th, td) {
  @apply px-3 py-2;
}
.vendor-proxies-table :deep(.text-sm) {
  @apply text-xs;
}
.vendor-proxies-table th {
  @apply bg-gray-50 text-xs font-medium text-gray-600 dark:bg-dark-800 dark:text-gray-300;
}
.vendor-proxies-table .proxy-actions-cell {
  @apply sticky right-0 z-10 whitespace-nowrap bg-white dark:bg-dark-900;
  box-shadow: -1px 0 0 rgb(156 163 175 / 20%);
}
.vendor-proxies-table th.proxy-actions-cell {
  @apply z-20 bg-gray-50 dark:bg-dark-800;
}
.proxy-action {
  @apply relative inline-flex min-h-11 min-w-12 flex-col items-center justify-center gap-1 rounded-md px-1 py-1 text-[10px] text-gray-600 hover:bg-gray-100 hover:text-primary-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-50 dark:text-gray-300 dark:hover:bg-dark-700 dark:hover:text-primary-400;
}
form > label {
  @apply min-w-32 flex-1;
}
</style>
