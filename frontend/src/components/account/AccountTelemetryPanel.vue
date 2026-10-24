<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { importAccountTelemetry } from '@/api/admin/accounts'
import { readAccountTelemetry, type AccountTelemetryState } from '@/utils/accountTelemetry'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ accountId?: number; value?: AccountTelemetryState | null }>()
const { t } = useI18n()
const { copied, copyToClipboard } = useClipboard()
const state = ref<AccountTelemetryState | null>(null)
const busy = ref(false)
const error = ref('')
const importInput = ref<HTMLInputElement>()
const device = computed(() => state.value?.device_installation)
const bundleJSON = computed(() => device.value ? JSON.stringify(device.value, null, 2) : '')
const deviceFacts = computed(() => {
  if (!device.value) return []
  return [
    ['deviceId', device.value.device_id],
    ['machineId', device.value.machine_id],
    ['cliVersion', device.value.cli_version],
    ['platform', device.value.platform],
    ['arch', device.value.arch],
    ['locale', device.value.locale],
    ['exportedAt', device.value.exported_at],
    ['importedAt', state.value?.imported_at],
    ['sessionId', state.value?.session_id]
  ].filter(([, value]) => value)
})
let generation = 0
let disposed = false

// Re-rendering the same account snapshot must not cancel an in-flight import
// or replace its result. A new revision, owner or account invalidates old work.
watch([() => props.accountId, () => props.value?.revision, () => props.value?.owner, () => props.value?.status], () => {
  generation++
  state.value = readAccountTelemetry({ account_telemetry: props.value })
  busy.value = false
  error.value = ''
  if (importInput.value) importInput.value.value = ''
}, { immediate: true })

async function importFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  const accountId = props.accountId
  if (!file || !accountId || busy.value) { input.value = ''; return }
  if (file.size > 1024 * 1024) {
    error.value = t('admin.accounts.telemetry.importTooLarge')
    input.value = ''
    return
  }
  const serial = ++generation
  busy.value = true
  error.value = ''
  try {
    let bundle: unknown
    try { bundle = JSON.parse(await file.text()) } catch { throw new Error(t('admin.accounts.telemetry.importInvalid')) }
    if (disposed || serial !== generation) return
    const result = await importAccountTelemetry(accountId, bundle)
    if (disposed || serial !== generation) return
    state.value = readAccountTelemetry({ account_telemetry: result })
  } catch (err) {
    if (!disposed && serial === generation) {
      const message = extractApiErrorMessage(err, t('admin.accounts.telemetry.importFailed'))
      const key = ({ telemetry_import_invalid: 'importInvalid', telemetry_account_changed: 'importAccountChanged' } as Record<string, string>)[message]
      error.value = key ? t('admin.accounts.telemetry.' + key) : message
    }
  } finally {
    if (!disposed && serial === generation) {
      input.value = ''
      busy.value = false
    }
  }
}

onBeforeUnmount(() => { disposed = true; generation++ })
</script>

<template>
  <section class="rounded-lg border border-gray-200 p-4 dark:border-dark-600" data-testid="account-telemetry-panel" :aria-busy="busy">
    <h3 class="text-sm font-medium">{{ t('admin.accounts.telemetry.title') }}</h3>
    <p class="input-hint mt-1">{{ t(accountId ? 'admin.accounts.telemetry.hint' : 'admin.accounts.telemetry.createHint') }}</p>
    <template v-if="accountId">
      <p class="mt-2 text-sm" data-testid="account-telemetry-status" role="status">{{ t('admin.accounts.telemetry.' + (state?.status || 'waiting_import')) }}</p>
      <p v-if="error" role="alert" class="mt-1 text-sm text-red-600">{{ error }}</p>
      <input ref="importInput" type="file" accept=".json,application/json" :disabled="busy" class="hidden" data-testid="account-telemetry-import-file" @change="importFile" />
      <button type="button" class="btn btn-secondary mt-3" :disabled="busy" data-testid="account-telemetry-import" @click="importInput?.click()">{{ t('admin.accounts.telemetry.' + (busy ? 'importing' : device ? 'reimport' : 'import')) }}</button>
      <p class="input-hint mt-2">{{ t('admin.accounts.telemetry.importHint') }}</p>
      <div v-if="device" class="mt-3 space-y-3">
        <dl class="grid gap-3 text-xs sm:grid-cols-2">
          <div v-for="[label, value] in deviceFacts" :key="label" class="min-w-0" :data-testid="'account-telemetry-' + label">
            <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.telemetry.' + label) }}</dt>
            <dd class="mt-1 break-all font-mono">{{ value }}</dd>
          </div>
        </dl>
        <details class="text-xs">
          <summary>{{ t('admin.accounts.telemetry.deviceBundle') }}</summary>
          <textarea readonly :value="bundleJSON" :aria-label="t('admin.accounts.telemetry.deviceBundle')" rows="8" class="input mt-2 font-mono text-xs" data-testid="account-telemetry-bundle" />
          <button type="button" class="btn btn-secondary mt-2" data-testid="account-telemetry-copy" @click="copyToClipboard(bundleJSON)">{{ t(copied ? 'admin.accounts.telemetry.copied' : 'admin.accounts.telemetry.copy') }}</button>
        </details>
      </div>
    </template>
  </section>
</template>
