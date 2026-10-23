<template>
  <section class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600" data-testid="cloud-authorization">
    <h4 class="font-medium">{{ t('admin.accounts.openai.cloud.title') }}</h4>
    <p class="text-sm text-gray-500">{{ t('admin.accounts.openai.cloud.hint') }}</p>
    <p v-if="status" role="status" class="text-sm">{{ t(`admin.accounts.openai.cloud.status.${status.status}`) }}</p>
    <p v-if="status?.last_verified_at" class="text-xs text-gray-500">{{ t('admin.accounts.openai.cloud.lastVerified', { time: status.last_verified_at }) }}</p>
    <p v-if="status?.last_error" class="text-sm text-amber-600">{{ status.last_error }}</p>
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
    <div v-if="status?.authorized" class="space-y-2">
      <label :for="`cloud-test-model-${accountId}`" class="input-label">{{ t('admin.accounts.openai.cloud.testModel') }}</label>
      <div class="flex flex-wrap gap-2">
        <select :id="`cloud-test-model-${accountId}`" v-model="testModel" class="input flex-1" :disabled="busy" data-testid="cloud-test-model">
          <option value="" disabled>{{ t('admin.accounts.selectTestModel') }}</option>
          <option v-for="model in candidateModels" :key="model" :value="model">{{ model }}</option>
        </select>
        <button type="button" class="btn btn-secondary" :disabled="busy || !testModel || !canTest" data-testid="cloud-test" @click="emit('test', testModel)">{{ t('admin.accounts.openai.cloud.verify') }}</button>
      </div>
      <p class="text-xs text-gray-500">{{ t('admin.accounts.openai.cloud.verificationHint') }}</p>
    </div>
    <fieldset :disabled="!canEdit || busy" class="space-y-2">
      <legend class="input-label">{{ t('admin.accounts.openai.cloud.models') }}</legend>
      <p v-if="!selectableModels.length" class="text-sm text-gray-500">{{ t('admin.accounts.openai.cloud.noVerifiedModels') }}</p>
      <label v-for="model in selectableModels" :key="model" class="flex items-center gap-2 text-sm">
        <input type="checkbox" :value="model" :checked="models.includes(model)" :disabled="!verifiedModels.includes(model) && !models.includes(model)" @change="selectModel(model, ($event.target as HTMLInputElement).checked)" />
        <span>{{ model }}</span>
        <span v-if="!verifiedModels.includes(model)" class="text-amber-600">{{ t('admin.accounts.openai.cloud.notVerified') }}</span>
      </label>
      <label class="flex items-center gap-2 text-sm">
        <input type="checkbox" :checked="enabled" :disabled="!enabled && !canEnable" data-testid="cloud-enabled" @change="emit('update:enabled', ($event.target as HTMLInputElement).checked)" />
        <span>{{ t('admin.accounts.openai.cloud.enable') }}</span>
      </label>
      <p v-if="bpsEnabled" class="text-xs text-amber-600">{{ t('admin.accounts.openai.cloud.bpsConflict') }}</p>
      <p class="text-xs text-gray-500">{{ t('admin.accounts.openai.cloud.saveHint') }}</p>
    </fieldset>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { authorizeCloud, completeCloudAuthorization, disconnectCloud, getCloudAuthorization, type OpenAICloudAuthorization } from '@/api/admin/accounts'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ accountId: number; enabled: boolean; models: string[]; bpsEnabled: boolean; canAuthorize: boolean; canEdit: boolean; canTest: boolean }>()
const emit = defineEmits<{
  'update:enabled': [value: boolean]
  'update:models': [value: string[]]
  status: [value: OpenAICloudAuthorization | null]
  test: [model: string]
}>()
const { t } = useI18n()
const status = ref<OpenAICloudAuthorization | null>(null)
const busy = ref(false), error = ref(''), authURL = ref(''), sessionID = ref(''), callback = ref(''), testModel = ref('')
let generation = 0
const verifiedModels = computed(() => status.value?.verified_models ?? [])
const candidateModels = computed(() => [...new Set([...(status.value?.models ?? []), ...verifiedModels.value])])
const selectableModels = computed(() => [...new Set([...verifiedModels.value, ...props.models])])
const canEnable = computed(() => status.value?.authorized && status.value.status === 'verified' && !props.bpsEnabled && props.models.length > 0 && props.models.every(model => verifiedModels.value.includes(model)))
function clearCallback() { authURL.value = ''; sessionID.value = ''; callback.value = '' }
function reset() { generation++; busy.value = false; status.value = null; error.value = ''; testModel.value = ''; clearCallback(); emit('status', null) }
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
  if (!candidateModels.value.includes(testModel.value)) testModel.value = candidateModels.value[0] ?? ''
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
    emit('update:enabled', false)
    emit('update:models', [])
    await load(id, version)
  })
}
function disconnect() {
  if (!props.canAuthorize) return
  return run(async (id, version) => {
    await disconnectCloud(id)
    if (version !== generation) return
    clearCallback(); emit('update:enabled', false); emit('update:models', [])
    acceptStatus({ authorized: false, enabled: false, status: 'unauthorized', verified_models: [], models: [] })
    testModel.value = ''
    await load(id, version)
  })
}
function selectModel(model: string, checked: boolean) {
  if (!props.canEdit || (checked && !verifiedModels.value.includes(model))) return
  emit('update:models', checked ? [...new Set([...props.models, model])] : props.models.filter(value => value !== model))
}
watch(() => props.accountId, () => { reset(); void refresh() }, { immediate: true })
onBeforeUnmount(reset)
defineExpose({ refresh, reset })
</script>
