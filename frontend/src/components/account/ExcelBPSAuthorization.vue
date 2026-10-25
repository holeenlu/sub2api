<template>
  <section class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600">
    <h4 class="font-medium">{{ t('autoBPSOps.authorization.title') }}</h4>
    <p class="text-sm text-gray-500">{{ t('autoBPSOps.authorization.hint') }}</p>
    <p v-if="status" role="status" class="text-sm">
      {{ t(status.source === 'primary' ? 'autoBPSOps.authorization.primary' : status.authorized ? 'autoBPSOps.authorization.ready' : 'autoBPSOps.authorization.required') }}
      <span v-if="status.refresh_required"> · {{ t('autoBPSOps.authorization.refreshRequired') }}</span>
    </p>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <div class="flex gap-2">
      <button v-if="status?.source !== 'primary'" type="button" class="btn btn-secondary" :disabled="busy" @click="authorize">{{ t('autoBPSOps.authorization.authorize') }}</button>
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="refresh">{{ t('autoBPSOps.refresh') }}</button>
      <button v-if="status?.authorized && status.source !== 'primary'" type="button" class="btn btn-secondary" :disabled="busy" @click="revoke">{{ t('autoBPSOps.authorization.revoke') }}</button>
    </div>
    <div v-if="authURL" class="space-y-2">
      <a :href="authURL" target="_blank" rel="noopener noreferrer" class="text-primary-600 underline">{{ t('autoBPSOps.authorization.openLogin') }}</a>
      <label class="block text-sm" :for="`bps-callback-${accountId}`">{{ t('autoBPSOps.authorization.callback') }}</label>
      <input :id="`bps-callback-${accountId}`" v-model="callback" type="password" autocomplete="off" class="input" />
      <button type="button" class="btn btn-secondary" :disabled="busy || !callback" @click="exchange">{{ t('autoBPSOps.authorization.save') }}</button>
    </div>
    <details v-if="status?.source !== 'primary'">
      <summary class="cursor-pointer text-sm">{{ t('autoBPSOps.authorization.import') }}</summary>
      <label :for="`bps-grant-${accountId}`" class="my-2 block text-sm">{{ t('autoBPSOps.authorization.importHint') }}</label>
      <textarea :id="`bps-grant-${accountId}`" v-model="grant" autocomplete="off" spellcheck="false" class="input font-mono" rows="4" />
      <button type="button" class="btn btn-secondary mt-2" :disabled="busy || !grant" @click="importGrant">{{ t('autoBPSOps.authorization.save') }}</button>
    </details>
  </section>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiClient } from '@/api/client'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ accountId: number; proxyId?: number | null }>()
const { t } = useI18n()
const status = ref<{ authorized: boolean; refresh_required?: boolean; source?: string } | null>(null)
const busy = ref(false), error = ref(''), authURL = ref(''), sessionID = ref(''), callback = ref(''), grant = ref('')
const endpoint = () => `/admin/openai/accounts/${props.accountId}/excel-authorization`
let generation = 0
async function run(action: (id: number, version: number) => Promise<void>) {
  if (busy.value) return
  busy.value = true; error.value = ''
  const version = generation, id = props.accountId
  try { await action(id, version) }
  catch (e) { if (version === generation) error.value = extractApiErrorMessage(e, t('autoBPSOps.authorization.failed')) }
  finally { if (version === generation) busy.value = false }
}
async function loadStatus(id: number, version: number) {
  const { data } = await apiClient.get(`/admin/openai/accounts/${id}/excel-authorization`)
  if (version === generation) status.value = data
}
function refresh() { return run(loadStatus) }
function authorize() {
  return run(async (_id, version) => {
    const { data } = await apiClient.post('/admin/openai/generate-auth-url', { oauth_profile: 'excel', proxy_id: props.proxyId })
    if (version === generation) { authURL.value = data.auth_url; sessionID.value = data.session_id; callback.value = '' }
  })
}
function exchange() {
  return run(async (id, version) => {
    const url = new URL(callback.value.trim())
    if (url.origin !== 'https://bps.openai.com' || !url.searchParams.get('code') || !url.searchParams.get('state')) throw new Error(t('autoBPSOps.authorization.invalidCallback'))
    const { data } = await apiClient.post('/admin/openai/exchange-code', { session_id: sessionID.value, code: url.searchParams.get('code'), state: url.searchParams.get('state') })
    if (version !== generation) return
    await apiClient.post(`/admin/openai/accounts/${id}/excel-authorization`, data)
    if (version === generation) { callback.value = ''; authURL.value = ''; sessionID.value = '' }
    await loadStatus(id, version)
  })
}
function importGrant() {
  return run(async (id, version) => {
    const credentials = JSON.parse(grant.value)
    await apiClient.post(`/admin/openai/accounts/${id}/excel-authorization`, credentials)
    if (version === generation) grant.value = ''
    await loadStatus(id, version)
  })
}
function revoke() { return run(async (id, version) => { await apiClient.delete(endpoint()); await loadStatus(id, version) }) }
watch(() => props.accountId, () => {
  generation++; busy.value = false; status.value = null; authURL.value = ''; sessionID.value = ''; callback.value = ''; grant.value = ''; error.value = ''
  void refresh()
}, { immediate: true })
</script>
