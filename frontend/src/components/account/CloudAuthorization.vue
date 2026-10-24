<template>
  <section class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600" data-testid="cloud-authorization">
    <h4 class="font-medium">{{ t('admin.accounts.openai.cloud.authorizationTitle') }}</h4>
    <p class="text-sm text-gray-500">{{ t('admin.accounts.openai.cloud.hint') }}</p>
    <p v-if="status" role="status" class="text-sm">{{ t(`admin.accounts.openai.cloud.status.${status.status}`) }}</p>
    <p v-if="status?.last_error" class="text-sm text-amber-600">{{ status.last_error }}</p>
    <CloudReconciliation v-if="status?.runtime_blocked" :key="accountId" :account-id="accountId" />
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <div class="flex flex-wrap gap-2">
      <button v-if="canAuthorize" type="button" class="btn btn-secondary" :disabled="busy" data-testid="cloud-authorize" @click="authorize">
        {{ t(status?.authorized ? 'admin.accounts.openai.cloud.reauthorize' : 'admin.accounts.openai.cloud.authorize') }}
      </button>
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="refresh">{{ t('common.refresh') }}</button>
      <button v-if="canAuthorize && status?.authorized" type="button" class="btn btn-secondary" :disabled="busy" data-testid="cloud-disconnect" @click="disconnect">
        {{ t('admin.accounts.openai.cloud.disconnect') }}
      </button>
    </div>
    <p class="text-xs text-gray-500">{{ t('admin.accounts.openai.cloud.sharedAuthorization') }}</p>
    <div v-if="authURL" class="space-y-2">
      <a :href="authURL" target="_blank" rel="noopener noreferrer" class="text-primary-600 underline">{{ t('admin.accounts.openai.cloud.openLogin') }}</a>
      <label :for="`cloud-callback-${accountId}`" class="block text-sm">{{ t('admin.accounts.openai.cloud.callback') }}</label>
      <input :id="`cloud-callback-${accountId}`" v-model="callback" type="password" autocomplete="off" spellcheck="false" class="input" data-testid="cloud-callback" />
      <button type="button" class="btn btn-secondary" :disabled="busy || !callback.trim()" data-testid="cloud-save-authorization" @click="exchange">{{ t('admin.accounts.openai.cloud.completeAuthorization') }}</button>
    </div>
    <fieldset :disabled="!canEdit || busy" class="space-y-2 border-t border-gray-200 pt-3 dark:border-dark-600">
      <label class="flex items-center gap-2 text-sm">
        <input type="checkbox" :checked="enabled" :disabled="!enabled && !canEnable" data-testid="cloud-enabled" @change="emit('update:enabled', ($event.target as HTMLInputElement).checked)" />
        <span>{{ t('admin.accounts.openai.cloud.enable') }}</span>
      </label>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.cloud.priorityHint') }}</p>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.cloud.modelPolicy') }}</p>
    </fieldset>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { authorizeCloud, completeCloudAuthorization, disconnectCloud, getCloudAuthorization, type OpenAICloudAuthorization } from '@/api/admin/accounts'
import { extractApiErrorMessage } from '@/utils/apiError'
import CloudReconciliation from './CloudReconciliation.vue'

const props = defineProps<{ accountId: number; enabled: boolean; canAuthorize: boolean; canEdit: boolean }>()
const emit = defineEmits<{
  'update:enabled': [value: boolean]
  status: [value: OpenAICloudAuthorization | null]
}>()
const { t } = useI18n()
const status = ref<OpenAICloudAuthorization | null>(null)
const busy = ref(false), error = ref(''), authURL = ref(''), sessionID = ref(''), callback = ref('')
let generation = 0
const canEnable = computed(() => status.value?.authorized === true && !status.value.runtime_blocked)
function clearCallback() { authURL.value = ''; sessionID.value = ''; callback.value = '' }
function reset() { generation++; busy.value = false; status.value = null; error.value = ''; clearCallback(); emit('status', null) }
function acceptStatus(value: OpenAICloudAuthorization) { status.value = value; emit('status', value) }
async function run(action: (id: number, version: number) => Promise<void>) {
  if (busy.value) return
  busy.value = true; error.value = ''
  const version = generation, id = props.accountId
  try { await action(id, version) }
  catch (cause) { if (version === generation) error.value = extractApiErrorMessage(cause, t('admin.accounts.openai.cloud.failed')) }
  finally { if (version === generation) busy.value = false }
}
async function load(id: number, version: number) {
  const value = await getCloudAuthorization(id)
  if (version !== generation) return
  acceptStatus(value)
}
function refresh() { return run(load) }
function authorize() {
  if (!props.canAuthorize) return
  return run(async (id, version) => {
    const value = await authorizeCloud(id)
    if (version !== generation) return
    const url = new URL(value.auth_url)
    if (url.protocol !== 'https:' || url.username || url.password) throw new Error(t('admin.accounts.openai.cloud.failed'))
    authURL.value = url.href; sessionID.value = value.session_id; callback.value = ''
  })
}
function exchange() {
  if (!props.canAuthorize) return
  return run(async (id, version) => {
    let url: URL
    try {
      url = new URL(callback.value.trim())
      const redirect = new URL(new URL(authURL.value).searchParams.get('redirect_uri') ?? '')
      if (url.origin !== redirect.origin || url.pathname !== redirect.pathname || !url.searchParams.get('code') || !url.searchParams.get('state')) throw new Error()
    } catch { throw new Error(t('admin.accounts.openai.cloud.invalidCallback')) }
    const value = await completeCloudAuthorization(id, { session_id: sessionID.value, code: url.searchParams.get('code')!, state: url.searchParams.get('state')! })
    if (version !== generation) return
    clearCallback(); acceptStatus(value)
    emit('update:enabled', value.enabled)
    await load(id, version)
  })
}
function disconnect() {
  if (!props.canAuthorize) return
  return run(async (id, version) => {
    await disconnectCloud(id)
    if (version !== generation) return
    clearCallback(); emit('update:enabled', false)
    acceptStatus({ authorized: false, enabled: false, status: 'unauthorized' })
    await load(id, version)
  })
}
watch(() => props.accountId, () => { reset(); void refresh() }, { immediate: true })
onBeforeUnmount(reset)
</script>
