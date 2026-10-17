<script setup lang="ts">
import { onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getClientTelemetry, listClientTelemetry, type ClientTelemetryLog, type ClientTelemetryQuery } from '@/api/admin/ops'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ refreshToken?: number }>()
const { t } = useI18n()
const { copyToClipboard, copied } = useClipboard()
const expanded = ref(false)
const loading = ref(false)
const error = ref('')
const rows = ref<ClientTelemetryLog[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const filters = reactive({ user_id: '', api_key_id: '', session_id: '', time_range: '24h' })
let applied: ClientTelemetryQuery = { time_range: '24h' }
let listSerial = 0
let detailSerial = 0
const detail = ref<ClientTelemetryLog | null>(null)
const showDetail = ref(false)
const detailLoading = ref(false)
const detailError = ref('')
onBeforeUnmount(() => { listSerial++; detailSerial++ })

async function load() {
  const serial = ++listSerial
  loading.value = true
  error.value = ''
  try {
    const result = await listClientTelemetry({ ...applied, page: page.value, page_size: pageSize })
    if (serial !== listSerial) return
    rows.value = result.items
    total.value = result.total
  } catch (err) {
    if (serial === listSerial) { rows.value = []; total.value = 0; error.value = extractApiErrorMessage(err, t('admin.ops.clientTelemetry.loadFailed')) }
  } finally { if (serial === listSerial) loading.value = false }
}
function toggle() {
  expanded.value = !expanded.value
  if (expanded.value) void load()
}
function search() {
  const query: ClientTelemetryQuery = { time_range: filters.time_range, session_id: filters.session_id.trim() || undefined }
  for (const field of ['user_id', 'api_key_id'] as const) {
    if (!String(filters[field]).trim()) continue
    const value = Number(filters[field])
    if (!Number.isSafeInteger(value) || value <= 0) { error.value = t('admin.ops.clientTelemetry.invalidID'); return }
    query[field] = value
  }
  applied = query
  page.value = 1
  void load()
}
function changePage(value: number) { page.value = value; void load() }
async function openDetail(id: number) {
  const serial = ++detailSerial
  detail.value = null
  detailError.value = ''
  copied.value = false
  showDetail.value = true
  detailLoading.value = true
  try {
    const value = await getClientTelemetry(id)
    if (serial === detailSerial) detail.value = value
  } catch (err) {
    if (serial === detailSerial) detailError.value = extractApiErrorMessage(err, t('admin.ops.clientTelemetry.loadFailed'))
  } finally { if (serial === detailSerial) detailLoading.value = false }
}
function closeDetail() { detailSerial++; showDetail.value = false; detail.value = null }
watch(() => props.refreshToken, () => { if (expanded.value) void load() })
</script>

<template>
  <section class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
    <button type="button" class="text-left font-semibold" :aria-expanded="expanded" data-testid="telemetry-toggle" @click="toggle">
      {{ t('admin.ops.clientTelemetry.title') }} {{ expanded ? '▾' : '▸' }}
    </button>
    <p class="mt-1 text-sm text-gray-500">{{ t('admin.ops.clientTelemetry.description') }}</p>
    <template v-if="expanded">
      <p class="mt-2 text-xs text-gray-500">{{ t('admin.ops.clientTelemetry.retention') }}</p>
      <form class="my-4 grid gap-3 sm:grid-cols-3 lg:grid-cols-5" @submit.prevent="search">
        <label v-for="field in ['user_id', 'api_key_id'] as const" :key="field" class="text-xs">
          {{ t(`admin.ops.clientTelemetry.${field}`) }}
          <input v-model="filters[field]" type="number" min="1" step="1" class="input mt-1" :data-testid="`telemetry-${field}`" />
        </label>
        <label class="text-xs">{{ t('admin.ops.clientTelemetry.session') }}
          <input v-model="filters.session_id" maxlength="512" class="input mt-1" />
        </label>
        <label class="text-xs">{{ t('admin.ops.systemLogs.timeRange') }}
          <select v-model="filters.time_range" class="input mt-1"><option v-for="range in ['1h', '24h', '7d', '30d']" :key="range" :value="range">{{ range }}</option></select>
        </label>
        <button type="submit" class="btn btn-secondary self-end" :disabled="loading">{{ t('admin.ops.systemLogs.search') }}</button>
      </form>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <p v-if="loading" class="py-4 text-sm text-gray-500">{{ t('admin.ops.loadingText') }}</p>
      <div v-else class="overflow-x-auto">
        <table v-if="rows.length" class="w-full text-left text-sm">
          <thead><tr class="border-b text-xs text-gray-500">
            <th class="p-2">{{ t('admin.ops.clientTelemetry.lastSeen') }}</th>
            <th class="p-2">{{ t('admin.ops.clientTelemetry.source') }}</th>
            <th class="p-2">{{ t('admin.ops.clientTelemetry.device') }}</th>
            <th class="p-2">{{ t('admin.ops.clientTelemetry.count') }}</th>
            <th class="p-2">{{ t('admin.ops.clientTelemetry.detail') }}</th>
          </tr></thead>
          <tbody><tr v-for="row in rows" :key="row.id" class="border-b dark:border-dark-700">
            <td class="whitespace-nowrap p-2">{{ new Date(row.last_seen_at).toLocaleString() }}</td>
            <td class="p-2">{{ row.client }}<br />{{ row.user_id }} / {{ row.api_key_id }}</td>
            <td class="max-w-48 truncate p-2" :title="row.device_id">{{ row.device_id || t('admin.ops.clientTelemetry.noDevice') }}</td>
            <td class="p-2">{{ row.received_count }}</td>
            <td class="p-2"><button type="button" class="btn btn-secondary btn-sm" data-testid="telemetry-detail" @click="openDetail(row.id)">{{ t('admin.ops.clientTelemetry.detail') }}</button></td>
          </tr></tbody>
        </table>
        <p v-else class="py-6 text-center text-sm text-gray-500">{{ t('admin.ops.noData') }}</p>
      </div>
      <Pagination v-if="total > 0" :page="page" :page-size="pageSize" :total="total" :show-page-size-selector="false" @update:page="changePage" />
    </template>
    <BaseDialog :show="showDetail" :title="t('admin.ops.clientTelemetry.detail')" width="extra-wide" @close="closeDetail">
      <p v-if="detailLoading">{{ t('admin.ops.loadingText') }}</p>
      <p v-if="detailError" role="alert" class="text-red-600">{{ detailError }}</p>
      <div v-if="detail" class="space-y-3">
        <p class="text-xs text-gray-500">{{ detail.request_path }} · {{ detail.event_type }} · {{ detail.created_at }}</p>
        <p class="text-sm">{{ t('admin.ops.clientTelemetry.session') }}: {{ detail.session_id || '—' }}</p>
        <label class="block text-sm">{{ t('admin.ops.clientTelemetry.body') }}
          <textarea readonly :value="detail.payload_json" rows="14" class="input mt-1 font-mono text-xs" data-testid="telemetry-body" />
        </label>
        <button type="button" class="btn btn-secondary" data-testid="telemetry-copy" @click="copyToClipboard(detail.payload_json || '')">{{ t(copied ? 'admin.ops.clientTelemetry.copied' : 'admin.ops.clientTelemetry.copy') }}</button>
        <p class="text-sm">{{ t('admin.ops.clientTelemetry.headers') }}</p>
        <pre class="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-3 text-xs dark:bg-dark-900">{{ JSON.stringify(detail.headers, null, 2) }}</pre>
      </div>
    </BaseDialog>
  </section>
</template>
