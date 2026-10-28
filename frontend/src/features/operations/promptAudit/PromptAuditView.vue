<template>
  <div>
    <div class="mx-auto max-w-[1600px]" :class="activeTab === 'config' && draft ? 'pb-28' : 'pb-8'">
      <header class="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <p class="text-xs font-semibold uppercase tracking-[0.16em] text-primary-600 dark:text-primary-400">{{ t('nav.securityAudit') }}</p>
          <h1 class="mt-1 text-2xl font-semibold tracking-tight text-gray-950 dark:text-white">{{ t('admin.promptAudit.title') }}</h1>
          <p class="mt-2 max-w-3xl text-sm text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.description') }}</p>
        </div>
        <div v-if="draft" class="text-right text-xs text-gray-500 dark:text-dark-400">
          <p>{{ t('admin.promptAudit.configVersion', { version: draft.config_version }) }}</p>
          <p v-if="draft.updated_at" class="mt-1">{{ formatDate(draft.updated_at) }}</p>
        </div>
      </header>

      <div v-if="loadErrors.config && !draft" role="alert" class="rounded-xl border border-red-200 bg-red-50 p-5 dark:border-red-900 dark:bg-red-950/30">
        <p class="text-sm text-red-700 dark:text-red-300">{{ loadErrors.config }}</p>
        <button type="button" class="btn btn-secondary btn-sm mt-3" @click="loadConfig">{{ t('admin.promptAudit.actions.retry') }}</button>
      </div>

      <template v-else>
        <div class="mb-4" role="tablist" :aria-label="t('admin.promptAudit.title')">
          <div class="tabs inline-flex">
            <button
              v-for="tab in pageTabs"
              :key="tab.id"
              type="button"
              role="tab"
              class="tab"
              :class="{ 'tab-active': activeTab === tab.id }"
              :aria-selected="activeTab === tab.id"
              :data-test="`tab-${tab.id}`"
              @click="activeTab = tab.id"
            >
              {{ tab.label }}
            </button>
          </div>
        </div>

        <main class="card px-4 sm:px-6 lg:px-8">
          <div v-show="activeTab === 'config'" data-test="tab-panel-config">
            <RuntimeOverview :runtime="runtime" :loading="loading.runtime" :error="loadErrors.runtime" @refresh="loadRuntime" />

            <template v-if="draft">
              <EndpointPool
                :endpoints="draft.endpoints"
                :probe-results="probeResults"
                :probing-ids="probingIds"
                @update:endpoints="updateEndpoints"
                @probe="runProbe"
              />
              <div v-if="loadErrors.groups" role="alert" class="mt-5 rounded-lg bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">{{ loadErrors.groups }}</div>
              <PolicyPanel :draft="draft" :groups="groups" @update:draft="replaceDraft" />
            </template>
          </div>

          <div v-show="activeTab === 'events'" data-test="tab-panel-events">
            <div
              v-if="draft?.enabled && !draft.store_pass_events"
              data-test="pass-events-disabled-notice"
              role="status"
              class="mt-6 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200"
            >
              <span>{{ t('admin.promptAudit.events.passEventsDisabled') }}</span>
              <button type="button" class="btn btn-secondary btn-sm" @click="activeTab = 'config'">
                {{ t('admin.promptAudit.events.openConfiguration') }}
              </button>
            </div>
            <EventWorkspace :can-delete="permissions.isSuperAdmin" :can-view-content="permissions.canReadPromptContent"
              :events="events.items"
              :total="events.total"
              :page="events.page"
              :page-size="events.page_size"
              :filters="filters"
              :selected-ids="selectedEventIds"
              :loading="loading.events"
              :error="loadErrors.events"
              @filters-change="handleFiltersChanged"
              @search="applyEventFilters"
              @selection="selectedEventIds = $event"
              @page="changePage"
              @page-size="changePageSize"
              @view="openEvent"
              @delete="requestSingleDelete"
              @batch-delete="requestBatchDelete"
              @preview-delete="requestFilterDeletePreview"
            />
          </div>
        </main>
      </template>
    </div>

    <div v-if="draft && activeTab === 'config'" class="fixed inset-x-0 bottom-0 z-30 border-t border-gray-200 bg-white/95 px-4 py-3 shadow-[0_-12px_35px_rgba(15,23,42,0.08)] backdrop-blur dark:border-dark-700/80 dark:bg-dark-900/95 dark:shadow-[0_-12px_35px_rgba(0,0,0,0.35)] lg:left-64">
      <div class="mx-auto flex max-w-[1600px] flex-wrap items-center justify-between gap-3">
        <div class="flex flex-wrap items-center gap-x-5 gap-y-2">
          <SaveToggle :label="t('admin.promptAudit.saveBar.enabled')" :model-value="draft.enabled" data-test="enabled-toggle" @update:model-value="setEnabled" />
          <SaveToggle :label="t('admin.promptAudit.saveBar.blocking')" :model-value="draft.blocking_enabled" :disabled="!draft.enabled" data-test="blocking-toggle" @update:model-value="setBlocking" />
          <SaveToggle :label="t('admin.promptAudit.saveBar.blockingLatestTurnOnly')" :model-value="draft.blocking_latest_turn_only" :disabled="!draft.enabled || !draft.blocking_enabled" data-test="blocking-latest-turn-only-toggle" @update:model-value="replaceDraft({ ...draft!, blocking_latest_turn_only: $event })" />
          <SaveToggle :label="t('admin.promptAudit.saveBar.storePass')" :model-value="draft.store_pass_events" data-test="store-pass-toggle" @update:model-value="replaceDraft({ ...draft!, store_pass_events: $event })" />
        </div>
        <div class="flex items-center gap-3">
          <span class="text-sm" :class="dirty ? 'text-amber-700 dark:text-amber-300' : 'text-gray-500 dark:text-dark-400'">
            {{ dirty ? t('admin.promptAudit.saveBar.dirty') : t('admin.promptAudit.saveBar.synced') }}
          </span>
          <button type="button" class="btn btn-secondary" :disabled="!dirty || loading.saving" @click="resetDraft">{{ t('common.reset') }}</button>
          <button type="button" class="btn btn-primary" :disabled="!dirty || loading.saving" data-test="save-config" @click="saveConfig">
            {{ loading.saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </div>
    </div>

    <ConfirmDialog
      :show="showBlockingConfirmation"
      :title="t('admin.promptAudit.blockingConfirm.title')"
      :message="t('admin.promptAudit.blockingConfirm.message')"
      :confirm-text="t('admin.promptAudit.blockingConfirm.confirm')"
      danger
      @confirm="confirmBlocking"
      @cancel="showBlockingConfirmation = false"
    />
    <ConfirmDialog
      :show="deleteRequest.mode !== ''"
      :title="t('admin.promptAudit.events.deleteConfirmTitle')"
      :message="t('admin.promptAudit.events.deleteConfirmMessage', { count: deleteRequest.ids.length })"
      :confirm-text="t('common.delete')"
      danger
      @confirm="confirmIDDelete"
      @cancel="clearDeleteRequest"
    />
    <FilterDeleteDialog
      :show="showFilterDelete"
      :initial-filters="filters"
      :preview="deletePreview"
      :previewing="loading.previewing"
      :deleting="loading.deleting"
      @close="closeFilterDelete"
      @preview="runFilterDeletePreview"
      @confirm="confirmFilterDelete"
      @criteria-change="clearDeletePreview"
    />
    <EventDetailDialog :show="showEventDetail" :event="activeEvent" :loading="loading.detail" @close="closeEventDetail" />
  </div>
</template>

<script setup lang="ts">
import { useOperationsRequest } from '@/features/operations/queries/useOperationsRequest'
const operationsRequests = useOperationsRequest()

import { useOperationsServices } from '@/features/operations/services'
const services = useOperationsServices()
const permissions = services.permissions

import { computed, defineComponent, h, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import { extractApiErrorCode, extractApiErrorMessage } from '@/shared/lib/apiError'
import RuntimeOverview from './components/RuntimeOverview.vue'
import EndpointPool from './components/EndpointPool.vue'
import PolicyPanel from './components/PolicyPanel.vue'
import EventWorkspace from './components/EventWorkspace.vue'
import EventDetailDialog from './components/EventDetailDialog.vue'
import FilterDeleteDialog from './components/FilterDeleteDialog.vue'
import promptAuditAPI from './api'
import type {
  PromptAuditDraft,
  PromptAuditEndpointDraft,
  PromptAuditEvent,
  PromptAuditGroup,
  PromptAuditRuntime,
  PromptDeletePreview,
  PromptEventFilters,
  PromptEventPage,
  PromptLoadErrors,
  PromptProbeResult,
} from './types'
import { buildUpdateRequest, cloneData, configToDraft, draftFingerprint, emptyEventFilters } from './viewModel'

const { t, locale } = useI18n()
type PromptAuditPageTab = 'config' | 'events'
const activeTab = ref<PromptAuditPageTab>('events')
const pageTabs = computed(() => [
  { id: 'events' as const, label: t('admin.promptAudit.tabs.events') },
  ...(services.permissions.value.isSuperAdmin ? [{ id: 'config' as const, label: t('admin.promptAudit.tabs.config') }] : []),
])
const serverConfig = ref<PromptAuditDraft | null>(null)
const draft = ref<PromptAuditDraft | null>(null)
const runtime = ref<PromptAuditRuntime | null>(null)
const groups = ref<PromptAuditGroup[]>([])
const events = reactive<PromptEventPage>({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
const filters = ref<PromptEventFilters>(emptyEventFilters())
const appliedFilters = ref<PromptEventFilters>(emptyEventFilters())
const selectedEventIds = ref<number[]>([])
const activeEvent = ref<PromptAuditEvent | null>(null)
const showEventDetail = ref(false)
const probeResults = reactive<Record<string, PromptProbeResult>>({})
const probingIds = ref<string[]>([])
const showFilterDelete = ref(false)
const deletePreview = ref<PromptDeletePreview | null>(null)
const deletePreviewFilters = ref<PromptEventFilters | null>(null)
const showBlockingConfirmation = ref(false)
const deleteRequest = reactive<{ mode: '' | 'single' | 'batch'; ids: number[] }>({ mode: '', ids: [] })
const loading = reactive({ config: false, runtime: false, groups: false, events: false, saving: false, detail: false, deleting: false, previewing: false })
const loadErrors = reactive<PromptLoadErrors>({ config: '', runtime: '', groups: '', events: '' })
const dirty = computed(() => draftFingerprint(draft.value) !== draftFingerprint(serverConfig.value))

const SaveToggle = defineComponent({
  inheritAttrs: false,
  props: { label: { type: String, required: true }, modelValue: { type: Boolean, required: true }, disabled: { type: Boolean, default: false } },
  emits: ['update:modelValue'],
  setup(props, { emit, attrs }) {
    return () => h('label', { class: ['flex items-center gap-2.5 text-sm', props.disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer'] }, [
      h('button', {
        ...attrs,
        type: 'button',
        role: 'switch',
        'aria-checked': props.modelValue,
        'aria-label': props.label,
        disabled: props.disabled,
        class: [
          'relative inline-flex h-6 w-11 shrink-0 items-center rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2',
          props.modelValue ? 'bg-primary-600' : 'bg-gray-300 dark:bg-dark-600',
          props.disabled ? 'cursor-not-allowed' : 'cursor-pointer',
        ],
        onClick: (event: MouseEvent) => {
          event.preventDefault()
          if (!props.disabled) emit('update:modelValue', !props.modelValue)
        },
      }, [
        h('span', {
          class: [
            'pointer-events-none inline-block h-5 w-5 rounded-full bg-white shadow transition-transform duration-200 ease-in-out',
            props.modelValue ? 'translate-x-5' : 'translate-x-0',
          ],
        }),
      ]),
      h('span', { class: 'select-none text-gray-700 dark:text-dark-200' }, props.label),
    ])
  },
})

function errorMessage(error: unknown, fallbackKey: string): string {
  const code = extractApiErrorCode(error)
  if (code) {
    const key = `admin.promptAudit.errors.${code}`
    const translated = t(key)
    if (translated !== key) return translated
  }
  return extractApiErrorMessage(error, t(fallbackKey))
}

async function loadConfig() {
  if (!services.permissions.value.isSuperAdmin) return

  const operation = operationsRequests.begin('loadConfig')
  if (!operation.isCurrent()) return


  loading.config = true
  loadErrors.config = ''
  try {
    const config = await operation.wait(promptAuditAPI.getConfig({ signal: operation.signal }))
    serverConfig.value = configToDraft(config)
    draft.value = configToDraft(config)
  } catch (error) {
    if (!operation.isCurrent()) return

    loadErrors.config = errorMessage(error, 'admin.promptAudit.errors.loadConfig')
  } finally {
    if (operation.isCurrent()) {

      loading.config = false

    }
  }

}
async function loadRuntime() {
  if (!services.permissions.value.isSuperAdmin) return

  const operation = operationsRequests.begin('loadRuntime')
  if (!operation.isCurrent()) return


  loading.runtime = true
  loadErrors.runtime = ''
  try { runtime.value = await operation.wait(promptAuditAPI.getRuntime({ signal: operation.signal })) }
  catch (error) {
    if (!operation.isCurrent()) return
    loadErrors.runtime = errorMessage(error, 'admin.promptAudit.errors.loadRuntime')
  }
  finally {
    if (operation.isCurrent()) {
      loading.runtime = false
    }
  }

}
async function loadGroups() {
  const operation = operationsRequests.begin('loadGroups')
  if (!operation.isCurrent()) return

  loading.groups = true
  loadErrors.groups = ''
  try { groups.value = await operation.wait(services.listGroups({ signal: operation.signal, includeInactive: true })) }
  catch (error) {
    if (!operation.isCurrent()) return
    loadErrors.groups = errorMessage(error, 'admin.promptAudit.errors.loadGroups')
  }
  finally {
    if (operation.isCurrent()) {
      loading.groups = false
    }
  }

}
async function loadEvents() {
  const operation = operationsRequests.begin('loadEvents')
  if (!operation.isCurrent()) return

  loading.events = true
  loadErrors.events = ''
  try {
    const result = await operation.wait(promptAuditAPI.listEvents(appliedFilters.value, events.page, events.page_size, { signal: operation.signal }))
    Object.assign(events, result)
    selectedEventIds.value = []
  } catch (error) {
    if (!operation.isCurrent()) return

    loadErrors.events = errorMessage(error, 'admin.promptAudit.errors.loadEvents')
  } finally {
    if (operation.isCurrent()) {

      loading.events = false

    }
  }

}
async function loadInitial() {
  const operation = operationsRequests.begin('loadInitial')
  if (!operation.isCurrent()) return
  try {

    await operation.wait(Promise.allSettled([loadConfig(), loadRuntime(), loadGroups(), loadEvents()]))

  } catch (error) {
    if (!operation.isCurrent()) return
    throw error
  }
}

function replaceDraft(value: PromptAuditDraft) { draft.value = cloneData(value) }
function updateEndpoints(value: PromptAuditEndpointDraft[]) {
  if (!draft.value) return
  replaceDraft({ ...draft.value, endpoints: value })
}
function setEnabled(value: boolean) {
  if (!draft.value) return
  replaceDraft({ ...draft.value, enabled: value, blocking_enabled: value ? draft.value.blocking_enabled : false })
}
function setBlocking(value: boolean) {
  if (!draft.value || !draft.value.enabled) return
  if (value && !draft.value.blocking_enabled) { showBlockingConfirmation.value = true; return }
  replaceDraft({ ...draft.value, blocking_enabled: value })
}
function confirmBlocking() {
  showBlockingConfirmation.value = false
  if (draft.value) replaceDraft({ ...draft.value, blocking_enabled: true })
}
function resetDraft() {
  if (serverConfig.value) draft.value = cloneData(serverConfig.value)
}
async function saveConfig() {
  if (!services.permissions.value.isSuperAdmin) return
  if (!draft.value || !dirty.value) return

  const operation = operationsRequests.begin('saveConfig')
  if (!operation.isCurrent()) return


  loading.saving = true
  try {
    const saved = await operation.wait(promptAuditAPI.updateConfig(buildUpdateRequest(draft.value), { signal: operation.signal }))
    serverConfig.value = configToDraft(saved)
    draft.value = configToDraft(saved)
    services.notifySuccess(t('admin.promptAudit.messages.saved'))
    await operation.wait(loadRuntime())
  } catch (error) {
    if (!operation.isCurrent()) return

    const code = extractApiErrorCode(error)
    services.notifyError(errorMessage(error, code === 'prompt_audit_config_conflict' ? 'admin.promptAudit.errors.prompt_audit_config_conflict' : 'admin.promptAudit.errors.saveConfig'))
  } finally {
    if (operation.isCurrent()) {

      loading.saving = false

    }
  }

}
async function runProbe(endpoint: PromptAuditEndpointDraft) {
  if (!services.permissions.value.isSuperAdmin) return
  if (probingIds.value.includes(endpoint.id)) return

  const operation = operationsRequests.begin('runProbe:' + endpoint.id)
  if (!operation.isCurrent()) return


  probingIds.value = [...probingIds.value, endpoint.id]
  try {
    const result = await operation.wait(promptAuditAPI.probeEndpoint(endpoint, { signal: operation.signal }))
    probeResults[endpoint.id] = result
    if (result.ok) services.notifySuccess(t('admin.promptAudit.messages.probeSucceeded'))
    else services.notifyError(`${result.error_code || result.status}: ${result.message}`)
  } catch (error) {
    if (!operation.isCurrent()) return

    services.notifyError(errorMessage(error, 'admin.promptAudit.errors.probe'))
  } finally {
    if (operation.isCurrent()) {

      probingIds.value = probingIds.value.filter((id) => id !== endpoint.id)

    }
  }

}

function handleFiltersChanged(value: PromptEventFilters) {
  filters.value = cloneData(value)
  clearDeletePreview()
}
function applyEventFilters(value: PromptEventFilters) {
  filters.value = cloneData(value)
  appliedFilters.value = cloneData(value)
  events.page = 1
  clearDeletePreview()
  void loadEvents()
}
function changePage(value: number) { events.page = value; void loadEvents() }
function changePageSize(value: number) { events.page_size = value; events.page = 1; void loadEvents() }
async function openEvent(id: number) {
  const operation = operationsRequests.begin('openEvent')
  if (!operation.isCurrent()) return

  showEventDetail.value = true
  loading.detail = true
  activeEvent.value = null
  try { activeEvent.value = await operation.wait(promptAuditAPI.getEvent(id, { signal: operation.signal })) }
  catch (error) {
    if (!operation.isCurrent()) return
    services.notifyError(errorMessage(error, 'admin.promptAudit.errors.loadDetail')); showEventDetail.value = false
  }
  finally {
    if (operation.isCurrent()) {
      loading.detail = false
    }
  }

}
function closeEventDetail() {
  operationsRequests.cancel('openEvent')
  loading.detail = false
  showEventDetail.value = false
  activeEvent.value = null
}
function requestSingleDelete(id: number) { deleteRequest.mode = 'single'; deleteRequest.ids = [id] }
function requestBatchDelete() { if (selectedEventIds.value.length) { deleteRequest.mode = 'batch'; deleteRequest.ids = [...selectedEventIds.value] } }
function clearDeleteRequest() { deleteRequest.mode = ''; deleteRequest.ids = [] }
async function confirmIDDelete() {
  if (!services.permissions.value.isSuperAdmin) return
  const operation = operationsRequests.begin('confirmIDDelete')
  if (!operation.isCurrent()) return

  const mode = deleteRequest.mode
  const ids = [...deleteRequest.ids]
  clearDeleteRequest()
  if (!mode || ids.length === 0) return
  loading.deleting = true
  try {
    const result = mode === 'single' ? await operation.wait(promptAuditAPI.deleteEvent(ids[0], { signal: operation.signal })) : await operation.wait(promptAuditAPI.batchDeleteEvents(ids, { signal: operation.signal }))
    services.notifySuccess(t('admin.promptAudit.messages.deleted', { count: result.deleted_events }))
    await operation.wait(Promise.allSettled([loadEvents(), loadRuntime()]))
  } catch (error) {
    if (!operation.isCurrent()) return
    services.notifyError(errorMessage(error, 'admin.promptAudit.errors.delete'))
  }
  finally {
    if (operation.isCurrent()) {
      loading.deleting = false
    }
  }

}
function clearDeletePreview() {
  operationsRequests.cancel('runFilterDeletePreview')
  loading.previewing = false
  deletePreview.value = null
  deletePreviewFilters.value = null
}
function requestFilterDeletePreview() {
  clearDeletePreview()
  showFilterDelete.value = true
}
function closeFilterDelete() {
  showFilterDelete.value = false
  clearDeletePreview()
}
async function runFilterDeletePreview(value: PromptEventFilters) {
  if (!services.permissions.value.isSuperAdmin) return
  const operation = operationsRequests.begin('runFilterDeletePreview')
  if (!operation.isCurrent()) return

  loading.previewing = true
  try {
    deletePreview.value = await operation.wait(promptAuditAPI.previewDelete(value, { signal: operation.signal }))
    deletePreviewFilters.value = cloneData(value)
  } catch (error) {
    if (!operation.isCurrent()) return

    clearDeletePreview()
    services.notifyError(errorMessage(error, 'admin.promptAudit.errors.previewDelete'))
  } finally {
    if (operation.isCurrent()) {
      loading.previewing = false
    }
  }

}
async function confirmFilterDelete(filters?: PromptEventFilters) {
  if (!services.permissions.value.isSuperAdmin) return
  if (loading.deleting) return

  const operation = operationsRequests.begin('confirmFilterDelete')
  if (!operation.isCurrent()) return


  loading.deleting = true
  try {
    let preview = deletePreview.value
    let previewFilters = deletePreviewFilters.value ? cloneData(deletePreviewFilters.value) : null
    // One-click path: no fresh preview (never requested, or cleared by a
    // criteria change) — mint the confirmation token on the fly from the
    // criteria the dialog just emitted, then delete in the same action.
    if ((!preview || !previewFilters) && filters) {
      preview = await operation.wait(promptAuditAPI.previewDelete(filters, { signal: operation.signal }))
      previewFilters = cloneData(filters)
    }
    if (!preview || !previewFilters) return
    const result = await operation.wait(promptAuditAPI.deleteEventsByFilter(previewFilters, preview, { signal: operation.signal }))
    closeFilterDelete()
    services.notifySuccess(t('admin.promptAudit.messages.deleted', { count: result.deleted_events }))
    await operation.wait(Promise.allSettled([loadEvents(), loadRuntime()]))
  } catch (error) {
    if (!operation.isCurrent()) return

    clearDeletePreview()
    services.notifyError(errorMessage(error, 'admin.promptAudit.errors.deleteConfirmation'))
  } finally {
    if (operation.isCurrent()) {
      loading.deleting = false
    }
  }

}
function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(value))
}

onMounted(loadInitial)

operationsRequests.onScopeChange(() => {
  serverConfig.value = null
  draft.value = null
  runtime.value = null
  groups.value = []
  Object.assign(events, { items: [], total: 0, page: 1, page_size: 20, pages: 0 })
  selectedEventIds.value = []
  activeEvent.value = null
  showEventDetail.value = false
  for (const key of Object.keys(probeResults)) delete probeResults[key]
  probingIds.value = []
  showFilterDelete.value = false
  deletePreview.value = null
  deletePreviewFilters.value = null
  showBlockingConfirmation.value = false
  Object.assign(deleteRequest, { mode: '', ids: [] })
  Object.assign(loading, { config: false, runtime: false, groups: false, events: false, saving: false, detail: false, deleting: false, previewing: false })
  Object.assign(loadErrors, { config: '', runtime: '', groups: '', events: '' })
  if (operationsRequests.scopeKey.value !== null) void loadInitial()
})
</script>
