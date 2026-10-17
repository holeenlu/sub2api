<template>
  <BaseDialog :title="text('title')" width="wide" :show="show" @close="close">
    <template #header="{ titleId }">
      <div class="flex min-w-0 items-center gap-3 pr-8">
        <div class="rounded-xl bg-primary-50 p-3 text-primary-600 dark:bg-primary-900/25"><Icon name="sparkles" size="md" /></div>
        <div class="min-w-0"><h2 :id="titleId" class="text-lg font-semibold">{{ text('title') }}</h2><p class="truncate text-sm text-gray-500">{{ account?.name }} · OpenAI</p></div>
      </div>
      <button type="button" class="rounded-lg p-2 text-gray-500 hover:bg-gray-100 dark:hover:bg-dark-700" :aria-label="t('ui.closeModal')" @click="close"><Icon name="x" size="md" /></button>
    </template>
    <div class="max-h-[72vh] space-y-5 overflow-y-auto p-1">
      <p class="text-sm leading-6 text-gray-500 dark:text-dark-400">{{ text('description') }}</p>
      <div v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</div>
      <p v-if="loading" role="status" class="py-8 text-center">{{ text('loading') }}</p>
      <template v-else>
        <div class="grid gap-3 rounded-xl border border-gray-200 p-4 dark:border-dark-700 sm:grid-cols-3">
          <div><p class="text-xs text-gray-500">{{ text('conclusion') }}</p><CodexDiagnosticBadge :summary="summary" @open="tab = 'history'" /></div>
          <div><p class="text-xs text-gray-500">{{ text('lastCheck') }}</p><p class="mt-2 text-sm tabular-nums">{{ date(summary?.checked_at) }}</p></div>
          <div><p class="text-xs text-gray-500">{{ text('nextCheck') }}</p><p class="mt-2 text-sm tabular-nums">{{ savedPlan?.enabled ? date(savedPlan.next_run_at) : text('autoOff') }}</p></div>
        </div>
        <div class="flex gap-5 border-b border-gray-200 dark:border-dark-700" role="tablist">
          <button v-for="name in tabs" :key="name" type="button" role="tab" :aria-selected="tab === name" class="border-b-2 px-1 pb-3 text-sm font-medium"
            :class="tab === name ? 'border-primary-500 text-primary-600' : 'border-transparent text-gray-500'" @click="tab = name">{{ text(name) }}</button>
        </div>
        <section v-if="tab === 'settings'" class="space-y-4" role="tabpanel">
          <div class="grid gap-4 sm:grid-cols-2">
            <div><label class="mb-2 block text-sm font-medium">{{ text('key') }}</label>
              <Select v-model="keyID" :options="keyOptions" :disabled="saving || starting" /></div>
            <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
              <label class="flex cursor-pointer items-center justify-between gap-4">
                <span class="text-sm font-medium">{{ text('hourly') }}</span>
                <input v-model="enabled" data-testid="diagnostic-hourly" type="checkbox" :disabled="saving" class="h-4 w-4 rounded text-primary-600" />
              </label>
              <div class="mt-3 flex items-center justify-between gap-3">
                <label for="diagnostic-interval" class="text-xs text-gray-500">{{ text('intervalHours') }}</label>
                <input id="diagnostic-interval" v-model.number="intervalHours" type="number" :min="rules ? rules.min_interval_minutes / 60 : undefined" :max="rules ? rules.max_interval_minutes / 60 : undefined" step="1" :disabled="saving" class="input !min-h-9 w-24 !py-1 text-sm" />
              </div>
              <p class="mt-2 text-xs text-gray-500">{{ text('hourlyHint') }}</p>
            </div>
          </div>
          <p v-if="!validInterval" role="alert" class="text-sm text-amber-600">{{ text('intervalInvalid', intervalBounds) }}</p>
          <p class="text-xs leading-5 text-gray-500">{{ text('paidHint', { maxModels: rules?.max_models || 0 }) }}</p>
          <p v-if="keyMissing" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">{{ text('keyMissing') }}</p>
          <div class="flex items-center justify-between gap-3"><h3 class="text-sm font-semibold">{{ text('models') }} <span class="text-gray-400">{{ selectedModels.length }} / {{ rules?.max_models }}</span></h3>
            <button class="text-xs text-primary-600 disabled:opacity-50" type="button" :disabled="modelsLoading || !keyID" @click="loadModels(true)">{{ text('refreshModels') }}</button></div>
          <p v-if="modelsLoading" role="status" class="py-4 text-sm text-gray-500">{{ text('loading') }}</p>
          <CodexDiagnosticModelPicker v-else-if="models.length" v-model="selectedModels" :models="models" :disabled-reasons="modelReasons" :source-label="text('groupSource') + (groupName ? ' · ' + groupName : '') + ' · ' + eligibleModels.length + ' ' + text('eligibleCount')" :compact="true" :max-selected="rules?.max_models" :disabled="saving || starting" />
          <p v-else class="rounded-lg border border-dashed border-gray-300 p-6 text-center text-sm text-gray-500 dark:border-dark-600">{{ text(!keyID ? 'selectKey' : !whitelistEnabled ? 'whitelistRequired' : 'noModels') }}</p>
          <p v-if="unavailableModels.length" class="text-sm text-amber-600">{{ text('unavailable') }}: {{ unavailableModels.join(', ') }}</p>
          <details class="rounded-lg bg-gray-50 p-3 text-xs leading-6 text-gray-500 dark:bg-dark-800"><summary class="cursor-pointer">{{ text('method') }}</summary>
            <p>{{ text('methodHint', { confidence: confidencePercent }) }}</p><p class="break-all">{{ text('fingerprint') }}: {{ fingerprintCommit || '—' }}</p></details>
        </section>
        <section v-else class="space-y-3" role="tabpanel">
          <div class="flex items-center justify-between gap-3"><span class="text-xs text-gray-500">{{ text('historyHint', { historyLimit: rules?.history_limit || 0 }) }}</span><button type="button" class="text-sm text-primary-600" :disabled="historyLoading" @click="loadHistory()">{{ text('refresh') }}</button></div>
          <p v-if="!runs.length" class="py-10 text-center text-sm text-gray-500">{{ text('noHistory') }}</p>
          <details v-for="run in runs" :key="run.id" class="overflow-hidden rounded-xl border border-gray-200 dark:border-dark-700" :open="active(run) || selectedRun === run.id">
            <summary class="flex cursor-pointer list-none flex-wrap items-center justify-between gap-3 bg-gray-50 px-4 py-3 dark:bg-dark-800" @click.prevent="selectedRun = selectedRun === run.id ? null : run.id">
              <div><p class="text-sm font-medium">{{ date(run.created_at) }}</p><p class="mt-1 text-xs text-gray-500">#{{ run.id }} · {{ text(run.source) }} · {{ run.models.length }} {{ text('modelUnit') }}</p></div>
              <span class="rounded-md px-2 py-1 text-xs font-medium" :class="tone(run.status)">{{ status(run.status) }} · {{ run.items.length }}/{{ run.models.length }}</span>
            </summary>
            <div class="space-y-3 border-t border-gray-200 p-4 dark:border-dark-700">
              <div class="flex flex-wrap justify-between gap-2 text-xs text-gray-500"><span>{{ text('key') }}: {{ run.api_key_name || '—' }} (#{{ run.api_key_id || '—' }})</span><span>{{ text('finished') }}: {{ date(run.finished_at) }}</span></div>
              <p v-if="run.reason" class="text-sm text-amber-600">{{ reason(run.reason) }}</p>
              <div v-for="model in run.models" :key="model" class="rounded-lg border border-gray-100 p-3 dark:border-dark-700">
                <div class="flex flex-wrap items-center justify-between gap-2"><span class="font-mono text-sm">{{ model }}</span>
                  <span class="text-xs" :class="tone(item(run, model)?.status || (active(run) ? 'queued' : 'canceled'))">{{ status(item(run, model)?.status || (active(run) ? 'queued' : 'canceled')) }}</span></div>
                <template v-if="item(run, model)">
                  <div class="mt-3 grid grid-cols-2 gap-3 text-xs sm:grid-cols-4">
                    <div><p class="text-gray-500">{{ text('predicted') }}</p><p class="mt-1 break-all font-mono">{{ item(run, model)?.predicted_model || '—' }}</p></div>
                    <div><p class="text-gray-500">{{ text('probability') }}</p><p class="mt-1">{{ item(run, model)?.predicted_model ? ((item(run, model)?.probability || 0) * 100).toFixed(1) + '%' : '—' }}</p></div>
                    <div><p class="text-gray-500">{{ text('numbers') }}</p><p class="mt-1">{{ item(run, model)?.parsed_number_count || 0 }} / {{ item(run, model)?.expected_count || '—' }}</p></div>
                    <div><p class="text-gray-500">{{ text('duration') }}</p><p class="mt-1">{{ ((item(run, model)?.duration_ms || 0) / 1000).toFixed(1) }} s</p></div>
                  </div>
                  <p v-if="item(run, model)?.reason" class="mt-2 text-xs text-amber-600">{{ reason(item(run, model)?.reason) }}</p>
                  <p class="mt-2 break-all text-xs text-gray-500">HTTP {{ item(run, model)?.http_status || '—' }} <span v-if="item(run, model)?.gateway_error_code">· {{ item(run, model)?.gateway_error_code }}</span> · {{ text('fingerprint') }} {{ item(run, model)?.fingerprint_commit?.slice(0, 12) || '—' }}</p>
                </template>
              </div>
              <button v-if="active(run)" type="button" class="btn btn-secondary text-sm" :disabled="run.cancel_requested" @click="cancelRun(run)">{{ text(run.cancel_requested ? 'canceling' : 'cancel') }}</button>
            </div>
          </details>
        </section>
      </template>
    </div>
    <template #footer>
      <div class="flex w-full flex-wrap items-center justify-between gap-3">
        <p class="text-xs text-gray-500">{{ savedMessage || text('backgroundHint') }}</p>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary" :disabled="loading || saving || starting || !canSave" @click="save">{{ text(saving ? 'saving' : 'save') }}</button>
          <button type="button" class="btn btn-primary" data-action="start" :disabled="loading || saving || starting || !canRun" @click="start">{{ text(starting ? 'starting' : 'runNow') }}</button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import * as api from '@/api/admin/codexDiagnostics'
import type { Account, ApiKey } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import CodexDiagnosticModelPicker from './CodexDiagnosticModelPicker.vue'
import CodexDiagnosticBadge from './CodexDiagnosticBadge.vue'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits<{ close: []; completed: [accountID: number] }>()
const { t } = useI18n()
const text = (key: string, values: Record<string, string | number> = {}) => t('admin.accounts.codexMonitor.' + key, values)
const status = (key: string) => t('admin.accounts.codexMonitor.status.' + key)
const reason = (key?: string) => {
  if (!key) return ''
  const known = ['low_confidence', 'fingerprint_mismatch', 'configuration_unavailable', 'worker_interrupted', 'interrupted', 'canceled_or_settings_changed', 'gateway_request_failed', 'response_incomplete', 'insufficient_numbers', 'template_invalid', 'template_unavailable', 'identity_resolution_failed', 'request_build_failed', 'challenge_generation_failed', 'response_read_failed', 'response_too_large', 'storage_unavailable', 'internal_error']
  return known.includes(key) ? t('admin.accounts.codexMonitor.reason.' + key, { confidence: confidencePercent.value }) : key
}
const date = (value?: string | null) => value ? new Date(value).toLocaleString() : '—'
const tone = (value: string) => value === 'normal' ? 'text-teal-700 dark:text-teal-300' : value === 'degraded' ? 'text-red-700 dark:text-red-300' : 'text-amber-700 dark:text-amber-300'
const tabs = ['settings', 'history'] as const
const tab = ref<(typeof tabs)[number]>('settings')
const keys = ref<ApiKey[]>([])
const keyID = ref<number | null>(null)
const enabled = ref(false)
const rules = ref<api.DiagnosticRules | null>(null)
const intervalHours = ref<number | null>(null)
const confidencePercent = computed(() => Math.round((rules.value?.confidence_threshold || 0) * 100))
const intervalBounds = computed(() => ({ minHours: (rules.value?.min_interval_minutes || 0) / 60, maxHours: (rules.value?.max_interval_minutes || 0) / 60 }))
const validInterval = computed(() => rules.value != null && intervalHours.value != null && Number.isInteger(intervalHours.value) && intervalHours.value >= intervalBounds.value.minHours && intervalHours.value <= intervalBounds.value.maxHours)
const selectedModels = ref<string[]>([])
const models = ref<string[]>([])
const groupName = ref('')
const modelReasons = ref<Record<string, string>>({})
const eligibleModels = ref<string[]>([])
const whitelistEnabled = ref(true)
const fingerprintCommit = ref('')
const savedPlan = ref<api.DiagnosticPlan | null>(null)
const summary = ref<api.DiagnosticSummary | null>(null)
const runs = ref<api.DiagnosticRun[]>([])
const selectedRun = ref<number | null>(null)
const loading = ref(false)
const modelsLoading = ref(false)
const historyLoading = ref(false)
const saving = ref(false)
const starting = ref(false)
const error = ref('')
const savedMessage = ref('')
let generation = 0
let modelGeneration = 0
let timer: ReturnType<typeof setTimeout> | undefined
const active = (run: api.DiagnosticRun) => run.status === 'queued' || run.status === 'running'
const item = (run: api.DiagnosticRun, model: string) => run.items.find(entry => entry.model === model)
const keyOptions = computed(() => [{ value: null, label: text('selectKey') }, ...keys.value.map(k => ({ value: k.id, label: k.name + ' · #' + k.id }))])
const keyMissing = computed(() => !!keyID.value && !keys.value.some(k => k.id === keyID.value))
const unavailableModels = computed(() => selectedModels.value.filter(m => !eligibleModels.value.includes(m)))
const valid = computed(() => !!keyID.value && !keyMissing.value && selectedModels.value.length > 0 && rules.value != null && selectedModels.value.length <= rules.value.max_models && !modelsLoading.value && !unavailableModels.value.length)
const disabling = computed(() => !enabled.value && savedPlan.value != null && savedPlan.value.api_key_id === (keyID.value || 0) && JSON.stringify(savedPlan.value.models) === JSON.stringify(selectedModels.value))
const canSave = computed(() => validInterval.value && (valid.value || disabling.value))
const canRun = computed(() => validInterval.value && valid.value && !runs.value.some(active))
function current(g: number) { return g === generation && props.show && !!props.account }
function fail(e: unknown) { error.value = extractApiErrorMessage(e, text('error')) }
async function loadModels(refresh = false) {
  const g = generation; const m = ++modelGeneration; const id = props.account?.id; const key = keyID.value
  if (!id || !key || keyMissing.value) { models.value = []; eligibleModels.value = []; modelReasons.value = {}; modelsLoading.value = false; return }
  modelsLoading.value = true
  try {
    if (refresh) await api.refreshFingerprint()
    const result = await api.diagnosticModels(id, key)
    if (!current(g) || m !== modelGeneration) return
    eligibleModels.value = result.models
    models.value = result.items ? result.items.map(m => m.id) : result.models
    modelReasons.value = Object.fromEntries((result.items || []).filter(m => !m.eligible).map(m => [m.id, text(m.reason || 'fingerprint_unavailable')]))
    groupName.value = result.group_name || ''; whitelistEnabled.value = result.whitelist_enabled !== false
    fingerprintCommit.value = result.commit
  } catch (e) { if (current(g) && m === modelGeneration) { models.value = []; eligibleModels.value = []; fail(e) } }
  finally { if (current(g) && m === modelGeneration) modelsLoading.value = false }
}
async function loadHistory() {
  const g = generation; const id = props.account?.id
  if (!id || historyLoading.value) return
  historyLoading.value = true
  try {
    const result = await api.diagnosticRuns(id)
    if (!current(g)) return
    runs.value = result.items.slice(0, rules.value?.history_limit || 0)
  } catch (e) { if (current(g)) fail(e) }
  finally { if (current(g)) historyLoading.value = false }
}
async function save() {
  if (!props.account || !canSave.value) return false
  const g = generation; const id = props.account.id
  saving.value = true; error.value = ''; savedMessage.value = ''
  try {
    const p = await api.saveDiagnosticPlan(id, { api_key_id: keyID.value || 0, models: [...selectedModels.value], enabled: enabled.value, interval_minutes: Number(intervalHours.value) * 60 })
    if (!current(g)) return false
    savedPlan.value = p; savedMessage.value = text('saved')
    const state = await api.diagnosticPlan(id)
    if (!current(g)) return false
    summary.value = state.summary; emit('completed', id)
    return true
  } catch (e) { if (current(g)) fail(e); return false }
  finally { if (current(g)) saving.value = false }
}
async function start() {
  if (!props.account || !canRun.value) return
  const g = generation; const id = props.account.id
  starting.value = true
  try {
    if (!await save() || !current(g)) return
    const run = await api.startDiagnosticRun(id)
    if (!current(g)) return
    runs.value = [run, ...runs.value.filter(r => r.id !== run.id)].slice(0, rules.value?.history_limit || 0); selectedRun.value = run.id; tab.value = 'history'; emit('completed', id)
  } catch (e) { if (current(g)) fail(e) }
  finally { if (current(g)) starting.value = false }
}
async function cancelRun(run: api.DiagnosticRun) {
  const g = generation
  try { await api.cancelDiagnosticRun(run.account_id, run.id); if (current(g)) { run.cancel_requested = true; await loadHistory() } }
  catch (e) { if (current(g)) fail(e) }
}
function schedulePoll(g: number) {
  timer = setTimeout(async () => {
    if (!current(g)) return
    try {
      const id = props.account!.id
      const state = await api.diagnosticPlan(id)
      if (!current(g)) return
      const changed = JSON.stringify(summary.value) !== JSON.stringify(state.summary)
      summary.value = state.summary
      if (savedPlan.value && state.plan) savedPlan.value.next_run_at = state.plan.next_run_at
      if (runs.value.some(active) || changed) {
        await loadHistory()
        if (current(g) && changed) emit('completed', id)
      }
    } catch (e) { if (current(g)) fail(e) }
    finally { if (current(g)) schedulePoll(g) }
  }, 4000)
}
function close() { ++generation; ++modelGeneration; clearTimeout(timer); emit('close') }
watch(keyID, () => { if (!loading.value) void loadModels() })
watch(() => [props.show, props.account?.id], async () => {
  const g = ++generation; ++modelGeneration; clearTimeout(timer)
  if (!props.show || !props.account) return
  loading.value = true; error.value = ''; savedMessage.value = ''; tab.value = 'settings'
  models.value = []; eligibleModels.value = []; modelReasons.value = {}; groupName.value = ''; whitelistEnabled.value = true; keys.value = []; keyID.value = null; enabled.value = false; intervalHours.value = null; rules.value = null; selectedModels.value = []; runs.value = []; savedPlan.value = null; summary.value = null; selectedRun.value = null; saving.value = false; starting.value = false; historyLoading.value = false; modelsLoading.value = false
  const id = props.account.id
  try {
    const [state, available, history] = await Promise.all([api.diagnosticPlan(id), api.ownKeys(), api.diagnosticRuns(id)])
    if (!current(g)) return
    keys.value = available.filter(k => k.status === 'active' && (!k.quota || k.quota_used < k.quota) && (!k.expires_at || new Date(k.expires_at).getTime() > Date.now()))
    rules.value = state.rules; savedPlan.value = state.plan; summary.value = state.summary; keyID.value = state.plan?.api_key_id || null
    enabled.value = state.plan?.enabled || false; intervalHours.value = (state.plan?.interval_minutes || state.rules.default_interval_minutes) / 60; selectedModels.value = [...(state.plan?.models || [])]
    runs.value = history.items.slice(0, rules.value?.history_limit || 0)
    await loadModels()
  } catch (e) { if (current(g)) fail(e) }
  finally { if (current(g)) { loading.value = false; schedulePoll(g) } }
}, { immediate: true })
onUnmounted(() => { ++generation; ++modelGeneration; clearTimeout(timer) })
</script>
