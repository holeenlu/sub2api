<template>
  <section class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600">
    <h4 class="font-medium">{{ t('autoBPSOps.authorization.title') }}</h4>
    <p v-if="canManageGrant" class="text-sm text-gray-500">{{ t('autoBPSOps.authorization.hint') }}</p>
    <p v-if="status" role="status" class="text-sm">
      {{ t(!status.authorized ? 'autoBPSOps.authorization.required' : status.source === 'primary' ? 'autoBPSOps.authorization.primary' : 'autoBPSOps.authorization.ready') }}
      <span v-if="status.refresh_required"> · {{ t('autoBPSOps.authorization.refreshRequired') }}</span>
    </p>
    <p v-if="status?.source === 'primary'" class="text-sm text-gray-500">{{ t('autoBPSOps.authorization.managePrimary') }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <div class="flex gap-2">
      <button v-if="canManageGrant" type="button" class="btn btn-secondary" :disabled="busy" @click="authorize">{{ t('autoBPSOps.authorization.authorize') }}</button>
      <button type="button" class="btn btn-secondary" :disabled="busy || !canOperate" @click="refresh">{{ t('autoBPSOps.refresh') }}</button>
      <button v-if="canManageGrant && status?.authorized" type="button" class="btn btn-secondary" :disabled="busy" @click="revoke">{{ t('autoBPSOps.authorization.revoke') }}</button>
    </div>
    <div v-if="canManageGrant && authURL" class="space-y-2">
      <a :href="authURL" target="_blank" rel="noopener noreferrer" class="text-primary-600 underline">{{ t('autoBPSOps.authorization.openLogin') }}</a>
      <label class="block text-sm" :for="`bps-callback-${accountId}`">{{ t('autoBPSOps.authorization.callback') }}</label>
      <input :id="`bps-callback-${accountId}`" v-model="callback" type="password" autocomplete="off" class="input" />
      <button type="button" class="btn btn-secondary" :disabled="busy || !callback" @click="exchange">{{ t('autoBPSOps.authorization.save') }}</button>
    </div>
    <details v-if="canManageGrant">
      <summary class="cursor-pointer text-sm">{{ t('autoBPSOps.authorization.import') }}</summary>
      <label :for="`bps-grant-${accountId}`" class="my-2 block text-sm">{{ t('autoBPSOps.authorization.importHint') }}</label>
      <textarea :id="`bps-grant-${accountId}`" v-model="grant" autocomplete="off" spellcheck="false" class="input font-mono" rows="4" />
      <button type="button" class="btn btn-secondary mt-2" :disabled="busy || !grant" @click="importGrant">{{ t('autoBPSOps.authorization.save') }}</button>
    </details>
  </section>
</template>

<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import accountsAPI from '../accounts'
import { useResourceEditorServices } from '../services'
import { useResourceTaskScope } from '../queries/resourceTaskScope'
import { extractApiErrorMessage } from '@/shared/lib/apiError'

const props = defineProps<{ accountId: number; proxyId?: number | null }>()
const services = useResourceEditorServices()
const tasks = useResourceTaskScope(() => [props.accountId, props.proxyId])
const { t } = useI18n()
const status = ref<Awaited<ReturnType<typeof services.excelAuthorization.get>> | null>(null)
const busy = ref(false), error = ref(''), authURL = ref(''), sessionID = ref(''), callback = ref(''), grant = ref('')
const canOperate = computed(() => services.scopeKey() !== '' && services.access.canAuthorize)
// The current API omits source for independent grants and returns primary for account OAuth.
const canManageGrant = computed(() => canOperate.value && status.value !== null
  && typeof status.value.authorized === 'boolean' && status.value.source === undefined)
type ResourceTask = ReturnType<typeof tasks.capture>

function clearDrafts() {
  authURL.value = ''; sessionID.value = ''; callback.value = ''; grant.value = ''
}
function resetEditor() {
  tasks.invalidate(); busy.value = false; status.value = null; error.value = ''; clearDrafts()
}
async function run(action: (id: number, pending: ResourceTask) => Promise<void>) {
  if (busy.value || !canOperate.value) return
  busy.value = true; error.value = ''
  const id = props.accountId
  const pending = tasks.capture()
  try { await action(id, pending) }
  catch (e) { if (pending.current()) error.value = extractApiErrorMessage(e, t('autoBPSOps.authorization.failed')) }
  finally { if (pending.current()) busy.value = false }
}
async function loadStatus(id: number, pending: ResourceTask) {
  if (!pending.current()) return
  status.value = null
  try {
    const data = await pending.wait(() => services.excelAuthorization.get(id))
    if (!pending.current()) return
    status.value = typeof data?.authorized === 'boolean' ? data : null
    if (!canManageGrant.value) clearDrafts()
  } catch (e) {
    if (pending.current()) clearDrafts()
    throw e
  }
}
function refresh() { return run(loadStatus) }
function authorize() {
  if (!canManageGrant.value) return
  const proxyID = props.proxyId
  return run(async (_id, pending) => {
    const data = await pending.wait(() => services.excelAuthorization.generateUrl(proxyID))
    if (pending.current()) { authURL.value = data.auth_url; sessionID.value = data.session_id; callback.value = '' }
  })
}
function exchange() {
  if (!canManageGrant.value || !authURL.value || !sessionID.value) return
  return run(async (id, pending) => {
    const url = new URL(callback.value.trim())
    const code = url.searchParams.get('code'), state = url.searchParams.get('state')
    if (url.origin !== 'https://bps.openai.com' || !code || !state) throw new Error(t('autoBPSOps.authorization.invalidCallback'))
    const session = sessionID.value
    const data = await pending.wait(() => accountsAPI.exchangeCode('/admin/openai/exchange-code', { session_id: session, code, state }))
    await pending.wait(() => services.excelAuthorization.save(id, data))
    if (!pending.current()) return
    callback.value = ''; authURL.value = ''; sessionID.value = ''
    await loadStatus(id, pending)
  })
}
function isCredentialObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
function importGrant() {
  if (!canManageGrant.value) return
  return run(async (id, pending) => {
    let credentials: unknown
    try { credentials = JSON.parse(grant.value) }
    catch { throw new Error(t('autoBPSOps.authorization.invalidCredentials')) }
    if (!isCredentialObject(credentials)) throw new Error(t('autoBPSOps.authorization.invalidCredentials'))
    const payload = credentials
    await pending.wait(() => services.excelAuthorization.save(id, payload))
    if (!pending.current()) return
    grant.value = ''
    await loadStatus(id, pending)
  })
}
function revoke() {
  if (!canManageGrant.value || !status.value?.authorized) return
  return run(async (id, pending) => {
    await pending.wait(() => services.excelAuthorization.revoke(id))
    await loadStatus(id, pending)
  })
}
watch(() => [services.scopeKey(), services.access.canAuthorize, services.access.isSuperAdmin, props.accountId, props.proxyId], () => {
  resetEditor()
  if (canOperate.value) void refresh()
}, { immediate: true, flush: 'sync' })
onScopeDispose(resetEditor)
</script>
