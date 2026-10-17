<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getClientTelemetry, listClientTelemetry, type ClientTelemetryLog } from '@/api/admin/ops'
import { telemetryFromSample, type AccountTelemetry } from '@/utils/accountTelemetry'
import { useClipboard } from '@/composables/useClipboard'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ modelValue: AccountTelemetry | null }>()
const emit = defineEmits<{ 'update:modelValue': [value: AccountTelemetry | null] }>()
const { t } = useI18n()
const { copied, copyToClipboard } = useClipboard()
const show = ref(false)
const loading = ref(false)
const copying = ref(false)
const error = ref('')
const rows = ref<ClientTelemetryLog[]>([])
const page = ref(1)
const total = ref(0)
const userFilter = ref('')
let appliedUser: number | undefined
let generation = 0
let disposed = false
onBeforeUnmount(() => { disposed = true; generation++ })

async function load() {
  const serial = ++generation
  loading.value = true
  error.value = ''
  try {
    const result = await listClientTelemetry({ page: page.value, page_size: 10, time_range: 'all', user_id: appliedUser })
    if (serial !== generation || disposed) return
    rows.value = result.items
    total.value = result.total
  } catch (err) {
    if (serial === generation && !disposed) error.value = extractApiErrorMessage(err, t('admin.ops.clientTelemetry.loadFailed'))
  } finally { if (serial === generation && !disposed) loading.value = false }
}
function open() { show.value = true; page.value = 1; void load() }
function close() { generation++; show.value = false; copying.value = false; loading.value = false }
function search() {
  if (copying.value) return
  const raw = String(userFilter.value).trim()
  const value = raw ? Number(raw) : undefined
  if (value !== undefined && (!Number.isSafeInteger(value) || value <= 0)) { error.value = t('admin.ops.clientTelemetry.invalidID'); return }
  appliedUser = value
  page.value = 1
  void load()
}
function changePage(value: number) { page.value = value; void load() }
async function select(id: number) {
  const serial = ++generation
  copying.value = true
  error.value = ''
  try {
    const sample = await getClientTelemetry(id)
    if (serial !== generation || disposed) return
    const copy = telemetryFromSample(sample)
    if (!copy) { error.value = t('admin.accounts.telemetryCopy.invalidSample'); return }
    emit('update:modelValue', copy)
    copied.value = false
    close()
  } catch (err) {
    if (serial === generation && !disposed) error.value = extractApiErrorMessage(err, t('admin.ops.clientTelemetry.loadFailed'))
  } finally { if (serial === generation && !disposed) copying.value = false }
}
function clear() { emit('update:modelValue', null); copied.value = false }
</script>

<template>
  <section class="rounded-lg border border-gray-200 p-4 dark:border-dark-600" data-testid="account-telemetry-picker">
    <h3 class="text-sm font-medium">{{ t('admin.accounts.telemetryCopy.title') }}</h3>
    <p class="input-hint mt-1">{{ t('admin.accounts.telemetryCopy.hint') }}</p>
    <div class="mt-3 flex flex-wrap gap-2">
      <button type="button" class="btn btn-secondary" data-testid="account-telemetry-open" @click="open">{{ t('admin.accounts.telemetryCopy.choose') }}</button>
      <button v-if="props.modelValue" type="button" class="btn btn-secondary" data-testid="account-telemetry-clear" @click="clear">{{ t('admin.accounts.telemetryCopy.clear') }}</button>
    </div>
    <div v-if="props.modelValue" class="mt-3 space-y-2">
      <p class="text-sm">{{ props.modelValue.client === 'claude' ? 'Claude CLI' : 'Codex CLI' }} · {{ t('admin.accounts.telemetryCopy.independent') }}</p>
      <textarea readonly :value="props.modelValue.payload_json" rows="8" class="input font-mono text-xs" data-testid="account-telemetry-body" />
      <details class="text-xs"><summary>{{ t('admin.ops.clientTelemetry.headers') }}</summary><pre class="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-all">{{ JSON.stringify(props.modelValue.headers, null, 2) }}</pre></details>
      <button type="button" class="btn btn-secondary" @click="copyToClipboard(props.modelValue.payload_json)">{{ t(copied ? 'admin.ops.clientTelemetry.copied' : 'admin.ops.clientTelemetry.copy') }}</button>
    </div>
    <BaseDialog :show="show" :title="t('admin.accounts.telemetryCopy.choose')" width="extra-wide" @close="close">
      <div class="mb-3 flex items-end gap-2">
        <label class="text-xs">{{ t('admin.ops.clientTelemetry.user_id') }}<input v-model="userFilter" type="number" min="1" class="input mt-1" @keydown.enter.prevent="search" /></label>
        <button type="button" class="btn btn-secondary" :disabled="loading || copying" @click="search">{{ t('admin.ops.systemLogs.search') }}</button>
      </div>
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
      <p v-if="loading">{{ t('admin.ops.loadingText') }}</p>
      <div v-else-if="!error" class="space-y-2">
        <p v-if="!rows.length" class="text-sm text-gray-500">{{ t('admin.accounts.telemetryCopy.empty') }}</p>
        <div v-for="row in rows" :key="row.id" class="flex items-center justify-between gap-3 rounded border p-3 dark:border-dark-600">
          <div class="min-w-0 text-xs">
            <p>{{ row.client }} · {{ t('admin.ops.clientTelemetry.user_id') }} {{ row.user_id }} · {{ t('admin.ops.clientTelemetry.api_key_id') }} {{ row.api_key_id }}</p>
            <p class="truncate" :title="row.device_id">{{ row.device_id || t('admin.ops.clientTelemetry.noDevice') }} · {{ row.event_type }}</p>
            <p class="text-gray-500">{{ row.last_seen_at }}</p>
          </div>
          <button type="button" class="btn btn-secondary shrink-0" :disabled="copying" data-testid="account-telemetry-select" @click="select(row.id)">{{ t('admin.accounts.telemetryCopy.select') }}</button>
        </div>
        <Pagination v-if="total > 0 && !copying" :page="page" :page-size="10" :total="total" :show-page-size-selector="false" @update:page="changePage" />
      </div>
    </BaseDialog>
  </section>
</template>
