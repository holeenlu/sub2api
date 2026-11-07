<template>
  <section class="space-y-3 border-t border-gray-200 pt-3 dark:border-dark-600" :aria-label="t('version.onlineUpdate.title')" :aria-busy="loading || checking || starting">
    <div class="flex items-center justify-between gap-2">
      <h3 class="font-medium">{{ t('version.onlineUpdate.title') }}</h3>
      <span class="text-gray-500 dark:text-dark-400">main</span>
    </div>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <p v-else-if="status && !status.enabled" role="status" class="text-gray-500 dark:text-dark-400">{{ t('version.onlineUpdate.disabled') }}</p>
    <template v-else-if="status?.enabled">
      <p v-if="status.target" data-testid="update-target">
        {{ t('version.onlineUpdate.target', { version: status.target.version }) }}
        <a v-if="releaseURL" :href="releaseURL" target="_blank" rel="noopener noreferrer" class="ml-2 text-primary-600 dark:text-primary-300">{{ t('version.onlineUpdate.releaseNotes') }}</a>
      </p>
      <p v-if="status.target?.requires_manual_migration" role="status" class="text-amber-700 dark:text-amber-300">{{ t('version.onlineUpdate.manualMigration') }}</p>
      <p v-else-if="status.target && !status.available && !status.job" role="status" class="text-gray-500 dark:text-dark-400">{{ t('version.onlineUpdate.current') }}</p>
      <p v-if="status.job && phaseText" role="status" data-testid="update-phase">{{ phaseText }}</p>
      <p v-if="running" role="status">{{ t('version.onlineUpdate.running') }}</p>
      <p v-if="reconnecting" role="status" class="text-amber-700 dark:text-amber-300">{{ t('version.onlineUpdate.reconnecting') }}</p>
      <p v-if="status.job?.state === 'failed'" role="status" class="text-red-700 dark:text-red-300">{{ t('version.onlineUpdate.failed') }}</p>
      <p v-if="status.job?.state === 'manual_intervention_required'" role="status" class="text-amber-700 dark:text-amber-300">{{ t('version.onlineUpdate.manualIntervention') }}</p>
      <div v-if="status.job?.state === 'succeeded'" class="space-y-2">
        <p role="status">{{ t('version.onlineUpdate.succeeded') }}</p>
        <button type="button" data-testid="update-reload" class="text-primary-600 dark:text-primary-300" @click="reloadPage">{{ t('version.onlineUpdate.reload') }}</button>
      </div>
      <button v-if="manualRecovery" type="button" class="text-primary-600 dark:text-primary-300" data-testid="update-recovery-status" :disabled="loading" @click="readStatus()">{{ t('version.refresh') }}</button>
      <div class="flex flex-wrap gap-3">
        <button type="button" data-testid="update-check" class="text-primary-600 disabled:opacity-50 dark:text-primary-300" :disabled="checking || starting || running || manualRecovery" @click="checkRelease">
          {{ checking ? t('common.loading') : t('version.onlineUpdate.check') }}
        </button>
        <button v-if="status.available" type="button" data-testid="update-start" class="text-primary-600 disabled:opacity-50 dark:text-primary-300" :disabled="!canStart" @click="confirmOpen = true">
          {{ starting ? t('common.loading') : t('version.onlineUpdate.install') }}
        </button>
      </div>
    </template>
    <div v-if="error" class="space-y-2">
      <p role="alert" class="text-red-700 dark:text-red-300">{{ error }}</p>
      <button v-if="!running" type="button" class="text-primary-600 dark:text-primary-300" data-testid="update-retry" :disabled="loading || checking || starting" @click="readStatus()">{{ t('version.refresh') }}</button>
    </div>
    <ConfirmDialog :show="confirmOpen" :title="t('version.onlineUpdate.confirmTitle')" :message="t('version.onlineUpdate.confirmMessage')" :confirm-text="t('version.onlineUpdate.install')" @cancel="confirmOpen = false" @confirm="installRelease" />
    <component :is="services.StepUpDialog" :controller="stepUp" />
  </section>
</template>

<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import { readApiError } from '@/shared/lib/apiError'
import { checkOnlineUpdate, getOnlineUpdate, startOnlineUpdate, type OnlineUpdateStatus } from '../api/update'
import { useSystemServices } from '../ports'
import { isSystemRequestCancelled, useSystemRequests } from '../queries/useSystemRequests'

const emit = defineEmits<{ updated: []; 'modal-open': [open: boolean] }>()
const { t } = useI18n()
const services = useSystemServices()
const requests = useSystemRequests(services.scope)
const stepUp = services.createStepUp()
const status = ref<OnlineUpdateStatus | null>(null)
const loading = ref(false)
const checking = ref(false)
const starting = ref(false)
const confirmOpen = ref(false)
const reconnecting = ref(false)
const submissionUnknown = ref(false)
const error = ref('')
const running = computed(() => status.value?.job?.state === 'running')
const manualRecovery = computed(() => status.value?.job?.state === 'manual_intervention_required')
const phaseKeys: Record<string, string> = {
  preparing: 'preparing', pulling: 'pulling', preflight: 'preflight', switching: 'switching', checking: 'checking',
  committing: 'committing', complete: 'complete', before_replacement: 'beforeReplacement', rolling_back: 'rollingBack',
  rolled_back: 'rolledBack', recovery_required: 'recoveryRequired', interrupted: 'interrupted'
}
const phaseText = computed(() => {
  const key = phaseKeys[status.value?.job?.phase ?? '']
  return key ? t('version.onlineUpdate.phases.' + key) : ''
})
const canStart = computed(() => status.value?.enabled && status.value.available && status.value.target && !status.value.target.requires_manual_migration
  && !running.value && !submissionUnknown.value && !loading.value && !checking.value && !starting.value && status.value.job?.state !== 'manual_intervention_required')
// This product has one trusted release page; do not turn metadata into an arbitrary link.
const releaseURL = computed(() => status.value?.target?.release_url === 'https://github.com/holeenlu/sub2api/releases/tag/NewAPI/latest'
  ? status.value.target.release_url : '')
let poll: ReturnType<typeof setTimeout> | undefined
let completedJob = ''
let operationKey = ''

function stopPolling() {
  if (poll !== undefined) clearTimeout(poll)
  poll = undefined
}

function schedulePoll() {
  stopPolling()
  if ((running.value || submissionUnknown.value) && services.scope.value) poll = setTimeout(() => { void readStatus(true) }, 3000)
}

function accept(value: OnlineUpdateStatus) {
  status.value = value
  reconnecting.value = false
  submissionUnknown.value = false
  if (value.job && value.job.state !== 'running') operationKey = ''
  if (value.job?.state === 'succeeded' && value.job.id !== completedJob) {
    completedJob = value.job.id
    emit('updated')
  }
  schedulePoll()
}

function actionError(value: unknown): string {
  if (services.isStepUpBlocked(value)) {
    return t(services.stepUpBlockReason(value) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled')
  }
  const code = marker(value)
  if (['UPDATE_MIGRATION_REQUIRED', 'MANUAL_MIGRATION_REQUIRED', 'ONLINE_UPDATE_MIGRATION_REQUIRED'].includes(code)) return t('version.onlineUpdate.manualMigration')
  if (code === 'UPDATE_TARGET_CHANGED') return t('version.onlineUpdate.targetChanged')
  return t('version.onlineUpdate.unavailable')
}

function marker(value: unknown): string {
  const detail = readApiError(value)
  return typeof detail.reason === 'string' ? detail.reason : String(detail.code ?? '')
}

async function readStatus(background = false) {
  if (!services.scope.value || starting.value || checking.value) return
  const request = requests.begin('status')
  if (!background) loading.value = true
  error.value = ''
  try {
    const value = await request.wait(signal => getOnlineUpdate({ signal }))
    accept(value)
  } catch (cause) {
    if (!request.isCurrent() || isSystemRequestCancelled(cause)) return
    if ([401, 403].includes(readApiError(cause).status ?? 0)) {
      reset()
      error.value = actionError(cause)
    } else if (running.value || submissionUnknown.value) {
      reconnecting.value = true
      schedulePoll()
    } else error.value = actionError(cause)
  } finally {
    if (request.isCurrent()) loading.value = false
  }
}

async function checkRelease() {
  if (!services.scope.value || checking.value || starting.value || running.value) return
  stopPolling()
  requests.cancel('status')
  const request = requests.begin('check')
  checking.value = true
  error.value = ''
  try {
    const value = await request.wait(signal => checkOnlineUpdate({ signal }))
    operationKey = ''
    accept(value)
  } catch (cause) {
    if (request.isCurrent() && !isSystemRequestCancelled(cause)) error.value = actionError(cause)
  } finally {
    if (request.isCurrent()) checking.value = false
  }
}

async function installRelease() {
  confirmOpen.value = false
  if (!canStart.value || !status.value?.target) return
  const commit = status.value.target.commit
  const request = requests.begin('install')
  requests.cancel('status')
  stopPolling()
  starting.value = true
  error.value = ''
  operationKey ||= crypto.randomUUID()
  try {
    const value = await stepUp.run(() => request.wait(signal => startOnlineUpdate(commit, operationKey, { signal })))
    request.check()
    accept(value)
  } catch (cause) {
    if (!request.isCurrent() || isSystemRequestCancelled(cause) || services.isStepUpCancelled(cause)) return
    // A lost response does not prove that the host rejected the operation.
    // Read its durable job before enabling another submission.
    const detail = readApiError(cause)
    if (detail.status === undefined || detail.status === 502 || detail.status === 503 || detail.status === 504) {
      reconnecting.value = true
      submissionUnknown.value = true
      schedulePoll()
      try {
        const value = await request.wait(signal => getOnlineUpdate({ signal }))
        accept(value)
      } catch (readError) {
        if (!request.isCurrent() || isSystemRequestCancelled(readError)) return
        if ([401, 403].includes(readApiError(readError).status ?? 0)) {
          reset()
          error.value = actionError(readError)
        } else schedulePoll()
      }
    } else {
      error.value = actionError(cause)
      if (marker(cause) === 'UPDATE_TARGET_CHANGED' && status.value) status.value = { ...status.value, available: false, target: undefined }
    }
  } finally {
    if (request.isCurrent()) starting.value = false
  }
}

function reloadPage() { window.location.reload() }
function reset() {
  stopPolling()
  stepUp.onCancel()
  status.value = null
  loading.value = false
  checking.value = false
  starting.value = false
  confirmOpen.value = false
  reconnecting.value = false
  submissionUnknown.value = false
  error.value = ''
  completedJob = ''
  operationKey = ''
}

requests.onInvalidate(reset)
watch(() => services.scope.value, (scope) => { if (scope) void readStatus() }, { immediate: true })
watch(() => confirmOpen.value || starting.value || stepUp.visible.value, (open) => emit('modal-open', open), { flush: 'sync' })
onScopeDispose(() => { stopPolling(); stepUp.onCancel(); emit('modal-open', false) })
</script>
