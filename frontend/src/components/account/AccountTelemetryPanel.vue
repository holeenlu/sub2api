<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { generateAccountTelemetry, getById } from '@/api/admin/accounts'
import { readAccountTelemetry, type AccountTelemetryState } from '@/utils/accountTelemetry'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ accountId?: number; value?: AccountTelemetryState | null }>()
const { t } = useI18n()
const { copied, copyToClipboard } = useClipboard()
const state = ref<AccountTelemetryState | null>(props.value || null)
const busy = ref(false)
const error = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
let generation = 0
let disposed = false
function stop() { if (timer) clearTimeout(timer); timer = undefined }
async function refresh(serial: number) {
  if (!props.accountId || disposed || serial !== generation) return
  try {
    const account = await getById(props.accountId)
    if (disposed || serial !== generation) return
    state.value = readAccountTelemetry(account.extra)
    if (state.value?.status === 'pending') timer = setTimeout(() => void refresh(serial), 2000)
  } catch (err) {
    if (!disposed && serial === generation) error.value = extractApiErrorMessage(err, t('admin.accounts.telemetry.loadFailed'))
  }
}
watch(() => [props.accountId, props.value] as const, () => {
  stop(); const serial = ++generation
  state.value = props.value || null
  if (state.value?.status === 'pending') timer = setTimeout(() => void refresh(serial), 2000)
}, { immediate: true })
async function generate() {
  if (!props.accountId || busy.value) return
  stop(); const serial = ++generation
  busy.value = true; error.value = ''
  try {
    const result = await generateAccountTelemetry(props.accountId)
    if (disposed || serial !== generation) return
    state.value = result
    timer = setTimeout(() => void refresh(serial), 1000)
  } catch (err) {
    if (!disposed && serial === generation) error.value = extractApiErrorMessage(err, t('admin.accounts.telemetry.generateFailed'))
  } finally { if (!disposed && serial === generation) busy.value = false }
}
onBeforeUnmount(() => { disposed = true; generation++; stop() })
</script>

<template>
  <section class="rounded-lg border border-gray-200 p-4 dark:border-dark-600" data-testid="account-telemetry-panel">
    <h3 class="text-sm font-medium">{{ t('admin.accounts.telemetry.title') }}</h3>
    <p class="input-hint mt-1">{{ t(accountId ? 'admin.accounts.telemetry.hint' : 'admin.accounts.telemetry.createHint') }}</p>
    <template v-if="accountId">
      <p class="mt-2 text-sm" data-testid="account-telemetry-status">{{ t('admin.accounts.telemetry.' + (state?.status || 'absent')) }}</p>
      <p v-if="state?.error_code" class="mt-1 text-xs text-red-600">{{ state.error_code }}</p>
      <p v-if="error" role="alert" class="mt-1 text-sm text-red-600">{{ error }}</p>
      <button type="button" class="btn btn-secondary mt-3" :disabled="busy || state?.status === 'pending'" data-testid="account-telemetry-generate" @click="generate">{{ t('admin.accounts.telemetry.generate') }}</button>
      <div v-if="state?.profile" class="mt-3 space-y-2">
        <textarea readonly :value="state.profile.payload_json" rows="8" class="input font-mono text-xs" data-testid="account-telemetry-body" />
        <details class="text-xs"><summary>{{ t('admin.accounts.telemetry.headers') }}</summary><pre class="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-all">{{ JSON.stringify(state.profile.headers, null, 2) }}</pre></details>
        <button type="button" class="btn btn-secondary" @click="copyToClipboard(state.profile.payload_json)">{{ t(copied ? 'admin.accounts.telemetry.copied' : 'admin.accounts.telemetry.copy') }}</button>
      </div>
    </template>
  </section>
</template>
