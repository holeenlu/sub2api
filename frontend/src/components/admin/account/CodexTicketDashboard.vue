<template>
  <Teleport to="body">
    <div v-if="show && account" class="fixed inset-0 z-[10000] flex items-center justify-center bg-gray-950/60 p-2 backdrop-blur-sm sm:p-6" @click.self="emit('close')">
      <section role="dialog" aria-modal="true" :aria-label="text.title" class="flex max-h-[94dvh] w-full max-w-5xl flex-col overflow-hidden rounded-2xl border border-gray-200 bg-gray-50 shadow-2xl dark:border-dark-600 dark:bg-dark-900">
        <header class="flex items-start justify-between gap-3 border-b border-gray-200 bg-white px-5 py-4 dark:border-dark-700 dark:bg-dark-800 sm:px-7 sm:py-5">
          <div class="min-w-0 flex-1">
            <p class="truncate text-xs font-semibold text-primary-600 dark:text-primary-400">{{ account.name }} · CODEX</p>
            <h2 class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ text.title }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ text.retention }}</p>
          </div>
          <div class="flex shrink-0 items-center gap-2">
            <button type="button" class="btn btn-secondary !px-3 !py-1.5 !text-xs" :disabled="loading || accountLoading || savingParticipation" @click="reload">{{ text.refresh }}</button>
            <button type="button" class="rounded-lg p-2 text-gray-500 hover:bg-gray-100 dark:hover:bg-dark-700" :aria-label="text.close" @click="emit('close')">✕</button>
          </div>
        </header>
        <nav class="flex shrink-0 gap-1 overflow-x-auto border-b border-gray-200 bg-white px-3 dark:border-dark-700 dark:bg-dark-800 sm:px-6" :aria-label="text.tabs">
          <button v-for="item in tabs" :key="item.value" type="button" class="shrink-0 border-b-2 px-2.5 py-3 text-xs font-medium transition sm:px-4 sm:text-sm" :class="activeTab === item.value ? 'border-primary-500 text-primary-700 dark:text-primary-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-white'" :aria-current="activeTab === item.value ? 'page' : undefined" @click="switchTab(item.value)">{{ item.label }}</button>
        </nav>
        <div class="min-h-0 flex-1 overflow-y-auto px-4 py-5 sm:px-7">
          <div v-if="error" role="alert" class="mb-4 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-900/20 dark:text-red-300">{{ error }}</div>
          <div class="mb-5 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-gray-200 bg-white px-4 py-3 dark:border-dark-700 dark:bg-dark-800">
            <div class="flex items-baseline gap-2"><strong class="text-2xl font-semibold tabular-nums text-primary-600 dark:text-primary-400">{{ statuses.filter(status => status.ready).length }} / {{ statuses.length }}</strong><span class="text-sm text-gray-600 dark:text-dark-300">{{ text.available }}</span></div>
            <div class="flex max-w-full items-center gap-2 text-xs text-gray-500 dark:text-dark-400"><span class="truncate" :title="fingerprintCommit">{{ text.fingerprint }} {{ fingerprintCommit ? fingerprintCommit.slice(0, 12) : '—' }}</span><button type="button" class="shrink-0 font-medium text-primary-600 hover:underline disabled:opacity-50 dark:text-primary-400" :disabled="refreshingBank" @click="updateFingerprint">{{ text.updateBank }}</button></div>
          </div>

          <section v-if="activeTab === 'tickets'" :aria-label="text.current">
            <div class="mb-4 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
              <div class="flex items-center justify-between gap-4">
                <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ text.accountParticipation }}</span>
                <Toggle :model-value="participation.enabled" :disabled="participationDisabled" :aria-label="text.accountParticipation" class="disabled:cursor-not-allowed disabled:opacity-50" @update:model-value="saveParticipation($event)" />
              </div>
              <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ text.participationHint }}</p>
              <p v-if="savingParticipation" role="status" class="mt-2 text-xs text-gray-500">{{ text.savingParticipation }}</p>
              <p v-else-if="participationSaved" role="status" class="mt-2 text-xs text-primary-600">{{ text.participationSaved }}</p>
              <p v-if="participationError" role="alert" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ participationError }}</p>
            </div>
            <div v-if="!statuses.length" class="rounded-xl border border-gray-200 bg-white p-10 text-center text-sm text-gray-500 dark:border-dark-700 dark:bg-dark-800">{{ text.noModels }}</div>
            <div v-else class="grid gap-3 md:grid-cols-2">
              <article v-for="status in statuses" :key="status.model" class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800 sm:p-5">
                <div class="flex flex-wrap items-center justify-between gap-2"><h3 class="min-w-0 break-all font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ status.model }}</h3><span class="rounded-md px-2 py-1 text-xs font-medium" :class="statusClass(status)">{{ statusLabel(status) }}</span></div>
                <div class="mt-4 flex items-center justify-between gap-4">
                  <span class="text-xs text-gray-700 dark:text-dark-200">{{ text.modelParticipation }}</span>
                  <Toggle :model-value="participation.models[status.model] !== false" :disabled="participationDisabled" :aria-label="text.modelParticipation + ' · ' + status.model" class="disabled:cursor-not-allowed disabled:opacity-50" @update:model-value="saveParticipation($event, status.model)" />
                </div>
                <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ status.harvest_enabled ? text.harvestActive : text.harvestInactive }}</p>
                <dl class="mt-4 grid grid-cols-2 gap-x-3 gap-y-3 text-xs">
                  <div><dt class="text-gray-500 dark:text-dark-400">{{ text.acquired }}</dt><dd class="mt-1 text-gray-800 dark:text-dark-200">{{ formatTime(status.captured_at) }}</dd></div>
                  <div><dt class="text-gray-500 dark:text-dark-400">{{ text.length }}</dt><dd class="mt-1 font-medium tabular-nums text-gray-800 dark:text-dark-200">{{ status.length ?? '—' }}</dd></div>
                  <div><dt class="text-gray-500 dark:text-dark-400">turn-state</dt><dd class="mt-1 text-gray-800 dark:text-dark-200">{{ status.turn_state_present ? text.present : text.absent }}</dd></div>
                  <div><dt class="text-gray-500 dark:text-dark-400">Cookie</dt><dd class="mt-1 text-gray-800 dark:text-dark-200">{{ status.cookie_present ? text.present : text.absent }}</dd></div>
                </dl>
                <button type="button" class="btn btn-secondary mt-4 !px-3 !py-1.5 !text-xs" :disabled="busyModel !== '' || participationDisabled || !participation.enabled || participation.models[status.model] === false || !status.harvest_enabled" @click="harvestModel(status.model)">{{ busyModel === status.model ? text.running : text.manual }}</button>
              </article>
            </div>
          </section>

          <section v-else-if="activeTab === 'attempts' || activeTab === 'invalidations'" :aria-label="activeTab === 'attempts' ? text.attempts : text.invalidations" class="rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
            <div class="border-b border-gray-100 p-4 dark:border-dark-700 sm:p-5">
              <h3 class="font-semibold text-gray-900 dark:text-white">{{ activeTab === 'attempts' ? text.attempts : text.invalidations }}</h3>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ activeTab === 'attempts' ? text.attemptsDesc : text.invalidationsDesc }}</p>
              <div class="mt-4 flex flex-wrap items-center gap-2">
                <div v-if="activeTab === 'attempts'" class="w-full sm:w-40"><Select v-model="filter" :options="attemptOptions" :aria-label="text.filter" /></div>
                <div class="w-full sm:w-48"><Select v-model="modelFilter" :options="modelOptions" searchable :aria-label="text.model" /></div>
                <DateRangePicker v-model:start-date="startDay" v-model:end-date="endDay" />
              </div>
            </div>
            <div v-if="loading" role="status" class="p-10 text-center text-sm text-gray-500">{{ text.loading }}</div>
            <div v-else-if="!events.length" class="p-10 text-center text-sm text-gray-500">{{ text.empty }}</div>
            <div v-else class="divide-y divide-gray-100 dark:divide-dark-700">
              <template v-for="event in events" :key="`${event.kind}-${event.id}`">
                <div v-if="isNewDay(event)" class="bg-gray-50 px-5 py-2 text-xs font-semibold text-gray-500 dark:bg-dark-900/50">{{ formatDay(event.occurred_at) }}</div>
                <button type="button" class="flex w-full flex-col gap-2 px-4 py-3 text-left transition hover:bg-gray-50 dark:hover:bg-dark-700/60 sm:flex-row sm:items-center sm:gap-4 sm:px-5" @click="openDetail(event)">
                  <span class="shrink-0 self-start rounded-md px-2 py-1 text-xs font-medium" :class="eventBadge(event.kind)">{{ kindLabel(event.kind) }}</span>
                  <span class="min-w-0 flex-1"><span class="block break-all font-mono text-xs font-semibold text-gray-900 dark:text-white">{{ event.model }} <span v-if="event.kind !== 'invalidation' && !event.verification_method" class="font-sans font-normal text-amber-600">{{ text.legacy }}</span></span><span class="mt-1 block break-words text-xs text-gray-500 dark:text-dark-400">{{ eventSummary(event) }}</span></span>
                  <time class="shrink-0 text-xs tabular-nums text-gray-500 dark:text-dark-400">{{ formatTime(event.occurred_at, true) }}</time>
                </button>
              </template>
            </div>
            <div class="flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 px-5 py-3 text-xs text-gray-500 dark:border-dark-700"><span>{{ total }} {{ text.records }}</span><div class="flex items-center gap-2"><button type="button" class="btn btn-secondary !px-2 !py-1" :disabled="page <= 1 || loading" @click="page--">←</button><span>{{ page }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span><button type="button" class="btn btn-secondary !px-2 !py-1" :disabled="page * pageSize >= total || loading" @click="page++">→</button></div></div>
          </section>


        </div>
      </section>

      <div v-if="detail" class="fixed inset-0 z-[10001] flex items-center justify-center bg-gray-950/70 p-3" @click.self="closeDetail">
        <section role="dialog" aria-modal="true" :aria-label="text.details" class="max-h-[90vh] w-full max-w-4xl overflow-y-auto rounded-2xl bg-white p-5 shadow-2xl dark:bg-dark-800 sm:p-7">
          <div class="flex items-center justify-between gap-3"><h3 class="break-all text-lg font-semibold text-gray-900 dark:text-white">{{ text.details }} · {{ detail.model }}</h3><button type="button" class="p-2 text-gray-500" :aria-label="text.close" @click="closeDetail">✕</button></div>
          <p class="mt-1 text-xs text-gray-500">{{ formatTime(detail.occurred_at) }} · {{ kindLabel(detail.kind) }}</p>
          <div v-if="detail.kind === 'invalidation'" class="mt-5">
            <p class="mb-3 text-xs text-gray-500">{{ reasonLabel(detail.reason_code) }} · {{ detail.request_kind }} · {{ detail.request_route }} · HTTP {{ detail.response_http_status ?? '—' }}</p>
            <div v-if="detailLoading" class="text-sm text-gray-500">{{ text.loading }}</div>
            <div v-else-if="detailError" class="text-sm text-red-600">{{ detailError }}</div>
            <div v-else-if="rawDetail" class="grid gap-4 md:grid-cols-2"><div v-for="field in credentialFields" :key="field.label" class="min-w-0 rounded-xl border border-gray-200 p-4 dark:border-dark-600"><div class="flex items-center justify-between gap-2"><span class="text-xs font-semibold text-gray-500">{{ field.label }}</span><button v-if="field.value" type="button" class="text-xs font-semibold text-primary-600" @click="copy(field.value)">{{ text.copy }}</button></div><pre class="mt-2 max-h-40 overflow-auto whitespace-pre-wrap break-all font-mono text-xs text-gray-800 dark:text-dark-200">{{ field.value || (field.original ? text.notCarried : text.notReturned) }}</pre></div></div>
            <div v-if="rawDetail?.returned_set_cookies?.length" class="mt-4 rounded-xl border border-gray-200 p-4 dark:border-dark-600"><h4 class="text-xs font-semibold text-gray-500">Set-Cookie</h4><pre v-for="(cookie, index) in rawDetail.returned_set_cookies" :key="index" class="mt-2 overflow-auto whitespace-pre-wrap break-all font-mono text-xs">{{ cookie }}</pre></div>
          </div>
          <dl v-else class="mt-5 grid gap-3 text-sm sm:grid-cols-2"><div v-for="field in attemptFields" :key="field.label" class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700"><dt class="text-xs text-gray-500 dark:text-dark-400">{{ field.label }}</dt><dd class="mt-1 break-all font-medium text-gray-900 dark:text-white">{{ field.value }}</dd></div></dl>
        </section>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getIntlLocale } from '@/i18n/localeUtils'
import { adminAPI } from '@/api/admin'
import * as tickets from '@/api/admin/codexTickets'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { Account } from '@/types'

const props = defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits<{ close: []; updated: [account: Account] }>()
const { t, locale } = useI18n()
const text = computed(() => ({
  title: t('admin.accounts.codexTickets.title'),
  retention: t('admin.accounts.codexTickets.retention'),
  refresh: t('admin.accounts.codexTickets.refresh'),
  close: t('admin.accounts.codexTickets.close'),
  available: t('admin.accounts.codexTickets.available'),
  fingerprint: t('admin.accounts.codexTickets.fingerprint'),
  updateBank: t('admin.accounts.codexTickets.updateBank'),
  tabs: t('admin.accounts.codexTickets.tabs'),
  current: t('admin.accounts.codexTickets.current'),
  attempts: t('admin.accounts.codexTickets.attempts'),
  invalidations: t('admin.accounts.codexTickets.invalidations'),
  noModels: t('admin.accounts.codexTickets.noModels'),
  length: t('admin.accounts.codexTickets.length'),
  acquired: t('admin.accounts.codexTickets.acquired'),
  present: t('admin.accounts.codexTickets.present'),
  absent: t('admin.accounts.codexTickets.absent'),
  accountParticipation: t('admin.accounts.codexTickets.accountParticipation'),
  modelParticipation: t('admin.accounts.codexTickets.modelParticipation'),
  participationHint: t('admin.accounts.codexTickets.participationHint'),
  savingParticipation: t('admin.accounts.codexTickets.savingParticipation'),
  participationSaved: t('admin.accounts.codexTickets.participationSaved'),
  participationRefreshError: t('admin.accounts.codexTickets.participationRefreshError'),
  harvestActive: t('admin.accounts.codexTickets.harvestActive'),
  harvestInactive: t('admin.accounts.codexTickets.harvestInactive'),
  credentialsChanged: t('admin.accounts.codexTickets.credentialsChanged'),
  legacyStateChanged: t('admin.accounts.codexTickets.legacyStateChanged'),
  ready: t('admin.accounts.codexTickets.ready'),
  missing: t('admin.accounts.codexTickets.missing'),
  paused: t('admin.accounts.codexTickets.paused'),
  manual: t('admin.accounts.codexTickets.manual'),
  running: t('admin.accounts.codexTickets.running'),
  attemptsDesc: t('admin.accounts.codexTickets.attemptsDesc'),
  invalidationsDesc: t('admin.accounts.codexTickets.invalidationsDesc'),
  filter: t('admin.accounts.codexTickets.filter'),
  model: t('admin.accounts.codexTickets.model'),
  all: t('admin.accounts.codexTickets.all'),
  success: t('admin.accounts.codexTickets.success'),
  failure: t('admin.accounts.codexTickets.failure'),
  invalidation: t('admin.accounts.codexTickets.invalidation'),
  allModels: t('admin.accounts.codexTickets.allModels'),
  loading: t('admin.accounts.codexTickets.loading'),
  empty: t('admin.accounts.codexTickets.empty'),
  records: t('admin.accounts.codexTickets.records'),
  legacy: t('admin.accounts.codexTickets.legacy'),
  details: t('admin.accounts.codexTickets.details'),
  copy: t('admin.accounts.codexTickets.copy'),
  notCarried: t('admin.accounts.codexTickets.notCarried'),
  notReturned: t('admin.accounts.codexTickets.notReturned'),
  error: t('admin.accounts.codexTickets.error')
}))
type Tab = 'tickets' | 'attempts' | 'invalidations'
const activeTab = ref<Tab>('tickets')
const tabs = computed(() => [
  { value: 'tickets' as const, label: text.value.current },
  { value: 'attempts' as const, label: text.value.attempts },
  { value: 'invalidations' as const, label: text.value.invalidations }
])
const pageSize = 20
const page = ref(1)
const filter = ref('all')
const modelFilter = ref('')
const startDay = ref('')
const endDay = ref('')
const events = ref<tickets.TicketEvent[]>([])
const total = ref(0)
const loading = ref(false)
const error = ref('')
const fingerprintCommit = ref('')
const refreshingBank = ref(false)
const accountDetail = ref<Account | null>(null)
const participation = ref<tickets.TicketParticipation>({ enabled: true, models: {} })
const accountLoaded = ref(false)
const accountLoading = ref(false)
const savingParticipation = ref(false)
const participationSaved = ref(false)
const participationError = ref('')
const participationDisabled = computed(() => !accountLoaded.value || accountLoading.value || savingParticipation.value)
let accountLoadSerial = 0
let bankLoadSerial = 0
const busyModel = ref('')
let accountSessionSerial = 0
const detail = ref<tickets.TicketEvent | null>(null)
const rawDetail = ref<tickets.TicketInvalidation | null>(null)
const detailLoading = ref(false)
const detailError = ref('')
let loadSerial = 0
const statuses = computed(() => (accountDetail.value ?? props.account)?.codex_turn_tickets as tickets.TicketStatus[] | undefined ?? [])
const attemptOptions = computed(() => [
  { value: 'all', label: text.value.all }, { value: 'success', label: text.value.success }, { value: 'failure', label: text.value.failure }
])
const modelOptions = computed(() => [
  { value: '', label: text.value.allModels }, ...statuses.value.map(({ model }) => ({ value: model, label: model }))
])
function formatTime(value?: string, clockOnly = false) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : new Intl.DateTimeFormat(getIntlLocale(locale.value), { dateStyle: clockOnly ? undefined : 'short', timeStyle: 'medium' }).format(date)
}
function formatDay(value: string) { return new Intl.DateTimeFormat(getIntlLocale(locale.value), { dateStyle: 'full' }).format(new Date(value)) }
function isNewDay(event: tickets.TicketEvent) {
  const index = events.value.indexOf(event)
  return index === 0 || new Date(events.value[index - 1].occurred_at).toDateString() !== new Date(event.occurred_at).toDateString()
}
function statusClass(status: tickets.TicketStatus) {
  return status.ready ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
    : status.blocked || status.harvest_paused ? 'bg-amber-50 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
      : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'
}
function statusLabel(status: tickets.TicketStatus) { return status.ready ? text.value.ready : status.blocked || status.harvest_paused ? text.value.paused : text.value.missing }
function eventBadge(kind: string) {
  return kind === 'success' ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
    : kind === 'invalidation' ? 'bg-amber-50 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
      : 'bg-red-50 text-red-700 dark:bg-red-900/30 dark:text-red-300'
}
function kindLabel(kind: string) { return kind === 'success' ? text.value.success : kind === 'invalidation' ? text.value.invalidation : text.value.failure }
function reasonLabel(reason?: string) {
  if (reason === 'upstream_turn_state_and_oailb_changed') return text.value.credentialsChanged
  if (reason === 'upstream_new_turn_state') return text.value.legacyStateChanged
  return reason || ''
}
function eventSummary(event: tickets.TicketEvent) {
  return [reasonLabel(event.reason_code), event.ticket_length != null ? `${text.value.length} ${event.ticket_length}` : '',
    (event.http_status ?? event.response_http_status) != null ? `HTTP ${event.http_status ?? event.response_http_status}` : '',
    event.duration_ms != null ? `${event.duration_ms}ms` : '', event.proxy_name,
    event.fingerprint_predicted_model ? `top-1 ${event.fingerprint_predicted_model}` : ''].filter(Boolean).join(' · ') || '—'
}
const credentialFields = computed(() => rawDetail.value ? [
  { label: 'Original turn-state', value: rawDetail.value.original_ticket, original: true },
  { label: 'Original Cookie', value: rawDetail.value.original_cookie, original: true },
  { label: 'Returned turn-state', value: rawDetail.value.returned_ticket, original: false },
  { label: 'Returned Cookie', value: rawDetail.value.returned_cookie, original: false }
] : [])
const attemptFields = computed(() => {
  const event = detail.value
  if (!event) return []
  return [
    { label: 'Outcome / source', value: `${event.kind} / ${event.trigger || '—'}` },
    { label: 'Turn-state length', value: String(event.ticket_length ?? '—') },
    { label: 'Credentials', value: `turn-state ${event.turn_state_present == null ? '—' : event.turn_state_present ? '✓' : '×'} · Cookie ${event.cookie_present == null ? '—' : event.cookie_present ? '✓' : '×'}` },
    { label: 'Challenge / parsed', value: `${event.challenge_expected_count ?? '—'} / ${event.parsed_number_count ?? '—'}` },
    { label: 'Top-1 / probability', value: `${event.fingerprint_predicted_model || '—'} / ${event.fingerprint_probability == null ? '—' : `${(event.fingerprint_probability * 100).toFixed(2)}%`}` },
    { label: 'Fingerprint / version', value: `${event.verification_method || text.value.legacy} · ${event.fingerprint_commit || '—'}` },
    { label: 'Reason / HTTP', value: `${reasonLabel(event.reason_code) || '—'} · ${event.http_status ?? '—'}` },
    { label: 'Duration / proxy', value: `${event.duration_ms ?? '—'} ms · ${event.proxy_name || '—'}` }
  ]
})
function switchTab(tab: Tab) {
  if (activeTab.value === tab) return
  activeTab.value = tab
  if (tab === 'attempts' || tab === 'invalidations') { page.value = 1; loadEvents() }
}
async function loadEvents() {
  if (!props.show || !props.account || (activeTab.value !== 'attempts' && activeTab.value !== 'invalidations')) return
  const serial = ++loadSerial
  loading.value = true
  error.value = ''
  try {
    const cutoff = new Date(Date.now() - 90 * 86400000)
    const requestedStart = startDay.value ? new Date(`${startDay.value}T00:00:00`) : undefined
    const start = requestedStart && requestedStart > cutoff ? requestedStart : cutoff
    const end = endDay.value ? new Date(new Date(`${endDay.value}T00:00:00`).getTime() + 86400000) : undefined
    const data = await tickets.events(props.account.id, {
      model: modelFilter.value || undefined,
      filter: activeTab.value === 'invalidations' ? 'invalidation' : filter.value === 'all' ? 'attempts' : filter.value,
      page: page.value, page_size: pageSize, start_time: start.toISOString(), end_time: end?.toISOString()
    })
    if (serial === loadSerial) { events.value = data.items ?? []; total.value = data.total }
  } catch (cause) {
    if (serial === loadSerial) error.value = extractApiErrorMessage(cause, text.value.error)
  } finally { if (serial === loadSerial) loading.value = false }
}
function savedParticipation(account: Account): tickets.TicketParticipation {
  const raw = account.extra?.codex_ticket_harvest_models
  const models = raw && typeof raw === 'object' && !Array.isArray(raw)
    ? Object.fromEntries(Object.entries(raw).map(([model, enabled]) => [model, enabled !== false])) : {}
  return { enabled: account.extra?.codex_ticket_harvest_enabled !== false, models }
}
function isCurrentAccount(session: number, id: number) {
  return session === accountSessionSerial && props.show && props.account?.id === id
}
async function loadAccount(afterSave = false) {
  if (!props.show || !props.account || (savingParticipation.value && !afterSave)) return
  const id = props.account.id
  const session = accountSessionSerial
  const serial = ++accountLoadSerial
  const isCurrent = () => serial === accountLoadSerial && isCurrentAccount(session, id)
  accountLoading.value = true
  participationError.value = ''
  try {
    const updated = await adminAPI.accounts.getById(id)
    if (!isCurrent()) return
    accountDetail.value = updated
    participation.value = savedParticipation(updated)
    accountLoaded.value = true
    emit('updated', updated)
  } catch (cause) {
    if (isCurrent()) participationError.value = afterSave ? text.value.participationRefreshError : extractApiErrorMessage(cause, text.value.error)
  } finally { if (isCurrent()) accountLoading.value = false }
}
async function saveParticipation(enabled: boolean, model?: string) {
  if (participationDisabled.value || !props.show || !props.account) return
  const id = props.account.id
  const session = accountSessionSerial
  const before = participation.value
  const next = { enabled: model ? before.enabled : enabled, models: { ...before.models } }
  if (model) next.models[model] = enabled
  participation.value = next
  savingParticipation.value = true
  participationSaved.value = false
  participationError.value = ''
  ++accountLoadSerial
  try {
    const saved = await tickets.saveParticipation(id, next)
    if (!isCurrentAccount(session, id)) return
    participation.value = { enabled: saved.enabled, models: { ...saved.models } }
    participationSaved.value = true
    // Commit the successful PUT locally before refreshing. A failed GET must
    // not roll back settings which are already stored on the server.
    const updated = { ...(accountDetail.value ?? props.account), extra: {
      ...(accountDetail.value ?? props.account).extra,
      codex_ticket_harvest_enabled: saved.enabled, codex_ticket_harvest_models: { ...saved.models }
    } } as Account
    accountDetail.value = updated
    emit('updated', updated)
    await loadAccount(true)
  } catch (cause) {
    if (isCurrentAccount(session, id)) {
      participation.value = before
      participationError.value = extractApiErrorMessage(cause, text.value.error)
    }
  } finally { if (isCurrentAccount(session, id)) savingParticipation.value = false }
}
async function loadFingerprint(refresh = false) {
  const session = accountSessionSerial
  const serial = ++bankLoadSerial
  const isCurrent = () => session === accountSessionSerial && serial === bankLoadSerial && props.show
  if (refresh) refreshingBank.value = true
  try {
    const bank = await (refresh ? tickets.refreshFingerprint() : tickets.fingerprint())
    if (!isCurrent()) return
    fingerprintCommit.value = bank.commit
  } catch (cause) { if (isCurrent()) error.value = extractApiErrorMessage(cause, text.value.error) }
  finally { if (isCurrent()) refreshingBank.value = false }
}
async function updateFingerprint() {
  error.value = ''
  await loadFingerprint(true)
  await loadAccount()
}
async function reload() {
  if (!props.show || !props.account) return
  error.value = ''
  await Promise.all([loadAccount(), loadFingerprint(), loadEvents()])
}
async function harvestModel(model: string) {
  if (!props.account || busyModel.value || participationDisabled.value) return
  const id = props.account.id
  const session = accountSessionSerial
  busyModel.value = model
  error.value = ''
  try {
    const attempt = await tickets.harvest(id, model)
    if (!isCurrentAccount(session, id)) return
    await reload()
    if (isCurrentAccount(session, id) && attempt.outcome !== 'success') error.value = attempt.reason_code || attempt.outcome
  } catch (cause) { if (isCurrentAccount(session, id)) error.value = extractApiErrorMessage(cause, text.value.error) }
  finally { if (isCurrentAccount(session, id)) busyModel.value = '' }
}
async function openDetail(event: tickets.TicketEvent) {
  detail.value = event
  rawDetail.value = null
  detailError.value = ''
  if (event.kind !== 'invalidation' || !props.account) return
  detailLoading.value = true
  try {
    const data = await tickets.invalidation(props.account.id, event.id)
    if (detail.value === event) rawDetail.value = data
  } catch (cause) { detailError.value = extractApiErrorMessage(cause, text.value.error) }
  finally { detailLoading.value = false }
}
function closeDetail() { detail.value = null; rawDetail.value = null }
async function copy(value?: string | null) { if (value) await navigator.clipboard.writeText(value) }
watch(() => [props.show, props.account?.id], () => {
  ++accountSessionSerial
  ++accountLoadSerial
  ++bankLoadSerial
  accountLoaded.value = false
  accountLoading.value = false
  savingParticipation.value = false
  participationSaved.value = false
  participationError.value = ''
  refreshingBank.value = false
  busyModel.value = ''
  fingerprintCommit.value = ''
  error.value = ''
  loading.value = false
  ++loadSerial
  closeDetail()
  if (!props.show || !props.account) return
  activeTab.value = 'tickets'
  page.value = 1
  filter.value = 'all'
  modelFilter.value = ''
  startDay.value = ''
  endDay.value = ''
  accountDetail.value = props.account
  participation.value = savedParticipation(props.account)
  reload()
}, { immediate: true })
watch([filter, modelFilter, startDay, endDay], () => { if (activeTab.value !== 'attempts' && activeTab.value !== 'invalidations') return; page.value = 1; loadEvents() })
watch(statuses, current => {
  if (modelFilter.value && !current.some(status => status.model === modelFilter.value)) modelFilter.value = ''
})
watch(page, loadEvents)
onUnmounted(() => { ++accountSessionSerial; ++loadSerial })
</script>
