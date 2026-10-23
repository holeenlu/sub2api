<template>
  <section class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600" data-testid="siwc-settings" :aria-labelledby="`siwc-title-${accountId}`">
    <h4 :id="`siwc-title-${accountId}`" class="font-medium">{{ t('siwc.title') }}</h4>
    <label class="input-label" :for="`siwc-protocol-${accountId}`">{{ t('siwc.protocol') }}</label>
    <select :id="`siwc-protocol-${accountId}`" class="input" :value="enabled ? 'siwc' : 'codex'"
      :disabled="busy" data-testid="siwc-protocol" @change="selectProtocol">
      <option value="codex">{{ t('siwc.codex') }}</option>
      <option value="siwc" :disabled="!ready">{{ t('siwc.siwc') }}</option>
    </select>
    <p class="input-hint">{{ t('siwc.protocolHint') }}</p>
    <p role="status" aria-live="polite" class="text-sm" data-testid="siwc-status">{{ t(statusKey) }}</p>
    <p class="input-hint">{{ t('siwc.entitlementHint') }}</p>
    <dl v-if="status?.registered" class="space-y-1 text-sm" data-testid="siwc-registration">
      <template v-if="status.client_id">
        <dt class="text-gray-500">{{ t('siwc.registration') }}</dt>
        <dd class="break-all font-mono text-xs">{{ status.client_id }}</dd>
      </template>
      <template v-if="status.identity_verified">
        <dt class="text-gray-500">{{ t('siwc.identity') }}</dt>
        <dd class="break-all" data-testid="siwc-identity">{{ status.email || status.subject }}</dd>
      </template>
    </dl>
    <p v-if="status?.identity_verified" class="input-hint">{{ t('siwc.identityHint') }}</p>
    <p v-if="revocationUnconfirmed" class="text-sm text-amber-700 dark:text-amber-300" data-testid="siwc-revocation-unconfirmed">{{ t('siwc.revocationUnconfirmed') }}</p>
    <div class="flex flex-wrap gap-2">
      <button v-if="canAuthorize" type="button" class="btn btn-secondary btn-sm" :disabled="busy" data-testid="siwc-authorize" @click="authorize">{{ t(status?.registered ? 'siwc.reconnect' : 'siwc.authorize') }}</button>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" data-testid="siwc-refresh" @click="refresh">{{ t('siwc.refresh') }}</button>
      <button v-if="canAuthorize && (status?.bound || status?.registered)" type="button" class="btn btn-secondary btn-sm" :disabled="busy" data-testid="siwc-disconnect" @click="disconnect">{{ t('siwc.disconnect') }}</button>
    </div>
    <div v-if="authorization && canAuthorize" class="space-y-2" data-testid="siwc-browser-flow">
      <p class="input-hint">{{ t('siwc.listenerHint') }}</p>
      <code class="block break-all rounded bg-gray-100 p-2 text-xs dark:bg-dark-700">python3 tools/openai_siwc_authorize.py</code>
      <label :for="`siwc-auth-url-${accountId}`" class="input-label">{{ t('siwc.authLink') }}</label>
      <textarea :id="`siwc-auth-url-${accountId}`" class="input w-full break-all font-mono text-xs" :value="authorization.auth_url"
        readonly rows="4" spellcheck="false" data-testid="siwc-auth-url" @focus="($event.target as HTMLTextAreaElement).select()" />
      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-secondary btn-sm" data-testid="siwc-copy" @click="copyAuthorization">{{ t('siwc.copyLink') }}</button>
        <a class="btn btn-secondary btn-sm" :href="authorization.auth_url" target="_blank" rel="noopener noreferrer" data-testid="siwc-open">{{ t('siwc.openPrepared') }}</a>
      </div>
      <p class="input-hint">{{ t('siwc.authLinkHint') }}</p>
      <label :for="`siwc-callback-${accountId}`" class="input-label">{{ t('siwc.callback') }}</label>
      <input :id="`siwc-callback-${accountId}`" v-model="callback" type="password" class="input" autocomplete="off" spellcheck="false" data-testid="siwc-callback" />
      <p class="input-hint">{{ t('siwc.callbackHint') }}</p>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="busy || !callback.trim()" data-testid="siwc-complete" @click="complete">{{ t('siwc.complete') }}</button>
    </div>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { authorizeSIWC, completeSIWC, disconnectSIWC, getSIWCStatus, type SIWCAuthorization, type SIWCStatus } from '@/api/admin/siwc'

const props = withDefaults(defineProps<{ accountId: number; enabled: boolean; active: boolean; canAuthorize?: boolean }>(), { canAuthorize: true })
const emit = defineEmits<{ 'update:enabled': [boolean] }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const status = ref<SIWCStatus | null>(null)
const authorization = ref<SIWCAuthorization | null>(null)
const callback = ref('')
const busy = ref(false)
const loaded = ref(false)
const error = ref('')
const revocationUnconfirmed = ref(false)
const ready = computed(() => loaded.value && status.value?.bound === true && status.value.identity_verified === true && status.value.direct_access === true && !status.value.refresh_required)
const statusKey = computed(() => {
  if (!loaded.value) return 'siwc.loading'
  if (status.value?.refresh_required) return 'siwc.refreshRequired'
  if (status.value?.bound && status.value.identity_verified && !status.value.direct_access) return 'siwc.identityOnly'
  if (ready.value) return 'siwc.bound'
  return status.value?.registered ? 'siwc.registered' : 'siwc.unbound'
})
let generation = 0

function selectProtocol(event: Event) {
  const enabled = (event.target as HTMLSelectElement).value === 'siwc'
  if (!enabled || ready.value) emit('update:enabled', enabled)
}
async function loadStatus(id: number, current: number) {
  const next = await getSIWCStatus(id)
  if (current !== generation || !props.active) return
  status.value = next
  loaded.value = true
}
async function run(action: (id: number, current: number) => Promise<void>) {
  if (busy.value || !props.active) return
  const current = generation
  busy.value = true
  error.value = ''
  try { await action(props.accountId, current) }
  catch (cause) { if (current === generation) error.value = extractI18nErrorMessage(cause, t, 'siwc.errors', t('siwc.failed')) }
  finally { if (current === generation) busy.value = false }
}
function refresh() { return run(loadStatus) }
function authorize() {
  if (!props.canAuthorize) return
  return run(async (id, current) => {
    authorization.value = null
    callback.value = ''
    const result = await authorizeSIWC(id)
    if (current !== generation) return
    let url: URL
    try { url = new URL(result.auth_url) } catch { throw new Error(t('siwc.invalidAuthorization')) }
    if (url.origin !== 'https://auth.openai.com' || url.pathname !== '/api/accounts/authorize' ||
        url.username || url.password || url.hash || !result.session_id?.trim() ||
        !url.searchParams.get('state') || url.searchParams.getAll('state').length !== 1 ||
        url.searchParams.get('redirect_uri') !== 'http://127.0.0.1:1455/auth/callback') {
      throw new Error(t('siwc.invalidAuthorization'))
    }
    authorization.value = result
  })
}
async function copyAuthorization() {
  if (!authorization.value) return
  const current = generation
  const copied = await copyToClipboard(authorization.value.auth_url)
  if (current === generation) error.value = copied ? '' : t('siwc.copyFailed')
}
function complete() {
  if (!authorization.value || !props.canAuthorize) return
  const attempt = authorization.value
  const callbackURL = callback.value.trim()
  return run(async (id, current) => {
    let url: URL
    try { url = new URL(callbackURL) } catch { throw new Error(t('siwc.invalidCallback')) }
    const request = new URL(attempt.auth_url)
    if (url.origin !== 'http://127.0.0.1:1455' || url.pathname !== '/auth/callback' || url.username || url.password || url.hash ||
        ['state', 'code', 'error', 'client_id'].some(key => url.searchParams.getAll(key).length > 1) ||
        (!url.searchParams.get('code') && !url.searchParams.get('error')) ||
        (url.searchParams.has('code') && url.searchParams.has('error'))) throw new Error(t('siwc.invalidCallback'))
    if (url.searchParams.get('state') !== request.searchParams.get('state')) throw new Error(t('siwc.stateMismatch'))
    const client = request.searchParams.get('client_id')
    const returnedClient = url.searchParams.get('client_id')
    if (client !== 'dynamic_agent_client' && returnedClient && returnedClient !== client) throw new Error(t('siwc.clientMismatch'))
    try { await completeSIWC(id, attempt.session_id, callbackURL) }
    catch (cause) {
      // Refresh registration metadata after a failed exchange without hiding its error.
      if (current === generation) {
        try { await loadStatus(id, current) } catch { /* Retain the exchange failure. */ }
      }
      throw cause
    } finally {
      // Codes are one-use. After an exchange attempt, start a fresh authorization;
      // the server preserves any issued client ID even when exchange fails.
      if (current === generation) { authorization.value = null; callback.value = '' }
    }
    if (current === generation) await loadStatus(id, current)
  })
}
function disconnect() {
  if (!props.canAuthorize) return
  return run(async (id, current) => {
    const next = await disconnectSIWC(id)
    if (current !== generation) return
    revocationUnconfirmed.value = !next.remote_revocation_confirmed
    authorization.value = null
    callback.value = ''
    emit('update:enabled', false)
    await loadStatus(id, current)
  })
}
watch(() => [props.accountId, props.active] as const, () => {
  generation++
  status.value = null
  authorization.value = null
  callback.value = ''
  loaded.value = false
  busy.value = false
  error.value = ''
  revocationUnconfirmed.value = false
  if (props.active) void refresh()
}, { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>
